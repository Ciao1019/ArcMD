// Package pdf streams vector pages with Unicode text, clickable link
// annotations, and outline bookmarks into PDF files without UI/CGO deps.
package pdf

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	A4Width  = 595.28
	A4Height = 841.89
)

// Writer holds offsets, vector font subsets, and outline bookmarks cleanly.
// A caller may discard each page immediately after FinishVectorPage.
type Writer struct {
	out           io.Writer
	offset        int64
	offsets       []int64
	nextID        int
	pages         []int
	reservedPages []int
	lastHeight    float64

	fm       *fontManager
	outlines []OutlineItem
	alphaGS  map[uint16]int

	err    error
	closed bool
}

func NewWriter(out io.Writer, title string) *Writer {
	w := &Writer{
		out:     out,
		offsets: make([]int64, 16),
		nextID:  4, // 1=Catalog, 2=Pages, 3=Info
		fm:      newFontManager(),
		alphaGS: map[uint16]int{},
	}
	w.write([]byte("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n"))
	w.object(3, fmt.Sprintf("<< /Title %s /Producer (ArcMD) >>", textString(title)), nil)
	return w
}

func (w *Writer) allocID() int {
	id := w.nextID
	w.nextID++
	return id
}

// ReservePages pre-allocates object IDs for count upcoming pages so forward
// intra-document link annotations can reference later pages while streaming.
func (w *Writer) ReservePages(count int) {
	for len(w.reservedPages) < count {
		w.reservedPages = append(w.reservedPages, w.allocID())
	}
}

func (w *Writer) pageObjID(idx int) int {
	if idx < 0 {
		idx = 0
	}
	for len(w.reservedPages) <= idx {
		w.reservedPages = append(w.reservedPages, w.allocID())
	}
	return w.reservedPages[idx]
}

// RegisterFontData registers an embedded TTF/OTF byte slice (such as KaTeX fonts)
// for vector Type 3 glyph outline generation.
func (w *Writer) RegisterFontData(family FontFamily, bold, italic bool, data []byte) {
	w.fm.registerFont(family, bold, italic, data)
}

// SetOutlines sets the document bookmark tree written on Close.
func (w *Writer) SetOutlines(items []OutlineItem) {
	w.outlines = append(w.outlines[:0], items...)
}

func (w *Writer) ensureAlphaGS(a uint16, alphaVal float64) int {
	if id, ok := w.alphaGS[a]; ok {
		return id
	}
	id := w.allocID()
	w.alphaGS[a] = id
	w.object(id, fmt.Sprintf("<< /Type /ExtGState /ca %.3f /CA %.3f >>", alphaVal, alphaVal), nil)
	return id
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

func (w *Writer) Close() error {
	if w.closed {
		return errors.New("pdf: writer is closed")
	}
	w.closed = true
	defer w.fm.close()
	if w.err != nil {
		return w.err
	}
	if len(w.pages) == 0 {
		return errors.New("pdf: no pages")
	}

	// Materialize any over-reserved page IDs so xref never contains an unemitted object.
	for i := len(w.pages); i < len(w.reservedPages); i++ {
		w.object(w.reservedPages[i], "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595.28 841.89] >>", nil)
	}

	w.writeFontObjects()

	var kids strings.Builder
	for _, id := range w.pages {
		fmt.Fprintf(&kids, "%d 0 R ", id)
	}
	w.object(2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), len(w.pages)), nil)

	outlinesID := 0
	if len(w.outlines) > 0 {
		outlinesID = w.allocID()
		w.writeOutlines(outlinesID)
	}
	if outlinesID > 0 {
		w.object(1, fmt.Sprintf("<< /Type /Catalog /Pages 2 0 R /Outlines %d 0 R /PageMode /UseOutlines >>", outlinesID), nil)
	} else {
		w.object(1, "<< /Type /Catalog /Pages 2 0 R >>", nil)
	}

	// Ensure deterministic sorted xref table covering 1..nextID-1.
	maxID := w.nextID - 1
	for len(w.offsets) <= maxID {
		w.offsets = append(w.offsets, 0)
	}
	xref := w.offset
	w.write(fmt.Appendf(nil, "xref\n0 %d\n0000000000 65535 f \n", maxID+1))
	for _, offset := range w.offsets[1 : maxID+1] {
		w.write(fmt.Appendf(nil, "%010d 00000 n \n", offset))
	}
	w.write(fmt.Appendf(nil, "trailer\n<< /Size %d /Root 1 0 R /Info 3 0 R >>\nstartxref\n%d\n%%%%EOF\n", maxID+1, xref))
	return w.err
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
