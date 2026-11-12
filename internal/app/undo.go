package app

import (
	"path"
	"time"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// undoTimeout is how long the toast offering to undo a file operation
// shows: longer than a plain message, to read and reach.
const undoTimeout = 6 * time.Second

// offerUndo shows a toast saying what a file operation did, title, to
// name, with 撤销 to take it back; undo runs on the main thread.
func (a *App) offerUndo(title, name string, undo func()) {
	a.undos = append(a.undos, ui.Toast{
		ID:          "undo:" + title + ":" + name,
		Title:       title,
		Description: docName(vault.Title(name)),
		Timeout:     undoTimeout,
		Action:      i18n.T("撤销"),
		OnAction:    undo,
	})
}

// folderName names a folder of sp for people: the space's name for its
// root.
func folderName(sp *space.Space, dir string) string {
	if dir == "" {
		return sp.Name
	}
	return path.Base(dir)
}
