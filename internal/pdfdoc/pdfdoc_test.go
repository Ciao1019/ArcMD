package pdfdoc

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/pdf"
)

func buildSamplePDF(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := pdf.NewWriter(&buf, "ArcMD PDF Test")

	// Page 0: A4 portrait with red top band, blue bottom band, and Unicode text.
	p0 := image.NewRGBA(image.Rect(0, 0, 200, 280))
	draw.Draw(p0, p0.Bounds(), &image.Uniform{C: color.RGBA{255, 255, 255, 255}}, image.Point{}, draw.Src)
	draw.Draw(p0, image.Rect(0, 0, 200, 40), &image.Uniform{C: color.RGBA{220, 35, 35, 255}}, image.Point{}, draw.Src)
	draw.Draw(p0, image.Rect(0, 240, 200, 280), &image.Uniform{C: color.RGBA{25, 70, 220, 255}}, image.Point{}, draw.Src)
	if err := w.AddPage(pdf.Page{
		Image:  p0,
		Width:  pdf.A4Width,
		Height: pdf.A4Height,
		Text: []pdf.Text{
			{Value: "架构总览 Architecture", X: 60, Y: 90, Width: 220, Height: 18},
			{Value: "平滑滚动与分页预览", X: 60, Y: 130, Width: 180, Height: 14},
		},
	}); err != nil {
		t.Fatalf("AddPage 0: %v", err)
	}

	// Page 1: Wide slide 800x450.
	p1 := image.NewRGBA(image.Rect(0, 0, 320, 180))
	draw.Draw(p1, p1.Bounds(), &image.Uniform{C: color.RGBA{245, 250, 245, 255}}, image.Point{}, draw.Src)
	if err := w.AddPage(pdf.Page{
		Image:  p1,
		Width:  800,
		Height: 450,
		Text: []pdf.Text{
			{Value: "性能基准 Benchmark", X: 80, Y: 100, Width: 200, Height: 18},
		},
	}); err != nil {
		t.Fatalf("AddPage 1: %v", err)
	}

	w.SetOutlines([]pdf.OutlineItem{
		{Title: "第一章 架构总览", Page: 0, Y: 80, Level: 1},
		{Title: "1.1 分页预览", Page: 0, Y: 120, Level: 2},
		{Title: "第二章 性能基准", Page: 1, Y: 90, Level: 1},
	})
	if err := w.Close(); err != nil {
		t.Fatalf("Writer.Close: %v", err)
	}
	return buf.Bytes()
}

func TestOpenRenderExtractAndSearch(t *testing.T) {
	data := buildSamplePDF(t)
	pdfPath := filepath.Join(t.TempDir(), "sample.pdf")
	if err := os.WriteFile(pdfPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Verify pure-Go structural parser directly as well.
	raw, err := parseRawPDF(data)
	if err != nil {
		t.Fatalf("parseRawPDF: %v", err)
	}
	if len(raw.pageInfos) != 2 {
		t.Fatalf("expected 2 pages in rawPDF, got %d", len(raw.pageInfos))
	}
	if rawTxt := raw.extractPageText(0); !strings.Contains(rawTxt, "架构总览") || !strings.Contains(rawTxt, "平滑滚动") {
		t.Fatalf("pure-Go extractPageText missed CJK text, got %q", rawTxt)
	}
	if len(raw.outlines) != 2 || raw.outlines[0].Title != "第一章 架构总览" || len(raw.outlines[0].Children) != 1 {
		t.Fatalf("unexpected raw outlines: %+v", raw.outlines)
	}

	// Open via disk path.
	doc, err := Open(pdfPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer doc.Close()

	if doc.PageCount() != 2 {
		t.Fatalf("expected 2 pages, got %d", doc.PageCount())
	}
	info0 := doc.PageInfo(0)
	if math.Abs(info0.Width-pdf.A4Width) > 1 || math.Abs(info0.Height-pdf.A4Height) > 1 {
		t.Fatalf("unexpected page 0 size: %+v", info0)
	}
	info1 := doc.PageInfo(1)
	if math.Abs(info1.Width-800) > 1 || math.Abs(info1.Height-450) > 1 {
		t.Fatalf("unexpected page 1 size: %+v", info1)
	}

	// Render page 0 upright and verify top band is red and bottom band is blue.
	img, err := doc.RenderPage(0, 400, 0)
	if err != nil {
		t.Fatalf("RenderPage(0): %v", err)
	}
	if img.Bounds().Dx() != 400 {
		t.Fatalf("expected rendered width 400, got %v", img.Bounds())
	}
	topPx := img.RGBAAt(200, 16)
	if topPx.R < 170 || topPx.G > 90 || topPx.B > 90 {
		t.Fatalf("expected red top banner (upright orientation), got top pixel %+v", topPx)
	}
	botPx := img.RGBAAt(200, img.Bounds().Dy()-16)
	if botPx.B < 170 || botPx.R > 90 {
		t.Fatalf("expected blue bottom banner, got bottom pixel %+v", botPx)
	}

	// Render page 0 rotated 90 degrees clockwise: top red band moves to right edge.
	rotImg, err := doc.RenderPage(0, 400, 90)
	if err != nil {
		t.Fatalf("RenderPage(0, rot=90): %v", err)
	}
	rightPx := rotImg.RGBAAt(rotImg.Bounds().Dx()-12, rotImg.Bounds().Dy()/2)
	if rightPx.R < 170 || rightPx.B > 90 {
		t.Fatalf("expected rotated red band on right edge, got %+v", rightPx)
	}

	// Outlines.
	outlines := doc.Outlines()
	if len(outlines) != 2 || outlines[1].Page != 1 {
		t.Fatalf("unexpected outlines: %+v", outlines)
	}

	// Text extraction & Find.
	fullTxt := doc.PlainText(0)
	if !strings.Contains(fullTxt, "Architecture") || !strings.Contains(fullTxt, "Benchmark") {
		t.Fatalf("PlainText missing expected words: %q", fullTxt)
	}
	matches := doc.Find("benchmark")
	if len(matches) == 0 || matches[0].Page != 1 {
		t.Fatalf("expected Find('benchmark') on page 1, got %+v", matches)
	}
	m0 := doc.Find("Architecture")
	if len(m0) == 0 || m0[0].Page != 0 {
		t.Fatalf("expected Find('Architecture') on page 0, got %+v", m0)
	}
	if len(m0[0].Rects) > 0 {
		r := m0[0].Rects[0]
		if r.Y < 0 || r.Y > 0.35 || r.W <= 0 || r.H <= 0 {
			t.Fatalf("expected highlight rect near top of page 0, got %+v", r)
		}
	}

	// OpenBytes parity.
	memDoc, err := OpenBytes(data)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	defer memDoc.Close()
	if memDoc.PageCount() != 2 {
		t.Fatalf("OpenBytes PageCount = %d", memDoc.PageCount())
	}
}

func TestInvalidAndEncryptedPDF(t *testing.T) {
	if _, err := OpenBytes([]byte("not a pdf at all")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got %v", err)
	}

	encPDF := "%PDF-1.4\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
		"4 0 obj\n<< /Filter /Standard /V 2 /R 3 /O <00112233445566778899AABBCCDDEEFF00112233445566778899AABBCCDDEEFF> /U <FFEEDDCCBBAA9988776655443322110000000000000000000000000000000000> /P -4 >>\nendobj\n" +
		"xref\n0 5\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n0000000115 00000 n \n0000000184 00000 n \n" +
		"trailer\n<< /Size 5 /Root 1 0 R /Encrypt 4 0 R >>\nstartxref\n365\n%%EOF\n"
	if _, err := parseRawPDF([]byte(encPDF)); !errors.Is(err, ErrEncrypted) {
		t.Fatalf("expected ErrEncrypted from pure-Go parser, got %v", err)
	}
}

func TestConcurrentRenderSearchAndClose(t *testing.T) {
	data := buildSamplePDF(t)
	pdfPath := filepath.Join(t.TempDir(), "concurrent.pdf")
	if err := os.WriteFile(pdfPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := Open(pdfPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for step := range 6 {
				_, _ = doc.RenderPage(step%doc.PageCount(), 320+(worker%3)*128, (step%2)*90)
				_ = doc.PageText(step % doc.PageCount())
				_ = doc.Find("Architecture")
			}
		}(g)
	}
	// Concurrently close partway through worker execution to verify zero use-after-free or pool mismatch.
	doc.Close()
	wg.Wait()
}
