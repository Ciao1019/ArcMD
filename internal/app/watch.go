package app

import (
	"context"
	"errors"
	"os"
	"path"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// errConflict and errMissing stop a save that would overwrite a change made
// elsewhere, or bring back a file someone moved or deleted, until the user
// chooses (conflictBar).
var (
	errConflict = errors.New("文件已在其他地方修改")
	errMissing  = errors.New("文件已被移动或删除")
)

// watchFunc follows the folder at root, calling changed on the main thread
// with what changed in it, and returns how to stop. Tests leave it nil.
type watchFunc func(root string, changed func([]mygo.FileEvent)) (stop func())

// folderWatch is a running watch of a space's folder.
type folderWatch struct {
	root string
	stop func()
}

// watchFolder is the watchFunc of the running app. The watch starts on a
// goroutine, since WatchFiles returns only once the whole tree is enrolled.
// A watch that ends (its folder removed, or too many files) is not started
// again until the space is entered anew: the tree shows its own error.
func (a *App) watchFolder(root string, changed func([]mygo.FileEvent)) func() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		w, err := mygo.WatchFiles(ctx, root, mygo.FileWatchOptions{
			Recursive: true,
			// The same folders vault.List leaves out, so that a busy .git
			// does not wake the window.
			Ignore: func(p string, isDir bool) bool {
				name := path.Base(p)
				return strings.HasPrefix(name, ".") || isDir && name == "node_modules"
			},
		})
		if err != nil {
			return
		}
		for {
			events, err := w.Next(ctx)
			if err != nil {
				return
			}
			a.win.Update(func() { changed(events) })
		}
	}()
	return cancel
}

// syncWatches keeps a watch on the active space's folder and on the folder
// of the open document, a favorite of another space perhaps, and stops the
// others.
func (a *App) syncWatches() {
	if a.watchFiles == nil {
		return
	}
	want := map[string]*space.Space{}
	if sp := a.state.ActiveSpace(); sp != nil {
		want[sp.ID] = sp
	}
	if a.doc != nil {
		want[a.doc.space.ID] = a.doc.space
	}
	for _, session := range a.sessions {
		if session.split != nil {
			for _, p := range session.split.panes {
				if p.doc != nil {
					want[p.doc.space.ID] = p.doc.space
				}
			}
		}
	}
	for id, w := range a.watches {
		if sp, ok := want[id]; !ok || sp.Root != w.root {
			w.stop()
			delete(a.watches, id)
		}
	}
	for id, sp := range want {
		if _, ok := a.watches[id]; ok {
			continue
		}
		if a.watches == nil {
			a.watches = map[string]folderWatch{}
		}
		a.watches[id] = folderWatch{sp.Root, a.watchFiles(sp.Root, func(events []mygo.FileEvent) {
			a.filesChanged(id, events)
		})}
	}
}

// filesChanged follows changes to the folder of the space with the ID:
// files appearing, going or moving redraw its tree, and a change to the
// open document's file reloads or guards it (checkDisk). Writes alone keep
// the tree, as autosave writes the open document often.
func (a *App) filesChanged(id string, events []mygo.FileEvent) {
	sp := a.state.Get(id)
	if sp == nil {
		return
	}
	docs := a.residentDocuments()
	touched := map[*document]bool{}
	structural := false
	for _, e := range events {
		if e.Op != mygo.FileWrite {
			structural = true
		}
		for _, d := range docs {
			if d.space != sp {
				continue
			}
			if e.Path == "." || within(d.rel, e.Path) || e.Op == mygo.FileRename && within(d.rel, e.OldPath) {
				touched[d] = true
			}
		}
		if e.Op == mygo.FileRename {
			a.moved(sp, e.OldPath, e.Path)
		}
	}
	if structural {
		delete(a.pageTrees, id)
		if a.state.ActiveSpace() == sp {
			a.refreshTree()
		}
	}
	for _, d := range docs {
		if touched[d] {
			a.withDocument(d, a.checkDisk)
		}
	}

}

// within reports whether rel is p or inside the folder p.
func within(rel, p string) bool {
	return rel == p || strings.HasPrefix(rel, p+"/")
}

// checkDisk compares the open document's file with what ArcMD last read
// or wrote. A change replaces a document without unsaved edits; with
// edits, it waits for the user (conflictBar). A file gone closes a
// document without edits, and keeps one with edits open, unsaved.
func (a *App) checkDisk() {
	d := a.doc
	if d == nil {
		return
	}
	text, err := vault.Read(d.space.Root, d.rel)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return
		}
		if d.loadErr != nil || !d.dirty() {
			a.closeMissing()
			return
		}
		d.missing, d.conflict = true, false
		return
	}
	d.missing = false
	if text == d.diskText {
		d.conflict = false
		return
	}
	if d.loadErr != nil || !d.dirty() {
		a.replaceDoc(text)
		return
	}
	d.conflict, d.diskNow = true, text
}

// replaceDoc shows text as the open document, where the old one was
// scrolled to.
func (a *App) replaceDoc(text string) {
	old := a.doc
	d := openDocument(old.space, old.rel, text, old.source)
	d.ed.Scroll = old.ed.Scroll
	a.replaceReferences(old, d)
}

// closeMissing closes the open document whose file is gone.
func (a *App) closeMissing() {
	d := a.doc
	a.replaceReferences(d, nil)
	a.state.Forget(d.space.ID, d.rel)
	a.saveState()
	a.notify("「%s」已被移动或删除。", d.title())
}

// useDisk drops the edits of a document in conflict for its file.
func (a *App) useDisk() {
	d := a.doc
	if d == nil {
		return
	}
	if d.missing {
		a.closeMissing()
		return
	}
	if d.conflict {
		a.replaceDoc(d.diskNow)
	}
}

// keepMine writes the edits of a document in conflict over its file, or
// where its file was.
func (a *App) keepMine() {
	d := a.doc
	if d == nil {
		return
	}
	if err := d.write(true); err != nil {
		a.notify("保存 %s 失败。%v", d.title(), err)
		return
	}
	a.followMigration()
	a.refreshTree()
}

// conflictBar sits over a document whose file changed or went elsewhere
// while it has unsaved edits. Autosave waits until the user chooses.
func (a *App) conflictBar(c *ui.Context) {
	a.conflictBarFor(c, a.doc)
}

func (a *App) conflictBarFor(c *ui.Context, d *document) {
	if d == nil || !d.conflict && !d.missing {
		return
	}
	t := c.Theme()
	msg := "「" + d.title() + i18n.T("」已在其他地方修改，这里还有未保存的修改。")
	mine, theirs := i18n.T("保留我的修改"), i18n.T("使用磁盘上的版本")
	if d.missing {
		msg = "「" + d.title() + i18n.T("」的文件已被移动或删除，这里还有未保存的修改。")
		mine, theirs = i18n.T("重新保存到原位置"), i18n.T("放弃修改并关闭")
	}
	ui.Row(c.Key("conflict")).Padding(10, 16).Gap(10).AlignItems(ui.Center).
		Background(t.Warning.Alpha(0.14)).Role(ui.RoleStatus).Children(func() {
		ui.Icon(c, iconAlert).FontSize(15).TextColor(t.Warning)
		ui.Text(c, msg).Grow(1).MinWidth(0)
		if ui.Button(c.Key("theirs"), theirs).Clicked() {
			a.activateDocument(d)
			a.useDisk()
		}
		if ui.PrimaryButton(c.Key("mine"), mine).Clicked() {
			a.activateDocument(d)
			a.keepMine()
		}
	})
}
