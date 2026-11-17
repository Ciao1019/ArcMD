package prefs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/fsutil"
)

// Secrets keeps credentials in separate, unencrypted local files. Dir belongs
// in the app's configuration folder, outside the document spaces. On Unix the
// directory and files are accessible only to their owner (0700 and 0600).
// No operation accesses the system keychain, including when a key is missing.
type Secrets struct{ Dir string }

func (s Secrets) file(key string) (string, error) {
	if s.Dir == "" || !filepath.IsLocal(key) || key == "." || strings.ContainsAny(key, `/\`) {
		return "", errors.New("invalid credential path")
	}
	return filepath.Join(s.Dir, key), nil
}

// Get reads a credential; a missing key returns os.ErrNotExist.
func (s Secrets) Get(key string) ([]byte, error) {
	file, err := s.file(key)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(file)
}

// Set atomically replaces a credential, keeping restrictive permissions even
// if a previous file or directory was made more widely readable.
func (s Secrets) Set(key string, value []byte) error {
	file, err := s.file(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.Dir, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(file); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("credential is not a regular file: %s", file)
		}
		// WriteFileAtomic preserves an existing file's mode, so restrict it
		// before writing the replacement (whose temporary file is also 0600).
		if err := os.Chmod(file, 0o600); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return fsutil.WriteFileAtomic(file, value, 0o600)
}

// Delete removes only this local credential. Removing a missing key succeeds.
func (s Secrets) Delete(key string) error {
	file, err := s.file(key)
	if err != nil {
		return err
	}
	if err := os.Remove(file); err != nil && !fsutil.IsMissingFile(file, err) {
		return err
	}
	return nil
}
