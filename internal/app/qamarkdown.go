package app

import (
	"slices"
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
	cites                []agent.Citation
	partID               int
	claimID              int
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
	v.numbers, v.refs, v.byNum, v.enrichedFor = map[string]int{}, nil, nil, ""
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
					b.partBase = prev.partBase
					b.partCount = prev.partCount
					b.reused = true
				} else {
					b.runs, b.partCount = qaMarkdownRuns(in)
					b.partBase = 1
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
					tr := qaBlock{kind: qaTableRow, start: rn.Start, end: rn.End, header: rn.Header, partBase: 1}
					for cell := range v.doc.Children(row) {
						align := parser.AlignNone
						if len(tr.cells) < len(node.Align) {
							align = node.Align[len(tr.cells)]
						}
						cRuns, cParts := qaMarkdownRuns(v.doc.Inlines(cell))
						if cParts > 0 && tr.partCount > 0 {
							cRuns = offsetRunCitationIDs(cRuns, tr.partCount, false)
						}
						tr.partCount += cParts
						tr.cells = append(tr.cells, qaCell{cRuns, align})
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
	nextPart := 1
	for i := range blocks {
		b := &blocks[i]
		if b.partCount == 0 {
			continue
		}
		if delta := nextPart - b.partBase; delta != 0 {
			if len(b.runs) > 0 {
				b.runs = offsetRunCitationIDs(b.runs, delta, b.reused)
			}
			for ci := range b.cells {
				b.cells[ci].runs = offsetRunCitationIDs(b.cells[ci].runs, delta, b.reused)
			}
			b.partBase = nextPart
		}
		nextPart += b.partCount
	}
	v.blocks = blocks
}

func offsetRunCitationIDs(runs []qaRun, delta int, clone bool) []qaRun {
	if delta == 0 {
		return runs
	}
	out := runs
	if clone {
		out = slices.Clone(runs)
	}
	for i := range out {
		if out[i].partID > 0 {
			out[i].partID += delta
		}
		if out[i].claimID > 0 {
			out[i].claimID += delta
		}
	}
	return out
}

func qaMarkdownRuns(in []parser.Inline) ([]qaRun, int) {
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
				run.text, run.cites = text[ci.Start:ci.End], []agent.Citation{ci}
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
	return finalizeCitationRuns(out)
}

func isCitationSeparator(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch r {
		case ' ', '\t', ',', '，', '、', ';', '；':
		default:
			return false
		}
	}
	return true
}

func appendUniqueCitations(dst, src []agent.Citation) []agent.Citation {
	for _, ci := range src {
		k := citeKey(ci)
		dup := false
		for _, ex := range dst {
			if citeKey(ex) == k {
				dup = true
				break
			}
		}
		if !dup {
			dst = append(dst, ci)
		}
	}
	return dst
}

// finalizeCitationRuns merges adjacent/comma-separated citations into one chip,
// trims horizontal whitespace directly before a chip, and tags the supporting
// sentence or clause before each chip with a local 1-based partID/claimID.
func finalizeCitationRuns(in []qaRun) ([]qaRun, int) {
	if len(in) == 0 {
		return nil, 0
	}
	// 1. Merge adjacent citations (including "[A], [B]" / "[A] [B]").
	merged := make([]qaRun, 0, len(in))
	for i := 0; i < len(in); i++ {
		r := in[i]
		if len(r.cites) == 0 {
			if i+1 < len(in) && len(in[i+1].cites) > 0 && len(merged) > 0 && len(merged[len(merged)-1].cites) > 0 &&
				!r.code && r.url == "" && isCitationSeparator(r.text) {
				// Skip separator between two citation chips.
				continue
			}
			merged = append(merged, r)
			continue
		}
		if len(merged) > 0 && len(merged[len(merged)-1].cites) > 0 {
			last := &merged[len(merged)-1]
			last.cites = appendUniqueCitations(last.cites, r.cites)
			continue
		}
		r.cites = slices.Clone(r.cites)
		merged = append(merged, r)
	}

	// 2. Trim trailing horizontal spaces immediately before any citation chip.
	cleaned := make([]qaRun, 0, len(merged))
	for i := 0; i < len(merged); i++ {
		r := merged[i]
		if len(r.cites) > 0 {
			for len(cleaned) > 0 {
				prev := &cleaned[len(cleaned)-1]
				if len(prev.cites) > 0 || prev.code {
					break
				}
				prev.text = strings.TrimRight(prev.text, " \t")
				if prev.text == "" {
					cleaned = cleaned[:len(cleaned)-1]
					continue
				}
				break
			}
		}
		cleaned = append(cleaned, r)
	}

	// 3. Assign local partIDs and split/tag the supporting claim before each chip.
	out := make([]qaRun, 0, len(cleaned)+4)
	partID := 0
	segStart := 0
	afterPrevCite := false
	for _, r := range cleaned {
		if len(r.cites) == 0 {
			out = append(out, r)
			continue
		}
		partID++
		claimStart := segStart
		// Skip leading whitespace and, right after a previous chip, leading
		// clause/sentence punctuation so it stays un-highlighted.
		for claimStart < len(out) {
			txt := out[claimStart].text
			if out[claimStart].code || out[claimStart].url != "" {
				break
			}
			cut := leadingNonClaimOffset(txt, afterPrevCite)
			if cut == 0 {
				break
			}
			if cut == len(txt) {
				// Keep the leading delimiter run un-highlighted and continue.
				claimStart++
				continue
			}
			prefix := out[claimStart]
			prefix.text = txt[:cut]
			rest := out[claimStart]
			rest.text = txt[cut:]
			out = slices.Insert(out, claimStart+1, rest)
			out[claimStart] = prefix
			claimStart++
			break
		}

		// Split at the last sentence boundary inside [claimStart, len(out))
		// so prior sentences in the same paragraph do not light up.
		splitIdx, splitByte := findLastSentenceBoundary(out[claimStart:])
		if splitIdx >= 0 {
			absIdx := claimStart + splitIdx
			txt := out[absIdx].text
			if splitByte == len(txt) {
				claimStart = absIdx + 1
			} else if splitByte > 0 {
				prefix := out[absIdx]
				prefix.text = txt[:splitByte]
				rest := out[absIdx]
				rest.text = txt[splitByte:]
				out = slices.Insert(out, absIdx+1, rest)
				out[absIdx] = prefix
				claimStart = absIdx + 1
			}
		}

		for j := claimStart; j < len(out); j++ {
			if len(out[j].cites) == 0 {
				out[j].claimID = partID
			}
		}

		r.partID = partID
		out = append(out, r)
		segStart = len(out)
		afterPrevCite = true
	}
	return out, partID
}

func leadingNonClaimOffset(s string, stripPunct bool) int {
	i := 0
	for i < len(s) {
		r, n := utf8.DecodeRuneInString(s[i:])
		if r == ' ' || r == '\t' {
			i += n
			continue
		}
		if stripPunct && isClaimLeadingPunct(r) {
			i += n
			continue
		}
		break
	}
	return i
}

func isClaimLeadingPunct(r rune) bool {
	switch r {
	case ',', '，', '.', '。', ';', '；', ':', '：', '!', '！', '?', '？', '、',
		')', '）', '」', '』', '”', '’', '"', '\'':
		return true
	}
	return false
}

// findLastSentenceBoundary returns the run index and byte offset right after the
// last sentence boundary (including trailing closing quotes and horizontal
// whitespace) within runs, or (-1, 0) if none exists.
func findLastSentenceBoundary(runs []qaRun) (int, int) {
	bestRun, bestByte := -1, 0
	for ri, r := range runs {
		if r.code || r.url != "" || len(r.cites) > 0 {
			continue
		}
		s := r.text
		for i := 0; i < len(s); {
			ch, n := utf8.DecodeRuneInString(s[i:])
			next := i + n
			switch ch {
			case '\n':
				cut := consumeHorizontalSpace(s, next)
				bestRun, bestByte = ri, cut
			case '。', '！', '？':
				cut := consumeClosingQuotesAndSpace(s, next)
				bestRun, bestByte = ri, cut
			case '.', '!', '?':
				followedBySpace := false
				if next < len(s) {
					nr, _ := utf8.DecodeRuneInString(s[next:])
					followedBySpace = nr == ' ' || nr == '\t' || nr == '\n'
				} else if ri+1 < len(runs) && len(runs[ri+1].text) > 0 {
					nr, _ := utf8.DecodeRuneInString(runs[ri+1].text)
					followedBySpace = nr == ' ' || nr == '\t' || nr == '\n'
				}
				if followedBySpace {
					cut := consumeClosingQuotesAndSpace(s, next)
					bestRun, bestByte = ri, cut
				}
			}
			i = next
		}
	}
	return bestRun, bestByte
}

func consumeHorizontalSpace(s string, pos int) int {
	for pos < len(s) {
		r, n := utf8.DecodeRuneInString(s[pos:])
		if r == ' ' || r == '\t' {
			pos += n
			continue
		}
		break
	}
	return pos
}

func consumeClosingQuotesAndSpace(s string, pos int) int {
	for pos < len(s) {
		r, n := utf8.DecodeRuneInString(s[pos:])
		if r == '”' || r == '’' || r == '」' || r == '』' || r == '"' || r == '\'' || r == ')' || r == '）' {
			pos += n
			continue
		}
		break
	}
	return consumeHorizontalSpace(s, pos)
}

func qaRunChars(r qaRun) int {
	if r.code {
		return 0
	} // upstream skips pre/code/svg/math
	if len(r.cites) > 0 {
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
// headings and recalled plain-text excerpts once per answer revision.
func (v *qaTurnView) enrich(fs *spacefs.FS) {
	if v.enrichedFor == v.source && (len(v.refs) == 0 || len(v.byNum) == len(v.refs)) {
		return
	}
	v.enrichedFor = v.source
	v.byNum = make(map[int]citeRef, len(v.refs))

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
	}
}
