// Adapted from mygo-native-ui/ui/mindmap (MIT); see third_party/mygo-native-ui.
// Package mindmap lays out a native two-sided map of a document outline.
package mindmap

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// Item uses an application-owned ID so insertions do not shift the selection
// or fold state. Rich nodes supply a natural size and a painter.
type Item struct {
	Image         bool
	Panel         bool // a framed rich node takes its incoming curve at the side, without an underline
	CodeBlock     bool // keep the native code surface distinct from ordinary branch labels
	ID, Text      string
	Depth         int
	Width, Height float32
	Draw          func(*ui.Painter, ui.Rect, float32)
}

type cachedLabel struct {
	text string
	fs   float32
	wt   int
	maxW float32
	w, h float32
}

type State struct {
	layoutRevision  uint64
	layoutBothSides bool
	nodes           []node
	nodeIndex       map[string]int
	labelCache      map[string]cachedLabel
	ox, oy          float32
	dragID          string
	Target          ui.Rect // selected node in unscaled map coordinates
	closed          map[string]bool
	Width, Height   float32 // natural size, independent of zoom
}

type Options struct {
	LayoutRevision uint64     // nonzero while the caller can identify changes to content, theme and wrapping
	Colors         []ui.Color // active ArcMD space colors; the document theme supplies readable ink
	MaxWidth       float32    // width ceiling in logical DIP, before zoom
	Pan            func(float32, float32)
	Locate         bool
	BothSides      bool
	Zoom           float32
	Selected       string
	FocusID        string // optional node ID to focus once on this frame
	Reveal         bool
	Chosen         func(string)
	Navigate       func(string) // arrow-key spatial navigation without stealing map focus
	// Visible is the viewport in scaled diagram coordinates. The selected
	// node still exists offscreen when it must be revealed.
	Visible                ui.Rect
	Prepare                func(index int) bool // measure a newly visible rich node
	FoldLabel, UnfoldLabel string
	NodeMenu               func(*ui.Menu, string)
}

type node struct {
	kids   []int
	parent int
	// w and h are the item's size, tall the height of the tree under it.
	w, h, tall                 float32
	x, y                       float32
	subX0, subY0, subX1, subY1 float32
	descendants                int
	leaves                     int
	left                       bool
	branch                     int
	shown                      bool
}

func labelFont(t *ui.Theme, i int, items []Item) (float32, int) {
	if i == len(items) {
		return t.FontSize + 3, 650
	}
	if items[i].Depth == 0 {
		return t.FontSize, 600
	}
	return t.FontSize, 400
}

// HasFolded reports whether any current branch with children is folded.
func (s *State) HasFolded() bool {
	if len(s.closed) == 0 {
		return false
	}
	n := len(s.nodes) - 1
	for id := range s.closed {
		if idx, ok := s.nodeIndex[id]; ok && idx >= 0 && idx < n && len(s.nodes[idx].kids) > 0 {
			return true
		}
	}
	return false
}

// HasFoldable reports whether the current map contains any collapsible branch.
func (s *State) HasFoldable() bool {
	n := len(s.nodes) - 1
	for i := 0; i < n; i++ {
		if len(s.nodes[i].kids) > 0 {
			return true
		}
	}
	return false
}

// UnfoldAll opens every folded branch in the map.
func (s *State) UnfoldAll() {
	if len(s.closed) == 0 {
		return
	}
	clear(s.closed)
	s.layoutRevision = 0
}

// HasChildren reports whether the node with id currently has child nodes.
func (s *State) HasChildren(id string) bool {
	idx, ok := s.nodeIndex[id]
	return ok && idx >= 0 && idx < len(s.nodes) && len(s.nodes[idx].kids) > 0
}

// IsFolded reports whether the node with id is currently folded.
func (s *State) IsFolded(id string) bool {
	return id != "" && s.closed[id]
}

// SetFolded folds or unfolds a single node by id.
func (s *State) SetFolded(id string, folded bool) {
	if id == "" {
		return
	}
	if folded {
		if s.closed == nil {
			s.closed = map[string]bool{}
		}
		if s.closed[id] {
			return
		}
		s.closed[id] = true
	} else {
		if !s.closed[id] {
			return
		}
		delete(s.closed, id)
	}
	s.layoutRevision = 0
}

// HasFoldableDescendant reports whether any descendant of id has children.
func (s *State) HasFoldableDescendant(id string) bool {
	idx, ok := s.nodeIndex[id]
	if !ok || idx < 0 || idx >= len(s.nodes) {
		return false
	}
	var walk func(i int) bool
	walk = func(i int) bool {
		for _, k := range s.nodes[i].kids {
			if len(s.nodes[k].kids) > 0 || walk(k) {
				return true
			}
		}
		return false
	}
	return walk(idx)
}

// HasFoldedDescendant reports whether any descendant of id is currently folded.
func (s *State) HasFoldedDescendant(items []Item, id string) bool {
	if len(s.closed) == 0 {
		return false
	}
	idx, ok := s.nodeIndex[id]
	if !ok || idx < 0 || idx >= len(s.nodes) {
		return false
	}
	n := len(items)
	var walk func(i int) bool
	walk = func(i int) bool {
		for _, k := range s.nodes[i].kids {
			if k < n && len(s.nodes[k].kids) > 0 && s.closed[items[k].ID] {
				return true
			}
			if walk(k) {
				return true
			}
		}
		return false
	}
	return walk(idx)
}

// FoldSubtree folds or unfolds all collapsible descendants of id. When
// unfolding, id itself is also unfolded so its subtree becomes visible.
func (s *State) FoldSubtree(items []Item, id string, folded bool) {
	idx, ok := s.nodeIndex[id]
	if !ok || idx < 0 || idx >= len(s.nodes) {
		return
	}
	if folded && s.closed == nil {
		s.closed = map[string]bool{}
	}
	n := len(items)
	if !folded && idx < n {
		delete(s.closed, items[idx].ID)
	}
	var walk func(i int)
	walk = func(i int) {
		for _, k := range s.nodes[i].kids {
			if k < n && len(s.nodes[k].kids) > 0 {
				if folded {
					s.closed[items[k].ID] = true
				} else {
					delete(s.closed, items[k].ID)
				}
			}
			walk(k)
		}
	}
	walk(idx)
	s.layoutRevision = 0
}

// ShowDepth expands the map up to maxDepth levels (1 shows top-level branches
// folded, 2 shows two levels, etc.; maxDepth <= 0 unfolds everything).
func (s *State) ShowDepth(items []Item, maxDepth int) {
	if maxDepth <= 0 {
		s.UnfoldAll()
		return
	}
	if s.closed == nil {
		s.closed = map[string]bool{}
	}
	clear(s.closed)
	s.CollapseBranches(items, maxDepth-1, "")
}

// CollapseBranches folds nodes at Depth >= minDepth that have children, while
// keeping ancestors of keepAncestorOf open so the active selection stays visible.
func (s *State) CollapseBranches(items []Item, minDepth int, keepAncestorOf string) {
	if s.closed == nil {
		s.closed = map[string]bool{}
	}
	n := len(items)
	if n == 0 {
		return
	}
	parent := make([]int, n)
	hasKids := make([]bool, n)
	var stack []int
	for i, it := range items {
		for len(stack) > 0 && items[stack[len(stack)-1]].Depth >= it.Depth {
			stack = stack[:len(stack)-1]
		}
		p := -1
		if len(stack) > 0 {
			p = stack[len(stack)-1]
			hasKids[p] = true
		}
		parent[i] = p
		stack = append(stack, i)
	}
	for i, it := range items {
		if hasKids[i] && it.Depth >= minDepth {
			s.closed[it.ID] = true
		}
	}
	if keepAncestorOf != "" {
		for i, it := range items {
			if it.ID == keepAncestorOf {
				for p := parent[i]; p >= 0; p = parent[p] {
					delete(s.closed, items[p].ID)
				}
				break
			}
		}
	}
	s.layoutRevision = 0
}

func (s *State) hasClosedAncestor(items []Item, id string) bool {
	if len(s.closed) == 0 || id == "" {
		return false
	}
	idx, ok := s.nodeIndex[id]
	if !ok || idx < 0 || idx >= len(s.nodes) {
		return false
	}
	n := len(items)
	for p := s.nodes[idx].parent; p >= 0 && p < len(s.nodes); p = s.nodes[p].parent {
		if p < n && s.closed[items[p].ID] {
			return true
		}
	}
	return false
}

func (s *State) layout(c *ui.Context, rootItem Item, items []Item, o Options) {
	t := c.Theme()
	if s.closed == nil {
		s.closed = map[string]bool{}
	}
	if s.labelCache == nil {
		s.labelCache = make(map[string]cachedLabel, len(items)+1)
	}
	gapX, gapY, pad := t.Space(5.5), t.Space(.75), t.Space(2)
	mainGapX, mainGapY := t.Space(8), t.Space(1.5)
	title := rootItem.Text
	padX, padY := t.Space(.75), t.Space(.5)
	// The title is node n, after the items.
	n := len(items)
	nodes := make([]node, n+1)
	nodes[n].parent = -1
	nodeIndex := make(map[string]int, n+1)
	nodeIndex[rootItem.ID] = n
	var stack []int
	for i, it := range items {
		for len(stack) > 0 && items[stack[len(stack)-1]].Depth >= it.Depth {
			stack = stack[:len(stack)-1]
		}
		parent := n
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		nodes[i].parent = parent
		nodes[parent].kids = append(nodes[parent].kids, i)
		nodeIndex[it.ID] = i
		if o.Reveal && it.ID == o.Selected {
			for _, ancestor := range stack {
				delete(s.closed, items[ancestor].ID)
			}
		}
		stack = append(stack, i)
	}
	for i := range nodes {
		text := title
		if i < n {
			text = items[i].Text
		}
		fs, wt := labelFont(t, i, items)
		item := rootItem
		if i < n {
			item = items[i]
		}
		w, h := item.Width, item.Height
		if item.Draw == nil {
			if cl, ok := s.labelCache[item.ID]; ok && cl.text == text && cl.fs == fs && cl.wt == wt && cl.maxW == o.MaxWidth {
				w, h = cl.w, cl.h
			} else {
				span := ui.Span{Text: text, Size: fs, Weight: wt}
				w, h = c.MeasureText(0, span)
				if o.MaxWidth > 0 && w > o.MaxWidth {
					w, h = c.MeasureText(o.MaxWidth, span)
					w = min(w, o.MaxWidth)
				}
				s.labelCache[item.ID] = cachedLabel{text: text, fs: fs, wt: wt, maxW: o.MaxWidth, w: w, h: h}
			}
		}
		px, py := padX, padY
		if i == n {
			px, py = t.Space(3.75), t.Space(2)
		} else if item.Image {
			px, py = 0, 0
		} else if items[i].Depth == 0 && !item.Panel {
			px, py = t.Space(2.5), t.Space(1.25)
		}
		nodes[i].w, nodes[i].h = max(w, t.Space(4))+2*px, h+2*py
	}
	if len(s.labelCache) > len(nodes)*2+64 {
		for id := range s.labelCache {
			if _, ok := nodeIndex[id]; !ok {
				delete(s.labelCache, id)
			}
		}
	}

	// Compute height, descendant count, and leaf weight in one bottom-up pass.
	var measure func(i int)
	measure = func(i int) {
		nd := &nodes[i]
		nd.tall = nd.h
		if i < n && items[i].CodeBlock {
			nd.tall += 2 * t.Space(2)
		}
		closed := i < n && s.closed[items[i].ID]
		var sum float32
		for k, j := range nd.kids {
			measure(j)
			nd.descendants += 1 + nodes[j].descendants
			nd.leaves += nodes[j].leaves
			if !closed {
				if k > 0 {
					sum += gapY
				}
				sum += nodes[j].tall
			}
		}
		nd.leaves = max(1, nd.leaves)
		if !closed {
			nd.tall = max(nd.tall, sum)
		}
	}

	root := &nodes[n]
	total := 0
	for k, j := range root.kids {
		measure(j)
		nodes[j].branch = k
		total += nodes[j].leaves
	}
	var right, left []int
	acc := 0
	for k, j := range root.kids {
		w := nodes[j].leaves
		if !o.BothSides || (len(left) == 0 && (k == 0 || 2*acc+w <= total)) {
			right = append(right, j)
			acc += w
		} else {
			left = append(left, j)
		}
	}

	// place puts node i with its middle at y, its side toward the title
	// at x, and the tree under it beyond, accumulating subtree bounds.
	var place func(i int, x, y float32, left bool, branch int)
	place = func(i int, x, y float32, left bool, branch int) {
		nd := &nodes[i]
		nd.left, nd.branch, nd.shown = left, branch, true
		nd.y = y - nd.h/2
		nd.x = x
		if left {
			nd.x = x - nd.w
		}
		nd.subX0, nd.subY0 = nd.x-4, nd.y-4
		nd.subX1, nd.subY1 = nd.x+nd.w+4, nd.y+nd.h+4
		if len(nd.kids) > 0 {
			if left {
				nd.subX0 -= 28
			} else {
				nd.subX1 += 28
			}
		}
		if s.closed[items[i].ID] {
			return
		}
		next := x + nd.w + gapX
		if left {
			next = x - nd.w - gapX
		}
		stackKids(nodes, nd.kids, next, y, gapY, left, branch, place)
		for _, j := range nd.kids {
			kid := &nodes[j]
			nd.subX0 = min(nd.subX0, kid.subX0)
			nd.subY0 = min(nd.subY0, kid.subY0)
			nd.subX1 = max(nd.subX1, kid.subX1)
			nd.subY1 = max(nd.subY1, kid.subY1)
		}
	}
	root.shown = true
	root.x, root.y = -root.w/2, -root.h/2
	root.subX0, root.subY0 = root.x-4, root.y-4
	root.subX1, root.subY1 = root.x+root.w+4, root.y+root.h+4
	stackKids(nodes, right, root.w/2+mainGapX, 0, mainGapY, false, -1, place)
	stackKids(nodes, left, -root.w/2-mainGapX, 0, mainGapY, true, -1, place)
	for _, j := range root.kids {
		kid := &nodes[j]
		root.subX0 = min(root.subX0, kid.subX0)
		root.subY0 = min(root.subY0, kid.subY0)
		root.subX1 = max(root.subX1, kid.subX1)
		root.subY1 = max(root.subY1, kid.subY1)
	}

	minX, minY, maxX, maxY := root.subX0+4, root.subY0+4, root.subX1-4, root.subY1-4
	if minX > root.x {
		minX = root.x
	}
	if minY > root.y {
		minY = root.y
	}
	if maxX < root.x+root.w {
		maxX = root.x + root.w
	}
	if maxY < root.y+root.h {
		maxY = root.y + root.h
	}
	ox, oy := pad-minX, pad-minY
	s.Width, s.Height = maxX-minX+2*pad, maxY-minY+2*pad
	s.nodes, s.nodeIndex, s.ox, s.oy = nodes, nodeIndex, ox, oy
	s.layoutRevision, s.layoutBothSides = o.LayoutRevision, o.BothSides
}

func (s *State) hitTest(items []Item, x, y, slack float32) string {
	nodes := s.nodes
	n := len(items)
	if len(nodes) != n+1 {
		return ""
	}
	var walk func(i int) string
	walk = func(i int) string {
		nd := &nodes[i]
		if !nd.shown {
			return ""
		}
		if x < nd.subX0-slack || x > nd.subX1+slack || y < nd.subY0-slack || y > nd.subY1+slack {
			return ""
		}
		if i < n && x >= nd.x-slack && x <= nd.x+nd.w+slack && y >= nd.y-slack && y <= nd.y+nd.h+slack {
			return items[i].ID
		}
		if i < n && s.closed[items[i].ID] {
			return ""
		}
		for _, j := range nd.kids {
			if hit := walk(j); hit != "" {
				return hit
			}
		}
		return ""
	}
	return walk(n)
}

func navVerticalTarget(nodes []node, items []Item, i, dir int) string {
	n := len(items)
	if i == n {
		kids := nodes[n].kids
		if len(kids) == 0 {
			return ""
		}
		if dir < 0 {
			return items[kids[len(kids)-1]].ID
		}
		return items[kids[0]].ID
	}
	nd := &nodes[i]
	if nd.parent >= 0 && nd.parent < len(nodes) {
		kids := nodes[nd.parent].kids
		pos := -1
		var sameSide []int
		for _, k := range kids {
			if nodes[k].shown && nodes[k].left == nd.left {
				if k == i {
					pos = len(sameSide)
				}
				sameSide = append(sameSide, k)
			}
		}
		if pos >= 0 {
			next := pos + dir
			if next >= 0 && next < len(sameSide) {
				return items[sameSide[next]].ID
			}
		}
	}
	best := -1
	var bestY float32
	for k := 0; k < n; k++ {
		if k == i || !nodes[k].shown || nodes[k].left != nd.left {
			continue
		}
		ky := nodes[k].y + nodes[k].h/2
		curY := nd.y + nd.h/2
		if dir < 0 && ky < curY-1 {
			if best < 0 || ky > bestY {
				best, bestY = k, ky
			}
		} else if dir > 0 && ky > curY+1 {
			if best < 0 || ky < bestY {
				best, bestY = k, ky
			}
		}
	}
	if best >= 0 {
		return items[best].ID
	}
	return ""
}

// View reuses unscaled geometry while panning and zooming.
func View(c *ui.Context, s *State, rootItem Item, items []Item, o Options) ui.Element {
	if o.LayoutRevision == 0 || o.LayoutRevision != s.layoutRevision || s.layoutBothSides != o.BothSides || len(s.nodes) != len(items)+1 || (o.Reveal && s.hasClosedAncestor(items, o.Selected)) {
		s.layout(c, rootItem, items, o)
	}
	t := c.Theme()
	nodes, ox, oy := s.nodes, s.ox, s.oy
	n, title := len(items), rootItem.Text
	z := o.Zoom
	if z <= 0 {
		z = 1
	}
	if o.Selected != "" {
		if idx, ok := s.nodeIndex[o.Selected]; ok && idx >= 0 && idx < len(nodes) && nodes[idx].shown {
			nd := nodes[idx]
			s.Target = ui.Rect{X: ox + nd.x, Y: oy + nd.y, W: nd.w, H: nd.h}
		}
	}
	visible := func(x0, y0, x1, y1 float32) bool {
		v := o.Visible
		return v.W <= 0 || v.H <= 0 || ((min(x0, x1)+ox)*z <= v.X+v.W+36 && (max(x0, x1)+ox)*z >= v.X-36 && (min(y0, y1)+oy)*z <= v.Y+v.H+36 && (max(y0, y1)+oy)*z >= v.Y-36)
	}
	box := ui.Box(c).Size(s.Width, s.Height).TransformOrigin(0, 0).Scale(z, z).Shrink(0)
	if o.Chosen != nil && z < 0.32 && box.Clicked() {
		if lx, ly, ok := box.PointerPosition(); ok {
			if hit := s.hitTest(items, lx-ox, ly-oy, 4/max(.1, z)); hit != "" {
				o.Chosen(hit)
			}
		}
	}

	box.Draw(func(p *ui.Painter, r ui.Rect) {
		var drawBranch func(i int)
		drawBranch = func(i int) {
			nd := nodes[i]
			if !nd.shown || !visible(nd.subX0, nd.subY0, nd.subX1, nd.subY1) {
				return
			}
			item := rootItem
			if i < n {
				item = items[i]
			}
			selected := o.Selected != "" && item.ID == o.Selected
			lod := z < 0.32 && nd.h*z < 9 && !selected && i < n && item.Depth > 0 && !item.Image
			if lod {
				if visible(nd.x, nd.y, nd.x+nd.w, nd.y+nd.h) {
					ink := branchInk(t, o.Colors, nd.branch).Alpha(.55)
					if item.CodeBlock {
						ink = t.Text.Alpha(.28)
					}
					p.Fill(ui.Rect{X: r.X + ox + nd.x, Y: r.Y + oy + nd.y, W: nd.w, H: nd.h}, ink, min(nd.h/2, 4))
				}
			} else if i < n && item.Depth > 0 && !item.Panel && visible(nd.x, nd.y+nd.h, nd.x+nd.w, nd.y+nd.h) {
				x, y := r.X+ox+nd.x, r.Y+oy+nd.y+nd.h
				p.Line(x, y, x+nd.w, y, .85, branchInk(t, o.Colors, nd.branch).Alpha(.42))
			}
			if i < n && s.closed[item.ID] {
				return
			}
			for _, j := range nd.kids {
				kid := nodes[j]
				x0, x1 := nd.x+nd.w, kid.x
				if kid.left {
					x0, x1 = nd.x, kid.x+kid.w
				}
				y0, y1 := nd.y+nd.h/2, kid.y+kid.h/2
				if i < n && item.Depth > 0 && !item.Panel {
					y0 = nd.y + nd.h
				}
				if items[j].Depth > 0 && !items[j].Panel {
					y1 = kid.y + kid.h
				}
				if visible(x0, y0, x1, y1) {
					px0, py0, px1, py1 := r.X+ox+x0, r.Y+oy+y0, r.X+ox+x1, r.Y+oy+y1
					dx := (px1 - px0) * .58
					var rise float32
					if py0-py1 < 2 && py1-py0 < 2 {
						rise = t.Space(.8)
					}
					// Opaque polylines use MyGo's GPU segment renderer while
					// retaining the cubic shape and its color on the canvas.
					var curve [17][2]float32
					for k := range curve {
						u := float32(k) / float32(len(curve)-1)
						v := 1 - u
						blend := u * u * (3 - 2*u)
						curve[k] = [2]float32{px0 + (px1-px0)*blend + 3*u*v*(v-u)*dx, py0 + (py1-py0)*blend - 3*u*v*rise}
					}
					width := float32(1.75)
					if i < n {
						width = 1.15
					}
					p.StrokePolyline(curve[:], width, t.Background.Mix(branchInk(t, o.Colors, kid.branch), .72))
				}
				drawBranch(j)
			}
		}
		drawBranch(n)
	})

	var revealPath map[int]bool
	if (o.Reveal || o.Locate) && o.Selected != "" {
		if idx, ok := s.nodeIndex[o.Selected]; ok {
			revealPath = map[int]bool{}
			for p := idx; p >= 0 && p < len(nodes); p = nodes[p].parent {
				revealPath[p] = true
			}
		}
	}

	box.Children(func() {
		var buildBranch func(i int)
		buildBranch = func(i int) {
			nd := nodes[i]
			if !nd.shown {
				return
			}
			onPath := revealPath[i]
			if !onPath && !visible(nd.subX0, nd.subY0, nd.subX1, nd.subY1) {
				return
			}
			text := title
			item := rootItem
			if i < n {
				item = items[i]
				text = item.Text
			}
			selected := o.Selected != "" && item.ID == o.Selected
			inView := visible(nd.x, nd.y, nd.x+nd.w, nd.y+nd.h) || ((o.Reveal || o.Locate) && selected)
			lod := z < 0.32 && nd.h*z < 9 && !selected && i < n && item.Depth > 0 && !item.Image
			if inView && !lod {
				fs, wt := labelFont(t, i, items)
				if i < n && item.Draw != nil && o.Prepare != nil && o.Prepare(i) {
					s.layoutRevision = 0
					c.After(0)
				}
				el := ui.ButtonBase(c.Key(item.ID)).Absolute().Left(ox+nd.x).Top(oy+nd.y).Size(nd.w, nd.h).
					Justify(ui.Center).AlignItems(ui.Center).Role(ui.RoleButton).Selected(selected).Label(text)
				if o.FocusID != "" && o.FocusID == item.ID {
					el.Focus(ui.FocusOptions{PreventScroll: true})
				}
				if o.Pan != nil {
					if dx, dy, held := el.Dragged(); held && (dx != 0 || dy != 0) {
						s.dragID = item.ID
						o.Pan(dx*z, dy*z)
						c.After(0)
						el.Cursor(ui.CursorGrabbing)
					}
				}
				color := t.Text
				switch {
				case i == n:
					if item.Draw != nil {
						el.Background(t.Surface).Border(1, branchInk(t, o.Colors, 0).Alpha(.55)).Radius(min(float32(12), nd.h/2))
					} else {
						el.Background(t.Text.Mix(branchInk(t, o.Colors, 0), .50)).Radius(min(float32(18), nd.h/2)).
							Shadow(0, 2, 6, 0, branchInk(t, o.Colors, 0).Alpha(.18))
						color = t.Background
					}
				case item.Image:
					el.Background(t.Background.Alpha(0))
				case item.CodeBlock:
					el.Background(t.Background.Mix(t.Text, .045).Mix(branchInk(t, o.Colors, nd.branch), .025)).Border(1, t.Border).Radius(6)
				case item.Panel:
					el.Background(t.Background.Alpha(0)).Radius(6)
					if el.Hovered() {
						el.Border(1, t.Border.Alpha(.75))
					}
				case item.Depth == 0:
					el.Background(branchInk(t, o.Colors, nd.branch).Alpha(.09)).Border(1, branchInk(t, o.Colors, nd.branch).Alpha(.27)).Radius(t.Space(1.5))
				default:
					el.Background(t.Background.Alpha(0)).Radius(t.Space(1))
					if el.Hovered() {
						el.Background(t.SurfaceHover)
					}
				}
				if selected {
					if item.CodeBlock {
						el.Border(1.25, branchInk(t, o.Colors, nd.branch).Alpha(.65))
					} else if item.Panel && !item.Image {
						el.Background(branchInk(t, o.Colors, nd.branch).Alpha(.06)).Border(1.25, branchInk(t, o.Colors, nd.branch).Alpha(.50))
					} else if i < n {
						el.Background(branchInk(t, o.Colors, nd.branch).Alpha(.12)).Border(1, branchInk(t, o.Colors, nd.branch).Alpha(.40))
					} else {
						el.Border(1.25, branchInk(t, o.Colors, 0).Alpha(.65))
					}
				}
				if el.Clicked() && s.dragID != item.ID && o.Chosen != nil {
					o.Chosen(item.ID)
				}
				if o.NodeMenu != nil {
					nodeID := item.ID
					el.ContextMenu(func(m *ui.Menu) {
						o.NodeMenu(m, nodeID)
					})
				}
				if s.dragID == item.ID && !el.Pressed() {
					s.dragID = ""
				}
				if i < n && len(nd.kids) > 0 && el.Shortcut(0, ui.KeySpace) {
					if s.closed[item.ID] {
						delete(s.closed, item.ID)
					} else {
						s.closed[item.ID] = true
					}
					s.layoutRevision = 0
					c.After(0)
				}
				if o.Navigate != nil {
					target := ""
					switch {
					case el.Shortcut(0, ui.KeyUp):
						target = navVerticalTarget(nodes, items, i, -1)
					case el.Shortcut(0, ui.KeyDown):
						target = navVerticalTarget(nodes, items, i, 1)
					case el.Shortcut(0, ui.KeyRight):
						if i == n {
							for _, j := range nd.kids {
								if !nodes[j].left {
									target = items[j].ID
									break
								}
							}
						} else if !nd.left {
							if len(nd.kids) > 0 {
								if s.closed[item.ID] {
									delete(s.closed, item.ID)
									s.layoutRevision = 0
								}
								target = items[nd.kids[0]].ID
							}
						} else if nd.parent == n {
							target = rootItem.ID
						} else if nd.parent >= 0 {
							target = items[nd.parent].ID
						}
					case el.Shortcut(0, ui.KeyLeft):
						if i == n {
							for _, j := range nd.kids {
								if nodes[j].left {
									target = items[j].ID
									break
								}
							}
						} else if nd.left {
							if len(nd.kids) > 0 {
								if s.closed[item.ID] {
									delete(s.closed, item.ID)
									s.layoutRevision = 0
								}
								target = items[nd.kids[0]].ID
							}
						} else if nd.parent == n {
							target = rootItem.ID
						} else if nd.parent >= 0 {
							target = items[nd.parent].ID
						}
					}
					if target != "" {
						o.Navigate(target)
						c.After(0)
					}
				}
				if item.Draw != nil {
					el.Clip().Draw(func(p *ui.Painter, r ui.Rect) {
						item.Draw(p, ui.Rect{X: r.X + (r.W-item.Width)/2, Y: r.Y + (r.H-item.Height)/2, W: item.Width, H: item.Height}, 1)
					})
				} else {
					el.Children(func() {
						label := ui.Text(c, text).FontSize(fs).FontWeight(wt).TextColor(color)
						if o.MaxWidth > 0 {
							label.MaxWidth(o.MaxWidth)
						} else {
							label.SingleLine()
						}
					})
				}
				if i < n && len(nd.kids) > 0 {
					count := 0
					if s.closed[item.ID] {
						count = nd.descendants
					}
					fold(c.Key("fold:"+item.ID), s, item.ID, nd, ox, oy, item.Depth == 0 || item.Panel, count, o)
				}
			}
			if i < n && s.closed[item.ID] {
				return
			}
			for _, j := range nd.kids {
				buildBranch(j)
			}
		}
		buildBranch(n)
	})
	return box
}

// stackKids places kids one under another, the middle of them all at y.
func stackKids(nodes []node, kids []int, x, y, gapY float32, left bool, branch int, place func(int, float32, float32, bool, int)) {
	var total float32
	for k, j := range kids {
		if k > 0 {
			total += gapY
		}
		total += nodes[j].tall
	}
	top := y - total/2
	for _, j := range kids {
		b := branch
		if b < 0 {
			b = nodes[j].branch
		}
		place(j, x, top+nodes[j].tall/2, left, b)
		top += nodes[j].tall + gapY
	}
}

// fold shows the dot at the outer end of item i that folds the count
// items under it, with the count while they are folded.
func fold(c *ui.Context, s *State, id string, nd node, ox, oy float32, branch bool, count int, o Options) {
	t := c.Theme()
	closed := s.closed[id]
	dotD := t.Space(2.25)
	pillW, pillH := dotD, dotD
	if closed {
		pillH = max(float32(15), t.Space(3.5))
		switch {
		case count >= 100:
			pillW = 26
		case count >= 10:
			pillW = 20
		default:
			pillW = pillH
		}
	}
	hitW := max(float32(18), pillW+4)
	hitH := max(float32(18), pillH+4)
	cx := ox + nd.x + nd.w + t.Space(.5) + pillW/2
	if nd.left {
		cx = ox + nd.x - t.Space(.5) - pillW/2
	}
	cy := oy + nd.y + nd.h
	if branch {
		cy = oy + nd.y + nd.h/2
	}
	tip := o.FoldLabel
	if closed {
		tip = o.UnfoldLabel
	}
	b := ui.ButtonBase(c).Absolute().Left(cx-hitW/2).Top(cy-hitH/2).Size(hitW, hitH).
		Justify(ui.Center).AlignItems(ui.Center).Label(tip).Tooltip(tip).Cursor(ui.CursorPointer)
	if b.Clicked() {
		if closed {
			delete(s.closed, id)
		} else {
			s.closed[id] = true
		}
		s.layoutRevision = 0
		c.After(0)
	}
	color := branchInk(t, o.Colors, nd.branch)
	if !closed {
		d := dotD
		if b.Hovered() {
			d += 2
		}
		b.Draw(func(p *ui.Painter, r ui.Rect) {
			dot := ui.Rect{X: r.X + (r.W-d)/2, Y: r.Y + (r.H-d)/2, W: d, H: d}
			p.Fill(dot, t.Background, d/2)
			p.Stroke(dot, color.Alpha(.75), d/2, 1.1)
		})
		return
	}
	b.Children(func() {
		ui.Box(c).Size(pillW, pillH).Radius(pillH / 2).Center().Background(color).Children(func() {
			ui.Text(c, fmt.Sprint(count)).FontSize(max(9, t.FontSize-3.5)).Bold().TextColor(t.Background).NoWrap()
		})
	})
}

// Mix branch colors with the editor ink for legibility on either paper theme.
func branchInk(t *ui.Theme, colors []ui.Color, k int) ui.Color {
	base := t.Accent
	if len(colors) > 0 {
		base = colors[k%len(colors)]
	}
	return t.Text.MixOklab(base, .73)
}
