package dbview_test

import (
	"slices"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/dbview"
	"github.com/egoist/mygo/ui"
)

func TestCalendarMonthWeekNavigationAndDragReschedule(t *testing.T) {
	st := buildTestStore(t)
	db := st.Database()
	calView := database.ViewConfig{
		ID:         "view_cal",
		Name:       "日历",
		Type:       database.ViewCalendar,
		OpenMode:   dbview.OpenSidePeek,
		Properties: slices.Clone(db.Views[0].Properties),
		Calendar: &database.CalendarViewConfig{
			DatePropID:   "prop_date",
			Mode:         "month",
			ShowWeekends: true,
			WeekStart:    1,
		},
	}
	if err := st.AddView(calView, true); err != nil {
		t.Fatal(err)
	}

	state, tt, changed, _, _ := makeTesterWithOptions(t, st, nil)

	if !tt.HasText("2026 年 11 月") {
		t.Fatalf("expected header 2026 年 11 月, texts=%v", tt.Texts())
	}
	for _, want := range []string{"周一", "周日", "计划开发", "界面设计", "质量验收"} {
		if !tt.HasText(want) {
			t.Fatalf("expected %q in Calendar view, texts=%v", want, tt.Texts())
		}
	}

	// Drag 计划开发 (2026-11-23) to day "25" (2026-11-25).
	planBox, ok := tt.Find("计划开发")
	if !ok {
		t.Fatal("missing 计划开发 chip in Calendar")
	}
	day25Box, ok := tt.Find("25")
	if !ok {
		t.Fatal("missing day 25 label in Calendar")
	}
	dragBetween(tt, planBox.X+planBox.W/2, planBox.Y+planBox.H/2, day25Box.X+10, day25Box.Y+36)

	rec := st.RecordByPath("产品路线图/plan.md")
	if rec == nil || rec.Props["prop_date"].Date.Start != "2026-11-25" {
		t.Fatalf("expected plan.md rescheduled to 2026-11-25, got %+v", rec.Props["prop_date"])
	}
	if !slices.Contains(*changed, "产品路线图/plan.md") {
		t.Fatalf("expected OnChanged for plan.md, got %v", *changed)
	}

	// Undo with Cmd+Z restores 2026-11-23.
	state.RequestFocus = true
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	rec = st.RecordByPath("产品路线图/plan.md")
	if rec.Props["prop_date"].Date.Start != "2026-11-23" {
		t.Fatalf("expected Undo to restore 2026-11-23, got %+v", rec.Props["prop_date"])
	}

	// Switch to Week mode and test navigation Prev -> Today.
	if err := tt.Click("周"); err != nil {
		t.Fatalf("switch to Week mode failed: %v", err)
	}
	tt.Frame()
	if got := state.ActiveView().Calendar.Mode; got != "week" {
		t.Fatalf("Calendar.Mode = %q, want week", got)
	}
	if !tt.HasText("2026-11-23 – 11-29") {
		t.Fatalf("expected week header 2026-11-23 – 11-29, texts=%v", tt.Texts())
	}

	if err := tt.Click("下一周期"); err != nil {
		t.Fatalf("next week failed: %v", err)
	}
	tt.Frame()
	if !tt.HasText("2026-11-30 – 12-06") {
		t.Fatalf("expected next week header 2026-11-30 – 12-06, texts=%v", tt.Texts())
	}

	if err := tt.Click("今天"); err != nil {
		t.Fatalf("jump to today failed: %v", err)
	}
	tt.Frame()
	if !tt.HasText("2026-11-23 – 11-29") {
		t.Fatalf("expected jump back to today week 2026-11-23 – 11-29, texts=%v", tt.Texts())
	}
}

func TestCalendarUnscheduledDrawerAndDayCreate(t *testing.T) {
	st := buildTestStore(t)
	db := st.Database()
	qaRec := st.RecordByPath("产品路线图/qa.md")
	if _, err := st.SetCell(qaRec.ID, "prop_date", database.EmptyValue()); err != nil {
		t.Fatal(err)
	}
	calView := database.ViewConfig{
		ID:         "view_cal",
		Name:       "日历",
		Type:       database.ViewCalendar,
		OpenMode:   dbview.OpenSidePeek,
		Properties: slices.Clone(db.Views[0].Properties),
		Calendar: &database.CalendarViewConfig{
			DatePropID:   "prop_date",
			Mode:         "month",
			ShowWeekends: true,
			WeekStart:    1,
		},
	}
	if err := st.AddView(calView, true); err != nil {
		t.Fatal(err)
	}

	state, tt, _, created, _ := makeTesterWithOptions(t, st, nil)

	if err := tt.Click("未排期 (1)"); err != nil {
		t.Fatalf("open unscheduled drawer failed: %v", err)
	}
	tt.Frame()
	if !state.CalendarUnscheduledOpen || !tt.HasText("质量验收") {
		t.Fatalf("expected unscheduled drawer showing 质量验收, open=%v texts=%v", state.CalendarUnscheduledOpen, tt.Texts())
	}

	// Drag 质量验收 from Unscheduled drawer onto 2026-11-26 day cell.
	unBox, ok := tt.Find("质量验收")
	if !ok {
		t.Fatal("missing 质量验收 in unscheduled drawer")
	}
	day26Add, ok := tt.Access("在此日新建: 2026-11-26")
	if !ok {
		t.Fatal("missing 2026-11-26 cell")
	}
	dragBetween(tt, unBox.X+unBox.W/2, unBox.Y+unBox.H/2, day26Add.Bounds.X+30, day26Add.Bounds.Y+42)

	if got := st.Record(qaRec.ID).Props["prop_date"].Date.Start; got != "2026-11-26" {
		t.Fatalf("expected qa.md scheduled on 2026-11-26 after drawer drop, got %q", got)
	}
	if !tt.HasText("所有记录均已排期") {
		t.Fatalf("expected empty unscheduled drawer message, texts=%v", tt.Texts())
	}

	// Click "+" on day 2026-11-24 and verify date pre-fill + inline title entry.
	add24, ok := tt.Access("在此日新建: 2026-11-24")
	if !ok {
		t.Fatalf("missing add button for 2026-11-24, access=%v", tt.AccessAll())
	}
	tt.ClickAt(add24.Bounds.X+add24.Bounds.W/2, add24.Bounds.Y+add24.Bounds.H/2)
	tt.Frame()
	if len(*created) != 1 || (*created)[0].Props["prop_date"].Date.Start != "2026-11-24" {
		t.Fatalf("expected created record with date 2026-11-24, got %#v", *created)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("评审会议")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if st.Record((*created)[0].ID).Title != "评审会议" {
		t.Fatalf("expected inline calendar title '评审会议', got %q", st.Record((*created)[0].ID).Title)
	}

	// Toggle "显示周末" off in Layout popover.
	if err := tt.Click("布局"); err != nil {
		t.Fatalf("open layout popover failed: %v", err)
	}
	tt.Frame()
	sw, ok := tt.AccessByRole(ui.RoleSwitch, "显示周末")
	if !ok {
		t.Fatalf("missing 显示周末 switch, access=%v", tt.AccessAll())
	}
	tt.ClickAt(sw.Bounds.X+sw.Bounds.W/2, sw.Bounds.Y+sw.Bounds.H/2)
	tt.Frame()
	if state.ActiveView().Calendar.ShowWeekends {
		t.Fatal("expected ShowWeekends toggled to false")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if tt.HasText("周六") || tt.HasText("周日") {
		t.Fatalf("did not expect 周六/周日 columns when ShowWeekends=false, texts=%v", tt.Texts())
	}
}

func TestTimelineRenderZoomTableToggleAndBarDragResize(t *testing.T) {
	st := buildTestStore(t)
	db := st.Database()
	planRec := st.RecordByPath("产品路线图/plan.md")
	if _, err := st.SetCell(planRec.ID, "prop_date", database.DateRangeValue("2026-11-20", "2026-11-24", false)); err != nil {
		t.Fatal(err)
	}

	tlView := database.ViewConfig{
		ID:         "view_tl",
		Name:       "时间轴",
		Type:       database.ViewTimeline,
		OpenMode:   dbview.OpenSidePeek,
		Properties: slices.Clone(db.Views[0].Properties),
		Timeline: &database.TimelineViewConfig{
			StartPropID: "prop_date",
			Zoom:        "week", // 32px per day
			ShowTable:   true,
		},
	}
	if err := st.AddView(tlView, true); err != nil {
		t.Fatal(err)
	}

	state, tt, _, _, _ := makeTesterWithOptions(t, st, nil)
	tt.Advance(20 * time.Millisecond)
	tt.Frame()

	for _, want := range []string{"名称", "计划开发", "界面设计", "质量验收"} {
		if !tt.HasText(want) {
			t.Fatalf("expected %q in Timeline view, texts=%v", want, tt.Texts())
		}
	}

	// Switch Zoom to Month and back to Week.
	zoomBtn, ok := tt.Access("缩放粒度: 周")
	if !ok {
		t.Fatalf("missing zoom selector button, access=%v", tt.AccessAll())
	}
	tt.ClickAt(zoomBtn.Bounds.X+zoomBtn.Bounds.W/2, zoomBtn.Bounds.Y+zoomBtn.Bounds.H/2)
	tt.Frame()
	if err := tt.ChooseMenuItem("月"); err != nil {
		t.Fatalf("choose 月 zoom failed: %v", err)
	}
	tt.Frame()
	if got := state.ActiveView().Timeline.Zoom; got != "month" {
		t.Fatalf("Timeline.Zoom = %q, want month", got)
	}
	zoomBtn2, ok := tt.Access("缩放粒度: 月")
	if !ok {
		t.Fatal("missing zoom selector button for 月")
	}
	tt.ClickAt(zoomBtn2.Bounds.X+zoomBtn2.Bounds.W/2, zoomBtn2.Bounds.Y+zoomBtn2.Bounds.H/2)
	tt.Frame()
	if err := tt.ChooseMenuItem("周"); err != nil {
		t.Fatalf("choose 周 zoom failed: %v", err)
	}
	tt.Frame()

	// Locate 计划开发's Gantt bar (to the right of left table X > 260, using tlbody / bar label).
	var barInfo ui.AccessInfo
	foundBar := false
	for _, info := range tt.AccessAll() {
		if info.Role == ui.RoleListItem && info.Name == "计划开发" && info.Bounds.X > 250 {
			barInfo = info
			foundBar = true
			break
		}
	}
	if !foundBar {
		t.Fatalf("could not find 计划开发 Gantt bar, access=%v", tt.AccessAll())
	}

	// Drag bar center right by +64px (+2 days at week zoom 32px/day).
	bx := barInfo.Bounds.X + barInfo.Bounds.W/2
	by := barInfo.Bounds.Y + barInfo.Bounds.H/2
	dragBetween(tt, bx, by, bx+64, by)

	d := st.Record(planRec.ID).Props["prop_date"].Date
	if d.Start != "2026-11-22" || d.End != "2026-11-26" {
		t.Fatalf("expected bar move +2d to 2026-11-22..2026-11-26, got %+v", d)
	}

	// Undo with Cmd+Z restores both start & end atomically.
	state.RequestFocus = true
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	d = st.Record(planRec.ID).Props["prop_date"].Date
	if d.Start != "2026-11-20" || d.End != "2026-11-24" {
		t.Fatalf("expected Undo to restore 2026-11-20..2026-11-24, got %+v", d)
	}

	// Resize right grip by +96px (+3 days) -> 2026-11-20..2026-11-27.
	gripR, ok := tt.Access("调整结束日期: 计划开发")
	if !ok {
		t.Fatalf("missing right resize grip, access=%v", tt.AccessAll())
	}
	gx := gripR.Bounds.X + gripR.Bounds.W/2
	gy := gripR.Bounds.Y + gripR.Bounds.H/2
	dragBetween(tt, gx, gy, gx+96, gy)

	d = st.Record(planRec.ID).Props["prop_date"].Date
	if d.Start != "2026-11-20" || d.End != "2026-11-27" {
		t.Fatalf("expected right-edge resize +3d to 2026-11-20..2026-11-27, got %+v", d)
	}

	// Toggle "显示表格" off and verify left header hides cleanly.
	if err := tt.Click("显示表格"); err != nil {
		t.Fatalf("toggle 显示表格 failed: %v", err)
	}
	tt.Frame()
	if state.ActiveView().Timeline.ShowTable || tt.HasText("名称") {
		t.Fatalf("expected ShowTable=false hiding left 名称 column, show=%v texts=%v",
			state.ActiveView().Timeline.ShowTable, tt.Texts())
	}
}
