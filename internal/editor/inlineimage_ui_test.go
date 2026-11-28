package editor

import (
	"bytes"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/egoist/mygo/ui"
)

func testSolidBitmap(w, h int, col color.RGBA) *ui.Bitmap {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, col)
		}
	}
	return ui.NewBitmap(img)
}

func TestInlineImageLayoutAlignmentAndAsyncInvalidation(t *testing.T) {
	bmp := testSolidBitmap(200, 100, color.RGBA{R: 40, G: 120, B: 220, A: 255})
	svg := ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="120" height="24" viewBox="0 0 120 24"><rect width="120" height="24" rx="4" fill="#2563eb"/></svg>`))

	md := strings.Join([]string{
		`<img src="right.png" width="200" align="right">`,
		`Trailing paragraph.`,
		``,
		`<img src="left.png" width="200" align="left">`,
		``,
		`Before <img src="icon.png" width="20" height="20"> <a href="https://example.com"><img src="badge.svg" alt="CI"></a> after`,
	}, "\n") + "\n"

	loaded := false
	e := New(md)
	opts := func(c *ui.Context) Options {
		return Options{
			Theme:  ui.LightTheme(),
			Width:  800,
			Column: 640,
			LoadImage: func(src string) ImageResource {
				switch src {
				case "right.png", "left.png", "icon.png":
					return ImageResource{Bitmap: bmp, Width: 200, Height: 100, Local: true}
				case "badge.svg":
					if !loaded {
						return ImageResource{Loading: true}
					}
					return ImageResource{SVG: svg, Width: 120, Height: 24, LoadedAt: time.Now(), Local: true}
				default:
					return ImageResource{Problem: ImageMissing, Local: true}
				}
			},
		}
	}

	tt := ui.NewTester(func(c *ui.Context) {
		View(c, e, opts(c))
	}, 800, 600)
	tt.Frame()

	if len(e.rows) != 4 {
		t.Fatalf("expected 4 rows (right img, para, left img, inline para), got %d", len(e.rows))
	}
	// Block 0: right-aligned image sits flush against right edge of column.
	rRight := e.rows[0]
	if rRight.picture == nil {
		t.Fatal("right image has no picture figure")
	}
	rightEdge := rRight.rect.X + rRight.rect.W
	if diff := rightEdge - (rRight.picture.bounds.X + rRight.picture.bounds.W); diff < -1 || diff > 1 {
		t.Fatalf("right-aligned image bounds=%+v rect=%+v", rRight.picture.bounds, rRight.rect)
	}
	// Block 2: left-aligned image sits flush against left edge of column.
	rLeft := e.rows[2]
	if rLeft.picture == nil {
		t.Fatal("left image has no picture figure")
	}
	if diff := rLeft.picture.bounds.X - rLeft.rect.X; diff < -1 || diff > 1 {
		t.Fatalf("left-aligned image bounds=%+v rect=%+v", rLeft.picture.bounds, rLeft.rect)
	}

	// Block 3: two inline images on one line; badge.svg starts loading then updates via InvalidateImage.
	rInline := e.rows[3]
	if rInline.layout == nil || len(rInline.layout.objects) != 2 {
		t.Fatalf("expected 2 inline image objects, got %+v", rInline.layout)
	}
	objIcon := rInline.layout.objects[0]
	objBadgeLoading := rInline.layout.objects[1]
	if !objIcon.hasImage || objIcon.rect.W < 19 || objIcon.rect.W > 21 || objIcon.imgH < 19 || objIcon.imgH > 21 {
		t.Fatalf("inline icon geometry unexpected: rect=%+v imgH=%v", objIcon.rect, objIcon.imgH)
	}
	if !objBadgeLoading.imageRes.Loading {
		t.Fatalf("expected badge.svg initially loading, got %+v", objBadgeLoading.imageRes)
	}

	// Complete async load and invalidate -> paragraph shape cache must re-shape to true 120x24 SVG bounds.
	loaded = true
	e.InvalidateImage("badge.svg")
	tt.Frame()

	objBadge := e.rows[3].layout.objects[1]
	if objBadge.imageRes.SVG == nil || objBadge.rect.W < 119 || objBadge.rect.W > 121 || objBadge.imgH < 23 || objBadge.imgH > 25 {
		t.Fatalf("after InvalidateImage badge.svg did not re-shape to 120x24: res=%+v rect=%+v imgH=%v", objBadge.imageRes, objBadge.rect, objBadge.imgH)
	}

	// Vector PDF export succeeds with both bitmap and SVG inline/block images.
	var pdfBuf bytes.Buffer
	pw := pdf.NewWriter(&pdfBuf, "Inline Images")
	vp := pw.NewVectorPage(pdf.A4Width, pdf.A4Height)
	e.ExportVectorPage(vp, Options{
		Theme: ui.LightTheme(),
		Width: 800,
		LoadImage: func(src string) ImageResource {
			if src == "badge.svg" {
				return ImageResource{SVG: svg, Width: 120, Height: 24, Local: true}
			}
			return ImageResource{Bitmap: bmp, Width: 200, Height: 100, Local: true}
		},
	}, 0, 800, 0, 1, []float32{0}, 800, 1100, 40)
	if err := pw.FinishVectorPage(vp); err != nil {
		t.Fatalf("FinishVectorPage failed: %v", err)
	}
	if err := pw.Close(); err != nil {
		t.Fatalf("PDF close failed: %v", err)
	}
	pdfBytes := pdfBuf.Bytes()
	if !bytes.Contains(pdfBytes, []byte("https://example.com")) || !bytes.Contains(pdfBytes, []byte("/Subtype /Image")) {
		t.Fatalf("PDF missing linked inline image annotation or embedded image")
	}
}

func TestLinkedInlineImageClickBehaviorAndPopoverPreservation(t *testing.T) {
	svg := ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="140" height="32" viewBox="0 0 140 32"><rect width="140" height="32" rx="6" fill="#16a34a"/></svg>`))
	md := "[![Download](dl.svg)](https://example.com/download)\n"

	// 1. Read-only mode (both with DirectLinks=true for .mdz and DirectLinks=false for locked .md):
	// plain click on a linked inline image opens the link directly.
	for _, direct := range []bool{true, false} {
		var opened []string
		ro := New(md)
		ro.ReadOnly = true
		roTT := ui.NewTester(func(c *ui.Context) {
			View(c, ro, Options{
				Theme:       ui.LightTheme(),
				Width:       800,
				DirectLinks: direct,
				LoadImage:   func(string) ImageResource { return ImageResource{SVG: svg, Width: 140, Height: 32} },
				OpenLink:    func(u string) { opened = append(opened, u) },
			})
		}, 800, 400)
		roTT.Frame()
		docBounds, _ := roTT.Find("文档")
		obj := ro.rows[0].layout.objects[0]
		// Click right half of the linked badge to verify full-rect hit testing.
		cx := docBounds.X + ro.rows[0].x + obj.rect.X + obj.rect.W*0.8
		cy := docBounds.Y + ro.rows[0].y + obj.rect.Y + obj.rect.H*0.5
		roTT.ClickAt(cx, cy)
		if len(opened) != 1 || opened[0] != "https://example.com/download" {
			t.Fatalf("read-only (directLinks=%v) click on linked image did not open URL: %v", direct, opened)
		}
	}

	// 2. Editable mode: plain click opens linkPopover with linkImage=true, and
	// editing URL + alt preserves the inline image instead of overwriting "\ufffc".
	var opened []string
	ed := New(md)
	edTT := ui.NewTester(func(c *ui.Context) {
		View(c, ed, Options{
			Theme:     ui.LightTheme(),
			Width:     800,
			LoadImage: func(string) ImageResource { return ImageResource{SVG: svg, Width: 140, Height: 32} },
			OpenLink:  func(u string) { opened = append(opened, u) },
		})
	}, 800, 400)
	edTT.Frame()
	docBounds, _ := edTT.Find("文档")
	obj := ed.rows[0].layout.objects[0]
	cx := docBounds.X + ed.rows[0].x + obj.rect.X + obj.rect.W*0.5
	cy := docBounds.Y + ed.rows[0].y + obj.rect.Y + obj.rect.H*0.5

	edTT.ClickAt(cx, cy)
	edTT.Frame()
	if !ed.linkOpen || !ed.linkImage || ed.linkText != "Download" || ed.linkURL != "https://example.com/download" {
		t.Fatalf("plain click did not open image link popover cleanly: open=%v img=%v text=%q url=%q", ed.linkOpen, ed.linkImage, ed.linkText, ed.linkURL)
	}
	ed.linkText = "Get ArcMD"
	ed.linkURL = "https://example.com/releases"
	ed.applyLink(false)
	edTT.Frame()
	run := ed.Doc.Blocks[0].Runs[0]
	if !run.IsImage() || run.Image.URL != "dl.svg" || run.Image.Alt != "Get ArcMD" || run.Style.Link != "https://example.com/releases" {
		t.Fatalf("applyLink corrupted inline image run: %+v", run)
	}
	// Backspace after inline image deletes it atomically; Cmd+Z restores it.
	ed.SetSelection(1, 1)
	edTT.Frame()
	edTT.Key(0, ui.KeyBackspace)
	if len(ed.Doc.Blocks[0].Runs) != 0 && ed.Doc.Blocks[0].Text() != "" {
		t.Fatalf("backspace did not remove inline image: %+v", ed.Doc.Blocks[0].Runs)
	}
	edTT.Key(ui.Cmd, ui.KeyZ)
	if len(ed.Doc.Blocks[0].Runs) != 1 || !ed.Doc.Blocks[0].Runs[0].IsImage() {
		t.Fatalf("undo did not restore inline image: %+v", ed.Doc.Blocks[0].Runs)
	}
}
