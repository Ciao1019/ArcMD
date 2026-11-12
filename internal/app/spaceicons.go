package app

import (
	"strings"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// The space icon picker follows Arc's as Cra (MIT) measured it: a 272-wide
// popover above the active space icon with emoji / symbol tabs and a
// remove button, a search field, and a six-column grid that opens on the
// current emoji's row, with a bar of categories below it.
// The symbol tab is Arc's 56 glyphs as their nearest Lucide ones.

const (
	// pickerW is the picker's width, as Arc's; pickerCell is the pitch of
	// its grids, six to a row beside the scroll bar.
	pickerW    = 272
	pickerCell = 32
)

// emojiEntry is one emoji of the picker and its name, which search matches.
type emojiEntry struct{ emoji, name string }

// emojiCategory is a run of the picker's emoji and the glyph of its button
// on the category bar.
type emojiCategory struct {
	id, label string
	icon      string // Lucide name
	first     int    // index of its first emoji in emojiAll
}

var (
	emojiAll        []emojiEntry
	emojiCategories []emojiCategory
)

func init() {
	icons := map[string][2]string{
		"smileys": {"face-slightly-smiling", "表情"}, "nature": {"sun", "自然"}, "food": {"coffee", "食物"},
		"activities": {"trophy", "活动"}, "travel": {"truck", "旅行"}, "objects": {"gift", "物品"},
		"symbols": {"heart", "符号"}, "flags": {"flag", "旗帜"},
	}
	for _, d := range emojiData {
		emojiCategories = append(emojiCategories, emojiCategory{id: d.id, icon: icons[d.id][0], label: icons[d.id][1], first: len(emojiAll)})
		for line := range strings.SplitSeq(d.lines, "\n") {
			if e, name, ok := strings.Cut(line, " "); ok {
				emojiAll = append(emojiAll, emojiEntry{e, name})
			}
		}
	}
}

// searchEmoji returns the emoji whose names hold every word of the query.
func searchEmoji(query string) []int {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil
	}
	var out []int
	for i, e := range emojiAll {
		ok := true
		for _, w := range words {
			if !strings.Contains(e.name, w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, i)
		}
	}
	return out
}

// emojiIndex is the place of the emoji in emojiAll, or -1.
func emojiIndex(emoji string) int {
	for i, e := range emojiAll {
		if e.emoji == emoji {
			return i
		}
	}
	return -1
}

// spaceGlyph draws a space's icon: its symbol, else its emoji, else the
// first letter of its name.
func spaceGlyph(c *ui.Context, emoji, symbol, name string, size float32, color ui.Color) ui.Element {
	if svg := lucideIcons[symbol]; svg != nil {
		return ui.Icon(c, svg).FontSize(size * 0.92).TextColor(color)
	}
	if emoji != "" {
		return ui.Text(c, emoji).FontSize(size).TextColor(color)
	}
	return ui.Text(c, monogram(vault.Title(name))).FontSize(size * 0.85).FontWeight(650).TextColor(color)
}

// iconPickerState is what the picker remembers while it is open.
type iconPickerState struct {
	symbols bool // the Icon tab
	query   string
	grid    ui.GridState
	// scroll is the emoji to bring to the top of the grid in the next
	// frame, -1 for none.
	scroll   int
	selected int
}

// openIconPicker opens the picker on the tab of the form's icon, the
// current emoji's row at the top.
func (f *spaceForm) openIconPicker() {
	f.iconsOpen = true
	f.picker.symbols = f.symbol != ""
	f.picker.query = ""
	f.picker.selected = -1
	f.picker.scroll = -1
	if i := emojiIndex(f.icon); i >= 0 {
		f.picker.scroll = i
	}
}

// iconPicker is anchored above the space switcher, or below a new draft's icon.
func (a *App) iconPicker(c *ui.Context, f *spaceForm, anchor ui.Element) {
	fill, _, rim := spaceEditorSurface(f.theme, c.Theme().Dark)
	ui.Overlay(c, func() {
		pop := ui.PopoverBase(c.Key("space-icons"), anchor, &f.iconsOpen, func(panel ui.Element) {
			panel.Width(pickerW).Height(310).Padding(10, 10, 8, 10).Radius(18).
				LinearGradient(fill).Border(0.5, rim).Shadow(0, 8, 24, 0, black.Alpha(0.20)).
				Role(ui.RoleDialog).Label(i18n.T("选择空间图标"))
			if f.id != "" {
				panel.AttachTo(anchor, ui.AnchorTop, ui.AnchorBottom).Margin(0, 0, 10, 0)
			}
			panel.Children(func() {
				a.pickerHeader(c, f)
				if f.picker.symbols {
					symbolGrid(c, f)
				} else {
					emojiGrid(c, f)
				}
			})
			// The small pointer keeps the picker visually attached to the
			// space icon even when the window edge shifts the panel sideways.
			if f.id != "" {
				panel.Draw(func(p *ui.Painter, r ui.Rect) {
					x := min(r.X+r.W-22, max(r.X+22, anchor.Bounds().X+anchor.Bounds().W/2))
					var tail ui.Path
					tail.MoveTo(x-9, r.Y+r.H-1)
					tail.LineTo(x, r.Y+r.H+9)
					tail.LineTo(x+9, r.Y+r.H-1)
					p.FillPath(&tail, fill.To)
				})
			}
		})
		if f.id != "" && !f.iconsOpen {
			if pop.Valid() && !pop.Dismissed() {
				a.submitSpace()
			} else {
				a.cancelSpaceForm()
			}
			if pop.Valid() && !pop.PressedOutside() {
				anchor.Focus()
			}
		}
	})
}

// pickerHeader is the Emoji / Icon tabs and the button removing the icon.
func (a *App) pickerHeader(c *ui.Context, f *spaceForm) {
	t := c.Theme()
	ui.Row(c).Padding(0, 0, 0, 22).Gap(4).AlignItems(ui.Center).Children(func() {
		ui.Row(c.Key("icon-tabs")).Gap(2).
			Role(ui.RoleTabList).Label(i18n.T("图标类型")).Children(func() {
			for _, tab := range []struct {
				label   string
				symbols bool
			}{{i18n.T("表情符号"), false}, {i18n.T("图标集"), true}} {
				on := f.picker.symbols == tab.symbols
				b := ui.Box(c.Key("tab-" + tab.label)).Height(26).PaddingX(8).Radius(6).Center().Cursor(ui.CursorPointer).
					Focusable().Role(ui.RoleTab).Label(tab.label).Selected(on)
				if on {
					b.Background(t.Text.Alpha(0.10))
				}
				b.Children(func() {
					color := t.TextMuted
					if on {
						color = t.Text
					}
					ui.Text(c, tab.label).FontSize(13).FontWeight(600).TextColor(color)
				})
				if b.Clicked() {
					f.picker.symbols = tab.symbols
				}
			}
		})
		ui.Spacer(c)
		if iconButton(c, "remove-icon", lucideIcons["trash"], i18n.T("移除图标"), i18n.T("移除图标"), f.icon == "" && f.symbol == "").Size(28, 28).Clicked() {
			f.icon, f.symbol = "", ""
			f.iconsOpen = false
		}
	})
}

// emojiGrid is the Emoji tab: the search field, the grid and the category
// bar.
func emojiGrid(c *ui.Context, f *spaceForm) {
	t := c.Theme()
	p := &f.picker
	query := ui.TextInput(c.Key("emoji-query"), &p.query).Height(26).Margin(8, 0, 0, 0).Padding(2, 6).Radius(10).
		Placeholder(i18n.T("搜索表情符号")).Label(i18n.T("搜索表情")).Tooltip(i18n.T("按英文名称搜索表情")).AutoFocus().
		Background(t.Background.Alpha(0.3)).Border(1, t.Text.Alpha(0.14))
	if query.Changed() {
		p.selected = -1
	}
	items := searchEmoji(p.query)
	searching := strings.TrimSpace(p.query) != ""
	n := len(emojiAll)
	if searching {
		n = len(items)
	}
	if searching && n == 0 {
		ui.Text(c, i18n.T("未能找到相关表情")).FontSize(12).TextColor(t.TextMuted).Padding(24, 0).AlignSelf(ui.Center).Grow(1)
	} else {
		at := func(i int) int {
			if searching {
				return items[i]
			}
			return i
		}
		p.grid.Key = func(i int) any { return emojiAll[at(i)].emoji }
		p.grid.Label = func(i int) string { return emojiAll[at(i)].name }
		p.grid.Selected = &p.selected
		grid := ui.GridView(c.Key("emoji-grid"), &p.grid, n, pickerCell, pickerCell, func(i int) {
			e := emojiAll[at(i)]
			on := e.emoji == f.icon && f.symbol == ""
			cell := ui.Box(c).Grow(1).Height(pickerCell).Center().Cursor(ui.CursorPointer).
				Role(ui.RoleButton).Label(e.emoji + " " + e.name).Tooltip(e.name)
			hovered := cell.Hovered()
			cell.Draw(func(pt *ui.Painter, r ui.Rect) {
				tile := ui.Rect{X: r.X + (r.W-27.5)/2, Y: r.Y + (r.H-27.5)/2, W: 27.5, H: 27.5}
				switch {
				case on:
					pt.Fill(tile, t.Text.Alpha(0.16), 5)
				case hovered:
					pt.Fill(tile, t.Text.Alpha(0.08), 5)
				}
			})
			cell.Children(func() { ui.Text(c, e.emoji).FontSize(21) })
			if cell.Clicked() {
				f.icon, f.symbol = e.emoji, ""
				f.iconsOpen = false
			}
		}).Grow(1).MinHeight(0).Margin(8, 0, 0, 0)
		if query.Shortcut(0, ui.KeyDown) {
			p.selected = 0
			grid.Focus()
			p.grid.ScrollIntoView(0)
		}
		if grid.Submitted() && p.selected >= 0 && p.selected < n {
			f.icon, f.symbol, f.iconsOpen = emojiAll[at(p.selected)].emoji, "", false
		}
		if p.scroll >= 0 && !searching {
			p.grid.ScrollTo(p.scroll, ui.Start)
		}
	}
	p.scroll = -1
	ui.Box(c).Height(1).Background(t.Text.Alpha(0.08))
	ui.Row(c.Key("emoji-categories")).Justify(ui.SpaceBetween).Children(func() {
		for _, cat := range emojiCategories {
			b := ui.Box(c.Key("cat-"+cat.id)).Size(28, 26).Center().Cursor(ui.CursorPointer).
				Focusable().Role(ui.RoleButton).Label(i18n.T(cat.label)).Tooltip(i18n.T(cat.label))
			color := t.TextMuted
			if b.Hovered() {
				color = t.Text
			}
			b.Children(func() { ui.Icon(c, lucideIcons[cat.icon]).FontSize(14).TextColor(color) })
			if b.Clicked() {
				p.query = ""
				p.scroll = cat.first
			}
		}
	})
}

// symbolGrid is the symbol tab: Arc's 56 glyphs in a scrollable six-column grid.
func symbolGrid(c *ui.Context, f *spaceForm) {
	t := c.Theme()
	ui.Scroll(c.Key("symbol-scroll")).Grow(1).MinHeight(0).Margin(8, 0, 0, 0).Children(func() {
		ui.Grid(c.Key("symbols")).Columns(6).Gap(8).AlignSelf(ui.Center).Label(i18n.T("图标列表")).FocusGroup(ui.Horizontal).Children(func() {
			for _, name := range spaceSymbols {
				on := name == f.symbol
				b := ui.Box(c.Key("symbol:"+name)).Size(pickerCell, pickerCell).Center().Cursor(ui.CursorPointer).
					Focusable().Role(ui.RoleButton).Label(i18n.T("图标 ") + name).Tooltip(name).Checked(on)
				hovered := b.Hovered()
				b.Draw(func(p *ui.Painter, r ui.Rect) {
					tile := ui.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2}
					switch {
					case on:
						p.Fill(tile, t.Text.Alpha(0.16), 5)
					case hovered:
						p.Fill(tile, t.Text.Alpha(0.1), 5)
					}
				})
				b.Children(func() { ui.Icon(c, lucideIcons[name]).FontSize(15).TextColor(t.Text) })
				if b.Clicked() {
					f.icon, f.symbol = "", name
					f.iconsOpen = false
				}
			}
		})
	})
}

// emojiAliases are GitHub's shortcodes for emoji people type most, beside
// the names of the catalog.
var emojiAliases = []editor.EmojiItem{
	{Emoji: "😄", Name: "smile"}, {Emoji: "😂", Name: "joy"}, {Emoji: "😆", Name: "laughing"}, {Emoji: "😉", Name: "wink"},
	{Emoji: "😢", Name: "cry"}, {Emoji: "😭", Name: "sob"}, {Emoji: "🤔", Name: "thinking"}, {Emoji: "👍", Name: "+1"},
	{Emoji: "👎", Name: "-1"}, {Emoji: "👌", Name: "ok_hand"}, {Emoji: "👏", Name: "clap"}, {Emoji: "🙏", Name: "pray"},
	{Emoji: "❤️", Name: "heart"}, {Emoji: "🎉", Name: "tada"}, {Emoji: "🔥", Name: "fire"}, {Emoji: "🚀", Name: "rocket"},
	{Emoji: "👀", Name: "eyes"}, {Emoji: "⭐", Name: "star"}, {Emoji: "✅", Name: "white_check_mark"}, {Emoji: "❌", Name: "x"},
	{Emoji: "⚠️", Name: "warning"}, {Emoji: "💡", Name: "bulb"}, {Emoji: "📌", Name: "pushpin"}, {Emoji: "📝", Name: "memo"},
}

// shortcode is the :shortcode: of a catalog name: lower case, words joined
// by underscores, punctuation dropped.
func shortcode(name string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r == ' ' || r == '-':
			out.WriteByte('_')
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_':
			out.WriteRune(r)
		}
	}
	return out.String()
}

// suggestEmoji lists the emoji for a :shortcode: being typed: GitHub's
// common ones and the catalog's names, those starting with what is typed
// before those holding it, at most eight.
func suggestEmoji(typed string) []editor.EmojiItem {
	typed = strings.ToLower(typed)
	var first, then []editor.EmojiItem
	seen := map[string]bool{}
	add := func(it editor.EmojiItem) {
		if seen[it.Name] {
			return
		}
		switch {
		case strings.HasPrefix(it.Name, typed):
			seen[it.Name] = true
			first = append(first, it)
		case strings.Contains(it.Name, typed):
			seen[it.Name] = true
			then = append(then, it)
		}
	}
	for _, it := range emojiAliases {
		add(it)
	}
	for _, e := range emojiAll {
		add(editor.EmojiItem{Emoji: e.emoji, Name: shortcode(e.name)})
	}
	out := append(first, then...)
	return out[:min(len(out), 8)]
}
