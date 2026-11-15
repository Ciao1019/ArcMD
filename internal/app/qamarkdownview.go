package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/agent"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
)

// Lobe's chat variant: 14px / 1.6, .02em tracking, 600 strong text,
// compact heading scale and a 4px quotation rule. Colors use ArcMD tokens.
func (a *App) qaAnswerBlock(c *ui.Context, spaceID string, turn *qaTurn, b *qaBlock) {
	t := c.Theme()
	if b.fade.active(c.Now()) {
		c.After(time.Second / 60)
	}
	index := 0
	text := func(key string, runs []qaRun, ink ui.Color) ui.Element {
		return a.qaMarkdownText(c, key, spaceID, turn, runs, b.fade, &index, ink)
	}
	switch b.kind {
	case qaRule:
		ui.Box(c).Height(1).Background(separator(t)).PaddingY(7)
	case qaCodeBlock:
		ui.Column(c).Opacity(qaFadeAlpha(c.Now(), b.fade.blockBirth)).Children(func() { qaMarkdownCode(c, b) })
	case qaHeading:
		text("text", b.runs, t.Text).FontSize(b.size).FontWeight(700).LineHeight(1.25).Padding(7, 0, 7, 0)
	case qaQuote:
		ui.Row(c).Gap(14).Children(func() {
			ui.Box(c).Width(4).Radius(2).AlignSelf(ui.Stretch).Background(t.Accent.Alpha(0.35))
			text("text", b.runs, t.TextMuted).Shrink(1).MinWidth(0)
		})
	case qaListItem:
		ui.Row(c).Gap(8).AlignItems(ui.Start).Padding(0, 0, 0, float32(max(0, b.indent-1))*20).Children(func() {
			ui.Text(c, b.marker).FontSize(14).LineHeight(1.6).TextColor(t.TextMuted).Width(20).TextAlign(ui.End).Shrink(0)
			text("text", b.runs, t.Text).Shrink(1).MinWidth(0)
		})
	case qaTableRow:
		row := ui.Row(c).Border(1, t.Border.Alpha(0.5))
		if b.header {
			row.Background(t.Text.Alpha(0.04)).FontWeight(600)
		}
		row.Children(func() {
			for i, cell := range b.cells {
				ui.Box(c.Key(i)).Grow(1).Basis(0).MinWidth(0).Padding(8).Children(func() {
					e := text("cell", cell.runs, t.Text)
					switch cell.align {
					case parser.AlignCenter:
						e.TextAlign(ui.Center)
					case parser.AlignRight:
						e.TextAlign(ui.End)
					}
				})
			}
		})
	default:
		text("text", b.runs, t.Text)
	}
}

func (a *App) qaMarkdownText(c *ui.Context, key, spaceID string, turn *qaTurn, runs []qaRun, fade *qaFade, index *int, ink ui.Color) ui.Element {
	t := c.Theme()
	return ui.RichText(c.Key(key)).FontSize(14).LineHeight(1.6).LetterSpacing(0.28).TextColor(ink).Selectable().Children(func() {
		for i, run := range runs {
			if run.cite != nil {
				ci := *run.cite
				n := turn.view.numbers[citeKey(ci)]
				alpha := fade.alpha(c.Now(), *index)
				*index++

				ref, hasRef := turn.view.byNum[n]
				tip := citeTooltip(ci, ref, hasRef)
				title := vault.Title(ci.Path)
				if hasRef && ref.title != "" {
					title = ref.title
				}

				chip := ui.RichText(c.Key(i)).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleLink).
					Label(fmt.Sprintf(i18n.T("来源 %d：%s"), n, title))
				if !hasRef {
					chip.Tooltip(tip)
				}
				hovered := chip.Hovered() || turn.hoverCite == n
				if chip.Hovered() {
					turn.nextHoverCite = n
					turn.nextHoverInline = true
					turn.nextCiteAnchor = chip.Bounds()
					if turn.hoverCite != n || !turn.hoverInline {
						c.AnimationFrame()
					}
				}
				fg := qaFadeInk(t.Accent, alpha)
				bg := t.Accent.Alpha(0.14 * alpha)
				if hovered {
					fg = qaFadeInk(t.AccentText, alpha)
					bg = t.Accent.Alpha(0.92 * alpha)
				}
				chip.Children(func() {
					ui.RichText(c, ui.Span{
						Text:          " " + strconv.Itoa(n) + " ",
						Size:          10.5,
						Weight:        650,
						Color:         fg,
						Background:    bg,
						Radius:        5,
						BaselineShift: 1,
					})
				})
				if chip.Clicked() {
					a.openCitation(spaceID, ci)
				}
				continue
			}
			base := ui.Span{Text: run.text, Weight: run.weight, Italic: run.italic, Strikethrough: run.strike, Color: ink}
			if run.strike {
				base.Color = t.TextMuted
			}
			if run.url != "" {
				base.Color = t.Accent
			}
			if run.code {
				base.Font, base.Size, base.Background, base.Radius = "monospace", 12.25, t.Text.Alpha(0.07), 4
			}
			var spans []ui.Span
			if !fade.blockBirth.IsZero() && fade.active(c.Now()) {
				base.Color = qaFadeInk(base.Color, fade.alpha(c.Now(), *index))
				base.Background = base.Background.Alpha(fade.alpha(c.Now(), *index))
				spans = []ui.Span{base}
				*index += qaRunChars(run)
			} else if !run.code && fade.active(c.Now()) {
				// Colors do not participate in MyGo's shape cache. The span
				// boundaries stay fixed while fading, so glyphs never reflow.
				prefix := 0
				for prefix < len(run.text) && *index < fade.settled {
					_, n := utf8.DecodeRuneInString(run.text[prefix:])
					prefix += n
					*index++
				}
				spans = make([]ui.Span, 0, 1+utf8.RuneCountInString(run.text[prefix:]))
				if prefix > 0 {
					s := base
					s.Text = run.text[:prefix]
					spans = append(spans, s)
				}
				for _, ch := range run.text[prefix:] {
					s := base
					s.Text = string(ch)
					s.Color = qaFadeInk(base.Color, fade.alpha(c.Now(), *index))
					*index++
					spans = append(spans, s)
				}
			} else {
				spans = []ui.Span{base}
				*index += qaRunChars(run)
			}
			if run.url != "" && !run.pending && qaWebLink(run.url) {
				ui.Link(c.Key(i), "", run.url).Label(run.text).Tooltip(run.url).Children(func() { ui.RichText(c, spans...) })
			} else {
				ui.RichText(c.Key(i), spans...)
			}
		}
	})
}

func citeTooltip(ci agent.Citation, ref citeRef, hasRef bool) string {
	title := vault.Title(ci.Path)
	var parts []string
	if hasRef {
		if ref.title != "" {
			title = ref.title
		}
		if ref.folder != "" {
			parts = append(parts, ref.folder+" / "+title)
		} else {
			parts = append(parts, title)
		}
		if ref.section != "" {
			parts = append(parts, "§ "+ref.section)
		}
	} else {
		parts = append(parts, title)
	}
	parts = append(parts, humanLines(ci.From, ci.To))
	head := strings.Join(parts, " · ")
	if hasRef && ref.excerpt != "" {
		return head + "\n“" + ref.excerpt + "”"
	}
	return head
}

func qaWebLink(url string) bool {
	lower := strings.ToLower(url)
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "mailto:")
}

func qaMarkdownCode(c *ui.Context, b *qaBlock) {
	t := c.Theme()
	ui.Column(c).Radius(8).Background(t.Text.Alpha(0.04)).Border(1, t.Border.Alpha(0.5)).Clip().Children(func() {
		ui.Row(c).Padding(6, 10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, b.language).FontSize(12).TextColor(t.TextMuted)
			ui.Spacer(c)
			if ui.Button(c.Key("copy"), i18n.T("复制代码")).FontSize(11).Clicked() {
				c.WriteClipboard(b.text)
			}
		})
		ui.ScrollHorizontal(c).Children(func() {
			ui.Text(c, b.text).Font("monospace").FontSize(12.25).LineHeight(1.6).NoWrap().Selectable().Padding(8, 12, 12, 12)
		})
	})
}
