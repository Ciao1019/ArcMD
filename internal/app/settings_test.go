package app

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestMoreMenuAndSettings(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	if err := tt.Click("更多"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("设置…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.settingsOpen || a.settingsTab != tabGeneral || !tt.HasText("新空间的默认位置") {
		t.Fatalf("settings: open %v, tab %q", a.settingsOpen, a.settingsTab)
	}
	if err := tt.Click("快捷键"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("命令栏") || !tt.HasText("⇧⌘H") || !tt.HasText("⌘[") {
		t.Errorf("shortcuts tab: %q", tt.Texts())
	}
	if err := tt.Click("空间"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Personal") {
		t.Errorf("spaces tab: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.settingsOpen {
		t.Fatal("Escape kept the settings open")
	}

	// ⌘, opens them too; the menu opens the About tab.
	tt.Key(ui.Cmd, ui.KeyComma)
	tt.Frame()
	if !a.settingsOpen {
		t.Fatal("⌘, did not open the settings")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Click("更多")
	tt.Frame()
	if err := tt.Click("关于 ArcMD"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settingsTab != tabAbout || !tt.HasText("版本 "+Version) {
		t.Errorf("about: tab %q", a.settingsTab)
	}
	if err := tt.Click("GitHub 开源仓库"); err != nil {
		t.Fatal(err)
	}
	wantURL := "https://github.com/Ciao1019/ArcMD"
	if got := tt.OpenedURLs(); !slices.Equal(got, []string{wantURL}) {
		t.Fatalf("repository link: %q", got)
	}
	tt.Key(0, ui.KeyEnter)
	if got := tt.OpenedURLs(); !slices.Equal(got, []string{wantURL, wantURL}) {
		t.Fatalf("repository link from keyboard: %q", got)
	}
}

// TestSettingsSnapshot renders the ⋯ menu and each settings tab, for review.
func TestSettingsSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the settings")
	for _, name := range []string{"more-menu", tabGeneral, tabSpaces, tabShortcuts, tabAbout, "about@dark", "about@narrow"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetSize(1240, 760)
			tab, variant, _ := strings.Cut(name, "@")
			if variant == "dark" {
				tt.SetDark(true)
			}
			if variant == "narrow" {
				tt.SetSize(640, 500)
			}
			tt.Frame()
			if name == "more-menu" {
				tt.Click("更多")
			} else {
				a.openSettings(tab)
			}
			for range 4 {
				tt.Frame()
			}
			file, err := os.Create(filepath.Join(dir, fmt.Sprintf("settings-%s.png", name)))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := png.Encode(file, tt.Image()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// The word count and the format menu sit in the toolbar, beside the
// address, and leave with the editor in the export preview.
func TestToolbarCorner(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	r, ok := tt.Find("更多格式")
	if !ok || r.Y > 46 {
		t.Fatalf("format menu at %v, found %v", r, ok)
	}
	if _, ok := tt.Find("预览 Markdown 导出 (⇧⌘M)"); ok {
		t.Fatal("the source toggle is still in the toolbar")
	}
	if err := tt.Click("更多格式"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tt.Menu(), "删除线") {
		t.Fatalf("format menu: %q", tt.Menu())
	}
	tt.CloseMenu()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	tt.Frame()
	if !a.doc.source {
		t.Fatal("⇧⌘M did not open the export preview")
	}
	if _, ok := tt.Find("更多格式"); ok {
		t.Fatal("format menu shown over the export preview")
	}
}
