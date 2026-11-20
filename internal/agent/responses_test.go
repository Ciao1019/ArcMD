package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProtocolSelection(t *testing.T) {
	for _, tc := range []struct{ model, setting, want string }{
		{"gpt-6.2-sol", "", ProtocolResponses},
		{"gpt-6.2-astra", "", ProtocolResponses},
		{"openai/gpt-6.2-astra", "", ProtocolResponses},
		{"other", "", ProtocolChat},
		{"gpt-6.2-sol", ProtocolChat, ProtocolChat},
		{"other", ProtocolResponses, ProtocolResponses},
	} {
		s := Settings{BaseURL: "https://api.openai.com/v1", Model: tc.model, Protocol: tc.setting}
		_, responses := s.Provider().(*Responses)
		if s.EffectiveProtocol() != tc.want || responses != (tc.want == ProtocolResponses) {
			t.Errorf("%+v: selected %q", tc, s.EffectiveProtocol())
		}
	}
}

func TestResponsesStatelessToolLoop(t *testing.T) {
	const reason = `{"id":"rs1","type":"reasoning","encrypted_content":"opaque","summary":[{"type":"summary_text","text":"先查资料"}]}`
	const call = `{"id":"fc1","type":"function_call","call_id":"call-one","name":"read","arguments":"{\"file\":1}","status":"completed"}`
	const answer = `{"id":"msg2","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"查到了","annotations":[]}]}`
	step := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		var b struct {
			Model, Instructions string
			Input               []json.RawMessage
			Tools               []responseTool
			Store               *bool
			Stream              bool
			Reasoning           map[string]string
			Include             []string
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Error(err)
			return
		}
		if r.Method != "POST" || r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer test" ||
			r.Header.Get("Accept") != "text/event-stream" || !b.Stream || b.Store == nil || *b.Store ||
			b.Instructions != "system" || b.Reasoning["effort"] != "medium" || b.Reasoning["summary"] != "auto" ||
			len(b.Tools) != 1 || b.Tools[0].Name != "read" || b.Tools[0].Type != "function" ||
			string(b.Tools[0].Parameters) != `{"type":"object"}` || len(b.Include) != 1 || b.Include[0] != "reasoning.encrypted_content" {
			t.Errorf("unexpected request %s %+v", r.URL, b)
		}
		switch step {
		case 1:
			sse(w, `{"type":"response.reasoning_summary_text.delta","delta":"先查"}`,
				`{"type":"response.reasoning_summary_text.delta","delta":"资料"}`,
				`{"type":"response.function_call_arguments.delta","item_id":"fc1","delta":"{\"file\":"}`,
				`{"type":"response.function_call_arguments.delta","item_id":"fc1","delta":"1}"}`,
				fmt.Sprintf(`{"type":"response.completed","response":{"status":"completed","output":[%s,%s],"usage":{"input_tokens":8,"output_tokens":5}}}`, reason, call))
		case 2:
			if len(b.Input) != 4 || string(b.Input[1]) != reason || string(b.Input[2]) != call {
				t.Errorf("reasoning/call replay %s", b.Input)
			}
			var toolOutput map[string]string
			if len(b.Input) != 4 || json.Unmarshal(b.Input[3], &toolOutput) != nil || toolOutput["type"] != "function_call_output" ||
				toolOutput["call_id"] != "call-one" || toolOutput["output"] != "file one" {
				t.Errorf("tool output: %s", b.Input)
			}
			sse(w, `{"type":"response.output_text.delta","delta":"查"}`, `{"type":"response.output_text.delta","delta":"到了"}`,
				fmt.Sprintf(`{"type":"response.completed","response":{"status":"completed","output":[%s]}}`, answer))
		default:
			t.Errorf("unexpected step %d", step)
			w.WriteHeader(400)
		}
	}))
	defer srv.Close()
	provider := (Settings{Protocol: ProtocolResponses, BaseURL: srv.URL + "/v1", Model: "gpt-6.2-astra", APIKey: "test"}).Provider()
	calls := 0
	tool := Tool{Spec: ToolSpec{Name: "read", Parameters: json.RawMessage(`{"type":"object"}`)}, Run: func(_ context.Context, args json.RawMessage) (string, error) {
		calls++
		if string(args) != `{"file":1}` {
			t.Errorf("arguments %s", args)
		}
		return "file one", nil
	}}
	a := Agent{System: "system", Provider: provider, Tools: []Tool{tool}}
	var text, summary strings.Builder
	msgs, err := a.Run(context.Background(), []Message{{Role: User, Text: "q"}}, func(e Event) {
		if e.Kind == TextDelta {
			text.WriteString(e.Text)
		}
		if e.Kind == ReasoningDelta {
			summary.WriteString(e.Text)
		}
		if e.Kind == StepDone && len(e.Message.Calls) > 0 && e.Usage != (Usage{Input: 8, Output: 5}) {
			t.Errorf("usage %+v", e.Usage)
		}
	})
	if err != nil || step != 2 || calls != 1 || text.String() != "查到了" || summary.String() != "先查资料" || len(msgs) != 3 ||
		msgs[0].Reasoning != "先查资料" || msgs[2].Text != "查到了" {
		t.Fatalf("result %+v %v: %d calls; %s / %s", msgs, err, calls, text.String(), summary.String())
	}
	other := Responses{ChatCompletions{BaseURL: "https://changed.example/v1", Model: "other"}}
	wire, _ := json.Marshal(other.request(Request{Messages: msgs}))
	if strings.Contains(string(wire), "opaque") || !strings.Contains(string(wire), "call-one") {
		t.Fatalf("provider switch: %s", wire)
	}
}

func TestResponsesMustCompleteBeforeExecutingTools(t *testing.T) {
	provider := Responses{}
	for _, tc := range []struct {
		stream string
		cut    bool
	}{
		{`{"type":"response.function_call_arguments.done","call_id":"call-1","arguments":"{}"}`, true},
		{`{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"}}}`, false},
		{`{"type":"response.failed","response":{"error":{"message":"failed"}}}`, false},
		{`{"type":"error","message":"bad input"}`, false},
	} {
		resp, err := provider.readStream(context.Background(), strings.NewReader("data: "+tc.stream+"\n\ndata: [DONE]\n\n"), func(Delta) {})
		if err == nil || errors.Is(err, ErrStreamCut) != tc.cut || len(resp.Message.Calls) != 0 {
			t.Errorf("%s: %+v %v", tc.stream, resp, err)
		}
	}
}

func TestResponsesCompletedItemEvents(t *testing.T) {
	c := Responses{}
	var buf strings.Builder
	items := []string{
		`{"type":"reasoning","summary":[{"type":"summary_text","text":"checking"}],"encrypted_content":"sealed"}`,
		`{"type":"function_call","call_id":"a","name":"first","arguments":"{}"}`,
		`{"type":"function_call","call_id":"b","name":"second","arguments":"{\"v\":2}"}`,
	}
	for _, i := range []int{2, 0, 1} {
		fmt.Fprintf(&buf, "data: {\"type\":\"response.output_item.done\",\"output_index\":%d,\"item\":%s}\n\n", i, items[i])
	}
	for _, output := range []string{"", `, "output":[]`} {
		completed := "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"" + output + "}}\n\n"
		var summary string
		resp, err := c.readStream(context.Background(), strings.NewReader(buf.String()+completed), func(d Delta) { summary += d.Reasoning })
		if err != nil || len(resp.Message.Calls) != 2 || resp.Message.Calls[0].ID != "a" || resp.Message.Calls[1].ID != "b" || summary != "checking" {
			t.Fatalf("%+v %q %v", resp, summary, err)
		}
	}
}

func TestAutomaticProtocolHosts(t *testing.T) {
	for _, test := range []struct{ base, want string }{
		{"https://OPENROUTER.AI/api/v1/", ProtocolResponses}, {"https://API.OPENAI.COM/v1", ProtocolResponses},
		{"https://gateway.example/v1", ProtocolChat}, {"https://api.openai.com.example/v1", ProtocolChat},
		{"http://localhost:11434/v1", ProtocolChat},
	} {
		s := Settings{BaseURL: test.base, Model: " OpenAI/GPT-6.2-SOL "}
		if got := s.EffectiveProtocol(); got != test.want {
			t.Errorf("%s: got %q, want %q", test.base, got, test.want)
		}
	}
}

func TestResponsesManualNonReasoner(t *testing.T) {
	c := Responses{ChatCompletions{BaseURL: "https://custom.example/v1", Model: "ordinary"}}
	resp, err := c.completed(responseObject{Output: []json.RawMessage{
		json.RawMessage(`{"type":"reasoning","id":"rs_private","summary":[{"text":"shown"}]}`),
		json.RawMessage(`{"type":"function_call","name":"read","call_id":"call_1","arguments":"{}"}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	c.BaseURL += "/"
	if !resp.Message.responses.matches(&c) {
		t.Fatal("trailing slash discarded replay")
	}
	wire, err := json.Marshal(c.request(Request{Messages: []Message{resp.Message}}))
	if err != nil || strings.Contains(string(wire), `"include"`) || strings.Contains(string(wire), "rs_private") || !strings.Contains(string(wire), "call_1") || resp.Message.Reasoning != "shown" {
		t.Fatalf("%s, %v", wire, err)
	}
}

func TestFitProtectsPendingToolBatch(t *testing.T) {
	c := &Responses{ChatCompletions{BaseURL: "https://a/v1", Model: "r"}}
	replay := &responseReplay{base: c.BaseURL, model: c.Model, items: []json.RawMessage{json.RawMessage(`{"type":"reasoning","encrypted_content":"` + strings.Repeat("x", 150000) + `"}`)}}
	history := []Message{
		{Role: Assistant, Calls: []ToolCall{{ID: "old"}}, responses: replay}, {Role: ToolRole, Text: strings.Repeat("a", 1000)},
		{Role: Assistant, Calls: []ToolCall{{ID: "a"}, {ID: "b"}}, responses: replay}, {Role: ToolRole, Text: "fresh A"}, {Role: ToolRole, Text: "fresh B"},
	}
	a := Agent{Provider: c}
	got := a.fit(append([]Message(nil), history...))
	if got[0].responses != nil || got[1].Text != omitted || got[2].responses != replay || got[3].Text != "fresh A" || got[4].Text != "fresh B" {
		t.Fatal("incorrect replay or tool retention")
	}
	for _, provider := range []Provider{&ChatCompletions{}, &Responses{ChatCompletions{BaseURL: "https://b/v1", Model: c.Model}}, &Responses{ChatCompletions{BaseURL: c.BaseURL, Model: "other"}}} {
		a.Provider = provider
		got = a.fit(append([]Message(nil), history...))
		if got[1].Text != history[1].Text {
			t.Fatal("budget charged for replay on another endpoint or model")
		}
	}
}

func TestResponseStreamErrors(t *testing.T) {
	c := Responses{}
	for _, tc := range []struct {
		event  string
		status int
		retry  bool
	}{
		{`{"type":"error","code":"rate_limit_exceeded","message":"slow"}`, 429, true},
		{`{"type":"response.failed","response":{"error":{"code":"server_error","message":"busy"}}}`, 500, true},
		{`{"type":"error","error":{"code":"invalid_api_key","message":"invalid"}}`, 401, false},
		{`{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"}}}`, 400, false},
	} {
		_, err := c.readStream(context.Background(), strings.NewReader("data: "+tc.event+"\n"), func(Delta) {})
		var api *APIError
		if !errors.As(err, &api) || api.Status != tc.status || Retryable(err) != tc.retry {
			t.Errorf("%s: %v", tc.event, err)
		}
	}
	data := "data: " + `{"type":"response.reasoning_text.delta","delta":"checking"}` + "\n" + "data: " + `{"type":"response.completed","response":{"output":[]}}` + "\n"
	var shown string
	r, err := c.readStream(context.Background(), strings.NewReader(data), func(d Delta) { shown += d.Reasoning })
	if err != nil || shown != "checking" || r.Message.Reasoning != shown {
		t.Fatalf("%+v, %q, %v", r, shown, err)
	}
}

func TestResponsesRetryBeforeFirstDelta(t *testing.T) {
	for _, partial := range []bool{false, true} {
		var attempts int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			if attempts == 1 {
				if partial {
					sse(w, `{"type":"response.output_text.delta","delta":"partial"}`, `{"type":"error","code":"rate_limit_exceeded"}`)
				} else {
					sse(w, `{"type":"error","code":"rate_limit_exceeded"}`)
				}
				return
			}
			sse(w, `{"type":"response.output_text.delta","delta":"ok"}`, `{"type":"response.completed","response":{"status":"completed"}}`)
		}))
		a := Agent{Provider: &Responses{ChatCompletions{BaseURL: srv.URL, Model: "test"}}, Retries: 1, Wait: func(context.Context, int) error { return nil }}
		var text string
		_, err := a.Run(context.Background(), []Message{{Role: User, Text: "q"}}, func(e Event) {
			if e.Kind == TextDelta {
				text += e.Text
			}
		})
		srv.Close()
		if partial {
			if err == nil || attempts != 1 || text != "partial" {
				t.Fatalf("partial: attempts %d, text %q, error %v", attempts, text, err)
			}
		} else if err != nil || attempts != 2 || text != "ok" {
			t.Fatalf("retry: attempts %d, text %q, error %v", attempts, text, err)
		}
	}
}

func TestFitCountsPlainTextAfterSwitch(t *testing.T) {
	a := Agent{Provider: &ChatCompletions{}, ContextChars: 500}
	result := strings.Repeat("r", 200)
	history := []Message{
		{Role: Assistant, Text: strings.Repeat("t", 400), responses: &responseReplay{items: []json.RawMessage{json.RawMessage("{}")}}},
		{Role: Assistant, Calls: []ToolCall{{ID: "old"}}}, {Role: ToolRole, CallID: "old", Text: result},
		{Role: User, Text: "another question"},
	}
	if got := a.fit(history); got[2].Text != omitted {
		t.Fatal("budget ignored assistant text sent by the new provider")
	}
}
