package prefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
)

// StyleFileName is the file name of the user's custom document style config,
// stored alongside prefs.json.
const StyleFileName = "style.json"

// PaperPresets are the built-in selectable paper color preset IDs, where ""
// means ArcMD's default Feishu paper theme.
var PaperPresets = []string{"", "warm", "sepia", "sage", "slate"}

// IsValidPaperPreset reports whether id is a recognized built-in paper preset
// (including "" for the default theme).
func IsValidPaperPreset(id string) bool {
	return slices.Contains(PaperPresets, id)
}

// RGBA8 is an 8-bit per channel sRGB color with straight alpha (255 = opaque).
type RGBA8 struct {
	R, G, B, A uint8
}

// StyleConfig configures custom document typography, table alignment, and
// light/dark paper color overrides loaded from style.json.
type StyleConfig struct {
	BoldWeight        int          `json:"boldWeight,omitempty"`
	HeadingWeight     int          `json:"headingWeight,omitempty"`
	TableAlign        string       `json:"tableAlign,omitempty"`
	TableHeaderAlign  string       `json:"tableHeaderAlign,omitempty"`
	TableHeaderWeight int          `json:"tableHeaderWeight,omitempty"`
	Light             StylePalette `json:"light,omitzero"`
	Dark              StylePalette `json:"dark,omitzero"`
}

// StylePalette holds optional color overrides for either light or dark paper.
type StylePalette struct {
	Paper           string            `json:"paper,omitempty"`
	Text            string            `json:"text,omitempty"`
	Muted           string            `json:"muted,omitempty"`
	HeadingText     string            `json:"headingText,omitempty"`
	BoldText        string            `json:"boldText,omitempty"`
	Accent          string            `json:"accent,omitempty"`
	TableHeader     string            `json:"tableHeader,omitempty"`
	TableHeaderText string            `json:"tableHeaderText,omitempty"`
	TableLine       string            `json:"tableLine,omitempty"`
	TableStripe     string            `json:"tableStripe,omitempty"`
	Highlight       string            `json:"highlight,omitempty"`
	Highlights      map[string]string `json:"highlights,omitempty"`
	TextColors      map[string]string `json:"textColors,omitempty"`
	CellFills       map[string]string `json:"cellFills,omitempty"`
	CodeFill        string            `json:"codeFill,omitempty"`
	CodeBorder      string            `json:"codeBorder,omitempty"`
	CodeText        string            `json:"codeText,omitempty"`
	InlineCodeFill  string            `json:"inlineCodeFill,omitempty"`
	InlineCodeText  string            `json:"inlineCodeText,omitempty"`
	InlineTexFill   string            `json:"inlineTexFill,omitempty"`
	TexBlockFill    string            `json:"texBlockFill,omitempty"`
	QuoteBar        string            `json:"quoteBar,omitempty"`
}

// IsZero reports whether p defines no color overrides, supporting json omitzero.
func (p StylePalette) IsZero() bool {
	return p.Paper == "" &&
		p.Text == "" &&
		p.Muted == "" &&
		p.HeadingText == "" &&
		p.BoldText == "" &&
		p.Accent == "" &&
		p.TableHeader == "" &&
		p.TableHeaderText == "" &&
		p.TableLine == "" &&
		p.TableStripe == "" &&
		p.Highlight == "" &&
		len(p.Highlights) == 0 &&
		len(p.TextColors) == 0 &&
		len(p.CellFills) == 0 &&
		p.CodeFill == "" &&
		p.CodeBorder == "" &&
		p.CodeText == "" &&
		p.InlineCodeFill == "" &&
		p.InlineCodeText == "" &&
		p.InlineTexFill == "" &&
		p.TexBlockFill == "" &&
		p.QuoteBar == ""
}

// ParseColor parses CSS-like color strings in #RGB, #RGBA, #RRGGBB, #RRGGBBAA,
// rgb(r,g,b), and rgba(r,g,b,a) (with alpha as a 0..1 float or 0..255 integer).
func ParseColor(s string) (RGBA8, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return RGBA8{}, false
	}
	if s[0] == '#' {
		return parseHexColor(s[1:])
	}
	if body, ok := unwrapFunc(s, "rgb"); ok {
		parts := strings.Split(body, ",")
		if len(parts) != 3 {
			return RGBA8{}, false
		}
		r, okR := parseChannel8(parts[0])
		g, okG := parseChannel8(parts[1])
		b, okB := parseChannel8(parts[2])
		if !okR || !okG || !okB {
			return RGBA8{}, false
		}
		return RGBA8{R: r, G: g, B: b, A: 255}, true
	}
	if body, ok := unwrapFunc(s, "rgba"); ok {
		parts := strings.Split(body, ",")
		if len(parts) != 4 {
			return RGBA8{}, false
		}
		r, okR := parseChannel8(parts[0])
		g, okG := parseChannel8(parts[1])
		b, okB := parseChannel8(parts[2])
		a, okA := parseAlpha(parts[3])
		if !okR || !okG || !okB || !okA {
			return RGBA8{}, false
		}
		return RGBA8{R: r, G: g, B: b, A: a}, true
	}
	return RGBA8{}, false
}

func parseHexColor(hex string) (RGBA8, bool) {
	switch len(hex) {
	case 3:
		r, okR := hexNibble(hex[0])
		g, okG := hexNibble(hex[1])
		b, okB := hexNibble(hex[2])
		if !okR || !okG || !okB {
			return RGBA8{}, false
		}
		return RGBA8{R: r * 17, G: g * 17, B: b * 17, A: 255}, true
	case 4:
		r, okR := hexNibble(hex[0])
		g, okG := hexNibble(hex[1])
		b, okB := hexNibble(hex[2])
		a, okA := hexNibble(hex[3])
		if !okR || !okG || !okB || !okA {
			return RGBA8{}, false
		}
		return RGBA8{R: r * 17, G: g * 17, B: b * 17, A: a * 17}, true
	case 6:
		r, okR := hexByte(hex[0], hex[1])
		g, okG := hexByte(hex[2], hex[3])
		b, okB := hexByte(hex[4], hex[5])
		if !okR || !okG || !okB {
			return RGBA8{}, false
		}
		return RGBA8{R: r, G: g, B: b, A: 255}, true
	case 8:
		r, okR := hexByte(hex[0], hex[1])
		g, okG := hexByte(hex[2], hex[3])
		b, okB := hexByte(hex[4], hex[5])
		a, okA := hexByte(hex[6], hex[7])
		if !okR || !okG || !okB || !okA {
			return RGBA8{}, false
		}
		return RGBA8{R: r, G: g, B: b, A: a}, true
	default:
		return RGBA8{}, false
	}
}

func hexNibble(c byte) (uint8, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

func hexByte(hi, lo byte) (uint8, bool) {
	h, okH := hexNibble(hi)
	l, okL := hexNibble(lo)
	if !okH || !okL {
		return 0, false
	}
	return (h << 4) | l, true
}

func unwrapFunc(s, name string) (string, bool) {
	if len(s) < len(name)+2 {
		return "", false
	}
	if !strings.EqualFold(s[:len(name)], name) || s[len(name)] != '(' || s[len(s)-1] != ')' {
		return "", false
	}
	return s[len(name)+1 : len(s)-1], true
}

func parseChannel8(s string) (uint8, bool) {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 8)
	if err != nil {
		return 0, false
	}
	return uint8(v), true
}

func parseAlpha(s string) (uint8, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if strings.Contains(s, ".") {
		if s == "." {
			return 0, false
		}
		dots := 0
		for i := 0; i < len(s); i++ {
			c := s[i]
			if c == '.' {
				dots++
				if dots > 1 {
					return 0, false
				}
				continue
			}
			if c < '0' || c > '9' {
				return 0, false
			}
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || f < 0 || f > 1 {
			return 0, false
		}
		return uint8(math.Round(f * 255)), true
	}
	v, err := strconv.ParseUint(s, 10, 8)
	if err != nil {
		return 0, false
	}
	if v == 1 {
		return 255, true
	}
	return uint8(v), true
}

// ParseStyle parses and validates style.json bytes into a StyleConfig.
func ParseStyle(data []byte) (*StyleConfig, error) {
	var cfg StyleConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("style: %w", err)
	}
	var err error
	if cfg.BoldWeight, err = sanitizeWeight("boldWeight", cfg.BoldWeight); err != nil {
		return nil, err
	}
	if cfg.HeadingWeight, err = sanitizeWeight("headingWeight", cfg.HeadingWeight); err != nil {
		return nil, err
	}
	if cfg.TableHeaderWeight, err = sanitizeWeight("tableHeaderWeight", cfg.TableHeaderWeight); err != nil {
		return nil, err
	}
	if err := validateAlign("tableAlign", cfg.TableAlign); err != nil {
		return nil, err
	}
	if err := validateAlign("tableHeaderAlign", cfg.TableHeaderAlign); err != nil {
		return nil, err
	}
	if err := validatePalette("light", cfg.Light); err != nil {
		return nil, err
	}
	if err := validatePalette("dark", cfg.Dark); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func sanitizeWeight(field string, w int) (int, error) {
	if w < 0 {
		return 0, fmt.Errorf("style: invalid %s %d: weight must not be negative", field, w)
	}
	if w == 0 {
		return 0, nil
	}
	if w < 400 {
		return 400, nil
	}
	if w > 900 {
		return 900, nil
	}
	return w, nil
}

func validateAlign(field, align string) error {
	switch align {
	case "", "left", "center", "right":
		return nil
	default:
		return fmt.Errorf("style: invalid %s %q: must be left, center, or right", field, align)
	}
}

func validatePalette(prefix string, p StylePalette) error {
	fields := []struct {
		name  string
		value string
	}{
		{"paper", p.Paper},
		{"text", p.Text},
		{"muted", p.Muted},
		{"headingText", p.HeadingText},
		{"boldText", p.BoldText},
		{"accent", p.Accent},
		{"tableHeader", p.TableHeader},
		{"tableHeaderText", p.TableHeaderText},
		{"tableLine", p.TableLine},
		{"tableStripe", p.TableStripe},
		{"highlight", p.Highlight},
		{"codeFill", p.CodeFill},
		{"codeBorder", p.CodeBorder},
		{"codeText", p.CodeText},
		{"inlineCodeFill", p.InlineCodeFill},
		{"inlineCodeText", p.InlineCodeText},
		{"inlineTexFill", p.InlineTexFill},
		{"texBlockFill", p.TexBlockFill},
		{"quoteBar", p.QuoteBar},
	}
	for _, f := range fields {
		if f.value == "" {
			continue
		}
		if _, ok := ParseColor(f.value); !ok {
			return fmt.Errorf("style: invalid color for %s.%s: %q", prefix, f.name, f.value)
		}
	}
	maps := []struct {
		name   string
		values map[string]string
	}{
		{"highlights", p.Highlights},
		{"textColors", p.TextColors},
		{"cellFills", p.CellFills},
	}
	for _, m := range maps {
		if len(m.values) == 0 {
			continue
		}
		keys := make([]string, 0, len(m.values))
		for k := range m.values {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			val := m.values[k]
			if val == "" {
				continue
			}
			if _, ok := ParseColor(val); !ok {
				return fmt.Errorf("style: invalid color for %s.%s.%s: %q", prefix, m.name, k, val)
			}
		}
	}
	return nil
}

// LoadStyleFile reads and validates a style.json file at path, returning
// (nil, nil) when the file does not exist.
func LoadStyleFile(path string) (*StyleConfig, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseStyle(data)
}

// DefaultStyleTemplateJSON returns a formatted starter style.json populated
// with ArcMD's Feishu light and dark paper defaults so users can edit any
// token immediately.
func DefaultStyleTemplateJSON() []byte {
	tpl := StyleConfig{
		BoldWeight:       600,
		HeadingWeight:    500,
		TableAlign:       "left",
		TableHeaderAlign: "left",
		Light: StylePalette{
			Paper:           "#FFFFFF",
			Text:            "#1F2329",
			Muted:           "#646A73",
			HeadingText:     "#1F2329",
			BoldText:        "#1F2329",
			Accent:          "#336DF4",
			TableHeader:     "#F2F3F5",
			TableHeaderText: "#1F2329",
			TableLine:       "#DEE0E3",
			Highlight:       "#FFF67ACC",
			CodeFill:        "#F5F6F7",
			CodeBorder:      "#DEE0E3",
			CodeText:        "#2B2F36",
			InlineCodeFill:  "#F2F3F5",
			InlineCodeText:  "#2B2F36",
			QuoteBar:        "#BBBFC4",
		},
		Dark: StylePalette{
			Paper:           "#1F2023",
			Text:            "#E3E4E7",
			Muted:           "#A2A5AB",
			HeadingText:     "#E3E4E7",
			BoldText:        "#E3E4E7",
			Accent:          "#4C88FF",
			TableHeader:     "#28292D",
			TableHeaderText: "#E3E4E7",
			TableLine:       "#393B40",
			Highlight:       "#FFE92873",
			CodeFill:        "#27282C",
			CodeBorder:      "#36383D",
			CodeText:        "#D5D7DB",
			InlineCodeFill:  "#303136",
			InlineCodeText:  "#D5D7DB",
			QuoteBar:        "#5A5D63",
		},
	}
	data, err := json.MarshalIndent(tpl, "", "  ")
	if err != nil {
		return []byte("{}\n")
	}
	return append(data, '\n')
}

// BuiltinPaperStyle returns a fresh StyleConfig for a built-in paper color
// preset ("warm", "sepia", "sage", "slate"), or nil for "" or unknown IDs.
func BuiltinPaperStyle(id string) *StyleConfig {
	switch id {
	case "warm":
		return &StyleConfig{
			Light: StylePalette{
				Paper:          "#FAF8F3",
				Text:           "#26231E",
				Muted:          "#6E675E",
				HeadingText:    "#1D1A15",
				Accent:         "#C2592B",
				TableHeader:    "#F2EEE5",
				TableLine:      "#E3DDD1",
				TableStripe:    "#F6F3EB",
				Highlight:      "#FDE68ACC",
				CodeFill:       "#F3EFE7",
				CodeBorder:     "#E3DDD1",
				CodeText:       "#2E2A24",
				InlineCodeFill: "#EFEBE1",
				InlineCodeText: "#2E2A24",
				QuoteBar:       "#C8BFB0",
			},
			Dark: StylePalette{
				Paper:          "#23201C",
				Text:           "#EAE4DA",
				Muted:          "#A69E93",
				HeadingText:    "#F3EEE6",
				Accent:         "#E58A5E",
				TableHeader:    "#2C2823",
				TableLine:      "#3D3831",
				TableStripe:    "#27241F",
				Highlight:      "#F59E0B59",
				CodeFill:       "#2A2621",
				CodeBorder:     "#3B352E",
				CodeText:       "#DFD8CD",
				InlineCodeFill: "#332E28",
				InlineCodeText: "#DFD8CD",
				QuoteBar:       "#635B50",
			},
		}
	case "sepia":
		return &StyleConfig{
			Light: StylePalette{
				Paper:          "#F4ECD8",
				Text:           "#2F261D",
				Muted:          "#756757",
				HeadingText:    "#231B13",
				Accent:         "#9E472A",
				TableHeader:    "#EAE0C8",
				TableLine:      "#D9CCB0",
				TableStripe:    "#EFE6D0",
				Highlight:      "#F8D878BF",
				CodeFill:       "#ECE2CA",
				CodeBorder:     "#D9CCB0",
				CodeText:       "#362C22",
				InlineCodeFill: "#E7DCC2",
				InlineCodeText: "#362C22",
				QuoteBar:       "#BFB093",
			},
			Dark: StylePalette{
				Paper:          "#262019",
				Text:           "#E6DAC6",
				Muted:          "#A69780",
				HeadingText:    "#EFE5D4",
				Accent:         "#DE8763",
				TableHeader:    "#30281F",
				TableLine:      "#42382C",
				TableStripe:    "#2B241C",
				Highlight:      "#EAB30854",
				CodeFill:       "#2E261E",
				CodeBorder:     "#40362A",
				CodeText:       "#DDD0BA",
				InlineCodeFill: "#382F25",
				InlineCodeText: "#DDD0BA",
				QuoteBar:       "#695A47",
			},
		}
	case "sage":
		return &StyleConfig{
			Light: StylePalette{
				Paper:          "#F2F6F2",
				Text:           "#1E2822",
				Muted:          "#5D6E63",
				HeadingText:    "#16201A",
				Accent:         "#2D7A56",
				TableHeader:    "#E6EEE7",
				TableLine:      "#D3DFD5",
				TableStripe:    "#ECF2EC",
				Highlight:      "#EBF599CC",
				CodeFill:       "#E8F0E9",
				CodeBorder:     "#D3DFD5",
				CodeText:       "#243029",
				InlineCodeFill: "#E3ECE4",
				InlineCodeText: "#243029",
				QuoteBar:       "#B0C2B4",
			},
			Dark: StylePalette{
				Paper:          "#1B221E",
				Text:           "#DFE8E2",
				Muted:          "#96A69B",
				HeadingText:    "#EAF2ED",
				Accent:         "#5BB98B",
				TableHeader:    "#232C27",
				TableLine:      "#334039",
				TableStripe:    "#1F2723",
				Highlight:      "#4ADE8047",
				CodeFill:       "#212A25",
				CodeBorder:     "#313E37",
				CodeText:       "#D4DFD8",
				InlineCodeFill: "#29342E",
				InlineCodeText: "#D4DFD8",
				QuoteBar:       "#4F6357",
			},
		}
	case "slate":
		return &StyleConfig{
			Light: StylePalette{
				Paper:          "#F4F6F9",
				Text:           "#1E2430",
				Muted:          "#5E6778",
				HeadingText:    "#151A24",
				Accent:         "#3B66D6",
				TableHeader:    "#EAEFF5",
				TableLine:      "#D7DFEA",
				TableStripe:    "#EEF2F7",
				Highlight:      "#FDE68ACC",
				CodeFill:       "#EBF0F6",
				CodeBorder:     "#D7DFEA",
				CodeText:       "#252C3A",
				InlineCodeFill: "#E6ECF3",
				InlineCodeText: "#252C3A",
				QuoteBar:       "#B4C0D0",
			},
			Dark: StylePalette{
				Paper:          "#1B1F27",
				Text:           "#E1E6F0",
				Muted:          "#98A2B3",
				HeadingText:    "#ECF0F8",
				Accent:         "#6894FF",
				TableHeader:    "#232833",
				TableLine:      "#333A48",
				TableStripe:    "#1F242D",
				Highlight:      "#60A5FA4D",
				CodeFill:       "#212630",
				CodeBorder:     "#313846",
				CodeText:       "#D5DCE8",
				InlineCodeFill: "#29303D",
				InlineCodeText: "#D5DCE8",
				QuoteBar:       "#4F596E",
			},
		}
	default:
		return nil
	}
}

// MergeStyleConfig clones base and overlays every non-zero/non-empty field and
// map entry from override on top, returning nil when both inputs are nil.
func MergeStyleConfig(base, override *StyleConfig) *StyleConfig {
	if base == nil && override == nil {
		return nil
	}
	var out StyleConfig
	if base != nil {
		applyStyleConfig(&out, base)
	}
	if override != nil {
		applyStyleConfig(&out, override)
	}
	return &out
}

func applyStyleConfig(dst, src *StyleConfig) {
	if src.BoldWeight != 0 {
		dst.BoldWeight = src.BoldWeight
	}
	if src.HeadingWeight != 0 {
		dst.HeadingWeight = src.HeadingWeight
	}
	if src.TableAlign != "" {
		dst.TableAlign = src.TableAlign
	}
	if src.TableHeaderAlign != "" {
		dst.TableHeaderAlign = src.TableHeaderAlign
	}
	if src.TableHeaderWeight != 0 {
		dst.TableHeaderWeight = src.TableHeaderWeight
	}
	applyStylePalette(&dst.Light, src.Light)
	applyStylePalette(&dst.Dark, src.Dark)
}

func applyStylePalette(dst *StylePalette, src StylePalette) {
	if src.Paper != "" {
		dst.Paper = src.Paper
	}
	if src.Text != "" {
		dst.Text = src.Text
	}
	if src.Muted != "" {
		dst.Muted = src.Muted
	}
	if src.HeadingText != "" {
		dst.HeadingText = src.HeadingText
	}
	if src.BoldText != "" {
		dst.BoldText = src.BoldText
	}
	if src.Accent != "" {
		dst.Accent = src.Accent
	}
	if src.TableHeader != "" {
		dst.TableHeader = src.TableHeader
	}
	if src.TableHeaderText != "" {
		dst.TableHeaderText = src.TableHeaderText
	}
	if src.TableLine != "" {
		dst.TableLine = src.TableLine
	}
	if src.TableStripe != "" {
		dst.TableStripe = src.TableStripe
	}
	if src.Highlight != "" {
		dst.Highlight = src.Highlight
	}
	mergeColorMap(&dst.Highlights, src.Highlights)
	mergeColorMap(&dst.TextColors, src.TextColors)
	mergeColorMap(&dst.CellFills, src.CellFills)
	if src.CodeFill != "" {
		dst.CodeFill = src.CodeFill
	}
	if src.CodeBorder != "" {
		dst.CodeBorder = src.CodeBorder
	}
	if src.CodeText != "" {
		dst.CodeText = src.CodeText
	}
	if src.InlineCodeFill != "" {
		dst.InlineCodeFill = src.InlineCodeFill
	}
	if src.InlineCodeText != "" {
		dst.InlineCodeText = src.InlineCodeText
	}
	if src.InlineTexFill != "" {
		dst.InlineTexFill = src.InlineTexFill
	}
	if src.TexBlockFill != "" {
		dst.TexBlockFill = src.TexBlockFill
	}
	if src.QuoteBar != "" {
		dst.QuoteBar = src.QuoteBar
	}
}

func mergeColorMap(dst *map[string]string, src map[string]string) {
	if len(src) == 0 {
		return
	}
	if *dst == nil {
		*dst = make(map[string]string, len(src))
	}
	for k, v := range src {
		if v != "" {
			(*dst)[k] = v
		}
	}
	if len(*dst) == 0 {
		*dst = nil
	}
}
