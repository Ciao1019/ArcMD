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
		if ext == ".svg" || (ext != "" && isImage("x"+ext)) {
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
	return a.remoteImageResourceWithScheme(d, rawURL, false)
}

func (a *App) remoteImageResourceWithScheme(d *document, rawURL string, httpsOnly bool) editor.ImageResource {
	if httpsOnly && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(rawURL)), "https://") {
		return editor.ImageResource{Problem: editor.ImageRemote}
	}
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

	baseClient := a.pluginsUI.httpClient
	if baseClient == nil {
		baseClient = &http.Client{Timeout: 15 * time.Second}
	}
	clientCopy := *baseClient
	prevCheck := baseClient.CheckRedirect
	clientCopy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if req.URL == nil {
			return errors.New("invalid redirect URL")
		}
		scheme := strings.ToLower(req.URL.Scheme)
		if httpsOnly {
			if scheme != "https" {
				return errors.New("disallowed redirect scheme")
			}
		} else if scheme != "http" && scheme != "https" {
			return errors.New("disallowed redirect scheme")
		}
		if prevCheck != nil {
			return prevCheck(req, via)
		}
		return nil
	}

	go func() {
		fetchErr := a.fetchAndCacheRemoteImageWithScheme(&clientCopy, rawURL, httpsOnly)
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
			invalidateDoc := func(target *document) {
				if target == nil || target.closed.Load() {
					return
				}
				if a.imgResolved != nil {
					delete(a.imgResolved, imageKey{doc: target, url: rawURL})
				}
				if target.imgCache != nil {
					delete(target.imgCache, rawURL)
				}
				target.invalidateImage(rawURL)
			}
			invalidateDoc(d)
			for _, rd := range a.residentDocuments() {
				if rd != d {
					invalidateDoc(rd)
				}
			}
		})
	}()

	return editor.ImageResource{Loading: true}
}

func (a *App) fetchAndCacheRemoteImage(client *http.Client, rawURL string) error {
	return a.fetchAndCacheRemoteImageWithScheme(client, rawURL, false)
}

func (a *App) fetchAndCacheRemoteImageWithScheme(client *http.Client, rawURL string, httpsOnly bool) error {
	u, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return errors.New("invalid remote image URL")
	}
	scheme := strings.ToLower(u.Scheme)
	if httpsOnly {
		if scheme != "https" {
			return errors.New("disallowed remote image scheme")
		}
	} else if scheme != "http" && scheme != "https" {
		return errors.New("disallowed remote image scheme")
	}
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
	if bmp, err := ui.DecodeBitmap(data); err != nil {
		if !looksLikeSVG(data) {
			return err
		}
		svg, svgErr := ui.ParseSVG(data)
		if svgErr != nil {
			return err
		}
		if !editor.SVGImageResource(svg, false).HasValidSVG() {
			return errors.New("invalid SVG dimensions")
		}
	} else if w, h := bmp.Size(); w <= 0 || h <= 0 {
		return errors.New("invalid bitmap dimensions")
	}
	_, err = a.cacheRemoteImageBytes(rawURL, data)
	return err
}
