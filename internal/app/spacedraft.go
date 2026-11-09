package app

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// spaceNameField occupies the title's original bounds. Enter or blur
// commits a nonempty name; Escape restores the saved name.
func (a *App) spaceNameField(c *ui.Context, f *spaceForm) {
	in := ui.TextInput(c.Key("space-name"), &f.name).Grow(1).MinWidth(0).Height(30).
		Margin(0, 0, 0, 5).Padding(2, 0).FontSize(13).FontWeight(600).Radius(5).
		Background(field(c.Theme())).Border(0, ui.Transparent).AutoFocus().Label("空间名称")
	if !f.focused {
		in.SetTextSelection(0, utf8.RuneCountInString(f.name))
	}
	switch {
	case c.Shortcut(0, ui.KeyEscape):
		a.cancelSpaceForm()
	case in.Submitted():
		a.submitSpace()
	case f.focused && !in.Focused():
		a.finishSpaceEdit()
	}
	f.focused = in.Focused()
}

// spaceDraft fills the sidebar while a new space is made, as Arc's
// "create space" pane: a title, a field holding the icon button and the
// name, the folder with a button to change it, a button that opens the
// theme card, and at the foot the buttons that create the space or give it
// up. The whole column anchors the theme card, which floats beside the
// sidebar at its middle, so presses in the column do not dismiss it.
func (a *App) spaceDraft(c *ui.Context, f *spaceForm) {
	t := c.Theme()
	if c.Shortcut(0, ui.KeyEscape) && !f.iconsOpen {
		a.cancelSpaceForm()
		return
	}
	root := f.root
	if root == "" {
		root = a.defaultRoot(strings.TrimSpace(f.name))
	}
	const fieldH, fieldR = 36, 10
	// tile is a filled box of the draft's fields, a little brighter under
	// the pointer when it acts.
	tile := func(e ui.Element, acts bool) ui.Element {
		e.Height(fieldH).Radius(fieldR).Background(field(t))
		if acts && e.Hovered() {
			e.Background(field(t).Mix(hoverFill(t), 0.6))
		}
		return e
	}
	form := ui.Column(c.Key("space-form")).Grow(1).MinHeight(0).Gap(8).Padding(14, 0, 0, 0)
	form.Children(func() {
		ui.Column(c).AlignItems(ui.Center).Gap(4).Margin(0, 0, 16, 0).Children(func() {
			ui.Text(c, "新建空间").FontSize(17).FontWeight(600).TextColor(t.Text)
			ui.Text(c, "空间用于整理文档和文件夹。").FontSize(12).TextColor(t.TextMuted).TextAlign(ui.Center)
		})

		tile(ui.Row(c.Key("space-form-header")), false).Padding(0, 6).Gap(6).AlignItems(ui.Center).Children(func() {
			icon := ui.Box(c.Key("space-icon")).Size(26, 26).Shrink(0).Radius(7).Center().Cursor(ui.CursorPointer).
				Focusable().Role(ui.RolePopUpButton).Label("空间图标").Tooltip("选择图标").Expanded(f.iconsOpen)
			if icon.Hovered() || f.iconsOpen {
				icon.Background(hoverFill(t))
			}
			empty := f.icon == "" && f.symbol == ""
			if empty {
				icon.Draw(func(p *ui.Painter, r ui.Rect) {
					p.StrokeDashed(ui.Rect{X: r.X + 0.5, Y: r.Y + 0.5, W: r.W - 1, H: r.H - 1}, t.TextMuted.Alpha(0.7), 7, 1)
				})
			}
			icon.Children(func() {
				if !empty {
					spaceGlyph(c, f.icon, f.symbol, f.name, 15, t.Accent)
				}
			})
			if icon.Clicked() {
				if f.iconsOpen {
					f.iconsOpen = false
				} else {
					f.openIconPicker()
				}
			}
			a.iconPicker(c, f, icon)

			name := ui.TextInput(c.Key("space-name"), &f.name).Grow(1).MinWidth(0).Height(fieldH).Padding(0, 2).FontSize(13).
				Placeholder("空间名称").Label("名称").AutoFocus().Background(ui.Transparent).Border(0, ui.Transparent)
			if name.Submitted() {
				a.submitSpace()
			}
		})

		tile(ui.Row(c.Key("draft-folder-row")), false).Padding(0, 5, 0, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "文件夹").FontSize(13).TextColor(t.Text).Shrink(0)
			ui.Spacer(c)
			pill := ui.Row(c.Key("draft-folder")).Height(fieldH - 12).MinWidth(0).Shrink(1).PaddingX(12).Gap(6).Radius(8).
				AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).
				Label("选择文件夹…").Tooltip(root).Background(hoverFill(t))
			if pill.Hovered() {
				pill.Background(hoverFill(t).Mix(t.Text, 0.08))
			}
			pill.Children(func() {
				ui.Icon(c, iconFolder).FontSize(13).TextColor(t.Accent).Shrink(0)
				ui.Text(c, filepath.Base(root)).FontSize(12).TextColor(t.Text).SingleLine().Shrink(1).MinWidth(0)
			})
			if pill.Clicked() {
				a.chooseFolder()
			}
		})

		theme := tile(ui.Box(c.Key("space-theme")), true).Center().Cursor(ui.CursorPointer).Focusable().
			Role(ui.RoleButton).Label("编辑主题").Expanded(f.cardOpen)
		if f.cardOpen {
			theme.Background(field(t).Mix(t.Accent, 0.12))
		}
		theme.Children(func() { ui.Text(c, "编辑主题").FontSize(13).TextColor(t.Text) })
		if theme.Clicked() {
			f.cardOpen = !f.cardOpen
		}

		ui.Spacer(c)
		blank := strings.TrimSpace(f.name) == ""
		if ui.PrimaryButton(c.Key("space-submit"), "创建空间").Height(fieldH).Radius(fieldR).FontSize(13).
			Tooltip("Enter").Disabled(blank).Clicked() {
			a.submitSpace()
		}
		cancel := tile(ui.Box(c.Key("space-cancel")), true).Center().Cursor(ui.CursorPointer).Focusable().
			Role(ui.RoleButton).Label("取消").Tooltip("Esc")
		cancel.Children(func() { ui.Text(c, "取消").FontSize(13).TextColor(t.Text) })
		if cancel.Clicked() {
			a.cancelSpaceForm()
		}
	})
	if f.open {
		a.themeCard(c, f, form)
	}
}

// cancelSpaceForm gives up the space being made or edited; the window goes
// back to the space's saved look.
func (a *App) cancelSpaceForm() { a.spaceForm = spaceForm{knob: -1} }

// shortPath writes the home folder as ~.
func shortPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

// draftState fills the content card while a new space is made.
func (a *App) draftState(c *ui.Context) {
	t := c.Theme()
	f := &a.spaceForm
	name := strings.TrimSpace(f.name)
	if name == "" {
		name = "新空间"
	}
	if f.cardOpen {
		// The theme card stands over the content; the card stays blank.
		ui.Spacer(c)
		return
	}
	ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
		spaceGlyph(c, f.icon, f.symbol, name, 40, t.Accent)
		ui.Text(c, name).FontSize(20).Bold()
		ui.Text(c, "命名空间、挑选颜色，按 Enter 创建。").TextColor(t.TextMuted)
	})
}
