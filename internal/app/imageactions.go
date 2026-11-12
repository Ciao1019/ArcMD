package app

import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// pickReplacementImage retains the document and block identities across the
// native dialog. Switching panes or deleting the block cannot redirect it.
func (a *App) pickReplacementImage(d *document, id, expected string) {
	current := func() bool {
		if d == nil || d.ed.ReadOnly || d.loadErr != nil || d.source || a.residentDocument(d.space.ID, d.rel) != d {
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
	opts := mygo.OpenDialogOptions{Parent: win, Title: i18n.T("重新选择图片"),
		Filters: []mygo.FileFilter{{Name: i18n.T("图片"), Extensions: imageExtensions}}}
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

// cropImage keeps the part r of the picture id of d as a new PNG beside
// the document and points the picture at it, as one step of its history.
// The original file stays: other pictures may show it.
func (a *App) cropImage(d *document, id, expected string, r image.Rectangle) {
	if d == nil || d.ed.ReadOnly || d.loadErr != nil || d.source {
		return
	}
	full, err := localImageFile(d.space.Root, d.rel, expected)
	if err != nil {
		a.notify("无法裁剪这张图片。%v", err)
		return
	}
	data, err := os.ReadFile(full)
	var src image.Image
	if err == nil {
		src, _, err = image.Decode(bytes.NewReader(data))
	}
	if err != nil {
		a.notify("无法裁剪这张图片。%v", err)
		return
	}
	r = r.Intersect(src.Bounds().Sub(src.Bounds().Min)).Add(src.Bounds().Min)
	if r.Empty() {
		return
	}
	cut := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(cut, cut.Bounds(), src, r.Min, draw.Src)
	var out bytes.Buffer
	if err := png.Encode(&out, cut); err != nil {
		a.notify("裁剪图片失败。%v", err)
		return
	}
	name := strings.TrimSuffix(filepath.Base(full), filepath.Ext(full)) + i18n.T("-裁剪.png")
	a.withDocument(d, func() {
		rel, err := a.writeAsset(path.Dir(d.rel), name, out.Bytes())
		if err != nil {
			a.notify("裁剪图片失败。%v", err)
			return
		}
		d.ed.ReplaceImage(id, expected, rel)
	})
}
