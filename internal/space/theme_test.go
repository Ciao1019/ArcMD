package space

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// near reports whether two hex colors are within tol/255 a channel.
func near(a, b string, tol float64) bool {
	ar, ag, ab := Components(a)
	br, bg, bb := Components(b)
	for _, d := range []float64{ar - br, ag - bg, ab - bb} {
		if d*255 > tol || d*255 < -tol {
			return false
		}
	}
	return true
}

func TestShadeMatchesArc(t *testing.T) {
	// Arc's default light and dark halves of #8389FF, as Cra measured them.
	if got := Shade("#8389FF", 0.9); !near(got, "#C8CAFF", 6) {
		t.Errorf("light shade = %s", got)
	}
	if got := Shade("#8389FF", 0.064); !near(got, "#01021F", 3) {
		t.Errorf("dark shade = %s", got)
	}
	if got := MidToneHex("#6CBAD9"); !near(got, "#6CBAD9", 2) {
		t.Errorf("midTone of Arc's light blue = %s", got)
	}
}

func TestLook(t *testing.T) {
	one := Theme{Colors: []string{"#F29BBB"}, Intensity: 0.8, Appearance: AppearanceAuto}
	for _, dark := range []bool{false, true} {
		l := one.Look(dark)
		if dark {
			// Auto shows its dark half while the system is dark, one
			// color as two.
			if !l.Dark || l.Solid || len(l.Colors) != 2 || Luminance(l.Colors[0]) > 0.2 {
				t.Errorf("one color on Auto, system dark: %+v", l)
			}
			continue
		}
		if !l.Solid || l.Dark || len(l.Colors) != 2 || l.Colors[0] != "#F29BBB" {
			t.Errorf("one light color on Auto, system light: %+v", l)
		}
	}
	// A dark color on Auto is lifted to a light tint while the system is
	// light, as Cra's light half is.
	if l := (Theme{Colors: []string{"#4E3A5A"}, Appearance: AppearanceAuto}).Look(false); l.Dark || IsDarkHex(l.Colors[0]) {
		t.Errorf("a dark color on Auto, system light: %+v", l)
	}
	pair := Theme{Colors: []string{"#6CBAD9", "#7F7EDC"}, Intensity: 0.5, Appearance: AppearanceAuto}
	if l := pair.Look(false); l.Dark || l.Solid || l.Colors[1] != "#7F7EDC" {
		t.Errorf("light half = %+v", l)
	}
	if l := pair.Look(true); !l.Dark || l.Intensity != 1 || Luminance(l.Colors[0]) > 0.2 {
		t.Errorf("dark half = %+v", l)
	}
	if l := (Theme{Colors: []string{"#26456B", "#226149"}, Appearance: AppearanceLight}).Look(true); l.Dark || IsDarkHex(l.Colors[0]) {
		t.Errorf("a Light theme of dark colors should be lifted: %+v", l)
	}
}

func TestThemeOfOldAndBrokenSpaces(t *testing.T) {
	th := ThemeOf(&Space{Color: "#7C6CF2"})
	if len(th.Colors) != 1 || th.Colors[0] != "#7C6CF2" || th.Appearance != AppearanceLight || th.Intensity != 0.8 {
		t.Errorf("theme of a version 1 space = %+v", th)
	}
	if th := ThemeOf(&Space{Color: "#26456B"}); th.Appearance != AppearanceDark {
		t.Errorf("a dark version 1 space should stay dark: %+v", th)
	}
	bad := &Space{Theme: &Theme{Colors: []string{"red", "#ABCDEF", "#111111", "#222222", "#333333"}, Intensity: 7, Noise: -1, Appearance: "neon"}}
	th = ThemeOf(bad)
	if len(th.Colors) != 3 || th.Colors[0] != "#ABCDEF" || th.Intensity != 1 || th.Noise != 0 || th.Appearance != AppearanceAuto {
		t.Errorf("sanitized theme = %+v", th)
	}
	if th := ThemeOf(nil); len(th.Colors) != 1 {
		t.Errorf("theme of no space = %+v", th)
	}
}

func TestWheelRoundTrip(t *testing.T) {
	for _, hue := range []float64{0, 40, 200, 320} {
		c := WheelColor(hue, 0.6)
		h, s, v := HSB(Components(c))
		if d := h - hue; d > 1.5 || d < -1.5 || s < 0.58 || s > 0.62 || v < 0.94 || v > 0.96 {
			t.Errorf("WheelColor(%v) = %s → %v %v %v", hue, c, h, s, v)
		}
	}
	if PageOf([]string{"#f8d558", "#A4F454"}) != 1 || PageOf([]string{"#123456"}) != -1 {
		t.Error("PageOf")
	}
}

func TestVersion1FileLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spaces.json")
	v1 := `{"version":1,"active":"a","spaces":[{"id":"a","name":"Work","icon":"✦","color":"#2E9E83","root":"/w"}]}`
	if err := os.WriteFile(path, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := NewStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if th := ThemeOf(st.Spaces[0]); th.Colors[0] != "#2E9E83" {
		t.Errorf("migrated theme = %+v", th)
	}
	st.Spaces[0].SetTheme(Theme{Colors: []string{"#F25E6B", "#EAAF5A"}, Intensity: 0.4, Noise: 0.2, Appearance: AppearanceDark})
	if err := NewStore(path).Save(st); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var raw struct {
		Version int
		Spaces  []*Space
	}
	json.Unmarshal(b, &raw)
	if raw.Version != CurrentVersion || raw.Spaces[0].Theme == nil || raw.Spaces[0].Color != "#F25E6B" {
		t.Errorf("saved %s", b)
	}
}

// Arc's palettes, as Cra measured them: tintedLight within 2/255.
func TestTintedLight(t *testing.T) {
	for in, want := range map[string]string{"#6CBAD9": "#E4F2F8", "#F8D558": "#FCEDBC", "#010220": "#ECEDFF"} {
		got := TintedLightHex(in)
		gr, gg, gb := Components(got)
		wr, wg, wb := Components(want)
		if math.Abs(gr-wr)*255 > 2.5 || math.Abs(gg-wg)*255 > 2.5 || math.Abs(gb-wb)*255 > 2.5 {
			t.Errorf("TintedLightHex(%s) = %s, want %s", in, got, want)
		}
	}
	o, f, _ := FolderTones("#6CBAD9", false)
	if f != TintedLightHex("#6CBAD9") || o != ShadedDarkHex("#6CBAD9") {
		t.Error("light folder tones")
	}
}
