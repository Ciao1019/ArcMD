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

// MoveTo moves the file or folder at rel of the vault at from into the
// folder dir ("" for the root) of the vault at to, as a document goes to
// another space, and returns its path there. A name taken there is
// numbered, as new documents are ("Notes 2.arcmd"). Where the two vaults
// are on different disks, it copies instead and reports copied: the
// original stays for the caller to move to the trash, as ArcMD never
// deletes a user's files itself.
func MoveTo(from, rel, to, dir string) (dest string, copied bool, err error) {
	src, err := Resolve(from, rel)
	if err != nil {
		return "", false, err
	}
	fi, err := os.Lstat(src)
	if err != nil {
		return "", false, err
	}
	if dir != "" {
		full, err := Resolve(to, dir)
		if err != nil {
			return "", false, err
		}
		if st, err := os.Stat(full); err != nil || !st.IsDir() {
			return "", false, fmt.Errorf("vault: %s is not a folder", dir)
		}
	}
	if same, _ := filepath.Abs(from); same != "" {
		if other, _ := filepath.Abs(to); filepath.Clean(other) == filepath.Clean(same) {
			if dir == rel || strings.HasPrefix(dir+"/", rel+"/") {
				return "", false, errors.New("vault: a folder can't move into itself")
			}
		}
	}
	dest, dst, err := freeName(to, dir, path.Base(rel), fi.IsDir())
	if err != nil {
		return "", false, err
	}
	if err := os.Rename(src, dst); err == nil {
		return dest, false, nil
	} else if !crossDevice(err) {
		return "", false, err
	}
	if err := copyTree(src, dst); err != nil {
		os.RemoveAll(dst) // only what this call made
		return "", false, err
	}
	return dest, true, nil
}

// freeName is the first unused path for name in the folder dir of the vault
// at root: name itself, then "name 2", "name 3"… before its extension.
func freeName(root, dir, name string, isDir bool) (string, string, error) {
	ext := ""
	if !isDir {
		ext = path.Ext(name)
	}
	stem := strings.TrimSuffix(name, ext)
	for i := 1; i < 1000; i++ {
		n := name
		if i > 1 {
			n = fmt.Sprintf("%s %d%s", stem, i, ext)
		}
		rel := path.Join(dir, n)
		full, err := Resolve(root, rel)
		if err != nil {
			return "", "", err
		}
		if _, err := os.Lstat(full); errors.Is(err, fs.ErrNotExist) {
			return rel, full, nil
		} else if err != nil {
			return "", "", err
		}
	}
	return "", "", errors.New("vault: too many files with that name")
}

// crossDevice reports whether a rename failed only because its two paths
// are on different disks.
func crossDevice(err error) bool {
	var le *os.LinkError
	if !errors.As(err, &le) {
		return false
	}
	return isCrossDevice(le.Err)
}

// copyTree copies the file or folder src to dst, which does not exist.
// Symbolic links are skipped rather than followed out of the vault.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			return nil
		case d.IsDir():
			return os.MkdirAll(out, 0o755)
		}
		return copyFile(p, out)
	})
}

func copyFile(src, dst string) error {
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
		out.Close()
		return err
	}
	return out.Close()
}
