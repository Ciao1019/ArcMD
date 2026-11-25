package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
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
	dstParent, err := resolveTargetDir(to, dir)
	if err != nil {
		return "", false, err
	}
	if fi.IsDir() && sameOrInsideDir(src, dstParent) {
		return "", false, errors.New("vault: a folder can't move into itself")
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
		ext = FileExt(name)
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
	return walkCopy(src, dst, false)
}
