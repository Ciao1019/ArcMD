package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// sectionOf is where rel's row is: "pinned", "today" or "".
func sectionOf(a *App, rel string) string {
	for _, r := range a.rows {
		if r.rel == rel {
			if a.pinnedSection(rel) {
				return "pinned"
			}
			return "today"
		}
	}
	return ""
}

func settle(tt interface {
	Frame()
	Advance(time.Duration)
}) {
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
}

// Pinning is the space's: a pinned document shows above the line in its
// space only, and the menu unpins it.
func TestPinFromMenu(t *testing.T) {
	a, tt := switchApp(t)
	a.state.Favorites = nil
	settle(tt)
	if err := tt.RightClick("Ideas"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("置顶"); err != nil {
		t.Fatalf("%v: %q", err, tt.Menu())
	}
	settle(tt)
	sp := a.state.ActiveSpace()
	if !sp.IsPinned("Journal/Ideas.arcmd") || sectionOf(a, "Journal/Ideas.arcmd") != "pinned" {
		t.Fatalf("pins %v, section %q", sp.Pins, sectionOf(a, "Journal/Ideas.arcmd"))
	}
	if a.dividerY == 0 {
		t.Error("no line")
	}
	st, _ := a.store.Load()
	if !slices.Equal(st.Spaces[0].Pins, []string{"Journal/Ideas.arcmd"}) {
		t.Fatalf("saved pins %v", st.Spaces[0].Pins)
	}
	if err := tt.RightClick("Ideas"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("取消置顶"); err != nil {
		t.Fatalf("%v: %q", err, tt.Menu())
	}
	settle(tt)
	if sp.IsPinned("Journal/Ideas.arcmd") || sectionOf(a, "Journal/Ideas.arcmd") != "today" {
		t.Fatal("still pinned")
	}
}

// A row dragged above the line is pinned there, with the empty section's
// placeholder taking it; dragged back below, it is unpinned where it lands.
func TestPinByDrag(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	os.WriteFile(filepath.Join(sp.Root, "Alpha.md"), []byte("# A\n"), 0o644)
	a.refreshTree()
	settle(tt)
	dragToPlaceholder := func() {
		var s = a.rows[0].r
		for _, r := range a.rows {
			if r.rel == "Alpha.md" {
				s = r.r
			}
		}
		x0, y0 := s.X+20, s.Y+s.H/2
		tt.Press(x0, y0)
		tt.Move(x0, y0+6)
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		tt.Frame()
		var ph *rowFrame
		for i := range a.rows {
			if a.rows[i].placeholder {
				ph = &a.rows[i]
			}
		}
		if ph == nil {
			t.Fatal("no placeholder while dragging")
		}
		x1, y1 := ph.r.X+20, ph.r.Y+ph.r.H/2
		for i := 1; i <= 6; i++ {
			k := float32(i) / 6
			tt.Move(x0+(x1-x0)*k, y0+(y1-y0)*k)
			tt.Frame()
		}
		if d := a.listDrop; d == nil || d.parent != pinRoot {
			t.Fatalf("drop = %+v", a.listDrop)
		}
		if a.dropSection(a.listDrop) != sectionPinned {
			t.Error("the pinned section does not light up")
		}
		tt.Release(x1, y1)
		tt.Frame()
	}
	dragToPlaceholder()
	if !slices.Equal(sp.Pins, []string{"Alpha.md"}) {
		t.Fatalf("pins %v", sp.Pins)
	}
	settle(tt)
	// Back below the line, after Welcome.
	dragTo(a, tt, "Alpha.md", "Welcome.arcmd", 0.8, nil)
	if len(sp.Pins) != 0 {
		t.Fatalf("still pinned: %v", sp.Pins)
	}
	if got := rootNames(a); !slices.Equal(got, []string{"Journal", "Welcome.arcmd", "Alpha.md"}) {
		t.Fatalf("today %v", got)
	}
}

// A pinned folder shows above the line with what it holds.
func TestPinFolder(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	a.togglePin(sp, "Journal")
	settle(tt)
	if sectionOf(a, "Journal") != "pinned" || sectionOf(a, "Journal/Ideas.arcmd") != "pinned" {
		t.Fatalf("Journal %q, Ideas %q", sectionOf(a, "Journal"), sectionOf(a, "Journal/Ideas.arcmd"))
	}
	if sectionOf(a, "Welcome.arcmd") != "today" {
		t.Fatal("Welcome left the rest")
	}
}

// One item is pinned or a favorite, never both.
func TestPinOrFavorite(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	settle(tt)
	if !a.state.IsFavorite(sp.ID, "Welcome.arcmd") {
		t.Fatal("Welcome is not a favorite on the first run")
	}
	a.togglePin(sp, "Welcome.arcmd")
	if a.state.IsFavorite(sp.ID, "Welcome.arcmd") || !sp.IsPinned("Welcome.arcmd") {
		t.Fatalf("pin: favorites %v pins %v", a.state.Favorites, sp.Pins)
	}
	a.toggleFavorite(sp, "Welcome.arcmd")
	if !a.state.IsFavorite(sp.ID, "Welcome.arcmd") || sp.IsPinned("Welcome.arcmd") {
		t.Fatalf("favorite: favorites %v pins %v", a.state.Favorites, sp.Pins)
	}
}

// The list's empty space makes folders and documents.
func TestListMenu(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	settle(tt)
	tt.RightClickAt(100, 480)
	if !slices.Contains(tt.Menu(), "新建文件夹") || !slices.Contains(tt.Menu(), "更改空间图标…") {
		t.Fatalf("menu %q", tt.Menu())
	}
	if err := tt.ChooseMenuItem("新建文件夹"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, err := os.Stat(filepath.Join(sp.Root, "新建文件夹")); err != nil || a.renaming != "新建文件夹" {
		t.Fatalf("folder: %v, renaming %q", err, a.renaming)
	}
}

// "新建包含此文档的文件夹" puts the document in a new folder to name.
func TestNewFolderWithDoc(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	settle(tt)
	a.newFolderWith("Welcome.arcmd")
	tt.Frame()
	if _, err := os.Stat(filepath.Join(sp.Root, "新建文件夹", "Welcome.arcmd")); err != nil || a.renaming != "新建文件夹" {
		t.Fatalf("moved: %v, renaming %q", err, a.renaming)
	}
}

// TestPinsSnapshot renders a space with pinned documents and a folder over
// the line, in light and dark, and the placeholder of an empty pinned
// section under a drag, for review.
func TestPinsSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the pinned section")
	for _, dark := range []bool{false, true} {
		name := "pins"
		if dark {
			name += "-dark"
		}
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetSize(900, 600)
			tt.SetDark(dark)
			sp := a.state.ActiveSpace()
			for _, n := range []string{"Roadmap.md", "周报.md", "Notes/Draft.md"} {
				p := filepath.Join(sp.Root, n)
				os.MkdirAll(filepath.Dir(p), 0o755)
				os.WriteFile(p, []byte("# x\n"), 0o644)
			}
			a.refreshTree()
			a.togglePin(sp, "Roadmap.md")
			a.togglePin(sp, "Journal")
			settle(tt)
			writePNG(t, filepath.Join(dir, name+".png"), tt.Image())
		})
	}
	t.Run("pins-placeholder", func(t *testing.T) {
		a, tt := newTestApp(t)
		tt.SetSize(900, 600)
		settle(tt)
		var s = a.rows[0].r
		tt.Press(s.X+20, s.Y+s.H/2)
		tt.Move(s.X+20, s.Y+s.H/2+8)
		settle(tt)
		writePNG(t, filepath.Join(dir, "pins-placeholder.png"), tt.Image())
		tt.Release(s.X+20, s.Y+s.H/2+8)
	})
}

// A click on the space header's icon folds the pinned section away but for the
// pin holding the open document, and unfolds it.
func TestHeaderFoldsPins(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	os.WriteFile(filepath.Join(sp.Root, "Alpha.md"), []byte("# A\n"), 0o644)
	a.refreshTree()
	a.togglePin(sp, "Welcome.arcmd")
	a.togglePin(sp, "Alpha.md")
	settle(tt)
	if err := tt.Click("折叠或展开置顶"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if !sp.PinsCollapsed {
		t.Fatal("the click did not fold the pinned section")
	}
	if sectionOf(a, "Alpha.md") != "" || sectionOf(a, "Welcome.arcmd") != "pinned" {
		t.Fatalf("folded: Alpha %q, Welcome %q", sectionOf(a, "Alpha.md"), sectionOf(a, "Welcome.arcmd"))
	}
	if err := tt.Click("折叠或展开置顶"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if sp.PinsCollapsed || sectionOf(a, "Alpha.md") != "pinned" {
		t.Fatal("the second click did not unfold it")
	}
}

// A closed folder keeps the open document in sight, one level in.
func TestFolderPeek(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	a.open("Journal/Ideas.arcmd")
	sp.SetExpanded("Journal", false)
	settle(tt)
	var depth = -1
	for _, r := range a.rows {
		if r.rel == "Journal/Ideas.arcmd" {
			depth = r.depth
		}
	}
	if depth != 1 {
		t.Fatalf("the open document under the closed folder: depth %d", depth)
	}
	a.open("Welcome.arcmd")
	settle(tt)
	if sectionOf(a, "Journal/Ideas.arcmd") != "" {
		t.Fatal("the peek stayed after another document opened")
	}
}

// The folder glyph turns open over a quarter second.
func TestFolderGlyphTurns(t *testing.T) {
	x0, y0 := panelPoint(16, 0, 12, -30, 0)
	x1, y1 := panelPoint(16, 0, 12, -30, 1)
	if x0 != 16 || y0 != 0 {
		t.Fatalf("closed corner at %v,%v", x0, y0)
	}
	if !(y1 > 0.5 && x1 > 16) {
		t.Fatalf("the open front's top corner at %v,%v: it should fall forward, to the right", x1, y1)
	}
}

// TestHeaderSnapshot renders the header under the pointer and the pinned
// section folded, with a closed folder keeping its open document in sight.
func TestHeaderSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the header")
	a, tt := newTestApp(t)
	tt.SetSize(900, 520)
	sp := a.state.ActiveSpace()
	a.state.Favorites = nil
	os.WriteFile(filepath.Join(sp.Root, "Roadmap.md"), []byte("# R\n"), 0o644)
	a.refreshTree()
	a.togglePin(sp, "Roadmap.md")
	a.open("Journal/Ideas.arcmd")
	sp.SetExpanded("Journal", false)
	settle(tt)
	h, _ := tt.Find("Personal")
	tt.Move(h.X+h.W/2, h.Y+h.H/2)
	settle(tt)
	writePNG(t, filepath.Join(dir, "header-hover.png"), tt.Image())
	tt.Click("折叠或展开置顶")
	settle(tt)
	writePNG(t, filepath.Join(dir, "header-folded.png"), tt.Image())
}

// Folding the pinned section, the rows under it slide up over a few frames,
// as Cra's list moves them, rather than jumping.
func TestFoldSlides(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	os.WriteFile(filepath.Join(sp.Root, "Alpha.md"), []byte("# A\n"), 0o644)
	a.refreshTree()
	a.togglePin(sp, "Alpha.md")
	settle(tt)
	before, _ := tt.Find("Welcome")
	if err := tt.Click("折叠或展开置顶"); err != nil {
		t.Fatal(err)
	}
	ys := []float32{before.Y}
	for range 12 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		w, _ := tt.Find("Welcome")
		ys = append(ys, w.Y)
	}
	var most float32
	for i := 1; i < len(ys); i++ {
		most = max(most, ys[i-1]-ys[i])
	}
	if moved := ys[0] - ys[len(ys)-1]; moved < 30 || most > moved*0.8 {
		t.Fatalf("the rows jumped instead of sliding: %v", ys)
	}
}
