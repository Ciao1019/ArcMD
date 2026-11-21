package editor

import (
	"fmt"
	"html"
	"math"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/wiki"
	"github.com/egoist/mygo/ui/markdown/parser"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type importedChunk struct {
	from, to int
	raw      string
}

type parsedMarkdown struct {
	doc            *document.Document
	rawFrontmatter string
	chunks         []importedChunk
}

// ImportMarkdown adapts Markdown (with GFM, TeX math, YAML frontmatter and
// semantic HTML extensions) to the native document tree. The original text is
// retained in ImportSource so unedited blocks preserve their author formatting.
func ImportMarkdown(source string) *document.Document {
	return parseMarkdownSourceMode(source, false).doc
}

// ExtractMarkdownText extracts plain block text and the page icon for search
// indexing without allocating unique block IDs, source chunks, or keeping ImportSource.
func ExtractMarkdownText(source string) (text, icon string) {
	doc := parseMarkdownSourceMode(source, true).doc
	return doc.PlainText(), doc.Icon
}

type rawSpan struct {
	block *document.Block
	start int
	end   int
}

const arcmdMarkdownExtensions = (parser.Default &^ (parser.ExtAlerts | parser.ExtMentions | parser.ExtReferences)) | parser.ExtDollarMath

func parseMarkdownSource(source string) parsedMarkdown {
	return parseMarkdownSourceMode(source, false)
}

func parseMarkdownSourceMode(source string, forSearch bool) parsedMarkdown {
	result := document.New()
	result.Blocks = nil
	if !forSearch {
		result.ImportSource = source
	}

	text, rawFM, fm, icon, cover, coverY, goal := splitFrontmatter(source)
	result.Frontmatter = fm
	result.Icon = icon
	result.Cover = cover
	if cover != "" {
		result.CoverY = coverY
	}
	result.Goal = goal

	parsed := parser.New(arcmdMarkdownExtensions)
	parsed.Set(text)

	nextID := func() string {
		if forSearch {
			return ""
		}
		return document.NewID()
	}

	var topSpans []rawSpan
	var tailItem document.ListPlacement
	explicitLeft := make(map[*document.Block]bool)

	var blocks func(int, string, int) []*document.Block
	blocks = func(parent int, context string, indent int) []*document.Block {
		var out []*document.Block
		listEnd := -1
		for index := range parsed.Children(parent) {
			node := parsed.Block(index)
			startCount := len(out)
			previousListEnd := listEnd
			listEnd = -1
			b := &document.Block{ID: nextID(), Kind: "paragraph", Indent: indent}
			switch node.Kind {
			case parser.Paragraph, parser.Heading:
				in := parsed.Inlines(index)
				if node.Kind == parser.Paragraph && context == "" {
					if img, ok := tryImportInlineImageBlock(in); ok && indent == 0 {
						out = append(out, img)
						if !forSearch && parent == 0 {
							topSpans = append(topSpans, rawSpan{img, node.Start, node.End})
						}
						continue
					}
					if split := splitObsidianImageParagraph(text, node.Start, node.End, in, indent, nextID, explicitLeft); len(split) > 0 {
						out = append(out, split...)
						if !forSearch && parent == 0 && len(out) > startCount {
							for k := startCount; k < len(out); k++ {
								s, e := -1, -1
								if k == startCount {
									s = node.Start
								}
								if k == len(out)-1 {
									e = node.End
								}
								topSpans = append(topSpans, rawSpan{out[k], s, e})
							}
						}
						continue
					}
				}
				b.Runs, b.Comments = importRunsAndComments(in, document.Style{})
				if context != "" {
					b.Kind = context
				}
				if node.Kind == parser.Heading {
					b.Kind, b.Level = "heading", node.Level
				}
				if len(in) > 0 && in[0].Kind == parser.Image && in[0].Next == len(in) {
					alt, width := parseMarkdownImageAlt(parser.PlainText(in[1:]))
					b.Kind, b.URL = "image", in[0].Dest
					b.ImageWidth = width
					b.Runs = []document.Run{{Text: alt}}
					b.Comments = nil
				}
			case parser.Quote:
				children := blocks(index, "quote", indent)
				if color, emoji, align, ok := alertMetadata(children); ok {
					for _, child := range children {
						child.Kind, child.Color, child.Emoji = "callout", color, emoji
						if align != "" {
							child.Align = align
						}
					}
					if len(children) > 1 && children[0].Text() == "" {
						if len(children[0].Comments) > 0 {
							children[1].Comments = document.MergeComments(children[0].Comments, 0, children[1].Comments)
						}
						children = children[1:]
					}
				}
				out = append(out, children...)
				if !forSearch && parent == 0 && len(out) > startCount {
					if resolvedEnd, ok := resolveContainerSpanEnd(parsed, index, node.End); ok {
						for k := startCount; k < len(out); k++ {
							s, e := -1, -1
							if k == startCount {
								s = node.Start
							}
							if k == len(out)-1 {
								e = resolvedEnd
							}
							topSpans = append(topSpans, rawSpan{out[k], s, e})
						}
					}
				}
				continue
			case parser.List:
				kind := "bullet"
				if node.Ordered {
					kind = "numbered"
				}
				for item := range parsed.Children(index) {
					it := parsed.Block(item)
					itemKind := kind
					if it.Task != parser.NoTask {
						itemKind = "task"
					}
					depth := indent
					if parsed.Block(parent).Kind == parser.Item {
						depth++
					}
					d := min(12, depth)
					// The last source item can be empty and therefore absent from out.
					tailItem = document.ListPlacement{Depth: d, Item: it.Next == item+1}
					children := blocks(item, itemKind, d)
					if !forSearch && len(children) > 0 {
						if !isList(children[0].Kind) && children[0].Indent == d && children[0].MapList == nil {
							children[0].MapList = &document.ListPlacement{Depth: d, Item: true}
						}
						for _, child := range children[1:] {
							if child.Indent == d && child.MapList == nil {
								child.MapList = &document.ListPlacement{Depth: d}
							}
						}
					}
					for _, child := range children {
						if child.Kind == "task" && child.Indent == d {
							child.Checked = it.Task == parser.Checked
						}
					}
					out = append(out, children...)
				}
				listEnd = node.End
				if !forSearch && parent == 0 && len(out) > startCount {
					if resolvedEnd, ok := resolveContainerSpanEnd(parsed, index, node.End); ok {
						for k := startCount; k < len(out); k++ {
							s, e := -1, -1
							if k == startCount {
								s = node.Start
							}
							if k == len(out)-1 {
								e = resolvedEnd
							}
							topSpans = append(topSpans, rawSpan{out[k], s, e})
						}
					}
				}
				continue
			case parser.FootnoteDefinition:
				b.Kind, b.NoteLabel = "footnote", node.Label()
				u16Len := 0
				for i, child := range blocks(index, "", 0) {
					if i > 0 {
						b.Runs = append(b.Runs, document.Run{Text: "\u2028"})
						u16Len++
					}
					if len(child.Comments) > 0 {
						b.Comments = document.MergeComments(b.Comments, u16Len, child.Comments)
					}
					b.Runs = append(b.Runs, child.Runs...)
					u16Len += runsU16Len(child.Runs)
				}
			case parser.CodeBlock:
				if !forSearch && node.Fenced && previousListEnd >= 0 && previousListEnd <= node.Start &&
					strings.Trim(text[previousListEnd:node.Start], " \t\r") == "\n" {
					placement := tailItem
					b.MapList = &placement
				}
				b.Kind, b.Language = "code", node.Language()
				b.Runs = []document.Run{{Text: strings.TrimSuffix(parsed.Lines(index), "\n")}}
			case parser.ThematicBreak:
				b.Kind = "rule"
				b.Runs = []document.Run{{Text: ""}}
			case parser.Table:
				b.Kind, b.Runs = "table", nil
				for row := range parsed.Children(index) {
					tr := &document.Block{ID: nextID(), Kind: "row"}
					col := 0
					for cell := range parsed.Children(row) {
						runs, comments := importRunsAndCommentsWithBreak(parsed.Inlines(cell), document.Style{}, "\n")
						td := &document.Block{
							ID:       nextID(),
							Kind:     "cell",
							Runs:     runs,
							Comments: comments,
						}
						if col < len(node.Align) {
							switch node.Align[col] {
							case parser.AlignCenter:
								td.Align = "center"
							case parser.AlignRight:
								td.Align = "right"
							}
						}
						tr.Children = append(tr.Children, td)
						col++
					}
					b.Children = append(b.Children, tr)
				}
			case parser.MathBlock:
				b.Kind = "math"
				b.Runs = []document.Run{{Text: strings.TrimSpace(parsed.Lines(index))}}
			default:
				raw := text[node.Start:node.End]
				if hbs, ok := tryImportHTMLBlocks(raw, indent, explicitLeft); ok && len(hbs) > 0 {
					if context != "" && len(hbs) == 1 && hbs[0].Kind == "paragraph" && strings.HasPrefix(strings.TrimSpace(raw), "<!--") {
						hbs[0].Kind = context
					}
					out = append(out, hbs...)
					if !forSearch && parent == 0 {
						if len(hbs) == 1 {
							topSpans = append(topSpans, rawSpan{hbs[0], node.Start, node.End})
						} else {
							topSpans = append(topSpans, rawSpan{hbs[0], node.Start, -1})
							for _, mid := range hbs[1 : len(hbs)-1] {
								topSpans = append(topSpans, rawSpan{mid, -1, -1})
							}
							topSpans = append(topSpans, rawSpan{hbs[len(hbs)-1], -1, node.End})
						}
					}
					continue
				}
				b.Kind = "raw"
				b.Runs = []document.Run{{Text: raw}}
			}
			out = append(out, b)
			if !forSearch && parent == 0 {
				endOff := node.End
				includeSpan := true
				switch node.Kind {
				case parser.Heading:
					if node.Start < len(text) && text[node.Start] != '#' {
						endOff = extendSetextUnderline(text, endOff)
					}
				case parser.MathBlock:
					if node.Open {
						includeSpan = false
					} else if extEnd, ok := extendMathBlockEnd(text, node.Start, endOff); ok {
						endOff = extEnd
					} else {
						includeSpan = false
					}
				case parser.CodeBlock:
					if node.Fenced && node.Open {
						includeSpan = false
					}
				}
				if includeSpan {
					topSpans = append(topSpans, rawSpan{b, node.Start, endOff})
				}
			}
		}
		return out
	}

	rawBlocks := blocks(0, "", 0)
	structured, chunks := buildStructuredBlocks(rawBlocks, topSpans, text, forSearch, explicitLeft)
	result.Blocks = structured
	if len(result.Blocks) == 0 {
		result.Blocks = []*document.Block{document.NewBlock("paragraph", strings.TrimRight(text, "\r\n"))}
		chunks = nil
	}
	return parsedMarkdown{doc: result, rawFrontmatter: rawFM, chunks: chunks}
}

// buildStructuredBlocks resolves multi-block HTML wrappers (columns, details,
// aligned containers) while tracking source byte ranges for top-level chunks.
func buildStructuredBlocks(blocks []*document.Block, spans []rawSpan, text string, forSearch bool, explicitLeft map[*document.Block]bool) ([]*document.Block, []importedChunk) {
	var spanStart, spanEnd map[*document.Block]int
	if !forSearch {
		spanStart = make(map[*document.Block]int, len(spans))
		spanEnd = make(map[*document.Block]int, len(spans))
		for _, s := range spans {
			spanStart[s.block] = s.start
			spanEnd[s.block] = s.end
		}
	}

	blocks = importColumns(blocks, spanStart, spanEnd, explicitLeft)
	blocks = importDetails(blocks, spanStart, spanEnd)
	blocks = importAlignedWrappers(blocks, spanStart, spanEnd, explicitLeft)

	if forSearch {
		return blocks, nil
	}
	var chunks []importedChunk
	for i := 0; i < len(blocks); {
		s, ok := spanStart[blocks[i]]
		if !ok || s < 0 {
			i++
			continue
		}
		j := i
		e := spanEnd[blocks[i]]
		for e < 0 && j+1 < len(blocks) {
			j++
			e = spanEnd[blocks[j]]
		}
		// A toggle's span extends through its indented body blocks.
		if blocks[i].Kind == "toggle" {
			for j+1 < len(blocks) && blocks[j+1].Kind != "heading" && blocks[j+1].Indent > blocks[i].Indent {
				j++
				if endJ := spanEnd[blocks[j]]; endJ > e {
					e = endJ
				}
			}
		}
		if e >= s && e <= len(text) {
			raw := strings.TrimRight(text[s:e], "\r\n")
			if raw != "" {
				chunks = append(chunks, importedChunk{from: i, to: j + 1, raw: raw})
			}
		}
		i = j + 1
	}
	return blocks, chunks
}

func splitFrontmatter(source string) (body, rawFM, userFM, icon, cover string, coverY float64, goal int) {
	if !strings.HasPrefix(source, "---\n") && !strings.HasPrefix(source, "---\r\n") {
		return source, "", "", "", "", 0, 0
	}
	lines := strings.SplitAfter(source, "\n")
	end := len(lines[0])
	closed := false
	var fmLines []string
	for _, line := range lines[1:] {
		end += len(line)
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" || trimmed == "..." {
			closed = true
			break
		}
		fmLines = append(fmLines, strings.TrimRight(line, "\r\n"))
	}
	if !closed {
		return source, "", "", "", "", 0, 0
	}
	rawFM = strings.TrimRight(source[:end], "\r\n")
	body = source[end:]
	// Strip up to one leading blank line separating frontmatter from the body.
	if strings.HasPrefix(body, "\r\n") {
		body = body[2:]
	} else if strings.HasPrefix(body, "\n") {
		body = body[1:]
	}

	var kept []string
	var parsedCoverY float64
	for _, line := range fmLines {
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			if k, v, ok := strings.Cut(line, ":"); ok {
				key := strings.TrimSpace(k)
				val := unquoteYAMLScalar(strings.TrimSpace(v))
				switch key {
				case "icon":
					if val != "" && len(val) <= 32 && !strings.ContainsAny(val, "\n\r") {
						icon = val
						continue
					}
				case "cover":
					if val != "" && document.ValidCover(val) {
						cover = val
						continue
					}
				case "coverY":
					if y, err := strconv.ParseFloat(val, 64); err == nil && y >= -1 && y <= 1 {
						parsedCoverY = y
						continue
					}
				case "goal":
					if g, err := strconv.Atoi(val); err == nil && g > 0 && g <= 1000000 {
						goal = g
						continue
					}
				}
			}
		}
		kept = append(kept, line)
	}
	if cover != "" {
		coverY = parsedCoverY
	}
	userFM = strings.TrimRight(strings.Join(kept, "\n"), "\r\n")
	return body, rawFM, userFM, icon, cover, coverY, goal
}

func unquoteYAMLScalar(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	return s
}

func formatYAMLScalar(s string) string {
	if s == "" {
		return `""`
	}
	needsQuote := strings.TrimSpace(s) != s ||
		strings.ContainsAny(s, "\n\r\t#\"'") ||
		strings.Contains(s, ": ") ||
		strings.HasPrefix(s, "[") || strings.HasPrefix(s, "{") ||
		strings.HasPrefix(s, "&") || strings.HasPrefix(s, "*") ||
		strings.HasPrefix(s, "!") || strings.HasPrefix(s, "%") ||
		strings.HasPrefix(s, "@") || strings.HasPrefix(s, "`")
	if needsQuote {
		return strconv.Quote(s)
	}
	return s
}

func exportFrontmatter(doc *document.Document) string {
	var lines []string
	if doc.Icon != "" {
		lines = append(lines, "icon: "+formatYAMLScalar(doc.Icon))
	}
	if doc.Cover != "" {
		lines = append(lines, "cover: "+formatYAMLScalar(doc.Cover))
		if doc.CoverY != 0 {
			lines = append(lines, "coverY: "+strconv.FormatFloat(doc.CoverY, 'f', -1, 64))
		}
	}
	if doc.Goal > 0 {
		lines = append(lines, "goal: "+strconv.Itoa(doc.Goal))
	}
	if strings.TrimSpace(doc.Frontmatter) != "" {
		lines = append(lines, strings.TrimRight(doc.Frontmatter, "\r\n"))
	}
	if len(lines) == 0 {
		return ""
	}
	return "---\n" + strings.Join(lines, "\n") + "\n---"
}

func parseMarkdownImageAlt(alt string) (string, float32) {
	idx := strings.LastIndexByte(alt, '|')
	if idx < 0 {
		return alt, 0
	}
	wStr := strings.TrimSpace(alt[idx+1:])
	if wStr == "" {
		return alt, 0
	}
	w, err := strconv.Atoi(wStr)
	if err != nil || w <= 0 || w > 10000 {
		return alt, 0
	}
	return alt[:idx], float32(w)
}

func tryImportInlineImageBlock(in []parser.Inline) (*document.Block, bool) {
	if len(in) == 0 || in[0].Kind != parser.HTML || in[0].Next != len(in) {
		return nil, false
	}
	return parseSingleImgTag(in[0].Text, "")
}

func overlapsProtectedInline(in []parser.Inline, start, end int) bool {
	for _, item := range in {
		switch item.Kind {
		case parser.Code, parser.Math, parser.HTML:
			if item.End > start && item.Start < end {
				return true
			}
		}
	}
	return false
}

func isObsidianImageExt(target string) bool {
	if isWhiteboardURL(target) || IsPDFURL(target) {
		return true
	}
	ext := strings.ToLower(path.Ext(target))
	base := path.Base(target)
	if base == "" || base == "." || base == ".." || len(base) <= len(ext) {
		return false
	}
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg", ".avif", ".ico", ".tif", ".tiff":
		return true
	default:
		return false
	}
}

func parseObsidianSizeSpec(s string) (float32, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if idx := strings.IndexAny(s, "xX×"); idx >= 0 {
		wStr := strings.TrimSpace(s[:idx])
		_, sepLen := utf8.DecodeRuneInString(s[idx:])
		hStr := strings.TrimSpace(s[idx+sepLen:])
		w, wErr := strconv.Atoi(wStr)
		h, hErr := strconv.Atoi(hStr)
		if wErr != nil || hErr != nil || w <= 0 || w > 10000 || h <= 0 || h > 10000 {
			return 0, false
		}
		return float32(w), true
	}
	w, err := strconv.Atoi(s)
	if err != nil || w <= 0 || w > 10000 {
		return 0, false
	}
	return float32(w), true
}

func parseObsidianImageEmbed(line string) (url, alt string, width float32, ok bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "![[") || !strings.HasSuffix(s, "]]") {
		return "", "", 0, false
	}
	inner := s[3 : len(s)-2]
	if inner == "" || strings.ContainsAny(inner, "[]\r\n") {
		return "", "", 0, false
	}
	parts := strings.Split(inner, "|")
	target, frag, hasFrag := strings.Cut(parts[0], "#")
	target = strings.TrimSpace(target)
	if target == "" || strings.ContainsAny(target, ":\x00") {
		return "", "", 0, false
	}
	target = strings.ReplaceAll(target, `\`, "/")
	target = strings.TrimLeft(target, "/")
	if target == "" || !isObsidianImageExt(target) {
		return "", "", 0, false
	}
	if hasFrag && IsPDFURL(target) {
		frag = strings.TrimSpace(frag)
		if frag != "" {
			target = target + "#" + frag
		}
	}
	rest := parts[1:]
	if len(rest) > 0 {
		if w, isSize := parseObsidianSizeSpec(rest[len(rest)-1]); isSize {
			width = w
			rest = rest[:len(rest)-1]
		}
	}
	alt = strings.TrimSpace(strings.Join(rest, "|"))
	return target, alt, width, true
}

func splitObsidianImageParagraph(text string, start, end int, in []parser.Inline, indent int, nextID func() string, explicitLeft map[*document.Block]bool) []*document.Block {
	raw := strings.TrimRight(text[start:end], "\r\n")
	if !strings.Contains(raw, "![[") {
		return nil
	}
	var blocks []*document.Block
	var prose []string
	matched := false
	flushProse := func() {
		if len(prose) == 0 {
			return
		}
		snippet := strings.Join(prose, "\n")
		prose = prose[:0]
		sub := parser.New(arcmdMarkdownExtensions)
		sub.Set(snippet)
		for cIdx := range sub.Children(0) {
			cNode := sub.Block(cIdx)
			switch cNode.Kind {
			case parser.Paragraph, parser.Heading:
				cIn := sub.Inlines(cIdx)
				if cNode.Kind == parser.Paragraph {
					if img, ok := tryImportInlineImageBlock(cIn); ok && indent == 0 {
						blocks = append(blocks, img)
						continue
					}
					if len(cIn) > 0 && cIn[0].Kind == parser.Image && cIn[0].Next == len(cIn) {
						alt, width := parseMarkdownImageAlt(parser.PlainText(cIn[1:]))
						blocks = append(blocks, &document.Block{
							ID:         nextID(),
							Kind:       "image",
							Indent:     indent,
							URL:        cIn[0].Dest,
							ImageWidth: width,
							Runs:       []document.Run{{Text: alt}},
						})
						continue
					}
				}
				runs, comments := importRunsAndComments(cIn, document.Style{})
				b := &document.Block{
					ID:       nextID(),
					Kind:     "paragraph",
					Indent:   indent,
					Runs:     runs,
					Comments: comments,
				}
				if cNode.Kind == parser.Heading {
					b.Kind, b.Level = "heading", cNode.Level
				}
				blocks = append(blocks, b)
			case parser.ThematicBreak:
				blocks = append(blocks, &document.Block{ID: nextID(), Kind: "rule", Indent: indent, Runs: []document.Run{{Text: ""}}})
			case parser.MathBlock:
				blocks = append(blocks, &document.Block{ID: nextID(), Kind: "math", Indent: indent, Runs: []document.Run{{Text: strings.TrimSpace(sub.Lines(cIdx))}}})
			default:
				rawChunk := snippet[cNode.Start:cNode.End]
				if hbs, ok := tryImportHTMLBlocks(rawChunk, indent, explicitLeft); ok && len(hbs) > 0 {
					blocks = append(blocks, hbs...)
				} else {
					blocks = append(blocks, &document.Block{ID: nextID(), Kind: "raw", Indent: indent, Runs: []document.Run{{Text: rawChunk}}})
				}
			}
		}
	}

	for pos := 0; pos <= len(raw); {
		nl := strings.IndexByte(raw[pos:], '\n')
		var line string
		nextPos := len(raw) + 1
		if nl >= 0 {
			line = raw[pos : pos+nl]
			nextPos = pos + nl + 1
		} else {
			line = raw[pos:]
		}
		trimmedRight := strings.TrimSuffix(line, "\r")
		absStart, absEnd := start+pos, start+pos+len(trimmedRight)
		if !overlapsProtectedInline(in, absStart, absEnd) {
			if imgURL, alt, width, ok := parseObsidianImageEmbed(trimmedRight); ok {
				flushProse()
				matched = true
				blocks = append(blocks, &document.Block{
					ID:         nextID(),
					Kind:       "image",
					Indent:     indent,
					URL:        imgURL,
					ImageWidth: width,
					Runs:       []document.Run{{Text: alt}},
				})
				pos = nextPos
				continue
			}
		}
		prose = append(prose, trimmedRight)
		pos = nextPos
		if nl < 0 {
			break
		}
	}
	if !matched {
		return nil
	}
	flushProse()
	return blocks
}

func parseSingleImgTag(raw, align string) (*document.Block, bool) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(trimmed), "<img") {
		return nil, false
	}
	ctx := &xhtml.Node{Type: xhtml.ElementNode, DataAtom: atom.Div, Data: "div"}
	nodes, err := xhtml.ParseFragment(strings.NewReader(trimmed), ctx)
	if err != nil || len(nodes) != 1 || nodes[0].Type != xhtml.ElementNode || nodes[0].DataAtom != atom.Img {
		return nil, false
	}
	img := nodes[0]
	src := strings.TrimSpace(attr(img, "src"))
	if src == "" || strings.HasPrefix(strings.ToLower(src), "data:") {
		return nil, false
	}
	b := document.NewBlock("image", attr(img, "alt"))
	b.URL = src
	b.ImageWidth = parseHTMLWidth(img)
	if align != "" {
		b.Align = align
	} else if a := parseBlockAlign(img); a != "" {
		b.Align = a
	}
	return b, true
}

// tryImportLeadingCommentParagraph converts a CommonMark type-2 HTML block
// consisting of one or more leading comments followed by single-line inline
// prose (such as "<!-- note -->Visible") into a paragraph with preserved hidden
// inline comments, while leaving standalone comment blocks and block/media HTML
// untouched as raw blocks.
func tryImportLeadingCommentParagraph(trimmed string, baseIndent int) (*document.Block, bool) {
	if !strings.HasPrefix(trimmed, "<!--") || isHTMLCommentBlock(trimmed) {
		return nil, false
	}
	rest := trimmed
	for strings.HasPrefix(rest, "<!--") {
		end := strings.Index(rest[4:], "-->")
		if end < 0 {
			return nil, false
		}
		rest = strings.TrimLeft(rest[4+end+3:], " \t")
	}
	if rest == "" || strings.ContainsAny(rest, "\r\n") {
		return nil, false
	}
	ctx := &xhtml.Node{Type: xhtml.ElementNode, DataAtom: atom.Div, Data: "div"}
	nodes, err := xhtml.ParseFragment(strings.NewReader(rest), ctx)
	if err != nil {
		return nil, false
	}
	wrap := &xhtml.Node{Type: xhtml.ElementNode, DataAtom: atom.Div, Data: "div"}
	for _, n := range nodes {
		wrap.AppendChild(n)
	}
	if hasBlockElementChild(wrap) || hasUnsupportedInlineMedia(wrap) {
		return nil, false
	}
	sub := parser.New(parser.Default | parser.ExtDollarMath)
	sub.Set("<span></span>" + trimmed)
	paraIdx := -1
	count := 0
	for idx := range sub.Children(0) {
		count++
		if sub.Block(idx).Kind == parser.Paragraph {
			paraIdx = idx
		}
	}
	if count != 1 || paraIdx < 0 {
		return nil, false
	}
	runs, comments := importRunsAndComments(sub.Inlines(paraIdx), document.Style{})
	if len(comments) == 0 {
		return nil, false
	}
	b := document.NewBlock("paragraph", "")
	b.Indent = baseIndent
	b.Runs = runs
	b.Comments = comments
	if len(b.Runs) == 0 {
		b.Runs = []document.Run{{Text: ""}}
	}
	return b, true
}

func tryImportHTMLBlocks(raw string, baseIndent int, explicitLeft map[*document.Block]bool) ([]*document.Block, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, false
	}
	// Structural multi-block wrappers are handled by importColumns / importDetails / importAlignedWrappers.
	if detailsOpen.MatchString(trimmed) || detailsClose.MatchString(trimmed) ||
		columnsOpenRe.MatchString(trimmed) || columnOpenRe.MatchString(trimmed) || divCloseRe.MatchString(trimmed) ||
		centerCloseRe.MatchString(trimmed) {
		return nil, false
	}
	if _, _, ok := parseAlignedWrapperOpen(trimmed); ok {
		return nil, false
	}
	if b, ok := parseSingleImgTag(trimmed, ""); ok {
		b.Indent = baseIndent
		return []*document.Block{b}, true
	}
	if b, ok := tryImportLeadingCommentParagraph(trimmed, baseIndent); ok {
		return []*document.Block{b}, true
	}

	ctx := &xhtml.Node{Type: xhtml.ElementNode, DataAtom: atom.Body, Data: "body"}
	nodes, err := xhtml.ParseFragment(strings.NewReader(trimmed), ctx)
	if err != nil {
		return nil, false
	}
	var elems []*xhtml.Node
	for _, n := range nodes {
		if n.Type == xhtml.TextNode && strings.TrimSpace(n.Data) == "" {
			continue
		}
		if n.Type != xhtml.ElementNode {
			return nil, false
		}
		elems = append(elems, n)
	}
	if len(elems) != 1 {
		return nil, false
	}
	el := elems[0]
	switch el.DataAtom {
	case atom.Hr:
		b := document.NewBlock("rule", "")
		b.Indent = baseIndent
		return []*document.Block{b}, true
	case atom.Table:
		if tb := tableOfMode(el, true); tb != nil {
			tb.Indent = baseIndent
			return []*document.Block{tb}, true
		}
	case atom.Div:
		if attr(el, "class") != "" {
			return nil, false
		}
		align := parseBlockAlign(el)
		if align == "" {
			return nil, false
		}
		return importAlignedHTMLContainer(el, align, baseIndent, explicitLeft)
	case atom.Center:
		return importAlignedHTMLContainer(el, "center", baseIndent, explicitLeft)
	case atom.P, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		align, explicit := parseBlockAlignExplicit(el)
		ind, hasInd := parseBlockIndent(el)
		if img := singleChildImg(el); img != nil && el.DataAtom == atom.P {
			if b, ok := imgNodeToBlock(img, align); ok {
				b.Indent = min(12, baseIndent+ind)
				if explicit && align == "" && explicitLeft != nil {
					explicitLeft[b] = true
				}
				return []*document.Block{b}, true
			}
		}
		if !explicit && !hasInd {
			return nil, false
		}
		if hasBlockElementChild(el) || hasUnsupportedInlineMedia(el) {
			return nil, false
		}
		kind, level := "paragraph", 0
		if el.DataAtom != atom.P {
			kind = "heading"
			level = int(el.Data[1] - '0')
		}
		b := document.NewBlock(kind, "")
		b.Level = level
		b.Align = align
		b.Indent = min(12, baseIndent+ind)
		b.Runs, b.Comments = parseInlineHTMLNodesWithBreak(el, document.Style{}, "\u2028")
		if len(b.Runs) == 0 {
			b.Runs = []document.Run{{Text: ""}}
		}
		if explicit && align == "" && explicitLeft != nil {
			explicitLeft[b] = true
		}
		return []*document.Block{b}, true
	}
	return nil, false
}

func importAlignedHTMLContainer(el *xhtml.Node, align string, baseIndent int, explicitLeft map[*document.Block]bool) ([]*document.Block, bool) {
	if img := singleChildImg(el); img != nil {
		if b, ok := imgNodeToBlock(img, align); ok {
			b.Indent = baseIndent
			if align == "" && explicitLeft != nil {
				explicitLeft[b] = true
			}
			return []*document.Block{b}, true
		}
	}
	if !hasBlockElementChild(el) {
		if hasUnsupportedInlineMedia(el) {
			return nil, false
		}
		b := document.NewBlock("paragraph", "")
		b.Align = align
		b.Indent = baseIndent
		b.Runs, b.Comments = parseInlineHTMLNodesWithBreak(el, document.Style{}, "\u2028")
		if len(b.Runs) == 0 {
			b.Runs = []document.Run{{Text: ""}}
		}
		if align == "" && explicitLeft != nil {
			explicitLeft[b] = true
		}
		return []*document.Block{b}, true
	}
	var out []*document.Block
	var inlineBuf []*xhtml.Node
	flushInline := func() {
		if len(inlineBuf) == 0 {
			return
		}
		runs, comments := parseInlineHTMLNodeSlice(inlineBuf, document.Style{}, "\u2028", false)
		inlineBuf = nil
		if len(runs) == 0 && len(comments) == 0 {
			return
		}
		if len(runs) == 0 {
			runs = []document.Run{{Text: ""}}
		}
		b := document.NewBlock("paragraph", "")
		b.Align = align
		b.Indent = baseIndent
		b.Runs = runs
		b.Comments = comments
		if align == "" && explicitLeft != nil {
			explicitLeft[b] = true
		}
		out = append(out, b)
	}
	for c := el.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case xhtml.TextNode:
			if strings.TrimSpace(c.Data) == "" && len(inlineBuf) == 0 {
				continue
			}
			inlineBuf = append(inlineBuf, c)
		case xhtml.CommentNode:
			if len(inlineBuf) == 0 {
				out = append(out, document.NewBlock("raw", "<!--"+c.Data+"-->"))
			} else {
				inlineBuf = append(inlineBuf, c)
			}
		case xhtml.ElementNode:
			switch c.DataAtom {
			case atom.Hr:
				flushInline()
				b := document.NewBlock("rule", "")
				b.Indent = baseIndent
				out = append(out, b)
			case atom.Img:
				flushInline()
				b, ok := imgNodeToBlock(c, align)
				if !ok {
					return nil, false
				}
				b.Indent = baseIndent
				if align == "" && explicitLeft != nil {
					explicitLeft[b] = true
				}
				out = append(out, b)
			case atom.P, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
				flushInline()
				if hasBlockElementChild(c) {
					return nil, false
				}
				childAlign, explicit := parseBlockAlignExplicit(c)
				if !explicit {
					childAlign = align
				}
				ind, _ := parseBlockIndent(c)
				if c.DataAtom == atom.P {
					if img := singleChildImg(c); img != nil {
						if b, ok := imgNodeToBlock(img, childAlign); ok {
							b.Indent = min(12, baseIndent+ind)
							if childAlign == "" && explicitLeft != nil {
								explicitLeft[b] = true
							}
							out = append(out, b)
							continue
						}
					}
				}
				if hasUnsupportedInlineMedia(c) {
					return nil, false
				}
				kind, level := "paragraph", 0
				if c.DataAtom != atom.P {
					kind = "heading"
					level = int(c.Data[1] - '0')
				}
				b := document.NewBlock(kind, "")
				b.Level = level
				b.Align = childAlign
				b.Indent = min(12, baseIndent+ind)
				b.Runs, b.Comments = parseInlineHTMLNodesWithBreak(c, document.Style{}, "\u2028")
				if len(b.Runs) == 0 {
					b.Runs = []document.Run{{Text: ""}}
				}
				if childAlign == "" && explicitLeft != nil {
					explicitLeft[b] = true
				}
				out = append(out, b)
			case atom.Div:
				if attr(c, "class") != "" {
					return nil, false
				}
				flushInline()
				childAlign, explicit := parseBlockAlignExplicit(c)
				if !explicit {
					childAlign = align
				}
				sub, ok := importAlignedHTMLContainer(c, childAlign, baseIndent, explicitLeft)
				if !ok {
					return nil, false
				}
				out = append(out, sub...)
			case atom.Center:
				flushInline()
				sub, ok := importAlignedHTMLContainer(c, "center", baseIndent, explicitLeft)
				if !ok {
					return nil, false
				}
				out = append(out, sub...)
			case atom.Table:
				flushInline()
				tb := tableOfMode(c, true)
				if tb == nil {
					return nil, false
				}
				tb.Indent = baseIndent
				out = append(out, tb)
			case atom.Span, atom.Mark, atom.Font,
				atom.B, atom.Strong, atom.I, atom.Em, atom.Cite, atom.Var, atom.Dfn,
				atom.U, atom.Ins, atom.S, atom.Del, atom.Strike,
				atom.Sup, atom.Sub, atom.Code, atom.Kbd, atom.Samp, atom.Tt,
				atom.A, atom.Small, atom.Abbr, atom.Q, atom.Br:
				if hasBlockElementChild(c) || hasUnsupportedInlineMedia(c) {
					return nil, false
				}
				inlineBuf = append(inlineBuf, c)
			default:
				return nil, false
			}
		}
	}
	flushInline()
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func singleChildImg(el *xhtml.Node) *xhtml.Node {
	var found *xhtml.Node
	for c := el.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.TextNode && strings.TrimSpace(c.Data) == "" {
			continue
		}
		if c.Type == xhtml.ElementNode && c.DataAtom == atom.Img && found == nil {
			found = c
			continue
		}
		return nil
	}
	return found
}

func imgNodeToBlock(img *xhtml.Node, align string) (*document.Block, bool) {
	src := strings.TrimSpace(attr(img, "src"))
	if src == "" || strings.HasPrefix(strings.ToLower(src), "data:") {
		return nil, false
	}
	b := document.NewBlock("image", attr(img, "alt"))
	b.URL = src
	b.ImageWidth = parseHTMLWidth(img)
	if align != "" {
		b.Align = align
	} else if a := parseBlockAlign(img); a != "" {
		b.Align = a
	}
	return b, true
}

func parseBlockIndent(el *xhtml.Node) (int, bool) {
	if raw := strings.TrimSpace(attr(el, "data-indent")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 && n <= 12 {
			return n, n > 0
		}
	}
	props := parseCSSProps(attr(el, "style"))
	ml := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(props["margin-left"])), "px")
	if ml != "" {
		if v, err := strconv.ParseFloat(ml, 64); err == nil && v >= 16 {
			return min(12, int((v+12)/24)), true
		}
	}
	return 0, false
}

type runAccumulator struct {
	runs   []document.Run
	has    bool
	style  document.Style
	buf    strings.Builder
	u16Len int
}

func (a *runAccumulator) add(text string, style document.Style) {
	if text == "" {
		return
	}
	a.u16Len += document.Len(text)
	if !a.has {
		a.has = true
		a.style = style
		a.buf.WriteString(text)
		return
	}
	if a.style == style {
		a.buf.WriteString(text)
		return
	}
	a.runs = append(a.runs, document.Run{Text: a.buf.String(), Style: a.style})
	a.buf.Reset()
	a.style = style
	a.buf.WriteString(text)
}

func (a *runAccumulator) finish() []document.Run {
	if a.has {
		a.runs = append(a.runs, document.Run{Text: a.buf.String(), Style: a.style})
		a.has = false
		a.buf.Reset()
	}
	if len(a.runs) == 0 {
		return nil
	}
	return slices.Clip(a.runs)
}

func importRuns(in []parser.Inline, base document.Style) []document.Run {
	runs, _ := importRunsAndComments(in, base)
	return runs
}

func importRunsAndComments(in []parser.Inline, base document.Style) ([]document.Run, []document.InlineComment) {
	return importRunsAndCommentsWithBreak(in, base, "\u2028")
}

func importRunsAndCommentsWithBreak(in []parser.Inline, base document.Style, hardBreak string) ([]document.Run, []document.InlineComment) {
	if len(in) == 0 {
		return nil, nil
	}
	var acc runAccumulator
	acc.runs = make([]document.Run, 0, min(len(in), 8))
	var comments []document.InlineComment
	var walk func(int, int, document.Style)
	walk = func(start, stop int, initial document.Style) {
		style := initial
		var stack []htmlStyleFrame
		for i := start; i < stop; {
			node := in[i]
			end := min(stop, max(i+1, node.Next))
			if node.Kind == parser.HTML {
				if handled, emit, comment := applyInlineHTMLToken(node.Text, &stack, &style, hardBreak); handled {
					if comment != "" {
						comments = append(comments, document.InlineComment{Offset: acc.u16Len, Text: comment})
					}
					acc.add(emit, style)
					i = end
					continue
				}
				acc.add(node.Text, style)
				i = end
				continue
			}
			next := style
			switch node.Kind {
			case parser.Strong:
				next.Bold = true
			case parser.Emphasis:
				next.Italic = true
			case parser.Strikethrough:
				next.Strike = true
			case parser.Link:
				next.Link = node.Dest
			case parser.Code:
				if !next.Kbd {
					next.Code = true
				}
			case parser.Text:
				next.NoMath = strings.Contains(node.Text, "$")
			}
			if end > i+1 && node.Kind != parser.Image {
				walk(i+1, end, next)
			} else if node.Kind == parser.Text && !next.Code && !next.Kbd {
				writeHighlightSyntax(&acc, node.Text, next)
			} else {
				text := node.Text
				switch node.Kind {
				case parser.LineBreak:
					text = hardBreak
				case parser.SoftBreak:
					text = "\n"
				case parser.FootnoteRef:
					text = "[^" + node.Text + "]"
				case parser.Math:
					text = "$" + node.Text + "$"
				case parser.Image:
					text = "![" + parser.PlainText(in[i+1:end]) + "](" + node.Dest + ")"
				}
				acc.add(text, next)
			}
			i = end
		}
	}
	walk(0, len(in), base)
	return acc.finish(), comments
}

// splitHighlightSyntax parses Obsidian/Textus ==highlight== spans in plain text
// without touching equality comparisons like "a == b" or "===".
func splitHighlightSyntax(s string, style document.Style) []document.Run {
	var acc runAccumulator
	writeHighlightSyntax(&acc, s, style)
	return acc.finish()
}

func writeHighlightSyntax(acc *runAccumulator, s string, style document.Style) {
	if !strings.Contains(s, "==") {
		acc.add(s, style)
		return
	}
	pos := 0
	for i := 0; i+3 < len(s); i++ {
		if s[i] != '=' || s[i+1] != '=' {
			continue
		}
		if (i > 0 && (s[i-1] == '=' || s[i-1] == '\\')) || (i+2 < len(s) && (s[i+2] == '=' || s[i+2] == ' ' || s[i+2] == '\t' || s[i+2] == '\n')) {
			continue
		}
		closeIdx := -1
		for j := i + 2; j+1 < len(s); j++ {
			if s[j] == '\n' {
				break
			}
			if s[j] == '=' && s[j+1] == '=' {
				if s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '=' || (j+2 < len(s) && s[j+2] == '=') {
					continue
				}
				closeIdx = j
				break
			}
		}
		if closeIdx < 0 {
			continue
		}
		if i > pos {
			plain := style
			plain.NoMath = strings.Contains(s[pos:i], "$")
			acc.add(s[pos:i], plain)
		}
		inner := s[i+2 : closeIdx]
		hl := style
		if hl.Highlight == "" {
			hl.Highlight = "light-yellow"
		}
		hl.NoMath = strings.Contains(inner, "$")
		acc.add(inner, hl)
		pos = closeIdx + 2
		i = closeIdx + 1
	}
	if pos < len(s) {
		plain := style
		plain.NoMath = strings.Contains(s[pos:], "$")
		acc.add(s[pos:], plain)
	}
}

const maxCachedSourceBytes = 16 << 20

type cachedSourceParse struct {
	source  string
	parsed  parsedMarkdown
	buckets map[uint64][]int
}

var (
	parsedSourceMu    sync.Mutex
	parsedSourceCache [2]cachedSourceParse
)

func parseMarkdownForExport(source string) (parsedMarkdown, map[uint64][]int) {
	if len(source) <= maxCachedSourceBytes {
		parsedSourceMu.Lock()
		for i := range parsedSourceCache {
			if parsedSourceCache[i].source == source && parsedSourceCache[i].parsed.doc != nil {
				entry := parsedSourceCache[i]
				if i == 1 {
					parsedSourceCache[0], parsedSourceCache[1] = parsedSourceCache[1], parsedSourceCache[0]
				}
				parsedSourceMu.Unlock()
				return entry.parsed, entry.buckets
			}
		}
		parsedSourceMu.Unlock()
	}

	orig := parseMarkdownSource(source)
	buckets := buildChunkBuckets(orig)

	if len(source) <= maxCachedSourceBytes {
		parsedSourceMu.Lock()
		parsedSourceCache[1] = parsedSourceCache[0]
		parsedSourceCache[0] = cachedSourceParse{source: source, parsed: orig, buckets: buckets}
		parsedSourceMu.Unlock()
	}
	return orig, buckets
}

func buildChunkBuckets(orig parsedMarkdown) map[uint64][]int {
	buckets := make(map[uint64][]int, len(orig.chunks))
	for ci, ch := range orig.chunks {
		if ch.to <= ch.from || ch.from < 0 || ch.from >= len(orig.doc.Blocks) {
			continue
		}
		h := blockSemanticHash(orig.doc.Blocks[ch.from])
		buckets[h] = append(buckets[h], ci)
	}
	return buckets
}

// ExportMarkdown serializes a rich Document to standard Markdown, preserving
// custom ArcMD features via GFM extensions, YAML frontmatter and semantic HTML,
// while keeping unedited blocks from ImportSource byte-for-byte intact.
func ExportMarkdown(doc *document.Document) string {
	if doc.ImportSource != "" {
		orig, buckets := parseMarkdownForExport(doc.ImportSource)
		if sameDocumentSemantics(doc, orig.doc) {
			return ensureTrailingNewline(doc.ImportSource)
		}
		return exportWithSourceChunks(doc, orig, buckets)
	}
	return exportFreshMarkdown(doc)
}

func ensureTrailingNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

func hasTrailingBlankBody(blocks []*document.Block) bool {
	if len(blocks) < 2 {
		return false
	}
	last := blocks[len(blocks)-1]
	prev := blocks[len(blocks)-2]
	return prev.Kind == "heading" && last.Kind == "paragraph" && last.Text() == "" &&
		normCellAlign(last.Align) == "" && last.Indent == 0 && len(last.Children) == 0
}

func exportFreshMarkdown(doc *document.Document) string {
	fm := exportFrontmatter(doc)
	blocks := doc.Blocks
	trailingBlank := hasTrailingBlankBody(blocks)
	if trailingBlank {
		blocks = blocks[:len(blocks)-1]
	}
	body := strings.TrimRight(renderMarkdownBlockList(blocks), "\n")
	tail := "\n"
	if trailingBlank {
		tail = "\n\n"
	}
	if fm != "" {
		if body == "" {
			return fm + "\n"
		}
		return fm + "\n\n" + body + tail
	}
	return body + tail
}

func exportWithSourceChunks(doc *document.Document, orig parsedMarkdown, buckets map[uint64][]int) string {
	var fm string
	if doc.Frontmatter == orig.doc.Frontmatter && doc.Icon == orig.doc.Icon &&
		doc.Cover == orig.doc.Cover && doc.CoverY == orig.doc.CoverY && doc.Goal == orig.doc.Goal {
		fm = orig.rawFrontmatter
	} else {
		fm = exportFrontmatter(doc)
	}

	blocks := doc.Blocks
	trailingBlank := hasTrailingBlankBody(blocks)
	if trailingBlank {
		blocks = blocks[:len(blocks)-1]
	}

	used := make([]bool, len(orig.chunks))
	cursors := make(map[uint64]int, len(buckets))
	type emittedItem struct {
		text   string
		isList bool
	}
	items := make([]emittedItem, 0, len(blocks))
	numberedSeq := map[int]int{}
	listCols := map[int]int{}

	for i := 0; i < len(blocks); {
		matched := -1
		h := blockSemanticHash(blocks[i])
		bucket := buckets[h]
		start := cursors[h]
		for start < len(bucket) && used[bucket[start]] {
			start++
		}
		cursors[h] = start
		for k := start; k < len(bucket); k++ {
			ci := bucket[k]
			if used[ci] {
				continue
			}
			ch := orig.chunks[ci]
			spanLen := ch.to - ch.from
			if spanLen <= 0 || i+spanLen > len(blocks) {
				continue
			}
			// Do not reuse a toggle chunk if new indented blocks were added right after it.
			if blocks[i].Kind == "toggle" && i+spanLen < len(blocks) &&
				blocks[i+spanLen].Kind != "heading" && blocks[i+spanLen].Indent > blocks[i].Indent {
				continue
			}
			if sameBlockSliceSemantics(blocks[i:i+spanLen], orig.doc.Blocks[ch.from:ch.to]) {
				matched = ci
				break
			}
		}
		if matched >= 0 {
			used[matched] = true
			ch := orig.chunks[matched]
			spanLen := ch.to - ch.from
			firstKind := blocks[i].Kind
			isList := isMarkdownListKind(firstKind) && blocks[i].Align == ""
			if !isList {
				clear(numberedSeq)
				clear(listCols)
			} else {
				for bi := i; bi < i+spanLen; bi++ {
					advanceListState(blocks[bi], numberedSeq, listCols)
				}
			}
			items = append(items, emittedItem{text: ch.raw, isList: isList})
			i += spanLen
			continue
		}
		text, consumed, isList := renderSingleMarkdownItem(blocks, i, numberedSeq, listCols)
		items = append(items, emittedItem{text: text, isList: isList})
		i += consumed
	}

	var body strings.Builder
	for idx, it := range items {
		if idx > 0 {
			if items[idx-1].isList && it.isList {
				body.WriteString("\n")
			} else {
				body.WriteString("\n\n")
			}
		}
		body.WriteString(it.text)
	}
	bodyStr := strings.TrimRight(body.String(), "\n")
	tail := "\n"
	if trailingBlank {
		tail = "\n\n"
	}
	if fm != "" {
		if bodyStr == "" {
			return fm + "\n"
		}
		return fm + "\n\n" + bodyStr + tail
	}
	return bodyStr + tail
}

// MarkdownPart is the Markdown of one top-level block, a toggle's with the
// blocks folded under it, and the block's ID: ExportMarkdownParts joins them
// with blank lines, so a reader of the Markdown can tell which block a
// line came from.
type MarkdownPart struct {
	Block string
	Text  string
}

// ExportMarkdownParts is ExportMarkdown cut at its top-level blocks.
func ExportMarkdownParts(doc *document.Document) []MarkdownPart {
	var top []MarkdownPart
	numberedSeq := map[int]int{}
	listCols := map[int]int{}
	for i := 0; i < len(doc.Blocks); {
		b := doc.Blocks[i]
		text, consumed, _ := renderSingleMarkdownItem(doc.Blocks, i, numberedSeq, listCols)
		top = append(top, MarkdownPart{Block: b.ID, Text: text})
		i += consumed
	}
	return top
}

func isMarkdownListKind(kind string) bool {
	return kind == "bullet" || kind == "numbered" || kind == "task"
}

func renderMarkdownBlockList(blocks []*document.Block) string {
	numberedSeq := map[int]int{}
	listCols := map[int]int{}
	type item struct {
		text   string
		isList bool
	}
	var items []item
	for i := 0; i < len(blocks); {
		text, consumed, isList := renderSingleMarkdownItem(blocks, i, numberedSeq, listCols)
		items = append(items, item{text: text, isList: isList})
		i += consumed
	}
	var out strings.Builder
	for idx, it := range items {
		if idx > 0 {
			if items[idx-1].isList && it.isList {
				out.WriteString("\n")
			} else {
				out.WriteString("\n\n")
			}
		}
		out.WriteString(it.text)
	}
	return out.String() + "\n"
}

func advanceListState(b *document.Block, numberedSeq, listCols map[int]int) (padCols, num int) {
	if b.Kind != "numbered" {
		for d := range numberedSeq {
			if !isMarkdownListKind(b.Kind) || d >= b.Indent {
				delete(numberedSeq, d)
			}
		}
	} else {
		for d := range numberedSeq {
			if d > b.Indent {
				delete(numberedSeq, d)
			}
		}
	}
	if !isMarkdownListKind(b.Kind) {
		clear(listCols)
		return 0, 0
	}
	for d := range listCols {
		if d >= b.Indent {
			delete(listCols, d)
		}
	}
	padCols = 2 * b.Indent
	if b.Indent > 0 {
		if parentCol, ok := listCols[b.Indent-1]; ok {
			padCols = max(padCols, parentCol)
		}
	}
	markerW := 2
	if b.Kind == "numbered" {
		numberedSeq[b.Indent]++
		num = numberedSeq[b.Indent]
		markerW = len(fmt.Sprintf("%d. ", num))
	}
	listCols[b.Indent] = padCols + markerW
	return padCols, num
}

func renderSingleMarkdownItem(blocks []*document.Block, i int, numberedSeq, listCols map[int]int) (text string, consumed int, isList bool) {
	b := blocks[i]
	consumed = 1
	align := b.Align
	if align == "left" {
		align = ""
	}
	padCols, num := advanceListState(b, numberedSeq, listCols)
	pad := strings.Repeat(" ", padCols)

	inline := exportRunsWithComments(b.Runs, b.Comments, b.Kind != "heading")
	switch b.Kind {
	case "toggle":
		var body []*document.Block
		for i+consumed < len(blocks) && blocks[i+consumed].Kind != "heading" && blocks[i+consumed].Indent > b.Indent {
			inner := *blocks[i+consumed]
			inner.Indent -= b.Indent + 1
			body = append(body, &inner)
			consumed++
		}
		open := "<details>"
		if !b.Folded {
			open = "<details open>"
		}
		summary := exportHTMLRunsWithComments(b.Runs, b.Comments, true)
		text = open + "\n<summary>" + summary + "</summary>\n\n"
		if len(body) > 0 {
			text += renderMarkdownBlockList(body) + "\n"
		}
		text += "</details>"
	case "footnote":
		text = "[^" + b.NoteLabel + "]: " + strings.ReplaceAll(inline, "\n", "\n    ")
	case "paragraph":
		if align != "" || b.Indent > 0 {
			text = formatAlignedBlockHTML("p", align, b.Indent, exportHTMLRunsWithComments(b.Runs, b.Comments, true))
		} else {
			text = inline
		}
	case "heading":
		lvl := max(1, min(6, b.Level))
		if align != "" || b.Indent > 0 {
			text = formatAlignedBlockHTML(fmt.Sprintf("h%d", lvl), align, b.Indent, exportHTMLRunsWithComments(b.Runs, b.Comments, true))
		} else {
			text = strings.Repeat("#", lvl) + " " + inline
		}
	case "bullet":
		cont := strings.Repeat(" ", listCols[b.Indent])
		text = pad + "- " + strings.ReplaceAll(inline, "\n", "\n"+cont)
		if align != "" {
			text = `<div align="` + align + "\">\n\n" + text + "\n\n</div>"
		} else {
			isList = true
		}
	case "numbered":
		cont := strings.Repeat(" ", listCols[b.Indent])
		text = fmt.Sprintf("%s%d. %s", pad, num, strings.ReplaceAll(inline, "\n", "\n"+cont))
		if align != "" {
			text = `<div align="` + align + "\">\n\n" + text + "\n\n</div>"
		} else {
			isList = true
		}
	case "task":
		check := " "
		if b.Checked {
			check = "x"
		}
		cont := strings.Repeat(" ", listCols[b.Indent])
		text = pad + "- [" + check + "] " + strings.ReplaceAll(inline, "\n", "\n"+cont)
		if align != "" {
			text = `<div align="` + align + "\">\n\n" + text + "\n\n</div>"
		} else {
			isList = true
		}
	case "quote":
		text = "> " + strings.ReplaceAll(inline, "\n", "\n> ")
		if align != "" {
			text = `<div align="` + align + "\">\n\n" + text + "\n\n</div>"
		}
	case "callout":
		prefix := ""
		if b.Emoji != "" || align != "" {
			var attrs []string
			if b.Emoji != "" {
				attrs = append(attrs, `data-emoji="`+html.EscapeString(b.Emoji)+`"`)
			}
			if align != "" {
				attrs = append(attrs, `data-align="`+align+`"`)
			}
			prefix = "<span " + strings.Join(attrs, " ") + "></span>"
		}
		name := alertNames[b.Color]
		if name == "" {
			name = "NOTE"
		}
		text = "> [!" + name + "]\n> " + prefix + strings.ReplaceAll(inline, "\n", "\n> ")
	case "code":
		fence := "```"
		for strings.Contains(b.Text(), fence) {
			fence += "`"
		}
		text = fence + b.Language + "\n" + b.Text() + "\n" + fence
	case "image":
		text = exportImageMarkdown(b, align)
	case "rule":
		text = "---"
	case "raw":
		text = b.Text()
	case "math":
		text = "$$\n" + b.Text() + "\n$$"
	case "table":
		if isSimpleMarkdownTable(b) {
			text = exportPipeTable(b)
		} else {
			text = exportRichHTMLTable(b)
		}
	case "columns":
		ratios := columnRatioStrings(b)
		var cols []string
		for idx, column := range b.Children {
			flexStyle := "flex:1;min-width:0"
			if len(ratios) == len(b.Children) {
				flexStyle = "flex:" + ratios[idx] + ";min-width:0"
			}
			colBody := strings.TrimRight(renderMarkdownBlockList(column.Children), "\n")
			if colBody == "" {
				cols = append(cols, `<div class="arcmd-column" style="`+flexStyle+`">`+"\n\n</div>")
			} else {
				cols = append(cols, `<div class="arcmd-column" style="`+flexStyle+`">`+"\n\n"+colBody+"\n\n</div>")
			}
		}
		openTag := `<div class="arcmd-columns" style="display:flex;gap:24px">`
		if len(ratios) == len(b.Children) {
			openTag = `<div class="arcmd-columns" data-widths="` + strings.Join(ratios, ",") + `" style="display:flex;gap:24px">`
		}
		text = openTag + "\n\n" + strings.Join(cols, "\n\n") + "\n\n</div>"
	default:
		text = inline
	}
	return text, consumed, isList
}

func formatAlignedBlockHTML(tag, align string, indent int, inner string) string {
	var attrs []string
	if align != "" {
		attrs = append(attrs, `align="`+align+`"`)
	}
	if indent > 0 {
		attrs = append(attrs, fmt.Sprintf(`style="margin-left:%dpx" data-indent="%d"`, indent*24, indent))
	}
	if len(attrs) == 0 {
		return "<" + tag + ">" + inner + "</" + tag + ">"
	}
	return "<" + tag + " " + strings.Join(attrs, " ") + ">" + inner + "</" + tag + ">"
}

func exportImageMarkdown(b *document.Block, align string) string {
	if b.ImageWidth == 0 && align == "" && b.Indent == 0 {
		dest := b.URL
		if strings.ContainsAny(dest, " ()\t") && !strings.ContainsAny(dest, "<>\r\n") {
			dest = "<" + dest + ">"
		}
		return "![" + exportRuns(b.Runs) + "](" + dest + ")"
	}
	var img strings.Builder
	img.WriteString(`<img src="` + html.EscapeString(b.URL) + `" alt="` + html.EscapeString(b.Text()) + `"`)
	if b.ImageWidth > 0 {
		fmt.Fprintf(&img, ` width="%.0f"`, b.ImageWidth)
	}
	img.WriteString(">")
	if align != "" || b.Indent > 0 {
		return formatAlignedBlockHTML("p", align, b.Indent, img.String())
	}
	return img.String()
}

func normCellAlign(a string) string {
	if a == "center" || a == "right" {
		return a
	}
	return ""
}

func isSimpleMarkdownTable(b *document.Block) bool {
	if len(b.Widths) != 0 || len(b.Children) == 0 {
		return false
	}
	head := b.Children[0].Children
	for r, row := range b.Children {
		for c, cell := range row.Children {
			if cell.ColSpan > 1 || cell.RowSpan > 1 || cell.Background != "" {
				return false
			}
			for _, cm := range cell.Comments {
				if strings.ContainsAny(cm.Text, "|\r\n") {
					return false
				}
			}
			if r > 0 && c < len(head) && normCellAlign(cell.Align) != normCellAlign(head[c].Align) {
				return false
			}
		}
	}
	return true
}

func exportPipeTable(b *document.Block) string {
	var rows []string
	for i, row := range b.Children {
		var cells []string
		for _, cell := range row.Children {
			cells = append(cells, strings.ReplaceAll(strings.ReplaceAll(exportRunsWithComments(cell.Runs, cell.Comments, false), "|", "\\|"), "\n", "<br>"))
		}
		rows = append(rows, "| "+strings.Join(cells, " | ")+" |")
		if i == 0 {
			var bars []string
			for _, cell := range row.Children {
				bar := "---"
				switch cell.Align {
				case "center":
					bar = ":---:"
				case "right":
					bar = "---:"
				}
				bars = append(bars, bar)
			}
			rows = append(rows, "| "+strings.Join(bars, " | ")+" |")
		}
	}
	return strings.Join(rows, "\n")
}

func exportRichHTMLTable(b *document.Block) string {
	var out strings.Builder
	out.WriteString("<table>\n")
	if len(b.Widths) > 0 {
		out.WriteString("<colgroup>")
		for _, w := range b.Widths {
			fmt.Fprintf(&out, `<col width="%.0f">`, w)
		}
		out.WriteString("</colgroup>\n")
	}
	covered := document.CoveredCells(b)
	for i, row := range b.Children {
		if i == 0 {
			out.WriteString("<thead>\n")
		} else if i == 1 {
			out.WriteString("<tbody>\n")
		}
		out.WriteString("<tr>")
		for _, cell := range row.Children {
			if covered[cell] {
				continue
			}
			tag := "td"
			if i == 0 {
				tag = "th"
			}
			var attrs []string
			cols, rows := document.Span(cell)
			if cols > 1 {
				attrs = append(attrs, fmt.Sprintf(`colspan="%d"`, cols))
			}
			if rows > 1 {
				attrs = append(attrs, fmt.Sprintf(`rowspan="%d"`, rows))
			}
			if a := normCellAlign(cell.Align); a != "" {
				attrs = append(attrs, `align="`+a+`"`)
			}
			if cell.Background != "" {
				if hex := cellFillHex[cell.Background]; hex != "" {
					attrs = append(attrs, `style="background-color:`+hex+`"`)
				}
				attrs = append(attrs, `data-bg="`+cell.Background+`"`)
			}
			if len(attrs) > 0 {
				out.WriteString("<" + tag + " " + strings.Join(attrs, " ") + ">")
			} else {
				out.WriteString("<" + tag + ">")
			}
			out.WriteString(exportHTMLRunsWithComments(cell.Runs, cell.Comments, true))
			out.WriteString("</" + tag + ">")
		}
		out.WriteString("</tr>\n")
		if i == 0 {
			out.WriteString("</thead>\n")
		}
	}
	if len(b.Children) > 1 {
		out.WriteString("</tbody>\n")
	}
	out.WriteString("</table>")
	return out.String()
}

func exportRuns(runs []document.Run) string {
	runs = document.Compact(runs)
	return renderMarkdownRunSlice(runs, 0, 0)
}

func sortedInlineComments(comments []document.InlineComment, totalLen int) []document.InlineComment {
	sorted := make([]document.InlineComment, len(comments))
	for i, c := range comments {
		c.Offset = max(0, min(totalLen, c.Offset))
		sorted[i] = c
	}
	slices.SortStableFunc(sorted, func(a, b document.InlineComment) int {
		return a.Offset - b.Offset
	})
	return sorted
}

// atMarkdownLineStart reports whether writing "<!--" at the end of s would
// trigger a CommonMark type-2 HTML block (0-3 spaces at the start of a line).
func atMarkdownLineStart(s string, guardFirstLine bool) bool {
	if nl := strings.LastIndexByte(s, '\n'); nl >= 0 {
		s = s[nl+1:]
	} else if !guardFirstLine {
		return false
	}
	if len(s) > 3 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' {
			return false
		}
	}
	return true
}

func exportRunsWithComments(runs []document.Run, comments []document.InlineComment, guardFirstLine bool) string {
	runs = document.Compact(runs)
	if len(comments) == 0 {
		return renderMarkdownRunSlice(runs, 0, 0)
	}
	totalLen := runsU16Len(runs)
	sorted := sortedInlineComments(comments, totalLen)
	var out strings.Builder
	prevOff := 0
	var prevRune rune
	for _, c := range sorted {
		if c.Offset > prevOff {
			seg := document.Compact(document.SliceRuns(runs, prevOff, c.Offset))
			out.WriteString(renderMarkdownRunSlice(seg, prevRune, '<'))
			prevOff = c.Offset
		}
		if atMarkdownLineStart(out.String(), guardFirstLine) {
			out.WriteString("<span></span>")
		}
		out.WriteString(c.Text)
		prevRune = '>'
	}
	if prevOff < totalLen {
		seg := document.Compact(document.SliceRuns(runs, prevOff, totalLen))
		out.WriteString(renderMarkdownRunSlice(seg, prevRune, 0))
	}
	return out.String()
}

func exportHTMLRunsWithComments(runs []document.Run, comments []document.InlineComment, preserveNoMath bool) string {
	runs = document.Compact(runs)
	if len(comments) == 0 {
		return renderHTMLRunSlice(runs, preserveNoMath)
	}
	totalLen := runsU16Len(runs)
	sorted := sortedInlineComments(comments, totalLen)
	var out strings.Builder
	prevOff := 0
	for _, c := range sorted {
		if c.Offset > prevOff {
			seg := document.Compact(document.SliceRuns(runs, prevOff, c.Offset))
			out.WriteString(renderHTMLRunSlice(seg, preserveNoMath))
			prevOff = c.Offset
		}
		out.WriteString(c.Text)
	}
	if prevOff < totalLen {
		seg := document.Compact(document.SliceRuns(runs, prevOff, totalLen))
		out.WriteString(renderHTMLRunSlice(seg, preserveNoMath))
	}
	return out.String()
}

func firstRuneOfRuns(runs []document.Run) rune {
	for _, r := range runs {
		for _, ch := range r.Text {
			return ch
		}
	}
	return 0
}

func lastRuneOfString(s string) rune {
	var last rune
	for _, ch := range s {
		last = ch
	}
	return last
}

var markdownMarkOrder = []string{"link", "kbd", "underline", "script", "span", "strike", "italic", "bold"}

func chooseOuterMarkdownMark(runs []document.Run, i int) (kind, val string, end int) {
	for _, k := range markdownMarkOrder {
		ok, v := hasHTMLMark(runs[i].Style, k)
		if !ok {
			continue
		}
		j := i + 1
		for j < len(runs) {
			ok2, v2 := hasHTMLMark(runs[j].Style, k)
			if !ok2 || v2 != v {
				break
			}
			j++
		}
		if j > end {
			kind, val, end = k, v, j
		}
	}
	return kind, val, end
}

func renderMarkdownRunSlice(runs []document.Run, before, after rune) string {
	return renderMarkdownRuns(runs, before, after, false)
}

func renderMarkdownRuns(runs []document.Run, before, after rune, inKbd bool) string {
	var out strings.Builder
	prev := before
	for i := 0; i < len(runs); {
		r := runs[i]
		kind, val, j := chooseOuterMarkdownMark(runs, i)
		if kind == "" {
			leaf := formatMarkdownLeafRun(r, inKbd)
			out.WriteString(leaf)
			if lr := lastRuneOfString(leaf); lr != 0 {
				prev = lr
			}
			i++
			continue
		}
		sub := make([]document.Run, j-i)
		for k := i; k < j; k++ {
			sub[k-i] = clearHTMLMark(runs[k], kind)
			if kind == "kbd" {
				sub[k-i].Style.NoMath = true
			}
		}
		next := after
		if j < len(runs) {
			next = firstRuneOfRuns(runs[j:])
		}
		// Inside explicit wrapper punctuation (links or HTML tags), inner
		// emphasis delimiters see '['/']' or '>'/'<'.
		innerPrev, innerNext := prev, next
		switch kind {
		case "link":
			innerPrev, innerNext = '[', ']'
		case "kbd", "underline", "script", "span":
			innerPrev, innerNext = '>', '<'
		case "bold", "italic":
			innerPrev, innerNext = '*', '*'
		case "strike":
			innerPrev, innerNext = '~', '~'
		}
		inner := renderMarkdownRuns(sub, innerPrev, innerNext, inKbd || kind == "kbd")
		var chunk string
		switch kind {
		case "link":
			chunk = fmt.Sprintf("[%s](%s)", inner, val)
		case "kbd":
			chunk = "<kbd>" + inner + "</kbd>"
		case "bold":
			if canUseMarkdownDelim(inner, prev, next, "**") {
				chunk = "**" + inner + "**"
			} else {
				chunk = "<strong>" + inner + "</strong>"
			}
		case "italic":
			if canUseMarkdownDelim(inner, prev, next, "*") {
				chunk = "*" + inner + "*"
			} else {
				chunk = "<em>" + inner + "</em>"
			}
		case "strike":
			if canUseMarkdownDelim(inner, prev, next, "~~") {
				chunk = "~~" + inner + "~~"
			} else {
				chunk = "<del>" + inner + "</del>"
			}
		case "underline":
			chunk = "<u>" + inner + "</u>"
		case "script":
			chunk = "<" + val + ">" + inner + "</" + val + ">"
		case "span":
			tag, attrs := spanTagAndAttrs(r.Style)
			if attrs != "" {
				chunk = "<" + tag + " " + attrs + ">" + inner + "</" + tag + ">"
			} else {
				chunk = "<" + tag + ">" + inner + "</" + tag + ">"
			}
		}
		out.WriteString(chunk)
		if lr := lastRuneOfString(chunk); lr != 0 {
			prev = lr
		}
		i = j
	}
	return out.String()
}

func formatMarkdownLeafRun(run document.Run, skipWiki bool) string {
	if run.Style.Code {
		fence := "`"
		for strings.Contains(run.Text, fence) {
			fence += "`"
		}
		if strings.HasPrefix(run.Text, "`") || strings.HasSuffix(run.Text, "`") || strings.HasPrefix(run.Text, " ") && strings.HasSuffix(run.Text, " ") {
			return fence + " " + run.Text + " " + fence
		}
		return fence + run.Text + fence
	}
	source := strings.ReplaceAll(run.Text, "\u2028", "  \n")
	var escaped strings.Builder
	pos := 0
	for _, token := range inlineTokens(source) {
		if run.Style.NoMath && token.note == "" {
			continue
		}
		escaped.WriteString(escapePlainSegment(source[pos:token.start], skipWiki))
		escaped.WriteString(source[token.start:token.end])
		pos = token.end
	}
	escaped.WriteString(escapePlainSegment(source[pos:], skipWiki))
	text := escaped.String()
	if run.Style.NoMath {
		text = strings.ReplaceAll(text, "$", `\$`)
	}
	return text
}

func escapePlainSegment(s string, skipWiki bool) string {
	if skipWiki {
		return escapeMarkdownPlain(s)
	}
	links := wiki.Links(s)
	if len(links) == 0 {
		return escapeMarkdownPlain(s)
	}
	var b strings.Builder
	pos := 0
	for _, l := range links {
		b.WriteString(escapeMarkdownPlain(s[pos:l.Start]))
		b.WriteString("[[")
		b.WriteString(escapeWikiInner(s[l.Start+2 : l.End-2]))
		b.WriteString("]]")
		pos = l.End
		// Prevent "[[Target]](note)" from turning "[Target](note)" into an inline link.
		if pos < len(s) && s[pos] == '(' {
			b.WriteString("&#40;")
			pos++
		}
	}
	b.WriteString(escapeMarkdownPlain(s[pos:]))
	return b.String()
}

func escapeMarkdownPlain(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '*':
			b.WriteString(`\*`)
		case '[':
			b.WriteString(`\[`)
		case '`':
			b.WriteString("\\`")
		case '<':
			b.WriteString("&lt;")
		case '_':
			prevAlnum := false
			if i > 0 {
				pr, _ := utf8.DecodeLastRuneInString(s[:i])
				prevAlnum = unicode.IsLetter(pr) || unicode.IsDigit(pr)
			}
			nextAlnum := false
			if i+n < len(s) {
				nr, _ := utf8.DecodeRuneInString(s[i+n:])
				nextAlnum = unicode.IsLetter(nr) || unicode.IsDigit(nr)
			}
			if prevAlnum && nextAlnum {
				b.WriteByte('_')
			} else {
				b.WriteString(`\_`)
			}
		default:
			b.WriteString(s[i : i+n])
		}
		i += n
	}
	out := b.String()
	// Only escape "==" when it would be misread as ==highlight== syntax.
	if strings.Contains(out, "==") && len(splitHighlightSyntax(out, document.Style{})) > 1 {
		out = strings.ReplaceAll(out, "==", `\==`)
	}
	return out
}

func escapeWikiInner(inner string) string {
	var b strings.Builder
	for i := 0; i < len(inner); {
		r, n := utf8.DecodeRuneInString(inner[i:])
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '*':
			b.WriteString(`\*`)
		case '`':
			b.WriteString("\\`")
		case '<':
			b.WriteString("&lt;")
		case '_':
			prevAlnum := false
			if i > 0 {
				pr, _ := utf8.DecodeLastRuneInString(inner[:i])
				prevAlnum = unicode.IsLetter(pr) || unicode.IsDigit(pr)
			}
			nextAlnum := false
			if i+n < len(inner) {
				nr, _ := utf8.DecodeRuneInString(inner[i+n:])
				nextAlnum = unicode.IsLetter(nr) || unicode.IsDigit(nr)
			}
			if prevAlnum && nextAlnum {
				b.WriteByte('_')
			} else {
				b.WriteString(`\_`)
			}
		default:
			b.WriteString(inner[i : i+n])
		}
		i += n
	}
	out := b.String()
	if strings.Contains(out, "==") && len(splitHighlightSyntax(out, document.Style{})) > 1 {
		out = strings.ReplaceAll(out, "==", `\==`)
	}
	return out
}

// alertNames are the GitHub alerts that carry callout colors in Markdown.
var alertNames = map[string]string{"": "NOTE", "green": "TIP", "purple": "IMPORTANT", "yellow": "WARNING", "red": "CAUTION"}

// alertMetadata reads a GitHub alert's marker ("[!...]") plus an optional empty
// <span data-emoji="..." data-align="..."></span> from the start of a quote.
func alertMetadata(children []*document.Block) (color, emoji, align string, ok bool) {
	if len(children) == 0 {
		return "", "", "", false
	}
	first := children[0]
	text := first.Text()
	end := strings.Index(text, "]")
	if !strings.HasPrefix(text, "[!") || end < 0 || strings.ContainsAny(text[:end], " \n") {
		return "", "", "", false
	}
	name := strings.ToUpper(text[2:end])
	for c, n := range alertNames {
		if n == name {
			color = c
		}
	}
	rest := text[end+1:]
	trimmed := strings.TrimLeft(rest, " \t\n\u2028")
	if strings.HasPrefix(trimmed, "<span ") {
		if closeIdx := strings.IndexByte(trimmed, '>'); closeIdx > 0 {
			tagRaw := trimmed[:closeIdx+1]
			z := xhtml.NewTokenizer(strings.NewReader(tagRaw))
			if z.Next() == xhtml.StartTagToken {
				tok := z.Token()
				n := &xhtml.Node{Type: xhtml.ElementNode, DataAtom: tok.DataAtom, Data: "span", Attr: tok.Attr}
				if em := attr(n, "data-emoji"); em != "" && len(em) <= 32 && !strings.ContainsAny(em, "\n\r") {
					emoji = em
				}
				if al := attr(n, "data-align"); al == "center" || al == "right" {
					align = al
				}
				if emoji != "" || align != "" {
					trimmed = trimmed[closeIdx+1:]
				}
			}
		}
	}
	start := document.Len(text) - document.Len(trimmed)
	endLen := document.Len(text)
	first.Runs = document.SliceRuns(first.Runs, start, endLen)
	first.Comments = document.SliceComments(first.Comments, start, endLen)
	return color, emoji, align, true
}

var (
	detailsOpen      = regexp.MustCompile(`(?is)^\s*<details(\s+open)?\s*>\s*<summary>(.*?)</summary>\s*$`)
	detailsClose     = regexp.MustCompile(`(?i)^\s*</details>\s*$`)
	columnsOpenRe    = regexp.MustCompile(`(?is)^\s*<div\b[^>]*\bclass\s*=\s*["'][^"']*\barcmd-columns\b[^"']*["'][^>]*>\s*(<div\b[^>]*>\s*)?$`)
	columnsWidthsRe  = regexp.MustCompile(`(?i)\bdata-widths\s*=\s*["']([^"']+)["']`)
	columnOpenRe     = regexp.MustCompile(`(?is)^\s*<div\b[^>]*\bclass\s*=\s*["'][^"']*\barcmd-column\b[^"']*["'][^>]*>\s*$`)
	divOpenGenericRe = regexp.MustCompile(`(?is)^\s*<div\s*>\s*$`)
	divNextColumnRe  = regexp.MustCompile(`(?is)^\s*</div>\s*<div\b[^>]*>\s*$`)
	divDoubleCloseRe = regexp.MustCompile(`(?is)^\s*</div>\s*</div>\s*$`)
	divCloseRe       = regexp.MustCompile(`(?i)^\s*</div>\s*$`)
	centerCloseRe    = regexp.MustCompile(`(?i)^\s*</center\s*>\s*$`)
)

func isHTMLCommentBlock(s string) bool {
	rest := strings.TrimSpace(s)
	if rest == "" {
		return false
	}
	found := false
	for rest != "" {
		if !strings.HasPrefix(rest, "<!--") {
			return false
		}
		end := strings.Index(rest[4:], "-->")
		if end < 0 {
			return false
		}
		found = true
		rest = strings.TrimSpace(rest[4+end+3:])
	}
	return found
}

func hasBlockElementChild(n *xhtml.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xhtml.ElementNode {
			continue
		}
		switch c.DataAtom {
		case atom.Div, atom.P, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6,
			atom.Ul, atom.Ol, atom.Blockquote, atom.Pre, atom.Table, atom.Hr, atom.Details, atom.Center:
			return true
		}
		if hasBlockElementChild(c) {
			return true
		}
	}
	return false
}

func hasUnsupportedInlineMedia(n *xhtml.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xhtml.ElementNode {
			continue
		}
		switch c.DataAtom {
		case atom.Img, atom.Svg, atom.Video, atom.Audio, atom.Iframe,
			atom.Picture, atom.Canvas, atom.Input, atom.Button,
			atom.Object, atom.Embed, atom.Form, atom.Select,
			atom.Textarea, atom.Script, atom.Style:
			return true
		}
		if hasUnsupportedInlineMedia(c) {
			return true
		}
	}
	return false
}

func divTagDelta(s string) int {
	z := xhtml.NewTokenizer(strings.NewReader(s))
	delta := 0
	for {
		switch z.Next() {
		case xhtml.ErrorToken:
			return delta
		case xhtml.StartTagToken:
			if z.Token().DataAtom == atom.Div {
				delta++
			}
		case xhtml.EndTagToken:
			if z.Token().DataAtom == atom.Div {
				delta--
			}
		}
	}
}

func isHTMLOpenTag(raw string, want atom.Atom) (*xhtml.Node, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, false
	}
	z := xhtml.NewTokenizer(strings.NewReader(trimmed))
	if z.Next() != xhtml.StartTagToken {
		return nil, false
	}
	tok := z.Token()
	if tok.DataAtom != want {
		return nil, false
	}
	for {
		next := z.Next()
		if next == xhtml.ErrorToken {
			break
		}
		if next == xhtml.TextToken && strings.TrimSpace(z.Token().Data) == "" {
			continue
		}
		return nil, false
	}
	return &xhtml.Node{Type: xhtml.ElementNode, DataAtom: tok.DataAtom, Data: tok.Data, Attr: tok.Attr}, true
}

func parseAlignedWrapperOpen(raw string) (align, closeTag string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || detailsOpen.MatchString(trimmed) || columnsOpenRe.MatchString(trimmed) || columnOpenRe.MatchString(trimmed) {
		return "", "", false
	}
	if _, isCenter := isHTMLOpenTag(trimmed, atom.Center); isCenter {
		return "center", "center", true
	}
	if node, isDiv := isHTMLOpenTag(trimmed, atom.Div); isDiv && attr(node, "class") == "" {
		if a := parseBlockAlign(node); a != "" {
			return a, "div", true
		}
	}
	return "", "", false
}

func importAlignedWrappers(blocks []*document.Block, spanStart, spanEnd map[*document.Block]int, explicitLeft map[*document.Block]bool) []*document.Block {
	var out []*document.Block
	for i := 0; i < len(blocks); i++ {
		b := blocks[i]
		if b.Kind == "raw" {
			if align, closeTag, ok := parseAlignedWrapperOpen(b.Text()); ok {
				closeIdx := -1
				depth := 1
				for j := i + 1; j < len(blocks); j++ {
					if blocks[j].Kind == "raw" {
						txt := blocks[j].Text()
						if closeTag == "center" {
							if centerCloseRe.MatchString(txt) {
								depth--
							} else if _, isOpen := isHTMLOpenTag(txt, atom.Center); isOpen {
								depth++
							}
						} else {
							if divCloseRe.MatchString(txt) {
								depth--
							} else if _, isOpen := isHTMLOpenTag(txt, atom.Div); isOpen {
								depth++
							}
						}
						if depth == 0 {
							closeIdx = j
							break
						}
					}
				}
				if closeIdx > i+1 {
					inner := blocks[i+1 : closeIdx]
					valid := true
					for _, child := range inner {
						if child.Kind == "raw" && !isHTMLCommentBlock(child.Text()) {
							valid = false
							break
						}
					}
					if valid {
						for _, child := range inner {
							if child.Align == "" && !explicitLeft[child] && child.Kind != "table" && child.Kind != "columns" && child.Kind != "raw" && child.Kind != "code" && child.Kind != "math" && child.Kind != "rule" {
								child.Align = align
							}
						}
						if spanStart != nil {
							openStart, hasOpen := spanStart[b]
							closeEnd, hasClose := spanEnd[blocks[closeIdx]]
							if hasOpen && hasClose && openStart >= 0 && closeEnd >= openStart {
								if len(inner) == 1 {
									spanStart[inner[0]] = openStart
									spanEnd[inner[0]] = closeEnd
								} else {
									spanStart[inner[0]] = openStart
									spanEnd[inner[0]] = -1
									for k := 1; k < len(inner)-1; k++ {
										spanStart[inner[k]] = -1
										spanEnd[inner[k]] = -1
									}
									spanStart[inner[len(inner)-1]] = -1
									spanEnd[inner[len(inner)-1]] = closeEnd
								}
							}
						}
						out = append(out, inner...)
						i = closeIdx
						continue
					}
				}
			}
		}
		out = append(out, b)
	}
	return out
}

func importColumns(blocks []*document.Block, spanStart, spanEnd map[*document.Block]int, explicitLeft map[*document.Block]bool) []*document.Block {
	var out []*document.Block
	for i := 0; i < len(blocks); i++ {
		b := blocks[i]
		if b.Kind != "raw" {
			out = append(out, b)
			continue
		}
		m := columnsOpenRe.FindStringSubmatch(b.Text())
		if m == nil {
			out = append(out, b)
			continue
		}
		cols, nextIdx, ok := parseColumnsGroup(blocks, i, m[1] != "", spanStart, spanEnd, explicitLeft)
		if !ok {
			out = append(out, b)
			continue
		}
		out = append(out, cols)
		i = nextIdx - 1
	}
	return out
}

func columnRatioStrings(b *document.Block) []string {
	n := len(b.Children)
	if n < 2 || len(b.Widths) != n {
		return nil
	}
	sum := float32(0)
	for _, w := range b.Widths {
		if w <= 0 {
			return nil
		}
		sum += w
	}
	equal := float32(1) / float32(n)
	custom := false
	for _, w := range b.Widths {
		if abs32(w/sum-equal) > 0.005 {
			custom = true
			break
		}
	}
	if !custom {
		return nil
	}
	out := make([]string, n)
	for i, w := range b.Widths {
		pct := w * 100 / sum
		s := fmt.Sprintf("%.1f", pct)
		out[i] = strings.TrimSuffix(s, ".0")
	}
	return out
}

func parseDataWidthsAttr(raw string, expected int) []float32 {
	if expected < 2 || raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) != expected {
		return nil
	}
	widths := make([]float32, expected)
	for i, part := range parts {
		val, err := strconv.ParseFloat(strings.TrimSpace(part), 32)
		if err != nil || val <= 0 || val > 10000 {
			return nil
		}
		widths[i] = float32(val)
	}
	return widths
}

func parseColumnsOpenWidths(tag string, expected int) []float32 {
	m := columnsWidthsRe.FindStringSubmatch(tag)
	if m == nil {
		return nil
	}
	return parseDataWidthsAttr(m[1], expected)
}

func parseColumnsGroup(blocks []*document.Block, start int, firstColOpened bool, spanStart, spanEnd map[*document.Block]int, explicitLeft map[*document.Block]bool) (*document.Block, int, bool) {
	var columns []*document.Block
	var cur []*document.Block
	inCol := firstColOpened
	nestedDivs := 0

	finishCol := func() {
		inner := importDetails(cur, spanStart, spanEnd)
		inner = importAlignedWrappers(inner, spanStart, spanEnd, explicitLeft)
		if len(inner) == 0 {
			inner = []*document.Block{document.NewBlock("paragraph", "")}
		}
		columns = append(columns, &document.Block{
			ID:       document.NewID(),
			Kind:     "column",
			Children: inner,
		})
		cur = nil
		inCol = false
		nestedDivs = 0
	}

	for j := start + 1; j < len(blocks); j++ {
		b := blocks[j]
		if b.Kind == "raw" {
			txt := b.Text()
			if inCol && nestedDivs > 0 {
				if divCloseRe.MatchString(txt) {
					nestedDivs--
					cur = append(cur, b)
					continue
				}
				if divDoubleCloseRe.MatchString(txt) {
					if nestedDivs == 1 {
						nestedDivs = 0
						cur = append(cur, document.NewBlock("raw", "</div>"))
						finishCol()
						continue
					}
					nestedDivs -= 2
					cur = append(cur, b)
					continue
				}
				nestedDivs = max(0, nestedDivs+divTagDelta(txt))
				cur = append(cur, b)
				continue
			}
			if inCol && divDoubleCloseRe.MatchString(txt) {
				finishCol()
				if len(columns) >= 2 && len(columns) <= 5 {
					res := &document.Block{
						ID:       document.NewID(),
						Kind:     "columns",
						Widths:   parseColumnsOpenWidths(blocks[start].Text(), len(columns)),
						Children: columns,
					}
					if spanStart != nil {
						spanStart[res] = spanStart[blocks[start]]
						spanEnd[res] = spanEnd[b]
					}
					return res, j + 1, true
				}
				return nil, 0, false
			}
			if inCol && divNextColumnRe.MatchString(txt) {
				finishCol()
				inCol = true
				continue
			}
			if !inCol && (columnOpenRe.MatchString(txt) || divOpenGenericRe.MatchString(txt)) {
				inCol = true
				continue
			}
			if divCloseRe.MatchString(txt) {
				if inCol {
					finishCol()
					continue
				}
				if len(columns) >= 2 && len(columns) <= 5 {
					res := &document.Block{
						ID:       document.NewID(),
						Kind:     "columns",
						Widths:   parseColumnsOpenWidths(blocks[start].Text(), len(columns)),
						Children: columns,
					}
					if spanStart != nil {
						spanStart[res] = spanStart[blocks[start]]
						spanEnd[res] = spanEnd[b]
					}
					return res, j + 1, true
				}
				return nil, 0, false
			}
			if inCol {
				if d := divTagDelta(txt); d > 0 {
					nestedDivs += d
				}
			}
		}
		if !inCol {
			return nil, 0, false
		}
		cur = append(cur, b)
	}
	return nil, 0, false
}

// importDetails turns HTML disclosures, kept as raw blocks, into toggles:
// what is between <details><summary>…</summary> and </details> becomes
// the toggle's content, indented under it. Unbalanced ones stay raw.
func importDetails(blocks []*document.Block, spanStart, spanEnd map[*document.Block]int) []*document.Block {
	type openToggle struct {
		block *document.Block
		openB *document.Block
	}
	var out []*document.Block
	var open []openToggle
	for _, b := range blocks {
		if b.Kind == "raw" {
			if m := detailsOpen.FindStringSubmatch(b.Text()); m != nil {
				t := document.NewBlock("toggle", "")
				t.Runs, t.Comments = parseInlineHTMLFragment(strings.TrimSpace(m[2]))
				t.Indent = len(open)
				t.Folded = m[1] == ""
				if spanStart != nil {
					spanStart[t] = spanStart[b]
					spanEnd[t] = -1
				}
				out = append(out, t)
				open = append(open, openToggle{block: t, openB: b})
				continue
			}
			if detailsClose.MatchString(b.Text()) && len(open) > 0 {
				top := open[len(open)-1]
				open = open[:len(open)-1]
				if spanEnd != nil {
					spanEnd[top.block] = spanEnd[b]
				}
				continue
			}
		}
		if len(open) > 0 {
			b.Indent = min(12, b.Indent+len(open))
		}
		out = append(out, b)
	}
	return out
}

// stripTags drops HTML tags from a summary's text.
func stripTags(s string) string {
	return regexp.MustCompile(`<[^>]*>`).ReplaceAllString(s, "")
}

func sameDocumentSemantics(a, b *document.Document) bool {
	if a.Icon != b.Icon || a.Cover != b.Cover || a.CoverY != b.CoverY ||
		a.Goal != b.Goal || a.Frontmatter != b.Frontmatter {
		return false
	}
	return sameBlockSliceSemantics(a.Blocks, b.Blocks)
}

func sameBlockSliceSemantics(a, b []*document.Block) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameBlockSemantics(a[i], b[i]) {
			return false
		}
	}
	return true
}

func sameBlockSemantics(a, b *document.Block) bool {
	if a.Kind != b.Kind || a.Level != b.Level || a.Indent != b.Indent ||
		a.Checked != b.Checked || a.Language != b.Language || a.URL != b.URL ||
		normCellAlign(a.Align) != normCellAlign(b.Align) ||
		a.Color != b.Color || a.Emoji != b.Emoji ||
		a.Background != b.Background || a.ColSpan != b.ColSpan || a.RowSpan != b.RowSpan ||
		a.ImageWidth != b.ImageWidth || a.NoteLabel != b.NoteLabel {
		return false
	}
	if a.Kind == "toggle" && a.Folded != b.Folded {
		return false
	}
	if !slices.Equal(a.Widths, b.Widths) {
		return false
	}
	if !sameRunsSemantics(a.Runs, b.Runs) {
		return false
	}
	if !slices.Equal(a.Comments, b.Comments) {
		return false
	}
	return sameBlockSliceSemantics(a.Children, b.Children)
}

func sameRunsSemantics(a, b []document.Run) bool {
	if slices.Equal(a, b) {
		return true
	}
	ca, cb := normalizeRunsForCompare(a), normalizeRunsForCompare(b)
	return slices.Equal(ca, cb)
}

func fnvMixByte(h uint64, b byte) uint64 {
	return (h ^ uint64(b)) * 1099511628211
}

func fnvMixUint32(h uint64, v uint32) uint64 {
	h = fnvMixByte(h, byte(v))
	h = fnvMixByte(h, byte(v>>8))
	h = fnvMixByte(h, byte(v>>16))
	return fnvMixByte(h, byte(v>>24))
}

func fnvMixUint64(h uint64, v uint64) uint64 {
	h = fnvMixUint32(h, uint32(v))
	return fnvMixUint32(h, uint32(v>>32))
}

func fnvMixString(h uint64, s string) uint64 {
	h = fnvMixUint64(h, uint64(len(s)))
	for i := 0; i < len(s); i++ {
		h = fnvMixByte(h, s[i])
	}
	return h
}

func fnvMixStyle(h uint64, st document.Style) uint64 {
	var flags byte
	if st.NoMath {
		flags |= 1
	}
	if st.Bold {
		flags |= 2
	}
	if st.Italic {
		flags |= 4
	}
	if st.Underline {
		flags |= 8
	}
	if st.Strike {
		flags |= 16
	}
	if st.Code {
		flags |= 32
	}
	if st.Kbd {
		flags |= 64
	}
	h = fnvMixByte(h, flags)
	h = fnvMixString(h, st.Script)
	h = fnvMixString(h, st.Link)
	h = fnvMixString(h, st.Color)
	h = fnvMixString(h, st.Highlight)
	h = fnvMixString(h, st.Annotation)
	return fnvMixString(h, st.AnnotationColor)
}

// blockSemanticHash computes a canonical 64-bit hash guaranteed to be equal
// whenever sameBlockSemantics(a, b) returns true.
func blockSemanticHash(b *document.Block) uint64 {
	h := uint64(14695981039346656037)
	h = fnvMixString(h, b.Kind)
	h = fnvMixUint64(h, uint64(b.Level))
	h = fnvMixUint64(h, uint64(b.Indent))
	if b.Checked {
		h = fnvMixByte(h, 1)
	} else {
		h = fnvMixByte(h, 0)
	}
	h = fnvMixString(h, b.Language)
	h = fnvMixString(h, b.URL)
	h = fnvMixString(h, normCellAlign(b.Align))
	h = fnvMixString(h, b.Color)
	h = fnvMixString(h, b.Emoji)
	h = fnvMixString(h, b.Background)
	h = fnvMixUint64(h, uint64(b.ColSpan))
	h = fnvMixUint64(h, uint64(b.RowSpan))
	imgW := b.ImageWidth
	if imgW == 0 {
		imgW = 0
	}
	h = fnvMixUint32(h, math.Float32bits(imgW))
	h = fnvMixString(h, b.NoteLabel)
	if b.Kind == "toggle" && b.Folded {
		h = fnvMixByte(h, 1)
	} else {
		h = fnvMixByte(h, 0)
	}
	h = fnvMixUint64(h, uint64(len(b.Widths)))
	for _, w := range b.Widths {
		if w == 0 {
			w = 0
		}
		h = fnvMixUint32(h, math.Float32bits(w))
	}
	h = hashNormalizedRuns(h, b.Runs)
	h = fnvMixUint64(h, uint64(len(b.Comments)))
	for _, c := range b.Comments {
		h = fnvMixUint64(h, uint64(c.Offset))
		h = fnvMixString(h, c.Text)
	}
	h = fnvMixUint64(h, uint64(len(b.Children)))
	for _, child := range b.Children {
		h = fnvMixUint64(h, blockSemanticHash(child))
	}
	return h
}

func hashNormalizedRuns(h uint64, rs []document.Run) uint64 {
	hasCur := false
	var curStyle document.Style
	emit := func(txt string, st document.Style) {
		if txt == "" {
			return
		}
		if !hasCur || curStyle != st {
			h = fnvMixByte(h, 0xff)
			h = fnvMixStyle(h, st)
			hasCur = true
			curStyle = st
		}
		for i := 0; i < len(txt); i++ {
			h = fnvMixByte(h, txt[i])
		}
	}
	for i := range rs {
		r := &rs[i]
		if !r.Style.NoMath {
			emit(r.Text, r.Style)
			continue
		}
		plain := r.Style
		plain.NoMath = false
		pos := 0
		for j := 0; j < len(r.Text); j++ {
			if r.Text[j] == '$' {
				emit(r.Text[pos:j], plain)
				emit("$", r.Style)
				pos = j + 1
			}
		}
		emit(r.Text[pos:], plain)
	}
	return fnvMixByte(h, 0xfe)
}

func extendSetextUnderline(text string, end int) int {
	if end >= len(text) {
		return end
	}
	rest := text[end:]
	if strings.HasPrefix(rest, "\r\n") {
		rest = rest[2:]
		end += 2
	} else if strings.HasPrefix(rest, "\n") {
		rest = rest[1:]
		end++
	}
	nl := strings.IndexByte(rest, '\n')
	line := rest
	lineLen := len(rest)
	if nl >= 0 {
		line = rest[:nl]
		lineLen = nl + 1
	}
	trimmed := strings.TrimSpace(line)
	if trimmed != "" && (strings.Trim(trimmed, "=") == "" || strings.Trim(trimmed, "-") == "") {
		return end + lineLen
	}
	return end
}

// extendMathBlockEnd advances a closed MathBlock end past its closing delimiter
// line because mygo's parser rewinds b.End to the last formula content line.
func extendMathBlockEnd(text string, start, end int) (int, bool) {
	if start < 0 || end < start || end > len(text) {
		return 0, false
	}
	var closeDelim string
	switch {
	case strings.HasPrefix(text[start:], "$$"):
		closeDelim = "$$"
	case strings.HasPrefix(text[start:], `\[`):
		closeDelim = `\]`
	default:
		return 0, false
	}
	already := strings.TrimRight(text[start:end], " \t\r\n")
	if len(already) >= 2*len(closeDelim) && strings.Trim(already[len(closeDelim):], " \t\r\n") == closeDelim {
		return end, true
	}
	readLine := func(s string) (trimmed string, consumed int) {
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			consumed = nl + 1
			trimmed = strings.TrimRight(s[:nl], "\r")
		} else {
			consumed = len(s)
			trimmed = strings.TrimRight(s, "\r")
		}
		return strings.Trim(trimmed, " \t"), consumed
	}
	rest := text[end:]
	cur, n1 := readLine(rest)
	if cur == closeDelim {
		return end + n1, true
	}
	if cur == "" && n1 < len(rest) {
		next, n2 := readLine(rest[n1:])
		if next == closeDelim {
			return end + n1 + n2, true
		}
	}
	return 0, false
}

func resolveContainerSpanEnd(parsed *parser.Document, index int, endOff int) (int, bool) {
	node := parsed.Block(index)
	for dIdx := index + 1; dIdx < node.Next; dIdx++ {
		desc := parsed.Block(dIdx)
		if desc.Open && (desc.Kind == parser.MathBlock || (desc.Kind == parser.CodeBlock && desc.Fenced)) {
			return 0, false
		}
		if desc.Kind == parser.MathBlock && dIdx == node.Next-1 {
			return 0, false
		}
		endOff = max(endOff, desc.End)
	}
	return endOff, true
}

func normalizeRunsForCompare(rs []document.Run) []document.Run {
	var out []document.Run
	emit := func(txt string, st document.Style) {
		if txt == "" {
			return
		}
		if len(out) > 0 && out[len(out)-1].Style == st {
			out[len(out)-1].Text += txt
		} else {
			out = append(out, document.Run{Text: txt, Style: st})
		}
	}
	for _, r := range rs {
		if !r.Style.NoMath {
			emit(r.Text, r.Style)
			continue
		}
		// NoMath only changes how literal '$' characters are interpreted.
		plain := r.Style
		plain.NoMath = false
		pos := 0
		for i := 0; i < len(r.Text); i++ {
			if r.Text[i] == '$' {
				emit(r.Text[pos:i], plain)
				emit("$", r.Style)
				pos = i + 1
			}
		}
		emit(r.Text[pos:], plain)
	}
	return out
}
