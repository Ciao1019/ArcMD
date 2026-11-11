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

// newTourApp is the first launch as a user has it: the tour showing.
func newTourApp(t *testing.T) (*App, *ui.Tester) {
	t.Helper()
	dir := t.TempDir()
	a, err := New(Config{ConfigDir: filepath.Join(dir, "config"), DocumentsDir: filepath.Join(dir, "docs")})
	if err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.View, 1200, 760)
	tt.SetLocale("zh-Hans")
	tt.SetTitleBar(ui.TitleBar{Height: 46, Left: 78})
	tt.Frame()
	tt.Advance(time.Second)
	return a, tt
}

// settleTour lets the hole and the card arrive.
func settleTour(tt *ui.Tester) {
	tt.Frame()
	tt.Advance(1200 * time.Millisecond)
	tt.Frame()
}

func TestTourShowsOnFirstRunOnly(t *testing.T) {
	a, tt := newTourApp(t)
	if !a.tour.open || !tt.HasText("欢迎来到 ArcMD") {
		t.Fatalf("no tour on the first run: %v", tt.Texts())
	}
	// Quitting in the middle of it, the next launch shows it again.
	b, err := New(a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !b.tour.open {
		t.Fatal("the tour left unfinished did not come back")
	}
	if err := tt.Click("跳过"); err != nil {
		t.Fatal(err)
	}
	settleTour(tt)
	if a.tour.open || tt.HasText("欢迎来到 ArcMD") {
		t.Fatal("skipping left the tour open")
	}
	b, err = New(a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if b.tour.open {
		t.Fatal("the tour came back after it was skipped")
	}
}

func TestTourWalksThroughEveryStep(t *testing.T) {
	a, tt := newTourApp(t)
	if err := tt.Click("开始导览"); err != nil {
		t.Fatal(err)
	}
	for i, title := range []string{"用空间分开不同的事", "给每个空间一身颜色", "左右轻扫，切换空间", "用书签记住读到哪儿", "把常用的文档置顶", "收藏，在每个空间都看得到"} {
		settleTour(tt)
		if !tt.HasText(title) {
			t.Fatalf("step %d: no %q in %v", i+1, title, tt.Texts())
		}
		// Every step of the sidebar and the page points at its part.
		if !a.tour.holed || a.tour.hole.W < 20 || a.tour.hole.H < 20 {
			t.Fatalf("step %q points at nothing: %+v", title, a.tour.hole)
		}
		card, ok := tt.Find(title)
		if !ok {
			t.Fatalf("step %q: card not found", title)
		}
		if h := a.tour.hole; card.X < h.X+h.W && title != "用书签记住读到哪儿" {
			t.Errorf("step %q: the card at %v covers what it points at, %v", title, card, h)
		}
		if err := tt.Click("下一步"); err != nil {
			t.Fatal(err)
		}
	}
	settleTour(tt)
	if !tt.HasText("准备好了，开始写吧") {
		t.Fatalf("no last step: %v", tt.Texts())
	}
	if err := tt.Click("开始写作"); err != nil {
		t.Fatal(err)
	}
	settleTour(tt)
	if a.tour.open || !a.prefs.TourSeen {
		t.Fatal("finishing left the tour open or pending")
	}
}

func TestTourKeyboard(t *testing.T) {
	a, tt := newTourApp(t)
	tt.Key(0, ui.KeyRight)
	tt.Key(0, ui.KeyRight)
	settleTour(tt)
	if a.tour.step != 2 {
		t.Fatalf("→ → reached step %d", a.tour.step)
	}
	tt.Key(0, ui.KeyLeft)
	settleTour(tt)
	if a.tour.step != 1 || !tt.HasText("用空间分开不同的事") {
		t.Fatalf("← reached step %d", a.tour.step)
	}
	// Enter presses the way on, which holds the focus.
	tt.Key(0, ui.KeyEnter)
	settleTour(tt)
	if a.tour.step != 2 {
		t.Fatalf("Enter reached step %d", a.tour.step)
	}
	// The window's own shortcuts wait while the tour shows.
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	if a.palette.open {
		t.Fatal("the command bar opened behind the tour")
	}
	tt.Key(0, ui.KeyEscape)
	settleTour(tt)
	if a.tour.open {
		t.Fatal("Escape did not skip the tour")
	}
	// The keyboard is the document's again.
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	if !a.palette.open {
		t.Fatal("the command bar did not open after the tour")
	}
}

// What is behind the tour does not act: a click on the sidebar's row
// opens nothing.
func TestTourCoversTheWindow(t *testing.T) {
	a, tt := newTourApp(t)
	before := a.doc.rel
	if r, ok := tt.Find("Ideas"); ok {
		tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	}
	tt.Frame()
	if a.doc.rel != before || !a.tour.open {
		t.Fatalf("a click went through the tour: %q", a.doc.rel)
	}
}

func TestTourFromCommandBarAndSettings(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	if a.tour.open {
		t.Fatal("the tour shows without being asked")
	}
	a.toggleSidebar()
	a.openPalette()
	tt.Frame()
	tt.Type("新手教程")
	tt.Key(0, ui.KeyEnter)
	settleTour(tt)
	if !a.tour.open || a.palette.open || a.folded() {
		t.Fatalf("command bar: tour %v, palette %v, folded %v", a.tour.open, a.palette.open, a.folded())
	}
	tt.Key(0, ui.KeyEscape)
	settleTour(tt)
	a.openSettings(tabAbout)
	tt.Frame()
	if err := tt.Click("重新观看"); err != nil {
		t.Fatal(err)
	}
	settleTour(tt)
	if !a.tour.open || a.settingsOpen || !tt.HasText("欢迎来到 ArcMD") {
		t.Fatalf("settings: tour %v, settings %v", a.tour.open, a.settingsOpen)
	}
}

// Without the sidebar's parts, as in a window too small for them, a step
// stands alone in the middle rather than pointing at nothing.
func TestTourPlacement(t *testing.T) {
	x, y := tourPlace(ui.Rect{}, false, 360, 400, 1200, 760)
	if x != 420 || y != 180 {
		t.Errorf("alone: %v, %v", x, y)
	}
	// Beside a part of the sidebar: to its right, level with it, inside
	// the window.
	x, y = tourPlace(ui.Rect{X: 4, Y: 700, W: 222, H: 48}, true, 360, 400, 1200, 760)
	if x != 4+222+tourGap || y != 760-400-tourMargin {
		t.Errorf("beside the foot: %v, %v", x, y)
	}
	// With no room on any side, over the part's bottom right corner.
	x, y = tourPlace(ui.Rect{X: 230, Y: 46, W: 962, H: 706}, true, 360, 400, 1200, 760)
	if x != 1192-360-24 || y != 752-400-24 {
		t.Errorf("over the page: %v, %v", x, y)
	}
}

func TestTourSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the tour")
	for _, dark := range []bool{false, true} {
		for i, step := range tourSteps {
			name := "tour-" + string(rune('0'+i)) + "-" + step.id
			if dark {
				if i != 0 && i != 3 && i != 5 {
					continue
				}
				name += "-dark"
			}
			t.Run(name, func(t *testing.T) {
				a, tt := newTourApp(t)
				tt.SetDark(dark)
				a.tourGo(i)
				settleTour(tt)
				// Into the picture's loop, at the moment that says most.
				if rest := time.Duration(tourArts[step.id].still*1000)*time.Millisecond - 1200*time.Millisecond; rest > 0 {
					tt.Advance(rest)
					tt.Frame()
				}
				savePNG(t, filepath.Join(dir, name+".png"), tt)
			})
		}
	}
}

// The step on themes opens the real theme card and points at it, its
// colors moving to show the preview; leaving the step, or the tour, keeps
// none of that.
func TestTourShowsTheThemeCard(t *testing.T) {
	a, tt := newTourApp(t)
	sp := a.state.ActiveSpace()
	before := space.ThemeOf(sp)
	a.tourGo(2)
	settleTour(tt)
	f := &a.spaceForm
	if !f.open || f.edit != "theme" || !f.cardOpen || !tt.HasText("给每个空间一身颜色") {
		t.Fatalf("no theme card on its step: %+v", f)
	}
	card := a.tour.marks["theme-card"]
	if h := a.tour.hole; card.W < 300 || h.X > card.X || h.X+h.W < card.X+card.W || h.Y > card.Y || h.Y+h.H < card.Y+card.H {
		t.Fatalf("the hole %v is not round the theme card %v", a.tour.hole, card)
	}
	tt.Advance(1500 * time.Millisecond)
	tt.Frame()
	if len(before.Colors) > 0 && slices.Equal(f.theme.Colors, before.Colors) {
		t.Error("the card's colors do not move")
	}
	// A press beside the card does not close it, nor keep its colors.
	tt.ClickAt(900, 700)
	settleTour(tt)
	if !a.spaceForm.cardOpen {
		t.Fatal("a press closed the tour's theme card")
	}
	tt.Key(0, ui.KeyRight)
	settleTour(tt)
	if a.spaceForm.open || a.tour.themeHeld {
		t.Fatal("the theme card outlived its step")
	}
	if after := space.ThemeOf(sp); !slices.Equal(after.Colors, before.Colors) || after.Intensity != before.Intensity {
		t.Fatalf("the tour changed the space's theme: %+v, was %+v", after, before)
	}
	// Skipped on the step itself, the same.
	tt.Key(0, ui.KeyLeft)
	settleTour(tt)
	tt.Key(0, ui.KeyEscape)
	settleTour(tt)
	if a.tour.open || a.spaceForm.open || !slices.Equal(space.ThemeOf(sp).Colors, before.Colors) {
		t.Fatal("skipping on the theme step kept its card or colors")
	}
}

// Someone who used ArcMD before it had a tour sees it once after the
// update: their spaces are there, their preferences say nothing of it.
func TestTourShowsOnceAfterUpdate(t *testing.T) {
	a, _ := newTestApp(t)
	a.prefs.TourSeen = false
	a.savePrefs()
	b, err := New(a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.state.Spaces) != 1 || !b.tour.open {
		t.Fatalf("no tour after the update: %d spaces, tour %v", len(b.state.Spaces), b.tour.open)
	}
	b.endTour()
	c, err := New(a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if c.tour.open {
		t.Fatal("the tour showed a second time")
	}
}

// Preferences that cannot be read are not overwritten, so the tour could
// not be remembered as seen: it stays away rather than show every launch.
func TestTourStaysAwayWithBrokenPrefs(t *testing.T) {
	a, _ := newTestApp(t)
	if err := os.WriteFile(a.prefStore.Path, []byte(`{"version": 99}`), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := New(a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !b.prefsBroken || b.tour.open {
		t.Fatalf("broken %v, tour %v", b.prefsBroken, b.tour.open)
	}
}
