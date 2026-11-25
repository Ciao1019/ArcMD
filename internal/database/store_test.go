package database

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/vault"
)

func TestStoreFolderSpaceLiveAndMtimeCache(t *testing.T) {
	root := t.TempDir()

	db := NewDefault("Roadmap", SourceConfig{Type: SourceFolder, Recursive: true})
	db.Properties = append(db.Properties,
		Property{ID: "p_uid", Key: "uid", Name: "ID", Type: PropUniqueID, UniqueID: &UniqueIDConfig{Prefix: "ROAD", Next: 1}},
	)
	dbText, err := EncodeDatabaseMarkdownRelative("Projects", db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "Projects/Roadmap.db.md", dbText); err != nil {
		t.Fatal(err)
	}

	// Non-markdown files (.db.md, .pdf, .excalidraw.svg) and hidden dirs must be ignored.
	if err := vault.Write(root, "Projects/Roadmap/Alpha.md", "---\nstatus: 进行中\ncustom_note: keep-me\n---\n\n# Alpha Task\n\nBody text.\n"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "Projects/Roadmap/Sub/Beta.md", "---\nstatus: 未开始\n---\n\n# Beta Task\n"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "Projects/Roadmap/Diagram.excalidraw.svg", "<svg/>"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "Projects/Roadmap/.hidden.md", "# Hidden\n"); err != nil {
		t.Fatal(err)
	}

	st, err := Open(root, "Projects/Roadmap.db.md", nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if st.ResolveSourceDir() != "Projects/Roadmap" {
		t.Fatalf("ResolveSourceDir = %q, want Projects/Roadmap", st.ResolveSourceDir())
	}
	if len(st.Records()) != 2 {
		t.Fatalf("records = %d, want 2", len(st.Records()))
	}

	// Mtime cache verification: rewrite Alpha.md with identical length AND reset its mtime.
	alphaFull := filepath.Join(root, "Projects/Roadmap/Alpha.md")
	origStat, err := os.Stat(alphaFull)
	if err != nil {
		t.Fatal(err)
	}
	origBytes, err := os.ReadFile(alphaFull)
	if err != nil {
		t.Fatal(err)
	}
	tweaked := strings.Replace(string(origBytes), "进行中", "已完成", 1)
	if len(tweaked) != len(string(origBytes)) {
		t.Fatalf("test assumption: same byte len %d vs %d", len(tweaked), len(string(origBytes)))
	}
	if err := os.WriteFile(alphaFull, []byte(tweaked), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(alphaFull, origStat.ModTime(), origStat.ModTime()); err != nil {
		t.Fatal(err)
	}
	changed, err := st.Refresh(nil)
	if err != nil || changed {
		t.Fatalf("expected mtime cache hit (changed=false), got changed=%v err=%v", changed, err)
	}

	// LiveDoc overlay must update query results in memory without touching disk.
	liveText := strings.Replace(string(origBytes), "进行中", "已暂停", 1)
	changed, err = st.Refresh([]LiveDoc{{
		Path:      "Projects/Roadmap/Alpha.md",
		Text:      liveText,
		UpdatedAt: time.Now(),
	}})
	if err != nil || !changed {
		t.Fatalf("expected live overlay change, got changed=%v err=%v", changed, err)
	}
	alphaRec := st.RecordByPath("Projects/Roadmap/Alpha.md")
	if alphaRec.Props["prop_status"].Text != "已暂停" {
		t.Fatalf("live status = %q, want 已暂停", alphaRec.Props["prop_status"].Text)
	}
	// Disk file must remain untouched by Refresh.
	if onDisk, _ := os.ReadFile(alphaFull); string(onDisk) != tweaked {
		t.Fatalf("Refresh wrote to disk unexpectedly")
	}

	// Restore clean disk state before FlushRecord test.
	if err := os.WriteFile(alphaFull, origBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Refresh(nil); err != nil {
		t.Fatal(err)
	}

	// SetCell + FlushRecord preserves body and custom_note frontmatter.
	affected, err := st.SetCell("Projects/Roadmap/Alpha.md", "prop_status", TextValue("已完成"))
	if err != nil || !slices.Equal(affected, []string{"Projects/Roadmap/Alpha.md"}) {
		t.Fatalf("SetCell = %v, %v", affected, err)
	}
	if err := st.FlushRecord("Projects/Roadmap/Alpha.md"); err != nil {
		t.Fatalf("FlushRecord: %v", err)
	}
	flushed, err := os.ReadFile(alphaFull)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(flushed), "status: 已完成\n") ||
		!strings.Contains(string(flushed), "custom_note: keep-me\n") ||
		!strings.Contains(string(flushed), "# Alpha Task\n\nBody text.\n") {
		t.Fatalf("FlushRecord did not preserve frontmatter/body:\n%s", string(flushed))
	}

	// CreateRecord assigns UniqueID and creates .md file atomically.
	created, err := st.CreateRecord("Gamma", map[string]Value{"prop_status": TextValue("未开始")}, "Gamma notes")
	if err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	if created.Props["p_uid"].Text != "ROAD-1" {
		t.Fatalf("UniqueID = %q, want ROAD-1", created.Props["p_uid"].Text)
	}
	if created.Path != "Projects/Roadmap/Gamma.md" {
		t.Fatalf("created path = %q", created.Path)
	}

	// DeleteRecord in folder mode returns path without deleting disk file.
	backing, err := st.DeleteRecord(created.ID)
	if err != nil || backing != "Projects/Roadmap/Gamma.md" {
		t.Fatalf("DeleteRecord = %q, %v", backing, err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(backing))); err != nil {
		t.Fatalf("DeleteRecord unlinked file on disk: %v", err)
	}

	// Space source + TagFilter.
	spaceDB := NewDefault("AllNotes", SourceConfig{Type: SourceSpace, Recursive: true, TagFilter: []string{"#release"}})
	if err := vault.Write(root, "Inbox/Release Plan.md", "# Release\n\nTagged #release/v1 here.\n"); err != nil {
		t.Fatal(err)
	}
	spaceStore := NewStore(root, "AllNotes.db.md", spaceDB)
	if _, err := spaceStore.Refresh(nil); err != nil {
		t.Fatal(err)
	}
	if len(spaceStore.Records()) != 1 || spaceStore.Records()[0].Path != "Inbox/Release Plan.md" {
		t.Fatalf("TagFilter space records = %+v", spaceStore.Records())
	}
}

func TestStoreRenameRecordPath(t *testing.T) {
	db := NewDefault("Tasks", SourceConfig{Type: SourceFolder})
	db.Properties = append(db.Properties, Property{ID: "p_rel", Key: "rel", Name: "Rel", Type: PropRelation})
	db.Views[0].ManualOrder = []string{"Tasks/A.md", "Tasks/B.md"}

	st := NewStore("", "Tasks.db.md", db)
	recA := &Record{ID: "Tasks/A.md", Path: "Tasks/A.md", Title: "A", Props: map[string]Value{}}
	recB := &Record{ID: "Tasks/B.md", Path: "Tasks/B.md", Title: "B", Props: map[string]Value{"p_rel": ListValue("Tasks/A.md", "A")}}
	st.records = []*Record{recA, recB}
	st.byID = map[string]*Record{recA.ID: recA, recB.ID: recB}
	st.byPath = map[string]*Record{recA.Path: recA, recB.Path: recB}

	st.RenameRecordPath("Tasks/A.md", "Tasks/Alpha.md")
	if st.Record("Tasks/Alpha.md") == nil || st.Record("Tasks/A.md") != nil {
		t.Fatal("record ID/path map not updated")
	}
	if !slices.Equal(st.Database().Views[0].ManualOrder, []string{"Tasks/Alpha.md", "Tasks/B.md"}) {
		t.Fatalf("ManualOrder = %v", st.Database().Views[0].ManualOrder)
	}
	if got := recB.Props["p_rel"].List; !slices.Equal(got, []string{"Tasks/Alpha.md", "Alpha"}) {
		t.Fatalf("relation links = %v, want [Tasks/Alpha.md Alpha]", got)
	}
}

func TestStoreRandomizedUndoRedoInvariants(t *testing.T) {
	db := NewDefault("Invariants", SourceConfig{Type: SourceInline})
	db.Properties = append(db.Properties,
		Property{ID: "p_num", Key: "num", Name: "数值", Type: PropNumber},
		Property{ID: "p_chk", Key: "chk", Name: "完成", Type: PropCheckbox},
		Property{ID: "p_txt", Key: "txt", Name: "文本", Type: PropText},
	)
	for i := range 6 {
		db.InlineRows = append(db.InlineRows, &Record{
			ID:    fmt.Sprintf("row_%d", i),
			Title: fmt.Sprintf("Item %d", i),
			Props: map[string]Value{
				"prop_status": TextValue("未开始"),
				"p_num":       NumberValue(float64(i * 10)),
				"p_chk":       BoolValue(i%2 == 0),
				"p_txt":       TextValue(fmt.Sprintf("%d", i+1)),
			},
		})
	}

	st := NewStore("", "inv.db.md", db)
	initialDigest := st.Database().Digest()

	rng := rand.New(rand.NewPCG(42, 99))
	propCounter := 0
	viewCounter := 0

	// Perform 80 random reversible operations (within maxHistoryEntries = 100).
	for step := range 80 {
		op := rng.IntN(7)
		switch op {
		case 0: // SetCell
			recs := st.Records()
			if len(recs) == 0 {
				continue
			}
			r := recs[rng.IntN(len(recs))]
			curDB := st.Database()
			p := curDB.Properties[rng.IntN(len(curDB.Properties))]
			switch p.Type {
			case PropTitle, PropText:
				_, _ = st.SetCell(r.ID, p.ID, TextValue(fmt.Sprintf("v_%d", step)))
			case PropNumber:
				_, _ = st.SetCell(r.ID, p.ID, NumberValue(float64(rng.IntN(500))))
			case PropCheckbox:
				_, _ = st.SetCell(r.ID, p.ID, BoolValue(rng.IntN(2) == 1))
			case PropStatus:
				opts := []string{"未开始", "进行中", "已完成"}
				_, _ = st.SetCell(r.ID, p.ID, TextValue(opts[rng.IntN(len(opts))]))
			}

		case 1: // SetCellsBatch
			recs := st.Records()
			if len(recs) < 2 {
				continue
			}
			_, _ = st.SetCellsBatch([]CellEdit{
				{RecordID: recs[0].ID, PropID: "p_num", Value: NumberValue(float64(step))},
				{RecordID: recs[1].ID, PropID: "p_chk", Value: BoolValue(step%2 == 0)},
			})

		case 2: // AddProperty
			propCounter++
			pid := fmt.Sprintf("dyn_%d", propCounter)
			_ = st.AddProperty(Property{ID: pid, Key: pid, Name: pid, Type: PropText})

		case 3: // UpdateProperty (including type conversion!)
			curDB := st.Database()
			var candidates []Property
			for _, p := range curDB.Properties {
				if p.Type == PropText || p.Type == PropNumber {
					candidates = append(candidates, p)
				}
			}
			if len(candidates) == 0 {
				continue
			}
			p := candidates[rng.IntN(len(candidates))]
			if p.Type == PropText {
				p.Type = PropNumber
			} else {
				p.Type = PropText
			}
			_, _ = st.UpdateProperty(p)

		case 4: // DeleteProperty (only dynamic properties so core test columns stay busy)
			curDB := st.Database()
			for _, p := range curDB.Properties {
				if strings.HasPrefix(p.ID, "dyn_") {
					_, _ = st.DeleteProperty(p.ID)
					break
				}
			}

		case 5: // Add/Duplicate/ManualOrder views
			curDB := st.Database()
			if len(curDB.Views) < 5 {
				viewCounter++
				vid := fmt.Sprintf("v_dyn_%d", viewCounter)
				_ = st.AddView(ViewConfig{ID: vid, Name: vid, Type: ViewList}, false)
			} else {
				recs := st.Records()
				ids := make([]string, len(recs))
				for i, r := range recs {
					ids[i] = r.ID
				}
				rng.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
				_ = st.SetManualOrder(curDB.Views[0].ID, ids)
			}

		case 6: // Inline Create / Delete
			if rng.IntN(2) == 0 || len(st.Records()) < 3 {
				_, _ = st.CreateRecord(fmt.Sprintf("DynRow %d", step), nil, "")
			} else {
				recs := st.Records()
				_, _ = st.DeleteRecord(recs[rng.IntN(len(recs))].ID)
			}
		}
		if err := st.Database().Validate(); err != nil {
			t.Fatalf("step %d invalid database: %v", step, err)
		}
	}

	finalDigest := st.Database().Digest()

	undoCount := 0
	for st.History().CanUndo() {
		if _, ok := st.Undo(); !ok {
			t.Fatal("CanUndo true but Undo returned false")
		}
		undoCount++
		if err := st.Database().Validate(); err != nil {
			t.Fatalf("after undo %d invalid database: %v", undoCount, err)
		}
	}
	if undoCount == 0 {
		t.Fatal("expected at least one undo step")
	}
	if got := st.Database().Digest(); got != initialDigest {
		t.Fatalf("full undo did not restore exact initial database state")
	}

	for st.History().CanRedo() {
		if _, ok := st.Redo(); !ok {
			t.Fatal("CanRedo true but Redo returned false")
		}
	}
	if got := st.Database().Digest(); got != finalDigest {
		t.Fatalf("full redo did not restore exact final database state")
	}
}

func BenchmarkQuery10kRows(b *testing.B) {
	db := NewDefault("Bench", SourceConfig{Type: SourceInline})
	db.Properties = []Property{
		{ID: "p_title", Key: "title", Name: "名称", Type: PropTitle},
		{
			ID: "p_status", Key: "status", Name: "状态", Type: PropStatus,
			Options: []SelectOption{
				{ID: "s1", Name: "Todo", Group: StatusTodo},
				{ID: "s2", Name: "Doing", Group: StatusInProgress},
				{ID: "s3", Name: "Done", Group: StatusComplete},
			},
		},
		{ID: "p_num", Key: "num", Name: "分数", Type: PropNumber},
		{ID: "p_due", Key: "due", Name: "日期", Type: PropDate},
	}
	statuses := []string{"Todo", "Doing", "Done"}
	rows := make([]*Record, 10000)
	for i := range rows {
		rows[i] = &Record{
			ID:    fmt.Sprintf("r_%05d", i),
			Title: fmt.Sprintf("Task item %05d", i),
			Props: map[string]Value{
				"p_status": TextValue(statuses[i%3]),
				"p_num":    NumberValue(float64((i * 17) % 1000)),
				"p_due":    DateRangeValue(fmt.Sprintf("2026-%02d-%02d", (i%12)+1, (i%28)+1), "", false),
			},
		}
	}
	db.InlineRows = rows
	db.Views = []ViewConfig{
		{
			ID:   "v1",
			Name: "Table",
			Type: ViewTable,
			Filter: &FilterGroup{
				Conjunction: "and",
				Rules:       []FilterRule{{PropertyID: "p_num", Op: OpGTE, Value: NumberValue(200)}},
			},
			Sorts: []SortRule{
				{PropertyID: "p_status", Desc: false},
				{PropertyID: "p_num", Desc: true},
			},
			Group: &GroupConfig{PropertyID: "p_status"},
			Table: &TableViewConfig{
				Aggregations: map[string]AggregationType{
					"p_title": AggCountAll,
					"p_num":   AggSum,
				},
			},
		},
	}
	db.ActiveView = "v1"

	st := NewStore("", "bench.db.md", db)
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	b.Run("Uncached", func(b *testing.B) {
		adHoc := &FilterGroup{
			Conjunction: "and",
			Rules:       []FilterRule{{PropertyID: "p_num", Op: OpGTE, Value: NumberValue(200)}},
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			res := st.Query("v1", QueryOptions{Now: now, AdHocFilter: adHoc})
			if res.TotalCount == 0 {
				b.Fatal("unexpected empty result")
			}
		}
	})

	b.Run("Cached", func(b *testing.B) {
		_ = st.Query("v1", QueryOptions{Now: now}) // warm cache
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			res := st.Query("v1", QueryOptions{Now: now})
			if res.TotalCount == 0 {
				b.Fatal("unexpected empty result")
			}
		}
	})
}

func TestFlushRecordTitleSync(t *testing.T) {
	root := t.TempDir()
	db := NewDefault("Roadmap", SourceConfig{Type: SourceFolder})
	rawDB, err := EncodeDatabaseMarkdown(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "Roadmap.db.md", rawDB); err != nil {
		t.Fatal(err)
	}
	origMD := "---\ncustom: keep-me\nstatus: 未开始\n---\n\n# Alpha\n\nBody paragraph.\n"
	if err := vault.Write(root, "Roadmap/alpha.md", origMD); err != nil {
		t.Fatal(err)
	}

	st, err := Open(root, "Roadmap.db.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := st.RecordByPath("Roadmap/alpha.md")
	if rec == nil {
		t.Fatal("missing record Roadmap/alpha.md")
	}

	affected, err := st.SetCellsBatch([]CellEdit{
		{RecordID: rec.ID, PropID: "prop_title", Value: TextValue("Alpha Renamed")},
		{RecordID: rec.ID, PropID: "prop_status", Value: TextValue("进行中")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(affected) != 1 || affected[0] != "Roadmap/alpha.md" {
		t.Fatalf("affected = %v", affected)
	}
	if err := st.FlushRecord(rec.ID); err != nil {
		t.Fatal(err)
	}

	gotDisk, err := vault.Read(root, "Roadmap/alpha.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotDisk, "custom: keep-me") || !strings.Contains(gotDisk, "status: 进行中") ||
		!strings.Contains(gotDisk, "# Alpha Renamed") || !strings.Contains(gotDisk, "Body paragraph.") {
		t.Fatalf("unexpected flushed markdown:\n%s", gotDisk)
	}

	// Verify Refresh(nil) picks up the updated H1 cleanly, and Undo/Redo + FlushRecord round-trips.
	st2, err := Open(root, "Roadmap.db.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r2 := st2.RecordByPath("Roadmap/alpha.md"); r2 == nil || r2.Title != "Alpha Renamed" {
		t.Fatalf("reopened record title = %#v", r2)
	}
	if paths, ok := st.Undo(); !ok || len(paths) != 1 {
		t.Fatalf("Undo = (%v, %v)", paths, ok)
	}
	if err := st.FlushRecord(rec.ID); err != nil {
		t.Fatal(err)
	}
	undoneDisk, err := vault.Read(root, "Roadmap/alpha.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(undoneDisk, "# Alpha\n") || !strings.Contains(undoneDisk, "status: 未开始") {
		t.Fatalf("unexpected undone markdown:\n%s", undoneDisk)
	}
	if paths, ok := st.Redo(); !ok || len(paths) != 1 {
		t.Fatalf("Redo = (%v, %v)", paths, ok)
	}
	if err := st.FlushRecord(rec.ID); err != nil {
		t.Fatal(err)
	}
	redoneDisk, err := vault.Read(root, "Roadmap/alpha.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(redoneDisk, "# Alpha Renamed") {
		t.Fatalf("unexpected redone markdown:\n%s", redoneDisk)
	}
}

func TestPrimaryViewProtectionAndMultiViewSyncScope(t *testing.T) {
	db := NewDefault("研发排期", SourceConfig{Type: SourceInline})
	if len(db.Views) != 1 || db.Views[0].Type != ViewTable || !db.IsPrimaryView(db.Views[0].ID) {
		t.Fatalf("expected 1 protected primary table view by default, got %+v", db.Views)
	}
	primaryID := db.Views[0].ID

	st := NewStore("", "schedule.db.md", db)
	r1, err := st.CreateRecord("核心引擎", map[string]Value{
		"prop_status": TextValue("进行中"),
		"prop_date":   DateRangeValue("2026-11-20", "2026-11-26", false),
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := st.CreateRecord("收尾上线", map[string]Value{
		"prop_status": TextValue("已完成"),
		"prop_date":   DateRangeValue("2026-11-27", "2026-11-30", false),
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	// Add a derived Gantt view with its own filter and timeline settings.
	gantt := ViewConfig{
		ID:       "view_gantt",
		Name:     "甘特视图",
		Type:     ViewTimeline,
		OpenMode: "side",
		Filter: &FilterGroup{
			Conjunction: "and",
			Rules: []FilterRule{
				{ID: "f1", PropertyID: "prop_status", Op: OpEquals, Value: TextValue("进行中")},
			},
		},
		Timeline: &TimelineViewConfig{
			StartPropID: "prop_date",
			Zoom:        "week",
			ShowTable:   true,
			TableProps:  []string{"prop_status", "prop_date"},
		},
	}
	if err := st.AddView(gantt, true); err != nil {
		t.Fatal(err)
	}

	// 1. Primary table view cannot be deleted even when derived views exist.
	if err := st.DeleteView(primaryID); err == nil {
		t.Fatal("expected DeleteView on primary table view to fail")
	}
	// Duplicating primary view clears Primary on the copy, so the copy can be deleted.
	dup, err := st.DuplicateView(primaryID)
	if err != nil {
		t.Fatal(err)
	}
	if dup.Primary || st.Database().IsPrimaryView(dup.ID) {
		t.Fatalf("duplicated primary view should not be marked primary: %+v", dup)
	}
	if err := st.DeleteView(dup.ID); err != nil {
		t.Fatalf("DeleteView on duplicated view failed: %v", err)
	}

	// 2. View config isolation: Gantt filter hides r2 in Gantt, while Primary Table still shows both.
	gRes := st.Query("view_gantt", QueryOptions{})
	tRes := st.Query(primaryID, QueryOptions{})
	if gRes.TotalCount != 1 || gRes.Rows[0].ID != r1.ID {
		t.Fatalf("Gantt view filter mismatch: count=%d rows=%+v", gRes.TotalCount, gRes.Rows)
	}
	if tRes.TotalCount != 2 {
		t.Fatalf("Primary table view should remain unfiltered (2 rows), got %d", tRes.TotalCount)
	}

	// 3. Data-layer sync: editing r1's date (e.g. Gantt bar drag) syncs immediately to Table view.
	if _, err := st.SetCell(r1.ID, "prop_date", DateRangeValue("2026-11-22", "2026-11-29", false)); err != nil {
		t.Fatal(err)
	}
	tRes = st.Query(primaryID, QueryOptions{})
	var gotDate DateValue
	for _, row := range tRes.Rows {
		if row.ID == r1.ID {
			gotDate = row.Props["prop_date"].Date
		}
	}
	if gotDate.Start != "2026-11-22" || gotDate.End != "2026-11-29" {
		t.Fatalf("Gantt date edit did not sync to Primary Table view: %+v", gotDate)
	}
	_ = r2

	// 4. Schema-layer sync: adding a property while on Primary Table shows it in Primary Table
	//    without cluttering unrelated derived Gantt view's visible columns.
	_ = st.SetActiveView(primaryID)
	if err := st.AddProperty(Property{ID: "prop_note", Key: "note", Name: "备注", Type: PropText}); err != nil {
		t.Fatal(err)
	}
	cur := st.Database()
	pv, _ := cur.ViewByID(primaryID)
	gv, _ := cur.ViewByID("view_gantt")
	findVis := func(v *ViewConfig, pid string) (bool, bool) {
		for _, vp := range v.Properties {
			if vp.PropertyID == pid {
				return vp.Visible, true
			}
		}
		return false, false
	}
	if vis, ok := findVis(pv, "prop_note"); !ok || !vis {
		t.Fatalf("expected prop_note visible in primary table view, ok=%v vis=%v", ok, vis)
	}
	if vis, ok := findVis(gv, "prop_note"); !ok || vis {
		t.Fatalf("expected prop_note hidden in unrelated Gantt view, ok=%v vis=%v", ok, vis)
	}

	// 5. Property type change & deletion clean up Gantt bindings cleanly and undo losslessly.
	dateProp, _ := cur.PropertyByID("prop_date")
	textProp := dateProp.Clone()
	textProp.Type = PropText
	textProp.Date = nil
	if _, err := st.UpdateProperty(textProp); err != nil {
		t.Fatal(err)
	}
	gv, _ = st.Database().ViewByID("view_gantt")
	if gv.Timeline.StartPropID != "" {
		t.Fatalf("expected Gantt StartPropID cleared when date prop became text, got %q", gv.Timeline.StartPropID)
	}
	if _, ok := st.Undo(); !ok {
		t.Fatal("expected Undo of property type change to succeed")
	}
	gv, _ = st.Database().ViewByID("view_gantt")
	if gv.Timeline.StartPropID != "prop_date" {
		t.Fatalf("expected Undo to restore Gantt StartPropID, got %q", gv.Timeline.StartPropID)
	}

	if _, err := st.DeleteProperty("prop_status"); err != nil {
		t.Fatal(err)
	}
	gv, _ = st.Database().ViewByID("view_gantt")
	if gv.Filter != nil {
		t.Fatalf("expected Gantt filter referencing deleted prop_status to be pruned, got %+v", gv.Filter)
	}
	if slices.Contains(gv.Timeline.TableProps, "prop_status") {
		t.Fatalf("expected prop_status removed from Gantt TableProps, got %v", gv.Timeline.TableProps)
	}
}

func TestSetCellsAndManualOrderAtMaxHistory(t *testing.T) {
	db := NewDefault("HistoryCap", SourceConfig{Type: SourceInline})
	db.Properties = append(db.Properties, Property{ID: "p_num", Key: "num", Name: "数值", Type: PropNumber})
	for _, id := range []string{"r1", "r2", "r3"} {
		db.InlineRows = append(db.InlineRows, &Record{
			ID:    id,
			Title: strings.ToUpper(id),
			Props: map[string]Value{
				"prop_status": TextValue("未开始"),
				"p_num":       NumberValue(0),
			},
		})
	}
	viewID := db.Views[0].ID
	db.Views[0].ManualOrder = []string{"r1", "r2", "r3"}

	st := NewStore("", "cap.db.md", db)
	for i := 1; i <= maxHistoryEntries+5; i++ {
		if _, err := st.SetCell("r1", "p_num", NumberValue(float64(i))); err != nil {
			t.Fatalf("SetCell %d: %v", i, err)
		}
	}
	if st.History().Len() != maxHistoryEntries {
		t.Fatalf("expected history capped at %d, got %d", maxHistoryEntries, st.History().Len())
	}

	// 1. Combined cell edit + manual order when history is full.
	wantOrder1 := []string{"r2", "r3", "r1"}
	if _, err := st.SetCellsAndManualOrder(
		[]CellEdit{{RecordID: "r2", PropID: "prop_status", Value: TextValue("已完成")}},
		viewID,
		wantOrder1,
	); err != nil {
		t.Fatalf("SetCellsAndManualOrder: %v", err)
	}
	if got := st.Record("r2").Props["prop_status"].Text; got != "已完成" {
		t.Fatalf("status after SetCellsAndManualOrder = %q, want 已完成", got)
	}
	if got := st.SchemaSnapshot().Views[0].ManualOrder; !slices.Equal(got, wantOrder1) {
		t.Fatalf("ManualOrder = %v, want %v", got, wantOrder1)
	}

	if _, ok := st.Undo(); !ok {
		t.Fatal("expected Undo to succeed")
	}
	if got := st.Record("r2").Props["prop_status"].Text; got != "未开始" {
		t.Fatalf("status after Undo = %q, want 未开始", got)
	}
	if got := st.SchemaSnapshot().Views[0].ManualOrder; !slices.Equal(got, []string{"r1", "r2", "r3"}) {
		t.Fatalf("ManualOrder after Undo = %v, want [r1 r2 r3]", got)
	}

	if _, ok := st.Redo(); !ok {
		t.Fatal("expected Redo to succeed")
	}
	if got := st.Record("r2").Props["prop_status"].Text; got != "已完成" {
		t.Fatalf("status after Redo = %q, want 已完成", got)
	}
	if got := st.SchemaSnapshot().Views[0].ManualOrder; !slices.Equal(got, wantOrder1) {
		t.Fatalf("ManualOrder after Redo = %v, want %v", got, wantOrder1)
	}

	// 2. Identical cell value (no-op SetCellsBatch) + manual order change when history is full.
	wantOrder2 := []string{"r3", "r2", "r1"}
	if _, err := st.SetCellsAndManualOrder(
		[]CellEdit{{RecordID: "r2", PropID: "prop_status", Value: TextValue("已完成")}},
		viewID,
		wantOrder2,
	); err != nil {
		t.Fatalf("SetCellsAndManualOrder (no-op cell): %v", err)
	}
	if _, ok := st.Undo(); !ok {
		t.Fatal("expected Undo of manual-order-only step to succeed")
	}
	if got := st.SchemaSnapshot().Views[0].ManualOrder; !slices.Equal(got, wantOrder1) {
		t.Fatalf("ManualOrder after Undo of no-op cell step = %v, want %v", got, wantOrder1)
	}
	if _, ok := st.Redo(); !ok {
		t.Fatal("expected Redo of manual-order-only step to succeed")
	}
	if got := st.SchemaSnapshot().Views[0].ManualOrder; !slices.Equal(got, wantOrder2) {
		t.Fatalf("ManualOrder after Redo of no-op cell step = %v, want %v", got, wantOrder2)
	}
}

func TestSpliceManualOrderPreservesFilteredRecords(t *testing.T) {
	db := NewDefault("FilteredOrder", SourceConfig{Type: SourceInline})
	var initOrder []string
	for i := range 10 {
		id := fmt.Sprintf("row_%02d", i)
		initOrder = append(initOrder, id)
		status := "未开始"
		if i == 2 || i == 7 {
			status = "进行中"
		}
		db.InlineRows = append(db.InlineRows, &Record{
			ID:    id,
			Title: fmt.Sprintf("Task %02d", i),
			Props: map[string]Value{"prop_status": TextValue(status)},
		})
	}
	viewID := db.Views[0].ID
	db.Views[0].ManualOrder = slices.Clone(initOrder)
	db.Views[0].Filter = &FilterGroup{
		Conjunction: "and",
		Rules:       []FilterRule{{ID: "f1", PropertyID: "prop_status", Op: OpEquals, Value: TextValue("进行中")}},
	}

	st := NewStore("", "filtered.db.md", db)
	filtered := st.Query(viewID, QueryOptions{})
	if len(filtered.Rows) != 2 || filtered.Rows[0].ID != "row_02" || filtered.Rows[1].ID != "row_07" {
		t.Fatalf("expected 2 visible rows [row_02 row_07], got %+v", filtered.Rows)
	}

	// Reorder the 2 visible rows (move row_07 before row_02) while 8 rows are filtered out.
	curView, _ := st.SchemaSnapshot().ViewByID(viewID)
	nextOrder := SpliceManualOrder(curView.ManualOrder, st.Records(), "row_07", "row_02", "")
	if err := st.SetManualOrder(viewID, nextOrder); err != nil {
		t.Fatal(err)
	}

	wantFull := []string{
		"row_00", "row_01", "row_07", "row_02", "row_03",
		"row_04", "row_05", "row_06", "row_08", "row_09",
	}
	if got := st.SchemaSnapshot().Views[0].ManualOrder; !slices.Equal(got, wantFull) {
		t.Fatalf("ManualOrder = %v, want %v", got, wantFull)
	}

	// Clear filter and verify all 10 rows appear in exact preserved order.
	unfilteredView := curView.Clone()
	unfilteredView.ManualOrder = slices.Clone(nextOrder)
	unfilteredView.Filter = nil
	if err := st.UpdateView(unfilteredView); err != nil {
		t.Fatal(err)
	}
	allRes := st.Query(viewID, QueryOptions{})
	gotIDs := make([]string, len(allRes.Rows))
	for i, r := range allRes.Rows {
		gotIDs[i] = r.ID
	}
	if !slices.Equal(gotIDs, wantFull) {
		t.Fatalf("unfiltered query rows = %v, want %v", gotIDs, wantFull)
	}
}

func TestCloneSchemaAndSchemaSnapshotOmitInlineRows(t *testing.T) {
	db := NewDefault("SchemaSnap", SourceConfig{Type: SourceInline, TagFilter: []string{"#work"}})
	db.Icon = "📦"
	db.Cover = "assets/cover.jpg"
	db.Templates = []Template{{
		ID:    "tpl_1",
		Name:  "Bug",
		Props: map[string]Value{"prop_status": TextValue("未开始")},
	}}
	db.InlineRows = []*Record{{
		ID:    "r1",
		Title: "Inline Row",
		Props: map[string]Value{"prop_status": TextValue("进行中")},
	}}

	st := NewStore("", "snap.db.md", db)
	snap := st.SchemaSnapshot()
	if snap == nil {
		t.Fatal("SchemaSnapshot returned nil")
	}
	if snap.InlineRows != nil {
		t.Fatalf("expected SchemaSnapshot InlineRows == nil, got %+v", snap.InlineRows)
	}
	if snap.Title != "SchemaSnap" || snap.Icon != "📦" || snap.Cover != "assets/cover.jpg" ||
		snap.Version != CurrentVersion || snap.ActiveView != db.ActiveView || len(snap.Templates) != 1 {
		t.Fatalf("unexpected SchemaSnapshot metadata: %+v", snap)
	}

	// Mutating snapshot slices/maps must not affect Store state.
	snap.Properties[0].Name = "Mutated"
	snap.Views[0].Name = "MutatedView"
	snap.Templates[0].Props["prop_status"] = TextValue("Mutated")
	snap.Source.TagFilter[0] = "#mutated"

	fresh := st.Database()
	if fresh.Properties[0].Name == "Mutated" || fresh.Views[0].Name == "MutatedView" ||
		fresh.Templates[0].Props["prop_status"].Text == "Mutated" || fresh.Source.TagFilter[0] == "#mutated" {
		t.Fatal("mutating SchemaSnapshot leaked into Store")
	}
	if len(fresh.InlineRows) != 1 {
		t.Fatalf("expected Store.Database() to still include InlineRows, got %d", len(fresh.InlineRows))
	}
}
