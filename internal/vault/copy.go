package vault

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Copy copies the file or folder at rel of the vault at root into the folder
// dir ("" for the root), choosing an unused numbered name ("Notes 2.md") when
// the name is taken, and returns its new path. A folder cannot be copied into
// itself or one of its subfolders.
func Copy(root, rel, dir string) (string, error) {
	return CopyTo(root, rel, root, dir)
}

// CopyTo copies the file or folder at rel of the vault at from into the folder
// dir ("" for the root) of the vault at to, choosing an unused numbered name
// when the name is taken, and returns its path there. When copying a folder,
// symbolic links, dot-prefixed entries and dependency folders (node_modules)
// are left out. A folder cannot be copied into itself or one of its subfolders.
func CopyTo(from, rel, to, dir string) (string, error) {
	src, err := Resolve(from, rel)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() && !fi.Mode().IsRegular() {
		return "", fmt.Errorf("vault: %s is not a file or folder", rel)
	}
	dstParent, err := resolveTargetDir(to, dir)
	if err != nil {
		return "", err
	}
	base := path.Base(rel)
	if fi.IsDir() {
		if sameOrInsideDir(src, dstParent) {
			return "", errors.New("vault: a folder can't copy into itself")
		}
		walkSrc := src
		if eval, evalErr := filepath.EvalSymlinks(src); evalErr == nil {
			walkSrc = eval
		}
		for i := 1; i < 1000; i++ {
			name := base
			if i > 1 {
				name = fmt.Sprintf("%s %d", base, i)
			}
			dest := path.Join(dir, name)
			dst, err := Resolve(to, dest)
			if err != nil {
				return "", err
			}
			err = os.Mkdir(dst, 0o755)
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			if err != nil {
				return "", err
			}
			if err := copyFolderContents(walkSrc, dst); err != nil {
				_ = os.RemoveAll(dst) // only what this call created
				return "", err
			}
			return dest, nil
		}
		return "", errors.New("vault: too many folders with that name")
	}

	return copyFileUnique(src, to, dir, base)
}

// ImportFile copies an external regular file at src into the folder dir
// ("" for the root) of the vault at root, choosing an unused numbered name
// when the name is taken, and returns its vault-relative path.
func ImportFile(src, root, dir string) (string, error) {
	fi, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("vault: %s is not a regular file", filepath.Base(src))
	}
	if _, err := resolveTargetDir(root, dir); err != nil {
		return "", err
	}
	return copyFileUnique(src, root, dir, filepath.Base(src))
}

func resolveTargetDir(root, dir string) (string, error) {
	dstParent := root
	if dir != "" {
		full, err := Resolve(root, dir)
		if err != nil {
			return "", err
		}
		dstParent = full
	}
	st, err := os.Stat(dstParent)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("vault: %s is not a folder", dir)
	}
	return dstParent, nil
}

func copyFileUnique(src, root, dir, base string) (string, error) {
	ext := FileExt(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i < 1000; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s %d%s", stem, i, ext)
		}
		dest := path.Join(dir, name)
		dst, err := Resolve(root, dest)
		if err != nil {
			return "", err
		}
		err = copyFileExclusive(src, dst)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		return dest, nil
	}
	return "", errors.New("vault: too many files with that name")
}

// sameOrInsideDir reports whether dstDir is src or lies inside src on disk,
// comparing canonical paths with os.SameFile so symlinked or case-folded
// aliases cannot cause a folder copy to recurse into itself.
func sameOrInsideDir(src, dstDir string) bool {
	s := src
	if eval, err := filepath.EvalSymlinks(s); err == nil {
		s = eval
	}
	d := dstDir
	if eval, err := filepath.EvalSymlinks(d); err == nil {
		d = eval
	}
	s, err1 := filepath.Abs(s)
	d, err2 := filepath.Abs(d)
	if err1 != nil || err2 != nil {
		return false
	}
	if si, err := os.Stat(s); err == nil {
		cur := d
		for {
			if di, err := os.Stat(cur); err == nil && os.SameFile(si, di) {
				return true
			}
			parent := filepath.Dir(cur)
			if parent == cur {
				return false
			}
			cur = parent
		}
	}
	rel, err := filepath.Rel(s, d)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

// copyFolderContents copies regular files and subfolders from src into dst,
// which already exists as a freshly created directory. Symbolic links, hidden
// entries and dependency directories are skipped.
func copyFolderContents(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == src {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if skipDir(name) {
				return filepath.SkipDir
			}
			rel, err := filepath.Rel(src, p)
			if err != nil {
				return err
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		return copyFileExclusive(p, filepath.Join(dst, rel))
	})
}

// copyFileExclusive copies src to a brand-new file at dst, returning fs.ErrExist
// without disturbing dst if a file is already there and removing partial writes
// on failure.
func copyFileExclusive(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}
