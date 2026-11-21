package app

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const mapSample = "# 阅读笔记\n\n普通正文，包含 ==重点==、**粗体** 和 `行内代码`。\n\n## 算法\n\n```go\nfunc sum(a, b int) int {\n    return a + b\n}\n```\n\n## 公式\n\n$$\nE = mc^2\n$$\n\n## 实验\n\n| 组别 | 得分 |\n| --- | ---: |\n| A | 12 |\n| B | 24 |"

func TestDocumentMapToggleAndJump(t *testing.T) {
	a, tt := newTestApp(t)
	openSample(t, a, "Map.arcmd", mapSample)
	tt.Frame()
	rev := a.doc.ed.Revision()
	if err := tt.Click("思维导图"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if a.doc.mindMap == nil {
		t.Fatal("missing map")
	}
	// Selecting a paragraph in the document brings that node into view.
	b := a.doc.ed.Doc.Blocks[1]
	a.doc.ed.Engine.FocusBlock(b.ID)
	tt.Frame()
	selected, exists := tt.AccessByRole(ui.RoleButton, editor.BlockExcerpt(b))
	if !exists || !selected.Checked {
		t.Fatal("left caret did not select the corresponding right node")
	}
	r, ok := tt.Find(editor.BlockExcerpt(b))
	if !ok || r.W < 5 {
		t.Fatal("missing paragraph node", r)
	}
	// Move the caret away without another frame, then click the card.
	a.doc.ed.Engine.FocusBlock(a.doc.ed.Doc.Blocks[0].ID)
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if got := a.doc.ed.Engine.LeafAt(a.doc.ed.Caret).Owner; got != b.ID {
		t.Fatalf("map click selected %q; want %q", got, b.ID)
	}
	if got := a.doc.ed.Revision(); got != rev {
		t.Fatal("navigation edited document", rev, got)
	}
}
func TestDocumentMapSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the mind map")
	a, _ := newTestApp(t)
	openSample(t, a, "Map.arcmd", mapSample)
	a.doc.ed.Doc.Blocks[2].Runs[0].Style.Annotation = "box"
	if !a.folded() {
		a.toggleSidebar()
	}
	toggleDocumentMap(a.doc)
	tt := ui.NewTester(a.View, 1480, 880)
	tt.Frame()
	tt.Frame()
	for _, name := range []string{"document-map-one.png", "document-map-both.png", "document-map-dark.png", "document-map-long.png"} {
		switch name {
		case "document-map-both.png":
			if err := tt.Click("双侧布局"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
		case "document-map-dark.png":
			tt.SetDark(true)
			settle(tt)
		case "document-map-long.png":
			tt.SetDark(false)
			var long strings.Builder
			long.WriteString("# 长内容文档\n\n## 超长代码块\n\n```go\n")
			for i := 1; i <= 120; i++ {
				long.WriteString("step := processBatchItem(item)\n")
			}
			long.WriteString("```\n\n## 超长段落\n\n")
			long.WriteString(strings.Repeat("这是一段较长的技术说明文字，包含公式 $E=mc^2$ 与示例说明。", 8))
			long.WriteString("\n\n## 大表格\n\n| 列 A | 列 B | 列 C |\n| --- | --- | --- |\n")
			for r := 1; r <= 30; r++ {
				long.WriteString("| Row | Data | Value |\n")
			}
			openSample(t, a, "LongMap.arcmd", long.String())
			if !a.folded() {
				a.toggleSidebar()
			}
			toggleDocumentMap(a.doc)
			settle(tt)
			if err := tt.Click("单侧布局"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
		}
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		closeErr := f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
}

func TestDocumentMapLayoutPreference(t *testing.T) {
	a, tt := newTestApp(t)
	openSample(t, a, "Layout.arcmd", "# Root\n\n## First\n\nOne\n\n## Second\n\nTwo")
	tt.Frame()
	if err := tt.Click("思维导图"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if a.prefs.MindMapBothSides {
		t.Fatal("new installations must open on one side")
	}
	if err := tt.Click("双侧布局"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if choice, ok := tt.AccessByRole(ui.RoleRadio, "双侧布局"); !ok || !choice.Checked {
		t.Fatal("two-sided segment not selected")
	}
	saved, err := a.prefStore.Load()
	if err != nil || !saved.MindMapBothSides {
		t.Fatalf("layout not saved: %v", err)
	}
	if err := tt.Click("思维导图"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("思维导图"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("单侧布局"); err != nil {
		t.Fatal(err)
	}
	if a.prefs.MindMapBothSides {
		t.Fatal("single-side toggle did not clear preference")
	}
}

func TestDocumentMapFollowsCaretIntoTable(t *testing.T) {
	a, tt := newTestApp(t)
	openSample(t, a, "Navigation.arcmd", "# Navigation\n\n"+strings.Repeat("A paragraph.\n\n", 45)+"## End\n\n| A | B |\n| --- | --- |\n| 1 | 2 |")
	tt.Frame()
	if err := tt.Click("思维导图"); err != nil {
		t.Fatal(err)
	}
	d := a.doc.ed
	d.SetSelection(d.Len(), d.Len())
	tt.Frame()
	table := d.Doc.Blocks[len(d.Doc.Blocks)-1]
	item, ok := tt.AccessByRole(ui.RoleButton, editor.BlockExcerpt(table))
	if !ok || !item.Checked || item.Bounds.H <= 0 || item.Bounds.W <= 0 {
		t.Fatalf("last cell did not reveal its table in the dock: %+v", item)
	}
}

func TestMapDividerAdjustsRichWidth(t *testing.T) {
	a, tt := newTestApp(t)
	openSample(t, a, "Divider.arcmd", mapSample)
	tt.Frame()
	if err := tt.Click("思维导图"); err != nil {
		t.Fatal(err)
	}
	h, ok := tt.Find("调整思维导图宽度")
	if !ok {
		t.Fatal("missing divider")
	}
	start := a.doc.mapFraction
	x, y := h.X+h.W/2, h.Y+h.H/2
	tt.Press(x, y)
	tt.Move(x-60, y)
	tt.Release(x-60, y)
	tt.Frame()
	if a.doc.mapFraction <= start {
		t.Fatalf("dock fraction %v → %v", start, a.doc.mapFraction)
	}
}

func TestDocumentMapRefreshesSavedWhiteboard(t *testing.T) {
	a, _ := newTestApp(t)
	const asset = "assets/diagram.excalidraw.svg"
	scene := whiteboard.NewScene()
	shape := whiteboard.NewElement(whiteboard.KindRectangle, 0, 0, 90, 60)
	shape.FillStyle = whiteboard.FillSolid
	scene.Elements = append(scene.Elements, shape)
	save := func(color string) {
		t.Helper()
		scene.Elements[0].FillColor = color
		svg, err := encodeWhiteboardFile(asset, scene)
		if err != nil {
			t.Fatal(err)
		}
		if err = vault.Write(root(a), asset, svg); err != nil {
			t.Fatal(err)
		}
	}
	save("red")
	openSample(t, a, "Board.arcmd", "# Board\n\n![Diagram]("+asset+")")
	toggleDocumentMap(a.doc)
	tt := ui.NewTester(a.View, 1480, 880)
	count := func() (red, blue int) {
		t.Helper()
		tt.Frame()
		box, ok := tt.Find("思维导图画布")
		if !ok {
			t.Fatal("missing map viewport")
		}
		im := tt.Image()
		for y := int(box.Y); y < int(box.Y+box.H); y++ {
			for x := int(box.X); x < int(box.X+box.W); x++ {
				c := im.RGBAAt(x, y)
				if int(c.R)-int(c.B) > 50 {
					red++
				}
				if int(c.B)-int(c.R) > 50 {
					blue++
				}
			}
		}
		return
	}
	r1, b1 := count()
	v, _ := tt.Find("思维导图画布")
	tt.Pinch(v.X+v.W/2, v.Y+v.H/2, 1.03)
	red, _ := count()
	if red < 100 {
		t.Fatalf("whiteboard disappeared during pinch: %d red pixels, initially %d", red, r1)
	}
	rev := a.doc.ed.Revision()
	save("blue")
	a.filesChanged(a.doc.space.ID, []mygo.FileEvent{{Path: asset, Op: mygo.FileWrite}})
	r2, b2 := count()
	if r1-r2 < 100 || b2-b1 < 100 || a.doc.ed.Revision() != rev {
		t.Fatalf("same-path SVG update without document edits: red/blue %d/%d -> %d/%d", r1, b1, r2, b2)
	}
}

func TestDocumentMapShortcutPaletteCloseAndResponsive(t *testing.T) {
	a, tt := newTestApp(t)
	openSample(t, a, "MapShortcuts.arcmd", mapSample)
	focusDocument(a, tt)
	rev := a.doc.ed.Revision()

	// ⌥⌘M toggles the mind map even while the writing surface has focus.
	tt.Key(ui.Cmd|ui.Alt, ui.KeyM)
	tt.Frame()
	if a.doc.mindMap == nil {
		t.Fatal("⌥⌘M did not open the mind map while editing")
	}
	if a.doc.ed.Revision() != rev {
		t.Fatal("⌥⌘M typed into the document")
	}

	// Close button inside the mind map header closes it and updates prefs.
	if err := tt.Click("关闭思维导图"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.doc.mindMap != nil || a.prefs.MindMapOpen {
		t.Fatal("header close button did not close mind map")
	}

	// Command Palette reaches mind map toggle and layout commands.
	a.openPalette()
	a.palette.query = "展开思维导图"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.doc.mindMap == nil {
		t.Fatal("command palette did not open mind map")
	}
	a.openPalette()
	a.palette.query = "思维导图：切换为双侧布局"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !a.prefs.MindMapBothSides {
		t.Fatal("command palette did not toggle mind map layout")
	}

	// Narrow width (< 580 card width) stacks editor and mind map vertically.
	tt.SetSize(520, 760)
	settle(tt)
	if _, ok := tt.Find("调整思维导图高度"); !ok {
		t.Fatal("narrow width did not switch mind map splitter to vertical stack")
	}

	// Multi-pane splits do not auto-open mind map across newly opened split panes.
	a.prefs.MindMapOpen = true
	a.openSplit(a.state.ActiveSpace(), "Journal/Ideas.md", false, false)
	tt.Frame()
	if a.split != nil && len(a.split.panes) > 1 {
		if second := a.split.panes[1].doc; second != nil && second.mindMap != nil {
			t.Fatal("secondary split pane auto-opened mind map")
		}
	}
}

func TestDocumentMapNodeAndCanvasContextMenus(t *testing.T) {
	a, tt := newTestApp(t)
	sample := strings.Join([]string{
		"# 项目总览",
		"",
		"## 架构设计",
		"",
		"### 网关层",
		"",
		"负责流量转发。",
		"",
		"## 开发计划",
		"",
		"- 搭建脚手架",
		"- 编写测试",
	}, "\n")
	openSample(t, a, "Plan.arcmd", sample)
	if !a.folded() {
		a.toggleSidebar()
	}
	tt.Frame()
	if err := tt.Click("思维导图"); err != nil {
		t.Fatal(err)
	}
	settle(tt)

	// 1. Focus branch (drill down) & breadcrumb exit.
	if err := tt.RightClick("架构设计"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("聚焦此分支"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.doc.mindMap.FocusRoot() == "" {
		t.Fatal("focus branch did not set focusRoot")
	}
	if _, ok := tt.Find("退出聚焦"); !ok {
		t.Fatal("missing focus breadcrumb exit button")
	}
	if _, ok := tt.Find("开发计划"); ok {
		t.Fatal("sibling branch should be hidden while focused")
	}
	if err := tt.Click("退出聚焦"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.doc.mindMap.FocusRoot() != "" {
		t.Fatal("exit focus did not clear focusRoot")
	}

	// 2. Move branch down & undo.
	if err := tt.RightClick("架构设计"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("下移分支"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := a.doc.ed.Doc.Blocks[1].Text(); got != "开发计划" {
		t.Fatalf("after move down, first section is %q; want 开发计划", got)
	}
	a.doc.ed.Undo()
	tt.Frame()
	if got := a.doc.ed.Doc.Blocks[1].Text(); got != "架构设计" {
		t.Fatalf("after undo move down, first section is %q; want 架构设计", got)
	}

	// 3. Add child node & undo.
	beforeBlocks := len(a.doc.ed.Doc.Blocks)
	if err := tt.RightClick("架构设计"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("添加", "添加子节点"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.doc.ed.Doc.Blocks) != beforeBlocks+1 {
		t.Fatalf("add child node produced %d blocks; want %d", len(a.doc.ed.Doc.Blocks), beforeBlocks+1)
	}
	a.doc.ed.Undo()
	tt.Frame()

	// 4. Delete node only (promotes child heading H3 -> H2) & undo.
	if err := tt.RightClick("架构设计"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("仅删除当前节点"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := a.doc.ed.Doc.Blocks[1]; got.Text() != "网关层" || got.Kind != "heading" || got.Level != 2 {
		t.Fatalf("delete only node left %+v; want H2 网关层", got)
	}
	a.doc.ed.Undo()
	tt.Frame()

	// 5. Delete whole branch & undo.
	if err := tt.RightClick("开发计划"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("删除整个分支"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if strings.Contains(editor.ExportMarkdown(a.doc.ed.Doc), "搭建脚手架") {
		t.Fatal("delete branch did not remove child list items")
	}
	a.doc.ed.Undo()
	tt.Frame()

	// 6. Extract branch to new document (creates .md + replaces with [[wikilink]]).
	if err := tt.RightClick("架构设计"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("提取为新文档"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !strings.Contains(editor.ExportMarkdown(a.doc.ed.Doc), "[[架构设计]]") {
		t.Fatalf("extract branch did not insert wikilink: %s", editor.ExportMarkdown(a.doc.ed.Doc))
	}
	root, _, err := fileLocation(a.doc.space, a.doc.rel)
	if err != nil {
		t.Fatal(err)
	}
	extracted, err := os.ReadFile(filepath.Join(root, "架构设计.md"))
	if err != nil {
		t.Fatalf("extracted document missing: %v", err)
	}
	if !strings.HasPrefix(string(extracted), "# 架构设计") || !strings.Contains(string(extracted), "## 网关层") {
		t.Fatalf("extracted document did not normalize headings:\n%s", string(extracted))
	}
	a.doc.ed.Undo()
	tt.Frame()

	// 7. Export branch as PNG & SVG.
	if err := tt.RightClick("开发计划"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("导出分支为", "PNG 图片…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	pngFile, err := os.Open(filepath.Join(root, "开发计划.png"))
	if err != nil {
		t.Fatalf("exported PNG missing: %v", err)
	}
	img, err := png.Decode(pngFile)
	pngFile.Close()
	if err != nil || img.Bounds().Dx() < 20 || img.Bounds().Dy() < 20 {
		t.Fatalf("invalid exported PNG: %v", err)
	}

	if err := tt.RightClick("开发计划"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("导出分支为", "SVG 矢量图…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	svgBytes, err := os.ReadFile(filepath.Join(root, "开发计划.svg"))
	if err != nil || !strings.Contains(string(svgBytes), "<svg ") || !strings.Contains(string(svgBytes), "开发计划") {
		t.Fatalf("invalid exported SVG: %v", err)
	}

	// 8. Canvas context menu: headings only & show depth.
	vp, ok := tt.Find("思维导图画布")
	if !ok {
		t.Fatal("missing mind map canvas")
	}
	tt.RightClickAt(vp.X+8, vp.Y+8)
	if err := tt.ChooseMenuItem("仅看标题大纲"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.doc.mindMap.HeadingsOnly() {
		t.Fatal("headings only toggle did not activate")
	}
	if _, ok := tt.Find("搭建脚手架"); ok {
		t.Fatal("list item should be hidden in headings-only mode")
	}
	tt.RightClickAt(vp.X+8, vp.Y+8)
	if err := tt.ChooseMenuItem("仅看标题大纲"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.doc.mindMap.HeadingsOnly() {
		t.Fatal("headings only toggle did not deactivate")
	}
	tt.RightClickAt(vp.X+8, vp.Y+8)
	if err := tt.ChooseMenuItem("显示层级", "仅显示第 1 层"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.doc.mindMap.HasFolded() {
		t.Fatal("show depth level 1 did not fold branches")
	}
}
