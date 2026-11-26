package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/prefs"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestPageZoomShortcutsPillMenuAndPersistence(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1500, 860)
	settle(tt)

	// At default 100%, the corner zoom pill stays hidden.
	if a.pageZoom() != prefs.DefaultPageZoom {
		t.Fatalf("initial zoom = %v; want 1.0", a.pageZoom())
	}
	if _, ok := tt.Find("页面缩放"); ok {
		t.Fatal("page zoom pill should be hidden at 100%")
	}

	// Focus the editor to verify zoom shortcuts work while actively editing.
	a.doc.ed.RequestFocus = true
	tt.Frame()
	if !tt.Focused("文档") {
		t.Fatal("editor did not receive focus")
	}

	// Cmd+= steps up to 110% and reveals the corner pill.
	tt.Key(ui.Cmd, ui.KeyEqual)
	tt.Frame()
	if a.pageZoom() != 1.1 || !tt.HasText("110%") {
		t.Fatalf("after Cmd+= zoom=%v texts=%q", a.pageZoom(), tt.Texts())
	}
	// On macOS, Cmd+Shift+= (Cmd++) also steps zoom up without conflicting with split-add.
	if ui.Cmd == ui.Super {
		tt.Key(ui.Cmd|ui.Shift, ui.KeyEqual)
	} else {
		tt.Key(ui.Cmd, ui.KeyEqual)
	}
	tt.Frame()
	if a.pageZoom() != 1.2 || !tt.HasText("120%") {
		t.Fatalf("after second zoom-in zoom=%v", a.pageZoom())
	}

	// Ctrl+Shift+= must still add a split pane without changing zoom.
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyEqual)
	settle(tt)
	if a.split == nil || len(a.split.panes) < 2 {
		t.Fatal("Ctrl+Shift+= did not open a split pane")
	}
	if a.pageZoom() != 1.2 {
		t.Fatalf("Ctrl+Shift+= unexpectedly changed zoom to %v", a.pageZoom())
	}
	if a.palette.open {
		tt.Key(0, ui.KeyEscape)
		tt.Frame()
	}
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyMinus)
	settle(tt)
	if a.split != nil {
		t.Fatal("Ctrl+Shift+- did not close the split pane")
	}
	if a.pageZoom() != 1.2 {
		t.Fatalf("Ctrl+Shift+- unexpectedly changed zoom to %v", a.pageZoom())
	}

	// Clicking the corner pill opens the zoom popover menu with presets and reset.
	if err := tt.Click("页面缩放"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"放大页面", "缩小页面", "重置为 100%", "75%", "100%", "125%", "150%", "200%"} {
		if !tt.HasText(want) {
			t.Fatalf("zoom popover menu missing %q: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("150%"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.pageZoom() != 1.5 || !tt.HasText("150%") {
		t.Fatalf("after preset 150%% zoom=%v", a.pageZoom())
	}

	// Saved to prefs.json and restored on restart.
	saved, err := (prefs.Store{Path: a.prefStore.Path}).Load()
	if err != nil || saved.Zoom() != 1.5 {
		t.Fatalf("saved prefs = %+v, err=%v", saved, err)
	}
	reloaded, err := New(a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.pageZoom() != 1.5 {
		t.Fatalf("reloaded app zoom = %v; want 1.5", reloaded.pageZoom())
	}

	// Cmd+- steps down to 140%; Cmd+0 resets to 100% and hides the pill.
	tt.Key(ui.Cmd, ui.KeyMinus)
	tt.Frame()
	if a.pageZoom() != 1.4 || !tt.HasText("140%") {
		t.Fatalf("after Cmd+- zoom=%v", a.pageZoom())
	}
	tt.Key(ui.Cmd, ui.Key0)
	tt.Frame()
	if a.pageZoom() != 1.0 {
		t.Fatalf("after Cmd+0 zoom=%v; want 1.0", a.pageZoom())
	}
	if _, ok := tt.Find("页面缩放"); ok {
		t.Fatal("page zoom pill should hide again after reset to 100%")
	}
}

func TestPageZoomMoreMenuPaletteFocusWindowAndIsolation(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1500, 860)
	settle(tt)

	// More menu -> 页面缩放 -> 125%.
	if err := tt.Click("更多"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("页面缩放"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("125%"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.pageZoom() != 1.25 {
		t.Fatalf("more menu zoom = %v; want 1.25", a.pageZoom())
	}

	// Command palette zoom in (125% -> 130%) and reset (130% -> 100%).
	a.openPalette()
	a.palette.query = "放大页面"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.pageZoom() != 1.3 {
		t.Fatalf("palette zoom-in = %v; want 1.3", a.pageZoom())
	}
	a.openPalette()
	a.palette.query = "重置页面缩放"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.pageZoom() != 1.0 {
		t.Fatalf("palette zoom-reset = %v; want 1.0", a.pageZoom())
	}

	// Focus mode hover bar shows the zoom pill when zoomed and resets via menu.
	a.setPageZoom(1.4)
	a.toggleFocus()
	tt.Move(750, 16)
	tt.Advance(time.Second)
	tt.Frame()
	if err := tt.Click("页面缩放"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("重置为 100%"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.pageZoom() != 1.0 {
		t.Fatalf("focus bar reset zoom = %v; want 1.0", a.pageZoom())
	}
	a.toggleFocus()
	tt.Frame()

	// Source export preview hides zoom pill and ignores zoom shortcuts.
	a.setPageZoom(1.2)
	a.toggleSource()
	tt.Frame()
	if _, ok := tt.Find("页面缩放"); ok {
		t.Fatal("zoom pill should be hidden in source export preview")
	}
	tt.Key(ui.Cmd, ui.KeyEqual)
	tt.Frame()
	if a.pageZoom() != 1.2 {
		t.Fatalf("source preview should ignore zoom-in shortcut, got %v", a.pageZoom())
	}
	a.toggleSource()
	tt.Frame()

	// Standalone window responds to zoom shortcuts even if main window shows a whiteboard.
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
	a.newWhiteboard("")
	tt.Frame()
	if _, ok := tt.Find("页面缩放"); ok {
		t.Fatal("zoom pill should be hidden for whiteboards")
	}
	tt.Key(ui.Cmd, ui.KeyEqual)
	tt.Frame()
	if a.pageZoom() != 1.2 {
		t.Fatalf("whiteboard should ignore page zoom shortcut, got %v", a.pageZoom())
	}

	wt := ui.NewTester(made[0].view, 1400, 800)
	wt.Frame()
	if !wt.HasText("120%") {
		t.Fatalf("standalone window missing 120%% pill: %q", wt.Texts())
	}
	wt.Key(ui.Cmd, ui.KeyEqual)
	wt.Frame()
	if a.pageZoom() != 1.3 {
		t.Fatalf("standalone Cmd+= zoom = %v; want 1.3", a.pageZoom())
	}
	wt.Key(ui.Cmd, ui.Key0)
	wt.Frame()
	if a.pageZoom() != 1.0 {
		t.Fatalf("standalone Cmd+0 zoom = %v; want 1.0", a.pageZoom())
	}

	// PDF document preserves its own Cmd+= zoom and does not alter pageZoom.
	sp := a.state.ActiveSpace()
	pdfRel := "sample.pdf"
	if err := os.WriteFile(filepath.Join(sp.Root, pdfRel), buildRichSamplePDF(t), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open(pdfRel)
	tt.Frame()
	if a.doc == nil || !a.doc.isPDF() {
		t.Fatal("expected PDF document open")
	}
	a.setPageZoom(1.2)
	a.doc.pdf.RequestFocus = true
	tt.Frame()
	if _, ok := tt.Find("页面缩放"); ok {
		t.Fatal("page zoom pill should not appear on PDF viewer")
	}
	pdfZoomBefore := a.doc.pdf.Zoom
	tt.Key(ui.Cmd, ui.KeyEqual)
	tt.Frame()
	if a.pageZoom() != 1.2 {
		t.Fatalf("PDF viewer Cmd+= mutated pageZoom to %v", a.pageZoom())
	}
	if a.doc.pdf.Zoom <= pdfZoomBefore {
		t.Fatalf("PDF viewer did not receive Cmd+= zoom: before=%v after=%v", pdfZoomBefore, a.doc.pdf.Zoom)
	}
}

func TestPageZoomShortcutRebindAndEnglishUI(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1400, 860)
	settle(tt)

	// Rebind zoom-reset to Cmd+Shift+0, verify old Cmd+0 stops resetting and new key works.
	if !a.rebind("zoom-reset", &editor.Combo{Mods: ui.Cmd | ui.Shift, Key: ui.Key0}) {
		t.Fatal("failed to rebind zoom-reset to Cmd+Shift+0")
	}
	a.setPageZoom(1.3)
	tt.Frame()
	tt.Key(ui.Cmd, ui.Key0)
	tt.Frame()
	if a.pageZoom() != 1.3 {
		t.Fatalf("old zoom-reset binding should not fire after rebind, zoom=%v", a.pageZoom())
	}
	tt.Key(ui.Cmd|ui.Shift, ui.Key0)
	tt.Frame()
	if a.pageZoom() != 1.0 {
		t.Fatalf("rebound zoom-reset did not reset zoom, zoom=%v", a.pageZoom())
	}
	// Restore default Cmd+0 (must not be rejected as a reserved combo).
	if !a.rebind("zoom-reset", nil) {
		t.Fatal("failed to restore default zoom-reset binding")
	}

	// Verify English labels in zoom pill popover and Settings -> Shortcuts.
	prevLang := i18n.Language()
	defer i18n.SetLanguage(prevLang)
	a.prefs.Language = "en"
	a.applyLanguage()
	a.setPageZoom(1.3)
	tt.Frame()

	if err := tt.Click("Page Zoom"); err != nil {
		t.Fatalf("English zoom pill not found: %v", err)
	}
	tt.Frame()
	for _, want := range []string{"Zoom In", "Zoom Out", "Reset to 100%"} {
		if !tt.HasText(want) {
			t.Fatalf("English zoom menu missing %q: %q", want, tt.Texts())
		}
	}
	tt.CloseMenu()
	tt.Frame()

	a.openSettings(tabShortcuts)
	tt.Frame()
	if !tt.HasText("Zoom In") || !tt.HasText("Zoom Out") || !tt.HasText("Reset Page Zoom") {
		t.Fatalf("English shortcuts list missing zoom items: %q", tt.Texts())
	}
}

func TestViewMenuDoesNotInterceptPageZoomOrReload(t *testing.T) {
	a, _ := newTestApp(t)
	menu := mygo.NewMenu([]*mygo.MenuItem{a.viewMenu()})
	items := menu.Items()
	if len(items) != 1 || items[0].Role != mygo.RoleViewMenu || items[0].Label == "" {
		t.Fatalf("unexpected view menu root: %+v", items)
	}
	sub := items[0].Submenu
	if len(sub) != 3 ||
		sub[0].Role != mygo.RoleToggleDevTools ||
		sub[1].Type != mygo.MenuItemSeparator ||
		sub[2].Role != mygo.RoleToggleFullScreen {
		t.Fatalf("unexpected view submenu: %+v", sub)
	}
	blocked := map[string]bool{
		"CmdOrCtrl+Plus":    true,
		"CmdOrCtrl+=":       true,
		"CmdOrCtrl+-":       true,
		"CmdOrCtrl+0":       true,
		"CmdOrCtrl+R":       true,
		"Shift+CmdOrCtrl+R": true,
	}
	for _, it := range sub {
		if it.Role == mygo.RoleZoomIn || it.Role == mygo.RoleZoomOut || it.Role == mygo.RoleResetZoom ||
			it.Role == mygo.RoleReload || it.Role == mygo.RoleForceReload {
			t.Fatalf("view submenu includes webview-only role %q", it.Role)
		}
		if blocked[it.Accelerator] {
			t.Fatalf("view submenu item %q intercepts app shortcut %q", it.Role, it.Accelerator)
		}
	}
}

func TestPageZoomSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render page zoom snapshots")
	a, tt := newTestApp(t)
	tt.SetSize(1440, 860)
	settle(tt)

	a.doc.ed = editor.New(
		"# 页面缩放演示文档\n\n" +
			"用于演示和讲解时的等比缩放效果：正文、**粗体**、`行内代码`、行内公式 $E = mc^2$ 与列表标记同步放大或缩小。\n\n" +
			"## 核心元素\n\n" +
			"- 键盘快捷键：`⌘+` / `⌘-` / `⌘0`（Windows 为 `Ctrl+=` / `Ctrl+-` / `Ctrl+0`）\n" +
			"1. 鼠标滚轮：`⌘/Ctrl + 滚轮` 逐级缩放\n" +
			"- [x] 触控板双指捏合缩放，右上角显示比例药丸\n\n" +
			"> [!TIP]\n" +
			"> 缩放比例为 100% 时自动隐藏右上角比例按钮，保持界面安静克制。\n\n" +
			"```go\n" +
			"func StepPageZoom(cur float32, delta int) float32 {\n" +
			"    return NormalizePageZoom(cur + float32(delta)*0.10)\n" +
			"}\n" +
			"```\n\n" +
			"| 档位 | 适用场景 | 布局表现 |\n" +
			"| --- | --- | --- |\n" +
			"| 80% | 浏览长文档与宽表 | 保持正文列宽，单行容纳更多文字 |\n" +
			"| 100% | 日常写作与阅读 | 默认比例，右上角不显示提示 |\n" +
			"| 140% | 会议投屏与讲解 | 标题、正文、表格、公式与代码块等比放大 |\n",
	)
	a.setOutline(false)
	tt.Frame()

	for _, tc := range []struct {
		name string
		zoom float32
		dark bool
		menu bool
	}{
		{"100-light", 1.0, false, false},
		{"140-light", 1.4, false, false},
		{"140-light-menu", 1.4, false, true},
		{"80-light", 0.8, false, false},
		{"140-dark", 1.4, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tt.SetDark(tc.dark)
			a.setPageZoom(tc.zoom)
			tt.CloseMenu()
			settle(tt)
			if tc.menu {
				if err := tt.Click("页面缩放"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
			}
			savePNG(t, filepath.Join(dir, "page-zoom-"+tc.name+".png"), tt)
			if tc.menu {
				tt.CloseMenu()
				tt.Frame()
			}
		})
	}
}
