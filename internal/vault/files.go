package vault

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/fsutil"
)

// Files is an explicit collection of local names mapped to absolute files.
// It never traverses their parent directories. Callers own the map and must
// copy it before handing it to a worker.
type Files map[string]string

// Resolve accepts only registered document names. Ordinary vault.Resolve
// retains its folder boundary; external files do not weaken that boundary.
func (f Files) Resolve(rel string) (string, error) {
	full, ok := f[rel]
	if !ok || !filepath.IsLocal(rel) || strings.ContainsAny(rel, "/\\") || !IsDocument(rel) || !filepath.IsAbs(full) || !IsDocument(full) {
		return "", ErrOutside
	}
	return filepath.Clean(full), nil
}

// List returns registered documents in stable name order, including missing
// files so the user can remove their entries or reconnect their storage.
func (f Files) List() []Doc {
	var docs []Doc
	for rel := range f {
		if _, err := f.Resolve(rel); err == nil {
			docs = append(docs, Doc{Path: rel})
		}
	}
	slices.SortFunc(docs, func(a, b Doc) int { return strings.Compare(a.Path, b.Path) })
	return docs
}

// Missing reports whether the registered file at rel is missing on disk or
// has been replaced by a directory. Permission and other non-existence errors
// return false so temporarily unreadable files are not treated as missing.
func (f Files) Missing(rel string) bool {
	full, err := f.Resolve(rel)
	if err != nil {
		return false
	}
	return isMissingExternalPath(full)
}

func isMissingExternalPath(full string) bool {
	info, err := os.Stat(full)
	if err == nil {
		return info.IsDir()
	}
	return fsutil.IsMissingFile(full, err)
}

// Tree is flat: opening a document does not import its siblings or folders.
func (f Files) Tree() []*Node {
	var nodes []*Node
	for _, d := range f.List() {
		n := &Node{Name: d.Path, Path: d.Path}
		full := f[d.Path]
		if info, err := os.Stat(full); err == nil {
			if info.IsDir() {
				n.Missing = true
			} else {
				n.Modified = info.ModTime()
			}
		} else if fsutil.IsMissingFile(full, err) {
			n.Missing = true
		}
		nodes = append(nodes, n)
	}
	return nodes
}
