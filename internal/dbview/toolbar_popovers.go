package dbview

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

func saveViewAndNotify(st *State, view database.ViewConfig, opts Options) {
	if err := st.Store.UpdateView(view); err != nil {
		if opts.OnNotify != nil {
			opts.OnNotify(err.Error())
		}
		return
	}
	if opts.OnChanged != nil {
		opts.OnChanged(nil)
	}
}

func filterPopover(
	c *ui.Context,
	anchor ui.Element,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	opts Options,
	pal Palette,
) {
	ui.Popover(c.Key("pop-filter"), anchor, &st.FilterOpen, func() {
		fg := view.Filter.Clone()
		if fg == nil {
			fg = &database.FilterGroup{Conjunction: "and"}
		}

		ui.Column(c.Key("filter-box")).Width(470).Gap(8).Padding(6).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, i18n.T("筛选条件")).FontSize(13).FontWeight(600).TextColor(pal.Text)
				ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
					andBtn := ui.Button(c.Key("fg-and"), i18n.T("满足全部 (AND)")).Height(24).Padding(0, 8)
					orBtn := ui.Button(c.Key("fg-or"), i18n.T("满足任一 (OR)")).Height(24).Padding(0, 8)
					if fg.Conjunction != "or" {
						andBtn.Background(pal.AccentSoft).TextColor(pal.Accent)
					} else {
						orBtn.Background(pal.AccentSoft).TextColor(pal.Accent)
					}
					if andBtn.Clicked() && fg.Conjunction != "and" {
						fg.Conjunction = "and"
						view.Filter = fg
						saveViewAndNotify(st, view, opts)
					}
					if orBtn.Clicked() && fg.Conjunction != "or" {
						fg.Conjunction = "or"
						view.Filter = fg
						saveViewAndNotify(st, view, opts)
					}
				})
			})

			if len(fg.Rules) == 0 {
				ui.Text(c, i18n.T("暂无筛选条件，点击下方按钮添加。")).FontSize(12).TextColor(pal.TextMuted).Padding(4, 2)
			}

			for i := range fg.Rules {
				idx := i
				rule := &fg.Rules[idx]
				prop, propIdx := db.PropertyByID(rule.PropertyID)
				if (propIdx < 0 || prop == nil) && len(db.Properties) > 0 {
					prop = &db.Properties[0]
					rule.PropertyID = prop.ID
				}
				if prop == nil {
					continue
				}

				ui.Row(c.Key("frule:" + rule.ID)).Gap(6).AlignItems(ui.Center).Children(func() {
					// Property selector.
					propBtn := ui.ButtonBase(c.Key("fprop")).Height(28).Padding(0, 8).Radius(6).
						Background(pal.Surface).Cursor(ui.CursorPointer).Width(118).AlignItems(ui.Center).
						Role(ui.RoleMenuButton).Label(i18n.T("选择属性"))
					propBtn.Children(func() {
						ui.Icon(c, PropertyIcon(prop.Type)).FontSize(12).TextColor(pal.TextMuted)
						ui.Box(c).Width(4)
						ui.Text(c, prop.Name).FontSize(12).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)
					})
					propBtn.PopoverMenuButton(func(m *ui.Menu) {
						for _, candidate := range db.Properties {
							cand := candidate
							if m.Item(cand.Name).Checked(cand.ID == prop.ID).Chosen() {
								rule.PropertyID = cand.ID
								rule.Op = defaultFilterOpFor(cand.Type)
								rule.Value = database.EmptyValue()
								delete(st.FilterDrafts, rule.ID)
								view.Filter = fg
								saveViewAndNotify(st, view, opts)
							}
						}
					})

					// Operator selector.
					ops := filterOpsForType(prop.Type)
					opBtn := ui.ButtonBase(c.Key("fop")).Height(28).Padding(0, 8).Radius(6).
						Background(pal.Surface).Cursor(ui.CursorPointer).Width(118).AlignItems(ui.Center).
						Role(ui.RoleMenuButton).Label(i18n.T("选择条件"))
					opBtn.Children(func() {
						ui.Text(c, filterOpLabel(rule.Op)).FontSize(12).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)
						ui.Icon(c, iconChevronDown).FontSize(12).TextColor(pal.TextMuted)
					})
					opBtn.PopoverMenuButton(func(m *ui.Menu) {
						for _, op := range ops {
							opChoice := op
							if m.Item(filterOpLabel(opChoice)).Checked(rule.Op == opChoice).Chosen() {
								rule.Op = opChoice
								if opChoice == database.OpDateWithin && rule.Value.Text == "" {
									rule.Value = database.TextValue("today")
								}
								if (opChoice == database.OpStatusGroupIs || opChoice == database.OpStatusGroupNot) && rule.Value.Text == "" {
									rule.Value = database.TextValue(string(database.StatusTodo))
								}
								view.Filter = fg
								saveViewAndNotify(st, view, opts)
							}
						}
					})

					// Value input / picker.
					filterRuleValueEditor(c, st, rule, *prop, func() {
						view.Filter = fg
						saveViewAndNotify(st, view, opts)
					}, pal)

					del := ui.Box(c.Key("fdel")).Size(24, 24).Radius(5).Center().Cursor(ui.CursorPointer).
						Role(ui.RoleButton).Label(i18n.T("删除条件"))
					if del.Hovered() {
						del.Background(pal.RowHover)
					}
					del.Children(func() { ui.Icon(c, iconX).FontSize(12).TextColor(pal.TextMuted) })
					if del.Clicked() {
						fg.Rules = slices.Delete(fg.Rules, idx, idx+1)
						delete(st.FilterDrafts, rule.ID)
						if len(fg.Rules) == 0 && len(fg.Groups) == 0 {
							view.Filter = nil
						} else {
							view.Filter = fg
						}
						saveViewAndNotify(st, view, opts)
					}
				})
			}

			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if ui.Button(c.Key("f-add"), "+ "+i18n.T("添加筛选条件")).Height(28).Padding(0, 10).Clicked() {
					if len(db.Properties) > 0 {
						p0 := db.Properties[0]
						fg.Rules = append(fg.Rules, database.FilterRule{
							ID:         "fr_" + database.NewID()[:6],
							PropertyID: p0.ID,
							Op:         defaultFilterOpFor(p0.Type),
						})
						view.Filter = fg
						saveViewAndNotify(st, view, opts)
					}
				}
				ui.Spacer(c)
				if len(fg.Rules) > 0 || len(fg.Groups) > 0 {
					if ui.Button(c.Key("f-clear"), i18n.T("清空筛选")).Height(28).Padding(0, 10).Clicked() {
						clear(st.FilterDrafts)
						view.Filter = nil
						saveViewAndNotify(st, view, opts)
					}
				}
			})
		})
	})
}

func filterRuleValueEditor(
	c *ui.Context,
	st *State,
	rule *database.FilterRule,
	prop database.Property,
	commit func(),
	pal Palette,
) {
	switch rule.Op {
	case database.OpIsEmpty, database.OpIsNotEmpty, database.OpChecked, database.OpUnchecked:
		ui.Box(c).Grow(1).MinWidth(0)
		return
	case database.OpDateWithin:
		choices := []struct{ tok, label string }{
			{"today", i18n.T("今天")},
			{"yesterday", i18n.T("昨天")},
			{"tomorrow", i18n.T("明天")},
			{"past_7_days", i18n.T("过去 7 天")},
			{"next_7_days", i18n.T("未来 7 天")},
			{"this_week", i18n.T("本周")},
			{"this_month", i18n.T("本月")},
			{"last_month", i18n.T("上月")},
			{"past_30_days", i18n.T("过去 30 天")},
			{"next_30_days", i18n.T("未来 30 天")},
			{"this_year", i18n.T("今年")},
		}
		curLabel := rule.Value.Text
		for _, ch := range choices {
			if ch.tok == rule.Value.Text {
				curLabel = ch.label
				break
			}
		}
		if curLabel == "" {
			curLabel = i18n.T("选择范围")
		}
		btn := ui.ButtonBase(c.Key("fwithin")).Grow(1).MinWidth(0).Height(28).Padding(0, 8).Radius(6).
			Background(pal.Surface).Cursor(ui.CursorPointer).AlignItems(ui.Center).
			Role(ui.RoleMenuButton).Label(i18n.T("相对日期"))
		btn.Children(func() {
			ui.Text(c, curLabel).FontSize(12).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)
			ui.Icon(c, iconChevronDown).FontSize(12).TextColor(pal.TextMuted)
		})
		btn.PopoverMenuButton(func(m *ui.Menu) {
			for _, ch := range choices {
				choice := ch
				if m.Item(choice.label).Checked(rule.Value.Text == choice.tok).Chosen() {
					rule.Value = database.TextValue(choice.tok)
					commit()
				}
			}
		})
		return
	case database.OpStatusGroupIs, database.OpStatusGroupNot:
		groups := []struct {
			tok   database.StatusGroup
			label string
		}{
			{database.StatusTodo, i18n.T("未开始")},
			{database.StatusInProgress, i18n.T("进行中")},
			{database.StatusComplete, i18n.T("已完成")},
		}
		curLabel := i18n.T("选择状态组")
		for _, g := range groups {
			if string(g.tok) == rule.Value.Text {
				curLabel = g.label
				break
			}
		}
		btn := ui.ButtonBase(c.Key("fsgrp")).Grow(1).MinWidth(0).Height(28).Padding(0, 8).Radius(6).
			Background(pal.Surface).Cursor(ui.CursorPointer).AlignItems(ui.Center).
			Role(ui.RoleMenuButton).Label(i18n.T("状态组"))
		btn.Children(func() {
			ui.Text(c, curLabel).FontSize(12).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)
			ui.Icon(c, iconChevronDown).FontSize(12).TextColor(pal.TextMuted)
		})
		btn.PopoverMenuButton(func(m *ui.Menu) {
			for _, g := range groups {
				grp := g
				if m.Item(grp.label).Checked(rule.Value.Text == string(grp.tok)).Chosen() {
					rule.Value = database.TextValue(string(grp.tok))
					commit()
				}
			}
		})
		return
	}

	if (prop.Type == database.PropSelect || prop.Type == database.PropStatus || prop.Type == database.PropMultiSelect) && len(prop.Options) > 0 {
		cur := rule.Value.PlainString()
		if cur == "" {
			cur = i18n.T("选择选项…")
		}
		btn := ui.ButtonBase(c.Key("fopt")).Grow(1).MinWidth(0).Height(28).Padding(0, 8).Radius(6).
			Background(pal.Surface).Cursor(ui.CursorPointer).AlignItems(ui.Center).
			Role(ui.RoleMenuButton).Label(i18n.T("筛选值"))
		btn.Children(func() {
			ui.Text(c, cur).FontSize(12).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)
			ui.Icon(c, iconChevronDown).FontSize(12).TextColor(pal.TextMuted)
		})
		btn.PopoverMenuButton(func(m *ui.Menu) {
			for _, opt := range prop.Options {
				name := opt.Name
				selected := strings.EqualFold(rule.Value.Text, name) || slices.Contains(rule.Value.List, name)
				if m.Item(name).Checked(selected).Chosen() {
					if prop.Type == database.PropMultiSelect {
						next := toggleStringInSlice(slices.Clone(rule.Value.List), name)
						rule.Value = database.ListValue(next...)
					} else {
						rule.Value = database.TextValue(name)
					}
					commit()
				}
			}
		})
		return
	}

	draft, ok := st.FilterDrafts[rule.ID]
	if !ok {
		draft = rule.Value.PlainString()
		st.FilterDrafts[rule.ID] = draft
	}
	in := ui.TextInput(c.Key("fval"), &draft).
		Placeholder(i18n.T("输入值…")).Label(i18n.T("筛选值")).
		Grow(1).MinWidth(0).Height(28).Padding(2, 8).Radius(6).Background(pal.Surface).TextColor(pal.Text)
	if in.Changed() || in.Submitted() {
		st.FilterDrafts[rule.ID] = draft
		trimmed := strings.TrimSpace(draft)
		switch prop.Type {
		case database.PropNumber:
			if trimmed == "" {
				rule.Value = database.EmptyValue()
				commit()
			} else if n, err := strconv.ParseFloat(trimmed, 64); err == nil {
				rule.Value = database.NumberValue(n)
				commit()
			}
		case database.PropDate, database.PropCreatedTime, database.PropEditedTime:
			if trimmed == "" {
				rule.Value = database.EmptyValue()
				commit()
			} else if _, _, ok := database.ParseDateString(trimmed, time.UTC); ok {
				rule.Value = database.DateRangeValue(trimmed, "", false)
				commit()
			}
		default:
			rule.Value = database.TextValue(draft)
			commit()
		}
	}
}

func sortPopover(
	c *ui.Context,
	anchor ui.Element,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	opts Options,
	pal Palette,
) {
	ui.Popover(c.Key("pop-sort"), anchor, &st.SortOpen, func() {
		sorts := slices.Clone(view.Sorts)
		ui.Column(c.Key("sort-box")).Width(320).Gap(8).Padding(6).Children(func() {
			ui.Text(c, i18n.T("排序规则")).FontSize(13).FontWeight(600).TextColor(pal.Text)
			if len(sorts) == 0 {
				ui.Text(c, i18n.T("暂无排序规则，按默认顺序排列。")).FontSize(12).TextColor(pal.TextMuted)
			}
			for i := range sorts {
				idx := i
				sr := &sorts[idx]
				prop, _ := db.PropertyByID(sr.PropertyID)
				propLabel := sr.PropertyID
				propType := database.PropText
				if prop != nil {
					propLabel = prop.Name
					propType = prop.Type
				}
				ui.Row(c.Key(fmt.Sprintf("srule:%d", idx))).Gap(6).AlignItems(ui.Center).Children(func() {
					pBtn := ui.ButtonBase(c.Key("sprop")).Grow(1).MinWidth(0).Height(28).Padding(0, 8).Radius(6).
						Background(pal.Surface).Cursor(ui.CursorPointer).Gap(6).AlignItems(ui.Center).
						Role(ui.RoleMenuButton).Label(i18n.T("排序属性"))
					pBtn.Children(func() {
						ui.Icon(c, PropertyIcon(propType)).FontSize(12).TextColor(pal.TextMuted)
						ui.Text(c, propLabel).FontSize(12).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)
					})
					pBtn.PopoverMenuButton(func(m *ui.Menu) {
						for _, p := range db.Properties {
							cand := p
							if m.Item(cand.Name).Checked(cand.ID == sr.PropertyID).Chosen() {
								sorts[idx].PropertyID = cand.ID
								view.Sorts = sorts
								saveViewAndNotify(st, view, opts)
							}
						}
					})

					dirLabel := i18n.T("升序")
					if sr.Desc {
						dirLabel = i18n.T("降序")
					}
					if ui.Button(c.Key("sdir"), dirLabel).Height(28).Padding(0, 8).Clicked() {
						sorts[idx].Desc = !sorts[idx].Desc
						view.Sorts = sorts
						saveViewAndNotify(st, view, opts)
					}

					del := ui.Box(c.Key("sdel")).Size(24, 24).Radius(5).Center().Cursor(ui.CursorPointer).
						Role(ui.RoleButton).Label(i18n.T("删除排序"))
					if del.Hovered() {
						del.Background(pal.RowHover)
					}
					del.Children(func() { ui.Icon(c, iconX).FontSize(12).TextColor(pal.TextMuted) })
					if del.Clicked() {
						view.Sorts = slices.Delete(sorts, idx, idx+1)
						saveViewAndNotify(st, view, opts)
					}
				})
			}

			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if ui.Button(c.Key("s-add"), "+ "+i18n.T("添加排序")).Height(28).Padding(0, 10).Clicked() {
					if len(db.Properties) > 0 {
						picked := db.Properties[0].ID
						for _, p := range db.Properties {
							used := false
							for _, sr := range sorts {
								if sr.PropertyID == p.ID {
									used = true
									break
								}
							}
							if !used {
								picked = p.ID
								break
							}
						}
						view.Sorts = append(sorts, database.SortRule{PropertyID: picked})
						saveViewAndNotify(st, view, opts)
					}
				}
				ui.Spacer(c)
				if len(sorts) > 0 {
					if ui.Button(c.Key("s-clear"), i18n.T("清空排序")).Height(28).Padding(0, 10).Clicked() {
						view.Sorts = nil
						saveViewAndNotify(st, view, opts)
					}
				}
			})
		})
	})
}

func groupPopover(
	c *ui.Context,
	anchor ui.Element,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	opts Options,
	pal Palette,
) {
	ui.Popover(c.Key("pop-group"), anchor, &st.GroupOpen, func() {
		grp := view.Group.Clone()
		curName := i18n.T("不分组")
		var curProp *database.Property
		if grp != nil && grp.PropertyID != "" {
			if p, idx := db.PropertyByID(grp.PropertyID); idx >= 0 {
				curProp = p
				curName = p.Name
			}
		}

		ui.Column(c.Key("group-box")).Width(260).Gap(8).Padding(6).Children(func() {
			ui.Text(c, i18n.T("分组依据")).FontSize(13).FontWeight(600).TextColor(pal.Text)
			btn := ui.ButtonBase(c.Key("g-prop")).Height(30).Padding(0, 10).Radius(6).
				Background(pal.Surface).Cursor(ui.CursorPointer).AlignItems(ui.Center).
				Role(ui.RoleMenuButton).Label(i18n.T("分组属性"))
			btn.Children(func() {
				if curProp != nil {
					ui.Icon(c, PropertyIcon(curProp.Type)).FontSize(12).TextColor(pal.TextMuted)
					ui.Box(c).Width(6)
				}
				ui.Text(c, curName).FontSize(12.5).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)
				ui.Icon(c, iconChevronDown).FontSize(12).TextColor(pal.TextMuted)
			})
			btn.PopoverMenuButton(func(m *ui.Menu) {
				if m.Item(i18n.T("不分组")).Checked(curProp == nil).Chosen() {
					view.Group = nil
					saveViewAndNotify(st, view, opts)
				}
				m.Separator()
				for _, p := range db.Properties {
					cand := p
					if m.Item(cand.Name).Checked(curProp != nil && curProp.ID == cand.ID).Chosen() {
						next := &database.GroupConfig{PropertyID: cand.ID}
						if cand.Type == database.PropStatus {
							next.StatusBy = "option"
						} else if cand.Type == database.PropDate || cand.Type == database.PropCreatedTime || cand.Type == database.PropEditedTime {
							next.DateBy = "month"
						}
						view.Group = next
						saveViewAndNotify(st, view, opts)
					}
				}
			})

			if grp != nil && curProp != nil {
				if curProp.Type == database.PropStatus {
					ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
						ui.Text(c, i18n.T("状态分组方式")).FontSize(12).TextColor(pal.TextMuted)
						modeLabel := i18n.T("按选项")
						if grp.StatusBy == "group" {
							modeLabel = i18n.T("按状态组")
						}
						mb := ui.Button(c.Key("g-status-by"), modeLabel).Height(26).Padding(0, 8)
						mb.PopoverMenuButton(func(m *ui.Menu) {
							if m.Item(i18n.T("按选项")).Checked(grp.StatusBy != "group").Chosen() {
								grp.StatusBy = "option"
								view.Group = grp
								saveViewAndNotify(st, view, opts)
							}
							if m.Item(i18n.T("按状态组")).Checked(grp.StatusBy == "group").Chosen() {
								grp.StatusBy = "group"
								view.Group = grp
								saveViewAndNotify(st, view, opts)
							}
						})
					})
				}

				if curProp.Type == database.PropDate || curProp.Type == database.PropCreatedTime || curProp.Type == database.PropEditedTime {
					ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
						ui.Text(c, i18n.T("日期粒度")).FontSize(12).TextColor(pal.TextMuted)
						dateModes := []struct{ id, label string }{
							{"day", i18n.T("按天")},
							{"week", i18n.T("按周")},
							{"month", i18n.T("按月")},
							{"year", i18n.T("按年")},
							{"relative", i18n.T("按相对时间")},
						}
						dl := i18n.T("按天")
						for _, dm := range dateModes {
							if dm.id == grp.DateBy {
								dl = dm.label
								break
							}
						}
						dbBtn := ui.Button(c.Key("g-date-by"), dl).Height(26).Padding(0, 8)
						dbBtn.PopoverMenuButton(func(m *ui.Menu) {
							for _, dm := range dateModes {
								mode := dm
								if m.Item(mode.label).Checked(grp.DateBy == mode.id).Chosen() {
									grp.DateBy = mode.id
									view.Group = grp
									saveViewAndNotify(st, view, opts)
								}
							}
						})
					})
				}

				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, i18n.T("分组排序")).FontSize(12).TextColor(pal.TextMuted)
					sl := i18n.T("默认")
					switch grp.Sort {
					case "asc":
						sl = i18n.T("升序")
					case "desc":
						sl = i18n.T("降序")
					}
					sb := ui.Button(c.Key("g-sort"), sl).Height(26).Padding(0, 8)
					sb.PopoverMenuButton(func(m *ui.Menu) {
						for _, opt := range []struct{ id, label string }{
							{"manual", i18n.T("默认")},
							{"asc", i18n.T("升序")},
							{"desc", i18n.T("降序")},
						} {
							o := opt
							if m.Item(o.label).Checked(grp.Sort == o.id || (o.id == "manual" && grp.Sort == "")).Chosen() {
								grp.Sort = o.id
								view.Group = grp
								saveViewAndNotify(st, view, opts)
							}
						}
					})
				})

				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, i18n.T("隐藏空分组")).FontSize(12.5).TextColor(pal.Text)
					hideEmpty := grp.HideEmpty
					sw := ui.Switch(c.Key("g-hide-empty"), &hideEmpty)
					if sw.Changed() {
						grp.HideEmpty = hideEmpty
						view.Group = grp
						saveViewAndNotify(st, view, opts)
					}
				})
			}
		})
	})
}

func propertiesPopover(
	c *ui.Context,
	anchor ui.Element,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	opts Options,
	pal Palette,
) {
	ui.Popover(c.Key("pop-props"), anchor, &st.PropsOpen, func() {
		ordered := orderedViewProperties(db, &view)
		ui.Column(c.Key("props-box")).Width(260).Gap(6).Padding(6).Children(func() {
			ui.Text(c, i18n.T("显示属性")).FontSize(13).FontWeight(600).TextColor(pal.Text)
			ui.Scroll(c.Key("props-scroll")).MaxHeight(260).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					for i, item := range ordered {
						idx := i
						vp := item.vp
						prop := item.prop
						ui.Row(c.Key("vp:"+prop.ID)).Height(30).Padding(0, 6).Radius(6).Gap(6).
							AlignItems(ui.Center).Children(func() {
							ui.Icon(c, PropertyIcon(prop.Type)).FontSize(13).TextColor(pal.TextMuted)
							ui.Text(c, prop.Name).FontSize(12.5).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)

							if !opts.ReadOnly && (prop.Type == database.PropRelation || prop.Type == database.PropRollup || prop.Type == database.PropFormula || prop.Type == database.PropNumber) {
								cfgBtn := ui.Box(c.Key("cfg")).Size(22, 22).Radius(4).Center().Cursor(ui.CursorPointer).
									Role(ui.RoleButton).Label(i18n.T("配置属性") + ": " + prop.Name).Tooltip(i18n.T("编辑属性配置"))
								if cfgBtn.Hovered() {
									cfgBtn.Background(pal.RowHover)
								}
								cfgBtn.Children(func() { ui.Icon(c, iconSettings).FontSize(12).TextColor(pal.TextMuted) })
								if cfgBtn.Clicked() {
									st.OpenPropertyConfig(prop)
									return
								}
							}

							if idx > 0 {
								up := ui.Box(c.Key("up")).Size(22, 22).Radius(4).Center().Cursor(ui.CursorPointer).
									Role(ui.RoleButton).Label(i18n.T("上移"))
								if up.Hovered() {
									up.Background(pal.RowHover)
								}
								up.Children(func() { ui.Icon(c, iconArrowUp).FontSize(11).TextColor(pal.TextMuted) })
								if up.Clicked() {
									nextVPs := extractViewProps(ordered)
									nextVPs[idx-1], nextVPs[idx] = nextVPs[idx], nextVPs[idx-1]
									view.Properties = nextVPs
									saveViewAndNotify(st, view, opts)
								}
							}
							if idx+1 < len(ordered) {
								dn := ui.Box(c.Key("dn")).Size(22, 22).Radius(4).Center().Cursor(ui.CursorPointer).
									Role(ui.RoleButton).Label(i18n.T("下移"))
								if dn.Hovered() {
									dn.Background(pal.RowHover)
								}
								dn.Children(func() { ui.Icon(c, iconArrowDown).FontSize(11).TextColor(pal.TextMuted) })
								if dn.Clicked() {
									nextVPs := extractViewProps(ordered)
									nextVPs[idx], nextVPs[idx+1] = nextVPs[idx+1], nextVPs[idx]
									view.Properties = nextVPs
									saveViewAndNotify(st, view, opts)
								}
							}

							visIcon := iconEye
							if !vp.Visible {
								visIcon = iconEyeOff
							}
							eye := ui.Box(c.Key("vis")).Size(24, 24).Radius(5).Center().Cursor(ui.CursorPointer).
								Disabled(prop.Type == database.PropTitle).
								Role(ui.RoleButton).Label(i18n.T("切换显示"))
							if eye.Hovered() && prop.Type != database.PropTitle {
								eye.Background(pal.RowHover)
							}
							eye.Children(func() { ui.Icon(c, visIcon).FontSize(13).TextColor(pal.TextMuted) })
							if eye.Clicked() && prop.Type != database.PropTitle {
								nextVPs := extractViewProps(ordered)
								nextVPs[idx].Visible = !nextVPs[idx].Visible
								view.Properties = nextVPs
								saveViewAndNotify(st, view, opts)
							}
						})
					}
				})
			})
			if !opts.ReadOnly {
				if ui.Button(c.Key("vp-add-prop"), "+ "+i18n.T("新建属性")).Height(28).Padding(0, 10).Clicked() {
					st.PropsOpen = false
					st.AddPropOpen = true
					st.NewPropName = ""
				}
			}
		})
	})
}

func addPropertyPopover(
	c *ui.Context,
	anchor ui.Element,
	st *State,
	db *database.Database,
	opts Options,
	pal Palette,
) {
	ui.Popover(c.Key("pop-add-prop"), anchor, &st.AddPropOpen, func() {
		create := func() {
			name := strings.TrimSpace(st.NewPropName)
			if name == "" {
				name = propertyTypeLabel(st.NewPropType)
			}
			pt := st.NewPropType
			if !database.ValidPropertyType(pt) || pt == database.PropTitle {
				pt = database.PropText
			}
			key := uniquePropKey(db, name)
			p := database.Property{
				ID:   "prop_" + database.NewID()[:8],
				Key:  key,
				Name: name,
				Type: pt,
			}
			if pt == database.PropDate {
				p.Date = &database.DateConfig{EndKey: key + "_end"}
			}
			if pt == database.PropUniqueID {
				p.UniqueID = &database.UniqueIDConfig{Prefix: "ID", Next: 1}
			}
			if pt == database.PropRelation {
				p.Relation = &database.RelationConfig{}
			}
			if pt == database.PropRollup {
				p.Rollup = &database.RollupConfig{Calculate: database.AggCountAll}
			}
			if pt == database.PropFormula {
				p.Formula = &database.FormulaConfig{Expression: ""}
			}
			if pt == database.PropStatus {
				p.Options = []database.SelectOption{
					{ID: "st_" + database.NewID()[:4], Name: "未开始", Color: "gray", Group: database.StatusTodo},
					{ID: "st_" + database.NewID()[:4], Name: "进行中", Color: "blue", Group: database.StatusInProgress},
					{ID: "st_" + database.NewID()[:4], Name: "已完成", Color: "green", Group: database.StatusComplete},
				}
			}
			if err := st.Store.AddProperty(p); err != nil {
				if opts.OnNotify != nil {
					opts.OnNotify(err.Error())
				}
				return
			}
			st.AddPropOpen = false
			st.NewPropName = ""
			if pt == database.PropRelation || pt == database.PropRollup || pt == database.PropFormula {
				st.OpenPropertyConfig(p)
			}
			if opts.OnChanged != nil {
				opts.OnChanged(nil)
			}
		}

		ui.Column(c.Key("add-prop-box")).Width(240).Gap(8).Padding(6).Children(func() {
			ui.Text(c, i18n.T("新建属性")).FontSize(13).FontWeight(600).TextColor(pal.Text)
			in := ui.TextInput(c.Key("new-prop-name"), &st.NewPropName).
				Placeholder(i18n.T("属性名称…")).Label(i18n.T("属性名称")).
				Height(30).Padding(4, 8).Radius(6).Background(pal.Surface).TextColor(pal.Text).AutoFocus()
			if in.Submitted() {
				create()
				return
			}

			typeBtn := ui.ButtonBase(c.Key("new-prop-type")).Height(30).Padding(0, 10).Radius(6).
				Background(pal.Surface).Cursor(ui.CursorPointer).Gap(6).AlignItems(ui.Center).
				Role(ui.RoleMenuButton).Label(i18n.T("属性类型"))
			typeBtn.Children(func() {
				ui.Icon(c, PropertyIcon(st.NewPropType)).FontSize(13).TextColor(pal.TextMuted)
				ui.Text(c, propertyTypeLabel(st.NewPropType)).FontSize(12.5).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)
				ui.Icon(c, iconChevronDown).FontSize(12).TextColor(pal.TextMuted)
			})
			typeBtn.PopoverMenuButton(func(m *ui.Menu) {
				for _, pt := range creatablePropertyTypes() {
					cand := pt
					if m.Item(propertyTypeLabel(cand)).Checked(st.NewPropType == cand).Chosen() {
						st.NewPropType = cand
					}
				}
			})

			ui.Row(c).Justify(ui.End).Gap(6).Children(func() {
				if ui.Button(c.Key("np-cancel"), i18n.T("取消")).Height(28).Padding(0, 10).Clicked() {
					st.AddPropOpen = false
				}
				if ui.PrimaryButton(c.Key("np-ok"), i18n.T("创建")).Height(28).Padding(0, 12).Clicked() {
					create()
				}
			})
		})
	})
}

func newViewPopover(
	c *ui.Context,
	anchor ui.Element,
	st *State,
	db *database.Database,
	opts Options,
	pal Palette,
) {
	ui.Popover(c.Key("pop-new-view"), anchor, &st.NewViewOpen, func() {
		create := func() {
			vt := st.NewViewType
			if !database.ValidViewType(vt) {
				vt = database.ViewTable
			}
			name := strings.TrimSpace(st.NewViewName)
			if name == "" {
				name = viewTypeLabel(vt)
			}
			var vps []database.ViewProperty
			for _, p := range db.Properties {
				vps = append(vps, database.ViewProperty{
					PropertyID: p.ID,
					Visible:    true,
					Width:      p.Width,
					Wrap:       p.Wrap,
				})
			}
			vc := database.ViewConfig{
				ID:         "view_" + database.NewID()[:8],
				Name:       name,
				Type:       vt,
				OpenMode:   OpenSidePeek,
				Properties: vps,
			}
			switch vt {
			case database.ViewTable:
				vc.Table = &database.TableViewConfig{FreezeFirst: true, ShowIcon: true}
			case database.ViewBoard:
				if gp := defaultBoardGroupProp(db); gp != nil {
					vc.Group = &database.GroupConfig{PropertyID: gp.ID}
					if gp.Type == database.PropStatus {
						vc.Group.StatusBy = "option"
					} else if gp.Type == database.PropDate || gp.Type == database.PropCreatedTime || gp.Type == database.PropEditedTime {
						vc.Group.DateBy = "month"
					}
				}
				vc.Board = &database.BoardViewConfig{
					CardSize:    "medium",
					CoverSource: "none",
					ColorColumn: true,
					ShowCount:   true,
				}
			case database.ViewGallery:
				vc.Gallery = &database.GalleryViewConfig{
					CardSize:    "medium",
					CoverSource: "cover",
					ShowIcon:    true,
				}
			case database.ViewCalendar:
				ensureCalendarConfig(&vc, db)
			case database.ViewTimeline:
				ensureTimelineConfig(&vc, db)
			case database.ViewChart:
				ensureChartConfig(&vc, db)
			case database.ViewFeed:
				ensureFeedConfig(&vc)
			case database.ViewForm:
				ensureFormConfig(&vc, db)
			}
			if err := st.Store.AddView(vc, true); err != nil {
				if opts.OnNotify != nil {
					opts.OnNotify(err.Error())
				}
				return
			}
			st.NewViewOpen = false
			st.NewViewName = ""
			if opts.OnChanged != nil {
				opts.OnChanged(nil)
			}
		}

		ui.Column(c.Key("new-view-box")).Width(280).Gap(8).Padding(6).Children(func() {
			ui.Text(c, i18n.T("新建视图")).FontSize(13).FontWeight(600).TextColor(pal.Text)
			in := ui.TextInput(c.Key("nv-name"), &st.NewViewName).
				Placeholder(i18n.T("视图名称…")).Label(i18n.T("视图名称")).
				Height(30).Padding(4, 8).Radius(6).Background(pal.Surface).TextColor(pal.Text).AutoFocus()
			if in.Submitted() {
				create()
				return
			}

			viewTypes := []database.ViewType{
				database.ViewTable, database.ViewBoard, database.ViewTimeline,
				database.ViewCalendar, database.ViewList, database.ViewGallery,
				database.ViewChart, database.ViewFeed, database.ViewForm,
			}
			ui.Row(c).Wrap().Gap(4).Children(func() {
				for _, vt := range viewTypes {
					cand := vt
					picked := st.NewViewType == cand
					chip := ui.Row(c.Key("vt:"+string(cand))).Width(84).Height(30).Padding(0, 6).Radius(6).
						Gap(5).AlignItems(ui.Center).Cursor(ui.CursorPointer)
					if picked {
						chip.Background(pal.AccentSoft).Border(1, pal.Accent)
					} else {
						chip.Background(pal.Surface)
					}
					chip.Children(func() {
						ink := pal.TextMuted
						if picked {
							ink = pal.Accent
						}
						ui.Icon(c, ViewIcon(cand)).FontSize(13).TextColor(ink)
						ui.Text(c, viewTypeLabel(cand)).FontSize(12).TextColor(pal.Text).SingleLine()
					})
					if chip.Clicked() {
						st.NewViewType = cand
					}
				}
			})

			ui.Row(c).Justify(ui.End).Gap(6).Children(func() {
				if ui.Button(c.Key("nv-cancel"), i18n.T("取消")).Height(28).Padding(0, 10).Clicked() {
					st.NewViewOpen = false
				}
				if ui.PrimaryButton(c.Key("nv-ok"), i18n.T("创建视图")).Height(28).Padding(0, 12).Clicked() {
					create()
				}
			})
		})
	})
}

func defaultBoardGroupProp(db *database.Database) *database.Property {
	if db == nil {
		return nil
	}
	order := []database.PropertyType{
		database.PropStatus,
		database.PropSelect,
		database.PropCheckbox,
		database.PropMultiSelect,
		database.PropPerson,
		database.PropDate,
	}
	for _, pt := range order {
		for i := range db.Properties {
			if db.Properties[i].Type == pt {
				return &db.Properties[i]
			}
		}
	}
	return nil
}

func ensureBoardConfig(v *database.ViewConfig) *database.BoardViewConfig {
	if v.Board == nil {
		v.Board = &database.BoardViewConfig{
			CardSize:    "medium",
			CoverSource: "none",
			ColorColumn: true,
			ShowCount:   true,
		}
	}
	return v.Board
}

func ensureGalleryConfig(v *database.ViewConfig) *database.GalleryViewConfig {
	if v.Gallery == nil {
		v.Gallery = &database.GalleryViewConfig{
			CardSize:    "medium",
			CoverSource: "cover",
			ShowIcon:    true,
		}
	}
	return v.Gallery
}

func defaultDateProp(db *database.Database) *database.Property {
	if db == nil {
		return nil
	}
	for i := range db.Properties {
		if db.Properties[i].Type == database.PropDate {
			return &db.Properties[i]
		}
	}
	for i := range db.Properties {
		if db.Properties[i].Type == database.PropCreatedTime || db.Properties[i].Type == database.PropEditedTime {
			return &db.Properties[i]
		}
	}
	return nil
}

func datePropertiesOf(db *database.Database) []database.Property {
	if db == nil {
		return nil
	}
	var out []database.Property
	for _, p := range db.Properties {
		if p.Type == database.PropDate || p.Type == database.PropCreatedTime || p.Type == database.PropEditedTime {
			out = append(out, p)
		}
	}
	return out
}

func ensureCalendarConfig(v *database.ViewConfig, db *database.Database) *database.CalendarViewConfig {
	if v.Calendar == nil {
		defID := ""
		if dp := defaultDateProp(db); dp != nil {
			defID = dp.ID
		}
		v.Calendar = &database.CalendarViewConfig{
			DatePropID:   defID,
			Mode:         "month",
			ShowWeekends: true,
			WeekStart:    1,
		}
	} else {
		if v.Calendar.DatePropID == "" {
			if dp := defaultDateProp(db); dp != nil {
				v.Calendar.DatePropID = dp.ID
			}
		}
		if v.Calendar.Mode == "" {
			v.Calendar.Mode = "month"
		}
	}
	return v.Calendar
}

func ensureTimelineConfig(v *database.ViewConfig, db *database.Database) *database.TimelineViewConfig {
	if v.Timeline == nil {
		defID := ""
		if dp := defaultDateProp(db); dp != nil {
			defID = dp.ID
		}
		v.Timeline = &database.TimelineViewConfig{
			StartPropID: defID,
			Zoom:        "month",
			ShowTable:   true,
		}
	} else {
		if v.Timeline.StartPropID == "" {
			if dp := defaultDateProp(db); dp != nil {
				v.Timeline.StartPropID = dp.ID
			}
		}
		if v.Timeline.Zoom == "" {
			v.Timeline.Zoom = "month"
		}
	}
	return v.Timeline
}

func ensureChartConfig(v *database.ViewConfig, db *database.Database) *database.ChartViewConfig {
	if v.Chart == nil {
		defX := ""
		if gp := defaultBoardGroupProp(db); gp != nil {
			defX = gp.ID
		} else if db != nil && len(db.Properties) > 0 {
			defX = db.Properties[0].ID
		}
		v.Chart = &database.ChartViewConfig{
			Kind:       "bar",
			XPropID:    defX,
			YMode:      "count",
			YAgg:       database.AggSum,
			ShowValues: true,
			ShowLegend: true,
			Smooth:     true,
			Donut:      true,
		}
	} else {
		if v.Chart.Kind == "" {
			v.Chart.Kind = "bar"
		}
		if v.Chart.YMode == "" {
			v.Chart.YMode = "count"
		}
		if v.Chart.XPropID == "" {
			if gp := defaultBoardGroupProp(db); gp != nil {
				v.Chart.XPropID = gp.ID
			}
		}
	}
	return v.Chart
}

func ensureFeedConfig(v *database.ViewConfig) *database.FeedViewConfig {
	if v.Feed == nil {
		v.Feed = &database.FeedViewConfig{
			ShowExcerpt: true,
			ShowImage:   true,
		}
	}
	return v.Feed
}

func isFormEditableProp(pt database.PropertyType) bool {
	switch pt {
	case database.PropCreatedTime, database.PropEditedTime, database.PropUniqueID, database.PropRollup, database.PropFormula:
		return false
	default:
		return true
	}
}

func ensureFormConfig(v *database.ViewConfig, db *database.Database) *database.FormViewConfig {
	if v.Form == nil {
		title := ""
		if db != nil {
			title = db.Title
		}
		v.Form = &database.FormViewConfig{
			Title:            title,
			SubmitText:       i18n.T("提交"),
			ContinueOnSubmit: true,
		}
	}
	if len(v.Form.Fields) == 0 && db != nil {
		fields := make([]database.FormFieldConfig, 0, len(db.Properties))
		for _, p := range db.Properties {
			if !isFormEditableProp(p.Type) {
				continue
			}
			fields = append(fields, database.FormFieldConfig{
				PropertyID: p.ID,
				Required:   p.Type == database.PropTitle,
			})
		}
		v.Form.Fields = fields
	}
	return v.Form
}

func layoutPopover(
	c *ui.Context,
	anchor ui.Element,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	opts Options,
	pal Palette,
) {
	ui.Popover(c.Key("pop-layout"), anchor, &st.LayoutOpen, func() {
		ui.Column(c.Key("layout-box")).Width(292).Gap(10).Padding(6).Children(func() {
			ui.Text(c, i18n.T("视图布局")).FontSize(13).FontWeight(600).TextColor(pal.Text)

			// Default record peek open mode.
			curOpen := view.OpenMode
			if curOpen == "" {
				curOpen = OpenSidePeek
			}
			openModes := []struct{ id, label string }{
				{OpenSidePeek, i18n.T("侧边预览")},
				{OpenCenterPeek, i18n.T("居中弹窗")},
				{OpenFullPage, i18n.T("整页打开")},
			}
			openLbl := i18n.T("侧边预览")
			for _, om := range openModes {
				if om.id == curOpen {
					openLbl = om.label
					break
				}
			}
			ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, i18n.T("默认打开方式")).FontSize(12.5).TextColor(pal.Text)
				mb := ui.Button(c.Key("lay-open"), openLbl).Height(26).Padding(0, 8)
				mb.PopoverMenuButton(func(m *ui.Menu) {
					for _, om := range openModes {
						mode := om
						if m.Item(mode.label).Checked(curOpen == mode.id).Chosen() {
							vc := view.Clone()
							vc.OpenMode = mode.id
							saveViewAndNotify(st, vc, opts)
						}
					}
				})
			})

			switch view.Type {
			case database.ViewTable:
				freeze := view.Table == nil || view.Table.FreezeFirst
				wrapAll := view.Table != nil && view.Table.WrapAll
				showIcon := view.Table == nil || view.Table.ShowIcon

				renderLayoutSwitch(c, "lay-freeze", i18n.T("冻结首列"), &freeze, pal, func(v bool) {
					vc := view.Clone()
					if vc.Table == nil {
						vc.Table = &database.TableViewConfig{FreezeFirst: true, ShowIcon: true}
					}
					vc.Table.FreezeFirst = v
					saveViewAndNotify(st, vc, opts)
				})
				renderLayoutSwitch(c, "lay-wrap", i18n.T("全部列换行"), &wrapAll, pal, func(v bool) {
					vc := view.Clone()
					if vc.Table == nil {
						vc.Table = &database.TableViewConfig{FreezeFirst: true, ShowIcon: true}
					}
					vc.Table.WrapAll = v
					saveViewAndNotify(st, vc, opts)
				})
				renderLayoutSwitch(c, "lay-icon", i18n.T("显示页面图标"), &showIcon, pal, func(v bool) {
					vc := view.Clone()
					if vc.Table == nil {
						vc.Table = &database.TableViewConfig{FreezeFirst: true, ShowIcon: true}
					}
					vc.Table.ShowIcon = v
					saveViewAndNotify(st, vc, opts)
				})

			case database.ViewBoard:
				bcfg := ensureBoardConfig(&view)
				cardSize := bcfg.CardSize
				if cardSize == "" {
					cardSize = "medium"
				}
				coverSrc := bcfg.CoverSource
				if coverSrc == "" {
					coverSrc = "none"
				}
				fitImg := bcfg.FitImage
				colorCol := bcfg.ColorColumn
				showCnt := bcfg.ShowCount
				hideEmpty := bcfg.HideEmptyCols || (view.Group != nil && view.Group.HideEmpty)

				renderCardSizeSelector(c, "lay-bsize", cardSize, pal, func(sz string) {
					vc := view.Clone()
					ensureBoardConfig(&vc).CardSize = sz
					saveViewAndNotify(st, vc, opts)
				})
				renderCoverSourceSelector(c, "lay-bcover", db, coverSrc, pal, func(src string) {
					vc := view.Clone()
					ensureBoardConfig(&vc).CoverSource = src
					saveViewAndNotify(st, vc, opts)
				})
				if coverSrc == "cover" || strings.HasPrefix(coverSrc, "prop:") {
					renderLayoutSwitch(c, "lay-bfit", i18n.T("适应图片"), &fitImg, pal, func(v bool) {
						vc := view.Clone()
						ensureBoardConfig(&vc).FitImage = v
						saveViewAndNotify(st, vc, opts)
					})
				}

				// Sub-group swimlanes selector.
				primaryID := ""
				if view.Group != nil {
					primaryID = view.Group.PropertyID
				}
				subName := i18n.T("不分子分组")
				if bcfg.SubGroup != nil && bcfg.SubGroup.PropertyID != "" {
					if sp, idx := db.PropertyByID(bcfg.SubGroup.PropertyID); idx >= 0 {
						subName = sp.Name
					}
				}
				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, i18n.T("泳道子分组")).FontSize(12.5).TextColor(pal.Text)
					sb := ui.Button(c.Key("lay-sub"), subName).Height(26).Padding(0, 8)
					sb.PopoverMenuButton(func(m *ui.Menu) {
						if m.Item(i18n.T("不分子分组")).Checked(bcfg.SubGroup == nil || bcfg.SubGroup.PropertyID == "").Chosen() {
							vc := view.Clone()
							ensureBoardConfig(&vc).SubGroup = nil
							saveViewAndNotify(st, vc, opts)
						}
						m.Separator()
						for _, p := range db.Properties {
							cand := p
							if cand.ID == primaryID || cand.Type == database.PropTitle {
								continue
							}
							if m.Item(cand.Name).Checked(bcfg.SubGroup != nil && bcfg.SubGroup.PropertyID == cand.ID).Chosen() {
								vc := view.Clone()
								sg := &database.GroupConfig{PropertyID: cand.ID}
								if cand.Type == database.PropStatus {
									sg.StatusBy = "option"
								} else if cand.Type == database.PropDate || cand.Type == database.PropCreatedTime || cand.Type == database.PropEditedTime {
									sg.DateBy = "month"
								}
								ensureBoardConfig(&vc).SubGroup = sg
								saveViewAndNotify(st, vc, opts)
							}
						}
					})
				})
				if bcfg.SubGroup != nil && bcfg.SubGroup.PropertyID != "" {
					hideSub := bcfg.SubGroup.HideEmpty
					renderLayoutSwitch(c, "lay-hidesub", i18n.T("隐藏空泳道"), &hideSub, pal, func(v bool) {
						vc := view.Clone()
						if vc.Board != nil && vc.Board.SubGroup != nil {
							vc.Board.SubGroup.HideEmpty = v
						}
						saveViewAndNotify(st, vc, opts)
					})
				}

				renderLayoutSwitch(c, "lay-colcolor", i18n.T("列背景着色"), &colorCol, pal, func(v bool) {
					vc := view.Clone()
					ensureBoardConfig(&vc).ColorColumn = v
					saveViewAndNotify(st, vc, opts)
				})
				renderLayoutSwitch(c, "lay-colcnt", i18n.T("显示列计数"), &showCnt, pal, func(v bool) {
					vc := view.Clone()
					ensureBoardConfig(&vc).ShowCount = v
					saveViewAndNotify(st, vc, opts)
				})
				renderLayoutSwitch(c, "lay-hideempty", i18n.T("隐藏空列"), &hideEmpty, pal, func(v bool) {
					vc := view.Clone()
					ensureBoardConfig(&vc).HideEmptyCols = v
					if vc.Group != nil {
						vc.Group.HideEmpty = v
					}
					saveViewAndNotify(st, vc, opts)
				})

			case database.ViewGallery:
				gcfg := ensureGalleryConfig(&view)
				cardSize := gcfg.CardSize
				if cardSize == "" {
					cardSize = "medium"
				}
				coverSrc := gcfg.CoverSource
				if coverSrc == "" {
					coverSrc = "cover"
				}
				fitImg := gcfg.FitImage
				showIcon := view.Gallery == nil || view.Gallery.ShowIcon

				renderCardSizeSelector(c, "lay-gsize", cardSize, pal, func(sz string) {
					vc := view.Clone()
					ensureGalleryConfig(&vc).CardSize = sz
					saveViewAndNotify(st, vc, opts)
				})
				renderCoverSourceSelector(c, "lay-gcover", db, coverSrc, pal, func(src string) {
					vc := view.Clone()
					ensureGalleryConfig(&vc).CoverSource = src
					saveViewAndNotify(st, vc, opts)
				})
				if coverSrc == "cover" || strings.HasPrefix(coverSrc, "prop:") {
					renderLayoutSwitch(c, "lay-gfit", i18n.T("适应图片"), &fitImg, pal, func(v bool) {
						vc := view.Clone()
						ensureGalleryConfig(&vc).FitImage = v
						saveViewAndNotify(st, vc, opts)
					})
				}
				renderLayoutSwitch(c, "lay-gicon", i18n.T("显示页面图标"), &showIcon, pal, func(v bool) {
					vc := view.Clone()
					ensureGalleryConfig(&vc).ShowIcon = v
					saveViewAndNotify(st, vc, opts)
				})

			case database.ViewList:
				ui.Row(c).Gap(6).Children(func() {
					if ui.Button(c.Key("lay-list-grp"), i18n.T("设置分组…")).Height(26).Padding(0, 8).Clicked() {
						st.LayoutOpen = false
						st.GroupOpen = true
					}
					if ui.Button(c.Key("lay-list-props"), i18n.T("管理属性…")).Height(26).Padding(0, 8).Clicked() {
						st.LayoutOpen = false
						st.PropsOpen = true
					}
				})

			case database.ViewCalendar:
				ccfg := ensureCalendarConfig(&view, db)
				dateProps := datePropertiesOf(db)
				dpName := i18n.T("未指定日期属性")
				for _, p := range dateProps {
					if p.ID == ccfg.DatePropID {
						dpName = p.Name
						break
					}
				}
				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, i18n.T("日历日期属性")).FontSize(12.5).TextColor(pal.Text)
					btn := ui.Button(c.Key("lay-cal-prop"), dpName).Height(26).Padding(0, 8)
					btn.PopoverMenuButton(func(m *ui.Menu) {
						for _, p := range dateProps {
							cand := p
							if m.Item(cand.Name).Checked(cand.ID == ccfg.DatePropID).Chosen() {
								vc := view.Clone()
								ensureCalendarConfig(&vc, db).DatePropID = cand.ID
								saveViewAndNotify(st, vc, opts)
							}
						}
					})
				})

				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, i18n.T("日历模式")).FontSize(12.5).TextColor(pal.Text)
					ui.Row(c.Key("lay-cal-mode")).Gap(3).Children(func() {
						for _, m := range []struct{ id, label string }{
							{"month", i18n.T("月")},
							{"week", i18n.T("周")},
						} {
							cand := m
							btn := ui.Button(c.Key(cand.id), cand.label).Height(24).Padding(0, 10)
							if ccfg.Mode == cand.id {
								btn.Background(pal.AccentSoft).TextColor(pal.Accent)
							}
							if btn.Clicked() && ccfg.Mode != cand.id {
								vc := view.Clone()
								ensureCalendarConfig(&vc, db).Mode = cand.id
								saveViewAndNotify(st, vc, opts)
							}
						}
					})
				})

				showWknd := ccfg.ShowWeekends
				renderLayoutSwitch(c, "lay-cal-wknd", i18n.T("显示周末"), &showWknd, pal, func(v bool) {
					vc := view.Clone()
					ensureCalendarConfig(&vc, db).ShowWeekends = v
					saveViewAndNotify(st, vc, opts)
				})

				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, i18n.T("每周起始日")).FontSize(12.5).TextColor(pal.Text)
					ui.Row(c.Key("lay-cal-start")).Gap(3).Children(func() {
						for _, ws := range []struct {
							day   int
							label string
						}{
							{1, i18n.T("周一")},
							{0, i18n.T("周日")},
						} {
							cand := ws
							btn := ui.Button(c.Key(fmt.Sprintf("ws%d", cand.day)), cand.label).Height(24).Padding(0, 8)
							if ccfg.WeekStart == cand.day {
								btn.Background(pal.AccentSoft).TextColor(pal.Accent)
							}
							if btn.Clicked() && ccfg.WeekStart != cand.day {
								vc := view.Clone()
								ensureCalendarConfig(&vc, db).WeekStart = cand.day
								saveViewAndNotify(st, vc, opts)
							}
						}
					})
				})

			case database.ViewTimeline:
				tcfg := ensureTimelineConfig(&view, db)
				dateProps := datePropertiesOf(db)
				spName := i18n.T("未指定日期属性")
				epName := i18n.T("使用同一日期范围")
				for _, p := range dateProps {
					if p.ID == tcfg.StartPropID {
						spName = p.Name
					}
					if p.ID == tcfg.EndPropID && tcfg.EndPropID != "" {
						epName = p.Name
					}
				}
				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, i18n.T("开始日期属性")).FontSize(12.5).TextColor(pal.Text)
					btn := ui.Button(c.Key("lay-tl-start"), spName).Height(26).Padding(0, 8)
					btn.PopoverMenuButton(func(m *ui.Menu) {
						for _, p := range dateProps {
							cand := p
							if m.Item(cand.Name).Checked(cand.ID == tcfg.StartPropID).Chosen() {
								vc := view.Clone()
								ensureTimelineConfig(&vc, db).StartPropID = cand.ID
								saveViewAndNotify(st, vc, opts)
							}
						}
					})
				})
				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, i18n.T("结束日期属性")).FontSize(12.5).TextColor(pal.Text)
					btn := ui.Button(c.Key("lay-tl-end"), epName).Height(26).Padding(0, 8)
					btn.PopoverMenuButton(func(m *ui.Menu) {
						if m.Item(i18n.T("使用同一日期范围")).Checked(tcfg.EndPropID == "").Chosen() {
							vc := view.Clone()
							ensureTimelineConfig(&vc, db).EndPropID = ""
							saveViewAndNotify(st, vc, opts)
						}
						m.Separator()
						for _, p := range dateProps {
							cand := p
							if cand.ID == tcfg.StartPropID {
								continue
							}
							if m.Item(cand.Name).Checked(cand.ID == tcfg.EndPropID).Chosen() {
								vc := view.Clone()
								ensureTimelineConfig(&vc, db).EndPropID = cand.ID
								saveViewAndNotify(st, vc, opts)
							}
						}
					})
				})

				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, i18n.T("时间缩放")).FontSize(12.5).TextColor(pal.Text)
					zLbl := timelineZoomLabel(tcfg.Zoom)
					zb := ui.Button(c.Key("lay-tl-zoom"), zLbl).Height(26).Padding(0, 8)
					zb.PopoverMenuButton(func(m *ui.Menu) {
						for _, z := range timelineZoomLevels() {
							cand := z
							if m.Item(cand.label).Checked(tcfg.Zoom == cand.id).Chosen() {
								vc := view.Clone()
								ensureTimelineConfig(&vc, db).Zoom = cand.id
								saveViewAndNotify(st, vc, opts)
							}
						}
					})
				})

				showTbl := tcfg.ShowTable
				renderLayoutSwitch(c, "lay-tl-tbl", i18n.T("显示左侧表格"), &showTbl, pal, func(v bool) {
					vc := view.Clone()
					ensureTimelineConfig(&vc, db).ShowTable = v
					saveViewAndNotify(st, vc, opts)
				})

			case database.ViewChart:
				chCfg := ensureChartConfig(&view, db)
				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, i18n.T("图表类型")).FontSize(12.5).TextColor(pal.Text)
					kLbl := chartKindLabel(chCfg.Kind)
					kb := ui.Button(c.Key("lay-ch-kind"), kLbl).Height(26).Padding(0, 8)
					kb.PopoverMenuButton(func(m *ui.Menu) {
						for _, ck := range chartKindsList() {
							cand := ck
							if m.Item(cand.label).Checked(chCfg.Kind == cand.id).Chosen() {
								vc := view.Clone()
								ensureChartConfig(&vc, db).Kind = cand.id
								saveViewAndNotify(st, vc, opts)
							}
						}
					})
				})

				// X-axis property selector.
				xName := i18n.T("选择属性")
				var xProp *database.Property
				if xp, idx := db.PropertyByID(chCfg.XPropID); idx >= 0 {
					xProp = xp
					xName = xp.Name
				}
				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, i18n.T("X 轴属性")).FontSize(12.5).TextColor(pal.Text)
					xb := ui.Button(c.Key("lay-ch-x"), xName).Height(26).Padding(0, 8)
					xb.PopoverMenuButton(func(m *ui.Menu) {
						for _, p := range db.Properties {
							cand := p
							if m.Item(cand.Name).Checked(cand.ID == chCfg.XPropID).Chosen() {
								vc := view.Clone()
								ensureChartConfig(&vc, db).XPropID = cand.ID
								saveViewAndNotify(st, vc, opts)
							}
						}
					})
				})

				if xProp != nil && (xProp.Type == database.PropDate || xProp.Type == database.PropCreatedTime || xProp.Type == database.PropEditedTime) {
					dateModes := []struct{ id, label string }{
						{"day", i18n.T("按天")},
						{"week", i18n.T("按周")},
						{"month", i18n.T("按月")},
						{"year", i18n.T("按年")},
					}
					dLbl := i18n.T("按月")
					for _, dm := range dateModes {
						if dm.id == chCfg.XDateBy {
							dLbl = dm.label
							break
						}
					}
					ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
						ui.Text(c, i18n.T("日期粒度")).FontSize(12.5).TextColor(pal.Text)
						dbtn := ui.Button(c.Key("lay-ch-dateby"), dLbl).Height(26).Padding(0, 8)
						dbtn.PopoverMenuButton(func(m *ui.Menu) {
							for _, dm := range dateModes {
								cand := dm
								if m.Item(cand.label).Checked(chCfg.XDateBy == cand.id || (cand.id == "month" && chCfg.XDateBy == "")).Chosen() {
									vc := view.Clone()
									ensureChartConfig(&vc, db).XDateBy = cand.id
									saveViewAndNotify(st, vc, opts)
								}
							}
						})
					})
				}

				sortChoices := []struct{ id, label string }{
					{"manual", i18n.T("默认")},
					{"val_desc", i18n.T("数值降序")},
					{"val_asc", i18n.T("数值升序")},
					{"asc", i18n.T("名称升序")},
					{"desc", i18n.T("名称降序")},
				}
				sortLbl := i18n.T("默认")
				for _, sc := range sortChoices {
					if sc.id == chCfg.XSort {
						sortLbl = sc.label
						break
					}
				}
				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, i18n.T("X 轴排序")).FontSize(12.5).TextColor(pal.Text)
					sb := ui.Button(c.Key("lay-ch-xsort"), sortLbl).Height(26).Padding(0, 8)
					sb.PopoverMenuButton(func(m *ui.Menu) {
						for _, sc := range sortChoices {
							cand := sc
							if m.Item(cand.label).Checked(chCfg.XSort == cand.id || (cand.id == "manual" && chCfg.XSort == "")).Chosen() {
								vc := view.Clone()
								ensureChartConfig(&vc, db).XSort = cand.id
								saveViewAndNotify(st, vc, opts)
							}
						}
					})
				})

				// Y-axis mode + numeric property selector.
				yModeLbl := i18n.T("记录数")
				if chCfg.YMode == "prop" {
					yModeLbl = i18n.T("属性聚合")
				}
				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, i18n.T("Y 轴计算")).FontSize(12.5).TextColor(pal.Text)
					yb := ui.Button(c.Key("lay-ch-ymode"), yModeLbl).Height(26).Padding(0, 8)
					yb.PopoverMenuButton(func(m *ui.Menu) {
						if m.Item(i18n.T("记录数")).Checked(chCfg.YMode != "prop").Chosen() {
							vc := view.Clone()
							ensureChartConfig(&vc, db).YMode = "count"
							saveViewAndNotify(st, vc, opts)
						}
						if m.Item(i18n.T("属性聚合")).Checked(chCfg.YMode == "prop").Chosen() {
							vc := view.Clone()
							cc := ensureChartConfig(&vc, db)
							cc.YMode = "prop"
							if cc.YPropID == "" {
								for _, p := range db.Properties {
									if p.Type == database.PropNumber {
										cc.YPropID = p.ID
										cc.YAgg = database.AggSum
										break
									}
								}
							}
							saveViewAndNotify(st, vc, opts)
						}
					})
				})

				if chCfg.YMode == "prop" {
					ypName := i18n.T("选择属性")
					if yp, idx := db.PropertyByID(chCfg.YPropID); idx >= 0 {
						ypName = yp.Name
					}
					ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
						ui.Text(c, i18n.T("Y 轴属性")).FontSize(12.5).TextColor(pal.Text)
						ypb := ui.Button(c.Key("lay-ch-yprop"), ypName).Height(26).Padding(0, 8)
						ypb.PopoverMenuButton(func(m *ui.Menu) {
							for _, p := range db.Properties {
								cand := p
								if m.Item(cand.Name).Checked(cand.ID == chCfg.YPropID).Chosen() {
									vc := view.Clone()
									cc := ensureChartConfig(&vc, db)
									cc.YPropID = cand.ID
									if cand.Type == database.PropNumber && (cc.YAgg == "" || cc.YAgg == database.AggNone) {
										cc.YAgg = database.AggSum
									}
									saveViewAndNotify(st, vc, opts)
								}
							}
						})
					})

					aggLbl := aggregationLabel(chCfg.YAgg)
					if chCfg.YAgg == "" {
						aggLbl = i18n.T("求和")
					}
					ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
						ui.Text(c, i18n.T("聚合方式")).FontSize(12.5).TextColor(pal.Text)
						ab := ui.Button(c.Key("lay-ch-yagg"), aggLbl).Height(26).Padding(0, 8)
						ab.PopoverMenuButton(func(m *ui.Menu) {
							for _, a := range []database.AggregationType{
								database.AggSum, database.AggAverage, database.AggMedian,
								database.AggMin, database.AggMax, database.AggCountNotEmpty,
							} {
								cand := a
								if m.Item(aggregationLabel(cand)).Checked(chCfg.YAgg == cand).Chosen() {
									vc := view.Clone()
									ensureChartConfig(&vc, db).YAgg = cand
									saveViewAndNotify(st, vc, opts)
								}
							}
						})
					})
				}

				if chCfg.Kind != "donut" {
					stkName := i18n.T("不堆叠")
					if sp, idx := db.PropertyByID(chCfg.StackPropID); idx >= 0 && chCfg.StackPropID != "" {
						stkName = sp.Name
					}
					ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
						ui.Text(c, i18n.T("堆叠分组")).FontSize(12.5).TextColor(pal.Text)
						sb := ui.Button(c.Key("lay-ch-stack"), stkName).Height(26).Padding(0, 8)
						sb.PopoverMenuButton(func(m *ui.Menu) {
							if m.Item(i18n.T("不堆叠")).Checked(chCfg.StackPropID == "").Chosen() {
								vc := view.Clone()
								ensureChartConfig(&vc, db).StackPropID = ""
								saveViewAndNotify(st, vc, opts)
							}
							m.Separator()
							for _, p := range db.Properties {
								cand := p
								if cand.ID == chCfg.XPropID || cand.Type == database.PropTitle {
									continue
								}
								if m.Item(cand.Name).Checked(cand.ID == chCfg.StackPropID).Chosen() {
									vc := view.Clone()
									ensureChartConfig(&vc, db).StackPropID = cand.ID
									saveViewAndNotify(st, vc, opts)
								}
							}
						})
					})
				}

				showVals := chCfg.ShowValues
				renderLayoutSwitch(c, "lay-ch-vals", i18n.T("显示数值"), &showVals, pal, func(v bool) {
					vc := view.Clone()
					ensureChartConfig(&vc, db).ShowValues = v
					saveViewAndNotify(st, vc, opts)
				})
				showLeg := chCfg.ShowLegend
				renderLayoutSwitch(c, "lay-ch-leg", i18n.T("显示图例"), &showLeg, pal, func(v bool) {
					vc := view.Clone()
					ensureChartConfig(&vc, db).ShowLegend = v
					saveViewAndNotify(st, vc, opts)
				})
				if chCfg.Kind == "line" {
					smooth := chCfg.Smooth
					renderLayoutSwitch(c, "lay-ch-smooth", i18n.T("平滑曲线"), &smooth, pal, func(v bool) {
						vc := view.Clone()
						ensureChartConfig(&vc, db).Smooth = v
						saveViewAndNotify(st, vc, opts)
					})
				}
				if chCfg.Kind == "donut" {
					donut := chCfg.Donut
					renderLayoutSwitch(c, "lay-ch-donut", i18n.T("环形样式"), &donut, pal, func(v bool) {
						vc := view.Clone()
						ensureChartConfig(&vc, db).Donut = v
						saveViewAndNotify(st, vc, opts)
					})
				}

			case database.ViewFeed:
				fcfg := ensureFeedConfig(&view)
				showEx := fcfg.ShowExcerpt
				showIm := fcfg.ShowImage
				compact := fcfg.Compact
				renderLayoutSwitch(c, "lay-fd-ex", i18n.T("显示正文摘要"), &showEx, pal, func(v bool) {
					vc := view.Clone()
					ensureFeedConfig(&vc).ShowExcerpt = v
					saveViewAndNotify(st, vc, opts)
				})
				renderLayoutSwitch(c, "lay-fd-img", i18n.T("显示封面图片"), &showIm, pal, func(v bool) {
					vc := view.Clone()
					ensureFeedConfig(&vc).ShowImage = v
					saveViewAndNotify(st, vc, opts)
				})
				renderLayoutSwitch(c, "lay-fd-compact", i18n.T("紧凑模式"), &compact, pal, func(v bool) {
					vc := view.Clone()
					ensureFeedConfig(&vc).Compact = v
					saveViewAndNotify(st, vc, opts)
				})

			case database.ViewForm:
				fmcfg := ensureFormConfig(&view, db)
				cont := fmcfg.ContinueOnSubmit
				renderLayoutSwitch(c, "lay-fm-cont", i18n.T("提交后继续填写"), &cont, pal, func(v bool) {
					vc := view.Clone()
					ensureFormConfig(&vc, db).ContinueOnSubmit = v
					saveViewAndNotify(st, vc, opts)
				})
				if !opts.ReadOnly {
					lbl := i18n.T("编辑表单配置")
					if st.FormEditMode {
						lbl = i18n.T("返回填写表单")
					}
					if ui.Button(c.Key("lay-fm-mode"), lbl).Height(26).Padding(0, 10).Clicked() {
						st.FormEditMode = !st.FormEditMode
						st.LayoutOpen = false
					}
				}
			}

			ui.Box(c).Height(1).Background(pal.Border)
			ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, i18n.T("条件着色")).FontSize(12.5).TextColor(pal.Text)
				lbl := i18n.T("设置规则")
				if len(view.Colors) > 0 {
					lbl = fmt.Sprintf("%s (%d)", i18n.T("已启用"), len(view.Colors))
				}
				if ui.Button(c.Key("lay-colors"), lbl).Height(26).Padding(0, 10).Clicked() {
					st.LayoutOpen = false
					st.ColorRulesOpen = true
				}
			})
		})
	})
}

func renderLayoutSwitch(c *ui.Context, key, label string, val *bool, pal Palette, onChange func(bool)) {
	ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Text(c, label).FontSize(12.5).TextColor(pal.Text)
		sw := ui.Switch(c.Key(key), val).Label(label)
		if sw.Changed() {
			onChange(*val)
		}
	})
}

func renderCardSizeSelector(c *ui.Context, key, cur string, pal Palette, onPick func(string)) {
	ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Text(c, i18n.T("卡片尺寸")).FontSize(12.5).TextColor(pal.Text)
		ui.Row(c.Key(key)).Gap(3).Children(func() {
			for _, sz := range []struct{ id, label string }{
				{"small", i18n.T("小")},
				{"medium", i18n.T("中")},
				{"large", i18n.T("大")},
			} {
				cand := sz
				btn := ui.Button(c.Key(cand.id), cand.label).Height(24).Padding(0, 8)
				if cur == cand.id {
					btn.Background(pal.AccentSoft).TextColor(pal.Accent)
				}
				if btn.Clicked() && cur != cand.id {
					onPick(cand.id)
				}
			}
		})
	})
}

func renderCoverSourceSelector(c *ui.Context, key string, db *database.Database, cur string, pal Palette, onPick func(string)) {
	choices := []struct{ id, label string }{
		{"none", i18n.T("无预览")},
		{"cover", i18n.T("页面封面")},
		{"content", i18n.T("页面内容")},
	}
	if db != nil {
		for _, p := range db.Properties {
			if p.Type == database.PropFiles {
				choices = append(choices, struct{ id, label string }{
					id:    "prop:" + p.ID,
					label: p.Name,
				})
			}
		}
	}
	curLbl := i18n.T("页面封面")
	for _, ch := range choices {
		if ch.id == cur {
			curLbl = ch.label
			break
		}
	}
	ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Text(c, i18n.T("卡片预览")).FontSize(12.5).TextColor(pal.Text)
		btn := ui.Button(c.Key(key), curLbl).Height(26).Padding(0, 8)
		btn.PopoverMenuButton(func(m *ui.Menu) {
			for _, ch := range choices {
				cand := ch
				if m.Item(cand.label).Checked(cur == cand.id).Chosen() {
					onPick(cand.id)
				}
			}
		})
	})
}

func creatablePropertyTypes() []database.PropertyType {
	return []database.PropertyType{
		database.PropText,
		database.PropNumber,
		database.PropSelect,
		database.PropMultiSelect,
		database.PropStatus,
		database.PropDate,
		database.PropCheckbox,
		database.PropURL,
		database.PropEmail,
		database.PropPhone,
		database.PropPerson,
		database.PropFiles,
		database.PropUniqueID,
		database.PropRelation,
		database.PropRollup,
		database.PropFormula,
	}
}

func uniquePropKey(db *database.Database, name string) string {
	base := strings.ToLower(strings.TrimSpace(name))
	base = strings.Map(func(r rune) rune {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_':
			return r
		case r == ' ' || r == '-':
			return '_'
		default:
			return -1
		}
	}, base)
	base = strings.Trim(base, "_")
	if base == "" {
		base = "field"
	}
	taken := func(k string) bool {
		for _, p := range db.Properties {
			if p.Key == k {
				return true
			}
		}
		return false
	}
	if !taken(base) {
		return base
	}
	for i := 2; i < 1000; i++ {
		cand := fmt.Sprintf("%s_%d", base, i)
		if !taken(cand) {
			return cand
		}
	}
	return "field_" + database.NewID()[:6]
}

func defaultFilterOpFor(pt database.PropertyType) database.FilterOp {
	switch pt {
	case database.PropNumber:
		return database.OpEquals
	case database.PropCheckbox:
		return database.OpChecked
	case database.PropSelect, database.PropStatus:
		return database.OpEquals
	case database.PropMultiSelect, database.PropFiles, database.PropPerson, database.PropRelation:
		return database.OpContainsAny
	case database.PropDate, database.PropCreatedTime, database.PropEditedTime:
		return database.OpDateIs
	default:
		return database.OpContains
	}
}

func filterOpsForType(pt database.PropertyType) []database.FilterOp {
	switch pt {
	case database.PropNumber:
		return []database.FilterOp{
			database.OpEquals, database.OpNotEquals,
			database.OpGT, database.OpGTE, database.OpLT, database.OpLTE,
			database.OpIsEmpty, database.OpIsNotEmpty,
		}
	case database.PropCheckbox:
		return []database.FilterOp{database.OpChecked, database.OpUnchecked}
	case database.PropStatus:
		return []database.FilterOp{
			database.OpEquals, database.OpNotEquals,
			database.OpStatusGroupIs, database.OpStatusGroupNot,
			database.OpIsEmpty, database.OpIsNotEmpty,
		}
	case database.PropSelect:
		return []database.FilterOp{
			database.OpEquals, database.OpNotEquals,
			database.OpIsEmpty, database.OpIsNotEmpty,
		}
	case database.PropMultiSelect, database.PropFiles, database.PropPerson, database.PropRelation:
		return []database.FilterOp{
			database.OpContainsAny, database.OpContainsAll, database.OpNotContainsAny,
			database.OpIsEmpty, database.OpIsNotEmpty,
		}
	case database.PropDate, database.PropCreatedTime, database.PropEditedTime:
		return []database.FilterOp{
			database.OpDateIs, database.OpDateBefore, database.OpDateAfter,
			database.OpDateOnOrBefore, database.OpDateOnOrAfter, database.OpDateWithin,
			database.OpIsEmpty, database.OpIsNotEmpty,
		}
	case database.PropFormula, database.PropRollup:
		return []database.FilterOp{
			database.OpEquals, database.OpNotEquals,
			database.OpContains, database.OpNotContains,
			database.OpGT, database.OpGTE, database.OpLT, database.OpLTE,
			database.OpIsEmpty, database.OpIsNotEmpty,
		}
	default:
		return []database.FilterOp{
			database.OpContains, database.OpNotContains,
			database.OpEquals, database.OpNotEquals,
			database.OpStartsWith, database.OpEndsWith,
			database.OpIsEmpty, database.OpIsNotEmpty,
		}
	}
}

func filterOpLabel(op database.FilterOp) string {
	switch op {
	case database.OpIsEmpty:
		return i18n.T("为空")
	case database.OpIsNotEmpty:
		return i18n.T("不为空")
	case database.OpEquals:
		return i18n.T("等于")
	case database.OpNotEquals:
		return i18n.T("不等于")
	case database.OpContains:
		return i18n.T("包含")
	case database.OpNotContains:
		return i18n.T("不包含")
	case database.OpStartsWith:
		return i18n.T("开头为")
	case database.OpEndsWith:
		return i18n.T("结尾为")
	case database.OpGT:
		return i18n.T("大于")
	case database.OpGTE:
		return i18n.T("大于等于")
	case database.OpLT:
		return i18n.T("小于")
	case database.OpLTE:
		return i18n.T("小于等于")
	case database.OpChecked:
		return i18n.T("已勾选")
	case database.OpUnchecked:
		return i18n.T("未勾选")
	case database.OpContainsAny:
		return i18n.T("包含任一")
	case database.OpContainsAll:
		return i18n.T("包含全部")
	case database.OpNotContainsAny:
		return i18n.T("不包含")
	case database.OpStatusGroupIs:
		return i18n.T("状态组为")
	case database.OpStatusGroupNot:
		return i18n.T("状态组不为")
	case database.OpDateIs:
		return i18n.T("日期为")
	case database.OpDateBefore:
		return i18n.T("早于")
	case database.OpDateAfter:
		return i18n.T("晚于")
	case database.OpDateOnOrBefore:
		return i18n.T("不晚于")
	case database.OpDateOnOrAfter:
		return i18n.T("不早于")
	case database.OpDateWithin:
		return i18n.T("在范围内")
	default:
		return string(op)
	}
}

type orderedPropItem struct {
	vp   database.ViewProperty
	prop database.Property
}

func orderedViewProperties(db *database.Database, view *database.ViewConfig) []orderedPropItem {
	var out []orderedPropItem
	seen := make(map[string]bool, len(db.Properties))
	if view != nil {
		for _, vp := range view.Properties {
			if p, idx := db.PropertyByID(vp.PropertyID); idx >= 0 && !seen[p.ID] {
				seen[p.ID] = true
				out = append(out, orderedPropItem{vp: vp, prop: *p})
			}
		}
	}
	for _, p := range db.Properties {
		if !seen[p.ID] {
			seen[p.ID] = true
			out = append(out, orderedPropItem{
				vp:   database.ViewProperty{PropertyID: p.ID, Visible: true, Width: p.Width, Wrap: p.Wrap},
				prop: p,
			})
		}
	}
	return out
}

func extractViewProps(items []orderedPropItem) []database.ViewProperty {
	out := make([]database.ViewProperty, len(items))
	for i, it := range items {
		out[i] = it.vp
	}
	return out
}
