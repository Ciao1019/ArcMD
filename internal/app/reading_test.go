package app

import (
	"testing"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/prefs"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestReadingSettingsAndToolbarLock(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	a.openSettings(tabAppearance)
	scrollSettingsTo(t, tt, "增大正文字号")
	if err := tt.Click("增大正文字号"); err != nil {
		t.Fatal(err)
	}
	if a.prefs.ReadingSize() != 17 {
		t.Fatal("font size")
	}
	scrollSettingsTo(t, tt, "增大行距")
	if err := tt.Click("增大行距"); err != nil {
		t.Fatal(err)
	}
	// Verify scrolling and clicking still work when the settings viewport is shorter.
	tt.SetSize(960, 520)
	scrollSettingsTo(t, tt, "减小正文字号")
	if err := tt.Click("减小正文字号"); err != nil {
		t.Fatal(err)
	}
	scrollSettingsTo(t, tt, "增大正文字号")
	if err := tt.Click("增大正文字号"); err != nil {
		t.Fatal(err)
	}
	tt.SetSize(1200, 760)
	p, err := (prefs.Store{Path: a.prefStore.Path}).Load()
	if err != nil || p.FontSize != 17 || p.LineHeight != a.prefs.LineHeight {
		t.Fatal("preferences not saved", p, err)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if err := tt.Click("编辑中，点击设为只读"); err != nil {
		t.Fatal(err)
	}
	if !a.doc.ed.ReadOnly {
		t.Fatal("not locked")
	}
	if err := tt.Click("只读，点击恢复编辑"); err != nil {
		t.Fatal(err)
	}
	if a.doc.ed.ReadOnly {
		t.Fatal("not unlocked")
	}
}

func TestWikiPreviewUsesUnsavedTarget(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	doc := editor.ImportMarkdown("# Target\n\nSaved text")
	data, _ := doc.Encode()
	if err := vault.Write(root(a), "Target.arcmd", data); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	from := a.doc
	a.open("Target.arcmd")
	a.doc.ed.ReplaceText(nil, "Unsaved ")
	p := a.previewWikiFor(from, "Target")
	if p.Missing || p.Text == "" || p.Title != "Target" {
		t.Fatal(p)
	}
	if p.Text[:8] != "Unsaved " {
		t.Fatal("preview missed live text", p.Text)
	}
	missing := a.previewWikiFor(from, "Does not exist")
	if !missing.Missing {
		t.Fatal("missing state")
	}
}
