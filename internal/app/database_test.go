package app

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/agent"
	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/dbview"
	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/search"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func seedAppDatabase(t *testing.T, a *App, title string) (*database.Database, string) {
	t.Helper()
	sp := a.state.ActiveSpace()
	db := database.NewDefault(title, database.SourceConfig{Type: database.SourceFolder})
	db.Properties = append(db.Properties,
		database.Property{ID: "prop_done", Key: "done", Name: "完成", Type: database.PropCheckbox, Width: 72},
		database.Property{ID: "prop_score", Key: "score", Name: "权重", Type: database.PropNumber, Width: 90},
	)
	for i := range db.Views {
		db.Views[i].Properties = append(db.Views[i].Properties,
			database.ViewProperty{PropertyID: "prop_done", Visible: true, Width: 72},
			database.ViewProperty{PropertyID: "prop_score", Visible: true, Width: 90},
		)
	}
	raw, err := database.EncodeDatabaseMarkdown(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	dbRel := title + vault.ExtDatabase
	if err := vault.Write(sp.Root, dbRel, raw); err != nil {
		t.Fatal(err)
	}
	return db, dbRel
}

func TestDatabaseShortcutCreateAndBrowseNoWrite(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()

	// Create a new database via Cmd+Alt+G.
	tt.Key(ui.Cmd|ui.Alt, ui.KeyG)
	tt.Frame()

	if a.doc == nil || !a.doc.isDatabase() || !vault.IsDatabase(a.doc.rel) {
		t.Fatalf("expected active .db.md database after Cmd+Alt+D, got doc=%v rel=%v", a.doc, func() string {
			if a.doc == nil {
				return ""
			}
			return a.doc.rel
		}())
	}
	if !tt.HasText("名称") || !tt.HasText("状态") || !tt.HasText("标签") {
		t.Fatalf("expected default database columns in table UI, texts=%v", tt.Texts())
	}

	// Unmodified browsing must never write .db.md back to disk.
	dbRel := a.doc.rel
	beforeBytes, err := vault.Read(root(a), dbRel)
	if err != nil {
		t.Fatal(err)
	}
	full, err := resolveFile(a.state.ActiveSpace(), dbRel)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-10 * time.Second)
	_ = os.Chtimes(full, past, past)
	beforeInfo, err := os.Stat(full)
	if err != nil {
		t.Fatal(err)
	}

	// Browse/click search toggle without editing any data or schema.
	a.doc.dbView.SearchOpen = true
	tt.Frame()
	a.doc.dbView.SearchOpen = false
	tt.Advance(2 * time.Second)
	tt.Frame()

	if a.doc.dirty() {
		t.Fatal("unmodified database browsing unexpectedly marked dirty")
	}
	afterBytes, _ := vault.Read(root(a), dbRel)
	afterInfo, _ := os.Stat(full)
	if afterBytes != beforeBytes || !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
		t.Fatalf("unmodified browsing rewrote .db.md on disk (mtime %v -> %v)", beforeInfo.ModTime(), afterInfo.ModTime())
	}
}

func TestDatabaseRecordCreationAndFrontmatterSync(t *testing.T) {
	a, tt := newTestApp(t)
	_, dbRel := seedAppDatabase(t, a, "Roadmap")
	if err := vault.Write(root(a), "Roadmap/alpha.md",
		"---\nauthor: Cizai\nstatus: 未开始\ndone: false\nscore: 42\n---\n\n# Alpha\n\nKeep **ArcMD** formatting.\n\nSecond paragraph.\n",
	); err != nil {
		t.Fatal(err)
	}

	a.refreshTree()
	a.open(dbRel)
	tt.Frame()

	if !a.doc.isDatabase() || !tt.HasText("Alpha") {
		t.Fatalf("expected Roadmap.db.md with Alpha row, texts=%v", tt.Texts())
	}

	// Create a new row via "+ 新建" -> creates backing .md file under Roadmap/.
	if err := tt.Click("+ 新建"); err != nil {
		t.Fatalf("click + 新建 failed: %v", err)
	}
	tt.Frame()

	entries, err := os.ReadDir(filepath.Join(root(a), "Roadmap"))
	if err != nil || len(entries) < 2 {
		t.Fatalf("expected backing .md created in Roadmap/, got err=%v entries=%d", err, len(entries))
	}

	// Edit Status & Title on alpha.md via Store + syncDatabaseEditsToDocs and verify lossless frontmatter flush.
	store := a.doc.dbView.Store
	rec := store.RecordByPath("Roadmap/alpha.md")
	if rec == nil {
		t.Fatal("missing Roadmap/alpha.md record")
	}
	affected, err := store.SetCellsBatch([]database.CellEdit{
		{RecordID: rec.ID, PropID: "prop_status", Value: database.TextValue("进行中")},
		{RecordID: rec.ID, PropID: "prop_title", Value: database.TextValue("Alpha Updated")},
		{RecordID: rec.ID, PropID: "prop_done", Value: database.BoolValue(true)},
	})
	if err != nil {
		t.Fatal(err)
	}
	a.syncDatabaseEditsToDocs(a.state.ActiveSpace(), a.doc, store, affected)
	tt.Frame()

	flushed, err := vault.Read(root(a), "Roadmap/alpha.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"author: Cizai", "status: 进行中", "done: true", "# Alpha Updated", "Keep **ArcMD** formatting.", "Second paragraph."} {
		if !strings.Contains(flushed, want) {
			t.Fatalf("expected %q preserved/synced in alpha.md, got:\n%s", want, flushed)
		}
	}
}

func TestDatabasePeekTwoWaySyncAndFullPage(t *testing.T) {
	a, tt := newTestApp(t)
	_, dbRel := seedAppDatabase(t, a, "Tasks")
	if err := vault.Write(root(a), "Tasks/spec.md",
		"---\nstatus: 未开始\nscore: 10\n---\n\n# 设计评审\n\n评审正文。\n",
	); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open(dbRel)
	tt.Frame()

	store := a.doc.dbView.Store
	rec := store.RecordByPath("Tasks/spec.md")
	if rec == nil {
		t.Fatal("missing Tasks/spec.md record")
	}

	// Open Side Peek.
	a.openRecordPeek(a.doc, store, a.state.ActiveSpace(), rec, dbview.OpenSidePeek)
	tt.Frame()

	if !a.dbPeek.open || a.dbPeek.mode != dbview.OpenSidePeek || a.dbPeek.doc == nil {
		t.Fatalf("Side Peek failed to open: %+v", a.dbPeek)
	}
	if !tt.HasText("页面属性") || !strings.Contains(a.dbPeek.doc.ed.Doc.PlainText(), "评审正文。") {
		t.Fatalf("expected Side Peek property sheet & body, plain=%q texts=%v", a.dbPeek.doc.ed.Doc.PlainText(), tt.Texts())
	}

	// Live sync Peek H1 -> Database Table.
	h1 := a.dbPeek.doc.ed.Doc.Blocks[0]
	a.dbPeek.doc.ed.Engine.ReplaceBlockRuns(h1.ID, []model.Run{{Text: "最终评审"}}, 0, 0, model.Style{}, false)
	tt.Frame()

	if got := a.doc.dbView.Store.Record(rec.ID).Title; got != "最终评审" {
		t.Fatalf("expected live sync Peek H1 -> DB Title '最终评审', got %q", got)
	}
	if !tt.HasText("最终评审") {
		t.Fatalf("expected table UI updated live with '最终评审'")
	}

	// Live sync Database Table -> Peek frontmatter without clobbering Peek body.
	affected, err := store.SetCell(rec.ID, "prop_status", database.TextValue("已完成"))
	if err != nil {
		t.Fatal(err)
	}
	a.syncDatabaseEditsToDocs(a.state.ActiveSpace(), a.doc, store, affected)
	tt.Frame()
	if !strings.Contains(a.dbPeek.doc.ed.Doc.Frontmatter, "status: 已完成") {
		t.Fatalf("expected Peek frontmatter updated live, got %q", a.dbPeek.doc.ed.Doc.Frontmatter)
	}

	// Switch Side Peek -> Center Peek.
	if err := tt.Click("居中弹窗"); err != nil {
		t.Fatalf("switch to Center Peek failed: %v", err)
	}
	tt.Frame()
	if !a.dbPeek.open || a.dbPeek.mode != dbview.OpenCenterPeek {
		t.Fatalf("expected Center Peek mode, got open=%v mode=%q", a.dbPeek.open, a.dbPeek.mode)
	}

	// Switch Center Peek -> Full Page (with PagePropertiesView header above editor).
	if err := tt.Click("打开为整页"); err != nil {
		t.Fatalf("open full page failed: %v", err)
	}
	tt.Frame()
	if a.dbPeek.open {
		t.Fatal("expected dbPeek closed after opening full page")
	}
	if a.doc == nil || a.doc.rel != "Tasks/spec.md" {
		t.Fatalf("expected active document Tasks/spec.md, got %#v", a.doc)
	}
	if !tt.HasText("页面属性") {
		t.Fatalf("expected collapsible PagePropertiesView on full-page database record, texts=%v", tt.Texts())
	}
}

func TestDatabaseSidebarManagedFolderFoldingAndExternalRefresh(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	_, dbRel := seedAppDatabase(t, a, "Backlog")
	if err := vault.Write(sp.Root, "Backlog/story.md", "---\nstatus: 未开始\n---\n\n# 需求一\n\n内容。\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	tt.Frame()

	// Backlog/ is folded collapsed under Backlog.db.md by default.
	if !tt.HasText("Backlog") {
		t.Fatalf("expected Backlog.db.md in sidebar, texts=%v", tt.Texts())
	}
	if tt.HasText("story") && a.doc != nil && a.doc.rel != "Backlog/story.md" {
		// Check sidebar node expansion state specifically.
		t.Fatalf("did not expect folded child 'story' before expanding Backlog folder")
	}

	// Expanding "Backlog" shows Backlog/story.md beneath Backlog.db.md.
	sp.SetExpanded("Backlog", true)
	tt.Frame()
	if !tt.HasText("story") && !tt.HasText("需求一") {
		t.Fatalf("expected Backlog children after expanding managed folder, texts=%v", tt.Texts())
	}

	// Open Backlog.db.md and simulate external disk modification of Backlog/story.md.
	a.open(dbRel)
	tt.Frame()
	if err := vault.Write(sp.Root, "Backlog/story.md", "---\nstatus: 已完成\n---\n\n# 需求一（外部更新）\n\n内容。\n"); err != nil {
		t.Fatal(err)
	}
	fullStory, _ := resolveFile(sp, "Backlog/story.md")
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(fullStory, future, future)
	a.filesChanged(sp.ID, []mygo.FileEvent{{Op: mygo.FileWrite, Path: "Backlog/story.md"}})
	tt.Frame()

	rec := a.doc.dbView.Store.RecordByPath("Backlog/story.md")
	if rec == nil || rec.Title != "需求一（外部更新）" {
		t.Fatalf("expected external refresh to update title, got %#v", rec)
	}
	if a.doc.dirty() {
		t.Fatal("external record refresh should not mark .db.md dirty")
	}
}

func writeSampleCoverPNG(t *testing.T, full string, r1, g1, b1, r2, g2, b2 uint8) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	w, h := 320, 160
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			tRatio := float32(x+y) / float32(w+h)
			c := color.RGBA{
				R: uint8(float32(r1)*(1-tRatio) + float32(r2)*tRatio),
				G: uint8(float32(g1)*(1-tRatio) + float32(g2)*tRatio),
				B: uint8(float32(b1)*(1-tRatio) + float32(b2)*tRatio),
				A: 255,
			}
			// Soft decorative geometric diagonal band.
			if (x-y*2) > -20 && (x-y*2) < 40 {
				c.R = uint8(min(255, int(c.R)+24))
				c.G = uint8(min(255, int(c.G)+24))
				c.B = uint8(min(255, int(c.B)+24))
			}
			img.SetRGBA(x, y, c)
		}
	}
	f, err := os.Create(full)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestDatabasePhase2ViewsCoverAndPersistence(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	db := database.NewDefault("Roadmap", database.SourceConfig{Type: database.SourceFolder})
	db.Properties = append(db.Properties,
		database.Property{ID: "prop_done", Key: "done", Name: "完成", Type: database.PropCheckbox, Width: 72},
		database.Property{ID: "prop_score", Key: "score", Name: "权重", Type: database.PropNumber, Width: 90},
	)
	vis := append(slices.Clone(db.Views[0].Properties),
		database.ViewProperty{PropertyID: "prop_done", Visible: true, Width: 72},
		database.ViewProperty{PropertyID: "prop_score", Visible: true, Width: 90},
	)
	db.Views = append(db.Views,
		database.ViewConfig{
			ID:         "view_list",
			Name:       "列表",
			Type:       database.ViewList,
			OpenMode:   dbview.OpenSidePeek,
			Properties: slices.Clone(vis),
		},
		database.ViewConfig{
			ID:         "view_gallery",
			Name:       "画廊",
			Type:       database.ViewGallery,
			OpenMode:   dbview.OpenSidePeek,
			Properties: slices.Clone(vis),
			Gallery:    &database.GalleryViewConfig{CardSize: "medium", CoverSource: "cover", ShowIcon: true},
		},
	)
	rawDB, err := database.EncodeDatabaseMarkdown(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Roadmap.db.md", rawDB); err != nil {
		t.Fatal(err)
	}
	writeSampleCoverPNG(t, filepath.Join(sp.Root, "Roadmap/assets/cover.png"), 59, 130, 246, 139, 92, 246)
	if err := vault.Write(sp.Root, "Roadmap/alpha.md",
		"---\ncover: assets/cover.png\nstatus: 未开始\ntags:\n  - 重要\ndone: false\nscore: 88\n---\n\n# Alpha Card\n\nAlpha body.\n",
	); err != nil {
		t.Fatal(err)
	}

	a.refreshTree()
	a.open("Roadmap.db.md")
	tt.Frame()

	// Verify cover image loading, caching, and traversal/remote rejection.
	bmp := loadDatabaseRecordImage(sp, a.doc, "Roadmap/alpha.md", "assets/cover.png")
	if bmp == nil {
		t.Fatal("expected loadDatabaseRecordImage to load Roadmap/assets/cover.png")
	}
	if loadDatabaseRecordImage(sp, a.doc, "Roadmap/alpha.md", "assets/cover.png") != bmp {
		t.Fatal("expected cached *ui.Bitmap hit on repeated cover load")
	}
	if loadDatabaseRecordImage(sp, a.doc, "Roadmap/alpha.md", "../../etc/passwd") != nil {
		t.Fatal("expected traversal path ../../etc/passwd to be rejected")
	}
	if loadDatabaseRecordImage(sp, a.doc, "Roadmap/alpha.md", "https://example.com/cover.png") != nil {
		t.Fatal("expected remote URL to return nil")
	}

	// Switch through Board, List, and Gallery tabs and verify records render & flush cleanly.
	for _, tab := range []string{"看板", "列表", "画廊"} {
		if err := tt.Click(tab); err != nil {
			t.Fatalf("switch to %s view failed: %v", tab, err)
		}
		tt.Frame()
		if !tt.HasText("Alpha Card") {
			t.Fatalf("expected Alpha Card in %s view, texts=%v", tab, tt.Texts())
		}
	}
}

func TestDatabasePhase3CalendarTimelinePersistence(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	now := tt.Now()
	todayStr := now.Format("2006-01-02")
	endStr := now.AddDate(0, 0, 4).Format("2006-01-02")

	db := database.NewDefault("Sprint", database.SourceConfig{Type: database.SourceFolder})
	vis := slices.Clone(db.Views[0].Properties)
	db.Views = append(db.Views,
		database.ViewConfig{
			ID:         "view_cal",
			Name:       "日历",
			Type:       database.ViewCalendar,
			OpenMode:   dbview.OpenSidePeek,
			Properties: slices.Clone(vis),
			Calendar: &database.CalendarViewConfig{
				DatePropID:   "prop_date",
				Mode:         "month",
				ShowWeekends: true,
				WeekStart:    1,
			},
		},
		database.ViewConfig{
			ID:         "view_tl",
			Name:       "时间轴",
			Type:       database.ViewTimeline,
			OpenMode:   dbview.OpenSidePeek,
			Properties: slices.Clone(vis),
			Timeline: &database.TimelineViewConfig{
				StartPropID: "prop_date",
				Zoom:        "week",
				ShowTable:   true,
			},
		},
	)
	rawDB, err := database.EncodeDatabaseMarkdown(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Sprint.db.md", rawDB); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Sprint/task.md",
		"---\nstatus: 进行中\ndate: "+todayStr+"\ndate_end: "+endStr+"\n---\n\n# 核心任务\n\n周期规划。\n",
	); err != nil {
		t.Fatal(err)
	}

	a.refreshTree()
	a.open("Sprint.db.md")
	tt.Frame()

	for _, tab := range []string{"日历", "时间轴"} {
		if err := tt.Click(tab); err != nil {
			t.Fatalf("switch to %s view failed: %v", tab, err)
		}
		tt.Frame()
		if !tt.HasText("核心任务") {
			t.Fatalf("expected 核心任务 in %s view, texts=%v", tab, tt.Texts())
		}
	}

	// Modify range via Store and verify lossless date + date_end frontmatter flush to disk.
	store := a.doc.dbView.Store
	rec := store.RecordByPath("Sprint/task.md")
	if rec == nil {
		t.Fatal("missing Sprint/task.md")
	}
	nextStart := now.AddDate(0, 0, 1).Format("2006-01-02")
	nextEnd := now.AddDate(0, 0, 6).Format("2006-01-02")
	affected, err := store.SetCell(rec.ID, "prop_date", database.DateRangeValue(nextStart, nextEnd, false))
	if err != nil {
		t.Fatal(err)
	}
	a.syncDatabaseEditsToDocs(sp, a.doc, store, affected)
	tt.Frame()

	flushed, err := vault.Read(sp.Root, "Sprint/task.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(flushed, "date: "+nextStart) || !strings.Contains(flushed, "date_end: "+nextEnd) {
		t.Fatalf("expected date/date_end synced in task.md, got:\n%s", flushed)
	}
}

func TestDatabasePhase4ChartFeedFormPersistence(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1240, 860)
	sp := a.state.ActiveSpace()

	db := database.NewDefault("Feedback", database.SourceConfig{Type: database.SourceFolder})
	db.Properties = append(db.Properties,
		database.Property{ID: "prop_score", Key: "score", Name: "权重", Type: database.PropNumber, Width: 90},
	)
	vis := append(slices.Clone(db.Views[0].Properties),
		database.ViewProperty{PropertyID: "prop_score", Visible: true, Width: 90},
	)
	db.Views[0].Properties = vis
	db.Views = append(db.Views,
		database.ViewConfig{
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
		},
		database.ViewConfig{
			ID:         "view_feed",
			Name:       "信息流",
			Type:       database.ViewFeed,
			OpenMode:   dbview.OpenSidePeek,
			Properties: slices.Clone(vis),
			Feed:       &database.FeedViewConfig{ShowExcerpt: true, ShowImage: true},
		},
		database.ViewConfig{
			ID:       "view_form",
			Name:     "表单",
			Type:     database.ViewForm,
			OpenMode: dbview.OpenSidePeek,
			Form: &database.FormViewConfig{
				Title:       "需求收集单",
				Description: "提交需求并持久化为空间 Markdown 文件",
				SubmitText:  "提交需求",
			},
		},
	)
	rawDB, err := database.EncodeDatabaseMarkdown(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Feedback.db.md", rawDB); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Feedback/seed.md", "---\nstatus: 已完成\nscore: 40\n---\n\n# 种子需求\n\n已有需求摘要。\n"); err != nil {
		t.Fatal(err)
	}

	a.refreshTree()
	a.open("Feedback.db.md")
	tt.Frame()

	// Switch to Chart and verify grand total.
	if err := tt.Click("图表"); err != nil {
		t.Fatalf("switch to 图表 failed: %v", err)
	}
	tt.Frame()
	if !tt.HasText("总计 40") {
		t.Fatalf("expected 总计 40 in Chart view, texts=%v", tt.Texts())
	}

	// Switch to Feed and verify Markdown excerpt renders.
	if err := tt.Click("信息流"); err != nil {
		t.Fatalf("switch to 信息流 failed: %v", err)
	}
	tt.Frame()
	if !tt.HasText("已有需求摘要。") {
		t.Fatalf("expected 已有需求摘要。 in Feed view, texts=%v", tt.Texts())
	}

	// Switch to Form and submit a complete new record.
	if err := tt.Click("表单"); err != nil {
		t.Fatalf("switch to 表单 failed: %v", err)
	}
	tt.Frame()

	clickAppRole := func(role ui.Role, name string) {
		t.Helper()
		info, ok := tt.AccessByRole(role, name)
		if !ok {
			t.Fatalf("missing role=%v name=%q, access=%+v", role, name, tt.AccessAll())
		}
		if info.Bounds.Y+info.Bounds.H > 840 {
			tt.Scroll(info.Bounds.X+info.Bounds.W/2, 420, 0, info.Bounds.Y+info.Bounds.H-740)
			tt.Frame()
			info, _ = tt.AccessByRole(role, name)
		} else if info.Bounds.Y < 100 {
			tt.Scroll(info.Bounds.X+info.Bounds.W/2, 420, 0, info.Bounds.Y-160)
			tt.Frame()
			info, _ = tt.AccessByRole(role, name)
		}
		tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
		tt.Frame()
	}

	clickAppRole(ui.RoleTextField, "名称")
	tt.Type("持久化表单任务")
	tt.Frame()

	clickAppRole(ui.RoleOption, "进行中")

	clickAppRole(ui.RoleTextField, "权重")
	tt.Type("88")
	tt.Frame()

	clickAppRole(ui.RoleTextField, "正文内容")
	tt.Type("来自表单提交的详细正文")
	tt.Frame()

	clickAppRole(ui.RoleButton, "提交需求")
	tt.Frame()

	if !tt.HasText("已提交「持久化表单任务」") {
		t.Fatalf("expected submission banner, texts=%v", tt.Texts())
	}

	var created *database.Record
	for _, r := range a.doc.dbView.Store.Records() {
		if r.Title == "持久化表单任务" {
			created = r
			break
		}
	}
	if created == nil {
		t.Fatalf("expected newly submitted record in Store, records=%d", len(a.doc.dbView.Store.Records()))
	}
	mdRaw, err := vault.Read(sp.Root, created.Path)
	if err != nil {
		t.Fatalf("read submitted markdown %q: %v", created.Path, err)
	}
	for _, want := range []string{"status: 进行中", "score: 88", "# 持久化表单任务", "来自表单提交的详细正文"} {
		if !strings.Contains(mdRaw, want) {
			t.Fatalf("submitted markdown missing %q, content:\n%s", want, mdRaw)
		}
	}
}

func TestDatabasePhase5RelationRollupFormulaColorsTemplates(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1280, 820)
	sp := a.state.ActiveSpace()
	todayStr := tt.Now().Format("2006-01-02")

	// Seed Tasks database.
	tasksDB := database.NewDefault("Tasks", database.SourceConfig{Type: database.SourceFolder})
	tasksDB.Properties = append(tasksDB.Properties,
		database.Property{ID: "t_score", Key: "score", Name: "权重", Type: database.PropNumber, Width: 80},
	)
	for i := range tasksDB.Views {
		tasksDB.Views[i].Properties = append(tasksDB.Views[i].Properties,
			database.ViewProperty{PropertyID: "t_score", Visible: true, Width: 80},
		)
	}
	rawTasks, err := database.EncodeDatabaseMarkdown(tasksDB, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Tasks.db.md", rawTasks); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Tasks/task1.md", "---\nstatus: 进行中\nscore: 60\n---\n\n# 核心任务 A\n"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Tasks/task2.md", "---\nstatus: 未开始\nscore: 50\n---\n\n# 核心任务 B\n"); err != nil {
		t.Fatal(err)
	}

	// Seed Projects database with Relation, Rollup, Formula 2.0, ColorRule, and Template.
	projDB := database.NewDefault("Projects", database.SourceConfig{Type: database.SourceFolder})
	projDB.Properties = append(projDB.Properties,
		database.Property{
			ID:       "p_rel",
			Key:      "tasks",
			Name:     "关联任务",
			Type:     database.PropRelation,
			Relation: &database.RelationConfig{TargetDB: "Tasks.db.md"},
			Width:    150,
		},
		database.Property{
			ID:     "p_sum",
			Key:    "task_sum",
			Name:   "总权重",
			Type:   database.PropRollup,
			Rollup: &database.RollupConfig{RelationPropID: "p_rel", TargetPropID: "t_score", Calculate: database.AggSum},
			Width:  90,
		},
		database.Property{
			ID:      "p_badge",
			Key:     "badge",
			Name:    "等级",
			Type:    database.PropFormula,
			Formula: &database.FormulaConfig{Expression: `if(prop("总权重") >= 100, "🔥 高优先级", "常规")`},
			Width:   130,
		},
	)
	for _, pid := range []string{"p_rel", "p_sum", "p_badge"} {
		projDB.Views[0].Properties = append(projDB.Views[0].Properties, database.ViewProperty{PropertyID: pid, Visible: true, Width: 130})
	}
	projDB.Views[0].Colors = []database.ColorRule{
		{
			ID:    "cr_hot",
			Color: "red",
			Filter: database.FilterGroup{
				Conjunction: "and",
				Rules:       []database.FilterRule{{ID: "r1", PropertyID: "p_badge", Op: database.OpEquals, Value: database.TextValue("🔥 高优先级")}},
			},
		},
	}
	projDB.Templates = []database.Template{
		{
			ID:   "tpl_sprint",
			Name: "冲刺任务 {{date}}",
			Icon: "🚀",
			Props: map[string]database.Value{
				"prop_status": database.TextValue("进行中"),
			},
			Body: "## 目标\n{{title}} - {{date}}\n",
		},
	}
	rawProj, err := database.EncodeDatabaseMarkdown(projDB, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Projects.db.md", rawProj); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Projects/core.md", "---\nstatus: 进行中\n---\n\n# 核心项目 Alpha\n"); err != nil {
		t.Fatal(err)
	}

	a.refreshTree()
	a.open("Projects.db.md")
	tt.Frame()

	store := a.doc.dbView.Store
	if _, err := store.SetupTwoWayRelation("p_rel", "Tasks.db.md", "所属项目"); err != nil {
		t.Fatalf("SetupTwoWayRelation: %v", err)
	}

	// Open Relation picker UI on Projects/core and link both tasks.
	coreRec := store.RecordByPath("Projects/core.md")
	if coreRec == nil {
		t.Fatal("missing Projects/core.md record")
	}
	pRel, _ := store.Database().PropertyByID("p_rel")
	a.doc.dbView.OpenCellPopover(coreRec, *pRel, tt.Now())
	tt.Frame()

	for _, title := range []string{"核心任务 A", "核心任务 B"} {
		opt, ok := tt.AccessByRole(ui.RoleOption, title)
		if !ok {
			t.Fatalf("missing relation candidate %q, access=%+v", title, tt.AccessAll())
		}
		tt.ClickAt(opt.Bounds.X+opt.Bounds.W/2, opt.Bounds.Y+opt.Bounds.H/2)
		tt.Frame()
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()

	// Verify Rollup + Formula + Conditional Color update on screen and symmetric YAML links on disk.
	if !tt.HasText("🔥 高优先级") {
		t.Fatalf("expected Formula output 🔥 高优先级 rendered in Table, texts=%v", tt.Texts())
	}
	res := store.Query("", database.QueryOptions{Now: tt.Now()})
	if res.RowColors[coreRec.ID] != "red" {
		t.Fatalf("expected conditional row color red for %s, got %+v", coreRec.ID, res.RowColors)
	}
	for _, tp := range []string{"Tasks/task1.md", "Tasks/task2.md"} {
		raw, rerr := vault.Read(sp.Root, tp)
		if rerr != nil {
			t.Fatal(rerr)
		}
		if !strings.Contains(raw, "[[Projects/core]]") {
			t.Fatalf("expected symmetric reverse relation [[Projects/core]] in %s:\n%s", tp, raw)
		}
	}

	// Instantiate Record Template via Templates dropdown.
	tplBtn, ok := tt.AccessByRole(ui.RoleButton, "模板菜单")
	if !ok {
		t.Fatalf("missing 模板菜单 button, access=%+v", tt.AccessAll())
	}
	tt.ClickAt(tplBtn.Bounds.X+tplBtn.Bounds.W/2, tplBtn.Bounds.Y+tplBtn.Bounds.H/2)
	tt.Frame()
	if err := tt.Click("冲刺任务 {{date}}"); err != nil {
		t.Fatalf("click template item failed: %v", err)
	}
	tt.Frame()

	wantTitle := "冲刺任务 " + todayStr
	var tplRec *database.Record
	for _, r := range store.Records() {
		if r.Title == wantTitle {
			tplRec = r
			break
		}
	}
	if tplRec == nil {
		t.Fatalf("expected record created from template %q", wantTitle)
	}
	tplDisk, err := vault.Read(sp.Root, tplRec.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"icon: 🚀", "status: 进行中", "# " + wantTitle, "## 目标\n" + wantTitle + " - " + todayStr} {
		if !strings.Contains(tplDisk, want) {
			t.Fatalf("template record markdown missing %q:\n%s", want, tplDisk)
		}
	}
}

func TestDatabaseSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render database views")

	buildShowcase := func(t *testing.T, dark bool) (*App, *ui.Tester, string) {
		t.Helper()
		a, tt := newTestApp(t)
		tt.SetSize(1240, 780)
		if dark {
			tt.SetDark(true)
		}
		now := tt.Now()
		dayOff := func(d int) string { return now.AddDate(0, 0, d).Format("2006-01-02") }

		_, dbRel := seedAppDatabase(t, a, "产品路线图")
		writeSampleCoverPNG(t, filepath.Join(root(a), "产品路线图/assets/engine-cover.png"), 37, 99, 235, 124, 58, 237)
		writeSampleCoverPNG(t, filepath.Join(root(a), "产品路线图/assets/wb-cover.png"), 16, 185, 129, 14, 165, 233)
		recs := []struct {
			file, md string
		}{
			{"database-engine.md", "---\ncover: assets/engine-cover.png\nstatus: 进行中\ntags:\n  - 重要\ndate: " + dayOff(-3) + "\ndate_end: " + dayOff(3) + "\ndone: false\nscore: 95\n---\n\n# 多维数据库引擎 (Phase 3)\n\n实现日历、甘特时间轴视图与双向日期范围 Frontmatter 同步。\n\n- [x] Board / List / Gallery\n- [x] Calendar / Timeline\n- [ ] Chart / Feed / Form\n"},
			{"quick-notes.md", "---\nstatus: 已完成\ntags:\n  - 灵感\ndate: " + dayOff(-6) + "\ndate_end: " + dayOff(-2) + "\ndone: true\nscore: 80\n---\n\n# 快捷键与命令栏体验\n\n支持 `⌥⌘G` 一键创建数据库，与拼音首字母筛选。\n"},
			{"pdf-export.md", "---\nstatus: 已完成\ntags:\n  - 重要\ndate: " + dayOff(-9) + "\ndate_end: " + dayOff(-4) + "\ndone: true\nscore: 72\n---\n\n# 纯 Go 矢量 PDF 导出\n\n支持 Type 3 字形子集、目录大纲与超链接。\n"},
			{"whiteboard.md", "---\ncover: assets/wb-cover.png\nstatus: 未开始\ntags:\n  - 灵感\ndate: " + dayOff(2) + "\ndate_end: " + dayOff(8) + "\ndone: false\nscore: 64\n---\n\n# 手绘白板协作扩展\n\n双栖 `.excalidraw.svg` 文档编辑与组件素材库。\n"},
			{"ai-qa.md", "---\nstatus: 进行中\ntags:\n  - 重要\ndate: " + dayOff(-1) + "\ndate_end: " + dayOff(5) + "\ndone: false\nscore: 88\n---\n\n# 空间只读问答 Agent\n\n像阅读代码库一样搜索空间文档并输出行级引用。\n"},
			{"perf-bench.md", "---\nstatus: 未开始\ntags:\n  - 重要\ndone: false\nscore: 76\n---\n\n# 万行表格性能压测\n\n验证万级记录查询、虚拟滚动与增量缓存延迟。\n"},
		}
		for _, r := range recs {
			if err := vault.Write(root(a), "产品路线图/"+r.file, r.md); err != nil {
				t.Fatal(err)
			}
		}
		a.refreshTree()
		a.open(dbRel)
		if a.doc != nil && a.doc.isDatabase() {
			store := a.doc.dbView.Store
			db := store.Database()
			vc := a.doc.dbView.ActiveView().Clone()
			if vc.Table == nil {
				vc.Table = &database.TableViewConfig{}
			}
			vc.Table.Aggregations = map[string]database.AggregationType{
				"prop_title": database.AggCountAll,
				"prop_score": database.AggSum,
			}
			_ = store.UpdateView(vc)
			if bv, bidx := db.ViewByID("view_board"); bidx >= 0 {
				bvc := bv.Clone()
				if bvc.Board == nil {
					bvc.Board = &database.BoardViewConfig{}
				}
				bvc.Board.CoverSource = "cover"
				bvc.Board.ColorColumn = true
				bvc.Board.ShowCount = true
				_ = store.UpdateView(bvc)
			}
			vis := slices.Clone(db.Views[0].Properties)
			_ = store.AddView(database.ViewConfig{
				ID:         "view_list",
				Name:       "列表",
				Type:       database.ViewList,
				OpenMode:   dbview.OpenSidePeek,
				Properties: slices.Clone(vis),
			}, false)
			_ = store.AddView(database.ViewConfig{
				ID:         "view_gallery",
				Name:       "画廊",
				Type:       database.ViewGallery,
				OpenMode:   dbview.OpenSidePeek,
				Properties: slices.Clone(vis),
				Gallery:    &database.GalleryViewConfig{CardSize: "medium", CoverSource: "cover", ShowIcon: true},
			}, false)
			_ = store.AddView(database.ViewConfig{
				ID:         "view_calendar",
				Name:       "日历",
				Type:       database.ViewCalendar,
				OpenMode:   dbview.OpenSidePeek,
				Properties: slices.Clone(vis),
				Calendar:   &database.CalendarViewConfig{DatePropID: "prop_date", Mode: "month", ShowWeekends: true, WeekStart: 1},
			}, false)
			_ = store.AddView(database.ViewConfig{
				ID:         "view_timeline",
				Name:       "时间轴",
				Type:       database.ViewTimeline,
				OpenMode:   dbview.OpenSidePeek,
				Properties: slices.Clone(vis),
				Timeline:   &database.TimelineViewConfig{StartPropID: "prop_date", Zoom: "week", ShowTable: true},
			}, false)
			_ = store.AddView(database.ViewConfig{
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
			}, false)
			_ = store.AddView(database.ViewConfig{
				ID:         "view_feed",
				Name:       "信息流",
				Type:       database.ViewFeed,
				OpenMode:   dbview.OpenSidePeek,
				Properties: slices.Clone(vis),
				Feed:       &database.FeedViewConfig{ShowExcerpt: true, ShowImage: true},
			}, false)
			_ = store.AddView(database.ViewConfig{
				ID:       "view_form",
				Name:     "表单",
				Type:     database.ViewForm,
				OpenMode: dbview.OpenSidePeek,
				Form: &database.FormViewConfig{
					Title:       "新功能提案收集",
					Description: "填写标题、状态与优先级，提交后自动生成空间 Markdown 文档。",
					SubmitText:  "提交提案",
				},
			}, false)
			_ = a.doc.save()
		}
		tt.Frame()
		tt.Advance(400 * time.Millisecond)
		tt.Frame()
		return a, tt, dbRel
	}

	t.Run("light-table", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, false)
		if vault.Find(a.tree, "产品路线图.db.md") == nil {
			t.Fatal("expected 产品路线图.db.md in sidebar tree")
		}
		savePNG(t, filepath.Join(dir, "db-table-light.png"), tt)
	})

	t.Run("dark-table", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, true)
		_ = a
		savePNG(t, filepath.Join(dir, "db-table-dark.png"), tt)
	})

	t.Run("side-peek", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, false)
		rec := a.doc.dbView.Store.RecordByPath("产品路线图/database-engine.md")
		a.openRecordPeek(a.doc, a.doc.dbView.Store, a.state.ActiveSpace(), rec, dbview.OpenSidePeek)
		tt.Frame()
		savePNG(t, filepath.Join(dir, "db-side-peek.png"), tt)
	})

	t.Run("board-light", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, false)
		a.doc.dbView.SetActiveView("view_board")
		tt.Frame()
		tt.Frame()
		savePNG(t, filepath.Join(dir, "db-board-light.png"), tt)
	})

	t.Run("board-dark", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, true)
		a.doc.dbView.SetActiveView("view_board")
		tt.Frame()
		tt.Frame()
		savePNG(t, filepath.Join(dir, "db-board-dark.png"), tt)
	})

	t.Run("board-swimlanes", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, false)
		a.doc.dbView.SetActiveView("view_board")
		bv := a.doc.dbView.ActiveView().Clone()
		bv.Board.SubGroup = &database.GroupConfig{PropertyID: "prop_done"}
		_ = a.doc.dbView.Store.UpdateView(bv)
		tt.Frame()
		tt.Frame()
		savePNG(t, filepath.Join(dir, "db-board-swimlanes.png"), tt)
	})

	t.Run("list", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, false)
		a.doc.dbView.SetActiveView("view_list")
		tt.Frame()
		tt.Frame()
		savePNG(t, filepath.Join(dir, "db-list.png"), tt)
	})

	t.Run("gallery", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, false)
		a.doc.dbView.SetActiveView("view_gallery")
		tt.Frame()
		tt.Frame()
		savePNG(t, filepath.Join(dir, "db-gallery.png"), tt)
	})

	t.Run("calendar-month", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, false)
		a.doc.dbView.SetActiveView("view_calendar")
		tt.Frame()
		tt.Frame()
		savePNG(t, filepath.Join(dir, "db-calendar-month.png"), tt)
	})

	t.Run("calendar-week", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, false)
		a.doc.dbView.SetActiveView("view_calendar")
		cv := a.doc.dbView.ActiveView().Clone()
		cv.Calendar.Mode = "week"
		_ = a.doc.dbView.Store.UpdateView(cv)
		a.doc.dbView.CalendarUnscheduledOpen = true
		tt.Frame()
		tt.Frame()
		savePNG(t, filepath.Join(dir, "db-calendar-week.png"), tt)
	})

	t.Run("timeline-light", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, false)
		a.doc.dbView.SetActiveView("view_timeline")
		tt.Frame()
		tt.Frame()
		savePNG(t, filepath.Join(dir, "db-timeline-light.png"), tt)
	})

	t.Run("timeline-dark", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, true)
		a.doc.dbView.SetActiveView("view_timeline")
		tt.Frame()
		tt.Frame()
		savePNG(t, filepath.Join(dir, "db-timeline-dark.png"), tt)
	})

	t.Run("chart-bar", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, false)
		a.doc.dbView.SetActiveView("view_chart")
		tt.Frame()
		tt.Frame()
		savePNG(t, filepath.Join(dir, "db-chart-bar.png"), tt)
	})

	t.Run("chart-donut-dark", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, true)
		a.doc.dbView.SetActiveView("view_chart")
		cv := a.doc.dbView.ActiveView().Clone()
		cv.Chart.Kind = "donut"
		cv.Chart.Donut = true
		_ = a.doc.dbView.Store.UpdateView(cv)
		tt.Frame()
		tt.Frame()
		savePNG(t, filepath.Join(dir, "db-chart-donut-dark.png"), tt)
	})

	t.Run("chart-line", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, false)
		a.doc.dbView.SetActiveView("view_chart")
		cv := a.doc.dbView.ActiveView().Clone()
		cv.Chart.Kind = "line"
		cv.Chart.Smooth = true
		cv.Chart.XPropID = "prop_date"
		cv.Chart.YMode = "prop"
		cv.Chart.YPropID = "prop_score"
		cv.Chart.YAgg = database.AggSum
		_ = a.doc.dbView.Store.UpdateView(cv)
		tt.Frame()
		tt.Frame()
		savePNG(t, filepath.Join(dir, "db-chart-line.png"), tt)
	})

	t.Run("feed", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, false)
		a.doc.dbView.SetActiveView("view_feed")
		tt.Frame()
		tt.Frame()
		savePNG(t, filepath.Join(dir, "db-feed.png"), tt)
	})

	t.Run("form", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, false)
		a.doc.dbView.SetActiveView("view_form")
		tt.Frame()
		tt.Frame()
		savePNG(t, filepath.Join(dir, "db-form.png"), tt)
	})

	setupPhase5Showcase := func(a *App, tt *ui.Tester) {
		store := a.doc.dbView.Store
		_ = store.AddProperty(database.Property{
			ID:       "p_deps",
			Key:      "deps",
			Name:     "前置依赖",
			Type:     database.PropRelation,
			Relation: &database.RelationConfig{},
			Width:    150,
		})
		_ = store.AddProperty(database.Property{
			ID:     "p_roll",
			Key:    "dep_avg",
			Name:   "依赖均分",
			Type:   database.PropRollup,
			Rollup: &database.RollupConfig{RelationPropID: "p_deps", TargetPropID: "prop_score", Calculate: database.AggAverage},
			Number: &database.NumberConfig{Format: "number", ShowAs: "bar", DivideBy: 100, BarColor: "blue", ShowNumber: true},
			Width:  130,
		})
		_ = store.AddProperty(database.Property{
			ID:      "p_grade",
			Key:     "grade",
			Name:    "综合评级",
			Type:    database.PropFormula,
			Formula: &database.FormulaConfig{Expression: `if(prop("权重") >= 85, "S · 核心主线", if(prop("权重") >= 70, "A · 重要推进", "B · 待规划"))`},
			Width:   130,
		})
		if engineRec := store.RecordByPath("产品路线图/database-engine.md"); engineRec != nil {
			_, _ = store.SetCell(engineRec.ID, "p_deps", database.ListValue("产品路线图/quick-notes", "产品路线图/pdf-export"))
		}
		if aiRec := store.RecordByPath("产品路线图/ai-qa.md"); aiRec != nil {
			_, _ = store.SetCell(aiRec.ID, "p_deps", database.ListValue("产品路线图/quick-notes"))
		}
		colorRules := []database.ColorRule{
			{
				ID:    "cr_s",
				Color: "blue",
				Filter: database.FilterGroup{
					Conjunction: "and",
					Rules:       []database.FilterRule{{ID: "r1", PropertyID: "p_grade", Op: database.OpContains, Value: database.TextValue("核心主线")}},
				},
			},
			{
				ID:    "cr_todo",
				Color: "orange",
				Filter: database.FilterGroup{
					Conjunction: "and",
					Rules:       []database.FilterRule{{ID: "r2", PropertyID: "prop_status", Op: database.OpEquals, Value: database.TextValue("未开始")}},
				},
			},
		}
		for _, v := range store.Database().Views {
			vc := v.Clone()
			vc.Colors = slices.Clone(colorRules)
			_ = store.UpdateView(vc)
		}
		_ = store.AddTemplate(database.Template{
			ID:   "tpl_1",
			Name: "周版本规划 {{date}}",
			Icon: "🚀",
			Body: "## 本周目标\n",
		})
		_ = store.AddTemplate(database.Template{
			ID:   "tpl_2",
			Name: "性能压测单",
			Icon: "⚡",
			Body: "## 压测指标\n",
		})
		tt.Frame()
	}

	t.Run("phase5-table-computed-colors", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, false)
		setupPhase5Showcase(a, tt)
		if v := a.doc.dbView.ActiveView(); v != nil {
			vc := v.Clone()
			for i := range vc.Properties {
				switch vc.Properties[i].PropertyID {
				case "prop_tags", "prop_date", "prop_done":
					vc.Properties[i].Visible = false
				case "p_deps":
					vc.Properties[i].Width = 200
					vc.Properties[i].Wrap = true
				}
			}
			_ = a.doc.dbView.Store.UpdateView(vc)
		}
		tt.Frame()
		tt.Frame()
		savePNG(t, filepath.Join(dir, "db-phase5-table.png"), tt)
	})

	t.Run("phase5-formula-editor", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, false)
		setupPhase5Showcase(a, tt)
		if gp, idx := a.doc.dbView.Store.Database().PropertyByID("p_grade"); idx >= 0 {
			a.doc.dbView.OpenPropertyConfig(*gp)
		}
		tt.Frame()
		tt.Frame()
		savePNG(t, filepath.Join(dir, "db-phase5-formula.png"), tt)
	})

	t.Run("phase5-board-colors-templates", func(t *testing.T) {
		a, tt, _ := buildShowcase(t, true)
		setupPhase5Showcase(a, tt)
		a.doc.dbView.SetActiveView("view_board")
		a.doc.dbView.TemplatesOpen = true
		tt.Frame()
		tt.Frame()
		savePNG(t, filepath.Join(dir, "db-phase5-board-dark.png"), tt)
	})

	t.Run("phase6-embedded-linked-view", func(t *testing.T) {
		a, tt, dbRel := buildShowcase(t, false)
		setupPhase5Showcase(a, tt)
		snap := database.RenderGFMSnapshot("", a.doc.dbView.Store.Database(), a.doc.dbView.Store.Query("view_table", database.QueryOptions{Now: tt.Now()}))
		embedBlock := editor.FormatDatabaseEmbed(editor.DatabaseEmbedSpec{
			Target: dbRel,
			ViewID: "view_table",
			Height: 340,
		}, snap)
		noteMD := strings.Join([]string{
			"# Q4 研发周报与里程碑联动",
			"",
			"下面在普通 Markdown 正文中直接嵌入《产品路线图》的 **实时关联视图**（外部编辑器打开时自动降级为标准 GFM 表格）：",
			"",
			embedBlock,
			"",
			"结论：核心主线按期推进，所有公式汇总与条件着色均在嵌入式视图内实时同步。",
		}, "\n")
		if err := vault.Write(root(a), "研发周报.md", noteMD); err != nil {
			t.Fatal(err)
		}
		a.refreshTree()
		a.open("研发周报.md")
		tt.Frame()
		tt.Frame()
		savePNG(t, filepath.Join(dir, "db-phase6-embed.png"), tt)
	})
}

func TestDatabasePhase6EmbeddedLinkedViewAndExport(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1240, 820)
	sp := a.state.ActiveSpace()

	_, dbRel := seedAppDatabase(t, a, "产品路线图")
	if err := vault.Write(sp.Root, "产品路线图/alpha.md", "---\nstatus: 进行中\ntags:\n  - 核心\nscore: 95\n---\n\n# Alpha Core\n"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "产品路线图/beta.md", "---\nstatus: 已完成\ntags:\n  - 发布\nscore: 80\n---\n\n# Beta Polish\n"); err != nil {
		t.Fatal(err)
	}
	embedBlock := editor.FormatDatabaseEmbed(editor.DatabaseEmbedSpec{
		Target: dbRel,
		ViewID: "view_table",
		Height: 360,
	}, "| 名称 | 状态 |\n| --- | --- |\n| Alpha Core | 进行中 |\n")
	weeklyMD := "# 周会同步\n\n" + embedBlock + "\n\n后续跟踪事项。\n"
	if err := vault.Write(sp.Root, "Weekly.md", weeklyMD); err != nil {
		t.Fatal(err)
	}

	a.refreshTree()
	a.open("Weekly.md")
	tt.Frame()
	tt.Frame()

	for _, want := range []string{"关联视图", "Alpha Core", "Beta Polish"} {
		if !tt.HasText(want) {
			t.Fatalf("expected embedded Linked View text %q in editor, texts=%v", want, tt.Texts())
		}
	}

	// Verify HTML export produces semantic table fallback rather than dropping the block.
	htmlOut := editor.ExportHTMLPage(a.doc.ed.Committed(), editor.HTMLTheme{})
	if !strings.Contains(htmlOut, `<figure class="arcmd-database">`) || !strings.Contains(htmlOut, "Alpha Core") {
		t.Fatalf("expected HTML export table degradation for embedded database, got:\n%s", htmlOut)
	}

	// Verify Vector PDF export renders without error and produces valid PDF output.
	a.exportPDF()
	finishTestPDF(t, a, tt)
	pdfBytes, err := os.ReadFile(filepath.Join(root(a), "Weekly.pdf"))
	if err != nil {
		t.Fatalf("expected Weekly.pdf exported: %v", err)
	}
	if !bytes.HasPrefix(pdfBytes, []byte("%PDF-")) {
		t.Fatalf("expected valid %%PDF- output, got %q", pdfBytes[:min(16, len(pdfBytes))])
	}

	// Click "全屏打开" inside the embedded Linked View card header -> opens 产品路线图.db.md.
	fullBtn, ok := tt.AccessByRole(ui.RoleButton, "全屏打开")
	if !ok {
		t.Fatalf("missing 全屏打开 button in embedded Linked View, access=%+v", tt.AccessAll())
	}
	tt.ClickAt(fullBtn.Bounds.X+fullBtn.Bounds.W/2, fullBtn.Bounds.Y+fullBtn.Bounds.H/2)
	tt.Frame()
	if a.doc == nil || a.doc.rel != dbRel {
		t.Fatalf("expected full-page navigation to %q, got %+v", dbRel, a.doc)
	}
}

func TestDatabasePhase6SearchAndQACitation(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1200, 760)
	sp := a.state.ActiveSpace()

	db := database.NewDefault("Tasks", database.SourceConfig{Type: database.SourceFolder})
	db.Properties = append(db.Properties,
		database.Property{ID: "t_score", Key: "score", Name: "权重", Type: database.PropNumber, Width: 90},
		database.Property{ID: "t_double", Key: "double", Name: "双倍分", Type: database.PropFormula, Formula: &database.FormulaConfig{Expression: `prop("权重") * 2`}, Width: 90},
	)
	for i := range db.Views {
		db.Views[i].Properties = append(db.Views[i].Properties,
			database.ViewProperty{PropertyID: "t_score", Visible: true, Width: 90},
			database.ViewProperty{PropertyID: "t_double", Visible: true, Width: 90},
		)
	}
	rawDB, err := database.EncodeDatabaseMarkdown(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Tasks.db.md", rawDB); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Tasks/perf.md", "---\nstatus: 进行中\ntags:\n  - 性能关键里程碑\nscore: 42\n---\n\n# 压测专项执行\n\n正文中不重复标签关键词。\n"); err != nil {
		t.Fatal(err)
	}

	a.refreshTree()
	a.refreshSearch()
	hits := a.liveIndex().SearchMulti("性能关键里程碑", nil, 10)
	if !slices.ContainsFunc(hits, func(h search.Hit) bool { return h.Path == "Tasks/perf.md" }) {
		t.Fatalf("expected Tasks/perf.md found via frontmatter search index, got hits=%+v", hits)
	}

	// Open Tasks.db.md and confirm qaSpace serves live evaluated Formula (84) and frontmatter on perf.md.
	a.open("Tasks.db.md")
	tt.Frame()

	fs := a.qaSpace(sp.ID)
	dbPage, err := fs.Page("Tasks.db.md")
	if err != nil {
		t.Fatalf("fs.Page(Tasks.db.md): %v", err)
	}
	recLine := 0
	for i, line := range dbPage.Lines {
		if strings.Contains(line, "压测专项执行") && strings.Contains(line, "84") {
			recLine = i + 1
			break
		}
	}
	if recLine == 0 {
		t.Fatalf("expected structured DB QA row with live Formula output 84, lines=%v", dbPage.Lines)
	}

	mdPage, err := fs.Page("Tasks/perf.md")
	if err != nil {
		t.Fatalf("fs.Page(Tasks/perf.md): %v", err)
	}
	if !strings.Contains(strings.Join(mdPage.Lines, "\n"), "性能关键里程碑") {
		t.Fatalf("expected frontmatter exposed in QA page for Tasks/perf.md, lines=%v", mdPage.Lines)
	}

	// Citing the database record row opens Side Peek directly on that record.
	a.open("Welcome.md")
	tt.Frame()
	a.openCitation(sp.ID, agent.Citation{Path: "Tasks.db.md", From: recLine, To: recLine})
	tt.Frame()
	if !a.dbPeek.open || a.doc == nil || a.doc.rel != "Tasks.db.md" {
		t.Fatalf("expected Side Peek opened on cited DB record, dbPeek=%+v doc=%+v", a.dbPeek, a.doc)
	}
	rec := a.doc.dbView.Store.RecordByPath("Tasks/perf.md")
	if rec == nil || a.dbPeek.recordID != rec.ID {
		t.Fatalf("expected Side Peek recordID=%v, got %q", rec, a.dbPeek.recordID)
	}
}

func TestDatabasePhase6CSVAndNotionImportExport(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()

	_, dbRel := seedAppDatabase(t, a, "OKR看板")
	if err := vault.Write(sp.Root, "OKR看板/goal1.md", "---\nstatus: 进行中\nscore: 92\n---\n\n# 指标达成率\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open(dbRel)
	tt.Frame()

	// 1. Export active view to CSV beside OKR看板.db.md.
	csvRel, err := a.exportDatabaseCSV(a.doc)
	if err != nil {
		t.Fatalf("exportDatabaseCSV: %v", err)
	}
	csvRaw, err := vault.Read(sp.Root, csvRel)
	if err != nil {
		t.Fatalf("read exported CSV %q: %v", csvRel, err)
	}
	if !strings.HasPrefix(csvRaw, "\xEF\xBB\xBF") || !strings.Contains(csvRaw, "指标达成率") || !strings.Contains(csvRaw, "92") {
		t.Fatalf("unexpected exported CSV contents:\n%s", csvRaw)
	}

	// 2. Import CSV both as a brand new database and appending into existing OKR看板.db.md.
	extDir := t.TempDir()
	newCSV := filepath.Join(extDir, "Sprint 导入.csv")
	if err := os.WriteFile(newCSV, []byte("Name,Status,Done\nTask X,进行中,Yes\nTask Y,已完成,No\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	newDBRel, ires, err := a.importCSVFromPath(sp, "", newCSV, nil)
	if err != nil || ires.CreatedRecords != 2 || !vault.IsDatabase(newDBRel) {
		t.Fatalf("importCSVFromPath new DB failed: rel=%q res=%+v err=%v", newDBRel, ires, err)
	}

	a.open(dbRel)
	tt.Frame()
	appendCSV := filepath.Join(extDir, "more.csv")
	if err := os.WriteFile(appendCSV, []byte("名称,权重\n新增目标,77\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, ares, err := a.importCSVFromPath(sp, "", appendCSV, a.doc)
	if err != nil || ares.CreatedRecords != 1 {
		t.Fatalf("importCSVFromPath append failed: res=%+v err=%v", ares, err)
	}
	if len(a.doc.dbView.Store.Records()) != 2 {
		t.Fatalf("expected 2 records in OKR看板 after append CSV, got %d", len(a.doc.dbView.Store.Records()))
	}

	// 3. Import Notion export ZIP through App wrapper.
	zipPath := filepath.Join(extDir, "notion.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	entries := map[string]string{
		"Roadmap aabbccddeeff00112233445566778899_all.csv":                                                "Name,Status\nLaunch Day,进行中\n",
		"Roadmap aabbccddeeff00112233445566778899/Launch Day 1234567890abcdef1234567890abcdef.md":         "# Launch Day\n\nStatus: 进行中\n\n![Poster](Launch%20Day%201234567890abcdef1234567890abcdef/poster.png)\n",
		"Roadmap aabbccddeeff00112233445566778899/Launch Day 1234567890abcdef1234567890abcdef/poster.png": "IMGDATA",
	}
	for name, body := range entries {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	_ = zw.Close()
	_ = zf.Close()

	nres, err := a.importNotionFromPath(sp, "", zipPath)
	if err != nil {
		t.Fatalf("importNotionFromPath: %v", err)
	}
	if len(nres.Databases) != 1 || nres.Records != 1 || nres.Assets != 1 {
		t.Fatalf("unexpected notion import summary: %+v", nres)
	}
	if a.doc == nil || a.doc.rel != nres.Databases[0] {
		t.Fatalf("expected app to open imported Notion database %q, got %+v", nres.Databases[0], a.doc)
	}
}
