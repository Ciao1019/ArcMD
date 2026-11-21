package app

import (
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
	if vault.IsPDF(rel) {
		full, err := resolveFile(sp, rel)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(full)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("pdf:%d:%d", info.ModTime().UnixNano(), info.Size()), nil
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
	delete(a.pageTrees, sp.ID)
	a.saveState()
	a.refreshTree()
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
				{Name: i18n.T("支持的文档"), Extensions: []string{"md", "markdown", "mdx", "arcmd", "pdf", "svg", "excalidraw"}},
				{Name: "Markdown", Extensions: []string{"md", "markdown", "mdx", "arcmd"}},
				{Name: "PDF", Extensions: []string{"pdf"}},
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
