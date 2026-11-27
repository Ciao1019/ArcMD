package clipper

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
)

// MaxAssetBytes caps a single clipped image upload at 10 MiB.
const MaxAssetBytes = 10 << 20

// ValidateImage inspects raw image bytes by magic header (never trusting the
// extension or MIME header alone), enforces size limits, rejects non-raster
// formats such as SVG/HTML/executables, and returns a normalized filename with
// safe extension suitable for writing into a Space's attachments folder.
func ValidateImage(filename, mimeHint string, data []byte) (ext string, safeName string, err error) {
	if len(data) == 0 {
		return "", "", errors.New("clipper: image data is empty")
	}
	if len(data) > MaxAssetBytes {
		return "", "", fmt.Errorf("clipper: image exceeds maximum size (%d > %d bytes)", len(data), MaxAssetBytes)
	}
	ext = sniffImageExt(data)
	if ext == "" {
		return "", "", fmt.Errorf("clipper: unsupported image format (mime hint %q)", strings.TrimSpace(mimeHint))
	}
	base := filepath.Base(strings.TrimSpace(strings.ReplaceAll(filename, `\`, "/")))
	if base == "" || base == "." || base == "/" {
		base = "clip-image"
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	stem = SafeDocTitle(stem)
	if stem == "" || stem == "Untitled Clip" {
		stem = "clip-image"
	}
	if runes := []rune(stem); len(runes) > 64 {
		stem = strings.Trim(string(runes[:64]), " .-_")
	}
	if stem == "" {
		stem = "clip-image"
	}
	return ext, stem + ext, nil
}

func sniffImageExt(data []byte) string {
	// Check WebP RIFF container explicitly (http.DetectContentType handles it too,
	// but doing both guarantees consistency across Go versions).
	if len(data) >= 12 && bytes.Equal(data[0:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
		return ".webp"
	}
	if len(data) >= 2 && data[0] == 'B' && data[1] == 'M' {
		return ".bmp"
	}
	ct := http.DetectContentType(data)
	switch ct {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	default:
		return ""
	}
}
