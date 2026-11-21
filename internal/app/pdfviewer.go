package app

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/pdfdoc"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/Ciao1019/ArcMD/internal/wiki"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const maxCachedPDFDocs = 12

type cachedPDFDoc struct {
	modNano int64
	size    int64
	rev     uint64
	doc     *pdfdoc.Document
	err     error
}

func (d *document) isPDF() bool {
	return d != nil && d.pdf != nil
}

func openPDFDocument(sp *space.Space, rel, diskStamp string) *document {
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
		d.pdf = editor.NewPDFViewer(nil, err)
		return d
	}
	if info, stErr := os.Stat(full); stErr == nil {
		d.pdfModNano = info.ModTime().UnixNano()
		d.pdfSize = info.Size()
		if d.diskText == "" {
			d.diskText = fmt.Sprintf("pdf:%d:%d", d.pdfModNano, d.pdfSize)
		}
	}
	pdfDoc, openErr := pdfdoc.Open(full)
	if openErr != nil {
		// Encrypted or malformed PDFs render a friendly, actionable card inside PDFView
		// rather than a raw red text error.
		d.pdf = editor.NewPDFViewer(nil, openErr)
		return d
	}
	d.pdf = editor.NewPDFViewer(pdfDoc, nil)
	return d
}

func (a *App) putCachedPDF(key string, entry cachedPDFDoc) {
	if a.pdfCache == nil {
		a.pdfCache = make(map[string]cachedPDFDoc, maxCachedPDFDocs)
	}
	if prev, ok := a.pdfCache[key]; ok && prev.doc != nil && prev.doc != entry.doc {
		prev.doc.Close()
	}
	a.pdfCache[key] = entry
	if idx := slices.Index(a.pdfOrder, key); idx >= 0 {
		a.pdfOrder = slices.Delete(a.pdfOrder, idx, idx+1)
	}
	a.pdfOrder = append(a.pdfOrder, key)
	for len(a.pdfCache) > maxCachedPDFDocs && len(a.pdfOrder) > 0 {
		oldest := a.pdfOrder[0]
		a.pdfOrder = a.pdfOrder[1:]
		if old, ok := a.pdfCache[oldest]; ok {
			if old.doc != nil {
				old.doc.Close()
			}
			delete(a.pdfCache, oldest)
		}
	}
}

func (a *App) pdfOptionsFor(c *ui.Context, d *document) editor.PDFOptions {
	var revealFn func()
	if d != nil && d.space != nil {
		sp, rel := d.space, d.rel
		revealFn = func() { a.revealIn(sp, rel) }
	}
	return editor.PDFOptions{
		Theme:      c.Theme(),
		Page:       pageStyle(a.paperDark),
		Title:      d.title(),
		Async:      a.win != nil,
		RevealFile: revealFn,
	}
}

func (a *App) pdfCorner(c *ui.Context, d *document) {
	if d == nil || !d.isPDF() || d.pdf == nil {
		return
	}
	t := c.Theme()
	ui.Row(c.Key("pdf-corner")).Gap(2).AlignItems(ui.Center).Children(func() {
		if cnt := d.pdf.PageCount(); cnt > 0 {
			ui.Box(c.Key("pdf-pages")).Height(28).PaddingX(8).Radius(7).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf(i18n.T("共 %d 页"), cnt)).FontSize(12.5).TextColor(t.TextMuted).NoWrap()
			})
		}
		side := iconButton(c, "pdf-outline", iconSidebar, i18n.T("缩略图与目录"), a.withKey(i18n.T("缩略图与目录"), "outline"), d.pdf.PageCount() == 0)
		if d.pdf.SidebarOpen {
			side.Background(glass(t))
		}
		if side.Clicked() {
			d.pdf.ToggleOutline()
		}
		find := iconButton(c, "pdf-find", iconSearch, i18n.T("在 PDF 中查找"), i18n.T("在 PDF 中查找")+"  ⌘F", d.pdf.PageCount() == 0)
		if d.pdf.FindOpen {
			find.Background(glass(t))
		}
		if find.Clicked() {
			if d.pdf.FindOpen {
				d.pdf.FindOpen = false
			} else {
				d.pdf.OpenFind()
			}
		}
	})
}

func (a *App) loadPDFFor(d *document, rawURL string) (*pdfdoc.Document, uint64, error) {
	cleanURL, _ := editor.ParsePDFURLPage(rawURL)
	if d == nil || d.space == nil || cleanURL == "" {
		return nil, 0, errors.New("invalid PDF reference")
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
	if c, exists := a.pdfCache[key]; exists && c.modNano == modNano && c.size == size {
		if idx := slices.Index(a.pdfOrder, key); idx >= 0 {
			a.pdfOrder = slices.Delete(a.pdfOrder, idx, idx+1)
			a.pdfOrder = append(a.pdfOrder, key)
		}
		return c.doc, c.rev, c.err
	}
	doc, openErr := pdfdoc.Open(full)
	a.putCachedPDF(key, cachedPDFDoc{
		modNano: modNano,
		size:    size,
		rev:     rev,
		doc:     doc,
		err:     openErr,
	})
	return doc, rev, openErr
}

func (a *App) resolvePDFRef(d *document, rawURL string) (*space.Space, string, string, int, bool) {
	cleanURL, page1Based := editor.ParsePDFURLPage(rawURL)
	if d == nil || d.space == nil || cleanURL == "" {
		return nil, "", "", page1Based, false
	}
	full, err := resolveSpaceImage(d.space, d.rel, cleanURL)
	if err == nil {
		if info, stErr := os.Stat(full); stErr == nil && !info.IsDir() {
			if !d.space.IsExternal() {
				if rel, relErr := filepath.Rel(d.space.Root, full); relErr == nil && filepath.IsLocal(rel) {
					return d.space, filepath.ToSlash(rel), full, page1Based, true
				}
			}
			return nil, "", full, page1Based, true
		}
	}
	if !d.space.IsExternal() {
		clean := unescapeImagePath(cleanURL)
		var foundRel string
		a.eachDoc(func(s *space.Space, n *vault.Node) bool {
			if s == d.space && vault.IsPDF(n.Path) && (strings.EqualFold(path.Base(n.Path), path.Base(clean)) || wiki.Same(n.Title(), vault.Title(clean))) {
				foundRel = n.Path
				return false
			}
			return true
		})
		if foundRel != "" {
			f, _ := resolveFile(d.space, foundRel)
			return d.space, foundRel, f, page1Based, true
		}
	}
	return nil, "", "", page1Based, false
}

func (a *App) openPDFFrom(d *document, rawURL string, split bool) {
	sp, rel, full, page1Based, ok := a.resolvePDFRef(d, rawURL)
	if !ok {
		clean, _ := editor.ParsePDFURLPage(rawURL)
		a.notify("找不到 PDF「%s」。", clean)
		return
	}
	jump := func(target *document, instant bool) {
		if target != nil && target.isPDF() && page1Based >= 1 {
			target.pdf.GoToPage(page1Based-1, instant)
		}
	}
	if sp == nil && full != "" {
		extSp, err := a.externalSpace()
		if err != nil {
			a.notify("无法打开 PDF。%v", err)
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

// pickPDFs asks for .pdf files, copies them into the space's attachment folder
// (or beside the document), and inserts them as inline PDF blocks at the caret.
func (a *App) pickPDFs() {
	d := a.doc
	if d == nil || d.isWhiteboard() || d.isPDF() || d.ed.ReadOnly {
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
		Title:    i18n.T("插入 PDF 文档"),
		Multiple: true,
		Filters:  []mygo.FileFilter{{Name: "PDF", Extensions: []string{"pdf"}}},
	}
	go func() {
		paths, err := mygo.Dialog.Open(opts)
		if err == nil && len(paths) > 0 {
			win.Update(func() { done(paths) })
		}
	}()
}

// choosePDFFile opens a system file dialog to browse any local PDF directly in ArcMD.
func (a *App) choosePDFFile() {
	if a.pickFiles != nil {
		a.pickFiles(func(paths []string) {
			for _, f := range paths {
				a.openSystemFile(f)
			}
		})
		return
	}
	if a.win == nil {
		return
	}
	win := a.win
	go func() {
		files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:   win,
			Title:    i18n.T("打开 PDF 文件…"),
			Multiple: true,
			Filters:  []mygo.FileFilter{{Name: "PDF", Extensions: []string{"pdf"}}},
		})
		if err != nil {
			return
		}
		win.Update(func() {
			for _, file := range files {
				a.openSystemFile(file)
			}
		})
	}()
}
