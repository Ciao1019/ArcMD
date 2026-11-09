package app

import (
	"path"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	// sidebarWidth matches the reference Zen profile's 230 CSS-pixel sidebar.
	sidebarWidth = 230
	// slide is how long the sidebar takes to fold and unfold.
	slide = 240 * time.Millisecond
	// peekDelay lets the pointer cross the floating panel's small outer gap.
	peekDelay = 160 * time.Millisecond
)

// Content returns the window's content: the view of the app.
func (a *App) Content() *ui.Content { return ui.View(a.View) }

// View builds the window from the state, every frame.
func (a *App) View(c *ui.Context) {
	a.now, a.reduceMotion = c.Now(), c.Preferences().ReduceMotion
	winW, _ := c.Size()
	if winW > 0 {
		a.narrowTick(winW)
	}
	a.splitResizing = false
	a.carried, a.carrying = a.carrying, nil
	a.syncOpen()
	a.overClosed, a.overClose = a.overClose, place{}
	a.shortcuts(c)
	// Finish swipe and spring state before deriving the chrome and paper,
	// so even the landing frame uses one consistent space and position.
	a.pagerTick(c)
	sp := a.state.ActiveSpace()
	base := c.Theme()
	a.docTheme = base
	th := space.ThemeOf(sp)
	if f := &a.spaceForm; f.open && (f.id == "" || sp != nil && f.id == sp.ID) {
		th = f.theme // the space being made or edited shows its look at once
	}
	a.look = lookOf(th, base.Dark)
	a.paperDark = a.look.Dark
	if !a.spaceForm.open && a.pager.moving() {
		// Paging, the window's tint is the two spaces either side mixed.
		pos := a.pagerShown(c.Now())
		last := float64(len(a.state.Spaces) - 1)
		pos = min(max(pos, 0), last)
		lo := int(pos)
		hi := min(lo+1, int(last))
		x := lookOf(space.ThemeOf(a.state.Spaces[lo]), base.Dark)
		y := lookOf(space.ThemeOf(a.state.Spaces[hi]), base.Dark)
		a.look = mixLooks(x, y, float32(pos-float64(lo)))
	}
	t := spaceTheme(base, a.look)
	c.SetTheme(t)
	a.syncTree()
	a.syncWatches()
	for _, d := range a.residentDocuments() {
		a.withDocument(d, func() { d.autosave(c); a.followMigration(); a.followTitle() })
	}
	for _, n := range a.notices {
		c.Toast(n)
	}
	a.notices = a.notices[:0]
	a.updateTick(c)
	a.updateToastTick()

	frame := ui.Row(c).Fill().AlignItems(ui.Stretch)
	vibrant := c.Vibrancy()
	if vibrant {
		c.Root().Background(ui.Transparent)
	}
	look := a.look
	frame.Draw(func(p *ui.Painter, r ui.Rect) { look.paint(p, r, 0, vibrant) })
	var docked float32
	frame.Children(func() {
		if a.focus {
			a.focusView(c)
			return
		}
		// The docked sidebar slides as it folds: its column narrows while
		// what is in it keeps its width, cut by the column.
		dock := ui.Column(c.Key("dock")).Shrink(0).Clip()
		target := float32(sidebarWidth)
		if a.folded() {
			target = 0
		}
		inner := float32(sidebarWidth)
		if a.library.open {
			// The library takes over the sidebar, widened for its rail,
			// shown even when the sidebar is folded away.
			inner = a.libraryWidth(winW)
			target = inner
		}
		docked = dock.Animate("width", target, slide)
		dock.Width(docked).Hidden(docked < 0.5).Children(func() {
			if docked < 0.5 {
				return
			}
			if a.library.open {
				a.libraryView(c, inner)
				return
			}
			side := ui.Column(c.Key("side")).Width(sidebarWidth).Grow(1)
			// Back from the library, the spaces fade in again, as Arc's.
			pose := ui.Pose{}
			if a.library.frames == 0 {
				pose.Fade = 1
			}
			side.AnimatePose(pose, ui.Animation{Duration: librarySlideIn, Ease: ui.EaseOut})
			if a.library.frames++; a.library.frames == 1 {
				c.AnimationFrame()
			}
			side.Children(func() { a.sidebar(c, false) })
		})
		a.main(c, docked)
	})
	if a.folded() && docked < 0.5 && !a.library.open && !a.focus {
		a.peekSidebar(c)
	} else {
		a.peek = false
		a.peekLeave = time.Time{}
	}
	a.previewView(c)
	a.paletteView(c)
	a.updateToast(c)
	a.settingsView(c)
	a.versionsView(c)
	a.updateWindowView(c)
	if a.captureSplits() {
		a.saveState()
	}
}

// shortcuts handles the window's keys.
func (a *App) shortcuts(c *ui.Context) {
	if a.recording != "" {
		return // the settings take the next key
	}
	switch {
	case a.pressed(c, "palette"):
		a.openPalette()
	case a.pressed(c, "split-add"):
		a.addSplitPane()
	case a.pressed(c, "split-close"):
		if a.split != nil {
			a.closePane(a.split.active)
		}
	case a.pressed(c, "split-next"):
		a.cyclePane()
	case a.pressed(c, "new-doc"):
		a.newDoc(a.currentDir())
	case a.pressed(c, "new-folder"):
		a.newFolder(a.currentDir())
	case a.pressed(c, "sidebar"):
		a.toggleSidebar()
	case a.pressed(c, "focus"):
		a.toggleFocus()
	case a.pressed(c, "source"):
		a.toggleSource()
	case a.pressed(c, "back"):
		a.goHistory(-1)
	case a.pressed(c, "forward"):
		a.goHistory(1)
	case a.pressed(c, "prev-space"), c.Shortcut(0, ui.KeyBack):
		a.stepSpace(-1)
	case a.pressed(c, "next-space"), c.Shortcut(0, ui.KeyForward):
		a.stepSpace(1)
	case a.pressed(c, "reload"):
		a.reload()
	case a.pressed(c, "save"):
		a.flush()
	case a.pressed(c, "settings"):
		a.openSettings(tabGeneral)
	case a.pressed(c, "overview"):
		a.toggleOverview()
	case a.pressed(c, "library"):
		a.toggleLibrary()
	case a.pressed(c, "outline"):
		a.toggleOutline()
	case a.pressed(c, "history"):
		a.openVersions()
	case a.pressed(c, "export-pdf"):
		a.exportPDF()
	}
	keys := []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9}
	for i, k := range keys {
		if i < splitLimit && c.Shortcut(ui.Ctrl|ui.Shift, k) {
			a.focusPane(i, true)
		}
		if c.Shortcut(ui.Cmd, k) || c.Shortcut(ui.Ctrl, k) {
			a.switchSpaceAt(i)
		}
	}
}

// currentDir is the folder new documents go in: the open document's.
func (a *App) currentDir() string {
	if a.doc == nil || a.doc.space != a.state.ActiveSpace() {
		return ""
	}
	if dir := path.Dir(a.doc.rel); dir != "." {
		return dir
	}
	return ""
}

// peekSidebar shows the folded sidebar over the content while the pointer
// is at the window's left edge or over it, as Arc does.
func (a *App) peekSidebar(c *ui.Context) {
	t := c.Theme()
	x, _, inside := c.Root().PointerPosition()
	// Test the final panel area, so moving into it during the slide does
	// not close it before the animated panel reaches the pointer.
	over := inside && (x < 8 || a.peek && x < sidebarWidth+12)
	switch {
	case a.palette.open || a.settingsOpen:
		a.peek = false
		a.peekLeave = time.Time{}
	case over || a.renaming != "":
		a.peek = true
		a.peekLeave = time.Time{}
	case a.peek:
		if a.peekLeave.IsZero() {
			a.peekLeave = c.Now().Add(peekDelay)
		}
		if wait := a.peekLeave.Sub(c.Now()); wait > 0 {
			c.After(wait)
		} else {
			a.peek = false
			a.peekLeave = time.Time{}
		}
	}
	look := a.look
	ui.Overlay(c, func() {
		_, h := c.Size()
		panel := ui.Column(c.Key("peek")).Absolute().Left(6).Top(6).Width(sidebarWidth).Height(h-12).
			Radius(12).Background(a.look.opaqueTop()).Border(1, t.Border.Alpha(0.5)).
			Draw(func(p *ui.Painter, r ui.Rect) { look.paint(p, r, 12, false) }).
			Shadow(0, 10, 32, 0, black.Alpha(0.28)).Clip()
		target := float32(0)
		if a.peek {
			target = 1
		}
		v := panel.Animate("in", target, 150*time.Millisecond)
		panel.Translate(-(sidebarWidth+24)*(1-v), 0).Hidden(v < 0.01 && !a.peek)
		panel.Children(func() { a.sidebar(c, true) })
	})
}

// main is the toolbar over the card holding the open document.
func (a *App) main(c *ui.Context, docked float32) {
	t := c.Theme()
	left := float32(8)
	if docked > 0.5 {
		left = 0
	}
	ui.Column(c).Grow(1).MinWidth(0).Padding(0, 8, 8, left).Children(func() {
		if a.split != nil && !(a.spaceForm.open && a.spaceForm.id == "") {
			a.splitView(c)
			return
		}
		a.toolbar(c)
		card := ui.Themed(c.Key("card"), documentTheme(a.paperDark)).Grow(1).MinHeight(0).Radius(10).
			Shadow(0, 1, 4, 0, cardShadow(t)).Border(1, t.Border.Alpha(0.35)).Clip()
		w, _ := c.Size()
		a.splitDropTarget(c, card)
		card.Children(func() { a.documentContent(c, a.doc, w-docked-8-left-2) })
	})
}

func (a *App) documentContent(c *ui.Context, d *document, cardWidth float32) {

	if a.spaceForm.open && a.spaceForm.id == "" {
		a.draftState(c)
		return
	}
	if d == nil {
		a.emptyState(c)
		return
	}
	if d.loadErr != nil {
		ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
			ui.Text(c, "文档无法打开").FontSize(20)
			ui.Text(c, d.loadErr.Error()).TextColor(c.Theme().Danger).MaxWidth(460)
			if ui.Button(c, "重新载入").Clicked() {
				a.activateDocument(d)
				a.reload()
			}
		})
		return
	}
	if d.source {
		ui.Column(c).Grow(1).Padding(24).Gap(12).Children(func() {
			ui.Row(c).Gap(12).Children(func() {
				ui.Text(c, "Markdown 导出预览").Grow(1)
				if ui.Button(c, "导出 Markdown 文件").Clicked() {
					a.activateDocument(d)
					a.exportMarkdown()
				}
			})
			ui.TextArea(c.Key("export-preview"), &d.preview).Grow(1).ReadOnly(true).Label("Markdown 导出预览")
		})
		return
	}
	// One column always, so that the bar coming and going keeps the
	// editor's place in the tree, and with it its scroll and focus.
	ui.Column(c.Key("page")).Grow(1).MinHeight(0).Children(func() {
		a.conflictBarFor(c, d)
		editor.View(c.Key(d.viewKey()), d.ed, a.editorOptionsFor(c, cardWidth, false, d))
	})
}

// toolbar is the row above the card: the history's arrows, the address
// of the document, as Arc's, the editor's word count and format menu,
// and the more menu. It drags the window.
func (a *App) toolbar(c *ui.Context) {
	bar := c.TitleBar()
	ui.Row(c.Key("toolbar")).Height(max(bar.Height, 46)).Gap(2).AlignItems(ui.Center).DragWindow().Children(func() {
		if a.folded() {
			ui.Box(c).Width(bar.Left + 4)
			if iconButton(c, "show-sidebar", iconSidebar, "展开侧边栏", a.withKey("展开侧边栏", "sidebar"), false).Clicked() {
				a.toggleSidebar()
			}
		}
		if iconButton(c, "back", iconBack, "后退", a.withKey("后退", "back"), !a.canGo(-1)).Clicked() {
			a.goHistory(-1)
		}
		if iconButton(c, "forward", iconForward, "前进", a.withKey("前进", "forward"), !a.canGo(1)).Clicked() {
			a.goHistory(1)
		}
		if iconButton(c, "reload", iconReload, "重新载入", a.withKey("从磁盘重新载入", "reload"), a.doc == nil).Clicked() {
			a.reload()
		}
		ui.Box(c).Width(8)
		a.address(c)
		ui.Box(c).Width(8)
		if a.doc != nil && a.doc.loadErr == nil && !a.doc.source {
			a.doc.ed.Corner(c)
		}
		if iconButton(c, "split", iconSplit, "添加分屏", a.withKey("添加分屏", "split-add"), a.doc == nil).Clicked() {
			a.addSplitPane()
		}
		a.moreMenu(c)
	})
}

// address shows where the document is, as Arc's address field: the
// space, its folders and its name; a click opens the command bar.
func (a *App) address(c *ui.Context) {
	t := c.Theme()
	f := ui.Row(c.Key("address")).Grow(1).MinWidth(0).Height(32).Radius(9).PaddingX(12).Gap(8).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label("文档路径").
		Background(field(t))
	if f.Hovered() {
		f.Background(field(t).Mix(white, 0.2))
	}
	f.Children(func() {
		if a.spaceForm.open && a.spaceForm.id == "" {
			ui.Icon(c, iconSearch).FontSize(14).TextColor(t.TextMuted)
			ui.Text(c, "新空间").TextColor(t.TextMuted).NoWrap()
			return
		}
		if a.doc == nil {
			ui.Icon(c, iconSearch).FontSize(14).TextColor(t.TextMuted)
			ui.Text(c, "搜索或打开文档").TextColor(t.TextMuted).NoWrap()
			return
		}
		docIcon(c, a.iconTitle(a.doc.space.ID, a.doc.rel), 16)
		sp := a.doc.space
		parts := []string{sp.Name}
		if dir := path.Dir(a.doc.rel); dir != "." {
			parts = append(parts, strings.Split(dir, "/")...)
		}
		ui.Text(c, strings.Join(parts, " / ")+" /").TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
		ui.Text(c, path.Base(a.doc.rel)).FontWeight(500).SingleLine().Shrink(0)
		ui.Spacer(c)
		status, color := "", t.TextMuted
		switch {
		case a.doc.saveErr != nil:
			status, color = "保存失败", t.Danger
		case a.doc.dirty():
			status = "已修改"
		}
		if status != "" {
			ui.Text(c, status).FontSize(12).TextColor(color).NoWrap().Tooltip(errText(a.doc.saveErr))
		}
	})
	if f.Clicked() {
		a.openPalette()
	}
}

// iconButton is a quiet square button showing an icon, as in Arc's
// toolbar.
func iconButton(c *ui.Context, key string, icon *ui.SVG, label, tip string, disabled bool) ui.Element {
	t := c.Theme()
	b := ui.Box(c.Key(key)).Size(30, 30).Radius(8).Center().Focusable().Role(ui.RoleButton).
		Label(label).Tooltip(tip).Disabled(disabled)
	color := t.Text.Alpha(0.72)
	if disabled {
		color = t.Text.Alpha(0.25)
	} else {
		b.Cursor(ui.CursorPointer)
		if b.Hovered() {
			b.Background(hoverFill(t))
		}
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(16).TextColor(color) })
	return b
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// followLink opens a link of the document: another document of the space,
// or a web address in the browser.
func (a *App) followLink(url string) {
	if a.doc != nil {
		if rel, ok := a.doc.resolveLink(url); ok {
			a.open(rel)
			return
		}
	}
	if ui.SafeToOpen(url) {
		mygo.Shell.OpenExternal(url)
	}
}

// emptyState shows when no document is open.
func (a *App) emptyState(c *ui.Context) {
	t := c.Theme()
	sp := a.state.ActiveSpace()
	ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
		if sp != nil {
			spaceGlyph(c, sp.Icon, sp.Symbol, sp.Name, 40, t.Accent)
			ui.Text(c, sp.Name).FontSize(20).Bold()
		}
		switch {
		case a.treeErr != nil:
			ui.Text(c, "读取空间文件夹失败。"+a.treeErr.Error()).TextColor(t.Danger).MaxWidth(420).TextAlign(ui.Center)
		case len(a.docs) == 0:
			ui.Text(c, "此空间暂无文档。").TextColor(t.TextMuted)
		default:
			ui.Text(c, "选择一篇文档，或按 ⌘K 搜索。").TextColor(t.TextMuted)
		}
		ui.Row(c).Gap(8).Children(func() {
			if ui.PrimaryButton(c.Key("empty-new"), "新建文档").Tooltip("⌘N").Clicked() {
				a.newDoc("")
			}
			if ui.Button(c.Key("empty-palette"), "命令栏").Tooltip("⌘K").Clicked() {
				a.openPalette()
			}
		})
	})
}
