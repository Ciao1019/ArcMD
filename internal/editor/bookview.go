package editor

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Ciao1019/ArcMD/internal/bookdoc"
	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

const (
	bookSidebarW        = float32(228)
	maxBookChapterCache = 6
	maxBookBitmaps      = 48
)

type flatBookTOC struct {
	title        string
	chapter      int
	anchor       string
	headingIndex int
	depth        int
}

type bookChapterEntry struct {
	ed       *Editor
	anchors  map[string]string
	lastUsed uint64
}

type bookBitmapEntry struct {
	bmp      *ui.Bitmap
	width    int
	height   int
	loadedAt time.Time
	released bool
	failed   bool
}

// HTMLWebView is an optional sandboxed native web view supplied by the host
// for faithful HTML/CSS/SVG/JS rendering. Headless tests return nil and fall
// back to the native Go reader view.
type HTMLWebView interface {
	Native() ui.NativeWebView
	ScrollToAnchor(anchor string, headingIndex int)
	Find(query string, forward bool, findNext bool, onResult func(active, total int))
	StopFind()
	Reload()
	Destroy()
}

// BookOptions configures BookView rendering and host callbacks.
type BookOptions struct {
	Theme      *ui.Theme
	Page       Page
	Title      string
	Compact    bool
	Async      bool
	FontSize   float32
	LineHeight float32
	FontFamily string
	Column     float32
	FullWidth  bool

	PinnedChapter int // 1-based pinned chapter for EPUB embeds when > 0
	PinChapter    func(ch1Based int)

	ExpandModal       func()
	OpenSplit         func(ch1Based int, anchor string)
	Done              func()
	RevealFile        func()
	OpenInBrowser     func()
	SaveAsMarkdown    func()
	OpenExternalURL   func(url string)
	LoadExternalImage func(src string) ImageResource
	WebView           func() HTMLWebView
}

// BookViewer holds immediate-mode UI state, chapter editor LRU cache, and
// embedded asset bitmaps for reading an HTML or EPUB document smoothly.
type BookViewer struct {
	Doc          *bookdoc.Document
	LoadErr      error
	RequestFocus bool
	TOCScroll    ui.ScrollState

	CurrentChapter int // 0-based
	SidebarOpen    bool
	ShowSource     bool // HTML raw source toggle

	FindOpen      bool
	FindFocus     bool
	FindQuery     string
	findLastQuery string
	Matches       []bookdoc.Match
	MatchIndex    int

	webFindActive int
	webFindTotal  int
	webFindQuery  string
	activeWebView HTMLWebView

	tocFlat         []flatBookTOC
	activeAnchor    string
	pendingAnchor   string
	pendingHeading  int
	pendingBlockIdx int

	chapterCache map[int]*bookChapterEntry
	chapterSeq   uint64
	sourceEd     *Editor

	syncedFindCh     int
	syncedFindQ      string
	syncedFindOcc    int
	syncedFindSource bool

	lastViewW float32

	imgMu    sync.Mutex
	imgCache map[string]bookBitmapEntry
	imgLRU   []string

	coverMu    sync.Mutex
	coverBmp   *ui.Bitmap
	coverTried bool

	closed atomic.Bool
}

// NewBookViewer creates a BookViewer for doc (or stores loadErr when doc could not open).
func NewBookViewer(doc *bookdoc.Document, loadErr error) *BookViewer {
	v := &BookViewer{
		Doc:             doc,
		LoadErr:         loadErr,
		pendingHeading:  -1,
		pendingBlockIdx: -1,
		syncedFindCh:    -1,
		syncedFindOcc:   -1,
		chapterCache:    make(map[int]*bookChapterEntry, maxBookChapterCache),
		imgCache:        make(map[string]bookBitmapEntry, maxBookBitmaps),
	}
	if doc != nil {
		v.tocFlat = flattenBookTOC(doc.TOC(), 0)
	}
	return v
}

func flattenBookTOC(items []bookdoc.TOCItem, depth int) []flatBookTOC {
	var out []flatBookTOC
	for _, it := range items {
		out = append(out, flatBookTOC{
			title:        it.Title,
			chapter:      it.Chapter,
			anchor:       it.Anchor,
			headingIndex: it.HeadingIndex,
			depth:        min(depth, 5),
		})
		if len(it.Children) > 0 {
			out = append(out, flattenBookTOC(it.Children, depth+1)...)
		}
	}
	return out
}

// Close releases cached bitmaps, chapter editors, and the underlying BookViewer document.
func (v *BookViewer) Close() {
	if v == nil || !v.closed.CompareAndSwap(false, true) {
		return
	}
	v.ClearBitmaps()
	if v.Doc != nil {
		v.Doc.Close()
	}
}

// ClearBitmaps releases decoded image bitmaps and cached chapter editors.
func (v *BookViewer) ClearBitmaps() {
	if v == nil {
		return
	}
	for _, ent := range v.chapterCache {
		if ent != nil && ent.ed != nil {
			ent.ed.ClearImages()
		}
	}
	clear(v.chapterCache)
	if v.sourceEd != nil {
		v.sourceEd.ClearImages()
		v.sourceEd = nil
	}
	v.imgMu.Lock()
	clear(v.imgCache)
	v.imgLRU = nil
	v.imgMu.Unlock()

	v.coverMu.Lock()
	v.coverBmp = nil
	v.coverTried = false
	v.coverMu.Unlock()
}

// IsEPUB reports whether the opened document is an EPUB e-book.
func (v *BookViewer) IsEPUB() bool {
	return v != nil && v.Doc != nil && v.Doc.Kind() == bookdoc.KindEPUB
}

// IsHTML reports whether the opened document is an HTML file.
func (v *BookViewer) IsHTML() bool {
	return v != nil && v.Doc != nil && v.Doc.Kind() == bookdoc.KindHTML
}

// ChapterCount returns the number of chapters (1 for HTML, >=1 for valid EPUB).
func (v *BookViewer) ChapterCount() int {
	if v == nil || v.Doc == nil {
		return 0
	}
	return v.Doc.ChapterCount()
}

// CurrentChapterTitle returns the display title of the active chapter.
func (v *BookViewer) CurrentChapterTitle() string {
	if v == nil || v.Doc == nil || v.Doc.ChapterCount() == 0 {
		return ""
	}
	ch := min(max(v.CurrentChapter, 0), v.Doc.ChapterCount()-1)
	return v.Doc.Chapter(ch).Title
}

// ToggleOutline toggles the TOC sidebar open/closed.
func (v *BookViewer) ToggleOutline() {
	if v == nil || v.Doc == nil {
		return
	}
	v.SidebarOpen = !v.SidebarOpen
}

// ToggleSource toggles raw HTML source view for HTML documents.
func (v *BookViewer) ToggleSource() {
	if !v.IsHTML() {
		return
	}
	v.ShowSource = !v.ShowSource
	v.syncedFindCh = -1
	v.RequestFocus = true
}

// OpenFind opens and focuses the unified find bar.
func (v *BookViewer) OpenFind() {
	if v == nil || v.Doc == nil {
		return
	}
	v.FindOpen = true
	v.FindFocus = true
}

// FindText populates the find bar with query and jumps to the first match.
func (v *BookViewer) FindText(query string) {
	if v == nil || v.Doc == nil {
		return
	}
	v.FindOpen = true
	v.FindFocus = true
	v.FindQuery = query
	v.findLastQuery = ""
	v.runSearch(query)
}

// GoToChapter switches to 0-based chapter ch and scrolls to its top.
func (v *BookViewer) GoToChapter(ch int, instant bool) {
	_ = instant
	if v == nil || v.Doc == nil || v.Doc.ChapterCount() == 0 {
		return
	}
	ch = min(max(ch, 0), v.Doc.ChapterCount()-1)
	v.ShowSource = false
	v.CurrentChapter = ch
	v.activeAnchor = ""
	v.pendingAnchor = ""
	v.pendingHeading = -1
	v.pendingBlockIdx = -1
	if !v.FindOpen {
		v.RequestFocus = true
	}
	if ent := v.chapterCache[ch]; ent != nil && ent.ed != nil {
		ent.ed.Scroll.Y = 0
		ent.ed.scrollGlide = scrollGlideState{}
	}
}

// GoToChapterAnchor jumps to 0-based chapter ch and scrolls to anchor if present.
func (v *BookViewer) GoToChapterAnchor(ch int, anchor string, instant bool) {
	_ = instant
	if v == nil || v.Doc == nil || v.Doc.ChapterCount() == 0 {
		return
	}
	ch = min(max(ch, 0), v.Doc.ChapterCount()-1)
	v.ShowSource = false
	v.CurrentChapter = ch
	v.activeAnchor = anchor
	v.pendingAnchor = anchor
	v.pendingHeading = -1
	v.pendingBlockIdx = -1
	if !v.FindOpen {
		v.RequestFocus = true
	}
	if anchor == "" {
		if ent := v.chapterCache[ch]; ent != nil && ent.ed != nil {
			ent.ed.Scroll.Y = 0
			ent.ed.scrollGlide = scrollGlideState{}
		}
	}
}

// GoToTOCItem jumps to a flattened TOC entry (supporting both #anchor and heading index).
func (v *BookViewer) GoToTOCItem(it flatBookTOC) {
	if v == nil || v.Doc == nil || v.Doc.ChapterCount() == 0 || it.chapter < 0 {
		return
	}
	ch := min(max(it.chapter, 0), v.Doc.ChapterCount()-1)
	v.ShowSource = false
	v.CurrentChapter = ch
	v.activeAnchor = it.anchor
	v.pendingAnchor = it.anchor
	v.pendingHeading = it.headingIndex
	v.pendingBlockIdx = -1
	if !v.FindOpen {
		v.RequestFocus = true
	}
	if it.anchor == "" && it.headingIndex < 0 {
		if ent := v.chapterCache[ch]; ent != nil && ent.ed != nil {
			ent.ed.Scroll.Y = 0
			ent.ed.scrollGlide = scrollGlideState{}
		}
	}
}

// StepChapter advances by delta chapters (+1 / -1).
func (v *BookViewer) StepChapter(delta int) {
	if v == nil || v.Doc == nil || v.Doc.ChapterCount() <= 1 {
		return
	}
	next := min(max(v.CurrentChapter+delta, 0), v.Doc.ChapterCount()-1)
	if next != v.CurrentChapter {
		v.GoToChapter(next, false)
	}
}

// RevealBlockIndex jumps to 0-based chapter ch and reveals top-level block blockIdx.
func (v *BookViewer) RevealBlockIndex(ch int, blockIdx int) {
	if v == nil || v.Doc == nil || v.Doc.ChapterCount() == 0 {
		return
	}
	ch = min(max(ch, 0), v.Doc.ChapterCount()-1)
	v.ShowSource = false
	v.CurrentChapter = ch
	v.pendingAnchor = ""
	v.pendingHeading = -1
	v.pendingBlockIdx = max(0, blockIdx)
}

// ExportCurrentChapterMarkdown converts the active HTML page or current EPUB
// chapter to Markdown text for saving as an editable note.
func (v *BookViewer) ExportCurrentChapterMarkdown() string {
	if v == nil || v.Doc == nil || v.Doc.ChapterCount() == 0 {
		return ""
	}
	ch := min(max(v.CurrentChapter, 0), v.Doc.ChapterCount()-1)
	ent := v.chapterEntry(ch)
	if ent == nil || ent.ed == nil {
		return ""
	}
	return ExportMarkdown(ent.ed.Doc)
}

// CoverBitmap decodes and caches the EPUB cover image bitmap when present.
func (v *BookViewer) CoverBitmap() *ui.Bitmap {
	if v == nil || v.Doc == nil || v.Doc.Kind() != bookdoc.KindEPUB || v.closed.Load() {
		return nil
	}
	v.coverMu.Lock()
	if v.coverTried {
		bmp := v.coverBmp
		v.coverMu.Unlock()
		return bmp
	}
	v.coverTried = true
	v.coverMu.Unlock()

	data, _, ok := v.Doc.CoverImage()
	if !ok || len(data) == 0 {
		return nil
	}
	bmp, err := ui.DecodeBitmapScaled(data, 420)
	if err != nil {
		return nil
	}
	v.coverMu.Lock()
	if !v.closed.Load() {
		v.coverBmp = bmp
	}
	v.coverMu.Unlock()
	return bmp
}

func (v *BookViewer) chapterEntry(chIdx int) *bookChapterEntry {
	if v == nil || v.Doc == nil || v.Doc.ChapterCount() == 0 {
		return nil
	}
	chIdx = min(max(chIdx, 0), v.Doc.ChapterCount()-1)
	if v.chapterCache == nil {
		v.chapterCache = make(map[int]*bookChapterEntry, maxBookChapterCache)
	}
	if ent, ok := v.chapterCache[chIdx]; ok && ent != nil && ent.ed != nil {
		v.chapterSeq++
		ent.lastUsed = v.chapterSeq
		return ent
	}

	markup, err := v.Doc.ChapterHTML(chIdx)
	var res HTMLImportResult
	if err != nil {
		doc := document.New()
		doc.Blocks = []*document.Block{document.NewBlock("paragraph", fmt.Sprintf(i18n.T("无法读取章节内容：%v"), err))}
		res = HTMLImportResult{Doc: doc}
	} else {
		res = ImportHTMLDocument(markup, HTMLImportOptions{EmbedImages: true})
	}
	ed := FromOwnedDocument(res.Doc)
	ed.SetReadOnly(true)

	v.chapterSeq++
	ent := &bookChapterEntry{
		ed:       ed,
		anchors:  res.Anchors,
		lastUsed: v.chapterSeq,
	}
	if len(v.chapterCache) >= maxBookChapterCache {
		oldestIdx := -1
		var oldestSeq uint64
		for k, item := range v.chapterCache {
			if k == chIdx {
				continue
			}
			if oldestIdx < 0 || item.lastUsed < oldestSeq {
				oldestIdx = k
				oldestSeq = item.lastUsed
			}
		}
		if oldestIdx >= 0 {
			if evicted := v.chapterCache[oldestIdx]; evicted != nil && evicted.ed != nil {
				evicted.ed.ClearImages()
			}
			delete(v.chapterCache, oldestIdx)
		}
	}
	v.chapterCache[chIdx] = ent
	return ent
}

func (v *BookViewer) sourceEditor() *Editor {
	if v == nil || v.Doc == nil {
		return nil
	}
	if v.sourceEd != nil {
		return v.sourceEd
	}
	doc := document.New()
	code := document.NewBlock("code", v.Doc.RawSource())
	code.Language = "html"
	doc.Blocks = []*document.Block{code}
	ed := FromOwnedDocument(doc)
	ed.SetReadOnly(true)
	v.sourceEd = ed
	return ed
}

func (v *BookViewer) releaseUploadedPixels(c *ui.Context) {
	now := c.Now()
	v.imgMu.Lock()
	defer v.imgMu.Unlock()
	needLater := false
	for k, ent := range v.imgCache {
		if ent.released || ent.bmp == nil || ent.loadedAt.IsZero() {
			continue
		}
		if wait := 250*time.Millisecond - now.Sub(ent.loadedAt); wait > 0 {
			needLater = true
			continue
		}
		ent.bmp.ReleasePixels()
		ent.released = true
		v.imgCache[k] = ent
	}
	if needLater {
		c.After(260 * time.Millisecond)
	}
}

func (v *BookViewer) loadBookImage(chIdx int, src string, extLoader func(string) ImageResource) ImageResource {
	if v == nil || v.Doc == nil || v.closed.Load() {
		return ImageResource{}
	}
	data, _, key, ok := v.Doc.ReadAsset(chIdx, src)
	if !ok {
		if extLoader != nil {
			return extLoader(src)
		}
		return ImageResource{Problem: ImageMissing}
	}

	v.imgMu.Lock()
	if ent, exists := v.imgCache[key]; exists {
		v.touchImageLocked(key)
		v.imgMu.Unlock()
		if ent.failed || ent.bmp == nil {
			return ImageResource{Problem: ImageUnsupported, Local: true}
		}
		return ImageResource{
			Bitmap:   ent.bmp,
			Width:    ent.width,
			Height:   ent.height,
			LoadedAt: ent.loadedAt,
			Local:    true,
		}
	}
	v.imgMu.Unlock()

	bmp, err := ui.DecodeBitmap(data)
	var ent bookBitmapEntry
	if err != nil || bmp == nil {
		ent = bookBitmapEntry{failed: true}
	} else {
		w, h := bmp.Size()
		ent = bookBitmapEntry{
			bmp:      bmp,
			width:    w,
			height:   h,
			loadedAt: time.Now(),
		}
	}

	v.imgMu.Lock()
	if v.imgCache == nil {
		v.imgCache = make(map[string]bookBitmapEntry, maxBookBitmaps)
	}
	v.imgCache[key] = ent
	v.touchImageLocked(key)
	for len(v.imgCache) > maxBookBitmaps && len(v.imgLRU) > 0 {
		old := v.imgLRU[0]
		v.imgLRU = v.imgLRU[1:]
		delete(v.imgCache, old)
	}
	v.imgMu.Unlock()

	if ent.failed {
		return ImageResource{Problem: ImageUnsupported, Local: true}
	}
	return ImageResource{
		Bitmap:   ent.bmp,
		Width:    ent.width,
		Height:   ent.height,
		LoadedAt: ent.loadedAt,
		Local:    true,
	}
}

func (v *BookViewer) touchImageLocked(key string) {
	if idx := slices.Index(v.imgLRU, key); idx >= 0 {
		v.imgLRU = slices.Delete(v.imgLRU, idx, idx+1)
	}
	v.imgLRU = append(v.imgLRU, key)
}

func (v *BookViewer) runSearch(q string) {
	v.findLastQuery = q
	trimmed := strings.TrimSpace(q)
	if trimmed == "" || v.Doc == nil {
		v.Matches = nil
		v.MatchIndex = 0
		v.syncSearchToEditor(false)
		return
	}
	v.Matches = v.Doc.Find(trimmed)
	v.MatchIndex = 0
	for i, m := range v.Matches {
		if m.Chapter >= v.CurrentChapter {
			v.MatchIndex = i
			break
		}
	}
	v.jumpToCurrentMatch(true)
}

func (v *BookViewer) stepMatch(delta int) {
	if len(v.Matches) == 0 {
		return
	}
	v.MatchIndex = (v.MatchIndex + delta + len(v.Matches)) % len(v.Matches)
	v.jumpToCurrentMatch(true)
}

func (v *BookViewer) jumpToCurrentMatch(scroll bool) {
	if len(v.Matches) == 0 {
		v.syncSearchToEditor(false)
		return
	}
	m := v.Matches[min(max(v.MatchIndex, 0), len(v.Matches)-1)]
	if m.Chapter != v.CurrentChapter || v.ShowSource {
		v.ShowSource = false
		v.CurrentChapter = m.Chapter
	}
	v.syncSearchToEditor(scroll)
}

func (v *BookViewer) syncSearchToEditor(scroll bool) {
	if v == nil || v.Doc == nil || v.Doc.ChapterCount() == 0 {
		return
	}
	ch := min(max(v.CurrentChapter, 0), v.Doc.ChapterCount()-1)
	var ed *Editor
	if v.ShowSource && v.IsHTML() {
		ed = v.sourceEditor()
	} else {
		if ent := v.chapterEntry(ch); ent != nil {
			ed = ent.ed
		}
	}
	if ed == nil {
		return
	}
	q := ""
	occ := 0
	if v.FindOpen {
		q = strings.TrimSpace(v.FindQuery)
		if len(v.Matches) > 0 && v.MatchIndex >= 0 && v.MatchIndex < len(v.Matches) {
			m := v.Matches[v.MatchIndex]
			if m.Chapter == ch {
				occ = m.Occurrence
			}
		}
	}
	if !scroll && v.syncedFindCh == ch && v.syncedFindQ == q && v.syncedFindOcc == occ && v.syncedFindSource == v.ShowSource {
		return
	}
	v.syncedFindCh = ch
	v.syncedFindQ = q
	v.syncedFindOcc = occ
	v.syncedFindSource = v.ShowSource
	ed.SetExternalSearch(q, occ, scroll)
}

func (v *BookViewer) applyPendingNavigation(ent *bookChapterEntry) {
	if v == nil || ent == nil || ent.ed == nil {
		return
	}
	if v.pendingBlockIdx >= 0 {
		idx := v.pendingBlockIdx
		v.pendingBlockIdx = -1
		if idx < len(ent.ed.Doc.Blocks) {
			ent.ed.RevealBlock(ent.ed.Doc.Blocks[idx].ID)
		}
		return
	}
	if v.pendingAnchor != "" {
		anc := v.pendingAnchor
		v.pendingAnchor = ""
		v.pendingHeading = -1
		id := ent.anchors[anc]
		if id == "" {
			id = ent.anchors[strings.ToLower(anc)]
		}
		if id != "" {
			ent.ed.ScrollToBlock(id)
			return
		}
	}
	if v.pendingHeading >= 0 {
		targetIdx := v.pendingHeading
		v.pendingHeading = -1
		count := 0
		for _, b := range ent.ed.Doc.Blocks {
			if b.Kind == "heading" && strings.TrimSpace(b.Text()) != "" {
				if count == targetIdx {
					ent.ed.ScrollToBlock(b.ID)
					return
				}
				count++
			}
		}
	}
}

// BookView renders an interactive read-only HTML or EPUB reader inside the
// current UI parent.
func BookView(c *ui.Context, v *BookViewer, opts BookOptions) ui.Element {
	t := opts.Theme
	if t == nil {
		t = c.Theme()
	}
	pg := opts.Page
	wb := pg.Whiteboard

	root := ui.Row(c.Key("book-root")).Grow(1).WidthPercent(100).HeightPercent(100).MinWidth(0).MinHeight(0).Clip()
	if v == nil || v.LoadErr != nil || v.Doc == nil || v.Doc.ChapterCount() == 0 {
		root.Center().Background(t.Background).Children(func() {
			card := ui.Column(c.Key("book-err")).MaxWidth(440).Padding(24).Gap(10).Radius(12).
				Border(1, pg.TableLine).AlignItems(ui.Center)
			card.Children(func() {
				icon := iconBookOpen
				if v != nil && v.Doc != nil && v.Doc.Kind() == bookdoc.KindHTML {
					icon = iconFileCode
				}
				ui.Icon(c, icon).FontSize(28).TextColor(t.TextMuted)
				title := i18n.T("无法打开文档")
				detail := i18n.T("文件可能已损坏或格式不受支持。")
				if v != nil && v.LoadErr != nil {
					if errors.Is(v.LoadErr, bookdoc.ErrEncrypted) {
						title = i18n.T("此 EPUB 受 DRM 保护")
						detail = i18n.T("当前不支持加密的 DRM 电子书预览，可在其他阅读器中打开。")
					} else {
						detail = v.LoadErr.Error()
					}
				}
				ui.Text(c, title).FontSize(15).FontWeight(600).TextColor(t.Text)
				ui.Text(c, detail).FontSize(13).TextColor(t.TextMuted)
				ui.Row(c.Key("book-err-actions")).Gap(8).AlignItems(ui.Center).Children(func() {
					if opts.OpenInBrowser != nil {
						openBtn := ui.ButtonBase(c.Key("book-open-ext")).Height(30).PaddingX(12).Radius(8).
							Border(1, pg.TableLine).Center().Cursor(ui.CursorPointer)
						if openBtn.Hovered() {
							openBtn.Background(pg.Hover)
						}
						openBtn.Children(func() {
							ui.Text(c, i18n.T("在默认浏览器中打开")).FontSize(12.5).TextColor(t.Text)
						})
						if openBtn.Clicked() {
							opts.OpenInBrowser()
						}
					}
					if opts.RevealFile != nil {
						btn := ui.ButtonBase(c.Key("book-reveal-err")).Height(30).PaddingX(12).Radius(8).
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
		})
		return root
	}

	v.releaseUploadedPixels(c)

	var liveWV HTMLWebView
	if v.IsHTML() && !v.ShowSource && opts.WebView != nil {
		liveWV = opts.WebView()
	}
	v.activeWebView = liveWV
	useNativeWebView := liveWV != nil && liveWV.Native() != nil

	if liveWV != nil {
		if v.pendingAnchor != "" || v.pendingHeading >= 0 {
			liveWV.ScrollToAnchor(v.pendingAnchor, v.pendingHeading)
			v.pendingAnchor = ""
			v.pendingHeading = -1
		}
		if v.FindOpen {
			if v.FindQuery != v.webFindQuery {
				v.syncWebFind(liveWV, true, false)
			}
		} else if v.webFindQuery != "" {
			liveWV.StopFind()
			v.webFindQuery = ""
			v.webFindActive, v.webFindTotal = 0, 0
		}
	} else {
		if v.webFindQuery != "" {
			v.webFindQuery = ""
			v.webFindActive, v.webFindTotal = 0, 0
		}
		if v.FindOpen && v.FindQuery != v.findLastQuery {
			v.runSearch(v.FindQuery)
		}
	}

	// Reader-wide shortcuts.
	if !opts.Compact && root.Shortcut(ui.Cmd|ui.Shift, ui.KeyO) {
		v.ToggleOutline()
	}
	if root.Shortcut(ui.Cmd, ui.KeyF) {
		v.OpenFind()
	}
	if v.FindOpen {
		if root.Shortcut(ui.Cmd, ui.KeyG) {
			if liveWV != nil {
				v.syncWebFind(liveWV, true, true)
			} else {
				v.stepMatch(1)
			}
		} else if root.Shortcut(ui.Cmd|ui.Shift, ui.KeyG) {
			if liveWV != nil {
				v.syncWebFind(liveWV, false, true)
			} else {
				v.stepMatch(-1)
			}
		} else if root.Shortcut(0, ui.KeyEscape) {
			v.closeFind()
		}
	} else if v.IsEPUB() && v.ChapterCount() > 1 {
		if root.Shortcut(ui.Alt, ui.KeyLeft) || root.Shortcut(0, ui.KeyBracketLeft) {
			v.StepChapter(-1)
		}
		if root.Shortcut(ui.Alt, ui.KeyRight) || root.Shortcut(0, ui.KeyBracketRight) {
			v.StepChapter(1)
		}
	}

	root.Background(t.Background)
	root.Children(func() {
		if v.SidebarOpen && !opts.Compact {
			v.sidebarView(c, t, pg)
			if liveWV != nil && (v.pendingAnchor != "" || v.pendingHeading >= 0) {
				liveWV.ScrollToAnchor(v.pendingAnchor, v.pendingHeading)
				v.pendingAnchor = ""
				v.pendingHeading = -1
			}
		}

		stage := ui.Column(c.Key("book-stage")).Grow(1).WidthPercent(100).HeightPercent(100).
			MinWidth(0).MinHeight(0).Background(t.Background)
		stageBounds := stage.LayoutBounds()
		availW := stageBounds.W
		if availW <= 0 {
			winW, _ := c.Size()
			availW = max(320, winW-260)
			if v.SidebarOpen && !opts.Compact {
				availW = max(240, availW-bookSidebarW)
			}
			c.After(0)
		}
		v.lastViewW = availW

		ch := min(max(v.CurrentChapter, 0), v.Doc.ChapterCount()-1)
		v.CurrentChapter = ch

		var ed *Editor
		var edKey string
		if !useNativeWebView {
			if v.ShowSource && v.IsHTML() {
				ed = v.sourceEditor()
				edKey = fmt.Sprintf("book-src-%p", v)
			} else {
				ent := v.chapterEntry(ch)
				v.applyPendingNavigation(ent)
				if ent != nil {
					ed = ent.ed
				}
				edKey = fmt.Sprintf("book-ch-%p-%d", v, ch)
			}
			v.syncSearchToEditor(false)
		}

		stage.Children(func() {
			if useNativeWebView {
				wvEl := ui.WebView(c.Key("book-html-wv"), liveWV.Native()).Grow(1).
					WidthPercent(100).HeightPercent(100).MinWidth(0).MinHeight(0)
				if v.RequestFocus {
					wvEl.Focus()
					v.RequestFocus = false
				}
			} else if ed != nil {
				if v.RequestFocus {
					ed.RequestFocus = true
					v.RequestFocus = false
				}
				col := opts.Column
				fullW := opts.FullWidth
				fs := opts.FontSize
				lh := opts.LineHeight
				if opts.Compact {
					fullW = true
					if fs <= 0 {
						fs = 15
					} else {
						fs = min(fs, 16)
					}
					if lh <= 0 {
						lh = 1.6
					}
				}
				showIsland := opts.Compact || v.IsHTML() || (v.IsEPUB() && v.ChapterCount() > 1)
				var footerFn func(*ui.Context)
				if (v.IsEPUB() && v.ChapterCount() > 1 && !v.ShowSource) || showIsland {
					footerFn = func(fc *ui.Context) {
						v.chapterFooterView(fc, t, pg, showIsland)
					}
				}

				View(c.Key(edKey), ed, Options{
					Width:              availW,
					Theme:              t,
					Page:               pg,
					FontSize:           fs,
					LineHeight:         lh,
					FontFamily:         opts.FontFamily,
					Column:             col,
					FullWidth:          fullW,
					CompactMargins:     opts.Compact,
					Outline:            false,
					DirectLinks:        true,
					DisableFind:        true,
					HideReadOnlyNotice: true,
					LoadImage: func(src string) ImageResource {
						return v.loadBookImage(ch, src, opts.LoadExternalImage)
					},
					OpenLink: func(rawURL string) {
						targetCh, anchor, extURL, ok := v.Doc.ResolveLink(ch, rawURL)
						if !ok {
							return
						}
						if extURL != "" {
							if opts.OpenExternalURL != nil {
								opts.OpenExternalURL(extURL)
							}
							return
						}
						if targetCh >= 0 {
							v.GoToChapterAnchor(targetCh, anchor, false)
						}
					},
					Footer: footerFn,
				})
			}

			if v.FindOpen {
				v.findBarView(c, t, pg)
			}

			if opts.Compact || v.IsHTML() || (v.IsEPUB() && v.ChapterCount() > 1) {
				v.floatingIslandView(c, t, pg, wb, availW, opts)
			}
		})
	})

	return root
}

func (v *BookViewer) chapterFooterView(c *ui.Context, t *ui.Theme, pg Page, extraBottomSpace bool) {
	cnt := v.ChapterCount()
	if v.IsEPUB() && cnt > 1 && !v.ShowSource {
		ui.Column(c.Key("book-ch-footer")).Gap(12).Padding(20, 0, 12, 0).Children(func() {
			ui.Box(c).Height(1).WidthPercent(100).Background(pg.TableLine)
			ui.Row(c.Key("book-ch-nav")).WidthPercent(100).AlignItems(ui.Center).Children(func() {
				if v.CurrentChapter > 0 {
					prevTitle := v.Doc.Chapter(v.CurrentChapter - 1).Title
					prev := ui.ButtonBase(c.Key("book-foot-prev")).Height(34).PaddingX(12).Gap(6).Radius(8).
						Border(1, pg.TableLine).AlignItems(ui.Center).Cursor(ui.CursorPointer)
					if prev.Hovered() {
						prev.Background(pg.Hover)
					}
					prev.Children(func() {
						ui.Icon(c, iconChevronLeft).FontSize(13).TextColor(t.TextMuted)
						ui.Text(c, i18n.T("上一章：")+prevTitle).FontSize(12.5).TextColor(t.Text).MaxLines(1)
					})
					if prev.Clicked() {
						v.StepChapter(-1)
					}
				}
				ui.Spacer(c)
				if v.CurrentChapter < cnt-1 {
					nextTitle := v.Doc.Chapter(v.CurrentChapter + 1).Title
					next := ui.ButtonBase(c.Key("book-foot-next")).Height(34).PaddingX(12).Gap(6).Radius(8).
						Border(1, pg.TableLine).AlignItems(ui.Center).Cursor(ui.CursorPointer)
					if next.Hovered() {
						next.Background(pg.Hover)
					}
					next.Children(func() {
						ui.Text(c, i18n.T("下一章：")+nextTitle).FontSize(12.5).TextColor(t.Text).MaxLines(1)
						ui.Icon(c, iconChevronRight).FontSize(13).TextColor(t.TextMuted)
					})
					if next.Clicked() {
						v.StepChapter(1)
					}
				}
			})
		})
	}
	if extraBottomSpace {
		ui.Box(c).Height(52)
	}
}

func (v *BookViewer) sidebarView(c *ui.Context, t *ui.Theme, pg Page) {
	rail := ui.Column(c.Key("book-sidebar")).Width(bookSidebarW).HeightPercent(100).Shrink(0).MinHeight(0).
		Background(t.Background).BorderWidth(0, 1, 0, 0).BorderColor(pg.TableLine)
	rail.Children(func() {
		if v.IsEPUB() {
			bmp := v.CoverBitmap()
			bookHdr := ui.Row(c.Key("book-side-meta")).Padding(12, 10, 10, 12).Gap(10).AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).BorderColor(pg.TableLine)
			bookHdr.Children(func() {
				if bmp != nil {
					cov := ui.Box(c.Key("book-side-cov")).Size(34, 48).Radius(4).Shrink(0).
						Background(ui.RGB(255, 255, 255)).Border(1, pg.TableLine).
						Shadow(0, 2, 6, 0, ui.RGBA(0, 0, 0, 0.12))
					cov.Draw(func(p *ui.Painter, r ui.Rect) {
						p.Clip(r, 4, func() { p.Image(bmp, r, ui.Cover) })
					})
				} else {
					covIcon := ui.Box(c.Key("book-side-cov-ph")).Size(32, 42).Radius(5).Shrink(0).
						Background(t.Accent.Alpha(0.10)).Center()
					covIcon.Children(func() {
						ui.Icon(c, iconBookOpen).FontSize(16).TextColor(t.Accent)
					})
				}
				ui.Column(c.Key("book-side-titles")).Grow(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, v.Doc.Title()).FontSize(12.5).FontWeight(600).TextColor(t.Text).MaxLines(2)
					if author := v.Doc.Author(); author != "" {
						ui.Text(c, author).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
					}
				})
				closeBtn := ui.ButtonBase(c.Key("book-side-close")).Size(24, 24).Radius(6).Center().
					Shrink(0).Cursor(ui.CursorPointer).Tooltip(i18n.T("收起 (⇧⌘O)"))
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
		} else {
			hdr := ui.Row(c.Key("book-side-hdr")).Height(40).PaddingX(10).Gap(6).AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).BorderColor(pg.TableLine)
			hdr.Children(func() {
				ui.Text(c, i18n.T("目录")).FontSize(12.5).FontWeight(600).TextColor(t.TextMuted).PaddingX(4)
				ui.Spacer(c)
				closeBtn := ui.ButtonBase(c.Key("book-side-close")).Size(24, 24).Radius(6).Center().
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
		}

		if len(v.tocFlat) == 0 {
			ui.Column(c.Key("book-toc-empty")).Grow(1).Center().Padding(16).Children(func() {
				ui.Text(c, i18n.T("此文档没有目录条目")).FontSize(12.5).TextColor(t.TextMuted)
			})
		} else {
			ui.Scroll(c.Key("book-toc-scroll")).Grow(1).MinHeight(0).TrackScroll(&v.TOCScroll).Children(func() {
				list := ui.Column(c.Key("book-toc-list")).Padding(8).Gap(2)
				list.Children(func() {
					for i, item := range v.tocFlat {
						active := false
						if v.IsEPUB() {
							active = item.chapter == v.CurrentChapter && (v.activeAnchor == "" || item.anchor == v.activeAnchor)
						} else {
							active = item.anchor != "" && item.anchor == v.activeAnchor
						}
						row := ui.Row(c.Key(fmt.Sprintf("book-toc:%d", i))).MinHeight(30).
							Padding(5, 8, 5, float32(8+item.depth*12)).Gap(6).Radius(7).
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
								Grow(1).MinWidth(0).MaxLines(2)
						})
						if row.Clicked() && item.chapter >= 0 {
							v.GoToTOCItem(item)
						}
					}
				})
			})
		}

		if v.IsEPUB() && v.ChapterCount() > 1 {
			foot := ui.Row(c.Key("book-side-foot")).Height(34).PaddingX(10).AlignItems(ui.Center).
				BorderWidth(1, 0, 0, 0).BorderColor(pg.TableLine)
			foot.Children(func() {
				ui.Text(c, fmt.Sprintf(i18n.T("第 %d / %d 章"), v.CurrentChapter+1, v.ChapterCount())).
					FontSize(11.5).TextColor(t.TextMuted).NoWrap()
			})
		}
	})
}

func (v *BookViewer) syncWebFind(wv HTMLWebView, forward, findNext bool) {
	if wv == nil {
		return
	}
	q := strings.TrimSpace(v.FindQuery)
	v.webFindQuery = v.FindQuery
	if q == "" {
		wv.StopFind()
		v.webFindActive, v.webFindTotal = 0, 0
		return
	}
	wv.Find(q, forward, findNext, func(active, total int) {
		v.webFindActive, v.webFindTotal = active, total
	})
}

func (v *BookViewer) closeFind() {
	v.FindOpen = false
	if v.activeWebView != nil {
		v.activeWebView.StopFind()
		v.webFindQuery = ""
		v.webFindActive, v.webFindTotal = 0, 0
	} else {
		v.syncSearchToEditor(false)
	}
	v.RequestFocus = true
}

func (v *BookViewer) findBarView(c *ui.Context, t *ui.Theme, pg Page) {
	bar := ui.Row(c.Key("book-find-bar")).Absolute().Right(16).Top(12).Width(364).Height(38).
		PaddingX(10).Gap(6).Radius(10).AlignItems(ui.Center).
		Background(pg.Popover).Border(1, pg.TableLine).Shadow(0, 6, 20, 0, pg.MathShadow)

	bar.Children(func() {
		ui.Icon(c, iconSearchPDF).FontSize(14).TextColor(t.TextMuted)
		in := ui.TextInput(c.Key("book-find-input"), &v.FindQuery).
			Placeholder(i18n.T("在文档中查找…")).Label(i18n.T("在文档中查找")).
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
				if v.activeWebView != nil {
					if v.FindQuery != v.webFindQuery {
						v.syncWebFind(v.activeWebView, true, false)
					} else {
						v.syncWebFind(v.activeWebView, ev.Mods&ui.Shift == 0, true)
					}
					return true
				}
				if v.FindQuery != v.findLastQuery {
					v.runSearch(v.FindQuery)
				} else if ev.Mods&ui.Shift != 0 {
					v.stepMatch(-1)
				} else {
					v.stepMatch(1)
				}
				return true
			case ui.KeyEscape:
				v.closeFind()
				return true
			}
			return false
		})

		noMatches := len(v.Matches) == 0
		if v.activeWebView != nil {
			noMatches = v.webFindTotal == 0
		}
		if strings.TrimSpace(v.FindQuery) != "" {
			status := i18n.T("无结果")
			if v.activeWebView != nil {
				if v.webFindTotal > 0 {
					status = fmt.Sprintf("%d/%d", v.webFindActive, v.webFindTotal)
				}
			} else if len(v.Matches) > 0 {
				if v.IsEPUB() && v.ChapterCount() > 1 {
					m := v.Matches[min(v.MatchIndex, len(v.Matches)-1)]
					status = fmt.Sprintf("%d/%d (Ch.%d)", v.MatchIndex+1, len(v.Matches), m.Chapter+1)
				} else {
					status = fmt.Sprintf("%d/%d", v.MatchIndex+1, len(v.Matches))
				}
			}
			ui.Text(c, status).FontSize(11.5).TextColor(t.TextMuted).NoWrap()
		}

		prevBtn := pdfIconBtn(c, "book-find-prev", iconArrowUp, i18n.T("上一个匹配 (⇧Enter)"), noMatches, pg, t)
		if prevBtn.Clicked() {
			if v.activeWebView != nil {
				v.syncWebFind(v.activeWebView, false, true)
			} else {
				v.stepMatch(-1)
			}
		}
		nextBtn := pdfIconBtn(c, "book-find-next", iconArrowDown, i18n.T("下一个匹配 (Enter)"), noMatches, pg, t)
		if nextBtn.Clicked() {
			if v.activeWebView != nil {
				v.syncWebFind(v.activeWebView, true, true)
			} else {
				v.stepMatch(1)
			}
		}
		closeBtn := pdfIconBtn(c, "book-find-close", iconClose, i18n.T("关闭 (Esc)"), false, pg, t)
		if closeBtn.Clicked() {
			v.closeFind()
		}
	})
}

func (v *BookViewer) floatingIslandView(c *ui.Context, t *ui.Theme, pg Page, wb WhiteboardStyle, availW float32, opts BookOptions) {
	cnt := v.ChapterCount()
	bg := wb.IslandFill
	border := wb.IslandBorder
	if bg.A == 0 {
		bg = pg.Popover
		border = pg.TableLine
	}

	holder := ui.Row(c.Key("book-island-wrap")).Absolute().Left(0).Right(0).Bottom(14).
		Justify(ui.Center).PassThrough()
	holder.Children(func() {
		island := ui.Row(c.Key("book-island")).Height(36).PaddingX(8).Gap(3).Radius(11).
			AlignItems(ui.Center).Background(bg).Border(1, border).
			Shadow(0, 6, 22, 0, pg.MathShadow)

		island.Children(func() {
			if !opts.Compact {
				sideBtn := pdfIconBtnActive(c, "book-btn-side", iconOutline, i18n.T("目录 (⇧⌘O)"), false, v.SidebarOpen, pg, t)
				if sideBtn.Clicked() {
					v.ToggleOutline()
				}
				pdfIslandDivider(c, pg)
			}

			if v.IsEPUB() && cnt > 1 {
				prev := pdfIconBtn(c, "book-btn-prev", iconChevronLeft, i18n.T("上一章"), v.CurrentChapter <= 0, pg, t)
				if prev.Clicked() {
					v.StepChapter(-1)
				}
				ui.Text(c, fmt.Sprintf(i18n.T("第 %d / %d 章"), v.CurrentChapter+1, cnt)).
					FontSize(12).FontWeight(550).TextColor(t.Text).PaddingX(6).NoWrap()
				next := pdfIconBtn(c, "book-btn-next", iconChevronRight, i18n.T("下一章"), v.CurrentChapter >= cnt-1, pg, t)
				if next.Clicked() {
					v.StepChapter(1)
				}
				pdfIslandDivider(c, pg)
			}

			if v.IsHTML() {
				if v.ShowSource {
					backBtn := ui.ButtonBase(c.Key("book-btn-src")).Height(26).PaddingX(9).Gap(5).Radius(7).
						AlignItems(ui.Center).Background(t.Accent.Alpha(0.15)).Cursor(ui.CursorPointer)
					backBtn.Children(func() {
						ui.Icon(c, iconCode).FontSize(13).TextColor(t.Accent)
						ui.Text(c, i18n.T("返回阅读视图")).FontSize(11.5).FontWeight(550).TextColor(t.Accent).NoWrap()
					})
					if backBtn.Clicked() {
						v.ToggleSource()
					}
				} else {
					srcBtn := pdfIconBtnActive(c, "book-btn-src", iconCode, i18n.T("查看 HTML 源码"), false, false, pg, t)
					if srcBtn.Clicked() {
						v.ToggleSource()
					}
				}
				if opts.OpenInBrowser != nil {
					brBtn := pdfIconBtn(c, "book-btn-browser", iconExternalLink, i18n.T("在默认浏览器中打开"), false, pg, t)
					if brBtn.Clicked() {
						opts.OpenInBrowser()
					}
				}
			}

			findBtn := pdfIconBtnActive(c, "book-btn-find", iconSearchPDF, i18n.T("在文档中查找 (⌘F)"), false, v.FindOpen, pg, t)
			if findBtn.Clicked() {
				if v.FindOpen {
					v.closeFind()
				} else {
					v.OpenFind()
				}
			}

			if opts.PinChapter != nil && v.IsEPUB() {
				pinned := opts.PinnedChapter == v.CurrentChapter+1
				pinTip := fmt.Sprintf(i18n.T("在文档中固定显示第 %d 章"), v.CurrentChapter+1)
				pinBtn := pdfIconBtnActive(c, "book-btn-pin", iconPinPage, pinTip, false, pinned, pg, t)
				if pinBtn.Clicked() {
					opts.PinChapter(v.CurrentChapter + 1)
				}
			}

			if opts.OpenSplit != nil && availW >= 480 {
				spBtn := pdfIconBtn(c, "book-btn-split", iconExternalSplit, i18n.T("在分屏中阅读"), false, pg, t)
				if spBtn.Clicked() {
					opts.OpenSplit(v.CurrentChapter+1, v.activeAnchor)
				}
			}

			if opts.ExpandModal != nil {
				maxBtn := pdfIconBtn(c, "book-btn-max", iconMaximize, i18n.T("全屏阅读"), false, pg, t)
				if maxBtn.Clicked() {
					opts.ExpandModal()
				}
			}

			if opts.Done != nil {
				pdfIslandDivider(c, pg)
				doneBtn := ui.ButtonBase(c.Key("book-btn-done")).Height(26).PaddingX(10).Radius(7).
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
