package dbview

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

func feedView(
	c *ui.Context,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	opts Options,
	pal Palette,
) {
	fcfg := database.FeedViewConfig{ShowExcerpt: true, ShowImage: true}
	if view.Feed != nil {
		fcfg = *view.Feed
	}

	statusProp := resolveFeedStatusProp(db, fcfg)
	dateProp := resolveFeedDateProp(db, fcfg)
	excludeIDs := make([]string, 0, 2)
	if statusProp != nil && statusProp.ID != "" {
		excludeIDs = append(excludeIDs, statusProp.ID)
	}
	if dateProp != nil && dateProp.ID != "" && dateProp.Type == database.PropDate {
		excludeIDs = append(excludeIDs, dateProp.ID)
	}
	cardProps := visibleNonTitleProps(db, &view, excludeIDs...)

	scroller := ui.Scroll(c.Key("feed-scroll")).TrackScroll(&st.FeedScroll).Grow(1).MinWidth(0).MinHeight(0)
	scroller.Children(func() {
		ui.Column(c.Key("feed-center")).FillWidth().AlignItems(ui.Center).Padding(16, 20, 28, 20).Children(func() {
			ui.Column(c.Key("feed-stream")).Width(720).MaxWidthPercent(100).Gap(12).Children(func() {
				if !opts.ReadOnly {
					renderFeedQuickCompose(c, st, db, opts, pal)
				}

				if res == nil || len(res.Rows) == 0 {
					ui.Column(c.Key("feed-empty")).Padding(40).Center().Gap(8).Children(func() {
						ui.Icon(c, iconViewFeed).FontSize(26).TextColor(pal.TextMuted.Alpha(0.45))
						ui.Text(c, i18n.T("暂无条目")).FontSize(13.5).TextColor(pal.TextMuted)
					})
					return
				}

				if len(res.Groups) > 0 {
					for i := range res.Groups {
						g := res.Groups[i]
						renderGalleryGroupHeader(c, st, &view, db, g, opts, pal)
						if !g.Collapsed {
							for _, r := range g.Rows {
								if r != nil {
									renderFeedCard(c, st, view, db, res, r, statusProp, dateProp, cardProps, fcfg, opts, pal)
								}
							}
						}
					}
					return
				}

				for _, r := range res.Rows {
					if r != nil {
						renderFeedCard(c, st, view, db, res, r, statusProp, dateProp, cardProps, fcfg, opts, pal)
					}
				}
			})
		})
	})
}

func renderFeedQuickCompose(
	c *ui.Context,
	st *State,
	db *database.Database,
	opts Options,
	pal Palette,
) {
	post := func() {
		title := strings.TrimSpace(st.FeedQuickDraft)
		if title == "" {
			return
		}
		st.FeedQuickDraft = ""
		rec, err := st.Store.CreateRecord(title, nil, "")
		if err != nil {
			if opts.OnNotify != nil {
				opts.OnNotify(err.Error())
			}
			return
		}
		st.Motion.TrackCreated(rec.ID)
		if opts.OnAfterCreate != nil {
			opts.OnAfterCreate(rec)
		}
		if opts.OnChanged != nil {
			var affected []string
			if rec.Path != "" {
				affected = []string{rec.Path}
			}
			opts.OnChanged(affected)
		}
	}

	ui.Row(c.Key("feed-compose")).Padding(10, 12).Radius(11).
		Background(pal.Paper).Border(1, pal.Border).Gap(8).AlignItems(ui.Center).
		Shadow(0, 1, 3, 0, pal.ShadowEdge).Children(func() {
		in := ui.TextInput(c.Key("feed-quick-in"), &st.FeedQuickDraft).
			Placeholder(i18n.T("快速发布一条动态，回车创建…")).Label(i18n.T("快速发布")).
			Grow(1).MinWidth(0).Height(32).Padding(4, 10).Radius(7).
			Background(pal.Surface).TextColor(pal.Text)
		if in.Submitted() {
			post()
			return
		}
		canPost := strings.TrimSpace(st.FeedQuickDraft) != ""
		btn := ui.PrimaryButton(c.Key("feed-quick-btn"), i18n.T("发布")).Height(32).Padding(0, 14).Disabled(!canPost)
		if btn.Clicked() && canPost {
			post()
		}
	})
	_ = db
}

func renderFeedCard(
	c *ui.Context,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	rec *database.Record,
	statusProp *database.Property,
	dateProp *database.Property,
	cardProps []database.Property,
	fcfg database.FeedViewConfig,
	opts Options,
	pal Palette,
) {
	padY, padX := float32(14), float32(16)
	gap := float32(9)
	covH := float32(196)
	if fcfg.Compact {
		padY, padX = 10, 14
		gap = 6
		covH = 128
	}

	cardBg := pal.Paper
	tintBg, tintAccent, tinted := tintedRowBackground(pal, res, rec.ID)
	if tinted {
		cardBg = tintBg
	}
	card := ui.Column(c.Key("fcard:"+rec.ID)).Radius(12).Background(cardBg).
		Border(1, pal.Border).Shadow(0, 1, 4, 0, pal.ShadowEdge).
		Padding(padY, padX).Gap(gap).Cursor(ui.CursorPointer).Clip().
		Focusable().Role(ui.RoleArticle).Label(rec.Title).
		Transition(ui.ElementTransition{
			Animation:    &dbSpringCard,
			Position:     true,
			Colors:       true,
			Shadows:      true,
			ExitDuration: 150 * time.Millisecond,
			Enter:        &ui.Motion{Y: -8, Scale: 0.97, Opacity: 0.78},
			Exit:         &ui.Motion{Scale: 0.95, Opacity: 0},
		})
	if tinted {
		card.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 3, H: r.H}, tintAccent, 0)
		})
	}
	pulse := st.Motion.pulseAlpha(c, rec.ID)
	if pulse > 0.01 {
		card.DrawOver(func(p *ui.Painter, r ui.Rect) {
			paintCreationPulse(p, r, 12, pulse, pal.Accent)
		})
	}
	hovered := card.Hovered()
	if hovered {
		card.Border(1, pal.BorderStrong).Shadow(0, 6, 16, -3, pal.ShadowEdge)
	}
	applyCardHoverLift(card, hovered, false)
	attachRecordContextMenu(card, st, rec, opts)

	var consumedTitle, consumedProps bool
	card.Children(func() {
		// Header meta row: timestamp + status badge.
		dateStr := ""
		if dateProp != nil {
			if dv := rec.ValueFor(*dateProp); !dv.IsEmpty() {
				dateStr = database.FormatValue(*dateProp, dv)
			}
		}
		if dateStr == "" && !rec.UpdatedAt.IsZero() {
			dateStr = rec.UpdatedAt.Format("2006-01-02")
		}

		hasStatus := false
		var statusVal string
		var statusOpt database.SelectOption
		if statusProp != nil {
			if sv := rec.ValueFor(*statusProp); !sv.IsEmpty() {
				hasStatus = true
				statusVal = sv.Text
				statusOpt = findOption(*statusProp, statusVal)
			}
		}

		if dateStr != "" || hasStatus {
			ui.Row(c.Key("fmeta")).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				if dateStr != "" {
					ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, iconViewCalendar).FontSize(12).TextColor(pal.TextMuted)
						ui.Text(c, dateStr).FontSize(11.5).TextColor(pal.TextMuted).SingleLine()
					})
				} else {
					ui.Box(c)
				}
				if hasStatus {
					renderTagPill(c, statusVal, statusOpt.Color, true, pal)
				}
			})
		}

		// Title.
		consumedTitle = renderCardTitleRow(c, st, db, rec, true, true, opts, pal)

		// Optional Cover thumbnail when the record specifies an image.
		if fcfg.ShowImage && rec.Cover != "" && opts.LoadImage != nil {
			if bmp := opts.LoadImage(rec.Path, rec.Cover); bmp != nil {
				ui.Box(c.Key("fcov:"+rec.ID)).Height(covH).Radius(8).Background(pal.Surface).
					Border(1, pal.Border).Clip().Children(func() {
					ui.Image(c.Key("fimg:"+rec.ID), bmp).WidthPercent(100).Height(covH).Fit(ui.Cover)
				})
			}
		}

		// Optional Markdown Excerpt.
		if fcfg.ShowExcerpt {
			txt := strings.TrimSpace(rec.Excerpt)
			if txt == "" {
				txt = strings.TrimSpace(rec.Body)
			}
			if runes := []rune(txt); len(runes) > 240 {
				txt = string(runes[:240]) + "…"
			}
			if txt != "" && !fcfg.Compact {
				ui.Text(c, txt).FontSize(13).LineHeight(1.55).TextColor(pal.TextMuted).Wrap()
			} else if txt != "" && fcfg.Compact {
				ui.Text(c, txt).FontSize(12.5).TextColor(pal.TextMuted).SingleLine()
			}
		}

		// Property pills / quick checkbox footer.
		if len(cardProps) > 0 {
			consumedProps = renderCardProperties(c, st, rec, cardProps, true, opts, pal)
		}
	})

	if card.Clicked() && !consumedTitle && !consumedProps && opts.OnOpenRecord != nil {
		opts.OnOpenRecord(rec, resolveCardOpenMode(view))
	}
	_ = fmt.Sprint
	_ = slices.Clip([]string{})
}
