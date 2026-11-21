package mindmap

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSingleSideDefaultAndBothSides(t *testing.T) {
	var s State
	var selected, clicked string
	var dual, reveal bool
	items := []Item{{ID: "a", Text: "Alpha"}, {ID: "leaf", Text: "Leaf", Depth: 1}, {ID: "b", Text: "Beta"}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.ScrollBoth(c).Fill().Children(func() {
			View(c, &s, Item{ID: "r", Text: "Root"}, items, Options{Zoom: 1, BothSides: dual, Selected: selected, Reveal: reveal,
				FoldLabel: "Fold", UnfoldLabel: "Unfold", Chosen: func(id string) { clicked = id }})
		})
	}, 1000, 550)
	tt.Frame()
	x := func(name string) float32 {
		t.Helper()
		r, ok := tt.Find(name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		return r.X + r.W/2
	}
	if x("Alpha") <= x("Root") || x("Beta") <= x("Root") {
		t.Fatal("expected right-sided branches")
	}
	if err := tt.Click("Fold"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("Leaf"); ok {
		t.Fatal("branch did not fold")
	}
	dual = true
	tt.Frame()
	if x("Alpha") <= x("Root") || x("Beta") >= x("Root") {
		t.Fatal("expected two-sided branches")
	}
	items = append([]Item{{ID: "new", Text: "New"}}, items...)
	tt.Frame()
	if _, ok := tt.Find("Leaf"); ok {
		t.Fatal("insertion or layout change lost the fold")
	}
	selected = "leaf"
	reveal = true
	tt.Frame()
	if _, ok := tt.Find("Leaf"); !ok {
		t.Fatal("moving the caret did not unfold its branch")
	}
	if err := tt.Click("Leaf"); err != nil || clicked != "leaf" {
		t.Fatalf("click got %q: %v", clicked, err)
	}
}

func TestShowDepthFoldSubtreeAndExportSVG(t *testing.T) {
	var s State
	items := []Item{
		{ID: "a", Text: "Alpha", Depth: 0},
		{ID: "a1", Text: "Alpha-1", Depth: 1},
		{ID: "a1a", Text: "Alpha-1-a", Depth: 2},
		{ID: "b", Text: "Beta", Depth: 0},
	}
	var svg string
	tt := ui.NewTester(func(c *ui.Context) {
		View(c, &s, Item{ID: "r", Text: "Root"}, items, Options{Zoom: 1, FoldLabel: "Fold", UnfoldLabel: "Unfold"})
		if svg == "" {
			svg = ExportSVG(c, &s, Item{ID: "r", Text: "Root"}, items, Options{Zoom: 1})
		}
	}, 1000, 600)
	tt.Frame()

	if !s.HasChildren("a") || !s.HasFoldableDescendant("a") {
		t.Fatal("expected Alpha to have foldable descendants")
	}
	s.FoldSubtree(items, "a", true)
	tt.Frame()
	if !s.HasFoldedDescendant(items, "a") || !s.IsFolded("a1") {
		t.Fatal("FoldSubtree(true) did not fold Alpha-1")
	}
	s.FoldSubtree(items, "a", false)
	tt.Frame()
	if s.HasFoldedDescendant(items, "a") || s.IsFolded("a1") {
		t.Fatal("FoldSubtree(false) did not unfold Alpha-1")
	}

	s.ShowDepth(items, 1)
	tt.Frame()
	if !s.IsFolded("a") {
		t.Fatal("ShowDepth(1) did not fold depth-0 branch")
	}
	s.ShowDepth(items, 2)
	tt.Frame()
	if s.IsFolded("a") || !s.IsFolded("a1") {
		t.Fatalf("ShowDepth(2) folded a=%v a1=%v; want false, true", s.IsFolded("a"), s.IsFolded("a1"))
	}
	s.ShowDepth(items, 0)
	tt.Frame()
	if s.HasFolded() {
		t.Fatal("ShowDepth(0) left folded branches")
	}

	if !strings.HasPrefix(svg, "<svg ") || !strings.Contains(svg, "Alpha-1-a") || !strings.Contains(svg, "<path ") {
		t.Fatalf("unexpected SVG output: %.120s", svg)
	}
}

func BenchmarkPanMap(b *testing.B) {
	items := make([]Item, 600)
	for i := range items {
		label := fmt.Sprintf("Node %d", i)
		depth := 1
		if i%12 == 0 {
			depth = 0
		}
		items[i] = Item{ID: label, Text: label, Depth: depth}
	}
	var s State
	var y float32
	tt := ui.NewTester(func(c *ui.Context) {
		View(c, &s, Item{ID: "r", Text: "Root"}, items, Options{LayoutRevision: 1, Visible: ui.Rect{X: 70, Y: y, W: 600, H: 460}, Zoom: 1}).Absolute().Left(-70).Top(-y)
	}, 600, 460)
	tt.Frame()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		y = float32(5000 + i%120)
		tt.Frame()
	}
}
