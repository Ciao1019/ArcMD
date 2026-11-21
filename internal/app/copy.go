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
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

// Sidebar copy, paste and duplicate: documents and folders copied from the
// sidebar (single row, multi-selection, favorite tile or command bar) are
// kept as space-qualified places so they can be pasted beside themselves,
// into another folder or into another space, preserving attachments while
// leaving the originals untouched.

// copyEntries puts rels of sp onto the sidebar clipboard, deduplicating items
// whose ancestor folder is already included.
func (a *App) copyEntries(sp *space.Space, rels []string) {
	if sp == nil || len(rels) == 0 {
		return
	}
	var clean []string
	for _, r := range rels {
		if r != "" && !slices.Contains(clean, r) {
			clean = append(clean, r)
		}
	}
	items := topLevelPaths(clean)
	if len(items) == 0 {
		return
	}
	places := make([]place, len(items))
	for i, r := range items {
		places[i] = place{space: sp.ID, rel: r}
	}
	a.copied = places
	a.dismissPreview()
	if len(items) == 1 {
		a.notify("已复制「%s」", entryDisplayName(items[0]))
		return
	}
	a.notify("已复制 %d 个项目", len(items))
}

// entryDisplayName formats a document or folder path for toast notices.
func entryDisplayName(rel string) string {
	if vault.IsDocument(rel) {
		return docName(vault.Title(rel))
	}
	return path.Base(rel)
}

// pasteMenuLabel names the Paste menu item, showing the count when multiple
// sidebar items are on the clipboard.
func (a *App) pasteMenuLabel() string {
	if len(a.copied) > 1 {
		return fmt.Sprintf(i18n.T("粘贴 %d 项"), len(a.copied))
	}
	return i18n.T("粘贴")
}

// canPasteInto reports whether the sidebar clipboard can be pasted into the
// folder dir ("" for root) of dst. External spaces cannot receive new files,
// and a folder cannot be pasted into itself or one of its descendants.
func (a *App) canPasteInto(dst *space.Space, dir string) bool {
	if dst == nil || dst.IsExternal() || len(a.copied) == 0 {
		return false
	}
	anyValid := false
	for _, p := range a.copied {
		if a.state.Get(p.space) == nil {
			continue
		}
		if p.space == dst.ID && dir != "" && under(dir, p.rel) {
			return false
		}
		anyValid = true
	}
	return anyValid
}

// pasteInto copies everything on the sidebar clipboard into dir of dst. When
// afterRel is a sibling in dir and dir uses manual ordering, the new copies
// are placed immediately after it.
func (a *App) pasteInto(dst *space.Space, dir, afterRel string) {
	if !a.canPasteInto(dst, dir) {
		return
	}
	if a.autosaveEnabled() && !a.flush() {
		return
	}
	items := slices.Clone(a.copied)
	if len(items) == 0 {
		return
	}

	var beforeNames []string
	insertAfter := -1
	if dst == a.state.ActiveSpace() && dst.SortOf(dir) == "" && afterRel != "" &&
		parentDir(afterRel) == dir && !dst.IsPinned(afterRel) && !a.state.IsFavorite(dst.ID, afterRel) {
		kids := a.childrenOf(dir)
		beforeNames = make([]string, len(kids))
		for i, k := range kids {
			beforeNames[i] = k.Name
		}
		insertAfter = slices.Index(beforeNames, path.Base(afterRel))
	}

	var created []string
	var createdNames []string
	for _, p := range items {
		src := a.state.Get(p.space)
		if src == nil {
			continue
		}
		if src.ID == dst.ID && dir != "" && under(dir, p.rel) {
			continue
		}
		full, err := resolveFile(src, p.rel)
		if err != nil {
			a.notify("无法粘贴「%s」：源文件已不存在。", entryDisplayName(p.rel))
			continue
		}
		if _, err := os.Stat(full); errors.Is(err, fs.ErrNotExist) {
			a.notify("无法粘贴「%s」：源文件已不存在。", entryDisplayName(p.rel))
			continue
		}
		to, err := a.copyWithResources(src, p.rel, dst, dir)
		if err != nil {
			a.notify("粘贴 %s 失败。%v", path.Base(p.rel), err)
			continue
		}
		created = append(created, to)
		createdNames = append(createdNames, path.Base(to))
	}
	if len(created) == 0 {
		return
	}
	expandDirChain(dst, dir)
	if insertAfter >= 0 {
		if dst.Order == nil {
			dst.Order = map[string][]string{}
		}
		dst.Order[dir] = slices.Insert(beforeNames, insertAfter+1, createdNames...)
	}
	a.clearPicks("")
	a.dismissPreview()
	a.saveState()
	delete(a.pageTrees, dst.ID)
	a.refreshTree()
	a.refreshSearch()

	targets := a.trackUndoCopies(dst.ID, created)
	a.offerUndo(fmt.Sprintf(i18n.T("已粘贴到「%s」"), folderName(dst, dir)), countLabel(created), func() {
		a.trashCopied(targets)
	})
}

// duplicateEntry makes an in-place copy of rel beside itself in sp. When the
// sidebar is visible on sp, inline renaming starts on the duplicate (and
// confirming Enter on a duplicated document opens and focuses it); otherwise
// a duplicated document opens immediately.
func (a *App) duplicateEntry(sp *space.Space, rel string) {
	if sp == nil || sp.IsExternal() || rel == "" {
		return
	}
	live := a.residentDocument(sp.ID, rel)
	if a.autosaveEnabled() && !a.flush() {
		return
	}
	if live != nil {
		rel = live.rel
	}
	dir := parentDir(rel)

	var beforeNames []string
	insertAfter := -1
	if sp == a.state.ActiveSpace() && sp.SortOf(dir) == "" &&
		!sp.IsPinned(rel) && !a.state.IsFavorite(sp.ID, rel) {
		kids := a.childrenOf(dir)
		beforeNames = make([]string, len(kids))
		for i, k := range kids {
			beforeNames[i] = k.Name
		}
		insertAfter = slices.Index(beforeNames, path.Base(rel))
	}

	to, err := a.copyWithResources(sp, rel, sp, dir)
	if err != nil {
		a.notify("创建 %s 的副本失败。%v", path.Base(rel), err)
		return
	}
	expandDirChain(sp, dir)
	if insertAfter >= 0 {
		if sp.Order == nil {
			sp.Order = map[string][]string{}
		}
		sp.Order[dir] = slices.Insert(beforeNames, insertAfter+1, path.Base(to))
	}
	a.clearPicks("")
	a.dismissPreview()
	a.saveState()
	delete(a.pageTrees, sp.ID)
	a.refreshTree()
	a.refreshSearch()

	sidebarVisible := sp == a.state.ActiveSpace() && !a.focus && (!a.folded() || a.peek)
	if sidebarVisible {
		a.renameIn(sp, to)
		a.pendingDupUndo = &place{space: sp.ID, rel: to}
		if vault.IsDocument(to) {
			a.openAfterRename = to
		}
		return
	}
	targets := a.trackUndoCopies(sp.ID, []string{to})
	a.offerUndo(i18n.T("已创建副本"), path.Base(to), func() {
		a.trashCopied(targets)
	})
	if vault.IsDocument(to) {
		a.openAnywhere(sp.ID, to)
	}
}

// flushPendingDupUndo emits the deferred Undo toast for a freshly duplicated
// item once its inline rename finishes or is canceled, using the final path
// and starting a fresh timeout.
func (a *App) flushPendingDupUndo(finalSpace, finalRel string) {
	p := a.pendingDupUndo
	if p == nil {
		return
	}
	a.pendingDupUndo = nil
	if finalSpace != "" && finalRel != "" {
		p.space, p.rel = finalSpace, finalRel
	}
	targets := a.trackUndoCopies(p.space, []string{p.rel})
	a.offerUndo(i18n.T("已创建副本"), path.Base(p.rel), func() {
		a.trashCopied(targets)
	})
}

// expandDirChain expands dir and every ancestor folder in sp so newly pasted
// or duplicated items inside it are immediately visible in the sidebar.
func expandDirChain(sp *space.Space, dir string) {
	for cur := dir; cur != "" && cur != "." && cur != "/"; cur = path.Dir(cur) {
		sp.SetExpanded(cur, true)
		if !strings.Contains(cur, "/") {
			break
		}
	}
}

// trackUndoCopies registers mutable place pointers for the items created by a
// paste or duplicate so clicking Undo still finds them even after an inline
// rename.
func (a *App) trackUndoCopies(spID string, rels []string) []*place {
	targets := make([]*place, len(rels))
	for i, rel := range rels {
		targets[i] = &place{space: spID, rel: rel}
	}
	a.recentCopies = append(a.recentCopies, targets...)
	if len(a.recentCopies) > 64 {
		a.recentCopies = a.recentCopies[len(a.recentCopies)-64:]
	}
	return targets
}

// trashCopied moves the items created by an undone paste or duplicate to the
// system trash (with trashbin backups), never permanently deleting user files.
func (a *App) trashCopied(targets []*place) {
	if len(targets) == 0 || (a.autosaveEnabled() && !a.flush()) {
		return
	}
	changed := false
	for i := len(targets) - 1; i >= 0; i-- {
		t := targets[i]
		if t == nil || t.rel == "" {
			continue
		}
		sp := a.state.Get(t.space)
		if sp == nil || sp.IsExternal() {
			continue
		}
		p, err := resolveFile(sp, t.rel)
		if err != nil {
			continue
		}
		if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if a.renameSpace == sp.ID && a.renaming != "" && under(a.renaming, t.rel) {
			a.renaming = ""
			a.openAfterRename = ""
		}
		kept, ok := a.keepDeleted(sp.ID, sp.Name, sp.Root, t.rel)
		if err := a.trashItem(p); err != nil {
			if ok {
				a.bin.Forget(kept)
			}
			a.notify("将 %s 移到废纸篓失败。%v", path.Base(t.rel), err)
			continue
		}
		for _, d := range a.residentDocuments() {
			if d.space == sp && within(d.rel, t.rel) {
				a.replaceReferences(d, nil)
			}
		}
		a.forgetPicks(sp.ID, t.rel)
		a.forgetCopied(sp.ID, t.rel)
		a.state.Forget(sp.ID, t.rel)
		delete(a.pageTrees, sp.ID)
		changed = true
	}
	if changed {
		a.saveState()
		a.refreshTree()
		a.refreshSearch()
	}
}

// followCopied updates copied clipboard entries and active copy-undo targets
// when a file or folder moves or is renamed.
func (a *App) followCopied(fromSpace, fromRel, toSpace, toRel string) {
	if fromSpace == toSpace && fromRel == toRel {
		return
	}
	for i, p := range a.copied {
		if p.space == fromSpace && under(p.rel, fromRel) {
			a.copied[i] = place{space: toSpace, rel: toRel + p.rel[len(fromRel):]}
		}
	}
	for _, p := range a.recentCopies {
		if p != nil && p.space == fromSpace && under(p.rel, fromRel) {
			p.space = toSpace
			p.rel = toRel + p.rel[len(fromRel):]
		}
	}
}

// forgetCopied drops rel and anything inside it from the sidebar clipboard.
func (a *App) forgetCopied(spID, rel string) {
	if len(a.copied) > 0 {
		a.copied = slices.DeleteFunc(a.copied, func(p place) bool {
			return p.space == spID && under(p.rel, rel)
		})
	}
}

// forgetCopiedSpace drops clipboard entries belonging to a removed space.
func (a *App) forgetCopiedSpace(spID string) {
	if len(a.copied) > 0 {
		a.copied = slices.DeleteFunc(a.copied, func(p place) bool {
			return p.space == spID
		})
	}
	if len(a.recentCopies) > 0 {
		a.recentCopies = slices.DeleteFunc(a.recentCopies, func(p *place) bool {
			return p == nil || p.space == spID
		})
	}
}

// copyWithResources copies a document or folder from src into dir of dst,
// keeping local images and covers resolvable in the copy while leaving the
// source document, its assets and its version history untouched. Legacy
// .arcmd copies receive fresh block IDs. Any failure rolls back everything
// this call created.
func (a *App) copyWithResources(src *space.Space, rel string, dst *space.Space, dir string) (to string, err error) {
	if dst == nil || dst.IsExternal() {
		return "", errors.New(i18n.T("外部空间只记录文件，不能作为粘贴目标。"))
	}
	if a.autosaveEnabled() && !a.flush() {
		return "", errors.New(i18n.T("请先保存文档并解决文件冲突"))
	}
	docs, err := a.copyDocuments(src, rel)
	if err != nil {
		return "", err
	}
	sourceRoot, sourcePath, err := fileLocation(src, rel)
	if err != nil {
		return "", err
	}
	to, err = vault.CopyTo(sourceRoot, sourcePath, dst.Root, dir)
	if err != nil {
		return "", err
	}
	copiedTo := to
	var created []string
	defer func() {
		if err == nil {
			return
		}
		for _, p := range created {
			_ = os.Remove(p)
		}
		if full, rerr := vault.Resolve(dst.Root, copiedTo); rerr == nil {
			_ = os.RemoveAll(full)
		}
	}()
	for _, d := range docs {
		target := copiedTo + d.rel[len(rel):]
		targetDir := path.Dir(target)
		if targetDir == "." {
			targetDir = ""
		}
		srcDir := path.Dir(d.rel)
		if srcDir == "." {
			srcDir = ""
		}
		coverURLs := map[string]string{}
		sameDir := src.ID == dst.ID && targetDir == srcDir
		if !sameDir {
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
					return "", relErr
				}
				asset = filepath.ToSlash(asset)
				inFolder := underAttachmentFolder(asset, attachmentFolder(src))
				data, readErr := os.ReadFile(file)
				if errors.Is(readErr, fs.ErrNotExist) {
					continue // an already missing picture stays missing
				}
				if readErr != nil {
					return "", readErr
				}
				var newURL, coverURL string
				switch {
				case src.ID == dst.ID && inFolder:
					// An attachment in the space's shared attachment folder stays
					// put; only the copied document's relative URL updates.
					newURL = docRelativeURL(targetDir, asset)
					coverURL = escapeImagePath(asset)
				default:
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
						return "", copyErr
					}
					if made != "" {
						created = append(created, made)
						a.markSelfWrite(made)
					}
					switch {
					case useFolder:
						newURL = docRelativeURL(targetDir, fileRel)
						coverURL = escapeImagePath(fileRel)
					case firstUsed:
						newURL = oldBase
						coverURL = oldBase
					default:
						newURL = path.Join(assetsDir, path.Base(fileRel))
						coverURL = newURL
					}
				}
				if hasFrag {
					newURL += "#" + oldFrag
				}
				d.urls[old] = newURL
				if model.ValidCover(newURL) {
					coverURLs[old] = newURL
				} else if model.ValidCover(coverURL) {
					coverURLs[old] = coverURL
				}
			}
		}
		text, rewriteErr := copyDocumentText(target, d.original, d.urls, coverURLs)
		if rewriteErr != nil {
			return "", rewriteErr
		}
		if text != d.original {
			if err = vault.Write(dst.Root, target, text); err != nil {
				return "", err
			}
		}
	}
	a.invalidateResolvedImages()
	return copiedTo, nil
}

// copyDocuments collects the current content and referenced local image/cover
// URLs for the file or folder at rel of sp, without inspecting version history
// (copies start with no history).
func (a *App) copyDocuments(sp *space.Space, rel string) ([]*movingDocument, error) {
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
		if live := a.residentDocument(sp.ID, p.Path); live != nil {
			live.waitAsyncSave()
		}
		if vault.IsPDF(p.Path) {
			docs = append(docs, &movingDocument{rel: p.Path, urls: map[string]string{}})
			continue
		}
		text, err := readFile(sp, p.Path)
		if err != nil {
			return nil, err
		}
		d := &movingDocument{rel: p.Path, original: text, urls: map[string]string{}}
		if vault.IsWhiteboard(p.Path) {
			docs = append(docs, d)
			continue
		}
		doc, err := imageDocument(p.Path, text)
		if err != nil {
			return nil, err
		}
		doc.MapImages(func(url string) string { d.urls[url] = url; return url })
		docs = append(docs, d)
	}
	return docs, nil
}

// copyDocumentText prepares a copied document's text: legacy .arcmd files
// always receive fresh block IDs (and any rebased image URLs); Markdown files
// stay byte-identical unless an image or cover URL changed.
func copyDocumentText(rel, text string, urls, coverURLs map[string]string) (string, error) {
	if vault.IsWhiteboard(rel) || vault.IsPDF(rel) {
		return text, nil
	}
	mapCopyURLs := func(doc *model.Document) {
		if doc.Cover != "" {
			if to, ok := coverURLs[doc.Cover]; ok {
				doc.Cover = to
			}
		}
		var walk func([]*model.Block)
		walk = func(blocks []*model.Block) {
			for _, b := range blocks {
				if b.Kind == "image" {
					if to, ok := urls[b.URL]; ok {
						b.URL = to
					}
				}
				walk(b.Children)
			}
		}
		walk(doc.Blocks)
	}
	if strings.EqualFold(path.Ext(rel), ".arcmd") {
		doc, err := model.Decode(text)
		if err != nil {
			return "", err
		}
		refreshBlockIDs(doc.Blocks)
		mapCopyURLs(doc)
		return doc.Encode()
	}
	changed := false
	for old, newURL := range urls {
		if old != newURL {
			changed = true
			break
		}
	}
	if !changed {
		for old, newURL := range coverURLs {
			if old != newURL {
				changed = true
				break
			}
		}
	}
	if !changed {
		return text, nil
	}
	if vault.IsMarkdown(rel) {
		if strings.HasPrefix(strings.TrimSpace(text), "{") {
			if doc, err := model.Decode(text); err == nil {
				refreshBlockIDs(doc.Blocks)
				mapCopyURLs(doc)
				return doc.Encode()
			}
		}
		doc := editor.ImportMarkdown(text)
		mapCopyURLs(doc)
		if err := doc.Validate(); err != nil {
			return "", err
		}
		return editor.ExportMarkdown(doc), nil
	}
	return relocateText(rel, text, urls)
}

func refreshBlockIDs(blocks []*model.Block) {
	for _, b := range blocks {
		b.ID = model.NewID()
		refreshBlockIDs(b.Children)
	}
}
