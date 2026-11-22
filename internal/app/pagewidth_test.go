package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/prefs"
	"github.com/egoist/mygo/ui"
)

func TestPageWidthButtonAndModes(t *testing.T) {
	a, tt := newTestApp(t)
	// Wide enough that sidebar (230) and default outline (248) leave room
	// beyond the 1040 wide column so narrow < standard < wide < full.
	tt.SetSize(1800, 860)
	settle(tt)

	stdBody := a.doc.ed.BodyWidth()
	stdPad := a.doc.ed.Padding()

	// Top-right button opens the Feishu-style page width popover menu.
	if err := tt.Click("页面宽度"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, item := range []string{"窄", "标准", "较宽", "全宽"} {
		if !tt.HasText(item) {
			t.Fatalf("page width menu missing %q: %q", item, tt.Texts())
		}
	}

	// Pick 较宽 (wide, 1040).
	if err := tt.Click("较宽"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.prefs.Page != "wide" {
		t.Fatalf("Page = %q; want wide", a.prefs.Page)
	}
	wideBody := a.doc.ed.BodyWidth()
	if wideBody <= stdBody {
		t.Fatalf("wide body %v should exceed standard %v", wideBody, stdBody)
	}

	// Pick 全宽 (full viewport width).
	if err := tt.Click("页面宽度"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("全宽"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.prefs.Page != "full" || !a.prefs.FullWidth() {
		t.Fatalf("Page = %q; want full", a.prefs.Page)
	}
	fullBody := a.doc.ed.BodyWidth()
	fullPad := a.doc.ed.Padding()
	if fullPad != 80 {
		t.Fatalf("full width left padding = %v; want 80", fullPad)
	}
	if fullBody <= wideBody {
		t.Fatalf("full width body %v should exceed wide %v", fullBody, wideBody)
	}

	// Persistence round-trip.
	saved, err := (prefs.Store{Path: a.prefStore.Path}).Load()
	if err != nil || saved.Page != "full" || !saved.FullWidth() {
		t.Fatalf("saved prefs = %+v, err=%v", saved, err)
	}

	// Pick 窄 (narrow, 680).
	if err := tt.Click("页面宽度"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("窄"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	narrowBody := a.doc.ed.BodyWidth()
	if narrowBody >= stdBody || a.doc.ed.Padding() <= stdPad {
		t.Fatalf("narrow body=%v pad=%v should be narrower than standard body=%v pad=%v", narrowBody, a.doc.ed.Padding(), stdBody, stdPad)
	}
}

func TestPageWidthShortcutRestoresPrevious(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1500, 860)
	settle(tt)

	// Starting in wide -> Cmd+Shift+\ enters full -> Cmd+Shift+\ restores wide.
	a.setPageWidth("wide")
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBackslash)
	tt.Frame()
	if a.prefs.Page != "full" {
		t.Fatalf("expected full after shortcut, got %q", a.prefs.Page)
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBackslash)
	tt.Frame()
	if a.prefs.Page != "wide" {
		t.Fatalf("expected wide restored after second shortcut, got %q", a.prefs.Page)
	}

	// Starting in narrow -> full -> narrow.
	a.setPageWidth("narrow")
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBackslash)
	tt.Frame()
	if a.prefs.Page != "full" {
		t.Fatalf("expected full from narrow, got %q", a.prefs.Page)
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBackslash)
	tt.Frame()
	if a.prefs.Page != "narrow" {
		t.Fatalf("expected narrow restored, got %q", a.prefs.Page)
	}

	// Freshly started in full width falls back to standard ("").
	a.prefs.Page = "full"
	a.pageWidthBeforeFull = ""
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBackslash)
	tt.Frame()
	if a.prefs.Page != "" {
		t.Fatalf("expected fallback to standard, got %q", a.prefs.Page)
	}
}

func TestPageWidthMoreMenuSettingsPaletteAndFocus(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1500, 860)
	settle(tt)

	// More menu submenu works on Markdown docs.
	if err := tt.Click("更多"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("页面宽度"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("全宽"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.prefs.FullWidth() {
		t.Fatalf("more menu did not enable full width: %q", a.prefs.Page)
	}

	// Settings Appearance segmented choice stays in sync and reset clears it.
	a.openSettings(tabAppearance)
	tt.Frame()
	if err := tt.Click("宽"); err != nil {
		t.Fatal(err)
	}
	if a.prefs.Page != "wide" {
		t.Fatalf("settings choice = %q; want wide", a.prefs.Page)
	}
	if err := tt.Click("全宽"); err != nil {
		t.Fatal(err)
	}
	if a.prefs.Page != "full" || a.pageWidthBeforeFull != "wide" {
		t.Fatalf("settings full choice = %q (before=%q)", a.prefs.Page, a.pageWidthBeforeFull)
	}
	a.settingsOpen = false
	tt.Frame()

	// Command palette switches to 较宽 and exits full width.
	a.openPalette()
	a.palette.query = "退出全宽显示"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.prefs.Page != "wide" {
		t.Fatalf("palette exit full width = %q; want wide", a.prefs.Page)
	}
	a.openPalette()
	a.palette.query = "页面宽度：全宽"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.prefs.Page != "full" {
		t.Fatalf("palette set full width = %q; want full", a.prefs.Page)
	}

	// Focus mode hover bar exposes the top-right page width button.
	a.toggleFocus()
	tt.Move(750, 16)
	tt.Advance(time.Second)
	tt.Frame()
	if err := tt.Click("页面宽度"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("标准"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.prefs.Page != "" {
		t.Fatalf("focus bar page width = %q; want standard", a.prefs.Page)
	}
	a.toggleFocus()
	tt.Frame()

	// Source preview hides the page-width button and disables toggling.
	a.toggleSource()
	tt.Frame()
	if _, ok := tt.Find("页面宽度"); ok {
		t.Fatal("page width button should be hidden in source export preview")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBackslash)
	tt.Frame()
	if a.prefs.Page != "" {
		t.Fatalf("shortcut should not toggle width in source preview: %q", a.prefs.Page)
	}
	a.toggleSource()
	tt.Frame()

	// Whiteboard hides the page-width button and ignores the shortcut.
	a.newWhiteboard("")
	tt.Frame()
	if _, ok := tt.Find("页面宽度"); ok {
		t.Fatal("page width button should be hidden for whiteboards")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBackslash)
	tt.Frame()
	if a.prefs.Page != "" {
		t.Fatalf("shortcut should not toggle width for whiteboards: %q", a.prefs.Page)
	}
}

func TestPageWidthStandaloneWindow(t *testing.T) {
	a, tt := newTestApp(t)
	var made []*fakeWindow
	a.newWindow = func(title string, view func(*ui.Context), close func() bool) windowHandle {
		w := &fakeWindow{title: title, view: view, close: close}
		made = append(made, w)
		return w
	}
	tt.Frame()
	a.openInWindow(a.state.ActiveSpace(), "Journal/Ideas.md")
	if len(made) != 1 {
		t.Fatalf("expected 1 standalone window, got %d", len(made))
	}
	// Make main window show a whiteboard so the shortcut proves it inspects the standalone doc.
	a.newWhiteboard("")
	tt.Frame()

	wt := ui.NewTester(made[0].view, 1400, 800)
	wt.Frame()
	if err := wt.Click("页面宽度"); err != nil {
		t.Fatal(err)
	}
	wt.Frame()
	if err := wt.Click("较宽"); err != nil {
		t.Fatal(err)
	}
	wt.Frame()
	if a.prefs.Page != "wide" {
		t.Fatalf("standalone button Page = %q; want wide", a.prefs.Page)
	}
	wt.Key(ui.Cmd|ui.Shift, ui.KeyBackslash)
	wt.Frame()
	if !a.prefs.FullWidth() {
		t.Fatalf("standalone shortcut did not toggle full width: %q", a.prefs.Page)
	}
	wt.Key(ui.Cmd|ui.Shift, ui.KeyBackslash)
	wt.Frame()
	if a.prefs.Page != "wide" {
		t.Fatalf("standalone shortcut did not restore wide: %q", a.prefs.Page)
	}
}

func TestPageWidthSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render page width snapshots")
	a, tt := newTestApp(t)
	tt.SetSize(1560, 860)
	settle(tt)

	// Populate a wide table, headings, and rich prose so column width and outline behavior are obvious.
	a.doc.ed = editor.New(
		"# 页面宽度对比示例\n\n" +
			"这段正文用来观察标准（820）、较宽（1040）与全宽模式在宽屏显示器上的排版视野差异，确认左侧块加号留白与右侧边距保持均衡。\n\n" +
			"## 宽度档位\n\n" +
			"| 模块 | 负责范围 | 宽度表现 | 备注 |\n" +
			"| --- | --- | --- | --- |\n" +
			"| internal/prefs | 持久化 page 选项（narrow / standard / wide / full） | 四档无缝兼容 | 不影响旧配置 |\n" +
			"| internal/editor | 全宽视口排版、大纲避让与块手柄安全边距 | 铺满可用编辑区域 | 保留左侧 80px 留白 |\n" +
			"| internal/app | 右上角页面宽度按钮、更多菜单、专注顶栏与命令栏 | 较宽与全宽一键切换 | 支持 ⇧⌘\\ 快捷键 |\n",
	)
	a.setOutline(false)
	tt.Frame()

	for _, tc := range []struct {
		name    string
		page    string
		menu    bool
		outline bool
	}{
		{"standard", "", false, false},
		{"wide", "wide", false, false},
		{"full", "full", false, false},
		{"full-menu", "full", true, false},
		{"full-outline", "full", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a.setOutline(tc.outline)
			a.setPageWidth(tc.page)
			tt.CloseMenu()
			settle(tt)
			if tc.menu {
				if err := tt.Click("页面宽度"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
			}
			savePNG(t, filepath.Join(dir, "page-width-"+tc.name+".png"), tt)
			if tc.menu {
				tt.CloseMenu()
				tt.Frame()
			}
		})
	}
}
