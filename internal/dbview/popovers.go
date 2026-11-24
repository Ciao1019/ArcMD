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

func cellPopover(
	c *ui.Context,
	anchor ui.Element,
	st *State,
	rec *database.Record,
	prop database.Property,
	opts Options,
	pal Palette,
) {
	ui.Popover(c.Key("cell-pop:"+rec.ID+":"+prop.ID), anchor, &st.EditPopoverOpen, func() {
		switch prop.Type {
		case database.PropSelect, database.PropMultiSelect, database.PropStatus:
			optionCellPopover(c, st, rec, prop, opts, pal)
		case database.PropDate:
			dateCellPopover(c, st, rec, prop, opts, pal)
		case database.PropRelation:
			relationCellPopover(c, st, rec, prop, opts, pal)
		case database.PropPerson, database.PropFiles:
			listCellPopover(c, st, rec, prop, opts, pal)
		}
	})
}

func optionCellPopover(
	c *ui.Context,
	st *State,
	rec *database.Record,
	prop database.Property,
	opts Options,
	pal Palette,
) {
	val := rec.ValueFor(prop)
	isMulti := prop.Type == database.PropMultiSelect

	selectedMap := map[string]bool{}
	if isMulti {
		for _, item := range val.List {
			selectedMap[strings.ToLower(item)] = true
		}
	} else if !val.IsEmpty() {
		selectedMap[strings.ToLower(val.Text)] = true
	}

	commitOption := func(name string) {
		if isMulti {
			cur := slices.Clone(val.List)
			idx := slices.IndexFunc(cur, func(x string) bool { return strings.EqualFold(x, name) })
			if idx >= 0 {
				cur = slices.Delete(cur, idx, idx+1)
			} else {
				cur = append(cur, name)
			}
			st.setCellAndNotify(rec.ID, prop.ID, database.ListValue(cur...), opts)
		} else {
			st.setCellAndNotify(rec.ID, prop.ID, database.TextValue(name), opts)
			st.EditPopoverOpen = false
		}
	}

	createAndSelect := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		updated := prop.Clone()
		exists := false
		for _, opt := range updated.Options {
			if strings.EqualFold(opt.Name, name) {
				name = opt.Name
				exists = true
				break
			}
		}
		if !exists {
			newOpt := database.SelectOption{
				ID:    "opt_" + database.NewID()[:6],
				Name:  name,
				Color: ensureOptionColor(len(updated.Options)),
			}
			if prop.Type == database.PropStatus {
				newOpt.Group = database.StatusTodo
			}
			updated.Options = append(updated.Options, newOpt)
			if _, err := st.Store.UpdateProperty(updated); err == nil {
				if opts.OnChanged != nil {
					opts.OnChanged(nil)
				}
			}
		}
		st.EditOptionQuery = ""
		commitOption(name)
	}

	ui.Column(c.Key("opt-pop")).Width(248).Gap(6).Padding(4).Children(func() {
		in := ui.TextInput(c.Key("opt-search"), &st.EditOptionQuery).
			Placeholder(i18n.T("搜索或输入新选项…")).Label(i18n.T("选项搜索")).
			Height(30).Padding(4, 8).Radius(6).Background(pal.Surface).TextColor(pal.Text).AutoFocus()
		if in.Submitted() {
			createAndSelect(st.EditOptionQuery)
			return
		}

		q := strings.ToLower(strings.TrimSpace(st.EditOptionQuery))
		exactMatch := false
		for _, opt := range prop.Options {
			if strings.EqualFold(opt.Name, strings.TrimSpace(st.EditOptionQuery)) {
				exactMatch = true
				break
			}
		}

		listBox := ui.Scroll(c.Key("opt-scroll")).MaxHeight(240).MinWidth(0)
		listBox.Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				renderOptionRow := func(opt database.SelectOption, idx int) {
					picked := selectedMap[strings.ToLower(opt.Name)]
					row := ui.Row(c.Key(fmt.Sprintf("opt:%s:%d", opt.ID, idx))).
						Height(30).Padding(0, 6).Radius(6).Gap(6).AlignItems(ui.Center).Cursor(ui.CursorPointer)
					if row.Hovered() {
						row.Background(pal.RowHover)
					}
					row.Children(func() {
						renderTagPill(c, opt.Name, opt.Color, prop.Type == database.PropStatus, pal)
						ui.Spacer(c)
						if picked {
							ui.Icon(c, iconCheck).FontSize(13).TextColor(pal.Accent)
						}
						menuBtn := ui.Box(c.Key("opt-more:"+opt.ID)).Size(22, 22).Radius(5).Center().Cursor(ui.CursorPointer).
							Role(ui.RoleMenuButton).Label(i18n.T("选项设置"))
						if menuBtn.Hovered() {
							menuBtn.Background(pal.Surface)
						}
						menuBtn.Children(func() {
							ui.Text(c, "•••").FontSize(11).TextColor(pal.TextMuted)
						})
						menuBtn.PopoverMenuButton(func(m *ui.Menu) {
							m.Submenu(i18n.T("标签颜色"), func(sub *ui.Menu) {
								for _, col := range database.OptionColors {
									colName := col
									if sub.Item(colorLabel(colName)).Checked(opt.Color == colName).Chosen() {
										updated := prop.Clone()
										if idx < len(updated.Options) {
											updated.Options[idx].Color = colName
											_, _ = st.Store.UpdateProperty(updated)
											if opts.OnChanged != nil {
												opts.OnChanged(nil)
											}
										}
									}
								}
							})
							if prop.Type == database.PropStatus {
								m.Submenu(i18n.T("所属状态组"), func(sub *ui.Menu) {
									for _, sg := range []struct {
										g     database.StatusGroup
										label string
									}{
										{database.StatusTodo, i18n.T("未开始")},
										{database.StatusInProgress, i18n.T("进行中")},
										{database.StatusComplete, i18n.T("已完成")},
									} {
										if sub.Item(sg.label).Checked(opt.Group == sg.g).Chosen() {
											updated := prop.Clone()
											if idx < len(updated.Options) {
												updated.Options[idx].Group = sg.g
												_, _ = st.Store.UpdateProperty(updated)
												if opts.OnChanged != nil {
													opts.OnChanged(nil)
												}
											}
										}
									}
								})
							}
							m.Separator()
							if m.Item(i18n.T("删除选项")).Chosen() {
								updated := prop.Clone()
								if idx < len(updated.Options) {
									updated.Options = slices.Delete(updated.Options, idx, idx+1)
									_, _ = st.Store.UpdateProperty(updated)
									if opts.OnChanged != nil {
										opts.OnChanged(nil)
									}
								}
							}
						})
					})
					if row.Clicked() {
						commitOption(opt.Name)
					}
				}

				if prop.Type == database.PropStatus {
					groups := []struct {
						g     database.StatusGroup
						label string
					}{
						{database.StatusTodo, i18n.T("未开始")},
						{database.StatusInProgress, i18n.T("进行中")},
						{database.StatusComplete, i18n.T("已完成")},
					}
					for _, grp := range groups {
						var groupIndices []int
						for i, opt := range prop.Options {
							og := opt.Group
							if og == "" {
								og = database.StatusTodo
							}
							if og == grp.g && (q == "" || strings.Contains(strings.ToLower(opt.Name), q)) {
								groupIndices = append(groupIndices, i)
							}
						}
						if len(groupIndices) == 0 {
							continue
						}
						ui.Text(c, grp.label).FontSize(11).FontWeight(600).TextColor(pal.TextMuted).Padding(4, 6, 2, 6)
						for _, idx := range groupIndices {
							renderOptionRow(prop.Options[idx], idx)
						}
					}
				} else {
					for i, opt := range prop.Options {
						if q != "" && !strings.Contains(strings.ToLower(opt.Name), q) {
							continue
						}
						renderOptionRow(opt, i)
					}
				}

				if trimmed := strings.TrimSpace(st.EditOptionQuery); trimmed != "" && !exactMatch {
					createRow := ui.Row(c.Key("opt-create")).Height(30).Padding(0, 8).Radius(6).Gap(6).
						AlignItems(ui.Center).Cursor(ui.CursorPointer).Background(pal.AccentSoft)
					createRow.Children(func() {
						ui.Icon(c, iconPlus).FontSize(13).TextColor(pal.Accent)
						ui.Text(c, fmt.Sprintf(i18n.T("创建「%s」"), trimmed)).FontSize(12.5).TextColor(pal.Accent).SingleLine()
					})
					if createRow.Clicked() {
						createAndSelect(trimmed)
					}
				}
			})
		})

		if !val.IsEmpty() {
			clearRow := ui.Row(c.Key("opt-clear")).Height(28).Padding(0, 8).Radius(6).Gap(6).
				AlignItems(ui.Center).Cursor(ui.CursorPointer)
			if clearRow.Hovered() {
				clearRow.Background(pal.RowHover)
			}
			clearRow.Children(func() {
				ui.Icon(c, iconX).FontSize(12).TextColor(pal.TextMuted)
				ui.Text(c, i18n.T("清除")).FontSize(12).TextColor(pal.TextMuted)
			})
			if clearRow.Clicked() {
				st.setCellAndNotify(rec.ID, prop.ID, database.EmptyValue(), opts)
				st.EditPopoverOpen = false
			}
		}
	})
}

func dateCellPopover(
	c *ui.Context,
	st *State,
	rec *database.Record,
	prop database.Property,
	opts Options,
	pal Palette,
) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	commitDate := func() {
		start := strings.TrimSpace(st.EditDateStart)
		end := ""
		if st.EditDateHasEnd {
			end = strings.TrimSpace(st.EditDateEnd)
		}
		if start == "" {
			st.setCellAndNotify(rec.ID, prop.ID, database.EmptyValue(), opts)
			return
		}
		val := database.DateRangeValue(start, end, st.EditDateIncludeTime)
		st.setCellAndNotify(rec.ID, prop.ID, val, opts)
	}

	ui.Column(c.Key("date-pop")).Width(272).Gap(8).Padding(6).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			startIn := ui.TextInput(c.Key("date-start"), &st.EditDateStart).
				Placeholder("YYYY-MM-DD").Label(i18n.T("开始日期")).
				Grow(1).MinWidth(0).Height(28).Padding(2, 8).Radius(6).Background(pal.Surface).TextColor(pal.Text)
			if startIn.Focused() {
				st.EditDateEditingEnd = false
			}
			if startIn.Submitted() {
				commitDate()
			}
			if st.EditDateHasEnd {
				ui.Text(c, "→").FontSize(12).TextColor(pal.TextMuted)
				endIn := ui.TextInput(c.Key("date-end"), &st.EditDateEnd).
					Placeholder("YYYY-MM-DD").Label(i18n.T("结束日期")).
					Grow(1).MinWidth(0).Height(28).Padding(2, 8).Radius(6).Background(pal.Surface).TextColor(pal.Text)
				if endIn.Focused() {
					st.EditDateEditingEnd = true
				}
				if endIn.Submitted() {
					commitDate()
				}
			}
		})

		cal := ui.Calendar(c.Key("date-cal"), &st.EditDateCursor)
		if cal.Changed() {
			chosen := st.EditDateCursor.Format("2006-01-02")
			if st.EditDateHasEnd && st.EditDateEditingEnd {
				st.EditDateEnd = chosen
			} else {
				st.EditDateStart = chosen
			}
			commitDate()
		}

		ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(c, i18n.T("结束日期")).FontSize(12.5).TextColor(pal.Text)
			sw := ui.Switch(c.Key("date-has-end"), &st.EditDateHasEnd)
			if sw.Changed() {
				if !st.EditDateHasEnd {
					st.EditDateEnd = ""
				} else if st.EditDateEnd == "" {
					st.EditDateEnd = st.EditDateStart
				}
				commitDate()
			}
		})

		ui.Row(c).Gap(6).Children(func() {
			if ui.Button(c.Key("date-today"), i18n.T("今天")).Height(26).Padding(0, 8).Clicked() {
				d := now.Format("2006-01-02")
				st.EditDateCursor = now
				if st.EditDateHasEnd && st.EditDateEditingEnd {
					st.EditDateEnd = d
				} else {
					st.EditDateStart = d
				}
				commitDate()
			}
			if ui.Button(c.Key("date-tomorrow"), i18n.T("明天")).Height(26).Padding(0, 8).Clicked() {
				tm := now.AddDate(0, 0, 1)
				d := tm.Format("2006-01-02")
				st.EditDateCursor = tm
				if st.EditDateHasEnd && st.EditDateEditingEnd {
					st.EditDateEnd = d
				} else {
					st.EditDateStart = d
				}
				commitDate()
			}
			if ui.Button(c.Key("date-nextweek"), i18n.T("下周")).Height(26).Padding(0, 8).Clicked() {
				nw := now.AddDate(0, 0, 7)
				d := nw.Format("2006-01-02")
				st.EditDateCursor = nw
				if st.EditDateHasEnd && st.EditDateEditingEnd {
					st.EditDateEnd = d
				} else {
					st.EditDateStart = d
				}
				commitDate()
			}
			ui.Spacer(c)
			if ui.Button(c.Key("date-clear"), i18n.T("清除")).Height(26).Padding(0, 8).Clicked() {
				st.EditDateStart = ""
				st.EditDateEnd = ""
				st.EditDateHasEnd = false
				commitDate()
				st.EditPopoverOpen = false
			}
		})
	})
}

func listCellPopover(
	c *ui.Context,
	st *State,
	rec *database.Record,
	prop database.Property,
	opts Options,
	pal Palette,
) {
	val := rec.ValueFor(prop)
	items := slices.Clone(val.List)

	addItem := func() {
		item := strings.TrimSpace(st.EditListItemDraft)
		if item == "" {
			return
		}
		if !slices.Contains(items, item) {
			if prop.Type == database.PropRelation && prop.Relation != nil && prop.Relation.LimitOne {
				items = []string{item}
			} else {
				items = append(items, item)
			}
			st.setCellAndNotify(rec.ID, prop.ID, database.ListValue(items...), opts)
		}
		st.EditListItemDraft = ""
	}

	ui.Column(c.Key("list-pop")).Width(240).Gap(6).Padding(6).Children(func() {
		ui.Row(c).Gap(6).Children(func() {
			in := ui.TextInput(c.Key("list-add-in"), &st.EditListItemDraft).
				Placeholder(i18n.T("输入名称后回车添加…")).Label(prop.Name).
				Grow(1).MinWidth(0).Height(28).Padding(2, 8).Radius(6).Background(pal.Surface).TextColor(pal.Text).AutoFocus()
			if in.Submitted() {
				addItem()
			}
			if ui.Button(c.Key("list-add-btn"), i18n.T("添加")).Height(28).Padding(0, 8).Clicked() {
				addItem()
			}
		})

		if len(items) == 0 {
			ui.Text(c, i18n.T("暂无条目")).FontSize(12).TextColor(pal.TextMuted).Padding(4, 2)
		} else {
			for i, item := range items {
				idx := i
				ui.Row(c.Key(fmt.Sprintf("item:%d:%s", i, item))).Height(28).Padding(0, 6).Radius(6).
					AlignItems(ui.Center).Background(pal.Surface).Children(func() {
					ui.Text(c, item).FontSize(12.5).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)
					del := ui.Box(c.Key("rm")).Size(20, 20).Radius(4).Center().Cursor(ui.CursorPointer).
						Role(ui.RoleButton).Label(i18n.T("移除"))
					if del.Hovered() {
						del.Background(pal.RowHover)
					}
					del.Children(func() { ui.Icon(c, iconX).FontSize(11).TextColor(pal.TextMuted) })
					if del.Clicked() {
						next := slices.Delete(slices.Clone(items), idx, idx+1)
						st.setCellAndNotify(rec.ID, prop.ID, database.ListValue(next...), opts)
					}
				})
			}
		}
	})
}

func relationCellPopover(
	c *ui.Context,
	st *State,
	rec *database.Record,
	prop database.Property,
	opts Options,
	pal Palette,
) {
	val := rec.ValueFor(prop)
	items := slices.Clone(val.List)
	cands := st.Store.RelationCandidates(prop)
	targetDB := ""
	limitOne := false
	if prop.Relation != nil {
		targetDB = prop.Relation.TargetDB
		limitOne = prop.Relation.LimitOne
	}

	toggleRef := func(ref string) {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return
		}
		if idx := slices.IndexFunc(items, func(s string) bool { return strings.EqualFold(s, ref) }); idx >= 0 {
			next := slices.Delete(slices.Clone(items), idx, idx+1)
			st.setCellAndNotify(rec.ID, prop.ID, database.ListValue(next...), opts)
			return
		}
		var next []string
		if limitOne {
			next = []string{ref}
			st.EditPopoverOpen = false
		} else {
			next = append(slices.Clone(items), ref)
		}
		st.setCellAndNotify(rec.ID, prop.ID, database.ListValue(next...), opts)
	}

	candTitle := func(ref string) string {
		for _, cand := range cands {
			if strings.EqualFold(cand.Ref, ref) || strings.EqualFold(cand.Title, ref) || strings.EqualFold(cand.Path, ref) {
				if cand.Icon != "" {
					return cand.Icon + " " + cand.Title
				}
				return cand.Title
			}
		}
		return ref
	}

	openRef := func(ref string) {
		st.EditPopoverOpen = false
		if opts.OnOpenRelationTarget != nil {
			opts.OnOpenRelationTarget(targetDB, ref)
			return
		}
		if targetDB == "" && opts.OnOpenRecord != nil {
			if tr := database.MatchRelationRecord(st.Store.Records(), ref); tr != nil {
				opts.OnOpenRecord(tr, OpenSidePeek)
			}
		}
	}

	ui.Column(c.Key("rel-pop")).Width(282).Gap(8).Padding(6).Children(func() {
		ui.Text(c, i18n.T("已关联记录")).FontSize(12).FontWeight(600).TextColor(pal.TextMuted)
		if len(items) == 0 {
			ui.Text(c, i18n.T("尚未关联记录")).FontSize(12).TextColor(pal.TextMuted).Padding(0, 2)
		} else {
			ui.Column(c.Key("rel-linked")).Gap(3).Children(func() {
				for i, ref := range items {
					curRef := ref
					ui.Row(c.Key(fmt.Sprintf("rl:%d:%s", i, curRef))).Height(28).Padding(0, 8).Radius(6).
						Gap(6).AlignItems(ui.Center).Background(pal.AccentSoft).Children(func() {
						ui.Icon(c, iconPropRelation).FontSize(12).TextColor(pal.Accent)
						ui.Text(c, candTitle(curRef)).FontSize(12.5).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)

						if opts.OnOpenRelationTarget != nil || (targetDB == "" && opts.OnOpenRecord != nil) {
							goBtn := ui.Box(c.Key("open")).Size(20, 20).Radius(4).Center().Cursor(ui.CursorPointer).
								Role(ui.RoleButton).Label(i18n.T("打开") + ": " + curRef).Tooltip(i18n.T("打开记录"))
							if goBtn.Hovered() {
								goBtn.Background(pal.RowHover)
							}
							goBtn.Children(func() { ui.Icon(c, iconExternal).FontSize(11).TextColor(pal.Accent) })
							if goBtn.Clicked() {
								openRef(curRef)
								return
							}
						}

						rm := ui.Box(c.Key("rm")).Size(20, 20).Radius(4).Center().Cursor(ui.CursorPointer).
							Role(ui.RoleButton).Label(i18n.T("移除") + ": " + curRef)
						if rm.Hovered() {
							rm.Background(pal.RowHover)
						}
						rm.Children(func() { ui.Icon(c, iconX).FontSize(11).TextColor(pal.TextMuted) })
						if rm.Clicked() {
							toggleRef(curRef)
						}
					})
				}
			})
		}

		in := ui.TextInput(c.Key("rel-search"), &st.RelationQuery).
			Placeholder(i18n.T("搜索关联记录…")).Label(i18n.T("搜索关联记录")).
			Height(30).Padding(4, 8).Radius(6).Background(pal.Surface).TextColor(pal.Text).AutoFocus()

		q := strings.ToLower(strings.TrimSpace(st.RelationQuery))
		var filtered []database.RelationCandidate
		for _, cand := range cands {
			if q == "" || strings.Contains(strings.ToLower(cand.Title), q) || strings.Contains(strings.ToLower(cand.Ref), q) {
				filtered = append(filtered, cand)
			}
		}
		if in.Submitted() {
			if len(filtered) > 0 {
				toggleRef(filtered[0].Ref)
			} else if raw := strings.TrimSpace(st.RelationQuery); raw != "" {
				toggleRef(raw)
			}
			st.RelationQuery = ""
			return
		}

		ui.Scroll(c.Key("rel-cand-scroll")).MaxHeight(196).MinWidth(0).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				if len(filtered) == 0 {
					ui.Text(c, i18n.T("无匹配的可关联记录")).FontSize(12).TextColor(pal.TextMuted).Padding(6, 4)
					return
				}
				for i, cand := range filtered {
					rc := cand
					linked := slices.ContainsFunc(items, func(s string) bool { return strings.EqualFold(s, rc.Ref) })
					row := ui.Row(c.Key(fmt.Sprintf("rc:%d:%s", i, rc.Ref))).Height(30).Padding(0, 8).Radius(6).
						Gap(6).AlignItems(ui.Center).Cursor(ui.CursorPointer).
						Role(ui.RoleOption).Selected(linked).Label(rc.Title)
					if row.Hovered() {
						row.Background(pal.RowHover)
					}
					row.Children(func() {
						if rc.Icon != "" {
							ui.Text(c, rc.Icon).FontSize(13).NoWrap()
						} else {
							ui.Icon(c, iconFileDoc).FontSize(12.5).TextColor(pal.TextMuted)
						}
						ui.Text(c, rc.Title).FontSize(12.5).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)
						if linked {
							ui.Icon(c, iconCheck).FontSize(13).TextColor(pal.Accent)
						}
					})
					if row.Clicked() {
						toggleRef(rc.Ref)
					}
				}
			})
		})
	})
}

func colorLabel(name string) string {
	switch name {
	case "default":
		return i18n.T("默认")
	case "gray":
		return i18n.T("灰色")
	case "brown":
		return i18n.T("棕色")
	case "orange":
		return i18n.T("橙色")
	case "yellow":
		return i18n.T("黄色")
	case "green":
		return i18n.T("绿色")
	case "blue":
		return i18n.T("蓝色")
	case "purple":
		return i18n.T("紫色")
	case "pink":
		return i18n.T("粉色")
	case "red":
		return i18n.T("红色")
	default:
		return name
	}
}
