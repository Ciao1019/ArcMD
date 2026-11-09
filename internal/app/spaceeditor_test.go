package app

import (
	"slices"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo/ui"
)

func TestNewSpaceColorEditor(t *testing.T) {
	a, tt := newTestApp(t)
	if err := tt.Click("新建空间"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.spaceForm.open || len(a.spaceForm.theme.Colors) != 1 {
		t.Fatalf("form = %+v", a.spaceForm)
	}
	for range 3 {
		if err := tt.Click("下一页预设颜色"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	if err := tt.Click("将空间颜色设为 Drab 2"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := a.spaceForm.theme.Colors; !slices.Equal(got, []string{space.Drab[1]}) {
		t.Fatalf("swatch gave %v", got)
	}
	if err := tt.Click("增加颜色"); err != nil {
		t.Fatal(err)
	}
	tt.Click("深色")
	tt.Frame()
	if th := a.spaceForm.theme; len(th.Colors) != 2 || th.Appearance != space.AppearanceDark {
		t.Fatalf("theme = %+v", th)
	}

	// Pressing on the wheel brings the first color there: right of the
	// center is red, at the rim fully saturated.
	wheel, ok := tt.Find("空间颜色")
	if !ok {
		t.Fatal("no wheel")
	}
	cx, cy := wheel.X+wheel.W/2, wheel.Y+wheel.H/2
	tt.Press(cx+padReach, cy)
	tt.Frame()
	tt.Move(cx+padReach, cy+1)
	tt.Frame()
	tt.Release(cx+padReach, cy+1)
	tt.Frame()
	h, s, _ := space.HSB(space.Components(a.spaceForm.theme.Colors[0]))
	if (h > 2 && h < 358) || s < 0.98 {
		t.Errorf("first color after a press at the rim = %s (hue %v, saturation %v)", a.spaceForm.theme.Colors[0], h, s)
	}
	if h2, _, _ := space.HSB(space.Components(a.spaceForm.theme.Colors[1])); h2 < 38 || h2 > 42 {
		t.Errorf("second color hue = %v, want 40° on", h2)
	}

	a.spaceForm.name = "Reading"
	if err := tt.Click("创建空间"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	sp := a.state.ActiveSpace()
	if sp.Name != "Reading" || sp.Theme == nil || len(sp.Theme.Colors) != 2 || sp.Color != sp.Theme.Colors[0] {
		t.Fatalf("space = %+v %+v", sp, sp.Theme)
	}
	tt.Advance(time.Second) // the tint springs over with the strip
	tt.Frame()
	if !a.look.Dark {
		t.Error("the window should take the dark look")
	}
}

func TestEditSpacePreviewsAndSavesOnDismiss(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	before := space.ThemeOf(sp)
	a.editSpace(sp)
	a.spaceForm.theme.Colors = []string{"#4E3A5A"}
	a.spaceForm.theme.Appearance = space.AppearanceDark
	tt.Frame()
	if !a.look.Dark {
		t.Error("editing should preview the new look in the window")
	}
	// Dismissing the independent editor commits its preview.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.spaceForm.open || slices.Equal(space.ThemeOf(sp).Colors, before.Colors) || !a.look.Dark {
		t.Fatalf("dismiss did not commit the preview: %+v", space.ThemeOf(sp))
	}
	saved, err := a.store.Load()
	if err != nil || !slices.Equal(space.ThemeOf(saved.Get(sp.ID)).Colors, []string{"#4E3A5A"}) {
		t.Fatalf("theme was not persisted: %v", err)
	}
}

func TestNewSpaceInSidebar(t *testing.T) {
	a, tt := newTestApp(t)
	before := len(a.state.Spaces)
	a.sidebarHidden = true
	if err := tt.Click("新建空间"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.sidebarHidden || !a.spaceForm.cardOpen || !tt.HasText("新建空间") || !tt.HasText("编辑主题") {
		t.Fatalf("draft not shown: hidden %v, card %v", a.sidebarHidden, a.spaceForm.cardOpen)
	}

	// The icon picker finds an emoji by name, keeping the theme card open.
	if err := tt.Click("空间图标"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.spaceForm.picker.query = "books"
	tt.Frame()
	if err := tt.Click("📚 books"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.spaceForm.icon != "📚" || a.spaceForm.symbol != "" || !a.spaceForm.cardOpen {
		t.Fatalf("icon %q symbol %q, card %v", a.spaceForm.icon, a.spaceForm.symbol, a.spaceForm.cardOpen)
	}

	// The Icon tab sets a symbol in place of the emoji.
	tt.Click("空间图标")
	tt.Frame()
	if err := tt.Click("图标集"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("图标 leaf"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.spaceForm.icon != "" || a.spaceForm.symbol != "leaf" {
		t.Fatalf("icon %q symbol %q", a.spaceForm.icon, a.spaceForm.symbol)
	}

	// Escape twice gives the draft up.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.spaceForm.open || len(a.state.Spaces) != before {
		t.Fatalf("draft kept: open %v, %d spaces", a.spaceForm.open, len(a.state.Spaces))
	}

	// Enter in the name field makes the space.
	a.newSpaceForm()
	tt.Frame()
	a.spaceForm.name = "Books"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if sp := a.state.ActiveSpace(); a.spaceForm.open || sp.Name != "Books" || len(a.state.Spaces) != before+1 || sp.Icon == "" {
		t.Fatalf("Enter did not make the space: %+v", sp)
	}
}

func TestNewSpaceThemeButtonTogglesCard(t *testing.T) {
	a, tt := newTestApp(t)
	a.newSpaceForm()
	tt.Frame()
	if !a.spaceForm.cardOpen {
		t.Fatal("the theme card should open with the draft")
	}
	// The button lies in the card's anchor, so a press there toggles the
	// card instead of dismissing it and opening it again.
	for _, want := range []bool{false, true} {
		if err := tt.Click("编辑主题"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if a.spaceForm.cardOpen != want {
			t.Fatalf("card open = %v, want %v", a.spaceForm.cardOpen, want)
		}
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.spaceForm.open {
		t.Fatal("cancel should drop the draft")
	}
}
