package app

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo/ui"
)

// switchApp is a test app with three spaces: Personal, Work and Travel.
func switchApp(t *testing.T) (*App, *ui.Tester) {
	a, tt := newTestApp(t)
	a.state.Add("Work", t.TempDir())
	a.state.Add("Travel", t.TempDir())
	tt.Frame()
	return a, tt
}

func active(a *App) string { return a.state.ActiveSpace().Name }

func TestSpaceKeys(t *testing.T) {
	a, tt := switchApp(t)
	steps := []struct {
		mods ui.Modifiers
		key  ui.Key
		want string
	}{
		{ui.Ctrl, ui.Key3, "Travel"},
		{ui.Ctrl, ui.Key1, "Personal"},
		{ui.Cmd | ui.Alt, ui.KeyRight, "Work"},
		{ui.Cmd | ui.Alt, ui.KeyRight, "Travel"},
		{ui.Cmd | ui.Alt, ui.KeyRight, "Personal"}, // wraps around
		{ui.Cmd | ui.Alt, ui.KeyLeft, "Travel"},    // and back
		{0, ui.KeyBack, "Work"},                    // the mouse's side buttons
		{0, ui.KeyForward, "Travel"},
		{ui.Cmd, ui.Key2, "Work"},
	}
	for i, s := range steps {
		tt.Key(s.mods, s.key)
		tt.Frame()
		if got := active(a); got != s.want {
			t.Fatalf("step %d: in %s, want %s", i, got, s.want)
		}
	}
}

// A switch springs the strip from where it shows onto the new page,
// passing the pages between, and settles; less motion jumps.
func TestPagerSpring(t *testing.T) {
	a, tt := switchApp(t)
	tt.Key(ui.Ctrl, ui.Key3)
	if a.pager.spring == nil || a.pager.spring.index != 2 {
		t.Fatalf("no spring: %+v", a.pager.spring)
	}
	if got := a.pagerShown(tt.Now()); got > 0.01 {
		t.Fatalf("the strip starts at %v, want 0", got)
	}
	tt.Advance(100 * time.Millisecond)
	tt.Frame()
	if got := a.pagerShown(tt.Now()); got <= 0.3 || got >= 2 {
		t.Fatalf("after 100ms the strip shows %v", got)
	}
	if !tt.FramePending() {
		t.Fatal("the spring asks for no next frame")
	}
	tt.Advance(time.Second)
	tt.Frame()
	if a.pager.spring != nil || a.pagerShown(tt.Now()) != 2 {
		t.Fatalf("not settled: %+v", a.pager.spring)
	}
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	tt.Key(ui.Ctrl, ui.Key1)
	if a.pager.spring != nil {
		t.Fatal("sprang while the system asks for less motion")
	}
}

// The strip follows a sideways swipe over the sidebar and lands on the
// next page past half way, else back; a scroll that goes down first is the
// list's.
func TestPagerSwipe(t *testing.T) {
	a, tt := switchApp(t)
	x, y := float32(100), float32(300)
	lift := func() {
		tt.Advance(swipeLift + time.Millisecond)
		tt.Frame()
	}
	tt.Scroll(x, y, 40, 0)
	tt.Frame()
	if a.pager.swipe <= 0 || active(a) != "Personal" {
		t.Fatalf("under the fingers: swipe %v in %s", a.pager.swipe, active(a))
	}
	tt.Scroll(x, y, 40, 0)
	tt.Scroll(x, y, 60, 0)
	lift()
	if active(a) != "Work" || a.pager.swipe != 0 || a.pager.spring == nil {
		t.Fatalf("past half way: in %s, swipe %v, spring %v", active(a), a.pager.swipe, a.pager.spring)
	}
	tt.Advance(time.Second)
	tt.Frame()

	// A short swipe goes back.
	tt.Scroll(x, y, 50, 0)
	lift()
	if active(a) != "Work" {
		t.Fatalf("a short swipe switched to %s", active(a))
	}
	tt.Advance(time.Second)
	tt.Frame()

	// Past the first space the strip stretches less and less.
	tt.Key(ui.Ctrl, ui.Key1)
	tt.Advance(time.Second)
	tt.Frame()
	tt.Scroll(x, y, -400, 0)
	if a.pager.swipe >= 0 || a.pager.swipe < -0.3 {
		t.Fatalf("the rubber band: %v", a.pager.swipe)
	}
	lift()
	tt.Advance(time.Second)
	tt.Frame()
	if active(a) != "Personal" {
		t.Fatalf("went past the first space to %s", active(a))
	}

	// Down first, the list scrolls.
	tt.Scroll(x, y, 2, 30)
	tt.Scroll(x, y, 200, 0)
	if a.pager.swipe != 0 {
		t.Fatal("a vertical scroll paged")
	}
}

// A fast flick lands on the next page however short it is, and the
// momentum after the fingers lift moves nothing.
func TestPagerFlick(t *testing.T) {
	a, tt := switchApp(t)
	now := tt.Now()
	ev := func(dx float32, momentum bool) {
		now = now.Add(8 * time.Millisecond)
		a.pagerScroll(ui.InputEvent{Kind: ui.InputScroll, DX: dx, Precise: true, Momentum: momentum, Time: now})
	}
	for range 4 {
		ev(20, false)
	}
	if active(a) != "Personal" {
		t.Fatal("switched under the fingers")
	}
	for range 40 {
		ev(30, true)
	}
	if active(a) != "Work" {
		t.Fatalf("the flick: in %s", active(a))
	}
	if a.pager.velocity != 0 || a.pager.spring == nil || a.pager.spring.v0 == 0 {
		t.Fatalf("the settle did not take the flick's speed: %+v", a.pager.spring)
	}
}

// Paging, the window's tint mixes the two spaces either side.
func TestPagerTint(t *testing.T) {
	a, tt := switchApp(t)
	a.pager.swipe = 0.5
	tt.Frame()
	x := lookOf(space.ThemeOf(a.state.Spaces[0]), false).stops[0]
	y := lookOf(space.ThemeOf(a.state.Spaces[1]), false).stops[0]
	if got := a.look.stops[0]; got == x || got == y {
		t.Fatalf("tint %v is one space's (%v, %v)", got, x, y)
	}
}

// A space whose folder is gone leaves the window where it was.
func TestSpaceFolderGone(t *testing.T) {
	a, tt := switchApp(t)
	if err := os.RemoveAll(a.state.Spaces[1].Root); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Ctrl, ui.Key2)
	tt.Frame()
	if active(a) != "Personal" || a.doc == nil {
		t.Fatalf("in %s with %v", active(a), a.doc)
	}
	found := false
	for _, s := range tt.Texts() {
		if len(s) > 0 && s[:min(len(s), len("无法打开空间"))] == "无法打开空间" {
			found = true
		}
	}
	if !found {
		t.Errorf("no error shown: %q", tt.Texts())
	}
}

// TestSpaceSlideSnapshot renders the strip a third and two thirds of the
// way to the next space, for review.
func TestSpaceSlideSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the space switch")
	a, tt := libraryApp(t)
	tt.SetSize(1240, 700)
	for _, f := range []float32{0.33, 0.66} {
		a.pager.swipe = f
		tt.Frame()
		tt.Frame()
		writePNG(t, filepath.Join(dir, fmt.Sprintf("space-page-%02.0f.png", f*100)), tt.Image())
	}
}

func writePNG(t *testing.T, name string, img image.Image) {
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// A light space and a dark one, each solid or not, mixed while paging:
// the chrome moves evenly from the one to the other, with no step on the
// way and none at either end, where it is that look alone.
func TestPagerLookContinuous(t *testing.T) {
	looks := []spaceLook{}
	for i, appearance := range []string{space.AppearanceLight, space.AppearanceDark} {
		for n := 1; n <= 2; n++ {
			th := space.NewTheme(i + n)
			th.Appearance = appearance
			if n == 2 {
				th.Colors = append(th.Colors, th.Colors[0])
			}
			looks = append(looks, lookOf(th, false))
		}
	}
	face := func(l spaceLook) [3]float32 {
		c := l.stops[0]
		if l.Solid {
			c = c.Over(materialGrey(false))
		}
		return [3]float32{float32(c.R), float32(c.G), float32(c.B)}
	}
	gap := func(p, q [3]float32) float32 {
		return max(abs32(p[0]-q[0]), abs32(p[1]-q[1]), abs32(p[2]-q[2]))
	}
	for _, x := range looks {
		for _, y := range looks {
			prev := face(x)
			for i := 1; i <= 100; i++ {
				f := float32(i) / 100
				m := mixLooks(x, y, f)
				now := face(m)
				// A hundredth of the way moves a hundredth of the
				// distance, give or take rounding.
				if d := gap(prev, now); d > gap(face(x), face(y))/100+3 {
					t.Fatalf("solid %v→%v dark %v→%v: a step of %v at %.2f", x.Solid, y.Solid, x.Dark, y.Dark, d, f)
				}
				prev = now
			}
			if d := gap(face(mixLooks(x, y, 0.999)), face(y)); d > 2 {
				t.Errorf("solid %v→%v: the mix ends %v away from the look", x.Solid, y.Solid, d)
			}
			if m := mixLooks(x, y, 0.5); m.darkness != (x.darkness+y.darkness)/2 {
				t.Errorf("darkness %v halfway", m.darkness)
			}
		}
	}
	if c := mixColor(white.Alpha(0.5), black, 0.5); c.A < 190 || c.A > 192 {
		t.Errorf("opacity halfway from 0.5 to 1 is %d/255", c.A)
	}
}

// The grain of a light space and a dark one stays while paging between
// them, rather than fading away halfway, where the two would cancel out.
func TestGrainKeepsStrengthWhilePaging(t *testing.T) {
	a, tt := libraryApp(t)
	tt.SetSize(1200, 700)
	for i, appearance := range []string{space.AppearanceLight, space.AppearanceDark} {
		th := space.ThemeOf(a.state.Spaces[i])
		th.Appearance, th.Noise = appearance, 1
		a.state.Spaces[i].SetTheme(th)
	}
	strength := func(f float32) float64 {
		a.pager.swipe = f
		tt.Frame()
		tt.Frame()
		img := tt.Image()
		s := img.Bounds().Dx() / 1200
		var sum, sq float64
		n := 0
		for y := 440 * s; y < 560*s; y++ {
			for x := 0; x < 200*s; x++ {
				o := img.PixOffset(x, y)
				l := float64(int(img.Pix[o])+int(img.Pix[o+1])+int(img.Pix[o+2])) / 3
				sum, sq, n = sum+l, sq+l*l, n+1
			}
		}
		m := sum / float64(n)
		return sq/float64(n) - m*m
	}
	light := strength(0)
	for _, f := range []float32{0.25, 0.5, 0.75} {
		if v := strength(f); v < 0.4*light {
			t.Errorf("grain at %.2f of the way is %.1f, against %.1f in the light space", f, v, light)
		}
	}
}
