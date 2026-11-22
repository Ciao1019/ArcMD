package editor

import (
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/mermaid"
	"github.com/egoist/mygo/ui"
)

// testHTMLTheme is the paper pair the app hands ExportHTMLPage in tests.
func testHTMLTheme() HTMLTheme {
	light, dark := ui.LightTheme(), ui.DarkTheme()
	return HTMLTheme{
		Light: HTMLStyle{Theme: light, Page: pageOf(Page{}, light)},
		Dark:  HTMLStyle{Theme: dark, Page: pageOf(Page{}, dark)},
	}
}

func TestExportHTMLPageHighlightsCode(t *testing.T) {
	doc := document.New()
	doc.Blocks = nil
	code := document.NewBlock("code", "public static void main() {\n    System.out.println(\"hello world\");\n}")
	code.Language = "java"
	doc.Blocks = append(doc.Blocks, code)

	out := ExportHTMLPage(doc, testHTMLTheme())
	for _, want := range []string{
		`<span class="arcmd-tok-keyword">public</span>`,
		`<span class="arcmd-tok-keyword">static</span>`,
		`<span class="arcmd-tok-type">System</span>`,
		`<span class="arcmd-tok-function">main</span>`,
		`<span class="arcmd-tok-string">&#34;hello world&#34;</span>`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("ExportHTMLPage missing %q:\n%s", want, out)
		}
	}

	// The spans are paint only: importing the export back gets the same code.
	reimported := ImportHTML(out)
	if len(reimported.Blocks) != 1 || reimported.Blocks[0].Kind != "code" {
		t.Fatalf("code block lost in the HTML round trip: %+v", reimported.Blocks)
	}
	if got, want := reimported.Blocks[0].Text(), code.Text(); got != want {
		t.Fatalf("code text = %q, want %q", got, want)
	}
}

func TestExportHTMLClipboardKeepsCodeAndMermaidSource(t *testing.T) {
	doc := document.New()
	doc.Blocks = nil
	code := document.NewBlock("code", "func main() {}\n")
	code.Language = "go"
	doc.Blocks = append(doc.Blocks, code)
	diagram := document.NewBlock("code", "flowchart LR\n  A[开始] --> B[结束]")
	diagram.Language = "mermaid"
	doc.Blocks = append(doc.Blocks, diagram)

	out := ExportHTML(doc)
	if strings.Contains(out, "arcmd-tok-") || strings.Contains(out, "<svg") {
		t.Fatalf("clipboard HTML should stay plain:\n%s", out)
	}
	if !strings.Contains(out, `<pre><code class="language-mermaid">flowchart LR`) {
		t.Fatalf("clipboard kept no Mermaid source:\n%s", out)
	}
}

func TestExportHTMLPageEscapesCodeWithoutLanguage(t *testing.T) {
	doc := document.New()
	doc.Blocks = nil
	code := document.NewBlock("code", "if a < b && c > d { }\n")
	doc.Blocks = append(doc.Blocks, code)

	out := ExportHTMLPage(doc, testHTMLTheme())
	if !strings.Contains(out, `if a &lt; b &amp;&amp; c &gt; d { }`) {
		t.Fatalf("plain code is not escaped:\n%s", out)
	}
	if strings.Contains(out, `<span class="arcmd-tok-`) {
		t.Fatalf("a code block without a language has no highlighting:\n%s", out)
	}
}

func TestExportHTMLPageRendersMermaidDiagrams(t *testing.T) {
	src := "flowchart LR\n  A[开始] --> B{条件判断}\n  B -- 通过 --> C[执行]\n  B -. 拒绝 .-> D[结束]"
	doc := document.New()
	doc.Blocks = nil
	code := document.NewBlock("code", src)
	code.Language = "mermaid"
	doc.Blocks = append(doc.Blocks, code)

	out := ExportHTMLPage(doc, testHTMLTheme())
	if !strings.Contains(out, `<figure class="arcmd-mermaid"><span class="arcmd-diagram-light"><svg`) {
		t.Fatalf("mermaid block is not drawn as a figure:\n%s", out)
	}
	if !strings.Contains(out, `<span class="arcmd-diagram-dark"><svg`) {
		t.Fatalf("mermaid diagram has no dark picture:\n%s", out)
	}
	if !strings.Contains(out, `font-family="`+mermaid.FontFamily+`,`) {
		t.Fatalf("diagram does not declare the measured font family:\n%s", out)
	}
	if strings.Contains(out, "language-mermaid") {
		t.Fatalf("mermaid source leaked into the export:\n%s", out)
	}
	for _, want := range []string{"开始", "条件判断", "执行", "结束"} {
		if !strings.Contains(out, want) {
			t.Fatalf("diagram is missing label %q:\n%s", want, out)
		}
	}
}

func TestExportHTMLPageDrawsSequenceDiagrams(t *testing.T) {
	src := "sequenceDiagram\n  Alice->>Bob: Hello\n  Note right of Bob: Thinking"
	doc := document.New()
	doc.Blocks = nil
	code := document.NewBlock("code", src)
	code.Language = "mermaid"
	doc.Blocks = append(doc.Blocks, code)

	out := ExportHTMLPage(doc, testHTMLTheme())
	if !strings.Contains(out, `<svg`) {
		t.Fatalf("sequence diagram is not drawn:\n%s", out)
	}
	for _, want := range []string{"Alice", "Bob", "Hello", "Thinking"} {
		if !strings.Contains(out, want) {
			t.Fatalf("diagram is missing %q:\n%s", want, out)
		}
	}
}

func TestExportHTMLPageKeepsUnknownMermaidAsSource(t *testing.T) {
	doc := document.New()
	doc.Blocks = nil
	code := document.NewBlock("code", "not a diagram")
	code.Language = "mermaid"
	doc.Blocks = append(doc.Blocks, code)

	out := ExportHTMLPage(doc, testHTMLTheme())
	if !strings.Contains(out, `<pre><code class="language-mermaid">not a diagram</code></pre>`) {
		t.Fatalf("an unreadable diagram keeps its source:\n%s", out)
	}
}

// The dark picture must be colored from the dark page tokens, not the light
// ones: the page swaps pictures with prefers-color-scheme.
func TestExportHTMLPageDrawsDarkDiagrams(t *testing.T) {
	doc := document.New()
	doc.Blocks = nil
	code := document.NewBlock("code", "flowchart LR\n  A[开始] --> B[结束]")
	code.Language = "mermaid"
	doc.Blocks = append(doc.Blocks, code)

	theme := testHTMLTheme()
	out := ExportHTMLPage(doc, theme)
	darkAt := strings.Index(out, `class="arcmd-diagram-dark"`)
	if darkAt < 0 {
		t.Fatalf("no dark diagram:\n%s", out)
	}
	dark, light := out[darkAt:], exportMermaidPalette(theme.Light)
	pal := exportMermaidPalette(theme.Dark)
	if pal.Paper == light.Paper || pal.Text == light.Text {
		t.Fatalf("dark palette is not the dark paper: %+v", pal)
	}
	for _, want := range []string{
		`fill="` + pal.Paper + `"`,
		`fill="` + pal.NodeFill + `"`,
		`fill="` + pal.Text + `"`,
	} {
		if !strings.Contains(dark, want) {
			t.Fatalf("dark diagram missing %q:\n%s", want, dark)
		}
	}
}

// exportMermaidMeasure must use the real system font, not mermaid's 0.56em
// estimate, or wide labels overflow the boxes the SVG draws around them.
func TestExportMermaidMeasureWideLetters(t *testing.T) {
	w, _ := exportMermaidMeasure(strings.Repeat("W", 20), 13, false)
	if w < 200 {
		t.Fatalf("20 W at 13 DIP measure %.1f, want the rendered width", w)
	}
}
