package app

import (
	"testing"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestDirtyCacheTracksTextStructureAndHistory(t *testing.T) {
	a, tt := newTestApp(t)
	if err := vault.Write(root(a), "Cache.md", "first\n\nsecond\n"); err != nil {
		t.Fatal(err)
	}
	a.open("Cache.md")
	d := a.doc
	d.ed.RequestFocus = true
	tt.Frame()
	check := func(want bool) {
		t.Helper()
		if got := d.dirty(); got != want || got != d.changed() {
			t.Fatalf("dirty = %v, full comparison = %v, want %v", got, d.changed(), want)
		}
	}
	check(false)
	d.ed.SetSelection(5, 5)
	tt.Type("😀")
	check(true)
	d.ed.SetSelection(d.ed.Len(), d.ed.Len())
	tt.Type("中文")
	check(true)
	tt.Key(ui.Cmd, ui.KeyZ)
	check(true)
	tt.Key(ui.Cmd, ui.KeyZ)
	check(false)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	check(true)
	// A text replacement can return exactly to the baseline without undoing.
	d.ed.ReplaceText(&ui.TextInputRange{Start: 5, End: 7}, "")
	check(false)
	d.ed.SetSelection(0, 5)
	tt.Key(ui.Cmd, ui.KeyB)
	check(true)
	d.ed.SetSelection(d.ed.Len(), d.ed.Len())
	tt.Type("tail")
	check(true)
	tt.Key(ui.Cmd, ui.KeyZ)
	check(true)
	tt.Key(ui.Cmd, ui.KeyZ)
	check(false)
	d.ed.Engine.Insert(model.Table(2, 2))
	tt.Frame()
	check(true)
	tt.Key(ui.Cmd, ui.KeyZ)
	check(false)
	// Skipping a revision must force a full comparison, then recover when clean.
	d.ed.ReplaceText(nil, "a")
	d.ed.ReplaceText(nil, "b")
	check(true)
	d.ed.Undo()
	d.ed.Undo()
	check(false)
}

func TestSaveDuringCompositionRefreshesCommittedBaseline(t *testing.T) {
	a, tt := newTestApp(t)
	d := a.doc
	d.ed.SetSelection(d.ed.Len(), d.ed.Len())
	d.ed.RequestFocus = true
	tt.Frame()
	tt.Type(" committed")
	committed := d.ed.Text()
	tt.Compose("候选", 2)
	if !d.dirty() {
		t.Fatal("committed edit lost dirty state during composition")
	}
	if err := d.save(); err != nil {
		t.Fatal(err)
	}
	stored, err := vault.Read(root(a), d.rel)
	if err != nil {
		t.Fatal(err)
	}
	native, err := model.Decode(stored)
	if err != nil || native.PlainText() != committed {
		t.Fatal("save included preedit", err)
	}
	if d.dirty() || d.changed() {
		t.Fatal("saved committed baseline is dirty")
	}
	tt.Compose("新候选", 3)
	if d.dirty() || d.changed() {
		t.Fatal("preedit dirtied committed baseline")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if d.ed.Text() != committed || d.dirty() {
		t.Fatal("cancel composition changed saved content")
	}
	tt.Compose("新候选", 3)
	tt.Type("中文")
	if !d.dirty() || !d.changed() {
		t.Fatal("committing composition lost dirty state")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if d.ed.Text() != committed || d.dirty() {
		t.Fatal("undo composition did not restore saved baseline")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	if !d.dirty() {
		t.Fatal("redo composition lost dirty state")
	}
}
