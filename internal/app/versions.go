package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/history"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// Version history (package history): saving a document takes a version of
// it, at most one per ten minutes of editing, and the first save of a
// session keeps the version the document had before. The history window
// lists them, shows what each differs from the document now, and brings one
// back as one step of the editor's history.

// versionsState is the history window's.
type versionsState struct {
	open     bool
	err      error
	versions []history.Version
	sel      int
	// for is the document the versions are of; shown the version whose
	// difference diff holds.
	for_  *document
	shown string
	doc   *model.Document
	diff  []history.Line
	added int
	gone  int
}

// recordVersion takes a version of the document as just written; before
// its first write this session, the version it had on disk is kept.
func (d *document) recordVersion(before string) {
	if d.historyOff {
		return
	}
	root := d.space.Root
	if !d.historyStarted && before != "" {
		d.historyStarted = true
		at := time.Now().Add(-time.Second)
		if info, err := os.Stat(d.fullPath()); err == nil && info.ModTime().Before(at) {
			at = info.ModTime()
		}
		_ = history.Record(root, d.rel, before, at, true)
	}
	d.historyStarted = true
	_ = history.Record(root, d.rel, d.diskText, time.Now(), false)
}

// fullPath is the document's file.
func (d *document) fullPath() string {
	p, _ := vault.Resolve(d.space.Root, d.rel)
	return p
}

// openVersions shows the history of the open document.
func (a *App) openVersions() {
	d := a.doc
	if d == nil || d.loadErr != nil || vault.IsMarkdown(d.rel) {
		a.notify("这篇文档还没有版本历史。")
		return
	}
	a.flush()
	v := &a.versions
	*v = versionsState{open: true, for_: d}
	v.versions, v.err = history.List(d.space.Root, d.rel)
}

// selectVersion reads the version at i and compares it with the document
// as it is.
func (a *App) selectVersion(i int) {
	v := &a.versions
	if i < 0 || i >= len(v.versions) || v.for_ == nil {
		return
	}
	v.sel = i
	ver := v.versions[i]
	if v.shown == ver.File {
		return
	}
	v.shown, v.doc, v.diff, v.err = ver.File, nil, nil, nil
	text, err := history.Read(v.for_.space.Root, v.for_.rel, ver)
	if err == nil {
		v.doc, err = model.Decode(text)
	}
	if err != nil {
		v.err = err
		return
	}
	now := strings.Split(v.for_.ed.Committed().PlainText(), "\n")
	then := strings.Split(v.doc.PlainText(), "\n")
	v.diff = history.Diff(now, then)
	v.added, v.gone = 0, 0
	for _, l := range v.diff {
		switch l.Op {
		case history.Added:
			v.added++
		case history.Removed:
			v.gone++
		}
	}
}

// restoreVersion puts the version shown in place of the document, as one
// step of the editor's history.
func (a *App) restoreVersion() {
	v := &a.versions
	if v.doc == nil || v.for_ != a.doc {
		return
	}
	at := v.versions[v.sel].Time
	a.doc.ed.Engine.ReplaceDocument(v.doc)
	a.doc.ed.RequestFocus = true
	v.open = false
	a.notify("已恢复到 %s 的版本，可用 ⌘Z 撤销。", versionTime(at, time.Now()))
}

// versionTime says when a version was taken, as people say it.
func versionTime(at, now time.Time) string {
	y1, m1, d1 := at.Date()
	y2, m2, d2 := now.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return i18n.T("今天 ") + at.Format("15:04")
	case y1 == y2 && m1 == m2 && d1 == d2-1:
		return i18n.T("昨天 ") + at.Format("15:04")
	case y1 == y2:
		return fmt.Sprintf(i18n.T("%d月%d日 %s"), m1, d1, at.Format("15:04"))
	}
	return fmt.Sprintf(i18n.T("%d年%d月%d日 %s"), y1, m1, d1, at.Format("15:04"))
}

// versionsView is the history window.
func (a *App) versionsView(c *ui.Context) {
	v := &a.versions
	if !v.open {
		return
	}
	if v.for_ != a.doc {
		v.open = false
		return
	}
	t := c.Theme()
	if len(v.versions) > 0 && v.shown == "" {
		a.selectVersion(0)
	}
	ui.Modal(c.Key("versions"), &v.open, func() {
		ui.Column(c.Key("versions-body")).Size(760, 500).Gap(12).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Icon(c, iconHistory).FontSize(16).TextColor(t.TextMuted)
				ui.Text(c, i18n.T("版本历史 · ")+v.for_.title()).FontSize(16).FontWeight(600).Grow(1).SingleLine().MinWidth(0)
				if v.doc != nil {
					if ui.PrimaryButton(c.Key("version-restore"), i18n.T("恢复此版本")).Clicked() {
						a.restoreVersion()
					}
				}
			})
			if len(v.versions) == 0 {
				ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
					switch {
					case v.err != nil:
						ui.Text(c, i18n.T("读取版本历史失败。")+v.err.Error()).TextColor(t.Danger)
					default:
						ui.Text(c, i18n.T("还没有历史版本")).FontSize(15)
						ui.Text(c, i18n.T("编辑并保存后，ArcMD 会自动保留版本（每 10 分钟至多一个，最多 100 个），保存在空间文件夹的 .arcmd/history 里。")).
							FontSize(12).TextColor(t.TextMuted).MaxWidth(420).TextAlign(ui.Center)
					}
				})
				return
			}
			ui.Row(c).Grow(1).MinHeight(0).Gap(12).AlignItems(ui.Stretch).Children(func() {
				ui.Scroll(c.Key("version-list")).Width(210).Children(func() {
					now := time.Now()
					for i, ver := range v.versions {
						on := i == v.sel
						row := ui.ButtonBase(c.Key("version:"+ver.File)).Padding(8, 10).Radius(8).Gap(2).Label(i18n.T("版本 ") + versionTime(ver.Time, now)).
							Cursor(ui.CursorPointer).Justify(ui.Start)
						switch {
						case on:
							row.Background(t.Accent.Alpha(0.14))
						case row.Hovered():
							row.Background(hoverFill(t))
						}
						row.Children(func() {
							ui.Column(c).Grow(1).Gap(2).AlignItems(ui.Start).Children(func() {
								ui.Text(c, versionTime(ver.Time, now)).FontSize(13).FontWeight(500)
								note := strings.TrimSpace(ago(ver.Time, now))
								if ver.Kept {
									note += i18n.T(" · 打开时的版本")
								}
								ui.Text(c, note).FontSize(11).TextColor(t.TextMuted)
							})
						})
						if row.Clicked() {
							a.selectVersion(i)
						}
					}
				})
				ui.Box(c).Width(1).Background(t.Text.Alpha(0.08))
				ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(func() {
					switch {
					case v.err != nil:
						ui.Text(c, i18n.T("无法读取这个版本。")+v.err.Error()).TextColor(t.Danger)
						return
					case v.doc == nil:
						return
					case v.added == 0 && v.gone == 0:
						ui.Text(c, i18n.T("与当前内容相同。")).FontSize(12).TextColor(t.TextMuted)
					default:
						ui.Text(c, fmt.Sprintf(i18n.T("与当前内容相比：恢复后增加 %d 行、删除 %d 行"), v.added, v.gone)).FontSize(12).TextColor(t.TextMuted)
					}
					pg := pageStyle(a.paperDark)
					ui.Scroll(c.Key("version-diff:" + v.shown)).Grow(1).MinHeight(0).Children(func() {
						ui.Column(c).Gap(1).Children(func() {
							for i, l := range v.diff {
								if l.Op == history.Same && !nearChange(v.diff, i) {
									continue
								}
								row := ui.Row(c.Key(i)).Padding(3, 8).Radius(4).Gap(8)
								sign, color := " ", t.Text
								switch l.Op {
								case history.Added:
									sign, color = "+", t.Text
									row.Background(pg.Callouts["green"].Fill)
								case history.Removed:
									sign, color = "−", t.TextMuted
									row.Background(pg.Callouts["red"].Fill)
								default:
									color = t.TextMuted
								}
								row.Children(func() {
									ui.Text(c, sign).FontSize(13).TextColor(t.TextMuted).Width(10)
									text := l.Text
									if text == "" {
										text = " "
									}
									line := ui.Text(c, text).FontSize(13).TextColor(color).Grow(1).MinWidth(0)
									if l.Op == history.Removed {
										line.Strikethrough()
									}
								})
							}
						})
					})
				})
			})
		})
	})
}

// nearChange reports whether a line is within two lines of a change, the
// context the difference shows around changes.
func nearChange(diff []history.Line, i int) bool {
	for j := max(0, i-2); j <= min(len(diff)-1, i+2); j++ {
		if diff[j].Op != history.Same {
			return true
		}
	}
	return false
}
