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
	normalized := normalizeHTMLFragment(source)
	root, err := html.Parse(strings.NewReader(normalized))
	doc := document.New()
	doc.Blocks = nil
	if err != nil {
		doc.Blocks = []*document.Block{document.NewBlock("paragraph", strings.TrimSpace(source))}
		return HTMLImportResult{Doc: doc}
	}
	var classes map[string]map[string]string
	if !opts.EmbedImages {
		classes = extractStyleClasses(root)
	}
	h := &htmlImporter{
		embedImages: opts.EmbedImages,
		resolveSrc:  opts.ResolveSrc,
		anchors:     make(map[string]string),
		classes:     classes,
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
	classes     map[string]map[string]string
	inTableCell bool

	blocks []*document.Block
	// cur is the block text goes in, made on the first text.
	cur *document.Block
	// kind, level, indent and checked are what a new block becomes, set by
	// the elements around the text.
	kind    string
	level   int
	indent  int
	align   string
	bg      string
	checked *bool
	// lists are the kinds of the lists the text is in, innermost last.
	lists     []string
	listStart int
	pre       bool
}

// normalizeHTMLFragment wraps bare HTML table-part fragments (such as Windows
// Excel's CF_HTML StartFragment..EndFragment slices that begin directly with
// <tr>, <col>, or <td>) in a <table> so the HTML5 parser does not discard them.
func normalizeHTMLFragment(source string) string {
	trimmed := strings.TrimPrefix(source, "\ufeff")
	z := html.NewTokenizer(strings.NewReader(trimmed))
	skipRawTag := ""
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return source
		case html.CommentToken, html.DoctypeToken:
			continue
		case html.TextToken:
			if skipRawTag != "" {
				continue
			}
			if strings.TrimSpace(string(z.Text())) != "" {
				return source
			}
		case html.EndTagToken:
			if skipRawTag != "" {
				name, _ := z.TagName()
				if strings.EqualFold(string(name), skipRawTag) {
					skipRawTag = ""
				}
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			if skipRawTag != "" {
				continue
			}
			nameBytes, _ := z.TagName()
			name := strings.ToLower(string(nameBytes))
			switch name {
			case "meta", "link", "base":
				continue
			case "style", "title":
				if tt == html.StartTagToken {
					skipRawTag = name
				}
				continue
			case "tr", "tbody", "thead", "tfoot", "col", "colgroup", "caption":
				return "<table>" + trimmed + "</table>"
			case "td", "th":
				return "<table><tbody><tr>" + trimmed + "</tr></tbody></table>"
			default:
				return source
			}
		}
	}
}

// htmlHasNonImageContent reports whether clipboard HTML carries real document
// or table content rather than only an image wrapper (such as browser "Copy
// Image", chat image copies, Excel charts, or empty Figma metadata).
func htmlHasNonImageContent(source string) bool {
	root, err := html.Parse(strings.NewReader(normalizeHTMLFragment(source)))
	if err != nil {
		return false
	}
	hasText := false
	hasMedia := false
	tableCells := 0
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, inTable bool) {
		if hasText {
			return
		}
		switch n.Type {
		case html.TextNode:
			if strings.TrimSpace(strings.ReplaceAll(n.Data, "\u00a0", " ")) != "" {
				hasText = true
			}
			return
		case html.ElementNode:
			switch n.DataAtom {
			case atom.Head, atom.Script, atom.Style, atom.Title, atom.Meta, atom.Link, atom.Noscript, atom.Template:
				return
			case atom.Img, atom.Svg, atom.Canvas, atom.Video:
				hasMedia = true
				return
			case atom.Table:
				inTable = true
			case atom.Td, atom.Th:
				if inTable {
					tableCells++
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inTable)
			if hasText {
				return
			}
		}
	}
	walk(root, false)
	return hasText || (tableCells >= 1 && !hasMedia)
}

// extractStyleClasses parses simple class selectors (.cls and tag.cls) from
// <style> blocks (such as Microsoft Excel's .xl65 and .font5 rules, including
// rules wrapped in <!-- ... -->). Complex selectors and @-rules are ignored.
func extractStyleClasses(root *html.Node) map[string]map[string]string {
	var css strings.Builder
	var collect func(*html.Node)
	collect = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Style {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					css.WriteString(c.Data)
					css.WriteByte('\n')
				}
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collect(c)
		}
	}
	collect(root)
	raw := css.String()
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	raw = stripCSSComments(raw)
	out := map[string]map[string]string{}
	depth := 0
	selStart := 0
	bodyStart := -1
	skipAtDepth := -1
	inQuote := byte(0)
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if inQuote != 0 {
			if ch == '\\' && i+1 < len(raw) {
				i++
				continue
			}
			if ch == inQuote {
				inQuote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			inQuote = ch
			continue
		}
		switch ch {
		case '{':
			if depth == 0 {
				sel := strings.TrimSpace(raw[selStart:i])
				if strings.HasPrefix(sel, "@") {
					skipAtDepth = 0
				} else {
					bodyStart = i + 1
				}
			}
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
			if depth == 0 {
				if skipAtDepth == 0 {
					skipAtDepth = -1
				} else if bodyStart >= 0 {
					selectors := raw[selStart : bodyStart-1]
					body := raw[bodyStart:i]
					if props := parseCSSProps(body); len(props) > 0 {
						for _, part := range strings.Split(selectors, ",") {
							key := simpleClassSelectorKey(part)
							if key == "" {
								continue
							}
							dst := out[key]
							if dst == nil {
								dst = map[string]string{}
								out[key] = dst
							}
							for k, v := range props {
								dst[k] = v
							}
						}
					}
				}
				bodyStart = -1
				selStart = i + 1
			}
		case ';':
			if depth == 0 {
				selStart = i + 1
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stripCSSComments(s string) string {
	s = strings.ReplaceAll(s, "<!--", "")
	s = strings.ReplaceAll(s, "-->", "")
	if !strings.Contains(s, "/*") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				break
			}
			i += 2 + end + 2
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func simpleClassSelectorKey(sel string) string {
	s := strings.ToLower(strings.TrimSpace(sel))
	if s == "" || strings.ContainsAny(s, " \t\r\n>+~[:#") {
		return ""
	}
	dot := strings.IndexByte(s, '.')
	if dot < 0 || strings.IndexByte(s[dot+1:], '.') >= 0 {
		return ""
	}
	tag, cls := s[:dot], s[dot+1:]
	if cls == "" {
		return ""
	}
	if tag == "" {
		return cls
	}
	switch tag {
	case "td", "th", "font", "span", "p", "div", "b", "i", "strong", "em":
		return tag + "." + cls
	}
	return ""
}

func mergedNodeCSSProps(n *html.Node, classes map[string]map[string]string) map[string]string {
	var props map[string]string
	if len(classes) > 0 {
		tag := strings.ToLower(n.Data)
		for _, cl := range strings.Fields(strings.ToLower(attr(n, "class"))) {
			for _, key := range []string{cl, tag + "." + cl} {
				if rule := classes[key]; len(rule) > 0 {
					if props == nil {
						props = make(map[string]string, len(rule))
					}
					for k, v := range rule {
						props[k] = v
					}
				}
			}
		}
	}
	if inline := parseCSSProps(attr(n, "style")); len(inline) > 0 {
		if props == nil {
			return inline
		}
		for k, v := range inline {
			props[k] = v
		}
	}
	return props
}

func cleanCSSValue(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.TrimSuffix(v, "!important")
	return strings.TrimSpace(v)
}

func isDefaultInkCSS(v string) bool {
	v = cleanCSSValue(v)
	switch v {
	case "black", "windowtext", "auto", "initial", "inherit", "currentcolor":
		return true
	}
	r, g, b, ok := parseHexRGB(v)
	if !ok {
		return false
	}
	hi, lo := max(r, max(g, b)), min(r, min(g, b))
	return hi < 75 && hi-lo < 24
}

func applyCSSPropsStyle(props map[string]string, hasDataMark, includeHighlight bool, style document.Style) document.Style {
	if len(props) == 0 {
		return style
	}
	if fw := cleanCSSValue(props["font-weight"]); fw != "" {
		switch fw {
		case "bold", "bolder", "600", "700", "800", "900":
			style.Bold = true
		case "normal", "lighter", "100", "200", "300", "400", "500":
			style.Bold = false
		}
	}
	if fs := cleanCSSValue(props["font-style"]); fs != "" {
		switch fs {
		case "italic", "oblique":
			style.Italic = true
		case "normal":
			style.Italic = false
		}
	}
	if !hasDataMark {
		for _, prop := range []string{"text-decoration", "text-decoration-line"} {
			if td := cleanCSSValue(props[prop]); td != "" {
				if td == "none" {
					style.Underline = false
					style.Strike = false
				} else {
					if strings.Contains(td, "underline") {
						style.Underline = true
					}
					if strings.Contains(td, "line-through") {
						style.Strike = true
					}
				}
			}
		}
	}
	if va := cleanCSSValue(props["vertical-align"]); va != "" {
		switch va {
		case "super":
			style.Script = "sup"
		case "sub":
			style.Script = "sub"
		}
	}
	if col := props["color"]; col != "" {
		if c := matchTextColorCSS(col); c != "" {
			style.Color = c
		} else if isDefaultInkCSS(col) {
			style.Color = ""
		}
	}
	if includeHighlight && style.Annotation != "highlight" {
		bg := props["background-color"]
		if bg == "" {
			bg = props["background"]
		}
		if h := matchBackgroundCSS(bg); h != "" {
			style.Highlight = h
		}
	}
	return style
}

func matchBackgroundCSS(raw string) string {
	if raw == "" {
		return ""
	}
	if h := matchHighlightCSS(raw); h != "" {
		return h
	}
	// Shorthand CSS background (e.g. Excel's "#D9E1F2 none" or "rgb(217, 225, 242) none").
	low := cleanCSSValue(raw)
	if idx := strings.Index(low, "rgb"); idx >= 0 {
		if end := strings.IndexByte(low[idx:], ')'); end > 0 {
			if h := matchHighlightCSS(low[idx : idx+end+1]); h != "" {
				return h
			}
		}
	}
	for _, field := range strings.Fields(low) {
		if h := matchHighlightCSS(field); h != "" {
			return h
		}
	}
	return ""
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
		bg := ""
		if document.AllowsBlockBackground(kind) {
			bg = h.bg
		}
		h.cur = &document.Block{ID: document.NewID(), Kind: kind, Level: h.level, Indent: h.indent, Align: align, Background: bg}
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

func (h *htmlImporter) withBg(bg string, inside func()) {
	if bg == "" {
		inside()
		return
	}
	saved := h.bg
	h.bg = bg
	inside()
	h.bg = saved
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
	nodeBg := ""
	if !h.inTableCell {
		switch n.DataAtom {
		case atom.P, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Blockquote, atom.Li:
			nodeBg = parseBlockBackground(n)
		case atom.Div, atom.Center:
			if bg := attr(n, "data-bg"); validStyleName(bg, document.TextHighlights) {
				nodeBg = bg
			}
		}
		prevHighlight := style.Highlight
		style = applyNodeRichStyle(n, styled(n, style))
		if nodeBg != "" && n.DataAtom != atom.Mark && attr(n, "data-highlight") == "" {
			style.Highlight = prevHighlight
		}
	} else {
		props := mergedNodeCSSProps(n, h.classes)
		style = applyCSSPropsStyle(props, attr(n, "data-mark") != "", true, style)
		if n.DataAtom == atom.Font {
			if col := attr(n, "color"); col != "" {
				if c := matchTextColorCSS(col); c != "" {
					style.Color = c
				} else if isDefaultInkCSS(col) {
					style.Color = ""
				}
			}
		}
		style = applyNodeRichStyle(n, style)
	}
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
		h.withBg(nodeBg, func() {
			h.withAlign(align, explicit, func() {
				h.block("heading", level, func() { h.children(n, style) })
			})
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
		h.withBg(nodeBg, func() {
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
		})
		return
	case atom.Blockquote:
		h.withBg(nodeBg, func() {
			h.block("quote", 0, func() { h.children(n, style) })
		})
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
		h.withBg(nodeBg, func() {
			h.children(n, style)
		})
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
		if t := tableOfWithOptions(n, false, h.classes); t != nil {
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
	return tableOfWithOptions(n, false, nil)
}

func tableOfMode(n *html.Node, exactWhitespace bool) *document.Block {
	return tableOfWithOptions(n, exactWhitespace, nil)
}

func tableOfWithOptions(n *html.Node, exactWhitespace bool, classes map[string]map[string]string) *document.Block {
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
				if exactWhitespace {
					for col := c.FirstChild; col != nil; col = col.NextSibling {
						if col.Type == html.ElementNode && col.DataAtom == atom.Col {
							colWidths = append(colWidths, parseHTMLWidth(col))
						}
					}
				}
			case atom.Col:
				if exactWhitespace {
					colWidths = append(colWidths, parseHTMLWidth(c))
				}
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
			var cellProps map[string]string
			if exactWhitespace {
				runs, comments := parseInlineHTMLNodesExact(td, document.Style{})
				if len(runs) > 0 {
					cell.Runs = runs
				}
				if len(comments) > 0 {
					cell.Comments = comments
				}
			} else {
				cellProps = mergedNodeCSSProps(td, classes)
				cellStyle := applyCSSPropsStyle(cellProps, attr(td, "data-mark") != "", false, document.Style{})
				inner := &htmlImporter{kind: "paragraph", classes: classes, inTableCell: true}
				inner.children(td, cellStyle)
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
			if align := parseCellAlign(td, cellProps); align != "" {
				cell.Align = align
			}
			if bg := parseCellBackgroundProps(td, cellProps); bg != "" {
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
	if exactWhitespace && len(colWidths) == numCols {
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
	if !exactWhitespace {
		normalizeSimpleTableAlignment(table)
	}
	return table
}

// normalizeSimpleTableAlignment propagates a uniform body-column alignment
// (such as Excel's automatic align=right on numeric columns) to an unaligned
// header and empty cells when the table otherwise fits a Markdown pipe table.
func normalizeSimpleTableAlignment(table *document.Block) {
	if len(table.Children) < 2 {
		return
	}
	cols := len(table.Children[0].Children)
	for _, row := range table.Children {
		for _, cell := range row.Children {
			if cell.ColSpan > 1 || cell.RowSpan > 1 || cell.Background != "" {
				return
			}
		}
	}
	for c := 0; c < cols; c++ {
		if table.Children[0].Children[c].Align != "" {
			continue
		}
		colAlign := ""
		uniform := true
		for r := 1; r < len(table.Children); r++ {
			cell := table.Children[r].Children[c]
			if strings.TrimSpace(cell.Text()) == "" && cell.Align == "" {
				continue
			}
			if cell.Align == "" {
				uniform = false
				break
			}
			if colAlign == "" {
				colAlign = cell.Align
			} else if colAlign != cell.Align {
				uniform = false
				break
			}
		}
		if uniform && colAlign != "" {
			table.Children[0].Children[c].Align = colAlign
			for r := 1; r < len(table.Children); r++ {
				if table.Children[r].Children[c].Align == "" {
					table.Children[r].Children[c].Align = colAlign
				}
			}
		}
	}
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

func parseCellAlign(n *html.Node, props map[string]string) string {
	if len(props) > 0 {
		switch cleanCSSValue(props["text-align"]) {
		case "center", "right":
			return cleanCSSValue(props["text-align"])
		case "left":
			return ""
		}
	}
	return parseBlockAlign(n)
}

func parseCellBackground(n *html.Node) string {
	return parseCellBackgroundProps(n, parseCSSProps(attr(n, "style")))
}

func parseCellBackgroundProps(n *html.Node, props map[string]string) string {
	if bg := attr(n, "data-bg"); validStyleName(bg, document.TextHighlights) {
		return bg
	}
	if props == nil {
		props = parseCSSProps(attr(n, "style"))
	}
	raw := props["background-color"]
	if raw == "" {
		raw = props["background"]
	}
	if raw == "" {
		raw = attr(n, "bgcolor")
	}
	return matchBackgroundCSS(raw)
}
