package editor

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/tex"
	"github.com/egoist/mygo/ui"
)

type inlineMathEdit struct {
	blockID    string
	start, end int
	source     string
}

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
	sourceLayout      *ui.TextLayout
	source            string
	note              string
	image             document.InlineImage
	imageLink         string
	imageRes          ImageResource
	imgW, imgH        float32
	imgDescent        float32
	hasImage          bool
	rect              ui.Rect
	scale             float32
	script            bool
	editing           bool
	placeholder       bool
	hasErr            bool
}

type textLayout struct {
	*ui.TextLayout
	objects []inlineObject
}

func inlineTokens(s string) []inlineToken {
	return scanInlineTokens(s, nil)
}

func runDisallowsInlineToken(st document.Style) bool {
	return st.Code || st.Kbd || st.Link != "" || st.NoMath
}

func (e *Editor) inlineImageMetrics(img document.InlineImage, res ImageResource, font ui.Font, maxW float32) (w, h, descent float32, chip *ui.TextLayout) {
	z := e.zoomFactor()
	iw, ih := res.IntrinsicSize()
	switch {
	case res.HasVisual() && iw > 0 && ih > 0:
		switch {
		case img.Width > 0 && img.Height > 0:
			w, h = img.Width*z, img.Height*z
		case img.Width > 0:
			w = img.Width * z
			h = w * ih / iw
		case img.Height > 0:
			h = img.Height * z
			w = h * iw / ih
		default:
			w, h = iw*z, ih*z
		}
	case res.Loading:
		switch {
		case img.Width > 0 && img.Height > 0:
			w, h = img.Width*z, img.Height*z
		case img.Width > 0 && iw > 0 && ih > 0:
			w = img.Width * z
			h = w * ih / iw
		case img.Height > 0 && iw > 0 && ih > 0:
			h = img.Height * z
			w = h * iw / ih
		case iw > 0 && ih > 0:
			w, h = iw*z, ih*z
		case img.Height > 0:
			h = img.Height * z
			w = max(h*2.2, 64*z)
		case img.Width > 0:
			w = img.Width * z
			h = max(20*z, min(w*0.6, font.Size*1.4))
		default:
			w, h = 72*z, max(20*z, font.Size*1.25)
		}
	default:
		text := strings.TrimSpace(img.Alt)
		if text == "" {
			text = imageName(img.URL)
		}
		if text == "" {
			text = i18n.T("图片")
		}
		chipFont := ui.Font{Family: "system-ui", Size: max(11, font.Size*0.78)}
		chip = ui.ShapeText(text, chipFont, 0)
		cw, ch := chip.Size()
		iconSz := max(12, chipFont.Size)
		w = iconSz + cw + 16*z
		h = max(20*z, ch+6*z)
	}
	if maxW > 0 && w > maxW && w > 0 {
		scale := maxW / w
		w = maxW
		if res.HasVisual() || res.Loading {
			h = max(1, h*scale)
		}
	}
	w = max(1, w)
	h = max(1, h)
	if h <= font.Size*1.5 {
		descent = max(font.Size*0.18, (h-font.Size*0.62)/2)
	} else {
		descent = font.Size * 0.16
	}
	return w, h, descent, chip
}

func inlineTokensInBlock(b *document.Block, s string) []inlineToken {
	if b == nil || literalBlock(b.Kind) {
		return nil
	}
	var forbid [][2]int
	off := 0
	for _, r := range b.Runs {
		n := len(r.Text)
		if n > 0 && runDisallowsInlineToken(r.Style) {
			if len(forbid) > 0 && forbid[len(forbid)-1][1] == off {
				forbid[len(forbid)-1][1] = off + n
			} else {
				forbid = append(forbid, [2]int{off, off + n})
			}
		}
		off += n
	}
	if len(forbid) == 0 {
		return scanInlineTokens(s, nil)
	}
	return scanInlineTokens(s, func(byteOff int) bool {
		idx := sort.Search(len(forbid), func(i int) bool { return forbid[i][1] > byteOff })
		return idx < len(forbid) && byteOff >= forbid[idx][0]
	})
}

func isLineBreakAt(s string, i int) bool {
	return s[i] == '\n' || strings.HasPrefix(s[i:], "\u2028")
}

func scanInlineTokens(s string, disallowed func(int) bool) []inlineToken {
	var out []inlineToken
	for i := 0; i < len(s); {
		if disallowed != nil && disallowed(i) {
			i++
			continue
		}
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
					ok := true
					if disallowed != nil {
						for k := i; k < z; k++ {
							if disallowed(k) {
								ok = false
								break
							}
						}
					}
					if ok {
						out = append(out, inlineToken{i, z, "", label})
						i = z
						continue
					}
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
		blocked := false
		for z < len(s) && !isLineBreakAt(s, z) {
			if disallowed != nil && disallowed(z) {
				blocked = true
				break
			}
			if s[z] == '\\' {
				_, rn := utf8.DecodeRuneInString(s[min(z+1, len(s)):])
				z += 1 + rn
				continue
			}
			if s[z] == '$' {
				break
			}
			z++
		}
		if blocked || z >= len(s) || s[z] != '$' {
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
		if r.Style.Code || r.Style.Kbd || r.Style.Link != "" || r.Style.NoMath {
			return false
		}
	}
	return true
}

func scanDollarSpan(s string, openByte int) (closeByte int, ok bool) {
	if openByte < 0 || openByte >= len(s) || s[openByte] != '$' {
		return 0, false
	}
	if openByte > 0 && (s[openByte-1] == '$' || s[openByte-1] == '\\') {
		return 0, false
	}
	z := openByte + 1
	for z < len(s) && !isLineBreakAt(s, z) {
		if s[z] == '\\' {
			_, rn := utf8.DecodeRuneInString(s[min(z+1, len(s)):])
			z += 1 + rn
			continue
		}
		if s[z] == '$' {
			if z+1 < len(s) && s[z+1] == '$' {
				return 0, false
			}
			return z, true
		}
		z++
	}
	return 0, false
}

func (e *Editor) watchInline() {
	e.collectNotes()
	prev := e.inlineMath
	e.inlineEditing = ""
	e.inlineMath = inlineMathEdit{}
	if !e.focused || e.ReadOnly || e.printing {
		return
	}
	lo, hi := min(e.Anchor, e.Caret), max(e.Anchor, e.Caret)
	l := e.LeafAt(lo)
	if e.LeafAt(hi).Block.ID != l.Block.ID || literalBlock(l.Block.Kind) {
		return
	}
	s := l.Block.Text()
	atLo, atHi := lo-l.Start, hi-l.Start
	activate := func(a, z int, src string) {
		e.inlineMath = inlineMathEdit{blockID: l.Block.ID, start: a, end: z, source: src}
		e.inlineEditing = fmt.Sprintf("%s:%d:%d:%s", l.Block.ID, a, z, src)
	}
	for _, t := range inlineTokensInBlock(l.Block, s) {
		a, z := document.Len(s[:t.start]), document.Len(s[:t.end])
		if t.note == "" && atLo > a && atHi < z && tokenAllowed(l.Block, a, z) {
			activate(a, z, t.source)
			return
		}
	}
	if e.pairRevision == e.Revision() {
		for i := len(e.pairs) - 1; i >= 0; i-- {
			p := e.pairs[i]
			if p.open != "$" || p.close != "$" || p.start < l.Start || p.end+1 > l.End {
				continue
			}
			a, z := p.start-l.Start, p.end+1-l.Start
			if atLo <= a || atHi >= z || !tokenAllowed(l.Block, a, z) {
				continue
			}
			ba, bz := document.ByteOffset(s, a), document.ByteOffset(s, z)
			if ba < len(s) && bz <= len(s) && bz-ba >= 2 && s[ba] == '$' && s[bz-1] == '$' && !strings.ContainsAny(s[ba+1:bz-1], "$\n") {
				activate(a, z, s[ba+1:bz-1])
				return
			}
		}
	}
	if prev.blockID == l.Block.ID && prev.start >= 0 && prev.start < document.Len(s) {
		ba := document.ByteOffset(s, prev.start)
		if bz, ok := scanDollarSpan(s, ba); ok {
			a, z := prev.start, document.Len(s[:bz+1])
			if z-a >= 2 && atLo > a && atHi < z && tokenAllowed(l.Block, a, z) {
				activate(a, z, s[ba+1:bz])
				return
			}
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

func (e *Editor) buildEditingInlineMath(b *document.Block, text string, font ui.Font, shape ui.TextShape) (inlineObject, bool) {
	if e.printing || e.inlineEditing == "" || e.inlineMath.blockID != b.ID {
		return inlineObject{}, false
	}
	a, z := e.inlineMath.start, e.inlineMath.end
	if a < 0 || z > document.Len(text) || z-a < 2 {
		return inlineObject{}, false
	}
	raw := text[document.ByteOffset(text, a):document.ByteOffset(text, z)]
	src := strings.TrimSpace(e.inlineMath.source)
	obj := inlineObject{
		start:        a,
		end:          z,
		source:       e.inlineMath.source,
		sourceLayout: ui.ShapeText(raw, ui.Font{Family: "monospace", Size: 13}, 0),
		scale:        1,
		editing:      true,
	}
	validKey := fmt.Sprintf("%s:%d", b.ID, a)
	if src == "" {
		box := tex.Box{W: max(18, font.Size*1.15), Ascent: font.Size * 0.78, Descent: font.Size * 0.22}
		obj.box = &box
		obj.placeholder = true
		return obj, true
	}
	availW := float32(0)
	if shape.Width > 0 {
		availW = max(1, shape.Width-30)
	}
	box, err := tex.LayoutInlineWidth(src, font.Size*1.1, availW, e.measureMath)
	if err == nil {
		obj.box = &box
		if availW > 0 {
			obj.scale = min(float32(1), availW/max(1, box.W))
		}
		e.inlineValidID = validKey
		e.inlineValidBox = box
		return obj, true
	}
	obj.hasErr = true
	if e.inlineValidID == validKey && e.inlineValidBox.W > 0 {
		prev := e.inlineValidBox
		obj.box = &prev
		if availW > 0 {
			obj.scale = min(float32(1), availW/max(1, prev.W))
		}
		return obj, true
	}
	fallback := tex.Box{W: max(22, font.Size*1.3), Ascent: font.Size * 0.78, Descent: font.Size * 0.22}
	obj.box = &fallback
	obj.placeholder = true
	return obj, true
}

func (e *Editor) shapeInline(b *document.Block, spans []ui.Span, font ui.Font, shape ui.TextShape) *textLayout {
	l := &textLayout{}
	text := b.Text()
	if !literalBlock(b.Kind) {
		runOff := 0
		for _, r := range b.Runs {
			n := document.Len(r.Text)
			if r.IsImage() && n > 0 {
				res := e.imageResource(r.Image.URL, Options{LoadImage: e.loadImage})
				w, h, descent, chip := e.inlineImageMetrics(r.Image, res, font, shape.Width)
				l.objects = append(l.objects, inlineObject{
					start:      runOff,
					end:        runOff + n,
					image:      r.Image,
					imageLink:  r.Style.Link,
					imageRes:   res,
					imgW:       w,
					imgH:       h,
					imgDescent: descent,
					hasImage:   true,
					label:      chip,
					scale:      1,
				})
			}
			runOff += n
		}
	}
	editObj, hasEdit := e.buildEditingInlineMath(b, text, font, shape)
	if hasEdit {
		l.objects = append(l.objects, editObj)
	}
	for _, token := range inlineTokensInBlock(b, text) {
		a, z := document.Len(text[:token.start]), document.Len(text[:token.end])
		if !tokenAllowed(b, a, z) {
			continue
		}
		if hasEdit && a < editObj.end && z > editObj.start {
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
			availW := float32(0)
			if shape.Width > 0 {
				availW = max(1, shape.Width-4)
			}
			box, err := tex.LayoutInlineWidth(token.source, font.Size*1.1, availW, e.measureMath)
			if err != nil {
				continue
			}
			obj.box = &box
			if availW > 0 {
				obj.scale = min(float32(1), availW/max(1, box.W))
			}
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
		if o.hasImage {
			padY := float32(0)
			if o.imgH >= 20 {
				padY = 2
			}
			span.ObjectWidth = o.imgW
			span.ObjectHeight = max(1, o.imgH-o.imgDescent) + padY
			span.ObjectDescent = o.imgDescent + padY
		} else if o.box != nil {
			extra := float32(4)
			if o.editing {
				extra = 10
			}
			span.ObjectWidth = o.box.W*o.scale + extra
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
	visible = e.shapeTabsAndCodeIndent(b, visible, font, shape)
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

func (e *Editor) spaceAdvance(font ui.Font) float32 {
	k := ui.Font{Family: font.Family, Size: font.Size, Weight: font.Weight, Italic: font.Italic}
	if w, ok := e.spaceWidths[k]; ok {
		return w
	}
	w, _ := ui.ShapeText("\u00a0", k, 0).Size()
	if w <= 0 {
		w = max(4, k.Size*0.5)
	}
	if e.spaceWidths == nil {
		e.spaceWidths = make(map[ui.Font]float32, 8)
	}
	e.spaceWidths[k] = w
	return w
}

// shapeTabsAndCodeIndent expands literal '\t' runes into 4-column inline object
// spacers so leading and mid-line tabs advance visually on every text backend
// while keeping 1:1 UTF-16 indices. In soft-wrapped code blocks, leading spaces
// become non-breaking spaces so long indented tokens do not drop onto row 2 at x=0.
func (e *Editor) shapeTabsAndCodeIndent(b *document.Block, spans []ui.Span, baseFont ui.Font, shape ui.TextShape) []ui.Span {
	keepCodeIndent := b.Kind == "code" && shape.Width > 0
	needs := false
	for _, sp := range spans {
		if sp.ObjectWidth > 0 {
			continue
		}
		if strings.ContainsRune(sp.Text, '\t') || (keepCodeIndent && strings.ContainsRune(sp.Text, ' ')) {
			needs = true
			break
		}
	}
	if !needs {
		return spans
	}

	out := make([]ui.Span, 0, len(spans)+4)
	leading := true
	col := 0
	for _, sp := range spans {
		if sp.ObjectWidth > 0 {
			for _, r := range sp.Text {
				if r == '\n' || r == '\u2028' {
					leading = true
					col = 0
				} else {
					leading = false
					col++
				}
			}
			out = append(out, sp)
			continue
		}
		if sp.Text == "" {
			out = append(out, sp)
			continue
		}
		spFont := baseFont
		if sp.Font != "" {
			spFont.Family = sp.Font
		}
		if sp.Size > 0 {
			spFont.Size = sp.Size
		}
		if sp.Weight > 0 {
			spFont.Weight = sp.Weight
		}
		if sp.Italic {
			spFont.Italic = true
		}
		isMono := strings.Contains(strings.ToLower(spFont.Family), "mono")
		spaceW := float32(0)

		var buf strings.Builder
		firstPiece := true
		flushBuf := func() {
			if buf.Len() == 0 {
				return
			}
			piece := sp
			if !firstPiece {
				piece.PaddingStart = 0
			}
			firstPiece = false
			piece.Text = buf.String()
			out = append(out, piece)
			buf.Reset()
		}

		for _, r := range sp.Text {
			switch r {
			case '\n', '\u2028':
				buf.WriteRune(r)
				leading = true
				col = 0
			case '\t':
				flushBuf()
				if spaceW <= 0 {
					spaceW = e.spaceAdvance(spFont)
				}
				cols := 4
				if isMono {
					cols = 4 - (col % 4)
				}
				tabSpan := sp
				if !firstPiece {
					tabSpan.PaddingStart = 0
				}
				firstPiece = false
				if leading {
					tabSpan.Text = "\u00a0"
				} else {
					tabSpan.Text = "\ufffc"
				}
				tabSpan.ObjectWidth = float32(cols) * spaceW
				tabSpan.ObjectHeight = spFont.Size * 0.8
				tabSpan.ObjectDescent = spFont.Size * 0.2
				out = append(out, tabSpan)
				col += cols
			case ' ':
				if leading && keepCodeIndent {
					buf.WriteRune('\u00a0')
				} else {
					buf.WriteByte(' ')
				}
				col++
			default:
				leading = false
				buf.WriteRune(r)
				col++
			}
		}
		flushBuf()
	}
	return out
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
			if o.script || o.hasImage {
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
	for _, o := range l.objects {
		if o.note == "" && !o.script {
			if at == o.start && o.end-o.start >= 2 {
				return o.start + 1
			}
			if o.editing && o.sourceLayout != nil && at >= o.start && at < o.end {
				return o.start + o.sourceLayout.NextBoundary(at-o.start)
			}
		}
	}
	return l.model(l.TextLayout.NextBoundary(l.visual(at, false)))
}

func (l *textLayout) PreviousBoundary(at int) int {
	for _, o := range l.objects {
		if o.note == "" && !o.script {
			if at == o.end && o.end-o.start >= 2 {
				return o.end - 1
			}
			if o.editing && o.sourceLayout != nil && at > o.start && at <= o.end {
				return o.start + o.sourceLayout.PreviousBoundary(at-o.start)
			}
		}
	}
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
	dx, dy := e.visualOffset(r)
	for _, o := range r.layout.objects {
		ox, oy := bounds.X+r.x+dx+o.rect.X, bounds.Y+r.y+dy+o.rect.Y
		x, y := ox+2, oy
		if o.hasImage {
			padY := float32(0)
			if o.imgH >= 20 && o.rect.H > o.imgH {
				padY = (o.rect.H - o.imgH) / 2
			}
			dst := ui.Rect{X: ox, Y: oy + padY, W: o.rect.W, H: max(1, o.rect.H-2*padY)}
			if o.imageRes.Bitmap != nil {
				bitmap := o.imageRes.Bitmap
				if !e.printing && !e.reduceMotion && dst.Y+dst.H > bounds.Y+e.Scroll.Y && dst.Y < bounds.Y+e.Scroll.Y+e.viewport {
					frame, next := o.imageRes.frameAt(p.Now())
					if next > 0 {
						bitmap = frame
						p.After(next)
					}
				}
				p.Image(bitmap, dst, ui.Contain)
			} else if svg := o.imageRes.SVGFor(t.Dark); svg != nil {
				p.Image(svg, dst, ui.Contain)
			} else if o.imageRes.Loading {
				rad := min(float32(4), min(dst.W, dst.H)/4)
				p.Fill(dst, e.page.CodeFill, rad)
				p.Stroke(dst, e.page.CodeBorder, rad, 1)
			} else {
				rad := min(float32(5), dst.H/2)
				p.Fill(dst, e.page.InlineFill, rad)
				p.Stroke(dst, e.page.CodeBorder, rad, 1)
				ink := e.page.Caption
				if o.imageLink != "" {
					ink = t.Accent
				}
				iconSz := min(float32(14), max(10, dst.H-6))
				p.Icon(iconImage, ui.Rect{X: dst.X + 5, Y: dst.Y + (dst.H-iconSz)/2, W: iconSz, H: iconSz}, ink)
				if o.label != nil {
					_, lh := o.label.Size()
					textX := dst.X + 5 + iconSz + 4
					clipW := max(0, dst.X+dst.W-textX-4)
					if clipW > 2 {
						p.Clip(ui.Rect{X: textX, Y: dst.Y, W: clipW, H: dst.H}, 0, func() {
							p.TextLayout(o.label, textX, dst.Y+(dst.H-lh)/2, ink)
						})
					}
				}
			}
			continue
		}
		if o.box != nil {
			if o.editing {
				frame := ui.Rect{X: ox, Y: oy - 2, W: max(20, o.rect.W-2), H: o.rect.H + 4}
				border := t.Accent.Alpha(0.42)
				fill := t.Accent.Alpha(0.065)
				if o.hasErr {
					border = t.Danger.Alpha(0.55)
				}
				p.Fill(frame, fill, 5)
				p.Stroke(frame, border, 5, 1)
				if o.placeholder && len(o.box.Ops) == 0 {
					fx := ui.ShapeText("fx", ui.Font{Family: mathFont, Size: 12, Italic: true}, 0)
					fw, fh := fx.Size()
					p.TextLayout(fx, ox+4+max(0, o.box.W*o.scale-fw)/2, oy+(o.rect.H-fh)/2, t.Accent.Alpha(0.7))
				} else {
					fx := ox + 4
					e.paintFormulaScaled(p, *o.box, fx, y+o.box.Ascent*o.scale, o.scale, t)
				}
				e.paintTabOutBadge(p, frame, t)
			} else {
				e.paintFormulaScaled(p, *o.box, x, y+o.box.Ascent*o.scale, o.scale, t)
			}
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
		label := ui.ShapeText(fmt.Sprintf("%d ↩", n.number), ui.Font{Size: 12 * e.zoomFactor()}, 0)
		p.TextLayout(label, bounds.X+r.rect.X+dx, bounds.Y+r.y+dy, t.Accent)
	}
}

// paintInlineMathPopover renders the floating monospace TeX bar beneath an
// active inline math formula and returns the document-space caret rect inside
// that bar when active.
func (e *Editor) paintInlineMathPopover(p *ui.Painter, bounds ui.Rect, t *ui.Theme) (ui.Rect, bool) {
	e.inlinePopRect = ui.Rect{}
	e.inlinePopLayout = nil
	if e.printing || e.ReadOnly || !e.focused || e.inlineMath.blockID == "" {
		return ui.Rect{}, false
	}
	idx, ok := e.rowIDs[e.inlineMath.blockID]
	if !ok {
		return ui.Rect{}, false
	}
	r := *e.ensureRow(idx)
	if r.layout == nil {
		return ui.Rect{}, false
	}
	for _, o := range r.layout.objects {
		if !o.editing {
			continue
		}
		dx, dy := e.visualOffset(r)
		anchorX := bounds.X + r.x + dx + o.rect.X
		anchorY := bounds.Y + r.y + dy + o.rect.Y + o.rect.H + 6
		delim := ui.ShapeText("$", ui.Font{Family: "monospace", Size: 12.5}, 0)
		dw, dh := delim.Size()
		srcLayout := ui.ShapeText(o.source, ui.Font{Family: "monospace", Size: 13}, 0)
		sw, sh := srcLayout.Size()
		sh = max(sh, 18)
		hint := ui.ShapeText(i18n.T("输入公式"), ui.Font{Family: "system-ui", Size: 12}, 0)
		hw, hh := hint.Size()
		innerW := sw
		if o.source == "" {
			innerW = max(innerW, hw+4)
		}
		popW := max(116, min(max(116, bounds.W-28), dw*2+innerW+30))
		popH := max(30, sh+12)
		popX := min(max(bounds.X+14, anchorX), max(bounds.X+14, bounds.X+bounds.W-14-popW))
		pop := ui.Rect{X: popX, Y: anchorY, W: popW, H: popH}

		p.Shadow(pop, 8, 0, 4, 14, 0, e.page.MathShadow)
		p.Fill(pop, e.page.Popover, 8)
		border := e.page.MathBorder
		if o.hasErr && strings.TrimSpace(o.source) != "" {
			border = t.Danger.Alpha(0.55)
		}
		p.Stroke(pop, border, 8, 1)

		textX := pop.X + 10 + dw + 6
		textY := pop.Y + (pop.H-sh)/2
		p.TextLayout(delim, pop.X+10, pop.Y+(pop.H-dh)/2, e.page.Caption)
		p.TextLayout(delim, pop.X+pop.W-10-dw, pop.Y+(pop.H-dh)/2, e.page.Caption)

		srcStart := r.leaf.Start + o.start + 1
		srcLen := document.Len(o.source)
		e.inlinePopRect = ui.Rect{X: pop.X - bounds.X, Y: pop.Y - bounds.Y, W: pop.W, H: pop.H}
		e.inlinePopTextX = textX - bounds.X
		e.inlinePopTextY = textY - bounds.Y
		e.inlinePopLayout = srcLayout
		e.inlinePopStart = srcStart
		e.inlinePopLen = srcLen

		if e.Anchor != e.Caret {
			selLo := max(0, min(srcLen, min(e.Anchor, e.Caret)-srcStart))
			selHi := max(0, min(srcLen, max(e.Anchor, e.Caret)-srcStart))
			for _, sr := range srcLayout.SelectionRects(ui.TextInputRange{Start: selLo, End: selHi}) {
				p.Fill(ui.Rect{X: textX + sr.X, Y: textY + sr.Y, W: sr.W, H: sr.H}, t.Selection, 2)
			}
		}
		if o.source == "" {
			p.TextLayout(hint, textX+2, pop.Y+(pop.H-hh)/2, e.page.Placeholder)
		} else {
			p.TextLayout(srcLayout, textX, textY, t.Text)
		}
		localCaret := max(0, min(srcLen, e.Caret-srcStart))
		cr := srcLayout.Caret(localCaret)
		caretDoc := ui.Rect{
			X: (textX - bounds.X) + cr.X,
			Y: (textY - bounds.Y) + cr.Y,
			W: 2,
			H: max(16, sh),
		}
		return caretDoc, true
	}
	return ui.Rect{}, false
}

func (e *Editor) clickInlineMathPopover(x, y float32) bool {
	pop := e.inlinePopRect
	if e.inlinePopLayout == nil || pop.W <= 0 || x < pop.X || x > pop.X+pop.W || y < pop.Y || y > pop.Y+pop.H {
		return false
	}
	idx := e.inlinePopLayout.IndexAt(ui.Point{X: x - e.inlinePopTextX, Y: y - e.inlinePopTextY})
	pos := e.inlinePopStart + max(0, min(e.inlinePopLen, idx))
	e.SetSelection(pos, pos)
	e.reveal = false
	return true
}

func (e *Editor) exitInlineMath() bool {
	if e.inlineMath.blockID == "" {
		return false
	}
	l := e.LeafAt(e.Caret)
	if l.Block.ID != e.inlineMath.blockID {
		return false
	}
	openPos := l.Start + e.inlineMath.start
	if e.pairRevision == e.Revision() {
		for i := len(e.pairs) - 1; i >= 0; i-- {
			if e.pairs[i].open == "$" && e.pairs[i].start == openPos {
				e.pairs = append(e.pairs[:i], e.pairs[i+1:]...)
				break
			}
		}
	}
	after := min(l.End, l.Start+e.inlineMath.end)
	e.SetSelection(after, after)
	e.smartBack = smartRollback{}
	e.reveal = true
	return true
}

func (e *Editor) escapeInlineMath() bool {
	if e.inlineMath.blockID == "" {
		return false
	}
	l := e.LeafAt(e.Caret)
	if l.Block.ID != e.inlineMath.blockID {
		return false
	}
	openPos := l.Start + e.inlineMath.start
	closeStart := l.Start + e.inlineMath.end - 1
	if e.pairRevision == e.Revision() {
		for i := len(e.pairs) - 1; i >= 0; i-- {
			p := e.pairs[i]
			if p.open == "$" && p.start == openPos && closeStart >= l.Start && closeStart+1 <= l.End && e.slice(closeStart, closeStart+1) == "$" {
				caret := e.Caret
				e.pairs = append(e.pairs[:i], e.pairs[i+1:]...)
				r := ui.TextInputRange{Start: closeStart, End: closeStart + 1}
				e.shiftPairs(r, 0)
				e.Buffer.ReplaceText(&r, "")
				e.SetSelection(min(caret, e.Len()), min(caret, e.Len()))
				return true
			}
		}
	}
	return e.exitInlineMath()
}

type noteTarget struct {
	number, start int
	id            string
}

// Only blocks with effective references need to participate in renumbering.
// A text edit cannot change definition labels or the order of blocks.
type noteReferences struct {
	id     string
	start  int
	labels []string
}

func referencedNotes(b *document.Block, text string) []string {
	if literalBlock(b.Kind) || !strings.Contains(text, "[^") {
		return nil
	}
	var labels []string
	for _, token := range inlineTokensInBlock(b, text) {
		if token.note != "" && tokenAllowed(b, document.Len(text[:token.start]), document.Len(text[:token.end])) {
			labels = append(labels, token.note)
		}
	}
	return labels
}

func (e *Editor) collectNotes() {
	if e.notesDoc == e.Doc && e.notesRevision == e.Revision() {
		return
	}
	if change, ok := e.TextChangeSince(e.notesRevision); ok && e.notesDoc == e.Doc {
		e.notesRevision = e.Revision()
		if len(e.notes) == 0 {
			return // text-only edits cannot create a footnote definition
		}
		delta := change.AfterEnd - change.BeforeEnd
		for label, note := range e.notes {
			if note.start > change.Start {
				note.start += delta
				e.notes[label] = note
			}
		}
		at := sort.Search(len(e.noteRefs), func(i int) bool { return e.noteRefs[i].start >= change.Start })
		labels := referencedNotes(e.Engine.LeafAt(change.Start).Block, change.After)
		var previous []string
		present := at < len(e.noteRefs) && e.noteRefs[at].id == change.ID
		if present {
			previous = e.noteRefs[at].labels
		}
		for i := at; i < len(e.noteRefs); i++ {
			if e.noteRefs[i].start > change.Start {
				e.noteRefs[i].start += delta
			}
		}
		if !slices.Equal(previous, labels) {
			switch {
			case present && len(labels) == 0:
				e.noteRefs = slices.Delete(e.noteRefs, at, at+1)
			case present:
				e.noteRefs[at].labels = labels
			default:
				e.noteRefs = slices.Insert(e.noteRefs, at, noteReferences{change.ID, change.Start, labels})
			}
			e.numberNotes()
		}
		return
	}
	e.notesDoc, e.notesRevision = e.Doc, e.Revision()
	e.noteRefs, e.noteOrder = nil, nil
	e.notesKey = ""
	e.notes = map[string]noteTarget{}
	if !e.Doc.HasKind("footnote") {
		// Only references to a definition are numbered, so a page without
		// one has nothing to number: skip the pass over all its text,
		// which every keystroke on a long page would otherwise pay.
		return
	}
	leaves := e.Doc.Leaves()
	for _, l := range leaves {
		if l.Block.Kind == "footnote" {
			if _, exists := e.notes[l.Block.NoteLabel]; !exists {
				e.notes[l.Block.NoteLabel] = noteTarget{start: l.Start, id: l.Block.ID}
				e.noteOrder = append(e.noteOrder, l.Block.NoteLabel)
			}
		}
	}
	for _, l := range leaves {
		if labels := referencedNotes(l.Block, l.Block.Text()); len(labels) > 0 {
			e.noteRefs = append(e.noteRefs, noteReferences{l.Block.ID, l.Start, labels})
		}
	}
	e.numberNotes()
}

func (e *Editor) numberNotes() {
	for label, note := range e.notes {
		note.number = 0
		e.notes[label] = note
	}
	n := 0
	assign := func(label string) {
		if note, ok := e.notes[label]; ok && note.number == 0 {
			n++
			note.number = n
			e.notes[label] = note
		}
	}
	for _, refs := range e.noteRefs {
		for _, label := range refs.labels {
			assign(label)
		}
	}
	for _, label := range e.noteOrder {
		assign(label)
	}
	e.notesKey = e.buildNoteKey()
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
	for _, t := range inlineTokensInBlock(l.Block, s) {
		a, z := l.Start+document.Len(s[:t.start]), l.Start+document.Len(s[:t.end])
		if t.note != "" && e.Caret >= a && e.Caret <= z {
			if n, ok := e.notes[t.note]; ok {
				e.noteReturn, e.noteHasReturn = e.Caret, true
				e.noteOriginID, e.noteOriginOffset = l.Block.ID, e.Caret-l.Start
				e.FocusBlock(n.id)
				e.flashBlock(e.LeafAt(e.Caret).Owner)
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
			for _, token := range inlineTokensInBlock(leaf.Block, s) {
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
	e.flashBlock(e.LeafAt(e.Caret).Owner)
	e.noteHasReturn = false
	e.reveal, e.RequestFocus = true, true
	return true
}
