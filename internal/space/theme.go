package space

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// The theme model follows Cra (https://github.com/VictorKildahl/cra, MIT,
// Copyright (c) 2026 Victor Kildahl), which measured Arc's space themes:
// SpaceTheme, SpaceThemeVariants, SpaceColorPresets and ThemeTones.

// Theme is how a space looks, as Arc's theme editor makes it: one to three
// key colors picked on a color wheel, how strongly they show, how much
// grain lies over them, and whether the space follows the system's light
// or dark appearance.
type Theme struct {
	// Colors are the editor's key colors, "#RRGGBB": one is a flat fill,
	// two or three a gradient from top to bottom.
	Colors []string `json:"colors"`
	// Intensity is how saturated the colors are drawn, 0 to 1.
	Intensity float64 `json:"intensity"`
	// Noise is the grain over the colors, 0 to 1.
	Noise float64 `json:"noise"`
	// Appearance is "auto", "light" or "dark".
	Appearance string `json:"appearance"`
}

// Appearances of a theme, in the editor's order.
const (
	AppearanceAuto  = "auto"
	AppearanceLight = "light"
	AppearanceDark  = "dark"
)

// Arc's catalog colors, four groups of nine, as Arc writes them in sRGB.
// BrandPairs are the second colors Arc gives each Brand color on its page
// of pairs, 29–46° further round the hue wheel.
var (
	Brand      = []string{"#F2EAE4", "#F29BBB", "#A6729D", "#F25E6B", "#FF8558", "#F8D558", "#34E895", "#6CBAD9", "#666789"}
	BrandPairs = []string{"#EFF1E3", "#EFB19B", "#A6727F", "#EAAF5A", "#F0E941", "#A4F454", "#38E5E7", "#7F7EDC", "#7F6589"}
	Drab       = []string{"#4E3A5A", "#693558", "#8F4042", "#B16D40", "#D7B35D", "#CDCCA4", "#7AA982", "#226149", "#26456B"}
	Pastel     = []string{"#FEFBFA", "#FFE9F3", "#F1D1EC", "#FFCBD1", "#FFE0D3", "#FFF9E5", "#DEFDEB", "#DCF3FC", "#C5C6E5"}
	Greyscale  = []string{"#FFFFFF", "#E6E6E6", "#CCCCCC", "#B2B2B2", "#808080", "#666666", "#333333", "#1A1A1A", "#000000"}
)

// PresetPage is one page of nine swatches in the editor: each swatch puts
// one or two colors on the wheel.
type PresetPage struct {
	Name     string
	Swatches [][]string
}

// PresetPages are Arc's five pages: Brand, Brand pairs, Pastel, Drab and
// Greyscale.
var PresetPages = []PresetPage{
	{"Brand", singles(Brand)},
	{"Brand pairs", pairs(Brand, BrandPairs)},
	{"Pastel", singles(Pastel)},
	{"Drab", singles(Drab)},
	{"Greyscale", singles(Greyscale)},
}

func singles(cs []string) [][]string {
	out := make([][]string, len(cs))
	for i, c := range cs {
		out[i] = []string{c}
	}
	return out
}

func pairs(a, b []string) [][]string {
	out := make([][]string, len(a))
	for i := range a {
		out[i] = []string{a[i], b[i]}
	}
	return out
}

// PageOf returns the page holding a swatch of exactly these colors, or -1.
func PageOf(colors []string) int {
	for i, p := range PresetPages {
		for _, s := range p.Swatches {
			if len(s) != len(colors) {
				continue
			}
			same := true
			for j := range s {
				same = same && strings.EqualFold(s[j], colors[j])
			}
			if same {
				return i
			}
		}
	}
	return -1
}

// newOrder is the order new spaces take the Brand colors in, the cream
// last: Arc picks one at random, but a fixed turn keeps neighbors apart.
var newOrder = []int{7, 1, 5, 6, 3, 2, 4, 8, 0}

// NewTheme is a new space's theme, as Arc's: one of its bright Brand colors,
// on Auto, at 0.8 intensity and without grain. The n-th space takes the
// n-th color of newOrder.
func NewTheme(n int) Theme {
	return Theme{Colors: []string{Brand[newOrder[n%len(newOrder)]]}, Intensity: 0.8, Appearance: AppearanceAuto}
}

// ThemeOf returns the space's theme, made from its single Color for a space
// saved before themes, and with its fields kept in range (spaces.json is
// the user's to edit).
func ThemeOf(s *Space) Theme {
	var t Theme
	if s != nil && s.Theme != nil {
		t = *s.Theme
	} else {
		t = Theme{Intensity: 0.8, Appearance: AppearanceAuto}
		if s != nil && ValidHex(s.Color) {
			// A space saved before themes keeps its one color on the side
			// it suits whatever the system shows, as Cra keeps an imported
			// single-color theme (Cra #171).
			t.Colors = []string{s.Color}
			t.Appearance = AppearanceLight
			if IsDarkHex(s.Color) {
				t.Appearance = AppearanceDark
			}
		}
	}
	var cs []string
	for _, c := range t.Colors {
		if ValidHex(c) && len(cs) < 3 {
			cs = append(cs, strings.ToUpper(c))
		}
	}
	if len(cs) == 0 {
		cs = []string{Brand[7]}
	}
	t.Colors = cs
	t.Intensity = clamp01(t.Intensity)
	t.Noise = clamp01(t.Noise)
	switch t.Appearance {
	case AppearanceLight, AppearanceDark:
	default:
		t.Appearance = AppearanceAuto
	}
	return t
}

// SetTheme gives the space a theme; Color follows its first color, for
// what shows a space by a single color.
func (s *Space) SetTheme(t Theme) {
	t.Colors = append([]string(nil), t.Colors...)
	s.Theme = &t
	if len(t.Colors) > 0 {
		s.Color = t.Colors[0]
	}
}

// Look is a theme as it is drawn while the system is light or dark: Arc's
// light or dark half of an Auto theme, or the one half of a Light or Dark
// one.
type Look struct {
	// Colors are the gradient's stops from top to bottom, at least two.
	Colors []string
	// Accent is the color the space's tones are made from.
	Accent string
	// Solid is a flat fill laid over a dark, see-through material at
	// SolidAlpha, rather than an opaque gradient.
	Solid bool
	// Dark says the colors are dark: what is over them is light.
	Dark      bool
	Intensity float64
	Noise     float64
}

// Look returns the theme as drawn while the system is dark or light.
func (t Theme) Look(systemDark bool) Look {
	t = ThemeOf(&Space{Theme: &t})
	// Auto always has both halves, one color too, as Cra's theme editor
	// writes them (SpaceThemeVariants, Cra #155): the color itself while
	// the system is light, its dark half while the system is dark. Adding
	// a second color then never changes which half shows.
	switch {
	case t.Appearance == AppearanceLight:
		return t.light()
	case t.Appearance == AppearanceDark:
		return t.dark()
	case systemDark:
		return t.dark()
	}
	return t.light()
}

// light is the half shown while the system is light: the colors
// themselves, lifted to HSL lightness 0.9 when they would read as dark.
// One color is a flat fill.
func (t Theme) light() Look {
	cs := append([]string(nil), t.Colors...)
	if IsDarkHex(cs[0]) {
		for i, c := range cs {
			cs[i] = Shade(c, 0.9)
		}
	}
	l := Look{Colors: cs, Accent: cs[0], Intensity: t.Intensity, Noise: t.Noise}
	if len(cs) == 1 {
		l.Colors = []string{cs[0], cs[0]}
		l.Solid = true
	}
	return l
}

// dark is the half shown while the system is dark: the key's hue at HSL
// lightness 0.064, drawn as Arc draws its opaque dark halves, 27% of the
// color over #2B2632.
func (t Theme) dark() Look {
	cs := make([]string, len(t.Colors))
	accent := ""
	for i, c := range t.Colors {
		base := Shade(c, 0.064)
		if i == 0 {
			accent = base
		}
		r, g, b := Components(base)
		ch := func(v, over float64) float64 { return (v*255*0.27 + over) / 255 }
		cs[i] = HexOf(ch(r, 43), ch(g, 38), ch(b, 50))
	}
	if len(cs) == 1 {
		cs = append(cs, cs[0])
	}
	return Look{Colors: cs, Accent: accent, Dark: true, Intensity: 1, Noise: t.Noise}
}

// SolidAlpha is how opaque a solid theme's color is over its material,
// fitted by Cra to Arc: 0.19 at intensity 0 to 0.74 at 1.
func SolidAlpha(intensity float64) float64 { return 0.19 + 0.55*clamp01(intensity) }

// ValidHex reports whether s is a "#RRGGBB" color.
func ValidHex(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	_, err := strconv.ParseUint(s[1:], 16, 32)
	return err == nil
}

// Components returns a hex color's sRGB channels, 0 to 1; mid grey for a
// malformed one.
func Components(hex string) (r, g, b float64) {
	if !ValidHex(hex) {
		return 0.5, 0.5, 0.5
	}
	v, _ := strconv.ParseUint(hex[1:], 16, 32)
	return float64(v>>16&0xff) / 255, float64(v>>8&0xff) / 255, float64(v&0xff) / 255
}

// HexOf returns the "#RRGGBB" of sRGB channels 0 to 1.
func HexOf(r, g, b float64) string {
	byte := func(v float64) int { return int(math.Round(clamp01(v) * 255)) }
	return fmt.Sprintf("#%02X%02X%02X", byte(r), byte(g), byte(b))
}

// Luminance is a hex color's luma, 0 to 1, by Rec. 709's weights on its
// sRGB values.
func Luminance(hex string) float64 {
	r, g, b := Components(hex)
	return 0.2126*r + 0.7152*g + 0.0722*b
}

// IsDarkHex reports whether what is over the color should be light.
func IsDarkHex(hex string) bool { return Luminance(hex) < 0.45 }

// HSB returns a color's hue in degrees, saturation and brightness.
func HSB(r, g, b float64) (h, s, v float64) {
	mx, mn := max(r, g, b), min(r, g, b)
	d := mx - mn
	if d > 1e-5 {
		switch mx {
		case r:
			h = math.Mod((g-b)/d, 6)
		case g:
			h = (b-r)/d + 2
		default:
			h = (r-g)/d + 4
		}
		h = math.Mod(h*60+360, 360)
	}
	if mx > 0 {
		s = d / mx
	}
	return h, s, mx
}

// FromHSB returns the channels of a hue in degrees (any, wrapped),
// saturation and brightness.
func FromHSB(h, s, v float64) (r, g, b float64) {
	h = math.Mod(math.Mod(h, 360)+360, 360) / 60
	s, v = clamp01(s), clamp01(v)
	i := math.Floor(h)
	f := h - i
	p, q, t := v*(1-s), v*(1-f*s), v*(1-(1-f)*s)
	switch int(i) % 6 {
	case 0:
		return v, t, p
	case 1:
		return q, v, p
	case 2:
		return p, v, t
	case 3:
		return p, q, v
	case 4:
		return t, p, v
	}
	return v, p, q
}

// WheelSpread is how far round the wheel the second and third colors sit
// from the first, in degrees.
const WheelSpread = 40.0

// WheelOffset is the hue offset of key color i: 0, then +spread, -spread.
func WheelOffset(i int) float64 {
	switch i {
	case 0:
		return 0
	case 1:
		return WheelSpread
	}
	return -WheelSpread
}

// WheelColor is the color at a place on the editor's wheel: its angle the
// hue, its distance from the center the saturation, at brightness 0.95.
func WheelColor(hue, saturation float64) string {
	return HexOf(FromHSB(hue, saturation, 0.95))
}

// Shade returns the color's HSL hue and saturation at another lightness.
func Shade(hex string, lightness float64) string {
	r, g, b := Components(hex)
	mx, mn := max(r, g, b), min(r, g, b)
	d := mx - mn
	l0 := (mx + mn) / 2
	s := 0.0
	if d != 0 {
		s = d / (1 - math.Abs(2*l0-1))
	}
	h := 0.0
	if d > 0 {
		switch mx {
		case r:
			h = math.Mod((g-b)/d, 6)
		case g:
			h = (b-r)/d + 2
		default:
			h = (r-g)/d + 4
		}
		h = math.Mod(h/6+1, 1)
	}
	q := (1 - math.Abs(2*lightness-1)) * min(1, s)
	x := q * (1 - math.Abs(math.Mod(h*6, 2)-1))
	m := lightness - q/2
	var rr, gg, bb float64
	switch int(h*6) % 6 {
	case 0:
		rr, gg, bb = q, x, 0
	case 1:
		rr, gg, bb = x, q, 0
	case 2:
		rr, gg, bb = 0, q, x
	case 3:
		rr, gg, bb = 0, x, q
	case 4:
		rr, gg, bb = x, 0, q
	default:
		rr, gg, bb = q, 0, x
	}
	return HexOf(rr+m, gg+m, bb+m)
}

// InkHex is the text over a light theme: the accent pushed to 13.5% of its
// brightness and a little more saturated (Arc's row titles).
func InkHex(accent string) string {
	h, s, v := HSB(Components(accent))
	return HexOf(FromHSB(h, min(1, s+0.10), v*0.135))
}

// ToolbarHex is Arc's band of the theme's color: a dark, fully saturated
// shade on light themes and a muted one on dark themes.
func ToolbarHex(accent string, dark bool) string {
	h, s, v := HSB(Components(accent))
	if dark {
		s, v = s*0.6, v*0.8
	} else {
		s, v = min(1, s+0.10), v*0.46
	}
	return HexOf(FromHSB(h, s, v))
}

// MidToneHex is Arc's palette midTone: the color while its OKLab lightness
// is within 0.68 to 0.794, else the most saturated color of its hue at the
// nearer bound.
func MidToneHex(accent string) string {
	base := paletteBase(accent)
	if oklabL(base) <= 0.794 {
		return HexOf(base[0], base[1], base[2])
	}
	e := edge(accent, 0.794)
	return HexOf(e[0], e[1], e[2])
}

func paletteBase(hex string) [3]float64 {
	r, g, b := Components(hex)
	c := [3]float64{r, g, b}
	if oklabL(c) >= 0.68 {
		return c
	}
	return edge(hex, 0.68)
}

func oklabL(c [3]float64) float64 {
	lin := func(x float64) float64 {
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	r, g, b := lin(c[0]), lin(c[1]), lin(c[2])
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return 0.2104542553*l + 0.7936177850*m - 0.0040720468*s
}

// edge is the color of hex's hue on the sRGB gamut's edge with the given
// OKLab lightness; greys stay grey.
func edge(hex string, target float64) [3]float64 {
	h, s, _ := HSB(Components(hex))
	at := func(s, v float64) [3]float64 { r, g, b := FromHSB(h, s, v); return [3]float64{r, g, b} }
	solve := func(f func(float64) float64) float64 {
		lo, hi := 0.0, 1.0
		for range 30 {
			x := (lo + hi) / 2
			if f(x) < target {
				lo = x
			} else {
				hi = x
			}
		}
		return (lo + hi) / 2
	}
	if s < 0.05 {
		return at(0, solve(func(v float64) float64 { return oklabL(at(0, v)) }))
	}
	if target <= oklabL(at(1, 1)) {
		return at(1, solve(func(v float64) float64 { return oklabL(at(1, v)) }))
	}
	return at(1-solve(func(x float64) float64 { return oklabL(at(1-x, 1)) }), 1)
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return min(1, max(0, v))
}

// TintedLightHex is Arc's palette tintedLight: the color, raised as for
// MidToneHex, mixed toward white until its OKLab lightness is 0.95.
func TintedLightHex(accent string) string {
	base := paletteBase(accent)
	if oklabL(base) >= 0.95 {
		return HexOf(base[0], base[1], base[2])
	}
	mix := func(t float64) [3]float64 {
		return [3]float64{base[0] + (1-base[0])*t, base[1] + (1-base[1])*t, base[2] + (1-base[2])*t}
	}
	lo, hi := 0.0, 1.0
	for range 30 {
		t := (lo + hi) / 2
		if oklabL(mix(t)) < 0.95 {
			lo = t
		} else {
			hi = t
		}
	}
	c := mix(lo)
	return HexOf(c[0], c[1], c[2])
}

// ShadedDarkHex is Arc's palette shadedDark: the color's hue at 94%
// saturation (greys stay grey) and 10% relative luminance.
func ShadedDarkHex(accent string) string {
	h, s, _ := HSB(Components(accent))
	ds := 0.94
	if s < 0.05 {
		ds = 0
	}
	lin := func(x float64) float64 {
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	lo, hi := 0.0, 1.0
	for range 20 {
		v := (lo + hi) / 2
		r, g, b := FromHSB(h, ds, v)
		if 0.2126*lin(r)+0.7152*lin(g)+0.0722*lin(b) < 0.1 {
			lo = v
		} else {
			hi = v
		}
	}
	return HexOf(FromHSB(h, ds, lo))
}

// FolderTones are the colors of Arc's folder glyph for a theme color: its
// outline, its front panel and its back panel. The back is the midTone on
// every theme; the front and the outline trade tintedLight and shadedDark
// between light and dark themes.
func FolderTones(accent string, dark bool) (outline, front, back string) {
	light, shaded := TintedLightHex(accent), ShadedDarkHex(accent)
	if dark {
		return light, shaded, MidToneHex(accent)
	}
	return shaded, light, MidToneHex(accent)
}
