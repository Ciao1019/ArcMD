package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/prefs"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestCustomToolbarCatalogCoversAllValidItemsAndTranslations(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("zh-Hans") })
	if len(toolbarItemDefs) != len(prefs.ValidToolbarItems) {
		t.Fatalf("toolbarItemDefs len = %d; want %d", len(toolbarItemDefs), len(prefs.ValidToolbarItems))
	}
	for _, id := range prefs.ValidToolbarItems {
		def, ok := toolbarItemByID[id]
		if !ok {
			t.Fatalf("missing toolbarItemDef for %q", id)
		}
		if def.icon == nil || def.label == "" || def.group == "" {
			t.Fatalf("incomplete toolbarItemDef for %q: %+v", id, def)
		}
	}

	i18n.SetLanguage("en")
	for _, s := range []string{
		"自定义工具栏",
		"自定义工具栏…",
		"常用写作工具",
		"当前工具栏顺序",
		"更多快捷工具",
		"新建文档默认使用一级标题",
		"打开样式配置文件",
		"重新载入样式配置",
		"Typora 标题键",
	} {
		got := i18n.T(s)
		if got == s || got == "" {
			t.Fatalf("missing English translation for %q (got %q)", s, got)
		}
	}
	for _, def := range toolbarItemDefs {
		if got := i18n.T(def.label); got == "" {
			t.Fatalf("empty English label for %q", def.id)
		}
	}
}

func TestCustomToolbarModalPresetReorderRemoveAndPersistence(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()

	if len(a.activeToolbarItems()) != 0 {
		t.Fatalf("default toolbar items = %v; want empty", a.activeToolbarItems())
	}

	// Open from Settings -> Appearance -> 自定义工具栏…
	a.openSettings(tabAppearance)
	tt.Frame()
	if r, ok := tt.Find("界面语言"); ok {
		tt.Scroll(r.X+r.W/2, r.Y+120, 0, 500)
		tt.Frame()
	}
	if err := tt.Click("自定义工具栏…"); err != nil {
		t.Fatalf("click 自定义工具栏… in Appearance settings: %v", err)
	}
	tt.Frame()
	if a.settingsOpen || !a.toolbarModal.open {
		t.Fatalf("expected settings closed and toolbar modal open: settings=%v modal=%v", a.settingsOpen, a.toolbarModal.open)
	}

	// Apply 常用写作工具 preset.
	if err := tt.Click("常用写作工具"); err != nil {
		t.Fatalf("click 常用写作工具: %v", err)
	}
	tt.Frame()
	if !slices.Equal(a.activeToolbarItems(), toolbarCommonPreset) {
		t.Fatalf("after preset: got %v; want %v", a.activeToolbarItems(), toolbarCommonPreset)
	}
	saved, err := a.prefStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(saved.ToolbarItems, toolbarCommonPreset) {
		t.Fatalf("persisted toolbarItems = %v; want %v", saved.ToolbarItems, toolbarCommonPreset)
	}

	// Reorder: move 表格 right (index 0 -> 1), then move 代码块 right (index 0 -> 1, back to original).
	if err := tt.Click("右移 表格"); err != nil {
		t.Fatalf("click 右移 表格: %v", err)
	}
	tt.Frame()
	if got := a.activeToolbarItems(); got[0] != "code-block" || got[1] != "table" {
		t.Fatalf("after move right: %v", got)
	}
	if err := tt.Click("左移 表格"); err != nil {
		t.Fatalf("click 左移 表格: %v", err)
	}
	tt.Frame()
	if !slices.Equal(a.activeToolbarItems(), toolbarCommonPreset) {
		t.Fatalf("after move left: %v", a.activeToolbarItems())
	}

	// Remove 高亮块 via × chip button.
	if err := tt.Click("移除 高亮块"); err != nil {
		t.Fatalf("click 移除 高亮块: %v", err)
	}
	tt.Frame()
	if a.hasToolbarItem("callout") {
		t.Fatalf("callout still active after remove: %v", a.activeToolbarItems())
	}

	// Close with 完成 button.
	if err := tt.Click("完成"); err != nil {
		t.Fatalf("click 完成: %v", err)
	}
	tt.Frame()
	if a.toolbarModal.open {
		t.Fatal("modal still open after 完成")
	}

	// Reopen via Command Palette (⌘K) and clear all, then close with Esc.
	a.openPalette()
	a.palette.query = "自定义工具栏"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !a.toolbarModal.open {
		t.Fatal("expected modal open from command palette")
	}
	if err := tt.Click("清空"); err != nil {
		t.Fatalf("click 清空: %v", err)
	}
	tt.Frame()
	if len(a.activeToolbarItems()) != 0 {
		t.Fatalf("expected empty after 清空, got %v", a.activeToolbarItems())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.toolbarModal.open {
		t.Fatal("modal still open after Escape")
	}

	// Also verify editor Options.OnCustomizeToolbar wires to openCustomToolbar.
	opts := a.editorOptionsFor(nil, 800, false, a.doc)
	if opts.OnCustomizeToolbar == nil {
		t.Fatal("editorOptionsFor.OnCustomizeToolbar is nil")
	}
	opts.OnCustomizeToolbar()
	if !a.toolbarModal.open {
		t.Fatal("OnCustomizeToolbar did not open custom toolbar modal")
	}
	a.toolbarModal.open = false
}

func TestCustomToolbarActionsReadOnlySourceAndNarrowHide(t *testing.T) {
	a, tt := newTestApp(t)
	if err := vault.Write(root(a), "Test.md", "# Title\n\nHello world\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Test.md")
	a.setToolbarItems([]string{"table", "code-block", "math", "callout", "bold", "source"})
	focusDocument(a, tt)

	// Narrow window (<520 cardWidth) hides custom toolbar items cleanly.
	tt.SetSize(480, 640)
	tt.Frame()
	if _, ok := tt.Find("表格"); ok {
		t.Fatal("expected custom toolbar items hidden when cardWidth < 520")
	}

	// Restore wide window: custom toolbar buttons appear.
	tt.SetSize(1240, 800)
	tt.Frame()
	if _, ok := tt.Find("表格"); !ok {
		t.Fatal("expected 表格 button visible in wide window")
	}

	// Place caret on "Hello world" and click 表格 quick button.
	leaves := a.doc.ed.Doc.Leaves()
	a.doc.ed.SetSelection(leaves[1].Start, leaves[1].End)
	tt.Frame()
	if err := tt.Click("粗体"); err != nil {
		t.Fatalf("click 粗体: %v", err)
	}
	tt.Frame()
	if !a.doc.ed.Doc.Blocks[1].Runs[0].Style.Bold {
		t.Fatalf("expected paragraph text bolded, runs=%+v", a.doc.ed.Doc.Blocks[1].Runs)
	}

	// Insert table via quick toolbar button.
	beforeBlocks := len(a.doc.ed.Doc.Blocks)
	if err := tt.Click("表格"); err != nil {
		t.Fatalf("click 表格: %v", err)
	}
	tt.Frame()
	hasTable := false
	for _, b := range a.doc.ed.Doc.Blocks {
		if b.Kind == "table" {
			hasTable = true
			break
		}
	}
	if !hasTable || len(a.doc.ed.Doc.Blocks) <= beforeBlocks {
		t.Fatalf("expected table inserted, blocks=%v", kindsOf(a))
	}

	// Read-only mode disables mutating toolbar actions while keeping 源码模式 active.
	a.doc.ed.SetReadOnly(true)
	tt.Frame()
	revBefore := a.doc.ed.Revision()
	a.runToolbarItem(a.doc, "code-block")
	if a.doc.ed.Revision() != revBefore {
		t.Fatal("expected code-block toolbar action to no-op in ReadOnly mode")
	}

	// Clicking 源码模式 switches to source mode and stays clickable to switch back.
	if err := tt.Click("源码模式"); err != nil {
		t.Fatalf("click 源码模式: %v", err)
	}
	tt.Frame()
	if !a.doc.source {
		t.Fatal("expected document in source mode")
	}
	if err := tt.Click("源码模式"); err != nil {
		t.Fatalf("click 源码模式 in source mode: %v", err)
	}
	tt.Frame()
	if a.doc.source {
		t.Fatal("expected document back in rich text mode")
	}
	a.doc.ed.SetReadOnly(false)

	// Exercise remaining quick-action handlers directly to ensure all 23 wire cleanly.
	picked := false
	a.pickFiles = func(done func([]string)) { picked = true }
	for _, id := range []string{
		"code-block", "math", "inline-math", "callout", "task", "bullet",
		"numbered", "quote", "toggle", "rule", "columns", "mermaid",
		"italic", "underline", "strike", "inline-code", "annotate", "link", "image",
	} {
		a.runToolbarItem(a.doc, id)
	}
	if !picked {
		t.Fatal("expected image toolbar action to invoke pickImages")
	}
}

func TestSettingsNoDefaultTitleToggleAndPaletteHeadingLabels(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()

	// Toggle 新建文档默认使用一级标题 in Settings -> General -> Writing.
	a.openSettings(tabGeneral)
	tt.Frame()
	if a.prefs.NoDefaultTitle {
		t.Fatal("expected NoDefaultTitle false by default")
	}
	if err := tt.Click("新建文档默认使用一级标题"); err != nil {
		t.Fatalf("click 新建文档默认使用一级标题: %v", err)
	}
	tt.Frame()
	if !a.prefs.NoDefaultTitle {
		t.Fatal("expected NoDefaultTitle true after clicking toggle")
	}
	saved, err := a.prefStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !saved.NoDefaultTitle {
		t.Fatal("expected noDefaultTitle persisted in prefs.json")
	}
	a.settingsOpen = false
	tt.Frame()

	// Verify palette heading commands display dynamic keyLabel("heading-1".."heading-6").
	a.applyTyporaHeadings()
	a.openPalette()
	a.palette.query = "转换为二级标题"
	tt.Frame()
	wantHint := a.keyLabel("heading-2")
	if wantHint == "" || !tt.HasText(wantHint) {
		t.Fatalf("expected palette to display dynamic heading-2 shortcut %q, texts=%v", wantHint, tt.Texts())
	}
}

func TestIssue92ToolbarAndHeadingSnapshots(t *testing.T) {
	toolbarOut := os.Getenv("ARCMD_TOOLBAR_PNG")
	h2Out := os.Getenv("ARCMD_HEADING_H2_PNG")
	if toolbarOut == "" && h2Out == "" {
		dir := snapshotDir(t, "set ARCMD_SNAPSHOT, ARCMD_TOOLBAR_PNG, or ARCMD_HEADING_H2_PNG to render snapshots")
		toolbarOut = filepath.Join(dir, "toolbar.png")
		h2Out = filepath.Join(dir, "heading-h2.png")
	}

	if toolbarOut != "" {
		a, tt := newTestApp(t)
		tt.SetSize(1280, 860)
		a.setToolbarItems([]string{"table", "code-block", "link", "image", "math", "callout", "bold", "source"})
		a.openCustomToolbar()
		for range 4 {
			tt.Frame()
		}
		if err := os.MkdirAll(filepath.Dir(toolbarOut), 0o755); err != nil {
			t.Fatal(err)
		}
		savePNG(t, toolbarOut, tt)
	}

	if h2Out != "" {
		a, tt := newTestApp(t)
		tt.SetSize(1240, 820)
		md := strings.Join([]string{
			"## 美剧观察笔记",
			"",
			"不用强制一级标题，文档首行可以直接从 **二级标题** 自然开始，自动命名文件并同步左侧目录。",
			"",
			"### 近期在看",
			"",
			"- **人生切割术（Severance）** — 冷峻克制的空间叙事与双线悬念",
			"- **熊家餐馆（The Bear）** — 高密度剪辑下的厨房群像与成长",
			"",
			"### 剧集评分记录",
			"",
			"| 剧名 | 季数 | 印象 |",
			"| --- | --- | --- |",
			"| Severance | S2 | 构图极简，节奏精准 |",
			"| The Bear | S3 | 情绪真挚，配乐出色 |",
			"",
		}, "\n")
		if err := vault.Write(root(a), "美剧观察笔记.md", md); err != nil {
			t.Fatal(err)
		}
		a.refreshTree()
		a.open("美剧观察笔记.md")
		for range 4 {
			tt.Frame()
		}
		if err := os.MkdirAll(filepath.Dir(h2Out), 0o755); err != nil {
			t.Fatal(err)
		}
		savePNG(t, h2Out, tt)
	}
}
