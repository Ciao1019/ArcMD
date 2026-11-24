package dbview

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func buildMotionTestStore(t *testing.T) *database.Store {
	t.Helper()
	root := t.TempDir()
	db := database.NewDefault("动效测试库", database.SourceConfig{Type: database.SourceFolder})
	db.Views = append(db.Views, database.ViewConfig{
		ID:    "view_board",
		Name:  "看板",
		Type:  database.ViewBoard,
		Group: &database.GroupConfig{PropertyID: "prop_status", StatusBy: "option"},
		Board: &database.BoardViewConfig{CardSize: "medium", ColorColumn: true, ShowCount: true},
	})
	raw, err := database.EncodeDatabaseMarkdown(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "动效测试库.db.md", raw); err != nil {
		t.Fatal(err)
	}
	writeRec := func(name, md string) {
		t.Helper()
		if err := vault.Write(root, filepath.ToSlash(filepath.Join("动效测试库", name)), md); err != nil {
			t.Fatal(err)
		}
	}
	writeRec("a.md", "---\nstatus: 未开始\n---\n\n# 需求 A\n")
	writeRec("b.md", "---\nstatus: 进行中\n---\n\n# 需求 B\n")
	writeRec("c.md", "---\nstatus: 已完成\n---\n\n# 需求 C\n")

	st, err := database.Open(root, "动效测试库.db.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestMotionCreationPulseAndCleanup(t *testing.T) {
	store := buildMotionTestStore(t)
	state := NewState(store)
	var created []*database.Record
	opts := Options{
		Now: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC),
		OnAfterCreate: func(rec *database.Record) {
			created = append(created, rec)
		},
	}

	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(ui.LightTheme())
		ui.Column(c.Key("host")).Fill().Children(func() {
			View(c, state, opts)
		})
	}, 1100, 720)
	tt.SetLocale("zh-Hans")
	tt.Frame()

	// Click unified "+ 新建" button in header.
	if err := tt.Click("+ 新建"); err != nil {
		t.Fatalf("click + 新建 failed: %v", err)
	}
	tt.Frame()

	if len(created) != 1 {
		t.Fatalf("expected 1 created record, got %d", len(created))
	}
	newID := created[0].ID
	if !state.Motion.IsRecentlyCreated(newID, tt.Now()) {
		t.Fatalf("expected newly created record %q tracked in motion state", newID)
	}

	// Mid-pulse frame should still keep the record pulsing.
	tt.Advance(200 * time.Millisecond)
	tt.Frame()
	if !state.Motion.IsRecentlyCreated(newID, tt.Now()) {
		t.Fatalf("expected creation pulse still active at 200ms")
	}

	// Past dbCreatePulse duration, the pulse entry is cleaned up cleanly.
	tt.Advance(dbCreatePulse + 50*time.Millisecond)
	tt.Frame()
	if state.Motion.IsRecentlyCreated(newID, tt.Now()) {
		t.Fatalf("expected creation pulse expired after %v", dbCreatePulse)
	}
	if len(state.Motion.created) != 0 {
		t.Fatalf("expected expired creation pulse map pruned, got %+v", state.Motion.created)
	}
}

func TestBoardCardFloatingDragAndSpringSettle(t *testing.T) {
	store := buildMotionTestStore(t)
	state := NewState(store)
	state.SetActiveView("view_board")
	opts := Options{
		Now: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC),
	}

	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(ui.LightTheme())
		ui.Column(c.Key("host")).Fill().Children(func() {
			View(c, state, opts)
		})
	}, 1100, 720)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	tt.Advance(400 * time.Millisecond)
	tt.Frame()

	srcRect, ok := tt.Find("需求 A")
	if !ok {
		t.Fatalf("missing board card 需求 A, texts=%v", tt.Texts())
	}
	dstRect, ok := tt.Find("需求 B")
	if !ok {
		t.Fatalf("missing board card 需求 B, texts=%v", tt.Texts())
	}

	x0, y0 := srcRect.X+srcRect.W/2, srcRect.Y+srcRect.H/2
	x1, y1 := dstRect.X+dstRect.W/2, dstRect.Y+dstRect.H/2

	tt.Press(x0, y0)
	tt.Move(x0+14, y0+4)
	tt.Frame()
	tt.Advance(60 * time.Millisecond)
	tt.Move((x0+x1)/2, (y0+y1)/2)
	tt.Frame()

	if !state.Motion.drag.active || state.Motion.drag.kind != dragKindCard {
		t.Fatalf("expected floating board card drag active, got %+v", state.Motion.drag)
	}

	tt.Move(x1, y1)
	tt.Frame()
	tt.Release(x1, y1)
	tt.Frame()

	// Release should hand off cleanly from drag to spring settle.
	if state.Motion.drag.active {
		t.Fatalf("expected drag cleared on release")
	}
	if !state.Motion.settle.active {
		t.Fatalf("expected spring settle active immediately after release")
	}

	// Render settle frames until completion.
	tt.Advance(60 * time.Millisecond)
	tt.Frame()
	tt.Advance(dbDragSettleDur + 40*time.Millisecond)
	tt.Frame()

	if state.Motion.settle.active {
		t.Fatalf("expected spring settle finished cleanly after %v", dbDragSettleDur)
	}

	recA := store.RecordByPath("动效测试库/a.md")
	if recA == nil || recA.Props["prop_status"].Text != "进行中" {
		t.Fatalf("expected 需求 A moved to 进行中 after drop, got %+v", recA)
	}
}

func TestTableSortKeepsTopPosition(t *testing.T) {
	root := t.TempDir()
	db := database.NewDefault("排序测试库", database.SourceConfig{Type: database.SourceFolder})
	db.Views = append(db.Views, database.ViewConfig{
		ID:   "view_list",
		Name: "列表",
		Type: database.ViewList,
	})
	raw, err := database.EncodeDatabaseMarkdown(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "排序测试库.db.md", raw); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 18; i++ {
		name := filepath.ToSlash(filepath.Join("排序测试库", "r"+string(rune('a'+i-1))+".md"))
		title := "记录 " + string(rune('A'+i-1))
		if err := vault.Write(root, name, "# "+title+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	store, err := database.Open(root, "排序测试库.db.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	state := NewState(store)
	opts := Options{Now: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}

	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(ui.LightTheme())
		ui.Column(c.Key("host")).Fill().Children(func() {
			View(c, state, opts)
		})
	}, 1100, 620)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()

	// Sort title ascending, then descending so the previously-first row moves to the bottom.
	if err := tt.Click("名称"); err != nil {
		t.Fatalf("click 名称 header (asc) failed: %v", err)
	}
	tt.Frame()
	if err := tt.Click("名称"); err != nil {
		t.Fatalf("click 名称 header (desc) failed: %v", err)
	}
	tt.Frame()
	tt.Advance(400 * time.Millisecond)
	tt.Frame()

	firstAfter, lastAfter := state.RowScroll.Visible()
	if firstAfter != 0 {
		t.Fatalf("expected table to stay at top (first=0) after sorting, got visible=(%d,%d) atEnd=%v",
			firstAfter, lastAfter, state.RowScroll.AtEnd())
	}
	if _, ok := tt.Find("记录 R"); !ok {
		t.Fatalf("expected top descending row 记录 R visible at top of table, texts=%v", tt.Texts())
	}

	// Verify List view also stays at top when sorted descending.
	state.SetActiveView("view_list")
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	lv := state.ActiveView().Clone()
	lv.Sorts = []database.SortRule{{PropertyID: "prop_title", Desc: true}}
	if err := store.UpdateView(lv); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Advance(400 * time.Millisecond)
	tt.Frame()
	if lf, ll := state.ListScroll.Visible(); lf != 0 {
		t.Fatalf("expected list view to stay at top (first=0) after sorting, got visible=(%d,%d)", lf, ll)
	}
}

func TestMotionRespectsReduceMotion(t *testing.T) {
	store := buildMotionTestStore(t)
	state := NewState(store)
	state.SetActiveView("view_board")
	opts := Options{
		Now: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC),
	}

	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(ui.LightTheme())
		ui.Column(c.Key("host")).Fill().Children(func() {
			View(c, state, opts)
		})
	}, 1100, 720)
	tt.SetLocale("zh-Hans")
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()

	// Creation pulse should not activate under ReduceMotion.
	state.Motion.TrackCreated("rec_test")
	tt.Frame()
	if state.Motion.IsRecentlyCreated("rec_test", tt.Now()) {
		t.Fatal("ReduceMotion should suppress creation pulse tracking")
	}

	// Dragging and releasing a board card should finish immediately with no settle glide.
	srcRect, ok := tt.Find("需求 A")
	if !ok {
		t.Fatalf("missing board card 需求 A")
	}
	dstRect, ok := tt.Find("需求 B")
	if !ok {
		t.Fatalf("missing board card 需求 B")
	}
	x0, y0 := srcRect.X+srcRect.W/2, srcRect.Y+srcRect.H/2
	x1, y1 := dstRect.X+dstRect.W/2, dstRect.Y+dstRect.H/2

	tt.Press(x0, y0)
	tt.Move(x0+14, y0+4)
	tt.Frame()
	tt.Move(x1, y1)
	tt.Frame()
	tt.Release(x1, y1)
	tt.Frame()

	if state.Motion.settle.active {
		t.Fatal("ReduceMotion should skip floating drag settle glide")
	}
}
