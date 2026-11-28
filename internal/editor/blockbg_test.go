package editor

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/egoist/mygo/ui"
)

func TestBlockBackgroundMarkdownRoundTrip(t *testing.T) {
	h2 := document.NewBlock("heading", "项目目标")
	h2.Level = 2
	h2.Background = "blue"

	plain := document.NewBlock("paragraph", "无背景色的普通段落")

	para := document.NewBlock("paragraph", "重点段落内容")
	para.Align = "center"
	para.Indent = 1
	para.Background = "light-yellow"

	b0 := document.NewBlock("bullet", "顶层列表项")
	b0.Background = "green"

	b1 := document.NewBlock("bullet", "二级缩进子列表项")
	b1.Indent = 2
	b1.Background = "light-green"

	n0 := document.NewBlock("numbered", "有序第一步")
	n0.Start = 3
	n0.Background = "orange"

	task := document.NewBlock("task", "已完成任务")
	task.Checked = true
	task.Background = "light-blue"

	quote := document.NewBlock("quote", "带背景的引用")
	quote.Background = "purple"

	toggle := document.NewBlock("toggle", "折叠块标题")
	toggle.Background = "light-orange"
	toggleChild := document.NewBlock("paragraph", "折叠体内子块不受背景影响")
	toggleChild.Indent = 1

	cols := document.Columns()
	cols.Children[0].Children[0].Runs = []document.Run{{Text: "左栏段落"}}
	cols.Children[0].Children[0].Background = "red"
	cols.Children[1].Children[0].Runs = []document.Run{{Text: "右栏列表"}}
	cols.Children[1].Children[0].Kind = "bullet"
	cols.Children[1].Children[0].Background = "light-blue"

	fn := document.NewBlock("footnote", "带底色的脚注")
	fn.NoteLabel = "n1"
	fn.Background = "gray"

	doc := document.New()
	doc.Blocks = []*document.Block{h2, plain, para, b0, b1, n0, task, quote, toggle, toggleChild, cols, fn}

	md := ExportMarkdown(doc)
	for _, want := range []string{
		`<h2 style="background-color:#E1EAFF" data-bg="blue">项目目标</h2>`,
		"\n无背景色的普通段落\n",
		`<p align="center" style="margin-left:24px;background-color:#FEFFF0" data-indent="1" data-bg="light-yellow">重点段落内容</p>`,
		`<div style="background-color:#D9F5D6" data-bg="green">`,
		`<div style="background-color:#F0FBEF" data-bg="light-green" data-indent="2">`,
		`<div style="background-color:#ECE2FE" data-bg="purple">`,
		`<details open style="background-color:#FFF5EB" data-bg="light-orange">`,
		`<p style="background-color:#FDE2E2" data-bg="red">左栏段落</p>`,
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("ExportMarkdown missing %q:\n%s", want, md)
		}
	}

	got := ImportMarkdown(md)
	if len(got.Blocks) != len(doc.Blocks) {
		t.Fatalf("round-trip block count = %d, want %d\nMD:\n%s", len(got.Blocks), len(doc.Blocks), md)
	}
	for i := range doc.Blocks {
		a, b := doc.Blocks[i], got.Blocks[i]
		if a.Kind != b.Kind || a.Background != b.Background || a.Indent != b.Indent || a.Align != b.Align || a.Text() != b.Text() {
			t.Fatalf("block %d mismatch:\n got: kind=%q bg=%q indent=%d align=%q text=%q\nwant: kind=%q bg=%q indent=%d align=%q text=%q",
				i, b.Kind, b.Background, b.Indent, b.Align, b.Text(), a.Kind, a.Background, a.Indent, a.Align, a.Text())
		}
	}
	if got.Blocks[9].Background != "" {
		t.Fatalf("toggle child inherited toggle background: %q", got.Blocks[9].Background)
	}
	if got.Blocks[10].Children[0].Children[0].Background != "red" || got.Blocks[10].Children[1].Children[0].Background != "light-blue" {
		t.Fatalf("column child backgrounds lost: left=%q right=%q",
			got.Blocks[10].Children[0].Children[0].Background, got.Blocks[10].Children[1].Children[0].Background)
	}
	if got.Blocks[5].Start != 3 {
		t.Fatalf("numbered block Start = %d, want 3", got.Blocks[5].Start)
	}

	// Partial edit preserves untouched source blocks verbatim (including untinted Markdown).
	src := "# Keep *Exact* Title\n\n<p style=\"background-color:#E1EAFF\" data-bg=\"blue\">Styled</p>\n\nPlain _untouched_ line\n"
	ed := New(src)
	if ed.Doc.Blocks[1].Background != "blue" {
		t.Fatalf("imported background = %q, want blue", ed.Doc.Blocks[1].Background)
	}
	ed.Engine.SetBlockBackground(ed.Doc.Blocks[1].ID, "green")
	out := ExportMarkdown(ed.Doc)
	if !strings.Contains(out, "# Keep *Exact* Title") || !strings.Contains(out, "Plain _untouched_ line") || !strings.Contains(out, `data-bg="green"`) {
		t.Fatalf("partial edit did not preserve untouched lines:\n%s", out)
	}
}

func TestBlockBackgroundBlockMenuAndMultiBlock(t *testing.T) {
	e, tt := newTester("第一段\n\n第二段\n\n```go\ncode\n```\n\n> [!NOTE]\n> tip")
	p1ID := e.Doc.Blocks[0].ID
	codeID := e.Doc.Blocks[2].ID
	calloutID := e.Doc.Blocks[3].ID

	// Single paragraph menu exposes 背景颜色 and side swatch grid sets & clears background.
	e.OpenBlockMenu(p1ID, "bg")
	tt.Frame()
	if !tt.HasText("背景颜色") {
		t.Fatal("paragraph block menu missing 背景颜色")
	}
	if err := tt.Click("块背景：深蓝"); err != nil {
		t.Fatalf("click swatch failed: %v", err)
	}
	if e.Doc.Blocks[0].Background != "blue" || e.menuOpen {
		t.Fatalf("after clicking swatch: bg=%q menuOpen=%v", e.Doc.Blocks[0].Background, e.menuOpen)
	}

	// Clearing via 无背景颜色 swatch.
	e.OpenBlockMenu(p1ID, "bg")
	tt.Frame()
	if err := tt.Click("无背景颜色"); err != nil {
		t.Fatalf("click clear swatch failed: %v", err)
	}
	if e.Doc.Blocks[0].Background != "" {
		t.Fatalf("expected empty background after clearing, got %q", e.Doc.Blocks[0].Background)
	}

	// Code block and callout do NOT show 背景颜色.
	for _, id := range []string{codeID, calloutID} {
		e.OpenBlockMenu(id, "")
		tt.Frame()
		if tt.HasText("背景颜色") {
			t.Fatalf("unsupported block %q should not offer 背景颜色", id)
		}
		tt.Key(0, ui.KeyEscape)
	}

	// Multi-block selection across paragraphs + code skips code, sets paragraphs, and undoes in one step.
	e.SelectBlocks(p1ID, codeID)
	e.OpenBlockMenu(p1ID, "bg")
	tt.Frame()
	if !tt.HasText("缩进") || !tt.HasText("背景颜色") {
		t.Fatalf("blocks menu missing 缩进 or 背景颜色: %v", tt.Texts())
	}
	if err := tt.Click("块背景：深绿"); err != nil {
		t.Fatalf("click multi-block swatch failed: %v", err)
	}
	if e.Doc.Blocks[0].Background != "green" || e.Doc.Blocks[1].Background != "green" || e.Doc.Blocks[2].Background != "" {
		t.Fatalf("multi-block bg mismatch: p1=%q p2=%q code=%q", e.Doc.Blocks[0].Background, e.Doc.Blocks[1].Background, e.Doc.Blocks[2].Background)
	}
	e.Undo()
	if e.Doc.Blocks[0].Background != "" || e.Doc.Blocks[1].Background != "" {
		t.Fatalf("single undo did not clear multi-block bg: p1=%q p2=%q", e.Doc.Blocks[0].Background, e.Doc.Blocks[1].Background)
	}

	// ReadOnly blocks SetBlockBackground.
	e.SetReadOnly(true)
	e.SetBlockBackground("yellow")
	if e.Doc.Blocks[0].Background != "" || e.Doc.Blocks[1].Background != "" {
		t.Fatalf("ReadOnly mutated background: p1=%q p2=%q", e.Doc.Blocks[0].Background, e.Doc.Blocks[1].Background)
	}
}

func TestBlockBackgroundLayoutAndIncrementalRelayout(t *testing.T) {
	e, tt := newTester("# Title\n\n## Section\n\nShort text\n\n> Quoted line\n\nFollowing tinted")
	h2ID := e.Doc.Blocks[1].ID
	pID := e.Doc.Blocks[2].ID
	qID := e.Doc.Blocks[3].ID
	fID := e.Doc.Blocks[4].ID

	e.Engine.SetBlockBackground(h2ID, "blue")
	e.Engine.SetBlockBackground(pID, "light-yellow")
	e.Engine.SetBlockBackground(qID, "purple")
	e.Engine.SetBlockBackground(fID, "green")
	tt.Frame()

	findDec := func(id string) (decoration, int) {
		count := 0
		var found decoration
		for i := range e.decorations {
			d := *e.indexedDecoration(i)
			if d.id == id {
				found = d
				count++
			}
		}
		return found, count
	}

	// Heading background skips top margin and stays strictly >= row.rect.Y.
	hDec, hCount := findDec(h2ID)
	if hCount != 1 || hDec.kind != "block-bg" || hDec.bg != "blue" {
		t.Fatalf("heading dec=%+v count=%d", hDec, hCount)
	}
	hRow := e.indexedRow(e.rowIDs[h2ID])
	if hDec.rect.Y <= hRow.rect.Y {
		t.Fatalf("heading bg rect.Y (%g) should skip top margin above row.rect.Y (%g)", hDec.rect.Y, hRow.rect.Y)
	}

	// Quote has exactly ONE decoration carrying both bar geometry and background.
	qDec, qCount := findDec(qID)
	if qCount != 1 || qDec.kind != "quote" || qDec.bg != "purple" {
		t.Fatalf("quote should have 1 combined decoration, got count=%d dec=%+v", qCount, qDec)
	}

	// Typing enough text into tinted paragraph triggers incremental relayout:
	// paragraph bg height grows AND following quote/block bg rects shift down by the same delta.
	pDecBefore, _ := findDec(pID)
	fDecBefore, _ := findDec(fID)
	leaf, _ := e.Engine.LeafByID(pID)
	e.SetSelection(leaf.End, leaf.End)
	long := strings.Repeat(" wrapping text across multiple lines", 12)
	e.Engine.Replace(leaf.End, leaf.End, long)
	e.sync()
	tt.Frame()
	tt.Advance(20 * time.Millisecond)

	pDecAfter, _ := findDec(pID)
	fDecAfter, _ := findDec(fID)
	dy := pDecAfter.rect.H - pDecBefore.rect.H
	if dy <= 10 {
		t.Fatalf("expected paragraph bg decoration height to grow after wrapping, before=%g after=%g", pDecBefore.rect.H, pDecAfter.rect.H)
	}
	shift := fDecAfter.rect.Y - fDecBefore.rect.Y
	if abs32(shift-dy) > 1 {
		t.Fatalf("following block bg did not shift by dy: dy=%g shift=%g", dy, shift)
	}
}

func TestBlockBackgroundHTMLClipboardAndVectorPDF(t *testing.T) {
	p := document.NewBlock("paragraph", "高亮背景段落")
	p.Background = "light-blue"
	q := document.NewBlock("quote", "带底色引用")
	q.Background = "purple"
	li := document.NewBlock("bullet", "列表项")
	li.Background = "green"
	tg := document.NewBlock("toggle", "折叠项")
	tg.Background = "orange"

	doc := document.New()
	doc.Blocks = []*document.Block{p, q, li, tg}

	if !rich(&document.Document{Format: "arcmd", Version: document.CurrentVersion, Blocks: []*document.Block{p}}) {
		t.Fatal("tinted paragraph must be considered rich for clipboard export")
	}

	// Clipboard HTML writes inline background-color + data-bg and round-trips via ImportHTML without leaking Highlight onto runs.
	clipHTML := ExportHTML(doc)
	for _, want := range []string{
		`style="background-color:#F0F4FF"`,
		`data-bg="light-blue"`,
		`<blockquote data-bg="purple" style="background-color:#ECE2FE">`,
		`<li style="background-color:#D9F5D6" data-bg="green">`,
		`<details open style="background-color:#FEEAD2" data-bg="orange">`,
	} {
		if !strings.Contains(clipHTML, want) {
			t.Fatalf("ExportHTML missing %q:\n%s", want, clipHTML)
		}
	}

	imported := ImportHTML(clipHTML)
	if len(imported.Blocks) < 3 {
		t.Fatalf("ImportHTML blocks=%d, want >=3", len(imported.Blocks))
	}
	if imported.Blocks[0].Background != "light-blue" || imported.Blocks[1].Background != "purple" || imported.Blocks[2].Background != "green" {
		t.Fatalf("ImportHTML lost block backgrounds: %q, %q, %q",
			imported.Blocks[0].Background, imported.Blocks[1].Background, imported.Blocks[2].Background)
	}
	for _, b := range imported.Blocks[:3] {
		for _, r := range b.Runs {
			if r.Style.Highlight != "" {
				t.Fatalf("block background leaked into inline run Highlight %q on %q", r.Style.Highlight, b.Kind)
			}
		}
	}

	// ExportHTMLPage uses adaptive classes (.arcmd-bg / .arcmd-bg-*).
	htmlTheme := testHTMLTheme()
	pageBody := ExportHTMLPage(doc, htmlTheme)
	for _, want := range []string{
		`class="arcmd-bg arcmd-bg-light-blue" data-bg="light-blue"`,
		`class="arcmd-bg arcmd-bg-purple" data-bg="purple"`,
		`class="arcmd-bg arcmd-bg-green" data-bg="green"`,
		`class="arcmd-bg arcmd-bg-orange" data-bg="orange"`,
	} {
		if !strings.Contains(pageBody, want) {
			t.Fatalf("ExportHTMLPage missing %q:\n%s", want, pageBody)
		}
	}
	css := BlockBackgroundCSS(pageOf(Page{}, ui.LightTheme()).CellFills)
	if !strings.Contains(css, ".arcmd-bg-light-blue { background-color:") {
		t.Fatalf("BlockBackgroundCSS missing rule:\n%s", css)
	}

	// Vector PDF export renders tinted blocks without error and emits extra vector fill ops.
	renderPDF := func(d *document.Document) []byte {
		ed := New(ExportMarkdown(d))
		opts := Options{Width: 794, Theme: ui.LightTheme()}
		ed.layout(opts)
		var pdfBuf bytes.Buffer
		pw := pdf.NewWriter(&pdfBuf, "blockbg")
		vp := pw.NewVectorPage(595, 842)
		ed.ExportVectorPage(vp, opts, 0, 1000, 0, 1, []float32{0}, 794, 1123, 48)
		if err := pw.FinishVectorPage(vp); err != nil {
			t.Fatalf("FinishVectorPage: %v", err)
		}
		if err := pw.Close(); err != nil {
			t.Fatalf("pdf Close: %v", err)
		}
		return pdfBuf.Bytes()
	}
	tintedPDF := renderPDF(doc)
	plainDoc := doc.Clone()
	for _, b := range plainDoc.Blocks {
		b.Background = ""
	}
	plainPDF := renderPDF(plainDoc)
	if !bytes.HasPrefix(tintedPDF, []byte("%PDF-")) || len(tintedPDF) <= len(plainPDF) {
		t.Fatalf("expected tinted vector PDF (%d bytes) to exceed plain PDF (%d bytes)", len(tintedPDF), len(plainPDF))
	}
}
