package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/egoist/mygo/ui"
)

const maxRemoteImageBytes = 20 << 20

type remoteImageState struct {
	mu      sync.Mutex
	loading map[string]bool
	failed  map[string]bool
}

func isRemoteImageURL(raw string) bool {
	u, err := neturl.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) && u.Host != ""
}

func remoteImageExt(rawURL string) string {
	u, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err == nil {
		ext := strings.ToLower(path.Ext(u.Path))
		if ext != "" && isImage("x"+ext) {
			return ext
		}
	}
	return ".img"
}

// remoteImageCachePath keeps fetched remote image bytes under ConfigDir/cache,
// strictly outside every Space folder so S3/WebDAV cloud sync never treats
// cached CDN images as local attachments.
func (a *App) remoteImageCachePath(rawURL string) string {
	if a.cfg.ConfigDir == "" || !isRemoteImageURL(rawURL) {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(rawURL)))
	return filepath.Join(a.cfg.ConfigDir, "cache", "remote-images", hex.EncodeToString(sum[:])+remoteImageExt(rawURL))
}

func (a *App) cacheRemoteImageBytes(rawURL string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("empty image bytes")
	}
	dst := a.remoteImageCachePath(rawURL)
	if dst == "" {
		return "", errors.New("remote image cache unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := fsutil.WriteFileAtomic(dst, data, 0o600); err != nil {
		return "", err
	}
	a.remoteImages.mu.Lock()
	if a.remoteImages.failed != nil {
		delete(a.remoteImages.failed, rawURL)
	}
	a.remoteImages.mu.Unlock()
	return dst, nil
}

func (a *App) remoteImageResource(d *document, rawURL string) editor.ImageResource {
	if cachePath := a.remoteImageCachePath(rawURL); cachePath != "" {
		if info, err := os.Stat(cachePath); err == nil && info.Mode().IsRegular() {
			res := d.imageResourceFrom(cachePath, rawURL, a.asyncPost())
			res.Local = false
			return res
		}
	}

	post := a.asyncPost()
	a.remoteImages.mu.Lock()
	if a.remoteImages.failed[rawURL] {
		a.remoteImages.mu.Unlock()
		return editor.ImageResource{Problem: editor.ImageRemote}
	}
	if post == nil {
		a.remoteImages.mu.Unlock()
		return editor.ImageResource{Problem: editor.ImageRemote}
	}
	if a.remoteImages.loading[rawURL] {
		a.remoteImages.mu.Unlock()
		return editor.ImageResource{Loading: true}
	}
	if a.remoteImages.loading == nil {
		a.remoteImages.loading = map[string]bool{}
	}
	a.remoteImages.loading[rawURL] = true
	a.remoteImages.mu.Unlock()

	client := a.pluginsUI.httpClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	go func() {
		fetchErr := a.fetchAndCacheRemoteImage(client, rawURL)
		a.remoteImages.mu.Lock()
		delete(a.remoteImages.loading, rawURL)
		if fetchErr != nil {
			if a.remoteImages.failed == nil {
				a.remoteImages.failed = map[string]bool{}
			}
			a.remoteImages.failed[rawURL] = true
		}
		a.remoteImages.mu.Unlock()

		post(func() {
			if a.imgResolved != nil {
				delete(a.imgResolved, imageKey{doc: d, url: rawURL})
			}
			if d != nil && !d.closed.Load() {
				if d.imgCache != nil {
					delete(d.imgCache, rawURL)
				}
				if d.ed != nil {
					d.ed.InvalidateImage(rawURL)
				}
			}
		})
	}()

	return editor.ImageResource{Loading: true}
}

func (a *App) fetchAndCacheRemoteImage(client *http.Client, rawURL string) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteImageBytes+1))
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return errors.New("empty response")
	}
	if len(data) > maxRemoteImageBytes {
		return errors.New("remote image exceeds size limit")
	}
	if _, err := ui.DecodeBitmap(data); err != nil {
		return err
	}
	_, err = a.cacheRemoteImageBytes(rawURL, data)
	return err
}
