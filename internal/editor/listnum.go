package editor

import (
	"strconv"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

func parseListStartInput(raw string) (int, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 999999 {
		return 0, false
	}
	return n, true
}

func (e *Editor) numberedMarkerRect(r row) ui.Rect {
	if r.leaf.Block == nil || r.leaf.Block.Kind != "numbered" || r.layout == nil {
		return ui.Rect{}
	}
	indent := r.leaf.Block.Indent
	if e.mapListNumber > 0 {
		indent = e.mapListIndent
	}
	label := listNumber(max(1, r.number), indent) + "."
	w, _ := ui.ShapeRichText([]ui.Span{{Text: label, Size: 16, Features: "tnum"}}, ui.Font{Size: 16}, 0).Size()
	line := r.layout.Caret(0)
	boxW := max(22, w+8)
	boxH := max(line.H, 26)
	return ui.Rect{
		X: r.x - 2 - boxW,
		Y: r.y + line.Y,
		W: boxW,
		H: boxH,
	}
}

func (e *Editor) hoveredNumberedMarker() *row {
	if e.ReadOnly || e.printing || !e.pointerInside {
		return nil
	}
	for i := range e.visibleRows(1) {
		r := *e.indexedRow(i)
		if r.leaf.Block == nil || r.leaf.Block.Kind != "numbered" {
			continue
		}
		dx, dy := e.visualOffset(r)
		mr := e.numberedMarkerRect(r)
		if mr.W <= 0 {
			continue
		}
		mr.X += dx
		mr.Y += dy
		if e.pointer.X >= mr.X && e.pointer.X <= mr.X+mr.W && e.pointer.Y >= mr.Y && e.pointer.Y <= mr.Y+mr.H {
			return &r
		}
	}
	return nil
}

func (e *Editor) clickNumberedMarker(x, y float32) bool {
	if e.ReadOnly || e.printing {
		return false
	}
	for i := range e.visibleRows(1) {
		r := *e.indexedRow(i)
		if r.leaf.Block == nil || r.leaf.Block.Kind != "numbered" {
			continue
		}
		dx, dy := e.visualOffset(r)
		raw := e.numberedMarkerRect(r)
		if raw.W <= 0 {
			continue
		}
		mr := ui.Rect{X: raw.X + dx, Y: raw.Y + dy, W: raw.W, H: raw.H}
		if x >= mr.X && x <= mr.X+mr.W && y >= mr.Y && y <= mr.Y+mr.H {
			e.FocusBlock(r.leaf.Block.ID)
			e.openListNumPopover(r.leaf.Block.ID, r.number, raw, false)
			return true
		}
	}
	return false
}

func (e *Editor) listNumInfo(id string) (b *document.Block, curNum, prevNum int, adjacent bool) {
	slice, idx := e.Engine.BlockSlice(id)
	if idx < 0 || idx >= len(slice) {
		return nil, 0, 0, false
	}
	b = slice[idx]
	if b == nil || b.Kind != "numbered" {
		return nil, 0, 0, false
	}
	nums := document.ListNumbers(slice[:idx+1])
	curNum = nums[idx]
	prevNum, adjacent = document.PreviousListNumber(slice, idx)
	return b, curNum, prevNum, adjacent
}

func (e *Editor) openListNumPopover(id string, curNum int, at ui.Rect, focusInput bool) {
	if e.ReadOnly || id == "" {
		return
	}
	if curNum <= 0 {
		if _, n, _, _ := e.listNumInfo(id); n > 0 {
			curNum = n
		}
	}
	e.menuOpen, e.menuSub = false, ""
	e.colorOpen, e.langOpen, e.slashOpen = false, false, false
	e.listNumOpen = true
	e.listNumOpened = false
	e.listNumID = id
	e.listNumAt = at
	e.listNumText = strconv.Itoa(max(1, curNum))
	e.listNumFocusInput = focusInput
	e.listNumRevision = e.Revision()
}

func (e *Editor) listNumPopover(c *ui.Context, surface ui.Element) {
	if !e.listNumOpen || e.ReadOnly {
		return
	}
	if e.Revision() != e.listNumRevision || c.Shortcut(0, ui.KeyEscape) {
		e.listNumOpen = false
		e.RequestFocus = true
		return
	}
	b, curNum, prevNum, adjacent := e.listNumInfo(e.listNumID)
	if b == nil {
		e.listNumOpen = false
		return
	}
	showContinue := prevNum > 0 && (!adjacent || b.Start > 0)
	showRestart := curNum > 1

	popW := float32(236)
	popH := float32(94)
	if showContinue {
		popH += 32
	}
	if showRestart {
		popH += 32
	}
	if showContinue || showRestart {
		popH += 9
	}

	bounds := e.surfaceBounds(surface)
	windowW, windowH := c.Size()
	anchor := ui.Rect{
		X: bounds.X + e.listNumAt.X,
		Y: bounds.Y + e.listNumAt.Y + e.ownerVisualDY(e.listNumID),
		W: e.listNumAt.W,
		H: e.listNumAt.H,
	}
	x, y, _ := placeMenu(anchor, popW, popH, 6, windowW, windowH)

	t := c.Theme()
	ui.Overlay(c, func() {
		panel := ui.Column(c.Key("list-num-popover")).Absolute().Left(x).Top(y).Width(popW).
			Padding(8).Gap(2).Background(e.page.Popover).Border(1, t.Border).Radius(6).
			Shadow(0, 4, 16, 0, t.Text.Alpha(.12)).Transition(popoverTransition)
		panel.Children(func() {
			if showContinue {
				hint := listNumber(prevNum+1, b.Indent) + "."
				if e.menuRow(c, "ln-continue", iconOrdered, e.page.CodeText, i18n.T("继续之前的编号"), hint, false).Clicked() {
					e.Engine.ContinueListNumbering(b.ID)
					e.sync()
					e.listNumOpen = false
					e.RequestFocus = true
					return
				}
			}
			if showRestart {
				hint := listNumber(1, b.Indent) + "."
				if e.menuRow(c, "ln-restart", iconSquarePlus, e.page.CodeText, i18n.T("开始新列表"), hint, false).Clicked() {
					e.Engine.SetListStart(b.ID, 1)
					e.sync()
					e.listNumOpen = false
					e.RequestFocus = true
					return
				}
			}
			if showContinue || showRestart {
				menuSeparator(c)
			}
			ui.Column(c).Padding(4, 6, 4, 6).Gap(6).Children(func() {
				ui.Text(c, i18n.T("修改编号值")).FontSize(12).FontWeight(500).TextColor(e.page.Caption)
				val, valid := parseListStartInput(e.listNumText)
				var apply bool
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					inp := ui.TextInput(c.Key("list-num-input"), &e.listNumText).Grow(1).MinWidth(0).
						Label(i18n.T("修改编号值")).Placeholder("1")
					if e.listNumFocusInput && !e.listNumOpened {
						inp.Focus()
						e.listNumOpened = true
					}
					btn := ui.PrimaryButton(c, i18n.T("确定")).Disabled(!valid)
					apply = inp.Submitted() || btn.Clicked()
				})
				if apply && valid {
					e.Engine.SetListStart(b.ID, val)
					e.sync()
					e.listNumOpen = false
					e.RequestFocus = true
					return
				}
				if !valid && strings.TrimSpace(e.listNumText) != "" {
					ui.Text(c, i18n.T("请输入 1–999999 的正整数")).FontSize(11.5).TextColor(c.Theme().Danger)
				}
			})
		})
		if panel.PressedOutside() {
			e.listNumOpen = false
			e.RequestFocus = true
		}
	})
}
