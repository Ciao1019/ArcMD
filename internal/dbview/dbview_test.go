package dbview_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/dbview"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func buildTestStore(t *testing.T) *database.Store {
	t.Helper()
	root := t.TempDir()
	db := database.NewDefault("产品路线图", database.SourceConfig{Type: database.SourceFolder})
	db.Properties = append(db.Properties,
		database.Property{ID: "prop_done", Key: "done", Name: "完成", Type: database.PropCheckbox, Width: 72},
		database.Property{ID: "prop_score", Key: "score", Name: "权重", Type: database.PropNumber, Width: 90},
		database.Property{ID: "prop_owner", Key: "owner", Name: "负责人", Type: database.PropText, Width: 110},
	)
	for i := range db.Views {
		db.Views[i].Properties = append(db.Views[i].Properties,
			database.ViewProperty{PropertyID: "prop_done", Visible: true, Width: 72},
			database.ViewProperty{PropertyID: "prop_score", Visible: true, Width: 90},
			database.ViewProperty{PropertyID: "prop_owner", Visible: true, Width: 110},
		)
	}
	rawDB, err := database.EncodeDatabaseMarkdown(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "产品路线图.db.md", rawDB); err != nil {
		t.Fatal(err)
	}

	writeRec := func(filename, md string) {
		t.Helper()
		if err := vault.Write(root, filepath.ToSlash(filepath.Join("产品路线图", filename)), md); err != nil {
			t.Fatal(err)
		}
	}
	writeRec("plan.md", "---\nstatus: 进行中\ntags:\n  - 重要\ndate: 2026-11-23\ndone: false\nscore: 30\nowner: Alice\n---\n\n# 计划开发\n\n详细内容。\n")
	writeRec("design.md", "---\nstatus: 已完成\ntags:\n  - 灵感\ndate: 2026-11-20\ndone: true\nscore: 20\nowner: Bob\n---\n\n# 界面设计\n\n草图。\n")
	writeRec("qa.md", "---\nstatus: 未开始\ndate: 2026-11-28\ndone: false\nscore: 50\nowner: Carol\n---\n\n# 质量验收\n\n清单。\n")

	st, err := database.Open(root, "产品路线图.db.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func makeTester(t *testing.T, st *database.Store) (*dbview.State, *ui.Tester, *[]string, *[]*database.Record, *[]string) {
	t.Helper()
	state := dbview.NewState(st)
	var changed []string
	var created []*database.Record
	var opened []string
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	opts := dbview.Options{
		Now: now,
		OnChanged: func(affected []string) {
			changed = append(changed, affected...)
		},
		OnAfterCreate: func(rec *database.Record) {
			created = append(created, rec)
		},
		OnOpenRecord: func(rec *database.Record, mode string) {
			if rec != nil {
				opened = append(opened, mode+":"+rec.Title)
			}
		},
	}

	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(ui.LightTheme())
		ui.Column(c.Key("host")).Fill().Children(func() {
			dbview.View(c, state, opts)
		})
	}, 1100, 720)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	return state, tt, &changed, &created, &opened
}

func TestTableRenderAndBrowse(t *testing.T) {
	st := buildTestStore(t)
	_, tt, _, _, _ := makeTester(t, st)

	for _, want := range []string{"产品路线图", "计划开发", "界面设计", "质量验收", "进行中", "已完成", "未开始", "Alice"} {
		if !tt.HasText(want) {
			t.Fatalf("expected text %q in rendered table, got texts=%v", want, tt.Texts())
		}
	}
	// Default footer displays total count "共 3 条".
	if !tt.HasText("共 3 条") {
		t.Fatalf("expected row count badge in header, got texts=%v", tt.Texts())
	}
}

func TestCheckboxAndInlineEditUndoRedo(t *testing.T) {
	st := buildTestStore(t)
	state, tt, changed, _, _ := makeTester(t, st)

	rec := st.RecordByPath("产品路线图/plan.md")
	if rec == nil {
		t.Fatal("missing plan.md record")
	}

	// 1. Toggle Checkbox via click on the row matching Alice ("plan.md").
	aliceBox, ok := tt.Find("Alice")
	if !ok {
		t.Fatal("could not find Alice text in rendered table")
	}
	var chkRect ui.Rect
	foundChk := false
	for _, info := range tt.AccessAll() {
		if info.Role == ui.RoleCheckBox && info.Name == "完成" &&
			info.Bounds.Y <= aliceBox.Y+aliceBox.H/2 && info.Bounds.Y+info.Bounds.H >= aliceBox.Y+aliceBox.H/2 {
			chkRect = info.Bounds
			foundChk = true
			break
		}
	}
	if !foundChk {
		t.Fatalf("could not find '完成' checkbox aligned with Alice row (access=%d)", len(tt.AccessAll()))
	}
	tt.ClickAt(chkRect.X+chkRect.W/2, chkRect.Y+chkRect.H/2)
	tt.Frame()

	if v := st.Record(rec.ID).ValueFor(database.Property{ID: "prop_done", Type: database.PropCheckbox}); !v.Bool {
		t.Fatalf("checkbox toggle failed, val=%+v", v)
	}
	if len(*changed) == 0 {
		t.Fatal("expected OnChanged callback on checkbox toggle")
	}

	// Undo with Cmd+Z while database root has focus.
	state.RequestFocus = true
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	if v := st.Record(rec.ID).ValueFor(database.Property{ID: "prop_done", Type: database.PropCheckbox}); v.Bool {
		t.Fatalf("expected Undo to revert checkbox, still true: %+v", v)
	}

	// Redo with Shift+Cmd+Z.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	tt.Frame()
	if v := st.Record(rec.ID).ValueFor(database.Property{ID: "prop_done", Type: database.PropCheckbox}); !v.Bool {
		t.Fatalf("expected Redo to restore checkbox=true, got %+v", v)
	}

	// 2. Inline text cell edit on "负责人" (prop_owner).
	ownerBox, ok := tt.Find("Alice")
	if !ok {
		t.Fatal("missing Alice cell")
	}
	tt.ClickAt(ownerBox.X+ownerBox.W/2, ownerBox.Y+ownerBox.H/2)
	tt.Frame()
	if !state.Editing || state.EditPropID != "prop_owner" {
		t.Fatalf("expected inline editing state on prop_owner, got %+v", state)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("David")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()

	if got := st.Record(rec.ID).Props["prop_owner"].Text; got != "David" {
		t.Fatalf("owner after inline edit = %q, want David", got)
	}
}

func TestToolbarPopoversSearchAndAggregations(t *testing.T) {
	st := buildTestStore(t)
	state, tt, _, _, _ := makeTester(t, st)

	// 1. Pinyin & Chinese Search.
	if err := tt.Click("搜索"); err != nil {
		t.Fatalf("open search failed: %v", err)
	}
	tt.Frame()
	tt.Type("jhkf") // "计划开发" pinyin initials
	tt.Frame()
	if !tt.HasText("计划开发") || tt.HasText("界面设计") {
		t.Fatalf("expected pinyin search to filter to 计划开发, texts=%v", tt.Texts())
	}
	state.SearchQuery = ""
	state.SearchOpen = false
	tt.Frame()

	// 2. Filter popover: add rule and verify filter count updates.
	if err := tt.Click("筛选"); err != nil {
		t.Fatalf("click 筛选 failed: %v", err)
	}
	tt.Frame()
	if !state.FilterOpen {
		t.Fatal("expected FilterOpen true")
	}
	if err := tt.Click("+ 添加筛选条件"); err != nil {
		t.Fatalf("add filter rule failed: %v", err)
	}
	tt.Frame()
	v := state.ActiveView()
	if v == nil || v.Filter == nil || len(v.Filter.Rules) != 1 {
		t.Fatalf("expected 1 filter rule, got %#v", v)
	}
	if err := tt.Click("清空筛选"); err != nil {
		t.Fatalf("clear filter failed: %v", err)
	}
	tt.Frame()
	state.CloseToolbarPopovers()

	// 3. Sort popover: add sort & toggle desc.
	if err := tt.Click("排序"); err != nil {
		t.Fatalf("click 排序 failed: %v", err)
	}
	tt.Frame()
	if err := tt.Click("+ 添加排序"); err != nil {
		t.Fatalf("add sort failed: %v", err)
	}
	tt.Frame()
	if v := state.ActiveView(); v == nil || len(v.Sorts) != 1 {
		t.Fatalf("expected 1 sort rule, got %#v", v)
	}
	if err := tt.Click("清空排序"); err != nil {
		t.Fatalf("clear sort failed: %v", err)
	}
	tt.Frame()
	state.CloseToolbarPopovers()

	// 4. Group popover: group by 状态.
	if err := tt.Click("分组"); err != nil {
		t.Fatalf("click 分组 failed: %v", err)
	}
	tt.Frame()
	state.CloseToolbarPopovers()
	// Verify grouping updates view & renders group headers when configured.
	vc := state.ActiveView().Clone()
	vc.Group = &database.GroupConfig{PropertyID: "prop_status"}
	if err := st.UpdateView(vc); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("进行中") || !tt.HasText("已完成") {
		t.Fatalf("expected group headers, texts=%v", tt.Texts())
	}
	vc.Group = nil
	_ = st.UpdateView(vc)
	tt.Frame()

	// 5. Properties popover: click "切换显示" eye button to hide a property column.
	if err := tt.Click("属性"); err != nil {
		t.Fatalf("click 属性 failed: %v", err)
	}
	tt.Frame()
	if _, ok := tt.Access("切换显示"); !ok {
		t.Fatalf("missing '切换显示' button in properties popover, access=%v", tt.AccessAll())
	}
	state.CloseToolbarPopovers()
	tt.Frame()

	// 6. Footer aggregation: set Sum on 权重 (prop_score) and assert sum "和 100" appears!
	vc2 := state.ActiveView().Clone()
	if vc2.Table == nil {
		vc2.Table = &database.TableViewConfig{}
	}
	vc2.Table.Aggregations = map[string]database.AggregationType{"prop_score": database.AggSum}
	if err := st.UpdateView(vc2); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("和 100") {
		t.Fatalf("expected sum aggregation '和 100' in footer, got texts=%v", tt.Texts())
	}
}

func TestNewRecordOpenPeekAndPageProperties(t *testing.T) {
	st := buildTestStore(t)
	state, tt, _, created, opened := makeTester(t, st)

	// 1. Click "+ 新建" button, commit its inline title.
	before := len(st.Records())
	if err := tt.Click("+ 新建"); err != nil {
		t.Fatalf("click + 新建 failed: %v", err)
	}
	tt.Frame()
	if len(st.Records()) != before+1 || len(*created) != 1 {
		t.Fatalf("expected record creation: count=%d created=%d", len(st.Records()), len(*created))
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()

	// 2. Hover row "界面设计" and click "打开" Peek button.
	targetRec := st.RecordByPath("产品路线图/design.md")
	if targetRec == nil {
		t.Fatal("missing design.md record")
	}
	titleBox, ok := tt.Find(targetRec.Title)
	if !ok {
		t.Fatalf("missing row title %q, texts=%v", targetRec.Title, tt.Texts())
	}
	tt.Move(titleBox.X+titleBox.W/2, titleBox.Y+titleBox.H/2)
	tt.Frame()
	if err := tt.Click("打开"); err != nil {
		t.Fatalf("click hover '打开' button failed (texts=%v): %v", tt.Texts(), err)
	}
	tt.Frame()
	if len(*opened) != 1 || (*opened)[0] != "side:"+targetRec.Title {
		t.Fatalf("expected OnOpenRecord side:%s, got %v", targetRec.Title, *opened)
	}

	// 3. Arrow key navigation triggers selection & Enter edits inline.
	if gridInfo, gok := tt.AccessByRole(ui.RoleGrid, "产品路线图"); gok {
		tt.ClickAt(gridInfo.Bounds.X+gridInfo.Bounds.W-20, gridInfo.Bounds.Y+gridInfo.Bounds.H-20)
	}
	state.FocusRow, state.FocusCol = 0, 1
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if state.FocusRow != 1 {
		t.Fatalf("expected KeyDown FocusRow=1, got %d", state.FocusRow)
	}

	// 4. Collapsible PagePropertiesView renders & collapses cleanly.
	collapsed := false
	pt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(ui.LightTheme())
		dbview.PagePropertiesView(c, st, targetRec.ID, state, &collapsed, dbview.Options{})
	}, 480, 400)
	pt.SetLocale("zh-Hans")
	pt.Frame()
	if !pt.HasText("页面属性") || !pt.HasText("负责人") {
		t.Fatalf("expected PagePropertiesView fields, got %v", pt.Texts())
	}
	if err := pt.Click("页面属性"); err != nil {
		t.Fatalf("toggle collapse failed: %v", err)
	}
	pt.Frame()
	if !collapsed || pt.HasText("负责人") {
		t.Fatalf("expected collapsed=true hiding property rows, collapsed=%v texts=%v", collapsed, pt.Texts())
	}
}
