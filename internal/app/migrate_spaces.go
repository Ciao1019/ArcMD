package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/space"
)

type legacyMigrationCandidate struct {
	sp    *space.Space
	root  string
	rel   string
	depth int
	order int
}

type movedSpaceRoot struct {
	oldRoot string
	newRoot string
}

type migrationSourceKind int

const (
	sourceDir migrationSourceKind = iota
	sourceMissing
	sourceBlocked
	sourceError
)

// migrateLegacySpaces moves existing spaces rooted inside LegacyDocumentsDir
// (the previous ~/Documents/ArcMD default) into DocumentsDir (~/.arcmd/spaces)
// and updates spaces.json. On macOS, moving spaces out of ~/Documents prevents
// repeated TCC folder-permission prompts after every app update.
//
// When no space in spaces.json points inside LegacyDocumentsDir, this returns
// immediately without touching the filesystem so macOS never prompts for
// ~/Documents access.
func (a *App) migrateLegacySpaces() {
	legacy := strings.TrimSpace(a.cfg.LegacyDocumentsDir)
	destBase := strings.TrimSpace(a.cfg.DocumentsDir)
	if legacy == "" || destBase == "" || a.state == nil {
		return
	}
	legacy = filepath.Clean(legacy)
	destBase = filepath.Clean(destBase)
	if sameDirPath(legacy, destBase) {
		return
	}

	var candidates []legacyMigrationCandidate
	for i, sp := range a.state.Spaces {
		if sp == nil || sp.IsExternal() || strings.TrimSpace(sp.Root) == "" {
			continue
		}
		cleanRoot := filepath.Clean(sp.Root)
		rel, ok := relUnderDir(legacy, cleanRoot)
		if !ok {
			continue
		}
		candidates = append(candidates, legacyMigrationCandidate{
			sp:    sp,
			root:  cleanRoot,
			rel:   rel,
			depth: strings.Count(rel, string(filepath.Separator)),
			order: i,
		})
	}
	if len(candidates) == 0 {
		return
	}

	// Refuse to traverse into LegacyDocumentsDir if it is itself a symlink
	// or non-directory; allow ErrNotExist so crash recovery can still finish.
	if lfi, err := os.Lstat(legacy); err == nil {
		if lfi.Mode()&os.ModeSymlink != 0 || !lfi.IsDir() {
			return
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		a.notifyDeferred("迁移原有空间目录失败，将继续使用原位置。%v", err)
		return
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].depth != candidates[j].depth {
			return candidates[i].depth < candidates[j].depth
		}
		return candidates[i].order < candidates[j].order
	})

	var moved []movedSpaceRoot
	var blocked []string
	migrated := 0
	var firstErr error

	for _, cand := range candidates {
		if dst, ok := remapMovedRoot(moved, cand.root); ok {
			oldRoot := cand.sp.Root
			cand.sp.Root = dst
			if err := a.store.Save(a.state); err != nil {
				cand.sp.Root = oldRoot
				blocked = append(blocked, cand.root)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			migrated++
			continue
		}
		if isBlockedRoot(blocked, cand.root) {
			continue
		}

		kind, err := inspectMigrationSource(legacy, cand.rel)
		switch kind {
		case sourceBlocked:
			blocked = append(blocked, cand.root)
			continue
		case sourceError:
			blocked = append(blocked, cand.root)
			if firstErr == nil {
				firstErr = err
			}
			continue
		case sourceMissing:
			// Crash recovery: a previous run may have renamed the folder
			// before persisting spaces.json. Only adopt an unclaimed real dir.
			dst := filepath.Join(destBase, cand.rel)
			if !isClaimedByOtherSpace(a.state.Spaces, cand.sp, cand.root, dst) &&
				isRealDirPath(destBase, cand.rel) {
				oldRoot := cand.sp.Root
				cand.sp.Root = dst
				if saveErr := a.store.Save(a.state); saveErr != nil {
					cand.sp.Root = oldRoot
					blocked = append(blocked, cand.root)
					if firstErr == nil {
						firstErr = saveErr
					}
					continue
				}
				moved = append(moved, movedSpaceRoot{oldRoot: cand.root, newRoot: dst})
				migrated++
			}
			continue
		}

		dst, err := freeMigrationPath(destBase, cand.rel, a.state.Spaces, cand.sp, cand.root)
		if err != nil {
			blocked = append(blocked, cand.root)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if sameDirPath(cand.root, dst) {
			continue
		}
		if _, nested := relUnderDir(cand.root, dst); nested {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			blocked = append(blocked, cand.root)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := os.Rename(cand.root, dst); err != nil {
			blocked = append(blocked, cand.root)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		oldRoot := cand.sp.Root
		cand.sp.Root = dst
		if err := a.store.Save(a.state); err != nil {
			cand.sp.Root = oldRoot
			_ = os.Rename(dst, cand.root)
			blocked = append(blocked, cand.root)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		moved = append(moved, movedSpaceRoot{oldRoot: cand.root, newRoot: dst})
		migrated++
	}

	if migrated > 0 {
		for _, m := range moved {
			pruneEmptyParents(filepath.Dir(m.oldRoot), legacy)
		}
		_ = os.Remove(legacy)
	}
	switch {
	case migrated > 0 && firstErr == nil:
		a.notifyDeferred("已将原有空间目录迁移至 %s。", shortPath(destBase))
	case migrated > 0 && firstErr != nil:
		a.notifyDeferred("部分空间已迁移至 %s，仍有空间保留在原位置。%v", shortPath(destBase), firstErr)
	case firstErr != nil:
		a.notifyDeferred("迁移原有空间目录失败，将继续使用原位置。%v", firstErr)
	}
}

// inspectMigrationSource checks every directory component from legacy down to
// legacy/rel using Lstat so a symlink in any parent or leaf is never traversed
// or moved out of its target location.
func inspectMigrationSource(legacy, rel string) (migrationSourceKind, error) {
	cur := legacy
	for _, seg := range splitPathSegments(rel) {
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			return sourceMissing, nil
		}
		if err != nil {
			return sourceError, err
		}
		if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
			return sourceBlocked, nil
		}
	}
	return sourceDir, nil
}

// isRealDirPath reports whether destBase/rel exists as a chain of real
// directories under destBase with no symlink segments.
func isRealDirPath(destBase, rel string) bool {
	cur := destBase
	for _, seg := range splitPathSegments(rel) {
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
			return false
		}
	}
	return true
}

func sameDirPath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// relUnderDir reports whether target is strictly inside base and returns its
// relative path under base. Segment-wise comparison avoids UTF-8 byte-length
// mismatches during Unicode case folding on case-insensitive filesystems.
func relUnderDir(base, target string) (string, bool) {
	base = filepath.Clean(base)
	target = filepath.Clean(target)
	if !filepath.IsAbs(base) || !filepath.IsAbs(target) {
		return "", false
	}
	if rel, err := filepath.Rel(base, target); err == nil && isStrictSubRel(rel) {
		return rel, true
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return "", false
	}
	baseVol, targetVol := filepath.VolumeName(base), filepath.VolumeName(target)
	if !strings.EqualFold(baseVol, targetVol) {
		return "", false
	}
	baseSegs := splitPathSegments(base[len(baseVol):])
	targetSegs := splitPathSegments(target[len(targetVol):])
	if len(targetSegs) <= len(baseSegs) {
		return "", false
	}
	for i := range baseSegs {
		if !strings.EqualFold(baseSegs[i], targetSegs[i]) {
			return "", false
		}
	}
	rel := filepath.Join(targetSegs[len(baseSegs):]...)
	if !isStrictSubRel(rel) {
		return "", false
	}
	return rel, true
}

func splitPathSegments(p string) []string {
	sep := string(filepath.Separator)
	p = strings.Trim(p, sep)
	if p == "" {
		return nil
	}
	return strings.Split(p, sep)
}

func isStrictSubRel(rel string) bool {
	return rel != "" && rel != "." && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(rel)
}

func remapMovedRoot(moved []movedSpaceRoot, root string) (string, bool) {
	for _, m := range moved {
		if sameDirPath(root, m.oldRoot) {
			return m.newRoot, true
		}
		if sub, ok := relUnderDir(m.oldRoot, root); ok {
			return filepath.Join(m.newRoot, sub), true
		}
	}
	return "", false
}

func isBlockedRoot(blocked []string, root string) bool {
	for _, b := range blocked {
		if sameDirPath(root, b) {
			return true
		}
		if _, ok := relUnderDir(b, root); ok {
			return true
		}
	}
	return false
}

// isClaimedByOtherSpace reports whether dst equals, contains, or lies inside
// another space's configured Root (ignoring duplicates of selfOldRoot, which
// follow self via remapMovedRoot).
func isClaimedByOtherSpace(spaces []*space.Space, self *space.Space, selfOldRoot, dst string) bool {
	for _, other := range spaces {
		if other == nil || other == self || other.IsExternal() || strings.TrimSpace(other.Root) == "" {
			continue
		}
		otherRoot := filepath.Clean(other.Root)
		if sameDirPath(otherRoot, selfOldRoot) {
			continue
		}
		if sameDirPath(otherRoot, dst) {
			return true
		}
		if _, inside := relUnderDir(otherRoot, dst); inside {
			return true
		}
		if _, contains := relUnderDir(dst, otherRoot); contains {
			return true
		}
	}
	return false
}

func freeMigrationPath(destBase, rel string, spaces []*space.Space, self *space.Space, selfOldRoot string) (string, error) {
	base := filepath.Join(destBase, rel)
	// If rel is nested inside a parent folder already owned by another space,
	// flatten to destBase/<leaf> so numbered siblings do not land inside it.
	if dir := filepath.Dir(base); !sameDirPath(dir, destBase) && isClaimedByOtherSpace(spaces, self, selfOldRoot, dir) {
		base = filepath.Join(destBase, filepath.Base(rel))
	}
	for i := 1; i < 1000; i++ {
		cand := base
		if i > 1 {
			cand = fmt.Sprintf("%s %d", base, i)
		}
		if isClaimedByOtherSpace(spaces, self, selfOldRoot, cand) {
			continue
		}
		if _, err := os.Lstat(cand); errors.Is(err, fs.ErrNotExist) {
			return cand, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("app: too many migrated folders with that name")
}

func pruneEmptyParents(dir, stopAt string) {
	for {
		if sameDirPath(dir, stopAt) {
			return
		}
		if _, ok := relUnderDir(stopAt, dir); !ok {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
