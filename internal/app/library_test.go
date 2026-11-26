package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
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

	tt.Key(ctrlCmd(), ui.KeyS)
	tt.Frame()
	if !a.library.open || a.library.page != libSpaces {
		t.Fatal("overview shortcut did not open the spaces")
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
// shown; the plugins page lists official plugins; it opens over a
// folded sidebar.
func TestLibraryPages(t *testing.T) {
	a, tt := libraryApp(t)
	wireCloudSyncMock(t, a)
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
	if a.library.page != libPlugins || !tt.HasText("官方插件") || !tt.HasText("云端同步") {
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
	a.trash("Journal/Ideas.md")
	a.trash("Welcome.md")
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
	if err := tt.Click("从列表移除 Welcome.md"); err != nil {
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
				if page == libPlugins {
					wireCloudSyncMock(t, a)
				}
				if page == libTrash {
					a.trashFile = os.RemoveAll
					a.trash("Journal/Ideas.md")
					a.trash("Welcome.md")
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
	tt.Key(ctrlCmd(), ui.KeyS)
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

// cardBlankPoint returns a point in the empty body area of space name's card.
func cardBlankPoint(t *testing.T, tt *ui.Tester, name string) (float32, float32) {
	t.Helper()
	head, ok := tt.Find("打开空间 " + name)
	if !ok {
		t.Fatalf("no card head for %s", name)
	}
	grip, ok := tt.Find("移动空间 " + name)
	if !ok {
		t.Fatalf("no card grip for %s", name)
	}
	return head.X + head.W/2, grip.Y - 30
}

// Clicking an inactive space card's blank body switches to it and closes the
// library; clicking the active card's blank body keeps the library open, and
// clicking a folder row inside an inactive card toggles that folder alone.
func TestLibraryClickSpaceCard(t *testing.T) {
	a, tt := libraryApp(t)
	personal, work, travel := a.state.Spaces[0], a.state.Spaces[1], a.state.Spaces[2]

	a.openLibrary(libSpaces)
	settle(tt)

	// Clicking an inactive card's blank body switches to Work and closes the library.
	wx, wy := cardBlankPoint(t, tt, "Work")
	tt.ClickAt(wx, wy)
	settle(tt)
	if a.library.open || a.state.ActiveSpace() != work {
		t.Fatalf("clicking Work card: open=%v active=%s", a.library.open, a.state.ActiveSpace().Name)
	}

	// Clicking Travel's header switches to Travel and closes the library.
	a.openLibrary(libSpaces)
	settle(tt)
	if err := tt.Click("打开空间 Travel"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if a.library.open || a.state.ActiveSpace() != travel {
		t.Fatalf("clicking Travel head: open=%v active=%s", a.library.open, a.state.ActiveSpace().Name)
	}

	// Clicking the active card's blank body does not close the library.
	a.openLibrary(libSpaces)
	settle(tt)
	tx, ty := cardBlankPoint(t, tt, "Travel")
	tt.ClickAt(tx, ty)
	settle(tt)
	if !a.library.open || a.state.ActiveSpace() != travel {
		t.Fatalf("clicking active Travel card: open=%v active=%s", a.library.open, a.state.ActiveSpace().Name)
	}

	// Clicking a folder row inside Personal (inactive) toggles its expansion without closing or switching.
	wasOpen := personal.IsExpanded("Journal")
	if err := tt.Click("Journal"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if !a.library.open || a.state.ActiveSpace() != travel {
		t.Fatalf("clicking folder switched or closed: open=%v active=%s", a.library.open, a.state.ActiveSpace().Name)
	}
	if personal.IsExpanded("Journal") == wasOpen {
		t.Fatal("clicking Personal's Journal folder did not toggle it")
	}
}

// Rows inside library space cards drag across spaces (to a card's root or
// into a folder, opening closed folders on hover) and within a space, with
// Escape canceling and external spaces refusing drops.
func TestLibraryDragAcrossSpaces(t *testing.T) {
	a, tt := libraryApp(t)
	personal, work, travel := a.state.Spaces[0], a.state.Spaces[1], a.state.Spaces[2]

	// Give Work and Travel distinct folder names for unambiguous hit targets.
	os.MkdirAll(filepath.Join(work.Root, "WorkBox"), 0o755)
	os.MkdirAll(filepath.Join(travel.Root, "TravelBox"), 0o755)
	os.WriteFile(filepath.Join(travel.Root, "TravelBox/Inside.md"), []byte("# Inside\n"), 0o644)
	travel.SetExpanded("TravelBox", true)

	a.openLibrary(libSpaces)
	settle(tt)

	// 1. Cross-space drop onto Travel card root, then undo.
	planRect, ok := tt.Find("Work Plan")
	if !ok {
		t.Fatalf("no Work Plan row: %q", tt.Texts())
	}
	sx, sy := planRect.X+20, planRect.Y+planRect.H/2
	tx, ty := cardBlankPoint(t, tt, "Travel")
	tt.Press(sx, sy)
	tt.Move(sx, sy+6)
	tt.Frame()
	glide(tt, sx, sy+6, tx, ty)
	if !tt.HasText("移到此空间") {
		t.Fatalf("missing cross-space drop hint: %q", tt.Texts())
	}
	tt.Release(tx, ty)
	settle(tt)
	if exists(work, "Work Plan.md") || !exists(travel, "Work Plan.md") {
		t.Fatal("Work Plan.md did not move to Travel root")
	}
	if !a.library.open || vault.Find(a.library.trees[travel.ID], "Work Plan.md") == nil || vault.Find(a.library.trees[work.ID], "Work Plan.md") != nil {
		t.Fatal("library trees did not refresh after cross-space drop")
	}
	if !tt.HasText("已移到空间「Travel」") {
		t.Fatalf("missing undo toast: %q", tt.Texts())
	}
	if err := tt.Click("撤销"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if !exists(work, "Work Plan.md") || exists(travel, "Work Plan.md") {
		t.Fatal("undo did not move Work Plan.md back to Work")
	}
	if vault.Find(a.library.trees[work.ID], "Work Plan.md") == nil {
		t.Fatal("library trees did not refresh after undo")
	}

	// 2. Cross-space drop into a target folder row (Work Ideas -> Travel/TravelBox).
	ideasRect, ok := tt.Find("Work Ideas")
	if !ok {
		t.Fatal("no Work Ideas row")
	}
	folderRect, ok := tt.Find("TravelBox")
	if !ok {
		t.Fatal("no TravelBox folder row")
	}
	ix, iy := ideasRect.X+20, ideasRect.Y+ideasRect.H/2
	fx, fy := folderRect.X+20, folderRect.Y+folderRect.H/2
	tt.Press(ix, iy)
	tt.Move(ix, iy+6)
	tt.Frame()
	glide(tt, ix, iy+6, fx, fy)
	if !tt.HasText("移到文件夹") {
		t.Fatalf("missing folder drop badge: %q", tt.Texts())
	}
	tt.Release(fx, fy)
	settle(tt)
	if exists(work, "Notes/Work Ideas.md") || !exists(travel, "TravelBox/Work Ideas.md") {
		t.Fatal("Work Ideas.md did not move into Travel/TravelBox")
	}

	// 3. Cross-space drag of an entire folder (Travel/TravelBox -> Personal root).
	folderRect, ok = tt.Find("TravelBox")
	if !ok {
		t.Fatal("no TravelBox row")
	}
	fx, fy = folderRect.X+20, folderRect.Y+folderRect.H/2
	px, py := cardBlankPoint(t, tt, "Personal")
	tt.Press(fx, fy)
	tt.Move(fx, fy+6)
	tt.Frame()
	glide(tt, fx, fy+6, px, py)
	tt.Release(px, py)
	settle(tt)
	if exists(travel, "TravelBox") || !exists(personal, "TravelBox/Work Ideas.md") || !exists(personal, "TravelBox/Inside.md") {
		t.Fatal("TravelBox folder did not move to Personal")
	}

	// 4. Hovering over a collapsed folder opens it after springDelay.
	work.SetExpanded("WorkBox", false)
	tt.Frame()
	welcomeRect, ok := tt.Find("Welcome")
	if !ok {
		t.Fatal("no Welcome row")
	}
	workBoxRect, ok := tt.Find("WorkBox")
	if !ok {
		t.Fatal("no WorkBox row")
	}
	wx0, wy0 := welcomeRect.X+20, welcomeRect.Y+welcomeRect.H/2
	wbx, wby := workBoxRect.X+20, workBoxRect.Y+workBoxRect.H/2
	tt.Press(wx0, wy0)
	tt.Move(wx0, wy0+6)
	tt.Frame()
	glide(tt, wx0, wy0+6, wbx, wby)
	tt.Advance(springDelay + 100*time.Millisecond)
	tt.Frame()
	if !work.IsExpanded("WorkBox") {
		t.Fatal("hovering collapsed folder did not spring-open WorkBox")
	}

	// 5. Pressing Escape during drag cancels it without closing the library or moving the file.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Release(wbx, wby)
	settle(tt)
	if !a.library.open || !exists(personal, "Welcome.md") || exists(work, "WorkBox/Welcome.md") {
		t.Fatalf("Escape did not cancel drag cleanly: open=%v", a.library.open)
	}

	// 6. Same-space drag into folder and back to root.
	welcomeRect, _ = tt.Find("Welcome")
	journalRect, ok := tt.Find("Journal")
	if !ok {
		t.Fatal("no Journal folder row")
	}
	wx0, wy0 = welcomeRect.X+20, welcomeRect.Y+welcomeRect.H/2
	jx, jy := journalRect.X+20, journalRect.Y+journalRect.H/2
	tt.Press(wx0, wy0)
	tt.Move(wx0, wy0+6)
	tt.Frame()
	glide(tt, wx0, wy0+6, jx, jy)
	tt.Release(jx, jy)
	settle(tt)
	if exists(personal, "Welcome.md") || !exists(personal, "Journal/Welcome.md") {
		t.Fatal("same-space drag into Journal failed")
	}

	welcomeRect, ok = tt.Find("Welcome")
	if !ok {
		t.Fatal("no Welcome row in Journal")
	}
	wx0, wy0 = welcomeRect.X+20, welcomeRect.Y+welcomeRect.H/2
	px, py = cardBlankPoint(t, tt, "Personal")
	tt.Press(wx0, wy0)
	tt.Move(wx0, wy0+6)
	tt.Frame()
	glide(tt, wx0, wy0+6, px, py)
	if !tt.HasText("移到根目录") {
		t.Fatalf("missing same-space root drop hint: %q", tt.Texts())
	}
	tt.Release(px, py)
	settle(tt)
	if !exists(personal, "Welcome.md") || exists(personal, "Journal/Welcome.md") {
		t.Fatal("same-space drag back to root failed")
	}

	// 7. External space refuses drops.
	extFile := filepath.Join(t.TempDir(), "ExtDoc.md")
	if err := os.WriteFile(extFile, []byte("# Ext\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !a.openSystemFile(extFile) {
		t.Fatal("openSystemFile failed")
	}
	ext := a.state.Get("external")
	if ext == nil || !ext.IsExternal() {
		t.Fatal("missing external space")
	}
	a.openLibrary(libSpaces)
	settle(tt)
	tt.SetSize(1400, 760)
	tt.Frame()
	planRect, _ = tt.Find("Work Plan")
	sx, sy = planRect.X+20, planRect.Y+planRect.H/2
	ex, ey := cardBlankPoint(t, tt, ext.Name)
	tt.Press(sx, sy)
	tt.Move(sx, sy+6)
	tt.Frame()
	glide(tt, sx, sy+6, ex, ey)
	tt.Release(ex, ey)
	settle(tt)
	if !exists(work, "Work Plan.md") {
		t.Fatal("external space accepted a drop")
	}
}
