package dbview

import (
	"fmt"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
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

type dfEventColor struct {
	bg           ui.Color
	selectedBg   ui.Color
	line         ui.Color
	text         ui.Color
	selectedText ui.Color
}

var dayflowEventPaletteMap = map[string]struct {
	light dfEventColor
	dark  dfEventColor
}{
	"blue": {
		light: dfEventColor{
			bg: ui.Hex("#EFF6FF"), selectedBg: ui.Hex("#3B82F6"),
			line: ui.Hex("#3B82F6"), text: ui.Hex("#1E3A8A"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(30, 64, 175, 0.48), selectedBg: ui.Hex("#2563EB"),
			line: ui.Hex("#60A5FA"), text: ui.Hex("#DBEAFE"), selectedText: ui.RGB(255, 255, 255),
		},
	},
	"green": {
		light: dfEventColor{
			bg: ui.Hex("#F0FDF4"), selectedBg: ui.Hex("#10B981"),
			line: ui.Hex("#10B981"), text: ui.Hex("#064E3B"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(4, 120, 87, 0.48), selectedBg: ui.Hex("#059669"),
			line: ui.Hex("#34D399"), text: ui.Hex("#D1FAE5"), selectedText: ui.RGB(255, 255, 255),
		},
	},
	"purple": {
		light: dfEventColor{
			bg: ui.Hex("#FAF5FF"), selectedBg: ui.Hex("#8B5CF6"),
			line: ui.Hex("#8B5CF6"), text: ui.Hex("#5B21B6"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(109, 40, 217, 0.48), selectedBg: ui.Hex("#7C3AED"),
			line: ui.Hex("#A78BFA"), text: ui.Hex("#EDE9FE"), selectedText: ui.RGB(255, 255, 255),
		},
	},
	"yellow": {
		light: dfEventColor{
			bg: ui.Hex("#FEFCE8"), selectedBg: ui.Hex("#F59E0B"),
			line: ui.Hex("#F59E0B"), text: ui.Hex("#78350F"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(180, 83, 9, 0.48), selectedBg: ui.Hex("#D97706"),
			line: ui.Hex("#FBBF24"), text: ui.Hex("#FEF3C7"), selectedText: ui.RGB(255, 255, 255),
		},
	},
	"red": {
		light: dfEventColor{
			bg: ui.Hex("#FEF2F2"), selectedBg: ui.Hex("#EF4444"),
			line: ui.Hex("#EF4444"), text: ui.Hex("#7F1D1D"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(185, 28, 28, 0.48), selectedBg: ui.Hex("#DC2626"),
			line: ui.Hex("#F87171"), text: ui.Hex("#FEE2E2"), selectedText: ui.RGB(255, 255, 255),
		},
	},
	"orange": {
		light: dfEventColor{
			bg: ui.Hex("#FFF7ED"), selectedBg: ui.Hex("#F97316"),
			line: ui.Hex("#F97316"), text: ui.Hex("#7C2D12"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(194, 65, 12, 0.48), selectedBg: ui.Hex("#EA580C"),
			line: ui.Hex("#FB923C"), text: ui.Hex("#FED7AA"), selectedText: ui.RGB(255, 255, 255),
		},
	},
	"pink": {
		light: dfEventColor{
			bg: ui.Hex("#FDF2F8"), selectedBg: ui.Hex("#EC4899"),
			line: ui.Hex("#EC4899"), text: ui.Hex("#831843"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(190, 24, 93, 0.48), selectedBg: ui.Hex("#DB2777"),
			line: ui.Hex("#F472B6"), text: ui.Hex("#FCE7F3"), selectedText: ui.RGB(255, 255, 255),
		},
	},
	"brown": {
		light: dfEventColor{
			bg: ui.Hex("#FBF6F1"), selectedBg: ui.Hex("#B45309"),
			line: ui.Hex("#B45309"), text: ui.Hex("#713F12"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(146, 64, 14, 0.48), selectedBg: ui.Hex("#B45309"),
			line: ui.Hex("#D97706"), text: ui.Hex("#FDE68A"), selectedText: ui.RGB(255, 255, 255),
		},
	},
	"gray": {
		light: dfEventColor{
			bg: ui.Hex("#F3F4F6"), selectedBg: ui.Hex("#64748B"),
			line: ui.Hex("#64748B"), text: ui.Hex("#334155"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(71, 85, 105, 0.48), selectedBg: ui.Hex("#475569"),
			line: ui.Hex("#94A3B8"), text: ui.Hex("#E2E8F0"), selectedText: ui.RGB(255, 255, 255),
		},
	},
	"default": {
		light: dfEventColor{
			bg: ui.Hex("#F0FDFA"), selectedBg: ui.Hex("#14B8A6"),
			line: ui.Hex("#14B8A6"), text: ui.Hex("#134E4A"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(15, 118, 110, 0.48), selectedBg: ui.Hex("#0D9488"),
			line: ui.Hex("#2DD4BF"), text: ui.Hex("#CCFBF1"), selectedText: ui.RGB(255, 255, 255),
		},
	},
}

func dayflowEventColors(dark bool, colorName string) dfEventColor {
	key := strings.ToLower(strings.TrimSpace(colorName))
	if key == "" {
		key = "blue"
	}
	pair, ok := dayflowEventPaletteMap[key]
	if !ok {
		pair = dayflowEventPaletteMap["blue"]
	}
	if dark {
		return pair.dark
	}
	return pair.light
}

type dfCalChrome struct {
	todayBg         ui.Color
	todayFg         ui.Color
	weekendBg       ui.Color
	nowLine         ui.Color
	nowBadgeBg      ui.Color
	nowBadgeFg      ui.Color
	gridLine        ui.Color
	subGridLine     ui.Color
	segTrackBg      ui.Color
	segActiveBg     ui.Color
	headerBtnBg     ui.Color
	headerBtnBorder ui.Color
	cellOutBg       ui.Color
	cellSelectedBg  ui.Color
	sidebarBg       ui.Color
	catProp         *database.Property
}

func dfCalendarPalette(pal Palette) dfCalChrome {
	if pal.Dark {
		return dfCalChrome{
			todayBg:         ui.RGB(255, 255, 255),
			todayFg:         ui.Hex("#18181B"),
			weekendBg:       ui.RGBA(148, 163, 184, 0.05),
			nowLine:         ui.Hex("#EF4444"),
			nowBadgeBg:      ui.Hex("#EF4444"),
			nowBadgeFg:      ui.RGB(255, 255, 255),
			gridLine:        ui.RGBA(255, 255, 255, 0.09),
			subGridLine:     ui.RGBA(255, 255, 255, 0.045),
			segTrackBg:      ui.RGBA(255, 255, 255, 0.07),
			segActiveBg:     ui.RGBA(255, 255, 255, 0.14),
			headerBtnBg:     ui.RGBA(255, 255, 255, 0.05),
			headerBtnBorder: ui.RGBA(255, 255, 255, 0.14),
			cellOutBg:       pal.Paper.Mix(ui.RGB(0, 0, 0), 0.16),
			cellSelectedBg:  ui.RGBA(255, 255, 255, 0.14),
			sidebarBg:       pal.Paper.Mix(ui.RGB(0, 0, 0), 0.06),
		}
	}
	return dfCalChrome{
		todayBg:         ui.Hex("#18181B"),
		todayFg:         ui.RGB(255, 255, 255),
		weekendBg:       ui.RGBA(59, 130, 246, 0.032),
		nowLine:         ui.Hex("#18181B"),
		nowBadgeBg:      ui.Hex("#18181B"),
		nowBadgeFg:      ui.RGB(255, 255, 255),
		gridLine:        ui.RGBA(15, 23, 42, 0.08),
		subGridLine:     ui.RGBA(15, 23, 42, 0.035),
		segTrackBg:      ui.Hex("#F3F4F6"),
		segActiveBg:     ui.RGB(255, 255, 255),
		headerBtnBg:     ui.RGB(255, 255, 255),
		headerBtnBorder: ui.Hex("#E5E7EB"),
		cellOutBg:       ui.RGBA(248, 250, 252, 0.85),
		cellSelectedBg:  ui.Hex("#F1F5F9"),
		sidebarBg:       pal.Paper.Mix(ui.RGB(0, 0, 0), 0.012),
	}
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
