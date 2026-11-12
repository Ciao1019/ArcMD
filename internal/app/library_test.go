package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo/ui"
)

// libraryApp is a test app with two more spaces, each holding documents.
func libraryApp(t *testing.T) (*App, *ui.Tester) {
	a, tt := newTestApp(t)
	for i, name := range []string{"Work", "Travel"} {
		root := t.TempDir()
		for _, f := range []string{name + " Plan.md", "Notes/" + name + " Ideas.md"} {
			p := filepath.Join(root, f)
			os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, []byte("# "+name+"\n\nhello\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		s := a.state.Add(name, root)
		s.SetTheme(space.NewTheme(i + 1))
		s.SetIcon([]string{"💼", "✈️"}[i], "")
		s.SetExpanded("Notes", true)
	}
	tt.Frame()
	return a, tt
}

// The button at the sidebar's foot opens the library on its spaces: a
// card for each, and a document opened from one opens in its space and
// closes the library. ⌃⌘S, ⇧⌘L and Escape open and close it.
func TestLibrarySpaces(t *testing.T) {
	a, tt := libraryApp(t)
	if err := tt.Click("资料库"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if !a.library.open || a.library.page != libSpaces {
		t.Fatal("the button did not open the library on its spaces")
	}
	for _, name := range []string{"Personal", "Work", "Travel"} {
		if _, ok := tt.Find("打开空间 " + name); !ok {
			t.Fatalf("no card for %s: %q", name, tt.Texts())
		}
	}
	if w := a.libraryWidth(1200); w <= libraryRail+libraryList {
		t.Fatalf("the spaces page is %v wide; the cards need more", w)
	}
	work := a.state.Spaces[1]
	r, ok := tt.Find("Work Ideas")
	if !ok {
		t.Fatalf("no Ideas row: %q", tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if a.library.open || a.state.ActiveSpace() != work || a.doc == nil || a.doc.rel != "Notes/Work Ideas.md" {
		t.Fatalf("open %v, space %v, doc %+v", a.library.open, a.state.ActiveSpace().Name, a.doc)
	}

	tt.Key(ui.Ctrl|ui.Cmd, ui.KeyS)
	tt.Frame()
	if !a.library.open || a.library.page != libSpaces {
		t.Fatal("⌃⌘S did not open the spaces")
	}
	tt.Move(900, 700) // off the cards: a tooltip would take the first Escape
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.library.open {
		t.Fatal("Escape kept the library open")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyL)
	settle(tt)
	if !a.library.open {
		t.Fatal("⇧⌘L did not open the library")
	}
	if err := tt.Click("退出资料库"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.library.open {
		t.Fatal("the back arrow kept the library open")
	}
}

// The rail switches pages, and the library reopens on the page last
// shown; the plugins page says the market is coming; it opens over a
// folded sidebar.
func TestLibraryPages(t *testing.T) {
	a, tt := libraryApp(t)
	a.toggleSidebar()
	settle(tt)
	a.toggleLibrary()
	settle(tt)
	if _, ok := tt.Find("最近删除"); !ok {
		t.Fatal("the folded sidebar hides the library")
	}
	if err := tt.Click("插件"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if a.library.page != libPlugins || !tt.HasText("插件市场即将开放") {
		t.Fatalf("page %v: %q", a.library.page, tt.Texts())
	}
	if err := tt.Click("最近删除"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.library.page != libTrash || !tt.HasText("这里什么都没有") {
		t.Fatalf("page %v: %q", a.library.page, tt.Texts())
	}
	a.toggleLibrary()
	a.toggleLibrary()
	tt.Frame()
	if a.library.page != libTrash {
		t.Fatal("the library did not reopen on the page last shown")
	}
}

// The recently deleted page groups by day, filters by the search, and
// forgets ArcMD's copy with the bin.
func TestLibraryTrash(t *testing.T) {
	a, tt := newTestApp(t)
	a.trashFile = os.RemoveAll
	a.trash("Journal/Ideas.arcmd")
	a.trash("Welcome.arcmd")
	a.undos = nil // the toasts name the documents, as the page does
	a.openTrash()
	settle(tt)
	if !tt.HasText("今天") || !tt.HasText("Ideas") || !tt.HasText("Welcome") {
		t.Fatalf("texts %q", tt.Texts())
	}
	a.library.query = "idea"
	tt.Frame()
	if tt.HasText("Welcome") || !tt.HasText("Ideas") {
		t.Fatal("the search did not filter")
	}
	a.library.query = "zzz"
	tt.Frame()
	if !tt.HasText("没有符合搜索的项目") {
		t.Fatal("no message for a search finding nothing")
	}
	a.library.query = ""
	tt.Frame()
	if err := tt.Click("从列表移除 Welcome.arcmd"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if items, _ := a.bin.List(time.Now()); len(items) != 1 || tt.HasText("Welcome") {
		t.Fatalf("%d items left", len(items))
	}
	if !a.library.open {
		t.Fatal("forgetting closed the library")
	}
}

func TestDayLabel(t *testing.T) {
	now := time.Date(2026, 11, 9, 15, 0, 0, 0, time.Local)
	for at, want := range map[time.Time]string{
		now.Add(-time.Hour):      "今天",
		now.Add(-20 * time.Hour): "昨天",
		now.AddDate(0, -1, 0):    "10月9日",
		now.AddDate(-1, 0, 0):    "2025年11月9日",
	} {
		if got := dayLabel(at, now); got != want {
			t.Errorf("dayLabel(%v) = %q, want %q", at, got, want)
		}
	}
}

// TestLibrarySnapshot renders the library's pages, for review.
func TestLibrarySnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the library")
	for _, dark := range []bool{false, true} {
		for _, page := range []libraryPage{libSpaces, libPlugins, libTrash} {
			name := []string{"library-spaces", "library-plugins", "library-trash"}[page]
			if dark {
				name += "-dark"
			}
			t.Run(name, func(t *testing.T) {
				a, tt := libraryApp(t)
				tt.SetSize(1240, 760)
				tt.SetDark(dark)
				if page == libTrash {
					a.trashFile = os.RemoveAll
					a.trash("Journal/Ideas.arcmd")
					a.trash("Welcome.arcmd")
				}
				a.openLibrary(page)
				for range 6 {
					tt.Frame()
					tt.Advance(200 * time.Millisecond)
				}
				if page == libTrash {
					if r, ok := tt.Find("Ideas"); ok {
						tt.Move(r.X+r.W/2, r.Y+r.H/2)
						tt.Frame()
					}
				}
				file, err := os.Create(filepath.Join(dir, name+".png"))
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
}

// Escape leaves the library from its search field too.
func TestLibraryEscapeFromSearch(t *testing.T) {
	a, tt := newTestApp(t)
	settle(tt)
	a.openTrash()
	settle(tt)
	if !tt.Focused("搜索最近删除") {
		t.Fatal("the search does not take the keys")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.library.open {
		t.Fatal("Escape kept the library open")
	}
}

// The paper follows the space's look: a space set to dark has dark paper
// while the system is light, and one set to light has light paper while
// the system is dark.
func TestPaperFollowsSpaceAppearance(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	paper := func() (r, g, b uint8) {
		tt.Frame()
		box, ok := tt.Find("文档")
		if !ok {
			t.Fatal("no document")
		}
		img := tt.Image()
		sx := float32(img.Bounds().Dx()) / 1200
		c := img.At(int((box.X+box.W-24)*sx), int((box.Y+box.H-24)*sx))
		r32, g32, b32, _ := c.RGBA()
		return uint8(r32 >> 8), uint8(g32 >> 8), uint8(b32 >> 8)
	}
	th := space.ThemeOf(sp)
	th.Appearance = space.AppearanceDark
	sp.SetTheme(th)
	if r, g, b := paper(); int(r)+int(g)+int(b) > 3*80 {
		t.Fatalf("paper %d,%d,%d under a dark space while the system is light", r, g, b)
	}
	th.Appearance = space.AppearanceLight
	sp.SetTheme(th)
	tt.SetDark(true)
	if r, g, b := paper(); int(r)+int(g)+int(b) < 3*200 {
		t.Fatalf("paper %d,%d,%d under a light space while the system is dark", r, g, b)
	}
}

// A card's grip drags it among the others to reorder the spaces: the card
// follows the pointer, an empty slot opens where it would land, and let go
// the spaces take that order. Escape gives the drag up, and ← → on the
// grip move a space by keyboard.
func TestLibraryCardDrag(t *testing.T) {
	a, tt := libraryApp(t)
	tt.Key(ui.Ctrl|ui.Cmd, ui.KeyS)
	settle(tt)
	names := func() []string {
		var out []string
		for _, s := range a.state.Spaces {
			out = append(out, s.Name)
		}
		return out
	}
	r, ok := tt.Find("移动空间 Personal")
	if !ok {
		t.Fatalf("no grip: %q", tt.Texts())
	}
	x, y := r.X+r.W/2, r.Y+r.H/2
	tt.Press(x, y)
	tt.Frame()
	for i := 1; i <= 10; i++ {
		tt.Move(x+float32(i)*cardStride*1.1/10, y)
		tt.Frame()
	}
	if a.library.move == nil || a.library.move.settling {
		t.Fatal("the grip did not drag the card")
	}
	if _, slot := a.cardOrder(); slot != 1 {
		t.Fatalf("the slot is at %d, want 1", slot)
	}
	tt.Release(x+cardStride*1.1, y)
	settle(tt)
	if got := names(); got[0] != "Work" || got[1] != "Personal" {
		t.Fatalf("order %v after the drop", got)
	}
	if a.library.move != nil {
		t.Fatal("the card did not settle")
	}

	// Escape gives up.
	r, _ = tt.Find("移动空间 Personal")
	x, y = r.X+r.W/2, r.Y+r.H/2
	tt.Press(x, y)
	tt.Frame()
	tt.Move(x+cardStride, y)
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Release(x+cardStride, y)
	settle(tt)
	if got := names(); got[1] != "Personal" || !a.library.open {
		t.Fatalf("order %v after Escape, library open %v", got, a.library.open)
	}

	// The keyboard.
	if err := tt.Click("移动空间 Personal"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	if got := names(); got[2] != "Personal" {
		t.Fatalf("order %v after →", got)
	}
	st, err := a.store.Load()
	if err != nil || st.Spaces[2].Name != "Personal" {
		t.Fatalf("the order was not saved: %v", err)
	}
}

// TestLibraryDragSnapshot renders a card dragged halfway over the next,
// for review.
func TestLibraryDragSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the library")
	a, tt := libraryApp(t)
	tt.SetSize(1240, 760)
	a.openLibrary(libSpaces)
	settle(tt)
	r, ok := tt.Find("移动空间 Personal")
	if !ok {
		t.Fatal("no grip")
	}
	x, y := r.X+r.W/2, r.Y+r.H/2
	tt.Press(x, y)
	tt.Frame()
	for i := 1; i <= 8; i++ {
		tt.Move(x+float32(i)*cardStride*0.7/8, y-float32(i)*2)
		tt.Frame()
	}
	tt.Advance(time.Second)
	tt.Frame()
	file, err := os.Create(filepath.Join(dir, "library-drag.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, tt.Image()); err != nil {
		t.Fatal(err)
	}
}
