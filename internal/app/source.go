package app

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

const sourcePreviewDebounce = 120 * time.Millisecond

// sourceFindState holds the inline find & replace state of Markdown source mode.
type sourceFindState struct {
	open        bool
	replace     bool
	query       string
	with        string
	matches     [][2]int // rune offsets [start, end) into d.preview
	current     int
	cacheText   string
	cacheQuery  string
	focusQuery  bool
	jumpPending bool
	replaceOne  bool
	replaceAll  bool
}

func (f *sourceFindState) refresh(text string) {
	if f.cacheText == text && f.cacheQuery == f.query {
		return
	}
	f.cacheText = text
	f.cacheQuery = f.query
	f.matches = findSourceMatches(text, f.query)
	if len(f.matches) == 0 {
		f.current = 0
		return
	}
	if f.current < 0 || f.current >= len(f.matches) {
		f.current = 0
	}
}

func (f *sourceFindState) step(delta int) {
	if len(f.matches) == 0 {
		return
	}
	n := len(f.matches)
	f.current = (f.current + delta%n + n) % n
	f.jumpPending = true
}

func findSourceMatches(text, query string) [][2]int {
	if query == "" || text == "" {
		return nil
	}
	q := []rune(strings.Map(unicode.ToLower, query))
	if len(q) == 0 {
		return nil
	}
	// KMP over lowercased runes so repeated prefixes stay O(n).
	fail := make([]int, len(q))
	for i, j := 1, 0; i < len(q); i++ {
		for j > 0 && q[i] != q[j] {
			j = fail[j-1]
		}
		if q[i] == q[j] {
			j++
		}
		fail[i] = j
	}
	var out [][2]int
	matched := 0
	runeIdx := 0
	for _, r := range text {
		lr := unicode.ToLower(r)
		for matched > 0 && lr != q[matched] {
			matched = fail[matched-1]
		}
		if lr == q[matched] {
			matched++
		}
		if matched == len(q) {
			out = append(out, [2]int{runeIdx - len(q) + 1, runeIdx + 1})
			matched = 0
		}
		runeIdx++
	}
	return out
}

func (a *App) openSourceFind(d *document, replace bool) {
	if d == nil {
		return
	}
	f := &d.sourceFind
	if d.ed != nil && d.ed.ReadOnly {
		replace = false
		f.replace = false
	}
	if sel := runeSlice(d.preview, d.sourceSelStart, d.sourceSelEnd); sel != "" && !strings.ContainsAny(sel, "\n\r") && utf8.RuneCountInString(sel) <= 80 {
		f.query = sel
	}
	f.open = true
	f.replace = replace || (f.replace && f.open)
	f.focusQuery = true
	f.cacheQuery = ""
	f.refresh(d.preview)
	if len(f.matches) > 0 {
		// Jump to first match at or after caret.
		f.current = 0
		for i, m := range f.matches {
			if m[0] >= d.sourceCaret {
				f.current = i
				break
			}
		}
		f.jumpPending = true
	}
}

func (a *App) closeSourceFind(d *document) {
	if d == nil {
		return
	}
	d.sourceFind.open = false
	d.sourceFocusPending = true
}

func (a *App) toggleSourceSplit(d *document) {
	if d == nil || !d.source {
		return
	}
	d.sourceSplit = !d.sourceSplit
	if !d.sourceSplit {
		d.releaseSourcePreview()
	} else {
		d.sourcePreviewLine = 0
	}
}

func (a *App) swapSourceSplit() {
	if a.prefs == nil {
		return
	}
	a.prefs.SourceSplitSwapped = !a.prefs.SourceSplitSwapped
	a.savePrefs()
}

func (a *App) sourceSplitSwapped() bool {
	return a.prefs != nil && a.prefs.SourceSplitSwapped
}

func snapSourceSplitFraction(f, total float32) float32 {
	if total <= 0 {
		return f
	}
	if math.Abs(float64((f-0.50)*total)) <= 5 {
		return 0.50
	}
	return f
}

// sourceView renders the editable Markdown source editor for d: conflict bars,
// quiet header strip, optional inline find/replace bar, and either the single
// source pane (default) or a resizable side-by-side rendered preview + source split.
func (a *App) sourceView(c *ui.Context, d *document, cardWidth float32) ui.Element {
	d.ensureSourceInit()
	t := c.Theme()
	pg := a.pageStyle(t.Dark)
	active := d == a.doc || a.inWindow(d)

	if active && !a.modalBlocksSidebarKeys() {
		switch {
		case a.pressed(c, "find"):
			a.openSourceFind(d, false)
		case a.pressed(c, "replace"):
			a.openSourceFind(d, true)
		}
	}

	box := ui.Column(c.Key("source-page:" + d.viewKey())).Grow(1).MinHeight(0)
	box.Children(func() {
		a.conflictBarFor(c, d)
		a.syncConflictBarFor(c, d)
		a.sourceHeaderBar(c, d, pg, cardWidth)
		if d.sourceFind.open {
			a.sourceFindBar(c, d, pg)
		}
		a.sourceWorkspace(c, d, pg, cardWidth)
	})
	return box
}

func (a *App) sourceHeaderBar(c *ui.Context, d *document, pg editor.Page, cardWidth float32) {
	t := c.Theme()
	line, col := lineColAtRune(d.preview, d.sourceCaret)
	totalLines := 1 + strings.Count(d.preview, "\n")
	totalChars := utf8.RuneCountInString(d.preview)
	compact := cardWidth > 0 && cardWidth < 680

	ui.Row(c.Key("source-header")).Height(40).Padding(0, 14, 0, 16).Gap(6).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border.Alpha(0.55)).Children(func() {
		ui.Icon(c, iconCode).FontSize(14).TextColor(t.TextMuted)
		ui.Text(c, i18n.T("Markdown 源码")).FontSize(13).FontWeight(500).TextColor(t.Text).NoWrap()
		if d.ed.ReadOnly {
			ui.Box(c).Padding(2, 6).Radius(4).Background(t.Text.Alpha(0.06)).Children(func() {
				ui.Text(c, i18n.T("只读")).FontSize(11).TextColor(t.TextMuted).NoWrap()
			})
		}
		if !compact {
			ui.Box(c).Width(4)
			stats := fmt.Sprintf(i18n.T("第 %d 行 · 第 %d 列 · %d 行 · %d 字"), line, col, totalLines, totalChars)
			ui.Text(c, stats).FontSize(11.5).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
		}
		ui.Spacer(c)

		splitTip := i18n.T("开启分栏预览")
		if d.sourceSplit {
			splitTip = i18n.T("关闭分栏预览")
		}
		if sourceBarToggleBtn(c, "source-split-btn", iconColumns2, i18n.T("分栏预览"), splitTip, d.sourceSplit, compact, pg).Clicked() {
			a.activateDocument(d)
			a.toggleSourceSplit(d)
		}
		if d.sourceSplit {
			if sourceBarToggleBtn(c, "source-swap-btn", iconArrowLeftRight, i18n.T("对调左右"), i18n.T("对调左右分栏"), false, compact, pg).Clicked() {
				a.activateDocument(d)
				a.swapSourceSplit()
			}
		}

		findBtn := sourceBarIconBtn(c, "source-find-btn", iconSearch, i18n.T("查找和替换"), a.withKey(i18n.T("查找"), "find"), d.sourceFind.open, pg)
		if findBtn.Clicked() {
			a.activateDocument(d)
			if d.sourceFind.open {
				a.closeSourceFind(d)
			} else {
				a.openSourceFind(d, false)
			}
		}

		lockIcon := iconUnlock
		lockLabel := i18n.T("锁定文档（只读）")
		if d.ed.ReadOnly {
			lockIcon = iconLock
			lockLabel = i18n.T("解除只读")
		}
		lockBtn := sourceBarIconBtn(c, "source-lock-btn", lockIcon, lockLabel, lockLabel, d.ed.ReadOnly, pg)
		if lockBtn.Clicked() {
			a.activateDocument(d)
			d.ed.SetReadOnly(!d.ed.ReadOnly)
			if d.ed.ReadOnly {
				d.sourceFind.replace = false
			}
		}

		if sourceBarTextBtn(c, "source-export-btn", i18n.T("导出 Markdown 文件"), "", false, pg).Clicked() {
			a.activateDocument(d)
			a.exportMarkdown()
		}
		if sourceBarTextBtn(c, "source-return-btn", i18n.T("返回富文本"), a.withKey(i18n.T("返回富文本编辑"), "source"), true, pg).Clicked() {
			a.activateDocument(d)
			d.toggleMode()
		}
	})
}

func (a *App) syncSourcePreview(c *ui.Context, d *document) {
	if !d.sourceSplit {
		return
	}
	now := c.Now()
	updated := false
	if d.sourcePreviewEd == nil {
		doc := editor.ImportMarkdown(d.preview)
		ed := editor.FromOwnedDocument(doc)
		ed.SetReadOnly(true)
		d.sourcePreviewEd = ed
		d.sourcePreviewText = d.preview
		d.sourcePreviewDirty = false
		d.sourcePreviewAt = time.Time{}
		updated = true
	} else if d.preview != d.sourcePreviewText {
		if !d.sourcePreviewDirty {
			d.sourcePreviewDirty = true
			d.sourcePreviewAt = now
		}
		if wait := sourcePreviewDebounce - now.Sub(d.sourcePreviewAt); wait > 0 {
			c.After(wait)
		} else {
			prevScroll := d.sourcePreviewEd.Scroll
			nextDoc := editor.ImportMarkdown(d.preview)
			d.sourcePreviewEd.SetReadOnly(false)
			d.sourcePreviewEd.Engine.ReplaceDocument(nextDoc)
			d.sourcePreviewEd.SetReadOnly(true)
			d.sourcePreviewEd.Scroll = prevScroll
			d.sourcePreviewText = d.preview
			d.sourcePreviewDirty = false
			d.sourcePreviewAt = time.Time{}
			updated = true
		}
	} else {
		d.sourcePreviewDirty = false
	}

	if d.sourcePreviewEd != nil {
		curLine, _ := lineColAtRune(d.preview, d.sourceCaret)
		if updated || curLine != d.sourcePreviewLine {
			d.sourcePreviewLine = curLine
			if d.sourceCaret <= 0 || curLine <= 1 {
				d.sourcePreviewEd.Scroll.Y = 0
			} else if id, first := markdownBlockAtRuneOffsetWithFirst(d.sourcePreviewEd.Doc, d.sourcePreviewText, d.sourceCaret); id != "" {
				if first && d.sourcePreviewEd.Doc.Frontmatter != "" {
					d.sourcePreviewEd.Scroll.Y = 0
				} else {
					d.sourcePreviewEd.ScrollToBlock(id)
				}
			}
		}
	}
}

func (a *App) sourcePreviewOptions(c *ui.Context, d *document, width float32) editor.Options {
	opts := a.editorOptionsFor(c, width, false, d)
	opts.Width = width
	opts.Outline = false
	opts.SetOutline = nil
	opts.Footer = nil
	opts.Typewriter = false
	opts.Bookmarks = nil
	opts.ToggleBookmark = nil
	opts.LabelBookmark = nil
	return opts
}

func (a *App) sourceWorkspace(c *ui.Context, d *document, pg editor.Page, cardWidth float32) {
	if !d.sourceSplit {
		a.sourceEditorBody(c, d, pg)
		return
	}
	a.syncSourcePreview(c, d)
	if d.sourceSplitFrac == 0 {
		d.sourceSplitFrac = 0.50
	}
	if d.sourceSplitVFrac == 0 {
		d.sourceSplitVFrac = 0.50
	}
	swapped := a.sourceSplitSwapped()

	renderPreview := func(w float32) {
		ui.Column(c.Key("source-preview-pane:" + d.viewKey())).Grow(1).MinWidth(0).MinHeight(0).Clip().Children(func() {
			if d.sourcePreviewEd != nil {
				editor.View(c.Key("source-preview:"+d.viewKey()), d.sourcePreviewEd, a.sourcePreviewOptions(c, d, w))
			}
		})
	}
	renderSource := func() {
		ui.Column(c.Key("source-editor-pane:" + d.viewKey())).Grow(1).MinWidth(0).MinHeight(0).Clip().Children(func() {
			a.sourceEditorBody(c, d, pg)
		})
	}

	if cardWidth < 560 {
		ws := ui.Column(c.Key("source-split-workspace")).Grow(1).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch).Clip()
		wsH := ws.LayoutBounds().H
		ws.Children(func() {
			topPane := ui.Column(c.Key("source-split-top")).HeightPercent(d.sourceSplitVFrac * 100).MinWidth(0).MinHeight(0).Clip()
			topPane.Children(func() {
				if swapped {
					renderSource()
				} else {
					renderPreview(max(1, cardWidth))
				}
			})
			divider := ui.Box(c.Key("source-split-divider-v")).Height(7).WidthPercent(100).Center().Focusable().Role(ui.RoleSplitter).
				Label(i18n.T("调整分栏大小")).Tooltip(i18n.T("拖动调整大小；双击均分；方向键微调")).Cursor(ui.CursorResizeNS)
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
				next := min(float32(.75), max(float32(.25), d.sourceSplitVFrac+dy/denom))
				d.sourceSplitVFrac = snapSourceSplitFraction(next, denom)
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
				d.sourceSplitVFrac = 0.50
				c.After(0)
			}
			botPane := ui.Column(c.Key("source-split-bottom")).Grow(1).MinWidth(0).MinHeight(0).Clip()
			botPane.Children(func() {
				if swapped {
					renderPreview(max(1, cardWidth))
				} else {
					renderSource()
				}
			})
		})
		return
	}

	avail := max(float32(200), cardWidth-7)
	minLeft := min(avail/2, max(float32(220), avail*0.25))
	maxLeft := max(minLeft, avail-minLeft)
	leftW := min(maxLeft, max(minLeft, avail*d.sourceSplitFrac))
	rightW := max(float32(1), avail-leftW)

	ui.Row(c.Key("source-split-workspace")).Grow(1).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch).Clip().Children(func() {
		ui.Column(c.Key("source-split-left")).Width(leftW).Shrink(0).MinHeight(0).Clip().Children(func() {
			if swapped {
				renderSource()
			} else {
				renderPreview(leftW)
			}
		})
		divider := ui.Box(c.Key("source-split-divider-h")).Width(7).HeightPercent(100).Center().Focusable().Role(ui.RoleSplitter).
			Label(i18n.T("调整分栏大小")).Tooltip(i18n.T("拖动调整大小；双击均分；方向键微调")).Cursor(ui.CursorResizeEW)
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
		adjustH := func(dx float32) {
			next := min(float32(.75), max(float32(.25), d.sourceSplitFrac+dx/avail))
			d.sourceSplitFrac = snapSourceSplitFraction(next, avail)
			c.After(0)
		}
		if dx, _, held := divider.Dragged(); held && dx != 0 {
			adjustH(dx)
		}
		if divider.Shortcut(0, ui.KeyLeft) {
			adjustH(-12)
		}
		if divider.Shortcut(0, ui.KeyRight) {
			adjustH(12)
		}
		if divider.DoubleClicked() {
			d.sourceSplitFrac = 0.50
			c.After(0)
		}
		ui.Column(c.Key("source-split-right")).Grow(1).MinWidth(0).MinHeight(0).Clip().Children(func() {
			if swapped {
				renderPreview(rightW)
			} else {
				renderSource()
			}
		})
	})
}

func sourceBarIconBtn(c *ui.Context, key string, icon *ui.SVG, label, tip string, active bool, pg editor.Page) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Size(26, 26).Radius(6).Center().
		Cursor(ui.CursorPointer).KeepFocus().Label(label).Tooltip(tip)
	ink := t.TextMuted
	switch {
	case active:
		ink = t.Accent
		b.Background(t.Accent.Alpha(0.14))
	case b.Hovered():
		ink = t.Text
		b.Background(pg.Hover)
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(14).TextColor(ink) })
	return b
}

func sourceBarToggleBtn(c *ui.Context, key string, icon *ui.SVG, label, tip string, active, compact bool, pg editor.Page) ui.Element {
	if compact {
		return sourceBarIconBtn(c, key, icon, label, tip, active, pg)
	}
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Height(26).PaddingX(8).Gap(5).Radius(6).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).KeepFocus().Label(label).Tooltip(tip)
	ink := t.TextMuted
	switch {
	case active:
		ink = t.Accent
		if b.Hovered() {
			b.Background(t.Accent.Alpha(0.20))
		} else {
			b.Background(t.Accent.Alpha(0.12))
		}
	case b.Hovered():
		ink = t.Text
		b.Background(pg.Hover)
	}
	b.Children(func() {
		ui.Icon(c, icon).FontSize(13.5).TextColor(ink)
		ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(ink).NoWrap()
	})
	return b
}

func sourceBarTextBtn(c *ui.Context, key, label, tip string, accent bool, pg editor.Page) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Height(26).PaddingX(9).Radius(6).Center().
		Cursor(ui.CursorPointer).KeepFocus().Label(label)
	if tip != "" {
		b.Tooltip(tip)
	}
	ink := t.TextMuted
	switch {
	case accent:
		ink = t.Accent
		if b.Hovered() {
			b.Background(t.Accent.Alpha(0.20))
		} else {
			b.Background(t.Accent.Alpha(0.12))
		}
	default:
		if b.Hovered() {
			ink = t.Text
			b.Background(pg.Hover)
		}
	}
	b.Children(func() { ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(ink).NoWrap() })
	return b
}

func (a *App) sourceFindBar(c *ui.Context, d *document, pg editor.Page) {
	t := c.Theme()
	f := &d.sourceFind
	f.refresh(d.preview)

	ui.Column(c.Key("source-find-bar")).Padding(8, 14).Gap(6).
		Background(t.Text.Alpha(0.025)).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border.Alpha(0.55)).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			if !d.ed.ReadOnly {
				chev := iconChevronRight
				if f.replace {
					chev = iconChevronDown
				}
				toggle := sourceBarIconBtn(c, "source-find-replace-toggle", chev, i18n.T("展开替换"), i18n.T("展开替换"), f.replace, pg)
				if toggle.Clicked() {
					f.replace = !f.replace
				}
			}
			in := ui.TextInput(c.Key("source-find-query"), &f.query).Width(240).Height(28).
				Placeholder(i18n.T("在源码中查找…")).Label(i18n.T("查找")).FontSize(12.5)
			if f.focusQuery {
				in.Focus()
				f.focusQuery = false
			}
			if in.Changed() {
				f.cacheQuery = ""
				f.refresh(d.preview)
				if len(f.matches) > 0 {
					f.current = 0
					f.jumpPending = true
				}
			}
			if in.Shortcut(ui.Shift, ui.KeyEnter) {
				f.step(-1)
			} else if in.Submitted() {
				f.step(1)
			}
			if in.Shortcut(0, ui.KeyEscape) {
				a.closeSourceFind(d)
			}

			countText := ""
			if f.query != "" {
				if len(f.matches) == 0 {
					countText = i18n.T("无匹配")
				} else {
					countText = fmt.Sprintf("%d / %d", f.current+1, len(f.matches))
				}
			}
			if countText != "" {
				col := t.TextMuted
				if len(f.matches) == 0 {
					col = t.Danger
				}
				ui.Text(c, countText).FontSize(12).TextColor(col).NoWrap().MinWidth(44)
			}

			if sourceBarIconBtn(c, "source-find-prev", iconChevronUp, i18n.T("上一个 (⇧Enter)"), i18n.T("上一个 (⇧Enter)"), false, pg).Clicked() {
				f.step(-1)
			}
			if sourceBarIconBtn(c, "source-find-next", iconChevronDown, i18n.T("下一个 (Enter)"), i18n.T("下一个 (Enter)"), false, pg).Clicked() {
				f.step(1)
			}
			ui.Spacer(c)
			if sourceBarIconBtn(c, "source-find-close", iconX, i18n.T("关闭查找"), i18n.T("关闭查找"), false, pg).Clicked() {
				a.closeSourceFind(d)
			}
		})
		if f.replace && !d.ed.ReadOnly {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Width(26)
				rin := ui.TextInput(c.Key("source-find-with"), &f.with).Width(240).Height(28).
					Placeholder(i18n.T("替换为…")).Label(i18n.T("替换为")).FontSize(12.5)
				if rin.Submitted() && len(f.matches) > 0 {
					f.replaceOne = true
				}
				if rin.Shortcut(0, ui.KeyEscape) {
					a.closeSourceFind(d)
				}
				if sourceBarTextBtn(c, "source-replace-one", i18n.T("替换"), "", false, pg).Disabled(len(f.matches) == 0).Clicked() {
					f.replaceOne = true
				}
				if sourceBarTextBtn(c, "source-replace-all", i18n.T("全部替换"), "", false, pg).Disabled(len(f.matches) == 0).Clicked() {
					f.replaceAll = true
				}
			})
		}
	})
}

func (a *App) sourceEditorBody(c *ui.Context, d *document, pg editor.Page) {
	t := c.Theme()
	fontSize := max(float32(12), a.prefs.ReadingSize()-2)
	const (
		lineHeightMult = float32(1.65)
		padTop         = float32(14)
		padRight       = float32(24)
		padBottom      = float32(40)
		padLeft        = float32(16)
	)
	totalLines := max(1, 1+strings.Count(d.preview, "\n"))
	digits := max(2, len(strconv.Itoa(totalLines)))
	gutterW := float32(digits)*8.5 + 22
	curLine, _ := lineColAtRune(d.preview, d.sourceCaret)
	_, winH := c.Size()
	if winH <= 0 {
		winH = 800
	}

	ui.Row(c.Key("source-body")).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		// Synchronized line-number gutter painted directly from d.sourceLines & d.previewScroll.
		gutter := ui.Box(c.Key("source-gutter")).Width(gutterW).Shrink(0).
			BorderWidth(0, 1, 0, 0).BorderColor(t.Border.Alpha(0.45)).Clip()
		gutter.Draw(func(p *ui.Painter, b ui.Rect) {
			lines := &d.sourceLines
			count := lines.Count()
			if count <= 0 {
				count = totalLines
			}
			scrollY := d.previewScroll.Y
			first := max(0, lines.At(max(0, scrollY-padTop))-1)
			last := min(count-1, lines.At(max(0, scrollY-padTop+b.H))+1)
			numSize := max(float32(11), fontSize-1.5)
			numFont := ui.Font{Family: "monospace", Size: numSize, LineHeight: lineHeightMult}
			activeFont := ui.Font{Family: "monospace", Size: numSize, Weight: 500, LineHeight: lineHeightMult}
			lineH := fontSize * lineHeightMult
			yOffset := (lineH - numSize*lineHeightMult) * 0.5
			mutedColor := t.TextMuted.Alpha(0.6)
			activeColor := t.Text
			for i := first; i <= last; i++ {
				y := b.Y + padTop + lines.Top(i) - scrollY
				if y+lineH < b.Y || y > b.Y+b.H {
					continue
				}
				fnt := numFont
				col := mutedColor
				if i+1 == curLine {
					fnt = activeFont
					col = activeColor
				}
				tl := ui.ShapeText(strconv.Itoa(i+1), fnt, 0)
				tw, _ := tl.Size()
				x := b.X + b.W - 11 - float32(math.Ceil(float64(tw)))
				p.TextLayout(tl, x, y+yOffset, col)
			}
		})

		ta := ui.TextAreaBase(c.Key("source-editor:"+d.viewKey()), &d.preview).
			Grow(1).MinWidth(0).MinHeight(0).
			Font("monospace").FontSize(fontSize).LineHeight(lineHeightMult).
			TextColor(pg.CodeText).Background(ui.Transparent).Border(0, ui.Transparent).
			Padding(padTop, padRight, padBottom, padLeft).FocusRing(false).
			ReadOnly(d.ed.ReadOnly).
			TrackScroll(&d.previewScroll).
			TrackLines(&d.sourceLines).
			Label(i18n.T("Markdown 源码编辑器"))

		ta.OnTextKey(func(k ui.TextKey) bool {
			if d.ed.ReadOnly {
				return false
			}
			if k.Key == ui.KeyTab && (k.Mods == 0 || k.Mods == ui.Shift) {
				if k.Mods == ui.Shift {
					d.sourceTabOp = -1
				} else {
					d.sourceTabOp = 1
				}
				return true
			}
			if k.Key == ui.KeyEscape && k.Mods == 0 && d.sourceFind.open {
				d.sourceFind.open = false
				return true
			}
			return false
		})

		if d.sourceTabOp != 0 && !d.ed.ReadOnly {
			op := d.sourceTabOp
			d.sourceTabOp = 0
			start, end := ta.TextSelection()
			editStart, editEnd, rep, newStart, newEnd, ok := applySourceIndent(d.preview, start, end, op > 0)
			if ok {
				ta.ReplaceText(editStart, editEnd, rep)
				ta.SetTextSelection(newStart, newEnd)
			}
		}

		f := &d.sourceFind
		if f.open {
			f.refresh(d.preview)
			if f.replaceOne && !d.ed.ReadOnly {
				f.replaceOne = false
				if len(f.matches) > 0 && f.current >= 0 && f.current < len(f.matches) {
					m := f.matches[f.current]
					ta.ReplaceText(m[0], m[1], f.with)
					f.cacheText = ""
					f.refresh(d.preview)
					if len(f.matches) > 0 {
						nextStart := m[0] + utf8.RuneCountInString(f.with)
						f.current = 0
						for i, nm := range f.matches {
							if nm[0] >= nextStart {
								f.current = i
								break
							}
						}
						f.jumpPending = true
					}
				}
			}
			if f.replaceAll && !d.ed.ReadOnly {
				f.replaceAll = false
				if replaced, n := replaceFold(d.preview, f.query, f.with); n > 0 {
					ta.ReplaceText(0, utf8.RuneCountInString(d.preview), replaced)
					f.cacheText = ""
					f.refresh(d.preview)
				}
			}
			if f.jumpPending {
				f.jumpPending = false
				if len(f.matches) > 0 && f.current >= 0 && f.current < len(f.matches) {
					m := f.matches[f.current]
					ta.SetTextSelection(m[0], m[1])
				}
			}
		}

		d.sourceComposing = ta.Composing()
		d.noteSourceText()

		if d.previewCaretPending {
			d.previewCaretPending = false
			ta.SetTextSelection(d.previewCaret, d.previewCaret)
		} else if d.previewEnteredY < 0 {
			d.previewEnteredY = d.previewScroll.Y
		}
		if d.sourceFocusPending && !d.sourceFind.open {
			d.sourceFocusPending = false
			ta.Focus()
		}

		selA, selB := ta.TextSelection()
		_, caret := ta.TextAnchorCaret()
		if d.previewEnteredY >= 0 && caret != d.sourceCaret {
			d.sourceCaretMoved = true
		}
		d.sourceCaret = caret
		d.sourceSelStart, d.sourceSelEnd = selA, selB

		// Submit syntax highlighting only for visible lines (+ margin) so large files stay 120 Hz.
		visTop := max(float32(0), d.previewScroll.Y-padTop)
		firstLine := max(0, d.sourceLines.At(visTop)-12)
		lastLine := d.sourceLines.At(visTop+winH) + 12
		if d.sourceHighlight == nil {
			d.sourceHighlight = &editor.SourceHighlightCache{}
		}
		if ranges := d.sourceHighlight.VisibleRanges(d.preview, pg, firstLine, lastLine); len(ranges) > 0 {
			ta.TextRanges(ranges...)
		}
		if f.open && len(f.matches) > 0 {
			var matchRanges []ui.TextRange
			for i, m := range f.matches {
				if i == f.current {
					matchRanges = append(matchRanges, ui.TextRange{Start: m[0], End: m[1], Color: t.Accent, Weight: 700})
				} else {
					matchRanges = append(matchRanges, ui.TextRange{Start: m[0], End: m[1], Color: t.Accent, Weight: 600})
				}
			}
			ta.TextRanges(matchRanges...)
		}
	})
}

func applySourceIndent(text string, start, end int, indent bool) (editStart, editEnd int, rep string, newStart, newEnd int, ok bool) {
	runes := []rune(text)
	n := len(runes)
	start = max(0, min(start, n))
	end = max(0, min(end, n))
	if start > end {
		start, end = end, start
	}
	if indent && start == end {
		return start, end, "  ", start + 2, end + 2, true
	}

	lineStart := start
	for lineStart > 0 && runes[lineStart-1] != '\n' {
		lineStart--
	}
	// If selection ends right at column 0 of a subsequent line, don't include that trailing line.
	endAnchor := end
	if endAnchor > start && endAnchor > 0 && runes[endAnchor-1] == '\n' {
		endAnchor--
	}
	lineEnd := endAnchor
	for lineEnd < n && runes[lineEnd] != '\n' {
		lineEnd++
	}

	segment := string(runes[lineStart:lineEnd])
	lines := strings.Split(segment, "\n")
	changed := false
	firstDelta := 0
	totalDelta := 0
	for i, ln := range lines {
		if indent {
			lines[i] = "  " + ln
			changed = true
			if i == 0 {
				firstDelta = 2
			}
			totalDelta += 2
		} else {
			removed := 0
			switch {
			case strings.HasPrefix(ln, "\t"):
				lines[i] = ln[1:]
				removed = 1
			case strings.HasPrefix(ln, "  "):
				lines[i] = ln[2:]
				removed = 2
			case strings.HasPrefix(ln, " "):
				lines[i] = ln[1:]
				removed = 1
			}
			if removed > 0 {
				changed = true
				if i == 0 {
					firstDelta = -removed
				}
				totalDelta -= removed
			}
		}
	}
	if !changed {
		return 0, 0, "", start, end, false
	}
	rep = strings.Join(lines, "\n")
	newStart = max(lineStart, start+firstDelta)
	newEnd = max(newStart, end+totalDelta)
	return lineStart, lineEnd, rep, newStart, newEnd, true
}

func lineColAtRune(text string, runeOff int) (line, col int) {
	line, col = 1, 1
	if runeOff <= 0 {
		return line, col
	}
	i := 0
	for _, r := range text {
		if i >= runeOff {
			break
		}
		if r == '\n' {
			line++
			col = 1
		} else {
			col++
		}
		i++
	}
	return line, col
}

func runeSlice(text string, start, end int) string {
	if start >= end || text == "" {
		return ""
	}
	if start < 0 {
		start = 0
	}
	var bStart, bEnd int
	seen := 0
	foundStart := false
	for idx := range text {
		if seen == start {
			bStart = idx
			foundStart = true
		}
		if seen == end {
			bEnd = idx
			return text[bStart:bEnd]
		}
		seen++
	}
	if !foundStart {
		return ""
	}
	return text[bStart:]
}
