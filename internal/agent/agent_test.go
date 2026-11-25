package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/spacefs"
)

// sse writes a Chat Completions stream of the chunks.
func sse(w http.ResponseWriter, chunks ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, c := range chunks {
		fmt.Fprintf(w, "data: %s\n\n", c)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func TestChatCompletionsStreamsTextAndAssemblesCalls(t *testing.T) {
	var got chatRequest
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&got)
		sse(w,
			`{"choices":[{"delta":{"reasoning_content":"想一想"}}]}`,
			`{"choices":[{"delta":{"content":"先"}}]}`,
			`{"choices":[{"delta":{"content":"搜索"}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"grep","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"c2","type":"function","function":{"name":"read","arguments":"{\"path\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"pattern\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"延期\"}"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"\"a.md\"}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
		)
	}))
	defer srv.Close()
	p := &ChatCompletions{BaseURL: srv.URL + "/v1/", Model: "m", APIKey: "k"}
	var deltas []Delta
	resp, err := p.Complete(context.Background(), Request{
		System:   "sys",
		Messages: []Message{{Role: User, Text: "为什么延期？"}},
		Tools:    []ToolSpec{{Name: "grep", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}},
	}, func(d Delta) { deltas = append(deltas, d) })
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer k" || got.Model != "m" || !got.Stream || len(got.Messages) != 2 || *got.Messages[0].Content != "sys" || got.Tools[0].Function.Name != "grep" {
		t.Fatalf("request %+v auth %q", got, auth)
	}
	m := resp.Message
	if m.Text != "先搜索" || m.Reasoning != "想一想" || len(deltas) != 3 {
		t.Fatalf("text %q reasoning %q deltas %v", m.Text, m.Reasoning, deltas)
	}
	if len(m.Calls) != 2 || m.Calls[0] != (ToolCall{"c1", "grep", `{"pattern":"延期"}`}) || m.Calls[1] != (ToolCall{"c2", "read", `{"path":"a.md"}`}) {
		t.Fatalf("calls %+v", m.Calls)
	}
	if resp.Usage != (Usage{10, 5}) {
		t.Fatalf("usage %+v", resp.Usage)
	}
}

func TestChatCompletionsMessagesAndReasoningReplay(t *testing.T) {
	msgs := []Message{
		{Role: User, Text: "q1"},
		{Role: Assistant, Reasoning: "old", Calls: []ToolCall{{"a", "grep", "{}"}}},
		{Role: ToolRole, CallID: "a", Text: "r"},
		{Role: Assistant, Text: "answer1"},
		{Role: User, Text: "q2"},
		{Role: Assistant, Reasoning: "new", Calls: []ToolCall{{"b", "read", "{}"}}},
		{Role: ToolRole, CallID: "b", Text: "r2"},
	}
	body := (&ChatCompletions{ReplayReasoning: true}).body(Request{Messages: msgs})
	if body.Messages[1].ReasoningContent != "old" || body.Messages[5].ReasoningContent != "new" {
		t.Fatal("every turn's reasoning goes back")
	}
	if body.Messages[1].Content != nil || body.Messages[1].ToolCalls[0].ID != "a" || body.Messages[2].ToolCallID != "a" {
		t.Fatalf("assistant call message %+v", body.Messages[1])
	}
	data, _ := json.Marshal(body.Messages[1])
	if !strings.Contains(string(data), `"content":null`) {
		t.Fatalf("a call-only message has null content: %s", data)
	}
	if b := (&ChatCompletions{}).body(Request{Messages: msgs}); b.Messages[5].ReasoningContent != "" {
		t.Fatal("reasoning sent without ReplayReasoning")
	}
}

func TestChatCompletionsErrors(t *testing.T) {
	status, body := 401, `{"error":{"message":"Incorrect API key"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status == 0 {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"半\"}}]}\n\n")
			return // no finish, no [DONE]
		}
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	defer srv.Close()
	p := &ChatCompletions{BaseURL: srv.URL}
	_, err := p.Complete(context.Background(), Request{}, func(Delta) {})
	var api *APIError
	if !errors.As(err, &api) || api.Status != 401 || api.Message != "Incorrect API key" || Retryable(err) {
		t.Fatalf("401: %v", err)
	}
	status, body = 429, "slow down"
	_, err = p.Complete(context.Background(), Request{}, func(Delta) {})
	if !errors.As(err, &api) || api.Message != "slow down" || !Retryable(err) {
		t.Fatalf("429: %v", err)
	}
	status = 0
	_, err = p.Complete(context.Background(), Request{}, func(Delta) {})
	if !errors.Is(err, ErrStreamCut) || !Retryable(err) {
		t.Fatalf("cut stream: %v", err)
	}
}

// script is a provider replying with the given responses in turn, and
// keeping the requests it was sent.
type script struct {
	mu      sync.Mutex
	replies []func(Request, func(Delta)) (Response, error)
	reqs    []Request
}

func (s *script) Complete(ctx context.Context, req Request, onDelta func(Delta)) (Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, req)
	if len(s.reqs) > len(s.replies) {
		return Response{}, errors.New("no more replies")
	}
	return s.replies[len(s.reqs)-1](req, onDelta)
}

func calls(cs ...ToolCall) func(Request, func(Delta)) (Response, error) {
	return func(Request, func(Delta)) (Response, error) {
		return Response{Message: Message{Role: Assistant, Calls: cs}}, nil
	}
}

func answer(text string) func(Request, func(Delta)) (Response, error) {
	return func(_ Request, d func(Delta)) (Response, error) {
		d(Delta{Text: text})
		return Response{Message: Message{Role: Assistant, Text: text}}, nil
	}
}

func testSpace(t *testing.T) *spacefs.FS {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "评审.md"), []byte("# 评审纪要\n\n决定延期发布，原因是支付联调未完成。"), 0o644)
	return spacefs.New(root, func(name, content string) (*spacefs.Page, error) {
		var parts []spacefs.Part
		for i, p := range strings.Split(content, "\n\n") {
			parts = append(parts, spacefs.Part{Block: fmt.Sprint(i), Text: p})
		}
		return spacefs.PageOf(name, parts), nil
	})
}

func TestRunCallsToolsInParallelThenAnswers(t *testing.T) {
	p := &script{replies: []func(Request, func(Delta)) (Response, error){
		calls(ToolCall{"0", ToolSearch, `{"query":"为什么延期 支付"}`}, ToolCall{"1", ToolGrep, `{"pattern":"延期"}`}, ToolCall{"2", ToolRead, `{"path":"评审.md"}`}, ToolCall{"3", "nope", `{}`}, ToolCall{"4", ToolRead, `{bad`}),
		answer("因为支付联调 [评审.md#L3]。"),
	}}
	a := &Agent{Provider: p, System: "s", Tools: SpaceTools(testSpace(t))}
	var events []Event
	added, err := a.Run(context.Background(), []Message{{Role: User, Text: "为什么延期？"}}, func(e Event) { events = append(events, e) })
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 7 || added[6].Text != "因为支付联调 [评审.md#L3]。" {
		t.Fatalf("added %+v", added)
	}
	if !strings.Contains(added[1].Text, "relevant passages") || added[1].IsError {
		t.Fatalf("search result %q", added[1].Text)
	}
	if !strings.Contains(added[2].Text, "评审.md:3:决定延期发布") || added[2].IsError {
		t.Fatalf("grep result %q", added[2].Text)
	}
	if !strings.Contains(added[3].Text, "3→决定延期发布") {
		t.Fatalf("read result %q", added[3].Text)
	}
	if !added[4].IsError || !strings.Contains(added[4].Text, "no tool named") || !added[5].IsError || !strings.Contains(added[5].Text, "not valid JSON") {
		t.Fatalf("errors %+v %+v", added[4], added[5])
	}
	if len(p.reqs[1].Messages) != 7 || p.reqs[1].Messages[6].CallID != "4" {
		t.Fatalf("second step sees the results in call order: %+v", p.reqs[1].Messages)
	}
	starts, dones := 0, 0
	for _, e := range events {
		switch e.Kind {
		case ToolStart:
			starts++
		case ToolDone:
			dones++
		}
	}
	if starts != 5 || dones != 5 || events[len(events)-1].Kind != StepDone {
		t.Fatalf("events %+v", events)
	}
}

func TestRunAnswersWithoutToolsAtTheStepLimit(t *testing.T) {
	grep := ToolCall{"x", ToolGrep, `{"pattern":"a"}`}
	p := &script{replies: []func(Request, func(Delta)) (Response, error){calls(grep), calls(grep), answer("就这些")}}
	a := &Agent{Provider: p, Tools: SpaceTools(testSpace(t)), MaxSteps: 3}
	added, err := a.Run(context.Background(), []Message{{Role: User, Text: "q"}}, nil)
	if err != nil || added[len(added)-1].Text != "就这些" {
		t.Fatalf("%v %+v", err, added)
	}
	last := p.reqs[2]
	if last.Tools != nil || !strings.Contains(last.Messages[len(last.Messages)-1].Text, "used all your tool calls") {
		t.Fatalf("last step %+v", last)
	}
}

func TestRunRetriesBeforeAnythingStreamed(t *testing.T) {
	waits := 0
	p := &script{replies: []func(Request, func(Delta)) (Response, error){
		func(Request, func(Delta)) (Response, error) { return Response{}, &APIError{Status: 503} },
		func(Request, func(Delta)) (Response, error) { return Response{}, ErrStreamCut },
		answer("好"),
	}}
	a := &Agent{Provider: p, Retries: 2, Wait: func(context.Context, int) error { waits++; return nil }}
	added, err := a.Run(context.Background(), []Message{{Role: User, Text: "q"}}, nil)
	if err != nil || waits != 2 || added[0].Text != "好" {
		t.Fatalf("%v waits %d %+v", err, waits, added)
	}
	// Not after text was shown, and not for a refusal.
	p = &script{replies: []func(Request, func(Delta)) (Response, error){
		func(_ Request, d func(Delta)) (Response, error) {
			d(Delta{Text: "半"})
			return Response{}, ErrStreamCut
		},
	}}
	a.Provider = p
	if _, err := a.Run(context.Background(), []Message{{Role: User, Text: "q"}}, nil); !errors.Is(err, ErrStreamCut) {
		t.Fatalf("retried after streaming: %v", err)
	}
	p = &script{replies: []func(Request, func(Delta)) (Response, error){
		func(Request, func(Delta)) (Response, error) { return Response{}, &APIError{Status: 400} },
	}}
	a.Provider = p
	if _, err := a.Run(context.Background(), []Message{{Role: User, Text: "q"}}, nil); len(p.reqs) != 1 || err == nil {
		t.Fatalf("retried a refusal: %v", err)
	}
}

func TestRunStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &script{replies: []func(Request, func(Delta)) (Response, error){
		func(Request, func(Delta)) (Response, error) {
			cancel()
			return Response{Message: Message{Calls: []ToolCall{{"1", ToolGlob, "{}"}}}}, nil
		},
	}}
	a := &Agent{Provider: p, Tools: SpaceTools(testSpace(t))}
	added, err := a.Run(ctx, []Message{{Role: User, Text: "q"}}, nil)
	if !errors.Is(err, context.Canceled) || len(p.reqs) != 1 || len(added) != 2 {
		t.Fatalf("%v %d %+v", err, len(p.reqs), added)
	}
}

func TestFitDropsOldestToolResults(t *testing.T) {
	big := strings.Repeat("x", 1000)
	a := &Agent{ContextChars: 2500}
	msgs := a.fit([]Message{
		{Role: User, Text: "q"},
		{Role: ToolRole, Text: big},
		{Role: ToolRole, Text: big},
		{Role: ToolRole, Text: big},
	})
	if msgs[1].Text != omitted || msgs[2].Text != big || msgs[3].Text != big {
		t.Fatalf("fit %v", []int{len(msgs[1].Text), len(msgs[2].Text), len(msgs[3].Text)})
	}
}

func TestCitations(t *testing.T) {
	text := "延期 [工作/评审 纪要.arcmd#L12-L18]，见 [a.md#L3] 与 [b.md#L9–7]; 不是 [[链接]] 或 [x](y)。"
	cs := Citations(text)
	if len(cs) != 3 {
		t.Fatalf("%+v", cs)
	}
	if cs[0].Path != "工作/评审 纪要.arcmd" || cs[0].From != 12 || cs[0].To != 18 || text[cs[0].Start:cs[0].End] != "[工作/评审 纪要.arcmd#L12-L18]" {
		t.Fatalf("%+v", cs[0])
	}
	if cs[1] != (Citation{cs[1].Start, cs[1].End, "a.md", 3, 3}) || cs[2].From != 7 || cs[2].To != 9 {
		t.Fatalf("%+v %+v", cs[1], cs[2])
	}
}

func TestPromptAndSchemas(t *testing.T) {
	p := Prompt(Context{Space: "工作", Overview: "1 documents.\n- a.md", Open: "a.md"})
	for _, want := range []string{`space "工作"`, "[path#L12-L18]", "this document\" means it", "- a.md"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	for _, tool := range SpaceTools(testSpace(t)) {
		var s struct {
			Type     string         `json:"type"`
			Props    map[string]any `json:"properties"`
			Required []string       `json:"required"`
		}
		if err := json.Unmarshal(tool.Spec.Parameters, &s); err != nil || s.Type != "object" || len(s.Props) == 0 || s.Required == nil {
			t.Errorf("%s schema %s: %v", tool.Spec.Name, tool.Spec.Parameters, err)
		}
	}
}

func TestSettingsCheck(t *testing.T) {
	for _, c := range []struct {
		s   Settings
		err error
	}{
		{Settings{BaseURL: "https://api.openai.com/v1", Model: "m", APIKey: "k"}, nil},
		{Settings{BaseURL: "http://localhost:11434/v1", Model: "llama", APIKey: ""}, nil},
		{Settings{BaseURL: "http://127.0.0.1:8080", Model: "m", APIKey: ""}, nil},
		{Settings{BaseURL: "http://api.example.com/v1", Model: "m", APIKey: "k"}, ErrBadAddress},
		{Settings{BaseURL: "api.openai.com", Model: "m", APIKey: "k"}, ErrBadAddress},
		{Settings{BaseURL: "https://api.openai.com/v1", Model: " ", APIKey: "k"}, ErrNoModel},
		{Settings{BaseURL: "https://api.openai.com/v1", Model: "m", APIKey: ""}, ErrNoKey},
	} {
		if err := c.s.Check(); !errors.Is(err, c.err) {
			t.Errorf("%+v: %v, want %v", c.s, err, c.err)
		}
	}
	if !(Settings{BaseURL: "https://api.deepseek.com"}).Provider().(*ChatCompletions).ReplayReasoning || (Settings{BaseURL: "https://api.openai.com/v1"}).Provider().(*ChatCompletions).ReplayReasoning {
		t.Fatal("reasoning replay follows the service")
	}
	if PresetOf("nope").ID != Custom || PresetOf("deepseek").BaseURL == "" {
		t.Fatal("presets")
	}
}

func TestModels(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/v1/models":
			io.WriteString(w, `{"object":"list","data":[{"id":"deepseek-v4-pro"},{"id":"deepseek-flash"},{"id":""},{"id":"deepseek-flash"}]}`)
		default:
			w.WriteHeader(401)
			io.WriteString(w, `{"error":{"message":"bad key"}}`)
		}
	}))
	defer srv.Close()
	got, err := (&ChatCompletions{BaseURL: srv.URL + "/v1/", APIKey: "k"}).Models(context.Background())
	if err != nil || strings.Join(got, ",") != "deepseek-flash,deepseek-v4-pro" || auth != "Bearer k" {
		t.Fatalf("%v %q %q", err, got, auth)
	}
	_, err = (&ChatCompletions{BaseURL: srv.URL}).Models(context.Background())
	var api *APIError
	if !errors.As(err, &api) || api.Status != 401 {
		t.Fatalf("%v", err)
	}
	if err := (Settings{BaseURL: "https://api.openai.com/v1", APIKey: "k"}).CheckService(); err != nil {
		t.Fatal(err)
	}
}

func TestChatStreamOmitsIndexContextCancelAndUTF8Truncation(t *testing.T) {
	// 1. OpenAI-compatible servers omitting tool_call index on streamed argument chunks.
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"id":"c_no_idx","type":"function","function":{"name":"read","arguments":"{\"path\":"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"\"C# 笔记.md\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"id":"c_second","type":"function","function":{"name":"grep","arguments":"{\"pattern\":"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"\"x\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
		"",
	}, "\n\n")
	resp, err := readChatStream(context.Background(), strings.NewReader(stream), func(Delta) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Message.Calls) != 2 ||
		resp.Message.Calls[0] != (ToolCall{ID: "c_no_idx", Name: "read", Args: `{"path":"C# 笔记.md"}`}) ||
		resp.Message.Calls[1] != (ToolCall{ID: "c_second", Name: "grep", Args: `{"pattern":"x"}`}) {
		t.Fatalf("omitted-index calls = %+v", resp.Message.Calls)
	}

	// 2. Context cancellation checked at the start of each scanner iteration.
	ctx, cancel := context.WithCancel(context.Background())
	_, err = readChatStream(ctx, strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"content\":\"前\"}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"content\":\"后\"},\"finish_reason\":\"stop\"}]}\n\n",
	), func(Delta) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from readChatStream, got %v", err)
	}

	// 3. UTF-8 safe truncation in truncateUTF8, runTool, and apiError.
	cjk := strings.Repeat("中", 200) // 600 bytes (3 bytes per rune)
	for n := 0; n <= 10; n++ {
		s := truncateUTF8(cjk, n)
		if !utf8.ValidString(s) || len(s) > n {
			t.Fatalf("truncateUTF8(%d) invalid or too long: len=%d valid=%v", n, len(s), utf8.ValidString(s))
		}
	}
	bigToolOut := "a" + strings.Repeat("中", maxToolResult/3+10) // misaligns maxToolResult mid-rune
	cutOut, err := runTool(context.Background(), map[string]Tool{
		"big": {Run: func(context.Context, json.RawMessage) (string, error) { return bigToolOut, nil }},
	}, ToolCall{ID: "1", Name: "big", Args: "{}"})
	if err != nil || !utf8.ValidString(cutOut) || !strings.HasSuffix(cutOut, "\n(result cut; ask for less)") {
		t.Fatalf("runTool UTF-8 truncation failed: err=%v valid=%v", err, utf8.ValidString(cutOut))
	}

	errBody := `{"error":{"message":"` + "x" + strings.Repeat("错", 200) + `"}}` // 500 falls mid-rune
	apiErr := apiError(&http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(errBody))})
	if !utf8.ValidString(apiErr.Error()) {
		t.Fatalf("apiError produced invalid UTF-8: %q", apiErr.Error())
	}

	// 4. Citations with '#' in filenames and case-insensitive localhost check.
	cs := Citations("请参考 [C# 笔记.md#L1-L5] 与 [Issue #12.md#L3] 以及普通链接 [说明](https://example.com)。")
	if len(cs) != 2 || cs[0].Path != "C# 笔记.md" || cs[0].From != 1 || cs[0].To != 5 || cs[1].Path != "Issue #12.md" || cs[1].From != 3 || cs[1].To != 3 {
		t.Fatalf("Citations with # in filename = %+v", cs)
	}
	if !IsLocal("http://LOCALHOST:11434/v1") {
		t.Fatal("IsLocal rejected uppercase LOCALHOST")
	}
	if err := (Settings{BaseURL: "http://LOCALHOST:11434/v1", Model: "qwen"}).Check(); err != nil {
		t.Fatalf("Settings.Check rejected uppercase LOCALHOST: %v", err)
	}
}
