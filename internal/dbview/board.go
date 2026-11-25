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

type boardCardDrag struct {
	state        *State
	recordID     string
	fromGroupKey string
	fromSubKey   string
}

type boardColDrag struct {
	key string
}

type boardSwimlane struct {
	key       string
	label     string
	color     string
	val       database.Value
	collapsed bool
	total     int
	cells     map[string][]*database.Record // primaryGroupKey -> records
}

func boardView(
	c *ui.Context,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	opts Options,
	pal Palette,
) {
	var groupProp database.Property
	hasGroup := false
	if view.Group != nil && view.Group.PropertyID != "" {
		if gp, gpIdx := db.PropertyByID(view.Group.PropertyID); gpIdx >= 0 && gp != nil {
			groupProp = *gp
			hasGroup = true
		}
	}

	bcfg := database.BoardViewConfig{
		CardSize:    "medium",
		CoverSource: "none",
		ColorColumn: true,
		ShowCount:   true,
	}
	if view.Board != nil {
		bcfg = *view.Board
	}
	colW, covH := boardDimensions(bcfg.CardSize)

	var subProp *database.Property
	if hasGroup && bcfg.SubGroup != nil && bcfg.SubGroup.PropertyID != "" {
		if sp, sIdx := db.PropertyByID(bcfg.SubGroup.PropertyID); sIdx >= 0 && sp.ID != groupProp.ID {
			subProp = sp
		}
	}

	var excludeIDs []string
	if hasGroup && groupProp.ID != "" {
		excludeIDs = append(excludeIDs, groupProp.ID)
	}
	if subProp != nil {
		excludeIDs = append(excludeIDs, subProp.ID)
	}
	cardProps := visibleNonTitleProps(db, &view, excludeIDs...)

	personProp := resolveBoardAssigneeProp(db, bcfg)
	assignees := collectBoardAssignees(res, personProp)
	if st.BoardAssigneeFilter != "" && !slices.Contains(assignees, st.BoardAssigneeFilter) {
		st.BoardAssigneeFilter = ""
	}

	boardRes := res
	if !hasGroup {
		groupProp = database.Property{Name: i18n.T("全部记录"), Type: database.PropText}
		var allRows []*database.Record
		if res != nil {
			allRows = res.Rows
		}
		var cp database.QueryResult
		if res != nil {
			cp = *res
		}
		cp.Groups = []database.RecordGroup{{
			Key:   "__all__",
			Label: i18n.T("全部记录"),
			Rows:  allRows,
			Count: len(allRows),
		}}
		boardRes = &cp
	}

	overlayCtx := &boardOverlayContext{
		view:      view,
		db:        db,
		res:       boardRes,
		bcfg:      bcfg,
		covH:      covH,
		cardProps: cardProps,
		opts:      opts,
		pal:       pal,
	}
	st.BoardDrag.beginBoardFrame(c, st, view, overlayCtx)

	boardRoot := ui.Column(c.Key("board-root")).Grow(1).MinWidth(0).MinHeight(0)
	if !opts.ReadOnly {
		if d, ok := ui.Drop[boardCardDrag](boardRoot); ok {
			st.BoardDrag.handleBoardDrop(c, st, &view, db, boardRes, d, "", database.EmptyValue(), "", database.EmptyValue(), false, opts)
		}
	}
	boardRoot.Children(func() {
		renderBoardSubToolbar(c, st, &view, db, bcfg, groupProp, personProp, assignees, opts, pal)
		if !hasGroup {
			action := ""
			if !opts.ReadOnly {
				action = "+ " + i18n.T("新建状态列")
			}
			renderFallbackBindingBanner(c, "board-nogroup-banner", i18n.T("当前未设置分组列，已展示全部记录"), action, pal, func() {
				addDefaultStatusProperty(st, db, view, opts)
			})
		}

		if hasGroup && subProp != nil && bcfg.SubGroup != nil {
			renderBoardSwimlanes(c, st, view, db, boardRes, groupProp, *subProp, *bcfg.SubGroup, bcfg, colW, covH, cardProps, personProp, opts, pal)
			return
		}

		renderBoardColumns(c, st, view, db, boardRes, groupProp, bcfg, colW, covH, cardProps, personProp, opts, pal)
	})
	st.BoardDrag.finishBoardFrame(c, st)
}

func boardDimensions(cardSize string) (colW, covH float32) {
	switch cardSize {
	case "small":
		return 228, 108
	case "large":
		return 324, 168
	default:
		return 268, 136
	}
}

func renderBoardColumns(
	c *ui.Context,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	groupProp database.Property,
	bcfg database.BoardViewConfig,
	colW, covH float32,
	cardProps []database.Property,
	personProp *database.Property,
	opts Options,
	pal Palette,
) {
	scroller := ui.ScrollHorizontal(c.Key("board-x")).TrackScroll(&st.BoardScrollX).
		Grow(1).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch)
	scroller.Children(func() {
		ui.Row(c.Key("board-row")).Padding(12, 18, 18, 18).Gap(12).AlignItems(ui.Stretch).MinHeightPercent(100).Children(func() {
			for i := range res.Groups {
				g := res.Groups[i]
				filteredRows := filterRowsByAssignee(g.Rows, personProp, st.BoardAssigneeFilter)
				if g.Collapsed {
					renderCollapsedBoardColumn(c, st, &view, db, res, groupProp, g, len(filteredRows), bcfg.ColorColumn, opts, pal)
					continue
				}
				renderExpandedBoardColumn(c, st, &view, db, res, groupProp, g, filteredRows, bcfg, colW, covH, cardProps, opts, pal)
			}

			if canAddBoardGroup(groupProp, view.Group, opts) {
				renderAddBoardGroupSlot(c, st, groupProp, opts, pal)
			}
		})
	})
}

func renderCollapsedBoardColumn(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	groupProp database.Property,
	g database.RecordGroup,
	visibleCount int,
	colorCol bool,
	opts Options,
	pal Palette,
) {
	bg := columnFillColor(g.Color, colorCol, pal)
	col := ui.Column(c.Key("bcol-collapsed:"+g.Key)).Width(42).Shrink(0).Radius(14).
		Background(bg).Border(1, pal.Border.Alpha(0.7)).Padding(10, 4).Gap(8).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("展开此列") + ": " + g.Label).
		Transition(ui.ElementTransition{Animation: &dbSpringList, Position: true, Width: true, Colors: true})
	if col.Hovered() {
		col.Border(1, pal.BorderStrong)
	}
	applyPressSpring(col, false)

	if !opts.ReadOnly {
		colAccent := columnAccentColor(g.Color, pal)
		st.BoardDrag.curBuckets = append(st.BoardDrag.curBuckets, boardBucketFrame{
			groupKey:  g.Key,
			groupVal:  g.Value,
			collapsed: true,
			r:         col.Bounds(),
			accent:    colAccent,
		})
		if d, ok := ui.Drop[boardCardDrag](col); ok {
			st.BoardDrag.handleBoardDrop(c, st, view, db, res, d, g.Key, g.Value, "", database.EmptyValue(), true, opts)
		}
		if slot := st.BoardDrag.slot; slot.active && slot.collapsed && slot.groupKey == g.Key && slot.subKey == "" {
			col.Border(1.5, colAccent).Background(pal.AccentSoft)
		}
	}

	col.Children(func() {
		ui.Icon(c, iconChevronRight).FontSize(13).TextColor(pal.TextMuted)
		if g.Color != "" && g.Color != "default" {
			ui.Box(c).Size(7, 7).Radius(3.5).Background(StatusDotColor(pal.Dark, g.Color))
		}
		ui.Text(c, fmt.Sprintf("%d", visibleCount)).FontSize(11.5).TextColor(pal.TextMuted).SingleLine()
		ui.Text(c, g.Label).FontSize(12).FontWeight(500).TextColor(pal.TextMuted).MaxWidth(28).TextAlign(ui.Center)
	})

	if col.Clicked() && view.Group != nil {
		vc := view.Clone()
		vc.Group.Collapsed = toggleStringInSlice(slices.Clone(vc.Group.Collapsed), g.Key)
		saveViewAndNotify(st, vc, opts)
	}
}

func renderExpandedBoardColumn(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	groupProp database.Property,
	g database.RecordGroup,
	rows []*database.Record,
	bcfg database.BoardViewConfig,
	colW, covH float32,
	cardProps []database.Property,
	opts Options,
	pal Palette,
) {
	bg := columnFillColor(g.Color, bcfg.ColorColumn, pal)
	colAccent := columnAccentColor(g.Color, pal)
	colDragging := st.Motion.isDragging(dragKindGroup, g.Key)

	col := ui.Column(c.Key("bcol:"+g.Key)).Width(colW).Shrink(0).Radius(14).
		Background(bg).Border(1, pal.Border.Alpha(0.65)).Clip().
		Transition(ui.ElementTransition{Animation: &dbSpringList, Position: true, Width: true, Colors: true})
	if colDragging {
		col.Opacity(0.35)
	}
	colHovered := col.Hovered()

	if !opts.ReadOnly {
		restingCount := 0
		for _, r := range rows {
			if r != nil && !st.BoardDrag.grab.matchesCard(r.ID, g.Key, "") {
				restingCount++
			}
		}
		st.BoardDrag.curBuckets = append(st.BoardDrag.curBuckets, boardBucketFrame{
			groupKey: g.Key,
			groupVal: g.Value,
			r:        col.Bounds(),
			accent:   colAccent,
			count:    restingCount,
		})
		if d, ok := ui.Drop[boardCardDrag](col); ok {
			st.BoardDrag.handleBoardDrop(c, st, view, db, res, d, g.Key, g.Value, "", database.EmptyValue(), false, opts)
		}
		if slot := st.BoardDrag.slot; slot.active && !slot.collapsed && slot.groupKey == g.Key && slot.subKey == "" && st.BoardDrag.grab.active && st.BoardDrag.grab.fromGroupKey != g.Key {
			col.Border(1.5, colAccent.Alpha(0.75)).Background(bg.Mix(colAccent, 0.08))
		}
		if cd, ok := ui.Drop[boardColDrag](col); ok && cd.key != g.Key {
			reorderBoardGroup(st, view, res, cd.key, g.Key, opts)
		}
		if cd, over := ui.DragOver[boardColDrag](col); over && cd.key != g.Key {
			col.DrawOver(func(p *ui.Painter, r ui.Rect) {
				paintDropRail(p, r, colAccent, true)
			})
		}
	}

	var grpCfg database.GroupConfig
	if view.Group != nil {
		grpCfg = *view.Group
	}

	col.Children(func() {
		// DayFlow inline rename box when editing column name.
		if st.BoardRenamingColKey == g.Key && !opts.ReadOnly {
			renderBoardColumnRenameBox(c, st, db, res, groupProp, g, opts, pal)
		} else {
			// Column Header: pill badge + soft count + smoothly fading hover actions (+, ..., fold).
			hdr := ui.Row(c.Key("bhdr:"+g.Key)).Height(42).Padding(4, 8, 2, 10).Gap(6).AlignItems(ui.Center)
			if view.Group != nil && !opts.ReadOnly {
				hdr.Cursor(ui.CursorGrab).Drag(boardColDrag{key: g.Key})
				st.Motion.trackDrag(c, hdr, dragKindGroup, g.Key, g.Label, formatRecordDragSubtitle(len(rows)), colAccent, true)
			}
			hdr.Children(func() {
				if g.Color != "" && g.Color != "default" {
					renderTagPill(c, g.Label, g.Color, groupProp.Type == database.PropStatus, pal)
				} else {
					ui.Box(c).Height(24).Padding(0, 8).Radius(6).Background(pal.Surface).Center().Children(func() {
						ui.Text(c, g.Label).FontSize(13).FontWeight(500).TextColor(pal.Text).SingleLine()
					})
				}
				if bcfg.ShowCount {
					ui.Text(c, fmt.Sprintf("%d", len(rows))).FontSize(12).FontWeight(500).TextColor(colAccent.Alpha(0.78)).SingleLine()
				}
				ui.Spacer(c)

				if !opts.ReadOnly {
					addBtn := ui.Box(c.Key("bhdr-add:"+g.Key)).Size(24, 24).Radius(6).Center().
						Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("在此分组新建") + ": " + g.Label).
						Tooltip(i18n.T("在此分组新建")).
						Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
					addVisTarget := float32(0)
					if colHovered || addBtn.Hovered() {
						addVisTarget = 1
					}
					addVis := addBtn.AnimateTo("vis", addVisTarget, ui.Animation{Duration: 120 * time.Millisecond, Ease: dbEaseOut, NonMotion: true})
					addBtn.Opacity(addVis)
					if addBtn.Hovered() {
						addBtn.Background(colAccent.Alpha(0.14))
					}
					applyPressSpring(addBtn, false)
					if addVis > 0.02 {
						addBtn.Children(func() { ui.Icon(c, iconPlus).FontSize(13).TextColor(colAccent) })
					}
					if addBtn.Clicked() {
						createBoardRecordInBucket(st, db, groupProp, grpCfg, g, nil, nil, database.RecordGroup{}, opts)
					}

					if canEditBoardOption(groupProp, view.Group, g.Key) {
						menuBtn := ui.Box(c.Key("bhdr-menu:"+g.Key)).Size(24, 24).Radius(6).Center().
							Cursor(ui.CursorPointer).Role(ui.RoleMenuButton).Label(i18n.T("列菜单") + ": " + g.Label).
							Tooltip(i18n.T("列菜单")).
							Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
						menuVisTarget := float32(0)
						if colHovered || menuBtn.Hovered() {
							menuVisTarget = 1
						}
						menuVis := menuBtn.AnimateTo("vis", menuVisTarget, ui.Animation{Duration: 120 * time.Millisecond, Ease: dbEaseOut, NonMotion: true})
						menuBtn.Opacity(menuVis)
						if menuBtn.Hovered() {
							menuBtn.Background(colAccent.Alpha(0.14))
						}
						applyPressSpring(menuBtn, false)
						if menuVis > 0.02 {
							menuBtn.Children(func() { ui.Icon(c, iconMoreHoriz).FontSize(13).TextColor(colAccent) })
						}
						menuBtn.PopoverMenuButton(func(m *ui.Menu) {
							if m.Item(i18n.T("重命名列")).Chosen() {
								st.BoardRenamingColKey = g.Key
								st.BoardRenamingColDraft = g.Label
								st.BoardRenamingColPending = true
							}
							m.Submenu(i18n.T("更改颜色"), func(sub *ui.Menu) {
								for _, colName := range database.OptionColors {
									cn := colName
									if sub.Item(i18n.T(cn)).Checked(g.Color == cn).Chosen() {
										updateBoardOptionColor(st, groupProp, g.Key, cn, opts)
									}
								}
							})
							m.Separator()
							if m.Item(i18n.T("折叠此列")).Chosen() && view.Group != nil {
								vc := view.Clone()
								vc.Group.Collapsed = toggleStringInSlice(slices.Clone(vc.Group.Collapsed), g.Key)
								saveViewAndNotify(st, vc, opts)
							}
						})
					}
				}

				if view.Group != nil {
					foldBtn := ui.Box(c.Key("bhdr-fold:"+g.Key)).Size(24, 24).Radius(6).Center().
						Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("折叠此列") + ": " + g.Label).
						Tooltip(i18n.T("折叠此列")).
						Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
					if foldBtn.Hovered() {
						foldBtn.Background(colAccent.Alpha(0.14))
					}
					applyPressSpring(foldBtn, false)
					foldBtn.Children(func() { ui.Icon(c, iconChevronLeft).FontSize(13).TextColor(pal.TextMuted) })
					if foldBtn.Clicked() {
						vc := view.Clone()
						vc.Group.Collapsed = toggleStringInSlice(slices.Clone(vc.Group.Collapsed), g.Key)
						saveViewAndNotify(st, vc, opts)
					}
				}
			})
		}

		// Cards Scroll Body.
		ui.Scroll(c.Key("bcol-sc:" + g.Key)).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c.Key("bcards:"+g.Key)).Padding(2, 8, 10, 8).Gap(8).Children(func() {
				slot := st.BoardDrag.slot
				slotHere := !opts.ReadOnly && slot.active && !slot.collapsed && slot.groupKey == g.Key && slot.subKey == ""
				restIdx := 0
				for _, r := range rows {
					if r == nil {
						continue
					}
					isDraggedRec := st.BoardDrag.grab.matchesCard(r.ID, g.Key, "")
					if !isDraggedRec && slotHere && !st.BoardDrag.emitted && slot.index <= restIdx {
						st.BoardDrag.renderDragSlot(c, colAccent, g.Key, "", slot.index, slot.self)
					}
					dragging, justStarted := renderBoardCard(c, st, view, db, res, r, g.Key, "", restIdx, bcfg, covH, cardProps, colAccent, opts, pal)
					if justStarted && !st.BoardDrag.emitted {
						st.BoardDrag.renderDragSlot(c, colAccent, g.Key, "", restIdx, true)
					}
					if !dragging {
						restIdx++
					}
				}
				if slotHere && !st.BoardDrag.emitted {
					st.BoardDrag.renderDragSlot(c, colAccent, g.Key, "", slot.index, slot.self)
				}

				if !opts.ReadOnly {
					footAdd := ui.Row(c.Key("bfoot-add:"+g.Key)).Height(32).Padding(0, 9).Radius(8).Gap(6).
						AlignItems(ui.Center).Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("新建") + ": " + g.Label).
						Transition(ui.ElementTransition{Animation: &boardListAnim, Position: true, Colors: true})
					if footAdd.Hovered() {
						footAdd.Background(colAccent.Alpha(0.12))
					}
					applyPressSpring(footAdd, false)
					footAdd.Children(func() {
						ui.Icon(c, iconPlus).FontSize(13).TextColor(colAccent.Alpha(0.8))
						ui.Text(c, i18n.T("新建")).FontSize(12.5).FontWeight(500).TextColor(colAccent.Alpha(0.85)).SingleLine()
					})
					if footAdd.Clicked() {
						createBoardRecordInBucket(st, db, groupProp, grpCfg, g, nil, nil, database.RecordGroup{}, opts)
					}
				}
			})
		})
	})
}

func renderBoardCard(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	rec *database.Record,
	fromGroupKey string,
	fromSubKey string,
	restIndex int,
	bcfg database.BoardViewConfig,
	covH float32,
	cardProps []database.Property,
	colAccent ui.Color,
	opts Options,
	pal Palette,
) (dragging bool, justStarted bool) {
	borderCol := pal.Border.Alpha(0.85)
	cardBg := pal.Paper
	tintBg, tintAccent, tinted := tintedRowBackground(pal, res, rec.ID)
	if tinted {
		cardBg = tintBg
	}
	isFocused := st.BoardFocusedCardID == rec.ID
	card := ui.Column(c.Key("bcard:" + fromGroupKey + ":" + fromSubKey + ":" + rec.ID))

	if !opts.ReadOnly {
		card.Drag(boardCardDrag{
			state:        st,
			recordID:     rec.ID,
			fromGroupKey: fromGroupKey,
			fromSubKey:   fromSubKey,
		})
	}

	if !opts.ReadOnly && card.Dragging() {
		justStarted = st.BoardDrag.startOrUpdateGrab(c, st, rec, card, fromGroupKey, fromSubKey, restIndex, colAccent)
		// Keep the source node alive with zero layout footprint so MyGo's drag
		// stays active while our floating card overlay owns all visuals (no ghost overlap).
		card.Absolute().Size(0, 0).Background(ui.Transparent).Transition(ui.ElementTransition{Colors: true})
		return true, justStarted
	}

	// On the exact release frame (before QueryResult refreshes on the next frame),
	// keep the dragged card collapsed in its source column so it never flashes back.
	if st.BoardDrag.grab.matchesCard(rec.ID, fromGroupKey, fromSubKey) {
		card.Absolute().Size(0, 0).Background(ui.Transparent).Transition(ui.ElementTransition{Colors: true})
		return true, false
	}

	settling := st.BoardDrag.settle.matchesCard(rec.ID, fromGroupKey, fromSubKey)
	var settleK float32
	if settling {
		elapsed := c.Now().Sub(st.BoardDrag.settle.started)
		if c.Preferences().ReduceMotion || elapsed >= boardDragSettleDur {
			st.BoardDrag.settle = boardDragSettle{}
			st.Motion.settle = dbDragSettle{}
			st.Motion.suppressOverlay = false
			settling = false
		} else {
			settleK = boardListEase(clampF32(float32(elapsed)/float32(boardDragSettleDur), 0, 1))
			tr := card.LayoutBounds()
			if tr.W <= 0 || tr.H <= 0 {
				tr = card.Bounds()
			}
			if tr.W > 0 && tr.H > 0 {
				st.BoardDrag.settle.toRect = tr
				st.BoardDrag.settle.hasTarget = true
			}
		}
	}

	if settling {
		slotFade := 1 - settleK
		fillCol, dashCol := boardSlotColors(colAccent, slotFade, pal.Dark)
		card.Radius(11).Background(ui.Transparent).Transition(ui.ElementTransition{Colors: true})
		card.Draw(func(p *ui.Painter, r ui.Rect) {
			fr := rectInset(r, 0.5)
			if fillCol.A > 0 {
				p.Fill(fr, fillCol, 11)
			}
			if dashCol.A > 0 {
				p.StrokeDashed(fr, dashCol, 11, 1.25)
			}
		})
		// Preserve exact card height invisibly while the floating card glides in.
		roOpts := opts
		roOpts.ReadOnly = true
		card.Children(func() {
			ui.Column(c.Key("bcard-settle-hold")).Opacity(0).IgnoreInput(true).Children(func() {
				renderBoardCardContent(c, st, db, rec, bcfg, covH, cardProps, roOpts, pal)
			})
		})
		st.BoardDrag.curCards = append(st.BoardDrag.curCards, boardCardFrame{
			recordID: rec.ID,
			groupKey: fromGroupKey,
			subKey:   fromSubKey,
			index:    restIndex,
			r:        card.Bounds(),
			target:   card.LayoutBounds(),
		})
		return false, false
	}

	card.Radius(11).Background(cardBg).Border(1, borderCol).
		Shadow(0, 1, 3, 0, pal.ShadowEdge).Cursor(ui.CursorPointer).Clip().
		Focusable().Role(ui.RoleListItem).Label(rec.Title).
		Transition(ui.ElementTransition{
			Animation:    &boardListAnim,
			Position:     true,
			Colors:       true,
			Shadows:      true,
			ExitDuration: 150 * time.Millisecond,
			Enter:        &ui.Motion{Y: -6, Scale: 0.97, Opacity: 0.78},
			Exit:         &ui.Motion{Scale: 0.95, Opacity: 0},
		})

	st.BoardDrag.curCards = append(st.BoardDrag.curCards, boardCardFrame{
		recordID: rec.ID,
		groupKey: fromGroupKey,
		subKey:   fromSubKey,
		index:    restIndex,
		r:        card.Bounds(),
		target:   card.LayoutBounds(),
	})

	if tinted {
		card.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 3, H: r.H}, tintAccent, 0)
		})
	}
	pulse := st.Motion.pulseAlpha(c, rec.ID)
	if pulse > 0.01 {
		card.DrawOver(func(p *ui.Painter, r ui.Rect) {
			paintCreationPulse(p, r, 11, pulse, colAccent)
		})
	}

	if isFocused {
		card.Border(1.5, pal.Accent)
		if st.BoardFocusPending {
			card.Focus()
			st.BoardFocusPending = false
		}
	}
	cardHovered := card.Hovered() && !st.BoardDrag.grab.active
	if cardHovered {
		card.Shadow(0, 6, 16, -3, pal.ShadowEdge)
		if !isFocused {
			card.Border(1, pal.BorderStrong)
		}
	}
	applyCardHoverLift(card, cardHovered, false)
	if card.Focused() {
		st.BoardFocusedCardID = rec.ID
	}
	if !opts.ReadOnly {
		// DayFlow keyboard card move: Alt+Left/Right between columns, Alt+Up/Down within column.
		if card.Focused() || isFocused {
			switch {
			case card.Shortcut(ui.Alt, ui.KeyLeft):
				moveBoardCardHoriz(st, view, res, db, rec, fromGroupKey, fromSubKey, -1, opts)
			case card.Shortcut(ui.Alt, ui.KeyRight):
				moveBoardCardHoriz(st, view, res, db, rec, fromGroupKey, fromSubKey, 1, opts)
			case card.Shortcut(ui.Alt, ui.KeyUp):
				moveBoardCardVert(st, view, res, fromGroupKey, rec.ID, -1, opts)
			case card.Shortcut(ui.Alt, ui.KeyDown):
				moveBoardCardVert(st, view, res, fromGroupKey, rec.ID, 1, opts)
			}
		}
	}

	attachRecordContextMenu(card, st, rec, opts)

	var consumedTitle, consumedProps, consumedDF bool
	card.Children(func() {
		consumedTitle, consumedProps, consumedDF = renderBoardCardContent(c, st, db, rec, bcfg, covH, cardProps, opts, pal)
	})

	if card.Clicked() && !consumedTitle && !consumedProps && !consumedDF && opts.OnOpenRecord != nil {
		st.BoardFocusedCardID = rec.ID
		opts.OnOpenRecord(rec, resolveCardOpenMode(*view))
	}
	return false, false
}

func renderBoardCardContent(
	c *ui.Context,
	st *State,
	db *database.Database,
	rec *database.Record,
	bcfg database.BoardViewConfig,
	covH float32,
	cardProps []database.Property,
	opts Options,
	pal Palette,
) (consumedTitle, consumedProps, consumedDF bool) {
	renderCardPreview(c, db, rec, bcfg.CoverSource, bcfg.FitImage, covH, false, opts, pal)
	ui.Column(c.Key("bcard-body")).Padding(10, 11).Gap(6).MinWidth(0).Children(func() {
		priProp, dateProp, personProp, extraProps := splitDayflowBoardProps(db, bcfg, cardProps)

		// Title + DayFlow 3-bar Priority Signal Indicator in Head Row.
		if priProp != nil && !rec.ValueFor(*priProp).IsEmpty() {
			ui.Row(c.Key("bcard-head")).Gap(6).AlignItems(ui.Start).MinWidth(0).Children(func() {
				ui.Box(c).Grow(1).MinWidth(0).Children(func() {
					consumedTitle = renderCardTitleRow(c, st, db, rec, true, true, opts, pal)
				})
				if renderDayflowPriorityBadge(c, st, rec, *priProp, opts, pal) {
					consumedDF = true
				}
			})
		} else {
			consumedTitle = renderCardTitleRow(c, st, db, rec, true, true, opts, pal)
		}

		if len(extraProps) > 0 {
			consumedProps = renderCardProperties(c, st, rec, extraProps, false, opts, pal)
		}

		if renderDayflowCardFooter(c, st, db, rec, dateProp, personProp, opts, pal) {
			consumedDF = true
		}
	})
	return consumedTitle, consumedProps, consumedDF
}

func renderBoardSwimlanes(
	c *ui.Context,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	groupProp database.Property,
	subProp database.Property,
	subCfg database.GroupConfig,
	bcfg database.BoardViewConfig,
	colW, covH float32,
	cardProps []database.Property,
	personProp *database.Property,
	opts Options,
	pal Palette,
) {
	lanes := collectBoardSwimlanes(res.Groups, subCfg)

	scrollerX := ui.ScrollHorizontal(c.Key("board-swim-x")).TrackScroll(&st.BoardScrollX).
		Grow(1).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch)
	scrollerX.Children(func() {
		ui.Column(c.Key("board-swim-root")).Padding(12, 18, 18, 18).Gap(8).Grow(1).MinHeight(0).Children(func() {
			// Column Header Row aligned with cells below.
			ui.Row(c.Key("bswim-colhdrs")).Gap(12).AlignItems(ui.Center).Children(func() {
				for _, g := range res.Groups {
					if g.Collapsed {
						hdr := ui.Box(c.Key("bsw-ch:" + g.Key)).Width(42).Height(34).Radius(8).
							Background(columnFillColor(g.Color, bcfg.ColorColumn, pal)).
							Center().Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("展开此列") + ": " + g.Label)
						hdr.Children(func() {
							ui.Icon(c, iconChevronRight).FontSize(13).TextColor(pal.TextMuted)
						})
						if hdr.Clicked() {
							vc := view.Clone()
							vc.Group.Collapsed = toggleStringInSlice(slices.Clone(vc.Group.Collapsed), g.Key)
							saveViewAndNotify(st, vc, opts)
						}
						continue
					}
					hdr := ui.Row(c.Key("bsw-ch:"+g.Key)).Width(colW).Height(36).Padding(0, 10).Radius(8).
						Background(columnFillColor(g.Color, bcfg.ColorColumn, pal)).
						Gap(6).AlignItems(ui.Center)
					hdr.Children(func() {
						if g.Color != "" && g.Color != "default" {
							renderTagPill(c, g.Label, g.Color, groupProp.Type == database.PropStatus, pal)
						} else {
							ui.Text(c, g.Label).FontSize(13).FontWeight(600).TextColor(pal.Text).SingleLine()
						}
						if bcfg.ShowCount {
							cnt := len(filterRowsByAssignee(g.Rows, personProp, st.BoardAssigneeFilter))
							ui.Text(c, fmt.Sprintf("%d", cnt)).FontSize(12).TextColor(pal.TextMuted).SingleLine()
						}
						ui.Spacer(c)
						fold := ui.Box(c.Key("bsw-fold:"+g.Key)).Size(22, 22).Radius(4).Center().
							Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("折叠此列") + ": " + g.Label)
						if fold.Hovered() {
							fold.Background(pal.RowHover)
						}
						fold.Children(func() { ui.Icon(c, iconChevronLeft).FontSize(12).TextColor(pal.TextMuted) })
						if fold.Clicked() {
							vc := view.Clone()
							vc.Group.Collapsed = toggleStringInSlice(slices.Clone(vc.Group.Collapsed), g.Key)
							saveViewAndNotify(st, vc, opts)
						}
					})
				}
			})

			// Vertically scrollable Swimlane Matrix.
			ui.Scroll(c.Key("board-swim-y")).TrackScroll(&st.BoardScrollY).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c.Key("bswim-lanes")).Gap(10).Children(func() {
					for _, lane := range lanes {
						ln := lane
						ui.Column(c.Key("bsw-lane:" + ln.key)).Gap(6).Children(func() {
							// Swimlane Header.
							lhdr := ui.Row(c.Key("bsw-lhdr:"+ln.key)).Height(32).Padding(0, 10).Radius(7).
								Background(pal.Surface).Gap(6).AlignItems(ui.Center).Cursor(ui.CursorPointer).
								Role(ui.RoleButton).Label(ln.label)
							if lhdr.Hovered() {
								lhdr.Background(pal.RowHover)
							}
							lhdr.Children(func() {
								chev := iconChevronDown
								if ln.collapsed {
									chev = iconChevronRight
								}
								ui.Icon(c, chev).FontSize(13).TextColor(pal.TextMuted)
								if ln.color != "" && ln.color != "default" {
									renderTagPill(c, ln.label, ln.color, subProp.Type == database.PropStatus, pal)
								} else {
									ui.Text(c, ln.label).FontSize(13).FontWeight(600).TextColor(pal.Text).SingleLine()
								}
								ui.Text(c, fmt.Sprintf("%d", ln.total)).FontSize(12).TextColor(pal.TextMuted).SingleLine()
							})
							if lhdr.Clicked() && view.Board != nil && view.Board.SubGroup != nil {
								vc := view.Clone()
								vc.Board.SubGroup.Collapsed = toggleStringInSlice(slices.Clone(vc.Board.SubGroup.Collapsed), ln.key)
								saveViewAndNotify(st, vc, opts)
							}

							if ln.collapsed {
								return
							}

							// Swimlane Cells Row.
							ui.Row(c.Key("bsw-row:" + ln.key)).Gap(12).AlignItems(ui.Stretch).Children(func() {
								for _, g := range res.Groups {
									bg := columnFillColor(g.Color, bcfg.ColorColumn, pal)
									colAccent := columnAccentColor(g.Color, pal)
									if g.Collapsed {
										cell := ui.Box(c.Key("bsw-c:"+g.Key)).Width(42).MinHeight(64).Radius(9).
											Background(bg).Border(1, ui.Transparent)
										if !opts.ReadOnly {
											st.BoardDrag.curBuckets = append(st.BoardDrag.curBuckets, boardBucketFrame{
												groupKey:  g.Key,
												groupVal:  g.Value,
												subKey:    ln.key,
												subVal:    ln.val,
												collapsed: true,
												r:         cell.Bounds(),
												accent:    colAccent,
											})
											if d, ok := ui.Drop[boardCardDrag](cell); ok {
												st.BoardDrag.handleBoardDrop(c, st, &view, db, res, d, g.Key, g.Value, ln.key, ln.val, true, opts)
											}
											if slot := st.BoardDrag.slot; slot.active && slot.collapsed && slot.groupKey == g.Key && slot.subKey == ln.key {
												cell.Border(1.5, colAccent).Background(pal.AccentSoft)
											}
										}
										continue
									}

									cellRows := filterRowsByAssignee(ln.cells[g.Key], personProp, st.BoardAssigneeFilter)
									cell := ui.Column(c.Key("bsw-cell:"+g.Key)).Width(colW).MinHeight(72).Radius(10).
										Background(bg).Border(1, ui.Transparent).Padding(8).Gap(7)
									if !opts.ReadOnly {
										restingCount := 0
										for _, r := range cellRows {
											if r != nil && !st.BoardDrag.grab.matchesCard(r.ID, g.Key, ln.key) {
												restingCount++
											}
										}
										st.BoardDrag.curBuckets = append(st.BoardDrag.curBuckets, boardBucketFrame{
											groupKey: g.Key,
											groupVal: g.Value,
											subKey:   ln.key,
											subVal:   ln.val,
											r:        cell.Bounds(),
											accent:   colAccent,
											count:    restingCount,
										})
										if d, ok := ui.Drop[boardCardDrag](cell); ok {
											st.BoardDrag.handleBoardDrop(c, st, &view, db, res, d, g.Key, g.Value, ln.key, ln.val, false, opts)
										}
										if slot := st.BoardDrag.slot; slot.active && !slot.collapsed && slot.groupKey == g.Key && slot.subKey == ln.key && st.BoardDrag.grab.active && (st.BoardDrag.grab.fromGroupKey != g.Key || st.BoardDrag.grab.fromSubKey != ln.key) {
											cell.Border(1.5, colAccent)
										}
									}
									cell.Children(func() {
										slot := st.BoardDrag.slot
										slotHere := !opts.ReadOnly && slot.active && !slot.collapsed && slot.groupKey == g.Key && slot.subKey == ln.key
										restIdx := 0
										for _, r := range cellRows {
											if r == nil {
												continue
											}
											isDraggedRec := st.BoardDrag.grab.matchesCard(r.ID, g.Key, ln.key)
											if !isDraggedRec && slotHere && !st.BoardDrag.emitted && slot.index <= restIdx {
												st.BoardDrag.renderDragSlot(c, colAccent, g.Key, ln.key, slot.index, slot.self)
											}
											dragging, justStarted := renderBoardCard(c, st, &view, db, res, r, g.Key, ln.key, restIdx, bcfg, covH, cardProps, colAccent, opts, pal)
											if justStarted && !st.BoardDrag.emitted {
												st.BoardDrag.renderDragSlot(c, colAccent, g.Key, ln.key, restIdx, true)
											}
											if !dragging {
												restIdx++
											}
										}
										if slotHere && !st.BoardDrag.emitted {
											st.BoardDrag.renderDragSlot(c, colAccent, g.Key, ln.key, slot.index, slot.self)
										}
										if !opts.ReadOnly {
											addCell := ui.Row(c.Key("bsw-add:"+g.Key+":"+ln.key)).Height(28).Padding(0, 8).Radius(6).
												Gap(4).AlignItems(ui.Center).Cursor(ui.CursorPointer).Role(ui.RoleButton).
												Label(i18n.T("新建") + ": " + g.Label + " / " + ln.label)
											if addCell.Hovered() {
												addCell.Background(pal.RowHover)
											}
											addCell.Children(func() {
												ui.Icon(c, iconPlus).FontSize(12).TextColor(pal.TextMuted)
												ui.Text(c, i18n.T("新建")).FontSize(12).TextColor(pal.TextMuted).SingleLine()
											})
											if addCell.Clicked() {
												lg := database.RecordGroup{Key: ln.key, Label: ln.label, Value: ln.val}
												createBoardRecordInBucket(st, db, groupProp, *view.Group, g, &subProp, &subCfg, lg, opts)
											}
										}
									})
								}
							})
						})
					}
				})
			})
		})
	})
}

func collectBoardSwimlanes(cols []database.RecordGroup, subCfg database.GroupConfig) []boardSwimlane {
	collapsedSet := make(map[string]bool, len(subCfg.Collapsed))
	for _, k := range subCfg.Collapsed {
		collapsedSet[k] = true
	}

	idxMap := make(map[string]int)
	var lanes []boardSwimlane

	for _, col := range cols {
		for _, sub := range col.SubGroups {
			idx, ok := idxMap[sub.Key]
			if !ok {
				idx = len(lanes)
				idxMap[sub.Key] = idx
				lanes = append(lanes, boardSwimlane{
					key:       sub.Key,
					label:     sub.Label,
					color:     sub.Color,
					val:       sub.Value,
					collapsed: collapsedSet[sub.Key],
					cells:     make(map[string][]*database.Record),
				})
			}
			if len(sub.Rows) > 0 {
				lanes[idx].cells[col.Key] = append(lanes[idx].cells[col.Key], sub.Rows...)
				lanes[idx].total += len(sub.Rows)
			}
		}
	}

	if subCfg.HideEmpty {
		lanes = slices.DeleteFunc(lanes, func(l boardSwimlane) bool { return l.total == 0 })
	}
	return lanes
}

func canAddBoardGroup(groupProp database.Property, grp *database.GroupConfig, opts Options) bool {
	if opts.ReadOnly || grp == nil {
		return false
	}
	if groupProp.Type == database.PropSelect {
		return true
	}
	return groupProp.Type == database.PropStatus && !strings.EqualFold(grp.StatusBy, "group")
}

func renderAddBoardGroupSlot(
	c *ui.Context,
	st *State,
	groupProp database.Property,
	opts Options,
	pal Palette,
) {
	if !st.AddingGroup {
		btn := ui.Row(c.Key("board-add-grp")).Width(148).Height(38).Padding(0, 12).Radius(12).
			Gap(6).AlignItems(ui.Center).Background(pal.Surface).
			Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("添加分组")).
			Transition(ui.ElementTransition{Animation: &dbSpringList, Position: true, Colors: true})
		hovered := btn.Hovered()
		if hovered {
			btn.Background(pal.RowHover)
		}
		btn.Draw(func(p *ui.Painter, r ui.Rect) {
			col := pal.BorderStrong
			if hovered {
				col = pal.Accent.Alpha(0.55)
			}
			p.StrokeDashed(rectInset(r, 0.5), col, 12, 1.1)
		})
		applyPressSpring(btn, false)
		btn.Children(func() {
			ui.Icon(c, iconPlus).FontSize(13).TextColor(pal.TextMuted)
			ui.Text(c, i18n.T("添加分组")).FontSize(13).TextColor(pal.TextMuted).SingleLine()
		})
		if btn.Clicked() {
			st.CloseToolbarPopovers()
			st.AddingGroup = true
			st.NewGroupName = ""
			st.NewGroupFocusPending = true
		}
		return
	}

	commit := func() {
		name := strings.TrimSpace(st.NewGroupName)
		st.AddingGroup = false
		st.NewGroupName = ""
		if name == "" {
			return
		}
		for _, opt := range groupProp.Options {
			if strings.EqualFold(opt.Name, name) {
				return
			}
		}
		updated := groupProp.Clone()
		newOpt := database.SelectOption{
			ID:    "opt_" + database.NewID()[:6],
			Name:  name,
			Color: ensureOptionColor(len(updated.Options)),
		}
		if updated.Type == database.PropStatus {
			newOpt.Group = database.StatusTodo
		}
		updated.Options = append(updated.Options, newOpt)
		if _, err := st.Store.UpdateProperty(updated); err == nil && opts.OnChanged != nil {
			opts.OnChanged(nil)
		}
	}

	ui.Column(c.Key("board-new-grp-box")).Width(216).Padding(10).Radius(12).Background(pal.HeaderBg).
		Border(1, pal.Accent).Gap(8).Shrink(0).
		Transition(ui.ElementTransition{
			Animation: &dbSpringSnappy,
			Enter:     &ui.Motion{Scale: 0.96, Opacity: 0},
		}).Children(func() {
		in := ui.TextInput(c.Key("board-new-grp-in"), &st.NewGroupName).
			Placeholder(i18n.T("分组名称…")).Label(i18n.T("分组名称")).
			Height(30).Padding(4, 8).Radius(6).Background(pal.Paper).TextColor(pal.Text).AutoFocus()
		if st.NewGroupFocusPending {
			in.Focus()
			st.NewGroupFocusPending = false
		}
		if in.Shortcut(0, ui.KeyEscape) {
			st.AddingGroup = false
			st.NewGroupName = ""
			return
		}
		if in.Submitted() {
			commit()
			return
		}
		ui.Row(c).Gap(6).Justify(ui.End).Children(func() {
			if ui.Button(c.Key("bng-cancel"), i18n.T("取消")).Height(26).Padding(0, 8).Clicked() {
				st.AddingGroup = false
				st.NewGroupName = ""
			}
			if ui.PrimaryButton(c.Key("bng-ok"), i18n.T("创建")).Height(26).Padding(0, 10).Clicked() {
				commit()
			}
		})
	})
}

func columnFillColor(colorName string, colorColumn bool, pal Palette) ui.Color {
	if !colorColumn || colorName == "" || colorName == "default" {
		return pal.HeaderBg
	}
	dot := StatusDotColor(pal.Dark, colorName)
	ratio := float32(0.045)
	if pal.Dark {
		ratio = 0.075
	}
	return pal.Paper.Mix(dot, ratio)
}

func resolveGroupDropValue(
	prop database.Property,
	cfg database.GroupConfig,
	rec *database.Record,
	fromKey, toKey string,
	toVal database.Value,
	now time.Time,
) (database.Value, bool) {
	switch prop.Type {
	case database.PropTitle, database.PropCreatedTime, database.PropEditedTime,
		database.PropUniqueID, database.PropFormula, database.PropRollup:
		return database.EmptyValue(), false

	case database.PropStatus:
		if toKey == "" {
			return database.EmptyValue(), true
		}
		if strings.EqualFold(cfg.StatusBy, "group") {
			wantGrp := database.StatusGroup(toKey)
			for _, opt := range prop.Options {
				eff := opt.Group
				if eff == "" {
					eff = database.StatusTodo
				}
				if eff == wantGrp {
					return database.TextValue(opt.Name), true
				}
			}
			return database.EmptyValue(), false
		}
		return database.TextValue(toKey), true

	case database.PropSelect:
		if toKey == "" {
			return database.EmptyValue(), true
		}
		return database.TextValue(toKey), true

	case database.PropCheckbox:
		return database.BoolValue(toKey == "true"), true

	case database.PropMultiSelect, database.PropPerson, database.PropRelation, database.PropFiles:
		if toKey == "" {
			return database.EmptyValue(), true
		}
		var cur []string
		if rec != nil {
			cur = slices.Clone(rec.ValueFor(prop).List)
		}
		if fromKey != "" {
			cur = slices.DeleteFunc(cur, func(s string) bool { return s == fromKey })
		}
		if !slices.Contains(cur, toKey) {
			cur = append(cur, toKey)
		}
		return database.ListValue(cur...), true

	case database.PropDate:
		if toKey == "" {
			return database.EmptyValue(), true
		}
		if cfg.DateBy == "" || cfg.DateBy == "day" {
			return database.DateRangeValue(toKey, "", false), true
		}
		if strings.EqualFold(cfg.DateBy, "relative") && toKey == "today" {
			t := now
			if t.IsZero() {
				t = time.Now()
			}
			return database.DateRangeValue(t.Format("2006-01-02"), "", false), true
		}
		return database.EmptyValue(), false

	case database.PropNumber:
		if toKey == "" {
			return database.EmptyValue(), true
		}
		if cfg.NumberStep > 0 {
			return database.EmptyValue(), false
		}
		return toVal, true

	default:
		if toKey == "" {
			return database.EmptyValue(), true
		}
		return database.TextValue(toKey), true
	}
}

func applyBoardCardDrop(
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	drag boardCardDrag,
	toGroupKey string,
	toGroupVal database.Value,
	toSubKey string,
	toSubVal database.Value,
	beforeRecordID string,
	opts Options,
) {
	if st == nil || st.Store == nil || view == nil || db == nil || opts.ReadOnly {
		return
	}
	if drag.state != nil && drag.state != st {
		return
	}
	rec := st.Store.Record(drag.recordID)
	if rec == nil {
		return
	}

	var edits []database.CellEdit
	if view.Group != nil && view.Group.PropertyID != "" && drag.fromGroupKey != toGroupKey {
		if prop, idx := db.PropertyByID(view.Group.PropertyID); idx >= 0 {
			if nextVal, ok := resolveGroupDropValue(*prop, *view.Group, rec, drag.fromGroupKey, toGroupKey, toGroupVal, opts.Now); ok {
				edits = append(edits, database.CellEdit{
					RecordID: drag.recordID,
					PropID:   prop.ID,
					Value:    nextVal,
				})
			}
		}
	}

	if view.Board != nil && view.Board.SubGroup != nil && view.Board.SubGroup.PropertyID != "" && drag.fromSubKey != toSubKey {
		if subProp, idx := db.PropertyByID(view.Board.SubGroup.PropertyID); idx >= 0 {
			if nextVal, ok := resolveGroupDropValue(*subProp, *view.Board.SubGroup, rec, drag.fromSubKey, toSubKey, toSubVal, opts.Now); ok {
				edits = append(edits, database.CellEdit{
					RecordID: drag.recordID,
					PropID:   subProp.ID,
					Value:    nextVal,
				})
			}
		}
	}

	if len(edits) > 0 {
		affected, err := st.Store.SetCellsBatch(edits)
		if err != nil {
			if opts.OnNotify != nil {
				opts.OnNotify(err.Error())
			}
			return
		}
		if opts.OnChanged != nil {
			opts.OnChanged(affected)
		}
		return
	}

	if beforeRecordID != "" && beforeRecordID != drag.recordID && len(view.Sorts) == 0 {
		reorderRecordRow(st, view, res, drag.recordID, beforeRecordID, opts)
	}
}

func createBoardRecordInBucket(
	st *State,
	db *database.Database,
	groupProp database.Property,
	groupCfg database.GroupConfig,
	g database.RecordGroup,
	subProp *database.Property,
	subCfg *database.GroupConfig,
	subGroup database.RecordGroup,
	opts Options,
) {
	initial := make(map[string]database.Value, 2)
	if groupProp.ID != "" {
		if val, ok := resolveGroupDropValue(groupProp, groupCfg, nil, "", g.Key, g.Value, opts.Now); ok && !val.IsEmpty() {
			initial[groupProp.ID] = val
		}
	}
	if subProp != nil && subCfg != nil {
		if val, ok := resolveGroupDropValue(*subProp, *subCfg, nil, "", subGroup.Key, subGroup.Value, opts.Now); ok && !val.IsEmpty() {
			initial[subProp.ID] = val
		}
	}
	createRecordWithInitial(st, db, initial, opts)
}

func reorderBoardGroup(
	st *State,
	view *database.ViewConfig,
	res *database.QueryResult,
	fromKey, toKey string,
	opts Options,
) {
	if view == nil || view.Group == nil || res == nil || len(res.Groups) == 0 || opts.ReadOnly {
		return
	}
	order := make([]string, len(res.Groups))
	for i, g := range res.Groups {
		order[i] = g.Key
	}
	fromIdx := slices.Index(order, fromKey)
	toIdx := slices.Index(order, toKey)
	if fromIdx < 0 || toIdx < 0 || fromIdx == toIdx {
		return
	}
	order = slices.Delete(order, fromIdx, fromIdx+1)
	order = slices.Insert(order, toIdx, fromKey)

	vc := view.Clone()
	vc.Group.Sort = "manual"
	vc.Group.Order = order
	saveViewAndNotify(st, vc, opts)
}
