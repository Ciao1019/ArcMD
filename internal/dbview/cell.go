package dbview

import (
	"fmt"
	"math"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

type cellOutcome struct {
	clicked bool
	tabDir  int // 1 for Tab, -1 for Shift+Tab
}

func rectInset(r ui.Rect, d float32) ui.Rect {
	return ui.Rect{X: r.X + d, Y: r.Y + d, W: max(0, r.W-2*d), H: max(0, r.H-2*d)}
}

func isInlineTextProp(pt database.PropertyType) bool {
	switch pt {
	case database.PropTitle, database.PropText, database.PropNumber,
		database.PropURL, database.PropEmail, database.PropPhone:
		return true
	}
	return false
}

func isPopoverProp(pt database.PropertyType) bool {
	switch pt {
	case database.PropSelect, database.PropMultiSelect, database.PropStatus,
		database.PropDate, database.PropPerson, database.PropFiles, database.PropRelation:
		return true
	}
	return false
}

func cellView(
	c *ui.Context,
	st *State,
	rec *database.Record,
	prop database.Property,
	focused bool,
	wrap bool,
	leftAlign bool,
	opts Options,
	pal Palette,
) cellOutcome {
	var out cellOutcome
	if rec == nil {
		return out
	}

	editingInline := st.Editing && st.EditRecordID == rec.ID && st.EditPropID == prop.ID
	popoverHere := st.EditPopoverOpen && st.ActivePopoverRecordID == rec.ID && st.ActivePopoverPropID == prop.ID

	box := ui.Row(c.Key("c:"+rec.ID+":"+prop.ID)).
		Grow(1).MinWidth(0).MinHeight(34).Padding(5, 8).Gap(6).AlignItems(ui.Center)

	if focused && !editingInline {
		box.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Stroke(rectInset(r, 0.5), pal.Accent, 4, 1.5)
		})
	}

	val := rec.ValueFor(prop)

	if editingInline && !opts.ReadOnly {
		box.Children(func() {
			in := ui.TextInput(c.Key("in"), &st.EditText).Grow(1).MinWidth(0).Height(26).Padding(2, 6).
				TextColor(pal.Text).Background(pal.Paper).Radius(4).Border(1, pal.Accent).
				AutoFocus().Label(prop.Name)
			if st.EditFocusPending {
				in.Focus()
				st.EditFocusPending = false
			}
			switch {
			case in.Shortcut(0, ui.KeyEscape):
				st.CancelEdit()
			case in.Shortcut(ui.Shift, ui.KeyTab):
				st.CommitTextEdit(opts)
				out.tabDir = -1
			case in.Shortcut(0, ui.KeyTab):
				st.CommitTextEdit(opts)
				out.tabDir = 1
			case in.Submitted():
				st.CommitTextEdit(opts)
			case st.EditWasFocused && !in.Focused():
				st.CommitTextEdit(opts)
			}
			st.EditWasFocused = in.Focused()
		})
		return out
	}

	readOnlyProp := opts.ReadOnly ||
		prop.Type == database.PropCreatedTime ||
		prop.Type == database.PropEditedTime ||
		prop.Type == database.PropUniqueID ||
		prop.Type == database.PropRollup ||
		prop.Type == database.PropFormula

	if !readOnlyProp && prop.Type != database.PropCheckbox {
		box.Cursor(ui.CursorPointer)
	}

	box.Children(func() {
		switch prop.Type {
		case database.PropCheckbox:
			checked := val.Kind == database.ValBool && val.Bool
			cb := ui.CheckboxBase(c.Key("chk"), &checked).Size(18, 18).Radius(4).Center().
				Disabled(readOnlyProp).Label(prop.Name)
			if !readOnlyProp {
				cb.Cursor(ui.CursorPointer)
			}
			cb.Draw(func(p *ui.Painter, r ui.Rect) {
				if checked {
					p.Fill(r, pal.Accent, 4)
					p.Icon(iconCheck, rectInset(r, 3), ui.RGB(255, 255, 255))
				} else {
					p.Stroke(rectInset(r, 0.5), pal.BorderStrong, 4, 1.3)
				}
			})
			if cb.Changed() && !readOnlyProp {
				st.setCellAndNotify(rec.ID, prop.ID, database.BoolValue(checked), opts)
			}

		case database.PropNumber:
			renderNumberCell(c, prop, val, leftAlign, pal)

		case database.PropSelect:
			if !val.IsEmpty() {
				opt := findOption(prop, val.Text)
				renderTagPill(c, val.Text, opt.Color, false, pal)
			}

		case database.PropStatus:
			if !val.IsEmpty() {
				opt := findOption(prop, val.Text)
				renderTagPill(c, val.Text, opt.Color, true, pal)
			}

		case database.PropMultiSelect, database.PropPerson, database.PropFiles, database.PropRelation:
			if len(val.List) > 0 {
				renderTagListCell(c, prop, relationDisplayLabels(st, prop, val.List), wrap, pal)
			}

		case database.PropDate, database.PropCreatedTime, database.PropEditedTime:
			if !val.IsEmpty() {
				text := database.FormatValue(prop, val)
				el := ui.Text(c, text).FontSize(13).TextColor(pal.Text).MinWidth(0)
				if !wrap {
					el.SingleLine()
				}
			}

		case database.PropURL:
			if !val.IsEmpty() {
				u := val.Text
				el := ui.Text(c, u).FontSize(13).TextColor(pal.Accent).MinWidth(0).Grow(1)
				if !wrap {
					el.SingleLine()
				}
				if box.Hovered() || focused {
					btn := ui.Box(c.Key("open-url")).Size(22, 22).Radius(5).Center().Cursor(ui.CursorPointer).
						Role(ui.RoleButton).Label(i18n.T("打开链接")).Tooltip(u)
					if btn.Hovered() {
						btn.Background(pal.Surface)
					}
					btn.Children(func() { ui.Icon(c, iconExternal).FontSize(12).TextColor(pal.TextMuted) })
					if btn.Clicked() {
						if opts.OnOpenURL != nil {
							opts.OnOpenURL(u)
						}
						return
					}
				}
			}

		case database.PropUniqueID:
			if !val.IsEmpty() {
				ui.Box(c).Padding(2, 6).Radius(5).Background(pal.Surface).Children(func() {
					ui.Text(c, val.Text).FontSize(11.5).FontWeight(500).TextColor(pal.TextMuted).SingleLine()
				})
			}

		case database.PropFormula, database.PropRollup:
			if val.Kind == database.ValError {
				ui.Text(c, val.Error).FontSize(12).TextColor(pal.Danger).SingleLine()
			} else if val.Kind == database.ValStringList && len(val.List) > 0 {
				renderTagListCell(c, prop, val.List, wrap, pal)
			} else if val.Kind == database.ValNumber && prop.Number != nil && (prop.Number.ShowAs == "bar" || prop.Number.ShowAs == "ring") {
				renderNumberCell(c, prop, val, leftAlign, pal)
			} else if !val.IsEmpty() {
				txt := database.FormatValue(prop, val)
				el := ui.Text(c, txt).FontSize(13).TextColor(pal.TextMuted).MinWidth(0)
				if !wrap {
					el.SingleLine()
				}
			}

		default: // PropText, PropEmail, PropPhone, PropTitle
			if !val.IsEmpty() {
				txt := database.FormatValue(prop, val)
				el := ui.Text(c, txt).FontSize(13).TextColor(pal.Text).MinWidth(0)
				if !wrap {
					el.SingleLine()
				}
			}
		}
	})

	if box.Clicked() {
		out.clicked = true
		if !readOnlyProp && prop.Type != database.PropCheckbox {
			if isInlineTextProp(prop.Type) {
				initial := ""
				if prop.Type == database.PropNumber && val.Kind == database.ValNumber {
					initial = database.FormatValue(database.Property{Type: database.PropText}, val)
				} else if !val.IsEmpty() {
					initial = val.PlainString()
				}
				st.StartEditCell(rec.ID, prop.ID, initial)
			} else if isPopoverProp(prop.Type) {
				st.OpenCellPopover(rec, prop, opts.Now)
			}
		}
	}

	if popoverHere && !readOnlyProp && isPopoverProp(prop.Type) {
		cellPopover(c, box, st, rec, prop, opts, pal)
	}
	return out
}

func renderNumberCell(c *ui.Context, prop database.Property, val database.Value, leftAlign bool, pal Palette) {
	if val.IsEmpty() {
		return
	}
	txt := database.FormatValue(prop, val)
	cfg := prop.Number
	if cfg == nil || (cfg.ShowAs != "bar" && cfg.ShowAs != "ring") {
		if !leftAlign {
			ui.Spacer(c)
		}
		ui.Text(c, txt).FontSize(13).TextColor(pal.Text).SingleLine()
		return
	}

	denom := cfg.DivideBy
	if denom <= 0 {
		denom = 100
		if strings.EqualFold(cfg.Format, "percent") {
			denom = 1
		}
	}
	ratio := float32(math.Min(1, math.Max(0, val.Number/denom)))
	barColor := pal.Accent
	if cfg.BarColor != "" && cfg.BarColor != "default" {
		barColor = StatusDotColor(pal.Dark, cfg.BarColor)
	}

	if cfg.ShowAs == "ring" {
		if cfg.ShowNumber || cfg.Format != "" {
			ui.Text(c, txt).FontSize(12.5).TextColor(pal.Text).SingleLine()
		}
		ring := ui.Box(c).Size(18, 18).Shrink(0)
		ring.Draw(func(p *ui.Painter, r ui.Rect) {
			cx, cy := r.X+r.W/2, r.Y+r.H/2
			rad := float32(6.5)
			track := (&ui.Path{}).Circle(cx, cy, rad)
			p.StrokePath(track, 2.4, pal.BorderStrong)
			if ratio > 0 {
				arc := (&ui.Path{}).Circle(cx, cy, rad).Trim(0, ratio).Cap(ui.RoundCap)
				p.Transform(ui.Rotation(-90), cx, cy, func() {
					p.StrokePath(arc, 2.4, barColor)
				})
			}
		})
		return
	}

	// Bar visualization.
	ui.Row(c).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
		if cfg.ShowNumber || cfg.Format != "" {
			ui.Text(c, txt).FontSize(12.5).TextColor(pal.Text).SingleLine()
		}
		bar := ui.Box(c).Grow(1).MinWidth(36).Height(6).Radius(3)
		bar.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(r, pal.Surface, 3)
			if ratio > 0 {
				fw := max(4, r.W*ratio)
				p.Fill(ui.Rect{X: r.X, Y: r.Y, W: min(r.W, fw), H: r.H}, barColor, 3)
			}
		})
	})
}

func relationDisplayLabels(st *State, prop database.Property, refs []string) []string {
	if prop.Type != database.PropRelation || len(refs) == 0 {
		return refs
	}
	isSelf := prop.Relation == nil || strings.TrimSpace(prop.Relation.TargetDB) == ""
	var recs []*database.Record
	if isSelf && st != nil && st.Store != nil {
		recs = st.Store.Records()
	}
	out := make([]string, len(refs))
	for i, ref := range refs {
		label := ref
		if recs != nil {
			if matched := database.MatchRelationRecord(recs, ref); matched != nil && matched.Title != "" {
				if matched.Icon != "" {
					label = matched.Icon + " " + matched.Title
				} else {
					label = matched.Title
				}
				out[i] = label
				continue
			}
		}
		trimmed := strings.TrimSpace(ref)
		if slash := strings.LastIndex(trimmed, "/"); slash >= 0 && slash+1 < len(trimmed) {
			trimmed = trimmed[slash+1:]
		}
		trimmed = strings.TrimSuffix(trimmed, ".md")
		if trimmed != "" {
			label = trimmed
		}
		out[i] = label
	}
	return out
}

func renderTagPill(c *ui.Context, name, colorName string, withDot bool, pal Palette) {
	renderStyledPill(c, name, colorName, withDot, nil, pal)
}

func renderStyledPill(c *ui.Context, name, colorName string, withDot bool, pillIcon *ui.SVG, pal Palette) {
	bg, fg := TagColors(pal.Dark, colorName)
	pill := ui.Row(c).Padding(2, 8).Radius(5).Background(bg).Gap(5).AlignItems(ui.Center).Shrink(0)
	pill.Children(func() {
		if pillIcon != nil {
			ui.Icon(c, pillIcon).FontSize(11).TextColor(pal.TextMuted)
		} else if withDot {
			dot := StatusDotColor(pal.Dark, colorName)
			ui.Box(c).Size(6, 6).Radius(3).Background(dot).Shrink(0)
		}
		ui.Text(c, name).FontSize(12).FontWeight(500).TextColor(fg).SingleLine()
	})
}

func renderTagListCell(c *ui.Context, prop database.Property, items []string, wrap bool, pal Palette) {
	maxShow := len(items)
	if !wrap && maxShow > 3 {
		maxShow = 3
	}
	row := ui.Row(c).Gap(4).AlignItems(ui.Center).MinWidth(0)
	if wrap {
		row.Wrap()
	} else {
		row.Grow(1).Clip()
	}
	var pillIcon *ui.SVG
	if prop.Type == database.PropRelation {
		pillIcon = iconPropRelation
	}
	row.Children(func() {
		for i := 0; i < maxShow; i++ {
			item := items[i]
			color := "default"
			if prop.Type == database.PropMultiSelect {
				color = findOption(prop, item).Color
			}
			renderStyledPill(c, item, color, false, pillIcon, pal)
		}
		if len(items) > maxShow {
			ui.Box(c).Padding(2, 6).Radius(5).Background(pal.Surface).Shrink(0).Children(func() {
				ui.Text(c, fmt.Sprintf("+%d", len(items)-maxShow)).FontSize(11.5).TextColor(pal.TextMuted).SingleLine()
			})
		}
	})
}

func findOption(prop database.Property, val string) database.SelectOption {
	for _, opt := range prop.Options {
		if opt.ID == val || strings.EqualFold(opt.Name, val) {
			return opt
		}
	}
	return database.SelectOption{Name: val, Color: "default"}
}
