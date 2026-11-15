package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

// openSystemFile handles file associations and command-line documents. The
// deepest existing folder space wins. External files share one collection;
// opening a Markdown file never converts it or copies the user's document.
func (a *App) openSystemFile(file string) bool {
	full, err := filepath.Abs(file)
	if err == nil {
		full, err = filepath.EvalSymlinks(full)
	}
	if err != nil {
		a.notify("无法打开文件。%v", err)
		return false
	}
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() || !vault.IsDocument(full) {
		a.notify("请选择 ArcMD 或 Markdown 文档：%s", filepath.Base(full))
		return false
	}
	if !a.flush() {
		return false
	}
	var target *space.Space
	var rel string
	depth := -1
	for _, sp := range a.state.Spaces {
		if sp.IsExternal() {
			continue
		}
		root, err := filepath.EvalSymlinks(sp.Root)
		if err != nil {
			continue
		}
		r, err := filepath.Rel(root, full)
		if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || filepath.IsAbs(r) {
			continue
		}
		if len(root) > depth {
			target, rel, depth = sp, filepath.ToSlash(r), len(root)
		}
	}
	if target == nil {
		target, err = a.externalSpace()
		if err != nil {
			a.notify("无法打开文件。%v", err)
			return false
		}
		rel = target.AddExternal(full)
		delete(a.pageTrees, target.ID)
		if a.state.ActiveSpace() == target {
			a.refreshTree()
		}
		a.saveState()
	}
	if !a.openAnywhere(target.ID, rel) {
		return false
	}
	a.focus = false
	if a.library.open {
		a.library.open, a.library.frames = false, 0
	}
	return true
}

// documentArguments accepts arguments without argv[0], as MyGo forwards them.
func documentArguments(args []string, workingDir string) []string {
	var files []string
	for _, file := range args {
		if !vault.IsDocument(file) {
			continue
		}
		if !filepath.IsAbs(file) {
			file = filepath.Join(workingDir, file)
		}
		files = append(files, file)
	}
	return files
}
