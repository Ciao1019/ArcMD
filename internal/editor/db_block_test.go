package editor_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/dbview"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/egoist/mygo/ui"
)

func seedEmbedTestStore(t *testing.T) *database.Store {
	t.Helper()
	db := database.NewDefault("Sprint Board", database.SourceConfig{Type: database.SourceInline})
	db.InlineRows = []*database.Record{
		{
			ID:    "rec_1",
			Title: "Implement Linked View",
			Props: map[string]database.Value{
				"prop_status": database.TextValue("进行中"),
				"prop_tags":   database.ListValue("重要"),
			},
		},
		{
			ID:    "rec_2",
			Title: "Ship CSV Importer",
			Props: map[string]database.Value{
				"prop_status": database.TextValue("已完成"),
				"prop_tags":   database.ListValue("灵感"),
			},
		},
	}
	return database.NewStore("", "sprint.db.md", db)
}

func TestDBBlockMarkdownRoundTripAndHTMLDegradation(t *testing.T) {
	store := seedEmbedTestStore(t)
	snap := database.RenderGFMSnapshot("", store.Database(), store.Query("view_table", database.QueryOptions{}))
	blockMD := editor.FormatDatabaseEmbed(editor.DatabaseEmbedSpec{
		Target: "sprint.db.md",
		ViewID: "view_table",
		Height: 360,
	}, snap)

	src := "# Weekly Report\n\n" + blockMD + "\n\n<!-- plain comment -->\n\nClosing paragraph.\n"
	doc := editor.ImportMarkdown(src)
	if len(doc.Blocks) != 4 {
		t.Fatalf("expected 4 top-level blocks (heading, db raw, plain comment raw, paragraph), got %d", len(doc.Blocks))
	}
	spec, parsedSnap, ok := editor.ParseDatabaseEmbed(doc.Blocks[1].Text())
	if !ok || spec.Target != "sprint.db.md" || spec.ViewID != "view_table" || spec.Height != 360 {
		t.Fatalf("unexpected parsed embed spec: ok=%v spec=%+v", ok, spec)
	}
	if !strings.Contains(parsedSnap, "Implement Linked View") {
		t.Fatalf("expected snapshot rows preserved, got:\n%s", parsedSnap)
	}

	exported := editor.ExportMarkdown(doc)
	doc2 := editor.ImportMarkdown(exported)
	if len(doc2.Blocks) != 4 || !strings.Contains(doc2.Blocks[1].Text(), "<!-- /arcmd-db:view -->") {
		t.Fatalf("round-trip lost database embed structure:\n%s", exported)
	}

	// Verify HTML export degrades embedded Linked View to semantic <figure><table> while still skipping plain HTML comment.
	htmlOut := editor.ExportHTMLPage(doc, editor.HTMLTheme{})
	if !strings.Contains(htmlOut, `<figure class="arcmd-database">`) || !strings.Contains(htmlOut, "Implement Linked View") {
		t.Fatalf("expected semantic HTML table fallback for embedded database, got:\n%s", htmlOut)
	}
	if strings.Contains(htmlOut, "plain comment") {
		t.Fatalf("expected plain HTML comment stripped from HTML export, got:\n%s", htmlOut)
	}

	// Verify empty snapshot embed round-trips cleanly.
	emptyEmbed := editor.FormatDatabaseEmbed(editor.DatabaseEmbedSpec{Target: "empty.db.md"}, "")
	emptyDoc := editor.ImportMarkdown("# Empty Embed\n\n" + emptyEmbed + "\n")
	if len(emptyDoc.Blocks) != 2 {
		t.Fatalf("expected 2 blocks for empty embed, got %d", len(emptyDoc.Blocks))
	}
	if es, esnap, ok := editor.ParseDatabaseEmbed(emptyDoc.Blocks[1].Text()); !ok || es.Target != "empty.db.md" || esnap != "" {
		t.Fatalf("empty embed parse failed: ok=%v spec=%+v snap=%q", ok, es, esnap)
	}
}

func TestDBBlockInteractiveLiveViewSlashPickerAndVectorPDF(t *testing.T) {
	store := seedEmbedTestStore(t)
	openedTarget := ""
	openedView := ""
	createdCalled := false

	ed := editor.New("# Embedded Demo\n\nIntro paragraph.")
	opts := editor.Options{
		Width: 820,
		Theme: ui.LightTheme(),
		LoadDatabase: func(target string) (*database.Store, dbview.Options, uint64, error) {
			return store, dbview.Options{}, store.Revision(), nil
		},
		OpenDatabase: func(target, viewID string, split bool) {
			openedTarget, openedView = target, viewID
		},
		ListDatabases: func() []database.SpaceDBInfo {
			return []database.SpaceDBInfo{{RelPath: "sprint.db.md", Title: "Sprint Board"}}
		},
		CreateDatabase: func(title string) (string, string, string, error) {
			createdCalled = true
			return "created.db.md", "view_table", "| Name |\n| --- |\n| Item |\n", nil
		},
		DatabaseSnapshot: func(target, viewID string) string {
			return database.RenderGFMSnapshot("", store.Database(), store.Query(viewID, database.QueryOptions{}))
		},
	}

	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(ui.LightTheme())
		ui.Column(c.Key("host")).Fill().Children(func() {
			editor.View(c, ed, opts)
		})
	}, 960, 760)
	tt.SetLocale("zh-Hans")
	ed.RequestFocus = true
	tt.Frame()

	// Move to trailing empty paragraph and open slash menu -> search "db" -> choose "关联数据库视图".
	ed.SetSelection(ed.Len(), ed.Len())
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	ed.ReplaceText(nil, "/db")
	tt.Frame()

	if !tt.HasText("关联数据库视图") {
		t.Fatalf("expected 关联数据库视图 in slash menu, texts=%v", tt.Texts())
	}
	if err := tt.Click("关联数据库视图"); err != nil {
		t.Fatalf("click 关联数据库视图: %v", err)
	}
	tt.Frame()

	if !tt.HasText("Sprint Board") {
		t.Fatalf("expected Sprint Board in inline database picker, texts=%v", tt.Texts())
	}
	opt, ok := tt.AccessByRole(ui.RoleOption, "Sprint Board")
	if !ok {
		t.Fatalf("missing Sprint Board option, access=%+v", tt.AccessAll())
	}
	tt.ClickAt(opt.Bounds.X+opt.Bounds.W/2, opt.Bounds.Y+opt.Bounds.H/2)
	tt.Frame()
	tt.Frame()

	// Live embedded dbview.View should display database records directly inside the editor.
	for _, want := range []string{"关联视图", "Implement Linked View", "Ship CSV Importer"} {
		if !tt.HasText(want) {
			t.Fatalf("expected %q rendered in embedded database card, texts=%v", want, tt.Texts())
		}
	}

	// Switch to Board view inside the embedded card -> should update block's spec.ViewID cleanly without dirtying store.ActiveView.
	boardTab, ok := tt.AccessByRole(ui.RoleTab, "看板")
	if !ok {
		t.Fatalf("missing 看板 tab in embedded view, access=%+v", tt.AccessAll())
	}
	tt.ClickAt(boardTab.Bounds.X+boardTab.Bounds.W/2, boardTab.Bounds.Y+boardTab.Bounds.H/2)
	tt.Frame()
	tt.Frame()

	if store.Database().ActiveView != "view_table" {
		t.Fatalf("expected LocalView tab switch not to mutate shared store.ActiveView, got %q", store.Database().ActiveView)
	}
	mdNow := editor.ExportMarkdown(ed.Committed())
	if !strings.Contains(mdNow, `"view":"view_board"`) {
		t.Fatalf("expected embedded spec view updated to view_board in Markdown:\n%s", mdNow)
	}

	// Clicking "全屏打开" should invoke OpenDatabase with target & current embedded viewID.
	fullBtn, ok := tt.AccessByRole(ui.RoleButton, "全屏打开")
	if !ok {
		t.Fatalf("missing 全屏打开 button, access=%+v", tt.AccessAll())
	}
	tt.ClickAt(fullBtn.Bounds.X+fullBtn.Bounds.W/2, fullBtn.Bounds.Y+fullBtn.Bounds.H/2)
	tt.Frame()
	if openedTarget != "sprint.db.md" || openedView != "view_board" {
		t.Fatalf("expected OpenDatabase(sprint.db.md, view_board), got (%q, %q)", openedTarget, openedView)
	}

	// Test "内嵌数据库" creation shortcut via slash menu too (caret on trailing paragraph).
	ed.RequestFocus = true
	tt.Frame()
	ed.SetSelection(ed.Len(), ed.Len())
	tt.Frame()
	ed.ReplaceText(nil, "/db")
	tt.Frame()
	if err := tt.Click("内嵌数据库"); err != nil {
		t.Fatalf("click 内嵌数据库: %v", err)
	}
	tt.Frame()
	if !createdCalled || !strings.Contains(editor.ExportMarkdown(ed.Committed()), `"target":"created.db.md"`) {
		t.Fatalf("expected 内嵌数据库 to invoke CreateDatabase and insert created.db.md")
	}

	// Verify print pagination and Vector PDF output succeed with database table rendered.
	printEd := editor.FromOwnedDocument(editor.ImportMarkdown(editor.ExportMarkdown(ed.Committed())))
	printOpts := opts
	printOpts.Width = 500
	var tops []float32
	ui.Render(func(c *ui.Context) {
		tops = printEd.Paginate(printOpts, 700)
	}, 1, 1, 1)
	if len(tops) == 0 {
		t.Fatal("expected at least 1 paginated PDF page")
	}
	var out bytes.Buffer
	w := pdf.NewWriter(&out, "Embedded Demo")
	w.ReservePages(len(tops))
	vp := w.NewVectorPage(pdf.A4Width, pdf.A4Height)
	bottom := tops[0] + 700
	if len(tops) > 1 {
		bottom = tops[1]
	}
	printEd.ExportVectorPage(vp, printOpts, tops[0], bottom, 0, len(tops), tops, 500, 780, 40)
	if err := w.FinishVectorPage(vp); err != nil {
		t.Fatalf("FinishVectorPage: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Writer.Close: %v", err)
	}
	if !bytes.HasPrefix(out.Bytes(), []byte("%PDF-")) {
		t.Fatalf("expected %%PDF- header, got %q", out.Bytes()[:min(16, out.Len())])
	}
}
