package editor

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func compactTester(markdown string, width int, codeMaxH, imageMaxH float32, res ImageResource) (*Editor, *ui.Tester) {
	e := New(markdown)
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{
			Width:          w,
			Theme:          c.Theme(),
			CodeMaxHeight:  codeMaxH,
			ImageMaxHeight: imageMaxH,
			LoadImage:      func(string) ImageResource { return res },
		})
	}, width, 680)
	tt.Frame()
	return e, tt
}

func makeTallGradientBitmap(w, h int) *ui.Bitmap {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		c := color.RGBA{
			R: uint8(40 + (y*180)/max(1, h)),
			G: uint8(110 + (y*90)/max(1, h)),
			B: uint8(210 - (y*120)/max(1, h)),
			A: 255,
		}
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return ui.NewBitmap(img)
}

func TestCompactCodeBlockScrollExpandAndPrint(t *testing.T) {
	var lines []string
	for i := 1; i <= 22; i++ {
		lines = append(lines, fmt.Sprintf("fmt.Println(\"line %02d\")", i))
	}
	md := "```go\n" + strings.Join(lines, "\n") + "\n```\n\nAfter paragraph"

	// Full height baseline (CodeMaxHeight == 0).
	fullEd, _ := compactTester(md, 900, 0, 0, ImageResource{})
	fullH := fullEd.rows[0].rect.H
	if fullEd.IsCodeCompact(fullEd.Doc.Blocks[0].ID) {
		t.Fatal("expected code block not to be compact when CodeMaxHeight == 0")
	}

	// Capped to 140px.
	e, tt := compactTester(md, 900, 140, 0, ImageResource{})
	b := e.Doc.Blocks[0]
	if !e.IsCodeCompact(b.ID) {
		t.Fatal("expected long code block to be compact when CodeMaxHeight == 140")
	}
	compactH := e.rows[0].rect.H
	if compactH >= fullH-100 {
		t.Fatalf("expected compact height (%v) << full height (%v)", compactH, fullH)
	}
	if !e.rows[0].codeExpandable || e.rows[0].codeViewH != 140 {
		t.Fatalf("unexpected compact code row metadata: %+v", e.rows[0])
	}

	// Vertical wheel scroll inside compact code block moves codeScrollY without moving outer Scroll.Y.
	r := e.rows[0]
	cx := r.rect.X + r.rect.W/2
	cy := r.y + r.codeViewH/2
	tt.Wheel(cx, cy, 0, 90)
	tt.Frame()
	if e.codeScrollY[b.ID] <= 0 {
		t.Fatalf("expected codeScrollY > 0 after wheel scroll, got %v", e.codeScrollY[b.ID])
	}
	if e.Scroll.Y != 0 {
		t.Fatalf("expected outer Scroll.Y to remain 0 while code block consumes scroll, got %v", e.Scroll.Y)
	}

	// Hit testing and caret bounds account for codeScrollY.
	afterScrollY := e.codeScrollY[b.ID]
	at, ok := e.IndexForPoint(ui.Point{X: r.x + 12, Y: r.y + 10})
	if !ok || at <= 0 {
		t.Fatalf("expected scrolled IndexForPoint > 0, got %d (ok=%v)", at, ok)
	}
	e.revealCodeOffsetV(at)
	caretRect, _, ok := e.BoundsForRange(ui.TextInputRange{Start: at, End: at})
	if !ok || caretRect.Y < r.y-2 || caretRect.Y > r.y+r.codeViewH {
		t.Fatalf("expected scrolled caret bounds within visible code viewport [%v, %v], got %+v", r.y, r.y+r.codeViewH, caretRect)
	}

	// Moving caret back to top reveals line 1 and resets codeScrollY to 0.
	e.SetSelection(0, 0)
	e.revealCodeOffsetV(0)
	if e.codeScrollY[b.ID] >= afterScrollY {
		t.Fatalf("expected revealCodeOffsetV(0) to scroll back up from %v, got %v", afterScrollY, e.codeScrollY[b.ID])
	}

	// Scrolling to the bottom clamps to maxScroll, then hands off further downward scroll.
	maxScroll := r.codeContentH - r.codeViewH
	if !e.handleCompactScrollInput(ui.InputEvent{Kind: ui.InputScroll, X: cx, Y: cy, DY: maxScroll + 500}) {
		t.Fatal("expected first scroll to bottom to be handled")
	}
	if got := e.codeScrollY[b.ID]; got < maxScroll-1 || got > maxScroll+1 {
		t.Fatalf("expected codeScrollY clamped near %v, got %v", maxScroll, got)
	}
	if e.handleCompactScrollInput(ui.InputEvent{Kind: ui.InputScroll, X: cx, Y: cy, DY: 40}) {
		t.Fatal("expected downward scroll at bottom boundary to fall through to outer document")
	}

	// Clicking bottom expand pill expands to full height; ReadOnly collapse click returns to compact.
	d := e.decorations[0]
	expRect := e.codeExpandRect(d)
	tt.ClickAt(expRect.X+expRect.W/2, expRect.Y+expRect.H/2-e.Scroll.Y)
	tt.Frame()
	if e.IsCodeCompact(b.ID) {
		t.Fatal("expected code block to expand after clicking expand pill")
	}
	if e.rows[0].rect.H < fullH {
		t.Fatalf("expected expanded height (%v) >= full height (%v)", e.rows[0].rect.H, fullH)
	}

	e.SetReadOnly(true)
	tt.Frame()
	dExpanded := e.decorations[0]
	colRect := e.codeExpandRect(dExpanded)
	tt.ClickAt(colRect.X+colRect.W/2, colRect.Y+colRect.H/2-e.Scroll.Y)
	tt.Frame()
	if !e.IsCodeCompact(b.ID) {
		t.Fatal("expected ReadOnly click on collapse pill to compact code block again")
	}

	// Printing renders full height regardless of CodeMaxHeight.
	e.printing = true
	e.ready = false
	e.layout(Options{Width: 900, Theme: ui.LightTheme(), CodeMaxHeight: 140})
	if e.IsCodeCompact(b.ID) || e.rows[0].rect.H < fullH {
		t.Fatalf("expected printing to expand code block completely: compact=%v H=%v full=%v", e.IsCodeCompact(b.ID), e.rows[0].rect.H, fullH)
	}
}

func TestCompactImageScrollExpandAndPrint(t *testing.T) {
	bmp := makeTallGradientBitmap(400, 520)
	md := "![长截图](tall.png)\n\n" + strings.Repeat("Para.\n\n", 10)
	e, tt := compactTester(md, 900, 0, 180, ImageResource{Bitmap: bmp, Width: 400, Height: 520, Local: true})

	imgBlock := e.Doc.Blocks[0]
	if !e.IsImageCompact(imgBlock.ID) {
		t.Fatal("expected tall image to be compact when ImageMaxHeight == 180")
	}
	fig := e.rows[0].picture
	if fig.bounds.H != 180 || fig.contentH != 520 || !fig.expandable {
		t.Fatalf("unexpected compact image geometry: bounds=%+v contentH=%v expandable=%v", fig.bounds, fig.contentH, fig.expandable)
	}

	// Wheel scroll inside compact image advances imageScrollY.
	cx := fig.bounds.X + fig.bounds.W/2
	cy := fig.bounds.Y + fig.bounds.H/2
	tt.Wheel(cx, cy, 0, 120)
	tt.Frame()
	if e.imageScrollY[imgBlock.ID] <= 0 {
		t.Fatalf("expected imageScrollY > 0 after wheel scroll, got %v", e.imageScrollY[imgBlock.ID])
	}

	// Scroll to bottom and verify boundary handoff returns false.
	if !e.handleCompactScrollInput(ui.InputEvent{Kind: ui.InputScroll, X: cx, Y: cy, DY: 2000}) {
		t.Fatal("expected scroll to bottom of compact image to be consumed")
	}
	if e.imageScrollY[imgBlock.ID] != 520-180 {
		t.Fatalf("expected imageScrollY clamped to %v, got %v", 520-180, e.imageScrollY[imgBlock.ID])
	}
	if e.handleCompactScrollInput(ui.InputEvent{Kind: ui.InputScroll, X: cx, Y: cy, DY: 40}) {
		t.Fatal("expected scroll past bottom of compact image to hand off to outer document")
	}

	// Clicking expand button expands image to full 520px height.
	if err := tt.Click("展开完整图片"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if e.IsImageCompact(imgBlock.ID) || e.rows[0].picture.bounds.H != 520 {
		t.Fatalf("expected full 520px image after expand, compact=%v h=%v", e.IsImageCompact(imgBlock.ID), e.rows[0].picture.bounds.H)
	}

	// In ReadOnly mode, clicking collapse button restores 180px compact mode.
	e.SetReadOnly(true)
	tt.Frame()
	if err := tt.Click("收起图片"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !e.IsImageCompact(imgBlock.ID) || e.rows[0].picture.bounds.H != 180 {
		t.Fatalf("expected 180px compact image after collapse, compact=%v h=%v", e.IsImageCompact(imgBlock.ID), e.rows[0].picture.bounds.H)
	}

	// Printing bypasses ImageMaxHeight.
	e.printing = true
	e.ready = false
	e.layout(Options{
		Width:          900,
		Theme:          ui.LightTheme(),
		ImageMaxHeight: 180,
		LoadImage:      func(string) ImageResource { return ImageResource{Bitmap: bmp, Width: 400, Height: 520} },
	})
	if e.IsImageCompact(imgBlock.ID) || e.rows[0].picture.bounds.H <= 180 {
		t.Fatalf("expected printing to show uncropped image: compact=%v h=%v", e.IsImageCompact(imgBlock.ID), e.rows[0].picture.bounds.H)
	}
}

func TestCompactBlocksSnapshot(t *testing.T) {
	dir := os.Getenv("ARCMD_SNAPSHOT")
	if dir == "" {
		t.Skip("set ARCMD_SNAPSHOT to render compact code and image UI")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bmp := makeTallGradientBitmap(420, 900)
	var lines []string
	for i := 1; i <= 24; i++ {
		lines = append(lines, fmt.Sprintf("    log.Printf(\"step %02d: ok\")", i))
	}
	md := "# Compact Blocks\n\n```go\nfunc run() {\n" + strings.Join(lines, "\n") + "\n}\n```\n\n![Tall Shot](tall.png)\n"
	for _, dark := range []bool{false, true} {
		name := "compact-blocks"
		if dark {
			name += "-dark"
		}
		t.Run(name, func(t *testing.T) {
			e := New(md)
			tt := ui.NewTester(func(c *ui.Context) {
				w, _ := c.Size()
				View(c, e, Options{
					Width:          w,
					Theme:          c.Theme(),
					CodeMaxHeight:  200,
					ImageMaxHeight: 200,
					LoadImage:      func(string) ImageResource { return ImageResource{Bitmap: bmp, Width: 420, Height: 900, Local: true} },
				})
			}, 920, 760)
			tt.SetDark(dark)
			for range 4 {
				tt.Frame()
			}
			f, err := os.Create(filepath.Join(dir, name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err := png.Encode(f, tt.Image()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
