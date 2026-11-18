package editor

import (
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
	"github.com/egoist/mygo/ui"
	"math"
	"strings"
	"testing"
)

func TestMapCanvasDragAndPointerZoom(t *testing.T) {
	ed := New("# Root\n\n- First\n- Second")
	m := &MindMap{}
	tt := ui.NewTester(func(c *ui.Context) {
		m.View(c, ed, Options{Theme: c.Theme()}, 600, nil, false, func(bool) {})
	}, 600, 460)
	tt.Frame()
	box, ok := tt.Find("思维导图画布")
	if !ok {
		t.Fatal("canvas unavailable")
	}
	x, y := box.X+12, box.Y+12
	oldX, oldY := m.panX, m.panY
	tt.Press(x, y)
	tt.Move(x+45, y+22)
	tt.Release(x+45, y+22)
	tt.Frame()
	if m.autoFit || m.panX <= oldX+35 || m.panY <= oldY+15 {
		t.Fatalf("canvas drag (%v,%v) → (%v,%v)", oldX, oldY, m.panX, m.panY)
	}
	z := m.zoom
	tt.Pinch(x+35, y+25, 1.35)
	tt.Frame()
	if m.zoom <= z {
		t.Fatal("pinch did not zoom")
	}
	child, ok := tt.Find("Second")
	if !ok {
		t.Fatal("missing node")
	}
	x, y = child.X+child.W/2, child.Y+child.H/2
	oldX = m.panX
	before := ed.Caret
	tt.Press(x, y)
	tt.Move(x+22, y)
	tt.Release(x+22, y)
	tt.Frame()
	if math.Abs(float64(m.panX-oldX-22)) > 3 || ed.Caret != before {
		t.Fatalf("dragging node jumped or failed to pan: dx=%v caret=%d→%d", m.panX-oldX, before, ed.Caret)
	}
	if err := tt.Click("居中并适应画布"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !m.autoFit || math.Abs(float64(m.panX-(m.viewW-m.tree.Width*m.zoom)/2)) > 1 || math.Abs(float64(m.panY-(m.viewH-m.tree.Height*m.zoom)/2)) > 1 {
		t.Fatal("center control did not fit and center the map after a gesture")
	}

}

func BenchmarkRichMapGesture(b *testing.B) {
	scene := whiteboard.NewScene()
	for i := 0; i < 32; i++ {
		shape := whiteboard.NewElement(whiteboard.KindEllipse, float32(i%8)*30, float32(i/8)*22, 28, 18)
		shape.FillColor = "blue"
		scene.Elements = append(scene.Elements, shape)
	}
	ed := New("# Reading\n\n" + strings.Repeat("## Section\n\n- **Important** and $x^2$\n\n```python\narr = x.numpy()\nprint(arr)\n```\n\n| Item | Cost |\n| --- | --- |\n| A | 12 |\n\n![Sketch](assets/test.excalidraw.svg)\n\n", 5))
	m := &MindMap{}
	tt := ui.NewTester(func(c *ui.Context) {
		m.View(c, ed, Options{Theme: c.Theme(), LoadWhiteboard: func(string) (*whiteboard.Scene, uint64, error) { return scene, 1, nil }}, 800, nil, false, func(bool) {})
	}, 800, 650)
	tt.Frame()
	m.autoFit = false
	m.zoom = .76
	m.panX = 0
	m.panY = -400
	tt.Frame()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		direction := float32(1)
		if i%12 >= 6 {
			direction = -1
		}
		tt.Scroll(390, 330, 0, direction*7)
		tt.Pinch(390, 330, 1+direction*.015)
	}
}
