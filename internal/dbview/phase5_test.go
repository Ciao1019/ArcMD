package dbview_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/dbview"
	"github.com/egoist/mygo/ui"
)

func makePhase5Tester(t *testing.T, st *database.Store) (*dbview.State, *ui.Tester, *[]string, *[]*database.Record, *[]string, *[]string) {
	t.Helper()
	state := dbview.NewState(st)
	var changed, opened, openedRel []*string
	_ = changed
	var chPaths []string
	var created []*database.Record
	var openRecs []string
	var openTargets []string
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	opts := dbview.Options{
		Now: now,
		ListSpaceDBs: func() []database.SpaceDBInfo {
			db := st.Database()
			if db == nil {
				return nil
			}
			return []database.SpaceDBInfo{{
				RelPath:    "产品路线图.db.md",
				ID:         db.ID,
				Title:      db.Title,
				Properties: db.Properties,
			}}
		},
		OnChanged: func(affected []string) {
			chPaths = append(chPaths, affected...)
		},
		OnAfterCreate: func(rec *database.Record) {
			created = append(created, rec)
		},
		OnOpenRecord: func(rec *database.Record, mode string) {
			if rec != nil {
				openRecs = append(openRecs, mode+":"+rec.Title)
			}
		},
		OnOpenRelationTarget: func(targetDB, ref string) {
			openTargets = append(openTargets, targetDB+"->"+ref)
		},
	}
	_ = opened
	_ = openedRel

	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(ui.LightTheme())
		ui.Column(c.Key("host")).Fill().Children(func() {
			dbview.View(c, state, opts)
		})
	}, 1220, 780)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	return state, tt, &chPaths, &created, &openRecs, &openTargets
}

func TestRelationPickerTwoWaySyncAndNavigation(t *testing.T) {
	st := buildTestStore(t)
	if err := st.AddProperty(database.Property{
		ID:       "p_rel",
		Key:      "rel",
		Name:     "关联",
		Type:     database.PropRelation,
		Relation: &database.RelationConfig{},
		Width:    160,
	}); err != nil {
		t.Fatal(err)
	}

	state, tt, _, _, _, openTargets := makePhase5Tester(t, st)

	// Open Properties -> Configure Relation ("关联"): enable 2-way sync and LimitOne.
	clickRole(t, tt, ui.RoleButton, "属性")
	clickRole(t, tt, ui.RoleButton, "配置属性: 关联")

	if !tt.HasText("双向关联") {
		t.Fatalf("expected relation config popover, texts=%v", tt.Texts())
	}
	clickRole(t, tt, ui.RoleTextField, "对端属性名称")
	tt.Type("反向关联")
	tt.Frame()
	clickRole(t, tt, ui.RoleButton, "启用双向关联")

	if !tt.HasText("已同步") {
		t.Fatalf("expected 已同步 badge after enabling two-way sync, texts=%v", tt.Texts())
	}
	pRel, _ := st.Database().PropertyByID("p_rel")
	if pRel.Relation == nil || pRel.Relation.SyncedPropID == "" {
		t.Fatalf("expected SyncedPropID populated, got %+v", pRel.Relation)
	}
	backPropID := pRel.Relation.SyncedPropID

	clickRole(t, tt, ui.RoleSwitch, "限制为单条关联")
	pRel, _ = st.Database().PropertyByID("p_rel")
	if !pRel.Relation.LimitOne {
		t.Fatal("expected LimitOne toggled true")
	}

	clickRole(t, tt, ui.RoleButton, "关闭")
	if state.EditPropOpen {
		t.Fatal("expected property config popover closed")
	}

	// Open cell popover for row "计划开发" on property "p_rel".
	plan := st.RecordByPath("产品路线图/plan.md")
	state.OpenCellPopover(plan, *pRel, time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
	tt.Frame()

	if !tt.HasText("尚未关联记录") {
		t.Fatalf("expected relation picker popover open, texts=%v", tt.Texts())
	}

	clickRole(t, tt, ui.RoleTextField, "搜索关联记录")
	tt.Type("界面")
	tt.Frame()

	clickRole(t, tt, ui.RoleOption, "界面设计")

	// LimitOne closed picker and linked "产品路线图/design", AND synced back link onto "界面设计".
	design := st.RecordByPath("产品路线图/design.md")
	backProp, _ := st.Database().PropertyByID(backPropID)
	if got := plan.ValueFor(*pRel).List; !slices.Equal(got, []string{"产品路线图/design"}) {
		t.Fatalf("expected plan.p_rel = [产品路线图/design], got %v", got)
	}
	if got := design.ValueFor(*backProp).List; !slices.Equal(got, []string{"产品路线图/plan"}) {
		t.Fatalf("expected symmetric back-link on design = [产品路线图/plan], got %v", got)
	}

	// Re-open relation picker on plan to test open-link navigation button.
	state.OpenCellPopover(plan, *pRel, time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
	tt.Frame()
	if !tt.HasText("界面设计") {
		t.Fatalf("expected linked chip 界面设计 in relation popover, texts=%v", tt.Texts())
	}
	clickRole(t, tt, ui.RoleButton, "打开: 产品路线图/design")
	if len(*openTargets) != 1 || !strings.HasSuffix((*openTargets)[0], "->产品路线图/design") {
		t.Fatalf("expected OnOpenRelationTarget callback for design, got %v", *openTargets)
	}
}

func TestRollupAndFormula2PropertyConfigAndLivePreview(t *testing.T) {
	st := buildTestStore(t)
	if err := st.AddProperty(database.Property{
		ID:       "p_deps",
		Key:      "deps",
		Name:     "前置",
		Type:     database.PropRelation,
		Relation: &database.RelationConfig{},
		Width:    130,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddProperty(database.Property{
		ID:     "p_roll",
		Key:    "rsum",
		Name:   "汇总",
		Type:   database.PropRollup,
		Rollup: &database.RollupConfig{RelationPropID: "p_deps", TargetPropID: "prop_score", Calculate: database.AggSum},
		Width:  110,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddProperty(database.Property{
		ID:      "p_form",
		Key:     "grade",
		Name:    "评级",
		Type:    database.PropFormula,
		Formula: &database.FormulaConfig{Expression: `prop("权重")`},
		Width:   110,
	}); err != nil {
		t.Fatal(err)
	}

	design := st.RecordByPath("产品路线图/design.md")
	plan := st.RecordByPath("产品路线图/plan.md")
	if _, err := st.SetCell(design.ID, "p_deps", database.ListValue("产品路线图/plan", "产品路线图/qa")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetCell(plan.ID, "p_deps", database.ListValue("产品路线图/design", "产品路线图/qa")); err != nil {
		t.Fatal(err)
	}

	state, tt, _, _, _, _ := makePhase5Tester(t, st)

	// Open Properties popover, scroll down inside props-scroll, and open Formula config for "评级".
	clickRole(t, tt, ui.RoleButton, "属性")
	if boxRect, ok := tt.Find("显示属性"); ok {
		tt.Scroll(boxRect.X+60, boxRect.Y+120, 0, 160)
		tt.Frame()
	}
	clickRole(t, tt, ui.RoleButton, "配置属性: 评级")

	if !tt.HasText("预览: 20") {
		t.Fatalf("expected initial live preview 20, texts=%v", tt.Texts())
	}

	clickRole(t, tt, ui.RoleTextField, "公式表达式")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type(`if(prop("汇总") >=`)
	tt.Frame()
	if !tt.HasText("语法错误") {
		t.Fatalf("expected live syntax error message, texts=%v", tt.Texts())
	}

	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type(`if(prop("汇总") >= 70, "达标", "未达标")`)
	tt.Frame()
	if !tt.HasText("预览: 达标") {
		t.Fatalf("expected live preview 达标 for sum 20+50=70, texts=%v", tt.Texts())
	}

	clickRole(t, tt, ui.RoleButton, "保存公式")
	if state.EditPropOpen {
		t.Fatal("expected config popover closed after saving formula")
	}

	// Verify computed text renders in table without leaking into Record.Props.
	tt.Frame()
	if !tt.HasText("达标") {
		t.Fatalf("expected computed cell text 达标 in table, texts=%v", tt.Texts())
	}
	if _, leaked := plan.Props["p_form"]; leaked {
		t.Fatal("computed Formula value leaked into Record.Props")
	}

	// Switch Rollup calculation to ShowOriginal via config popover.
	clickRole(t, tt, ui.RoleButton, "属性")
	if boxRect, ok := tt.Find("显示属性"); ok {
		tt.Scroll(boxRect.X+60, boxRect.Y+120, 0, 160)
		tt.Frame()
	}
	clickRole(t, tt, ui.RoleButton, "配置属性: 汇总")
	aggBtn, ok := tt.Access("求和")
	if !ok {
		t.Fatalf("missing rollup calc button, access=%+v", tt.AccessAll())
	}
	tt.ClickAt(aggBtn.Bounds.X+aggBtn.Bounds.W/2, aggBtn.Bounds.Y+aggBtn.Bounds.H/2)
	tt.Frame()
	if err := tt.ChooseMenuItem("显示原始值"); err != nil {
		t.Fatalf("ChooseMenuItem 显示原始值 failed: %v", err)
	}
	tt.Frame()
	pRoll, _ := st.Database().PropertyByID("p_roll")
	if pRoll.Rollup.Calculate != database.AggShowOriginal {
		t.Fatalf("expected AggShowOriginal persisted, got %q", pRoll.Rollup.Calculate)
	}
}

func TestConditionalColoringAndRecordTemplatesUI(t *testing.T) {
	st := buildTestStore(t)
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	state, tt, _, created, opened, _ := makePhase5Tester(t, st)

	// 1. Open Conditional Coloring popover, add a rule, and customize its value.
	clickRole(t, tt, ui.RoleButton, "条件着色")
	if !tt.HasText("条件着色规则") {
		t.Fatalf("expected color rules popover, texts=%v", tt.Texts())
	}
	clickRole(t, tt, ui.RoleButton, "+ 添加着色规则")

	view := state.ActiveView()
	if len(view.Colors) != 1 {
		t.Fatalf("expected 1 color rule added, got %+v", view.Colors)
	}
	res := st.Query("", database.QueryOptions{Now: now})
	if len(res.RowColors) == 0 {
		t.Fatalf("expected QueryResult.RowColors populated by color rule, rules=%+v", view.Colors)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()

	// 2. Open Record Templates split button -> create & edit template -> instantiate.
	clickRole(t, tt, ui.RoleButton, "模板菜单")
	if !tt.HasText("记录模板") {
		t.Fatalf("expected templates popover open, texts=%v", tt.Texts())
	}
	clickRole(t, tt, ui.RoleButton, "+ 新建模板")
	if !tt.HasText("编辑记录模板") {
		t.Fatalf("expected inline template editor, texts=%v", tt.Texts())
	}

	clickRole(t, tt, ui.RoleTextField, "图标")
	tt.Type("🚀")
	tt.Frame()

	clickRole(t, tt, ui.RoleTextField, "模板名称")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("周迭代 {{date}}")
	tt.Frame()

	clickRole(t, tt, ui.RoleTextField, "模板正文")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("## 冲刺\n标题={{title}}")
	tt.Frame()

	clickRole(t, tt, ui.RoleButton, "保存模板")

	// Instantiate the saved template from list.
	if err := tt.Click("周迭代 {{date}}"); err != nil {
		t.Fatalf("click template item failed: %v", err)
	}
	tt.Frame()

	if len(*created) != 1 {
		t.Fatalf("expected 1 record created from template, got %d", len(*created))
	}
	rec := (*created)[0]
	if rec.Title != "周迭代 2026-11-23" || rec.Icon != "🚀" || !strings.Contains(rec.Excerpt, "标题=周迭代 2026-11-23") {
		t.Fatalf("unexpected instantiated template record: title=%q icon=%q excerpt=%q", rec.Title, rec.Icon, rec.Excerpt)
	}
	if len(*opened) != 1 {
		t.Fatalf("expected OnOpenRecord called after template creation, got %v", *opened)
	}
}
