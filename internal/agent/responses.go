package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Responses shares authentication, transport and model listing with Chat Completions.
type Responses struct{ ChatCompletions }

type responseReplay struct {
	base, model string
	items       []json.RawMessage
}

type responseTool struct {
	Strict      bool            `json:"strict"` // Preserve optional parameters from the tool schema.
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type responseRequest struct {
	Model        string            `json:"model"`
	Instructions string            `json:"instructions,omitempty"`
	Input        []any             `json:"input"`
	Tools        []responseTool    `json:"tools,omitempty"`
	Stream       bool              `json:"stream"`
	Store        bool              `json:"store"`
	Include      []string          `json:"include"`
	Reasoning    map[string]string `json:"reasoning,omitempty"`
}

func (c *Responses) request(req Request) responseRequest {
	out := responseRequest{Model: c.Model, Instructions: req.System, Input: make([]any, 0, len(req.Messages)), Stream: true, Store: false, Include: []string{"reasoning.encrypted_content"}}
	if responsesReasoner(c.Model) {
		out.Reasoning = map[string]string{"effort": "medium", "summary": "auto"}
	}
	for _, m := range req.Messages {
		switch m.Role {
		case ToolRole:
			out.Input = append(out.Input, map[string]string{"type": "function_call_output", "call_id": m.CallID, "output": m.Text})
		case Assistant:
			if prev := m.responses; prev != nil && prev.base == c.BaseURL && prev.model == c.Model {
				for _, item := range prev.items {
					out.Input = append(out.Input, item)
				}
				continue
			}
			if m.Text != "" || len(m.Calls) == 0 {
				out.Input = append(out.Input, map[string]string{"role": "assistant", "content": m.Text})
			}
			for _, call := range m.Calls {
				out.Input = append(out.Input, map[string]string{"type": "function_call", "call_id": call.ID, "name": call.Name, "arguments": call.Args})
			}
		case User:
			out.Input = append(out.Input, map[string]string{"role": "user", "content": m.Text})
		}
	}
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, responseTool{Type: "function", Name: t.Name, Description: t.Description, Parameters: t.Parameters})
	}
	return out
}

func (c *Responses) Complete(ctx context.Context, req Request, onDelta func(Delta)) (Response, error) {
	return c.postStream(ctx, "/responses", c.request(req), onDelta, c.readStream)
}

type responseObject struct {
	Status string
	Output []json.RawMessage
	Usage  struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	}
	Error             *struct{ Message string }
	IncompleteDetails *struct{ Reason string } `json:"incomplete_details"`
}

// completed is authoritative for complete tool arguments, multiple parallel calls,
// and the ordered reasoning items that must accompany their outputs on the next step.
func (c *Responses) completed(r responseObject) (Response, error) {
	if r.Status != "" && r.Status != "completed" {
		return Response{}, fmt.Errorf("model response: %s", r.Status)
	}
	replay := &responseReplay{base: c.BaseURL, model: c.Model}
	msg := Message{Role: Assistant}
	var text, reasoning strings.Builder
	for _, raw := range r.Output {
		var item struct {
			Type, Name, Arguments string
			CallID                string `json:"call_id"`
			Content               []struct{ Type, Text, Refusal string }
			Summary               []struct{ Text string }
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return Response{}, err
		}
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				switch part.Type {
				case "output_text":
					text.WriteString(part.Text)
				case "refusal":
					text.WriteString(part.Refusal)
				}
			}
		case "reasoning":
			for _, part := range item.Summary {
				reasoning.WriteString(part.Text)
			}
		case "function_call":
			if item.CallID == "" || item.Name == "" {
				return Response{}, errors.New("model returned a function call without a call_id or name")
			}
			msg.Calls = append(msg.Calls, ToolCall{ID: item.CallID, Name: item.Name, Args: item.Arguments})
		default:
			continue
		}
		replay.items = append(replay.items, raw)
	}
	msg.Text, msg.Reasoning = text.String(), reasoning.String()
	if len(replay.items) > 0 {
		msg.responses = replay
	}
	return Response{Message: msg, Usage: Usage{Input: r.Usage.InputTokens, Output: r.Usage.OutputTokens}}, nil
}

func (c *Responses) readStream(ctx context.Context, r io.Reader, onDelta func(Delta)) (Response, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	var text, reasoning strings.Builder
	// Some compatible endpoints finish with a compact response object. The
	// item-done events still contain the complete tool calls and reasoning.
	done := make(map[int]json.RawMessage)
	sawCall := false
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return Response{}, err
		}
		data, ok := strings.CutPrefix(sc.Text(), "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			break
		}
		var e struct {
			OutputIndex          int `json:"output_index"`
			Item                 json.RawMessage
			Type, Delta, Message string
			Response             responseObject
		}
		if err := json.Unmarshal([]byte(data), &e); err != nil {
			return Response{}, fmt.Errorf("invalid Responses event: %w", err)
		}
		switch e.Type {
		case "response.output_text.delta", "response.refusal.delta":
			text.WriteString(e.Delta)
			onDelta(Delta{Text: e.Delta})
		case "response.reasoning_summary_text.delta":
			reasoning.WriteString(e.Delta)
			onDelta(Delta{Reasoning: e.Delta})
		case "response.function_call_arguments.delta", "response.function_call_arguments.done":
			sawCall = true
		case "response.output_item.done":
			done[e.OutputIndex] = e.Item
		case "response.completed":
			if e.Response.Output == nil && len(done) > 0 {
				indices := make([]int, 0, len(done))
				for index := range done {
					indices = append(indices, index)
				}
				sort.Ints(indices)
				for _, index := range indices {
					e.Response.Output = append(e.Response.Output, done[index])
				}
			}
			resp, err := c.completed(e.Response)
			if err != nil {
				return Response{}, err
			}
			if sawCall && len(resp.Message.Calls) == 0 {
				return Response{}, ErrStreamCut
			}
			if resp.Message.Text == "" {
				resp.Message.Text = text.String()
			}
			if resp.Message.Reasoning == "" {
				resp.Message.Reasoning = reasoning.String()
			}
			if text.Len() == 0 && resp.Message.Text != "" {
				onDelta(Delta{Text: resp.Message.Text})
			}
			if reasoning.Len() == 0 && resp.Message.Reasoning != "" {
				onDelta(Delta{Reasoning: resp.Message.Reasoning})
			}
			return resp, nil
		case "error", "response.failed", "response.incomplete":
			reason := e.Message
			if e.Response.Error != nil {
				reason = e.Response.Error.Message
			}
			if e.Response.IncompleteDetails != nil {
				reason = e.Response.IncompleteDetails.Reason
			}
			return Response{}, fmt.Errorf("model service: %s: %s", e.Type, reason)
		}
	}
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	if err := sc.Err(); err != nil {
		return Response{}, fmt.Errorf("%w: %v", ErrStreamCut, err)
	}
	return Response{}, ErrStreamCut
}
