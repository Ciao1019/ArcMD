package editor

import (
	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/ui"
)

const columnInset = float32(12)

// layoutColumns gives each column a stable writing area. Empty columns
// remain large enough to discover and click, including when stacked.
func (e *Editor) layoutColumns(b *document.Block, x, y, w float32, hidden map[string]string, layout func(*document.Block, float32, float32, float32) float32) float32 {
	const gap, minHeight = float32(24), float32(72)
	stacked := w < 480
	cw := w
	if !stacked {
		cw = (w - float32(len(b.Children)-1)*gap) / float32(len(b.Children))
	}
	top, bottom := y, y
	for i, column := range b.Children {
		cx := x
		if !stacked {
			cx += float32(i) * (cw + gap)
		}
		start := len(e.rows)
		cy := y + columnInset
		for _, child := range column.Children {
			if hidden[child.ID] == "" {
				cy += layout(child, cx+columnInset, cy, max(24, cw-2*columnInset))
			}
		}
		h := max(minHeight, cy-y+columnInset)
		r := ui.Rect{X: cx, Y: y, W: cw, H: h}
		empty := document.Empty(column)
		if empty && start < len(e.rows) && e.rows[start].leaf.Block.Kind == "paragraph" {
			for j := start; j < len(e.rows); j++ {
				e.rows[j].emptyColumn = true
			}
			if len(e.rows) == start+1 {
				// The entire empty area accepts a click, not just its text line.
				e.rows[start].rect = r
			}
		}
		e.decorations = append(e.decorations, decoration{kind: "column", rect: r, id: column.ID, column: column, empty: empty})
		bottom = max(bottom, y+h)
		if stacked {
			y = bottom + 12
		}
	}
	return bottom - top + 12
}

// paintColumn keeps the scaffold quiet once there is content, and never
// participates in document storage or printing.
func (e *Editor) paintColumn(p *ui.Painter, r ui.Rect, d decoration) {
	active := e.focused && e.Engine.LeafAt(e.Caret).Parent == &d.column.Children
	if !d.empty && !active && !e.hovered(d.rect, 0) {
		return
	}
	p.StrokeDashed(r, e.page.TableLine, 8, 1)
}

// columnTab switches between sibling columns without inserting whitespace.
// Lists, code, table cells and selections keep their existing Tab behavior.
func (e *Editor) columnTab(backward bool) bool {
	if e.Anchor != e.Caret || e.mark != nil {
		return false
	}
	leaf := e.Engine.LeafAt(e.Caret)
	switch leaf.Block.Kind {
	case "paragraph", "heading", "quote", "callout":
	default:
		return false
	}
	var group *document.Block
	index := 0
	var find func([]*document.Block)
	find = func(blocks []*document.Block) {
		for _, b := range blocks {
			if b.Kind == "columns" {
				for i, col := range b.Children {
					if &col.Children == leaf.Parent {
						group, index = b, i
						return
					}
				}
			}
			find(b.Children)
			if group != nil {
				return
			}
		}
	}
	find(e.Doc.Blocks)
	if group == nil {
		return false
	}
	step := 1
	if backward {
		step = -1
	}
	column := group.Children[(index+step+len(group.Children))%len(group.Children)]
	// Find the first visible leaf, also for nested content and folded sections.
	ids := make(map[string]bool)
	var collect func(*document.Block)
	collect = func(b *document.Block) {
		ids[b.ID] = true
		for _, child := range b.Children {
			collect(child)
		}
	}
	collect(column)
	for _, r := range e.rows {
		if ids[r.leaf.Block.ID] {
			e.SetSelection(r.leaf.Start, r.leaf.Start)
			e.reveal, e.vertical = true, false
			return true
		}
	}
	return false
}
