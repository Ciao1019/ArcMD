package dbview

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

const (
	tlDragNone uint8 = iota
	tlDragMove
	tlDragLeft
	tlDragRight
)

const (
	tlHeaderH = float32(48)
	tlRowH    = float32(40)
)

type timelineScaleSpec struct {
	id         string
	label      string
	dayW       float32
	spanBefore int
	spanAfter  int
}

func timelineZoomLevels() []timelineScaleSpec {
	return []timelineScaleSpec{
		{id: "hour", label: i18n.T("小时"), dayW: 144, spanBefore: 4, spanAfter: 10},
		{id: "day", label: i18n.T("天"), dayW: 56, spanBefore: 14, spanAfter: 42},
		{id: "week", label: i18n.T("周"), dayW: 32, spanBefore: 28, spanAfter: 84},
		{id: "biweek", label: i18n.T("双周"), dayW: 22, spanBefore: 42, spanAfter: 126},
		{id: "month", label: i18n.T("月"), dayW: 16, spanBefore: 45, spanAfter: 135},
		{id: "quarter", label: i18n.T("季度"), dayW: 7, spanBefore: 90, spanAfter: 270},
		{id: "year", label: i18n.T("年"), dayW: 3, spanBefore: 180, spanAfter: 540},
	}
}

func timelineScaleFor(zoom string) timelineScaleSpec {
	levels := timelineZoomLevels()
	for _, s := range levels {
		if s.id == zoom {
			return s
		}
	}
	return levels[4] // month
}

func timelineZoomLabel(zoom string) string {
	return timelineScaleFor(zoom).label
}

func timelineView(
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

	tcfg := database.TimelineViewConfig{Zoom: "month", ShowTable: true}
	if view.Timeline != nil {
		tcfg = *view.Timeline
		if tcfg.Zoom == "" {
			tcfg.Zoom = "month"
		}
	}
	startProp, isBuiltinDate, _ := resolveDateBinding(db, tcfg.StartPropID)

	var endProp *database.Property
	if !isBuiltinDate && tcfg.EndPropID != "" && tcfg.EndPropID != startProp.ID {
		if p, idx := db.PropertyByID(tcfg.EndPropID); idx >= 0 {
			endProp = p
		}
	}

	spec := timelineScaleFor(tcfg.Zoom)
	if st.TimelineTableWidth <= 0 {
		st.TimelineTableWidth = 250
	}
	leftW := float32(0)
	if tcfg.ShowTable {
		leftW = min(max(st.TimelineTableWidth, 180), 420)
	}

	// Determine canvas date bounds encompassing today + scheduled items.
	originDay := today.AddDate(0, 0, -spec.spanBefore)
	maxDay := today.AddDate(0, 0, spec.spanAfter)
	if res != nil {
		for _, r := range res.Rows {
			sDay, eDay, _, _, ok := parseRecordDateRange(r, startProp, endProp, loc)
			if !ok {
				continue
			}
			lo := sDay.AddDate(0, 0, -14)
			hi := eDay.AddDate(0, 0, 14)
			if lo.Before(originDay) && daysBetween(lo, today) <= 900 {
				originDay = lo
			}
			if hi.After(maxDay) && daysBetween(today, hi) <= 900 {
				maxDay = hi
			}
		}
	}
	totalDays := max(14, daysBetween(originDay, maxDay)+1)
	canvasW := float32(totalDays) * spec.dayW
	todayX := float32(daysBetween(originDay, today)) * spec.dayW

	colorProp := resolveColorProp(db, tcfg.ColorPropID)

	items := buildTableItems(view, res)
	readOnlyDate := opts.ReadOnly || startProp.Type != database.PropDate
	openMode := resolveCardOpenMode(view)

	tlRoot := ui.Column(c.Key("tl-root")).Grow(1).MinWidth(0).MinHeight(0)
	tlRoot.Children(func() {
		viewportW := tlRoot.LayoutBounds().W
		if viewportW <= 0 {
			viewportW = 900
		}

		// 1. Sub-toolbar at the top (Show Table toggle, explicit bindings, Today jump, Left/Right scroll, Zoom selector).
		renderTimelineSubToolbar(c, st, &view, db, tcfg, startProp, colorProp, spec, today, originDay, todayX, leftW, canvasW, viewportW, opts, pal)
		if isBuiltinDate && len(datePropertiesOf(db)) == 0 {
			action := ""
			if !opts.ReadOnly {
				action = "+ " + i18n.T("添加「排期」字段")
			}
			renderFallbackBindingBanner(c, "tl-fallback", i18n.T("当前按「修改时间」展示"), action, pal, func() {
				addDefaultDateProperty(st, db, view, opts)
			})
		}
		ui.Box(c).Height(1).Background(pal.Border)

		// 2. Horizontally scrollable Timeline ruler + virtualized rows.
		scrollerX := ui.ScrollHorizontal(c.Key("tl-x")).TrackScroll(&st.TimelineScrollX).
			AlignItems(ui.Stretch).Grow(1).MinWidth(0).MinHeight(0)
		if sw := scrollerX.LayoutBounds().W; sw > 0 {
			viewportW = sw
		}

		// Center near Today on initial entry or zoom change once layout width is known.
		centerKey := view.ID + ":" + spec.id
		if st.TimelineCenteredKey != centerKey && viewportW > 0 {
			st.TimelineCenteredKey = centerKey
			laneW := max(200, viewportW-leftW)
			targetX := todayX - laneW*0.28
			maxScroll := max(float32(0), leftW+canvasW-viewportW)
			st.TimelineScrollX.X = min(maxScroll, max(0, targetX))
		}

		contentW := max(viewportW, leftW+canvasW)
		scrollX := st.TimelineScrollX.X

		scrollerX.Children(func() {
			ui.Column(c.Key("tl-inner")).Width(contentW).Grow(1).MinHeight(0).Children(func() {
				// Top sticky timeline ruler + optional frozen left table header.
				renderTimelineHeaderRow(c, st, leftW, canvasW, contentW, scrollX, viewportW, originDay, totalDays, today, todayX, spec, tcfg.ShowTable, pal)

				// Synchronized rows list.
				st.TimelineRowScroll.KeepOffset = true
				st.TimelineRowScroll.MoveAnimation = &dbSpringList
				list := ui.List(c.Key("tl-rows"), &st.TimelineRowScroll, len(items)).
					Grow(1).MinHeight(0).ItemKey(func(i int) any { return items[i].key })
				list.Rows(func(row ui.ListRow) {
					rc := row.Context
					it := items[row.Index]
					switch it.kind {
					case itemGroupHeader:
						renderGroupHeaderRow(rc, st, &view, it.group, contentW, scrollX, opts, pal)
					case itemAddRow:
						renderTimelineAddRow(rc, st, db, startProp, it.groupProp, it.groupValue, today, contentW, scrollX, opts, pal)
					case itemRecord:
						renderTimelineRecordRow(rc, st, &view, db, res, it.record, startProp, endProp, colorProp,
							originDay, totalDays, today, todayX, spec, leftW, canvasW, contentW, scrollX, viewportW,
							tcfg.ShowTable, readOnlyDate, openMode, opts, pal, loc)
					}
				})
			})
		})
	})
}

func renderTimelineSubToolbar(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	tcfg database.TimelineViewConfig,
	startProp database.Property,
	colorProp *database.Property,
	spec timelineScaleSpec,
	today, originDay time.Time,
	todayX, leftW, canvasW, viewportW float32,
	opts Options,
	pal Palette,
) {
	laneW := max(200, viewportW-leftW)
	centerDays := int((st.TimelineScrollX.X + laneW*0.35) / max(1, spec.dayW))
	anchorDate := originDay.AddDate(0, 0, centerDays)
	anchorLabel := fmt.Sprintf(i18n.T("%d 年 %d 月"), anchorDate.Year(), int(anchorDate.Month()))

	ui.Row(c.Key("tl-bar")).Height(40).Padding(4, 18).Gap(8).AlignItems(ui.Center).Children(func() {
		tblBtn := toolbarPillBtn(c, "tl-toggle-tbl", iconPanelRight, i18n.T("显示表格"), tcfg.ShowTable, pal)
		if tblBtn.Clicked() {
			vc := view.Clone()
			ensureTimelineConfig(&vc, db).ShowTable = !tcfg.ShowTable
			saveViewAndNotify(st, vc, opts)
		}

		ui.Text(c, anchorLabel).FontSize(13.5).FontWeight(600).TextColor(pal.Text).SingleLine()

		dateBtn := renderFieldBindingPill(c, "tl-bind-date", i18n.T("时间"), startProp.Name, startProp.Type != database.PropDate, pal)
		dateBtn.PopoverMenuButton(func(m *ui.Menu) {
			appendDatePropMenuItems(m, db, startProp.ID, false, opts.ReadOnly, func(propID string) {
				vc := view.Clone()
				ensureTimelineConfig(&vc, db).StartPropID = propID
				saveViewAndNotify(st, vc, opts)
			}, func() {
				addDefaultDateProperty(st, db, *view, opts)
			})
		})

		colorLabel := i18n.T("无")
		if colorProp != nil {
			colorLabel = colorProp.Name
		}
		colorBtn := renderFieldBindingPill(c, "tl-bind-color", i18n.T("配色"), colorLabel, colorProp == nil, pal)
		colorBtn.PopoverMenuButton(func(m *ui.Menu) {
			appendColorPropMenuItems(m, db, colorProp, tcfg.ColorPropID, i18n.T("不按属性着色"), opts.ReadOnly, func(propID string) {
				vc := view.Clone()
				ensureTimelineConfig(&vc, db).ColorPropID = propID
				saveViewAndNotify(st, vc, opts)
			}, func() {
				addDefaultStatusProperty(st, db, *view, opts)
			})
		})

		ui.Spacer(c)

		maxScroll := max(float32(0), leftW+canvasW-viewportW)
		prevBtn := ui.Box(c.Key("tl-prev")).Size(26, 26).Radius(6).Center().
			Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("向左滚动")).Tooltip(i18n.T("向左滚动"))
		if prevBtn.Hovered() {
			prevBtn.Background(pal.RowHover)
		}
		prevBtn.Children(func() { ui.Icon(c, iconChevronLeft).FontSize(14).TextColor(pal.TextMuted) })
		if prevBtn.Clicked() {
			st.TimelineScrollX.X = max(0, st.TimelineScrollX.X-laneW*0.45)
		}

		todayBtn := ui.Button(c.Key("tl-today"), i18n.T("今天")).Height(26).Padding(0, 10)
		if todayBtn.Clicked() {
			target := todayX - laneW*0.28
			st.TimelineScrollX.X = min(maxScroll, max(0, target))
		}

		nextBtn := ui.Box(c.Key("tl-next")).Size(26, 26).Radius(6).Center().
			Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("向右滚动")).Tooltip(i18n.T("向右滚动"))
		if nextBtn.Hovered() {
			nextBtn.Background(pal.RowHover)
		}
		nextBtn.Children(func() { ui.Icon(c, iconChevronRight).FontSize(14).TextColor(pal.TextMuted) })
		if nextBtn.Clicked() {
			st.TimelineScrollX.X = min(maxScroll, st.TimelineScrollX.X+laneW*0.45)
		}

		zBtn := ui.ButtonBase(c.Key("tl-zoom-btn")).Height(26).Padding(0, 10).Radius(6).
			Background(pal.Surface).Border(1, pal.Border).Gap(5).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Role(ui.RoleMenuButton).Label(i18n.T("缩放粒度") + ": " + spec.label)
		if zBtn.Hovered() {
			zBtn.Background(pal.RowHover)
		}
		zBtn.Children(func() {
			ui.Text(c, spec.label).FontSize(12.5).TextColor(pal.Text).SingleLine()
			ui.Icon(c, iconChevronDown).FontSize(12).TextColor(pal.TextMuted)
		})
		zBtn.PopoverMenuButton(func(m *ui.Menu) {
			for _, z := range timelineZoomLevels() {
				cand := z
				if m.Item(cand.label).Checked(cand.id == spec.id).Chosen() {
					vc := view.Clone()
					ensureTimelineConfig(&vc, db).Zoom = cand.id
					saveViewAndNotify(st, vc, opts)
				}
			}
		})
	})
}

func renderTimelineHeaderRow(
	c *ui.Context,
	st *State,
	leftW, canvasW, contentW, scrollX, viewportW float32,
	originDay time.Time,
	totalDays int,
	today time.Time,
	todayX float32,
	spec timelineScaleSpec,
	showTable bool,
	pal Palette,
) {
	head := ui.Box(c.Key("tl-head")).Width(contentW).Height(tlHeaderH).Shrink(0).
		Background(pal.HeaderBg).BorderWidth(0, 0, 1, 0).BorderColor(pal.Border).Clip()
	head.Children(func() {
		ruler := ui.Box(c.Key("tl-ruler")).PaddingStart(leftW).Width(leftW + canvasW).Height(tlHeaderH)
		ruler.Draw(func(p *ui.Painter, r ui.Rect) {
			canvasX := r.X + leftW
			p.Clip(ui.Rect{X: canvasX, Y: r.Y, W: r.W - leftW, H: r.H}, 0, func() {
				visStart := max(float32(0), scrollX-120)
				visEnd := min(canvasW, scrollX+viewportW+120)

				// Divider between top tier (Month/Year) and bottom tier (Days/Hours/Months).
				p.Line(canvasX+visStart, r.Y+24, canvasX+visEnd, r.Y+24, 1, pal.Border)

				dayStart := max(0, int(math.Floor(float64(visStart/spec.dayW)))-1)
				dayEnd := min(totalDays-1, int(math.Ceil(float64(visEnd/spec.dayW)))+1)

				// Sticky current-month label at left visible edge before first month boundary.
				firstVisDayIdx := min(totalDays-1, max(0, int(math.Floor(float64(scrollX/spec.dayW)))))
				firstVisDay := originDay.AddDate(0, 0, firstVisDayIdx)
				stickyMonthX := canvasX + scrollX + 8
				nextMonthX := canvasX + float32(totalDays)*spec.dayW

				for dIdx := dayStart; dIdx <= dayEnd; dIdx++ {
					day := originDay.AddDate(0, 0, dIdx)
					x := canvasX + float32(dIdx)*spec.dayW
					isTodayCol := day.Year() == today.Year() && day.Month() == today.Month() && day.Day() == today.Day()

					// Top tier: month/year markers on 1st of month.
					if day.Day() == 1 {
						if dIdx > 0 {
							p.Line(x, r.Y, x, r.Y+24, 1, pal.BorderStrong)
						}
						if x > stickyMonthX && x < nextMonthX {
							nextMonthX = x
						}
						lbl := fmt.Sprintf("%d/%02d", day.Year(), int(day.Month()))
						p.Text(x+6, r.Y+5, lbl, 11.5, pal.TextMuted)
					}

					// Bottom tier ticks by zoom density.
					switch spec.id {
					case "hour":
						p.Line(x, r.Y+24, x, r.Y+tlHeaderH, 1, pal.BorderStrong)
						if !isTodayCol {
							p.Text(x+4, r.Y+29, day.Format("01-02"), 11, pal.Text)
						}
						for h := 6; h < 24; h += 6 {
							hx := x + float32(h)/24*spec.dayW
							p.Line(hx, r.Y+36, hx, r.Y+tlHeaderH, 1, pal.Border)
							p.Text(hx+2, r.Y+30, fmt.Sprintf("%02d:00", h), 9.5, pal.TextMuted)
						}
					case "day", "week":
						p.Line(x, r.Y+24, x, r.Y+tlHeaderH, 1, pal.Border)
						if !isTodayCol {
							p.Text(x+4, r.Y+29, fmt.Sprintf("%d", day.Day()), 11, pal.TextMuted)
						}
					case "biweek":
						if day.Weekday() == time.Monday || day.Day() == 1 {
							p.Line(x, r.Y+24, x, r.Y+tlHeaderH, 1, pal.Border)
							if !isTodayCol {
								p.Text(x+3, r.Y+29, fmt.Sprintf("%d", day.Day()), 10.5, pal.TextMuted)
							}
						}
					case "month":
						if day.Weekday() == time.Monday {
							p.Line(x, r.Y+24, x, r.Y+tlHeaderH, 1, pal.Border)
							if !isTodayCol {
								p.Text(x+3, r.Y+29, fmt.Sprintf("%d", day.Day()), 10.5, pal.TextMuted)
							}
						}
					default: // quarter, year
						if day.Day() == 1 {
							p.Line(x, r.Y+24, x, r.Y+tlHeaderH, 1, pal.Border)
							p.Text(x+4, r.Y+29, fmt.Sprintf(i18n.T("%d月"), int(day.Month())), 10.5, pal.TextMuted)
						}
					}
				}

				if nextMonthX-stickyMonthX >= 68 {
					p.Text(stickyMonthX, r.Y+5, fmt.Sprintf("%d/%02d", firstVisDay.Year(), int(firstVisDay.Month())), 11.5, pal.TextMuted)
				}

				// Today badge centered in Today's column.
				txCenter := canvasX + todayX + spec.dayW*0.5
				if txCenter >= canvasX+visStart-20 && txCenter <= canvasX+visEnd+20 {
					bw := min(max(float32(26), spec.dayW-4), float32(32))
					badge := ui.Rect{X: txCenter - bw*0.5, Y: r.Y + 27, W: bw, H: 18}
					p.Fill(badge, pal.Danger, 9)
					p.Text(txCenter-7.5, badge.Y+2.5, fmt.Sprintf("%02d", today.Day()), 10.5, ui.RGB(255, 255, 255))
				}
			})
		})

		if showTable && leftW > 0 {
			frozen := ui.Row(c.Key("tl-fz-head")).Absolute().Left(0).Top(0).Bottom(0).Width(leftW).
				Translate(scrollX, 0).Background(pal.HeaderBg).AlignItems(ui.Center).
				Padding(0, 0, 0, 12).BorderWidth(0, 1, 0, 0).BorderColor(pal.Border)
			if scrollX > 0.5 {
				frozen.Shadow(2, 0, 8, 0, pal.ShadowEdge)
			}
			frozen.Children(func() {
				ui.Icon(c, iconPropTitle).FontSize(12).TextColor(pal.TextMuted)
				ui.Box(c).Width(6)
				ui.Text(c, i18n.T("名称")).FontSize(12.5).FontWeight(600).TextColor(pal.TextMuted).SingleLine().Grow(1).MinWidth(0)

				// Left table resize grip.
				grip := ui.Box(c.Key("tl-tbl-resize")).Width(6).Height(tlHeaderH).Shrink(0).Cursor(ui.CursorResizeEW)
				if dx, _, held := grip.Dragged(); held && dx != 0 {
					st.TimelineTableWidth = min(max(leftW+dx, 180), 420)
				}
			})
		}
	})
}

func renderTimelineAddRow(
	c *ui.Context,
	st *State,
	db *database.Database,
	startProp database.Property,
	groupProp string,
	groupVal database.Value,
	today time.Time,
	contentW, scrollX float32,
	opts Options,
	pal Palette,
) {
	if opts.ReadOnly {
		return
	}
	row := ui.Box(c.Key("tl-add:"+groupProp+":"+groupVal.PlainString())).
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
			initial := make(map[string]database.Value, 2)
			if startProp.Type == database.PropDate {
				initial[startProp.ID] = database.DateRangeValue(today.Format("2006-01-02"), "", false)
			}
			if groupProp != "" && !groupVal.IsEmpty() {
				initial[groupProp] = groupVal
			}
			createRecordWithInitial(st, db, initial, opts)
		}
	})
}

func renderTimelineRecordRow(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	rec *database.Record,
	startProp database.Property,
	endProp *database.Property,
	statusProp *database.Property,
	originDay time.Time,
	totalDays int,
	today time.Time,
	todayX float32,
	spec timelineScaleSpec,
	leftW, canvasW, contentW, scrollX, viewportW float32,
	showTable, readOnlyDate bool,
	openMode string,
	opts Options,
	pal Palette,
	loc *time.Location,
) {
	if rec == nil {
		return
	}
	tintBg, tintAccent, tinted := tintedRowBackground(pal, res, rec.ID)
	rowBg := pal.Paper
	if tinted {
		rowBg = tintBg
	}
	row := ui.Box(c.Key("tlr:"+rec.ID)).Width(contentW).Height(tlRowH).Background(rowBg).
		BorderWidth(0, 0, 1, 0).BorderColor(pal.Border.Alpha(0.7))
	hovered := row.Hovered()
	if hovered {
		if tinted {
			rowBg = tintBg.Mix(pal.Text, 0.03)
		} else {
			rowBg = pal.RowHover
		}
		row.Background(rowBg)
	}
	if tinted {
		row.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X + scrollX, Y: r.Y, W: 3, H: r.H}, tintAccent, 0)
		})
	}

	sDay, eDay, sVal, eVal, hasRange := parseRecordDateRange(rec, startProp, endProp, loc)

	row.Children(func() {
		// Right timeline lane.
		lane := ui.Box(c.Key("lane:" + rec.ID)).PaddingStart(leftW).Width(leftW + canvasW).Height(tlRowH)
		lane.Draw(func(p *ui.Painter, r ui.Rect) {
			canvasX := r.X + leftW
			p.Clip(ui.Rect{X: canvasX, Y: r.Y, W: r.W - leftW, H: r.H}, 0, func() {
				visStart := max(float32(0), scrollX-120)
				visEnd := min(canvasW, scrollX+viewportW+120)
				d0 := max(0, int(math.Floor(float64(visStart/spec.dayW)))-1)
				d1 := min(totalDays-1, int(math.Ceil(float64(visEnd/spec.dayW)))+1)

				for dIdx := d0; dIdx <= d1; dIdx++ {
					day := originDay.AddDate(0, 0, dIdx)
					x := canvasX + float32(dIdx)*spec.dayW

					if spec.dayW >= 16 && (day.Weekday() == time.Saturday || day.Weekday() == time.Sunday) {
						p.Fill(ui.Rect{X: x, Y: r.Y, W: spec.dayW, H: r.H}, pal.Surface.Alpha(0.7), 0)
					}

					drawGrid := false
					switch spec.id {
					case "hour", "day", "week":
						drawGrid = true
					case "biweek":
						drawGrid = day.Weekday() == time.Monday
					case "month":
						drawGrid = day.Weekday() == time.Monday
					default:
						drawGrid = day.Day() == 1
					}
					if drawGrid {
						p.Line(x, r.Y, x, r.Y+r.H, 1, pal.Border.Alpha(0.55))
					}
				}

				// Vertical Today line centered on Today's column.
				tx := canvasX + todayX + spec.dayW*0.5
				if tx >= canvasX+visStart && tx <= canvasX+visEnd {
					p.Line(tx, r.Y, tx, r.Y+r.H, 1.5, pal.Danger)
				}
			})
		})

		lane.Children(func() {
			if hasRange {
				dispStart, dispEnd := sDay, eDay
				draggingThis := st.TimelineDragActive && st.TimelineDragRecordID == rec.ID
				if draggingThis {
					dispStart, dispEnd = previewTimelineDrag(sDay, eDay, st.TimelineDragMode, st.TimelineDragDX, spec.dayW)
				}

				rawBarX := float32(daysBetween(originDay, dispStart)) * spec.dayW
				rawBarW := max(26, float32(daysBetween(dispStart, dispEnd)+1)*spec.dayW)
				barX := min(max(rawBarX, 0), canvasW-26)
				barW := min(rawBarW, max(26, canvasW-barX))

				accent := pal.Accent
				barBg := pal.AccentSoft
				colName := ""
				if res != nil && res.RowColors != nil {
					colName = res.RowColors[rec.ID]
				}
				if colName == "" {
					colName = recordAccentColorForProp(statusProp, rec)
				}
				if colName != "" {
					accent = StatusDotColor(pal.Dark, colName)
					pillBg, _ := TagColors(pal.Dark, colName)
					barBg = pillBg
				}

				bar := ui.Row(c.Key("tlbar:"+rec.ID)).Absolute().Left(leftW+barX).Top(5).Width(barW).Height(30).
					Radius(7).Background(barBg).Border(1, accent.Alpha(0.45)).
					BorderWidth(1, 1, 1, 3).AlignItems(ui.Center).Clip().
					Role(ui.RoleListItem).Label(rec.Title)
				if !draggingThis {
					bar.Transition(ui.ElementTransition{
						Animation: &dbSpringList,
						Position:  true,
						Width:     true,
						Colors:    true,
						Shadows:   true,
					})
				}
				barHovered := bar.Hovered()
				if barHovered || draggingThis {
					bar.Shadow(0, 4, 12, -2, pal.ShadowEdge).Border(1, accent)
				}
				if pulse := st.Motion.pulseAlpha(c, rec.ID); pulse > 0.01 {
					bar.DrawOver(func(p *ui.Painter, r ui.Rect) {
						paintCreationPulse(p, r, 7, pulse, accent)
					})
				}
				applyCardHoverLift(bar, barHovered && !draggingThis, false)
				attachRecordContextMenu(bar, st, rec, opts)

				bar.Children(func() {
					// Left resize grip.
					if !readOnlyDate {
						lGrip := ui.Box(c.Key("tlgL")).Width(7).Height(30).Shrink(0).Cursor(ui.CursorResizeEW).
							Role(ui.RoleButton).Label(i18n.T("调整开始日期") + ": " + rec.Title)
						trackTimelineDrag(st, lGrip, rec, tlDragLeft, sDay, eDay, sVal, eVal, startProp, endProp, spec.dayW, loc, opts)
					}

					// Center draggable bar body.
					body := ui.Row(c.Key("tlbody")).Grow(1).MinWidth(0).Height(30).Padding(0, 6).Gap(5).
						AlignItems(ui.Center).Cursor(ui.CursorPointer)
					if !readOnlyDate {
						body.Cursor(ui.CursorGrab)
					}
					wasMoved := false
					if !readOnlyDate {
						wasMoved = trackTimelineDrag(st, body, rec, tlDragMove, sDay, eDay, sVal, eVal, startProp, endProp, spec.dayW, loc, opts)
					}
					body.Children(func() {
						if rec.Icon != "" {
							ui.Text(c, rec.Icon).FontSize(12).NoWrap()
						}
						title := strings.TrimSpace(rec.Title)
						if title == "" {
							title = i18n.T("未命名")
						}
						ui.Text(c, title).FontSize(12.5).FontWeight(500).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)
						if draggingThis && barW >= 120 {
							rangeTxt := fmt.Sprintf("%s → %s", dispStart.Format("01-02"), dispEnd.Format("01-02"))
							ui.Text(c, rangeTxt).FontSize(10.5).TextColor(pal.TextMuted).NoWrap()
						}
					})
					if body.Clicked() && !wasMoved && opts.OnOpenRecord != nil {
						opts.OnOpenRecord(rec, openMode)
					}

					// Right resize grip.
					if !readOnlyDate {
						rGrip := ui.Box(c.Key("tlgR")).Width(7).Height(30).Shrink(0).Cursor(ui.CursorResizeEW).
							Role(ui.RoleButton).Label(i18n.T("调整结束日期") + ": " + rec.Title)
						trackTimelineDrag(st, rGrip, rec, tlDragRight, sDay, eDay, sVal, eVal, startProp, endProp, spec.dayW, loc, opts)
					}
				})
			} else if !readOnlyDate {
				// Undated record: always-discoverable quick schedule pill right after Today line.
				slotX := min(max(todayX+spec.dayW*0.5+8, 6), max(6, canvasW-116))
				schedBtn := ui.ButtonBase(c.Key("tl-sched:"+rec.ID)).Absolute().Left(leftW+slotX).Top(7).Height(26).Padding(0, 10).
					Radius(5).Background(pal.Paper).Border(1, pal.Border).AlignItems(ui.Center).Cursor(ui.CursorPointer).
					Role(ui.RoleButton).Label(i18n.T("安排排期") + ": " + rec.Title)
				txtCol := pal.TextMuted
				if hovered || schedBtn.Hovered() {
					schedBtn.Background(pal.AccentSoft).Border(1, pal.Accent.Alpha(0.45))
					txtCol = pal.Accent
				}
				schedBtn.Children(func() {
					ui.Text(c, "+ "+i18n.T("安排排期")).FontSize(11.5).FontWeight(500).TextColor(txtCol).SingleLine()
				})
				if schedBtn.Clicked() {
					sKey := today.Format("2006-01-02")
					eKey := today.AddDate(0, 0, 2).Format("2006-01-02")
					st.setCellAndNotify(rec.ID, startProp.ID, database.DateRangeValue(sKey, eKey, false), opts)
				}
			}
		})

		// Frozen left table row.
		if showTable && leftW > 0 {
			fz := ui.Row(c.Key("tlfz:"+rec.ID)).Absolute().Left(0).Top(0).Bottom(0).Width(leftW).
				Translate(scrollX, 0).Background(rowBg).AlignItems(ui.Center).
				Padding(0, 8, 0, 12).Gap(6).BorderWidth(0, 1, 0, 0).BorderColor(pal.Border).
				Cursor(ui.CursorPointer)
			if scrollX > 0.5 {
				fz.Shadow(2, 0, 8, 0, pal.ShadowEdge)
			}
			attachRecordContextMenu(fz, st, rec, opts)

			var consumedTitle bool
			fz.Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
					consumedTitle = renderCardTitleRow(c, st, db, rec, true, false, opts, pal)
				})
				if statusProp != nil && leftW >= 220 {
					sv := rec.ValueFor(*statusProp)
					if !sv.IsEmpty() {
						opt := findOption(*statusProp, sv.Text)
						dot := StatusDotColor(pal.Dark, opt.Color)
						ui.Box(c).Size(7, 7).Radius(3.5).Background(dot).Shrink(0).Tooltip(sv.Text)
					}
				}
			})
			if fz.Clicked() && !consumedTitle && opts.OnOpenRecord != nil {
				opts.OnOpenRecord(rec, openMode)
			}
		}
	})
	_ = view
}

func previewTimelineDrag(sDay, eDay time.Time, mode uint8, dx, dayW float32) (time.Time, time.Time) {
	delta := int(math.Round(float64(dx / max(1, dayW))))
	if delta == 0 {
		return sDay, eDay
	}
	switch mode {
	case tlDragMove:
		return sDay.AddDate(0, 0, delta), eDay.AddDate(0, 0, delta)
	case tlDragLeft:
		next := sDay.AddDate(0, 0, delta)
		if next.After(eDay) {
			next = eDay
		}
		return next, eDay
	case tlDragRight:
		next := eDay.AddDate(0, 0, delta)
		if next.Before(sDay) {
			next = sDay
		}
		return sDay, next
	default:
		return sDay, eDay
	}
}

func trackTimelineDrag(
	st *State,
	el ui.Element,
	rec *database.Record,
	mode uint8,
	sDay, eDay time.Time,
	sVal, eVal database.DateValue,
	startProp database.Property,
	endProp *database.Property,
	dayW float32,
	loc *time.Location,
	opts Options,
) (justMoved bool) {
	dx, _, held := el.Dragged()
	if held {
		if !st.TimelineDragActive || st.TimelineDragRecordID != rec.ID || st.TimelineDragMode != mode {
			st.TimelineDragActive = true
			st.TimelineDragRecordID = rec.ID
			st.TimelineDragMode = mode
			st.TimelineDragDX = 0
			st.TimelineDragMoved = false
			st.TimelineDragOrigStart = sDay
			st.TimelineDragOrigEnd = eDay
			st.TimelineDragOrigVal = sVal
			st.TimelineDragOrigEndVal = eVal
		}
		if dx != 0 {
			st.TimelineDragDX += dx
			if math.Abs(float64(st.TimelineDragDX)) > 2 {
				st.TimelineDragMoved = true
			}
		}
		return false
	}

	if el.PressCanceled() {
		if st.TimelineDragActive && st.TimelineDragRecordID == rec.ID && st.TimelineDragMode == mode {
			st.TimelineDragActive = false
			st.TimelineDragRecordID = ""
			st.TimelineDragMode = tlDragNone
			st.TimelineDragDX = 0
			st.TimelineDragMoved = false
		}
		return false
	}

	if st.TimelineDragActive && st.TimelineDragRecordID == rec.ID && st.TimelineDragMode == mode {
		wasMoved := st.TimelineDragMoved
		origStart := st.TimelineDragOrigStart
		origEnd := st.TimelineDragOrigEnd
		origStartVal := st.TimelineDragOrigVal
		origEndVal := st.TimelineDragOrigEndVal
		newStart, newEnd := previewTimelineDrag(origStart, origEnd, mode, st.TimelineDragDX, dayW)

		st.TimelineDragActive = false
		st.TimelineDragRecordID = ""
		st.TimelineDragMode = tlDragNone
		st.TimelineDragDX = 0
		st.TimelineDragMoved = false

		dStart := daysBetween(origStart, newStart)
		dEnd := daysBetween(origEnd, newEnd)
		if dStart != 0 || dEnd != 0 {
			commitTimelineDateEdit(st, rec, startProp, endProp, origStartVal, origEndVal, newStart, newEnd, dStart, dEnd, loc, opts)
		}
		return wasMoved
	}
	return false
}

func commitTimelineDateEdit(
	st *State,
	rec *database.Record,
	startProp database.Property,
	endProp *database.Property,
	origStartVal, origEndVal database.DateValue,
	newStartDay, newEndDay time.Time,
	dStart, dEnd int,
	loc *time.Location,
	opts Options,
) {
	if endProp != nil && endProp.ID != startProp.ID {
		var edits []database.CellEdit
		if dStart != 0 {
			ns := shiftDateString(origStartVal.Start, dStart, loc)
			edits = append(edits, database.CellEdit{
				RecordID: rec.ID,
				PropID:   startProp.ID,
				Value:    database.DateRangeValue(ns, origStartVal.End, origStartVal.IncludeTime),
			})
		}
		if dEnd != 0 {
			var ne string
			if origEndVal.Start != "" {
				ne = shiftDateString(origEndVal.Start, dEnd, loc)
			} else {
				ne = newEndDay.Format("2006-01-02")
			}
			edits = append(edits, database.CellEdit{
				RecordID: rec.ID,
				PropID:   endProp.ID,
				Value:    database.DateRangeValue(ne, origEndVal.End, origEndVal.IncludeTime),
			})
		}
		if len(edits) > 0 {
			if affected, err := st.Store.SetCellsBatch(edits); err == nil && opts.OnChanged != nil {
				opts.OnChanged(affected)
			}
		}
		return
	}

	nextStart := origStartVal.Start
	if dStart != 0 {
		nextStart = shiftDateString(origStartVal.Start, dStart, loc)
	}
	nextEnd := origStartVal.End
	if dEnd != 0 {
		switch {
		case newEndDay.Equal(newStartDay) && origStartVal.End == "":
			nextEnd = ""
		case origStartVal.End != "":
			nextEnd = shiftDateString(origStartVal.End, dEnd, loc)
		default:
			nextEnd = newEndDay.Format("2006-01-02")
		}
	}
	st.setCellAndNotify(rec.ID, startProp.ID, database.DateRangeValue(nextStart, nextEnd, origStartVal.IncludeTime), opts)
}
