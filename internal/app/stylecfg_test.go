package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/prefs"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestStyleConfigMappingLightAndDark(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	styleJSON := `{
  "boldWeight": 700,
  "headingWeight": 600,
  "tableAlign": "center",
  "tableHeaderAlign": "right",
  "tableHeaderWeight": 700,
  "light": {
    "paper": "#FAF7F0",
    "text": "#23262B",
    "muted": "#6E737B",
    "headingText": "#1A365D",
    "boldText": "#7C2D12",
    "accent": "#2B6CB0",
    "tableHeader": "#EFE9DB",
    "tableHeaderText": "#1A365D",
    "tableLine": "#DDD4C2",
    "tableStripe": "#F5F0E6",
    "highlight": "rgba(253, 230, 138, 0.8)",
    "highlights": { "green": "#BBF7D0" },
    "textColors": { "red": "#B91C1C" },
    "cellFills": { "blue": "#DBEAFE" },
    "codeFill": "#F3EFE6",
    "codeBorder": "#E2DAC9",
    "codeText": "#1F2937",
    "inlineCodeFill": "#EDE8DC",
    "inlineCodeText": "#9A3412",
    "inlineTexFill": "#EFF6FF",
    "texBlockFill": "#F2F7FF",
    "quoteBar": "#94A3B8"
  },
  "dark": {
    "paper": "#181A1F",
    "text": "#E5E7EB",
    "muted": "#9CA3AF",
    "headingText": "#93C5FD",
    "boldText": "#FDBA74",
    "accent": "#60A5FA",
    "tableHeader": "#242832",
    "tableHeaderText": "#F3F4F6",
    "tableLine": "#374151",
    "tableStripe": "#1E222B",
    "highlight": "#FACC1566",
    "codeFill": "#1F242D",
    "codeBorder": "#374151",
    "codeText": "#E5E7EB",
    "inlineCodeFill": "#2A303C",
    "inlineCodeText": "#FDBA74",
    "inlineTexFill": "#283552",
    "texBlockFill": "#242F46",
    "quoteBar": "#4B5563"
  }
}`
	if err := os.WriteFile(filepath.Join(cfgDir, prefs.StyleFileName), []byte(styleJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := New(Config{ConfigDir: cfgDir, DocumentsDir: filepath.Join(dir, "docs")})
	if err != nil {
		t.Fatal(err)
	}
	a.tour = tour{}
	t.Cleanup(a.closePlugins)
	tt := ui.NewTester(a.View, 1200, 760)
	tt.Frame()

	lt := a.documentTheme(false)
	lp := a.pageStyle(false)
	if lt.Background != ui.Hex("#FAF7F0") || lt.Text != ui.Hex("#23262B") || lt.TextMuted != ui.Hex("#6E737B") || lt.Accent != ui.Hex("#2B6CB0") {
		t.Fatalf("light theme mismatch: bg=%+v text=%+v muted=%+v accent=%+v", lt.Background, lt.Text, lt.TextMuted, lt.Accent)
	}
	if lp.BoldWeight != 700 || lp.HeadingWeight != 600 || lp.TableAlign != "center" || lp.TableHeaderAlign != "right" || lp.TableHeaderWeight != 700 {
		t.Fatalf("typography/table config mismatch: %+v", lp)
	}
	if lp.HeadingText != ui.Hex("#1A365D") || lp.BoldText != ui.Hex("#7C2D12") || lp.TableHeader != ui.Hex("#EFE9DB") || lp.TableHeaderText != ui.Hex("#1A365D") {
		t.Fatalf("light page colors mismatch: heading=%+v bold=%+v th=%+v thText=%+v", lp.HeadingText, lp.BoldText, lp.TableHeader, lp.TableHeaderText)
	}
	if lp.TableLine != ui.Hex("#DDD4C2") || lp.TableStripe != ui.Hex("#F5F0E6") || lp.Highlights["green"] != ui.Hex("#BBF7D0") || lp.TextColors["red"] != ui.Hex("#B91C1C") || lp.CellFills["blue"] != ui.Hex("#DBEAFE") {
		t.Fatalf("light table/map colors mismatch")
	}
	if lp.CodeFill != ui.Hex("#F3EFE6") || lp.CodeBorder != ui.Hex("#E2DAC9") || lp.CodeText != ui.Hex("#1F2937") || lp.InlineFill != ui.Hex("#EDE8DC") || lp.InlineText != ui.Hex("#9A3412") || lp.QuoteBar != ui.Hex("#94A3B8") {
		t.Fatalf("light code/quote colors mismatch")
	}
	if lp.InlineTexFill != ui.Hex("#EFF6FF") || lp.TexBlockFill != ui.Hex("#F2F7FF") {
		t.Fatal("light TeX colors mismatch")
	}
	if lp.HighlightDefault.R != 253 || lp.HighlightDefault.G != 230 || lp.HighlightDefault.B != 138 || lp.HighlightDefault.A == 0 {
		t.Fatalf("light default highlight mismatch: %+v", lp.HighlightDefault)
	}

	dt := a.documentTheme(true)
	dp := a.pageStyle(true)
	if dt.Background != ui.Hex("#181A1F") || dt.Text != ui.Hex("#E5E7EB") || dt.Accent != ui.Hex("#60A5FA") {
		t.Fatalf("dark theme mismatch: bg=%+v text=%+v accent=%+v", dt.Background, dt.Text, dt.Accent)
	}
	if dp.HeadingText != ui.Hex("#93C5FD") || dp.BoldText != ui.Hex("#FDBA74") || dp.TableStripe != ui.Hex("#1E222B") || dp.HighlightDefault != ui.Hex("#FACC1566") {
		t.Fatalf("dark page colors mismatch: heading=%+v bold=%+v stripe=%+v hl=%+v", dp.HeadingText, dp.BoldText, dp.TableStripe, dp.HighlightDefault)
	}
	if dp.InlineTexFill != ui.Hex("#283552") || dp.TexBlockFill != ui.Hex("#242F46") {
		t.Fatal("dark TeX colors mismatch")
	}

	// Uncustomized fallback helpers stay untouched for PDF export and baseline tests.
	if base := documentTheme(false); base.Background != ui.Hex("#FFFFFF") {
		t.Fatalf("baseline documentTheme(false) mutated: %+v", base.Background)
	}
}

func TestStyleConfigLiveReloadErrorResilienceAndRemoval(t *testing.T) {
	a, tt := newTestApp(t)
	stylePath := a.styleConfigPath()
	baseMtime := time.Now().Add(-10 * time.Second)

	writeWithMtime := func(content string, offset time.Duration) {
		t.Helper()
		if err := os.WriteFile(stylePath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		ts := baseMtime.Add(offset)
		if err := os.Chtimes(stylePath, ts, ts); err != nil {
			t.Fatal(err)
		}
	}

	writeWithMtime(`{"light": {"paper": "#FAF8F5"}, "tableAlign": "center"}`, time.Second)
	tt.Advance(styleConfigPollInterval + 50*time.Millisecond)
	tt.Frame()
	if got := a.documentTheme(false).Background; got != ui.Hex("#FAF8F5") {
		t.Fatalf("initial paper = %+v; want #FAF8F5", got)
	}

	// Live reload after 1500ms via Tester.Advance without goroutines or sleep.
	writeWithMtime(`{"light": {"paper": "#EEF6FF", "inlineTexFill": "#EDF4FF", "texBlockFill": "#F2F7FF"}, "tableAlign": "right"}`, 3*time.Second)
	tt.Advance(styleConfigPollInterval + 50*time.Millisecond)
	tt.Frame()
	if got := a.documentTheme(false).Background; got != ui.Hex("#EEF6FF") {
		t.Fatalf("reloaded paper = %+v; want #EEF6FF", got)
	}
	if got := a.pageStyle(false).TableAlign; got != "right" {
		t.Fatalf("reloaded TableAlign = %q; want right", got)
	}
	if page := a.pageStyle(false); page.InlineTexFill != ui.Hex("#EDF4FF") || page.TexBlockFill != ui.Hex("#F2F7FF") {
		t.Fatal("TeX fills were not reloaded")
	}

	// Invalid JSON/color keeps previous valid config and surfaces error toast once per mtime.
	writeWithMtime(`{"light": {"paper": "not-a-valid-color"}}`, 5*time.Second)
	tt.Advance(styleConfigPollInterval + 50*time.Millisecond)
	tt.Frame()
	if got := a.documentTheme(false).Background; got != ui.Hex("#EEF6FF") {
		t.Fatalf("expected previous valid paper #EEF6FF kept on error, got %+v", got)
	}
	if !tt.HasText("style.json") {
		t.Fatalf("expected toast mentioning style.json error, texts=%v", tt.Texts())
	}

	// Advancing again with the same mtime does not re-queue a duplicate notice.
	errModBefore := a.styleCfg.errModNano
	tt.Advance(styleConfigPollInterval + 50*time.Millisecond)
	if a.styleCfg.errModNano != errModBefore || len(a.notices) != 0 {
		t.Fatalf("unexpected duplicate error notice for unchanged mtime: notices=%v", a.notices)
	}

	// Removing style.json restores the default Feishu paper.
	if err := os.Remove(stylePath); err != nil {
		t.Fatal(err)
	}
	tt.Advance(styleConfigPollInterval + 50*time.Millisecond)
	tt.Frame()
	if got := a.documentTheme(false).Background; got != ui.Hex("#FFFFFF") {
		t.Fatalf("after removing style.json: paper = %+v; want #FFFFFF", got)
	}
	if page := a.pageStyle(false); page.InlineTexFill.A != 0 || page.TexBlockFill.A != 0 {
		t.Fatal("TeX fills should be transparent without style.json")
	}
}

func TestStyleConfigOpenTemplateAndManualReloadControls(t *testing.T) {
	a, tt := newTestApp(t)
	var openedPath string
	a.styleCfg.openFile = func(p string) { openedPath = p }
	tt.Frame()

	stylePath := a.styleConfigPath()
	if _, err := os.Stat(stylePath); !os.IsNotExist(err) {
		t.Fatalf("expected no style.json initially, got %v", err)
	}

	// Click 打开样式配置文件 in Settings -> Appearance.
	a.openSettings(tabAppearance)
	scrollSettingsTo(t, tt, "打开样式配置文件")
	if err := tt.Click("打开样式配置文件"); err != nil {
		t.Fatalf("click 打开样式配置文件: %v", err)
	}
	tt.Frame()
	if openedPath != stylePath {
		t.Fatalf("openFile called with %q; want %q", openedPath, stylePath)
	}
	data, err := os.ReadFile(stylePath)
	if err != nil {
		t.Fatalf("style.json not created: %v", err)
	}
	if string(data) != string(prefs.DefaultStyleTemplateJSON()) {
		t.Fatal("created style.json does not match DefaultStyleTemplateJSON")
	}
	if a.styleCfg.active == nil || a.styleCfg.active.BoldWeight != 600 {
		t.Fatalf("expected template loaded into active StyleConfig, got %+v", a.styleCfg.active)
	}

	// Edit style.json and trigger 重新载入样式配置 via Command Palette immediately (without waiting 1500ms).
	a.settingsOpen = false
	if err := os.WriteFile(stylePath, []byte(`{"boldWeight": 800, "light": {"paper": "#F7FAFC"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openPalette()
	a.palette.query = "重新载入样式配置"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got := a.pageStyle(false).BoldWeight; got != 800 {
		t.Fatalf("after manual reload: BoldWeight = %d; want 800", got)
	}
	if got := a.documentTheme(false).Background; got != ui.Hex("#F7FAFC") {
		t.Fatalf("after manual reload: Background = %+v; want #F7FAFC", got)
	}
}

func TestStyleConfigSnapshot(t *testing.T) {
	out := os.Getenv("ARCMD_STYLE_PNG")
	if out == "" {
		dir := snapshotDir(t, "set ARCMD_SNAPSHOT or ARCMD_STYLE_PNG to render style-config snapshot")
		out = filepath.Join(dir, "style-config.png")
	}
	a, tt := newTestApp(t)
	tt.SetSize(1240, 840)

	styleJSON := `{
  "boldWeight": 700,
  "headingWeight": 700,
  "tableAlign": "center",
  "tableHeaderAlign": "center",
  "tableHeaderWeight": 700,
  "light": {
    "paper": "#FAF6EE",
    "text": "#26231E",
    "muted": "#78716C",
    "headingText": "#1E3A5F",
    "boldText": "#9A3412",
    "accent": "#2563EB",
    "tableHeader": "#EDE5D5",
    "tableHeaderText": "#1E3A5F",
    "tableLine": "#D8CEBC",
    "tableStripe": "#F3ECE0",
    "highlight": "#FDE047B3",
    "codeFill": "#F1EADF",
    "codeBorder": "#DED4C3",
    "codeText": "#292524",
    "inlineCodeFill": "#EAE2D3",
    "inlineCodeText": "#9A3412",
    "quoteBar": "#C28E5C"
  }
}`
	if err := os.WriteFile(a.styleConfigPath(), []byte(styleJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	tt.Advance(styleConfigPollInterval + 50*time.Millisecond)
	md := strings.Join([]string{
		"# 纸面配色与排版定制",
		"",
		"通过配置目录中的 `style.json`，可以为浅色与深色纸面分别指定 **自定义粗体字重与颜色**、<mark>荧光笔默认高亮色</mark>、标题墨色与表格对齐。",
		"",
		"> 修改保存 `style.json` 后自动热重载；配置有误时保留上一次有效样式并弹出提示。",
		"",
		"## 表格居中对齐与表头配色",
		"",
		"| 配置项 | 示例取值 | 视觉效果 |",
		"| --- | --- | --- |",
		"| paper | `rgb(250, 246, 238)` | 温润米白阅读纸面 |",
		"| tableAlign | `center` | 单元格默认居中排版 |",
		"| tableHeader | `rgb(237, 229, 213)` | 表头底色与斑马纹区分 |",
		"| boldWeight | `700` | 重点内容更醒目清晰 |",
		"",
		"```json",
		"{ \"tableAlign\": \"center\", \"boldWeight\": 700, \"light\": { \"paper\": \"#FAF6EE\" } }",
		"```",
		"",
	}, "\n")
	if err := vault.Write(root(a), "样式定制示例.md", md); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("样式定制示例.md")
	for range 4 {
		tt.Frame()
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	savePNG(t, out, tt)
}

func TestPaperPresetsSettingsPaletteDarkOverrideAndReset(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1360, 860)
	tt.Frame()

	if len(paperPresetChoices) != len(prefs.PaperPresets) {
		t.Fatalf("paperPresetChoices len = %d; want %d", len(paperPresetChoices), len(prefs.PaperPresets))
	}
	for i, id := range prefs.PaperPresets {
		if paperPresetChoices[i].id != id {
			t.Fatalf("paperPresetChoices[%d].id = %q; want %q", i, paperPresetChoices[i].id, id)
		}
	}

	// Open Settings -> Appearance and click 宣纸 ("sepia").
	a.openSettings(tabAppearance)
	tt.Frame()
	if err := tt.Click("宣纸"); err != nil {
		t.Fatalf("click 宣纸 swatch: %v", err)
	}
	tt.Frame()
	if a.paperPreset() != "sepia" {
		t.Fatalf("paperPreset = %q; want sepia", a.paperPreset())
	}
	saved, err := a.prefStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.PaperPreset != "sepia" {
		t.Fatalf("persisted PaperPreset = %q; want sepia", saved.PaperPreset)
	}
	if got := a.documentTheme(false).Background; got != ui.Hex("#F4ECD8") {
		t.Fatalf("sepia light paper = %+v; want #F4ECD8", got)
	}
	if got := a.documentTheme(true).Background; got != ui.Hex("#262019") {
		t.Fatalf("sepia dark paper = %+v; want #262019", got)
	}
	a.settingsOpen = false
	tt.Frame()

	// Switch to 竹青 ("sage") via Command Palette (⌘K) and verify status hint.
	a.openPalette()
	a.palette.query = "纸面配色：竹青"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.paperPreset() != "sage" {
		t.Fatalf("after palette command: paperPreset = %q; want sage", a.paperPreset())
	}
	if got := a.documentTheme(false).Background; got != ui.Hex("#F2F6F2") {
		t.Fatalf("sage light paper = %+v; want #F2F6F2", got)
	}
	if got := a.documentTheme(true).Background; got != ui.Hex("#1B221E") {
		t.Fatalf("sage dark paper = %+v; want #1B221E", got)
	}
	a.openPalette()
	a.palette.query = "纸面配色：竹青"
	tt.Frame()
	if !tt.HasText("当前使用") {
		t.Fatalf("expected 当前使用 hint on active paper preset in palette, texts=%v", tt.Texts())
	}
	a.palette.open = false
	tt.Frame()

	// style.json layers on top of the built-in paper preset without wiping unset preset fields.
	stylePath := a.styleConfigPath()
	if err := os.WriteFile(stylePath, []byte(`{"boldWeight": 700, "light": {"accent": "#1D4ED8"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a.reloadStyleConfigAction()
	tt.Frame()
	if got := a.documentTheme(false).Background; got != ui.Hex("#F2F6F2") {
		t.Fatalf("expected sage paper #F2F6F2 preserved under partial style.json, got %+v", got)
	}
	if got := a.documentTheme(false).Accent; got != ui.Hex("#1D4ED8") {
		t.Fatalf("expected style.json accent #1D4ED8 override, got %+v", got)
	}
	if got := a.pageStyle(false).BoldWeight; got != 700 {
		t.Fatalf("expected style.json BoldWeight 700 override, got %d", got)
	}

	// Explicit paper in style.json overrides preset paper; removing style.json falls back to preset.
	if err := os.WriteFile(stylePath, []byte(`{"light": {"paper": "#FFFBEB"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a.reloadStyleConfigAction()
	tt.Frame()
	if got := a.documentTheme(false).Background; got != ui.Hex("#FFFBEB") {
		t.Fatalf("expected style.json paper #FFFBEB to override preset, got %+v", got)
	}
	if err := os.Remove(stylePath); err != nil {
		t.Fatal(err)
	}
	a.reloadStyleConfigAction()
	tt.Frame()
	if got := a.documentTheme(false).Background; got != ui.Hex("#F2F6F2") {
		t.Fatalf("expected fallback to sage preset #F2F6F2 after removing style.json, got %+v", got)
	}

	// 恢复默认外观 resets paperPreset back to default white paper.
	a.openSettings(tabAppearance)
	scrollSettingsTo(t, tt, "恢复默认外观")
	if err := tt.Click("恢复默认外观"); err != nil {
		t.Fatalf("click 恢复默认外观: %v", err)
	}
	tt.Frame()
	if a.paperPreset() != "" || a.documentTheme(false).Background != ui.Hex("#FFFFFF") {
		t.Fatalf("expected default paper after reset: preset=%q bg=%+v", a.paperPreset(), a.documentTheme(false).Background)
	}
}

func TestPaperPresetSnapshot(t *testing.T) {
	out := os.Getenv("ARCMD_PAPER_PRESETS_PNG")
	if out == "" {
		dir := snapshotDir(t, "set ARCMD_SNAPSHOT or ARCMD_PAPER_PRESETS_PNG to render paper-presets snapshot")
		out = filepath.Join(dir, "paper-presets.png")
	}
	a, tt := newTestApp(t)
	tt.SetSize(1280, 860)
	a.setPaperPreset("sepia")
	a.openSettings(tabAppearance)
	tt.Frame()
	if r, ok := tt.Find("界面语言"); ok {
		tt.Scroll(r.X+r.W/2, r.Y+140, 0, 150)
	}
	for range 4 {
		tt.Frame()
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	savePNG(t, out, tt)
}
