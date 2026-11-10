package app

import (
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/egoist/mygo/ui"
)

// narrowWidth is the window width below which the sidebar folds away by
// itself, as Arc's does, and comes back when the window widens.
const narrowWidth = 900

// narrowTick notes whether the window is narrow; crossing the width
// forgets the sidebar shown while narrow.
func (a *App) narrowTick(w float32) {
	if narrow := w < narrowWidth; narrow != a.narrow {
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
		a.doc.ed.RequestFocus = true
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
	if a.doc != nil && a.doc.loadErr == nil && !a.doc.source {
		a.doc.ed.ToggleOutline()
	}
}

// outlineOpen reports whether the document in front shows its outline.
func (a *App) outlineOpen() bool {
	return a.doc != nil && a.doc.ed.OutlineOpen()
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
		top := ui.Row(c.Key("focus-bar")).Height(max(bar.Height, 36)).Padding(0, bar.Right, 0, bar.Left).AlignItems(ui.Center).DragWindow()
		_, py, inside := c.Root().PointerPosition()
		show := inside && py < 60
		top.Children(func() {
			ui.Spacer(c)
			if a.doc != nil {
				words := ui.Text(c, a.doc.title()).FontSize(12).TextColor(t.TextMuted).SingleLine()
				words.Opacity(words.Animate("shown", b2f(show), 150*time.Millisecond))
			}
			ui.Spacer(c)
			exit := iconButton(c, "focus-exit", iconMinimize, "退出专注模式", a.withKey("退出专注模式", "focus"), false)
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
					ui.Text(c, "专注模式下没有可编辑的文档。").TextColor(c.Theme().TextMuted)
					if ui.Button(c.Key("focus-leave"), "退出专注模式").Clicked() {
						a.toggleFocus()
					}
				})
				return
			}
			w, _ := c.Size()
			ui.Column(c.Key("page")).Grow(1).MinHeight(0).Children(func() {
				a.conflictBar(c)
				editor.View(c.Key(a.doc.viewKey()), a.doc.ed, a.editorOptions(c, w-18, true))
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
		if a.activateDocument(d) {
			fn()
		}
	}
	return editor.Options{
		Resizing:     a.splitResizing,
		Width:        width,
		Theme:        c.Theme(),
		Page:         pageStyle(a.paperDark),
		Attach:       func() { run(a.pickImages) },
		DropFiles:    func(paths []string, at int) { run(func() { a.insertImages(paths, at) }) },
		PasteImage:   func(data []byte) { run(func() { a.pasteImage(data) }) },
		LoadImage:    d.imageResource,
		ReplaceImage: func(id, url string) { a.pickReplacementImage(d, id, url) },
		RevealImage:  func(url string) { a.revealImage(d, url) },
		OpenLink:     func(url string) { a.navigateInPane(d, func() { a.followLink(url) }) },
		OpenWiki:     func(name string) { a.navigateInPane(d, func() { a.openWiki(name) }) },
		OpenTag:      func(tag string) { run(func() { a.openTag(tag) }) },
		WikiSuggest:  func(typed string) []editor.WikiItem { return a.wikiSuggestFor(d, typed) },
		Footer:       func(c *ui.Context) { a.pageFooterFor(c, d) },
		Typewriter:   a.prefs.Typewriter,
		Dim:          focus && !a.prefs.NoDim,
		PickCover:    func(done func(string)) { run(func() { a.pickCover(done) }) },
		Bindings:     a.editorBindings(),
		Outline:      !a.prefs.OutlineClosed,
		SetOutline:   a.setOutline,
	}
}
