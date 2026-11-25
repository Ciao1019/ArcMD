package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const headlessTrashCmdTimeout = 10 * time.Second

func runHeadlessTrashCmd(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), headlessTrashCmdTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// trashItemHeadless moves a file or directory into the operating system's
// Trash / Recycle Bin without requiring mygo's GUI event loop. It never
// permanently deletes user files.
func trashItemHeadless(target string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(abs); err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		return trashDarwinHeadless(abs)
	case "windows":
		return trashWindowsHeadless(abs)
	default:
		return trashFreedesktopHeadless(abs)
	}
}

func trashDarwinHeadless(abs string) error {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		trashDir := filepath.Join(home, ".Trash")
		if mkErr := os.MkdirAll(trashDir, 0o700); mkErr == nil {
			err = moveIntoDirUnique(abs, trashDir)
			if err == nil {
				return nil
			}
			if !errors.Is(err, syscall.EXDEV) {
				return err
			}
		}
	}
	// Cross-volume fallback: macOS 14+ ships /usr/bin/trash; older releases use Finder AppleScript.
	if trashBin, lookErr := exec.LookPath("trash"); lookErr == nil {
		if out, runErr := runHeadlessTrashCmd(trashBin, abs); runErr == nil {
			return nil
		} else {
			return fmt.Errorf("trash: %w (%s)", runErr, strings.TrimSpace(string(out)))
		}
	}
	escaped := strings.ReplaceAll(strings.ReplaceAll(abs, `\`, `\\`), `"`, `\"`)
	script := fmt.Sprintf(`tell application "Finder" to delete POSIX file "%s"`, escaped)
	if out, runErr := runHeadlessTrashCmd("osascript", "-e", script); runErr != nil {
		return fmt.Errorf("osascript trash: %w (%s)", runErr, strings.TrimSpace(string(out)))
	}
	return nil
}

func trashFreedesktopHeadless(abs string) error {
	dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	trashRoot := filepath.Join(dataHome, "Trash")
	filesDir := filepath.Join(trashRoot, "files")
	infoDir := filepath.Join(trashRoot, "info")
	if err := os.MkdirAll(filesDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(infoDir, 0o700); err != nil {
		return err
	}
	base := filepath.Base(abs)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	now := time.Now().Format("2006-01-02T15:04:05")
	escapedPath := (&url.URL{Path: filepath.ToSlash(abs)}).EscapedPath()

	for i := 1; i < 10000; i++ {
		cand := base
		if i > 1 {
			cand = fmt.Sprintf("%s %d%s", stem, i, ext)
		}
		infoPath := filepath.Join(infoDir, cand+".trashinfo")
		destPath := filepath.Join(filesDir, cand)
		if _, err := os.Lstat(destPath); err == nil {
			continue
		}
		f, err := os.OpenFile(infoPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		_, writeErr := fmt.Fprintf(f, "[Trash Info]\nPath=%s\nDeletionDate=%s\n", escapedPath, now)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			_ = os.Remove(infoPath)
			if writeErr != nil {
				return writeErr
			}
			return closeErr
		}
		if mvErr := os.Rename(abs, destPath); mvErr != nil {
			_ = os.Remove(infoPath)
			if errors.Is(mvErr, syscall.EXDEV) {
				if gio, lookErr := exec.LookPath("gio"); lookErr == nil {
					if out, runErr := runHeadlessTrashCmd(gio, "trash", abs); runErr == nil {
						return nil
					} else {
						return fmt.Errorf("gio trash: %w (%s)", runErr, strings.TrimSpace(string(out)))
					}
				}
			}
			return mvErr
		}
		return nil
	}
	return errors.New("trash: too many colliding names in Trash")
}

func moveIntoDirUnique(abs, destDir string) error {
	base := filepath.Base(abs)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i < 10000; i++ {
		cand := base
		if i > 1 {
			cand = fmt.Sprintf("%s %d%s", stem, i, ext)
		}
		dst := filepath.Join(destDir, cand)
		if _, err := os.Lstat(dst); err == nil {
			continue
		}
		return os.Rename(abs, dst)
	}
	return errors.New("trash: too many colliding names")
}
