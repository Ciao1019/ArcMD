package vault

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// Tree is flat: opening a document does not import its siblings or folders.
func (f Files) Tree() []*Node {
	var nodes []*Node
	for _, d := range f.List() {
		n := &Node{Name: d.Path, Path: d.Path}
		if info, err := os.Stat(f[d.Path]); err == nil {
			n.Modified = info.ModTime()
		}
		nodes = append(nodes, n)
	}
	return nodes
}
