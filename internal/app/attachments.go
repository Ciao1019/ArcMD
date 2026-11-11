package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
)

const assetsDir = "assets"

var imageExtensions = []string{"png", "jpg", "jpeg", "gif", "webp", "bmp"}

// pickImages asks for pictures, copies them beside the document and
// inserts them at the caret as one step of the undo history.
func (a *App) pickImages() {
	d := a.doc
	if d == nil || d.ed.ReadOnly {
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
	opts := mygo.OpenDialogOptions{Parent: win, Title: "插入图片", Multiple: true,
		Filters: []mygo.FileFilter{{Name: "图片", Extensions: imageExtensions}}}
	go func() {
		paths, err := mygo.Dialog.Open(opts)
		if err == nil && len(paths) > 0 {
			win.Update(func() { done(paths) })
		}
	}()
}

// pickCover asks for a picture for the open document's cover, copies it
// beside the document and gives its URL to done.
func (a *App) pickCover(done func(url string)) {
	d := a.doc
	if d == nil || d.ed.ReadOnly {
		return
	}
	got := func(paths []string) {
		if a.residentDocument(d.space.ID, d.rel) != d || d.ed.ReadOnly || len(paths) == 0 {
			return
		}
		if !isImage(paths[0]) {
			a.notify("封面只能是图片（%s）。", strings.Join(imageExtensions, "、"))
			return
		}
		var rel string
		var err error
		a.withDocument(d, func() { rel, _, err = a.copyAsset(path.Dir(d.rel), paths[0]) })
		if err != nil {
			a.notify("设置封面失败。%v", err)
			return
		}
		done(rel)
	}
	if a.pickFiles != nil {
		a.pickFiles(got)
		return
	}
	win := a.win
	opts := mygo.OpenDialogOptions{Parent: win, Title: "选择封面图片",
		Filters: []mygo.FileFilter{{Name: "图片", Extensions: imageExtensions}}}
	go func() {
		paths, err := mygo.Dialog.Open(opts)
		if err == nil && len(paths) > 0 {
			win.Update(func() { got(paths) })
		}
	}()
}

// isImage reports whether a file's name says it is a picture ArcMD shows.
func isImage(name string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	return slices.Contains(imageExtensions, ext)
}

// insertImages copies the pictures among paths beside the document and
// inserts them, in order, as one step of the undo history: before the
// top-level block at index, or at the caret for -1. Files that are no
// pictures, folders included, are left out with a notice, as Markdown
// has no attachments.
func (a *App) insertImages(paths []string, at int) {
	if a.doc == nil || a.doc.ed.ReadOnly {
		return
	}
	d := a.doc
	if d == nil || d.loadErr != nil || d.source {
		return
	}
	var blocks []*model.Block
	var skipped []string
	for _, src := range paths {
		if info, err := os.Stat(src); err == nil && info.IsDir() || !isImage(src) {
			skipped = append(skipped, filepath.Base(src))
			continue
		}
		rel, _, err := a.copyAsset(path.Dir(d.rel), src)
		if err != nil {
			a.notify("插入 %s 失败。%v", filepath.Base(src), err)
			continue
		}
		blocks = append(blocks, imageBlock(filepath.Base(src), rel))
	}
	if len(skipped) > 0 {
		a.notify("只能插入图片（%s）：%s 未插入。", strings.Join(imageExtensions, "、"), strings.Join(skipped, "、"))
	}
	a.placeImages(blocks, at)
}

// pasteImage saves a picture pasted from the clipboard beside the document
// as a PNG and inserts it at the caret.
func (a *App) pasteImage(png []byte) {
	if a.doc == nil || a.doc.ed.ReadOnly {
		return
	}
	d := a.doc
	if d == nil || d.loadErr != nil || d.source {
		return
	}
	rel, err := a.writeAsset(path.Dir(d.rel), "粘贴的图片.png", png)
	if err != nil {
		a.notify("粘贴图片失败。%v", err)
		return
	}
	a.placeImages([]*model.Block{imageBlock("粘贴的图片", rel)}, -1)
}

func imageBlock(alt, rel string) *model.Block {
	block := model.NewBlock("image", alt)
	block.URL = rel
	return block
}

// placeImages inserts image blocks as one step of the history: before the
// top-level block at index, or at the caret for -1.
func (a *App) placeImages(blocks []*model.Block, at int) {
	if a.doc == nil || a.doc.ed.ReadOnly {
		return
	}
	if len(blocks) == 0 {
		return
	}
	ed := a.doc.ed
	if at >= 0 {
		ed.Engine.InsertAt(at, blocks...)
	} else {
		ed.Engine.Edit(func() {
			for _, block := range blocks {
				ed.Engine.Insert(block)
			}
		})
	}
	ed.RequestFocus = true
}

// copyAsset copies the file at src into the folder assets beside the
// documents of dir, under a name not taken yet, and returns its URL from
// them, and its bytes.
func (a *App) copyAsset(dir, src string) (string, []byte, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return "", nil, err
	}
	rel, err := a.writeAsset(dir, filepath.Base(src), data)
	return rel, data, err
}

// writeAsset writes data into the folder assets beside the documents of
// dir as a new file named for name, numbered when the name is taken, and
// returns its URL from them. It never replaces a file.
func (a *App) writeAsset(dir, name string, data []byte) (string, error) {
	root := a.doc.space.Root
	base := assetName(name)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; ; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		rel := path.Join(assetsDir, name)
		full, err := vault.Resolve(root, path.Join(dir, rel))
		if err != nil {
			return "", err
		}
		if _, err := os.Lstat(full); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return "", err
		}
		if err := fsutil.CreateFileAtomic(full, data, 0o644); err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue // taken meanwhile
			}
			return "", err
		}
		return rel, nil
	}
}

// assetName is a file's name as a URL can hold it without escapes:
// spaces and the characters Markdown's links mind become dashes.
func assetName(name string) string {
	r := strings.NewReplacer(" ", "-", "(", "-", ")", "-", "[", "-", "]", "-", "<", "-", ">", "-", "#", "-", "?", "-", "%", "-")
	name = r.Replace(name)
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	return name
}
