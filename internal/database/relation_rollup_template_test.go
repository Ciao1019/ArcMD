package database

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/vault"
)

func TestRelationSyncRollupFormulaColorsAndTemplates(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 23, 9, 30, 0, 0, time.UTC)

	// 1. Seed Tasks DB.
	tasksDB := NewDefault("任务", SourceConfig{Type: SourceFolder})
	tasksDB.Properties = append(tasksDB.Properties,
		Property{ID: "t_score", Key: "score", Name: "权重", Type: PropNumber},
		Property{ID: "t_done", Key: "done", Name: "完成", Type: PropCheckbox},
	)
	for i := range tasksDB.Views {
		tasksDB.Views[i].Properties = append(tasksDB.Views[i].Properties,
			ViewProperty{PropertyID: "t_score", Visible: true},
			ViewProperty{PropertyID: "t_done", Visible: true},
		)
	}
	rawTasks, err := EncodeDatabaseMarkdown(tasksDB, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "任务.db.md", rawTasks); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "任务/ui.md", "---\nstatus: 进行中\ndate: 2026-11-24\nscore: 40\ndone: true\n---\n\n# 界面设计\n"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "任务/api.md", "---\nstatus: 未开始\ndate: 2026-11-28\nscore: 60\ndone: false\n---\n\n# 接口联调\n"); err != nil {
		t.Fatal(err)
	}

	// 2. Seed Projects DB pointing to Tasks DB.
	projDB := NewDefault("项目", SourceConfig{Type: SourceFolder})
	projDB.Properties = append(projDB.Properties,
		Property{
			ID:       "p_tasks",
			Key:      "tasks",
			Name:     "关联任务",
			Type:     PropRelation,
			Relation: &RelationConfig{TargetDB: "任务.db.md"},
		},
	)
	rawProj, err := EncodeDatabaseMarkdown(projDB, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "项目.db.md", rawProj); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "项目/v2.md", "---\nstatus: 进行中\n---\n\n# 核心版本 V2\n"); err != nil {
		t.Fatal(err)
	}

	dbs, err := ListSpaceDatabases(root)
	if err != nil || len(dbs) != 2 {
		t.Fatalf("ListSpaceDatabases => len=%d err=%v", len(dbs), err)
	}

	projStore, err := Open(root, "项目.db.md", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 3. Configure two-way relation + Rollup + Formula + Conditional Colors.
	syncedID, err := projStore.SetupTwoWayRelation("p_tasks", "任务.db.md", "所属项目")
	if err != nil {
		t.Fatalf("SetupTwoWayRelation: %v", err)
	}
	if syncedID == "" {
		t.Fatal("expected non-empty synced property id on Tasks.db.md")
	}

	// Check candidate discovery.
	pRel, _ := projStore.Database().PropertyByID("p_tasks")
	cands := projStore.RelationCandidates(*pRel)
	if len(cands) != 2 {
		t.Fatalf("expected 2 relation candidates from Tasks, got %+v", cands)
	}

	// Add Rollup sum, Rollup checked%, Rollup original, and Formula 2.0 using relation map + rollup.
	for _, rp := range []Property{
		{
			ID:     "p_sum",
			Key:    "task_score_sum",
			Name:   "总权重",
			Type:   PropRollup,
			Rollup: &RollupConfig{RelationPropID: "p_tasks", TargetPropID: "t_score", Calculate: AggSum},
		},
		{
			ID:     "p_pct",
			Key:    "done_pct",
			Name:   "完成率",
			Type:   PropRollup,
			Rollup: &RollupConfig{RelationPropID: "p_tasks", TargetPropID: "t_done", Calculate: AggPercentChecked},
		},
		{
			ID:     "p_orig",
			Key:    "task_titles",
			Name:   "任务列表",
			Type:   PropRollup,
			Rollup: &RollupConfig{RelationPropID: "p_tasks", TargetPropID: "prop_title", Calculate: AggShowOriginal},
		},
		{
			ID:      "p_badge",
			Key:     "badge",
			Name:    "等级",
			Type:    PropFormula,
			Formula: &FormulaConfig{Expression: `if(prop("总权重") >= 100 && prop("关联任务").map(current.prop("权重")).sum() == 100, "重点", "普通")`},
		},
	} {
		if err := projStore.AddProperty(rp); err != nil {
			t.Fatalf("AddProperty(%s): %v", rp.ID, err)
		}
	}

	// Add conditional coloring rule matching computed Formula column p_badge == "重点".
	vc := projStore.Database().Views[0].Clone()
	vc.Colors = []ColorRule{
		{
			ID:    "cr_hot",
			Color: "red",
			Filter: FilterGroup{
				Conjunction: "and",
				Rules: []FilterRule{
					{ID: "r1", PropertyID: "p_badge", Op: OpEquals, Value: TextValue("重点")},
				},
			},
		},
	}
	if err := projStore.UpdateView(vc); err != nil {
		t.Fatal(err)
	}

	// Link both Tasks to 项目/v2 and verify symmetric reverse link persisted on Tasks markdown files!
	v2 := projStore.RecordByPath("项目/v2.md")
	if _, err := projStore.SetCell(v2.ID, "p_tasks", ListValue("任务/ui", "任务/api")); err != nil {
		t.Fatalf("SetCell relation: %v", err)
	}
	if err := projStore.FlushRecord(v2.ID); err != nil {
		t.Fatal(err)
	}

	uiRaw, err := vault.Read(root, "任务/ui.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(uiRaw, "[[项目/v2]]") {
		t.Fatalf("expected symmetric [[项目/v2]] link synced in 任务/ui.md:\n%s", uiRaw)
	}
	apiRaw, err := vault.Read(root, "任务/api.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(apiRaw, "[[项目/v2]]") {
		t.Fatalf("expected symmetric [[项目/v2]] link synced in 任务/api.md:\n%s", apiRaw)
	}

	// Query and verify computed values + RowColors, AND verify Computed is never persisted in Props/frontmatter.
	res := projStore.Query("", QueryOptions{Now: now})
	if len(res.Rows) != 1 {
		t.Fatalf("expected 1 project row, got %d", len(res.Rows))
	}
	row := res.Rows[0]
	dbNow := projStore.Database()
	getProp := func(id string) Property {
		p, _ := dbNow.PropertyByID(id)
		return *p
	}
	if v := row.ValueFor(getProp("p_sum")); v.Kind != ValNumber || v.Number != 100 {
		t.Fatalf("p_sum = %+v, want 100", v)
	}
	if s := FormatValue(getProp("p_pct"), row.ValueFor(getProp("p_pct"))); s != "50%" {
		t.Fatalf("p_pct formatted = %q, want 50%%", s)
	}
	if v := row.ValueFor(getProp("p_orig")); v.Kind != ValStringList || !slices.Equal(v.List, []string{"界面设计", "接口联调"}) {
		t.Fatalf("p_orig = %+v, want [界面设计 接口联调]", v)
	}
	if v := row.ValueFor(getProp("p_badge")); v.Kind != ValText || v.Text != "重点" {
		t.Fatalf("p_badge formula = %+v, want 重点", v)
	}
	if res.RowColors[row.ID] != "red" {
		t.Fatalf("expected RowColors[%s] = red, got %+v", row.ID, res.RowColors)
	}
	if _, leaked := row.Props["p_sum"]; leaked {
		t.Fatal("computed Rollup value leaked into Record.Props")
	}
	v2Disk, _ := vault.Read(root, "项目/v2.md")
	if strings.Contains(v2Disk, "task_score_sum") || strings.Contains(v2Disk, "badge:") {
		t.Fatalf("computed properties leaked into markdown frontmatter:\n%s", v2Disk)
	}

	// Verify removing one relation updates symmetric Tasks/api.md and recomputes totals.
	if _, err := projStore.SetCell(v2.ID, "p_tasks", ListValue("任务/ui")); err != nil {
		t.Fatal(err)
	}
	apiAfter, _ := vault.Read(root, "任务/api.md")
	if strings.Contains(apiAfter, "[[项目/v2]]") {
		t.Fatalf("expected symmetric link removed from 任务/api.md:\n%s", apiAfter)
	}
	res2 := projStore.Query("", QueryOptions{Now: now})
	if v := res2.Rows[0].ValueFor(getProp("p_sum")); v.Number != 40 {
		t.Fatalf("expected recomputed p_sum = 40 after unlinking api, got %+v", v)
	}
	if v := res2.Rows[0].ValueFor(getProp("p_badge")); v.Text != "普通" {
		t.Fatalf("expected recomputed p_badge = 普通, got %+v", v)
	}
	if len(res2.RowColors) != 0 {
		t.Fatalf("expected no RowColors match after formula change, got %+v", res2.RowColors)
	}

	// 4. Test Self-Relation atomic Undo/Redo inside Tasks Store.
	tasksStore, err := Open(root, "任务.db.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tasksStore.AddProperty(Property{
		ID:       "t_blocks",
		Key:      "blocks",
		Name:     "阻塞任务",
		Type:     PropRelation,
		Relation: &RelationConfig{TargetDB: "任务.db.md"},
	}); err != nil {
		t.Fatal(err)
	}
	backSelfID, err := tasksStore.SetupTwoWayRelation("t_blocks", "", "前置依赖")
	if err != nil {
		t.Fatalf("self SetupTwoWayRelation: %v", err)
	}
	uiRec := tasksStore.RecordByPath("任务/ui.md")
	apiRec := tasksStore.RecordByPath("任务/api.md")
	affectedSelf, err := tasksStore.SetCell(uiRec.ID, "t_blocks", ListValue("任务/api"))
	if err != nil {
		t.Fatal(err)
	}
	if len(affectedSelf) != 2 {
		t.Fatalf("expected 2 affected paths on atomic self 2-way relation, got %v", affectedSelf)
	}
	backProp, _ := tasksStore.Database().PropertyByID(backSelfID)
	if got := apiRec.ValueFor(*backProp).List; !slices.Equal(got, []string{"任务/ui"}) {
		t.Fatalf("expected symmetric self link [任务/ui] on apiRec, got %v", got)
	}
	undoPaths, ok := tasksStore.Undo()
	if !ok || len(undoPaths) != 2 {
		t.Fatalf("expected undo returning 2 paths, got ok=%v paths=%v", ok, undoPaths)
	}
	if got := apiRec.ValueFor(*backProp).List; len(got) != 0 {
		t.Fatalf("expected symmetric self link cleared by Undo, got %v", got)
	}

	// 5. Test Record Templates & placeholder instantiation.
	tpl := Template{
		ID:   "tpl_sprint",
		Name: "周会纪要 {{date}}",
		Icon: "🚀",
		Props: map[string]Value{
			"prop_status": TextValue("进行中"),
			"prop_date":   TextValue("today"),
			"t_score":     NumberValue(55),
		},
		Body: "## 议题 ({{weekday}})\n标题: {{title}} @ {{time}}\n",
	}
	if err := tasksStore.AddTemplate(tpl); err != nil {
		t.Fatal(err)
	}
	created, err := tasksStore.CreateRecordFromTemplate("tpl_sprint", "", nil, now)
	if err != nil {
		t.Fatalf("CreateRecordFromTemplate: %v", err)
	}
	if created.Title != "周会纪要 2026-11-23" || created.Icon != "🚀" {
		t.Fatalf("unexpected template record metadata: title=%q icon=%q", created.Title, created.Icon)
	}
	createdMD, err := vault.Read(root, created.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"icon: 🚀", "status: 进行中", "date: 2026-11-23", "score: 55", "# 周会纪要 2026-11-23", "## 议题 (星期一)", "标题: 周会纪要 2026-11-23 @ 09:30"} {
		if !strings.Contains(createdMD, want) {
			t.Fatalf("created template record missing %q:\n%s", want, createdMD)
		}
	}
}
