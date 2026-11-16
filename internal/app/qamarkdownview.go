package app

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
)

// Lobe's chat variant: 14px / 1.72, .02em tracking, 600 strong text,
// compact heading scale and a 4px quotation rule. Colors use ArcMD tokens.
func (a *App) qaAnswerBlock(c *ui.Context, spaceID string, s *qaSession, turn *qaTurn, b *qaBlock) {
	t := c.Theme()
	if b.fade.active(c.Now()) {
		c.After(time.Second / 60)
	}
	index := 0
	text := func(key string, runs []qaRun, ink ui.Color, fontSize float32) ui.Element {
		return a.qaMarkdownText(c, key, spaceID, s, turn, runs, b.fade, &index, ink, fontSize)
	}
	switch b.kind {
	case qaRule:
		ui.Box(c).Height(1).Background(separator(t)).PaddingY(7)
	case qaCodeBlock:
		ui.Column(c).Opacity(qaFadeAlpha(c.Now(), b.fade.blockBirth)).Children(func() { qaMarkdownCode(c, b) })
	case qaHeading:
		text("text", b.runs, t.Text, b.size).FontSize(b.size).FontWeight(700).LineHeight(1.38).Padding(7, 0, 7, 0)
	case qaQuote:
		ui.Row(c).Gap(14).Children(func() {
			ui.Box(c).Width(4).Radius(2).AlignSelf(ui.Stretch).Background(t.Accent.Alpha(0.35))
			text("text", b.runs, t.TextMuted, 14).Shrink(1).MinWidth(0)
		})
	case qaListItem:
		ui.Row(c).Gap(8).AlignItems(ui.Start).Padding(0, 0, 0, float32(max(0, b.indent-1))*20).Children(func() {
			ui.Text(c, b.marker).FontSize(14).LineHeight(1.72).TextColor(t.TextMuted).Width(20).TextAlign(ui.End).Shrink(0)
			text("text", b.runs, t.Text, 14).Shrink(1).MinWidth(0)
		})
	case qaTableRow:
		row := ui.Row(c).Border(1, t.Border.Alpha(0.5))
		if b.header {
			row.Background(t.Text.Alpha(0.04)).FontWeight(600)
		}
		row.Children(func() {
			for i, cell := range b.cells {
				ui.Box(c.Key(i)).Grow(1).Basis(0).MinWidth(0).Padding(8).Children(func() {
					e := text("cell", cell.runs, t.Text, 14)
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
		text("text", b.runs, t.Text, 14)
	}
}

func (a *App) qaMarkdownText(c *ui.Context, key, spaceID string, s *qaSession, turn *qaTurn, runs []qaRun, fade *qaFade, index *int, ink ui.Color, fontSize float32) ui.Element {
	t := c.Theme()
	cols := qaCitationColors(t)
	anim := ui.Animation{Duration: 150 * time.Millisecond, Ease: qaCiteEase}
	para := ui.RichText(c.Key(key)).FontSize(fontSize).LineHeight(1.72).LetterSpacing(0.28).TextColor(ink).Selectable()
	var chips []qaDrawnChip
	runePos := 0
	para.Children(func() {
		for i, run := range runs {
			if len(run.cites) > 0 {
				alpha := fade.alpha(c.Now(), *index)
				*index++

				nums := make([]int, 0, len(run.cites))
				titles := make([]string, 0, len(run.cites))
				for _, ci := range run.cites {
					n := turn.view.numbers[citeKey(ci)]
					if n > 0 {
						nums = append(nums, n)
					}
					title := vault.Title(ci.Path)
					if ref, ok := turn.view.byNum[n]; ok && ref.title != "" {
						title = ref.title
					}
					titles = append(titles, docName(title))
				}

				first := run.cites[0]
				firstTitle := vault.Title(first.Path)
				if len(nums) > 0 {
					if ref, ok := turn.view.byNum[nums[0]]; ok && ref.title != "" {
						firstTitle = ref.title
					}
				}
				chipLabel := qaChipLabel(firstTitle)
				extra := ""
				if len(run.cites) > 1 {
					extra = fmt.Sprintf("+%d", len(run.cites)-1)
				}
				leftPad := float32(0)
				if runePos > 0 {
					ui.RichText(c.Key(fmt.Sprintf("sp-%d", i)), ui.Span{Text: "\u00a0"})
					runePos++
				}
				rightPad := float32(2)
				objW := qaChipWidth(c, turn, chipLabel, extra, leftPad, rightPad)
				shift := max(0, (fontSize-14)*0.3)

				chip := ui.RichText(c.Key(i), ui.Span{
					Text:          "\uFFFC",
					ObjectWidth:   objW,
					ObjectHeight:  qaChipObjHeight,
					ObjectDescent: qaChipObjDescent,
					BaselineShift: shift,
				}).Cursor(ui.CursorPointer).Focusable().FocusRing(false).Role(ui.RoleButton).
					Label(fmt.Sprintf(i18n.T("来源：%s"), strings.Join(titles, ", ")))

				focused := chip.Focused() && chip.FocusVisible()
				hovered := chip.Hovered()
				if (hovered || focused) && s != nil {
					s.preview.observeChip(turn, run.partID, nums, chip.Bounds(), focused)
				}

				lit := s.isChipLit(turn, run.partID, nums)
				targetBg, targetInk := cols.chipFill, cols.chipInk
				if lit {
					targetBg, targetInk = cols.chipActiveFill, cols.chipActiveInk
				} else if hovered {
					targetBg = cols.chipHoverFill
				}
				pillBg := chip.AnimateColor("bg", targetBg, anim).Alpha(alpha)
				pillInk := chip.AnimateColor("ink", targetInk, anim).Alpha(alpha)
				glyph, isEmoji := qaMonogramGlyph(firstTitle)

				chips = append(chips, qaDrawnChip{
					at:        runePos,
					leftPad:   leftPad,
					rightPad:  rightPad,
					monogram:  glyph,
					isEmoji:   isEmoji,
					label:     chipLabel,
					extra:     extra,
					bg:        pillBg,
					ink:       pillInk,
					monoBg:    cols.monoFill.Alpha(alpha),
					monoInk:   cols.monoInk.Alpha(alpha),
					focusRing: focused,
				})
				runePos++

				if chip.Clicked() {
					a.openCitation(spaceID, first)
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
			if run.claimID > 0 {
				lit := s.isClaimLit(turn, run.claimID)
				targetLine := cols.claimLine.Alpha(0)
				if lit {
					targetLine = cols.claimLine
				}
				lineCol := para.AnimateColor(fmt.Sprintf("cl-%d", run.claimID), targetLine, anim)
				if lineCol.A > 0 {
					base.Underline = true
					base.DecorationColor = lineCol
					base.DecorationThickness = 1
				}
				targetFill := cols.claimFill.Alpha(0)
				if lit {
					targetFill = cols.claimFill
				}
				fillCol := para.AnimateColor(fmt.Sprintf("cf-%d", run.claimID), targetFill, anim)
				if fillCol.A > 0 {
					if run.code {
						if cols.claimFill.A > 0 {
							base.Background = base.Background.Mix(cols.claimFill, float32(fillCol.A)/float32(cols.claimFill.A))
						}
					} else {
						base.Background = fillCol
						base.Radius = 3
					}
				}
			}

			var spans []ui.Span
			if !fade.blockBirth.IsZero() && fade.active(c.Now()) {
				a := fade.alpha(c.Now(), *index)
				base.Color = qaFadeInk(base.Color, a)
				if base.Background.A > 0 {
					base.Background = base.Background.Alpha(a)
				}
				if base.DecorationColor.A > 0 {
					base.DecorationColor = base.DecorationColor.Alpha(a)
				}
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
			runePos += utf8.RuneCountInString(run.text)
		}
	})
	if len(chips) > 0 {
		para.DrawObjects(func(p *ui.Painter, at int, r ui.Rect) {
			paintQAChipObject(p, chips, at, r)
		})
	}
	return para
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
