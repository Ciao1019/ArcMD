package prefs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseColorValidFormats(t *testing.T) {
	cases := []struct {
		in   string
		want RGBA8
	}{
		{"#f0a", RGBA8{0xFF, 0x00, 0xAA, 0xFF}},
		{"  #09C  ", RGBA8{0x00, 0x99, 0xCC, 0xFF}},
		{"#f0a8", RGBA8{0xFF, 0x00, 0xAA, 0x88}},
		{"#1F2329", RGBA8{0x1F, 0x23, 0x29, 0xFF}},
		{"#336df4", RGBA8{0x33, 0x6D, 0xF4, 0xFF}},
		{"#FFF67ACC", RGBA8{0xFF, 0xF6, 0x7A, 0xCC}},
		{"#00000000", RGBA8{0x00, 0x00, 0x00, 0x00}},
		{"rgb(31, 35, 41)", RGBA8{31, 35, 41, 255}},
		{"RGB( 0 , 128 , 255 )", RGBA8{0, 128, 255, 255}},
		{"rgba(255, 246, 122, 0)", RGBA8{255, 246, 122, 0}},
		{"rgba(255, 246, 122, 0.0)", RGBA8{255, 246, 122, 0}},
		{"rgba(255, 246, 122, 0.5)", RGBA8{255, 246, 122, 128}},
		{"rgba(255, 246, 122, .25)", RGBA8{255, 246, 122, 64}},
		{"rgba(255, 246, 122, 1)", RGBA8{255, 246, 122, 255}},
		{"rgba(255, 246, 122, 1.0)", RGBA8{255, 246, 122, 255}},
		{"rgba(255, 246, 122, 204)", RGBA8{255, 246, 122, 204}},
		{"RGBA(10, 20, 30, 255)", RGBA8{10, 20, 30, 255}},
	}
	for _, tc := range cases {
		got, ok := ParseColor(tc.in)
		if !ok {
			t.Errorf("ParseColor(%q) = false; want %+v", tc.in, tc.want)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseColor(%q) = %+v; want %+v", tc.in, got, tc.want)
		}
	}
}

func TestParseColorInvalidFormats(t *testing.T) {
	invalid := []string{
		"",
		"   ",
		"#",
		"#12",
		"#12345",
		"#1234567",
		"#123456789",
		"#GGG",
		"#12345G",
		"1F2329",
		"red",
		"rgb()",
		"rgb(1, 2)",
		"rgb(1, 2, 3, 4)",
		"rgb(256, 0, 0)",
		"rgb(-1, 0, 0)",
		"rgb(12.5, 0, 0)",
		"rgba(1, 2, 3)",
		"rgba(1, 2, 3, )",
		"rgba(1, 2, 3, -0.1)",
		"rgba(1, 2, 3, -5)",
		"rgba(1, 2, 3, 1.5)",
		"rgba(1, 2, 3, 256)",
		"rgba(1, 2, 3, .)",
		"rgba(1, 2, 3, 0.5.1)",
		"rgba(1, 2, 3, NaN)",
		"hsl(120, 50%, 50%)",
	}
	for _, s := range invalid {
		if got, ok := ParseColor(s); ok {
			t.Errorf("ParseColor(%q) = (%+v, true); want false", s, got)
		}
	}
}

func TestParseStyleValidAndWeightClamping(t *testing.T) {
	raw := []byte(`{
		"boldWeight": 250,
		"headingWeight": 950,
		"tableHeaderWeight": 600,
		"tableAlign": "center",
		"tableHeaderAlign": "right",
		"light": {
			"paper": "#FAF9F5",
			"text": "rgb(30, 32, 36)",
			"tableHeader": "#EFECE6",
			"tableStripe": "rgba(0, 0, 0, 0.03)",
			"highlight": "#FFE880CC",
			"highlights": {"yellow": "#FFF3A1"},
			"textColors": {"blue": "#1D4ED8"},
			"cellFills": {"green": "#DCFCE7"}
		},
		"dark": {
			"paper": "#18191C",
			"text": "#E5E7EB"
		}
	}`)
	cfg, err := ParseStyle(raw)
	if err != nil {
		t.Fatalf("ParseStyle unexpected error: %v", err)
	}
	if cfg.BoldWeight != 400 {
		t.Fatalf("BoldWeight = %d; want clamped 400", cfg.BoldWeight)
	}
	if cfg.HeadingWeight != 900 {
		t.Fatalf("HeadingWeight = %d; want clamped 900", cfg.HeadingWeight)
	}
	if cfg.TableHeaderWeight != 600 {
		t.Fatalf("TableHeaderWeight = %d; want 600", cfg.TableHeaderWeight)
	}
	if cfg.TableAlign != "center" || cfg.TableHeaderAlign != "right" {
		t.Fatalf("alignments = (%q, %q); want (center, right)", cfg.TableAlign, cfg.TableHeaderAlign)
	}
	if cfg.Light.Highlights["yellow"] != "#FFF3A1" || cfg.Light.TextColors["blue"] != "#1D4ED8" || cfg.Light.CellFills["green"] != "#DCFCE7" {
		t.Fatalf("maps not preserved: %+v", cfg.Light)
	}

	// Empty JSON object leaves weights at 0 and palettes zero.
	emptyCfg, err := ParseStyle([]byte(`{}`))
	if err != nil {
		t.Fatalf("ParseStyle({}) error: %v", err)
	}
	if emptyCfg.BoldWeight != 0 || emptyCfg.HeadingWeight != 0 || emptyCfg.TableHeaderWeight != 0 {
		t.Fatalf("zero weights should stay 0, got %+v", emptyCfg)
	}
	if !emptyCfg.Light.IsZero() || !emptyCfg.Dark.IsZero() {
		t.Fatalf("expected empty palettes to be zero: %+v", emptyCfg)
	}
	marshaled, err := json.Marshal(emptyCfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(marshaled) != "{}" {
		t.Fatalf("omitzero failed on empty StyleConfig: %s", marshaled)
	}
}

func TestParseStyleValidationErrors(t *testing.T) {
	cases := []struct {
		name      string
		json      string
		wantField string
	}{
		{"malformed json", `{bad`, "style:"},
		{"negative boldWeight", `{"boldWeight": -100}`, "boldWeight"},
		{"negative headingWeight", `{"headingWeight": -1}`, "headingWeight"},
		{"negative tableHeaderWeight", `{"tableHeaderWeight": -400}`, "tableHeaderWeight"},
		{"invalid tableAlign", `{"tableAlign": "justify"}`, "tableAlign"},
		{"invalid tableHeaderAlign", `{"tableHeaderAlign": "middle"}`, "tableHeaderAlign"},
		{"invalid light.paper", `{"light": {"paper": "not-a-color"}}`, "light.paper"},
		{"invalid dark.tableHeader", `{"dark": {"tableHeader": "#12"}}`, "dark.tableHeader"},
		{"invalid light.highlight", `{"light": {"highlight": "   "}}`, "light.highlight"},
		{"invalid map highlight", `{"light": {"highlights": {"yellow": "#GGGGGG"}}}`, "light.highlights.yellow"},
		{"invalid map textColor", `{"dark": {"textColors": {"red": "rgb(999,0,0)"}}}`, "dark.textColors.red"},
		{"invalid map cellFill", `{"light": {"cellFills": {"blue": "rgba(0,0,0,2.5)"}}}`, "light.cellFills.blue"},
	}
	for _, tc := range cases {
		_, err := ParseStyle([]byte(tc.json))
		if err == nil {
			t.Errorf("%s: ParseStyle(%s) = nil error; want error mentioning %q", tc.name, tc.json, tc.wantField)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantField) {
			t.Errorf("%s: error = %q; want substring %q", tc.name, err.Error(), tc.wantField)
		}
	}
}

func TestLoadStyleFileAndDefaultTemplate(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, StyleFileName)

	cfg, err := LoadStyleFile(missingPath)
	if err != nil || cfg != nil {
		t.Fatalf("LoadStyleFile(missing) = (%+v, %v); want (nil, nil)", cfg, err)
	}
	if cfg, err := LoadStyleFile(""); err != nil || cfg != nil {
		t.Fatalf("LoadStyleFile(\"\") = (%+v, %v); want (nil, nil)", cfg, err)
	}

	tpl := DefaultStyleTemplateJSON()
	if len(tpl) == 0 {
		t.Fatal("DefaultStyleTemplateJSON returned empty bytes")
	}
	parsedTpl, err := ParseStyle(tpl)
	if err != nil {
		t.Fatalf("DefaultStyleTemplateJSON failed ParseStyle: %v\n%s", err, tpl)
	}
	if parsedTpl.BoldWeight == 0 || parsedTpl.Light.Paper == "" || parsedTpl.Dark.Paper == "" {
		t.Fatalf("DefaultStyleTemplateJSON missing starter fields: %+v", parsedTpl)
	}

	if err := os.WriteFile(missingPath, tpl, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadStyleFile(missingPath)
	if err != nil || loaded == nil {
		t.Fatalf("LoadStyleFile(valid) = (%+v, %v)", loaded, err)
	}
	if loaded.Light.Paper != "#FFFFFF" || loaded.Dark.Paper != "#1F2023" {
		t.Fatalf("unexpected template paper defaults: light=%q dark=%q", loaded.Light.Paper, loaded.Dark.Paper)
	}

	if err := os.WriteFile(missingPath, []byte(`{"tableAlign":"diagonal"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStyleFile(missingPath); err == nil {
		t.Fatal("LoadStyleFile(invalid) expected error, got nil")
	}
}

func TestBuiltinPaperStyleAndMerge(t *testing.T) {
	if BuiltinPaperStyle("") != nil || BuiltinPaperStyle("unknown") != nil {
		t.Fatal("BuiltinPaperStyle should return nil for empty or unknown ID")
	}
	for _, id := range []string{"warm", "sepia", "sage", "slate"} {
		cfg := BuiltinPaperStyle(id)
		if cfg == nil {
			t.Fatalf("BuiltinPaperStyle(%q) = nil", id)
		}
		// Must return a fresh copy on each call.
		other := BuiltinPaperStyle(id)
		other.Light.Paper = "#000000"
		if cfg.Light.Paper == "#000000" {
			t.Fatalf("BuiltinPaperStyle(%q) returned shared pointer", id)
		}
		// Every preset must serialize cleanly and pass ParseStyle validation.
		raw, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("marshal %q: %v", id, err)
		}
		validated, err := ParseStyle(raw)
		if err != nil {
			t.Fatalf("BuiltinPaperStyle(%q) failed ParseStyle: %v", id, err)
		}
		for _, pal := range []struct {
			mode string
			p    StylePalette
		}{
			{"light", validated.Light},
			{"dark", validated.Dark},
		} {
			if pal.p.Paper == "" || pal.p.Text == "" || pal.p.Muted == "" || pal.p.HeadingText == "" ||
				pal.p.Accent == "" || pal.p.TableHeader == "" || pal.p.TableLine == "" || pal.p.TableStripe == "" ||
				pal.p.CodeFill == "" || pal.p.CodeBorder == "" || pal.p.InlineCodeFill == "" ||
				pal.p.QuoteBar == "" || pal.p.Highlight == "" {
				t.Fatalf("preset %q %s missing required palette token: %+v", id, pal.mode, pal.p)
			}
		}
	}

	if MergeStyleConfig(nil, nil) != nil {
		t.Fatal("MergeStyleConfig(nil, nil) != nil")
	}

	base := BuiltinPaperStyle("sepia")
	base.Light.Highlights = map[string]string{"yellow": "#112233", "green": "#223344"}
	onlyBase := MergeStyleConfig(base, nil)
	onlyBase.Light.Paper = "#ABCDEF"
	onlyBase.Light.Highlights["yellow"] = "#FFFFFF"
	if base.Light.Paper == "#ABCDEF" || base.Light.Highlights["yellow"] == "#FFFFFF" {
		t.Fatal("MergeStyleConfig(base, nil) mutated base")
	}

	override := &StyleConfig{
		BoldWeight: 700,
		TableAlign: "center",
		Light: StylePalette{
			Paper:      "#FFFDF8",
			BoldText:   "#111111",
			Highlights: map[string]string{"yellow": "#FFEE88", "blue": "#AACCEE"},
		},
		Dark: StylePalette{
			Accent: "#88AAFF",
		},
	}
	merged := MergeStyleConfig(base, override)
	if merged.BoldWeight != 700 || merged.TableAlign != "center" {
		t.Fatalf("top-level override not applied: %+v", merged)
	}
	if merged.Light.Paper != "#FFFDF8" || merged.Light.BoldText != "#111111" {
		t.Fatalf("light override not applied: %+v", merged.Light)
	}
	if merged.Light.Text != base.Light.Text || merged.Light.TableHeader != base.Light.TableHeader {
		t.Fatalf("base light fields lost during merge: %+v", merged.Light)
	}
	if merged.Dark.Paper != base.Dark.Paper || merged.Dark.Accent != "#88AAFF" {
		t.Fatalf("dark merge mismatch: %+v", merged.Dark)
	}
	if merged.Light.Highlights["yellow"] != "#FFEE88" || merged.Light.Highlights["green"] != "#223344" || merged.Light.Highlights["blue"] != "#AACCEE" {
		t.Fatalf("map merge mismatch: %+v", merged.Light.Highlights)
	}
}
