// Package pdfdoc opens, inspects, searches, and renders PDF documents to
// standard Go images without cgo or UI dependencies.
//
// Metadata, page boxes, outlines, and fallback text extraction are implemented
// in pure Go so every platform can inspect and index PDFs immediately. Page
// rasterization and exact glyph search geometry use native system engines
// where available (PDFKit + CoreGraphics on macOS, Poppler + Cairo on Linux,
// Windows.Data.Pdf on Windows) and degrade gracefully to a pure-Go preview
// renderer when no system rasterizer is present.
package pdfdoc

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
)

var (
	// ErrInvalid reports that the input is not a readable PDF document.
	ErrInvalid = errors.New("pdfdoc: invalid or damaged PDF document")
	// ErrEncrypted reports that the PDF requires a password to open.
	ErrEncrypted = errors.New("pdfdoc: password-protected PDF is not supported")
)

// PageInfo describes a single 0-indexed PDF page's display geometry in points
// (1 point = 1/72 inch) after applying its CropBox and /Rotate attribute.
type PageInfo struct {
	Width    float64
	Height   float64
	Rotation int
}

// Aspect returns Width / Height, falling back to A4 portrait when degenerate.
func (p PageInfo) Aspect() float64 {
	if p.Width <= 0 || p.Height <= 0 {
		return 595.28 / 841.89
	}
	return p.Width / p.Height
}

// OutlineItem is one entry in a PDF's bookmark tree (/Outlines).
type OutlineItem struct {
	Title    string
	Page     int // 0-based page index, or -1 if external/unresolved
	Children []OutlineItem
}

// NormRect is a normalized rectangle in top-left page coordinates ([0..1] of
// displayed page width and height), independent of zoom or DPI.
type NormRect struct {
	X, Y, W, H float64
}

// Match is one occurrence of a search query on a 0-based page.
// Rects holds normalized highlight boxes when available; Snippet holds a brief
// plain-text excerpt around the match.
type Match struct {
	Page    int
	Snippet string
	Rects   []NormRect
}

// platformDoc is implemented by OS-specific backends.
type platformDoc interface {
	PageCount() int
	PageInfo(page int) (PageInfo, bool)
	RenderPage(page int, widthPx, heightPx int, extraRotate int) (*image.RGBA, error)
	PageText(page int) (string, bool)
	Outlines() ([]OutlineItem, bool)
	Find(query string) ([]Match, bool)
	Close()
}

// Document represents an opened read-only PDF file. All methods are safe for
// concurrent use from background rendering and indexing goroutines.
type Document struct {
	path     string
	parsedMu sync.Mutex
	parsed   *rawPDF
	parseErr error

	plat     platformDoc
	pages    []PageInfo
	outlines []OutlineItem

	textMu    sync.Mutex
	pageTexts map[int]string
	allText   string
	allReady  bool

	closedMu sync.RWMutex
	closed   bool
}

// Open opens a PDF file from disk without reading the whole file into memory
// when the platform backend supports direct file-backed access.
func Open(path string) (*Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	var head [1024]byte
	n, _ := io.ReadFull(f, head[:])
	_ = f.Close()
	if n < 5 || !bytes.Contains(head[:n], []byte("%PDF-")) {
		return nil, ErrInvalid
	}

	doc := &Document{
		path:      path,
		pageTexts: map[int]string{},
	}

	plat, platErr := openPlatformFile(path)
	if errors.Is(platErr, ErrEncrypted) {
		return nil, ErrEncrypted
	}
	if plat != nil && plat.PageCount() > 0 {
		doc.plat = plat
		cnt := plat.PageCount()
		doc.pages = make([]PageInfo, cnt)
		for i := range cnt {
			if info, ok := plat.PageInfo(i); ok && info.Width > 0 && info.Height > 0 {
				doc.pages[i] = info
			} else {
				doc.pages[i] = PageInfo{Width: 595.28, Height: 841.89}
			}
		}
		if items, ok := plat.Outlines(); ok {
			doc.outlines = items
		}
		return doc, nil
	}
	if plat != nil {
		plat.Close()
	}

	// Fall back to pure-Go structural parser.
	raw, err := doc.ensureParsed()
	if err != nil {
		return nil, err
	}
	if len(raw.pages) == 0 {
		return nil, ErrInvalid
	}
	doc.pages = slices.Clone(raw.pageInfos)
	doc.outlines = raw.outlines
	return doc, nil
}

// OpenBytes opens a PDF document from an in-memory byte slice.
func OpenBytes(data []byte) (*Document, error) {
	if len(data) < 5 || !bytes.Contains(data[:min(len(data), 1024)], []byte("%PDF-")) {
		return nil, ErrInvalid
	}
	doc := &Document{
		pageTexts: map[int]string{},
	}
	plat, platErr := openPlatformBytes(data)
	if errors.Is(platErr, ErrEncrypted) {
		return nil, ErrEncrypted
	}
	raw, rawErr := parseRawPDF(data)
	if errors.Is(rawErr, ErrEncrypted) && plat == nil {
		return nil, ErrEncrypted
	}
	if rawErr == nil {
		doc.parsed = raw
	} else {
		doc.parseErr = rawErr
	}

	if plat != nil && plat.PageCount() > 0 {
		doc.plat = plat
		cnt := plat.PageCount()
		doc.pages = make([]PageInfo, cnt)
		for i := range cnt {
			if info, ok := plat.PageInfo(i); ok && info.Width > 0 && info.Height > 0 {
				doc.pages[i] = info
			} else if raw != nil && i < len(raw.pageInfos) {
				doc.pages[i] = raw.pageInfos[i]
			} else {
				doc.pages[i] = PageInfo{Width: 595.28, Height: 841.89}
			}
		}
		if items, ok := plat.Outlines(); ok && len(items) > 0 {
			doc.outlines = items
		} else if raw != nil {
			doc.outlines = raw.outlines
		}
		return doc, nil
	}
	if plat != nil {
		plat.Close()
	}
	if rawErr != nil {
		return nil, rawErr
	}
	if len(raw.pages) == 0 {
		return nil, ErrInvalid
	}
	doc.pages = slices.Clone(raw.pageInfos)
	doc.outlines = raw.outlines
	return doc, nil
}

func (d *Document) ensureParsed() (*rawPDF, error) {
	d.parsedMu.Lock()
	defer d.parsedMu.Unlock()
	if d.parsed != nil || d.parseErr != nil {
		return d.parsed, d.parseErr
	}
	if d.path == "" {
		d.parseErr = ErrInvalid
		return nil, d.parseErr
	}
	data, err := os.ReadFile(d.path)
	if err != nil {
		d.parseErr = err
		return nil, err
	}
	d.parsed, d.parseErr = parseRawPDF(data)
	return d.parsed, d.parseErr
}

// Close releases any native handles held by the document.
func (d *Document) Close() {
	if d == nil {
		return
	}
	d.closedMu.Lock()
	defer d.closedMu.Unlock()
	if d.closed {
		return
	}
	d.closed = true
	plat := d.plat
	d.plat = nil
	if plat != nil {
		plat.Close()
	}
}

// PageCount returns the total number of pages in the PDF.
func (d *Document) PageCount() int {
	if d == nil {
		return 0
	}
	return len(d.pages)
}

// PageInfo returns the geometry of 0-based page index i, rotated by extraRotate
// degrees (0, 90, 180, 270).
func (d *Document) PageInfo(i int) PageInfo {
	return d.PageInfoRotated(i, 0)
}

// PageInfoRotated returns the geometry of 0-based page i with extraRotate
// degrees (multiples of 90) applied.
func (d *Document) PageInfoRotated(i int, extraRotate int) PageInfo {
	if d == nil || i < 0 || i >= len(d.pages) {
		return PageInfo{Width: 595.28, Height: 841.89}
	}
	info := d.pages[i]
	rot := ((extraRotate % 360) + 360) % 360
	if rot == 90 || rot == 270 {
		info.Width, info.Height = info.Height, info.Width
	}
	info.Rotation = (info.Rotation + rot) % 360
	return info
}

// Outlines returns the hierarchical table of contents of the PDF, if any.
func (d *Document) Outlines() []OutlineItem {
	if d == nil {
		return nil
	}
	return d.outlines
}

// RenderPage rasterizes 0-based page i to an RGBA image of targetWidthPx (height
// derived from page aspect ratio), clamped to safe bounds.
func (d *Document) RenderPage(i int, targetWidthPx int, extraRotate int) (*image.RGBA, error) {
	if d == nil || i < 0 || i >= len(d.pages) {
		return nil, ErrInvalid
	}
	rot := ((extraRotate % 360) + 360) % 360
	info := d.PageInfoRotated(i, rot)
	w := min(max(targetWidthPx, 64), 3600)
	h := max(int(math.Round(float64(w)/info.Aspect())), 64)
	if h > 4800 {
		h = 4800
		w = max(int(math.Round(float64(h)*info.Aspect())), 64)
	}

	d.closedMu.RLock()
	if d.closed {
		d.closedMu.RUnlock()
		return nil, errors.New("pdfdoc: document closed")
	}
	var nativeImg *image.RGBA
	var nativeErr error
	if d.plat != nil {
		nativeImg, nativeErr = d.plat.RenderPage(i, w, h, rot)
	}
	d.closedMu.RUnlock()

	if nativeErr == nil && nativeImg != nil {
		return nativeImg, nil
	}
	return d.renderFallbackPage(i, w, h, rot)
}

// PageText extracts plain Unicode text from 0-based page i.
func (d *Document) PageText(i int) string {
	if d == nil || i < 0 || i >= len(d.pages) {
		return ""
	}
	d.textMu.Lock()
	if s, ok := d.pageTexts[i]; ok {
		d.textMu.Unlock()
		return s
	}
	d.textMu.Unlock()

	var text string
	var ok bool
	d.closedMu.RLock()
	if !d.closed && d.plat != nil {
		text, ok = d.plat.PageText(i)
	}
	d.closedMu.RUnlock()

	if !ok {
		if raw, err := d.ensureParsed(); err == nil && raw != nil {
			text = raw.extractPageText(i)
		}
	}
	text = cleanExtractedText(text)

	d.textMu.Lock()
	d.pageTexts[i] = text
	d.textMu.Unlock()
	return text
}

// PlainText extracts plain text across up to maxPages pages (0 = all pages).
func (d *Document) PlainText(maxPages int) string {
	if d == nil {
		return ""
	}
	if maxPages <= 0 || maxPages >= len(d.pages) {
		d.textMu.Lock()
		if d.allReady {
			s := d.allText
			d.textMu.Unlock()
			return s
		}
		d.textMu.Unlock()
	}
	limit := len(d.pages)
	if maxPages > 0 && maxPages < limit {
		limit = maxPages
	}
	var b strings.Builder
	for i := range limit {
		t := strings.TrimSpace(d.PageText(i))
		if t == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(t)
	}
	out := b.String()
	if limit == len(d.pages) {
		d.textMu.Lock()
		d.allText = out
		d.allReady = true
		d.textMu.Unlock()
	}
	return out
}

// Find searches all pages case-insensitively for query and returns ordered matches.
func (d *Document) Find(query string) []Match {
	q := strings.TrimSpace(query)
	if d == nil || q == "" {
		return nil
	}
	d.closedMu.RLock()
	if !d.closed && d.plat != nil {
		if matches, ok := d.plat.Find(q); ok {
			d.closedMu.RUnlock()
			return matches
		}
	}
	d.closedMu.RUnlock()

	// Pure-Go text fallback search across pages.
	lowQ := strings.ToLower(q)
	var out []Match
	for p := range len(d.pages) {
		txt := d.PageText(p)
		if txt == "" {
			continue
		}
		lowTxt := strings.ToLower(txt)
		offset := 0
		for {
			idx := strings.Index(lowTxt[offset:], lowQ)
			if idx < 0 {
				break
			}
			at := offset + idx
			out = append(out, Match{
				Page:    p,
				Snippet: snippetAround(txt, at, len(q)),
			})
			offset = at + max(len(lowQ), 1)
			if len(out) >= 2000 {
				return out
			}
		}
	}
	return out
}

func snippetAround(s string, byteIdx, matchLen int) string {
	start := max(0, byteIdx-36)
	for start > 0 && (s[start]&0xC0) == 0x80 {
		start--
	}
	end := min(len(s), byteIdx+matchLen+48)
	for end < len(s) && (s[end]&0xC0) == 0x80 {
		end++
	}
	sub := strings.Join(strings.Fields(s[start:end]), " ")
	if start > 0 {
		sub = "…" + sub
	}
	if end < len(s) {
		sub += "…"
	}
	return sub
}

func cleanExtractedText(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	out := lines[:0]
	blank := 0
	for _, l := range lines {
		t := strings.TrimRight(l, " \t\f\v")
		if t == "" {
			blank++
			if blank <= 1 && len(out) > 0 {
				out = append(out, "")
			}
			continue
		}
		blank = 0
		out = append(out, t)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// renderFallbackPage draws a clean white page sheet and composites any embedded
// raster images or simple vector rectangles when no native PDF library is present.
func (d *Document) renderFallbackPage(page, w, h, extraRotate int) (*image.RGBA, error) {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.RGBA{255, 255, 255, 255}}, image.Point{}, draw.Src)

	raw, err := d.ensureParsed()
	if err != nil || raw == nil || page < 0 || page >= len(raw.pages) {
		return dst, nil
	}
	// Draw subtle horizontal text-line hints proportional to extracted text lines
	// or embedded JPEG/Flate images so headless environments without Poppler still
	// produce a valid non-blank page image.
	if img := raw.firstPageImage(page); img != nil {
		scaleRect := dst.Bounds()
		drawNearest(dst, scaleRect, img)
		return dst, nil
	}
	txt := d.PageText(page)
	if txt != "" {
		lines := strings.Split(txt, "\n")
		marginX := max(w/10, 12)
		marginY := max(h/12, 14)
		lineStep := max(h/38, 8)
		barH := max(lineStep*2/5, 2)
		y := marginY
		for i, line := range lines {
			if y+barH > h-marginY {
				break
			}
			t := strings.TrimSpace(line)
			if t == "" {
				y += lineStep / 2
				continue
			}
			runes := len([]rune(t))
			availW := max(w-2*marginX, 16)
			barW := min(availW, max(availW/6, runes*max(w/52, 4)))
			shade := uint8(58)
			if i == 0 {
				shade = 32
				barH0 := max(barH+2, 4)
				fillRectRGBA(dst, image.Rect(marginX, y, marginX+min(availW, barW*5/4), y+barH0), color.RGBA{shade, shade, shade + 6, 255})
				y += lineStep + 4
				continue
			}
			fillRectRGBA(dst, image.Rect(marginX, y, marginX+barW, y+barH), color.RGBA{shade, shade, shade + 8, 255})
			y += lineStep
		}
	}
	_ = extraRotate
	return dst, nil
}

func fillRectRGBA(dst *image.RGBA, r image.Rectangle, c color.RGBA) {
	r = r.Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := dst.Pix[y*dst.Stride:]
		for x := r.Min.X; x < r.Max.X; x++ {
			off := x * 4
			row[off], row[off+1], row[off+2], row[off+3] = c.R, c.G, c.B, c.A
		}
	}
}

func drawNearest(dst *image.RGBA, r image.Rectangle, src image.Image) {
	r = r.Intersect(dst.Bounds())
	sb := src.Bounds()
	if r.Empty() || sb.Empty() {
		return
	}
	dw, dh := r.Dx(), r.Dy()
	sw, sh := sb.Dx(), sb.Dy()
	for y := range dh {
		sy := sb.Min.Y + y*sh/dh
		row := dst.Pix[(r.Min.Y+y)*dst.Stride:]
		for x := range dw {
			sx := sb.Min.X + x*sw/dw
			cr, cg, cb, ca := src.At(sx, sy).RGBA()
			off := (r.Min.X + x) * 4
			row[off] = uint8(cr >> 8)
			row[off+1] = uint8(cg >> 8)
			row[off+2] = uint8(cb >> 8)
			row[off+3] = uint8(ca >> 8)
		}
	}
}

// --- Pure-Go PDF Structural Parser (xref tables, xref streams, ObjStm, pages, outlines) ---

type pdfKind uint8

const (
	valNull pdfKind = iota
	valBool
	valNum
	valName
	valString
	valArray
	valDict
	valRef
)

type pdfVal struct {
	kind   pdfKind
	b      bool
	num    float64
	str    string // decoded bytes for string, or name without '/'
	arr    []pdfVal
	dict   map[string]pdfVal
	refNum int
	refGen int
	stream []byte // raw stream bytes when this indirect object has a stream
}

func (v pdfVal) dictGet(k string) (pdfVal, bool) {
	if v.kind != valDict || v.dict == nil {
		return pdfVal{}, false
	}
	out, ok := v.dict[k]
	return out, ok
}

func (v pdfVal) intVal() (int, bool) {
	if v.kind != valNum {
		return 0, false
	}
	return int(math.Round(v.num)), true
}

type xrefEntry struct {
	kind   byte // 1 = uncompressed offset, 2 = compressed in ObjStm
	offset int64
	gen    int
	stmObj int
	stmIdx int
}

type rawPDF struct {
	data      []byte
	xref      map[int]xrefEntry
	trailer   pdfVal
	cache     map[int]pdfVal
	objStms   map[int]map[int]pdfVal
	pages     []int // object numbers of leaf /Page dictionaries
	pageByObj map[int]int
	pageDicts []pdfVal
	pageInfos []PageInfo
	outlines  []OutlineItem
}

func parseRawPDF(data []byte) (*rawPDF, error) {
	idx := bytes.Index(data[:min(len(data), 1024)], []byte("%PDF-"))
	if idx < 0 {
		return nil, ErrInvalid
	}
	r := &rawPDF{
		data:      data,
		xref:      map[int]xrefEntry{},
		cache:     map[int]pdfVal{},
		objStms:   map[int]map[int]pdfVal{},
		pageByObj: map[int]int{},
	}
	if err := r.loadXRef(); err != nil {
		return nil, err
	}
	if enc, ok := r.trailer.dictGet("Encrypt"); ok && enc.kind != valNull {
		return nil, ErrEncrypted
	}
	rootVal, ok := r.trailer.dictGet("Root")
	if !ok {
		return nil, ErrInvalid
	}
	catalog := r.resolve(rootVal, 0)
	if catalog.kind != valDict {
		return nil, ErrInvalid
	}
	pagesRef, ok := catalog.dictGet("Pages")
	if !ok {
		return nil, ErrInvalid
	}
	r.walkPages(pagesRef, [4]float64{0, 0, 595.28, 841.89}, [4]float64{}, false, 0, pdfVal{}, map[int]bool{})
	if len(r.pages) == 0 {
		return nil, ErrInvalid
	}
	if outRef, ok := catalog.dictGet("Outlines"); ok {
		outDict := r.resolve(outRef, 0)
		if first, ok := outDict.dictGet("First"); ok {
			r.outlines = r.readOutlineChain(first, catalog, 0, map[int]bool{})
		}
	}
	return r, nil
}

func (r *rawPDF) loadXRef() error {
	tailStart := max(0, len(r.data)-4096)
	tail := r.data[tailStart:]
	pos := bytes.LastIndex(tail, []byte("startxref"))
	if pos < 0 {
		return r.scanFallbackXRef()
	}
	sc := pdfScanner{b: r.data, pos: tailStart + pos + len("startxref")}
	sc.skipWS()
	startTok, ok := sc.readNumber()
	if !ok {
		return r.scanFallbackXRef()
	}
	start := int(startTok.num)
	if start < 0 || start >= len(r.data) {
		return r.scanFallbackXRef()
	}
	visited := map[int]bool{}
	cur := start
	first := true
	for cur >= 0 && cur < len(r.data) && !visited[cur] {
		visited[cur] = true
		tr, prev, err := r.readXRefAt(cur)
		if err != nil {
			if first {
				return r.scanFallbackXRef()
			}
			break
		}
		if first {
			r.trailer = tr
			first = false
		} else if r.trailer.kind == valDict && tr.kind == valDict {
			for k, v := range tr.dict {
				if _, exists := r.trailer.dict[k]; !exists {
					r.trailer.dict[k] = v
				}
			}
		}
		cur = prev
	}
	if len(r.xref) == 0 {
		return r.scanFallbackXRef()
	}
	return nil
}

func (r *rawPDF) readXRefAt(offset int) (pdfVal, int, error) {
	sc := pdfScanner{b: r.data, pos: offset}
	sc.skipWS()
	if sc.hasPrefix("xref") {
		sc.pos += 4
		for {
			sc.skipWS()
			if sc.hasPrefix("trailer") {
				sc.pos += 7
				break
			}
			firstNum, ok1 := sc.readInt()
			count, ok2 := sc.readInt()
			if !ok1 || !ok2 || count < 0 {
				return pdfVal{}, -1, ErrInvalid
			}
			for i := range count {
				off, okA := sc.readInt64()
				gen, okB := sc.readInt()
				sc.skipWS()
				if !okA || !okB || sc.pos >= len(r.data) {
					return pdfVal{}, -1, ErrInvalid
				}
				flag := r.data[sc.pos]
				sc.pos++
				objNum := firstNum + i
				if flag == 'n' && off > 0 {
					if _, exists := r.xref[objNum]; !exists {
						r.xref[objNum] = xrefEntry{kind: 1, offset: off, gen: gen}
					}
				}
			}
		}
		sc.skipWS()
		tr, ok := sc.readValue()
		if !ok || tr.kind != valDict {
			return pdfVal{}, -1, ErrInvalid
		}
		prev := -1
		if p, ok := tr.dictGet("Prev"); ok {
			if pi, ok := p.intVal(); ok {
				prev = pi
			}
		}
		// Hybrid-reference PDFs may also carry XRefStm.
		if xs, ok := tr.dictGet("XRefStm"); ok {
			if xi, ok := xs.intVal(); ok && xi > 0 && xi < len(r.data) {
				_, _, _ = r.readXRefAt(xi)
			}
		}
		return tr, prev, nil
	}

	// Cross-reference stream object: "<num> <gen> obj << ... >> stream ... endstream"
	obj, err := r.parseIndirectAt(offset)
	if err != nil || obj.kind != valDict || obj.stream == nil {
		return pdfVal{}, -1, ErrInvalid
	}
	decoded, err := r.decodeStream(obj)
	if err != nil {
		return pdfVal{}, -1, err
	}
	wVal, ok := obj.dictGet("W")
	if !ok || wVal.kind != valArray || len(wVal.arr) != 3 {
		return pdfVal{}, -1, ErrInvalid
	}
	w0, _ := wVal.arr[0].intVal()
	w1, _ := wVal.arr[1].intVal()
	w2, _ := wVal.arr[2].intVal()
	stride := w0 + w1 + w2
	if stride <= 0 {
		return pdfVal{}, -1, ErrInvalid
	}
	size, _ := obj.dict["Size"].intVal()
	var ranges [][2]int
	if idxVal, ok := obj.dictGet("Index"); ok && idxVal.kind == valArray && len(idxVal.arr) >= 2 {
		for i := 0; i+1 < len(idxVal.arr); i += 2 {
			f, _ := idxVal.arr[i].intVal()
			c, _ := idxVal.arr[i+1].intVal()
			ranges = append(ranges, [2]int{f, c})
		}
	} else {
		ranges = [][2]int{{0, size}}
	}
	pos := 0
	readField := func(width int) int64 {
		var v int64
		for range width {
			if pos < len(decoded) {
				v = (v << 8) | int64(decoded[pos])
				pos++
			}
		}
		return v
	}
	for _, rg := range ranges {
		for i := range rg[1] {
			if pos+stride > len(decoded) {
				break
			}
			typ := int64(1)
			if w0 > 0 {
				typ = readField(w0)
			}
			f1 := readField(w1)
			f2 := readField(w2)
			objNum := rg[0] + i
			if _, exists := r.xref[objNum]; exists {
				continue
			}
			switch typ {
			case 1:
				if f1 > 0 {
					r.xref[objNum] = xrefEntry{kind: 1, offset: f1, gen: int(f2)}
				}
			case 2:
				r.xref[objNum] = xrefEntry{kind: 2, stmObj: int(f1), stmIdx: int(f2)}
			}
		}
	}
	prev := -1
	if p, ok := obj.dictGet("Prev"); ok {
		if pi, ok := p.intVal(); ok {
			prev = pi
		}
	}
	return obj, prev, nil
}

// scanFallbackXRef linearly indexes "<num> <gen> obj" markers for slightly
// malformed PDFs whose startxref offset is off by a few bytes.
func (r *rawPDF) scanFallbackXRef() error {
	b := r.data
	for i := 0; i < len(b); i++ {
		if i > 0 && b[i-1] != '\n' && b[i-1] != '\r' && b[i-1] != ' ' {
			continue
		}
		if b[i] < '0' || b[i] > '9' {
			continue
		}
		sc := pdfScanner{b: b, pos: i}
		num, ok1 := sc.readInt()
		gen, ok2 := sc.readInt()
		sc.skipWS()
		if ok1 && ok2 && num > 0 && sc.hasPrefix("obj") && isPDFDelimOrWS(sc.peekAt(3)) {
			r.xref[num] = xrefEntry{kind: 1, offset: int64(i), gen: gen}
		}
	}
	if trPos := bytes.LastIndex(b, []byte("trailer")); trPos >= 0 {
		sc := pdfScanner{b: b, pos: trPos + 7}
		if tr, ok := sc.readValue(); ok && tr.kind == valDict {
			r.trailer = tr
		}
	}
	if r.trailer.kind != valDict {
		// Look for /Type /Catalog among objects.
		for num := range r.xref {
			v := r.getObject(num, 0)
			if t, ok := v.dictGet("Type"); ok && t.kind == valName && t.str == "Catalog" {
				r.trailer = pdfVal{kind: valDict, dict: map[string]pdfVal{
					"Root": {kind: valRef, refNum: num},
				}}
				break
			}
		}
	}
	if len(r.xref) == 0 || r.trailer.kind != valDict {
		return ErrInvalid
	}
	return nil
}

func (r *rawPDF) resolve(v pdfVal, depth int) pdfVal {
	if v.kind != valRef || depth > 16 {
		return v
	}
	return r.getObject(v.refNum, depth+1)
}

func (r *rawPDF) getObject(num int, depth int) pdfVal {
	if depth > 16 {
		return pdfVal{}
	}
	if cached, ok := r.cache[num]; ok {
		return cached
	}
	ent, ok := r.xref[num]
	if !ok {
		return pdfVal{}
	}
	var out pdfVal
	switch ent.kind {
	case 1:
		if ent.offset >= 0 && ent.offset < int64(len(r.data)) {
			out, _ = r.parseIndirectAt(int(ent.offset))
		}
	case 2:
		stmMap := r.getObjStm(ent.stmObj, depth+1)
		if stmMap != nil {
			out = stmMap[num]
		}
	}
	r.cache[num] = out
	return out
}

func (r *rawPDF) getObjStm(stmNum int, depth int) map[int]pdfVal {
	if m, ok := r.objStms[stmNum]; ok {
		return m
	}
	m := map[int]pdfVal{}
	r.objStms[stmNum] = m
	stmObj := r.getObject(stmNum, depth)
	if stmObj.kind != valDict || stmObj.stream == nil {
		return m
	}
	nObjs, ok1 := stmObj.dict["N"].intVal()
	firstOff, ok2 := stmObj.dict["First"].intVal()
	if !ok1 || !ok2 || nObjs <= 0 || firstOff < 0 {
		return m
	}
	decoded, err := r.decodeStream(stmObj)
	if err != nil || firstOff > len(decoded) {
		return m
	}
	hdr := pdfScanner{b: decoded[:firstOff]}
	type pair struct{ num, off int }
	pairs := make([]pair, 0, nObjs)
	for range nObjs {
		on, okA := hdr.readInt()
		oo, okB := hdr.readInt()
		if !okA || !okB {
			break
		}
		pairs = append(pairs, pair{on, firstOff + oo})
	}
	for _, p := range pairs {
		if p.off < 0 || p.off >= len(decoded) {
			continue
		}
		sc := pdfScanner{b: decoded, pos: p.off}
		if v, ok := sc.readValue(); ok {
			m[p.num] = v
		}
	}
	return m
}

func (r *rawPDF) parseIndirectAt(offset int) (pdfVal, error) {
	sc := pdfScanner{b: r.data, pos: offset}
	_, ok1 := sc.readInt()
	_, ok2 := sc.readInt()
	sc.skipWS()
	if !ok1 || !ok2 || !sc.hasPrefix("obj") {
		return pdfVal{}, ErrInvalid
	}
	sc.pos += 3
	val, ok := sc.readValue()
	if !ok {
		return pdfVal{}, ErrInvalid
	}
	sc.skipWS()
	if val.kind == valDict && sc.hasPrefix("stream") {
		sc.pos += 6
		if sc.pos < len(r.data) && r.data[sc.pos] == '\r' {
			sc.pos++
		}
		if sc.pos < len(r.data) && r.data[sc.pos] == '\n' {
			sc.pos++
		}
		start := sc.pos
		length := -1
		if lenVal, ok := val.dictGet("Length"); ok {
			resolved := r.resolve(lenVal, 1)
			if n, ok := resolved.intVal(); ok && n >= 0 && start+n <= len(r.data) {
				endCheck := start + n
				tail := r.data[endCheck:min(len(r.data), endCheck+32)]
				if bytes.Contains(tail, []byte("endstream")) {
					length = n
				}
			}
		}
		if length >= 0 {
			val.stream = r.data[start : start+length]
		} else if end := bytes.Index(r.data[start:], []byte("endstream")); end >= 0 {
			endPos := start + end
			if endPos > start && r.data[endPos-1] == '\n' {
				endPos--
			}
			if endPos > start && r.data[endPos-1] == '\r' {
				endPos--
			}
			val.stream = r.data[start:endPos]
		}
	}
	return val, nil
}

func (r *rawPDF) decodeStream(obj pdfVal) ([]byte, error) {
	if obj.stream == nil {
		return nil, nil
	}
	data := obj.stream
	filterVal, hasFilter := obj.dictGet("Filter")
	if !hasFilter {
		return data, nil
	}
	var filters []string
	var parms []pdfVal
	if filterVal.kind == valName {
		filters = []string{filterVal.str}
		if dp, ok := obj.dictGet("DecodeParms"); ok {
			parms = []pdfVal{r.resolve(dp, 1)}
		}
	} else if filterVal.kind == valArray {
		for _, f := range filterVal.arr {
			f = r.resolve(f, 1)
			if f.kind == valName {
				filters = append(filters, f.str)
			}
		}
		if dp, ok := obj.dictGet("DecodeParms"); ok {
			dp = r.resolve(dp, 1)
			if dp.kind == valArray {
				for _, p := range dp.arr {
					parms = append(parms, r.resolve(p, 1))
				}
			}
		}
	}
	for i, f := range filters {
		var parm pdfVal
		if i < len(parms) {
			parm = parms[i]
		}
		switch f {
		case "FlateDecode", "Fl":
			zr, err := zlib.NewReader(bytes.NewReader(data))
			if err != nil {
				return nil, err
			}
			out, err := io.ReadAll(io.LimitReader(zr, 64<<20))
			_ = zr.Close()
			if err != nil && len(out) == 0 {
				return nil, err
			}
			data = applyPredictor(out, parm)
		case "ASCIIHexDecode", "AHx":
			data = decodeASCIIHex(data)
		case "ASCII85Decode", "A85":
			data = decodeASCII85(data)
		default:
			return data, nil
		}
	}
	return data, nil
}

func applyPredictor(data []byte, parm pdfVal) []byte {
	if parm.kind != valDict {
		return data
	}
	pred, ok := parm.dict["Predictor"].intVal()
	if !ok || pred <= 1 {
		return data
	}
	cols := 1
	if c, ok := parm.dict["Columns"].intVal(); ok && c > 0 {
		cols = c
	}
	colors := 1
	if c, ok := parm.dict["Colors"].intVal(); ok && c > 0 {
		colors = c
	}
	bpc := 8
	if b, ok := parm.dict["BitsPerComponent"].intVal(); ok && b > 0 {
		bpc = b
	}
	bpp := max(1, (colors*bpc+7)/8)
	rowBytes := (cols*colors*bpc + 7) / 8
	if pred >= 10 && pred <= 15 {
		stride := rowBytes + 1
		if stride <= 1 || len(data) < stride {
			return data
		}
		rows := len(data) / stride
		out := make([]byte, 0, rows*rowBytes)
		prev := make([]byte, rowBytes)
		cur := make([]byte, rowBytes)
		for r := range rows {
			typ := data[r*stride]
			src := data[r*stride+1 : (r+1)*stride]
			copy(cur, src)
			switch typ {
			case 1: // Sub
				for i := bpp; i < rowBytes; i++ {
					cur[i] += cur[i-bpp]
				}
			case 2: // Up
				for i := range rowBytes {
					cur[i] += prev[i]
				}
			case 3: // Average
				for i := range rowBytes {
					var left byte
					if i >= bpp {
						left = cur[i-bpp]
					}
					cur[i] += byte((int(left) + int(prev[i])) / 2)
				}
			case 4: // Paeth
				for i := range rowBytes {
					var a, c byte
					b := prev[i]
					if i >= bpp {
						a = cur[i-bpp]
						c = prev[i-bpp]
					}
					cur[i] += paeth(a, b, c)
				}
			}
			out = append(out, cur...)
			copy(prev, cur)
		}
		return out
	}
	return data
}

func paeth(a, b, c byte) byte {
	ia, ib, ic := int(a), int(b), int(c)
	p := ia + ib - ic
	pa := absInt(p - ia)
	pb := absInt(p - ib)
	pc := absInt(p - ic)
	if pa <= pb && pa <= pc {
		return a
	}
	if pb <= pc {
		return b
	}
	return c
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func (r *rawPDF) walkPages(ref pdfVal, mediaBox [4]float64, cropBox [4]float64, hasCrop bool, rotate int, inheritedRes pdfVal, visited map[int]bool) {
	objNum := 0
	if ref.kind == valRef {
		objNum = ref.refNum
		if visited[objNum] {
			return
		}
		visited[objNum] = true
	}
	node := r.resolve(ref, 0)
	if node.kind != valDict {
		return
	}
	if mb, ok := r.readBox(node, "MediaBox"); ok {
		mediaBox = mb
	}
	if cb, ok := r.readBox(node, "CropBox"); ok {
		cropBox, hasCrop = cb, true
	}
	if rotVal, ok := node.dictGet("Rotate"); ok {
		if rv, ok := r.resolve(rotVal, 1).intVal(); ok {
			rotate = ((rv % 360) + 360) % 360
		}
	}
	if resVal, ok := node.dictGet("Resources"); ok {
		inheritedRes = resVal
	}

	typVal, _ := node.dictGet("Type")
	typ := typVal.str
	if kidsVal, hasKids := node.dictGet("Kids"); hasKids && typ != "Page" {
		kids := r.resolve(kidsVal, 1)
		if kids.kind == valArray {
			for _, k := range kids.arr {
				r.walkPages(k, mediaBox, cropBox, hasCrop, rotate, inheritedRes, visited)
			}
		}
		return
	}

	// Leaf /Page node.
	box := mediaBox
	if hasCrop {
		box = cropBox
	}
	w := math.Abs(box[2] - box[0])
	h := math.Abs(box[3] - box[1])
	if w <= 0 || h <= 0 {
		w, h = 595.28, 841.89
	}
	if rotate == 90 || rotate == 270 {
		w, h = h, w
	}
	if _, hasOwnRes := node.dictGet("Resources"); !hasOwnRes && inheritedRes.kind != valNull {
		cloned := make(map[string]pdfVal, len(node.dict)+1)
		for k, v := range node.dict {
			cloned[k] = v
		}
		cloned["Resources"] = inheritedRes
		node.dict = cloned
	}
	idx := len(r.pages)
	r.pages = append(r.pages, objNum)
	if objNum > 0 {
		r.pageByObj[objNum] = idx
	}
	r.pageDicts = append(r.pageDicts, node)
	r.pageInfos = append(r.pageInfos, PageInfo{Width: w, Height: h, Rotation: rotate})
}

func (r *rawPDF) readBox(dict pdfVal, key string) ([4]float64, bool) {
	v, ok := dict.dictGet(key)
	if !ok {
		return [4]float64{}, false
	}
	v = r.resolve(v, 1)
	if v.kind != valArray || len(v.arr) < 4 {
		return [4]float64{}, false
	}
	var out [4]float64
	for i := range 4 {
		elem := r.resolve(v.arr[i], 1)
		if elem.kind != valNum {
			return [4]float64{}, false
		}
		out[i] = elem.num
	}
	return out, true
}

func (r *rawPDF) readOutlineChain(firstRef pdfVal, catalog pdfVal, depth int, seen map[int]bool) []OutlineItem {
	if depth > 12 {
		return nil
	}
	var out []OutlineItem
	cur := firstRef
	for cur.kind != valNull {
		if cur.kind == valRef {
			if seen[cur.refNum] {
				break
			}
			seen[cur.refNum] = true
		}
		item := r.resolve(cur, 0)
		if item.kind != valDict {
			break
		}
		titleVal, _ := item.dictGet("Title")
		titleVal = r.resolve(titleVal, 1)
		title := strings.TrimSpace(decodePDFTextString(titleVal.str))
		pageIdx := r.resolveOutlinePage(item, catalog)
		var kids []OutlineItem
		if childFirst, ok := item.dictGet("First"); ok {
			kids = r.readOutlineChain(childFirst, catalog, depth+1, seen)
		}
		if title != "" || len(kids) > 0 {
			if title == "" {
				title = fmt.Sprintf("Page %d", max(pageIdx+1, 1))
			}
			out = append(out, OutlineItem{Title: title, Page: pageIdx, Children: kids})
		}
		next, ok := item.dictGet("Next")
		if !ok {
			break
		}
		cur = next
	}
	return out
}

func (r *rawPDF) resolveOutlinePage(item pdfVal, catalog pdfVal) int {
	if dest, ok := item.dictGet("Dest"); ok {
		return r.destPage(dest, catalog)
	}
	if act, ok := item.dictGet("A"); ok {
		act = r.resolve(act, 1)
		if act.kind == valDict {
			if s, ok := act.dictGet("S"); ok && s.str == "GoTo" {
				if d, ok := act.dictGet("D"); ok {
					return r.destPage(d, catalog)
				}
			}
		}
	}
	return -1
}

func (r *rawPDF) destPage(dest pdfVal, catalog pdfVal) int {
	dest = r.resolve(dest, 1)
	if dest.kind == valString || dest.kind == valName {
		dest = r.lookupNamedDest(dest.str, catalog)
	}
	if dest.kind == valDict {
		if d, ok := dest.dictGet("D"); ok {
			dest = r.resolve(d, 1)
		}
	}
	if dest.kind == valArray && len(dest.arr) > 0 {
		first := dest.arr[0]
		if first.kind == valRef {
			if idx, ok := r.pageByObj[first.refNum]; ok {
				return idx
			}
		} else if n, ok := first.intVal(); ok && n >= 0 && n < len(r.pages) {
			return n
		}
	}
	return -1
}

func (r *rawPDF) lookupNamedDest(name string, catalog pdfVal) pdfVal {
	if dests, ok := catalog.dictGet("Dests"); ok {
		dests = r.resolve(dests, 1)
		if v, ok := dests.dictGet(name); ok {
			return r.resolve(v, 1)
		}
	}
	if names, ok := catalog.dictGet("Names"); ok {
		names = r.resolve(names, 1)
		if tree, ok := names.dictGet("Dests"); ok {
			return r.searchNameTree(r.resolve(tree, 1), name, 0)
		}
	}
	return pdfVal{}
}

func (r *rawPDF) searchNameTree(node pdfVal, target string, depth int) pdfVal {
	if node.kind != valDict || depth > 12 {
		return pdfVal{}
	}
	if arr, ok := node.dictGet("Names"); ok {
		arr = r.resolve(arr, 1)
		if arr.kind == valArray {
			for i := 0; i+1 < len(arr.arr); i += 2 {
				k := r.resolve(arr.arr[i], 1)
				if k.str == target {
					return r.resolve(arr.arr[i+1], 1)
				}
			}
		}
	}
	if kids, ok := node.dictGet("Kids"); ok {
		kids = r.resolve(kids, 1)
		if kids.kind == valArray {
			for _, kid := range kids.arr {
				if v := r.searchNameTree(r.resolve(kid, 1), target, depth+1); v.kind != valNull {
					return v
				}
			}
		}
	}
	return pdfVal{}
}

func (r *rawPDF) firstPageImage(page int) image.Image {
	if page < 0 || page >= len(r.pageDicts) {
		return nil
	}
	res, ok := r.pageDicts[page].dictGet("Resources")
	if !ok {
		return nil
	}
	res = r.resolve(res, 1)
	xobj, ok := res.dictGet("XObject")
	if !ok {
		return nil
	}
	xobj = r.resolve(xobj, 1)
	if xobj.kind != valDict {
		return nil
	}
	for _, v := range xobj.dict {
		im := r.resolve(v, 1)
		if im.kind != valDict || im.stream == nil {
			continue
		}
		if sub, ok := im.dictGet("Subtype"); !ok || sub.str != "Image" {
			continue
		}
		fVal, _ := im.dictGet("Filter")
		fName := fVal.str
		if fVal.kind == valArray && len(fVal.arr) == 1 {
			fName = r.resolve(fVal.arr[0], 1).str
		}
		if fName == "DCTDecode" {
			if decoded, _, err := image.Decode(bytes.NewReader(im.stream)); err == nil {
				return decoded
			}
		}
	}
	return nil
}

func decodePDFTextString(raw string) string {
	b := []byte(raw)
	if len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF {
		u16 := make([]uint16, 0, (len(b)-2)/2)
		for i := 2; i+1 < len(b); i += 2 {
			u16 = append(u16, binary.BigEndian.Uint16(b[i:i+2]))
		}
		return string(utf16.Decode(u16))
	}
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		u16 := make([]uint16, 0, (len(b)-2)/2)
		for i := 2; i+1 < len(b); i += 2 {
			u16 = append(u16, binary.LittleEndian.Uint16(b[i:i+2]))
		}
		return string(utf16.Decode(u16))
	}
	// PDFDocEncoding / Latin-1 fallback.
	var out strings.Builder
	out.Grow(len(b))
	for _, ch := range b {
		out.WriteRune(pdfDocEncodingRune(ch))
	}
	return out.String()
}

func pdfDocEncodingRune(b byte) rune {
	switch b {
	case 0x80:
		return '•'
	case 0x84:
		return '—'
	case 0x85:
		return '–'
	case 0x8C:
		return '“'
	case 0x8D:
		return '”'
	case 0x8E:
		return '‘'
	case 0x8F:
		return '’'
	default:
		return rune(b)
	}
}

func decodeASCIIHex(src []byte) []byte {
	var out []byte
	hi := -1
	for _, c := range src {
		if c == '>' {
			break
		}
		v := hexNib(c)
		if v < 0 {
			continue
		}
		if hi < 0 {
			hi = v
		} else {
			out = append(out, byte(hi<<4|v))
			hi = -1
		}
	}
	if hi >= 0 {
		out = append(out, byte(hi<<4))
	}
	return out
}

func decodeASCII85(src []byte) []byte {
	var out []byte
	var tuple uint32
	count := 0
	for i := 0; i < len(src); i++ {
		c := src[i]
		if c == '~' {
			break
		}
		if isPDFWS(c) {
			continue
		}
		if c == 'z' && count == 0 {
			out = append(out, 0, 0, 0, 0)
			continue
		}
		if c < '!' || c > 'u' {
			continue
		}
		tuple = tuple*85 + uint32(c-'!')
		count++
		if count == 5 {
			out = append(out, byte(tuple>>24), byte(tuple>>16), byte(tuple>>8), byte(tuple))
			tuple, count = 0, 0
		}
	}
	if count > 1 {
		for i := count; i < 5; i++ {
			tuple = tuple*85 + 84
		}
		var tail [4]byte
		binary.BigEndian.PutUint32(tail[:], tuple)
		out = append(out, tail[:count-1]...)
	}
	return out
}

func hexNib(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return -1
	}
}

// --- Lexical Scanner for PDF Objects ---

type pdfScanner struct {
	b   []byte
	pos int
}

func isPDFWS(c byte) bool {
	return c == 0 || c == '\t' || c == '\n' || c == '\f' || c == '\r' || c == ' '
}

func isPDFDelim(c byte) bool {
	switch c {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}

func isPDFDelimOrWS(c byte) bool {
	return isPDFWS(c) || isPDFDelim(c)
}

func (s *pdfScanner) peekAt(off int) byte {
	if s.pos+off >= len(s.b) {
		return ' '
	}
	return s.b[s.pos+off]
}

func (s *pdfScanner) hasPrefix(p string) bool {
	return s.pos+len(p) <= len(s.b) && string(s.b[s.pos:s.pos+len(p)]) == p
}

func (s *pdfScanner) skipWS() {
	for s.pos < len(s.b) {
		c := s.b[s.pos]
		if isPDFWS(c) {
			s.pos++
			continue
		}
		if c == '%' {
			for s.pos < len(s.b) && s.b[s.pos] != '\n' && s.b[s.pos] != '\r' {
				s.pos++
			}
			continue
		}
		break
	}
}

func (s *pdfScanner) readInt() (int, bool) {
	v, ok := s.readNumber()
	if !ok {
		return 0, false
	}
	return int(v.num), true
}

func (s *pdfScanner) readInt64() (int64, bool) {
	s.skipWS()
	start := s.pos
	if s.pos < len(s.b) && (s.b[s.pos] == '+' || s.b[s.pos] == '-') {
		s.pos++
	}
	digits := 0
	for s.pos < len(s.b) && s.b[s.pos] >= '0' && s.b[s.pos] <= '9' {
		s.pos++
		digits++
	}
	if digits == 0 {
		s.pos = start
		return 0, false
	}
	n, err := strconv.ParseInt(string(s.b[start:s.pos]), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func (s *pdfScanner) readNumber() (pdfVal, bool) {
	s.skipWS()
	start := s.pos
	if s.pos < len(s.b) && (s.b[s.pos] == '+' || s.b[s.pos] == '-') {
		s.pos++
	}
	digits := 0
	dot := false
	for s.pos < len(s.b) {
		c := s.b[s.pos]
		if c >= '0' && c <= '9' {
			digits++
			s.pos++
		} else if c == '.' && !dot {
			dot = true
			s.pos++
		} else {
			break
		}
	}
	if digits == 0 {
		s.pos = start
		return pdfVal{}, false
	}
	f, err := strconv.ParseFloat(string(s.b[start:s.pos]), 64)
	if err != nil {
		s.pos = start
		return pdfVal{}, false
	}
	return pdfVal{kind: valNum, num: f}, true
}

func (s *pdfScanner) readValue() (pdfVal, bool) {
	return s.readValueDepth(0)
}

func (s *pdfScanner) readValueDepth(depth int) (pdfVal, bool) {
	if depth > 24 {
		return pdfVal{}, false
	}
	s.skipWS()
	if s.pos >= len(s.b) {
		return pdfVal{}, false
	}
	c := s.b[s.pos]
	switch {
	case c == '/':
		s.pos++
		var name bytes.Buffer
		for s.pos < len(s.b) && !isPDFDelimOrWS(s.b[s.pos]) {
			if s.b[s.pos] == '#' && s.pos+2 < len(s.b) {
				h1, h2 := hexNib(s.b[s.pos+1]), hexNib(s.b[s.pos+2])
				if h1 >= 0 && h2 >= 0 {
					name.WriteByte(byte(h1<<4 | h2))
					s.pos += 3
					continue
				}
			}
			name.WriteByte(s.b[s.pos])
			s.pos++
		}
		return pdfVal{kind: valName, str: name.String()}, true

	case c == '(':
		return pdfVal{kind: valString, str: string(s.readLiteralString())}, true

	case c == '<':
		if s.pos+1 < len(s.b) && s.b[s.pos+1] == '<' {
			s.pos += 2
			dict := map[string]pdfVal{}
			for {
				s.skipWS()
				if s.pos >= len(s.b) {
					break
				}
				if s.hasPrefix(">>") {
					s.pos += 2
					break
				}
				k, ok := s.readValueDepth(depth + 1)
				if !ok || k.kind != valName {
					s.pos++
					continue
				}
				v, ok := s.readValueDepth(depth + 1)
				if !ok {
					break
				}
				dict[k.str] = v
			}
			return pdfVal{kind: valDict, dict: dict}, true
		}
		s.pos++
		start := s.pos
		for s.pos < len(s.b) && s.b[s.pos] != '>' {
			s.pos++
		}
		raw := decodeASCIIHex(s.b[start:s.pos])
		if s.pos < len(s.b) && s.b[s.pos] == '>' {
			s.pos++
		}
		return pdfVal{kind: valString, str: string(raw)}, true

	case c == '[':
		s.pos++
		var arr []pdfVal
		for {
			s.skipWS()
			if s.pos >= len(s.b) {
				break
			}
			if s.b[s.pos] == ']' {
				s.pos++
				break
			}
			v, ok := s.readValueDepth(depth + 1)
			if !ok {
				s.pos++
				continue
			}
			arr = append(arr, v)
		}
		return pdfVal{kind: valArray, arr: arr}, true

	case c == '+' || c == '-' || c == '.' || (c >= '0' && c <= '9'):
		num1, ok := s.readNumber()
		if !ok {
			return pdfVal{}, false
		}
		saved := s.pos
		if gen, ok2 := s.readNumber(); ok2 && !strings.ContainsRune(fmt.Sprintf("%g", num1.num), '.') {
			s.skipWS()
			if s.pos < len(s.b) && s.b[s.pos] == 'R' && isPDFDelimOrWS(s.peekAt(1)) {
				s.pos++
				return pdfVal{kind: valRef, refNum: int(num1.num), refGen: int(gen.num)}, true
			}
		}
		s.pos = saved
		return num1, true

	default:
		start := s.pos
		for s.pos < len(s.b) && !isPDFDelimOrWS(s.b[s.pos]) {
			s.pos++
		}
		tok := string(s.b[start:s.pos])
		switch tok {
		case "true":
			return pdfVal{kind: valBool, b: true}, true
		case "false":
			return pdfVal{kind: valBool, b: false}, true
		case "null":
			return pdfVal{kind: valNull}, true
		default:
			if tok == "" {
				return pdfVal{}, false
			}
			return pdfVal{kind: valName, str: tok}, true
		}
	}
}

func (s *pdfScanner) readLiteralString() []byte {
	if s.pos >= len(s.b) || s.b[s.pos] != '(' {
		return nil
	}
	s.pos++
	var out []byte
	depth := 1
	for s.pos < len(s.b) && depth > 0 {
		c := s.b[s.pos]
		s.pos++
		switch c {
		case '(':
			depth++
			out = append(out, '(')
		case ')':
			depth--
			if depth > 0 {
				out = append(out, ')')
			}
		case '\\':
			if s.pos >= len(s.b) {
				break
			}
			esc := s.b[s.pos]
			s.pos++
			switch esc {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case '\r':
				if s.pos < len(s.b) && s.b[s.pos] == '\n' {
					s.pos++
				}
			case '\n':
				// line continuation
			default:
				if esc >= '0' && esc <= '7' {
					oct := int(esc - '0')
					for range 2 {
						if s.pos < len(s.b) && s.b[s.pos] >= '0' && s.b[s.pos] <= '7' {
							oct = oct*8 + int(s.b[s.pos]-'0')
							s.pos++
						} else {
							break
						}
					}
					out = append(out, byte(oct))
				} else {
					out = append(out, esc)
				}
			}
		default:
			out = append(out, c)
		}
	}
	return out
}
