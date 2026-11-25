package agentmcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatGuideTopics(t *testing.T) {
	all := FormatGuide("all")
	for _, needle := range []string{
		"data-mark=",
		"data-highlight=",
		"arcmd-columns",
		"<colgroup>",
		".excalidraw.svg",
		"arcmd-db:view",
	} {
		if !strings.Contains(all, needle) {
			t.Fatalf("FormatGuide(all) missing %q", needle)
		}
	}
	for _, topic := range FormatTopics {
		if got := FormatGuide(topic); len(got) < 120 {
			t.Fatalf("FormatGuide(%q) unexpectedly short: %d", topic, len(got))
		}
	}
}

func TestInstallSkillsAndMCPConfig(t *testing.T) {
	home := t.TempDir()
	// Fallback when no agent folders exist yet.
	paths, err := InstallSkills(home, "/usr/local/bin/arcmd")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || !strings.HasSuffix(paths[0], filepath.Join(".agents", "skills", "arcmd", "SKILL.md")) {
		t.Fatalf("unexpected fallback paths: %v", paths)
	}
	raw, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "name: arcmd") || !strings.Contains(string(raw), "/usr/local/bin/arcmd") {
		t.Fatalf("unexpected SKILL.md content: %s", raw)
	}

	// Detected Claude Code & Antigravity folders.
	home2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home2, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home2, ".gemini", "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	paths2, err := InstallSkills(home2, "arcmd")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths2) != 2 {
		t.Fatalf("expected 2 detected skill paths, got %v", paths2)
	}

	cfgJSON := MCPConfigJSON("/Applications/ArcMD.app/Contents/MacOS/arcmd")
	var parsed map[string]any
	if err := json.Unmarshal([]byte(cfgJSON), &parsed); err != nil {
		t.Fatalf("invalid MCPConfigJSON: %v\n%s", err, cfgJSON)
	}
}

func TestMCPServerLineAndFramedTransport(t *testing.T) {
	caller := func(_ context.Context, action string, params json.RawMessage) (any, bool, error) {
		if action == "space.list" {
			return map[string]any{"spaces": []string{"Personal"}}, true, nil
		}
		return nil, false, fmt.Errorf("boom: %s", action)
	}

	// 1. Newline-delimited JSON-RPC (standard MCP stdio).
	reqs := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"arcmd_format_guide","arguments":{"topic":"inline"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"space_list","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"resources/read","params":{"uri":"arcmd://skill"}}`,
	}, "\n") + "\n"

	var out bytes.Buffer
	if err := Serve(context.Background(), strings.NewReader(reqs), &out, "0.3.0", caller); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 replies (notification skipped), got %d:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[1], `"whiteboard_write"`) || !strings.Contains(lines[1], `"database_records"`) {
		t.Fatalf("tools/list missing expected tools: %s", lines[1])
	}
	if !strings.Contains(lines[2], `data-mark`) {
		t.Fatalf("arcmd_format_guide reply missing data-mark: %s", lines[2])
	}
	if !strings.Contains(lines[3], `\"liveWindow\": true`) {
		t.Fatalf("space_list reply missing liveWindow: %s", lines[3])
	}

	// 2. Content-Length header-framed JSON-RPC.
	pingBody := `{"jsonrpc":"2.0","id":99,"method":"ping"}`
	framedIn := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(pingBody), pingBody)
	var framedOut bytes.Buffer
	if err := Serve(context.Background(), strings.NewReader(framedIn), &framedOut, "0.3.0", caller); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(framedOut.String(), "Content-Length:") || !strings.Contains(framedOut.String(), `"id":99`) {
		t.Fatalf("unexpected framed reply: %q", framedOut.String())
	}
}

func TestResolveBinaryPathFallback(t *testing.T) {
	if got := ResolveBinaryCommand(); got != "arcmd" {
		t.Fatalf("expected ResolveBinaryCommand() under go test to fall back to 'arcmd', got %q", got)
	}
	cases := []struct {
		exe  string
		want string
	}{
		{"", "arcmd"},
		{"/var/folders/xx/go-build123456/b001/exe/arcmd", "arcmd"},
		{`C:\Users\dev\AppData\Local\Temp\go-build999\b001\exe\arcmd.exe`, "arcmd"},
		{"/private/var/folders/xx/AppTranslocation/ABCD/d/ArcMD.app/Contents/MacOS/arcmd", "arcmd"},
		{"/tmp/arcmd.test", "arcmd"},
		{"/usr/local/bin/arcmd", "/usr/local/bin/arcmd"},
		{"/Applications/ArcMD.app/Contents/MacOS/arcmd", "/Applications/ArcMD.app/Contents/MacOS/arcmd"},
	}
	for _, tc := range cases {
		if got := resolveBinaryPath(tc.exe); got != tc.want {
			t.Errorf("resolveBinaryPath(%q) = %q, want %q", tc.exe, got, tc.want)
		}
	}
}

type repeatByteReader struct {
	b   byte
	rem int64
}

func (r *repeatByteReader) Read(p []byte) (int, error) {
	if r.rem <= 0 {
		return 0, io.EOF
	}
	n := len(p)
	if int64(n) > r.rem {
		n = int(r.rem)
	}
	for i := 0; i < n; i++ {
		p[i] = r.b
	}
	r.rem -= int64(n)
	return n, nil
}

func TestReadRPCFrameRejectsOversizedNewlineLine(t *testing.T) {
	stream := io.MultiReader(
		strings.NewReader(`{"jsonrpc":"2.0","method":"`),
		&repeatByteReader{b: 'a', rem: maxRPCFrameBytes + 4096},
		strings.NewReader("\"}\n"),
	)
	_, framed, err := readRPCFrame(bufio.NewReaderSize(stream, 64*1024))
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("expected oversized line error, got framed=%v err=%v", framed, err)
	}
}
