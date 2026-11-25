package dbview

import (
	"fmt"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
)

const uncategorizedCatKey = "__uncategorized__"

const (
	calDragNone uint8 = iota
	calDragSpanLeft
	calDragSpanRight
	calDragTimedMove
	calDragTimedTop
	calDragTimedBottom
)

type calEventDrag struct {
	recordID    string
	origStart   string
	origEnd     string
	includeTime bool
	spanDays    int
}

type calEvent struct {
	rec       *database.Record
	startDay  time.Time
	endDay    time.Time
	startTime time.Time
	endTime   time.Time
	hasTime   bool
	isAllDay  bool
	origVal   database.DateValue
	category  string
	color     string
}

type calCategoryItem struct {
	key   string
	label string
	color string
	count int
}

func recordCalendarCategory(prop *database.Property, rec *database.Record) (catKey, color string) {
	if prop == nil || rec == nil {
		return uncategorizedCatKey, ""
	}
	v := rec.ValueFor(*prop)
	txt := strings.TrimSpace(v.Text)
	if txt == "" {
		return uncategorizedCatKey, ""
	}
	opt := findOption(*prop, txt)
	return txt, opt.Color
}

func collectCalendarCategories(prop *database.Property, recs []*database.Record) []calCategoryItem {
	counts := map[string]int{}
	for _, r := range recs {
		k, _ := recordCalendarCategory(prop, r)
		counts[k]++
	}
	var out []calCategoryItem
	if prop != nil {
		for _, opt := range prop.Options {
			out = append(out, calCategoryItem{
				key:   opt.Name,
				label: opt.Name,
				color: opt.Color,
				count: counts[opt.Name],
			})
		}
	}
	if counts[uncategorizedCatKey] > 0 || len(out) == 0 {
		out = append(out, calCategoryItem{
			key:   uncategorizedCatKey,
			label: i18n.T("未分类"),
			color: "gray",
			count: counts[uncategorizedCatKey],
		})
	}
	return out
}

func parseRecordCalendarEvent(
	res *database.QueryResult,
	rec *database.Record,
	dateProp database.Property,
	catProp *database.Property,
	loc *time.Location,
) (calEvent, bool) {
	if rec == nil {
		return calEvent{}, false
	}
	if loc == nil {
		loc = time.UTC
	}
	sv := rec.ValueFor(dateProp)
	if sv.IsEmpty() || sv.Kind != database.ValDate {
		return calEvent{}, false
	}
	stTime, sHasTime, sok := database.ParseDateString(sv.Date.Start, loc)
	if !sok {
		return calEvent{}, false
	}
	sDay := dateOnly(stTime)
	eDay := sDay
	endTime := stTime.Add(time.Hour)
	hasTime := sHasTime || sv.Date.IncludeTime

	if sv.Date.End != "" {
		if etTime, eHasTime, eok := database.ParseDateString(sv.Date.End, loc); eok {
			ed := dateOnly(etTime)
			if !ed.Before(sDay) {
				eDay = ed
			}
			if eHasTime {
				hasTime = true
			}
			if !etTime.Before(stTime) {
				endTime = etTime
			}
		}
	}
	if hasTime && !sv.Date.IncludeTime && sv.Date.Start != "" && !strings.ContainsAny(sv.Date.Start, "T ") {
		hasTime = false
	}
	if hasTime && !endTime.After(stTime) {
		endTime = stTime.Add(30 * time.Minute)
	}

	catKey, colName := recordCalendarCategory(catProp, rec)
	if res != nil && res.RowColors != nil && res.RowColors[rec.ID] != "" {
		colName = res.RowColors[rec.ID]
	}
	if colName == "" {
		colName = "blue"
	}

	isMultiDay := eDay.After(sDay)
	return calEvent{
		rec:       rec,
		startDay:  sDay,
		endDay:    eDay,
		startTime: stTime,
		endTime:   endTime,
		hasTime:   hasTime,
		isAllDay:  !hasTime || isMultiDay,
		origVal:   sv.Date,
		category:  catKey,
		color:     colName,
	}, true
}

func formatEventShortTime(ev calEvent) string {
	if !ev.hasTime || ev.isAllDay {
		return ""
	}
	return ev.startTime.Format("15:04")
}

func formatEventTimeRange(ev calEvent) string {
	if ev.hasTime && !ev.isAllDay {
		return fmt.Sprintf("%s – %s", ev.startTime.Format("15:04"), ev.endTime.Format("15:04"))
	}
	if ev.endDay.After(ev.startDay) {
		return fmt.Sprintf("%s → %s", ev.startDay.Format("01/02"), ev.endDay.Format("01/02"))
	}
	return i18n.T("全天")
}

func formatDateTimeToken(t time.Time, includeTime bool) string {
	if includeTime {
		return t.Format("2006-01-02T15:04")
	}
	return t.Format("2006-01-02")
}

func snapMinutes15(mins int) int {
	if mins < 0 {
		return ((mins - 7) / 15) * 15
	}
	return ((mins + 7) / 15) * 15
}

func startOfWeekFor(day time.Time, weekStart int) time.Time {
	ws := 1
	if weekStart == 0 {
		ws = 0
	}
	offset := (int(day.Weekday()) - ws + 7) % 7
	return dateOnly(day).AddDate(0, 0, -offset)
}

func narrowWeekdayNames() [7]string {
	if i18n.English() {
		return [7]string{"S", "M", "T", "W", "T", "F", "S"}
	}
	return [7]string{"日", "一", "二", "三", "四", "五", "六"}
}

func shortMonthLabel(m time.Month) string {
	if i18n.English() {
		s := m.String()
		if len(s) >= 3 {
			return s[:3]
		}
		return s
	}
	return fmt.Sprintf("%d月", int(m))
}
