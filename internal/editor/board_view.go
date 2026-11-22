package editor

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
	"github.com/Ciao1019/ArcMD/internal/wiki"
	"github.com/egoist/mygo/ui"
)

type toolDef struct {
	tool  BoardTool
	icon  *ui.SVG
	label string
	key   string
}

var boardTools = []toolDef{
	{ToolHand, iconHand, "抓手", "H"},
	{ToolSelect, iconPointer, "选择", "V"},
	{ToolRect, iconSquare, "矩形", "R"},
	{ToolDiamond, iconDiamond, "菱形", "D"},
	{ToolEllipse, iconCircle, "椭圆", "O"},
}

var boardSecondaryTools = []toolDef{
	{ToolArrow, iconArrowUpRight, "箭头", "A"},
	{ToolLine, iconMinusLine, "直线", "L"},
	{ToolFreedraw, iconPencil, "自由画笔", "P"},
	{ToolText, iconType, "文字", "T"},
	{ToolNote, iconStickyNote, "文档便签", "N"},
	{ToolFrame, iconFrame, "画框", "F"},
	{ToolLaser, iconLaser, "激光笔", "K"},
	{ToolEraser, iconEraser, "橡皮擦", "E"},
}

// BoardView renders an interactive Excalidraw-style whiteboard inside the
// surrounding container and returns its root element.
func BoardView(c *ui.Context, b *Board, o BoardOptions) ui.Element {
	if b == nil {
		return ui.Box(c)
	}
	if o.Theme == nil {
		o.Theme = c.Theme()
	}
	pg := pageOf(o.Page, o.Theme)
	wb := pg.Whiteboard
	b.ReadOnly = o.ReadOnly

	root := ui.Box(c.Key("whiteboard-root")).Grow(1).WidthPercent(100).MinWidth(0).MinHeight(0).Clip()
	root.Children(func() {
		canvas := ui.Box(c.Key("board-canvas")).Fill().Focusable().FocusRing(false).
			Role(ui.RoleGroup).Label(i18n.T("手绘白板"))
		if b.RequestFocus {
			canvas.Focus()
			b.RequestFocus = false
		}

		rect := canvas.LayoutBounds()
		if rect.W > 0 && rect.H > 0 {
			b.lastRect = rect
			b.ensureViewportFit(rect.W, rect.H)
		} else {
			c.After(0)
		}

		canvas.Cursor(b.cursorFor(canvas))
		services := c.Services()
		canvas.HandleInput(func(ev ui.InputEvent) bool {
			return b.handleInput(ev, b.lastRect, services.ReadClipboard, services.WriteClipboard, o)
		})
		canvas.ContextMenu(func(m *ui.Menu) {
			b.buildContextMenu(m, services.ReadClipboard, services.WriteClipboard, o)
		})
		canvas.Draw(func(p *ui.Painter, r ui.Rect) {
			b.lastRect = r
			b.ensureViewportFit(r.W, r.H)
			b.paintCanvas(p, r, wb, o.Theme, o)
			if len(b.Scene.Elements) == 0 && b.editingID == "" {
				b.paintEmptyPrompt(p, r, wb, o.Theme)
			}
		})

		b.inlineTextOverlay(c, wb, o)

		if !b.ReadOnly {
			b.floatingToolIsland(c, wb, o)
			if o.Compact {
				b.compactStyleBar(c, wb, o)
			} else {
				b.floatingStylePanel(c, wb, o)
			}
			b.shapesPopover(c, wb, o)
			b.mermaidPopover(c, wb, o)
			b.libraryDrawer(c, wb, o)
		}
		b.bottomZoomBar(c, wb, o)
		b.linkPickerPopover(c, wb, o)

		if b.ReadOnly && !o.Compact {
			b.readOnlyChip(c, wb, o)
		}
	})
	return root
}

// Corner renders the whiteboard controls in the document toolbar's right corner.
func (b *Board) Corner(c *ui.Context, o BoardOptions, setReadOnly func(bool)) {
	if b == nil {
		return
	}
	if o.Theme == nil {
		o.Theme = c.Theme()
	}
	pg := pageOf(o.Page, o.Theme)
	wb := pg.Whiteboard
	t := o.Theme

	ui.Row(c.Key("board-corner")).Gap(2).AlignItems(ui.Center).Children(func() {
		count := len(b.Scene.Elements)
		countLabel := fmt.Sprintf(i18n.T("%d 个图形"), count)
		if count == 0 {
			countLabel = i18n.T("白板暂无图形")
		}
		ui.Text(c, countLabel).FontSize(12).TextColor(t.TextMuted).Padding(0, 6).NoWrap()

		links := b.Scene.WikiLinks()
		var mentions []string
		if o.LinkedMentions != nil {
			mentions = o.LinkedMentions()
		}
		linkAnchor := toolButton(c, "board-links", iconLink, i18n.T("关联文档"), b.linksPanelOpen)
		if linkAnchor.Clicked() {
			b.linksPanelOpen = !b.linksPanelOpen
		}
		if b.linksPanelOpen {
			b.linkedDocsPopover(c, linkAnchor, wb, t, links, mentions, o)
		}

		if o.ExportSVG != nil {
			if toolButton(c, "board-svg", iconCopy, i18n.T("导出 SVG"), false).Clicked() {
				o.ExportSVG()
			}
		}

		icon, tip := iconUnlocked, i18n.T("编辑中，点击设为只读")
		if b.ReadOnly {
			icon, tip = iconLocked, i18n.T("只读，点击恢复编辑")
		}
		if toolButton(c, "board-lock", icon, tip, b.ReadOnly).Clicked() && setReadOnly != nil {
			setReadOnly(!b.ReadOnly)
		}
	})
}

func (b *Board) linkedDocsPopover(c *ui.Context, anchor ui.Element, wb WhiteboardStyle, t *ui.Theme, links []wiki.Link, mentions []string, o BoardOptions) {
	ui.Popover(c, anchor, &b.linksPanelOpen, func() {
		col := ui.Column(c.Key("board-links-pop")).Width(250).Padding(10).Gap(8).Radius(10).
			Background(wb.IslandFill).Border(1, wb.IslandBorder).Shadow(0, 6, 18, 0, wb.IslandShadow)
		col.Children(func() {
			seen := map[string]bool{}
			var targets []string
			for _, l := range links {
				k := strings.ToLower(l.Target)
				if !seen[k] {
					seen[k] = true
					targets = append(targets, l.Target)
				}
			}
			if len(targets) == 0 && len(mentions) == 0 {
				ui.Text(c, i18n.T("白板暂无关联文档")).FontSize(13).FontWeight(500).TextColor(t.Text)
				ui.Text(c, i18n.T("在图形属性或文字中输入 [[文档名]] 即可关联")).FontSize(12).TextColor(t.TextMuted)
				return
			}
			if len(targets) > 0 {
				ui.Text(c, i18n.T("当前白板关联的文档")).FontSize(11).FontWeight(600).TextColor(t.TextMuted)
				for _, target := range targets {
					row := ui.ButtonBase(c.Key("link-to:" + target)).Height(28).PaddingX(8).Radius(6).
						Gap(6).AlignItems(ui.Center).Cursor(ui.CursorPointer)
					if row.Hovered() {
						row.Background(t.SurfaceHover)
					}
					row.Children(func() {
						ui.Icon(c, iconLink).FontSize(13).TextColor(wb.ActiveInk)
						ui.Text(c, target).FontSize(13).TextColor(t.Text).SingleLine().Grow(1)
						ui.Text(c, "↗").FontSize(12).TextColor(t.TextMuted)
					})
					if row.Clicked() && o.OpenWiki != nil {
						b.linksPanelOpen = false
						o.OpenWiki(target, false)
					}
				}
			}
			if len(mentions) > 0 {
				ui.Text(c, i18n.T("引用了当前白板的文档")).FontSize(11).FontWeight(600).TextColor(t.TextMuted).Padding(4, 0, 0, 0)
				for _, doc := range mentions {
					row := ui.ButtonBase(c.Key("mention:" + doc)).Height(28).PaddingX(8).Radius(6).
						Gap(6).AlignItems(ui.Center).Cursor(ui.CursorPointer)
					if row.Hovered() {
						row.Background(t.SurfaceHover)
					}
					row.Children(func() {
						ui.Icon(c, iconParagraph).FontSize(13).TextColor(t.TextMuted)
						ui.Text(c, doc).FontSize(13).TextColor(t.Text).SingleLine().Grow(1)
					})
					if row.Clicked() && o.OpenWiki != nil {
						b.linksPanelOpen = false
						o.OpenWiki(doc, false)
					}
				}
			}
		})
	})
}

func (b *Board) paintEmptyPrompt(p *ui.Painter, rect ui.Rect, wb WhiteboardStyle, t *ui.Theme) {
	msg := i18n.T("从工具栏选择图形或直接拖拽绘制，双击任意处输入文字，支持 [[文档]] 双链")
	tl := ui.ShapeRichTextWith(
		[]ui.Span{{Text: msg, Size: 13}},
		ui.Font{Family: "system-ui", Size: 13},
		ui.TextShape{Width: max(120, rect.W-64), Align: ui.Center},
	)
	tw, th := tl.Size()
	p.TextLayout(tl, rect.X+(rect.W-tw)/2, rect.Y+(rect.H-th)/2, t.TextMuted.Alpha(0.75))
}

func (b *Board) readOnlyChip(c *ui.Context, wb WhiteboardStyle, o BoardOptions) {
	ui.Row(c.Key("board-ro-chip")).Absolute().Top(12).Right(14).Height(28).PaddingX(10).Radius(8).
		Background(wb.IslandFill).Border(1, wb.IslandBorder).AlignItems(ui.Center).
		Children(func() {
			ui.Text(c, i18n.T("白板为只读 · 点击右上角的锁恢复编辑")).FontSize(11).TextColor(o.Theme.TextMuted).NoWrap()
		})
}

func (b *Board) floatingToolIsland(c *ui.Context, wb WhiteboardStyle, o BoardOptions) {
	t := o.Theme
	ui.Row(c.Key("board-tool-bar-wrap")).Absolute().Top(10).Left(8).Right(8).Justify(ui.Center).PassThrough().Children(func() {
		bar := ui.Row(c.Key("board-tool-bar")).Height(38).PaddingX(6).Gap(2).Radius(12).AlignItems(ui.Center).
			Background(wb.IslandFill).Border(1, wb.IslandBorder).Shadow(0, 4, 14, 0, wb.IslandShadow)
		bar.Children(func() {
			lockIcon := iconUnlocked
			if b.ToolLocked {
				lockIcon = iconLocked
			}
			lockBtn := b.islandIconBtn(c, "tool-lock", lockIcon, i18n.T("锁定工具")+" (Q)", b.ToolLocked, wb, t)
			if lockBtn.Clicked() {
				b.ToolLocked = !b.ToolLocked
			}
			b.islandDivider(c, wb)

			for _, def := range boardTools {
				active := b.Tool == def.tool
				btn := b.islandIconBtn(c, "tool-"+def.key, def.icon, i18n.T(def.label)+" ("+def.key+")", active, wb, t)
				if btn.Clicked() {
					b.Tool = def.tool
					if def.tool == ToolFreedraw {
						b.Selected = map[string]bool{}
					}
					b.shapesPopoverOpen = false
					b.RequestFocus = true
				}
			}

			extActive := b.shapesPopoverOpen || isExtendedShapeTool(b.Tool)
			extIcon := extendedShapeIcon(b.Tool)
			extBtn := b.islandIconBtn(c, "tool-more-shapes", extIcon, i18n.T("更多手绘图形")+" (S)", extActive, wb, t)
			if extBtn.Clicked() {
				b.shapesPopoverOpen = !b.shapesPopoverOpen
				b.mermaidOpen = false
			}

			for _, def := range boardSecondaryTools {
				if o.Compact && def.tool == ToolFrame {
					continue
				}
				active := b.Tool == def.tool
				btn := b.islandIconBtn(c, "tool-"+def.key, def.icon, i18n.T(def.label)+" ("+def.key+")", active, wb, t)
				if btn.Clicked() {
					b.Tool = def.tool
					b.shapesPopoverOpen = false
					b.RequestFocus = true
				}
			}

			b.islandDivider(c, wb)
			if !o.Compact {
				if b.islandIconBtn(c, "tool-mermaid", iconWorkflow, i18n.T("Mermaid 生成手绘图形"), b.mermaidOpen, wb, t).Clicked() {
					b.mermaidOpen = !b.mermaidOpen
					b.shapesPopoverOpen = false
				}
			}
			if b.islandIconBtn(c, "tool-library", iconLibrary, i18n.T("组件素材库")+" (B)", b.libraryOpen, wb, t).Clicked() {
				b.libraryOpen = !b.libraryOpen
				b.shapesPopoverOpen = false
			}
			gridBtn := b.islandIconBtn(c, "tool-grid", iconBoardGrid, i18n.T("切换点阵网格"), b.ShowGrid, wb, t)
			if gridBtn.Clicked() {
				b.ShowGrid = !b.ShowGrid
			}
			if o.Compact {
				if o.ExpandModal != nil {
					if b.islandIconBtn(c, "tool-expand", iconMaximize, i18n.T("放大画布"), false, wb, t).Clicked() {
						o.ExpandModal()
					}
				}
				if o.Done != nil {
					doneBtn := ui.ButtonBase(c.Key("board-done")).Height(26).PaddingX(10).Radius(7).
						Background(t.Accent).Center().Cursor(ui.CursorPointer)
					doneBtn.Children(func() {
						ui.Text(c, i18n.T("完成")).FontSize(12).FontWeight(600).TextColor(t.AccentText).NoWrap()
					})
					if doneBtn.Clicked() {
						b.commitTextEdit(o)
						o.Done()
					}
				}
			}
		})
	})
}

func (b *Board) islandIconBtn(c *ui.Context, id string, icon *ui.SVG, tip string, active bool, wb WhiteboardStyle, t *ui.Theme) ui.Element {
	btn := ui.ButtonBase(c.Key(id)).Size(28, 28).Radius(8).Center().Tooltip(tip).Cursor(ui.CursorPointer)
	fg := t.Text
	if active {
		btn.Background(wb.ActiveFill)
		fg = wb.ActiveInk
	} else if btn.Hovered() {
		btn.Background(t.SurfaceHover)
	}
	btn.Children(func() {
		ui.Icon(c, icon).FontSize(15).TextColor(fg)
	})
	return btn
}

func (b *Board) islandDivider(c *ui.Context, wb WhiteboardStyle) {
	ui.Box(c).Size(1, 18).Margin(0, 3).Background(wb.IslandBorder)
}

func (b *Board) compactStyleBar(c *ui.Context, wb WhiteboardStyle, o BoardOptions) {
	sel := b.selectedList()
	if len(sel) == 0 && b.editingGroupID == "" && (b.Tool == ToolSelect || b.Tool == ToolHand || b.Tool == ToolEraser || b.Tool == ToolLaser) {
		return
	}
	t := o.Theme
	props := b.activeProps()
	strokeCol, fillCol, fillStyle, rough := props.stroke, props.fill, props.fillStyle, props.rough
	sharedGID := whiteboard.SharedGroupID(sel, b.editingGroupID)

	ui.Row(c.Key("board-compact-style-wrap")).Absolute().Top(54).Left(8).Right(8).Justify(ui.Center).PassThrough().Children(func() {
		bar := ui.Row(c.Key("board-compact-style")).Height(32).PaddingX(8).Gap(4).Radius(10).AlignItems(ui.Center).
			Background(wb.IslandFill).Border(1, wb.IslandBorder).Shadow(0, 3, 10, 0, wb.IslandShadow)
		bar.Children(func() {
			if len(sel) > 0 || (b.Tool != ToolSelect && b.Tool != ToolHand && b.Tool != ToolEraser && b.Tool != ToolLaser) {
				for _, name := range []string{"ink", "red", "green", "blue", "orange", "purple"} {
					col := resolveBoardStroke(wb, name, t)
					if b.colorSwatchBtn(c, "csc-"+name, col, strokeCol == name, false, wb, t).Clicked() {
						b.setStrokeColor(name, o)
					}
				}
				b.islandDivider(c, wb)
				hasFill := fillCol != ""
				if b.pillChoice(c, "cf-tgl", i18n.T("斜线"), hasFill && fillStyle == whiteboard.FillHachure, wb, t).Clicked() {
					if hasFill && fillStyle == whiteboard.FillHachure {
						b.setFillColor("", o)
					} else {
						b.FillStyle = whiteboard.FillHachure
						b.setFillColor(strokeCol, o)
						b.applyStyleToSelection(func(el *whiteboard.Element) { el.FillStyle = whiteboard.FillHachure })
					}
				}
				if b.pillChoice(c, "cr-tgl", i18n.T("手绘"), rough > 0, wb, t).Clicked() {
					next := 1
					if rough > 0 {
						next = 0
					}
					b.Roughness = next
					b.applyStyleToSelection(func(el *whiteboard.Element) { el.Roughness = next })
					if o.OnChange != nil {
						o.OnChange()
					}
				}
			}
			if sharedGID != "" {
				if len(sel) > 0 {
					b.islandDivider(c, wb)
				}
				if b.pillChoice(c, "c-enter-grp", i18n.T("编辑内部"), false, wb, t).Clicked() {
					b.enterGroup(sharedGID, "")
				}
				if b.pillChoice(c, "c-ungrp", i18n.T("取消编组"), false, wb, t).Clicked() {
					b.ungroupSelected(o)
				}
			} else if b.editingGroupID != "" {
				if len(sel) > 0 {
					b.islandDivider(c, wb)
				}
				if b.pillChoice(c, "c-exit-grp", i18n.T("完成组编辑"), true, wb, t).Clicked() {
					b.exitGroup(true)
				}
			}
			if len(sel) > 0 {
				b.islandDivider(c, wb)
				if b.islandIconBtn(c, "cdel", iconTrash, i18n.T("删除"), false, wb, t).Clicked() {
					b.deleteSelected()
					if o.OnChange != nil {
						o.OnChange()
					}
				}
			}
		})
	})
}

func (b *Board) styleSection(c *ui.Context, t *ui.Theme, title string, fn func()) {
	ui.Column(c).Gap(4).Children(func() {
		ui.Text(c, i18n.T(title)).FontSize(11).TextColor(t.TextMuted)
		fn()
	})
}

func (b *Board) colorSwatchBtn(c *ui.Context, id string, col ui.Color, active, slash bool, wb WhiteboardStyle, t *ui.Theme) ui.Element {
	btn := ui.ButtonBase(c.Key(id)).Size(22, 22).Radius(6).Center().Cursor(ui.CursorPointer)
	if active {
		btn.Border(2, wb.Selection)
	} else {
		btn.Border(1, t.Border.Alpha(0.6))
	}
	btn.Draw(func(p *ui.Painter, r ui.Rect) {
		inner := ui.Rect{X: r.X + 3, Y: r.Y + 3, W: r.W - 6, H: r.H - 6}
		p.Fill(inner, col, 4)
		if slash {
			p.Line(inner.X+2, inner.Y+inner.H-2, inner.X+inner.W-2, inner.Y+2, 1.5, t.Danger)
		}
	})
	return btn
}

func (b *Board) pillChoice(c *ui.Context, id, label string, active bool, wb WhiteboardStyle, t *ui.Theme) ui.Element {
	btn := ui.ButtonBase(c.Key(id)).Grow(1).Height(24).PaddingX(6).Radius(6).Center().Cursor(ui.CursorPointer)
	fg := t.Text
	if active {
		btn.Background(wb.ActiveFill)
		fg = wb.ActiveInk
	} else {
		btn.Background(t.SurfaceHover.Alpha(0.65))
	}
	btn.Children(func() {
		ui.Text(c, label).FontSize(11).FontWeight(500).TextColor(fg).NoWrap()
	})
	return btn
}

func (b *Board) bottomZoomBar(c *ui.Context, wb WhiteboardStyle, o BoardOptions) {
	t := o.Theme
	ui.Row(c.Key("board-bottom-bar")).Absolute().Left(14).Bottom(12).Gap(8).AlignItems(ui.Center).Children(func() {
		zoomBox := ui.Row(c.Key("zoom-capsule")).Height(32).PaddingX(4).Gap(2).Radius(10).AlignItems(ui.Center).
			Background(wb.IslandFill).Border(1, wb.IslandBorder).Shadow(0, 3, 12, 0, wb.IslandShadow)
		zoomBox.Children(func() {
			if b.islandIconBtn(c, "zoom-out", iconRule, i18n.T("缩小"), false, wb, t).Clicked() {
				b.zoomAtCenter(1 / 1.2)
			}
			pct := fmt.Sprintf("%d%%", int(math.Round(float64(b.Zoom*100))))
			resetBtn := ui.ButtonBase(c.Key("zoom-reset")).Height(26).PaddingX(6).Radius(6).Center().
				Tooltip(i18n.T("重置缩放")).Cursor(ui.CursorPointer)
			if resetBtn.Hovered() {
				resetBtn.Background(t.SurfaceHover)
			}
			resetBtn.Children(func() {
				ui.Text(c, pct).FontSize(11).FontWeight(500).TextColor(t.Text).NoWrap()
			})
			if resetBtn.Clicked() {
				b.zoomToAtCenter(1)
			}
			if b.islandIconBtn(c, "zoom-in", iconPlus, i18n.T("放大"), false, wb, t).Clicked() {
				b.zoomAtCenter(1.2)
			}
			if b.islandIconBtn(c, "zoom-fit", iconFit, i18n.T("适应画布"), false, wb, t).Clicked() {
				b.userViewport = false
				b.FitToContent(b.lastRect)
				b.autoFitW, b.autoFitH = b.lastRect.W, b.lastRect.H
			}
		})

		if !b.ReadOnly {
			histBox := ui.Row(c.Key("hist-capsule")).Height(32).PaddingX(4).Gap(2).Radius(10).AlignItems(ui.Center).
				Background(wb.IslandFill).Border(1, wb.IslandBorder).Shadow(0, 3, 12, 0, wb.IslandShadow)
			histBox.Children(func() {
				uBtn := b.islandIconBtn(c, "board-undo", iconUndo, i18n.T("撤销")+" (⌘Z)", false, wb, t)
				if !b.History.CanUndo() {
					uBtn.Opacity(0.35).Disabled(true)
				}
				if uBtn.Clicked() && b.Undo() && o.OnChange != nil {
					o.OnChange()
				}
				rBtn := b.islandIconBtn(c, "board-redo", iconRedo, i18n.T("重做")+" (⇧⌘Z)", false, wb, t)
				if !b.History.CanRedo() {
					rBtn.Opacity(0.35).Disabled(true)
				}
				if rBtn.Clicked() && b.Redo() && o.OnChange != nil {
					o.OnChange()
				}
			})
		}
	})
}

func (b *Board) linkPickerPopover(c *ui.Context, wb WhiteboardStyle, o BoardOptions) {
	if !b.linkPopoverOpen || o.WikiSuggest == nil {
		return
	}
	sel := b.selectedList()
	if len(sel) != 1 {
		b.linkPopoverOpen = false
		return
	}
	t := o.Theme
	el := sel[0]
	box := ui.Column(c.Key("board-link-picker")).Absolute().Left(228).Top(160).Width(240).
		Padding(10).Gap(6).Radius(10).
		Background(wb.IslandFill).Border(1, wb.IslandBorder).Shadow(0, 8, 24, 0, wb.IslandShadow)
	box.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.T("关联文档")).FontSize(12).FontWeight(600).TextColor(t.Text).Grow(1)
			closeBtn := ui.ButtonBase(c.Key("lp-close")).Size(20, 20).Radius(4).Center().Cursor(ui.CursorPointer)
			closeBtn.Children(func() { ui.Icon(c, iconClose).FontSize(12).TextColor(t.TextMuted) })
			if closeBtn.Clicked() {
				b.linkPopoverOpen = false
			}
		})
		in := ui.SearchField(c.Key("lp-query"), &b.linkSearchQuery).Height(28).Placeholder(i18n.T("链接到文档")).AutoFocus()
		_ = in
		if c.Shortcut(0, ui.KeyEscape) {
			b.linkPopoverOpen = false
			return
		}
		matches := o.WikiSuggest(b.linkSearchQuery)
		for i, name := range matches {
			if i >= 8 {
				break
			}
			row := ui.ButtonBase(c.Key("lp-item-" + name)).Height(28).PaddingX(8).Radius(6).
				Gap(6).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			if row.Hovered() {
				row.Background(t.SurfaceHover)
			}
			row.Children(func() {
				ui.Icon(c, iconLink).FontSize(13).TextColor(wb.ActiveInk)
				ui.Text(c, name).FontSize(13).TextColor(t.Text).SingleLine().Grow(1)
			})
			if row.Clicked() {
				b.History.Record(b.Scene)
				el.Link = "[[" + name + "]]"
				if strings.TrimSpace(el.Text) == "" {
					el.Text = "[[" + name + "]]"
				}
				el.Version++
				b.bump(true)
				b.linkPopoverOpen = false
				if o.OnChange != nil {
					o.OnChange()
				}
			}
		}
	})
}

func (b *Board) inlineTextOverlay(c *ui.Context, wb WhiteboardStyle, o BoardOptions) {
	if b.editingID == "" || b.ReadOnly {
		return
	}
	el := b.Scene.ElementByID(b.editingID)
	if el == nil {
		b.editingID = ""
		return
	}
	t := o.Theme
	eb := whiteboard.ElementBounds(el)
	if eb.W < 140 {
		eb.W = 180
	}
	if eb.H < 44 {
		eb.H = 48
	}
	sx := b.PanX + eb.X*b.Zoom
	sy := b.PanY + eb.Y*b.Zoom
	sw := max(120, eb.W*b.Zoom)
	sh := max(44, eb.H*b.Zoom)

	bg := wb.Canvas.Alpha(0.95)
	if el.Kind == whiteboard.KindNote {
		bg = wb.NoteFill
	}
	col := ui.Column(c.Key("board-text-edit:"+el.ID)).Absolute().Left(sx).Top(sy).Width(sw).MinHeight(sh).
		Radius(8).Background(bg).Border(1.5, wb.Selection).Shadow(0, 4, 14, 0, wb.IslandShadow).Padding(4)
	col.Children(func() {
		ta := ui.TextArea(c.Key("ta:"+el.ID), &b.editText).Width(sw - 8).MinHeight(sh - 8).
			FontSize(max(13, el.FontSize*min(1.4, b.Zoom))).Font(boardFontFamily(el.FontFamily))
		if b.editJustOpened {
			ta.Focus()
			b.editJustOpened = false
		}
		el.Text = b.editText
		el.Version++
		b.bump(false)

		// Detect [[ for wiki-link auto-suggest while typing in whiteboard text.
		if o.WikiSuggest != nil {
			if idx := strings.LastIndex(b.editText, "[["); idx >= 0 && !strings.Contains(b.editText[idx:], "]]") {
				q := b.editText[idx+2:]
				if !strings.ContainsRune(q, '\n') {
					matches := o.WikiSuggest(q)
					if len(matches) > 0 {
						ui.Column(c.Key("board-wiki-suggest")).Width(min(220, max(160, sw))).Padding(4).Gap(2).
							Background(wb.IslandFill).Border(1, wb.IslandBorder).Radius(8).Children(func() {
							for i, m := range matches {
								if i >= 6 {
									break
								}
								btn := ui.ButtonBase(c.Key("bw:" + m)).Height(24).PaddingX(6).Radius(5).
									AlignItems(ui.Center).Cursor(ui.CursorPointer)
								if btn.Hovered() {
									btn.Background(t.SurfaceHover)
								}
								btn.Children(func() {
									ui.Text(c, m).FontSize(12).TextColor(t.Text).SingleLine()
								})
								if btn.Clicked() {
									b.editText = b.editText[:idx] + "[[" + m + "]]"
									el.Text = b.editText
									if el.Link == "" {
										el.Link = "[[" + m + "]]"
									}
									el.Version++
									b.commitTextEdit(o)
									b.RequestFocus = true
								}
							}
						})
					}
				}
			}
		}

		focused := col.FocusWithin()
		if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyEnter) || (b.editWasFocused && !focused) {
			b.commitTextEdit(o)
			b.RequestFocus = true
			return
		}
		b.editWasFocused = focused
	})
}

func (b *Board) startTextEdit(el *whiteboard.Element) {
	if el == nil || b.ReadOnly {
		return
	}
	b.editBefore = b.Scene.Clone()
	b.editingID = el.ID
	b.editText = el.Text
	b.editJustOpened = true
	b.editWasFocused = false
	b.Selected = map[string]bool{el.ID: true}
}

func (b *Board) commitTextEdit(o BoardOptions) {
	if b.editingID == "" {
		return
	}
	el := b.Scene.ElementByID(b.editingID)
	if el != nil {
		el.Text = b.editText
		if el.Kind == whiteboard.KindText {
			w, h := whiteboard.EstimateTextBounds(el.Text, el.FontSize)
			el.W, el.H = w, h
		}
		if el.Kind == whiteboard.KindText && strings.TrimSpace(el.Text) == "" {
			b.Scene.Elements = slices.DeleteFunc(b.Scene.Elements, func(x *whiteboard.Element) bool { return x.ID == el.ID })
			delete(b.Selected, el.ID)
		}
		el.Version++
		if b.editBefore != nil && b.editBefore.Digest() != b.Scene.Digest() {
			b.History.Record(b.editBefore)
			b.bump(true)
			if o.OnChange != nil {
				o.OnChange()
			}
		}
	}
	b.editingID = ""
	b.editBefore = nil
	b.editWasFocused = false
}

func (b *Board) updateHover(sx, sy float32) {
	if b.hoverValid && (absf(sx-b.hoverScene[0]) > 2 || absf(sy-b.hoverScene[1]) > 2) {
		b.hoverMovedSinceCopy = true
	}
	b.hoverScene = [2]float32{sx, sy}
	b.hoverValid = true
}

func cursorForHandle(h whiteboard.HandleKind) ui.Cursor {
	switch h {
	case whiteboard.HandleNW, whiteboard.HandleSE:
		return ui.CursorResizeNWSE
	case whiteboard.HandleNE, whiteboard.HandleSW:
		return ui.CursorResizeNESW
	case whiteboard.HandleN, whiteboard.HandleS:
		return ui.CursorResizeNS
	case whiteboard.HandleE, whiteboard.HandleW:
		return ui.CursorResizeEW
	case whiteboard.HandleStart, whiteboard.HandleMid, whiteboard.HandleEnd:
		return ui.CursorMove
	}
	return ui.CursorDefault
}

func (b *Board) cursorFor(canvas ui.Element) ui.Cursor {
	lx, ly, inside := canvas.PointerPosition()
	var sx, sy float32
	if inside {
		sx, sy = b.localToScene(lx, ly)
		b.updateHover(sx, sy)
	} else {
		b.hoverValid = false
	}
	if b.panning || b.spaceDown || b.Tool == ToolHand {
		if b.dragMode == dragPan {
			return ui.CursorGrabbing
		}
		return ui.CursorGrab
	}
	if b.dragMode == dragResize {
		if cur := cursorForHandle(b.dragHandle); cur != ui.CursorDefault {
			return cur
		}
	}
	if b.Tool != ToolSelect && b.Tool != ToolEraser {
		return ui.CursorCrosshair
	}
	if !inside {
		return ui.CursorDefault
	}
	rect := b.lastRect
	if b.groupPill.groupID != "" {
		pr := b.groupPill.rect
		if lx >= pr.X-rect.X && lx <= pr.X-rect.X+pr.W &&
			ly >= pr.Y-rect.Y && ly <= pr.Y-rect.Y+pr.H {
			return ui.CursorPointer
		}
	}
	for _, pill := range b.linkPills {
		if lx >= pill.rect.X-rect.X && lx <= pill.rect.X-rect.X+pill.rect.W &&
			ly >= pill.rect.Y-rect.Y && ly <= pill.rect.Y-rect.Y+pill.rect.H {
			return ui.CursorPointer
		}
	}
	sel := b.selectedList()
	if len(sel) == 1 && !b.ReadOnly {
		if cur := cursorForHandle(whiteboard.HitHandle(sel[0], sx, sy, b.Zoom)); cur != ui.CursorDefault {
			return cur
		}
		if _, ok := whiteboard.HitConnectPort(sel[0], sx, sy, b.Zoom); ok {
			return ui.CursorCrosshair
		}
	} else if len(sel) >= 2 && !b.ReadOnly && hasUnlockedElements(sel) {
		if cur := cursorForHandle(whiteboard.HitBoxHandle(whiteboard.SelectionBounds(sel), sx, sy, b.Zoom)); cur != ui.CursorDefault {
			return cur
		}
	}
	if b.Tool == ToolSelect && whiteboard.HitTest(b.Scene, sx, sy, 6/max(0.25, b.Zoom)) != nil {
		return ui.CursorMove
	}
	return ui.CursorDefault
}

func (b *Board) handleInput(ev ui.InputEvent, rect ui.Rect, readClip func() string, writeClip func(string), o BoardOptions) bool {
	switch ev.Kind {
	case ui.InputKeyDown:
		if ev.Key == ui.KeySpace && b.editingID == "" {
			b.spaceDown = true
			return true
		}
		return b.handleKeyDown(ev, readClip, writeClip, o)
	case ui.InputKeyUp:
		if ev.Key == ui.KeySpace {
			b.spaceDown = false
			return true
		}
	case ui.InputScroll:
		if ev.Mods&(ui.Cmd|ui.Ctrl) != 0 {
			factor := float32(math.Pow(1.008, float64(-ev.DY)))
			b.zoomAtPoint(ev.X, ev.Y, factor)
			return true
		}
		if o.Compact && !b.spaceDown && b.Tool != ToolHand && ev.Mods&ui.Alt == 0 {
			return false // let the parent Markdown scroll view scroll naturally
		}
		b.PanX -= ev.DX
		b.PanY -= ev.DY
		b.userViewport = true
		return true
	case ui.InputZoom:
		b.zoomAtPoint(ev.X, ev.Y, ev.Scale)
		return true
	case ui.InputCommand:
		return b.handleCommand(ev.Text, readClip, writeClip, o)
	case ui.InputPointerDown:
		return b.handlePointerDown(ev, rect, o)
	case ui.InputPointerMove:
		return b.handlePointerMove(ev, o)
	case ui.InputPointerUp:
		return b.handlePointerUp(ev, o)
	}
	return false
}

func (b *Board) handleKeyDown(ev ui.InputEvent, readClip func() string, writeClip func(string), o BoardOptions) bool {
	if b.editingID != "" {
		return false
	}
	cmd := ev.Mods&ui.Cmd != 0
	shift := ev.Mods&ui.Shift != 0
	if cmd {
		switch ev.Key {
		case ui.KeyZ:
			if b.ReadOnly {
				return false
			}
			if shift {
				if b.Redo() && o.OnChange != nil {
					o.OnChange()
				}
			} else {
				if b.Undo() && o.OnChange != nil {
					o.OnChange()
				}
			}
			return true
		case ui.KeyC:
			if !shift {
				return b.copySelected(writeClip)
			}
		case ui.KeyX:
			if !shift {
				return b.cutSelected(writeClip, o)
			}
		case ui.KeyV:
			if !shift {
				return b.pasteFromClipboard(readClip, o)
			}
		case ui.KeyA:
			b.Selected = map[string]bool{}
			for _, el := range b.Scene.Elements {
				if el != nil {
					b.Selected[el.ID] = true
				}
			}
			return true
		case ui.KeyD:
			if b.duplicateSelected() && o.OnChange != nil {
				o.OnChange()
			}
			return true
		case ui.KeyG:
			if shift {
				b.ungroupSelected(o)
			} else {
				b.groupSelected(o)
			}
			return true
		case ui.KeyEqual:
			b.zoomAtCenter(1.2)
			return true
		case ui.KeyMinus:
			b.zoomAtCenter(1 / 1.2)
			return true
		case ui.Key0:
			b.zoomToAtCenter(1)
			return true
		}
		return false
	}
	if ev.Mods != 0 {
		return false
	}
	switch ev.Key {
	case ui.KeyEscape:
		if b.shapesPopoverOpen || b.mermaidOpen || b.libraryOpen || b.linkPopoverOpen {
			b.shapesPopoverOpen = false
			b.mermaidOpen = false
			b.libraryOpen = false
			b.linkPopoverOpen = false
			return true
		}
		if b.editingGroupID != "" {
			b.exitGroup(true)
			return true
		}
		if len(b.Selected) > 0 {
			b.Selected = map[string]bool{}
			return true
		}
		if b.Tool != ToolSelect {
			b.Tool = ToolSelect
			return true
		}
		return false
	case ui.KeyBackspace, ui.KeyDelete:
		if b.deleteSelected() && o.OnChange != nil {
			o.OnChange()
		}
		return true
	case ui.KeyEnter:
		if !b.ReadOnly {
			sel := b.selectedList()
			if len(sel) == 1 {
				b.startTextEdit(sel[0])
				return true
			}
			if sharedGID := whiteboard.SharedGroupID(sel, b.editingGroupID); sharedGID != "" {
				b.enterGroup(sharedGID, "")
				return true
			}
		}
	case ui.KeyQ:
		b.ToolLocked = !b.ToolLocked
		return true
	case ui.KeyH:
		b.Tool = ToolHand
		b.shapesPopoverOpen = false
		return true
	case ui.KeyV, ui.Key1:
		b.Tool = ToolSelect
		b.shapesPopoverOpen = false
		return true
	case ui.KeyR, ui.Key2:
		b.Tool = ToolRect
		b.shapesPopoverOpen = false
		return true
	case ui.KeyD, ui.Key3:
		b.Tool = ToolDiamond
		b.shapesPopoverOpen = false
		return true
	case ui.KeyO, ui.Key4:
		b.Tool = ToolEllipse
		b.shapesPopoverOpen = false
		return true
	case ui.KeyS:
		if !b.ReadOnly {
			b.shapesPopoverOpen = !b.shapesPopoverOpen
			b.mermaidOpen = false
			return true
		}
	case ui.KeyA, ui.Key5:
		b.Tool = ToolArrow
		b.shapesPopoverOpen = false
		return true
	case ui.KeyL, ui.Key6:
		b.Tool = ToolLine
		b.shapesPopoverOpen = false
		return true
	case ui.KeyP, ui.Key7:
		b.Tool = ToolFreedraw
		b.Selected = map[string]bool{}
		b.shapesPopoverOpen = false
		return true
	case ui.KeyT, ui.Key8:
		b.Tool = ToolText
		b.shapesPopoverOpen = false
		return true
	case ui.KeyN, ui.Key9:
		b.Tool = ToolNote
		b.shapesPopoverOpen = false
		return true
	case ui.KeyF:
		b.Tool = ToolFrame
		b.shapesPopoverOpen = false
		return true
	case ui.KeyK:
		b.Tool = ToolLaser
		b.shapesPopoverOpen = false
		return true
	case ui.KeyB:
		if !b.ReadOnly {
			b.libraryOpen = !b.libraryOpen
			b.shapesPopoverOpen = false
			return true
		}
	case ui.KeyE, ui.Key0:
		b.Tool = ToolEraser
		b.shapesPopoverOpen = false
		return true
	case ui.KeyLeft, ui.KeyRight, ui.KeyUp, ui.KeyDown:
		if !b.ReadOnly && len(b.Selected) > 0 {
			var dx, dy float32
			switch ev.Key {
			case ui.KeyLeft:
				dx = -2
			case ui.KeyRight:
				dx = 2
			case ui.KeyUp:
				dy = -2
			case ui.KeyDown:
				dy = 2
			}
			b.History.Record(b.Scene)
			whiteboard.MoveElements(b.Scene, b.selectedIDs(), dx, dy)
			b.bump(true)
			if o.OnChange != nil {
				o.OnChange()
			}
			return true
		}
	}
	return false
}

func (b *Board) handleCommand(cmd string, readClip func() string, writeClip func(string), o BoardOptions) bool {
	if b.editingID != "" {
		return false
	}
	switch cmd {
	case "copy":
		return b.copySelected(writeClip)
	case "cut":
		return b.cutSelected(writeClip, o)
	case "paste", "pasteAndMatchStyle":
		return b.pasteFromClipboard(readClip, o)
	case "undo":
		if b.Undo() && o.OnChange != nil {
			o.OnChange()
		}
		return true
	case "redo":
		if b.Redo() && o.OnChange != nil {
			o.OnChange()
		}
		return true
	case "selectAll":
		b.Selected = map[string]bool{}
		for _, el := range b.Scene.Elements {
			if el != nil {
				b.Selected[el.ID] = true
			}
		}
		return true
	case "delete":
		if b.deleteSelected() && o.OnChange != nil {
			o.OnChange()
		}
		return true
	}
	return false
}

func (b *Board) newElementFromDefaults(kind whiteboard.ElementKind, x, y float32) *whiteboard.Element {
	el := whiteboard.NewElement(kind, x, y, 2, 2)
	el.StrokeColor = b.StrokeColor
	el.FillColor = b.FillColor
	el.FillStyle = b.FillStyle
	el.StrokeWidth = b.StrokeWidth
	el.StrokeStyle = b.StrokeStyle
	el.Roughness = b.Roughness
	el.Roundness = b.Roundness
	if b.Opacity > 0 {
		el.Opacity = b.Opacity
	}
	if b.FontSize > 0 {
		el.FontSize = b.FontSize
	}
	el.FontFamily = b.FontFamily
	if kind == whiteboard.KindNote && b.FillColor == "" {
		el.FillColor = "orange"
	}
	if kind == whiteboard.KindActor {
		el.FillStyle = whiteboard.FillNone
	}
	if kind == whiteboard.KindArrow || kind == whiteboard.KindLine {
		el.Points = [][2]float32{{0, 0}, {0, 0}}
		el.Smooth = b.ArrowRouting == "curved"
		el.Elbowed = b.ArrowRouting == "elbow"
		if kind == whiteboard.KindArrow {
			el.StartArrowhead = b.StartArrowhead
			el.EndArrowhead = b.EndArrowhead
			if el.EndArrowhead == whiteboard.ArrowNone && el.StartArrowhead == whiteboard.ArrowNone {
				el.EndArrowhead = whiteboard.ArrowOpen
			}
		} else {
			el.StartArrowhead = whiteboard.ArrowNone
			el.EndArrowhead = whiteboard.ArrowNone
		}
	}
	return el
}

func (b *Board) startBoundArrowCreate(el *whiteboard.Element, sx, sy, startX, startY float32, startBindID string) {
	if startBindID != "" {
		el.StartBinding = &whiteboard.Binding{ElementID: startBindID, Gap: 6}
	} else if el.Kind == whiteboard.KindArrow || el.Kind == whiteboard.KindLine {
		if target := whiteboard.FindBindTarget(b.Scene, el.ID, sx, sy, 18/max(0.25, b.Zoom)); target != nil {
			el.StartBinding = &whiteboard.Binding{ElementID: target.ID, Gap: 6}
		}
	}
	b.Scene.Elements = append(b.Scene.Elements, el)
	b.creatingID = el.ID
	if el.Kind == whiteboard.KindFreedraw {
		b.Selected = map[string]bool{}
	} else {
		b.Selected = map[string]bool{el.ID: true}
	}
	b.dragMode = dragCreate
	b.dragStartScene = [2]float32{startX, startY}
	b.dragLastScene = [2]float32{sx, sy}
	b.dragMoved = false
}

func (b *Board) handlePointerDown(ev ui.InputEvent, rect ui.Rect, o BoardOptions) bool {
	b.commitTextEdit(o)
	b.shapesPopoverOpen = false
	b.RequestFocus = true

	// Check floating group action pill ("✎ 编辑内部" / "组内编辑 · 完成 (Esc)").
	if b.groupPill.groupID != "" {
		lx := rect.X + ev.X
		ly := rect.Y + ev.Y
		pr := b.groupPill.rect
		if lx >= pr.X && lx <= pr.X+pr.W && ly >= pr.Y && ly <= pr.Y+pr.H {
			if b.groupPill.exiting {
				b.exitGroup(true)
			} else {
				b.enterGroup(b.groupPill.groupID, "")
			}
			return true
		}
	}

	// Check clickable ↗ linked-document badges first.
	for _, pill := range b.linkPills {
		lx := rect.X + ev.X
		ly := rect.Y + ev.Y
		if lx >= pill.rect.X && lx <= pill.rect.X+pill.rect.W && ly >= pill.rect.Y && ly <= pill.rect.Y+pill.rect.H {
			if o.OpenWiki != nil {
				o.OpenWiki(pill.target, ev.Mods&ui.Alt != 0)
			}
			return true
		}
	}

	sx, sy := b.localToScene(ev.X, ev.Y)
	if ev.Button == 2 || (ev.Button == 0 && (b.spaceDown || b.Tool == ToolHand)) {
		b.dragMode = dragPan
		b.panning = true
		b.dragStartScreen = [2]float32{ev.X, ev.Y}
		return true
	}
	if ev.Button != 0 {
		return false
	}

	tol := 6 / max(0.25, b.Zoom)
	if ev.Mods&ui.Cmd != 0 {
		if hit := whiteboard.HitTest(b.Scene, sx, sy, tol); hit != nil {
			if target, _, ok := hit.LinkTarget(); ok && o.OpenWiki != nil {
				o.OpenWiki(target, ev.Mods&ui.Alt != 0)
				return true
			}
			if !b.ReadOnly && len(hit.GroupIDs) > 0 {
				b.editingGroupID = hit.GroupIDs[0]
				b.Selected = map[string]bool{hit.ID: true}
				b.contextElementID = hit.ID
				b.userViewport = true
				b.History.Begin(b.Scene)
				b.dragMode = dragMove
				b.dragStartScene = [2]float32{sx, sy}
				b.dragLastScene = [2]float32{sx, sy}
				b.dragMoved = false
				return true
			}
		}
	}

	if b.Tool == ToolLaser {
		b.dragMode = dragLaser
		b.laserTrail = append(b.laserTrail, laserSample{x: sx, y: sy, at: ev.Time})
		return true
	}

	if b.ReadOnly {
		b.dragMode = dragPan
		b.panning = true
		b.dragStartScreen = [2]float32{ev.X, ev.Y}
		return true
	}

	if b.Tool == ToolEraser {
		b.userViewport = true
		b.History.Begin(b.Scene)
		b.dragMode = dragErase
		b.eraseAt(sx, sy, tol)
		return true
	}

	if b.Tool == ToolText {
		b.userViewport = true
		b.History.Record(b.Scene)
		el := whiteboard.NewElement(whiteboard.KindText, sx, sy-12, 160, 32)
		el.StrokeColor = b.StrokeColor
		el.FontFamily = b.FontFamily
		if b.FontSize > 0 {
			el.FontSize = b.FontSize
		}
		if b.Opacity > 0 {
			el.Opacity = b.Opacity
		}
		if b.editingGroupID != "" {
			el.GroupIDs = whiteboard.GroupChainFor(b.Scene, b.editingGroupID)
		}
		b.Scene.Elements = append(b.Scene.Elements, el)
		b.bump(true)
		b.startTextEdit(el)
		if !b.ToolLocked {
			b.Tool = ToolSelect
		}
		return true
	}

	if b.Tool != ToolSelect {
		b.userViewport = true
		b.History.Begin(b.Scene)
		kind := toolToKind(b.Tool)
		el := b.newElementFromDefaults(kind, sx, sy)
		if b.editingGroupID != "" {
			el.GroupIDs = whiteboard.GroupChainFor(b.Scene, b.editingGroupID)
		}
		b.startBoundArrowCreate(el, sx, sy, sx, sy, "")
		return true
	}

	// Selection tool.
	if ev.Clicks >= 2 {
		b.userViewport = true
		if hit := whiteboard.HitTest(b.Scene, sx, sy, tol); hit != nil {
			if b.editingGroupID != "" && !whiteboard.ElementInGroup(hit, b.editingGroupID) {
				b.editingGroupID = ""
			}
			if nextGID := whiteboard.NextGroupToEnter(hit, b.editingGroupID); nextGID != "" {
				b.enterGroup(nextGID, hit.ID)
				return true
			}
			b.startTextEdit(hit)
			return true
		}
		if b.editingGroupID != "" {
			igb := whiteboard.GroupBounds(b.Scene, b.editingGroupID)
			expanded := whiteboard.Box{X: igb.X - 12, Y: igb.Y - 12, W: igb.W + 24, H: igb.H + 24}
			if igb.Empty() || !expanded.Contains(sx, sy) {
				b.editingGroupID = ""
			}
		}
		b.History.Record(b.Scene)
		el := whiteboard.NewElement(whiteboard.KindText, sx, sy-12, 160, 32)
		el.StrokeColor = b.StrokeColor
		el.FontFamily = b.FontFamily
		if b.editingGroupID != "" {
			el.GroupIDs = whiteboard.GroupChainFor(b.Scene, b.editingGroupID)
		}
		b.Scene.Elements = append(b.Scene.Elements, el)
		b.bump(true)
		b.startTextEdit(el)
		return true
	}

	sel := b.selectedList()
	if len(sel) == 1 {
		if handle := whiteboard.HitHandle(sel[0], sx, sy, b.Zoom); handle != whiteboard.HandleNone {
			b.userViewport = true
			b.History.Begin(b.Scene)
			b.dragMode = dragResize
			b.dragHandle = handle
			b.resizeTargetID = sel[0].ID
			b.resizeOrig = sel[0].Clone()
			b.resizeOrigGroup = nil
			b.resizeOrigBounds = whiteboard.Box{}
			b.dragStartScene = [2]float32{sx, sy}
			b.dragMoved = false
			return true
		}
		if portPt, ok := whiteboard.HitConnectPort(sel[0], sx, sy, b.Zoom); ok {
			b.userViewport = true
			b.History.Begin(b.Scene)
			el := b.newElementFromDefaults(whiteboard.KindArrow, portPt[0], portPt[1])
			if b.editingGroupID != "" && whiteboard.ElementInGroup(sel[0], b.editingGroupID) {
				el.GroupIDs = whiteboard.GroupChainFor(b.Scene, b.editingGroupID)
			}
			b.startBoundArrowCreate(el, sx, sy, portPt[0], portPt[1], sel[0].ID)
			return true
		}
	} else if len(sel) >= 2 && hasUnlockedElements(sel) {
		gb := whiteboard.SelectionBounds(sel)
		if handle := whiteboard.HitBoxHandle(gb, sx, sy, b.Zoom); handle != whiteboard.HandleNone {
			items, _ := b.selectedWithFrameChildren()
			origMap := make(map[string]*whiteboard.Element, len(items))
			for _, item := range items {
				if item != nil {
					origMap[item.ID] = item.Clone()
				}
			}
			b.userViewport = true
			b.History.Begin(b.Scene)
			b.dragMode = dragResize
			b.dragHandle = handle
			b.resizeTargetID = ""
			b.resizeOrig = nil
			b.resizeOrigGroup = origMap
			b.resizeOrigBounds = gb
			b.dragStartScene = [2]float32{sx, sy}
			b.dragMoved = false
			return true
		}
	}

	if hit := whiteboard.HitTest(b.Scene, sx, sy, tol); hit != nil {
		b.contextElementID = hit.ID
		if b.editingGroupID != "" && !whiteboard.ElementInGroup(hit, b.editingGroupID) {
			b.editingGroupID = ""
		}
		if ev.Mods&ui.Shift != 0 {
			if b.Selected[hit.ID] {
				for id := range whiteboard.ExpandGroupSelectionWithin(b.Scene, map[string]bool{hit.ID: true}, b.editingGroupID) {
					delete(b.Selected, id)
				}
			} else {
				for id := range whiteboard.ExpandGroupSelectionWithin(b.Scene, map[string]bool{hit.ID: true}, b.editingGroupID) {
					b.Selected[id] = true
				}
			}
			return true
		}
		if !b.Selected[hit.ID] {
			b.Selected = whiteboard.ExpandGroupSelectionWithin(b.Scene, map[string]bool{hit.ID: true}, b.editingGroupID)
		}
		b.userViewport = true
		b.History.Begin(b.Scene)
		if ev.Mods&ui.Alt != 0 {
			// Alt-drag duplicates the selected elements immediately before dragging.
			b.duplicateSelectionWithoutHistory()
		}
		b.dragMode = dragMove
		b.dragStartScene = [2]float32{sx, sy}
		b.dragLastScene = [2]float32{sx, sy}
		b.dragMoved = false
		return true
	}

	if b.editingGroupID != "" {
		igb := whiteboard.GroupBounds(b.Scene, b.editingGroupID)
		expanded := whiteboard.Box{X: igb.X - 12, Y: igb.Y - 12, W: igb.W + 24, H: igb.H + 24}
		if igb.Empty() || !expanded.Contains(sx, sy) {
			b.editingGroupID = ""
		}
	}
	b.contextElementID = ""
	b.Selected = map[string]bool{}
	b.dragMode = dragMarquee
	b.dragStartScene = [2]float32{sx, sy}
	b.marquee = &whiteboard.Box{X: sx, Y: sy, W: 0, H: 0}
	return true
}

func (b *Board) duplicateSelectionWithoutHistory() {
	items, _ := b.selectedWithFrameChildren()
	nextSel := map[string]bool{}
	idMap := map[string]string{}
	groupMap := map[string]string{}
	var copies []*whiteboard.Element
	for _, el := range items {
		cp := el.Clone()
		cp.ID = whiteboard.NewID()
		cp.Seed = whiteboard.NewSeed()
		cp.Version++
		if len(cp.GroupIDs) > 0 {
			remapped := make([]string, 0, len(cp.GroupIDs))
			for _, gid := range cp.GroupIDs {
				if gid == "" {
					continue
				}
				ng, ok := groupMap[gid]
				if !ok {
					ng = whiteboard.NewID()
					groupMap[gid] = ng
				}
				remapped = append(remapped, ng)
			}
			cp.GroupIDs = remapped
		}
		idMap[el.ID] = cp.ID
		copies = append(copies, cp)
		if b.Selected[el.ID] {
			nextSel[cp.ID] = true
		}
	}
	for _, cp := range copies {
		if cp.StartBinding != nil {
			if mapped, ok := idMap[cp.StartBinding.ElementID]; ok {
				cp.StartBinding.ElementID = mapped
			}
		}
		if cp.EndBinding != nil {
			if mapped, ok := idMap[cp.EndBinding.ElementID]; ok {
				cp.EndBinding.ElementID = mapped
			}
		}
		if cp.FrameID != "" {
			if mapped, ok := idMap[cp.FrameID]; ok {
				cp.FrameID = mapped
			}
		}
	}
	b.Scene.Elements = append(b.Scene.Elements, copies...)
	b.Selected = nextSel
}

func (b *Board) handlePointerMove(ev ui.InputEvent, o BoardOptions) bool {
	sx, sy := b.localToScene(ev.X, ev.Y)
	b.updateHover(sx, sy)
	if b.dragMode == dragNone {
		return false
	}
	switch b.dragMode {
	case dragPan:
		b.PanX += ev.X - b.dragStartScreen[0]
		b.PanY += ev.Y - b.dragStartScreen[1]
		b.dragStartScreen = [2]float32{ev.X, ev.Y}
		b.userViewport = true
		return true
	case dragErase:
		b.eraseAt(sx, sy, 8/max(0.25, b.Zoom))
		return true
	case dragLaser:
		b.laserTrail = append(b.laserTrail, laserSample{x: sx, y: sy, at: ev.Time})
		if len(b.laserTrail) > 512 {
			b.laserTrail = b.laserTrail[len(b.laserTrail)-512:]
		}
		return true
	case dragMarquee:
		x0 := min(b.dragStartScene[0], sx)
		y0 := min(b.dragStartScene[1], sy)
		x1 := max(b.dragStartScene[0], sx)
		y1 := max(b.dragStartScene[1], sy)
		box := whiteboard.Box{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
		b.marquee = &box
		b.Selected = map[string]bool{}
		for _, id := range whiteboard.ElementsInRect(b.Scene, box) {
			if b.editingGroupID == "" || whiteboard.ElementInGroup(b.Scene.ElementByID(id), b.editingGroupID) {
				b.Selected[id] = true
			}
		}
		b.Selected = whiteboard.ExpandGroupSelectionWithin(b.Scene, b.Selected, b.editingGroupID)
		return true
	case dragMove:
		dx := sx - b.dragLastScene[0]
		dy := sy - b.dragLastScene[1]
		if !b.dragMoved && (absf(sx-b.dragStartScene[0]) > 2 || absf(sy-b.dragStartScene[1]) > 2) {
			b.dragMoved = true
		}
		whiteboard.MoveElements(b.Scene, b.selectedIDs(), dx, dy)
		// Compute snap guides on the bounding box of selected elements.
		if sel := b.selectedList(); len(sel) > 0 && ev.Mods&ui.Cmd == 0 {
			box := selectionBounds(sel)
			guides, snapDX, snapDY := whiteboard.ComputeSnapGuides(b.Scene, b.Selected, box, 5/max(0.25, b.Zoom))
			b.guides = guides
			if snapDX != 0 || snapDY != 0 {
				whiteboard.MoveElements(b.Scene, b.selectedIDs(), snapDX, snapDY)
				sx += snapDX
				sy += snapDY
			}
		}
		b.dragLastScene = [2]float32{sx, sy}
		b.bump(false)
		return true
	case dragResize:
		dx := sx - b.dragStartScene[0]
		dy := sy - b.dragStartScene[1]
		b.dragMoved = true
		if len(b.resizeOrigGroup) > 0 {
			whiteboard.ResizeElements(b.Scene, b.resizeOrigBounds, b.resizeOrigGroup, b.dragHandle, dx, dy, ev.Mods&ui.Shift != 0)
			b.bump(false)
			return true
		}
		el := b.Scene.ElementByID(b.resizeTargetID)
		if el == nil || b.resizeOrig == nil {
			return true
		}
		whiteboard.ResizeElement(el, b.resizeOrig, b.dragHandle, dx, dy, ev.Mods&ui.Shift != 0)
		b.bindPreviewID = ""
		if (el.Kind == whiteboard.KindArrow || el.Kind == whiteboard.KindLine) && (b.dragHandle == whiteboard.HandleStart || b.dragHandle == whiteboard.HandleEnd) {
			var otherID string
			if b.dragHandle == whiteboard.HandleStart && el.EndBinding != nil {
				otherID = el.EndBinding.ElementID
			} else if b.dragHandle == whiteboard.HandleEnd && el.StartBinding != nil {
				otherID = el.StartBinding.ElementID
			}
			target := whiteboard.FindBindTargetExcluding(b.Scene, sx, sy, 20/max(0.25, b.Zoom), el.ID, otherID)
			var bind *whiteboard.Binding
			if target != nil {
				b.bindPreviewID = target.ID
				bind = &whiteboard.Binding{ElementID: target.ID, Gap: 6}
			}
			if b.dragHandle == whiteboard.HandleStart {
				el.StartBinding = bind
			} else {
				el.EndBinding = bind
			}
		}
		whiteboard.UpdateBindings(b.Scene)
		b.bump(false)
		return true
	case dragCreate:
		el := b.Scene.ElementByID(b.creatingID)
		if el == nil {
			return true
		}
		if absf(sx-b.dragStartScene[0]) > 4 || absf(sy-b.dragStartScene[1]) > 4 {
			b.dragMoved = true
		}
		switch el.Kind {
		case whiteboard.KindFreedraw:
			last := el.Points[len(el.Points)-1]
			lx, ly := sx-el.X, sy-el.Y
			if math.Hypot(float64(lx-last[0]), float64(ly-last[1])) >= 2.5 {
				el.Points = append(el.Points, [2]float32{lx, ly})
				el.Version++
			}
		case whiteboard.KindArrow, whiteboard.KindLine:
			dx, dy := sx-el.X, sy-el.Y
			el.Points = [][2]float32{{0, 0}, {dx, dy}}
			el.W, el.H = dx, dy
			b.bindPreviewID = ""
			var startID string
			if el.StartBinding != nil {
				startID = el.StartBinding.ElementID
			}
			if target := whiteboard.FindBindTargetExcluding(b.Scene, sx, sy, 20/max(0.25, b.Zoom), el.ID, startID); target != nil {
				b.bindPreviewID = target.ID
				el.EndBinding = &whiteboard.Binding{ElementID: target.ID, Gap: 6}
			} else {
				el.EndBinding = nil
			}
			whiteboard.UpdateBindings(b.Scene)
			if el.Elbowed && el.StartBinding == nil && el.EndBinding == nil {
				whiteboard.RouteElbowArrow(b.Scene, el)
			}
			el.Version++
		default:
			x0, y0 := b.dragStartScene[0], b.dragStartScene[1]
			w, h := sx-x0, sy-y0
			if ev.Mods&ui.Shift != 0 {
				m := max(absf(w), absf(h))
				if w < 0 {
					w = -m
				} else {
					w = m
				}
				if h < 0 {
					h = -m
				} else {
					h = m
				}
			}
			el.X = min(x0, x0+w)
			el.Y = min(y0, y0+h)
			el.W = absf(w)
			el.H = absf(h)
			el.Version++
		}
		b.bump(false)
		return true
	}
	return false
}

func (b *Board) handlePointerUp(ev ui.InputEvent, o BoardOptions) bool {
	if b.dragMode == dragNone {
		return false
	}
	mode := b.dragMode
	b.dragMode = dragNone
	b.panning = false
	b.guides = nil
	b.bindPreviewID = ""
	b.marquee = nil

	switch mode {
	case dragPan, dragMarquee, dragLaser:
		return true
	case dragCreate:
		el := b.Scene.ElementByID(b.creatingID)
		b.creatingID = ""
		if el != nil && !b.dragMoved {
			switch el.Kind {
			case whiteboard.KindArrow, whiteboard.KindLine:
				dx, dy := float32(120), float32(0)
				if el.StartBinding != nil {
					if target := b.Scene.ElementByID(el.StartBinding.ElementID); target != nil {
						cx, cy := whiteboard.ElementBounds(target).Center()
						vx, vy := b.dragStartScene[0]-cx, b.dragStartScene[1]-cy
						if absf(vx) >= absf(vy) {
							if vx < 0 {
								dx = -96
							} else {
								dx = 96
							}
							dy = 0
						} else {
							dx = 0
							if vy < 0 {
								dy = -96
							} else {
								dy = 96
							}
						}
					}
				}
				el.W, el.H = dx, dy
				el.Points = [][2]float32{{0, 0}, {dx, dy}}
				whiteboard.UpdateBindings(b.Scene)
				if el.Elbowed && el.StartBinding == nil && el.EndBinding == nil {
					whiteboard.RouteElbowArrow(b.Scene, el)
				}
			case whiteboard.KindFreedraw:
				// single dot kept as-is
			case whiteboard.KindNote:
				el.W, el.H = 200, 124
			case whiteboard.KindActor:
				el.W, el.H = 84, 124
			case whiteboard.KindCylinder, whiteboard.KindStar:
				el.W, el.H = 110, 100
			default:
				el.W, el.H = 140, 84
			}
			el.Version++
		}
		if el != nil && b.editingGroupID != "" && (el.Kind == whiteboard.KindArrow || el.Kind == whiteboard.KindLine) && len(el.GroupIDs) > 0 {
			outStart := el.StartBinding != nil && !whiteboard.ElementInGroup(b.Scene.ElementByID(el.StartBinding.ElementID), b.editingGroupID)
			outEnd := el.EndBinding != nil && !whiteboard.ElementInGroup(b.Scene.ElementByID(el.EndBinding.ElementID), b.editingGroupID)
			if outStart || outEnd {
				el.GroupIDs = nil
			}
		}
		if b.History.Commit(b.Scene) {
			b.bump(true)
			if o.OnChange != nil {
				o.OnChange()
			}
		}
		if el != nil && el.Kind == whiteboard.KindNote && !b.dragMoved {
			b.startTextEdit(el)
		}
		if !b.ToolLocked && (el == nil || el.Kind != whiteboard.KindFreedraw) {
			b.Tool = ToolSelect
		}
		return true
	case dragMove, dragResize, dragErase:
		b.resizeOrig = nil
		b.resizeTargetID = ""
		b.resizeOrigGroup = nil
		b.resizeOrigBounds = whiteboard.Box{}
		if b.History.Commit(b.Scene) {
			b.bump(true)
			if o.OnChange != nil {
				o.OnChange()
			}
		}
		return true
	}
	return false
}

func (b *Board) eraseAt(sx, sy, tol float32) {
	hit := whiteboard.HitTest(b.Scene, sx, sy, tol)
	if hit == nil {
		return
	}
	b.Scene.Elements = slices.DeleteFunc(b.Scene.Elements, func(x *whiteboard.Element) bool {
		return x != nil && x.ID == hit.ID
	})
	delete(b.Selected, hit.ID)
	b.bump(false)
}

func (b *Board) buildContextMenu(m *ui.Menu, readClip func() string, writeClip func(string), o BoardOptions) {
	if len(b.Selected) == 1 {
		el := b.selectedList()[0]
		if target, _, ok := el.LinkTarget(); ok && o.OpenWiki != nil {
			if m.Item(i18n.T("打开关联文档") + " · " + target).Chosen() {
				o.OpenWiki(target, false)
			}
			if m.Item(i18n.T("在分屏中打开关联文档")).Chosen() {
				o.OpenWiki(target, true)
			}
			m.Separator()
		}
	}
	hasClipboardMenu := false
	if len(b.Selected) > 0 {
		if m.Item(i18n.T("复制")).Shortcut(ui.Cmd, ui.KeyC).Chosen() {
			b.copySelected(writeClip)
		}
		hasClipboardMenu = true
		if !b.ReadOnly {
			if m.Item(i18n.T("剪切")).Shortcut(ui.Cmd, ui.KeyX).Chosen() {
				b.cutSelected(writeClip, o)
			}
		}
	}
	if !b.ReadOnly && readClip != nil && strings.TrimSpace(readClip()) != "" {
		if m.Item(i18n.T("粘贴")).Shortcut(ui.Cmd, ui.KeyV).Chosen() {
			b.pasteFromClipboard(readClip, o)
		}
		hasClipboardMenu = true
	}
	if hasClipboardMenu {
		m.Separator()
	}
	if !b.ReadOnly && len(b.Selected) > 0 {
		if m.Item(i18n.T("创建副本")).Shortcut(ui.Cmd, ui.KeyD).Chosen() {
			if b.duplicateSelected() && o.OnChange != nil {
				o.OnChange()
			}
		}
		if m.Item(i18n.T("置顶一层")).Chosen() {
			b.zOrderSelected(true)
			if o.OnChange != nil {
				o.OnChange()
			}
		}
		if m.Item(i18n.T("置底一层")).Chosen() {
			b.zOrderSelected(false)
			if o.OnChange != nil {
				o.OnChange()
			}
		}
		if m.Item(i18n.T("删除")).Shortcut(0, ui.KeyBackspace).Chosen() {
			if b.deleteSelected() && o.OnChange != nil {
				o.OnChange()
			}
		}
		selNow := b.selectedList()
		if sharedGID := whiteboard.SharedGroupID(selNow, b.editingGroupID); sharedGID != "" {
			m.Separator()
			if m.Item(i18n.T("进入组内编辑")).Shortcut(0, ui.KeyEnter).Chosen() {
				b.enterGroup(sharedGID, "")
			}
		}
		if len(b.Selected) >= 2 {
			if whiteboard.SharedGroupID(selNow, b.editingGroupID) == "" {
				m.Separator()
			}
			if m.Item(i18n.T("编组")).Shortcut(ui.Cmd, ui.KeyG).Chosen() {
				b.groupSelected(o)
			}
		}
		for _, el := range selNow {
			if len(el.GroupIDs) > 0 {
				if m.Item(i18n.T("取消编组")).Shortcut(ui.Cmd|ui.Shift, ui.KeyG).Chosen() {
					b.ungroupSelected(o)
				}
				break
			}
		}
		m.Separator()
	}
	if !b.ReadOnly && b.editingGroupID != "" {
		if m.Item(i18n.T("退出组内编辑")).Shortcut(0, ui.KeyEscape).Chosen() {
			b.exitGroup(true)
		}
		m.Separator()
	}
	if m.Item(i18n.T("适应画布")).Chosen() {
		b.FitToContent(b.lastRect)
	}
	if m.Item(i18n.T("重置缩放")).Chosen() {
		b.zoomToAtCenter(1)
	}
	if o.ExportSVG != nil {
		m.Separator()
		if m.Item(i18n.T("导出 SVG")).Chosen() {
			o.ExportSVG()
		}
	}
}

func (b *Board) zoomAtCenter(factor float32) {
	cx := b.lastRect.W / 2
	cy := b.lastRect.H / 2
	b.zoomAtPoint(cx, cy, factor)
}

func (b *Board) zoomToAtCenter(target float32) {
	if b.Zoom <= 0 {
		b.Zoom = 1
	}
	b.zoomAtCenter(target / b.Zoom)
}

func (b *Board) zoomAtPoint(lx, ly, factor float32) {
	if b.Zoom <= 0 {
		b.Zoom = 1
	}
	next := clampZoom(b.Zoom * factor)
	b.userViewport = true
	if next == b.Zoom {
		return
	}
	sx, sy := b.localToScene(lx, ly)
	b.Zoom = next
	b.PanX = lx - sx*next
	b.PanY = ly - sy*next
}

func (b *Board) selectedIDs() []string {
	var ids []string
	for id := range b.Selected {
		ids = append(ids, id)
	}
	return ids
}

func selectionBounds(sel []*whiteboard.Element) whiteboard.Box {
	return whiteboard.SelectionBounds(sel)
}

func toolToKind(t BoardTool) whiteboard.ElementKind {
	switch t {
	case ToolDiamond:
		return whiteboard.KindDiamond
	case ToolEllipse:
		return whiteboard.KindEllipse
	case ToolArrow:
		return whiteboard.KindArrow
	case ToolLine:
		return whiteboard.KindLine
	case ToolFreedraw:
		return whiteboard.KindFreedraw
	case ToolText:
		return whiteboard.KindText
	case ToolNote:
		return whiteboard.KindNote
	case ToolFrame:
		return whiteboard.KindFrame
	case ToolTriangle:
		return whiteboard.KindTriangle
	case ToolParallelogram:
		return whiteboard.KindParallelogram
	case ToolHexagon:
		return whiteboard.KindHexagon
	case ToolCylinder:
		return whiteboard.KindCylinder
	case ToolCloud:
		return whiteboard.KindCloud
	case ToolStar:
		return whiteboard.KindStar
	case ToolCallout:
		return whiteboard.KindCallout
	case ToolDocument:
		return whiteboard.KindDocument
	case ToolStadium:
		return whiteboard.KindStadium
	case ToolActor:
		return whiteboard.KindActor
	default:
		return whiteboard.KindRectangle
	}
}
