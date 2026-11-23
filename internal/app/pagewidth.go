package app

import (
	"fmt"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

type pageWidthChoice struct {
	value string
	label string
}

// pageWidthChoices are the page width modes offered from the top-right button
// and the more menu, spanning from narrow to Feishu-style wide and full width.
var pageWidthChoices = []pageWidthChoice{
	{value: "narrow", label: "窄"},
	{value: "", label: "标准"},
	{value: "wide", label: "较宽"},
	{value: "full", label: "全宽"},
}

func pageWidthLabel(page string) string {
	for _, opt := range pageWidthChoices {
		if opt.value == page {
			return opt.label
		}
	}
	return "标准"
}

func (a *App) canAdjustPageWidth(d *document) bool {
	if d == nil || d.loadErr != nil || d.source || d.isWhiteboard() || d.isPDF() {
		return false
	}
	if d.isBook() {
		return d.book != nil && !d.book.ShowSource && (d.book.IsEPUB() || a.hostWindowFor(d) == nil)
	}
	return true
}

func (a *App) setPageWidth(page string) {
	switch page {
	case "narrow", "", "wide", "full":
	default:
		page = ""
	}
	if a.prefs.Page == page {
		return
	}
	if page == "full" && a.prefs.Page != "full" {
		a.pageWidthBeforeFull = a.prefs.Page
	}
	a.prefs.Page = page
	a.savePrefs()
}

// toggleFullWidth switches between full viewport width and the previous column width.
func (a *App) toggleFullWidth() {
	if a.doc != nil && !a.canAdjustPageWidth(a.doc) {
		return
	}
	if a.prefs.FullWidth() {
		target := a.pageWidthBeforeFull
		if target == "full" {
			target = ""
		}
		a.setPageWidth(target)
		return
	}
	a.setPageWidth("full")
}

func (a *App) pageWidthMenu(m *ui.Menu) {
	for _, opt := range pageWidthChoices {
		item := m.Item(i18n.T(opt.label))
		if opt.value == "full" {
			item = a.menuKey(item, "full-width")
		}
		if item.Checked(a.prefs.Page == opt.value).Chosen() {
			a.setPageWidth(opt.value)
		}
	}
}

// pageWidthButton draws the top-right page width control for Markdown documents.
func (a *App) pageWidthButton(c *ui.Context, d *document) ui.Element {
	icon := iconUnfoldHorizontal
	if a.prefs.FullWidth() {
		icon = iconFoldHorizontal
	}
	tip := a.withKey(fmt.Sprintf(i18n.T("页面宽度：%s"), i18n.T(pageWidthLabel(a.prefs.Page))), "full-width")
	b := iconButton(c, "page-width", icon, i18n.T("页面宽度"), tip, false).Role(ui.RoleMenuButton)
	if a.prefs.Page == "wide" || a.prefs.Page == "full" {
		if !a.inWindow(d) && a.split == nil {
			b.Background(glass(c.Theme()))
		} else {
			b.Background(c.Theme().Accent.Alpha(.14))
		}
	}
	b.PopoverMenuButton(a.pageWidthMenu)
	return b
}
