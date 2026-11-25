package editor

import (
	"fmt"
	"image"
	"math"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func testZoomImage(w, h int) ImageResource {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	return ImageResource{Bitmap: ui.NewBitmap(img), Width: w, Height: h}
}

func TestPageZoomScalesDocumentProportionally(t *testing.T) {
	md := strings.Join([]string{
		"# 页面缩放测试标题",
		"",
		"这是一段包含 **粗体** 和行内公式 $a^2 + b^2 = c^2$ 的正文。",
		"",
		"## 二级标题",
		"",
		"- 无序列表项",
		"1. 有序列表项",
		"- [x] 已完成任务",
		"",
		"> [!NOTE]",
		"> 这是一段高亮提示块。",
		"",
		"```go",
		"func veryLongFunctionNameForHorizontalScrollingCheck(alpha, beta, gamma, delta, epsilon int) int { return alpha + beta + gamma + delta + epsilon }",
		"```",
		"",
		"| 列一 | 列二 |",
		"| --- | --- |",
		"| 单元格 A | 单元格 B |",
		"",
		"$$",
		"\\frac{-b \\pm \\sqrt{b^2 - 4ac}}{2a}",
		"$$",
		"",
		"```mermaid",
		"flowchart LR",
		"  A[Start] --> B[End]",
		"```",
		"",
		"![sample](sample.png)",
	}, "\n")

	e := New(md)
	e.codeNoWrap = true
	zoom := float32(1.0)
	imgRes := testZoomImage(360, 180)
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{
			Width:      w,
			Theme:      c.Theme(),
			Zoom:       zoom,
			CodeNoWrap: true,
			LoadImage: func(src string) ImageResource {
				if src == "sample.png" {
					return imgRes
				}
				return ImageResource{}
			},
		})
	}, 1200, 900)
	tt.Frame()

	type metrics struct {
		height       float32
		titleSize    float32
		bodySize     float32
		h2Size       float32
		codeSize     float32
		cellSize     float32
		tableH       float32
		mathH        float32
		mermaidScale float32
		imgW         float32
		codeOverflow bool
		calloutOK    bool
	}
	capture := func(z float32) metrics {
		zoom = z
		tt.Frame()
		var m metrics
		m.height = e.height
		m.calloutOK = true
		for i := range e.rows {
			r := *e.indexedRow(i)
			b := r.leaf.Block
			switch {
			case b.Kind == "heading" && b.Level == 1 && m.titleSize == 0:
				m.titleSize = r.size
			case b.Kind == "paragraph" && m.bodySize == 0:
				m.bodySize = r.size
			case b.Kind == "heading" && b.Level == 2 && m.h2Size == 0:
				m.h2Size = r.size
			case b.Kind == "code" && !isMermaidBlock(b) && m.codeSize == 0:
				m.codeSize = r.size
				barBox, _, availW, _, _, noWrap := e.codeScrollFor(r)
				if noWrap && barBox.X+availW > r.rect.X+r.rect.W+0.5 {
					m.codeOverflow = true
				}
			case b.Kind == "cell" && m.cellSize == 0:
				m.cellSize = r.size
			case b.Kind == "math" && r.math != nil:
				m.mathH = (r.math.Ascent + r.math.Descent) * r.mathScale
			case isMermaidBlock(b) && r.mermaid != nil:
				m.mermaidScale = r.mermaidScale
			case b.Kind == "image" && r.picture != nil:
				m.imgW = r.picture.bounds.W
			}
		}
		for i := range e.decorations {
			d := *e.indexedDecoration(i)
			if d.kind == "table" && m.tableH == 0 {
				m.tableH = d.rect.H
			}
			if d.kind == "callout" {
				icon := e.calloutIconBox(d)
				if icon.Y+icon.H > d.rect.Y+d.rect.H+0.5 || icon.X+icon.W > d.rect.X+d.rect.W {
					m.calloutOK = false
				}
			}
		}
		return m
	}

	m100 := capture(1.0)
	m150 := capture(1.5)
	m070 := capture(0.7)
	m050 := capture(0.5)
	m200 := capture(2.0)

	near := func(name string, got, want float32) {
		t.Helper()
		if math.Abs(float64(got-want)) > 0.5 {
			t.Errorf("%s = %.2f; want %.2f", name, got, want)
		}
	}

	near("titleSize@150%", m150.titleSize, m100.titleSize*1.5)
	near("bodySize@150%", m150.bodySize, m100.bodySize*1.5)
	near("h2Size@150%", m150.h2Size, m100.h2Size*1.5)
	near("codeSize@150%", m150.codeSize, m100.codeSize*1.5)
	near("cellSize@150%", m150.cellSize, m100.cellSize*1.5)
	near("bodySize@70%", m070.bodySize, m100.bodySize*0.7)
	near("imgW@150%", m150.imgW, m100.imgW*1.5)
	near("imgW@70%", m070.imgW, m100.imgW*0.7)

	if m150.height <= m100.height || m070.height >= m100.height {
		t.Fatalf("document height did not scale monotonically: 70%%=%.1f 100%%=%.1f 150%%=%.1f", m070.height, m100.height, m150.height)
	}
	if m150.tableH <= m100.tableH || m070.tableH >= m100.tableH {
		t.Fatalf("table height did not scale: 70%%=%.1f 100%%=%.1f 150%%=%.1f", m070.tableH, m100.tableH, m150.tableH)
	}
	if m150.mathH <= m100.mathH || m070.mathH >= m100.mathH {
		t.Fatalf("display math did not scale: 70%%=%.1f 100%%=%.1f 150%%=%.1f", m070.mathH, m100.mathH, m150.mathH)
	}
	if m150.mermaidScale <= m100.mermaidScale || m070.mermaidScale >= m100.mermaidScale {
		t.Fatalf("mermaid diagram did not scale: 70%%=%.2f 100%%=%.2f 150%%=%.2f", m070.mermaidScale, m100.mermaidScale, m150.mermaidScale)
	}
	if m200.codeOverflow {
		t.Fatal("unwrapped code block horizontal scroll area overflowed code box at 200% zoom")
	}
	if !m050.calloutOK || !m100.calloutOK || !m200.calloutOK {
		t.Fatalf("callout icon box overflowed container: 50%%=%v 100%%=%v 200%%=%v", m050.calloutOK, m100.calloutOK, m200.calloutOK)
	}
}

func TestPageZoomColumnAndScrollAnchor(t *testing.T) {
	var parts []string
	for i := 1; i <= 40; i++ {
		parts = append(parts, fmt.Sprintf("第 %02d 段：这是一段用于测试页面缩放时列宽扩展以及滚动锚点保持能力的较长正文内容。", i))
	}
	e := New(strings.Join(parts, "\n\n"))
	zoom := float32(1.0)
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), Zoom: zoom})
	}, 1600, 640)
	tt.Frame()

	body100 := e.BodyWidth()
	// Scroll to the middle of the document.
	targetIdx := 20
	e.Scroll.Y = e.rows[targetIdx].rect.Y
	tt.Frame()
	anchorBefore := e.resizeAnchor()

	// Zoom in: column grows proportionally (up to viewport), anchor stays near top.
	zoom = 1.5
	tt.Frame()
	if e.BodyWidth() <= body100 {
		t.Fatalf("expected body width to grow on zoom in: 100%%=%.1f 150%%=%.1f", body100, e.BodyWidth())
	}
	anchor150 := e.resizeAnchor()
	if anchor150.id != anchorBefore.id {
		t.Fatalf("scroll anchor drifted on zoom in: before=%q after=%q", anchorBefore.id, anchor150.id)
	}

	// Zoom out below 100%: column width stays at standard so zooming out shows more text per line.
	zoom = 0.7
	tt.Frame()
	if math.Abs(float64(e.BodyWidth()-body100)) > 0.5 {
		t.Fatalf("zooming out should keep standard body width %.1f, got %.1f", body100, e.BodyWidth())
	}
}

func TestPageZoomPrintingIsolationAndImageResize(t *testing.T) {
	e := New("![sample](sample.png)")
	e.pageZoom = 1.6
	if e.zoomFactor() != 1.6 {
		t.Fatalf("zoomFactor() = %v; want 1.6", e.zoomFactor())
	}
	e.printing = true
	if e.zoomFactor() != 1.0 {
		t.Fatalf("printing zoomFactor() = %v; want 1.0", e.zoomFactor())
	}
	e.printing = false

	// Image width saved while zoomed at 150% divides out the zoom factor so
	// returning to 100% preserves the logical persisted width.
	zoom := float32(1.5)
	imgRes := testZoomImage(400, 200)
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{
			Width: w,
			Theme: c.Theme(),
			Zoom:  zoom,
			LoadImage: func(string) ImageResource {
				return imgRes
			},
		})
	}, 1200, 700)
	tt.Frame()
	r := e.rows[0]
	if r.picture == nil {
		t.Fatal("expected image frame")
	}
	startW := r.picture.bounds.W // 400 * 1.5 = 600
	if math.Abs(float64(startW-600)) > 1 {
		t.Fatalf("zoomed image width = %.1f; want 600", startW)
	}
	// Drag right resize handle inward by 75px (centered image shrinks both sides:
	// visual 600 -> 450, divided by 1.5 zoom -> persisted logical width 300).
	handleX := r.picture.bounds.X + r.picture.bounds.W
	handleY := r.picture.bounds.Y + r.picture.bounds.H/2
	tt.Move(handleX, handleY)
	tt.Frame()
	tt.Press(handleX, handleY)
	tt.Move(handleX-75, handleY)
	tt.Release(handleX-75, handleY)
	tt.Frame()
	if got := e.Doc.Blocks[0].ImageWidth; got < 290 || got > 310 {
		t.Fatalf("persisted image width at 150%% zoom = %.1f; want ~300", got)
	}
}

func TestPageZoomWheelPinchAndModalIsolation(t *testing.T) {
	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, fmt.Sprintf("Line %d content for scroll verification.", i))
	}
	e := New(strings.Join(lines, "\n\n"))
	zoom := float32(1.0)
	var callbacks []float32
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{
			Width: w,
			Theme: c.Theme(),
			Zoom:  zoom,
			OnZoom: func(next float32) {
				callbacks = append(callbacks, next)
				zoom = next
			},
		})
	}, 900, 600)
	tt.Frame()

	// Single Windows mouse wheel notch (DY = -120) with Cmd held is consumed
	// (preventing scroll) and steps exactly +10%, not +50%.
	if !e.input(ui.InputEvent{Kind: ui.InputScroll, Mods: ui.Cmd, X: 450, Y: 300, DY: -120}, nil, nil, nil) {
		t.Fatal("Cmd+Wheel up was not consumed by editor")
	}
	tt.Frame()
	if zoom != 1.1 {
		t.Fatalf("Cmd+Wheel up zoom = %v; want 1.1 (callbacks=%v)", zoom, callbacks)
	}

	// Wheel down steps -10% back to 1.0; small sub-notch deltas accumulate cleanly.
	if !e.input(ui.InputEvent{Kind: ui.InputScroll, Mods: ui.Cmd, X: 450, Y: 300, DY: 10}, nil, nil, nil) {
		t.Fatal("small Cmd+Wheel down was not consumed")
	}
	if zoom != 1.1 {
		t.Fatalf("sub-notch wheel should not step yet, zoom=%v", zoom)
	}
	if !e.input(ui.InputEvent{Kind: ui.InputScroll, Mods: ui.Cmd, X: 450, Y: 300, DY: 20}, nil, nil, nil) {
		t.Fatal("accumulated Cmd+Wheel down was not consumed")
	}
	tt.Frame()
	if zoom != 1.0 {
		t.Fatalf("Cmd+Wheel down zoom = %v; want 1.0", zoom)
	}

	// Unmodified wheel event falls through to ScrollView instead of zooming.
	if e.input(ui.InputEvent{Kind: ui.InputScroll, X: 450, Y: 300, DY: -120}, nil, nil, nil) {
		t.Fatal("unmodified wheel scroll should not be intercepted as zoom")
	}
	if zoom != 1.0 {
		t.Fatalf("unmodified wheel changed zoom to %v", zoom)
	}

	// Trackpad pinch through full ui.Tester event dispatch scales continuously and clamps.
	tt.Pinch(450, 300, 1.35)
	tt.Frame()
	if zoom != 1.35 {
		t.Fatalf("Pinch zoom = %v; want 1.35", zoom)
	}
	tt.Pinch(450, 300, 3.0)
	tt.Frame()
	if zoom != 2.0 {
		t.Fatalf("Pinch upper clamp = %v; want 2.0", zoom)
	}

	// Active zoom modals / inline zoomable embeds isolate page zoom.
	prevCalls := len(callbacks)
	e.imageViewer.open = true
	if !e.HasZoomModal() {
		t.Fatal("HasZoomModal should be true when image lightbox is open")
	}
	if e.handleZoomInput(ui.InputEvent{Kind: ui.InputZoom, Scale: 0.8}) {
		t.Fatal("handleZoomInput must not intercept zoom while image lightbox is open")
	}
	e.imageViewer.open = false

	e.mermaidViewer.open = true
	if !e.HasZoomModal() || e.handleZoomInput(ui.InputEvent{Kind: ui.InputScroll, Mods: ui.Cmd, DY: -120}) {
		t.Fatal("HasZoomModal must isolate Mermaid lightbox")
	}
	e.mermaidViewer.open = false

	e.boardEditing = "b1"
	if !e.HasZoomModal() {
		t.Fatal("HasZoomModal must isolate inline whiteboard editing")
	}
	e.boardEditing = ""

	e.pdfBrowsing = "p1"
	if !e.HasZoomModal() {
		t.Fatal("HasZoomModal must isolate inline PDF browsing")
	}
	e.pdfBrowsing = ""

	if len(callbacks) != prevCalls {
		t.Fatalf("OnZoom fired while zoom modal was active: %v", callbacks)
	}
}
