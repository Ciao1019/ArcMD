package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// ChatCompletions is a service speaking OpenAI's Chat Completions format,
// which most model services offer (DeepSeek, Qwen, Kimi, GLM, OpenRouter,
// Ollama…): POST {BaseURL}/chat/completions, the reply streamed as
// server-sent events.
type ChatCompletions struct {
	// BaseURL is the service's API root, as "https://api.openai.com/v1".
	BaseURL string
	Model   string
	APIKey  string
	Client  *http.Client
	// ReplayReasoning sends every assistant message's reasoning back as
	// reasoning_content, which DeepSeek's thinking models require when
	// tools are offered (a 400 otherwise). Other services may refuse the
	// field, so it is off by default.
	ReplayReasoning bool
}

type chatMessage struct {
	Role             string         `json:"role"`
	Content          *string        `json:"content"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
}

type chatToolCall struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type chatRequest struct {
	Model           string        `json:"model"`
	Messages        []chatMessage `json:"messages"`
	Tools           []chatTool    `json:"tools,omitempty"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
	Stream          bool          `json:"stream"`
}

type chatChunk struct {
	Choices []struct {
		Delta struct {
			Content          string         `json:"content"`
			ReasoningContent string         `json:"reasoning_content"`
			ToolCalls        []chatToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func text(s string) *string { return &s }

// body is the request in the service's format.
func (c *ChatCompletions) body(req Request) chatRequest {
	out := chatRequest{Model: c.Model, Stream: true}
	// With function tools, this model's Chat Completions endpoint requires
	// an explicit opt-out. Omitting the field leaves the service default active.
	if strings.TrimPrefix(c.Model, "openai/") == "gpt-6.2-sol" && len(req.Tools) > 0 {
		out.ReasoningEffort = "none"
	}
	if req.System != "" {
		out.Messages = append(out.Messages, chatMessage{Role: "system", Content: text(req.System)})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case User:
			out.Messages = append(out.Messages, chatMessage{Role: "user", Content: text(m.Text)})
		case ToolRole:
			out.Messages = append(out.Messages, chatMessage{Role: "tool", Content: text(m.Text), ToolCallID: m.CallID})
		case Assistant:
			cm := chatMessage{Role: "assistant"}
			if m.Text != "" || len(m.Calls) == 0 {
				cm.Content = text(m.Text)
			}
			if c.ReplayReasoning {
				cm.ReasoningContent = m.Reasoning
			}
			for _, call := range m.Calls {
				tc := chatToolCall{ID: call.ID, Type: "function"}
				tc.Function.Name, tc.Function.Arguments = call.Name, call.Args
				cm.ToolCalls = append(cm.ToolCalls, tc)
			}
			out.Messages = append(out.Messages, cm)
		}
	}
	for _, t := range req.Tools {
		ct := chatTool{Type: "function"}
		ct.Function.Name, ct.Function.Description, ct.Function.Parameters = t.Name, t.Description, t.Parameters
		out.Tools = append(out.Tools, ct)
	}
	return out
}

// Complete sends the request and reads the streamed reply.
func (c *ChatCompletions) Complete(ctx context.Context, req Request, onDelta func(Delta)) (Response, error) {
	return c.postStream(ctx, "/chat/completions", c.body(req), onDelta, readChatStream)
}

func (c *ChatCompletions) postStream(ctx context.Context, endpoint string, body any, onDelta func(Delta), read func(context.Context, io.Reader, func(Delta)) (Response, error)) (Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, err
	}
	url := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/") + endpoint
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return Response{}, err
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		hr.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(hr)
	if err != nil {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{}, fmt.Errorf("cannot reach the model service: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Response{}, apiError(res)
	}
	return read(ctx, res.Body, onDelta)
}

// apiError reads a refusal's message from its body, in OpenAI's
// {"error": {"message": …}} or as plain text.
func apiError(res *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	var body struct {
		Error json.RawMessage `json:"error"`
	}
	msg := strings.TrimSpace(string(data))
	if json.Unmarshal(data, &body) == nil && len(body.Error) > 0 {
		var e struct {
			Message string `json:"message"`
		}
		var s string
		if json.Unmarshal(body.Error, &e) == nil && e.Message != "" {
			msg = e.Message
		} else if json.Unmarshal(body.Error, &s) == nil {
			msg = s
		}
	}
	if len(msg) > 500 {
		msg = msg[:500] + "…"
	}
	return &APIError{Status: res.StatusCode, Message: msg}
}

// readChatStream assembles the reply from its events: text and reasoning
// as they come, tool calls from the pieces of their arguments, by index.
func readChatStream(ctx context.Context, r io.Reader, onDelta func(Delta)) (Response, error) {
	var resp Response
	var content, reasoning strings.Builder
	calls := map[int]*ToolCall{}
	finished := false
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		line := sc.Text()
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			finished = true
			break
		}
		var chunk chatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // a keep-alive or a field this reader does not know
		}
		if chunk.Error != nil {
			return Response{}, &APIError{Status: 500, Message: chunk.Error.Message}
		}
		if chunk.Usage != nil {
			resp.Usage = Usage{Input: chunk.Usage.PromptTokens, Output: chunk.Usage.CompletionTokens}
		}
		for _, ch := range chunk.Choices {
			d := ch.Delta
			if d.ReasoningContent != "" {
				reasoning.WriteString(d.ReasoningContent)
			}
			if d.Content != "" {
				content.WriteString(d.Content)
			}
			if d.Content != "" || d.ReasoningContent != "" {
				onDelta(Delta{Text: d.Content, Reasoning: d.ReasoningContent})
			}
			for _, tc := range d.ToolCalls {
				i := len(calls)
				if tc.Index != nil {
					i = *tc.Index
				}
				call := calls[i]
				if call == nil {
					call = &ToolCall{}
					calls[i] = call
				}
				if tc.ID != "" {
					call.ID = tc.ID
				}
				if tc.Function.Name != "" {
					call.Name = tc.Function.Name
				}
				call.Args += tc.Function.Arguments
			}
			if ch.FinishReason != nil && *ch.FinishReason != "" {
				finished = true
			}
		}
	}
	if err := sc.Err(); err != nil {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{}, fmt.Errorf("%w: %v", ErrStreamCut, err)
	}
	if !finished {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{}, ErrStreamCut
	}
	resp.Message = Message{Role: Assistant, Text: content.String(), Reasoning: reasoning.String()}
	idx := make([]int, 0, len(calls))
	for i := range calls {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for n, i := range idx {
		c := *calls[i]
		if c.ID == "" {
			c.ID = fmt.Sprintf("call_%d", n)
		}
		resp.Message.Calls = append(resp.Message.Calls, c)
	}
	return resp, nil
}

// Models lists the service's models, GET {BaseURL}/models in OpenAI's
// format ({"data": [{"id": …}]}), which the services speaking Chat
// Completions offer too; sorted by name, without repeats.
func (c *ChatCompletions) Models(ctx context.Context) ([]string, error) {
	url := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/") + "/models"
	hr, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Accept", "application/json")
	if c.APIKey != "" {
		hr.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(hr)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("cannot reach the model service: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, apiError(res)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("the model list is not in the expected format: %w", err)
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range body.Data {
		id := strings.TrimSpace(m.ID)
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}
