package app

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/update"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Software updates, as Arc's and Cra's: ArcMD checks its GitHub releases
// in the background, ten seconds after launch and every eight hours,
// downloads a new version on its own (unless the user turned that off) and
// says so in a card above the spaces at the foot of the sidebar, or in a
// toast while the sidebar is hidden. Clicking it restarts into the new
// version, whose first launch says "ArcMD 已更新！". "检查更新…" opens the
// update window. MyGo's Updater downloads, checks the signature and
// replaces the app; the views are in updateview.go.

// updateService finds and installs new versions: MyGo's Updater, or a
// fake in tests.
type updateService interface {
	// Enabled reports whether this build can update itself.
	Enabled() bool
	// Check returns the newer release, or nil when ArcMD is up to date.
	Check(ctx context.Context) (updateRelease, error)
}

// updateRelease is a newer version of ArcMD.
type updateRelease interface {
	Version() string
	// Notes are the release notes, in Markdown.
	Notes() string
	// Install downloads the release and puts it in place of the app; it
	// runs at the next launch.
	Install(ctx context.Context, progress func(done, total int64)) error
}

// mygoUpdates is MyGo's Updater.
type mygoUpdates struct{}

func (mygoUpdates) Enabled() bool { return mygo.Updater.Enabled() }

func (mygoUpdates) Check(ctx context.Context) (updateRelease, error) {
	up, err := mygo.Updater.Check(ctx)
	if err != nil || up == nil {
		return nil, err
	}
	return mygoRelease{up}, nil
}

type mygoRelease struct{ up *mygo.Update }

func (r mygoRelease) Version() string { return r.up.Version }
func (r mygoRelease) Notes() string   { return r.up.Notes }
func (r mygoRelease) Install(ctx context.Context, progress func(done, total int64)) error {
	return r.up.Install(ctx, progress)
}

// updatePhase is what the update window shows (Cra's UpdaterWindowView).
type updatePhase int

const (
	phaseChecking updatePhase = iota
	phaseUpToDate
	phaseFound
	phaseDownloading
	phaseReady
	phaseFailed
	phaseDisabled
)

// updateState is the updater's state.
type updateState struct {
	svc updateService
	// async runs checks and downloads on goroutines, as the running app
	// does; tests run them at once.
	async bool
	// enabled is svc.Enabled, asked once: it probes the disk.
	enabled, asked bool
	// next is when the next background check is due.
	next time.Time
	// checking and downloading are the work under way; cancel stops it.
	checking, downloading bool
	cancel                context.CancelFunc
	// release is the newer version found, downloading or downloaded;
	// received and total its download's progress.
	release         updateRelease
	received, total int64
	// ready is the version downloaded and waiting for a restart; found the
	// one a background check found while automatic updates are off.
	ready, found string
	// updatedTo is the version this launch was updated to, for the
	// "updated" card, until its ✕.
	updatedTo string
	// dismissed hides the ready banner until the next launch.
	dismissed bool
	// window shows the update window on phase; failure is why it failed.
	window  bool
	phase   updatePhase
	failure string
	// told is the last notice toasted, toast the one showing.
	told, toast *update.Notice
	toastFrames int
	// shown is the banner shown last frame and since when, for its
	// entrance and the heart's first beat.
	shown      string
	shownSince time.Time
	// lastCheck is when the last check answered.
	lastCheck time.Time
	checkErr  error
	// relaunch restarts ArcMD and openURL opens a page in the browser;
	// tests replace them.
	relaunch func()
	openURL  func(url string)
}

// startUpdates notes, at launch, whether the restart before installed the
// version this ArcMD is.
func (a *App) startUpdates() {
	if v := a.prefs.PendingUpdate; v != "" {
		if v == Version {
			a.updates.updatedTo = Version
		}
		a.prefs.PendingUpdate = ""
		if !a.prefsBroken {
			a.savePrefs()
		}
	}
}

// updatesEnabled reports whether this build can update itself.
func (a *App) updatesEnabled() bool {
	u := &a.updates
	if u.svc == nil {
		return false
	}
	if !u.asked {
		u.enabled, u.asked = u.svc.Enabled(), true
	}
	return u.enabled
}

// updateTick runs the background checks.
func (a *App) updateTick(c *ui.Context) {
	u := &a.updates
	if !a.updatesEnabled() {
		return
	}
	now := c.Now()
	if u.next.IsZero() {
		u.next = now.Add(update.FirstCheck)
	}
	if update.Due(u.next, now) {
		u.next = now.Add(update.Interval)
		if !u.checking && !u.downloading && u.ready == "" {
			a.checkUpdates(false)
		}
	}
	c.After(u.next.Sub(now))
}

// run does work off the main thread in the running app, and done back on
// it; tests do both at once.
func (a *App) runUpdate(work func() func()) {
	if !a.updates.async || a.win == nil {
		work()()
		return
	}
	go func() {
		done := work()
		a.win.Update(done)
	}()
}

// checkForUpdates is "检查更新…": the update window, checking at once.
func (a *App) checkForUpdates() {
	u := &a.updates
	u.window = true
	switch {
	case u.ready != "":
		u.phase = phaseReady
	case !a.updatesEnabled():
		u.phase = phaseDisabled
	case u.downloading:
		u.phase = phaseDownloading
	case u.found != "" && u.release != nil:
		u.phase = phaseFound
	default:
		u.phase = phaseChecking
		if !u.checking {
			a.checkUpdates(true)
		}
	}
}

// checkUpdates asks for a newer release; manual is the user's own check,
// which the window answers.
func (a *App) checkUpdates(manual bool) {
	u := &a.updates
	u.checking = true
	svc := u.svc
	a.runUpdate(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		rel, err := svc.Check(ctx)
		return func() { a.checked(rel, err) }
	})
}

// checked takes a check's answer.
func (a *App) checked(rel updateRelease, err error) {
	u := &a.updates
	u.checking = false
	u.lastCheck, u.checkErr = a.now, err
	asked := u.window && u.phase == phaseChecking
	switch {
	case err != nil:
		if asked {
			u.phase, u.failure = phaseFailed, i18n.T("无法检查更新：")+err.Error()
		}
	case rel == nil:
		if asked {
			u.phase = phaseUpToDate
		}
	case asked:
		// The user checked: the window offers the download.
		u.release = rel
		u.phase = phaseFound
	case !a.prefs.NoAutoUpdate:
		u.release = rel
		a.downloadUpdate()
	default:
		u.release, u.found = rel, rel.Version()
	}
}

// downloadUpdate downloads the release found, which a restart installs.
func (a *App) downloadUpdate() {
	u := &a.updates
	if u.release == nil || u.downloading {
		return
	}
	rel := u.release
	u.downloading = true
	u.received, u.total = 0, 0
	if u.window {
		u.phase = phaseDownloading
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel
	var shown atomic.Int64
	shown.Store(-1)
	a.runUpdate(func() func() {
		err := rel.Install(ctx, func(done, total int64) {
			// Tell the window as the percentage changes, not every block.
			pct := int64(0)
			if total > 0 {
				pct = done * 100 / total
			}
			if !a.updates.async || a.win == nil || shown.Swap(pct) == pct {
				if !a.updates.async || a.win == nil {
					u.received, u.total = done, total
				}
				return
			}
			a.win.Update(func() { u.received, u.total = done, total })
		})
		return func() { a.downloaded(rel, err) }
	})
}

// downloaded takes the end of a download.
func (a *App) downloaded(rel updateRelease, err error) {
	u := &a.updates
	u.downloading = false
	if u.cancel != nil {
		u.cancel()
		u.cancel = nil
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		if u.window && u.phase == phaseDownloading {
			u.phase, u.failure = phaseFailed, i18n.T("下载更新失败：")+err.Error()
		}
		return
	}
	u.ready, u.found = rel.Version(), ""
	u.dismissed = false
	if u.window {
		u.phase = phaseReady
	}
}

// installUpdate restarts into the downloaded version, or downloads the one
// found first. Documents are saved as on quitting: a document that cannot
// be saved keeps ArcMD open.
func (a *App) installUpdate() {
	u := &a.updates
	if u.ready == "" {
		a.downloadUpdate()
		return
	}
	if !a.flush() {
		return
	}
	a.prefs.PendingUpdate = u.ready
	a.savePrefs()
	if u.relaunch != nil {
		u.relaunch()
		return
	}
	mygo.App.Relaunch()
}

// closeUpdateWindow is the window's Later, Cancel and Close: a download
// under way stops, and an update found and put off leaves the banner.
func (a *App) closeUpdateWindow() {
	u := &a.updates
	if u.downloading && u.cancel != nil {
		u.cancel()
		u.downloading = false
	}
	if u.phase == phaseFound {
		u.found = ""
		if u.ready == "" {
			u.release = nil
		}
	}
	u.window = false
}

// updateNotice is what the update banner says now.
func (a *App) updateNotice() *update.Notice {
	u := &a.updates
	return update.Current(u.ready, u.found, u.dismissed)
}

// actOnNotice is a click on the banner or the toast's button.
func (a *App) actOnNotice(n update.Notice) {
	u := &a.updates
	u.toast = nil
	if n.Kind == update.Ready {
		a.installUpdate()
		return
	}
	u.window, u.phase = true, phaseFound
}

// dismissBanner is a banner's ✕: the ready card hides until the next
// launch, a found update is put off, the updated card goes.
func (a *App) dismissBanner() {
	u := &a.updates
	switch {
	case u.ready != "":
		u.dismissed = true
	case u.found != "":
		u.found, u.release = "", nil
	default:
		u.updatedTo = ""
	}
}

// openUpdatePage opens a page about ArcMD's releases in the browser.
func (a *App) openUpdatePage(url string) {
	if a.updates.openURL != nil {
		a.updates.openURL(url)
		return
	}
	mygo.Shell.OpenExternal(url)
}

// updateToastTick toasts the banner's news while the sidebar is hidden.
func (a *App) updateToastTick() {
	u := &a.updates
	n := a.updateNotice()
	if n == nil {
		u.toast = nil
		return
	}
	visible := !a.focus && (!a.folded() || a.library.open)
	if t := update.Toast(n, u.told, visible); t != nil {
		u.toast, u.told, u.toastFrames = t, t, 0
	}
	if u.toast != nil && *u.toast != *n {
		u.toast = nil
	}
}

// toggleAutoUpdate turns the automatic download of new versions on or off.
func (a *App) toggleAutoUpdate() {
	a.prefs.NoAutoUpdate = !a.prefs.NoAutoUpdate
	a.savePrefs()
	u := &a.updates
	if !a.prefs.NoAutoUpdate && u.found != "" && u.release != nil {
		// Turned on with an update found: download it now.
		u.found = ""
		a.downloadUpdate()
	}
}

// updateSummary says where the updates stand, for the settings.
func (a *App) updateSummary() string {
	u := &a.updates
	switch {
	case !a.updatesEnabled():
		return i18n.T("开发版本或无法写入的安装位置不会自动更新。")
	case u.ready != "":
		return "ArcMD " + u.ready + i18n.T(" 已下载，重新启动后安装。")
	case u.downloading:
		return i18n.T("正在下载新版本…")
	case u.found != "":
		return "ArcMD " + u.found + i18n.T(" 可以更新。")
	case u.checking:
		return i18n.T("正在检查更新…")
	case u.lastCheck.IsZero():
		return i18n.T("从 GitHub Releases 获取新版本。")
	case u.checkErr != nil:
		return i18n.T("上次检查失败：") + u.checkErr.Error()
	}
	return i18n.T("上次检查：") + u.lastCheck.Format(i18n.T("1月2日 15:04")) + i18n.T("，已是最新版本。")
}
