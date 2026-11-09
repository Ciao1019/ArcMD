package editor

import "github.com/egoist/mygo/ui"

// paintDim fades every top-level block but the caret's, so the paragraph
// being written stands out, as focus modes do.
func (e *Editor) paintDim(p *ui.Painter, bounds ui.Rect, t *ui.Theme) {
	if !e.dim || e.printing || len(e.handles) == 0 {
		return
	}
	owner := e.Engine.LeafAt(e.Caret).Owner
	fill := t.Background.Alpha(.62)
	for j, h := range e.handles {
		if h.id == owner {
			continue
		}
		bottom := e.contentEnd
		if j+1 < len(e.handles) {
			bottom = e.handles[j+1].top
		}
		if bottom < e.Scroll.Y || h.top > e.Scroll.Y+e.viewport {
			continue
		}
		// Up to the next block, the gap between them included, across the
		// whole page.
		p.Fill(ui.Rect{X: bounds.X, Y: bounds.Y + h.top - 4, W: e.width, H: bottom - h.top}, fill, 0)
	}
}
