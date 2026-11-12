package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/templates"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func setToday(t *testing.T, at time.Time) {
	old := today
	today = func() time.Time { return at }
	t.Cleanup(func() { today = old })
}

func TestDailyNoteOpensOrMakesToday(t *testing.T) {
	setToday(t, time.Date(2026, 11, 12, 8, 0, 0, 0, time.Local))
	a, tt := newTestApp(t)
	tt.Frame()
	tt.Key(ui.Cmd|ui.Alt, ui.KeyT)
	tt.Frame()
	if a.doc == nil || a.doc.rel != "日记/2026-11-12.arcmd" {
		t.Fatal("daily note not open", a.doc.rel)
	}
	if got := docTitle(a.doc.ed.Doc); got != "2026年11月12日 星期四" {
		t.Fatal("title", got)
	}
	if !tt.HasText("日记") {
		t.Fatal("the daily folder shows in the list")
	}
	// Written today, opened again: the same note, not a second one.
	a.doc.ed.ReplaceText(nil, "今天")
	if !a.flush() {
		t.Fatal("save")
	}
	a.open("Welcome.arcmd")
	a.openDaily()
	tt.Frame()
	entries, _ := os.ReadDir(filepath.Join(root(a), "日记"))
	if a.doc.rel != "日记/2026-11-12.arcmd" || len(entries) != 1 || !strings.Contains(a.doc.ed.Text(), "今天") {
		t.Fatal("opening again opens the same note", len(entries))
	}
}

func TestDailyNoteFromTemplateAndNewFromTemplate(t *testing.T) {
	setToday(t, time.Date(2026, 11, 13, 8, 0, 0, 0, time.Local))
	a, tt := newTestApp(t)
	tt.Frame()
	// The welcome page kept as a template, renamed to the daily one.
	a.saveAsTemplate()
	list := a.spaceTemplates(a.state.ActiveSpace())
	if len(list) != 1 {
		t.Fatal("saved template", list)
	}
	text, _ := vault.Read(root(a), list[0].Path)
	if err := vault.Write(root(a), templates.Dir+"/日记.arcmd", strings.Replace(text, "Welcome to ArcMD", "{{date}} 的记录", 1)); err != nil {
		t.Fatal(err)
	}
	a.openDaily()
	tt.Frame()
	if !strings.Contains(a.doc.ed.Text(), "2026-11-13 的记录") {
		t.Fatal("daily note from the template", a.doc.ed.Text()[:40])
	}
	// From the palette, a template makes a new document in the folder,
	// named for its title once it is open.
	a.openPalette()
	a.palette.query = "从模板新建：日记"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.doc == nil || a.doc.rel != "日记/2026-11-13 的记录.arcmd" || !strings.Contains(a.doc.ed.Text(), "2026-11-13 的记录") {
		t.Fatal("new from template", a.doc.rel)
	}
	if tt.HasText("templates") {
		t.Fatal("the templates folder stays out of the list")
	}
}
