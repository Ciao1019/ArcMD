package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/app"
)

func runTestCLI(t *testing.T, cfg app.Config, stdin string, args ...string) (string, string, int) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code, handled := dispatchCLI(args, cfg, strings.NewReader(stdin), &out, &errBuf)
	if !handled {
		t.Fatalf("expected CLI subcommand %v to be handled", args)
	}
	return out.String(), errBuf.String(), code
}

func TestLocalAgentCLISubcommandsAndMCP(t *testing.T) {
	base := t.TempDir()
	cfg := app.Config{
		ConfigDir:    filepath.Join(base, "config"),
		DocumentsDir: filepath.Join(base, "spaces"),
	}

	// 1. Status & Space CLI.
	out, errStr, code := runTestCLI(t, cfg, "", "status", "-json")
	if code != 0 {
		t.Fatalf("status failed (%d): %s", code, errStr)
	}
	if !strings.Contains(out, `"Personal"`) {
		t.Fatalf("expected default Personal space in status: %s", out)
	}

	_, errStr, code = runTestCLI(t, cfg, "", "space", "create", "工程笔记", "-icon", "⚡", "-color", "#245BDB", "-switch")
	if code != 0 {
		t.Fatalf("space create failed (%d): %s", code, errStr)
	}

	// 2. Doc create with stdin + flexible flag ordering after positional path.
	richBody := "# 迭代周报\n\n> [!IMPORTANT]\n> 核心目标：<mark>零延迟体验</mark> 与 <span style=\"border-bottom:1.5px solid #D83931\" data-mark=\"box\" data-mark-color=\"red\">高可靠持久化</span>。\n\n## 进展\n\n- 初版完成\n"
	_, errStr, code = runTestCLI(t, cfg, richBody, "doc", "create", "Weekly/周报.md", "-space", "工程笔记", "-icon", "📅", "-stdin")
	if code != 0 {
		t.Fatalf("doc create failed (%d): %s", code, errStr)
	}

	_, errStr, code = runTestCLI(t, cfg, "- 接入 MCP 与 CLI", "doc", "append", "Weekly/周报.md", "-space", "工程笔记", "-heading", "## 进展", "-stdin")
	if code != 0 {
		t.Fatalf("doc append failed (%d): %s", code, errStr)
	}

	readOut, errStr, code := runTestCLI(t, cfg, "", "doc", "read", "Weekly/周报.md", "-space", "工程笔记", "-lines")
	if code != 0 {
		t.Fatalf("doc read failed (%d): %s", code, errStr)
	}
	if !strings.Contains(readOut, "L1: ---") || !strings.Contains(readOut, "接入 MCP 与 CLI") {
		t.Fatalf("unexpected doc read output:\n%s", readOut)
	}

	searchOut, errStr, code := runTestCLI(t, cfg, "", "doc", "search", "零延迟体验", "-space", "工程笔记")
	if code != 0 || !strings.Contains(searchOut, "Weekly/周报.md") {
		t.Fatalf("doc search failed (%d): out=%q err=%q", code, searchOut, errStr)
	}

	// 3. Whiteboard CLI (-mermaid + -embed).
	mermaidSrc := "flowchart LR\n  U[Agent] --> A[ArcMD]"
	_, errStr, code = runTestCLI(t, cfg, mermaidSrc, "board", "write", "-space", "工程笔记", "-path", "Weekly/assets/flow.excalidraw.svg", "-embed", "Weekly/周报.md", "-mermaid")
	if code != 0 {
		t.Fatalf("board write failed (%d): %s", code, errStr)
	}

	// 4. Database CLI (create, add-record, query).
	dbSpec := `{"title":"版本排期","properties":[{"name":"状态","type":"status"},{"name":"进度","type":"number"}]}`
	_, errStr, code = runTestCLI(t, cfg, dbSpec, "db", "create", "排期表.db.md", "-space", "工程笔记", "-spec-stdin")
	if code != 0 {
		t.Fatalf("db create failed (%d): %s", code, errStr)
	}
	_, errStr, code = runTestCLI(t, cfg, "", "db", "add-record", "-space", "工程笔记", "-db", "排期表.db.md", "-title", "v0.4 发布", "-fields", `{"状态":"进行中","进度":80}`)
	if code != 0 {
		t.Fatalf("db add-record failed (%d): %s", code, errStr)
	}
	qOut, errStr, code := runTestCLI(t, cfg, "", "db", "query", "排期表.db.md", "-space", "工程笔记")
	if code != 0 || !strings.Contains(qOut, "v0.4 发布") {
		t.Fatalf("db query failed (%d): out=%s err=%s", code, qOut, errStr)
	}

	// 5. Skill install to custom directory & format-guide.
	skillDir := filepath.Join(base, "custom-skills")
	_, errStr, code = runTestCLI(t, cfg, "", "skill", "install", "-dir", skillDir)
	if code != 0 {
		t.Fatalf("skill install failed (%d): %s", code, errStr)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "arcmd", "SKILL.md")); err != nil {
		t.Fatalf("expected installed SKILL.md: %v", err)
	}
	fgOut, _, code := runTestCLI(t, cfg, "", "format-guide", "tables")
	if code != 0 || !strings.Contains(fgOut, "<colgroup>") {
		t.Fatalf("unexpected format-guide tables output: %s", fgOut)
	}

	// 6. MCP stdio end-to-end via CLI.
	mcpIn := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"arcmd_status","arguments":{}}}`,
	}, "\n") + "\n"
	mcpOut, errStr, code := runTestCLI(t, cfg, mcpIn, "mcp")
	if code != 0 {
		t.Fatalf("mcp command failed (%d): %s", code, errStr)
	}
	var lines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(mcpOut), "\n") {
		var obj map[string]any
		if err := json.Unmarshal([]byte(l), &obj); err != nil {
			t.Fatalf("invalid MCP JSON line %q: %v", l, err)
		}
		lines = append(lines, obj)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 MCP JSON-RPC responses, got %d", len(lines))
	}
}

func TestDBUpdatePropAndUpdateViewWithoutTypePreserveExistingKind(t *testing.T) {
	base := t.TempDir()
	cfg := app.Config{
		ConfigDir:    filepath.Join(base, "config"),
		DocumentsDir: filepath.Join(base, "spaces"),
	}

	dbSpec := `{
		"title": "研发看板",
		"properties": [
			{"name": "状态", "type": "status"},
			{"name": "模块", "type": "select", "options": [{"name": "核心", "color": "blue"}]}
		],
		"views": [
			{"name": "总表", "type": "table"},
			{"name": "迭代看板", "type": "board", "groupBy": "状态"}
		]
	}`
	if _, errStr, code := runTestCLI(t, cfg, dbSpec, "db", "create", "研发看板.db.md", "-spec-stdin"); code != 0 {
		t.Fatalf("db create failed (%d): %s", code, errStr)
	}

	// Update property via positional dbPath WITHOUT passing -type: must stay "status" (not reset to "text").
	if _, errStr, code := runTestCLI(t, cfg, "", "db", "update-prop", "研发看板.db.md", "-name", "状态", "-prop", `{"description":"任务流转状态","width":180}`); code != 0 {
		t.Fatalf("db update-prop failed (%d): %s", code, errStr)
	}

	// Update view via positional dbPath WITHOUT passing -type: must stay "board" (not reset to "table").
	if _, errStr, code := runTestCLI(t, cfg, "", "db", "update-view", "研发看板.db.md", "-view", "迭代看板", "-group-by", "模块", "-spec", `{"icon":"kanban"}`); code != 0 {
		t.Fatalf("db update-view failed (%d): %s", code, errStr)
	}

	inspectOut, errStr, code := runTestCLI(t, cfg, "", "db", "inspect", "研发看板.db.md", "-json")
	if code != 0 {
		t.Fatalf("db inspect failed (%d): %s", code, errStr)
	}
	var env struct {
		Result struct {
			Properties []struct {
				Name        string  `json:"name"`
				Type        string  `json:"type"`
				Description string  `json:"description"`
				Width       float64 `json:"width"`
			} `json:"properties"`
			Views []struct {
				Name string `json:"name"`
				Type string `json:"type"`
				Icon string `json:"icon"`
			} `json:"views"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(inspectOut), &env); err != nil {
		t.Fatalf("unmarshal inspect output: %v\n%s", err, inspectOut)
	}
	inspected := env.Result

	foundStatus := false
	for _, pr := range inspected.Properties {
		if pr.Name == "状态" {
			foundStatus = true
			if pr.Type != "status" || pr.Description != "任务流转状态" {
				t.Fatalf("expected updated property '状态' to preserve type 'status' and description, got %+v", pr)
			}
		}
	}
	if !foundStatus {
		t.Fatalf("expected property '状态' in %+v", inspected.Properties)
	}

	foundBoard := false
	for _, vw := range inspected.Views {
		if vw.Name == "迭代看板" {
			foundBoard = true
			if vw.Type != "board" || vw.Icon != "kanban" {
				t.Fatalf("expected updated view '迭代看板' to preserve type 'board' and icon 'kanban', got %+v", vw)
			}
		}
	}
	if !foundBoard {
		t.Fatalf("expected view '迭代看板' in %+v", inspected.Views)
	}
}
