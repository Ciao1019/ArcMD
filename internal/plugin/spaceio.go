package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

// SpaceSnapshot captures one syncable Space's identity and root folder for an invocation.
type SpaceSnapshot struct {
	ID   string
	Name string
	Root string
}

// TrashFunc moves a file at rel inside space root to ArcMD's trashbin and system trash.
type TrashFunc func(spaceID, spaceName, root, rel string) error

// SpaceError tags a SpaceService failure with a HostReply error code.
type SpaceError struct {
	Code string
	Err  error
}

func (e *SpaceError) Error() string { return e.Err.Error() }
func (e *SpaceError) Unwrap() error { return e.Err }

// IsSyncPath reports whether rel is a clean, relative, non-hidden path eligible
// for cloud sync (documents, subfolders, assets/*, and .arcmd/templates/*),
// excluding .arcmd/history/*, .git/*, node_modules/*, and hidden dotfiles.
func IsSyncPath(rel string) bool {
	if rel == "" || strings.Contains(rel, `\`) || path.IsAbs(rel) || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return false
	}
	if path.Clean(rel) != rel {
		return false
	}
	parts := strings.Split(rel, "/")
	if len(parts) == 0 {
		return false
	}
	for _, seg := range parts {
		if seg == "" || seg == "." || seg == ".." || seg == "node_modules" {
			return false
		}
	}
	base := parts[len(parts)-1]
	if strings.HasPrefix(base, ".") {
		return false
	}
	if parts[0] == ".arcmd" {
		if len(parts) < 3 || parts[1] != "templates" {
			return false
		}
		for _, seg := range parts[2:] {
			if strings.HasPrefix(seg, ".") {
				return false
			}
		}
		if slices.Contains(parts[2:len(parts)-1], "assets") {
			return true
		}
		return vault.IsDocument(base) || isSyncAttachment(base)
	}
	for _, seg := range parts {
		if strings.HasPrefix(seg, ".") {
			return false
		}
	}
	if slices.Contains(parts[:len(parts)-1], "assets") {
		return true
	}
	return vault.IsDocument(base) || isSyncAttachment(base)
}

func isSyncAttachment(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
		return true
	}
	return false
}

// FolderSpaces implements SpaceService directly against a snapshotted set of
// Space folders with optimistic concurrency and trash-safe deletes.
type FolderSpaces struct {
	mu      sync.Mutex
	spaces  map[string]SpaceSnapshot
	order   []SpaceInfo
	dirty   map[SpaceFileKey]bool
	trash   TrashFunc
	now     func() time.Time
	touched map[SpaceFileKey]bool
}

// NewFolderSpaces constructs a SpaceService over spaces, dirty open-document paths,
// and trashFn. External spaces or spaces without a root should be omitted by the caller.
func NewFolderSpaces(spaces []SpaceSnapshot, dirty map[SpaceFileKey]bool, trashFn TrashFunc) *FolderSpaces {
	byID := make(map[string]SpaceSnapshot, len(spaces))
	order := make([]SpaceInfo, 0, len(spaces))
	for _, sp := range spaces {
		if sp.ID == "" || sp.Root == "" {
			continue
		}
		if _, dup := byID[sp.ID]; dup {
			continue
		}
		byID[sp.ID] = sp
		order = append(order, SpaceInfo{ID: sp.ID, Name: sp.Name})
	}
	dirtyCopy := make(map[SpaceFileKey]bool, len(dirty))
	for k, v := range dirty {
		if v {
			dirtyCopy[k] = true
		}
	}
	return &FolderSpaces{
		spaces:  byID,
		order:   order,
		dirty:   dirtyCopy,
		trash:   trashFn,
		now:     time.Now,
		touched: map[SpaceFileKey]bool{},
	}
}

func (f *FolderSpaces) space(id string) (SpaceSnapshot, error) {
	sp, ok := f.spaces[id]
	if !ok {
		return SpaceSnapshot{}, &SpaceError{Code: CodeNotFound, Err: fmt.Errorf("unknown space %q", id)}
	}
	return sp, nil
}

func (f *FolderSpaces) markTouched(spaceID, rel string) {
	f.mu.Lock()
	f.touched[SpaceFileKey{SpaceID: spaceID, Path: rel}] = true
	f.mu.Unlock()
}

// Touched returns all space files created, modified, or trashed through this SpaceService.
func (f *FolderSpaces) Touched() []SpaceFileKey {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]SpaceFileKey, 0, len(f.touched))
	for k := range f.touched {
		out = append(out, k)
	}
	slices.SortFunc(out, func(a, b SpaceFileKey) int {
		if c := strings.Compare(a.SpaceID, b.SpaceID); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	return out
}

// ListSpaces lists all snapshotted spaces in order.
func (f *FolderSpaces) ListSpaces() []SpaceInfo {
	return slices.Clone(f.order)
}

// ListFiles lists sync-eligible files inside spaceID.
func (f *FolderSpaces) ListFiles(spaceID string) ([]SpaceFileEntry, error) {
	sp, err := f.space(spaceID)
	if err != nil {
		return nil, err
	}
	var out []SpaceFileEntry
	err = filepath.WalkDir(sp.Root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if p == sp.Root {
				return walkErr
			}
			return nil
		}
		if p == sp.Root {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		relOS, err := filepath.Rel(sp.Root, p)
		if err != nil {
			return nil
		}
		rel := filepath.ToSlash(relOS)
		if d.IsDir() {
			if rel == ".arcmd" || rel == ".arcmd/templates" {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !IsSyncPath(rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		out = append(out, SpaceFileEntry{
			Path:            rel,
			Size:            info.Size(),
			ModTimeUnixNano: info.ModTime().UnixNano(),
			Dirty:           f.dirty[SpaceFileKey{SpaceID: spaceID, Path: rel}],
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b SpaceFileEntry) int {
		return strings.Compare(a.Path, b.Path)
	})
	return out, nil
}

// ReadFile reads one sync-eligible file inside spaceID and returns its bytes and metadata.
func (f *FolderSpaces) ReadFile(spaceID, rel string) ([]byte, SpaceFileMeta, error) {
	sp, err := f.space(spaceID)
	if err != nil {
		return nil, SpaceFileMeta{}, err
	}
	if !IsSyncPath(rel) {
		return nil, SpaceFileMeta{}, &SpaceError{Code: CodeInvalidArgument, Err: fmt.Errorf("path %q cannot be synced", rel)}
	}
	full, err := vault.Resolve(sp.Root, rel)
	if err != nil {
		return nil, SpaceFileMeta{}, &SpaceError{Code: CodeInvalidArgument, Err: err}
	}
	info, err := os.Lstat(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, SpaceFileMeta{}, &SpaceError{Code: CodeNotFound, Err: err}
		}
		return nil, SpaceFileMeta{}, err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, SpaceFileMeta{}, &SpaceError{Code: CodeInvalidArgument, Err: fmt.Errorf("path %q is not a regular file", rel)}
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return nil, SpaceFileMeta{}, err
	}
	return b, SpaceFileMeta{
		Path:            rel,
		Size:            info.Size(),
		ModTimeUnixNano: info.ModTime().UnixNano(),
		SHA256:          sha256Hex(b),
		Dirty:           f.dirty[SpaceFileKey{SpaceID: spaceID, Path: rel}],
	}, nil
}

// WriteFile writes data to rel atomically if it matches expectedSHA256 (empty means new file)
// and has no unsaved editor edits.
func (f *FolderSpaces) WriteFile(spaceID, rel string, data []byte, expectedSHA256 string, modTimeUnixNano int64) (SpaceFileMeta, error) {
	sp, err := f.space(spaceID)
	if err != nil {
		return SpaceFileMeta{}, err
	}
	if !IsSyncPath(rel) {
		return SpaceFileMeta{}, &SpaceError{Code: CodeInvalidArgument, Err: fmt.Errorf("path %q cannot be synced", rel)}
	}
	if f.dirty[SpaceFileKey{SpaceID: spaceID, Path: rel}] {
		return SpaceFileMeta{}, &SpaceError{Code: CodeDirty, Err: fmt.Errorf("file %q has unsaved local edits", rel)}
	}
	full, err := vault.Resolve(sp.Root, rel)
	if err != nil {
		return SpaceFileMeta{}, &SpaceError{Code: CodeInvalidArgument, Err: err}
	}
	if info, err := os.Lstat(full); err == nil {
		if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return SpaceFileMeta{}, &SpaceError{Code: CodeInvalidArgument, Err: fmt.Errorf("path %q is not a regular file", rel)}
		}
		cur, rerr := os.ReadFile(full)
		if rerr != nil {
			return SpaceFileMeta{}, rerr
		}
		curSum := sha256Hex(cur)
		if expectedSHA256 == "" || !strings.EqualFold(curSum, expectedSHA256) {
			return SpaceFileMeta{}, &SpaceError{Code: CodeConflict, Err: fmt.Errorf("file %q changed locally", rel)}
		}
	} else if !fsutil.IsMissingFile(full, err) {
		return SpaceFileMeta{}, err
	} else if expectedSHA256 != "" {
		return SpaceFileMeta{}, &SpaceError{Code: CodeConflict, Err: fmt.Errorf("file %q was removed or moved locally", rel)}
	}

	if err := fsutil.WriteFileAtomic(full, data, 0o644); err != nil {
		return SpaceFileMeta{}, err
	}
	if modTimeUnixNano > 0 {
		mt := time.Unix(0, modTimeUnixNano)
		_ = os.Chtimes(full, mt, mt)
	}
	info, err := os.Stat(full)
	if err != nil {
		return SpaceFileMeta{}, err
	}
	f.markTouched(spaceID, rel)
	return SpaceFileMeta{
		Path:            rel,
		Size:            info.Size(),
		ModTimeUnixNano: info.ModTime().UnixNano(),
		SHA256:          sha256Hex(data),
	}, nil
}

// WriteConflict writes data into an exclusive conflict copy next to rel without overwriting existing files.
func (f *FolderSpaces) WriteConflict(spaceID, rel string, data []byte) (SpaceFileMeta, error) {
	sp, err := f.space(spaceID)
	if err != nil {
		return SpaceFileMeta{}, err
	}
	if !IsSyncPath(rel) {
		return SpaceFileMeta{}, &SpaceError{Code: CodeInvalidArgument, Err: fmt.Errorf("path %q cannot be synced", rel)}
	}
	dir := path.Dir(rel)
	if dir == "." {
		dir = ""
	}
	base := path.Base(rel)
	ext := vault.FileExt(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		stem = base
		ext = ""
	}
	stamp := f.now().Format("2006-01-02 1504")
	for i := 1; i < 1000; i++ {
		name := fmt.Sprintf("%s (冲突副本 %s)%s", stem, stamp, ext)
		if i > 1 {
			name = fmt.Sprintf("%s (冲突副本 %s)-%d%s", stem, stamp, i, ext)
		}
		candRel := path.Join(dir, name)
		full, err := vault.Resolve(sp.Root, candRel)
		if err != nil {
			return SpaceFileMeta{}, &SpaceError{Code: CodeInvalidArgument, Err: err}
		}
		err = fsutil.CreateFileAtomic(full, data, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return SpaceFileMeta{}, err
		}
		info, err := os.Stat(full)
		if err != nil {
			return SpaceFileMeta{}, err
		}
		f.markTouched(spaceID, candRel)
		return SpaceFileMeta{
			Path:            candRel,
			Size:            info.Size(),
			ModTimeUnixNano: info.ModTime().UnixNano(),
			SHA256:          sha256Hex(data),
		}, nil
	}
	return SpaceFileMeta{}, errors.New("plugin: too many conflict copies")
}

// DeleteFile verifies expectedSHA256 and moves rel to ArcMD's trash via TrashFunc.
func (f *FolderSpaces) DeleteFile(spaceID, rel, expectedSHA256 string) error {
	sp, err := f.space(spaceID)
	if err != nil {
		return err
	}
	if !IsSyncPath(rel) {
		return &SpaceError{Code: CodeInvalidArgument, Err: fmt.Errorf("path %q cannot be synced", rel)}
	}
	if f.dirty[SpaceFileKey{SpaceID: spaceID, Path: rel}] {
		return &SpaceError{Code: CodeDirty, Err: fmt.Errorf("file %q has unsaved local edits", rel)}
	}
	full, err := vault.Resolve(sp.Root, rel)
	if err != nil {
		return &SpaceError{Code: CodeInvalidArgument, Err: err}
	}
	info, err := os.Lstat(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &SpaceError{Code: CodeNotFound, Err: err}
		}
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return &SpaceError{Code: CodeInvalidArgument, Err: fmt.Errorf("path %q is not a regular file", rel)}
	}
	cur, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	if expectedSHA256 != "" && !strings.EqualFold(sha256Hex(cur), expectedSHA256) {
		return &SpaceError{Code: CodeConflict, Err: fmt.Errorf("file %q changed locally before delete", rel)}
	}
	if f.trash == nil {
		return errors.New("plugin: trash handler is not configured")
	}
	if err := f.trash(sp.ID, sp.Name, sp.Root, rel); err != nil {
		return err
	}
	f.markTouched(spaceID, rel)
	return nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
