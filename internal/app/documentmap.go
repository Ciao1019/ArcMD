package app

import (
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo/ui"
	"slices"
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

// Use the document's normal surface and full width while the dock is closed.
func (a *App) documentMapWorkspace(c *ui.Context, d *document, width float32) {
	if !d.mapInitialized {
		d.mapInitialized = true
		if a.prefs.MindMapOpen {
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
	side := max(1, width*d.mapFraction)
	opts := a.editorOptionsFor(c, width-side, false, d)
	ui.Row(c.Key("document-workspace")).Grow(1).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch).Clip().Children(func() {
		ui.Column(c.Key("document-body")).Grow(1).MinWidth(0).MinHeight(0).Clip().Children(func() {
			editor.View(c.Key(d.viewKey()), d.ed, opts)
		})
		divider := ui.Box(c.Key("map-divider")).Width(7).HeightPercent(100).Center().Focusable().Role(ui.RoleSplitter).
			Label(i18n.T("调整思维导图宽度")).Tooltip(i18n.T("调整思维导图宽度")).Cursor(ui.CursorResizeEW)
		ink := c.Theme().Border
		if divider.Hovered() || divider.Pressed() {
			ink = c.Theme().Accent.Alpha(.55)
		}
		divider.Children(func() { ui.Box(c).Width(1).HeightPercent(100).Background(ink) })
		adjust := func(dx float32) {
			d.mapFraction = min(float32(.78), max(float32(.22), d.mapFraction-dx/max(1, width)))
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
		look := space.ThemeOf(d.space).Look(opts.Theme.Dark)
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
		d.mindMap.View(c.Key("document-map-panel"), d.ed, opts, max(1, side-7), colors, a.prefs.MindMapBothSides, func(both bool) {
			a.prefs.MindMapBothSides = both
			a.savePrefs()
		})
	})
}
