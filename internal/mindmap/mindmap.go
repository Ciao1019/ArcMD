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

type State struct {
	layoutRevision  uint64
	layoutBothSides bool
	nodes           []node
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
	Reveal         bool
	Chosen         func(string)
	// Visible is the viewport in scaled diagram coordinates. The selected
	// node still exists offscreen when it must be revealed.
	Visible                ui.Rect
	Prepare                func(index int) bool // measure a newly visible rich node
	FoldLabel, UnfoldLabel string
}

type node struct {
	kids []int
	// w and h are the item's size, tall the height of the tree under it.
	w, h, tall float32
	x, y       float32
	left       bool
	branch     int
	shown      bool
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

func (s *State) layout(c *ui.Context, rootItem Item, items []Item, o Options) {
	t := c.Theme()
	if s.closed == nil {
		s.closed = map[string]bool{}
	}
	gapX, gapY, pad := t.Space(5.5), t.Space(.75), t.Space(2)
	mainGapX, mainGapY := t.Space(8), t.Space(1.5)
	title := rootItem.Text
	padX, padY := t.Space(.75), t.Space(.5)
	// The title is node n, after the items.
	n := len(items)
	nodes := make([]node, n+1)
	var stack []int
	for i, it := range items {
		for len(stack) > 0 && items[stack[len(stack)-1]].Depth >= it.Depth {
			stack = stack[:len(stack)-1]
		}
		parent := n
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		nodes[parent].kids = append(nodes[parent].kids, i)
		if o.Reveal && items[i].ID == o.Selected {
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
			span := ui.Span{Text: text, Size: fs, Weight: wt}
			w, h = c.MeasureText(0, span)
			if o.MaxWidth > 0 && w > o.MaxWidth {
				w, h = c.MeasureText(o.MaxWidth, span)
				w = min(w, o.MaxWidth)
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
	var measure func(i int) float32
	measure = func(i int) float32 {
		nd := &nodes[i]
		nd.tall = nd.h
		if i < n && items[i].CodeBlock {
			nd.tall += 2 * t.Space(2)
		}
		if i < n && s.closed[items[i].ID] {
			return nd.tall
		}
		var sum float32
		for k, j := range nd.kids {
			if k > 0 {
				sum += gapY
			}
			sum += measure(j)
		}
		nd.tall = max(nd.tall, sum)
		return nd.tall
	}
	root := &nodes[n]
	var right, left []int
	// The sides split by items, folded or not, so that folding a branch
	// leaves the others where they are.
	var leaves func(i int) int
	leaves = func(i int) int {
		k := 0
		for _, j := range nodes[i].kids {
			k += leaves(j)
		}
		return max(1, k)
	}
	total, acc := leaves(n), 0
	for k, j := range root.kids {
		measure(j)
		nodes[j].branch = k
		w := leaves(j)
		if !o.BothSides || (len(left) == 0 && (k == 0 || 2*acc+w <= total)) {
			right = append(right, j)
			acc += w
		} else {
			left = append(left, j)
		}
	}
	// place puts node i with its middle at y, its side toward the title
	// at x, and the tree under it beyond.
	var place func(i int, x, y float32, left bool, branch int)
	place = func(i int, x, y float32, left bool, branch int) {
		nd := &nodes[i]
		nd.left, nd.branch, nd.shown = left, branch, true
		nd.y = y - nd.h/2
		nd.x = x
		if left {
			nd.x = x - nd.w
		}
		if s.closed[items[i].ID] {
			return
		}
		next := x + nd.w + gapX
		if left {
			next = x - nd.w - gapX
		}
		stackKids(nodes, nd.kids, next, y, gapY, left, branch, place)
	}
	root.shown = true
	root.x, root.y = -root.w/2, -root.h/2
	stackKids(nodes, right, root.w/2+mainGapX, 0, mainGapY, false, -1, place)
	stackKids(nodes, left, -root.w/2-mainGapX, 0, mainGapY, true, -1, place)
	minX, minY, maxX, maxY := root.x, root.y, root.x+root.w, root.y+root.h
	for _, nd := range nodes {
		if nd.shown {
			minX, minY = min(minX, nd.x), min(minY, nd.y)
			maxX, maxY = max(maxX, nd.x+nd.w), max(maxY, nd.y+nd.h)
		}
	}
	ox, oy := pad-minX, pad-minY
	s.Width, s.Height = maxX-minX+2*pad, maxY-minY+2*pad
	s.nodes, s.ox, s.oy = nodes, ox, oy
	s.layoutRevision, s.layoutBothSides = o.LayoutRevision, o.BothSides
}

// View reuses unscaled geometry while panning and zooming.
func View(c *ui.Context, s *State, rootItem Item, items []Item, o Options) ui.Element {
	if o.LayoutRevision == 0 || o.LayoutRevision != s.layoutRevision || s.layoutBothSides != o.BothSides || o.Reveal && len(s.closed) > 0 {
		s.layout(c, rootItem, items, o)
	}
	t := c.Theme()
	nodes, ox, oy := s.nodes, s.ox, s.oy
	n, title := len(items), rootItem.Text
	z := o.Zoom
	if z <= 0 {
		z = 1
	}
	visible := func(x0, y0, x1, y1 float32) bool {
		v := o.Visible
		return v.W <= 0 || v.H <= 0 || ((min(x0, x1)+ox)*z <= v.X+v.W+4 && (max(x0, x1)+ox)*z >= v.X-4 && (min(y0, y1)+oy)*z <= v.Y+v.H+4 && (max(y0, y1)+oy)*z >= v.Y-4)
	}
	box := ui.Box(c).Size(s.Width, s.Height).TransformOrigin(0, 0).Scale(z, z).Shrink(0)
	var under func(i int) int
	under = func(i int) int {
		k := 0
		for _, j := range nodes[i].kids {
			k += 1 + under(j)
		}
		return k
	}
	box.Draw(func(p *ui.Painter, r ui.Rect) {

		for i, nd := range nodes {
			if nd.shown && i < n && items[i].Depth > 0 && !items[i].Panel && visible(nd.x, nd.y+nd.h, nd.x+nd.w, nd.y+nd.h) {
				x, y := r.X+ox+nd.x, r.Y+oy+nd.y+nd.h
				p.Line(x, y, x+nd.w, y, .85, branchInk(t, o.Colors, nd.branch).Alpha(.42))
			}
			if !nd.shown || (i < n && s.closed[items[i].ID]) {
				continue
			}
			for _, j := range nd.kids {
				kid := nodes[j]
				x0, x1 := nd.x+nd.w, kid.x
				if kid.left {
					x0, x1 = nd.x, kid.x+kid.w
				}
				y0, y1 := nd.y+nd.h/2, kid.y+kid.h/2
				if i < n && items[i].Depth > 0 && !items[i].Panel {
					y0 = nd.y + nd.h
				}
				if items[j].Depth > 0 && !items[j].Panel {
					y1 = kid.y + kid.h
				}
				if !visible(x0, y0, x1, y1) {
					continue
				}
				x0, y0, x1, y1 = r.X+ox+x0, r.Y+oy+y0, r.X+ox+x1, r.Y+oy+y1
				dx := (x1 - x0) * .58
				var rise float32
				if y0-y1 < 2 && y1-y0 < 2 {
					rise = t.Space(.8)
				}
				// Opaque polylines use MyGo's GPU segment renderer while
				// retaining the cubic shape and its color on the canvas.
				var curve [17][2]float32
				for k := range curve {
					u := float32(k) / float32(len(curve)-1)
					v := 1 - u
					blend := u * u * (3 - 2*u)
					curve[k] = [2]float32{x0 + (x1-x0)*blend + 3*u*v*(v-u)*dx, y0 + (y1-y0)*blend - 3*u*v*rise}
				}
				width := float32(1.75)
				if i < n {
					width = 1.15
				}
				p.StrokePolyline(curve[:], width, t.Background.Mix(branchInk(t, o.Colors, kid.branch), .72))
			}
		}
	})
	box.Children(func() {
		for i, nd := range nodes {
			if !nd.shown {
				continue
			}
			text := title
			if i < n {
				text = items[i].Text
			}
			fs, wt := labelFont(t, i, items)
			item := rootItem
			if i < n {
				item = items[i]
			}
			rect := ui.Rect{X: (ox + nd.x) * z, Y: (oy + nd.y) * z, W: nd.w * z, H: nd.h * z}
			selected := o.Selected != "" && item.ID == o.Selected
			v := o.Visible
			if v.W > 0 && v.H > 0 && !((o.Reveal || o.Locate) && selected) && (rect.X+rect.W < v.X-28 || rect.X > v.X+v.W+28 || rect.Y+rect.H < v.Y-28 || rect.Y > v.Y+v.H+28) {
				continue
			}
			if i < n && item.Draw != nil && o.Prepare != nil && o.Prepare(i) {
				s.layoutRevision = 0
				c.After(0)
			}
			el := ui.ButtonBase(c.Key(item.ID)).Absolute().Left(ox+nd.x).Top(oy+nd.y).Size(nd.w, nd.h).
				Justify(ui.Center).AlignItems(ui.Center).Role(ui.RoleButton).Selected(selected).Label(text)
			if selected {
				s.Target = ui.Rect{X: ox + nd.x, Y: oy + nd.y, W: nd.w, H: nd.h}
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
				el.Background(t.Text.Mix(branchInk(t, o.Colors, 0), .50)).Radius(nd.h / 2)
				color = t.Background
				if item.Draw != nil {
					el.Background(t.Surface).Border(1, branchInk(t, o.Colors, 0).Alpha(.55))
				}
			case item.Image:
				el.Background(t.Background.Alpha(0))
			case item.CodeBlock:
				el.Background(t.Background.Mix(t.Text, .045).Mix(branchInk(t, o.Colors, nd.branch), .025)).Border(1, t.Border).Radius(5)
			case items[i].Depth == 0:
				el.Background(branchInk(t, o.Colors, nd.branch).Alpha(.09)).Border(1, branchInk(t, o.Colors, nd.branch).Alpha(.27)).Radius(t.Space(1.5))
			default:
				el.Background(t.Background.Alpha(0)).Radius(t.Space(1))
				if el.Hovered() {
					el.Background(t.SurfaceHover)
				}
			}
			if selected {
				if item.CodeBlock {
					el.Border(1, branchInk(t, o.Colors, nd.branch).Alpha(.60))
				} else if i < n {
					el.Background(branchInk(t, o.Colors, nd.branch).Alpha(.12)).Border(1, branchInk(t, o.Colors, nd.branch).Alpha(.40))
				} else {
					el.Border(1, branchInk(t, o.Colors, 0).Alpha(.65))
				}
			}
			if el.Clicked() && s.dragID != item.ID && o.Chosen != nil {
				o.Chosen(item.ID)
			}
			if s.dragID == item.ID && !el.Pressed() {
				s.dragID = ""
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
					count = under(i)
				}
				fold(c.Key("fold:"+item.ID), s, item.ID, nd, ox, oy, items[i].Depth == 0 || items[i].Panel, count, o)
			}
		}
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
	d := t.Space(2.25)
	if closed {
		d = t.Space(3.5)
	}
	cx := ox + nd.x + nd.w + t.Space(.5) + d/2
	if nd.left {
		cx = ox + nd.x - t.Space(.5) - d/2
	}
	cy := oy + nd.y + nd.h
	if branch {
		cy = oy + nd.y + nd.h/2
	}
	b := ui.ButtonBase(c).Absolute().Left(cx-d/2).Top(cy-d/2).Size(d, d).Radius(d / 2).
		Justify(ui.Center).AlignItems(ui.Center).Label(o.FoldLabel)
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
		b.Background(t.Background).Border(1.1, color.Alpha(.75))
		return
	}
	b.Label(o.UnfoldLabel).Background(color).Children(func() {
		ui.Text(c, fmt.Sprint(count)).FontSize(t.FontSize - 3).Bold().TextColor(t.Background)
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
