package app

import (
	"image/color"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo/ui"
)

// Grain fading out must only darken light chrome, including the last
// frames where its tint's alpha rounds down to zero.
func TestSpaceSwitchGrainDoesNotFlash(t *testing.T) {
	a, tt := switchApp(t)
	th := space.Theme{Colors: []string{"#F25991", "#F28659"}, Intensity: 0.43, Appearance: space.AppearanceLight}
	a.state.Spaces[1].SetTheme(th)
	th.Noise = 0.9
	a.state.Spaces[0].SetTheme(th)
	tt.Frame()
	tt.Advance(time.Second)
	if err := tt.Click("切换到 Work"); err != nil {
		t.Fatal(err)
	}
	// Both spaces have the same gradient, so fading grain cannot make
	// any pixel lighter than that gradient with no grain at all.
	plain := lookOf(space.ThemeOf(a.state.Spaces[1]), false)
	img := ui.Render(func(c *ui.Context) {
		ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) { plain.paint(p, r, 0, false) })
	}, 1200, 760, 1)
	want := color.NRGBAModel.Convert(img.At(100, 600)).(color.NRGBA)
	for frame := 0; frame < 60; frame++ {
		tt.Advance(time.Second / 60)
		got := color.NRGBAModel.Convert(tt.Image().At(100, 600)).(color.NRGBA)
		if int(got.R) > int(want.R)+1 || int(got.G) > int(want.G)+1 || int(got.B) > int(want.B)+1 {
			t.Fatalf("frame %d: fading grain brightened chrome to %v, plain gradient %v (noise %.6f)", frame, got, want, a.look.Noise)
		}
	}
	if a.pager.moving() || a.look.Noise != 0 {
		t.Fatalf("settled frame kept transition state: moving=%v noise=%v", a.pager.moving(), a.look.Noise)
	}
}

// A timer can land a swipe without an input event causing a rebuild. The
// destination's document and paper must agree in that very build pass.
func TestSwipeLandingUsesDestinationPaper(t *testing.T) {
	a, _ := switchApp(t)
	for i, appearance := range []string{space.AppearanceLight, space.AppearanceDark} {
		th := space.ThemeOf(a.state.Spaces[i])
		th.Appearance = appearance
		a.state.Spaces[i].SetTheme(th)
	}
	var mismatched bool
	tt := ui.NewTester(func(c *ui.Context) {
		a.View(c)
		if a.state.ActiveSpace() == a.state.Spaces[1] && !a.paperDark {
			mismatched = true
		}
	}, 1200, 760)
	tt.Scroll(100, 400, 150, 0)
	tt.Advance(swipeLift + time.Millisecond)
	if a.state.ActiveSpace() != a.state.Spaces[1] {
		t.Fatal("swipe did not land on Work")
	}
	if mismatched {
		t.Fatal("destination rendered with the source's paper on the swipe landing frame")
	}
}
