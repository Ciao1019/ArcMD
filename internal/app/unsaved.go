package app

import (
	"fmt"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// unsavedPrompt asks what to do with modified documents before an action
// that would close or replace them while background autosave is turned off.
type unsavedPrompt struct {
	open    bool
	docs    []*document
	proceed func()
}

func (a *App) autosaveEnabled() bool {
	return a.prefs == nil || !a.prefs.NoAutosave
}

func (a *App) toggleAutosave() {
	a.prefs.NoAutosave = !a.prefs.NoAutosave
	a.savePrefs()
}

// saveNow explicitly saves resident documents (⌘S or the command bar).
func (a *App) saveNow() {
	a.flush()
}

// discardEdits drops in-memory edits on d, restoring what is on disk without
// touching the file.
func (a *App) discardEdits(d *document) {
	if d == nil {
		return
	}
	d.waitAsyncSave()
	clean := openDocument(d.space, d.rel, d.diskText, d.source)
	if d.isWhiteboard() && clean.isWhiteboard() {
		clean.board.PreserveViewport(d.board)
		clean.boardReadOnly = d.boardReadOnly
	} else {
		clean.ed.Scroll = d.ed.Scroll
	}
	a.replaceReferences(d, clean)
	d.close()
}

func (a *App) discardCurrentEdits() {
	if a.doc == nil || !a.doc.dirty() {
		return
	}
	a.discardEdits(a.doc)
}

func (a *App) flushDocs(docs []*document) bool {
	for _, d := range docs {
		if d == nil {
			continue
		}
		ok := false
		a.withDocument(d, func() { ok = a.flushCurrent() })
		if !ok {
			a.showSaveFailure(d)
			return false
		}
	}
	return true
}

// confirmLeaveDocs flushes immediately when autosave is enabled, or opens the
// Save / Don't Save / Cancel dialog when autosave is disabled and any of docs
// has unsaved edits.
func (a *App) confirmLeaveDocs(docs []*document, proceed func()) bool {
	if a.autosaveEnabled() {
		return a.flush()
	}
	var dirty []*document
	seen := make(map[*document]bool, len(docs))
	for _, d := range docs {
		if d != nil && !seen[d] && d.loadErr == nil && d.dirty() {
			seen[d] = true
			dirty = append(dirty, d)
		}
	}
	if len(dirty) == 0 {
		return true
	}
	a.unsaved = unsavedPrompt{
		open:    true,
		docs:    dirty,
		proceed: proceed,
	}
	return false
}

// leavingCurrentDoc returns the currently shown document if navigating away in
// place would retire it from memory (unpinned and not held by a split group).
func (a *App) leavingCurrentDoc() []*document {
	d := a.doc
	if d == nil || d.loadErr != nil || !d.dirty() {
		return nil
	}
	if inPins(d.space, d.rel) {
		return nil
	}
	if a.split != nil && !a.paneNavigation {
		return nil
	}
	return []*document{d}
}

func (a *App) unsavedPromptView(c *ui.Context) {
	if !a.unsaved.open || len(a.unsaved.docs) == 0 {
		return
	}
	docs := a.unsaved.docs
	proceed := a.unsaved.proceed
	title := fmt.Sprintf(i18n.T("保存对「%s」的修改吗？"), docs[0].title())
	if len(docs) > 1 {
		title = fmt.Sprintf(i18n.T("保存对「%s」等 %d 篇文档的修改吗？"), docs[0].title(), len(docs))
	}
	a.spaceModal(c, "unsaved-prompt", &a.unsaved.open, func(t *ui.Theme) {
		ui.Column(c.Key("unsaved-body")).Width(390).Gap(14).Children(func() {
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, title).FontSize(16).FontWeight(600)
				ui.Text(c, i18n.T("已关闭自动保存，如果不保存，未保存的修改将会丢失。")).FontSize(13).LineHeight(1.5).TextColor(t.TextMuted)
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if ui.Button(c.Key("discard"), i18n.T("不保存")).Clicked() {
					a.unsaved = unsavedPrompt{}
					for _, d := range docs {
						a.discardEdits(d)
					}
					if proceed != nil {
						proceed()
					}
				}
				ui.Spacer(c)
				if ui.Button(c.Key("cancel"), i18n.T("取消")).Clicked() {
					a.unsaved = unsavedPrompt{}
				}
				if ui.PrimaryButton(c.Key("save"), i18n.T("保存")).Clicked() {
					a.unsaved = unsavedPrompt{}
					if a.flushDocs(docs) && proceed != nil {
						proceed()
					}
				}
			})
		})
	})
}

func (a *App) retryCloseMain(w mainWindow, goos string) {
	if a.quitting && a.win != nil {
		mygo.App.Quit()
		return
	}
	if a.win != nil {
		a.win.Close()
		return
	}
	a.closeMain(w, goos)
}
