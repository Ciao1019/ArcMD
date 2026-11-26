package app

import (
	"fmt"
	"math"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/prefs"
	"github.com/egoist/mygo/ui"
)

var pageZoomPresets = []float32{0.75, 1.00, 1.25, 1.50, 2.00}

func (a *App) pageZoom() float32 {
	if a.prefs == nil {
		return prefs.DefaultPageZoom
	}
	return a.prefs.Zoom()
}

func pageZoomPercent(z float32) int {
	return int(math.Round(float64(prefs.NormalizePageZoom(z)) * 100))
}

func (a *App) canZoomPage(d *document) bool {
	if !a.canAdjustPageWidth(d) {
		return false
	}
	if d.isMDZ() {
		if ed := d.mdz.activeEditor(); ed != nil && ed.HasZoomModal() {
			return false
		}
		return true
	}
	if !d.isBook() && d.ed != nil && d.ed.HasZoomModal() {
		return false
	}
	return true
}

func (a *App) overlayBlocksZoom() bool {
	return a.modalBlocksSidebarKeys() || a.gitChanges.open || a.mergeUI.open || a.dbPeek.open
}

func (a *App) setPageZoom(zoom float32) {
	if a.prefs == nil {
		return
	}
	next := prefs.NormalizePageZoom(zoom)
	if next == a.prefs.Zoom() {
		return
	}
	if next == prefs.DefaultPageZoom {
		a.prefs.PageZoom = 0
	} else {
		a.prefs.PageZoom = next
	}
	a.savePrefs()
}

func (a *App) stepPageZoom(delta int) {
	a.setPageZoom(prefs.StepPageZoom(a.pageZoom(), delta))
}

func (a *App) resetPageZoom() {
	a.setPageZoom(prefs.DefaultPageZoom)
}

func (a *App) pageZoomMenu(m *ui.Menu) {
	z := a.pageZoom()
	if a.menuKey(m.Item(i18n.T("放大页面")), "zoom-in").Disabled(z >= prefs.MaxPageZoom).Chosen() {
		a.stepPageZoom(1)
	}
	if a.menuKey(m.Item(i18n.T("缩小页面")), "zoom-out").Disabled(z <= prefs.MinPageZoom).Chosen() {
		a.stepPageZoom(-1)
	}
	if a.menuKey(m.Item(i18n.T("重置为 100%")), "zoom-reset").Disabled(z == prefs.DefaultPageZoom).Chosen() {
		a.resetPageZoom()
	}
	m.Separator()
	curPct := pageZoomPercent(z)
	for _, preset := range pageZoomPresets {
		pct := pageZoomPercent(preset)
		val := preset
		if m.Item(fmt.Sprintf("%d%%", pct)).Radio(curPct == pct).Chosen() {
			a.setPageZoom(val)
		}
	}
}

func (a *App) showPageZoomButton(d *document) bool {
	return a.canZoomPage(d) && a.pageZoom() != prefs.DefaultPageZoom
}

// pageZoomButton draws a subtle percentage pill in the top-right corner when
// page zoom differs from 100%, staying hidden at 100% to keep the bar calm.
func (a *App) pageZoomButton(c *ui.Context, d *document) ui.Element {
	pct := pageZoomPercent(a.pageZoom())
	t := c.Theme()
	tip := a.withKey(fmt.Sprintf(i18n.T("页面缩放：%d%%"), pct), "zoom-reset")
	b := ui.ButtonBase(c.Key("page-zoom")).Height(28).PaddingX(8).Radius(7).Center().
		Cursor(ui.CursorPointer).Role(ui.RoleMenuButton).
		Label(i18n.T("页面缩放")).Tooltip(tip)
	if b.Hovered() {
		b.Background(hoverFill(t))
	} else if !a.inWindow(d) && a.split == nil {
		b.Background(glass(t))
	} else {
		b.Background(t.Accent.Alpha(.14))
	}
	b.Children(func() {
		ui.Text(c, fmt.Sprintf("%d%%", pct)).FontSize(12).FontWeight(500).TextColor(t.Text).NoWrap()
	})
	b.PopoverMenuButton(a.pageZoomMenu)
	return b
}
