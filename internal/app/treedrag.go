package app

import (
	"path"
	"slices"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// Dragging in the sidebar's list follows Cra's (MIT) list, after Arc's:
// the row dragged follows the pointer on a card while its place dims; over
// the top or bottom half of a row (a quarter for a folder) a line shows
// where it lands, the rows either side easing 3 DIPs apart; over the middle
// of a folder the folder takes it, saying so, and a closed folder held over
// for 0.6 s opens. Letting go moves the file there, folders included, and
// the rows slide to their new places. The order is the space's to keep
// (Space.Order): files are listed as they were dragged.

const (
	// listNudge is how far the rows either side of the line ease apart.
	listNudge = 3
	// springDelay is how long a closed folder is held over before it opens.
	springDelay = 600 * time.Millisecond
)

// listAnim is how the list's rows move: SwiftUI's snappy curve over
// 0.22 s, as Cra's list moves them.
var listAnim = ui.Animation{Duration: 220 * time.Millisecond, Ease: ui.CubicBezier(0.2, 0.9, 0.25, 1)}

// pageTree is a space's folders as read for the strip of spaces.
type pageTree struct {
	nodes []*vault.Node
	err   error
}

// readTree reads the space's folder in the order the space keeps.
func readTree(sp *space.Space) ([]*vault.Node, error) {
	nodes, err := vault.Tree(sp.Root)
	arrangeNodes(sp, "", nodes)
	return nodes, err
}

func arrangeNodes(sp *space.Space, dir string, nodes []*vault.Node) {
	if len(nodes) == 0 {
		return
	}
	names := make([]string, len(nodes))
	byName := make(map[string]*vault.Node, len(nodes))
	for i, n := range nodes {
		names[i] = n.Name
		byName[n.Name] = n
	}
	for i, name := range sp.Arrange(dir, names) {
		nodes[i] = byName[name]
	}
	for _, n := range nodes {
		if n.Dir {
			arrangeNodes(sp, n.Path, n.Children)
		}
	}
}

// treeOf is the space's folders: the active space's, or those read for
// the strip while it shows the space.
func (a *App) treeOf(sp *space.Space) ([]*vault.Node, error) {
	if sp == a.state.ActiveSpace() {
		return a.tree, a.treeErr
	}
	if pt, ok := a.pageTrees[sp.ID]; ok {
		return pt.nodes, pt.err
	}
	if a.pageTrees == nil {
		a.pageTrees = map[string]pageTree{}
	}
	nodes, err := readTree(sp)
	a.pageTrees[sp.ID] = pageTree{nodes, err}
	return nodes, err
}

// pinRoot stands for the space's pinned section as a parent: inserting
// there pins at the index.
const pinRoot = "\x00pins"

// pinnedNodes are the space's pins as found in its folders, in order.
func pinnedNodes(sp *space.Space, nodes []*vault.Node) []*vault.Node {
	var out []*vault.Node
	for _, p := range sp.Pins {
		if n := vault.Find(nodes, p); n != nil {
			out = append(out, n)
		}
	}
	return out
}

// hidden reports whether rel has no row of its own in the list's folders:
// a favorite or a pin of the space shows elsewhere.
func (a *App) hidden(sp *space.Space, n *vault.Node) bool {
	return sp.IsPinned(n.Path) || !n.Dir && (a.state.IsFavorite(sp.ID, n.Path) || a.grouped(sp, n.Path))
}

// pinnedSection reports whether rel shows in the active space's pinned
// section: pinned, or inside a pinned folder.
func (a *App) pinnedSection(rel string) bool {
	for _, p := range a.state.ActiveSpace().Pins {
		if under(rel, p) {
			return true
		}
	}
	return false
}

// childrenOf is what the folder dir of the active space holds, as shown;
// for pinRoot, its pins.
func (a *App) childrenOf(dir string) []*vault.Node {
	if dir == pinRoot {
		return pinnedNodes(a.state.ActiveSpace(), a.tree)
	}
	if dir == "" {
		return a.tree
	}
	if n := vault.Find(a.tree, dir); n != nil {
		return n.Children
	}
	return nil
}

// rowFrame is where a row of the list was in the frame before.
type rowFrame struct {
	rel, parent string
	index       int // among its parent's children
	depth       int
	dir, open   bool
	// placeholder is the pinned section's "拖到此处置顶" row.
	placeholder bool
	r           ui.Rect
}

// The kinds of listDrop.
const (
	dropInsert = iota + 1
	dropInto
)

// listDrop is where a dragged file or folder would land: inserted at
// index in the folder parent, with a line at y from x0 to x1, or into the
// folder parent. above and below are the rows either side of the line.
type listDrop struct {
	rel          string
	kind         int
	parent       string
	index        int
	line         bool
	y, x0, x1    float32
	above, below string
}

// sidebarDrag is what the sidebar drags: a row of the list (dragged) or a
// favorite's tile (favoriteDrag).
type sidebarDrag interface{ sidebarDrag() }

func (dragged) sidebarDrag()      {}
func (favoriteDrag) sidebarDrag() {}

// springOpen is the closed folder held under a drag, and since when.
type springOpen struct {
	rel   string
	since time.Time
}

// under reports whether p is rel or inside it.
func under(p, rel string) bool { return p == rel || strings.HasPrefix(p, rel+"/") }

// parentDir is the folder holding rel, "" for the root.
func parentDir(rel string) string {
	if d := path.Dir(rel); d != "." {
		return d
	}
	return ""
}

// resolveDrop is where the file or folder rel dragged with the pointer at
// (px, py) in window DIPs would land, as Cra resolves its list's drops: the
// top and bottom quarters of a folder's row insert before and after it (an
// open folder's bottom, at its top), its middle takes it; a document's row
// inserts before its top half and after its bottom one. Over no row it goes
// at the start or the end of the root. Nil changes nothing.
func (a *App) resolveDrop(rel string, px, py float32, rows []rowFrame) *listDrop {
	const half = 1
	d := &listDrop{rel: rel}
	var hit *rowFrame
	for i := range rows {
		r := &rows[i]
		if py >= r.r.Y-half && py < r.r.Y+r.r.H+half {
			hit = r
			break
		}
	}
	switch {
	case hit != nil && hit.placeholder:
		d.kind, d.parent, d.index = dropInsert, pinRoot, 0
	case hit != nil:
		f := (py - hit.r.Y) / max(1, hit.r.H)
		before := func() { d.kind, d.parent, d.index = dropInsert, hit.parent, hit.index }
		after := func() { d.kind, d.parent, d.index = dropInsert, hit.parent, hit.index+1 }
		switch {
		case under(hit.rel, rel):
			return nil
		case hit.dir && f < 0.25:
			before()
		case hit.dir && f >= 0.75 && hit.open:
			d.kind, d.parent, d.index = dropInsert, hit.rel, 0
		case hit.dir && f >= 0.75:
			after()
		case hit.dir:
			d.kind, d.parent = dropInto, hit.rel
		case f < 0.5:
			before()
		default:
			after()
		}
	case py < a.dividerY:
		// Between the sections, or past the pinned rows: the line splits
		// the pinned from the rest, as Cra's divider does.
		pins := a.childrenOf(pinRoot)
		d.kind, d.parent, d.index = dropInsert, pinRoot, len(pins)
		if len(rows) > 0 && py < rows[0].r.Y {
			d.index = 0
		}
	default:
		d.kind, d.parent, d.index = dropInsert, "", len(a.tree)
		for _, r := range rows {
			if r.parent == "" {
				if py < r.r.Y {
					d.index = 0 // above the first row after the line
				}
				break
			}
		}
	}
	sp := a.state.ActiveSpace()
	pinned := sp.IsPinned(rel)
	if d.kind == dropInto {
		if parentDir(rel) == d.parent && !pinned || under(d.parent, rel) {
			return nil // already there, or into itself
		}
		return d
	}
	if under(d.parent, rel) {
		return nil
	}
	kids := a.childrenOf(d.parent)
	if d.parent == pinRoot && pinned || d.parent != pinRoot && !pinned && parentDir(rel) == d.parent {
		old := slices.IndexFunc(kids, func(n *vault.Node) bool { return n.Path == rel })
		if d.index == old || d.index == old+1 {
			return nil // next to itself
		}
	}
	// The line: above the row it goes before, else below the last one
	// it goes after (with what that holds open), else under the folder.
	var y float32
	found := false
	at := func(p string) *rowFrame {
		for i := range rows {
			if rows[i].rel == p {
				return &rows[i]
			}
		}
		return nil
	}
	if d.index < len(kids) {
		if r := at(kids[d.index].Path); r != nil {
			y, found = r.r.Y-half, true
		}
	}
	if !found && d.index > 0 && d.index-1 < len(kids) {
		prev := kids[d.index-1].Path
		for i := len(rows) - 1; i >= 0; i-- {
			if under(rows[i].rel, prev) {
				y, found = rows[i].r.Y+rows[i].r.H+half, true
				break
			}
		}
	}
	if !found && d.parent != "" && d.parent != pinRoot {
		if r := at(d.parent); r != nil {
			y, found = r.r.Y+r.r.H+half, true
		}
	}
	if !found || len(rows) == 0 {
		return d
	}
	depth := 0
	if r := at(d.parent); r != nil {
		depth = r.depth + 1
	}
	left, right := rows[0].r.X, rows[0].r.X+rows[0].r.W
	d.line, d.y = true, y
	d.x0, d.x1 = left+10+float32(depth*rowIndent), right-6
	for _, r := range rows {
		if r.r.Y+r.r.H <= y+1 {
			d.above = r.rel
		}
	}
	for _, r := range rows {
		if r.r.Y >= y-1 {
			d.below = r.rel
			break
		}
	}
	return d
}

// showsAny reports whether any of nodes has a row: a folder holding only
// favorites shows as empty.
func (a *App) showsAny(sp *space.Space, nodes []*vault.Node) bool {
	for _, n := range nodes {
		if n.Dir || !a.state.IsFavorite(sp.ID, n.Path) && !a.grouped(sp, n.Path) {
			return true
		}
	}
	return false
}

// listDrag reads a drag over the list's element e, whose rows of the frame
// before are rows: where it would land, the folder it springs open, and
// what letting go does.
func (a *App) listDrag(c *ui.Context, e ui.Element, rows []rowFrame) {
	// A favorite of this space dragged into the list leaves the favorites
	// for the place it is let go at, as Arc's do.
	sp := a.state.ActiveSpace()
	favorite := func(i int) (string, bool) {
		if i < 0 || i >= len(a.state.Favorites) || a.state.Favorites[i].Space != sp.ID {
			return "", false
		}
		return a.state.Favorites[i].Path, true
	}
	// One query of each kind: an element takes the type it last asked
	// about, so the favorites' tiles and the list's rows share one.
	dropped, isDrop := ui.Drop[sidebarDrag](e)
	over, isOver := ui.DragOver[sidebarDrag](e)
	a.listDragging = isOver
	if d, ok := dropped.(favoriteDrag); isDrop && ok {
		if rel, ok := favorite(d.index); ok {
			x, y, _ := c.Root().PointerPosition()
			drop := a.resolveDrop(rel, x, y, rows)
			a.state.ToggleFavorite(sp.ID, rel)
			a.saveState()
			if drop != nil {
				a.applyDrop(drop)
			}
		}
		a.listDrop, a.favDrop = nil, nil
		return
	}
	if d, ok := over.(favoriteDrag); isOver && ok {
		if rel, ok := favorite(d.index); ok {
			x, y, _ := c.Root().PointerPosition()
			a.listDrop = a.resolveDrop(rel, x, y, rows)
			if a.listDrop == nil {
				// Next to where it sits hidden: it still leaves the favorites.
				a.listDrop = &listDrop{rel: rel}
			}
		}
		return
	}
	if d, ok := dropped.(dragged); isDrop && ok && a.foreign(d) {
		drop := a.listDrop
		if drop == nil || drop.rel != foreignKey(d.rel) {
			x, y, _ := c.Root().PointerPosition()
			drop = a.resolveDrop(foreignKey(d.rel), x, y, rows)
		}
		a.listDrop, a.springOpen = nil, springOpen{}
		a.dropForeign(d, drop)
		return
	}
	if d, ok := dropped.(dragged); isDrop && ok {
		drop := a.listDrop
		if drop == nil || drop.rel != d.rel {
			x, y, _ := c.Root().PointerPosition()
			drop = a.resolveDrop(d.rel, x, y, rows)
		}
		a.listDrop, a.springOpen = nil, springOpen{}
		if drop != nil {
			a.applyDrop(drop)
		}
		return
	}
	d, ok := over.(dragged)
	if !isOver || !ok {
		a.listDrop, a.springOpen = nil, springOpen{}
		return
	}
	key := d.rel
	if a.foreign(d) {
		key = foreignKey(d.rel)
	}
	x, y, _ := c.Root().PointerPosition()
	a.listDrop = a.resolveDrop(key, x, y, rows)
	// Spring-loaded folders: one held under the drag opens.
	if drop := a.listDrop; drop != nil && drop.kind == dropInto && !sp.IsExpanded(drop.parent) {
		switch {
		case a.springOpen.rel != drop.parent:
			a.springOpen = springOpen{drop.parent, c.Now()}
			c.After(springDelay)
		case c.Now().Sub(a.springOpen.since) >= springDelay:
			sp.SetExpanded(drop.parent, true)
			a.saveState()
			a.springOpen = springOpen{}
		default:
			c.After(springDelay - c.Now().Sub(a.springOpen.since))
		}
	} else {
		a.springOpen = springOpen{}
	}
}

// dropSection is the section a drop lands in when that pins or unpins
// what is dragged, which lights up: sectionPinned, sectionToday or none.
func (a *App) dropSection(d *listDrop) int {
	if d == nil || d.kind == 0 {
		return 0
	}
	lands := sectionToday
	if d.parent == pinRoot || a.pinnedSection(d.parent) && d.parent != "" {
		lands = sectionPinned
	}
	from := sectionToday
	if a.pinnedSection(d.rel) {
		from = sectionPinned
	}
	if lands == from {
		return 0
	}
	return lands
}

// The sections of the list.
const (
	sectionPinned = iota + 1
	sectionToday
)

// applyDrop moves the dragged file or folder where the drop says: among
// the pins it is pinned there, elsewhere it leaves the pins for the place.
func (a *App) applyDrop(d *listDrop) {
	sp := a.state.ActiveSpace()
	if d.kind == 0 {
		return
	}
	if d.parent == pinRoot {
		a.pinIn(sp, d.rel, d.index)
		return
	}
	if sp.IsPinned(d.rel) {
		sp.Unpin(d.rel)
		a.saveState()
	}
	switch d.kind {
	case dropInsert:
		a.placeEntry(d.rel, d.parent, d.index)
	case dropInto:
		a.placeEntry(d.rel, d.parent, len(a.childrenOf(d.parent)))
		a.state.ActiveSpace().SetExpanded(d.parent, true)
		a.saveState()
	}
}

// placeEntry moves the file or folder at rel of the active space to index
// among what the folder dir holds, into dir when it is elsewhere.
func (a *App) placeEntry(rel, dir string, index int) {
	sp := a.state.ActiveSpace()
	kids := a.childrenOf(dir)
	names := make([]string, len(kids))
	for i, n := range kids {
		names[i] = n.Name
	}
	if parentDir(rel) == dir {
		name := path.Base(rel)
		if old := slices.Index(names, name); old >= 0 && old < index {
			index--
		}
		sp.Place(dir, names, name, index)
		a.saveState()
		a.refreshTree()
		return
	}
	to, _, err := a.moveWithResources(sp, rel, sp, dir)
	if err != nil {
		a.notify("移动 %s 失败。%v", path.Base(rel), err)
		return
	}
	if dir != "" {
		sp.SetExpanded(dir, true)
	}
	a.moved(sp, rel, to)
	sp.Place(dir, names, path.Base(to), index)
	a.saveState()
	a.refreshTree()
}

// pinIn pins rel at index among the space's pins as shown, out of the
// favorites, as one item is pinned or a favorite, never both.
func (a *App) pinIn(sp *space.Space, rel string, index int) {
	shown := pinnedNodes(sp, a.tree)
	at := len(sp.Pins)
	if index < len(shown) {
		at = slices.Index(sp.Pins, shown[index].Path)
		if old := slices.Index(sp.Pins, rel); old >= 0 && old < at {
			at--
		}
	}
	if i := a.state.FavoriteIndex(sp.ID, rel); i >= 0 {
		a.state.ToggleFavorite(sp.ID, rel)
	}
	sp.Pin(rel, at)
	a.saveState()
}

// dropLine draws the line where the drag lands, above the list.
func (a *App) dropLine(c *ui.Context) {
	d := a.listDrop
	if d == nil || !d.line {
		return
	}
	t := c.Theme()
	ui.Overlay(c, func() {
		ui.Box(c.Key("drop-line")).Absolute().Left(d.x0).Top(d.y-1).Size(max(0, d.x1-d.x0), 2).Radius(1).
			Background(t.Text.Alpha(0.5)).
			Transition(ui.ElementTransition{Duration: 200 * time.Millisecond, Position: true})
	})
}

// dragCard is the fill of a row as it is dragged: a light card, as Cra's
// drag image puts the row on.
func dragCard(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGB(46, 46, 46).Alpha(0.92)
	}
	return white.Alpha(0.92)
}

// intoBadge is the pill a folder taking the drag shows at its right:
// 「移到文件夹」.
func intoBadge(c *ui.Context) {
	t := c.Theme()
	fill := white.Alpha(0.9)
	if t.Dark {
		fill = white.Alpha(0.14)
	}
	ui.Row(c.Key("into-badge")).Absolute().Right(8).Top(6).Height(22).PaddingX(8).Gap(4).Radius(11).
		AlignItems(ui.Center).Background(fill).Shadow(0, 1, 2, 0, black.Alpha(0.08)).Children(func() {
		ui.Icon(c, iconFolder).FontSize(12).TextColor(t.Text)
		ui.Text(c, "移到文件夹").FontSize(11).FontWeight(600).TextColor(t.Text).NoWrap()
	})
}
