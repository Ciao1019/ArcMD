package editor

import (
	"fmt"
	"image"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/pdfdoc"
	"github.com/egoist/mygo/ui"
)

var (
	iconChevronLeft = lucide(`<path d="m15 18-6-6 6-6"/>`)
	iconRotateCW    = lucide(`<path d="M21 12a9 9 0 1 1-9-9c2.52 0 4.93 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/>`)
	iconSearchPDF   = lucide(`<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>`)
	iconMinusZoom   = lucide(`<path d="M5 12h14"/>`)
	iconPinPage     = lucide(`<path d="M12 17v5"/><path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z"/>`)
)

// PDFFitMode controls how PDF pages size themselves inside the viewport.
type PDFFitMode uint8

const (
	PDFFitWidth PDFFitMode = iota
	PDFFitPage
	PDFFreeZoom
)

const (
	maxPDFPageBitmaps  = 20
	maxPDFThumbBitmaps = 64
	pdfSidebarW        = float32(212)
	pdfOuterPad        = float32(24)
	pdfCompactOuterPad = float32(14)
	pdfPageGap         = float32(16)
	pdfThumbW          = 160
)

var pdfRenderSem = make(chan struct{}, 2)

type pdfBitmapKey struct {
	page    int
	widthPx int
	rotate  int
}

type pdfRenderedBitmap struct {
	bmp      *ui.Bitmap
	loadedAt time.Time
	released bool
}

type pdfRenderResult struct {
	key pdfBitmapKey
	img *image.RGBA
	err error
}

type pdfPageSlot struct {
	page int
	rect ui.Rect // content-space coordinates inside scroll box
}

type flatPDFOutline struct {
	title string
	page  int
	depth int
}

// PDFOptions configures PDFView rendering and host callbacks.
type PDFOptions struct {
	Theme       *ui.Theme
	Page        Page
	Title       string
	Compact     bool
	Async       bool // when true (live desktop window), rasterizes pages & searches asynchronously
	PinnedPage  int  // 1-based pinned page when > 0
	PinPage     func(page1Based int)
	ExpandModal func()
	OpenSplit   func(page1Based int)
	Done        func()
	RevealFile  func()
}

// PDFViewer holds immediate-mode UI state and bounded multi-resolution page
// caches for viewing a read-only PDF document smoothly at 60fps.
type PDFViewer struct {
	Doc           *pdfdoc.Document
	LoadErr       error
	RequestFocus  bool
	Scroll        ui.ScrollState
	ThumbScroll   ui.ScrollState
	OutlineScroll ui.ScrollState

	CurrentPage int // 0-based
	FitMode     PDFFitMode
	Zoom        float32 // 1.0 = 100% of fit-width base
	TwoPage     bool
	ExtraRotate int // 0, 90, 180, 270

	SidebarOpen bool
	SidebarTab  int // 0 = thumbnails, 1 = outline

	PageInputOpen  bool
	PageInputText  string
	PageInputFocus bool

	FindOpen      bool
	FindFocus     bool
	FindQuery     string
	findLastQuery string
	Matches       []pdfdoc.Match
	MatchIndex    int
	searching     bool
	searchSeq     uint64
	searchDone    chan pdfSearchResult

	pendingJumpPage int // >= 0 when waiting for layout geometry to scroll
	pendingInstant  bool
	suppressSyncTil time.Time

	outlinesFlat []flatPDFOutline
	lastViewW    float32
	lastViewH    float32
	slots        []pdfPageSlot
	totalContent float32

	mu       sync.Mutex
	pages    map[pdfBitmapKey]pdfRenderedBitmap
	pageLRU  []pdfBitmapKey
	inflight map[pdfBitmapKey]bool
	thumbs   map[pdfBitmapKey]*ui.Bitmap
	thumbLRU []pdfBitmapKey
	thumbRun map[pdfBitmapKey]bool
	results  chan pdfRenderResult
	thumbRes chan pdfRenderResult
	closed   atomic.Bool
}

type pdfSearchResult struct {
	seq     uint64
	query   string
	matches []pdfdoc.Match
}

// NewPDFViewer creates a viewer for doc (or records loadErr when doc could not open).
func NewPDFViewer(doc *pdfdoc.Document, loadErr error) *PDFViewer {
	v := &PDFViewer{
		Doc:             doc,
		LoadErr:         loadErr,
		FitMode:         PDFFitWidth,
		Zoom:            1.0,
		pendingJumpPage: -1,
		pages:           make(map[pdfBitmapKey]pdfRenderedBitmap, maxPDFPageBitmaps),
		inflight:        make(map[pdfBitmapKey]bool),
		thumbs:          make(map[pdfBitmapKey]*ui.Bitmap, maxPDFThumbBitmaps),
		thumbRun:        make(map[pdfBitmapKey]bool),
		results:         make(chan pdfRenderResult, 24),
		thumbRes:        make(chan pdfRenderResult, 32),
		searchDone:      make(chan pdfSearchResult, 4),
	}
	if doc != nil {
		v.outlinesFlat = flattenPDFOutlines(doc.Outlines(), 0)
		if len(v.outlinesFlat) > 0 {
			v.SidebarTab = 1
		}
	}
	return v
}

func flattenPDFOutlines(items []pdfdoc.OutlineItem, depth int) []flatPDFOutline {
	var out []flatPDFOutline
	for _, it := range items {
		out = append(out, flatPDFOutline{
			title: it.Title,
			page:  it.Page,
			depth: depth,
		})
		if len(it.Children) > 0 && depth < 8 {
			out = append(out, flattenPDFOutlines(it.Children, depth+1)...)
		}
	}
	return out
}

// Close cancels background rasterizers and releases cached bitmaps and native handles.
func (v *PDFViewer) Close() {
	if v == nil {
		return
	}
	v.closed.Store(true)
	v.mu.Lock()
	clear(v.pages)
	v.pageLRU = nil
	clear(v.thumbs)
	v.thumbLRU = nil
	v.mu.Unlock()
	if v.Doc != nil {
		v.Doc.Close()
	}
}

// ClearBitmaps drops cached GPU/CPU bitmaps without closing the underlying PDF
// (used when sharing or suspending background views).
func (v *PDFViewer) ClearBitmaps() {
	if v == nil {
		return
	}
	v.mu.Lock()
	clear(v.pages)
	v.pageLRU = nil
	clear(v.thumbs)
	v.thumbLRU = nil
	v.mu.Unlock()
}

// PageCount returns the number of pages in the PDF.
func (v *PDFViewer) PageCount() int {
	if v == nil || v.Doc == nil {
		return 0
	}
	return v.Doc.PageCount()
}

// PreviewBitmap returns a cached thumbnail bitmap of 0-based page p for hover cards.
func (v *PDFViewer) PreviewBitmap(p int) *ui.Bitmap {
	return v.thumbBitmap(p, nil)
}

// ToggleOutline toggles the left thumbnail / outline rail (⇧⌘O).
func (v *PDFViewer) ToggleOutline() {
	if v == nil || v.Doc == nil {
		return
	}
	v.SidebarOpen = !v.SidebarOpen
}

// OpenFind opens the top-right PDF search bar and focuses its input (⌘F).
func (v *PDFViewer) OpenFind() {
	if v == nil || v.Doc == nil {
		return
	}
	v.FindOpen = true
	v.FindFocus = true
}

// FindText opens the search bar with query and jumps to its first match.
func (v *PDFViewer) FindText(query string) {
	if v == nil || v.Doc == nil {
		return
	}
	v.FindQuery = query
	v.FindOpen = true
	v.FindFocus = true
	v.findLastQuery = ""
	v.runSearchSync(query)
}

// GoToPage scrolls smoothly (or immediately when instant is true) to 0-based page p.
func (v *PDFViewer) GoToPage(p int, instant bool) {
	if v == nil || v.Doc == nil || v.Doc.PageCount() == 0 {
		return
	}
	p = min(max(p, 0), v.Doc.PageCount()-1)
	v.CurrentPage = p
	v.pendingJumpPage = p
	v.pendingInstant = instant
}

// StepPage moves delta pages (-1 or +1).
func (v *PDFViewer) StepPage(delta int) {
	if v == nil || v.Doc == nil || v.Doc.PageCount() == 0 {
		return
	}
	step := delta
	if v.TwoPage && step != 0 {
		step *= 2
	}
	next := min(max(v.CurrentPage+step, 0), v.Doc.PageCount()-1)
	if next != v.CurrentPage {
		v.GoToPage(next, false)
	}
}

// SetZoom updates zoom clamped to [0.25, 4.0] and switches to free zoom mode.
func (v *PDFViewer) SetZoom(z float32) {
	if v == nil {
		return
	}
	clamped := min(max(z, 0.25), 4.0)
	if math.Abs(float64(clamped-v.Zoom)) < 0.001 && v.FitMode == PDFFreeZoom {
		return
	}
	prevMax := v.Scroll.MaxY
	frac := float32(0)
	if prevMax > 0 {
		frac = min(max(v.Scroll.Y/prevMax, 0), 1)
	}
	v.FitMode = PDFFreeZoom
	v.Zoom = clamped
	if prevMax > 0 {
		// Preserve approximate reading position across zoom changes.
		v.pendingJumpPage = v.CurrentPage
		v.pendingInstant = true
		_ = frac
	}
}

// RotateCW rotates all pages 90 degrees clockwise for the current session.
func (v *PDFViewer) RotateCW() {
	if v == nil {
		return
	}
	v.ExtraRotate = (v.ExtraRotate + 90) % 360
	v.pendingJumpPage = v.CurrentPage
	v.pendingInstant = true
}

func quantizePDFWidth(px int) int {
	px = min(max(px, 192), 3200)
	buckets := [...]int{256, 384, 512, 640, 768, 896, 1024, 1280, 1536, 1920, 2400, 3200}
	for _, b := range buckets {
		if px <= b {
			return b
		}
	}
	return 3200
}

func (v *PDFViewer) drainRenderResults(now time.Time) bool {
	changed := false
	for {
		select {
		case res := <-v.results:
			v.mu.Lock()
			delete(v.inflight, res.key)
			if res.err == nil && res.img != nil && !v.closed.Load() {
				bmp := ui.NewBitmap(res.img)
				v.putPageLocked(res.key, pdfRenderedBitmap{bmp: bmp, loadedAt: now})
				changed = true
			}
			v.mu.Unlock()
		default:
			goto drainThumbs
		}
	}
drainThumbs:
	for {
		select {
		case res := <-v.thumbRes:
			v.mu.Lock()
			delete(v.thumbRun, res.key)
			if res.err == nil && res.img != nil && !v.closed.Load() {
				v.putThumbLocked(res.key, ui.NewBitmap(res.img))
				changed = true
			}
			v.mu.Unlock()
		default:
			goto drainSearch
		}
	}
drainSearch:
	for {
		select {
		case sr := <-v.searchDone:
			if sr.seq == v.searchSeq && sr.query == v.FindQuery {
				v.searching = false
				v.Matches = sr.matches
				v.MatchIndex = 0
				if len(v.Matches) > 0 {
					v.GoToPage(v.Matches[0].Page, false)
				}
				changed = true
			}
		default:
			return changed
		}
	}
}

func (v *PDFViewer) releaseUploadedPixels(c *ui.Context) {
	now := c.Now()
	v.mu.Lock()
	defer v.mu.Unlock()
	needLater := false
	for k, ent := range v.pages {
		if ent.released || ent.bmp == nil || ent.loadedAt.IsZero() {
			continue
		}
		if wait := 250*time.Millisecond - now.Sub(ent.loadedAt); wait > 0 {
			needLater = true
			continue
		}
		ent.bmp.ReleasePixels()
		ent.released = true
		v.pages[k] = ent
	}
	if needLater {
		c.After(260 * time.Millisecond)
	}
}

func (v *PDFViewer) putPageLocked(k pdfBitmapKey, ent pdfRenderedBitmap) {
	v.pages[k] = ent
	if idx := slices.Index(v.pageLRU, k); idx >= 0 {
		v.pageLRU = slices.Delete(v.pageLRU, idx, idx+1)
	}
	v.pageLRU = append(v.pageLRU, k)
	for len(v.pages) > maxPDFPageBitmaps && len(v.pageLRU) > 0 {
		old := v.pageLRU[0]
		v.pageLRU = v.pageLRU[1:]
		delete(v.pages, old)
	}
}

func (v *PDFViewer) putThumbLocked(k pdfBitmapKey, bmp *ui.Bitmap) {
	v.thumbs[k] = bmp
	if idx := slices.Index(v.thumbLRU, k); idx >= 0 {
		v.thumbLRU = slices.Delete(v.thumbLRU, idx, idx+1)
	}
	v.thumbLRU = append(v.thumbLRU, k)
	for len(v.thumbs) > maxPDFThumbBitmaps && len(v.thumbLRU) > 0 {
		old := v.thumbLRU[0]
		v.thumbLRU = v.thumbLRU[1:]
		delete(v.thumbs, old)
	}
}

// pageBitmap returns the best available bitmap for (page, targetPx, rotate) and
// schedules an async high-DPI render when needed (or renders synchronously in
// headless tests where invalidate == nil).
func (v *PDFViewer) pageBitmap(page, targetPx int, now time.Time, invalidate func()) *ui.Bitmap {
	if v == nil || v.Doc == nil || v.closed.Load() {
		return nil
	}
	rot := ((v.ExtraRotate % 360) + 360) % 360
	bucket := quantizePDFWidth(targetPx)
	key := pdfBitmapKey{page: page, widthPx: bucket, rotate: rot}

	v.mu.Lock()
	if ent, ok := v.pages[key]; ok && ent.bmp != nil {
		if idx := slices.Index(v.pageLRU, key); idx >= 0 && idx != len(v.pageLRU)-1 {
			v.pageLRU = slices.Delete(v.pageLRU, idx, idx+1)
			v.pageLRU = append(v.pageLRU, key)
		}
		v.mu.Unlock()
		return ent.bmp
	}

	// Pick any existing resolution of this page & rotation so zooming never flashes blank.
	var fallback *ui.Bitmap
	bestDiff := int(1e9)
	for k, ent := range v.pages {
		if k.page == page && k.rotate == rot && ent.bmp != nil {
			diff := k.widthPx - bucket
			if diff < 0 {
				diff = -diff
			}
			if diff < bestDiff {
				bestDiff = diff
				fallback = ent.bmp
			}
		}
	}
	if fallback == nil {
		if th, ok := v.thumbs[pdfBitmapKey{page: page, widthPx: pdfThumbW, rotate: rot}]; ok {
			fallback = th
		}
	}

	if invalidate == nil {
		v.mu.Unlock()
		img, err := v.Doc.RenderPage(page, bucket, rot)
		if err != nil || img == nil {
			return fallback
		}
		bmp := ui.NewBitmap(img)
		v.mu.Lock()
		v.putPageLocked(key, pdfRenderedBitmap{bmp: bmp, loadedAt: now})
		v.mu.Unlock()
		return bmp
	}

	if !v.inflight[key] {
		v.inflight[key] = true
		doc := v.Doc
		go func() {
			pdfRenderSem <- struct{}{}
			defer func() { <-pdfRenderSem }()
			if v.closed.Load() {
				return
			}
			img, err := doc.RenderPage(page, bucket, rot)
			if v.closed.Load() {
				return
			}
			select {
			case v.results <- pdfRenderResult{key: key, img: img, err: err}:
			default:
			}
			invalidate()
		}()
	}
	v.mu.Unlock()
	return fallback
}

func (v *PDFViewer) thumbBitmap(page int, invalidate func()) *ui.Bitmap {
	if v == nil || v.Doc == nil || v.closed.Load() {
		return nil
	}
	rot := ((v.ExtraRotate % 360) + 360) % 360
	key := pdfBitmapKey{page: page, widthPx: pdfThumbW, rotate: rot}

	v.mu.Lock()
	if bmp, ok := v.thumbs[key]; ok && bmp != nil {
		v.mu.Unlock()
		return bmp
	}
	if invalidate == nil {
		v.mu.Unlock()
		img, err := v.Doc.RenderPage(page, pdfThumbW, rot)
		if err != nil || img == nil {
			return nil
		}
		bmp := ui.NewBitmap(img)
		v.mu.Lock()
		v.putThumbLocked(key, bmp)
		v.mu.Unlock()
		return bmp
	}
	if !v.thumbRun[key] {
		v.thumbRun[key] = true
		doc := v.Doc
		go func() {
			pdfRenderSem <- struct{}{}
			defer func() { <-pdfRenderSem }()
			if v.closed.Load() {
				return
			}
			img, err := doc.RenderPage(page, pdfThumbW, rot)
			if v.closed.Load() {
				return
			}
			select {
			case v.thumbRes <- pdfRenderResult{key: key, img: img, err: err}:
			default:
			}
			invalidate()
		}()
	}
	v.mu.Unlock()
	return nil
}

func (v *PDFViewer) runSearchSync(q string) {
	v.searchSeq++
	v.searching = false
	if strings.TrimSpace(q) == "" || v.Doc == nil {
		v.Matches = nil
		v.MatchIndex = 0
		return
	}
	v.Matches = v.Doc.Find(q)
	v.MatchIndex = 0
	if len(v.Matches) > 0 {
		v.GoToPage(v.Matches[0].Page, false)
	}
}

func (v *PDFViewer) triggerSearch(q string, invalidate func()) {
	v.findLastQuery = q
	if strings.TrimSpace(q) == "" || v.Doc == nil {
		v.searchSeq++
		v.searching = false
		v.Matches = nil
		v.MatchIndex = 0
		return
	}
	if invalidate == nil || v.Doc.PageCount() <= 12 {
		v.runSearchSync(q)
		return
	}
	v.searchSeq++
	seq := v.searchSeq
	v.searching = true
	doc := v.Doc
	go func() {
		if v.closed.Load() {
			return
		}
		matches := doc.Find(q)
		if v.closed.Load() {
			return
		}
		select {
		case v.searchDone <- pdfSearchResult{seq: seq, query: q, matches: matches}:
		default:
		}
		invalidate()
	}()
}

func (v *PDFViewer) stepMatch(delta int) {
	if len(v.Matches) == 0 {
		return
	}
	n := len(v.Matches)
	v.MatchIndex = ((v.MatchIndex+delta)%n + n) % n
	v.GoToPage(v.Matches[v.MatchIndex].Page, false)
}

func (v *PDFViewer) computeSlots(availW, availH float32, compact bool) {
	cnt := v.PageCount()
	v.slots = v.slots[:0]
	if cnt == 0 || availW <= 0 {
		v.totalContent = 0
		return
	}
	pad := pdfOuterPad
	if compact {
		pad = pdfCompactOuterPad
	}
	innerW := max(120, availW-2*pad)
	innerH := max(120, availH-2*pad-48)

	twoPage := v.TwoPage && !compact && innerW >= 460 && cnt > 1
	y := pad

	sizePage := func(idx int, colW float32) (float32, float32) {
		info := v.Doc.PageInfoRotated(idx, v.ExtraRotate)
		aspect := float32(info.Aspect())
		if aspect <= 0 {
			aspect = 595.28 / 841.89
		}
		var w, h float32
		switch v.FitMode {
		case PDFFitPage:
			h = innerH
			w = h * aspect
			if w > colW {
				w = colW
				h = w / aspect
			}
		case PDFFreeZoom:
			baseW := min(colW, float32(860))
			w = baseW * v.Zoom
			h = w / aspect
		default: // PDFFitWidth
			w = min(colW, float32(860))
			h = w / aspect
		}
		return max(48, w), max(48, h)
	}

	if twoPage {
		colW := max(80, (innerW-pdfPageGap)/2)
		for i := 0; i < cnt; i += 2 {
			w0, h0 := sizePage(i, colW)
			if i+1 < cnt {
				w1, h1 := sizePage(i+1, colW)
				rowH := max(h0, h1)
				totalPairW := w0 + pdfPageGap + w1
				startX := max(pad, (availW-totalPairW)/2)
				v.slots = append(v.slots,
					pdfPageSlot{page: i, rect: ui.Rect{X: startX, Y: y + (rowH-h0)/2, W: w0, H: h0}},
					pdfPageSlot{page: i + 1, rect: ui.Rect{X: startX + w0 + pdfPageGap, Y: y + (rowH-h1)/2, W: w1, H: h1}},
				)
				y += rowH + pdfPageGap
			} else {
				startX := max(pad, (availW-w0)/2)
				v.slots = append(v.slots, pdfPageSlot{page: i, rect: ui.Rect{X: startX, Y: y, W: w0, H: h0}})
				y += h0 + pdfPageGap
			}
		}
	} else {
		for i := range cnt {
			w, h := sizePage(i, innerW)
			startX := max(pad, (availW-w)/2)
			v.slots = append(v.slots, pdfPageSlot{page: i, rect: ui.Rect{X: startX, Y: y, W: w, H: h}})
			y += h + pdfPageGap
		}
	}
	v.totalContent = y + pad + 36 // extra room for floating bottom bar
}

// PDFView renders a read-only interactive PDF reader inside the current UI parent.
func PDFView(c *ui.Context, v *PDFViewer, opts PDFOptions) ui.Element {
	t := opts.Theme
	if t == nil {
		t = c.Theme()
	}
	pg := opts.Page
	wb := pg.Whiteboard

	root := ui.Row(c.Key("pdf-root")).Grow(1).WidthPercent(100).HeightPercent(100).MinWidth(0).MinHeight(0).Clip()
	if v == nil || v.LoadErr != nil || v.Doc == nil || v.Doc.PageCount() == 0 {
		root.Center().Background(t.Background).Children(func() {
			card := ui.Column(c.Key("pdf-err")).MaxWidth(420).Padding(24).Gap(10).Radius(12).
				Border(1, pg.TableLine).AlignItems(ui.Center)
			card.Children(func() {
				ui.Icon(c, iconFileDoc).FontSize(28).TextColor(t.TextMuted)
				title := i18n.T("无法打开 PDF 文档")
				detail := i18n.T("文件可能已损坏或格式不受支持。")
				if v != nil && v.LoadErr != nil {
					if strings.Contains(v.LoadErr.Error(), "password-protected") {
						title = i18n.T("此 PDF 受密码保护")
						detail = i18n.T("当前暂不支持带密码锁定的加密 PDF 预览，可在系统阅读器中打开。")
					} else {
						detail = v.LoadErr.Error()
					}
				}
				ui.Text(c, title).FontSize(15).FontWeight(600).TextColor(t.Text)
				ui.Text(c, detail).FontSize(13).TextColor(t.TextMuted)
				if opts.RevealFile != nil {
					btn := ui.ButtonBase(c.Key("pdf-reveal-err")).Height(30).PaddingX(12).Radius(8).
						Border(1, pg.TableLine).Center().Cursor(ui.CursorPointer)
					if btn.Hovered() {
						btn.Background(pg.Hover)
					}
					btn.Children(func() {
						ui.Text(c, i18n.T("在访达中显示")).FontSize(12.5).TextColor(t.Text)
					})
					if btn.Clicked() {
						opts.RevealFile()
					}
				}
			})
		})
		return root
	}

	services := c.Services()
	var invalidate func()
	if opts.Async && !c.Instant() && services != (ui.Services{}) {
		invalidate = services.Invalidate
	}
	v.drainRenderResults(c.Now())
	v.releaseUploadedPixels(c)

	if v.FindOpen && v.FindQuery != v.findLastQuery {
		v.triggerSearch(v.FindQuery, invalidate)
	}

	deskBg := t.Background.Mix(t.Text, 0.035)
	root.Background(deskBg)

	// Reader-wide shortcuts work whether focus is on the viewport or any toolbar button.
	if !opts.Compact && root.Shortcut(ui.Cmd|ui.Shift, ui.KeyO) {
		v.ToggleOutline()
	}
	if root.Shortcut(ui.Cmd, ui.KeyF) {
		v.OpenFind()
	}
	if v.FindOpen {
		if root.Shortcut(ui.Cmd, ui.KeyG) {
			v.stepMatch(1)
		} else if root.Shortcut(ui.Cmd|ui.Shift, ui.KeyG) {
			v.stepMatch(-1)
		}
	}
	if root.Shortcut(ui.Cmd, ui.KeyEqual) {
		v.SetZoom(v.effectiveZoom() * 1.15)
	}
	if root.Shortcut(ui.Cmd, ui.KeyMinus) {
		v.SetZoom(v.effectiveZoom() / 1.15)
	}
	if root.Shortcut(ui.Cmd, ui.Key0) {
		v.FitMode = PDFFitWidth
		v.Zoom = 1.0
	}
	if !v.FindOpen && !v.PageInputOpen {
		if root.Shortcut(0, ui.KeyLeft) || root.Shortcut(0, ui.KeyPageUp) {
			v.StepPage(-1)
		}
		if root.Shortcut(0, ui.KeyRight) || root.Shortcut(0, ui.KeyPageDown) {
			v.StepPage(1)
		}
		if root.Shortcut(0, ui.KeyHome) || root.Shortcut(ui.Cmd, ui.KeyUp) {
			v.GoToPage(0, false)
		}
		if root.Shortcut(0, ui.KeyEnd) || root.Shortcut(ui.Cmd, ui.KeyDown) {
			v.GoToPage(v.PageCount()-1, false)
		}
	}

	root.Children(func() {
		if v.SidebarOpen && !opts.Compact {
			v.sidebarView(c, t, pg, invalidate)
		}

		stage := ui.Column(c.Key("pdf-stage")).Grow(1).HeightPercent(100).MinWidth(0).MinHeight(0)
		stageBounds := stage.LayoutBounds()
		availW, availH := stageBounds.W, stageBounds.H
		if availW <= 0 {
			winW, winH := c.Size()
			availW, availH = max(320, winW-260), max(240, winH-80)
			if v.SidebarOpen && !opts.Compact {
				availW = max(240, availW-pdfSidebarW)
			}
			c.After(0)
		}
		prevW := v.lastViewW
		hasResizeAnchor := false
		resizeFrac := float32(0)
		if prevW > 0 && math.Abs(float64(availW-prevW)) > 1.5 && v.pendingJumpPage < 0 && v.CurrentPage >= 0 && v.CurrentPage < len(v.slots) {
			oldSlot := v.slots[v.CurrentPage]
			if v.CurrentPage > 0 || v.Scroll.Y > 4 {
				hasResizeAnchor = true
				resizeFrac = min(max((v.Scroll.Y-oldSlot.rect.Y)/max(1, oldSlot.rect.H), -0.08), 0.95)
			}
		}
		v.lastViewW, v.lastViewH = availW, availH
		v.computeSlots(availW, availH, opts.Compact)
		if hasResizeAnchor && v.CurrentPage < len(v.slots) {
			newSlot := v.slots[v.CurrentPage]
			v.Scroll.Y = max(0, newSlot.rect.Y+resizeFrac*newSlot.rect.H)
			v.suppressSyncTil = c.Now().Add(150 * time.Millisecond)
		}

		// Apply pending page jump once layout geometry and scroll bounds are ready.
		if v.pendingJumpPage >= 0 && v.pendingJumpPage < len(v.slots) {
			if stageBounds.W <= 0 || (v.pendingJumpPage > 0 && v.Scroll.MaxY <= 0 && v.totalContent > availH+4) {
				c.After(0)
			} else {
				v.CurrentPage = v.pendingJumpPage
				targetSlot := v.slots[v.pendingJumpPage]
				targetY := max(0, targetSlot.rect.Y-pdfOuterPad)
				v.suppressSyncTil = c.Now().Add(320 * time.Millisecond)
				if v.pendingInstant || c.Preferences().ReduceMotion {
					v.Scroll.Y = targetY
				} else {
					v.Scroll.ScrollTo(v.Scroll.X, targetY, ui.Animation{Duration: 240 * time.Millisecond})
				}
				v.pendingJumpPage = -1
				v.pendingInstant = false
			}
		}

		stage.Children(func() {
			scroll := ui.ScrollBoth(c.Key("pdf-scroll")).Grow(1).MinWidth(0).MinHeight(0).
				TrackScroll(&v.Scroll).Focusable().FocusRing(false).Role(ui.RoleRegion).
				Label(i18n.T("PDF 阅读器"))
			if v.RequestFocus {
				scroll.Focus(ui.FocusOptions{PreventScroll: true})
				v.RequestFocus = false
			}
			// Sync CurrentPage after TrackScroll has stepped any ScrollTo glide for this frame.
			if v.pendingJumpPage < 0 && !v.Scroll.Scrolling() && c.Now().After(v.suppressSyncTil) && len(v.slots) > 0 {
				keepCur := false
				if v.CurrentPage >= 0 && v.CurrentPage < len(v.slots) {
					curRect := v.slots[v.CurrentPage].rect
					visTop, visBot := v.Scroll.Y, v.Scroll.Y+availH
					overlap := min(curRect.Y+curRect.H, visBot) - max(curRect.Y, visTop)
					if overlap >= min(curRect.H*0.65, availH*0.45) {
						keepCur = true
					}
				}
				if !keepCur {
					focalY := v.Scroll.Y + max(availH*0.36, 48)
					best := v.CurrentPage
					for _, sl := range v.slots {
						if focalY >= sl.rect.Y && focalY <= sl.rect.Y+sl.rect.H+pdfPageGap {
							best = sl.page
							break
						}
						if sl.rect.Y <= focalY {
							best = sl.page
						}
					}
					v.CurrentPage = best
				}
			}

			scroll.HandleInput(func(ev ui.InputEvent) bool {
				return v.handleViewportInput(ev, availH, opts)
			})
			scroll.ContextMenu(func(m *ui.Menu) {
				v.buildContextMenu(m, opts)
			})

			//Compute max content width for horizontal scroll when zoomed in > 100%.
			maxContentW := availW
			for _, sl := range v.slots {
				if right := sl.rect.X + sl.rect.W + pdfOuterPad; right > maxContentW {
					maxContentW = right
				}
			}

			scroll.Children(func() {
				canvas := ui.Box(c.Key("pdf-canvas")).Size(maxContentW, max(availH, v.totalContent)).Shrink(0)
				if canvas.Pressed() && !v.FindOpen && !v.PageInputOpen {
					scroll.Focus(ui.FocusOptions{PreventScroll: true})
				}
				canvas.Draw(func(p *ui.Painter, bounds ui.Rect) {
					v.paintPages(p, bounds, availH, t, pg, invalidate)
				})
			})

			if v.FindOpen {
				v.findBarView(c, t, pg, invalidate)
			}
			v.floatingIslandView(c, t, pg, wb, availW, opts)
		})
	})

	return root
}

func (v *PDFViewer) paintPages(p *ui.Painter, bounds ui.Rect, viewH float32, t *ui.Theme, pg Page, invalidate func()) {
	dpi := max(p.Scale(), 1.0)
	overscan := max(viewH*0.85, 360)
	visTop := v.Scroll.Y - overscan
	visBot := v.Scroll.Y + max(viewH, 480) + overscan

	paperFill := ui.RGB(255, 255, 255)
	paperBorder := ui.RGBA(0, 0, 0, 0.08)
	shadowCol := ui.RGBA(0, 0, 0, 0.11)
	matchFill := ui.RGBA(250, 204, 21, 0.30)
	activeFill := ui.RGBA(249, 115, 22, 0.26)
	activeStroke := ui.RGBA(234, 88, 12, 0.85)

	for _, sl := range v.slots {
		if sl.rect.Y+sl.rect.H < visTop || sl.rect.Y > visBot {
			continue
		}
		dst := ui.Rect{
			X: bounds.X + sl.rect.X,
			Y: bounds.Y + sl.rect.Y,
			W: sl.rect.W,
			H: sl.rect.H,
		}
		p.Shadow(dst, 5, 0, 4, 16, 0, shadowCol)
		p.Fill(dst, paperFill, 5)

		targetPx := int(math.Ceil(float64(sl.rect.W * dpi)))
		bmp := v.pageBitmap(sl.page, targetPx, p.Now(), invalidate)
		if bmp != nil {
			p.Clip(dst, 5, func() {
				p.Image(bmp, dst, ui.Contain)
			})
		} else {
			// Quiet skeleton shimmer lines while background worker decodes first frame.
			lineCol := ui.RGBA(0, 0, 0, 0.055)
			mx := dst.X + dst.W*0.1
			mw := dst.W * 0.8
			for row := range 6 {
				ly := dst.Y + dst.H*(0.12+float32(row)*0.09)
				rw := mw
				if row == 0 {
					rw = mw * 0.55
				} else if row == 5 {
					rw = mw * 0.68
				}
				p.Fill(ui.Rect{X: mx, Y: ly, W: rw, H: max(6, dst.H*0.016)}, lineCol, 3)
			}
		}
		p.Stroke(dst, paperBorder, 5, 1)

		// Highlight in-PDF search matches on this page.
		if v.FindOpen && len(v.Matches) > 0 {
			for mi, m := range v.Matches {
				if m.Page != sl.page {
					continue
				}
				active := mi == v.MatchIndex
				fill := matchFill
				if active {
					fill = activeFill
				}
				if len(m.Rects) > 0 {
					for _, nr := range m.Rects {
						rr := pdfdoc.RotateNormRect(nr, v.ExtraRotate)
						hr := ui.Rect{
							X: dst.X + float32(rr.X)*dst.W - 2,
							Y: dst.Y + float32(rr.Y)*dst.H - 1.5,
							W: float32(rr.W)*dst.W + 4,
							H: float32(rr.H)*dst.H + 3,
						}
						p.Fill(hr, fill, 3)
						if active {
							p.Stroke(hr, activeStroke, 3, 1.5)
						}
					}
				} else if active {
					// Fallback page-level ring when native character boxes aren't available.
					p.Stroke(dst, activeStroke, 5, 2)
				}
			}
		}
	}
	_ = t
	_ = pg
}

func (v *PDFViewer) handleViewportInput(ev ui.InputEvent, viewH float32, opts PDFOptions) bool {
	if ev.Kind == ui.InputScroll && ev.Mods&ui.Cmd != 0 {
		// ⌘ + wheel zooms smoothly.
		base := v.effectiveZoom()
		delta := float32(-ev.DY) * 0.0025
		if delta == 0 {
			delta = float32(ev.DX) * 0.0025
		}
		if delta != 0 {
			v.SetZoom(base * float32(math.Exp(float64(delta))))
			return true
		}
	}
	if ev.Kind != ui.InputKeyDown {
		return false
	}
	shift := ev.Mods&ui.Shift != 0
	switch ev.Key {
	case ui.KeyEscape:
		if v.PageInputOpen {
			v.PageInputOpen = false
			return true
		}
		if v.FindOpen {
			v.FindOpen = false
			return true
		}
		if opts.Done != nil {
			opts.Done()
			return true
		}
	case ui.KeyUp:
		v.Scroll.Y = max(0, v.Scroll.Y-72)
		return true
	case ui.KeyDown:
		v.Scroll.Y = min(v.Scroll.MaxY, v.Scroll.Y+72)
		return true
	case ui.KeySpace:
		step := max(160, viewH*0.82)
		if shift {
			v.Scroll.ScrollTo(v.Scroll.X, max(0, v.Scroll.Y-step), ui.Animation{Duration: 180 * time.Millisecond})
		} else {
			v.Scroll.ScrollTo(v.Scroll.X, min(v.Scroll.MaxY, v.Scroll.Y+step), ui.Animation{Duration: 180 * time.Millisecond})
		}
		return true
	}
	return false
}

func (v *PDFViewer) effectiveZoom() float32 {
	if v.FitMode == PDFFreeZoom {
		return v.Zoom
	}
	return 1.0
}

func (v *PDFViewer) zoomPercentLabel() string {
	if v.FitMode == PDFFitPage {
		return i18n.T("整页")
	}
	return fmt.Sprintf("%d%%", int(math.Round(float64(v.effectiveZoom()*100))))
}

func (v *PDFViewer) buildContextMenu(m *ui.Menu, opts PDFOptions) {
	menuAction(m, i18n.T("查找…"), func() { v.OpenFind() })
	if !opts.Compact {
		label := i18n.T("展开缩略图与目录")
		if v.SidebarOpen {
			label = i18n.T("收起缩略图与目录")
		}
		menuAction(m, label, func() { v.ToggleOutline() })
	}
	m.Separator()
	menuAction(m, i18n.T("适应宽度"), func() {
		v.FitMode = PDFFitWidth
		v.Zoom = 1.0
	})
	menuAction(m, i18n.T("适应整页"), func() {
		v.FitMode = PDFFitPage
	})
	if !opts.Compact {
		spreadLabel := i18n.T("双页并排视图")
		if v.TwoPage {
			spreadLabel = i18n.T("单页连续视图")
		}
		menuAction(m, spreadLabel, func() {
			v.TwoPage = !v.TwoPage
			v.pendingJumpPage = v.CurrentPage
			v.pendingInstant = true
		})
	}
	menuAction(m, i18n.T("顺时针旋转 90°"), func() { v.RotateCW() })
	if opts.OpenSplit != nil {
		m.Separator()
		menuAction(m, i18n.T("在分屏中阅读"), func() { opts.OpenSplit(v.CurrentPage + 1) })
	}
	if opts.RevealFile != nil {
		m.Separator()
		menuAction(m, i18n.T("在访达中显示"), opts.RevealFile)
	}
}

func (v *PDFViewer) sidebarView(c *ui.Context, t *ui.Theme, pg Page, invalidate func()) {
	rail := ui.Column(c.Key("pdf-sidebar")).Width(pdfSidebarW).HeightPercent(100).Shrink(0).MinHeight(0).
		Background(t.Background).BorderWidth(0, 1, 0, 0).BorderColor(pg.TableLine)
	rail.Children(func() {
		hasOutlines := len(v.outlinesFlat) > 0
		hdr := ui.Row(c.Key("pdf-side-hdr")).Height(40).PaddingX(10).Gap(4).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(pg.TableLine)
		hdr.Children(func() {
			if hasOutlines {
				for idx, name := range []string{i18n.T("缩略图"), i18n.T("目录")} {
					active := v.SidebarTab == idx
					tab := ui.ButtonBase(c.Key(fmt.Sprintf("pdf-tab:%d", idx))).Height(26).PaddingX(10).Radius(6).
						Center().Cursor(ui.CursorPointer)
					if active {
						tab.Background(t.Accent.Alpha(0.14))
					} else if tab.Hovered() {
						tab.Background(pg.Hover)
					}
					fg := t.TextMuted
					weight := 450
					if active {
						fg = t.Accent
						weight = 600
					}
					tab.Children(func() {
						ui.Text(c, name).FontSize(12.5).FontWeight(weight).TextColor(fg)
					})
					if tab.Clicked() {
						v.SidebarTab = idx
					}
				}
			} else {
				ui.Text(c, fmt.Sprintf(i18n.T("缩略图 · %d 页"), v.PageCount())).
					FontSize(12.5).FontWeight(600).TextColor(t.TextMuted).PaddingX(4)
			}
			ui.Box(c).Grow(1)
			closeBtn := ui.ButtonBase(c.Key("pdf-side-close")).Size(24, 24).Radius(6).Center().
				Cursor(ui.CursorPointer).Tooltip(i18n.T("收起 (⇧⌘O)"))
			if closeBtn.Hovered() {
				closeBtn.Background(pg.Hover)
			}
			closeBtn.Children(func() {
				ui.Icon(c, iconChevronsLeft).FontSize(14).TextColor(t.TextMuted)
			})
			if closeBtn.Clicked() {
				v.SidebarOpen = false
			}
		})

		if hasOutlines && v.SidebarTab == 1 {
			ui.Scroll(c.Key("pdf-outline-scroll")).Grow(1).MinHeight(0).TrackScroll(&v.OutlineScroll).Children(func() {
				list := ui.Column(c.Key("pdf-outline-list")).Padding(8).Gap(2)
				list.Children(func() {
					for i, item := range v.outlinesFlat {
						active := item.page == v.CurrentPage
						row := ui.Row(c.Key(fmt.Sprintf("pdf-out:%d", i))).Height(30).
							Padding(0, 8, 0, float32(8+item.depth*12)).Gap(6).Radius(7).
							AlignItems(ui.Center).Cursor(ui.CursorPointer).Tooltip(item.title)
						if active {
							row.Background(t.Accent.Alpha(0.13))
						} else if row.Hovered() {
							row.Background(pg.Hover)
						}
						fg := t.Text
						weight := 400
						if active {
							fg = t.Accent
							weight = 550
						}
						row.Children(func() {
							ui.Text(c, item.title).FontSize(13).FontWeight(weight).TextColor(fg).
								Grow(1).MinWidth(0).MaxLines(1)
							if item.page >= 0 {
								ui.Text(c, strconv.Itoa(item.page+1)).FontSize(11).TextColor(t.TextMuted)
							}
						})
						if row.Clicked() && item.page >= 0 {
							v.GoToPage(item.page, false)
						}
					}
				})
			})
			return
		}

		// Virtualized thumbnail strip.
		const itemH = float32(174)
		cnt := v.PageCount()
		ui.Scroll(c.Key("pdf-thumb-scroll")).Grow(1).MinHeight(0).TrackScroll(&v.ThumbScroll).Children(func() {
			totalH := float32(cnt)*itemH + 16
			wrap := ui.Box(c.Key("pdf-thumb-wrap")).Height(totalH).Shrink(0)
			visTop := max(0, v.ThumbScroll.Y-itemH*2)
			visBot := v.ThumbScroll.Y + max(v.lastViewH, 600) + itemH*2
			first := max(0, int(visTop/itemH))
			last := min(cnt-1, int(visBot/itemH)+1)
			wrap.Children(func() {
				for idx := first; idx <= last; idx++ {
					pageIdx := idx
					active := pageIdx == v.CurrentPage
					info := v.Doc.PageInfoRotated(pageIdx, v.ExtraRotate)
					aspect := float32(info.Aspect())
					boxW, boxH := float32(118), float32(132)
					if aspect > boxW/boxH {
						boxH = max(44, boxW/aspect)
					} else {
						boxW = max(44, boxH*aspect)
					}

					cell := ui.Column(c.Key(fmt.Sprintf("pdf-th:%d", pageIdx))).
						Absolute().Left(10).Top(8+float32(pageIdx)*itemH).Size(pdfSidebarW-20, itemH-6).
						Radius(9).AlignItems(ui.Center).Justify(ui.Center).Gap(6).
						Cursor(ui.CursorPointer)
					if active {
						cell.Background(t.Accent.Alpha(0.11))
					} else if cell.Hovered() {
						cell.Background(pg.Hover)
					}

					bmp := v.thumbBitmap(pageIdx, invalidate)
					cell.Children(func() {
						sheet := ui.Box(c.Key("sheet")).Size(boxW, boxH).Radius(4).
							Background(ui.RGB(255, 255, 255)).Shadow(0, 2, 6, 0, ui.RGBA(0, 0, 0, 0.11))
						if active {
							sheet.Border(2, t.Accent)
						} else {
							sheet.Border(1, ui.RGBA(0, 0, 0, 0.10))
						}
						sheet.Draw(func(p *ui.Painter, r ui.Rect) {
							if bmp != nil {
								p.Clip(r, 4, func() {
									p.Image(bmp, r, ui.Contain)
								})
							}
						})
						numFg := t.TextMuted
						numWeight := 450
						if active {
							numFg = t.Accent
							numWeight = 600
						}
						ui.Text(c, strconv.Itoa(pageIdx+1)).FontSize(11.5).FontWeight(numWeight).TextColor(numFg)
					})
					if cell.Clicked() {
						v.GoToPage(pageIdx, false)
					}
				}
			})
		})
	})
}

func (v *PDFViewer) findBarView(c *ui.Context, t *ui.Theme, pg Page, invalidate func()) {
	bar := ui.Row(c.Key("pdf-find-bar")).Absolute().Right(16).Top(12).Width(348).Height(38).
		PaddingX(10).Gap(6).Radius(10).AlignItems(ui.Center).
		Background(pg.Popover).Border(1, pg.TableLine).Shadow(0, 6, 20, 0, pg.MathShadow)

	bar.Children(func() {
		ui.Icon(c, iconSearchPDF).FontSize(14).TextColor(t.TextMuted)
		in := ui.TextInput(c.Key("pdf-find-input"), &v.FindQuery).
			Placeholder(i18n.T("在 PDF 中查找…")).Label(i18n.T("在 PDF 中查找")).
			FontSize(13).Padding(4, 4).Grow(1).MinWidth(0)
		if v.FindFocus {
			in.Focus()
			v.FindFocus = false
		}
		in.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputKeyDown {
				return false
			}
			switch ev.Key {
			case ui.KeyEnter:
				if v.FindQuery != v.findLastQuery {
					v.triggerSearch(v.FindQuery, invalidate)
				} else if ev.Mods&ui.Shift != 0 {
					v.stepMatch(-1)
				} else {
					v.stepMatch(1)
				}
				return true
			case ui.KeyEscape:
				v.FindOpen = false
				v.RequestFocus = true
				return true
			}
			return false
		})

		if strings.TrimSpace(v.FindQuery) != "" {
			status := i18n.T("无结果")
			if v.searching {
				status = "…"
			} else if len(v.Matches) > 0 {
				status = fmt.Sprintf("%d/%d", v.MatchIndex+1, len(v.Matches))
			}
			ui.Text(c, status).FontSize(11.5).TextColor(t.TextMuted).NoWrap()
		}

		prevBtn := pdfIconBtn(c, "pdf-find-prev", iconArrowUp, i18n.T("上一个匹配 (⇧Enter)"), len(v.Matches) == 0, pg, t)
		if prevBtn.Clicked() {
			v.stepMatch(-1)
		}
		nextBtn := pdfIconBtn(c, "pdf-find-next", iconArrowDown, i18n.T("下一个匹配 (Enter)"), len(v.Matches) == 0, pg, t)
		if nextBtn.Clicked() {
			v.stepMatch(1)
		}
		closeBtn := pdfIconBtn(c, "pdf-find-close", iconClose, i18n.T("关闭 (Esc)"), false, pg, t)
		if closeBtn.Clicked() {
			v.FindOpen = false
			v.RequestFocus = true
		}
	})
}

func (v *PDFViewer) floatingIslandView(c *ui.Context, t *ui.Theme, pg Page, wb WhiteboardStyle, availW float32, opts PDFOptions) {
	cnt := v.PageCount()
	bg := wb.IslandFill
	border := wb.IslandBorder
	if bg.A == 0 {
		bg = pg.Popover
		border = pg.TableLine
	}

	holder := ui.Row(c.Key("pdf-island-wrap")).Absolute().Left(0).Right(0).Bottom(14).
		Justify(ui.Center).PassThrough()
	holder.Children(func() {
		island := ui.Row(c.Key("pdf-island")).Height(36).PaddingX(8).Gap(3).Radius(11).
			AlignItems(ui.Center).Background(bg).Border(1, border).
			Shadow(0, 6, 22, 0, pg.MathShadow)

		island.Children(func() {
			if !opts.Compact {
				sideBtn := pdfIconBtnActive(c, "pdf-btn-side", iconOutline, i18n.T("缩略图与目录 (⇧⌘O)"), false, v.SidebarOpen, pg, t)
				if sideBtn.Clicked() {
					v.ToggleOutline()
				}
				pdfIslandDivider(c, pg)
			}

			prev := pdfIconBtn(c, "pdf-btn-prev", iconChevronLeft, i18n.T("上一页 (←)"), v.CurrentPage <= 0, pg, t)
			if prev.Clicked() {
				v.StepPage(-1)
			}

			if v.PageInputOpen {
				pin := ui.TextInput(c.Key("pdf-page-in"), &v.PageInputText).Width(56).FontSize(12.5).
					Padding(2, 6).Label(i18n.T("跳转到页码"))
				if v.PageInputFocus {
					pin.Focus()
					v.PageInputFocus = false
				}
				pin.HandleInput(func(ev ui.InputEvent) bool {
					if ev.Kind != ui.InputKeyDown {
						return false
					}
					switch ev.Key {
					case ui.KeyEnter:
						if pNum, err := strconv.Atoi(strings.TrimSpace(v.PageInputText)); err == nil {
							v.GoToPage(pNum-1, false)
						}
						v.PageInputOpen = false
						v.RequestFocus = true
						return true
					case ui.KeyEscape:
						v.PageInputOpen = false
						v.RequestFocus = true
						return true
					}
					return false
				})
			} else {
				pagePill := ui.ButtonBase(c.Key("pdf-page-pill")).Height(26).PaddingX(8).Radius(7).
					Center().Cursor(ui.CursorPointer).Tooltip(i18n.T("点击输入页码跳转"))
				if pagePill.Hovered() {
					pagePill.Background(pg.Hover)
				}
				pagePill.Children(func() {
					ui.Text(c, fmt.Sprintf("%d / %d", v.CurrentPage+1, cnt)).
						FontSize(12.5).FontWeight(550).TextColor(t.Text).NoWrap()
				})
				if pagePill.Clicked() {
					v.PageInputOpen = true
					v.PageInputFocus = true
					v.PageInputText = strconv.Itoa(v.CurrentPage + 1)
				}
			}

			next := pdfIconBtn(c, "pdf-btn-next", iconChevronRight, i18n.T("下一页 (→)"), v.CurrentPage >= cnt-1, pg, t)
			if next.Clicked() {
				v.StepPage(1)
			}

			pdfIslandDivider(c, pg)

			zOut := pdfIconBtn(c, "pdf-btn-zout", iconMinusZoom, i18n.T("缩小 (⌘-)"), false, pg, t)
			if zOut.Clicked() {
				v.SetZoom(v.effectiveZoom() / 1.2)
			}

			zReset := ui.ButtonBase(c.Key("pdf-btn-zpct")).Height(26).PaddingX(7).Radius(7).
				Center().Cursor(ui.CursorPointer).Tooltip(i18n.T("重置为适应宽度 (⌘0)"))
			if zReset.Hovered() {
				zReset.Background(pg.Hover)
			}
			zReset.Children(func() {
				ui.Text(c, v.zoomPercentLabel()).FontSize(12).FontWeight(500).TextColor(t.TextMuted).NoWrap()
			})
			if zReset.Clicked() {
				v.FitMode = PDFFitWidth
				v.Zoom = 1.0
			}

			zIn := pdfIconBtn(c, "pdf-btn-zin", iconPlus, i18n.T("放大 (⌘+)"), false, pg, t)
			if zIn.Clicked() {
				v.SetZoom(v.effectiveZoom() * 1.2)
			}

			if availW >= 420 {
				fitTip := i18n.T("适应整页")
				if v.FitMode == PDFFitPage {
					fitTip = i18n.T("适应宽度")
				}
				fitBtn := pdfIconBtnActive(c, "pdf-btn-fit", iconFit, fitTip, false, v.FitMode == PDFFitPage, pg, t)
				if fitBtn.Clicked() {
					if v.FitMode == PDFFitPage {
						v.FitMode = PDFFitWidth
						v.Zoom = 1.0
					} else {
						v.FitMode = PDFFitPage
					}
				}
			}

			if !opts.Compact && availW >= 540 && cnt > 1 {
				twoBtn := pdfIconBtnActive(c, "pdf-btn-two", iconColumns, i18n.T("双页并排视图"), false, v.TwoPage, pg, t)
				if twoBtn.Clicked() {
					v.TwoPage = !v.TwoPage
					v.pendingJumpPage = v.CurrentPage
					v.pendingInstant = true
				}
			}

			if availW >= 480 {
				rotBtn := pdfIconBtn(c, "pdf-btn-rot", iconRotateCW, i18n.T("顺时针旋转 90°"), false, pg, t)
				if rotBtn.Clicked() {
					v.RotateCW()
				}
			}

			findBtn := pdfIconBtnActive(c, "pdf-btn-find", iconSearchPDF, i18n.T("在 PDF 中查找 (⌘F)"), false, v.FindOpen, pg, t)
			if findBtn.Clicked() {
				if v.FindOpen {
					v.FindOpen = false
				} else {
					v.OpenFind()
				}
			}

			if opts.PinPage != nil {
				pinned := opts.PinnedPage == v.CurrentPage+1
				pinTip := fmt.Sprintf(i18n.T("在文档中固定显示第 %d 页"), v.CurrentPage+1)
				pinBtn := pdfIconBtnActive(c, "pdf-btn-pin", iconPinPage, pinTip, false, pinned, pg, t)
				if pinBtn.Clicked() {
					opts.PinPage(v.CurrentPage + 1)
				}
			}

			if opts.OpenSplit != nil && availW >= 520 {
				spBtn := pdfIconBtn(c, "pdf-btn-split", iconExternalSplit, i18n.T("在分屏中阅读"), false, pg, t)
				if spBtn.Clicked() {
					opts.OpenSplit(v.CurrentPage + 1)
				}
			}

			if opts.ExpandModal != nil {
				maxBtn := pdfIconBtn(c, "pdf-btn-max", iconMaximize, i18n.T("全屏阅读"), false, pg, t)
				if maxBtn.Clicked() {
					opts.ExpandModal()
				}
			}

			if opts.Done != nil {
				pdfIslandDivider(c, pg)
				doneBtn := ui.ButtonBase(c.Key("pdf-btn-done")).Height(26).PaddingX(10).Radius(7).
					Background(t.Accent).Center().Cursor(ui.CursorPointer)
				doneBtn.Children(func() {
					ui.Text(c, i18n.T("完成")).FontSize(12).FontWeight(600).TextColor(t.AccentText).NoWrap()
				})
				if doneBtn.Clicked() {
					opts.Done()
				}
			}
		})
	})
}

func pdfIslandDivider(c *ui.Context, pg Page) {
	ui.Box(c).Size(1, 16).Background(pg.TableLine)
}

func pdfIconBtn(c *ui.Context, key string, icon *ui.SVG, tip string, disabled bool, pg Page, t *ui.Theme) ui.Element {
	return pdfIconBtnActive(c, key, icon, tip, disabled, false, pg, t)
}

func pdfIconBtnActive(c *ui.Context, key string, icon *ui.SVG, tip string, disabled, active bool, pg Page, t *ui.Theme) ui.Element {
	btn := ui.ButtonBase(c.Key(key)).Size(28, 28).Radius(7).Center().Tooltip(tip)
	if disabled {
		btn.Disabled(true)
	} else {
		btn.Cursor(ui.CursorPointer)
		if active {
			btn.Background(t.Accent.Alpha(0.15))
		} else if btn.Hovered() {
			btn.Background(pg.Hover)
		}
	}
	fg := t.Text
	if disabled {
		fg = t.TextMuted.Alpha(0.4)
	} else if active {
		fg = t.Accent
	}
	btn.Children(func() {
		ui.Icon(c, icon).FontSize(14).TextColor(fg)
	})
	return btn
}
