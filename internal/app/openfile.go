package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

// openSystemFile handles file associations and command-line documents. The
// deepest existing space wins. An external file's folder becomes a space;
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
		root := filepath.Dir(full)
		name := filepath.Base(root)
		if name == string(filepath.Separator) || name == "." {
			name = "文档"
		}
		target = a.state.Add(name, root)
		rel = filepath.Base(full)
		a.saveState()
		a.notify("已将文件夹「%s」添加为空间。", name)
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
