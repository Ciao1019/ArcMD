package dbview_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/dbview"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

func buildZeroPropStore(t *testing.T) *database.Store {
	t.Helper()
	root := t.TempDir()
	db := database.NewDefault("极简笔记", database.SourceConfig{Type: database.SourceFolder})
	db.Properties = []database.Property{
		{ID: "prop_title", Key: "title", Name: "标题", Type: database.PropTitle, Width: 240},
	}
	db.ActiveView = "view_table"
	db.Views = []database.ViewConfig{
		{
			ID:         "view_table",
			Name:       "表格",
			Type:       database.ViewTable,
			Properties: []database.ViewProperty{{PropertyID: "prop_title", Visible: true, Width: 240}},
		},
	}
	rawDB, err := database.EncodeDatabaseMarkdown(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "极简笔记.db.md", rawDB); err != nil {
		t.Fatal(err)
	}

	modTS := time.Date(2026, 11, 23, 9, 30, 0, 0, time.UTC)
	for _, item := range []struct {
		file string
		body string
	}{
		{"note1.md", "# 晨间随笔\n\n第一篇纯文本笔记。\n"},
		{"note2.md", "# 阅读摘录\n\n第二篇纯文本笔记。\n"},
	} {
		rel := filepath.ToSlash(filepath.Join("极简笔记", item.file))
		if err := vault.Write(root, rel, item.body); err != nil {
			t.Fatal(err)
		}
		full := filepath.Join(root, "极简笔记", item.file)
		if err := os.Chtimes(full, modTS, modTS); err != nil {
			t.Fatal(err)
		}
	}

	st, err := database.Open(root, "极简笔记.db.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestZeroPropertyDatabaseFallbacksAcrossViews(t *testing.T) {
	t.Run("FeedTimelineAndBoardUngroupedFallback", func(t *testing.T) {
		st := buildZeroPropStore(t)
		db := st.Database()
		for _, spec := range []struct {
			vt   database.ViewType
			name string
		}{
			{database.ViewFeed, "动态"},
			{database.ViewTimeline, "甘特"},
			{database.ViewBoard, "看板"},
		} {
			if err := st.AddView(dbview.NewViewConfig(spec.vt, spec.name, db), false); err != nil {
				t.Fatal(err)
			}
		}

		state, tt, _, _, _ := makeTesterWithOptions(t, st, nil)

		// 1. Feed view renders zero-property records cleanly.
		if err := tt.Click("动态"); err != nil {
			t.Fatalf("switch to Feed failed: %v", err)
		}
		tt.Frame()
		for _, want := range []string{"晨间随笔", "阅读摘录"} {
			if !tt.HasText(want) {
				t.Fatalf("Feed missing %q, texts=%v", want, tt.Texts())
			}
		}

		// 2. Timeline falls back to built-in edited time with inline banner, never blocking.
		if err := tt.Click("甘特"); err != nil {
			t.Fatalf("switch to Timeline failed: %v", err)
		}
		tt.Frame()
		for _, want := range []string{"当前按「修改时间」展示", "+ 添加「排期」字段", "晨间随笔", "阅读摘录"} {
			if !tt.HasText(want) {
				t.Fatalf("Timeline fallback missing %q, texts=%v", want, tt.Texts())
			}
		}

		// 3. Board falls back to single "全部记录" column with "+ 新建状态列" CTA.
		if err := tt.Click("看板"); err != nil {
			t.Fatalf("switch to Board failed: %v", err)
		}
		tt.Frame()
		for _, want := range []string{"全部记录", "当前未设置分组列，已展示全部记录", "+ 新建状态列", "晨间随笔", "阅读摘录"} {
			if !tt.HasText(want) {
				t.Fatalf("Board fallback missing %q, texts=%v", want, tt.Texts())
			}
		}

		// Clicking "+ 新建状态列" creates a PropStatus column and binds Board Group.
		if err := tt.Click("+ 新建状态列"); err != nil {
			t.Fatalf("click + 新建状态列 failed: %v", err)
		}
		tt.Frame()

		av := state.ActiveView()
		if av == nil || av.Group == nil || av.Group.PropertyID == "" {
			t.Fatalf("expected Board view Group to bind newly created status property, got %+v", av)
		}
		statusProp, idx := st.Database().PropertyByID(av.Group.PropertyID)
		if idx < 0 || statusProp.Type != database.PropStatus {
			t.Fatalf("expected bound PropStatus, got %+v", statusProp)
		}
		for _, want := range []string{"待处理", "进行中", "已完成"} {
			if !tt.HasText(want) {
				t.Fatalf("Board after adding status column missing %q, texts=%v", want, tt.Texts())
			}
		}
	})

	t.Run("CalendarFallbackAddScheduleAndTimelineQuickSchedule", func(t *testing.T) {
		st := buildZeroPropStore(t)
		db := st.Database()
		if err := st.AddView(dbview.NewViewConfig(database.ViewCalendar, "日历", db), true); err != nil {
			t.Fatal(err)
		}
		if err := st.AddView(dbview.NewViewConfig(database.ViewTimeline, "甘特", db), false); err != nil {
			t.Fatal(err)
		}

		state, tt, _, _, _ := makeTesterWithOptions(t, st, nil)

		// Calendar renders records on ModTime (2026-11-23) with fallback notice.
		for _, want := range []string{"当前按「修改时间」展示", "+ 添加「排期」字段", "晨间随笔", "阅读摘录"} {
			if !tt.HasText(want) {
				t.Fatalf("Calendar fallback missing %q, texts=%v", want, tt.Texts())
			}
		}

		// Clicking "+ 添加「排期」字段" creates a PropDate column, binds Calendar, and auto-opens 未排期.
		if err := tt.Click("+ 添加「排期」字段"); err != nil {
			t.Fatalf("click + 添加「排期」字段 failed: %v", err)
		}
		tt.Frame()

		av := state.ActiveView()
		if av == nil || av.Calendar == nil || av.Calendar.DatePropID == "" {
			t.Fatalf("expected Calendar DatePropID bound after adding schedule prop, got %+v", av)
		}
		datePropID := av.Calendar.DatePropID
		dp, idx := st.Database().PropertyByID(datePropID)
		if idx < 0 || dp.Type != database.PropDate {
			t.Fatalf("expected PropDate for %q, got %+v", datePropID, dp)
		}
		if !state.CalendarUnscheduledOpen {
			t.Fatal("expected CalendarUnscheduledOpen to auto-open when all records lack dates")
		}

		// Switch to Timeline: undated rows display "+ 安排排期" quick action.
		if err := tt.Click("甘特"); err != nil {
			t.Fatalf("switch to Timeline failed: %v", err)
		}
		tt.Frame()
		if !tt.HasText("+ 安排排期") {
			t.Fatalf("expected + 安排排期 pill on undated Timeline rows, texts=%v", tt.Texts())
		}
		if err := tt.Click("安排排期: 晨间随笔"); err != nil {
			t.Fatalf("click quick schedule for 晨间随笔 failed: %v", err)
		}
		tt.Frame()

		rec := st.RecordByPath("极简笔记/note1.md")
		if rec == nil || rec.Props[datePropID].Date.Start != "2026-11-23" {
			t.Fatalf("expected note1.md scheduled on 2026-11-23, got %+v", rec)
		}
	})
}

func TestExplicitCalendarColorAndBoardMemberPriorityBindings(t *testing.T) {
	root := t.TempDir()
	db := database.NewDefault("显式绑定测试", database.SourceConfig{Type: database.SourceFolder})
	db.Properties = []database.Property{
		{ID: "prop_title", Key: "title", Name: "标题", Type: database.PropTitle, Width: 220},
		{
			ID: "prop_status", Key: "status", Name: "研发状态", Type: database.PropStatus, Width: 120,
			Options: []database.SelectOption{
				{ID: "s1", Name: "待办", Color: "gray", Group: database.StatusTodo},
				{ID: "s2", Name: "进行中", Color: "blue", Group: database.StatusInProgress},
			},
		},
		{
			ID: "prop_cat", Key: "category", Name: "业务线", Type: database.PropSelect, Width: 120,
			Options: []database.SelectOption{
				{ID: "c1", Name: "平台底座", Color: "purple"},
				{ID: "c2", Name: "增长实验", Color: "green"},
			},
		},
		{
			ID: "prop_pri", Key: "priority", Name: "优先级", Type: database.PropSelect, Width: 100,
			Options: []database.SelectOption{
				{ID: "p0", Name: "P0 紧急", Color: "red"},
				{ID: "p2", Name: "P2 常规", Color: "gray"},
			},
		},
		{ID: "prop_date", Key: "date", Name: "排期", Type: database.PropDate, Width: 130},
		{
			ID: "prop_team", Key: "team", Name: "协作成员", Type: database.PropMultiSelect, Width: 140,
			Options: []database.SelectOption{
				{ID: "m1", Name: "林晚", Color: "blue"},
				{ID: "m2", Name: "顾清", Color: "orange"},
			},
		},
	}
	showSidebar := true
	db.ActiveView = "view_cal"
	db.Views = []database.ViewConfig{
		{
			ID:   "view_cal",
			Name: "日历",
			Type: database.ViewCalendar,
			Calendar: &database.CalendarViewConfig{
				DatePropID:   "prop_date",
				ColorPropID:  "prop_status",
				Mode:         "month",
				ShowSidebar:  &showSidebar,
				ShowWeekends: true,
				WeekStart:    1,
			},
		},
		{
			ID:    "view_board",
			Name:  "看板",
			Type:  database.ViewBoard,
			Group: &database.GroupConfig{PropertyID: "prop_status", StatusBy: "option"},
			Properties: []database.ViewProperty{
				{PropertyID: "prop_date", Visible: true},
				{PropertyID: "prop_pri", Visible: false},
			},
			Board: &database.BoardViewConfig{
				CardSize:       "medium",
				ColorColumn:    true,
				ShowCount:      true,
				AssigneePropID: "prop_team",
				PriorityPropID: "prop_pri",
				DatePropID:     "prop_date",
			},
		},
	}

	rawDB, err := database.EncodeDatabaseMarkdown(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "显式绑定测试.db.md", rawDB); err != nil {
		t.Fatal(err)
	}
	writeRec := func(file, md string) {
		t.Helper()
		if err := vault.Write(root, filepath.ToSlash(filepath.Join("显式绑定测试", file)), md); err != nil {
			t.Fatal(err)
		}
	}
	writeRec("r1.md", "---\nstatus: 进行中\ncategory: 平台底座\npriority: P0 紧急\ndate: 2026-11-23\nteam:\n  - 林晚\n---\n\n# 核心引擎改造\n")
	writeRec("r2.md", "---\nstatus: 待办\ncategory: 增长实验\npriority: P2 常规\ndate: 2026-11-24\nteam:\n  - 顾清\n---\n\n# 落地页改版\n")

	st, err := database.Open(root, "显式绑定测试.db.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, tt, _, _, _ := makeTesterWithOptions(t, st, nil)

	// 1. Calendar initially colors/groups categories by prop_status ("进行中", "待办").
	for _, want := range []string{"研发状态", "进行中", "待办"} {
		if !tt.HasText(want) {
			t.Fatalf("Calendar with ColorPropID=prop_status missing %q, texts=%v", want, tt.Texts())
		}
	}

	// Switch Calendar ColorPropID to prop_cat ("业务线"): sidebar categories switch to "平台底座" / "增长实验".
	calVC := st.Database().Views[0].Clone()
	calVC.Calendar.ColorPropID = "prop_cat"
	if err := st.UpdateView(calVC); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"业务线", "平台底座", "增长实验"} {
		if !tt.HasText(want) {
			t.Fatalf("Calendar with ColorPropID=prop_cat missing %q, texts=%v", want, tt.Texts())
		}
	}

	// Switch Calendar ColorPropID to PropBindNone: sidebar category list disappears.
	calVC = st.Database().Views[0].Clone()
	calVC.Calendar.ColorPropID = database.PropBindNone
	if err := st.UpdateView(calVC); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("平台底座") {
		t.Fatalf("expected category sidebar entries hidden when ColorPropID=PropBindNone, texts=%v", tt.Texts())
	}

	// 2. Switch to Board: explicit AssigneePropID ("prop_team" multi_select) & PriorityPropID ("prop_pri").
	if err := tt.Click("看板"); err != nil {
		t.Fatalf("switch to Board failed: %v", err)
	}
	tt.Frame()

	for _, want := range []string{"协作成员", "P0 紧急", "核心引擎改造", "落地页改版"} {
		if !tt.HasText(want) {
			t.Fatalf("Board missing %q, texts=%v", want, tt.Texts())
		}
	}

	// Filter by multi_select member avatar "林晚".
	if err := tt.Click("筛选成员: 林晚"); err != nil {
		t.Fatalf("click member avatar 林晚 failed: %v", err)
	}
	tt.Frame()
	if !tt.HasText("核心引擎改造") || tt.HasText("落地页改版") {
		t.Fatalf("expected member filter 林晚 to keep 核心引擎改造 and hide 落地页改版, texts=%v", tt.Texts())
	}
	if err := tt.Click("清除筛选"); err != nil {
		t.Fatalf("clear member filter failed: %v", err)
	}
	tt.Frame()
	if !tt.HasText("落地页改版") {
		t.Fatalf("expected 落地页改版 restored after clearing filter, texts=%v", tt.Texts())
	}

	// Turn off PriorityPropID (PropBindNone): 3-bar priority badge ("P0 紧急") disappears.
	boardVC := st.Database().Views[1].Clone()
	boardVC.Board.PriorityPropID = database.PropBindNone
	if err := st.UpdateView(boardVC); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("P0 紧急") {
		t.Fatalf("expected priority badge hidden when PriorityPropID=PropBindNone, texts=%v", tt.Texts())
	}
}
