package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChatToolReasoningEffort(t *testing.T) {
	tools := []ToolSpec{{Name: "read", Parameters: json.RawMessage(`{"type":"object"}`)}}
	for _, tt := range []struct {
		name, model string
		tools       []ToolSpec
		wantNone    bool
	}{
		{"sol with tools", "gpt-6.2-sol", tools, true},
		{"astra with tools", "gpt-6.2-astra", tools, false},
		{"sol without tools", "gpt-6.2-sol", nil, false},
		{"another model with tools", "other", tools, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				effort, present := body["reasoning_effort"]
				if tt.wantNone && string(effort) != `"none"` {
					http.Error(w, "Function tools require reasoning_effort=none for gpt-6.2-sol in chat completions", http.StatusBadRequest)
					return
				}
				if !tt.wantNone && present {
					t.Errorf("unexpected reasoning_effort: %s", effort)
				}
				if len(tt.tools) > 0 {
					var sent []chatTool
					if err := json.Unmarshal(body["tools"], &sent); err != nil || len(sent) != 1 || sent[0].Function.Name != "read" {
						t.Errorf("unexpected tools: %s", body["tools"])
					}
					sse(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"read","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
					return
				}
				sse(w, `{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
			}))
			defer srv.Close()
			provider := (Settings{BaseURL: srv.URL + "/v1", Model: tt.model, Protocol: ProtocolChat}).Provider()
			resp, err := provider.Complete(context.Background(), Request{Tools: tt.tools, Messages: []Message{{Role: User, Text: "read"}}}, func(Delta) {})
			if err != nil {
				t.Fatal(err)
			}
			if len(tt.tools) > 0 && (len(resp.Message.Calls) != 1 || resp.Message.Calls[0].ID != "call-1") {
				t.Fatalf("missing streamed tool call: %+v", resp.Message)
			}
		})
	}
}
