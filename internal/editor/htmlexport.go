package editor

import (
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/mermaid"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func isSafeLangClass(lang string) bool {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return false
	}
	for i := 0; i < len(lang); i++ {
		c := lang[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '+' || c == '-') {
			return false
		}
	}
	return true
}

func safeHTMLURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "", "http", "https", "mailto", "file", "tel":
		return true
	}
	return false
}

// ExportHTML supplies semantic clipboard content without depending on a web
// runtime. Escaping also keeps raw Markdown/TeX and untrusted links inert.
// Code blocks keep their text and Mermaid blocks their source: a clipboard
// has no page style sheet to color or place them.
func ExportHTML(doc *document.Document) string { return exportHTML(doc, nil) }

// HTMLStyle is the paper an exported page draws with: the document theme and
// the page's colors beyond it.
type HTMLStyle struct {
	Theme *ui.Theme
	Page  Page
}

// HTMLTheme is the pair of papers an exported page follows: the light one, and
// the dark one a reader whose system prefers dark gets. A missing theme takes
// the default light or dark theme.
type HTMLTheme struct {
	Light, Dark HTMLStyle
}

// ExportHTMLPage renders the body of an exported page in its paper theme:
// code blocks keep the editor's highlighting and Mermaid diagrams are drawn
// as inline SVG, one picture for each system appearance.
func ExportHTMLPage(doc *document.Document, theme HTMLTheme) string {
	if theme.Light.Theme == nil {
		theme.Light.Theme = ui.LightTheme()
	}
	if theme.Dark.Theme == nil {
		theme.Dark.Theme = ui.DarkTheme()
	}
	return exportHTML(doc, &theme)
}

// exportHTML writes a document's blocks; a nil theme is the clipboard's plain
// HTML, which has no page style sheet.
func exportHTML(doc *document.Document, theme *HTMLTheme) string {
	var blocks func([]*document.Block) string
	runs := func(rs []document.Run) string {
		return exportHTMLRuns(rs, false)
	}
	blocks = func(bs []*document.Block) string {
		var out strings.Builder
		for i := 0; i < len(bs); i++ {
			b := bs[i]
			body := runs(b.Runs)
			tag := "p"
			switch b.Kind {
			case "heading":
				tag = fmt.Sprintf("h%d", max(1, min(6, b.Level)))
			case "quote", "callout":
				tag = "blockquote"
			case "bullet", "numbered", "task":
				var list func(int)
				list = func(depth int) {
					kind := bs[i].Kind
					listTag := "ul"
					if kind == "numbered" {
						listTag = "ol"
					}
					out.WriteString("<" + listTag + ">")
					for i < len(bs) && bs[i].Indent == depth && bs[i].Kind == kind {
						item := bs[i]
						out.WriteString("<li>")
						if item.Kind == "task" {
							if item.Checked {
								out.WriteString("☑ ")
							} else {
								out.WriteString("☐ ")
							}
						}
						out.WriteString(runs(item.Runs))
						i++
						for i < len(bs) && isHTMLList(bs[i]) && bs[i].Indent > depth {
							list(bs[i].Indent)
						}
						out.WriteString("</li>")
					}
					out.WriteString("</" + listTag + ">")
				}
				list(b.Indent)
				i--
				continue
			case "code":
				if isWhiteboardBlock(b) {
					if scene, err := whiteboard.Decode(b.Text()); err == nil {
						out.WriteString(`<figure class="arcmd-whiteboard">` + whiteboard.ExportSVG(scene, whiteboard.DefaultLightPalette()) + `</figure>`)
						continue
					}
				}
				if theme != nil && normalizeLexLang(b.Language) == langMermaid {
					if svg, ok := mermaidSVG(b.Text(), *theme); ok {
						out.WriteString(`<figure class="arcmd-mermaid">` + svg + `</figure>`)
						continue
					}
				}
				classAttr := ""
				if isSafeLangClass(b.Language) {
					classAttr = ` class="language-` + html.EscapeString(strings.ToLower(strings.TrimSpace(b.Language))) + `"`
				}
				code := html.EscapeString(b.Text())
				if theme != nil {
					code = htmlCode(b.Text(), b.Language)
				}
				out.WriteString("<pre><code" + classAttr + ">" + code + "</code></pre>")
				continue
			case "toggle":
				var inner []*document.Block
				for i+1 < len(bs) && bs[i+1].Kind != "heading" && bs[i+1].Indent > b.Indent {
					c := *bs[i+1]
					c.Indent -= b.Indent + 1
					inner = append(inner, &c)
					i++
				}
				open := ""
				if !b.Folded {
					open = " open"
				}
				out.WriteString("<details" + open + "><summary>" + body + "</summary>" + blocks(inner) + "</details>")
				continue
			case "raw":
				if isHTMLCommentBlock(b.Text()) {
					continue
				}
				tag = "pre"
				body = html.EscapeString(b.Text())
			case "math":
				tag = "pre"
				body = "$$\n" + html.EscapeString(b.Text()) + "\n$$"
			case "rule":
				out.WriteString("<hr>")
				continue
			case "image":
				if safeHTMLURL(b.URL) {
					if IsPDFURL(b.URL) {
						h := float32(480)
						out.WriteString(`<figure class="arcmd-pdf"><object data="` + html.EscapeString(b.URL) + `" type="application/pdf" width="100%" height="` + fmt.Sprintf("%.0f", h) + `"><a href="` + html.EscapeString(b.URL) + `">` + html.EscapeString(imageName(b.URL)) + `</a></object></figure>`)
						continue
					}
					out.WriteString("<img src=\"" + html.EscapeString(b.URL) + "\" alt=\"" + html.EscapeString(b.Text()) + "\"")
					if b.ImageWidth > 0 {
						fmt.Fprintf(&out, " width=\"%.0f\"", b.ImageWidth)
					}
					out.WriteString(">")
				}
				continue
			case "table":
				out.WriteString("<table><tbody>")
				covered := document.CoveredCells(b)
				for i, row := range b.Children {
					out.WriteString("<tr>")
					for _, cell := range row.Children {
						if covered[cell] {
							continue
						}
						tag := "td"
						if i == 0 {
							tag = "th"
						}
						cols, rows := document.Span(cell)
						fmt.Fprintf(&out, "<%s colspan=\"%d\" rowspan=\"%d\">%s</%s>", tag, cols, rows, runs(cell.Runs), tag)
					}
					out.WriteString("</tr>")
				}
				out.WriteString("</tbody></table>")
				continue
			case "columns":
				ratios := columnRatioStrings(b)
				if len(ratios) == len(b.Children) {
					fmt.Fprintf(&out, `<div class="arcmd-columns" data-widths="%s" style="display:flex;gap:24px">`, strings.Join(ratios, ","))
				} else {
					out.WriteString(`<div class="arcmd-columns" style="display:flex;gap:24px">`)
				}
				for ci, col := range b.Children {
					flexStyle := "flex:1;min-width:0"
					if len(ratios) == len(b.Children) {
						flexStyle = "flex:" + ratios[ci] + ";min-width:0"
					}
					fmt.Fprintf(&out, `<div class="arcmd-column" style="%s">%s</div>`, flexStyle, blocks(col.Children))
				}
				out.WriteString(`</div>`)
				continue
			case "column":
				out.WriteString(blocks(b.Children))
				continue
			case "footnote":
				body = "[" + html.EscapeString(b.NoteLabel) + "] " + body
			}
			attrs := ""
			if (b.Align == "center" || b.Align == "right") && tag != "pre" {
				attrs = ` style="text-align:` + b.Align + `"`
			}
			out.WriteString("<" + tag + attrs + ">" + body + "</" + tag + ">")
		}
		return out.String()
	}
	return "<html><body><!--StartFragment-->" + blocks(doc.Blocks) + "<!--EndFragment--></body></html>"
}

func isHTMLList(b *document.Block) bool {
	return b.Kind == "bullet" || b.Kind == "numbered" || b.Kind == "task"
}

// htmlLexerPage gives every token kind a color of its own so htmlCode can
// recover the kind from the lexer's spans. These colors never reach the
// page: the exported HTML's CSS colors the classes instead.
var htmlLexerPage = Page{
	CodeText: ui.RGB(0, 0, 0),
	Tokens: CodeTokens{
		Comment:  ui.RGB(1, 0, 0),
		Keyword:  ui.RGB(2, 0, 0),
		String:   ui.RGB(3, 0, 0),
		Number:   ui.RGB(4, 0, 0),
		Type:     ui.RGB(5, 0, 0),
		Function: ui.RGB(6, 0, 0),
	},
}

// htmlCode renders a code block with its tokens wrapped in the classes the
// exported page's style sheet colors, so a shared file keeps its highlighting
// and follows the reader's light or dark preference.
func htmlCode(text, language string) string {
	var out strings.Builder
	for _, sp := range codeSpansFor("", language, text, htmlLexerPage, nil) {
		class := htmlTokenClass(sp.Color)
		if class == "" {
			out.WriteString(html.EscapeString(sp.Text))
			continue
		}
		out.WriteString(`<span class="` + class + `">` + html.EscapeString(sp.Text) + `</span>`)
	}
	return out.String()
}

func htmlTokenClass(c ui.Color) string {
	switch c {
	case htmlLexerPage.Tokens.Comment:
		return "arcmd-tok-comment"
	case htmlLexerPage.Tokens.Keyword:
		return "arcmd-tok-keyword"
	case htmlLexerPage.Tokens.String:
		return "arcmd-tok-string"
	case htmlLexerPage.Tokens.Number:
		return "arcmd-tok-number"
	case htmlLexerPage.Tokens.Type:
		return "arcmd-tok-type"
	case htmlLexerPage.Tokens.Function:
		return "arcmd-tok-function"
	}
	return ""
}

// CodeTokenCSS writes the rules that color the token classes htmlCode emits,
// from the page tokens the editor paints with, so an exported page owns no
// colors of its own.
func CodeTokenCSS(tokens CodeTokens) string {
	classes := []struct {
		name  string
		color ui.Color
	}{
		{"comment", tokens.Comment},
		{"keyword", tokens.Keyword},
		{"string", tokens.String},
		{"number", tokens.Number},
		{"type", tokens.Type},
		{"function", tokens.Function},
	}
	var b strings.Builder
	for _, c := range classes {
		fmt.Fprintf(&b, "pre code .arcmd-tok-%s { color: %s; }\n", c.name, cssColor(c.color))
	}
	return b.String()
}

// mermaidSVG lays out a mermaid block with the system font's real metrics and
// draws it as inline SVG, the way the editor and the vector PDF export draw
// it. Both system appearances are written; the page's CSS shows one. A block
// that is not a diagram ArcMD understands keeps its source.
func mermaidSVG(src string, theme HTMLTheme) (string, bool) {
	diagram, err := mermaid.ParseAndLayout(src, exportMermaidMeasure)
	if err != nil || diagram.W <= 0 || diagram.H <= 0 {
		return "", false
	}
	alt := i18n.T("流程图")
	if diagram.Kind == "sequence" {
		alt = i18n.T("时序图")
	}
	light := mermaid.ExportSVG(diagram, exportMermaidPalette(theme.Light), alt)
	dark := mermaid.ExportSVG(diagram, exportMermaidPalette(theme.Dark), alt)
	return `<span class="arcmd-diagram-light">` + light + `</span><span class="arcmd-diagram-dark">` + dark + `</span>`, true
}

// exportMermaidMeasure measures a diagram label with the system font the
// exported page shows, so the SVG's boxes and its text agree; mermaid's
// default estimate narrows wide letters such as W.
func exportMermaidMeasure(text string, size float32, bold bool) (w, h float32) {
	if text == "" {
		return 0, size * 1.3
	}
	weight := 400
	if bold {
		weight = 600
	}
	l := ui.ShapeText(text, ui.Font{Family: mermaid.FontFamily, Size: size, Weight: weight}, 0)
	return l.Size()
}

// exportMermaidPalette colors an exported diagram from the page's own paper
// tokens, matching what mermaidShapeColors paints in the editor. ExportSVG
// paints the paper first, so the translucent tints composite as they do on a
// page.
func exportMermaidPalette(style HTMLStyle) mermaid.Palette {
	t := style.Theme
	pg := pageOf(style.Page, t)
	blue := mermaidBlueCallout(pg)
	yellow := pg.Callouts["yellow"]
	return mermaid.Palette{
		Text:             cssColor(t.Text),
		Caption:          cssColor(pg.Caption),
		Paper:            cssColor(t.Background),
		NodeFill:         cssColor(pg.CodeFill),
		NodeBorder:       cssColor(t.Accent.Alpha(0.45)),
		ContainerFill:    cssColor(pg.CodeFill.Alpha(0.50)),
		ContainerBorder:  cssColor(pg.CodeBorder),
		KeywordFill:      cssColor(pg.CodeHover.Alpha(0.28)),
		KeywordBorder:    cssColor(pg.CodeBorder),
		ActorFill:        cssColor(blue.Fill),
		ActorBorder:      cssColor(blue.Border),
		NoteFill:         cssColor(yellow.Fill),
		NoteBorder:       cssColor(yellow.Border),
		ActivationFill:   cssColor(t.Accent.Alpha(0.18)),
		ActivationBorder: cssColor(t.Accent.Alpha(0.68)),
	}
}

// cssColor writes a color as a CSS value.
func cssColor(c ui.Color) string {
	s := c.SRGB()
	if s.A == 255 {
		return fmt.Sprintf("#%02x%02x%02x", s.R, s.G, s.B)
	}
	return fmt.Sprintf("rgba(%d,%d,%d,%.3f)", s.R, s.G, s.B, float64(s.A)/255)
}
