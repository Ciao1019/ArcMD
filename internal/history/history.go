// Package history keeps earlier versions of a space's documents, so that
// what was written can be seen and brought back: each document's versions
// are files in the space's hidden .arcmd/history folder, at the document's
// own path, named by the time they were taken. The package has no
// dependency on the window.
package history

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

// Dir is the folder of a space holding its documents' versions.
const Dir = ".arcmd/history"

// Keep is how many versions of a document are kept; Gap how long a version
// takes in the edits after it before another is taken: edits within Gap of
// the last version replace it.
const (
	Keep = 100
	Gap  = 10 * time.Minute
)

// stamp names a version file by its time, so that names sort as times.
const stamp = "20060102-150405.000"

// Version is an earlier version of a document.
type Version struct {
	Time time.Time
	// File is the version's file name in the document's history folder.
	File string
	Size int64
	// Kept versions are never replaced by later edits: the version a
	// document had when first changed.
	Kept bool
}

// folder is the history folder of the document at rel of the space at
// root.
func folder(root, rel string) (string, error) {
	if _, err := vault.Resolve(root, rel); err != nil {
		return "", err
	}
	return filepath.Join(root, filepath.FromSlash(Dir), filepath.FromSlash(rel)), nil
}

// List returns the versions of the document at rel, newest first.
func List(root, rel string) ([]Version, error) {
	dir, err := folder(root, rel)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Version
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".arcmd") {
			continue
		}
		base := strings.TrimSuffix(name, ".arcmd")
		kept := strings.HasSuffix(base, "~")
		at, err := time.ParseInLocation(stamp, strings.TrimSuffix(base, "~"), time.Local)
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Version{Time: at, File: name, Size: info.Size(), Kept: kept})
	}
	slices.SortFunc(out, func(a, b Version) int { return b.Time.Compare(a.Time) })
	return out, nil
}

// Read returns the content of a version.
func Read(root, rel string, v Version) (string, error) {
	dir, err := folder(root, rel)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(v.File, `/\`) || v.File == ".." {
		return "", errors.New("history: invalid version")
	}
	data, err := os.ReadFile(filepath.Join(dir, v.File))
	return string(data), err
}

// Record takes a version of the document at rel holding content, at now.
// A content the last version already holds is not taken again; one within
// Gap of the last version replaces it, unless keep is set, as for the
// version a document had before this session first changed it. The oldest
// versions beyond Keep go.
func Record(root, rel, content string, now time.Time, keep bool) error {
	dir, err := folder(root, rel)
	if err != nil {
		return err
	}
	versions, err := List(root, rel)
	if err != nil {
		return err
	}
	if len(versions) > 0 {
		last := versions[0]
		if old, err := Read(root, rel, last); err == nil && old == content {
			return nil
		}
		if !keep && !last.Kept && now.Sub(last.Time) < Gap && !last.Time.After(now) {
			if err := os.Remove(filepath.Join(dir, last.File)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			versions = versions[1:]
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := now.Format(stamp) + ".arcmd"
	if keep {
		name = now.Format(stamp) + "~.arcmd"
	}
	for _, v := range versions {
		if strings.TrimSuffix(strings.TrimSuffix(v.File, ".arcmd"), "~") == now.Format(stamp) {
			// Two versions in one millisecond: the later wins.
			_ = os.Remove(filepath.Join(dir, v.File))
		}
	}
	if err := fsutil.WriteFileAtomic(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		return err
	}
	for _, v := range versions[min(len(versions), Keep-1):] {
		_ = os.Remove(filepath.Join(dir, v.File))
	}
	return nil
}

// Move takes the versions of the file or folder at from along to to, as
// the document moves or is renamed.
func Move(root, from, to string) error {
	src, err := folder(root, from)
	if err != nil {
		return err
	}
	dst, err := folder(root, to)
	if err != nil {
		return err
	}
	if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if _, err := os.Stat(dst); err == nil {
		return nil // the name's history stays with the name
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

// Op is a line's part in a difference.
type Op int8

const (
	Same Op = iota
	Added
	Removed
)

// Line is a line of a difference between two texts.
type Line struct {
	Op   Op
	Text string
}

// Diff returns the lines that turn a into b: unchanged, removed from a
// and added in b, in order, by the longest common subsequence of lines.
// Very long texts compare their changed middle only.
func Diff(a, b []string) []Line {
	// Common head and tail first: most edits touch a few lines.
	head := 0
	for head < len(a) && head < len(b) && a[head] == b[head] {
		head++
	}
	tail := 0
	for tail < len(a)-head && tail < len(b)-head && a[len(a)-1-tail] == b[len(b)-1-tail] {
		tail++
	}
	var out []Line
	for _, s := range a[:head] {
		out = append(out, Line{Same, s})
	}
	ma, mb := a[head:len(a)-tail], b[head:len(b)-tail]
	if len(ma)*len(mb) > 4_000_000 {
		for _, s := range ma {
			out = append(out, Line{Removed, s})
		}
		for _, s := range mb {
			out = append(out, Line{Added, s})
		}
	} else {
		// lcs[i][j] is the common length of ma[i:] and mb[j:].
		lcs := make([][]int32, len(ma)+1)
		for i := range lcs {
			lcs[i] = make([]int32, len(mb)+1)
		}
		for i := len(ma) - 1; i >= 0; i-- {
			for j := len(mb) - 1; j >= 0; j-- {
				if ma[i] == mb[j] {
					lcs[i][j] = lcs[i+1][j+1] + 1
				} else {
					lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
				}
			}
		}
		i, j := 0, 0
		for i < len(ma) || j < len(mb) {
			switch {
			case i < len(ma) && j < len(mb) && ma[i] == mb[j]:
				out = append(out, Line{Same, ma[i]})
				i++
				j++
			case j < len(mb) && (i == len(ma) || lcs[i][j+1] >= lcs[i+1][j]):
				out = append(out, Line{Added, mb[j]})
				j++
			default:
				out = append(out, Line{Removed, ma[i]})
				i++
			}
		}
	}
	for _, s := range a[len(a)-tail:] {
		out = append(out, Line{Same, s})
	}
	return out
}
