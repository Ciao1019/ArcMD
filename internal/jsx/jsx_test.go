package jsx

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestThemeScriptsAreExpressionsAndDifferByAppearance(t *testing.T) {
	light := NewTheme(ui.LightTheme())
	dark := NewTheme(ui.DarkTheme())
	if light.Dark || !dark.Dark {
		t.Fatalf("color-scheme: light.Dark=%v dark.Dark=%v", light.Dark, dark.Dark)
	}
	if !ScriptIsExpression(light.Script()) || !ScriptIsExpression(dark.Script()) {
		t.Fatal("theme scripts must be single IIFE expressions for MyGo evalExpr")
	}
	if light.Vars["--bg"] == dark.Vars["--bg"] || light.Script() == dark.Script() {
		t.Fatal("light and dark tokens should differ")
	}
}

func TestMeasureScriptIsExpressionAndUsesContentBox(t *testing.T) {
	if !ScriptIsExpression(MeasureScript) {
		t.Fatal("measure script must be a single IIFE expression for MyGo evalExpr")
	}
	for _, needle := range []string{`getElementById("root")`, "getBoundingClientRect", "paddingTop", "offsetHeight"} {
		if !strings.Contains(MeasureScript, needle) {
			t.Fatalf("measure script missing %s", needle)
		}
	}
	if strings.Contains(MeasureScript, "height:100%") || strings.Contains(MeasureScript, "height: 100%") {
		t.Fatal("measure script must not force html/body to the viewport")
	}
	for _, line := range strings.Split(MeasureScript, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "scrollHeight") {
			t.Fatal("measure script should not read scrollHeight")
		}
	}
}

func TestParseAndClampMeasure(t *testing.T) {
	h, ready, ok := ParseMeasure(map[string]any{"height": float64(112.4), "ready": true})
	if !ok || !ready || h != 112 {
		t.Fatalf("got %v %v %v", h, ready, ok)
	}
	if ClampHeight(50) != HeightMin || ClampHeight(3000) != HeightMax || ClampHeight(120) != 120 {
		t.Fatal("clamp")
	}
	if _, _, ok := ParseMeasure(math.NaN()); ok {
		t.Fatal("NaN should be rejected")
	}
	if _, _, ok := ParseMeasure(json.Number("130")); !ok {
		t.Fatal("json.Number should parse")
	}
	if HeightChanged(112, 113) || !HeightChanged(112, 130) {
		t.Fatal("slop threshold")
	}
}

func TestRestoreSourceUnwrapsOnlyNamespacedFences(t *testing.T) {
	ns := "00112233445566778899aabbccddeeff"
	in := "# Title\n\n```arcmd-jsx-" + ns + "-0\n<Counter initial={5} />\n```\n\n```js\nkeep()\n```\n\n```mdx-source-only-" + ns + "\nimport {X} from './x'\n```\n"
	got := RestoreSource(in, ns)
	if !strings.Contains(got, "# Title\n\n<Counter initial={5} />\n") {
		t.Fatalf("island not restored: %q", got)
	}
	if !strings.Contains(got, "```js\nkeep()\n```") {
		t.Fatalf("ordinary fence was unwrapped: %q", got)
	}
	if !strings.Contains(got, "import {X} from './x'") || strings.Contains(got, "mdx-source-only") {
		t.Fatalf("source-only fence not restored: %q", got)
	}
	if RestoreSource(in, "") != in || RestoreSource(in, "other") != in {
		t.Fatal("unknown namespace must leave markdown unchanged")
	}
}

func TestCompileProjectsIslandsAndRestoreMatchesSource(t *testing.T) {
	source := "# Native\n\nA paragraph.\n\n<Counter initial={5} />\n\nAfter the island.\n"
	m, err := Compile(source, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Blocks) != 1 || m.Blocks[0].HTML == "" || m.Blocks[0].Error != "" {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	if m.SourceSHA256 != SourceSHA256(source) {
		t.Fatal("source sha")
	}
	if RestoreSource(m.Markdown, m.Namespace) != source {
		t.Fatalf("restore mismatch:\n%s\n----\n%s", RestoreSource(m.Markdown, m.Namespace), source)
	}
	again, err := Compile(source, Options{})
	if err != nil || again.SourceSHA256 != m.SourceSHA256 {
		t.Fatalf("repeat compilation failed: %v %+v", err, again)
	}
}

func TestCompileWithoutNode(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("ARCMD_JSX_NODE", "/does/not/exist")
	m, err := Compile("# Fresh file\n\n<Callout title=\"Fresh\">\n\n<Counter initial={2+3} />\n\n</Callout>\n", Options{})
	if err != nil || !m.HasHTML() {
		t.Fatalf("no-Node compiler: %v %+v", err, m)
	}
}
