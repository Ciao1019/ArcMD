package editor

import (
	"bytes"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestBoardViewDrawUndoRedoAndReveal(t *testing.T) {
	scene := whiteboard.NewScene()
	board := NewBoard(scene)
	var changed int
	tt := ui.NewTester(func(c *ui.Context) {
		BoardView(c, board, BoardOptions{
			Theme:    c.Theme(),
			OnChange: func() { changed++ },
		})
	}, 960, 640)
	board.RequestFocus = true
	tt.Frame()

	// Switch to Rectangle tool using shortcut 'R' and draw a rectangle.
	tt.Key(0, ui.KeyR)
	tt.Frame()
	if board.Tool != ToolRect {
		t.Fatalf("expected ToolRect after pressing R, got %v", board.Tool)
	}

	tt.Press(260, 180)
	tt.Move(460, 300)
	tt.Release(460, 300)
	tt.Frame()

	if len(board.Scene.Elements) != 1 || board.Scene.Elements[0].Kind != whiteboard.KindRectangle {
		t.Fatalf("expected 1 drawn rectangle, got %+v", board.Scene.Elements)
	}
	if changed == 0 {
		t.Fatalf("expected OnChange callback after drawing")
	}

	// Tool returns to Select automatically unless locked; switch to Arrow ('A') and draw another shape.
	if board.Tool != ToolSelect {
		t.Fatalf("expected ToolSelect after shape creation, got %v", board.Tool)
	}
	tt.Key(0, ui.KeyA)
	tt.Frame()
	if board.Tool != ToolArrow {
		t.Fatalf("expected ToolArrow after pressing A, got %v", board.Tool)
	}
	tt.Press(460, 240)
	tt.Move(640, 240)
	tt.Release(640, 240)
	tt.Frame()

	if len(board.Scene.Elements) != 2 || board.Scene.Elements[1].Kind != whiteboard.KindArrow {
		t.Fatalf("expected arrow as 2nd element, got %+v", board.Scene.Elements)
	}

	// Undo removes the arrow, Redo restores it.
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	if len(board.Scene.Elements) != 1 {
		t.Fatalf("expected Undo to leave 1 element, got %d", len(board.Scene.Elements))
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	tt.Frame()
	if len(board.Scene.Elements) != 2 {
		t.Fatalf("expected Redo to restore 2 elements, got %d", len(board.Scene.Elements))
	}

	// RevealElement selects the target element and centers the viewport on it.
	rectID := board.Scene.Elements[0].ID
	board.RevealElement(rectID)
	tt.Frame()
	if !board.Selected[rectID] {
		t.Fatalf("expected RevealElement(%q) to select element", rectID)
	}
}

func TestMarkdownInlineWhiteboardRoundTripEditAndExport(t *testing.T) {
	s := whiteboard.NewScene()
	box := whiteboard.NewElement(whiteboard.KindRectangle, 40, 40, 180, 90)
	box.Text = "服务网关\n[[API 设计]]"
	box.StrokeColor = "blue"
	box.FillColor = "blue"
	box.FillStyle = whiteboard.FillHachure
	s.Elements = append(s.Elements, box)

	rawJSON, err := s.Encode()
	if err != nil {
		t.Fatalf("Encode scene: %v", err)
	}
	md := "# 技术方案\n\n```excalidraw\n" + rawJSON + "\n```\n\n结束说明。"

	e, tt := newTester(md)
	if len(e.Doc.Blocks) != 3 || !isWhiteboardBlock(e.Doc.Blocks[1]) {
		t.Fatalf("expected excalidraw code block at index 1, got %+v", e.Doc.Blocks)
	}
	if exported := ExportMarkdown(e.Doc); !strings.Contains(exported, "```excalidraw") || !strings.Contains(exported, "服务网关") {
		t.Fatalf("Markdown round-trip dropped excalidraw block: %q", exported)
	}

	// Hover the resting whiteboard preview and click its "编辑画布" pill to expand inline BoardView.
	r := e.rows[1]
	bounds, _ := tt.Find("文档")
	tt.Move(bounds.X+r.rect.X+r.rect.W/2, bounds.Y+r.rect.Y+r.rect.H/2)
	tt.Frame()
	editBtn := boardEditButtonRect(decoration{rect: r.boardRect})
	tt.ClickAt(bounds.X+editBtn.X+editBtn.W/2, bounds.Y+editBtn.Y+editBtn.H/2)
	tt.Advance(20 * time.Millisecond)
	tt.Frame()
	if e.boardEditing != e.Doc.Blocks[1].ID {
		t.Fatalf("expected click on edit pill to open inline BoardView, boardEditing=%q", e.boardEditing)
	}

	// HTML export renders inline SVG instead of raw JSON code.
	htmlOut := ExportHTML(e.Doc)
	if !strings.Contains(htmlOut, "<svg") || !strings.Contains(htmlOut, "服务网关") {
		t.Fatalf("ExportHTML did not embed rendered whiteboard SVG: %s", htmlOut)
	}

	// Vector PDF export draws directly without error.
	var buf bytes.Buffer
	w := pdf.NewWriter(&buf, "wb")
	w.ReservePages(1)
	vp := w.NewVectorPage(pdf.A4Width, pdf.A4Height)
	ExportWhiteboardPDF(vp, s, Page{}, nil, 36, 36, pdf.A4Width-72, pdf.A4Height-72, 1)
	if err := w.FinishVectorPage(vp); err != nil {
		t.Fatalf("FinishVectorPage: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("pdf Close: %v", err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) || buf.Len() < 400 {
		t.Fatalf("unexpected vector PDF output length %d", buf.Len())
	}
}

func TestAmphibiousSVGMarkdownEmbedInlineEdit(t *testing.T) {
	stored := map[string]*whiteboard.Scene{}
	revs := map[string]uint64{}
	var e *Editor
	opts := Options{
		Theme: nil,
		CreateWhiteboard: func(scene *whiteboard.Scene) (string, error) {
			url := "assets/test-board.excalidraw.svg"
			stored[url] = scene.Clone()
			revs[url] = 1
			return url, nil
		},
		LoadWhiteboard: func(url string) (*whiteboard.Scene, uint64, error) {
			return stored[url], revs[url], nil
		},
		SaveWhiteboard: func(url string, scene *whiteboard.Scene) uint64 {
			stored[url] = scene.Clone()
			revs[url]++
			return revs[url]
		},
	}
	e = New("# 文档标题\n\n")
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		opts.Width = w
		opts.Theme = c.Theme()
		View(c, e, opts)
	}, 900, 650)
	focus(e, tt)
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()

	e.InsertWhiteboard()
	tt.Frame()

	mdOut := ExportMarkdown(e.Doc)
	if !strings.Contains(mdOut, "![手绘白板](assets/test-board.excalidraw.svg)") || strings.Contains(mdOut, "```excalidraw") {
		t.Fatalf("expected standard Markdown image embed pointing to .excalidraw.svg, got:\n%s", mdOut)
	}
	if e.boardEditing == "" {
		t.Fatalf("expected newly inserted .excalidraw.svg embed to open inline BoardView")
	}

	// Simulate an edit inside the inline board and verify SaveWhiteboard received the update.
	board := e.boardForBlock(e.Doc.Blocks[1])
	extra := whiteboard.NewElement(whiteboard.KindRectangle, 20, 260, 110, 50)
	extra.Text = "Typora 可见"
	board.Scene.Elements = append(board.Scene.Elements, extra)
	e.commitBoardToBlock(e.Doc.Blocks[1].ID, board)
	tt.Frame()

	if len(stored["assets/test-board.excalidraw.svg"].Elements) != len(board.Scene.Elements) {
		t.Fatalf("SaveWhiteboard did not persist edited scene")
	}
}

func TestMathBlockPreservedAfterInsertWhiteboardAndSlashFocus(t *testing.T) {
	stored := map[string]*whiteboard.Scene{}
	opts := Options{
		CreateWhiteboard: func(scene *whiteboard.Scene) (string, error) {
			url := "assets/Ideas-Export-白板.excalidraw.svg"
			stored[url] = scene.Clone()
			return url, nil
		},
		LoadWhiteboard: func(url string) (*whiteboard.Scene, uint64, error) {
			return stored[url], 1, nil
		},
		SaveWhiteboard: func(url string, scene *whiteboard.Scene) uint64 {
			stored[url] = scene.Clone()
			return 2
		},
	}

	source := strings.Join([]string{
		"$$",
		`\int_0^\infty x^2 e^{-x} dx = a`,
		"$$",
		"",
		`<div class="arcmd-columns" style="display:flex;gap:24px">`,
		"",
		`<div class="arcmd-column" style="flex:1;min-width:0">`,
		"",
		"</div>",
		"",
		`<div class="arcmd-column" style="flex:1;min-width:0">`,
		"",
		"啊打算",
		"",
		"</div>",
		"",
		"</div>",
		"",
		"> [!WARNING]",
		"",
		"```go",
		"```",
		"",
	}, "\n")

	e := New(source)
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		opts.Width = w
		opts.Theme = c.Theme()
		View(c, e, opts)
	}, 960, 720)
	focus(e, tt)
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()

	// Insert via slash item path (which sets e.RequestFocus = true alongside InsertWhiteboard).
	e.insertItem(insertion{kind: "whiteboard"})
	tt.Frame()
	tt.Frame()

	if e.boardEditing == "" {
		t.Fatalf("expected inline whiteboard editing to be active after slash insertion")
	}
	var imgBlockID string
	for _, b := range e.Doc.Blocks {
		if b.Kind == "image" && strings.HasSuffix(b.URL, ".excalidraw.svg") {
			imgBlockID = b.ID
			break
		}
	}
	if imgBlockID == "" {
		t.Fatalf("expected inserted .excalidraw.svg image block, got %+v", e.Doc.Blocks)
	}

	// Pressing 'r' must select the Rectangle tool on BoardView instead of typing into the alt text.
	tt.Key(0, ui.KeyR)
	tt.Frame()
	board := e.boards[imgBlockID]
	if board == nil || board.Tool != ToolRect {
		t.Fatalf("expected keyboard focus on inline BoardView (ToolRect), got board=%v", board)
	}
	if got := e.blockByID(imgBlockID).Text(); got != "手绘白板" {
		t.Fatalf("expected image alt text untouched, got %q", got)
	}

	md := ExportMarkdown(e.Doc)
	if !strings.Contains(md, "$$\n\\int_0^\\infty x^2 e^{-x} dx = a\n$$") {
		t.Fatalf("ExportMarkdown dropped closing $$ after whiteboard insertion:\n%s", md)
	}
	round := ImportMarkdown(md)
	if len(round.Blocks) < 5 {
		t.Fatalf("expected at least 5 top-level blocks after round-trip, got %d: %+v\nmarkdown:\n%s", len(round.Blocks), round.Blocks, md)
	}
	if round.Blocks[0].Kind != "math" || round.Blocks[0].Text() != `\int_0^\infty x^2 e^{-x} dx = a` {
		t.Fatalf("unexpected first block after round-trip: %+v", round.Blocks[0])
	}
	if round.Blocks[1].Kind != "columns" || round.Blocks[2].Kind != "callout" || round.Blocks[3].Kind != "code" || round.Blocks[4].Kind != "image" {
		t.Fatalf("math block swallowed subsequent blocks on round-trip: kinds=[%s, %s, %s, %s]",
			round.Blocks[1].Kind, round.Blocks[2].Kind, round.Blocks[3].Kind, round.Blocks[4].Kind)
	}

	// Additional display-math edge variants must also survive untouched after appending a block.
	variants := []struct {
		name string
		src  string
		want string
	}{
		{name: "single-line-dollar", src: "$$ x^2 + 1 $$\n\n后继段落\n", want: "x^2 + 1"},
		{name: "bracket-multiline", src: "\\[\nx + y\n\\]\n\n后继段落\n", want: "x + y"},
		{name: "quoted-multiline", src: "> $$\n> x^2\n> $$\n\n后继段落\n", want: "x^2"},
	}
	for _, tc := range variants {
		t.Run(tc.name, func(t *testing.T) {
			ev := New(tc.src)
			ev.Engine.SetSelection(ev.Len(), ev.Len())
			ev.Engine.Insert(ev.Doc.Blocks[0].Clone()) // trigger partial export via structural change
			out := ExportMarkdown(ev.Doc)
			re := ImportMarkdown(out)
			if len(re.Blocks) < 3 {
				t.Fatalf("%s swallowed following blocks:\nout:\n%s\ngot %d blocks", tc.name, out, len(re.Blocks))
			}
			if re.Blocks[0].Kind != "math" || re.Blocks[0].Text() != tc.want {
				t.Fatalf("%s unexpected block[0]: kind=%q text=%q", tc.name, re.Blocks[0].Kind, re.Blocks[0].Text())
			}
			if re.Blocks[1].Kind != "paragraph" || re.Blocks[1].Text() != "后继段落" {
				t.Fatalf("%s swallowed paragraph block: %+v", tc.name, re.Blocks[1])
			}
		})
	}
}

func TestBoardModalAndLargeCanvasDrawingCoordinates(t *testing.T) {
	assertRectMatchesWindow := func(t *testing.T, label string, b *Board, el *whiteboard.Element, wantX0, wantY0, wantX1, wantY1 float32) {
		t.Helper()
		x0, y0 := b.sceneToScreen(b.lastRect, el.X, el.Y)
		x1, y1 := b.sceneToScreen(b.lastRect, el.X+el.W, el.Y+el.H)
		if math.Abs(float64(x0-wantX0)) >= 1 || math.Abs(float64(y0-wantY0)) >= 1 ||
			math.Abs(float64(x1-wantX1)) >= 1 || math.Abs(float64(y1-wantY1)) >= 1 {
			t.Fatalf("%s: screen rect mismatch got (%.2f,%.2f)-(%.2f,%.2f), want (%.2f,%.2f)-(%.2f,%.2f) [zoom=%.3f pan=(%.2f,%.2f) rect=%+v]",
				label, x0, y0, x1, y1, wantX0, wantY0, wantX1, wantY1, b.Zoom, b.PanX, b.PanY, b.lastRect)
		}
	}

	t.Run("modal-expand-from-inline", func(t *testing.T) {
		stored := map[string]*whiteboard.Scene{}
		opts := Options{
			CreateWhiteboard: func(scene *whiteboard.Scene) (string, error) {
				url := "assets/modal-coords.excalidraw.svg"
				stored[url] = scene.Clone()
				return url, nil
			},
			LoadWhiteboard: func(url string) (*whiteboard.Scene, uint64, error) {
				return stored[url], 1, nil
			},
			SaveWhiteboard: func(url string, scene *whiteboard.Scene) uint64 {
				stored[url] = scene.Clone()
				return 2
			},
		}

		e := New("# 标题\n\n")
		tt := ui.NewTester(func(c *ui.Context) {
			w, _ := c.Size()
			opts.Width = w
			opts.Theme = c.Theme()
			View(c, e, opts)
		}, 1280, 860)
		focus(e, tt)
		e.SetSelection(e.Len(), e.Len())
		tt.Frame()

		// Insert inline whiteboard first (small screen), then open enlarged modal ("放大画布").
		e.InsertWhiteboard()
		tt.Frame()
		var imgID string
		for _, b := range e.Doc.Blocks {
			if b.Kind == "image" && strings.HasSuffix(b.URL, ".excalidraw.svg") {
				imgID = b.ID
				break
			}
		}
		if imgID == "" {
			t.Fatalf("missing whiteboard embed block")
		}
		initialCount := len(e.boards[imgID].Scene.Elements)

		e.openBoardModal(imgID)
		tt.Frame()
		tt.Frame()

		board := e.boards[imgID]
		if board == nil {
			t.Fatalf("missing board instance")
		}
		if board.lastRect.W < 900 || board.lastRect.H < 600 {
			t.Fatalf("expected enlarged modal canvas bounds, got %+v", board.lastRect)
		}

		// Select Rectangle tool and drag across multiple frames.
		tt.Key(0, ui.KeyR)
		tt.Frame()
		tt.Press(420, 260)
		tt.Frame()
		tt.Move(520, 330)
		tt.Frame()
		tt.Move(680, 430)
		tt.Frame()
		tt.Release(680, 430)
		tt.Frame()

		if len(board.Scene.Elements) != initialCount+1 {
			t.Fatalf("expected 1 new rectangle in modal, got elements=%d (was %d)", len(board.Scene.Elements), initialCount)
		}
		created := board.Scene.Elements[len(board.Scene.Elements)-1]
		assertRectMatchesWindow(t, "modal-expand", board, created, 420, 260, 680, 430)
	})

	t.Run("empty-large-board-and-zoomed-stroke", func(t *testing.T) {
		scene := whiteboard.NewScene()
		board := NewBoard(scene)
		tt := ui.NewTester(func(c *ui.Context) {
			BoardView(c, board, BoardOptions{Theme: c.Theme()})
		}, 1280, 860)
		board.RequestFocus = true
		tt.Frame()
		tt.Frame()

		// First stroke on an initially empty large canvas must stay aligned to cursor.
		tt.Key(0, ui.KeyR)
		tt.Frame()
		tt.Press(300, 200)
		tt.Frame()
		tt.Move(430, 290)
		tt.Frame()
		tt.Move(560, 380)
		tt.Frame()
		tt.Release(560, 380)
		tt.Frame()

		if len(board.Scene.Elements) != 1 {
			t.Fatalf("expected 1 rectangle on empty large board, got %d", len(board.Scene.Elements))
		}
		assertRectMatchesWindow(t, "first-stroke-empty-large", board, board.Scene.Elements[0], 300, 200, 560, 380)

		// Zoom in significantly and draw another rectangle: coordinates must still match pointer 1:1.
		board.zoomAtCenter(1.8)
		tt.Frame()
		tt.Key(0, ui.KeyR)
		tt.Frame()
		tt.Press(450, 250)
		tt.Frame()
		tt.Move(580, 345)
		tt.Frame()
		tt.Move(710, 440)
		tt.Frame()
		tt.Release(710, 440)
		tt.Frame()

		if len(board.Scene.Elements) != 2 {
			t.Fatalf("expected 2 rectangles after zoomed stroke, got %d", len(board.Scene.Elements))
		}
		assertRectMatchesWindow(t, "zoomed-large-stroke", board, board.Scene.Elements[1], 450, 250, 710, 440)
	})
}

func TestConvertMermaidToWhiteboard(t *testing.T) {
	src := "flowchart LR\n  A([Start]) --> B{Auth?}\n  B -->|Yes| C[(User DB)]\n  B -->|No| D[Reject]"
	elems, err := ConvertMermaidToWhiteboard(src, 400, 260)
	if err != nil {
		t.Fatalf("ConvertMermaidToWhiteboard: %v", err)
	}
	if len(elems) < 7 {
		t.Fatalf("expected 4 node shapes + 3 arrows, got %d elements", len(elems))
	}
	kinds := map[whiteboard.ElementKind]int{}
	for _, el := range elems {
		kinds[el.Kind]++
	}
	for _, want := range []whiteboard.ElementKind{
		whiteboard.KindStadium,
		whiteboard.KindDiamond,
		whiteboard.KindCylinder,
		whiteboard.KindRectangle,
		whiteboard.KindArrow,
	} {
		if kinds[want] == 0 {
			t.Fatalf("expected %q elements in converted scene, got kinds=%+v", want, kinds)
		}
	}
}

func TestLaserPointerAndLibraryDrawer(t *testing.T) {
	scene := whiteboard.NewScene()
	board := NewBoard(scene)
	tt := ui.NewTester(func(c *ui.Context) {
		BoardView(c, board, BoardOptions{Theme: c.Theme()})
	}, 1100, 760)
	board.RequestFocus = true
	tt.Frame()
	tt.Frame()

	// Laser pointer ('K') leaves a transient glowing trail and records neither elements nor undo history.
	tt.Key(0, ui.KeyK)
	tt.Frame()
	if board.Tool != ToolLaser {
		t.Fatalf("expected ToolLaser after pressing K, got %v", board.Tool)
	}
	tt.Press(240, 220)
	tt.Frame()
	tt.Move(360, 280)
	tt.Frame()
	tt.Release(360, 280)
	tt.Frame()
	if len(board.laserTrail) == 0 {
		t.Fatal("expected non-empty laserTrail during laser stroke")
	}
	if len(board.Scene.Elements) != 0 || board.History.CanUndo() {
		t.Fatalf("laser pointer must not mutate scene or history: elems=%d canUndo=%v", len(board.Scene.Elements), board.History.CanUndo())
	}

	// After > 1.5s, the laser trail fades cleanly.
	tt.Advance(1500 * time.Millisecond)
	tt.Frame()
	if len(board.laserTrail) != 0 {
		t.Fatalf("expected laserTrail to fade out after 1.5s, still has %d samples", len(board.laserTrail))
	}

	// Open Component Library drawer ('B') and stamp "数据库主从集群".
	tt.Key(0, ui.KeyB)
	tt.Frame()
	if !board.libraryOpen {
		t.Fatal("expected B shortcut to toggle libraryOpen")
	}
	bounds, ok := tt.Find("数据库主从集群")
	if !ok {
		t.Fatal("expected built-in library card '数据库主从集群' to be visible")
	}
	tt.ClickAt(bounds.X+bounds.W/2, bounds.Y+bounds.H/2)
	tt.Frame()
	if len(board.Scene.Elements) != 3 {
		t.Fatalf("expected 3 stamped elements from '数据库主从集群', got %d", len(board.Scene.Elements))
	}
	if len(board.Selected) != 3 {
		t.Fatalf("expected all 3 stamped elements selected, got %+v", board.Selected)
	}
	var cylinders int
	for _, el := range board.Scene.Elements {
		if el.Kind == whiteboard.KindCylinder {
			cylinders++
		}
	}
	if cylinders != 2 {
		t.Fatalf("expected 2 cylinder elements stamped, got %d", cylinders)
	}
}

func TestQuickConnectPortsAndNoSelfBinding(t *testing.T) {
	scene := whiteboard.NewScene()
	boxA := whiteboard.NewElement(whiteboard.KindRectangle, 120, 180, 160, 80)
	boxA.ID = "box-a"
	boxB := whiteboard.NewElement(whiteboard.KindDiamond, 460, 170, 140, 100)
	boxB.ID = "box-b"
	scene.Elements = []*whiteboard.Element{boxA, boxB}

	board := NewBoard(scene)
	board.Selected = map[string]bool{boxA.ID: true}
	tt := ui.NewTester(func(c *ui.Context) {
		BoardView(c, board, BoardOptions{Theme: c.Theme()})
	}, 1100, 760)
	board.RequestFocus = true
	tt.Frame()
	tt.Frame()

	// Stay in ToolSelect: drag directly from boxA's Right quick-connect port to boxB's center.
	if board.Tool != ToolSelect {
		t.Fatalf("expected initial ToolSelect, got %v", board.Tool)
	}
	rightPort := whiteboard.ConnectPortPoints(boxA, board.Zoom)[1]
	sx0, sy0 := board.sceneToScreen(board.lastRect, rightPort[0], rightPort[1])
	bx, by := whiteboard.ElementBounds(boxB).Center()
	sx1, sy1 := board.sceneToScreen(board.lastRect, bx, by)

	tt.Press(sx0, sy0)
	tt.Frame()
	tt.Move(sx1, sy1)
	tt.Frame()
	tt.Release(sx1, sy1)
	tt.Frame()

	if len(board.Scene.Elements) != 3 {
		t.Fatalf("expected 3 elements after quick-connect drag, got %d", len(board.Scene.Elements))
	}
	arr := board.Scene.Elements[2]
	if arr.Kind != whiteboard.KindArrow {
		t.Fatalf("expected quick-connect to create KindArrow, got %q", arr.Kind)
	}
	if arr.StartBinding == nil || arr.StartBinding.ElementID != boxA.ID {
		t.Fatalf("expected StartBinding to %q, got %+v", boxA.ID, arr.StartBinding)
	}
	if arr.EndBinding == nil || arr.EndBinding.ElementID != boxB.ID {
		t.Fatalf("expected EndBinding to %q, got %+v", boxB.ID, arr.EndBinding)
	}

	// Arrow tool dragged out inside boxA without reaching another target must NOT self-bind both ends to boxA.
	tt.Key(0, ui.KeyA)
	tt.Frame()
	ax, ay := whiteboard.ElementBounds(boxA).Center()
	px0, py0 := board.sceneToScreen(board.lastRect, ax, ay)
	px1, py1 := board.sceneToScreen(board.lastRect, boxA.X+boxA.W+4, ay+2)

	tt.Press(px0, py0)
	tt.Frame()
	tt.Move(px1, py1)
	tt.Frame()
	tt.Release(px1, py1)
	tt.Frame()

	if len(board.Scene.Elements) != 4 {
		t.Fatalf("expected 4th element from ToolArrow drag, got %d", len(board.Scene.Elements))
	}
	selfArr := board.Scene.Elements[3]
	if selfArr.StartBinding == nil || selfArr.StartBinding.ElementID != boxA.ID {
		t.Fatalf("expected StartBinding to %q, got %+v", boxA.ID, selfArr.StartBinding)
	}
	if selfArr.EndBinding != nil && selfArr.EndBinding.ElementID == boxA.ID {
		t.Fatalf("arrow self-bound both ends to %q: %+v", boxA.ID, selfArr.EndBinding)
	}
}

func TestBoardCopyCutPasteAcrossBoardsAndPlainText(t *testing.T) {
	s1 := whiteboard.NewScene()
	boxA := whiteboard.NewElement(whiteboard.KindCylinder, 80, 140, 120, 90)
	boxA.ID = "b1-a"
	boxA.Text = "DB"
	boxB := whiteboard.NewElement(whiteboard.KindHexagon, 300, 145, 130, 80)
	boxB.ID = "b1-b"
	boxB.Text = "API"
	arr := whiteboard.NewElement(whiteboard.KindArrow, 200, 185, 100, 0)
	arr.ID = "b1-arr"
	arr.StartBinding = &whiteboard.Binding{ElementID: boxA.ID, Gap: 6}
	arr.EndBinding = &whiteboard.Binding{ElementID: boxB.ID, Gap: 6}
	s1.Elements = []*whiteboard.Element{boxA, boxB, arr}
	whiteboard.UpdateBindings(s1)

	b1 := NewBoard(s1)
	tt1 := ui.NewTester(func(c *ui.Context) {
		BoardView(c, b1, BoardOptions{Theme: c.Theme()})
	}, 960, 640)
	b1.RequestFocus = true
	tt1.Frame()
	tt1.Frame()

	// Select all (⌘A), copy (⌘C), then verify Excalidraw clipboard payload.
	tt1.Key(ui.Cmd, ui.KeyA)
	tt1.Frame()
	tt1.Key(ui.Cmd, ui.KeyC)
	tt1.Frame()
	clip := tt1.Clipboard()
	if !strings.Contains(clip, `"excalidraw/clipboard"`) {
		t.Fatalf("expected excalidraw/clipboard JSON after ⌘C, got %q", clip)
	}

	// Cut (⌘X) removes elements and Undo (⌘Z) restores them.
	tt1.Key(ui.Cmd, ui.KeyX)
	tt1.Frame()
	if len(b1.Scene.Elements) != 0 {
		t.Fatalf("expected empty scene after ⌘X, got %d elements", len(b1.Scene.Elements))
	}
	tt1.Key(ui.Cmd, ui.KeyZ)
	tt1.Frame()
	if len(b1.Scene.Elements) != 3 {
		t.Fatalf("expected Undo after ⌘X to restore 3 elements, got %d", len(b1.Scene.Elements))
	}

	// Paste into a separate whiteboard canvas (⌘V): fresh IDs + intact bindings.
	b2 := NewBoard(whiteboard.NewScene())
	tt2 := ui.NewTester(func(c *ui.Context) {
		BoardView(c, b2, BoardOptions{Theme: c.Theme()})
	}, 960, 640)
	b2.RequestFocus = true
	tt2.Frame()
	tt2.Frame()

	tt2.SetClipboard(clip)
	tt2.Key(ui.Cmd, ui.KeyV)
	tt2.Frame()

	if len(b2.Scene.Elements) != 3 {
		t.Fatalf("expected 3 pasted elements on board2, got %d", len(b2.Scene.Elements))
	}
	pA, pB, pArr := b2.Scene.Elements[0], b2.Scene.Elements[1], b2.Scene.Elements[2]
	if pA.ID == boxA.ID || pB.ID == boxB.ID || pArr.ID == arr.ID {
		t.Fatal("expected pasted elements on board2 to receive fresh IDs")
	}
	if pArr.StartBinding == nil || pArr.StartBinding.ElementID != pA.ID || pArr.EndBinding == nil || pArr.EndBinding.ElementID != pB.ID {
		t.Fatalf("expected cross-board pasted bindings remapped to new shapes: start=%+v end=%+v", pArr.StartBinding, pArr.EndBinding)
	}

	// Plain text clipboard pastes as a new KindText element.
	tt2.SetClipboard("新服务节点")
	tt2.Key(ui.Cmd, ui.KeyV)
	tt2.Frame()
	if len(b2.Scene.Elements) != 4 {
		t.Fatalf("expected 4th pasted plain-text element on board2, got %d", len(b2.Scene.Elements))
	}
	txtEl := b2.Scene.Elements[3]
	if txtEl.Kind != whiteboard.KindText || txtEl.Text != "新服务节点" {
		t.Fatalf("expected KindText '新服务节点', got kind=%q text=%q", txtEl.Kind, txtEl.Text)
	}
}

func TestBoardResizeLibraryComponentAndUndo(t *testing.T) {
	builtins := whiteboard.BuiltinLibrary()
	var browser whiteboard.LibraryItem
	for _, it := range builtins {
		if it.ID == "builtin-browser" {
			browser = it
			break
		}
	}
	if len(browser.Elements) == 0 {
		t.Fatal("missing builtin-browser library item")
	}

	scene := whiteboard.NewScene()
	compElems := whiteboard.InstantiateLibraryItem(browser, 420, 300)
	scene.Elements = append(scene.Elements, compElems...)

	// Add an external card bound to Card A inside the browser component to verify live rebinding.
	var cardA *whiteboard.Element
	for _, el := range compElems {
		if el.Text == "Card A" {
			cardA = el
			break
		}
	}
	if cardA == nil {
		t.Fatal("missing Card A inside desktop browser component")
	}
	extNode := whiteboard.NewElement(whiteboard.KindRectangle, 20, 320, 140, 72)
	extNode.ID = "ext-caller"
	extNode.Text = "核心想法"
	extNode.StrokeColor = "blue"
	extNode.FillColor = "blue"
	extArr := whiteboard.NewElement(whiteboard.KindArrow, 160, 356, 120, 0)
	extArr.ID = "ext-arrow"
	extArr.StartBinding = &whiteboard.Binding{ElementID: extNode.ID, Gap: 6}
	extArr.EndBinding = &whiteboard.Binding{ElementID: cardA.ID, Gap: 6}
	scene.Elements = append(scene.Elements, extNode, extArr)
	whiteboard.UpdateBindings(scene)

	board := NewBoard(scene)
	var changed int
	tt := ui.NewTester(func(c *ui.Context) {
		BoardView(c, board, BoardOptions{
			Theme:    c.Theme(),
			OnChange: func() { changed++ },
		})
	}, 1100, 760)
	board.RequestFocus = true
	tt.Frame()
	tt.Frame()

	// Click inside Card B to select the entire grouped browser component (6 elements, NOT extNode/extArr).
	var cardB *whiteboard.Element
	for _, el := range compElems {
		if el.Text == "Card B" {
			cardB = el
			break
		}
	}
	cbx, cby := whiteboard.ElementBounds(cardB).Center()
	clickX, clickY := board.sceneToScreen(board.lastRect, cbx, cby)
	tt.ClickAt(clickX, clickY)
	tt.Frame()

	if len(board.Selected) != len(compElems) {
		t.Fatalf("expected all %d component elements selected on click, got %d (%+v)", len(compElems), len(board.Selected), board.Selected)
	}

	origBox := whiteboard.SelectionBounds(compElems)
	origArrEndX := extArr.X + extArr.Points[len(extArr.Points)-1][0]

	// Drag the SE outer group handle outward by (+120, +80) screen pixels to enlarge the component.
	se := whiteboard.BoxHandlePoints(origBox)[whiteboard.HandleSE]
	hx, hy := board.sceneToScreen(board.lastRect, se[0], se[1])
	tt.Press(hx, hy)
	tt.Frame()
	tt.Move(hx+120, hy+80)
	tt.Frame()
	tt.Release(hx+120, hy+80)
	tt.Frame()

	resizedBox := whiteboard.SelectionBounds(compElems)
	wantDW := 120 / board.Zoom
	wantDH := 80 / board.Zoom
	if math.Abs(float64(resizedBox.W-(origBox.W+wantDW))) > 2 || math.Abs(float64(resizedBox.H-(origBox.H+wantDH))) > 2 {
		t.Fatalf("component resize mismatch: got %.1fx%.1f, want %.1fx%.1f",
			resizedBox.W, resizedBox.H, origBox.W+wantDW, origBox.H+wantDH)
	}
	if math.Abs(float64(resizedBox.X-origBox.X)) > 0.5 || math.Abs(float64(resizedBox.Y-origBox.Y)) > 0.5 {
		t.Fatalf("HandleSE moved anchored top-left: got (%v,%v), want (%v,%v)", resizedBox.X, resizedBox.Y, origBox.X, origBox.Y)
	}
	if changed == 0 {
		t.Fatal("expected OnChange after resizing component")
	}

	// External bound arrow must follow resized Card A's left perimeter.
	newArrEndX := extArr.X + extArr.Points[len(extArr.Points)-1][0]
	if math.Abs(float64(newArrEndX-origArrEndX)) < 1 && math.Abs(float64(cardA.X-compElems[0].X)) < 0.1 {
		t.Fatal("expected bound arrow or card bounds to update")
	}
	if !(newArrEndX <= cardA.X+1 && newArrEndX >= cardA.X-12) {
		t.Fatalf("external arrow endX=%v not anchored near resized Card A left=%v", newArrEndX, cardA.X)
	}

	if path := os.Getenv("ARCMD_WB_RESIZE_SHOT"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if encErr := png.Encode(f, tt.Image()); encErr != nil {
			_ = f.Close()
			t.Fatal(encErr)
		}
		_ = f.Close()
	}

	// Undo (⌘Z) restores original component bounds as a single history step; Redo (⇧⌘Z) re-applies.
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	var afterUndo []*whiteboard.Element
	for _, el := range board.Scene.Elements {
		if len(el.GroupIDs) > 0 {
			afterUndo = append(afterUndo, el)
		}
	}
	undoBox := whiteboard.SelectionBounds(afterUndo)
	if math.Abs(float64(undoBox.W-origBox.W)) > 1 || math.Abs(float64(undoBox.H-origBox.H)) > 1 {
		t.Fatalf("Undo did not restore original component size: got %.1fx%.1f, want %.1fx%.1f", undoBox.W, undoBox.H, origBox.W, origBox.H)
	}

	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	tt.Frame()
	var afterRedo []*whiteboard.Element
	for _, el := range board.Scene.Elements {
		if len(el.GroupIDs) > 0 {
			afterRedo = append(afterRedo, el)
		}
	}
	redoBox := whiteboard.SelectionBounds(afterRedo)
	if math.Abs(float64(redoBox.W-resizedBox.W)) > 1 || math.Abs(float64(redoBox.H-resizedBox.H)) > 1 {
		t.Fatalf("Redo did not restore resized component size: got %.1fx%.1f, want %.1fx%.1f", redoBox.W, redoBox.H, resizedBox.W, resizedBox.H)
	}
}

func TestBoardEditInsideLibraryComponentGroup(t *testing.T) {
	builtins := whiteboard.BuiltinLibrary()
	var browser whiteboard.LibraryItem
	for _, it := range builtins {
		if it.ID == "builtin-browser" {
			browser = it
			break
		}
	}
	if len(browser.Elements) == 0 {
		t.Fatal("missing builtin-browser library item")
	}

	scene := whiteboard.NewScene()
	compElems := whiteboard.InstantiateLibraryItem(browser, 450, 310)
	scene.Elements = append(scene.Elements, compElems...)
	outside := whiteboard.NewElement(whiteboard.KindRectangle, 40, 260, 120, 72)
	outside.ID = "outside-note"
	outside.Text = "外部节点"
	outside.StrokeColor = "blue"
	outside.FillColor = "blue"
	scene.Elements = append(scene.Elements, outside)

	var cardA, cardB, hero *whiteboard.Element
	for _, el := range compElems {
		switch el.Text {
		case "Card A":
			cardA = el
		case "Card B":
			cardB = el
		case "Hero Section":
			hero = el
		}
	}
	if cardA == nil || cardB == nil || hero == nil || len(cardA.GroupIDs) == 0 {
		t.Fatalf("missing browser child elements or GroupIDs")
	}
	gid := cardA.GroupIDs[len(cardA.GroupIDs)-1]

	board := NewBoard(scene)
	var changed int
	opts := BoardOptions{OnChange: func() { changed++ }}
	tt := ui.NewTester(func(c *ui.Context) {
		o := opts
		o.Theme = c.Theme()
		BoardView(c, board, o)
	}, 1100, 760)
	board.RequestFocus = true
	tt.Frame()
	tt.Frame()

	saveShot := func(name string) {
		dir := os.Getenv("ARCMD_WB_ISOLATION_SHOT_DIR")
		if dir == "" {
			return
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}

	// 1. Single click on Card A selects the entire 6-element component.
	ax, ay := whiteboard.ElementBounds(cardA).Center()
	cax, cay := board.sceneToScreen(board.lastRect, ax, ay)
	tt.ClickAt(cax, cay)
	tt.Frame()
	if len(board.Selected) != len(compElems) || board.editingGroupID != "" {
		t.Fatalf("expected whole component selected outside isolation: sel=%d editingGroup=%q", len(board.Selected), board.editingGroupID)
	}
	if board.groupPill.groupID != gid || board.groupPill.exiting {
		t.Fatalf("expected floating '✎ 编辑内部' groupPill for %q, got %+v", gid, board.groupPill)
	}
	saveShot("group-selected.png")

	// 2. Click floating '✎ 编辑内部' pill to enter group isolation, then Esc to exit and reselect group.
	pill := board.groupPill.rect
	tt.Advance(600 * time.Millisecond)
	tt.ClickAt(pill.X+pill.W/2, pill.Y+pill.H/2)
	tt.Frame()
	if board.editingGroupID != gid {
		t.Fatalf("expected clicking pill to enter group %q, got %q", gid, board.editingGroupID)
	}
	tt.Advance(600 * time.Millisecond)
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if board.editingGroupID != "" || len(board.Selected) != len(compElems) {
		t.Fatalf("expected Esc to exit group and reselect group: group=%q sel=%d", board.editingGroupID, len(board.Selected))
	}

	// 3. Double-click Card A to drill into the group and select ONLY Card A.
	tt.Advance(600 * time.Millisecond)
	tt.ClickAt(cax, cay)
	tt.Advance(80 * time.Millisecond)
	tt.ClickAt(cax, cay)
	tt.Frame()
	if board.editingGroupID != gid || len(board.Selected) != 1 || !board.Selected[cardA.ID] {
		t.Fatalf("expected double-click to isolate %q and select only Card A: group=%q sel=%+v", gid, board.editingGroupID, board.Selected)
	}

	// 4. Resize Card A individually and change its color without touching Card B or Hero.
	origAW, origAH := cardA.W, cardA.H
	origBColor, origBW := cardB.StrokeColor, cardB.W
	origHeroColor := hero.StrokeColor
	se := whiteboard.HandlePoints(cardA)[whiteboard.HandleSE]
	hx, hy := board.sceneToScreen(board.lastRect, se[0], se[1])
	tt.Advance(600 * time.Millisecond)
	tt.Press(hx, hy)
	tt.Frame()
	tt.Move(hx+36, hy+24)
	tt.Frame()
	tt.Release(hx+36, hy+24)
	tt.Frame()
	board.setStrokeColor("purple", opts)
	board.setFillColor("purple", opts)
	tt.Frame()

	if cardA.W <= origAW+20 || cardA.H <= origAH+12 || cardA.StrokeColor != "purple" {
		t.Fatalf("expected Card A resized & purple, got w=%.1f h=%.1f color=%q", cardA.W, cardA.H, cardA.StrokeColor)
	}
	if cardB.StrokeColor != origBColor || cardB.W != origBW || hero.StrokeColor != origHeroColor {
		t.Fatalf("sibling shapes mutated during isolated Card A edit: cardB=(%q,%.1f) hero=%q", cardB.StrokeColor, cardB.W, hero.StrokeColor)
	}
	saveShot("group-editing-card-a.png")

	// 5. Single-click Card B inside the isolated group -> selects only Card B.
	bx, by := whiteboard.ElementBounds(cardB).Center()
	cbx, cby := board.sceneToScreen(board.lastRect, bx, by)
	tt.Advance(600 * time.Millisecond)
	tt.ClickAt(cbx, cby)
	tt.Frame()
	if len(board.Selected) != 1 || !board.Selected[cardB.ID] {
		t.Fatalf("expected single-click inside group to select only Card B, got %+v", board.Selected)
	}

	// 6. Double-click Card B while already inside group -> opens inline text editing for Card B.
	tt.Advance(80 * time.Millisecond)
	tt.ClickAt(cbx, cby)
	tt.Frame()
	if board.editingID != cardB.ID {
		t.Fatalf("expected 2nd double-click on Card B inside group to open text edit, editingID=%q", board.editingID)
	}
	tt.Type(" Edited")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if board.editingID != "" || cardB.Text != "Card B Edited" || board.editingGroupID != gid {
		t.Fatalf("expected text commit inside group: editingID=%q text=%q group=%q", board.editingID, cardB.Text, board.editingGroupID)
	}

	// 7. Esc exits group isolation and re-selects the whole browser group; 2nd Esc clears selection.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if board.editingGroupID != "" || len(board.Selected) != len(compElems) {
		t.Fatalf("expected Esc to exit group isolation and reselect component: group=%q sel=%d", board.editingGroupID, len(board.Selected))
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if len(board.Selected) != 0 {
		t.Fatalf("expected second Esc to clear selection, got %d", len(board.Selected))
	}

	// 8. ⌘ + Click directly isolates and selects Card A in one click.
	tt.Advance(600 * time.Millisecond)
	ax, ay = whiteboard.ElementBounds(cardA).Center()
	cax, cay = board.sceneToScreen(board.lastRect, ax, ay)
	tt.ClickAtWith(ui.Cmd, cax, cay)
	tt.Frame()
	if board.editingGroupID != gid || len(board.Selected) != 1 || !board.Selected[cardA.ID] {
		t.Fatalf("expected ⌘+Click to select Card A directly inside group %q: group=%q sel=%+v", gid, board.editingGroupID, board.Selected)
	}
	// Release completes any started dragMove cleanly.
	tt.Advance(200 * time.Millisecond)
	_ = outside
}

func TestBoardFreedrawContinuousWithoutSelectionBox(t *testing.T) {
	scene := whiteboard.NewScene()
	rect := whiteboard.NewElement(whiteboard.KindRectangle, 80, 80, 120, 70)
	rect.ID = "existing-rect"
	scene.Elements = append(scene.Elements, rect)

	board := NewBoard(scene)
	board.Selected = map[string]bool{rect.ID: true}
	tt := ui.NewTester(func(c *ui.Context) {
		BoardView(c, board, BoardOptions{Theme: c.Theme()})
	}, 960, 640)
	board.RequestFocus = true
	tt.Frame()
	tt.Frame()

	// Switching to Freedraw clears prior selection and stays unlocked.
	tt.Key(0, ui.KeyP)
	tt.Frame()
	if board.Tool != ToolFreedraw || board.ToolLocked {
		t.Fatalf("expected unlocked ToolFreedraw after pressing P, tool=%v locked=%v", board.Tool, board.ToolLocked)
	}
	if len(board.Selected) != 0 {
		t.Fatalf("expected switching to ToolFreedraw to clear selection, got %+v", board.Selected)
	}

	// First stroke: neither mid-drag nor release selects the stroke, and tool remains ToolFreedraw.
	sx0, sy0 := board.sceneToScreen(board.lastRect, 260, 220)
	sx1, sy1 := board.sceneToScreen(board.lastRect, 360, 220)
	tt.Press(sx0, sy0)
	tt.Frame()
	tt.Move(sx1, sy1)
	tt.Frame()
	if board.dragMode != dragCreate || len(board.Selected) != 0 {
		t.Fatalf("expected freedraw dragCreate with no selection box, dragMode=%v selected=%+v", board.dragMode, board.Selected)
	}
	tt.Release(sx1, sy1)
	tt.Frame()
	if board.Tool != ToolFreedraw {
		t.Fatalf("expected ToolFreedraw to remain active after first stroke when unlocked, got %v", board.Tool)
	}
	if len(board.Selected) != 0 {
		t.Fatalf("expected no selection box after freedraw stroke, got %+v", board.Selected)
	}
	if len(board.Scene.Elements) != 2 || board.Scene.Elements[1].Kind != whiteboard.KindFreedraw {
		t.Fatalf("expected first freedraw stroke added, got %+v", board.Scene.Elements)
	}
	stroke1 := board.Scene.Elements[1]

	// Second stroke directly without re-selecting tool or locking Q.
	sx2, sy2 := board.sceneToScreen(board.lastRect, 260, 320)
	sx3, sy3 := board.sceneToScreen(board.lastRect, 380, 360)
	tt.Press(sx2, sy2)
	tt.Frame()
	tt.Move(sx3, sy3)
	tt.Frame()
	tt.Release(sx3, sy3)
	tt.Frame()
	if board.Tool != ToolFreedraw || len(board.Selected) != 0 {
		t.Fatalf("expected continuous freedraw with no selection after 2nd stroke, tool=%v selected=%+v", board.Tool, board.Selected)
	}
	if len(board.Scene.Elements) != 3 || board.Scene.Elements[2].Kind != whiteboard.KindFreedraw {
		t.Fatalf("expected second freedraw stroke added continuously, got %+v", board.Scene.Elements)
	}

	// Esc exits back to ToolSelect, where clicking stroke1 selects it and shows its edit box.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if board.Tool != ToolSelect {
		t.Fatalf("expected Esc to return to ToolSelect, got %v", board.Tool)
	}
	cx, cy := board.sceneToScreen(board.lastRect, 310, 220)
	tt.ClickAt(cx, cy)
	tt.Frame()
	if len(board.Selected) != 1 || !board.Selected[stroke1.ID] {
		t.Fatalf("expected clicking stroke1 in ToolSelect to select it, got %+v", board.Selected)
	}
}
