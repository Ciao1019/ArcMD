package editor

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestResizeReflowsVisibleTextAndFinishesExactly(t *testing.T) {
	var source strings.Builder
	source.WriteString("# Resize\n\n")
	for i := range 160 {
		fmt.Fprintf(&source, "Paragraph %d：调整分屏时应实时换行，屏幕外的段落不应反复重排。留下足够的文字，验证换行和滚动位置。\n\n", i)
	}
	e := New(source.String())
	width := float32(620)
	resizing := false
	tt := ui.NewTester(func(c *ui.Context) { View(c, e, Options{Width: width, Theme: c.Theme(), Resizing: resizing}) }, 640, 500)
	tt.Frame()
	last := e.rows[len(e.rows)-1].leaf.Block.ID
	oldLast := e.cache[last].layout
	oldFirst := e.rows[1].layout
	width, resizing = 420, true
	tt.Frame()
	if e.rows[1].layout == oldFirst {
		t.Fatal("visible text did not reflow")
	}
	if e.cache[last].layout != oldLast || !e.resizePending {
		t.Fatal("offscreen text was reshaped during drag")
	}
	// Scrolling while holding the divider must also refresh newly visible rows.
	e.Scroll.Y = e.rows[80].rect.Y
	tt.Frame()
	if cached := e.cache[e.rows[80].leaf.Block.ID]; cached.width != width-2*e.padding {
		t.Fatal("newly visible text kept stale wrapping")
	}
	anchor := e.resizeAnchor()
	resizing = false
	tt.Frame()
	if e.resizePending || e.cache[last].layout == oldLast {
		t.Fatal("release left approximate geometry")
	}
	if got := e.resizeAnchor(); got.id != anchor.id || math.Abs(float64(got.offset-anchor.offset)) > 1 {
		t.Fatalf("release changed reading anchor: %+v -> %+v", anchor, got)
	}
	pixels := bytes.Clone(tt.Image().Pix)
	height := e.height
	e.ready = false
	clear(e.cache)
	tt.Frame()
	if math.Abs(float64(height-e.height)) > .01 || !bytes.Equal(pixels, tt.Image().Pix) {
		t.Fatal("released resize differs from complete layout")
	}
}

// Table cells are laid out at y 0 and moved into place afterwards; resize
// deferral must still judge them by their real position, or a long table
// below the fold is reshaped on every frame of a divider drag.
func TestResizeDefersOffscreenTableCells(t *testing.T) {
	var source strings.Builder
	source.WriteString("# Resize\n\n")
	for i := range 40 {
		fmt.Fprintf(&source, "Paragraph %d：表格在屏幕外时，拖动分屏不应重排它的单元格。\n\n", i)
	}
	source.WriteString("| A | B | C |\n|---|---|---|\n")
	for i := range 30 {
		fmt.Fprintf(&source, "| 单元格 %d | 把结构化数据渲染成卡片、表格或审批界面 | 普通内容 |\n", i)
	}
	e := New(source.String())
	width := float32(620)
	resizing := false
	tt := ui.NewTester(func(c *ui.Context) { View(c, e, Options{Width: width, Theme: c.Theme(), Resizing: resizing}) }, 640, 500)
	tt.Frame()
	var cells []string
	for _, r := range e.rows {
		if r.leaf.Block.Kind == "cell" {
			cells = append(cells, r.leaf.Block.ID)
		}
	}
	if len(cells) == 0 || e.rows[len(e.rows)-1].rect.Y < 2000 {
		t.Fatal("fixture table is not below the fold")
	}
	before := map[string]float32{}
	for _, id := range cells {
		before[id] = e.cache[id].width
	}
	width, resizing = 420, true
	tt.Frame()
	for _, id := range cells {
		if e.cache[id].width != before[id] {
			t.Fatal("offscreen table cell was reshaped during drag")
		}
	}
	// Scrolled onto the table, its visible cells wrap at the new width.
	e.Scroll.Y = e.rows[len(e.rows)-1].rect.Y - 200
	tt.Frame()
	if e.cache[cells[len(cells)-1]].width == before[cells[len(cells)-1]] {
		t.Fatal("visible table cell kept stale wrapping")
	}
	resizing = false
	tt.Frame()
	for _, id := range cells {
		if e.cache[id].width == before[id] {
			t.Fatal("release left a table cell at the old width")
		}
	}
}
