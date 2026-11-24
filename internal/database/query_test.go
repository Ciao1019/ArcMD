package database

import (
	"slices"
	"testing"
	"time"
)

func buildQueryTestStore(t *testing.T) (*Store, time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 23, 14, 0, 0, 0, time.UTC)

	db := NewDefault("工作任务", SourceConfig{Type: SourceInline})
	db.Properties = []Property{
		{ID: "p_title", Key: "title", Name: "名称", Type: PropTitle},
		{
			ID: "p_status", Key: "status", Name: "状态", Type: PropStatus,
			Options: []SelectOption{
				{ID: "s1", Name: "待办", Color: "gray", Group: StatusTodo},
				{ID: "s2", Name: "进行中", Color: "blue", Group: StatusInProgress},
				{ID: "s3", Name: "评审中", Color: "purple", Group: StatusInProgress},
				{ID: "s4", Name: "已完成", Color: "green", Group: StatusComplete},
			},
		},
		{
			ID: "p_priority", Key: "priority", Name: "优先级", Type: PropSelect,
			Options: []SelectOption{
				{ID: "pr1", Name: "P0", Color: "red"},
				{ID: "pr2", Name: "P1", Color: "orange"},
				{ID: "pr3", Name: "P2", Color: "blue"},
			},
		},
		{ID: "p_tags", Key: "tags", Name: "标签", Type: PropMultiSelect},
		{ID: "p_pts", Key: "points", Name: "点数", Type: PropNumber},
		{ID: "p_done", Key: "done", Name: "完成", Type: PropCheckbox},
		{ID: "p_due", Key: "due", Name: "截止", Type: PropDate},
		{ID: "p_uid", Key: "uid", Name: "编号", Type: PropUniqueID, UniqueID: &UniqueIDConfig{Prefix: "TASK", Next: 12}},
		{ID: "p_secret", Key: "secret", Name: "隐藏列", Type: PropText},
	}
	db.Views = []ViewConfig{
		{
			ID:   "v_main",
			Name: "主视图",
			Type: ViewTable,
			Properties: []ViewProperty{
				{PropertyID: "p_title", Visible: true},
				{PropertyID: "p_status", Visible: true},
				{PropertyID: "p_priority", Visible: true},
				{PropertyID: "p_tags", Visible: true},
				{PropertyID: "p_pts", Visible: true},
				{PropertyID: "p_done", Visible: true},
				{PropertyID: "p_due", Visible: true},
				{PropertyID: "p_uid", Visible: true},
				{PropertyID: "p_secret", Visible: false},
			},
			Table: &TableViewConfig{
				Aggregations: map[string]AggregationType{
					"p_title": AggCountAll,
					"p_pts":   AggMedian,
					"p_done":  AggPercentChecked,
					"p_due":   AggDateRange,
				},
			},
		},
	}
	db.ActiveView = "v_main"
	db.InlineRows = []*Record{
		{
			ID: "r1", Title: "看板视图",
			Props: map[string]Value{
				"p_status":   TextValue("进行中"),
				"p_priority": TextValue("P0"),
				"p_tags":     ListValue("UI", "核心"),
				"p_pts":      NumberValue(10),
				"p_done":     BoolValue(false),
				"p_due":      DateRangeValue("2026-11-20", "2026-11-25", false),
				"p_uid":      TextValue("TASK-10"),
				"p_secret":   TextValue("hidden-only-token"),
			},
		},
		{
			ID: "r2", Title: "任务筛选器",
			Props: map[string]Value{
				"p_status":   TextValue("待办"),
				"p_priority": TextValue("P2"),
				"p_tags":     ListValue("核心"),
				"p_pts":      NumberValue(2),
				"p_done":     BoolValue(false),
				"p_due":      DateRangeValue("2026-11-10", "", false),
				"p_uid":      TextValue("TASK-2"),
			},
		},
		{
			ID: "r3", Title: "日程视图",
			Props: map[string]Value{
				"p_status":   TextValue("评审中"),
				"p_priority": TextValue("P1"),
				"p_tags":     ListValue("UI", "日历"),
				"p_pts":      NumberValue(6),
				"p_done":     BoolValue(true),
				"p_due":      DateRangeValue("2026-11-23", "", false),
				"p_uid":      TextValue("TASK-1"),
			},
		},
		{
			ID: "r4", Title: "归档清理",
			Props: map[string]Value{
				"p_status": TextValue("已完成"),
				"p_pts":    NumberValue(4),
				"p_done":   BoolValue(true),
				"p_due":    DateRangeValue("2026-12-02", "", false),
				"p_uid":    TextValue("TASK-3"),
			},
		},
	}
	return NewStore("", "work.db.md", db), now
}

func rowIDs(rows []*Record) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func TestQueryFiltersAndNestedGroups(t *testing.T) {
	st, now := buildQueryTestStore(t)

	// Nested (status_group_is == in_progress) AND (points >= 6 OR tags contains "日历")
	res := st.Query("v_main", QueryOptions{
		Now: now,
		AdHocFilter: &FilterGroup{
			Conjunction: "and",
			Rules: []FilterRule{
				{PropertyID: "p_status", Op: OpStatusGroupIs, Value: TextValue("in_progress")},
			},
			Groups: []FilterGroup{
				{
					Conjunction: "or",
					Rules: []FilterRule{
						{PropertyID: "p_pts", Op: OpGTE, Value: NumberValue(10)},
						{PropertyID: "p_tags", Op: OpContainsAny, Value: ListValue("日历")},
					},
				},
			},
		},
	})
	if got := rowIDs(res.Rows); !slices.Equal(got, []string{"r3", "r1"}) && !slices.Equal(got, []string{"r1", "r3"}) {
		t.Fatalf("nested filter rows = %v, want r1 & r3", got)
	}

	// Relative date: today (2026-11-23) matches r1 (range 11-20..11-25) and r3 (11-23).
	todayRes := st.Query("v_main", QueryOptions{
		Now: now,
		AdHocFilter: &FilterGroup{
			Conjunction: "and",
			Rules:       []FilterRule{{PropertyID: "p_due", Op: OpDateWithin, Value: TextValue("today")}},
		},
	})
	if got := rowIDs(todayRes.Rows); len(got) != 2 {
		t.Fatalf("today relative filter = %v, want 2 rows (r1, r3)", got)
	}

	// MultiSelect contains_all ["UI", "核心"] -> only r1.
	allTags := st.Query("v_main", QueryOptions{
		Now: now,
		AdHocFilter: &FilterGroup{
			Conjunction: "and",
			Rules:       []FilterRule{{PropertyID: "p_tags", Op: OpContainsAll, Value: ListValue("UI", "核心")}},
		},
	})
	if got := rowIDs(allTags.Rows); !slices.Equal(got, []string{"r1"}) {
		t.Fatalf("contains_all = %v, want [r1]", got)
	}

	// Checkbox checked -> r3, r4.
	checked := st.Query("v_main", QueryOptions{
		Now: now,
		AdHocFilter: &FilterGroup{
			Conjunction: "and",
			Rules:       []FilterRule{{PropertyID: "p_done", Op: OpChecked}},
		},
	})
	if len(checked.Rows) != 2 {
		t.Fatalf("checked = %v, want 2 rows", rowIDs(checked.Rows))
	}
}

func TestQuerySortEmptiesAlwaysLastAndSpecialTypes(t *testing.T) {
	st, now := buildQueryTestStore(t)

	// Priority select order: r1(P0) < r3(P1) < r2(P2) < r4(empty)
	asc := st.Query("v_main", QueryOptions{
		Now:        now,
		AdHocSorts: []SortRule{{PropertyID: "p_priority", Desc: false}},
	})
	if got := rowIDs(asc.Rows); !slices.Equal(got, []string{"r1", "r3", "r2", "r4"}) {
		t.Fatalf("select asc = %v, want [r1 r3 r2 r4]", got)
	}

	// Descending: r2(P2) > r3(P1) > r1(P0), r4(empty) MUST STILL BE LAST!
	desc := st.Query("v_main", QueryOptions{
		Now:        now,
		AdHocSorts: []SortRule{{PropertyID: "p_priority", Desc: true}},
	})
	if got := rowIDs(desc.Rows); !slices.Equal(got, []string{"r2", "r3", "r1", "r4"}) {
		t.Fatalf("select desc (empties last) = %v, want [r2 r3 r1 r4]", got)
	}

	// UniqueID numeric sort: TASK-1 < TASK-2 < TASK-3 < TASK-10 (not lexicographical TASK-10 < TASK-2!).
	uidRes := st.Query("v_main", QueryOptions{
		Now:        now,
		AdHocSorts: []SortRule{{PropertyID: "p_uid", Desc: false}},
	})
	if got := rowIDs(uidRes.Rows); !slices.Equal(got, []string{"r3", "r2", "r4", "r1"}) {
		t.Fatalf("uniqueID numeric sort = %v, want [r3 r2 r4 r1]", got)
	}
}

func TestQueryGroupingSubGroupingAndPinyinSearch(t *testing.T) {
	st, now := buildQueryTestStore(t)

	// Group by Status group + SubGroup by Priority on a Board view.
	board := ViewConfig{
		ID:   "v_board",
		Name: "看板",
		Type: ViewBoard,
		Group: &GroupConfig{
			PropertyID: "p_status",
			StatusBy:   "group",
		},
		Board: &BoardViewConfig{
			SubGroup: &GroupConfig{
				PropertyID: "p_priority",
				HideEmpty:  true,
			},
		},
	}
	if err := st.AddView(board, true); err != nil {
		t.Fatal(err)
	}
	res := st.Query("v_board", QueryOptions{Now: now})
	if len(res.Groups) != 3 {
		t.Fatalf("status groups = %d, want 3 (todo/in_progress/complete)", len(res.Groups))
	}
	if res.Groups[1].Key != string(StatusInProgress) || res.Groups[1].Count != 2 {
		t.Fatalf("in_progress group = %+v", res.Groups[1])
	}
	if len(res.Groups[1].SubGroups) != 2 {
		t.Fatalf("in_progress subgroups = %+v, want 2 non-empty priority groups", res.Groups[1].SubGroups)
	}

	// MultiSelect grouping places r1 and r3 both under "UI".
	v, _ := st.Database().ViewByID("v_main")
	vc := v.Clone()
	vc.Group = &GroupConfig{PropertyID: "p_tags", HideEmpty: true, Sort: "asc"}
	if err := st.UpdateView(vc); err != nil {
		t.Fatal(err)
	}
	multiRes := st.Query("v_main", QueryOptions{Now: now})
	foundUI := false
	for _, g := range multiRes.Groups {
		if g.Key == "UI" && g.Count == 2 {
			foundUI = true
		}
	}
	if !foundUI {
		t.Fatalf("expected UI multiselect group with count 2, got %+v", multiRes.Groups)
	}

	// Date grouping by month.
	vc.Group = &GroupConfig{PropertyID: "p_due", DateBy: "month", HideEmpty: true}
	if err := st.UpdateView(vc); err != nil {
		t.Fatal(err)
	}
	dateRes := st.Query("v_main", QueryOptions{Now: now})
	if len(dateRes.Groups) != 2 || dateRes.Groups[0].Key != "2026-11" || dateRes.Groups[0].Count != 3 || dateRes.Groups[1].Key != "2026-12" {
		t.Fatalf("date month groups = %+v", dateRes.Groups)
	}

	// Pinyin search: "kb" matches "看板视图" (r1), and hidden property token is ignored.
	pyRes := st.Query("v_main", QueryOptions{Now: now, Search: "kb"})
	if got := rowIDs(pyRes.Rows); !slices.Equal(got, []string{"r1"}) {
		t.Fatalf("pinyin search 'kb' = %v, want [r1]", got)
	}
	hiddenRes := st.Query("v_main", QueryOptions{Now: now, Search: "hidden-only-token"})
	if len(hiddenRes.Rows) != 0 {
		t.Fatalf("hidden property should not match search, got %v", rowIDs(hiddenRes.Rows))
	}
}

func TestAggregationsAllModesAndPagination(t *testing.T) {
	st, now := buildQueryTestStore(t)
	res := st.Query("v_main", QueryOptions{Now: now, Offset: 1, Limit: 2})
	if res.TotalCount != 4 || len(res.Rows) != 2 {
		t.Fatalf("pagination TotalCount=%d len=%d, want 4 and 2", res.TotalCount, len(res.Rows))
	}

	// Points are [10, 2, 6, 4] -> sorted [2, 4, 6, 10], median = (4+6)/2 = 5.
	if med := res.Aggregations["p_pts"]; !med.HasValue || med.Number != 5 || med.Display != "5" {
		t.Fatalf("median = %+v, want 5", med)
	}
	if pct := res.Aggregations["p_done"]; !pct.HasValue || pct.Display != "50%" {
		t.Fatalf("percent_checked = %+v, want 50%%", pct)
	}
	if dr := res.Aggregations["p_due"]; !dr.HasValue || dr.Number != 22 || dr.Date != "2026-11-10 → 2026-12-02" {
		t.Fatalf("date_range = %+v", dr)
	}

	// Odd median + numeric/value/unique aggregations.
	rows := st.Records()[:3] // points: 10, 2, 6 -> median = 6
	numProp, _ := st.Database().PropertyByID("p_pts")
	if got := ComputeAggregation(*numProp, rows, AggMedian); got.Number != 6 {
		t.Fatalf("odd median = %v, want 6", got.Number)
	}
	if got := ComputeAggregation(*numProp, st.Records(), AggSum); got.Number != 22 {
		t.Fatalf("sum = %v, want 22", got.Number)
	}
	if got := ComputeAggregation(*numProp, st.Records(), AggAverage); got.Number != 5.5 {
		t.Fatalf("avg = %v, want 5.5", got.Number)
	}
	if got := ComputeAggregation(*numProp, st.Records(), AggMin); got.Number != 2 {
		t.Fatalf("min = %v, want 2", got.Number)
	}
	if got := ComputeAggregation(*numProp, st.Records(), AggMax); got.Number != 10 {
		t.Fatalf("max = %v, want 10", got.Number)
	}
	if got := ComputeAggregation(*numProp, st.Records(), AggRange); got.Number != 8 {
		t.Fatalf("range = %v, want 8", got.Number)
	}

	tagProp, _ := st.Database().PropertyByID("p_tags")
	if got := ComputeAggregation(*tagProp, st.Records(), AggCountValues); got.Number != 5 {
		t.Fatalf("count_values = %v, want 5", got.Number)
	}
	if got := ComputeAggregation(*tagProp, st.Records(), AggCountUnique); got.Number != 3 {
		t.Fatalf("count_unique = %v, want 3", got.Number)
	}
	if got := ComputeAggregation(*tagProp, st.Records(), AggCountEmpty); got.Number != 1 {
		t.Fatalf("count_empty = %v, want 1", got.Number)
	}
	if got := ComputeAggregation(*tagProp, st.Records(), AggCountNotEmpty); got.Number != 3 {
		t.Fatalf("count_not_empty = %v, want 3", got.Number)
	}
	if got := ComputeAggregation(*tagProp, st.Records(), AggPercentEmpty); got.Display != "25%" {
		t.Fatalf("percent_empty = %v, want 25%%", got.Display)
	}
	if got := ComputeAggregation(*tagProp, st.Records(), AggPercentNotEmpty); got.Display != "75%" {
		t.Fatalf("percent_not_empty = %v, want 75%%", got.Display)
	}
}

func TestBoardHideEmptyCols(t *testing.T) {
	st, now := buildQueryTestStore(t)
	db := st.Database()
	pPri, _ := db.PropertyByID("p_priority")
	updatedPri := pPri.Clone()
	updatedPri.Options = append(updatedPri.Options, SelectOption{ID: "pr4", Name: "P3", Color: "gray"})
	if _, err := st.UpdateProperty(updatedPri); err != nil {
		t.Fatal(err)
	}

	board := ViewConfig{
		ID:    "v_board_empty",
		Name:  "优先级看板",
		Type:  ViewBoard,
		Group: &GroupConfig{PropertyID: "p_priority", HideEmpty: false},
		Board: &BoardViewConfig{HideEmptyCols: false},
	}
	if err := st.AddView(board, true); err != nil {
		t.Fatal(err)
	}

	resAll := st.Query("v_board_empty", QueryOptions{Now: now})
	foundEmpty := false
	for _, g := range resAll.Groups {
		if g.Key == "P3" && g.Count == 0 {
			foundEmpty = true
		}
	}
	if !foundEmpty {
		t.Fatalf("expected empty column P3 when HideEmptyCols is false, got %+v", resAll.Groups)
	}

	board.Board.HideEmptyCols = true
	if err := st.UpdateView(board); err != nil {
		t.Fatal(err)
	}
	resHidden := st.Query("v_board_empty", QueryOptions{Now: now})
	for _, g := range resHidden.Groups {
		if g.Count == 0 {
			t.Fatalf("did not expect empty column %q when HideEmptyCols is true", g.Key)
		}
	}
}
