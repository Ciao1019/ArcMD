package editor

import (
	"sort"

	"github.com/Ciao1019/ArcMD/internal/document"
)

// relayoutText reshapes one top-level text block and translates the following
// geometry. Containers still use the complete layout because a cell or column
// can change the heights of its siblings. Missing revisions also rebuild fully.
func (e *Editor) relayoutText(o Options) bool {
	if e.Blank() != e.laidBlank {
		return false // the starter's room comes or goes
	}
	change, ok := e.TextChangeSince(e.cacheRevision)
	if !ok {
		return false
	}
	i := sort.Search(len(e.rows), func(i int) bool { return change.Start <= e.rows[i].leaf.End })
	if i == len(e.rows) {
		return false
	}
	old := e.rows[i]
	leaf := e.Engine.LeafAt(change.Start)
	b := leaf.Block
	if b.ID != change.ID || old.leaf.Owner != b.ID || b.Kind == "heading" {
		return false
	}
	rows, decorations := e.rows, e.decorations
	e.rows, e.decorations = nil, nil
	layout := e.blockLayouter(o, map[string]document.Leaf{b.ID: leaf}, nil, make(map[string]bool))
	layout(b, e.padding, old.rect.Y, max(80, o.Width-2*e.padding))
	updated, decorated := e.rows[0], e.decorations
	e.rows, e.decorations = rows, decorations
	updated.number = old.number
	e.rows[i] = updated
	dy := updated.rect.H - old.rect.H
	units := change.AfterEnd - change.BeforeEnd
	for j := i + 1; j < len(e.rows); j++ {
		r := &e.rows[j]
		r.leaf.Start += units
		r.leaf.End += units
		r.rect.Y += dy
		r.y += dy
	}
	bottom := old.rect.Y + old.rect.H
	for j := range e.decorations {
		d := &e.decorations[j]
		if d.id == b.ID {
			// Text-only edits preserve the decoration's kind and count.
			*d = decorated[0]
			continue
		}
		if d.rect.Y >= bottom {
			d.rect.Y += dy
			if d.table.H > 0 {
				d.table.Y += dy
			}
			for k := range d.rows {
				d.rows[k] += dy
			}
		}
	}
	var handleShift float32
	for j := range e.handles {
		h := &e.handles[j]
		if h.id == b.ID {
			caret := updated.layout.Caret(0)
			previous := h.rect.Y
			h.rect.Y = updated.y + caret.Y + max(caret.H, 20)/2 - 12
			handleShift = h.rect.Y - previous
		} else if h.top >= bottom {
			h.top += dy
			h.rect.Y += dy
		}
	}
	for j := range e.resizers {
		if e.resizers[j].y >= bottom {
			e.resizers[j].y += dy
		}
	}
	for j := range e.foldButtons {
		if e.foldButtons[j].id == b.ID {
			e.foldButtons[j].rect.Y += handleShift
		} else if e.foldButtons[j].rect.Y >= bottom {
			e.foldButtons[j].rect.Y += dy
		}
	}
	for j := range e.outline {
		if e.outline[j].y >= bottom {
			e.outline[j].y += dy
		}
	}
	e.height += dy
	e.contentEnd += dy
	return true
}
