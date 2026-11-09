package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/prefs"
	"github.com/Ciao1019/ArcMD/internal/update"
	"github.com/egoist/mygo/ui"
)

// fakeUpdates answers checks with release, or err.
type fakeUpdates struct {
	disabled bool
	release  *fakeRelease
	err      error
	checks   int
}

func (f *fakeUpdates) Enabled() bool { return !f.disabled }
func (f *fakeUpdates) Check(context.Context) (updateRelease, error) {
	f.checks++
	if f.err != nil || f.release == nil {
		return nil, f.err
	}
	return f.release, nil
}

type fakeRelease struct {
	version, notes string
	installs       int
	err            error
}

func (r *fakeRelease) Version() string { return r.version }
func (r *fakeRelease) Notes() string   { return r.notes }
func (r *fakeRelease) Install(_ context.Context, progress func(done, total int64)) error {
	r.installs++
	progress(512, 1024)
	progress(1024, 1024)
	return r.err
}

// withUpdates gives a test app a fake update service, a relaunch it
// counts and pages it records.
func withUpdates(a *App, f *fakeUpdates) (relaunched *int, opened *[]string) {
	relaunched, opened = new(int), new([]string)
	a.updates.svc = f
	a.updates.relaunch = func() { *relaunched++ }
	a.updates.openURL = func(url string) { *opened = append(*opened, url) }
	return
}

func TestUpdateBackgroundDownloadsAndRestarts(t *testing.T) {
	a, tt := newTestApp(t)
	f := &fakeUpdates{release: &fakeRelease{version: "0.2.0"}}
	relaunched, _ := withUpdates(a, f)
	tt.Frame()
	if f.checks != 0 {
		t.Fatal("no check at launch: it waits", update.FirstCheck)
	}
	tt.Advance(update.FirstCheck)
	tt.Frame()
	if f.checks != 1 || f.release.installs != 1 {
		t.Fatalf("checks %d, installs %d: a background check downloads the update", f.checks, f.release.installs)
	}
	tt.Advance(time.Second)
	if !tt.HasText("更新已准备就绪！") || !tt.HasText("点击重新启动") {
		t.Fatalf("the ready card shows above the spaces: %q", tt.Texts())
	}
	tt.Click("更新已准备就绪！")
	tt.Frame()
	if *relaunched != 1 {
		t.Fatal("clicking the card restarts")
	}
	if a.prefs.PendingUpdate != "0.2.0" {
		t.Fatalf("pending update %q", a.prefs.PendingUpdate)
	}
	// Eight hours on, a downloaded update is not checked again.
	a.updates.next = tt.Now() // the next check falls due
	tt.Frame()
	if f.checks != 1 {
		t.Fatalf("checked %d times with an update waiting", f.checks)
	}
}

func TestUpdateFoundWithoutAutomaticDownload(t *testing.T) {
	a, tt := newTestApp(t)
	a.prefs.NoAutoUpdate = true
	f := &fakeUpdates{release: &fakeRelease{version: "0.3.0", notes: "## 新功能\n\n- 自动更新"}}
	withUpdates(a, f)
	tt.Frame()
	tt.Advance(update.FirstCheck)
	tt.Frame()
	tt.Advance(time.Second)
	if f.release.installs != 0 {
		t.Fatal("automatic downloads are off")
	}
	if !tt.HasText("ArcMD 有新版本") || !tt.HasText("ArcMD 0.3.0") {
		t.Fatalf("the available card: %q", tt.Texts())
	}
	tt.Click("ArcMD 有新版本")
	tt.Frame()
	if !a.updates.window || !tt.HasText("• 自动更新") || !tt.HasText("下载并安装") {
		t.Fatalf("the update window with the notes: %q", tt.Texts())
	}
	tt.Click("下载并安装")
	tt.Frame()
	if f.release.installs != 1 || !tt.HasText("安装并重新启动") {
		t.Fatalf("downloaded, ready to install: %q", tt.Texts())
	}
	tt.Click("以后再说")
	tt.Frame()
	if a.updates.window || !tt.HasText("更新已准备就绪！") {
		t.Fatalf("later leaves the ready card: %q", tt.Texts())
	}
}

func TestUpdateManualCheck(t *testing.T) {
	a, tt := newTestApp(t)
	f := &fakeUpdates{}
	withUpdates(a, f)
	tt.Frame()
	a.checkForUpdates()
	tt.Frame()
	if !tt.HasText("ArcMD 已是最新版本") {
		t.Fatalf("up to date: %q", tt.Texts())
	}
	tt.Click("好")
	tt.Frame()
	if a.updates.window {
		t.Fatal("the window closes")
	}

	f.err = errors.New("offline")
	a.checkForUpdates()
	tt.Frame()
	if !tt.HasText("无法检查更新：offline") || !tt.HasText("手动下载") {
		t.Fatalf("failed check: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.updates.window {
		t.Fatal("Esc closes the window")
	}

	f.err, f.release = nil, &fakeRelease{version: "0.2.0"}
	a.checkForUpdates()
	tt.Frame()
	if !tt.HasText("ArcMD 0.2.0") || !tt.HasText("当前版本 "+Version) || !tt.HasText("有新版本可用") {
		t.Fatalf("found: %q", tt.Texts())
	}
	tt.Click("以后再说")
	tt.Frame()
	if tt.HasText("ArcMD 有新版本") {
		t.Fatal("put off, the update leaves no banner")
	}
}

func TestUpdateDisabledBuild(t *testing.T) {
	a, tt := newTestApp(t)
	f := &fakeUpdates{disabled: true}
	_, opened := withUpdates(a, f)
	tt.Frame()
	tt.Advance(update.FirstCheck + time.Second)
	tt.Frame()
	if f.checks != 0 {
		t.Fatal("a build that cannot update never checks")
	}
	a.checkForUpdates()
	tt.Frame()
	if !tt.HasText("前往下载页面") {
		t.Fatalf("disabled: %q", tt.Texts())
	}
	tt.Click("前往下载页面")
	tt.Frame()
	if len(*opened) != 1 || (*opened)[0] != update.ReleaseURL("") {
		t.Fatalf("opened %q", *opened)
	}
}

func TestUpdateToastWhileSidebarHidden(t *testing.T) {
	a, tt := newTestApp(t)
	relaunched, _ := withUpdates(a, &fakeUpdates{})
	a.updates.ready = "0.2.0"
	tt.Frame()
	if a.updates.toast != nil {
		t.Fatal("the banner says it while the sidebar shows")
	}
	a.toggleSidebar()
	tt.Frame()
	tt.Advance(time.Second)
	if a.updates.toast == nil || !tt.HasText("重新启动") {
		t.Fatalf("the toast: %q", tt.Texts())
	}
	tt.Click("关闭")
	tt.Frame()
	if a.updates.toast != nil {
		t.Fatal("✕ closes the toast")
	}
	a.toggleSidebar()
	tt.Frame()
	a.toggleSidebar()
	tt.Frame()
	if a.updates.toast != nil {
		t.Fatal("a notice toasts once")
	}
	a.updates.ready = "0.2.1"
	tt.Frame()
	tt.Advance(time.Second)
	if a.updates.toast == nil {
		t.Fatal("a newer update is news again")
	}
	tt.Click("重新启动")
	tt.Frame()
	if *relaunched != 1 {
		t.Fatal("the toast's button restarts")
	}
}

func TestUpdatedCardAfterRestart(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{ConfigDir: filepath.Join(dir, "config"), DocumentsDir: filepath.Join(dir, "docs")}
	store := prefs.Store{Path: filepath.Join(cfg.ConfigDir, "prefs.json")}
	if err := store.Save(&prefs.Prefs{PendingUpdate: Version}); err != nil {
		t.Fatal(err)
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, opened := withUpdates(a, &fakeUpdates{})
	tt := ui.NewTester(a.View, 1200, 760)
	tt.SetTitleBar(ui.TitleBar{Height: 46, Left: 78})
	tt.Frame()
	tt.Advance(time.Second)
	if !tt.HasText("ArcMD 已更新！") || !tt.HasText("新功能") {
		t.Fatalf("the updated card: %q", tt.Texts())
	}
	if p, _ := store.Load(); p.PendingUpdate != "" {
		t.Fatal("the pending update is cleared")
	}
	tt.Click("新功能")
	tt.Frame()
	if len(*opened) != 1 || (*opened)[0] != update.ReleaseURL(Version) {
		t.Fatalf("opened %q", *opened)
	}
	tt.Click("关闭")
	tt.Frame()
	if tt.HasText("ArcMD 已更新！") {
		t.Fatal("✕ puts the card away")
	}

	// A restart that did not land on the new version says nothing.
	if err := store.Save(&prefs.Prefs{PendingUpdate: "9.9.9"}); err != nil {
		t.Fatal(err)
	}
	b, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if b.updates.updatedTo != "" {
		t.Fatal("not updated")
	}
}

func TestArrowCutsOutOfHeart(t *testing.T) {
	area := func(pts [][2]float32) float32 {
		var s float32
		for i := range pts {
			p, q := pts[i], pts[(i+1)%len(pts)]
			s += p[0]*q[1] - q[0]*p[1]
		}
		return s / 2
	}
	var heart [][2]float32
	for _, s := range heartSegments {
		heart = append(heart, s[0])
	}
	arrow := arrowOutline(0)
	if h, w := area(heart), area(arrow); h*w >= 0 || -w > h/4 {
		t.Fatalf("heart %v, arrow %v: the arrow must run the other way, within the heart", h, w)
	}
	for _, p := range arrow {
		if p[0] < 60 || p[0] > 190 || p[1] < 60 || p[1] > 170 {
			t.Fatalf("arrow point %v outside the heart's middle", p)
		}
	}
	if dy, scale, alpha := heartPose(0); dy != 0 || scale != 1 || alpha != 0 {
		t.Fatal("at rest")
	}
	if dy, _, _ := heartPose(15.0 / 120); dy != -10 {
		t.Fatalf("lifted %v", dy)
	}
	if dy, _, _ := heartPose(45.0 / 120); dy != 17.5 {
		t.Fatalf("dipped %v", dy)
	}
	if _, scale, alpha := heartPose(1); scale != 1.29 || alpha != 0 {
		t.Fatal("the ripple ends grown and gone")
	}
}

func TestSnapshotUpdates(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the update banners")
	for _, name := range []string{"ready", "available", "updated", "toast", "window-found", "window-ready", "ready@dark", "updated@dark", "settings"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			withUpdates(a, &fakeUpdates{})
			if base, ok := strings.CutSuffix(name, "@dark"); ok {
				name = base + "-dark"
				tt.SetDark(true)
			}
			rel := &fakeRelease{version: "0.2.0", notes: "## 新功能\n\n- 自动更新：在后台下载新版本\n- 侧边栏的更新提示\n\n## 修复\n\n- 修复了分屏时的滚动问题"}
			switch name {
			case "ready", "ready-dark":
				a.updates.ready = "0.2.0"
			case "available":
				a.updates.found, a.updates.release = "0.2.0", rel
			case "updated", "updated-dark":
				a.updates.updatedTo = Version
			case "toast":
				a.updates.ready = "0.2.0"
				a.toggleSidebar()
			case "window-found":
				a.updates.release, a.updates.window, a.updates.phase = rel, true, phaseFound
			case "window-ready":
				a.updates.release, a.updates.ready, a.updates.window, a.updates.phase = rel, "0.2.0", true, phaseReady
			case "settings":
				a.openSettings(tabGeneral)
			}
			tt.Frame()
			tt.Advance(1200 * time.Millisecond)
			tt.Frame()
			savePNG(t, filepath.Join(dir, "update-"+name+".png"), tt)
		})
	}
}
