package database

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

// ChartSeries holds the evaluated numeric values for one series across all chart categories.
type ChartSeries struct {
	Key    string    `json:"key"`
	Label  string    `json:"label"`
	Color  string    `json:"color,omitempty"`
	Values []float64 `json:"values"`
}

// ChartData holds the computed categories, series, and totals consumed by Chart view.
type ChartData struct {
	XPropName      string        `json:"xPropName"`
	YLabel         string        `json:"yLabel"`
	Categories     []string      `json:"categories"`
	CategoryColors []string      `json:"categoryColors,omitempty"`
	Series         []ChartSeries `json:"series,omitempty"`
	Totals         []float64     `json:"totals,omitempty"`
	MaxValue       float64       `json:"maxValue"`
	GrandTotal     float64       `json:"grandTotal"`
}

// BuildChartData aggregates rows into ChartData according to cfg.
func BuildChartData(db *Database, rows []*Record, cfg ChartViewConfig, now time.Time) ChartData {
	if db == nil {
		return ChartData{}
	}
	if now.IsZero() {
		now = time.Now()
	}

	propMap := make(map[string]Property, len(db.Properties))
	for _, p := range db.Properties {
		propMap[p.ID] = p
	}

	xProp, ok := resolveChartXProp(db, propMap, cfg.XPropID)
	if !ok {
		return ChartData{}
	}

	dateBy := cfg.XDateBy
	if (xProp.Type == PropDate || xProp.Type == PropCreatedTime || xProp.Type == PropEditedTime) && dateBy == "" {
		dateBy = "month"
	}
	bucketSort := "manual"
	if cfg.XSort == "asc" || cfg.XSort == "desc" {
		bucketSort = cfg.XSort
	}

	xGroups := buildRecordGroups(rows, xProp, GroupConfig{
		PropertyID: xProp.ID,
		DateBy:     dateBy,
		Sort:       bucketSort,
		HideEmpty:  false,
	}, nil, propMap, now)

	if cfg.Kind == "donut" {
		xGroups = slices.DeleteFunc(xGroups, func(g RecordGroup) bool { return len(g.Rows) == 0 })
	}

	yProp, hasYProp := propMap[cfg.YPropID]
	usePropAgg := strings.EqualFold(cfg.YMode, "prop") && hasYProp
	yAgg := cfg.YAgg
	yLabel := "记录数"
	if usePropAgg {
		if yAgg == "" || yAgg == AggNone {
			if yProp.Type == PropNumber {
				yAgg = AggSum
			} else {
				yAgg = AggCountNotEmpty
			}
		}
		yLabel = yProp.Name
	}

	evalSubset := func(subset []*Record) float64 {
		if len(subset) == 0 {
			return 0
		}
		if !usePropAgg {
			return float64(len(subset))
		}
		res := ComputeAggregation(yProp, subset, yAgg)
		if !res.HasValue {
			return 0
		}
		return res.Number
	}

	nCat := len(xGroups)
	out := ChartData{
		XPropName:      xProp.Name,
		YLabel:         yLabel,
		Categories:     make([]string, nCat),
		CategoryColors: make([]string, nCat),
		Totals:         make([]float64, nCat),
	}
	for i, g := range xGroups {
		out.Categories[i] = g.Label
		out.CategoryColors[i] = g.Color
	}

	var stackProp Property
	hasStack := false
	if cfg.StackPropID != "" && cfg.Kind != "donut" {
		if sp, okS := propMap[cfg.StackPropID]; okS && sp.ID != xProp.ID {
			stackProp = sp
			hasStack = true
		}
	}

	var stackKeys []RecordGroup
	if hasStack {
		stackKeys = buildRecordGroups(rows, stackProp, GroupConfig{
			PropertyID: stackProp.ID,
			DateBy:     "month",
			HideEmpty:  true,
		}, nil, propMap, now)
		if len(stackKeys) == 0 {
			hasStack = false
		}
	}

	if hasStack {
		out.Series = make([]ChartSeries, len(stackKeys))
		stackIdx := make(map[string]int, len(stackKeys))
		for si, sg := range stackKeys {
			stackIdx[sg.Key] = si
			out.Series[si] = ChartSeries{
				Key:    sg.Key,
				Label:  sg.Label,
				Color:  sg.Color,
				Values: make([]float64, nCat),
			}
		}
		for ci, xg := range xGroups {
			subs := buildRecordGroups(xg.Rows, stackProp, GroupConfig{
				PropertyID: stackProp.ID,
				DateBy:     "month",
				HideEmpty:  true,
			}, nil, propMap, now)
			var rowTotal float64
			for _, sub := range subs {
				if si, ok := stackIdx[sub.Key]; ok {
					v := evalSubset(sub.Rows)
					out.Series[si].Values[ci] = v
					rowTotal += v
				}
			}
			out.Totals[ci] = rowTotal
		}
	} else {
		vals := make([]float64, nCat)
		for ci, xg := range xGroups {
			v := evalSubset(xg.Rows)
			vals[ci] = v
			out.Totals[ci] = v
		}
		out.Series = []ChartSeries{{
			Key:    "value",
			Label:  yLabel,
			Values: vals,
		}}
	}

	if (cfg.XSort == "val_desc" || cfg.XSort == "val_asc") && nCat > 1 {
		perm := make([]int, nCat)
		for i := range perm {
			perm[i] = i
		}
		desc := cfg.XSort == "val_desc"
		slices.SortStableFunc(perm, func(a, b int) int {
			c := cmp.Compare(out.Totals[a], out.Totals[b])
			if desc {
				return -c
			}
			return c
		})
		cats := make([]string, nCat)
		cols := make([]string, nCat)
		tots := make([]float64, nCat)
		for i, orig := range perm {
			cats[i] = out.Categories[orig]
			cols[i] = out.CategoryColors[orig]
			tots[i] = out.Totals[orig]
		}
		out.Categories = cats
		out.CategoryColors = cols
		out.Totals = tots
		for si := range out.Series {
			reordered := make([]float64, nCat)
			for i, orig := range perm {
				reordered[i] = out.Series[si].Values[orig]
			}
			out.Series[si].Values = reordered
		}
	}

	isLine := cfg.Kind == "line"
	for ci, tot := range out.Totals {
		if !isLine && tot > out.MaxValue {
			out.MaxValue = tot
		}
		if isLine {
			for si := range out.Series {
				if v := out.Series[si].Values[ci]; v > out.MaxValue {
					out.MaxValue = v
				}
			}
		}
		out.GrandTotal += tot
	}

	if usePropAgg && (yAgg == AggAverage || yAgg == AggMedian || yAgg == AggMin || yAgg == AggMax) {
		overall := ComputeAggregation(yProp, rows, yAgg)
		if overall.HasValue {
			out.GrandTotal = overall.Number
		}
	}
	return out
}

func resolveChartXProp(db *Database, propMap map[string]Property, wantID string) (Property, bool) {
	if wantID != "" {
		if p, ok := propMap[wantID]; ok {
			return p, true
		}
	}
	for _, pt := range []PropertyType{PropStatus, PropSelect, PropMultiSelect, PropDate, PropCheckbox, PropPerson} {
		for _, p := range db.Properties {
			if p.Type == pt {
				return p, true
			}
		}
	}
	if len(db.Properties) > 0 {
		return db.Properties[0], true
	}
	return Property{}, false
}
