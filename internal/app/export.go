package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

// PDF export and printing: the document is laid out on A4 paper by the
// editor (Paginate), each page is drawn without a window (ui.Render) on
// white paper, and the pages go into a PDF (package pdf). Printing opens
// that PDF in the system's viewer, where it prints.

// Paper in DIPs, 96 to the inch: A4, with a margin above and below the
// page's text (the editor keeps its own at the sides).
const (
	paperWidth  = 794
	paperHeight = 1123
	paperMargin = 48
	paperScale  = 2
)

// createNumbered writes data as a new file named title and ext in the
// folder dir of the space's folder root, numbered when the name is taken,
// and returns its path. It never replaces a file.
func createNumbered(root, dir, title, ext string, data []byte) (string, error) {
	if dir == "." {
		dir = ""
	}
	stem := sanitizeName(title)
	for i := 1; i < 1000; i++ {
		name := stem + ext
		if i > 1 {
			name = fmt.Sprintf("%s %d%s", stem, i, ext)
		}
		rel := path.Join(dir, name)
		full, err := vault.Resolve(root, rel)
		if err != nil {
			return "", err
		}
		if _, err := os.Lstat(full); err == nil {
			continue
		}
		if err := fsutil.CreateFileAtomic(full, data, 0o644); err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			return "", err
		}
		return rel, nil
	}
	return "", errors.New("no free name")
}

// sanitizeName makes a title a file name.
func sanitizeName(title string) string {
	name := strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(title))
	if name == "" || name == "." || name == ".." {
		return "文档"
	}
	return name
}
