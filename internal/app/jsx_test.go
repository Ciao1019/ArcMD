package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/jsx"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

type jsxTestView struct{ ui.NativeWebView }

func TestOpenMDXAttachesEmbeddedBlocks(t *testing.T) {

	a, tt := newTestApp(t)
	a.testIslandView = func() ui.NativeWebView { return &jsxTestView{} }
	source := "# Project\n\nIntro paragraph.\n\n<Counter initial={5} />\n\nAfter the island.\n"
	if err := vault.Write(root(a), "Demo.mdx", source); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Demo.mdx")
	if a.doc == nil || a.doc.jsxErr != nil || a.doc.jsx == nil {
		t.Fatalf("expected a compiled projection, doc=%v err=%v", a.doc, a.doc.jsxErr)
	}
	if len(a.doc.jsx.Blocks) != 1 || a.doc.jsx.Blocks[0].HTML == "" {
		t.Fatalf("islands: %+v", a.doc.jsx.Blocks)
	}
	tt.Frame()
	if strings.Contains(strings.Join(tt.Texts(), "\n"), "内置编译器") {
		t.Fatal("technical success banner leaked")
	}
	if len(a.doc.jsxBlocks) != 1 {
		t.Fatalf("EmbeddedBlock not attached: %+v", a.doc.jsxBlocks)
	}
	var view ui.NativeWebView
	for _, b := range a.doc.jsxBlocks {
		view = b.View
	}
	if view == nil {
		t.Fatal("missing web view")
	}
	// Saving must write the original MDX, not the projection fences.
	if got := a.doc.text(); got != source {
		t.Fatalf("persist text changed the source:\n%s", got)
	}
	if a.doc.dirty() {
		t.Fatal("opening a .mdx file should not mark it dirty")
	}
	disk, err := vault.Read(root(a), "Demo.mdx")
	if err != nil || disk != source {
		t.Fatalf("open rewrote the file: %q %v", disk, err)
	}
}

func TestJSXCompileFailureFallsBackToSource(t *testing.T) {
	a, tt := newTestApp(t)
	source := "# Broken\n\n<Counter initial={ />\n"
	if err := vault.Write(root(a), "Broken.mdx", source); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Broken.mdx")
	if a.doc == nil || a.doc.loadErr != nil {
		t.Fatal("document should still open")
	}
	if a.doc.jsxErr == nil || a.doc.jsxDiagnostic() == "" {
		t.Fatal("expected a compile fallback error")
	}
	if a.doc.jsx != nil || len(a.doc.jsxBlocks) != 0 {
		t.Fatal("failed compile should not attach islands")
	}
	tt.Frame()
	if a.doc.text() != source && !strings.Contains(a.doc.text(), "Broken") {
		t.Fatalf("source fallback lost the file: %q", a.doc.text())
	}
}

func TestJSXThemeAndMeasureScriptsStayExpressions(t *testing.T) {
	if !jsx.ScriptIsExpression(jsx.NewTheme(ui.LightTheme()).Script()) {
		t.Fatal("theme script is not an expression IIFE")
	}
	if !jsx.ScriptIsExpression(jsx.MeasureScript) {
		t.Fatal("measure script is not an expression IIFE")
	}
}

func TestOpenMarkdownWithoutJSXIsUnchanged(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("Welcome.md")
	if a.doc.jsx != nil || a.doc.jsxErr != nil || len(a.doc.jsxBlocks) != 0 {
		t.Fatal("ordinary markdown should not run the JSX compiler")
	}
}

func TestJSXFailureDiagnosticIsVisible(t *testing.T) {
	a, tt := newTestApp(t)
	if err := vault.Write(root(a), "Bad.mdx", "# Bad\n\n<Counter initial={ />\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Bad.mdx")
	tt.Frame()
	if !strings.Contains(strings.Join(tt.Texts(), "\n"), "JSX 编译失败") {
		t.Fatalf("hidden failure: %v", tt.Texts())
	}
	if !tt.HasText("重新载入并重试 JSX") {
		t.Fatal("retry action missing")
	}
	a.doc.jsxErr = nil
	a.doc.jsxViewErr = fmt.Errorf("controlled native initialization failure")
	tt.Frame()
	if !strings.Contains(strings.Join(tt.Texts(), "\n"), "WebView 失败") {
		t.Fatal("hidden WebView failure")
	}
}

func TestOpenMoeNestedMDXWithoutNode(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	a, tt := newTestApp(t)
	a.testIslandView = func() ui.NativeWebView { return &jsxTestView{} }
	source := "# 新文档 😀\n\n<Counter initial={17+24} />\n\n<Callout title=\"nested\">\n\n**bold**\n\n<Counter initial={12} />\n\n</Callout>\n"
	if e := vault.Write(root(a), "Fresh.mdx", source); e != nil {
		t.Fatal(e)
	}
	a.refreshTree()
	a.open("Fresh.mdx")
	tt.Frame()
	if a.doc.jsxErr != nil || len(a.doc.jsxBlocks) != 2 {
		t.Fatalf("nested islands not mounted: %v %+v", a.doc.jsxErr, a.doc.jsxBlocks)
	}
	if a.doc.text() != source || a.doc.dirty() {
		t.Fatal("projection altered persisted source")
	}
}

func TestJSXSuccessHasNoStatusOrPlaceholder(t *testing.T) {
	a := &App{}
	d := &document{rel: "Normal.mdx", jsx: &jsx.Manifest{}}
	view := func(status bool) func(*ui.Context) {
		return func(c *ui.Context) {
			ui.Column(c).Children(func() {
				if status {
					a.jsxStatus(c, d)
				}
				ui.Text(c, "body sentinel")
			})
		}
	}
	control := ui.NewTester(view(false), 600, 400)
	control.Frame()
	actual := ui.NewTester(view(true), 600, 400)
	actual.Frame()
	if got := actual.Texts(); len(got) != 1 || got[0] != "body sentinel" {
		t.Fatalf("success leaked status UI: %q", got)
	}
	want, wok := control.Find("body sentinel")
	got, gok := actual.Find("body sentinel")
	if !wok || !gok || got != want {
		t.Fatalf("success reserved space: got=%+v want=%+v", got, want)
	}
}

func TestJSXPerIslandErrorRemainsVisible(t *testing.T) {
	a := &App{}
	d := &document{rel: "Partial.mdx", jsx: &jsx.Manifest{Blocks: []jsx.Block{{Error: "controlled island compile failure"}}}}
	tt := ui.NewTester(func(c *ui.Context) { a.jsxStatus(c, d) }, 600, 400)
	tt.Frame()
	if !strings.Contains(strings.Join(tt.Texts(), "\n"), "controlled island compile failure") {
		t.Fatal("per-island failure hidden")
	}
}
