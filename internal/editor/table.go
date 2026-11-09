package editor

// Tables show bars above their columns and left of their rows: a press on
// a bar selects its row or column and shows a toolbar to insert beside it
// or delete it; a "+" pin on a boundary inserts a line there.

import (
	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/ui"
)

const (
	// tableBar is the thickness of the bars; tableBarGap parts them from
	// the grid.
	tableBar    = 10
	tableBarGap = 4
)

// tableSelection is a selected row or column of a table.
type tableSelection struct {
	id    string
	rows  bool
	index int
	ok    bool
}

// columnBar and rowBar are the bar segments of column or row i.
func columnBar(d decoration, i int) ui.Rect {
	return ui.Rect{X: d.cols[i], Y: d.rect.Y - tableBarGap - tableBar, W: d.cols[i+1] - d.cols[i], H: tableBar}
}
func rowBar(d decoration, i int) ui.Rect {
	return ui.Rect{X: d.rect.X - tableBarGap - tableBar, Y: d.rows[i], W: tableBar, H: d.rows[i+1] - d.rows[i]}
}

// pin is the "+" on boundary k of the column bar (cols) or the row bar.
func pin(d decoration, cols bool, k int) ui.Rect {
	if cols {
		return ui.Rect{X: d.cols[k] - 8, Y: d.rect.Y - tableBarGap - tableBar/2 - 8, W: 16, H: 16}
	}
	return ui.Rect{X: d.rect.X - tableBarGap - tableBar/2 - 8, Y: d.rows[k] - 8, W: 16, H: 16}
}

// barsShown reports whether table d shows its bars: under the pointer,
// holding the caret, or with a line selected.
func (e *Editor) barsShown(d decoration) bool {
	if e.tableSel.ok && e.tableSel.id == d.id {
		return true
	}
	if e.focused && e.Engine.LeafAt(e.Caret).Owner == d.id {
		return true
	}
	return e.hovered(ui.Rect{X: d.rect.X - 24, Y: d.rect.Y - 24, W: d.rect.W + 24, H: d.rect.H + 24}, 0)
}

// hoveredPin is the boundary pin under the pointer, if any.
func (e *Editor) hoveredPin(d decoration) (cols bool, k int, ok bool) {
	p := e.pointer
	if !e.pointerInside || e.dragging {
		return false, 0, false
	}
	near := func(r ui.Rect) bool { return p.X >= r.X && p.X <= r.X+r.W && p.Y >= r.Y && p.Y <= r.Y+r.H }
	for k := range d.cols {
		if near(pin(d, true, k)) {
			return true, k, true
		}
	}
	for k := range d.rows {
		if near(pin(d, false, k)) {
			return false, k, true
		}
	}
	return false, 0, false
}

// selectedLine is the table selection's decoration, if it still exists.
func (e *Editor) selectedLine() (decoration, bool) {
	if !e.tableSel.ok {
		return decoration{}, false
	}
	for _, d := range e.decorations {
		if d.kind == "table" && d.id == e.tableSel.id {
			n := len(d.cols) - 1
			if e.tableSel.rows {
				n = len(d.rows) - 1
			}
			if e.tableSel.index < n {
				return d, true
			}
		}
	}
	e.tableSel = tableSelection{}
	return decoration{}, false
}

// paintTables draws the bars, pins and the selected line of each table.
func (e *Editor) paintTables(p *ui.Painter, bounds ui.Rect) {
	offset := func(r ui.Rect) ui.Rect { r.X += bounds.X; r.Y += bounds.Y; return r }
	pg := e.page
	for _, d := range e.decorations {
		// Include the bars and insertion pins above and beside the table.
		if d.kind != "table" || d.rect.Y+d.rect.H+24 < e.Scroll.Y || d.rect.Y-24 > e.Scroll.Y+e.viewport || len(d.cols) < 2 || !e.barsShown(d) {
			continue
		}
		sel := e.tableSel
		selected := func(rows bool, i int) bool { return sel.ok && sel.id == d.id && sel.rows == rows && sel.index == i }
		// The selected line's cells, red while its delete is under the
		// pointer.
		if sel.ok && sel.id == d.id {
			var r ui.Rect
			if sel.rows && sel.index < len(d.rows)-1 {
				r = ui.Rect{X: d.rect.X, Y: d.rows[sel.index], W: d.rect.W, H: d.rows[sel.index+1] - d.rows[sel.index]}
			} else if !sel.rows && sel.index < len(d.cols)-1 {
				r = ui.Rect{X: d.cols[sel.index], Y: d.rect.Y, W: d.cols[sel.index+1] - d.cols[sel.index], H: d.rect.H}
			}
			fill := pg.CellSelected
			if e.tableDanger {
				fill = pg.DangerCell
			}
			p.Clip(offset(d.rect), 8, func() { p.Fill(offset(r), fill, 0) })
		}
		if id, r, ok := e.cellRange(); ok && id == d.id {
			cells := ui.Rect{X: d.cols[r.Col0], Y: d.rows[r.Row0], W: d.cols[r.Col1+1] - d.cols[r.Col0], H: d.rows[r.Row1+1] - d.rows[r.Row0]}
			p.Clip(offset(d.rect), 8, func() { p.Fill(offset(cells), pg.CellSelected, 0) })
		}
		for i := 0; i < len(d.cols)-1; i++ {
			e.paintBar(p, offset(columnBar(d, i)), columnBar(d, i), selected(false, i))
		}
		for i := 0; i < len(d.rows)-1; i++ {
			e.paintBar(p, offset(rowBar(d, i)), rowBar(d, i), selected(true, i))
		}
		if cols, k, ok := e.hoveredPin(d); ok {
			r := offset(pin(d, cols, k))
			// The line the insert would go along.
			if cols {
				p.Fill(ui.Rect{X: r.X + 7, Y: bounds.Y + d.rect.Y, W: 2, H: d.rect.H}, pg.Marker, 1)
			} else {
				p.Fill(ui.Rect{X: bounds.X + d.rect.X, Y: r.Y + 7, W: d.rect.W, H: 2}, pg.Marker, 1)
			}
			p.Fill(r, pg.Marker, 8)
			p.Icon(iconPlus, ui.Rect{X: r.X + 3, Y: r.Y + 3, W: 10, H: 10}, pg.OnMarker)
		}
	}
}

// paintBar draws one bar segment at r (local is its page rectangle).
func (e *Editor) paintBar(p *ui.Painter, r, local ui.Rect, selected bool) {
	pg := e.page
	fill := pg.BarFill
	switch {
	case selected && e.tableDanger:
		fill = pg.Danger
	case selected:
		fill = pg.BarSelected
	case e.hovered(local, 0):
		fill = pg.BarHover
	}
	if r.W > r.H {
		r.X, r.W = r.X+1, r.W-2
	} else {
		r.Y, r.H = r.Y+1, r.H-2
	}
	p.Fill(r, fill, 3)
}

// tableControl runs a press on table d's pins or bars.
func (e *Editor) tableControl(d decoration, x, y float32) bool {
	if len(d.cols) < 2 || !e.barsShown(d) {
		return false
	}
	in := func(r ui.Rect) bool { return x >= r.X && x <= r.X+r.W && y >= r.Y && y <= r.Y+r.H }
	insert := func(cols bool, k int) {
		e.tableSel = tableSelection{}
		e.Engine.TableInsert(d.id, !cols, k)
		e.sync()
		e.RequestFocus, e.reveal = true, true
	}
	for k := range d.cols {
		if in(pin(d, true, k)) {
			insert(true, k)
			return true
		}
	}
	for k := range d.rows {
		if in(pin(d, false, k)) {
			insert(false, k)
			return true
		}
	}
	for i := 0; i < len(d.cols)-1; i++ {
		if in(columnBar(d, i)) {
			e.tableSel = tableSelection{id: d.id, index: i, ok: true}
			return true
		}
	}
	for i := 0; i < len(d.rows)-1; i++ {
		if in(rowBar(d, i)) {
			e.tableSel = tableSelection{id: d.id, rows: true, index: i, ok: true}
			return true
		}
	}
	return false
}

// SelectTableLine selects row (rows) or column index of table id, as a
// press on its bar does.
func (e *Editor) SelectTableLine(id string, rows bool, index int) {
	e.tableSel = tableSelection{id: id, rows: rows, index: index, ok: true}
}

// deleteTableLine deletes the selected row or column.
func (e *Editor) deleteTableLine() {
	sel := e.tableSel
	e.tableSel, e.tableDanger = tableSelection{}, false
	e.Engine.TableDelete(sel.id, sel.rows, sel.index)
	e.sync()
	e.RequestFocus, e.reveal = true, true
}

// cellRange is the rectangle of cells a text selection spans when it
// runs across two or more cells of one table, grown over merged cells.
func (e *Editor) cellRange() (string, document.CellRange, bool) {
	sel := e.Selection().Range
	if sel.Start == sel.End {
		return "", document.CellRange{}, false
	}
	a, z := e.Engine.LeafAt(sel.Start), e.Engine.LeafAt(sel.End)
	if a.Block == z.Block || a.Block.Kind != "cell" || z.Block.Kind != "cell" || a.Owner != z.Owner {
		return "", document.CellRange{}, false
	}
	table, r0, c0, ok0 := e.Doc.CellAt(a.Block.ID)
	_, r1, c1, ok1 := e.Doc.CellAt(z.Block.ID)
	if !ok0 || !ok1 {
		return "", document.CellRange{}, false
	}
	r := document.CellRange{Row0: min(r0, r1), Col0: min(c0, c1), Row1: max(r0, r1), Col1: max(c0, c1)}
	return table.ID, document.Expand(table, r), true
}

// caretCell is the merged cell holding the caret, if any.
func (e *Editor) caretCell() (*document.Block, bool) {
	leaf := e.Engine.LeafAt(e.Caret)
	if leaf.Block.Kind != "cell" {
		return nil, false
	}
	cols, rows := document.Span(leaf.Block)
	return leaf.Block, cols > 1 || rows > 1
}

// cellFillButton opens the cell background panel.
func (e *Editor) cellFillButton(c *ui.Context) {
	b := ui.ButtonBase(c.Key("table:fill")).Size(24, 24).Radius(4).Center().Label("单元格背景颜色").Tooltip("单元格背景颜色").KeepFocus().Cursor(ui.CursorPointer)
	if b.Hovered() || e.cellFillOpen {
		b.Background(e.page.Hover)
	}
	b.Children(func() { ui.Icon(c, iconPaintBucket).FontSize(18).TextColor(e.page.CodeText) })
	if b.Clicked() {
		e.cellFillOpen, e.cellFillOpened = !e.cellFillOpen, true
	}
}

// cellFillPanel picks the background of the cells of r in table id.
func (e *Editor) cellFillPanel(c *ui.Context, x, y float32, id string, r document.CellRange, current string) {
	if !e.cellFillOpen {
		return
	}
	t := c.Theme()
	pg := e.page
	panel := ui.Column(c.Key("cell-fill-panel")).Absolute().Left(max(8, x)).Top(y).Width(230).Padding(4, 10, 12, 10)
	e.popover(c, panel).Children(func() {
		ui.Text(c, "单元格背景颜色").FontSize(12).TextColor(t.Text).Margin(7, 0, 4, 0)
		names := append([]string{""}, document.TextHighlights...)
		ui.Column(c).Gap(4).Children(func() {
			for row := 0; row < len(names); row += 8 {
				ui.Row(c.Key(row)).Justify(ui.SpaceBetween).Children(func() {
					for _, name := range names[row:min(row+8, len(names))] {
						label := "无背景颜色"
						if name != "" {
							label = "单元格背景：" + ColorName(name, true)
						}
						s := ui.ButtonBase(c.Key("cf:"+name)).Size(26, 26).Radius(4).Center().Label(label).Tooltip(label).KeepFocus().Cursor(ui.CursorPointer)
						if current == name {
							s.Border(2, pg.CodeHover)
						} else if s.Hovered() {
							s.Background(pg.Hover)
						}
						s.Children(func() {
							box := ui.Box(c).Size(22, 22).Radius(2).Border(1, pg.Hover)
							if name == "" {
								box.Background(t.Background).Center().Children(func() {
									ui.Icon(c, iconSlash).FontSize(20).TextColor(pg.Placeholder)
								})
							} else {
								box.Background(pg.CellFills[name])
							}
						})
						if s.Clicked() {
							e.Engine.SetCellBackground(id, r, name)
							e.cellFillOpen = false
							e.RequestFocus = true
						}
					}
				})
			}
		})
	})
	if panel.PressedOutside() && !e.cellFillOpened {
		e.cellFillOpen = false
	}
	e.cellFillOpened = false
}

// tableToolbar floats over a selected row or column (insert before,
// insert after, background, delete) or over cells selected across (merge,
// split, background).
func (e *Editor) tableToolbar(c *ui.Context, surface ui.Element) {
	bounds := surface.Bounds()
	if id, r, ok := e.cellRange(); ok && e.focused && !e.dragging {
		e.cellToolbar(c, bounds, id, r)
		return
	}
	d, ok := e.selectedLine()
	if !ok {
		e.tableDanger, e.cellFillOpen = false, false
		return
	}
	sel := e.tableSel
	x, y := d.cols[sel.index], d.rect.Y-tableBarGap-tableBar-46
	before, after, remove := "在左侧插入列", "在右侧插入列", "删除列"
	beforeIcon, afterIcon := iconInsertLeft, iconInsertRight
	lines := document.CellRange{Row0: 0, Col0: sel.index, Row1: len(d.rows) - 2, Col1: sel.index}
	if sel.rows {
		x, y = d.rect.X, d.rows[sel.index]-46
		before, after, remove = "在上方插入行", "在下方插入行", "删除行"
		beforeIcon, afterIcon = iconInsertAbove, iconInsertBelow
		lines = document.CellRange{Row0: sel.index, Col0: 0, Row1: sel.index, Col1: len(d.cols) - 2}
	}
	ui.Overlay(c, func() {
		bar := ui.Row(c.Key("table-toolbar")).Absolute().Left(bounds.X + x).Top(bounds.Y + y).Height(40).PaddingX(8).Gap(8).AlignItems(ui.Center)
		e.popover(c, bar).Children(func() {
			insert := func(at int) {
				id, rows := sel.id, sel.rows
				e.tableSel, e.tableDanger = tableSelection{}, false
				e.Engine.TableInsert(id, rows, at)
				e.sync()
				e.RequestFocus, e.reveal = true, true
			}
			if markButton(c, e.page, "table:before", beforeIcon, before, false).Clicked() {
				insert(sel.index)
			}
			if markButton(c, e.page, "table:after", afterIcon, after, false).Clicked() {
				insert(sel.index + 1)
			}
			e.cellFillButton(c)
			ui.Box(c).Size(1, 20).Background(c.Theme().Border)
			del := ui.ButtonBase(c.Key("table:delete")).Size(24, 24).Radius(4).Center().Label(remove).Tooltip(remove + " (⌫)").KeepFocus().Cursor(ui.CursorPointer)
			e.tableDanger = del.Hovered()
			if e.tableDanger {
				del.Background(e.page.DangerCell)
			}
			del.Children(func() { ui.Icon(c, iconTrash).FontSize(18).TextColor(e.page.Danger) })
			if del.Clicked() {
				e.deleteTableLine()
			}
		})
		e.cellFillPanel(c, bounds.X+x, bounds.Y+y+44, sel.id, lines, "")
	})
}

// cellToolbar floats over cells selected across: merge, split and their
// background.
func (e *Editor) cellToolbar(c *ui.Context, bounds ui.Rect, id string, r document.CellRange) {
	var d decoration
	for _, dd := range e.decorations {
		if dd.kind == "table" && dd.id == id {
			d = dd
		}
	}
	if len(d.cols) <= r.Col1+1 || len(d.rows) <= r.Row1+1 {
		return
	}
	table, _, _, _ := e.Doc.CellAt(e.Engine.LeafAt(e.Selection().Range.Start).Block.ID)
	if table == nil {
		return
	}
	merged := false
	for ri := r.Row0; ri <= r.Row1; ri++ {
		for ci := r.Col0; ci <= r.Col1; ci++ {
			if cols, rows := document.Span(table.Children[ri].Children[ci]); cols > 1 || rows > 1 {
				merged = true
			}
		}
	}
	single := merged && func() bool {
		cols, rows := document.Span(table.Children[r.Row0].Children[r.Col0])
		return r.Row1-r.Row0+1 == rows && r.Col1-r.Col0+1 == cols
	}()
	x, y := d.cols[r.Col0], d.rows[r.Row0]-46
	current := table.Children[r.Row0].Children[r.Col0].Background
	ui.Overlay(c, func() {
		bar := ui.Row(c.Key("cell-toolbar")).Absolute().Left(bounds.X + x).Top(bounds.Y + y).Height(40).PaddingX(8).Gap(8).AlignItems(ui.Center)
		e.popover(c, bar).Children(func() {
			if !single && markButton(c, e.page, "cell:merge", iconMerge, "合并单元格", false).Clicked() {
				e.Engine.MergeCells(id, r)
				e.sync()
				e.RequestFocus = true
			}
			if merged && markButton(c, e.page, "cell:split", iconSplit, "拆分单元格", false).Clicked() {
				e.Engine.Edit(func() {
					for ri := r.Row0; ri <= r.Row1; ri++ {
						for ci := r.Col0; ci <= r.Col1; ci++ {
							if cell := table.Children[ri].Children[ci]; cell.ColSpan > 1 || cell.RowSpan > 1 {
								e.Engine.SplitCell(cell.ID)
							}
						}
					}
				})
				e.sync()
				e.RequestFocus = true
			}
			e.cellFillButton(c)
		})
		e.cellFillPanel(c, bounds.X+x, bounds.Y+y+44, id, r, current)
	})
}

// MergeSelectedCells merges the cells the selection runs across.
func (e *Editor) MergeSelectedCells() {
	if id, r, ok := e.cellRange(); ok {
		e.Engine.MergeCells(id, r)
		e.sync()
	}
}

// SplitCaretCell splits the merged cell holding the caret.
func (e *Editor) SplitCaretCell() {
	if cell, ok := e.caretCell(); ok {
		e.Engine.SplitCell(cell.ID)
		e.sync()
	}
}
