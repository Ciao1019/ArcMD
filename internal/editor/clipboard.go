package editor

import (
	"slices"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
)

const fragmentFormat transfer.Format = "application/vnd.arcmd.document+json"

func (e *Editor) copySelection(write func(string)) {
	r := e.Selection().Range
	if r.Start == r.End {
		return
	}
	text := strings.ReplaceAll(e.slice(r.Start, r.End), "\u2028", "\n")
	fragment := e.Engine.Fragment()
	fragment.ImportSource = ""
	data, err := fragment.Encode()
	if err != nil {
		write(text)
		return
	}
	text = e.clipboardPlainText(r, fragment, text)
	e.writeClipboard(fragment, data, text)
}

func (e *Editor) copyPlainSelection(write func(string)) {
	r := e.Selection().Range
	if r.Start == r.End {
		return
	}
	slice := strings.ReplaceAll(e.slice(r.Start, r.End), "\u2028", "\n")
	fragment := e.Engine.Fragment()
	fragment.ImportSource = ""
	e.writePlainClipboard(e.selectionPlainText(r, fragment, slice), write)
}

func runsPlainText(runs []document.Run) string {
	return strings.ReplaceAll(document.RunsVisibleText(runs), "\u2028", "\n")
}

func (e *Editor) slicePlainText(r ui.TextInputRange, a document.Leaf) string {
	sub := document.SliceRuns(a.Block.Runs, max(0, r.Start-a.Start), max(0, min(a.End, r.End)-a.Start))
	return runsPlainText(sub)
}

func (e *Editor) clipboardPlainText(r ui.TextInputRange, fragment *document.Document, slice string) string {
	if e.copyPlainText {
		return e.selectionPlainText(r, fragment, slice)
	}
	a, z := e.LeafAt(r.Start), e.LeafAt(r.End)
	if literalBlock(a.Block.Kind) {
		return slice
	}
	// Copying inside a single table cell or inline code/keycap span puts the
	// literal characters on the plain-text pasteboard so passwords, identifiers,
	// and commands paste into chat apps, terminals, and login inputs without
	// Markdown backslash escapes or wrapper markup.
	if a.Block == z.Block && (a.Block.Kind == "cell" || inlineLiteralRuns(a.Block, r.Start-a.Start, r.End-a.Start)) {
		return e.slicePlainText(r, a)
	}
	if tableID, cr, ok := e.cellRange(); ok && !(len(fragment.Blocks) == 1 && fragment.Blocks[0].Kind == "table") {
		if table := e.blockByID(tableID); table != nil {
			return exportCellRangePlainText(table, cr)
		}
	}
	if !rich(fragment) {
		if a.Block == z.Block {
			return e.slicePlainText(r, a)
		}
		return plainFragmentText(fragment)
	}
	return trimNewline(ExportMarkdown(fragment))
}

func (e *Editor) selectionPlainText(r ui.TextInputRange, fragment *document.Document, slice string) string {
	a, z := e.LeafAt(r.Start), e.LeafAt(r.End)
	if a.Block == z.Block && a.Block.Kind != "rule" && !isAtomicVisualBlock(a.Block) {
		if literalBlock(a.Block.Kind) {
			return slice
		}
		return e.slicePlainText(r, a)
	}
	if tableID, cr, ok := e.cellRange(); ok && !(len(fragment.Blocks) == 1 && fragment.Blocks[0].Kind == "table") {
		if table := e.blockByID(tableID); table != nil {
			return exportCellRangePlainText(table, cr)
		}
	}
	return exportFragmentPlainText(fragment)
}

type plainTextItem struct {
	text   string
	isList bool
}

func exportFragmentPlainText(doc *document.Document) string {
	if doc == nil {
		return ""
	}
	var items []plainTextItem
	collectPlainTextBlocks(doc.Blocks, &items)
	var out strings.Builder
	for i, it := range items {
		if i > 0 {
			if items[i-1].isList && it.isList {
				out.WriteByte('\n')
			} else {
				out.WriteString("\n\n")
			}
		}
		out.WriteString(it.text)
	}
	return out.String()
}

func collectPlainTextBlocks(blocks []*document.Block, out *[]plainTextItem) {
	for _, b := range blocks {
		if b == nil || b.Kind == "rule" || isAtomicVisualBlock(b) {
			continue
		}
		switch b.Kind {
		case "table":
			if len(b.Children) == 0 {
				continue
			}
			maxCols := 0
			for _, row := range b.Children {
				if len(row.Children) > maxCols {
					maxCols = len(row.Children)
				}
			}
			if maxCols == 0 {
				continue
			}
			t := exportCellRangePlainText(b, document.CellRange{
				Row0: 0, Col0: 0,
				Row1: len(b.Children) - 1, Col1: maxCols - 1,
			})
			if t != "" {
				*out = append(*out, plainTextItem{text: t})
			}
		case "columns":
			for _, col := range b.Children {
				collectPlainTextBlocks(col.Children, out)
			}
		case "image":
			if alt := strings.ReplaceAll(b.Text(), "\u2028", "\n"); alt != "" {
				*out = append(*out, plainTextItem{text: alt})
			}
		default:
			isList := b.Kind == "bullet" || b.Kind == "numbered" || b.Kind == "task"
			*out = append(*out, plainTextItem{
				text:   runsPlainText(b.Runs),
				isList: isList,
			})
		}
	}
}

func (e *Editor) writePlainClipboard(text string, write func(string)) {
	if e.services != (ui.Services{}) {
		e.services.WriteClipboardData(transfer.New(transfer.NewItem(
			transfer.Bytes(transfer.Text, []byte(text)),
		)))
		return
	}
	if write != nil {
		write(text)
	}
}

func inlineLiteralRuns(b *document.Block, from, to int) bool {
	runs := document.SliceRuns(b.Runs, max(0, from), max(0, to))
	if len(runs) == 0 {
		return false
	}
	for _, r := range runs {
		if !r.Style.Code && !r.Style.Kbd {
			return false
		}
	}
	return true
}

func plainFragmentText(doc *document.Document) string {
	parts := make([]string, 0, len(doc.Blocks))
	for _, b := range doc.Blocks {
		parts = append(parts, runsPlainText(b.Runs))
	}
	return strings.Join(parts, "\n\n")
}

func exportCellRangePlainText(table *document.Block, cr document.CellRange) string {
	covered := document.CoveredCells(table)
	var rows []string
	for ri := max(0, cr.Row0); ri <= min(cr.Row1, len(table.Children)-1); ri++ {
		row := table.Children[ri]
		var cols []string
		for ci := max(0, cr.Col0); ci <= min(cr.Col1, len(row.Children)-1); ci++ {
			cell := row.Children[ci]
			if covered[cell] {
				cols = append(cols, "")
				continue
			}
			cols = append(cols, runsPlainText(cell.Runs))
		}
		rows = append(rows, strings.Join(cols, "\t"))
	}
	return strings.Join(rows, "\n")
}

func trimNewline(s string) string { return strings.TrimSuffix(s, "\n") }

// writeClipboard offers a fragment, encoded as data, with its text,
// Markdown and HTML. When CopyPlainText is enabled, Markdown and HTML are
// omitted so external apps (including rich editors) paste plain text while
// ArcMD itself still restores formatting via fragmentFormat.
func (e *Editor) writeClipboard(fragment *document.Document, data, text string) {
	if e.copyPlainText {
		e.services.WriteClipboardData(transfer.New(transfer.NewItem(
			transfer.Bytes(fragmentFormat, []byte(data)),
			transfer.Bytes(transfer.Text, []byte(text)),
		)))
		return
	}
	e.services.WriteClipboardData(transfer.New(transfer.NewItem(
		transfer.Bytes(fragmentFormat, []byte(data)),
		transfer.Bytes(transfer.Text, []byte(text)),
		transfer.Bytes(transfer.Markdown, []byte(ExportMarkdown(fragment))),
		transfer.Bytes(transfer.HTML, []byte(ExportHTML(fragment))),
	)))
}

func (e *Editor) pasteSelection(read func() string) {
	if e.ReadOnly {
		return
	}
	kind := e.Engine.LeafAt(e.Caret).Block.Kind
	inLiteral := kind == "code" || kind == "raw" || kind == "math"
	offered := e.services.ClipboardFormats()
	hasFormat := func(f transfer.Format) bool {
		return len(offered) == 0 || slices.Contains(offered, f)
	}
	readFormat := func(f transfer.Format) ([]byte, bool) {
		if !hasFormat(f) {
			return nil, false
		}
		b, err := e.services.ReadClipboardData(f).Read(f)
		return b, err == nil && len(b) > 0
	}

	if data, ok := readFormat(fragmentFormat); ok && len(data) < 16<<20 {
		if doc, err := document.Decode(string(data)); err == nil {
			if inLiteral {
				e.Buffer.ReplaceText(nil, doc.VisiblePlainText())
				return
			}
			e.UnmarkText()
			e.Engine.Paste(doc)
			e.sync()
			return
		}
	}
	// Files copied in Finder go in as dropped at the caret.
	if e.dropFiles != nil && hasFormat(transfer.FileList) {
		if files, err := e.services.ReadClipboardData(transfer.FileList).Files(); err == nil && len(files) > 0 {
			e.UnmarkText()
			e.dropFiles(files, -1)
			return
		}
	}

	// Read structured text formats before deciding whether a clipboard PNG is
	// an actual picture (screenshot, browser "Copy Image", chat image, Excel
	// chart) or a fallback bitmap snapshot attached alongside spreadsheet or
	// office content (Excel, WPS, Numbers, LibreOffice Calc, Word).
	var htmlStr string
	htmlSubstantive := false
	if data, ok := readFormat(transfer.HTML); ok && len(data) < 8<<20 {
		htmlStr = string(data)
		htmlSubstantive = htmlHasNonImageContent(htmlStr)
	}
	mdBytes, hasMD := readFormat(transfer.Markdown)
	if hasMD && len(mdBytes) >= 8<<20 {
		mdBytes, hasMD = nil, false
	}
	text := read()
	hasPNG := e.pasteImage != nil && hasFormat(transfer.PNG)
	if hasPNG && !htmlSubstantive && !hasMD && !isSpreadsheetPlainText(text) {
		if png, ok := readFormat(transfer.PNG); ok {
			e.UnmarkText()
			e.pasteImage(png)
			return
		}
	}
	if kind != "code" && kind != "raw" && kind != "cell" && !e.Engine.CurrentStyle().Code && !e.Engine.CurrentStyle().Kbd {
		if hasMD {
			text = string(mdBytes)
		}
	}
	// A complete display formula copied from a chat or Markdown document
	// is math even when the clipboard also offers a styled HTML code block.
	// A formula editor accepts the body alone; other literal contexts keep
	// the text, including its delimiters.
	if !inLiteral && kind != "cell" && !e.Engine.CurrentStyle().Code && !e.Engine.CurrentStyle().Kbd {
		if doc := pastedMath(text); doc != nil {
			e.UnmarkText()
			e.Engine.Paste(doc)
			e.sync()
			return
		}
	} else if kind == "math" && !e.Engine.CurrentStyle().Code {
		if doc := pastedMath(text); doc != nil && len(doc.Blocks) == 1 && doc.Blocks[0].Kind == "math" {
			e.ReplaceText(nil, doc.Blocks[0].Text())
			return
		}
	}
	// Rich text from a browser, spreadsheet or another editor keeps its
	// structure; HTML that holds only plain text pastes as the text. Inside a
	// code block, if plain text lost leading indentation, recover it from HTML.
	if htmlStr != "" {
		doc := ImportHTML(htmlStr)
		if !inLiteral {
			if singleCell, ok := unwrapSingleCellTable(doc); ok {
				e.UnmarkText()
				e.Engine.Paste(singleCell)
				e.sync()
				return
			}
			if rich(doc) {
				e.UnmarkText()
				e.Engine.Paste(doc)
				e.sync()
				return
			}
		} else if kind == "code" && strings.ContainsRune(text, '\n') && !hasIndentedLine(text) {
			if htmlText := doc.PlainText(); hasIndentedLine(htmlText) && sameTrimmedLines(text, htmlText) {
				if strings.HasSuffix(normalizeLineBreaks(text), "\n") && !strings.HasSuffix(htmlText, "\n") {
					text = htmlText + "\n"
				} else {
					text = htmlText
				}
			}
		}
	}
	if !literalBlock(kind) && kind != "cell" && !e.CurrentStyle().Code && !e.CurrentStyle().Kbd && len(text) < 8<<20 {
		// Rows copied from a spreadsheet, as tab-separated lines.
		if doc := tabTable(text); doc != nil {
			e.UnmarkText()
			e.Engine.Paste(doc)
			e.sync()
			return
		}
		if hasPNG {
			if doc := singleRowTabTable(text); doc != nil {
				e.UnmarkText()
				e.Engine.Paste(doc)
				e.sync()
				return
			}
		}
		if doc := ImportMarkdown(text); rich(doc) {
			e.UnmarkText()
			e.Engine.Paste(doc)
			e.sync()
			return
		}
	}
	e.Buffer.ReplaceText(nil, text)
}

func isSpreadsheetPlainText(text string) bool {
	return tabTable(text) != nil || singleRowTabTable(text) != nil
}

// unwrapSingleCellTable turns a pasted 1x1 HTML table (such as a single cell
// copied from Excel) into a paragraph with that cell's runs so pasting a single
// value into prose or another cell does not insert a 1x1 table or stray newline.
func unwrapSingleCellTable(doc *document.Document) (*document.Document, bool) {
	if doc == nil || len(doc.Blocks) != 1 || doc.Blocks[0].Kind != "table" {
		return nil, false
	}
	t := doc.Blocks[0]
	if len(t.Children) != 1 || len(t.Children[0].Children) != 1 {
		return nil, false
	}
	cell := t.Children[0].Children[0]
	p := document.NewBlock("paragraph", "")
	if len(cell.Runs) > 0 {
		p.Runs = document.Compact(cell.Runs)
	}
	out := document.New()
	out.Blocks = []*document.Block{p}
	return out, true
}

// pastedMath only opts plain clipboard text into Markdown import when a
// complete display formula exists. Unfinished delimiters, currency and
// dollar signs in fenced code keep the ordinary paste behavior.
func pastedMath(source string) *document.Document {
	if len(source) > 8<<20 || !strings.Contains(source, "$$") && !strings.Contains(source, `\[`) {
		return nil
	}
	parsed := parser.Parse(source)
	found := false
	for i := 1; i < parsed.Block(0).Next; i++ {
		b := parsed.Block(i)
		if b.Kind == parser.MathBlock {
			if b.Open {
				return nil
			}
			found = true
		}
	}
	if !found {
		return nil
	}
	return ImportMarkdown(source)
}

// rich reports whether a document holds more than plain paragraphs of
// plain text, worth pasting as structure.
func rich(doc *document.Document) bool {
	for _, b := range doc.Blocks {
		if b.Kind != "paragraph" || b.Align != "" || b.Indent != 0 || b.Background != "" || len(b.Comments) > 0 {
			return true
		}
		for _, r := range b.Runs {
			if r.IsImage() {
				return true
			}
			s := r.Style
			s.NoMath = false
			if s != (document.Style{}) {
				return true
			}
		}
	}
	return false
}

func normalizeLineBreaks(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

func hasIndentedLine(s string) bool {
	for _, line := range strings.Split(normalizeLineBreaks(s), "\n") {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') && strings.TrimLeft(line, " \t") != "" {
			return true
		}
	}
	return false
}

func sameTrimmedLines(a, b string) bool {
	na := strings.TrimSuffix(normalizeLineBreaks(a), "\n")
	nb := strings.TrimSuffix(normalizeLineBreaks(b), "\n")
	al := strings.Split(na, "\n")
	bl := strings.Split(nb, "\n")
	if len(al) != len(bl) {
		return false
	}
	for i := range al {
		if strings.TrimLeft(al[i], " \t") != strings.TrimLeft(bl[i], " \t") {
			return false
		}
	}
	return true
}
