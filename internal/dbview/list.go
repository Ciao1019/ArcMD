package dbview

import (
	"fmt"
	"slices"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

type listRowDrag struct {
	recordID     string
	fromGroupKey string
}

func listView(
	c *ui.Context,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	opts Options,
	pal Palette,
) {
	items := buildTableItems(view, res)
	props := visibleNonTitleProps(db, &view)

	ui.Column(c.Key("list-area")).Grow(1).MinWidth(0).MinHeight(0).Padding(8, 22, 14, 22).Children(func() {
		st.ListScroll.KeepOffset = true
		st.ListScroll.MoveAnimation = &dbSpringList
		list := ui.List(c.Key("list-rows"), &st.ListScroll, len(items)).
			Grow(1).MinHeight(0).ItemKey(func(i int) any { return items[i].key })
		list.Rows(func(row ui.ListRow) {
			rc := row.Context
			it := items[row.Index]
			switch it.kind {
			case itemGroupHeader:
				renderListGroupHeader(rc, st, &view, db, res, it.group, opts, pal)
			case itemAddRow:
				renderListAddRow(rc, st, db, it.groupProp, it.groupValue, opts, pal)
			case itemRecord:
				gKey := ""
				var gVal database.Value
				if it.group != nil {
					gKey = it.group.Key
					gVal = it.group.Value
				}
				renderListRecordRow(rc, st, &view, db, res, it.record, props, gKey, gVal, opts, pal)
			}
		})
	})
}

func renderListGroupHeader(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	g *database.RecordGroup,
	opts Options,
	pal Palette,
) {
	if g == nil {
		return
	}
	hdr := ui.Row(c.Key("lgh:"+g.Key)).Height(36).Padding(4, 8, 2, 8).Radius(8).
		Gap(8).AlignItems(ui.Center).Cursor(ui.CursorPointer).
		Role(ui.RoleButton).Label(g.Label).
		Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
	if hdr.Hovered() {
		hdr.Background(pal.RowHover)
	}
	if !opts.ReadOnly && view.Group != nil && view.Group.PropertyID != "" {
		if d, ok := ui.Drop[listRowDrag](hdr); ok && d.fromGroupKey != g.Key {
			if gp, idx := db.PropertyByID(view.Group.PropertyID); idx >= 0 {
				if rec := st.Store.Record(d.recordID); rec != nil {
					if val, vok := resolveGroupDropValue(*gp, *view.Group, rec, d.fromGroupKey, g.Key, g.Value, opts.Now); vok {
						st.setCellAndNotify(d.recordID, gp.ID, val, opts)
					}
				}
			}
		}
		if d, over := ui.DragOver[listRowDrag](hdr); over && d.fromGroupKey != g.Key {
			hdr.Background(pal.AccentSoft)
		}
	}
	hdr.Children(func() {
		renderAnimatedChevron(c, "lgh-chev:"+g.Key, g.Collapsed, 13, pal.TextMuted)
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
	_ = res
}

func renderListAddRow(
	c *ui.Context,
	st *State,
	db *database.Database,
	groupProp string,
	groupVal database.Value,
	opts Options,
	pal Palette,
) {
	if opts.ReadOnly {
		return
	}
	row := ui.Row(c.Key("ladd:"+groupProp+":"+groupVal.PlainString())).Height(36).Padding(0, 10).Radius(8).
		Gap(6).AlignItems(ui.Center).Cursor(ui.CursorPointer).
		Role(ui.RoleButton).Label(i18n.T("新建页面")).
		Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
	addHovered := row.Hovered()
	if addHovered {
		row.Background(pal.RowHover)
	}
	applyPressSpring(row, false)
	ink := pal.TextMuted
	if addHovered {
		ink = pal.Accent
	}
	row.Children(func() {
		ui.Icon(c, iconPlus).FontSize(13).TextColor(ink)
		ui.Text(c, i18n.T("新建页面")).FontSize(12.5).FontWeight(500).TextColor(ink).SingleLine()
	})
	if row.Clicked() {
		var initial map[string]database.Value
		if groupProp != "" && !groupVal.IsEmpty() {
			initial = map[string]database.Value{groupProp: groupVal}
			if gp, idx := db.PropertyByID(groupProp); idx >= 0 {
				if view := st.ActiveView(); view != nil && view.Group != nil {
					if v, ok := resolveGroupDropValue(*gp, *view.Group, nil, "", groupVal.PlainString(), groupVal, opts.Now); ok {
						initial[groupProp] = v
					}
				}
			}
		}
		createRecordWithInitial(st, db, initial, opts)
	}
}

func renderListRecordRow(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	rec *database.Record,
	props []database.Property,
	groupKey string,
	groupVal database.Value,
	opts Options,
	pal Palette,
) {
	if rec == nil {
		return
	}
	row := ui.Row(c.Key("lrec:"+groupKey+":"+rec.ID)).Height(40).Padding(0, 10).Radius(9).Gap(10).
		AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(pal.Border.Alpha(0.5)).
		Cursor(ui.CursorPointer).Focusable().Role(ui.RoleListItem).Label(rec.Title)

	rowDragging := st.Motion.isDragging(dragKindRow, rec.ID)
	rowSettling := st.Motion.isSettling(dragKindRow, rec.ID)
	if rowDragging {
		row.Opacity(0.28)
	} else if rowSettling {
		row.Opacity(0.18)
		st.Motion.trackDrag(c, row, dragKindRow, rec.ID, rec.Title, groupKey, pal.Accent, false)
	}

	tintBg, tintAccent, tinted := tintedRowBackground(pal, res, rec.ID)
	if tinted {
		row.Background(tintBg)
		row.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 3, H: r.H}, tintAccent, 0)
		})
	}
	pulse := st.Motion.pulseAlpha(c, rec.ID)
	if pulse > 0.01 {
		row.DrawOver(func(p *ui.Painter, r ui.Rect) {
			paintCreationPulse(p, r, 9, pulse, pal.Accent)
		})
	}
	if st.Motion.IsRecentlyCreated(rec.ID, c.Now()) {
		row.Transition(ui.ElementTransition{
			Duration: 200 * time.Millisecond,
			Ease:     dbEaseOut,
			Colors:   !tinted,
			Enter:    &ui.Motion{Y: -6, Opacity: 0},
		})
	} else if !tinted {
		row.Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
	}

	hovered := row.Hovered() && !rowDragging
	if hovered {
		if tinted {
			row.Background(tintBg.Mix(pal.Text, 0.03))
		} else {
			row.Background(pal.RowHover)
		}
	}

	if !opts.ReadOnly {
		if d, ok := ui.Drop[listRowDrag](row); ok && d.recordID != rec.ID {
			if d.fromGroupKey != groupKey && view.Group != nil && view.Group.PropertyID != "" {
				if gp, idx := db.PropertyByID(view.Group.PropertyID); idx >= 0 {
					if draggedRec := st.Store.Record(d.recordID); draggedRec != nil {
						if val, vok := resolveGroupDropValue(*gp, *view.Group, draggedRec, d.fromGroupKey, groupKey, groupVal, opts.Now); vok {
							st.setCellAndNotify(d.recordID, gp.ID, val, opts)
						}
					}
				}
			} else if len(view.Sorts) == 0 {
				reorderRecordRow(st, view, res, d.recordID, rec.ID, opts)
			}
		}
		if d, over := ui.DragOver[listRowDrag](row); over && d.recordID != rec.ID {
			row.DrawOver(func(p *ui.Painter, r ui.Rect) {
				paintDropRail(p, r, pal.Accent, false)
			})
		}
	}

	attachRecordContextMenu(row, st, rec, opts)

	var consumedTitle, consumedProps bool
	row.Children(func() {
		if !opts.ReadOnly && len(view.Sorts) == 0 {
			grip := ui.Box(c.Key("lgrip")).Size(14, 22).Center().Cursor(ui.CursorGrab)
			grip.Drag(listRowDrag{recordID: rec.ID, fromGroupKey: groupKey})
			st.Motion.trackDrag(c, grip, dragKindRow, rec.ID, rec.Title, groupKey, pal.Accent, false)
			gTarget := float32(0)
			if hovered || rowDragging {
				gTarget = 1
			}
			gVis := grip.AnimateTo("vis", gTarget, ui.Animation{Duration: 120 * time.Millisecond, Ease: dbEaseOut, NonMotion: true})
			if gVis > 0.02 {
				grip.Children(func() { ui.Icon(c, iconGrip).FontSize(12).TextColor(pal.TextMuted.Alpha(gVis)) })
			}
		}

		ui.Row(c.Key("lleft")).Grow(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
			consumedTitle = renderCardTitleRow(c, st, db, rec, true, false, opts, pal)
		})

		consumedProps = renderCardProperties(c, st, rec, props, true, opts, pal)
	})

	if row.Clicked() && !consumedTitle && !consumedProps && opts.OnOpenRecord != nil {
		opts.OnOpenRecord(rec, resolveCardOpenMode(*view))
	}
}
