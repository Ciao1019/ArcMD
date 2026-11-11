package editor

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

const integralSource = `\int_0^\infty x^2 e^{-x} dx = 2`
const integralMarkdown = "$$\n" + integralSource + "\n$$"

func TestIntegralMarkdownLayout(t *testing.T) {
	if err := loadMathFonts(); err != nil {
		t.Fatal(err)
	}
	e, _ := newTester(integralMarkdown)
	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "math" || e.Doc.Blocks[0].Text() != integralSource {
		t.Fatalf("imported blocks: %+v", e.Doc.Blocks)
	}
	if len(e.rows) != 1 || e.rows[0].math == nil || e.rows[0].mathErr != "" {
		t.Fatalf("integral did not render: %+v", e.rows)
	}
	var integral, infinity bool
	for _, op := range e.rows[0].math.Ops {
		if op.Error {
			t.Fatalf("unsupported formula token: %q", op.Text)
		}
		integral = integral || op.Text == "∫"
		infinity = infinity || op.Text == "∞"
	}
	if !integral || !infinity {
		t.Fatal("integral or upper limit missing")
	}
}

func TestMathReadingAndEditing(t *testing.T) {
	e, tt := newTester("Before\n\n" + integralMarkdown + "\n\nAfter")
	r := e.rows[1]
	if !r.mathHidden || r.mathPanel != 0 {
		t.Fatal("formula is boxed while reading")
	}
	bounds, _ := tt.Find("文档")
	tt.ClickAt(bounds.X+r.rect.X+r.rect.W/2, bounds.Y+r.rect.Y+r.mathTop+8)
	tt.Frame()
	r = e.rows[1]
	if r.mathHidden || r.mathPanel == 0 || r.mathTop <= mathMargin+r.mathPanel {
		t.Fatal("click did not reveal the source and separate preview")
	}
	e.SetSelection(r.leaf.End, r.leaf.End)
	tt.Frame()
	tt.Type("+1")
	if e.maths[r.leaf.Block.ID].src != integralSource+"+1" {
		t.Fatal("preview did not update while typing")
	}
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	if !e.rows[1].mathHidden || e.rows[1].mathPanel != 0 {
		t.Fatal("source panel remained after leaving the formula")
	}
}

func TestMathFenceTyping(t *testing.T) {
	e, tt := newTester("")
	focus(e, tt)
	tt.Type("$$")
	tt.Key(0, ui.KeyEnter)
	tt.Type(integralSource)
	tt.Key(0, ui.KeyEnter)
	tt.Type("$$")
	tt.Key(0, ui.KeyEnter)
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Kind != "math" || e.Doc.Blocks[0].Text() != integralSource || !e.rows[0].mathHidden {
		t.Fatal("typed fences did not produce a rendered formula")
	}
}

func TestMathPasteContexts(t *testing.T) {
	for _, kind := range []string{"paragraph", "code", "raw", "math", "cell"} {
		t.Run(kind, func(t *testing.T) {
			e, tt := newTester("")
			if kind == "cell" {
				e.Engine.Insert(document.Table(1, 1))
			} else {
				e.Engine.SetKind(kind, 0)
			}
			focus(e, tt)
			tt.SetClipboardData(transfer.New(transfer.NewItem(
				transfer.Bytes(transfer.Text, []byte(integralMarkdown)),
				transfer.Bytes(transfer.HTML, []byte("<pre><code>"+integralMarkdown+"</code></pre>")),
			)))
			tt.Key(ui.Cmd, ui.KeyV)
			got := e.Engine.LeafAt(e.Caret).Block
			want := integralMarkdown
			if kind == "paragraph" || kind == "math" {
				want = integralSource
				if got.Kind != "math" {
					t.Fatal("formula lost its block type")
				}
			}
			if got.Text() != want {
				t.Fatalf("paste into %s: %q", kind, got.Text())
			}
		})
	}
	for _, source := range []string{"$$\nx^2", "Price $5 and $6", "```tex\n" + integralMarkdown + "\n```", "`$$x^2$$`"} {
		if pastedMath(source) != nil {
			t.Errorf("interpreted literal text as math: %q", source)
		}
	}
	for _, source := range []string{integralMarkdown, strings.ReplaceAll(integralMarkdown, "\n", "\r\n"), `\[x^2\]`, "Before\n\n" + integralMarkdown + "\n\nAfter"} {
		if pastedMath(source) == nil {
			t.Errorf("did not recognize formula: %q", source)
		}
	}
}

func TestMathMovesWithPrecedingText(t *testing.T) {
	e, tt := newTester("Before\n\n" + integralMarkdown)
	focus(e, tt)
	e.SetSelection(6, 6)
	tt.Frame()
	tt.Type(strings.Repeat(" more text", 24))
	pixels := append([]byte(nil), tt.Image().Pix...)
	e.ready = false
	tt.Frame()
	if !bytes.Equal(pixels, tt.Image().Pix) {
		t.Fatal("formula did not move with the paragraph during incremental layout")
	}
}

func TestMathFitsNarrowPagesAndPrintsWithoutSource(t *testing.T) {
	e, tt := newTester("$$\n" + strings.Repeat("x+", 60) + "1\n$$")
	tt.SetSize(320, 650)
	focus(e, tt)
	tt.Frame()
	r := e.rows[0]
	if r.math.W*r.mathScale > r.rect.W-2*mathPaddingX+.01 {
		t.Fatal("preview overflows the narrow page")
	}
	e.Paginate(Options{Width: 320, Theme: &e.cacheTheme}, 600)
	if !e.rows[0].mathHidden || e.rows[0].mathPanel != 0 || len(e.PageText(0, 600)) != 0 {
		t.Fatal("printing included formula editing chrome or source")
	}
}

func TestMathErrorRecoversWhileEditing(t *testing.T) {
	e, tt := newTester("$$\n\\frac{a}\n$$")
	if e.rows[0].mathError == nil || e.rows[0].mathHidden {
		t.Fatal("invalid formula hid its source or error")
	}
	focus(e, tt)
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	tt.Type("{b}")
	if e.rows[0].mathError != nil || e.rows[0].math == nil {
		t.Fatal("corrected formula did not recover its preview")
	}
	if got := ExportMarkdown(e.Doc); got != "$$\n\\frac{a}{b}\n$$\n" {
		t.Fatalf("formula export changed the source: %q", got)
	}
}

func TestPasteIntegralMarkdown(t *testing.T) {
	e, tt := newTester("")
	focus(e, tt)
	before := e.Doc.Clone()
	tt.SetClipboard(integralMarkdown)
	tt.Key(ui.Cmd, ui.KeyV)
	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "math" || e.Doc.Blocks[0].Text() != integralSource {
		t.Fatalf("paste left the formula as text: %+v", e.Doc.Blocks)
	}
	if e.rows[0].math == nil {
		t.Fatal("pasted formula has no preview")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if !reflect.DeepEqual(e.Doc, before) {
		t.Fatal("paste was not undone in one step")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	if e.Doc.Blocks[0].Kind != "math" {
		t.Fatal("redo lost the formula")
	}
}

func TestCopySingleMathBlock(t *testing.T) {
	e, tt := newTester(integralMarkdown + "\n\nDestination")
	focus(e, tt)
	e.SetSelection(0, ui.UTF16Len(integralSource))
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyC)
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyV)
	blocks := e.Doc.Blocks
	if len(blocks) != 3 || blocks[1].Text() != "Destination" || blocks[2].Kind != "math" || blocks[2].Text() != integralSource {
		t.Fatalf("copy lost the math block: %+v", blocks)
	}
}
