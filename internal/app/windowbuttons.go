package app

import (
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const windowButtonsWidth = 3*30 + 2*4

type captionWindow interface {
	IsFullScreen() bool
	IsMaximized() bool
	Minimize()
	ToggleMaximize()
	Close()
}

var _ captionWindow = (*mygo.Window)(nil)

func (a *App) captionWindow() captionWindow {
	if a.testCaptionWindow != nil {
		return a.testCaptionWindow
	}
	if a.win != nil && hasAppWindowControls() {
		return a.win
	}
	return nil
}

func (a *App) captionWidth() float32 {
	if win := a.captionWindow(); win != nil && !win.IsFullScreen() {
		return windowButtonsWidth
	}
	return 0
}

func (a *App) mainWindowButtons(c *ui.Context) {
	if a.captionWidth() == 0 {
		return
	}
	top := float32(8)
	if a.focus || (a.split != nil && a.folded() && a.qa.shown <= 0.5) {
		top = 3
	}
	ui.Row(c.Key("caption-host")).Absolute().Right(8).Top(top).Children(func() {
		windowButtons(c, a.captionWindow())
	})
}

// windowButtons adds client-owned controls to an existing toolbar, rather
// than creating a title row above the application.
func windowButtons(c *ui.Context, win captionWindow) {
	if win == nil || win.IsFullScreen() {
		return
	}
	ui.Row(c.Key("window-controls")).Shrink(0).PaddingX(4).AlignItems(ui.Center).Children(func() {
		caption := func(key string, icon *ui.SVG, label string, action func()) {
			label = i18n.T(label)
			if iconButton(c, key, icon, label, label, false).Shrink(0).Clicked() {
				action()
			}
		}
		caption("minimize", iconMinus, "最小化窗口", win.Minimize)
		if win.IsMaximized() {
			caption("maximize", lucideIcons["copy"], "还原窗口", win.ToggleMaximize)
		} else {
			caption("maximize", lucideIcons["square"], "最大化窗口", win.ToggleMaximize)
		}
		caption("close", iconX, "关闭窗口", win.Close)
	})
}
