package editor

import (
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/mindmap"
	"github.com/egoist/mygo/ui"
)

// MindMap is a navigable projection of one editor. Geometry, scroll and folds
// belong to the view; the document engine owns all text and selection.
type MindMap struct {
	gestureChanged bool
	movingUntil    time.Time
	moving         bool
	layers         map[*mapBody]bool
	layoutRevision uint64
	wrapWidth      float32
	importSource   string
	importHints    map[string]document.ListPlacement
	caret          int
	tree           mindmap.State
	panX, panY     float32
	viewW, viewH   float32
	zoom           float32
	autoFit        bool
	active         string
	doc            *document.Document
	revision       uint64
	theme          ui.Theme
	style          layoutPageKey
	title          mindmap.Item
	items          []mindmap.Item
	bodies         map[string]*mapBody
}

type mapBody struct {
	layer       *ui.Canvas
	codeCaption *ui.TextLayout
	limit       float32
	code        *ui.TextLayout
	source      *document.Block
	snapshot    *document.Block
	editor      *Editor
	span        ui.Rect
	w, h        float32
}

// A short unformatted label gets the component's original typography. Other
// nodes use ArcMD's native block painter, including inline TeX and source colors.
func richMapBlock(b *document.Block) bool {
	if b.Kind != "heading" && b.Kind != "paragraph" {
		return true
	}
	for _, r := range b.Runs {
		if r.Style != (document.Style{}) || strings.ContainsAny(r.Text, "$\n") {
			return true
		}
	}
	return utf8.RuneCountInString(b.Text()) > 28
}

// Whiteboards have an independent revision even when their Markdown URL is unchanged.
func (b *mapBody) needsMeasure(o Options) bool {
	if b.code != nil {
		return false
	}
	if b.editor == nil {
		return true
	}
	if isWhiteboardEmbed(b.source) && o.LoadWhiteboard != nil {
		scene, rev, err := o.LoadWhiteboard(b.source.URL)
		cached, ok := b.editor.embedBoards[b.source.URL]
		if err != nil || scene == nil {
			return ok
		}
		return !ok || scene != cached.scene || rev != cached.rev
	}
	if b.source.Kind == "image" && o.LoadImage != nil && len(b.editor.rows) == 1 {
		fig := b.editor.rows[0].picture
		return fig != nil && fig.resource.Loading && !o.LoadImage(b.source.URL).Loading
	}
	return false
}

func (b *mapBody) measure(o Options) {
	b.layer = nil
	b.snapshot = b.source.Clone()
	if b.source.Kind == "code" && b.source.Language != "mermaid" && !isWhiteboardBlock(b.source) {
		limit := max(36, b.limit-8)
		b.code, b.w, b.h = mapCodeLayout(b.source.Text(), b.source.Language, pageOf(o.Page, o.Theme), limit)
		label := "</>"
		if name := languageLabel(b.source.Language); name != "" {
			label += "  " + name
		}
		b.codeCaption = ui.ShapeText(label, ui.Font{Family: "monospace", Size: 10, Weight: 600, LineHeight: 1.1}, limit)
		cw, ch := b.codeCaption.Size()
		b.w = max(b.w, cw) + 8
		b.h += ch + 7
		return
	}
	block := b.snapshot.Clone()
	// Depth is already expressed by the map. Give styled headings a compact
	// text layout instead of printing each standalone snapshot as a page title.
	if block.Kind == "heading" {
		block.Kind = "paragraph"
		for j := range block.Runs {
			block.Runs[j].Style.Bold = true
		}
	}
	block.Indent = 0
	b.w = b.limit
	if block.Kind == "table" {
		b.w = mapTableWidth(block, b.limit)
	}
	ed := FromOwnedDocument(&document.Document{Blocks: []*document.Block{block}})
	ed.mapTablePreview = block.Kind == "table"
	o.Width = b.w + 92 // native page layout reserves 46 DIP on each side
	o.FontSize = 14
	ed.Paginate(o, 1e8)
	b.editor = ed
	b.span, _ = ed.blockSpan(block.ID)
	b.span.X = ed.padding
	if block.Kind == "math" && len(ed.rows) == 1 && ed.rows[0].math != nil {
		r := ed.rows[0]
		b.w = max(12, r.math.W*r.mathScale)
		b.h = max(16, (r.math.Ascent+r.math.Descent)*r.mathScale)
		return
	}
	if block.Kind == "table" && len(ed.decorations) > 0 {
		for _, d := range ed.decorations {
			if d.table.W > 0 && d.table.H > 0 {
				b.span.Y = d.table.Y
				b.span.H = d.table.H
				break
			}
		}
	}
	if (block.Kind == "paragraph" || isList(block.Kind) || block.Kind == "quote") && len(ed.rows) == 1 && ed.rows[0].layout != nil {
		r := ed.rows[0]
		_, h := r.layout.Size()
		var right float32
		for _, line := range r.layout.Lines() {
			right = max(right, line.Rect.X+line.Rect.W)
		}
		b.span.Y = r.y
		b.span.H = h
		b.w = max(12, float32(math.Ceil(float64(r.x-ed.padding+right))))
	}
	if block.Kind == "image" && len(ed.rows) == 1 {
		row := ed.rows[0]
		if fig := row.picture; fig != nil && fig.resource.Bitmap != nil && !ed.captionVisible(block) {
			b.span = fig.bounds
			b.w = b.span.W
		}
	}
	b.span.W = b.w
	b.span = mapAnnotationBounds(ed, b.span)
	b.w, b.h = b.span.W, max(b.span.H, 18)
}

// Hand-drawn marks may extend outside the text layout on all four sides.
func mapAnnotationBounds(ed *Editor, ink ui.Rect) ui.Rect {
	for _, r := range ed.rows {
		if r.layout == nil || r.leaf.Block == nil {
			continue
		}
		for _, run := range annotationRuns(r.leaf.Block.Runs) {
			look, ok := lookOf(run.variant)
			if !ok {
				continue
			}
			for _, line := range annotationLineRects(r, run, look, r.size) {
				if line.W < 1 {
					continue
				}
				mark := annotationBox(look, line, r.size)
				// Include the hand-drawn wobble and the pen's half width.
				edge := max(float32(3), r.size*.16)
				x, y := r.x+mark.X, r.y+mark.Y
				l, t := min(ink.X, x-edge), min(ink.Y, y-edge)
				ink = ui.Rect{X: l, Y: t, W: max(ink.X+ink.W, x+mark.W+edge) - l, H: max(ink.Y+ink.H, y+mark.H+edge) - t}
			}
		}
	}
	return ink
}

func (b *mapBody) paint(p *ui.Painter, r ui.Rect, z float32, t *ui.Theme) {
	if b.code != nil {
		p.Clip(r, 0, func() {
			p.Transform(ui.Scaling(z, z), r.X, r.Y, func() {
				_, ch := b.codeCaption.Size()
				p.TextLayout(b.codeCaption, r.X+4, r.Y+2, t.TextMuted)
				p.TextLayout(b.code, r.X+4, r.Y+ch+5, t.Text)
			})
		})
		return
	}
	if b.editor == nil {
		return
	}
	ed := b.editor
	if b.source.Kind == "math" && len(ed.rows) == 1 && ed.rows[0].math != nil {
		row := ed.rows[0]
		scale := row.mathScale * z
		p.Transform(ui.Scaling(scale, scale), r.X, r.Y, func() { ed.paintFormula(p, *row.math, r.X, r.Y+row.math.Ascent, t) })
		return
	}
	p.Clip(r, 0, func() {
		p.Transform(ui.Scaling(z, z), r.X, r.Y, func() {
			ed.PaintPage(p, ui.Rect{X: r.X - b.span.X, Y: r.Y, W: ed.width, H: b.h}, b.span.Y, b.span.Y+b.h, t)
		})
	})
}

func (m *MindMap) sync(e *Editor, o Options) {
	doc := e.Committed()
	key := layoutPageKey{has: true, page: o.Page, fontFamily: o.FontFamily, lineHeight: o.LineHeight, column: m.wrapWidth}
	reuse := m.theme == *o.Theme && sameLayoutPageKey(key, m.style)
	if m.doc == doc && m.revision == e.Revision() && reuse && m.importSource == doc.ImportSource {
		return
	}
	m.layoutRevision++
	m.theme = *o.Theme
	m.style = key
	m.style.page = clonePage(o.Page)
	if m.importSource != doc.ImportSource || m.doc != doc {
		m.importSource = doc.ImportSource
		m.importHints = restoredListHints(doc)
	}
	m.doc, m.revision = doc, e.Revision()
	previous := m.bodies
	m.bodies = make(map[string]*mapBody)
	m.items = m.items[:0]
	m.title = mindmap.Item{Text: i18n.T("思维导图")}
	start := 0
	if len(doc.Blocks) > 0 && doc.Blocks[0].Kind == "heading" {
		start = 1
		m.title = m.item(doc.Blocks[0], 0, previous, reuse, o.Theme)
		// The root is always visible.
		if body := m.bodies[m.title.ID]; body != nil && body.editor == nil {
			body.measure(o)
			m.title.Width, m.title.Height = body.w, body.h
		}
	}
	var headings, lists []int
	for _, b := range doc.Blocks[start:] {
		if b.Kind == "heading" {
			for len(headings) > 0 && headings[len(headings)-1] >= b.Level {
				headings = headings[:len(headings)-1]
			}
		}
		depth := len(headings)
		placement, known := m.importHints[b.ID]
		if b.MapList != nil {
			placement, known = *b.MapList, true
		}
		level, listItem, body := b.Indent, isList(b.Kind), false
		if known {
			level, listItem, body = placement.Depth, placement.Item, !placement.Item
		}
		if listItem {
			for len(lists) > 0 && lists[len(lists)-1] >= level {
				lists = lists[:len(lists)-1]
			}
			depth += len(lists)
			lists = append(lists, level)
		} else if body || (b.Kind != "heading" && b.Indent > 0) {
			for len(lists) > 0 && (lists[len(lists)-1] > level || (!body && lists[len(lists)-1] == level)) {
				lists = lists[:len(lists)-1]
			}
			depth += len(lists)
		} else {
			lists = nil
		}
		m.items = append(m.items, m.item(b, depth, previous, reuse, o.Theme))
		if b.Kind == "heading" {
			headings = append(headings, b.Level)
		}
	}
}

// Decode intentionally omits transient parse hints. Recover them once from the
// retained Markdown; bind to the currently opened IDs, never to label strings.
func restoredListHints(doc *document.Document) map[string]document.ListPlacement {
	if doc.ImportSource == "" {
		return nil
	}
	parsed, _ := parseMarkdownForExport(doc.ImportSource)
	bs := parsed.doc.Blocks
	live := doc.Blocks
	if len(live) > len(bs) && len(live) > 0 && live[0].Kind == "heading" && (len(bs) == 0 || bs[0].Kind != "heading") {
		live = live[1:]
	}
	if len(live) > len(bs) && live[len(live)-1].Kind == "paragraph" && live[len(live)-1].Text() == "" {
		live = live[:len(live)-1]
	}
	if len(bs) != len(live) {
		return nil
	}
	hints := map[string]document.ListPlacement{}
	for i, src := range bs {
		if src.Kind != live[i].Kind {
			return nil
		}
		if src.MapList != nil {
			hints[live[i].ID] = *src.MapList
		}
	}
	return hints
}

func (m *MindMap) item(b *document.Block, depth int, old map[string]*mapBody, reuse bool, t *ui.Theme) mindmap.Item {
	item := mindmap.Item{ID: b.ID, Text: BlockExcerpt(b), Depth: depth}
	if !richMapBlock(b) {
		if text := b.Text(); text != "" {
			item.Text = text
		}
		return item
	}
	body := old[b.ID]
	if !reuse || body == nil || body.snapshot == nil || !document.SameBlock(body.snapshot, b) {
		limit := m.wrapWidth
		if limit <= 0 {
			limit = 35 * 14
		}
		body = &mapBody{limit: limit, w: limit, h: 32}
	}
	body.source = b
	item.CodeBlock = b.Kind == "code" && b.Language != "mermaid" && !isWhiteboardBlock(b)
	m.bodies[b.ID] = body
	item.Image = b.Kind == "image" || b.Kind == "image_ref"
	item.Panel = item.CodeBlock || b.Kind == "table" || item.Image
	item.Width, item.Height = body.w, body.h
	item.Draw = func(p *ui.Painter, r ui.Rect, z float32) { m.paintBody(body, p, r, z, t) }
	return item
}

// During a gesture each visible rich block is composited from its own texture.
// At rest the normal vector painter renders at the exact final scale.
func (m *MindMap) paintBody(b *mapBody, p *ui.Painter, r ui.Rect, z float32, t *ui.Theme) {
	if !m.moving || (b.editor == nil && b.code == nil) {
		b.paint(p, r, z, t)
		return
	}
	if b.layer == nil {
		scale := max(.5, m.zoom) * p.Scale()
		w, h := int(math.Ceil(float64(b.w*scale))), int(math.Ceil(float64(b.h*scale)))
		if w <= 0 || h <= 0 || w > 4096 || h > 4096 || w*h > 2<<20 {
			b.paint(p, r, z, t)
			return
		}
		b.layer = ui.NewCanvas(w, h)
		b.layer.Draw(p, func(cp *ui.Painter) { b.paint(cp, ui.Rect{W: b.w * scale, H: b.h * scale}, scale, t) })
	}
	m.layers[b] = true
	p.Image(b.layer.Bitmap(), r, ui.FillBox)
}

func (m *MindMap) updateMotion(c *ui.Context) {
	now := c.Now()
	if m.gestureChanged {
		m.movingUntil = now.Add(140 * time.Millisecond)
		m.gestureChanged = false
	}
	m.moving = now.Before(m.movingUntil)
	if m.moving {
		c.After(m.movingUntil.Sub(now))
		if m.layers == nil {
			m.layers = make(map[*mapBody]bool)
		}
	}
	for b, visible := range m.layers {
		if !m.moving || !visible {
			b.layer = nil
			delete(m.layers, b)
		} else {
			m.layers[b] = false
		}
	}
}

const (
	mapCellX      float32 = 6
	mapCellY      float32 = 3
	mapCellMinRow float32 = 21
	mapCellFont   float32 = 12
)

// Map table columns follow their actual cell text; source widths remain untouched.
func mapTableWidth(b *document.Block, limit float32) float32 {
	if len(b.Children) == 0 {
		return 80
	}
	widths := make([]float32, len(b.Children[0].Children))
	font := ui.Font{Size: mapCellFont, Weight: 600}
	for _, row := range b.Children {
		for j, cell := range row.Children {
			if j >= len(widths) {
				break
			}
			sample := cell.Text()
			if len(sample) > 256 {
				sample = sample[:document.ByteOffset(sample, 64)]
			}
			w, _ := ui.ShapeText(sample, font, 0).Size()
			widths[j] = max(widths[j], max(24+2*mapCellX, float32(math.Ceil(float64(w)))+2*mapCellX+2))
		}
	}
	var total float32
	for _, w := range widths {
		total += w
	}
	// Each preview cell keeps its horizontal padding and at least 24
	// for its text. Shrink only the space above that readable minimum.
	floor := float32(len(widths)) * (24 + 2*mapCellX)
	target := max(floor, min(total, limit))
	if target < total && total > floor {
		for i, w := range widths {
			widths[i] = (24 + 2*mapCellX) + max(0, w-(24+2*mapCellX))*(target-floor)/(total-floor)
		}
	}
	b.Widths = widths
	return max(80, target)
}

func (m *MindMap) pan(dx, dy float32) {
	m.gestureChanged = true
	m.autoFit = false
	m.panX += dx
	m.panY += dy
}
func (m *MindMap) zoomAt(x, y, factor float32) {
	if factor <= 0 || m.zoom <= 0 {
		return
	}
	next := min(3.0, max(.10, m.zoom*factor))
	ratio := next / m.zoom
	m.panX = x - (x-m.panX)*ratio
	m.panY = y - (y-m.panY)*ratio
	m.zoom = next
	m.gestureChanged = true
	m.autoFit = false
}
func (m *MindMap) gesture(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputZoom:
		m.zoomAt(ev.X, ev.Y, ev.Scale)
		return true
	case ui.InputScroll:
		if ev.Mods&(ui.Cmd|ui.Ctrl) != 0 {
			m.zoomAt(ev.X, ev.Y, float32(math.Exp(-float64(ev.DY)*.008)))
		} else if ev.Mods&ui.Shift != 0 && ev.DX == 0 {
			m.pan(-ev.DY, 0)
		} else {
			m.pan(-ev.DX, -ev.DY)
		}
		return true
	}
	return false
}
func (m *MindMap) locate(v ui.Rect) {
	r := m.tree.Target
	x, y := m.panX+r.X*m.zoom, m.panY+r.Y*m.zoom
	if x < 4 || x+r.W*m.zoom > v.W-4 {
		m.panX = (v.W-r.W*m.zoom)/2 - r.X*m.zoom
	}
	if y < 4 || y+r.H*m.zoom > v.H-4 {
		m.panY = (v.H-r.H*m.zoom)/2 - r.Y*m.zoom
	}
}

func mapLayoutSwitch(c *ui.Context, both bool) bool {
	selected := 0
	if both {
		selected = 1
	}
	t := c.Theme()
	parts := ui.SegmentedBase(c, &selected, 2)
	parts.Track.Label(i18n.T("导图布局")).AlignItems(ui.Stretch).Padding(2).Radius(7).Background(t.Surface).Border(1, t.Border).Children(func() {
		for i, icon := range []*ui.SVG{iconMapOneSide, iconMapBothSides} {
			name := i18n.T("单侧布局")
			if i == 1 {
				name = i18n.T("双侧布局")
			}
			segment := parts.Segment(i).Size(29, 22).Radius(5).Label(name).Tooltip(name)
			ink := t.TextMuted
			if i == selected {
				fill := t.Background
				if t.Dark {
					fill = t.SurfacePressed
				}
				segment.Background(fill).Shadow(0, 1, 2, 0, t.Text.Alpha(.14))
				ink = t.Text
			}
			segment.Children(func() { ui.Icon(c, icon).FontSize(17).TextColor(ink) })
		}
	})
	return selected == 1
}

// View lays out a mygo-native-ui map inside a native two-axis scroll view.
// Switching between document blocks reveals their map nodes, even under a fold.
func (m *MindMap) View(c *ui.Context, e *Editor, o Options, width float32, colors []ui.Color, bothSides bool, setBothSides func(bool)) {
	if m.zoom == 0 {
		m.zoom = 1
		m.autoFit = true
	}
	// Like StackMind's max-width:35em, this is a wrapping ceiling, not a
	// node width. Leave room in the dock for the root and branch connectors.
	share := float32(.68)
	if bothSides {
		share = .50
	}
	m.wrapWidth = max(80, min(35*14, float32(math.Floor(float64((width-12)*share)))))
	m.sync(e, o)
	id := e.Engine.LeafAt(e.Caret).Owner
	reveal := id != m.active || e.Caret != m.caret
	m.active, m.caret = id, e.Caret
	locate := false
	t := o.Theme
	ui.Column(c.Key("map-pane")).Width(width).HeightPercent(100).MinWidth(0).MinHeight(0).Clip().Children(func() {
		ui.Row(c.Key("map-controls")).Height(32).PaddingX(6).Gap(3).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.T("思维导图")).Grow(1).FontSize(12).TextColor(t.TextMuted)
			if next := mapLayoutSwitch(c.Key("map-layout"), bothSides); next != bothSides {
				bothSides = next
				setBothSides(bothSides)
				locate = true
				c.After(0)
			}
			if toolButton(c, "map-minus", iconRule, i18n.T("缩小"), false).Clicked() {
				m.zoomAt(m.viewW/2, m.viewH/2, 1/1.2)
			}
			if toolButton(c, "map-fit", iconMapCenter, i18n.T("居中并适应画布"), false).Clicked() {
				m.autoFit = true
				reveal = true
			}
			if toolButton(c, "map-plus", iconPlus, i18n.T("放大"), false).Clicked() {
				m.zoomAt(m.viewW/2, m.viewH/2, 1.2)
			}
		})
		sc := ui.Box(c.Key("map-viewport")).Grow(1).MinHeight(0).MinWidth(0).Clip().Cursor(ui.CursorGrab).Label(i18n.T("思维导图画布"))
		v := sc.LayoutBounds()
		m.viewW, m.viewH = v.W, v.H
		sc.HandleInput(m.gesture)
		if dx, dy, held := sc.Dragged(); held && (dx != 0 || dy != 0) {
			m.pan(dx, dy)
			sc.Cursor(ui.CursorGrabbing)
		}
		m.updateMotion(c)
		sc.Children(func() {
			tree := mindmap.View(c.Key("map-tree"), &m.tree, m.title, m.items, mindmap.Options{
				LayoutRevision: m.layoutRevision, Colors: colors, MaxWidth: m.wrapWidth, Locate: locate, BothSides: bothSides, Zoom: m.zoom, Selected: id, Reveal: reveal, Pan: m.pan,
				Visible: ui.Rect{X: -m.panX, Y: -m.panY, W: v.W, H: v.H},
				Prepare: func(i int) bool {
					item := &m.items[i]
					body := m.bodies[item.ID]
					if !body.needsMeasure(o) {
						return false
					}
					body.measure(o)
					item.Width, item.Height = body.w, body.h
					return true
				},
				Chosen: func(id string) {
					if id != "" {
						e.RevealBlock(id)
						c.After(0)
					}
				},
				FoldLabel: i18n.T("折叠"), UnfoldLabel: i18n.T("展开"),
			})
			if m.autoFit && v.W > 0 && v.H > 0 && m.tree.Width > 0 && m.tree.Height > 0 {
				next := min(1, min(v.W/m.tree.Width, v.H/m.tree.Height))
				if next != m.zoom {
					m.zoom = next
					c.After(0)
				}
				m.panX = (v.W - m.tree.Width*next) / 2
				m.panY = (v.H - m.tree.Height*next) / 2
			} else if v.W > 0 && (reveal || locate) {
				m.locate(v)
			}
			tree.Absolute().Left(0).Top(0).TransformOrigin(0, 0).Transform(ui.Scaling(m.zoom, m.zoom).Then(ui.Translation(m.panX, m.panY)))
		})
	})
}
