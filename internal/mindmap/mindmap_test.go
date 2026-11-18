package mindmap

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"testing"
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
