// Package bookdoc opens, inspects, searches, and reads EPUB and HTML
// documents in pure Go without cgo or UI dependencies.
package bookdoc

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"
)

const (
	maxHTMLBytes    = 32 << 20
	maxChapterBytes = 16 << 20
	maxAssetBytes   = 20 << 20
	maxPlainBytes   = 2 << 20
	maxFindMatches  = 500
)

var (
	// ErrInvalid reports that the input is not a readable HTML or EPUB document.
	ErrInvalid = errors.New("bookdoc: invalid or damaged document")
	// ErrEncrypted reports that the EPUB uses DRM encryption on its content.
	ErrEncrypted = errors.New("bookdoc: DRM-protected EPUB is not supported")
)

// Kind identifies whether a Document is an HTML page or an EPUB book.
type Kind uint8

const (
	KindHTML Kind = iota
	KindEPUB
)

// Chapter describes one reading section (one file in an EPUB spine, or the
// single body of an HTML document).
type Chapter struct {
	Index int
	ID    string
	Href  string
	Title string
}

// TOCItem represents one entry in a book's table of contents or an HTML heading outline.
type TOCItem struct {
	Title        string
	Href         string
	Chapter      int // 0-based chapter index, or -1 if unresolved
	Anchor       string
	HeadingIndex int // 0-based heading index for HTML outlines, or -1
	Children     []TOCItem
}

// Match is one search occurrence inside a chapter.
type Match struct {
	Chapter    int
	Occurrence int // 0-based match index within ChapterText(Chapter)
	Snippet    string
}

// Document represents an opened read-only HTML or EPUB document.
// All exported methods are safe for concurrent use.
type Document struct {
	kind        Kind
	path        string
	title       string
	author      string
	language    string
	description string

	chapters []Chapter
	toc      []TOCItem

	// HTML fields.
	rawSource string

	// EPUB fields.
	zipSrc       *scopedZipFile
	zr           *zip.Reader
	entries      map[string]*zip.File
	lowerEntries map[string]*zip.File
	spineByPath  map[string]int
	spineLower   map[string]int
	coverPath    string
	coverMIME    string

	mu           sync.Mutex
	chapterHTMLs map[int]string
	chapterTexts map[int]string
	closed       bool
}

// scopedZipFile backs zip.Reader without keeping a persistent OS file handle
// open when idle, allowing Windows callers to rename, move, or delete an open
// or cached EPUB file cleanly.
type scopedZipFile struct {
	path string
	cur  *os.File // guarded by Document.mu (or single-goroutine openEPUBFile)
}

func (s *scopedZipFile) withFile(fn func() ([]byte, error)) ([]byte, error) {
	if s.cur != nil {
		return fn()
	}
	f, err := os.Open(s.path)
	if err != nil {
		return nil, err
	}
	s.cur = f
	defer func() {
		s.cur = nil
		_ = f.Close()
	}()
	return fn()
}

func (s *scopedZipFile) ReadAt(p []byte, off int64) (int, error) {
	if s.cur != nil {
		return s.cur.ReadAt(p, off)
	}
	f, err := os.Open(s.path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	return f.ReadAt(p, off)
}

// Open opens an HTML (.html/.htm) or EPUB (.epub) file from disk.
func Open(filePath string) (*Document, error) {
	if filePath == "" {
		return nil, ErrInvalid
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".epub" {
		return openEPUBFile(filePath)
	}
	if ext == ".html" || ext == ".htm" {
		return openHTMLFile(filePath)
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	var head [4]byte
	n, _ := io.ReadFull(f, head[:])
	_ = f.Close()
	if n == 4 && head[0] == 'P' && head[1] == 'K' && head[2] == 3 && head[3] == 4 {
		return openEPUBFile(filePath)
	}
	return openHTMLFile(filePath)
}

// OpenBytes opens an HTML or EPUB document from an in-memory byte slice.
func OpenBytes(name string, data []byte) (*Document, error) {
	if len(data) == 0 {
		return nil, ErrInvalid
	}
	ext := strings.ToLower(path.Ext(strings.ReplaceAll(name, `\`, "/")))
	if ext == ".epub" || (ext == "" && len(data) >= 4 && data[0] == 'P' && data[1] == 'K' && data[2] == 3 && data[3] == 4) {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, ErrInvalid
		}
		return parseEPUB(name, zr, nil)
	}
	if len(data) > maxHTMLBytes {
		data = data[:maxHTMLBytes]
	}
	return parseHTMLBytes(name, data)
}

func openHTMLFile(filePath string) (*Document, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxHTMLBytes))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, ErrInvalid
	}
	return parseHTMLBytes(filePath, data)
}

func openEPUBFile(filePath string) (*Document, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	src := &scopedZipFile{path: filePath, cur: f}
	defer func() {
		src.cur = nil
		_ = f.Close()
	}()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() <= 0 {
		return nil, ErrInvalid
	}
	zr, err := zip.NewReader(src, info.Size())
	if err != nil {
		return nil, ErrInvalid
	}
	return parseEPUB(filePath, zr, src)
}

// Kind returns whether the document is KindHTML or KindEPUB.
func (d *Document) Kind() Kind {
	if d == nil {
		return KindHTML
	}
	return d.kind
}

// Path returns the path or name passed when opening the document.
func (d *Document) Path() string {
	if d == nil {
		return ""
	}
	d.mu.Lock()
	p := d.path
	d.mu.Unlock()
	return p
}

// SetPath updates the on-disk path after the underlying file is renamed or moved.
func (d *Document) SetPath(filePath string) {
	if d == nil || filePath == "" {
		return
	}
	d.mu.Lock()
	d.path = filePath
	if d.zipSrc != nil {
		d.zipSrc.path = filePath
	}
	d.mu.Unlock()
}

// Title returns the book or page title.
func (d *Document) Title() string {
	if d == nil {
		return ""
	}
	return d.title
}

// Author returns the extracted author metadata, if any.
func (d *Document) Author() string {
	if d == nil {
		return ""
	}
	return d.author
}

// Language returns the document language tag, if any.
func (d *Document) Language() string {
	if d == nil {
		return ""
	}
	return d.language
}

// Description returns the document summary or meta description, if any.
func (d *Document) Description() string {
	if d == nil {
		return ""
	}
	return d.description
}

// ChapterCount returns the number of spine chapters (1 for HTML documents).
func (d *Document) ChapterCount() int {
	if d == nil {
		return 0
	}
	return len(d.chapters)
}

// Chapter returns metadata for the 0-based chapter index.
func (d *Document) Chapter(idx int) Chapter {
	if d == nil || idx < 0 || idx >= len(d.chapters) {
		return Chapter{}
	}
	return d.chapters[idx]
}

// Chapters returns a copy of the document's chapters slice.
func (d *Document) Chapters() []Chapter {
	if d == nil {
		return nil
	}
	return slices.Clone(d.chapters)
}

// TOC returns a deep copy of the document's table of contents.
func (d *Document) TOC() []TOCItem {
	if d == nil || len(d.toc) == 0 {
		return nil
	}
	return cloneTOC(d.toc)
}

func cloneTOC(items []TOCItem) []TOCItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]TOCItem, len(items))
	for i, it := range items {
		out[i] = it
		out[i].Children = cloneTOC(it.Children)
	}
	return out
}

// RawSource returns the UTF-8 source for HTML documents (or the first chapter
// for EPUB documents if needed).
func (d *Document) RawSource() string {
	if d == nil {
		return ""
	}
	if d.kind == KindHTML {
		return d.rawSource
	}
	return ""
}

// ChapterHTML returns the UTF-8 HTML/XHTML markup for 0-based chapter idx.
func (d *Document) ChapterHTML(idx int) (string, error) {
	if d == nil || idx < 0 || idx >= len(d.chapters) {
		return "", ErrInvalid
	}
	if d.kind == KindHTML {
		return d.rawSource, nil
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return "", ErrInvalid
	}
	if cached, ok := d.chapterHTMLs[idx]; ok {
		d.mu.Unlock()
		return cached, nil
	}
	href := d.chapters[idx].Href
	raw, err := d.readZipLocked(href, maxChapterBytes)
	if err != nil {
		d.mu.Unlock()
		return "", err
	}
	text := decodeHTMLBytes(raw)
	if d.chapterHTMLs == nil {
		d.chapterHTMLs = make(map[int]string)
	}
	if len(d.chapterHTMLs) >= 8 {
		// Bound cached raw XHTML payloads in memory for large books.
		clear(d.chapterHTMLs)
	}
	d.chapterHTMLs[idx] = text
	d.mu.Unlock()
	return text, nil
}

// CoverImage returns the EPUB cover image bytes and MIME type when available.
func (d *Document) CoverImage() (data []byte, mimeType string, ok bool) {
	if d == nil || d.kind != KindEPUB || d.coverPath == "" {
		return nil, "", false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, "", false
	}
	raw, err := d.readZipLocked(d.coverPath, maxAssetBytes)
	if err != nil || len(raw) == 0 {
		return nil, "", false
	}
	mt := d.coverMIME
	if mt == "" {
		mt = inferMIMEFromPath(d.coverPath)
	}
	return raw, mt, true
}

// ReadAsset reads an embedded asset referenced by rawSrc from chapterIdx:
// - data: URIs (base64 or URL-encoded) for both HTML and EPUB;
// - EPUB zip entries resolved relative to chapterIdx's Href.
// External http(s) URLs and local disk images next to standalone HTML files
// return ok=false so the UI host can resolve/cache them with its image pipeline.
func (d *Document) ReadAsset(chapterIdx int, rawSrc string) (data []byte, mimeType string, key string, ok bool) {
	if d == nil {
		return nil, "", "", false
	}
	src := strings.TrimSpace(rawSrc)
	if src == "" {
		return nil, "", "", false
	}
	if len(src) >= 5 && strings.EqualFold(src[:5], "data:") {
		b, mt, err := decodeDataURI(src)
		if err != nil || len(b) == 0 {
			return nil, "", "", false
		}
		sum := sha256.Sum256(b)
		return b, mt, "data:" + hex.EncodeToString(sum[:12]), true
	}
	if d.kind != KindEPUB {
		return nil, "", "", false
	}
	lower := strings.ToLower(src)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "javascript:") {
		return nil, "", "", false
	}
	baseHref := ""
	if chapterIdx >= 0 && chapterIdx < len(d.chapters) {
		baseHref = d.chapters[chapterIdx].Href
	}
	target, valid := resolveRelativeZipPath(baseHref, src)
	if !valid {
		return nil, "", "", false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, "", "", false
	}
	b, err := d.readZipLocked(target, maxAssetBytes)
	if err != nil || len(b) == 0 {
		return nil, "", "", false
	}
	return b, inferMIMEFromPath(target), "epub:" + target, true
}

// ResolveLink resolves a link href clicked in chapter fromChapter.
// Internal targets return (chapter >= 0, anchor, "", true); external URLs return
// (-1, "", externalURL, true).
func (d *Document) ResolveLink(fromChapter int, href string) (chapter int, anchor string, externalURL string, ok bool) {
	if d == nil || len(d.chapters) == 0 {
		return -1, "", "", false
	}
	raw := strings.TrimSpace(href)
	if raw == "" {
		return -1, "", "", false
	}
	low := strings.ToLower(raw)
	if strings.HasPrefix(low, "javascript:") || strings.HasPrefix(low, "vbscript:") || strings.HasPrefix(low, "data:") {
		return -1, "", "", false
	}
	if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "mailto:") {
		return -1, "", raw, true
	}
	if strings.HasPrefix(raw, "#") {
		ch := max(0, min(fromChapter, len(d.chapters)-1))
		return ch, unescapeFragment(raw[1:]), "", true
	}
	filePart, frag, _ := strings.Cut(raw, "#")
	filePart, _, _ = strings.Cut(filePart, "?")
	filePart = strings.TrimSpace(filePart)
	if d.kind == KindHTML {
		if filePart == "" || strings.EqualFold(path.Base(strings.ReplaceAll(filePart, `\`, "/")), path.Base(strings.ReplaceAll(d.Path(), `\`, "/"))) {
			return 0, unescapeFragment(frag), "", true
		}
		return -1, "", "", false
	}
	baseHref := ""
	if fromChapter >= 0 && fromChapter < len(d.chapters) {
		baseHref = d.chapters[fromChapter].Href
	}
	target, valid := resolveRelativeZipPath(baseHref, filePart)
	if !valid {
		return -1, "", "", false
	}
	if idx, found := d.lookupSpineIndex(target); found {
		return idx, unescapeFragment(frag), "", true
	}
	return -1, "", "", false
}

// ChapterText returns the extracted plain text of 0-based chapter idx.
func (d *Document) ChapterText(idx int) string {
	if d == nil || idx < 0 || idx >= len(d.chapters) {
		return ""
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return ""
	}
	if txt, ok := d.chapterTexts[idx]; ok {
		d.mu.Unlock()
		return txt
	}
	d.mu.Unlock()

	src, err := d.ChapterHTML(idx)
	if err != nil {
		return ""
	}
	txt := extractHTMLPlainText(src)

	d.mu.Lock()
	if !d.closed {
		if d.chapterTexts == nil {
			d.chapterTexts = make(map[int]string, len(d.chapters))
		}
		d.chapterTexts[idx] = txt
	}
	d.mu.Unlock()
	return txt
}

// PlainText joins extracted plain text across up to maxChapters chapters (capped at 2 MiB).
func (d *Document) PlainText(maxChapters int) string {
	if d == nil || len(d.chapters) == 0 {
		return ""
	}
	cnt := len(d.chapters)
	if maxChapters > 0 && maxChapters < cnt {
		cnt = maxChapters
	}
	var b strings.Builder
	for i := range cnt {
		txt := strings.TrimSpace(d.ChapterText(i))
		if txt == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		if b.Len()+len(txt) > maxPlainBytes {
			rem := maxPlainBytes - b.Len()
			if rem > 0 {
				b.WriteString(txt[:rem])
			}
			break
		}
		b.WriteString(txt)
	}
	return b.String()
}

// ChapterPreview extracts a concise heading and up to maxParas paragraphs from chapter idx.
func (d *Document) ChapterPreview(idx int, maxParas int) (heading string, paras []string) {
	if d == nil || idx < 0 || idx >= len(d.chapters) {
		return "", nil
	}
	if maxParas <= 0 {
		maxParas = 5
	}
	src, err := d.ChapterHTML(idx)
	if err != nil {
		return d.chapters[idx].Title, nil
	}
	heading, paras = extractHTMLPreview(src, maxParas)
	if heading == "" {
		heading = d.chapters[idx].Title
	}
	return heading, paras
}

// Find performs a case-insensitive substring search across all chapters.
func (d *Document) Find(query string) []Match {
	if d == nil || len(d.chapters) == 0 {
		return nil
	}
	q := []rune(strings.Map(unicode.ToLower, strings.TrimSpace(query)))
	if len(q) == 0 {
		return nil
	}
	matcher := newRuneMatcher(q)
	var out []Match
	for ch := range len(d.chapters) {
		text := d.ChapterText(ch)
		if text == "" {
			continue
		}
		spans := matcher.findByteSpans(text)
		for occ, sp := range spans {
			out = append(out, Match{
				Chapter:    ch,
				Occurrence: occ,
				Snippet:    buildMatchSnippet(text, sp[0], sp[1]),
			})
			if len(out) >= maxFindMatches {
				return out
			}
		}
	}
	return out
}

// Close releases cached payloads and marks the document closed.
func (d *Document) Close() {
	if d == nil {
		return
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	d.zipSrc = nil
	d.zr = nil
	d.chapterHTMLs = nil
	d.mu.Unlock()
}

func resolveRelativeZipPath(baseHref, rawRef string) (string, bool) {
	ref, _, _ := strings.Cut(strings.TrimSpace(rawRef), "#")
	ref, _, _ = strings.Cut(ref, "?")
	ref = strings.ReplaceAll(ref, `\`, "/")
	if unesc, err := neturl.PathUnescape(ref); err == nil {
		ref = unesc
	}
	if ref == "" || strings.ContainsRune(ref, '\x00') {
		return "", false
	}
	var joined string
	if strings.HasPrefix(ref, "/") {
		joined = path.Clean(strings.TrimLeft(ref, "/"))
	} else {
		baseDir := ""
		if baseHref != "" {
			baseDir = path.Dir(normalizeZipPath(baseHref))
			if baseDir == "." {
				baseDir = ""
			}
		}
		if baseDir != "" {
			joined = path.Clean(baseDir + "/" + ref)
		} else {
			joined = path.Clean(ref)
		}
	}
	joined = strings.TrimLeft(joined, "/")
	if joined == "" || joined == "." || joined == ".." || strings.HasPrefix(joined, "../") {
		return "", false
	}
	return joined, true
}

func normalizeZipPath(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	if unesc, err := neturl.PathUnescape(p); err == nil {
		p = unesc
	}
	p = path.Clean(strings.TrimLeft(p, "/"))
	if p == "." {
		return ""
	}
	return p
}

func unescapeFragment(frag string) string {
	frag = strings.TrimSpace(frag)
	if unesc, err := neturl.PathUnescape(frag); err == nil {
		return unesc
	}
	return frag
}

func inferMIMEFromPath(p string) string {
	ext := strings.ToLower(path.Ext(p))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".bmp":
		return "image/bmp"
	case ".avif":
		return "image/avif"
	}
	if mt := mime.TypeByExtension(ext); mt != "" {
		return mt
	}
	return ""
}

type runeMatcher struct {
	pattern []rune
	failure []int
}

func newRuneMatcher(q []rune) runeMatcher {
	m := runeMatcher{pattern: q, failure: make([]int, len(q))}
	for i, j := 1, 0; i < len(q); i++ {
		for j > 0 && q[i] != q[j] {
			j = m.failure[j-1]
		}
		if q[i] == q[j] {
			j++
		}
		m.failure[i] = j
	}
	return m
}

func (m runeMatcher) findByteSpans(text string) [][2]int {
	q := m.pattern
	if len(q) == 0 || len(text) == 0 {
		return nil
	}
	ring := make([]int, len(q))
	var out [][2]int
	idx, matched := 0, 0
	for bytePos, r := range text {
		ring[idx%len(q)] = bytePos
		lr := unicode.ToLower(r)
		for matched > 0 && lr != q[matched] {
			matched = m.failure[matched-1]
		}
		if lr == q[matched] {
			matched++
		}
		if matched == len(q) {
			start := ring[(idx-len(q)+1)%len(q)]
			end := bytePos + len(string(r))
			out = append(out, [2]int{start, end})
			matched = 0
		}
		idx++
	}
	return out
}

func buildMatchSnippet(text string, startByte, endByte int) string {
	const contextRunes = 24
	before := []rune(text[:startByte])
	if len(before) > contextRunes {
		before = before[len(before)-contextRunes:]
	}
	after := []rune(text[endByte:])
	if len(after) > contextRunes {
		after = after[:contextRunes]
	}
	raw := string(before) + text[startByte:endByte] + string(after)
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return ""
	}
	snip := strings.Join(fields, " ")
	if len([]rune(text[:startByte])) > contextRunes {
		snip = "…" + snip
	}
	if len([]rune(text[endByte:])) > contextRunes {
		snip += "…"
	}
	return snip
}
