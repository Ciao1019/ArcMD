package app

import (
	"fmt"
	"path/filepath"
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
		a.openSplit(a.state.ActiveSpace(), "Journal/Ideas.arcmd", false, false)
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
					labels := []string{"后退", "前进", "重新载入", "文档路径", "更多格式", "更多"}
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
