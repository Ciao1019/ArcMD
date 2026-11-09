package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestFavoriteLayout(t *testing.T) {
	const w = float32(sidebarWidth) - 20
	for _, c := range []struct{ n, cols int }{{0, 3}, {1, 1}, {2, 2}, {3, 3}, {4, 2}, {5, 3}, {9, 3}} {
		cols, tileW := favoriteLayout(c.n, w)
		if cols != c.cols || tileW < favoriteTileH {
			t.Errorf("%d favorites: %d columns of %.1f, want %d", c.n, cols, tileW, c.cols)
		}
	}
}

// Favorites are every space's: a favorite opens in any space without
// leaving it. Each space keeps what it shows, a favorite of another space
// included: switching away from one does not carry it along, and coming
// back shows it again, as it was.
func TestFavoritesAcrossSpaces(t *testing.T) {
	a, tt := switchApp(t)
	personal, work := a.state.Spaces[0], a.state.Spaces[1]
	os.WriteFile(filepath.Join(personal.Root, "Note.md"), []byte("# Note\n"), 0o644)
	os.WriteFile(filepath.Join(work.Root, "Tasks.md"), []byte("# Tasks\n"), 0o644)
	work.LastOpen = "Tasks.md"
	a.refreshTree()
	a.open("Note.md")
	tt.Frame()

	tt.Key(ui.Ctrl, ui.Key2)
	tt.Frame()
	if active(a) != "Work" || a.doc == nil || a.doc.rel != "Tasks.md" {
		t.Fatalf("in %s with %+v", active(a), a.doc)
	}
	// Welcome, Personal's favorite, opens from its tile in Work.
	r, ok := tt.Find("Welcome")
	if !ok {
		t.Fatal("Work does not show the favorite")
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if active(a) != "Work" || a.doc.space != personal || a.doc.rel != "Welcome.arcmd" {
		t.Fatalf("the tile: in %s with %s", active(a), a.doc.rel)
	}
	if work.LastOpen != "Tasks.md" {
		t.Errorf("a favorite changed Work's last document to %q", work.LastOpen)
	}
	fav := a.doc

	// Personal shows its own document, not the favorite Work had open.
	tt.Key(ui.Ctrl, ui.Key1)
	tt.Frame()
	if active(a) != "Personal" || a.doc == nil || a.doc.rel != "Note.md" {
		t.Fatalf("back in Personal: %+v", a.doc)
	}
	// And Work shows its favorite again, the same editor.
	tt.Key(ui.Ctrl, ui.Key2)
	tt.Frame()
	if active(a) != "Work" || a.doc != fav {
		t.Fatalf("back in Work: %+v", a.doc)
	}

	// A document of the space replaces it.
	a.open("Tasks.md")
	tt.Key(ui.Ctrl, ui.Key3)
	tt.Key(ui.Ctrl, ui.Key2)
	tt.Frame()
	if a.doc == nil || a.doc.rel != "Tasks.md" {
		t.Fatalf("Work after its own document: %+v", a.doc)
	}
	// A favorite removed while away is not shown again.
	r, _ = tt.Find("Welcome")
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Key(ui.Ctrl, ui.Key1)
	a.toggleFavorite(personal, "Welcome.arcmd")
	tt.Key(ui.Ctrl, ui.Key2)
	tt.Frame()
	if a.doc == nil || a.doc.rel != "Tasks.md" {
		t.Fatalf("Work after its favorite went: %+v", a.doc)
	}
}

func TestPinFromTree(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	a.state.Favorites = nil
	tt.Frame()
	if !tt.HasText("拖动以添加收藏") {
		t.Fatalf("no empty card: %q", tt.Texts())
	}
	if err := tt.RightClick("Ideas"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("添加到收藏"); err != nil {
		t.Fatalf("%v: %q", err, tt.Menu())
	}
	tt.Frame()
	if !a.state.IsFavorite(sp.ID, "Journal/Ideas.arcmd") || tt.HasText("拖动以添加收藏") {
		t.Fatalf("favorites %v", a.state.Favorites)
	}
	// Dragging a row onto the tile opens a slot before it and pins it there.
	drag(tt, "Welcome", "收藏", 8, func() {
		if a.favDrop == nil || a.favDrop.slot != 0 {
			t.Errorf("no slot held open: %+v", a.favDrop)
		}
	})
	if fmt.Sprint(a.state.Favorites) != fmt.Sprintf("[{%s Welcome.arcmd} {%s Journal/Ideas.arcmd}]", sp.ID, sp.ID) {
		t.Fatalf("after the drop: %v", a.state.Favorites)
	}
	if a.favDrop != nil {
		t.Fatal("the slot stayed open")
	}
	// And the tile's menu unpins.
	if err := tt.RightClick("Ideas"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("从收藏中移除"); err != nil {
		t.Fatalf("%v: %q", err, tt.Menu())
	}
	if a.state.IsFavorite(sp.ID, "Journal/Ideas.arcmd") {
		t.Fatal("still pinned")
	}
}

// The empty card closes for good with its ×.
func TestFavoritesCardDismiss(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	tt.Frame()
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.state.FavoritesHintDismissed || tt.HasText("拖动以添加收藏") {
		t.Fatal("the card stayed")
	}
}

// drag presses the element labeled from, moves to the element labeled to
// (at dy DIPs down its box) in steps, calls over while there, and lets go.
func drag(tt *ui.Tester, from, to string, dy float32, over func()) {
	f, _ := tt.Find(from)
	g, _ := tt.Find(to)
	x0, y0 := f.X+20, f.Y+f.H/2
	x1, y1 := g.X+20, g.Y+dy
	tt.Press(x0, y0)
	for i := 1; i <= 6; i++ {
		k := float32(i) / 6
		tt.Move(x0+(x1-x0)*k, y0+(y1-y0)*k)
		tt.Frame()
	}
	tt.Frame()
	if over != nil {
		over()
	}
	tt.Release(x1, y1)
	tt.Frame()
}

// TestFavoritesSnapshot renders one, three and five favorites, the open
// one light, for review.
func TestFavoritesSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the favorites")
	for _, n := range []int{1, 3, 5} {
		for _, dark := range []bool{false, true} {
			name := fmt.Sprintf("favorites-%d", n)
			if dark {
				name += "-dark"
			}
			t.Run(name, func(t *testing.T) {
				a, tt := newTestApp(t)
				tt.SetSize(1000, 600)
				tt.SetDark(dark)
				sp := a.state.ActiveSpace()
				for i := 1; i < n; i++ {
					rel := fmt.Sprintf("Note %d.md", i)
					os.WriteFile(filepath.Join(sp.Root, rel), []byte("# x\n"), 0o644)
					a.state.ToggleFavorite(sp.ID, rel)
				}
				a.refreshTree()
				for range 3 {
					tt.Frame()
				}
				writePNG(t, filepath.Join(dir, name+".png"), tt.Image())
			})
		}
	}
}

func hasRow(a *App, rel string) bool {
	for _, r := range a.rows {
		if r.rel == rel {
			return true
		}
	}
	return false
}

// A pinned document is the favorites' alone; unpinned, its row is back.
func TestFavoritesLeaveTheList(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	tt.Frame()
	if hasRow(a, "Welcome.arcmd") {
		t.Fatal("the pinned Welcome has a row")
	}
	a.state.ToggleFavorite(sp.ID, "Welcome.arcmd")
	tt.Frame()
	tt.Frame()
	if !hasRow(a, "Welcome.arcmd") {
		t.Fatal("unpinned, Welcome has no row")
	}
}

// A tile dragged into the list leaves the favorites for where it lands.
func TestFavoriteDraggedToList(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	tt.Frame()
	tile, _ := tt.Find("Welcome")
	var ideas ui.Rect
	for _, r := range a.rows {
		if r.rel == "Journal/Ideas.arcmd" {
			ideas = r.r
		}
	}
	x0, y0 := tile.X+tile.W/2, tile.Y+tile.H/2
	x1, y1 := ideas.X+30, ideas.Y+ideas.H*0.8
	tt.Press(x0, y0)
	for i := 1; i <= 6; i++ {
		k := float32(i) / 6
		tt.Move(x0+(x1-x0)*k, y0+(y1-y0)*k)
		tt.Frame()
	}
	tt.Release(x1, y1)
	tt.Frame()
	if a.state.IsFavorite(sp.ID, "Welcome.arcmd") {
		t.Fatal("still pinned")
	}
	if _, err := os.Stat(filepath.Join(sp.Root, "Journal", "Welcome.arcmd")); err != nil {
		t.Fatalf("not placed in Journal: %v", err)
	}
}

// When a slot adds a row of tiles, the list under them slides down over a
// few frames instead of jumping.
func TestFavoritesSlotSlidesList(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	for _, n := range []string{"A.md", "B.md", "C.md"} {
		os.WriteFile(filepath.Join(sp.Root, n), []byte("# x\n"), 0o644)
	}
	a.state.ToggleFavorite(sp.ID, "A.md")
	a.state.ToggleFavorite(sp.ID, "B.md")
	a.refreshTree()
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	var c ui.Rect
	for _, r := range a.rows {
		if r.rel == "C.md" {
			c = r.r
		}
	}
	top := func() float32 {
		for _, r := range a.rows {
			if r.rel == "Journal" {
				return r.r.Y
			}
		}
		return 0
	}
	before := top()
	tt.Press(c.X+30, c.Y+c.H/2)
	ys := []float32{before}
	frame := func() {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		ys = append(ys, top())
	}
	steps := 8
	for i := 1; i <= steps; i++ {
		k := float32(i) / float32(steps)
		tt.Move(c.X+30, c.Y+c.H/2+(75-c.Y-c.H/2)*k)
		frame()
	}
	for range 20 {
		frame()
	}
	var most float32
	for i := 1; i < len(ys); i++ {
		most = max(most, ys[i]-ys[i-1])
	}
	if most > 30 {
		t.Errorf("the list jumped %v DIPs in a frame: %v", most, ys)
	}
	if moved := ys[len(ys)-1] - before; moved < 50 {
		t.Errorf("the list moved only %v for a new row: %v", moved, ys)
	}
	tt.Release(c.X+30, 75)
}

// TestRenameFavorite renames a favorite, another space's, from its tile's
// menu, in a field under the tile, and then from the command bar.
func TestRenameFavorite(t *testing.T) {
	a, tt := newTestApp(t)
	home := a.state.ActiveSpace()
	work := a.state.Add("Work", t.TempDir())
	a.open("Welcome.arcmd")
	tt.Frame()
	if !a.switchSpaceOnly(work.ID) {
		t.Fatal("did not switch to Work")
	}
	a.openIn(home, "Welcome.arcmd") // from its tile, in Work
	tt.Advance(time.Second)
	if err := tt.RightClick("Welcome"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("重命名…"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	if !tt.Focused("名称") {
		t.Fatal("the favorite's name field has no focus")
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Hello")
	tt.Key(0, ui.KeyEnter)
	tt.Advance(time.Second)
	if _, err := os.Stat(filepath.Join(home.Root, "Hello.arcmd")); err != nil {
		t.Fatal(err)
	}
	if !a.state.IsFavorite(home.ID, "Hello.arcmd") || a.doc.rel != "Hello.arcmd" || a.state.ActiveSpace() != work {
		t.Fatalf("after rename: favorites %v, doc %q", a.state.Favorites, a.doc.rel)
	}
	// The command bar renames the open favorite under its tile too.
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Type("重命名文档")
	tt.Key(0, ui.KeyEnter)
	tt.Advance(time.Second)
	if !tt.Focused("名称") {
		t.Fatal("the command bar's rename shows no field")
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Bye")
	tt.Key(0, ui.KeyEnter)
	tt.Advance(time.Second)
	if _, err := os.Stat(filepath.Join(home.Root, "Bye.arcmd")); err != nil {
		t.Fatal(err)
	}
}
