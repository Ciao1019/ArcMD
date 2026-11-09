package editor

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/tex"
	"github.com/egoist/mygo/ui"
)

type Editor struct {
	services ui.Services
	hoverID  string
	*Buffer
	Scroll                           ui.ScrollState
	RequestFocus                     bool
	rows                             []row
	decorations                      []decoration
	handles                          []blockHandle
	resizers                         []columnHandle
	cache                            map[string]cachedShape
	images                           map[string]*ui.Bitmap
	width, height, viewport, padding float32
	cacheRevision                    uint64
	resizePending                    bool
	resizeScroll                     float32
	cacheTheme                       ui.Theme
	ready, focused, dragging, reveal bool
	preferredX                       float32
	vertical                         bool
	lastInput                        time.Time
	dragID                           string
	dropIndex                        int
	resizing                         *columnHandle
	resizeX                          float32
	menuOpen                         bool
	menuID                           string
	menuSub                          string
	tableSel                         tableSelection
	cellFillOpen, cellFillOpened     bool
	foldButtons                      []foldButton
	revealKey                        [2]uint64
	find                             finder
	revealAt                         *ui.TextInputRange
	colorPanel, colorPanelOpened     bool
	lastColor, lastHighlight         string
	tableDanger                      bool
	attach                           func()
	// pasteImage takes a picture pasted from the clipboard; dropFiles takes
	// files dragged in from another app or pasted, to go before the top-level
	// block at the index (-1: at the caret). fileOver is where files dragged
	// over the page would go, the y of the line showing it, or -1.
	pasteImage func([]byte)
	// contentEnd is the bottom of the last block; laidBlank whether the
	// layout left room for the starter.
	contentEnd     float32
	laidBlank      bool
	dropFiles      func([]string, int)
	fileOver       float32
	slashOpen      bool
	slashQuery     string
	slashIndex     int
	slashDismissed bool
	linkOpen       bool
	linkURL        string
	outline        []outlineItem
	outlineState   outlineState
	pointer        ui.Point
	pointerInside  bool
	copiedID       string
	copiedAt       time.Time
	page           Page
	pageKey        string
	colorOpen      bool
	colorID        string
	langOpen       bool
	langID         string
	langAt         ui.Rect
	colorAt        ui.Rect
	words          int
	wordsRevision  uint64
	wordsReady     bool
	// wiki completes the [[link]] being typed (wikilinks.go).
	wiki wikiState
	// footerH is the height the host's footer took the last frame.
	footerH float32
	// printing paints for paper: no caret, hints, controls or hover
	// (print.go).
	printing bool
	// typewriter keeps the caret's line at the middle of the page; dim
	// fades the blocks but the caret's, as focus mode does.
	typewriter, dim bool
	// header is the page's icon and cover (header.go).
	header headerState
	// mathEditing is the math block the caret is in, which shows its TeX;
	// the others show the formula (math.go).
	mathEditing, laidMath string
	bindings              map[string]Combo
	openWiki, openTag     func(string)
	maths                 map[string]mathShape
	mathGlyphs            map[string]*ui.TextLayout
}

type row struct {
	leaf   document.Leaf
	layout *ui.TextLayout
	rect   ui.Rect
	x, y   float32
	image  *ui.Bitmap
	// number is the position of a numbered item in its list, from 1.
	number int
	// math is a math block's formula, drawn from mathTop: in place of its
	// TeX when mathHidden; mathErr says why its TeX did not lay out.
	math       *tex.Box
	mathTop    float32
	mathHidden bool
	mathErr    string
}

// decoration is what the page draws behind its text: the fill of a code
// block or callout, the bar of a quote, a table's cells.
type decoration struct {
	kind   string
	rect   ui.Rect
	header bool
	// table is the whole table a cell belongs to, which clips its header's
	// fill to the table's rounded corners; right and below draw the cell's
	// inner rules, so neighbouring cells share one hairline.
	table        ui.Rect
	right, below bool
	label        string
	// tone is a callout's color.
	tone string
	// id is the block of a code block or table, whose hover controls act
	// on it.
	id string
	// cols and rows are a table's column and row edges.
	cols, rows []float32
}

// hovered reports whether the pointer is over r, with slack around it for
// controls drawn just outside, as a table's add bars are.
func (e *Editor) hovered(r ui.Rect, slack float32) bool {
	p := e.pointer
	return e.pointerInside && !e.dragging && e.dragID == "" && p.X >= r.X-slack && p.X <= r.X+r.W+slack && p.Y >= r.Y-slack && p.Y <= r.Y+r.H+slack
}

// copyButton is where a code block shows its copy button.
func copyButton(d decoration) ui.Rect {
	return ui.Rect{X: d.rect.X + d.rect.W - 76, Y: d.rect.Y + 7, W: 66, H: 22}
}

// languageButton is where a code block shows and picks its language.
func languageButton(d decoration) ui.Rect {
	return ui.Rect{X: d.rect.X + 12, Y: d.rect.Y + 7, W: 104, H: 22}
}

// codeLanguages are the languages the picker offers, by fence name.
var codeLanguages = []struct{ name, label string }{
	{"", "纯文本"}, {"go", "Go"}, {"javascript", "JavaScript"}, {"typescript", "TypeScript"}, {"python", "Python"},
	{"rust", "Rust"}, {"java", "Java"}, {"c", "C"}, {"cpp", "C++"}, {"csharp", "C#"}, {"swift", "Swift"},
	{"kotlin", "Kotlin"}, {"ruby", "Ruby"}, {"php", "PHP"}, {"bash", "Shell"}, {"sql", "SQL"},
	{"html", "HTML"}, {"css", "CSS"}, {"json", "JSON"}, {"yaml", "YAML"}, {"markdown", "Markdown"},
}

// languageLabel names a fence language for people.
func languageLabel(name string) string {
	for _, l := range codeLanguages {
		if strings.EqualFold(l.name, name) {
			return l.label
		}
	}
	return name
}

// paintLanguage draws a code block's language button.
func (e *Editor) paintLanguage(p *ui.Painter, r ui.Rect, d decoration, t *ui.Theme) {
	label := languageLabel(d.label)
	if label == "纯文本" || label == "" {
		label = "Plain Text"
	}
	span := ui.Span{Text: label, Size: 14, Color: t.Text}
	w, h := p.MeasureText(0, span)
	if e.langOpen && e.langID == d.id || e.hovered(languageButton(d), 0) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: w + 26, H: r.H}, e.page.Hover, 4)
	}
	p.RichText(r.X+4, r.Y+(r.H-h)/2, 0, span)
	p.Icon(iconChevron, ui.Rect{X: r.X + 6 + w, Y: r.Y + 4, W: 14, H: 14}, e.page.Placeholder)
}

// calloutIconRect is a callout's icon, which picks its color.
func calloutIconRect(d decoration) ui.Rect {
	return ui.Rect{X: d.rect.X + 16, Y: d.rect.Y + 17, W: 24, H: 24}
}

// calloutHue is the color of a callout's tone.
func (e *Editor) calloutHue(tone string, t *ui.Theme) ui.Color {
	return e.page.Callouts[tone].Icon
}

// calloutIcon tells the tones apart by more than color, as GitHub's alerts
// do: a note, a tip, something important, a warning, a caution.
func calloutIcon(tone string) *ui.SVG {
	switch tone {
	case "green":
		return iconBulb
	case "purple":
		return iconNotice
	case "yellow":
		return iconWarn
	case "red":
		return iconStop
	}
	return iconInfo
}

// CalloutName names a callout color for menus and the command bar.
func CalloutName(tone string) string {
	switch tone {
	case "green":
		return "绿色 · 提示"
	case "purple":
		return "紫色 · 重要"
	case "yellow":
		return "黄色 · 警告"
	case "red":
		return "红色 · 危险"
	}
	return "蓝色 · 说明"
}

// blockHandle is the grip beside a top-level block, with the add button
// before it; top is where the block begins, where a dragged block drops.
type blockHandle struct {
	id    string
	index int
	rect  ui.Rect
	top   float32
}

// plus is the add button left of the grip, as Notion's.
func (h blockHandle) plus() ui.Rect {
	r := h.rect
	r.X -= 26
	return r
}

// foldButton is the triangle that folds a heading or list item.
type foldButton struct {
	id     string
	rect   ui.Rect
	folded bool
}

func isList(kind string) bool { return kind == "bullet" || kind == "numbered" || kind == "task" }

type columnHandle struct {
	id             string
	col            int
	x, y, h, width float32
	widths         []float32
}

// outlineDrag is a heading of the outline dragged to move its section.
type outlineDrag struct{ id string }

// outlineItem is a heading of the outline beside the page; the page
// title is one too.
type outlineItem struct {
	id, text string
	level    int
	y        float32
}

// pageTop is the room above the first block, as a page's top margin.
const pageTop = 48

// pageBottom is the room left below the last block, and starterRoom the
// more a blank document leaves for the starter.
const (
	pageBottom  = 140
	starterRoom = 120
)

type cachedShape struct {
	key    string
	width  float32
	layout *ui.TextLayout
}

type Options struct {
	// Resizing keeps offscreen shaping cached during an interactive resize.
	// Hosts must clear it on release; the final layout is always exact.
	Resizing  bool
	Width     float32
	Theme     *ui.Theme
	LoadImage func(string) *ui.Bitmap
	// Attach, when set, lets the slash menu insert images: the host picks
	// them and inserts them at the caret.
	Attach func()
	// DropFiles, when set, makes the page take files dragged in from other
	// apps and files pasted from the clipboard: at is the top-level block
	// they go before, len(Doc.Blocks) for the end, or -1 for the caret.
	// The host decides which files it can insert.
	DropFiles func(paths []string, at int)
	// PasteImage, when set, takes a picture pasted from the clipboard, as
	// PNG.
	PasteImage func(png []byte)
	// Page are the page's colors beyond Theme; empty ones follow Theme.
	Page     Page
	OpenLink func(string)
	// OpenWiki opens the document a [[link]] names, ⌘-clicked; OpenTag
	// shows the documents of a #tag. WikiSuggest lists the documents to
	// complete a [[link]] being typed with, by what is typed of its name.
	OpenWiki    func(name string)
	OpenTag     func(tag string)
	WikiSuggest func(typed string) []WikiItem
	// Footer, when set, draws below the document, in the page's text
	// column: the host's backlinks.
	Footer func(c *ui.Context)
	// Typewriter keeps the caret's line in the middle of the page; Dim
	// fades every block but the caret's.
	Typewriter, Dim bool
	// PickCover, when set, lets the cover be a picture: the host picks one,
	// puts it beside the document and returns its URL.
	PickCover func(done func(url string))
	// Bindings rebind the editor's commands (DefaultBindings) to other
	// keys; a command left out keeps its default.
	Bindings map[string]Combo
	// Outline opens the outline left of the page; SetOutline, when set,
	// shows the outline's buttons and keeps what the reader picks. Without
	// it the page has no outline, as in thumbnails.
	Outline    bool
	SetOutline func(open bool)
}

func New(markdown string) *Editor { return FromDocument(ImportMarkdown(markdown)) }
func FromDocument(doc *document.Document) *Editor {
	// The color button starts out as a yellow highlighter.
	return &Editor{Buffer: newBuffer(doc), cache: make(map[string]cachedShape), images: make(map[string]*ui.Bitmap), dropIndex: -1, lastHighlight: "light-yellow",
		wiki: wikiState{dismissed: -1}}
}
func (e *Editor) ReplaceText(r *ui.TextInputRange, text string) {
	e.Buffer.ReplaceText(r, text)
	e.reveal, e.vertical = true, false
	e.slashDismissed = false
}
func (e *Editor) SetMarkedText(r *ui.TextInputRange, text string, selected ui.TextInputRange) {
	e.Buffer.SetMarkedText(r, text, selected)
	e.reveal = true
}

func View(c *ui.Context, e *Editor, o Options) ui.Element {
	e.services = c.Services()
	e.page = pageOf(o.Page, o.Theme)
	e.attach = o.Attach
	e.dropFiles, e.pasteImage = o.DropFiles, o.PasteImage
	e.typewriter, e.dim, e.bindings = o.Typewriter, o.Dim, o.Bindings
	e.openWiki, e.openTag = o.OpenWiki, o.OpenTag
	e.header.pickCover = o.PickCover
	e.sync()
	e.findShortcuts(c)
	e.revealCaret()
	e.watchMath()
	pane := ui.Column(c).Grow(1).MinHeight(0).MinWidth(0)
	// The outline, open, takes room left of the page: the page slides over
	// as it opens, keeping offscreen shaping while it moves.
	reserve := e.outlineReserve(c, o)
	if reserve > 0 {
		o.Width -= reserve
	}
	if reserve != e.outlineState.target {
		o.Resizing = true
	}
	e.layout(o)
	e.watchSlash()
	e.watchWiki(o.WikiSuggest)
	var surface ui.Element
	pane.Children(func() {
		e.stepJump(c)
		if e.revealAt != nil && e.viewport > 0 {
			// A find match scrolls to a third of the way down the page.
			if r, _, ok := e.BoundsForRange(*e.revealAt); ok && (r.Y < e.Scroll.Y+20 || r.Y+r.H > e.Scroll.Y+e.viewport-32) {
				e.Scroll.Y = max(0, r.Y-e.viewport/3)
			}
			e.revealAt = nil
		}
		if e.reveal && e.viewport > 0 && e.typewriter {
			// The caret's line stays in the middle of the page.
			if r, _, ok := e.BoundsForRange(ui.TextInputRange{Start: e.Caret, End: e.Caret}); ok {
				e.Scroll.Y = max(0, r.Y+r.H/2-e.viewport/2)
			}
			e.reveal = false
		}
		if e.reveal && e.viewport > 0 {
			r, _, ok := e.BoundsForRange(ui.TextInputRange{Start: e.Caret, End: e.Caret})
			if ok {
				if r.Y < e.Scroll.Y+20 {
					e.Scroll.Y = max(0, r.Y-20)
				}
				if r.Y+r.H > e.Scroll.Y+e.viewport-32 {
					e.Scroll.Y = max(0, r.Y+r.H-e.viewport+32)
				}
			}
			e.reveal = false
		}
		scroll := ui.Scroll(c.Key("page-scroll")).Grow(1).MinHeight(0).TrackScroll(&e.Scroll)
		// Bounds are the last frame's: none yet in the first frame of a new
		// document, where the window's height stands in, or painting would
		// cull every row as out of view and nothing would draw until
		// another frame came.
		// The box as laid out, before transforms: a scaled thumbnail shows all
		// its rows.
		e.viewport = scroll.LayoutBounds().H
		if e.viewport <= 0 {
			_, e.viewport = c.Size()
		}
		scroll.Children(func() {
			height := e.height
			if o.Footer != nil {
				height = max(height, e.footerTop()+e.footerH+pageBottom)
			}
			if e.typewriter {
				// Room for the last line to come to the middle.
				height += e.viewport / 2
			}
			// The surface and what is placed over it scroll together in one
			// box: placed elements stay where the scroll's content was laid
			// out, not where it scrolled to.
			e.outlineState.scrollMax = max(0, max(height, e.viewport)-e.viewport)
			content := ui.Box(c.Key("page-content")).Width(o.Width).Height(max(height, e.viewport)).Shrink(0).MarginStart(reserve)
			content.Children(func() {
				surface = ui.Box(c.Key("writing-surface")).Width(o.Width).Height(max(height, e.viewport)).Shrink(0).
					HandleTextInput(e).Role(ui.RoleTextField).Label("文档").Value(e.text).FocusRing(false)
				if e.RequestFocus {
					surface.Focus()
					e.RequestFocus = false
				}
				e.focused = surface.Focused()
				px, py, inside := surface.PointerPosition()
				e.pointer, e.pointerInside = ui.Point{X: px, Y: py}, inside
				e.fileOver = -1
				if o.DropFiles != nil {
					// A file drag keeps the pointer's position up to date, so
					// the frame after the drop still has where it landed.
					at, line := e.fileDropAt(py)
					if surface.FileDragOver() {
						e.fileOver = line
					}
					if files := surface.DroppedFiles(); len(files) > 0 {
						o.DropFiles(files, at)
					}
				}
				e.hoverID = ""
				if inside {
					for _, h := range e.handles {
						if py >= h.top-4 {
							e.hoverID = h.id
						}
					}
				}
				services := c.Services()
				surface.HandleInput(func(ev ui.InputEvent) bool {
					return e.input(ev, services.ReadClipboard, services.WriteClipboard, o.OpenLink)
				})
				surface.ContextMenu(func(menu *ui.Menu) {
					menuAction(menu, "加粗", func() { e.Wrap("**") })
					menuAction(menu, "斜体", func() { e.Wrap("*") })
					menuAction(menu, "插入表格", func() { e.Engine.Insert(document.Table(3, 3)); e.RequestFocus = true })
				})
				caret := e.focused && (c.Now().Sub(e.lastInput) < time.Second || c.Now().UnixMilli()/530%2 == 0)
				if e.focused {
					c.After(530 * time.Millisecond)
				}
				surface.Draw(func(p *ui.Painter, r ui.Rect) { e.paint(p, r, o.Theme, caret) })
				// Beside the surface, not in it: the surface takes the
				// pointer's presses before anything inside it.
				e.starter(c)
				e.headerControls(c)
				if o.Footer != nil {
					footer := ui.Column(c.Key("page-footer")).Absolute().Left(e.padding).Top(e.footerTop()).Width(max(80, e.width-2*e.padding))
					footer.Children(func() { o.Footer(c) })
					if h := footer.LayoutBounds().H; h != e.footerH {
						e.footerH = h
						c.After(0)
					}
				}
			})
		})
		e.outlineView(c, pane, reserve)
		e.findBox(c)
	})
	e.popups(c, surface)
	e.wikiMenu(c, surface)
	e.headerPopups(c, surface)
	return pane
}

// footerTop is where the host's footer goes: below the last block, and
// the starter when it shows.
func (e *Editor) footerTop() float32 {
	top := e.contentEnd + 40
	if e.laidBlank {
		top += starterRoom
	}
	return top
}

func (e *Editor) layout(o Options) {
	pageKey := fmt.Sprint(e.page)
	finishResize := e.resizePending && !o.Resizing
	refreshResize := e.resizePending && e.resizeScroll != e.Scroll.Y
	anchor := resizeAnchor{}
	if finishResize {
		anchor = e.resizeAnchor()
	}
	same := !finishResize && !refreshResize && e.ready && e.width == o.Width && e.cacheTheme == *o.Theme && e.pageKey == pageKey && e.laidMath == e.mathEditing
	if same && e.cacheRevision == e.Revision() {
		return
	}
	if same && e.relayoutText(o) {
		e.cacheRevision = e.Revision()
		return
	}
	if e.cacheTheme != *o.Theme || e.pageKey != pageKey {
		clear(e.cache)
		clear(e.mathGlyphs)
		clear(e.maths)
	}
	e.resizePending = false
	e.resizeScroll = e.Scroll.Y
	e.ready, e.width, e.cacheRevision, e.cacheTheme, e.pageKey = true, o.Width, e.Revision(), *o.Theme, pageKey
	e.laidMath = e.mathEditing
	// The text column is 820 wide.
	e.padding = max(46, (o.Width-820)/2)
	width := max(80, o.Width-2*e.padding)
	e.rows = nil
	e.decorations = nil
	e.handles = nil
	e.resizers = nil
	leaves := make(map[string]document.Leaf)
	for _, leaf := range e.Doc.Leaves() {
		leaves[leaf.Block.ID] = leaf
	}
	seen := make(map[string]bool)
	hidden := e.Doc.HiddenBlocks()
	foldable := e.Doc.FoldableBlocks()
	blockLayout := e.blockLayouter(o, leaves, hidden, seen)
	y := e.layoutHeader(o)
	e.outline = e.outline[:0]
	e.foldButtons = e.foldButtons[:0]
	for i, b := range e.Doc.Blocks {
		if hidden[b.ID] != "" {
			continue
		}
		start := len(e.rows)
		h := blockLayout(b, e.padding, y, width)
		// The handle sits beside the block's first line.
		hy := y + 2
		if start < len(e.rows) && e.rows[start].layout != nil {
			r := e.rows[start]
			c := r.layout.Caret(0)
			hy = r.y + c.Y + max(c.H, 20)/2 - 12
		}
		hx := e.padding - 30
		if b.Kind == "table" {
			// A table's handle sits left of its column bar, clear of the
			// row bar.
			hx, hy = e.padding-48, y+22-21
		}
		if foldable[b.ID] {
			// The fold triangle sits just left of the block, 20
			// left of the text; the handle further out.
			fx := e.padding - 22
			if isList(b.Kind) {
				fx += float32(b.Indent) * 24
			}
			e.foldButtons = append(e.foldButtons, foldButton{b.ID, ui.Rect{X: fx, Y: hy + 4, W: 16, H: 16}, b.Folded})
			if fx-hx < 26 {
				hx = fx - 28
			}
		}
		e.handles = append(e.handles, blockHandle{b.ID, i, ui.Rect{X: hx, Y: hy, W: 24, H: 24}, y})
		if b.Kind == "heading" && b.Text() != "" {
			e.outline = append(e.outline, outlineItem{b.ID, b.Text(), b.Level, y})
		}
		y += h
	}
	for id := range e.cache {
		if !seen[id] {
			delete(e.cache, id)
		}
	}
	if finishResize {
		e.restoreResizeAnchor(anchor)
	}
	e.contentEnd = y
	e.height = y + pageBottom
	e.laidBlank = e.Blank()
	if e.laidBlank {
		e.height += starterRoom
	}
}

func (e *Editor) blockLayouter(o Options, leaves map[string]document.Leaf, hidden map[string]string, seen map[string]bool) func(*document.Block, float32, float32, float32) float32 {
	pg := e.page
	// counters numbers the items of numbered lists by indent; any other
	// block ends the list.
	var counters []int
	header := false
	// cellTop is where the table row being laid out starts. Cells are laid
	// out at y 0 and moved afterwards, so resize deferral needs the real
	// position to tell whether a cell is on screen.
	cellTop := float32(0)
	var blockLayout func(*document.Block, float32, float32, float32) float32
	blockLayout = func(b *document.Block, x, y, w float32) float32 {
		if b.Kind == "columns" {
			top := y
			count := len(b.Children)
			if w < 480 {
				for _, column := range b.Children {
					for _, child := range column.Children {
						if hidden[child.ID] != "" {
							continue
						}
						y += blockLayout(child, x, y, w)
					}
				}
				return y - top + 12
			}
			cw := (w - float32(count-1)*24) / float32(count)
			bottom := y
			for i, column := range b.Children {
				cy := y
				for _, child := range column.Children {
					if hidden[child.ID] != "" {
						continue
					}
					cy += blockLayout(child, x+float32(i)*(cw+24), cy, cw)
				}
				bottom = max(bottom, cy)
			}
			return bottom - top + 12
		}
		if b.Kind == "table" {
			// Room above for the column bar.
			start := y
			y += 22
			top := y
			cols := len(b.Children[0].Children)
			widths := make([]float32, cols)
			sum := float32(cols)
			if len(b.Widths) == cols {
				sum = 0
				for _, v := range b.Widths {
					sum += v
				}
			}
			for i := range widths {
				value := float32(1)
				if len(b.Widths) == cols {
					value = b.Widths[i]
				}
				widths[i] = w * value / sum
			}
			var cells []decoration
			colEdges := []float32{x}
			for _, cw := range widths {
				colEdges = append(colEdges, colEdges[len(colEdges)-1]+cw)
			}
			// Merged cells span several columns and rows; the cells they
			// cover are out of the text and get no row. Each cell is laid
			// out at y 0 first, then moved once the row heights are known.
			covered := document.CoveredCells(b)
			type placed struct {
				index, r, c, rows, cols int
				h                       float32
				cell                    *document.Block
			}
			var spots []placed
			nrows := len(b.Children)
			heights := make([]float32, nrows)
			rowTop := top
			for ri, tr := range b.Children {
				heights[ri] = 42
				header = ri == 0
				cellTop = rowTop
				for ci, cell := range tr.Children {
					if covered[cell] {
						continue
					}
					spanCols, spanRows := document.Span(cell)
					spanCols, spanRows = min(spanCols, cols-ci), min(spanRows, nrows-ri)
					cw := colEdges[ci+spanCols] - colEdges[ci]
					index := len(e.rows)
					h := blockLayout(cell, colEdges[ci]+14, 0, max(24, cw-28)) + 4
					spots = append(spots, placed{index, ri, ci, spanRows, spanCols, h, cell})
					if spanRows == 1 {
						heights[ri] = max(heights[ri], h)
					}
				}
				header = false
				rowTop += heights[ri]
			}
			cellTop = 0
			for _, sp := range spots {
				if sp.rows > 1 {
					sum := float32(0)
					for _, h := range heights[sp.r : sp.r+sp.rows] {
						sum += h
					}
					if sum < sp.h {
						heights[sp.r+sp.rows-1] += sp.h - sum
					}
				}
			}
			rowEdges := []float32{y}
			for _, h := range heights {
				y += h
				rowEdges = append(rowEdges, y)
			}
			for _, sp := range spots {
				rect := ui.Rect{X: colEdges[sp.c], Y: rowEdges[sp.r], W: colEdges[sp.c+sp.cols] - colEdges[sp.c], H: rowEdges[sp.r+sp.rows] - rowEdges[sp.r]}
				row := &e.rows[sp.index]
				row.y += rect.Y + 7
				row.rect = rect
				cells = append(cells, decoration{kind: "cell", rect: rect, header: sp.r == 0, right: sp.c+sp.cols < cols, below: sp.r+sp.rows < nrows, tone: sp.cell.Background, id: sp.cell.ID})
			}
			table := ui.Rect{X: x, Y: top, W: w, H: y - top}
			for _, d := range cells {
				d.table = table
				e.decorations = append(e.decorations, d)
			}
			e.decorations = append(e.decorations, decoration{kind: "table", rect: table, id: b.ID, cols: colEdges, rows: rowEdges})
			counters = nil
			cx := x
			for col := 0; col < cols-1; col++ {
				cx += widths[col]
				e.resizers = append(e.resizers, columnHandle{b.ID, col, cx, top, y - top, w, widths})
			}
			return y - start + 22
		}
		if b.Kind != "bullet" && b.Kind != "numbered" && b.Kind != "task" && b.Indent > 0 {
			indent := min(float32(b.Indent)*20, max(0, w-80))
			x += indent
			w -= indent
		}
		// Body text is 16 on 26 with 8 between blocks; table
		// cells are 14 on 22.
		font := ui.Font{Family: "system-ui", Size: 16, LineHeight: 26.0 / 16}
		if b.Kind == "cell" {
			font.Size, font.LineHeight = 14, 22.0/14
		}
		left, right := float32(0), float32(0)
		top, bottom := float32(0), float32(8)
		// gap is the space below a block's decoration, outside its fill.
		gap := float32(8)
		number := 0
		switch b.Kind {
		case "bullet", "task":
			counters = counters[:min(len(counters), b.Indent)]
		case "numbered":
			counters = counters[:min(len(counters), b.Indent+1)]
			for len(counters) <= b.Indent {
				counters = append(counters, 0)
			}
			counters[b.Indent]++
			number = counters[b.Indent]
		default:
			counters = nil
		}
		switch b.Kind {
		case "heading":
			level := max(1, b.Level)
			// Feishu heading metrics: size, line height, space above
			// and below.
			level = min(level, 6)
			font.Size = []float32{26, 22, 20, 18, 16, 16}[level-1]
			font.LineHeight = []float32{36.4, 30.8, 28, 25.2, 26, 26}[level-1] / font.Size
			font.Weight = 500
			top, bottom = []float32{26, 22, 20, 18, 18, 16}[level-1], []float32{10, 8, 8, 8, 8, 8}[level-1]
			if len(e.Doc.Blocks) > 0 && e.Doc.Blocks[0].ID == b.ID {
				// The first heading is the page title: 34 on 55.25.
				font.Size, font.Weight, font.LineHeight = 34, 500, 55.25/34
				top, bottom = 0, 22
			}
		case "code", "raw", "math":
			// Code blocks: 14 on 22, line numbers right-aligned
			// in the first 36, text from 60, the header in the top 32.
			font.Family, font.Size, font.LineHeight = "monospace", 14, 22.0/14
			gap = 8
			left, right, top, bottom = 20, 20, 16, 16+gap
			if b.Kind == "code" {
				left, top, bottom = 60, 40, 26+gap
			}
		case "quote":
			// A 2px bar and 14 of indent; caption-colored text.
			left, top, bottom, gap = 14, 0, 8, 8
		case "callout":
			// 16 of padding, a 24 icon and 8 after it.
			gap = 8
			left, right, top, bottom = 48, 16, 16, 16+gap
		case "bullet", "numbered", "task":
			// The marker takes 24 (a task's box 24 too) per level.
			left = 24 + float32(b.Indent)*24
			bottom = 8
		}
		var spans []ui.Span
		for _, run := range b.Runs {
			style := run.Style
			span := ui.Span{Text: run.Text, Italic: style.Italic, Underline: style.Underline, Strikethrough: style.Strike}
			if style.Bold {
				span.Weight = 600
			}
			if style.Code {
				span.Font, span.Size, span.Background, span.Radius = "monospace", font.Size*.875, pg.InlineFill, 4
				span.Color = pg.InlineText
			}
			if style.Link != "" {
				span.Color, span.Underline, span.DecorationColor = o.Theme.Accent, style.Underline, o.Theme.Accent.Alpha(.35)
			}
			if c, ok := pg.TextColors[style.Color]; ok {
				span.Color, span.DecorationColor = c, c
			}
			if c, ok := pg.Highlights[style.Highlight]; ok {
				span.Background, span.Radius = c, 2
			}
			switch {
			case header:
				span.Weight = max(span.Weight, 500)
			case b.Kind == "quote":
				span.Color = pg.Caption
			case b.Kind == "task" && b.Checked:
				span.Color, span.Strikethrough, span.DecorationColor = pg.TaskDone, true, pg.TaskDone
			}
			if style.Link == "" {
				spans = append(spans, e.wikiSpans(span, run, b.Kind, o.Theme)...)
				continue
			}
			spans = append(spans, span)
		}
		if b.Kind == "code" {
			spans = codeSpans(b.Text(), pg)
		}
		if len(spans) == 0 {
			spans = []ui.Span{{Text: ""}}
		}
		data, _ := json.Marshal(struct {
			Runs            []document.Run
			Font            ui.Font
			Align, Kind     string
			Checked, Header bool
		}{b.Runs, font, b.Align, b.Kind, b.Checked, header})
		key := string(data)
		cached, ok := e.cache[b.ID]
		shapeWidth := max(24, w-left-right)
		keepOffscreen := false
		if ok && cached.key == key && cached.width != shapeWidth {
			_, oldHeight := cached.layout.Size()
			keepOffscreen = e.deferResizeShape(o, cellTop+y, oldHeight+top+bottom)
			if keepOffscreen {
				e.resizePending = true
			}
		}
		if !ok || cached.key != key || cached.width != shapeWidth && !keepOffscreen {
			align := ui.Start
			if b.Align == "center" {
				align = ui.Center
			}
			if b.Align == "right" {
				align = ui.End
			}
			cached = cachedShape{key: key, width: shapeWidth, layout: ui.ShapeRichTextWith(spans, font, ui.TextShape{Width: shapeWidth, Align: align})}
			e.cache[b.ID] = cached
		}
		seen[b.ID] = true
		_, h := cached.layout.Size()
		h = max(h, font.Size*font.LineHeight)
		total := top + h + bottom
		row := row{leaf: leaves[b.ID], layout: cached.layout, rect: ui.Rect{X: x, Y: y, W: w, H: total}, x: x + left, y: y + top, number: number}
		if b.Kind == "image" {
			bitmap, found := e.images[b.URL]
			if !found && o.LoadImage != nil {
				bitmap = o.LoadImage(b.URL)
				e.images[b.URL] = bitmap
			}
			row.image = bitmap
			if bitmap != nil {
				bw, bh := bitmap.Size()
				h = min(360, w*float32(bh)/float32(max(1, bw)))
				total = h + 38
				row.rect.H = total
				row.y = y + h + 6
			}
		}
		if b.Kind == "rule" {
			total = 25 + gap
			row.rect.H = total
		}
		if b.Kind == "math" {
			total = e.mathRow(&row, total, h, top, bottom)
			row.rect.H = total
		}
		e.rows = append(e.rows, row)
		if b.Kind == "code" || b.Kind == "raw" || b.Kind == "math" || b.Kind == "quote" || b.Kind == "callout" || b.Kind == "rule" {
			d := decoration{kind: b.Kind, id: b.ID, rect: ui.Rect{X: x, Y: y, W: w, H: total - gap}}
			if b.Kind == "code" {
				d.label, d.id = b.Language, b.ID
			}
			if b.Kind == "callout" {
				d.tone, d.id, d.label = b.Color, b.ID, b.Emoji
			}
			e.decorations = append(e.decorations, d)
		}
		return total
	}
	return blockLayout
}

func (e *Editor) rowAt(at int) *row {
	if len(e.rows) > 0 {
		i := sort.Search(len(e.rows), func(i int) bool { return at <= e.rows[i].leaf.End })
		return &e.rows[min(i, len(e.rows)-1)]
	}
	return nil
}
func (e *Editor) BoundsForRange(r ui.TextInputRange) (ui.Rect, ui.TextInputRange, bool) {
	row := e.rowAt(r.Start)
	if row == nil {
		return ui.Rect{}, r, false
	}
	local := max(0, min(r.Start-row.leaf.Start, row.leaf.End-row.leaf.Start))
	rect := row.layout.Caret(local)
	rect.X += row.x
	rect.Y += row.y
	rect.H = max(20, rect.H)
	return rect, ui.TextInputRange{Start: row.leaf.Start + local, End: row.leaf.Start + local}, true
}
func (e *Editor) IndexForPoint(point ui.Point) (int, bool) {
	if len(e.rows) == 0 {
		return 0, false
	}
	best := &e.rows[0]
	distance := float32(math.MaxFloat32)
	for i := range e.rows {
		r := &e.rows[i]
		dx := max(r.rect.X-point.X, point.X-r.rect.X-r.rect.W, 0)
		dy := max(r.rect.Y-point.Y, point.Y-r.rect.Y-r.rect.H, 0)
		d := dy*dy + dx*dx
		if d < distance {
			best, distance = r, d
		}
	}
	return best.leaf.Start + best.layout.IndexAt(ui.Point{X: point.X - best.x, Y: point.Y - best.y}), true
}

func (e *Editor) paint(p *ui.Painter, bounds ui.Rect, t *ui.Theme, caret bool) {
	offset := func(r ui.Rect) ui.Rect { r.X += bounds.X; r.Y += bounds.Y; return r }
	e.paintHeader(p, bounds)
	for _, d := range e.decorations {
		if d.rect.Y+d.rect.H < e.Scroll.Y-1 || d.rect.Y > e.Scroll.Y+e.viewport+1 {
			continue
		}
		r := offset(d.rect)
		switch d.kind {
		case "code", "raw", "math":
			pg := e.page
			border := pg.CodeBorder
			if d.kind == "code" && (e.hovered(d.rect, 0) || e.langOpen && e.langID == d.id) {
				border = pg.CodeHover
			}
			p.Fill(r, pg.CodeFill, 8)
			p.Stroke(r, border, 8, 1)
			if d.kind == "code" {
				e.paintLanguage(p, offset(languageButton(d)), d, t)
				e.paintLineNumbers(p, bounds, d)
			} else if d.label != "" {
				p.Text(r.X+20, r.Y+10, d.label, 11, pg.Caption)
			}
		case "callout":
			look := e.page.Callouts[d.tone]
			p.Fill(r, look.Fill, 8)
			p.Stroke(r, look.Border, 8, 1)
			icon := offset(calloutIconRect(d))
			if d.id == e.colorID && e.colorOpen || e.hovered(calloutIconRect(d), 0) {
				p.Fill(icon, e.page.Hover, 4)
			}
			if d.label != "" {
				// An emoji icon.
				span := ui.Span{Text: d.label, Size: 18}
				w, h := p.MeasureText(0, span)
				p.RichText(icon.X+(icon.W-w)/2, icon.Y+(icon.H-h)/2, 0, span)
			} else {
				p.Icon(calloutIcon(d.tone), ui.Rect{X: icon.X + 3, Y: icon.Y + 3, W: 18, H: 18}, look.Icon)
			}
		case "quote":
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 2, H: r.H}, e.page.QuoteBar, 1)
		case "rule":
			p.Line(r.X, r.Y+12.5, r.X+r.W, r.Y+12.5, 1, e.page.Rule)
		case "cell":
			if d.header {
				p.Clip(offset(d.table), 8, func() { p.Fill(r, e.page.TableHeader, 0) })
			}
			if fill, ok := e.page.CellFills[d.tone]; ok {
				p.Clip(offset(d.table), 8, func() { p.Fill(r, fill, 0) })
			}
		case "table":
			p.Stroke(r, e.page.TableLine, 8, 1)
		}
	}
	// Cell rules go over every fill, so a filled cell never hides the rule
	// of the merged cell beside it.
	for _, d := range e.decorations {
		if d.kind != "cell" || d.rect.Y+d.rect.H < e.Scroll.Y-1 || d.rect.Y > e.Scroll.Y+e.viewport+1 {
			continue
		}
		r := offset(d.rect)
		if d.right {
			p.Line(r.X+r.W, r.Y, r.X+r.W, r.Y+r.H, 1, e.page.TableLine)
		}
		if d.below {
			p.Line(r.X, r.Y+r.H, r.X+r.W, r.Y+r.H, 1, e.page.TableLine)
		}
	}
	if !e.printing {
		e.paintHoverControls(p, bounds, t)
		e.paintTables(p, bounds)
		e.paintMatches(p, bounds)
	}
	selection := e.Selection().Range
	for _, r := range e.rows {
		if r.rect.Y+r.rect.H < e.Scroll.Y || r.rect.Y > e.Scroll.Y+e.viewport+40 {
			continue
		}
		if r.image != nil {
			p.Image(r.image, offset(ui.Rect{X: r.rect.X, Y: r.rect.Y, W: r.rect.W, H: r.rect.H - 38}), ui.Contain)
		}
		e.paintMarker(p, bounds, r, t)
		if r.leaf.Block.Kind == "image" && r.image == nil {
			p.Text(bounds.X+r.x, bounds.Y+r.y+30, "图片未加载 · "+r.leaf.Block.URL, 12, t.TextMuted)
		}
		if e.focused && selection.End > r.leaf.Start && selection.Start < r.leaf.End {
			local := ui.TextInputRange{Start: max(0, selection.Start-r.leaf.Start), End: min(r.leaf.End, selection.End) - r.leaf.Start}
			for _, rect := range r.layout.SelectionRects(local) {
				rect.X += bounds.X + r.x
				rect.Y += bounds.Y + r.y
				p.Fill(rect, t.Selection, 2)
			}
		}
		if e.paintMath(p, bounds, r, t) {
			continue
		}
		p.TextLayout(r.layout, bounds.X+r.x, bounds.Y+r.y, t.Text)
		if !e.printing {
			e.paintHint(p, bounds, r, t)
		}
		if e.mark != nil && e.mark.End >= r.leaf.Start && e.mark.Start <= r.leaf.End {
			for _, rect := range r.layout.SelectionRects(ui.TextInputRange{Start: max(0, e.mark.Start-r.leaf.Start), End: min(r.leaf.End, e.mark.End) - r.leaf.Start}) {
				p.Line(bounds.X+r.x+rect.X, bounds.Y+r.y+rect.Y+rect.H-2, bounds.X+r.x+rect.X+rect.W, bounds.Y+r.y+rect.Y+rect.H-2, 1, t.Accent)
			}
		}
	}
	e.paintDim(p, bounds, t)
	if e.printing {
		return
	}
	for _, f := range e.foldButtons {
		if !f.folded && f.id != e.hoverID {
			continue
		}
		color := e.page.Caption
		if e.hovered(f.rect, 2) {
			color = t.Text
		}
		icon := iconTriangleDown
		if f.folded {
			icon = iconTriangleRight
		}
		p.Icon(icon, offset(f.rect), color)
	}
	for _, h := range e.handles {
		if h.id != e.hoverID && h.id != e.dragID && !(e.menuOpen && e.menuID == h.id) {
			continue
		}
		if h.rect.Y < e.Scroll.Y || h.rect.Y > e.Scroll.Y+e.viewport {
			continue
		}
		hr := offset(h.rect)
		if h.id == e.dragID || (e.menuOpen && e.menuID == h.id) {
			p.Fill(hr, t.Text.Alpha(.06), 6)
		}
		p.Icon(iconGrip, ui.Rect{X: hr.X + 4, Y: hr.Y + 4, W: 16, H: 16}, t.TextMuted.Alpha(.7))
		if e.dragID == "" {
			pr := offset(h.plus())
			p.Icon(iconPlus, ui.Rect{X: pr.X + 4, Y: pr.Y + 4, W: 16, H: 16}, t.TextMuted.Alpha(.7))
		}
	}
	if e.dragID != "" && e.dropIndex >= 0 && e.dropIndex < len(e.handles) {
		y := bounds.Y + e.handles[e.dropIndex].top - 4
		p.Fill(ui.Rect{X: bounds.X + e.padding, Y: y - 1, W: e.width - 2*e.padding, H: 2}, t.Accent, 1)
	}
	if e.fileOver >= 0 {
		y := bounds.Y + e.fileOver - 4
		p.Fill(ui.Rect{X: bounds.X + e.padding, Y: y - 1, W: e.width - 2*e.padding, H: 2}, t.Accent, 1)
	}
	if len(e.Doc.Blocks) == 1 && e.Doc.Blocks[0].Kind == "paragraph" && e.text == "" && !e.focused {
		// The starter below says the rest.
		p.Text(bounds.X+e.padding, bounds.Y+pageTop+4, "开始写作", 16, t.TextMuted.Alpha(.8))
	}
	if caret && selection.Start == selection.End {
		if r, _, ok := e.BoundsForRange(selection); ok {
			r = offset(r)
			r.W = 1.5
			p.Fill(r, t.Text, 0)
		}
	}
}

// paintHoverControls draws the controls a block shows under the pointer:
// a code block's copy button. Tables draw their bars in paintTables.
func (e *Editor) paintHoverControls(p *ui.Painter, bounds ui.Rect, t *ui.Theme) {
	offset := func(r ui.Rect) ui.Rect { r.X += bounds.X; r.Y += bounds.Y; return r }
	for _, d := range e.decorations {
		switch {
		case d.kind == "code" && d.id != "":
			copied := d.id == e.copiedID && p.Now().Sub(e.copiedAt) < 1500*time.Millisecond
			if !copied && !e.hovered(d.rect, 0) {
				continue
			}
			if copied {
				p.After(1500*time.Millisecond - p.Now().Sub(e.copiedAt))
			}
			// A flat header button: icon and
			// caption text, a fill only under the pointer.
			r := offset(copyButton(d))
			if e.hovered(copyButton(d), 0) {
				p.Fill(r, e.page.Hover, 4)
			}
			icon, label := iconCopy, "复制"
			if copied {
				icon, label = iconCheck, "已复制"
			}
			p.Icon(icon, ui.Rect{X: r.X + 4, Y: r.Y + 3, W: 16, H: 16}, e.page.Caption)
			span := ui.Span{Text: label, Size: 14, Color: e.page.Caption}
			_, h := p.MeasureText(0, span)
			p.RichText(r.X+24, r.Y+(r.H-h)/2, 0, span)
		}
	}
}

// hoverControl runs the hover control under a press, if any.
func (e *Editor) hoverControl(x, y float32) bool {
	in := func(r ui.Rect) bool { return x >= r.X && x <= r.X+r.W && y >= r.Y && y <= r.Y+r.H }
	for _, f := range e.foldButtons {
		if (f.folded || f.id == e.hoverID) && in(ui.Rect{X: f.rect.X - 2, Y: f.rect.Y - 2, W: f.rect.W + 4, H: f.rect.H + 4}) {
			e.toggleFold(f.id)
			return true
		}
	}
	for _, d := range e.decorations {
		switch {
		case d.kind == "callout" && d.id != "" && in(calloutIconRect(d)):
			e.colorOpen = !(e.colorOpen && e.colorID == d.id)
			e.colorID, e.colorAt = d.id, calloutIconRect(d)
			return true
		case d.kind == "code" && d.id != "" && in(languageButton(d)):
			e.langOpen = !(e.langOpen && e.langID == d.id)
			e.langID, e.langAt = d.id, languageButton(d)
			return true
		case d.kind == "code" && d.id != "" && in(copyButton(d)) && e.hovered(d.rect, 0):
			e.copyBlock(d.id)
			return true
		case d.kind == "table" && e.tableControl(d, x, y):
			return true
		}
	}
	return false
}

// copyBlock copies the text of a code block, as its copy button does.
func (e *Editor) copyBlock(id string) {
	for _, leaf := range e.Doc.Leaves() {
		if leaf.Block.ID == id {
			if e.services != (ui.Services{}) {
				e.services.WriteClipboard(leaf.Block.Text())
			}
			e.copiedID, e.copiedAt = id, e.lastInput
			return
		}
	}
}

// paintHint shows what an empty block is for, as Notion does: the type
// of an empty heading, and on the empty line being written, the slash menu.
func (e *Editor) paintHint(p *ui.Painter, bounds ui.Rect, r row, t *ui.Theme) {
	b := r.leaf.Block
	if r.leaf.Start != r.leaf.End || e.mark != nil {
		return
	}
	hint := ""
	size := float32(16)
	switch {
	case b.Kind == "heading":
		size = []float32{26, 22, 20, 18, 16, 16}[max(1, min(6, b.Level))-1]
		if len(e.Doc.Blocks) > 0 && e.Doc.Blocks[0].ID == b.ID {
			size, hint = 34, "请输入标题"
		} else {
			hint = blockLabel(b)
		}
	case e.focused && e.Caret == r.leaf.Start && (b.Kind == "paragraph" || b.Kind == "bullet" || b.Kind == "numbered" || b.Kind == "task"):
		hint = "输入「/」插入内容"
	}
	if hint == "" {
		return
	}
	line := r.layout.Caret(0)
	span := ui.Span{Text: hint, Size: size, Color: e.page.Placeholder}
	if b.Kind == "heading" {
		span.Weight = 500
	}
	_, h := p.MeasureText(0, span)
	p.RichText(bounds.X+r.x, bounds.Y+r.y+line.Y+(max(line.H, h)-h)/2, 0, span)
}

// paintMarker draws the marker of a list item before its first line: a
// dot, its number, or a task's check box.
func (e *Editor) paintMarker(p *ui.Painter, bounds ui.Rect, r row, t *ui.Theme) {
	kind := r.leaf.Block.Kind
	if kind != "bullet" && kind != "numbered" && kind != "task" {
		return
	}
	line := r.layout.Caret(0)
	cy := bounds.Y + r.y + line.Y + max(line.H, 26)/2
	x := bounds.X + r.x
	pg := e.page
	switch kind {
	case "bullet":
		// Nested lists cycle disc, circle and square.
		dot := ui.Rect{X: x - 20, Y: cy - 2.75, W: 5.5, H: 5.5}
		switch r.leaf.Block.Indent % 3 {
		case 0:
			p.Fill(dot, pg.Marker, 2.75)
		case 1:
			p.Stroke(dot, pg.Marker, 2.75, 1)
		default:
			p.Fill(ui.Rect{X: x - 19.75, Y: cy - 2.5, W: 5, H: 5}, pg.Marker, 0)
		}
	case "numbered":
		label := listNumber(max(1, r.number), r.leaf.Block.Indent) + "."
		span := ui.Span{Text: label, Size: 16, Color: pg.Marker, Features: "tnum"}
		w, h := p.MeasureText(0, span)
		p.RichText(x-6-w, cy-h/2, 0, span)
	case "task":
		box := ui.Rect{X: x - 24, Y: cy - 8, W: 16, H: 16}
		p.Fill(box, t.Background, 4)
		if r.leaf.Block.Checked {
			p.Stroke(box, pg.Marker, 4, 1)
			p.Icon(iconCheck, ui.Rect{X: box.X + 2, Y: box.Y + 2, W: 12, H: 12}, pg.Marker)
		} else {
			p.Stroke(box, pg.TaskBorder, 4, 1)
		}
	}
}

// paintLineNumbers numbers a code block's lines in its left 36; a
// wrapped line keeps one number.
func (e *Editor) paintLineNumbers(p *ui.Painter, bounds ui.Rect, d decoration) {
	for _, r := range e.rows {
		if r.leaf.Block.ID != d.id {
			continue
		}
		text := r.leaf.Block.Text()
		at := 0
		for i, line := range strings.Split(text, "\n") {
			caret := r.layout.Caret(at)
			span := ui.Span{Text: fmt.Sprint(i + 1), Font: "monospace", Size: 14, Color: e.page.Caption, Features: "tnum"}
			w, h := p.MeasureText(0, span)
			p.RichText(bounds.X+d.rect.X+36-w, bounds.Y+r.y+caret.Y+(22-h)/2, 0, span)
			at += document.Len(line) + 1
		}
		return
	}
}

// insertAfter opens a new paragraph below block id with the slash menu
// open in it, as Notion's add button does; an empty paragraph is reused.
func (e *Editor) insertAfter(id string) {
	end := -1
	for _, leaf := range e.Doc.Leaves() {
		if leaf.Block.ID == id || leaf.Owner == id {
			end = leaf.End
		}
	}
	if end < 0 {
		return
	}
	e.UnmarkText()
	e.SetSelection(end, end)
	e.Engine.Insert(document.NewBlock("paragraph", "/"))
	_, caret := e.Engine.Positions()
	e.SetSelection(caret+1, caret+1)
	e.slashDismissed = false
	e.RequestFocus, e.reveal = true, true
	e.sync()
}

func (e *Editor) linkAt(at int) string {
	leaf := e.Engine.LeafAt(at)
	offset := leaf.Start
	for _, run := range leaf.Block.Runs {
		offset += document.Len(run.Text)
		if at <= offset {
			return run.Style.Link
		}
	}
	return ""
}
func (e *Editor) toggleTask(at int, x float32) bool {
	r := e.rowAt(at)
	if r == nil || r.leaf.Block.Kind != "task" || x > r.x || x < r.x-28 {
		return false
	}
	e.Engine.ToggleTask(r.leaf.Block.ID)
	return true
}

func (e *Editor) structuralPointer(ev ui.InputEvent) bool {
	if ev.Kind == ui.InputPointerDown && ev.Button == 0 {
		e.closeHeader()
		if e.hoverControl(ev.X, ev.Y) {
			return true
		}
		e.colorOpen, e.langOpen = false, false
		e.tableSel = tableSelection{}
		for _, resize := range e.resizers {
			if math.Abs(float64(ev.X-resize.x)) < 5 && ev.Y >= resize.y && ev.Y <= resize.y+resize.h {
				value := resize
				e.resizing = &value
				e.resizeX = ev.X
				return true
			}
		}
		for _, h := range e.handles {
			if p := h.plus(); ev.X >= p.X && ev.X <= p.X+p.W && ev.Y >= p.Y && ev.Y <= p.Y+p.H {
				e.insertAfter(h.id)
				return true
			}
			if ev.X >= h.rect.X && ev.X <= h.rect.X+h.rect.W && ev.Y >= h.rect.Y && ev.Y <= h.rect.Y+h.rect.H {
				e.dragID, e.dropIndex = h.id, h.index
				return true
			}
		}
	}
	if e.resizing != nil {
		if ev.Kind == ui.InputPointerMove {
			e.resizeX = ev.X
			return true
		}
		if ev.Kind == ui.InputPointerUp {
			r := e.resizing
			widths := append([]float32(nil), r.widths...)
			delta := e.resizeX - r.x
			delta = max(48-widths[r.col], min(delta, widths[r.col+1]-48))
			widths[r.col] += delta
			widths[r.col+1] -= delta
			e.Engine.SetWidths(r.id, widths)
			e.resizing = nil
			return true
		}
	}
	if e.dragID != "" {
		if ev.Kind == ui.InputPointerMove {
			for _, h := range e.handles {
				if ev.Y >= h.top-6 {
					e.dropIndex = h.index
				}
			}
			return true
		}
		if ev.Kind == ui.InputPointerUp {
			old := 0
			for _, h := range e.handles {
				if h.id == e.dragID {
					old = h.index
				}
			}
			if old == e.dropIndex {
				e.menuOpen, e.menuID = true, e.dragID
			} else {
				e.Engine.MoveBlock(e.dragID, e.dropIndex)
			}
			e.dragID = ""
			e.RequestFocus = true
			return true
		}
	}
	return false
}

// fileDropAt is where files dropped at y on the page go: before the
// top-level block whose upper half y is in, or after the last, and the y
// of the line showing it.
func (e *Editor) fileDropAt(y float32) (int, float32) {
	end := e.contentEnd
	for j, h := range e.handles {
		bottom := end
		if j+1 < len(e.handles) {
			bottom = e.handles[j+1].top
		}
		if y < (h.top+bottom)/2 {
			return h.index, h.top
		}
	}
	return len(e.Doc.Blocks), end + 4
}

func blockLabel(b *document.Block) string {
	switch b.Kind {
	case "heading":
		return fmt.Sprintf("标题 %d", b.Level)
	case "code":
		return "代码块"
	case "cell":
		return "表格"
	case "quote":
		return "引用"
	case "task":
		return "任务"
	case "bullet":
		return "无序列表"
	case "numbered":
		return "有序列表"
	case "callout":
		return "高亮块"
	}
	return "正文"
}

func (e *Editor) Focused() bool { return e.focused }

// listNumber writes n as nested ordered lists count: 1, a, i by depth.
func listNumber(n, indent int) string {
	switch indent % 3 {
	case 1:
		out := ""
		for n > 0 {
			n--
			out = string(rune('a'+n%26)) + out
			n /= 26
		}
		return out
	case 2:
		var out strings.Builder
		for _, v := range []struct {
			value int
			digit string
		}{{1000, "m"}, {900, "cm"}, {500, "d"}, {400, "cd"}, {100, "c"}, {90, "xc"}, {50, "l"}, {40, "xl"}, {10, "x"}, {9, "ix"}, {5, "v"}, {4, "iv"}, {1, "i"}} {
			for n >= v.value {
				out.WriteString(v.digit)
				n -= v.value
			}
		}
		return out.String()
	}
	return fmt.Sprint(n)
}

// toggleFold folds or unfolds block id; a caret folded away moves to the
// end of the block that hides it.
func (e *Editor) toggleFold(id string) {
	e.Engine.ToggleFold(id)
	e.sync()
	leaf := e.Engine.LeafAt(e.Caret)
	hidden := e.Doc.HiddenBlocks()
	if hidden[leaf.Owner] == id || hidden[leaf.Block.ID] == id {
		for _, l := range e.Doc.Leaves() {
			if l.Block.ID == id {
				e.SetSelection(l.End, l.End)
			}
		}
	}
}

// revealCaret unfolds a section the caret has moved into, as by arrow keys,
// undo or a search, so the caret is never inside folded text.
func (e *Editor) revealCaret() {
	key := [2]uint64{e.Revision(), uint64(e.Caret)}
	if key == e.revealKey {
		return
	}
	e.revealKey = key
	leaf := e.Engine.LeafAt(e.Caret)
	if e.Engine.Reveal(leaf.Owner) || e.Engine.Reveal(leaf.Block.ID) {
		e.sync()
		e.revealKey = [2]uint64{e.Revision(), uint64(e.Caret)}
	}
}

// ToggleFoldAtCaret folds or unfolds the heading or list item holding the
// caret.
func (e *Editor) ToggleFoldAtCaret() {
	leaf := e.Engine.LeafAt(e.Caret)
	e.toggleFold(leaf.Block.ID)
}

// UnfoldAll opens every folded heading and list item.
func (e *Editor) UnfoldAll() {
	var walk func([]*document.Block)
	walk = func(blocks []*document.Block) {
		for _, b := range blocks {
			if b.Folded {
				e.Engine.ToggleFold(b.ID)
			}
			walk(b.Children)
		}
	}
	walk(e.Doc.Blocks)
	e.sync()
}
