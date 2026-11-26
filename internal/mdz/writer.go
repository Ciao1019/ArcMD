package mdz

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// File represents one file to store inside an .mdz archive.
type File struct {
	Path string
	Data []byte
}

// WriteOptions configures creation of a conforming MDZip v1.2.0 archive (Spec §14.1).
type WriteOptions struct {
	Manifest Manifest // Spec.Version and EntryPoint are filled in if omitted
	Files    []File   // Markdown and asset files; manifest.json is generated automatically
	ModTime  time.Time
}

// Build serializes a conforming MDZip v1.2.0 archive and returns the ZIP bytes.
func Build(opts WriteOptions) ([]byte, error) {
	var buf bytes.Buffer
	if err := Write(&buf, opts); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Write writes a conforming MDZip v1.2.0 archive to w (Spec §14.1).
func Write(w io.Writer, opts WriteOptions) error {
	now := opts.ModTime.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}

	m := opts.Manifest
	if m.Spec == nil {
		m.Spec = &SpecInfo{Name: SpecName, Version: SpecVersion}
	} else {
		cp := *m.Spec
		if cp.Name == "" {
			cp.Name = SpecName
		}
		if cp.Version == "" {
			cp.Version = SpecVersion
		}
		m.Spec = &cp
	}
	if _, _, _, ok := parseSemver(m.Spec.Version); !ok {
		return fmt.Errorf("%w: invalid spec.version %q", ErrManifestInvalid, m.Spec.Version)
	}
	if m.Mode == "" {
		m.Mode = ModeDocument
	}
	if m.Mode != ModeDocument && m.Mode != ModeProject {
		return fmt.Errorf("%w: %q", ErrModeUnsupported, m.Mode)
	}
	if m.EntryPoint == "" {
		m.EntryPoint = "index.md"
	}
	m.EntryPoint = normalizeNFC(cleanSingleLeadingDotSlash(m.EntryPoint))
	if err := ValidateEntryPath(m.EntryPoint, false); err != nil {
		return fmt.Errorf("%w: %v", ErrEntryPointInvalid, err)
	}
	if !IsMarkdownPath(m.EntryPoint) {
		return fmt.Errorf("%w: %q is not a Markdown file", ErrEntryPointInvalid, m.EntryPoint)
	}
	if m.Cover != "" {
		m.Cover = normalizeNFC(cleanSingleLeadingDotSlash(m.Cover))
		if err := ValidateEntryPath(m.Cover, false); err != nil {
			return fmt.Errorf("%w: invalid cover %q", ErrPathInvalid, m.Cover)
		}
	}
	if m.Created == nil {
		m.Created = &Timestamp{When: now.Format(time.RFC3339)}
	}
	if m.Modified == nil {
		m.Modified = &Timestamp{When: now.Format(time.RFC3339)}
	}

	// Validate all file entries before writing any ZIP bytes.
	seenNFC := make(map[string]struct{}, len(opts.Files)+1)
	dirPrefixes := make(map[string]struct{})
	seenNFC[ManifestPath] = struct{}{}

	normalized := make([]File, 0, len(opts.Files))
	hasEntryPoint := false
	hasCover := m.Cover == ""

	for _, f := range opts.Files {
		nfc := normalizeNFC(f.Path)
		if err := ValidateEntryPath(nfc, false); err != nil {
			return err
		}
		if nfc == ManifestPath {
			return fmt.Errorf("%w: manifest.json is generated automatically", ErrDuplicatePath)
		}
		if _, dup := seenNFC[nfc]; dup {
			return fmt.Errorf("%w: duplicate path %q", ErrDuplicatePath, nfc)
		}
		seenNFC[nfc] = struct{}{}
		for idx := strings.IndexByte(nfc, '/'); idx >= 0; {
			dirPrefixes[nfc[:idx]] = struct{}{}
			next := strings.IndexByte(nfc[idx+1:], '/')
			if next < 0 {
				break
			}
			idx += 1 + next
		}

		data := f.Data
		if IsMarkdownPath(nfc) {
			if !utf8.Valid(data) {
				return fmt.Errorf("%w: Markdown file %q is not valid UTF-8", ErrZipInvalid, nfc)
			}
			// Spec §12.2 / §14.1: write LF line endings in Markdown files we create.
			data = normalizeLFBytes(data)
		}
		if nfc == m.EntryPoint {
			hasEntryPoint = true
		}
		if m.Cover != "" && nfc == m.Cover {
			hasCover = true
		}
		normalized = append(normalized, File{Path: nfc, Data: data})
	}

	for nfc := range seenNFC {
		if _, asDir := dirPrefixes[nfc]; asDir {
			return fmt.Errorf("%w: file %q conflicts with directory", ErrPathConflict, nfc)
		}
	}
	if !hasEntryPoint {
		return fmt.Errorf("%w: %q not found in files", ErrEntryPointMissing, m.EntryPoint)
	}
	if !hasCover {
		// Drop missing cover rather than emitting a dangling manifest reference.
		m.Cover = ""
	}

	// Serialize manifest.json as UTF-8 JSON with LF line endings.
	manifestJSON, err := json.MarshalIndent(&m, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}
	manifestJSON = append(normalizeLFBytes(manifestJSON), '\n')

	// Order entries deterministically: manifest.json, entryPoint, then remaining files sorted by path.
	slices.SortFunc(normalized, func(a, b File) int {
		if a.Path == m.EntryPoint {
			return -1
		}
		if b.Path == m.EntryPoint {
			return 1
		}
		return strings.Compare(a.Path, b.Path)
	})

	zw := zip.NewWriter(w)
	writeOne := func(entryPath string, data []byte) error {
		hdr := &zip.FileHeader{
			Name:     entryPath,
			Method:   compressionMethodFor(entryPath),
			Modified: now,
		}
		hdr.SetMode(0o644)
		ew, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		_, err = ew.Write(data)
		return err
	}

	if err := writeOne(ManifestPath, manifestJSON); err != nil {
		_ = zw.Close()
		return err
	}
	for _, f := range normalized {
		if err := writeOne(f.Path, f.Data); err != nil {
			_ = zw.Close()
			return err
		}
	}
	return zw.Close()
}

func normalizeLFBytes(b []byte) []byte {
	if !bytes.ContainsRune(b, '\r') {
		return b
	}
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(b, []byte("\r"), []byte("\n"))
}

// compressionMethodFor chooses Store (0) for already-compressed binary formats
// and Deflate (8) for compressible text/vector formats (Spec §4).
func compressionMethodFor(entryPath string) uint16 {
	switch asciiLower(path.Ext(entryPath)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".heic", ".heif",
		".zip", ".gz", ".br", ".zst", ".xz", ".7z", ".epub", ".mdz",
		".mp3", ".mp4", ".m4a", ".webm", ".ogg", ".woff", ".woff2", ".pdf":
		return zip.Store
	default:
		return zip.Deflate
	}
}
