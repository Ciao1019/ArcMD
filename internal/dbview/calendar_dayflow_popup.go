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

func openCalendarDetailPopover(st *State, ev calEvent, anchorKey string, loc *time.Location) {
	if st == nil || ev.rec == nil {
		return
	}
	if loc == nil {
		loc = time.UTC
	}
	st.CloseToolbarPopovers()
	st.EditPopoverOpen = false
	st.CalendarDetailOpen = true
	st.CalendarDetailRecordID = ev.rec.ID
	st.CalendarDetailAnchorKey = anchorKey
	st.CalendarDetailTitleDraft = ev.rec.Title
	st.CalendarDetailPickerOpen = false
	st.CalendarDetailEditingEnd = false
	st.EditDateStart = ev.origVal.Start
	if st.EditDateStart == "" {
		st.EditDateStart = formatDateTimeToken(ev.startTime, ev.hasTime)
	}
	st.EditDateEnd = ev.origVal.End
	st.EditDateIncludeTime = ev.origVal.IncludeTime || (ev.hasTime && !ev.isAllDay)
	st.EditDateHasEnd = st.EditDateEnd != ""
	st.CalendarDetailPickerMonth = time.Date(ev.startDay.Year(), ev.startDay.Month(), 1, 0, 0, 0, 0, loc)
}

func maybeRenderCalendarDetailPopover(
	c *ui.Context,
	anchor ui.Element,
	st *State,
	db *database.Database,
	dateProp database.Property,
	ev calEvent,
	anchorKey string,
	openMode string,
	opts Options,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
) {
	if !st.CalendarDetailOpen || ev.rec == nil || st.CalendarDetailRecordID != ev.rec.ID {
		return
	}
	if st.CalendarDetailAnchorKey == anchorKey {
		st.calDetailMatchedAnchor = true
		renderCalendarEventDetailPopover(c, anchor, st, db, dateProp, ev, openMode, opts, pal, df, loc)
	} else if !st.calDetailMatchedAnchor && st.calDetailFallbackKey == "" {
		st.calDetailFallbackKey = anchorKey
		st.calDetailFallbackAnchor = anchor
		st.calDetailFallbackEv = ev
	}
}

func renderCalendarEventDetailPopover(
	c *ui.Context,
	anchor ui.Element,
	st *State,
	db *database.Database,
	dateProp database.Property,
	ev calEvent,
	openMode string,
	opts Options,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
) {
	if loc == nil {
		loc = time.UTC
	}
	tp := db.TitleProperty()
	catProp := df.catProp
	readOnlyDate := opts.ReadOnly || dateProp.Type != database.PropDate
	evCol := dayflowEventColors(pal.Dark, ev.color)

	styledPopover(c, "cal-det-pop:"+ev.rec.ID, anchor, &st.CalendarDetailOpen, pal, func() {
		commitTitle := func() {
			if opts.ReadOnly || tp == nil {
				return
			}
			next := strings.TrimSpace(st.CalendarDetailTitleDraft)
			if next == "" || next == ev.rec.Title {
				return
			}
			st.setCellAndNotify(ev.rec.ID, tp.ID, database.TextValue(next), opts)
		}

		ui.Column(c.Key("cdp-box")).Width(364).Padding(10).Gap(10).Children(func() {
			// 1. Header: Event Title + Color/Category Swatch Picker.
			ui.Column(c).Gap(5).Children(func() {
				ui.Text(c, i18n.T("事件标题")).FontSize(11.5).FontWeight(500).TextColor(pal.TextMuted)
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					if !opts.ReadOnly && tp != nil {
						tin := ui.TextInput(c.Key("cdp-tin"), &st.CalendarDetailTitleDraft).
							Grow(1).MinWidth(0).Height(32).Padding(0, 10).Radius(8).
							Background(pal.Paper).Border(1, pal.BorderStrong).TextColor(pal.Text).
							Label(i18n.T("事件标题"))
						if tin.Submitted() {
							commitTitle()
						}
					} else {
						title := strings.TrimSpace(ev.rec.Title)
						if title == "" {
							title = i18n.T("未命名")
						}
						ui.Text(c, title).FontSize(14).FontWeight(600).TextColor(pal.Text).Grow(1).MinWidth(0)
					}

					if catProp != nil && !opts.ReadOnly {
						swBtn := ui.Row(c.Key("cdp-cat")).Height(32).Padding(0, 8).Radius(8).Gap(5).
							Background(pal.Paper).Border(1, pal.BorderStrong).AlignItems(ui.Center).
							Cursor(ui.CursorPointer).Role(ui.RoleMenuButton).Label(i18n.T("选择分类"))
						swBtn.Children(func() {
							ui.Box(c).Size(14, 14).Radius(4).Background(evCol.line)
							ui.Icon(c, iconChevronDown).FontSize(12).TextColor(pal.TextMuted)
						})
						swBtn.PopoverMenuButton(func(m *ui.Menu) {
							for _, opt := range catProp.Options {
								cand := opt
								if m.Item(cand.Name).Checked(ev.category == cand.Name).Chosen() {
									st.setCellAndNotify(ev.rec.ID, catProp.ID, database.TextValue(cand.Name), opts)
								}
							}
							if ev.category != uncategorizedCatKey && ev.category != "" {
								m.Separator()
								if m.Item(i18n.T("清除")).Chosen() {
									st.setCellAndNotify(ev.rec.ID, catProp.ID, database.EmptyValue(), opts)
								}
							}
						})
					} else {
						ui.Box(c).Size(24, 24).Radius(6).Background(evCol.bg).Center().Children(func() {
							ui.Box(c).Size(10, 10).Radius(3).Background(evCol.line)
						})
					}
				})
			})

			// 2. Time Range Section + All-Day Switch.
			ui.Column(c).Gap(6).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, i18n.T("时间范围")).FontSize(11.5).FontWeight(500).TextColor(pal.TextMuted)
					if !readOnlyDate {
						allDay := !st.EditDateIncludeTime
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Text(c, i18n.T("全天")).FontSize(11.5).TextColor(pal.TextMuted)
							sw := ui.Switch(c.Key("cdp-allday"), &allDay).Label(i18n.T("全天"))
							if sw.Changed() {
								nextInclude := !allDay
								st.EditDateIncludeTime = nextInclude
								sT, _, okS := database.ParseDateString(st.EditDateStart, loc)
								if !okS {
									sT = ev.startTime
								}
								eT, _, okE := database.ParseDateString(st.EditDateEnd, loc)
								if !okE {
									eT = ev.endTime
								}
								if nextInclude {
									if sT.Hour() == 0 && sT.Minute() == 0 {
										sT = time.Date(sT.Year(), sT.Month(), sT.Day(), 9, 0, 0, 0, loc)
									}
									if !eT.After(sT) {
										eT = sT.Add(time.Hour)
									}
									st.EditDateStart = formatDateTimeToken(sT, true)
									st.EditDateEnd = formatDateTimeToken(eT, true)
									st.EditDateHasEnd = true
								} else {
									st.EditDateStart = dateOnly(sT).Format("2006-01-02")
									if dateOnly(eT).After(dateOnly(sT)) {
										st.EditDateEnd = dateOnly(eT).Format("2006-01-02")
										st.EditDateHasEnd = true
									} else {
										st.EditDateEnd = ""
										st.EditDateHasEnd = false
									}
								}
								commitTitle()
								st.setCellAndNotify(ev.rec.ID, dateProp.ID, database.DateRangeValue(st.EditDateStart, st.EditDateEnd, nextInclude), opts)
							}
						})
					}
				})

				startLabel := strings.ReplaceAll(st.EditDateStart, "T", " ")
				if startLabel == "" {
					startLabel = ev.startDay.Format("2006-01-02")
				}
				endLabel := strings.ReplaceAll(st.EditDateEnd, "T", " ")
				if endLabel == "" {
					endLabel = startLabel
				}

				rangeBox := ui.Row(c.Key("cdp-range")).Gap(6).AlignItems(ui.Center)
				rangeBox.Children(func() {
					sBtn := ui.Row(c.Key("cdp-sbtn")).Grow(1).MinWidth(0).Height(32).Padding(0, 7).Radius(8).
						Background(pal.Paper).Border(1, pal.BorderStrong).AlignItems(ui.Center).
						Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("开始时间"))
					if st.CalendarDetailPickerOpen && !st.CalendarDetailEditingEnd {
						sBtn.Border(1.5, pal.Text)
					}
					sBtn.Children(func() {
						ui.Text(c, startLabel).FontSize(11.5).FontWeight(500).TextColor(pal.Text).SingleLine()
					})
					if sBtn.Clicked() && !readOnlyDate {
						if st.CalendarDetailPickerOpen && !st.CalendarDetailEditingEnd {
							st.CalendarDetailPickerOpen = false
						} else {
							st.CalendarDetailPickerOpen = true
							st.CalendarDetailEditingEnd = false
							if t, _, ok := database.ParseDateString(st.EditDateStart, loc); ok {
								st.CalendarDetailPickerMonth = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
								st.CalendarDetailHourScroll.Y = max(0, float32(t.Hour())*24-56)
								st.CalendarDetailMinScroll.Y = max(0, float32(t.Minute()/5)*24-56)
							}
						}
					}

					ui.Text(c, "→").FontSize(12).TextColor(pal.TextMuted).NoWrap()

					eBtn := ui.Row(c.Key("cdp-ebtn")).Grow(1).MinWidth(0).Height(32).Padding(0, 7).Radius(8).
						Background(pal.Paper).Border(1, pal.BorderStrong).AlignItems(ui.Center).
						Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("结束时间"))
					if st.CalendarDetailPickerOpen && st.CalendarDetailEditingEnd {
						eBtn.Border(1.5, pal.Text)
					}
					eBtn.Children(func() {
						ui.Text(c, endLabel).FontSize(11.5).FontWeight(500).TextColor(pal.Text).SingleLine()
					})
					if eBtn.Clicked() && !readOnlyDate {
						if st.CalendarDetailPickerOpen && st.CalendarDetailEditingEnd {
							st.CalendarDetailPickerOpen = false
						} else {
							st.CalendarDetailPickerOpen = true
							st.CalendarDetailEditingEnd = true
							if st.EditDateEnd == "" {
								st.EditDateEnd = st.EditDateStart
							}
							if t, _, ok := database.ParseDateString(st.EditDateEnd, loc); ok {
								st.CalendarDetailPickerMonth = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
								st.CalendarDetailHourScroll.Y = max(0, float32(t.Hour())*24-56)
								st.CalendarDetailMinScroll.Y = max(0, float32(t.Minute()/5)*24-56)
							}
						}
					}
				})

				// Expandable DayFlow Split Calendar + Time Selector Panel.
				if st.CalendarDetailPickerOpen && !readOnlyDate {
					renderDayflowInlineDateTimePicker(c, st, dateProp, ev, df, pal, loc, opts, commitTitle)
				}
			})

			// 3. Footer Bar: Open Detail Page + Delete.
			ui.Row(c.Key("cdp-foot")).Padding(8, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(pal.Border).
				AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				openBtn := ui.Row(c.Key("cdp-open")).Height(28).Padding(0, 10).Radius(7).Gap(5).
					AlignItems(ui.Center).Background(pal.Surface).Cursor(ui.CursorPointer).
					Role(ui.RoleButton).Label(i18n.T("打开详情页"))
				if openBtn.Hovered() {
					openBtn.Background(pal.RowHover)
				}
				openBtn.Children(func() {
					ui.Icon(c, iconExternal).FontSize(12).TextColor(pal.Text)
					ui.Text(c, i18n.T("打开详情页")).FontSize(12).FontWeight(500).TextColor(pal.Text).NoWrap()
				})
				if openBtn.Clicked() {
					commitTitle()
					st.CalendarDetailOpen = false
					st.CalendarDetailPickerOpen = false
					if opts.OnOpenRecord != nil {
						opts.OnOpenRecord(ev.rec, openMode)
					}
				}

				if !opts.ReadOnly {
					delBtn := ui.Row(c.Key("cdp-del")).Height(28).Padding(0, 10).Radius(7).Gap(5).
						AlignItems(ui.Center).Cursor(ui.CursorPointer).
						Role(ui.RoleButton).Label(i18n.T("删除"))
					if delBtn.Hovered() {
						delBtn.Background(pal.Danger.Alpha(0.12))
					}
					delBtn.Children(func() {
						ui.Icon(c, iconTrash).FontSize(12).TextColor(pal.Danger)
						ui.Text(c, i18n.T("删除")).FontSize(12).FontWeight(500).TextColor(pal.Danger).NoWrap()
					})
					if delBtn.Clicked() {
						st.CalendarDetailOpen = false
						st.CalendarDetailPickerOpen = false
						trashSingleRecord(st, ev.rec, opts)
					}
				}
			})
		})
	})
}

func renderDayflowInlineDateTimePicker(
	c *ui.Context,
	st *State,
	dateProp database.Property,
	ev calEvent,
	df dfCalChrome,
	pal Palette,
	loc *time.Location,
	opts Options,
	commitTitle func(),
) {
	targetStr := &st.EditDateStart
	if st.CalendarDetailEditingEnd {
		targetStr = &st.EditDateEnd
	}
	curT, _, okT := database.ParseDateString(*targetStr, loc)
	if !okT {
		curT = ev.startTime
	}
	monthCur := st.CalendarDetailPickerMonth
	if monthCur.IsZero() {
		monthCur = time.Date(curT.Year(), curT.Month(), 1, 0, 0, 0, 0, loc)
		st.CalendarDetailPickerMonth = monthCur
	}

	ui.Column(c.Key("cdp-picker")).Padding(10).Radius(10).Gap(8).
		Background(pal.Paper).Border(1, pal.BorderStrong).Shadow(0, 4, 14, 0, pal.ShadowEdge).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Stretch).Children(func() {
			// Left: Mini Month Calendar with << < YYYY年M月 > >>
			ui.Column(c.Key("cdp-cal")).Grow(1).MinWidth(0).Gap(5).Children(func() {
				ui.Row(c).Height(22).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Row(c).Gap(2).Children(func() {
						prevY := ui.Box(c.Key("cdp-py")).Size(20, 20).Radius(4).Center().Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("cdp-py")
						if prevY.Hovered() {
							prevY.Background(pal.RowHover)
						}
						prevY.Children(func() { ui.Icon(c, iconChevronsLeft).FontSize(11).TextColor(pal.TextMuted) })
						if prevY.Clicked() {
							st.CalendarDetailPickerMonth = monthCur.AddDate(-1, 0, 0)
						}

						prevM := ui.Box(c.Key("cdp-pm")).Size(20, 20).Radius(4).Center().Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("cdp-pm")
						if prevM.Hovered() {
							prevM.Background(pal.RowHover)
						}
						prevM.Children(func() { ui.Icon(c, iconChevronLeft).FontSize(11).TextColor(pal.TextMuted) })
						if prevM.Clicked() {
							st.CalendarDetailPickerMonth = monthCur.AddDate(0, -1, 0)
						}
					})

					ui.Text(c, fmt.Sprintf(i18n.T("%d 年 %d 月"), monthCur.Year(), int(monthCur.Month()))).
						FontSize(12).FontWeight(600).TextColor(pal.Text).NoWrap()

					ui.Row(c).Gap(2).Children(func() {
						nextM := ui.Box(c.Key("cdp-nm")).Size(20, 20).Radius(4).Center().Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("cdp-nm")
						if nextM.Hovered() {
							nextM.Background(pal.RowHover)
						}
						nextM.Children(func() { ui.Icon(c, iconChevronRight).FontSize(11).TextColor(pal.TextMuted) })
						if nextM.Clicked() {
							st.CalendarDetailPickerMonth = monthCur.AddDate(0, 1, 0)
						}

						nextY := ui.Box(c.Key("cdp-ny")).Size(20, 20).Radius(4).Center().Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("cdp-ny")
						if nextY.Hovered() {
							nextY.Background(pal.RowHover)
						}
						nextY.Children(func() { ui.Icon(c, iconChevronsRight).FontSize(11).TextColor(pal.TextMuted) })
						if nextY.Clicked() {
							st.CalendarDetailPickerMonth = monthCur.AddDate(1, 0, 0)
						}
					})
				})

				narrowNames := narrowWeekdayNames()
				ui.Box(c).Height(18).Draw(func(p *ui.Painter, r ui.Rect) {
					cw := r.W / 7
					for i := 0; i < 7; i++ {
						wd := (1 + i) % 7
						lbl := narrowNames[wd]
						x := r.X + float32(i)*cw + cw*0.5 - float32(len([]rune(lbl)))*4.0
						p.Text(x, r.Y+3, lbl, 10, pal.TextMuted)
					}
				})

				first := time.Date(monthCur.Year(), monthCur.Month(), 1, 0, 0, 0, 0, loc)
				off := (int(first.Weekday()) - 1 + 7) % 7
				start := first.AddDate(0, 0, -off)
				for rIdx := 0; rIdx < 6; rIdx++ {
					ui.Row(c.Key(fmt.Sprintf("cdp-r:%d", rIdx))).Height(24).AlignItems(ui.Center).Children(func() {
						for cIdx := 0; cIdx < 7; cIdx++ {
							d := start.AddDate(0, 0, rIdx*7+cIdx)
							dKey := d.Format("2006-01-02")
							inM := d.Month() == monthCur.Month()
							sel := d.Year() == curT.Year() && d.Month() == curT.Month() && d.Day() == curT.Day()
							btn := ui.Box(c.Key("cdp-d:" + dKey)).Width(0).Grow(1).Height(24).Center().
								Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("cdp-day:" + dKey)
							cellBox := ui.Box(c).Size(22, 22).Radius(6).Center()
							ink := pal.Text
							if !inM {
								ink = pal.TextMuted.Alpha(0.42)
							}
							if sel {
								cellBox.Background(df.todayBg)
								ink = df.todayFg
							} else if btn.Hovered() {
								cellBox.Background(pal.RowHover)
							}
							btn.Children(func() {
								cellBox.Children(func() {
									ui.Text(c, fmt.Sprintf("%d", d.Day())).FontSize(11).FontWeight(500).TextColor(ink).NoWrap()
								})
							})
							if btn.Clicked() {
								nextT := time.Date(d.Year(), d.Month(), d.Day(), curT.Hour(), curT.Minute(), 0, 0, loc)
								*targetStr = formatDateTimeToken(nextT, st.EditDateIncludeTime)
							}
						}
					})
				}
			})

			// Right: Dual Hour / Minute Picker Columns (when IncludeTime == true).
			if st.EditDateIncludeTime {
				ui.Column(c.Key("cdp-time")).Width(98).Shrink(0).Padding(0, 0, 0, 8).Gap(6).
					BorderWidth(0, 0, 0, 1).BorderColor(pal.Border).Children(func() {
					ui.Box(c).Height(22).Center().Children(func() {
						ui.Text(c, curT.Format("15:04")).FontSize(12.5).FontWeight(600).TextColor(pal.Text).NoWrap()
					})

					minChoices := []int{0, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55}
					if !slices.Contains(minChoices, curT.Minute()) {
						minChoices = append(minChoices, curT.Minute())
						slices.Sort(minChoices)
					}

					ui.Row(c).Height(156).Gap(4).AlignItems(ui.Start).Children(func() {
						// Hours 00..23
						ui.Scroll(c.Key("cdp-hrs")).TrackScroll(&st.CalendarDetailHourScroll).
							Width(0).Grow(1).Height(156).MaxHeight(156).Clip().Children(func() {
							ui.Column(c).Gap(2).Children(func() {
								for h := 0; h < 24; h++ {
									hr := h
									active := curT.Hour() == hr
									item := ui.Box(c.Key(fmt.Sprintf("cdph:%02d", hr))).Height(22).Radius(5).Center().
										Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(fmt.Sprintf("%02d", hr))
									ink := pal.Text
									if active {
										item.Background(df.todayBg)
										ink = df.todayFg
									} else if item.Hovered() {
										item.Background(pal.RowHover)
									}
									item.Children(func() {
										ui.Text(c, fmt.Sprintf("%02d", hr)).FontSize(11).FontWeight(500).TextColor(ink).NoWrap()
									})
									if item.Clicked() {
										nextT := time.Date(curT.Year(), curT.Month(), curT.Day(), hr, curT.Minute(), 0, 0, loc)
										*targetStr = formatDateTimeToken(nextT, true)
									}
								}
							})
						})

						// Minutes
						ui.Scroll(c.Key("cdp-mins")).TrackScroll(&st.CalendarDetailMinScroll).
							Width(0).Grow(1).Height(156).MaxHeight(156).Clip().Children(func() {
							ui.Column(c).Gap(2).Children(func() {
								for _, m := range minChoices {
									mn := m
									active := curT.Minute() == mn
									item := ui.Box(c.Key(fmt.Sprintf("cdpm:%02d", mn))).Height(22).Radius(5).Center().
										Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(fmt.Sprintf("%02d", mn))
									ink := pal.Text
									if active {
										item.Background(df.todayBg)
										ink = df.todayFg
									} else if item.Hovered() {
										item.Background(pal.RowHover)
									}
									item.Children(func() {
										ui.Text(c, fmt.Sprintf("%02d", mn)).FontSize(11).FontWeight(500).TextColor(ink).NoWrap()
									})
									if item.Clicked() {
										nextT := time.Date(curT.Year(), curT.Month(), curT.Day(), curT.Hour(), mn, 0, 0, loc)
										*targetStr = formatDateTimeToken(nextT, true)
									}
								}
							})
						})
					})
				})
			}
		})

		// Bottom-right Confirm pill button matching popup.png.
		ui.Row(c).Justify(ui.End).Children(func() {
			okBtn := ui.Row(c.Key("cdp-ok")).Height(26).Padding(0, 14).Radius(7).Center().
				Background(df.todayBg).Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("确定"))
			okBtn.Children(func() {
				ui.Text(c, i18n.T("确定")).FontSize(12).FontWeight(600).TextColor(df.todayFg).NoWrap()
			})
			if okBtn.Clicked() {
				sVal := st.EditDateStart
				eVal := st.EditDateEnd
				if sT, _, okS := database.ParseDateString(sVal, loc); okS {
					if eT, _, okE := database.ParseDateString(eVal, loc); okE && eT.Before(sT) {
						eVal = sVal
						st.EditDateEnd = eVal
					}
				}
				if eVal == sVal && !st.EditDateIncludeTime {
					eVal = ""
				}
				commitTitle()
				st.setCellAndNotify(ev.rec.ID, dateProp.ID, database.DateRangeValue(sVal, eVal, st.EditDateIncludeTime), opts)
				st.CalendarDetailPickerOpen = false
			}
		})
	})
}
