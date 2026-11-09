package app

import (
	"testing"
)

const ideasRel = "Journal/Ideas.arcmd"

// hoverRow rests the pointer on rel's row in the sidebar.
func hoverRow(t *testing.T, a *App, tt interface {
	Frame()
	Move(x, y float32)
}, rel string) {
	t.Helper()
	tt.Frame()
	for _, r := range a.rows {
		if r.rel == rel {
			tt.Move(r.r.X+r.r.W/2, r.r.Y+r.r.H/2)
			tt.Frame()
			return
		}
	}
	t.Fatalf("no row for %s", rel)
}

// A pinned document shown once stays open, editor and all, while another
// is shown; one outside the pinned section does not.
func TestPinnedDocumentStaysOpen(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	a.togglePin(sp, ideasRel)
	settle(tt)
	start := a.doc.rel
	a.open(ideasRel)
	settle(tt)
	kept := a.doc
	kept.ed.SetSelection(3, 3)
	a.open(start)
	settle(tt)
	if !a.isOpen(sp, ideasRel) || a.isOpen(sp, "missing.arcmd") {
		t.Fatal("the pinned document closed when another was shown")
	}
	if a.isOpen(sp, start) == false {
		t.Fatal("the shown document is open")
	}
	if a.openDocs[place{sp.ID, start}] != nil {
		t.Fatal("a document outside the pinned section is kept")
	}
	a.open(ideasRel)
	settle(tt)
	if a.doc != kept {
		t.Fatal("showing it again opened it anew")
	}
	if _, caret := a.doc.ed.Positions(); caret != 3 {
		t.Fatalf("caret %d, want 3", caret)
	}
	// Unpinned, it is an ordinary document again.
	a.open(start)
	a.togglePin(sp, ideasRel)
	settle(tt)
	if a.isOpen(sp, ideasRel) {
		t.Fatal("an unpinned document stays open")
	}
}

// The minus on an open pinned row closes it and keeps it pinned: in the
// background, the shown document stays; shown, the document before it
// opens.
func TestMinusClosesAndKeepsPinned(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	a.togglePin(sp, ideasRel)
	settle(tt)
	start := a.doc.rel

	hoverRow(t, a, tt, ideasRel)
	if _, ok := tt.Find("关闭并保持置顶"); ok {
		t.Fatal("a closed pinned row offers to close")
	}

	a.open(ideasRel)
	a.open(start)
	settle(tt)
	hoverRow(t, a, tt, ideasRel)
	if err := tt.Click("关闭并保持置顶"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if a.isOpen(sp, ideasRel) || !sp.IsPinned(ideasRel) {
		t.Fatalf("open %v, pinned %v", a.isOpen(sp, ideasRel), sp.IsPinned(ideasRel))
	}
	if a.doc == nil || a.doc.rel != start {
		t.Fatal("closing in the background changed the shown document")
	}

	a.open(ideasRel)
	settle(tt)
	hoverRow(t, a, tt, ideasRel)
	if err := tt.Click("关闭并保持置顶"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if a.isOpen(sp, ideasRel) || a.doc == nil || a.doc.rel != start {
		t.Fatalf("shown %v after closing the shown pinned document", a.doc)
	}
}

// Closing the shown pinned document with nothing open before it shows
// nothing, and the space forgets it as its last document; the command bar
// and the row's menu close it too.
func TestCloseLastOpen(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	a.history, a.at = nil, 0
	a.open(ideasRel)
	a.togglePin(sp, ideasRel)
	settle(tt)
	a.palette.query = "关闭并保持置顶"
	a.palette.open = true
	tt.Frame()
	if err := tt.Click("关闭并保持置顶"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if a.doc != nil || sp.LastOpen != "" {
		t.Fatalf("doc %v, last %q", a.doc, sp.LastOpen)
	}
	if !tt.HasText("搜索或打开文档") {
		t.Fatal("no empty address")
	}

	a.open(ideasRel)
	settle(tt)
	if err := tt.RightClick("Ideas"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("关闭并保持置顶"); err != nil {
		t.Fatalf("%v: %q", err, tt.Menu())
	}
	settle(tt)
	if a.isOpen(sp, ideasRel) {
		t.Fatal("the menu did not close it")
	}
}

// An open pinned document follows a rename.
func TestOpenFollowsRename(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	start := a.doc.rel
	a.togglePin(sp, ideasRel)
	a.open(ideasRel)
	a.open(start)
	settle(tt)
	a.renameIn(sp, ideasRel)
	a.newName = "Plans"
	a.commitRename()
	settle(tt)
	if !a.isOpen(sp, "Journal/Plans.arcmd") {
		t.Fatalf("open %v", a.openDocs)
	}
}
