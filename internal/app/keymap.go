package app

import (
	"strings"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// Keyboard shortcuts that can be rebound: every command here has an ID
// prefs.json keys its shortcut by, its default keys, and whether the
// editor takes it. A rebound command answers only to its new key.

type binding struct {
	id, label string
	defaults  []editor.Combo
	// editor commands are the editor's (editor.DefaultBindings).
	editor bool
}

func combo(mods ui.Modifiers, key ui.Key) editor.Combo { return editor.Combo{Mods: mods, Key: key} }

// bindings are the commands that can be rebound, in the order the
// settings list them.
var bindings = []binding{
	{id: "palette", label: "命令栏", defaults: []editor.Combo{combo(ui.Cmd, ui.KeyK), combo(ui.Cmd, ui.KeyL), combo(ui.Cmd, ui.KeyP)}},
	{id: "new-doc", label: "新建文档", defaults: []editor.Combo{combo(ui.Cmd, ui.KeyN)}},
	{id: "new-folder", label: "新建文件夹", defaults: []editor.Combo{combo(ui.Cmd|ui.Shift, ui.KeyN)}},
	{id: "daily", label: "今日笔记", defaults: []editor.Combo{combo(ui.Cmd|ui.Alt, ui.KeyT)}},
	{id: "replace-all", label: "在所有文档中查找和替换", defaults: []editor.Combo{combo(ui.Cmd|ui.Shift, ui.KeyR)}},
	{id: "sidebar", label: "收起/展开侧边栏", defaults: []editor.Combo{combo(ui.Cmd, ui.KeyBackslash)}},
	{id: "focus", label: "专注模式", defaults: []editor.Combo{combo(ui.Cmd|ui.Shift, ui.KeyF)}},
	{id: "settings", label: "设置", defaults: []editor.Combo{combo(ui.Cmd, ui.KeyComma)}},
	{id: "source", label: "富文本 ⇄ Markdown 导出预览", defaults: []editor.Combo{combo(ui.Cmd|ui.Shift, ui.KeyM)}},
	{id: "back", label: "后退", defaults: []editor.Combo{combo(ui.Cmd, ui.KeyBracketLeft)}},
	{id: "forward", label: "前进", defaults: []editor.Combo{combo(ui.Cmd, ui.KeyBracketRight)}},
	{id: "reload", label: "从磁盘重新载入", defaults: []editor.Combo{combo(ui.Cmd, ui.KeyR)}},
	{id: "prev-space", label: "上一个空间", defaults: []editor.Combo{combo(ui.Cmd|ui.Alt, ui.KeyLeft)}},
	{id: "next-space", label: "下一个空间", defaults: []editor.Combo{combo(ui.Cmd|ui.Alt, ui.KeyRight)}},
	{id: "library", label: "资料库", defaults: []editor.Combo{combo(ui.Cmd|ui.Shift, ui.KeyL)}},
	{id: "overview", label: "所有空间", defaults: []editor.Combo{combo(ui.Ctrl|ui.Cmd, ui.KeyS)}},
	{id: "split-add", label: "添加分屏", defaults: []editor.Combo{combo(ui.Ctrl|ui.Shift, ui.KeyEqual)}},
	{id: "split-close", label: "关闭此分屏", defaults: []editor.Combo{combo(ui.Ctrl|ui.Shift, ui.KeyMinus)}},
	{id: "split-next", label: "下一个分屏", defaults: []editor.Combo{combo(ui.Ctrl|ui.Shift, ui.KeyTab)}},
	{id: "save", label: "立即保存", defaults: []editor.Combo{combo(ui.Cmd, ui.KeyS)}},
	{id: "outline", label: "展开/收起目录", defaults: []editor.Combo{combo(ui.Cmd|ui.Shift, ui.KeyO)}},
	{id: "bookmark", label: "添加/移除书签", defaults: []editor.Combo{combo(ui.Cmd, ui.KeyD)}},
	{id: "bookmarks", label: "书签列表", defaults: []editor.Combo{combo(ui.Cmd|ui.Shift, ui.KeyD)}},
	{id: "prev-bookmark", label: "上一个书签", defaults: []editor.Combo{combo(ui.Ctrl|ui.Cmd, ui.KeyUp)}},
	{id: "next-bookmark", label: "下一个书签", defaults: []editor.Combo{combo(ui.Ctrl|ui.Cmd, ui.KeyDown)}},
	{id: "history", label: "版本历史", defaults: []editor.Combo{combo(ui.Cmd|ui.Alt, ui.KeyY)}},
	{id: "export-pdf", label: "导出 PDF", defaults: []editor.Combo{combo(ui.Cmd|ui.Shift, ui.KeyE)}},
	{id: "bold", label: "粗体", editor: true},
	{id: "italic", label: "斜体", editor: true},
	{id: "underline", label: "下划线", editor: true},
	{id: "strike", label: "删除线", editor: true},
	{id: "code", label: "行内代码", editor: true},
	{id: "find", label: "查找", editor: true},
	{id: "replace", label: "查找和替换", editor: true},
	{id: "newline", label: "在下方新建段落", editor: true},
}

func init() {
	defaults := editor.DefaultBindings()
	for i, b := range bindings {
		if b.editor {
			bindings[i].defaults = []editor.Combo{defaults[b.id]}
		}
	}
}

// fixedShortcuts are the keys that cannot be rebound, for the settings'
// list; AGENTS.md keeps them in its table too.
var fixedShortcuts = []struct{ keys, label string }{
	{"⌃1…⌃9 / ⌘1…⌘9", "切换到第 N 个空间"},
	{"⌃⇧1…⌃⇧4", "切换到第 N 个分屏"},
	{"⌥⌘0…⌥⌘6", "正文 / 标题 1–6"},
	{"⌘Z / ⇧⌘Z", "撤销 / 重做"},
	{"⌘ 点击", "打开链接、[[双链]] 或 #标签"},
	{"[[", "链接到其他文档"},
	{"Esc / ⇧↑ / ⇧↓", "选择内容块（选中后 ⇧⌘↑ / ⇧⌘↓ 一起移动）"},
}

// unbound is how prefs.json writes a command with no key.
const unbound = "none"

func bindingOf(id string) (binding, bool) {
	for _, b := range bindings {
		if b.id == id {
			return b, true
		}
	}
	return binding{}, false
}

// combos are the keys of the command with the ID: its own when rebound,
// else its defaults.
func (a *App) combos(id string) []editor.Combo {
	if a.prefs != nil {
		if s, ok := a.prefs.Keys[id]; ok {
			if c, ok := editor.ParseCombo(s); ok {
				return []editor.Combo{c}
			}
			return nil // unbound
		}
	}
	b, _ := bindingOf(id)
	return b.defaults
}

// pressed reports whether a key of the command with the ID was pressed.
func (a *App) pressed(c *ui.Context, id string) bool {
	for _, k := range a.combos(id) {
		if c.Shortcut(k.Mods, k.Key) {
			return true
		}
	}
	return false
}

// keyLabel shows the keys of the command with the ID, "" when it has none.
func (a *App) keyLabel(id string) string {
	var parts []string
	for _, k := range a.combos(id) {
		parts = append(parts, k.Label())
	}
	return strings.Join(parts, " / ")
}

// withKey is a tooltip with the command's key after it.
func (a *App) withKey(tip, id string) string {
	if k := a.keyLabel(id); k != "" {
		return tip + " (" + strings.Split(k, " / ")[0] + ")"
	}
	return tip
}

// menuKey gives a menu item the command's first key.
func (a *App) menuKey(item *ui.MenuItem, id string) *ui.MenuItem {
	if ks := a.combos(id); len(ks) > 0 {
		item.Shortcut(ks[0].Mods, ks[0].Key)
	}
	return item
}

// editorBindings are the editor's commands rebound by the user.
func (a *App) editorBindings() map[string]editor.Combo {
	if a.prefs == nil || len(a.prefs.Keys) == 0 {
		return nil
	}
	out := map[string]editor.Combo{}
	for _, b := range bindings {
		if !b.editor {
			continue
		}
		if _, ok := a.prefs.Keys[b.id]; ok {
			ks := a.combos(b.id)
			if len(ks) == 0 {
				out[b.id] = editor.Combo{} // matches no key
				continue
			}
			out[b.id] = ks[0]
		}
	}
	return out
}

// rebind gives the command with the ID the key, or none, after checking no
// other command has it; it reports whether it did.
func (a *App) rebind(id string, k *editor.Combo) bool {
	if a.prefs == nil {
		return false
	}
	if k != nil {
		if !k.Valid() {
			a.notify("快捷键需要包含 ⌘、⌃ 或 ⌥（功能键除外）。")
			return false
		}
		for _, b := range bindings {
			if b.id == id {
				continue
			}
			for _, other := range a.combos(b.id) {
				if other == *k {
					a.notify("%s 已被「%s」使用。", k.Label(), i18n.T(b.label))
					return false
				}
			}
		}
		for _, f := range reservedCombos() {
			if f == *k {
				a.notify("%s 是固定的快捷键，不能使用。", k.Label())
				return false
			}
		}
	}
	if a.prefs.Keys == nil {
		a.prefs.Keys = map[string]string{}
	}
	switch b, _ := bindingOf(id); {
	case k == nil:
		a.prefs.Keys[id] = unbound
	case len(b.defaults) == 1 && b.defaults[0] == *k:
		delete(a.prefs.Keys, id)
	default:
		a.prefs.Keys[id] = k.String()
	}
	a.savePrefs()
	return true
}

// resetKeys gives every command, or the one with the ID, its default keys.
func (a *App) resetKeys(id string) {
	if a.prefs == nil {
		return
	}
	if id == "" {
		a.prefs.Keys = nil
	} else {
		delete(a.prefs.Keys, id)
	}
	a.savePrefs()
}

// reservedCombos are the fixed keys a command cannot take.
func reservedCombos() []editor.Combo {
	out := []editor.Combo{combo(ui.Cmd, ui.KeyZ), combo(ui.Cmd|ui.Shift, ui.KeyZ), combo(ui.Cmd, ui.KeyC),
		combo(ui.Cmd, ui.KeyV), combo(ui.Cmd, ui.KeyX), combo(ui.Cmd, ui.KeyA), combo(ui.Cmd, ui.KeyQ), combo(ui.Cmd, ui.KeyW)}
	for _, k := range []ui.Key{ui.Key0, ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9} {
		out = append(out, combo(ui.Cmd, k), combo(ui.Ctrl, k), combo(ui.Cmd|ui.Alt, k))
	}
	for _, k := range []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4} {
		out = append(out, combo(ui.Ctrl|ui.Shift, k))
	}
	return out
}

// settingsShortcuts lists the shortcuts; a click on one records a new key
// for it.
func (a *App) settingsShortcuts(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Column(c).Grow(1).Children(func() {
			settingsHeading(c, i18n.T("快捷键"), i18n.T("点击快捷键后按下新的组合键；Esc 取消，⌫ 清除。所有功能也都能从命令栏找到。"))
		})
		if ui.Button(c.Key("keys-reset-all"), i18n.T("全部恢复默认")).Disabled(a.prefs == nil || len(a.prefs.Keys) == 0).Clicked() {
			a.resetKeys("")
			a.recording = ""
		}
	})
	settingsGroup(c, "shortcut-list", func() {
		for i, b := range bindings {
			id := b.id
			row := ui.Row(c.Key("key-row:" + id)).Height(36).PaddingX(12).Gap(8).AlignItems(ui.Center)
			if i%2 == 1 {
				row.Background(t.Text.Alpha(0.025))
			}
			row.Children(func() {
				ui.Text(c, i18n.T(b.label)).Grow(1).MinWidth(0)
				_, custom := a.prefs.Keys[id]
				if custom && a.recording != id {
					reset := ui.ButtonBase(c.Key("key-reset:" + id)).Height(24).PaddingX(6).Radius(5).Label(i18n.T("恢复默认：") + i18n.T(b.label)).Cursor(ui.CursorPointer)
					if reset.Hovered() {
						reset.Background(hoverFill(t))
					}
					reset.Children(func() { ui.Text(c, i18n.T("恢复默认")).FontSize(12).TextColor(t.TextMuted) })
					if reset.Clicked() {
						a.resetKeys(id)
					}
				}
				a.keyField(c, b)
			})
		}
	})
	settingsGroup(c, "shortcut-fixed", func() {
		for i, s := range fixedShortcuts {
			row := ui.Row(c.Key(s.keys)).Height(34).PaddingX(12).AlignItems(ui.Center)
			if i%2 == 1 {
				row.Background(t.Text.Alpha(0.025))
			}
			row.Children(func() {
				ui.Text(c, i18n.T(s.label)).Grow(1).MinWidth(0)
				ui.Text(c, i18n.T(s.keys)).FontSize(12).TextColor(t.TextMuted).NoWrap()
			})
		}
	})
}

// keyField shows a command's keys; clicked, it takes the next key pressed.
func (a *App) keyField(c *ui.Context, b binding) {
	t := c.Theme()
	recording := a.recording == b.id
	f := ui.Box(c.Key("key-field:" + b.id)).Height(26).MinWidth(96).PaddingX(10).Radius(6).Center().Focusable().
		Role(ui.RoleButton).Label(i18n.T("更改快捷键：") + i18n.T(b.label)).Cursor(ui.CursorPointer).Background(t.Text.Alpha(0.06))
	if recording {
		f.Border(1.5, t.Accent).Background(t.Accent.Alpha(0.1))
		if a.recordFocus {
			f.Focus()
			a.recordFocus = !f.Focused()
		}
		f.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputKeyDown {
				return false
			}
			switch {
			case ev.Key == ui.KeyEscape && ev.Mods == 0:
				a.recording = ""
			case (ev.Key == ui.KeyBackspace || ev.Key == ui.KeyDelete) && ev.Mods == 0:
				if a.rebind(b.id, nil) {
					a.recording = ""
				}
			default:
				k := editor.Combo{Mods: ev.Mods, Key: ev.Key}
				if k.Valid() || ev.Mods != 0 {
					if a.rebind(b.id, &k) {
						a.recording = ""
					}
				}
			}
			return true
		})
		if !f.Focused() && !a.recordFocus {
			// Focus went elsewhere: stop recording.
			a.recording = ""
		}
	} else if f.Hovered() {
		f.Background(t.Text.Alpha(0.1))
	}
	f.Children(func() {
		switch label := a.keyLabel(b.id); {
		case recording:
			ui.Text(c, i18n.T("按下快捷键…")).FontSize(12).TextColor(t.Accent).NoWrap()
		case label == "":
			ui.Text(c, i18n.T("未设置")).FontSize(12).TextColor(t.TextMuted).NoWrap()
		default:
			ui.Text(c, label).FontSize(12).NoWrap()
		}
	})
	if f.Clicked() && !recording {
		a.recording, a.recordFocus = b.id, true
	}
}
