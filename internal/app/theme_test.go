package app

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The paper follows the system appearance, back and forth, in the editor
// and the Markdown preview alike.
func TestPaperFollowsSystemAppearance(t *testing.T) {
	a, tt := newTestApp(t)
	for _, dark := range []bool{false, true, false} {
		tt.SetDark(dark)
		tt.Frame()
		tt.Frame()
		if a.docTheme.Dark != dark {
			t.Fatal("test did not apply the system appearance")
		}
		for _, source := range []bool{false, true} {
			a.doc.source = source
			tt.Frame()
			label := "文档"
			if source {
				label = "Markdown 导出预览"
			}
			bounds, ok := tt.Find(label)
			if !ok {
				t.Fatal("writing surface missing")
			}
			pixel := color.NRGBAModel.Convert(tt.Image().At(int(bounds.X+bounds.W-20), int(bounds.Y+bounds.H-20))).(color.NRGBA)
			want := documentTheme(dark).Background
			if pixel.R != want.R || pixel.G != want.G || pixel.B != want.B {
				t.Fatalf("dark=%v source=%v paper=%v, want %v", dark, source, pixel, want)
			}
		}
	}
	if theme := documentTheme(false); theme.Dark || theme.Text != ui.Hex("#1F2329") {
		t.Fatal("light paper did not use the light text palette")
	}
	if theme := documentTheme(true); !theme.Dark || theme.Background != ui.Hex("#1F2023") {
		t.Fatal("dark paper did not use the dark palette")
	}
}

// Every color a document keeps by name has a color on dark paper too, so
// nothing falls back to the theme's generic one there.
func TestDarkPageKeepsNamedColors(t *testing.T) {
	light, dark := pageStyle(false), pageStyle(true)
	for name, m := range map[string][2]map[string]ui.Color{
		"text colors": {light.TextColors, dark.TextColors},
		"highlights":  {light.Highlights, dark.Highlights},
		"cell fills":  {light.CellFills, dark.CellFills},
		"tints":       {light.Tints, dark.Tints},
	} {
		for k := range m[0] {
			if _, ok := m[1][k]; !ok {
				t.Errorf("%s: %q missing on dark paper", name, k)
			}
		}
	}
	for k := range light.Callouts {
		if _, ok := dark.Callouts[k]; !ok {
			t.Errorf("callout %q missing on dark paper", k)
		}
	}
}
