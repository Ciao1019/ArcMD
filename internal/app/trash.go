package app

import (
	"path"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/trashbin"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

// Recently deleted (package trashbin): what goes to the system's trash
// from ArcMD is copied first, so that the library's page can list it and
// put it back where it was for thirty days (library.go).

// keepDeleted copies the file or folder at rel of the space before it goes
// to the trash; failing, only the trash has it.
func (a *App) keepDeleted(spaceID, spaceName, root, rel string) (trashbin.Item, bool) {
	it, err := a.bin.Add(spaceID, spaceName, root, rel, time.Now())
	if err != nil {
		if trashbin.TooLarge(err) {
			a.notify("「%s」太大，ArcMD 不保留副本，可在系统废纸篓中找回。", path.Base(rel))
		}
		return trashbin.Item{}, false
	}
	return it, true
}

// restoreDeleted puts the item back in its space, numbered when its name
// is taken, and opens it when it is a document of the active space.
func (a *App) restoreDeleted(it trashbin.Item) {
	sp := a.state.Get(it.Space)
	if sp == nil {
		a.notify("「%s」所在的空间「%s」已从 ArcMD 移除，无法恢复。", it.Name(), it.SpaceName)
		return
	}
	rel, err := a.bin.Restore(it, sp.Root)
	if err != nil {
		a.notify("恢复「%s」失败。%v", it.Name(), err)
		return
	}
	a.library.items, a.library.err = a.bin.List(time.Now())
	if sp == a.state.ActiveSpace() {
		a.refreshTree()
		if dir := path.Dir(rel); dir != "." {
			sp.SetExpanded(dir, true)
		}
		if vault.IsDocument(rel) {
			a.closeLibrary()
			a.open(rel)
		}
	}
	a.notify("已恢复「%s」到 %s / %s", path.Base(rel), sp.Name, strings.TrimSuffix(rel, "/"+path.Base(rel)))
}
