package dbview_test

import (
	"testing"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/dbview"
	"github.com/egoist/mygo/ui"
)

func clickRole(t *testing.T, tt *ui.Tester, role ui.Role, name string) {
	t.Helper()
	info, ok := tt.AccessByRole(role, name)
	if !ok {
		t.Fatalf("missing element role=%v name=%q, access=%+v", role, name, tt.AccessAll())
	}
	if info.Bounds.Y+info.Bounds.H > 720 {
		tt.Scroll(580, 400, 0, info.Bounds.Y+info.Bounds.H-620)
		tt.Frame()
		info, _ = tt.AccessByRole(role, name)
	} else if info.Bounds.Y < 100 {
		tt.Scroll(580, 400, 0, info.Bounds.Y-160)
		tt.Frame()
		info, _ = tt.AccessByRole(role, name)
	}
	tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
	tt.Frame()
}

func TestChartKindsLayoutAndAggregation(t *testing.T) {
	st := buildTestStore(t)
	err := st.AddView(database.ViewConfig{
		ID:       "view_chart",
		Name:     "图表",
		Type:     database.ViewChart,
		OpenMode: dbview.OpenSidePeek,
		Chart: &database.ChartViewConfig{
			Kind:       "bar",
			XPropID:    "prop_status",
			YMode:      "prop",
			YPropID:    "prop_score",
			YAgg:       database.AggSum,
			ShowLegend: true,
			ShowValues: true,
		},
	}, true)
	if err != nil {
		t.Fatalf("AddView: %v", err)
	}

	state, tt, _, _, _ := makeTesterWithOptions(t, st, nil)

	if !tt.HasText("按「状态」统计 · 权重 · 求和") {
		t.Fatalf("expected chart header summary for status + score sum, texts=%v", tt.Texts())
	}
	if !tt.HasText("总计 100") {
		t.Fatalf("expected grand total 100 (30+20+50), texts=%v", tt.Texts())
	}

	// Switch across all 4 chart kinds cleanly.
	for _, tc := range []struct {
		label string
		kind  string
	}{
		{"条形图", "hbar"},
		{"折线图", "line"},
		{"环形图", "donut"},
		{"柱状图", "bar"},
	} {
		clickRole(t, tt, ui.RoleTab, tc.label)
		if got := state.ActiveView().Chart.Kind; got != tc.kind {
			t.Fatalf("expected chart kind %q, got %q", tc.kind, got)
		}
	}

	// Hover inside chart canvas to verify pointer tooltip inspection path without panic.
	tt.Move(480, 320)
	tt.Frame()
	if !tt.HasText("总计 100") {
		t.Fatal("expected chart total preserved on pointer hover")
	}

	// Switch Y-axis metric from score sum to record count via sub-toolbar menu button.
	clickRole(t, tt, ui.RoleMenuButton, "Y 轴计算: 权重")
	if err := tt.ChooseMenuItem("记录数"); err != nil {
		t.Fatalf("ChooseMenuItem 记录数 failed: %v", err)
	}
	tt.Frame()
	if !tt.HasText("总计 3") {
		t.Fatalf("expected grand total 3 after switching Y mode to count, texts=%v", tt.Texts())
	}

	// Toggle ShowLegend via Layout popover.
	if err := tt.Click("布局"); err != nil {
		t.Fatalf("open layout popover failed: %v", err)
	}
	tt.Frame()
	clickRole(t, tt, ui.RoleSwitch, "显示图例")
	if state.ActiveView().Chart.ShowLegend {
		t.Fatal("expected ShowLegend toggled off")
	}
}

func TestFeedViewRenderQuickComposeAndCompact(t *testing.T) {
	st := buildTestStore(t)
	if plan := st.RecordByPath("产品路线图/plan.md"); plan != nil {
		plan.Cover = "feed-cover.png"
		plan.Excerpt = "第一条动态的 Markdown 摘要文本"
	}
	if err := st.AddView(database.ViewConfig{
		ID:       "view_feed",
		Name:     "信息流",
		Type:     database.ViewFeed,
		OpenMode: dbview.OpenSidePeek,
		Feed: &database.FeedViewConfig{
			ShowExcerpt: true,
			ShowImage:   true,
		},
	}, true); err != nil {
		t.Fatalf("AddView: %v", err)
	}

	loadedImages := 0
	cover := makeTestCoverBitmap(320, 160)
	state, tt, _, afterCreate, _ := makeTesterWithOptions(t, st, func(recPath, imgURL string) *ui.Bitmap {
		loadedImages++
		return cover
	})

	if !tt.HasText("设计评审") && !tt.HasText("计划开发") {
		t.Fatalf("expected record title in feed stream, texts=%v", tt.Texts())
	}
	if !tt.HasText("第一条动态的 Markdown 摘要文本") {
		t.Fatalf("expected markdown excerpt in feed stream, texts=%v", tt.Texts())
	}
	if loadedImages == 0 {
		t.Fatal("expected LoadImage invoked for feed card cover")
	}

	// Quick compose a new post.
	clickRole(t, tt, ui.RoleTextField, "快速发布")
	tt.Type("动态新帖")
	tt.Frame()
	clickRole(t, tt, ui.RoleButton, "发布")

	if len(*afterCreate) != 1 || (*afterCreate)[0].Title != "动态新帖" {
		t.Fatalf("expected OnAfterCreate with 动态新帖, got %+v", *afterCreate)
	}
	if len(st.Records()) != 4 || !tt.HasText("动态新帖") {
		t.Fatalf("expected newly composed feed post in store and view, count=%d texts=%v", len(st.Records()), tt.Texts())
	}
	if state.FeedQuickDraft != "" {
		t.Fatalf("expected FeedQuickDraft cleared after submit, got %q", state.FeedQuickDraft)
	}

	// Toggle Compact mode in Layout popover.
	if err := tt.Click("布局"); err != nil {
		t.Fatalf("open layout popover failed: %v", err)
	}
	tt.Frame()
	clickRole(t, tt, ui.RoleSwitch, "紧凑模式")
	if !state.ActiveView().Feed.Compact {
		t.Fatal("expected Feed Compact toggled true")
	}
}

func TestFormViewValidationSubmitAndBuilder(t *testing.T) {
	st := buildTestStore(t)
	if err := st.AddView(database.ViewConfig{
		ID:       "view_form",
		Name:     "反馈表单",
		Type:     database.ViewForm,
		OpenMode: dbview.OpenSidePeek,
		Form: &database.FormViewConfig{
			Title:       "需求收集单",
			Description: "提交一条新需求记录",
			SubmitText:  "提交需求",
		},
	}, true); err != nil {
		t.Fatalf("AddView: %v", err)
	}

	state, tt, _, afterCreate, opened := makeTesterWithOptions(t, st, nil)

	if !tt.HasText("需求收集单") || !tt.HasText("提交一条新需求记录") {
		t.Fatalf("expected form title and description, texts=%v", tt.Texts())
	}

	// Empty submit triggers required validation error without creating record.
	clickRole(t, tt, ui.RoleButton, "提交需求")
	if !tt.HasText("此项为必填") || len(st.Records()) != 3 {
		t.Fatalf("expected required validation error and unchanged record count, count=%d texts=%v", len(st.Records()), tt.Texts())
	}

	// Fill title, select status, fill numeric weight and markdown body, then submit.
	clickRole(t, tt, ui.RoleTextField, "名称")
	tt.Type("表单新任务")
	tt.Frame()

	clickRole(t, tt, ui.RoleOption, "进行中")

	clickRole(t, tt, ui.RoleTextField, "权重")
	tt.Type("75")
	tt.Frame()

	clickRole(t, tt, ui.RoleTextField, "正文内容")
	tt.Type("表单正文详情")
	tt.Frame()

	clickRole(t, tt, ui.RoleButton, "提交需求")

	if !tt.HasText("已提交「表单新任务」") {
		t.Fatalf("expected confirmation banner after form submission, texts=%v", tt.Texts())
	}
	if len(*afterCreate) != 1 || len(st.Records()) != 4 {
		t.Fatalf("expected 4 records and 1 created callback, got %d / %d", len(st.Records()), len(*afterCreate))
	}
	created := (*afterCreate)[0]
	if created.Props["prop_status"].Text != "进行中" || created.Props["prop_score"].Number != 75 || created.Excerpt != "表单正文详情" {
		t.Fatalf("unexpected created record data: props=%+v excerpt=%q", created.Props, created.Excerpt)
	}

	// Click "查看记录" button from confirmation banner.
	clickRole(t, tt, ui.RoleButton, "查看记录")
	if len(*opened) != 1 || (*opened)[0] != dbview.OpenSidePeek+":"+created.Title {
		t.Fatalf("expected OnOpenRecord(%s), got %+v", created.Title, *opened)
	}

	// Switch to Form Builder ("编辑表单"): hide 负责人, require 权重, toggle ContinueOnSubmit.
	clickRole(t, tt, ui.RoleTab, "编辑表单")

	clickRole(t, tt, ui.RoleButton, "切换显示: 负责人")
	clickRole(t, tt, ui.RoleCheckBox, "必填: 权重")
	clickRole(t, tt, ui.RoleSwitch, "提交后继续填写")

	fcfg := state.ActiveView().Form
	if fcfg == nil || !fcfg.ContinueOnSubmit {
		t.Fatalf("expected ContinueOnSubmit enabled in Form config, got %+v", fcfg)
	}
	var ownerHidden, scoreReq bool
	for _, f := range fcfg.Fields {
		if f.PropertyID == "prop_owner" {
			ownerHidden = f.Hidden
		}
		if f.PropertyID == "prop_score" {
			scoreReq = f.Required
		}
	}
	if !ownerHidden || !scoreReq {
		t.Fatalf("expected prop_owner hidden (%v) and prop_score required (%v)", ownerHidden, scoreReq)
	}

	// Switch back to Fill Form: hidden field disappears; 清空 resets confirmation banner.
	clickRole(t, tt, ui.RoleTab, "填写表单")
	if _, found := tt.AccessByRole(ui.RoleTextField, "负责人"); found {
		t.Fatal("expected hidden 负责人 field not rendered in fill form")
	}
	clickRole(t, tt, ui.RoleButton, "清空")
	if tt.HasText("已提交「表单新任务」") {
		t.Fatal("expected confirmation banner cleared after 清空")
	}
}
