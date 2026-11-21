package app

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/Ciao1019/ArcMD/internal/history"
	"github.com/Ciao1019/ArcMD/internal/i18n"
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
	if dst.IsExternal() {
		return "", false, errors.New(i18n.T("外部空间只记录文件，不能作为移动目标。"))
	}
	if !a.flush() {
		return "", false, errors.New(i18n.T("请先保存文档并解决文件冲突"))
	}
	docs, err := a.movingDocuments(src, rel)
	if err != nil {
		return "", false, err
	}
	sourceRoot, sourcePath, err := fileLocation(src, rel)
	if err != nil {
		return "", false, err
	}
	if src.ID == dst.ID {
		to, err = vault.Move(sourceRoot, sourcePath, dir)
	} else {
		to, copied, err = vault.MoveTo(sourceRoot, sourcePath, dst.Root, dir)
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
				err = errors.Join(err, fmt.Errorf(i18n.T("恢复原文：%w"), restoreErr))
			}
		}
		for _, p := range created {
			_ = os.Remove(p)
		}
		if !copied {
			old, _ := vault.Resolve(sourceRoot, sourcePath)
			moved, _ := vault.Resolve(dst.Root, to)
			if _, statErr := os.Lstat(old); errors.Is(statErr, fs.ErrNotExist) {
				if rollbackErr := os.Rename(moved, old); rollbackErr != nil {
					err = errors.Join(err, fmt.Errorf(i18n.T("移回原位置：%w"), rollbackErr))
				}
			} else {
				err = errors.Join(err, errors.New(i18n.T("原位置已被占用，文档仍保留在目标位置")))
			}
		} else {
			// This is an unpublished copy; the user's original still exists.
			p, _ := vault.Resolve(dst.Root, to)
			_ = os.RemoveAll(p)
		}
	}()
	for _, d := range docs {
		target := to + d.rel[len(rel):]
		targetDir := path.Dir(target)
		for old := range d.urls {
			oldBase, oldFrag, hasFrag := strings.Cut(old, "#")
			if vault.IsWhiteboard(oldBase) || vault.IsPDF(oldBase) {
				clean := strings.TrimPrefix(path.Clean(unescapeImagePath(oldBase)), "/")
				if !underAttachmentFolder(clean, assetsDir) && !underAttachmentFolder(clean, attachmentFolder(src)) {
					continue
				}
			}
			file, resolveErr := resolveSpaceImage(src, d.rel, oldBase)
			if resolveErr != nil {
				continue
			}
			asset, relErr := filepath.Rel(sourceRoot, file)
			if relErr != nil {
				return to, copied, relErr
			}
			asset = filepath.ToSlash(asset)
			// Whether the picture belongs to the space's attachment folder,
			// and where it ends up: pictures inside a moved folder already
			// live at its new path, the others stay put.
			inFolder := underAttachmentFolder(asset, attachmentFolder(src))
			destRel := asset
			if !copied && within(asset, sourcePath) {
				destRel = to + asset[len(sourcePath):]
				file, resolveErr = vault.Resolve(dst.Root, destRel)
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
			var newURL string
			switch {
			case src.ID == dst.ID && inFolder:
				// An attachment of a space with an attachment folder stays
				// in it; only the URL follows the document's new directory,
				// so nothing is copied twice.
				newURL = docRelativeURL(targetDir, destRel)
			default:
				// The picture is copied beside the document at its new
				// place — or into the destination space's attachment folder
				// when that space has one — under the name it can keep, or
				// a numbered one when the name is taken by other content.
				// An assets/ or source-folder prefix has no meaning there
				// and is left out.
				folderMode := attachmentFolder(dst) != ""
				destDir, useFolder := targetDir, false
				orig := oldBase
				if folderMode {
					destDir, useFolder = attachmentFolder(dst), true
					clean := path.Clean(strings.ReplaceAll(unescapeImagePath(oldBase), `\`, "/"))
					for strings.HasPrefix(clean, "../") {
						clean = strings.TrimPrefix(clean, "../")
					}
					orig = clean
					switch {
					case attachmentFolder(src) != "" && attachmentFolder(src) != "." && strings.HasPrefix(asset, attachmentFolder(src)+"/"):
						orig = strings.TrimPrefix(asset, attachmentFolder(src)+"/")
					case strings.HasPrefix(clean, assetsDir+"/"):
						orig = strings.TrimPrefix(clean, assetsDir+"/")
					}
				}
				fileRel, made, firstUsed, copyErr := publishCopy(dst.Root, destDir, orig, data, useFolder)
				if copyErr != nil {
					return to, copied, copyErr
				}
				if made != "" {
					created = append(created, made)
				}
				switch {
				case useFolder:
					newURL = docRelativeURL(targetDir, fileRel)
				case firstUsed:
					newURL = oldBase // the picture sits where the URL points
				default:
					newURL = path.Join(assetsDir, path.Base(fileRel))
				}
			}
			if hasFrag {
				newURL += "#" + oldFrag
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
		if d.live.isWhiteboard() || d.live.isPDF() {
			if d.live.isWhiteboard() {
				d.live.diskText = d.original
			}
			continue
		}
		d.live.ed.RelocateImages(d.urls)
		text, _ := relocateText(d.rel, d.original, d.urls)
		d.live.diskText = text
		d.live.baseline, _ = d.live.ed.Committed().Digest()
		d.live.rememberBaseline(d.live.ed.Committed())
		if d.live.source {
			d.live.preview = d.live.text()
		}
	}
	if cleanupErr := history.Forget(src.Root, rel); cleanupErr != nil {
		a.notify("文档和历史已移动；原位置的历史副本未能清理。%v", cleanupErr)
	}
	a.invalidateResolvedImages()
	return to, copied, nil
}

func (a *App) movingDocuments(sp *space.Space, rel string) ([]*movingDocument, error) {
	file, err := resolveFile(sp, rel)
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
		live := a.residentDocument(sp.ID, p.Path)
		if live != nil {
			live.waitAsyncSave()
		}
		if vault.IsPDF(p.Path) {
			docs = append(docs, &movingDocument{rel: p.Path, urls: map[string]string{}, live: live})
			continue
		}
		text, err := readFile(sp, p.Path)
		if err != nil {
			return nil, err
		}
		d := &movingDocument{rel: p.Path, original: text, urls: map[string]string{}, live: live}
		if vault.IsWhiteboard(p.Path) {
			docs = append(docs, d)
			continue
		}
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
	if vault.IsWhiteboard(rel) || vault.IsPDF(rel) {
		return model.New(), nil
	}
	if vault.IsMarkdown(rel) {
		if strings.HasPrefix(strings.TrimSpace(text), "{") {
			if doc, err := model.Decode(text); err == nil {
				return doc, nil
			}
		}
		return editor.ImportMarkdown(text), nil
	}
	return model.Decode(text)
}

func relocateText(rel, text string, urls map[string]string) (string, error) {
	if vault.IsWhiteboard(rel) || vault.IsPDF(rel) {
		return text, nil
	}
	changed := false
	for old, newURL := range urls {
		changed = changed || old != newURL
	}
	if !changed {
		return text, nil
	}
	mapURLs := func(doc *model.Document) {
		doc.MapImages(func(url string) string {
			if to, ok := urls[url]; ok {
				return to
			}
			return url
		})
	}
	if vault.IsMarkdown(rel) {
		if strings.HasPrefix(strings.TrimSpace(text), "{") {
			if doc, err := model.Decode(text); err == nil {
				mapURLs(doc)
				return doc.Encode()
			}
		}
		doc := editor.ImportMarkdown(text)
		mapURLs(doc)
		if err := doc.Validate(); err != nil {
			return "", err
		}
		return editor.ExportMarkdown(doc), nil
	}
	doc, err := model.Decode(text)
	if err != nil {
		return "", err
	}
	mapURLs(doc)
	return doc.Encode()
}

// publishCopy copies data into the folder destDir of root at the document
// relative path the URL decoded names, or, when that is taken by other
// content or escapes the space, under a numbered name — inside destDir
// itself when it is an attachment folder, in its assets folder otherwise.
// It returns the space-relative file location, the file it created (empty
// when the file was already there with the same content) and whether the
// first candidate was used.
func publishCopy(root, destDir, original string, data []byte, folderMode bool) (fileRel, made string, firstUsed bool, err error) {
	decoded := unescapeImagePath(original)
	// Inside an attachment folder the copy keeps the name the URL points
	// at, minus parent segments: ../top.png names the space's root of the
	// old arrangement, and the copy belongs at the folder's top, never
	// outside it.
	cleaned := path.Clean(decoded)
	if folderMode {
		for strings.HasPrefix(cleaned, "../") {
			cleaned = cleaned[len("../"):]
		}
		if cleaned == ".." {
			cleaned = "."
		}
	}
	ext := vault.FileExt(decoded)
	stem := strings.TrimSuffix(assetName(path.Base(decoded)), ext)
	numbered := func(i int) string {
		name := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if folderMode {
			return path.Join(destDir, name)
		}
		return path.Join(destDir, assetsDir, name)
	}
	for i := 0; ; i++ {
		rel := numbered(i)
		if i == 0 && cleaned != "." && cleaned != "" {
			rel = path.Join(destDir, cleaned)
		}
		full, resErr := vault.Resolve(root, rel)
		if resErr != nil {
			if i == 0 {
				// Parent references may have been readable outside the
				// source Space. Copy them into the destination, never
				// outside it.
				continue
			}
			return "", "", false, resErr
		}
		old, readErr := os.ReadFile(full)
		if readErr == nil {
			if bytes.Equal(old, data) {
				return rel, "", i == 0, nil
			}
			continue
		}
		if !errors.Is(readErr, fs.ErrNotExist) {
			return "", "", false, readErr
		}
		if err := fsutil.CreateFileAtomic(full, data, 0o644); errors.Is(err, fs.ErrExist) {
			continue
		} else if err != nil {
			return "", "", false, err
		}
		return rel, full, i == 0, nil
	}
}
