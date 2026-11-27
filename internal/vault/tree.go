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
	// Missing reports that an explicitly registered file (such as an entry in
	// the built-in external space) is no longer present on disk.
	Missing bool
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
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	anc := &treeAncestors{
		paths: []string{root},
		infos: []os.FileInfo{info},
	}
	return readBranch(root, root, "", anc)
}

type treeAncestors struct {
	paths []string
	infos []os.FileInfo
}

func (a *treeAncestors) hasCycle(info os.FileInfo) bool {
	for i, p := range a.paths {
		anc := a.infos[i]
		if anc == nil {
			fi, err := os.Stat(p)
			if err != nil {
				continue
			}
			anc = fi
			a.infos[i] = fi
		}
		if os.SameFile(info, anc) {
			return true
		}
	}
	return false
}

func (a *treeAncestors) push(dir string, info os.FileInfo) {
	a.paths = append(a.paths, dir)
	a.infos = append(a.infos, info)
}

func (a *treeAncestors) pop() {
	n := len(a.paths) - 1
	a.paths = a.paths[:n]
	a.infos = a.infos[:n]
}

func readBranch(root, dir, rel string, anc *treeAncestors) ([]*Node, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var dirs, files []*Node
	for _, e := range entries {
		name := e.Name()
		p := path.Join(rel, name)
		isSymlink := e.Type()&fs.ModeSymlink != 0
		isDir := e.IsDir()
		var linkInfo os.FileInfo
		if isSymlink {
			if _, err := Resolve(root, p); err != nil {
				continue
			}
			fi, err := os.Stat(filepath.Join(dir, name))
			if err == nil {
				linkInfo = fi
				isDir = fi.IsDir()
			}
		}
		switch {
		case isDir:
			if skipDir(name) || name == "assets" {
				continue
			}
			childDir := filepath.Join(dir, name)
			if isSymlink {
				if linkInfo == nil || anc.hasCycle(linkInfo) {
					continue
				}
			}
			anc.push(childDir, linkInfo)
			kids, err := readBranch(root, childDir, p, anc)
			anc.pop()
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
			if isSymlink {
				if linkInfo != nil {
					n.Modified = linkInfo.ModTime()
				}
			} else if fi, err := e.Info(); err == nil {
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
	if !fi.IsDir() && !(IsMarkdown(rel) && IsMarkdown(name)) && !strings.EqualFold(FileExt(name), FileExt(rel)) {
		name += FileExt(rel)
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
