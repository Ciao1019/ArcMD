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

// The row dragged over the top of another lifts onto its floating card while
// a dashed slot opens before that row, and letting go glides it into place.
func TestListReorder(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	a.state.Favorites = nil
	os.WriteFile(filepath.Join(sp.Root, "Alpha.md"), []byte("# A\n"), 0o644)
	a.refreshTree()
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if got := rootNames(a); !slices.Equal(got, []string{"Journal", "Alpha.md", "Welcome.md"}) {
		t.Fatalf("before: %v", got)
	}
	dragTo(a, tt, "Welcome.md", "Alpha.md", 0.2, func() {
		d := a.listDrop
		if d == nil || d.kind != dropInsert || d.parent != "" || d.index != 1 {
			t.Fatalf("drop = %+v", d)
		}
		if !a.dragGrab.active || a.dragGrab.rel != "Welcome.md" {
			t.Fatalf("floating drag card not active: %+v", a.dragGrab)
		}
		var slot *rowFrame
		for i := range a.rows {
			if a.rows[i].slot {
				slot = &a.rows[i]
			}
		}
		if slot == nil || slot.parent != "" || slot.index != 1 || slot.slotSelf {
			t.Fatalf("slot = %+v", slot)
		}
	})
	if got := rootNames(a); !slices.Equal(got, []string{"Journal", "Welcome.md", "Alpha.md"}) {
		t.Fatalf("after: %v", got)
	}
	if a.listDrop != nil {
		t.Fatal("the drop state stayed")
	}
	if !a.dragSettle.active || a.dragSettle.rel != "Welcome.md" {
		t.Fatalf("release did not start settle glide: %+v", a.dragSettle)
	}
	tt.Advance(dragSettleDur + 20*time.Millisecond)
	tt.Frame()
	if a.dragSettle.active {
		t.Fatal("settle glide did not finish")
	}
	// The order is the space's, saved.
	st, err := a.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Spaces[0].Order[""]; !slices.Equal(got, []string{"Journal", "Welcome.md", "Alpha.md"}) {
		t.Fatalf("saved order %v", got)
	}
}

// Over the middle of a folder the folder takes the row, saying so; a closed
// folder held over opens.
func TestListIntoFolder(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	a.open("Journal/Ideas.md")
	a.state.Favorites = []space.Favorite{{Space: sp.ID, Path: "Journal/Ideas.md"}}
	sp.SetExpanded("Journal", false)
	tt.Frame()
	tt.Advance(time.Second) // the rows slide up
	tt.Frame()
	dragTo(a, tt, "Welcome.md", "Journal", 0.5, func() {
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
	if _, err := os.Stat(filepath.Join(sp.Root, "Journal", "Welcome.md")); err != nil {
		t.Fatalf("not moved: %v", err)
	}
	if a.state.IsFavorite(sp.ID, "Journal/Welcome.md") {
		t.Fatalf("pins changed: %v", a.state.Favorites)
	}
}

// Dropping a row next to itself, or a folder into itself, changes nothing.
func TestListDropNoop(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	tt.Frame()
	before := rootNames(a)
	dragTo(a, tt, "Journal", "Journal/Ideas.md", 0.5, func() {
		if a.listDrop != nil {
			t.Errorf("a folder into itself: %+v", a.listDrop)
		}
	})
	if got := rootNames(a); !slices.Equal(got, before) {
		t.Fatalf("changed to %v", got)
	}
}

// Clicking "+ 新建文档" creates a document above the button while the dashed
// button stays lit and glides down smoothly over several frames.
func TestNewDocSlidesButton(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()

	btnY := func() float32 {
		r, ok := tt.Find("新建文档")
		if !ok {
			t.Fatal("missing 新建文档 row")
		}
		return r.Y
	}
	before := btnY()
	if err := tt.Click("新建文档"); err != nil {
		t.Fatal(err)
	}
	if a.newDocPulse.IsZero() {
		t.Fatal("newDocPulse was not armed on click")
	}
	ys := []float32{btnY()}
	for range 16 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		ys = append(ys, btnY())
	}
	var maxStep float32
	for i := 1; i < len(ys); i++ {
		maxStep = max(maxStep, ys[i]-ys[i-1])
	}
	if maxStep > 28 {
		t.Fatalf("新建文档 jumped %v DIPs in one frame: %v", maxStep, ys)
	}
	if total := ys[len(ys)-1] - before; total < 38 || total > 44 {
		t.Fatalf("新建文档 moved %v DIPs, want ~41: %v", total, ys)
	}
}

// Resting the pointer between sidebar root items must NOT pop open an inline
// "+ 新建文档" slot, while the bottom "+ 新建文档" button remains available.
func TestSidebarSeamHoverDoesNotOpenInlineNewDoc(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	sp.SetExpanded("Journal", false)
	os.WriteFile(filepath.Join(sp.Root, "Alpha.md"), []byte("# A\n"), 0o644)
	a.refreshTree()
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()

	var alpha ui.Rect
	for _, r := range a.rows {
		if r.rel == "Alpha.md" {
			alpha = r.r
		}
	}
	tt.Move(alpha.X+40, alpha.Y+alpha.H+float32(rowGap)/2)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if _, ok := tt.Find("在此处新建文档"); ok {
		t.Fatal("unexpected inline new-doc slot opened on seam hover")
	}
	if _, ok := tt.Find("新建文档"); !ok {
		t.Fatal("bottom new-doc button missing")
	}
}

// Dragging across multiple siblings slides the dashed slot monotonically
// without oscillation, Escape cancels back to the original place with a settle
// glide, and ReduceMotion skips spatial settle.
func TestListDragLiveSlotAndEscapeCancel(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	sp.SetExpanded("Journal", false)
	os.WriteFile(filepath.Join(sp.Root, "Alpha.md"), []byte("# A\n"), 0o644)
	os.WriteFile(filepath.Join(sp.Root, "Beta.md"), []byte("# B\n"), 0o644)
	a.refreshTree()
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()

	want := []string{"Journal", "Alpha.md", "Beta.md", "Welcome.md"}
	if got := rootNames(a); !slices.Equal(got, want) {
		t.Fatalf("before: %v", got)
	}
	var start ui.Rect
	for _, r := range a.rows {
		if r.rel == "Welcome.md" {
			start = r.r
		}
	}
	x0, y0 := start.X+30, start.Y+start.H/2
	tt.Press(x0, y0)
	tt.Move(x0, y0-6)
	tt.Advance(240 * time.Millisecond)
	tt.Frame()
	var curY, goalY float32
	for _, r := range a.rows {
		if r.slot {
			curY = r.r.Y + r.r.H/2
		}
		if r.rel == "Alpha.md" {
			goalY = r.r.Y + r.r.H*0.25
		}
	}
	prevIdx := 3
	for i := 1; i <= 12; i++ {
		k := float32(i) / 12
		tt.Move(x0, curY+(goalY-curY)*k)
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		for _, r := range a.rows {
			if r.slot {
				if r.index > prevIdx {
					t.Fatalf("slot oscillated backward from %d to %d on step %d", prevIdx, r.index, i)
				}
				prevIdx = r.index
			}
		}
	}
	// Hold steady while the transition runs to make sure the slot stays locked at index 1.
	for range 8 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		if a.slot.index != 1 || !a.slot.active {
			t.Fatalf("slot drifted while holding pointer still: %+v", a.slot)
		}
	}
	// Escape cancels the drag, keeps original order, and starts settle glide.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if got := rootNames(a); !slices.Equal(got, want) {
		t.Fatalf("after Escape: %v", got)
	}
	if !a.dragSettle.active || a.dragSettle.rel != "Welcome.md" {
		t.Fatalf("Escape did not start settle back: %+v", a.dragSettle)
	}
	tt.Advance(dragSettleDur + 20*time.Millisecond)
	tt.Frame()
	if a.dragSettle.active {
		t.Fatal("Escape settle did not finish")
	}

	// With ReduceMotion on, dropping finishes immediately with no spatial settle.
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	dragTo(a, tt, "Welcome.md", "Alpha.md", 0.2, nil)
	if a.dragSettle.active {
		t.Fatal("ReduceMotion should skip spatial settle")
	}
}

// TestDragSnapshot renders a row dragged between rows, settling into its slot,
// over a folder, over the favorites, and the empty favorites card, for review.
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
			var s ui.Rect
			for _, r := range a.rows {
				if r.rel == from {
					s = r.r
				}
			}
			x0, y0 := s.X+30, s.Y+s.H/2
			tt.Press(x0, y0)
			tt.Move(x0, y0-6)
			tt.Advance(260 * time.Millisecond)
			tt.Frame()
			var g ui.Rect
			for _, r := range a.rows {
				if r.rel == to {
					g = r.r
				}
			}
			if to == "favorites" {
				b, _ := tt.Find("收藏")
				g = ui.Rect{X: b.X + 4, Y: b.Y, W: b.W, H: 47}
			}
			x1, y1 := g.X+38, g.Y+g.H*f
			for i := 1; i <= 6; i++ {
				k := float32(i) / 6
				tt.Move(x0+(x1-x0)*k, (y0-6)+(y1-(y0-6))*k)
				tt.Frame()
			}
			tt.Advance(260 * time.Millisecond)
			tt.Frame()
			writePNG(t, filepath.Join(dir, name+".png"), tt.Image())
			tt.Release(x1, y1)
		})
	}
	hold("drag-between", func(a *App) { a.state.Favorites = nil; a.state.ActiveSpace().SetExpanded("Journal", false) }, "Welcome.md", "Alpha.md", 0.15)
	hold("drag-into", func(a *App) { a.state.ActiveSpace().SetExpanded("Journal", false) }, "Alpha.md", "Journal", 0.5)
	hold("drag-favorites", nil, "Alpha.md", "favorites", 0.5)
	hold("favorites-empty", func(a *App) { a.state.Favorites = nil }, "", "", 0)
}
