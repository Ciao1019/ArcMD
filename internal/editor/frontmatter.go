package editor

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

type frontmatterLayoutKey struct {
	visible, collapsed, editing, hide bool
	draft                             string
}

func (e *Editor) frontmatterLayoutState(o Options) frontmatterLayoutKey {
	k := frontmatterLayoutKey{
		visible:   e.frontmatterVisible(o),
		collapsed: e.header.fmCollapsed,
		editing:   e.header.fmEditing,
		hide:      o.HideFrontmatter,
	}
	if e.header.fmEditing {
		k.draft = e.header.fmDraft
	}
	return k
}

func (e *Editor) frontmatterVisible(o Options) bool {
	return !e.printing && !o.HideFrontmatter && (strings.TrimSpace(e.Doc.Frontmatter) != "" || e.header.fmEditing)
}

func (e *Editor) layoutFrontmatter(o Options, y float32) float32 {
	h := &e.header
	h.fmRect = ui.Rect{}
	h.fmLayout = nil
	h.fmBarH = 0
	h.fmBodyY = 0
	if !e.frontmatterVisible(o) {
		return y
	}
	z := e.zoomFactor()
	x := e.padding
	w := e.bodyWidth()
	barH := 30 * z
	padX := 14 * z
	padTop := 10 * z
	padBot := 12 * z
	font := ui.Font{Family: "monospace", Size: 13 * z, LineHeight: 1.55}
	wrapW := max(60, w-2*padX)

	var totalH float32
	switch {
	case h.fmEditing:
		draft := strings.ReplaceAll(h.fmDraft, "\t", "    ")
		if draft == "" {
			draft = " "
		}
		tl := ui.ShapeRichText([]ui.Span{{Text: draft}}, font, wrapW)
		_, th := tl.Size()
		bodyH := min(360*z, max(88*z, float32(math.Ceil(float64(th)))+padTop+padBot))
		totalH = barH + bodyH
	case h.fmCollapsed:
		totalH = barH
	default:
		src := strings.ReplaceAll(strings.TrimRight(e.Doc.Frontmatter, "\r\n"), "\t", "    ")
		spans := codeSpansFor("", "yaml", src, e.page, nil)
		tl := ui.ShapeRichText(spans, font, wrapW)
		_, th := tl.Size()
		h.fmLayout = tl
		totalH = barH + padTop + float32(math.Ceil(float64(th))) + padBot
	}

	h.fmBarH = barH
	h.fmBodyY = barH + padTop
	h.fmRect = ui.Rect{X: x, Y: y, W: w, H: totalH}
	return y + totalH + 20*z
}

func (e *Editor) paintFrontmatter(p *ui.Painter, bounds ui.Rect) {
	h := &e.header
	if h.fmRect.H <= 0 || e.printing {
		return
	}
	if e.viewport > 0 && (h.fmRect.Y+h.fmRect.H < e.Scroll.Y || h.fmRect.Y > e.Scroll.Y+e.viewport) {
		return
	}
	z := e.zoomFactor()
	r := h.fmRect
	r.X += bounds.X
	r.Y += bounds.Y
	rad := 8 * z

	p.Fill(r, e.page.CodeFill, rad)
	border := e.page.CodeBorder
	borderW := float32(1)
	switch {
	case h.fmEditing:
		border = e.page.CodeHover
		borderW = 1.5
	case !e.ReadOnly && e.hovered(h.fmRect, 0):
		border = e.page.CodeHover.Alpha(0.55)
	}
	p.Stroke(r, border, rad, borderW)

	if h.fmEditing || !h.fmCollapsed {
		p.Line(r.X, r.Y+h.fmBarH, r.X+r.W, r.Y+h.fmBarH, 1, e.page.CodeBorder)
	}
	if !h.fmEditing && !h.fmCollapsed && h.fmLayout != nil {
		p.TextLayout(h.fmLayout, r.X+14*z, r.Y+h.fmBodyY, e.page.CodeText)
	}
}

func (e *Editor) frontmatterControls(c *ui.Context) {
	h := &e.header
	if h.fmRect.H <= 0 || e.printing || e.hideFrontmatter {
		return
	}
	z := e.zoomFactor()
	r := h.fmRect
	t := c.Theme()

	bar := ui.Row(c.Key("frontmatter-bar")).Absolute().Left(r.X).Top(r.Y).Width(r.W).Height(h.fmBarH).
		Padding(0, 8*z, 0, 10*z).Gap(6 * z).AlignItems(ui.Center)

	if !h.fmEditing {
		bar.Children(func() {
			toggle := ui.ButtonBase(c.Key("frontmatter-toggle")).Height(22 * z).PaddingX(6 * z).Gap(5 * z).Radius(5 * z).
				AlignItems(ui.Center).Role(ui.RoleButton).Expanded(!h.fmCollapsed).
				Label(i18n.T("元数据")).KeepFocus().Cursor(ui.CursorPointer)
			if toggle.Hovered() {
				toggle.Background(e.page.Hover)
			}
			toggle.Children(func() {
				chev := iconChevron
				if h.fmCollapsed {
					chev = iconChevronRight
				}
				ui.Icon(c, chev).FontSize(12 * z).TextColor(e.page.Caption)
				ui.Text(c, i18n.T("元数据")).FontSize(12 * z).FontWeight(600).TextColor(e.page.Caption)
				if sum := frontmatterSummary(e.Doc.Frontmatter); sum != "" {
					ui.Text(c, sum).FontSize(11 * z).TextColor(e.page.Placeholder)
				}
			})
			if toggle.Clicked() {
				h.fmCollapsed = !h.fmCollapsed
				c.After(0)
			}

			ui.Spacer(c)

			copied := !h.fmCopiedAt.IsZero() && c.Now().Sub(h.fmCopiedAt) < 1500*time.Millisecond
			if copied {
				c.After(1500*time.Millisecond - c.Now().Sub(h.fmCopiedAt))
			}
			copyIcon := iconCopy
			if copied {
				copyIcon = iconCheck
			}
			copyBtn := ui.ButtonBase(c.Key("frontmatter-copy")).Size(22*z, 22*z).Radius(5 * z).Center().
				KeepFocus().Cursor(ui.CursorPointer).
				Label(i18n.T("复制元数据")).Tooltip(i18n.T("复制元数据"))
			if copyBtn.Hovered() {
				copyBtn.Background(e.page.Hover)
			}
			copyBtn.Children(func() {
				ink := e.page.Caption
				if copied {
					ink = t.Accent
				}
				ui.Icon(c, copyIcon).FontSize(12 * z).TextColor(ink)
			})
			if copyBtn.Clicked() {
				e.services.WriteClipboard("---\n" + strings.TrimRight(e.Doc.Frontmatter, "\r\n") + "\n---")
				h.fmCopiedAt = c.Now()
			}

			if !e.ReadOnly {
				editBtn := ui.ButtonBase(c.Key("frontmatter-edit")).Size(22*z, 22*z).Radius(5 * z).Center().
					KeepFocus().Cursor(ui.CursorPointer).
					Label(i18n.T("编辑元数据")).Tooltip(i18n.T("编辑元数据"))
				if editBtn.Hovered() {
					editBtn.Background(e.page.Hover)
				}
				editBtn.Children(func() {
					ui.Icon(c, iconPencil).FontSize(12 * z).TextColor(e.page.Caption)
				})
				if editBtn.Clicked() {
					e.startFrontmatterEdit()
				}
			}
		})

		if !h.fmCollapsed && !e.ReadOnly {
			bodyBtn := ui.ButtonBase(c.Key("frontmatter-body")).Absolute().
				Left(r.X).Top(r.Y + h.fmBarH).Width(r.W).Height(max(1, r.H-h.fmBarH)).
				KeepFocus().Cursor(ui.CursorText).Label(i18n.T("编辑元数据内容"))
			if bodyBtn.Clicked() {
				e.startFrontmatterEdit()
			}
		}
		return
	}

	bar.Children(func() {
		ui.Text(c, i18n.T("元数据")).FontSize(12 * z).FontWeight(600).TextColor(e.page.Caption)
		ui.Text(c, "YAML").FontSize(11 * z).TextColor(e.page.Placeholder)
		ui.Spacer(c)
		if strings.TrimSpace(e.Doc.Frontmatter) != "" {
			if e.fmBarActionButton(c, "frontmatter-remove", i18n.T("移除"), z, false, true).Clicked() {
				e.Engine.SetFrontmatter("")
				h.fmEditing = false
				h.fmFocusPending = false
				e.RequestFocus = true
			}
		}
		if e.fmBarActionButton(c, "frontmatter-cancel", i18n.T("取消"), z, false, false).Clicked() {
			e.cancelFrontmatterEdit()
			e.RequestFocus = true
		}
		if e.fmBarActionButton(c, "frontmatter-done", i18n.T("完成"), z, true, false).Clicked() {
			e.commitFrontmatterEdit()
			e.RequestFocus = true
		}
	})

	ta := ui.TextAreaBase(c.Key("frontmatter-input"), &h.fmDraft).
		Absolute().Left(r.X).Top(r.Y+h.fmBarH).Width(r.W).Height(max(1, r.H-h.fmBarH)).
		Font("monospace").FontSize(13*z).LineHeight(1.55).
		TextColor(e.page.CodeText).Background(ui.Transparent).
		Padding(10*z, 14*z, 12*z, 14*z).FocusRing(false).
		Placeholder(i18n.T("name: 示例\ntags:\n  - 笔记")).
		Label(i18n.T("元数据编辑器"))
	if h.fmFocusPending {
		ta.Focus()
		h.fmFocusPending = false
	}
	if ta.Shortcut(ui.Cmd, ui.KeyEnter) || ta.Shortcut(ui.Ctrl, ui.KeyEnter) {
		e.commitFrontmatterEdit()
		e.RequestFocus = true
	} else if ta.Shortcut(0, ui.KeyEscape) {
		e.cancelFrontmatterEdit()
		e.RequestFocus = true
	}
}

func (e *Editor) fmBarActionButton(c *ui.Context, key, label string, z float32, accent, danger bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Height(22 * z).PaddingX(8 * z).Radius(5 * z).Center().
		KeepFocus().Cursor(ui.CursorPointer).Label(label)
	ink := e.page.Caption
	switch {
	case accent:
		ink = t.Accent
		if b.Hovered() {
			b.Background(t.Accent.Alpha(0.22))
		} else {
			b.Background(e.page.TurnActive)
		}
	case danger:
		ink = e.page.Danger
		if b.Hovered() {
			b.Background(e.page.DangerCell)
		}
	default:
		if b.Hovered() {
			b.Background(e.page.Hover)
		}
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(12 * z).TextColor(ink)
	})
	return b
}

func (e *Editor) startFrontmatterEdit() {
	if e.ReadOnly || e.hideFrontmatter {
		return
	}
	h := &e.header
	h.iconOpen, h.coverOpen, h.moving = false, false, false
	h.fmCollapsed = false
	h.fmDraft = strings.TrimRight(e.Doc.Frontmatter, "\r\n")
	h.fmEditing = true
	h.fmFocusPending = true
}

func (e *Editor) commitFrontmatterEdit() {
	h := &e.header
	if !h.fmEditing {
		return
	}
	normalized := normalizeFrontmatterInput(h.fmDraft)
	h.fmEditing = false
	h.fmFocusPending = false
	if !e.ReadOnly {
		e.Engine.SetFrontmatter(normalized)
	}
}

func (e *Editor) cancelFrontmatterEdit() {
	h := &e.header
	if !h.fmEditing {
		return
	}
	h.fmEditing = false
	h.fmFocusPending = false
}

func normalizeFrontmatterInput(raw string) string {
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	if strings.TrimSpace(s) == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines) - 1
	for end >= start && strings.TrimSpace(lines[end]) == "" {
		end--
	}
	if end > start && strings.TrimSpace(lines[start]) == "---" {
		tail := strings.TrimSpace(lines[end])
		if tail == "---" || tail == "..." {
			inner := strings.Join(lines[start+1:end], "\n")
			if strings.TrimSpace(inner) == "" {
				return ""
			}
			return strings.TrimRight(inner, "\r\n")
		}
	}
	return strings.TrimRight(s, "\r\n")
}

func frontmatterSummary(raw string) string {
	s := strings.ReplaceAll(strings.TrimSpace(raw), "\r\n", "\n")
	if s == "" {
		return ""
	}
	keys := 0
	lines := 0
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines++
		if line[0] != ' ' && line[0] != '\t' && !strings.HasPrefix(line, "-") {
			if k, _, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) != "" {
				keys++
			}
		}
	}
	if keys > 0 {
		return fmt.Sprintf(i18n.T("%d 项"), keys)
	}
	if lines > 0 {
		return fmt.Sprintf(i18n.T("%d 行"), lines)
	}
	return ""
}
