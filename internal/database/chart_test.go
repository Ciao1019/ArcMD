package database

import (
	"slices"
	"testing"
)

func TestBuildChartDataModesStackAndSort(t *testing.T) {
	st, now := buildQueryTestStore(t)
	db := st.Database()
	rows := st.Records()

	// 1. Default count by Status.
	cd := BuildChartData(db, rows, ChartViewConfig{
		Kind:    "bar",
		XPropID: "p_status",
		YMode:   "count",
	}, now)
	if cd.XPropName != "状态" || cd.GrandTotal != 4 {
		t.Fatalf("count chart = %+v, want XPropName=状态 GrandTotal=4", cd)
	}
	if !slices.Equal(cd.Categories, []string{"待办", "进行中", "评审中", "已完成"}) {
		t.Fatalf("categories = %v", cd.Categories)
	}
	if !slices.Equal(cd.Series[0].Values, []float64{1, 1, 1, 1}) {
		t.Fatalf("series values = %v, want [1 1 1 1]", cd.Series[0].Values)
	}

	// 2. Numeric property Sum by Priority, sorted descending by value.
	sumCD := BuildChartData(db, rows, ChartViewConfig{
		Kind:    "bar",
		XPropID: "p_priority",
		YMode:   "prop",
		YPropID: "p_pts",
		YAgg:    AggSum,
		XSort:   "val_desc",
	}, now)
	// P0=10, P1=6, empty=4, P2=2 -> order P0, P1, 未选择, P2.
	if len(sumCD.Categories) != 4 || sumCD.Categories[0] != "P0" || sumCD.Series[0].Values[0] != 10 {
		t.Fatalf("val_desc sum chart = %+v", sumCD)
	}
	if sumCD.MaxValue != 10 || sumCD.GrandTotal != 22 {
		t.Fatalf("max=%v grand=%v, want 10 and 22", sumCD.MaxValue, sumCD.GrandTotal)
	}

	// 3. Stacked bar by Status x Done checkbox.
	stacked := BuildChartData(db, rows, ChartViewConfig{
		Kind:        "bar",
		XPropID:     "p_status",
		StackPropID: "p_done",
		YMode:       "prop",
		YPropID:     "p_pts",
		YAgg:        AggSum,
	}, now)
	if len(stacked.Series) != 2 {
		t.Fatalf("expected 2 stacked series (已勾选/未勾选), got %d: %+v", len(stacked.Series), stacked.Series)
	}
	if stacked.GrandTotal != 22 {
		t.Fatalf("stacked grand total = %v, want 22", stacked.GrandTotal)
	}

	// 4. Date month grouping line chart.
	lineCD := BuildChartData(db, rows, ChartViewConfig{
		Kind:    "line",
		XPropID: "p_due",
		XDateBy: "month",
		YMode:   "count",
	}, now)
	if !slices.Equal(lineCD.Categories, []string{"2026-11", "2026-12"}) {
		t.Fatalf("date month categories = %v", lineCD.Categories)
	}
	if !slices.Equal(lineCD.Series[0].Values, []float64{3, 1}) {
		t.Fatalf("date month counts = %v, want [3 1]", lineCD.Series[0].Values)
	}
}
