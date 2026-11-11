package editor

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/tex"
	"github.com/egoist/mygo/ui"
)

// Inline syntax stays lossless in the model. Only layout replaces it with an
// object, with a reversible UTF-16 mapping shared by caret, selection and PDF.
type inlineToken struct {
	start, end   int
	source, note string
}
type inlineObject struct {
	start, end, index int
	box               *tex.Box
	label             *ui.TextLayout
	note              string
	rect              ui.Rect
	scale             float32
	script            bool
}
type textLayout struct {
	*ui.TextLayout
	objects []inlineObject
}

func inlineTokens(s string) []inlineToken {
	var out []inlineToken
	for i := 0; i < len(s); {
		if s[i] == '\\' {
			_, n := utf8.DecodeRuneInString(s[min(i+1, len(s)):])
			i += 1 + n
			continue
		}
		if strings.HasPrefix(s[i:], "[^") {
			if n := strings.IndexByte(s[i+2:], ']'); n > 0 {
				z := i + 2 + n + 1
				label := s[i+2 : z-1]
				if !strings.ContainsAny(label, " \t\r\n[") {
					out = append(out, inlineToken{i, z, "", label})
					i = z
					continue
				}
			}
		}
		if s[i] != '$' || i+1 >= len(s) || s[i+1] == '$' || i > 0 && s[i-1] == '$' {
			i++
			continue
		}
		first, _ := utf8.DecodeRuneInString(s[i+1:])
		if unicode.IsSpace(first) {
			i++
			continue
		}
		z := i + 1
		for z < len(s) && s[z] != '\n' {
			if s[z] == '\\' {
				z += min(2, len(s)-z)
				continue
			}
			if s[z] == '$' {
				break
			}
			z++
		}
		if z >= len(s) || s[z] != '$' {
			i++
			continue
		}
		last, _ := utf8.DecodeLastRuneInString(s[i+1 : z])
		if z == i+1 || unicode.IsSpace(last) || z+1 < len(s) && (s[z+1] >= '0' && s[z+1] <= '9' || s[z+1] == '$') {
			i++
			continue
		}
		out = append(out, inlineToken{i, z + 1, s[i+1 : z], ""})
		i = z + 1
	}
	return out
}

func literalBlock(kind string) bool {
	return kind == "code" || kind == "raw" || kind == "math" || kind == "image"
}
func tokenAllowed(b *document.Block, a, z int) bool {
	if literalBlock(b.Kind) {
		return false
	}
	for _, r := range document.SliceRuns(b.Runs, a, z) {
		if r.Style.Code || r.Style.Link != "" || r.Style.NoMath {
			return false
		}
	}
	return true
}
func (e *Editor) watchInline() {
	e.collectNotes()
	e.inlineEditing = ""
	if !e.focused || e.ReadOnly || e.printing {
		return
	}
	l := e.LeafAt(e.Caret)
	s := l.Block.Text()
	at := e.Caret - l.Start
	for _, t := range inlineTokens(s) {
		a, z := document.Len(s[:t.start]), document.Len(s[:t.end])
		if t.note == "" && at > a && at < z && e.Anchor == e.Caret && tokenAllowed(l.Block, a, z) {
			e.inlineEditing = fmt.Sprintf("%s:%d", l.Block.ID, a)
			return
		}
	}
}

func sliceSpans(spans []ui.Span, a, z int) []ui.Span {
	var out []ui.Span
	at := 0
	for _, s := range spans {
		n := document.Len(s.Text)
		lo, hi := max(0, a-at), min(n, z-at)
		if hi > lo {
			s.Text = s.Text[document.ByteOffset(s.Text, lo):document.ByteOffset(s.Text, hi)]
			out = append(out, s)
		}
		at += n
	}
	return out
}

func (e *Editor) shapeInline(b *document.Block, spans []ui.Span, font ui.Font, shape ui.TextShape) *textLayout {
	l := &textLayout{}
	text := b.Text()
	for _, token := range inlineTokens(text) {
		a, z := document.Len(text[:token.start]), document.Len(text[:token.end])
		if !tokenAllowed(b, a, z) || e.inlineEditing == fmt.Sprintf("%s:%d", b.ID, a) {
			continue
		}
		obj := inlineObject{start: a, end: z, note: token.note, scale: 1}
		if token.note != "" {
			note, ok := e.notes[token.note]
			if !ok {
				continue
			}
			obj.label = ui.ShapeText(fmt.Sprint(note.number), ui.Font{Family: font.Family, Size: font.Size * .72}, 0)
		} else {
			box, err := tex.LayoutInline(token.source, font.Size*1.1, e.measureMath)
			if err != nil {
				continue
			}
			obj.box = &box
			obj.scale = min(float32(1), max(1, shape.Width-4)/max(1, box.W))
		}
		l.objects = append(l.objects, obj)
	}
	// MyGo 0.7.4 measures BaselineShift but its glyph painter ignores g.Y.
	// Draw each script grapheme as an object, retaining native caret boundaries,
	// marks and wrapping instead of painting small glyphs on the wrong baseline.
	offset := 0
	shifts := map[int]float32{}
	for _, span := range spans {
		n := document.Len(span.Text)
		if span.BaselineShift != 0 {
			shift := span.BaselineShift
			span.BaselineShift = 0
			whole := ui.ShapeRichText([]ui.Span{span}, font, 0)
			for a := 0; a < n; {
				z := whole.NextBoundary(a)
				if z <= a {
					break
				}
				overlap := false
				for _, o := range l.objects {
					if offset+a < o.end && offset+z > o.start {
						overlap = true
						break
					}
				}
				if !overlap {
					part := span
					part.Text = span.Text[document.ByteOffset(span.Text, a):document.ByteOffset(span.Text, z)]
					label := ui.ShapeRichText([]ui.Span{part}, font, 0)
					l.objects = append(l.objects, inlineObject{start: offset + a, end: offset + z, label: label, script: true, scale: 1})
					shifts[offset+a] = shift
				}
				a = z
			}
		}
		offset += n
	}
	sort.Slice(l.objects, func(i, j int) bool { return l.objects[i].start < l.objects[j].start })
	var visible []ui.Span
	pos, delta := 0, 0
	for i := range l.objects {
		o := &l.objects[i]
		o.index = o.start - delta
		span := ui.Span{Text: "\ufffc"}
		if o.box != nil {
			span.ObjectWidth = o.box.W*o.scale + 4
			span.ObjectHeight = o.box.Ascent * o.scale
			span.ObjectDescent = o.box.Descent * o.scale
		} else {
			w, h := o.label.Size()
			span.ObjectWidth = w + 4
			span.ObjectHeight = h + font.Size*.25
			if o.script {
				base := o.label.Lines()[0].Baseline
				shift := shifts[o.start]
				span.ObjectWidth = w
				span.ObjectHeight = base + shift
				span.ObjectDescent = max(0, h-base-shift)
			}
		}
		visible = append(visible, sliceSpans(spans, pos, o.start)...)
		visible = append(visible, span)
		pos = o.end
		delta += o.end - o.start - 1
	}
	visible = append(visible, sliceSpans(spans, pos, document.Len(text))...)
	if len(visible) == 0 {
		visible = spans
	}
	l.TextLayout = ui.ShapeRichTextWith(visible, font, shape)
	for _, o := range l.TextLayout.Objects() {
		for i := range l.objects {
			if o.Index == l.objects[i].index {
				l.objects[i].rect = o.Rect
				break
			}
		}
	}
	return l
}

func (l *textLayout) visual(at int, end bool) int {
	delta := 0
	for _, o := range l.objects {
		if at <= o.start {
			break
		}
		if at < o.end {
			if end {
				return o.index + 1
			}
			return o.index
		}
		delta += o.end - o.start - 1
	}
	return at - delta
}
func (l *textLayout) model(at int) int {
	delta := 0
	for _, o := range l.objects {
		if at <= o.index {
			break
		}
		delta += o.end - o.start - 1
	}
	return at + delta
}
func (l *textLayout) Caret(at int) ui.Rect { return l.TextLayout.Caret(l.visual(at, false)) }
func (l *textLayout) IndexAt(p ui.Point) int {
	for _, o := range l.objects {
		if p.X >= o.rect.X && p.X <= o.rect.X+o.rect.W && p.Y >= o.rect.Y && p.Y <= o.rect.Y+o.rect.H {
			if o.script {
				if p.X < o.rect.X+o.rect.W/2 {
					return o.start
				}
				return o.end
			}
			return o.start + 1
		}
	}
	return l.model(l.TextLayout.IndexAt(p))
}
func (l *textLayout) SelectionRects(r ui.TextInputRange) []ui.Rect {
	return l.TextLayout.SelectionRects(ui.TextInputRange{Start: l.visual(r.Start, false), End: l.visual(r.End, true)})
}
func (l *textLayout) NextBoundary(at int) int {
	return l.model(l.TextLayout.NextBoundary(l.visual(at, false)))
}
func (l *textLayout) PreviousBoundary(at int) int {
	return l.model(l.TextLayout.PreviousBoundary(l.visual(at, true)))
}
func (l *textLayout) Lines() []ui.TextLine {
	lines := l.TextLayout.Lines()
	for i := range lines {
		lines[i].Start = l.model(lines[i].Start)
		lines[i].End = l.model(lines[i].End)
	}
	return lines
}

func (e *Editor) paintInline(p *ui.Painter, bounds ui.Rect, r row, t *ui.Theme) {
	for _, o := range r.layout.objects {
		x, y := bounds.X+r.x+o.rect.X+2, bounds.Y+r.y+o.rect.Y
		if o.box != nil {
			p.Transform(ui.Scaling(o.scale, o.scale), x, y, func() { e.paintFormula(p, *o.box, x, y+o.box.Ascent, t) })
		} else {
			if o.script {
				p.TextLayout(o.label, x-2, y, t.Text)
			} else {
				p.TextLayout(o.label, x, y, t.Accent)
			}
		}
	}
	if r.leaf.Block.Kind == "footnote" {
		n := e.notes[r.leaf.Block.NoteLabel]
		label := ui.ShapeText(fmt.Sprintf("%d ↩", n.number), ui.Font{Size: 12}, 0)
		p.TextLayout(label, bounds.X+r.rect.X, bounds.Y+r.y, t.Accent)
	}
}

type noteTarget struct {
	number, start int
	id            string
}

func (e *Editor) collectNotes() {
	if e.notesDoc == e.Doc && e.notesRevision == e.Revision() {
		return
	}
	e.notesDoc, e.notesRevision = e.Doc, e.Revision()
	defer func() { e.notesKey = e.buildNoteKey() }()
	e.notes = map[string]noteTarget{}
	leaves := e.Doc.Leaves()
	for _, l := range leaves {
		if l.Block.Kind == "footnote" {
			if _, exists := e.notes[l.Block.NoteLabel]; !exists {
				e.notes[l.Block.NoteLabel] = noteTarget{start: l.Start, id: l.Block.ID}
			}
		}
	}
	n := 0
	for _, l := range leaves {
		s := l.Block.Text()
		for _, t := range inlineTokens(s) {
			note, ok := e.notes[t.note]
			if ok && note.number == 0 && tokenAllowed(l.Block, document.Len(s[:t.start]), document.Len(s[:t.end])) {
				n++
				note.number = n
				e.notes[t.note] = note
			}
		}
	}
	for _, l := range leaves {
		if l.Block.Kind == "footnote" {
			note := e.notes[l.Block.NoteLabel]
			if note.number == 0 {
				n++
				note.number = n
				e.notes[l.Block.NoteLabel] = note
			}
		}
	}
}
func (e *Editor) noteKey() string { return e.notesKey }
func (e *Editor) buildNoteKey() string {
	keys := make([]string, 0, len(e.notes))
	for label, n := range e.notes {
		keys = append(keys, fmt.Sprintf("%s:%d", label, n.number))
	}
	sort.Strings(keys)
	return strings.Join(keys, "|")
}

// FollowFootnote also works at a keyboard caret; returning restores the exact
// originating reference, including when several references share a definition.
func (e *Editor) FollowFootnote() bool {
	l := e.LeafAt(e.Caret)
	if l.Block.Kind == "footnote" {
		return e.ReturnFootnote()
	}
	s := l.Block.Text()
	for _, t := range inlineTokens(s) {
		a, z := l.Start+document.Len(s[:t.start]), l.Start+document.Len(s[:t.end])
		if t.note != "" && e.Caret >= a && e.Caret <= z {
			if n, ok := e.notes[t.note]; ok {
				e.noteReturn, e.noteHasReturn = e.Caret, true
				e.noteOriginID, e.noteOriginOffset = l.Block.ID, e.Caret-l.Start
				e.FocusBlock(n.id)
				e.reveal, e.RequestFocus = true, true
				return true
			}
		}
	}
	return false
}
func (e *Editor) ReturnFootnote() bool {
	if !e.noteHasReturn {
		label := e.LeafAt(e.Caret).Block.NoteLabel
		if label == "" {
			return false
		}
		for _, leaf := range e.Doc.Leaves() {
			s := leaf.Block.Text()
			for _, token := range inlineTokens(s) {
				if token.note == label && tokenAllowed(leaf.Block, document.Len(s[:token.start]), document.Len(s[:token.end])) {
					e.noteReturn = leaf.Start + document.Len(s[:token.start])
					e.noteHasReturn = true
					e.noteOriginID = ""
					break
				}
			}
			if e.noteHasReturn {
				break
			}
		}
		if !e.noteHasReturn {
			return false
		}
	}
	for _, leaf := range e.Doc.Leaves() {
		if leaf.Block.ID == e.noteOriginID {
			e.noteReturn = min(leaf.End, leaf.Start+e.noteOriginOffset)
			break
		}
	}
	e.SetSelection(e.noteReturn, e.noteReturn)
	e.noteHasReturn = false
	e.reveal, e.RequestFocus = true, true
	return true
}
