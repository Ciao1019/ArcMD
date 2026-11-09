// Package vault reads and writes the document files of a space's folder.
// Files on disk are the source of truth: ArcMD keeps no copy of their
// content elsewhere, so any other editor, Git or a sync service can work on
// the same folder.
//
// Paths given to and returned by the package are slash-separated and
// relative to the folder (the root); Resolve refuses those that leave it.
package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/fsutil"
)

// Extensions are the file extensions the vault lists, lowercase.
var Extensions = []string{".md", ".markdown", ".mdx"}

// IsDocument includes native rich documents and Markdown import sources.
func IsDocument(name string) bool {
	return strings.EqualFold(path.Ext(name), ".arcmd") || IsMarkdown(name)
}

// IsMarkdown reports whether name has one of Extensions.
func IsMarkdown(name string) bool {
	return slices.Contains(Extensions, strings.ToLower(path.Ext(name)))
}

// Doc is a Markdown file of the vault.
type Doc struct {
	// Path is relative to the root, slash-separated, as "journal/today.md".
	Path string
}

// Dir returns the folder of the document, "" at the root.
func (d Doc) Dir() string {
	if dir := path.Dir(d.Path); dir != "." {
		return dir
	}
	return ""
}

// Title returns the document's name without its extension.
func (d Doc) Title() string { return Title(d.Path) }

// Title returns the name of the file at rel without its extension.
func Title(rel string) string {
	base := path.Base(rel)
	return strings.TrimSuffix(base, path.Ext(base))
}

// ErrOutside reports a path that leaves the root.
var ErrOutside = errors.New("vault: path is outside the space folder")

// Resolve returns the absolute path of rel under root.
func Resolve(root, rel string) (string, error) {
	rel = filepath.ToSlash(rel)
	if rel == "" || path.IsAbs(rel) || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", ErrOutside
	}
	for seg := range strings.SplitSeq(rel, "/") {
		if seg == ".." {
			return "", ErrOutside
		}
	}
	clean := path.Clean(rel)
	if clean == "." {
		return "", ErrOutside
	}
	return filepath.Join(root, filepath.FromSlash(clean)), nil
}

// skipDir reports whether the walk leaves out a directory: hidden ones (as
// .git and ArcMD's own .arcmd) and dependency folders.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules"
}

// List returns the document files under root, sorted by folder then name,
// case-insensitively, the files of a folder before its subfolders'.
func List(root string) ([]Doc, error) {
	var docs []Doc
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil // unreadable entries are left out, not fatal
		}
		if d.IsDir() {
			if p != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || !IsDocument(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		docs = append(docs, Doc{Path: filepath.ToSlash(rel)})
		return nil
	})
	slices.SortFunc(docs, func(a, b Doc) int {
		if c := strings.Compare(strings.ToLower(a.Dir()), strings.ToLower(b.Dir())); c != 0 {
			return c
		}
		return strings.Compare(strings.ToLower(a.Path), strings.ToLower(b.Path))
	})
	return docs, err
}

// CreateFile atomically chooses an unused document name and writes its content.
func CreateFile(root, dir, title, extension, text string) (string, error) {
	if extension != ".arcmd" && extension != ".md" {
		return "", errors.New("vault: unsupported document extension")
	}
	title = sanitize(title)
	if title == "" {
		title = "Untitled"
	}
	for i := 1; i < 1000; i++ {
		name := title
		if i > 1 {
			name = fmt.Sprintf("%s %d", title, i)
		}
		rel := path.Join(dir, name+extension)
		full, err := Resolve(root, rel)
		if err != nil {
			return "", err
		}
		err = fsutil.CreateFileAtomic(full, []byte(text), 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return rel, err
	}
	return "", errors.New("vault: too many documents with that name")
}

// Read returns the text of the document at rel.
func Read(root, rel string) (string, error) {
	p, err := Resolve(root, rel)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	return string(b), err
}

// Write replaces the document at rel with text, atomically, creating its
// folders.
func Write(root, rel, text string) error {
	p, err := Resolve(root, rel)
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(p, []byte(text), 0o644)
}

// Create makes a new document named after title in the folder dir (""
// for the root), adding " 2", " 3"… when the name is taken, and returns
// its path. The file starts with title as a heading.
func Create(root, dir, title string) (string, error) {
	title = sanitize(title)
	if title == "" {
		title = "Untitled"
	}
	for i := 1; i < 1000; i++ {
		name := title
		if i > 1 {
			name = fmt.Sprintf("%s %d", title, i)
		}
		rel := path.Join(dir, name+".md")
		p, err := Resolve(root, rel)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", err
		}
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = fmt.Fprintf(f, "# %s\n\n", name)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return rel, err
	}
	return "", fmt.Errorf("vault: too many documents named %q", title)
}

// sanitize makes title a file name that every platform accepts.
func sanitize(title string) string {
	title = strings.Map(func(r rune) rune {
		switch {
		case r < 32 || strings.ContainsRune(`/\:*?"<>|`, r):
			return '-'
		}
		return r
	}, title)
	return strings.Trim(strings.TrimSpace(title), ".")
}
