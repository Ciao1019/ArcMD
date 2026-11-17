package search

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Ciao1019/ArcMD/internal/fsutil"
)

const (
	cacheMagic          = "ARCMSCH\x01"
	cacheVersion uint32 = 1
)

var errInvalidCache = errors.New("search: invalid or incompatible disk cache")

// SaveCache atomically writes the pure disk index snapshot to path with 0600
// permissions so private document text stays unreadable to other local users.
// Live unsaved overlays must never be passed to SaveCache.
func SaveCache(path string, ix *Index) error {
	if path == "" {
		return nil
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("search: cache target is not a regular file")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	var count uint32
	approx := len(cacheMagic) + 8
	if ix != nil {
		count = uint32(len(ix.order))
		for _, k := range ix.order {
			if e := ix.entries[k]; e != nil {
				approx += 20 + len(e.space) + len(e.path) + len(e.file) + len(e.icon) + len(e.text) + 20
			}
		}
	}

	buf := make([]byte, 0, approx)
	buf = append(buf, cacheMagic...)
	buf = binary.LittleEndian.AppendUint32(buf, cacheVersion)
	buf = binary.LittleEndian.AppendUint32(buf, count)

	appendStr := func(s string) {
		buf = binary.LittleEndian.AppendUint32(buf, uint32(len(s)))
		buf = append(buf, s...)
	}

	if ix != nil {
		for _, k := range ix.order {
			e := ix.entries[k]
			if e == nil {
				appendStr("")
				appendStr("")
				appendStr("")
				appendStr("")
				appendStr("")
				buf = binary.LittleEndian.AppendUint64(buf, 0)
				buf = binary.LittleEndian.AppendUint64(buf, 0)
				buf = binary.LittleEndian.AppendUint32(buf, 0)
				continue
			}
			appendStr(e.space)
			appendStr(e.path)
			appendStr(e.file)
			appendStr(e.icon)
			appendStr(e.text)
			buf = binary.LittleEndian.AppendUint64(buf, uint64(e.size))
			buf = binary.LittleEndian.AppendUint64(buf, uint64(e.mod.Unix()))
			buf = binary.LittleEndian.AppendUint32(buf, uint32(e.mod.Nanosecond()))
		}
	}

	if err := fsutil.WriteFileAtomic(path, buf, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// LoadCache reads a previously saved disk index snapshot. Corrupt, symlinked,
// missing, or incompatible caches return an error so callers rebuild safely.
func LoadCache(path string) (*Index, error) {
	if path == "" {
		return nil, errInvalidCache
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errInvalidCache
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	headerLen := len(cacheMagic) + 8
	if len(data) < headerLen || string(data[:len(cacheMagic)]) != cacheMagic {
		return nil, errInvalidCache
	}
	pos := len(cacheMagic)
	ver := binary.LittleEndian.Uint32(data[pos:])
	pos += 4
	if ver != cacheVersion {
		return nil, errInvalidCache
	}
	count := binary.LittleEndian.Uint32(data[pos:])
	pos += 4
	// Each entry takes at least 5*4 + 20 = 40 bytes.
	if uint64(count)*40 > uint64(len(data)-pos) {
		return nil, errInvalidCache
	}

	readStr := func() (string, bool) {
		if len(data)-pos < 4 {
			return "", false
		}
		n := int(binary.LittleEndian.Uint32(data[pos:]))
		pos += 4
		if n < 0 || len(data)-pos < n {
			return "", false
		}
		s := string(data[pos : pos+n])
		pos += n
		return s, true
	}

	ix := &Index{
		entries: make(map[key]*entry, count),
		order:   make([]key, 0, count),
	}
	for i := uint32(0); i < count; i++ {
		space, ok1 := readStr()
		relPath, ok2 := readStr()
		file, ok3 := readStr()
		icon, ok4 := readStr()
		text, ok5 := readStr()
		if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || len(data)-pos < 20 {
			return nil, errInvalidCache
		}
		size := int64(binary.LittleEndian.Uint64(data[pos:]))
		pos += 8
		modSec := int64(binary.LittleEndian.Uint64(data[pos:]))
		pos += 8
		modNsec := int32(binary.LittleEndian.Uint32(data[pos:]))
		pos += 4
		if space == "" || relPath == "" {
			continue
		}
		k := key{space, relPath}
		if _, dup := ix.entries[k]; dup {
			continue
		}
		ix.entries[k] = &entry{
			file:  file,
			text:  text,
			icon:  icon,
			size:  size,
			mod:   time.Unix(modSec, int64(modNsec)),
			space: space,
			path:  relPath,
		}
		ix.order = append(ix.order, k)
	}
	if pos != len(data) {
		return nil, errInvalidCache
	}
	return ix, nil
}
