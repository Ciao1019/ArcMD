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
	"time"
)

// Node is a folder or a Markdown file of the vault's tree.
type Node struct {
	Name string // the file or folder name
	Path string // relative to the root, slash-separated
	Dir  bool
	// Children of a folder: its folders first, then its files, each
	// sorted by name, case-insensitively.
	Children []*Node
	// Modified is when the file last changed; for a folder, the latest of
	// what it holds, or the folder's own time when empty.
	Modified time.Time
}

// Title returns a file's name without its extension, or a folder's name.
func (n *Node) Title() string {
	if n.Dir {
		return n.Name
	}
	return Title(n.Name)
}

// Tree reads the folders and document files under root, leaving out what
// List leaves out; empty folders are kept, as the user made them.
func Tree(root string) ([]*Node, error) {
	return readDir(root, "")
}

func readDir(root, rel string) ([]*Node, error) {
	return readBranch(root, rel, nil)
}

func readBranch(root, rel string, ancestors []os.FileInfo) ([]*Node, error) {
	dir := root
	if rel != "" {
		var err error
		dir, err = Resolve(root, rel)
		if err != nil {
			return nil, err
		}
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	for _, parent := range ancestors {
		if os.SameFile(info, parent) {
			return nil, errors.New("vault: symbolic link cycle")
		}
	}
	ancestors = append(ancestors, info)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var dirs, files []*Node
	for _, e := range entries {
		name := e.Name()
		p := path.Join(rel, name)
		isDir := e.IsDir()
		if e.Type()&fs.ModeSymlink != 0 {
			if _, err := Resolve(root, p); err != nil {
				continue
			}
			if fi, err := os.Stat(filepath.Join(dir, name)); err == nil {
				isDir = fi.IsDir()
			}
		}
		switch {
		case isDir:
			if skipDir(name) {
				continue
			}
			kids, err := readBranch(root, p, ancestors)
			if err != nil {
				continue // unreadable folders are left out
			}
			n := &Node{Name: name, Path: p, Dir: true, Children: kids}
			if fi, err := e.Info(); err == nil {
				n.Modified = fi.ModTime()
			}
			for _, k := range kids {
				if k.Modified.After(n.Modified) || n.Modified.IsZero() {
					n.Modified = k.Modified
				}
			}
			dirs = append(dirs, n)
		case !strings.HasPrefix(name, ".") && IsDocument(name):
			n := &Node{Name: name, Path: p}
			if fi, err := os.Stat(filepath.Join(dir, name)); err == nil {
				n.Modified = fi.ModTime()
			}
			files = append(files, n)
		}
	}
	byName := func(a, b *Node) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) }
	slices.SortFunc(dirs, byName)
	slices.SortFunc(files, byName)
	return append(dirs, files...), nil
}

// Find returns the node at rel in a tree, or nil.
func Find(nodes []*Node, rel string) *Node {
	for _, n := range nodes {
		if n.Path == rel {
			return n
		}
		if n.Dir && strings.HasPrefix(rel, n.Path+"/") {
			return Find(n.Children, rel)
		}
	}
	return nil
}

// Mkdir makes a folder named name in dir ("" for the root), adding " 2",
// " 3"… when the name is taken, and returns its path.
func Mkdir(root, dir, name string) (string, error) {
	name = sanitize(name)
	if name == "" {
		name = "New Folder"
	}
	for i := 1; i < 1000; i++ {
		n := name
		if i > 1 {
			n = fmt.Sprintf("%s %d", name, i)
		}
		rel := path.Join(dir, n)
		p, err := Resolve(root, rel)
		if err != nil {
			return "", err
		}
		err = os.Mkdir(p, 0o755)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return rel, err
	}
	return "", fmt.Errorf("vault: too many folders named %q", name)
}

// ErrExists reports a rename or move onto a name that is taken.
var ErrExists = errors.New("vault: a file or folder with that name already exists")

// Rename gives the file or folder at rel a new name in the same folder; a
// file keeps its extension when the new name has none of Markdown's. It
// returns the new path.
func Rename(root, rel, name string) (string, error) {
	name = sanitize(name)
	if name == "" {
		return "", errors.New("vault: the name is empty")
	}
	src, err := Resolve(root, rel)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() && !(IsMarkdown(rel) && IsMarkdown(name)) && !strings.EqualFold(path.Ext(name), path.Ext(rel)) {
		name += path.Ext(rel)
	}
	to := path.Join(path.Dir(rel), name)
	if to == rel {
		return rel, nil
	}
	return to, move(root, rel, to)
}

// Move moves the file or folder at rel into the folder dir ("" for the
// root) and returns its new path.
func Move(root, rel, dir string) (string, error) {
	if dir == rel || strings.HasPrefix(dir+"/", rel+"/") {
		return "", errors.New("vault: a folder can't move into itself")
	}
	to := path.Join(dir, path.Base(rel))
	if to == rel {
		return rel, nil
	}
	return to, move(root, rel, to)
}

func move(root, from, to string) error {
	src, err := Resolve(root, from)
	if err != nil {
		return err
	}
	dst, err := Resolve(root, to)
	if err != nil {
		return err
	}
	// A case-only rename may refer to the same file on a case-insensitive
	// disk. Equal names alone do not establish that on other file systems.
	if target, err := os.Lstat(dst); err == nil {
		source, err := os.Lstat(src)
		if err != nil {
			return err
		}
		if !strings.EqualFold(from, to) || !os.SameFile(source, target) {
			return ErrExists
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.Rename(src, dst)
}
