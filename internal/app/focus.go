package app

import (
	"image"
	"time"

	"github.com/Ciao1019/ArcMD/internal/bookdoc"
	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/dbview"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/pdfdoc"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// narrowWidth is the window width below which the sidebar folds away by
// itself, as Arc's does. Widening again waits until narrowUnfoldWidth so
// re-opening the sidebar never makes the document narrower than before.
const (
	narrowWidth       = 900
	narrowUnfoldWidth = narrowWidth + sidebarWidth + 10
)

// narrowTick notes whether the window is narrow; crossing the threshold
// forgets the sidebar shown while narrow.
func (a *App) narrowTick(w float32) {
	narrow := a.narrow
	switch {
	case !a.narrowReady:
		narrow = w < narrowWidth
		a.narrowReady = true
	case !a.narrow && w < narrowWidth:
		narrow = true
	case a.narrow && w >= narrowUnfoldWidth:
		narrow = false
	}
	if narrow != a.narrow {
		a.narrow, a.narrowShown = narrow, false
	}
}

// folded reports whether the sidebar is folded away: by ⌘\, or by a
// narrow window unless shown there.
func (a *App) folded() bool {
	return a.sidebarHidden || a.narrow && !a.narrowShown
}

func (a *App) toggleSidebar() {
	a.finishSpaceEdit()
	if a.narrow {
		if a.folded() {
			a.sidebarHidden, a.narrowShown = false, true
		} else {
			a.narrowShown = false
		}
	} else {
		a.sidebarHidden = !a.sidebarHidden
	}
	a.peek = false
	a.peekLeave = time.Time{}
}

// toggleFocus enters or leaves focus mode, as Zen's: the document alone,
// nothing of the window around it, the other paragraphs faded.
func (a *App) toggleFocus() {
	a.finishSpaceEdit()
	a.focus = !a.focus
	if a.doc != nil {
		if a.doc.isWhiteboard() {
			a.doc.board.RequestFocus = true
		} else if a.doc.isPDF() {
			a.doc.pdf.RequestFocus = true
		} else if a.doc.isBook() {
			a.doc.book.RequestFocus = true
		} else if a.doc.isDatabase() {
			a.doc.dbView.RequestFocus = true
		} else {
			a.doc.ed.RequestFocus = true
		}
	}
}

// toggleTypewriter keeps the line being written in the middle of the page,
// or lets it go.
func (a *App) toggleTypewriter() {
	a.prefs.Typewriter = !a.prefs.Typewriter
	a.savePrefs()
}

// setOutline opens or collapses the outline left of the page, in every
// document.
func (a *App) setOutline(open bool) {
	a.prefs.OutlineClosed = !open
	a.savePrefs()
}

// toggleOutline opens or collapses the outline of the document in front.
func (a *App) toggleOutline() {
	if a.doc == nil || a.doc.loadErr != nil {
		return
	}
	if a.doc.isPDF() {
		a.doc.pdf.ToggleOutline()
		return
	}
	if a.doc.isBook() {
		a.doc.book.ToggleOutline()
		return
	}
	if !a.doc.source && !a.doc.isWhiteboard() && !a.doc.isDatabase() {
		a.doc.ed.ToggleOutline()
	}
}

// outlineOpen reports whether the document in front shows its outline.
func (a *App) outlineOpen() bool {
	if a.doc == nil || a.doc.isWhiteboard() || a.doc.isDatabase() {
		return false
	}
	if a.doc.isPDF() {
		return a.doc.pdf.SidebarOpen
	}
	if a.doc.isBook() {
		return a.doc.book.SidebarOpen
	}
	return a.doc.ed.OutlineOpen()
}

// toggleDim fades the paragraphs but the caret's in focus mode, or not.
func (a *App) toggleDim() {
	a.prefs.NoDim = !a.prefs.NoDim
	a.savePrefs()
}

// focusView is focus mode: the card over the whole window, a strip at its
// top to drag the window by and to leave.
func (a *App) focusView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c.Key("focus")).Grow(1).MinWidth(0).Padding(0, 8, 8, 8).Children(func() {
		bar := c.TitleBar()
		top := ui.Row(c.Key("focus-bar")).Height(max(bar.Height, 36)).Padding(0, bar.Right+a.captionWidth(), 0, bar.Left).Gap(2).AlignItems(ui.Center).DragWindow()
		_, py, inside := c.Root().PointerPosition()
		show := inside && py < 60
		top.Children(func() {
			ui.Spacer(c)
			if a.doc != nil {
				words := ui.Text(c, a.doc.title()).FontSize(12).TextColor(t.TextMuted).SingleLine()
				words.Opacity(words.Animate("shown", b2f(show), 150*time.Millisecond))
			}
			ui.Spacer(c)
			if a.canAdjustPageWidth(a.doc) {
				pw := a.pageWidthButton(c, a.doc)
				pw.Opacity(pw.Animate("shown", b2f(show || pw.Focused()), 150*time.Millisecond))
			}
			exit := iconButton(c, "focus-exit", iconMinimize, i18n.T("退出专注模式"), a.withKey(i18n.T("退出专注模式"), "focus"), false)
			exit.Opacity(exit.Animate("shown", b2f(show || exit.Focused()), 150*time.Millisecond))
			if exit.Clicked() {
				a.toggleFocus()
			}
		})
		card := ui.Themed(c.Key("card"), documentTheme(a.paperDark)).Grow(1).MinHeight(0).Radius(10).
			Shadow(0, 1, 4, 0, cardShadow(t)).Border(1, t.Border.Alpha(0.35)).Clip()
		card.Children(func() {
			if a.doc == nil || a.doc.loadErr != nil || a.doc.source {
				ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
					ui.Text(c, i18n.T("专注模式下没有可编辑的文档。")).TextColor(c.Theme().TextMuted)
					if ui.Button(c.Key("focus-leave"), i18n.T("退出专注模式")).Clicked() {
						a.toggleFocus()
					}
				})
				return
			}
			w, _ := c.Size()
			ui.Column(c.Key("page")).Grow(1).MinHeight(0).Children(func() {
				a.conflictBar(c)
				if a.doc.isWhiteboard() {
					editor.BoardView(c.Key(a.doc.viewKey()), a.doc.board, a.boardOptionsFor(c, a.doc))
				} else if a.doc.isPDF() {
					editor.PDFView(c.Key(a.doc.viewKey()), a.doc.pdf, a.pdfOptionsFor(c, a.doc))
				} else if a.doc.isBook() {
					editor.BookView(c.Key(a.doc.viewKey()), a.doc.book, a.bookOptionsFor(c, a.doc))
				} else if a.doc.isDatabase() {
					a.databaseWorkspace(c, a.doc, w-18)
				} else {
					editor.View(c.Key(a.doc.viewKey()), a.doc.ed, a.editorOptions(c, w-18, true))
				}
			})
		})
	})
}

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// editorOptions are the open document's editor's, width wide; focus mode
// fades the other paragraphs.
func (a *App) editorOptions(c *ui.Context, width float32, focus bool) editor.Options {
	return a.editorOptionsFor(c, width, focus, a.doc)
}

func (a *App) editorOptionsFor(c *ui.Context, width float32, focus bool, d *document) editor.Options {
	run := func(fn func()) {
		switch {
		case a.activateDocument(d):
			fn()
		case a.dbPeek.open && a.dbPeek.doc == d:
			a.withDocument(d, fn)
		case a.inWindow(d):
			// A document in its own window acts as the document.
			a.withDocument(d, fn)
		}
	}
	docTitle := ""
	if d != nil {
		docTitle = d.title()
	}
	var htmlWVFn func(url, slot string, scrollToContainer bool) editor.HTMLWebView
	if a.hostWindowFor(d) != nil || a.makeHTMLWebView != nil {
		htmlWVFn = func(url, slot string, scrollToContainer bool) editor.HTMLWebView {
			return a.htmlWebViewForEmbed(d, url, slot, scrollToContainer)
		}
	}

	return editor.Options{
		FontSize:              a.prefs.ReadingSize(),
		LineHeight:            a.prefs.ReadingLine(),
		FontFamily:            a.prefs.BodyFamily(),
		DocTitle:              docTitle,
		Column:                a.prefs.Column(),
		FullWidth:             a.prefs.FullWidth(),
		NoAutoPair:            a.prefs.NoAutoPair,
		AutoSpace:             a.prefs.AutoSpace,
		CodeNoWrap:            a.prefs.CodeNoWrap,
		CodeMaxHeight:         a.prefs.CodeMaxHeight,
		ImageMaxHeight:        a.prefs.ImageMaxHeight,
		EmojiSuggest:          suggestEmoji,
		Resizing:              a.splitResizing || a.qa.resizing || a.dockSliding,
		Width:                 width,
		Theme:                 c.Theme(),
		Page:                  pageStyle(a.paperDark),
		Attach:                func() { run(a.pickImages) },
		DropFiles:             func(paths []string, at int) { run(func() { a.handleEditorFileDrop(paths, at) }) },
		PasteImage:            func(data []byte) { run(func() { a.pasteImage(data) }) },
		LoadImage:             func(url string) editor.ImageResource { return a.imageResourceFor(d, url) },
		ReplaceImage:          func(id, url string) { a.pickReplacementImage(d, id, url) },
		RevealImage:           func(url string) { a.revealImage(d, url) },
		CropImage:             func(id, url string, r image.Rectangle) { a.cropImage(d, id, url, r) },
		OpenLink:              func(url string) { a.navigateInPane(d, func() { a.followLink(url) }) },
		OpenWiki:              func(name string) { a.navigateInPane(d, func() { a.openWiki(name) }) },
		OpenTag:               func(tag string) { run(func() { a.openTag(tag) }) },
		PreviewWiki:           func(name string) editor.WikiPreview { return a.previewWikiFor(d, name) },
		WikiSuggest:           func(typed string) []editor.WikiItem { return a.wikiSuggestFor(d, typed) },
		Footer:                func(c *ui.Context) { a.pageFooterFor(c, d) },
		Typewriter:            a.prefs.Typewriter,
		DrawAnnotations:       a.prefs.DrawAnnotations,
		Dim:                   focus && !a.prefs.NoDim,
		PickCover:             func(done func(string)) { run(func() { a.pickCover(done) }) },
		Bindings:              a.editorBindings(),
		Outline:               !a.prefs.OutlineClosed,
		SetOutline:            a.setOutline,
		Bookmarks:             a.marksOf(d),
		LoadWhiteboard:        func(url string) (*whiteboard.Scene, uint64, error) { return a.loadWhiteboardFor(d, url) },
		CreateWhiteboard:      func(scene *whiteboard.Scene) (string, error) { return a.createWhiteboardAssetFor(d, scene) },
		SaveWhiteboard:        func(url string, scene *whiteboard.Scene) uint64 { return a.saveWhiteboardFor(d, url, scene) },
		OpenWhiteboard:        func(url string, split bool) { a.openWhiteboardFrom(d, url, split) },
		SaveBlockAsWhiteboard: func(blockID string, scene *whiteboard.Scene) { a.saveBlockAsWhiteboard(d, blockID, scene) },
		BoardWikiSuggest:      func(q string) []string { return a.boardWikiNames(d, q) },
		OpenWikiSplit:         func(name string, split bool) { a.openWikiFromBoard(d, name, split) },
		UserWhiteboardLib:     a.userWhiteboardLibrary,
		SaveWhiteboardLib:     a.saveWhiteboardLibrary,
		ImportWhiteboardLib:   a.importWhiteboardLibrary,
		LoadPDF:               func(url string) (*pdfdoc.Document, uint64, error) { return a.loadPDFFor(d, url) },
		OpenPDF:               func(url string, split bool) { a.openPDFFrom(d, url, split) },
		AttachPDF:             func() { run(a.pickPDFs) },
		AsyncPDF:              a.win != nil,
		LoadBook:              func(url string) (*bookdoc.Document, uint64, error) { return a.loadBookFor(d, url) },
		OpenBook:              func(url string, split bool) { a.openBookFrom(d, url, split) },
		OpenBookInBrowser:     func(url string) { a.openBookInBrowserFor(d, url) },
		LoadBookExternalImg: func(bookURL, imgSrc string) editor.ImageResource {
			return a.loadBookExternalImageFor(d, bookURL, imgSrc)
		},
		HTMLWebView: htmlWVFn,
		AttachHTML:  func() { run(a.pickHTMLFiles) },
		AttachEPUB:  func() { run(a.pickEPUBFiles) },
		LoadDatabase: func(target string) (*database.Store, dbview.Options, uint64, error) {
			return a.loadDatabaseFor(d, target)
		},
		OpenDatabase: func(target, viewID string, split bool) { a.openDatabaseFrom(d, target, viewID, split) },
		ListDatabases: func() []database.SpaceDBInfo {
			if d.space == nil || d.space.IsExternal() || d.space.Root == "" {
				return nil
			}
			dbs, _ := database.ListSpaceDatabases(d.space.Root)
			return dbs
		},
		CreateDatabase:   func(title string) (string, string, string, error) { return a.createEmbeddedDatabaseFor(d, title) },
		DatabaseSnapshot: func(target, viewID string) string { return a.databaseSnapshotFor(d, target, viewID) },
		ToggleBookmark: func(id string) {
			a.toggleBookmark(d, id)
		},
		LabelBookmark: func(id, label string) { a.labelBookmark(d.space.ID, d.rel, id, label) },
		BookmarkKey:   a.cachedBookmarkShortKey(),
		Reserved:      a.reservedKeys(),
	}
}
