package app

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// Reuse the existing Lucide panel glyph for split controls.
var iconSplit = iconSidebar

func (a *App) splitView(c *ui.Context) {
	a.syncSplit()
	s := a.split
	bar := c.TitleBar()
	if a.folded() {
		ui.Row(c.Key("split-window-bar")).Height(max(36, bar.Height)).Padding(0, bar.Right, 0, bar.Left+4).AlignItems(ui.Center).DragWindow().Children(func() {
			if iconButton(c, "sidebar", iconSidebar, "展开侧边栏", a.withKey("展开侧边栏", "sidebar"), false).Clicked() {
				a.toggleSidebar()
			}
			ui.Spacer(c)
			a.moreMenu(c)
		})
	} else {
		// Left controls sit above the sidebar. Right controls need a full
		// title-bar band so they cannot cover the top pane's toolbar.
		height := float32(8)
		if bar.Right > 0 {
			height = max(height, bar.Height)
		}
		ui.Box(c).Height(height).DragWindow()
	}
	area := ui.Box(c.Key("split-area")).Grow(1).MinWidth(0).MinHeight(0)
	b := area.LayoutBounds()
	total := b.W
	if s.vertical {
		total = b.H
	}
	panes := slices.Clone(s.panes)
	vertical := s.vertical
	lengths := splitLengths(panes, total)
	area.Children(func() {
		// Resolve divider input before laying out either neighboring pane;
		// both cards and their editors use the same frame's geometry.
		cursor := float32(0)
		for i := 0; i < len(panes)-1; i++ {
			cursor += lengths[i]
			a.splitDivider(c, s, i, cursor, b, lengths)
			cursor = splitGap * float32(i+1)
			for _, n := range lengths[:i+1] {
				cursor += n
			}
		}
		if !a.splitResizing {
			s.dragPane = nil
		}
		cursor = 0
		for i, pane := range panes {
			x, y, w, h := cursor, float32(0), lengths[i], b.H
			if s.vertical {
				x, y, w, h = 0, cursor, b.W, lengths[i]
			}
			e := ui.Column(c.Key(fmt.Sprintf("pane:%p", pane))).Absolute().Left(x).Top(y).Size(w, h).MinWidth(0).MinHeight(0)
			pane.bounds = e.Bounds()
			e.Children(func() { a.splitPaneView(c, s, pane, i, w) })
			if a.split != s || !slices.Equal(panes, s.panes) || vertical != s.vertical {
				return
			}
			if e.FocusWithin() && a.split == s && s.active != i {
				a.focusPane(i, false)
			}
			cursor += lengths[i]
			if i < len(s.panes)-1 {
				cursor += splitGap
			}
		}
	})
}

func (a *App) splitPaneView(c *ui.Context, s *splitGroup, pane *splitPane, index int, width float32) {
	t := c.Theme()
	d := pane.doc
	card := ui.Themed(c.Key("card"), documentTheme(a.paperDark)).Grow(1).MinWidth(0).MinHeight(0).Radius(10).Clip().
		Shadow(0, 1, 4, 0, cardShadow(t)).Label(fmt.Sprintf("分屏 %d", index+1))
	if d == nil {
		card.Background(splitEmpty(t))
	}
	a.splitDropTargetFor(c, card, index)
	card.Children(func() {
		a.paneToolbar(c, s, pane, index, width)
		if d == nil {
			ui.Box(c).Height(1).Background(c.Theme().Border.Alpha(0.4))
			ui.Column(c).Grow(1).MinHeight(0).Center().Padding(16).Gap(12).Children(func() {
				ui.Icon(c, iconSplit).FontSize(28).TextColor(c.Theme().TextMuted)
				ui.Text(c, "打开另一篇文档").FontSize(15).TextColor(c.Theme().TextMuted)
				if ui.Button(c, "选择文档").Clicked() {
					a.focusPane(index, false)
					a.openPalette()
					a.palette.pane = pane
				}
			})
		} else {
			a.documentContent(c, d, width-2)
		}
	})
	card.DrawOver(func(p *ui.Painter, r ui.Rect) {
		if a.split == s && s.active == index {
			p.Stroke(r, splitFocus(t), 10, 2)
		}
	})
}

func (a *App) paneToolbar(c *ui.Context, s *splitGroup, pane *splitPane, index int, width float32) {
	d := pane.doc
	run := func(fn func()) {
		if a.split == s {
			a.focusPane(index, false)
			fn()
		}
	}
	t := c.Theme()
	bar := ui.Row(c.Key("pane-toolbar")).Height(36).PaddingX(5).Gap(2).MinWidth(0).AlignItems(ui.Center)
	full := s.active == index || bar.Hovered()
	bar.Children(func() {
		if d != nil && width >= 260 {
			if iconButton(c, "back", iconBack, fmt.Sprintf("分屏 %d 后退", index+1), "后退", pane.at <= 0).Size(26, 26).Clicked() {
				run(func() { a.goHistory(-1) })
			}
			if iconButton(c, "forward", iconForward, fmt.Sprintf("分屏 %d 前进", index+1), "前进", pane.at+1 >= len(pane.history)).Size(26, 26).Clicked() {
				run(func() { a.goHistory(1) })
			}
			if full && width >= 380 {
				if iconButton(c, "reload", iconReload, fmt.Sprintf("分屏 %d 重新载入", index+1), "重新载入", false).Size(26, 26).Clicked() {
					run(a.reload)
				}
			}
		}
		address := ui.Row(c.Key("pane-address")).Grow(1).MinWidth(0).Height(28).PaddingX(6).Gap(6).Radius(6).AlignItems(ui.Center).
			Focusable().Role(ui.RoleButton).Label(fmt.Sprintf("分屏 %d 文档路径", index+1)).Cursor(ui.CursorPointer)
		if address.Hovered() {
			address.Background(t.Text.Alpha(0.05))
		}
		address.Children(func() {
			name := "搜索或打开文档…"
			if d != nil {
				name = d.title()
				if width >= 325 {
					docIcon(c, a.iconTitle(d.space.ID, d.rel), 14)
				}
			}
			ui.Text(c, name).FontSize(12).SingleLine().Grow(1).MinWidth(0).TextColor(t.TextMuted)
			if d != nil && (d.dirty() || d.saveErr != nil || d.conflict || d.missing) {
				label := "已修改"
				if d.saveErr != nil {
					label = "保存失败"
				}
				if d.conflict || d.missing {
					label = "文件冲突"
				}
				ui.Text(c, label).FontSize(10).TextColor(t.Danger).NoWrap()
			}
		})
		if address.Clicked() {
			run(func() { a.openPalette(); a.palette.pane = pane })
		}
		if d != nil && full && width >= 480 && d.loadErr == nil && !d.source {
			d.ed.Corner(c)
		}
		if full {
			options := iconButton(c, "options", iconSplit, fmt.Sprintf("分屏 %d 选项", index+1), "分屏选项", false).Size(26, 26)
			options.PopoverMenuButton(func(m *ui.Menu) { a.splitMenu(m, index) })
		}
		if iconButton(c, "close", iconX, fmt.Sprintf("关闭分屏 %d", index+1), a.withKey("关闭此分屏", "split-close"), false).Size(26, 26).Clicked() {
			a.closePane(index)
		}
	})
}

func (a *App) splitMenu(m *ui.Menu, index int) {
	s := a.split
	if s == nil {
		return
	}
	if a.menuKey(m.Item("添加分屏"), "split-add").Disabled(len(s.panes) >= splitLimit).Chosen() {
		a.focusPane(index, false)
		a.addSplitPane()
	}
	if m.Item("从分屏中分离").Chosen() {
		a.focusPane(index, false)
		a.separateSplit()
	}
	label := "切换为上下分屏"
	if s.vertical {
		label = "切换为左右分屏"
	}
	if m.Item(label).Chosen() {
		s.vertical = !s.vertical
	}
	if m.Item("均分所有分屏").Chosen() {
		a.equalSplit()
	}
	if m.Item("向前移动此分屏").Disabled(index == 0).Chosen() {
		a.movePane(index, index-1)
	}
	if m.Item("向后移动此分屏").Disabled(index == len(s.panes)-1).Chosen() {
		a.movePane(index, index+1)
	}
	m.Separator()
	if a.menuKey(m.Item("关闭此分屏"), "split-close").Chosen() {
		a.closePane(index)
	}
}

func (a *App) movePane(from, to int) {
	s := a.split
	if s == nil || from < 0 || to < 0 || from >= len(s.panes) || to >= len(s.panes) {
		return
	}
	a.syncSplit()
	active := s.panes[s.active]
	s.panes[from], s.panes[to] = s.panes[to], s.panes[from]
	for i, p := range s.panes {
		if p == active {
			s.active = i
		}
	}
}

func (a *App) splitDivider(c *ui.Context, s *splitGroup, index int, offset float32, b ui.Rect, lengths []float32) {
	t := c.Theme()
	handle := ui.Box(c.Key(fmt.Sprintf("divider-%d", index))).Absolute().Focusable().Role(ui.RoleSplitter).
		Label(fmt.Sprintf("调整分屏 %d 大小", index+1)).Tooltip("拖动调整大小；双击均分；方向键微调")
	if s.vertical {
		handle.Left(0).Top(offset-4).Size(b.W, splitGap+8).Cursor(ui.CursorResizeNS)
	} else {
		handle.Left(offset-4).Top(0).Size(splitGap+8, b.H).Cursor(ui.CursorResizeEW)
	}
	move := func(position float32) {
		sum := lengths[index] + lengths[index+1]
		minLen := min(splitMinimum, sum/2)
		left := max(minLen, min(sum-minLen, position))
		if math.Abs(float64(left-sum/2)) < float64(splitSnap) {
			left = sum / 2
		}
		lengths[index], lengths[index+1] = left, sum-left
		total := float32(0)
		for _, n := range lengths {
			total += n
		}
		if total > 0 {
			for i, n := range lengths {
				s.panes[i].factor = n / total
			}
		}
	}
	dx, dy, held := handle.Dragged()
	a.splitResizing = a.splitResizing || held
	if held {
		if s.dragPane != s.panes[index] {
			s.dragPane = s.panes[index]
			s.dragOrigin, s.dragDistance = lengths[index], 0
		}
		delta := dx
		if s.vertical {
			delta = dy
		}
		s.dragDistance += delta
		move(s.dragOrigin + s.dragDistance)
	}
	if handle.DoubleClicked() {
		move((lengths[index+1] + lengths[index]) / 2)
	}
	if handle.Shortcut(0, ui.KeyLeft) || handle.Shortcut(0, ui.KeyUp) {
		move(lengths[index] - 10)
	}
	if handle.Shortcut(0, ui.KeyRight) || handle.Shortcut(0, ui.KeyDown) {
		move(lengths[index] + 10)
	}
	if handle.Shortcut(0, ui.KeyEnter) {
		move((lengths[index+1] + lengths[index]) / 2)
	}
	offset = splitGap * float32(index)
	for _, n := range lengths[:index+1] {
		offset += n
	}
	if s.vertical {
		handle.Top(offset - 4)
	} else {
		handle.Left(offset - 4)
	}
	alpha := handle.Animate("grip", b2f(held), 120*time.Millisecond)
	handle.Draw(func(p *ui.Painter, r ui.Rect) {
		w, h := float32(4), float32(36)
		if s.vertical {
			w, h = h, w
		}
		p.Fill(ui.Rect{X: r.X + (r.W-w)/2, Y: r.Y + (r.H-h)/2, W: w, H: h}, splitGrip(t).Alpha(alpha), 2)
	})
}

// A document dragged from the sidebar opens at the nearest page edge.
// This is a view operation; the document's file and sidebar position stay put.
func (a *App) splitDropTarget(c *ui.Context, target ui.Element) { a.splitDropTargetFor(c, target, -1) }
func (a *App) splitDropTargetFor(c *ui.Context, target ui.Element, index int) {
	d, over := ui.DragOver[dragged](target)
	dropped, ok := ui.Drop[dragged](target)
	if ok {
		d = dropped
	}
	if !over && !ok {
		return
	}
	if !vault.IsDocument(d.rel) {
		return
	}
	r := target.Bounds()
	x, y, _ := target.PointerPosition()
	vertical := min(y, r.H-y) < min(x, r.W-x)
	if a.split != nil {
		vertical = a.split.vertical
	}
	leading := x < r.W/2
	if vertical {
		leading = y < r.H/2
	}
	if ok {
		if index >= 0 {
			a.focusPane(index, false)
		}
		a.openSplit(a.state.Get(d.space), d.rel, leading, vertical)
		return
	}
	fill, rim := splitDropColors(c.Theme())
	target.DrawOver(func(p *ui.Painter, r ui.Rect) {
		if vertical {
			r.H /= 2
			if !leading {
				r.Y += r.H
			}
		} else {
			r.W /= 2
			if !leading {
				r.X += r.W
			}
		}
		r.X += 8
		r.Y += 8
		r.W -= 16
		r.H -= 16
		p.Fill(r, fill, 10)
		p.Stroke(r, rim, 10, 1.5)
	})
}

func (a *App) splitSidebarRow(c *ui.Context, sp *space.Space) {
	s := a.session(sp.ID).split
	if s == nil || len(s.panes) < 2 {
		return
	}
	row := ui.Row(c.Key("split-row")).Height(rowHeight).Padding(4).Gap(4).Radius(10).Margin(2, 0, 4, 0)
	if a.split == s {
		row.Background(glass(c.Theme()))
	} else if row.Hovered() {
		row.Background(hoverFill(c.Theme()))
	}
	row.Children(func() {
		for i, p := range slices.Clone(s.panes) {
			if i > 0 {
				ui.Box(c).Width(1).Height(18).AlignSelf(ui.Center).Background(separator(c.Theme()))
			}
			name := "新分屏"
			if p.doc != nil {
				name = p.doc.title()
			}
			cell := ui.Row(c.Key(fmt.Sprintf("pane:%p", p))).Grow(1).Basis(0).MinWidth(0).PaddingX(4).Gap(4).Radius(6).AlignItems(ui.Center).
				Focusable().Role(ui.RoleButton).Label(fmt.Sprintf("打开分屏 %d：%s", i+1, name)).Tooltip(name).Cursor(ui.CursorPointer)
			if a.split == s && s.active == i {
				cell.Background(hoverFill(c.Theme()))
			}
			cell.Children(func() {
				if p.doc != nil {
					docIcon(c, a.iconTitle(p.doc.space.ID, p.doc.rel), 14)
				} else {
					ui.Icon(c, iconSplit).FontSize(14)
				}
				ui.Text(c, name).FontSize(12).SingleLine().Grow(1).MinWidth(0)
			})
			cell.ContextMenu(func(m *ui.Menu) {
				if a.split == s {
					a.splitMenu(m, i)
				} else if m.Item("打开分屏组").Chosen() {
					a.restoreSplit(s, i)
				}
			})
			if cell.Clicked() {
				if a.split == s {
					a.focusPane(i, true)
				} else {
					a.restoreSplit(s, i)
				}
			}
		}
	})
}
