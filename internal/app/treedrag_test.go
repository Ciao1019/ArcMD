package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo/ui"
)

// rootNames is the active space's root as the sidebar shows it.
func rootNames(a *App) []string {
	var out []string
	for _, n := range a.tree {
		out = append(out, n.Name)
	}
	return out
}

// dragTo presses the list's row of from, starts dragging it, then moves
// over the row of to at the fraction f of its height, where it is once the
// drag opened the pinned section's placeholder, runs over there, and lets
// go.
func dragTo(a *App, tt *ui.Tester, from, to string, f float32, over func()) {
	row := func(rel string) ui.Rect {
		for _, r := range a.rows {
			if r.rel == rel {
				return r.r
			}
		}
		panic("no row " + rel)
	}
	s := row(from)
	x0, y0 := s.X+20, s.Y+s.H/2
	tt.Press(x0, y0)
	tt.Move(x0, y0+6)
	tt.Frame()
	tt.Advance(time.Second) // the rows make room for the placeholder
	tt.Frame()
	tt.Frame()
	g := row(to)
	x1, y1 := g.X+20, g.Y+g.H*f
	for i := 1; i <= 6; i++ {
		k := float32(i) / 6
		tt.Move(x0+(x1-x0)*k, y0+6+(y1-y0-6)*k)
		tt.Frame()
	}
	tt.Frame()
	if over != nil {
		over()
	}
	tt.Release(x1, y1)
	tt.Frame()
}

// The row dragged over the top of another goes before it: a line shows
// where, the rows either side ease apart, and the order is kept.
func TestListReorder(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	a.state.Favorites = nil
	os.WriteFile(filepath.Join(sp.Root, "Alpha.md"), []byte("# A\n"), 0o644)
	a.refreshTree()
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if got := rootNames(a); !slices.Equal(got, []string{"Journal", "Alpha.md", "Welcome.arcmd"}) {
		t.Fatalf("before: %v", got)
	}
	dragTo(a, tt, "Welcome.arcmd", "Alpha.md", 0.2, func() {
		d := a.listDrop
		if d == nil || d.kind != dropInsert || d.parent != "" || d.index != 1 || !d.line {
			t.Fatalf("drop = %+v", d)
		}
		if d.above != "Journal/Ideas.arcmd" || d.below != "Alpha.md" {
			t.Errorf("nudged %q / %q", d.above, d.below)
		}
	})
	if got := rootNames(a); !slices.Equal(got, []string{"Journal", "Welcome.arcmd", "Alpha.md"}) {
		t.Fatalf("after: %v", got)
	}
	if a.listDrop != nil {
		t.Fatal("the line stayed")
	}
	// The order is the space's, saved.
	st, err := a.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Spaces[0].Order[""]; !slices.Equal(got, []string{"Journal", "Welcome.arcmd", "Alpha.md"}) {
		t.Fatalf("saved order %v", got)
	}
}

// Over the middle of a folder the folder takes the row, saying so; a closed
// folder held over opens.
func TestListIntoFolder(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	a.open("Journal/Ideas.arcmd")
	a.state.Favorites = []space.Favorite{{Space: sp.ID, Path: "Journal/Ideas.arcmd"}}
	sp.SetExpanded("Journal", false)
	tt.Frame()
	tt.Advance(time.Second) // the rows slide up
	tt.Frame()
	dragTo(a, tt, "Welcome.arcmd", "Journal", 0.5, func() {
		d := a.listDrop
		if d == nil || d.kind != dropInto || d.parent != "Journal" {
			t.Fatalf("drop = %+v", d)
		}
		if !tt.HasText("移到文件夹") {
			t.Error("no badge")
		}
		tt.Advance(springDelay + time.Millisecond)
		tt.Frame()
		if !sp.IsExpanded("Journal") {
			t.Error("the folder held over did not open")
		}
	})
	if _, err := os.Stat(filepath.Join(sp.Root, "Journal", "Welcome.arcmd")); err != nil {
		t.Fatalf("not moved: %v", err)
	}
	if a.state.IsFavorite(sp.ID, "Journal/Welcome.arcmd") {
		t.Fatalf("pins changed: %v", a.state.Favorites)
	}
}

// Dropping a row next to itself, or a folder into itself, changes nothing.
func TestListDropNoop(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	tt.Frame()
	before := rootNames(a)
	dragTo(a, tt, "Journal", "Journal/Ideas.arcmd", 0.5, func() {
		if a.listDrop != nil {
			t.Errorf("a folder into itself: %+v", a.listDrop)
		}
	})
	if got := rootNames(a); !slices.Equal(got, before) {
		t.Fatalf("changed to %v", got)
	}
}

// TestDragSnapshot renders a row dragged between rows, over a folder and
// over the favorites, and the empty favorites card, for review.
func TestDragSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the drags")
	hold := func(name string, setup func(a *App), from, to string, f float32) {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetSize(900, 560)
			sp := a.state.ActiveSpace()
			os.WriteFile(filepath.Join(sp.Root, "Alpha.md"), []byte("# A\n"), 0o644)
			os.WriteFile(filepath.Join(sp.Root, "Roadmap.md"), []byte("# R\n"), 0o644)
			a.state.ToggleFavorite(sp.ID, "Roadmap.md")
			if setup != nil {
				setup(a)
			}
			a.refreshTree()
			tt.Frame()
			tt.Advance(time.Second)
			tt.Frame()
			if from == "" {
				writePNG(t, filepath.Join(dir, name+".png"), tt.Image())
				return
			}
			var s, g ui.Rect
			for _, r := range a.rows {
				if r.rel == from {
					s = r.r
				}
				if r.rel == to {
					g = r.r
				}
			}
			if to == "favorites" {
				b, _ := tt.Find("收藏")
				g = ui.Rect{X: b.X + 4, Y: b.Y, W: b.W, H: 47}
			}
			x0, y0 := s.X+30, s.Y+s.H/2
			x1, y1 := g.X+30, g.Y+g.H*f
			tt.Press(x0, y0)
			for i := 1; i <= 6; i++ {
				k := float32(i) / 6
				tt.Move(x0+(x1-x0)*k, y0+(y1-y0)*k)
				tt.Frame()
			}
			tt.Advance(time.Second)
			tt.Frame()
			writePNG(t, filepath.Join(dir, name+".png"), tt.Image())
			tt.Release(x1, y1)
		})
	}
	hold("drag-between", nil, "Welcome.arcmd", "Alpha.md", 0.15)
	hold("drag-into", func(a *App) { a.state.ActiveSpace().SetExpanded("Journal", false) }, "Alpha.md", "Journal", 0.5)
	hold("drag-favorites", nil, "Alpha.md", "favorites", 0.5)
	hold("favorites-empty", func(a *App) { a.state.Favorites = nil }, "", "", 0)
}
