package app

import (
	"errors"
	"fmt"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/Ciao1019/ArcMD/internal/bookdoc"
	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/mdz"
	"github.com/Ciao1019/ArcMD/internal/pdfdoc"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	maxCachedMDZEditors = 6
	mdzRailWidth        = 216
)

type mdzState struct {
	archive        *mdz.Archive
	loadErr        error
	pages          []string
	currentPage    string
	sidebarOpen    bool
	history        []string
	historyAt      int
	editors        map[string]*editor.Editor
	editorOrder    []string
	images         map[string]editor.ImageResource
	whiteboards    map[string]*whiteboard.Scene
	pdfs           map[string]*pdfdoc.Document
	books          map[string]*bookdoc.Document
	requestFocus   bool
	pendingOutline bool

	previewReady   bool
	previewTitle   string
	previewExcerpt string
	previewCover   *ui.Bitmap
}

func (d *document) isMDZ() bool {
	return d != nil && d.mdz != nil
}

func (m *mdzState) hasActivePage() bool {
	return m != nil && m.archive != nil && m.currentPage != ""
}

func (m *mdzState) close() {
	if m == nil {
		return
	}
	for _, ed := range m.editors {
		if ed != nil {
			ed.ClearImages()
		}
	}
	m.editors = nil
	m.editorOrder = nil
	m.images = nil
	m.whiteboards = nil
	for _, pdfDoc := range m.pdfs {
		if pdfDoc != nil {
			pdfDoc.Close()
		}
	}
	m.pdfs = nil
	for _, bk := range m.books {
		if bk != nil {
			bk.Close()
		}
	}
	m.books = nil
	m.previewCover = nil
	if m.archive != nil {
		_ = m.archive.Close()
		m.archive = nil
	}
}

func (m *mdzState) pageCount() int {
	if m == nil {
		return 0
	}
	return len(m.pages)
}

func (m *mdzState) currentIndex() int {
	if m == nil || m.currentPage == "" {
		return -1
	}
	return slices.Index(m.pages, m.currentPage)
}

func (m *mdzState) activeEditor() *editor.Editor {
	if !m.hasActivePage() {
		return nil
	}
	if m.editors == nil {
		m.editors = make(map[string]*editor.Editor, maxCachedMDZEditors)
	}
	if ed, ok := m.editors[m.currentPage]; ok && ed != nil {
		m.touchEditor(m.currentPage)
		return ed
	}
	md, err := m.archive.ReadMarkdown(m.currentPage)
	if err != nil {
		doc := model.New()
		doc.Blocks = []*model.Block{model.NewBlock("paragraph", fmt.Sprintf(i18n.T("无法读取包内文档：%v"), err))}
		ed := editor.FromOwnedDocument(doc)
		ed.ReadOnly = true
		m.putEditor(m.currentPage, ed)
		return ed
	}
	doc := editor.ImportMarkdown(md)
	if m.currentPage == m.archive.EntryPoint() && doc.Cover == "" && m.archive.CoverPath() != "" {
		doc.Cover = relativeArchiveURL(m.currentPage, m.archive.CoverPath())
	}
	ed := editor.FromOwnedDocument(doc)
	ed.ReadOnly = true
	m.putEditor(m.currentPage, ed)
	return ed
}

func (m *mdzState) touchEditor(pagePath string) {
	if idx := slices.Index(m.editorOrder, pagePath); idx >= 0 {
		m.editorOrder = slices.Delete(m.editorOrder, idx, idx+1)
	}
	m.editorOrder = append(m.editorOrder, pagePath)
}

func (m *mdzState) putEditor(pagePath string, ed *editor.Editor) {
	if m.editors == nil {
		m.editors = make(map[string]*editor.Editor, maxCachedMDZEditors)
	}
	m.editors[pagePath] = ed
	m.touchEditor(pagePath)
	for len(m.editors) > maxCachedMDZEditors && len(m.editorOrder) > 0 {
		oldest := m.editorOrder[0]
		m.editorOrder = m.editorOrder[1:]
		if oldest == m.currentPage {
			continue
		}
		if oldEd, ok := m.editors[oldest]; ok {
			if oldEd != nil {
				oldEd.ClearImages()
			}
			delete(m.editors, oldest)
		}
	}
}

func (m *mdzState) openPage(pagePath string, recordHistory bool, fragment string) bool {
	if m == nil || m.archive == nil || pagePath == "" || !m.archive.HasFile(pagePath) || !mdz.IsMarkdownPath(pagePath) {
		return false
	}
	changed := m.currentPage != pagePath
	m.currentPage = pagePath
	ed := m.activeEditor()
	if recordHistory {
		if len(m.history) == 0 {
			m.history = []string{pagePath}
			m.historyAt = 0
		} else if changed {
			if m.historyAt >= 0 && m.historyAt+1 < len(m.history) {
				m.history = m.history[:m.historyAt+1]
			}
			m.history = append(m.history, pagePath)
			m.historyAt = len(m.history) - 1
		}
	}
	if ed != nil {
		if fragment != "" {
			if id := findHeadingByFragment(ed.Doc, fragment); id != "" {
				ed.ScrollToBlock(id)
			}
		} else if changed {
			ed.Scroll.Y = 0
		}
		ed.RequestFocus = true
	}
	return true
}

func (m *mdzState) canBack() bool {
	return m != nil && m.historyAt > 0 && m.historyAt < len(m.history)
}

func (m *mdzState) goBack() bool {
	if !m.canBack() {
		return false
	}
	m.historyAt--
	return m.openPage(m.history[m.historyAt], false, "")
}

func (m *mdzState) stepPage(delta int) bool {
	if !m.hasActivePage() || len(m.pages) <= 1 {
		return false
	}
	idx := m.currentIndex()
	if idx < 0 {
		return false
	}
	next := idx + delta
	if next < 0 || next >= len(m.pages) {
		return false
	}
	return m.openPage(m.pages[next], true, "")
}

func (m *mdzState) preserveFrom(old *mdzState) {
	if m == nil || old == nil {
		return
	}
	if len(m.pages) > 1 {
		m.sidebarOpen = old.sidebarOpen
	}
	if old.currentPage != "" && m.archive != nil && m.archive.HasFile(old.currentPage) && mdz.IsMarkdownPath(old.currentPage) {
		m.openPage(old.currentPage, true, "")
		if oldEd := old.editors[old.currentPage]; oldEd != nil {
			if newEd := m.activeEditor(); newEd != nil {
				newEd.Scroll = oldEd.Scroll
			}
		}
	}
}

func (m *mdzState) ensurePreview() {
	if m == nil || m.previewReady {
		return
	}
	m.previewReady = true
	if m.archive == nil {
		return
	}
	m.previewTitle = m.archive.Title()
	if cov := m.archive.CoverPath(); cov != "" {
		if data, err := m.archive.ReadFile(cov); err == nil {
			if bmp, decErr := ui.DecodeBitmapScaled(data, 480); decErr == nil {
				m.previewCover = bmp
			}
		}
	}
	pagePath := m.currentPage
	if pagePath == "" {
		pagePath = m.archive.EntryPoint()
	}
	if pagePath == "" && len(m.pages) > 0 {
		pagePath = m.pages[0]
	}
	if pagePath == "" {
		return
	}
	md, err := m.archive.ReadMarkdown(pagePath)
	if err != nil {
		return
	}
	plain, _ := editor.ExtractMarkdownText(md)
	var lines []string
	for _, line := range strings.Split(plain, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if len(lines) >= 4 {
			break
		}
	}
	m.previewExcerpt = strings.Join(lines, "\n")
}

func openMDZDocument(sp *space.Space, rel, diskStamp string) *document {
	d := &document{
		space:      sp,
		rel:        rel,
		diskText:   diskStamp,
		ed:         editor.FromOwnedDocument(model.New()),
		historyOff: true,
	}
	d.ed.ReadOnly = true
	d.initLazyBaseline()

	full, err := resolveFile(sp, rel)
	if err != nil {
		d.mdz = &mdzState{loadErr: err}
		return d
	}
	if info, stErr := os.Stat(full); stErr == nil {
		d.mdzModNano = info.ModTime().UnixNano()
		d.mdzSize = info.Size()
		if d.diskText == "" {
			d.diskText = fmt.Sprintf("mdz:%d:%d", d.mdzModNano, d.mdzSize)
		}
	}
	arc, openErr := mdz.Open(full)
	if openErr != nil && !errors.Is(openErr, mdz.ErrEntryPointUnresolved) {
		d.mdz = &mdzState{loadErr: openErr}
		return d
	}
	pages := orderedMDZPages(arc)
	m := &mdzState{
		archive:     arc,
		loadErr:     openErr,
		pages:       pages,
		sidebarOpen: len(pages) > 1 || (arc != nil && arc.Mode() == mdz.ModeProject),
	}
	if openErr == nil && arc != nil && arc.EntryPoint() != "" {
		m.openPage(arc.EntryPoint(), true, "")
	}
	d.mdz = m
	return d
}

func orderedMDZPages(arc *mdz.Archive) []string {
	if arc == nil {
		return nil
	}
	all := arc.MarkdownPages()
	ep := arc.EntryPoint()
	dirOf := func(p string) string {
		d := path.Dir(p)
		if d == "." {
			return ""
		}
		return d
	}
	epDir := ""
	if ep != "" {
		epDir = dirOf(ep)
	}
	dirRank := func(d string) int {
		if ep != "" && d == epDir {
			return 0
		}
		if d == "" {
			return 1
		}
		return 2
	}
	out := make([]string, 0, len(all))
	for _, p := range all {
		if p != ep {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b string) int {
		da, db := dirOf(a), dirOf(b)
		if ra, rb := dirRank(da), dirRank(db); ra != rb {
			return ra - rb
		}
		if cmp := strings.Compare(da, db); cmp != 0 {
			return cmp
		}
		return strings.Compare(path.Base(a), path.Base(b))
	})
	if ep != "" {
		out = append([]string{ep}, out...)
	}
	return out
}

func relativeArchiveURL(fromPage, targetPath string) string {
	fromDir := path.Dir(fromPage)
	if fromDir == "." || fromDir == "" {
		return encodeArchiveRelURL(targetPath)
	}
	rel, err := filepath.Rel(filepath.FromSlash(fromDir), filepath.FromSlash(targetPath))
	if err != nil {
		return encodeArchiveRelURL(targetPath)
	}
	return encodeArchiveRelURL(filepath.ToSlash(rel))
}

func (a *App) mdzLoadImage(d *document, rawURL string) editor.ImageResource {
	if d == nil || !d.isMDZ() || d.mdz.archive == nil {
		return editor.ImageResource{Problem: editor.ImageMissing, Local: true}
	}
	return a.mdzLoadImageFrom(d, d.mdz.currentPage, rawURL, true)
}

func (a *App) mdzLoadImageFrom(d *document, basePage, rawURL string, allowManifestCover bool) editor.ImageResource {
	if d == nil || !d.isMDZ() || d.mdz.archive == nil {
		return editor.ImageResource{Problem: editor.ImageMissing, Local: true}
	}
	m := d.mdz
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return editor.ImageResource{Problem: editor.ImageInvalid}
	}
	// Spec §9.2 & §13.2: never auto-load unencrypted http:// remote images.
	if strings.HasPrefix(strings.ToLower(trimmed), "http://") {
		return editor.ImageResource{Problem: editor.ImageRemote}
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		return a.remoteImageResource(d, trimmed)
	}
	var targetPath string
	if link := m.archive.ResolveLink(basePage, trimmed); link.Kind == mdz.LinkInternal && link.Found {
		targetPath = link.Path
	} else if allowManifestCover && m.currentPage == m.archive.EntryPoint() {
		if cov := m.archive.CoverPath(); cov != "" && trimmed == cov && m.archive.HasFile(cov) {
			targetPath = cov
		}
	}
	if targetPath == "" {
		return editor.ImageResource{Problem: editor.ImageMissing, Local: true}
	}
	if m.images == nil {
		m.images = make(map[string]editor.ImageResource)
	}
	if cached, ok := m.images[targetPath]; ok {
		return cached
	}
	data, err := m.archive.ReadFile(targetPath)
	if err != nil {
		res := editor.ImageResource{Problem: editor.ImageUnreadable, Local: true}
		m.images[targetPath] = res
		return res
	}
	res := decodeImageBytes(data, true)
	m.images[targetPath] = res
	return res
}

func (a *App) mdzLoadBookExternalImage(d *document, bookURL, imgSrc string) editor.ImageResource {
	if d == nil || !d.isMDZ() || d.mdz.archive == nil {
		return editor.ImageResource{Problem: editor.ImageMissing, Local: true}
	}
	basePage := d.mdz.currentPage
	if cleanBook, _, _ := editor.ParseBookURLRef(bookURL); cleanBook != "" {
		if res := d.mdz.archive.ResolveLink(basePage, cleanBook); res.Kind == mdz.LinkInternal && res.Found {
			basePage = res.Path
		}
	}
	return a.mdzLoadImageFrom(d, basePage, imgSrc, false)
}

func (a *App) mdzLoadWhiteboard(d *document, rawURL string) (*whiteboard.Scene, uint64, error) {
	if d == nil || !d.isMDZ() || d.mdz.archive == nil {
		return nil, 0, errors.New(i18n.T("找不到白板文件。"))
	}
	m := d.mdz
	base, _, _ := strings.Cut(strings.TrimSpace(rawURL), "#")
	res := m.archive.ResolveLink(m.currentPage, base)
	if res.Kind != mdz.LinkInternal || !res.Found {
		return nil, 0, errors.New(i18n.T("归档中找不到该白板文件。"))
	}
	if m.whiteboards == nil {
		m.whiteboards = make(map[string]*whiteboard.Scene)
	}
	if sc, ok := m.whiteboards[res.Path]; ok {
		return sc, 1, nil
	}
	data, err := m.archive.ReadFile(res.Path)
	if err != nil {
		return nil, 0, err
	}
	sc, err := whiteboard.Decode(string(data))
	if err != nil {
		return nil, 0, err
	}
	m.whiteboards[res.Path] = sc
	return sc, 1, nil
}

func (a *App) mdzLoadPDF(d *document, rawURL string) (*pdfdoc.Document, uint64, error) {
	if d == nil || !d.isMDZ() || d.mdz.archive == nil {
		return nil, 0, errors.New(i18n.T("找不到 PDF 文件。"))
	}
	m := d.mdz
	base, _, _ := strings.Cut(strings.TrimSpace(rawURL), "#")
	res := m.archive.ResolveLink(m.currentPage, base)
	if res.Kind != mdz.LinkInternal || !res.Found {
		return nil, 0, errors.New(i18n.T("归档中找不到该 PDF 文件。"))
	}
	if m.pdfs == nil {
		m.pdfs = make(map[string]*pdfdoc.Document)
	}
	if doc, ok := m.pdfs[res.Path]; ok {
		return doc, 1, nil
	}
	data, err := m.archive.ReadFile(res.Path)
	if err != nil {
		return nil, 0, err
	}
	doc, err := pdfdoc.OpenBytes(data)
	if err != nil {
		return nil, 0, err
	}
	m.pdfs[res.Path] = doc
	return doc, 1, nil
}

func (a *App) mdzLoadBook(d *document, rawURL string) (*bookdoc.Document, uint64, error) {
	if d == nil || !d.isMDZ() || d.mdz.archive == nil {
		return nil, 0, errors.New(i18n.T("找不到内嵌文档。"))
	}
	m := d.mdz
	base, _, _ := strings.Cut(strings.TrimSpace(rawURL), "#")
	res := m.archive.ResolveLink(m.currentPage, base)
	if res.Kind != mdz.LinkInternal || !res.Found {
		return nil, 0, errors.New(i18n.T("归档中找不到该内嵌文档。"))
	}
	if m.books == nil {
		m.books = make(map[string]*bookdoc.Document)
	}
	if bk, ok := m.books[res.Path]; ok {
		return bk, 1, nil
	}
	data, err := m.archive.ReadFile(res.Path)
	if err != nil {
		return nil, 0, err
	}
	bk, err := bookdoc.OpenBytes(res.Path, data)
	if err != nil {
		return nil, 0, err
	}
	m.books[res.Path] = bk
	return bk, 1, nil
}

func (a *App) mdzOpenLink(d *document, rawURL string) {
	if d == nil || !d.isMDZ() || d.mdz.archive == nil {
		return
	}
	m := d.mdz
	res := m.archive.ResolveLink(m.currentPage, rawURL)
	switch res.Kind {
	case mdz.LinkExternal:
		if ui.SafeToOpen(rawURL) {
			mygo.Shell.OpenExternal(rawURL)
		} else {
			a.notify("该链接协议不被支持，已阻止打开。")
		}
	case mdz.LinkInternal:
		if res.Found && mdz.IsMarkdownPath(res.Path) {
			m.openPage(res.Path, true, res.Fragment)
			return
		}
		if res.Found {
			a.notify("包内文件：%s（解包后即可打开）", res.Path)
			return
		}
		a.notify("归档中找不到链接目标：%s", rawURL)
	case mdz.LinkOutsideArchive:
		a.notify("该链接指向归档外部文件，已按安全策略拦截。")
	default:
		if res.NearMatch != "" {
			a.notify("归档中找不到该文件（大小写敏感，是否指 %s？）", res.NearMatch)
			return
		}
		a.notify("归档中找不到链接目标：%s", rawURL)
	}
}

func (a *App) mdzOpenWiki(d *document, name string) {
	if d == nil || !d.isMDZ() || d.mdz.archive == nil {
		return
	}
	m := d.mdz
	target, frag, _ := strings.Cut(strings.TrimSpace(name), "#")
	if target == "" {
		return
	}
	// Spec §9.7: resolve wikilinks against archive documents first.
	for _, p := range m.pages {
		stem := vault.Title(p)
		if strings.EqualFold(stem, target) || strings.EqualFold(p, target) || strings.EqualFold(p, target+".md") {
			m.openPage(p, true, frag)
			return
		}
	}
	a.notify("归档中找不到双链文档「%s」。", target)
}

func (a *App) mdzEditorOptions(c *ui.Context, width float32, focus, showRail bool, d *document) editor.Options {
	opts := a.editorOptionsFor(c, width, focus, d)
	if d != nil && d.isMDZ() && d.mdz.archive != nil {
		title := docName(d.title())
		if d.mdz.currentPage != "" && d.mdz.currentPage != d.mdz.archive.EntryPoint() {
			title = vault.Title(d.mdz.currentPage)
		} else if mt := d.mdz.archive.Title(); mt != "" {
			title = mt
		}
		opts.DocTitle = title
	}
	// Read-only archive: disable every mutating or disk-writing callback.
	opts.Attach = nil
	opts.DropFiles = nil
	opts.PasteImage = nil
	opts.ReplaceImage = nil
	opts.RevealImage = nil
	opts.CropImage = nil
	opts.PickCover = nil
	opts.Footer = nil
	opts.Bookmarks = nil
	opts.ToggleBookmark = nil
	opts.LabelBookmark = nil
	opts.CreateWhiteboard = nil
	opts.SaveWhiteboard = nil
	opts.OpenWhiteboard = nil
	opts.SaveBlockAsWhiteboard = nil
	opts.OpenPDF = nil
	opts.AttachPDF = nil
	opts.OpenBook = nil
	opts.OpenBookInBrowser = nil
	opts.HTMLWebView = nil
	opts.AttachHTML = nil
	opts.AttachEPUB = nil
	opts.LoadDatabase = nil
	opts.OpenDatabase = nil
	opts.ListDatabases = nil
	opts.CreateDatabase = nil
	opts.DatabaseSnapshot = nil
	opts.OpenTag = nil
	opts.PreviewWiki = nil
	opts.WikiSuggest = nil
	opts.BoardWikiSuggest = nil
	opts.OpenWikiSplit = nil

	// Wire purely in-memory archive resource loaders and safe link navigation.
	opts.DirectLinks = true
	opts.ReadOnlyNotice = i18n.T("MDZip 归档为只读 · 点击右上角「解包另存为文档」即可编辑")
	opts.LoadImage = func(u string) editor.ImageResource { return a.mdzLoadImage(d, u) }
	opts.LoadWhiteboard = func(u string) (*whiteboard.Scene, uint64, error) { return a.mdzLoadWhiteboard(d, u) }
	opts.LoadPDF = func(u string) (*pdfdoc.Document, uint64, error) { return a.mdzLoadPDF(d, u) }
	opts.LoadBook = func(u string) (*bookdoc.Document, uint64, error) { return a.mdzLoadBook(d, u) }
	opts.LoadBookExternalImg = func(bookURL, imgSrc string) editor.ImageResource {
		return a.mdzLoadBookExternalImage(d, bookURL, imgSrc)
	}
	opts.OpenLink = func(u string) { a.mdzOpenLink(d, u) }
	opts.OpenWiki = func(name string) { a.mdzOpenWiki(d, name) }
	if showRail {
		// Avoid stacking the editor heading outline next to the multi-page archive rail.
		opts.Outline = false
		opts.SetOutline = nil
	}
	return opts
}

func (a *App) mdzCorner(c *ui.Context, d *document) {
	if d == nil || !d.isMDZ() || d.mdz == nil {
		return
	}
	m := d.mdz
	t := c.Theme()
	ui.Row(c.Key("mdz-corner")).Gap(4).AlignItems(ui.Center).Children(func() {
		if a.showPageZoomButton(d) {
			a.pageZoomButton(c, d)
		}
		if a.canAdjustPageWidth(d) {
			a.pageWidthButton(c, d)
		}
		if m.hasActivePage() && len(m.pages) > 1 {
			ui.Row(c.Key("mdz-page-count")).Height(28).PaddingX(8).Radius(7).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf(i18n.T("%d / %d 页"), m.currentIndex()+1, len(m.pages))).
					FontSize(12).TextColor(t.TextMuted).NoWrap()
			})
			tip := i18n.T("展开包内文件")
			if m.sidebarOpen {
				tip = i18n.T("收起包内文件")
			}
			rail := iconButton(c, "mdz-rail", iconSidebar, tip, tip, false)
			if m.sidebarOpen {
				if !a.inWindow(d) && a.split == nil {
					rail.Background(glass(t))
				} else {
					rail.Background(t.Accent.Alpha(.14))
				}
			}
			if rail.Clicked() {
				m.sidebarOpen = !m.sidebarOpen
			}
		}
		if m.hasActivePage() {
			if ed := m.activeEditor(); ed != nil {
				find := iconButton(c, "mdz-find", iconSearch, i18n.T("在文档中查找"), i18n.T("在文档中查找")+"  ⌘F", false)
				if _, open := ed.FindQuery(); open {
					find.Background(glass(t))
				}
				if find.Clicked() {
					ed.OpenFind(false)
				}
			}
		}
		ui.Row(c.Key("mdz-badge")).Height(22).PaddingX(7).Radius(6).Background(t.Text.Alpha(0.08)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "MDZ").FontSize(10.5).FontWeight(600).TextColor(t.TextMuted).NoWrap()
		})
		if m.hasActivePage() {
			unpack := ui.ButtonBase(c.Key("mdz-unpack")).Height(28).PaddingX(10).Radius(8).Center().
				Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("解包另存为文档")).
				Tooltip(i18n.T("将当前 MDZip 页面及附件解包为可编辑的 Markdown 文档"))
			if unpack.Hovered() {
				unpack.Background(hoverFill(t))
			} else if !a.inWindow(d) && a.split == nil {
				unpack.Background(glass(t))
			} else {
				unpack.Background(t.Accent.Alpha(.14))
			}
			unpack.Children(func() {
				ui.Text(c, i18n.T("解包另存为文档")).FontSize(12).FontWeight(500).TextColor(t.Text).NoWrap()
			})
			if unpack.Clicked() {
				a.unpackMDZAsDocument(d)
			}
		}
	})
}

func (a *App) mdzView(c *ui.Context, d *document, width float32, focus bool) {
	if d == nil || !d.isMDZ() || d.mdz == nil {
		return
	}
	m := d.mdz
	dt := documentTheme(a.paperDark)

	// Case 1: Fatal load or spec validation error.
	if m.archive == nil {
		ui.Column(c.Key("mdz-error")).Grow(1).Center().Padding(32).Gap(12).Children(func() {
			ui.Icon(c, iconAlert).FontSize(28).TextColor(dt.TextMuted)
			ui.Text(c, i18n.T("无法打开 MDZip 文档")).FontSize(16).FontWeight(600).TextColor(dt.Text)
			ui.Text(c, mdzErrorMessage(m.loadErr)).FontSize(13).TextColor(dt.TextMuted).MaxWidth(440).TextAlign(ui.Center)
			if d.space != nil && d.rel != "" {
				if ui.Button(c.Key("mdz-err-reveal"), i18n.T("在访达中显示")).Clicked() {
					a.revealIn(d.space, d.rel)
				}
			}
		})
		return
	}

	// Case 2: Valid archive, but no unambiguous entry point (Spec §5.5 rule 4).
	if !m.hasActivePage() {
		ui.Column(c.Key("mdz-picker")).Grow(1).Center().Padding(32).Gap(14).Children(func() {
			ui.Icon(c, iconFileArchive).FontSize(28).TextColor(dt.Accent)
			if len(m.pages) == 0 {
				ui.Text(c, i18n.T("压缩包内没有 Markdown 文档")).FontSize(16).FontWeight(600).TextColor(dt.Text)
				ui.Text(c, i18n.T("该 .mdz 归档中未找到可阅读的 .md 或 .markdown 文件。")).FontSize(13).TextColor(dt.TextMuted).MaxWidth(420).TextAlign(ui.Center)
				if d.space != nil && d.rel != "" {
					if ui.Button(c.Key("mdz-empty-reveal"), i18n.T("在访达中显示")).Clicked() {
						a.revealIn(d.space, d.rel)
					}
				}
				return
			}
			ui.Text(c, i18n.T("请选择要打开的文档")).FontSize(16).FontWeight(600).TextColor(dt.Text)
			ui.Text(c, i18n.T("该 MDZip 归档未声明入口文件且包含多个文档，请手动选择一篇开始阅读。")).
				FontSize(13).TextColor(dt.TextMuted).MaxWidth(440).TextAlign(ui.Center)
			boxW := min(float32(460), max(float32(260), width-64))
			ui.Scroll(c.Key("mdz-picker-list")).Width(boxW).MaxHeight(300).
				Radius(10).Border(1, dt.Border).Background(dt.Background.Mix(dt.Text, 0.02)).Padding(6).Children(func() {
				for _, pagePath := range m.pages {
					p := pagePath
					row := ui.Row(c.Key("pick:" + p)).Height(36).PaddingX(10).Radius(7).Gap(8).
						AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(p)
					if row.Hovered() {
						row.Background(dt.Text.Alpha(0.06))
					}
					row.Children(func() {
						ui.Icon(c, iconFile).FontSize(14).TextColor(dt.TextMuted)
						ui.Text(c, p).FontSize(13).TextColor(dt.Text).SingleLine().Grow(1).MinWidth(0)
					})
					if row.Clicked() {
						m.openPage(p, true, "")
					}
				}
			})
		})
		return
	}

	// Case 3: Active read-only Markdown page (with optional collapsible multi-page rail).
	ed := m.activeEditor()
	if ed == nil {
		return
	}
	if m.requestFocus {
		ed.RequestFocus = true
		m.requestFocus = false
	}
	showRail := len(m.pages) > 1 && m.sidebarOpen && !focus && width >= 540
	editorW := width
	if showRail {
		editorW = max(320, width-mdzRailWidth)
	}
	ui.Row(c.Key("mdz-body")).Grow(1).MinHeight(0).MinWidth(0).Gap(0).AlignItems(ui.Stretch).Children(func() {
		if showRail {
			a.mdzPageRail(c, d, dt)
			ui.Box(c).Width(1).Shrink(0).AlignSelf(ui.Stretch).Background(dt.Border)
		}
		ui.Column(c.Key("mdz-editor-wrap")).Grow(1).MinHeight(0).MinWidth(0).AlignSelf(ui.Stretch).Children(func() {
			editor.View(c.Key(fmt.Sprintf("mdz:%p:%s", m, m.currentPage)), ed, a.mdzEditorOptions(c, editorW, focus, showRail, d))
		})
	})
	if m.pendingOutline {
		m.pendingOutline = false
		if !showRail && !ed.OutlineOpen() {
			ed.ToggleOutline()
		}
	}
}

func (a *App) mdzPageRail(c *ui.Context, d *document, dt *ui.Theme) {
	m := d.mdz
	railBg := dt.Background.Mix(dt.Text, 0.025)
	ui.Column(c.Key("mdz-rail-pane")).Width(mdzRailWidth).Shrink(0).MinHeight(0).AlignSelf(ui.Stretch).
		Background(railBg).Children(func() {
		ui.Row(c.Key("mdz-rail-head")).Height(40).PaddingX(12).Gap(6).AlignItems(ui.Center).Children(func() {
			label := i18n.T("包内文件")
			if m.archive != nil && m.archive.Mode() == mdz.ModeProject {
				label = i18n.T("项目文档")
			}
			ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(dt.TextMuted).SingleLine().Grow(1).MinWidth(0)
			if m.canBack() {
				back := ui.Box(c.Key("mdz-rail-back")).Size(24, 24).Radius(6).Center().
					Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("返回上一页")).Tooltip(i18n.T("返回上一页"))
				if back.Hovered() {
					back.Background(dt.Text.Alpha(0.08))
				}
				back.Children(func() {
					ui.Icon(c, iconBack).FontSize(13).TextColor(dt.TextMuted)
				})
				if back.Clicked() {
					m.goBack()
				}
			}
			ui.Box(c).Padding(2, 6).Radius(5).Background(dt.Text.Alpha(0.06)).Children(func() {
				ui.Text(c, fmt.Sprintf("%d", len(m.pages))).FontSize(10.5).FontWeight(500).TextColor(dt.TextMuted).NoWrap()
			})
		})
		ui.Box(c).Height(1).Shrink(0).Background(dt.Border.Alpha(0.6))
		ui.Scroll(c.Key("mdz-rail-scroll")).Grow(1).MinHeight(0).Padding(6).Children(func() {
			lastDir := ""
			for i, pagePath := range m.pages {
				p := pagePath
				dir := path.Dir(p)
				if dir == "." {
					dir = ""
				}
				if dir != "" && dir != lastDir {
					ui.Row(c.Key(fmt.Sprintf("mdz-dir:%d:%s", i, dir))).Height(24).Padding(6, 8, 2, 8).Gap(5).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, iconFolder).FontSize(12).TextColor(dt.TextMuted.Alpha(0.75))
						ui.Text(c, dir).FontSize(11).FontWeight(500).TextColor(dt.TextMuted).SingleLine().Grow(1).MinWidth(0)
					})
					lastDir = dir
				} else if dir == "" {
					lastDir = ""
				}
				active := p == m.currentPage
				padLeft := float32(8)
				if dir != "" {
					padLeft = 18
				}
				row := ui.Row(c.Key("mdz-page:"+p)).Height(30).Padding(0, 8, 0, padLeft).Radius(7).Gap(6).
					AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(p).Tooltip(p)
				switch {
				case active:
					row.Background(dt.Accent.Alpha(0.14))
				case row.Hovered():
					row.Background(dt.Text.Alpha(0.06))
				}
				row.Children(func() {
					iconColor := dt.TextMuted
					textColor := dt.Text
					weight := 400
					if active {
						iconColor = dt.Accent
						textColor = dt.Accent
						weight = 600
					}
					ui.Icon(c, iconFile).FontSize(13).TextColor(iconColor)
					ui.Text(c, path.Base(p)).FontSize(12.5).FontWeight(weight).TextColor(textColor).SingleLine().Grow(1).MinWidth(0)
				})
				if row.Clicked() {
					m.openPage(p, true, "")
				}
			}
		})
	})
}

func mdzErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, mdz.ErrZipEncrypted):
		return i18n.T("该 .mdz 归档包含加密条目，MDZip 规范不支持打开加密压缩包。")
	case errors.Is(err, mdz.ErrVersionUnsupported):
		return i18n.T("该 .mdz 文件使用了更高主版本的 MDZip 规范，当前版本的 ArcMD 暂不支持。")
	case errors.Is(err, mdz.ErrModeUnsupported):
		return i18n.T("该 .mdz 清单声明了当前不支持的归档模式（mode）。")
	case errors.Is(err, mdz.ErrEntryPointInvalid):
		return i18n.T("manifest.json 中声明的入口文件路径无效。")
	case errors.Is(err, mdz.ErrEntryPointMissing):
		return fmt.Sprintf(i18n.T("manifest.json 中声明的入口文件不存在：%v"), err)
	case errors.Is(err, mdz.ErrManifestInvalid):
		return i18n.T("该 .mdz 中的 manifest.json 格式损坏或包含非法字段。")
	case errors.Is(err, mdz.ErrPathInvalid), errors.Is(err, mdz.ErrPathConflict), errors.Is(err, mdz.ErrDuplicatePath):
		return i18n.T("该 .mdz 归档包含不安全或冲突的文件路径，已按安全规则拒绝打开。")
	case errors.Is(err, mdz.ErrLimitExceeded):
		return i18n.T("该 .mdz 归档超出了安全解压体积或文件数量限制。")
	case errors.Is(err, mdz.ErrZipInvalid):
		return i18n.T("文件已损坏或不是有效的 ZIP / MDZip 压缩包。")
	default:
		return err.Error()
	}
}

func findHeadingByFragment(doc *model.Document, fragment string) string {
	if doc == nil || fragment == "" {
		return ""
	}
	decoded, err := neturl.PathUnescape(fragment)
	if err != nil {
		decoded = fragment
	}
	wantExact := strings.ToLower(strings.TrimSpace(decoded))
	wantSlug := headingAnchorSlug(decoded)

	var match string
	var walk func([]*model.Block)
	walk = func(blocks []*model.Block) {
		for _, b := range blocks {
			if match != "" {
				return
			}
			if b.Kind == "heading" {
				txt := strings.TrimSpace(b.Text())
				if strings.ToLower(txt) == wantExact || (wantSlug != "" && headingAnchorSlug(txt) == wantSlug) {
					match = b.ID
					return
				}
			}
			if len(b.Children) > 0 {
				walk(b.Children)
			}
		}
	}
	walk(doc.Blocks)
	return match
}

func headingAnchorSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			b.WriteRune(r)
			prevDash = false
		case r == '-' || unicode.IsSpace(r):
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// exportMDZ bundles the open Markdown document together with its cover, local
// images, embedded whiteboards/PDFs/books, and local non-Markdown linked files
// into a conforming MDZip v1.2.0 (.mdz) archive beside the document.
func (a *App) exportMDZ() {
	d := a.doc
	if d == nil || d.loadErr != nil || d.isWhiteboard() || d.isPDF() || d.isBook() || d.isDatabase() || d.isMDZ() {
		return
	}
	d.ed.UnmarkText()
	root, nameOnDisk, err := fileLocation(d.space, d.rel)
	if err != nil {
		a.notify("导出失败。%v", err)
		return
	}
	docFull, err := vault.Resolve(root, nameOnDisk)
	if err != nil {
		a.notify("导出失败。%v", err)
		return
	}
	canonDocDir := canonicalPath(filepath.Dir(docFull))

	srcDoc := a.documentForExport(d)
	if srcDoc == nil {
		return
	}
	cloned := srcDoc.Clone()

	byDisk := make(map[string]string)
	usedNFC := map[string]bool{
		mdz.ManifestPath: true,
		"index.md":       true,
	}
	usedFold := map[string]bool{
		mdz.ManifestPath: true,
		"index.md":       true,
	}
	usedDirs := make(map[string]bool)
	recordArchivePath := func(archPath string) {
		nfc := norm.NFC.String(archPath)
		usedNFC[nfc] = true
		usedFold[cases.Fold().String(nfc)] = true
		for idx := strings.IndexByte(nfc, '/'); idx >= 0; {
			usedDirs[nfc[:idx]] = true
			next := strings.IndexByte(nfc[idx+1:], '/')
			if next < 0 {
				break
			}
			idx += 1 + next
		}
	}
	conflictsPath := func(archPath string) bool {
		nfc := norm.NFC.String(archPath)
		if usedNFC[nfc] || usedFold[cases.Fold().String(nfc)] || usedDirs[nfc] {
			return true
		}
		for idx := strings.IndexByte(nfc, '/'); idx >= 0; {
			if usedNFC[nfc[:idx]] {
				return true
			}
			next := strings.IndexByte(nfc[idx+1:], '/')
			if next < 0 {
				break
			}
			idx += 1 + next
		}
		return false
	}

	var files []mdz.File
	missing := 0
	anyRewritten := false

	allocAssetsPath := func(origBase string) string {
		dir := assetsDir
		for d := 2; usedNFC[dir] || usedFold[cases.Fold().String(dir)]; d++ {
			dir = fmt.Sprintf("%s-%d", assetsDir, d)
		}
		cleanBase := sanitizeMDZFilename(origBase)
		ext := vault.FileExt(cleanBase)
		stem := strings.TrimSuffix(cleanBase, ext)
		if stem == "" {
			stem = "asset"
		}
		for i := 1; ; i++ {
			cand := cleanBase
			if i > 1 {
				cand = fmt.Sprintf("%s %d%s", stem, i, ext)
			}
			archPath := path.Join(dir, cand)
			if mdz.ValidateEntryPath(archPath, false) == nil && !conflictsPath(archPath) {
				return norm.NFC.String(archPath)
			}
		}
	}

	addAsset := func(rawRef string, isInlineLink bool) (string, string, bool) {
		trimmed := strings.TrimSpace(rawRef)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "data:") || strings.HasPrefix(trimmed, "gradient:") || hasSchemePrefix(trimmed) {
			return rawRef, "", false
		}
		pathAndQuery, frag, hasFrag := strings.Cut(trimmed, "#")
		cleanBase, query, hasQuery := strings.Cut(pathAndQuery, "?")
		if cleanBase == "" {
			return rawRef, "", false
		}
		if isInlineLink {
			// Skip links to other Markdown documents or archives; only bundle local non-Markdown files.
			decodedBase := unescapeImagePath(cleanBase)
			if vault.IsMarkdown(decodedBase) || mdz.IsMarkdownPath(decodedBase) || vault.IsMDZ(decodedBase) || strings.HasSuffix(decodedBase, "/") {
				return rawRef, "", false
			}
		}
		full, rErr := resolveSpaceImage(d.space, d.rel, cleanBase)
		if rErr != nil {
			if !isInlineLink {
				missing++
			}
			return rawRef, "", false
		}
		info, stErr := os.Stat(full)
		if stErr != nil || info.IsDir() {
			if !isInlineLink {
				missing++
			}
			return rawRef, "", false
		}
		canon := canonicalPath(full)
		archPath, exists := byDisk[canon]
		if !exists {
			data, readErr := os.ReadFile(full)
			if readErr != nil {
				if !isInlineLink {
					missing++
				}
				return rawRef, "", false
			}
			// Keep existing relative path untouched when the file is already under the
			// document's directory (e.g. default assets/photo.png) and valid per Spec §5.4.
			relFromDoc, relErr := filepath.Rel(canonDocDir, canon)
			relSlash := norm.NFC.String(filepath.ToSlash(relFromDoc))
			decodedClean := norm.NFC.String(strings.TrimPrefix(unescapeImagePath(cleanBase), "./"))
			if relErr == nil && filepath.IsLocal(relFromDoc) &&
				mdz.ValidateEntryPath(relSlash, false) == nil &&
				!conflictsPath(relSlash) &&
				decodedClean == relSlash {
				archPath = relSlash
			} else {
				archPath = allocAssetsPath(filepath.Base(full))
			}
			recordArchivePath(archPath)
			byDisk[canon] = archPath
			files = append(files, mdz.File{Path: archPath, Data: data})
		}

		// Determine whether the Markdown reference needs rewriting.
		decodedClean := norm.NFC.String(strings.TrimPrefix(unescapeImagePath(cleanBase), "./"))
		if decodedClean == archPath && !hasQuery && !strings.HasPrefix(cleanBase, "./") {
			return rawRef, archPath, false
		}
		rewrittenURL := encodeArchiveRelURL(archPath)
		if hasQuery && query != "" {
			rewrittenURL += "?" + query
		}
		if hasFrag {
			rewrittenURL += "#" + frag
		}
		return rewrittenURL, archPath, rewrittenURL != rawRef
	}

	var manifestCover string
	if cloned.Cover != "" && !strings.HasPrefix(cloned.Cover, "gradient:") {
		nextCover, archCover, changed := addAsset(cloned.Cover, false)
		if archCover != "" {
			manifestCover = archCover
		}
		if changed {
			cloned.Cover = nextCover
			anyRewritten = true
		}
	}

	var walk func([]*model.Block)
	walk = func(blocks []*model.Block) {
		for _, b := range blocks {
			if b == nil {
				continue
			}
			if b.Kind == "image" && b.URL != "" {
				if nextURL, _, changed := addAsset(b.URL, false); changed {
					b.URL = nextURL
					anyRewritten = true
				}
			}
			for i := range b.Runs {
				if b.Runs[i].Image.URL != "" {
					if nextURL, _, changed := addAsset(b.Runs[i].Image.URL, false); changed {
						b.Runs[i].Image.URL = nextURL
						anyRewritten = true
					}
				}
				if b.Runs[i].Style.Link != "" {
					if nextURL, _, changed := addAsset(b.Runs[i].Style.Link, true); changed {
						b.Runs[i].Style.Link = nextURL
						anyRewritten = true
					}
				}
			}
			if len(b.Children) > 0 {
				walk(b.Children)
			}
		}
	}
	walk(cloned.Blocks)

	if anyRewritten {
		// Clear ImportSource so ExportMarkdown emits the rewritten URLs instead of verbatim original chunks.
		cloned.ImportSource = ""
	}
	mdContent := editor.ExportMarkdown(cloned)
	allFiles := make([]mdz.File, 0, len(files)+1)
	allFiles = append(allFiles, mdz.File{Path: "index.md", Data: []byte(mdContent)})
	allFiles = append(allFiles, files...)

	now := time.Now().UTC()
	archiveBytes, err := mdz.Build(mdz.WriteOptions{
		Manifest: mdz.Manifest{
			Spec: &mdz.SpecInfo{Name: mdz.SpecName, Version: mdz.SpecVersion},
			Producer: &mdz.ProducerInfo{
				Application: &mdz.ToolInfo{
					Name:    "ArcMD",
					Version: Version,
					URL:     "https://github.com/Ciao1019/ArcMD",
				},
			},
			Title:      docName(d.title()),
			Mode:       mdz.ModeDocument,
			EntryPoint: "index.md",
			Language:   htmlLang(),
			Cover:      manifestCover,
			Created:    &mdz.Timestamp{When: now.Format(time.RFC3339)},
			Modified:   &mdz.Timestamp{When: now.Format(time.RFC3339)},
		},
		Files:   allFiles,
		ModTime: now,
	})
	if err != nil {
		a.notify("导出失败。%v", err)
		return
	}

	dir := path.Dir(nameOnDisk)
	if dir == "." {
		dir = ""
	}
	rel, err := createNumbered(root, dir, d.title(), vault.ExtMDZ, archiveBytes)
	if err != nil {
		a.notify("导出失败。%v", err)
		return
	}
	delete(a.pageTrees, d.space.ID)
	a.refreshTree()
	a.refreshSearch()
	if missing > 0 {
		a.notify("已导出 %s（%d 个缺失附件未打包）", rel, missing)
	} else {
		a.notify("已导出 %s", rel)
	}
	if a.win != nil {
		if full, rErr := vault.Resolve(root, rel); rErr == nil {
			mygo.Shell.ShowItemInFolder(full)
		}
	}
}

// unpackMDZAsDocument extracts the active page (and all in-archive assets it
// references) from an .mdz archive into an editable .md document and assets
// folder inside a regular space, never overwriting existing files.
func (a *App) unpackMDZAsDocument(d *document) {
	if d == nil || !d.isMDZ() || d.mdz == nil || d.mdz.archive == nil {
		return
	}
	m := d.mdz
	pagePath := m.currentPage
	if pagePath == "" {
		pagePath = m.archive.EntryPoint()
	}
	if pagePath == "" && len(m.pages) > 0 {
		pagePath = m.pages[0]
	}
	if pagePath == "" {
		a.notify("压缩包内没有可解包的 Markdown 文档。")
		return
	}
	targetSp := d.space
	dir := path.Dir(d.rel)
	if dir == "." {
		dir = ""
	}
	if targetSp == nil || targetSp.IsExternal() {
		targetSp = nil
		for _, sp := range a.state.Spaces {
			if !sp.IsExternal() {
				targetSp = sp
				break
			}
		}
		dir = ""
	}
	if targetSp == nil {
		a.notify("请先选择一个空间以解包文档。")
		return
	}

	mdText, err := m.archive.ReadMarkdown(pagePath)
	if err != nil {
		a.notify("解包失败。%v", err)
		return
	}
	doc := editor.ImportMarkdown(mdText)
	fromManifestCover := false
	if pagePath == m.archive.EntryPoint() && doc.Cover == "" && m.archive.CoverPath() != "" {
		doc.Cover = relativeArchiveURL(pagePath, m.archive.CoverPath())
		fromManifestCover = true
	}

	extracted := make(map[string]string) // archive path -> relative URL from unpacked .md
	anyRewritten := false
	extractAsset := func(rawRef string, isLink, allowManifestCover bool) (string, bool) {
		trimmed := strings.TrimSpace(rawRef)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "data:") || strings.HasPrefix(trimmed, "gradient:") || hasSchemePrefix(trimmed) {
			return rawRef, false
		}
		var targetArch string
		_, frag, hasFrag := strings.Cut(trimmed, "#")
		res := m.archive.ResolveLink(pagePath, trimmed)
		if res.Kind == mdz.LinkInternal && res.Found {
			if isLink && mdz.IsMarkdownPath(res.Path) {
				return rawRef, false
			}
			targetArch = res.Path
		} else if allowManifestCover {
			if cov := m.archive.CoverPath(); cov != "" && trimmed == cov && m.archive.HasFile(cov) {
				targetArch = cov
				hasFrag = false
				frag = ""
			}
		}
		if targetArch == "" {
			return rawRef, false
		}
		relURL, ok := extracted[targetArch]
		if !ok {
			data, rErr := m.archive.ReadFile(targetArch)
			if rErr != nil {
				return rawRef, false
			}
			writtenURL, wErr := writeUnpackedAsset(targetSp, dir, path.Base(targetArch), data)
			if wErr != nil {
				return rawRef, false
			}
			extracted[targetArch] = writtenURL
			relURL = writtenURL
		}
		next := relURL
		if hasFrag {
			next += "#" + frag
		}
		return next, next != rawRef
	}

	if doc.Cover != "" && !strings.HasPrefix(doc.Cover, "gradient:") {
		if nextCover, changed := extractAsset(doc.Cover, false, fromManifestCover); changed {
			doc.Cover = nextCover
			anyRewritten = true
		}
	}
	var walk func([]*model.Block)
	walk = func(blocks []*model.Block) {
		for _, b := range blocks {
			if b == nil {
				continue
			}
			if b.Kind == "image" && b.URL != "" {
				if nextURL, changed := extractAsset(b.URL, false, false); changed {
					b.URL = nextURL
					anyRewritten = true
				}
			}
			for i := range b.Runs {
				if b.Runs[i].Image.URL != "" {
					if nextURL, changed := extractAsset(b.Runs[i].Image.URL, false, false); changed {
						b.Runs[i].Image.URL = nextURL
						anyRewritten = true
					}
				}
				if b.Runs[i].Style.Link != "" {
					if nextURL, changed := extractAsset(b.Runs[i].Style.Link, true, false); changed {
						b.Runs[i].Style.Link = nextURL
						anyRewritten = true
					}
				}
			}
			if len(b.Children) > 0 {
				walk(b.Children)
			}
		}
	}
	walk(doc.Blocks)

	if anyRewritten || (pagePath == m.archive.EntryPoint() && doc.Cover != "") {
		doc.ImportSource = ""
	}
	outMD := editor.ExportMarkdown(doc)

	title := docName(d.title())
	if len(m.pages) > 1 && pagePath != m.archive.EntryPoint() {
		if subTitle := strings.TrimSpace(vault.Title(pagePath)); subTitle != "" {
			title = title + " - " + subTitle
		}
	}
	newRel, err := vault.CreateFile(targetSp.Root, dir, title, ".md", outMD)
	if err != nil {
		a.notify("解包失败。%v", err)
		return
	}
	delete(a.pageTrees, targetSp.ID)
	a.refreshTree()
	a.refreshSearch()
	a.openAnywhere(targetSp.ID, newRel)
	a.notify("已解包为 %s", newRel)
}

func writeUnpackedAsset(sp *space.Space, docDir, origName string, data []byte) (string, error) {
	writeDir := path.Join(docDir, assetsDir)
	urlDir := assetsDir
	folderMode := false
	if folder := attachmentFolder(sp); folder != "" {
		writeDir, urlDir, folderMode = folder, folder, true
	}
	base := assetName(origName)
	ext := vault.FileExt(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i < 1000; i++ {
		cand := base
		if i > 1 {
			cand = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		rel := path.Join(writeDir, cand)
		full, err := vault.Resolve(sp.Root, rel)
		if err != nil {
			return "", err
		}
		if _, err := os.Lstat(full); err == nil {
			continue
		}
		if err := fsutil.CreateFileAtomic(full, data, 0o644); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return "", err
		}
		if folderMode {
			return encodeArchiveRelURL(relativeAssetURL(docDir, path.Join(urlDir, cand))), nil
		}
		return encodeArchiveRelURL(path.Join(urlDir, cand)), nil
	}
	return "", errors.New("no free asset name")
}

func relativeAssetURL(docDir, targetRel string) string {
	if docDir == "" || docDir == "." {
		return targetRel
	}
	rel, err := filepath.Rel(filepath.FromSlash(docDir), filepath.FromSlash(targetRel))
	if err != nil {
		return targetRel
	}
	return filepath.ToSlash(rel)
}

func sanitizeMDZFilename(name string) string {
	name = norm.NFC.String(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		if r <= 0x1F || r == 0x7F {
			continue
		}
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			b.WriteByte('-')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == ".." {
		return "asset"
	}
	return out
}

func encodeArchiveRelURL(rel string) string {
	segs := strings.Split(rel, "/")
	for i, seg := range segs {
		segs[i] = neturl.PathEscape(seg)
	}
	return strings.Join(segs, "/")
}

func hasSchemePrefix(dest string) bool {
	for i := 0; i < len(dest); i++ {
		c := dest[i]
		if c == ':' {
			return i > 0
		}
		if c == '/' || c == '?' || c == '#' {
			return false
		}
		if i == 0 {
			if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
				return false
			}
		} else {
			if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.') {
				return false
			}
		}
	}
	return false
}
