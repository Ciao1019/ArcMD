package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/app"
)

// A question asked in the terminal searches, reads and answers, and the
// answer's citations are checked against the documents.
func TestAsk(t *testing.T) {
	dir := t.TempDir()
	spaceDir := filepath.Join(dir, "工作")
	os.MkdirAll(spaceDir, 0o755)
	os.WriteFile(filepath.Join(spaceDir, "评审.md"), []byte("# 评审纪要\n\n决定延期发布，原因是支付联调未完成。\n"), 0o644)
	var step atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []map[string]any `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")
		switch step.Add(1) {
		case 1:
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"grep","arguments":"{\"pattern\":\"延期\"}"}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
		case 2:
			last := req.Messages[len(req.Messages)-1]
			if !strings.Contains(fmt.Sprint(last["content"]), "评审.md:3:决定延期发布") {
				t.Errorf("the grep result did not reach the model: %v", last)
			}
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"因为支付联调 [评审.md#L3]，另见 [不存在.md#L1]。"},"finish_reason":"stop"}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	t.Setenv("ARCMD_AI_KEY", "")
	var out, errOut bytes.Buffer
	cfg := app.Config{ConfigDir: filepath.Join(dir, "config")}
	code := ask([]string{"-space", spaceDir, "-base-url", "http://127.0.0.1:" + strings.Split(srv.URL, ":")[2], "-model", "m", "为什么延期？"}, cfg, &out, &errOut)
	if code != 3 {
		t.Fatalf("exit %d, stderr:\n%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "因为支付联调 [评审.md#L3]") {
		t.Fatalf("answer: %q", out.String())
	}
	for _, want := range []string{`→ grep {"pattern":"延期"}`, "← 1 matches in 1 documents.", "1 tool calls, 2 model steps", "✗ [不存在.md#L1]"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr lacks %q:\n%s", want, errOut.String())
		}
	}
	if strings.Contains(errOut.String(), "✗ [评审.md") {
		t.Errorf("a good citation was refused:\n%s", errOut.String())
	}
	errOut.Reset()
	if code := ask([]string{"-space", spaceDir, "q"}, cfg, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "cannot ask") {
		t.Fatalf("without settings: %d %s", code, errOut.String())
	}
}

func TestAskProtocolOverrides(t *testing.T) {
	for _, tc := range []struct{ name, env, flag, path string }{
		{"saved", "", "", "/responses"},
		{"environment", "chat-completions", "", "/chat/completions"},
		{"flag", "chat-completions", "responses", "/responses"},
		{"automatic", "responses", "auto", "/chat/completions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "prefs.json"), []byte(`{"version":1,"ai":{"protocol":"responses"}}`), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("ARCMD_AI_PROTOCOL", tc.env)
			t.Setenv("ARCMD_AI_KEY", "")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					t.Errorf("got %s, want %s", r.URL.Path, tc.path)
				}
				if r.URL.Path == "/responses" {
					fmt.Fprintln(w, `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}}`)
				} else {
					fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
				}
			}))
			defer server.Close()
			args := []string{"-space", dir, "-base-url", server.URL, "-model", "gpt-6.2-sol"}
			if tc.flag != "" {
				args = append(args, "-protocol", tc.flag)
			}
			var stdout, stderr bytes.Buffer
			if exit := ask(append(args, "q"), app.Config{ConfigDir: dir}, &stdout, &stderr); exit != 0 || strings.TrimSpace(stdout.String()) != "ok" {
				t.Fatalf("exit %d: %s / %s", exit, &stdout, &stderr)
			}
		})
	}
}
