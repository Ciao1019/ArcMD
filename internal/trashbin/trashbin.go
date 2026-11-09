// Package trashbin keeps what ArcMD moved to the system's trash, so that
// it can be put back from ArcMD: the system's trash cannot be listed or
// restored from on every platform. Before a document or folder goes to the
// trash, a copy goes in the bin, with where it was; restoring writes the
// copy back, never over a file. Copies older than Keep are let go. The
// package has no dependency on the window.
package trashbin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

// Keep is how long the bin keeps a copy; MaxSize the largest file or
// folder it copies, beyond which only the system's trash has it.
const (
	Keep    = 30 * 24 * time.Hour
	MaxSize = 200 << 20
)

// Bin is the folder holding the copies, one folder each.
type Bin struct{ Dir string }

// Item is something deleted.
type Item struct {
	ID string `json:"-"`
	// Space is the ID of the space it was in, SpaceName its name then, and
	// Path where it was in the space's folder.
	Space     string    `json:"space"`
	SpaceName string    `json:"spaceName"`
	Path      string    `json:"path"`
	Dir       bool      `json:"dir,omitempty"`
	Deleted   time.Time `json:"deleted"`
}

// Name is the item's file or folder name.
func (it Item) Name() string { return path.Base(it.Path) }

// errTooLarge says a file or folder is too large to copy.
var errTooLarge = errors.New("trashbin: too large to keep")

// Add copies the file or folder at rel of the space's folder root into the
// bin, before it goes to the trash. A file or folder larger than MaxSize
// is not copied, and errTooLarge says so: the trash still has it.
func (b Bin) Add(space, spaceName, root, rel string, now time.Time) (Item, error) {
	src, err := vault.Resolve(root, rel)
	if err != nil {
		return Item{}, err
	}
	info, err := os.Stat(src)
	if err != nil {
		return Item{}, err
	}
	if size(src) > MaxSize {
		return Item{}, errTooLarge
	}
	it := Item{ID: document.NewID()[:16], Space: space, SpaceName: spaceName, Path: rel, Dir: info.IsDir(), Deleted: now}
	dir := filepath.Join(b.Dir, it.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Item{}, err
	}
	if err := copyAll(src, filepath.Join(dir, "item")); err != nil {
		os.RemoveAll(dir)
		return Item{}, err
	}
	meta, _ := json.MarshalIndent(it, "", "  ")
	if err := fsutil.WriteFileAtomic(filepath.Join(dir, "item.json"), meta, 0o644); err != nil {
		os.RemoveAll(dir)
		return Item{}, err
	}
	return it, nil
}

// size is the bytes of a file or folder, more than MaxSize once it passes
// it.
func size(p string) int64 {
	var total int64
	filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := d.Info(); err == nil && !d.IsDir() {
			total += info.Size()
		}
		if total > MaxSize {
			return filepath.SkipAll
		}
		return nil
	})
	return total
}

// copyAll copies a file, or a folder with what it holds, leaving out
// symbolic links.
func copyAll(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			return nil
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}

// List returns what the bin keeps, last deleted first, letting go of what
// is older than Keep.
func (b Bin) List(now time.Time) ([]Item, error) {
	entries, err := os.ReadDir(b.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Item
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(b.Dir, e.Name(), "item.json"))
		if err != nil {
			continue
		}
		var it Item
		if json.Unmarshal(data, &it) != nil {
			continue
		}
		it.ID = e.Name()
		if now.Sub(it.Deleted) > Keep {
			os.RemoveAll(filepath.Join(b.Dir, e.Name()))
			continue
		}
		out = append(out, it)
	}
	slices.SortFunc(out, func(x, y Item) int { return y.Deleted.Compare(x.Deleted) })
	return out, nil
}

func (b Bin) itemDir(it Item) (string, error) {
	if it.ID == "" || strings.ContainsAny(it.ID, `/\.`) {
		return "", errors.New("trashbin: invalid item")
	}
	return filepath.Join(b.Dir, it.ID), nil
}

// Restore writes the item back where it was in the space's folder root,
// numbered when the name is taken ("Plan 2.arcmd"), and lets the bin's
// copy go. It returns the path it was written to.
func (b Bin) Restore(it Item, root string) (string, error) {
	dir, err := b.itemDir(it)
	if err != nil {
		return "", err
	}
	if _, err := vault.Resolve(root, it.Path); err != nil {
		return "", err
	}
	parent := path.Dir(it.Path)
	if parent == "." {
		parent = ""
	}
	if parent != "" {
		full, _ := vault.Resolve(root, parent)
		if err := os.MkdirAll(full, 0o755); err != nil {
			return "", err
		}
	}
	name := path.Base(it.Path)
	ext := path.Ext(name)
	if it.Dir {
		ext = ""
	}
	stem := strings.TrimSuffix(name, ext)
	for n := 1; n < 1000; n++ {
		candidate := name
		if n > 1 {
			candidate = fmt.Sprintf("%s %d%s", stem, n, ext)
		}
		rel := path.Join(parent, candidate)
		target, err := vault.Resolve(root, rel)
		if err != nil {
			return "", err
		}
		if _, err := os.Lstat(target); err == nil {
			continue
		}
		if err := copyAll(filepath.Join(dir, "item"), target); err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			return "", err
		}
		os.RemoveAll(dir)
		return rel, nil
	}
	return "", errors.New("trashbin: no free name")
}

// Forget lets the bin's copy of the item go; the system's trash keeps the
// original.
func (b Bin) Forget(it Item) error {
	dir, err := b.itemDir(it)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// TooLarge reports whether Add declined to copy for the size.
func TooLarge(err error) bool { return errors.Is(err, errTooLarge) }
