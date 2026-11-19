package main

import (
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

type webdavClient struct {
	baseURL       *url.URL
	authHeader    string
	root          string
	allowInsecure bool
	knownDirs     map[string]bool
	now           func() time.Time
}

func newWebDAVClient(rawURL, username, password, root string, allowInsecure bool) (*webdavClient, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("请填写 WebDAV 服务器地址")
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return nil, fmt.Errorf("无效的 WebDAV 地址: %q", rawURL)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""

	var auth string
	if username != "" || password != "" {
		auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
	}
	return &webdavClient{
		baseURL:       u,
		authHeader:    auth,
		root:          strings.Trim(root, "/"),
		allowInsecure: allowInsecure,
		knownDirs:     map[string]bool{},
		now:           time.Now,
	}, nil
}

func (c *webdavClient) Name() string { return "WebDAV" }

func (c *webdavClient) headers(extra map[string]string) map[string]string {
	out := make(map[string]string, len(extra)+1)
	if c.authHeader != "" {
		out["Authorization"] = c.authHeader
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func (c *webdavClient) urlFor(parts ...string) string {
	basePath := strings.TrimRight(c.baseURL.Path, "/")
	var segs []string
	if basePath != "" {
		for _, s := range strings.Split(strings.TrimLeft(basePath, "/"), "/") {
			if s != "" {
				segs = append(segs, url.PathEscape(s))
			}
		}
	}
	for _, p := range parts {
		p = strings.Trim(p, "/")
		if p == "" {
			continue
		}
		for _, s := range strings.Split(p, "/") {
			if s != "" {
				segs = append(segs, url.PathEscape(s))
			}
		}
	}
	fullPath := "/" + strings.Join(segs, "/")
	return c.baseURL.Scheme + "://" + c.baseURL.Host + fullPath
}

const propfindBody = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:">
  <D:prop>
    <D:resourcetype/>
    <D:getcontentlength/>
    <D:getlastmodified/>
    <D:getetag/>
  </D:prop>
</D:propfind>`

func (c *webdavClient) Test() error {
	target := c.urlFor("")
	if !strings.HasSuffix(target, "/") {
		target += "/"
	}
	resp, err := sdkDoHTTP("PROPFIND", target, c.headers(map[string]string{
		"Depth":        "0",
		"Content-Type": "application/xml; charset=utf-8",
	}), []byte(propfindBody), c.allowInsecure)
	if err != nil {
		return err
	}
	if resp.Status == http.StatusUnauthorized || resp.Status == http.StatusForbidden {
		return fmt.Errorf("WebDAV 认证失败 (%d)，请检查用户名与应用密码", resp.Status)
	}
	if resp.Status >= 400 {
		return fmt.Errorf("WebDAV 连接失败 (HTTP %d)", resp.Status)
	}
	return nil
}

type davMultistatus struct {
	Responses []davResponse `xml:"response"`
}

type davResponse struct {
	Href      string        `xml:"href"`
	Propstats []davPropstat `xml:"propstat"`
}

type davPropstat struct {
	Prop   davProp `xml:"prop"`
	Status string  `xml:"status"`
}

type davProp struct {
	ResourceType struct {
		Collection *struct{} `xml:"collection"`
	} `xml:"resourcetype"`
	ContentLength string `xml:"getcontentlength"`
	LastModified  string `xml:"getlastmodified"`
	ETag          string `xml:"getetag"`
}

func (c *webdavClient) List(prefix string) ([]RemoteObject, error) {
	spaceBaseRel := joinSlash(c.root, prefix)
	spaceDecodedPrefix := strings.TrimRight(c.baseURL.Path, "/")
	if spaceBaseRel != "" {
		spaceDecodedPrefix += "/" + spaceBaseRel
	}
	spaceDecodedPrefix = strings.TrimRight(spaceDecodedPrefix, "/")

	queue := []string{""}
	seenDirs := map[string]bool{"": true}
	var out []RemoteObject

	for len(queue) > 0 {
		dirRel := queue[0]
		queue = queue[1:]

		reqURL := c.urlFor(c.root, prefix, dirRel)
		if !strings.HasSuffix(reqURL, "/") {
			reqURL += "/"
		}
		resp, err := sdkDoHTTP("PROPFIND", reqURL, c.headers(map[string]string{
			"Depth":        "1",
			"Content-Type": "application/xml; charset=utf-8",
		}), []byte(propfindBody), c.allowInsecure)
		if err != nil {
			return nil, err
		}
		if resp.Status == http.StatusNotFound {
			if dirRel == "" {
				return nil, nil // Space directory not created on remote yet.
			}
			continue
		}
		if resp.Status == http.StatusUnauthorized || resp.Status == http.StatusForbidden {
			return nil, fmt.Errorf("WebDAV 认证或权限失败 (HTTP %d)", resp.Status)
		}
		if resp.Status != http.StatusMultiStatus && resp.Status != http.StatusOK {
			return nil, fmt.Errorf("WebDAV PROPFIND 失败 (HTTP %d)", resp.Status)
		}

		var ms davMultistatus
		if err := xml.Unmarshal(resp.Body, &ms); err != nil {
			return nil, fmt.Errorf("解析 WebDAV 目录失败: %w", err)
		}

		curDirAbs := spaceDecodedPrefix
		if dirRel != "" {
			curDirAbs += "/" + dirRel
		}

		for _, r := range ms.Responses {
			rel, ok := stripDAVHref(r.Href, spaceDecodedPrefix, curDirAbs)
			if !ok || rel == "" {
				continue
			}
			prop, ok := pickDAVProp(r.Propstats)
			if !ok {
				continue
			}
			if prop.ResourceType.Collection != nil {
				if shouldDescendRemoteDir(rel) && !seenDirs[rel] {
					seenDirs[rel] = true
					queue = append(queue, rel)
				}
				continue
			}
			var size int64
			if prop.ContentLength != "" {
				size, _ = strconv.ParseInt(strings.TrimSpace(prop.ContentLength), 10, 64)
			}
			var modUnix int64
			if prop.LastModified != "" {
				if t, err := http.ParseTime(strings.TrimSpace(prop.LastModified)); err == nil {
					modUnix = t.Unix()
				} else if t, err := time.Parse(time.RFC3339, strings.TrimSpace(prop.LastModified)); err == nil {
					modUnix = t.Unix()
				}
			}
			out = append(out, RemoteObject{
				Path:        rel,
				Size:        size,
				ETag:        cleanETag(prop.ETag),
				ModTimeUnix: modUnix,
			})
		}
	}
	return out, nil
}

func shouldDescendRemoteDir(rel string) bool {
	if rel == ".arcmd" || rel == ".arcmd/templates" {
		return true
	}
	if strings.HasPrefix(rel, ".arcmd/templates/") {
		rest := strings.TrimPrefix(rel, ".arcmd/templates/")
		for _, seg := range strings.Split(rest, "/") {
			if strings.HasPrefix(seg, ".") || seg == "node_modules" {
				return false
			}
		}
		return true
	}
	for _, seg := range strings.Split(rel, "/") {
		if strings.HasPrefix(seg, ".") || seg == "node_modules" {
			return false
		}
	}
	return true
}

func stripDAVHref(href, spacePrefix, curDirAbs string) (string, bool) {
	href = strings.TrimSpace(href)
	if href == "" {
		return "", false
	}
	u, err := url.Parse(href)
	if err != nil {
		return "", false
	}
	rawPath := u.EscapedPath()
	if rawPath == "" {
		rawPath = u.Path
	}
	decoded, err := url.PathUnescape(rawPath)
	if err != nil {
		decoded = u.Path
	}
	decoded = strings.TrimRight(path.Clean("/"+strings.TrimLeft(decoded, "/")), "/")
	if decoded == curDirAbs || decoded == spacePrefix {
		return "", false
	}
	if spacePrefix != "" && spacePrefix != "/" {
		if !strings.HasPrefix(decoded, spacePrefix+"/") {
			return "", false
		}
		return strings.TrimPrefix(decoded, spacePrefix+"/"), true
	}
	return strings.TrimPrefix(decoded, "/"), true
}

func pickDAVProp(stats []davPropstat) (davProp, bool) {
	if len(stats) == 0 {
		return davProp{}, false
	}
	for _, s := range stats {
		if s.Status == "" || strings.Contains(s.Status, " 200 ") || strings.HasSuffix(strings.TrimSpace(s.Status), " 200") {
			return s.Prop, true
		}
	}
	return davProp{}, false
}

func (c *webdavClient) ensureDir(prefix, relDir string) error {
	full := joinSlash(c.root, prefix, relDir)
	if full == "" {
		return nil
	}
	parts := strings.Split(full, "/")
	cur := ""
	for _, seg := range parts {
		if seg == "" {
			continue
		}
		cur = joinSlash(cur, seg)
		if c.knownDirs[cur] {
			continue
		}
		dirURL := c.urlFor(cur)
		if !strings.HasSuffix(dirURL, "/") {
			dirURL += "/"
		}
		resp, err := sdkDoHTTP("PROPFIND", dirURL, c.headers(map[string]string{
			"Depth":        "0",
			"Content-Type": "application/xml; charset=utf-8",
		}), []byte(propfindBody), c.allowInsecure)
		if err != nil {
			return err
		}
		if resp.Status == http.StatusMultiStatus || resp.Status == http.StatusOK {
			c.knownDirs[cur] = true
			continue
		}
		if resp.Status != http.StatusNotFound {
			return fmt.Errorf("检查 WebDAV 目录 %s 失败 (HTTP %d)", cur, resp.Status)
		}
		mkResp, err := sdkDoHTTP("MKCOL", dirURL, c.headers(nil), nil, c.allowInsecure)
		if err != nil {
			return err
		}
		if mkResp.Status >= 400 && mkResp.Status != http.StatusMethodNotAllowed {
			return fmt.Errorf("创建 WebDAV 目录 %s 失败 (HTTP %d)", cur, mkResp.Status)
		}
		c.knownDirs[cur] = true
	}
	return nil
}

func (c *webdavClient) Get(prefix, rel string) ([]byte, RemoteObject, error) {
	resp, err := sdkDoHTTP(http.MethodGet, c.urlFor(c.root, prefix, rel), c.headers(nil), nil, c.allowInsecure)
	if err != nil {
		return nil, RemoteObject{}, err
	}
	if resp.Status >= 400 {
		return nil, RemoteObject{}, fmt.Errorf("WebDAV 下载 %s 失败 (HTTP %d)", rel, resp.Status)
	}
	var modUnix int64
	if lm := headerIgnoreCase(resp.Headers, "Last-Modified"); lm != "" {
		if t, err := http.ParseTime(lm); err == nil {
			modUnix = t.Unix()
		}
	}
	return resp.Body, RemoteObject{
		Path:        rel,
		Size:        int64(len(resp.Body)),
		ETag:        cleanETag(headerIgnoreCase(resp.Headers, "ETag")),
		ModTimeUnix: modUnix,
	}, nil
}

func (c *webdavClient) Put(prefix, rel string, data []byte) (RemoteObject, error) {
	dir := path.Dir(rel)
	if dir == "." {
		dir = ""
	}
	if err := c.ensureDir(prefix, dir); err != nil {
		return RemoteObject{}, err
	}
	target := c.urlFor(c.root, prefix, rel)
	resp, err := sdkDoHTTP(http.MethodPut, target, c.headers(map[string]string{
		"Content-Type": mimeForPath(rel),
	}), data, c.allowInsecure)
	if err != nil {
		return RemoteObject{}, err
	}
	if resp.Status >= 400 {
		return RemoteObject{}, fmt.Errorf("WebDAV 上传 %s 失败 (HTTP %d)", rel, resp.Status)
	}
	etag := cleanETag(headerIgnoreCase(resp.Headers, "ETag"))
	var modUnix int64
	if lm := headerIgnoreCase(resp.Headers, "Last-Modified"); lm != "" {
		if t, err := http.ParseTime(lm); err == nil {
			modUnix = t.Unix()
		}
	}
	if etag == "" {
		// Some WebDAV servers omit ETag on PUT; probe Depth: 0 so baseline matches List().
		if meta, ok := c.statFile(target); ok {
			etag = meta.ETag
			if meta.ModTimeUnix != 0 {
				modUnix = meta.ModTimeUnix
			}
		}
	}
	if modUnix == 0 {
		modUnix = c.now().Unix()
	}
	return RemoteObject{
		Path:        rel,
		Size:        int64(len(data)),
		ETag:        etag,
		ModTimeUnix: modUnix,
	}, nil
}

func (c *webdavClient) statFile(target string) (RemoteObject, bool) {
	resp, err := sdkDoHTTP("PROPFIND", target, c.headers(map[string]string{
		"Depth":        "0",
		"Content-Type": "application/xml; charset=utf-8",
	}), []byte(propfindBody), c.allowInsecure)
	if err != nil || (resp.Status != http.StatusMultiStatus && resp.Status != http.StatusOK) {
		return RemoteObject{}, false
	}
	var ms davMultistatus
	if xml.Unmarshal(resp.Body, &ms) != nil || len(ms.Responses) == 0 {
		return RemoteObject{}, false
	}
	prop, ok := pickDAVProp(ms.Responses[0].Propstats)
	if !ok {
		return RemoteObject{}, false
	}
	var modUnix int64
	if prop.LastModified != "" {
		if t, err := http.ParseTime(strings.TrimSpace(prop.LastModified)); err == nil {
			modUnix = t.Unix()
		}
	}
	return RemoteObject{
		ETag:        cleanETag(prop.ETag),
		ModTimeUnix: modUnix,
	}, true
}

func (c *webdavClient) Delete(prefix, rel string) error {
	resp, err := sdkDoHTTP(http.MethodDelete, c.urlFor(c.root, prefix, rel), c.headers(nil), nil, c.allowInsecure)
	if err != nil {
		return err
	}
	if resp.Status == http.StatusNotFound {
		return nil
	}
	if resp.Status >= 400 {
		return fmt.Errorf("WebDAV 删除 %s 失败 (HTTP %d)", rel, resp.Status)
	}
	return nil
}
