package editor

import (
	"encoding/json"
	"html"
	"math"
	"path"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/dbview"
	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/egoist/mygo/ui"
)

const (
	dbDefaultInlineH = float32(380)
	dbMinInlineH     = float32(220)
	dbMaxInlineH     = float32(760)
	dbMargin         = float32(14)

	dbViewOpenPrefix = "<!-- arcmd-db:view"
	dbViewCloseTag   = "<!-- /arcmd-db:view -->"
)

// DatabaseEmbedSpec describes an amphibious database Linked View embedded in Markdown.
type DatabaseEmbedSpec struct {
	Target string  `json:"target,omitempty"`
	ViewID string  `json:"view,omitempty"`
	Height float32 `json:"height,omitempty"`
}

// FormatDatabaseEmbed serializes spec and snapshotMD into the amphibious Markdown block.
func FormatDatabaseEmbed(spec DatabaseEmbedSpec, snapshotMD string) string {
	rawJSON, _ := json.Marshal(spec)
	safeJSON := strings.ReplaceAll(string(rawJSON), "-->", `--\u003e`)
	safeJSON = strings.ReplaceAll(safeJSON, "<!--", `\u003c!--`)
	openLine := dbViewOpenPrefix + " " + safeJSON + " -->"
	snap := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(snapshotMD, "\r\n", "\n"), "\r", "\n"))
	if snap == "" {
		return openLine + "\n\n" + dbViewCloseTag
	}
	return openLine + "\n\n" + snap + "\n\n" + dbViewCloseTag
}

func parseDatabaseOpenComment(s string) (DatabaseEmbedSpec, bool) {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, dbViewOpenPrefix) || !strings.HasSuffix(trimmed, "-->") {
		return DatabaseEmbedSpec{}, false
	}
	if strings.Contains(trimmed, dbViewCloseTag) {
		return DatabaseEmbedSpec{}, false
	}
	payload := strings.TrimSpace(trimmed[len(dbViewOpenPrefix) : len(trimmed)-3])
	if payload == "" {
		return DatabaseEmbedSpec{}, true
	}
	var spec DatabaseEmbedSpec
	if err := json.Unmarshal([]byte(payload), &spec); err != nil {
		return DatabaseEmbedSpec{}, false
	}
	return spec, true
}

func isDatabaseOpenComment(s string) bool {
	_, ok := parseDatabaseOpenComment(s)
	return ok
}

func isDatabaseCloseComment(s string) bool {
	return strings.TrimSpace(s) == dbViewCloseTag
}

// ParseDatabaseEmbed parses a complete amphibious Linked View raw block.
func ParseDatabaseEmbed(raw string) (DatabaseEmbedSpec, string, bool) {
	norm := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n"))
	if !strings.HasPrefix(norm, dbViewOpenPrefix) {
		return DatabaseEmbedSpec{}, "", false
	}
	endOpenRel := strings.Index(norm[len(dbViewOpenPrefix):], "-->")
	if endOpenRel < 0 {
		return DatabaseEmbedSpec{}, "", false
	}
	openEnd := len(dbViewOpenPrefix) + endOpenRel + 3
	closeIdx := strings.LastIndex(norm, dbViewCloseTag)
	if closeIdx < openEnd || strings.TrimSpace(norm[closeIdx+len(dbViewCloseTag):]) != "" {
		return DatabaseEmbedSpec{}, "", false
	}
	payload := strings.TrimSpace(norm[len(dbViewOpenPrefix) : openEnd-3])
	var spec DatabaseEmbedSpec
	if payload != "" {
		if err := json.Unmarshal([]byte(payload), &spec); err != nil {
			return DatabaseEmbedSpec{}, "", false
		}
	}
	snap := strings.TrimSpace(norm[openEnd:closeIdx])
	return spec, snap, true
}

func isDatabaseEmbedText(s string) bool {
	_, _, ok := ParseDatabaseEmbed(s)
	return ok
}

func isDatabaseBlock(b *document.Block) bool {
	return b != nil && b.Kind == "raw" && isDatabaseEmbedText(b.Text())
}

// InsertDatabaseEmbed inserts an amphibious Linked View block at the caret as
// one undoable editing step.
func (e *Editor) InsertDatabaseEmbed(target, viewID, snapshotMD string, height float32) {
	if e.ReadOnly {
		return
	}
	if height <= 0 {
		height = dbDefaultInlineH
	}
	spec := DatabaseEmbedSpec{Target: target, ViewID: viewID, Height: height}
	text := FormatDatabaseEmbed(spec, snapshotMD)
	e.Engine.Edit(func() {
		b := document.NewBlock("raw", text)
		e.Engine.Insert(b)
		p := document.NewBlock("paragraph", "")
		e.Engine.Insert(p)
	})
	e.sync()
	e.reveal = true
}

func (e *Editor) updateDatabaseBlockSpec(id string, spec DatabaseEmbedSpec, snapshotMD string) {
	if e.ReadOnly || id == "" {
		return
	}
	b := e.blockByID(id)
	if b == nil || !isDatabaseBlock(b) {
		return
	}
	next := FormatDatabaseEmbed(spec, snapshotMD)
	if b.Text() == next {
		return
	}
	anchor, caret := e.Anchor, e.Caret
	e.Engine.ReplaceBlockRuns(id, []document.Run{{Text: next}}, anchor, caret, e.Engine.Typing, e.Engine.Explicit)
	e.sync()
	e.ready = false
}

func databaseEmbedDisplayTitle(target, liveTitle string) string {
	if strings.TrimSpace(liveTitle) != "" {
		return strings.TrimSpace(liveTitle)
	}
	if strings.TrimSpace(target) != "" {
		base := path.Base(strings.ReplaceAll(target, `\`, "/"))
		base = strings.TrimSuffix(base, ".db.md")
		base = strings.TrimSuffix(base, ".md")
		if base != "" && base != "." {
			return base
		}
	}
	return i18n.T("未绑定数据库")
}

func (e *Editor) layoutDatabaseBlock(r *row, o Options, gap float32) float32 {
	b := r.leaf.Block
	spec, snapMD, _ := ParseDatabaseEmbed(b.Text())
	availW := r.rect.W

	if e.printing {
		title, rows := resolveDatabasePrintTable(spec, snapMD, o)
		r.dbEmbed = true
		r.dbPrintTitle = title
		r.dbPrintRows = rows
		rowCount := max(1, len(rows))
		const headerH = float32(34)
		const tableRowH = float32(28)
		h := headerH + float32(rowCount)*tableRowH + 8
		r.dbRect = ui.Rect{X: r.rect.X, Y: r.rect.Y + dbMargin, W: availW, H: h}
		r.rect.H = 2*dbMargin + h + gap
		return r.rect.H
	}

	h := dbDefaultInlineH
	if custom, ok := e.dbHeights[b.ID]; ok {
		h = custom
	} else if spec.Height > 0 {
		h = spec.Height
	}
	h = max(dbMinInlineH, min(dbMaxInlineH, h))

	r.dbEmbed = true
	r.dbRect = ui.Rect{X: r.rect.X, Y: r.rect.Y + dbMargin, W: availW, H: h}
	r.rect.H = 2*dbMargin + h + gap
	return r.rect.H
}

func resolveDatabasePrintTable(spec DatabaseEmbedSpec, snapMD string, o Options) (string, [][]string) {
	liveTitle := ""
	liveSnap := snapMD
	if spec.Target != "" {
		if o.DatabaseSnapshot != nil {
			if s := strings.TrimSpace(o.DatabaseSnapshot(spec.Target, spec.ViewID)); s != "" {
				liveSnap = s
			}
		}
		if o.LoadDatabase != nil {
			if st, _, _, err := o.LoadDatabase(spec.Target); err == nil && st != nil {
				if db := st.Database(); db != nil {
					liveTitle = db.Title
					if liveSnap == snapMD && o.DatabaseSnapshot == nil {
						liveSnap = database.RenderGFMSnapshot("", db, st.Query(spec.ViewID, database.QueryOptions{}))
					}
				}
			}
		}
	}
	title := databaseEmbedDisplayTitle(spec.Target, liveTitle)
	rows := parseGFMTableCells(liveSnap)
	return title, rows
}

func (e *Editor) paintDatabaseRow(p *ui.Painter, bounds ui.Rect, r row, t *ui.Theme) bool {
	if !r.dbEmbed {
		return false
	}
	if !e.printing {
		return true // interactive view rendered by databaseControls
	}
	box := r.dbRect
	box.X += bounds.X
	box.Y += bounds.Y
	p.Fill(box, t.Background, 8)
	p.Stroke(box, e.page.TableLine, 8, 1)

	const hdrH = float32(34)
	hdrFill := e.page.TableHeader
	if hdrFill.A == 0 {
		hdrFill = t.Background.Mix(t.Text, 0.04)
	}
	p.Clip(box, 8, func() {
		p.Fill(ui.Rect{X: box.X, Y: box.Y, W: box.W, H: hdrH}, hdrFill, 0)
		p.Line(box.X, box.Y+hdrH, box.X+box.W, box.Y+hdrH, 1, e.page.TableLine)
		p.Icon(iconDatabase, ui.Rect{X: box.X + 12, Y: box.Y + 10, W: 14, H: 14}, t.Accent)
		titleSpan := ui.Span{Text: r.dbPrintTitle, Font: "system-ui", Size: 12.5, Weight: 600, Color: t.Text}
		p.RichText(box.X+32, box.Y+9, 0, titleSpan)

		rows := r.dbPrintRows
		if len(rows) == 0 {
			p.RichText(box.X+14, box.Y+hdrH+10, 0, ui.Span{Text: i18n.T("暂无记录"), Font: "system-ui", Size: 12, Color: t.TextMuted})
			return
		}
		cols := len(rows[0])
		if cols == 0 {
			return
		}
		colW := box.W / float32(cols)
		const rowH = float32(28)
		for ri, tr := range rows {
			ry := box.Y + hdrH + float32(ri)*rowH
			if ri > 0 {
				p.Line(box.X, ry, box.X+box.W, ry, 0.75, e.page.TableLine)
			}
			for ci := 0; ci < cols; ci++ {
				cx := box.X + float32(ci)*colW
				if ci > 0 {
					p.Line(cx, ry, cx, ry+rowH, 0.75, e.page.TableLine)
				}
				val := ""
				if ci < len(tr) {
					val = tr[ci]
				}
				sp := ui.Span{Text: val, Font: "system-ui", Size: 11.5, Color: t.Text}
				if ri == 0 {
					sp.Weight = 600
					sp.Color = t.TextMuted
				}
				p.Clip(ui.Rect{X: cx + 6, Y: ry + 2, W: max(4, colW-12), H: rowH - 4}, 0, func() {
					p.RichText(cx+8, ry+7, 0, sp)
				})
			}
		}
	})
	return true
}

func (e *Editor) databaseControls(c *ui.Context, o Options) {
	if e.printing {
		return
	}
	t := o.Theme

	for i := range e.visibleRows(1) {
		r := *e.indexedRow(i)
		if !r.dbEmbed || r.dbRect.W <= 0 {
			continue
		}
		b := r.leaf.Block
		id := b.ID
		spec, snapMD, _ := ParseDatabaseEmbed(b.Text())
		dy := e.ownerVisualDY(r.leaf.Owner)
		box := r.dbRect
		box.Y += dy

		var store *database.Store
		var dbOpts dbview.Options
		var loadErr error
		if spec.Target != "" && e.loadDatabase != nil {
			store, dbOpts, _, loadErr = e.loadDatabase(spec.Target)
		}

		var db *database.Database
		if store != nil {
			db = store.Database()
		}
		liveTitle := ""
		if db != nil {
			liveTitle = db.Title
		}
		displayTitle := databaseEmbedDisplayTitle(spec.Target, liveTitle)

		var st *dbview.State
		if store != nil {
			if e.dbStates == nil {
				e.dbStates = map[string]*dbview.State{}
			}
			if e.dbSyncedView == nil {
				e.dbSyncedView = map[string]string{}
			}
			syncKey := id + "\x00" + spec.Target
			st = e.dbStates[id]
			if st == nil || st.Store != store {
				st = dbview.NewState(store)
				st.LocalView = true
				e.dbStates[id] = st
				delete(e.dbSyncedView, syncKey)
			}
			synced, ok := e.dbSyncedView[syncKey]
			if !ok || (spec.ViewID != "" && synced != spec.ViewID) {
				if spec.ViewID != "" && db != nil {
					if _, idx := db.ViewByID(spec.ViewID); idx >= 0 {
						st.SetActiveView(spec.ViewID)
					}
				}
				e.dbSyncedView[syncKey] = st.ActiveViewID()
			}
		}

		curViewID := spec.ViewID
		if st != nil {
			if avid := st.ActiveViewID(); avid != "" {
				curViewID = avid
			}
		}

		var boundView *database.ViewConfig
		if db != nil {
			if v, idx := db.ViewByID(curViewID); idx >= 0 {
				boundView = v
			} else if len(db.Views) > 0 {
				boundView = &db.Views[0]
				curViewID = boundView.ID
			}
		}

		refreshSnapshot := func() {
			if e.ReadOnly || spec.Target == "" {
				return
			}
			nextSnap := snapMD
			if e.databaseSnapshot != nil {
				if s := strings.TrimSpace(e.databaseSnapshot(spec.Target, curViewID)); s != "" {
					nextSnap = s
				}
			} else if db != nil && store != nil {
				nextSnap = database.RenderGFMSnapshot("", db, store.Query(curViewID, database.QueryOptions{}))
			}
			e.updateDatabaseBlockSpec(id, spec, nextSnap)
		}

		selectEmbeddedView := func(target, viewID string) {
			if e.ReadOnly || target == "" {
				return
			}
			nextSpec := spec
			nextSpec.Target = target
			nextSpec.ViewID = viewID
			nextSnap := ""
			if e.databaseSnapshot != nil {
				nextSnap = strings.TrimSpace(e.databaseSnapshot(target, viewID))
			} else if target == spec.Target && db != nil && store != nil {
				nextSnap = database.RenderGFMSnapshot("", db, store.Query(viewID, database.QueryOptions{}))
			}
			if target == spec.Target && st != nil {
				if viewID != "" {
					st.SetActiveView(viewID)
				}
				if e.dbSyncedView == nil {
					e.dbSyncedView = map[string]string{}
				}
				e.dbSyncedView[id+"\x00"+target] = st.ActiveViewID()
				if nextSpec.ViewID == "" {
					nextSpec.ViewID = st.ActiveViewID()
				}
			} else {
				if e.dbStates != nil {
					delete(e.dbStates, id)
				}
				if e.dbSyncedView != nil {
					delete(e.dbSyncedView, id+"\x00"+spec.Target)
				}
			}
			e.updateDatabaseBlockSpec(id, nextSpec, nextSnap)
		}

		card := ui.Column(c.Key("inline-db:"+id)).Absolute().Left(box.X).Top(box.Y).Size(box.W, box.H).
			Radius(10).Background(t.Background).Border(1, t.Border).
			Shadow(0, 2, 12, 0, e.page.MathShadow).Clip()
		card.Children(func() {
			// Compact card header bound to a single linked view.
			hdr := ui.Row(c.Key("hdr")).Height(34).Padding(0, 8, 0, 12).Gap(6).AlignItems(ui.Center).
				Background(t.Background.Mix(t.Text, 0.025)).BorderWidth(0, 0, 1, 0).BorderColor(t.Border)
			hdr.ContextMenu(func(m *ui.Menu) {
				if !e.ReadOnly && spec.Target != "" {
					if m.Item(i18n.T("刷新快照")).Chosen() {
						refreshSnapshot()
					}
				}
				if e.openDatabase != nil && spec.Target != "" {
					if m.Item(i18n.T("全屏打开")).Chosen() {
						e.openDatabase(spec.Target, curViewID, false)
					}
					if m.Item(i18n.T("分屏打开")).Chosen() {
						e.openDatabase(spec.Target, curViewID, true)
					}
				}
				if !e.ReadOnly {
					m.Separator()
					if m.Item(i18n.T("删除")).Chosen() {
						e.DeleteBlock(id)
					}
				}
			})
			hdr.Children(func() {
				ui.Icon(c, iconDatabase).FontSize(13.5).TextColor(t.Accent)
				ui.Text(c, displayTitle).FontSize(12.5).FontWeight(600).TextColor(t.Text).SingleLine().MinWidth(0)
				if boundView != nil {
					ui.Text(c, "/").FontSize(11.5).TextColor(t.TextMuted).NoWrap()
					ui.Icon(c, dbview.ViewIcon(boundView.Type)).FontSize(12).TextColor(t.TextMuted)
					ui.Text(c, embeddedViewDisplayName(boundView.Name, boundView.Type)).FontSize(12.5).FontWeight(500).TextColor(t.Text).SingleLine().MinWidth(0)
				}
				ui.Box(c).Padding(2, 6).Radius(4).Background(t.Accent.Alpha(0.10)).Shrink(0).Children(func() {
					ui.Text(c, i18n.T("关联视图")).FontSize(10.5).FontWeight(500).TextColor(t.Accent).SingleLine()
				})
				ui.Spacer(c)

				if !e.ReadOnly && (db != nil || e.listDatabases != nil) {
					srcBtn := dbHeaderBtn(c, "src", nil, i18n.T("切换视图"), "", t, e.page)
					srcBtn.PopoverMenuButton(func(m *ui.Menu) {
						hasCurViews := db != nil && len(db.Views) > 0
						if hasCurViews {
							for _, v := range db.Views {
								vc := v
								if m.Item(embeddedViewDisplayName(vc.Name, vc.Type)).Checked(vc.ID == curViewID).Chosen() {
									selectEmbeddedView(spec.Target, vc.ID)
								}
							}
						}
						if e.listDatabases != nil {
							dbs := e.listDatabases()
							var otherDBs []database.SpaceDBInfo
							for _, cand := range dbs {
								if hasCurViews && cand.RelPath == spec.Target {
									continue
								}
								otherDBs = append(otherDBs, cand)
							}
							if len(otherDBs) > 0 {
								renderDBItems := func(targetMenu *ui.Menu) {
									for _, cand := range otherDBs {
										dbi := cand
										if len(dbi.Views) > 0 {
											targetMenu.Submenu(dbi.Title, func(vm *ui.Menu) {
												for _, v := range dbi.Views {
													vv := v
													if vm.Item(embeddedViewDisplayName(vv.Name, vv.Type)).Checked(dbi.RelPath == spec.Target && vv.ID == curViewID).Chosen() {
														selectEmbeddedView(dbi.RelPath, vv.ID)
													}
												}
											})
										} else {
											label := dbi.Title
											if dbi.RelPath != "" {
												label = dbi.Title + " · " + dbi.RelPath
											}
											if targetMenu.Item(label).Checked(dbi.RelPath == spec.Target).Chosen() {
												selectEmbeddedView(dbi.RelPath, "")
											}
										}
									}
								}
								if hasCurViews {
									m.Separator()
									m.Submenu(i18n.T("其他数据库"), renderDBItems)
								} else {
									renderDBItems(m)
								}
							} else if !hasCurViews {
								m.Item(i18n.T("空间中暂无其他数据库")).Disabled(true)
							}
						}
					})
				}

				if !e.ReadOnly && spec.Target != "" {
					refBtn := dbHeaderBtn(c, "refresh", iconRefreshCW, "", i18n.T("刷新快照"), t, e.page)
					if refBtn.Clicked() {
						refreshSnapshot()
					}
				}
				if e.openDatabase != nil && spec.Target != "" {
					splitBtn := dbHeaderBtn(c, "split", iconExternalSplit, "", i18n.T("在分屏中打开"), t, e.page)
					if splitBtn.Clicked() {
						e.openDatabase(spec.Target, curViewID, true)
					}
					fullBtn := dbHeaderBtn(c, "full", iconMaximize, "", i18n.T("全屏打开"), t, e.page)
					if fullBtn.Clicked() {
						e.openDatabase(spec.Target, curViewID, false)
					}
				}
			})

			// Live embedded dbview body (single linked view without multi-view tab bar) or recovery empty state.
			if store != nil && st != nil {
				embOpts := dbOpts
				embOpts.Embedded = true
				embOpts.HideViewTabs = true
				if e.ReadOnly {
					embOpts.ReadOnly = true
				}
				if embOpts.Now.IsZero() {
					embOpts.Now = c.Now()
				}
				ui.Column(c.Key("body")).Grow(1).MinHeight(0).MinWidth(0).Children(func() {
					dbview.View(c.Key("ed-db:"+id), st, embOpts)
				})
				nextViewID := st.ActiveViewID()
				syncKey := id + "\x00" + spec.Target
				if !e.ReadOnly && nextViewID != "" && e.dbSyncedView[syncKey] != nextViewID {
					e.dbSyncedView[syncKey] = nextViewID
					nextSpec := spec
					nextSpec.ViewID = nextViewID
					nextSnap := snapMD
					if e.databaseSnapshot != nil {
						if s := strings.TrimSpace(e.databaseSnapshot(spec.Target, nextViewID)); s != "" {
							nextSnap = s
						}
					} else if db != nil {
						nextSnap = database.RenderGFMSnapshot("", db, store.Query(nextViewID, database.QueryOptions{}))
					}
					e.updateDatabaseBlockSpec(id, nextSpec, nextSnap)
				}
			} else {
				ui.Column(c.Key("missing")).Grow(1).Center().Gap(8).Padding(16).Children(func() {
					ui.Icon(c, iconDatabase).FontSize(22).TextColor(t.TextMuted)
					msg := i18n.T("尚未关联数据库")
					if spec.Target != "" {
						msg = i18n.T("找不到关联数据库：") + spec.Target
					}
					if loadErr != nil {
						msg = loadErr.Error()
					}
					ui.Text(c, msg).FontSize(12.5).TextColor(t.TextMuted).SingleLine()
					if !e.ReadOnly {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							if e.listDatabases != nil {
								pickBtn := dbHeaderBtn(c, "pick-missing", nil, i18n.T("选择数据库…"), "", t, e.page)
								pickBtn.PopoverMenuButton(func(m *ui.Menu) {
									for _, cand := range e.listDatabases() {
										dbi := cand
										if len(dbi.Views) > 0 {
											m.Submenu(dbi.Title, func(vm *ui.Menu) {
												for _, v := range dbi.Views {
													vv := v
													if vm.Item(embeddedViewDisplayName(vv.Name, vv.Type)).Chosen() {
														selectEmbeddedView(dbi.RelPath, vv.ID)
													}
												}
											})
										} else if m.Item(dbi.Title + " · " + dbi.RelPath).Chosen() {
											selectEmbeddedView(dbi.RelPath, "")
										}
									}
								})
							}
							delBtn := dbHeaderBtn(c, "del-missing", nil, i18n.T("删除"), "", t, e.page)
							if delBtn.Clicked() {
								e.DeleteBlock(id)
							}
						})
					}
				})
			}
		})

		// Bottom drag handle for resizing inline height.
		if !e.ReadOnly {
			grip := ui.Box(c.Key("db-resize:"+id)).Absolute().Left(box.X+box.W/2-36).Top(box.Y+box.H-5).
				Size(72, 10).Radius(5).Cursor(ui.CursorResizeNS).Tooltip(i18n.T("拖动调整视图高度"))
			if grip.Hovered() || e.dbDragID == id {
				grip.Background(t.Accent.Alpha(0.35))
			} else {
				grip.Background(e.page.CodeBorder)
			}
			_, dragDY, pressed := grip.Dragged()
			if pressed {
				if e.dbDragID != id {
					e.dbDragID = id
					e.dbDragStartH = box.H
				}
				if e.dbHeights == nil {
					e.dbHeights = map[string]float32{}
				}
				baseH := e.dbHeights[id]
				if baseH == 0 {
					baseH = e.dbDragStartH
				}
				e.dbHeights[id] = max(dbMinInlineH, min(dbMaxInlineH, baseH+dragDY))
				e.ready = false
			} else if e.dbDragID == id {
				finalH := e.dbHeights[id]
				e.dbDragID = ""
				if finalH > 0 && math.Abs(float64(finalH-spec.Height)) >= 1 {
					nextSpec := spec
					nextSpec.Height = finalH
					e.updateDatabaseBlockSpec(id, nextSpec, snapMD)
				}
			}
		}
	}
}

func dbHeaderBtn(c *ui.Context, key string, icon *ui.SVG, label, tip string, t *ui.Theme, pg Page) ui.Element {
	btn := ui.ButtonBase(c.Key(key)).Height(24).PaddingX(7).Radius(5).Gap(4).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Role(ui.RoleButton)
	if label != "" {
		btn.Label(label)
	} else if tip != "" {
		btn.Label(tip)
	}
	if tip != "" {
		btn.Tooltip(tip)
	}
	fg := pg.Caption
	if btn.Hovered() {
		btn.Background(pg.Hover)
		fg = t.Text
	}
	btn.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).FontSize(12).TextColor(fg)
		}
		if label != "" {
			ui.Text(c, label).FontSize(11.5).FontWeight(500).TextColor(fg).SingleLine()
		}
	})
	return btn
}

func embeddedViewDisplayName(name string, vt database.ViewType) string {
	if s := strings.TrimSpace(name); s != "" {
		return s
	}
	if spec, ok := dbview.ViewSpecFor(vt); ok {
		return spec.Label()
	}
	return string(vt)
}

func (e *Editor) databasePickerControl(c *ui.Context, surface ui.Element, o Options) {
	if !e.dbPickerOpen || e.ReadOnly {
		return
	}
	t := o.Theme
	bounds := e.surfaceBounds(surface)
	caret, _, _ := e.BoundsForRange(e.Selection().Range)
	windowW, windowH := c.Size()
	anchor := ui.Rect{X: bounds.X + caret.X, Y: bounds.Y + caret.Y, W: caret.W, H: caret.H}
	const pickerW = float32(320)
	x, y, maxH := placeMenu(anchor, pickerW, 320, 4, windowW, windowH)

	var dbs []database.SpaceDBInfo
	if e.listDatabases != nil {
		dbs = e.listDatabases()
	}

	ui.Overlay(c, func() {
		backdrop := ui.Box(c.Key("db-picker-backdrop")).Fill()
		if backdrop.Clicked() {
			e.dbPickerOpen = false
			e.RequestFocus = true
			return
		}
		panel := ui.Column(c.Key("db-picker")).Absolute().Left(x).Top(y).Width(pickerW).MaxHeight(maxH).
			Padding(8).Gap(6)
		e.popover(c, panel).Children(func() {
			in := ui.TextInput(c.Key("db-pick-q"), &e.dbPickerQuery).
				Placeholder(i18n.T("搜索要关联的数据库或视图…")).Label(i18n.T("搜索要关联的数据库或视图")).
				Height(30).Padding(4, 8).Radius(6).Background(t.Background).TextColor(t.Text).AutoFocus()
			if in.Shortcut(0, ui.KeyEscape) {
				e.dbPickerOpen = false
				e.RequestFocus = true
				return
			}

			type pickEntry struct {
				dbi    database.SpaceDBInfo
				viewID string
			}
			type pickGroup struct {
				dbi   database.SpaceDBInfo
				views []database.SpaceDBViewInfo
			}
			q := strings.ToLower(strings.TrimSpace(e.dbPickerQuery))
			var groups []pickGroup
			var flatFirst *pickEntry
			for _, cand := range dbs {
				dbMatch := q == "" || strings.Contains(strings.ToLower(cand.Title+" "+cand.RelPath), q)
				if len(cand.Views) > 0 {
					var matched []database.SpaceDBViewInfo
					for _, v := range cand.Views {
						vn := embeddedViewDisplayName(v.Name, v.Type)
						if dbMatch || strings.Contains(strings.ToLower(vn), q) {
							matched = append(matched, v)
						}
					}
					if len(matched) > 0 {
						groups = append(groups, pickGroup{dbi: cand, views: matched})
						if flatFirst == nil {
							flatFirst = &pickEntry{dbi: cand, viewID: matched[0].ID}
						}
					}
				} else if dbMatch {
					groups = append(groups, pickGroup{dbi: cand})
					if flatFirst == nil {
						flatFirst = &pickEntry{dbi: cand}
					}
				}
			}

			insertTarget := func(relPath, viewID string) {
				e.dbPickerOpen = false
				e.dbPickerQuery = ""
				snap := ""
				if e.databaseSnapshot != nil {
					snap = e.databaseSnapshot(relPath, viewID)
				}
				e.InsertDatabaseEmbed(relPath, viewID, snap, dbDefaultInlineH)
				e.RequestFocus = true
			}

			if in.Submitted() && flatFirst != nil {
				insertTarget(flatFirst.dbi.RelPath, flatFirst.viewID)
				return
			}

			ui.Scroll(c.Key("db-pick-scroll")).MaxHeight(216).MinWidth(0).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					if len(groups) == 0 {
						ui.Text(c, i18n.T("没有匹配的视图")).FontSize(12.5).TextColor(t.TextMuted).Padding(6, 4)
						return
					}
					for _, grp := range groups {
						dbi := grp.dbi
						if len(grp.views) > 0 {
							ui.Row(c.Key("pick-hdr:"+dbi.RelPath)).Height(24).Padding(2, 8, 0, 8).Gap(6).AlignItems(ui.Center).Children(func() {
								ui.Icon(c, iconDatabase).FontSize(12).TextColor(t.Accent)
								ui.Text(c, dbi.Title).FontSize(11.5).FontWeight(600).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
							})
							for _, v := range grp.views {
								vv := v
								vName := embeddedViewDisplayName(vv.Name, vv.Type)
								row := ui.Row(c.Key("pick:"+dbi.RelPath+"/"+vv.ID)).Height(28).Padding(0, 8, 0, 22).Radius(6).
									Gap(7).AlignItems(ui.Center).Cursor(ui.CursorPointer).
									Role(ui.RoleOption).Label(dbi.Title + " / " + vName)
								if row.Hovered() {
									row.Background(e.page.Hover)
								}
								row.Children(func() {
									ui.Icon(c, dbview.ViewIcon(vv.Type)).FontSize(13).TextColor(t.TextMuted)
									ui.Text(c, vName).FontSize(12.5).TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
								})
								if row.Clicked() {
									insertTarget(dbi.RelPath, vv.ID)
									return
								}
							}
							continue
						}

						row := ui.Row(c.Key("pick:"+dbi.RelPath)).Height(30).Padding(0, 8).Radius(6).
							Gap(8).AlignItems(ui.Center).Cursor(ui.CursorPointer).
							Role(ui.RoleOption).Label(dbi.Title)
						if row.Hovered() {
							row.Background(e.page.Hover)
						}
						row.Children(func() {
							ui.Icon(c, iconDatabase).FontSize(13.5).TextColor(t.Accent)
							ui.Text(c, dbi.Title).FontSize(12.5).TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
							ui.Text(c, dbi.RelPath).FontSize(11).TextColor(t.TextMuted).SingleLine()
						})
						if row.Clicked() {
							insertTarget(dbi.RelPath, "")
							return
						}
					}
				})
			})

			if e.createDatabase != nil {
				menuSeparator(c)
				newRow := e.menuRow(c, "db-pick-new", iconPlus, t.Accent, i18n.T("新建并嵌入数据库"), "", false)
				if newRow.Clicked() {
					e.dbPickerOpen = false
					e.dbPickerQuery = ""
					if targetRel, viewID, snapMD, err := e.createDatabase(i18n.T("未命名数据库")); err == nil && targetRel != "" {
						e.InsertDatabaseEmbed(targetRel, viewID, snapMD, dbDefaultInlineH)
					}
					e.RequestFocus = true
				}
			}
		})
	})
}

func parseGFMTableCells(snapMD string) [][]string {
	norm := strings.ReplaceAll(strings.ReplaceAll(snapMD, "\r\n", "\n"), "\r", "\n")
	var rows [][]string
	for _, line := range strings.Split(norm, "\n") {
		tr := strings.TrimSpace(line)
		if !strings.HasPrefix(tr, "|") {
			continue
		}
		trimmed := strings.TrimSuffix(strings.TrimPrefix(tr, "|"), "|")
		parts := splitEscapedPipeCells(trimmed)
		if len(parts) == 0 || isSeparatorPipeRow(parts) {
			continue
		}
		for i := range parts {
			parts[i] = stripSimpleMarkdownLinkText(strings.ReplaceAll(strings.TrimSpace(parts[i]), "<br>", " "))
		}
		rows = append(rows, parts)
	}
	return rows
}

func splitEscapedPipeCells(s string) []string {
	var out []string
	var cur strings.Builder
	escaped := false
	for _, r := range s {
		if escaped {
			cur.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '|' {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	out = append(out, cur.String())
	return out
}

func isSeparatorPipeRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		t := strings.TrimSpace(c)
		if t == "" || strings.Trim(t, "-:") != "" || !strings.Contains(t, "-") {
			return false
		}
	}
	return true
}

func stripSimpleMarkdownLinkText(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") {
		if mid := strings.Index(s, "]("); mid > 0 && strings.HasSuffix(s, ")") {
			return s[1:mid]
		}
	}
	return s
}

func exportDatabaseHTML(spec DatabaseEmbedSpec, snapMD string) string {
	title := databaseEmbedDisplayTitle(spec.Target, "")
	var out strings.Builder
	out.WriteString(`<figure class="arcmd-database"><figcaption><strong>`)
	out.WriteString(html.EscapeString(title))
	out.WriteString(`</strong></figcaption>`)

	norm := strings.ReplaceAll(strings.ReplaceAll(snapMD, "\r\n", "\n"), "\r", "\n")
	var rawRows [][]string
	for _, line := range strings.Split(norm, "\n") {
		tr := strings.TrimSpace(line)
		if !strings.HasPrefix(tr, "|") {
			continue
		}
		trimmed := strings.TrimSuffix(strings.TrimPrefix(tr, "|"), "|")
		parts := splitEscapedPipeCells(trimmed)
		if len(parts) == 0 || isSeparatorPipeRow(parts) {
			continue
		}
		rawRows = append(rawRows, parts)
	}
	if len(rawRows) == 0 {
		out.WriteString(`<p><em>`)
		out.WriteString(html.EscapeString(i18n.T("暂无记录")))
		out.WriteString(`</em></p></figure>`)
		return out.String()
	}
	out.WriteString("<table><thead><tr>")
	for _, cell := range rawRows[0] {
		out.WriteString("<th>")
		out.WriteString(renderPipeCellHTML(cell))
		out.WriteString("</th>")
	}
	out.WriteString("</tr></thead>")
	if len(rawRows) > 1 {
		out.WriteString("<tbody>")
		for _, row := range rawRows[1:] {
			out.WriteString("<tr>")
			for i := 0; i < len(rawRows[0]); i++ {
				cell := ""
				if i < len(row) {
					cell = row[i]
				}
				out.WriteString("<td>")
				out.WriteString(renderPipeCellHTML(cell))
				out.WriteString("</td>")
			}
			out.WriteString("</tr>")
		}
		out.WriteString("</tbody>")
	}
	out.WriteString("</table></figure>")
	return out.String()
}

func renderPipeCellHTML(raw string) string {
	s := strings.TrimSpace(strings.ReplaceAll(raw, "<br>", " "))
	if strings.HasPrefix(s, "[") {
		if mid := strings.Index(s, "]("); mid > 0 && strings.HasSuffix(s, ")") {
			label := s[1:mid]
			href := strings.TrimSpace(s[mid+2 : len(s)-1])
			if safeHTMLURL(href) {
				return `<a href="` + html.EscapeString(href) + `">` + html.EscapeString(label) + `</a>`
			}
			return html.EscapeString(label)
		}
	}
	return html.EscapeString(s)
}

// ExportDatabaseEmbedToVectorPage writes the embedded database Linked View
// card and table grid cleanly into a vector PDF export page.
func ExportDatabaseEmbedToVectorPage(
	vp *pdf.VectorPage,
	title string,
	rows [][]string,
	pg Page,
	t *ui.Theme,
	dstX, dstY, dstW, dstH, sc float64,
) {
	if vp == nil || dstW <= 0 || dstH <= 0 {
		return
	}
	vp.FillRect(dstX, dstY, dstW, dstH, 8*sc, pdfRGBA(t.Background))
	vp.StrokeRect(dstX, dstY, dstW, dstH, 8*sc, max(0.75, 1*sc), pdfRGBA(pg.TableLine), false)

	hdrH := 34.0 * sc
	hdrFill := pg.TableHeader
	if hdrFill.A == 0 {
		hdrFill = t.Background.Mix(t.Text, 0.04)
	}
	vp.PushClip(dstX, dstY, dstW, dstH, 8*sc)
	defer vp.PopClip()

	vp.FillRect(dstX, dstY, dstW, min(hdrH, dstH), 0, pdfRGBA(hdrFill))
	vp.Line(dstX, dstY+hdrH, dstX+dstW, dstY+hdrH, max(0.75, 1*sc), pdfRGBA(pg.TableLine), false)

	displayTitle := strings.TrimSpace(title)
	if displayTitle == "" {
		displayTitle = i18n.T("关联视图")
	}
	vp.DrawText(displayTitle, dstX+12*sc, dstY+21*sc, 0, 12.5*sc, pdf.TextStyle{Family: pdf.FontSans, Bold: true}, pdfRGBA(t.Text))

	if len(rows) == 0 || len(rows[0]) == 0 {
		vp.DrawText(i18n.T("暂无记录"), dstX+12*sc, dstY+hdrH+20*sc, 0, 11.5*sc, pdf.TextStyle{Family: pdf.FontSans}, pdfRGBA(t.TextMuted))
		return
	}

	cols := len(rows[0])
	colW := dstW / float64(cols)
	rowH := 28.0 * sc
	for ri, tr := range rows {
		ry := dstY + hdrH + float64(ri)*rowH
		if ry >= dstY+dstH {
			break
		}
		if ri > 0 {
			vp.Line(dstX, ry, dstX+dstW, ry, max(0.5, 0.75*sc), pdfRGBA(pg.TableLine), false)
		}
		for ci := 0; ci < cols; ci++ {
			cx := dstX + float64(ci)*colW
			if ci > 0 {
				vp.Line(cx, ry, cx, min(dstY+dstH, ry+rowH), max(0.5, 0.75*sc), pdfRGBA(pg.TableLine), false)
			}
			val := ""
			if ci < len(tr) {
				val = tr[ci]
			}
			style := pdf.TextStyle{Family: pdf.FontSans}
			col := pdfRGBA(t.Text)
			if ri == 0 {
				style.Bold = true
				col = pdfRGBA(t.TextMuted)
			}
			vp.PushClip(cx+4*sc, ry+2*sc, max(4, colW-8*sc), rowH-4*sc, 0)
			vp.DrawText(val, cx+8*sc, ry+18*sc, 0, 11*sc, style, col)
			vp.PopClip()
		}
	}
}
