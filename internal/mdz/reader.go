package mdz

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

// Archive represents a validated read-only MDZip (.mdz) archive.
// All exported methods are safe for concurrent use.
type Archive struct {
	mu       sync.Mutex
	zipSrc   *scopedZipFile // nil when opened from in-memory bytes
	zr       *zip.Reader
	entries  map[string]*zip.File // keyed by NFC-normalized stored file path
	folded   map[string][]string  // Unicode case-folded path -> stored file paths (for near-match hints)
	allFiles []string             // sorted stored paths of all non-directory entries
	pages    []string             // sorted stored paths of all .md/.markdown entries

	manifest   *Manifest
	mode       string
	entryPoint string
	coverPath  string
	warnings   []string
	closed     bool
}

// scopedZipFile backs zip.Reader without keeping a persistent OS file handle
// open when idle, allowing Windows callers to rename, move, or delete an open
// .mdz archive cleanly.
type scopedZipFile struct {
	path string
	cur  *os.File // guarded by Archive.mu (or single-goroutine Open)
}

func (s *scopedZipFile) withFile(fn func() ([]byte, error)) ([]byte, error) {
	if s.cur != nil {
		return fn()
	}
	f, err := os.Open(s.path)
	if err != nil {
		return nil, err
	}
	s.cur = f
	defer func() {
		s.cur = nil
		_ = f.Close()
	}()
	return fn()
}

func (s *scopedZipFile) ReadAt(p []byte, off int64) (int, error) {
	if s.cur != nil {
		return s.cur.ReadAt(p, off)
	}
	f, err := os.Open(s.path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	return f.ReadAt(p, off)
}

// Open opens and validates an .mdz archive from disk.
// If the archive is structurally valid but has no unambiguous entry point
// (Spec §5.5 rule 4), Open returns the opened *Archive together with
// ErrEntryPointUnresolved so callers can present MarkdownPages() to the user.
func Open(filePath string) (*Archive, error) {
	if filePath == "" {
		return nil, fmt.Errorf("%w: empty path", ErrZipInvalid)
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if info.IsDir() {
		_ = f.Close()
		return nil, fmt.Errorf("%w: path is a directory", ErrZipInvalid)
	}
	src := &scopedZipFile{path: filePath, cur: f}
	zr, err := zip.NewReader(src, info.Size())
	if err != nil {
		src.cur = nil
		_ = f.Close()
		return nil, fmt.Errorf("%w: %v", ErrZipInvalid, err)
	}
	arc, openErr := buildArchive(zr, src)
	src.cur = nil
	_ = f.Close()
	return arc, openErr
}

// OpenBytes opens and validates an .mdz archive from raw bytes in memory.
// As with Open, when no unambiguous entry point can be determined, OpenBytes
// returns (*Archive, ErrEntryPointUnresolved).
func OpenBytes(data []byte) (*Archive, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: empty input", ErrZipInvalid)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrZipInvalid, err)
	}
	return buildArchive(zr, nil)
}

func buildArchive(zr *zip.Reader, src *scopedZipFile) (*Archive, error) {
	if len(zr.File) > MaxEntries {
		return nil, fmt.Errorf("%w: archive contains %d entries (max %d)", ErrLimitExceeded, len(zr.File), MaxEntries)
	}

	arc := &Archive{
		zipSrc:  src,
		zr:      zr,
		entries: make(map[string]*zip.File, len(zr.File)),
		folded:  make(map[string][]string, len(zr.File)),
		mode:    ModeDocument,
	}

	seenNFC := make(map[string]bool, len(zr.File)) // normalized path -> isDir
	dirPrefixes := make(map[string]struct{})
	var totalUncompressed uint64

	for _, f := range zr.File {
		// Reject encrypted entries (Spec §4, §13.1): bit 0 is standard ZIP encryption,
		// method 99 is WinZip AES encryption.
		if f.Flags&0x1 != 0 || f.Method == 99 {
			return nil, fmt.Errorf("%w: entry %q is encrypted", ErrZipEncrypted, f.Name)
		}
		// Reject compression methods other than Store (0) and Deflate (8) (Spec §4, §16.5).
		if f.Method != zip.Store && f.Method != zip.Deflate {
			return nil, fmt.Errorf("%w: entry %q uses unsupported compression method %d", ErrZipInvalid, f.Name, f.Method)
		}
		// Reject symlinks and special filesystem nodes (Spec §13.1).
		mode := f.Mode()
		if mode&fs.ModeSymlink != 0 || mode&(fs.ModeDevice|fs.ModeNamedPipe|fs.ModeSocket|fs.ModeCharDevice) != 0 {
			return nil, fmt.Errorf("%w: entry %q is a symlink or special file", ErrPathInvalid, f.Name)
		}

		// Validate entry path syntax (Spec §5.4).
		if err := ValidateEntryPath(f.Name, true); err != nil {
			return nil, err
		}
		isDir := strings.HasSuffix(f.Name, "/")
		trimmed := strings.TrimSuffix(f.Name, "/")
		nfc := normalizeNFC(trimmed)

		// Check duplicate normalized paths and file/dir conflicts (Spec §5.4, §13.1).
		if prevIsDir, exists := seenNFC[nfc]; exists {
			if prevIsDir != isDir {
				return nil, fmt.Errorf("%w: %q is both file and directory", ErrPathConflict, trimmed)
			}
			return nil, fmt.Errorf("%w: duplicate entry %q", ErrDuplicatePath, f.Name)
		}
		seenNFC[nfc] = isDir

		// Record every ancestor directory of this entry.
		if isDir {
			dirPrefixes[nfc] = struct{}{}
		}
		for idx := strings.IndexByte(nfc, '/'); idx >= 0; {
			dirPrefixes[nfc[:idx]] = struct{}{}
			next := strings.IndexByte(nfc[idx+1:], '/')
			if next < 0 {
				break
			}
			idx += 1 + next
		}

		if isDir {
			continue
		}

		// Enforce declared size & compression ratio limits (Spec §13.1).
		if f.UncompressedSize64 > MaxEntryBytes {
			return nil, fmt.Errorf("%w: entry %q exceeds max entry size (%d bytes)", ErrLimitExceeded, f.Name, f.UncompressedSize64)
		}
		if totalUncompressed+f.UncompressedSize64 > MaxTotalBytes || totalUncompressed+f.UncompressedSize64 < totalUncompressed {
			return nil, fmt.Errorf("%w: archive exceeds max total size (%d bytes)", ErrLimitExceeded, MaxTotalBytes)
		}
		totalUncompressed += f.UncompressedSize64
		if f.UncompressedSize64 > minRatioCheckBytes {
			if f.CompressedSize64 == 0 || f.UncompressedSize64/f.CompressedSize64 > MaxCompressionRatio {
				return nil, fmt.Errorf("%w: entry %q exceeds compression ratio limit", ErrLimitExceeded, f.Name)
			}
		}

		arc.entries[nfc] = f
		foldedKey := foldCase(nfc)
		arc.folded[foldedKey] = append(arc.folded[foldedKey], f.Name)
		arc.allFiles = append(arc.allFiles, f.Name)
		if IsMarkdownPath(f.Name) {
			arc.pages = append(arc.pages, f.Name)
		}
	}

	// Second pass: ensure no file shares a path with an implicit or explicit directory (Spec §5.4).
	for nfc, isDir := range seenNFC {
		if !isDir {
			if _, usedAsDir := dirPrefixes[nfc]; usedAsDir {
				return nil, fmt.Errorf("%w: file %q conflicts with directory", ErrPathConflict, nfc)
			}
		}
	}

	slices.Sort(arc.allFiles)
	slices.Sort(arc.pages)

	// Parse manifest.json if present at the archive root (Spec §6).
	if mf := arc.entries[ManifestPath]; mf != nil {
		raw, err := arc.readZipFileLocked(mf, MaxEntryBytes)
		if err != nil {
			return nil, err
		}
		m, warnings, err := parseManifest(raw)
		if err != nil {
			return nil, err
		}
		arc.manifest = m
		arc.warnings = append(arc.warnings, warnings...)
		if m.Mode != "" {
			arc.mode = m.Mode
		}
		if m.Cover != "" {
			if cf := arc.entries[normalizeNFC(m.Cover)]; cf != nil {
				arc.coverPath = cf.Name
			} else {
				arc.warnings = append(arc.warnings, fmt.Sprintf("ignored missing manifest cover %q", m.Cover))
			}
		}
	}

	// Entry point discovery (Spec §5.5).
	ep, epErr := arc.discoverEntryPoint()
	if epErr != nil {
		if epErr == ErrEntryPointUnresolved {
			return arc, ErrEntryPointUnresolved
		}
		return nil, epErr
	}
	arc.entryPoint = ep
	return arc, nil
}

func (a *Archive) discoverEntryPoint() (string, error) {
	// Rule 1: manifest.json declares entryPoint.
	if a.manifest != nil && a.manifest.EntryPoint != "" {
		nfc := normalizeNFC(a.manifest.EntryPoint)
		if f := a.entries[nfc]; f != nil {
			return f.Name, nil
		}
		if near := a.nearMatch(nfc); near != "" {
			return "", fmt.Errorf("%w: %q (did you mean %q?)", ErrEntryPointMissing, a.manifest.EntryPoint, near)
		}
		return "", fmt.Errorf("%w: %q", ErrEntryPointMissing, a.manifest.EntryPoint)
	}

	// Rule 2: root-level index.md (case-sensitive NFC exact match).
	if f := a.entries["index.md"]; f != nil {
		return f.Name, nil
	}

	// Rule 3: root-level .md / .markdown files, setting aside closed support files.
	var rootAll []string
	var rootContent []string
	for _, p := range a.pages {
		if strings.ContainsRune(p, '/') {
			continue
		}
		rootAll = append(rootAll, p)
		if !isSupportFile(p) {
			rootContent = append(rootContent, p)
		}
	}
	if len(rootContent) == 1 {
		return rootContent[0], nil
	}
	if len(rootContent) == 0 && len(rootAll) == 1 {
		return rootAll[0], nil
	}

	// Rule 4: ambiguous or no Markdown entry point.
	return "", ErrEntryPointUnresolved
}

func (a *Archive) nearMatch(nfcPath string) string {
	candidates := a.folded[foldCase(nfcPath)]
	for _, cand := range candidates {
		if normalizeNFC(cand) != nfcPath {
			return cand
		}
	}
	return ""
}

// SetPath updates the underlying disk path after the .mdz file is moved or renamed.
func (a *Archive) SetPath(filePath string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.zipSrc != nil {
		a.zipSrc.path = filePath
	}
}

// Close marks the archive closed. Because disk reads use a scoped file handle,
// no OS file descriptor is held open between reads.
func (a *Archive) Close() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	return nil
}

// Manifest returns a shallow copy of the parsed manifest, or nil if manifest.json was absent.
func (a *Archive) Manifest() *Manifest {
	if a == nil || a.manifest == nil {
		return nil
	}
	cp := *a.manifest
	return &cp
}

// Mode returns the effective interpretation mode ("document" or "project").
func (a *Archive) Mode() string {
	if a == nil || a.mode == "" {
		return ModeDocument
	}
	return a.mode
}

// EntryPoint returns the resolved primary Markdown entry path, or "" if unresolved.
func (a *Archive) EntryPoint() string {
	if a == nil {
		return ""
	}
	return a.entryPoint
}

// CoverPath returns the validated archive path of manifest.cover, or "".
func (a *Archive) CoverPath() string {
	if a == nil {
		return ""
	}
	return a.coverPath
}

// Warnings returns non-fatal conformance warnings recorded while opening the archive.
func (a *Archive) Warnings() []string {
	if a == nil || len(a.warnings) == 0 {
		return nil
	}
	return slices.Clone(a.warnings)
}

// Title returns a display title derived from manifest.title, the entry point
// filename, or empty string if neither is available.
func (a *Archive) Title() string {
	if a == nil {
		return ""
	}
	if a.manifest != nil && a.manifest.Title != "" {
		return a.manifest.Title
	}
	if a.entryPoint != "" {
		base := path.Base(a.entryPoint)
		if dot := strings.LastIndexByte(base, '.'); dot > 0 {
			return base[:dot]
		}
		return base
	}
	return ""
}

// MarkdownPages returns a sorted copy of all Markdown (.md / .markdown) file paths in the archive.
func (a *Archive) MarkdownPages() []string {
	if a == nil {
		return nil
	}
	return slices.Clone(a.pages)
}

// Files returns a sorted copy of all file entry paths in the archive.
func (a *Archive) Files() []string {
	if a == nil {
		return nil
	}
	return slices.Clone(a.allFiles)
}

// Entries returns metadata for every file entry in the archive, sorted by path.
func (a *Archive) Entries() []EntryInfo {
	if a == nil {
		return nil
	}
	out := make([]EntryInfo, 0, len(a.allFiles))
	for _, p := range a.allFiles {
		if f := a.entries[normalizeNFC(p)]; f != nil {
			out = append(out, EntryInfo{
				Path:             f.Name,
				UncompressedSize: f.UncompressedSize64,
				CompressedSize:   f.CompressedSize64,
				Method:           f.Method,
			})
		}
	}
	return out
}

// HasFile reports whether archivePath exists in the archive (NFC-normalized, case-sensitive).
func (a *Archive) HasFile(archivePath string) bool {
	if a == nil {
		return false
	}
	_, ok := a.entries[normalizeNFC(archivePath)]
	return ok
}

// ReadFile reads and returns the bytes of the entry matching archivePath
// (NFC-normalized, case-sensitive) while enforcing decompression size limits.
func (a *Archive) ReadFile(archivePath string) ([]byte, error) {
	if a == nil {
		return nil, os.ErrClosed
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil, os.ErrClosed
	}
	f := a.entries[normalizeNFC(archivePath)]
	if f == nil {
		return nil, os.ErrNotExist
	}
	if a.zipSrc != nil {
		return a.zipSrc.withFile(func() ([]byte, error) {
			return a.readZipFileLocked(f, MaxEntryBytes)
		})
	}
	return a.readZipFileLocked(f, MaxEntryBytes)
}

// ReadMarkdown reads a Markdown entry as UTF-8 text and normalizes CRLF/CR to LF.
func (a *Archive) ReadMarkdown(archivePath string) (string, error) {
	data, err := a.ReadFile(archivePath)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("%w: %q is not valid UTF-8", ErrZipInvalid, archivePath)
	}
	s := string(data)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s, nil
}

func (a *Archive) readZipFileLocked(f *zip.File, maxBytes int64) ([]byte, error) {
	limit := maxBytes
	if f.UncompressedSize64 < uint64(limit) {
		limit = int64(f.UncompressedSize64)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrZipInvalid, err)
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrZipInvalid, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: entry %q decompressed past size limit", ErrLimitExceeded, f.Name)
	}
	return data, nil
}

// ResolveLink resolves a standard Markdown link destination relative to fromPage
// according to MDZip Specification §9.3 and §5.4.1.
func (a *Archive) ResolveLink(fromPage, rawDest string) ResolvedLink {
	dest := strings.TrimSpace(rawDest)
	if dest == "" {
		return ResolvedLink{Kind: LinkUnresolved}
	}
	// Strip surrounding angle brackets used in CommonMark links (<path with spaces.md>).
	if len(dest) >= 2 && dest[0] == '<' && dest[len(dest)-1] == '>' {
		dest = dest[1 : len(dest)-1]
	}
	if hasURLScheme(dest) {
		return ResolvedLink{Kind: LinkExternal}
	}

	// Step 1: Split at first '#', keeping '?' inside the fragment, then remove '?query'.
	var fragment string
	pathPart := dest
	if hash := strings.IndexByte(pathPart, '#'); hash >= 0 {
		fragment = pathPart[hash+1:]
		pathPart = pathPart[:hash]
	}
	if q := strings.IndexByte(pathPart, '?'); q >= 0 {
		pathPart = pathPart[:q]
	}

	// Step 2: Empty path refers to the referencing document itself.
	if pathPart == "" {
		if a != nil && fromPage != "" {
			if f := a.entries[normalizeNFC(fromPage)]; f != nil {
				return ResolvedLink{
					Kind:     LinkInternal,
					Path:     f.Name,
					Fragment: fragment,
					Found:    true,
				}
			}
		}
		return ResolvedLink{Kind: LinkUnresolved, Fragment: fragment}
	}

	// Step 3: Reject leading '/' or any '\'.
	if strings.HasPrefix(pathPart, "/") || strings.ContainsRune(pathPart, '\\') {
		return ResolvedLink{Kind: LinkUnresolved, Fragment: fragment}
	}

	trailingSlash := strings.HasSuffix(pathPart, "/")
	rawSegs := strings.Split(pathPart, "/")

	// Step 4 & 5: Single UTF-8 percent-decode per segment and apply against base dir.
	var stack []string
	if fromPage != "" {
		baseDir := path.Dir(normalizeNFC(fromPage))
		if baseDir != "." && baseDir != "/" && baseDir != "" {
			stack = strings.Split(baseDir, "/")
		}
	}

	outside := false
	for _, rawSeg := range rawSegs {
		seg, ok := percentDecodeSegment(rawSeg)
		if !ok {
			return ResolvedLink{Kind: LinkUnresolved, Fragment: fragment}
		}
		if seg == "" || seg == "." {
			continue
		}
		if seg == ".." {
			if len(stack) == 0 {
				outside = true
				break
			}
			stack = stack[:len(stack)-1]
			continue
		}
		stack = append(stack, seg)
	}

	// Step 7: Never clamp '..' at archive root.
	if outside {
		return ResolvedLink{Kind: LinkOutsideArchive, Fragment: fragment}
	}

	// Trailing slash or empty path inside archive names a directory, not a file.
	if trailingSlash || len(stack) == 0 {
		return ResolvedLink{Kind: LinkUnresolved, Fragment: fragment}
	}

	// Step 6: Match NFC-normalized path case-sensitively against archive entries.
	nfc := normalizeNFC(strings.Join(stack, "/"))
	if a != nil {
		if f := a.entries[nfc]; f != nil {
			return ResolvedLink{
				Kind:     LinkInternal,
				Path:     f.Name,
				Fragment: fragment,
				Found:    true,
			}
		}
		if near := a.nearMatch(nfc); near != "" {
			return ResolvedLink{
				Kind:      LinkUnresolved,
				Fragment:  fragment,
				NearMatch: near,
			}
		}
	}
	return ResolvedLink{Kind: LinkUnresolved, Fragment: fragment}
}

// percentDecodeSegment decodes a single URL path segment as UTF-8 exactly once
// per Spec §9.3 step 4, rejecting malformed escapes, invalid UTF-8, path
// separators ('/', '\'), and ASCII control characters.
func percentDecodeSegment(seg string) (string, bool) {
	if !strings.ContainsRune(seg, '%') {
		if !utf8.ValidString(seg) || hasForbiddenDecodedByte(seg) {
			return "", false
		}
		return seg, true
	}
	buf := make([]byte, 0, len(seg))
	for i := 0; i < len(seg); i++ {
		if seg[i] != '%' {
			buf = append(buf, seg[i])
			continue
		}
		if i+2 >= len(seg) {
			return "", false
		}
		hi, ok1 := fromHex(seg[i+1])
		lo, ok2 := fromHex(seg[i+2])
		if !ok1 || !ok2 {
			return "", false
		}
		buf = append(buf, hi<<4|lo)
		i += 2
	}
	if !utf8.Valid(buf) {
		return "", false
	}
	out := string(buf)
	if hasForbiddenDecodedByte(out) {
		return "", false
	}
	return out, true
}

func fromHex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

func hasForbiddenDecodedByte(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '/' || c == '\\' || c <= 0x1F || c == 0x7F {
			return true
		}
	}
	return false
}

// PlainText concatenates readable Markdown content from the archive (entry point
// first, followed by remaining pages) up to maxBytes for search and QA indexing.
func (a *Archive) PlainText(maxBytes int) string {
	if a == nil || len(a.pages) == 0 {
		return ""
	}
	if maxBytes <= 0 {
		maxBytes = 2 << 20
	}
	var order []string
	if a.entryPoint != "" {
		order = append(order, a.entryPoint)
	}
	for _, p := range a.pages {
		if p != a.entryPoint {
			order = append(order, p)
		}
	}
	var b strings.Builder
	if a.manifest != nil {
		if a.manifest.Title != "" {
			b.WriteString(a.manifest.Title)
			b.WriteByte('\n')
		}
		if a.manifest.Description != "" {
			b.WriteString(a.manifest.Description)
			b.WriteByte('\n')
		}
	}
	for _, p := range order {
		if b.Len() >= maxBytes {
			break
		}
		md, err := a.ReadMarkdown(p)
		if err != nil {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		rem := maxBytes - b.Len()
		if len(md) > rem {
			md = trimUTF8Bytes(md, rem)
		}
		b.WriteString(md)
	}
	return b.String()
}

func trimUTF8Bytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	n := maxBytes
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
