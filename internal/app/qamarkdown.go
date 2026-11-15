package app

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/agent"
	"github.com/Ciao1019/ArcMD/internal/spacefs"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui/markdown/parser"
)

type qaRun struct {
	text                 string
	weight               int
	italic, strike, code bool
	url                  string
	pending              bool
	cite                 *agent.Citation
}

type qaCell struct {
	runs  []qaRun
	align parser.Align
}

// Keep the incremental CommonMark tree, including its healed streaming tail.
// Completed blocks keep their runs and animation clocks across appends.
func (v *qaTurnView) parse(answer string, streaming bool) {
	if v.source == answer && v.streaming == streaming {
		return
	}
	old := make(map[int]qaBlock, len(v.blocks))
	if strings.HasPrefix(answer, v.source) {
		for _, b := range v.blocks {
			old[b.start] = b
		}
	}
	if v.doc == nil {
		v.doc = parser.New(parser.GFM)
	}
	v.doc.SetStreaming(streaming)
	v.doc.Set(answer)
	v.source, v.streaming = answer, streaming
	v.tailUnits = 0
	for i := range v.doc.Children(0) {
		b := v.doc.Block(i)
		v.tailUnits = 0
		for _, r := range answer[b.Start:b.End] {
			v.tailUnits++
			if r > 0xffff {
				v.tailUnits++
			}
		}
	}
	v.numbers, v.refs = map[string]int{}, nil
	for _, ci := range agent.Citations(answer) {
		key := citeKey(ci)
		if v.numbers[key] == 0 {
			n := len(v.refs) + 1
			v.numbers[key] = n
			v.refs = append(v.refs, citeRef{
				n:      n,
				cite:   ci,
				title:  vault.Title(ci.Path),
				folder: docFolderBreadcrumb(ci.Path),
			})
		}
	}
	blocks := make([]qaBlock, 0, len(v.blocks)+1)
	var walk func(int, int, bool, string)
	walk = func(parent, depth int, quoted bool, marker string) {
		for index := range v.doc.Children(parent) {
			node := v.doc.Block(index)
			b := qaBlock{start: node.Start, end: node.End, indent: depth, marker: marker, raw: answer[node.Start:node.End]}
			switch node.Kind {
			case parser.Quote:
				walk(index, depth, true, marker)
				continue
			case parser.List:
				number := node.Number
				for item := range v.doc.Children(index) {
					mark := "•"
					if depth%2 == 1 {
						mark = "◦"
					}
					if node.Ordered {
						mark = strconv.Itoa(number) + "."
						number++
					}
					switch v.doc.Block(item).Task {
					case parser.Unchecked:
						mark = "☐"
					case parser.Checked:
						mark = "☑"
					}
					walk(item, depth+1, quoted, mark)
				}
				continue
			case parser.Paragraph, parser.Heading:
				in := v.doc.Inlines(index)
				b.revision = v.doc.Block(index).Revision()
				b.text = strings.TrimSpace(v.doc.Lines(index))
				if quoted {
					b.kind = qaQuote
				}
				if marker != "" {
					b.kind = qaListItem
				}
				if node.Kind == parser.Heading {
					b.kind = qaHeading
					b.size = []float32{19.25, 17.5, 15.75, 14.875, 14, 14}[node.Level-1]
				}
				if prev, ok := old[b.start]; ok && prev.revision == b.revision && prev.kind == b.kind && prev.text == b.text {
					b.runs = prev.runs
				} else {
					b.runs = qaMarkdownRuns(in)
				}
			case parser.CodeBlock, parser.HTMLBlock:
				b.kind, b.language = qaCodeBlock, node.Language()
				b.text = v.doc.Lines(index)
				if !node.Open {
					b.text = strings.TrimSuffix(b.text, "\n")
				}
			case parser.ThematicBreak:
				b.kind = qaRule
			case parser.Table:
				for row := range v.doc.Children(index) {
					rn := v.doc.Block(row)
					tr := qaBlock{kind: qaTableRow, start: rn.Start, end: rn.End, header: rn.Header}
					for cell := range v.doc.Children(row) {
						align := parser.AlignNone
						if len(tr.cells) < len(node.Align) {
							align = node.Align[len(tr.cells)]
						}
						tr.cells = append(tr.cells, qaCell{qaMarkdownRuns(v.doc.Inlines(cell)), align})
					}
					tr.text = answer[tr.start:tr.end]
					tr.raw = tr.text
					tr.fade = old[tr.start].fade
					if tr.fade == nil {
						tr.fade = &qaFade{}
					}
					blocks = append(blocks, tr)
				}
				continue
			default:
				continue
			}
			b.fade = old[b.start].fade
			if b.fade == nil {
				b.fade = &qaFade{}
			}
			blocks = append(blocks, b)
			marker = "" // a continuation paragraph has no second bullet
		}
	}
	walk(0, 0, false, "")
	v.blocks = blocks
}

func qaMarkdownRuns(in []parser.Inline) []qaRun {
	var out []qaRun
	add := func(text string, style qaRun) {
		if text == "" {
			return
		}
		// Citations are the app's extension, kept as an interactive inline
		// object. Code and destinations are never interpreted as citations.
		pos := 0
		if !style.code && style.url == "" {
			for _, ci := range agent.Citations(text) {
				if ci.Start > pos {
					run := style
					run.text = text[pos:ci.Start]
					out = append(out, run)
				}
				run := style
				run.text, run.cite = text[ci.Start:ci.End], &ci
				out = append(out, run)
				pos = ci.End
			}
		}
		if pos < len(text) {
			style.text = text[pos:]
			out = append(out, style)
		}
	}
	var walk func(int, int, qaRun)
	walk = func(start, end int, style qaRun) {
		for i := start; i < end; {
			n := in[i]
			next := min(end, max(i+1, n.Next))
			s := style
			switch n.Kind {
			case parser.Strong:
				s.weight = 600
			case parser.Emphasis:
				s.italic = true
			case parser.Strikethrough:
				s.strike = true
			case parser.Code:
				s.code = true
			case parser.Link:
				s.url, s.pending = n.Dest, n.Pending
			}
			if next > i+1 {
				walk(i+1, next, s)
			} else {
				text := n.Text
				switch n.Kind {
				case parser.SoftBreak:
					text = " "
				case parser.LineBreak:
					text = "\n"
				}
				add(text, s)
			}
			i = next
		}
	}
	walk(0, len(in), qaRun{})
	return out
}

func qaRunChars(r qaRun) int {
	if r.code {
		return 0
	} // upstream skips pre/code/svg/math
	if r.cite != nil {
		return 1
	}
	return utf8.RuneCountInString(r.text)
}

func (v *qaTurnView) animate(now time.Time, enabled bool) {
	if v.animated && v.animatedSource == v.source && v.animationEnabled == enabled {
		return
	}
	v.animated, v.animatedSource, v.animationEnabled = true, v.source, enabled
	gap := now.Sub(v.revealAt)
	for i := range v.blocks {
		b := &v.blocks[i]
		count := 0
		for _, r := range b.runs {
			count += qaRunChars(r)
		}
		for _, cell := range b.cells {
			for _, r := range cell.runs {
				count += qaRunChars(r)
			}
		}
		b.fade.update(now, count, utf8.RuneCountInString(b.raw), i == len(v.blocks)-1, enabled, gap)
	}
	v.revealAt = now
}

// enrich resolves clean document titles, folder breadcrumbs, enclosing section
// headings and recalled plain-text excerpts once per answer revision, grouping
// passages under their parent document.
func (v *qaTurnView) enrich(fs *spacefs.FS) {
	if v.enrichedFor == v.source && len(v.groups) > 0 {
		return
	}
	v.enrichedFor = v.source
	v.groups = v.groups[:0]
	v.byNum = make(map[int]citeRef, len(v.refs))
	groupIdx := map[string]int{}

	for i := range v.refs {
		r := &v.refs[i]
		r.title = vault.Title(r.cite.Path)
		r.folder = docFolderBreadcrumb(r.cite.Path)
		r.section = ""
		r.excerpt = ""
		if fs != nil {
			if p, err := fs.Page(r.cite.Path); err == nil {
				if p.Title != "" {
					r.title = p.Title
				}
				loc := p.Describe(r.cite.From, r.cite.To)
				if loc.Section != "" && loc.Section != r.title {
					r.section = loc.Section
				}
				r.excerpt = loc.Excerpt
			}
		}
		v.byNum[r.n] = *r
		idx, ok := groupIdx[r.cite.Path]
		if !ok {
			idx = len(v.groups)
			groupIdx[r.cite.Path] = idx
			v.groups = append(v.groups, citeGroup{
				path:   r.cite.Path,
				title:  r.title,
				folder: r.folder,
			})
		}
		v.groups[idx].items = append(v.groups[idx].items, *r)
	}
}
