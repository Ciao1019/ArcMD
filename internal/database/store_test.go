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
