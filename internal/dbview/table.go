package dbview

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

type tableRowKind uint8

const (
	itemGroupHeader tableRowKind = iota
	itemRecord
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
	tableGutterW = float32(48)
	tableHeaderH = float32(34)
	tableFooterH = float32(32)
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
		!st.ColorRulesOpen && !st.TemplatesOpen && !st.EditPropOpen

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

	tableArea.Children(func() {
		scroller := ui.ScrollHorizontal(c.Key("table-x")).TrackScroll(&st.TableScrollX).
			AlignItems(ui.Stretch).Grow(1).MinWidth(0).MinHeight(0)
		viewportW := scroller.LayoutBounds().W
		contentW := max(viewportW, totalColsW)
		scrollX := st.TableScrollX.X

		scroller.Children(func() {
			ui.Column(c.Key("table-inner")).Width(contentW).Grow(1).MinHeight(0).Children(func() {
				// 1. Sticky Header Row.
				renderTableHeaderRow(c, st, &view, db, frozenCol, rightCols, freezeFirst, frozenW, contentW, scrollX, res, opts, pal)

				// 2. Virtualized Rows Body.
				recSeq := 0
				recIdxByID := make(map[string]int, len(recordItemIndices))
				for _, itemIdx := range recordItemIndices {
					if r := items[itemIdx].record; r != nil {
						recIdxByID[r.ID] = recSeq
						recSeq++
					}
				}

				list := ui.List(c.Key("table-rows"), &st.RowScroll, len(items)).
					Grow(1).MinHeight(0).ItemKey(func(i int) any { return items[i].key })
				list.Rows(func(row ui.ListRow) {
					rc := row.Context
					it := items[row.Index]
					switch it.kind {
					case itemGroupHeader:
						renderGroupHeaderRow(rc, st, &view, it.group, contentW, scrollX, opts, pal)
					case itemAddRow:
						renderAddRow(rc, st, &view, db, it.groupProp, it.groupValue, contentW, scrollX, opts, pal)
					case itemRecord:
						rIdx := recIdxByID[it.record.ID]
						tabDir := renderRecordRow(rc, st, &view, db, it.record, cols, frozenCol, rightCols, freezeFirst, frozenW, contentW, scrollX, rIdx, res, opts, pal)
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
				renderTableFooterRow(c, st, &view, db, frozenCol, rightCols, freezeFirst, frozenW, contentW, scrollX, res, opts, pal)
			})
		})
	})
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
	res *database.QueryResult,
	opts Options,
	pal Palette,
) {
	head := ui.Box(c.Key("table-head")).Width(contentW).Height(tableHeaderH).Shrink(0).
		Background(pal.HeaderBg).BorderWidth(0, 0, 1, 0).BorderColor(pal.Border).Clip()
	head.Children(func() {
		// Right scrollable column headers.
		ui.Row(c).PaddingStart(frozenW).Height(tableHeaderH).AlignItems(ui.Stretch).Children(func() {
			for _, col := range rightCols {
				renderColumnHeaderCell(c, st, view, db, col, opts, pal)
			}
			// Add property "+" header button.
			addCol := ui.Box(c.Key("add-col-hdr")).Width(tableAddColW).Height(tableHeaderH).Center().
				Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("新建属性")).Tooltip(i18n.T("新建属性"))
			if addCol.Hovered() {
				addCol.Background(pal.RowHover)
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
			Translate(scrollX, 0).Background(pal.HeaderBg).AlignItems(ui.Stretch).
			BorderWidth(0, 1, 0, 0).BorderColor(pal.Border)
		if scrollX > 0.5 {
			frozen.Shadow(2, 0, 8, 0, pal.ShadowEdge)
		}
		frozen.Children(func() {
			renderSelectAllGutter(c, st, res, pal)
			if freezeFirst {
				renderColumnHeaderCell(c, st, view, db, frozenCol, opts, pal)
			}
		})
	})
}

func renderSelectAllGutter(c *ui.Context, st *State, res *database.QueryResult, pal Palette) {
	allSelected := len(res.Rows) > 0 && len(st.SelectedIDs) >= len(res.Rows)
	if allSelected {
		for _, r := range res.Rows {
			if r != nil && !st.SelectedIDs[r.ID] {
				allSelected = false
				break
			}
		}
	}
	gut := ui.Row(c.Key("gutter-all")).Width(tableGutterW).Shrink(0).Center()
	gut.Children(func() {
		cb := ui.CheckboxBase(c.Key("sel-all"), &allSelected).Size(16, 16).Radius(4).Center().
			Cursor(ui.CursorPointer).Label(i18n.T("全选"))
		cb.Draw(func(p *ui.Painter, r ui.Rect) {
			if allSelected {
				p.Fill(r, pal.Accent, 4)
				p.Icon(iconCheck, rectInset(r, 2.5), ui.RGB(255, 255, 255))
			} else {
				p.Stroke(rectInset(r, 0.5), pal.BorderStrong, 4, 1.2)
			}
		})
		if cb.Changed() {
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
	opts Options,
	pal Palette,
) {
	prop := col.prop
	cell := ui.Row(c.Key("hdr:"+prop.ID)).Width(col.width).Shrink(0).Height(tableHeaderH).
		Padding(0, 6, 0, 8).Gap(5).AlignItems(ui.Center).BorderWidth(0, 1, 0, 0).BorderColor(pal.Border).Cursor(ui.CursorPointer)
	if cell.Hovered() {
		cell.Background(pal.RowHover)
	}
	if prop.Type != database.PropTitle && !opts.ReadOnly {
		cell.Drag(colDrag{propID: prop.ID})
		if d, ok := ui.Drop[colDrag](cell); ok && d.propID != prop.ID {
			reorderViewColumn(st, view, db, d.propID, prop.ID, opts)
		}
		if d, over := ui.DragOver[colDrag](cell); over && d.propID != prop.ID {
			cell.Background(pal.AccentSoft)
		}
	}

	sortDesc, isSorted := sortDirectionFor(view, prop.ID)
	renaming := st.RenamingPropID == prop.ID

	cell.Children(func() {
		ui.Icon(c, PropertyIcon(prop.Type)).FontSize(13).TextColor(pal.TextMuted)
		if renaming {
			in := ui.TextInput(c.Key("hdr-ren"), &st.RenamingPropText).Grow(1).MinWidth(0).Height(24).
				Padding(1, 4).Radius(4).Background(pal.Paper).TextColor(pal.Text).AutoFocus().Label(i18n.T("属性名称"))
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
			ui.Text(c, prop.Name).FontSize(12.5).FontWeight(500).TextColor(pal.TextMuted).SingleLine().Grow(1).MinWidth(0)
			if isSorted {
				sIcon := iconArrowUp
				if sortDesc {
					sIcon = iconArrowDown
				}
				ui.Icon(c, sIcon).FontSize(11).TextColor(pal.Accent)
			}
		}

		// Right-edge column width resize grip.
		grip := ui.Box(c.Key("resize:" + prop.ID)).Width(6).Height(tableHeaderH).Shrink(0).Cursor(ui.CursorResizeEW)
		if dx, _, held := grip.Dragged(); held && dx != 0 {
			nextW := min(max(col.width+dx, 72), 600)
			setViewColumnWidth(st, view, db, prop.ID, nextW, opts)
		}
	})

	if !renaming {
		cell.PopoverMenuButton(func(m *ui.Menu) {
			if !opts.ReadOnly {
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
			if m.Item(i18n.T("升序排序")).Chosen() {
				vc := view.Clone()
				vc.Sorts = []database.SortRule{{PropertyID: prop.ID, Desc: false}}
				saveViewAndNotify(st, vc, opts)
			}
			if m.Item(i18n.T("降序排序")).Chosen() {
				vc := view.Clone()
				vc.Sorts = []database.SortRule{{PropertyID: prop.ID, Desc: true}}
				saveViewAndNotify(st, vc, opts)
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
		})
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
	if g == nil {
		return
	}
	row := ui.Box(c.Key("gh:"+g.Key)).Width(contentW).Height(34).Background(pal.Paper).BorderWidth(0, 0, 1, 0).BorderColor(pal.Border)
	row.Children(func() {
		inner := ui.Row(c.Key("gh-in")).Absolute().Left(0).Top(0).Bottom(0).Width(420).
			Translate(scrollX, 0).Padding(0, 12).Gap(8).AlignItems(ui.Center).Cursor(ui.CursorPointer)
		inner.Children(func() {
			chev := iconChevronDown
			if g.Collapsed {
				chev = iconChevronRight
			}
			ui.Icon(c, chev).FontSize(13).TextColor(pal.TextMuted)
			if g.Color != "" && g.Color != "default" {
				renderTagPill(c, g.Label, g.Color, false, pal)
			} else {
				ui.Text(c, g.Label).FontSize(13).FontWeight(600).TextColor(pal.Text).SingleLine()
			}
			ui.Text(c, fmt.Sprintf("%d", g.Count)).FontSize(12).TextColor(pal.TextMuted).SingleLine()
		})
		if inner.Clicked() && view.Group != nil {
			vc := view.Clone()
			vc.Group.Collapsed = toggleStringInSlice(slices.Clone(vc.Group.Collapsed), g.Key)
			saveViewAndNotify(st, vc, opts)
		}
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
	opts Options,
	pal Palette,
) {
	if opts.ReadOnly {
		return
	}
	row := ui.Box(c.Key("add-row:"+groupProp+":"+groupVal.PlainString())).
		Width(contentW).Height(34).Background(pal.Paper).BorderWidth(0, 0, 1, 0).BorderColor(pal.Border)
	if row.Hovered() {
		row.Background(pal.RowHover)
	}
	row.Children(func() {
		inner := ui.Row(c).Absolute().Left(0).Top(0).Bottom(0).Width(200).Translate(scrollX, 0).
			Padding(0, 14).Gap(6).AlignItems(ui.Center).Cursor(ui.CursorPointer)
		inner.Children(func() {
			ui.Icon(c, iconPlus).FontSize(13).TextColor(pal.TextMuted)
			ui.Text(c, i18n.T("新建")).FontSize(13).TextColor(pal.TextMuted).SingleLine()
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
	rec *database.Record,
	cols []tableCol,
	frozenCol tableCol,
	rightCols []tableCol,
	freezeFirst bool,
	frozenW, contentW, scrollX float32,
	rIdx int,
	res *database.QueryResult,
	opts Options,
	pal Palette,
) int {
	if rec == nil {
		return 0
	}
	tabDir := 0
	selected := st.SelectedIDs[rec.ID]
	tintBg, tintAccent, tinted := tintedRowBackground(pal, res, rec.ID)
	rowBg := pal.Paper
	if tinted {
		rowBg = tintBg
	}
	if selected {
		rowBg = pal.RowSelected
	}

	row := ui.Box(c.Key("r:"+rec.ID)).Width(contentW).MinHeight(34).Background(rowBg).BorderWidth(0, 0, 1, 0).BorderColor(pal.Border)
	hovered := row.Hovered()
	if hovered && !selected {
		if tinted {
			rowBg = tintBg.Mix(pal.Text, 0.03)
		} else {
			rowBg = pal.RowHover
		}
		row.Background(rowBg)
	}

	// Support drag-to-reorder rows when view has no active Sort rules.
	if len(view.Sorts) == 0 && !opts.ReadOnly {
		if d, ok := ui.Drop[rowDrag](row); ok && d.recordID != rec.ID {
			reorderRecordRow(st, view, res, d.recordID, rec.ID, opts)
		}
		if d, over := ui.DragOver[rowDrag](row); over && d.recordID != rec.ID {
			row.DrawOver(func(p *ui.Painter, r ui.Rect) {
				p.Line(r.X, r.Y, r.X+r.W, r.Y, 2, pal.Accent)
			})
		}
	}

	openMode := view.OpenMode
	if openMode == "" {
		openMode = OpenSidePeek
	}

	row.ContextMenu(func(m *ui.Menu) {
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
			m.Separator()
		}
		if !opts.ReadOnly {
			if m.Item(i18n.T("移到废纸篓")).Chosen() {
				trashSingleRecord(st, rec, opts)
			}
		}
	})

	row.Children(func() {
		// Right scrollable columns.
		ui.Row(c).PaddingStart(frozenW).MinHeight(34).AlignItems(ui.Stretch).Children(func() {
			colOffset := 0
			if freezeFirst {
				colOffset = 1
			}
			for j, col := range rightCols {
				cIdx := colOffset + j
				focused := st.FocusRow == rIdx && st.FocusCol == cIdx
				wrap := col.wrap || (view.Table != nil && view.Table.WrapAll)
				cellBox := ui.Row(c.Key("rc:"+col.prop.ID)).Width(col.width).Shrink(0).MinHeight(34).
					BorderWidth(0, 1, 0, 0).BorderColor(pal.Border).AlignItems(ui.Stretch).Clip()
				cellBox.Children(func() {
					out := cellView(c, st, rec, col.prop, focused, wrap, false, opts, pal)
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
			Translate(scrollX, 0).Background(rowBg).AlignItems(ui.Stretch).BorderWidth(0, 1, 0, 0).BorderColor(pal.Border)
		if scrollX > 0.5 {
			frozen.Shadow(2, 0, 8, 0, pal.ShadowEdge)
		}
		if tinted && !selected {
			frozen.DrawOver(func(p *ui.Painter, r ui.Rect) {
				p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 3, H: r.H}, tintAccent, 0)
			})
		}
		frozen.Children(func() {
			// Gutter: drag handle + row checkbox.
			gut := ui.Row(c.Key("gut")).Width(tableGutterW).Shrink(0).Padding(0, 4).Gap(2).AlignItems(ui.Center)
			gut.Children(func() {
				grip := ui.Box(c.Key("drag")).Size(16, 22).Center().Cursor(ui.CursorGrab)
				if hovered && len(view.Sorts) == 0 && !opts.ReadOnly {
					grip.Drag(rowDrag{recordID: rec.ID})
					grip.Children(func() { ui.Icon(c, iconGrip).FontSize(12).TextColor(pal.TextMuted) })
				}
				cb := ui.CheckboxBase(c.Key("sel"), &selected).Size(16, 16).Radius(4).Center().
					Cursor(ui.CursorPointer).Label(rec.Title)
				cb.Draw(func(p *ui.Painter, r ui.Rect) {
					if selected {
						p.Fill(r, pal.Accent, 4)
						p.Icon(iconCheck, rectInset(r, 2.5), ui.RGB(255, 255, 255))
					} else if hovered {
						p.Stroke(rectInset(r, 0.5), pal.BorderStrong, 4, 1.2)
					}
				})
				if cb.Changed() {
					if selected {
						st.SelectedIDs[rec.ID] = true
					} else {
						delete(st.SelectedIDs, rec.ID)
					}
				}
			})

			if freezeFirst {
				focused := st.FocusRow == rIdx && st.FocusCol == 0
				wrap := frozenCol.wrap || (view.Table != nil && view.Table.WrapAll)
				titleWrap := ui.Row(c.Key("title-cell")).Width(frozenCol.width).Shrink(0).
					AlignItems(ui.Center).Padding(0, 6, 0, 0).Clip()
				titleWrap.Children(func() {
					if (view.Table == nil || view.Table.ShowIcon) && rec.Icon != "" {
						ui.Text(c, rec.Icon).FontSize(14).Padding(0, 0, 0, 6)
					}
					out := cellView(c, st, rec, frozenCol.prop, focused, wrap, false, opts, pal)
					if out.clicked {
						st.FocusRow, st.FocusCol = rIdx, 0
					}
					if out.tabDir != 0 {
						tabDir = out.tabDir
					}
					if opts.OnOpenRecord != nil && (hovered || focused) && !(st.Editing && st.EditRecordID == rec.ID && st.EditPropID == frozenCol.prop.ID) {
						openBtn := ui.ButtonBase(c.Key("open-rec")).Height(22).Padding(0, 7).Radius(5).
							Background(pal.Surface).Border(1, pal.BorderStrong).Cursor(ui.CursorPointer).
							AlignItems(ui.Center).Role(ui.RoleButton).Label(i18n.T("打开")).
							Tooltip(i18n.T("打开记录"))
						if openBtn.Hovered() {
							openBtn.Background(pal.AccentSoft).Border(1, pal.Accent)
						}
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
	opts Options,
	pal Palette,
) {
	foot := ui.Box(c.Key("table-foot")).Width(contentW).Height(tableFooterH).Shrink(0).
		Background(pal.HeaderBg).BorderWidth(1, 0, 0, 0).BorderColor(pal.Border).Clip()
	foot.Children(func() {
		ui.Row(c).PaddingStart(frozenW).Height(tableFooterH).AlignItems(ui.Stretch).Children(func() {
			for _, col := range rightCols {
				renderAggFooterCell(c, st, view, col, res, opts, pal)
			}
			ui.Box(c).Width(tableAddColW).Shrink(0)
		})

		frozen := ui.Row(c.Key("frozen-foot")).Absolute().Left(0).Top(0).Bottom(0).Width(frozenW).
			Translate(scrollX, 0).Background(pal.HeaderBg).AlignItems(ui.Stretch).BorderWidth(0, 1, 0, 0).BorderColor(pal.Border)
		if scrollX > 0.5 {
			frozen.Shadow(2, 0, 8, 0, pal.ShadowEdge)
		}
		frozen.Children(func() {
			ui.Box(c).Width(tableGutterW).Shrink(0)
			if freezeFirst {
				renderAggFooterCell(c, st, view, frozenCol, res, opts, pal)
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
	if aggRes, ok := res.Aggregations[prop.ID]; ok && aggRes.HasValue {
		label = aggregationShortPrefix(curAgg) + aggRes.Display
	}

	cell := ui.Row(c.Key("agg:"+prop.ID)).Width(col.width).Shrink(0).Height(tableFooterH).
		Padding(0, 8).AlignItems(ui.Center).Cursor(ui.CursorPointer).
		Role(ui.RoleMenuButton).Label(i18n.T("列统计"))
	if cell.Hovered() {
		cell.Background(pal.RowHover)
	}
	cell.Children(func() {
		if label != "" {
			ui.Spacer(c)
			ui.Text(c, label).FontSize(12).TextColor(pal.TextMuted).SingleLine()
		} else if cell.Hovered() {
			ui.Spacer(c)
			ui.Text(c, i18n.T("计算")).FontSize(11.5).TextColor(pal.TextMuted).SingleLine()
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
		out := make([]tableItem, 0, len(res.Rows)+1)
		for _, r := range res.Rows {
			if r != nil {
				out = append(out, tableItem{key: "rec:" + r.ID, kind: itemRecord, record: r})
			}
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
					out = append(out, tableItem{key: "rec:" + g.Key + ":" + r.ID, kind: itemRecord, group: g, record: r})
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
		return 76
	case database.PropNumber:
		return 112
	case database.PropSelect, database.PropStatus, database.PropUniqueID:
		return 128
	case database.PropDate, database.PropCreatedTime, database.PropEditedTime:
		return 156
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
