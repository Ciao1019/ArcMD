package editor

import (
	"fmt"
	"math"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

func (e *Editor) isCodeCompactEligible(b *document.Block) bool {
	return e.codeMaxHeight > 0 && !e.printing && b != nil && b.Kind == "code" && !isMermaidBlock(b) && !isWhiteboardBlock(b)
}

func (e *Editor) applyCodeCompact(r *row, top, bottom, gap, contentH float32) float32 {
	b := r.leaf.Block
	total := top + contentH + bottom
	r.codeContentH = contentH
	r.codeViewH = contentH
	r.codeCompact = false
	r.codeExpandable = false
	if e.isCodeCompactEligible(b) && contentH > e.codeMaxHeight {
		r.codeExpandable = true
		const footerExtra = float32(20)
		if !e.codeExpanded[b.ID] {
			r.codeCompact = true
			r.codeViewH = e.codeMaxHeight
			total = top + r.codeViewH + bottom + footerExtra
		} else {
			total = top + contentH + bottom + footerExtra
		}
	}
	_ = gap
	r.rect.H = total
	return total
}

// ToggleCodeExpand toggles between compact scrolling height and full height
// for the code block with id in the current editing session.
func (e *Editor) ToggleCodeExpand(id string) {
	b := e.blockByID(id)
	if b == nil || b.Kind != "code" || isMermaidBlock(b) || isWhiteboardBlock(b) {
		return
	}
	if e.codeExpanded == nil {
		e.codeExpanded = make(map[string]bool)
	}
	next := !e.codeExpanded[id]
	e.codeExpanded[id] = next
	if next && e.codeScrollY != nil {
		delete(e.codeScrollY, id)
	}
	e.ready = false
}

// ToggleImageExpand toggles between compact scrolling height and full height
// for the image block with id in the current editing session.
func (e *Editor) ToggleImageExpand(id string) {
	b := e.blockByID(id)
	if b == nil || b.Kind != "image" || isPDFEmbed(b) || isWhiteboardEmbed(b) {
		return
	}
	if e.imageExpanded == nil {
		e.imageExpanded = make(map[string]bool)
	}
	next := !e.imageExpanded[id]
	e.imageExpanded[id] = next
	if next && e.imageScrollY != nil {
		delete(e.imageScrollY, id)
	}
	e.ready = false
}

// IsCodeCompact reports whether code block id is currently displayed compacted.
func (e *Editor) IsCodeCompact(id string) bool {
	idx, ok := e.rowIDs[id]
	if !ok {
		return false
	}
	return e.indexedRow(idx).codeCompact
}

// IsImageCompact reports whether image block id is currently displayed compacted.
func (e *Editor) IsImageCompact(id string) bool {
	idx, ok := e.rowIDs[id]
	if !ok {
		return false
	}
	f := e.indexedRow(idx).picture
	return f != nil && f.compact
}

func (e *Editor) codeVScrollFor(r row) (barBox, clipBox ui.Rect, viewH, contentH, scrollY float32, compact bool) {
	if !r.codeCompact || r.layout == nil || r.leaf.Block == nil || e.printing {
		return ui.Rect{}, ui.Rect{}, 0, 0, 0, false
	}
	viewH = r.codeViewH
	contentH = r.codeContentH
	maxScroll := max(0, contentH-viewH)
	if maxScroll <= 1 {
		return ui.Rect{}, ui.Rect{}, 0, 0, 0, false
	}
	if e.codeScrollY != nil {
		scrollY = min(max(0, e.codeScrollY[r.leaf.Block.ID]), maxScroll)
	}
	barBox = ui.Rect{X: r.rect.X + r.rect.W - 14, Y: r.y, W: 14, H: max(20, viewH)}
	clipBox = ui.Rect{X: r.rect.X, Y: r.y - 2, W: r.rect.W, H: viewH + 4}
	return barBox, clipBox, viewH, contentH, scrollY, true
}

func (e *Editor) imageVScrollFor(r row) (barBox ui.Rect, viewH, contentH, scrollY float32, compact bool) {
	f := r.picture
	if f == nil || !f.compact || r.leaf.Block == nil || e.printing {
		return ui.Rect{}, 0, 0, 0, false
	}
	viewH = f.bounds.H
	contentH = f.contentH
	maxScroll := max(0, contentH-viewH)
	if maxScroll <= 1 {
		return ui.Rect{}, 0, 0, 0, false
	}
	if e.imageScrollY != nil {
		scrollY = min(max(0, e.imageScrollY[r.leaf.Block.ID]), maxScroll)
	}
	barBox = ui.Rect{X: f.bounds.X + f.bounds.W - 14, Y: f.bounds.Y + 4, W: 14, H: max(20, f.bounds.H-8)}
	return barBox, viewH, contentH, scrollY, true
}

func setScrollY(store *map[string]float32, id string, y, maxScroll float32) {
	clamped := min(max(0, y), max(0, maxScroll))
	if *store == nil {
		*store = make(map[string]float32)
	}
	(*store)[id] = clamped
}

func (e *Editor) revealCodeOffsetV(at int) {
	leaf := e.Engine.LeafAt(at)
	if leaf.Block == nil || leaf.Block.Kind != "code" {
		return
	}
	idx, ok := e.rowIDs[leaf.Block.ID]
	if !ok {
		return
	}
	r := e.ensureRow(idx)
	_, _, viewH, contentH, scrollY, compact := e.codeVScrollFor(*r)
	if !compact || contentH <= viewH+1 {
		return
	}
	local := max(0, min(at-r.leaf.Start, r.leaf.End-r.leaf.Start))
	caret := r.layout.Caret(local)
	caretBottom := caret.Y + max(20, caret.H)
	next := scrollY
	if caret.Y < scrollY+8 {
		next = max(0, caret.Y-8)
	} else if caretBottom > scrollY+viewH-8 {
		next = min(contentH-viewH, caretBottom-viewH+16)
	}
	if next != scrollY {
		setScrollY(&e.codeScrollY, leaf.Block.ID, next, contentH-viewH)
	}
}

func handleVerticalBarEvent(ev ui.InputEvent, id string, barBox ui.Rect, viewH, contentH, scrollY float32, dragID *string, grab *float32, store *map[string]float32) bool {
	maxScroll := contentH - viewH
	bar := ui.Scrollbar{Box: barBox, Horizontal: false, View: viewH, Content: contentH, Offset: scrollY}
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button == 0 && ev.X >= barBox.X && ev.X <= barBox.X+barBox.W && ev.Y >= barBox.Y && ev.Y <= barBox.Y+barBox.H {
			start, length := bar.Thumb()
			*dragID = id
			if ev.Y >= barBox.Y+start && ev.Y <= barBox.Y+start+length {
				*grab = ev.Y - (barBox.Y + start)
			} else {
				*grab = length / 2
				setScrollY(store, id, bar.OffsetAt(ev.Y-barBox.Y-*grab), maxScroll)
			}
			return true
		}
	case ui.InputPointerMove:
		if *dragID == id {
			setScrollY(store, id, bar.OffsetAt(ev.Y-barBox.Y-*grab), maxScroll)
			return true
		}
	case ui.InputPointerUp:
		if *dragID == id {
			*dragID = ""
			return true
		}
	}
	return false
}

func (e *Editor) handleCompactScrollInput(ev ui.InputEvent) bool {
	if e.codeVBarDrag != "" {
		if idx, ok := e.rowIDs[e.codeVBarDrag]; ok {
			barBox, _, viewH, contentH, scrollY, compact := e.codeVScrollFor(*e.indexedRow(idx))
			if !compact {
				e.codeVBarDrag = ""
				return false
			}
			if handleVerticalBarEvent(ev, e.codeVBarDrag, barBox, viewH, contentH, scrollY, &e.codeVBarDrag, &e.codeVBarGrab, &e.codeScrollY) {
				return true
			}
		} else {
			e.codeVBarDrag = ""
		}
	}
	if e.imageVBarDrag != "" {
		if idx, ok := e.rowIDs[e.imageVBarDrag]; ok {
			barBox, viewH, contentH, scrollY, compact := e.imageVScrollFor(*e.indexedRow(idx))
			if !compact {
				e.imageVBarDrag = ""
				return false
			}
			if handleVerticalBarEvent(ev, e.imageVBarDrag, barBox, viewH, contentH, scrollY, &e.imageVBarDrag, &e.imageVBarGrab, &e.imageScrollY) {
				return true
			}
		} else {
			e.imageVBarDrag = ""
		}
	}

	if ev.Kind == ui.InputPointerDown && ev.Button == 0 && (e.codeMaxHeight > 0 || e.imageMaxHeight > 0) {
		for di := range e.visibleDecorations(20) {
			d := *e.indexedDecoration(di)
			if d.kind != "code" || d.id == "" {
				continue
			}
			idx, ok := e.rowIDs[d.id]
			if !ok {
				continue
			}
			barBox, _, viewH, contentH, scrollY, compact := e.codeVScrollFor(*e.indexedRow(idx))
			if compact && handleVerticalBarEvent(ev, d.id, barBox, viewH, contentH, scrollY, &e.codeVBarDrag, &e.codeVBarGrab, &e.codeScrollY) {
				return true
			}
		}
		for ri := range e.visibleRows(0) {
			r := *e.indexedRow(ri)
			if r.picture == nil {
				continue
			}
			barBox, viewH, contentH, scrollY, compact := e.imageVScrollFor(r)
			if compact && handleVerticalBarEvent(ev, r.leaf.Block.ID, barBox, viewH, contentH, scrollY, &e.imageVBarDrag, &e.imageVBarGrab, &e.imageScrollY) {
				return true
			}
		}
	}

	if ev.Kind != ui.InputScroll || ev.Mods&ui.Shift != 0 {
		return false
	}
	delta := ev.DY
	if math.Abs(float64(delta)) < 0.01 || abs32(ev.DX) > abs32(delta) {
		return false
	}
	for di := range e.visibleDecorations(20) {
		d := *e.indexedDecoration(di)
		if d.kind != "code" || d.id == "" {
			continue
		}
		if ev.X < d.rect.X || ev.X > d.rect.X+d.rect.W || ev.Y < d.rect.Y || ev.Y > d.rect.Y+d.rect.H {
			continue
		}
		idx, ok := e.rowIDs[d.id]
		if !ok {
			continue
		}
		_, _, viewH, contentH, scrollY, compact := e.codeVScrollFor(*e.indexedRow(idx))
		if !compact {
			return false
		}
		next := min(max(0, scrollY+delta), contentH-viewH)
		if next != scrollY {
			setScrollY(&e.codeScrollY, d.id, next, contentH-viewH)
			return true
		}
		return false
	}
	for ri := range e.visibleRows(0) {
		r := *e.indexedRow(ri)
		if r.picture == nil {
			continue
		}
		b := r.picture.bounds
		if ev.X < b.X || ev.X > b.X+b.W || ev.Y < b.Y || ev.Y > b.Y+b.H {
			continue
		}
		_, viewH, contentH, scrollY, compact := e.imageVScrollFor(r)
		if !compact {
			return false
		}
		next := min(max(0, scrollY+delta), contentH-viewH)
		if next != scrollY {
			setScrollY(&e.imageScrollY, r.leaf.Block.ID, next, contentH-viewH)
			return true
		}
		return false
	}
	return false
}

// handleImageBoxInput translates events delivered directly to the picture box
// (local coordinates) into editor-space compact scroll or scrollbar drag events.
func (e *Editor) handleImageBoxInput(id string, bounds ui.Rect, ev ui.InputEvent) bool {
	if ev.Kind != ui.InputScroll && ev.Kind != ui.InputPointerDown && ev.Kind != ui.InputPointerMove && ev.Kind != ui.InputPointerUp {
		return false
	}
	idx, ok := e.rowIDs[id]
	if !ok {
		return false
	}
	r := *e.indexedRow(idx)
	barBox, viewH, contentH, scrollY, compact := e.imageVScrollFor(r)
	if !compact && e.imageVBarDrag != id {
		return false
	}
	if ev.Kind == ui.InputPointerMove && ev.Button == 0 && ev.X >= 0 && ev.Y >= 0 && ev.X <= bounds.W && ev.Y <= bounds.H {
		// Button == 0 move is ignored if not dragging, so let other handlers proceed.
	}
	edEv := ev
	edEv.X += bounds.X
	edEv.Y += bounds.Y
	if e.imageVBarDrag == id || (ev.Kind == ui.InputPointerDown && ev.Button == 0 && edEv.X >= barBox.X && edEv.X <= barBox.X+barBox.W && edEv.Y >= barBox.Y && edEv.Y <= barBox.Y+barBox.H) {
		return handleVerticalBarEvent(edEv, id, barBox, viewH, contentH, scrollY, &e.imageVBarDrag, &e.imageVBarGrab, &e.imageScrollY)
	}
	if ev.Kind == ui.InputScroll && ev.Mods&ui.Shift == 0 && math.Abs(float64(ev.DY)) >= 0.01 && abs32(ev.DY) >= abs32(ev.DX) {
		next := min(max(0, scrollY+ev.DY), contentH-viewH)
		if next != scrollY {
			setScrollY(&e.imageScrollY, id, next, contentH-viewH)
			return true
		}
	}
	return false
}

func intersectRect(a, b ui.Rect) ui.Rect {
	x1 := max(a.X, b.X)
	y1 := max(a.Y, b.Y)
	x2 := min(a.X+a.W, b.X+b.W)
	y2 := min(a.Y+a.H, b.Y+b.H)
	return ui.Rect{X: x1, Y: y1, W: max(0, x2-x1), H: max(0, y2-y1)}
}

func (e *Editor) hasCodeExpandButton(d decoration) bool {
	if d.id == "" {
		return false
	}
	idx, ok := e.rowIDs[d.id]
	if !ok {
		return false
	}
	return e.indexedRow(idx).codeExpandable && e.codeExpandRect(d).W > 0
}

func (e *Editor) codeExpandRect(d decoration) ui.Rect {
	w := min(d.rect.W-24, float32(188))
	if idx, ok := e.rowIDs[d.id]; ok && !e.indexedRow(idx).codeCompact {
		w = min(d.rect.W-24, float32(116))
	}
	if w < 64 {
		return ui.Rect{}
	}
	return ui.Rect{X: d.rect.X + (d.rect.W-w)/2, Y: d.rect.Y + d.rect.H - 28, W: w, H: 22}
}

func codeLineCount(text string) int {
	if text == "" {
		return 1
	}
	return strings.Count(text, "\n") + 1
}

func paintBottomFade(p *ui.Painter, rect ui.Rect, base ui.Color, maxAlpha float32) {
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	p.FillGradient(rect, ui.LinearGradient{
		From:  base.Alpha(0),
		To:    base.Alpha(maxAlpha),
		Angle: 180,
	}, 0)
}

func (e *Editor) paintCodeCompactChrome(p *ui.Painter, bounds ui.Rect, d decoration, t *ui.Theme) {
	if e.printing || d.id == "" {
		return
	}
	idx, ok := e.rowIDs[d.id]
	if !ok {
		return
	}
	r := *e.indexedRow(idx)
	if !r.codeExpandable {
		return
	}
	ddy := e.ownerVisualDY(d.ownerID)
	barBox, _, viewH, contentH, scrollY, compact := e.codeVScrollFor(r)
	if compact {
		if scrollY < contentH-viewH-1 {
			fadeH := min(float32(32), viewH)
			paintBottomFade(p, ui.Rect{
				X: bounds.X + d.rect.X + 1,
				Y: bounds.Y + r.y + viewH - fadeH + ddy,
				W: max(0, d.rect.W-2),
				H: fadeH,
			}, e.page.CodeFill, 0.92)
		}
		if e.hovered(d.rect, 0) || e.codeVBarDrag == d.id || scrollY > 0 {
			barBox.X += bounds.X
			barBox.Y += bounds.Y + ddy
			p.Scrollbar(ui.Scrollbar{Box: barBox, Horizontal: false, View: viewH, Content: contentH, Offset: scrollY}, ui.ScrollbarState{Dragging: e.codeVBarDrag == d.id})
		}
	}

	btn := e.codeExpandRect(d)
	if btn.W <= 0 {
		return
	}
	label := i18n.T("收起代码")
	if compact {
		label = fmt.Sprintf(i18n.T("展开完整代码（共 %d 行）"), codeLineCount(r.leaf.Block.Text()))
	}
	span := ui.Span{Text: label, Size: 12, Color: e.page.Caption}
	tw, th := p.MeasureText(0, span)
	pillW := min(d.rect.W-24, max(tw+24, float32(84)))
	localPill := ui.Rect{
		X: d.rect.X + (d.rect.W-pillW)/2,
		Y: btn.Y,
		W: pillW,
		H: btn.H,
	}
	pill := ui.Rect{
		X: bounds.X + localPill.X,
		Y: bounds.Y + localPill.Y + ddy,
		W: localPill.W,
		H: localPill.H,
	}
	fill := e.page.Popover.Alpha(0.92)
	if e.hovered(localPill, 0) || e.hovered(btn, 0) {
		fill = e.page.Hover
		span.Color = t.Text
	}
	p.Fill(pill, fill, 11)
	p.Stroke(pill, e.page.CodeBorder, 11, 1)
	p.RichText(pill.X+(pill.W-min(tw, pill.W-12))/2, pill.Y+(pill.H-th)/2, max(1, pill.W-12), span)
}
