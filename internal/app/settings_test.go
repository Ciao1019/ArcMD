package app

import (
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/vault"
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

	// ⌘, opens them too; About remains available inside settings.
	tt.Key(ui.Cmd, ui.KeyComma)
	tt.Frame()
	if !a.settingsOpen {
		t.Fatal("⌘, did not open the settings")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Click("更多")
	tt.Frame()
	if err := tt.Click("设置…"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("关于"); err != nil {
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

// Export is one menu entry, with both formats reachable by pointer or keyboard.
func TestMoreMenuExport(t *testing.T) {
	for _, keyboard := range []bool{false, true} {
		t.Run(fmt.Sprintf("keyboard=%v", keyboard), func(t *testing.T) {
			a, tt := newTestApp(t)
			if err := tt.Click("更多"); err != nil {
				t.Fatal(err)
			}
			if tt.HasText("导出 Markdown 文件") || tt.HasText("导出 PDF") {
				t.Fatal("export formats should only appear in the submenu")
			}
			if keyboard {
				tt.Key(0, ui.KeyHome)
				for range 5 {
					tt.Key(0, ui.KeyDown)
				}
				tt.Key(0, ui.KeyEnter)
			} else if err := tt.Click("导出"); err != nil {
				t.Fatal(err)
			}
			if !tt.HasText("导出 Markdown 文件") || !tt.HasText("导出 PDF") {
				t.Fatal("export submenu did not show both formats")
			}
			if keyboard {
				tt.Key(0, ui.KeyEnter)
			} else if err := tt.Click("导出 Markdown 文件"); err != nil {
				t.Fatal(err)
			}
			got, err := vault.Read(root(a), "Welcome Export.md")
			if err != nil || got != a.doc.text() {
				t.Fatalf("exported Markdown differs from the document: %v", err)
			}
		})
	}
}

func TestMoreMenuWithoutUsableDocument(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprintf("load-error=%v", failed), func(t *testing.T) {
			a, tt := newTestApp(t)
			if failed {
				a.doc.loadErr = errors.New("unreadable document")
			} else {
				a.doc = nil
			}
			tt.Frame()
			if err := tt.Click("更多"); err != nil {
				t.Fatal(err)
			}
			if err := tt.Click("导出"); err != nil {
				t.Fatal(err)
			}
			if tt.HasText("导出 Markdown 文件") || tt.HasText("导出 PDF") {
				t.Fatal("export opened without a usable document")
			}
			if err := tt.Click("设置…"); err != nil {
				t.Fatal(err)
			}
			if !a.settingsOpen {
				t.Fatal("settings should remain reachable without a usable document")
			}
		})
	}
}

// TestSettingsSnapshot renders the ⋯ menu and each settings tab, for review.
func TestSettingsSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the settings")
	for _, name := range []string{"more-menu", "more-menu@dark", "more-menu@narrow", "export-menu", tabGeneral, tabAppearance, tabSpaces, tabShortcuts, tabAbout, "about@dark", "about@narrow"} {
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
			settle(tt)
			if tab == "more-menu" || tab == "export-menu" {
				tt.Click("更多")
				if tab == "export-menu" {
					tt.Click("导出")
				}
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
	if menu := tt.Menu(); !slices.Contains(menu, "文字") || !slices.Contains(menu, "段落") || !slices.Contains(menu, "插入") || slices.Contains(menu, "撤销") {
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
