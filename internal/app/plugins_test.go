package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/plugin"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo/ui"
)

const testCloudSyncManifestJSON = `{
  "id": "dev.arcmd.cloud-sync",
  "name": "云端同步",
  "version": "0.1.0",
  "description": "通过 S3（兼容 R2 / OSS / COS / MinIO）或 WebDAV（坚果云 / Nextcloud / Alist）双向增量同步空间文档与附件。",
  "author": "ArcMD",
  "homepage": "https://github.com/Ciao1019/arcmd-plugins",
  "license": "MIT",
  "icon": "cloud",
  "engine": "0.2.3",
  "runtime": "wasm",
  "main": "plugin.wasm",
  "permissions": ["space:read", "space:write", "network", "secrets", "ui:panel"],
  "contributes": {
    "commands": [
      {"id": "sync-now", "title": "云端同步：立即同步", "primary": true, "inSettings": true},
      {"id": "test-connection", "title": "云端同步：测试连接", "inSettings": true},
      {"id": "sync-status", "title": "云端同步：查看同步记录"}
    ],
    "panels": [
      {"id": "status", "title": "云端同步", "icon": "☁"}
    ],
    "settings": [
      {"id": "provider", "title": "同步方式", "group": "连接设置", "type": "segmented", "default": "s3", "options": [{"value": "s3", "label": "S3 对象存储"}, {"value": "webdav", "label": "WebDAV"}]},
      {"id": "s3Endpoint", "title": "Endpoint", "description": "S3 兼容服务地址", "group": "连接设置", "type": "string", "placeholder": "https://...r2.cloudflarestorage.com", "when": {"field": "provider", "equals": "s3"}, "presets": [{"name": "Cloudflare R2", "values": {"s3Endpoint": "https://<accountid>.r2.cloudflarestorage.com", "s3Region": "auto", "s3PathStyle": false}}, {"name": "AWS S3", "values": {"s3Endpoint": "https://s3.us-east-1.amazonaws.com", "s3Region": "us-east-1", "s3PathStyle": false}}]},
      {"id": "s3Region", "title": "Region", "group": "连接设置", "type": "string", "default": "auto", "when": {"field": "provider", "equals": "s3"}},
      {"id": "s3Bucket", "title": "Bucket", "group": "连接设置", "type": "string", "when": {"field": "provider", "equals": "s3"}},
      {"id": "s3AccessKey", "title": "Access Key ID", "group": "连接设置", "type": "string", "when": {"field": "provider", "equals": "s3"}},
      {"id": "s3SecretKey", "title": "Secret Access Key", "description": "保存在本机凭据目录，不进空间文件夹", "group": "连接设置", "type": "secret", "when": {"field": "provider", "equals": "s3"}},
      {"id": "s3Prefix", "title": "远端目录前缀", "group": "连接设置", "type": "string", "default": "ArcMD", "when": {"field": "provider", "equals": "s3"}},
      {"id": "s3PathStyle", "title": "使用 Path-Style 路径模式", "group": "连接设置", "type": "bool", "default": false, "when": {"field": "provider", "equals": "s3"}},
      {"id": "webdavURL", "title": "服务器地址", "description": "WebDAV 服务根地址", "group": "连接设置", "type": "string", "when": {"field": "provider", "equals": "webdav"}, "presets": [{"name": "坚果云", "values": {"webdavURL": "https://dav.jianguoyun.com/dav/"}}]},
      {"id": "webdavUsername", "title": "用户名", "group": "连接设置", "type": "string", "when": {"field": "provider", "equals": "webdav"}},
      {"id": "webdavPassword", "title": "密码 / 应用密码", "description": "坚果云请在网页端生成应用专用密码；保存在本机凭据目录", "group": "连接设置", "type": "secret", "when": {"field": "provider", "equals": "webdav"}},
      {"id": "webdavRoot", "title": "远端目录", "group": "连接设置", "type": "string", "default": "ArcMD", "when": {"field": "provider", "equals": "webdav"}},
      {"id": "spaces", "title": "选择要同步的空间", "description": "每个勾选的空间会同步到远端前缀下各自名称的目录，跳过本地版本历史快照。", "group": "同步空间", "type": "spaces"},
      {"id": "autoSync", "title": "定时自动同步", "description": "每隔一段时间在后台自动双向增量同步。", "group": "自动同步", "type": "bool", "default": false},
      {"id": "intervalMinutes", "title": "同步间隔", "group": "自动同步", "type": "segmented", "default": "15", "options": [{"value": "5", "label": "5 分钟"}, {"value": "15", "label": "15 分钟"}, {"value": "30", "label": "30 分钟"}, {"value": "60", "label": "60 分钟"}], "when": {"field": "autoSync", "equals": true}},
      {"id": "syncOnSave", "title": "保存文档后自动同步", "description": "保存已勾选空间的文档 15 秒后自动增量同步。", "group": "自动同步", "type": "bool", "default": true},
      {"id": "allowInsecureHTTP", "title": "允许非加密 HTTP", "group": "自动同步", "type": "bool", "default": false}
    ]
  }
}`

func buildMockPluginZip(t *testing.T, manifestJSON string) (*plugin.Manifest, []byte, string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string][]byte{
		plugin.ManifestFile: []byte(manifestJSON),
		"plugin.wasm":       {0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00},
	}
	for name, data := range files {
		hdr := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Unix(0, 0).UTC()}
		hdr.SetMode(0o644)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	man, shaHex, err := plugin.InspectArchive(raw)
	if err != nil {
		t.Fatal(err)
	}
	return man, raw, shaHex
}

func newMockPluginRegistry(t *testing.T, overrideSHA string, manifestJSONs ...string) *httptest.Server {
	t.Helper()
	zips := map[string][]byte{}
	var listings []plugin.Listing
	var srv *httptest.Server

	type pkgEntry struct {
		man  *plugin.Manifest
		name string
		sha  string
	}
	var entries []pkgEntry
	for _, mj := range manifestJSONs {
		man, zipBytes, shaHex := buildMockPluginZip(t, mj)
		if overrideSHA != "" {
			shaHex = overrideSHA
		}
		name := fmt.Sprintf("/%s-%s.zip", man.ID, man.Version)
		zips[name] = zipBytes
		entries = append(entries, pkgEntry{man: man, name: name, sha: shaHex})
	}

	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.json" {
			listings = listings[:0]
			for _, e := range entries {
				listings = append(listings, plugin.Listing{
					ID:          e.man.ID,
					Name:        e.man.Name,
					Version:     e.man.Version,
					Description: e.man.Description,
					Author:      e.man.Author,
					Homepage:    e.man.Homepage,
					Icon:        e.man.Icon,
					Permissions: e.man.Permissions,
					Download:    srv.URL + e.name,
					SHA256:      e.sha,
				})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(plugin.Index{Version: 1, Plugins: listings})
			return
		}
		if data, ok := zips[r.URL.Path]; ok {
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

type mockCloudSyncInstance struct {
	app *App
	p   *plugin.Installed
}

func (m *mockCloudSyncInstance) Close() error { return nil }

func (m *mockCloudSyncInstance) Invoke(ctx context.Context, req plugin.Request) (*plugin.Response, error) {
	switch req.Command {
	case "test-connection":
		return &plugin.Response{Status: "WebDAV 连接成功。", Toast: "WebDAV 连接成功。"}, nil
	case "sync-status":
		return &plugin.Response{Markdown: "# 云端同步记录\n\n最近同步成功。\n"}, nil
	case "sync-now":
		return m.syncNow(ctx)
	default:
		return nil, fmt.Errorf("unknown command %q", req.Command)
	}
}

func (m *mockCloudSyncInstance) davReq(method, rawURL, user, pass string, depth string, body []byte) (int, http.Header, []byte, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, rawURL, bodyReader)
	if err != nil {
		return 0, nil, nil, err
	}
	req.SetBasicAuth(user, pass)
	if depth != "" {
		req.Header.Set("Depth", depth)
	}
	client := m.app.pluginsUI.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, data, err
}

func (m *mockCloudSyncInstance) ensureDAVDir(rootURL, relDir, user, pass string) error {
	cur := strings.TrimRight(rootURL, "/")
	for _, seg := range strings.Split(strings.Trim(relDir, "/"), "/") {
		if seg == "" {
			continue
		}
		cur += "/" + url.PathEscape(seg)
		code, _, _, err := m.davReq("MKCOL", cur+"/", user, pass, "", nil)
		if err != nil {
			return err
		}
		if code >= 400 && code != http.StatusConflict && code != http.StatusMethodNotAllowed {
			return fmt.Errorf("MKCOL %s returned %d", cur, code)
		}
	}
	return nil
}

type mockDAVMeta struct {
	ETag    string
	Size    int64
	ModUnix int64
}

func (m *mockCloudSyncInstance) listDAVFiles(spaceURL, user, pass string) (map[string]mockDAVMeta, error) {
	baseParsed, err := url.Parse(spaceURL)
	if err != nil {
		return nil, err
	}
	basePath := strings.TrimRight(baseParsed.Path, "/")
	files := map[string]mockDAVMeta{}
	queue := []string{""}

	type davRespXML struct {
		Responses []struct {
			Href     string `xml:"href"`
			PropStat []struct {
				Prop struct {
					Collection *struct{} `xml:"resourcetype>collection"`
					Length     int64     `xml:"getcontentlength"`
					LastMod    string    `xml:"getlastmodified"`
					ETag       string    `xml:"getetag"`
				} `xml:"prop"`
			} `xml:"propstat"`
		} `xml:"response"`
	}

	for len(queue) > 0 {
		sub := queue[0]
		queue = queue[1:]
		dirURL := strings.TrimRight(spaceURL, "/") + "/"
		if sub != "" {
			dirURL += escapeAppDAVPath(sub)[1:] + "/"
		}
		code, _, body, err := m.davReq("PROPFIND", dirURL, user, pass, "1", nil)
		if err != nil {
			return nil, err
		}
		if code == http.StatusNotFound {
			continue
		}
		if code >= 400 {
			return nil, fmt.Errorf("PROPFIND %s returned %d", dirURL, code)
		}
		var ms davRespXML
		if err := xml.Unmarshal(body, &ms); err != nil {
			return nil, err
		}
		for _, r := range ms.Responses {
			u, perr := url.Parse(r.Href)
			if perr != nil {
				continue
			}
			pClean := path.Clean(u.Path)
			if pClean == basePath || !strings.HasPrefix(pClean, basePath+"/") {
				continue
			}
			rel := strings.TrimPrefix(pClean, basePath+"/")
			if rel == sub || rel == "" {
				continue
			}
			var isCol bool
			var meta mockDAVMeta
			for _, ps := range r.PropStat {
				if ps.Prop.Collection != nil {
					isCol = true
				}
				if ps.Prop.ETag != "" {
					meta.ETag = strings.Trim(ps.Prop.ETag, "\"")
				}
				if ps.Prop.Length > 0 {
					meta.Size = ps.Prop.Length
				}
				if t, terr := http.ParseTime(ps.Prop.LastMod); terr == nil {
					meta.ModUnix = t.Unix()
				}
			}
			if isCol || strings.HasSuffix(r.Href, "/") {
				queue = append(queue, rel)
				continue
			}
			files[rel] = meta
		}
	}
	return files, nil
}

func (m *mockCloudSyncInstance) syncNow(ctx context.Context) (*plugin.Response, error) {
	spaceSvc := plugin.SpaceServiceFromContext(ctx)
	if spaceSvc == nil {
		return nil, errors.New("missing space service")
	}
	cfg, err := m.app.pluginsUI.cfg.Load(m.p.Manifest)
	if err != nil {
		return nil, err
	}
	davURL := strings.TrimRight(fmt.Sprint(cfg["webdavURL"]), "/")
	davRoot := strings.Trim(fmt.Sprint(cfg["webdavRoot"]), "/")
	if davRoot == "" {
		davRoot = "ArcMD"
	}
	user := fmt.Sprint(cfg["webdavUsername"])
	sk, _ := plugin.SecretKey(m.p.Manifest.ID, "webdavPassword")
	passBytes, _ := m.app.qa.secrets.Get(sk)
	pass := string(passBytes)

	selectedSpaces := map[string]bool{}
	for _, id := range stringSliceValue(cfg["spaces"]) {
		selectedSpaces[id] = true
	}

	uploaded := 0
	var conflicts []persistedConflict

	for _, sp := range spaceSvc.ListSpaces() {
		if !selectedSpaces[sp.ID] {
			continue
		}
		prefixRel := davRoot + "/" + sp.Name
		if err := m.ensureDAVDir(davURL, prefixRel, user, pass); err != nil {
			return nil, err
		}
		spaceURL := davURL + escapeAppDAVPath(prefixRel)

		basePath, err := m.app.pluginsUI.cfg.StatePath(m.p.Manifest.ID, "baseline_"+sp.ID+".json")
		if err != nil {
			return nil, err
		}
		baseline := persistedBaseline{Version: 1, Files: map[string]persistedBaselineRow{}}
		if raw, err := os.ReadFile(basePath); err == nil {
			_ = json.Unmarshal(raw, &baseline)
			if baseline.Files == nil {
				baseline.Files = map[string]persistedBaselineRow{}
			}
		}

		localList, err := spaceSvc.ListFiles(sp.ID)
		if err != nil {
			return nil, err
		}
		localMap := map[string]plugin.SpaceFileEntry{}
		for _, f := range localList {
			localMap[f.Path] = f
		}
		remoteMap, err := m.listDAVFiles(spaceURL, user, pass)
		if err != nil {
			return nil, err
		}

		allPaths := map[string]bool{}
		for p := range localMap {
			allPaths[p] = true
		}
		for p := range remoteMap {
			allPaths[p] = true
		}
		for p := range baseline.Files {
			allPaths[p] = true
		}

		for rel := range allPaths {
			_, hasLocal := localMap[rel]
			rMeta, hasRemote := remoteMap[rel]
			bRow, hasBase := baseline.Files[rel]
			fileURL := spaceURL + escapeAppDAVPath(rel)

			switch {
			case hasLocal && !hasRemote && !hasBase:
				data, meta, err := spaceSvc.ReadFile(sp.ID, rel)
				if err != nil {
					return nil, err
				}
				if dir := path.Dir(rel); dir != "." {
					if err := m.ensureDAVDir(spaceURL, dir, user, pass); err != nil {
						return nil, err
					}
				}
				code, hdr, _, err := m.davReq(http.MethodPut, fileURL, user, pass, "", data)
				if err != nil || code >= 400 {
					return nil, fmt.Errorf("PUT %s failed: %d %v", rel, code, err)
				}
				etag := strings.Trim(hdr.Get("ETag"), "\"")
				baseline.Files[rel] = persistedBaselineRow{
					LocalSize:    meta.Size,
					LocalModNano: meta.ModTimeUnixNano,
					LocalSHA256:  meta.SHA256,
					RemoteSize:   int64(len(data)),
					RemoteETag:   etag,
				}
				writeCloudBaseSnapshot(filepath.Dir(basePath), sp.ID, rel, string(data))
				uploaded++

			case hasLocal && !hasRemote && hasBase:
				_, meta, err := spaceSvc.ReadFile(sp.ID, rel)
				if err != nil {
					return nil, err
				}
				if !meta.Dirty && meta.SHA256 == bRow.LocalSHA256 {
					if err := spaceSvc.DeleteFile(sp.ID, rel, meta.SHA256); err != nil {
						return nil, err
					}
					delete(baseline.Files, rel)
				}

			case hasLocal && hasRemote:
				data, meta, err := spaceSvc.ReadFile(sp.ID, rel)
				if err != nil {
					return nil, err
				}
				localChanged := meta.Dirty || !hasBase || meta.SHA256 != bRow.LocalSHA256
				remoteChanged := !hasBase || rMeta.ETag != bRow.RemoteETag
				if !localChanged && remoteChanged {
					code, _, rBody, err := m.davReq(http.MethodGet, fileURL, user, pass, "", nil)
					if err != nil || code >= 400 {
						return nil, fmt.Errorf("GET %s failed: %d %v", rel, code, err)
					}
					written, err := spaceSvc.WriteFile(sp.ID, rel, rBody, meta.SHA256, rMeta.ModUnix*1e9)
					if err != nil {
						return nil, err
					}
					baseline.Files[rel] = persistedBaselineRow{
						LocalSize:     written.Size,
						LocalModNano:  written.ModTimeUnixNano,
						LocalSHA256:   written.SHA256,
						RemoteSize:    rMeta.Size,
						RemoteETag:    rMeta.ETag,
						RemoteModUnix: rMeta.ModUnix,
					}
					writeCloudBaseSnapshot(filepath.Dir(basePath), sp.ID, rel, string(rBody))
				} else if localChanged && !remoteChanged && !meta.Dirty {
					if dir := path.Dir(rel); dir != "." {
						_ = m.ensureDAVDir(spaceURL, dir, user, pass)
					}
					code, hdr, _, err := m.davReq(http.MethodPut, fileURL, user, pass, "", data)
					if err != nil || code >= 400 {
						return nil, fmt.Errorf("PUT %s failed: %d %v", rel, code, err)
					}
					baseline.Files[rel] = persistedBaselineRow{
						LocalSize:    meta.Size,
						LocalModNano: meta.ModTimeUnixNano,
						LocalSHA256:  meta.SHA256,
						RemoteSize:   int64(len(data)),
						RemoteETag:   strings.Trim(hdr.Get("ETag"), "\""),
					}
					writeCloudBaseSnapshot(filepath.Dir(basePath), sp.ID, rel, string(data))
					uploaded++
				} else if localChanged && remoteChanged {
					code, _, rBody, err := m.davReq(http.MethodGet, fileURL, user, pass, "", nil)
					if err != nil || code >= 400 {
						return nil, fmt.Errorf("GET conflict %s failed: %d %v", rel, code, err)
					}
					baseSnap, _ := readCloudBaseSnapshot(filepath.Dir(basePath), sp.ID, rel)
					conflicts = append(conflicts, persistedConflict{
						SpaceID:       sp.ID,
						SpaceName:     sp.Name,
						Path:          rel,
						BaseText:      baseSnap,
						LocalText:     string(data),
						RemoteText:    string(rBody),
						RemoteSize:    rMeta.Size,
						RemoteETag:    rMeta.ETag,
						RemoteModUnix: rMeta.ModUnix,
						CreatedUnix:   time.Now().Unix(),
					})
				}
			}
		}

		bJSON, _ := json.Marshal(baseline)
		_ = os.MkdirAll(filepath.Dir(basePath), 0o755)
		_ = os.WriteFile(basePath, bJSON, 0o600)
	}

	cfPath, err := m.app.pluginsUI.cfg.StatePath(m.p.Manifest.ID, "conflicts.json")
	if err == nil {
		if len(conflicts) > 0 {
			cJSON, _ := json.Marshal(conflicts)
			_ = os.WriteFile(cfPath, cJSON, 0o600)
		} else {
			_ = os.Remove(cfPath)
		}
	}
	msg := fmt.Sprintf("云端同步完成：上传 %d", uploaded)
	return &plugin.Response{Status: msg, Toast: msg}, nil
}

func wireCloudSyncMock(t *testing.T, a *App) {
	t.Helper()
	regSrv := newMockPluginRegistry(t, "", testCloudSyncManifestJSON)
	a.pluginsUI.registryURL = regSrv.URL + "/index.json"
	a.pluginsUI.httpClient = regSrv.Client()
	a.pluginsUI.sync = true
	a.pluginsUI.startInstance = func(p *plugin.Installed) (plugin.Instance, error) {
		if p.Manifest.ID == cloudSyncPluginID {
			return &mockCloudSyncInstance{app: a, p: p}, nil
		}
		return nil, fmt.Errorf("unexpected mock plugin %q", p.Manifest.ID)
	}
}

type appDAVObj struct {
	data    []byte
	etag    string
	modTime time.Time
}

type appDAVServer struct {
	mu       sync.Mutex
	user     string
	pass     string
	dirs     map[string]bool
	files    map[string]appDAVObj
	putCount atomic.Int32
}

func newAppDAVServer(t *testing.T, user, pass string) (*appDAVServer, *httptest.Server) {
	t.Helper()
	m := &appDAVServer{
		user:  user,
		pass:  pass,
		dirs:  map[string]bool{"/dav": true},
		files: map[string]appDAVObj{},
	}
	srv := httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(srv.Close)
	return m, srv
}

func (m *appDAVServer) serve(w http.ResponseWriter, r *http.Request) {
	u, p, ok := r.BasicAuth()
	if !ok || u != m.user || p != m.pass {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	reqPath := path.Clean("/" + strings.TrimLeft(r.URL.Path, "/"))
	m.mu.Lock()
	defer m.mu.Unlock()

	switch r.Method {
	case "PROPFIND":
		if strings.EqualFold(r.Header.Get("Depth"), "infinity") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		isDir := m.dirs[reqPath]
		f, isFile := m.files[reqPath]
		if !isDir && !isFile {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		var buf bytes.Buffer
		buf.WriteString(`<?xml version="1.0" encoding="utf-8"?><D:multistatus xmlns:D="DAV:">`)
		writeDir := func(dp string) {
			buf.WriteString("<D:response><D:href>" + escapeAppDAVPath(dp) + "/</D:href><D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>")
		}
		writeFile := func(fp string, obj appDAVObj) {
			buf.WriteString("<D:response><D:href>" + escapeAppDAVPath(fp) + "</D:href><D:propstat><D:prop><D:resourcetype/>")
			fmt.Fprintf(&buf, "<D:getcontentlength>%d</D:getcontentlength>", len(obj.data))
			fmt.Fprintf(&buf, "<D:getlastmodified>%s</D:getlastmodified>", obj.modTime.UTC().Format(http.TimeFormat))
			fmt.Fprintf(&buf, `<D:getetag>"%s"</D:getetag>`, obj.etag)
			buf.WriteString("</D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>")
		}
		if isFile {
			writeFile(reqPath, f)
		} else {
			writeDir(reqPath)
			if r.Header.Get("Depth") == "1" {
				for d := range m.dirs {
					if d != reqPath && path.Dir(d) == reqPath {
						writeDir(d)
					}
				}
				for fp, obj := range m.files {
					if path.Dir(fp) == reqPath {
						writeFile(fp, obj)
					}
				}
			}
		}
		buf.WriteString("</D:multistatus>")
		w.Write(buf.Bytes())

	case "MKCOL":
		if !m.dirs[path.Dir(reqPath)] {
			w.WriteHeader(http.StatusConflict)
			return
		}
		m.dirs[reqPath] = true
		w.WriteHeader(http.StatusCreated)

	case http.MethodGet:
		obj, ok := m.files[reqPath]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("ETag", `"`+obj.etag+`"`)
		w.Header().Set("Last-Modified", obj.modTime.UTC().Format(http.TimeFormat))
		w.Write(obj.data)

	case http.MethodPut:
		if !m.dirs[path.Dir(reqPath)] {
			w.WriteHeader(http.StatusConflict)
			return
		}
		m.putCount.Add(1)
		body, _ := io.ReadAll(r.Body)
		sum := md5.Sum(body)
		etag := hex.EncodeToString(sum[:])
		now := time.Now().UTC()
		m.files[reqPath] = appDAVObj{data: body, etag: etag, modTime: now}
		w.Header().Set("ETag", `"`+etag+`"`)
		w.Header().Set("Last-Modified", now.Format(http.TimeFormat))
		w.WriteHeader(http.StatusCreated)

	case http.MethodDelete:
		delete(m.files, reqPath)
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func escapeAppDAVPath(p string) string {
	segs := strings.Split(strings.TrimLeft(p, "/"), "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return "/" + strings.Join(segs, "/")
}

func installCloudSyncViaUI(t *testing.T, a *App, tt *ui.Tester) {
	t.Helper()
	wireCloudSyncMock(t, a)
	a.openPlugins()
	settle(tt)
	if err := tt.Click("安装 云端同步"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.pluginsUI.install.open || !tt.HasText("此插件请求以下权限：") || !tt.HasText("读取空间内的文档与附件") {
		t.Fatalf("permission confirmation modal not shown: %q", tt.Texts())
	}
	if len(a.installed) != 0 {
		t.Fatal("plugin was installed before permission confirmation")
	}
	if err := tt.Click("下载并安装"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.installed) != 1 || !a.installed[0].Enabled || !a.pluginsUI.settings.open {
		t.Fatalf("install did not enable plugin and open settings: installed=%d, settingsOpen=%v", len(a.installed), a.pluginsUI.settings.open)
	}
}

func TestPluginInstallPermissionAndSettings(t *testing.T) {
	a, tt := newTestApp(t)
	if err := a.qa.secrets.Set(apiKeySecret, []byte("core-ai-key")); err != nil {
		t.Fatal(err)
	}

	installCloudSyncViaUI(t, a, tt)

	// S3 settings are visible by default.
	if !tt.HasText("Endpoint") || !tt.HasText("Bucket") || tt.HasText("服务器地址") {
		t.Fatalf("unexpected default S3 settings view: %q", tt.Texts())
	}

	// Switch to WebDAV -> WebDAV fields appear, S3 fields hide.
	if err := tt.Click("WebDAV"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("服务器地址") || !tt.HasText("密码 / 应用密码") || tt.HasText("Bucket") {
		t.Fatalf("WebDAV toggle did not update fields: %q", tt.Texts())
	}

	// Select preset "坚果云".
	if err := tt.Click("常用服务"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.ChooseMenuItem("坚果云"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := a.pluginsUI.settings.values["webdavURL"]; got != "https://dav.jianguoyun.com/dav/" {
		t.Fatalf("preset did not fill webdavURL: %v", got)
	}

	// Save secret and verify isolation from core AI key and settings.json.
	*a.pluginsUI.settings.secretBufs["webdavPassword"] = "secret-dav-pass"
	tt.Frame()
	if err := tt.Click("保存 密码 / 应用密码"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("已保存 ••••pass") {
		t.Fatalf("saved secret indicator missing: %q", tt.Texts())
	}
	sp, err := a.pluginsUI.cfg.SettingsPath(cloudSyncPluginID)
	if err != nil {
		t.Fatal(err)
	}
	rawCfg, _ := os.ReadFile(sp)
	if strings.Contains(string(rawCfg), "secret-dav-pass") {
		t.Fatal("secret leaked into plugin settings.json")
	}
	if aiKey, _ := a.qa.secrets.Get(apiKeySecret); string(aiKey) != "core-ai-key" {
		t.Fatal("core AI secret was modified")
	}

	// First Escape closes plugin settings modal while keeping Library open; second closes Library.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.pluginsUI.settings.open || !a.library.open {
		t.Fatalf("first Escape wrong: settings=%v library=%v", a.pluginsUI.settings.open, a.library.open)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.library.open {
		t.Fatal("second Escape did not close library")
	}
}

func TestPluginCloudSyncWebDAVLiveReloadConflictAndTrash(t *testing.T) {
	dav, srv := newAppDAVServer(t, "alice", "app-pass")
	a, tt := newTestApp(t)
	a.trashFile = os.RemoveAll

	installCloudSyncViaUI(t, a, tt)
	_ = srv

	if err := tt.Click("WebDAV"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	*a.pluginsUI.settings.strBufs["webdavURL"] = srv.URL + "/dav/"
	*a.pluginsUI.settings.strBufs["webdavUsername"] = "alice"
	*a.pluginsUI.settings.secretBufs["webdavPassword"] = "app-pass"
	*a.pluginsUI.settings.spaceBufs[a.state.ActiveSpace().ID] = true
	a.savePluginSettings()
	a.savePendingPluginSecrets()
	tt.Frame()

	// 1. Test connection button inside settings.
	if err := tt.Click("测试连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("WebDAV 连接成功。") {
		t.Fatalf("test-connection toast missing: %q", tt.Texts())
	}

	// 2. Sync Now uploads initial Personal space documents.
	if err := tt.Click("立即同步"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("云端同步完成：上传 2") {
		t.Fatalf("initial sync status missing: %q", tt.Texts())
	}
	dav.mu.Lock()
	_, hasWelcome := dav.files["/dav/ArcMD/Personal/Welcome.md"]
	_, hasIdeas := dav.files["/dav/ArcMD/Personal/Journal/Ideas.md"]
	dav.mu.Unlock()
	if !hasWelcome || !hasIdeas {
		t.Fatalf("uploaded files missing on WebDAV: welcome=%v ideas=%v", hasWelcome, hasIdeas)
	}

	// Close settings and library so we watch the live editor.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()

	// 3. Remote updates Welcome.md while open without unsaved edits -> sync reloads live editor.
	remoteBody := []byte("# Updated Remotely\n\nFresh content.\n")
	sum := md5.Sum(remoteBody)
	dav.mu.Lock()
	dav.files["/dav/ArcMD/Personal/Welcome.md"] = appDAVObj{
		data:    remoteBody,
		etag:    hex.EncodeToString(sum[:]),
		modTime: time.Now().Add(2 * time.Second).UTC(),
	}
	dav.mu.Unlock()

	p := a.findInstalledPlugin(cloudSyncPluginID)
	a.runPluginCommand(p, "sync-now", nil, false)
	tt.Frame()
	if !strings.Contains(a.doc.text(), "Updated Remotely") {
		t.Fatalf("live editor did not reload remote update: %q", a.doc.text())
	}

	// 4. Dirty unsaved edit in editor + remote change -> stages 3-way merge without clobbering dirty buffer or creating "(冲突副本)" files.
	a.doc.ed.SetSelection(0, 0)
	a.doc.ed.ReplaceText(nil, "Unsaved ")
	tt.Frame()
	remoteV3 := []byte("# Conflict Version\n")
	sum3 := md5.Sum(remoteV3)
	dav.mu.Lock()
	dav.files["/dav/ArcMD/Personal/Welcome.md"] = appDAVObj{
		data:    remoteV3,
		etag:    hex.EncodeToString(sum3[:]),
		modTime: time.Now().Add(4 * time.Second).UTC(),
	}
	dav.mu.Unlock()

	a.runPluginCommand(p, "sync-now", nil, false)
	tt.Frame()
	if !strings.Contains(a.doc.text(), "Unsaved") {
		t.Fatalf("dirty editor buffer was clobbered: %q", a.doc.text())
	}
	matches, _ := filepath.Glob(filepath.Join(a.state.ActiveSpace().Root, "Welcome (冲突副本*).md"))
	if len(matches) != 0 {
		t.Fatalf("expected 0 conflict copy files for text merge, got %v", matches)
	}
	if !a.mergeUI.open {
		t.Fatal("expected 3-way merge modal to open automatically on sync conflict")
	}
	if err := tt.Click("全部采用本地"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("完成合并并保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	// 5. Remote deletion of Journal/Ideas.md moves local copy into ArcMD trashbin ("最近删除").
	dav.mu.Lock()
	delete(dav.files, "/dav/ArcMD/Personal/Journal/Ideas.md")
	dav.mu.Unlock()
	a.runPluginCommand(p, "sync-now", nil, false)
	tt.Frame()
	if _, err := os.Stat(filepath.Join(a.state.ActiveSpace().Root, "Journal/Ideas.md")); !os.IsNotExist(err) {
		t.Fatal("Ideas.md still exists on disk after remote delete")
	}
	trashed, err := a.bin.List(time.Now())
	if err != nil || len(trashed) != 1 || trashed[0].Path != "Journal/Ideas.md" {
		t.Fatalf("remote delete did not move to trashbin: %+v, err=%v", trashed, err)
	}

	// 6. Command palette runs plugin command "云端同步：查看同步记录" and opens Markdown sheet.
	a.openPalette()
	a.palette.query = "查看同步记录"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !a.pluginsUI.sheet.open || !tt.HasText("云端同步记录") {
		t.Fatalf("sync history markdown modal did not open: open=%v texts=%q", a.pluginsUI.sheet.open, tt.Texts())
	}
}

func TestPluginAutoSyncFrameClock(t *testing.T) {
	dav, srv := newAppDAVServer(t, "u", "p")
	a, tt := newTestApp(t)

	installCloudSyncViaUI(t, a, tt)
	if err := tt.Click("WebDAV"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	*a.pluginsUI.settings.strBufs["webdavURL"] = srv.URL + "/dav/"
	*a.pluginsUI.settings.strBufs["webdavUsername"] = "u"
	*a.pluginsUI.settings.secretBufs["webdavPassword"] = "p"
	*a.pluginsUI.settings.spaceBufs[a.state.ActiveSpace().ID] = true
	*a.pluginsUI.settings.boolBufs["autoSync"] = true
	*a.pluginsUI.settings.boolBufs["syncOnSave"] = true
	*a.pluginsUI.settings.segBufs["intervalMinutes"] = 0 // 5 minutes
	a.savePluginSettings()
	a.savePendingPluginSecrets()
	a.pluginsUI.settings.open = false
	a.closeLibrary()
	tt.Frame()

	// Edit and wait for autosave (700ms) -> arms 15s post-save sync.
	a.doc.ed.SetSelection(0, 0)
	a.doc.ed.ReplaceText(nil, "AutoSync ")
	tt.Frame()
	tt.Advance(autosaveDelay + 50*time.Millisecond)
	tt.Frame()
	if dav.putCount.Load() != 0 {
		t.Fatal("sync ran immediately before 15s debounce")
	}
	tt.Advance(15*time.Second + 50*time.Millisecond)
	tt.Frame()
	if dav.putCount.Load() < 2 {
		t.Fatalf("post-save auto sync did not upload files: puts=%d", dav.putCount.Load())
	}

	// Modify locally on disk and advance 5 minutes -> periodic autoSync runs.
	putsBefore := dav.putCount.Load()
	os.WriteFile(filepath.Join(a.state.ActiveSpace().Root, "Extra.md"), []byte("# Extra\n"), 0o644)
	tt.Advance(5*time.Minute + 50*time.Millisecond)
	tt.Frame()
	if dav.putCount.Load() <= putsBefore {
		t.Fatalf("periodic auto sync did not trigger: puts=%d -> %d", putsBefore, dav.putCount.Load())
	}
}

func TestPluginLocalZipInstallEnableDisableUninstall(t *testing.T) {
	a, tt := newTestApp(t)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(plugin.ManifestFile)
	w.Write([]byte(`{
	  "id": "dev.example.hello",
	  "name": "Hello Plugin",
	  "version": "1.0.0",
	  "description": "Test local zip install",
	  "runtime": "wasm",
	  "main": "plugin.wasm"
	}`))
	w2, _ := zw.Create("plugin.wasm")
	w2.Write([]byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00})
	zw.Close()

	zipPath := filepath.Join(t.TempDir(), "hello.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	a.pluginsUI.pickFile = func(done func(string)) { done(zipPath) }

	a.openPlugins()
	settle(tt)
	if err := tt.Click("从本地安装插件…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Hello Plugin") || !tt.HasText("此插件不请求额外系统权限。") {
		t.Fatalf("local plugin confirm missing: %q", tt.Texts())
	}
	if err := tt.Click("安装并启用"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.findInstalledPlugin("dev.example.hello") == nil {
		t.Fatal("local zip plugin not installed")
	}

	// Toggle off via switch.
	if err := tt.Click("启用 Hello Plugin"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if p := a.findInstalledPlugin("dev.example.hello"); p == nil || p.Enabled {
		t.Fatal("plugin was not disabled")
	}
	if !tt.HasText("已停用") {
		t.Fatalf("disabled text missing: %q", tt.Texts())
	}

	// Uninstall directly and verify removal.
	a.uninstallPlugin("dev.example.hello")
	tt.Frame()
	if a.findInstalledPlugin("dev.example.hello") != nil {
		t.Fatal("plugin still present after uninstall")
	}
}

func TestPluginEnglishLocalization(t *testing.T) {
	a, tt := newTestApp(t)
	wireCloudSyncMock(t, a)
	i18n.SetLanguage("en")
	t.Cleanup(func() { i18n.SetLanguage("zh-Hans") })

	a.openPlugins()
	settle(tt)
	if !tt.HasText("Official Plugins") || !tt.HasText("Cloud Sync") {
		t.Fatalf("English plugins page missing labels: %q", tt.Texts())
	}
	if err := tt.Click("Install Cloud Sync"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("This plugin requests the following permissions:") || !tt.HasText("Download and Install") {
		t.Fatalf("English permission prompt missing labels: %q", tt.Texts())
	}
	if err := tt.Click("Download and Install"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Connection") || !tt.HasText("Spaces to Sync") || !tt.HasText("Sync Now") {
		t.Fatalf("English plugin settings missing labels: %q", tt.Texts())
	}
}

func TestPluginSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render plugin UI")

	render := func(name string, dark bool, setup func(a *App, tt *ui.Tester)) {
		t.Run(name, func(t *testing.T) {
			a, tt := libraryApp(t)
			tt.SetSize(1240, 760)
			tt.SetDark(dark)
			setup(a, tt)
			for range 6 {
				tt.Frame()
				tt.Advance(200 * time.Millisecond)
			}
			file, err := os.Create(filepath.Join(dir, name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := png.Encode(file, tt.Image()); err != nil {
				t.Fatal(err)
			}
		})
	}

	for _, dark := range []bool{false, true} {
		suffix := ""
		if dark {
			suffix = "-dark"
		}
		render("plugin-install-confirm"+suffix, dark, func(a *App, tt *ui.Tester) {
			wireCloudSyncMock(t, a)
			a.openPlugins()
			settle(tt)
			_ = tt.Click("安装 云端同步")
		})
		render("plugin-settings-s3"+suffix, dark, func(a *App, tt *ui.Tester) {
			wireCloudSyncMock(t, a)
			a.openPlugins()
			settle(tt)
			_ = tt.Click("安装 云端同步")
			tt.Frame()
			_ = tt.Click("下载并安装")
		})
	}
	render("plugin-settings-webdav", false, func(a *App, tt *ui.Tester) {
		wireCloudSyncMock(t, a)
		a.openPlugins()
		settle(tt)
		_ = tt.Click("安装 云端同步")
		tt.Frame()
		_ = tt.Click("下载并安装")
		tt.Frame()
		_ = tt.Click("WebDAV")
	})
	render("plugin-history-sheet", false, func(a *App, tt *ui.Tester) {
		wireCloudSyncMock(t, a)
		a.openPlugins()
		settle(tt)
		_ = tt.Click("安装 云端同步")
		tt.Frame()
		_ = tt.Click("下载并安装")
		tt.Frame()
		a.pluginsUI.settings.open = false
		p := a.findInstalledPlugin(cloudSyncPluginID)
		a.runPluginCommand(p, "sync-status", nil, false)
	})
	render("plugin-card-pink", false, func(a *App, tt *ui.Tester) {
		wireCloudSyncMock(t, a)
		a.state.ActiveSpace().SetTheme(space.Theme{
			Colors:     []string{"#EAB8C2", "#F1D4C2"},
			Intensity:  0.75,
			Noise:      0.25,
			Appearance: space.AppearanceLight,
		})
		a.openPlugins()
		settle(tt)
	})
	render("plugin-card-installed-pink", false, func(a *App, tt *ui.Tester) {
		wireCloudSyncMock(t, a)
		a.state.ActiveSpace().SetTheme(space.Theme{
			Colors:     []string{"#EAB8C2", "#F1D4C2"},
			Intensity:  0.75,
			Noise:      0.25,
			Appearance: space.AppearanceLight,
		})
		a.openPlugins()
		settle(tt)
		_ = tt.Click("安装 云端同步")
		tt.Frame()
		_ = tt.Click("下载并安装")
		tt.Frame()
		a.pluginsUI.settings.open = false
		a.undos = nil
		a.notices = nil
	})
}
