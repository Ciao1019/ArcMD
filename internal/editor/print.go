package editor

import (
	"sort"

	"github.com/egoist/mygo/ui"
)

// Printing lays the document out on paper: Paginate breaks it into pages
// at the lines of its text, never through one, and PaintPage draws a page
// without the caret, hints or controls. The host renders the pages
// (ui.Render) and writes them out.

// Paginate lays the document out o.Width wide and returns the tops of its
// pages, each at most height tall, in the page's coordinates.
func (e *Editor) Paginate(o Options, height float32) []float32 {
	e.printing = true
	e.ready = false
	e.mathEditing = ""
	e.page = pageOf(o.Page, o.Theme)
	e.layout(o)
	end := e.contentEnd + 24
	// Where a page may end: between top-level blocks, and between the
	// lines of a paragraph that is not in a table or columns.
	cuts := []float32{end}
	for _, h := range e.handles {
		cuts = append(cuts, h.top-4)
	}
	for _, r := range e.rows {
		if r.layout == nil || r.leaf.Block.ID != r.leaf.Owner || r.image != nil || r.math != nil {
			continue
		}
		for _, l := range r.layout.Lines()[1:] {
			cuts = append(cuts, r.y+l.Rect.Y)
		}
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i] < cuts[j] })
	tops := []float32{0}
	top := float32(0)
	for top+height < end {
		next := top + height
		best := float32(-1)
		for _, c := range cuts {
			if c > top+height*0.3 && c <= next {
				best = c
			}
		}
		if best < 0 {
			best = next // nothing to break at: cut through
		}
		top = best
		tops = append(tops, top)
	}
	return tops
}

// PaintPage draws the part of the page from top, height tall, into r, as
// Paginate laid it out, with the page's colors from t. The page's next
// page starts at bottom, which the drawing stops above.
func (e *Editor) PaintPage(p *ui.Painter, r ui.Rect, top, bottom float32, t *ui.Theme) {
	e.printing = true
	e.Scroll.Y, e.viewport = top, bottom-top
	e.pointerInside = false
	e.pointer = ui.Point{X: -1e6, Y: -1e6}
	p.Clip(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: bottom - top}, 0, func() {
		e.paint(p, ui.Rect{X: r.X, Y: r.Y - top, W: r.W, H: r.H + top}, t, false)
	})
}
