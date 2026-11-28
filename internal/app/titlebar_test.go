package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Windows keeps three 46-DIP caption buttons at the right of the client
// area, even with a hidden title bar. Tester does not draw those buttons.
var windowsTitleBar = ui.TitleBar{Height: 32, Right: 138}

func titleBarTestApp(t *testing.T, mode string, width int, bar ui.TitleBar) (*App, *ui.Tester) {
	t.Helper()
	a, tt := newTestApp(t)
	tt.SetTitleBar(bar)
	tt.SetSize(width, 760)
	switch mode {
	case "folded":
		a.sidebarHidden = true
	case "focus":
		a.toggleFocus()
	case "split", "split-folded", "split-vertical":
		a.openSplit(a.state.ActiveSpace(), "Journal/Ideas.md", false, false)
		a.sidebarHidden = mode == "split-folded"
		a.split.vertical = mode == "split-vertical"
	}
	settle(tt)
	if mode == "focus" {
		tt.Move(float32(width)/2, 16)
		tt.Advance(time.Second)
		tt.Frame()
	}
	return a, tt
}

func TestTitleBarControlsStayClear(t *testing.T) {
	for _, profile := range []struct {
		name string
		bar  ui.TitleBar
	}{
		{"windows", windowsTitleBar},
		{"macos", ui.TitleBar{Height: 46, Left: 78}},
		{"fullscreen", ui.TitleBar{}},
	} {
		for _, mode := range []string{"normal", "folded", "focus", "split", "split-folded", "split-vertical"} {
			for _, width := range []int{640, 1240} {
				t.Run(fmt.Sprintf("%s/%s/%d", profile.name, mode, width), func(t *testing.T) {
					a, tt := titleBarTestApp(t, mode, width, profile.bar)
					labels := []string{"后退", "前进", "文档路径", "更多格式", "更多"}
					if !a.focus && a.split == nil {
						if _, ok := tt.Find("重新载入"); ok {
							t.Fatal("toolbar should not show reload button")
						}
					}
					if a.focus {
						labels = []string{"退出专注模式"}
					} else if a.split != nil {
						labels = []string{"分屏 1 文档路径", "分屏 2 文档路径", "关闭分屏 1", "关闭分屏 2"}
						if a.folded() {
							labels = append(labels, "展开侧边栏", "更多")
						}
					}
					for _, label := range labels {
						r, ok := tt.Find(label)
						if !ok || r.W <= 0 || r.H <= 0 {
							t.Fatalf("%q is missing or empty: %+v", label, r)
						}
						if r.Y < profile.bar.Height && (r.X < profile.bar.Left || r.X+r.W > float32(width)-profile.bar.Right) {
							t.Errorf("%q overlaps native window controls: bounds %+v, title bar %+v", label, r, profile.bar)
						}
					}
				})
			}
		}
	}
}

type testCaption struct {
	fullscreen, maximized bool
	minimized, closed     int
}

func (w *testCaption) IsFullScreen() bool { return w.fullscreen }
func (w *testCaption) IsMaximized() bool  { return w.maximized }
func (w *testCaption) Minimize()          { w.minimized++ }
func (w *testCaption) ToggleMaximize()    { w.maximized = !w.maximized }
func (w *testCaption) Close()             { w.closed++ }

func TestClientCaptionStaysReachable(t *testing.T) {
	a, tt := titleBarTestApp(t, "split", 640, ui.TitleBar{})
	w := &testCaption{}
	a.testCaptionWindow = w
	click := func(label string) {
		t.Helper()
		r, ok := tt.Find(label)
		if !ok || r.W < 30 || r.X < 0 || r.X+r.W > 640 || r.Y+r.H > 46 {
			t.Fatalf("%q outside visible caption area: %+v", label, r)
		}
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	tt.Frame()
	click("最大化窗口")
	click("还原窗口")
	if w.maximized {
		t.Fatal("restore did not leave maximized mode")
	}
	a.openLibrary(libSpaces)
	settle(tt)
	a.qa.open = true
	tt.Frame()
	tt.Advance(50 * time.Millisecond)
	tt.Frame()
	click("最小化窗口")
	settle(tt)
	click("关闭窗口")
	if w.minimized != 1 || w.closed != 1 {
		t.Fatalf("caption actions: minimize=%d, close=%d", w.minimized, w.closed)
	}
	w.fullscreen = true
	tt.Frame()
	if _, ok := tt.Find("关闭窗口"); ok {
		t.Fatal("caption buttons remain visible in fullscreen")
	}
}

func TestWindowsTitleBarFullscreen(t *testing.T) {
	_, tt := titleBarTestApp(t, "folded", 1240, windowsTitleBar)
	// Display scaling must not turn the native button reserve into pixels.
	tt.SetScale(1.5)
	before, ok := tt.Find("更多")
	if !ok || before.X+before.W > 1240-windowsTitleBar.Right {
		t.Fatalf("toolbar overlaps caption buttons at 150%% scale: %+v", before)
	}
	if err := tt.Click("更多"); err != nil || !tt.HasText("设置…") {
		t.Fatalf("toolbar menu did not open: %v", err)
	}
	tt.CloseMenu()
	tt.SetTitleBar(ui.TitleBar{})
	after, ok := tt.Find("更多")
	if !ok || after.X-before.X < windowsTitleBar.Right-1 {
		t.Fatalf("fullscreen did not reclaim caption space: before %+v, after %+v", before, after)
	}
	tt.SetTitleBar(windowsTitleBar)
	restored, ok := tt.Find("更多")
	if !ok || restored.X+restored.W > 1240-windowsTitleBar.Right {
		t.Fatalf("leaving fullscreen overlaps caption buttons: %+v", restored)
	}
}

func TestWindowsTitleBarSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the Windows title bar layout")
	for _, mode := range []string{"normal", "folded", "focus", "split", "split-folded"} {
		for _, width := range []int{640, 1240} {
			name := fmt.Sprintf("%s-%d", mode, width)
			t.Run(name, func(t *testing.T) {
				_, tt := titleBarTestApp(t, mode, width, windowsTitleBar)
				savePNG(t, filepath.Join(dir, "windows-titlebar-"+name+".png"), tt)
			})
		}
	}
}

func TestLongDocumentNameKeepsToolbarReachable(t *testing.T) {
	a, tt := titleBarTestApp(t, "folded", 640, windowsTitleBar)
	a.doc.rel = "这是一篇标题很长很长的文档用于检查窄窗口中的工具栏不会丢失任何操作按钮.arcmd"
	settle(tt)
	for _, label := range []string{"文档路径", "更多格式", "更多"} {
		r, ok := tt.Find(label)
		if !ok || r.W <= 0 || r.X+r.W > 640-windowsTitleBar.Right {
			t.Fatalf("%s is unreachable: %+v", label, r)
		}
	}
}

func TestLongTitleSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render long document names")
	for _, width := range []int{640, 1240} {
		a, tt := titleBarTestApp(t, "folded", width, windowsTitleBar)
		a.doc.rel = "这是一篇标题很长很长的文档用于检查窄窗口中的工具栏不会丢失任何操作按钮.arcmd"
		settle(tt)
		savePNG(t, filepath.Join(dir, fmt.Sprintf("long-title-%d.png", width)), tt)
	}
}

func TestToolbarKeepsWindowDragSpace(t *testing.T) {
	macBar := ui.TitleBar{Height: 46, Left: 78}
	rightEdgeOf := func(t *testing.T, tt *ui.Tester, bar ui.TitleBar) float32 {
		t.Helper()
		minX := float32(1e9)
		for _, info := range tt.AccessAll() {
			if info.Bounds.W <= 0 || info.Bounds.H <= 0 || info.Bounds.Y >= bar.Height {
				continue
			}
			// Any button to the right of the address bar belongs to the right toolbar group.
			if info.Name == "页面宽度" || info.Name == "思维导图" || info.Name == "更多格式" || info.Name == "问答" || info.Name == "更多" {
				if info.Bounds.X < minX {
					minX = info.Bounds.X
				}
			}
		}
		if minX >= 1e8 {
			t.Fatal("could not find right-side toolbar buttons")
		}
		return minX
	}

	for _, tc := range []struct {
		name        string
		mode        string
		longTitle   bool
		emptyDoc    bool
		dirtyDoc    bool
		minDragRoom float32
	}{
		{"folded-normal-title", "folded", false, false, false, 400},
		{"normal-sidebar-normal-title", "normal", false, false, false, 200},
		{"folded-long-title", "folded", true, false, false, 120},
		{"normal-sidebar-long-title", "normal", true, false, false, 80},
		{"folded-empty-doc", "folded", false, true, false, 400},
		{"folded-dirty-doc", "folded", false, false, true, 350},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, tt := titleBarTestApp(t, tc.mode, 1240, macBar)
			switch {
			case tc.emptyDoc:
				a.doc = nil
				settle(tt)
			case tc.longTitle:
				a.doc.rel = "这是一篇标题很长很长非常长用于验证顶部工具栏始终保留窗口拖动空白的测试文档名称.arcmd"
				settle(tt)
			case tc.dirtyDoc:
				a.doc.ed.SetSelection(0, 0)
				a.doc.ed.ReplaceText(nil, "Hello ")
				tt.Frame()
				if !a.doc.dirty() || !tt.HasText("已修改") {
					t.Fatal("dirty status badge not shown in address pill")
				}
			}
			addr, ok := tt.Find("文档路径")
			if !ok || addr.W <= 0 {
				t.Fatalf("address bar missing: %+v", addr)
			}
			rightStart := rightEdgeOf(t, tt, macBar)
			dragW := rightStart - (addr.X + addr.W)
			if dragW < tc.minDragRoom {
				t.Fatalf("expected at least %.0fpx drag space to the right of address, got %.1fpx (addr=%+v, rightStart=%.1f)", tc.minDragRoom, dragW, addr, rightStart)
			}

			// Moving and clicking in the empty drag strip must not trigger hover cursor or open palette.
			dragX := addr.X + addr.W + dragW/2
			dragY := addr.Y + addr.H/2
			tt.Move(dragX, dragY)
			if got := tt.Cursor(); got == ui.CursorPointer {
				t.Fatalf("drag strip should not show pointer cursor, got %v", got)
			}
			tt.ClickAt(dragX, dragY)
			if a.palette.open {
				t.Fatal("clicking the toolbar drag strip opened the command palette")
			}

			// Right-clicking the drag strip still opens the toolbar context menu when a document is open.
			if !tc.emptyDoc {
				tt.RightClickAt(dragX, dragY)
				if !slices.Contains(tt.Menu(), "自定义工具栏…") {
					t.Fatalf("right-clicking toolbar drag strip did not open custom toolbar menu: %v", tt.Menu())
				}
				tt.CloseMenu()
			}

			// Clicking the compact address pill still opens the command palette.
			tt.Move(addr.X+addr.W/2, dragY)
			if got := tt.Cursor(); got != ui.CursorPointer {
				t.Fatalf("address pill should show pointer cursor, got %v", got)
			}
			tt.ClickAt(addr.X+addr.W/2, dragY)
			if !a.palette.open {
				t.Fatal("clicking the address pill did not open the command palette")
			}
		})
	}
}
