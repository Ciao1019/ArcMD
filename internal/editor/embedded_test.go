package editor

import (
	"math"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Embedding the interface is enough for ui.Tester placement: it never calls
// platform methods. This is NOT a real browser or a JavaScript runtime test.
type placementOnlyWebView struct{ ui.NativeWebView }

func TestEmbeddedBlockNativeLayoutAndSourceFallback(t *testing.T) {
	source := "# Native heading\n\n```arcmd-jsx-0\n<Counter initial={5} />\n```\n\nAfter island.\n\n" + strings.Repeat("More native reading.\n\n", 40)
	e := New(source)
	e.ReadOnly = true
	b := e.Doc.Blocks[1]
	view := &placementOnlyWebView{}
	blocks := map[string]EmbeddedBlock{b.ID: {View: view, Height: 240}}
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), EmbeddedBlocks: blocks})
	}, 900, 650)
	tt.Frame()
	row := e.rows[e.rowIDs[b.ID]]
	if row.rect.H != 256 {
		t.Fatalf("reserved native height: %v", row.rect.H)
	}
	x, y := row.rect.X+20, row.rect.Y+20
	if tt.WebViewAt(x, y) != view {
		t.Fatal("island not over native placeholder")
	}
	if e.rows[e.rowIDs[e.Doc.Blocks[2].ID]].rect.Y < row.rect.Y+256 {
		t.Fatal("following native paragraph overlaps island")
	}
	// Native wheel forwarding from WebKit needs a platform GUI test.
	// Here the wheel is deliberately over the native margin.
	tt.Scroll(850, 600, 0, 100)
	if e.Scroll.Y <= 0 {
		t.Fatal("outer native document did not scroll")
	}
	if tt.WebViewAt(x, y-e.Scroll.Y) != view {
		t.Fatal("web view did not follow native scroll")
	}
	tt.Scroll(850, 600, 0, 1200)
	if tt.WebViewAt(x, 100) != nil {
		t.Fatal("offscreen web view still takes pointer")
	}
	e.Scroll.Y = 0
	tt.Frame()
	blocks[b.ID] = EmbeddedBlock{View: view, Height: 360}
	tt.Frame()
	if e.rows[e.rowIDs[b.ID]].rect.H != 376 {
		t.Fatal("height update did not invalidate layout")
	}
	blocks = nil
	tt.Frame()
	if tt.WebViewAt(x, y) != nil {
		t.Fatal("source mode retained web view")
	}
	if e.rows[e.rowIDs[b.ID]].rect.H == 376 {
		t.Fatal("source fallback did not restore native height")
	}
	if got := ExportMarkdown(e.Doc); got != source {
		t.Fatalf("reading changed the source: %q", got)
	}
	if e.Version() != 0 {
		t.Fatal("reading created an edit transaction")
	}
}

func TestEmbeddedBlocksRequireCodeAndFiniteHeight(t *testing.T) {
	e := New("Text\n\n```jsx\n<Counter />\n```\n")
	view := &placementOnlyWebView{}
	id := e.Doc.Blocks[1].ID
	blocks := map[string]EmbeddedBlock{id: {View: view, Height: float32(math.NaN())}, e.Doc.Blocks[0].ID: {View: view}, "missing": {View: view}}
	e.syncEmbeddedBlocks(blocks)
	if len(e.embeddedBlocks) != 1 || e.embeddedBlocks[id].Height != 240 {
		t.Fatal("invalid block or NaN height accepted")
	}
	blocks[id] = EmbeddedBlock{Height: 100}
	e.syncEmbeddedBlocks(blocks)
	if len(e.embeddedBlocks) != 0 {
		t.Fatal("nil view should leave source visible")
	}
}

func TestEmbeddedBlockShrinksToMeasuredContentHeight(t *testing.T) {
	source := "# Native heading\n\n```arcmd-jsx-0\n<Counter initial={5} />\n```\n\nAfter island.\n"
	e := New(source)
	e.ReadOnly = true
	b := e.Doc.Blocks[1]
	view := &placementOnlyWebView{}
	blocks := map[string]EmbeddedBlock{b.ID: {View: view, Height: 240}}
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), EmbeddedBlocks: blocks})
	}, 900, 650)
	tt.Frame()
	row := e.rows[e.rowIDs[b.ID]]
	if row.rect.H != 256 {
		t.Fatalf("initial reserved height: %v", row.rect.H)
	}
	afterY := e.rows[e.rowIDs[e.Doc.Blocks[2].ID]].rect.Y
	blocks[b.ID] = EmbeddedBlock{View: view, Height: 112}
	tt.Frame()
	row = e.rows[e.rowIDs[b.ID]]
	if row.rect.H != 128 {
		t.Fatalf("layout did not follow measured height: %v", row.rect.H)
	}
	next := e.rows[e.rowIDs[e.Doc.Blocks[2].ID]]
	if next.rect.Y < row.rect.Y+128 {
		t.Fatal("following paragraph overlaps the resized island")
	}
	if next.rect.Y >= afterY {
		t.Fatal("following paragraph should move up when the island shrinks")
	}
}
