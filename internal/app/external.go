package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Ciao1019/ArcMD/internal/history"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
)

// fileLocation separates a document's list identity from its disk location.
// Every ordinary space keeps the vault boundary; external entries must be
// explicitly registered and are resolved relative to their actual parent.
func fileLocation(sp *space.Space, rel string) (root, name string, err error) {
	if !sp.IsExternal() {
		return sp.Root, rel, nil
	}
	full, err := vault.Files(sp.ExternalFiles).Resolve(rel)
	if err != nil {
		return "", "", err
	}
	return filepath.Dir(full), filepath.Base(full), nil
}

func resolveFile(sp *space.Space, rel string) (string, error) {
	root, name, err := fileLocation(sp, rel)
	if err != nil {
		return "", err
	}
	return vault.Resolve(root, name)
}

func readFile(sp *space.Space, rel string) (string, error) {
	root, name, err := fileLocation(sp, rel)
	if err != nil {
		return "", err
	}
	return vault.Read(root, name)
}

// readDocSource reads text for text-based documents, or returns a lightweight
// stat stamp for read-only binary PDFs so opening, pinning, and watching PDFs
// never copy multi-megabyte files into Go strings.
func readDocSource(sp *space.Space, rel string) (string, error) {
	if vault.IsPDF(rel) || vault.IsBook(rel) || vault.IsMDZ(rel) {
		full, err := resolveFile(sp, rel)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(full)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", &os.PathError{Op: "open", Path: full, Err: os.ErrNotExist}
		}
		prefix := "pdf"
		if vault.IsBook(rel) {
			prefix = "book"
		} else if vault.IsMDZ(rel) {
			prefix = "mdz"
		}
		return fmt.Sprintf("%s:%d:%d", prefix, info.ModTime().UnixNano(), info.Size()), nil
	}
	return readFile(sp, rel)
}

func writeFile(sp *space.Space, rel, text string) error {
	root, name, err := fileLocation(sp, rel)
	if err != nil {
		return err
	}
	return vault.Write(root, name, text)
}

func renameFile(sp *space.Space, rel, name string) (string, error) {
	root, disk, err := fileLocation(sp, rel)
	if err != nil {
		return "", err
	}
	to, err := vault.Rename(root, disk, name)
	if err == nil && sp.IsExternal() {
		to = sp.AddExternal(filepath.Join(root, to))
	}
	return to, err
}

func (d *document) localImage(url string) (string, error) {
	return resolveSpaceImage(d.space, d.rel, url)
}

// forgetExternal removes only the reference. Saving and conflict handling are
// identical to leaving a normal document; no user's file goes to the trash.
func (a *App) forgetExternal(sp *space.Space, rel string) {
	if !sp.IsExternal() {
		return
	}
	doc := a.residentDocument(sp.ID, rel)
	if !a.flush() {
		return
	}
	if doc != nil {
		rel = doc.rel // the first save may have migrated Markdown
	}
	for _, d := range a.residentDocuments() {
		if d.space == sp && d.rel == rel {
			a.replaceReferences(d, nil)
		}
	}
	a.forgetCopied(sp.ID, rel)
	a.state.Forget(sp.ID, rel)
	delete(sp.ExternalFiles, rel)
	_ = history.Forget(sp.Root, rel)
	a.saveState()
	a.refreshExternalTree(sp)
	a.refreshSearch()
}

func (a *App) chooseExternalFiles() {
	if a.win == nil {
		return
	}
	win := a.win
	go func() {
		files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: i18n.T("打开文件…"), Multiple: true,
			Filters: []mygo.FileFilter{
				{Name: i18n.T("支持的文档"), Extensions: []string{"md", "markdown", "mdx", "arcmd", "mdz", "pdf", "epub", "html", "htm", "svg", "excalidraw"}},
				{Name: "Markdown", Extensions: []string{"md", "markdown", "mdx", "arcmd"}},
				{Name: "MDZip", Extensions: []string{"mdz"}},
				{Name: "PDF", Extensions: []string{"pdf"}},
				{Name: "EPUB / HTML", Extensions: []string{"epub", "html", "htm"}},
			}})
		if err != nil {
			return
		}
		win.Update(func() {
			for _, file := range files {
				a.openSystemFile(file)
			}
		})
	}()
}

func (a *App) externalSpace() (*space.Space, error) {
	root := filepath.Join(a.cfg.ConfigDir, "external")
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	return a.state.EnsureExternal(i18n.T("外部空间"), root), nil
}

func countMissingNodes(nodes []*vault.Node) int {
	n := 0
	for _, node := range nodes {
		if !node.Dir && node.Missing {
			n++
		}
	}
	return n
}

func (a *App) noteMissingExternal(sp *space.Space, rel string, err error) bool {
	if sp == nil || !sp.IsExternal() {
		return false
	}
	if !errors.Is(err, os.ErrNotExist) && !vault.Files(sp.ExternalFiles).Missing(rel) {
		return false
	}
	a.refreshExternalTree(sp)
	a.notify("文件已被移动或删除：%s", sp.ExternalFiles[rel])
	return true
}

func (a *App) cachedMissingExternal(sp *space.Space) int {
	if sp == nil || !sp.IsExternal() {
		return 0
	}
	if a.library.open && a.library.page == libSpaces {
		if nodes, ok := a.library.trees[sp.ID]; ok {
			return countMissingNodes(nodes)
		}
	}
	if sp == a.state.ActiveSpace() {
		return countMissingNodes(a.tree)
	}
	if pt, ok := a.pageTrees[sp.ID]; ok {
		return countMissingNodes(pt.nodes)
	}
	return 0
}

func (a *App) externalCleanMenuLabel(sp *space.Space) string {
	if n := a.cachedMissingExternal(sp); n > 0 {
		return fmt.Sprintf(i18n.T("清理不存在的文件 (%d)"), n)
	}
	return i18n.T("清理不存在的文件")
}

func (a *App) refreshExternalTree(sp *space.Space) {
	if sp == nil || !sp.IsExternal() {
		return
	}
	delete(a.pageTrees, sp.ID)
	if sp == a.state.ActiveSpace() {
		a.refreshTree()
	}
	if a.library.open && a.library.page == libSpaces {
		if a.library.trees == nil {
			a.library.trees = map[string][]*vault.Node{}
		}
		if a.library.errs == nil {
			a.library.errs = map[string]error{}
		}
		if sp.ID == a.treeFor {
			a.library.trees[sp.ID], a.library.errs[sp.ID] = a.tree, a.treeErr
		} else {
			a.library.trees[sp.ID], a.library.errs[sp.ID] = readTree(sp)
		}
	} else if a.library.trees != nil {
		delete(a.library.trees, sp.ID)
	}
}

// cleanMissingExternal removes references in the built-in external space whose
// target files no longer exist on disk. It never touches disk files and skips
// any open document that still holds unsaved edits in memory.
func (a *App) cleanMissingExternal(sp *space.Space) (removed, skipped int) {
	if sp == nil || !sp.IsExternal() {
		return 0, 0
	}
	files := vault.Files(sp.ExternalFiles)
	var missing []string
	for _, d := range files.List() {
		if files.Missing(d.Path) {
			missing = append(missing, d.Path)
		}
	}
	if len(missing) == 0 {
		a.refreshExternalTree(sp)
		a.notify("外部空间中没有不存在的文件。")
		return 0, 0
	}

	a.dismissPreview()
	resident := a.residentDocuments()
	for _, rel := range missing {
		var docs []*document
		busy := false
		for _, d := range resident {
			if d.space == sp && d.rel == rel {
				d.pollAsyncSave()
				if d.saving {
					busy = true
					continue
				}
				if d.dirty() {
					d.missing, d.conflict = true, false
					busy = true
					continue
				}
				docs = append(docs, d)
			}
		}
		if busy {
			skipped++
			continue
		}
		for _, d := range docs {
			a.replaceReferences(d, nil)
		}
		if a.renameSpace == sp.ID && a.renaming == rel {
			a.renaming = ""
		}
		a.forgetPicks(sp.ID, rel)
		a.forgetCopied(sp.ID, rel)
		a.state.Forget(sp.ID, rel)
		delete(sp.ExternalFiles, rel)
		_ = history.Forget(sp.Root, rel)
		removed++
	}

	if removed > 0 {
		a.invalidateConflictsCache()
		a.saveState()
		a.refreshExternalTree(sp)
		a.refreshSearch()
	} else {
		a.refreshExternalTree(sp)
	}

	switch {
	case removed == 0 && skipped == 1:
		a.notify("1 个不存在的文件有未保存修改，已跳过。")
	case removed == 0 && skipped > 1:
		a.notify("%d 个不存在的文件有未保存修改，已跳过。", skipped)
	case removed == 1 && skipped == 0:
		a.notify("已清理外部空间中 1 个不存在的文件。")
	case removed > 1 && skipped == 0:
		a.notify("已清理外部空间中 %d 个不存在的文件。", removed)
	default:
		a.notify("已清理外部空间中 %d 个不存在的文件，另有 %d 个因存在未保存修改已跳过。", removed, skipped)
	}
	return removed, skipped
}
