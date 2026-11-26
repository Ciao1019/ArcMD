// Package mdz reads and writes MDZip (.mdz) archives according to MDZip
// Specification v1.2.0 in pure Go without cgo or UI dependencies.
package mdz

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	// SpecName is the MDZip specification identifier written to manifest.json.
	SpecName = "mdzip-spec"
	// SpecVersion is the MDZip specification version produced by this package.
	SpecVersion = "1.2.0"
	// SupportedMajor is the highest MDZip spec major version supported by this consumer.
	SupportedMajor = 1
	// MIMEType is the proposed media type for MDZip archives (Spec §10).
	MIMEType = "application/vnd.mdzip"
	// ManifestPath is the archive-root path of the optional manifest file.
	ManifestPath = "manifest.json"
	// ModeDocument represents a single logical publication (default mode).
	ModeDocument = "document"
	// ModeProject represents a collection of independent Markdown documents.
	ModeProject = "project"
)

// Resource limits enforced when opening archives to guard against ZIP bombs (Spec §13.1).
const (
	MaxEntries          = 4096
	MaxEntryBytes       = 32 << 20  // 32 MiB per decompressed file
	MaxTotalBytes       = 128 << 20 // 128 MiB total decompressed archive size
	MaxCompressionRatio = 200
	minRatioCheckBytes  = 1 << 20 // check compression ratio once an entry exceeds 1 MiB
)

// Conformance error categories aligned with MDZip Specification §16.5.
var (
	ErrZipInvalid           = errors.New("mdz: invalid ZIP archive or unsupported compression method")
	ErrZipEncrypted         = errors.New("mdz: encrypted ZIP entries are not supported")
	ErrPathInvalid          = errors.New("mdz: archive contains an invalid entry path")
	ErrPathConflict         = errors.New("mdz: archive has a file/directory path conflict")
	ErrDuplicatePath        = errors.New("mdz: archive contains duplicate paths after NFC normalization")
	ErrLimitExceeded        = errors.New("mdz: archive exceeds size or entry safety limits")
	ErrManifestInvalid      = errors.New("mdz: invalid manifest.json")
	ErrVersionUnsupported   = errors.New("mdz: unsupported MDZip specification major version")
	ErrModeUnsupported      = errors.New("mdz: unsupported or unrecognized archive mode")
	ErrEntryPointInvalid    = errors.New("mdz: manifest entryPoint is not a valid Markdown path")
	ErrEntryPointMissing    = errors.New("mdz: manifest entryPoint does not exist in archive")
	ErrEntryPointUnresolved = errors.New("mdz: no unambiguous entry point could be determined")
)

// LinkKind classifies the outcome of resolving a Markdown link inside an archive (Spec §9.3).
type LinkKind uint8

const (
	LinkUnresolved LinkKind = iota
	LinkInternal
	LinkExternal
	LinkOutsideArchive
)

// ResolvedLink describes the result of resolving a Markdown link destination
// relative to a page inside the archive (Spec §9.3).
type ResolvedLink struct {
	Kind      LinkKind
	Path      string // stored archive path when Kind == LinkInternal and Found is true
	Fragment  string // text after first '#', preserving any '?' inside it
	Found     bool
	NearMatch string // case-only near match path when lookup failed only by case (Spec §5.4.1)
}

// EntryInfo describes one file entry stored inside an MDZip archive.
type EntryInfo struct {
	Path             string
	UncompressedSize uint64
	CompressedSize   uint64
	Method           uint16
}

// IsMarkdownPath reports whether p has a .md or .markdown extension, compared
// ASCII case-insensitively per Spec §5.4.1 and §7.3.
func IsMarkdownPath(p string) bool {
	i := strings.LastIndexByte(p, '.')
	if i < 0 {
		return false
	}
	// Make sure '.' is in the final segment, not a parent directory.
	if slash := strings.LastIndexByte(p, '/'); slash > i {
		return false
	}
	ext := asciiLower(p[i:])
	return ext == ".md" || ext == ".markdown"
}

// isSupportFile reports whether name is one of the five closed root-level
// support files defined in Spec §5.5 (ASCII case-insensitive).
func isSupportFile(rootFile string) bool {
	if strings.ContainsRune(rootFile, '/') {
		return false
	}
	i := strings.LastIndexByte(rootFile, '.')
	if i <= 0 {
		return false
	}
	ext := asciiLower(rootFile[i:])
	if ext != ".md" && ext != ".markdown" {
		return false
	}
	switch asciiLower(rootFile[:i]) {
	case "readme", "agents", "license", "changelog", "contributing":
		return true
	default:
		return false
	}
}

func asciiLower(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			if b.Len() == 0 {
				b.Grow(len(s))
				b.WriteString(s[:i])
			}
			b.WriteByte(c + ('a' - 'A'))
		} else if b.Len() > 0 {
			b.WriteByte(c)
		}
	}
	if b.Len() == 0 {
		return s
	}
	return b.String()
}

func foldCase(s string) string {
	return cases.Fold().String(norm.NFC.String(s))
}

func normalizeNFC(s string) string {
	return norm.NFC.String(s)
}

// ValidateEntryPath checks whether p conforms to MDZip Spec §5.4 path constraints.
// Directory entries end with '/'; set allowDir=true when validating raw ZIP entry names.
func ValidateEntryPath(p string, allowDir bool) error {
	if p == "" {
		return fmt.Errorf("%w: empty path", ErrPathInvalid)
	}
	if !utf8.ValidString(p) {
		return fmt.Errorf("%w: path is not valid UTF-8", ErrPathInvalid)
	}
	if strings.HasPrefix(p, "/") {
		return fmt.Errorf("%w: leading slash in %q", ErrPathInvalid, p)
	}
	isDir := strings.HasSuffix(p, "/")
	if isDir && !allowDir {
		return fmt.Errorf("%w: trailing slash in file path %q", ErrPathInvalid, p)
	}
	trimmed := p
	if isDir {
		trimmed = strings.TrimSuffix(p, "/")
		if trimmed == "" {
			return fmt.Errorf("%w: root directory entry %q", ErrPathInvalid, p)
		}
	}
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		if c <= 0x1F || c == 0x7F {
			return fmt.Errorf("%w: control character in %q", ErrPathInvalid, p)
		}
		switch c {
		case '\\', ':', '*', '?', '"', '<', '>', '|':
			return fmt.Errorf("%w: reserved character %q in %q", ErrPathInvalid, c, p)
		}
	}
	segs := strings.Split(trimmed, "/")
	for _, seg := range segs {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("%w: invalid segment %q in %q", ErrPathInvalid, seg, p)
		}
	}
	return nil
}

// cleanSingleLeadingDotSlash strips at most one leading "./" per Spec §5.5 and §6.2 (cover).
func cleanSingleLeadingDotSlash(p string) string {
	return strings.TrimPrefix(p, "./")
}

// hasURLScheme reports whether dest begins with an RFC 3986 URL scheme such as "https:" or "mailto:".
func hasURLScheme(dest string) bool {
	for i := 0; i < len(dest); i++ {
		c := dest[i]
		if c == ':' {
			return i > 0
		}
		if c == '/' || c == '?' || c == '#' {
			return false
		}
		if i == 0 {
			if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
				return false
			}
		} else {
			if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.') {
				return false
			}
		}
	}
	return false
}
