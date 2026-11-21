package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/plugin"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

const testImageHostManifestJSON = `{
  "id": "dev.arcmd.image-host",
  "name": "图床上传",
  "version": "0.1.0",
  "description": "粘贴、拖入或插入图片时自动上传到 S3（R2 / OSS / COS / MinIO）或自定义 HTTP 图床，文档内保留外链且避免云端同步重复同步图片。",
  "author": "ArcMD",
  "homepage": "https://github.com/Ciao1019/arcmd-plugins",
  "license": "MIT",
  "icon": "image",
  "engine": "0.2.5",
  "runtime": "wasm",
  "main": "plugin.wasm",
  "permissions": ["network", "secrets"],
  "contributes": {
    "commands": [
      {"id": "upload-doc-images", "title": "图床上传：上传当前文档本地图片", "when": "document", "primary": true},
      {"id": "test-connection", "title": "图床上传：测试连接", "inSettings": true}
    ],
    "settings": [
      {"id": "autoUpload", "title": "粘贴或拖入图片时自动上传", "description": "开启后，图片直接上传到图床并在正文中插入远程链接，不再保存到空间 assets 目录。", "group": "基本设置", "type": "bool", "default": true},
      {"id": "fallbackLocal", "title": "上传失败时回退保存到本地", "description": "网络异常或配置未完成时，改为写入当前文档旁的 assets 目录，避免图片丢失。", "group": "基本设置", "type": "bool", "default": true},
      {"id": "deleteLocalAfterMigrate", "title": "迁移文档本地图片后移至废纸篓", "description": "使用「上传当前文档本地图片」成功替换外链后，将未被其他文档引用的本地图片移至废纸篓。", "group": "基本设置", "type": "bool", "default": true},
      {"id": "provider", "title": "图床服务", "group": "连接设置", "type": "segmented", "default": "s3", "options": [{"value": "s3", "label": "S3 对象存储"}, {"value": "custom", "label": "自定义 HTTP"}]},
      {"id": "s3Endpoint", "title": "Endpoint", "description": "S3 兼容服务地址", "group": "连接设置", "type": "string", "when": {"field": "provider", "equals": "s3"}},
      {"id": "s3Region", "title": "Region", "group": "连接设置", "type": "string", "default": "auto", "when": {"field": "provider", "equals": "s3"}},
      {"id": "s3Bucket", "title": "Bucket", "group": "连接设置", "type": "string", "when": {"field": "provider", "equals": "s3"}},
      {"id": "s3AccessKey", "title": "Access Key ID", "group": "连接设置", "type": "string", "when": {"field": "provider", "equals": "s3"}},
      {"id": "s3SecretKey", "title": "Secret Access Key", "description": "保存在本机凭据目录，不进空间文件夹", "group": "连接设置", "type": "secret", "when": {"field": "provider", "equals": "s3"}},
      {"id": "s3Prefix", "title": "对象路径前缀", "group": "连接设置", "type": "string", "default": "images", "when": {"field": "provider", "equals": "s3"}},
      {"id": "s3PublicBaseURL", "title": "公开访问域名（可选）", "description": "例如 Cloudflare R2 自定义域名 https://img.example.com；留空则使用 S3 访问地址。", "group": "连接设置", "type": "string", "when": {"field": "provider", "equals": "s3"}},
      {"id": "s3PathStyle", "title": "使用 Path-Style 路径模式", "group": "连接设置", "type": "bool", "default": false, "when": {"field": "provider", "equals": "s3"}},
      {"id": "s3AllowInsecure", "title": "允许非加密 HTTP", "group": "连接设置", "type": "bool", "default": false, "when": {"field": "provider", "equals": "s3"}},
      {"id": "customURL", "title": "上传接口地址", "group": "连接设置", "type": "string", "when": {"field": "provider", "equals": "custom"}},
      {"id": "customField", "title": "表单字段名", "group": "连接设置", "type": "string", "default": "file", "when": {"field": "provider", "equals": "custom"}},
      {"id": "customAuthHeader", "title": "Authorization 请求头（可选）", "group": "连接设置", "type": "secret", "when": {"field": "provider", "equals": "custom"}},
      {"id": "customJSONPath", "title": "响应 URL JSON 路径", "group": "连接设置", "type": "string", "default": "url", "when": {"field": "provider", "equals": "custom"}},
      {"id": "customAllowInsecure", "title": "允许非加密 HTTP", "group": "连接设置", "type": "bool", "default": false, "when": {"field": "provider", "equals": "custom"}}
    ]
  }
}`

type mockImageHostInstance struct {
	app *App
	p   *plugin.Installed
}

func (m *mockImageHostInstance) Close() error { return nil }

func (m *mockImageHostInstance) Invoke(_ context.Context, req plugin.Request) (*plugin.Response, error) {
	cfg, err := m.app.pluginsUI.cfg.Load(m.p.Manifest)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(fmt.Sprint(cfg["s3Endpoint"]), "/")
	bucket := strings.Trim(fmt.Sprint(cfg["s3Bucket"]), "/")
	prefix := strings.Trim(fmt.Sprint(cfg["s3Prefix"]), "/")
	publicBase := strings.TrimRight(fmt.Sprint(cfg["s3PublicBaseURL"]), "/")

	switch req.Command {
	case "test-connection":
		return &plugin.Response{Status: "S3 连接成功。", Toast: "S3 连接成功。"}, nil
	case "upload-image":
		rawB64, _ := req.Args["dataBase64"].(string)
		data, err := base64.StdEncoding.DecodeString(rawB64)
		if err != nil {
			return nil, err
		}
		name, _ := req.Args["filename"].(string)
		if name == "" {
			name = "image.png"
		}
		key := url.PathEscape(name)
		if prefix != "" {
			key = prefix + "/" + key
		}
		putURL := endpoint + "/" + bucket + "/" + key
		httpReq, err := http.NewRequest(http.MethodPut, putURL, bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if ct, _ := req.Args["contentType"].(string); ct != "" {
			httpReq.Header.Set("Content-Type", ct)
		}
		client := m.app.pluginsUI.httpClient
		if client == nil {
			client = http.DefaultClient
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, err
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("image host returned HTTP %d", resp.StatusCode)
		}
		outURL := putURL
		if publicBase != "" && cfg["s3PublicBaseURL"] != nil && strings.TrimSpace(fmt.Sprint(cfg["s3PublicBaseURL"])) != "" {
			outURL = publicBase + "/" + key
		}
		return &plugin.Response{Data: map[string]any{"url": outURL}}, nil
	default:
		return nil, fmt.Errorf("unknown command %q", req.Command)
	}
}

func newMockRegistryServer(t *testing.T, overrideSHA string) *httptest.Server {
	t.Helper()
	return newMockPluginRegistry(t, overrideSHA, testImageHostManifestJSON)
}

type mockImageS3Server struct {
	mu      sync.Mutex
	objects map[string][]byte
	putReqs []string
	getReqs []string
	failPut bool
}

func newMockImageS3(t *testing.T) (*mockImageS3Server, *httptest.Server) {
	t.Helper()
	m := &mockImageS3Server{objects: map[string][]byte{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			if m.failPut {
				http.Error(w, `<Error><Code>InternalError</Code><Message>upload failed</Message></Error>`, http.StatusInternalServerError)
				return
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			m.objects[r.URL.Path] = body
			m.putReqs = append(m.putReqs, r.URL.EscapedPath())
			w.Header().Set("ETag", `"mock-etag"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			if r.URL.Query().Get("list-type") == "2" {
				w.Header().Set("Content-Type", "application/xml")
				_, _ = io.WriteString(w, `<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`)
				return
			}
			m.getReqs = append(m.getReqs, r.URL.Path)
			data, ok := m.objects[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(data)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)
	return m, srv
}

func installImageHostViaUI(t *testing.T, a *App, tt *ui.Tester) {
	t.Helper()
	regSrv := newMockRegistryServer(t, "")
	a.pluginsUI.registryURL = regSrv.URL + "/index.json"
	a.pluginsUI.httpClient = regSrv.Client()
	a.pluginsUI.startInstance = func(p *plugin.Installed) (plugin.Instance, error) {
		if p.Manifest.ID == imageHostPluginID {
			return &mockImageHostInstance{app: a, p: p}, nil
		}
		return nil, fmt.Errorf("unexpected mock plugin %q", p.Manifest.ID)
	}
	a.openPlugins()
	settle(tt)
	if !tt.HasText("图床上传") {
		t.Fatalf("remote catalog missing 图床上传: %q", tt.Texts())
	}
	if err := tt.Click("安装 图床上传"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("访问网络") || !tt.HasText("在本机凭据目录读写密码与密钥") || !tt.HasText("下载并安装") {
		t.Fatalf("missing remote install confirm labels: %q", tt.Texts())
	}
	if err := tt.Click("下载并安装"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
}

func configureImageHostS3(t *testing.T, a *App, tt *ui.Tester, endpoint, publicBase string, autoUpload, fallbackLocal, deleteLocal bool) {
	t.Helper()
	*a.pluginsUI.settings.strBufs["s3Endpoint"] = endpoint
	*a.pluginsUI.settings.strBufs["s3Region"] = "auto"
	*a.pluginsUI.settings.strBufs["s3Bucket"] = "media"
	*a.pluginsUI.settings.strBufs["s3AccessKey"] = "AKIA"
	*a.pluginsUI.settings.strBufs["s3Prefix"] = "images"
	*a.pluginsUI.settings.strBufs["s3PublicBaseURL"] = publicBase
	*a.pluginsUI.settings.boolBufs["s3PathStyle"] = true
	*a.pluginsUI.settings.boolBufs["s3AllowInsecure"] = true
	*a.pluginsUI.settings.boolBufs["autoUpload"] = autoUpload
	*a.pluginsUI.settings.boolBufs["fallbackLocal"] = fallbackLocal
	*a.pluginsUI.settings.boolBufs["deleteLocalAfterMigrate"] = deleteLocal
	*a.pluginsUI.settings.secretBufs["s3SecretKey"] = "SECRET"
	a.savePluginSettings()
	a.savePendingPluginSecrets()
	tt.Frame()
	if err := tt.Click("测试连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.pluginsUI.settings.open = false
	a.library.open = false
	tt.Frame()
}

func encodePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestRemotePluginInstallRejectsBadSHA256(t *testing.T) {
	regSrv := newMockRegistryServer(t, strings.Repeat("0", 64))
	a, tt := newTestApp(t)
	a.pluginsUI.registryURL = regSrv.URL + "/index.json"
	a.pluginsUI.httpClient = regSrv.Client()
	a.pluginsUI.sync = true

	a.openPlugins()
	settle(tt)
	if err := tt.Click("安装 图床上传"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("下载并安装"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	if a.findInstalledPlugin(imageHostPluginID) != nil {
		t.Fatal("plugin with mismatched SHA-256 must not be installed")
	}
	if !tt.HasText("SHA-256") {
		t.Fatalf("expected SHA-256 mismatch error in dialog, got %q", tt.Texts())
	}
}

func TestImageHostPasteAndDropNoS3SyncDuplication(t *testing.T) {
	s3, srv := newMockImageS3(t)
	a, tt := newTestApp(t)
	a.pluginsUI.sync = true
	sp := a.state.ActiveSpace()

	installImageHostViaUI(t, a, tt)
	configureImageHostS3(t, a, tt, srv.URL, srv.URL+"/media", true, true, true)

	if err := vault.Write(sp.Root, "Notes.md", "# Remote Note\n\nBody\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Notes.md")
	tt.Frame()

	// Paste a PNG screenshot: uploads to S3, inserts remote URL, writes zero local assets.
	ed := a.doc.ed
	end := ed.Len()
	ed.SetSelection(end, end)
	ed.RequestFocus = true
	tt.Frame()

	pngBytes := encodePNG(t, 28, 18)
	tt.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, pngBytes))))
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()

	s3.mu.Lock()
	putCount := len(s3.putReqs)
	var putPath string
	if putCount > 0 {
		putPath = s3.putReqs[0]
	}
	s3.mu.Unlock()
	if putCount != 1 {
		t.Fatalf("expected 1 S3 PUT request, got %d", putCount)
	}
	wantURL := srv.URL + putPath
	gotBlocks := blockList(a)
	if !slices.Contains(gotBlocks, "image:"+wantURL) {
		t.Fatalf("expected remote image block %q in %q", "image:"+wantURL, gotBlocks)
	}

	// Zero local files created inside <spaceRoot>/assets/.
	if entries, err := os.ReadDir(filepath.Join(sp.Root, "assets")); err == nil && len(entries) > 0 {
		t.Fatalf("expected no local assets in space folder, found %d", len(entries))
	}

	// Verify cloud-sync's FolderSpaces.ListFiles sees zero image files for this space.
	spaces := plugin.NewFolderSpaces([]plugin.SpaceSnapshot{{ID: sp.ID, Name: sp.Name, Root: sp.Root}}, nil, nil)
	listed, err := spaces.ListFiles(sp.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range listed {
		if strings.HasSuffix(entry.Path, ".png") || strings.Contains(entry.Path, "assets/") {
			t.Fatalf("cloud-sync should not enumerate uploaded image %q", entry.Path)
		}
	}

	// Freshly uploaded image renders immediately from ConfigDir/cache without extra GET.
	res := a.imageResourceFor(a.doc, wantURL)
	if res.Bitmap == nil || res.Local {
		t.Fatalf("expected pre-cached remote bitmap with Local=false, got %#v", res)
	}
	s3.mu.Lock()
	getCount := len(s3.getReqs)
	s3.mu.Unlock()
	if getCount != 0 {
		t.Fatalf("expected 0 HTTP GETs for pre-cached uploaded image, got %d", getCount)
	}

	// Dropping an image file onto the editor also uploads via image-host without local assets.
	dropPic, _ := pngFile(t, "dropped.png")
	dropAt(t, a, tt, "Body", dropPic)
	s3.mu.Lock()
	putCountAfterDrop := len(s3.putReqs)
	s3.mu.Unlock()
	if putCountAfterDrop != 2 {
		t.Fatalf("expected 2 total S3 PUT requests after image drop, got %d", putCountAfterDrop)
	}
	if entries, err := os.ReadDir(filepath.Join(sp.Root, "assets")); err == nil && len(entries) > 0 {
		t.Fatalf("dropped image should not write to local assets when autoUpload=true, found %d", len(entries))
	}

	// Dropping a PDF onto the editor still saves locally in assets/ (not sent to image host).
	pdfFile := writeExternalFile(t, t.TempDir(), "slides.pdf", buildRichSamplePDF(t))
	dropAt(t, a, tt, "Body", pdfFile)
	if _, err := os.Stat(filepath.Join(sp.Root, "assets", "slides.pdf")); err != nil {
		t.Fatalf("expected dropped PDF to stay local in assets/: %v", err)
	}
	s3.mu.Lock()
	putCountAfterPDF := len(s3.putReqs)
	s3.mu.Unlock()
	if putCountAfterPDF != 2 {
		t.Fatalf("PDF drop must not upload to image host, puts=%d", putCountAfterPDF)
	}
}

func TestImageHostFallbackAndStrictError(t *testing.T) {
	s3, srv := newMockImageS3(t)
	s3.failPut = true
	a, tt := newTestApp(t)
	a.pluginsUI.sync = true
	sp := a.state.ActiveSpace()

	installImageHostViaUI(t, a, tt)
	configureImageHostS3(t, a, tt, srv.URL, "", true, true, true)

	if err := vault.Write(sp.Root, "Notes.md", "# Notes\n\nBody"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Notes.md")
	tt.Frame()

	ed := a.doc.ed
	end := ed.Len()
	ed.SetSelection(end, end)
	ed.RequestFocus = true
	tt.Frame()

	// Fallback=true saves locally into assets/ when upload fails.
	tt.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, encodePNG(t, 16, 16)))))
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	if !slices.Contains(blockList(a), "image:assets/粘贴的图片.png") {
		t.Fatalf("expected local fallback block, got %q", blockList(a))
	}
	if !tt.HasText("已回退保存到本地") {
		t.Fatalf("expected fallback toast, got %q", tt.Texts())
	}

	// Fallback=false inserts nothing and writes no extra asset file on upload failure.
	a.openPluginSettings(imageHostPluginID)
	tt.Frame()
	*a.pluginsUI.settings.boolBufs["fallbackLocal"] = false
	a.savePluginSettings()
	a.pluginsUI.settings.open = false
	tt.Frame()

	before := blockList(a)
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	if !slices.Equal(blockList(a), before) {
		t.Fatalf("expected no block inserted when fallbackLocal=false, got %q", blockList(a))
	}
	if _, err := os.Stat(filepath.Join(sp.Root, "assets", "粘贴的图片-2.png")); !os.IsNotExist(err) {
		t.Fatalf("expected no fallback asset saved when fallbackLocal=false, err=%v", err)
	}
}

func TestImageHostMigrateDocumentImagesAndCleanup(t *testing.T) {
	s3, srv := newMockImageS3(t)
	a, tt := newTestApp(t)
	a.pluginsUI.sync = true
	var trashed []string
	a.trashFile = func(p string) error {
		trashed = append(trashed, filepath.ToSlash(p))
		return os.RemoveAll(p)
	}
	sp := a.state.ActiveSpace()

	installImageHostViaUI(t, a, tt)
	configureImageHostS3(t, a, tt, srv.URL, srv.URL+"/media", true, true, true)

	pngBytes := encodePNG(t, 20, 20)
	if err := os.MkdirAll(filepath.Join(sp.Root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sp.Root, "assets", "shot.png"), pngBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sp.Root, "assets", "shared.png"), pngBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Notes.md", "# Notes\n\n![shot](assets/shot.png)\n\n![shared](assets/shared.png)\n"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Other.md", "# Other\n\n![shared](assets/shared.png)\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Notes.md")
	tt.Frame()

	p := a.findInstalledPlugin(imageHostPluginID)
	a.runPluginCommand(p, "upload-doc-images", nil, false)
	tt.Frame()

	s3.mu.Lock()
	puts := len(s3.putReqs)
	s3.mu.Unlock()
	if puts != 2 {
		t.Fatalf("expected 2 uploaded images during migration, got %d", puts)
	}
	for _, b := range blockList(a) {
		if strings.HasPrefix(b, "image:assets/") {
			t.Fatalf("expected all local image blocks migrated to remote URLs, got %q", blockList(a))
		}
	}

	// Unreferenced assets/shot.png is trashed; shared assets/shared.png is preserved.
	if _, err := os.Stat(filepath.Join(sp.Root, "assets", "shot.png")); !os.IsNotExist(err) {
		t.Fatalf("expected unreferenced shot.png to be trashed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(sp.Root, "assets", "shared.png")); err != nil {
		t.Fatalf("expected shared.png referenced by Other.md to stay intact: %v", err)
	}
	if len(trashed) != 1 || !strings.HasSuffix(trashed[0], "/assets/shot.png") {
		t.Fatalf("unexpected trashed paths: %q", trashed)
	}

	// Single undo step restores the local URLs inside the editor.
	a.doc.ed.SetSelection(0, 0)
	a.doc.ed.RequestFocus = true
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	if !slices.Contains(blockList(a), "image:assets/shot.png") {
		t.Fatalf("expected one undo step to restore image URLs, got %q", blockList(a))
	}
}

func TestRemoteImageLazyFetchAndCache(t *testing.T) {
	s3, srv := newMockImageS3(t)
	pngBytes := encodePNG(t, 32, 24)
	s3.mu.Lock()
	s3.objects["/cdn/remote.png"] = pngBytes
	s3.mu.Unlock()

	a, tt := newTestApp(t)
	updates := make(chan func(), 16)
	a.search.async = true
	a.runAsync = func(fn func()) { updates <- fn }
	a.pluginsUI.httpClient = srv.Client()
	t.Cleanup(func() {
		a.search.async = false
		a.runAsync = nil
	})

	sp := a.state.ActiveSpace()
	remoteURL := srv.URL + "/cdn/remote.png"
	if err := vault.Write(sp.Root, "Synced.md", "# Synced\n\n![pic]("+remoteURL+")\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Synced.md")
	tt.Frame()

	res1 := a.imageResourceFor(a.doc, remoteURL)
	if !res1.Loading {
		t.Fatalf("expected Loading=true on first remote image request, got %#v", res1)
	}

	// Wait for the background download callback and apply it on main thread.
	fn := <-updates
	fn()
	tt.Frame()

	// Cache file lives strictly under ConfigDir/cache/remote-images, not inside Space.
	cachePath := a.remoteImageCachePath(remoteURL)
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("expected cached remote image at %q: %v", cachePath, err)
	}
	if _, err := os.Stat(filepath.Join(sp.Root, "assets")); !os.IsNotExist(err) {
		t.Fatal("remote image cache must not write into Space folder")
	}

	// Switch to sync decode for final assertion of cached bitmap.
	a.search.async = false
	res2 := a.imageResourceFor(a.doc, remoteURL)
	if res2.Bitmap == nil || res2.Width != 32 || res2.Height != 24 || res2.Local {
		t.Fatalf("expected cached 32x24 bitmap with Local=false, got %#v", res2)
	}

	s3.mu.Lock()
	gets := len(s3.getReqs)
	s3.mu.Unlock()
	if gets != 1 {
		t.Fatalf("expected exactly 1 HTTP GET, got %d", gets)
	}
}

func TestImageHostEnglishLocalization(t *testing.T) {
	regSrv := newMockRegistryServer(t, "")
	a, tt := newTestApp(t)
	a.pluginsUI.registryURL = regSrv.URL + "/index.json"
	a.pluginsUI.httpClient = regSrv.Client()
	a.pluginsUI.sync = true
	i18n.SetLanguage("en")
	t.Cleanup(func() { i18n.SetLanguage("zh-Hans") })

	a.openPlugins()
	settle(tt)
	if !tt.HasText("Image Host") {
		t.Fatalf("English catalog missing Image Host: %q", tt.Texts())
	}
	if err := tt.Click("Install Image Host"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Download and Install") {
		t.Fatalf("English dialog missing Download and Install: %q", tt.Texts())
	}
	if err := tt.Click("Download and Install"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Auto-upload pasted or dropped images") || !tt.HasText("Public Base URL (Optional)") {
		t.Fatalf("English image-host settings missing labels: %q", tt.Texts())
	}
}
