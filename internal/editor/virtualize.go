package editor

import (
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/ui"
)

const (
	// Virtualization activates only on large documents so ordinary notes and
	// resize tests continue to use the exact eager layout path.
	virtualizeLeafThreshold = 800
	virtualizeTextThreshold = 120_000
	maxCachedShapes         = 768
	cacheTrimHysteresis     = 128
	virtualOverscanMin      = 1200
)

// runsStamp is a pointer-free 128-bit fingerprint of a block's styled runs so
// thousands of offscreen height hints retain zero heap pointers for GC.
type runsStamp struct {
	lo uint64
	hi uint64
}

func makeRunsStamp(runs []document.Run) runsStamp {
	lo := uint64(14695981039346656037)
	hi := uint64(0x9e3779b97f4a7c15) ^ uint64(len(runs))
	const prime2 = uint64(6364136223846793005)
	lo = fnvMixUint64(lo, uint64(len(runs)))
	for i := range runs {
		r := &runs[i]
		lo = fnvMixStyle(lo, r.Style)
		lo = fnvMixString(lo, r.Text)
		hi = (hi^uint64(len(r.Text)))*prime2 + uint64(i+1)
		if r.Style != (document.Style{}) {
			hi ^= fnvMixStyle(hi, r.Style)
		}
		for j := 0; j < len(r.Text); j++ {
			hi = (hi ^ uint64(r.Text[j]) ^ uint64(j+1)<<8) * prime2
		}
	}
	return runsStamp{lo: lo, hi: hi}
}

func fontStamp(f ui.Font) uint64 {
	h := uint64(14695981039346656037)
	h = fnvMixString(h, f.Family)
	h = fnvMixString(h, f.Features)
	h = fnvMixUint32(h, math.Float32bits(f.Size))
	h = fnvMixUint32(h, math.Float32bits(f.LineHeight))
	h = fnvMixUint64(h, uint64(f.Weight))
	if f.Italic {
		h = fnvMixByte(h, 1)
	} else {
		h = fnvMixByte(h, 0)
	}
	return h
}

type heightHint struct {
	width    float32
	height   float32
	fontHash uint64
	stamp    runsStamp
	exact    bool
}

type blockMetric struct {
	font       ui.Font
	left       float32
	right      float32
	top        float32
	bottom     float32
	gap        float32
	shapeWidth float32
}

type rowShift struct {
	oldTop    float32
	oldBottom float32
	dy        float32
	cumAfter  float32
	id        string
}

func (e *Editor) shouldVirtualize() bool {
	if e.printing {
		return false
	}
	return e.Engine.LeafCount() > virtualizeLeafThreshold || e.Engine.TextLength() > virtualizeTextThreshold
}

func (e *Editor) virtualWindow() (top, bottom float32) {
	vp := e.viewport
	if vp <= 0 {
		vp = 900
	}
	overscan := max(float32(virtualOverscanMin), vp)
	return e.Scroll.Y - overscan, e.Scroll.Y + vp + overscan
}

func (e *Editor) nextCacheClock() uint64 {
	e.cacheClock++
	return e.cacheClock
}

func canDeferKind(kind string) bool {
	switch kind {
	case "image", "math", "rule", "table", "columns", "cell":
		return false
	}
	return true
}

func (e *Editor) blockMetrics(b *document.Block, w float32, header bool) blockMetric {
	// Body text is 16 on 26 with 8 between blocks; table cells are 14 on 22.
	m := blockMetric{
		font:   ui.Font{Family: bodyFamily(e.fontFamily), Size: 16, LineHeight: 26.0 / 16},
		bottom: 8,
		gap:    8,
	}
	if e.readingSize >= 12 && e.readingSize <= 28 {
		m.font.Size = e.readingSize
	}
	if e.readingLine >= 1.2 && e.readingLine <= 2.2 {
		m.font.LineHeight = e.readingLine
	}
	if b.Kind == "cell" {
		m.font.Size, m.font.LineHeight = 14, 22.0/14
	}
	switch b.Kind {
	case "heading":
		level := min(6, max(1, b.Level))
		m.font.Size = []float32{26, 22, 20, 18, 16, 16}[level-1]
		m.font.LineHeight = []float32{36.4, 30.8, 28, 25.2, 26, 26}[level-1] / m.font.Size
		m.font.Weight = 500
		m.top = []float32{26, 22, 20, 18, 18, 16}[level-1]
		m.bottom = []float32{10, 8, 8, 8, 8, 8}[level-1]
		if len(e.Doc.Blocks) > 0 && e.Doc.Blocks[0].ID == b.ID {
			// The first heading is the page title: 34 on 55.25.
			m.font.Size, m.font.Weight, m.font.LineHeight = 34, 500, 55.25/34
			m.top, m.bottom = 0, 22
		}
	case "code", "raw", "math":
		m.font.Family, m.font.Size, m.font.LineHeight = "monospace", 14, 22.0/14
		m.gap = 8
		m.left, m.right, m.top, m.bottom = 20, 20, 16, 16+m.gap
		if b.Kind == "code" {
			m.left, m.top, m.bottom = 60, 40, 26+m.gap
		}
		if b.Kind == "math" {
			m.font.Size, m.font.LineHeight = 14.4, 22.0/14.4
			m.left, m.right = mathPaddingX, mathPaddingX
		}
	case "quote":
		m.left, m.top, m.bottom, m.gap = 14, 0, 8, 8
	case "callout":
		m.gap = 8
		m.left, m.right, m.top, m.bottom = 48, 16, 16, 16+m.gap
	case "bullet", "numbered", "task":
		m.left = 24 + float32(b.Indent)*24
		m.bottom = 8
	case "toggle":
		m.left = 20 + float32(b.Indent)*20
	}
	if b.Kind == "footnote" {
		m.left, m.top = 36, 12
	}
	m.shapeWidth = max(24, w-m.left-m.right)
	_ = header
	return m
}

func (e *Editor) blockShapeKey(b *document.Block, font ui.Font, header bool) shapeStyle {
	revealed := ""
	if strings.HasPrefix(e.inlineEditing, b.ID+":") {
		revealed = e.inlineEditing
	}
	return shapeStyle{font, b.Align, b.Kind, revealed, e.noteKey(), b.Checked, header}
}

func (e *Editor) shapeBlock(b *document.Block, key shapeStyle, shapeWidth float32, theme *ui.Theme) cachedShape {
	pg := e.page
	font := key.font
	var spans []ui.Span
	for _, run := range b.Runs {
		style := run.Style
		span := ui.Span{Text: run.Text, Italic: style.Italic, Underline: style.Underline, Strikethrough: style.Strike}
		if style.Script != "" {
			span.Size = font.Size * .72
			span.BaselineShift = font.Size * .35
			if style.Script == "sub" {
				span.BaselineShift = -font.Size * .18
			}
		}
		if style.Bold {
			span.Weight = 600
		}
		if style.Code {
			span.Font, span.Size, span.Background, span.Radius = "monospace", font.Size*.875, pg.InlineFill, 4
			span.Color = pg.InlineText
		}
		if style.Link != "" {
			span.Color, span.Underline, span.DecorationColor = theme.Accent, style.Underline, theme.Accent.Alpha(.35)
		}
		if c, ok := pg.TextColors[style.Color]; ok {
			span.Color, span.DecorationColor = c, c
		}
		if c, ok := pg.Highlights[style.Highlight]; ok {
			span.Background, span.Radius = c, 2
		}
		switch {
		case key.header:
			span.Weight = max(span.Weight, 500)
		case b.Kind == "quote":
			span.Color = pg.Caption
		case b.Kind == "task" && b.Checked:
			span.Color, span.Strikethrough, span.DecorationColor = pg.TaskDone, true, pg.TaskDone
		}
		if style.Link == "" {
			spans = append(spans, e.wikiSpans(span, run, b.Kind, theme)...)
			continue
		}
		spans = append(spans, span)
	}
	if b.Kind == "code" {
		spans = codeSpans(b.Text(), pg)
	} else if b.Kind != "math" && b.Kind != "raw" {
		spans = padAnnotations(spans, b.Runs, font.Size)
	}
	if len(spans) == 0 {
		spans = []ui.Span{{Text: ""}}
	}
	align := ui.Start
	if b.Align == "center" {
		align = ui.Center
	}
	if b.Align == "right" {
		align = ui.End
	}
	if b.Kind == "math" || b.Kind == "image" {
		align = ui.Center
	}
	return cachedShape{
		key:      key,
		runs:     slices.Clone(b.Runs),
		width:    shapeWidth,
		layout:   e.shapeInline(b, spans, font, ui.TextShape{Width: shapeWidth, Align: align}),
		lastUsed: e.nextCacheClock(),
	}
}

func (e *Editor) recordExactHeight(id string, width, height float32, font ui.Font, runs []document.Run) {
	if e.heightHints == nil {
		e.heightHints = make(map[string]heightHint, 64)
	}
	e.heightHints[id] = heightHint{
		width:    width,
		height:   height,
		fontHash: fontStamp(font),
		stamp:    makeRunsStamp(runs),
		exact:    true,
	}
}

// estimateBlockHeight predicts wrapped text height without invoking CoreText
// or allocating span slices. Exact measurements and unchanged heuristic estimates
// are reused via heightHints while content, font, and wrap width match.
func (e *Editor) estimateBlockHeight(b *document.Block, font ui.Font, shapeWidth float32) float32 {
	lineH := font.Size * font.LineHeight
	fHash := fontStamp(font)
	stamp := makeRunsStamp(b.Runs)
	if e.heightHints != nil {
		if hint, ok := e.heightHints[b.ID]; ok && hint.width == shapeWidth && hint.fontHash == fHash && hint.stamp == stamp {
			return max(hint.height, lineH)
		}
	}
	avail := shapeWidth
	if font.Family != "monospace" {
		// Word-boundary wrapping breaks slightly earlier than glyph sums.
		avail *= 0.94
	}
	avail = max(16, avail)
	lines := 1
	curW := float32(0)
	baseMono := font.Family == "monospace"
	for i := range b.Runs {
		run := &b.Runs[i]
		size := font.Size
		mono := baseMono || run.Style.Code
		if run.Style.Code {
			size *= 0.875
		} else if run.Style.Script != "" {
			size *= 0.72
		}
		for _, r := range run.Text {
			if r == '\n' || r == '\u2028' {
				lines++
				curW = 0
				continue
			}
			var adv float32
			switch {
			case r >= 0x1100:
				adv = size
			case r == '\t':
				adv = 2 * size
			case mono:
				adv = 0.60 * size
			case r < 0x80:
				switch r {
				case 'i', 'I', 'l', '1', '!', '|', '.', ',', ';', ':', '\'', '"', '[', ']', '(', ')', ' ':
					adv = 0.30 * size
				case 'M', 'W', 'm', 'w', '@', '#', '%', '&':
					adv = 0.78 * size
				default:
					if r >= 'A' && r <= 'Z' {
						adv = 0.65 * size
					} else {
						adv = 0.52 * size
					}
				}
			default:
				adv = 0.58 * size
			}
			if curW+adv > avail && curW > 0 {
				lines++
				curW = adv
			} else {
				curW += adv
			}
		}
	}
	est := float32(lines) * lineH
	if e.heightHints == nil {
		e.heightHints = make(map[string]heightHint, 64)
	}
	e.heightHints[b.ID] = heightHint{
		width:    shapeWidth,
		height:   est,
		fontHash: fHash,
		stamp:    stamp,
		exact:    false,
	}
	return est
}

func (e *Editor) activeBlockIDs() (caretID, anchorID, revealID string) {
	caretID = e.Engine.LeafAt(e.Caret).Block.ID
	if e.Anchor != e.Caret {
		anchorID = e.Engine.LeafAt(e.Anchor).Block.ID
	}
	if e.revealAt != nil {
		revealID = e.Engine.LeafAt(e.revealAt.Start).Block.ID
	}
	return caretID, anchorID, revealID
}

// ensureRow returns row i with its TextLayout shaped and cached, materializing
// deferred offscreen blocks on demand without breaking reading anchors.
func (e *Editor) ensureRow(i int) *row {
	r := e.indexedRow(i)
	if r.layout != nil {
		if b := r.leaf.Block; b != nil {
			if cached, ok := e.cache[b.ID]; ok {
				cached.lastUsed = e.nextCacheClock()
				e.cache[b.ID] = cached
			}
		}
		return r
	}
	e.materializeRows([]int{i}, &e.cacheTheme)
	return &e.rows[i]
}

// materializeViewport ensures every visible/overscan row is shaped and trims
// offscreen TextLayouts beyond the LRU budget.
func (e *Editor) materializeViewport(o Options) {
	if !e.shouldVirtualize() {
		return
	}
	winTop, winBottom := e.virtualWindow()
	targets := e.collectUnshapedRows(winTop, winBottom, true)
	if len(targets) > 0 {
		e.materializeRows(targets, o.Theme)
	}
	e.trimShapeCache(winTop, winBottom)
}

// ensureVisibleShaped shapes any deferred rows inside [Scroll.Y-slack, Scroll.Y+viewport+slack]
// before paint traverses rowSpace/decorationSpace.
func (e *Editor) ensureVisibleShaped(slack float32, theme *ui.Theme) {
	if !e.shouldVirtualize() {
		return
	}
	targets := e.collectUnshapedRows(e.Scroll.Y-slack, e.Scroll.Y+e.viewport+slack, false)
	if len(targets) > 0 {
		e.materializeRows(targets, theme)
	}
}

func (e *Editor) collectUnshapedRows(top, bottom float32, includeActive bool) []int {
	var targets []int
	for i := range e.rowSpace.between(top, bottom) {
		if e.rows[i].layout == nil {
			targets = append(targets, i)
		}
	}
	if includeActive {
		caretID, anchorID, revealID := e.activeBlockIDs()
		for _, id := range []string{caretID, anchorID, revealID} {
			if id == "" {
				continue
			}
			if idx, ok := e.rowIDs[id]; ok && e.rows[idx].layout == nil {
				targets = append(targets, idx)
			}
		}
	}
	if len(targets) > 1 {
		slices.Sort(targets)
		targets = slices.Compact(targets)
	}
	return targets
}

// materializeRows shapes ascending row indices in one linear geometry pass,
// keeping the current reading anchor and smoothed block baselines aligned.
func (e *Editor) materializeRows(targets []int, theme *ui.Theme) {
	if len(targets) == 0 || theme == nil {
		return
	}
	anchor := e.resizeAnchor()
	if e.cache == nil {
		e.cache = make(map[string]cachedShape)
	}
	firstRow := targets[0]
	shifts := make([]rowShift, 0, len(targets))
	caretAdjusted := make(map[string]float32, len(targets))
	cum := float32(0)
	ti := 0
	minDec := len(e.decorations)
	for j := firstRow; j < len(e.rows); j++ {
		r := e.indexedRow(j)
		origY, origH := r.rect.Y, r.rect.H
		if cum != 0 {
			r.rect.Y += cum
			r.y += cum
			if f := r.picture; f != nil {
				f.bounds.Y += cum
				for k := range f.actions {
					f.actions[k].bounds.Y += cum
				}
			}
		}
		if ti < len(targets) && j == targets[ti] {
			ti++
			b := r.leaf.Block
			m := e.blockMetrics(b, r.rect.W, r.tableHeader)
			key := e.blockShapeKey(b, m.font, r.tableHeader)
			cached, ok := e.cache[b.ID]
			if !ok || cached.key != key || cached.width != m.shapeWidth || !slices.Equal(cached.runs, b.Runs) {
				cached = e.shapeBlock(b, key, m.shapeWidth, theme)
				e.cache[b.ID] = cached
			} else {
				cached.lastUsed = e.nextCacheClock()
				e.cache[b.ID] = cached
			}
			r.layout = cached.layout
			_, h := cached.layout.Size()
			h = max(h, m.font.Size*m.font.LineHeight)
			if canDeferKind(b.Kind) {
				e.recordExactHeight(b.ID, m.shapeWidth, h, m.font, b.Runs)
			}
			newH := m.top + h + m.bottom
			dy := newH - origH
			r.rect.H = newH
			caret := cached.layout.Caret(0)
			caretAdjusted[b.ID] = r.y + caret.Y + max(caret.H, 20)/2 - 12
			if dy != 0 {
				cum += dy
				shifts = append(shifts, rowShift{oldTop: origY, oldBottom: origY + origH, dy: dy, cumAfter: cum, id: b.ID})
				if di, ok := e.decorationIDs[b.ID]; ok {
					e.decorations[di].rect.H = newH - m.gap
					minDec = min(minDec, di)
				}
			}
		}
	}
	if len(shifts) == 0 {
		for id, hy := range caretAdjusted {
			if hi, ok := e.handleIDs[id]; ok {
				e.handles[hi].rect.Y = hy
			}
		}
		for j := range e.foldButtons {
			if hy, ok := caretAdjusted[e.foldButtons[j].id]; ok {
				e.foldButtons[j].rect.Y = hy + 4
			}
		}
		return
	}
	e.rowSpace.refitSuffix(firstRow)

	cumBefore := func(y float32) float32 {
		idx := sort.Search(len(shifts), func(k int) bool { return shifts[k].oldBottom > y })
		if idx == 0 {
			return 0
		}
		return shifts[idx-1].cumAfter
	}
	firstTop := shifts[0].oldTop

	if hi, ok := e.handleIDs[e.rows[firstRow].leaf.Owner]; ok {
		for j := hi; j < len(e.handles); j++ {
			h := &e.handles[j]
			delta := cumBefore(h.top)
			if delta != 0 {
				h.top += delta
				h.rect.Y += delta
				if bs, ok := e.motion.blocks[h.id]; ok {
					bs.laidY += delta
					e.motion.blocks[h.id] = bs
				}
			}
			if hy, ok := caretAdjusted[h.id]; ok {
				h.rect.Y = hy
			}
		}
	}
	firstFold := sort.Search(len(e.foldButtons), func(j int) bool { return e.foldButtons[j].rect.Y+4 >= firstTop })
	if firstFold > 0 {
		firstFold--
	}
	for j := firstFold; j < len(e.foldButtons); j++ {
		f := &e.foldButtons[j]
		if hi, ok := e.handleIDs[f.id]; ok {
			f.rect.Y = e.handles[hi].rect.Y + 4
		}
	}
	for j := range e.decorations {
		d := &e.decorations[j]
		if d.rect.Y < firstTop {
			continue
		}
		delta := cumBefore(d.rect.Y)
		if delta == 0 {
			continue
		}
		minDec = min(minDec, j)
		d.rect.Y += delta
		if d.table.H > 0 {
			d.table.Y += delta
		}
		for k := range d.rows {
			d.rows[k] += delta
		}
		if d.kind == "table" && len(e.motion.tableRows) > 0 {
			if tb := e.blockByID(d.id); tb != nil {
				for _, tr := range tb.Children {
					if slide, ok := e.motion.tableRows[tr.ID]; ok {
						slide.laidY += delta
						e.motion.tableRows[tr.ID] = slide
					}
				}
			}
		}
	}
	if minDec < len(e.decorations) {
		e.decorationSpace.refitSuffix(minDec)
	}
	for j := range e.resizers {
		if e.resizers[j].y >= firstTop {
			e.resizers[j].y += cumBefore(e.resizers[j].y)
		}
	}
	firstOutline := sort.Search(len(e.outline), func(j int) bool { return e.outline[j].y >= firstTop })
	for j := firstOutline; j < len(e.outline); j++ {
		e.outline[j].y += cumBefore(e.outline[j].y)
	}
	e.contentEnd += cum
	e.height += cum
	e.restoreResizeAnchor(anchor)
}

// trimShapeCache evicts the least-recently-used offscreen TextLayouts once the
// cache grows past budget, recording exact heightHints before freeing memory.
func (e *Editor) trimShapeCache(winTop, winBottom float32) {
	if len(e.cache) <= maxCachedShapes+cacheTrimHysteresis {
		return
	}
	caretID, anchorID, revealID := e.activeBlockIDs()
	type candidate struct {
		id   string
		used uint64
		row  int
	}
	candidates := make([]candidate, 0, len(e.cache)-maxCachedShapes)
	for id, cached := range e.cache {
		if _, embedded := e.embeddedBlocks[id]; embedded {
			continue
		}
		if id == caretID || id == anchorID || id == revealID || id == e.dragID || id == e.mathEditing || strings.HasPrefix(e.inlineEditing, id+":") {
			continue
		}
		idx, ok := e.rowIDs[id]
		if !ok {
			candidates = append(candidates, candidate{id: id, used: cached.lastUsed, row: -1})
			continue
		}
		r := &e.rows[idx]
		if r.leaf.Owner != id || !canDeferKind(r.leaf.Block.Kind) {
			continue
		}
		if r.rect.Y+r.rect.H >= winTop && r.rect.Y <= winBottom {
			continue
		}
		candidates = append(candidates, candidate{id: id, used: cached.lastUsed, row: idx})
	}
	need := len(e.cache) - maxCachedShapes
	if need <= 0 || len(candidates) == 0 {
		return
	}
	slices.SortFunc(candidates, func(a, b candidate) int {
		if a.used < b.used {
			return -1
		}
		if a.used > b.used {
			return 1
		}
		return 0
	})
	if len(candidates) > need {
		candidates = candidates[:need]
	}
	if e.heightHints == nil {
		e.heightHints = make(map[string]heightHint, len(candidates))
	}
	for _, c := range candidates {
		cached := e.cache[c.id]
		if cached.layout != nil {
			_, h := cached.layout.Size()
			h = max(h, cached.key.font.Size*cached.key.font.LineHeight)
			e.recordExactHeight(c.id, cached.width, h, cached.key.font, cached.runs)
		}
		delete(e.cache, c.id)
		if c.row >= 0 && c.row < len(e.rows) && e.rows[c.row].leaf.Block != nil && e.rows[c.row].leaf.Block.ID == c.id {
			e.rows[c.row].layout = nil
		}
	}
}
