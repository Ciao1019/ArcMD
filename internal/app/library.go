package app

import (
	"path"
	"slices"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/trashbin"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// The library is Arc's Library (Cra's LibraryView): not a window but a
// mode of the sidebar, opened by the button at its foot (⇧⌘L). The sidebar
// widens and shows a rail of pages on the left, the page beside it:
//
//   - 空间, every space as a card in its own theme, the sidebar as wide as
//     the cards need (Arc's Spaces page);
//   - 插件, the plugins, empty until the market opens;
//   - 最近删除, what was deleted recently, as Arc's archive.
//
// As Arc's, it is left by the shortcut, Escape or the back arrow, never by
// a click elsewhere, and reopens on the page last shown. Measured by Cra on
// Arc 1.170: a 113 DIP rail with a faint wash brightening to the right and
// a hairline; page buttons 97×80, 20 apart, centred; a 304 DIP list with a
// 36 DIP search field and 56 DIP rows; the back arrow 32×32, 8 in.

type libraryPage int

const (
	libSpaces libraryPage = iota
	libPlugins
	libTrash
)

var libraryPages = []struct {
	page  libraryPage
	title string
	icon  *ui.SVG
}{
	{libSpaces, "空间", iconSpaces},
	{libPlugins, "插件", iconPlugins},
	{libTrash, "最近删除", iconTrash},
}

const (
	libraryRail     = 113
	libraryList     = 304
	libraryButtonW  = 97
	libraryButtonH  = 80
	libraryRowH     = 56
	libraryHeaderH  = 40
	libraryCardW    = 240
	libraryCardGap  = 24
	libraryCardLead = 27
	libraryInset    = 48 // the cards from the window's top and bottom
	libraryPlus     = 29
	libraryPlusGap  = 34
	libraryTrail    = 37.5
	libraryPageRoom = 56 // what the Spaces page leaves the document
	librarySlide    = 14 // how far the library slides in from the left
	librarySlideIn  = 120 * time.Millisecond
	libraryCardRow  = 30
)

// library is the state of the library: shown or not, and its pages'.
type library struct {
	open bool
	page libraryPage // the page shown, kept while it is closed
	// frames counts the frames since it opened or closed, for the slide.
	frames int
	// trees and errs are each space's folder by ID, read as it opened.
	trees map[string][]*vault.Node
	errs  map[string]error
	cards ui.ScrollState
	// move is the card the pointer drags to reorder the spaces.
	move *cardMove
	// query filters what was deleted; items is what the bin lists.
	query string
	items []trashbin.Item
	err   error
}

// openLibrary shows the library on page.
func (a *App) openLibrary(page libraryPage) {
	a.finishSpaceEdit()
	l := &a.library
	if !l.open {
		l.frames = 0
	}
	l.open, l.page = true, page
	a.spaceForm = spaceForm{}
	a.renaming = ""
	a.palette.open = false
	a.loadLibrary()
}

// loadLibrary reads what the page shows.
func (a *App) loadLibrary() {
	l := &a.library
	switch l.page {
	case libSpaces:
		l.trees = make(map[string][]*vault.Node, len(a.state.Spaces))
		l.errs = make(map[string]error)
		for _, s := range a.state.Spaces {
			if s.ID == a.treeFor {
				l.trees[s.ID], l.errs[s.ID] = a.tree, a.treeErr
				continue
			}
			l.trees[s.ID], l.errs[s.ID] = vault.Tree(s.Root)
		}
	case libTrash:
		l.items, l.err = a.bin.List(time.Now())
	case libPlugins:
		a.reloadPlugins()
	}
}

// closeLibrary gives the sidebar back its spaces.
func (a *App) closeLibrary() {
	if a.library.open {
		a.library.open, a.library.frames, a.library.move = false, 0, nil
	}
}

// toggleLibrary opens the library on the page last shown, or closes it.
func (a *App) toggleLibrary() {
	if a.library.open {
		a.closeLibrary()
		return
	}
	a.openLibrary(a.library.page)
}

// toggleOverview opens the library on its spaces, or closes it there.
func (a *App) toggleOverview() {
	if a.library.open && a.library.page == libSpaces {
		a.closeLibrary()
		return
	}
	a.openLibrary(libSpaces)
}

// openTrash shows what was deleted recently.
func (a *App) openTrash() { a.openLibrary(libTrash) }

// openPlugins shows the plugins.
func (a *App) openPlugins() { a.openLibrary(libPlugins) }

// libraryWidth is the sidebar's width with the library in it, the window
// width wide: the rail and the list, or on the Spaces page as wide as
// every card needs, leaving the document some room (the cards scroll
// sideways when they do not fit).
func (a *App) libraryWidth(window float32) float32 {
	base := float32(libraryRail + libraryList)
	if a.library.page != libSpaces {
		return base
	}
	n := float32(len(a.state.Spaces))
	want := libraryRail + libraryCardLead + n*libraryCardW + max(n-1, 0)*libraryCardGap + libraryPlusGap + libraryPlus + libraryTrail
	return max(base, min(want, window-libraryPageRoom))
}

// libraryInk is the library's ink: black on light chrome, white on dark,
// faded per use.
func libraryInk(t *ui.Theme) ui.Color { return pick(t, black, white) }

// libraryView is the sidebar while the library is open, width wide.
func (a *App) libraryView(c *ui.Context, width float32) {
	l := &a.library
	if c.Shortcut(0, ui.KeyEscape) && !a.palette.open && !a.settingsOpen {
		if m := l.move; m != nil && !m.settling {
			m.canceled = true // Escape gives up a card's drag first
		} else {
			a.closeLibrary()
		}
	}
	row := ui.Row(c.Key("library")).Width(width).Grow(1).AlignItems(ui.Stretch)
	// It slides in from a little to the left, as Arc's.
	pose := ui.Pose{}
	if l.frames == 0 {
		pose = ui.Pose{X: -librarySlide, Fade: 1}
	}
	row.AnimatePose(pose, ui.Animation{Duration: librarySlideIn, Ease: ui.EaseOut})
	if l.frames++; l.frames == 1 {
		c.AnimationFrame()
	}
	row.Children(func() {
		a.libraryRail(c)
		ui.Column(c.Key("library-page")).Grow(1).MinWidth(0).Children(func() {
			switch l.page {
			case libSpaces:
				a.librarySpaces(c)
			case libPlugins:
				a.libraryPlugins(c)
			case libTrash:
				a.libraryTrash(c)
			}
		})
	})
}

// libraryRail is the column of pages: a faint wash brightening to the
// right and ending in a hairline, the page buttons centred, the back
// arrow at the foot.
func (a *App) libraryRail(c *ui.Context) {
	t := c.Theme()
	ink := libraryInk(t)
	bar := c.TitleBar()
	rail := ui.Column(c.Key("library-rail")).Width(libraryRail).Shrink(0).AlignItems(ui.Center).DragWindow()
	rail.Draw(func(p *ui.Painter, r ui.Rect) {
		wash := ui.Rect{X: r.X, Y: r.Y, W: r.W - 1, H: r.H}
		p.FillGradient(wash, ui.LinearGradient{Angle: 90, Stops: []ui.GradientStop{
			{Color: ink.Alpha(0.02), At: 0}, {Color: ink.Alpha(0.045), At: 1},
		}}, 0)
		p.Fill(ui.Rect{X: r.X + r.W - 1, Y: r.Y, W: 1, H: r.H}, ink.Alpha(0.09), 0)
	})
	rail.Children(func() {
		ui.Box(c).Height(max(bar.Height, 46)).Shrink(0)
		ui.Spacer(c)
		ui.Column(c).Gap(20).Children(func() {
			for _, pg := range libraryPages {
				a.libraryButton(c, pg.page, i18n.T(pg.title), pg.icon)
			}
		})
		ui.Spacer(c)
		ui.Row(c).AlignSelf(ui.Stretch).Height(32).Margin(0, 0, 8, 8).Children(func() {
			b := ui.Box(c.Key("library-back")).Size(32, 32).Radius(8).Center().Cursor(ui.CursorPointer).
				Focusable().Role(ui.RoleButton).Label(i18n.T("退出资料库")).Tooltip(i18n.T("退出资料库 (Esc)"))
			if b.Hovered() {
				b.Background(hoverFill(t))
			}
			b.Children(func() { ui.Icon(c, iconBack).FontSize(15).TextColor(ink.Alpha(0.6)) })
			if b.Clicked() {
				a.closeLibrary()
			}
		})
	})
}

// libraryButton is a page's button on the rail: its glyph and name, the
// page shown on a light fill with its glyph in the space's accent.
func (a *App) libraryButton(c *ui.Context, page libraryPage, title string, icon *ui.SVG) {
	t := c.Theme()
	ink := libraryInk(t)
	chosen := a.library.page == page
	b := ui.Column(c.Key("page:"+title)).Size(libraryButtonW, libraryButtonH).Radius(12).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Focusable().Role(ui.RoleTab).Label(title).Selected(chosen)
	if chosen && a.library.page != libTrash {
		// The keys leave the document, which would take Escape, for the
		// page shown; the trash page's search takes them there.
		b.AutoFocus()
	}
	switch {
	case chosen:
		b.Background(ink.Alpha(0.07))
	case b.Hovered():
		b.Background(ink.Alpha(0.04))
	}
	b.Children(func() {
		ui.Box(c).Height(21)
		glyph := ink.Alpha(0.6)
		if chosen {
			glyph = t.Accent
		}
		ui.Icon(c, icon).FontSize(19).TextColor(glyph)
		ui.Box(c).Height(9)
		alpha := float32(0.6)
		if chosen {
			alpha = 0.82
		}
		ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(ink.Alpha(alpha)).NoWrap()
	})
	if b.Clicked() && !chosen {
		a.openLibrary(page)
	}
}

// librarySpaces is the Spaces page: a card for every space in its own
// theme, and + making another.
func (a *App) librarySpaces(c *ui.Context) {
	t := c.Theme()
	row := ui.ScrollHorizontal(c.Key("library-cards")).Grow(1).MinHeight(0).TrackScroll(&a.library.cards).
		FocusGroup(ui.Horizontal)
	// The cards are as tall as the row, which a scroll does not give its
	// content by itself.
	height := row.Bounds().H
	row.Children(func() {
		ui.Row(c).Height(max(height, 160)).PaddingStart(libraryCardLead).PaddingEnd(libraryTrail).
			PaddingY(libraryInset).Gap(libraryCardGap).AlignItems(ui.Stretch).Children(func() {
			// A drag the grip starts while the row builds shows from the
			// next frame: this one built the card in its place already.
			moved := a.library.move
			shown, slot := a.cardOrder()
			for i, s := range shown {
				if i == slot {
					cardSlot(c)
				}
				a.spaceCard(c, s, false)
			}
			if slot == len(shown) {
				cardSlot(c)
			}
			if m := a.library.move; m != moved {
				c.AnimationFrame()
			} else if m != nil {
				// The card dragged, or gliding into its slot, above the rest.
				x, moving := a.movedCardX(c.Now())
				if s := a.state.Get(m.id); s != nil && moving {
					a.spaceCard(c, s, true).Absolute().Left(x).Top(libraryInset).
						Height(max(height, 160) - 2*libraryInset)
					if m.settling {
						c.AnimationFrame()
					}
				} else {
					// Home: it takes its slot in the row from the next frame.
					a.library.move = nil
					c.AnimationFrame()
				}
			}
			plus := ui.Box(c.Key("library-new-space")).Size(libraryPlus, libraryPlus).Shrink(0).AlignSelf(ui.Center).
				Margin(0, 0, 0, libraryPlusGap-libraryCardGap).Radius(libraryPlus / 2).Center().Cursor(ui.CursorPointer).
				Focusable().Role(ui.RoleButton).Label(i18n.T("新建空间")).Tooltip(i18n.T("新建空间")).Background(t.Accent)
			if plus.Hovered() {
				plus.Background(t.Accent.Mix(white, 0.15))
			}
			plus.Children(func() { ui.Icon(c, iconPlus).FontSize(16).TextColor(white) })
			if plus.Clicked() {
				a.closeLibrary()
				a.newSpaceForm()
			}
		})
	})
}

// spaceCard is one space's card on the Spaces page: its name, folders and
// documents, and a foot to make a document or open the space's menu.
// moved is the card the grip drags or one gliding into its slot, which the
// caller places; the others slide as the slots change.
func (a *App) spaceCard(c *ui.Context, s *space.Space, moved bool) ui.Element {
	base := a.docTheme
	look := lookOf(space.ThemeOf(s), base.Dark)
	t := spaceTheme(base, look)
	active := s == a.state.ActiveSpace()
	card := ui.Themed(c.Key("card-"+s.ID), t).Width(libraryCardW).Shrink(0).Radius(14).Padding(8).Gap(4).
		Role(ui.RoleGroup).Label(s.Name).Clip()
	if moved {
		// Lifted while the pointer holds it, settling flat into its slot.
		lift := a.library.move != nil && !a.library.move.settling
		pose := ui.Pose{}
		if lift {
			pose.Scale, pose.Y = 1.02, -4
		}
		card.AnimatePose(pose, ui.Animation{Duration: 160 * time.Millisecond, Ease: ui.EaseOut})
		if lift {
			card.Shadow(0, 14, 32, 0, black.Alpha(0.22))
		}
	} else {
		card.Transition(ui.ElementTransition{Animation: &listAnim, Position: true})
	}
	card.Draw(func(p *ui.Painter, r ui.Rect) {
		look.paint(p, r, 14, false)
		if active {
			p.Fill(r, white.Alpha(0.22), 14)
		}
	})
	switch {
	case active:
		card.Border(2, white.Alpha(0.7))
	default:
		card.Border(1, t.Border.Alpha(0.4))
	}
	if !moved || a.library.move.settling {
		if active {
			card.Shadow(0, 6, 18, 0, black.Alpha(0.18))
		} else {
			card.Shadow(0, 2, 8, 0, black.Alpha(0.1))
		}
	}
	card.Children(func() {
		h := ui.Row(c.Key("head")).Height(34).PaddingX(8).Gap(8).Radius(9).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("打开空间 ") + s.Name)
		hovered := h.Hovered()
		if hovered {
			h.Background(hoverFill(t))
		}
		h.Children(func() {
			spaceGlyph(c, s.Icon, s.Symbol, s.Name, 15, t.Accent)
			ui.Text(c, s.Name).FontWeight(650).SingleLine().Grow(1).MinWidth(0)
			edit := iconButton(c, "edit", iconPalette, i18n.T("编辑空间"), i18n.T("编辑空间"), false).Size(26, 26)
			edit.Opacity(map[bool]float32{true: 1, false: 0}[hovered || edit.Hovered()])
			if edit.Clicked() {
				a.closeLibrary()
				a.editSpace(s)
			}
		})
		if h.Clicked() {
			a.switchSpace(s.ID)
			if a.state.ActiveSpace() == s {
				a.closeLibrary()
			}
		}
		body := ui.Scroll(c.Key("body")).Grow(1).MinHeight(0).Gap(1)
		body.Children(func() {
			if err := a.library.errs[s.ID]; err != nil {
				ui.Text(c, i18n.T("读取文件夹失败。")+err.Error()).TextColor(t.Danger).FontSize(12).Padding(4, 8)
				return
			}
			nodes := a.library.trees[s.ID]
			if len(nodes) == 0 {
				ui.Text(c, i18n.T("暂无文档")).FontSize(12).TextColor(t.TextMuted).Padding(6, 10)
			}
			a.cardNodes(c, s, nodes, 0)
		})
		ui.Row(c.Key("foot")).Height(30).Gap(2).AlignItems(ui.Center).Children(func() {
			a.cardGrip(c, s, slices.Index(a.state.Spaces, s))
			ui.Spacer(c)
			iconButton(c, "menu", iconEllipsis, s.Name+i18n.T(" 的菜单"), i18n.T("更多"), false).Size(28, 28).Menu(func(m *ui.Menu) {
				if m.Item(i18n.T("编辑空间…")).Chosen() {
					a.closeLibrary()
					a.editSpace(s)
				}
				if m.Item(i18n.T("新建文档")).Chosen() {
					a.switchSpace(s.ID)
					if a.state.ActiveSpace() == s {
						a.closeLibrary()
						a.newDoc("")
					}
				}
				if m.Item(i18n.T("在访达中显示")).Chosen() {
					revealDir(s.Root)
				}
				m.Separator()
				if m.Item(i18n.T("从 ArcMD 移除")).Disabled(len(a.state.Spaces) < 2).Chosen() {
					a.removeSpace(s.ID)
					a.loadLibrary()
				}
			})
		})
	})
	return card
}

// cardNodes builds a card's folders and documents.
func (a *App) cardNodes(c *ui.Context, s *space.Space, nodes []*vault.Node, depth int) {
	for _, n := range nodes {
		if !n.Dir {
			a.cardDoc(c, s, n.Path, n.Title(), depth)
			continue
		}
		t := c.Theme()
		open := s.IsExpanded(n.Path)
		row := a.cardRow(c, "dir:"+n.Path, n.Title(), depth).Role(ui.RoleTreeItem).Expanded(open)
		row.Children(func() {
			icon := iconFolder
			if open {
				icon = iconFolderOpen
			}
			ui.Icon(c, icon).FontSize(15).TextColor(t.Accent)
			ui.Text(c, n.Title()).FontSize(13).FontWeight(600).SingleLine().Grow(1).MinWidth(0)
		})
		if row.Clicked() {
			s.SetExpanded(n.Path, !open)
			a.saveState()
		}
		if open {
			a.cardNodes(c, s, n.Children, depth+1)
		}
	}
}

// cardDoc is a document's row in a card; the open document is glass. A
// click opens it in its space and closes the library.
func (a *App) cardDoc(c *ui.Context, s *space.Space, rel, title string, depth int) {
	t := c.Theme()
	chosen := a.doc != nil && a.doc.space == s && a.doc.rel == rel
	row := a.cardRow(c, "doc:"+rel, title, depth).Role(ui.RoleTreeItem).Selected(chosen)
	if chosen {
		row.Background(glass(t)).Shadow(0, 1, 3, 0, black.Alpha(0.07))
	}
	row.Children(func() {
		docIcon(c, title, 15)
		name := ui.Text(c, docName(title)).FontSize(13).TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
		if chosen {
			name.FontWeight(600)
		}
	})
	if row.Clicked() {
		a.switchSpaceOnly(s.ID)
		if a.state.ActiveSpace() != s {
			return // the open document could not be saved
		}
		a.open(rel)
		a.closeLibrary()
	}
}

// cardRow is the frame shared by a card's rows.
func (a *App) cardRow(c *ui.Context, key, label string, depth int) ui.Element {
	t := c.Theme()
	row := ui.Row(c.Key(key)).Height(libraryCardRow).Shrink(0).PaddingStart(float32(8 + depth*16)).PaddingEnd(8).Gap(8).
		Radius(8).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Label(label).Tooltip(label)
	if row.Hovered() {
		row.Background(hoverFill(t))
	}
	return row
}

// libraryPlugins is the Plugins page, empty until the market opens.
func (a *App) libraryPlugins(c *ui.Context) {
	ui.Box(c).Height(46).Shrink(0)
	libraryEmpty(c, iconPlugins, i18n.T("插件市场即将开放"), i18n.T("扩展能力将以插件提供，敬请期待。"))
}

// libraryEmpty is a page with nothing on it: a glyph, a title and a line
// saying why.
func libraryEmpty(c *ui.Context, icon *ui.SVG, title, detail string) {
	ink := libraryInk(c.Theme())
	ui.Column(c.Key("library-empty")).Grow(1).PaddingX(16).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Height(168).Shrink(0)
		ui.Icon(c, icon).FontSize(30).TextColor(ink.Alpha(0.45))
		ui.Box(c).Height(12)
		ui.Text(c, title).FontSize(16).FontWeight(600).TextColor(ink.Alpha(0.9)).TextAlign(ui.Center)
		ui.Box(c).Height(8)
		ui.Text(c, detail).FontSize(14).TextColor(ink.Alpha(0.7)).TextAlign(ui.Center).MaxWidth(260)
	})
}

// libraryTrash is the Recently Deleted page, as Arc's archive: a search
// field, then what was deleted under the day it was, newest first.
func (a *App) libraryTrash(c *ui.Context) {
	l := &a.library
	t := c.Theme()
	ink := libraryInk(t)
	ui.Row(c.Key("trash-search")).Height(36).Shrink(0).Margin(10, 8, 0, 7).Radius(10).PaddingX(12).Gap(10).
		AlignItems(ui.Center).Background(librarySearch(t)).Children(func() {
		ui.Icon(c, iconSearch).FontSize(14).TextColor(ink.Alpha(0.5))
		ui.TextInput(c.Key("trash-query"), &l.query).Grow(1).MinWidth(0).Height(24).Padding(0).
			Placeholder(i18n.T("搜索最近删除…")).Label(i18n.T("搜索最近删除")).TextColor(ink.Alpha(0.82)).
			Background(ui.Transparent).Border(0, ui.Transparent).FontSize(14).AutoFocus()
	})
	switch {
	case l.err != nil:
		ui.Text(c, i18n.T("读取失败。")+l.err.Error()).TextColor(t.Danger).FontSize(13).Margin(16, 12, 0, 12)
		return
	case len(l.items) == 0:
		libraryEmpty(c, iconTrash, i18n.T("这里什么都没有"), i18n.T("从 ArcMD 删除的文档和文件夹会在这里保留 30 天，可以放回原处。"))
		return
	}
	words := strings.Fields(strings.ToLower(l.query))
	var shown []trashbin.Item
	for _, it := range l.items {
		hay := strings.ToLower(it.Name() + " " + it.SpaceName + " " + it.Path)
		ok := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			shown = append(shown, it)
		}
	}
	if len(shown) == 0 {
		ui.Text(c, i18n.T("没有符合搜索的项目")).FontSize(13).TextColor(ink.Alpha(0.55)).AlignSelf(ui.Center).Margin(24, 0, 0, 0)
		return
	}
	now := time.Now()
	ui.Scroll(c.Key("trash-list")).Grow(1).MinHeight(0).Children(func() {
		day := ""
		for _, it := range shown {
			if d := dayLabel(it.Deleted, now); d != day {
				day = d
				ui.Row(c.Key("day:"+d)).Height(libraryHeaderH).Shrink(0).Padding(0, 0, 6, 11).
					AlignItems(ui.End).Children(func() {
					ui.Text(c, d).FontSize(13).FontWeight(600).TextColor(ink.Alpha(0.6))
				})
			}
			a.trashRow(c, it, now)
		}
	})
}

// trashRow is a deleted item's row: its icon on a faint tile, its name
// over where it was and when it went, and, under the pointer, the bin
// (forget ArcMD's copy) and the arrow putting it back. A click puts it
// back too, as a click on an archived tab restores it in Arc.
func (a *App) trashRow(c *ui.Context, it trashbin.Item, now time.Time) {
	t := c.Theme()
	ink := libraryInk(t)
	name := it.Name()
	title := name
	if !it.Dir {
		title = vault.Title(it.Path)
	}
	where := it.SpaceName
	if dir := path.Dir(it.Path); dir != "." {
		where += " / " + dir
	}
	ui.Row(c.Key("trash:"+it.ID)).Height(libraryRowH).Shrink(0).Padding(2, 4, 2, 4).Children(func() {
		card := ui.Row(c.Key("card")).Grow(1).MinWidth(0).Radius(10).PaddingStart(9).PaddingEnd(14).Gap(10).
			AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(title).Tooltip(where + " / " + name)
		hovered := card.Hovered()
		if hovered {
			card.Background(hoverFill(t))
		}
		acted := false
		card.Children(func() {
			ui.Box(c).Size(24, 24).Shrink(0).Radius(6).Center().Background(ink.Alpha(0.04)).Children(func() {
				if it.Dir {
					ui.Icon(c, iconFolder).FontSize(15).TextColor(ink.Alpha(0.55))
				} else {
					docIcon(c, title, 16)
				}
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, docName(title)).FontSize(14).TextColor(ink.Alpha(0.82)).SingleLine()
				ui.Text(c, where+" · "+strings.TrimSpace(ago(it.Deleted, now))+i18n.T("删除")).FontSize(12).
					TextColor(ink.Alpha(0.55)).SingleLine()
			})
			forget := a.trashButton(c, "forget", iconTrash, i18n.T("从列表移除 ")+name, i18n.T("只移除 ArcMD 保留的副本，系统废纸篓中的原件不受影响"), hovered)
			if forget.Clicked() {
				acted = true
				a.bin.Forget(it)
				a.library.items, a.library.err = a.bin.List(time.Now())
			}
			restore := a.trashButton(c, "restore", iconUndo, i18n.T("恢复 ")+name, i18n.T("放回原处"), hovered).Margin(0, 0, 0, 10)
			if restore.Clicked() {
				acted = true
				a.restoreDeleted(it)
			}
		})
		if card.Clicked() && !acted {
			a.restoreDeleted(it)
		}
	})
}

// trashButton is a row's hover button: shown while the row is under the
// pointer or the button has the keys.
func (a *App) trashButton(c *ui.Context, key string, icon *ui.SVG, label, tip string, rowHovered bool) ui.Element {
	t := c.Theme()
	b := ui.Box(c.Key(key)).Size(24, 24).Shrink(0).Radius(6).Center().Cursor(ui.CursorPointer).
		Focusable().Role(ui.RoleButton).Label(label).Tooltip(tip)
	if b.Hovered() {
		b.Background(hoverFill(t))
	}
	if !rowHovered && !b.Focused() {
		b.Opacity(0)
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(14).TextColor(libraryInk(t).Alpha(0.6)) })
	return b
}

// dayLabel is the header of the day t was: 今天, 昨天, or its date.
func dayLabel(t, now time.Time) string {
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	today := time.Date(y2, m2, d2, 0, 0, 0, 0, now.Location())
	day := time.Date(y1, m1, d1, 0, 0, 0, 0, t.Location())
	switch {
	case day.Equal(today):
		return i18n.T("今天")
	case day.Equal(today.AddDate(0, 0, -1)):
		return i18n.T("昨天")
	case y1 == y2:
		return t.Format(i18n.T("1月2日"))
	}
	return t.Format(i18n.T("2006年1月2日"))
}

// libraryFootButton is the library's button at the sidebar's foot: Arc's
// Library glyph, as Cra draws it (LibraryGlyph), a quiet square like the
// toolbar's buttons.
func (a *App) libraryFootButton(c *ui.Context) ui.Element {
	t := c.Theme()
	b := ui.Box(c.Key("library")).Size(30, 30).Shrink(0).Radius(8).Cursor(ui.CursorPointer).Focusable().
		Role(ui.RoleButton).Label(i18n.T("资料库")).Tooltip(a.withKey(i18n.T("资料库"), "library")).Checked(a.library.open)
	if b.Hovered() || a.library.open {
		b.Background(hoverFill(t))
	}
	dark := t.Dark
	b.Draw(func(p *ui.Painter, r ui.Rect) { libraryGlyph(p, r, dark) })
	return b
}

// libraryGlyph draws Arc's Library glyph centred in r: a filled box with
// a light slot near its top, and a puff of confetti rising from it, a
// cloud, a dot and a triangle. The shapes are Cra's in a 28 DIP square.
func libraryGlyph(p *ui.Painter, r ui.Rect, dark bool) {
	ink, slot := black.Alpha(0.26), white.Alpha(0.7)
	if dark {
		ink, slot = white.Alpha(0.55), black.Alpha(0.5)
	}
	x0 := r.X + (r.W-28)/2
	y0 := r.Y + (r.H-28)/2
	at := func(x, y float32) (float32, float32) { return x0 + x, y0 + y }
	// The box and its slot.
	bx, by := at(7, 13)
	p.Fill(ui.Rect{X: bx, Y: by, W: 14, H: 11}, ink, 2.5)
	sx, sy := at(11.5, 15.4)
	p.Fill(ui.Rect{X: sx, Y: sy, W: 5, H: 1.6}, slot, 0.8)
	// The cloud: one outline, so its lobes do not darken where they meet.
	cloud := &ui.Path{}
	cloud.MoveTo(at(8.6, 10))
	cloud.LineTo(at(14.9, 10))
	c1x, c1y := at(16.7, 10)
	c2x, c2y := at(16.7, 7.1)
	ex, ey := at(14.9, 7.1)
	cloud.CubeTo(c1x, c1y, c2x, c2y, ex, ey)
	c1x, c1y = at(14.8, 3.4)
	c2x, c2y = at(10.1, 3.4)
	ex, ey = at(10.1, 6.4)
	cloud.CubeTo(c1x, c1y, c2x, c2y, ex, ey)
	c1x, c1y = at(7.2, 6.0)
	c2x, c2y = at(6.7, 10)
	ex, ey = at(8.6, 10)
	cloud.CubeTo(c1x, c1y, c2x, c2y, ex, ey)
	cloud.Close()
	p.FillPath(cloud, ink)
	// The dot, raised, and the triangle.
	dx, dy := at(18.3, 5)
	p.FillPath((&ui.Path{}).Circle(dx, dy, 1), ink)
	tri := &ui.Path{}
	tri.MoveTo(at(22.5, 5))
	tri.LineTo(at(24.7, 9))
	tri.LineTo(at(20.3, 9))
	tri.Close()
	p.FillPath(tri, ink)
}
