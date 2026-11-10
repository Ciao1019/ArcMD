package app

import (
	"errors"
	"os"
	"path"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// pickReplacementImage retains the document and block identities across the
// native dialog. Switching panes or deleting the block cannot redirect it.
func (a *App) pickReplacementImage(d *document, id, expected string) {
	current := func() bool {
		if d == nil || d.loadErr != nil || d.source || a.residentDocument(d.space.ID, d.rel) != d {
			return false
		}
		for _, leaf := range d.ed.Doc.Leaves() {
			if leaf.Block.ID == id {
				return leaf.Block.Kind == "image" && leaf.Block.URL == expected
			}
		}
		return false
	}
	if !current() {
		return
	}
	done := func(files []string) {
		if len(files) == 0 || !current() {
			return
		}
		data, err := os.ReadFile(files[0])
		if err == nil {
			_, err = ui.DecodeBitmap(data)
		}
		if err != nil {
			a.notify("无法使用所选的图片。%v", err)
			return
		}
		active := a.doc == d
		a.withDocument(d, func() {
			rel, err := a.writeAsset(path.Dir(d.rel), filepath.Base(files[0]), data)
			if err != nil {
				a.notify("替换图片失败。%v", err)
				return
			}
			if d.ed.ReplaceImage(id, expected, rel) && active {
				d.ed.RequestFocus = true
			}
		})
	}
	if a.pickFiles != nil {
		a.pickFiles(done)
		return
	}
	win := a.win
	if win == nil {
		return
	}
	opts := mygo.OpenDialogOptions{Parent: win, Title: "重新选择图片",
		Filters: []mygo.FileFilter{{Name: "图片", Extensions: imageExtensions}}}
	go func() {
		files, err := mygo.Dialog.Open(opts)
		win.Update(func() {
			if err != nil {
				a.notify("无法选择图片。%v", err)
				return
			}
			done(files)
		})
	}()
}

func (a *App) revealImage(d *document, src string) {
	file, err := localImageFile(d.space.Root, d.rel, src)
	if err != nil {
		return
	}
	for {
		_, err := os.Stat(file)
		if err == nil {
			if a.revealFile != nil {
				a.revealFile(file)
			} else {
				mygo.Shell.ShowItemInFolder(file)
			}
			return
		}
		if !errors.Is(err, os.ErrNotExist) || filepath.Dir(file) == file {
			a.notify("无法定位图片。%v", err)
			return
		}
		file = filepath.Dir(file)
	}
}
