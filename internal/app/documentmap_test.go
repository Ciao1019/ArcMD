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
	for _, name := range []string{"document-map-one.png", "document-map-both.png"} {
		if name == "document-map-both.png" {
			if err := tt.Click("双侧布局"); err != nil {
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
