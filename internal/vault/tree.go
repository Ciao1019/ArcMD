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
)

// Node is a folder or a Markdown file of the vault's tree.
type Node struct {
	Name string // the file or folder name
	Path string // relative to the root, slash-separated
	Dir  bool
	// Children of a folder: its folders first, then its files, each
	// sorted by name, case-insensitively.
	Children []*Node
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
	dir := root
	if rel != "" {
		dir = filepath.Join(root, filepath.FromSlash(rel))
	}
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
			if fi, err := os.Stat(filepath.Join(dir, name)); err == nil {
				isDir = fi.IsDir()
			}
		}
		switch {
		case isDir:
			if skipDir(name) {
				continue
			}
			kids, err := readDir(root, p)
			if err != nil {
				continue // unreadable folders are left out
			}
			dirs = append(dirs, &Node{Name: name, Path: p, Dir: true, Children: kids})
		case !strings.HasPrefix(name, ".") && IsDocument(name):
			files = append(files, &Node{Name: name, Path: p})
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
	// Renaming only the case of a name is allowed on case-insensitive disks.
	if _, err := os.Stat(dst); err == nil && !strings.EqualFold(from, to) {
		return ErrExists
	}
	return os.Rename(src, dst)
}
