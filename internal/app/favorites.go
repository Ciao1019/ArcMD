package app

import (
	"time"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// The favorites are the documents pinned to the top of the sidebar, as
// Arc's and Cra's (MIT) measurements of them: the same in every space,
// tiles 47 DIPs tall with 13 DIP corners and the document's icon, in as
// many columns (up to four) as keep a tile as wide as it is tall, then the
// fewest that need no more rows. A document or a tile dragged over them
// opens a slot under the pointer, the tiles after it sliding one slot
// along over 0.15 s, and letting go puts it there. Under the tiles a strip
// takes a document dropped on it ("添加到收藏"); with no favorites a card
// stands in for the tiles until its × closes it.

// Favorite tiles as Arc 1.171 draws them (Cra's FavoritesLayout and
// FavoriteTileView).
const (
	favoriteTileH  = 47
	favoriteGap    = 8
	favoriteCorner = 13
	favoriteIcon   = 20
	// favoriteWidth is the grid's width: the sidebar less its margins.
	favoriteWidth = float32(sidebarWidth) - 20
	// favoriteCardH is the empty card's fill; the list goes 4 DIPs below.
	favoriteCardH = 93.5
)

// favoriteSlide is how the tiles move aside: fast at first, over 0.15 s,
// as Arc's do in a 60 fps capture.
var favoriteSlide = ui.Animation{Duration: 150 * time.Millisecond, Ease: ui.EaseOut}

// favoriteDrag is a favorite's tile being dragged to another place.
type favoriteDrag struct{ index int }

// favDrop is the slot the favorites hold open for what is dragged over
// them: a favorite (from, its index) or a document of the list (rel).
type favDrop struct {
	from int // -1 for a document of the list
	rel  string
	slot int
}

// favoriteLayout is the columns of n favorites in a grid width wide and
// the tiles' width: the most columns, up to four, that keep a tile at
// least as wide as it is tall, then the fewest that need no more rows (one
// favorite fills the row, five go 3 + 2).
func favoriteLayout(n int, width float32) (cols int, tileW float32) {
	widest := 1
	for k := 4; k >= 1; k-- {
		if (width-float32(k-1)*favoriteGap)/float32(k) >= favoriteTileH {
			widest = k
			break
		}
	}
	cols = widest
	if n > 0 {
		rows := (n + widest - 1) / widest
		cols = (n + rows - 1) / rows
	}
	return cols, (width - float32(cols-1)*favoriteGap) / float32(cols)
}

// favoriteGrid is the grid of n slots: its columns, the tiles' width and
// its height.
type favoriteGrid struct {
	n, cols  int
	tileW, h float32
}

func gridOf(n int) favoriteGrid {
	cols, w := favoriteLayout(n, favoriteWidth)
	g := favoriteGrid{n: n, cols: cols, tileW: w}
	if rows := (n + cols - 1) / cols; rows > 0 {
		g.h = float32(rows)*favoriteTileH + float32(rows-1)*favoriteGap
	}
	return g
}

// frame is where the slot i goes, on a half DIP as Arc puts each tile.
func (g favoriteGrid) frame(i int) (x, y float32) {
	r, col := i/g.cols, i%g.cols
	x = float32(int(float32(col)*(g.tileW+favoriteGap)*2+0.5)) / 2
	return x, float32(r) * (favoriteTileH + favoriteGap)
}

// slotAt is the slot a tile dragged to (x, y) would land in: the one under
// it, a gap going to the nearer side, past the last tile the last slot.
func (g favoriteGrid) slotAt(x, y float32) int {
	if g.n == 0 {
		return 0
	}
	rows := (g.n + g.cols - 1) / g.cols
	row := min(max(int((y+favoriteGap/2)/(favoriteTileH+favoriteGap)), 0), rows-1)
	col := min(max(int((x+favoriteGap/2)/(g.tileW+favoriteGap)), 0), g.cols-1)
	return min(row*g.cols+col, g.n-1)
}

// favorites builds the favorites: the grid and the strip under it, or the
// card standing for none.
func (a *App) favorites(c *ui.Context) {
	favs := a.state.Favorites
	drop := a.favDrop
	if len(favs) == 0 && !a.state.FavoritesHintDismissed {
		a.favoritesCard(c)
		return
	}
	// The tiles keeping a place: all but a favorite dragged over the grid.
	resting := make([]int, 0, len(favs))
	for i := range favs {
		if drop == nil || drop.from != i {
			resting = append(resting, i)
		}
	}
	slots := len(resting)
	if drop != nil {
		slots++
	}
	g := gridOf(slots)
	col := ui.Column(c.Key("favorites")).PaddingX(10)
	a.tourMark("favorites", col)
	col.Children(func() {
		// The height the layout takes is the animated one, so the list
		// under the tiles slides with them rather than jumping when a slot
		// adds a row.
		box := ui.Box(c.Key("favorites-grid")).Label(i18n.T("收藏"))
		box.Height(box.AnimateTo("height", g.h, listAnim))
		a.favoritesDrag(c, box, resting)
		dragging := false
		box.Children(func() {
			for k, i := range resting {
				slot := k
				if drop != nil && k >= drop.slot {
					slot = k + 1
				}
				if a.favoriteTile(c, i, favs[i], g, slot, false) {
					dragging = true
				}
			}
			if drop != nil && drop.from >= 0 && drop.from < len(favs) {
				// The favorite dragged waits, hidden, in the open slot.
				if a.favoriteTile(c, drop.from, favs[drop.from], g, drop.slot, true) {
					dragging = true
				}
			}
		})
		// A favorite's drag that ended anywhere gives its slot back.
		if drop != nil && drop.from >= 0 && !dragging {
			a.favDrop = nil
		}
		if len(favs) > 0 {
			ui.Box(c).Height(4)
		}
		a.favoritesStrip(c, len(favs) > 0)
	})
}

// favoritesDrag opens a slot under a document or a favorite dragged over
// the grid box, and puts it there when let go.
func (a *App) favoritesDrag(c *ui.Context, box ui.Element, resting []int) {
	slotUnder := func() int {
		x, y, _ := box.PointerPosition()
		return gridOf(len(resting)+1).slotAt(x, y)
	}
	dropped, isDrop := ui.Drop[sidebarDrag](box)
	over, isOver := ui.DragOver[sidebarDrag](box)
	if d, ok := dropped.(favoriteDrag); isDrop && ok {
		slot := slotUnder()
		if a.favDrop != nil {
			slot = a.favDrop.slot
		}
		a.state.MoveFavorite(d.index, slot)
		a.saveState()
		a.favDrop = nil
		return
	}
	if d, ok := dropped.(dragged); isDrop && ok {
		slot := slotUnder()
		if a.favDrop != nil {
			slot = a.favDrop.slot
		}
		a.pinAt(d, slot)
		a.favDrop, a.listDrop = nil, nil
		return
	}
	if d, ok := over.(favoriteDrag); isOver && ok {
		a.favDrop = &favDrop{from: d.index, slot: slotUnder()}
		return
	}
	if d, ok := over.(dragged); isOver && ok && a.isDocOf(d) {
		from := a.state.FavoriteIndex(d.space, d.rel)
		a.favDrop = &favDrop{from: from, rel: d.rel, slot: slotUnder()}
		return
	}
	// A document of the list gives its slot back as it leaves; a favorite
	// keeps it open until its drag ends.
	if a.favDrop != nil && a.favDrop.rel != "" {
		a.favDrop = nil
	}
}

// favoriteTile is the tile of the favorite at index i in slot of the grid
// g; hidden, it is the favorite being dragged, waiting in the open slot.
// It reports whether the tile is being dragged.
func (a *App) favoriteTile(c *ui.Context, i int, f space.Favorite, g favoriteGrid, slot int, hidden bool) bool {
	t := c.Theme()
	sp := a.state.Get(f.Space)
	title := vault.Title(f.Path)
	tip := title
	if sp != nil && sp != a.state.ActiveSpace() {
		tip = title + " · " + sp.Name
	}
	open := a.doc != nil && a.doc.space == sp && a.doc.rel == f.Path
	x, y := g.frame(slot)
	tile := ui.Box(c.Key("favorite:"+f.Space+"/"+f.Path)).Absolute().Left(x).Top(y).Size(g.tileW, favoriteTileH).
		Radius(favoriteCorner).Center().Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).
		Label(title).Tooltip(tip).Selected(open).Drag(favoriteDrag{i}).
		Transition(ui.ElementTransition{Animation: &favoriteSlide, Position: true, Size: true})
	dragging := tile.Dragging()
	tile.Background(favoriteFill(t, open, tile.Hovered() && !dragging))
	if tile.Hovered() && !dragging && sp != nil {
		// The favorites' tiles sit in the sidebar's margin, so the card
		// lines up with the rows'.
		b := tile.Bounds()
		b.W = sidebarWidth - rowInset - b.X
		a.hoverRow(previewDoc, sp, f.Path, b)
	}
	if open {
		tile.Border(2, favoriteRim(t))
	}
	if hidden || dragging {
		tile.Opacity(0) // the copy under the pointer stands for it
	}
	tile.Children(func() { docIcon(c, title, favoriteIcon) })
	if sp != nil && a.renameSpace == sp.ID && a.renaming == f.Path {
		a.renameUnder(c, tile)
	}
	tile.ContextMenu(func(m *ui.Menu) {
		if m.Item(i18n.T("在分屏中打开")).Disabled(sp == nil || a.doc == nil && a.split == nil).Chosen() {
			a.openSplit(sp, f.Path, false, false)
		}
		if m.Item(i18n.T("重命名…")).Disabled(sp == nil).Chosen() {
			a.renameIn(sp, f.Path)
		}
		if m.Item(i18n.T("从收藏中移除")).Chosen() {
			a.toggleFavorite(sp, f.Path)
		}
		if m.Item(i18n.T("在访达中显示")).Disabled(sp == nil).Chosen() {
			a.revealIn(sp, f.Path)
		}
	})
	if tile.Clicked() {
		a.openFavorite(f)
	}
	return dragging
}

// renameUnder edits a favorite's name in a field floating under its tile,
// the tile having no room for one: Enter or leaving it keeps the name,
// Escape gives up, as in a row.
func (a *App) renameUnder(c *ui.Context, tile ui.Element) {
	look := previewColors(c.Theme(), a.look)
	ui.Overlay(c, func() {
		card := ui.Row(c.Key("rename-favorite")).AttachTo(tile, ui.AnchorBottomLeft, ui.AnchorTopLeft).MarginY(6).
			Width(200).Padding(6, 6).Radius(10).Background(look.fill).Border(0.5, look.outline).
			Shadow(0, 4, 14, 0, look.shadow)
		card.Children(func() { a.renameField(c) })
	})
}

// favoritesStrip is the strip under the tiles: 6 DIPs of nothing (8 with no
// favorites), 36 saying "添加到收藏" while a document not pinned yet is
// dragged over it.
func (a *App) favoritesStrip(c *ui.Context, has bool) {
	t := c.Theme()
	h := float32(8)
	if has {
		h = 6
	}
	strip := ui.Box(c.Key("favorites-strip")).Radius(10).Center()
	accepts := func(d dragged) bool { return a.isDocOf(d) && !a.state.IsFavorite(d.space, d.rel) }
	if d, ok := ui.Drop[dragged](strip); ok && accepts(d) {
		a.pinAt(d, len(a.state.Favorites))
		a.listDrop = nil
	}
	d, over := ui.DragOver[dragged](strip)
	targeted := over && accepts(d)
	if targeted {
		h = 36
		strip.Background(hoverFill(t)).Draw(func(p *ui.Painter, r ui.Rect) {
			p.StrokeDashed(ui.Rect{X: r.X + 0.5, Y: r.Y + 0.5, W: r.W - 1, H: r.H - 1}, t.TextMuted.Alpha(0.6), 10, 1)
		})
	}
	strip.Height(strip.AnimateTo("height", h, listAnim)).Children(func() {
		if targeted {
			ui.Text(c, i18n.T("添加到收藏")).FontSize(11).FontWeight(500).TextColor(t.TextMuted).NoWrap()
		}
	})
}

// favoritesCard is Arc's empty favorites: a card where the tiles go, a
// star badge, "拖动以添加收藏" and a line on what they are. It takes a
// dragged document, the line going solid, the fill lighter and the rest
// half faded, and its × closes it for good.
func (a *App) favoritesCard(c *ui.Context) {
	t := c.Theme()
	col := ui.Column(c.Key("favorites")).PaddingX(10)
	a.tourMark("favorites", col)
	col.Children(func() {
		card := ui.Column(c.Key("favorites-card")).Height(favoriteCardH).Radius(12).AlignItems(ui.Center).Label(i18n.T("收藏"))
		if d, ok := ui.Drop[dragged](card); ok && a.isDocOf(d) {
			a.pinAt(d, 0)
			a.listDrop = nil
		}
		d, over := ui.DragOver[dragged](card)
		targeted := over && a.isDocOf(d)
		ink := t.Text
		text := t.Text
		if t.Dark {
			text = white.Alpha(0.85)
		}
		fill := white.Alpha(0.14)
		switch {
		case t.Dark && targeted:
			fill = ui.Color{}
		case t.Dark:
			fill = black.Alpha(0.135)
		case targeted:
			fill = white.Alpha(0.32)
		}
		card.Background(fill).Draw(func(p *ui.Painter, r ui.Rect) {
			edge := ui.Rect{X: r.X + 0.5, Y: r.Y + 0.5, W: r.W - 1, H: r.H - 1}
			if targeted {
				p.Stroke(edge, ink.Alpha(0.3), 11.5, 1)
			} else {
				p.StrokeDashed(edge, ink.Alpha(0.3), 11.5, 1)
			}
		})
		card.Children(func() {
			content := ui.Column(c.Key("card-content")).Grow(1).AlignSelf(ui.Stretch).AlignItems(ui.Center)
			if targeted {
				content.Opacity(0.5)
			}
			content.Children(func() {
				star := white.Alpha(0.95)
				if t.Dark {
					star = ui.RGB(40, 40, 40)
				}
				ui.Box(c).Size(16, 16).Margin(11.75, 0, 0, 0).Radius(8).Background(ink).Center().Children(func() {
					ui.Icon(c, iconStar).FontSize(10).TextColor(star)
				})
				ui.Text(c, i18n.T("拖动以添加收藏")).FontSize(13).FontWeight(600).TextColor(text).Margin(5.75, 0, 0, 0).NoWrap()
				ui.Text(c, i18n.T("收藏常用的文档，在每个空间都能一键打开")).FontSize(11).LineHeight(15.0/11).TextColor(text.Alpha(0.7)).
					MaxWidth(150).Margin(3.5, 0, 0, 0).TextAlign(ui.Center)
			})
			x := ui.Box(c.Key("card-close")).Absolute().Right(8).Top(8).Size(14, 14).Center().Cursor(ui.CursorPointer).
				Focusable().Role(ui.RoleButton).Label(i18n.T("关闭")).Tooltip(i18n.T("不再显示"))
			x.Children(func() { ui.Icon(c, iconX).FontSize(10).TextColor(text.Alpha(0.7)) })
			if x.Clicked() {
				a.state.FavoritesHintDismissed = true
				a.saveState()
			}
		})
		ui.Box(c).Height(4)
	})
}

// isDoc reports whether rel is a document of the active space's tree.
func (a *App) isDoc(rel string) bool {
	for _, d := range a.docs {
		if d.Path == rel {
			return true
		}
	}
	return false
}

// isDocOf reports whether the row d is a document: of the active space, one
// of its listed documents; of another, a document by its name.
func (a *App) isDocOf(d dragged) bool {
	if a.foreign(d) {
		return a.state.Get(d.space) != nil && vault.IsDocument(d.rel)
	}
	return a.isDoc(d.rel)
}

// pinAt pins the dragged document d, of its own space, among the favorites
// at index i, or moves it there when it is pinned already. Folders are not
// pinned.
func (a *App) pinAt(d dragged, i int) {
	sp, rel := a.state.Get(d.space), d.rel
	if sp == nil || !a.isDocOf(d) {
		return
	}
	if !a.state.IsFavorite(sp.ID, rel) {
		a.state.ToggleFavorite(sp.ID, rel)
	}
	sp.Unpin(rel) // one item is pinned or a favorite, never both
	a.state.MoveFavorite(a.state.FavoriteIndex(sp.ID, rel), i)
	a.saveState()
}
