package app

import (
	"github.com/Ciao1019/ArcMD/internal/vault"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo/ui"
)

// exists reports whether rel is a file or folder of sp's folder.
func exists(sp *space.Space, rel string) bool {
	_, err := os.Stat(filepath.Join(sp.Root, filepath.FromSlash(rel)))
	return err == nil
}

// pressRow presses the row of rel and starts dragging it.
func pressRow(t *testing.T, a *App, tt *ui.Tester, rel string) (float32, float32) {
	t.Helper()
	tt.Frame()
	for _, r := range a.rows {
		if r.rel == rel {
			x, y := r.r.X+20, r.r.Y+r.r.H/2
			tt.Press(x, y)
			tt.Move(x, y+6)
			tt.Frame()
			return x, y + 6
		}
	}
	t.Fatalf("no row %s", rel)
	return 0, 0
}

// glide moves the pointer from (x0, y0) to (x1, y1) in steps.
func glide(tt *ui.Tester, x0, y0, x1, y1 float32) {
	for i := 1; i <= 6; i++ {
		k := float32(i) / 6
		tt.Move(x0+(x1-x0)*k, y0+(y1-y0)*k)
		tt.Frame()
	}
}

func TestMoveToSpaceMenu(t *testing.T) {
	a, tt := newTestApp(t)
	home := a.state.ActiveSpace()
	work := a.state.Add("Work", t.TempDir())
	tt.Frame()
	tt.Advance(time.Second)
	if err := tt.RightClick("Ideas"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("移到空间", work.Icon+" Work"); err != nil {
		t.Fatalf("%v; menu %q", err, tt.Menu())
	}
	tt.Frame()
	if exists(home, "Journal/Ideas.arcmd") || !exists(work, "Ideas.arcmd") {
		t.Fatal("the document did not move to Work")
	}
	// The undo toast names the document; the list must not.
	if a.state.ActiveSpace() != home || vault.Find(a.tree, "Journal/Ideas.arcmd") != nil || !tt.HasText("已移到空间「Work」") {
		t.Fatal("moving a closed document switched spaces or kept its row")
	}
}

func TestMoveOpenDocumentFollows(t *testing.T) {
	a, tt := newTestApp(t)
	work := a.state.Add("Work", t.TempDir())
	os.WriteFile(filepath.Join(work.Root, "Ideas.arcmd"), []byte("taken"), 0o644)
	a.open("Journal/Ideas.arcmd")
	tt.Advance(time.Second)
	a.history = append(a.history, place{a.state.Spaces[0].ID, "Journal/Ideas.arcmd"})
	if a.moveAcross(a.state.Spaces[0].ID, "Journal", work.ID, "") != "Journal" {
		t.Fatal("the folder did not move")
	}
	tt.Advance(time.Second)
	if a.state.ActiveSpace() != work || a.doc.space != work || a.doc.rel != "Journal/Ideas.arcmd" {
		t.Fatalf("the window did not follow the open document: %q in %s", a.doc.rel, a.state.ActiveSpace().Name)
	}
	if !work.IsExpanded("Journal") || a.history[len(a.history)-1].space != work.ID {
		t.Error("the folder's state or the history stayed behind")
	}
	if !tt.HasText("Ideas") {
		t.Errorf("Work does not list the moved folder: %q", tt.Texts())
	}
}

func TestDragOntoSpaceIcon(t *testing.T) {
	a, tt := newTestApp(t)
	home := a.state.ActiveSpace()
	work := a.state.Add("Work", t.TempDir())
	tt.Frame()
	tt.Advance(time.Second)
	icon, ok := tt.Find("切换到 Work")
	if !ok {
		t.Fatal("no Work icon")
	}
	x, y := pressRow(t, a, tt, "Journal/Ideas.arcmd")
	glide(tt, x, y, icon.X+icon.W/2, icon.Y+icon.H/2)
	if a.spaceSpring.id != work.ID {
		t.Fatal("the icon does not take the drag")
	}
	tt.Release(icon.X+icon.W/2, icon.Y+icon.H/2)
	tt.Frame()
	if !exists(work, "Ideas.arcmd") || exists(home, "Journal/Ideas.arcmd") || a.state.ActiveSpace() != home {
		t.Fatal("letting go on the icon did not move the document there")
	}
}

func TestDragHoldsOnSpaceIconThenDrops(t *testing.T) {
	a, tt := newTestApp(t)
	home := a.state.ActiveSpace()
	work := a.state.Add("Work", t.TempDir())
	os.MkdirAll(filepath.Join(work.Root, "Inbox"), 0o755)
	tt.Frame()
	tt.Advance(time.Second)
	icon, _ := tt.Find("切换到 Work")
	x, y := pressRow(t, a, tt, "Journal/Ideas.arcmd")
	ix, iy := icon.X+icon.W/2, icon.Y+icon.H/2
	glide(tt, x, y, ix, iy)
	tt.Advance(spaceSpringDelay + 100*time.Millisecond)
	tt.Frame()
	if a.state.ActiveSpace() != work {
		t.Fatal("resting on the icon did not open Work")
	}
	tt.Advance(time.Second) // the pager settles
	tt.Frame()
	row := func() ui.Rect {
		for _, r := range a.rows {
			if r.rel == "Inbox" {
				return r.r
			}
		}
		t.Fatalf("Work's list has no Inbox row: %+v", a.rows)
		return ui.Rect{}
	}
	// Into the list first: its pinned placeholder opens and moves the rows.
	inbox := row()
	glide(tt, ix, iy, inbox.X+40, inbox.Y+inbox.H/2)
	tt.Advance(time.Second)
	tt.Frame()
	inbox = row()
	tt.Move(inbox.X+40, inbox.Y+inbox.H/2)
	tt.Frame()
	if a.carried == nil || a.carried.rel != "Journal/Ideas.arcmd" {
		t.Fatal("the drag from Personal is not carried into Work")
	}
	if d := a.listDrop; d == nil || d.kind != dropInto || d.parent != "Inbox" {
		t.Fatalf("drop over Inbox = %+v", a.listDrop)
	}
	tt.Release(inbox.X+40, inbox.Y+inbox.H/2)
	tt.Frame()
	if !exists(work, "Inbox/Ideas.arcmd") || exists(home, "Journal/Ideas.arcmd") {
		t.Fatal("the document did not land in Work's Inbox")
	}
	tt.Frame()
	if a.carried != nil {
		t.Error("the carried row outlived the drag")
	}
}
