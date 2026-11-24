package dbview

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

type tableRowKind uint8

const (
	itemGroupHeader tableRowKind = iota
	itemRecord
	itemEmptyState
	itemAddRow
)

type tableItem struct {
	key        string
	kind       tableRowKind
	group      *database.RecordGroup
	record     *database.Record
	groupProp  string
	groupValue database.Value
}

type tableCol struct {
	prop  database.Property
	vp    database.ViewProperty
	width float32
	wrap  bool
}

type colDrag struct{ propID string }
type rowDrag struct{ recordID string }

const (
	tableGutterW = float32(46)
	tableHeaderH = float32(42)
	tableFooterH = float32(36)
	tableAddColW = float32(40)
)

func tableView(
	c *ui.Context,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	opts Options,
	pal Palette,
) {
	items := buildTableItems(view, res)
	cols := visibleTableColumns(db, &view)
	if len(cols) == 0 {
		return
	}
	be := beTablePalette(pal)
	rowH := tableRowMinH(view)

	freezeFirst := view.Table == nil || view.Table.FreezeFirst
	frozenCol := cols[0]
	var rightCols []tableCol
	if freezeFirst {
		rightCols = cols[1:]
	} else {
		rightCols = cols
	}
	frozenW := tableGutterW + frozenCol.width
	if !freezeFirst {
		frozenW = tableGutterW
	}

	// Build flat list of record indices inside items for keyboard navigation.
	var recordItemIndices []int
	for i, it := range items {
		if it.kind == itemRecord {
			recordItemIndices = append(recordItemIndices, i)
		}
	}

	// Handle keyboard navigation when table has focus and no editor/popover is open.
	tableArea := ui.Column(c.Key("table-area")).Grow(1).MinWidth(0).MinHeight(0).
		Focusable().Role(ui.RoleGrid).Label(db.Title)
	if tableArea.Clicked() {
		tableArea.Focus()
	}

	canNav := tableArea.FocusWithin() && !c.FocusTakesText() &&
		!st.Editing && !st.EditPopoverOpen && !st.FilterOpen && !st.SortOpen &&
		!st.GroupOpen && !st.PropsOpen && !st.NewViewOpen && !st.AddPropOpen &&
		!st.ColorRulesOpen && !st.TemplatesOpen && !st.EditPropOpen &&
		!st.TableColMenuOpen && !st.TableRowMenuOpen

	if canNav && len(recordItemIndices) > 0 {
		if st.FocusRow < 0 || st.FocusRow >= len(recordItemIndices) {
			if tableArea.Focused() {
				st.FocusRow = 0
				if st.FocusCol < 0 {
					st.FocusCol = 0
				}
			}
		}
		switch {
		case tableArea.Shortcut(0, ui.KeyDown):
			if st.FocusRow+1 < len(recordItemIndices) {
				st.FocusRow++
				if st.FocusCol < 0 {
					st.FocusCol = 0
				}
				st.RowScroll.ScrollIntoView(recordItemIndices[st.FocusRow])
			}
		case tableArea.Shortcut(0, ui.KeyUp):
			if st.FocusRow > 0 {
				st.FocusRow--
				if st.FocusCol < 0 {
					st.FocusCol = 0
				}
				st.RowScroll.ScrollIntoView(recordItemIndices[st.FocusRow])
			}
		case tableArea.Shortcut(0, ui.KeyRight):
			if st.FocusRow < 0 {
				st.FocusRow = 0
			}
			if st.FocusCol+1 < len(cols) {
				st.FocusCol++
			}
		case tableArea.Shortcut(0, ui.KeyLeft):
			if st.FocusCol > 0 {
				st.FocusCol--
			}
		case tableArea.Shortcut(ui.Shift, ui.KeyTab):
			moveTableFocus(&st.FocusRow, &st.FocusCol, len(recordItemIndices), len(cols), -1)
			if st.FocusRow >= 0 && st.FocusRow < len(recordItemIndices) {
				st.RowScroll.ScrollIntoView(recordItemIndices[st.FocusRow])
			}
		case tableArea.Shortcut(0, ui.KeyTab):
			moveTableFocus(&st.FocusRow, &st.FocusCol, len(recordItemIndices), len(cols), 1)
			if st.FocusRow >= 0 && st.FocusRow < len(recordItemIndices) {
				st.RowScroll.ScrollIntoView(recordItemIndices[st.FocusRow])
			}
		case tableArea.Shortcut(0, ui.KeyEnter), tableArea.Shortcut(0, ui.KeyF2):
			if st.FocusRow >= 0 && st.FocusRow < len(recordItemIndices) && st.FocusCol >= 0 && st.FocusCol < len(cols) {
				rec := items[recordItemIndices[st.FocusRow]].record
				prop := cols[st.FocusCol].prop
				activateCellEdit(st, rec, prop, opts)
			}
		}
	}

	totalColsW := frozenW + tableAddColW
	for _, rc := range rightCols {
		totalColsW += rc.width
	}

	rowTotal := 0
	if res != nil {
		rowTotal = len(res.Rows)
	}
	selectedCount := countSelectedInResult(st, res)

	tableArea.Children(func() {
		wrap := ui.Column(c.Key("table-wrap")).Grow(1).MinWidth(0).MinHeight(0)
		if opts.Embedded {
			wrap.Padding(4, 0, 8, 0).Gap(6)
		} else {
			wrap.Padding(8, 22, 16, 22).Gap(6)
		}

		wrap.Children(func() {
			// Quiet meta strip ("6 行" / "已选 1 项").
			ui.Row(c.Key("table-meta")).Height(18).Padding(0, 2).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, fmt.Sprintf(i18n.T("%d 行"), rowTotal)).FontSize(11.5).FontWeight(500).TextColor(be.metaInk).NoWrap()
				if selectedCount > 0 {
					ui.Text(c, fmt.Sprintf(i18n.T("已选 %d 项"), selectedCount)).FontSize(11.5).FontWeight(500).TextColor(be.primary).NoWrap()
				}
			})

			// Refined Arc/Linear rounded card container with subtle hairline border & ambient shadow.
			card := ui.Column(c.Key("table-card")).Grow(1).MinWidth(0).MinHeight(0).
				Radius(12).Background(be.cardBg).Border(1, be.border).
				Shadow(0, 1, 3, 0, pal.ShadowEdge).Clip()

			card.Children(func() {
				scroller := ui.ScrollHorizontal(c.Key("table-x")).TrackScroll(&st.TableScrollX).
					AlignItems(ui.Stretch).Grow(1).MinWidth(0).MinHeight(0)
				viewportW := scroller.LayoutBounds().W
				contentW := max(viewportW, totalColsW)
				scrollX := st.TableScrollX.X

				scroller.Children(func() {
					ui.Column(c.Key("table-inner")).Width(contentW).Grow(1).MinHeight(0).Children(func() {
						// 1. Sticky Header Row.
						renderTableHeaderRow(c, st, &view, db, frozenCol, rightCols, freezeFirst, frozenW, contentW, scrollX, selectedCount, rowTotal, res, be, opts, pal)

						// 2. Virtualized Rows Body.
						recSeq := 0
						recIdxByID := make(map[string]int, len(recordItemIndices))
						for _, itemIdx := range recordItemIndices {
							if r := items[itemIdx].record; r != nil {
								recIdxByID[r.ID] = recSeq
								recSeq++
							}
						}

						st.RowScroll.KeepOffset = true
						st.RowScroll.MoveAnimation = &dbSpringList
						list := ui.List(c.Key("table-rows"), &st.RowScroll, len(items)).
							Grow(1).MinHeight(0).ItemKey(func(i int) any { return items[i].key })
						list.Rows(func(row ui.ListRow) {
							rc := row.Context
							it := items[row.Index]
							switch it.kind {
							case itemGroupHeader:
								renderTableGroupHeaderRow(rc, st, &view, it.group, contentW, scrollX, be, opts, pal)
							case itemEmptyState:
								renderEmptyTableRow(rc, contentW, scrollX, be, pal)
							case itemAddRow:
								renderAddRow(rc, st, &view, db, it.groupProp, it.groupValue, contentW, scrollX, be, opts)
							case itemRecord:
								rIdx := recIdxByID[it.record.ID]
								tabDir := renderRecordRow(rc, st, &view, db, it, cols, frozenCol, rightCols, freezeFirst, frozenW, contentW, scrollX, rowH, rIdx, res, be, opts, pal)
								if tabDir != 0 {
									moveTableFocus(&st.FocusRow, &st.FocusCol, len(recordItemIndices), len(cols), tabDir)
									if st.FocusRow >= 0 && st.FocusRow < len(recordItemIndices) {
										st.RowScroll.ScrollIntoView(recordItemIndices[st.FocusRow])
										nextRec := items[recordItemIndices[st.FocusRow]].record
										nextProp := cols[st.FocusCol].prop
										if isInlineTextProp(nextProp.Type) {
											activateCellEdit(st, nextRec, nextProp, opts)
										}
									}
								}
							}
						})

						// 3. Sticky Aggregation Footer Row.
						renderTableFooterRow(c, st, &view, db, frozenCol, rightCols, freezeFirst, frozenW, contentW, scrollX, res, be, opts, pal)
					})
				})
			})
		})
	})
}

func countSelectedInResult(st *State, res *database.QueryResult) int {
	if st == nil || res == nil || len(st.SelectedIDs) == 0 {
		return 0
	}
	count := 0
	for _, r := range res.Rows {
		if r != nil && st.SelectedIDs[r.ID] {
			count++
		}
	}
	return count
}

func renderTableHeaderRow(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	frozenCol tableCol,
	rightCols []tableCol,
	freezeFirst bool,
	frozenW, contentW, scrollX float32,
	selectedCount, rowTotal int,
	res *database.QueryResult,
	be beTableColors,
	opts Options,
	pal Palette,
) {
	head := ui.Box(c.Key("table-head")).Width(contentW).Height(tableHeaderH).Shrink(0).
		Background(be.headerBg).BorderWidth(0, 0, 1, 0).BorderColor(be.border).Clip()
	head.Children(func() {
		// Right scrollable column headers.
		ui.Row(c).PaddingStart(frozenW).Height(tableHeaderH).AlignItems(ui.Stretch).Children(func() {
			for _, col := range rightCols {
				renderColumnHeaderCell(c, st, view, db, col, be, opts, pal)
			}
			// Add property "+" header button.
			addCol := ui.Box(c.Key("add-col-hdr")).Width(tableAddColW).Height(tableHeaderH).Center().
				Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("新建属性")).Tooltip(i18n.T("新建属性"))
			if addCol.Hovered() {
				addCol.Background(be.rowHover)
			}
			addCol.Children(func() { ui.Icon(c, iconPlus).FontSize(14).TextColor(pal.TextMuted) })
			if addCol.Clicked() && !opts.ReadOnly {
				st.CloseToolbarPopovers()
				st.AddPropOpen = true
				st.NewPropName = ""
			}
			if st.AddPropOpen && !opts.ReadOnly {
				addPropertyPopover(c, addCol, st, db, opts, pal)
			}
		})

		// Frozen left gutter + first column header.
		frozen := ui.Row(c.Key("frozen-head")).Absolute().Left(0).Top(0).Bottom(0).Width(frozenW).
			Translate(scrollX, 0).Background(be.headerBg).AlignItems(ui.Stretch)
		if scrollX > 0.5 {
			frozen.BorderWidth(0, 1, 0, 0).BorderColor(be.border).Shadow(2, 0, 8, 0, pal.ShadowEdge)
		}
		frozen.Children(func() {
			renderSelectAllGutter(c, st, res, selectedCount, rowTotal, be)
			if freezeFirst {
				renderColumnHeaderCell(c, st, view, db, frozenCol, be, opts, pal)
			}
		})
	})
}

func renderSelectAllGutter(
	c *ui.Context,
	st *State,
	res *database.QueryResult,
	selectedCount, rowTotal int,
	be beTableColors,
) {
	allSelected := rowTotal > 0 && selectedCount == rowTotal
	indeterminate := !allSelected && selectedCount > 0

	gut := ui.Row(c.Key("gutter-all")).Width(tableGutterW).Shrink(0).Center()
	gutTarget := float32(0.50)
	if gut.Hovered() || allSelected || indeterminate {
		gutTarget = 1
	}
	gutVis := gut.AnimateTo("vis", gutTarget, ui.Animation{Duration: 120 * time.Millisecond, Ease: dbEaseOut, NonMotion: true})
	gut.Opacity(gutVis).Children(func() {
		cb := renderBeuiCheckbox(c, "sel-all", &allSelected, indeterminate, 18, 5.5, rowTotal == 0, i18n.T("全选"), be)
		if cb.Changed() && res != nil {
			if allSelected {
				for _, r := range res.Rows {
					if r != nil {
						st.SelectedIDs[r.ID] = true
					}
				}
			} else {
				st.ClearSelection()
			}
		}
	})
}

func renderColumnHeaderCell(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	col tableCol,
	be beTableColors,
	opts Options,
	pal Palette,
) {
	prop := col.prop
	sortDesc, isSorted := sortDirectionFor(view, prop.ID)
	renaming := st.RenamingPropID == prop.ID
	colOpen := st.TableColMenuOpen && st.TableColMenuPropID == prop.ID

	cell := ui.Box(c.Key("hdr:" + prop.ID)).Width(col.width).Shrink(0).Height(tableHeaderH).
		Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
	hovered := cell.Hovered() || colOpen
	colDragging := st.Motion.isDragging(dragKindCol, prop.ID)
	if colDragging {
		cell.Opacity(0.35)
	} else if hovered {
		cell.Background(be.rowHover)
	}

	if prop.Type != database.PropTitle && !opts.ReadOnly {
		if d, ok := ui.Drop[colDrag](cell); ok && d.propID != prop.ID {
			reorderViewColumn(st, view, db, d.propID, prop.ID, opts)
		}
		if d, over := ui.DragOver[colDrag](cell); over && d.propID != prop.ID {
			cell.DrawOver(func(p *ui.Painter, r ui.Rect) {
				paintDropRail(p, r, be.primary, true)
			})
		}
	}

	buildColMenu := func(m *ui.Menu) {
		if !opts.ReadOnly {
			if m.Item(i18n.T("在左侧插入列")).Chosen() {
				insertColumnRelative(st, view, db, prop.ID, -1, opts)
			}
			if m.Item(i18n.T("在右侧插入列")).Chosen() {
				insertColumnRelative(st, view, db, prop.ID, 1, opts)
			}
			m.Separator()
			if m.Item(i18n.T("重命名属性…")).Chosen() {
				st.RenamingPropID = prop.ID
				st.RenamingPropText = prop.Name
				st.RenamingPropPending = true
			}
			if prop.Type == database.PropRelation || prop.Type == database.PropRollup || prop.Type == database.PropFormula || prop.Type == database.PropNumber {
				if m.Item(i18n.T("编辑属性配置…")).Chosen() {
					st.OpenPropertyConfig(prop)
				}
			}
			if prop.Type != database.PropTitle {
				m.Submenu(i18n.T("修改属性类型"), func(sub *ui.Menu) {
					for _, pt := range creatablePropertyTypes() {
						cand := pt
						if sub.Item(propertyTypeLabel(cand)).Checked(prop.Type == cand).Chosen() {
							updated := prop.Clone()
							updated.Type = cand
							if cand == database.PropDate && updated.Date == nil {
								updated.Date = &database.DateConfig{EndKey: updated.Key + "_end"}
							}
							affected, err := st.Store.UpdateProperty(updated)
							if err == nil && opts.OnChanged != nil {
								opts.OnChanged(affected)
							}
						}
					}
				})
			}
			m.Separator()
		}
		if m.Item(i18n.T("升序排序")).Checked(isSorted && !sortDesc).Chosen() {
			vc := view.Clone()
			vc.Sorts = []database.SortRule{{PropertyID: prop.ID, Desc: false}}
			saveViewAndNotify(st, vc, opts)
		}
		if m.Item(i18n.T("降序排序")).Checked(isSorted && sortDesc).Chosen() {
			vc := view.Clone()
			vc.Sorts = []database.SortRule{{PropertyID: prop.ID, Desc: true}}
			saveViewAndNotify(st, vc, opts)
		}
		if isSorted {
			if m.Item(i18n.T("清除排序")).Chosen() {
				vc := view.Clone()
				vc.Sorts = nil
				saveViewAndNotify(st, vc, opts)
			}
		}
		if m.Item(i18n.T("按此列分组")).Chosen() {
			vc := view.Clone()
			vc.Group = &database.GroupConfig{PropertyID: prop.ID}
			if prop.Type == database.PropStatus {
				vc.Group.StatusBy = "option"
			} else if prop.Type == database.PropDate || prop.Type == database.PropCreatedTime || prop.Type == database.PropEditedTime {
				vc.Group.DateBy = "month"
			}
			saveViewAndNotify(st, vc, opts)
		}
		if prop.Type != database.PropTitle && !opts.ReadOnly {
			m.Separator()
			wrapLabel := i18n.T("开启列内容换行")
			if col.wrap {
				wrapLabel = i18n.T("关闭列内容换行")
			}
			if m.Item(wrapLabel).Chosen() {
				toggleViewColumnWrap(st, view, db, prop.ID, opts)
			}
			if m.Item(i18n.T("隐藏此列")).Chosen() {
				hideViewColumn(st, view, db, prop.ID, opts)
			}
			if m.Item(i18n.T("删除属性")).Chosen() {
				affected, err := st.Store.DeleteProperty(prop.ID)
				if err == nil && opts.OnChanged != nil {
					opts.OnChanged(affected)
				}
			}
		}
	}

	cell.Children(func() {
		narrow := col.width < 100
		innerPadL, innerPadR, innerGap := float32(4), float32(8), float32(2)
		gripW, gripIconSize := float32(18), float32(12.5)
		sortPadX, sortGap := float32(6), float32(4)
		if narrow {
			innerPadL, innerPadR, innerGap = 2, 4, 0
			gripW, gripIconSize = 13, 11
			sortPadX, sortGap = 3, 2
		}

		inner := ui.Row(c).Height(tableHeaderH).Padding(0, innerPadR, 0, innerPadL).Gap(innerGap).AlignItems(ui.Center)
		inner.Children(func() {
			// Quiet drag grip: fades in cleanly on column hover (keeps header clutter-free).
			canDragCol := prop.Type != database.PropTitle && !opts.ReadOnly
			grip := ui.Box(c.Key("cgrip")).Width(gripW).Height(tableHeaderH).Shrink(0).Center()
			if canDragCol {
				grip.Cursor(ui.CursorGrab).Drag(colDrag{propID: prop.ID})
				st.Motion.trackDrag(c, grip, dragKindCol, prop.ID, prop.Name, "", be.primary, true)
			}
			gripTarget := float32(0)
			if canDragCol && (hovered || grip.Hovered() || colDragging) {
				gripTarget = 1
			}
			gripVis := grip.AnimateTo("vis", gripTarget, ui.Animation{
				Duration:  120 * time.Millisecond,
				Ease:      dbEaseOut,
				NonMotion: true,
			})
			gripInk := be.grip
			if grip.Hovered() {
				gripInk = pal.Text
			}
			if gripVis > 0.02 {
				grip.Children(func() {
					ui.Icon(c, iconGrip).FontSize(gripIconSize).TextColor(gripInk.Alpha(gripVis))
				})
			}

			if renaming {
				in := ui.TextInput(c.Key("hdr-ren"), &st.RenamingPropText).Grow(1).MinWidth(0).Height(28).
					Padding(2, 6).Radius(6).Background(be.cardBg).Border(1, be.primary).
					TextColor(pal.Text).AutoFocus().Label(i18n.T("属性名称"))
				if st.RenamingPropPending {
					in.Focus()
					st.RenamingPropPending = false
				}
				if in.Shortcut(0, ui.KeyEscape) {
					st.RenamingPropID = ""
				} else if in.Submitted() || (!in.Focused() && !st.RenamingPropPending) {
					nextName := strings.TrimSpace(st.RenamingPropText)
					st.RenamingPropID = ""
					if nextName != "" && nextName != prop.Name {
						updated := prop.Clone()
						updated.Name = nextName
						_, _ = st.Store.UpdateProperty(updated)
						if opts.OnChanged != nil {
							opts.OnChanged(nil)
						}
					}
				}
			} else {
				sortBtn := ui.Row(c.Key("sort")).Grow(1).MinWidth(0).Height(tableHeaderH).Padding(0, sortPadX).
					Gap(sortGap).AlignItems(ui.Center).Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(prop.Name)
				if prop.Type == database.PropNumber {
					sortBtn.Justify(ui.End)
				}
				titleInk := pal.TextMuted
				if isSorted || hovered || sortBtn.Hovered() {
					titleInk = pal.Text
				}
				sortBtn.Children(func() {
					ui.Text(c, prop.Name).FontSize(12.5).FontWeight(500).TextColor(titleInk).SingleLine()
					chevWrap := ui.Box(c.Key("chev")).Size(13, 13).Shrink(0).Center()
					rot := float32(0)
					if isSorted && sortDesc {
						rot = 180
					}
					chevWrap.AnimatePose(ui.Pose{Rotate: rot}, ui.Animation{
						Duration: 180 * time.Millisecond,
						Ease:     dbEaseOut,
					})
					chevTarget := float32(0)
					if isSorted {
						chevTarget = 1
					} else if hovered || sortBtn.Hovered() {
						chevTarget = 0.6
					}
					chevVis := chevWrap.AnimateTo("vis", chevTarget, ui.Animation{
						Duration:  120 * time.Millisecond,
						Ease:      dbEaseOut,
						NonMotion: true,
					})
					chevCol := be.chevronMuted
					if isSorted {
						chevCol = be.primary
					}
					if chevVis > 0.02 {
						chevWrap.Children(func() {
							ui.Icon(c, iconChevronUp).FontSize(12.5).TextColor(chevCol.Alpha(chevVis))
						})
					}
				})
				if sortBtn.Clicked() {
					cycleColumnSort(st, view, prop.ID, opts)
				}
			}
		})

		// Top-edge centered floating blue pill menu handle matching beUI ColumnHandle.
		if !renaming {
			visTarget := float32(0)
			if hovered || colOpen {
				visTarget = 1
			}
			colHandle := ui.Box(c.Key("chnd:"+prop.ID)).Absolute().Left(max(4, (col.width-24)/2)).Top(1).
				Size(24, 10).Radius(999).Background(be.primary).Center().Cursor(ui.CursorPointer).
				Role(ui.RoleMenuButton).Label(i18n.T("列选项") + ": " + prop.Name)
			vis := colHandle.AnimateTo("vis", visTarget, ui.Animation{
				Duration:  120 * time.Millisecond,
				Ease:      beuiEaseOut,
				NonMotion: true,
			})
			colHandle.Opacity(vis).IgnoreInput(!(hovered || colOpen))
			if colHandle.Hovered() || colOpen {
				colHandle.Background(be.primary.Mix(ui.RGB(255, 255, 255), 0.14))
			}
			colHandle.Children(func() {
				ui.Icon(c, iconMoreHoriz).FontSize(10).TextColor(be.primaryFg)
			})
			colHandle.PopoverMenuButton(buildColMenu, ui.PopoverMenuButtonOptions{Open: &colOpen})
			if colOpen {
				st.TableColMenuPropID = prop.ID
				st.TableColMenuOpen = true
			} else if st.TableColMenuPropID == prop.ID {
				st.TableColMenuOpen = false
				st.TableColMenuPropID = ""
			}
		}

		// Right-edge column width resize grip.
		resizeGrip := ui.Box(c.Key("resize:" + prop.ID)).Absolute().Right(0).Top(0).Width(6).Height(tableHeaderH).
			Cursor(ui.CursorResizeEW)
		dx, _, held := resizeGrip.Dragged()
		if resizeGrip.Hovered() || held {
			barCol := be.resizeHover
			if held {
				barCol = be.primary
			}
			resizeGrip.DrawOver(func(p *ui.Painter, r ui.Rect) {
				p.Fill(ui.Rect{X: r.X + r.W - 2, Y: r.Y + 8, W: 2, H: r.H - 16}, barCol, 1)
			})
		}
		if held && dx != 0 {
			nextW := min(max(col.width+dx, 72), 600)
			setViewColumnWidth(st, view, db, prop.ID, nextW, opts)
		}
	})

	if !renaming {
		cell.ContextMenu(buildColMenu)
	}
}

func renderGroupHeaderRow(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	g *database.RecordGroup,
	contentW, scrollX float32,
	opts Options,
	pal Palette,
) {
	renderTableGroupHeaderRow(c, st, view, g, contentW, scrollX, beTablePalette(pal), opts, pal)
}

func renderTableGroupHeaderRow(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	g *database.RecordGroup,
	contentW, scrollX float32,
	be beTableColors,
	opts Options,
	pal Palette,
) {
	if g == nil {
		return
	}
	row := ui.Box(c.Key("gh:"+g.Key)).Width(contentW).Height(38).Background(be.headerBg).
		BorderWidth(0, 0, 1, 0).BorderColor(be.rowBorder).
		Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
	row.Children(func() {
		inner := ui.Row(c.Key("gh-in")).Absolute().Left(0).Top(0).Bottom(0).Width(420).
			Translate(scrollX, 0).Padding(0, 14).Gap(8).AlignItems(ui.Center).Cursor(ui.CursorPointer)
		if inner.Hovered() {
			row.Background(be.rowHover)
		}
		inner.Children(func() {
			renderAnimatedChevron(c, "gh-chev:"+g.Key, g.Collapsed, 13, pal.TextMuted)
			if g.Color != "" && g.Color != "default" {
				renderBeuiPill(c, g.Label, g.Color, nil, pal)
			} else {
				ui.Text(c, g.Label).FontSize(13).FontWeight(600).TextColor(pal.Text).SingleLine()
			}
			ui.Text(c, fmt.Sprintf("%d", g.Count)).FontSize(12).TextColor(be.metaInk).SingleLine()
		})
		if inner.Clicked() && view.Group != nil {
			vc := view.Clone()
			vc.Group.Collapsed = toggleStringInSlice(slices.Clone(vc.Group.Collapsed), g.Key)
			saveViewAndNotify(st, vc, opts)
		}
	})
}

func renderEmptyTableRow(
	c *ui.Context,
	contentW, scrollX float32,
	be beTableColors,
	pal Palette,
) {
	row := ui.Box(c.Key("table-empty")).Width(contentW).Padding(36, 16).
		Background(be.cardBg).BorderWidth(0, 0, 1, 0).BorderColor(be.rowBorder)
	row.Children(func() {
		ui.Row(c).Translate(scrollX, 0).Center().Children(func() {
			ui.Text(c, i18n.T("暂无记录")).FontSize(13.5).TextColor(pal.TextMuted)
		})
	})
}

func renderAddRow(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	groupProp string,
	groupVal database.Value,
	contentW, scrollX float32,
	be beTableColors,
	opts Options,
) {
	if opts.ReadOnly {
		return
	}
	row := ui.Box(c.Key("add-row:" + groupProp + ":" + groupVal.PlainString())).
		Width(contentW).Height(40).Background(be.cardBg).
		Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
	addHovered := row.Hovered()
	if addHovered {
		row.Background(be.rowHover)
	}
	row.Children(func() {
		inner := ui.Row(c).Absolute().Left(0).Top(0).Bottom(0).Width(200).Translate(scrollX, 0).
			Padding(0, 16).Gap(6).AlignItems(ui.Center).Cursor(ui.CursorPointer)
		ink := be.metaInk
		if addHovered || inner.Hovered() {
			ink = be.primary
		}
		inner.Children(func() {
			ui.Icon(c, iconPlus).FontSize(13).TextColor(ink)
			ui.Text(c, i18n.T("新建")).FontSize(12.5).FontWeight(500).TextColor(ink).SingleLine()
		})
		if row.Clicked() || inner.Clicked() {
			createRecordWithDefaults(st, db, groupProp, groupVal, opts)
		}
	})
}

func renderRecordRow(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	it tableItem,
	cols []tableCol,
	frozenCol tableCol,
	rightCols []tableCol,
	freezeFirst bool,
	frozenW, contentW, scrollX, rowH float32,
	rIdx int,
	res *database.QueryResult,
	be beTableColors,
	opts Options,
	pal Palette,
) int {
	rec := it.record
	if rec == nil {
		return 0
	}
	tabDir := 0
	selected := st.SelectedIDs[rec.ID]
	tintBg, tintAccent, tinted := tintedRowBackground(pal, res, rec.ID)
	rowOpen := st.TableRowMenuOpen && st.TableRowMenuRecordID == rec.ID

	rowBg := be.cardBg
	if tinted {
		rowBg = tintBg
	}
	if selected {
		rowBg = be.rowSelected
	}

	row := ui.Box(c.Key("r:"+rec.ID)).Width(contentW).MinHeight(rowH).
		Background(rowBg).BorderWidth(0, 0, 1, 0).BorderColor(be.rowBorder)
	hovered := row.Hovered() || rowOpen
	rowDragging := st.Motion.isDragging(dragKindRow, rec.ID)
	rowSettling := st.Motion.isSettling(dragKindRow, rec.ID)
	if rowDragging {
		row.Opacity(0.28)
	} else if rowSettling {
		row.Opacity(0.18)
	}
	if hovered && !selected && !rowDragging {
		if tinted {
			rowBg = tintBg.Mix(pal.Text, 0.03)
		} else {
			rowBg = be.rowHover
		}
		row.Background(rowBg)
	}
	pulse := st.Motion.pulseAlpha(c, rec.ID)
	if pulse > 0.01 {
		row.DrawOver(func(p *ui.Painter, r ui.Rect) {
			paintCreationPulse(p, r, 0, pulse, be.primary)
		})
	}
	if st.Motion.IsRecentlyCreated(rec.ID, c.Now()) {
		row.Transition(ui.ElementTransition{
			Duration: 200 * time.Millisecond,
			Ease:     dbEaseOut,
			Colors:   !tinted,
			Enter:    &ui.Motion{Y: -6, Opacity: 0.72},
		})
	} else if !tinted {
		row.Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
	}

	// Support drag-to-reorder rows when view has no active Sort rules.
	if len(view.Sorts) == 0 && !opts.ReadOnly {
		if d, ok := ui.Drop[rowDrag](row); ok && d.recordID != rec.ID {
			reorderRecordRow(st, view, res, d.recordID, rec.ID, opts)
		}
		if d, over := ui.DragOver[rowDrag](row); over && d.recordID != rec.ID {
			row.DrawOver(func(p *ui.Painter, r ui.Rect) {
				paintDropRail(p, r, be.primary, false)
			})
		}
	}

	openMode := view.OpenMode
	if openMode == "" {
		openMode = OpenSidePeek
	}

	buildRowMenu := func(m *ui.Menu) {
		if !opts.ReadOnly {
			if m.Item(i18n.T("在上方插入行")).Chosen() {
				insertRecordRelative(st, view, db, res, rec, it.groupProp, it.groupValue, -1, opts)
			}
			if m.Item(i18n.T("在下方插入行")).Chosen() {
				insertRecordRelative(st, view, db, res, rec, it.groupProp, it.groupValue, 1, opts)
			}
			m.Separator()
		}
		if opts.OnOpenRecord != nil {
			if m.Item(i18n.T("在侧边栏预览")).Chosen() {
				opts.OnOpenRecord(rec, OpenSidePeek)
			}
			if m.Item(i18n.T("居中弹窗打开")).Chosen() {
				opts.OnOpenRecord(rec, OpenCenterPeek)
			}
			if rec.Path != "" {
				if m.Item(i18n.T("整页打开")).Chosen() {
					opts.OnOpenRecord(rec, OpenFullPage)
				}
			}
			if !opts.ReadOnly {
				m.Separator()
			}
		}
		if !opts.ReadOnly {
			if m.Item(i18n.T("删除行")).Chosen() {
				trashSingleRecord(st, rec, opts)
			}
		}
	}
	row.ContextMenu(buildRowMenu)

	row.Children(func() {
		// Right scrollable columns (borderless vertical columns matching beUI).
		ui.Row(c).PaddingStart(frozenW).MinHeight(rowH).AlignItems(ui.Stretch).Children(func() {
			colOffset := 0
			if freezeFirst {
				colOffset = 1
			}
			for j, col := range rightCols {
				cIdx := colOffset + j
				focused := st.FocusRow == rIdx && st.FocusCol == cIdx
				wrap := col.wrap || (view.Table != nil && view.Table.WrapAll)
				cellBox := ui.Row(c.Key("rc:" + col.prop.ID)).Width(col.width).Shrink(0).MinHeight(rowH).
					AlignItems(ui.Stretch).Clip()
				cellBox.Children(func() {
					out := tableCellView(c, st, rec, col.prop, focused, wrap, false, rowH, be, opts, pal)
					if out.clicked {
						st.FocusRow, st.FocusCol = rIdx, cIdx
					}
					if out.tabDir != 0 {
						tabDir = out.tabDir
					}
				})
			}
			// Spacer under "+" header column.
			ui.Box(c).Width(tableAddColW).Shrink(0)
		})

		// Frozen gutter + Title cell.
		frozen := ui.Row(c.Key("fz")).Absolute().Left(0).Top(0).Bottom(0).Width(frozenW).
			Translate(scrollX, 0).Background(rowBg).AlignItems(ui.Stretch)
		if !tinted {
			frozen.Transition(ui.ElementTransition{Duration: 140 * time.Millisecond, Colors: true})
		}
		if scrollX > 0.5 {
			frozen.BorderWidth(0, 1, 0, 0).BorderColor(be.border).Shadow(2, 0, 8, 0, pal.ShadowEdge)
		}
		if tinted && !selected {
			frozen.DrawOver(func(p *ui.Painter, r ui.Rect) {
				p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 3, H: r.H}, tintAccent, 0)
			})
		}
		frozen.Children(func() {
			// Gutter: subtle quiet checkbox (fades to full contrast on hover/select) + vertical row pill handle.
			gut := ui.Box(c.Key("gut")).Width(tableGutterW).Height(rowH).Shrink(0)
			gut.Children(func() {
				cbWrap := ui.Box(c.Key("cbw")).Fill().Center()
				cbTarget := float32(0.42)
				if hovered || selected || len(st.SelectedIDs) > 0 {
					cbTarget = 1
				}
				cbVis := cbWrap.AnimateTo("vis", cbTarget, ui.Animation{
					Duration:  120 * time.Millisecond,
					Ease:      dbEaseOut,
					NonMotion: true,
				})
				cbWrap.Opacity(cbVis).Children(func() {
					cb := renderBeuiCheckbox(c, "sel", &selected, false, 18, 5.5, false, rec.Title, be)
					if cb.Changed() {
						if selected {
							st.SelectedIDs[rec.ID] = true
						} else {
							delete(st.SelectedIDs, rec.ID)
						}
					}
				})

				if !opts.ReadOnly {
					rVisTarget := float32(0)
					if hovered || rowOpen || rowDragging {
						rVisTarget = 1
					}
					rowHandle := ui.Box(c.Key("rhnd")).Absolute().Left(2).Top((rowH-22)/2).
						Size(8, 22).Radius(999).Background(be.primary).Center().Cursor(ui.CursorGrab).
						Role(ui.RoleMenuButton).Label(i18n.T("行选项") + ": " + rec.Title)
					rVis := rowHandle.AnimateTo("vis", rVisTarget, ui.Animation{
						Duration:  120 * time.Millisecond,
						Ease:      dbEaseOut,
						NonMotion: true,
					})
					rowHandle.Opacity(rVis).IgnoreInput(!(hovered || rowOpen || rowDragging))
					if rowHandle.Hovered() || rowOpen {
						rowHandle.Background(be.primary.Mix(ui.RGB(255, 255, 255), 0.14))
					}
					if len(view.Sorts) == 0 {
						rowHandle.Drag(rowDrag{recordID: rec.ID})
						st.Motion.trackDrag(c, rowHandle, dragKindRow, rec.ID, rec.Title, "", be.primary, true)
					}
					rowHandle.Children(func() {
						ui.Icon(c, iconMoreVert).FontSize(10).TextColor(be.primaryFg)
					})
					rowHandle.PopoverMenuButton(buildRowMenu, ui.PopoverMenuButtonOptions{Open: &rowOpen})
					if rowOpen {
						st.TableRowMenuRecordID = rec.ID
						st.TableRowMenuOpen = true
					} else if st.TableRowMenuRecordID == rec.ID {
						st.TableRowMenuOpen = false
						st.TableRowMenuRecordID = ""
					}
				}
			})

			if freezeFirst {
				focused := st.FocusRow == rIdx && st.FocusCol == 0
				wrap := frozenCol.wrap || (view.Table != nil && view.Table.WrapAll)
				titleWrap := ui.Row(c.Key("title-cell")).Width(frozenCol.width).Shrink(0).
					AlignItems(ui.Center).Padding(0, 6, 0, 0).Clip()
				if rowSettling {
					st.Motion.trackDrag(c, titleWrap, dragKindRow, rec.ID, rec.Title, "", be.primary, true)
				}
				titleWrap.Children(func() {
					if (view.Table == nil || view.Table.ShowIcon) && rec.Icon != "" {
						ui.Text(c, rec.Icon).FontSize(14).Padding(0, 0, 0, 12)
					}
					out := tableCellView(c, st, rec, frozenCol.prop, focused, wrap, false, rowH, be, opts, pal)
					if out.clicked {
						st.FocusRow, st.FocusCol = rIdx, 0
					}
					if out.tabDir != 0 {
						tabDir = out.tabDir
					}
					if opts.OnOpenRecord != nil && (hovered || focused) && !(st.Editing && st.EditRecordID == rec.ID && st.EditPropID == frozenCol.prop.ID) {
						openBtn := ui.ButtonBase(c.Key("open-rec")).Height(24).Padding(0, 8).Radius(6).
							Background(be.headerBg).Border(1, be.border).Cursor(ui.CursorPointer).
							AlignItems(ui.Center).Role(ui.RoleButton).Label(i18n.T("打开")).
							Tooltip(i18n.T("打开记录")).
							Transition(ui.ElementTransition{
								Duration: 130 * time.Millisecond,
								Ease:     dbEaseOut,
								Colors:   true,
								Enter:    &ui.Motion{X: 4, Opacity: 0},
							})
						if openBtn.Hovered() {
							openBtn.Background(be.rowSelected).Border(1, be.primary.Alpha(0.45))
						}
						applyPressSpring(openBtn, false)
						openBtn.Children(func() {
							ui.Text(c, i18n.T("打开")).FontSize(11).FontWeight(500).TextColor(pal.Text).SingleLine()
						})
						if openBtn.Clicked() {
							opts.OnOpenRecord(rec, openMode)
						}
					}
				})
			}
		})
	})

	return tabDir
}

func renderTableFooterRow(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	frozenCol tableCol,
	rightCols []tableCol,
	freezeFirst bool,
	frozenW, contentW, scrollX float32,
	res *database.QueryResult,
	be beTableColors,
	opts Options,
	pal Palette,
) {
	foot := ui.Box(c.Key("table-foot")).Width(contentW).Height(tableFooterH).Shrink(0).
		Background(be.footerBg).BorderWidth(1, 0, 0, 0).BorderColor(be.border).Clip()
	foot.Children(func() {
		ui.Row(c).PaddingStart(frozenW).Height(tableFooterH).AlignItems(ui.Stretch).Children(func() {
			for _, col := range rightCols {
				renderAggFooterCell(c, st, view, col, res, be, opts, pal)
			}
			ui.Box(c).Width(tableAddColW).Shrink(0)
		})

		frozen := ui.Row(c.Key("frozen-foot")).Absolute().Left(0).Top(0).Bottom(0).Width(frozenW).
			Translate(scrollX, 0).Background(be.footerBg).AlignItems(ui.Stretch)
		if scrollX > 0.5 {
			frozen.BorderWidth(0, 1, 0, 0).BorderColor(be.border).Shadow(2, 0, 8, 0, pal.ShadowEdge)
		}
		frozen.Children(func() {
			ui.Box(c).Width(tableGutterW).Shrink(0)
			if freezeFirst {
				renderAggFooterCell(c, st, view, frozenCol, res, be, opts, pal)
			}
		})
	})
}

func renderAggFooterCell(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	col tableCol,
	res *database.QueryResult,
	be beTableColors,
	opts Options,
	pal Palette,
) {
	prop := col.prop
	curAgg := database.AggNone
	if view.Table != nil && view.Table.Aggregations != nil {
		if a, ok := view.Table.Aggregations[prop.ID]; ok {
			curAgg = a
		}
	}

	label := ""
	if res != nil {
		if aggRes, ok := res.Aggregations[prop.ID]; ok && aggRes.HasValue {
			label = aggregationShortPrefix(curAgg) + aggRes.Display
		}
	}

	cell := ui.Row(c.Key("agg:"+prop.ID)).Width(col.width).Shrink(0).Height(tableFooterH).
		Padding(0, 14).AlignItems(ui.Center).Cursor(ui.CursorPointer).
		Role(ui.RoleMenuButton).Label(i18n.T("列统计"))
	if cell.Hovered() {
		cell.Background(be.rowHover)
	}
	cell.Children(func() {
		if label != "" {
			ui.Spacer(c)
			ui.Text(c, label).FontSize(12).TextColor(be.metaInk).SingleLine()
		} else if cell.Hovered() {
			ui.Spacer(c)
			ui.Text(c, i18n.T("计算")).FontSize(11.5).TextColor(be.metaInk).SingleLine()
		}
	})
	cell.PopoverMenuButton(func(m *ui.Menu) {
		for _, agg := range aggregationsForType(prop.Type) {
			cand := agg
			if m.Item(aggregationLabel(cand)).Checked(curAgg == cand || (cand == database.AggNone && curAgg == "")).Chosen() {
				vc := view.Clone()
				if vc.Table == nil {
					vc.Table = &database.TableViewConfig{FreezeFirst: true, ShowIcon: true}
				}
				if vc.Table.Aggregations == nil {
					vc.Table.Aggregations = make(map[string]database.AggregationType)
				}
				if cand == database.AggNone {
					delete(vc.Table.Aggregations, prop.ID)
				} else {
					vc.Table.Aggregations[prop.ID] = cand
				}
				saveViewAndNotify(st, vc, opts)
			}
		}
	})
}

func aggregationShortPrefix(agg database.AggregationType) string {
	switch agg {
	case database.AggCountAll:
		return i18n.T("共 ")
	case database.AggSum:
		return i18n.T("和 ")
	case database.AggAverage:
		return i18n.T("均 ")
	case database.AggMedian:
		return i18n.T("中位 ")
	case database.AggMin:
		return i18n.T("最小 ")
	case database.AggMax:
		return i18n.T("最大 ")
	case database.AggRange:
		return i18n.T("差 ")
	case database.AggChecked:
		return i18n.T("已选 ")
	case database.AggUnchecked:
		return i18n.T("未选 ")
	default:
		return ""
	}
}

func aggregationsForType(pt database.PropertyType) []database.AggregationType {
	common := []database.AggregationType{
		database.AggNone,
		database.AggCountAll,
		database.AggCountValues,
		database.AggCountUnique,
		database.AggCountEmpty,
		database.AggCountNotEmpty,
		database.AggPercentEmpty,
		database.AggPercentNotEmpty,
	}
	switch pt {
	case database.PropNumber:
		return append(common, database.AggSum, database.AggAverage, database.AggMedian, database.AggMin, database.AggMax, database.AggRange)
	case database.PropCheckbox:
		return append(common, database.AggChecked, database.AggUnchecked, database.AggPercentChecked, database.AggPercentUnchecked)
	case database.PropDate, database.PropCreatedTime, database.PropEditedTime:
		return append(common, database.AggEarliestDate, database.AggLatestDate, database.AggDateRange)
	default:
		return common
	}
}

func buildTableItems(view database.ViewConfig, res *database.QueryResult) []tableItem {
	if res == nil {
		return nil
	}
	if len(res.Groups) == 0 {
		out := make([]tableItem, 0, len(res.Rows)+2)
		for _, r := range res.Rows {
			if r != nil {
				out = append(out, tableItem{key: "rec:" + r.ID, kind: itemRecord, record: r})
			}
		}
		if len(out) == 0 {
			out = append(out, tableItem{key: "empty:root", kind: itemEmptyState})
		}
		out = append(out, tableItem{key: "add:root", kind: itemAddRow})
		return out
	}

	groupPropID := ""
	if view.Group != nil {
		groupPropID = view.Group.PropertyID
	}
	var out []tableItem
	for i := range res.Groups {
		g := &res.Groups[i]
		out = append(out, tableItem{key: "grp:" + g.Key, kind: itemGroupHeader, group: g})
		if !g.Collapsed {
			for _, r := range g.Rows {
				if r != nil {
					out = append(out, tableItem{
						key:        "rec:" + g.Key + ":" + r.ID,
						kind:       itemRecord,
						group:      g,
						record:     r,
						groupProp:  groupPropID,
						groupValue: g.Value,
					})
				}
			}
			out = append(out, tableItem{
				key:        "add:" + g.Key,
				kind:       itemAddRow,
				groupProp:  groupPropID,
				groupValue: g.Value,
			})
		}
	}
	return out
}

func visibleTableColumns(db *database.Database, view *database.ViewConfig) []tableCol {
	if db == nil {
		return nil
	}
	var cols []tableCol
	seen := make(map[string]bool, len(db.Properties))
	if view != nil {
		for _, vp := range view.Properties {
			if !vp.Visible {
				continue
			}
			if p, idx := db.PropertyByID(vp.PropertyID); idx >= 0 && !seen[p.ID] {
				seen[p.ID] = true
				cols = append(cols, makeTableCol(*p, vp))
			}
		}
	}
	if len(cols) == 0 {
		for _, p := range db.Properties {
			seen[p.ID] = true
			cols = append(cols, makeTableCol(p, database.ViewProperty{
				PropertyID: p.ID,
				Visible:    true,
				Width:      p.Width,
				Wrap:       p.Wrap,
			}))
		}
	} else if tp := db.TitleProperty(); tp != nil && !seen[tp.ID] {
		cols = append([]tableCol{makeTableCol(*tp, database.ViewProperty{
			PropertyID: tp.ID,
			Visible:    true,
			Width:      tp.Width,
			Wrap:       tp.Wrap,
		})}, cols...)
	}
	return cols
}

func makeTableCol(prop database.Property, vp database.ViewProperty) tableCol {
	w := vp.Width
	if w <= 0 {
		w = prop.Width
	}
	if w <= 0 {
		w = defaultColumnWidth(prop.Type)
	}
	return tableCol{
		prop:  prop,
		vp:    vp,
		width: w,
		wrap:  vp.Wrap || prop.Wrap,
	}
}

func defaultColumnWidth(pt database.PropertyType) float32 {
	switch pt {
	case database.PropTitle:
		return 260
	case database.PropCheckbox:
		return 96
	case database.PropNumber:
		return 112
	case database.PropSelect, database.PropStatus, database.PropUniqueID:
		return 128
	case database.PropDate, database.PropCreatedTime, database.PropEditedTime:
		return 216
	case database.PropMultiSelect, database.PropPerson, database.PropRelation, database.PropFiles:
		return 176
	default:
		return 180
	}
}

func sortDirectionFor(view *database.ViewConfig, propID string) (desc, active bool) {
	if view == nil {
		return false, false
	}
	for _, sr := range view.Sorts {
		if sr.PropertyID == propID {
			return sr.Desc, true
		}
	}
	return false, false
}

func moveTableFocus(row, col *int, nRows, nCols, dir int) {
	if nRows <= 0 || nCols <= 0 {
		return
	}
	if *row < 0 {
		*row, *col = 0, 0
		return
	}
	if *col < 0 {
		*col = 0
	}
	*col += dir
	if *col >= nCols {
		if *row+1 < nRows {
			*row++
			*col = 0
		} else {
			*col = nCols - 1
		}
	} else if *col < 0 {
		if *row > 0 {
			*row--
			*col = nCols - 1
		} else {
			*col = 0
		}
	}
}

func activateCellEdit(st *State, rec *database.Record, prop database.Property, opts Options) {
	if st == nil || rec == nil || opts.ReadOnly {
		return
	}
	switch {
	case prop.Type == database.PropCheckbox:
		val := rec.ValueFor(prop)
		cur := val.Kind == database.ValBool && val.Bool
		st.setCellAndNotify(rec.ID, prop.ID, database.BoolValue(!cur), opts)
	case isInlineTextProp(prop.Type):
		val := rec.ValueFor(prop)
		initial := ""
		if prop.Type == database.PropNumber && val.Kind == database.ValNumber {
			initial = database.FormatValue(database.Property{Type: database.PropText}, val)
		} else if !val.IsEmpty() {
			initial = val.PlainString()
		}
		st.StartEditCell(rec.ID, prop.ID, initial)
	case isPopoverProp(prop.Type):
		st.OpenCellPopover(rec, prop, opts.Now)
	}
}

func setViewColumnWidth(st *State, view *database.ViewConfig, db *database.Database, propID string, width float32, opts Options) {
	vc := view.Clone()
	if len(vc.Properties) == 0 {
		vc.Properties = extractViewProps(orderedViewProperties(db, &vc))
	}
	for i := range vc.Properties {
		if vc.Properties[i].PropertyID == propID {
			vc.Properties[i].Width = width
			break
		}
	}
	saveViewAndNotify(st, vc, opts)
}

func toggleViewColumnWrap(st *State, view *database.ViewConfig, db *database.Database, propID string, opts Options) {
	vc := view.Clone()
	if len(vc.Properties) == 0 {
		vc.Properties = extractViewProps(orderedViewProperties(db, &vc))
	}
	for i := range vc.Properties {
		if vc.Properties[i].PropertyID == propID {
			vc.Properties[i].Wrap = !vc.Properties[i].Wrap
			break
		}
	}
	saveViewAndNotify(st, vc, opts)
}

func hideViewColumn(st *State, view *database.ViewConfig, db *database.Database, propID string, opts Options) {
	vc := view.Clone()
	if len(vc.Properties) == 0 {
		vc.Properties = extractViewProps(orderedViewProperties(db, &vc))
	}
	for i := range vc.Properties {
		if vc.Properties[i].PropertyID == propID {
			vc.Properties[i].Visible = false
			break
		}
	}
	saveViewAndNotify(st, vc, opts)
}

func reorderViewColumn(st *State, view *database.ViewConfig, db *database.Database, fromPropID, toPropID string, opts Options) {
	vc := view.Clone()
	if len(vc.Properties) == 0 {
		vc.Properties = extractViewProps(orderedViewProperties(db, &vc))
	}
	fromIdx := slices.IndexFunc(vc.Properties, func(vp database.ViewProperty) bool { return vp.PropertyID == fromPropID })
	toIdx := slices.IndexFunc(vc.Properties, func(vp database.ViewProperty) bool { return vp.PropertyID == toPropID })
	if fromIdx < 0 || toIdx < 0 || fromIdx == toIdx {
		return
	}
	item := vc.Properties[fromIdx]
	vc.Properties = slices.Delete(vc.Properties, fromIdx, fromIdx+1)
	vc.Properties = slices.Insert(vc.Properties, toIdx, item)
	saveViewAndNotify(st, vc, opts)
}

func reorderRecordRow(st *State, view *database.ViewConfig, res *database.QueryResult, fromID, toID string, opts Options) {
	if res == nil || len(res.Rows) == 0 {
		return
	}
	ids := make([]string, len(res.Rows))
	for i, r := range res.Rows {
		ids[i] = r.ID
	}
	fromIdx := slices.Index(ids, fromID)
	toIdx := slices.Index(ids, toID)
	if fromIdx < 0 || toIdx < 0 || fromIdx == toIdx {
		return
	}
	ids = slices.Delete(ids, fromIdx, fromIdx+1)
	ids = slices.Insert(ids, toIdx, fromID)
	if err := st.Store.SetManualOrder(view.ID, ids); err == nil && opts.OnChanged != nil {
		opts.OnChanged(nil)
	}
}
