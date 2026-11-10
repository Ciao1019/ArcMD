// Package pdf streams raster pages with a Unicode text layer into PDF files.
// It has no dependency on the window or drawing toolkit.
package pdf

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"strings"
)

// Text is an invisible selectable glyph or cluster, in points from the page's
// top-left corner. The raster layer remains the source of visual fidelity.
type Text struct {
	Value               string
	X, Y, Width, Height float64
}

type Page struct {
	Image         image.Image
	Width, Height float64
	Text          []Text
}

const (
	A4Width  = 595.28
	A4Height = 841.89
)

// Writer holds offsets and the Unicode alphabet, never earlier page bitmaps.
// A caller may discard each Page immediately after AddPage returns.
type Writer struct {
	out      io.Writer
	offset   int64
	offsets  []int64
	pages    []int
	glyphs   map[string]int
	alphabet []string
	err      error
	closed   bool
}

func NewWriter(out io.Writer, title string) *Writer {
	w := &Writer{out: out, offsets: make([]int64, 8), glyphs: map[string]int{}}
	w.write([]byte("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n"))
	w.object(1, "<< /Type /Catalog /Pages 2 0 R >>", nil)
	w.object(3, fmt.Sprintf("<< /Title %s /Producer (ArcMD) >>", textString(title)), nil)
	w.object(4, "<< /Type /Font /Subtype /Type0 /BaseFont /ArcMDText /Encoding /Identity-H /DescendantFonts [5 0 R] /ToUnicode 7 0 R >>", nil)
	w.object(5, "<< /Type /Font /Subtype /CIDFontType2 /BaseFont /ArcMDText /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 6 0 R /DW 1000 /CIDToGIDMap /Identity >>", nil)
	w.object(6, "<< /Type /FontDescriptor /FontName /ArcMDText /Flags 4 /FontBBox [0 -200 1000 800] /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 800 /StemV 80 >>", nil)
	return w
}

func (w *Writer) write(data []byte) {
	if w.err != nil {
		return
	}
	n, err := w.out.Write(data)
	w.offset += int64(n)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	w.err = err
}

func (w *Writer) object(id int, body string, stream []byte) {
	for len(w.offsets) <= id {
		w.offsets = append(w.offsets, 0)
	}
	w.offsets[id] = w.offset
	w.write(fmt.Appendf(nil, "%d 0 obj\n%s", id, body))
	if stream != nil {
		w.write([]byte("\nstream\n"))
		w.write(stream)
		w.write([]byte("\nendstream"))
	}
	w.write([]byte("\nendobj\n"))
}

func (w *Writer) AddPage(p Page) error {
	if w.closed {
		return errors.New("pdf: writer is closed")
	}
	if w.err != nil {
		return w.err
	}
	if p.Image == nil || p.Width <= 0 || p.Height <= 0 {
		return errors.New("pdf: invalid page")
	}
	var img bytes.Buffer
	if err := jpeg.Encode(&img, p.Image, &jpeg.Options{Quality: 90}); err != nil {
		return err
	}
	page := 8 + 3*len(w.pages)
	content, picture := page+1, page+2
	w.pages = append(w.pages, page)
	w.object(page, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /Font << /F1 4 0 R >> /XObject << /Im %d 0 R >> >> /Contents %d 0 R >>", p.Width, p.Height, picture, content), nil)
	var draw strings.Builder
	fmt.Fprintf(&draw, "q %.4f 0 0 %.4f 0 0 cm /Im Do Q\n", p.Width, p.Height)
	for _, text := range p.Text {
		if text.Value == "" || text.Width <= 0 || text.Height <= 0 {
			continue
		}
		glyph := w.glyphs[text.Value]
		if glyph == 0 {
			if len(w.alphabet) >= 65535 {
				return errors.New("pdf: too many distinct text clusters")
			}
			w.alphabet = append(w.alphabet, text.Value)
			glyph = len(w.alphabet)
			w.glyphs[text.Value] = glyph
		}
		fmt.Fprintf(&draw, "BT /F1 %.4f Tf 3 Tr %.4f Tz 1 0 0 1 %.4f %.4f Tm <%04X> Tj ET\n", text.Height, 100*text.Width/text.Height, text.X, p.Height-text.Y-.8*text.Height, glyph)
	}
	w.object(content, fmt.Sprintf("<< /Length %d >>", draw.Len()), []byte(draw.String()))
	b := p.Image.Bounds()
	w.object(picture, fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>", b.Dx(), b.Dy(), img.Len()), img.Bytes())
	return w.err
}

func (w *Writer) Close() error {
	if w.closed {
		return errors.New("pdf: writer is closed")
	}
	w.closed = true
	if w.err != nil {
		return w.err
	}
	if len(w.pages) == 0 {
		return errors.New("pdf: no pages")
	}
	var kids strings.Builder
	for _, id := range w.pages {
		fmt.Fprintf(&kids, "%d 0 R ", id)
	}
	w.object(2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), len(w.pages)), nil)
	var cmap strings.Builder
	cmap.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n/CMapName /ArcMDUnicode def\n/CMapType 2 def\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
	for start := 0; start < len(w.alphabet); start += 100 {
		end := min(start+100, len(w.alphabet))
		fmt.Fprintf(&cmap, "%d beginbfchar\n", end-start)
		for i := start; i < end; i++ {
			fmt.Fprintf(&cmap, "<%04X> <%s\n", i+1, strings.TrimPrefix(textString(w.alphabet[i]), "<FEFF"))
		}
		cmap.WriteString("endbfchar\n")
	}
	cmap.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
	w.object(7, fmt.Sprintf("<< /Length %d >>", cmap.Len()), []byte(cmap.String()))
	xref := w.offset
	w.write(fmt.Appendf(nil, "xref\n0 %d\n0000000000 65535 f \n", len(w.offsets)))
	for _, offset := range w.offsets[1:] {
		w.write(fmt.Appendf(nil, "%010d 00000 n \n", offset))
	}
	w.write(fmt.Appendf(nil, "trailer\n<< /Size %d /Root 1 0 R /Info 3 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(w.offsets), xref))
	return w.err
}

func Write(out io.Writer, title string, pages []Page) error {
	w := NewWriter(out, title)
	for _, p := range pages {
		if err := w.AddPage(p); err != nil {
			return err
		}
	}
	return w.Close()
}

func textString(s string) string {
	var b strings.Builder
	b.WriteString("<FEFF")
	for _, r := range s {
		if r > 0xffff {
			r -= 0x10000
			fmt.Fprintf(&b, "%04X%04X", 0xd800+(r>>10), 0xdc00+(r&0x3ff))
		} else {
			fmt.Fprintf(&b, "%04X", r)
		}
	}
	b.WriteString(">")
	return b.String()
}
