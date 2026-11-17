package history

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/fsutil"
)

// CopyTo copies a file or folder's history across spaces without deleting the
// source. The caller removes it only after the document move has committed.
// Existing snapshots are preserved; timestamp collisions receive a new stamp.
// Milestone labels in labels.json are remapped to follow their snapshots.
// rewrite receives the original document path, including for nested folders.
func CopyTo(fromRoot, from, toRoot, to string, rewrite func(string, string) (string, error)) (err error) {
	src, err := folder(fromRoot, from)
	if err != nil {
		return err
	}
	dst, err := folder(toRoot, to)
	if err != nil {
		return err
	}
	if src == dst {
		return nil
	}
	if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	var created []string
	defer func() {
		if err != nil {
			for _, p := range created {
				_ = os.Remove(p)
			}
		}
	}()
	labelDirs := map[string]bool{}
	remap := map[string]map[string]string{}
	recordRemap := func(relDir, origBase, destBase string) {
		if origBase == destBase {
			return
		}
		m := remap[relDir]
		if m == nil {
			m = map[string]string{}
			remap[relDir] = m
		}
		m[origBase] = destBase
	}
	if walkErr := filepath.WalkDir(src, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return errors.New("history: symbolic link in snapshots")
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		relDir := filepath.Dir(rel)
		if entry.Name() == labelsFile {
			labelDirs[relDir] = true
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if rewrite != nil {
			name := filepath.ToSlash(filepath.Join(from, relDir))
			text, err := rewrite(name, string(data))
			if err != nil {
				return err
			}
			data = []byte(text)
		}
		origBase := filepath.Base(rel)
		target := filepath.Join(dst, rel)
		for {
			err = fsutil.CreateFileAtomic(target, data, 0o644)
			if err == nil {
				created = append(created, target)
				recordRemap(relDir, origBase, filepath.Base(target))
				return nil
			}
			if !errors.Is(err, fs.ErrExist) {
				return err
			}
			old, err := os.ReadFile(target)
			if err != nil {
				return err
			}
			if bytes.Equal(old, data) {
				recordRemap(relDir, origBase, filepath.Base(target))
				return nil
			}
			ext := filepath.Ext(target)
			if ext != ".md" && ext != ".arcmd" {
				ext = ".md"
			}
			base := strings.TrimSuffix(filepath.Base(target), ext)
			kept := strings.HasSuffix(base, "~")
			at, err := time.ParseInLocation(stamp, strings.TrimSuffix(base, "~"), time.Local)
			if err != nil {
				return err
			}
			name := at.Add(time.Millisecond).Format(stamp)
			if kept {
				name += "~"
			}
			target = filepath.Join(filepath.Dir(target), name+ext)
		}
	}); walkErr != nil {
		return walkErr
	}

	dirs := make([]string, 0, len(labelDirs))
	for d := range labelDirs {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, relDir := range dirs {
		srcLabels := loadLabelsFromDir(filepath.Join(src, relDir))
		if len(srcLabels) == 0 {
			continue
		}
		dstDir := filepath.Join(dst, relDir)
		dstLabels := loadLabelsFromDir(dstDir)
		if dstLabels == nil {
			dstLabels = map[string]string{}
		}
		for oldName, lbl := range srcLabels {
			if lbl == "" {
				continue
			}
			destName := oldName
			if mapped, ok := remap[relDir][oldName]; ok {
				destName = mapped
			}
			if _, statErr := os.Stat(filepath.Join(dstDir, destName)); statErr == nil {
				if dstLabels[destName] == "" {
					dstLabels[destName] = lbl
				}
			}
		}
		if len(dstLabels) > 0 {
			if err := saveLabelsToDir(dstDir, dstLabels); err != nil {
				return err
			}
		}
	}
	return nil
}

// Forget removes only ArcMD's managed snapshots after a successful transfer.
func Forget(root, rel string) error {
	dir, err := folder(root, rel)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}
