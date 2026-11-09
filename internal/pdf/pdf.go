// Package pdf writes pages drawn as pictures into a PDF document, the
// simplest PDF there is: a page each, its picture filling it as a JPEG. It
// is how ArcMD exports and prints documents, which it draws itself. The
// package has no dependency on the window.
package pdf

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"io"
)

// Page is a page: its picture and its size in points (1/72 inch).
type Page struct {
	Image         image.Image
	Width, Height float64
}

// A4 is the size of an A4 sheet in points.
const (
	A4Width  = 595.28
	A4Height = 841.89
)

// Write writes the pages as a PDF document titled title.
func Write(w io.Writer, title string, pages []Page) error {
	if len(pages) == 0 {
		return fmt.Errorf("pdf: no pages")
	}
	var out bytes.Buffer
	var offsets []int
	obj := func(body string, stream []byte) {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s", len(offsets), body)
		if stream != nil {
			out.WriteString("\nstream\n")
			out.Write(stream)
			out.WriteString("\nendstream")
		}
		out.WriteString("\nendobj\n")
	}
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	// 1 catalog, 2 pages, 3 info; then per page: page, content, image.
	n := len(pages)
	kids := ""
	for i := range n {
		kids += fmt.Sprintf("%d 0 R ", 4+3*i)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>", nil)
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids, n), nil)
	obj(fmt.Sprintf("<< /Title %s /Producer (ArcMD) >>", textString(title)), nil)
	for i, p := range pages {
		var img bytes.Buffer
		if err := jpeg.Encode(&img, p.Image, &jpeg.Options{Quality: 90}); err != nil {
			return err
		}
		b := p.Image.Bounds()
		page, content, picture := 4+3*i, 5+3*i, 6+3*i
		_ = page
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /XObject << /Im%d %d 0 R >> >> /Contents %d 0 R >>",
			p.Width, p.Height, i, picture, content), nil)
		draw := fmt.Sprintf("q %.2f 0 0 %.2f 0 0 cm /Im%d Do Q", p.Width, p.Height, i)
		obj(fmt.Sprintf("<< /Length %d >>", len(draw)), []byte(draw))
		obj(fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>",
			b.Dx(), b.Dy(), img.Len()), img.Bytes())
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, off := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R /Info 3 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	_, err := w.Write(out.Bytes())
	return err
}

// textString writes s as a PDF text string, in UTF-16 so that any title
// shows.
func textString(s string) string {
	var b bytes.Buffer
	b.WriteString("<FEFF")
	for _, r := range s {
		if r > 0xffff {
			r -= 0x10000
			fmt.Fprintf(&b, "%04X%04X", 0xd800+(r>>10), 0xdc00+(r&0x3ff))
			continue
		}
		fmt.Fprintf(&b, "%04X", r)
	}
	b.WriteString(">")
	return b.String()
}
