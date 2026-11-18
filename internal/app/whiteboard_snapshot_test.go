package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func buildSampleArchitectureScene() (*whiteboard.Scene, string) {
	s := whiteboard.NewScene()

	frame := whiteboard.NewElement(whiteboard.KindFrame, 20, 16, 740, 444)
	frame.Name = "Core Pipeline · 核心链路"
	frame.StrokeColor = "gray"

	cloud := whiteboard.NewElement(whiteboard.KindCloud, 48, 86, 150, 96)
	cloud.Text = "CDN / Edge\n全球加速"
	cloud.StrokeColor = "blue"
	cloud.FillColor = "blue"
	cloud.FillStyle = whiteboard.FillHachure
	cloud.FrameID = frame.ID

	gw := whiteboard.NewElement(whiteboard.KindHexagon, 286, 88, 156, 92)
	gw.Text = "API 网关\n鉴权 · 限流"
	gw.StrokeColor = "orange"
	gw.FillColor = "orange"
	gw.FillStyle = whiteboard.FillCrossHatch
	gw.FrameID = frame.ID

	stream := whiteboard.NewElement(whiteboard.KindParallelogram, 534, 92, 174, 84)
	stream.Text = "Event Stream\nKafka 管道"
	stream.StrokeColor = "green"
	stream.FillColor = "green"
	stream.FillStyle = whiteboard.FillHachure
	stream.FrameID = frame.ID

	store := whiteboard.NewElement(whiteboard.KindCylinder, 294, 294, 140, 108)
	store.Text = "Vault 数据库\nPrimary + WAL"
	store.StrokeColor = "purple"
	store.FillColor = "purple"
	store.FillStyle = whiteboard.FillSolid
	store.FrameID = frame.ID

	// Bound rough arrows showcasing straight, curved, and 90° elbow orthogonal routing.
	a1 := whiteboard.NewElement(whiteboard.KindArrow, 198, 134, 88, 0)
	a1.Points = [][2]float32{{0, 0}, {88, 0}}
	a1.EndArrowhead = whiteboard.ArrowOpen
	a1.StrokeColor = "ink"
	a1.Text = "HTTPS"
	a1.StartBinding = &whiteboard.Binding{ElementID: cloud.ID, Gap: 6}
	a1.EndBinding = &whiteboard.Binding{ElementID: gw.ID, Gap: 6}

	a2 := whiteboard.NewElement(whiteboard.KindArrow, 442, 134, 92, 0)
	a2.Points = [][2]float32{{0, 0}, {46, -24}, {92, 0}}
	a2.Smooth = true
	a2.EndArrowhead = whiteboard.ArrowOpen
	a2.StrokeColor = "blue"
	a2.Text = "gRPC"
	a2.StartBinding = &whiteboard.Binding{ElementID: gw.ID, Gap: 6}
	a2.EndBinding = &whiteboard.Binding{ElementID: stream.ID, Gap: 6}

	a3 := whiteboard.NewElement(whiteboard.KindArrow, 364, 180, 0, 114)
	a3.Points = [][2]float32{{0, 0}, {0, 114}}
	a3.EndArrowhead = whiteboard.ArrowTriangle
	a3.StrokeColor = "orange"
	a3.StrokeStyle = whiteboard.StrokeDashed
	a3.Text = "Sync"
	a3.StartBinding = &whiteboard.Binding{ElementID: gw.ID, Gap: 6}
	a3.EndBinding = &whiteboard.Binding{ElementID: store.ID, Gap: 6}

	a4 := whiteboard.NewElement(whiteboard.KindArrow, 621, 176, -187, 172)
	a4.Points = [][2]float32{{0, 0}, {0, 172}, {-187, 172}}
	a4.Elbowed = true
	a4.EndArrowhead = whiteboard.ArrowTriangle
	a4.StrokeColor = "green"
	a4.Text = "WAL"
	a4.StartBinding = &whiteboard.Binding{ElementID: stream.ID, Gap: 6}
	a4.EndBinding = &whiteboard.Binding{ElementID: store.ID, Gap: 6}

	// Folded-corner linked Markdown document card.
	docCard := whiteboard.NewElement(whiteboard.KindNote, 46, 290, 178, 112)
	docCard.StrokeColor = "blue"
	docCard.FillColor = "blue"
	docCard.FillStyle = whiteboard.FillHachure
	docCard.Text = "架构设计评审\n[[架构设计评审]]\n采用 gRPC 与本地签名校验"
	docCard.Link = "[[架构设计评审]]"
	docCard.FrameID = frame.ID

	// Speech-bubble callout annotation.
	callout := whiteboard.NewElement(whiteboard.KindCallout, 536, 286, 176, 108)
	callout.StrokeColor = "red"
	callout.FillColor = "red"
	callout.FillStyle = whiteboard.FillHachure
	callout.Text = "Q4 压测达标\nP99 < 18ms"
	callout.FrameID = frame.ID

	// Freedraw annotation curve highlighting the API gateway.
	free := whiteboard.NewElement(whiteboard.KindFreedraw, 290, 198, 148, 18)
	free.StrokeColor = "red"
	free.StrokeWidth = 2.5
	free.Points = [][2]float32{{0, 10}, {32, 4}, {74, 12}, {116, 3}, {148, 9}}

	s.Elements = []*whiteboard.Element{frame, cloud, gw, stream, store, a1, a2, a3, a4, docCard, callout, free}
	whiteboard.UpdateBindings(s)
	return s, docCard.ID
}

func buildInlineSketchScene() *whiteboard.Scene {
	s := whiteboard.NewScene()
	n1 := whiteboard.NewElement(whiteboard.KindRectangle, 30, 50, 140, 68)
	n1.Text = "Q4 规划\n需求评审"
	n1.StrokeColor = "blue"
	n1.FillColor = "blue"
	n1.FillStyle = whiteboard.FillHachure
	n1.Roundness = true

	n2 := whiteboard.NewElement(whiteboard.KindDiamond, 230, 38, 120, 92)
	n2.Text = "灰度发布"
	n2.StrokeColor = "orange"
	n2.FillColor = "orange"
	n2.FillStyle = whiteboard.FillCrossHatch

	n3 := whiteboard.NewElement(whiteboard.KindEllipse, 410, 50, 130, 68)
	n3.Text = "全量上线"
	n3.StrokeColor = "green"
	n3.FillColor = "green"
	n3.FillStyle = whiteboard.FillHachure

	a1 := whiteboard.NewElement(whiteboard.KindArrow, 170, 84, 60, 0)
	a1.Points = [][2]float32{{0, 0}, {60, 0}}
	a1.EndArrowhead = "arrow"
	a1.StartBinding = &whiteboard.Binding{ElementID: n1.ID, Gap: 6}
	a1.EndBinding = &whiteboard.Binding{ElementID: n2.ID, Gap: 6}

	a2 := whiteboard.NewElement(whiteboard.KindArrow, 350, 84, 60, 0)
	a2.Points = [][2]float32{{0, 0}, {60, 0}}
	a2.EndArrowhead = "arrow"
	a2.StartBinding = &whiteboard.Binding{ElementID: n2.ID, Gap: 6}
	a2.EndBinding = &whiteboard.Binding{ElementID: n3.ID, Gap: 6}

	s.Elements = []*whiteboard.Element{n1, n2, n3, a1, a2}
	whiteboard.UpdateBindings(s)
	return s
}

func TestWhiteboardSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render whiteboard snapshots")

	setupSpace := func(t *testing.T, dark bool) (*App, *ui.Tester, string, string) {
		t.Helper()
		a, tt := newTestApp(t)
		tt.SetSize(1440, 920)
		sp := a.state.ActiveSpace()
		if dark {
			tt.SetDark(true)
			sp.SetTheme(space.Theme{Colors: []string{"#5B6EE1"}, Appearance: space.AppearanceDark, Intensity: 0.5})
		} else {
			sp.SetTheme(space.Theme{Colors: []string{"#DCE8F8", "#ECE7F8"}, Appearance: space.AppearanceLight, Intensity: 0.45})
		}

		reviewMD := "# 架构设计评审\n\n> [!NOTE]\n> 本文档与 [[系统架构白板]] 保持双向关联。\n\n## 核心模块\n\n- [x] API 网关统一鉴权与限流策略\n- [x] Vault 本地持久化和 `.excalidraw.svg` 矢量无损双态存储\n- [ ] 多端实时增量同步压测\n\n## 实时结构草图\n\n![系统架构白板](系统架构白板.excalidraw.svg)\n"
		if err := writeNative(root(a), "架构设计评审.md", reviewMD); err != nil {
			t.Fatal(err)
		}

		archScene, noteID := buildSampleArchitectureScene()
		archRaw, err := encodeWhiteboardFile("系统架构白板.excalidraw.svg", archScene)
		if err != nil {
			t.Fatal(err)
		}
		if err := vault.Write(root(a), "系统架构白板.excalidraw.svg", archRaw); err != nil {
			t.Fatal(err)
		}

		sketch := buildInlineSketchScene()
		sketchRaw, err := encodeWhiteboardFile("assets/产品路线图-白板.excalidraw.svg", sketch)
		if err != nil {
			t.Fatal(err)
		}
		if err := vault.Write(root(a), "assets/产品路线图-白板.excalidraw.svg", sketchRaw); err != nil {
			t.Fatal(err)
		}
		roadmapMD := "# 产品路线图 Q4\n\n参考整体拓扑见 [[系统架构白板]]，以下标准 Markdown 图片嵌入兼容 Typora，并可直接展开手绘编辑。\n\n![手绘白板](assets/产品路线图-白板.excalidraw.svg)\n\n## 发布检查清单\n\n- 确认 SVG 与矢量 PDF 导出保真度\n- 确认双链重命名与 AI 问答引用定位\n"
		if err := writeNative(root(a), "产品路线图.md", roadmapMD); err != nil {
			t.Fatal(err)
		}

		a.refreshTree()
		a.refreshSearch()
		return a, tt, "系统架构白板.excalidraw.svg", noteID
	}

	t.Run("standalone-light", func(t *testing.T) {
		a, tt, boardRel, noteID := setupSpace(t, false)
		a.open(boardRel)
		tt.Frame()
		a.doc.board.ShowGrid = true
		a.doc.board.Selected = map[string]bool{noteID: true}
		a.doc.board.FitToContent(ui.Rect{})
		a.doc.board.Zoom = 1.0
		a.doc.board.PanX = 248
		a.doc.board.PanY = 176
		for range 4 {
			tt.Frame()
			tt.Advance(150 * time.Millisecond)
		}
		savePNG(t, filepath.Join(dir, "whiteboard-standalone-light.png"), tt)
	})

	t.Run("standalone-dark", func(t *testing.T) {
		a, tt, boardRel, _ := setupSpace(t, true)
		a.open(boardRel)
		tt.Frame()
		a.doc.board.ShowGrid = true
		if len(a.doc.board.Scene.Elements) > 2 {
			a.doc.board.Selected = map[string]bool{a.doc.board.Scene.Elements[2].ID: true}
		}
		a.doc.board.FitToContent(ui.Rect{})
		a.doc.board.Zoom = 1.0
		a.doc.board.PanX = 248
		a.doc.board.PanY = 176
		for range 4 {
			tt.Frame()
			tt.Advance(150 * time.Millisecond)
		}
		savePNG(t, filepath.Join(dir, "whiteboard-standalone-dark.png"), tt)
	})

	t.Run("split-md", func(t *testing.T) {
		a, tt, boardRel, _ := setupSpace(t, false)
		a.open("架构设计评审.md")
		tt.Frame()
		a.openSplit(a.state.ActiveSpace(), boardRel, false, false)
		settle(tt)
		for range 4 {
			tt.Frame()
			tt.Advance(150 * time.Millisecond)
		}
		savePNG(t, filepath.Join(dir, "whiteboard-split-md.png"), tt)
	})

	t.Run("inline-md", func(t *testing.T) {
		a, tt, _, _ := setupSpace(t, false)
		a.open("产品路线图.md")
		tt.Frame()
		if len(a.doc.ed.Doc.Blocks) >= 3 {
			blockID := a.doc.ed.Doc.Blocks[2].ID
			a.doc.ed.EditWhiteboardBlock(blockID)
		}
		for range 4 {
			tt.Frame()
			tt.Advance(150 * time.Millisecond)
		}
		savePNG(t, filepath.Join(dir, "whiteboard-inline-md.png"), tt)
	})

	t.Run("shapes-and-library", func(t *testing.T) {
		a, tt, boardRel, noteID := setupSpace(t, false)
		a.open(boardRel)
		tt.Frame()
		a.doc.board.ShowGrid = true
		a.doc.board.Selected = map[string]bool{noteID: true}
		a.doc.board.SetShapesPopoverOpen(true)
		a.doc.board.SetLibraryOpen(true)
		a.doc.board.FitToContent(ui.Rect{})
		a.doc.board.Zoom = 0.82
		a.doc.board.PanX = 236
		a.doc.board.PanY = 305
		for range 4 {
			tt.Frame()
			tt.Advance(150 * time.Millisecond)
		}
		savePNG(t, filepath.Join(dir, "whiteboard-shapes-library.png"), tt)
	})
}
