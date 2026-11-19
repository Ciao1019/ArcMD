// Package fsutil holds small file system helpers shared by ArcMD's
// packages.
package fsutil

import (
	"errors"
	"os"
	"path/filepath"
)

// IsMissingFile reports whether err indicates that path (or a missing
// directory along path) does not exist, rather than a regular file blocking
// directory traversal (which Windows reports as ERROR_PATH_NOT_FOUND and
// matches os.ErrNotExist).
func IsMissingFile(path string, err error) bool {
	if !errors.Is(err, os.ErrNotExist) {
		return false
	}
	dir := filepath.Dir(path)
	for {
		info, statErr := os.Stat(dir)
		if statErr == nil {
			return info.IsDir()
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return true
		}
		dir = parent
	}
}

// WriteFileAtomic writes data to path through a temporary file in the same
// directory renamed over it, so that a crash never leaves half a file. It
// creates the directory when missing. When path is an existing regular file
// with additional hard links, it falls back to writing in place so that the
// links remain connected to the same underlying file.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	return writeAtomic(path, data, perm, false)
}

// CreateFileAtomic publishes a complete file only if its destination is free.
// Linking the temporary file prevents concurrent imports from overwriting one
// another after a filename availability check.
func CreateFileAtomic(path string, data []byte, perm os.FileMode) error {
	return writeAtomic(path, data, perm, true)
}

func writeAtomic(path string, data []byte, perm os.FileMode, exclusive bool) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if !exclusive {
		if fi, err := os.Lstat(path); err == nil {
			perm = fi.Mode().Perm()
			if fi.Mode().IsRegular() && hasHardLinks(path, fi) {
				return writeInPlace(path, data, perm, fi)
			}
		}
	} else if fi, err := os.Stat(path); err == nil {
		perm = fi.Mode().Perm()
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // a no-op once renamed
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, perm); err != nil {
		return err
	}
	if exclusive {
		return os.Link(tmp, path)
	}
	return os.Rename(tmp, path)
}

func writeInPlace(path string, data []byte, perm os.FileMode, target os.FileInfo) error {
	f, err := os.OpenFile(path, os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if openInfo, err := f.Stat(); err != nil || !openInfo.Mode().IsRegular() || !os.SameFile(target, openInfo) {
		f.Close()
		if err != nil {
			return err
		}
		return &os.PathError{Op: "write", Path: path, Err: os.ErrInvalid}
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Truncate(int64(len(data))); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
