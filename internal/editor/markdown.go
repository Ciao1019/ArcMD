package editor

import (
	"fmt"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/ui/markdown/parser"
)

// ImportMarkdown adapts interchange syntax to the native document tree. The
// original is retained in ImportSource for recovery alongside opaque blocks.
func ImportMarkdown(source string) *document.Document {
	result := document.New()
	result.Blocks = nil
	result.ImportSource = source
	text := source
	if strings.HasPrefix(text, "---\n") || strings.HasPrefix(text, "---\r\n") {
		lines := strings.SplitAfter(text, "\n")
		end := len(lines[0])
		for _, line := range lines[1:] {
			end += len(line)
			if strings.TrimSpace(line) == "---" || strings.TrimSpace(line) == "..." {
				result.Blocks = append(result.Blocks, document.NewBlock("raw", text[:end]))
				text = text[end:]
				break
			}
		}
	}
	parsed := parser.New(parser.Default | parser.ExtDollarMath)
	parsed.Set(text)
	var blocks func(int, string, int) []*document.Block
	blocks = func(parent int, context string, indent int) []*document.Block {
		var out []*document.Block
		for index := range parsed.Children(parent) {
			node := parsed.Block(index)
			b := document.NewBlock("paragraph", "")
			b.Indent = indent
			switch node.Kind {
			case parser.Paragraph, parser.Heading:
				b.Runs = importRuns(parsed.Inlines(index), document.Style{})
				if context != "" {
					b.Kind = context
				}
				if node.Kind == parser.Heading {
					b.Kind, b.Level = "heading", node.Level
				}
				in := parsed.Inlines(index)
				if len(in) > 0 && in[0].Kind == parser.Image && in[0].Next == len(in) {
					b.Kind, b.URL = "image", in[0].Dest
					b.Runs = []document.Run{{Text: parser.PlainText(in[1:])}}
				}
			case parser.Quote:
				children := blocks(index, "quote", indent)
				if color, ok := alertColor(children); ok {
					for _, child := range children {
						child.Kind, child.Color = "callout", color
					}
					if len(children) > 1 && children[0].Text() == "" {
						children = children[1:]
					}
				}
				out = append(out, children...)
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
					children := blocks(item, itemKind, depth)
					for _, child := range children {
						if child.Kind == "task" {
							child.Checked = it.Task == parser.Checked
						}
					}
					out = append(out, children...)
				}
				continue
			case parser.FootnoteDefinition:
				b.Kind, b.NoteLabel = "footnote", node.Label()
				for i, child := range blocks(index, "", 0) {
					if i > 0 {
						b.Runs = append(b.Runs, document.Run{Text: "\u2028"})
					}
					b.Runs = append(b.Runs, child.Runs...)
				}
			case parser.CodeBlock:
				b.Kind, b.Language = "code", node.Language()
				b.Runs = []document.Run{{Text: strings.TrimSuffix(parsed.Lines(index), "\n")}}
			case parser.ThematicBreak:
				b.Kind = "rule"
			case parser.Table:
				b.Kind, b.Runs = "table", nil
				for row := range parsed.Children(index) {
					tr := &document.Block{ID: document.NewID(), Kind: "row"}
					col := 0
					for cell := range parsed.Children(row) {
						td := document.NewBlock("cell", "")
						td.Runs = importRuns(parsed.Inlines(cell), document.Style{})
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
			default:
				b.Kind = "raw"
				b.Runs = []document.Run{{Text: text[node.Start:node.End]}}
				if node.Kind == parser.MathBlock {
					b.Kind = "math"
					b.Runs = []document.Run{{Text: strings.TrimSpace(parsed.Lines(index))}}
				}
			}
			out = append(out, b)
		}
		return out
	}
	result.Blocks = append(result.Blocks, blocks(0, "", 0)...)
	if len(result.Blocks) == 0 {
		result.Blocks = []*document.Block{document.NewBlock("paragraph", text)}
	}
	return result
}

func importRuns(in []parser.Inline, style document.Style) []document.Run {
	var walk func(int, int, document.Style) []document.Run
	walk = func(start, stop int, style document.Style) []document.Run {
		var runs []document.Run
		for i := start; i < stop; {
			node := in[i]
			end := min(stop, max(i+1, node.Next))
			if node.Kind == parser.HTML {
				switch strings.ToLower(node.Text) {
				case "<sup>":
					style.Script = "sup"
				case "<sub>":
					style.Script = "sub"
				case "</sup>", "</sub>":
					style.Script = ""
				case "<u>":
					style.Underline = true
				case "</u>":
					style.Underline = false
				default:
					runs = append(runs, document.Run{Text: node.Text, Style: style})
				}
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
				next.Code = true
			case parser.Text:
				next.NoMath = strings.Contains(node.Text, "$")
			}
			if end > i+1 && node.Kind != parser.Image {
				runs = append(runs, walk(i+1, end, next)...)
			} else {
				text := node.Text
				switch node.Kind {
				case parser.SoftBreak, parser.LineBreak:
					text = "\n"
				case parser.FootnoteRef:
					text = "[^" + node.Text + "]"
				case parser.Math:
					text = "$" + node.Text + "$"
				case parser.Image:
					text = "![" + parser.PlainText(in[i+1:end]) + "](" + node.Dest + ")"
				}
				runs = append(runs, document.Run{Text: text, Style: next})
			}
			i = end
		}
		return document.Compact(runs)
	}
	return walk(0, len(in), style)
}

func ExportMarkdown(doc *document.Document) string {
	var render func([]*document.Block) string
	render = func(blocks []*document.Block) string {
		var parts []string
		for _, b := range blocks {
			text := exportRuns(b.Runs)
			switch b.Kind {
			case "footnote":
				text = "[^" + b.NoteLabel + "]: " + strings.ReplaceAll(text, "\n", "\n    ")
			case "heading":
				text = strings.Repeat("#", max(1, b.Level)) + " " + text
			case "bullet":
				text = strings.Repeat("  ", b.Indent) + "- " + text
			case "numbered":
				text = strings.Repeat("  ", b.Indent) + "1. " + text
			case "task":
				check := " "
				if b.Checked {
					check = "x"
				}
				text = "- [" + check + "] " + text
			case "quote":
				text = "> " + strings.ReplaceAll(text, "\n", "\n> ")
			case "callout":
				text = "> [!" + alertNames[b.Color] + "]\n> " + strings.ReplaceAll(text, "\n", "\n> ")
			case "code":
				fence := "```"
				for strings.Contains(b.Text(), fence) {
					fence += "`"
				}
				text = fence + b.Language + "\n" + b.Text() + "\n" + fence
			case "image":
				text = "![" + text + "](" + b.URL + ")"
			case "rule":
				text = "---"
			case "raw":
				text = b.Text()
			case "math":
				text = "$$\n" + b.Text() + "\n$$"
			case "table":
				var rows []string
				for i, row := range b.Children {
					var cells []string
					for _, cell := range row.Children {
						cells = append(cells, strings.ReplaceAll(strings.ReplaceAll(exportRuns(cell.Runs), "|", "\\|"), "\n", "<br>"))
					}
					rows = append(rows, "| "+strings.Join(cells, " | ")+" |")
					if i == 0 {
						var bars []string
						for _, cell := range row.Children {
							bar := "---"
							if cell.Align == "center" {
								bar = ":---:"
							}
							if cell.Align == "right" {
								bar = "---:"
							}
							bars = append(bars, bar)
						}
						rows = append(rows, "| "+strings.Join(bars, " | ")+" |")
					}
				}
				text = strings.Join(rows, "\n")
			case "columns":
				var cols []string
				for _, column := range b.Children {
					cols = append(cols, "<div>\n\n"+render(column.Children)+"\n</div>")
				}
				text = "<div class=\"arcmd-columns\">\n" + strings.Join(cols, "\n") + "\n</div>"
			}
			parts = append(parts, text)
		}
		return strings.Join(parts, "\n\n") + "\n"
	}
	return render(doc.Blocks)
}

func exportRuns(runs []document.Run) string {
	// Markdown has no text colors: runs that differ only in them merge, so
	// marks don't break apart at a color.
	plain := make([]document.Run, len(runs))
	for i, r := range runs {
		r.Style.Color, r.Style.Highlight = "", ""
		plain[i] = r
	}
	runs = document.Compact(plain)
	var out strings.Builder
	escape := strings.NewReplacer("\\", "\\\\", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "`", "\\`")
	for _, run := range runs {
		source := strings.ReplaceAll(run.Text, "\u2028", "  \n")
		var escaped strings.Builder
		pos := 0
		for _, token := range inlineTokens(source) {
			if run.Style.NoMath && token.note == "" {
				continue
			}
			escaped.WriteString(escape.Replace(source[pos:token.start]))
			escaped.WriteString(source[token.start:token.end])
			pos = token.end
		}
		escaped.WriteString(escape.Replace(source[pos:]))
		text := escaped.String()
		if run.Style.NoMath {
			text = strings.ReplaceAll(text, "$", `\$`)
		}
		s := run.Style
		if s.Code {
			fence := "`"
			for strings.Contains(run.Text, fence) {
				fence += "`"
			}
			text = fence + " " + run.Text + " " + fence
		}
		if s.Bold {
			text = "**" + text + "**"
		}
		if s.Italic {
			text = "*" + text + "*"
		}
		if s.Strike {
			text = "~~" + text + "~~"
		}
		if s.Script != "" {
			text = "<" + s.Script + ">" + text + "</" + s.Script + ">"
		}
		if s.Underline {
			text = "<u>" + text + "</u>"
		}
		if s.Link != "" {
			text = fmt.Sprintf("[%s](%s)", text, s.Link)
		}
		out.WriteString(text)
	}
	return out.String()
}

// alertNames are the GitHub alerts that carry callout colors in Markdown.
var alertNames = map[string]string{"": "NOTE", "green": "TIP", "purple": "IMPORTANT", "yellow": "WARNING", "red": "CAUTION"}

// alertColor reads a GitHub alert's marker, "[!WARNING]", from the start of
// a quote, takes it out of the text and gives the callout color it names.
// An unknown alert is a note.
func alertColor(children []*document.Block) (string, bool) {
	if len(children) == 0 {
		return "", false
	}
	first := children[0]
	text := first.Text()
	end := strings.Index(text, "]")
	if !strings.HasPrefix(text, "[!") || end < 0 || strings.ContainsAny(text[:end], " \n") {
		return "", false
	}
	name := strings.ToUpper(text[2:end])
	color := ""
	for c, n := range alertNames {
		if n == name {
			color = c
		}
	}
	rest := text[end+1:]
	trimmed := strings.TrimLeft(rest, " \t\n\u2028")
	start := document.Len(text) - document.Len(trimmed)
	first.Runs = document.SliceRuns(first.Runs, start, document.Len(text))
	return color, true
}
