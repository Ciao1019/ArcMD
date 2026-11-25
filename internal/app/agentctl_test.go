package app

import (
	"context"
	"encoding/json"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func testControlConfig(t *testing.T) Config {
	t.Helper()
	base := t.TempDir()
	return Config{
		ConfigDir:    filepath.Join(base, "config"),
		DocumentsDir: filepath.Join(base, "spaces"),
	}
}

func mustControl(t *testing.T, cfg Config, action string, params any) (map[string]any, bool) {
	t.Helper()
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		raw = b
	}
	res, live, err := ExecuteControl(context.Background(), cfg, ControlRequest{
		Action: action,
		Params: raw,
	})
	if err != nil {
		t.Fatalf("ExecuteControl(%s) failed: %v", action, err)
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out, live
}

func TestAgentControlHeadlessSpacesRichDocsWhiteboardAndDatabase(t *testing.T) {
	cfg := testControlConfig(t)

	// 1. Space lifecycle: create, update theme/attachments, switch, remove (preserving folder on disk).
	spRes, live := mustControl(t, cfg, "space.create", map[string]any{
		"name":       "研发空间",
		"icon":       "🛠️",
		"colors":     []string{"#245BDB", "#34E895"},
		"appearance": "dark",
		"switch":     true,
	})
	if live {
		t.Fatal("expected headless mode when no GUI IPC is active")
	}
	if spRes["name"] != "研发空间" || spRes["active"] != true {
		t.Fatalf("unexpected created space: %+v", spRes)
	}
	workRoot := spRes["root"].(string)

	updRes, _ := mustControl(t, cfg, "space.update", map[string]any{
		"space":          "研发空间",
		"attachmentsDir": "附件",
	})
	if updRes["attachmentsDir"] != "附件" {
		t.Fatalf("expected attachmentsDir='附件', got %+v", updRes)
	}

	tempSp, _ := mustControl(t, cfg, "space.create", map[string]any{
		"name":    "临时空间",
		"starter": true,
		"switch":  false,
	})
	tempRoot := tempSp["root"].(string)
	mustControl(t, cfg, "space.remove", map[string]any{"space": "临时空间"})
	if _, err := os.Stat(tempRoot); err != nil {
		t.Fatalf("removing a Space must keep its physical folder on disk: %v", err)
	}

	// 2. Rich Markdown document creation & lossless native model verification.
	richMD := strings.Join([]string{
		"# 核心设计方案",
		"",
		"包含 <mark>默认黄底高亮</mark>、<span style=\"color:#D83931;background-color:#D8E4FF\" data-color=\"red\" data-highlight=\"light-blue\">红字浅蓝高亮</span>、手绘圈出 <span style=\"border-bottom:1.5px solid #D83931\" data-mark=\"circle\" data-mark-color=\"red\">关键指标</span>、手绘波浪线 <span style=\"text-decoration:underline wavy #245BDB\" data-mark=\"wavy\" data-mark-color=\"blue\">风险项</span>、快捷键 <kbd>⌘K</kbd>、上标 x<sup>2</sup>、公式 $E = mc^2$、双链 [[关联笔记]] 与 #架构/2026 标签。",
		"",
		"> [!TIP]",
		"> <span data-emoji=\"🚀\"></span>这是带自定义图标的绿色高亮块。",
		"",
		"<details open>",
		"<summary>展开查看性能指标</summary>",
		"",
		"- 启动耗时：`8 ms`",
		"",
		"</details>",
		"",
		"<div class=\"arcmd-columns\" data-widths=\"60,40\" style=\"display:flex;gap:24px\">",
		"",
		"<div class=\"arcmd-column\" style=\"flex:60;min-width:0\">",
		"",
		"### 左栏说明",
		"",
		"左栏正文内容。",
		"",
		"</div>",
		"",
		"<div class=\"arcmd-column\" style=\"flex:40;min-width:0\">",
		"",
		"### 右栏说明",
		"",
		"右栏正文内容。",
		"",
		"</div>",
		"",
		"</div>",
		"",
		"<table>",
		"<colgroup><col width=\"200\"><col width=\"260\"></colgroup>",
		"<thead>",
		"<tr><th style=\"background-color:#F0F4FF\" data-bg=\"light-blue\">模块</th><th style=\"background-color:#F0F4FF\" data-bg=\"light-blue\">状态</th></tr>",
		"</thead>",
		"<tbody>",
		"<tr><td colspan=\"2\" align=\"center\" style=\"background-color:#F0FBEF\" data-bg=\"light-green\"><strong>全部就绪</strong></td></tr>",
		"</tbody>",
		"</table>",
		"",
		"$$",
		"\\sum_{i=1}^{n} x_i = S",
		"$$",
		"",
		"```mermaid",
		"flowchart LR",
		"  A[Agent] --> B[ArcMD]",
		"```",
		"",
		"## 待办",
		"",
		"- [ ] 第一项",
		"",
		"## 附录",
		"",
		"尾部说明。",
	}, "\n")

	mustControl(t, cfg, "doc.create", map[string]any{
		"space":       "研发空间",
		"path":        "Specs/核心设计.md",
		"icon":        "✨",
		"cover":       "gradient:dawn",
		"goal":        1200,
		"frontmatter": map[string]any{"owner": "team-core"},
		"content":     richMD,
		"pin":         true,
	})
	mustControl(t, cfg, "doc.create", map[string]any{
		"space":   "研发空间",
		"path":    "关联笔记.md",
		"content": "# 关联笔记\n\n引用回 [[核心设计]]。\n",
	})

	// Edit under specific heading ("## 待办") & patch text.
	mustControl(t, cfg, "doc.edit", map[string]any{
		"space":   "研发空间",
		"path":    "Specs/核心设计.md",
		"mode":    "append",
		"heading": "## 待办",
		"content": "- [x] 新增手绘荧光笔 <span style=\"background-color:#CCF3C8\" data-mark=\"highlight\" data-mark-color=\"green\">已完成</span>",
	})
	mustControl(t, cfg, "doc.edit", map[string]any{
		"space":   "研发空间",
		"path":    "Specs/核心设计.md",
		"mode":    "patch",
		"oldText": "尾部说明。",
		"newText": "更新后的尾部说明。",
	})

	savedMD, err := vault.Read(workRoot, "Specs/核心设计.md")
	if err != nil {
		t.Fatal(err)
	}
	parsedDoc := editor.ImportMarkdown(savedMD)
	if parsedDoc.Icon != "✨" || parsedDoc.Cover != "gradient:dawn" || parsedDoc.Goal != 1200 {
		t.Fatalf("unexpected page metadata: icon=%q cover=%q goal=%d", parsedDoc.Icon, parsedDoc.Cover, parsedDoc.Goal)
	}
	if !strings.Contains(parsedDoc.Frontmatter, "owner:") {
		t.Fatalf("expected custom frontmatter 'owner', got %q", parsedDoc.Frontmatter)
	}

	var sawHighlight, sawCircle, sawWavy, sawKbd, sawCallout, sawToggle, sawColumns, sawRichTable, sawMath, sawMermaid bool
	for _, b := range parsedDoc.Blocks {
		for _, r := range b.Runs {
			if r.Style.Highlight == "light-yellow" {
				sawHighlight = true
			}
			if r.Style.Annotation == "circle" && r.Style.AnnotationColor == "red" {
				sawCircle = true
			}
			if r.Style.Annotation == "wavy" && r.Style.AnnotationColor == "blue" {
				sawWavy = true
			}
			if r.Style.Kbd {
				sawKbd = true
			}
		}
		switch b.Kind {
		case "callout":
			if b.Color == "green" && b.Emoji == "🚀" {
				sawCallout = true
			}
		case "toggle":
			sawToggle = true
		case "columns":
			if len(b.Children) == 2 && len(b.Widths) == 2 {
				sawColumns = true
			}
		case "table":
			if len(b.Widths) == 2 && len(b.Children) == 2 && b.Children[1].Children[0].ColSpan == 2 && b.Children[1].Children[0].Background == "light-green" {
				sawRichTable = true
			}
		case "math":
			sawMath = true
		case "code":
			if b.Language == "mermaid" {
				sawMermaid = true
			}
		}
	}
	if !sawHighlight || !sawCircle || !sawWavy || !sawKbd || !sawCallout || !sawToggle || !sawColumns || !sawRichTable || !sawMath || !sawMermaid {
		t.Fatalf("missing rich construct after ImportMarkdown: highlight=%v circle=%v wavy=%v kbd=%v callout=%v toggle=%v columns=%v table=%v math=%v mermaid=%v",
			sawHighlight, sawCircle, sawWavy, sawKbd, sawCallout, sawToggle, sawColumns, sawRichTable, sawMath, sawMermaid)
	}

	// 3. Rename document -> verify [[核心设计]] in 关联笔记.md rewrites automatically!
	mustControl(t, cfg, "doc.organize", map[string]any{
		"space":   "研发空间",
		"op":      "rename",
		"path":    "Specs/核心设计.md",
		"newName": "架构总览",
	})
	linkedMD, err := vault.Read(workRoot, "关联笔记.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(linkedMD, "[[架构总览]]") {
		t.Fatalf("expected wikilink rewrite to [[架构总览]], got %q", linkedMD)
	}

	// 4. Whiteboard: Mermaid + declarative bound elements + embed into Markdown doc.
	wbRes, _ := mustControl(t, cfg, "whiteboard.write", map[string]any{
		"space":        "研发空间",
		"path":         "附件/arch-flow.excalidraw.svg",
		"mermaid":      "flowchart LR\n  A[Agent] --> B{IPC}\n  B --> C([ArcMD UI])",
		"embedInDoc":   "Specs/架构总览.md",
		"embedHeading": "## 附录",
		"caption":      "Agent 架构手绘图",
	})
	if wbRes["embeddedIn"] != "Specs/架构总览.md" {
		t.Fatalf("expected whiteboard embeddedIn Specs/架构总览.md, got %+v", wbRes)
	}
	// Append declarative nodes + bound arrow.
	mustControl(t, cfg, "whiteboard.write", map[string]any{
		"space":  "研发空间",
		"path":   "附件/arch-flow.excalidraw.svg",
		"append": true,
		"elements": []map[string]any{
			{"id": "store", "type": "cylinder", "text": "Vault 存储", "color": "green"},
			{"id": "note1", "type": "note", "text": "原子写盘保障"},
			{"type": "arrow", "from": "store", "to": "note1", "text": "说明"},
		},
	})
	svgRaw, err := vault.Read(workRoot, "附件/arch-flow.excalidraw.svg")
	if err != nil {
		t.Fatal(err)
	}
	scene, err := whiteboard.Decode(svgRaw)
	if err != nil {
		t.Fatalf("whiteboard.Decode failed: %v", err)
	}
	if scene.ElementByID("store") == nil || scene.ElementByID("note1") == nil {
		t.Fatalf("expected declarative elements in scene, got %d elements", len(scene.Elements))
	}
	updatedSpecMD, err := vault.Read(workRoot, "Specs/架构总览.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updatedSpecMD, "![Agent 架构手绘图](../附件/arch-flow.excalidraw.svg)") {
		t.Fatalf("expected relative whiteboard embed in Specs/架构总览.md, got:\n%s", updatedSpecMD)
	}

	// 5. Database (.db.md): create schema + Formula 2.0 + board view + records + Linked View embed.
	mustControl(t, cfg, "database.schema", map[string]any{
		"space":      "研发空间",
		"op":         "create",
		"path":       "任务库.db.md",
		"title":      "任务库",
		"icon":       "📋",
		"sourceType": "folder",
		"properties": []map[string]any{
			{"name": "状态", "type": "status"},
			{"name": "优先级", "type": "select", "options": []map[string]any{{"name": "P0", "color": "red"}, {"name": "P1", "color": "blue"}}},
			{"name": "进度", "type": "number", "number": map[string]any{"format": "percent", "showAs": "bar", "barColor": "green"}},
			{"name": "结论", "type": "formula", "formula": map[string]any{"expression": `if(prop("进度") >= 1, "已达成", "进行中")`}},
		},
		"views": []map[string]any{
			{"name": "总表", "type": "table"},
			{"name": "状态看板", "type": "board", "groupBy": "状态"},
		},
		"records": []map[string]any{
			{
				"title":  "实现本地控制层",
				"fields": map[string]any{"状态": "进行中", "优先级": "P0", "进度": 0.5},
				"body":   "# 实现本地控制层\n\n详细记录正文。",
			},
		},
	})

	mustControl(t, cfg, "database.records", map[string]any{
		"space":  "研发空间",
		"op":     "update",
		"path":   "任务库.db.md",
		"record": "实现本地控制层",
		"fields": map[string]any{"状态": "已完成", "进度": 1.0, "优先级": "P0"},
	})
	qOut, _ := mustControl(t, cfg, "database.records", map[string]any{
		"space": "研发空间",
		"op":    "query",
		"path":  "任务库.db.md",
		"view":  "状态看板",
	})
	recs := qOut["records"].([]any)
	if len(recs) != 1 {
		t.Fatalf("expected 1 database record, got %d", len(recs))
	}
	recMap := recs[0].(map[string]any)
	fieldsMap := recMap["fields"].(map[string]any)
	if fieldsMap["结论"] != "已达成" {
		t.Fatalf("expected Formula 2.0 '结论' == '已达成', got %+v", fieldsMap)
	}
	recPath := recMap["path"].(string)
	backingRaw, err := vault.Read(workRoot, recPath)
	if err != nil {
		t.Fatalf("expected backing record file %q: %v", recPath, err)
	}
	if !strings.Contains(backingRaw, "已完成") {
		t.Fatalf("expected frontmatter sync in backing record file, got:\n%s", backingRaw)
	}

	mustControl(t, cfg, "database.schema", map[string]any{
		"space":      "研发空间",
		"op":         "embed_view",
		"path":       "任务库.db.md",
		"view":       map[string]any{"name": "状态看板"},
		"embedInDoc": "Specs/架构总览.md",
		"height":     400,
	})
	specWithDB, _ := vault.Read(workRoot, "Specs/架构总览.md")
	if !strings.Contains(specWithDB, "<!-- arcmd-db:view") || !strings.Contains(specWithDB, "../任务库.db.md") {
		t.Fatalf("expected database Linked View embed in Specs/架构总览.md, got:\n%s", specWithDB)
	}
	dbDocCheck := editor.ImportMarkdown(specWithDB)
	foundDBBlock := false
	for _, b := range dbDocCheck.Blocks {
		if b.Kind == "raw" {
			if spec, _, ok := editor.ParseDatabaseEmbed(b.Text()); ok && spec.Target == "../任务库.db.md" {
				foundDBBlock = true
			}
		}
	}
	if !foundDBBlock {
		t.Fatalf("expected ImportMarkdown to parse embedded Linked View block")
	}

	// 6. Safe Trash & Restore via headless App with injected trash collector (no permanent delete).
	h, err := newHeadlessApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var trashedOSPaths []string
	trashSink := t.TempDir()
	h.trashFile = func(p string) error {
		trashedOSPaths = append(trashedOSPaths, p)
		return os.Rename(p, filepath.Join(trashSink, filepath.Base(p)))
	}
	delRaw, _ := json.Marshal(map[string]any{
		"space": "研发空间",
		"op":    "trash",
		"path":  "关联笔记.md",
	})
	delOut, err := h.handleControl(ControlRequest{Action: "doc.delete", Params: delRaw})
	if err != nil {
		t.Fatal(err)
	}
	trashID := delOut.(map[string]any)["trashID"].(string)
	if len(trashedOSPaths) != 1 || trashID == "" {
		t.Fatalf("expected safe trash + backup ID, got paths=%v id=%q", trashedOSPaths, trashID)
	}
	restRaw, _ := json.Marshal(map[string]any{
		"op":      "restore",
		"trashID": trashID,
	})
	if _, err := h.handleControl(ControlRequest{Action: "doc.delete", Params: restRaw}); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Read(workRoot, "关联笔记.md"); err != nil {
		t.Fatalf("expected restored document on disk: %v", err)
	}
}

func TestAgentControlLiveWindowIPCAndSettingsUI(t *testing.T) {
	cfg := testControlConfig(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	a.prefs.TourSeen = true
	a.tour = tour{}
	fakeHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fakeHome, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.agentHomeDir = fakeHome

	var uiMu sync.Mutex
	tt := ui.NewTester(a.View, 1200, 800)
	a.controlExec = func(fn func() (any, error)) (any, error) {
		uiMu.Lock()
		defer uiMu.Unlock()
		return fn()
	}
	if err := a.startAgentIPC(); err != nil {
		t.Fatal(err)
	}
	defer a.stopAgentIPC()

	// Edit the currently open Welcome.md over real HTTP loopback IPC!
	editOut, live := mustControl(t, cfg, "doc.edit", map[string]any{
		"path":    "Welcome.md",
		"mode":    "append",
		"content": "> [!NOTE]\n> <mark>来自本地 Agent 的实时编辑</mark>",
	})
	if !live {
		t.Fatal("expected ExecuteControl to route through live IPC server")
	}
	if editOut["path"] != "Welcome.md" {
		t.Fatalf("unexpected edit result: %+v", editOut)
	}
	uiMu.Lock()
	if a.doc == nil || a.doc.conflict {
		uiMu.Unlock()
		t.Fatalf("live edit must not trigger conflict banner: doc=%+v", a.doc)
	}
	if !a.doc.ed.Engine.CanUndo() {
		uiMu.Unlock()
		t.Fatal("expected live agent edit on open document to be undoable with ⌘Z")
	}
	uiMu.Unlock()

	// Create and switch to a new Space live over IPC.
	mustControl(t, cfg, "space.create", map[string]any{
		"name":   "设计评审",
		"icon":   "🎨",
		"switch": true,
	})
	uiMu.Lock()
	if got := a.state.ActiveSpace().Name; got != "设计评审" {
		uiMu.Unlock()
		t.Fatalf("expected live active space '设计评审', got %q", got)
	}
	// Open Settings › AI and test one-click Skill install + MCP config copy.
	a.openSettings(tabAI)
	uiMu.Unlock()

	tt.SetSize(1200, 820)
	tt.Frame()
	if r, ok := tt.Find("Base URL"); ok {
		tt.Scroll(r.X+r.W/2, r.Y+80, 0, 520)
	}
	tt.Frame()
	if !tt.HasText("本地 Agent 接入") {
		t.Fatal("expected '本地 Agent 接入' section in Settings › AI")
	}
	if err := tt.Click("一键安装 Skill"); err != nil {
		t.Fatalf("click Install Skill: %v", err)
	}
	skillPath := filepath.Join(fakeHome, ".claude", "skills", "arcmd", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("expected installed SKILL.md at %s: %v", skillPath, err)
	}
	if err := tt.Click("复制 MCP 配置"); err != nil {
		t.Fatalf("click Copy MCP Config: %v", err)
	}
	if !strings.Contains(tt.Clipboard(), `"mcpServers"`) {
		t.Fatalf("expected MCP config JSON in clipboard, got %q", tt.Clipboard())
	}

	if snapDir := os.Getenv("ARCMD_SNAPSHOT"); snapDir != "" {
		_ = os.MkdirAll(snapDir, 0o755)
		tt.SetSize(1240, 960)
		tt.Frame()
		if f, err := os.Create(filepath.Join(snapDir, "settings-local-agent.png")); err == nil {
			_ = png.Encode(f, tt.Image())
			_ = f.Close()
		}
	}
}

func TestAgentControlIPCCancelAndTimeoutDoNotDeleteIPCOrSplitBrainWrite(t *testing.T) {
	cfg := testControlConfig(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	spRoot := a.state.ActiveSpace().Root
	ipcFile := agentIPCPath(cfg)

	enteredMain := make(chan struct{}, 4)
	releaseMain := make(chan struct{})
	var blockMain sync.Mutex
	shouldBlock := true

	a.controlExec = func(fn func() (any, error)) (any, error) {
		blockMain.Lock()
		blocking := shouldBlock
		blockMain.Unlock()
		if blocking {
			select {
			case enteredMain <- struct{}{}:
			default:
			}
			<-releaseMain
		}
		return fn()
	}
	if err := a.startAgentIPC(); err != nil {
		t.Fatal(err)
	}
	defer a.stopAgentIPC()

	createReqParams, _ := json.Marshal(map[string]any{
		"path":    "SplitBrainGuard.md",
		"content": "# Must Not Write Headless\n",
	})
	req := ControlRequest{Action: "doc.create", Params: createReqParams}

	// (a1) Pre-cancelled context must return context.Canceled without deleting agent-ipc.json or running headless fallback.
	canceledCtx, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	if _, _, err := ExecuteControl(canceledCtx, cfg, req); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if _, err := os.Stat(ipcFile); err != nil {
		t.Fatalf("cancelled request deleted agent-ipc.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(spRoot, "SplitBrainGuard.md")); !os.IsNotExist(err) {
		t.Fatalf("cancelled request triggered split-brain headless disk write: %v", err)
	}

	// (a2) Post-connect timeout while main thread is busy must NOT delete agent-ipc.json and must NOT fall back to headless write.
	shortCtx, cancelShort := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancelShort()
	errCh := make(chan error, 1)
	go func() {
		_, _, callErr := ExecuteControl(shortCtx, cfg, req)
		errCh <- callErr
	}()
	select {
	case <-enteredMain:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live IPC handler to enter invokeOnMain")
	}
	callErr := <-errCh
	if !errors.Is(callErr, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", callErr)
	}
	if _, err := os.Stat(ipcFile); err != nil {
		t.Fatalf("timed-out live IPC request deleted agent-ipc.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(spRoot, "SplitBrainGuard.md")); !os.IsNotExist(err) {
		t.Fatalf("timed-out live IPC request triggered split-brain headless disk write: %v", err)
	}

	// Unblock main thread and verify subsequent live IPC call still routes to the live server cleanly.
	blockMain.Lock()
	shouldBlock = false
	blockMain.Unlock()
	close(releaseMain)

	if _, live := mustControl(t, cfg, "status", nil); !live {
		t.Fatal("expected live IPC server to remain reachable after timeout")
	}
}

func TestAgentControlStopAgentIPCUnblocksInFlightInvokeImmediately(t *testing.T) {
	cfg := testControlConfig(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	inMainWait := make(chan struct{})
	mainClosed := make(chan struct{})
	a.controlExec = func(fn func() (any, error)) (any, error) {
		close(inMainWait)
		<-mainClosed
		return nil, context.Canceled
	}
	if err := a.startAgentIPC(); err != nil {
		t.Fatal(err)
	}
	defer close(mainClosed)

	reqDone := make(chan error, 1)
	go func() {
		_, _, err := ExecuteControl(context.Background(), cfg, ControlRequest{Action: "status"})
		reqDone <- err
	}()

	select {
	case <-inMainWait:
	case <-time.After(time.Second):
		t.Fatal("IPC request never reached invokeOnMain")
	}

	start := time.Now()
	a.stopAgentIPC()
	elapsed := time.Since(start)
	if elapsed >= 50*time.Millisecond {
		t.Fatalf("stopAgentIPC blocked for %v (want < 50ms)", elapsed)
	}

	select {
	case err := <-reqDone:
		if err == nil {
			t.Fatal("expected aborted IPC request to return an error when server shuts down")
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight ExecuteControl did not unblock after stopAgentIPC")
	}
}

func TestAgentControlDatabaseUpdateViewPreservesConfigAndCreateOverwriteGuard(t *testing.T) {
	cfg := testControlConfig(t)
	spOut, _ := mustControl(t, cfg, "space.create", map[string]any{
		"name":   "数据完整性空间",
		"switch": true,
	})
	spRoot := spOut["root"].(string)

	mustControl(t, cfg, "database.schema", map[string]any{
		"space": "数据完整性空间",
		"op":    "create",
		"path":  "看板.db.md",
		"title": "看板",
		"properties": []map[string]any{
			{"id": "prop_status", "name": "状态", "type": "status"},
			{"id": "prop_prio", "name": "优先级", "type": "select", "options": []map[string]any{{"name": "高", "color": "red"}}},
		},
		"views": []map[string]any{
			{"id": "v_board", "name": "研发看板", "type": "board", "groupBy": "状态"},
		},
		"records": []map[string]any{
			{"title": "卡片一", "fields": map[string]any{"状态": "未开始"}},
		},
	})

	// Seed rich view configuration (Filter, ManualOrder, Board settings, ColorRules) directly on disk.
	rawDB, err := vault.Read(spRoot, "看板.db.md")
	if err != nil {
		t.Fatal(err)
	}
	parsedDB, err := database.DecodeDatabaseMarkdown(rawDB)
	if err != nil {
		t.Fatal(err)
	}
	st := database.NewStore(spRoot, "看板.db.md", parsedDB)
	var recID string
	if rows := st.Query("v_board", database.QueryOptions{}).Rows; len(rows) > 0 {
		recID = rows[0].ID
	}
	for _, vw := range st.Database().Views {
		if vw.ID == "v_board" {
			cloned := vw.Clone()
			cloned.Filter = &database.FilterGroup{
				Conjunction: "and",
				Rules: []database.FilterRule{
					{ID: "r1", PropertyID: "prop_status", Op: database.OpEquals, Value: database.TextValue("未开始")},
				},
			}
			cloned.ManualOrder = []string{recID}
			cloned.Board = &database.BoardViewConfig{
				CardSize:      "large",
				HideEmptyCols: true,
				ColorColumn:   true,
				ShowCount:     true,
			}
			cloned.Colors = []database.ColorRule{
				{
					ID:    "c1",
					Color: "blue",
					Filter: database.FilterGroup{
						Conjunction: "and",
						Rules:       []database.FilterRule{{ID: "cr1", PropertyID: "prop_status", Op: database.OpEquals, Value: database.TextValue("未开始")}},
					},
				},
			}
			if err := st.UpdateView(cloned); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	hSeed, err := newHeadlessApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := hSeed.saveDBStoreForControl(hSeed.state.ActiveSpace(), "看板.db.md", st, nil); err != nil {
		t.Fatal(err)
	}

	// 1. update_view patching only Name and Icon must preserve Filter, ManualOrder, Board, and Colors.
	mustControl(t, cfg, "database.schema", map[string]any{
		"space": "数据完整性空间",
		"op":    "update_view",
		"path":  "看板.db.md",
		"view": map[string]any{
			"id":   "v_board",
			"name": "主研发看板",
			"icon": "kanban",
		},
	})

	afterRaw, err := vault.Read(spRoot, "看板.db.md")
	if err != nil {
		t.Fatal(err)
	}
	afterDB, err := database.DecodeDatabaseMarkdown(afterRaw)
	if err != nil {
		t.Fatal(err)
	}
	var gotView database.ViewConfig
	for _, vw := range afterDB.Views {
		if vw.ID == "v_board" {
			gotView = vw
			break
		}
	}
	if gotView.Type != database.ViewBoard || gotView.Name != "主研发看板" || gotView.Icon != "kanban" {
		t.Fatalf("unexpected updated view metadata: %+v", gotView)
	}
	if gotView.Filter == nil || len(gotView.Filter.Rules) != 1 || gotView.Filter.Rules[0].PropertyID != "prop_status" {
		t.Fatalf("update_view wiped Filter: %+v", gotView.Filter)
	}
	if len(gotView.ManualOrder) != 1 || gotView.ManualOrder[0] != recID {
		t.Fatalf("update_view wiped ManualOrder: %+v", gotView.ManualOrder)
	}
	if gotView.Board == nil || gotView.Board.CardSize != "large" || !gotView.Board.HideEmptyCols || !gotView.Board.ColorColumn {
		t.Fatalf("update_view wiped Board config: %+v", gotView.Board)
	}
	if len(gotView.Colors) != 1 || gotView.Colors[0].Color != "blue" {
		t.Fatalf("update_view wiped ColorRules: %+v", gotView.Colors)
	}

	// 2. database.schema create without overwrite must refuse to clobber an existing .db.md.
	dupParams, _ := json.Marshal(map[string]any{
		"space": "数据完整性空间",
		"op":    "create",
		"path":  "看板.db.md",
		"title": "误覆盖库",
	})
	if _, _, err := ExecuteControl(context.Background(), cfg, ControlRequest{Action: "database.schema", Params: dupParams}); err == nil {
		t.Fatal("expected database.schema create without overwrite=true to fail on existing .db.md")
	}
	stillRaw, _ := vault.Read(spRoot, "看板.db.md")
	stillDB, _ := database.DecodeDatabaseMarkdown(stillRaw)
	if stillDB.Title != "看板" {
		t.Fatalf("failed create mutated existing database title to %q", stillDB.Title)
	}

	// With overwrite: true, replacing succeeds cleanly.
	mustControl(t, cfg, "database.schema", map[string]any{
		"space":     "数据完整性空间",
		"op":        "create",
		"path":      "看板.db.md",
		"title":     "新版看板",
		"overwrite": true,
	})
	overRaw, _ := vault.Read(spRoot, "看板.db.md")
	overDB, _ := database.DecodeDatabaseMarkdown(overRaw)
	if overDB.Title != "新版看板" {
		t.Fatalf("expected overwritten database title '新版看板', got %q", overDB.Title)
	}
}

func TestAgentControlHeadlessCrossDatabaseRelationAndRollup(t *testing.T) {
	cfg := testControlConfig(t)
	mustControl(t, cfg, "space.create", map[string]any{
		"name":   "关联测试空间",
		"switch": true,
	})

	// 1. Target DB: Projects.db.md with two-way relation back to Tasks.db.md and a Rollup summing points.
	mustControl(t, cfg, "database.schema", map[string]any{
		"space":      "关联测试空间",
		"op":         "create",
		"path":       "Projects.db.md",
		"title":      "项目库",
		"sourceType": "inline",
		"properties": []map[string]any{
			{
				"id":   "prop_tasks",
				"name": "关联任务",
				"type": "relation",
				"relation": map[string]any{
					"targetDB":     "Tasks.db.md",
					"syncedPropID": "prop_proj",
				},
			},
			{
				"id":   "prop_total_pts",
				"name": "总工时",
				"type": "rollup",
				"rollup": map[string]any{
					"relationPropID": "prop_tasks",
					"targetPropID":   "prop_pts",
					"calculate":      "sum",
				},
			},
		},
		"records": []map[string]any{
			{"title": "ArcMD 2.0"},
		},
	})

	projQuery, _ := mustControl(t, cfg, "database.records", map[string]any{
		"space": "关联测试空间",
		"op":    "query",
		"path":  "Projects.db.md",
	})
	projRows := projQuery["records"].([]any)
	if len(projRows) != 1 {
		t.Fatalf("expected 1 project record, got %d", len(projRows))
	}
	projID := projRows[0].(map[string]any)["id"].(string)

	// 2. Source DB: Tasks.db.md with two-way relation to Projects.db.md and numeric prop_pts.
	mustControl(t, cfg, "database.schema", map[string]any{
		"space":      "关联测试空间",
		"op":         "create",
		"path":       "Tasks.db.md",
		"title":      "任务库",
		"sourceType": "inline",
		"properties": []map[string]any{
			{
				"id":   "prop_proj",
				"name": "所属项目",
				"type": "relation",
				"relation": map[string]any{
					"targetDB":     "Projects.db.md",
					"syncedPropID": "prop_tasks",
				},
			},
			{
				"id":   "prop_pts",
				"name": "工时",
				"type": "number",
			},
		},
	})

	// Add two tasks linked to projID via headless ExecuteControl: two-way sync must persist into Projects.db.md!
	mustControl(t, cfg, "database.records", map[string]any{
		"space":  "关联测试空间",
		"op":     "add",
		"path":   "Tasks.db.md",
		"title":  "IPC 安全加固",
		"fields": map[string]any{"所属项目": []string{projID}, "工时": 5},
	})
	mustControl(t, cfg, "database.records", map[string]any{
		"space":  "关联测试空间",
		"op":     "add",
		"path":   "Tasks.db.md",
		"title":  "数据库视图增量更新",
		"fields": map[string]any{"所属项目": []string{projID}, "工时": 8},
	})

	// Query Projects.db.md in a fresh headless ExecuteControl call and verify two-way relation + Rollup sum == 13!
	afterProj, _ := mustControl(t, cfg, "database.records", map[string]any{
		"space": "关联测试空间",
		"op":    "query",
		"path":  "Projects.db.md",
	})
	afterRows := afterProj["records"].([]any)
	fields := afterRows[0].(map[string]any)["fields"].(map[string]any)
	linkedTasks, ok := fields["关联任务"].([]any)
	if !ok || len(linkedTasks) != 2 {
		t.Fatalf("expected 2 synced backlinked tasks in headless mode, got %+v", fields)
	}
	if fields["总工时"] != "13" {
		t.Fatalf("expected headless cross-DB Rollup '总工时' == '13', got %v (%T) in %+v", fields["总工时"], fields["总工时"], fields)
	}
}
