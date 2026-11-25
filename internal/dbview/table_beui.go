package dbview

import (
	"slices"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

var (
	beuiEaseOut     = ui.CubicBezier(0.16, 1, 0.3, 1)
	beuiSpringPress = ui.Animation{Spring: ui.SpringOptions{Stiffness: 500, Damping: 30, Mass: 0.6}}
)

// renderBeuiCheckbox paints the animated rounded-md checkbox modeled after beUI Checkbox.
func renderBeuiCheckbox(
	c *ui.Context,
	key string,
	checked *bool,
	indeterminate bool,
	size float32,
	radius float32,
	disabled bool,
	label string,
	be beTableColors,
) ui.Element {
	showMark := (checked != nil && *checked) || indeterminate
	cb := ui.CheckboxBase(c.Key(key), checked).Size(size, size).Radius(radius).Center().
		Disabled(disabled).Label(label)
	if !disabled {
		cb.Cursor(ui.CursorPointer)
	}

	scale := float32(1)
	if !disabled && cb.Pressed() {
		scale = 0.92
	}
	cb.AnimatePose(ui.Pose{Scale: scale}, beuiSpringPress)

	fillTarget := float32(0)
	if showMark {
		fillTarget = 1
	}
	fillProg := cb.AnimateTo("fill", fillTarget, ui.Animation{
		Duration:  180 * time.Millisecond,
		Ease:      beuiEaseOut,
		NonMotion: true,
	})
	markDur := 260 * time.Millisecond
	if indeterminate {
		markDur = 190 * time.Millisecond
	}
	markProg := cb.AnimateTo("mark", fillTarget, ui.Animation{
		Duration: markDur,
		Ease:     beuiEaseOut,
	})

	borderCol := be.checkboxBorder
	if !disabled && cb.Hovered() {
		borderCol = be.checkboxBorderHover
	}

	cb.Draw(func(p *ui.Painter, r ui.Rect) {
		if fillProg < 0.99 {
			p.Fill(r, be.cardBg, radius)
			p.Stroke(rectInset(r, 0.9), borderCol, radius, 1.8)
		}
		if fillProg > 0.01 {
			p.Fill(r, be.primary.Alpha(fillProg), radius)
		}
		if markProg > 0.02 {
			s := min(r.W, r.H) * 0.56 / 24
			ox := r.X + r.W/2 - 12*s
			oy := r.Y + r.H/2 - 12*s
			var path *ui.Path
			if indeterminate {
				path = (&ui.Path{}).
					MoveTo(ox+6*s, oy+12*s).
					LineTo(ox+18*s, oy+12*s).
					Trim(0, markProg).
					Cap(ui.RoundCap)
			} else {
				path = (&ui.Path{}).
					MoveTo(ox+5*s, oy+13*s).
					LineTo(ox+9*s, oy+17*s).
					LineTo(ox+19*s, oy+7*s).
					Trim(0, markProg).
					Cap(ui.RoundCap).
					Join(ui.RoundJoin)
			}
			p.StrokePath(path, 1.8, be.primaryFg.Alpha(markProg))
		}
	})

	return cb
}

func renderBeuiPill(c *ui.Context, name, colorName string, pillIcon *ui.SVG, pal Palette) {
	bg, fg := bePillColors(pal.Dark, colorName)
	pill := ui.Row(c).Padding(2.5, 9).Radius(999).Background(bg).Gap(4).AlignItems(ui.Center).Shrink(0)
	pill.Children(func() {
		if pillIcon != nil {
			ui.Icon(c, pillIcon).FontSize(11).TextColor(fg.Alpha(0.85))
		}
		ui.Text(c, name).FontSize(12).FontWeight(500).TextColor(fg).SingleLine()
	})
}

func tableRowMinH(view database.ViewConfig) float32 {
	if view.Table != nil {
		switch view.Table.RowHeight {
		case "compact":
			return 36
		case "tall":
			return 60
		}
	}
	return 44
}

// cycleColumnSort implements beUI Table's 3-state sort cycle: none -> asc -> desc -> none.
func cycleColumnSort(st *State, view *database.ViewConfig, propID string, opts Options) {
	if st == nil || view == nil || propID == "" {
		return
	}
	vc := view.Clone()
	desc, active := sortDirectionFor(view, propID)
	switch {
	case !active:
		vc.Sorts = []database.SortRule{{PropertyID: propID, Desc: false}}
	case !desc:
		vc.Sorts = []database.SortRule{{PropertyID: propID, Desc: true}}
	default:
		vc.Sorts = nil
	}
	saveViewAndNotify(st, vc, opts)
}

func insertColumnRelative(st *State, view *database.ViewConfig, db *database.Database, anchorPropID string, delta int, opts Options) {
	if st == nil || st.Store == nil || db == nil || view == nil || opts.ReadOnly {
		return
	}
	propID := "prop_" + database.NewID()[:8]
	newProp := database.Property{
		ID:    propID,
		Key:   propID,
		Name:  i18n.T("新建列"),
		Type:  database.PropText,
		Width: defaultColumnWidth(database.PropText),
	}
	if err := st.Store.AddProperty(newProp); err != nil {
		if opts.OnNotify != nil {
			opts.OnNotify(err.Error())
		}
		return
	}
	latestDB := st.Store.SchemaSnapshot()
	freshView := st.activeViewFrom(latestDB)
	if freshView == nil {
		freshView = view
	}
	vc := freshView.Clone()
	if len(vc.Properties) == 0 && latestDB != nil {
		vc.Properties = extractViewProps(orderedViewProperties(latestDB, &vc))
	}
	newIdx := slices.IndexFunc(vc.Properties, func(vp database.ViewProperty) bool { return vp.PropertyID == propID })
	if newIdx >= 0 {
		vp := vc.Properties[newIdx]
		vp.Visible = true
		vc.Properties = slices.Delete(vc.Properties, newIdx, newIdx+1)
		anchorIdx := slices.IndexFunc(vc.Properties, func(item database.ViewProperty) bool { return item.PropertyID == anchorPropID })
		if anchorIdx < 0 {
			vc.Properties = append(vc.Properties, vp)
		} else {
			insertAt := anchorIdx
			if delta > 0 {
				insertAt = anchorIdx + 1
			}
			vc.Properties = slices.Insert(vc.Properties, insertAt, vp)
		}
		saveViewAndNotify(st, vc, opts)
	} else if opts.OnChanged != nil {
		opts.OnChanged(nil)
	}

	st.RenamingPropID = propID
	st.RenamingPropText = newProp.Name
	st.RenamingPropPending = true
}

func insertRecordRelative(
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	anchorRec *database.Record,
	groupProp string,
	groupVal database.Value,
	delta int,
	opts Options,
) {
	if st == nil || st.Store == nil || db == nil || opts.ReadOnly {
		return
	}
	var initial map[string]database.Value
	if groupProp != "" && !groupVal.IsEmpty() {
		initial = map[string]database.Value{groupProp: groupVal}
	}
	rec, err := st.Store.CreateRecord(i18n.T("未命名记录"), initial, "")
	if err != nil {
		if opts.OnNotify != nil {
			opts.OnNotify(err.Error())
		}
		return
	}
	st.Motion.TrackCreated(rec.ID)

	if view != nil && len(view.Sorts) == 0 && res != nil && anchorRec != nil {
		var beforeID, afterID string
		if delta < 0 {
			beforeID = anchorRec.ID
		} else if delta > 0 {
			afterID = anchorRec.ID
		}
		ids := database.SpliceManualOrder(view.ManualOrder, st.Store.Records(), rec.ID, beforeID, afterID)
		_ = st.Store.SetManualOrder(view.ID, ids)
	}

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
	if tp := db.TitleProperty(); tp != nil {
		st.StartEditCell(rec.ID, tp.ID, rec.Title)
	}
}
