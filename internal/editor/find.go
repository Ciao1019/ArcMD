package editor

// Find and replace: ⌘F opens a box at the page's top right that finds text
// case-insensitively in every block, folded ones included; ⇧⌘H opens it
// with replace. Enter steps to the next match, ⇧Enter to the one before.

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"
)

// finder is the state of the find box.
type finder struct {
	open, replace bool
	query, with   string
	// matches are in document order, as UTF-16 ranges of the plain text;
	// current indexes them.
	matches []ui.TextInputRange
	current int
	// key is what matches were found for.
	key string
	// focus asks the query field for the keyboard.
	focus bool
}

// findIn finds query in text case-insensitively, without overlaps; the
// ranges are UTF-16 offsets into text.
func findIn(text, query string) [][2]int {
	fold := func(r rune) rune { return unicode.ToLower(r) }
	q := []rune(strings.Map(fold, query))
	if len(q) == 0 {
		return nil
	}
	runes := []rune(text)
	var out [][2]int
	// offsets[i] is the UTF-16 offset of runes[i].
	offsets := make([]int, len(runes)+1)
	for i, r := range runes {
		n := 1
		if r > 0xffff {
			n = 2
		}
		offsets[i+1] = offsets[i] + n
	}
	for i := 0; i+len(q) <= len(runes); {
		ok := true
		for j, r := range q {
			if fold(runes[i+j]) != r {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, [2]int{offsets[i], offsets[i+len(q)]})
			i += len(q)
			continue
		}
		i++
	}
	return out
}

// OpenFind opens the find box, with replace when replace is set. A
// selection within one line seeds the query, as a browser's find bar does.
func (e *Editor) OpenFind(replace bool) {
	if e.ReadOnly {
		replace = false
		e.find.replace = false
	}
	r := e.Selection().Range
	if r.Start != r.End {
		if text, _ := e.TextForRange(r); !strings.ContainsAny(text, "\n ") {
			e.find.query = text
		}
	}
	e.find.open, e.find.replace, e.find.focus = true, replace || e.find.replace && e.find.open, true
	e.find.key = ""
}

// FindText opens the find box on query and scrolls to its first match, as
// the command bar does when it opens a document at text it found there.
// The box takes the keyboard, so Enter steps on through the matches and
// Escape returns to the page.
func (e *Editor) FindText(query string) {
	f := &e.find
	f.query, f.open, f.replace, f.focus = query, true, false, true
	f.key, f.current = "", 0
	e.refreshMatches()
	e.showMatch()
}

// FindQuery is the find box's query while it is open.
func (e *Editor) FindQuery() (string, bool) { return e.find.query, e.find.open }

// closeFind puts the find box away and gives the page the keyboard back.
func (e *Editor) closeFind() {
	e.find.open = false
	e.RequestFocus = true
}

// refreshMatches finds the query again when it or the document changed.
func (e *Editor) refreshMatches() {
	f := &e.find
	key := fmt.Sprint(e.Revision(), "\x00", f.query)
	if key == f.key {
		return
	}
	f.key = key
	f.matches = f.matches[:0]
	for _, leaf := range e.Doc.Leaves() {
		for _, m := range findIn(leaf.Block.Text(), f.query) {
			f.matches = append(f.matches, ui.TextInputRange{Start: leaf.Start + m[0], End: leaf.Start + m[1]})
		}
	}
	f.current = max(0, min(f.current, len(f.matches)-1))
}

// stepMatch moves to the next (1) or previous (-1) match and shows it,
// unfolding a section that hides it.
func (e *Editor) stepMatch(delta int) {
	f := &e.find
	if len(f.matches) == 0 {
		return
	}
	f.current = (f.current + delta + len(f.matches)) % len(f.matches)
	e.showMatch()
}

// showMatch scrolls the current match into view.
func (e *Editor) showMatch() {
	f := &e.find
	if f.current >= len(f.matches) {
		return
	}
	m := f.matches[f.current]
	leaf := e.Engine.LeafAt(m.Start)
	if e.Engine.Reveal(leaf.Owner) || e.Engine.Reveal(leaf.Block.ID) {
		e.sync()
	}
	e.revealAt = &m
}

// replaceCurrent replaces the current match and moves on to the next.
func (e *Editor) replaceCurrent() {
	f := &e.find
	if f.current >= len(f.matches) {
		return
	}
	m := f.matches[f.current]
	e.UnmarkText()
	e.Engine.Replace(m.Start, m.End, f.with)
	e.sync()
	e.refreshMatches()
	if len(f.matches) > 0 {
		e.showMatch()
	}
}

// replaceAll replaces every match as one step of the undo history.
func (e *Editor) replaceAll() {
	f := &e.find
	if len(f.matches) == 0 {
		return
	}
	e.UnmarkText()
	matches := append([]ui.TextInputRange(nil), f.matches...)
	e.Engine.Edit(func() {
		for i := len(matches) - 1; i >= 0; i-- {
			e.Engine.Replace(matches[i].Start, matches[i].End, f.with)
		}
	})
	e.sync()
	e.refreshMatches()
}

// findShortcuts opens and steers the find box from anywhere in the window.
func (e *Editor) findShortcuts(c *ui.Context) {
	if b := e.binding("find"); c.Shortcut(b.Mods, b.Key) {
		e.OpenFind(false)
	}
	if b := e.binding("replace"); c.Shortcut(b.Mods, b.Key) {
		e.OpenFind(true)
	}
	if !e.find.open {
		return
	}
	if c.Shortcut(0, ui.KeyEscape) {
		e.closeFind()
	}
	if c.Shortcut(ui.Shift, ui.KeyEnter) {
		e.stepMatch(-1)
	}
	if c.Shortcut(ui.Cmd, ui.KeyG) {
		e.stepMatch(1)
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyG) {
		e.stepMatch(-1)
	}
}

// findBox is the find (and replace) box at the page's top right.
func (e *Editor) findBox(c *ui.Context) {
	f := &e.find
	if !f.open {
		return
	}
	e.refreshMatches()
	t := c.Theme()
	pg := e.page
	button := func(key, label string, icon *ui.SVG, on bool) ui.Element {
		b := ui.ButtonBase(c.Key(key)).Size(24, 24).Radius(4).Center().Label(label).Tooltip(label).KeepFocus().Cursor(ui.CursorPointer)
		if on || b.Hovered() {
			b.Background(pg.Hover)
		}
		b.Children(func() { ui.Icon(c, icon).FontSize(16).TextColor(pg.CodeText) })
		return b
	}
	field := func(key, placeholder string, value *string, focus bool) ui.Element {
		in := ui.TextInput(c.Key(key), value).Placeholder(placeholder).Label(placeholder).FontSize(14).Padding(5, 8).Grow(1).MinWidth(0)
		if focus {
			in.Focus()
		}
		return in
	}
	// Not e.popover: its fields must take the keyboard.
	box := ui.Column(c.Key("find-box")).Absolute().Right(16).Top(12).Width(404).Padding(8).Gap(8).
		Background(pg.Popover).Border(1, t.Border).Radius(6).Shadow(0, 4, 16, 0, t.Text.Alpha(.12))
	box.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			if button("find-toggle", "切换替换 (⇧⌘H)", iconChevronRight, f.replace).Disabled(e.ReadOnly).Clicked() {
				f.replace = !f.replace
			}
			query := field("find-query", "查找", &f.query, f.focus)
			f.focus = false
			if query.Submitted() {
				e.stepMatch(1)
			}
			count := "无结果"
			if len(f.matches) > 0 {
				count = fmt.Sprintf("%d/%d", f.current+1, len(f.matches))
			}
			if f.query == "" {
				count = ""
			}
			ui.Text(c, count).FontSize(12).TextColor(pg.Caption).NoWrap().MinWidth(44)
			if button("find-prev", "上一个 (⇧Enter)", iconArrowUp, false).Clicked() {
				e.stepMatch(-1)
			}
			if button("find-next", "下一个 (Enter)", iconArrowDown, false).Clicked() {
				e.stepMatch(1)
			}
			if button("find-close", "关闭查找 (Esc)", iconClose, false).Clicked() {
				e.closeFind()
			}
		})
		if !f.replace {
			return
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(0, 0, 0, 30).Children(func() {
			with := field("find-with", "替换为", &f.with, false)
			if with.Submitted() {
				e.replaceCurrent()
			}
			for _, b := range []struct {
				label string
				run   func()
			}{{"替换", e.replaceCurrent}, {"全部替换", e.replaceAll}} {
				el := ui.ButtonBase(c.Key("find:"+b.label)).Height(28).Width(80).Radius(6).Border(1, t.Border).Center().Label(b.label).KeepFocus().Cursor(ui.CursorPointer)
				if len(f.matches) == 0 {
					el.Disabled(true).Opacity(.4)
				} else if el.Hovered() {
					el.Background(pg.Hover)
				}
				el.Children(func() { ui.Text(c, b.label).FontSize(13).TextColor(t.Text) })
				if el.Clicked() {
					b.run()
				}
			}
		})
	})
}

// paintMatches marks the matches on the page, the current one stronger.
func (e *Editor) paintMatches(p *ui.Painter, bounds ui.Rect) {
	f := &e.find
	if !f.open || len(f.matches) == 0 {
		return
	}
	for _, r := range e.rows {
		if r.rect.Y+r.rect.H < e.Scroll.Y || r.rect.Y > e.Scroll.Y+e.viewport+40 {
			continue
		}
		for i, m := range f.matches {
			if m.End <= r.leaf.Start || m.Start >= r.leaf.End+1 {
				continue
			}
			local := ui.TextInputRange{Start: max(0, m.Start-r.leaf.Start), End: min(r.leaf.End, m.End) - r.leaf.Start}
			color := e.page.FindMatch
			if i == f.current {
				color = e.page.FindCurrent
			}
			for _, rect := range r.layout.SelectionRects(local) {
				rect.X += bounds.X + r.x
				rect.Y += bounds.Y + r.y
				p.Fill(rect, color, 2)
			}
		}
	}
}
