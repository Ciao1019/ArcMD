package app

import (
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// Resting the pointer on a closed folder previews what it holds, as Arc's
// folder preview does (Cra's FolderPreview, MIT, measured on Arc 1.171): a
// dark card right of the sidebar lists every document in it, nested
// folders included, most recently changed first with how long ago, under
// a "搜索…" field that already has the keys; ↑↓ and ↩ pick one, Escape,
// leaving or a click elsewhere closes it. Resting on a document shows the
// same card with a thumbnail of the document, drawn by the editor small.

// Measured on Arc 1.171 (Cra's FolderPreviewMetrics).
const (
	previewW          = 240
	previewMaxH       = 320
	previewCorner     = 10
	previewSearchH    = 48
	previewListInset  = 4
	previewRowPitch   = 45
	previewRowFill    = 44
	previewRowCorner  = 4
	previewSideInset  = 4
	previewIconCenter = 20
	previewTextX      = 36
	previewGap        = 4
	previewWindowGap  = 18
	previewStartScale = 0.85
	// previewThumbW is the width the editor lays a thumbnail out at; the
	// thumbnail is the card's width less its margins.
	previewThumbW = 520
	previewThumbH = 150
)

// The row turns hovered, 0.45 s later the preview fades in over 90 ms,
// growing from 0.85 at the middle of its left edge; it fades out over 80 ms
// once the pointer is elsewhere, crossing to it allowed 60 ms.
const (
	previewOpenDelay  = 450 * time.Millisecond
	previewFadeIn     = 90 * time.Millisecond
	previewFadeOut    = 80 * time.Millisecond
	previewCloseGrace = 60 * time.Millisecond
)

// The kinds of preview.
const (
	previewFolder = iota + 1
	previewDoc
)

// previewTarget is a row the pointer rests on: a folder or a document of a
// space, and where its row was.
type previewTarget struct {
	kind   int
	space  *space.Space
	rel    string
	anchor ui.Rect
}

func (t previewTarget) same(o previewTarget) bool {
	return t.kind == o.kind && t.space == o.space && t.rel == o.rel
}

// previewItem is a document of a folder's preview.
type previewItem struct {
	rel, title string
	changed    time.Time
}

// preview is the preview's state across frames.
type preview struct {
	// hover is the row under the pointer this frame, gathered as the rows
	// are built; resting is the one it has rested on since restingSince.
	hover        *previewTarget
	resting      *previewTarget
	restingSince time.Time
	// shown is the preview open, or fading out until closingAt.
	shown     *previewTarget
	frames    int
	closing   bool
	closingAt time.Time
	leftAt    time.Time
	// The folder's documents, the search and the row picked; extended
	// once typed or arrowed in, when only Escape, a pick or a click
	// elsewhere closes it.
	items    []previewItem
	query    string
	selected int
	extended bool
	// thumbs are the documents the thumbnails draw, by space and path,
	// with the time their file was changed.
	thumbs map[string]thumb
	// refocus gives the editor the keys back once the search field had
	// them.
	refocus bool
	// col are the card's colors this frame, in the space's look.
	col previewLook
}

type thumb struct {
	doc     *document
	changed time.Time
}

// hoverRow tells the preview the pointer is over a row this frame.
func (a *App) hoverRow(kind int, sp *space.Space, rel string, anchor ui.Rect) {
	a.preview.hover = &previewTarget{kind: kind, space: sp, rel: rel, anchor: anchor}
}

// previewView settles the preview after the frame's rows reported the
// pointer, and draws it.
func (a *App) previewView(c *ui.Context) {
	p := &a.preview
	now := c.Now()
	hover := p.hover
	p.hover = nil
	if a.listDragging || a.listDrop != nil || a.favDrop != nil || a.renaming != "" || a.spaceForm.open {
		hover = nil
	}
	// The pointer resting: a new row starts the wait, the same row ends it.
	switch {
	case hover == nil:
		p.resting = nil
	case p.resting == nil || !p.resting.same(*hover):
		p.resting, p.restingSince = hover, now
	default:
		p.resting.anchor = hover.anchor
	}
	open := p.shown != nil && !p.closing
	overPanel := open && a.overPreview(c)
	switch {
	case open && hover != nil && hover.same(*p.shown):
		p.shown.anchor = hover.anchor
		p.leftAt = time.Time{}
	case open && (overPanel || p.extended):
		p.leftAt = time.Time{}
	case open:
		if p.leftAt.IsZero() {
			p.leftAt = now
		}
		if wait := previewCloseGrace - now.Sub(p.leftAt); wait > 0 {
			c.After(wait)
		} else {
			a.closePreview(now)
		}
	}
	if r := p.resting; r != nil && (p.shown == nil || p.closing || !r.same(*p.shown)) && !overPanel {
		if wait := previewOpenDelay - now.Sub(p.restingSince); wait > 0 {
			c.After(wait)
		} else {
			a.openPreview(*r)
		}
	}
	if p.shown == nil {
		return
	}
	if p.closing && now.Sub(p.closingAt) >= previewFadeOut {
		p.shown, p.closing = nil, false
		return
	}
	a.previewCard(c)
}

// overPreview reports whether the pointer is over the preview's card.
func (a *App) overPreview(c *ui.Context) bool {
	x, y, in := c.Root().PointerPosition()
	r := a.previewRect(c)
	return in && x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// openPreview opens the preview of the row rested on.
func (a *App) openPreview(t previewTarget) {
	p := &a.preview
	if t.kind == previewFolder {
		items := a.folderItems(t.space, t.rel)
		if len(items) == 0 {
			return
		}
		p.items = items
	} else if a.thumbOf(t.space, t.rel) == nil {
		return
	}
	if p.shown == nil || p.closing {
		p.frames = 0
		p.refocus = a.doc != nil && a.doc.ed.Focused()
	}
	tt := t
	p.shown, p.closing, p.leftAt = &tt, false, time.Time{}
	p.query, p.selected, p.extended = "", 0, false
}

// closePreview fades the preview out.
func (a *App) closePreview(now time.Time) {
	p := &a.preview
	if p.shown == nil || p.closing {
		return
	}
	p.closing, p.closingAt, p.extended = true, now, false
	if p.refocus && a.doc != nil {
		a.doc.ed.RequestFocus = true
	}
	p.refocus = false
}

// folderItems are the documents under the folder rel of the space, at any
// depth, most recently changed first (Arc sorts by last visit).
func (a *App) folderItems(sp *space.Space, rel string) []previewItem {
	nodes, _ := a.treeOf(sp)
	n := vault.Find(nodes, rel)
	if n == nil {
		return nil
	}
	var out []previewItem
	var walk func([]*vault.Node)
	walk = func(nodes []*vault.Node) {
		for _, k := range nodes {
			if k.Dir {
				walk(k.Children)
				continue
			}
			it := previewItem{rel: k.Path, title: k.Title()}
			if p, err := vault.Resolve(sp.Root, k.Path); err == nil {
				if info, err := os.Stat(p); err == nil {
					it.changed = info.ModTime()
				}
			}
			out = append(out, it)
		}
	}
	walk(n.Children)
	slices.SortStableFunc(out, func(x, y previewItem) int { return y.changed.Compare(x.changed) })
	return out
}

// thumbOf is the document a thumbnail of rel draws, read again when its
// file changed.
func (a *App) thumbOf(sp *space.Space, rel string) *document {
	p := &a.preview
	if a.doc != nil && a.doc.space == sp && a.doc.rel == rel {
		// The open one, as edited, in an editor of its own: one editor
		// takes one place in a frame.
		key := "open/" + sp.ID + "/" + rel
		rev := time.Unix(0, int64(a.doc.ed.Revision()))
		if th, ok := p.thumbs[key]; ok && th.changed.Equal(rev) {
			return th.doc
		}
		text, err := a.doc.ed.Committed().Encode()
		if err != nil {
			return nil
		}
		if p.thumbs == nil {
			p.thumbs = map[string]thumb{}
		}
		d := openDocument(sp, strings.TrimSuffix(rel, path.Ext(rel))+".arcmd", text, false)
		d.rel = rel
		p.thumbs[key] = thumb{d, rev}
		return d
	}
	full, err := vault.Resolve(sp.Root, rel)
	if err != nil {
		return nil
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil
	}
	key := sp.ID + "/" + rel
	if th, ok := p.thumbs[key]; ok && th.changed.Equal(info.ModTime()) {
		return th.doc
	}
	text, err := vault.Read(sp.Root, rel)
	if err != nil {
		return nil
	}
	if p.thumbs == nil {
		p.thumbs = map[string]thumb{}
	}
	d := openDocument(sp, rel, text, false)
	p.thumbs[key] = thumb{d, info.ModTime()}
	return d
}

// previewHeight is the card's height: the search field, a hairline and a
// row of 45 for each document, six and more stopping at 320 and
// scrolling; a thumbnail's card holds it and a line under it.
func (a *App) previewHeight() float32 {
	p := &a.preview
	if p.shown != nil && p.shown.kind == previewDoc {
		return 8 + previewThumbH + previewRowPitch + 4
	}
	return min(previewSearchH+1+previewListInset+float32(max(len(p.items), 1))*previewRowPitch, previewMaxH)
}

// previewRect is where the card goes: right of the sidebar, centered on the
// row, kept 18 DIPs inside the window.
func (a *App) previewRect(c *ui.Context) ui.Rect {
	p := &a.preview
	_, wh := c.Size()
	h := a.previewHeight()
	r := p.shown.anchor
	x := r.X + r.W + rowInset + previewGap
	y := r.Y + r.H/2 - h/2
	y = max(min(y, wh-previewWindowGap-h), previewWindowGap)
	return ui.Rect{X: x, Y: y, W: previewW, H: h}
}

// previewCard draws the card, fading and growing in, fading out.
func (a *App) previewCard(c *ui.Context) {
	p := &a.preview
	r := a.previewRect(c)
	p.col = previewColors(c.Theme(), a.look)
	col := p.col
	ui.Overlay(c, func() {
		card := ui.Column(c.Key("preview")).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).Radius(previewCorner).
			Background(col.fill).Shadow(0, 12, 32, 0, col.shadow).Clip().TransformOrigin(0, 0.5)
		pose := ui.Pose{}
		switch {
		case p.closing:
			pose.Fade = 1
		case p.frames == 0:
			pose = ui.Pose{Scale: previewStartScale, Fade: 1}
		}
		d := previewFadeIn
		if p.closing {
			d = previewFadeOut
		}
		card.AnimatePose(pose, ui.Animation{Duration: d, Ease: ui.EaseOut})
		p.frames++
		if p.frames == 1 || p.closing {
			c.AnimationFrame()
		}
		card.Draw(func(pt *ui.Painter, b ui.Rect) {
			// Two half-DIP rims: an outline, a faint light line inside.
			pt.Stroke(ui.Rect{X: b.X + 0.25, Y: b.Y + 0.25, W: b.W - 0.5, H: b.H - 0.5}, col.outline, previewCorner-0.25, 0.5)
			pt.Stroke(ui.Rect{X: b.X + 0.75, Y: b.Y + 0.75, W: b.W - 1.5, H: b.H - 1.5}, col.rim, previewCorner-0.75, 0.5)
		})
		card.Children(func() {
			if p.shown.kind == previewFolder {
				a.folderPreview(c)
			} else {
				a.docPreview(c)
			}
		})
	})
}

// folderPreview is the folder's card: the search field, a hairline, and
// the documents.
func (a *App) folderPreview(c *ui.Context) {
	p := &a.preview
	t := p.shown
	words := strings.Fields(strings.ToLower(p.query))
	var shown []previewItem
	for _, it := range p.items {
		hay := strings.ToLower(it.title + " " + it.rel)
		ok := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			shown = append(shown, it)
		}
	}
	p.selected = min(max(p.selected, 0), max(len(shown)-1, 0))
	ui.Row(c.Key("search")).Height(previewSearchH).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Width(previewIconCenter - 7)
		ui.Icon(c, iconSearch).FontSize(13).TextColor(p.col.muted)
		ui.Box(c).Width(previewTextX - previewIconCenter - 7)
		in := ui.TextInput(c.Key("preview-query"), &p.query).Grow(1).MinWidth(0).Height(24).Padding(0).
			Placeholder("搜索 "+path.Base(t.rel)+"…").Label("搜索文件夹").TextColor(p.col.title).
			Background(ui.Transparent).Border(0, ui.Transparent).FontSize(13)
		if !p.closing {
			in.AutoFocus()
		}
		if in.Changed() {
			p.extended, p.selected = true, 0
		}
		switch {
		case c.Shortcut(0, ui.KeyDown):
			p.extended, p.selected = true, min(p.selected+1, len(shown)-1)
		case c.Shortcut(0, ui.KeyUp):
			p.extended, p.selected = true, max(p.selected-1, 0)
		case c.Shortcut(0, ui.KeyEscape):
			a.closePreview(c.Now())
		case in.Submitted() && len(shown) > 0:
			a.pickPreview(c, shown[p.selected].rel)
		}
		ui.Box(c).Width(12)
	})
	ui.Box(c).Height(1).Background(p.col.hairline)
	if len(shown) == 0 {
		ui.Text(c, "没有符合搜索的文档 🤔").FontSize(13).TextColor(p.col.muted).MaxWidth(192).
			TextAlign(ui.Center).AlignSelf(ui.Center).Margin(24, 0, 0, 0)
		return
	}
	list := ui.Scroll(c.Key("preview-rows")).Grow(1).MinHeight(0).PaddingY(previewListInset)
	now := time.Now()
	list.Children(func() {
		for i, it := range shown {
			row := ui.Row(c.Key("item:" + it.rel)).Height(previewRowPitch).Shrink(0).PaddingX(previewSideInset).
				Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(it.title)
			on := i == p.selected || row.Hovered()
			row.Children(func() {
				fill := ui.Row(c).Grow(1).Height(previewRowFill).Radius(previewRowCorner).AlignItems(ui.Center).
					AlignSelf(ui.Start)
				if on {
					fill.Background(p.col.row)
				}
				fill.Children(func() {
					ui.Box(c).Width(previewIconCenter - previewSideInset - 8)
					docIcon(c, it.title, 16)
					ui.Box(c).Width(previewTextX - previewIconCenter - 8)
					ui.Column(c).Grow(1).MinWidth(0).PaddingEnd(10).Children(func() {
						ui.Text(c, docName(it.title)).FontSize(13).TextColor(p.col.title).SingleLine()
						ui.Text(c, "修改于"+ago(it.changed, now)).FontSize(11).TextColor(p.col.muted).NoWrap()
					})
				})
			})
			if i == p.selected {
				row.ScrollIntoView()
			}
			row.ContextMenu(func(m *ui.Menu) {
				p.extended = true
				if m.Item("打开").Chosen() {
					a.pickPreview(c, it.rel)
				}
				if m.Item("在访达中显示").Chosen() {
					a.revealIn(t.space, it.rel)
				}
			})
			if row.Clicked() {
				a.pickPreview(c, it.rel)
			}
		}
	})
}

// pickPreview opens a document of the folder's preview, closing it.
func (a *App) pickPreview(c *ui.Context, rel string) {
	sp := a.preview.shown.space
	a.preview.refocus = false
	a.closePreview(c.Now())
	if sp == a.state.ActiveSpace() {
		a.open(rel)
	} else {
		a.openIn(sp, rel)
	}
	if a.doc != nil {
		a.doc.ed.RequestFocus = true
	}
}

// docPreview is a document's card: its thumbnail on the paper, its name
// and when it changed.
func (a *App) docPreview(c *ui.Context) {
	p := &a.preview
	t := p.shown
	d := a.thumbOf(t.space, t.rel)
	if d == nil {
		return
	}
	const thumbW = previewW - 16
	scale := float32(thumbW) / previewThumbW
	dt := documentTheme(a.paperDark)
	paper := ui.Box(c.Key("paper")).Size(thumbW, previewThumbH).Margin(8, 8, 0, 8).Radius(6).Background(dt.Background).Clip()
	if dt.Dark {
		// Dark paper on a dark card needs an edge to read as a page.
		paper.Border(1, dt.Border)
	}
	paper.Children(func() {
		page := ui.Themed(c.Key("thumb:"+t.space.ID+"/"+t.rel), dt).Absolute().Left(0).Top(0).
			Size(previewThumbW, previewThumbH/scale).TransformOrigin(0, 0).Scale(scale, scale).Disabled(true)
		page.Children(func() {
			editor.View(c.Key("thumb-editor"), d.ed, editor.Options{
				Width:     previewThumbW,
				Theme:     dt,
				Page:      pageStyle(a.paperDark),
				LoadImage: d.loadImage,
			})
		})
	})
	title := vault.Title(t.rel)
	ui.Row(c.Key("thumb-meta")).Height(previewRowPitch).PaddingX(previewSideInset).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Width(previewIconCenter - previewSideInset - 8)
		docIcon(c, title, 16)
		ui.Box(c).Width(previewTextX - previewIconCenter - 8)
		ui.Column(c).Grow(1).MinWidth(0).PaddingEnd(10).Children(func() {
			ui.Text(c, docName(title)).FontSize(13).TextColor(p.col.title).SingleLine()
			where := t.space.Name
			if dir := parentDir(t.rel); dir != "" {
				where += " / " + dir
			}
			if full, err := vault.Resolve(t.space.Root, t.rel); err == nil {
				if info, err := os.Stat(full); err == nil {
					where += " · 修改于" + ago(info.ModTime(), time.Now())
				}
			}
			ui.Text(c, where).FontSize(11).TextColor(p.col.muted).SingleLine()
		})
	})
}

// ago is how long ago t was, as Arc words it: "刚刚", "3 小时前".
func ago(t, now time.Time) string {
	if t.IsZero() {
		return "未知时间"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf(" %d 分钟前", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf(" %d 小时前", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf(" %d 天前", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf(" %d 周前", int(d.Hours()/24/7))
	case d < 365*24*time.Hour:
		return fmt.Sprintf(" %d 个月前", int(d.Hours()/24/30))
	}
	return fmt.Sprintf(" %d 年前", int(d.Hours()/24/365))
}
