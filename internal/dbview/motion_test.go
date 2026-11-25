package dbview

import (
	"image/png"
	"os"
	"path/filepath"
	"slices"
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

func TestBoardDragLiveSlotPartingExactPlacementAndSnapshot(t *testing.T) {
	root := t.TempDir()
	db := database.NewDefault("拖拽动画测试", database.SourceConfig{Type: database.SourceFolder})
	db.Properties = append(db.Properties,
		database.Property{
			ID:   "prop_priority",
			Name: "优先级",
			Key:  "priority",
			Type: database.PropSelect,
			Options: []database.SelectOption{
				{ID: "p0", Name: "P0 紧急", Color: "red"},
				{ID: "p1", Name: "P1 高", Color: "orange"},
			},
		},
		database.Property{
			ID:   "prop_owner",
			Name: "负责人",
			Key:  "owner",
			Type: database.PropPerson,
		},
	)
	db.Views = append(db.Views, database.ViewConfig{
		ID:    "view_board",
		Name:  "看板",
		Type:  database.ViewBoard,
		Group: &database.GroupConfig{PropertyID: "prop_status", StatusBy: "option"},
		Board: &database.BoardViewConfig{CardSize: "medium", ColorColumn: true, ShowCount: true},
		Properties: []database.ViewProperty{
			{PropertyID: "prop_title", Visible: true},
			{PropertyID: "prop_priority", Visible: true},
			{PropertyID: "prop_owner", Visible: true},
		},
	})
	raw, err := database.EncodeDatabaseMarkdown(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "拖拽动画测试.db.md", raw); err != nil {
		t.Fatal(err)
	}
	writeRec := func(name, md string) {
		t.Helper()
		if err := vault.Write(root, filepath.ToSlash(filepath.Join("拖拽动画测试", name)), md); err != nil {
			t.Fatal(err)
		}
	}
	writeRec("a1.md", "---\nstatus: 未开始\npriority: P1 高\nowner: 周\n---\n\n# 卡片 A1：导出迁移\n")
	writeRec("a2.md", "---\nstatus: 未开始\npriority: P1 高\nowner: 顾\n---\n\n# 卡片 A2：时间轴拉伸\n")
	writeRec("b1.md", "---\nstatus: 进行中\npriority: P0 紧急\nowner: 沈\n---\n\n# 卡片 B1：两栖文档块\n")
	writeRec("b2.md", "---\nstatus: 进行中\npriority: P1 高\nowner: 林\n---\n\n# 卡片 B2：条件着色\n")
	writeRec("c1.md", "---\nstatus: 已完成\npriority: P0 紧急\nowner: 周\n---\n\n# 卡片 C1：公式引擎\n")

	store, err := database.Open(root, "拖拽动画测试.db.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	state := NewState(store)
	state.SetActiveView("view_board")
	opts := Options{Now: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)}

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

	findCard := func(title string) ui.Rect {
		t.Helper()
		r, ok := tt.Find(title)
		if !ok {
			t.Fatalf("missing card %q", title)
		}
		return r
	}

	a1Before := findCard("卡片 A1：导出迁移")
	b1Before := findCard("卡片 B1：两栖文档块")
	b2Before := findCard("卡片 B2：条件着色")

	// Drag A1 over the top half of B1 in the second column ("进行中").
	x0, y0 := a1Before.X+a1Before.W/2, a1Before.Y+a1Before.H/2
	xTarget, yTarget := b1Before.X+b1Before.W/2, b1Before.Y+b1Before.H*0.25

	tt.Press(x0, y0)
	tt.Move(x0+16, y0+4)
	tt.Frame()
	for i := 1; i <= 5; i++ {
		f := float32(i) / 5
		tt.Move(x0+(xTarget-x0)*f, y0+(yTarget-y0)*f)
		tt.Advance(24 * time.Millisecond)
		tt.Frame()
	}
	// Let the target column's cards finish parting smoothly for the live slot.
	tt.Advance(260 * time.Millisecond)
	tt.Frame()

	if !state.BoardDrag.grab.active || !state.BoardDrag.slot.active {
		t.Fatalf("expected active drag and live slot, grab=%+v slot=%+v", state.BoardDrag.grab, state.BoardDrag.slot)
	}
	if state.BoardDrag.slot.groupKey != "进行中" || state.BoardDrag.slot.index != 0 {
		t.Fatalf("expected live slot at 进行中 index 0, got %+v", state.BoardDrag.slot)
	}
	if !state.Motion.suppressOverlay {
		t.Fatal("expected generic fake motion overlay suppressed while full board card overlay is active")
	}

	// B1 must have shifted down smoothly to make room for the dashed slot above it.
	b1Parted := findCard("卡片 B1：两栖文档块")
	if delta := b1Parted.Y - b1Before.Y; delta < a1Before.H*0.7 {
		t.Fatalf("expected B1 to part down by ~%.1f DIPs for live slot, only moved %.1f (before=%.1f after=%.1f)",
			a1Before.H, delta, b1Before.Y, b1Parted.Y)
	}

	if snapDir := os.Getenv("ARCMD_SNAPSHOT"); snapDir != "" {
		saveTestPNG(t, filepath.Join(snapDir, "board-drag-mid.png"), tt)
	}

	// Release over the open slot -> A1 lands cleanly at index 0 of "进行中" (before B1).
	tt.Release(xTarget, yTarget)
	tt.Frame()
	if !state.BoardDrag.settle.active {
		t.Fatal("expected settle glide active on release")
	}
	tt.Advance(60 * time.Millisecond)
	tt.Frame()
	if !state.BoardDrag.settle.hasTarget {
		t.Fatal("expected settle glide to lock onto landed target rect")
	}
	if snapDir := os.Getenv("ARCMD_SNAPSHOT"); snapDir != "" {
		saveTestPNG(t, filepath.Join(snapDir, "board-drag-settle.png"), tt)
	}

	tt.Advance(boardDragSettleDur + 40*time.Millisecond)
	tt.Frame()
	if state.BoardDrag.settle.active || state.Motion.suppressOverlay {
		t.Fatalf("expected settle finished cleanly, settle=%+v suppress=%v", state.BoardDrag.settle, state.Motion.suppressOverlay)
	}

	// Verify A1 is at index 0 of "进行中" (before B1 and B2).
	res := store.Query("view_board", database.QueryOptions{Now: opts.Now})
	var inProg []string
	for _, g := range res.Groups {
		if g.Key == "进行中" {
			for _, r := range g.Rows {
				inProg = append(inProg, r.Title)
			}
		}
	}
	if len(inProg) != 3 || inProg[0] != "卡片 A1：导出迁移" || inProg[1] != "卡片 B1：两栖文档块" {
		t.Fatalf("expected A1 landed at top of 进行中 before B1, got %v", inProg)
	}

	// Single Cmd+Z atomically reverts BOTH status change and manual ordering.
	state.RequestFocus = true
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	recA1 := store.RecordByPath("拖拽动画测试/a1.md")
	if recA1 == nil || recA1.Props["prop_status"].Text != "未开始" {
		t.Fatalf("expected single Cmd+Z to revert A1 back to 未开始, got %+v", recA1)
	}

	// Verify intra-column drag to bottom: drag B1 below B2 in "进行中".
	b1Now := findCard("卡片 B1：两栖文档块")
	b2Now := findCard("卡片 B2：条件着色")
	_ = b2Before
	bx0, by0 := b1Now.X+b1Now.W/2, b1Now.Y+b1Now.H/2
	bx1, by1 := b2Now.X+b2Now.W/2, b2Now.Y+b2Now.H*0.85
	tt.Press(bx0, by0)
	tt.Move(bx0, by0+14)
	tt.Frame()
	tt.Move(bx1, by1)
	tt.Advance(100 * time.Millisecond)
	tt.Frame()
	tt.Release(bx1, by1)
	tt.Frame()
	tt.Advance(boardDragSettleDur + 80*time.Millisecond)
	tt.Frame()

	res = store.Query("view_board", database.QueryOptions{Now: opts.Now})
	inProg = nil
	for _, g := range res.Groups {
		if g.Key == "进行中" {
			for _, r := range g.Rows {
				inProg = append(inProg, r.Title)
			}
		}
	}
	if len(inProg) != 2 || inProg[0] != "卡片 B2：条件着色" || inProg[1] != "卡片 B1：两栖文档块" {
		t.Fatalf("expected intra-column reorder [B2, B1], got %v", inProg)
	}
}

func saveTestPNG(t *testing.T, path string, tt *ui.Tester) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestBoardSwitchViewMidSettleResetsSuppressOverlay(t *testing.T) {
	store := buildMotionTestStore(t)
	state := NewState(store)
	state.SetActiveView("view_board")
	opts := Options{Now: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}

	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(ui.LightTheme())
		ui.Column(c.Key("host")).Fill().Children(func() {
			View(c, state, opts)
		})
	}, 1100, 720)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()

	srcRect, ok := tt.Find("需求 A")
	if !ok {
		t.Fatal("missing board card 需求 A")
	}
	dstRect, ok := tt.Find("需求 B")
	if !ok {
		t.Fatal("missing board card 需求 B")
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

	if !state.BoardDrag.settle.active || !state.Motion.suppressOverlay {
		t.Fatalf("expected board settle active with suppressOverlay=true, settle=%+v suppress=%v",
			state.BoardDrag.settle, state.Motion.suppressOverlay)
	}

	// Switch view directly on Store mid-settle (without calling state.SetActiveView)
	// to verify View + beginFrame unconditionally clear suppressOverlay and reset BoardDrag.
	if err := store.SetActiveView("view_table"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(20 * time.Millisecond)
	tt.Frame()

	if state.Motion.suppressOverlay {
		t.Fatal("expected suppressOverlay reset to false immediately upon switching away from board mid-settle")
	}
	if state.BoardDrag.grab.active || state.BoardDrag.settle.active {
		t.Fatalf("expected BoardDrag reset on non-board view, grab=%+v settle=%+v",
			state.BoardDrag.grab, state.BoardDrag.settle)
	}
}

func TestMultiValueBoardDragCollapsesAndSettlesSingleBucketCard(t *testing.T) {
	root := t.TempDir()
	db := database.NewDefault("多值分组看板", database.SourceConfig{Type: database.SourceFolder})
	for i := range db.Properties {
		if db.Properties[i].ID == "prop_tags" {
			db.Properties[i].Options = []database.SelectOption{
				{ID: "t_fe", Name: "前端", Color: "blue"},
				{ID: "t_urg", Name: "紧急", Color: "red"},
				{ID: "t_be", Name: "后端", Color: "green"},
			}
		}
	}
	db.Views = append(db.Views, database.ViewConfig{
		ID:    "view_multi_board",
		Name:  "标签看板",
		Type:  database.ViewBoard,
		Group: &database.GroupConfig{PropertyID: "prop_tags"},
		Board: &database.BoardViewConfig{CardSize: "medium", ColorColumn: true, ShowCount: true},
	})
	raw, err := database.EncodeDatabaseMarkdown(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "多值分组看板.db.md", raw); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "多值分组看板/shared.md", "---\ntags:\n  - 前端\n  - 紧急\n---\n\n# 共享卡片\n"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "多值分组看板/backend.md", "---\ntags:\n  - 后端\n---\n\n# 后端锚点\n"); err != nil {
		t.Fatal(err)
	}

	store, err := database.Open(root, "多值分组看板.db.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	sharedRec := store.RecordByPath("多值分组看板/shared.md")
	if sharedRec == nil {
		t.Fatal("missing shared.md")
	}

	state := NewState(store)
	state.SetActiveView("view_multi_board")
	opts := Options{Now: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)}

	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(ui.LightTheme())
		ui.Column(c.Key("host")).Fill().Children(func() {
			View(c, state, opts)
		})
	}, 1100, 720)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()

	cardRectIn := func(recID, grpKey string) (ui.Rect, bool) {
		for _, cf := range state.BoardDrag.curCards {
			if !cf.slot && cf.recordID == recID && cf.groupKey == grpKey {
				return cf.r, true
			}
		}
		return ui.Rect{}, false
	}

	feRect, okFE := cardRectIn(sharedRec.ID, "前端")
	_, okUrg := cardRectIn(sharedRec.ID, "紧急")
	if !okFE || !okUrg {
		t.Fatalf("expected shared card rendered in both 前端 (%v) and 紧急 (%v)", okFE, okUrg)
	}
	beRect, okBE := tt.Find("后端锚点")
	if !okBE {
		t.Fatal("missing 后端锚点")
	}

	// 1. Start dragging shared card from "前端" column; verify "紧急" copy remains resting!
	fx, fy := feRect.X+feRect.W/2, feRect.Y+feRect.H/2
	bx, by := beRect.X+beRect.W/2, beRect.Y+beRect.H*0.25

	tt.Press(fx, fy)
	tt.Move(fx+16, fy+4)
	tt.Frame()
	tt.Advance(40 * time.Millisecond)
	tt.Move((fx+bx)/2, (fy+by)/2)
	tt.Frame()

	if !state.BoardDrag.grab.matchesCard(sharedRec.ID, "前端", "") {
		t.Fatalf("expected grab matched to 前端 card, got %+v", state.BoardDrag.grab)
	}
	if _, stillInFE := cardRectIn(sharedRec.ID, "前端"); stillInFE {
		t.Fatal("expected dragged source card in 前端 collapsed out of curCards")
	}
	if urgDuring, stillInUrg := cardRectIn(sharedRec.ID, "紧急"); !stillInUrg || urgDuring.W <= 0 || urgDuring.H <= 0 {
		t.Fatalf("expected sibling copy of shared card in 紧急 to stay visible/resting during drag, got ok=%v rect=%+v", stillInUrg, urgDuring)
	}

	// Cancel drag without drop -> settle must target ("前端", "") rather than ("紧急", "").
	tt.CancelPress()
	tt.Frame()
	if !state.BoardDrag.settle.active || state.BoardDrag.settle.toGroupKey != "前端" {
		t.Fatalf("expected cancelled drag settle to target 前端, got %+v", state.BoardDrag.settle)
	}
	if state.BoardDrag.settle.matchesCard(sharedRec.ID, "紧急", "") {
		t.Fatal("cancelled drag settle must NOT match sibling card in 紧急")
	}
	tt.Advance(boardDragSettleDur + 40*time.Millisecond)
	tt.Frame()

	// 2. Drag shared card from "前端" and drop onto "后端" -> settle must ONLY match "后端", leaving "紧急" untouched.
	feRect, okFE = cardRectIn(sharedRec.ID, "前端")
	if !okFE {
		t.Fatal("missing shared card in 前端 after cancel")
	}
	fx, fy = feRect.X+feRect.W/2, feRect.Y+feRect.H/2
	tt.Press(fx, fy)
	tt.Move(fx+16, fy+4)
	tt.Frame()
	tt.Move(bx, by)
	tt.Advance(80 * time.Millisecond)
	tt.Frame()
	tt.Release(bx, by)
	tt.Frame()

	if !state.BoardDrag.settle.active || state.BoardDrag.settle.toGroupKey != "后端" {
		t.Fatalf("expected dropped drag settle to target 后端, got %+v", state.BoardDrag.settle)
	}
	if !state.BoardDrag.settle.matchesCard(sharedRec.ID, "后端", "") {
		t.Fatal("expected settle to match landed card in 后端")
	}
	if state.BoardDrag.settle.matchesCard(sharedRec.ID, "紧急", "") {
		t.Fatal("settle must NOT match sibling card in 紧急")
	}

	gotTags := store.Record(sharedRec.ID).Props["prop_tags"].List
	if !slices.Equal(gotTags, []string{"紧急", "后端"}) {
		t.Fatalf("expected tags updated to [紧急 后端], got %v", gotTags)
	}
}

func TestCrossStateBoardDropIgnored(t *testing.T) {
	store1 := buildMotionTestStore(t)
	store2 := buildMotionTestStore(t)
	state1 := NewState(store1)
	state2 := NewState(store2)
	state1.SetActiveView("view_board")
	state2.SetActiveView("view_board")
	opts := Options{Now: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)}

	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(ui.LightTheme())
		ui.Row(c.Key("split-host")).Fill().Children(func() {
			ui.Column(c.Key("pane-1")).Width(600).HeightPercent(100).Children(func() {
				View(c, state1, opts)
			})
			ui.Column(c.Key("pane-2")).Width(600).HeightPercent(100).Children(func() {
				View(c, state2, opts)
			})
		})
	}, 1200, 720)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()

	rev2Before := store2.Revision()

	// Find 需求 A in left pane (X < 600) and 进行中 bucket in right pane (X >= 600).
	var srcRect ui.Rect
	for _, cf := range state1.BoardDrag.curCards {
		if cf.groupKey == "未开始" {
			srcRect = cf.r
			break
		}
	}
	var dstBucket ui.Rect
	for _, bk := range state2.BoardDrag.curBuckets {
		if bk.groupKey == "进行中" {
			dstBucket = bk.r
			break
		}
	}
	if srcRect.W <= 0 || dstBucket.W <= 0 || dstBucket.X < 600 {
		t.Fatalf("unexpected split layout: src=%+v dst=%+v", srcRect, dstBucket)
	}

	x0, y0 := srcRect.X+srcRect.W/2, srcRect.Y+srcRect.H/2
	x1, y1 := dstBucket.X+dstBucket.W/2, dstBucket.Y+60

	tt.Press(x0, y0)
	tt.Move(x0+16, y0+4)
	tt.Frame()
	tt.Move(x1, y1)
	tt.Frame()
	tt.Release(x1, y1)
	tt.Frame()

	if store2.Revision() != rev2Before {
		t.Fatalf("expected cross-State drop onto state2 to be ignored, but store2 revision changed %d -> %d",
			rev2Before, store2.Revision())
	}
	if state2.BoardDrag.grab.dropped {
		t.Fatal("expected state2.BoardDrag.grab.dropped to remain false on foreign drag payload")
	}
}

func TestFilteredReorderPreservesHiddenRecords(t *testing.T) {
	db := database.NewDefault("过滤拖拽保序", database.SourceConfig{Type: database.SourceInline})
	var initialOrder []string
	for i := range 6 {
		id := "rec_" + string(rune('a'+i))
		initialOrder = append(initialOrder, id)
		status := "已完成"
		if i == 1 || i == 4 {
			status = "进行中"
		}
		db.InlineRows = append(db.InlineRows, &database.Record{
			ID:    id,
			Title: "条目 " + string(rune('A'+i)),
			Props: map[string]database.Value{"prop_status": database.TextValue(status)},
		})
	}
	viewID := db.Views[0].ID
	db.Views[0].ManualOrder = slices.Clone(initialOrder)
	db.Views[0].Filter = &database.FilterGroup{
		Conjunction: "and",
		Rules: []database.FilterRule{
			{ID: "f_prog", PropertyID: "prop_status", Op: database.OpEquals, Value: database.TextValue("进行中")},
		},
	}

	store := database.NewStore("", "filtered_ui.db.md", db)
	state := NewState(store)
	view := state.ActiveView()
	res := store.Query(viewID, database.QueryOptions{})
	if len(res.Rows) != 2 || res.Rows[0].ID != "rec_b" || res.Rows[1].ID != "rec_e" {
		t.Fatalf("expected filtered rows [rec_b rec_e], got %+v", res.Rows)
	}

	// 1. Table/List/Gallery row reorder while filter hides 4 records.
	reorderRecordRow(state, view, res, "rec_e", "rec_b", Options{})
	wantAfterTable := []string{"rec_a", "rec_e", "rec_b", "rec_c", "rec_d", "rec_f"}
	if got := store.SchemaSnapshot().Views[0].ManualOrder; !slices.Equal(got, wantAfterTable) {
		t.Fatalf("ManualOrder after filtered reorderRecordRow = %v, want %v", got, wantAfterTable)
	}

	// 2. Board slot reorder while filtered: move rec_e immediately after rec_b,
	// preserving the relative order of all 4 hidden records [rec_a, rec_c, rec_d, rec_f].
	view = state.ActiveView()
	res = store.Query(viewID, database.QueryOptions{})
	boardOrder := computeManualOrderForSlot(state, view, res, "rec_e", "", "rec_b")
	wantAfterBoard := []string{"rec_a", "rec_b", "rec_e", "rec_c", "rec_d", "rec_f"}
	if !slices.Equal(boardOrder, wantAfterBoard) {
		t.Fatalf("computeManualOrderForSlot after filtered move = %v, want %v", boardOrder, wantAfterBoard)
	}
}

func TestZeroWidthCalendarStopsAnimationFrames(t *testing.T) {
	store := buildMotionTestStore(t)
	db := store.SchemaSnapshot()
	calView := NewViewConfig(database.ViewCalendar, "日历", db)
	if err := store.AddView(calView, true); err != nil {
		t.Fatal(err)
	}
	state := NewState(store)
	opts := Options{Now: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)}

	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(ui.DarkTheme())
		// Collapsed zero-width container.
		ui.Column(c.Key("collapsed-host")).Size(0, 600).Clip().Children(func() {
			View(c, state, opts)
		})
	}, 800, 600)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	// Advance past initial view-stage enter transition (150ms) and render one frame.
	tt.Advance(250 * time.Millisecond)
	tt.Frame()
	tt.Frame()

	if tt.FramePending() {
		t.Fatal("expected zero-width calendar to stop requesting AnimationFrame after single measurement retry")
	}
}

func TestPaletteAccentTextAndDarkSurfaceTokens(t *testing.T) {
	custom := *ui.DarkTheme()
	custom.Background = ui.Hex("#1F2023")
	custom.Accent = ui.Hex("#FACC15")
	custom.AccentText = ui.Hex("#18181B")

	pal := paletteOf(&custom)
	if pal.AccentText != custom.AccentText {
		t.Fatalf("expected Palette.AccentText=%v from Theme.AccentText, got %v", custom.AccentText, pal.AccentText)
	}

	lightCal := dfCalendarPalette(paletteOf(ui.LightTheme()))
	darkCal := dfCalendarPalette(pal)
	if lightCal.nowLine != paletteOf(ui.LightTheme()).Danger || darkCal.nowLine != pal.Danger {
		t.Fatalf("expected consistent semantic Danger color for current-time indicator, light=%v dark=%v",
			lightCal.nowLine, darkCal.nowLine)
	}
}
