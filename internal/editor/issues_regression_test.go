package editor

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

func TestIssue19CodeTabVisualIndentAndPaste(t *testing.T) {
	e := New("```cpp\n\n```")
	var theme ui.Theme
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		theme = *c.Theme()
		View(c, e, Options{Width: w, Theme: c.Theme()})
	}, 900, 650)
	tt.Frame()
	focus(e, tt)

	cpp := "int main(){\n\tint n;\n\twhile(cin>>n){\n\t\tif(n%2==0) {\n\t\t\tcout << n;\n\t\t}\n\t}\n}"
	tt.SetClipboard(cpp)
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()

	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "code" {
		t.Fatalf("expected single code block, got %+v", e.Doc.Blocks)
	}
	if got := e.Doc.Blocks[0].Text(); got != cpp {
		t.Fatalf("code text mismatch:\ngot:  %q\nwant: %q", got, cpp)
	}

	r := e.rows[0]
	if r.layout == nil {
		t.Fatal("code row layout missing")
	}
	line2Start := document.Len("int main(){\n")
	line4Start := document.Len("int main(){\n\tint n;\n\twhile(cin>>n){\n")
	caretLine2Start := r.layout.Caret(line2Start)
	caretLine2Char := r.layout.Caret(line2Start + 1)
	caretLine4Char := r.layout.Caret(line4Start + 2)

	if caretLine2Char.X <= caretLine2Start.X+15 {
		t.Fatalf("leading tab did not advance caret visually: start.X=%v char.X=%v", caretLine2Start.X, caretLine2Char.X)
	}
	if caretLine4Char.X <= caretLine2Char.X*1.7 {
		t.Fatalf("double leading tab did not advance ~2x single tab: 1tab.X=%v 2tab.X=%v", caretLine2Char.X, caretLine4Char.X)
	}

	// Vector PDF export must handle tab-indented code cleanly.
	var buf bytes.Buffer
	w := pdf.NewWriter(&buf, "tab-test")
	vp := w.NewVectorPage(pdf.A4Width, pdf.A4Height)
	e.ExportVectorPage(vp, Options{Width: 794, Theme: &theme}, 0, 1123, 0, 1, []float32{0}, 794, 1123, 56)
	if err := w.FinishVectorPage(vp); err != nil {
		t.Fatalf("FinishVectorPage failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("ExportVectorPage pdf close failed: %v", err)
	}
	if !strings.HasPrefix(buf.String(), "%PDF-") {
		t.Fatalf("expected valid PDF output, got %q", buf.String()[:min(16, buf.Len())])
	}
}

func TestIssue19LongIndentedCodeLineSoftWrap(t *testing.T) {
	for _, tc := range []struct {
		name      string
		indent    string
		indentLen int
	}{
		{name: "spaces", indent: "    ", indentLen: 4},
		{name: "tab", indent: "\t", indentLen: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			longTok := strings.Repeat("o", 60)
			code := "int main(){\n" + tc.indent + "cout<<\"No" + longTok + "\"<<endl;\n}"
			doc := document.New()
			b := document.NewBlock("code", code)
			b.Language = "cpp"
			doc.Blocks = []*document.Block{b}

			e := FromDocument(doc)
			tt := ui.NewTester(func(c *ui.Context) {
				w, _ := c.Size()
				View(c, e, Options{Width: w, Theme: c.Theme()})
			}, 420, 650)
			tt.Frame()

			r := e.rows[0]
			if r.layout == nil {
				t.Fatal("code row layout missing")
			}
			line2Start := document.Len("int main(){\n")
			coutOffset := line2Start + tc.indentLen
			caretStart := r.layout.Caret(line2Start)
			caretCout := r.layout.Caret(coutOffset)

			if caretCout.Y != caretStart.Y {
				t.Fatalf("indented long token dropped to next visual row: start=%+v cout=%+v", caretStart, caretCout)
			}
			if caretCout.X <= 15 {
				t.Fatalf("indented long token lost leading visual indent: cout.X=%v", caretCout.X)
			}
			if len(r.layout.Lines()) < 4 {
				t.Fatalf("expected long code line to soft-wrap across multiple visual rows, got %d lines", len(r.layout.Lines()))
			}
		})
	}
}

func TestIssue19HTMLCodePasteAndIndentRecovery(t *testing.T) {
	t.Run("pre-nested-divs", func(t *testing.T) {
		e, tt := newTester("")
		focus(e, tt)
		tt.SetClipboardData(transfer.New(transfer.NewItem(
			transfer.Bytes(transfer.HTML, []byte(`<pre><div>    int x = 1;</div><div>        return x;</div></pre>`)),
			transfer.Bytes(transfer.Text, []byte("    int x = 1;\n        return x;")),
		)))
		tt.Key(ui.Cmd, ui.KeyV)
		tt.Frame()
		if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "code" {
			t.Fatalf("expected single code block, got %+v", e.Doc.Blocks)
		}
		if got, want := e.Doc.Blocks[0].Text(), "    int x = 1;\n        return x;"; got != want {
			t.Fatalf("got %q; want %q", got, want)
		}
	})

	t.Run("white-space-pre-nbsp", func(t *testing.T) {
		e, tt := newTester("")
		focus(e, tt)
		tt.SetClipboardData(transfer.New(transfer.NewItem(
			transfer.Bytes(transfer.HTML, []byte(`<div style="white-space: pre"><div><span>&nbsp;&nbsp;&nbsp;&nbsp;int y = 2;</span></div><div><span>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;return y;</span></div></div>`)),
		)))
		tt.Key(ui.Cmd, ui.KeyV)
		tt.Frame()
		if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "code" {
			t.Fatalf("expected single code block, got %+v", e.Doc.Blocks)
		}
		if got, want := e.Doc.Blocks[0].Text(), "    int y = 2;\n        return y;"; got != want {
			t.Fatalf("got %q; want %q", got, want)
		}
	})

	t.Run("recover-stripped-plain-indent-in-code-block", func(t *testing.T) {
		e, tt := newTester("```cpp\n\n```")
		focus(e, tt)
		tt.SetClipboardData(transfer.New(transfer.NewItem(
			transfer.Bytes(transfer.HTML, []byte(`<pre>int main(){
    int n;
    return 0;
}</pre>`)),
			transfer.Bytes(transfer.Text, []byte("int main(){\nint n;\nreturn 0;\n}")),
		)))
		tt.Key(ui.Cmd, ui.KeyV)
		tt.Frame()
		if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "code" {
			t.Fatalf("expected single code block, got %+v", e.Doc.Blocks)
		}
		want := "int main(){\n    int n;\n    return 0;\n}"
		if got := e.Doc.Blocks[0].Text(); got != want {
			t.Fatalf("failed to recover indent:\ngot:  %q\nwant: %q", got, want)
		}
	})
}

func TestIssue19CodeEnterAutoIndentAndTabOutdent(t *testing.T) {
	t.Run("enter-spaces-and-blank-line-cleanup", func(t *testing.T) {
		doc := document.New()
		b := document.NewBlock("code", "int main() {\n    if (ok) {")
		b.Language = "cpp"
		doc.Blocks = []*document.Block{b}

		e := FromDocument(doc)
		tt := ui.NewTester(func(c *ui.Context) { w, _ := c.Size(); View(c, e, Options{Width: w, Theme: c.Theme()}) }, 900, 650)
		tt.Frame()
		focus(e, tt)
		e.SetSelection(e.Len(), e.Len())
		tt.Frame()

		tt.Key(0, ui.KeyEnter)
		tt.Frame()
		want1 := "int main() {\n    if (ok) {\n    "
		if got := e.Text(); got != want1 {
			t.Fatalf("1st Enter got %q; want %q", got, want1)
		}
		if e.Caret != document.Len(want1) {
			t.Fatalf("1st Enter caret=%d; want %d", e.Caret, document.Len(want1))
		}

		// Pressing Enter on a blank indented line cleans the abandoned spaces above.
		tt.Key(0, ui.KeyEnter)
		tt.Frame()
		want2 := "int main() {\n    if (ok) {\n\n    "
		if got := e.Text(); got != want2 {
			t.Fatalf("2nd Enter got %q; want %q", got, want2)
		}
	})

	t.Run("enter-tabs", func(t *testing.T) {
		doc := document.New()
		b := document.NewBlock("code", "int main() {\n\tif (ok) {")
		doc.Blocks = []*document.Block{b}

		e := FromDocument(doc)
		tt := ui.NewTester(func(c *ui.Context) { w, _ := c.Size(); View(c, e, Options{Width: w, Theme: c.Theme()}) }, 900, 650)
		tt.Frame()
		focus(e, tt)
		e.SetSelection(e.Len(), e.Len())
		tt.Frame()

		tt.Key(0, ui.KeyEnter)
		tt.Frame()
		if got, want := e.Text(), "int main() {\n\tif (ok) {\n\t"; got != want {
			t.Fatalf("tab Enter got %q; want %q", got, want)
		}
	})

	t.Run("tab-and-shift-tab-in-code", func(t *testing.T) {
		doc := document.New()
		b := document.NewBlock("code", "alpha\n    beta\ngamma")
		doc.Blocks = []*document.Block{b}

		e := FromDocument(doc)
		tt := ui.NewTester(func(c *ui.Context) { w, _ := c.Size(); View(c, e, Options{Width: w, Theme: c.Theme()}) }, 900, 650)
		tt.Frame()
		focus(e, tt)

		// Collapsed Tab at start inserts literal \t, deleted in 1 Backspace.
		e.SetSelection(0, 0)
		tt.Frame()
		tt.Key(0, ui.KeyTab)
		tt.Frame()
		if got := e.Text(); got != "\talpha\n    beta\ngamma" {
			t.Fatalf("collapsed Tab got %q", got)
		}
		if e.Doc.Blocks[0].Indent != 0 {
			t.Fatalf("code Tab changed block.Indent=%d; want 0", e.Doc.Blocks[0].Indent)
		}
		tt.Key(0, ui.KeyBackspace)
		tt.Frame()
		if got := e.Text(); got != "alpha\n    beta\ngamma" {
			t.Fatalf("single Backspace did not delete tab: got %q", got)
		}

		// Multi-line selection Tab indents covered lines; Shift+Tab outdents tabs/spaces.
		e.SetSelection(0, document.Len("alpha\n    beta"))
		tt.Frame()
		tt.Key(0, ui.KeyTab)
		tt.Frame()
		if got, want := e.Text(), "\talpha\n\t    beta\ngamma"; got != want {
			t.Fatalf("multi-line Tab got %q; want %q", got, want)
		}
		tt.Key(ui.Shift, ui.KeyTab)
		tt.Frame()
		if got, want := e.Text(), "alpha\n    beta\ngamma"; got != want {
			t.Fatalf("multi-line 1st Shift+Tab got %q; want %q", got, want)
		}
		tt.Key(ui.Shift, ui.KeyTab)
		tt.Frame()
		if got, want := e.Text(), "alpha\nbeta\ngamma"; got != want {
			t.Fatalf("multi-line 2nd Shift+Tab (4 spaces) got %q; want %q", got, want)
		}
		if e.Doc.Blocks[0].Indent != 0 {
			t.Fatalf("Shift+Tab in code changed block.Indent=%d; want 0", e.Doc.Blocks[0].Indent)
		}
	})
}

func TestIssue18HierarchicalSelectAll(t *testing.T) {
	doc := document.New()
	pIntro := document.NewBlock("paragraph", "Intro text")
	codeBlk := document.NewBlock("code", "line 1\n    line 2")
	calloutBlk := document.NewBlock("callout", "Callout body")
	bulletBlk := document.NewBlock("bullet", "Item A")
	tblBlk := document.Table(2, 2)
	tblBlk.Children[0].Children[0].Runs = []document.Run{{Text: "H1"}}
	tblBlk.Children[0].Children[1].Runs = []document.Run{{Text: "H2"}}
	tblBlk.Children[1].Children[0].Runs = []document.Run{{Text: "C1"}}
	tblBlk.Children[1].Children[1].Runs = []document.Run{{Text: "C2"}}
	colsBlk := document.Columns()
	colsBlk.Children[0].Children = []*document.Block{
		document.NewBlock("paragraph", "Col0 A"),
		document.NewBlock("paragraph", "Col0 B"),
	}
	colsBlk.Children[1].Children = []*document.Block{
		document.NewBlock("paragraph", "Col1 A"),
	}
	mathPara := document.NewBlock("paragraph", "See $x+y$ here")
	emptyPara := document.NewBlock("paragraph", "")
	doc.Blocks = []*document.Block{pIntro, codeBlk, calloutBlk, bulletBlk, tblBlk, colsBlk, mathPara, emptyPara}

	e := FromDocument(doc)
	tt := ui.NewTester(func(c *ui.Context) { w, _ := c.Size(); View(c, e, Options{Width: w, Theme: c.Theme()}) }, 900, 650)
	tt.Frame()
	focus(e, tt)

	leafOf := func(id string) document.Leaf {
		l, ok := e.Engine.LeafByID(id)
		if !ok {
			t.Fatalf("missing leaf %s", id)
		}
		return l
	}
	wantRange := func(name string, a, z int) {
		t.Helper()
		if got := e.Selection().Range; got != (ui.TextInputRange{Start: a, End: z}) {
			t.Fatalf("%s: selection = %+v; want [%d, %d]", name, got, a, z)
		}
	}

	// Code block: leaf -> full doc.
	cl := leafOf(codeBlk.ID)
	e.SetSelection(cl.Start+2, cl.Start+2)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyA)
	wantRange("code 1st", cl.Start, cl.End)
	tt.Key(ui.Cmd, ui.KeyA)
	wantRange("code 2nd", 0, e.Len())

	// Callout block: leaf -> full doc.
	cal := leafOf(calloutBlk.ID)
	e.SetSelection(cal.Start+3, cal.Start+3)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyA)
	wantRange("callout 1st", cal.Start, cal.End)
	tt.Key(ui.Cmd, ui.KeyA)
	wantRange("callout 2nd", 0, e.Len())

	// Bullet block: leaf -> full doc.
	bl := leafOf(bulletBlk.ID)
	e.SetSelection(bl.Start+1, bl.Start+1)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyA)
	wantRange("bullet 1st", bl.Start, bl.End)
	tt.Key(ui.Cmd, ui.KeyA)
	wantRange("bullet 2nd", 0, e.Len())

	// Table cell: cell -> table -> full doc.
	cell := leafOf(tblBlk.Children[1].Children[0].ID)
	tFirst := leafOf(tblBlk.Children[0].Children[0].ID)
	tLast := leafOf(tblBlk.Children[1].Children[1].ID)
	e.SetSelection(cell.Start+1, cell.Start+1)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyA)
	wantRange("table 1st (cell)", cell.Start, cell.End)
	tt.Key(ui.Cmd, ui.KeyA)
	wantRange("table 2nd (table)", tFirst.Start, tLast.End)
	tt.Key(ui.Cmd, ui.KeyA)
	wantRange("table 3rd (doc)", 0, e.Len())

	// Columns: block -> column -> full doc.
	col0A := leafOf(colsBlk.Children[0].Children[0].ID)
	col0B := leafOf(colsBlk.Children[0].Children[1].ID)
	e.SetSelection(col0A.Start+2, col0A.Start+2)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyA)
	wantRange("column 1st (block)", col0A.Start, col0A.End)
	tt.Key(ui.Cmd, ui.KeyA)
	wantRange("column 2nd (column)", col0A.Start, col0B.End)
	tt.Key(ui.Cmd, ui.KeyA)
	wantRange("column 3rd (doc)", 0, e.Len())

	// Inline math: formula body -> enclosing paragraph -> full doc.
	mp := leafOf(mathPara.ID)
	e.SetSelection(mp.Start+6, mp.Start+6) // inside "$x+y$" (between x and +)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyA)
	wantRange("math 1st (body)", mp.Start+5, mp.Start+8)
	tt.Key(ui.Cmd, ui.KeyA)
	wantRange("math 2nd (paragraph)", mp.Start, mp.End)
	tt.Key(ui.Cmd, ui.KeyA)
	wantRange("math 3rd (doc)", 0, e.Len())

	// Empty paragraph escalates straight to full doc.
	ep := leafOf(emptyPara.ID)
	e.SetSelection(ep.Start, ep.Start)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyA)
	wantRange("empty para", 0, e.Len())

	// Block selection mode + Cmd+A selects all top-level blocks.
	e.SetSelection(cl.Start, cl.Start)
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Frame()
	if got := len(e.BlockSelection()); got != len(doc.Blocks) {
		t.Fatalf("block selection Cmd+A selected %d blocks; want %d", got, len(doc.Blocks))
	}

	// Tall code block does not jump scroll position on Cmd+A.
	tallLines := strings.Repeat("int x = 1;\n", 60)
	tallDoc := document.New()
	tallDoc.Blocks = []*document.Block{document.NewBlock("code", tallLines)}
	eTall := FromDocument(tallDoc)
	ttTall := ui.NewTester(func(c *ui.Context) { w, _ := c.Size(); View(c, eTall, Options{Width: w, Theme: c.Theme()}) }, 900, 400)
	ttTall.Frame()
	focus(eTall, ttTall)
	eTall.Scroll.Y = 0
	eTall.SetSelection(0, 0)
	ttTall.Frame()
	ttTall.Key(ui.Cmd, ui.KeyA)
	ttTall.Frame()
	if eTall.Scroll.Y != 0 {
		t.Fatalf("Cmd+A jumped scroll to %v; want 0", eTall.Scroll.Y)
	}
}

func TestIssue17ListIndentationAndSingleBackspaceTab(t *testing.T) {
	for _, kind := range []string{"bullet", "numbered", "task"} {
		t.Run(kind+"-tab-enter-backspace", func(t *testing.T) {
			doc := document.New()
			doc.Blocks = []*document.Block{
				document.NewBlock(kind, "First item"),
				document.NewBlock(kind, "Second item"),
				document.NewBlock(kind, ""),
			}

			e := FromDocument(doc)
			tt := ui.NewTester(func(c *ui.Context) { w, _ := c.Size(); View(c, e, Options{Width: w, Theme: c.Theme()}) }, 900, 650)
			tt.Frame()
			focus(e, tt)

			b1 := func() *document.Block { return e.Doc.Blocks[0] }
			b2 := func() *document.Block { return e.Doc.Blocks[1] }
			b3 := func() *document.Block { return e.Doc.Blocks[2] }

			// Collapsed caret in the middle of b1: Tab indents 0 -> 1 -> 2 without inserting text.
			e.SetSelection(5, 5)
			tt.Frame()
			tt.Key(0, ui.KeyTab)
			tt.Key(0, ui.KeyTab)
			tt.Frame()
			if b1().Indent != 2 || b1().Text() != "First item" {
				t.Fatalf("%s collapsed Tab: indent=%d text=%q", kind, b1().Indent, b1().Text())
			}

			// Shift+Tab outdents 2 -> 1 -> 0.
			tt.Key(ui.Shift, ui.KeyTab)
			tt.Key(ui.Shift, ui.KeyTab)
			tt.Frame()
			if b1().Indent != 0 {
				t.Fatalf("%s collapsed Shift+Tab: indent=%d; want 0", kind, b1().Indent)
			}

			// Multi-item selection indents and outdents both b1 and b2.
			l1, _ := e.Engine.LeafByID(b1().ID)
			l2, _ := e.Engine.LeafByID(b2().ID)
			e.SetSelection(l1.Start+2, l2.Start+3)
			tt.Frame()
			tt.Key(0, ui.KeyTab)
			tt.Frame()
			if b1().Indent != 1 || b2().Indent != 1 {
				t.Fatalf("%s multi-item Tab: indents=(%d,%d); want (1,1)", kind, b1().Indent, b2().Indent)
			}
			tt.Key(ui.Shift, ui.KeyTab)
			tt.Frame()
			if b1().Indent != 0 || b2().Indent != 0 {
				t.Fatalf("%s multi-item Shift+Tab: indents=(%d,%d); want (0,0)", kind, b1().Indent, b2().Indent)
			}

			// Backspace at start of indented list item outdents first, then unstyles.
			b2().Indent = 1
			l2, _ = e.Engine.LeafByID(b2().ID)
			e.SetSelection(l2.Start, l2.Start)
			tt.Frame()
			tt.Key(0, ui.KeyBackspace)
			tt.Frame()
			if b2().Indent != 0 || b2().Kind != kind || b2().Text() != "Second item" {
				t.Fatalf("%s 1st Backspace at start: kind=%q indent=%d text=%q", kind, b2().Kind, b2().Indent, b2().Text())
			}
			tt.Key(0, ui.KeyBackspace)
			tt.Frame()
			if b2().Kind != "paragraph" {
				t.Fatalf("%s 2nd Backspace at start: kind=%q; want paragraph", kind, b2().Kind)
			}

			// Enter on empty indented list item outdents to 0 first, then exits to paragraph.
			b3().Indent = 1
			l3, _ := e.Engine.LeafByID(b3().ID)
			e.SetSelection(l3.Start, l3.Start)
			tt.Frame()
			tt.Key(0, ui.KeyEnter)
			tt.Frame()
			if b3().Indent != 0 || b3().Kind != kind {
				t.Fatalf("%s 1st Enter on empty indented item: kind=%q indent=%d", kind, b3().Kind, b3().Indent)
			}
			tt.Key(0, ui.KeyEnter)
			tt.Frame()
			if b3().Kind != "paragraph" {
				t.Fatalf("%s 2nd Enter on empty item: kind=%q; want paragraph", kind, b3().Kind)
			}
		})
	}

	t.Run("ordered-list-nested-markdown-roundtrip", func(t *testing.T) {
		doc := document.New()
		n0 := document.NewBlock("numbered", "First")
		n1 := document.NewBlock("numbered", "Nested")
		n1.Indent = 1
		n2 := document.NewBlock("numbered", "Deep")
		n2.Indent = 2
		n3 := document.NewBlock("numbered", "Second")
		doc.Blocks = []*document.Block{n0, n1, n2, n3}

		md := ExportMarkdown(doc)
		reimported := ImportMarkdown(md)
		if len(reimported.Blocks) != 4 {
			t.Fatalf("roundtrip blocks=%d; want 4\nmd:\n%s", len(reimported.Blocks), md)
		}
		wantIndents := []int{0, 1, 2, 0}
		for i, b := range reimported.Blocks {
			if b.Kind != "numbered" || b.Indent != wantIndents[i] {
				t.Fatalf("block %d: got kind=%q indent=%d; want numbered indent=%d\nmd:\n%s", i, b.Kind, b.Indent, wantIndents[i], md)
			}
		}
	})

	t.Run("prose-tab-single-backspace", func(t *testing.T) {
		e, tt := newTester("")
		focus(e, tt)

		tt.Key(0, ui.KeyTab)
		tt.Frame()
		if e.Text() != "\t" || e.Len() != 1 {
			t.Fatalf("Tab in prose: text=%q len=%d; want \"\\t\", 1", e.Text(), e.Len())
		}
		r := e.rows[0]
		if r.layout.Caret(1).X <= r.layout.Caret(0).X+15 {
			t.Fatalf("prose Tab did not advance caret visually: c0.X=%v c1.X=%v", r.layout.Caret(0).X, r.layout.Caret(1).X)
		}

		// One single Backspace must delete the entire tab indent.
		tt.Key(0, ui.KeyBackspace)
		tt.Frame()
		if e.Text() != "" || e.Len() != 0 {
			t.Fatalf("single Backspace did not delete tab: text=%q len=%d", e.Text(), e.Len())
		}
	})
}
