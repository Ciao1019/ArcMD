package app

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// Dragging in the sidebar's list lifts the dragged row onto a floating card
// above the window while a dashed slot holds where it will land: sliding
// live as the pointer crosses other rows, across the divider to pin or unpin,
// or into a folder whose middle is held over (opening a closed folder after
// 0.6 s). Letting go glides the floating card into its slot as the outline
// fades away. The order is the space's to keep (Space.Order).

const (
	// springDelay is how long a closed folder is held over before it opens.
	springDelay = 600 * time.Millisecond
	// dragLiftDur is how quickly a picked-up row scales and shadows into its floating card.
	dragLiftDur = 120 * time.Millisecond
	// dragSettleDur is how long a released floating card takes to glide into its slot.
	dragSettleDur = 220 * time.Millisecond
)

// listEase is SwiftUI's snappy curve, shared by row transitions and card settle.
var listEase = ui.CubicBezier(0.2, 0.9, 0.25, 1)

// listAnim is how the list's rows and insertion slots move over 0.22 s.
var listAnim = ui.Animation{Duration: 220 * time.Millisecond, Ease: listEase}

// pageTree is a space's folders as read for the strip of spaces.
type pageTree struct {
	nodes []*vault.Node
	err   error
}

// readTree reads the space's folder in the order the space keeps. The
// space's attachment folder stays out, as vault.Tree already hides assets/:
// it holds pictures and whiteboard files, not documents to open.
func readTree(sp *space.Space) ([]*vault.Node, error) {
	if sp.IsExternal() {
		nodes := vault.Files(sp.ExternalFiles).Tree()
		arrangeNodes(sp, "", nodes)
		return nodes, nil
	}
	nodes, err := vault.Tree(sp.Root)
	if err == nil {
		nodes = dropTreeFolder(nodes, attachmentFolder(sp))
	}
	arrangeNodes(sp, "", nodes)
	return nodes, err
}

// dropTreeFolder hides the space's attachment folder from the tree, and the
// folders it emptied that lead only to it.
func dropTreeFolder(nodes []*vault.Node, folder string) []*vault.Node {
	if folder == "" || folder == "." {
		return nodes
	}
	var filter func(nodes []*vault.Node) []*vault.Node
	filter = func(nodes []*vault.Node) []*vault.Node {
		out := make([]*vault.Node, 0, len(nodes))
		for _, n := range nodes {
			if n.Path == folder {
				continue
			}
			if n.Dir {
				n.Children = filter(n.Children)
				if len(n.Children) == 0 && strings.HasPrefix(folder, n.Path+"/") {
					continue
				}
			}
			out = append(out, n)
		}
		return out
	}
	return filter(nodes)
}

func arrangeNodes(sp *space.Space, dir string, nodes []*vault.Node) {
	if len(nodes) == 0 {
		return
	}
	switch sp.SortOf(dir) {
	case "name":
		// Folders first, then documents, each by name as people read it.
		slices.SortStableFunc(nodes, func(x, y *vault.Node) int {
			if x.Dir != y.Dir {
				return b2i(y.Dir) - b2i(x.Dir)
			}
			return strings.Compare(strings.ToLower(x.Title()), strings.ToLower(y.Title()))
		})
	case "modified":
		slices.SortStableFunc(nodes, func(x, y *vault.Node) int { return y.Modified.Compare(x.Modified) })
	default:
		names := make([]string, len(nodes))
		byName := make(map[string]*vault.Node, len(nodes))
		for i, n := range nodes {
			names[i] = n.Name
			byName[n.Name] = n
		}
		for i, name := range sp.Arrange(dir, names) {
			nodes[i] = byName[name]
		}
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

// rowFrame is the resting box of a row or slot in the list from the frame before.
type rowFrame struct {
	rel, parent string
	index       int // among its parent's children
	depth       int
	dir, open   bool
	// placeholder is the pinned section's "拖到此处置顶" row.
	placeholder bool
	// slot is the dashed insertion slot replacing a dragged row, and
	// slotSelf whether it sits at the dragged row's own original place.
	slot, slotSelf bool
	r, target      ui.Rect
}

// The kinds of listDrop.
const (
	dropInsert = iota + 1
	dropInto
)

// listDrop is where a dragged file or folder would land: inserted at
// index in the folder parent, or moved into the folder parent.
type listDrop struct {
	rel    string
	kind   int
	parent string
	index  int
}

// slotState says where the dashed insertion slot sits in the active space's
// list during the current frame.
type slotState struct {
	active   bool
	parent   string
	index    int
	self     bool
	fromList bool
	emitted  bool
}

// dragGrab tracks a sidebar row actively lifted onto the floating drag card.
type dragGrab struct {
	active     bool
	consumed   bool
	space, rel string
	count      int
	dir        bool
	openFolder bool
	holdsOpen  bool
	dim        bool
	title      string
	grabX      float32
	grabY      float32
	cardW      float32
	x, y       float32
	started    time.Time
}

// dragSettle glides a released floating card from its release coordinates
// into its landed row's bounds over dragSettleDur.
type dragSettle struct {
	active     bool
	space, rel string
	count      int
	dir        bool
	openFolder bool
	holdsOpen  bool
	dim        bool
	title      string
	fromX      float32
	fromY      float32
	fromW      float32
	toRect     ui.Rect
	hasTarget  bool
	started    time.Time
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
// (px, py) in window DIPs would land: the top and bottom quarters of a
// folder's row insert before and after it (an open folder's bottom, at its
// top), its middle takes it; a document's row inserts before its top half
// and after its bottom one; resting inside an open insertion slot keeps that
// slot. Over no row it goes at the start or the end of the root. Nil changes
// nothing.
func (a *App) resolveDrop(rel string, px, py float32, rows []rowFrame) *listDrop {
	const half = 1
	sp := a.state.ActiveSpace()
	foreign := strings.HasPrefix(rel, "\x00")
	var group []string
	if !foreign && sp != nil {
		group = a.dragGroup(sp.ID, rel)
	}
	inDragged := func(candidate string) bool {
		if foreign {
			return under(candidate, rel)
		}
		for _, m := range group {
			if under(candidate, m) {
				return true
			}
		}
		return false
	}

	d := &listDrop{rel: rel}
	var hit *rowFrame
	for i := range rows {
		r := &rows[i]
		if py >= r.r.Y-half && py < r.r.Y+r.r.H+half {
			hit = r
			break
		}
	}
	if hit == nil {
		for i := range rows {
			r := &rows[i]
			if !r.slot {
				continue
			}
			if r.target.W > 0 && py >= r.target.Y-half && py < r.target.Y+r.target.H+half {
				hit = r
				break
			}
			dist := py - (r.r.Y + r.r.H/2)
			if dist < 0 {
				dist = -dist
			}
			if !r.slotSelf && dist <= rowHeight+6 {
				hit = r
				break
			}
		}
	}
	switch {
	case hit != nil && hit.slot:
		if hit.slotSelf {
			return nil
		}
		return &listDrop{rel: rel, kind: dropInsert, parent: hit.parent, index: hit.index}
	case hit != nil && hit.placeholder:
		d.kind, d.parent, d.index = dropInsert, pinRoot, 0
	case hit != nil:
		f := (py - hit.r.Y) / max(1, hit.r.H)
		before := func() { d.kind, d.parent, d.index = dropInsert, hit.parent, hit.index }
		after := func() { d.kind, d.parent, d.index = dropInsert, hit.parent, hit.index+1 }
		switch {
		case inDragged(hit.rel):
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
	if foreign {
		return d
	}
	if d.kind == dropInto {
		if inDragged(d.parent) {
			return nil
		}
		allThere := true
		for _, m := range group {
			if parentDir(m) != d.parent || sp.IsPinned(m) {
				allThere = false
				break
			}
		}
		if allThere {
			return nil // already there
		}
		return d
	}
	if inDragged(d.parent) {
		return nil
	}
	kids := a.childrenOf(d.parent)
	if len(group) <= 1 {
		pinned := sp.IsPinned(rel)
		if d.parent == pinRoot && pinned || d.parent != pinRoot && !pinned && parentDir(rel) == d.parent {
			old := slices.IndexFunc(kids, func(n *vault.Node) bool { return n.Path == rel })
			if d.index == old || d.index == old+1 {
				return nil // next to itself
			}
		}
		return d
	}
	sameRun := true
	firstOld := -1
	for i, m := range group {
		mPin := sp.IsPinned(m)
		if (d.parent == pinRoot) != mPin || (d.parent != pinRoot && parentDir(m) != d.parent) {
			sameRun = false
			break
		}
		idx := slices.IndexFunc(kids, func(n *vault.Node) bool { return n.Path == m })
		if idx < 0 || (i > 0 && idx != firstOld+i) {
			sameRun = false
			break
		}
		if i == 0 {
			firstOld = idx
		}
	}
	if sameRun && firstOld >= 0 && d.index >= firstOld && d.index <= firstOld+len(group) {
		return nil
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
// what letting go does. It returns the item currently dragged over e.
func (a *App) listDrag(c *ui.Context, e ui.Element, rows []rowFrame) sidebarDrag {
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
		return nil
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
		return over
	}
	if d, ok := dropped.(dragged); isDrop && ok && a.foreign(d) {
		drop := a.listDrop
		if drop == nil || drop.rel != foreignKey(d.rel) {
			x, y, _ := c.Root().PointerPosition()
			drop = a.resolveDrop(foreignKey(d.rel), x, y, rows)
		}
		a.listDrop, a.springOpen = nil, springOpen{}
		a.dropForeign(d, drop)
		return nil
	}
	if d, ok := dropped.(dragged); isDrop && ok {
		drop := a.listDrop
		if drop == nil || drop.rel != d.rel {
			x, y, _ := c.Root().PointerPosition()
			drop = a.resolveDrop(d.rel, x, y, rows)
		}
		a.listDrop, a.springOpen = nil, springOpen{}
		if drop != nil {
			a.applyGroupDrop(a.dragGroup(sp.ID, d.rel), drop)
		}
		return nil
	}
	d, ok := over.(dragged)
	if !isOver || !ok {
		a.listDrop, a.springOpen = nil, springOpen{}
		return nil
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
	return over
}

// computeSlot determines where the dashed insertion slot sits for sp during
// this frame, and counts how many pinned rows remain visible at rest.
func (a *App) computeSlot(sp *space.Space, pins []*vault.Node, over sidebarDrag) (slotState, int) {
	localRel := ""
	fromList := false
	if d, ok := over.(dragged); ok && d.space == sp.ID {
		localRel, fromList = d.rel, true
	} else if a.dragGrab.active && a.dragGrab.space == sp.ID {
		localRel = a.dragGrab.rel
	}
	restingPins := 0
	for _, n := range pins {
		if sp.PinsCollapsed && !a.holdsOpen(sp, n.Path) {
			continue
		}
		if n.Path == localRel {
			continue
		}
		restingPins++
	}
	if !a.listDragging || over == nil {
		return slotState{}, restingPins
	}
	d := a.listDrop
	switch {
	case d != nil && d.kind == dropInsert:
		if d.parent == pinRoot && restingPins == 0 {
			// The empty pinned section's own dashed placeholder takes this drop.
			return slotState{}, restingPins
		}
		return slotState{active: true, parent: d.parent, index: d.index, fromList: fromList}, restingPins
	case d != nil && d.kind == 0 && d.rel != "":
		p := parentDir(d.rel)
		idx := slices.IndexFunc(a.childrenOf(p), func(n *vault.Node) bool { return n.Path == d.rel })
		if idx >= 0 {
			return slotState{active: true, parent: p, index: idx}, restingPins
		}
	case d == nil && fromList:
		if sp.IsPinned(localRel) {
			idx := slices.IndexFunc(pins, func(n *vault.Node) bool { return n.Path == localRel })
			if idx >= 0 && (!sp.PinsCollapsed || a.holdsOpen(sp, localRel)) {
				return slotState{active: true, parent: pinRoot, index: idx, self: true, fromList: true}, restingPins
			}
			return slotState{}, restingPins
		}
		p := parentDir(localRel)
		idx := slices.IndexFunc(a.childrenOf(p), func(n *vault.Node) bool { return n.Path == localRel })
		if idx >= 0 {
			return slotState{active: true, parent: p, index: idx, self: true, fromList: true}, restingPins
		}
	}
	return slotState{}, restingPins
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
	if strings.HasPrefix(d.rel, "\x00") {
		if lands == sectionPinned {
			return sectionPinned
		}
		return 0
	}
	sp := a.state.ActiveSpace()
	for _, m := range a.dragGroup(sp.ID, d.rel) {
		from := sectionToday
		if a.pinnedSection(m) {
			from = sectionPinned
		}
		if lands != from {
			return lands
		}
	}
	return 0
}

// The sections of the list.
const (
	sectionPinned = iota + 1
	sectionToday
)

// applyGroupDrop moves every item in group where the drop says, delegating
// single-item drops to applyDrop.
func (a *App) applyGroupDrop(group []string, d *listDrop) {
	if len(group) <= 1 {
		a.applyDrop(d)
		return
	}
	sp := a.state.ActiveSpace()
	if sp == nil || d == nil || d.kind == 0 {
		return
	}
	if d.parent == pinRoot {
		a.pinGroup(sp, group, d.index)
		return
	}
	switch d.kind {
	case dropInsert:
		a.placeEntries(group, d.parent, d.index)
	case dropInto:
		a.placeEntries(group, d.parent, len(a.childrenOf(d.parent)))
		sp.SetExpanded(d.parent, true)
		a.saveState()
	}
}

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
	from := parentDir(rel)
	back := slices.IndexFunc(a.childrenOf(from), func(n *vault.Node) bool { return n.Path == rel })
	to, _, err := a.moveWithResources(sp, rel, sp, dir)
	if err != nil {
		a.notify("移动 %s 失败。%v", path.Base(rel), err)
		return
	}
	defer a.offerUndo(i18n.T("已移到「")+folderName(sp, dir)+"」", path.Base(to), func() {
		if a.state.ActiveSpace() != sp {
			a.notify("「%s」已不在当前空间，无法撤销移动。", path.Base(to))
			return
		}
		a.placeEntry(to, from, max(0, back))
	})
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

// dragOverlayView draws the lifted floating card while a sidebar row is
// dragged, and glides it smoothly into its target slot on release.
func (a *App) dragOverlayView(c *ui.Context) {
	if a.carrying == nil && a.dragGrab.active {
		a.dragGrab = dragGrab{}
	}
	now := c.Now()
	switch {
	case a.carrying != nil && a.dragGrab.active:
		g := &a.dragGrab
		lift := float32(1)
		if !a.reduceMotion {
			elapsed := now.Sub(g.started)
			if elapsed < dragLiftDur {
				lift = ui.EaseOut(min(max(float32(elapsed)/float32(dragLiftDur), 0), 1))
				c.AnimationFrame()
			}
		}
		into := a.listDrop != nil && a.listDrop.kind == dropInto
		ui.Overlay(c, func() {
			a.floatingDragCard(c, g.x, g.y, g.cardW, lift, into, g.dir, g.openFolder, g.holdsOpen, g.dim, g.rel, g.title, max(1, g.count), true)
		})
	case a.dragSettle.active:
		s := &a.dragSettle
		elapsed := now.Sub(s.started)
		if a.reduceMotion || !s.hasTarget || elapsed >= dragSettleDur {
			a.dragSettle = dragSettle{}
			return
		}
		c.AnimationFrame()
		k := listEase(min(max(float32(elapsed)/float32(dragSettleDur), 0), 1))
		x := s.fromX + (s.toRect.X-s.fromX)*k
		y := s.fromY + (s.toRect.Y-s.fromY)*k
		w := s.fromW + (s.toRect.W-s.fromW)*k
		lift := 1 - k
		ui.Overlay(c, func() {
			a.floatingDragCard(c, x, y, w, lift, false, s.dir, s.openFolder, s.holdsOpen, s.dim, s.rel, s.title, max(1, s.count), false)
		})
	}
}

// floatingDragCard draws a lifted row card at (x, y) above the window; when
// count > 1, stacked companion cards peek behind it and a count pill shows
// how many items move together.
func (a *App) floatingDragCard(c *ui.Context, x, y, w, lift float32, intoFolder, dir, openFolder, holdsOpen, dim bool, rel, title string, count int, label bool) {
	t := c.Theme()
	look := dragCardColors(t, a.look)
	gap := float32(10)
	if dir {
		gap = 11
	}
	card := ui.Row(c.Key("drag-card")).Absolute().Left(x).Top(y).Size(max(48, w), rowHeight).
		PaddingStart(10).PaddingEnd(8).Gap(gap).AlignItems(ui.Center).IgnoreInput(true).Background(ui.Transparent)
	if intoFolder {
		card.Opacity(0.68)
	}
	if label {
		card.Label(title)
	}
	card.Draw(func(p *ui.Painter, r ui.Rect) {
		grow := 1.5 * lift
		fr := ui.Rect{X: r.X - grow, Y: r.Y + 2 - grow*0.6, W: r.W + 2*grow, H: r.H - 4 + 1.2*grow}
		if count > 1 && lift > 0.01 {
			layers := min(count-1, 2)
			for layer := layers; layer >= 1; layer-- {
				lf := float32(layer) * lift
				sr := ui.Rect{
					X: fr.X + 3*lf,
					Y: fr.Y + 4*lf,
					W: max(32, fr.W-6*lf),
					H: fr.H,
				}
				p.Shadow(sr, 10, 0, 1+3*lift, 2+8*lift, 0, scaleAlpha(look.shadow, 0.45*lift))
				p.Fill(sr, scaleAlpha(look.fill, 0.92), 10)
				p.Stroke(sr, scaleAlpha(look.rim, 0.85*lift), 10, 1)
			}
		}
		p.Shadow(fr, 10, 0, 1+5*lift, 3+15*lift, 0, scaleAlpha(look.shadow, 0.2+0.8*lift))
		p.Fill(fr, look.fill, 10)
		if lift > 0.01 {
			p.Stroke(fr, scaleAlpha(look.rim, lift), 10, 1)
		}
	})
	card.Children(func() {
		if dir {
			folderGlyph(c, a.look, openFolder, holdsOpen)
		} else if icon := fileIcon(c, rel, title, 16); dim {
			icon.Opacity(closedIconAlpha)
		}
		ink := t.Text
		if dim {
			ink = closedInk(t)
		}
		name := ui.Text(c, docName(title)).FontSize(14).TextColor(ink).SingleLine().Shrink(1).MinWidth(0)
		if dir {
			name.FontWeight(500)
		}
		if count > 1 {
			ui.Box(c.Key("drag-count")).Height(19).MinWidth(19).PaddingX(6).Radius(9.5).Shrink(0).
				Center().Background(look.badgeFill).Children(func() {
				ui.Text(c, fmt.Sprint(count)).FontSize(11).FontWeight(600).TextColor(look.badgeText).NoWrap()
			})
		}
	})
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
		ui.Text(c, i18n.T("移到文件夹")).FontSize(11).FontWeight(600).TextColor(t.Text).NoWrap()
	})
}
