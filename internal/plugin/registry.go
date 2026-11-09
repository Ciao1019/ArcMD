package plugin

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Index is the marketplace's registry: a static JSON file listing every
// published plugin's latest release, served from a Git repository so that
// publishing is a reviewed pull request. See docs/plugins.md.
type Index struct {
	Version int       `json:"version"`
	Plugins []Listing `json:"plugins"`
}

// Listing is a plugin's entry in the index.
type Listing struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Version     string       `json:"version"`
	Description string       `json:"description,omitempty"`
	Author      string       `json:"author,omitempty"`
	Homepage    string       `json:"homepage,omitempty"`
	Tags        []string     `json:"tags,omitempty"`
	Permissions []Permission `json:"permissions,omitempty"`
	// Download is the URL of the release's zip, holding the manifest at
	// its root, and SHA256 the hex digest of that zip.
	Download string `json:"download"`
	SHA256   string `json:"sha256"`
}

// MaxPackageSize bounds a plugin's zip, and what it unpacks to.
const MaxPackageSize = 64 << 20

// FetchIndex downloads and decodes the index at url.
func FetchIndex(ctx context.Context, client *http.Client, url string) (*Index, error) {
	b, err := get(ctx, client, url, 8<<20)
	if err != nil {
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(b, &idx); err != nil {
		return nil, fmt.Errorf("plugin: index: %w", err)
	}
	return &idx, nil
}

// Install downloads the listing's zip, checks its digest and manifest, and
// unpacks it into the plugins folder, replacing an older version.
func (m *Manager) Install(ctx context.Context, client *http.Client, l Listing) (*Installed, error) {
	if !idPattern.MatchString(l.ID) {
		return nil, fmt.Errorf("plugin: invalid id %q", l.ID)
	}
	b, err := get(ctx, client, l.Download, MaxPackageSize)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), l.SHA256) {
		return nil, fmt.Errorf("plugin: %s: the download's SHA-256 does not match the index", l.ID)
	}
	return m.installZip(b, l)
}

func (m *Manager) installZip(b []byte, l Listing) (*Installed, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("plugin: %s: %w", l.ID, err)
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return nil, err
	}
	staging, err := os.MkdirTemp(m.dir, ".install-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)
	var total int64
	for _, f := range zr.File {
		name := path.Clean(f.Name)
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, `\`) {
			return nil, fmt.Errorf("plugin: %s: unsafe path %q in the package", l.ID, f.Name)
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("plugin: %s: symbolic link %q in the package", l.ID, f.Name)
		}
		dst := filepath.Join(staging, filepath.FromSlash(name))
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return nil, err
			}
			continue
		}
		total += int64(f.UncompressedSize64)
		if total > MaxPackageSize {
			return nil, fmt.Errorf("plugin: %s: the package unpacks to more than %d bytes", l.ID, MaxPackageSize)
		}
		if err := unpack(f, dst); err != nil {
			return nil, err
		}
	}
	mb, err := os.ReadFile(filepath.Join(staging, ManifestFile))
	if err != nil {
		return nil, fmt.Errorf("plugin: %s: the package has no %s at its root", l.ID, ManifestFile)
	}
	man, err := ParseManifest(mb)
	if err != nil {
		return nil, err
	}
	if man.ID != l.ID || man.Version != l.Version {
		return nil, fmt.Errorf("plugin: the package is %s %s, the index says %s %s", man.ID, man.Version, l.ID, l.Version)
	}
	dir := filepath.Join(m.dir, l.ID)
	old := dir + ".old"
	os.RemoveAll(old)
	if _, err := os.Stat(dir); err == nil {
		if err := os.Rename(dir, old); err != nil {
			return nil, err
		}
	}
	if err := os.Rename(staging, dir); err != nil {
		os.Rename(old, dir)
		return nil, err
	}
	os.RemoveAll(old)
	enabled := true
	for _, d := range m.readSettings().Disabled {
		enabled = enabled && d != l.ID
	}
	return &Installed{Manifest: man, Dir: dir, Enabled: enabled}, nil
}

func unpack(f *zip.File, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	mode := os.FileMode(0o644)
	if f.Mode()&0o111 != 0 {
		mode = 0o755 // a process plugin's executable
	}
	w, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, io.LimitReader(r, MaxPackageSize))
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	return err
}

func get(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://127.0.0.1") {
		return nil, fmt.Errorf("plugin: %q is not an https URL", url)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("plugin: GET %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("plugin: " + url + " is too large")
	}
	return b, nil
}
