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

	"github.com/Ciao1019/ArcMD/internal/bookdoc"
	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/Ciao1019/ArcMD/internal/wiki"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const maxCachedBookDocs = 12

type cachedBookDoc struct {
	modNano int64
	size    int64
	rev     uint64
	doc     *bookdoc.Document
	err     error
}

func (d *document) isBook() bool {
	return d != nil && d.book != nil
}

func (d *document) syncMovedPath() {
	if !d.isBook() || d.book.Doc == nil || d.space == nil || d.rel == "" {
		return
	}
	if full, err := resolveFile(d.space, d.rel); err == nil {
		d.book.Doc.SetPath(full)
	}
}

func openBookDocument(sp *space.Space, rel, diskStamp string) *document {
	d := &document{
		space:      sp,
		rel:        rel,
		diskText:   diskStamp,
		ed:         editor.FromOwnedDocument(model.New()),
		historyOff: true,
	}
	d.initLazyBaseline()
	full, err := resolveFile(sp, rel)
	if err != nil {
		d.loadErr = err
		d.book = editor.NewBookViewer(nil, err)
		return d
	}
	if info, stErr := os.Stat(full); stErr == nil {
		d.bookModNano = info.ModTime().UnixNano()
		d.bookSize = info.Size()
		if d.diskText == "" {
			d.diskText = fmt.Sprintf("book:%d:%d", d.bookModNano, d.bookSize)
		}
	}
	bookDoc, openErr := bookdoc.Open(full)
	if openErr != nil {
		d.book = editor.NewBookViewer(nil, openErr)
		return d
	}
	d.book = editor.NewBookViewer(bookDoc, nil)
	return d
}

func (a *App) putCachedBook(key string, entry cachedBookDoc) {
	if a.bookCache == nil {
		a.bookCache = make(map[string]cachedBookDoc, maxCachedBookDocs)
	}
	if prev, ok := a.bookCache[key]; ok && prev.doc != nil && prev.doc != entry.doc {
		prev.doc.Close()
	}
	a.bookCache[key] = entry
	if idx := slices.Index(a.bookOrder, key); idx >= 0 {
		a.bookOrder = slices.Delete(a.bookOrder, idx, idx+1)
	}
	a.bookOrder = append(a.bookOrder, key)
	for len(a.bookCache) > maxCachedBookDocs && len(a.bookOrder) > 0 {
		oldest := a.bookOrder[0]
		a.bookOrder = a.bookOrder[1:]
		if old, ok := a.bookCache[oldest]; ok {
			if old.doc != nil {
				old.doc.Close()
			}
			delete(a.bookCache, oldest)
		}
	}
}

func (a *App) bookOptionsFor(c *ui.Context, d *document) editor.BookOptions {
	var revealFn, browserFn, saveMDFn func()
	var loadExtImg func(string) editor.ImageResource
	var webViewFn func() editor.HTMLWebView
	if d != nil && d.space != nil {
		sp, rel := d.space, d.rel
		revealFn = func() { a.revealIn(sp, rel) }
		if d.book != nil && d.book.IsHTML() {
			browserFn = func() { a.openBookInBrowserFor(d, "") }
			webViewFn = func() editor.HTMLWebView { return a.htmlWebViewForDoc(d) }
		}
		if d.book != nil && d.book.LoadErr == nil && d.book.ChapterCount() > 0 {
			saveMDFn = func() { a.saveBookAsMarkdown(d) }
		}
		loadExtImg = func(src string) editor.ImageResource {
			if isRemoteImageURL(src) {
				return a.remoteImageResource(d, src)
			}
			return d.imageResourceWith(src, a.asyncPost())
		}
	}
	return editor.BookOptions{
		Theme:          c.Theme(),
		Page:           pageStyle(a.paperDark),
		Title:          d.title(),
		Async:          a.win != nil,
		FontSize:       a.prefs.ReadingSize(),
		LineHeight:     a.prefs.ReadingLine(),
		FontFamily:     a.prefs.BodyFamily(),
		Column:         a.prefs.Column(),
		FullWidth:      a.prefs.FullWidth(),
		Zoom:           a.pageZoom(),
		OnZoom:         a.setPageZoom,
		RevealFile:     revealFn,
		OpenInBrowser:  browserFn,
		SaveAsMarkdown: saveMDFn,
		OpenExternalURL: func(url string) {
			if ui.SafeToOpen(url) {
				mygo.Shell.OpenExternal(url)
			}
		},
		LoadExternalImage: loadExtImg,
		WebView:           webViewFn,
	}
}

func (a *App) bookCorner(c *ui.Context, d *document) {
	if d == nil || !d.isBook() || d.book == nil {
		return
	}
	t := c.Theme()
	ui.Row(c.Key("book-corner")).Gap(2).AlignItems(ui.Center).Children(func() {
		if a.showPageZoomButton(d) {
			a.pageZoomButton(c, d)
		}
		if a.canAdjustPageWidth(d) {
			a.pageWidthButton(c, d)
		}
		if d.book.IsEPUB() && d.book.ChapterCount() > 0 {
			ui.Row(c.Key("book-chapters")).Height(28).PaddingX(8).Radius(7).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf(i18n.T("第 %d / %d 章"), d.book.CurrentChapter+1, d.book.ChapterCount())).
					FontSize(12.5).TextColor(t.TextMuted).NoWrap()
			})
		}
		side := iconButton(c, "book-outline", iconSidebar, i18n.T("目录"), a.withKey(i18n.T("目录"), "outline"), d.book.ChapterCount() == 0)
		if d.book.SidebarOpen {
			side.Background(glass(t))
		}
		if side.Clicked() {
			d.book.ToggleOutline()
		}
		find := iconButton(c, "book-find", iconSearch, i18n.T("在文档中查找"), i18n.T("在文档中查找")+"  ⌘F", d.book.ChapterCount() == 0)
		if d.book.FindOpen {
			find.Background(glass(t))
		}
		if find.Clicked() {
			if d.book.FindOpen {
				d.book.FindOpen = false
			} else {
				d.book.OpenFind()
			}
		}
		if d.book.IsHTML() {
			srcTip := i18n.T("查看 HTML 源码")
			if d.book.ShowSource {
				srcTip = i18n.T("返回阅读视图")
			}
			srcBtn := iconButton(c, "book-source", iconCode, srcTip, srcTip, false)
			if d.book.ShowSource {
				srcBtn.Background(glass(t))
			}
			if srcBtn.Clicked() {
				d.book.ToggleSource()
			}
			brBtn := iconButton(c, "book-browser", iconExternalLink, i18n.T("在默认浏览器中打开"), i18n.T("在默认浏览器中打开"), false)
			if brBtn.Clicked() {
				a.openBookInBrowserFor(d, "")
			}
		}
	})
}

func (a *App) saveBookAsMarkdown(d *document) {
	if d == nil || !d.isBook() || d.book == nil || d.book.LoadErr != nil {
		return
	}
	md := d.book.ExportCurrentChapterMarkdown()
	if strings.TrimSpace(md) == "" {
		a.notify("当前章节没有可转换的文本内容。")
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
		a.notify("请先选择一个空间以保存 Markdown 笔记。")
		return
	}
	title := docName(d.title())
	if d.book.IsEPUB() && d.book.ChapterCount() > 1 {
		if chTitle := strings.TrimSpace(d.book.CurrentChapterTitle()); chTitle != "" {
			title = title + " - " + chTitle
		}
	}
	rel, err := vault.CreateFile(targetSp.Root, dir, title, ".md", md)
	if err != nil {
		a.notify("转换 Markdown 失败。%v", err)
		return
	}
	delete(a.pageTrees, targetSp.ID)
	a.refreshTree()
	a.refreshSearch()
	a.openAnywhere(targetSp.ID, rel)
	a.notify("已另存为 Markdown：%s", path.Base(rel))
}

func (a *App) loadBookFor(d *document, rawURL string) (*bookdoc.Document, uint64, error) {
	cleanURL, _, _ := editor.ParseBookURLRef(rawURL)
	if d == nil || d.space == nil || cleanURL == "" {
		return nil, 0, errors.New("invalid document reference")
	}
	full, err := resolveSpaceImage(d.space, d.rel, cleanURL)
	if err != nil {
		return nil, 0, err
	}
	info, err := os.Stat(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, errors.New(i18n.T("这个路径下没有文件。"))
		}
		return nil, 0, err
	}
	modNano, size := info.ModTime().UnixNano(), info.Size()
	rev := uint64(modNano) ^ uint64(size<<12)
	if rev == 0 {
		rev = 1
	}
	key := full
	if c, exists := a.bookCache[key]; exists && c.modNano == modNano && c.size == size {
		if idx := slices.Index(a.bookOrder, key); idx >= 0 {
			a.bookOrder = slices.Delete(a.bookOrder, idx, idx+1)
			a.bookOrder = append(a.bookOrder, key)
		}
		return c.doc, c.rev, c.err
	}
	doc, openErr := bookdoc.Open(full)
	a.putCachedBook(key, cachedBookDoc{
		modNano: modNano,
		size:    size,
		rev:     rev,
		doc:     doc,
		err:     openErr,
	})
	return doc, rev, openErr
}

func (a *App) loadBookExternalImageFor(d *document, bookURL, imgSrc string) editor.ImageResource {
	if isRemoteImageURL(imgSrc) {
		return a.remoteImageResource(d, imgSrc)
	}
	if d == nil || d.space == nil {
		return editor.ImageResource{Problem: editor.ImageInvalid}
	}
	cleanBook, _, _ := editor.ParseBookURLRef(bookURL)
	bookFull, err := resolveSpaceImage(d.space, d.rel, cleanBook)
	if err != nil {
		return editor.ImageResource{Problem: editor.ImageInvalid}
	}
	decoded, err := neturl.PathUnescape(strings.TrimSpace(imgSrc))
	if err != nil || decoded == "" || strings.ContainsAny(decoded, ":\\\x00") || path.IsAbs(decoded) || filepath.IsAbs(decoded) {
		return editor.ImageResource{Problem: editor.ImageInvalid}
	}
	imgFull := filepath.Join(filepath.Dir(bookFull), filepath.FromSlash(decoded))
	return d.imageResourceFrom(imgFull, cleanBook+"|"+imgSrc, a.asyncPost())
}

func (a *App) resolveBookRef(d *document, rawURL string) (*space.Space, string, string, int, string, bool) {
	cleanURL, ch1Based, anchor := editor.ParseBookURLRef(rawURL)
	if d == nil || d.space == nil || cleanURL == "" {
		return nil, "", "", ch1Based, anchor, false
	}
	full, err := resolveSpaceImage(d.space, d.rel, cleanURL)
	if err == nil {
		if info, stErr := os.Stat(full); stErr == nil && !info.IsDir() {
			if !d.space.IsExternal() {
				if rel, relErr := filepath.Rel(d.space.Root, full); relErr == nil && filepath.IsLocal(rel) {
					return d.space, filepath.ToSlash(rel), full, ch1Based, anchor, true
				}
			}
			return nil, "", full, ch1Based, anchor, true
		}
	}
	if !d.space.IsExternal() {
		clean := unescapeImagePath(cleanURL)
		var foundRel string
		a.eachDoc(func(s *space.Space, n *vault.Node) bool {
			if s == d.space && vault.IsBook(n.Path) && (strings.EqualFold(path.Base(n.Path), path.Base(clean)) || wiki.Same(n.Title(), vault.Title(clean))) {
				foundRel = n.Path
				return false
			}
			return true
		})
		if foundRel != "" {
			f, _ := resolveFile(d.space, foundRel)
			return d.space, foundRel, f, ch1Based, anchor, true
		}
	}
	return nil, "", "", ch1Based, anchor, false
}

func (a *App) openBookFrom(d *document, rawURL string, split bool) {
	sp, rel, full, ch1Based, anchor, ok := a.resolveBookRef(d, rawURL)
	if !ok {
		clean, _, _ := editor.ParseBookURLRef(rawURL)
		a.notify("找不到文档「%s」。", clean)
		return
	}
	jump := func(target *document, instant bool) {
		if target == nil || !target.isBook() {
			return
		}
		ch := max(0, ch1Based-1)
		if anchor != "" {
			target.book.GoToChapterAnchor(ch, anchor, instant)
		} else if ch1Based >= 1 {
			target.book.GoToChapter(ch, instant)
		}
	}
	if sp == nil && full != "" {
		extSp, err := a.externalSpace()
		if err != nil {
			a.notify("无法打开文档。%v", err)
			return
		}
		name := extSp.AddExternal(full)
		a.saveState()
		sp, rel = extSp, name
	}
	if split {
		alreadyOpen := a.residentDocument(sp.ID, rel) != nil
		a.openSplit(sp, rel, false, false)
		jump(a.residentDocument(sp.ID, rel), !alreadyOpen)
		return
	}
	alreadyOpen := a.residentDocument(sp.ID, rel) != nil
	a.navigateInPane(d, func() {
		a.openIn(sp, rel)
		jump(a.doc, !alreadyOpen)
	})
}

func (a *App) openBookInBrowserFor(d *document, rawURL string) {
	if d == nil {
		return
	}
	var full string
	if rawURL == "" {
		var err error
		full, err = resolveFile(d.space, d.rel)
		if err != nil {
			a.notify("无法在浏览器中打开。%v", err)
			return
		}
	} else {
		_, _, resolvedFull, _, _, ok := a.resolveBookRef(d, rawURL)
		if !ok || resolvedFull == "" {
			clean, _, _ := editor.ParseBookURLRef(rawURL)
			a.notify("找不到文档「%s」。", clean)
			return
		}
		full = resolvedFull
	}
	if err := mygo.Shell.OpenPath(full); err != nil {
		a.notify("无法在默认浏览器中打开。%v", err)
	}
}

func (a *App) pickHTMLFiles() {
	a.pickBookAssets(i18n.T("插入 HTML 文档"), "HTML", []string{"html", "htm"})
}

func (a *App) pickEPUBFiles() {
	a.pickBookAssets(i18n.T("插入 EPUB 电子书"), "EPUB", []string{"epub"})
}

func (a *App) pickBookAssets(title, filterName string, exts []string) {
	d := a.doc
	if d == nil || d.isWhiteboard() || d.isPDF() || d.isBook() || d.ed.ReadOnly {
		return
	}
	done := func(paths []string) {
		if a.residentDocument(d.space.ID, d.rel) == d {
			a.withDocument(d, func() { a.insertImages(paths, -1) })
		}
	}
	if a.pickFiles != nil {
		a.pickFiles(done)
		return
	}
	win := a.win
	if win == nil {
		return
	}
	opts := mygo.OpenDialogOptions{
		Parent:   win,
		Title:    title,
		Multiple: true,
		Filters:  []mygo.FileFilter{{Name: filterName, Extensions: exts}},
	}
	go func() {
		paths, err := mygo.Dialog.Open(opts)
		if err == nil && len(paths) > 0 {
			win.Update(func() { done(paths) })
		}
	}()
}
