package main

import (
	"path"
	"strings"
)

// RemoteObject describes one file on the remote S3 or WebDAV storage.
type RemoteObject struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	ETag        string `json:"etag,omitempty"`
	ModTimeUnix int64  `json:"modTimeUnix,omitempty"`
}

// RemoteBackend abstracts S3 and WebDAV operations under a Space prefix.
type RemoteBackend interface {
	Name() string
	Test() error
	List(prefix string) ([]RemoteObject, error)
	Get(prefix, rel string) ([]byte, RemoteObject, error)
	Put(prefix, rel string, data []byte) (RemoteObject, error)
	Delete(prefix, rel string) error
}

func cleanETag(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "W/")
	v = strings.TrimPrefix(v, "w/")
	return strings.Trim(v, `"`)
}

func mimeForPath(rel string) string {
	low := strings.ToLower(rel)
	if strings.HasSuffix(low, ".excalidraw.svg") {
		return "image/svg+xml"
	}
	switch strings.ToLower(path.Ext(low)) {
	case ".md", ".markdown", ".mdx":
		return "text/markdown; charset=utf-8"
	case ".arcmd", ".excalidraw", ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".txt":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func joinSlash(parts ...string) string {
	var clean []string
	for _, p := range parts {
		p = strings.Trim(p, "/")
		if p != "" {
			clean = append(clean, p)
		}
	}
	return strings.Join(clean, "/")
}
