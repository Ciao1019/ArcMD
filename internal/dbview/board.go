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
	if view.Group == nil || view.Group.PropertyID == "" {
		renderBoardPickGroupPrompt(c, st, view, db, opts, pal)
		return
	}
	groupProp, gpIdx := db.PropertyByID(view.Group.PropertyID)
	if gpIdx < 0 || groupProp == nil {
		renderBoardPickGroupPrompt(c, st, view, db, opts, pal)
		return
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
	if bcfg.SubGroup != nil && bcfg.SubGroup.PropertyID != "" {
		if sp, sIdx := db.PropertyByID(bcfg.SubGroup.PropertyID); sIdx >= 0 && sp.ID != groupProp.ID {
			subProp = sp
		}
	}

	excludeIDs := []string{groupProp.ID}
	if subProp != nil {
		excludeIDs = append(excludeIDs, subProp.ID)
	}
	cardProps := visibleNonTitleProps(db, &view, excludeIDs...)

	if subProp != nil && bcfg.SubGroup != nil {
		renderBoardSwimlanes(c, st, view, db, res, *groupProp, *subProp, *bcfg.SubGroup, bcfg, colW, covH, cardProps, opts, pal)
		return
	}

	renderBoardColumns(c, st, view, db, res, *groupProp, bcfg, colW, covH, cardProps, opts, pal)
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

func renderBoardPickGroupPrompt(
	c *ui.Context,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	opts Options,
	pal Palette,
) {
	ui.Column(c.Key("board-nogroup")).Grow(1).Center().Gap(12).Padding(32).Children(func() {
		ui.Box(c).Size(44, 44).Radius(11).Background(pal.Surface).Center().Children(func() {
			ui.Icon(c, iconViewBoard).FontSize(22).TextColor(pal.Accent)
		})
		ui.Text(c, i18n.T("请选择看板的分组依据")).FontSize(16).FontWeight(600).TextColor(pal.Text)
		ui.Text(c, i18n.T("选择一个状态、单选、复选框或日期属性作为看板列。")).FontSize(13).TextColor(pal.TextMuted)
		ui.Row(c).Wrap().Gap(8).Center().MaxWidth(460).Children(func() {
			for _, p := range db.Properties {
				if p.Type == database.PropTitle {
					continue
				}
				cand := p
				btn := ui.ButtonBase(c.Key("bpick:"+cand.ID)).Height(30).Padding(0, 12).Radius(7).
					Background(pal.Surface).Border(1, pal.Border).Cursor(ui.CursorPointer).
					Gap(6).AlignItems(ui.Center).Role(ui.RoleButton).Label(cand.Name)
				if btn.Hovered() {
					btn.Background(pal.AccentSoft).Border(1, pal.Accent)
				}
				btn.Children(func() {
					ui.Icon(c, PropertyIcon(cand.Type)).FontSize(13).TextColor(pal.TextMuted)
					ui.Text(c, cand.Name).FontSize(12.5).TextColor(pal.Text).SingleLine()
				})
				if btn.Clicked() {
					vc := view.Clone()
					vc.Group = &database.GroupConfig{PropertyID: cand.ID}
					if cand.Type == database.PropStatus {
						vc.Group.StatusBy = "option"
					} else if cand.Type == database.PropDate || cand.Type == database.PropCreatedTime || cand.Type == database.PropEditedTime {
						vc.Group.DateBy = "month"
					}
					saveViewAndNotify(st, vc, opts)
				}
			}
		})
	})
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
	opts Options,
	pal Palette,
) {
	scroller := ui.ScrollHorizontal(c.Key("board-x")).TrackScroll(&st.BoardScrollX).
		Grow(1).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch)
	scroller.Children(func() {
		ui.Row(c.Key("board-row")).Padding(14, 18, 18, 18).Gap(12).AlignItems(ui.Stretch).MinHeightPercent(100).Children(func() {
			for i := range res.Groups {
				g := res.Groups[i]
				if g.Collapsed {
					renderCollapsedBoardColumn(c, st, &view, db, res, groupProp, g, bcfg.ColorColumn, opts, pal)
					continue
				}
				renderExpandedBoardColumn(c, st, &view, db, res, groupProp, g, bcfg, colW, covH, cardProps, opts, pal)
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
	colorCol bool,
	opts Options,
	pal Palette,
) {
	bg := columnFillColor(g.Color, colorCol, pal)
	col := ui.Column(c.Key("bcol-collapsed:"+g.Key)).Width(42).Shrink(0).Radius(10).
		Background(bg).Border(1, pal.Border).Padding(10, 4).Gap(8).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("展开此列") + ": " + g.Label)

	if !opts.ReadOnly {
		if d, ok := ui.Drop[boardCardDrag](col); ok && d.fromGroupKey != g.Key {
			applyBoardCardDrop(st, view, db, res, d, g.Key, g.Value, "", database.EmptyValue(), "", opts)
		}
		if d, over := ui.DragOver[boardCardDrag](col); over && d.fromGroupKey != g.Key {
			col.Border(1.5, pal.Accent).Background(pal.AccentSoft)
		}
	}

	col.Children(func() {
		ui.Icon(c, iconChevronRight).FontSize(13).TextColor(pal.TextMuted)
		if g.Color != "" && g.Color != "default" {
			ui.Box(c).Size(7, 7).Radius(3.5).Background(StatusDotColor(pal.Dark, g.Color))
		}
		ui.Text(c, fmt.Sprintf("%d", g.Count)).FontSize(11.5).TextColor(pal.TextMuted).SingleLine()
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
	bcfg database.BoardViewConfig,
	colW, covH float32,
	cardProps []database.Property,
	opts Options,
	pal Palette,
) {
	bg := columnFillColor(g.Color, bcfg.ColorColumn, pal)
	borderCol := pal.Border

	col := ui.Column(c.Key("bcol:"+g.Key)).Width(colW).Shrink(0).Radius(10).
		Background(bg).Border(1, borderCol).Clip()

	if !opts.ReadOnly {
		if d, ok := ui.Drop[boardCardDrag](col); ok {
			applyBoardCardDrop(st, view, db, res, d, g.Key, g.Value, "", database.EmptyValue(), "", opts)
		}
		if d, over := ui.DragOver[boardCardDrag](col); over && d.fromGroupKey != g.Key {
			col.Border(1.5, pal.Accent)
		}
		if cd, ok := ui.Drop[boardColDrag](col); ok && cd.key != g.Key {
			reorderBoardGroup(st, view, res, cd.key, g.Key, opts)
		}
		if cd, over := ui.DragOver[boardColDrag](col); over && cd.key != g.Key {
			col.Border(1.5, pal.Accent)
		}
	}

	col.Children(func() {
		// Column Header.
		hdr := ui.Row(c.Key("bhdr:"+g.Key)).Height(40).Padding(0, 10).Gap(6).AlignItems(ui.Center)
		if !opts.ReadOnly {
			hdr.Drag(boardColDrag{key: g.Key})
		}
		hdr.Children(func() {
			if g.Color != "" && g.Color != "default" {
				renderTagPill(c, g.Label, g.Color, groupProp.Type == database.PropStatus, pal)
			} else {
				ui.Text(c, g.Label).FontSize(13).FontWeight(600).TextColor(pal.Text).SingleLine()
			}
			if bcfg.ShowCount {
				ui.Text(c, fmt.Sprintf("%d", g.Count)).FontSize(12).TextColor(pal.TextMuted).SingleLine()
			}
			ui.Spacer(c)

			if !opts.ReadOnly {
				addBtn := ui.Box(c.Key("bhdr-add:"+g.Key)).Size(24, 24).Radius(5).Center().
					Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("在此分组新建") + ": " + g.Label).
					Tooltip(i18n.T("在此分组新建"))
				if addBtn.Hovered() {
					addBtn.Background(pal.RowHover)
				}
				addBtn.Children(func() { ui.Icon(c, iconPlus).FontSize(13).TextColor(pal.TextMuted) })
				if addBtn.Clicked() {
					createBoardRecordInBucket(st, db, groupProp, *view.Group, g, nil, nil, database.RecordGroup{}, opts)
				}
			}

			foldBtn := ui.Box(c.Key("bhdr-fold:"+g.Key)).Size(24, 24).Radius(5).Center().
				Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("折叠此列") + ": " + g.Label).
				Tooltip(i18n.T("折叠此列"))
			if foldBtn.Hovered() {
				foldBtn.Background(pal.RowHover)
			}
			foldBtn.Children(func() { ui.Icon(c, iconChevronLeft).FontSize(13).TextColor(pal.TextMuted) })
			if foldBtn.Clicked() && view.Group != nil {
				vc := view.Clone()
				vc.Group.Collapsed = toggleStringInSlice(slices.Clone(vc.Group.Collapsed), g.Key)
				saveViewAndNotify(st, vc, opts)
			}
		})

		// Cards Scroll Body.
		ui.Scroll(c.Key("bcol-sc:" + g.Key)).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c.Key("bcards:"+g.Key)).Padding(2, 8, 8, 8).Gap(8).Children(func() {
				for _, r := range g.Rows {
					if r == nil {
						continue
					}
					renderBoardCard(c, st, view, db, res, r, g.Key, "", bcfg.CoverSource, bcfg.FitImage, covH, cardProps, g.Key, g.Value, "", database.EmptyValue(), opts, pal)
				}

				if !opts.ReadOnly {
					footAdd := ui.Row(c.Key("bfoot-add:"+g.Key)).Height(32).Padding(0, 8).Radius(7).Gap(6).
						AlignItems(ui.Center).Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("新建") + ": " + g.Label)
					if footAdd.Hovered() {
						footAdd.Background(pal.RowHover)
					}
					footAdd.Children(func() {
						ui.Icon(c, iconPlus).FontSize(13).TextColor(pal.TextMuted)
						ui.Text(c, i18n.T("新建")).FontSize(12.5).TextColor(pal.TextMuted).SingleLine()
					})
					if footAdd.Clicked() {
						createBoardRecordInBucket(st, db, groupProp, *view.Group, g, nil, nil, database.RecordGroup{}, opts)
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
	coverSource string,
	fitImage bool,
	covH float32,
	cardProps []database.Property,
	toGroupKey string,
	toGroupVal database.Value,
	toSubKey string,
	toSubVal database.Value,
	opts Options,
	pal Palette,
) {
	borderCol := pal.Border
	cardBg := pal.Paper
	tintBg, tintAccent, tinted := tintedRowBackground(pal, res, rec.ID)
	if tinted {
		cardBg = tintBg
	}
	card := ui.Column(c.Key("bcard:"+fromGroupKey+":"+fromSubKey+":"+rec.ID)).
		Radius(9).Background(cardBg).Border(1, borderCol).
		Shadow(0, 1, 3, 0, pal.ShadowEdge).Cursor(ui.CursorPointer).Clip().
		Focusable().Role(ui.RoleListItem).Label(rec.Title)
	if tinted {
		card.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 3, H: r.H}, tintAccent, 0)
		})
	}

	if card.Hovered() {
		card.Border(1, pal.BorderStrong)
	}
	if !opts.ReadOnly {
		card.Drag(boardCardDrag{
			recordID:     rec.ID,
			fromGroupKey: fromGroupKey,
			fromSubKey:   fromSubKey,
		})
		if d, ok := ui.Drop[boardCardDrag](card); ok && d.recordID != rec.ID {
			applyBoardCardDrop(st, view, db, res, d, toGroupKey, toGroupVal, toSubKey, toSubVal, rec.ID, opts)
		}
		if d, over := ui.DragOver[boardCardDrag](card); over && d.recordID != rec.ID {
			card.DrawOver(func(p *ui.Painter, r ui.Rect) {
				p.Line(r.X+4, r.Y+1, r.X+r.W-4, r.Y+1, 2.5, pal.Accent)
			})
		}
	}

	attachRecordContextMenu(card, st, rec, opts)

	var consumedTitle, consumedProps bool
	card.Children(func() {
		renderCardPreview(c, db, rec, coverSource, fitImage, covH, false, opts, pal)
		ui.Column(c.Key("bcard-body")).Padding(10, 11).Gap(6).MinWidth(0).Children(func() {
			consumedTitle = renderCardTitleRow(c, st, db, rec, true, true, opts, pal)
			consumedProps = renderCardProperties(c, st, rec, cardProps, false, opts, pal)
		})
	})

	if card.Clicked() && !consumedTitle && !consumedProps && opts.OnOpenRecord != nil {
		opts.OnOpenRecord(rec, resolveCardOpenMode(*view))
	}
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
						hdr := ui.Box(c.Key("bsw-ch:"+g.Key)).Width(42).Height(34).Radius(8).
							Background(columnFillColor(g.Color, bcfg.ColorColumn, pal)).Border(1, pal.Border).
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
						Background(columnFillColor(g.Color, bcfg.ColorColumn, pal)).Border(1, pal.Border).
						Gap(6).AlignItems(ui.Center)
					hdr.Children(func() {
						if g.Color != "" && g.Color != "default" {
							renderTagPill(c, g.Label, g.Color, groupProp.Type == database.PropStatus, pal)
						} else {
							ui.Text(c, g.Label).FontSize(13).FontWeight(600).TextColor(pal.Text).SingleLine()
						}
						if bcfg.ShowCount {
							ui.Text(c, fmt.Sprintf("%d", g.Count)).FontSize(12).TextColor(pal.TextMuted).SingleLine()
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
									if g.Collapsed {
										cell := ui.Box(c.Key("bsw-c:"+g.Key)).Width(42).MinHeight(64).Radius(9).
											Background(bg).Border(1, pal.Border)
										if !opts.ReadOnly {
											if d, ok := ui.Drop[boardCardDrag](cell); ok {
												applyBoardCardDrop(st, &view, db, res, d, g.Key, g.Value, ln.key, ln.val, "", opts)
											}
											if d, over := ui.DragOver[boardCardDrag](cell); over && (d.fromGroupKey != g.Key || d.fromSubKey != ln.key) {
												cell.Border(1.5, pal.Accent).Background(pal.AccentSoft)
											}
										}
										continue
									}

									cell := ui.Column(c.Key("bsw-cell:"+g.Key)).Width(colW).MinHeight(72).Radius(10).
										Background(bg).Border(1, pal.Border).Padding(8).Gap(8)
									if !opts.ReadOnly {
										if d, ok := ui.Drop[boardCardDrag](cell); ok {
											applyBoardCardDrop(st, &view, db, res, d, g.Key, g.Value, ln.key, ln.val, "", opts)
										}
										if d, over := ui.DragOver[boardCardDrag](cell); over && (d.fromGroupKey != g.Key || d.fromSubKey != ln.key) {
											cell.Border(1.5, pal.Accent)
										}
									}
									cell.Children(func() {
										for _, r := range ln.cells[g.Key] {
											if r == nil {
												continue
											}
											renderBoardCard(c, st, &view, db, res, r, g.Key, ln.key, bcfg.CoverSource, bcfg.FitImage, covH, cardProps, g.Key, g.Value, ln.key, ln.val, opts, pal)
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
		btn := ui.Row(c.Key("board-add-grp")).Width(148).Height(38).Padding(0, 12).Radius(10).
			Gap(6).AlignItems(ui.Center).Background(pal.Surface).Border(1, pal.Border).
			Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("添加分组"))
		if btn.Hovered() {
			btn.Background(pal.RowHover).Border(1, pal.BorderStrong)
		}
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

	ui.Column(c.Key("board-new-grp-box")).Width(216).Padding(10).Radius(10).Background(pal.HeaderBg).
		Border(1, pal.Accent).Gap(8).Shrink(0).Children(func() {
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
	ratio := float32(0.055)
	if pal.Dark {
		ratio = 0.09
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
	if val, ok := resolveGroupDropValue(groupProp, groupCfg, nil, "", g.Key, g.Value, opts.Now); ok && !val.IsEmpty() {
		initial[groupProp.ID] = val
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
