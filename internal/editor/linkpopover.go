package editor

import (
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

type linkSpan struct {
	url        string
	start, end int
}

func linkSpansInLeaf(leaf document.Leaf) []linkSpan {
	if leaf.Block == nil || literalBlock(leaf.Block.Kind) {
		return nil
	}
	var out []linkSpan
	at := leaf.Start
	for _, run := range leaf.Block.Runs {
		n := document.Len(run.Text)
		if link := run.Style.Link; link != "" && n > 0 {
			if k := len(out) - 1; k >= 0 && out[k].end == at && out[k].url == link {
				out[k].end = at + n
			} else {
				out = append(out, linkSpan{url: link, start: at, end: at + n})
			}
		}
		at += n
	}
	return out
}

func (e *Editor) linkSpanAt(at int) (linkSpan, bool) {
	leaf := e.Engine.LeafAt(at)
	for _, s := range linkSpansInLeaf(leaf) {
		if s.start < at && at < s.end || (s.end-s.start == 1 && at == s.start) {
			return s, true
		}
	}
	return linkSpan{}, false
}

func (e *Editor) pointInTextRange(x, y float32, start, end int) bool {
	if start >= end {
		return false
	}
	r := e.rowAt(start)
	if r == nil || r.leaf.Block == nil || r.layout == nil || start < r.leaf.Start || end > r.leaf.End {
		return false
	}
	px, py := x-r.x, y-r.y
	if _, _, _, _, scrollX, noWrap := e.codeScrollFor(*r); noWrap {
		px += scrollX
	}
	for _, rect := range r.layout.SelectionRects(ui.TextInputRange{Start: start - r.leaf.Start, End: end - r.leaf.Start}) {
		if px >= rect.X && px <= rect.X+rect.W && py >= rect.Y && py <= rect.Y+rect.H {
			return true
		}
	}
	return false
}

func (e *Editor) linkAtPoint(x, y float32, strictInterior bool) (linkSpan, bool) {
	at, ok := e.IndexForPoint(ui.Point{X: x, Y: y})
	if !ok {
		return linkSpan{}, false
	}
	leaf := e.Engine.LeafAt(at)
	for _, s := range linkSpansInLeaf(leaf) {
		if !e.pointInTextRange(x, y, s.start, s.end) {
			continue
		}
		if strictInterior && s.end-s.start >= 2 && (at <= s.start || at >= s.end) {
			continue
		}
		return s, true
	}
	return linkSpan{}, false
}

func (e *Editor) openLinkSpan(span linkSpan) {
	if e.ReadOnly || span.url == "" {
		return
	}
	e.UnmarkText()
	e.sync()
	r := ui.TextInputRange{Start: span.start, End: span.end}
	e.linkRange = r
	e.SetSelection(r.Start, r.End)
	e.linkText = e.slice(r.Start, r.End)
	e.linkURL = span.url
	e.linkOpen, e.linkOpened = true, false
	e.linkRevision = e.Revision()
}

func (e *Editor) EditLink() {
	if e.ReadOnly {
		return
	}
	e.UnmarkText()
	e.sync()
	r := e.Selection().Range
	if r.Start == r.End {
		if span, ok := e.linkSpanAt(r.Start); ok {
			e.openLinkSpan(span)
			return
		}
	}
	e.linkRange = r
	e.SetSelection(r.Start, r.End)
	e.linkText = e.slice(r.Start, r.End)
	e.linkURL = e.CurrentStyle().Link
	e.linkOpen, e.linkOpened = true, false
	e.linkRevision = e.Revision()
}

func (e *Editor) applyLink(remove bool) {
	if e.ReadOnly || e.Revision() != e.linkRevision {
		e.linkOpen = false
		return
	}
	r := e.linkRange
	label := e.linkText
	address := strings.TrimSpace(e.linkURL)
	if label == "" {
		label = address
	}
	if !remove && !safeHTMLURL(address) {
		return
	}
	e.Engine.Edit(func() {
		e.SetSelection(r.Start, r.End)
		if !remove && label != e.slice(r.Start, r.End) {
			e.Engine.Replace(r.Start, r.End, label)
			e.SetSelection(r.Start, r.Start+document.Len(label))
		}
		if remove {
			address = ""
		}
		e.Engine.SetLink(address)
	})
	e.sync()
	e.linkOpen = false
	e.RequestFocus = true
}

func (e *Editor) linkPopover(c *ui.Context, surface ui.Element) {
	if !e.linkOpen {
		return
	}
	if e.Revision() != e.linkRevision || c.Shortcut(0, ui.KeyEscape) {
		e.linkOpen = false
		e.RequestFocus = true
		return
	}
	r, _, ok := e.BoundsForRange(e.linkRange)
	if !ok {
		e.linkOpen = false
		return
	}
	b := surface.Bounds()
	w, h := c.Size()
	pw := min(float32(340), w-24)
	x := max(float32(12), min(w-pw-12, b.X+r.X))
	y := b.Y + r.Y + r.H + 8
	if y+205 > h-12 {
		y = max(float32(12), b.Y+r.Y-213)
	}
	ui.Overlay(c, func() {
		back := ui.ButtonBase(c.Key("link-outside")).Absolute().Left(0).Top(0).Size(w, h).KeepFocus().Label(i18n.T("关闭链接编辑"))
		if back.Clicked() {
			e.linkOpen = false
			e.RequestFocus = true
		}
		ui.Column(c.Key("edit-link")).Absolute().Left(x).Top(y).Width(pw).Padding(12).Gap(8).Radius(10).Background(e.page.Popover).Border(1, e.page.MathBorder).Shadow(0, 6, 20, 0, e.page.MathShadow).Transition(popoverTransition).Children(func() {
			ui.Text(c, i18n.T("编辑链接")).FontSize(13).FontWeight(600)
			label := ui.TextInput(c.Key("link-label"), &e.linkText).Label(i18n.T("链接文字")).Placeholder(i18n.T("链接文字"))
			address := ui.TextInput(c.Key("link-address"), &e.linkURL).Label(i18n.T("链接地址")).Placeholder("https://")
			if !e.linkOpened {
				address.Focus()
				e.linkOpened = true
			}
			if !safeHTMLURL(e.linkURL) {
				ui.Text(c, i18n.T("请输入有效的链接地址")).FontSize(12).TextColor(c.Theme().Danger)
			}
			apply := label.Submitted() || address.Submitted()
			ui.Row(c).Gap(6).Children(func() {
				if ui.Button(c, i18n.T("打开链接")).Disabled(e.linkURL == "" || !safeHTMLURL(e.linkURL)).Clicked() && e.openLink != nil {
					e.openLink(e.linkURL)
				}
				if ui.Button(c, i18n.T("移除链接")).Clicked() {
					e.applyLink(true)
				}
				if ui.PrimaryButton(c, i18n.T("应用链接")).Disabled(!safeHTMLURL(e.linkURL)).Clicked() {
					apply = true
				}
			})
			if apply {
				e.applyLink(false)
			}
		})
	})
}

// WikiPreview is a local note excerpt supplied by the host after the hover
// delay. Missing notes have a visible empty state and never cause a write.
type WikiPreview struct {
	Title, Location, Text string
	Missing               bool
}
type wikiHoverState struct {
	left       time.Time
	name       string
	since      time.Time
	open, over bool
	scroll     float32
	at         ui.Rect
	preview    WikiPreview
}

func (e *Editor) wikiPreview(c *ui.Context, surface ui.Element, load func(string) WikiPreview) {
	hover := &e.wikiHover
	if load == nil || e.dragging || e.dragID != "" || e.linkOpen || e.printing || hover.scroll != e.Scroll.Y || hover.open && c.Shortcut(0, ui.KeyEscape) {
		*hover = wikiHoverState{scroll: e.Scroll.Y}
		return
	}
	name := ""
	at := 0
	if e.pointerInside && surface.Hovered() {
		at, _ = e.IndexForPoint(e.pointer)
		name = e.wikiAtPoint(e.pointer.X, e.pointer.Y)
	}
	if name == "" && hover.open {
		if hover.over {
			hover.left = time.Time{}
			name = hover.name
		} else {
			if hover.left.IsZero() {
				hover.left = c.Now()
			}
			if c.Now().Sub(hover.left) < 140*time.Millisecond {
				name = hover.name
				c.After(140*time.Millisecond - c.Now().Sub(hover.left))
			}
		}
	} else {
		hover.left = time.Time{}
	}
	if name != hover.name {
		*hover = wikiHoverState{name: name, since: c.Now(), scroll: e.Scroll.Y}
	}
	if name == "" {
		return
	}
	if !hover.open {
		if c.Now().Sub(hover.since) < 450*time.Millisecond {
			c.After(450*time.Millisecond - c.Now().Sub(hover.since))
			return
		}
		hover.preview = load(name)
		hover.at, _, _ = e.BoundsForRange(ui.TextInputRange{Start: at, End: at})
		hover.open = true
	}
	w, h := c.Size()
	pw := min(float32(320), w-24)
	b := surface.Bounds()
	x := max(float32(12), min(w-pw-12, b.X+hover.at.X))
	y := max(float32(12), min(h-240, b.Y+hover.at.Y+hover.at.H+4))
	ui.Overlay(c, func() {
		panel := ui.Column(c.Key("wiki-preview")).Absolute().Left(x).Top(y).Width(pw).MaxHeight(230).Padding(14).Gap(8).Radius(10).Background(e.page.Popover).Border(1, e.page.MathBorder).Shadow(0, 6, 20, 0, e.page.MathShadow).Transition(popoverTransition).KeepFocus()
		hover.over = panel.Hovered()
		panel.Children(func() {
			p := hover.preview
			ui.Text(c, p.Title).FontSize(15).FontWeight(600).MaxLines(1)
			ui.Text(c, p.Location).FontSize(11).TextColor(c.Theme().TextMuted).MaxLines(1)
			text := p.Text
			if p.Missing {
				text = i18n.T("尚未创建这篇笔记")
			}
			if text == "" {
				text = i18n.T("这篇笔记还是空的")
			}
			ui.Text(c, text).FontSize(13).MaxLines(6)
			if !p.Missing && ui.Button(c, i18n.T("打开笔记")).Clicked() && e.openWiki != nil {
				e.openWiki(name)
				*hover = wikiHoverState{scroll: e.Scroll.Y}
			}
		})
	})
}

func (e *Editor) inlineClick(x, y float32) bool {
	if e.clickInlineMathPopover(x, y) {
		return true
	}
	// A footnote's return target stays 26 DIP high even with small fonts;
	// include its overhang before applying the original exact hit tests.
	var candidates []int
	for i := range e.rowSpace.between(y-26, y+26) {
		candidates = append(candidates, i)
	}
	for _, i := range candidates {
		r := e.ensureRow(i)
		if r.leaf.Block.Kind == "footnote" && x >= r.rect.X && x < r.x && y >= r.y && y < r.y+max(float32(26), 26*e.zoomFactor()) {
			return e.ReturnFootnote()
		}
		for _, o := range r.layout.objects {
			if o.note == "" {
				continue
			}
			p := o.rect
			p.X += r.x
			p.Y += r.y
			if x >= p.X && x <= p.X+p.W && y >= p.Y && y <= p.Y+p.H {
				e.SetSelection(r.leaf.Start+o.start, r.leaf.Start+o.start)
				return e.FollowFootnote()
			}
		}
	}
	return false
}
