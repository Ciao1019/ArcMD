package dbview

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

func dateOnly(t time.Time) time.Time {
	loc := t.Location()
	if loc == nil {
		loc = time.UTC
	}
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

func daysBetween(a, b time.Time) int {
	// Use UTC calendar dates to avoid DST 23h/25h rounding errors.
	ua := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
	ub := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	return int(ub.Sub(ua) / (24 * time.Hour))
}

func parseRecordDateRange(
	rec *database.Record,
	startProp database.Property,
	endProp *database.Property,
	loc *time.Location,
) (startDay, endDay time.Time, startVal, endVal database.DateValue, ok bool) {
	if rec == nil {
		return time.Time{}, time.Time{}, database.DateValue{}, database.DateValue{}, false
	}
	if loc == nil {
		loc = time.UTC
	}
	sv := rec.ValueFor(startProp)
	if sv.IsEmpty() || sv.Kind != database.ValDate {
		return time.Time{}, time.Time{}, database.DateValue{}, database.DateValue{}, false
	}
	stTime, _, sok := database.ParseDateString(sv.Date.Start, loc)
	if !sok {
		return time.Time{}, time.Time{}, database.DateValue{}, database.DateValue{}, false
	}
	sDay := dateOnly(stTime)
	eDay := sDay

	if endProp != nil && endProp.ID != startProp.ID {
		ev := rec.ValueFor(*endProp)
		if !ev.IsEmpty() && ev.Kind == database.ValDate {
			endVal = ev.Date
			if etTime, _, eok := database.ParseDateString(ev.Date.Start, loc); eok {
				ed := dateOnly(etTime)
				if !ed.Before(sDay) {
					eDay = ed
				}
			}
		}
	} else if sv.Date.End != "" {
		if etTime, _, eok := database.ParseDateString(sv.Date.End, loc); eok {
			ed := dateOnly(etTime)
			if !ed.Before(sDay) {
				eDay = ed
			}
		}
	}
	return sDay, eDay, sv.Date, endVal, true
}

func shiftDateString(orig string, deltaDays int, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	t, hasTime, ok := database.ParseDateString(orig, loc)
	if !ok {
		return orig
	}
	shifted := t.AddDate(0, 0, deltaDays)
	if hasTime {
		return shifted.Format("2006-01-02T15:04")
	}
	return shifted.Format("2006-01-02")
}

func recordAccentColorForProp(prop *database.Property, rec *database.Record) string {
	if prop == nil || rec == nil || !isColorablePropType(prop.Type) {
		return ""
	}
	v := rec.ValueFor(*prop)
	if !v.IsEmpty() {
		opt := findOption(*prop, v.Text)
		if opt.Color != "" && opt.Color != "default" {
			return opt.Color
		}
	}
	return ""
}

func calendarView(
	c *ui.Context,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	opts Options,
	pal Palette,
) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	loc := now.Location()
	if loc == nil {
		loc = time.UTC
	}
	today := dateOnly(now.In(loc))
	if st.CalendarCursor.IsZero() {
		st.CalendarCursor = today
	}
	cursor := dateOnly(st.CalendarCursor.In(loc))
	if st.CalendarMiniMonth.IsZero() {
		st.CalendarMiniMonth = time.Date(cursor.Year(), cursor.Month(), 1, 0, 0, 0, 0, loc)
	}

	ccfg := database.CalendarViewConfig{
		Mode:         "month",
		YearMode:     "fixed-week",
		ShowWeekends: true,
		WeekStart:    1,
	}
	if view.Calendar != nil {
		ccfg = *view.Calendar
		if ccfg.Mode == "" {
			ccfg.Mode = "month"
		}
		if ccfg.YearMode == "" {
			ccfg.YearMode = "fixed-week"
		}
	}

	dateProp, usingBuiltinDate, explicitBuiltin := resolveDateBinding(db, ccfg.DatePropID)
	catProp := resolveColorProp(db, ccfg.ColorPropID)

	var allRecords []*database.Record
	if res != nil {
		allRecords = res.Rows
	}
	categories := collectCalendarCategories(catProp, allRecords)

	var visibleEvents []calEvent
	var unscheduled []*database.Record
	eventsByDay := make(map[string][]calEvent)

	for _, r := range allRecords {
		if r == nil {
			continue
		}
		ev, ok := parseRecordCalendarEvent(res, r, dateProp, catProp, loc)
		if !ok {
			unscheduled = append(unscheduled, r)
			continue
		}
		if st.CalendarHiddenCats != nil && st.CalendarHiddenCats[ev.category] {
			continue
		}
		visibleEvents = append(visibleEvents, ev)
		// Index by day for MiniCalendar dots and fast single-day lookups.
		for cur := ev.startDay; !cur.After(ev.endDay); cur = cur.AddDate(0, 0, 1) {
			k := cur.Format("2006-01-02")
			eventsByDay[k] = append(eventsByDay[k], ev)
		}
	}

	df := dfCalendarPalette(pal)
	df.catProp = catProp
	readOnlyDate := opts.ReadOnly || dateProp.Type != database.PropDate
	openMode := resolveCardOpenMode(view)
	showSidebar := ccfg.ShowSidebar != nil && *ccfg.ShowSidebar && !opts.Embedded

	// If the calendar has a writable date property and every record is unscheduled,
	// open the Unscheduled drawer automatically once per view so data stays visible.
	if !readOnlyDate && len(visibleEvents) == 0 && len(unscheduled) > 0 && st.CalendarAutoUnschedViewID != view.ID {
		st.CalendarAutoUnschedViewID = view.ID
		st.CalendarUnscheduledOpen = true
	}

	st.calDetailMatchedAnchor = false
	st.calDetailFallbackKey = ""
	st.calDetailFallbackAnchor = ui.Element{}
	st.calDetailFallbackEv = calEvent{}

	ui.Row(c.Key("cal-shell")).Grow(1).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		if showSidebar {
			renderCalendarSidebar(c, st, view, db, cursor, today, ccfg, catProp, categories, eventsByDay, opts, pal, df, loc)
		}

		ui.Column(c.Key("cal-root")).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
			renderCalendarHeader(c, st, view, db, dateProp, cursor, today, ccfg, len(unscheduled), readOnlyDate, opts, pal, df, loc)
			renderCalendarBindingBar(c, st, view, db, ccfg, dateProp, catProp, usingBuiltinDate, explicitBuiltin, opts, pal)

			ui.Row(c.Key("cal-body")).Grow(1).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				switch ccfg.Mode {
				case "day":
					renderDayTimeGridView(c, st, &view, db, res, dateProp, visibleEvents, eventsByDay, now.In(loc), cursor, today, ccfg, readOnlyDate, openMode, opts, pal, df, loc)
				case "week":
					renderWeekTimeGridView(c, st, &view, db, res, dateProp, visibleEvents, now.In(loc), cursor, today, ccfg, readOnlyDate, openMode, opts, pal, df, loc)
				case "year":
					renderYearView(c, st, &view, db, dateProp, visibleEvents, cursor, today, ccfg, readOnlyDate, openMode, opts, pal, df, loc)
				case "agenda":
					renderAgendaView(c, st, &view, db, dateProp, visibleEvents, cursor, today, ccfg, readOnlyDate, openMode, opts, pal, df, loc)
				default:
					renderMonthView(c, st, &view, db, dateProp, visibleEvents, eventsByDay, cursor, today, ccfg, readOnlyDate, openMode, opts, pal, df, loc)
				}

				if st.CalendarUnscheduledOpen {
					renderCalendarUnscheduledDrawer(c, st, db, dateProp, catProp, unscheduled, today, readOnlyDate, openMode, opts, pal, loc)
				}

				if st.CalendarDetailOpen && !st.calDetailMatchedAnchor && st.calDetailFallbackKey != "" {
					st.CalendarDetailAnchorKey = st.calDetailFallbackKey
					renderCalendarEventDetailPopover(c, st.calDetailFallbackAnchor, st, db, dateProp, st.calDetailFallbackEv, openMode, opts, pal, df, loc)
				}
				st.calDetailFallbackAnchor = ui.Element{}
			})
		})
	})
}

func renderCalendarBindingBar(
	c *ui.Context,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	ccfg database.CalendarViewConfig,
	dateProp database.Property,
	catProp *database.Property,
	usingBuiltinDate bool,
	explicitBuiltin bool,
	opts Options,
	pal Palette,
) {
	curDateID := ccfg.DatePropID
	if curDateID == "" {
		curDateID = dateProp.ID
	}
	colorName := i18n.T("无")
	if catProp != nil {
		colorName = catProp.Name
	}

	barBg := pal.HeaderBg.Alpha(0.55)
	if usingBuiltinDate && !explicitBuiltin {
		barBg = pal.AccentSoft.Alpha(0.45)
	}

	ui.Row(c.Key("cal-bind-bar")).Height(34).Padding(0, 16).Gap(8).AlignItems(ui.Center).
		Background(barBg).BorderWidth(0, 0, 1, 0).BorderColor(pal.Border.Alpha(0.55)).Children(func() {
		dateBtn := renderFieldBindingPill(c, "cal-bind-date", i18n.T("日期字段"), dateProp.Name, usingBuiltinDate && !explicitBuiltin, pal)
		dateBtn.PopoverMenuButton(func(m *ui.Menu) {
			appendDatePropMenuItems(m, db, curDateID, false, opts.ReadOnly, func(propID string) {
				vc := view.Clone()
				ensureCalendarConfig(&vc, db).DatePropID = propID
				saveViewAndNotify(st, vc, opts)
			}, func() {
				addDefaultDateProperty(st, db, view, opts)
			})
		})

		colorBtn := renderFieldBindingPill(c, "cal-bind-color", i18n.T("分类着色"), colorName, catProp == nil, pal)
		colorBtn.PopoverMenuButton(func(m *ui.Menu) {
			appendColorPropMenuItems(m, db, catProp, ccfg.ColorPropID, i18n.T("不按分类着色"), opts.ReadOnly, func(propID string) {
				vc := view.Clone()
				ensureCalendarConfig(&vc, db).ColorPropID = propID
				saveViewAndNotify(st, vc, opts)
			}, func() {
				addDefaultStatusProperty(st, db, view, opts)
			})
		})

		ui.Spacer(c)

		if usingBuiltinDate && !explicitBuiltin && !opts.ReadOnly {
			ui.Text(c, i18n.T("当前按「修改时间」展示")).FontSize(11.5).TextColor(pal.TextMuted).SingleLine()
			addBtn := ui.Row(c.Key("cal-add-date-prop")).Height(22).Padding(0, 8).Radius(5).
				Background(pal.Paper).Border(1, pal.Accent.Alpha(0.35)).AlignItems(ui.Center).
				Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("+ " + i18n.T("添加「排期」字段"))
			if addBtn.Hovered() {
				addBtn.Background(pal.AccentSoft)
			}
			addBtn.Children(func() {
				ui.Text(c, "+ "+i18n.T("添加「排期」字段")).FontSize(11.5).FontWeight(500).TextColor(pal.Accent).NoWrap()
			})
			if addBtn.Clicked() {
				addDefaultDateProperty(st, db, view, opts)
			}
		}
	})
}

func buildCalendarGrid(cursor time.Time, cfg database.CalendarViewConfig, loc *time.Location) (rows [][]time.Time, gridStart, gridEnd time.Time) {
	ws := cfg.WeekStart
	if ws != 0 {
		ws = 1
	}

	if cfg.Mode == "week" {
		offset := (int(cursor.Weekday()) - ws + 7) % 7
		start := cursor.AddDate(0, 0, -offset)
		var week []time.Time
		for i := 0; i < 7; i++ {
			d := start.AddDate(0, 0, i)
			if !cfg.ShowWeekends && (d.Weekday() == time.Saturday || d.Weekday() == time.Sunday) {
				continue
			}
			week = append(week, d)
		}
		return [][]time.Time{week}, start, start.AddDate(0, 0, 6)
	}

	firstOfMonth := time.Date(cursor.Year(), cursor.Month(), 1, 0, 0, 0, 0, loc)
	lastOfMonth := firstOfMonth.AddDate(0, 1, -1)
	offset := (int(firstOfMonth.Weekday()) - ws + 7) % 7
	start := firstOfMonth.AddDate(0, 0, -offset)

	cur := start
	for len(rows) < 6 {
		var week []time.Time
		for i := 0; i < 7; i++ {
			d := cur.AddDate(0, 0, i)
			if !cfg.ShowWeekends && (d.Weekday() == time.Saturday || d.Weekday() == time.Sunday) {
				continue
			}
			week = append(week, d)
		}
		rows = append(rows, week)
		cur = cur.AddDate(0, 0, 7)
		if cur.After(lastOfMonth) && len(rows) >= 5 {
			break
		}
	}
	return rows, start, cur.AddDate(0, 0, -1)
}

func renderCalendarSidebar(
	c *ui.Context,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	cursor, today time.Time,
	cfg database.CalendarViewConfig,
	catProp *database.Property,
	categories []calCategoryItem,
	eventsByDay map[string][]calEvent,
	opts Options,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
) {
	_ = view
	_ = db
	_ = opts

	ui.Column(c.Key("cal-sidebar")).Width(212).Shrink(0).
		Background(df.sidebarBg).BorderWidth(0, 1, 0, 0).BorderColor(pal.Border).Children(func() {
		// Top section: DayFlow color-coded calendar categories.
		ui.Row(c.Key("cal-sb-head")).Height(38).Padding(0, 12).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			title := i18n.T("日历分类")
			if catProp != nil && catProp.Name != "" {
				title = catProp.Name
			}
			ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(pal.TextMuted).SingleLine()
			if len(st.CalendarHiddenCats) > 0 {
				showAllBtn := ui.ButtonBase(c.Key("cal-sb-all")).Padding(2, 6).Radius(4).
					Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("全部显示"))
				if showAllBtn.Hovered() {
					showAllBtn.Background(pal.RowHover)
				}
				showAllBtn.Children(func() {
					ui.Text(c, i18n.T("全部显示")).FontSize(11).TextColor(pal.Accent).NoWrap()
				})
				if showAllBtn.Clicked() {
					clear(st.CalendarHiddenCats)
				}
			}
		})

		ui.Scroll(c.Key("cal-sb-cats")).Grow(1).MinHeight(0).Padding(0, 8, 8, 8).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				for _, cat := range categories {
					item := cat
					checked := !st.CalendarHiddenCats[item.key]
					evCol := dayflowEventColors(pal.Dark, item.color)

					row := ui.Row(c.Key("calcat:"+item.key)).Height(30).Padding(0, 8).Radius(6).Gap(9).
						AlignItems(ui.Center).Cursor(ui.CursorPointer).
						Role(ui.RoleCheckBox).Checked(checked).Label(item.label)
					if row.Hovered() {
						row.Background(pal.RowHover)
					}
					row.Children(func() {
						box := ui.Box(c).Size(15, 15).Radius(4).Center().Shrink(0)
						if checked {
							box.Background(evCol.line)
							box.Children(func() {
								ui.Icon(c, iconCheck).FontSize(10).TextColor(ui.RGB(255, 255, 255))
							})
						} else {
							box.Background(ui.Transparent).Border(1.5, evCol.line)
						}

						ink := pal.Text
						if !checked {
							ink = pal.TextMuted
						}
						ui.Text(c, item.label).FontSize(13).TextColor(ink).SingleLine().Grow(1).MinWidth(0)
						if item.count > 0 {
							ui.Text(c, fmt.Sprintf("%d", item.count)).FontSize(11).TextColor(pal.TextMuted).NoWrap()
						}
					})
					if row.Clicked() {
						if st.CalendarHiddenCats == nil {
							st.CalendarHiddenCats = make(map[string]bool)
						}
						if checked {
							st.CalendarHiddenCats[item.key] = true
						} else {
							delete(st.CalendarHiddenCats, item.key)
						}
					}
				}
			})
		})

		// Bottom section: DayFlow MiniCalendar with event dots.
		ui.Box(c.Key("cal-sb-mini")).Padding(10, 12, 14, 12).
			BorderWidth(1, 0, 0, 0).BorderColor(pal.Border).Children(func() {
			renderMiniCalendar(
				c, "sidebar", st.CalendarMiniMonth, cursor, today, cfg.WeekStart,
				eventsByDay, pal, df, loc,
				func(deltaMonths int) {
					st.CalendarMiniMonth = st.CalendarMiniMonth.AddDate(0, deltaMonths, 0)
				},
				func(picked time.Time) {
					st.CalendarCursor = picked
					st.CalendarMiniMonth = time.Date(picked.Year(), picked.Month(), 1, 0, 0, 0, 0, loc)
				},
			)
		})
	})
}

func renderMiniCalendar(
	c *ui.Context,
	scopeKey string,
	monthCursor, selectedDay, today time.Time,
	weekStart int,
	eventsByDay map[string][]calEvent,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
	onMonthChange func(deltaMonths int),
	onSelectDate func(time.Time),
) {
	if loc == nil {
		loc = time.UTC
	}
	if monthCursor.IsZero() {
		monthCursor = time.Date(selectedDay.Year(), selectedDay.Month(), 1, 0, 0, 0, 0, loc)
	}
	ws := 1
	if weekStart == 0 {
		ws = 0
	}

	ui.Column(c.Key("mini-cal:" + scopeKey)).Gap(6).Children(func() {
		// Header: <  2026年11月  >
		ui.Row(c.Key("mini-hdr:" + scopeKey)).Height(24).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			prev := ui.Box(c.Key("mini-prev:"+scopeKey)).Size(22, 22).Radius(5).Center().
				Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("mini-prev:" + scopeKey)
			if prev.Hovered() {
				prev.Background(pal.RowHover)
			}
			prev.Children(func() { ui.Icon(c, iconChevronLeft).FontSize(13).TextColor(pal.TextMuted) })
			if prev.Clicked() && onMonthChange != nil {
				onMonthChange(-1)
			}

			ui.Text(c, fmt.Sprintf(i18n.T("%d 年 %d 月"), monthCursor.Year(), int(monthCursor.Month()))).
				FontSize(12.5).FontWeight(600).TextColor(pal.Text).SingleLine()

			next := ui.Box(c.Key("mini-next:"+scopeKey)).Size(22, 22).Radius(5).Center().
				Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("mini-next:" + scopeKey)
			if next.Hovered() {
				next.Background(pal.RowHover)
			}
			next.Children(func() { ui.Icon(c, iconChevronRight).FontSize(13).TextColor(pal.TextMuted) })
			if next.Clicked() && onMonthChange != nil {
				onMonthChange(1)
			}
		})

		// Narrow weekday header drawn cleanly without competing with main view tabs in text/click lookup.
		narrowNames := narrowWeekdayNames()
		ui.Box(c.Key("mini-wk:" + scopeKey)).Height(18).Draw(func(p *ui.Painter, r ui.Rect) {
			cw := r.W / 7
			for i := 0; i < 7; i++ {
				wd := (ws + i) % 7
				lbl := narrowNames[wd]
				x := r.X + float32(i)*cw + cw*0.5 - float32(len([]rune(lbl)))*4.2
				p.Text(x, r.Y+3, lbl, 10.5, pal.TextMuted)
			}
		})

		first := time.Date(monthCursor.Year(), monthCursor.Month(), 1, 0, 0, 0, 0, loc)
		offset := (int(first.Weekday()) - ws + 7) % 7
		start := first.AddDate(0, 0, -offset)

		for rowIdx := 0; rowIdx < 6; rowIdx++ {
			ui.Row(c.Key(fmt.Sprintf("mini-r:%s:%d", scopeKey, rowIdx))).Height(26).AlignItems(ui.Center).Children(func() {
				for colIdx := 0; colIdx < 7; colIdx++ {
					day := start.AddDate(0, 0, rowIdx*7+colIdx)
					dKey := day.Format("2006-01-02")
					inMonth := day.Month() == monthCursor.Month() && day.Year() == monthCursor.Year()
					isToday := day.Year() == today.Year() && day.Month() == today.Month() && day.Day() == today.Day()
					isSelected := !selectedDay.IsZero() && day.Year() == selectedDay.Year() && day.Month() == selectedDay.Month() && day.Day() == selectedDay.Day()

					cell := ui.Column(c.Key("mini-d:" + scopeKey + ":" + dKey)).Width(0).Grow(1).Height(26).
						Center().Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("mini-day:" + dKey)

					cell.Children(func() {
						numBox := ui.Box(c).Size(20, 20).Radius(10).Center()
						numColor := pal.Text
						if !inMonth {
							numColor = pal.TextMuted.Alpha(0.42)
						}
						switch {
						case isToday:
							numBox.Background(df.todayBg)
							numColor = df.todayFg
						case isSelected:
							numBox.Background(df.cellSelectedBg).Border(1, pal.BorderStrong)
						case cell.Hovered():
							numBox.Background(pal.RowHover)
						}
						numBox.Children(func() {
							ui.Text(c, fmt.Sprintf("%d", day.Day())).FontSize(11).FontWeight(500).TextColor(numColor).NoWrap()
						})

						if eventsByDay != nil {
							if evs := eventsByDay[dKey]; len(evs) > 0 {
								ui.Row(c).Gap(2).Height(4).AlignItems(ui.Center).Children(func() {
									shown := 0
									seenCols := map[string]bool{}
									for _, ev := range evs {
										if seenCols[ev.color] {
											continue
										}
										seenCols[ev.color] = true
										ui.Box(c).Size(3, 3).Radius(1.5).Background(dayflowEventColors(pal.Dark, ev.color).line)
										shown++
										if shown >= 3 {
											break
										}
									}
								})
							}
						}
					})
					if cell.Clicked() && onSelectDate != nil {
						onSelectDate(day)
					}
				}
			})
		}
	})
}

func renderCalendarHeader(
	c *ui.Context,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	dateProp database.Property,
	cursor, today time.Time,
	cfg database.CalendarViewConfig,
	unscheduledCount int,
	readOnlyDate bool,
	opts Options,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
) {
	titleText := fmt.Sprintf(i18n.T("%d 年 %d 月"), cursor.Year(), int(cursor.Month()))
	switch cfg.Mode {
	case "day":
		titleText = fmt.Sprintf(i18n.T("%d 年 %d 月 %d 日"), cursor.Year(), int(cursor.Month()), cursor.Day())
	case "week":
		ws := cfg.WeekStart
		if ws != 0 {
			ws = 1
		}
		offset := (int(cursor.Weekday()) - ws + 7) % 7
		wStart := cursor.AddDate(0, 0, -offset)
		wEnd := wStart.AddDate(0, 0, 6)
		titleText = fmt.Sprintf("%s – %s", wStart.Format("2006-01-02"), wEnd.Format("01-02"))
	case "year":
		titleText = fmt.Sprintf(i18n.T("%d 年"), cursor.Year())
	case "agenda":
		titleText = fmt.Sprintf(i18n.T("%d 年 %d 月 · 日程"), cursor.Year(), int(cursor.Month()))
	}

	stepPeriod := func(dir int) {
		var next time.Time
		switch cfg.Mode {
		case "day":
			next = cursor.AddDate(0, 0, dir)
		case "week":
			next = cursor.AddDate(0, 0, 7*dir)
		case "year":
			next = cursor.AddDate(dir, 0, 0)
		default:
			next = cursor.AddDate(0, dir, 0)
		}
		st.CalendarCursor = next
		st.CalendarMiniMonth = time.Date(next.Year(), next.Month(), 1, 0, 0, 0, 0, loc)
	}

	showSidebar := cfg.ShowSidebar != nil && *cfg.ShowSidebar

	ui.Row(c.Key("cal-hdr")).Height(46).Padding(4, 16).Gap(8).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(pal.Border.Alpha(0.65)).Children(func() {
		// Sidebar toggle & quick create button matching DayFlow header.
		if !opts.Embedded {
			sbBtn := ui.Box(c.Key("cal-sb-toggle")).Size(28, 28).Radius(6).Center().
				Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).
				Label(i18n.T("切换日历侧边栏")).Tooltip(i18n.T("切换日历侧边栏"))
			if showSidebar {
				sbBtn.Background(pal.Surface)
			} else if sbBtn.Hovered() {
				sbBtn.Background(pal.RowHover)
			}
			sbBtn.Children(func() {
				ink := pal.TextMuted
				if showSidebar {
					ink = pal.Text
				}
				ui.Icon(c, iconPanelLeft).FontSize(14).TextColor(ink)
			})
			if sbBtn.Clicked() {
				vc := view.Clone()
				next := !showSidebar
				ensureCalendarConfig(&vc, db).ShowSidebar = &next
				saveViewAndNotify(st, vc, opts)
			}
		}

		if !readOnlyDate {
			addHdrBtn := ui.Box(c.Key("cal-quick-add")).Size(28, 28).Radius(6).Center().
				Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).
				Label(i18n.T("快速新建日程")).Tooltip(i18n.T("新建日程"))
			if addHdrBtn.Hovered() {
				addHdrBtn.Background(pal.RowHover)
			}
			addHdrBtn.Children(func() { ui.Icon(c, iconPlus).FontSize(15).TextColor(pal.TextMuted) })
			if addHdrBtn.Clicked() {
				initVal := database.DateRangeValue(cursor.Format("2006-01-02"), "", false)
				if cfg.Mode == "day" || cfg.Mode == "week" {
					initVal = database.DateRangeValue(cursor.Format("2006-01-02")+"T09:00", cursor.Format("2006-01-02")+"T10:00", true)
				}
				createRecordWithInitial(st, db, map[string]database.Value{
					dateProp.ID: initVal,
				}, opts)
			}
		}

		ui.Text(c, titleText).FontSize(17.5).FontWeight(700).TextColor(pal.Text).SingleLine()

		ui.Spacer(c)

		// DayFlow Segmented View Switcher: Day | Week | Month | Year | Agenda
		ui.Row(c.Key("cal-mode-seg")).Gap(2).Padding(2).Radius(9).Background(df.segTrackBg).Children(func() {
			for _, m := range []struct{ id, label string }{
				{"day", i18n.T("日")},
				{"week", i18n.T("周")},
				{"month", i18n.T("月")},
				{"year", i18n.T("年")},
				{"agenda", i18n.T("日程")},
			} {
				cand := m
				active := cfg.Mode == cand.id
				btn := ui.ButtonBase(c.Key("cm:"+cand.id)).Height(24).Padding(0, 10).Radius(7).
					AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleTab).Selected(active).Label(cand.label).
					Background(ui.Transparent).TextColor(pal.Text).
					Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
				if !active && btn.Hovered() {
					btn.Background(pal.RowHover)
				}
				applyPressSpring(btn, false)
				btn.Children(func() {
					if active {
						ui.Box(c.Key("cm-pill")).Absolute().Left(0).Top(0).Right(0).Bottom(0).Radius(7).
							Background(df.segActiveBg).Shadow(0, 1, 2, 0, pal.ShadowEdge).
							IgnoreInput(true).ZIndex(0).
							Transition(ui.ElementTransition{
								Animation: &dbSpringSnappy,
								Match:     calModePillMatch{},
							})
					}
					ink := pal.TextMuted
					if active {
						ink = pal.Text
					}
					ui.Text(c, cand.label).FontSize(12).FontWeight(500).TextColor(ink).SingleLine().ZIndex(1)
				})
				if btn.Clicked() && !active {
					vc := view.Clone()
					ensureCalendarConfig(&vc, db).Mode = cand.id
					saveViewAndNotify(st, vc, opts)
				}
			}
		})

		if cfg.Mode == "year" {
			yMode := cfg.YearMode
			if yMode == "" {
				yMode = "fixed-week"
			}
			ui.Row(c.Key("cal-ymode-seg")).Gap(2).Padding(2).Radius(8).Background(df.segTrackBg).Children(func() {
				for _, ym := range []struct{ id, label string }{
					{"fixed-week", i18n.T("按月对齐")},
					{"year-canvas", i18n.T("连续画布")},
				} {
					cand := ym
					active := yMode == cand.id
					btn := ui.ButtonBase(c.Key("cym:"+cand.id)).Height(24).Padding(0, 8).Radius(6).
						AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleTab).Selected(active).Label(cand.label)
					if active {
						btn.Background(df.segActiveBg).Shadow(0, 1, 2, 0, pal.ShadowEdge)
					} else if btn.Hovered() {
						btn.Background(pal.RowHover)
					}
					btn.Children(func() {
						ink := pal.TextMuted
						if active {
							ink = pal.Text
						}
						ui.Text(c, cand.label).FontSize(11.5).FontWeight(500).TextColor(ink).SingleLine()
					})
					if btn.Clicked() && !active {
						vc := view.Clone()
						ensureCalendarConfig(&vc, db).YearMode = cand.id
						saveViewAndNotify(st, vc, opts)
					}
				}
			})
		}

		// DayFlow unified TodayBox: < | Today | >
		ui.Row(c.Key("cal-nav-group")).Height(28).Radius(7).Background(df.headerBtnBg).
			Border(1, df.headerBtnBorder).AlignItems(ui.Center).Clip().Children(func() {
			prevBtn := ui.Box(c.Key("cal-prev")).Size(26, 26).Center().
				Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("上一周期")).Tooltip(i18n.T("上一周期"))
			if prevBtn.Hovered() {
				prevBtn.Background(pal.RowHover)
			}
			prevBtn.Children(func() { ui.Icon(c, iconChevronLeft).FontSize(13).TextColor(pal.TextMuted) })
			if prevBtn.Clicked() {
				stepPeriod(-1)
			}

			ui.Box(c).Width(1).Height(16).Background(df.headerBtnBorder)

			todayBtn := ui.ButtonBase(c.Key("cal-today")).Height(26).Padding(0, 10).
				AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("今天"))
			if todayBtn.Hovered() {
				todayBtn.Background(pal.RowHover)
			}
			todayBtn.Children(func() {
				ui.Text(c, i18n.T("今天")).FontSize(12).FontWeight(500).TextColor(pal.Text).SingleLine()
			})
			if todayBtn.Clicked() {
				st.CalendarCursor = today
				st.CalendarMiniMonth = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, loc)
			}

			ui.Box(c).Width(1).Height(16).Background(df.headerBtnBorder)

			nextBtn := ui.Box(c.Key("cal-next")).Size(26, 26).Center().
				Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("下一周期")).Tooltip(i18n.T("下一周期"))
			if nextBtn.Hovered() {
				nextBtn.Background(pal.RowHover)
			}
			nextBtn.Children(func() { ui.Icon(c, iconChevronRight).FontSize(13).TextColor(pal.TextMuted) })
			if nextBtn.Clicked() {
				stepPeriod(1)
			}
		})

		unschedLbl := fmt.Sprintf("%s (%d)", i18n.T("未排期"), unscheduledCount)
		unschedBtn := toolbarPillBtn(c, "cal-unsched-btn", iconViewList, unschedLbl, st.CalendarUnscheduledOpen, pal)
		if unschedBtn.Clicked() {
			st.CalendarUnscheduledOpen = !st.CalendarUnscheduledOpen
		}
	})
}

func renderCalendarWeekdayBar(c *ui.Context, cfg database.CalendarViewConfig, pal Palette) {
	ws := cfg.WeekStart
	if ws != 0 {
		ws = 1
	}
	names := []string{
		i18n.T("周日"), i18n.T("周一"), i18n.T("周二"), i18n.T("周三"),
		i18n.T("周四"), i18n.T("周五"), i18n.T("周六"),
	}
	ui.Row(c.Key("cal-wkdays")).Height(28).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(pal.Border.Alpha(0.7)).Children(func() {
		for i := 0; i < 7; i++ {
			wd := (ws + i) % 7
			if !cfg.ShowWeekends && (wd == 0 || wd == 6) {
				continue
			}
			ui.Box(c).Width(0).Grow(1).MinWidth(0).Padding(0, 8).AlignItems(ui.End).Children(func() {
				ui.Text(c, names[wd]).FontSize(11.5).FontWeight(500).TextColor(pal.TextMuted).SingleLine()
			})
		}
	})
}

func renderMonthView(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	dateProp database.Property,
	visibleEvents []calEvent,
	eventsByDay map[string][]calEvent,
	cursor, today time.Time,
	ccfg database.CalendarViewConfig,
	readOnlyDate bool,
	openMode string,
	opts Options,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
) {
	weekRows, gridStart, gridEnd := buildCalendarGrid(cursor, ccfg, loc)
	_ = gridStart
	_ = gridEnd

	ui.Column(c.Key("cal-main")).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
		renderCalendarWeekdayBar(c, ccfg, pal)

		scroll := ui.Scroll(c.Key("cal-scroll")).TrackScroll(&st.CalendarScroll).Grow(1).MinHeight(0)
		gridW := scroll.LayoutBounds().W
		if gridW <= 0 {
			c.AnimationFrame()
			gridW = 880
		}

		scroll.Children(func() {
			ui.Column(c.Key("cal-weeks")).Children(func() {
				for wi, wDays := range weekRows {
					renderMonthWeekRow(c, st, view, db, dateProp, wDays, visibleEvents, eventsByDay, today, cursor, gridW, readOnlyDate, openMode, opts, pal, df, loc)
					if wi+1 < len(weekRows) {
						ui.Box(c).Height(1).Background(df.gridLine)
					}
				}
			})
		})
	})
}

func renderMonthWeekRow(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	dateProp database.Property,
	days []time.Time,
	visibleEvents []calEvent,
	eventsByDay map[string][]calEvent,
	today, cursor time.Time,
	gridW float32,
	readOnlyDate bool,
	openMode string,
	opts Options,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
) {
	if len(days) == 0 {
		return
	}
	numCols := len(days)
	dayW := (gridW - float32(numCols-1)) / float32(numCols)
	if dayW < 40 {
		dayW = 110
	}

	// Partition multi-day spanning events (or active drag-resizing events) into top horizontal tracks.
	var multiEvents []calEvent
	multiSet := map[string]bool{}
	for _, ev := range visibleEvents {
		sDay, eDay, resizing := calSpanPreviewRange(st, ev, dayW)
		if eDay.After(sDay) || resizing {
			multiEvents = append(multiEvents, ev)
			multiSet[ev.rec.ID] = true
		}
	}
	lanes := packSpanSegments(st, multiEvents, days, dayW)
	const spanRowH float32 = 21
	const spanGap float32 = 2
	const headerH float32 = 26
	spanAreaH := float32(len(lanes)) * (spanRowH + spanGap)
	maxSingleInWeek := 0
	for _, day := range days {
		cnt := 0
		for _, ev := range eventsByDay[day.Format("2006-01-02")] {
			if !multiSet[ev.rec.ID] {
				cnt++
			}
		}
		if cnt > maxSingleInWeek {
			maxSingleInWeek = cnt
		}
	}
	shownSlots := min(3, maxSingleInWeek)
	rowMinH := max(float32(108), headerH+spanAreaH+float32(shownSlots)*23+12)

	weekKey := days[0].Format("2006-01-02")
	weekBox := ui.Box(c.Key("cweek:" + weekKey)).MinHeight(rowMinH)
	weekBox.Children(func() {
		// Layer 1: Background grid of day cells.
		ui.Row(c.Key("cweek-cells:" + weekKey)).MinHeight(rowMinH).AlignItems(ui.Stretch).Children(func() {
			for i, day := range days {
				if i > 0 {
					ui.Box(c).Width(1).Background(df.gridLine)
				}
				dKey := day.Format("2006-01-02")
				dayEvs := eventsByDay[dKey]
				var singleDayEvs []calEvent
				for _, ev := range dayEvs {
					if !multiSet[ev.rec.ID] {
						singleDayEvs = append(singleDayEvs, ev)
					}
				}
				renderMonthDayCell(c, st, view, db, dateProp, day, singleDayEvs, dayEvs, today, cursor, spanAreaH, len(lanes), readOnlyDate, openMode, opts, pal, df, loc)
			}
		})

		// Layer 2: Multi-day continuous spanning bars (exact pixel coordinates over day columns).
		for laneIdx, lane := range lanes {
			y := headerH + 2 + float32(laneIdx)*(spanRowH+spanGap)
			for _, seg := range lane {
				x := float32(seg.colStart)*(dayW+1) + 3
				w := float32(seg.colEnd-seg.colStart+1)*(dayW+1) - 7
				if w < 18 {
					w = 18
				}
				anchorKey := fmt.Sprintf("mspan:%s:%s", weekKey, seg.ev.rec.ID)
				renderSpanningEventBar(c, st, db, dateProp, seg.ev, anchorKey, x, y, w, spanRowH, dayW, seg.contLeft, seg.contRight, readOnlyDate, openMode, opts, pal, df, loc)
			}
		}
	})
}

func renderMonthDayCell(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	dateProp database.Property,
	day time.Time,
	singleEvents []calEvent,
	allDayEvents []calEvent,
	today, cursor time.Time,
	spanAreaH float32,
	spanLanes int,
	readOnlyDate bool,
	openMode string,
	opts Options,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
) {
	dayKey := day.Format("2006-01-02")
	isToday := day.Year() == today.Year() && day.Month() == today.Month() && day.Day() == today.Day()
	inMonth := day.Year() == cursor.Year() && day.Month() == cursor.Month()

	bg := pal.Paper
	if !inMonth {
		bg = df.cellOutBg
	} else if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
		bg = df.weekendBg
	}

	cell := ui.Column(c.Key("cday:"+dayKey)).Width(0).Grow(1).MinWidth(0).
		Padding(4, 5, 5, 5).Gap(2).Background(bg)

	hovered := cell.Hovered()

	if !readOnlyDate {
		if d, ok := ui.Drop[calEventDrag](cell); ok {
			applyCalendarDayDrop(st, dateProp, d, day, loc, opts)
		}
		if _, over := ui.DragOver[calEventDrag](cell); over {
			cell.Background(pal.AccentSoft)
		}
	}

	cell.Children(func() {
		// Day header row: hover "+" quick create (left) + Day number / Today circle (right).
		ui.Row(c.Key("cdh:" + dayKey)).Height(22).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			if !readOnlyDate {
				addBtn := ui.Box(c.Key("cd-add:"+dayKey)).Size(18, 18).Radius(4).Center().
					Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("在此日新建") + ": " + dayKey).
					Tooltip(i18n.T("新建"))
				if addBtn.Hovered() {
					addBtn.Background(pal.RowHover)
				}
				if hovered || addBtn.Hovered() {
					addBtn.Children(func() { ui.Icon(c, iconPlus).FontSize(11).TextColor(pal.TextMuted) })
				}
				if addBtn.Clicked() {
					createRecordWithInitial(st, db, map[string]database.Value{
						dateProp.ID: database.DateRangeValue(dayKey, "", false),
					}, opts)
				}
			} else {
				ui.Box(c).Size(18, 18)
			}

			numStr := fmt.Sprintf("%d", day.Day())
			if day.Day() == 1 && !isToday {
				numStr = fmt.Sprintf("%d/%d", int(day.Month()), day.Day())
			}
			if isToday {
				ui.Box(c).MinWidth(22).Height(22).Padding(0, 5).Radius(11).Background(df.todayBg).Center().Children(func() {
					ui.Text(c, numStr).FontSize(11.5).FontWeight(600).TextColor(df.todayFg).SingleLine()
				})
			} else {
				ink := pal.Text
				if !inMonth {
					ink = pal.TextMuted.Alpha(0.55)
				}
				ui.Text(c, numStr).FontSize(12).FontWeight(500).TextColor(ink).SingleLine()
			}
		})

		// Leave clean vertical room for multi-day continuous spanning bars overlaid in this week row.
		if spanAreaH > 0 {
			ui.Box(c).Height(spanAreaH)
		}

		// Sort single-day events chronologically so timed morning events appear first.
		sortedSingle := append([]calEvent(nil), singleEvents...)
		sort.SliceStable(sortedSingle, func(i, j int) bool {
			if sortedSingle[i].hasTime != sortedSingle[j].hasTime {
				return sortedSingle[i].hasTime
			}
			if !sortedSingle[i].startTime.Equal(sortedSingle[j].startTime) {
				return sortedSingle[i].startTime.Before(sortedSingle[j].startTime)
			}
			return sortedSingle[i].rec.Title < sortedSingle[j].rec.Title
		})

		const maxSingle = 3
		limit := len(sortedSingle)
		overflow := 0
		if len(sortedSingle) > maxSingle {
			limit = 2
			overflow = len(sortedSingle) - limit
		}

		for _, ev := range sortedSingle[:limit] {
			anchorKey := "month:" + dayKey + ":" + ev.rec.ID
			renderCalendarEventChip(c, st, view, db, dateProp, ev, day, anchorKey, true, readOnlyDate, openMode, opts, pal, df, loc)
		}

		if overflow > 0 {
			moreBtn := ui.Row(c.Key("cd-more:"+dayKey)).Height(19).Padding(0, 5).Radius(4).
				AlignItems(ui.Center).Cursor(ui.CursorPointer).Role(ui.RoleButton).
				Label(fmt.Sprintf(i18n.T("还有 %d 项"), overflow))
			if moreBtn.Hovered() {
				moreBtn.Background(pal.RowHover)
			}
			moreBtn.Children(func() {
				ui.Text(c, fmt.Sprintf(i18n.T("还有 %d 项"), overflow)).FontSize(11).FontWeight(500).TextColor(pal.TextMuted).SingleLine()
			})
			if moreBtn.Clicked() {
				st.CalendarOverflowDay = dayKey
				st.CalendarOverflowOpen = true
			}
			if st.CalendarOverflowOpen && st.CalendarOverflowDay == dayKey {
				styledPopover(c, "cal-ov-pop:"+dayKey, moreBtn, &st.CalendarOverflowOpen, pal, func() {
					ui.Column(c.Key("cal-ov-box")).Width(252).Gap(5).Padding(8).Children(func() {
						ui.Text(c, dayKey).FontSize(12.5).FontWeight(600).TextColor(pal.Text)
						ui.Scroll(c).MaxHeight(240).Children(func() {
							ui.Column(c).Gap(4).Children(func() {
								for _, ev := range allDayEvents {
									ak := "ov:" + dayKey + ":" + ev.rec.ID
									renderCalendarEventChip(c, st, view, db, dateProp, ev, day, ak, false, readOnlyDate, openMode, opts, pal, df, loc)
								}
							})
						})
					})
				})
			}
		}
	})

	if !readOnlyDate && cell.DoubleClicked() {
		createRecordWithInitial(st, db, map[string]database.Value{
			dateProp.ID: database.DateRangeValue(dayKey, "", false),
		}, opts)
	}
}

func renderSpanningEventBar(
	c *ui.Context,
	st *State,
	db *database.Database,
	dateProp database.Property,
	ev calEvent,
	anchorKey string,
	x, y, w, h, dayW float32,
	contLeft, contRight bool,
	readOnlyDate bool,
	openMode string,
	opts Options,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
) {
	evCol := dayflowEventColors(pal.Dark, ev.color)
	isSelected := st.CalendarDetailOpen && st.CalendarDetailRecordID == ev.rec.ID

	bg := evCol.bg
	fg := evCol.text
	if isSelected {
		bg = evCol.selectedBg
		fg = evCol.selectedText
	}

	leftR := float32(4)
	rightR := float32(4)
	if contLeft {
		leftR = 0
	}
	if contRight {
		rightR = 0
	}

	bar := ui.Row(c.Key("cspan:"+anchorKey)).Absolute().Left(x).Top(y).Width(w).Height(h).
		Radius(leftR, rightR, rightR, leftR).Background(bg).AlignItems(ui.Center).Clip().
		Cursor(ui.CursorPointer).Role(ui.RoleListItem).Label(ev.rec.Title)
	if !contLeft {
		bar.BorderWidth(0, 0, 0, 3).BorderColor(evCol.line)
	}
	if bar.Hovered() && !isSelected {
		bar.Background(bg.Mix(pal.Text, 0.06))
	}
	attachRecordContextMenu(bar, st, ev.rec, opts)

	tp := db.TitleProperty()
	editingTitle := tp != nil && st.Editing && st.EditRecordID == ev.rec.ID && st.EditPropID == tp.ID && !opts.ReadOnly

	var dragMoved bool
	bar.Children(func() {
		if editingTitle {
			ui.Box(c).Padding(0, 4).Grow(1).MinWidth(0).Children(func() {
				renderCalendarEventTitleInput(c, "cal-sp-tin:"+anchorKey, st, tp, pal, opts, h-2)
			})
			return
		}

		// Left resize grip (only when start day sits in this week segment).
		if !readOnlyDate && !contLeft {
			lGrip := ui.Box(c.Key("cspanL:" + anchorKey)).Width(6).Height(h).Shrink(0).
				Cursor(ui.CursorResizeEW).Role(ui.RoleButton).Label(i18n.T("调整开始日期") + ": " + ev.rec.Title)
			if trackCalendarSpanResize(st, lGrip, ev, calDragSpanLeft, dayW, dateProp, loc, opts) {
				dragMoved = true
			}
		}

		body := ui.Row(c.Key("cspanB:"+anchorKey)).Grow(1).MinWidth(0).Height(h).
			Padding(0, 5).Gap(4).AlignItems(ui.Center)
		if !readOnlyDate {
			body.Drag(calEventDrag{
				recordID:    ev.rec.ID,
				origStart:   ev.origVal.Start,
				origEnd:     ev.origVal.End,
				includeTime: ev.origVal.IncludeTime,
				spanDays:    daysBetween(ev.startDay, ev.endDay),
			})
			if st.Motion.trackDrag(c, body, dragKindEvent, ev.rec.ID, ev.rec.Title, "", evCol.line, true) {
				bar.Opacity(0.32)
			}
		}
		body.Children(func() {
			if contLeft {
				ui.Text(c, "…").FontSize(10.5).TextColor(fg.Alpha(0.75)).NoWrap()
			}
			if ev.rec.Icon != "" {
				ui.Text(c, ev.rec.Icon).FontSize(11).NoWrap()
			}
			title := strings.TrimSpace(ev.rec.Title)
			if title == "" {
				title = i18n.T("未命名")
			}
			ui.Text(c, title).FontSize(11.5).FontWeight(500).TextColor(fg).SingleLine().Grow(1).MinWidth(0)
			if contRight {
				ui.Text(c, "→").FontSize(10).TextColor(fg.Alpha(0.75)).NoWrap()
			}
		})

		// Right resize grip (only when end day sits in this week segment).
		if !readOnlyDate && !contRight {
			rGrip := ui.Box(c.Key("cspanR:" + anchorKey)).Width(6).Height(h).Shrink(0).
				Cursor(ui.CursorResizeEW).Role(ui.RoleButton).Label(i18n.T("调整结束日期") + ": " + ev.rec.Title)
			if trackCalendarSpanResize(st, rGrip, ev, calDragSpanRight, dayW, dateProp, loc, opts) {
				dragMoved = true
			}
		}
	})

	if !editingTitle && !dragMoved {
		if bar.DoubleClicked() {
			st.CalendarDetailOpen = false
			if opts.OnOpenRecord != nil {
				opts.OnOpenRecord(ev.rec, openMode)
			}
		} else if bar.Clicked() {
			openCalendarDetailPopover(st, ev, anchorKey, loc)
		}
	}
	maybeRenderCalendarDetailPopover(c, bar, st, db, dateProp, ev, anchorKey, openMode, opts, pal, df, loc)
}

func renderCalendarEventTitleInput(
	c *ui.Context,
	key string,
	st *State,
	tp *database.Property,
	pal Palette,
	opts Options,
	height float32,
) {
	in := ui.TextInput(c.Key(key), &st.EditText).
		Grow(1).MinWidth(0).Height(height).Padding(0, 4).Radius(3).
		Background(pal.Paper).Border(1, pal.Accent).TextColor(pal.Text).
		AutoFocus().Label(tp.Name)
	if st.EditFocusPending {
		in.Focus()
		st.EditFocusPending = false
	}
	switch {
	case in.Shortcut(0, ui.KeyEscape):
		st.CancelEdit()
	case in.Submitted():
		st.CommitTextEdit(opts)
	case st.EditWasFocused && !in.Focused():
		st.CommitTextEdit(opts)
	}
	st.EditWasFocused = in.Focused()
}

func renderCalendarEventChip(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	dateProp database.Property,
	ev calEvent,
	day time.Time,
	anchorKey string,
	compact bool,
	readOnlyDate bool,
	openMode string,
	opts Options,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
) {
	rec := ev.rec
	evCol := dayflowEventColors(pal.Dark, ev.color)
	isSelected := st.CalendarDetailOpen && st.CalendarDetailRecordID == rec.ID
	bg := evCol.bg
	fg := evCol.text
	if isSelected {
		bg = evCol.selectedBg
		fg = evCol.selectedText
	}

	chip := ui.Row(c.Key("cev:"+anchorKey)).
		Padding(2, 5).Radius(4).Gap(4).AlignItems(ui.Center).
		Background(bg).BorderWidth(0, 0, 0, 3).BorderColor(evCol.line).
		Cursor(ui.CursorPointer).Focusable().Role(ui.RoleListItem).Label(rec.Title)
	if !compact {
		chip.Padding(4, 7).Radius(6)
	}
	if chip.Hovered() && !isSelected {
		chip.Background(bg.Mix(pal.Text, 0.06))
	}

	if !readOnlyDate {
		chip.Drag(calEventDrag{
			recordID:    rec.ID,
			origStart:   ev.origVal.Start,
			origEnd:     ev.origVal.End,
			includeTime: ev.origVal.IncludeTime,
			spanDays:    daysBetween(ev.startDay, ev.endDay),
		})
		if st.Motion.trackDrag(c, chip, dragKindEvent, rec.ID, rec.Title, "", evCol.line, true) {
			chip.Opacity(0.32)
		}
	}
	chip.Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
	attachRecordContextMenu(chip, st, rec, opts)

	tp := db.TitleProperty()
	editingTitle := tp != nil && st.Editing && st.EditRecordID == rec.ID && st.EditPropID == tp.ID && !opts.ReadOnly

	chip.Children(func() {
		if editingTitle {
			renderCalendarEventTitleInput(c, "cal-tin:"+anchorKey, st, tp, pal, opts, 20)
			return
		}
		if compact {
			if ev.startDay.Before(day) {
				ui.Text(c, "←").FontSize(10).TextColor(fg.Alpha(0.75)).NoWrap()
			}
			if rec.Icon != "" {
				ui.Text(c, rec.Icon).FontSize(11).NoWrap()
			}
			title := strings.TrimSpace(rec.Title)
			if title == "" {
				title = i18n.T("未命名")
			}
			ui.Text(c, title).FontSize(11.5).FontWeight(500).TextColor(fg).SingleLine().Grow(1).MinWidth(0)
			if shortTime := formatEventShortTime(ev); shortTime != "" {
				ui.Text(c, shortTime).FontSize(10.5).TextColor(fg.Alpha(0.75)).NoWrap()
			} else if ev.endDay.After(day) {
				ui.Text(c, "→").FontSize(10).TextColor(fg.Alpha(0.75)).NoWrap()
			}
			return
		}

		// Overflow popover / compact card: title + time / status row.
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).Gap(4).AlignItems(ui.Center).MinWidth(0).Children(func() {
				if rec.Icon != "" {
					ui.Text(c, rec.Icon).FontSize(11.5).NoWrap()
				}
				title := strings.TrimSpace(rec.Title)
				if title == "" {
					title = i18n.T("未命名")
				}
				ui.Text(c, title).FontSize(12).FontWeight(500).TextColor(fg).SingleLine().Grow(1).MinWidth(0)
				ui.Text(c, formatEventTimeRange(ev)).FontSize(10.5).TextColor(fg.Alpha(0.75)).NoWrap()
			})
		})
	})

	if !editingTitle {
		if chip.DoubleClicked() {
			st.CalendarDetailOpen = false
			if opts.OnOpenRecord != nil {
				opts.OnOpenRecord(rec, openMode)
			}
		} else if chip.Clicked() {
			openCalendarDetailPopover(st, ev, anchorKey, loc)
		}
	}
	maybeRenderCalendarDetailPopover(c, chip, st, db, dateProp, ev, anchorKey, openMode, opts, pal, df, loc)
	_ = view
}

func renderCalendarUnscheduledDrawer(
	c *ui.Context,
	st *State,
	db *database.Database,
	dateProp database.Property,
	catProp *database.Property,
	unscheduled []*database.Record,
	today time.Time,
	readOnlyDate bool,
	openMode string,
	opts Options,
	pal Palette,
	loc *time.Location,
) {
	drawer := ui.Column(c.Key("cal-unsched-drawer")).Width(242).Shrink(0).
		Background(pal.HeaderBg).BorderWidth(0, 0, 0, 1).BorderColor(pal.Border)

	if !readOnlyDate {
		if d, ok := ui.Drop[calEventDrag](drawer); ok && d.origStart != "" {
			st.setCellAndNotify(d.recordID, dateProp.ID, database.EmptyValue(), opts)
		}
		if d, over := ui.DragOver[calEventDrag](drawer); over && d.origStart != "" {
			drawer.Background(pal.AccentSoft)
		}
	}

	_ = loc
	drawer.Children(func() {
		ui.Row(c).Height(38).Padding(0, 12).AlignItems(ui.Center).Justify(ui.SpaceBetween).
			BorderWidth(0, 0, 1, 0).BorderColor(pal.Border).Children(func() {
			ui.Text(c, fmt.Sprintf("%s (%d)", i18n.T("未排期"), len(unscheduled))).
				FontSize(12.5).FontWeight(600).TextColor(pal.Text).SingleLine()
			closeBtn := ui.Box(c.Key("cal-unsched-close")).Size(22, 22).Radius(4).Center().
				Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("关闭未排期列表"))
			if closeBtn.Hovered() {
				closeBtn.Background(pal.RowHover)
			}
			closeBtn.Children(func() { ui.Icon(c, iconX).FontSize(12).TextColor(pal.TextMuted) })
			if closeBtn.Clicked() {
				st.CalendarUnscheduledOpen = false
			}
		})

		if len(unscheduled) == 0 {
			ui.Column(c).Grow(1).Center().Padding(20).Children(func() {
				ui.Text(c, i18n.T("所有记录均已排期")).FontSize(12.5).TextColor(pal.TextMuted).TextAlign(ui.Center)
			})
			return
		}

		todayKey := today.Format("2006-01-02")
		ui.Scroll(c.Key("cal-unsched-scroll")).TrackScroll(&st.CalendarUnschedScroll).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).Padding(8, 10).Gap(6).Children(func() {
				for _, r := range unscheduled {
					rec := r
					card := ui.Row(c.Key("cun:"+rec.ID)).Padding(7, 9).Radius(7).Gap(6).
						AlignItems(ui.Center).Background(pal.Paper).Border(1, pal.Border).
						Cursor(ui.CursorPointer).Role(ui.RoleListItem).Label(rec.Title)
					hovered := card.Hovered()
					if hovered {
						card.Border(1, pal.BorderStrong)
					}
					if !readOnlyDate {
						card.Drag(calEventDrag{recordID: rec.ID})
					}
					attachRecordContextMenu(card, st, rec, opts)

					var consumed bool
					card.Children(func() {
						accent := recordAccentColorForProp(catProp, rec)
						if accent != "" {
							ui.Box(c).Size(6, 6).Radius(3).Background(StatusDotColor(pal.Dark, accent)).Shrink(0)
						}
						title := strings.TrimSpace(rec.Title)
						if title == "" {
							title = i18n.T("未命名")
						}
						ui.Text(c, title).FontSize(12.5).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)

						if !readOnlyDate {
							schedBtn := ui.ButtonBase(c.Key("cun-today:"+rec.ID)).Height(20).Padding(0, 6).Radius(4).
								AlignItems(ui.Center).Cursor(ui.CursorPointer).Role(ui.RoleButton).
								Label(i18n.T("排至今天") + ": " + rec.Title).Tooltip(i18n.T("排至今天"))
							if hovered || schedBtn.Hovered() {
								schedBtn.Background(pal.Surface)
								schedBtn.Children(func() {
									ui.Text(c, i18n.T("今天")).FontSize(11).TextColor(pal.Accent).SingleLine()
								})
							}
							if schedBtn.Clicked() {
								consumed = true
								st.setCellAndNotify(rec.ID, dateProp.ID, database.DateRangeValue(todayKey, "", false), opts)
							}
						}
					})
					if card.Clicked() && !consumed && opts.OnOpenRecord != nil {
						opts.OnOpenRecord(rec, openMode)
					}
				}
			})
		})
	})
}
