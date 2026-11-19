package app

import (
	"archive/zip"
	"bytes"
	"crypto/md5"
	"encoding/hex"
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
	if err := tt.Click("安装并启用"); err != nil {
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
	a.pluginsUI.httpClient = srv.Client()
	a.trashFile = os.RemoveAll

	installCloudSyncViaUI(t, a, tt)

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
	a.pluginsUI.httpClient = srv.Client()

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
	if !tt.HasText("This plugin requests the following permissions:") || !tt.HasText("Install & Enable") {
		t.Fatalf("English permission prompt missing labels: %q", tt.Texts())
	}
	if err := tt.Click("Install & Enable"); err != nil {
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
			a.openPlugins()
			settle(tt)
			_ = tt.Click("安装 云端同步")
		})
		render("plugin-settings-s3"+suffix, dark, func(a *App, tt *ui.Tester) {
			a.openPlugins()
			settle(tt)
			_ = tt.Click("安装 云端同步")
			tt.Frame()
			_ = tt.Click("安装并启用")
		})
	}
	render("plugin-settings-webdav", false, func(a *App, tt *ui.Tester) {
		a.openPlugins()
		settle(tt)
		_ = tt.Click("安装 云端同步")
		tt.Frame()
		_ = tt.Click("安装并启用")
		tt.Frame()
		_ = tt.Click("WebDAV")
	})
	render("plugin-history-sheet", false, func(a *App, tt *ui.Tester) {
		a.openPlugins()
		settle(tt)
		_ = tt.Click("安装 云端同步")
		tt.Frame()
		_ = tt.Click("安装并启用")
		tt.Frame()
		a.pluginsUI.settings.open = false
		p := a.findInstalledPlugin(cloudSyncPluginID)
		a.runPluginCommand(p, "sync-status", nil, false)
	})
	render("plugin-card-pink", false, func(a *App, tt *ui.Tester) {
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
		_ = tt.Click("安装并启用")
		tt.Frame()
		a.pluginsUI.settings.open = false
		a.undos = nil
		a.notices = nil
	})
}
