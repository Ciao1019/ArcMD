package app

import (
	"fmt"
	"path"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// dragged is a file or folder of the sidebar being dragged: the ID of its
// space, which it may leave for another (crossspace.go), and its path.
type dragged struct{ space, rel string }

// The list's rows as Cra measures Arc 1.171's: 40 DIPs tall and 1 apart,
// the fill 2 in from top and bottom with 10 DIP corners, starting after a
// nested row's indent of 16; the icon 10 into the fill.
const (
	rowHeight = 40
	rowIndent = 16
	rowGap    = 1
	rowInset  = 8 // the rows' margin from the sidebar's edges
)

// sidebar is Arc's sidebar: room for the window's buttons, the favorites,
// the strip of spaces with the active one's header and folders, and the
// spaces at the foot. floating draws it over the content, while it is
// folded away.
func (a *App) sidebar(c *ui.Context, floating bool) {
	sp := a.state.ActiveSpace()
	bar := c.TitleBar()
	// The strip of spaces runs edge to edge, so the sidebar's margins are
	// its parts' own.
	col := ui.Column(c.Key("sidebar")).Grow(1).MinHeight(0).Padding(0, 0, 10, 0).Gap(6)
	// A sideways swipe over the sidebar pages through the spaces.
	col.HandleInput(a.pagerScroll)
	col.Children(func() {
		top := ui.Row(c).Height(max(bar.Height, 46)).PaddingX(10).AlignItems(ui.Center)
		if !floating {
			top.PaddingStart(bar.Left).DragWindow()
		}
		top.Children(func() {
			ui.Spacer(c)
			label := "收起侧边栏"
			if floating {
				label = "固定侧边栏"
			}
			if iconButton(c, "toggle-sidebar", iconSidebar, label, label+" (⌘\\)", false).Clicked() {
				a.toggleSidebar()
			}
		})
		if sp == nil {
			return
		}
		padded := func(key string, fn func()) { ui.Column(c.Key(key)).PaddingX(10).Children(fn) }
		if f := &a.spaceForm; f.open && f.id == "" {
			ui.Column(c.Key("space-form-pad")).Grow(1).MinHeight(0).PaddingX(10).Children(func() { a.spaceDraft(c, f) })
			padded("foot", func() { a.spaceBar(c) })
			return
		}
		a.favorites(c)
		a.pagerView(c)
		a.updateBanner(c)
		padded("foot", func() { a.spaceBar(c) })
	})
}

// pagerView is the strip of spaces, each a page as wide as the sidebar, at
// the place the pager shows; only the pages in view are built.
func (a *App) pagerView(c *ui.Context) {
	box := ui.Column(c.Key("pager")).Grow(1).MinHeight(0).Clip()
	b := box.LayoutBounds()
	if b.W > 0 {
		a.pager.width = b.W
	}
	offset := float32(a.pagerOffset())
	moving := a.pager.moving()
	active := a.activeIndex()
	box.Children(func() {
		for i, s := range a.state.Spaces {
			left := float32(i-active)*b.W + offset
			// The page of a row dragged off to another space stays built,
			// out of sight: the drag lives only while its row does.
			carrying := a.carried != nil && a.carried.space == s.ID
			if i != active && !carrying && (b.W <= 0 || left >= b.W || left+b.W <= 0) {
				continue
			}
			page := ui.Column(c.Key("page-" + s.ID))
			if b.W > 0 {
				page.Absolute().Left(left).Top(0).Size(b.W, b.H)
			} else {
				page.Grow(1).MinHeight(0) // the first frame, before the strip's size is known
			}
			live := i == active && !moving
			page.Children(func() { a.spaceColumn(c, s, live, true) })
		}
	})
}

// spaceColumn is a space's page: its header, its pinned documents and
// folders, Cra's line, and the rest of its folders, in its own look. live
// is the active space at rest, whose rows act; the others show only.
func (a *App) spaceColumn(c *ui.Context, sp *space.Space, live, header bool) {
	build := func() {
		t := c.Theme()
		// Cra's column: the header 8 DIPs under the favorites, the list
		// from just under it.
		ui.Column(c.Key("column")).Grow(1).MinHeight(0).PaddingX(rowInset).Children(func() {
			if header {
				ui.Column(c.Key("header-pad")).Margin(8, 0, 0, 0).Children(func() { a.spaceHeader(c, sp, live) })
			}
			nodes, err := a.treeOf(sp)
			tree := ui.Scroll(c.Key("tree")).Grow(1).MinHeight(0).TrackScroll(&a.session(sp.ID).tree)
			var prev []rowFrame
			highlight := 0
			if live {
				prev = append([]rowFrame(nil), a.rows...)
				a.rows = a.rows[:0]
				a.listDrag(c, tree, prev)
				highlight = a.dropSection(a.listDrop)
				tree.ContextMenu(func(m *ui.Menu) { a.listMenu(m, sp) })
			}
			tree.Children(func() {
				if err != nil {
					ui.Text(c, "读取文件夹失败。"+err.Error()).TextColor(t.Danger).FontSize(12).Padding(4, 10)
				}
				pins := pinnedNodes(sp, nodes)
				// Cra's sections: 3 DIPs on top, the pinned rows (with the
				// placeholder taking a drag while there are none), the line,
				// then the rest. The section a drop pins or unpins into lights up.
				ui.Box(c).Height(3)
				// The sections, the line and the rows all move as Cra's list
				// moves its views: rows coming fade in, rows going fade out
				// where they were, the rest slide, over 0.22 s.
				pinned := ui.Column(c.Key("pinned")).Gap(rowGap).Radius(10).Transition(sectionMove)
				if highlight == sectionPinned && len(pins) > 0 {
					pinned.Background(hoverFill(t))
				}
				pinned.Children(func() {
					for i, n := range pins {
						// Folded, the section keeps only the pin holding the
						// open document in sight.
						if sp.PinsCollapsed && !a.holdsOpen(sp, n.Path) {
							continue
						}
						a.nodeRow(c, sp, n, 0, pinRoot, i, live)
						if n.Dir {
							a.folderContent(c, sp, n, 1, live)
						}
					}
					if len(pins) == 0 && live && a.listDragging {
						a.pinPlaceholder(c, highlight == sectionPinned)
					}
				})
				today := a.showsAny(sp, nodes)
				a.divider(c, live, today)
				rest := ui.Column(c.Key("today")).Gap(rowGap).Radius(10).Transition(sectionMove)
				if highlight == sectionToday {
					rest.Background(hoverFill(t))
				}
				rest.Children(func() {
					if live {
						a.splitSidebarRow(c, sp)
					}
					a.nodes(c, sp, nodes, 0, "", live)
					if err == nil && live {
						a.newDocRow(c)
					}
				})
				ui.Box(c).Height(40)
			})
			if live {
				a.dropLine(c)
			}
		})
	}
	// Every page sits in a Themed, the active one at rest in the window's
	// theme, so a row keeps its identity as its page stops being active:
	// a drag carried to another space goes on from it.
	th := c.Theme()
	if sp != a.state.ActiveSpace() || a.pager.moving() {
		th = spaceTheme(a.docTheme, lookOf(space.ThemeOf(sp), a.docTheme.Dark))
	}
	ui.Themed(c.Key("theme"), th).Grow(1).MinHeight(0).Background(ui.Transparent).Children(build)
}

// sectionMove slides the list's sections and line to new places with
// their rows.
var sectionMove = ui.ElementTransition{Animation: &listAnim, Position: true, Colors: true}

// divider is Cra's line between the pinned and the rest: 18 DIPs with a
// hairline 18 in from the sidebar's edges, while there is a rest to part.
func (a *App) divider(c *ui.Context, live, shown bool) {
	t := c.Theme()
	h := float32(0)
	if shown {
		h = 18
	}
	line := ui.Box(c.Key("divider")).Height(h).Transition(sectionMove)
	if live {
		b := line.LayoutBounds()
		a.dividerY = b.Y + b.H/2
	}
	if shown {
		line.Draw(func(p *ui.Painter, r ui.Rect) {
			inset := float32(18 - rowInset)
			p.Fill(ui.Rect{X: r.X + inset, Y: r.Y + (r.H-1)/2, W: r.W - 2*inset, H: 1}, separator(t), 0)
		})
	}
}

// pinPlaceholder is the empty pinned section while something is dragged:
// a dashed row saying "拖到此处置顶", lit while the drop would land there.
func (a *App) pinPlaceholder(c *ui.Context, active bool) {
	t := c.Theme()
	row := ui.Row(c.Key("pin-placeholder")).Height(rowHeight).Radius(10).Gap(6).Center()
	a.rows = append(a.rows, rowFrame{placeholder: true, parent: pinRoot, r: row.LayoutBounds()})
	border := t.TextMuted.Alpha(0.3)
	if active {
		border = t.TextMuted.Alpha(0.6)
		row.Background(hoverFill(t))
	}
	row.Draw(func(p *ui.Painter, r ui.Rect) {
		p.StrokeDashed(ui.Rect{X: r.X + 0.5, Y: r.Y + 0.5, W: r.W - 1, H: r.H - 1}, border, 10, 1)
	})
	row.Children(func() {
		ui.Icon(c, iconPin).FontSize(11).TextColor(t.TextMuted)
		ui.Text(c, "拖到此处置顶").FontSize(12).FontWeight(500).TextColor(t.TextMuted).NoWrap()
	})
}

// monogram is a document's mark on its tile: the first letter or emoji of
// its title.
func monogram(title string) string {
	for _, r := range title {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			continue
		}
		return string(unicode.ToUpper(r))
	}
	if r, _ := utf8.DecodeRuneInString(title); r != utf8.RuneError {
		return string(r)
	}
	return "·"
}

// spaceHeader is the space's header as Cra measures Arc's: 36 DIPs with
// 12 DIP corners, the icon in a 26 DIP box 5 in, the name 36 in at 13
// semibold in the secondary ink. Under the pointer it fills, a chevron
// takes the icon's place (down, or right while the pinned section is
// folded, turning a quarter over 0.12 s) and ••• opens the space's menu. A
// click on the icon folds the pins; clicking the name edits it in place.
// Files dropped on the header move to the root.
func (a *App) spaceHeader(c *ui.Context, sp *space.Space, live bool) {
	t := c.Theme()
	h := ui.Row(c.Key("space-header")).Height(36).PaddingEnd(5).Radius(12).AlignItems(ui.Center).
		Label("空间标题")
	over := live && a.dropZone(h, "")
	lit := live && h.Hovered()
	switch {
	case over:
		h.Background(t.Accent.Alpha(0.16))
	case lit && t.Dark:
		h.Background(white.Alpha(0.1))
	case lit:
		h.Background(hoverFill(t))
	}
	ink := t.TextMuted
	if t.Dark {
		ink = white.Alpha(0.45)
	}
	collapsed := sp.PinsCollapsed
	h.Children(func() {
		icon := ui.Box(c.Key("header-icon")).Size(26, 26).Margin(0, 0, 0, 5).Center()
		if live {
			icon.Focusable().Cursor(ui.CursorPointer).Role(ui.RoleButton).Label("折叠或展开置顶").Expanded(!collapsed).Tooltip("折叠或展开置顶")
		}
		icon.Children(func() {
			hasIcon := sp.Icon != "" || sp.Symbol != ""
			chevron := lit || collapsed && !hasIcon
			if chevron {
				turn := float32(0)
				if collapsed {
					turn = -90
				}
				ui.Icon(c.Key("chevron"), iconChevronDown).FontSize(13).TextColor(t.TextMuted).
					AnimatePose(ui.Pose{Rotate: turn}, ui.Animation{Duration: 120 * time.Millisecond, Ease: ui.EaseInOut})
			} else {
				spaceGlyph(c, sp.Icon, sp.Symbol, sp.Name, 14.5, t.Accent)
			}
		})
		if live && icon.Clicked() {
			sp.PinsCollapsed = !sp.PinsCollapsed
			a.saveState()
		}
		f := &a.spaceForm
		if live && f.open && f.id == sp.ID && f.edit == "name" {
			a.spaceNameField(c, f)
		} else {
			name := ui.Box(c.Key("space-title")).Grow(1).MinWidth(0).Height(30).Justify(ui.Center).Margin(0, 0, 0, 5)
			if live {
				name.Focusable().Role(ui.RoleButton).Label(sp.Name).Cursor(ui.CursorText).Tooltip("重命名空间")
			}
			name.Children(func() {
				ui.Text(c, sp.Name).FontSize(13).FontWeight(600).TextColor(ink).SingleLine().MinWidth(0)
			})
			if live && name.Clicked() {
				a.editSpaceProperty(sp, "name")
			}
		}
		more := iconButton(c, "space-more", iconEllipsis, "空间菜单", "空间菜单", false).Size(24, 24)
		if !lit {
			more.Opacity(0)
		}
		if live {
			more.Menu(func(m *ui.Menu) { a.listMenu(m, sp) })
		}
	})
	if !live {
		return
	}
	h.ContextMenu(func(m *ui.Menu) { a.listMenu(m, sp) })
	if f := &a.spaceForm; f.open && f.id == sp.ID && f.edit == "theme" {
		a.themeCard(c, f, h)
	}
}

// dropZone makes e take files and folders dragged onto it, moving them into
// the folder dir, and reports whether one is over it.
func (a *App) dropZone(e ui.Element, dir string) bool {
	if d, ok := ui.Drop[dragged](e); ok {
		if a.foreign(d) {
			a.moveAcross(d.space, d.rel, a.state.ActiveSpace().ID, dir)
		} else {
			a.moveInto(d.rel, dir)
		}
	}
	d, over := ui.DragOver[dragged](e)
	if over && a.foreign(d) {
		return true
	}
	return over && d.rel != dir && path.Dir(d.rel) != dir && !(dir == "" && path.Dir(d.rel) == ".")
}

// nodes builds the rows of folders and documents of the folder parent, the
// open folders' children under them. What is pinned or a favorite shows
// there alone: it has no row here.
func (a *App) nodes(c *ui.Context, sp *space.Space, nodes []*vault.Node, depth int, parent string, live bool) {
	for i, n := range nodes {
		if a.hidden(sp, n) {
			continue
		}
		a.nodeRow(c, sp, n, depth, parent, i, live)
		if n.Dir {
			a.folderContent(c, sp, n, depth+1, live)
		}
	}
}

// holdsOpen reports whether the open document is rel of the space sp or
// inside it.
func (a *App) holdsOpen(sp *space.Space, rel string) bool {
	return a.doc != nil && a.doc.space == sp && under(a.doc.rel, rel)
}

// folderContent is what a folder shows under its row: open, its rows; closed,
// the open document when it holds it, one level in, as Arc keeps the open
// tab of a closed folder in sight.
func (a *App) folderContent(c *ui.Context, sp *space.Space, n *vault.Node, depth int, live bool) {
	if sp.IsExpanded(n.Path) {
		a.folderBody(c, sp, n, depth, live)
		return
	}
	if !a.holdsOpen(sp, n.Path) || a.doc.rel == n.Path {
		return
	}
	doc := vault.Find(n.Children, a.doc.rel)
	if doc == nil || a.hidden(sp, doc) {
		return
	}
	parent := parentDir(doc.Path)
	index := 0
	if pn := vault.Find(n.Children, parent); pn != nil || parent == n.Path {
		kids := n.Children
		if pn != nil {
			kids = pn.Children
		}
		for i, k := range kids {
			if k.Path == doc.Path {
				index = i
			}
		}
	}
	a.nodeRow(c, sp, doc, depth, parent, index, live)
}

// columnLook is the look a space's page is drawn in.
func (a *App) columnLook(sp *space.Space) spaceLook {
	if sp == a.state.ActiveSpace() && !a.pager.moving() {
		return a.look
	}
	return lookOf(space.ThemeOf(sp), a.docTheme.Dark)
}

// folderBody is an open folder's rows, or a line saying it is empty.
func (a *App) folderBody(c *ui.Context, sp *space.Space, n *vault.Node, depth int, live bool) {
	if !a.showsAny(sp, n.Children) {
		ui.Text(c.Key("empty:"+n.Path), "空文件夹").FontSize(12).TextColor(c.Theme().TextMuted).
			PaddingStart(float32(10 + depth*rowIndent + 27)).Height(28).
			Transition(ui.ElementTransition{Animation: &listAnim, Position: true, Enter: &ui.Motion{Y: -6}, Exit: &ui.Motion{}})
		return
	}
	a.nodes(c, sp, n.Children, depth, n.Path, live)
}

// docCount is how many documents a folder holds, at any depth, as a closed
// folder tells on hover.
func docCount(n *vault.Node) int {
	k := 0
	for _, ch := range n.Children {
		if ch.Dir {
			k += docCount(ch)
		} else {
			k++
		}
	}
	return k
}

// nodeRow is a folder's or a document's row, as Cra draws Arc's: a click
// opens it, a double click renames it, and it drags to another place in
// the list, into a folder, across the line to pin or unpin it, or onto the
// favorites.
func (a *App) nodeRow(c *ui.Context, sp *space.Space, n *vault.Node, depth int, parent string, index int, live bool) {
	t := c.Theme()
	chosen := !n.Dir && a.doc != nil && a.doc.space == sp && a.doc.rel == n.Path
	open := n.Dir && sp.IsExpanded(n.Path)
	renaming := live && a.renameSpace == sp.ID && a.renaming == n.Path
	gap := float32(10)
	if n.Dir {
		gap = 11
	}
	row := ui.Row(c.Key("node:" + n.Path)).Height(rowHeight).PaddingStart(float32(10 + depth*rowIndent)).PaddingEnd(8).
		Gap(gap).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Label(n.Title())
	if n.Dir {
		row.Role(ui.RoleTreeItem).Expanded(open)
	} else {
		row.Role(ui.RoleTreeItem).Selected(chosen)
	}
	// Rows opening in a folder drop in from just above; others fade in
	// place, and all slide to new places, as Cra's list moves them.
	enter := &ui.Motion{}
	if depth > 0 {
		enter.Y = -6
	}
	row.Transition(ui.ElementTransition{Animation: &listAnim, Position: true, Enter: enter, Exit: &ui.Motion{}})
	// The row dragged to another space keeps dragging from its page,
	// which no longer acts.
	carried := a.carried != nil && *a.carried == dragged{sp.ID, n.Path}
	if live {
		a.rows = append(a.rows, rowFrame{rel: n.Path, parent: parent, index: index, depth: depth, dir: n.Dir, open: open, r: row.LayoutBounds()})
	}
	if live && !renaming || carried {
		row.Drag(dragged{sp.ID, n.Path})
	}
	d := a.listDrop
	var nudge float32
	if d != nil && d.line {
		switch n.Path {
		case d.above:
			nudge = -listNudge
		case d.below:
			nudge = listNudge
		}
	}
	row.AnimatePose(ui.Pose{Y: nudge}, listAnim)
	into := live && d != nil && d.kind == dropInto && d.parent == n.Path
	dragging := (live || carried) && row.Dragging()
	if dragging {
		a.sees(dragged{sp.ID, n.Path})
	}
	hovered := live && row.Hovered() && a.listDrop == nil && !a.listDragging
	// A pinned document stays open once shown (opendocs.go): open, it shows
	// a minus on hover that closes it and keeps it pinned; closed, it dims.
	pinned := !n.Dir && inPins(sp, n.Path)
	opened := pinned && a.isOpen(sp, n.Path)
	minus := opened && hovered && !renaming && !dragging
	if minus {
		row.PaddingEnd(6)
	}
	a.rowBounce(c, row, place{sp.ID, n.Path})
	if hovered && !renaming {
		// Resting here previews a closed folder's documents, or the
		// document.
		switch {
		case n.Dir && !open:
			a.hoverRow(previewFolder, sp, n.Path, row.Bounds())
		case !n.Dir && a.overClosed != (place{sp.ID, n.Path}):
			// Not while the pointer is on the row's minus, about to click it.
			a.hoverRow(previewDoc, sp, n.Path, row.Bounds())
		}
	}
	var fill ui.Color
	lifted := false
	switch {
	case dragging:
		fill = dragCard(t)
	case chosen:
		fill, lifted = glass(t), !t.Dark
	case hovered:
		fill = hoverFill(t)
	}
	if dragging {
		row.Opacity(0.4)
	}
	// The fill starts after the indent, so the gutter by a folder's rows
	// stays empty, as Arc's does; the row's own background, which a chosen
	// tree item would take, is none.
	row.Background(ui.Transparent)
	inset := float32(depth * rowIndent)
	row.Draw(func(p *ui.Painter, r ui.Rect) {
		fr := ui.Rect{X: r.X + inset, Y: r.Y + 2, W: r.W - inset, H: r.H - 4}
		if lifted {
			p.Shadow(fr, 10, 0, 1, 3, 0, black.Alpha(0.06))
		}
		if fill.A > 0 {
			p.Fill(fr, fill, 10)
		}
		if into {
			p.Stroke(fr, t.Text.Alpha(0.3), 10, 1.5)
		}
	})
	closed := false
	row.Children(func() {
		dim := pinned && !opened && !dragging
		if n.Dir {
			folderGlyph(c, a.columnLook(sp), open, a.holdsOpen(sp, n.Path))
		} else if icon := docIcon(c, n.Title(), 16); dim {
			icon.Opacity(closedIconAlpha)
		}
		if renaming {
			a.renameField(c)
			return
		}
		ink := t.Text
		if dim {
			ink = closedInk(t)
		}
		name := ui.Text(c, docName(n.Title())).FontSize(14).TextColor(ink).SingleLine().Grow(1).MinWidth(0)
		if n.Dir {
			name.FontWeight(500)
		}
		if n.Dir && !open && hovered {
			// A closed folder tells how many documents it holds.
			ui.Text(c, fmt.Sprint(docCount(n))).FontSize(11).FontWeight(500).TextColor(t.TextMuted)
		}
		switch {
		case minus:
			// The button takes the dot's place while hovered, as Arc's.
			b := ui.Box(c.Key("close-open")).Size(24, 24).Radius(6).Shrink(0).Cursor(ui.CursorPointer).
				Focusable().Role(ui.RoleButton).Label("关闭并保持置顶").Tooltip("关闭并保持置顶")
			if b.Hovered() {
				b.Background(hoverFill(t))
				a.overClose = place{sp.ID, n.Path}
			}
			ink := closeMarkInk(t)
			b.Draw(func(p *ui.Painter, r ui.Rect) { minusMark(p, r, ink) })
			if b.Clicked() {
				a.closeOpen(sp, n.Path)
				closed = true
			}
		case chosen && a.doc.dirty():
			ui.Box(c).Size(6, 6).Radius(3).Background(t.Accent).Tooltip("已修改")
		}
		if into {
			intoBadge(c)
		}
	})
	if !live {
		return
	}
	row.ContextMenu(func(m *ui.Menu) { a.nodeMenu(m, sp, n) })
	switch {
	case renaming, closed:
	case row.DoubleClicked():
		a.renameIn(sp, n.Path)
	case row.Clicked():
		if n.Dir {
			a.toggleFolder(n.Path)
		} else {
			a.open(n.Path)
		}
	}
}

// renameField edits the name of the row being renamed: Enter or leaving
// it keeps the name, Escape gives up.
func (a *App) renameField(c *ui.Context) {
	in := ui.TextInput(c.Key("rename"), &a.newName).Grow(1).MinWidth(0).Height(26).Padding(2, 6).
		AutoFocus().Label("名称")
	switch {
	case c.Shortcut(0, ui.KeyEscape):
		a.renaming = ""
	case in.Submitted():
		a.commitRename()
	case a.renameFocused && !in.Focused():
		a.commitRename()
	}
	a.renameFocused = in.Focused()
}

// nodeMenu is the context menu of a folder's or document's row, as Cra's
// tab and folder menus.
func (a *App) nodeMenu(m *ui.Menu, sp *space.Space, n *vault.Node) {
	pin := "置顶"
	if sp.IsPinned(n.Path) {
		pin = "取消置顶"
	}
	if n.Dir {
		if m.Item("重命名…").Chosen() {
			a.renameIn(sp, n.Path)
		}
		if m.Item("新建文档").Chosen() {
			a.newDoc(n.Path)
		}
		if m.Item("在其中新建文件夹").Chosen() {
			a.newFolder(n.Path)
		}
		if m.Item(pin).Chosen() {
			a.togglePin(sp, n.Path)
		}
	} else {
		if m.Item("在分屏中打开").Disabled(a.doc == nil && a.split == nil).Chosen() {
			a.openSplit(sp, n.Path, false, false)
		}
		if inPins(sp, n.Path) && a.isOpen(sp, n.Path) {
			if m.Item("关闭并保持置顶").Chosen() {
				a.closeOpen(sp, n.Path)
			}
			m.Separator()
		}
		if m.Item(pin).Chosen() {
			a.togglePin(sp, n.Path)
		}
		fav := "添加到收藏"
		if a.state.IsFavorite(sp.ID, n.Path) {
			fav = "从收藏中移除"
		}
		if m.Item(fav).Chosen() {
			a.toggleFavorite(sp, n.Path)
		}
		m.Separator()
		if m.Item("重命名…").Chosen() {
			a.renameIn(sp, n.Path)
		}
		m.Separator()
		var folders []*vault.Node
		for _, f := range a.tree {
			if f.Dir {
				folders = append(folders, f)
			}
		}
		if len(folders) > 0 {
			m.Submenu("移到文件夹", func(sub *ui.Menu) {
				for _, f := range folders {
					if sub.Item(f.Name).Disabled(parentDir(n.Path) == f.Path).Chosen() {
						a.placeEntry(n.Path, f.Path, len(a.childrenOf(f.Path)))
					}
				}
			})
		}
		if m.Item("新建包含此文档的文件夹").Chosen() {
			a.newFolderWith(n.Path)
		}
	}
	a.spacesMenu(m, sp, n.Path)
	m.Separator()
	if m.Item("在访达中显示").Chosen() {
		a.revealInFinder(n.Path)
	}
	if m.Item("移到废纸篓").Chosen() {
		a.trash(n.Path)
	}
}

// listMenu is the menu of the list's empty space, as Cra's: the space's
// look first, then what the list can make.
func (a *App) listMenu(m *ui.Menu, sp *space.Space) {
	a.spaceEditMenu(m, sp)
	m.Separator()
	if m.Item("新建文档").Shortcut(ui.Cmd, ui.KeyN).Chosen() {
		a.newDoc("")
	}
	if m.Item("新建文件夹").Shortcut(ui.Cmd|ui.Shift, ui.KeyN).Chosen() {
		a.newFolder("")
	}
	m.Separator()
	if m.Item("在访达中显示").Chosen() {
		revealDir(sp.Root)
	}
	if m.Item("最近删除…").Chosen() {
		a.openTrash()
	}
}

// spaceEditMenu keeps the three independent editors available from both
// the header and the space switcher's context menus.
func (a *App) spaceEditMenu(m *ui.Menu, sp *space.Space) {
	if m.Item("更改空间图标…").Chosen() {
		a.editSpaceProperty(sp, "icon")
	}
	if m.Item("重命名空间…").Chosen() {
		a.editSpaceProperty(sp, "name")
	}
	if m.Item("编辑主题颜色…").Chosen() {
		a.editSpace(sp)
	}
}

// spaceBar is the foot of the sidebar, as Arc's: the library at the left,
// the spaces spread evenly across the middle (scrolling when they do not
// fit), the chosen one bright and the others faint, and + at the right.
func (a *App) spaceBar(c *ui.Context) {
	t := c.Theme()
	active := a.state.ActiveSpace()
	changed := active != nil && a.spaceBarFor != active.ID
	if active != nil {
		a.spaceBarFor = active.ID
	}
	draft := a.spaceForm.open && a.spaceForm.id == ""
	ui.Row(c.Key("spaces")).Height(36).Gap(2).AlignItems(ui.Center).Children(func() {
		if a.libraryFootButton(c).Clicked() {
			a.toggleLibrary()
		}
		list := ui.ScrollHorizontal(c.Key("space-list")).Grow(1).MinWidth(0).Height(34).
			FocusGroup(ui.Horizontal).TrackScroll(&a.spaceScroll).Scrollbars(false)
		width := list.Bounds().W
		list.Children(func() {
			// As wide as the bar at least, so a few spaces spread across it.
			ui.Row(c.Key("space-row")).MinWidth(width).Height(34).Gap(2).Justify(ui.SpaceEvenly).AlignItems(ui.Center).Children(func() {
				for i, s := range a.state.Spaces {
					tip := s.Name
					if i < 9 {
						tip = fmt.Sprintf("%s (⌘%d)", s.Name, i+1)
					}
					b := ui.Box(c.Key("space-"+s.ID)).Size(30, 30).Shrink(0).Radius(8).Center().Cursor(ui.CursorPointer).
						Focusable().Role(ui.RoleButton).Label("切换到 " + s.Name).Tooltip(tip)
					on := active != nil && s.ID == active.ID && !draft
					b.Checked(on)
					if on {
						b.Tooltip("更改空间图标").Expanded(a.spaceForm.open && a.spaceForm.id == s.ID && a.spaceForm.edit == "icon")
					}
					if on && changed && list.FocusWithin() {
						b.Focus()
					}
					if on && changed || b.FocusVisible() {
						b.ScrollIntoView()
					}
					targeted := a.spaceIconDrag(c, b, s, on)
					if b.Hovered() && !on || targeted {
						b.Background(hoverFill(t))
					}
					b.Children(func() { a.spaceBarGlyph(c, s.Icon, s.Symbol, s.Name, on, b.Hovered()) })
					b.ContextMenu(func(m *ui.Menu) {
						a.spaceEditMenu(m, s)
						m.Separator()
						if m.Item("在访达中显示").Chosen() {
							revealDir(s.Root)
						}
						m.Separator()
						if m.Item("从 ArcMD 移除").Disabled(len(a.state.Spaces) < 2).Chosen() {
							a.removeSpace(s.ID)
						}
					})
					if b.Clicked() {
						if on {
							if a.spaceForm.open && a.spaceForm.id == s.ID && a.spaceForm.edit == "icon" {
								a.cancelSpaceForm()
							} else {
								a.editSpaceProperty(s, "icon")
							}
						} else {
							a.switchSpace(s.ID)
						}
					}
					if f := &a.spaceForm; f.open && f.id == s.ID && f.edit == "icon" {
						a.iconPicker(c, f, b)
					}
				}
				if f := &a.spaceForm; draft {
					// The draft takes its place at the end, chosen.
					d := ui.Box(c.Key("space-draft")).Size(30, 30).Shrink(0).Center().Label("新空间")
					if changed || a.spaceBarFor != "draft" {
						d.ScrollIntoView()
					}
					d.Children(func() { a.spaceBarGlyph(c, f.icon, f.symbol, f.name, true, false) })
				}
			})
		})
		if draft {
			a.spaceBarFor = "draft"
		}
		if iconButton(c, "add-space", iconPlus, "新建空间", "新建空间", false).Shrink(0).Clicked() {
			a.newSpaceForm()
		}
	})
}

// spaceBarGlyph is a space's icon on the bar: in the accent and a little
// larger when chosen, faint otherwise and brighter under the pointer.
func (a *App) spaceBarGlyph(c *ui.Context, emoji, symbol, name string, on, hovered bool) {
	t := c.Theme()
	size, color := float32(15), t.Text
	if on {
		size, color = 17, t.Accent
	}
	g := spaceGlyph(c, emoji, symbol, name, size, color)
	switch {
	case on:
	case hovered:
		g.Opacity(0.8)
	default:
		g.Opacity(0.42)
	}
}

// newDocRow ends the tree with a faint row making a document at the root,
// as Arc's New Tab row ends its tabs; it is also the empty space's hint.
func (a *App) newDocRow(c *ui.Context) {
	t := c.Theme()
	row := ui.Row(c.Key("new-doc-row")).Height(rowHeight).PaddingX(10).Gap(10).Radius(10).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label("新建文档").Tooltip("新建文档 (⌘N)")
	alpha := float32(0.45)
	if row.Hovered() {
		row.Background(hoverFill(t))
		alpha = 0.75
	}
	row.Children(func() {
		ui.Icon(c, iconPlus).FontSize(16).TextColor(t.Text.Alpha(alpha))
		ui.Text(c, "新建文档").TextColor(t.Text.Alpha(alpha))
	})
	if row.Clicked() {
		a.newDoc("")
	}
}
