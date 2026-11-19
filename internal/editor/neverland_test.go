package editor

import (
	"bytes"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/Ciao1019/ArcMD/internal/wiki"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

func typeKeys(tt *ui.Tester, s string) {
	for _, r := range s {
		tt.Type(string(r))
	}
}

func TestCaretGlideAndBlockSlideMotion(t *testing.T) {
	e, tt := newTester("First\n\nSecond\n\nThird")
	focus(e, tt)
	e.SetSelection(0, 0)
	tt.Frame()
	tt.Advance(250 * time.Millisecond)
	startX := e.motion.caret.current.X

	e.SetSelection(5, 5)
	tt.Frame()
	tt.Advance(24 * time.Millisecond)
	midX := e.motion.caret.current.X
	targetX := e.motion.caret.target.X
	if midX <= startX || midX >= targetX {
		t.Fatalf("caret did not glide smoothly: start=%v mid=%v target=%v", startX, midX, targetX)
	}
	tt.Advance(250 * time.Millisecond)
	if e.motion.caret.current.X != targetX {
		t.Fatalf("caret did not settle at target: got=%v want=%v", e.motion.caret.current.X, targetX)
	}

	// Inserting a paragraph before Second slides Second and Third smoothly.
	thirdID := e.Doc.Blocks[2].ID
	e.SetSelection(5, 5)
	tt.Key(0, ui.KeyEnter)
	if slide := e.motion.blocks[thirdID]; slide.dy == 0 {
		t.Fatal("neighboring block did not start a slide transition on Enter")
	}
	tt.Advance(24 * time.Millisecond)
	midDY := e.motion.blocks[thirdID].dy
	if midDY == 0 {
		t.Fatal("block slide snapped immediately instead of easing")
	}
	// Full relayout at dt == 0 must preserve bit-exact rendering.
	tt.Frame()
	pixels := append([]byte(nil), tt.Image().Pix...)
	e.ready = false
	clear(e.cache)
	tt.Frame()
	if !bytes.Equal(pixels, tt.Image().Pix) {
		t.Fatal("motion state broke incremental vs full-layout pixel determinism")
	}
	tt.Advance(300 * time.Millisecond)
	if e.motion.blocks[thirdID].dy != 0 {
		t.Fatalf("block slide did not settle: dy=%v", e.motion.blocks[thirdID].dy)
	}
}

func TestLiveMathPopoverAndCardFlow(t *testing.T) {
	e, tt := newTester("Energy")
	focus(e, tt)
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	typeKeys(tt, " $")
	if e.Text() != "Energy $$" || e.Caret != 8 || e.inlineMath.blockID == "" {
		t.Fatalf("inline math did not auto-pair and activate: text=%q caret=%d math=%+v", e.Text(), e.Caret, e.inlineMath)
	}
	typeKeys(tt, `E=mc^2`)
	if len(e.rows[0].layout.objects) != 1 || !e.rows[0].layout.objects[0].editing {
		t.Fatalf("inline formula did not stay live-rendered while typing: %+v", e.rows[0].layout.objects)
	}
	tt.Key(0, ui.KeyTab)
	if e.Caret != e.Len() || e.inlineMath.blockID != "" {
		t.Fatalf("Tab did not jump out past closing $: caret=%d len=%d", e.Caret, e.Len())
	}

	// Escape on a newly typed $100 unwraps the auto-paired closing $ into literal $100.
	typeKeys(tt, " cost $100")
	tt.Key(0, ui.KeyEscape)
	if e.Text() != "Energy $E=mc^2$ cost $100" {
		t.Fatalf("Escape did not unwrap literal dollar amount: %q", e.Text())
	}

	// Display math: $$ + Enter opens unified card, double-Enter exits to paragraph.
	tt.Key(0, ui.KeyEnter)
	typeKeys(tt, "$$")
	tt.Key(0, ui.KeyEnter)
	if e.Doc.Blocks[1].Kind != "math" || e.rows[1].mathHidden || e.rows[1].y <= e.rows[1].rect.Y+e.rows[1].mathTop {
		t.Fatalf("display math unified card did not open with preview above source: %+v", e.rows[1])
	}
	typeKeys(tt, `x^2 + 1`)
	tt.Key(0, ui.KeyEnter)
	tt.Key(0, ui.KeyEnter)
	if len(e.Doc.Blocks) != 3 || e.Doc.Blocks[1].Kind != "math" || e.Doc.Blocks[1].Text() != "x^2 + 1" || e.Doc.Blocks[2].Kind != "paragraph" {
		t.Fatalf("double-Enter did not exit display math cleanly: %+v", e.Doc.Blocks)
	}
}

func TestPairScopeTabOutAndSmartTypography(t *testing.T) {
	e, tt := newTester("")
	focus(e, tt)

	// Bracket pair capsule + Tab jump-out.
	typeKeys(tt, "(scope")
	if _, _, ok := e.activePair(); !ok {
		t.Fatal("active pair scope not tracked")
	}
	tt.Key(0, ui.KeyTab)
	if e.Text() != "(scope)" || e.Caret != 7 {
		t.Fatalf("Tab jump-out failed: text=%q caret=%d", e.Text(), e.Caret)
	}

	// Smart curly quotes + straight quote closing skip + contractions.
	e.SetSelection(0, e.Len())
	typeKeys(tt, `"hello" don't`)
	if e.Text() != "“hello” don't" {
		t.Fatalf("smart quotes / contraction mismatch: %q", e.Text())
	}

	// Em-dash + Backspace rollback.
	e.SetSelection(0, e.Len())
	typeKeys(tt, "wait--")
	if e.Text() != "wait—" {
		t.Fatalf("em-dash failed: %q", e.Text())
	}
	tt.Key(0, ui.KeyBackspace)
	if e.Text() != "wait--" {
		t.Fatalf("Backspace did not roll back em-dash: %q", e.Text())
	}

	// Ellipsis + Backspace rollback.
	e.SetSelection(0, e.Len())
	typeKeys(tt, "hmm...")
	if e.Text() != "hmm…" {
		t.Fatalf("ellipsis failed: %q", e.Text())
	}
	tt.Key(0, ui.KeyBackspace)
	if e.Text() != "hmm..." {
		t.Fatalf("Backspace did not roll back ellipsis: %q", e.Text())
	}

	// Triple hyphen on empty paragraph turns into a horizontal rule + paragraph.
	e.SetSelection(0, e.Len())
	typeKeys(tt, "---")
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Kind != "rule" || e.Doc.Blocks[1].Kind != "paragraph" {
		t.Fatalf("--- did not create divider block: %+v", e.Doc.Blocks)
	}
}

func TestLiveInlineMarkdownAndPipeTable(t *testing.T) {
	e, tt := newTester("")
	focus(e, tt)

	// **bold** compiles live and Backspace restores literal **bold**.
	typeKeys(tt, "**bold**")
	if e.Text() != "bold" || len(e.Doc.Blocks[0].Runs) != 1 || !e.Doc.Blocks[0].Runs[0].Style.Bold {
		t.Fatalf("**bold** did not compile live: text=%q runs=%+v", e.Text(), e.Doc.Blocks[0].Runs)
	}
	if e.CurrentStyle().Bold {
		t.Fatal("live bold leaked into typing style after closing **")
	}
	tt.Key(0, ui.KeyBackspace)
	if e.Text() != "**bold**" || e.Doc.Blocks[0].Runs[0].Style.Bold {
		t.Fatalf("Backspace did not restore literal **bold**: text=%q runs=%+v", e.Text(), e.Doc.Blocks[0].Runs)
	}

	// *italic*, ~~strike~~, ~tilde~, `code`, and non-misfiring **word*.
	e.SetSelection(0, e.Len())
	typeKeys(tt, "*em* ~~del~~ ~sub~ `fn()` **open*")
	runs := e.Doc.Blocks[0].Runs
	if e.Text() != "em del sub fn() **open*" {
		t.Fatalf("unexpected live-compiled text: %q (runs=%+v)", e.Text(), runs)
	}

	// Pipe table line compiles into a 2x3 table on Enter and undoes in one step.
	e.SetSelection(0, e.Len())
	typeKeys(tt, "| Port | Low | High")
	tt.Key(0, ui.KeyEnter)
	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "table" {
		t.Fatalf("pipe line did not compile into table: %+v", e.Doc.Blocks)
	}
	tbl := e.Doc.Blocks[0]
	if len(tbl.Children) != 2 || len(tbl.Children[0].Children) != 3 || tbl.Children[0].Children[0].Text() != "Port" {
		t.Fatalf("unexpected compiled table shape: %+v", tbl.Children)
	}
	if leaf := e.LeafAt(e.Caret); leaf.Block.ID != tbl.Children[1].Children[0].ID {
		t.Fatal("caret did not land in Row 1 Col 0 of compiled pipe table")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if e.Doc.Blocks[0].Kind != "paragraph" || e.Text() != "| Port | Low | High" {
		t.Fatalf("pipe table did not undo in one step: %q", e.Text())
	}
}

func TestLiveInlineHTMLTagsAndRollback(t *testing.T) {
	// <kbd> compiles live, exits keycap style immediately after >, and rolls back on Backspace.
	e, tt := newTester("")
	focus(e, tt)
	typeKeys(tt, "<kbd>Ctrl</kbd>")
	if e.Text() != "Ctrl" {
		t.Fatalf("<kbd>Ctrl</kbd> did not compile cleanly: %q", e.Text())
	}
	runs := e.Doc.Blocks[0].Runs
	if len(runs) != 1 || !runs[0].Style.Kbd || runs[0].Style.Code {
		t.Fatalf("expected Kbd run without Code, got %+v", runs)
	}
	typeKeys(tt, "+K")
	if e.Text() != "Ctrl+K" {
		t.Fatalf("expected Ctrl+K after continuing to type, got %q", e.Text())
	}
	runs = e.Doc.Blocks[0].Runs
	if len(runs) != 2 || !runs[0].Style.Kbd || runs[1].Style.Kbd {
		t.Fatalf("typing after </kbd> leaked Kbd style: %+v", runs)
	}

	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<kbd>Shift</kbd>")
	if e.Text() != "Shift" {
		t.Fatalf("expected Shift before rollback, got %q", e.Text())
	}
	tt.Key(0, ui.KeyBackspace)
	if e.Text() != "<kbd>Shift</kbd>" {
		t.Fatalf("Backspace did not roll back <kbd>Shift</kbd>: %q", e.Text())
	}
	if len(e.Doc.Blocks[0].Runs) != 1 || e.Doc.Blocks[0].Runs[0].Style.Kbd {
		t.Fatalf("rolled-back <kbd> should be plain text: %+v", e.Doc.Blocks[0].Runs)
	}

	// <u>, <sup>, <sub>, <mark>, <span style="color:red"> compile live.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, `<u>重点</u> x<sup>2</sup> H<sub>2</sub>O <mark>hi</mark> <span style="color:red">red</span>`)
	runs = e.Doc.Blocks[0].Runs
	var gotU, gotSup, gotSub, gotMark, gotRed bool
	for _, r := range runs {
		switch {
		case r.Text == "重点" && r.Style.Underline:
			gotU = true
		case r.Text == "2" && r.Style.Script == "sup":
			gotSup = true
		case r.Text == "2" && r.Style.Script == "sub":
			gotSub = true
		case r.Text == "hi" && r.Style.Highlight == "light-yellow":
			gotMark = true
		case r.Text == "red" && r.Style.Color == "red":
			gotRed = true
		}
	}
	if !gotU || !gotSup || !gotSub || !gotMark || !gotRed {
		t.Fatalf("missing expected live-compiled runs (u=%v sup=%v sub=%v mark=%v red=%v) in %+v",
			gotU, gotSup, gotSub, gotMark, gotRed, runs)
	}

	// <q>quote</q> compiles to typographic quotes and rolls back cleanly on Backspace.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<q>quote</q>")
	if e.Text() != "“quote”" {
		t.Fatalf("<q>quote</q> did not compile to smart quotes: %q", e.Text())
	}
	tt.Key(0, ui.KeyBackspace)
	if e.Text() != "<q>quote</q>" {
		t.Fatalf("Backspace did not roll back <q>quote</q>: %q", e.Text())
	}

	// <br> compiles to U+2028 in paragraphs (and rolls back) and to \n in table cells.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "line1<br>")
	if e.Text() != "line1\u2028" {
		t.Fatalf("<br> in paragraph got %q, want %q", e.Text(), "line1\u2028")
	}
	tt.Key(0, ui.KeyBackspace)
	if e.Text() != "line1<br>" {
		t.Fatalf("Backspace did not roll back <br>: %q", e.Text())
	}
	e, tt = newTester("| H |\n| --- |\n| a |")
	focus(e, tt)
	e.SetSelection(5, 5) // inside second row cell "a"
	tt.Frame()
	typeKeys(tt, "<br/>b")
	table := e.Doc.Blocks[0]
	if table.Kind != "table" || len(table.Children) < 2 || len(table.Children[1].Children) == 0 {
		t.Fatalf("unexpected table structure: %+v", table)
	}
	cell := table.Children[1].Children[0]
	if cell.Kind != "cell" || cell.Text() != "a\nb" {
		t.Fatalf("<br/> in table cell got kind=%q text=%q, want cell %q", cell.Kind, cell.Text(), "a\nb")
	}

	// Empty-paragraph <hr> / <hr/> compiles to rule + trailing paragraph.
	for _, hr := range []string{"<hr>", "<hr/>"} {
		e, tt = newTester("")
		focus(e, tt)
		typeKeys(tt, hr)
		if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Kind != "rule" || e.Doc.Blocks[1].Kind != "paragraph" {
			t.Fatalf("%s did not convert to rule+paragraph: %+v", hr, e.Doc.Blocks)
		}
	}

	// Non-triggers stay literal.
	for _, lit := range []string{"vector<int>", "a < b > c", "<span>plain</span>"} {
		e, tt = newTester("")
		focus(e, tt)
		typeKeys(tt, lit)
		if e.Text() != lit {
			t.Fatalf("non-trigger %q mutated into %q", lit, e.Text())
		}
	}

	// Repro: <kbd>Alt</kbd> -> Backspace rollback -> Ctrl+Z -> Enter -> typing "<" stays plain.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<kbd>Alt</kbd>")
	tt.Key(0, ui.KeyBackspace)
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Key(0, ui.KeyEnter)
	tt.Type("<")
	if len(e.Doc.Blocks) != 2 || len(e.Doc.Blocks[1].Runs) != 1 || e.Doc.Blocks[1].Runs[0].Style.Kbd || e.Doc.Blocks[1].Text() != "<" {
		t.Fatalf("Enter after <kbd> rollback+undo leaked Kbd: %+v", e.Doc.Blocks)
	}

	// Deleting whole <kbd>Alt</kbd> span via Backspace resets Kbd so "<" is plain.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<kbd>Alt</kbd>")
	tt.Key(0, ui.KeyBackspace)
	tt.Key(ui.Cmd, ui.KeyZ)
	for range 3 {
		tt.Key(0, ui.KeyBackspace)
	}
	tt.Type("<")
	if runs := e.Doc.Blocks[0].Runs; len(runs) != 1 || runs[0].Style.Kbd || runs[0].Text != "<" {
		t.Fatalf("deleting <kbd>Alt</kbd> leaked Kbd: %+v", runs)
	}

	// Pressing KeyRight at end of a trailing <kbd> span exits capsule style.
	e, tt = newTester("Press ")
	focus(e, tt)
	e.SetSelection(6, 6)
	tt.Frame()
	typeKeys(tt, "<kbd>Alt</kbd>")
	e.SetSelection(9, 9) // simulate clicking/navigating to end of keycap
	tt.Frame()
	tt.Key(0, ui.KeyRight)
	tt.Type("<")
	if runs := e.Doc.Blocks[0].Runs; len(runs) != 3 || !runs[1].Style.Kbd || runs[2].Style.Kbd || runs[2].Text != "<" {
		t.Fatalf("KeyRight at end of <kbd> did not exit capsule style: %+v", runs)
	}
}

func TestWrapKbdAndVectorPDFKeycapAndComment(t *testing.T) {
	e, tt := newTester("Shortcut")
	focus(e, tt)
	e.SetSelection(0, len("Shortcut"))
	tt.Frame()
	e.Wrap("<kbd>")
	if runs := e.Doc.Blocks[0].Runs; len(runs) != 1 || !runs[0].Style.Kbd || runs[0].Style.Code {
		t.Fatalf("Wrap(<kbd>) did not set Kbd: %+v", runs)
	}
	e.Wrap("<kbd>")
	if runs := e.Doc.Blocks[0].Runs; len(runs) != 1 || runs[0].Style.Kbd {
		t.Fatalf("second Wrap(<kbd>) did not toggle Kbd off: %+v", runs)
	}

	doc := ImportMarkdown("Press <kbd>Ctrl</kbd> + <kbd>K</kbd>\n\n<!-- secret comment -->\n")
	e2 := FromOwnedDocument(doc)
	if len(doc.Blocks) != 2 || doc.Blocks[1].Kind != "raw" {
		t.Fatalf("expected paragraph + raw comment block, got %+v", doc.Blocks)
	}
	var tops []float32
	var opts Options
	ui.Render(func(c *ui.Context) {
		opts = Options{Width: 680, Theme: c.Theme()}
		tops = e2.Paginate(opts, 900)
	}, 680, 900, 1)
	if len(tops) == 0 {
		t.Fatal("expected at least one paginated PDF page")
	}
	if _, laid := e2.rowIDs[doc.Blocks[1].ID]; laid {
		t.Fatal("expected standalone HTML comment block to be omitted from print/PDF layout")
	}
	plainDoc := ImportMarkdown("Press <kbd>Ctrl</kbd> + <kbd>K</kbd>\n")
	ePlain := FromOwnedDocument(plainDoc)
	ui.Render(func(c *ui.Context) {
		ePlain.Paginate(Options{Width: 680, Theme: c.Theme()}, 900)
	}, 680, 900, 1)
	if e2.contentEnd != ePlain.contentEnd {
		t.Fatalf("omitted comment block left blank vertical gap in PDF layout: withComment=%v without=%v", e2.contentEnd, ePlain.contentEnd)
	}
	var out bytes.Buffer
	w := pdf.NewWriter(&out, "HTML & Keycap Test")
	vp := w.NewVectorPage(pdf.A4Width, pdf.A4Height)
	e2.ExportVectorPage(vp, opts, tops[0], tops[0]+900, 0, len(tops), tops, 680, 980, 40)
	if err := w.FinishVectorPage(vp); err != nil {
		t.Fatalf("FinishVectorPage failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("pdf writer close failed: %v", err)
	}
	if !bytes.HasPrefix(out.Bytes(), []byte("%PDF-")) {
		t.Fatalf("expected valid PDF header, got %q", out.Bytes()[:min(16, out.Len())])
	}
}

func TestComprehensiveHTMLEditingWorkflow(t *testing.T) {
	// 1. Forward opening-tag compilation: rollback <kbd>Alt</kbd>, delete '>' of <kbd>, retype '>'.
	e, tt := newTester("")
	focus(e, tt)
	typeKeys(tt, "<kbd>Alt</kbd>")
	tt.Key(0, ui.KeyBackspace) // rolls back to "<kbd>Alt</kbd>" with caret at end of "<kbd>"? No, at end of "</kbd>".
	// Place caret after "<kbd" (position 4) after deleting '>' at position 5, then retype '>'.
	e.SetSelection(5, 5)
	tt.Frame()
	tt.Key(0, ui.KeyBackspace) // "<kbdAlt</kbd>"
	tt.Type(">")
	if e.Text() != "Alt" || len(e.Doc.Blocks[0].Runs) != 1 || !e.Doc.Blocks[0].Runs[0].Style.Kbd {
		t.Fatalf("forward opening-tag compile failed on <kbd>: text=%q runs=%+v", e.Text(), e.Doc.Blocks[0].Runs)
	}

	// Changing <u>hi</u> to <mark>hi</mark> by editing opening tag triggers forward compile.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<u>hi</mark>") // unmatched closer stays literal
	e.SetSelection(0, 3)         // select "<u>"
	tt.Frame()
	typeKeys(tt, "<mark>") // delete selection and type opening tag up to '>'
	// Ensure <mark> compiles with </mark>
	if e.Text() != "hi" || len(e.Doc.Blocks[0].Runs) != 1 || e.Doc.Blocks[0].Runs[0].Style.Highlight != "light-yellow" {
		t.Fatalf("forward <mark> compile failed: text=%q runs=%+v", e.Text(), e.Doc.Blocks[0].Runs)
	}

	// 2. Leading [Esc]退出: Home -> KeyLeft -> typing "按 " stays plain outside [Esc].
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<kbd>Esc</kbd>退出")
	tt.Key(0, ui.KeyHome)
	tt.Key(0, ui.KeyLeft)
	tt.Type("按 ")
	runs := e.Doc.Blocks[0].Runs
	if len(runs) != 3 || runs[0].Text != "按 " || runs[0].Style.Kbd || runs[1].Text != "Esc" || !runs[1].Style.Kbd || runs[2].Text != "退出" || runs[2].Style.Kbd {
		t.Fatalf("Home + KeyLeft before leading <kbd> leaked Kbd: %+v", runs)
	}

	// 3. Interior "按 [Ctrl] 打开": move to right wall of Ctrl -> KeyRight -> "+K" stays plain.
	e, tt = newTester("按 Ctrl 打开")
	focus(e, tt)
	e.SetSelection(2, 6) // select "Ctrl"
	tt.Frame()
	e.Wrap("<kbd>")
	e.SetSelection(6, 6) // caret right at right edge of [Ctrl]
	tt.Frame()
	tt.Key(0, ui.KeyRight)
	tt.Type("+K")
	runs = e.Doc.Blocks[0].Runs
	if len(runs) != 3 || runs[0].Text != "按 " || !runs[1].Style.Kbd || runs[1].Text != "Ctrl" || runs[2].Style.Kbd || runs[2].Text != "+K 打开" {
		t.Fatalf("interior KeyRight exit from <kbd> failed: %+v", runs)
	}

	// 4. Multi-block selection starting inside <kbd> -> Backspace -> typing "<" stays plain.
	e, tt = newTester("Press <kbd>Ctrl</kbd>\n\nSecond paragraph")
	focus(e, tt)
	e.SetSelection(6, e.Len()) // from inside "Ctrl" across block boundary to end
	tt.Frame()
	tt.Key(0, ui.KeyBackspace)
	tt.Type("<")
	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Text() != "Press <" {
		t.Fatalf("multi-block delete from <kbd> unexpected: %+v", e.Doc.Blocks)
	}
	if last := e.Doc.Blocks[0].Runs[len(e.Doc.Blocks[0].Runs)-1]; last.Style.Kbd {
		t.Fatalf("multi-block delete from <kbd> leaked Kbd onto <: %+v", e.Doc.Blocks[0].Runs)
	}

	// 5. Special-character <kbd> Markdown round-trip.
	specials := []string{"⌘[", "[[x]]", "$x$", "*", "<", `\`}
	doc := ImportMarkdown("")
	for _, sp := range specials {
		_ = doc
		eng, engTT := newTester("")
		focus(eng, engTT)
		engTT.Type(sp)
		eng.SetSelection(0, eng.Len())
		engTT.Frame()
		eng.Wrap("<kbd>")
		md := ExportMarkdown(eng.Doc)
		reimported := ImportMarkdown(md)
		if got := reimported.PlainText(); got != sp {
			t.Fatalf("special kbd %q round-trip mismatch via %q: got %q", sp, md, got)
		}
		if len(reimported.Blocks) != 1 || len(reimported.Blocks[0].Runs) != 1 || !reimported.Blocks[0].Runs[0].Style.Kbd {
			t.Fatalf("special kbd %q lost Kbd style via %q: %+v", sp, md, reimported.Blocks)
		}
	}

	// 6. <font color="blue">蓝字</font> live compilation.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, `<font color="blue">蓝字</font>`)
	if len(e.Doc.Blocks[0].Runs) != 1 || e.Doc.Blocks[0].Runs[0].Text != "蓝字" || e.Doc.Blocks[0].Runs[0].Style.Color != "blue" {
		t.Fatalf("<font color=blue> live compile failed: %+v", e.Doc.Blocks[0].Runs)
	}
}

func TestRound2HTMLCommentTypingCaseAndAlignExport(t *testing.T) {
	// 1. Hand-typing <!-- ... --> preserves "--" and straight ASCII quotes, promotes to raw comment + next paragraph, and undoes in one Cmd+Z step.
	e, tt := newTester("")
	focus(e, tt)
	commentSrc := `<!-- 备注：检查 a < b 且含 "引号" -->`
	typeKeys(tt, commentSrc)
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Kind != "raw" || e.Doc.Blocks[0].Text() != commentSrc || !isHTMLCommentBlock(e.Doc.Blocks[0].Text()) {
		t.Fatalf("hand-typed comment block unexpected: %+v", e.Doc.Blocks)
	}
	if e.Doc.Blocks[1].Kind != "paragraph" || e.Caret != e.LeafAt(e.Caret).Start {
		t.Fatalf("expected focused trailing paragraph after comment promotion, got %+v caret=%d", e.Doc.Blocks, e.Caret)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "paragraph" || !strings.HasPrefix(e.Doc.Blocks[0].Text(), `<!-- 备注：检查 a < b 且含 "引号"`) {
		t.Fatalf("Cmd+Z did not undo comment promotion back to paragraph cleanly: %+v", e.Doc.Blocks)
	}

	// 2. Trailing raw comment block supports double-Enter exit into a new paragraph.
	e, tt = newTester("<!-- 尾部注释 -->\n")
	focus(e, tt)
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Key(0, ui.KeyEnter)
	tt.Type("正文")
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Kind != "raw" || e.Doc.Blocks[0].Text() != "<!-- 尾部注释 -->" {
		t.Fatalf("double-Enter exit on raw block damaged comment block: %+v", e.Doc.Blocks)
	}
	if e.Doc.Blocks[1].Kind != "paragraph" || e.Doc.Blocks[1].Text() != "正文" {
		t.Fatalf("double-Enter did not focus new paragraph after raw block: %+v", e.Doc.Blocks)
	}

	// 3. Uppercase HTML tags work in both live typing and Markdown import.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<KBD>F1</KBD> <MARK>亮</MARK><BR>换")
	runs := e.Doc.Blocks[0].Runs
	if !runs[0].Style.Kbd || runs[0].Text != "F1" || !strings.Contains(e.Text(), "\u2028换") {
		t.Fatalf("uppercase inline live tags failed: %+v text=%q", runs, e.Text())
	}
	var gotMark bool
	for _, r := range runs {
		if strings.Contains(r.Text, "亮") && r.Style.Highlight == "light-yellow" {
			gotMark = true
		}
	}
	if !gotMark {
		t.Fatalf("uppercase <MARK> did not compile highlight: %+v", runs)
	}

	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<HR>")
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Kind != "rule" {
		t.Fatalf("uppercase <HR> failed: %+v", e.Doc.Blocks)
	}

	upperMD := ImportMarkdown("<DIV ALIGN=\"CENTER\">\n\n### 居中\n\n</DIV>\n\n<KBD>F1</KBD>+<KBD>&lt;</KBD>\n")
	if len(upperMD.Blocks) != 2 || upperMD.Blocks[0].Align != "center" || upperMD.Blocks[1].Runs[0].Text != "F1" || !upperMD.Blocks[1].Runs[0].Style.Kbd || upperMD.Blocks[1].Runs[2].Text != "<" || !upperMD.Blocks[1].Runs[2].Style.Kbd {
		t.Fatalf("uppercase ImportMarkdown failed: %+v", upperMD.Blocks)
	}

	// 4. ExportHTML preserves center/right block alignment and escapes <kbd>&lt;</kbd>.
	alignDoc := ImportMarkdown("<div align=\"center\">\n\n居中段落\n\n</div>\n\n<div align=\"right\">\n\n右对齐段落\n\n</div>\n\n<kbd>&lt;</kbd>\n")
	exportedHTML := ExportHTML(alignDoc)
	if !strings.Contains(exportedHTML, `<p style="text-align:center">居中段落</p>`) ||
		!strings.Contains(exportedHTML, `<p style="text-align:right">右对齐段落</p>`) ||
		!strings.Contains(exportedHTML, `<kbd>&lt;</kbd>`) {
		t.Fatalf("ExportHTML alignment or <kbd>&lt;</kbd> unexpected:\n%s", exportedHTML)
	}
}

func TestRound3HTMLAdversarialStress(t *testing.T) {
	// 1. <center>\n...\n</center> has no leading/trailing raw '\n' and does not shatter on edit.
	e, tt := newTester("<center>\n单段或多段 center 标签同样居中显示。\n</center>\n")
	focus(e, tt)
	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Align != "center" || e.Doc.Blocks[0].Text() != "单段或多段 center 标签同样居中显示。" {
		t.Fatalf("<center> whitespace normalization failed: %+v", e.Doc.Blocks)
	}
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	tt.Type("追加")
	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Align != "center" || e.Doc.Blocks[0].Text() != "单段或多段 center 标签同样居中显示。追加" {
		t.Fatalf("editing inside <center> shattered or lost alignment: %+v", e.Doc.Blocks)
	}

	// 2. <p align="center">第一行<br>\n  第二行</p> uses U+2028 (not raw \n) and stays 1 block on edit.
	e, tt = newTester("<p align=\"center\">第一行<br>\n  第二行</p>\n")
	focus(e, tt)
	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Text() != "第一行\u2028第二行" {
		t.Fatalf("<p align=center> <br> normalization failed: %+v text=%q", e.Doc.Blocks, e.Text())
	}
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	tt.Type("!")
	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Text() != "第一行\u2028第二行!" {
		t.Fatalf("editing inside <p align=center> with <br> split paragraph: %+v", e.Doc.Blocks)
	}

	// 3. No-blank-line GitHub README <div align="center"> and <center> with block children.
	readme := "<div align=\"center\">\n  <h1>ArcMD</h1>\n  <p>按 <kbd>Ctrl</kbd> + <kbd>K</kbd> 启动。</p>\n  <hr>\n</div>\n"
	doc := ImportMarkdown(readme)
	if len(doc.Blocks) != 3 ||
		doc.Blocks[0].Kind != "heading" || doc.Blocks[0].Level != 1 || doc.Blocks[0].Align != "center" || doc.Blocks[0].Text() != "ArcMD" ||
		doc.Blocks[1].Kind != "paragraph" || doc.Blocks[1].Align != "center" ||
		doc.Blocks[2].Kind != "rule" {
		t.Fatalf("no-blank-line <div align=center> failed: %+v", doc.Blocks)
	}
	if out := ExportMarkdown(doc); out != readme {
		t.Fatalf("untouched no-blank-line <div align=center> changed on export:\ngot:\n%s\nwant:\n%s", out, readme)
	}

	centerMulti := ImportMarkdown("<center>\n  <h3>标题</h3>\n  <p>第一段</p>\n  <p>第二段</p>\n</center>\n")
	if len(centerMulti.Blocks) != 3 ||
		centerMulti.Blocks[0].Kind != "heading" || centerMulti.Blocks[0].Level != 3 || centerMulti.Blocks[0].Align != "center" ||
		centerMulti.Blocks[1].Kind != "paragraph" || centerMulti.Blocks[1].Text() != "第一段" || centerMulti.Blocks[1].Align != "center" ||
		centerMulti.Blocks[2].Kind != "paragraph" || centerMulti.Blocks[2].Text() != "第二段" || centerMulti.Blocks[2].Align != "center" {
		t.Fatalf("no-blank-line <center> block children failed: %+v", centerMulti.Blocks)
	}

	// 4. Clipboard HTML import preserves align, <center>, <kbd>, <font color="red">, and rich() recognizes centered plain paragraph.
	pasted := ImportHTML(`<div align="center"><h2>居中标题</h2><p>按 <kbd>Ctrl</kbd> + <font color="red">红字</font></p></div><center><p>纯居中</p></center>`)
	if len(pasted.Blocks) != 3 ||
		pasted.Blocks[0].Kind != "heading" || pasted.Blocks[0].Align != "center" ||
		pasted.Blocks[1].Kind != "paragraph" || pasted.Blocks[1].Align != "center" ||
		pasted.Blocks[2].Kind != "paragraph" || pasted.Blocks[2].Align != "center" {
		t.Fatalf("clipboard ImportHTML lost align/center: %+v", pasted.Blocks)
	}
	var gotKbd, gotRed bool
	for _, r := range pasted.Blocks[1].Runs {
		if r.Text == "Ctrl" && r.Style.Kbd {
			gotKbd = true
		}
		if r.Text == "红字" && r.Style.Color == "red" {
			gotRed = true
		}
	}
	if !gotKbd || !gotRed {
		t.Fatalf("clipboard ImportHTML lost <kbd> or <font color=red>: %+v", pasted.Blocks[1].Runs)
	}
	centeredOnly := ImportHTML(`<p style="text-align:center">纯居中</p>`)
	if !rich(centeredOnly) {
		t.Fatal("rich() should return true for centered plain paragraph")
	}

	// 5. Multi-line standalone comment + multiple inline comments in prose.
	commentsMD := "前文<!-- 1 -->中段<!-- 2 -->后文\n\n<!--\n第一行备注\n第二行备注\n-->\n"
	cdoc := ImportMarkdown(commentsMD)
	if len(cdoc.Blocks) != 2 || cdoc.Blocks[0].Text() != "前文中段后文" || cdoc.Blocks[1].Kind != "raw" || !isHTMLCommentBlock(cdoc.Blocks[1].Text()) {
		t.Fatalf("multi/inline comments import failed: %+v", cdoc.Blocks)
	}
	if htmlOut := ExportHTML(cdoc); strings.Contains(htmlOut, "第一行备注") {
		t.Fatalf("ExportHTML leaked multi-line standalone comment:\n%s", htmlOut)
	}

	// 6. Nested live tags (<b><u>重点</u></b>, <mark>x<sup>2</sup></mark>), IME after <kbd>, and <hr> safety in list/quote/table.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<b><u>重点</u></b> <mark>x<sup>2</sup></mark>")
	var gotBoldU, gotMarkSup bool
	for _, r := range e.Doc.Blocks[0].Runs {
		if r.Text == "重点" && r.Style.Bold && r.Style.Underline {
			gotBoldU = true
		}
		if r.Text == "2" && r.Style.Highlight == "light-yellow" && r.Style.Script == "sup" {
			gotMarkSup = true
		}
	}
	if !gotBoldU || !gotMarkSup {
		t.Fatalf("nested live HTML tags failed (boldU=%v markSup=%v): %+v", gotBoldU, gotMarkSup, e.Doc.Blocks[0].Runs)
	}

	// IME composition immediately after <kbd>Ctrl</kbd> stays plain outside keycap.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<kbd>Ctrl</kbd>")
	tt.Compose("中文", 2)
	tt.Type("中文")
	if runs := e.Doc.Blocks[0].Runs; len(runs) != 2 || !runs[0].Style.Kbd || runs[1].Style.Kbd || runs[1].Text != "中文" {
		t.Fatalf("IME commit right after <kbd> leaked Kbd: %+v", runs)
	}

	// <hr> inside bullet list must NOT corrupt list into a rule block.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "- <hr>")
	if e.Doc.Blocks[0].Kind != "bullet" || e.Doc.Blocks[0].Text() != "<hr>" {
		t.Fatalf("<hr> inside bullet list should stay literal: kind=%q text=%q", e.Doc.Blocks[0].Kind, e.Doc.Blocks[0].Text())
	}

	// 7. Malformed / adversarial HTML inputs must never panic in ImportMarkdown, ImportHTML, ExportMarkdown, ExportHTML, or live typing.
	malformed := []string{
		"<kbd></kbd>",
		"<>",
		"</>",
		"<!--",
		"<!-->",
		"<!---->",
		`<br foo="bar">`,
		`<hr class="x">`,
		`<div align="center">unclosed`,
		`</div>`,
		`<span><span><span>deep</span></span></span>`,
		`<kbd>Ctrl</kbd><kbd>C</kbd>`,
		`a < b && c > d`,
	}
	for _, src := range malformed {
		d := ImportMarkdown(src)
		_ = ExportMarkdown(d)
		_ = ExportHTML(d)
		_ = ImportHTML(src)
		ed, ett := newTester("")
		focus(ed, ett)
		typeKeys(ett, src)
		ett.Frame()
	}
}

func TestTableEnterFlowAndRowDragReorder(t *testing.T) {
	e, tt := newTester("| ColA | ColB |\n| --- | --- |\n| R1A | R1B |")
	focus(e, tt)
	tbl := e.Doc.Blocks[0]

	// Enter in Row 0 Col 0 moves straight down to Row 1 Col 0.
	l00, _ := e.Engine.LeafByID(tbl.Children[0].Children[0].ID)
	e.SetSelection(l00.End, l00.End)
	tt.Key(0, ui.KeyEnter)
	if e.LeafAt(e.Caret).Block.ID != tbl.Children[1].Children[0].ID {
		t.Fatal("Enter did not move down to same column")
	}

	// Enter at Row 1 Col 1 (end of filled row) appends Row 2 and wraps to Col 0.
	l11, _ := e.Engine.LeafByID(tbl.Children[1].Children[1].ID)
	e.SetSelection(l11.End, l11.End)
	tt.Key(0, ui.KeyEnter)
	tbl = e.Doc.Blocks[0]
	if len(tbl.Children) != 3 || e.LeafAt(e.Caret).Block.ID != tbl.Children[2].Children[0].ID {
		t.Fatal("Enter on last column did not append row and focus Col 0")
	}

	// Pressing Enter again on the empty trailing row at Col 0 exits the table cleanly.
	tt.Key(0, ui.KeyEnter)
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Kind != "table" || len(e.Doc.Blocks[0].Children) != 2 || e.Doc.Blocks[1].Kind != "paragraph" {
		t.Fatalf("double-Enter did not exit trailing blank table row: %+v", e.Doc.Blocks)
	}

	// Add a second data row and drag row 2 above row 1 using its rowBar.
	l10, _ := e.Engine.LeafByID(e.Doc.Blocks[0].Children[1].Children[0].ID)
	e.SetSelection(l10.End, l10.End)
	tt.Key(0, ui.KeyEnter)
	typeKeys(tt, "R2A")
	tt.Frame()

	bounds, _ := tt.Find("文档")
	d, ok := e.tableDecoration(e.Doc.Blocks[0].ID)
	if !ok {
		t.Fatal("missing table decoration")
	}
	rb2 := rowBar(d, 2)
	rb1 := rowBar(d, 1)
	sx, sy := bounds.X+rb2.X+rb2.W/2, bounds.Y+rb2.Y+rb2.H/2
	ty := bounds.Y + rb1.Y + 4
	tt.Move(sx, sy)
	tt.Frame()
	tt.Press(sx, sy)
	tt.Move(sx, ty)
	tt.Frame()
	tt.Release(sx, ty)
	tt.Frame()

	tbl = e.Doc.Blocks[0]
	if tbl.Children[1].Children[0].Text() != "R2A" || tbl.Children[2].Children[0].Text() != "R1A" {
		t.Fatalf("row drag did not reorder rows: r1=%q r2=%q", tbl.Children[1].Children[0].Text(), tbl.Children[2].Children[0].Text())
	}
}

func TestImageCaptionEnterAndWidthPill(t *testing.T) {
	bitmap := ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 600, 360)))
	e, tt := imageTester("![photo.png](photo.png)", 900, ImageResource{Bitmap: bitmap}, nil, nil)
	imgID := e.Doc.Blocks[0].ID

	e.FocusInsertedImage(imgID)
	tt.Frame()
	typeKeys(tt, "Star trails, 2 hours")
	tt.Key(0, ui.KeyEnter)
	typeKeys(tt, "Body paragraph below")
	tt.Frame()

	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Kind != "image" || e.Doc.Blocks[0].Text() != "Star trails, 2 hours" {
		t.Fatalf("image caption flow failed: %+v", e.Doc.Blocks)
	}
	if e.Doc.Blocks[1].Kind != "paragraph" || e.Doc.Blocks[1].Text() != "Body paragraph below" {
		t.Fatalf("Enter from image caption did not create paragraph below: %+v", e.Doc.Blocks)
	}
	if !e.captionVisible(e.Doc.Blocks[0]) {
		t.Fatal("user-typed caption disappeared after leaving the image block")
	}

	// Hover the image to reveal its resize handles and drag to verify live width percentage state.
	bounds, _ := tt.Find("文档")
	pic := e.rows[0].picture.bounds
	tt.Move(bounds.X+pic.X+pic.W/2, bounds.Y+pic.Y+pic.H/2)
	tt.Frame()
	r, ok := tt.Find("调整图片宽度")
	if !ok {
		t.Fatal("missing image resize handle")
	}
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	tt.Move(r.X-60, r.Y+r.H/2)
	tt.Frame()
	if e.imageResize.id != imgID {
		t.Fatal("image resize state not active during drag")
	}
	tt.Release(r.X-60, r.Y+r.H/2)
	tt.Frame()
}

func TestSmoothScrollGlideManualInterruptAndFootnoteFlash(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("Line ")
		b.WriteByte(byte('A' + i%26))
		b.WriteString("\n\n")
	}
	b.WriteString("Anchor[^1]\n\n[^1]: Note target at bottom")
	e := New(b.String())
	typewriter := true
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), Typewriter: typewriter})
	}, 900, 420)
	tt.Frame()
	focus(e, tt)

	// Move caret to paragraph 20: typewriter mode starts a smooth scroll glide.
	leaf20, _ := e.Engine.LeafByID(e.Doc.Blocks[20].ID)
	e.SetSelection(leaf20.Start, leaf20.Start)
	e.reveal = true
	tt.Frame()
	if !e.scrollGlide.active || e.Scroll.Y <= 0 || e.Scroll.Y >= e.scrollGlide.to {
		t.Fatalf("typewriter glide did not start smoothly: active=%v y=%v to=%v", e.scrollGlide.active, e.Scroll.Y, e.scrollGlide.to)
	}
	wantY := e.scrollGlide.to
	tt.Advance(400 * time.Millisecond)
	if e.scrollGlide.active || abs32(e.Scroll.Y-wantY) > 1 {
		t.Fatalf("scroll glide did not settle at target: active=%v y=%v want=%v", e.scrollGlide.active, e.Scroll.Y, wantY)
	}

	// Start another glide, then interrupt manually — glide cancels immediately.
	e.SetSelection(0, 0)
	e.reveal = true
	tt.Frame()
	if !e.scrollGlide.active {
		t.Fatal("expected upward scroll glide")
	}
	manualY := e.Scroll.Y + 45
	e.Scroll.Y = manualY
	tt.Frame()
	if e.scrollGlide.active || abs32(e.Scroll.Y-manualY) > 1 {
		t.Fatalf("manual scroll did not cancel glide: active=%v y=%v want=%v", e.scrollGlide.active, e.Scroll.Y, manualY)
	}

	// Footnote jump and return flash their destination blocks.
	typewriter = false
	anchorBlock := e.Doc.Blocks[40]
	noteBlock := e.Doc.Blocks[41]
	anchorLeaf, _ := e.Engine.LeafByID(anchorBlock.ID)
	e.SetSelection(anchorLeaf.End, anchorLeaf.End)
	tt.Frame()
	if !e.FollowFootnote() {
		t.Fatal("FollowFootnote did not jump to note")
	}
	if e.bookmarks.flash != noteBlock.ID {
		t.Fatalf("FollowFootnote did not flash note block: got=%q want=%q", e.bookmarks.flash, noteBlock.ID)
	}
	if !e.ReturnFootnote() {
		t.Fatal("ReturnFootnote did not jump back to reference")
	}
	if e.bookmarks.flash != anchorBlock.ID {
		t.Fatalf("ReturnFootnote did not flash origin block: got=%q want=%q", e.bookmarks.flash, anchorBlock.ID)
	}
}

func TestFocusAfterglowFoldChevronAndTaskBounce(t *testing.T) {
	e := New("# Section\n\nBody one\n\n- [ ] Ship release")
	dim := true
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), Dim: dim})
	}, 900, 500)
	tt.Frame()
	focus(e, tt)

	secID := e.Doc.Blocks[0].ID
	bodyID := e.Doc.Blocks[1].ID
	taskID := e.Doc.Blocks[2].ID
	e.SetSelection(0, 0)
	tt.Frame()

	// Moving focus from Section to Body one lights Body one immediately and leaves
	// a decaying spotlight afterglow on Section.
	bodyLeaf, _ := e.Engine.LeafByID(bodyID)
	e.SetSelection(bodyLeaf.Start, bodyLeaf.Start)
	tt.Frame()
	if g := e.motion.dimGlow[secID]; g <= 0 {
		t.Fatalf("exited block did not get focus spotlight afterglow: %v", g)
	}
	if g := e.motion.dimGlow[bodyID]; g != 0 {
		t.Fatalf("entered block should light up immediately without dimGlow: %v", g)
	}
	tt.Advance(500 * time.Millisecond)
	if g := e.motion.dimGlow[secID]; g != 0 {
		t.Fatalf("focus afterglow did not decay to zero: %v", g)
	}

	// Folding Section rotates its chevron from 0 toward -90; unfolding animates
	// revealed child blocks smoothly.
	dim = false
	e.toggleFold(secID)
	tt.Frame()
	tt.Advance(24 * time.Millisecond)
	angle := e.motion.foldAngles[secID]
	if angle >= 0 || angle <= -90 {
		t.Fatalf("fold chevron did not animate smoothly between 0 and -90: %v", angle)
	}
	tt.Advance(350 * time.Millisecond)
	if e.motion.foldAngles[secID] != -90 {
		t.Fatalf("fold chevron did not settle at -90: %v", e.motion.foldAngles[secID])
	}

	e.toggleFold(secID)
	tt.Frame()
	if e.motion.blocks[bodyID].dy == 0 || e.motion.foldFade[bodyID] == 0 {
		t.Fatalf("unfolded child block did not glide/fade in: dy=%v fade=%v", e.motion.blocks[bodyID].dy, e.motion.foldFade[bodyID])
	}
	tt.Advance(400 * time.Millisecond)
	if e.motion.blocks[bodyID].dy != 0 || e.motion.foldFade[bodyID] != 0 {
		t.Fatalf("unfold animation did not settle cleanly: dy=%v fade=%v", e.motion.blocks[bodyID].dy, e.motion.foldFade[bodyID])
	}

	// Clicking the task checkbox checks it and plays the spring/checkmark animation.
	var taskRow row
	for _, r := range e.rows {
		if r.leaf.Block.ID == taskID {
			taskRow = r
			break
		}
	}
	if !e.toggleTask(taskRow.leaf.Start, taskRow.x-16) {
		t.Fatal("toggleTask did not toggle checkbox")
	}
	if !e.Doc.Blocks[2].Checked {
		t.Fatal("task block not marked Checked")
	}
	if _, ok := e.taskAnim[taskID]; !ok {
		t.Fatal("task checkbox spring animation was not armed")
	}
	tt.Frame()
	tt.Advance(260 * time.Millisecond)
	tt.Frame()
	if _, ok := e.taskAnim[taskID]; ok {
		t.Fatal("task checkbox animation did not finish and clean up")
	}
}

func TestTableColumnDragReorderAndEqualWidthSnap(t *testing.T) {
	e, tt := newTester("| ColA | ColB | ColC |\n| --- | --- | --- |\n| 1 | 2 | 3 |")
	focus(e, tt)
	tblID := e.Doc.Blocks[0].ID
	l00, _ := e.Engine.LeafByID(e.Doc.Blocks[0].Children[0].Children[0].ID)
	e.SetSelection(l00.End, l00.End)
	tt.Frame()

	bounds, _ := tt.Find("文档")
	d, ok := e.tableDecoration(tblID)
	if !ok {
		t.Fatal("missing table decoration")
	}
	cb2 := columnBar(d, 2)
	cb0 := columnBar(d, 0)
	sx, sy := bounds.X+cb2.X+cb2.W/2, bounds.Y+cb2.Y+cb2.H/2
	tx := bounds.X + cb0.X + 8
	tt.Move(sx, sy)
	tt.Frame()
	tt.Press(sx, sy)
	tt.Move(tx, sy)
	tt.Frame()
	if !e.tableColDrag.active || e.tableColDrag.to != 0 {
		t.Fatalf("column drag did not target column 0: %+v", e.tableColDrag)
	}
	tt.Release(tx, sy)
	tt.Frame()

	hdr := e.Doc.Blocks[0].Children[0].Children
	if hdr[0].Text() != "ColC" || hdr[1].Text() != "ColA" || hdr[2].Text() != "ColB" {
		t.Fatalf("column drag reorder mismatch: %q, %q, %q", hdr[0].Text(), hdr[1].Text(), hdr[2].Text())
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	hdr = e.Doc.Blocks[0].Children[0].Children
	if hdr[0].Text() != "ColA" || hdr[1].Text() != "ColB" || hdr[2].Text() != "ColC" {
		t.Fatal("column drag reorder did not undo in a single step")
	}

	// Start with unequal 2-column widths and drag within 6 DIP of equal width:
	// magnetic snap commits exact 50% / 50% widths.
	e2, tt2 := newTester("| Left | Right |\n| --- | --- |\n| a | b |")
	focus(e2, tt2)
	id2 := e2.Doc.Blocks[0].ID
	e2.Engine.SetWidths(id2, []float32{200, 400})
	tt2.Frame()
	if len(e2.resizers) != 1 {
		t.Fatalf("expected 1 column resizer, got %d", len(e2.resizers))
	}
	res := e2.resizers[0]
	bounds2, _ := tt2.Find("文档")
	equalX := res.x + (res.widths[1]-res.widths[0])*0.5
	tt2.Press(bounds2.X+res.x, bounds2.Y+res.y+12)
	tt2.Move(bounds2.X+equalX+3, bounds2.Y+res.y+12)
	tt2.Frame()
	tt2.Release(bounds2.X+equalX+3, bounds2.Y+res.y+12)
	tt2.Frame()

	w := e2.Doc.Blocks[0].Widths
	if len(w) != 2 || abs32(w[0]-w[1]) > 0.01 {
		t.Fatalf("column resizer did not magnetically snap to equal widths: %v", w)
	}
}

func TestLivePrependBlockAndInlineMarkdown(t *testing.T) {
	e, tt := newTester("")
	focus(e, tt)

	// 1. Prepend "## " before existing text -> heading level 2.
	typeKeys(tt, "我要变二级标题")
	e.SetSelection(0, 0)
	tt.Frame()
	typeKeys(tt, "## ")
	if b := e.Doc.Blocks[0]; b.Kind != "heading" || b.Level != 2 || b.Text() != "我要变二级标题" || e.Caret != 0 {
		t.Fatalf("live prepend ## failed: kind=%s level=%d text=%q caret=%d", b.Kind, b.Level, b.Text(), e.Caret)
	}

	// 2. Prepend " > " before existing text -> quote.
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	typeKeys(tt, "我要变引用")
	l := e.Engine.LeafAt(e.Caret)
	e.SetSelection(l.Start, l.Start)
	tt.Frame()
	typeKeys(tt, " > ")
	if b := e.Doc.Blocks[1]; b.Kind != "quote" || b.Text() != "我要变引用" {
		t.Fatalf("live prepend > failed: kind=%s text=%q", b.Kind, b.Text())
	}

	// 3. Prepend "- [ ] " before existing text -> task.
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	// Empty quote block Enter drops quote style to a plain paragraph.
	tt.Key(0, ui.KeyEnter)
	typeKeys(tt, "我要变任务")
	l = e.Engine.LeafAt(e.Caret)
	e.SetSelection(l.Start, l.Start)
	tt.Frame()
	typeKeys(tt, "- [ ] ")
	if b := e.Doc.Blocks[2]; b.Kind != "task" || b.Checked || b.Text() != "我要变任务" {
		t.Fatalf("live prepend - [ ] failed: kind=%s checked=%v text=%q", b.Kind, b.Checked, b.Text())
	}

	// 4. Prepend opening "**" before text ending with "**文字" -> live bold + Backspace rollback.
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	// Empty task block Enter drops task style to a plain paragraph.
	tt.Key(0, ui.KeyEnter)
	typeKeys(tt, "这是一段重点**文字")
	l = e.Engine.LeafAt(e.Caret)
	beforeBold := l.Start + len([]rune("这是一段"))
	e.SetSelection(beforeBold, beforeBold)
	tt.Frame()
	typeKeys(tt, "**")
	b := e.Doc.Blocks[3]
	if b.Text() != "这是一段重点文字" || len(b.Runs) != 3 || !b.Runs[1].Style.Bold || b.Runs[1].Text != "重点" {
		t.Fatalf("live prepend ** failed: text=%q runs=%+v", b.Text(), b.Runs)
	}
	tt.Key(0, ui.KeyBackspace)
	if e.Doc.Blocks[3].Text() != "这是一段**重点**文字" {
		t.Fatalf("backspace did not roll back reverse inline bold: %q", e.Doc.Blocks[3].Text())
	}
}

func TestReview9FixesRegression(t *testing.T) {
	// 1. [P1] Columns containing a centered quote followed by a normal paragraph
	// must round-trip without shattering the columns block.
	quote := document.NewBlock("quote", "居中引用")
	quote.Align = "center"
	colsDoc := &document.Document{
		Blocks: []*document.Block{
			{
				ID:   document.NewID(),
				Kind: "columns",
				Children: []*document.Block{
					{
						ID:       document.NewID(),
						Kind:     "column",
						Children: []*document.Block{quote, document.NewBlock("paragraph", "普通段落")},
					},
					{
						ID:       document.NewID(),
						Kind:     "column",
						Children: []*document.Block{document.NewBlock("paragraph", "第二列")},
					},
				},
			},
		},
	}
	colsMD := ExportMarkdown(colsDoc)
	reCols := ImportMarkdown(colsMD)
	if len(reCols.Blocks) != 1 || reCols.Blocks[0].Kind != "columns" || len(reCols.Blocks[0].Children) != 2 {
		t.Fatalf("columns + centered quote shattered on reopen:\nmd:\n%s\nblocks: %+v", colsMD, reCols.Blocks)
	}
	col0 := reCols.Blocks[0].Children[0].Children
	if len(col0) != 2 || col0[0].Kind != "quote" || col0[0].Align != "center" || col0[0].Text() != "居中引用" ||
		col0[1].Kind != "paragraph" || col0[1].Align != "" || col0[1].Text() != "普通段落" {
		t.Fatalf("columns col0 content corrupted: %+v", col0)
	}

	// 2. [P1] Centered README image badge (<a><img></a>) must stay lossless raw HTML
	// instead of turning into an empty paragraph and dropping href/src on edit.
	badges := []string{
		"<div align=\"center\">\n  <a href=\"https://example.com/ci\"><img src=\"https://example.com/badge.svg\" alt=\"CI\"></a>\n</div>\n",
		"<div align=\"center\"><a href=\"https://example.com/ci\"><img src=\"https://example.com/badge.svg\" alt=\"CI\"></a></div>\n",
		"<p align=\"center\"><a href=\"https://example.com/ci\"><img src=\"https://example.com/badge.svg\" alt=\"CI\"></a></p>\n",
	}
	for _, badgeMD := range badges {
		bdoc := ImportMarkdown(badgeMD)
		if len(bdoc.Blocks) != 1 || bdoc.Blocks[0].Kind != "raw" ||
			!strings.Contains(bdoc.Blocks[0].Text(), "https://example.com/ci") ||
			!strings.Contains(bdoc.Blocks[0].Text(), "https://example.com/badge.svg") {
			t.Fatalf("centered badge lost raw HTML or URLs for %q: %+v", badgeMD, bdoc.Blocks)
		}
	}

	// 3. [P1] Editing visible text in a paragraph with an inline HTML comment
	// must preserve the comment in Markdown export while keeping it hidden in HTML export.
	e, tt := newTester("Before <!-- keep me --> after\n")
	focus(e, tt)
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	tt.Type("!")
	if got := ExportMarkdown(e.Doc); got != "Before <!-- keep me --> after!\n" {
		t.Fatalf("editing paragraph dropped inline comment: got %q", got)
	}
	if htmlOut := ExportHTML(e.Doc); strings.Contains(htmlOut, "keep me") {
		t.Fatalf("ExportHTML leaked inline comment: %s", htmlOut)
	}
	encoded, err := e.Doc.Encode()
	if err != nil {
		t.Fatalf("Encode with inline comments failed: %v", err)
	}
	decoded, err := document.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode with inline comments failed: %v", err)
	}
	if got := ExportMarkdown(decoded); got != "Before <!-- keep me --> after!\n" {
		t.Fatalf("Decode round-trip lost inline comment: got %q", got)
	}
	// Undo keeps comment; inserting at start shifts comment offset cleanly.
	tt.Key(ui.Cmd, ui.KeyZ)
	e.SetSelection(0, 0)
	tt.Frame()
	tt.Type("X")
	if got := ExportMarkdown(e.Doc); got != "XBefore <!-- keep me --> after\n" {
		t.Fatalf("prefix edit misaligned inline comment: got %q", got)
	}
	// Comment inside bold + centered paragraph also survives edit & reopen.
	e, tt = newTester("<p align=\"center\">Hi <!-- c --> there</p>\n")
	focus(e, tt)
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	tt.Type("!")
	if got := ExportMarkdown(e.Doc); !strings.Contains(got, "<!-- c -->") || !strings.Contains(got, "there!") {
		t.Fatalf("centered paragraph edit lost inline comment: %q", got)
	}

	// 4. [P2] Rich HTML table with custom column widths preserves exact spaces.
	tbl := document.Table(2, 2)
	tbl.Widths = []float32{180, 180}
	tbl.Children[0].Children[0].Runs = []document.Run{{Text: "H1"}}
	tbl.Children[0].Children[1].Runs = []document.Run{{Text: "H2"}}
	tbl.Children[1].Children[0].Runs = []document.Run{{Text: "  A  B  "}}
	tbl.Children[1].Children[1].Runs = []document.Run{{Text: "plain"}}
	tblDoc := &document.Document{Blocks: []*document.Block{tbl}}
	tblMD := ExportMarkdown(tblDoc)
	reTbl := ImportMarkdown(tblMD)
	if len(reTbl.Blocks) != 1 || reTbl.Blocks[0].Kind != "table" {
		t.Fatalf("rich table reopen failed: %+v", reTbl.Blocks)
	}
	if got := reTbl.Blocks[0].Children[1].Children[0].Text(); got != "  A  B  " {
		t.Fatalf("rich table collapsed cell spaces: got %q want %q\nmd:\n%s", got, "  A  B  ", tblMD)
	}

	// 5. [P2] Enter preserves active Bold on the new paragraph while still exiting Kbd.
	e, tt = newTester("")
	focus(e, tt)
	e.Engine.Toggle("bold")
	typeKeys(tt, "first")
	tt.Key(0, ui.KeyEnter)
	typeKeys(tt, "second")
	if len(e.Doc.Blocks) != 2 || !e.Doc.Blocks[0].Runs[0].Style.Bold ||
		!e.Doc.Blocks[1].Runs[0].Style.Bold || e.Doc.Blocks[1].Text() != "second" {
		t.Fatalf("Enter cleared active Bold style: %+v", e.Doc.Blocks)
	}
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<kbd>Alt</kbd>")
	tt.Key(0, ui.KeyEnter)
	tt.Type("<")
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[1].Runs[0].Style.Kbd {
		t.Fatalf("Enter after <kbd> leaked Kbd into next paragraph: %+v", e.Doc.Blocks)
	}

	// 6. [P2] Hand-typing HTML comment keeps tags and ellipsis literal inside unclosed comment.
	e, tt = newTester("")
	focus(e, tt)
	typeKeys(tt, "<!-- use <b>x</b> ... -->")
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Kind != "raw" || e.Doc.Blocks[0].Text() != "<!-- use <b>x</b> ... -->" {
		t.Fatalf("hand-typed comment mutated body: %+v", e.Doc.Blocks)
	}

	// 7. [P2] Second-line <kbd> keycap paints on line 2, not line 1.
	line2Doc := &document.Document{
		Blocks: []*document.Block{
			{
				ID:   document.NewID(),
				Kind: "paragraph",
				Runs: []document.Run{
					{Text: "X\u2028          "},
					{Text: "Ctrl", Style: document.Style{Kbd: true}},
				},
			},
		},
	}
	e2 := FromOwnedDocument(line2Doc)
	tt2 := ui.NewTester(func(c *ui.Context) { w, _ := c.Size(); View(c, e2, Options{Width: w, Theme: c.Theme()}) }, 640, 320)
	tt2.Frame()
	if len(e2.rows) == 0 || e2.rows[0].layout == nil {
		t.Fatal("line2 keycap row did not lay out")
	}
	r0 := e2.rows[0]
	kbdStart := document.Len("X\u2028          ")
	kbdEnd := kbdStart + document.Len("Ctrl")
	rects := r0.layout.SelectionRects(ui.TextInputRange{Start: kbdStart, End: kbdEnd})
	if len(rects) == 0 || rects[0].Y < 10 {
		t.Fatalf("expected Ctrl on line 2 (Y >= 10), got rects=%+v", rects)
	}
	img := tt2.Image()
	bg := img.At(img.Bounds().Max.X-4, img.Bounds().Max.Y-4)
	sampleX := int(r0.x + rects[0].X + rects[0].W/2)
	line1Top := int(r0.y)
	line1Bot := int(r0.y + rects[0].Y - 2)
	for y := line1Top; y <= line1Bot; y++ {
		if img.At(sampleX, y) != bg {
			t.Fatalf("phantom keycap pixels drawn on line 1 at (%d,%d)", sampleX, y)
		}
	}
	line2Top := int(r0.y + rects[0].Y)
	line2Bot := int(r0.y + rects[0].Y + rects[0].H + 2)
	foundLine2 := false
	for y := line2Top; y <= line2Bot; y++ {
		if img.At(sampleX, y) != bg {
			foundLine2 = true
			break
		}
	}
	if !foundLine2 {
		t.Fatalf("keycap pixels missing on line 2 at x=%d y=[%d..%d]", sampleX, line2Top, line2Bot)
	}

	// 8. [P2] Clipboard paste of a single centered HTML paragraph preserves Align on empty doc,
	// without re-aligning when pasted into the middle of an existing paragraph.
	e, tt = newTester("")
	focus(e, tt)
	e.services.WriteClipboardData(transfer.New(transfer.NewItem(
		transfer.Bytes(transfer.HTML, []byte(`<p style="text-align:center">居中</p>`)),
		transfer.Bytes(transfer.Text, []byte("居中")),
	)))
	tt.Key(ui.Cmd, ui.KeyV)
	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Text() != "居中" || e.Doc.Blocks[0].Align != "center" {
		t.Fatalf("clipboard paste of single centered paragraph lost align: %+v", e.Doc.Blocks)
	}
	e, tt = newTester("hello world")
	focus(e, tt)
	e.SetSelection(6, 6)
	tt.Frame()
	e.services.WriteClipboardData(transfer.New(transfer.NewItem(
		transfer.Bytes(transfer.HTML, []byte(`<p style="text-align:center">居中</p>`)),
		transfer.Bytes(transfer.Text, []byte("居中")),
	)))
	tt.Key(ui.Cmd, ui.KeyV)
	if e.Doc.Blocks[0].Align != "" {
		t.Fatalf("mid-paragraph paste should not overwrite existing alignment: %+v", e.Doc.Blocks[0])
	}

	// 9. [P2] Explicit align="left" child inside a centered container must stay left-aligned.
	leftCases := []*document.Document{
		ImportMarkdown("<div align=\"center\">\n  <h3>Title</h3>\n  <p align=\"left\">Body</p>\n</div>\n"),
		ImportMarkdown("<div align=\"center\">\n\n### Title\n\n<p align=\"left\">Body</p>\n\n</div>\n"),
		ImportHTML(`<div align="center"><h3>Title</h3><p align="left">Body</p></div>`),
	}
	for idx, d := range leftCases {
		if len(d.Blocks) != 2 || d.Blocks[0].Kind != "heading" || d.Blocks[0].Align != "center" ||
			d.Blocks[1].Kind != "paragraph" || d.Blocks[1].Align != "" || d.Blocks[1].Text() != "Body" {
			t.Fatalf("explicit align=left case %d overridden by centered parent: %+v", idx, d.Blocks)
		}
	}
}

func TestReviewCommentInvariantsRegression(t *testing.T) {
	// 1. [P1] A hidden inline comment that lands at the start of a line (offset 0,
	// after leading spaces, after a soft break, or in a quote/list/footnote) must
	// round-trip as a hidden comment and NEVER reopen as a visible raw block or
	// leak into ExportHTML.
	e, tt := newTester("Before <!-- review-marker --> after\n")
	focus(e, tt)
	// Delete "Before " so the hidden comment moves to offset 0 of the paragraph.
	e.SetSelection(0, document.Len("Before "))
	tt.Frame()
	tt.Key(0, ui.KeyBackspace)
	md := ExportMarkdown(e.Doc)
	re := ImportMarkdown(md)
	if len(re.Blocks) != 1 || re.Blocks[0].Kind != "paragraph" || re.Blocks[0].Text() != " after" ||
		len(re.Blocks[0].Comments) != 1 || re.Blocks[0].Comments[0].Offset != 0 || re.Blocks[0].Comments[0].Text != "<!-- review-marker -->" {
		t.Fatalf("line-start hidden comment corrupted on reopen:\nmd: %q\nkind=%q text=%q comments=%+v", md, re.Blocks[0].Kind, re.Blocks[0].Text(), re.Blocks[0].Comments)
	}
	if htmlOut := ExportHTML(re); strings.Contains(htmlOut, "review-marker") {
		t.Fatalf("line-start hidden comment leaked into ExportHTML after reopen: %s", htmlOut)
	}

	// Also verify soft break, quote, bullet, and empty-text paragraph with line-start comment.
	lineStartDocs := []struct {
		name  string
		block *document.Block
		text  string
	}{
		{
			name: "soft-break",
			block: &document.Block{
				ID:       document.NewID(),
				Kind:     "paragraph",
				Runs:     []document.Run{{Text: "Line1\u2028Line2"}},
				Comments: []document.InlineComment{{Offset: document.Len("Line1\u2028"), Text: "<!-- secret -->"}},
			},
			text: "Line1\u2028Line2",
		},
		{
			name: "quote",
			block: &document.Block{
				ID:       document.NewID(),
				Kind:     "quote",
				Runs:     []document.Run{{Text: "Quoted"}},
				Comments: []document.InlineComment{{Offset: 0, Text: "<!-- secret -->"}},
			},
			text: "Quoted",
		},
		{
			name: "bullet",
			block: &document.Block{
				ID:       document.NewID(),
				Kind:     "bullet",
				Runs:     []document.Run{{Text: "Item"}},
				Comments: []document.InlineComment{{Offset: 0, Text: "<!-- secret -->"}},
			},
			text: "Item",
		},
		{
			name: "leading-spaces",
			block: &document.Block{
				ID:       document.NewID(),
				Kind:     "paragraph",
				Runs:     []document.Run{{Text: "  Body"}},
				Comments: []document.InlineComment{{Offset: 2, Text: "<!-- secret -->"}},
			},
			text: "Body",
		},
	}
	for _, tc := range lineStartDocs {
		doc := &document.Document{Blocks: []*document.Block{tc.block}}
		out := ExportMarkdown(doc)
		reDoc := ImportMarkdown(out)
		if len(reDoc.Blocks) != 1 || reDoc.Blocks[0].Kind != tc.block.Kind || reDoc.Blocks[0].Text() != tc.text ||
			len(reDoc.Blocks[0].Comments) != 1 || reDoc.Blocks[0].Comments[0].Text != "<!-- secret -->" {
			t.Fatalf("%s line-start comment round-trip failed:\nmd: %q\nblocks: %+v", tc.name, out, reDoc.Blocks)
		}
		if htmlOut := ExportHTML(reDoc); strings.Contains(htmlOut, "secret") {
			t.Fatalf("%s leaked hidden comment into ExportHTML: %s", tc.name, htmlOut)
		}
	}

	// Empty paragraph holding only a hidden comment at offset 0 must not leak to ExportHTML either.
	emptyCommentDoc := &document.Document{
		Blocks: []*document.Block{{
			ID:       document.NewID(),
			Kind:     "paragraph",
			Runs:     []document.Run{{Text: ""}},
			Comments: []document.InlineComment{{Offset: 0, Text: "<!-- only-secret -->"}},
		}},
	}
	emptyMD := ExportMarkdown(emptyCommentDoc)
	emptyRe := ImportMarkdown(emptyMD)
	if len(emptyRe.Blocks) != 1 || emptyRe.Blocks[0].Kind != "paragraph" || emptyRe.Blocks[0].Text() != "" ||
		len(emptyRe.Blocks[0].Comments) != 1 || emptyRe.Blocks[0].Comments[0].Text != "<!-- only-secret -->" {
		t.Fatalf("empty-text comment block did not round-trip as hidden paragraph:\nmd: %q\nblocks: %+v", emptyMD, emptyRe.Blocks)
	}
	if htmlOut := ExportHTML(emptyRe); strings.Contains(htmlOut, "only-secret") {
		t.Fatalf("empty-text comment block leaked into ExportHTML:\nmd: %q\nblocks: %+v\nhtml: %s", emptyMD, emptyRe.Blocks, htmlOut)
	}

	// 2. [P1] Shortening a tag via MapProse (or wiki link via MapText) must shift
	// inline comment offsets so native .arcmd Validate/Encode still succeeds and
	// comments remain anchored in the right place.
	tagDoc := ImportMarkdown("#very-long-tag-name <!-- keep -->\n\n#very-long-tag-name <!-- mid --> #very-long-tag-name\n")
	eng := document.NewEngine(tagDoc)
	if !eng.MapProse(func(s string) string { return strings.ReplaceAll(s, "#very-long-tag-name", "#x") }) {
		t.Fatal("MapProse did not report change")
	}
	if err := eng.Doc.Validate(); err != nil {
		t.Fatalf("Validate failed after shortening tag via MapProse: %v", err)
	}
	if _, err := eng.Doc.Encode(); err != nil {
		t.Fatalf("Encode failed after shortening tag via MapProse: %v", err)
	}
	if got := ExportMarkdown(eng.Doc); got != "#x <!-- keep -->\n\n#x <!-- mid --> #x\n" {
		t.Fatalf("MapProse misaligned comments after tag shorten: got %q", got)
	}
	// MapText shortening a wiki link before a comment also keeps Validate/Encode healthy.
	linkDoc := ImportMarkdown("[[Very Long Linked Document]] <!-- note -->\n")
	eng2 := document.NewEngine(linkDoc)
	eng2.MapText(func(s string) string { return strings.ReplaceAll(s, "Very Long Linked Document", "A") })
	if err := eng2.Doc.Validate(); err != nil {
		t.Fatalf("Validate failed after MapText shorten: %v", err)
	}
	if got := ExportMarkdown(eng2.Doc); got != "[[A]] <!-- note -->\n" {
		t.Fatalf("MapText misaligned comment: got %q", got)
	}

	// 3. [P2] Pasting multi-block content right at the offset of a hidden comment
	// must NOT duplicate the comment onto both prefix and suffix blocks.
	e, tt = newTester("AB<!--paste-anchor-->CD\n")
	focus(e, tt)
	e.SetSelection(2, 2)
	tt.Frame()
	multi := ImportMarkdown("**Alpha**\n\n**Beta**\n")
	e.Engine.Paste(multi)
	e.sync()
	tt.Frame()
	pastedMD := ExportMarkdown(e.Doc)
	if count := strings.Count(pastedMD, "<!--paste-anchor-->"); count != 1 {
		t.Fatalf("multi-block paste at hidden comment duplicated/dropped comment (count=%d):\n%s", count, pastedMD)
	}
	rePasted := ImportMarkdown(pastedMD)
	if htmlOut := ExportHTML(rePasted); strings.Contains(htmlOut, "paste-anchor") {
		t.Fatalf("reopened multi-block paste leaked hidden comment into ExportHTML:\nmd:\n%s\nhtml:\n%s", pastedMD, htmlOut)
	}
	// Also test pasting multi-block content at offset 0 of a block with a leading comment (comment preserved once on suffix).
	e, tt = newTester("Before <!--lead-->CD\n")
	focus(e, tt)
	e.SetSelection(0, document.Len("Before "))
	tt.Frame()
	tt.Key(0, ui.KeyBackspace)
	e.SetSelection(0, 0)
	tt.Frame()
	e.Engine.Paste(ImportMarkdown("First\n\nSecond\n"))
	e.sync()
	leadMD := ExportMarkdown(e.Doc)
	if count := strings.Count(leadMD, "<!--lead-->"); count != 1 {
		t.Fatalf("multi-block paste at offset 0 lost/duplicated comment (count=%d):\n%s", count, leadMD)
	}
	if htmlOut := ExportHTML(ImportMarkdown(leadMD)); strings.Contains(htmlOut, "lead") {
		t.Fatalf("reopened offset-0 multi-block paste leaked comment:\nmd:\n%s\nhtml:\n%s", leadMD, htmlOut)
	}

	// 4. Audit checks: SetKind to code/math clears comments cleanly, MergeCells keeps
	// comments valid, and IME composition Committed().Digest() stays valid.
	e, tt = newTester("Hello <!-- c --> world\n")
	focus(e, tt)
	e.SetSelection(0, 0)
	tt.Frame()
	tt.Compose("测试输入", 4)
	if _, err := e.Committed().Digest(); err != nil {
		t.Fatalf("Committed().Digest() failed during IME composition on block with comment: %v", err)
	}
	tt.Type("测试输入")
	e.Engine.SetKind("code", 0)
	if err := e.Doc.Validate(); err != nil {
		t.Fatalf("SetKind(code) left invalid comments: %v", err)
	}
	tbl := document.Table(2, 2)
	tbl.Children[0].Children[0].Runs = []document.Run{{Text: "A"}}
	tbl.Children[0].Children[1].Runs = []document.Run{{Text: "B"}}
	tbl.Children[0].Children[1].Comments = []document.InlineComment{{Offset: 1, Text: "<!-- cell-c -->"}}
	tblDoc := document.New()
	tblDoc.Blocks = []*document.Block{tbl}
	tblEng := document.NewEngine(tblDoc)
	tblEng.MergeCells(tbl.ID, document.CellRange{Row0: 0, Col0: 0, Row1: 0, Col1: 1})
	if err := tblEng.Doc.Validate(); err != nil {
		t.Fatalf("MergeCells produced invalid comments: %v", err)
	}
	merged := tblEng.Doc.Blocks[0]
	if len(merged.Children[0].Children[0].Comments) != 1 || merged.Children[0].Children[0].Comments[0].Offset != 3 || len(merged.Children[0].Children[1].Comments) != 0 {
		t.Fatalf("MergeCells did not move comment cleanly to anchor cell: anchor=%+v covered=%+v", merged.Children[0].Children[0].Comments, merged.Children[0].Children[1].Comments)
	}

	// 5. Legacy / hand-written files with bare leading comment before prose
	// ("<!--paste-anchor-->CD") must import as a paragraph with hidden comment
	// and never leak into visible body or ExportHTML, while standalone comments
	// and trailing block/media HTML stay raw.
	legacy := ImportMarkdown("<!--paste-anchor-->CD\n\n> <!--quote-anchor-->Quoted\n\n<!-- standalone -->\n\n<!-- badge --><div align=\"center\"><a href=\"https://example.com\"><img src=\"https://example.com/b.svg\"></a></div>\n")
	if len(legacy.Blocks) != 4 {
		t.Fatalf("legacy leading-comment block count=%d: %+v", len(legacy.Blocks), legacy.Blocks)
	}
	if legacy.Blocks[0].Kind != "paragraph" || legacy.Blocks[0].Text() != "CD" ||
		len(legacy.Blocks[0].Comments) != 1 || legacy.Blocks[0].Comments[0].Text != "<!--paste-anchor-->" {
		t.Fatalf("legacy leading-comment paragraph failed: %+v", legacy.Blocks[0])
	}
	if legacy.Blocks[1].Kind != "quote" || legacy.Blocks[1].Text() != "Quoted" ||
		len(legacy.Blocks[1].Comments) != 1 || legacy.Blocks[1].Comments[0].Text != "<!--quote-anchor-->" {
		t.Fatalf("legacy leading-comment quote failed: %+v", legacy.Blocks[1])
	}
	if legacy.Blocks[2].Kind != "raw" || legacy.Blocks[2].Text() != "<!-- standalone -->" {
		t.Fatalf("standalone comment should stay raw: %+v", legacy.Blocks[2])
	}
	if legacy.Blocks[3].Kind != "raw" || !strings.Contains(legacy.Blocks[3].Text(), "https://example.com/b.svg") {
		t.Fatalf("leading comment + block/media HTML should stay raw: %+v", legacy.Blocks[3])
	}
	if htmlOut := ExportHTML(legacy); strings.Contains(htmlOut, "paste-anchor") || strings.Contains(htmlOut, "quote-anchor") || strings.Contains(htmlOut, "standalone") {
		t.Fatalf("legacy leading comments leaked into ExportHTML:\n%s", htmlOut)
	}
}

func TestPostMergePR28Remediation(t *testing.T) {
	// 1. <br> and Markdown hard line breaks ("  \n") in prose import as soft
	// line breaks ("\u2028") and do not split into separate paragraphs on Enter.
	for _, md := range []string{"Line1<br>Line2\n", "Line1  \nLine2\n"} {
		e, tt := newTester(md)
		focus(e, tt)
		e.SetSelection(e.Len(), e.Len())
		tt.Key(0, ui.KeyEnter)
		typeKeys(tt, "Line3")
		if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Text() != "Line1\u2028Line2" || e.Doc.Blocks[1].Text() != "Line3" {
			t.Fatalf("prose <br> split paragraph on Enter for %q: %+v", md, e.Doc.Blocks)
		}
	}

	// 2. Live typing inline HTML comments in prose and list items hides them in
	// Block.Comments with Backspace rollback and unescaped Markdown export.
	{
		e, tt := newTester("")
		focus(e, tt)
		typeKeys(tt, "Hello <!-- secret -->")
		if e.Doc.Blocks[0].Text() != "Hello " || len(e.Doc.Blocks[0].Comments) != 1 || e.Doc.Blocks[0].Comments[0].Text != "<!-- secret -->" {
			t.Fatalf("inline comment did not compile live: runs=%+v comments=%+v", e.Doc.Blocks[0].Runs, e.Doc.Blocks[0].Comments)
		}
		tt.Key(0, ui.KeyBackspace)
		if e.Doc.Blocks[0].Text() != "Hello <!-- secret -->" || len(e.Doc.Blocks[0].Comments) != 0 {
			t.Fatalf("Backspace did not roll back live inline comment: text=%q comments=%+v", e.Doc.Blocks[0].Text(), e.Doc.Blocks[0].Comments)
		}
		// Re-type in a bullet list item and verify Markdown export stays unescaped.
		e2, tt2 := newTester("- ")
		focus(e2, tt2)
		e2.SetSelection(e2.Len(), e2.Len())
		typeKeys(tt2, "Item <!-- list-note --> done")
		exported := ExportMarkdown(e2.Doc)
		if strings.Contains(exported, "&lt;!--") || !strings.Contains(exported, "Item <!-- list-note --> done") {
			t.Fatalf("list inline comment escaped on export: %q", exported)
		}
	}

	// 3. Semantic inline HTML tags (<var>, <dfn>, <cite>, <samp>, <tt>, <small>, <abbr>) compile live.
	for _, tc := range []struct {
		input  string
		want   string
		italic bool
		code   bool
	}{
		{"<var>x</var>", "x", true, false},
		{"<dfn>term</dfn>", "term", true, false},
		{"<cite>book</cite>", "book", true, false},
		{"<samp>out</samp>", "out", false, true},
		{"<tt>mono</tt>", "mono", false, true},
		{"<small>note</small>", "note", false, false},
		{`<abbr title="HyperText">HTML</abbr>`, "HTML", false, false},
	} {
		e, tt := newTester("")
		focus(e, tt)
		typeKeys(tt, tc.input)
		if len(e.Doc.Blocks[0].Runs) != 1 {
			t.Fatalf("live tag %s runs=%+v", tc.input, e.Doc.Blocks[0].Runs)
		}
		r := e.Doc.Blocks[0].Runs[0]
		if r.Text != tc.want || r.Style.Italic != tc.italic || r.Style.Code != tc.code {
			t.Fatalf("live tag %s got run=%+v", tc.input, r)
		}
		if md := ExportMarkdown(e.Doc); strings.Contains(md, "&lt;") {
			t.Fatalf("live tag %s escaped on export: %q", tc.input, md)
		}
	}

	// 4 & 5. Standalone HTML comment cards reserve header space above text and
	// stay excluded from tags, wiki links, word count, and mind map nodes.
	{
		e, tt := newTester("正文两字\n\n<!-- " + strings.Repeat("长单行注释内容 ", 12) + "#secret-tag [[SecretDoc]] -->\n")
		tt.Frame()
		if got := e.wordCount(); got != 4 {
			t.Fatalf("standalone HTML comment counted toward words: got %d, want 4", got)
		}
		if tags := wiki.Tags(e.Doc.PlainText()); len(tags) != 0 {
			t.Fatalf("standalone HTML comment leaked tags: %+v", tags)
		}
		if links := wiki.Links(e.Doc.PlainText()); len(links) != 0 {
			t.Fatalf("standalone HTML comment leaked wiki links: %+v", links)
		}
		if !shouldSkipMapBlock(e.Doc.Blocks[1]) {
			t.Fatal("standalone HTML comment block should be skipped in mind map")
		}
		if len(e.rows) < 2 || len(e.decorations) == 0 {
			t.Fatalf("expected laid out comment row and decoration: rows=%d decs=%d", len(e.rows), len(e.decorations))
		}
		if gap := e.rows[1].y - e.decorations[0].rect.Y; gap < 32 {
			t.Fatalf("standalone HTML comment text lacks top clearance for badge: gap=%v", gap)
		}
	}

	// 6. Edge cases: <kbd>`Ctrl`</kbd> mutual exclusion, <div align="center"> skipping
	// code/math/rule, MindMap truncateMapRuns with Style.Kbd '$', and overlapping box+kbd padding.
	{
		kbdCode := ImportMarkdown("<kbd>`Ctrl`</kbd>\n")
		if r := kbdCode.Blocks[0].Runs[0]; !r.Style.Kbd || r.Style.Code {
			t.Fatalf("<kbd>`Ctrl`</kbd> should be Kbd without Code: %+v", r)
		}
		aligned := ImportMarkdown("<div align=\"center\">\n\n# Title\n\n```go\nfmt.Println(1)\n```\n\n$$\nx=1\n$$\n\n</div>\n")
		if len(aligned.Blocks) != 3 || aligned.Blocks[0].Align != "center" || aligned.Blocks[1].Align != "" || aligned.Blocks[2].Align != "" {
			t.Fatalf("code/math inside <div align=center> should remain unaligned: %+v", aligned.Blocks)
		}
		runs := []document.Run{
			{Text: "按 "},
			{Text: "$", Style: document.Style{Kbd: true}},
			{Text: " 触发公式，" + strings.Repeat("后文内容", 85)},
		}
		kept, clipped := truncateMapRuns(runs, mapMaxProseRunes, mapMaxProseLines)
		if !clipped || len(kept) < 3 || !strings.Contains(kept[2].Text, "后文内容") {
			t.Fatalf("truncateMapRuns truncated at <kbd>$</kbd>: %+v", kept)
		}
		both := []document.Run{{Text: "Ctrl", Style: document.Style{Kbd: true, Annotation: "box"}}}
		if pad := trailingPadAt(both, 4, 16); pad != 0.4*16 {
			t.Fatalf("overlapping box+kbd trailingPadAt got %v, want %v", pad, 0.4*16)
		}
	}
}
