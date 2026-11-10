package app

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/Ciao1019/ArcMD/internal/history"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

type movingDocument struct {
	rel, original string
	urls          map[string]string
	live          *document
}

// moveWithResources keeps pictures, covers, undo states and snapshots with
// their documents. Shared source images are copied, never removed. File moves
// are rolled back if a resource cannot be published; originals on another disk
// remain untouched until the caller trashes them after this function succeeds.
func (a *App) moveWithResources(src *space.Space, rel string, dst *space.Space, dir string) (to string, copied bool, err error) {
	if !a.flush() {
		return "", false, errors.New("请先保存文档并解决文件冲突")
	}
	docs, err := a.movingDocuments(src, rel)
	if err != nil {
		return "", false, err
	}
	if src.ID == dst.ID {
		to, err = vault.Move(src.Root, rel, dir)
	} else {
		to, copied, err = vault.MoveTo(src.Root, rel, dst.Root, dir)
	}
	if err != nil || to == rel && src.Root == dst.Root {
		return to, copied, err
	}
	var created []string
	var rewritten []*movingDocument
	defer func() {
		if err == nil {
			return
		}
		// Restore only files written by this operation before moving back.
		for _, d := range rewritten {
			if restoreErr := vault.Write(dst.Root, to+d.rel[len(rel):], d.original); restoreErr != nil {
				err = errors.Join(err, fmt.Errorf("恢复原文：%w", restoreErr))
			}
		}
		for _, p := range created {
			_ = os.Remove(p)
		}
		if !copied {
			old, _ := vault.Resolve(src.Root, rel)
			moved, _ := vault.Resolve(dst.Root, to)
			if _, statErr := os.Lstat(old); errors.Is(statErr, fs.ErrNotExist) {
				if rollbackErr := os.Rename(moved, old); rollbackErr != nil {
					err = errors.Join(err, fmt.Errorf("移回原位置：%w", rollbackErr))
				}
			} else {
				err = errors.Join(err, errors.New("原位置已被占用，文档仍保留在目标位置"))
			}
		} else {
			// This is an unpublished copy; the user's original still exists.
			p, _ := vault.Resolve(dst.Root, to)
			_ = os.RemoveAll(p)
		}
	}()
	for _, d := range docs {
		target := to + d.rel[len(rel):]
		for old := range d.urls {
			file, resolveErr := localImageFile(src.Root, d.rel, old)
			if resolveErr != nil {
				continue
			}
			asset, relErr := filepath.Rel(src.Root, file)
			if relErr != nil {
				return to, copied, relErr
			}
			asset = filepath.ToSlash(asset)
			// Pictures inside a moved folder already live at its new path.
			if !copied && within(asset, rel) {
				file, resolveErr = vault.Resolve(dst.Root, to+asset[len(rel):])
				if resolveErr != nil {
					return to, copied, resolveErr
				}
			}
			data, readErr := os.ReadFile(file)
			if errors.Is(readErr, fs.ErrNotExist) {
				continue
			} // an already missing picture stays missing
			if readErr != nil {
				return to, copied, readErr
			}
			newURL, made, copyErr := relocateImage(dst.Root, path.Dir(target), old, data)
			if copyErr != nil {
				return to, copied, copyErr
			}
			if made != "" {
				created = append(created, made)
			}
			d.urls[old] = newURL
		}
		text, rewriteErr := relocateText(d.rel, d.original, d.urls)
		if rewriteErr != nil {
			return to, copied, rewriteErr
		}
		if text != d.original {
			if err = vault.Write(dst.Root, target, text); err != nil {
				return to, copied, err
			}
			rewritten = append(rewritten, d)
		}
	}
	err = history.CopyTo(src.Root, rel, dst.Root, to, func(name, text string) (string, error) {
		for _, d := range docs {
			if d.rel == name {
				return relocateText(name, text, d.urls)
			}
		}
		return text, nil
	})
	if err != nil {
		return to, copied, err
	}
	for _, d := range docs {
		if d.live == nil {
			continue
		}
		d.live.ed.RelocateImages(d.urls)
		text, _ := relocateText(d.rel, d.original, d.urls)
		d.live.diskText = text
		d.live.baseline, _ = d.live.ed.Committed().Encode()
		d.live.rememberBaseline(d.live.ed.Committed())
		if d.live.source {
			d.live.preview = d.live.text()
		}
	}
	if cleanupErr := history.Forget(src.Root, rel); cleanupErr != nil {
		a.notify("文档和历史已移动；原位置的历史副本未能清理。%v", cleanupErr)
	}
	return to, copied, nil
}

func (a *App) movingDocuments(sp *space.Space, rel string) ([]*movingDocument, error) {
	file, err := vault.Resolve(sp.Root, rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(file)
	if err != nil {
		return nil, err
	}
	paths := []vault.Doc{{Path: rel}}
	if info.IsDir() {
		paths, err = vault.List(file)
		if err != nil {
			return nil, err
		}
		for i := range paths {
			paths[i].Path = path.Join(rel, paths[i].Path)
		}
	}
	var docs []*movingDocument
	for _, p := range paths {
		if !vault.IsDocument(p.Path) {
			continue
		}
		text, err := vault.Read(sp.Root, p.Path)
		if err != nil {
			return nil, err
		}
		d := &movingDocument{rel: p.Path, original: text, urls: map[string]string{}, live: a.residentDocument(sp.ID, p.Path)}
		collect := func(text string) error {
			doc, err := imageDocument(p.Path, text)
			if err != nil {
				return err
			}
			doc.MapImages(func(url string) string { d.urls[url] = url; return url })
			return nil
		}
		if err := collect(text); err != nil {
			return nil, err
		}
		if d.live != nil {
			for _, url := range d.live.ed.ImageURLs() {
				d.urls[url] = url
			}
		}
		versions, err := history.List(sp.Root, p.Path)
		if err != nil {
			return nil, err
		}
		for _, v := range versions {
			text, err := history.Read(sp.Root, p.Path, v)
			if err != nil {
				return nil, err
			}
			if err := collect(text); err != nil {
				return nil, err
			}
		}
		docs = append(docs, d)
	}
	return docs, nil
}

func imageDocument(rel, text string) (*model.Document, error) {
	if vault.IsMarkdown(rel) {
		return editor.ImportMarkdown(text), nil
	}
	return model.Decode(text)
}

func relocateText(rel, text string, urls map[string]string) (string, error) {
	changed := false
	for old, newURL := range urls {
		changed = changed || old != newURL
	}
	if !changed {
		return text, nil
	}
	if vault.IsMarkdown(rel) {
		// Preserve Markdown source exactly. Refuse an ambiguous destination
		// rather than round-trip a user's import source through the exporter.
		return "", errors.New("移动图片需要调整引用；请先将 Markdown 转为 ArcMD 文档再移动")
	}
	doc, err := model.Decode(text)
	if err != nil {
		return "", err
	}
	doc.MapImages(func(url string) string {
		if to, ok := urls[url]; ok {
			return to
		}
		return url
	})
	return doc.Encode()
}

func relocateImage(root, dir, original string, data []byte) (string, string, error) {
	decoded, _ := url.PathUnescape(original)
	stem := strings.TrimSuffix(assetName(path.Base(decoded)), path.Ext(decoded))
	ext := path.Ext(decoded)
	for i := 0; ; i++ {
		candidate, label := path.Join(dir, decoded), original
		if i > 0 {
			label = path.Join(assetsDir, fmt.Sprintf("%s-%d%s", stem, i, ext))
			candidate = path.Join(dir, label)
		}
		full, err := vault.Resolve(root, candidate)
		if err != nil {
			if i == 0 {
				// Parent references may have been readable outside the source
				// Space. Copy them into assets, never outside the destination.
				continue
			}
			return "", "", err
		}
		old, err := os.ReadFile(full)
		if err == nil {
			if bytes.Equal(old, data) {
				return label, "", nil
			}
			continue
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", "", err
		}
		if err := fsutil.CreateFileAtomic(full, data, 0o644); errors.Is(err, fs.ErrExist) {
			continue
		} else if err != nil {
			return "", "", err
		}
		return label, full, nil
	}
}
