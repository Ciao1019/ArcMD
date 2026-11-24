package dbview

import (
	"fmt"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

type calEventDrag struct {
	recordID    string
	origStart   string
	origEnd     string
	includeTime bool
	spanDays    int
}

type calEvent struct {
	rec      *database.Record
	startDay time.Time
	endDay   time.Time
	origVal  database.DateValue
	color    string
}

func dateOnly(t time.Time) time.Time {
	loc := t.Location()
	if loc == nil {
		loc = time.UTC
	}
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

func daysBetween(a, b time.Time) int {
	da := dateOnly(a.In(time.UTC))
	db := dateOnly(b.In(time.UTC))
	// Use UTC calendar dates to avoid DST 23h/25h rounding errors.
	ua := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
	ub := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	_ = da
	_ = db
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

func recordAccentColor(db *database.Database, rec *database.Record) string {
	if db == nil || rec == nil {
		return ""
	}
	for _, p := range db.Properties {
		if p.Type == database.PropStatus || p.Type == database.PropSelect {
			v := rec.ValueFor(p)
			if !v.IsEmpty() {
				opt := findOption(p, v.Text)
				if opt.Color != "" && opt.Color != "default" {
					return opt.Color
				}
			}
		}
	}
	return ""
}

func addDefaultDateProperty(st *State, db *database.Database, view database.ViewConfig, opts Options) {
	if st == nil || st.Store == nil || db == nil || opts.ReadOnly {
		return
	}
	key := uniquePropKey(db, "date")
	p := database.Property{
		ID:   "prop_" + database.NewID()[:8],
		Key:  key,
		Name: i18n.T("日期"),
		Type: database.PropDate,
		Date: &database.DateConfig{EndKey: key + "_end"},
	}
	if err := st.Store.AddProperty(p); err != nil {
		if opts.OnNotify != nil {
			opts.OnNotify(err.Error())
		}
		return
	}
	vc := view.Clone()
	switch vc.Type {
	case database.ViewCalendar:
		ensureCalendarConfig(&vc, db).DatePropID = p.ID
	case database.ViewTimeline:
		ensureTimelineConfig(&vc, db).StartPropID = p.ID
	}
	saveViewAndNotify(st, vc, opts)
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

	ccfg := database.CalendarViewConfig{Mode: "month", ShowWeekends: true, WeekStart: 1}
	if view.Calendar != nil {
		ccfg = *view.Calendar
		if ccfg.Mode == "" {
			ccfg.Mode = "month"
		}
	}
	var dateProp *database.Property
	if ccfg.DatePropID != "" {
		if p, idx := db.PropertyByID(ccfg.DatePropID); idx >= 0 {
			dateProp = p
		}
	}
	if dateProp == nil {
		dateProp = defaultDateProp(db)
	}
	if dateProp == nil {
		renderMissingDatePrompt(c, st, view, db, opts, pal, true)
		return
	}

	// Build week grid coordinates and partition records into scheduled/unscheduled.
	weekRows, gridStart, gridEnd := buildCalendarGrid(cursor, ccfg, loc)
	dayEvents := make(map[string][]calEvent)
	var unscheduled []*database.Record

	if res != nil {
		for _, r := range res.Rows {
			if r == nil {
				continue
			}
			sDay, eDay, sVal, _, ok := parseRecordDateRange(r, *dateProp, nil, loc)
			if !ok {
				unscheduled = append(unscheduled, r)
				continue
			}
			if eDay.Before(gridStart) || sDay.After(gridEnd) {
				continue
			}
			evColor := ""
			if res.RowColors != nil {
				evColor = res.RowColors[r.ID]
			}
			if evColor == "" {
				evColor = recordAccentColor(db, r)
			}
			ev := calEvent{
				rec:      r,
				startDay: sDay,
				endDay:   eDay,
				origVal:  sVal,
				color:    evColor,
			}
			cur := sDay
			if cur.Before(gridStart) {
				cur = gridStart
			}
			last := eDay
			if last.After(gridEnd) {
				last = gridEnd
			}
			for !cur.After(last) {
				k := cur.Format("2006-01-02")
				dayEvents[k] = append(dayEvents[k], ev)
				cur = cur.AddDate(0, 0, 1)
			}
		}
	}

	readOnlyDate := opts.ReadOnly || dateProp.Type != database.PropDate
	openMode := resolveCardOpenMode(view)

	ui.Column(c.Key("cal-root")).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
		renderCalendarHeader(c, st, view, db, cursor, today, ccfg, len(unscheduled), opts, pal)

		ui.Row(c.Key("cal-body")).Grow(1).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c.Key("cal-main")).Grow(1).MinWidth(0).MinHeight(0).Padding(0, 18, 14, 18).Children(func() {
				renderCalendarWeekdayBar(c, ccfg, pal)

				if ccfg.Mode == "week" {
					if len(weekRows) > 0 {
						renderCalendarWeekRow(c, st, &view, db, *dateProp, weekRows[0], dayEvents, today, cursor, true, readOnlyDate, openMode, opts, pal, loc)
					}
				} else {
					ui.Scroll(c.Key("cal-scroll")).TrackScroll(&st.CalendarScroll).Grow(1).MinHeight(0).Children(func() {
						ui.Column(c.Key("cal-weeks")).Border(1, pal.Border).Radius(8).Clip().Children(func() {
							for wi, wDays := range weekRows {
								renderCalendarWeekRow(c, st, &view, db, *dateProp, wDays, dayEvents, today, cursor, false, readOnlyDate, openMode, opts, pal, loc)
								if wi+1 < len(weekRows) {
									ui.Box(c).Height(1).Background(pal.Border)
								}
							}
						})
					})
				}
			})

			if st.CalendarUnscheduledOpen {
				renderCalendarUnscheduledDrawer(c, st, db, *dateProp, unscheduled, today, readOnlyDate, openMode, opts, pal, loc)
			}
		})
	})
}

func renderMissingDatePrompt(
	c *ui.Context,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	opts Options,
	pal Palette,
	isCalendar bool,
) {
	msg := i18n.T("时间轴视图需要日期属性")
	icon := iconViewTimeline
	if isCalendar {
		msg = i18n.T("日历视图需要日期属性")
		icon = iconViewCalendar
	}
	ui.Column(c.Key("missing-date-prompt")).Grow(1).Center().Gap(12).Padding(32).Children(func() {
		ui.Box(c).Size(44, 44).Radius(11).Background(pal.Surface).Center().Children(func() {
			ui.Icon(c, icon).FontSize(22).TextColor(pal.Accent)
		})
		ui.Text(c, msg).FontSize(16).FontWeight(600).TextColor(pal.Text)
		ui.Text(c, i18n.T("添加一个日期属性后即可按时间安排与查看记录。")).FontSize(13).TextColor(pal.TextMuted)
		if !opts.ReadOnly {
			if ui.PrimaryButton(c.Key("add-default-date"), "+ "+i18n.T("添加日期属性")).Height(32).Padding(0, 14).Clicked() {
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

func renderCalendarHeader(
	c *ui.Context,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	cursor, today time.Time,
	cfg database.CalendarViewConfig,
	unscheduledCount int,
	opts Options,
	pal Palette,
) {
	titleText := fmt.Sprintf(i18n.T("%d 年 %d 月"), cursor.Year(), int(cursor.Month()))
	if cfg.Mode == "week" {
		ws := cfg.WeekStart
		if ws != 0 {
			ws = 1
		}
		offset := (int(cursor.Weekday()) - ws + 7) % 7
		wStart := cursor.AddDate(0, 0, -offset)
		wEnd := wStart.AddDate(0, 0, 6)
		titleText = fmt.Sprintf("%s – %s", wStart.Format("2006-01-02"), wEnd.Format("01-02"))
	}

	ui.Row(c.Key("cal-hdr")).Height(44).Padding(4, 18).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, titleText).FontSize(15).FontWeight(600).TextColor(pal.Text).SingleLine()

		// Prev / Today / Next.
		prevBtn := ui.Box(c.Key("cal-prev")).Size(26, 26).Radius(6).Center().
			Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("上一周期")).Tooltip(i18n.T("上一周期"))
		if prevBtn.Hovered() {
			prevBtn.Background(pal.RowHover)
		}
		prevBtn.Children(func() { ui.Icon(c, iconChevronLeft).FontSize(14).TextColor(pal.TextMuted) })
		if prevBtn.Clicked() {
			if cfg.Mode == "week" {
				st.CalendarCursor = cursor.AddDate(0, 0, -7)
			} else {
				st.CalendarCursor = cursor.AddDate(0, -1, 0)
			}
		}

		todayBtn := ui.Button(c.Key("cal-today"), i18n.T("今天")).Height(26).Padding(0, 10)
		if todayBtn.Clicked() {
			st.CalendarCursor = today
		}

		nextBtn := ui.Box(c.Key("cal-next")).Size(26, 26).Radius(6).Center().
			Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("下一周期")).Tooltip(i18n.T("下一周期"))
		if nextBtn.Hovered() {
			nextBtn.Background(pal.RowHover)
		}
		nextBtn.Children(func() { ui.Icon(c, iconChevronRight).FontSize(14).TextColor(pal.TextMuted) })
		if nextBtn.Clicked() {
			if cfg.Mode == "week" {
				st.CalendarCursor = cursor.AddDate(0, 0, 7)
			} else {
				st.CalendarCursor = cursor.AddDate(0, 1, 0)
			}
		}

		ui.Spacer(c)

		// Month / Week quick toggle.
		ui.Row(c.Key("cal-mode-seg")).Gap(2).Padding(2).Radius(7).Background(pal.Surface).Children(func() {
			for _, m := range []struct{ id, label string }{
				{"month", i18n.T("月")},
				{"week", i18n.T("周")},
			} {
				cand := m
				active := cfg.Mode == cand.id
				btn := ui.ButtonBase(c.Key("cm:"+cand.id)).Height(24).Padding(0, 10).Radius(5).
					AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleTab).Selected(active).Label(cand.label)
				if active {
					btn.Background(pal.Paper).Shadow(0, 1, 2, 0, pal.ShadowEdge)
				} else if btn.Hovered() {
					btn.Background(pal.RowHover)
				}
				btn.Children(func() {
					ink := pal.TextMuted
					if active {
						ink = pal.Text
					}
					ui.Text(c, cand.label).FontSize(12).FontWeight(500).TextColor(ink).SingleLine()
				})
				if btn.Clicked() && !active {
					vc := view.Clone()
					ensureCalendarConfig(&vc, db).Mode = cand.id
					saveViewAndNotify(st, vc, opts)
				}
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
	ui.Row(c.Key("cal-wkdays")).Height(28).AlignItems(ui.Center).Children(func() {
		for i := 0; i < 7; i++ {
			wd := (ws + i) % 7
			if !cfg.ShowWeekends && (wd == 0 || wd == 6) {
				continue
			}
			ui.Box(c).Width(0).Grow(1).MinWidth(0).Padding(0, 8).Children(func() {
				ui.Text(c, names[wd]).FontSize(12).FontWeight(500).TextColor(pal.TextMuted).SingleLine()
			})
		}
	})
}

func renderCalendarWeekRow(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	dateProp database.Property,
	days []time.Time,
	dayEvents map[string][]calEvent,
	today, cursor time.Time,
	isWeekMode bool,
	readOnlyDate bool,
	openMode string,
	opts Options,
	pal Palette,
	loc *time.Location,
) {
	row := ui.Row(c.Key("cweek:" + days[0].Format("2006-01-02"))).AlignItems(ui.Stretch)
	if isWeekMode {
		row.Grow(1).MinHeight(320).Border(1, pal.Border).Radius(8).Clip()
	} else {
		row.MinHeight(102)
	}

	row.Children(func() {
		for i, day := range days {
			if i > 0 {
				ui.Box(c).Width(1).Background(pal.Border)
			}
			renderCalendarDayCell(c, st, view, db, dateProp, day, dayEvents[day.Format("2006-01-02")], today, cursor, isWeekMode, readOnlyDate, openMode, opts, pal, loc)
		}
	})
}

func renderCalendarDayCell(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	dateProp database.Property,
	day time.Time,
	events []calEvent,
	today, cursor time.Time,
	isWeekMode bool,
	readOnlyDate bool,
	openMode string,
	opts Options,
	pal Palette,
	loc *time.Location,
) {
	dayKey := day.Format("2006-01-02")
	isToday := day.Year() == today.Year() && day.Month() == today.Month() && day.Day() == today.Day()
	inMonth := isWeekMode || (day.Year() == cursor.Year() && day.Month() == cursor.Month())

	bg := pal.Paper
	if !inMonth {
		bg = pal.HeaderBg
	}

	cell := ui.Column(c.Key("cday:"+dayKey)).Width(0).Grow(1).MinWidth(0).
		Padding(5, 6, 6, 6).Gap(4).Background(bg)

	hovered := cell.Hovered()

	if !readOnlyDate {
		if d, ok := ui.Drop[calEventDrag](cell); ok {
			var nextVal database.Value
			if d.origStart == "" {
				nextVal = database.DateRangeValue(dayKey, "", false)
			} else if origStartTime, _, okS := database.ParseDateString(d.origStart, loc); okS {
				delta := daysBetween(dateOnly(origStartTime), day)
				newStart := shiftDateString(d.origStart, delta, loc)
				newEnd := ""
				if d.origEnd != "" {
					newEnd = shiftDateString(d.origEnd, delta, loc)
				} else if d.spanDays > 0 {
					newEnd = day.AddDate(0, 0, d.spanDays).Format("2006-01-02")
				}
				nextVal = database.DateRangeValue(newStart, newEnd, d.includeTime)
			} else {
				nextVal = database.DateRangeValue(dayKey, "", false)
			}
			st.setCellAndNotify(d.recordID, dateProp.ID, nextVal, opts)
		}
		if _, over := ui.DragOver[calEventDrag](cell); over {
			cell.Background(pal.AccentSoft)
		}
	}

	cell.Children(func() {
		// Day number + hover add button.
		ui.Row(c.Key("cdh")).Height(22).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
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
			if day.Day() == 1 && !isToday && !isWeekMode {
				numStr = fmt.Sprintf("%d/%d", int(day.Month()), day.Day())
			}
			if isToday {
				ui.Box(c).MinWidth(22).Height(22).Padding(0, 5).Radius(11).Background(pal.Accent).Center().Children(func() {
					ui.Text(c, numStr).FontSize(11.5).FontWeight(600).TextColor(ui.RGB(255, 255, 255)).SingleLine()
				})
			} else {
				ink := pal.Text
				if !inMonth {
					ink = pal.TextMuted.Alpha(0.65)
				}
				ui.Text(c, numStr).FontSize(12).FontWeight(500).TextColor(ink).SingleLine()
			}
		})

		limit := len(events)
		overflow := 0
		if !isWeekMode && len(events) > 3 {
			limit = 2
			overflow = len(events) - limit
		}

		renderChipList := func(list []calEvent, compact bool) {
			for _, ev := range list {
				renderCalendarEventChip(c, st, view, db, dateProp, ev, day, compact, readOnlyDate, openMode, opts, pal)
			}
		}

		if isWeekMode {
			ui.Scroll(c.Key("cd-wscroll:" + dayKey)).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Gap(4).Children(func() {
					renderChipList(events, false)
				})
			})
		} else {
			renderChipList(events[:limit], true)
			if overflow > 0 {
				moreBtn := ui.Row(c.Key("cd-more:"+dayKey)).Height(20).Padding(0, 6).Radius(4).
					AlignItems(ui.Center).Cursor(ui.CursorPointer).Role(ui.RoleButton).
					Label(fmt.Sprintf(i18n.T("还有 %d 项"), overflow))
				if moreBtn.Hovered() {
					moreBtn.Background(pal.RowHover)
				}
				moreBtn.Children(func() {
					ui.Text(c, fmt.Sprintf(i18n.T("还有 %d 项"), overflow)).FontSize(11).TextColor(pal.TextMuted).SingleLine()
				})
				if moreBtn.Clicked() {
					st.CalendarOverflowDay = dayKey
					st.CalendarOverflowOpen = true
				}
				if st.CalendarOverflowOpen && st.CalendarOverflowDay == dayKey {
					ui.Popover(c.Key("cal-ov-pop:"+dayKey), moreBtn, &st.CalendarOverflowOpen, func() {
						ui.Column(c.Key("cal-ov-box")).Width(240).Gap(5).Padding(6).Children(func() {
							ui.Text(c, dayKey).FontSize(12.5).FontWeight(600).TextColor(pal.Text)
							ui.Scroll(c).MaxHeight(240).Children(func() {
								ui.Column(c).Gap(4).Children(func() {
									renderChipList(events, false)
								})
							})
						})
					})
				}
			}
		}
	})
}

func renderCalendarEventChip(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	dateProp database.Property,
	ev calEvent,
	day time.Time,
	compact bool,
	readOnlyDate bool,
	openMode string,
	opts Options,
	pal Palette,
) {
	rec := ev.rec
	accent := pal.Accent
	bg := pal.Surface
	if ev.color != "" {
		accent = StatusDotColor(pal.Dark, ev.color)
		pillBg, _ := TagColors(pal.Dark, ev.color)
		bg = pillBg
	}

	chip := ui.Row(c.Key("cev:"+day.Format("2006-01-02")+":"+rec.ID)).
		Padding(3, 6).Radius(5).Gap(4).AlignItems(ui.Center).
		Background(bg).BorderWidth(0, 0, 0, 3).BorderColor(accent).
		Cursor(ui.CursorPointer).Focusable().Role(ui.RoleListItem).Label(rec.Title)
	if chip.Hovered() {
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
	}
	attachRecordContextMenu(chip, st, rec, opts)

	tp := db.TitleProperty()
	editingTitle := tp != nil && st.Editing && st.EditRecordID == rec.ID && st.EditPropID == tp.ID && !opts.ReadOnly

	chip.Children(func() {
		if editingTitle {
			in := ui.TextInput(c.Key("cal-tin:"+rec.ID), &st.EditText).
				Grow(1).MinWidth(0).Height(20).Padding(0, 4).Radius(3).
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
			return
		}
		if compact {
			if ev.startDay.Before(day) {
				ui.Text(c, "←").FontSize(10).TextColor(pal.TextMuted).NoWrap()
			}
			if rec.Icon != "" {
				ui.Text(c, rec.Icon).FontSize(11).NoWrap()
			}
			title := strings.TrimSpace(rec.Title)
			if title == "" {
				title = i18n.T("未命名")
			}
			ui.Text(c, title).FontSize(11.5).FontWeight(500).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)
			if ev.endDay.After(day) {
				ui.Text(c, "→").FontSize(10).TextColor(pal.TextMuted).NoWrap()
			}
			return
		}

		// Week mode / overflow popover card: stack wrapped title cleanly with optional status line.
		ui.Column(c).Gap(3).Grow(1).MinWidth(0).Padding(2, 1).Children(func() {
			ui.Row(c).Gap(4).AlignItems(ui.Start).MinWidth(0).Children(func() {
				if rec.Icon != "" {
					ui.Text(c, rec.Icon).FontSize(11.5).NoWrap()
				}
				title := strings.TrimSpace(rec.Title)
				if title == "" {
					title = i18n.T("未命名")
				}
				ui.Text(c, title).FontSize(12).FontWeight(500).TextColor(pal.Text).Wrap().Grow(1).MinWidth(0)
			})
			var statusText string
			var statusColor string
			for _, p := range db.Properties {
				if p.Type == database.PropStatus {
					if sv := rec.ValueFor(p); !sv.IsEmpty() {
						statusText = sv.Text
						statusColor = findOption(p, sv.Text).Color
					}
					break
				}
			}
			hasSpan := ev.startDay.Before(day) || ev.endDay.After(day)
			if statusText != "" || hasSpan {
				ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
					if statusText != "" {
						dot := StatusDotColor(pal.Dark, statusColor)
						ui.Box(c).Size(5, 5).Radius(2.5).Background(dot).Shrink(0)
						ui.Text(c, statusText).FontSize(10.5).TextColor(pal.TextMuted).SingleLine()
					}
					if ev.startDay.Before(day) || ev.endDay.After(day) {
						ui.Spacer(c)
						cue := "→"
						if ev.startDay.Before(day) && ev.endDay.After(day) {
							cue = "← →"
						} else if ev.startDay.Before(day) {
							cue = "←"
						}
						ui.Text(c, cue).FontSize(10).TextColor(pal.TextMuted).NoWrap()
					}
				})
			}
		})
	})

	if chip.Clicked() && !editingTitle && opts.OnOpenRecord != nil {
		opts.OnOpenRecord(rec, openMode)
	}
	_ = view
}

func renderCalendarUnscheduledDrawer(
	c *ui.Context,
	st *State,
	db *database.Database,
	dateProp database.Property,
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
						accent := recordAccentColor(db, rec)
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
