package dbview

import (
	"strings"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

func visibleNonTitleProps(db *database.Database, view *database.ViewConfig, excludeIDs ...string) []database.Property {
	if db == nil {
		return nil
	}
	excluded := make(map[string]bool, len(excludeIDs))
	for _, id := range excludeIDs {
		if id != "" {
			excluded[id] = true
		}
	}
	items := orderedViewProperties(db, view)
	var out []database.Property
	for _, it := range items {
		if !it.vp.Visible || it.prop.Type == database.PropTitle || excluded[it.prop.ID] {
			continue
		}
		out = append(out, it.prop)
	}
	return out
}

func resolveCardOpenMode(view database.ViewConfig) string {
	if view.OpenMode == "" {
		return OpenSidePeek
	}
	return view.OpenMode
}

func attachRecordContextMenu(el ui.Element, st *State, rec *database.Record, opts Options) {
	if rec == nil {
		return
	}
	el.ContextMenu(func(m *ui.Menu) {
		if opts.OnOpenRecord != nil {
			if m.Item(i18n.T("在侧边栏预览")).Chosen() {
				opts.OnOpenRecord(rec, OpenSidePeek)
			}
			if m.Item(i18n.T("居中弹窗打开")).Chosen() {
				opts.OnOpenRecord(rec, OpenCenterPeek)
			}
			if rec.Path != "" {
				if m.Item(i18n.T("整页打开")).Chosen() {
					opts.OnOpenRecord(rec, OpenFullPage)
				}
			}
			m.Separator()
		}
		if !opts.ReadOnly {
			if m.Item(i18n.T("移到废纸篓")).Chosen() {
				trashSingleRecord(st, rec, opts)
			}
		}
	})
}

func resolveCoverImageURL(db *database.Database, rec *database.Record, coverSource string) string {
	if rec == nil {
		return ""
	}
	switch {
	case coverSource == "cover":
		return strings.TrimSpace(rec.Cover)
	case strings.HasPrefix(coverSource, "prop:") && db != nil:
		pid := strings.TrimPrefix(coverSource, "prop:")
		if prop, idx := db.PropertyByID(pid); idx >= 0 {
			v := rec.ValueFor(*prop)
			for _, item := range v.List {
				if s := strings.TrimSpace(item); s != "" {
					return s
				}
			}
			if s := strings.TrimSpace(v.Text); s != "" {
				return s
			}
		}
	}
	return ""
}

func excerptPreviewText(rec *database.Record) string {
	if rec == nil {
		return ""
	}
	s := strings.TrimSpace(rec.Excerpt)
	if s == "" {
		s = strings.TrimSpace(rec.Body)
	}
	if s == "" {
		return ""
	}
	runes := []rune(s)
	if len(runes) > 160 {
		s = string(runes[:160]) + "…"
	}
	return s
}

func renderCardPreview(
	c *ui.Context,
	db *database.Database,
	rec *database.Record,
	coverSource string,
	fitImage bool,
	height float32,
	showEmptyFallback bool,
	opts Options,
	pal Palette,
) {
	if rec == nil || coverSource == "" || coverSource == "none" || height <= 0 {
		return
	}

	if coverSource == "content" {
		txt := excerptPreviewText(rec)
		box := ui.Column(c.Key("prev-content:"+rec.ID)).Height(height).Background(pal.HeaderBg).
			BorderWidth(0, 0, 1, 0).BorderColor(pal.Border).Padding(10, 12).Clip()
		box.Children(func() {
			if txt != "" {
				ui.Text(c, txt).FontSize(11.5).LineHeight(1.5).TextColor(pal.TextMuted).Wrap()
			} else {
				ui.Box(c).Grow(1).Center().Children(func() {
					ui.Text(c, i18n.T("暂无正文内容")).FontSize(11.5).TextColor(pal.TextMuted.Alpha(0.6))
				})
			}
		})
		return
	}

	imgURL := resolveCoverImageURL(db, rec, coverSource)
	if imgURL != "" && opts.LoadImage != nil {
		if bmp := opts.LoadImage(rec.Path, imgURL); bmp != nil {
			fit := ui.Cover
			if fitImage {
				fit = ui.Contain
			}
			ui.Box(c.Key("prev-img:"+rec.ID)).Height(height).Background(pal.Surface).
				BorderWidth(0, 0, 1, 0).BorderColor(pal.Border).Clip().Children(func() {
				ui.Image(c.Key("img:"+rec.ID), bmp).WidthPercent(100).Height(height).Fit(fit)
			})
			return
		}
	}

	if !showEmptyFallback {
		return
	}

	if txt := excerptPreviewText(rec); txt != "" {
		ui.Column(c.Key("prev-excerpt:"+rec.ID)).Height(height).Background(pal.HeaderBg).
			BorderWidth(0, 0, 1, 0).BorderColor(pal.Border).Padding(10, 12).Clip().Children(func() {
			ui.Text(c, txt).FontSize(11.5).LineHeight(1.5).TextColor(pal.TextMuted).Wrap()
		})
		return
	}

	ui.Box(c.Key("prev-empty:"+rec.ID)).Height(height).Background(pal.HeaderBg).
		BorderWidth(0, 0, 1, 0).BorderColor(pal.Border).Center().Children(func() {
		ui.Icon(c, iconImage).FontSize(22).TextColor(pal.TextMuted.Alpha(0.35))
	})
}

func renderCardTitleRow(
	c *ui.Context,
	st *State,
	db *database.Database,
	rec *database.Record,
	showIcon bool,
	wrapTitle bool,
	opts Options,
	pal Palette,
) (consumed bool) {
	if rec == nil || db == nil {
		return false
	}
	tp := db.TitleProperty()
	editing := tp != nil && st.Editing && st.EditRecordID == rec.ID && st.EditPropID == tp.ID && !opts.ReadOnly
	if editing {
		in := ui.TextInput(c.Key("card-title-in:"+rec.ID), &st.EditText).
			Grow(1).MinWidth(0).Height(26).Padding(2, 6).Radius(5).
			Background(pal.Paper).Border(1, pal.Accent).TextColor(pal.Text).
			AutoFocus().Label(tp.Name)
		if st.EditFocusPending {
			in.Focus()
			st.EditFocusPending = false
		}
		switch {
		case in.Shortcut(0, ui.KeyEscape):
			st.CancelEdit()
		case in.Submitted():
			st.CommitTextEdit(opts)
		case st.EditWasFocused && !in.Focused():
			st.CommitTextEdit(opts)
		}
		st.EditWasFocused = in.Focused()
		return true
	}

	title := strings.TrimSpace(rec.Title)
	if title == "" {
		title = i18n.T("未命名")
	}
	ui.Row(c.Key("card-title:" + rec.ID)).Gap(6).AlignItems(ui.Center).MinWidth(0).Children(func() {
		if showIcon {
			if rec.Icon != "" {
				ui.Text(c, rec.Icon).FontSize(14).NoWrap()
			} else {
				ui.Icon(c, iconFileDoc).FontSize(13).TextColor(pal.TextMuted.Alpha(0.75))
			}
		}
		lbl := ui.Text(c, title).FontSize(13.5).FontWeight(600).TextColor(pal.Text).MinWidth(0)
		if wrapTitle {
			lbl.Wrap()
		} else {
			lbl.SingleLine()
		}
	})
	return false
}

func renderCardProperties(
	c *ui.Context,
	st *State,
	rec *database.Record,
	props []database.Property,
	compactInline bool,
	opts Options,
	pal Palette,
) (consumed bool) {
	if rec == nil || len(props) == 0 {
		return false
	}

	if compactInline {
		ui.Row(c.Key("cprops:" + rec.ID)).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
			for _, prop := range props {
				p := prop
				val := rec.ValueFor(p)
				if p.Type != database.PropCheckbox && val.IsEmpty() {
					continue
				}
				if renderCompactCardProp(c, st, rec, p, val, true, opts, pal) {
					consumed = true
				}
			}
		})
		return consumed
	}

	hasAny := false
	for _, p := range props {
		if p.Type == database.PropCheckbox || !rec.ValueFor(p).IsEmpty() {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return false
	}

	ui.Column(c.Key("cprops:" + rec.ID)).Gap(5).MinWidth(0).Children(func() {
		for _, prop := range props {
			p := prop
			val := rec.ValueFor(p)
			if p.Type != database.PropCheckbox && val.IsEmpty() {
				continue
			}
			if renderCompactCardProp(c, st, rec, p, val, false, opts, pal) {
				consumed = true
			}
		}
	})
	return consumed
}

func renderCompactCardProp(
	c *ui.Context,
	st *State,
	rec *database.Record,
	prop database.Property,
	val database.Value,
	inlineRow bool,
	opts Options,
	pal Palette,
) (consumed bool) {
	readOnly := opts.ReadOnly ||
		prop.Type == database.PropCreatedTime ||
		prop.Type == database.PropEditedTime ||
		prop.Type == database.PropUniqueID ||
		prop.Type == database.PropRollup ||
		prop.Type == database.PropFormula

	popoverHere := st.EditPopoverOpen && st.ActivePopoverRecordID == rec.ID && st.ActivePopoverPropID == prop.ID

	switch prop.Type {
	case database.PropCheckbox:
		checked := val.Kind == database.ValBool && val.Bool
		row := ui.Row(c.Key("cp:" + prop.ID)).Gap(5).AlignItems(ui.Center)
		if !readOnly {
			row.Cursor(ui.CursorPointer)
		}
		row.Children(func() {
			cb := ui.CheckboxBase(c.Key("cchk"), &checked).Size(15, 15).Radius(4).Center().
				Disabled(readOnly).Label(prop.Name + ": " + rec.Title)
			if !readOnly {
				cb.Cursor(ui.CursorPointer)
			}
			cb.Draw(func(p *ui.Painter, r ui.Rect) {
				if checked {
					p.Fill(r, pal.Accent, 3.5)
					p.Icon(iconCheck, rectInset(r, 2.5), ui.RGB(255, 255, 255))
				} else {
					p.Stroke(rectInset(r, 0.5), pal.BorderStrong, 3.5, 1.2)
				}
			})
			if !inlineRow {
				ui.Text(c, prop.Name).FontSize(11.5).TextColor(pal.TextMuted).SingleLine()
			}
			if cb.Changed() && !readOnly {
				st.setCellAndNotify(rec.ID, prop.ID, database.BoolValue(checked), opts)
				consumed = true
			}
		})
		if row.Hovered() {
			// Ensure clicking right near checkbox label also won't accidentally stay unconsumed if cb toggled.
		}
		return consumed

	case database.PropSelect, database.PropStatus:
		withDot := prop.Type == database.PropStatus
		opt := findOption(prop, val.Text)
		pillBtn := ui.Row(c.Key("cp:" + prop.ID)).AlignItems(ui.Center)
		if !readOnly {
			pillBtn.Cursor(ui.CursorPointer)
		}
		pillBtn.Children(func() {
			renderTagPill(c, val.Text, opt.Color, withDot, pal)
		})
		if pillBtn.Clicked() && !readOnly {
			st.OpenCellPopover(rec, prop, opts.Now)
			consumed = true
		}
		if popoverHere && !readOnly {
			cellPopover(c, pillBtn, st, rec, prop, opts, pal)
			consumed = true
		}
		return consumed

	case database.PropMultiSelect, database.PropPerson, database.PropFiles, database.PropRelation:
		pillBtn := ui.Row(c.Key("cp:" + prop.ID)).AlignItems(ui.Center).MinWidth(0)
		if !readOnly {
			pillBtn.Cursor(ui.CursorPointer)
		}
		pillBtn.Children(func() {
			renderTagListCell(c, prop, relationDisplayLabels(st, prop, val.List), !inlineRow, pal)
		})
		if pillBtn.Clicked() && !readOnly {
			st.OpenCellPopover(rec, prop, opts.Now)
			consumed = true
		}
		if popoverHere && !readOnly {
			cellPopover(c, pillBtn, st, rec, prop, opts, pal)
			consumed = true
		}
		return consumed

	case database.PropDate:
		txt := database.FormatValue(prop, val)
		dateBtn := ui.Row(c.Key("cp:"+prop.ID)).Padding(1, 4).Radius(4).AlignItems(ui.Center)
		if !readOnly {
			dateBtn.Cursor(ui.CursorPointer)
			if dateBtn.Hovered() {
				dateBtn.Background(pal.Surface)
			}
		}
		dateBtn.Children(func() {
			ui.Text(c, txt).FontSize(12).TextColor(pal.TextMuted).SingleLine()
		})
		if dateBtn.Clicked() && !readOnly {
			st.OpenCellPopover(rec, prop, opts.Now)
			consumed = true
		}
		if popoverHere && !readOnly {
			cellPopover(c, dateBtn, st, rec, prop, opts, pal)
			consumed = true
		}
		return consumed

	case database.PropNumber:
		row := ui.Row(c.Key("cp:" + prop.ID)).Gap(4).AlignItems(ui.Center)
		row.Children(func() {
			if !inlineRow {
				ui.Text(c, prop.Name+":").FontSize(11.5).TextColor(pal.TextMuted).SingleLine()
			}
			renderNumberCell(c, prop, val, true, pal)
		})
		return false

	case database.PropRollup, database.PropFormula:
		if val.Kind == database.ValError {
			ui.Text(c, val.Error).FontSize(11.5).TextColor(pal.Danger).SingleLine()
			return false
		}
		if val.Kind == database.ValStringList && len(val.List) > 0 {
			ui.Row(c.Key("cp:" + prop.ID)).AlignItems(ui.Center).MinWidth(0).Children(func() {
				renderTagListCell(c, prop, val.List, !inlineRow, pal)
			})
			return false
		}
		if val.Kind == database.ValNumber {
			row := ui.Row(c.Key("cp:" + prop.ID)).Gap(4).AlignItems(ui.Center)
			row.Children(func() {
				if !inlineRow {
					ui.Text(c, prop.Name+":").FontSize(11.5).TextColor(pal.TextMuted).SingleLine()
				}
				renderNumberCell(c, prop, val, true, pal)
			})
			return false
		}
		txt := database.FormatValue(prop, val)
		if txt == "" {
			return false
		}
		ui.Box(c.Key("cp:" + prop.ID)).MinWidth(0).Children(func() {
			ui.Text(c, txt).FontSize(12).TextColor(pal.TextMuted).SingleLine()
		})
		return false

	case database.PropUniqueID:
		ui.Box(c.Key("cp:"+prop.ID)).Padding(1, 5).Radius(4).Background(pal.Surface).Children(func() {
			ui.Text(c, val.Text).FontSize(11).FontWeight(500).TextColor(pal.TextMuted).SingleLine()
		})
		return false

	default:
		txt := database.FormatValue(prop, val)
		if txt == "" {
			return false
		}
		ui.Box(c.Key("cp:" + prop.ID)).MinWidth(0).Children(func() {
			ui.Text(c, txt).FontSize(12).TextColor(pal.TextMuted).SingleLine()
		})
		return false
	}
}
