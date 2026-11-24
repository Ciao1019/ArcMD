package dbview

import (
	"slices"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

var (
	beuiEaseOut     = ui.CubicBezier(0.16, 1, 0.3, 1)
	beuiSpringPress = ui.Animation{Spring: ui.SpringOptions{Stiffness: 500, Damping: 30, Mass: 0.6}}
)

type beTableColors struct {
	cardBg              ui.Color
	headerBg            ui.Color
	footerBg            ui.Color
	border              ui.Color
	rowBorder           ui.Color
	rowHover            ui.Color
	rowSelected         ui.Color
	primary             ui.Color
	primaryFg           ui.Color
	checkboxBorder      ui.Color
	checkboxBorderHover ui.Color
	grip                ui.Color
	chevronMuted        ui.Color
	resizeHover         ui.Color
	metaInk             ui.Color
}

func beTablePalette(pal Palette) beTableColors {
	primary := pal.Accent
	if primary.A == 0 {
		primary = ui.RGB(59, 130, 246)
	}
	if pal.Dark {
		card := pal.Paper.Mix(ui.RGB(255, 255, 255), 0.018)
		return beTableColors{
			cardBg:              card,
			headerBg:            pal.Paper.Mix(ui.RGB(255, 255, 255), 0.042),
			footerBg:            pal.Paper.Mix(ui.RGB(255, 255, 255), 0.030),
			border:              ui.RGBA(255, 255, 255, 0.08),
			rowBorder:           ui.RGBA(255, 255, 255, 0.05),
			rowHover:            card.Mix(ui.RGB(255, 255, 255), 0.042),
			rowSelected:         card.Mix(primary, 0.14),
			primary:             primary,
			primaryFg:           ui.RGB(255, 255, 255),
			checkboxBorder:      ui.RGBA(161, 161, 170, 0.36),
			checkboxBorderHover: ui.Hex("#A1A1AA"),
			grip:                ui.RGBA(161, 161, 170, 0.45),
			chevronMuted:        ui.RGBA(161, 161, 170, 0.42),
			resizeHover:         primary.Alpha(0.42),
			metaInk:             pal.TextMuted,
		}
	}
	card := pal.Paper
	return beTableColors{
		cardBg:              card,
		headerBg:            card.Mix(ui.RGB(15, 23, 42), 0.026),
		footerBg:            card.Mix(ui.RGB(15, 23, 42), 0.018),
		border:              ui.RGBA(15, 23, 42, 0.075),
		rowBorder:           ui.RGBA(15, 23, 42, 0.05),
		rowHover:            card.Mix(ui.RGB(15, 23, 42), 0.028),
		rowSelected:         card.Mix(primary, 0.08),
		primary:             primary,
		primaryFg:           ui.RGB(255, 255, 255),
		checkboxBorder:      ui.RGBA(100, 116, 139, 0.36),
		checkboxBorderHover: ui.Hex("#64748B"),
		grip:                ui.RGBA(100, 116, 139, 0.48),
		chevronMuted:        ui.RGBA(100, 116, 139, 0.42),
		resizeHover:         primary.Alpha(0.38),
		metaInk:             pal.TextMuted,
	}
}

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

type bePillPair struct {
	lightBg, lightFg ui.Color
	darkBg, darkFg   ui.Color
}

var bePillColorMap = map[string]bePillPair{
	"default": {
		lightBg: ui.RGBA(113, 113, 122, 0.11), lightFg: ui.Hex("#3F3F46"),
		darkBg: ui.RGBA(161, 161, 170, 0.14), darkFg: ui.Hex("#D4D4D8"),
	},
	"gray": {
		lightBg: ui.RGBA(113, 113, 122, 0.12), lightFg: ui.Hex("#52525B"),
		darkBg: ui.RGBA(161, 161, 170, 0.16), darkFg: ui.Hex("#D4D4D8"),
	},
	"brown": {
		lightBg: ui.RGBA(180, 83, 9, 0.11), lightFg: ui.Hex("#92400E"),
		darkBg: ui.RGBA(217, 119, 6, 0.18), darkFg: ui.Hex("#FCD34D"),
	},
	"orange": {
		lightBg: ui.RGBA(245, 158, 11, 0.14), lightFg: ui.Hex("#B45309"),
		darkBg: ui.RGBA(245, 158, 11, 0.14), darkFg: ui.Hex("#FBBF24"),
	},
	"yellow": {
		lightBg: ui.RGBA(234, 179, 8, 0.15), lightFg: ui.Hex("#A16207"),
		darkBg: ui.RGBA(250, 204, 21, 0.15), darkFg: ui.Hex("#FDE047"),
	},
	"green": {
		lightBg: ui.RGBA(16, 185, 129, 0.12), lightFg: ui.Hex("#059669"),
		darkBg: ui.RGBA(16, 185, 129, 0.14), darkFg: ui.Hex("#34D399"),
	},
	"blue": {
		lightBg: ui.RGBA(59, 130, 246, 0.12), lightFg: ui.Hex("#2563EB"),
		darkBg: ui.RGBA(59, 130, 246, 0.16), darkFg: ui.Hex("#60A5FA"),
	},
	"purple": {
		lightBg: ui.RGBA(139, 92, 246, 0.12), lightFg: ui.Hex("#7C3AED"),
		darkBg: ui.RGBA(139, 92, 246, 0.18), darkFg: ui.Hex("#C4B5FD"),
	},
	"pink": {
		lightBg: ui.RGBA(236, 72, 153, 0.12), lightFg: ui.Hex("#DB2777"),
		darkBg: ui.RGBA(236, 72, 153, 0.16), darkFg: ui.Hex("#F472B6"),
	},
	"red": {
		lightBg: ui.RGBA(244, 63, 94, 0.12), lightFg: ui.Hex("#E11D48"),
		darkBg: ui.RGBA(244, 63, 94, 0.14), darkFg: ui.Hex("#FB7185"),
	},
}

func renderBeuiPill(c *ui.Context, name, colorName string, pillIcon *ui.SVG, pal Palette) {
	key := strings.ToLower(strings.TrimSpace(colorName))
	if key == "" {
		key = "default"
	}
	pair, ok := bePillColorMap[key]
	if !ok {
		pair = bePillColorMap["default"]
	}
	bg, fg := pair.lightBg, pair.lightFg
	if pal.Dark {
		bg, fg = pair.darkBg, pair.darkFg
	}
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
	latestDB := st.Store.Database()
	freshView := st.ActiveView()
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
		ids := make([]string, 0, len(res.Rows)+1)
		inserted := false
		for _, r := range res.Rows {
			if r == nil || r.ID == rec.ID {
				continue
			}
			if r.ID == anchorRec.ID && delta < 0 {
				ids = append(ids, rec.ID)
				inserted = true
			}
			ids = append(ids, r.ID)
			if r.ID == anchorRec.ID && delta > 0 {
				ids = append(ids, rec.ID)
				inserted = true
			}
		}
		if !inserted {
			ids = append(ids, rec.ID)
		}
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
