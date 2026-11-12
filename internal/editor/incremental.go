package editor

import (
	"slices"
	"sort"

	"github.com/Ciao1019/ArcMD/internal/document"
)

// relayoutText reshapes one top-level block and translates the following
// geometry. Tables rebuild their own cells together; columns and missing
// revisions retain the complete-layout fallback.
func (e *Editor) relayoutText(o Options) bool {
	if e.Blank() != e.laidBlank {
		return false // the starter's room comes or goes
	}
	change, ok := e.TextChangeSince(e.cacheRevision)
	if !ok {
		return false
	}
	i, found := e.rowIDs[change.ID]
	if !found {
		return false
	}
	old := *e.indexedRow(i)
	leaf := e.Engine.LeafAt(change.Start)
	b := leaf.Block
	if b.ID != change.ID || old.leaf.Block.ID != change.ID {
		return false
	}
	if old.leaf.Owner != b.ID {
		return e.relayoutTable(o, change, i)
	}
	rows, decorations := e.rows, e.decorations
	e.rows, e.decorations = nil, nil
	layout := e.blockLayouter(o, map[string]document.Leaf{b.ID: leaf}, nil, make(map[string]bool))
	layout(b, e.padding, old.rect.Y, max(80, o.Width-2*e.padding))
	updated, decorated := e.rows[0], e.decorations
	e.rows, e.decorations = rows, decorations
	updated.number = old.number
	e.rows[i] = updated
	e.rowSpace.update(i, updated.rect)
	dy := updated.rect.H - old.rect.H
	decStart, decEnd := -1, -1
	if j, ok := e.decorationIDs[b.ID]; ok {
		e.decorations[j] = decorated[0]
		e.decorationSpace.update(j, decorated[0].rect)
		decStart, decEnd = j, j+1
	}
	if b.Kind == "heading" {
		// The heading's identity and level did not change; only its label
		// and presence in the outline can change in a text transaction.
		at := slices.IndexFunc(e.outline, func(item outlineItem) bool { return item.id == b.ID })
		text := b.Text()
		switch {
		case at >= 0 && text == "":
			e.outline = slices.Delete(e.outline, at, at+1)
		case at >= 0:
			e.outline[at].text = text
		case text != "":
			at = sort.Search(len(e.outline), func(j int) bool { return e.outline[j].y >= old.rect.Y })
			e.outline = slices.Insert(e.outline, at, outlineItem{b.ID, text, b.Level, old.rect.Y})
		}
	}
	handleShift := float32(0)
	if j, ok := e.handleIDs[b.ID]; ok {
		h := &e.handles[j]
		caret := updated.layout.Caret(0)
		previous := h.rect.Y
		h.rect.Y = updated.y + caret.Y + max(caret.H, 20)/2 - 12
		handleShift = h.rect.Y - previous
	}
	e.shiftFollowing(i+1, old.rect.Y+old.rect.H, dy, decStart, decEnd, b.ID, handleShift)
	return true
}

// A cell can change the heights of other cells in its row, including merged
// cells. Reuse the full table layouter so those constraints stay identical,
// without walking or reshaping unrelated blocks in the document.
func (e *Editor) relayoutTable(o Options, change document.TextChange, rowIndex int) bool {
	owner := e.rows[rowIndex].leaf.Owner
	var table *document.Block
	if j, ok := e.handleIDs[owner]; ok {
		table = e.Doc.Blocks[e.handles[j].index]
	}
	if table == nil || table.Kind != "table" || e.resizePending || o.Resizing {
		return false
	}
	di, found := e.decorationIDs[owner]
	decEnd := di + 1
	if !found || e.decorations[di].kind != "table" {
		return false
	}
	oldTable := e.decorations[decEnd-1]
	from, to := rowIndex, rowIndex+1
	for from > 0 && e.rows[from-1].leaf.Owner == owner {
		from--
	}
	for to < len(e.rows) && e.rows[to].leaf.Owner == owner {
		to++
	}
	leaves := make(map[string]document.Leaf, to-from)
	for _, row := range e.rows[from:to] {
		leaf, ok := e.Engine.LeafByID(row.leaf.Block.ID)
		if !ok {
			return false
		}
		leaves[leaf.Block.ID] = leaf
	}
	rows, decorations, resizers := e.rows, e.decorations, e.resizers
	e.rows, e.decorations, e.resizers = nil, nil, nil
	layout := e.blockLayouter(o, leaves, nil, make(map[string]bool))
	start := oldTable.rect.Y - 22
	height := layout(table, oldTable.rect.X, start, oldTable.rect.W)
	updated, decorated, handles := e.rows, e.decorations, e.resizers
	e.rows, e.decorations, e.resizers = rows, decorations, resizers
	// Text-only edits preserve cells, decorations and resize handles.
	copy(e.rows[from:to], updated)
	for j := from; j < to; j++ {
		e.rowSpace.update(j, e.rows[j].rect)
	}
	decStart := decEnd - len(decorated)
	copy(e.decorations[decStart:decEnd], decorated)
	for j := decStart; j < decEnd; j++ {
		e.decorationSpace.update(j, e.decorations[j].rect)
	}
	next := 0
	for j := range e.resizers {
		if e.resizers[j].id == owner {
			e.resizers[j] = handles[next]
			next++
		}
	}
	oldHeight := oldTable.rect.H + 44
	e.shiftFollowing(to, start+oldHeight, height-oldHeight, decStart, decEnd, owner, 0)
	return true
}

func (e *Editor) shiftFollowing(after int, bottom, dy float32, decStart, decEnd int, owner string, handleShift float32) {
	if dy == 0 && handleShift == 0 {
		return
	}
	for j := after; j < len(e.rows); j++ {
		r := &e.rows[j]
		r.rect.Y += dy
		r.y += dy
		if f := r.picture; f != nil {
			f.bounds.Y += dy
			for i := range f.actions {
				f.actions[i].bounds.Y += dy
			}
		}
	}
	e.rowSpace.refitSuffix(after)
	firstDecoration := len(e.decorations)
	for j := range e.decorations {
		d := &e.decorations[j]
		if j >= decStart && j < decEnd {
			continue
		}
		if d.rect.Y >= bottom {
			firstDecoration = min(firstDecoration, j)
			d.rect.Y += dy
			if d.table.H > 0 {
				d.table.Y += dy
			}
			for k := range d.rows {
				d.rows[k] += dy
			}
		}
	}
	e.decorationSpace.refitSuffix(firstDecoration)
	for j := range e.handles {
		h := &e.handles[j]
		if h.id != owner && h.top >= bottom {
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
		if e.foldButtons[j].id == owner {
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
}
