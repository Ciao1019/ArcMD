package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/agent"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestNewWhiteboardDrawAndAutosave(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()

	// Shortcut ⌥⌘B creates a standalone .excalidraw.svg whiteboard.
	tt.Key(ui.Cmd|ui.Alt, ui.KeyB)
	tt.Frame()

	if a.doc == nil || !a.doc.isWhiteboard() || !vault.IsWhiteboardSVG(a.doc.rel) {
		t.Fatalf("expected ⌥⌘B to create & open .excalidraw.svg whiteboard, got %+v", a.doc)
	}
	rel := a.doc.rel

	// Draw a rectangle via BoardView on the canvas and let autosave persist it.
	tt.Key(0, ui.KeyR)
	tt.Frame()
	tt.Press(460, 220)
	tt.Move(660, 340)
	tt.Release(660, 340)
	tt.Frame()

	if len(a.doc.board.Scene.Elements) != 1 {
		t.Fatalf("expected 1 drawn shape, got %d", len(a.doc.board.Scene.Elements))
	}
	saveSettle(tt)

	// Saved file on disk must be valid standalone SVG carrying Excalidraw JSON payload.
	diskRaw, err := os.ReadFile(filepath.Join(root(a), rel))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(diskRaw), "<svg") || !strings.Contains(string(diskRaw), "payload-start") {
		t.Fatalf("expected amphibious .excalidraw.svg on disk, got %.120q", string(diskRaw))
	}

	// Reopening from disk preserves the drawn element.
	a.open("Welcome.md")
	tt.Frame()
	a.open(rel)
	tt.Frame()
	if !a.doc.isWhiteboard() || len(a.doc.board.Scene.Elements) != 1 {
		t.Fatalf("reopened whiteboard did not preserve elements: doc=%+v", a.doc)
	}

	// Backward compatibility: legacy raw .excalidraw JSON file still opens, edits, and saves as JSON.
	legacyScene := whiteboard.NewScene()
	legacyScene.Elements = append(legacyScene.Elements, whiteboard.NewElement(whiteboard.KindEllipse, 40, 40, 90, 60))
	legacyRaw, _ := legacyScene.Encode()
	if err := writeNative(root(a), "旧版白板.excalidraw", legacyRaw); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("旧版白板.excalidraw")
	tt.Frame()
	if !a.doc.isWhiteboard() || len(a.doc.board.Scene.Elements) != 1 {
		t.Fatalf("failed opening legacy .excalidraw file: %+v", a.doc)
	}
	a.doc.board.FitToContent(ui.Rect{})
	tt.Frame()
	tt.Key(0, ui.KeyR)
	tt.Frame()
	tt.Press(460, 220)
	tt.Move(660, 340)
	tt.Release(660, 340)
	tt.Frame()
	saveSettle(tt)
	legacySaved, err := os.ReadFile(filepath.Join(root(a), "旧版白板.excalidraw"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(strings.TrimSpace(string(legacySaved)), "<svg") {
		t.Fatalf("legacy .excalidraw should stay raw JSON on save, got SVG")
	}
	if decoded, derr := whiteboard.Decode(string(legacySaved)); derr != nil || len(decoded.Elements) != 2 {
		t.Fatalf("legacy .excalidraw decode after save failed: len=%v err=%v", decoded, derr)
	}
}

func TestWhiteboardMarkdownAssociations(t *testing.T) {
	a, tt := newTestApp(t)
	if err := writeNative(root(a), "接口规范.md", "# 接口规范\n\n核心协议采用 gRPC 与本地签名验证。\n"); err != nil {
		t.Fatal(err)
	}
	a.newWhiteboard("")
	tt.Frame()
	boardRel := a.doc.rel

	// Drag/drop simulation: insert a linked doc card for 接口规范.md onto the whiteboard.
	a.boardDropDocCard(a.doc, 360, 240, a.state.ActiveSpace().ID, "接口规范.md")
	tt.Frame()
	if len(a.doc.board.Scene.Elements) != 1 {
		t.Fatalf("expected boardDropDocCard to add 1 note card, got %d", len(a.doc.board.Scene.Elements))
	}
	card := a.doc.board.Scene.Elements[0]
	if card.Kind != whiteboard.KindNote || !strings.Contains(card.Link, "[[接口规范]]") {
		t.Fatalf("unexpected doc card: %+v", card)
	}
	elID := card.ID
	saveSettle(tt)

	// Open the linked note in Split View alongside the whiteboard.
	a.openWikiFromBoard(a.doc, "接口规范", true)
	tt.Frame()
	if a.split == nil || len(a.split.panes) != 2 {
		t.Fatalf("expected openWikiFromBoard(..., split=true) to open split view, got %+v", a.split)
	}
	if a.split.panes[0].doc.rel != boardRel || a.split.panes[1].doc.rel != "接口规范.md" {
		t.Fatalf("unexpected split panes: %q, %q", a.split.panes[0].doc.rel, a.split.panes[1].doc.rel)
	}

	// Renaming the Markdown document updates [[links]] inside the resident whiteboard too.
	a.open("接口规范.md")
	a.separateSplit()
	tt.Frame()
	a.startRename("接口规范.md")
	a.newName = "新版接口规范"
	a.commitRename()
	tt.Frame()

	a.open(boardRel)
	tt.Frame()
	updated := a.doc.board.Scene.Elements[0]
	if !strings.Contains(updated.Link, "[[新版接口规范]]") || !strings.Contains(updated.Text, "[[新版接口规范]]") {
		t.Fatalf("wiki rename did not sync into whiteboard element: link=%q text=%q", updated.Link, updated.Text)
	}

	// QAPage & citation jump locate and reveal the exact element inside the whiteboard.
	fs := a.qaSpace(a.state.ActiveSpace().ID)
	page, err := fs.Page(boardRel)
	if err != nil || len(page.Lines) == 0 {
		t.Fatalf("QAPage failed for whiteboard: page=%+v err=%v", page, err)
	}
	a.openCitation(a.state.ActiveSpace().ID, agent.Citation{Path: boardRel, From: 1, To: 1})
	tt.Frame()
	if !a.doc.board.Selected[elID] {
		t.Fatalf("openCitation did not reveal element %q on whiteboard", elID)
	}

	// Versioning: save a milestone, add an element, then restore milestone.
	a.versions = versionsState{open: true, for_: a.doc, createLabel: "初始连线"}
	a.createMilestone()
	b2 := whiteboard.NewElement(whiteboard.KindRectangle, 80, 320, 120, 60)
	b2.Text = "临时节点"
	a.doc.board.Scene.Elements = append(a.doc.board.Scene.Elements, b2)
	a.doc.board.Selected = map[string]bool{b2.ID: true}
	saveSettle(tt)

	a.openVersions()
	tt.Frame()
	milestoneIdx := -1
	for i, ver := range a.versions.versions {
		if ver.Label == "初始连线" {
			milestoneIdx = i
			break
		}
	}
	if milestoneIdx < 0 {
		t.Fatalf("missing saved whiteboard milestone in %+v", a.versions.versions)
	}
	a.selectVersion(milestoneIdx)
	tt.Frame()
	a.restoreVersion()
	tt.Frame()
	if len(a.doc.board.Scene.Elements) != 1 {
		t.Fatalf("expected restoreVersion to restore 1 element, got %d", len(a.doc.board.Scene.Elements))
	}

	// Save inline Markdown whiteboard block as standalone file -> replaces block with ![[...-白板.excalidraw]].
	inlineScene := whiteboard.NewScene()
	inlineScene.Elements = append(inlineScene.Elements, whiteboard.NewElement(whiteboard.KindRectangle, 20, 20, 100, 60))
	raw, _ := inlineScene.Encode()
	if err := writeNative(root(a), "设计草稿.md", "# 设计草稿\n\n```excalidraw\n"+raw+"\n```\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("设计草稿.md")
	tt.Frame()
	blockID := a.doc.ed.Doc.Blocks[1].ID
	a.saveBlockAsWhiteboard(a.doc, blockID, inlineScene)
	tt.Frame()
	if a.doc.ed.Doc.Blocks[1].Kind != "image" || !vault.IsWhiteboardSVG(a.doc.ed.Doc.Blocks[1].URL) {
		t.Fatalf("saveBlockAsWhiteboard did not convert inline block to .excalidraw.svg embed: %+v", a.doc.ed.Doc.Blocks[1])
	}
	loaded, _, lerr := a.loadWhiteboardFor(a.doc, a.doc.ed.Doc.Blocks[1].URL)
	if lerr != nil || loaded == nil || len(loaded.Elements) != 1 {
		t.Fatalf("loadWhiteboardFor embedded whiteboard failed: loaded=%+v err=%v", loaded, lerr)
	}

	// Export SVG file next to the whiteboard.
	a.open(boardRel)
	tt.Frame()
	a.exportWhiteboardSVG(a.doc)
	svgPath := filepath.Join(root(a), strings.TrimSuffix(boardRel, vault.FileExt(boardRel))+".svg")
	if data, err := os.ReadFile(svgPath); err != nil || !strings.Contains(string(data), "<svg") {
		t.Fatalf("exportWhiteboardSVG did not write valid SVG at %s: %v", svgPath, err)
	}
}

func TestTyporaCompatibleWhiteboardInsertAndInlineSave(t *testing.T) {
	a, tt := newTestApp(t)
	if err := writeNative(root(a), "笔记草稿.md", "# 笔记草稿\n\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("笔记草稿.md")
	tt.Frame()

	a.doc.ed.InsertWhiteboard()
	tt.Frame()

	// Document should contain a standard Markdown image referencing assets/*.excalidraw.svg.
	var imgURL string
	for _, b := range a.doc.ed.Doc.Blocks {
		if b.Kind == "image" && vault.IsWhiteboardSVG(b.URL) {
			imgURL = b.URL
			break
		}
	}
	if imgURL == "" || !strings.HasPrefix(imgURL, "assets/") {
		t.Fatalf("expected InsertWhiteboard to create assets/*.excalidraw.svg image block, got blocks=%+v", a.doc.ed.Doc.Blocks)
	}

	// Save document and verify Typora-friendly Markdown on disk + hidden assets/ in sidebar tree.
	if err := a.doc.write(false); err != nil {
		t.Fatal(err)
	}
	mdDisk, err := os.ReadFile(filepath.Join(root(a), "笔记草稿.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mdDisk), "![手绘白板]("+imgURL+")") {
		t.Fatalf("saved Markdown missing standard image syntax: %q", string(mdDisk))
	}
	if strings.Contains(string(mdDisk), "```excalidraw") || strings.Contains(string(mdDisk), "<svg") {
		t.Fatalf("saved Markdown should not inline raw JSON/SVG: %q", string(mdDisk))
	}
	for _, node := range a.tree {
		if node.Name == "assets" || strings.HasPrefix(node.Path, "assets") {
			t.Fatalf("sidebar tree should hide assets/ directory, found %+v", node)
		}
	}

	// Draw inside the open inline whiteboard canvas and verify it saves directly to disk SVG.
	tt.Key(0, ui.KeyR)
	tt.Frame()
	tt.Press(600, 320)
	tt.Move(740, 420)
	tt.Release(740, 420)
	tt.Frame()

	assetDisk, err := os.ReadFile(filepath.Join(root(a), filepath.FromSlash(imgURL)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(assetDisk), "<svg") {
		t.Fatalf("expected inline save to write valid SVG, got %.120q", string(assetDisk))
	}
	decoded, err := whiteboard.Decode(string(assetDisk))
	if err != nil || len(decoded.Elements) == 0 {
		t.Fatalf("expected inline drawn shape persisted inside amphibious SVG, len=%v err=%v", decoded, err)
	}
}

func TestWhiteboardEnglishUI(t *testing.T) {
	defer i18n.SetLanguage("zh-Hans")
	a, tt := newTestApp(t)
	a.prefs.Language = "en"
	a.applyLanguage()
	tt.SetLocale("en")
	a.newWhiteboard("")
	tt.Frame()

	if !tt.HasText("Empty whiteboard") && !tt.HasText("Pick a shape above or drag to draw. Double-click anywhere for text or [[wiki links]].") {
		t.Fatalf("expected English whiteboard empty UI text, got %q", tt.Texts())
	}

	// Open the component library drawer with 'B' and confirm new English strings render.
	tt.Key(0, ui.KeyB)
	tt.Frame()
	if !tt.HasText("Primary-Replica DB") {
		t.Fatalf("expected English library item 'Primary-Replica DB', got %q", tt.Texts())
	}
}

func TestWhiteboardUserLibraryPersistenceAndImport(t *testing.T) {
	a, tt := newTestApp(t)
	extItem := whiteboard.LibraryItem{
		ID:       "imp-1",
		Name:     "导入服务组件",
		Category: "我的",
		Elements: []*whiteboard.Element{
			whiteboard.NewElement(whiteboard.KindHexagon, 0, 0, 120, 72),
		},
	}
	encodedLib, err := whiteboard.EncodeLibrary([]whiteboard.LibraryItem{extItem})
	if err != nil {
		t.Fatalf("EncodeLibrary: %v", err)
	}
	importPath := filepath.Join(t.TempDir(), "custom-pack.excalidrawlib")
	if err := os.WriteFile(importPath, []byte(encodedLib), 0o644); err != nil {
		t.Fatal(err)
	}

	a.pickFiles = func(done func(paths []string)) {
		done([]string{importPath})
	}
	a.newWhiteboard("")
	tt.Frame()

	// Open library drawer and trigger Import…
	tt.Key(0, ui.KeyB)
	tt.Frame()
	impBounds, ok := tt.Find("导入…")
	if !ok {
		t.Fatal("expected '导入…' button in library drawer")
	}
	tt.ClickAt(impBounds.X+impBounds.W/2, impBounds.Y+impBounds.H/2)
	tt.Frame()

	if got := a.userWhiteboardLibrary(); len(got) != 1 || got[0].Name != "导入服务组件" {
		t.Fatalf("expected imported library item saved in memory, got %+v", got)
	}

	// Reset in-memory cache and reload from ConfigDir/whiteboard-library.excalidrawlib.
	a.wbLib = whiteboardLibState{}
	reloaded := a.userWhiteboardLibrary()
	if len(reloaded) != 1 || len(reloaded[0].Elements) != 1 || reloaded[0].Elements[0].Kind != whiteboard.KindHexagon {
		t.Fatalf("expected library item persisted on disk and reloaded cleanly, got %+v", reloaded)
	}
}
