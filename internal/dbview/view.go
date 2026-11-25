package dbview

import (
	"fmt"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// View renders the complete database workspace (header, view tabs, toolbar,
// bulk bar, and active view body) inside c.
func View(c *ui.Context, st *State, opts Options) {
	if st == nil || st.Store == nil {
		return
	}
	db := st.Store.SchemaSnapshot()
	if db == nil {
		return
	}
	pal := paletteOf(c.Theme())

	view := st.activeViewFrom(db)
	if view == nil {
		return
	}
	st.Motion.beginFrame(c)
	if view.Type != database.ViewBoard && (st.BoardDrag.grab.active || st.BoardDrag.settle.active) {
		st.BoardDrag.reset()
		st.Motion.drag = dbDragPreview{}
		st.Motion.settle = dbDragSettle{}
	}

	root := ui.Column(c.Key("db-root")).Grow(1).MinWidth(0).MinHeight(0).
		Background(pal.Paper).Focusable().Role(ui.RoleMain).Label(db.Title).Bind(&st.RootHandle)
	if st.RequestFocus {
		st.RequestFocus = false
		root.Focus()
	}

	// Global database undo / redo / escape handling.
	if root.FocusWithin() && !c.FocusTakesText() {
		switch {
		case root.Shortcut(ui.Cmd, ui.KeyZ):
			if !opts.ReadOnly {
				if paths, ok := st.Store.Undo(); ok && opts.OnChanged != nil {
					opts.OnChanged(paths)
				}
			}
		case root.Shortcut(ui.Cmd|ui.Shift, ui.KeyZ):
			if !opts.ReadOnly {
				if paths, ok := st.Store.Redo(); ok && opts.OnChanged != nil {
					opts.OnChanged(paths)
				}
			}
		case root.Shortcut(0, ui.KeyEscape):
			switch {
			case st.TimelineDragActive:
				st.TimelineDragActive = false
			case st.Editing || st.EditPopoverOpen:
				st.CancelEdit()
			case st.FilterOpen || st.SortOpen || st.GroupOpen || st.PropsOpen || st.LayoutOpen || st.NewViewOpen || st.AddPropOpen || st.ColorRulesOpen || st.TemplatesOpen || st.EditPropOpen || st.AddingGroup || st.CalendarOverflowOpen:
				st.CloseToolbarPopovers()
			case len(st.SelectedIDs) > 0:
				st.ClearSelection()
			}
		}
	}

	res := st.Store.Query(view.ID, database.QueryOptions{
		Search: st.SearchQuery,
		Now:    opts.Now,
	})

	root.Children(func() {
		// 1. Unified Database Header (title, count pill, search & primary create).
		if !opts.Embedded {
			renderDatabaseHeader(c, st, db, *view, res, opts, pal)
		}

		// 2. Sleek Utility Ribbon (embedded view tabs + filter/sort/group/props/layout).
		renderViewToolbar(c, st, db, *view, opts, pal)

		// 3. Animated Bulk Selection Action Bar.
		if len(st.SelectedIDs) > 0 && !opts.ReadOnly {
			renderBulkActionBar(c, st, db, res, opts, pal)
		}

		ui.Box(c).Height(1).Background(pal.Border.Alpha(0.75))

		// 4. Active View Body with soft cross-fade glide on view switch.
		stage := ui.Column(c.Key("view-stage:" + view.ID)).Grow(1).MinWidth(0).MinHeight(0).
			Transition(ui.ElementTransition{
				Duration: 150 * time.Millisecond,
				Ease:     dbEaseOut,
				Enter:    &ui.Motion{Y: 5, Opacity: 0.78},
			})
		stage.Children(func() {
			if spec, ok := ViewSpecFor(view.Type); ok && spec.Render != nil {
				spec.Render(c, st, *view, db, res, opts, pal)
			} else {
				renderPlannedViewPlaceholder(c, st, *view, db, pal)
			}
		})
	})

	// Paint lifted floating drag previews and smooth spring drop settles on top.
	st.Motion.finishFrame(c, pal)
}

func renderDatabaseHeader(c *ui.Context, st *State, db *database.Database, view database.ViewConfig, res *database.QueryResult, opts Options, pal Palette) {
	ui.Row(c.Key("db-header")).Padding(14, 22, 6, 22).Gap(10).AlignItems(ui.Center).Children(func() {
		if db.Icon != "" {
			ui.Text(c, db.Icon).FontSize(22).NoWrap()
		} else {
			ui.Box(c).Size(30, 30).Radius(8).Background(pal.AccentSoft).
				Border(1, pal.Accent.Alpha(0.18)).Center().Shrink(0).Children(func() {
				ui.Icon(c, ViewIcon(view.Type)).FontSize(15).TextColor(pal.Accent)
			})
		}

		if st.EditingTitle && !opts.ReadOnly {
			in := ui.TextInput(c.Key("db-title-in"), &st.TitleDraft).
				FontSize(19).FontWeight(700).TextColor(pal.Text).
				Grow(1).MinWidth(120).Height(34).Padding(2, 8).Radius(7).
				Background(pal.Surface).Border(1, pal.Accent).AutoFocus().Label(i18n.T("数据库名称"))
			if st.TitleFocusPending {
				in.Focus()
				st.TitleFocusPending = false
			}
			switch {
			case in.Shortcut(0, ui.KeyEscape):
				st.EditingTitle = false
			case in.Submitted():
				st.EditingTitle = false
				if err := st.Store.SetTitle(st.TitleDraft); err == nil && opts.OnChanged != nil {
					opts.OnChanged(nil)
				}
			}
		} else {
			titleEl := ui.Row(c.Key("db-title")).Padding(2, 6).Radius(7).Cursor(ui.CursorPointer).
				AlignItems(ui.Center).Gap(8).Shrink(1).MinWidth(0).
				Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
			if !opts.ReadOnly && titleEl.Hovered() {
				titleEl.Background(pal.RowHover)
			}
			titleEl.Children(func() {
				ui.Text(c, db.Title).FontSize(19).FontWeight(700).TextColor(pal.Text).SingleLine()
			})
			if titleEl.Clicked() && !opts.ReadOnly {
				st.EditingTitle = true
				st.TitleDraft = db.Title
				st.TitleFocusPending = true
			}
		}

		ui.Box(c).Height(22).Padding(0, 8).Radius(999).Background(pal.Surface).
			Border(1, pal.Border.Alpha(0.75)).Center().Shrink(0).Children(func() {
			ui.Text(c, fmt.Sprintf(i18n.T("共 %d 条"), res.TotalCount)).FontSize(11.5).FontWeight(500).TextColor(pal.TextMuted).NoWrap()
		})
		ui.Spacer(c)

		renderHeaderRightActions(c, st, db, view, opts, pal)
	})
}

func renderHeaderRightActions(c *ui.Context, st *State, db *database.Database, view database.ViewConfig, opts Options, pal Palette) {
	ui.Row(c.Key("db-right-acts")).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
		if st.SearchOpen {
			wrap := ui.Box(c.Key("db-search-wrap")).
				Transition(ui.ElementTransition{
					Duration:     180 * time.Millisecond,
					Ease:         dbEaseOut,
					ExitDuration: 120 * time.Millisecond,
					Enter:        &ui.Motion{X: 10, Collapse: true, Opacity: 0},
					Exit:         &ui.Motion{X: 8, Collapse: true, Opacity: 0},
				})
			wrap.Children(func() {
				sf := ui.SearchField(c.Key("db-search"), &st.SearchQuery).
					Placeholder(i18n.T("搜索记录…")).Label(i18n.T("搜索记录")).Width(172).Height(28)
				if st.searchFocusPending {
					sf.Focus()
					st.searchFocusPending = false
				}
				if sf.Shortcut(0, ui.KeyEscape) {
					st.SearchQuery = ""
					st.SearchOpen = false
				}
			})
		}
		srBtn := toolbarIconBtn(c, "tb-search", iconSearch, i18n.T("搜索"), false, st.SearchOpen || st.SearchQuery != "", pal)
		if srBtn.Clicked() {
			st.SearchOpen = !st.SearchOpen
			if st.SearchOpen {
				st.searchFocusPending = true
			} else {
				st.SearchQuery = ""
			}
		}

		if !opts.ReadOnly {
			renderUnifiedNewButton(c, st, db, view, opts, pal)
		}
	})
}

func renderUnifiedNewButton(c *ui.Context, st *State, db *database.Database, view database.ViewConfig, opts Options, pal Palette) {
	split := ui.Row(c.Key("db-new-split")).Height(29).Radius(8).AlignItems(ui.Center).Shrink(0).
		Background(pal.Accent).Shadow(0, 1, 3, 0, pal.Accent.Alpha(0.28)).Clip()
	split.Children(func() {
		newBtn := ui.Row(c.Key("db-new-rec")).Height(29).Padding(0, 11).Gap(4).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label("+ " + i18n.T("新建")).
			Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
		if newBtn.Hovered() {
			newBtn.Background(pal.AccentText.Alpha(0.12))
		}
		applyPressSpring(newBtn, false)
		newBtn.Children(func() {
			ui.Text(c, "+ "+i18n.T("新建")).FontSize(12.5).FontWeight(500).TextColor(pal.AccentText).NoWrap()
		})
		if newBtn.Clicked() {
			createRecordWithDefaults(st, db, "", database.EmptyValue(), opts)
		}

		ui.Box(c).Width(1).Height(15).Background(pal.AccentText.Alpha(0.24)).Shrink(0)

		tplBtn := ui.Box(c.Key("db-tpl-menu")).Size(25, 29).Center().
			Cursor(ui.CursorPointer).Focusable().
			Role(ui.RoleButton).Label(i18n.T("模板菜单")).Tooltip(i18n.T("记录模板")).
			Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
		if tplBtn.Hovered() || st.TemplatesOpen {
			tplBtn.Background(pal.AccentText.Alpha(0.16))
		}
		applyPressSpring(tplBtn, false)
		tplBtn.Children(func() {
			ui.Icon(c, iconChevronDown).FontSize(12).TextColor(pal.AccentText)
		})
		if tplBtn.Clicked() {
			open := !st.TemplatesOpen
			st.CloseToolbarPopovers()
			st.TemplatesOpen = open
			st.EditingTemplateID = ""
		}
		if st.TemplatesOpen {
			templatesPopover(c, tplBtn, st, db, view, opts, pal)
		}
	})
}

func renderViewToolbar(c *ui.Context, st *State, db *database.Database, view database.ViewConfig, opts Options, pal Palette) {
	padTop := float32(2)
	if opts.Embedded {
		padTop = 6
	}
	ui.Row(c.Key("db-toolbar")).Wrap().Padding(padTop, 22, 8, 22).Gap(8).AlignItems(ui.Center).Children(func() {
		// Left: View Tabs (hidden in full-page workspace where views live in the left sidebar tree).
		if !opts.HideViewTabs {
			tabsScroll := ui.ScrollHorizontal(c.Key("view-tabs")).TrackScroll(&st.TabScroll).Grow(1).MinWidth(160).AlignItems(ui.Center)
			tabsScroll.Children(func() {
				ui.Row(c).Gap(3).AlignItems(ui.Center).Children(func() {
					for _, v := range db.Views {
						vc := v
						active := vc.ID == view.ID
						renaming := st.RenameViewID == vc.ID

						if renaming {
							in := ui.TextInput(c.Key("vren:"+vc.ID), &st.RenameViewText).
								Width(120).Height(28).Padding(2, 8).Radius(6).
								Background(pal.Surface).Border(1, pal.Accent).TextColor(pal.Text).AutoFocus().Label(i18n.T("视图名称"))
							if st.RenameViewPending {
								in.Focus()
								st.RenameViewPending = false
							}
							if in.Shortcut(0, ui.KeyEscape) {
								st.RenameViewID = ""
							} else if in.Submitted() || (!in.Focused() && !st.RenameViewPending) {
								next := strings.TrimSpace(st.RenameViewText)
								st.RenameViewID = ""
								if next != "" && next != vc.Name {
									updated := vc.Clone()
									updated.Name = next
									saveViewAndNotify(st, updated, opts)
								}
							}
							continue
						}

						tab := ui.Row(c.Key("vtab:"+vc.ID)).Height(29).Padding(0, 10).Radius(8).Gap(6).
							AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().
							Role(ui.RoleTab).Selected(active).Label(vc.Name).
							Background(ui.Transparent).
							Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
						if !active && tab.Hovered() {
							tab.Background(pal.RowHover)
						}
						applyPressSpring(tab, false)
						tab.Children(func() {
							if active {
								ui.Box(c.Key("vtab-pill")).Absolute().Left(0).Top(0).Right(0).Bottom(0).Radius(8).
									Background(pal.AccentSoft).Border(1, pal.Accent.Alpha(0.18)).
									IgnoreInput(true).ZIndex(0).
									Transition(ui.ElementTransition{
										Animation: &dbSpringSnappy,
										Match:     viewTabPillMatch{},
									})
							}
							ink := pal.TextMuted
							if active {
								ink = pal.Accent
							}
							ui.Icon(c, ViewIcon(vc.Type)).FontSize(13).TextColor(ink).ZIndex(1)
							lbl := ui.Text(c, vc.Name).FontSize(12.5).SingleLine().ZIndex(1)
							if active {
								lbl.FontWeight(600).TextColor(pal.Accent)
							} else {
								lbl.TextColor(pal.TextMuted)
							}
						})
						if tab.Clicked() {
							st.SetActiveView(vc.ID)
							if !st.LocalView && opts.OnChanged != nil {
								opts.OnChanged(nil)
							}
						}
						isPrimary := db.IsPrimaryView(vc.ID)
						tab.ContextMenu(func(m *ui.Menu) {
							if !opts.ReadOnly {
								if m.Item(i18n.T("重命名视图…")).Chosen() {
									st.RenameViewID = vc.ID
									st.RenameViewText = vc.Name
									st.RenameViewPending = true
								}
								if m.Item(i18n.T("复制视图")).Chosen() {
									if _, err := st.Store.DuplicateView(vc.ID); err == nil && opts.OnChanged != nil {
										opts.OnChanged(nil)
									}
								}
								m.Submenu(i18n.T("默认打开方式"), func(sub *ui.Menu) {
									curMode := vc.OpenMode
									if curMode == "" {
										curMode = OpenSidePeek
									}
									for _, om := range []struct{ id, label string }{
										{OpenSidePeek, i18n.T("侧边预览")},
										{OpenCenterPeek, i18n.T("居中弹窗")},
										{OpenFullPage, i18n.T("整页打开")},
									} {
										mode := om
										if sub.Item(mode.label).Checked(curMode == mode.id).Chosen() {
											updated := vc.Clone()
											updated.OpenMode = mode.id
											saveViewAndNotify(st, updated, opts)
										}
									}
								})
								if len(db.Views) > 1 && !isPrimary {
									m.Separator()
									if m.Item(i18n.T("删除视图")).Chosen() {
										if err := st.Store.DeleteView(vc.ID); err == nil && opts.OnChanged != nil {
											opts.OnChanged(nil)
										}
									}
								}
							}
						})
					}

					if !opts.ReadOnly {
						addViewBtn := ui.Box(c.Key("add-view-btn")).Size(28, 28).Radius(7).Center().
							Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("新建视图")).Tooltip(i18n.T("新建视图"))
						if addViewBtn.Hovered() || st.NewViewOpen {
							addViewBtn.Background(pal.RowHover)
						}
						addViewBtn.Children(func() { ui.Icon(c, iconPlus).FontSize(14).TextColor(pal.TextMuted) })
						if addViewBtn.Clicked() {
							open := !st.NewViewOpen
							st.CloseToolbarPopovers()
							st.NewViewOpen = open
							st.NewViewName = ""
							st.NewViewType = database.ViewTimeline
						}
						if st.NewViewOpen {
							newViewPopover(c, addViewBtn, st, db, opts, pal)
						}
					}
				})
			})
		}

		// Toolbar Actions: Undo/Redo, Filter, Sort, Group, Properties, Layout, Search, New Record.
		actionsRow := ui.Row(c.Key("tb-actions")).Gap(4).AlignItems(ui.Center)
		if opts.HideViewTabs {
			actionsRow.Wrap().Grow(1).MinWidth(0)
		} else {
			actionsRow.Shrink(0)
		}
		actionsRow.Children(func() {
			if !opts.ReadOnly {
				undoBtn := toolbarIconBtn(c, "undo", iconUndo, i18n.T("撤销"), !st.Store.History().CanUndo(), false, pal)
				if undoBtn.Clicked() {
					if paths, ok := st.Store.Undo(); ok && opts.OnChanged != nil {
						opts.OnChanged(paths)
					}
				}
				redoBtn := toolbarIconBtn(c, "redo", iconRedo, i18n.T("重做"), !st.Store.History().CanRedo(), false, pal)
				if redoBtn.Clicked() {
					if paths, ok := st.Store.Redo(); ok && opts.OnChanged != nil {
						opts.OnChanged(paths)
					}
				}
				ui.Box(c).Width(1).Height(14).Margin(0, 4).Background(pal.Border).Shrink(0)
			}

			filterCount := 0
			if view.Filter != nil {
				filterCount = len(view.Filter.Rules) + len(view.Filter.Groups)
			}
			fLabel := i18n.T("筛选")
			if filterCount > 0 {
				fLabel = fmt.Sprintf("%s (%d)", fLabel, filterCount)
			}
			fBtn := toolbarPillBtn(c, "tb-filter", iconFilter, fLabel, filterCount > 0 || st.FilterOpen, pal)
			if fBtn.Clicked() {
				open := !st.FilterOpen
				st.CloseToolbarPopovers()
				st.FilterOpen = open
			}
			if st.FilterOpen {
				filterPopover(c, fBtn, st, view, db, opts, pal)
			}

			sLabel := i18n.T("排序")
			if len(view.Sorts) > 0 {
				sLabel = fmt.Sprintf("%s (%d)", sLabel, len(view.Sorts))
			}
			sBtn := toolbarPillBtn(c, "tb-sort", iconSort, sLabel, len(view.Sorts) > 0 || st.SortOpen, pal)
			if sBtn.Clicked() {
				open := !st.SortOpen
				st.CloseToolbarPopovers()
				st.SortOpen = open
			}
			if st.SortOpen {
				sortPopover(c, sBtn, st, view, db, opts, pal)
			}

			gLabel := i18n.T("分组")
			hasGroup := view.Group != nil && view.Group.PropertyID != ""
			if hasGroup {
				if gp, idx := db.PropertyByID(view.Group.PropertyID); idx >= 0 {
					gLabel = gp.Name
				}
			}
			gBtn := toolbarPillBtn(c, "tb-group", iconGroup, gLabel, hasGroup || st.GroupOpen, pal)
			if gBtn.Clicked() {
				open := !st.GroupOpen
				st.CloseToolbarPopovers()
				st.GroupOpen = open
			}
			if st.GroupOpen {
				groupPopover(c, gBtn, st, view, db, opts, pal)
			}

			pBtn := toolbarPillBtn(c, "tb-props", iconSliders, i18n.T("属性"), st.PropsOpen || st.EditPropOpen, pal)
			if pBtn.Clicked() {
				open := !st.PropsOpen
				st.CloseToolbarPopovers()
				st.PropsOpen = open
			}
			if st.PropsOpen {
				propertiesPopover(c, pBtn, st, view, db, opts, pal)
			}
			if st.EditPropOpen && st.ConfigPropID != "" {
				if ep, pIdx := db.PropertyByID(st.ConfigPropID); pIdx >= 0 {
					propertyConfigPopover(c, pBtn, st, db, *ep, opts, pal)
				} else {
					st.EditPropOpen = false
				}
			}

			cBtn := toolbarIconBtn(c, "tb-colors", iconPalette, i18n.T("条件着色"), false, st.ColorRulesOpen || len(view.Colors) > 0, pal)
			if cBtn.Clicked() {
				open := !st.ColorRulesOpen
				st.CloseToolbarPopovers()
				st.ColorRulesOpen = open
			}
			if st.ColorRulesOpen {
				colorRulesPopover(c, cBtn, st, view, db, opts, pal)
			}

			lBtn := toolbarPillBtn(c, "tb-layout", iconLayout, i18n.T("布局"), st.LayoutOpen, pal)
			if lBtn.Clicked() {
				open := !st.LayoutOpen
				st.CloseToolbarPopovers()
				st.LayoutOpen = open
			}
			if st.LayoutOpen {
				layoutPopover(c, lBtn, st, view, db, opts, pal)
			}

			if opts.Embedded {
				if opts.HideViewTabs {
					ui.Spacer(c)
				}
				renderHeaderRightActions(c, st, db, view, opts, pal)
			}
		})
	})
}

func renderBulkActionBar(c *ui.Context, st *State, db *database.Database, res *database.QueryResult, opts Options, pal Palette) {
	selected := st.SelectedRecords(res.Rows)
	if len(selected) == 0 {
		return
	}
	bar := ui.Row(c.Key("bulk-bar")).Margin(0, 22, 8, 22).Padding(6, 12).Radius(10).
		Background(pal.AccentSoft).Border(1, pal.Accent.Alpha(0.24)).Gap(10).AlignItems(ui.Center).
		Transition(ui.ElementTransition{
			Animation:    &dbSpringSnappy,
			Colors:       true,
			ExitDuration: 120 * time.Millisecond,
			Enter:        &ui.Motion{Y: -6, Scale: 0.98, Opacity: 0.75},
			Exit:         &ui.Motion{Y: -4, Scale: 0.98, Opacity: 0},
		})
	bar.Children(func() {
		ui.Text(c, fmt.Sprintf(i18n.T("已选择 %d 项"), len(selected))).FontSize(12.5).FontWeight(600).TextColor(pal.Accent)

		// Offer quick batch status/select edit if available.
		for _, p := range db.Properties {
			if (p.Type == database.PropStatus || p.Type == database.PropSelect) && len(p.Options) > 0 {
				prop := p
				btn := ui.Button(c.Key("bulk-set-"+prop.ID), i18n.T("设置")+prop.Name).Height(24).Padding(0, 8)
				btn.PopoverMenuButton(func(m *ui.Menu) {
					for _, opt := range prop.Options {
						optName := opt.Name
						if m.Item(optName).Chosen() {
							edits := make([]database.CellEdit, 0, len(selected))
							for _, r := range selected {
								edits = append(edits, database.CellEdit{
									RecordID: r.ID,
									PropID:   prop.ID,
									Value:    database.TextValue(optName),
								})
							}
							if affected, err := st.Store.SetCellsBatch(edits); err == nil && opts.OnChanged != nil {
								opts.OnChanged(affected)
							}
						}
					}
				})
				break
			}
		}

		ui.Spacer(c)

		if ui.Button(c.Key("bulk-trash"), i18n.T("移到废纸篓")).Height(24).Padding(0, 10).TextColor(pal.Danger).Clicked() {
			var trashPaths []string
			for _, r := range selected {
				if bp, err := st.Store.DeleteRecord(r.ID); err == nil && bp != "" {
					trashPaths = append(trashPaths, bp)
				}
			}
			st.ClearSelection()
			if len(trashPaths) > 0 && opts.OnTrashRecords != nil {
				opts.OnTrashRecords(trashPaths)
			}
			if opts.OnChanged != nil {
				opts.OnChanged(nil)
			}
		}
		if ui.Button(c.Key("bulk-clear"), i18n.T("取消选择")).Height(24).Padding(0, 8).Clicked() {
			st.ClearSelection()
		}
	})
}

func renderPlannedViewPlaceholder(c *ui.Context, st *State, view database.ViewConfig, db *database.Database, pal Palette) {
	ui.Column(c.Key("view-placeholder")).Grow(1).Center().Gap(12).Padding(32).Children(func() {
		ui.Box(c).Size(44, 44).Radius(11).Background(pal.Surface).Center().Children(func() {
			ui.Icon(c, ViewIcon(view.Type)).FontSize(22).TextColor(pal.Accent)
		})
		ui.Text(c, viewTypeLabel(view.Type)).FontSize(17).FontWeight(600).TextColor(pal.Text)
		ui.Text(c, fmt.Sprintf(i18n.T("「%s」视图将在后续阶段实现，可先切换到表格视图浏览与编辑全部数据。"), viewTypeLabel(view.Type))).
			FontSize(13).TextColor(pal.TextMuted).MaxWidth(420).TextAlign(ui.Center)
		for _, v := range db.Views {
			if v.Type == database.ViewTable {
				tableID := v.ID
				if ui.PrimaryButton(c.Key("back-to-table"), i18n.T("切回表格视图")).Height(30).Padding(0, 14).Clicked() {
					st.SetActiveView(tableID)
				}
				break
			}
		}
	})
}

func toolbarPillBtn(c *ui.Context, key string, icon *ui.SVG, label string, active bool, pal Palette) ui.Element {
	btn := ui.Row(c.Key(key)).Height(27).Padding(0, 9).Radius(7).Gap(5).Shrink(0).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().
		Role(ui.RoleButton).Label(label).
		Border(1, ui.Transparent).
		Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
	switch {
	case active:
		btn.Background(pal.AccentSoft).Border(1, pal.Accent.Alpha(0.20))
	case btn.Hovered():
		btn.Background(pal.RowHover)
	}
	applyPressSpring(btn, false)
	ink := pal.TextMuted
	if active {
		ink = pal.Accent
	} else if btn.Hovered() {
		ink = pal.Text
	}
	btn.Children(func() {
		ui.Icon(c, icon).FontSize(13).TextColor(ink)
		ui.Text(c, label).FontSize(12.5).TextColor(ink).NoWrap()
	})
	return btn
}

func toolbarIconBtn(c *ui.Context, key string, icon *ui.SVG, label string, disabled, active bool, pal Palette) ui.Element {
	btn := ui.Box(c.Key(key)).Size(27, 27).Radius(7).Center().
		Focusable().Role(ui.RoleButton).Label(label).Tooltip(label).Disabled(disabled).
		Border(1, ui.Transparent).
		Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
	ink := pal.TextMuted
	switch {
	case disabled:
		ink = pal.TextMuted.Alpha(0.32)
	case active:
		btn.Cursor(ui.CursorPointer).Background(pal.AccentSoft).Border(1, pal.Accent.Alpha(0.20))
		ink = pal.Accent
	default:
		btn.Cursor(ui.CursorPointer)
		if btn.Hovered() {
			btn.Background(pal.RowHover)
			ink = pal.Text
		}
	}
	applyPressSpring(btn, disabled)
	btn.Children(func() { ui.Icon(c, icon).FontSize(13.5).TextColor(ink) })
	return btn
}

func createRecordWithDefaults(st *State, db *database.Database, groupProp string, groupVal database.Value, opts Options) {
	var initial map[string]database.Value
	if groupProp != "" && !groupVal.IsEmpty() {
		initial = map[string]database.Value{groupProp: groupVal}
	}
	createRecordWithInitial(st, db, initial, opts)
}

func createRecordWithInitial(st *State, db *database.Database, initial map[string]database.Value, opts Options) {
	if st == nil || st.Store == nil || opts.ReadOnly {
		return
	}
	rec, err := st.Store.CreateRecord(i18n.T("未命名记录"), initial, "")
	if err != nil {
		if opts.OnNotify != nil {
			opts.OnNotify(err.Error())
		}
		return
	}
	st.Motion.TrackCreated(rec.ID)
	if opts.OnAfterCreate != nil {
		opts.OnAfterCreate(rec)
	}
	if opts.OnChanged != nil {
		var affected []string
		if rec.Path != "" {
			affected = []string{rec.Path}
		}
		opts.OnChanged(affected)
	}
	if tp := db.TitleProperty(); tp != nil {
		st.StartEditCell(rec.ID, tp.ID, rec.Title)
	}
}

func trashSingleRecord(st *State, rec *database.Record, opts Options) {
	if st == nil || rec == nil || opts.ReadOnly {
		return
	}
	bp, err := st.Store.DeleteRecord(rec.ID)
	if err != nil {
		if opts.OnNotify != nil {
			opts.OnNotify(err.Error())
		}
		return
	}
	delete(st.SelectedIDs, rec.ID)
	if bp != "" && opts.OnTrashRecords != nil {
		opts.OnTrashRecords([]string{bp})
	}
	if opts.OnChanged != nil {
		opts.OnChanged(nil)
	}
}
