package app

// Documents in windows of their own (a row's menu, the command bar):
// beside the main window and its split view, for a second display. The
// document moves there — out of the main window, which shows the one
// before it — keeping its caret, scroll and undo; opening it again from
// the main window brings its window forward rather than opening it twice,
// since one file has one editor. It saves as the main window's documents
// do, follows renames and moves, and closing the window saves it.

import (
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// windowHandle is what the app does with a document's window; tests stand
// in for the system's.
type windowHandle interface {
	Focus()
	Close()
	SetTitle(string)
}

// docWindow is a document shown in a window of its own.
type docWindow struct {
	win windowHandle
	doc *document
}

// windowOf is the window showing the document at rel of the space with
// the ID, or nil.
func (a *App) windowOf(id, rel string) *docWindow {
	for _, w := range a.windows {
		if w.doc != nil && w.doc.space.ID == id && w.doc.rel == rel {
			return w
		}
	}
	return nil
}

// openInWindow shows the document at rel of sp in a window of its own.
func (a *App) openInWindow(sp *space.Space, rel string) {
	if sp == nil || !vault.IsDocument(rel) {
		return
	}
	if w := a.windowOf(sp.ID, rel); w != nil {
		w.win.Focus()
		return
	}
	if !a.flush() {
		return
	}
	d := a.residentDocument(sp.ID, rel)
	if d != nil {
		// It leaves the main window, which shows what it showed before.
		if a.split != nil {
			for i, p := range a.split.panes {
				if p.doc == d {
					a.closePane(i)
					break
				}
			}
		}
		if a.doc == d {
			a.showAfter(sp, rel)
		}
		a.replaceReferences(d, nil)
		delete(a.openDocs, place{sp.ID, rel})
	} else {
		text, err := vault.Read(sp.Root, rel)
		if err != nil {
			a.notify("无法打开 %s。%v", vault.Title(rel), err)
			return
		}
		d = openDocument(sp, rel, text, false)
	}
	w := &docWindow{doc: d}
	w.win = a.makeWindow(docName(d.title()), func(c *ui.Context) { a.windowView(c, w) }, func() bool { return a.closeWindow(w) })
	a.windows = append(a.windows, w)
}

// makeWindow opens a system window titled title that shows view; closing
// it asks close, which may keep it open.
func (a *App) makeWindow(title string, view func(*ui.Context), close func() bool) windowHandle {
	if a.newWindow != nil {
		return a.newWindow(title, view, close)
	}
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:                title,
		Width:                900,
		Height:               760,
		MinWidth:             480,
		MinHeight:            360,
		TitleBarStyle:        mygo.TitleBarHidden,
		TrafficLightPosition: &mygo.Point{X: 18, Y: 18},
		Content:              ui.View(view),
	})
	win.OnClose(func(e *mygo.CloseEvent) {
		if !close() {
			e.PreventDefault()
		}
	})
	return win
}

// closeWindow saves the window's document and lets the window go, unless
// it cannot be saved.
func (a *App) closeWindow(w *docWindow) bool {
	if d := w.doc; d != nil {
		ok := true
		a.withDocument(d, func() { ok = a.flushCurrent() })
		if !ok {
			a.showSaveFailure(d)
			return false
		}
	}
	for i, x := range a.windows {
		if x == w {
			a.windows = append(a.windows[:i], a.windows[i+1:]...)
			break
		}
	}
	return true
}

// backToMain closes the window and shows its document in the main window.
func (a *App) backToMain(w *docWindow) {
	d := w.doc
	if d == nil || !a.closeWindow(w) {
		return
	}
	w.win.Close()
	sp := d.space
	if sp != a.state.ActiveSpace() && !a.state.IsFavorite(sp.ID, d.rel) {
		a.switchSpace(sp.ID)
	}
	a.openIn(sp, d.rel)
}

// windowView is a document's window: a bar to drag it by, with its name,
// over the page.
func (a *App) windowView(c *ui.Context, w *docWindow) {
	paper := documentTheme(a.paperDark)
	c.SetTheme(paper)
	t := c.Theme()
	d := w.doc
	c.Root().Background(paper.Background)
	if d == nil {
		ui.Text(c, i18n.T("文档已关闭")).TextColor(t.TextMuted).Padding(80, 24)
		return
	}
	bar := c.TitleBar()
	ui.Column(c.Key("window-doc")).Fill().Children(func() {
		top := ui.Row(c.Key("window-bar")).Height(max(44, bar.Height)).PaddingX(12).Gap(8).AlignItems(ui.Center).DragWindow()
		top.Children(func() {
			ui.Box(c).Width(bar.Left)
			docIcon(c, a.iconTitle(d.space.ID, d.rel), 16)
			ui.Text(c, docName(d.title())).FontSize(13).FontWeight(600).SingleLine().Shrink(1).MinWidth(0)
			ui.Text(c, d.space.Name).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
			ui.Box(c).Grow(1)
			if d.loadErr == nil {
				d.ed.Corner(c)
			}
			if iconButton(c, "window-main", iconWindowBack, i18n.T("在主窗口中打开"), i18n.T("在主窗口中打开"), false).Clicked() {
				a.backToMain(w)
			}
			ui.Box(c).Width(bar.Right)
		})
		if d.loadErr != nil {
			ui.Text(c, i18n.T("文档无法打开。")+d.loadErr.Error()).TextColor(t.Danger).Padding(24)
			return
		}
		width, _ := c.Size()
		editor.View(c.Key(d.viewKey()), d.ed, a.editorOptionsFor(c, width, false, d))
	})
	w.win.SetTitle(docName(d.title()))
}

// inWindow reports whether d is shown in a window of its own.
func (a *App) inWindow(d *document) bool {
	for _, w := range a.windows {
		if w.doc == d && d != nil {
			return true
		}
	}
	return false
}
