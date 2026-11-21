package editor

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/Ciao1019/ArcMD/internal/pdfdoc"
	"github.com/egoist/mygo/ui"
)

func buildTestPDF(t testing.TB) (string, *pdfdoc.Document) {
	t.Helper()
	var buf bytes.Buffer
	w := pdf.NewWriter(&buf, "ArcMD PDF Spec")

	pages := []struct {
		w, h  float64
		band  color.RGBA
		title string
		body  string
	}{
		{pdf.A4Width, pdf.A4Height, color.RGBA{38, 92, 224, 255}, "第一章 架构设计 Architecture", "统一虚拟滚动与双页阅读体验"},
		{800, 450, color.RGBA{34, 156, 98, 255}, "第二章 性能基准 Benchmark", "无阻塞异步位图缓存与多级分辨率"},
		{pdf.A4Width, pdf.A4Height, color.RGBA{222, 112, 36, 255}, "第三章 部署方案 Deployment", "支持 Markdown 内联翻页与分屏联动"},
	}

	for _, pg := range pages {
		img := image.NewRGBA(image.Rect(0, 0, 240, 180))
		draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{255, 255, 255, 255}}, image.Point{}, draw.Src)
		draw.Draw(img, image.Rect(0, 0, 240, 36), &image.Uniform{C: pg.band}, image.Point{}, draw.Src)
		if err := w.AddPage(pdf.Page{
			Image:  img,
			Width:  pg.w,
			Height: pg.h,
			Text: []pdf.Text{
				{Value: pg.title, X: 56, Y: 88, Width: 260, Height: 18},
				{Value: pg.body, X: 56, Y: 124, Width: 260, Height: 14},
			},
		}); err != nil {
			t.Fatalf("AddPage: %v", err)
		}
	}
	w.SetOutlines([]pdf.OutlineItem{
		{Title: "第一章 架构设计", Page: 0, Y: 80, Level: 1},
		{Title: "第二章 性能基准", Page: 1, Y: 80, Level: 1},
		{Title: "第三章 部署方案", Page: 2, Y: 80, Level: 1},
	})
	if err := w.Close(); err != nil {
		t.Fatalf("Writer.Close: %v", err)
	}

	path := filepath.Join(t.TempDir(), "spec.pdf")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := pdfdoc.Open(path)
	if err != nil {
		t.Fatalf("pdfdoc.Open: %v", err)
	}
	t.Cleanup(func() { doc.Close() })
	return path, doc
}

func TestPDFURLHelpersAndMarkdownRoundTrip(t *testing.T) {
	if !IsPDFURL("assets/report%202026.pdf#page=4") || !IsPDFURL(`docs\slides.PDF?v=1`) {
		t.Fatal("IsPDFURL rejected valid PDF URLs")
	}
	if IsPDFURL("assets/photo.png") || IsPDFURL(".pdf") {
		t.Fatal("IsPDFURL accepted non-PDF URLs")
	}

	clean, p := ParsePDFURLPage("assets/spec.pdf#page=3")
	if clean != "assets/spec.pdf" || p != 3 {
		t.Fatalf("ParsePDFURLPage(#page=3) = %q, %d", clean, p)
	}
	clean2, p2 := ParsePDFURLPage("assets/spec.pdf#5")
	if clean2 != "assets/spec.pdf" || p2 != 5 {
		t.Fatalf("ParsePDFURLPage(#5) = %q, %d", clean2, p2)
	}
	if got := WithPDFURLPage("assets/spec.pdf#page=3", 1); got != "assets/spec.pdf" {
		t.Fatalf("WithPDFURLPage(..., 1) should strip fragment, got %q", got)
	}
	if got := WithPDFURLPage("assets/spec.pdf", 4); got != "assets/spec.pdf#page=4" {
		t.Fatalf("WithPDFURLPage(..., 4) = %q", got)
	}

	// Standard Markdown + Obsidian PDF embed import/export preservation.
	src := "# 论文笔记\n\n![系统规范](assets/spec.pdf#page=2)\n\n![[slides.pdf#page=3|520]]\n"
	doc := ImportMarkdown(src)
	if len(doc.Blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(doc.Blocks))
	}
	b1, b2 := doc.Blocks[1], doc.Blocks[2]
	if !isPDFEmbed(b1) || b1.URL != "assets/spec.pdf#page=2" {
		t.Fatalf("unexpected block 1: %+v", b1)
	}
	if !isPDFEmbed(b2) || b2.URL != "slides.pdf#page=3" || b2.ImageWidth != 520 {
		t.Fatalf("unexpected Obsidian PDF block 2: %+v", b2)
	}
	if out := ExportMarkdown(doc); out != src {
		t.Fatalf("unmodified ExportMarkdown changed source:\ngot:  %q\nwant: %q", out, src)
	}
}

func TestStandalonePDFViewNavigationOutlineAndSearch(t *testing.T) {
	_, pdfDoc := buildTestPDF(t)
	v := NewPDFViewer(pdfDoc, nil)
	defer v.Close()

	tt := ui.NewTester(func(c *ui.Context) {
		PDFView(c, v, PDFOptions{
			Theme: c.Theme(),
			Title: "spec.pdf",
		})
	}, 1040, 720)
	tt.SetLocale("zh-Hans")
	v.RequestFocus = true
	tt.Frame()

	if v.CurrentPage != 0 || !tt.HasText("1 / 3") {
		t.Fatalf("expected initial page 1 / 3, got page=%d texts=%v", v.CurrentPage, tt.Texts())
	}

	// Step forward with keyboard Right arrow and settle smooth scroll.
	tt.Key(0, ui.KeyRight)
	tt.Advance(350 * time.Millisecond)
	tt.Frame()
	if v.CurrentPage != 1 || !tt.HasText("2 / 3") {
		t.Fatalf("expected page 2 after KeyRight, got %d", v.CurrentPage)
	}

	// Open Outline/Thumbnail rail with ⇧⌘O and jump to chapter 3 via outline item.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyO)
	tt.Frame()
	if !v.SidebarOpen || !tt.HasText("第三章 部署方案") {
		t.Fatalf("expected outline sidebar open with chapter titles, open=%v texts=%v", v.SidebarOpen, tt.Texts())
	}
	if err := tt.Click("第三章 部署方案"); err != nil {
		t.Fatalf("Click outline item: %v", err)
	}
	tt.Advance(350 * time.Millisecond)
	tt.Frame()
	if v.CurrentPage != 2 || !tt.HasText("3 / 3") {
		t.Fatalf("expected outline click to jump to page 3, got %d", v.CurrentPage)
	}

	// Search inside PDF (⌘F -> FindText) jumps directly to matching page.
	v.FindText("Benchmark")
	tt.Advance(350 * time.Millisecond)
	tt.Frame()
	if len(v.Matches) == 0 || v.CurrentPage != 1 {
		t.Fatalf("expected FindText('Benchmark') to match page 2 (idx 1), matches=%+v page=%d", v.Matches, v.CurrentPage)
	}

	// Zoom in and reset zoom.
	tt.Key(ui.Cmd, ui.KeyEqual)
	tt.Frame()
	if v.FitMode != PDFFreeZoom || v.Zoom <= 1.0 {
		t.Fatalf("expected Cmd+= to zoom in, mode=%v zoom=%v", v.FitMode, v.Zoom)
	}
	tt.Key(ui.Cmd, ui.Key0)
	tt.Frame()
	if v.FitMode != PDFFitWidth || v.Zoom != 1.0 {
		t.Fatalf("expected Cmd+0 to reset fit-width, mode=%v zoom=%v", v.FitMode, v.Zoom)
	}
}

func TestInlinePDFBlockStepperPinModalAndExport(t *testing.T) {
	_, pdfDoc := buildTestPDF(t)
	doc := ImportMarkdown("# 架构说明\n\n![系统规范](assets/spec.pdf)\n\n正文继续保持顺滑滚动。\n")
	e := FromDocument(doc)
	blockID := e.Doc.Blocks[1].ID

	var splitTarget string
	tt := ui.NewTester(func(c *ui.Context) {
		View(c, e, Options{
			Theme: c.Theme(),
			Width: 880,
			LoadPDF: func(src string) (*pdfdoc.Document, uint64, error) {
				if src == "assets/spec.pdf" {
					return pdfDoc, 1, nil
				}
				return nil, 0, os.ErrNotExist
			},
			OpenPDF: func(target string, split bool) {
				if split {
					splitTarget = target
				}
			},
		})
	}, 960, 760)
	tt.SetLocale("zh-Hans")
	tt.Frame()

	if !tt.HasText("第 1 / 3 页") {
		t.Fatalf("expected rest-mode PDF card stepper '第 1 / 3 页', got %v", tt.Texts())
	}
	revBefore := e.Revision()

	// Flip to page 2 directly on the rest-mode card without modifying the Markdown document.
	e.stepRestPDFPage(e.Doc.Blocks[1], pdfDoc, 1)
	tt.Frame()
	if !tt.HasText("第 2 / 3 页") {
		t.Fatalf("expected rest-mode PDF card to flip to '第 2 / 3 页', got %v", tt.Texts())
	}
	if e.Revision() != revBefore {
		t.Fatal("flipping preview pages in rest mode should not mutate the document")
	}

	// Activate inline interactive browsing and pin page 2 into the Markdown URL (undoable).
	e.BrowsePDFBlock(blockID)
	tt.Frame()
	if e.pdfBrowsing != blockID {
		t.Fatal("expected inline PDF browsing to be active")
	}
	e.pinPDFBlockPage(blockID, 2)
	tt.Frame()
	if got := e.Doc.Blocks[1].URL; got != "assets/spec.pdf#page=2" {
		t.Fatalf("expected pinned URL 'assets/spec.pdf#page=2', got %q", got)
	}
	if md := ExportMarkdown(e.Doc); !strings.Contains(md, "![系统规范](assets/spec.pdf#page=2)") {
		t.Fatalf("expected ExportMarkdown to persist #page=2, got %q", md)
	}
	e.Undo()
	tt.Frame()
	if got := e.Doc.Blocks[1].URL; got != "assets/spec.pdf" {
		t.Fatalf("expected Undo to restore 'assets/spec.pdf', got %q", got)
	}

	// Open full-window modal reader, split-read from modal, and verify Esc closes modal.
	e.OpenPDFModal(blockID)
	tt.Frame()
	if e.pdfModalID != blockID || !tt.HasText("完成") {
		t.Fatalf("expected full-window PDF modal open, modalID=%q texts=%v", e.pdfModalID, tt.Texts())
	}
	if err := tt.Click("在分屏中阅读"); err != nil {
		t.Fatalf("Click split button in modal: %v", err)
	}
	tt.Frame()
	if splitTarget != "assets/spec.pdf#page=2" || e.pdfModalID != "" {
		t.Fatalf("expected modal split button to pass current page 'assets/spec.pdf#page=2' and close modal, got target=%q modal=%q", splitTarget, e.pdfModalID)
	}

	// HTML & Vector PDF exports include the embedded PDF cleanly.
	htmlOut := ExportHTML(e.Doc)
	if !strings.Contains(htmlOut, "assets/spec.pdf") {
		t.Fatalf("ExportHTML missing PDF link: %q", htmlOut)
	}
	th := ui.LightTheme()
	opts := Options{
		Theme: th,
		Width: 680,
		LoadPDF: func(src string) (*pdfdoc.Document, uint64, error) {
			return pdfDoc, 1, nil
		},
	}
	var tops []float32
	ui.Render(func(c *ui.Context) {
		tops = e.Paginate(opts, 848)
	}, 1, 1, 1)
	if len(tops) == 0 {
		t.Fatal("Paginate returned no pages")
	}
	var pdfBuf bytes.Buffer
	vw := pdf.NewWriter(&pdfBuf, "Exported Note")
	vw.ReservePages(len(tops))
	vp := vw.NewVectorPage(pdf.A4Width, pdf.A4Height)
	e.ExportVectorPage(vp, opts, tops[0], tops[0]+848, 0, len(tops), tops, 680, 920, 36)
	if err := vw.FinishVectorPage(vp); err != nil {
		t.Fatalf("FinishVectorPage: %v", err)
	}
	if err := vw.Close(); err != nil {
		t.Fatalf("Close exported PDF: %v", err)
	}
	if pdfBuf.Len() < 512 {
		t.Fatalf("exported vector PDF suspiciously small: %d bytes", pdfBuf.Len())
	}
}
