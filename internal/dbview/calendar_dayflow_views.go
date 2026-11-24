package dbview

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

const dfHourHeight float32 = 54
const dfTimeGutterW float32 = 56

type spanSegment struct {
	ev        calEvent
	colStart  int
	colEnd    int
	contLeft  bool
	contRight bool
}

func calSpanPreviewRange(st *State, ev calEvent, dayW float32) (sDay, eDay time.Time, resizing bool) {
	sDay, eDay = ev.startDay, ev.endDay
	if st == nil || !st.CalendarDragActive || st.CalendarDragRecordID != ev.rec.ID {
		return sDay, eDay, false
	}
	if st.CalendarDragMode != calDragSpanLeft && st.CalendarDragMode != calDragSpanRight {
		return sDay, eDay, false
	}
	delta := 0
	if dayW > 0 {
		delta = int(math.Round(float64(st.CalendarDragDX / dayW)))
	}
	ns, ne := applySpanDragDays(sDay, eDay, st.CalendarDragMode, delta)
	return ns, ne, true
}

func applySpanDragDays(sDay, eDay time.Time, mode uint8, deltaDays int) (time.Time, time.Time) {
	switch mode {
	case calDragSpanLeft:
		ns := sDay.AddDate(0, 0, deltaDays)
		if ns.After(eDay) {
			ns = eDay
		}
		return ns, eDay
	case calDragSpanRight:
		ne := eDay.AddDate(0, 0, deltaDays)
		if ne.Before(sDay) {
			ne = sDay
		}
		return sDay, ne
	default:
		return sDay, eDay
	}
}

func packSpanSegments(st *State, events []calEvent, days []time.Time, dayW float32) [][]spanSegment {
	if len(days) == 0 || len(events) == 0 {
		return nil
	}
	firstDay := dateOnly(days[0])
	lastDay := dateOnly(days[len(days)-1])

	var segs []spanSegment
	for _, ev := range events {
		sDay, eDay, _ := calSpanPreviewRange(st, ev, dayW)
		if eDay.Before(firstDay) || sDay.After(lastDay) {
			continue
		}
		cStart := -1
		cEnd := -1
		for idx, d := range days {
			dd := dateOnly(d)
			if !dd.Before(sDay) && !dd.After(eDay) {
				if cStart < 0 {
					cStart = idx
				}
				cEnd = idx
			}
		}
		if cStart < 0 || cEnd < cStart {
			continue
		}
		segs = append(segs, spanSegment{
			ev:        ev,
			colStart:  cStart,
			colEnd:    cEnd,
			contLeft:  sDay.Before(dateOnly(days[cStart])),
			contRight: eDay.After(dateOnly(days[cEnd])),
		})
	}

	sort.SliceStable(segs, func(i, j int) bool {
		if segs[i].colStart != segs[j].colStart {
			return segs[i].colStart < segs[j].colStart
		}
		li := segs[i].colEnd - segs[i].colStart
		lj := segs[j].colEnd - segs[j].colStart
		if li != lj {
			return li > lj
		}
		return segs[i].ev.rec.Title < segs[j].ev.rec.Title
	})

	var lanes [][]spanSegment
	var laneEnds []int
	for _, seg := range segs {
		placed := false
		for li := range lanes {
			if seg.colStart > laneEnds[li] {
				lanes[li] = append(lanes[li], seg)
				laneEnds[li] = seg.colEnd
				placed = true
				break
			}
		}
		if !placed {
			lanes = append(lanes, []spanSegment{seg})
			laneEnds = append(laneEnds, seg.colEnd)
		}
	}
	return lanes
}

func trackCalendarSpanResize(
	st *State,
	el ui.Element,
	ev calEvent,
	mode uint8,
	dayW float32,
	dateProp database.Property,
	loc *time.Location,
	opts Options,
) (justMoved bool) {
	dx, _, held := el.Dragged()
	if held {
		if !st.CalendarDragActive || st.CalendarDragRecordID != ev.rec.ID || st.CalendarDragMode != mode {
			st.CalendarDragActive = true
			st.CalendarDragRecordID = ev.rec.ID
			st.CalendarDragMode = mode
			st.CalendarDragDX = 0
			st.CalendarDragDY = 0
			st.CalendarDragMoved = false
			st.CalendarDragOrigStart = ev.startDay
			st.CalendarDragOrigEnd = ev.endDay
			st.CalendarDragOrigVal = ev.origVal
		}
		if dx != 0 {
			st.CalendarDragDX += dx
			if math.Abs(float64(st.CalendarDragDX)) > 2 {
				st.CalendarDragMoved = true
			}
		}
		return false
	}

	if el.PressCanceled() {
		if st.CalendarDragActive && st.CalendarDragRecordID == ev.rec.ID && st.CalendarDragMode == mode {
			st.CalendarDragActive = false
			st.CalendarDragRecordID = ""
			st.CalendarDragMode = calDragNone
			st.CalendarDragDX = 0
			st.CalendarDragMoved = false
		}
		return false
	}

	if st.CalendarDragActive && st.CalendarDragRecordID == ev.rec.ID && st.CalendarDragMode == mode {
		wasMoved := st.CalendarDragMoved
		deltaDays := 0
		if dayW > 0 {
			deltaDays = int(math.Round(float64(st.CalendarDragDX / dayW)))
		}
		origStart := st.CalendarDragOrigStart
		origEnd := st.CalendarDragOrigEnd
		origVal := st.CalendarDragOrigVal

		st.CalendarDragActive = false
		st.CalendarDragRecordID = ""
		st.CalendarDragMode = calDragNone
		st.CalendarDragDX = 0
		st.CalendarDragMoved = false

		if deltaDays != 0 {
			nsDay, neDay := applySpanDragDays(origStart, origEnd, mode, deltaDays)
			dStart := daysBetween(origStart, nsDay)
			dEnd := daysBetween(origEnd, neDay)
			if dStart != 0 || dEnd != 0 {
				nextStart := origVal.Start
				if dStart != 0 {
					nextStart = shiftDateString(origVal.Start, dStart, loc)
				}
				nextEnd := origVal.End
				if dEnd != 0 {
					if origVal.End != "" {
						nextEnd = shiftDateString(origVal.End, dEnd, loc)
					} else {
						nextEnd = neDay.Format("2006-01-02")
					}
				}
				st.setCellAndNotify(ev.rec.ID, dateProp.ID, database.DateRangeValue(nextStart, nextEnd, origVal.IncludeTime), opts)
			}
		}
		return wasMoved
	}
	return false
}

func applyCalendarDayDrop(
	st *State,
	dateProp database.Property,
	d calEventDrag,
	targetDay time.Time,
	loc *time.Location,
	opts Options,
) {
	dayKey := targetDay.Format("2006-01-02")
	var nextVal database.Value
	if d.origStart == "" {
		nextVal = database.DateRangeValue(dayKey, "", false)
	} else if origStartTime, _, okS := database.ParseDateString(d.origStart, loc); okS {
		delta := daysBetween(dateOnly(origStartTime), targetDay)
		newStart := shiftDateString(d.origStart, delta, loc)
		newEnd := ""
		if d.origEnd != "" {
			newEnd = shiftDateString(d.origEnd, delta, loc)
		} else if d.spanDays > 0 {
			newEnd = targetDay.AddDate(0, 0, d.spanDays).Format("2006-01-02")
		}
		nextVal = database.DateRangeValue(newStart, newEnd, d.includeTime)
	} else {
		nextVal = database.DateRangeValue(dayKey, "", false)
	}
	st.setCellAndNotify(d.recordID, dateProp.ID, nextVal, opts)
}

type timedSlotLayout struct {
	ev       calEvent
	startMin int
	endMin   int
	colIdx   int
	colCount int
}

func layoutOverlappingTimedEvents(evs []calEvent) []timedSlotLayout {
	if len(evs) == 0 {
		return nil
	}
	items := make([]timedSlotLayout, 0, len(evs))
	for _, ev := range evs {
		sm := ev.startTime.Hour()*60 + ev.startTime.Minute()
		em := ev.endTime.Hour()*60 + ev.endTime.Minute()
		if !ev.endDay.Equal(ev.startDay) {
			em = 24 * 60
		}
		if em <= sm {
			em = min(24*60, sm+30)
		}
		items = append(items, timedSlotLayout{ev: ev, startMin: sm, endMin: em})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].startMin != items[j].startMin {
			return items[i].startMin < items[j].startMin
		}
		if items[i].endMin != items[j].endMin {
			return items[i].endMin > items[j].endMin
		}
		return items[i].ev.rec.Title < items[j].ev.rec.Title
	})

	// Cluster overlapping events and assign non-overlapping columns per cluster.
	for i := 0; i < len(items); {
		clusterEnd := items[i].endMin
		j := i + 1
		for j < len(items) && items[j].startMin < clusterEnd {
			if items[j].endMin > clusterEnd {
				clusterEnd = items[j].endMin
			}
			j++
		}
		var colEnds []int
		for k := i; k < j; k++ {
			placedCol := -1
			for cIdx, ce := range colEnds {
				if ce <= items[k].startMin {
					placedCol = cIdx
					colEnds[cIdx] = items[k].endMin
					break
				}
			}
			if placedCol < 0 {
				placedCol = len(colEnds)
				colEnds = append(colEnds, items[k].endMin)
			}
			items[k].colIdx = placedCol
		}
		totalCols := max(1, len(colEnds))
		for k := i; k < j; k++ {
			items[k].colCount = totalCols
		}
		i = j
	}
	return items
}

func trackTimedEventDrag(
	st *State,
	el ui.Element,
	ev calEvent,
	mode uint8,
	colW float32,
	numDays int,
	dateProp database.Property,
	loc *time.Location,
	opts Options,
) (justMoved bool) {
	dx, dy, held := el.Dragged()
	if held {
		if !st.CalendarDragActive || st.CalendarDragRecordID != ev.rec.ID || st.CalendarDragMode != mode {
			st.CalendarDragActive = true
			st.CalendarDragRecordID = ev.rec.ID
			st.CalendarDragMode = mode
			st.CalendarDragDX = 0
			st.CalendarDragDY = 0
			st.CalendarDragMoved = false
			st.CalendarDragOrigStart = ev.startTime
			st.CalendarDragOrigEnd = ev.endTime
			st.CalendarDragOrigVal = ev.origVal
		}
		if dx != 0 || dy != 0 {
			st.CalendarDragDX += dx
			st.CalendarDragDY += dy
			if math.Abs(float64(st.CalendarDragDX)) > 2 || math.Abs(float64(st.CalendarDragDY)) > 2 {
				st.CalendarDragMoved = true
			}
		}
		return false
	}

	if el.PressCanceled() {
		if st.CalendarDragActive && st.CalendarDragRecordID == ev.rec.ID && st.CalendarDragMode == mode {
			st.CalendarDragActive = false
			st.CalendarDragRecordID = ""
			st.CalendarDragMode = calDragNone
			st.CalendarDragDX = 0
			st.CalendarDragDY = 0
			st.CalendarDragMoved = false
		}
		return false
	}

	if st.CalendarDragActive && st.CalendarDragRecordID == ev.rec.ID && st.CalendarDragMode == mode {
		wasMoved := st.CalendarDragMoved
		origStart := st.CalendarDragOrigStart
		origEnd := st.CalendarDragOrigEnd
		dMins := snapMinutes15(int(math.Round(float64(st.CalendarDragDY / dfHourHeight * 60))))
		dDays := 0
		if mode == calDragTimedMove && numDays > 1 && colW > 0 {
			dDays = int(math.Round(float64(st.CalendarDragDX / colW)))
		}

		st.CalendarDragActive = false
		st.CalendarDragRecordID = ""
		st.CalendarDragMode = calDragNone
		st.CalendarDragDX = 0
		st.CalendarDragDY = 0
		st.CalendarDragMoved = false

		ns, ne := previewTimedRange(origStart, origEnd, mode, dDays, dMins)
		if !ns.Equal(origStart) || !ne.Equal(origEnd) {
			_ = loc
			st.setCellAndNotify(ev.rec.ID, dateProp.ID, database.DateRangeValue(formatDateTimeToken(ns, true), formatDateTimeToken(ne, true), true), opts)
		}
		return wasMoved
	}
	return false
}

func previewTimedRange(origStart, origEnd time.Time, mode uint8, dDays, dMins int) (time.Time, time.Time) {
	dur := origEnd.Sub(origStart)
	if dur < 15*time.Minute {
		dur = 15 * time.Minute
	}
	switch mode {
	case calDragTimedMove:
		ns := origStart.AddDate(0, 0, dDays).Add(time.Duration(dMins) * time.Minute)
		// Clamp inside the same target day if midnight crossed due to vertical drag.
		dayAnchor := dateOnly(origStart).AddDate(0, 0, dDays)
		minStart := dayAnchor
		maxStart := dayAnchor.Add(24*time.Hour - dur)
		if ns.Before(minStart) {
			ns = minStart
		}
		if ns.After(maxStart) {
			ns = maxStart
		}
		return ns, ns.Add(dur)
	case calDragTimedTop:
		ns := origStart.Add(time.Duration(dMins) * time.Minute)
		minStart := dateOnly(origStart)
		maxStart := origEnd.Add(-15 * time.Minute)
		if ns.Before(minStart) {
			ns = minStart
		}
		if ns.After(maxStart) {
			ns = maxStart
		}
		return ns, origEnd
	case calDragTimedBottom:
		ne := origEnd.Add(time.Duration(dMins) * time.Minute)
		minEnd := origStart.Add(15 * time.Minute)
		maxEnd := dateOnly(origStart).Add(24 * time.Hour)
		if ne.Before(minEnd) {
			ne = minEnd
		}
		if ne.After(maxEnd) {
			ne = maxEnd
		}
		return origStart, ne
	default:
		return origStart, origEnd
	}
}

func renderWeekTimeGridView(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	dateProp database.Property,
	visibleEvents []calEvent,
	now, cursor, today time.Time,
	ccfg database.CalendarViewConfig,
	readOnlyDate bool,
	openMode string,
	opts Options,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
) {
	_ = res
	weekRows, _, _ := buildCalendarGrid(cursor, database.CalendarViewConfig{
		Mode:         "week",
		ShowWeekends: ccfg.ShowWeekends,
		WeekStart:    ccfg.WeekStart,
	}, loc)
	if len(weekRows) == 0 || len(weekRows[0]) == 0 {
		return
	}
	days := weekRows[0]
	renderDayOrWeekTimeGrid(c, st, view, db, dateProp, visibleEvents, days, false, now, today, readOnlyDate, openMode, opts, pal, df, loc)
}

func renderDayTimeGridView(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	dateProp database.Property,
	visibleEvents []calEvent,
	eventsByDay map[string][]calEvent,
	now, cursor, today time.Time,
	ccfg database.CalendarViewConfig,
	readOnlyDate bool,
	openMode string,
	opts Options,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
) {
	_ = res
	days := []time.Time{cursor}
	ui.Row(c.Key("cal-day-split")).Grow(1).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		dayLeft := ui.Column(c.Key("cal-day-main")).Grow(1).MinWidth(0).MinHeight(0)
		mainW := dayLeft.LayoutBounds().W
		dayLeft.Children(func() {
			renderDayOrWeekTimeGrid(c, st, view, db, dateProp, visibleEvents, days, true, now, today, readOnlyDate, openMode, opts, pal, df, loc)
		})

		// DayView right-hand companion panel (Mini Calendar + Day Event List) per DayFlow DayView.png.
		if !opts.Embedded && (mainW == 0 || mainW >= 460) {
			renderDayRightPanel(c, st, db, dateProp, visibleEvents, eventsByDay, cursor, today, ccfg, openMode, opts, pal, df, loc)
		}
	})
}

func renderDayRightPanel(
	c *ui.Context,
	st *State,
	db *database.Database,
	dateProp database.Property,
	visibleEvents []calEvent,
	eventsByDay map[string][]calEvent,
	cursor, today time.Time,
	ccfg database.CalendarViewConfig,
	openMode string,
	opts Options,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
) {
	_ = visibleEvents
	weekdayFull := []string{
		i18n.T("星期日"), i18n.T("星期一"), i18n.T("星期二"), i18n.T("星期三"),
		i18n.T("星期四"), i18n.T("星期五"), i18n.T("星期六"),
	}
	dayKey := cursor.Format("2006-01-02")
	dayEvents := slices.Clone(eventsByDay[dayKey])
	sort.SliceStable(dayEvents, func(i, j int) bool {
		if dayEvents[i].isAllDay != dayEvents[j].isAllDay {
			return dayEvents[i].isAllDay
		}
		return dayEvents[i].startTime.Before(dayEvents[j].startTime)
	})

	ui.Column(c.Key("cal-day-right")).Width(258).Shrink(0).
		Background(df.sidebarBg).BorderWidth(0, 0, 0, 1).BorderColor(pal.Border).Children(func() {
		ui.Box(c).Padding(12, 12, 12, 12).BorderWidth(0, 0, 1, 0).BorderColor(pal.Border).Children(func() {
			renderMiniCalendar(
				c, "dayright", st.CalendarMiniMonth, cursor, today, ccfg.WeekStart,
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

		ui.Column(c.Key("cal-day-agenda")).Grow(1).MinHeight(0).Padding(12, 12).Gap(8).Children(func() {
			dayHdr := fmt.Sprintf(i18n.T("%d 月 %d 日 %s"), int(cursor.Month()), cursor.Day(), weekdayFull[int(cursor.Weekday())])
			ui.Text(c, dayHdr).FontSize(14).FontWeight(600).TextColor(pal.Text).SingleLine()

			if len(dayEvents) == 0 {
				ui.Box(c).Padding(16, 4).Children(func() {
					ui.Text(c, i18n.T("当日暂无日程")).FontSize(12.5).TextColor(pal.TextMuted)
				})
				return
			}

			ui.Scroll(c.Key("cal-day-rscroll")).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Gap(6).Children(func() {
					for _, ev := range dayEvents {
						item := ev
						evCol := dayflowEventColors(pal.Dark, item.color)
						isSelected := st.CalendarDetailOpen && st.CalendarDetailRecordID == item.rec.ID
						bg := pal.Paper
						fg := pal.Text
						subFg := pal.TextMuted
						if isSelected {
							bg = evCol.selectedBg
							fg = evCol.selectedText
							subFg = evCol.selectedText.Alpha(0.85)
						}
						ak := "dayright:" + dayKey + ":" + item.rec.ID
						card := ui.Column(c.Key("cdr:"+ak)).Padding(7, 10).Radius(7).Gap(2).
							Background(bg).Border(1, pal.Border).BorderWidth(1, 1, 1, 3.5).
							BorderColor(pal.Border).Cursor(ui.CursorPointer).Role(ui.RoleListItem).Label(item.rec.Title)
						card.Draw(func(p *ui.Painter, r ui.Rect) {
							p.Fill(ui.Rect{X: r.X, Y: r.Y + 2, W: 3, H: max(4, r.H-4)}, evCol.line, 1.5)
						})
						if card.Hovered() && !isSelected {
							card.Background(pal.RowHover)
						}
						card.Children(func() {
							title := strings.TrimSpace(item.rec.Title)
							if title == "" {
								title = i18n.T("未命名")
							}
							ui.Text(c, title).FontSize(12.5).FontWeight(500).TextColor(fg).SingleLine()
							ui.Text(c, formatEventTimeRange(item)).FontSize(11).TextColor(subFg).SingleLine()
						})
						if card.DoubleClicked() {
							st.CalendarDetailOpen = false
							if opts.OnOpenRecord != nil {
								opts.OnOpenRecord(item.rec, openMode)
							}
						} else if card.Clicked() {
							openCalendarDetailPopover(st, item, ak, loc)
						}
						maybeRenderCalendarDetailPopover(c, card, st, db, dateProp, item, ak, openMode, opts, pal, df, loc)
					}
				})
			})
		})
	})
}

func renderDayOrWeekTimeGrid(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	dateProp database.Property,
	visibleEvents []calEvent,
	days []time.Time,
	isDayMode bool,
	now, today time.Time,
	readOnlyDate bool,
	openMode string,
	opts Options,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
) {
	numCols := len(days)
	if numCols == 0 {
		return
	}
	wkNames := []string{
		i18n.T("周日"), i18n.T("周一"), i18n.T("周二"), i18n.T("周三"),
		i18n.T("周四"), i18n.T("周五"), i18n.T("周六"),
	}
	weekdayFull := []string{
		i18n.T("星期日"), i18n.T("星期一"), i18n.T("星期二"), i18n.T("星期三"),
		i18n.T("星期四"), i18n.T("星期五"), i18n.T("星期六"),
	}

	firstDay := dateOnly(days[0])
	lastDay := dateOnly(days[numCols-1])
	var allDayEvents []calEvent
	timedByDay := make([][]calEvent, numCols)
	for _, ev := range visibleEvents {
		if ev.endDay.Before(firstDay) || ev.startDay.After(lastDay) {
			continue
		}
		if ev.isAllDay {
			allDayEvents = append(allDayEvents, ev)
			continue
		}
		for ci, d := range days {
			if dateOnly(d).Equal(ev.startDay) {
				timedByDay[ci] = append(timedByDay[ci], ev)
				break
			}
		}
	}

	root := ui.Column(c.Key("cal-tg-root")).Grow(1).MinWidth(0).MinHeight(0)
	totalW := root.LayoutBounds().W
	if totalW <= 0 {
		c.AnimationFrame()
		totalW = 860
	}
	colsAreaW := max(float32(220), totalW-dfTimeGutterW)
	dayW := (colsAreaW - float32(numCols-1)) / float32(numCols)

	// Scroll to 07:30 on first visit to Week or Day mode.
	scrollKey := view.ID + ":" + map[bool]string{true: "day", false: "week"}[isDayMode]
	if st.CalendarTimeScrolledKey != scrollKey {
		st.CalendarTimeScrolledKey = scrollKey
		st.CalendarTimeScroll.Y = 7.5 * dfHourHeight
	}

	allDayLanes := packSpanSegments(st, allDayEvents, days, dayW)
	allDayRowH := max(float32(28), float32(len(allDayLanes))*23+8)

	root.Children(func() {
		// 1. Fixed Weekday / Day Subtitle Header Row.
		ui.Row(c.Key("cal-tg-hdr")).Height(32).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(df.gridLine).Children(func() {
			ui.Box(c).Width(dfTimeGutterW).Shrink(0)
			if isDayMode {
				d := days[0]
				ui.Box(c).Grow(1).Padding(0, 10).Children(func() {
					ui.Text(c, weekdayFull[int(d.Weekday())]).FontSize(12.5).FontWeight(500).TextColor(pal.TextMuted).SingleLine()
				})
			} else {
				for i, d := range days {
					isToday := d.Year() == today.Year() && d.Month() == today.Month() && d.Day() == today.Day()
					ui.Row(c).Width(0).Grow(1).Gap(5).Center().Children(func() {
						ui.Text(c, wkNames[int(d.Weekday())]).FontSize(12).FontWeight(500).TextColor(pal.TextMuted).NoWrap()
						if isToday {
							ui.Box(c).Size(22, 22).Radius(11).Background(df.todayBg).Center().Children(func() {
								ui.Text(c, fmt.Sprintf("%d", d.Day())).FontSize(11.5).FontWeight(600).TextColor(df.todayFg).NoWrap()
							})
						} else {
							ui.Text(c, fmt.Sprintf("%d", d.Day())).FontSize(12).FontWeight(500).TextColor(pal.Text).NoWrap()
						}
					})
					_ = i
				}
			}
		})

		// 2. Fixed All-Day Row with packed multi-day/all-day spanning bars.
		ui.Row(c.Key("cal-tg-allday")).Height(allDayRowH).AlignItems(ui.Stretch).
			BorderWidth(0, 0, 1, 0).BorderColor(df.gridLine).Children(func() {
			ui.Box(c).Width(dfTimeGutterW).Padding(0, 6).AlignItems(ui.End).Justify(ui.Center).Shrink(0).Children(func() {
				ui.Text(c, i18n.T("全天")).FontSize(11).TextColor(pal.TextMuted).NoWrap()
			})

			allDayArea := ui.Box(c.Key("cal-tg-adarea")).Grow(1).MinWidth(0).Height(allDayRowH)
			allDayArea.Children(func() {
				// Day drop cells in All-Day strip.
				ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
					for i, d := range days {
						if i > 0 {
							ui.Box(c).Width(1).Background(df.gridLine)
						}
						day := d
						dKey := day.Format("2006-01-02")
						bg := pal.Paper
						if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
							bg = df.weekendBg
						}
						cell := ui.Box(c.Key("cad:" + dKey)).Width(0).Grow(1).Height(allDayRowH).Background(bg)
						if !readOnlyDate {
							if dropVal, ok := ui.Drop[calEventDrag](cell); ok {
								applyCalendarDayDrop(st, dateProp, dropVal, day, loc, opts)
							}
							if _, over := ui.DragOver[calEventDrag](cell); over {
								cell.Background(pal.AccentSoft)
							}
							if cell.DoubleClicked() {
								createRecordWithInitial(st, db, map[string]database.Value{
									dateProp.ID: database.DateRangeValue(dKey, "", false),
								}, opts)
							}
						}
					}
				})

				// Overlaid continuous All-day event bars.
				for laneIdx, lane := range allDayLanes {
					y := 4 + float32(laneIdx)*23
					for _, seg := range lane {
						x := float32(seg.colStart)*(dayW+1) + 3
						w := float32(seg.colEnd-seg.colStart+1)*(dayW+1) - 7
						if w < 18 {
							w = 18
						}
						ak := "allday:" + seg.ev.rec.ID
						renderSpanningEventBar(c, st, db, dateProp, seg.ev, ak, x, y, w, 20, dayW, seg.contLeft, seg.contRight, readOnlyDate, openMode, opts, pal, df, loc)
					}
				}
			})
		})

		// 3. Scrollable 24-Hour Time Grid.
		totalGridH := 24 * dfHourHeight
		todayCol := -1
		for ci, d := range days {
			if d.Year() == today.Year() && d.Month() == today.Month() && d.Day() == today.Day() {
				todayCol = ci
				break
			}
		}
		nowMins := now.Hour()*60 + now.Minute()
		nowY := float32(nowMins) / 60 * dfHourHeight

		ui.Scroll(c.Key("cal-tg-scroll")).TrackScroll(&st.CalendarTimeScroll).Grow(1).MinHeight(0).Children(func() {
			gridRow := ui.Row(c.Key("cal-tg-canvas")).Height(totalGridH).AlignItems(ui.Stretch)
			gridRow.Children(func() {
				// Left Hour Gutter (00:00 .. 23:00 + current time badge).
				gutter := ui.Box(c.Key("cal-tg-gutter")).Width(dfTimeGutterW).Height(totalGridH).Shrink(0)
				gutter.Draw(func(p *ui.Painter, r ui.Rect) {
					for h := 1; h < 24; h++ {
						y := r.Y + float32(h)*dfHourHeight
						p.Text(r.X+10, y-7, fmt.Sprintf("%02d:00", h), 11, pal.TextMuted)
					}
				})
				if todayCol >= 0 {
					gutter.Children(func() {
						badgeY := max(0, min(totalGridH-18, nowY-9))
						ui.Box(c.Key("cal-now-badge")).Absolute().Right(6).Top(badgeY).Height(18).Padding(0, 6).
							Radius(4).Background(df.nowBadgeBg).Center().Children(func() {
							ui.Text(c, now.Format("15:04")).FontSize(10.5).FontWeight(600).TextColor(df.nowBadgeFg).NoWrap()
						})
					})
				}

				// Day Columns Area.
				dayArea := ui.Box(c.Key("cal-tg-cols")).Grow(1).MinWidth(0).Height(totalGridH)
				dayArea.Children(func() {
					// Background hour lines & double-click hourly creation cells.
					ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
						for ci, d := range days {
							if ci > 0 {
								ui.Box(c).Width(1).Background(df.gridLine)
							}
							day := d
							dKey := day.Format("2006-01-02")
							bg := pal.Paper
							if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
								bg = df.weekendBg
							}
							col := ui.Column(c.Key("tg-col:" + dKey)).Width(0).Grow(1).Height(totalGridH).Background(bg)
							col.Draw(func(p *ui.Painter, r ui.Rect) {
								for h := 0; h < 24; h++ {
									y := r.Y + float32(h)*dfHourHeight
									if h > 0 {
										p.Line(r.X, y, r.X+r.W, y, 1, df.gridLine)
									}
									halfY := y + dfHourHeight*0.5
									p.Line(r.X, halfY, r.X+r.W, halfY, 1, df.subGridLine)
								}
							})
							if !readOnlyDate {
								if dropVal, ok := ui.Drop[calEventDrag](col); ok {
									applyCalendarDayDrop(st, dateProp, dropVal, day, loc, opts)
								}
								col.Children(func() {
									for h := 0; h < 24; h++ {
										hr := h
										slot := ui.Box(c.Key(fmt.Sprintf("tg-slot:%s:%02d", dKey, hr))).Height(dfHourHeight)
										if slot.DoubleClicked() {
											sStr := fmt.Sprintf("%sT%02d:00", dKey, hr)
											endHr := hr + 1
											eStr := fmt.Sprintf("%sT%02d:00", dKey, endHr)
											if endHr >= 24 {
												eStr = dKey + "T23:59"
											}
											createRecordWithInitial(st, db, map[string]database.Value{
												dateProp.ID: database.DateRangeValue(sStr, eStr, true),
											}, opts)
										}
									}
								})
							}
						}
					})

					// Positioned overlapping timed events per day column.
					for ci, d := range days {
						dKey := d.Format("2006-01-02")
						laidOut := layoutOverlappingTimedEvents(timedByDay[ci])
						colLeft := float32(ci) * (dayW + 1)
						for _, slot := range laidOut {
							renderTimedEventBlock(c, st, db, dateProp, slot, colLeft, dayW, numCols, dKey, readOnlyDate, openMode, opts, pal, df, loc)
						}
					}

					// Live Current Time Indicator Line across columns + dot on Today column.
					if todayCol >= 0 {
						dotX := float32(todayCol)*(dayW+1) - 3
						if dotX < 0 {
							dotX = 0
						}
						line := ui.Box(c.Key("cal-now-line")).Absolute().Left(0).Right(0).Top(nowY).Height(1.5).Background(df.nowLine).PassThrough()
						_ = line
						ui.Box(c.Key("cal-now-dot")).Absolute().Left(dotX).Top(nowY-3).Size(7, 7).Radius(3.5).Background(df.nowLine).PassThrough()
					}
				})
			})
		})
	})
}

func renderTimedEventBlock(
	c *ui.Context,
	st *State,
	db *database.Database,
	dateProp database.Property,
	slot timedSlotLayout,
	colLeft, dayW float32,
	numDays int,
	dayKey string,
	readOnlyDate bool,
	openMode string,
	opts Options,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
) {
	ev := slot.ev
	draggingMove := st.CalendarDragActive && st.CalendarDragRecordID == ev.rec.ID && st.CalendarDragMode == calDragTimedMove
	draggingTop := st.CalendarDragActive && st.CalendarDragRecordID == ev.rec.ID && st.CalendarDragMode == calDragTimedTop
	draggingBot := st.CalendarDragActive && st.CalendarDragRecordID == ev.rec.ID && st.CalendarDragMode == calDragTimedBottom

	dispStart, dispEnd := ev.startTime, ev.endTime
	previewLeft := colLeft
	if draggingMove || draggingTop || draggingBot {
		dMins := snapMinutes15(int(math.Round(float64(st.CalendarDragDY / dfHourHeight * 60))))
		dDays := 0
		if draggingMove && numDays > 1 && dayW > 0 {
			dDays = int(math.Round(float64(st.CalendarDragDX / colWFloat(dayW))))
			maxLeft := float32(numDays-1) * (dayW + 1)
			previewLeft = min(max(0, colLeft+float32(dDays)*(dayW+1)), maxLeft)
		}
		dispStart, dispEnd = previewTimedRange(st.CalendarDragOrigStart, st.CalendarDragOrigEnd, st.CalendarDragMode, dDays, dMins)
	}

	sMin := dispStart.Hour()*60 + dispStart.Minute()
	eMin := dispEnd.Hour()*60 + dispEnd.Minute()
	if !dateOnly(dispEnd).Equal(dateOnly(dispStart)) {
		eMin = 24 * 60
	}
	if eMin <= sMin {
		eMin = min(24*60, sMin+30)
	}

	topPx := float32(sMin) / 60 * dfHourHeight
	hPx := max(float32(22), float32(eMin-sMin)/60*dfHourHeight-2)
	subW := (dayW - 6) / float32(slot.colCount)
	x := previewLeft + 3 + float32(slot.colIdx)*subW
	w := max(float32(28), subW-2)

	evCol := dayflowEventColors(pal.Dark, ev.color)
	isSelected := st.CalendarDetailOpen && st.CalendarDetailRecordID == ev.rec.ID
	bg := evCol.bg
	fg := evCol.text
	if isSelected {
		bg = evCol.selectedBg
		fg = evCol.selectedText
	}

	anchorKey := "timed:" + dayKey + ":" + ev.rec.ID
	blk := ui.Column(c.Key("ctimed:"+anchorKey)).Absolute().Left(x).Top(topPx).Width(w).Height(hPx).
		Radius(6).Background(bg).BorderWidth(0, 0, 0, 3).BorderColor(evCol.line).Clip().
		Cursor(ui.CursorPointer).Role(ui.RoleListItem).Label(ev.rec.Title)
	if draggingMove || draggingTop || draggingBot {
		blk.Shadow(0, 4, 12, 0, pal.ShadowEdge).Border(1, evCol.line)
	} else if blk.Hovered() && !isSelected {
		blk.Background(bg.Mix(pal.Text, 0.05))
	}
	attachRecordContextMenu(blk, st, ev.rec, opts)

	tp := db.TitleProperty()
	editingTitle := tp != nil && st.Editing && st.EditRecordID == ev.rec.ID && st.EditPropID == tp.ID && !opts.ReadOnly

	var moved bool
	blk.Children(func() {
		if editingTitle {
			ui.Box(c).Padding(2, 4).Grow(1).MinWidth(0).Children(func() {
				renderCalendarEventTitleInput(c, "cal-tm-tin:"+anchorKey, st, tp, pal, opts, 20)
			})
			return
		}

		// Top resize handle.
		if !readOnlyDate {
			tGrip := ui.Box(c.Key("ctmT:" + anchorKey)).Height(5).Cursor(ui.CursorResizeNS).
				Role(ui.RoleButton).Label(i18n.T("调整开始时间") + ": " + ev.rec.Title)
			if trackTimedEventDrag(st, tGrip, ev, calDragTimedTop, dayW, numDays, dateProp, loc, opts) {
				moved = true
			}
		}

		// Center draggable body.
		body := ui.Column(c.Key("ctmB:"+anchorKey)).Grow(1).MinHeight(0).Padding(2, 6).Gap(1)
		if !readOnlyDate {
			body.Cursor(ui.CursorGrab)
			if trackTimedEventDrag(st, body, ev, calDragTimedMove, dayW, numDays, dateProp, loc, opts) {
				moved = true
			}
		}
		body.Children(func() {
			title := strings.TrimSpace(ev.rec.Title)
			if title == "" {
				title = i18n.T("未命名")
			}
			titleEl := ui.Text(c, title).FontSize(11.5).FontWeight(600).TextColor(fg)
			if hPx >= 42 {
				titleEl.Wrap()
			} else {
				titleEl.SingleLine()
			}
			if hPx >= 36 {
				timeTxt := fmt.Sprintf("%s – %s", dispStart.Format("15:04"), dispEnd.Format("15:04"))
				ui.Text(c, timeTxt).FontSize(10.5).TextColor(fg.Alpha(0.82)).SingleLine()
			}
		})

		// Bottom resize handle.
		if !readOnlyDate {
			bGrip := ui.Box(c.Key("ctmBot:" + anchorKey)).Height(5).Cursor(ui.CursorResizeNS).
				Role(ui.RoleButton).Label(i18n.T("调整结束时间") + ": " + ev.rec.Title)
			if trackTimedEventDrag(st, bGrip, ev, calDragTimedBottom, dayW, numDays, dateProp, loc, opts) {
				moved = true
			}
		}
	})

	if !editingTitle && !moved {
		if blk.DoubleClicked() {
			st.CalendarDetailOpen = false
			if opts.OnOpenRecord != nil {
				opts.OnOpenRecord(ev.rec, openMode)
			}
		} else if blk.Clicked() {
			openCalendarDetailPopover(st, ev, anchorKey, loc)
		}
	}
	maybeRenderCalendarDetailPopover(c, blk, st, db, dateProp, ev, anchorKey, openMode, opts, pal, df, loc)
}

func colWFloat(w float32) float32 {
	if w <= 0 {
		return 100
	}
	return w
}

// Year View (Fixed-Week & Year-Canvas modes modeled after DayFlow YearView.tsx).
func renderYearView(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	dateProp database.Property,
	visibleEvents []calEvent,
	cursor, today time.Time,
	ccfg database.CalendarViewConfig,
	readOnlyDate bool,
	openMode string,
	opts Options,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
) {
	yMode := ccfg.YearMode
	if yMode == "" {
		yMode = "fixed-week"
	}
	if yMode == "year-canvas" {
		renderYearCanvasView(c, st, view, db, dateProp, visibleEvents, cursor, today, readOnlyDate, openMode, opts, pal, df, loc)
		return
	}
	renderYearFixedWeekView(c, st, view, db, dateProp, visibleEvents, cursor, today, ccfg, readOnlyDate, openMode, opts, pal, df, loc)
}

func renderYearFixedWeekView(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	dateProp database.Property,
	visibleEvents []calEvent,
	cursor, today time.Time,
	ccfg database.CalendarViewConfig,
	readOnlyDate bool,
	openMode string,
	opts Options,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
) {
	_ = readOnlyDate
	year := cursor.Year()
	ws := 1
	if ccfg.WeekStart == 0 {
		ws = 0
	}
	const numCols = 37
	const monthLabelW float32 = 42

	yrRoot := ui.Column(c.Key("cal-yr-fixed")).Grow(1).MinWidth(0).MinHeight(0)
	areaW := yrRoot.LayoutBounds().W
	if areaW <= 0 {
		c.AnimationFrame()
		areaW = 920
	}
	gridW := max(float32(370), areaW-monthLabelW)
	colW := gridW / float32(numCols)
	narrowNames := narrowWeekdayNames()

	yrRoot.Children(func() {
		// 1. Fixed 37-column Weekday Header Strip at the top.
		ui.Row(c.Key("cal-yr-wkhdr")).Height(24).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(df.gridLine).Children(func() {
			ui.Box(c).Width(monthLabelW).Shrink(0)
			for col := 0; col < numCols; col++ {
				wd := (ws + col) % 7
				isWknd := wd == 0 || wd == 6
				lbl := narrowNames[wd]
				cell := ui.Box(c).Width(0).Grow(1).Height(24)
				if isWknd {
					cell.Background(df.weekendBg)
				}
				cell.Draw(func(p *ui.Painter, r ui.Rect) {
					x := r.X + r.W*0.5 - float32(len([]rune(lbl)))*3.8
					p.Text(x, r.Y+6, lbl, 10.5, pal.TextMuted)
				})
			}
		})

		// 2. Scrollable 12-Month rows below header.
		ui.Scroll(c.Key("cal-yr-fscroll")).TrackScroll(&st.CalendarYearScroll).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c.Key("cal-yr-rows")).Children(func() {
				for m := time.January; m <= time.December; m++ {
					first := time.Date(year, m, 1, 0, 0, 0, 0, loc)
					nextM := first.AddDate(0, 1, 0)
					daysInM := int(nextM.Sub(first) / (24 * time.Hour))
					offset := (int(first.Weekday()) - ws + 7) % 7

					// Pack all events intersecting this month across the 37-column span.
					type yrSeg struct {
						ev     calEvent
						c0, c1 int
						cLeft  bool
						cRight bool
					}
					var segs []yrSeg
					lastM := first.AddDate(0, 0, daysInM-1)
					for _, ev := range visibleEvents {
						if ev.endDay.Before(first) || ev.startDay.After(lastM) {
							continue
						}
						sDay := ev.startDay
						eDay := ev.endDay
						if sDay.Before(first) {
							sDay = first
						}
						if eDay.After(lastM) {
							eDay = lastM
						}
						c0 := offset + sDay.Day() - 1
						c1 := offset + eDay.Day() - 1
						segs = append(segs, yrSeg{
							ev:     ev,
							c0:     c0,
							c1:     c1,
							cLeft:  ev.startDay.Before(first),
							cRight: ev.endDay.After(lastM),
						})
					}
					var lanes [][]yrSeg
					var laneEnds []int
					for _, seg := range segs {
						placed := false
						for li := range lanes {
							if seg.c0 > laneEnds[li] {
								lanes[li] = append(lanes[li], seg)
								laneEnds[li] = seg.c1
								placed = true
								break
							}
						}
						if !placed {
							lanes = append(lanes, []yrSeg{seg})
							laneEnds = append(laneEnds, seg.c1)
						}
					}

					rowH := max(float32(54), 22+float32(len(lanes))*18+4)
					row := ui.Row(c.Key(fmt.Sprintf("yrm:%d:%d", year, int(m)))).Height(rowH).
						AlignItems(ui.Stretch).BorderWidth(0, 0, 1, 0).BorderColor(df.gridLine)
					row.Children(func() {
						ui.Box(c).Width(monthLabelW).Padding(6, 4).AlignItems(ui.Center).Shrink(0).
							BorderWidth(0, 1, 0, 0).BorderColor(df.gridLine).Children(func() {
							ui.Text(c, shortMonthLabel(m)).FontSize(11.5).FontWeight(600).TextColor(pal.TextMuted).NoWrap()
						})

						mGrid := ui.Box(c.Key(fmt.Sprintf("yrmg:%d:%d", year, int(m)))).Grow(1).MinWidth(0).Height(rowH)
						mGrid.Children(func() {
							ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
								for col := 0; col < numCols; col++ {
									wd := (ws + col) % 7
									isWknd := wd == 0 || wd == 6
									dayNum := col - offset + 1
									valid := dayNum >= 1 && dayNum <= daysInM
									cell := ui.Box(c).Width(0).Grow(1).Height(rowH).Padding(3, 2, 0, 0).
										BorderWidth(0, 1, 0, 0).BorderColor(df.subGridLine)
									if isWknd {
										cell.Background(df.weekendBg)
									}
									if valid {
										d := time.Date(year, m, dayNum, 0, 0, 0, 0, loc)
										isToday := d.Year() == today.Year() && d.Month() == today.Month() && d.Day() == today.Day()
										cell.Cursor(ui.CursorPointer)
										cell.Children(func() {
											ui.Row(c).Justify(ui.End).Padding(0, 2).Children(func() {
												if isToday {
													ui.Box(c).Size(17, 17).Radius(8.5).Background(df.todayBg).Center().Children(func() {
														ui.Text(c, fmt.Sprintf("%d", dayNum)).FontSize(10).FontWeight(600).TextColor(df.todayFg).NoWrap()
													})
												} else {
													ui.Text(c, fmt.Sprintf("%d", dayNum)).FontSize(10.5).TextColor(pal.TextMuted).NoWrap()
												}
											})
										})
										if cell.Clicked() {
											st.CalendarCursor = d
											st.CalendarMiniMonth = first
										}
									}
								}
							})

							// Event bars overlaid inside month row.
							for laneIdx, lane := range lanes {
								y := 21 + float32(laneIdx)*18
								for _, seg := range lane {
									x := float32(seg.c0)*colW + 1
									w := max(colW-2, float32(seg.c1-seg.c0+1)*colW-2)
									ak := fmt.Sprintf("yfw:%d:%d:%s", year, int(m), seg.ev.rec.ID)
									renderYearEventPill(c, st, view, db, dateProp, seg.ev, ak, x, y, w, 16, seg.cLeft, seg.cRight, openMode, opts, pal, df, loc)
								}
							}
						})
					})
				}
			})
		})
	})
}

func renderYearCanvasView(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	dateProp database.Property,
	visibleEvents []calEvent,
	cursor, today time.Time,
	readOnlyDate bool,
	openMode string,
	opts Options,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
) {
	_ = readOnlyDate
	year := cursor.Year()
	jan1 := time.Date(year, time.January, 1, 0, 0, 0, 0, loc)
	dec31 := time.Date(year, time.December, 31, 0, 0, 0, 0, loc)
	const colsPerRow = 18

	var allDays []time.Time
	for d := jan1; !d.After(dec31); d = d.AddDate(0, 0, 1) {
		allDays = append(allDays, d)
	}

	scroll := ui.Scroll(c.Key("cal-yr-cscroll")).TrackScroll(&st.CalendarYearScroll).Grow(1).MinWidth(0).MinHeight(0)
	areaW := scroll.LayoutBounds().W
	if areaW <= 0 {
		c.AnimationFrame()
		areaW = 920
	}
	colW := areaW / float32(colsPerRow)

	scroll.Children(func() {
		ui.Column(c.Key("cal-yr-canvas")).Children(func() {
			for rowStart := 0; rowStart < len(allDays); rowStart += colsPerRow {
				rowEnd := min(len(allDays), rowStart+colsPerRow)
				rowDays := allDays[rowStart:rowEnd]
				lanes := packSpanSegments(st, visibleEvents, rowDays, colW)
				// Also include single-day events inside packSpanSegments? packSpanSegments works for both single and multi-day!
				rowH := max(float32(58), 24+float32(len(lanes))*18+6)

				rowBox := ui.Box(c.Key(fmt.Sprintf("yrcr:%d:%d", year, rowStart))).Height(rowH).
					BorderWidth(0, 0, 1, 0).BorderColor(df.gridLine)
				rowBox.Children(func() {
					ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
						for ci := 0; ci < colsPerRow; ci++ {
							if ci >= len(rowDays) {
								ui.Box(c).Width(0).Grow(1).Height(rowH).Background(df.cellOutBg)
								continue
							}
							d := rowDays[ci]
							isToday := d.Year() == today.Year() && d.Month() == today.Month() && d.Day() == today.Day()
							isMonthStart := d.Day() == 1
							cell := ui.Box(c.Key("yrcd:"+d.Format("2006-01-02"))).Width(0).Grow(1).Height(rowH).
								Padding(4, 4, 0, 4).BorderWidth(0, 1, 0, 0).BorderColor(df.subGridLine).
								Cursor(ui.CursorPointer)
							if isMonthStart {
								cell.Draw(func(p *ui.Painter, r ui.Rect) {
									p.Line(r.X, r.Y, r.X, r.Y+r.H, 1.5, df.todayBg)
								})
							}
							cell.Children(func() {
								ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
									if isMonthStart {
										ui.Box(c).Padding(1, 5).Radius(4).Background(df.todayBg).Children(func() {
											ui.Text(c, shortMonthLabel(d.Month())).FontSize(10).FontWeight(700).TextColor(df.todayFg).NoWrap()
										})
									} else {
										ui.Box(c)
									}
									if isToday {
										ui.Box(c).Size(18, 18).Radius(9).Background(df.todayBg).Center().Children(func() {
											ui.Text(c, fmt.Sprintf("%d", d.Day())).FontSize(10).FontWeight(600).TextColor(df.todayFg).NoWrap()
										})
									} else {
										ui.Text(c, fmt.Sprintf("%d", d.Day())).FontSize(11).TextColor(pal.TextMuted).NoWrap()
									}
								})
							})
							if cell.Clicked() {
								st.CalendarCursor = d
								st.CalendarMiniMonth = time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, loc)
							}
						}
					})

					for laneIdx, lane := range lanes {
						y := 24 + float32(laneIdx)*18
						for _, seg := range lane {
							x := float32(seg.colStart)*colW + 2
							w := max(float32(14), float32(seg.colEnd-seg.colStart+1)*colW-4)
							ak := fmt.Sprintf("ycnv:%d:%s", rowStart, seg.ev.rec.ID)
							renderYearEventPill(c, st, view, db, dateProp, seg.ev, ak, x, y, w, 16, seg.contLeft, seg.contRight, openMode, opts, pal, df, loc)
						}
					}
				})
			}
		})
	})
}

func renderYearEventPill(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	dateProp database.Property,
	ev calEvent,
	anchorKey string,
	x, y, w, h float32,
	contLeft, contRight bool,
	openMode string,
	opts Options,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
) {
	_ = view
	evCol := dayflowEventColors(pal.Dark, ev.color)
	isSelected := st.CalendarDetailOpen && st.CalendarDetailRecordID == ev.rec.ID
	bg, fg := evCol.bg, evCol.text
	if isSelected {
		bg, fg = evCol.selectedBg, evCol.selectedText
	}
	lr, rr := float32(4), float32(4)
	if contLeft {
		lr = 0
	}
	if contRight {
		rr = 0
	}
	pill := ui.Row(c.Key("ypill:"+anchorKey)).Absolute().Left(x).Top(y).Width(w).Height(h).
		Padding(0, 3).Radius(lr, rr, rr, lr).Background(bg).AlignItems(ui.Center).Gap(2).Clip().
		Cursor(ui.CursorPointer).Role(ui.RoleListItem).Label(ev.rec.Title)
	if !contLeft {
		pill.BorderWidth(0, 0, 0, 2).BorderColor(evCol.line)
	}
	pill.Children(func() {
		title := strings.TrimSpace(ev.rec.Title)
		if title == "" {
			title = i18n.T("未命名")
		}
		ui.Text(c, title).FontSize(10).FontWeight(500).TextColor(fg).SingleLine()
	})
	if pill.DoubleClicked() {
		st.CalendarDetailOpen = false
		if opts.OnOpenRecord != nil {
			opts.OnOpenRecord(ev.rec, openMode)
		}
	} else if pill.Clicked() {
		openCalendarDetailPopover(st, ev, anchorKey, loc)
	}
	maybeRenderCalendarDetailPopover(c, pill, st, db, dateProp, ev, anchorKey, openMode, opts, pal, df, loc)
}

// Agenda View modeled after DayFlow AgendaView.tsx.
func renderAgendaView(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	dateProp database.Property,
	visibleEvents []calEvent,
	cursor, today time.Time,
	ccfg database.CalendarViewConfig,
	readOnlyDate bool,
	openMode string,
	opts Options,
	pal Palette,
	df dfCalChrome,
	loc *time.Location,
) {
	_ = view
	_ = ccfg
	_ = readOnlyDate
	weekdayFull := []string{
		i18n.T("星期日"), i18n.T("星期一"), i18n.T("星期二"), i18n.T("星期三"),
		i18n.T("星期四"), i18n.T("星期五"), i18n.T("星期六"),
	}

	firstOfMonth := time.Date(cursor.Year(), cursor.Month(), 1, 0, 0, 0, 0, loc)
	lastOfMonth := firstOfMonth.AddDate(0, 1, -1)

	type agendaDay struct {
		day    time.Time
		allDay []calEvent
		timed  []calEvent
	}
	var daysList []agendaDay
	for d := firstOfMonth; !d.After(lastOfMonth); d = d.AddDate(0, 0, 1) {
		var ad, tm []calEvent
		for _, ev := range visibleEvents {
			if d.Before(ev.startDay) || d.After(ev.endDay) {
				continue
			}
			if ev.isAllDay {
				ad = append(ad, ev)
			} else {
				tm = append(tm, ev)
			}
		}
		if len(ad) > 0 || len(tm) > 0 {
			sort.SliceStable(tm, func(i, j int) bool {
				return tm[i].startTime.Before(tm[j].startTime)
			})
			daysList = append(daysList, agendaDay{day: d, allDay: ad, timed: tm})
		}
	}

	ui.Scroll(c.Key("cal-agenda-scroll")).TrackScroll(&st.CalendarAgendaScroll).Grow(1).MinWidth(0).MinHeight(0).
		Padding(14, 22).Children(func() {
		if len(daysList) == 0 {
			ui.Column(c).Grow(1).Center().Padding(48).Gap(8).Children(func() {
				ui.Icon(c, iconViewCalendar).FontSize(28).TextColor(pal.TextMuted.Alpha(0.45))
				ui.Text(c, i18n.T("本月暂无日程")).FontSize(14).FontWeight(500).TextColor(pal.TextMuted)
			})
			return
		}

		ui.Column(c.Key("cal-agenda-list")).MaxWidth(820).Gap(14).Children(func() {
			for _, ag := range daysList {
				d := ag.day
				dKey := d.Format("2006-01-02")
				isToday := d.Year() == today.Year() && d.Month() == today.Month() && d.Day() == today.Day()

				ui.Column(c.Key("agday:" + dKey)).Gap(6).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(0, 2, 4, 2).
						BorderWidth(0, 0, 1, 0).BorderColor(df.gridLine).Children(func() {
						label := fmt.Sprintf(i18n.T("%d 月 %d 日 · %s"), int(d.Month()), d.Day(), weekdayFull[int(d.Weekday())])
						ui.Text(c, label).FontSize(13.5).FontWeight(600).TextColor(pal.Text)
						if isToday {
							ui.Box(c).Padding(1, 7).Radius(9).Background(df.todayBg).Children(func() {
								ui.Text(c, i18n.T("今天")).FontSize(10.5).FontWeight(600).TextColor(df.todayFg).NoWrap()
							})
						}
					})

					if len(ag.allDay) > 0 {
						ui.Row(c).Wrap().Gap(6).Children(func() {
							for _, ev := range ag.allDay {
								item := ev
								evCol := dayflowEventColors(pal.Dark, item.color)
								ak := "agad:" + dKey + ":" + item.rec.ID
								chip := ui.Row(c.Key("agchip:"+ak)).Padding(4, 9).Radius(6).Gap(6).AlignItems(ui.Center).
									Background(evCol.bg).BorderWidth(0, 0, 0, 3).BorderColor(evCol.line).
									Cursor(ui.CursorPointer).Role(ui.RoleListItem).Label(item.rec.Title)
								chip.Children(func() {
									title := strings.TrimSpace(item.rec.Title)
									if title == "" {
										title = i18n.T("未命名")
									}
									ui.Text(c, title).FontSize(12.5).FontWeight(500).TextColor(evCol.text).SingleLine()
									if item.endDay.After(item.startDay) {
										ui.Text(c, fmt.Sprintf("%s → %s", item.startDay.Format("01/02"), item.endDay.Format("01/02"))).
											FontSize(11).TextColor(evCol.text.Alpha(0.75)).NoWrap()
									}
								})
								if chip.DoubleClicked() {
									st.CalendarDetailOpen = false
									if opts.OnOpenRecord != nil {
										opts.OnOpenRecord(item.rec, openMode)
									}
								} else if chip.Clicked() {
									openCalendarDetailPopover(st, item, ak, loc)
								}
								maybeRenderCalendarDetailPopover(c, chip, st, db, dateProp, item, ak, openMode, opts, pal, df, loc)
							}
						})
					}

					for _, ev := range ag.timed {
						item := ev
						evCol := dayflowEventColors(pal.Dark, item.color)
						ak := "agtm:" + dKey + ":" + item.rec.ID
						row := ui.Row(c.Key("agrow:"+ak)).Padding(7, 10).Radius(7).Gap(10).AlignItems(ui.Center).
							Background(pal.Paper).Border(1, pal.Border).Cursor(ui.CursorPointer).
							Role(ui.RoleListItem).Label(item.rec.Title)
						if row.Hovered() {
							row.Background(pal.RowHover)
						}
						attachRecordContextMenu(row, st, item.rec, opts)
						row.Children(func() {
							ui.Box(c).Width(3).Height(18).Radius(1.5).Background(evCol.line).Shrink(0)
							ui.Text(c, formatEventTimeRange(item)).FontSize(12).FontWeight(500).TextColor(pal.TextMuted).Width(94).NoWrap()
							title := strings.TrimSpace(item.rec.Title)
							if title == "" {
								title = i18n.T("未命名")
							}
							ui.Text(c, title).FontSize(13).FontWeight(500).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)
							if item.category != uncategorizedCatKey && item.category != "" {
								ui.Box(c).Padding(2, 7).Radius(5).Background(evCol.bg).Shrink(0).Children(func() {
									ui.Text(c, item.category).FontSize(11).FontWeight(500).TextColor(evCol.text).NoWrap()
								})
							}
						})
						if row.DoubleClicked() {
							st.CalendarDetailOpen = false
							if opts.OnOpenRecord != nil {
								opts.OnOpenRecord(item.rec, openMode)
							}
						} else if row.Clicked() {
							openCalendarDetailPopover(st, item, ak, loc)
						}
						maybeRenderCalendarDetailPopover(c, row, st, db, dateProp, item, ak, openMode, opts, pal, df, loc)
					}
				})
			}
		})
	})
}
