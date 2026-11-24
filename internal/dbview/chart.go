package dbview

import (
	"fmt"
	"math"
	"strconv"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

type chartKindOption struct {
	id    string
	label string
}

func chartKindsList() []chartKindOption {
	return []chartKindOption{
		{"bar", i18n.T("柱状图")},
		{"hbar", i18n.T("条形图")},
		{"line", i18n.T("折线图")},
		{"donut", i18n.T("环形图")},
	}
}

func chartKindLabel(kind string) string {
	for _, k := range chartKindsList() {
		if k.id == kind {
			return k.label
		}
	}
	return i18n.T("柱状图")
}

var chartColorCycle = []string{"blue", "purple", "green", "orange", "pink", "yellow", "red", "gray"}

func chartSeriesColor(dark bool, name string, idx int) ui.Color {
	if name != "" && name != "default" {
		return StatusDotColor(dark, name)
	}
	return StatusDotColor(dark, chartColorCycle[idx%len(chartColorCycle)])
}

func formatChartValue(v float64) string {
	if math.Abs(v-math.Round(v)) < 1e-6 {
		return strconv.FormatInt(int64(math.Round(v)), 10)
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

func niceChartScale(maxVal float64) (ceil float64, ticks []float64) {
	if maxVal <= 0 {
		return 4, []float64{0, 1, 2, 3, 4}
	}
	targetSteps := 4.0
	rawStep := maxVal / targetSteps
	mag := math.Pow(10, math.Floor(math.Log10(rawStep)))
	norm := rawStep / mag
	var niceNorm float64
	switch {
	case norm <= 1:
		niceNorm = 1
	case norm <= 2:
		niceNorm = 2
	case norm <= 2.5:
		niceNorm = 2.5
	case norm <= 5:
		niceNorm = 5
	default:
		niceNorm = 10
	}
	step := niceNorm * mag
	if step <= 0 {
		step = 1
	}
	ceil = math.Ceil(maxVal/step) * step
	if ceil <= 0 {
		ceil = step
	}
	for v := 0.0; v <= ceil+step*0.01; v += step {
		ticks = append(ticks, v)
	}
	return ceil, ticks
}

func chartView(
	c *ui.Context,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	opts Options,
	pal Palette,
) {
	cfg := ensureChartConfig(&view, db)
	var rows []*database.Record
	if res != nil {
		rows = res.Rows
	}
	data := database.BuildChartData(db, rows, *cfg, opts.Now)

	ui.Column(c.Key("chart-root")).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
		renderChartSubToolbar(c, st, &view, db, *cfg, opts, pal)
		ui.Box(c).Height(1).Background(pal.Border)

		ui.Column(c.Key("chart-wrap")).Grow(1).MinWidth(0).MinHeight(0).Padding(16, 22, 20, 22).Children(func() {
			card := ui.Column(c.Key("chart-card")).Grow(1).MinWidth(0).MinHeight(300).
				Radius(12).Background(pal.Paper).Border(1, pal.Border).Padding(18, 22).Gap(14).
				Shadow(0, 1, 4, 0, pal.ShadowEdge)

			card.Children(func() {
				// Header summary + Total badge.
				ui.Row(c.Key("chart-hdr")).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					subTitle := i18n.T("记录数")
					if cfg.YMode == "prop" && data.YLabel != "" {
						aggName := aggregationLabel(cfg.YAgg)
						if cfg.YAgg == "" {
							aggName = i18n.T("求和")
						}
						subTitle = data.YLabel + " · " + aggName
					}
					heading := fmt.Sprintf(i18n.T("按「%s」统计 · %s"), data.XPropName, subTitle)
					ui.Text(c, heading).FontSize(14).FontWeight(600).TextColor(pal.Text).SingleLine()

					ui.Box(c).Padding(3, 10).Radius(10).Background(pal.Surface).Children(func() {
						ui.Text(c, fmt.Sprintf(i18n.T("总计 %s"), formatChartValue(data.GrandTotal))).
							FontSize(12).FontWeight(600).TextColor(pal.TextMuted).SingleLine()
					})
				})

				if len(data.Categories) == 0 || data.MaxValue <= 0 {
					ui.Column(c.Key("chart-empty")).Grow(1).Center().Gap(8).Children(func() {
						ui.Icon(c, iconViewChart).FontSize(28).TextColor(pal.TextMuted.Alpha(0.45))
						ui.Text(c, i18n.T("暂无可展示的图表数据")).FontSize(13.5).TextColor(pal.TextMuted)
					})
					return
				}

				canvas := ui.Box(c.Key("chart-canvas:" + cfg.Kind)).Grow(1).MinWidth(0).MinHeight(220)
				px, py, hasPtr := canvas.PointerPosition()
				canvas.Draw(func(p *ui.Painter, r ui.Rect) {
					switch cfg.Kind {
					case "hbar":
						drawHorizontalBarChart(p, r, data, *cfg, px, py, hasPtr, pal)
					case "line":
						drawLineChart(p, r, data, *cfg, px, py, hasPtr, pal)
					case "donut":
						drawDonutChart(p, r, data, *cfg, px, py, hasPtr, pal)
					default:
						drawVerticalBarChart(p, r, data, *cfg, px, py, hasPtr, pal)
					}
				})

				if cfg.ShowLegend {
					renderChartLegend(c, data, *cfg, pal)
				}
			})
		})
	})
}

func renderChartSubToolbar(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	cfg database.ChartViewConfig,
	opts Options,
	pal Palette,
) {
	ui.Row(c.Key("chart-bar")).Height(42).Padding(4, 18).Gap(8).AlignItems(ui.Center).Children(func() {
		// 4-kind segmented switcher.
		ui.Row(c.Key("chart-kinds")).Gap(2).Padding(2).Radius(7).Background(pal.Surface).Children(func() {
			for _, k := range chartKindsList() {
				cand := k
				active := cfg.Kind == cand.id
				btn := ui.ButtonBase(c.Key("ck:"+cand.id)).Height(26).Padding(0, 10).Radius(5).
					AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().
					Role(ui.RoleTab).Selected(active).Label(cand.label)
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
					ensureChartConfig(&vc, db).Kind = cand.id
					saveViewAndNotify(st, vc, opts)
				}
			}
		})

		ui.Spacer(c)

		// Quick X-axis selector.
		xLabel := i18n.T("选择属性")
		if xp, idx := db.PropertyByID(cfg.XPropID); idx >= 0 {
			xLabel = xp.Name
		}
		xBtn := ui.ButtonBase(c.Key("ch-x-quick")).Height(26).Padding(0, 10).Radius(6).
			Background(pal.Surface).Border(1, pal.Border).Gap(5).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Role(ui.RoleMenuButton).Label(i18n.T("X 轴属性") + ": " + xLabel)
		xBtn.Children(func() {
			ui.Text(c, i18n.T("X 轴")+": "+xLabel).FontSize(12).TextColor(pal.Text).SingleLine()
			ui.Icon(c, iconChevronDown).FontSize(11).TextColor(pal.TextMuted)
		})
		xBtn.PopoverMenuButton(func(m *ui.Menu) {
			for _, p := range db.Properties {
				cand := p
				if m.Item(cand.Name).Checked(cand.ID == cfg.XPropID).Chosen() {
					vc := view.Clone()
					ensureChartConfig(&vc, db).XPropID = cand.ID
					saveViewAndNotify(st, vc, opts)
				}
			}
		})

		// Quick Y-axis metric selector.
		yLabel := i18n.T("记录数")
		if cfg.YMode == "prop" {
			if yp, idx := db.PropertyByID(cfg.YPropID); idx >= 0 {
				yLabel = yp.Name
			}
		}
		yBtn := ui.ButtonBase(c.Key("ch-y-quick")).Height(26).Padding(0, 10).Radius(6).
			Background(pal.Surface).Border(1, pal.Border).Gap(5).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Role(ui.RoleMenuButton).Label(i18n.T("Y 轴计算") + ": " + yLabel)
		yBtn.Children(func() {
			ui.Text(c, i18n.T("Y 轴")+": "+yLabel).FontSize(12).TextColor(pal.Text).SingleLine()
			ui.Icon(c, iconChevronDown).FontSize(11).TextColor(pal.TextMuted)
		})
		yBtn.PopoverMenuButton(func(m *ui.Menu) {
			if m.Item(i18n.T("记录数")).Checked(cfg.YMode != "prop").Chosen() {
				vc := view.Clone()
				ensureChartConfig(&vc, db).YMode = "count"
				saveViewAndNotify(st, vc, opts)
			}
			m.Separator()
			for _, p := range db.Properties {
				if p.Type != database.PropNumber {
					continue
				}
				cand := p
				if m.Item(cand.Name + " (" + i18n.T("求和") + ")").Checked(cfg.YMode == "prop" && cfg.YPropID == cand.ID).Chosen() {
					vc := view.Clone()
					cc := ensureChartConfig(&vc, db)
					cc.YMode = "prop"
					cc.YPropID = cand.ID
					cc.YAgg = database.AggSum
					saveViewAndNotify(st, vc, opts)
				}
			}
		})
	})
}

func renderChartLegend(c *ui.Context, data database.ChartData, cfg database.ChartViewConfig, pal Palette) {
	ui.Row(c.Key("chart-legend")).Wrap().Gap(12).AlignItems(ui.Center).Children(func() {
		if len(data.Series) > 1 && cfg.Kind != "donut" {
			for si, s := range data.Series {
				col := chartSeriesColor(pal.Dark, s.Color, si)
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(9, 9).Radius(3).Background(col).Shrink(0)
					ui.Text(c, s.Label).FontSize(12).TextColor(pal.Text).SingleLine()
				})
			}
			return
		}
		if cfg.Kind == "donut" || cfg.Kind == "bar" || cfg.Kind == "hbar" {
			for ci, cat := range data.Categories {
				if cfg.Kind == "donut" && ci < len(data.Totals) && data.Totals[ci] <= 0 {
					continue
				}
				colName := ""
				if ci < len(data.CategoryColors) {
					colName = data.CategoryColors[ci]
				}
				col := chartSeriesColor(pal.Dark, colName, ci)
				valStr := formatChartValue(data.Totals[ci])
				pctText := ""
				if cfg.Kind == "donut" && data.GrandTotal > 0 {
					pct := data.Totals[ci] * 100 / data.GrandTotal
					pctText = fmt.Sprintf(" (%.0f%%)", pct)
				}
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(9, 9).Radius(3).Background(col).Shrink(0)
					ui.Text(c, cat+" · "+valStr+pctText).FontSize(12).TextColor(pal.TextMuted).SingleLine()
				})
			}
		}
	})
}

func drawVerticalBarChart(p *ui.Painter, r ui.Rect, data database.ChartData, cfg database.ChartViewConfig, px, py float32, hasPtr bool, pal Palette) {
	padL, padR, padT, padB := float32(48), float32(16), float32(18), float32(34)
	plot := ui.Rect{
		X: r.X + padL,
		Y: r.Y + padT,
		W: max(40, r.W-padL-padR),
		H: max(40, r.H-padT-padB),
	}
	ceil, ticks := niceChartScale(data.MaxValue)

	// Horizontal gridlines & Y ticks.
	for _, tk := range ticks {
		frac := float32(tk / ceil)
		y := plot.Y + plot.H*(1-frac)
		p.Line(plot.X, y, plot.X+plot.W, y, 1, pal.Border)
		lbl := formatChartValue(tk)
		tw, th := p.MeasureText(padL-8, ui.Span{Text: lbl, Size: 11})
		p.Text(plot.X-tw-8, y-th*0.5, lbl, 11, pal.TextMuted)
	}

	nCat := len(data.Categories)
	if nCat == 0 {
		return
	}
	slotW := plot.W / float32(nCat)
	barW := min(float32(54), max(float32(12), slotW*0.52))

	hoverIdx := -1
	if hasPtr && px >= plot.X && px <= plot.X+plot.W && py >= plot.Y && py <= plot.Y+plot.H {
		idx := int((px - plot.X) / slotW)
		if idx >= 0 && idx < nCat {
			hoverIdx = idx
		}
	}

	multiSeries := len(data.Series) > 1
	for ci, cat := range data.Categories {
		slotX := plot.X + float32(ci)*slotW
		if ci == hoverIdx {
			p.Fill(ui.Rect{X: slotX + 2, Y: plot.Y, W: slotW - 4, H: plot.H}, pal.Surface, 6)
		}

		cx := slotX + slotW*0.5
		bx := cx - barW*0.5
		accY := plot.Y + plot.H

		for si, ser := range data.Series {
			v := 0.0
			if ci < len(ser.Values) {
				v = ser.Values[ci]
			}
			if v <= 0 {
				continue
			}
			bh := float32(v/ceil) * plot.H
			if bh < 2 {
				bh = 2
			}
			col := chartSeriesColor(pal.Dark, ser.Color, si)
			if !multiSeries {
				colName := ""
				if ci < len(data.CategoryColors) {
					colName = data.CategoryColors[ci]
				}
				col = chartSeriesColor(pal.Dark, colName, ci)
			}
			if ci == hoverIdx {
				col = col.Mix(pal.Text, 0.12)
			}
			top := max(plot.Y, accY-bh)
			rad := float32(5)
			if multiSeries && si+1 < len(data.Series) {
				rad = 2
			}
			p.Fill(ui.Rect{X: bx, Y: top, W: barW, H: accY - top}, col, rad)
			accY = top
		}

		if cfg.ShowValues && data.Totals[ci] > 0 {
			valStr := formatChartValue(data.Totals[ci])
			tw, th := p.MeasureText(slotW, ui.Span{Text: valStr, Size: 11})
			p.Text(cx-tw*0.5, max(r.Y+2, accY-th-4), valStr, 11, pal.Text)
		}

		// Category X label centered below bar.
		lbl := truncateChartLabel(cat, max(24, slotW-6), p, 11.5)
		tw, _ := p.MeasureText(slotW, ui.Span{Text: lbl, Size: 11.5})
		p.Text(cx-tw*0.5, plot.Y+plot.H+8, lbl, 11.5, pal.TextMuted)
	}

	if hoverIdx >= 0 {
		drawChartTooltip(p, r, plot.X+float32(hoverIdx)*slotW+slotW*0.5, plot.Y+14, data, hoverIdx, pal)
	}
}

func drawHorizontalBarChart(p *ui.Painter, r ui.Rect, data database.ChartData, cfg database.ChartViewConfig, px, py float32, hasPtr bool, pal Palette) {
	padL, padR, padT, padB := float32(116), float32(44), float32(10), float32(28)
	plot := ui.Rect{
		X: r.X + padL,
		Y: r.Y + padT,
		W: max(60, r.W-padL-padR),
		H: max(40, r.H-padT-padB),
	}
	ceil, ticks := niceChartScale(data.MaxValue)

	// Vertical gridlines & bottom X ticks.
	for _, tk := range ticks {
		frac := float32(tk / ceil)
		x := plot.X + plot.W*frac
		p.Line(x, plot.Y, x, plot.Y+plot.H, 1, pal.Border)
		lbl := formatChartValue(tk)
		tw, _ := p.MeasureText(60, ui.Span{Text: lbl, Size: 11})
		p.Text(x-tw*0.5, plot.Y+plot.H+6, lbl, 11, pal.TextMuted)
	}

	nCat := len(data.Categories)
	if nCat == 0 {
		return
	}
	rowH := min(float32(46), plot.H/float32(nCat))
	barH := min(float32(26), max(float32(10), rowH*0.56))

	hoverIdx := -1
	if hasPtr && px >= plot.X && px <= plot.X+plot.W && py >= plot.Y && py <= plot.Y+float32(nCat)*rowH {
		idx := int((py - plot.Y) / rowH)
		if idx >= 0 && idx < nCat {
			hoverIdx = idx
		}
	}

	multiSeries := len(data.Series) > 1
	for ci, cat := range data.Categories {
		rowY := plot.Y + float32(ci)*rowH
		if ci == hoverIdx {
			p.Fill(ui.Rect{X: r.X + 4, Y: rowY + 2, W: r.W - 8, H: rowH - 4}, pal.Surface, 6)
		}

		lbl := truncateChartLabel(cat, padL-14, p, 12)
		tw, th := p.MeasureText(padL-12, ui.Span{Text: lbl, Size: 12})
		p.Text(plot.X-tw-10, rowY+(rowH-th)*0.5, lbl, 12, pal.Text)

		by := rowY + (rowH-barH)*0.5
		accX := plot.X
		for si, ser := range data.Series {
			v := 0.0
			if ci < len(ser.Values) {
				v = ser.Values[ci]
			}
			if v <= 0 {
				continue
			}
			bw := float32(v/ceil) * plot.W
			if bw < 2 {
				bw = 2
			}
			col := chartSeriesColor(pal.Dark, ser.Color, si)
			if !multiSeries {
				colName := ""
				if ci < len(data.CategoryColors) {
					colName = data.CategoryColors[ci]
				}
				col = chartSeriesColor(pal.Dark, colName, ci)
			}
			p.Fill(ui.Rect{X: accX, Y: by, W: bw, H: barH}, col, 5)
			accX += bw
		}

		if cfg.ShowValues && data.Totals[ci] > 0 {
			valStr := formatChartValue(data.Totals[ci])
			_, vth := p.MeasureText(40, ui.Span{Text: valStr, Size: 11})
			p.Text(accX+6, rowY+(rowH-vth)*0.5, valStr, 11, pal.TextMuted)
		}
	}

	if hoverIdx >= 0 {
		drawChartTooltip(p, r, plot.X+plot.W*0.65, plot.Y+float32(hoverIdx)*rowH+rowH*0.5, data, hoverIdx, pal)
	}
}

func drawLineChart(p *ui.Painter, r ui.Rect, data database.ChartData, cfg database.ChartViewConfig, px, py float32, hasPtr bool, pal Palette) {
	padL, padR, padT, padB := float32(48), float32(22), float32(20), float32(34)
	plot := ui.Rect{
		X: r.X + padL,
		Y: r.Y + padT,
		W: max(40, r.W-padL-padR),
		H: max(40, r.H-padT-padB),
	}
	ceil, ticks := niceChartScale(data.MaxValue)

	for _, tk := range ticks {
		frac := float32(tk / ceil)
		y := plot.Y + plot.H*(1-frac)
		p.Line(plot.X, y, plot.X+plot.W, y, 1, pal.Border)
		lbl := formatChartValue(tk)
		tw, th := p.MeasureText(padL-8, ui.Span{Text: lbl, Size: 11})
		p.Text(plot.X-tw-8, y-th*0.5, lbl, 11, pal.TextMuted)
	}

	nCat := len(data.Categories)
	if nCat == 0 {
		return
	}
	xAt := func(i int) float32 {
		if nCat == 1 {
			return plot.X + plot.W*0.5
		}
		return plot.X + float32(i)*(plot.W/float32(nCat-1))
	}

	hoverIdx := -1
	if hasPtr && px >= plot.X-12 && px <= plot.X+plot.W+12 && py >= plot.Y && py <= plot.Y+plot.H {
		bestDist := float32(math.MaxFloat32)
		for i := 0; i < nCat; i++ {
			d := float32(math.Abs(float64(px - xAt(i))))
			if d < bestDist {
				bestDist = d
				hoverIdx = i
			}
		}
	}

	if hoverIdx >= 0 {
		hx := xAt(hoverIdx)
		p.Line(hx, plot.Y, hx, plot.Y+plot.H, 1, pal.BorderStrong)
	}

	type pt struct{ x, y float32 }
	multi := len(data.Series) > 1
	for si, ser := range data.Series {
		pts := make([]pt, nCat)
		for ci := 0; ci < nCat; ci++ {
			v := 0.0
			if ci < len(ser.Values) {
				v = ser.Values[ci]
			}
			pts[ci] = pt{
				x: xAt(ci),
				y: plot.Y + plot.H*(1-float32(v/ceil)),
			}
		}

		col := pal.Accent
		if multi {
			col = chartSeriesColor(pal.Dark, ser.Color, si)
		}

		if len(pts) > 1 {
			stroke := (&ui.Path{}).Cap(ui.RoundCap).Join(ui.RoundJoin)
			stroke.MoveTo(pts[0].x, pts[0].y)
			for i := 1; i < len(pts); i++ {
				if cfg.Smooth {
					midX := (pts[i-1].x + pts[i].x) * 0.5
					stroke.CubeTo(midX, pts[i-1].y, midX, pts[i].y, pts[i].x, pts[i].y)
				} else {
					stroke.LineTo(pts[i].x, pts[i].y)
				}
			}
			if !multi {
				area := (&ui.Path{}).MoveTo(pts[0].x, plot.Y+plot.H).LineTo(pts[0].x, pts[0].y)
				for i := 1; i < len(pts); i++ {
					if cfg.Smooth {
						midX := (pts[i-1].x + pts[i].x) * 0.5
						area.CubeTo(midX, pts[i-1].y, midX, pts[i].y, pts[i].x, pts[i].y)
					} else {
						area.LineTo(pts[i].x, pts[i].y)
					}
				}
				area.LineTo(pts[len(pts)-1].x, plot.Y+plot.H).Close()
				p.FillPath(area, col.Alpha(0.12))
			}
			p.StrokePath(stroke, 2.5, col)
		}

		for ci, pt := range pts {
			rad := float32(4)
			if ci == hoverIdx {
				rad = 6
			}
			p.FillPath((&ui.Path{}).Circle(pt.x, pt.y, rad+1.5), pal.Paper)
			p.FillPath((&ui.Path{}).Circle(pt.x, pt.y, rad), col)
			if cfg.ShowValues && !multi && ci < len(ser.Values) {
				valStr := formatChartValue(ser.Values[ci])
				tw, th := p.MeasureText(60, ui.Span{Text: valStr, Size: 11})
				p.Text(pt.x-tw*0.5, max(r.Y+2, pt.y-th-6), valStr, 11, pal.Text)
			}
		}
	}

	step := max(1, nCat/8)
	for ci, cat := range data.Categories {
		if ci%step != 0 && ci != nCat-1 {
			continue
		}
		lbl := truncateChartLabel(cat, 76, p, 11.5)
		tw, _ := p.MeasureText(80, ui.Span{Text: lbl, Size: 11.5})
		p.Text(xAt(ci)-tw*0.5, plot.Y+plot.H+8, lbl, 11.5, pal.TextMuted)
	}

	if hoverIdx >= 0 {
		drawChartTooltip(p, r, xAt(hoverIdx), plot.Y+16, data, hoverIdx, pal)
	}
}

func drawDonutChart(p *ui.Painter, r ui.Rect, data database.ChartData, cfg database.ChartViewConfig, px, py float32, hasPtr bool, pal Palette) {
	type sliceSeg struct {
		catIdx int
		label  string
		value  float64
		start  float32
		end    float32
		color  ui.Color
	}

	var sum float64
	for _, t := range data.Totals {
		if t > 0 {
			sum += t
		}
	}
	if sum <= 0 {
		return
	}

	var segs []sliceSeg
	acc := float32(0)
	for ci, t := range data.Totals {
		if t <= 0 {
			continue
		}
		frac := float32(t / sum)
		colName := ""
		if ci < len(data.CategoryColors) {
			colName = data.CategoryColors[ci]
		}
		segs = append(segs, sliceSeg{
			catIdx: ci,
			label:  data.Categories[ci],
			value:  t,
			start:  acc,
			end:    acc + frac,
			color:  chartSeriesColor(pal.Dark, colName, ci),
		})
		acc += frac
	}

	cx := r.X + r.W*0.5
	cy := r.Y + r.H*0.5
	outerR := min(r.W, r.H) * 0.42
	if outerR < 36 {
		outerR = 36
	}
	strokeW := outerR * 0.36
	midR := outerR - strokeW*0.5
	innerR := outerR - strokeW

	hoverSeg := -1
	if hasPtr {
		dx, dy := float64(px-cx), float64(py-cy)
		dist := float32(math.Hypot(dx, dy))
		hitMin := float32(0)
		if cfg.Donut {
			hitMin = max(8, innerR-4)
		}
		if dist >= hitMin && dist <= outerR+6 {
			theta := math.Atan2(dy, dx) + math.Pi/2
			if theta < 0 {
				theta += 2 * math.Pi
			}
			frac := float32(theta / (2 * math.Pi))
			for si, seg := range segs {
				if frac >= seg.start && frac <= seg.end {
					hoverSeg = si
					break
				}
			}
		}
	}

	if cfg.Donut {
		gapRad := 0.0
		if len(segs) > 1 {
			gapRad = min(0.035, 3.0/float64(max(24, midR)))
		}
		for si, seg := range segs {
			a0 := float64(seg.start)*2*math.Pi - math.Pi/2 + gapRad*0.5
			a1 := float64(seg.end)*2*math.Pi - math.Pi/2 - gapRad*0.5
			if a1 <= a0 {
				a0 = float64(seg.start)*2*math.Pi - math.Pi/2
				a1 = float64(seg.end)*2*math.Pi - math.Pi/2
			}
			rOut, rIn := outerR, innerR
			col := seg.color
			if si == hoverSeg {
				rOut += 5
				rIn = max(12, rIn-2)
				col = col.Mix(pal.Text, 0.1)
			}
			steps := max(16, int(math.Ceil((a1-a0)*40)))
			ring := (&ui.Path{}).MoveTo(cx+rOut*float32(math.Cos(a0)), cy+rOut*float32(math.Sin(a0)))
			for i := 1; i <= steps; i++ {
				a := a0 + (a1-a0)*float64(i)/float64(steps)
				ring.LineTo(cx+rOut*float32(math.Cos(a)), cy+rOut*float32(math.Sin(a)))
			}
			for i := steps; i >= 0; i-- {
				a := a0 + (a1-a0)*float64(i)/float64(steps)
				ring.LineTo(cx+rIn*float32(math.Cos(a)), cy+rIn*float32(math.Sin(a)))
			}
			ring.Close()
			p.FillPath(ring, col)
		}

		centerVal := formatChartValue(data.GrandTotal)
		centerCap := i18n.T("总计")
		if hoverSeg >= 0 {
			seg := segs[hoverSeg]
			centerVal = formatChartValue(seg.value)
			centerCap = fmt.Sprintf("%s (%.0f%%)", seg.label, seg.value*100/sum)
		}
		tw, th := p.MeasureText(innerR*1.8, ui.Span{Text: centerVal, Size: 22})
		p.Text(cx-tw*0.5, cy-th*0.65, centerVal, 22, pal.Text)
		cw, _ := p.MeasureText(innerR*1.8, ui.Span{Text: centerCap, Size: 11.5})
		p.Text(cx-cw*0.5, cy+th*0.35, centerCap, 11.5, pal.TextMuted)
	} else {
		for si, seg := range segs {
			rad := outerR
			col := seg.color
			if si == hoverSeg {
				rad += 5
				col = col.Mix(pal.Text, 0.1)
			}
			a0 := float64(seg.start)*2*math.Pi - math.Pi/2
			a1 := float64(seg.end)*2*math.Pi - math.Pi/2
			steps := max(8, int(math.Ceil((a1-a0)*24)))
			wedge := (&ui.Path{}).MoveTo(cx, cy)
			for i := 0; i <= steps; i++ {
				a := a0 + (a1-a0)*float64(i)/float64(steps)
				wedge.LineTo(cx+rad*float32(math.Cos(a)), cy+rad*float32(math.Sin(a)))
			}
			wedge.Close()
			p.FillPath(wedge, col)
		}
		if hoverSeg >= 0 {
			drawChartTooltip(p, r, px, py, data, segs[hoverSeg].catIdx, pal)
		}
	}
}

func truncateChartLabel(s string, maxW float32, p *ui.Painter, size float32) string {
	tw, _ := p.MeasureText(1000, ui.Span{Text: s, Size: size})
	if tw <= maxW {
		return s
	}
	runes := []rune(s)
	for len(runes) > 1 {
		runes = runes[:len(runes)-1]
		cand := string(runes) + "…"
		cw, _ := p.MeasureText(1000, ui.Span{Text: cand, Size: size})
		if cw <= maxW {
			return cand
		}
	}
	return s
}

func drawChartTooltip(p *ui.Painter, bounds ui.Rect, ax, ay float32, data database.ChartData, catIdx int, pal Palette) {
	if catIdx < 0 || catIdx >= len(data.Categories) {
		return
	}
	lines := []string{data.Categories[catIdx]}
	if len(data.Series) > 1 {
		for _, ser := range data.Series {
			v := 0.0
			if catIdx < len(ser.Values) {
				v = ser.Values[catIdx]
			}
			lines = append(lines, fmt.Sprintf("%s: %s", ser.Label, formatChartValue(v)))
		}
	} else {
		lines = append(lines, fmt.Sprintf("%s: %s", data.YLabel, formatChartValue(data.Totals[catIdx])))
	}

	var maxW float32
	for _, ln := range lines {
		tw, _ := p.MeasureText(240, ui.Span{Text: ln, Size: 11.5})
		if tw > maxW {
			maxW = tw
		}
	}
	boxW := maxW + 20
	boxH := float32(len(lines))*17 + 12
	tx := min(bounds.X+bounds.W-boxW-8, max(bounds.X+8, ax-boxW*0.5))
	ty := min(bounds.Y+bounds.H-boxH-8, max(bounds.Y+8, ay))
	tipRect := ui.Rect{X: tx, Y: ty, W: boxW, H: boxH}
	p.Shadow(tipRect, 7, 0, 4, 12, 0, pal.PopoverShadow)
	p.Fill(tipRect, pal.PopoverBg, 7)
	p.Stroke(tipRect, pal.PopoverBorder, 7, 1)
	for i, ln := range lines {
		col := pal.Text
		if i == 0 {
			col = pal.Text
		} else {
			col = pal.TextMuted
		}
		p.Text(tx+10, ty+7+float32(i)*17, ln, 11.5, col)
	}
}
