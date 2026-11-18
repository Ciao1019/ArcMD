package app

import (
	"math"
	"slices"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo/ui"
)

func toggleDocumentMap(d *document) {
	d.mapInitialized = true
	if d.mindMap == nil {
		d.mindMap = &editor.MindMap{}
	} else {
		d.mindMap = nil
	}
}

func (a *App) toggleDocumentMap(d *document) {
	toggleDocumentMap(d)
	a.prefs.MindMapOpen = d.mindMap != nil
	a.savePrefs()
}

func (a *App) toggleActiveDocumentMap() {
	if a.doc == nil || a.doc.loadErr != nil || a.doc.isWhiteboard() || a.doc.source {
		return
	}
	if a.focus {
		a.focus = false
	}
	a.toggleDocumentMap(a.doc)
}

func mapBranchPalette(d *document, dark bool) []ui.Color {
	look := space.ThemeOf(d.space).Look(dark)
	base := ui.Hex(look.Accent)
	colors := []ui.Color{base}
	for _, hex := range look.Colors {
		col := ui.Hex(hex)
		if !slices.Contains(colors, col) {
			colors = append(colors, col)
		}
	}
	// The same swatches as ArcMD's sidebar document icons, gently
	// harmonized with this space instead of repeating a single hue.
	for _, i := range []int{2, 10, 7, 4, 0, 8, 9, 6} {
		colors = append(colors, docPalette[i].MixOklab(base, .18))
	}
	return colors
}

func snapMapFraction(f, total float32) float32 {
	if total <= 0 {
		return f
	}
	for _, target := range []float32{.42, .50} {
		if math.Abs(float64((f-target)*total)) <= 5 {
			return target
		}
	}
	return f
}

// Use the document's normal surface and full width while the dock is closed.
func (a *App) documentMapWorkspace(c *ui.Context, d *document, width float32) {
	if !d.mapInitialized {
		d.mapInitialized = true
		if a.prefs.MindMapOpen && !(a.split != nil && len(a.split.panes) > 1) {
			d.mindMap = &editor.MindMap{}
		}
	}
	if d.mindMap == nil {
		editor.View(c.Key(d.viewKey()), d.ed, a.editorOptionsFor(c, width, false, d))
		return
	}
	if d.mapFraction == 0 {
		d.mapFraction = .42
	}
	if d.mapVFraction == 0 {
		d.mapVFraction = .44
	}
	onClose := func() { a.toggleDocumentMap(d) }
	closeTip := a.withKey(i18n.T("关闭思维导图"), "mindmap")
	setBoth := func(both bool) {
		a.prefs.MindMapBothSides = both
		a.savePrefs()
	}

	if width < 580 {
		opts := a.editorOptionsFor(c, width, false, d)
		colors := mapBranchPalette(d, opts.Theme.Dark)
		ws := ui.Column(c.Key("document-workspace")).Grow(1).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch).Clip()
		wsH := ws.LayoutBounds().H
		ws.Children(func() {
			ui.Column(c.Key("document-body")).HeightPercent((1 - d.mapVFraction) * 100).MinWidth(0).MinHeight(0).Clip().Children(func() {
				editor.View(c.Key(d.viewKey()), d.ed, opts)
			})
			divider := ui.Box(c.Key("map-divider")).Height(7).WidthPercent(100).Center().Focusable().Role(ui.RoleSplitter).
				Label(i18n.T("调整思维导图高度")).Tooltip(i18n.T("调整思维导图高度")).Cursor(ui.CursorResizeNS)
			active := divider.Hovered() || divider.Pressed()
			ink := c.Theme().Border
			if active {
				ink = c.Theme().Accent.Alpha(.55)
			}
			divider.Children(func() { ui.Box(c).Height(1).WidthPercent(100).Background(ink) })
			divider.DrawOver(func(p *ui.Painter, r ui.Rect) {
				if active {
					p.Fill(ui.Rect{X: r.X + (r.W-28)/2, Y: r.Y + (r.H-3)/2, W: 28, H: 3}, c.Theme().Accent.Alpha(.72), 1.5)
				}
			})
			adjustV := func(dy float32) {
				denom := max(float32(200), wsH)
				next := min(float32(.68), max(float32(.28), d.mapVFraction-dy/denom))
				d.mapVFraction = snapMapFraction(next, denom)
				c.After(0)
			}
			if _, dy, held := divider.Dragged(); held && dy != 0 {
				adjustV(dy)
			}
			if divider.Shortcut(0, ui.KeyUp) {
				adjustV(-12)
			}
			if divider.Shortcut(0, ui.KeyDown) {
				adjustV(12)
			}
			if divider.DoubleClicked() {
				d.mapVFraction = .44
				c.After(0)
			}
			ui.Column(c.Key("map-bottom")).Grow(1).MinWidth(0).MinHeight(0).Clip().Children(func() {
				d.mindMap.View(c.Key("document-map-panel"), d.ed, opts, width, colors, a.prefs.MindMapBothSides, setBoth, closeTip, onClose)
			})
		})
		return
	}

	maxSide := max(float32(240), width-300)
	minSide := min(maxSide, max(float32(240), width*.22))
	side := min(maxSide, max(minSide, width*d.mapFraction))
	opts := a.editorOptionsFor(c, width-side, false, d)
	colors := mapBranchPalette(d, opts.Theme.Dark)
	ui.Row(c.Key("document-workspace")).Grow(1).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch).Clip().Children(func() {
		ui.Column(c.Key("document-body")).Grow(1).MinWidth(0).MinHeight(0).Clip().Children(func() {
			editor.View(c.Key(d.viewKey()), d.ed, opts)
		})
		divider := ui.Box(c.Key("map-divider")).Width(7).HeightPercent(100).Center().Focusable().Role(ui.RoleSplitter).
			Label(i18n.T("调整思维导图宽度")).Tooltip(i18n.T("调整思维导图宽度")).Cursor(ui.CursorResizeEW)
		active := divider.Hovered() || divider.Pressed()
		ink := c.Theme().Border
		if active {
			ink = c.Theme().Accent.Alpha(.55)
		}
		divider.Children(func() { ui.Box(c).Width(1).HeightPercent(100).Background(ink) })
		divider.DrawOver(func(p *ui.Painter, r ui.Rect) {
			if active {
				p.Fill(ui.Rect{X: r.X + (r.W-3)/2, Y: r.Y + (r.H-28)/2, W: 3, H: 28}, c.Theme().Accent.Alpha(.72), 1.5)
			}
		})
		adjust := func(dx float32) {
			next := min(float32(.76), max(float32(.22), d.mapFraction-dx/max(1, width)))
			d.mapFraction = snapMapFraction(next, width)
			c.After(0)
		}
		if dx, _, held := divider.Dragged(); held && dx != 0 {
			adjust(dx)
		}
		if divider.Shortcut(0, ui.KeyLeft) {
			adjust(-12)
		}
		if divider.Shortcut(0, ui.KeyRight) {
			adjust(12)
		}
		if divider.DoubleClicked() {
			d.mapFraction = .42
			c.After(0)
		}
		d.mindMap.View(c.Key("document-map-panel"), d.ed, opts, max(1, side-7), colors, a.prefs.MindMapBothSides, setBoth, closeTip, onClose)
	})
}
