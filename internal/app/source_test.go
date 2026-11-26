package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestSourceModeEditAutosaveAndRoundTrip(t *testing.T) {
	a, tt := newTestApp(t)
	// Raw Markdown with broken/incompatible syntax and no trailing newline.
	raw := "# Raw Title\n\n| A | B |\n| ---\n| 1 | 2 |"
	if err := vault.Write(root(a), "Broken.md", raw); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Broken.md")
	tt.Frame()

	// Entering source mode shows exact raw disk bytes without marking dirty.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	tt.Frame()
	if !a.doc.source {
		t.Fatal("⇧⌘M did not enter source mode")
	}
	if a.doc.preview != raw {
		t.Fatalf("source buffer altered untouched file:\ngot:  %q\nwant: %q", a.doc.preview, raw)
	}
	if a.doc.dirty() {
		t.Fatal("entering source mode marked untouched file dirty")
	}

	// Fix the broken table separator directly in source mode.
	fixed := "# Raw Title\n\n| A | B |\n| --- | --- |\n| 1 | 2 |\n\n<!-- keep comment -->\n"
	if err := tt.Click("Markdown 源码编辑器"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.SetClipboard(fixed)
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	if !a.doc.dirty() || !tt.HasText("已修改") {
		t.Fatalf("editing in source mode did not mark document dirty: dirty=%v texts=%q", a.doc.dirty(), tt.Texts())
	}

	// Autosave writes the exact raw Markdown to disk.
	tt.Advance(autosaveDelay + 50*time.Millisecond)
	tt.Frame()
	if a.doc.dirty() {
		t.Fatal("document stayed dirty after autosave in source mode")
	}
	onDisk, err := vault.Read(root(a), "Broken.md")
	if err != nil {
		t.Fatal(err)
	}
	if onDisk != fixed {
		t.Fatalf("saved disk text mismatch:\ngot:  %q\nwant: %q", onDisk, fixed)
	}

	// Returning to rich mode parses the fixed table and stays clean.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	tt.Frame()
	if a.doc.source {
		t.Fatal("⇧⌘M did not return to rich mode")
	}
	if a.doc.dirty() {
		t.Fatal("returning to rich mode after source autosave became spuriously dirty")
	}
	hasTable := false
	for _, b := range a.doc.ed.Doc.Blocks {
		if b.Kind == "table" {
			hasTable = true
			break
		}
	}
	if !hasTable {
		t.Fatalf("fixed table syntax did not parse into table block: %+v", a.doc.ed.Doc.Blocks)
	}

	// One Cmd+Z in rich mode undoes the entire source-mode edit cleanly.
	a.doc.ed.Undo()
	tt.Frame()
	if !a.doc.dirty() {
		t.Fatal("undoing source edit in rich mode did not mark document dirty")
	}
	if a.doc.ed.Doc.ImportSource != raw {
		t.Fatalf("undo did not restore original ImportSource:\ngot:  %q\nwant: %q", a.doc.ed.Doc.ImportSource, raw)
	}
}

func TestSourceModeFormattingOnlyEditPreserved(t *testing.T) {
	a, tt := newTestApp(t)
	orig := "* alpha\n* beta\n"
	if err := vault.Write(root(a), "List.md", orig); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("List.md")
	tt.Frame()

	a.toggleSource()
	tt.Frame()
	a.doc.preview = "- alpha\n- beta\n"
	tt.Frame()

	// Switch back to rich mode BEFORE autosave fires: formatting-only change must remain dirty and save!
	a.toggleSource()
	tt.Frame()
	if !a.doc.dirty() {
		t.Fatal("formatting-only edit in source mode was lost when returning to rich mode")
	}
	if err := a.doc.save(); err != nil {
		t.Fatal(err)
	}
	got, err := vault.Read(root(a), "List.md")
	if err != nil {
		t.Fatal(err)
	}
	if got != "- alpha\n- beta\n" {
		t.Fatalf("saved formatting-only change mismatch:\ngot:  %q\nwant: %q", got, "- alpha\n- beta\n")
	}
}

func TestSourceModeUndoAfterAutosaveAndNoAutosave(t *testing.T) {
	a, tt := newTestApp(t)
	if err := vault.Write(root(a), "UndoSource.md", "# Alpha\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("UndoSource.md")
	tt.Frame()

	// Edit in source mode, let autosave write, then undo inside source mode.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	tt.Frame()
	tt.Type("Extra ")
	tt.Frame()
	edited := a.doc.preview
	tt.Advance(autosaveDelay + 50*time.Millisecond)
	tt.Frame()
	if a.doc.dirty() {
		t.Fatal("expected clean after source autosave")
	}
	if got, _ := vault.Read(root(a), "UndoSource.md"); got != edited {
		t.Fatalf("disk did not get edited text: %q", got)
	}

	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	if a.doc.preview != "# Alpha\n" {
		t.Fatalf("Cmd+Z inside source mode did not revert preview: %q", a.doc.preview)
	}
	if !a.doc.dirty() {
		t.Fatal("undoing back to pre-edit text after autosave did not mark document dirty against newer disk")
	}
	tt.Advance(autosaveDelay + 50*time.Millisecond)
	tt.Frame()
	if got, _ := vault.Read(root(a), "UndoSource.md"); got != "# Alpha\n" || a.doc.dirty() {
		t.Fatalf("reverted text did not re-save: dirty=%v disk=%q", a.doc.dirty(), got)
	}

	// With autosave off: edit -> save -> undo in source -> exit to rich must stay dirty.
	a.prefs.NoAutosave = true
	tt.Type("Again ")
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyS)
	tt.Frame()
	if a.doc.dirty() {
		t.Fatal("Cmd+S in source mode did not clear dirty")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	if !a.doc.dirty() {
		t.Fatal("undo after Cmd+S did not mark dirty")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	tt.Frame()
	if a.doc.source || !a.doc.dirty() {
		t.Fatalf("exiting source mode with unsaved undo must remain dirty: source=%v dirty=%v", a.doc.source, a.doc.dirty())
	}
}

func TestSourceModeReadOnlyLock(t *testing.T) {
	a, tt := newTestApp(t)
	a.doc.ed.SetReadOnly(true)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	tt.Frame()

	before := a.doc.preview
	if !tt.HasText("只读") {
		t.Fatal("read-only badge missing in source mode")
	}
	tt.Type("blocked")
	tt.Frame()
	if a.doc.preview != before {
		t.Fatalf("read-only source editor accepted edits: %q", a.doc.preview)
	}

	// Unlock right from the source header bar.
	if err := tt.Click("解除只读"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.doc.ed.ReadOnly {
		t.Fatal("clicking unlock did not clear ReadOnly")
	}
	if err := tt.Click("Markdown 源码编辑器"); err != nil {
		t.Fatal(err)
	}
	tt.Type("ok")
	tt.Frame()
	if a.doc.preview == before {
		t.Fatal("unlocked source editor did not accept typing")
	}
}

func TestSourceModeUnsavedPromptAndDiskConflict(t *testing.T) {
	a, tt := newTestApp(t)
	a.prefs.NoAutosave = true
	if err := vault.Write(root(a), "Manual.md", "# Original\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Manual.md")
	tt.Frame()

	a.toggleSource()
	tt.Frame()
	a.doc.preview = "# Edited In Source\n"
	tt.Frame()

	// Navigating away with autosave off prompts to save.
	a.open("Journal/Ideas.md")
	tt.Frame()
	if !a.unsaved.open {
		t.Fatal("expected unsaved prompt when leaving source-edited document with autosave off")
	}
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.doc.rel != "Journal/Ideas.md" {
		t.Fatalf("did not navigate after saving source edits: %q", a.doc.rel)
	}
	if got, _ := vault.Read(root(a), "Manual.md"); got != "# Edited In Source\n" {
		t.Fatalf("unsaved prompt Save did not persist source edits: %q", got)
	}

	// Re-open Manual.md in source mode, edit, then simulate external disk change.
	a.open("Manual.md")
	a.toggleSource()
	tt.Frame()
	a.doc.preview = "# Local Source Edit\n"
	tt.Frame()
	if err := vault.Write(root(a), "Manual.md", "# External Disk Edit\n"); err != nil {
		t.Fatal(err)
	}
	a.checkDisk()
	tt.Frame()
	if !a.doc.conflict || !tt.HasText("保留我的修改") {
		t.Fatalf("conflict bar not shown in source mode: conflict=%v texts=%q", a.doc.conflict, tt.Texts())
	}
	if err := tt.Click("保留我的修改"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.doc.conflict {
		t.Fatal("conflict not cleared after keeping mine")
	}
	if got, _ := vault.Read(root(a), "Manual.md"); got != "# Local Source Edit\n" {
		t.Fatalf("keep mine did not write source edits: %q", got)
	}
}

func TestSourceModeFindReplaceAndTabIndent(t *testing.T) {
	a, tt := newTestApp(t)
	if err := vault.Write(root(a), "Syntax.md", "foo one\nfoo two\nfoo three\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Syntax.md")
	tt.Frame()

	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	tt.Frame()

	// Tab indents, Shift+Tab unindents, both undoable.
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if !strings.HasPrefix(a.doc.preview, "  foo one") {
		t.Fatalf("Tab did not insert 2-space indent: %q", a.doc.preview)
	}
	tt.Key(ui.Shift, ui.KeyTab)
	tt.Frame()
	if !strings.HasPrefix(a.doc.preview, "foo one") {
		t.Fatalf("Shift+Tab did not unindent line: %q", a.doc.preview)
	}

	// Open inline find & replace via Cmd+Shift+H.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyH)
	tt.Frame()
	if !a.doc.sourceFind.open || !a.doc.sourceFind.replace {
		t.Fatalf("⇧⌘H did not open source find+replace bar: %+v", a.doc.sourceFind)
	}
	tt.Type("foo")
	tt.Frame()
	if len(a.doc.sourceFind.matches) != 3 || !tt.HasText("1 / 3") {
		t.Fatalf("expected 3 matches with '1 / 3', got %d texts=%q", len(a.doc.sourceFind.matches), tt.Texts())
	}

	a.doc.sourceFind.with = "bar"
	tt.Frame()
	if err := tt.Click("替换"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.doc.preview != "bar one\nfoo two\nfoo three\n" {
		t.Fatalf("single replace mismatch: %q", a.doc.preview)
	}
	if err := tt.Click("全部替换"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.doc.preview != "bar one\nbar two\nbar three\n" {
		t.Fatalf("replace all mismatch: %q", a.doc.preview)
	}

	// Esc closes find bar and refocuses source editor.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.doc.sourceFind.open {
		t.Fatal("Escape did not close source find bar")
	}
}

func TestSourceModeFocusWindowSearchQAAndArcmd(t *testing.T) {
	a, tt := newTestApp(t)
	a.toggleSource()
	tt.Frame()
	a.doc.preview = "# UniqueSourceHeading\n\nLiveSourceToken99\n"
	tt.Frame()

	// Live search and QA space immediately observe unsaved source edits.
	if !strings.Contains(a.doc.plainText(), "LiveSourceToken99") {
		t.Fatalf("plainText missed unsaved source edit: %q", a.doc.plainText())
	}
	if hits := a.liveIndex().Search("LiveSourceToken99", nil, 5); len(hits) == 0 {
		t.Fatal("liveIndex did not find unsaved source edit")
	}
	fs := a.qaSpace(a.state.ActiveSpace().ID)
	page, err := fs.Page(a.doc.rel)
	if err != nil || !strings.Contains(strings.Join(page.Lines, "\n"), "LiveSourceToken99") {
		t.Fatalf("qaSpace live page missed unsaved source edit: err=%v page=%+v", err, page)
	}

	// Focus mode renders the editable source view instead of blocking it.
	a.toggleFocus()
	tt.Frame()
	if tt.HasText("专注模式下没有可编辑的文档。") || !tt.HasText("Markdown 源码") {
		t.Fatalf("focus mode did not show editable source view: %q", tt.Texts())
	}
	a.toggleFocus()
	tt.Frame()

	// Standalone window supports toggling source mode.
	var made []*fakeWindow
	a.newWindow = func(title string, view func(*ui.Context), close func() bool) windowHandle {
		w := &fakeWindow{title: title, view: view, close: close}
		made = append(made, w)
		return w
	}
	a.openInWindow(a.state.ActiveSpace(), "Journal/Ideas.md")
	if len(made) != 1 {
		t.Fatalf("expected 1 standalone window, got %d", len(made))
	}
	wt := ui.NewTester(made[0].view, 900, 700)
	wt.Frame()
	wt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	wt.Frame()
	if !wt.HasText("Markdown 源码") {
		t.Fatalf("standalone window did not open source mode: %q", wt.Texts())
	}

	// Legacy .arcmd file also supports bidirectional source editing & save.
	legacyDoc := model.New()
	legacyDoc.Blocks = []*model.Block{model.NewBlock("heading", "Legacy"), model.NewBlock("paragraph", "Before")}
	legacyDoc.Blocks[0].Level = 1
	encoded, err := legacyDoc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root(a), "Legacy.arcmd"), []byte(encoded), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Legacy.arcmd")
	tt.Frame()
	a.toggleSource()
	tt.Frame()
	a.doc.preview = "# Legacy\n\nAfterSourceEdit\n"
	tt.Frame()
	if !a.doc.dirty() {
		t.Fatal(".arcmd edited in source mode was not marked dirty")
	}
	if err := a.doc.save(); err != nil {
		t.Fatal(err)
	}
	if a.doc.dirty() {
		t.Fatal(".arcmd remained dirty after saving source edit")
	}
	savedJSON, err := vault.Read(root(a), "Legacy.arcmd")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := model.Decode(savedJSON)
	if err != nil || !strings.Contains(decoded.PlainText(), "AfterSourceEdit") {
		t.Fatalf("re-decoded .arcmd missing source edit: err=%v doc=%+v", err, decoded)
	}
}

func TestSourceModeSplitPreviewLiveSyncAndSwap(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1240, 820)
	if err := vault.Write(root(a), "SplitDoc.md", "# Split Title\n\nInitial body.\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("SplitDoc.md")
	tt.Frame()

	// Default entering source mode is single-pane pure source (no split preview, no swap button).
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	tt.Frame()
	if !a.doc.source || a.doc.sourceSplit {
		t.Fatalf("expected pure source mode by default: source=%v split=%v", a.doc.source, a.doc.sourceSplit)
	}
	if _, ok := tt.Find("对调左右"); ok {
		t.Fatal("swap button should stay hidden when split preview is closed")
	}

	// Click "分栏预览" -> left rendered preview, right source editor.
	if err := tt.Click("分栏预览"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.doc.sourceSplit || a.doc.sourcePreviewEd == nil {
		t.Fatalf("split preview did not open: split=%v ed=%v", a.doc.sourceSplit, a.doc.sourcePreviewEd)
	}
	if !a.doc.sourcePreviewEd.ReadOnly {
		t.Fatal("rendered preview pane should be read-only")
	}
	prevRect, ok := tt.Find("文档")
	if !ok {
		t.Fatal("rendered preview surface missing in split mode")
	}
	srcRect, ok := tt.Find("Markdown 源码编辑器")
	if !ok {
		t.Fatal("source editor surface missing in split mode")
	}
	if prevRect.X >= srcRect.X {
		t.Fatalf("expected left rendered preview and right source editor by default: preview=%+v source=%+v", prevRect, srcRect)
	}

	// Edit source -> rendered preview refreshes after the 120ms debounce.
	updated := "# Split Title\n\n| ColA | ColB |\n| --- | --- |\n| 1 | 2 |\n"
	if err := tt.Click("Markdown 源码编辑器"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.SetClipboard(updated)
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	tt.Advance(sourcePreviewDebounce + 30*time.Millisecond)
	tt.Frame()
	hasTable := false
	for _, b := range a.doc.sourcePreviewEd.Doc.Blocks {
		if b.Kind == "table" {
			hasTable = true
			break
		}
	}
	if !hasTable {
		t.Fatalf("live preview did not refresh table after editing source: %+v", a.doc.sourcePreviewEd.Doc.Blocks)
	}

	// One-click swap left/right -> left source editor, right rendered preview, persisted in prefs.
	if err := tt.Click("对调左右"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.prefs.SourceSplitSwapped {
		t.Fatal("swap left/right did not update prefs.SourceSplitSwapped")
	}
	prevRect2, ok := tt.Find("文档")
	if !ok {
		t.Fatal("rendered preview surface missing after swap")
	}
	srcRect2, ok := tt.Find("Markdown 源码编辑器")
	if !ok {
		t.Fatal("source editor surface missing after swap")
	}
	if srcRect2.X >= prevRect2.X {
		t.Fatalf("expected left source editor and right rendered preview after swap: source=%+v preview=%+v", srcRect2, prevRect2)
	}

	// Narrow window stacks vertically cleanly without losing either pane.
	tt.SetSize(520, 720)
	tt.Frame()
	prevNarrow, ok1 := tt.Find("文档")
	srcNarrow, ok2 := tt.Find("Markdown 源码编辑器")
	if !ok1 || !ok2 {
		t.Fatalf("narrow split missing panes: preview=%v source=%v", ok1, ok2)
	}
	if srcNarrow.Y >= prevNarrow.Y {
		t.Fatalf("expected swapped narrow split to stack source above preview: source=%+v preview=%+v", srcNarrow, prevNarrow)
	}

	// Restore wide window, close split preview -> releases preview editor.
	tt.SetSize(1240, 820)
	tt.Frame()
	if err := tt.Click("分栏预览"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.doc.sourceSplit || a.doc.sourcePreviewEd != nil {
		t.Fatalf("closing split preview did not release preview editor: split=%v ed=%v", a.doc.sourceSplit, a.doc.sourcePreviewEd)
	}

	// Command palette exposes split & swap actions while in source mode.
	a.openPalette()
	tt.Frame()
	a.palette.query = "分栏预览"
	tt.Frame()
	if !tt.HasText("源码模式：开启分栏预览") {
		t.Fatalf("palette missing source split command: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
}

func TestSourceModeSplitSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render source split snapshots")
	sample := strings.Join([]string{
		"---",
		"title: 源码分栏对照",
		"tags: [markdown, preview]",
		"---",
		"",
		"# 源码分栏对照",
		"",
		"左侧实时渲染结果，右侧直接微调原始 Markdown 与不兼容语法。",
		"",
		"> [!TIP] 实时预览",
		"> 支持 **加粗**、`行内代码`、公式 $E = mc^2$ 与表格。",
		"",
		"| 语法 | 状态 |",
		"| --- | --- |",
		"| GFM 表格 | 已支持 |",
		"| 数学公式 | 已支持 |",
		"",
		"```go",
		"func greet(name string) string {",
		"    return \"Hello, \" + name",
		"}",
		"```",
	}, "\n")
	for _, tc := range []struct {
		name    string
		dark    bool
		swapped bool
	}{
		{"source-split-light", false, false},
		{"source-split-dark", true, false},
		{"source-split-swapped", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetSize(1360, 860)
			tt.SetDark(tc.dark)
			if err := vault.Write(root(a), "SplitDemo.md", sample); err != nil {
				t.Fatal(err)
			}
			a.refreshTree()
			a.open("SplitDemo.md")
			a.toggleSource()
			a.toggleSourceSplit(a.doc)
			a.prefs.SourceSplitSwapped = tc.swapped
			for range 4 {
				tt.Frame()
			}
			writePNG(t, filepath.Join(dir, tc.name+".png"), tt.Image())
		})
	}
}
