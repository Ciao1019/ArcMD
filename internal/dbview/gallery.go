package dbview

import (
	"fmt"
	"slices"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

func galleryView(
	c *ui.Context,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	opts Options,
	pal Palette,
) {
	gcfg := database.GalleryViewConfig{
		CardSize:    "medium",
		CoverSource: "cover",
		ShowIcon:    true,
	}
	if view.Gallery != nil {
		gcfg = *view.Gallery
		if gcfg.CoverSource == "" {
			gcfg.CoverSource = "cover"
		}
	}
	minCardW, coverH := galleryCardDimensions(gcfg.CardSize)

	scroller := ui.Scroll(c.Key("gallery-scroll")).TrackScroll(&st.GalleryScroll).Grow(1).MinWidth(0).MinHeight(0)
	padX := float32(22)
	gap := float32(14)
	measuredW := scroller.LayoutBounds().W
	availW := measuredW - 2*padX
	if availW <= 120 {
		availW = 860
	}
	cols := max(1, int((availW+gap)/(minCardW+gap)))
	cardW := (availW - float32(cols-1)*gap) / float32(cols)
	props := visibleNonTitleProps(db, &view)
	animatePos := measuredW > 120

	scroller.Children(func() {
		ui.Column(c.Key("gallery-body")).Padding(14, padX, 24, padX).Gap(16).Children(func() {
			if len(res.Groups) == 0 {
				renderGalleryGrid(c, st, &view, db, res, res.Rows, cols, cardW, coverH, gap, animatePos, gcfg, props, "", database.EmptyValue(), opts, pal)
				return
			}
			for i := range res.Groups {
				g := res.Groups[i]
				ui.Column(c.Key("gg:" + g.Key)).Gap(10).Children(func() {
					renderGalleryGroupHeader(c, st, &view, db, g, opts, pal)
					if !g.Collapsed {
						gPropID := ""
						if view.Group != nil {
							gPropID = view.Group.PropertyID
						}
						renderGalleryGrid(c, st, &view, db, res, g.Rows, cols, cardW, coverH, gap, animatePos, gcfg, props, gPropID, g.Value, opts, pal)
					}
				})
			}
		})
	})
}

func galleryCardDimensions(cardSize string) (minW, coverH float32) {
	switch cardSize {
	case "small":
		return 196, 112
	case "large":
		return 340, 192
	default:
		return 264, 148
	}
}

func renderGalleryGroupHeader(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	g database.RecordGroup,
	opts Options,
	pal Palette,
) {
	hdr := ui.Row(c.Key("ggh:"+g.Key)).Height(32).Padding(0, 8).Radius(8).Gap(8).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(g.Label).
		Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
	if hdr.Hovered() {
		hdr.Background(pal.RowHover)
	}
	hdr.Children(func() {
		renderAnimatedChevron(c, "ggh-chev:"+g.Key, g.Collapsed, 13, pal.TextMuted)
		if g.Color != "" && g.Color != "default" {
			isStatus := false
			if view.Group != nil {
				if gp, idx := db.PropertyByID(view.Group.PropertyID); idx >= 0 && gp.Type == database.PropStatus {
					isStatus = true
				}
			}
			renderTagPill(c, g.Label, g.Color, isStatus, pal)
		} else {
			ui.Text(c, g.Label).FontSize(13).FontWeight(600).TextColor(pal.Text).SingleLine()
		}
		ui.Text(c, fmt.Sprintf("%d", g.Count)).FontSize(12).TextColor(pal.TextMuted).SingleLine()
	})
	if hdr.Clicked() && view.Group != nil {
		vc := view.Clone()
		vc.Group.Collapsed = toggleStringInSlice(slices.Clone(vc.Group.Collapsed), g.Key)
		saveViewAndNotify(st, vc, opts)
	}
}

func renderGalleryGrid(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	rows []*database.Record,
	cols int,
	cardW, coverH, gap float32,
	animatePos bool,
	gcfg database.GalleryViewConfig,
	props []database.Property,
	groupPropID string,
	groupVal database.Value,
	opts Options,
	pal Palette,
) {
	type gridEntry struct {
		rec   *database.Record
		isAdd bool
	}
	entries := make([]gridEntry, 0, len(rows)+1)
	for _, r := range rows {
		if r != nil {
			entries = append(entries, gridEntry{rec: r})
		}
	}
	if !opts.ReadOnly {
		entries = append(entries, gridEntry{isAdd: true})
	}

	minCardH := coverH + 64
	if gcfg.CoverSource == "none" {
		minCardH = 88
	}

	for start := 0; start < len(entries); start += cols {
		end := min(len(entries), start+cols)
		ui.Row(c.Key(fmt.Sprintf("grow:%s:%d", groupPropID+groupVal.PlainString(), start))).Gap(gap).AlignItems(ui.Stretch).Children(func() {
			for i := start; i < end; i++ {
				ent := entries[i]
				if ent.isAdd {
					renderGalleryAddCard(c, st, db, minCardH, groupPropID, groupVal, opts, pal)
					continue
				}
				renderGalleryCard(c, st, view, db, res, ent.rec, coverH, animatePos, gcfg, props, opts, pal)
			}
			for pad := end - start; pad < cols; pad++ {
				ui.Box(c).Grow(1).MinWidth(0)
			}
		})
	}
	_ = cardW
}

func renderGalleryCard(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	rec *database.Record,
	coverH float32,
	animatePos bool,
	gcfg database.GalleryViewConfig,
	props []database.Property,
	opts Options,
	pal Palette,
) {
	cardBg := pal.Paper
	tintBg, tintAccent, tinted := tintedRowBackground(pal, res, rec.ID)
	if tinted {
		cardBg = tintBg
	}
	card := ui.Column(c.Key("gcard:"+rec.ID)).Grow(1).MinWidth(0).Radius(12).
		Background(cardBg).Border(1, pal.Border).Shadow(0, 1, 4, 0, pal.ShadowEdge).
		Cursor(ui.CursorPointer).Clip().Focusable().Role(ui.RoleListItem).Label(rec.Title)

	cardDragging := false
	cardSettling := st.Motion.isSettling(dragKindCard, rec.ID)
	if !opts.ReadOnly && len(view.Sorts) == 0 {
		card.Drag(rowDrag{recordID: rec.ID})
		cardDragging = st.Motion.trackDrag(c, card, dragKindCard, rec.ID, rec.Title, "", pal.Accent, false)
	}
	if cardDragging {
		card.Opacity(0.28)
		card.DrawOver(func(p *ui.Painter, r ui.Rect) {
			paintDashedGhost(p, r, 12, pal.Accent)
		})
	} else if cardSettling {
		card.Opacity(0.16)
	} else {
		card.Transition(ui.ElementTransition{
			Animation:    &dbSpringCard,
			Position:     animatePos,
			Colors:       true,
			Shadows:      true,
			ExitDuration: 150 * time.Millisecond,
			Enter:        &ui.Motion{Y: -8, Scale: 0.96, Opacity: 0.78},
			Exit:         &ui.Motion{Scale: 0.94, Opacity: 0},
		})
	}
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

	hovered := card.Hovered() && !cardDragging
	if hovered {
		card.Border(1, pal.BorderStrong).Shadow(0, 8, 20, -4, pal.ShadowEdge)
	}
	applyCardHoverLift(card, hovered, cardDragging)

	if !opts.ReadOnly && len(view.Sorts) == 0 {
		if d, ok := ui.Drop[rowDrag](card); ok && d.recordID != rec.ID {
			reorderRecordRow(st, view, res, d.recordID, rec.ID, opts)
		}
		if d, over := ui.DragOver[rowDrag](card); over && d.recordID != rec.ID {
			card.Border(1.5, pal.Accent)
		}
	}

	attachRecordContextMenu(card, st, rec, opts)

	var consumedTitle, consumedProps bool
	card.Children(func() {
		renderCardPreview(c, db, rec, gcfg.CoverSource, gcfg.FitImage, coverH, true, opts, pal)
		ui.Column(c.Key("gcard-body")).Padding(10, 12).Gap(6).MinWidth(0).Children(func() {
			consumedTitle = renderCardTitleRow(c, st, db, rec, gcfg.ShowIcon, true, opts, pal)
			consumedProps = renderCardProperties(c, st, rec, props, false, opts, pal)
		})
	})

	if card.Clicked() && !consumedTitle && !consumedProps && opts.OnOpenRecord != nil {
		opts.OnOpenRecord(rec, resolveCardOpenMode(*view))
	}
}

func renderGalleryAddCard(
	c *ui.Context,
	st *State,
	db *database.Database,
	minH float32,
	groupPropID string,
	groupVal database.Value,
	opts Options,
	pal Palette,
) {
	box := ui.Column(c.Key("gadd:" + groupPropID + ":" + groupVal.PlainString())).
		Grow(1).MinWidth(0).MinHeight(minH).Radius(12).Center().Gap(6).
		Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("新建页面")).
		Transition(ui.ElementTransition{Animation: &dbSpringCard, Position: true, Colors: true})
	hovered := box.Hovered()
	if hovered {
		box.Background(pal.RowHover)
	}
	applyPressSpring(box, false)
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		col := pal.BorderStrong
		if hovered {
			col = pal.Accent.Alpha(0.6)
		}
		p.StrokeDashed(rectInset(r, 0.5), col, 12, 1.2)
	})
	box.Children(func() {
		ui.Icon(c, iconPlus).FontSize(18).TextColor(pal.TextMuted)
		ui.Text(c, i18n.T("新建页面")).FontSize(13).TextColor(pal.TextMuted).SingleLine()
	})
	if box.Clicked() {
		var initial map[string]database.Value
		if groupPropID != "" && !groupVal.IsEmpty() {
			initial = map[string]database.Value{groupPropID: groupVal}
			if gp, idx := db.PropertyByID(groupPropID); idx >= 0 {
				if view := st.ActiveView(); view != nil && view.Group != nil {
					if v, ok := resolveGroupDropValue(*gp, *view.Group, nil, "", groupVal.PlainString(), groupVal, opts.Now); ok {
						initial[groupPropID] = v
					}
				}
			}
		}
		createRecordWithInitial(st, db, initial, opts)
	}
}
