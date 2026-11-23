package editor

import (
	"strconv"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// HTMLImportOptions configures ImportHTMLDocument for read-only HTML/EPUB reading.
type HTMLImportOptions struct {
	EmbedImages bool
	ResolveSrc  func(string) string
}

// HTMLImportResult holds the parsed document, HTML id/name-to-block-ID mapping,
// and any <title> extracted from <head>.
type HTMLImportResult struct {
	Doc     *document.Document
	Anchors map[string]string
	Title   string
}

// ImportHTML turns HTML copied from a browser, Feishu, Notion or a word
// processor into a document: headings, paragraphs, lists and tasks, quotes,
// code, tables, rules and images, with bold, italic, underline, strike,
// code and links. Styles, scripts and what ArcMD cannot show are left out;
// the text always survives.
func ImportHTML(source string) *document.Document {
	return ImportHTMLDocument(source, HTMLImportOptions{}).Doc
}

// ImportHTMLDocument imports HTML with optional real image block emission and
// anchor ID mapping for native HTML and EPUB reading.
func ImportHTMLDocument(source string, opts HTMLImportOptions) HTMLImportResult {
	root, err := html.Parse(strings.NewReader(source))
	doc := document.New()
	doc.Blocks = nil
	if err != nil {
		doc.Blocks = []*document.Block{document.NewBlock("paragraph", strings.TrimSpace(source))}
		return HTMLImportResult{Doc: doc}
	}
	h := &htmlImporter{
		embedImages: opts.EmbedImages,
		resolveSrc:  opts.ResolveSrc,
		anchors:     make(map[string]string),
	}
	h.walk(root, document.Style{})
	h.flush()
	if len(h.pendingIDs) > 0 && len(h.blocks) > 0 {
		h.bindPendingIDs(h.blocks[len(h.blocks)-1].ID)
	}
	doc.Blocks = h.blocks
	if len(doc.Blocks) == 0 {
		b := document.NewBlock("paragraph", "")
		h.bindPendingIDs(b.ID)
		doc.Blocks = []*document.Block{b}
	}
	return HTMLImportResult{
		Doc:     doc,
		Anchors: h.anchors,
		Title:   h.title,
	}
}

type htmlImporter struct {
	embedImages bool
	resolveSrc  func(string) string
	anchors     map[string]string
	pendingIDs  []string
	title       string

	blocks []*document.Block
	// cur is the block text goes in, made on the first text.
	cur *document.Block
	// kind, level, indent and checked are what a new block becomes, set by
	// the elements around the text.
	kind    string
	level   int
	indent  int
	align   string
	checked *bool
	// lists are the kinds of the lists the text is in, innermost last.
	lists     []string
	listStart int
	pre       bool
}

func (h *htmlImporter) recordID(id string) {
	if !h.embedImages {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	for _, existing := range h.pendingIDs {
		if existing == id {
			return
		}
	}
	h.pendingIDs = append(h.pendingIDs, id)
}

func (h *htmlImporter) bindPendingIDs(blockID string) {
	if !h.embedImages || blockID == "" || len(h.pendingIDs) == 0 {
		return
	}
	if h.anchors == nil {
		h.anchors = make(map[string]string)
	}
	for _, id := range h.pendingIDs {
		if _, exists := h.anchors[id]; !exists {
			h.anchors[id] = blockID
		}
		if low := strings.ToLower(id); low != id {
			if _, exists := h.anchors[low]; !exists {
				h.anchors[low] = blockID
			}
		}
	}
	h.pendingIDs = h.pendingIDs[:0]
}

func (h *htmlImporter) appendBlock(b *document.Block) {
	if b == nil {
		return
	}
	h.bindPendingIDs(b.ID)
	h.blocks = append(h.blocks, b)
}

// flush ends the block being written; an empty one is dropped.
func (h *htmlImporter) flush() {
	if h.cur == nil {
		return
	}
	b := h.cur
	h.cur = nil
	b.Runs = document.Compact(b.Runs)
	if b.Kind != "code" {
		// Collapse the whitespace HTML ignores; trim the ends.
		for i := range b.Runs {
			b.Runs[i].Text = collapseSpace(b.Runs[i].Text)
		}
		if len(b.Runs) > 0 {
			b.Runs[0].Text = strings.TrimLeft(b.Runs[0].Text, " ")
			last := len(b.Runs) - 1
			b.Runs[last].Text = strings.TrimRight(b.Runs[last].Text, " ")
		}
		b.Runs = document.Compact(b.Runs)
	} else {
		text := strings.ReplaceAll(strings.TrimSuffix(b.Text(), "\n"), "\u00a0", " ")
		b.Runs = []document.Run{{Text: text}}
	}
	if len(b.Runs) == 0 && b.Kind != "task" && b.Kind != "bullet" && b.Kind != "numbered" {
		return
	}
	if len(b.Runs) == 0 {
		b.Runs = []document.Run{{Text: ""}}
	}
	h.appendBlock(b)
}

func collapseSpace(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' || r == ' ' {
			if !space {
				b.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// text adds text in the style to the block being written, starting one.
func (h *htmlImporter) text(s string, style document.Style) {
	if h.cur == nil {
		if !h.pre && strings.TrimSpace(strings.ReplaceAll(s, " ", " ")) == "" {
			return
		}
		kind := h.kind
		if kind == "" {
			kind = "paragraph"
		}
		align := ""
		if kind == "paragraph" || kind == "heading" {
			align = h.align
		}
		h.cur = &document.Block{ID: document.NewID(), Kind: kind, Level: h.level, Indent: h.indent, Align: align}
		if h.checked != nil {
			h.cur.Kind, h.cur.Checked = "task", *h.checked
		}
	}
	h.cur.Runs = append(h.cur.Runs, document.Run{Text: s, Style: style})
}

// block runs inside with what new blocks become set, flushing around it.
func (h *htmlImporter) block(kind string, level int, inside func()) {
	h.flush()
	saved, savedLevel := h.kind, h.level
	h.kind, h.level = kind, level
	inside()
	h.flush()
	h.kind, h.level = saved, savedLevel
}

func (h *htmlImporter) withAlign(align string, explicit bool, inside func()) {
	if !explicit {
		inside()
		return
	}
	saved := h.align
	h.align = align
	inside()
	h.align = saved
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}

// styled reads the inline styles word processors write instead of tags.
func styled(n *html.Node, style document.Style) document.Style {
	css := strings.ToLower(strings.ReplaceAll(attr(n, "style"), " ", ""))
	if css == "" {
		return style
	}
	hasDataMark := attr(n, "data-mark") != ""
	if strings.Contains(css, "font-weight:bold") || strings.Contains(css, "font-weight:700") || strings.Contains(css, "font-weight:600") {
		style.Bold = true
	}
	if strings.Contains(css, "font-style:italic") {
		style.Italic = true
	}
	if !hasDataMark && (strings.Contains(css, "text-decoration:underline") || strings.Contains(css, "text-decoration-line:underline")) {
		style.Underline = true
	}
	if !hasDataMark && strings.Contains(css, "line-through") {
		style.Strike = true
	}
	if strings.Contains(css, "vertical-align:super") {
		style.Script = "sup"
	}
	if strings.Contains(css, "vertical-align:sub") {
		style.Script = "sub"
	}
	return style
}

func (h *htmlImporter) children(n *html.Node, style document.Style) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		h.walk(c, style)
	}
}

func (h *htmlImporter) walk(n *html.Node, style document.Style) {
	switch n.Type {
	case html.TextNode:
		h.text(n.Data, style)
		return
	case html.DocumentNode:
		h.children(n, style)
		return
	case html.ElementNode:
	default:
		return
	}
	if h.embedImages && htmlElementHidden(n) {
		return
	}
	h.recordID(attr(n, "id"))
	if n.DataAtom == atom.A {
		h.recordID(attr(n, "name"))
	}
	style = applyNodeRichStyle(n, styled(n, style))
	switch n.DataAtom {
	case atom.Head:
		if h.embedImages && h.title == "" {
			h.title = extractHTMLHeadTitle(n)
		}
		return
	case atom.Script, atom.Style, atom.Title, atom.Meta, atom.Link, atom.Noscript, atom.Template, atom.Button:
		return
	case atom.Svg:
		if h.embedImages {
			if src := svgImageHref(n); src != "" {
				if h.resolveSrc != nil {
					src = h.resolveSrc(src)
				}
				if safeImageEmbedURL(src) {
					h.flush()
					b := document.NewBlock("image", "")
					b.URL = src
					h.appendBlock(b)
				}
			}
		}
		return
	case atom.B, atom.Strong:
		style.Bold = true
	case atom.I, atom.Em, atom.Cite, atom.Var, atom.Dfn:
		style.Italic = true
	case atom.Sup:
		style.Script = "sup"
	case atom.Sub:
		style.Script = "sub"
	case atom.U, atom.Ins:
		style.Underline = true
	case atom.S, atom.Del, atom.Strike:
		style.Strike = true
	case atom.Kbd:
		if !h.pre {
			style.Kbd = true
			style.Code = false
		}
	case atom.Code, atom.Samp, atom.Tt:
		if !h.pre {
			style.Code = true
			style.Kbd = false
		}
	case atom.Q:
		h.text("“", style)
		h.children(n, style)
		h.text("”", style)
		return
	case atom.A:
		if href := attr(n, "href"); href != "" && !strings.HasPrefix(strings.ToLower(href), "javascript:") {
			style.Link = href
		}
	case atom.Br:
		if h.pre {
			h.text("\n", style)
			return
		}
		// A line break inside a paragraph begins another, as ArcMD's
		// paragraphs hold no line breaks.
		h.flush()
		return
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		level := int(n.Data[1] - '0')
		align, explicit := parseBlockAlignExplicit(n)
		h.withAlign(align, explicit, func() {
			h.block("heading", level, func() { h.children(n, style) })
		})
		return
	case atom.P, atom.Div, atom.Section, atom.Article, atom.Header, atom.Footer, atom.Main, atom.Aside, atom.Nav, atom.Figure, atom.Figcaption, atom.Dt, atom.Dd, atom.Address, atom.Center, atom.Details, atom.Summary, atom.Dl:
		if h.pre {
			if h.cur != nil && len(h.cur.Runs) > 0 && !strings.HasSuffix(h.cur.Text(), "\n") {
				h.text("\n", document.Style{})
			}
			h.children(n, document.Style{})
			return
		}
		if h.embedImages {
			switch n.DataAtom {
			case atom.Figcaption:
				capStyle := style
				capStyle.Italic = true
				h.withAlign("center", true, func() {
					h.block("paragraph", 0, func() { h.children(n, capStyle) })
				})
				return
			case atom.Dt, atom.Summary:
				dtStyle := style
				dtStyle.Bold = true
				h.block("paragraph", 0, func() { h.children(n, dtStyle) })
				return
			case atom.Dd:
				savedIndent := h.indent
				h.indent = min(12, h.indent+1)
				h.block("paragraph", 0, func() { h.children(n, style) })
				h.indent = savedIndent
				return
			}
		}
		if n.DataAtom == atom.Div && hasHTMLClass(n, "arcmd-columns") {
			h.flush()
			if cols := columnsOfHTML(n); cols != nil {
				h.appendBlock(cols)
				return
			}
		}
		if n.DataAtom == atom.Div && h.kind == "" && hasPreWhiteSpace(n) {
			h.flush()
			h.pre = true
			h.block("code", 0, func() {
				h.cur = &document.Block{ID: document.NewID(), Kind: "code", Language: codeLanguage(n)}
				h.children(n, document.Style{})
			})
			h.pre = false
			return
		}
		align, explicit := parseBlockAlignExplicit(n)
		if n.DataAtom == atom.Center {
			align, explicit = "center", true
		}
		h.withAlign(align, explicit, func() {
			if h.kind == "quote" || h.kind == "bullet" || h.kind == "numbered" {
				// Paragraphs inside a quote or a list item stay what they are in.
				h.flush()
				h.children(n, style)
				h.flush()
				return
			}
			h.block(h.kind, h.level, func() { h.children(n, style) })
		})
		return
	case atom.Blockquote:
		h.block("quote", 0, func() { h.children(n, style) })
		return
	case atom.Pre:
		h.flush()
		h.pre = true
		h.block("code", 0, func() {
			h.cur = &document.Block{ID: document.NewID(), Kind: "code", Language: codeLanguage(n)}
			h.children(n, document.Style{})
		})
		h.pre = false
		return
	case atom.Hr:
		h.flush()
		h.appendBlock(document.NewBlock("rule", ""))
		return
	case atom.Img:
		h.flush()
		src := strings.TrimSpace(attr(n, "src"))
		if h.embedImages {
			if isTinyTrackingPixel(n) {
				return
			}
			if h.resolveSrc != nil && src != "" {
				src = h.resolveSrc(src)
			}
			if src != "" && safeImageEmbedURL(src) {
				b := document.NewBlock("image", strings.TrimSpace(attr(n, "alt")))
				b.URL = src
				h.appendBlock(b)
				return
			}
		}
		if src == "" || strings.HasPrefix(src, "data:") {
			if alt := attr(n, "alt"); alt != "" {
				h.text(alt, style)
			}
			return
		}
		// A picture elsewhere is a link to it: ArcMD shows pictures kept
		// beside the document.
		label := attr(n, "alt")
		if label == "" {
			label = i18n.T("图片")
		}
		h.text(label, document.Style{Link: src})
		h.flush()
		return
	case atom.Ul, atom.Ol, atom.Menu:
		if h.pre {
			h.children(n, document.Style{})
			return
		}
		kind := "bullet"
		startVal := 0
		if n.DataAtom == atom.Ol {
			kind = "numbered"
			if s := strings.TrimSpace(attr(n, "start")); s != "" {
				if v, err := strconv.Atoi(s); err == nil && v >= 1 && v <= 999999 {
					startVal = v
				}
			}
		}
		h.flush()
		h.lists = append(h.lists, kind)
		savedIndent := h.indent
		savedStart := h.listStart
		h.indent = min(12, len(h.lists)-1)
		if n.DataAtom == atom.Ol {
			if startVal == 0 {
				probe := &document.Block{Kind: "numbered", Indent: h.indent}
				if prevNum, adj := document.PreviousListNumber(append(append([]*document.Block(nil), h.blocks...), probe), len(h.blocks)); adj && prevNum > 0 {
					startVal = 1
				}
			}
			h.listStart = startVal
		} else {
			h.listStart = 0
		}
		h.children(n, style)
		h.flush()
		h.lists = h.lists[:len(h.lists)-1]
		h.indent = savedIndent
		h.listStart = savedStart
		return
	case atom.Li:
		if h.pre {
			if h.cur != nil && len(h.cur.Runs) > 0 && !strings.HasSuffix(h.cur.Text(), "\n") {
				h.text("\n", document.Style{})
			}
			h.children(n, document.Style{})
			return
		}
		kind := "bullet"
		if len(h.lists) > 0 {
			kind = h.lists[len(h.lists)-1]
		}
		h.flush()
		liStart := h.listStart
		h.listStart = 0
		if s := strings.TrimSpace(attr(n, "value")); s != "" {
			if v, err := strconv.Atoi(s); err == nil && v >= 1 && v <= 999999 {
				liStart = v
			}
		}
		saved, savedChecked, savedListStart := h.kind, h.checked, h.listStart
		h.kind, h.checked, h.listStart = kind, taskState(n), 0
		beforeLen := len(h.blocks)
		h.children(n, style)
		h.flush()
		h.kind, h.checked, h.listStart = saved, savedChecked, savedListStart
		if liStart > 0 && len(h.blocks) > beforeLen {
			first := h.blocks[beforeLen]
			if first.Kind == "numbered" {
				if liStart > 1 {
					first.Start = liStart
				} else if prevNum, adj := document.PreviousListNumber(h.blocks, beforeLen); adj && prevNum > 0 {
					first.Start = 1
				}
			}
		}
		return
	case atom.Input:
		return
	case atom.Table:
		h.flush()
		if h.embedImages {
			h.emitTableCaption(n, style)
		}
		if t := tableOf(n); t != nil {
			h.appendBlock(t)
		}
		return
	}
	h.children(n, style)
}

func htmlElementHidden(n *html.Node) bool {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, "hidden") {
			return true
		}
		if strings.EqualFold(a.Key, "style") {
			css := strings.ToLower(strings.ReplaceAll(a.Val, " ", ""))
			if strings.Contains(css, "display:none") {
				return true
			}
		}
	}
	return false
}

func extractHTMLHeadTitle(head *html.Node) string {
	for c := head.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.DataAtom == atom.Title {
			var b strings.Builder
			for t := c.FirstChild; t != nil; t = t.NextSibling {
				if t.Type == html.TextNode {
					b.WriteString(t.Data)
				}
			}
			return strings.TrimSpace(collapseSpace(b.String()))
		}
	}
	return ""
}

func svgImageHref(svg *html.Node) string {
	var found string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != "" {
			return
		}
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "image") {
			for _, name := range []string{"href", "xlink:href"} {
				if v := strings.TrimSpace(attr(n, name)); v != "" {
					found = v
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(svg)
	return found
}

func isTinyTrackingPixel(img *html.Node) bool {
	w := strings.TrimSpace(strings.TrimSuffix(strings.ToLower(attr(img, "width")), "px"))
	h := strings.TrimSpace(strings.TrimSuffix(strings.ToLower(attr(img, "height")), "px"))
	return (w == "1" && h == "1") || w == "0" || h == "0"
}

func safeImageEmbedURL(src string) bool {
	low := strings.ToLower(strings.TrimSpace(src))
	if low == "" {
		return false
	}
	if strings.HasPrefix(low, "javascript:") || strings.HasPrefix(low, "vbscript:") {
		return false
	}
	if strings.HasPrefix(low, "data:") {
		return strings.HasPrefix(low, "data:image/")
	}
	return true
}

func (h *htmlImporter) emitTableCaption(table *html.Node, style document.Style) {
	for c := table.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.DataAtom == atom.Caption {
			capStyle := style
			capStyle.Bold = true
			h.withAlign("center", true, func() {
				h.block("paragraph", 0, func() { h.children(c, capStyle) })
			})
			return
		}
	}
}

// taskState is whether a list item is a task, by the check box it starts
// with, and whether it is done.
func taskState(li *html.Node) *bool {
	var found *bool
	var look func(*html.Node, int)
	look = func(n *html.Node, depth int) {
		for c := n.FirstChild; c != nil && found == nil && depth < 3; c = c.NextSibling {
			if c.Type == html.ElementNode && c.DataAtom == atom.Input && strings.EqualFold(attr(c, "type"), "checkbox") {
				checked := false
				for _, a := range c.Attr {
					if strings.EqualFold(a.Key, "checked") {
						checked = true
					}
				}
				found = &checked
				return
			}
			if c.Type == html.ElementNode && (c.DataAtom == atom.Ul || c.DataAtom == atom.Ol) {
				continue
			}
			look(c, depth+1)
		}
	}
	look(li, 0)
	return found
}

func hasPreWhiteSpace(n *html.Node) bool {
	css := strings.ToLower(attr(n, "style"))
	for _, decl := range strings.Split(css, ";") {
		prop, val, ok := strings.Cut(decl, ":")
		if !ok || strings.TrimSpace(prop) != "white-space" {
			continue
		}
		val = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(val), "!important"))
		if val == "pre" || val == "pre-wrap" {
			return true
		}
	}
	return false
}

func hasHTMLClass(n *html.Node, want string) bool {
	for _, cl := range strings.Fields(attr(n, "class")) {
		if strings.EqualFold(cl, want) {
			return true
		}
	}
	return false
}

func columnsOfHTML(n *html.Node) *document.Block {
	var cols []*document.Block
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.DataAtom != atom.Div {
			continue
		}
		inner := &htmlImporter{}
		inner.children(c, document.Style{})
		inner.flush()
		children := inner.blocks
		if len(children) == 0 {
			children = []*document.Block{document.NewBlock("paragraph", "")}
		}
		cols = append(cols, &document.Block{
			ID:       document.NewID(),
			Kind:     "column",
			Children: children,
		})
	}
	if len(cols) < 2 || len(cols) > 5 {
		return nil
	}
	return &document.Block{
		ID:       document.NewID(),
		Kind:     "columns",
		Widths:   parseDataWidthsAttr(attr(n, "data-widths"), len(cols)),
		Children: cols,
	}
}

// codeLanguage reads a code block's language from its class, as
// "language-go" or "lang-go".
func codeLanguage(pre *html.Node) string {
	for n := pre; n != nil; n = n.FirstChild {
		for _, class := range strings.Fields(attr(n, "class")) {
			for _, prefix := range []string{"language-", "lang-"} {
				if lang, ok := strings.CutPrefix(class, prefix); ok {
					return strings.ToLower(lang)
				}
			}
		}
		if n.FirstChild == nil || n.FirstChild.Type != html.ElementNode {
			break
		}
	}
	return ""
}

// tableOf makes a table block of an HTML table, its cells' text with its
// styles; rows are padded to the widest.
func tableOf(n *html.Node) *document.Block {
	return tableOfMode(n, false)
}

func tableOfMode(n *html.Node, exactWhitespace bool) *document.Block {
	var colWidths []float32
	var rawRows [][]*html.Node
	var collect func(*html.Node)
	collect = func(cur *html.Node) {
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.DataAtom {
			case atom.Colgroup:
				for col := c.FirstChild; col != nil; col = col.NextSibling {
					if col.Type == html.ElementNode && col.DataAtom == atom.Col {
						colWidths = append(colWidths, parseHTMLWidth(col))
					}
				}
			case atom.Col:
				colWidths = append(colWidths, parseHTMLWidth(c))
			case atom.Tr:
				var tds []*html.Node
				for td := c.FirstChild; td != nil; td = td.NextSibling {
					if td.Type == html.ElementNode && (td.DataAtom == atom.Td || td.DataAtom == atom.Th) {
						tds = append(tds, td)
					}
				}
				if len(tds) > 0 {
					rawRows = append(rawRows, tds)
				}
			case atom.Table:
				// A table inside a cell is not followed.
			default:
				collect(c)
			}
		}
	}
	collect(n)
	if len(rawRows) == 0 {
		return nil
	}

	occupied := map[[2]int]bool{}
	placed := map[[2]int]*document.Block{}
	numRows := len(rawRows)
	numCols := 0

	for r, tds := range rawRows {
		c := 0
		for _, td := range tds {
			for occupied[[2]int{r, c}] {
				c++
			}
			colSpan := parseSpanAttr(attr(td, "colspan"))
			rowSpan := parseSpanAttr(attr(td, "rowspan"))
			// Clamp spans that would collide with an already occupied cell.
			for dc := 1; dc < colSpan; dc++ {
				if occupied[[2]int{r, c + dc}] {
					colSpan = dc
					break
				}
			}
			cell := document.NewBlock("cell", "")
			if exactWhitespace {
				runs, comments := parseInlineHTMLNodesExact(td, document.Style{})
				if len(runs) > 0 {
					cell.Runs = runs
				}
				if len(comments) > 0 {
					cell.Comments = comments
				}
			} else {
				inner := &htmlImporter{kind: "paragraph"}
				inner.children(td, document.Style{})
				inner.flush()
				var runs []document.Run
				for i, b := range inner.blocks {
					if i > 0 {
						runs = append(runs, document.Run{Text: "\n"})
					}
					runs = append(runs, b.Runs...)
				}
				if runs = document.Compact(runs); len(runs) > 0 {
					cell.Runs = runs
				}
			}
			if align := parseBlockAlign(td); align != "" {
				cell.Align = align
			}
			if bg := parseCellBackground(td); bg != "" {
				cell.Background = bg
			}
			if colSpan > 1 {
				cell.ColSpan = colSpan
			}
			if rowSpan > 1 {
				cell.RowSpan = rowSpan
			}
			placed[[2]int{r, c}] = cell
			for dr := 0; dr < rowSpan; dr++ {
				for dc := 0; dc < colSpan; dc++ {
					occupied[[2]int{r + dr, c + dc}] = true
				}
			}
			numRows = max(numRows, r+rowSpan)
			numCols = max(numCols, c+colSpan)
			c += colSpan
		}
	}
	if numCols == 0 {
		return nil
	}
	table := document.Table(numRows, numCols)
	for pos, cell := range placed {
		table.Children[pos[0]].Children[pos[1]] = cell
	}
	if len(colWidths) == numCols {
		valid := true
		for _, w := range colWidths {
			if !(w > 0 && w <= 10000) {
				valid = false
				break
			}
		}
		if valid {
			table.Widths = colWidths
		}
	}
	return table
}

func parseSpanAttr(val string) int {
	n, err := strconv.Atoi(strings.TrimSpace(val))
	if err != nil || n < 1 {
		return 1
	}
	return min(64, n)
}

func parseHTMLWidth(n *html.Node) float32 {
	raw := strings.TrimSpace(attr(n, "width"))
	if raw == "" {
		raw = parseCSSProps(attr(n, "style"))["width"]
	}
	raw = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), "px")
	if raw == "" || strings.HasSuffix(raw, "%") {
		return 0
	}
	v, err := strconv.ParseFloat(raw, 32)
	if err != nil || v <= 0 || v > 10000 {
		return 0
	}
	return float32(v)
}

func parseBlockAlignExplicit(n *html.Node) (string, bool) {
	a := strings.ToLower(strings.TrimSpace(attr(n, "align")))
	if a == "" {
		a = strings.ToLower(strings.TrimSpace(parseCSSProps(attr(n, "style"))["text-align"]))
	}
	switch a {
	case "center", "right":
		return a, true
	case "left":
		return "", true
	}
	return "", false
}

func parseBlockAlign(n *html.Node) string {
	a, _ := parseBlockAlignExplicit(n)
	return a
}

func parseCellBackground(n *html.Node) string {
	if bg := attr(n, "data-bg"); validStyleName(bg, document.TextHighlights) {
		return bg
	}
	props := parseCSSProps(attr(n, "style"))
	raw := props["background-color"]
	if raw == "" {
		raw = props["background"]
	}
	if raw == "" {
		raw = attr(n, "bgcolor")
	}
	return matchHighlightCSS(raw)
}
