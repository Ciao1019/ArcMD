package editor

// While a host resizes the viewport, retain the cached shaping of paragraphs
// well outside it. Visible paragraphs still wrap at the exact current width.
// The first frame after release lays everything out and restores the reading
// anchor, so estimated offscreen heights never leak into normal editing.
func (e *Editor) deferResizeShape(o Options, y, height float32) bool {
	if !o.Resizing || e.viewport <= 0 {
		return false
	}
	overscan := e.viewport / 2
	return y+height < e.Scroll.Y-overscan || y > e.Scroll.Y+e.viewport+overscan
}

type resizeAnchor struct {
	id     string
	offset float32
}

func (e *Editor) resizeAnchor() resizeAnchor {
	if e.Scroll.Y <= 0 {
		return resizeAnchor{}
	}
	for _, r := range e.rows {
		if r.rect.Y+r.rect.H > e.Scroll.Y {
			return resizeAnchor{r.leaf.Block.ID, e.Scroll.Y - r.rect.Y}
		}
	}
	return resizeAnchor{}
}

func (e *Editor) restoreResizeAnchor(anchor resizeAnchor) {
	if anchor.id == "" {
		return
	}
	for _, r := range e.rows {
		if r.leaf.Block.ID == anchor.id {
			e.Scroll.Y = max(0, r.rect.Y+min(anchor.offset, max(0, r.rect.H-1)))
			return
		}
	}
}
