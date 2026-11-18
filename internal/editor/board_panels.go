package editor

import (
	"fmt"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

type extShapeDef struct {
	tool  BoardTool
	icon  *ui.SVG
	label string
}

var extendedShapeDefs = []extShapeDef{
	{ToolCylinder, iconDatabase, "圆柱 · 数据库"},
	{ToolCloud, iconCloud, "云朵 · 云服务"},
	{ToolHexagon, iconHexagon, "六边形 · 服务"},
	{ToolParallelogram, iconParallelogram, "平行四边形"},
	{ToolStadium, iconStadium, "胶囊起止框"},
	{ToolTriangle, iconTriangle, "三角形"},
	{ToolCallout, iconMessageCircle, "气泡批注"},
	{ToolDocument, iconFileDoc, "波浪文档"},
	{ToolStar, iconStar, "五角星"},
	{ToolActor, iconActor, "角色小人"},
}

func isExtendedShapeTool(t BoardTool) bool {
	for _, d := range extendedShapeDefs {
		if d.tool == t {
			return true
		}
	}
	return false
}

func extendedShapeIcon(t BoardTool) *ui.SVG {
	for _, d := range extendedShapeDefs {
		if d.tool == t {
			return d.icon
		}
	}
	return iconShapes
}

func (b *Board) shapesPopover(c *ui.Context, wb WhiteboardStyle, o BoardOptions) {
	if !b.shapesPopoverOpen || b.ReadOnly {
		return
	}
	t := o.Theme
	ui.Row(c.Key("board-shapes-pop-wrap")).Absolute().Top(54).Left(8).Right(8).Justify(ui.Center).PassThrough().Children(func() {
		card := ui.Column(c.Key("board-shapes-pop")).Width(308).Padding(10).Gap(8).Radius(12).
			Background(wb.IslandFill).Border(1, wb.IslandBorder).Shadow(0, 8, 24, 0, wb.IslandShadow)
		card.Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T("更多手绘图形")).FontSize(12).FontWeight(600).TextColor(t.Text).Grow(1)
				closeBtn := ui.ButtonBase(c.Key("sp-close")).Size(20, 20).Radius(4).Center().Cursor(ui.CursorPointer)
				closeBtn.Children(func() { ui.Icon(c, iconClose).FontSize(12).TextColor(t.TextMuted) })
				if closeBtn.Clicked() {
					b.shapesPopoverOpen = false
				}
			})
			for rowIdx := 0; rowIdx < len(extendedShapeDefs); rowIdx += 2 {
				ui.Row(c).Gap(6).Children(func() {
					for col := 0; col < 2 && rowIdx+col < len(extendedShapeDefs); col++ {
						def := extendedShapeDefs[rowIdx+col]
						active := b.Tool == def.tool
						item := ui.ButtonBase(c.Key(fmt.Sprintf("ext-shape-%d", def.tool))).
							Grow(1).Height(32).PaddingX(8).Radius(8).Gap(7).AlignItems(ui.Center).Cursor(ui.CursorPointer)
						fg := t.Text
						if active {
							item.Background(wb.ActiveFill)
							fg = wb.ActiveInk
						} else if item.Hovered() {
							item.Background(t.SurfaceHover)
						} else {
							item.Background(t.SurfaceHover.Alpha(0.45))
						}
						item.Children(func() {
							ui.Icon(c, def.icon).FontSize(15).TextColor(fg)
							ui.Text(c, i18n.T(def.label)).FontSize(12).TextColor(fg).SingleLine()
						})
						if item.Clicked() {
							b.Tool = def.tool
							b.shapesPopoverOpen = false
							b.RequestFocus = true
						}
					}
				})
			}
		})
	})
}

func (b *Board) mermaidPopover(c *ui.Context, wb WhiteboardStyle, o BoardOptions) {
	if !b.mermaidOpen || b.ReadOnly {
		return
	}
	if b.mermaidInput == "" {
		b.mermaidInput = defaultMermaidToBoardSample
	}
	t := o.Theme
	ui.Row(c.Key("board-mermaid-pop-wrap")).Absolute().Top(54).Left(8).Right(8).Justify(ui.Center).PassThrough().Children(func() {
		card := ui.Column(c.Key("board-mermaid-pop")).Width(364).Padding(12).Gap(8).Radius(12).
			Background(wb.IslandFill).Border(1, wb.IslandBorder).Shadow(0, 8, 24, 0, wb.IslandShadow)
		card.Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconWorkflow).FontSize(14).TextColor(wb.ActiveInk)
				ui.Text(c, i18n.T("Mermaid 生成手绘图形")).FontSize(12).FontWeight(600).TextColor(t.Text).Padding(0, 0, 0, 6).Grow(1)
				closeBtn := ui.ButtonBase(c.Key("mmd-close")).Size(20, 20).Radius(4).Center().Cursor(ui.CursorPointer)
				closeBtn.Children(func() { ui.Icon(c, iconClose).FontSize(12).TextColor(t.TextMuted) })
				if closeBtn.Clicked() {
					b.mermaidOpen = false
					b.mermaidErr = ""
				}
			})
			ui.TextArea(c.Key("mmd-src"), &b.mermaidInput).Width(340).MinHeight(112).FontSize(12).Font("ui-monospace, monospace")
			if b.mermaidErr != "" {
				ui.Text(c, b.mermaidErr).FontSize(11).TextColor(t.Danger)
			}
			ui.Row(c).Gap(6).Justify(ui.End).Children(func() {
				cancel := ui.ButtonBase(c.Key("mmd-cancel")).Height(28).PaddingX(10).Radius(7).Center().
					Cursor(ui.CursorPointer).Background(t.SurfaceHover)
				cancel.Children(func() {
					ui.Text(c, i18n.T("取消")).FontSize(12).TextColor(t.Text)
				})
				if cancel.Clicked() {
					b.mermaidOpen = false
					b.mermaidErr = ""
				}
				gen := ui.ButtonBase(c.Key("mmd-generate")).Height(28).PaddingX(12).Radius(7).Center().
					Cursor(ui.CursorPointer).Background(t.Accent)
				gen.Children(func() {
					ui.Text(c, i18n.T("生成到画布")).FontSize(12).FontWeight(600).TextColor(t.AccentText)
				})
				if gen.Clicked() {
					cx, cy := b.viewportCenterScene()
					elems, err := ConvertMermaidToWhiteboard(b.mermaidInput, cx, cy)
					if err != nil {
						b.mermaidErr = err.Error()
						return
					}
					if len(elems) == 0 {
						b.mermaidErr = i18n.T("未识别到图形节点")
						return
					}
					b.History.Record(b.Scene)
					b.Scene.Elements = append(b.Scene.Elements, elems...)
					b.Selected = make(map[string]bool, len(elems))
					for _, el := range elems {
						b.Selected[el.ID] = true
					}
					b.userViewport = true
					b.Tool = ToolSelect
					b.bump(true)
					b.mermaidOpen = false
					b.mermaidErr = ""
					b.RequestFocus = true
					if o.OnChange != nil {
						o.OnChange()
					}
				}
			})
		})
	})
}

func (b *Board) libraryDrawer(c *ui.Context, wb WhiteboardStyle, o BoardOptions) {
	if !b.libraryOpen || b.ReadOnly {
		return
	}
	t := o.Theme
	builtins := whiteboard.BuiltinLibrary()
	var userItems []whiteboard.LibraryItem
	if o.UserLibrary != nil {
		userItems = o.UserLibrary()
	}

	maxScrollH := float32(280)
	if b.lastRect.H > 0 {
		maxScrollH = max(140, b.lastRect.H-210)
	}

	drawer := ui.Column(c.Key("board-library-drawer")).Absolute().Right(14).Top(58).Width(272).Radius(12).
		Background(wb.IslandFill).Border(1, wb.IslandBorder).Shadow(0, 8, 24, 0, wb.IslandShadow).Clip()
	drawer.Children(func() {
		// Drawer header
		ui.Row(c).Padding(10, 10, 6, 10).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconLibrary).FontSize(14).TextColor(wb.ActiveInk)
			ui.Text(c, i18n.T("组件素材库")).FontSize(12).FontWeight(600).TextColor(t.Text).Grow(1)
			if o.ImportLibraryFile != nil {
				imp := ui.ButtonBase(c.Key("lib-import")).Height(22).PaddingX(7).Radius(5).Center().
					Cursor(ui.CursorPointer).Background(t.SurfaceHover).Tooltip(i18n.T("导入 .excalidrawlib 素材包"))
				imp.Children(func() {
					ui.Text(c, i18n.T("导入…")).FontSize(11).TextColor(t.Text)
				})
				if imp.Clicked() {
					o.ImportLibraryFile(func(imported []whiteboard.LibraryItem) {
						if len(imported) == 0 || o.SaveUserLibrary == nil {
							return
						}
						var cur []whiteboard.LibraryItem
						if o.UserLibrary != nil {
							cur = o.UserLibrary()
						}
						merged := append(cur, imported...)
						o.SaveUserLibrary(merged)
						b.libraryCat = "我的"
					})
				}
			}
			closeBtn := ui.ButtonBase(c.Key("lib-close")).Size(20, 20).Radius(4).Center().Cursor(ui.CursorPointer)
			closeBtn.Children(func() { ui.Icon(c, iconClose).FontSize(12).TextColor(t.TextMuted) })
			if closeBtn.Clicked() {
				b.libraryOpen = false
			}
		})

		// Save current selection as a reusable library component
		if sel := b.selectedList(); len(sel) > 0 && o.SaveUserLibrary != nil {
			ui.Box(c).Padding(0, 10, 6, 10).Children(func() {
				saveSel := ui.ButtonBase(c.Key("lib-save-sel")).Width(252).Height(28).Radius(7).Center().
					Cursor(ui.CursorPointer).Background(wb.ActiveFill)
				saveSel.Children(func() {
					ui.Text(c, i18n.T("+ 将选中图形存入素材库")).FontSize(11).FontWeight(600).TextColor(wb.ActiveInk)
				})
				if saveSel.Clicked() {
					name := ""
					for _, el := range sel {
						if s := strings.TrimSpace(el.Text); s != "" {
							first, _, _ := strings.Cut(s, "\n")
							name = first
							break
						}
					}
					if name == "" {
						name = fmt.Sprintf(i18n.T("自定义组件 %d"), len(userItems)+1)
					}
					clones := make([]*whiteboard.Element, len(sel))
					for i, el := range sel {
						clones[i] = el.Clone()
					}
					item := whiteboard.LibraryItem{
						ID:       whiteboard.NewID(),
						Name:     name,
						Category: "我的",
						Elements: clones,
					}
					next := append([]whiteboard.LibraryItem{item}, userItems...)
					o.SaveUserLibrary(next)
					b.libraryCat = "我的"
				}
			})
		}

		// Category filter tabs
		ui.Row(c).Padding(0, 10, 8, 10).Gap(4).Wrap().Children(func() {
			cats := []struct{ key, label string }{
				{"", "全部"},
				{"我的", "我的"},
				{"系统架构", "系统架构"},
				{"界面线框", "界面线框"},
				{"流程与思维", "流程与思维"},
			}
			for _, ct := range cats {
				if ct.key == "我的" && len(userItems) == 0 {
					continue
				}
				if b.pillChoice(c, "lib-cat-"+ct.key, i18n.T(ct.label), b.libraryCat == ct.key, wb, t).Clicked() {
					b.libraryCat = ct.key
				}
			}
		})

		ui.Box(c).Height(1).Background(wb.IslandBorder)

		var visible []struct {
			item   whiteboard.LibraryItem
			isUser bool
			uIdx   int
		}
		if b.libraryCat == "" || b.libraryCat == "我的" {
			for i, it := range userItems {
				visible = append(visible, struct {
					item   whiteboard.LibraryItem
					isUser bool
					uIdx   int
				}{item: it, isUser: true, uIdx: i})
			}
		}
		for _, it := range builtins {
			if b.libraryCat == "" || b.libraryCat == it.Category {
				visible = append(visible, struct {
					item   whiteboard.LibraryItem
					isUser bool
					uIdx   int
				}{item: it})
			}
		}

		ui.Scroll(c.Key("board-lib-scroll")).MaxHeight(maxScrollH).Children(func() {
			ui.Column(c).Padding(10).Gap(8).Children(func() {
				if len(visible) == 0 {
					ui.Text(c, i18n.T("暂无素材组件")).FontSize(12).TextColor(t.TextMuted)
					return
				}
				for i := 0; i < len(visible); i += 2 {
					ui.Row(c).Gap(8).Children(func() {
						for col := 0; col < 2 && i+col < len(visible); col++ {
							entry := visible[i+col]
							item := entry.item
							previewScene := &whiteboard.Scene{Elements: item.Elements}
							card := ui.ButtonBase(c.Key("lib-card-"+item.ID)).Width(120).Height(94).Padding(6).Radius(8).
								Background(wb.Canvas).Border(1, wb.IslandBorder).Cursor(ui.CursorPointer)
							if card.Hovered() {
								card.Border(1.25, t.Accent.Alpha(0.75))
							}
							card.Children(func() {
								ui.Column(c).Fill().Gap(4).Children(func() {
									ui.Box(c).Width(108).Height(58).Draw(func(p *ui.Painter, r ui.Rect) {
										PaintSceneFit(p, r, previewScene, wb, t, 4)
									})
									ui.Row(c).WidthPercent(100).MinWidth(0).Gap(2).AlignItems(ui.Center).Children(func() {
										ui.Text(c, i18n.T(item.Name)).FontSize(11).TextColor(t.Text).SingleLine().MinWidth(0).Grow(1)
										if entry.isUser && o.SaveUserLibrary != nil {
											del := ui.ButtonBase(c.Key("lib-del-"+item.ID)).Size(16, 16).Radius(4).Center().Cursor(ui.CursorPointer)
											del.Children(func() { ui.Icon(c, iconClose).FontSize(10).TextColor(t.TextMuted) })
											if del.Clicked() {
												idx := entry.uIdx
												updated := make([]whiteboard.LibraryItem, 0, len(userItems)-1)
												updated = append(updated, userItems[:idx]...)
												updated = append(updated, userItems[idx+1:]...)
												o.SaveUserLibrary(updated)
												return
											}
										}
									})
								})
							})
							if card.Clicked() {
								cx, cy := b.viewportCenterScene()
								elems := whiteboard.InstantiateLibraryItem(item, cx, cy)
								if len(elems) > 0 {
									b.History.Record(b.Scene)
									b.Scene.Elements = append(b.Scene.Elements, elems...)
									b.Selected = make(map[string]bool, len(elems))
									for _, el := range elems {
										b.Selected[el.ID] = true
									}
									b.userViewport = true
									b.Tool = ToolSelect
									b.bump(true)
									b.RequestFocus = true
									if o.OnChange != nil {
										o.OnChange()
									}
								}
							}
						}
					})
				}
			})
		})
	})
}

func (b *Board) viewportCenterScene() (float32, float32) {
	if b.lastRect.W > 0 && b.lastRect.H > 0 {
		return b.localToScene(b.lastRect.W*0.5, b.lastRect.H*0.5)
	}
	return 360, 240
}

type boardStyleSnapshot struct {
	stroke      string
	fill        string
	fillStyle   whiteboard.FillStyle
	sw          float32
	ss          whiteboard.StrokeStyle
	rough       int
	round       bool
	ff          string
	fontSize    float32
	textAlign   string
	opacity     int
	routing     string
	startHead   whiteboard.Arrowhead
	endHead     whiteboard.Arrowhead
	locked      bool
	isConnector bool
	hasTextBox  bool
}

func (b *Board) activeProps() boardStyleSnapshot {
	sel := b.selectedList()
	routing := b.ArrowRouting
	if routing == "" {
		routing = "sharp"
	}
	if len(sel) > 0 {
		el := sel[0]
		isConn := el.Kind == whiteboard.KindArrow || el.Kind == whiteboard.KindLine
		r := "sharp"
		if el.Elbowed {
			r = "elbow"
		} else if el.Smooth {
			r = "curved"
		}
		op := el.Opacity
		if op <= 0 {
			op = 100
		}
		fs := el.FontSize
		if fs <= 0 {
			fs = 18
		}
		ta := el.TextAlign
		if ta == "" {
			ta = "center"
		}
		return boardStyleSnapshot{
			stroke:      el.StrokeColor,
			fill:        el.FillColor,
			fillStyle:   el.FillStyle,
			sw:          el.StrokeWidth,
			ss:          el.StrokeStyle,
			rough:       el.Roughness,
			round:       el.Roundness,
			ff:          el.FontFamily,
			fontSize:    fs,
			textAlign:   ta,
			opacity:     op,
			routing:     r,
			startHead:   el.StartArrowhead,
			endHead:     el.EndArrowhead,
			locked:      el.Locked,
			isConnector: isConn,
			hasTextBox:  !isConn && el.Kind != whiteboard.KindFreedraw && el.Kind != whiteboard.KindFrame,
		}
	}
	isConn := b.Tool == ToolArrow || b.Tool == ToolLine
	return boardStyleSnapshot{
		stroke:      b.StrokeColor,
		fill:        b.FillColor,
		fillStyle:   b.FillStyle,
		sw:          b.StrokeWidth,
		ss:          b.StrokeStyle,
		rough:       b.Roughness,
		round:       b.Roundness,
		ff:          b.FontFamily,
		fontSize:    b.FontSize,
		textAlign:   b.TextAlign,
		opacity:     b.Opacity,
		routing:     routing,
		startHead:   b.StartArrowhead,
		endHead:     b.EndArrowhead,
		isConnector: isConn,
		hasTextBox:  !isConn && b.Tool != ToolFreedraw && b.Tool != ToolFrame,
	}
}

func (b *Board) setStrokeColor(col string, o BoardOptions) {
	b.StrokeColor = col
	b.applyStyleToSelection(func(el *whiteboard.Element) { el.StrokeColor = col })
	if o.OnChange != nil {
		o.OnChange()
	}
}

func (b *Board) setFillColor(col string, o BoardOptions) {
	b.FillColor = col
	b.applyStyleToSelection(func(el *whiteboard.Element) {
		el.FillColor = col
		if col != "" && el.FillStyle == whiteboard.FillNone {
			el.FillStyle = b.FillStyle
			if el.FillStyle == whiteboard.FillNone {
				el.FillStyle = whiteboard.FillHachure
			}
		}
	})
	if o.OnChange != nil {
		o.OnChange()
	}
}

func (b *Board) floatingStylePanel(c *ui.Context, wb WhiteboardStyle, o BoardOptions) {
	sel := b.selectedList()
	showStyleControls := len(sel) > 0 || (b.Tool != ToolSelect && b.Tool != ToolHand && b.Tool != ToolEraser && b.Tool != ToolLaser)
	if !showStyleControls && b.editingGroupID == "" {
		return
	}
	t := o.Theme
	props := b.activeProps()
	maxH := float32(420)
	if b.lastRect.H > 0 {
		maxH = max(200, b.lastRect.H-128)
	}

	panel := ui.Column(c.Key("board-style-panel")).Absolute().Left(14).Top(62).Width(212).Radius(12).
		Background(wb.IslandFill).Border(1, wb.IslandBorder).Shadow(0, 6, 18, 0, wb.IslandShadow).Clip()

	panel.Children(func() {
		ui.Scroll(c.Key("board-style-scroll")).MaxHeight(maxH).Children(func() {
			ui.Column(c).Padding(11).Gap(9).Children(func() {
				if b.editingGroupID != "" {
					if b.pillChoice(c, "grp-exit-iso", i18n.T("退出组内编辑 (Esc)"), true, wb, t).Clicked() {
						b.exitGroup(true)
					}
					if !showStyleControls {
						return
					}
				}
				// Stroke color swatches.
				b.styleSection(c, t, "描边颜色", func() {
					ui.Row(c).Gap(4).Wrap().Children(func() {
						for _, name := range whiteboard.SemanticColors {
							col := resolveBoardStroke(wb, name, t)
							if b.colorSwatchBtn(c, "sc-"+name, col, props.stroke == name, false, wb, t).Clicked() {
								b.setStrokeColor(name, o)
							}
						}
					})
				})

				// Fill color swatches & fill style.
				if !props.isConnector {
					b.styleSection(c, t, "填充颜色", func() {
						ui.Row(c).Gap(4).Wrap().Children(func() {
							if b.colorSwatchBtn(c, "fc-none", t.Background, props.fill == "", true, wb, t).Clicked() {
								b.setFillColor("", o)
							}
							for _, name := range whiteboard.SemanticColors {
								col := resolveBoardFill(wb, name, t)
								if b.colorSwatchBtn(c, "fc-"+name, col, props.fill == name, false, wb, t).Clicked() {
									b.setFillColor(name, o)
								}
							}
						})
					})

					b.styleSection(c, t, "填充样式", func() {
						ui.Row(c).Gap(4).Children(func() {
							for _, opt := range []struct {
								label string
								val   whiteboard.FillStyle
							}{
								{"斜线", whiteboard.FillHachure},
								{"网格", whiteboard.FillCrossHatch},
								{"纯色", whiteboard.FillSolid},
							} {
								if b.pillChoice(c, "fs-"+string(opt.val), i18n.T(opt.label), props.fillStyle == opt.val, wb, t).Clicked() {
									b.FillStyle = opt.val
									b.applyStyleToSelection(func(el *whiteboard.Element) { el.FillStyle = opt.val })
									if o.OnChange != nil {
										o.OnChange()
									}
								}
							}
						})
					})
				}

				// Connector routing & arrowheads.
				if props.isConnector {
					b.styleSection(c, t, "连接线型", func() {
						ui.Row(c).Gap(4).Children(func() {
							for _, opt := range []struct{ label, val string }{{"直线", "sharp"}, {"曲线", "curved"}, {"折线", "elbow"}} {
								if b.pillChoice(c, "rt-"+opt.val, i18n.T(opt.label), props.routing == opt.val, wb, t).Clicked() {
									b.ArrowRouting = opt.val
									b.applyStyleToSelection(func(el *whiteboard.Element) {
										if el.Kind != whiteboard.KindArrow && el.Kind != whiteboard.KindLine {
											return
										}
										el.Smooth = opt.val == "curved"
										el.Elbowed = opt.val == "elbow"
										if el.Elbowed {
											whiteboard.RouteElbowArrow(b.Scene, el)
										} else if len(el.Points) > 2 {
											el.Points = [][2]float32{el.Points[0], el.Points[len(el.Points)-1]}
										}
									})
									if o.OnChange != nil {
										o.OnChange()
									}
								}
							}
						})
					})

					headOpts := []struct {
						label string
						val   whiteboard.Arrowhead
					}{
						{"无", whiteboard.ArrowNone},
						{"→", whiteboard.ArrowOpen},
						{"▶", whiteboard.ArrowTriangle},
						{"|", whiteboard.ArrowBar},
						{"●", whiteboard.ArrowDot},
					}
					b.styleSection(c, t, "终点箭头", func() {
						ui.Row(c).Gap(3).Children(func() {
							for _, opt := range headOpts {
								if b.pillChoice(c, "eh-"+string(opt.val), i18n.T(opt.label), props.endHead == opt.val, wb, t).Clicked() {
									b.EndArrowhead = opt.val
									b.applyStyleToSelection(func(el *whiteboard.Element) { el.EndArrowhead = opt.val })
									if o.OnChange != nil {
										o.OnChange()
									}
								}
							}
						})
					})
					b.styleSection(c, t, "起点箭头", func() {
						ui.Row(c).Gap(3).Children(func() {
							for _, opt := range headOpts {
								if b.pillChoice(c, "sh-"+string(opt.val), i18n.T(opt.label), props.startHead == opt.val, wb, t).Clicked() {
									b.StartArrowhead = opt.val
									b.applyStyleToSelection(func(el *whiteboard.Element) { el.StartArrowhead = opt.val })
									if o.OnChange != nil {
										o.OnChange()
									}
								}
							}
						})
					})
				}

				// Stroke width & style.
				b.styleSection(c, t, "线条粗细", func() {
					ui.Row(c).Gap(4).Children(func() {
						for _, opt := range []struct {
							label string
							w     float32
						}{{"细", 1.25}, {"中", 2}, {"粗", 4}} {
							if b.pillChoice(c, "sw-"+opt.label, i18n.T(opt.label), absf(props.sw-opt.w) < 0.4, wb, t).Clicked() {
								b.StrokeWidth = opt.w
								b.applyStyleToSelection(func(el *whiteboard.Element) { el.StrokeWidth = opt.w })
								if o.OnChange != nil {
									o.OnChange()
								}
							}
						}
					})
				})

				b.styleSection(c, t, "线条样式", func() {
					ui.Row(c).Gap(4).Children(func() {
						for _, opt := range []struct {
							label string
							val   whiteboard.StrokeStyle
						}{
							{"实线", whiteboard.StrokeSolid},
							{"虚线", whiteboard.StrokeDashed},
							{"点线", whiteboard.StrokeDotted},
						} {
							if b.pillChoice(c, "ss-"+string(opt.val), i18n.T(opt.label), props.ss == opt.val, wb, t).Clicked() {
								b.StrokeStyle = opt.val
								b.applyStyleToSelection(func(el *whiteboard.Element) { el.StrokeStyle = opt.val })
								if o.OnChange != nil {
									o.OnChange()
								}
							}
						}
					})
				})

				b.styleSection(c, t, "手绘风格", func() {
					ui.Row(c).Gap(4).Children(func() {
						for _, opt := range []struct {
							label string
							val   int
						}{{"规整", 0}, {"手绘", 1}, {"涂鸦", 2}} {
							if b.pillChoice(c, fmt.Sprintf("rg-%d", opt.val), i18n.T(opt.label), props.rough == opt.val, wb, t).Clicked() {
								b.Roughness = opt.val
								b.applyStyleToSelection(func(el *whiteboard.Element) { el.Roughness = opt.val })
								if o.OnChange != nil {
									o.OnChange()
								}
							}
						}
					})
				})

				if !props.isConnector {
					b.styleSection(c, t, "边角", func() {
						ui.Row(c).Gap(4).Children(func() {
							if b.pillChoice(c, "rnd-0", i18n.T("直角"), !props.round, wb, t).Clicked() {
								b.Roundness = false
								b.applyStyleToSelection(func(el *whiteboard.Element) { el.Roundness = false })
								if o.OnChange != nil {
									o.OnChange()
								}
							}
							if b.pillChoice(c, "rnd-1", i18n.T("圆角"), props.round, wb, t).Clicked() {
								b.Roundness = true
								b.applyStyleToSelection(func(el *whiteboard.Element) { el.Roundness = true })
								if o.OnChange != nil {
									o.OnChange()
								}
							}
						})
					})
				}

				if props.hasTextBox || props.isConnector {
					b.styleSection(c, t, "字体", func() {
						ui.Row(c).Gap(4).Children(func() {
							for _, opt := range []struct{ label, val string }{{"手写体", "hand"}, {"系统字体", "sans"}, {"等宽体", "mono"}} {
								if b.pillChoice(c, "ff-"+opt.val, i18n.T(opt.label), props.ff == opt.val, wb, t).Clicked() {
									b.FontFamily = opt.val
									b.applyStyleToSelection(func(el *whiteboard.Element) { el.FontFamily = opt.val })
									if o.OnChange != nil {
										o.OnChange()
									}
								}
							}
						})
					})

					b.styleSection(c, t, "字号", func() {
						ui.Row(c).Gap(4).Children(func() {
							for _, opt := range []struct {
								label string
								sz    float32
							}{{"小", 14}, {"中", 18}, {"大", 24}, {"特大", 32}} {
								if b.pillChoice(c, "fz-"+opt.label, i18n.T(opt.label), absf(props.fontSize-opt.sz) < 2, wb, t).Clicked() {
									b.FontSize = opt.sz
									b.applyStyleToSelection(func(el *whiteboard.Element) { el.FontSize = opt.sz })
									if o.OnChange != nil {
										o.OnChange()
									}
								}
							}
						})
					})
				}

				b.styleSection(c, t, "透明度", func() {
					ui.Row(c).Gap(4).Children(func() {
						for _, pct := range []int{25, 50, 75, 100} {
							if b.pillChoice(c, fmt.Sprintf("op-%d", pct), fmt.Sprintf("%d%%", pct), props.opacity == pct, wb, t).Clicked() {
								b.Opacity = pct
								b.applyStyleToSelection(func(el *whiteboard.Element) { el.Opacity = pct })
								if o.OnChange != nil {
									o.OnChange()
								}
							}
						}
					})
				})

				if len(sel) > 0 {
					ui.Box(c).Height(1).Background(wb.IslandBorder)

					if len(sel) >= 2 {
						b.styleSection(c, t, "对齐排列", func() {
							ui.Column(c).Gap(4).Children(func() {
								ui.Row(c).Gap(4).Children(func() {
									for _, opt := range []struct{ label, mode string }{{"左", "left"}, {"水平中", "center-h"}, {"右", "right"}} {
										if b.pillChoice(c, "al-"+opt.mode, i18n.T(opt.label), false, wb, t).Clicked() {
											b.History.Record(b.Scene)
											if whiteboard.AlignElements(b.Scene, b.selectedIDs(), opt.mode) {
												b.bump(true)
												if o.OnChange != nil {
													o.OnChange()
												}
											}
										}
									}
								})
								ui.Row(c).Gap(4).Children(func() {
									for _, opt := range []struct{ label, mode string }{{"顶", "top"}, {"垂直中", "center-v"}, {"底", "bottom"}} {
										if b.pillChoice(c, "al-"+opt.mode, i18n.T(opt.label), false, wb, t).Clicked() {
											b.History.Record(b.Scene)
											if whiteboard.AlignElements(b.Scene, b.selectedIDs(), opt.mode) {
												b.bump(true)
												if o.OnChange != nil {
													o.OnChange()
												}
											}
										}
									}
								})
								if len(sel) >= 3 {
									ui.Row(c).Gap(4).Children(func() {
										if b.pillChoice(c, "dist-h", i18n.T("水平等距"), false, wb, t).Clicked() {
											b.History.Record(b.Scene)
											if whiteboard.DistributeElements(b.Scene, b.selectedIDs(), true) {
												b.bump(true)
												if o.OnChange != nil {
													o.OnChange()
												}
											}
										}
										if b.pillChoice(c, "dist-v", i18n.T("垂直等距"), false, wb, t).Clicked() {
											b.History.Record(b.Scene)
											if whiteboard.DistributeElements(b.Scene, b.selectedIDs(), false) {
												b.bump(true)
												if o.OnChange != nil {
													o.OnChange()
												}
											}
										}
									})
								}
							})
						})

						if sharedGID := whiteboard.SharedGroupID(sel, b.editingGroupID); sharedGID != "" {
							ui.Row(c).Gap(4).Children(func() {
								if b.pillChoice(c, "grp-enter-iso", i18n.T("进入组内编辑"), false, wb, t).Clicked() {
									b.enterGroup(sharedGID, "")
								}
							})
						}
						ui.Row(c).Gap(4).Children(func() {
							if b.pillChoice(c, "grp-make", i18n.T("编组"), false, wb, t).Clicked() {
								b.groupSelected(o)
							}
							if b.pillChoice(c, "grp-un", i18n.T("取消编组"), false, wb, t).Clicked() {
								b.ungroupSelected(o)
							}
						})
					}

					// Document linking.
					if len(sel) == 1 && o.WikiSuggest != nil {
						target, _, hasLink := sel[0].LinkTarget()
						ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
							lbl := i18n.T("关联文档…")
							if hasLink {
								lbl = "↗ " + target
							}
							linkBtn := ui.ButtonBase(c.Key("link-doc-btn")).Grow(1).Height(26).PaddingX(8).Radius(6).
								Gap(5).AlignItems(ui.Center).Cursor(ui.CursorPointer).Background(t.SurfaceHover)
							linkBtn.Children(func() {
								ui.Icon(c, iconLink).FontSize(12).TextColor(wb.ActiveInk)
								ui.Text(c, lbl).FontSize(11).TextColor(t.Text).SingleLine().Grow(1)
							})
							if linkBtn.Clicked() {
								b.linkPopoverOpen = !b.linkPopoverOpen
								b.linkSearchQuery = ""
							}
							if hasLink {
								clr := ui.ButtonBase(c.Key("unlink-doc-btn")).Size(24, 26).Radius(6).Center().
									Tooltip(i18n.T("取消关联")).Cursor(ui.CursorPointer)
								if clr.Hovered() {
									clr.Background(t.SurfaceHover)
								}
								clr.Children(func() { ui.Icon(c, iconClose).FontSize(12).TextColor(t.TextMuted) })
								if clr.Clicked() {
									b.applyStyleToSelection(func(el *whiteboard.Element) { el.Link = "" })
									if o.OnChange != nil {
										o.OnChange()
									}
								}
							}
						})
					}

					ui.Row(c).Gap(4).Children(func() {
						if b.pillChoice(c, "z-back", i18n.T("置底一层"), false, wb, t).Clicked() {
							b.zOrderSelected(false)
							if o.OnChange != nil {
								o.OnChange()
							}
						}
						if b.pillChoice(c, "z-fwd", i18n.T("置顶一层"), false, wb, t).Clicked() {
							b.zOrderSelected(true)
							if o.OnChange != nil {
								o.OnChange()
							}
						}
					})
					ui.Row(c).Gap(4).Children(func() {
						lockLbl := "锁定"
						if props.locked {
							lockLbl = "解锁"
						}
						if b.pillChoice(c, "act-lock", i18n.T(lockLbl), props.locked, wb, t).Clicked() {
							next := !props.locked
							b.applyStyleToSelection(func(el *whiteboard.Element) { el.Locked = next })
							if o.OnChange != nil {
								o.OnChange()
							}
						}
						if b.pillChoice(c, "act-dup", i18n.T("创建副本"), false, wb, t).Clicked() {
							b.duplicateSelected()
							if o.OnChange != nil {
								o.OnChange()
							}
						}
						del := ui.ButtonBase(c.Key("act-del")).Grow(1).Height(24).Radius(6).Center().
							Background(t.Danger.Alpha(0.12)).Cursor(ui.CursorPointer)
						del.Children(func() {
							ui.Text(c, i18n.T("删除")).FontSize(11).TextColor(t.Danger).NoWrap()
						})
						if del.Clicked() {
							b.deleteSelected()
							if o.OnChange != nil {
								o.OnChange()
							}
						}
					})
				}
			})
		})
	})
}

func (b *Board) groupSelected(o BoardOptions) bool {
	if b.ReadOnly || len(b.Selected) < 2 {
		return false
	}
	b.History.Record(b.Scene)
	if whiteboard.GroupElements(b.Scene, b.selectedIDs()) == "" {
		return false
	}
	b.bump(true)
	if o.OnChange != nil {
		o.OnChange()
	}
	return true
}

func (b *Board) ungroupSelected(o BoardOptions) bool {
	if b.ReadOnly || len(b.Selected) == 0 {
		return false
	}
	b.History.Record(b.Scene)
	if !whiteboard.UngroupElements(b.Scene, b.selectedIDs()) {
		return false
	}
	b.pruneEditingGroup()
	b.bump(true)
	if o.OnChange != nil {
		o.OnChange()
	}
	return true
}
