package dbview

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

func columnAccentColor(colorName string, pal Palette) ui.Color {
	if colorName == "" || colorName == "default" {
		return pal.Accent
	}
	return StatusDotColor(pal.Dark, colorName)
}

func collectBoardAssignees(res *database.QueryResult, personProp *database.Property) []string {
	if res == nil || personProp == nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, r := range res.Rows {
		if r == nil {
			continue
		}
		for _, n := range extractAssigneeNames(r, personProp) {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	return out
}

func filterRowsByAssignee(rows []*database.Record, personProp *database.Property, filter string) []*database.Record {
	if filter == "" || personProp == nil {
		return rows
	}
	var out []*database.Record
	for _, r := range rows {
		if r == nil {
			continue
		}
		for _, n := range extractAssigneeNames(r, personProp) {
			if n == filter {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

func personInitial(name string) string {
	s := strings.TrimSpace(name)
	if s == "" {
		return "?"
	}
	r, _ := utf8.DecodeRuneInString(s)
	return strings.ToUpper(string(r))
}

func renderPersonAvatarCircle(c *ui.Context, name string, size float32, ringColor ui.Color) ui.Element {
	bg := personAvatarColor(name)
	el := ui.Box(c).Size(size, size).Radius(size * 0.5).Background(bg).Center().Shrink(0)
	if ringColor.A > 0 {
		el.Border(1.5, ringColor)
	}
	el.Children(func() {
		ui.Text(c, personInitial(name)).FontSize(size * 0.46).FontWeight(600).TextColor(solidAvatarTextColor()).NoWrap()
	})
	return el
}

func renderBoardSubToolbar(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	bcfg database.BoardViewConfig,
	groupProp database.Property,
	personProp *database.Property,
	assignees []string,
	opts Options,
	pal Palette,
) {
	ui.Row(c.Key("board-subtb")).Height(36).Padding(2, 22).Gap(8).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(pal.Border.Alpha(0.5)).Children(func() {
		groupLabel := groupProp.Name
		if groupProp.ID == "" {
			groupLabel = i18n.T("未分组")
		}
		// Group-by pill selector.
		grpBtn := ui.Row(c.Key("board-grp-pick")).Height(26).Padding(0, 9).Radius(7).Gap(5).
			Background(pal.Surface).Border(1, pal.Border.Alpha(0.75)).
			AlignItems(ui.Center).Cursor(ui.CursorPointer).Role(ui.RoleMenuButton).
			Label(i18n.T("分组方式") + ": " + groupLabel).
			Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Colors: true})
		if grpBtn.Hovered() {
			grpBtn.Background(pal.RowHover).Border(1, pal.BorderStrong)
		}
		applyPressSpring(grpBtn, false)
		grpBtn.Children(func() {
			ui.Text(c, i18n.T("分组方式")).FontSize(12).TextColor(pal.TextMuted).NoWrap()
			ui.Text(c, groupLabel).FontSize(12.5).FontWeight(500).TextColor(pal.Text).NoWrap()
			ui.Icon(c, iconChevronDown).FontSize(12).TextColor(pal.TextMuted)
		})
		grpBtn.PopoverMenuButton(func(m *ui.Menu) {
			if m.Item(i18n.T("不分组")).Checked(groupProp.ID == "").Chosen() {
				vc := view.Clone()
				vc.Group = nil
				saveViewAndNotify(st, vc, opts)
			}
			var hasGroupProps bool
			for _, p := range db.Properties {
				if p.Type == database.PropTitle {
					continue
				}
				if !hasGroupProps {
					m.Separator()
					hasGroupProps = true
				}
				cand := p
				if m.Item(cand.Name).Checked(cand.ID == groupProp.ID).Chosen() {
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
			if !opts.ReadOnly {
				m.Separator()
				if m.Item("+ " + i18n.T("新建状态列")).Chosen() {
					addDefaultStatusProperty(st, db, *view, opts)
				}
			}
		})

		assigneeLabel := i18n.T("关闭")
		if personProp != nil {
			assigneeLabel = personProp.Name
		}
		assigneeBtn := renderFieldBindingPill(c, "board-assignee-pick", i18n.T("成员"), assigneeLabel, personProp == nil, pal)
		assigneeBtn.PopoverMenuButton(func(m *ui.Menu) {
			isOff := bcfg.AssigneePropID == database.PropBindNone || (personProp == nil && bcfg.AssigneePropID == "")
			if m.Item(i18n.T("关闭")).Checked(isOff).Chosen() {
				st.BoardAssigneeFilter = ""
				vc := view.Clone()
				ensureBoardConfig(&vc).AssigneePropID = database.PropBindNone
				saveViewAndNotify(st, vc, opts)
			}
			cands := assigneePropertiesOf(db)
			if len(cands) > 0 {
				m.Separator()
				for _, p := range cands {
					cand := p
					checked := personProp != nil && personProp.ID == cand.ID && bcfg.AssigneePropID != database.PropBindNone
					if m.Item(cand.Name).Checked(checked).Chosen() {
						st.BoardAssigneeFilter = ""
						vc := view.Clone()
						ensureBoardConfig(&vc).AssigneePropID = cand.ID
						saveViewAndNotify(st, vc, opts)
					}
				}
			}
		})

		// Overlapping Circular Assignee Filter Avatars matching DayFlow .df-board-people.
		if personProp != nil && len(assignees) > 0 {
			hasFilter := st.BoardAssigneeFilter != ""
			ui.Row(c.Key("board-people")).AlignItems(ui.Center).Children(func() {
				for idx, person := range assignees {
					name := person
					active := st.BoardAssigneeFilter == name
					btn := ui.Box(c.Key("bp:"+name)).Size(26, 26).Radius(13).Center().
						Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).
						Label(i18n.T("筛选成员") + ": " + name).Tooltip(name)
					if idx > 0 {
						btn.Margin(0, 0, 0, -5)
					}
					if active {
						btn.Border(2, pal.Accent)
					} else if hasFilter {
						btn.Opacity(0.36)
					}
					btn.Children(func() {
						renderPersonAvatarCircle(c, name, 22, pal.Paper)
					})
					if btn.Clicked() {
						if active {
							st.BoardAssigneeFilter = ""
						} else {
							st.BoardAssigneeFilter = name
						}
					}
				}
			})

			if hasFilter {
				clr := ui.ButtonBase(c.Key("board-clr-filter")).Height(24).Padding(0, 6).Radius(5).
					AlignItems(ui.Center).Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(i18n.T("清除筛选"))
				if clr.Hovered() {
					clr.Background(pal.RowHover)
				}
				clr.Children(func() {
					ui.Text(c, i18n.T("清除筛选")).FontSize(12).TextColor(pal.TextMuted).NoWrap()
				})
				if clr.Clicked() {
					st.BoardAssigneeFilter = ""
				}
			}
		}

		ui.Spacer(c)
	})
}

func canEditBoardOption(groupProp database.Property, grp *database.GroupConfig, groupKey string) bool {
	if groupKey == "" || grp == nil {
		return false
	}
	if groupProp.Type == database.PropSelect {
		return true
	}
	return groupProp.Type == database.PropStatus && !strings.EqualFold(grp.StatusBy, "group")
}

func updateBoardOptionColor(st *State, groupProp database.Property, optionName, colorName string, opts Options) {
	if st == nil || st.Store == nil || opts.ReadOnly {
		return
	}
	updated := groupProp.Clone()
	found := false
	for i := range updated.Options {
		if updated.Options[i].Name == optionName {
			updated.Options[i].Color = colorName
			found = true
			break
		}
	}
	if !found {
		return
	}
	if affected, err := st.Store.UpdateProperty(updated); err == nil && opts.OnChanged != nil {
		opts.OnChanged(affected)
	}
}

func renderBoardColumnRenameBox(
	c *ui.Context,
	st *State,
	db *database.Database,
	res *database.QueryResult,
	groupProp database.Property,
	g database.RecordGroup,
	opts Options,
	pal Palette,
) {
	_ = db
	_ = res
	commit := func() {
		oldName := st.BoardRenamingColKey
		newName := strings.TrimSpace(st.BoardRenamingColDraft)
		st.BoardRenamingColKey = ""
		st.BoardRenamingColDraft = ""
		if newName == "" || newName == oldName || opts.ReadOnly {
			return
		}
		updated := groupProp.Clone()
		for i := range updated.Options {
			if updated.Options[i].Name == oldName {
				updated.Options[i].Name = newName
				break
			}
		}
		if _, err := st.Store.UpdateProperty(updated); err != nil {
			if opts.OnNotify != nil {
				opts.OnNotify(err.Error())
			}
			return
		}
		var edits []database.CellEdit
		for _, r := range g.Rows {
			if r != nil && r.ValueFor(groupProp).Text == oldName {
				edits = append(edits, database.CellEdit{
					RecordID: r.ID,
					PropID:   groupProp.ID,
					Value:    database.TextValue(newName),
				})
			}
		}
		if len(edits) > 0 {
			if affected, err := st.Store.SetCellsBatch(edits); err == nil && opts.OnChanged != nil {
				opts.OnChanged(affected)
			}
		} else if opts.OnChanged != nil {
			opts.OnChanged(nil)
		}
	}

	ui.Row(c.Key("bcol-rename:"+g.Key)).Height(42).Padding(4, 6).Gap(4).AlignItems(ui.Center).
		Background(pal.Paper).Border(1, pal.BorderStrong).Radius(8).Children(func() {
		in := ui.TextInput(c.Key("bcol-rin"), &st.BoardRenamingColDraft).
			Grow(1).MinWidth(0).Height(28).Padding(2, 6).Radius(5).
			Background(pal.Surface).TextColor(pal.Text).AutoFocus().Label(i18n.T("重命名列"))
		if st.BoardRenamingColPending {
			in.Focus()
			st.BoardRenamingColPending = false
		}
		if in.Shortcut(0, ui.KeyEscape) {
			st.BoardRenamingColKey = ""
			st.BoardRenamingColDraft = ""
			return
		}
		if in.Submitted() {
			commit()
			return
		}
		if ui.PrimaryButton(c.Key("bcol-rok"), i18n.T("保存")).Height(26).Padding(0, 8).Clicked() {
			commit()
		}
	})
}

func moveBoardCardHoriz(
	st *State,
	view *database.ViewConfig,
	res *database.QueryResult,
	db *database.Database,
	rec *database.Record,
	fromGroupKey string,
	fromSubKey string,
	dir int,
	opts Options,
) {
	if res == nil || len(res.Groups) == 0 || opts.ReadOnly {
		return
	}
	var visibleGroups []database.RecordGroup
	curIdx := -1
	for _, g := range res.Groups {
		if g.Collapsed {
			continue
		}
		if g.Key == fromGroupKey {
			curIdx = len(visibleGroups)
		}
		visibleGroups = append(visibleGroups, g)
	}
	if curIdx < 0 {
		return
	}
	targetIdx := curIdx + dir
	if targetIdx < 0 || targetIdx >= len(visibleGroups) {
		return
	}
	target := visibleGroups[targetIdx]
	st.BoardFocusedCardID = rec.ID
	st.BoardFocusPending = true
	applyBoardCardDrop(st, view, db, res, boardCardDrag{
		state:        st,
		recordID:     rec.ID,
		fromGroupKey: fromGroupKey,
		fromSubKey:   fromSubKey,
	}, target.Key, target.Value, fromSubKey, database.EmptyValue(), "", opts)
}

func moveBoardCardVert(
	st *State,
	view *database.ViewConfig,
	res *database.QueryResult,
	groupKey string,
	recordID string,
	dir int,
	opts Options,
) {
	if res == nil || len(view.Sorts) > 0 || opts.ReadOnly {
		return
	}
	for _, g := range res.Groups {
		if g.Key != groupKey {
			continue
		}
		idx := -1
		for i, r := range g.Rows {
			if r != nil && r.ID == recordID {
				idx = i
				break
			}
		}
		target := idx + dir
		if idx < 0 || target < 0 || target >= len(g.Rows) || g.Rows[target] == nil {
			return
		}
		st.BoardFocusedCardID = recordID
		st.BoardFocusPending = true
		reorderRecordRow(st, view, res, recordID, g.Rows[target].ID, opts)
		return
	}
}

func isPriorityProperty(p database.Property) bool {
	if p.Type != database.PropSelect && p.Type != database.PropStatus {
		return false
	}
	lower := strings.ToLower(p.Name)
	if strings.Contains(lower, "优先") || strings.Contains(lower, "priority") {
		return true
	}
	for _, opt := range p.Options {
		on := strings.ToLower(opt.Name)
		if strings.HasPrefix(on, "p0") || strings.HasPrefix(on, "p1") || strings.HasPrefix(on, "p2") || on == "high" || on == "medium" || on == "low" {
			return true
		}
	}
	return false
}

func splitDayflowBoardProps(db *database.Database, bcfg database.BoardViewConfig, cardProps []database.Property) (priProp, dateProp, personProp *database.Property, extra []database.Property) {
	priProp = resolveBoardPriorityProp(db, bcfg, cardProps)
	dateProp = resolveBoardDateProp(db, bcfg, cardProps)
	personProp = resolveBoardAssigneeProp(db, bcfg)
	for i := range cardProps {
		p := cardProps[i]
		if priProp != nil && p.ID == priProp.ID {
			continue
		}
		if dateProp != nil && p.ID == dateProp.ID {
			continue
		}
		if personProp != nil && p.ID == personProp.ID {
			continue
		}
		extra = append(extra, p)
	}
	return priProp, dateProp, personProp, extra
}

func renderDayflowPriorityBadge(
	c *ui.Context,
	st *State,
	rec *database.Record,
	priProp database.Property,
	opts Options,
	pal Palette,
) (consumed bool) {
	val := rec.ValueFor(priProp)
	if val.IsEmpty() {
		return false
	}
	opt := findOption(priProp, val.Text)
	level, barCol := priorityLevelAndColor(pal, val.Text, opt)
	popoverHere := st.EditPopoverOpen && st.ActivePopoverRecordID == rec.ID && st.ActivePopoverPropID == priProp.ID

	badge := ui.Row(c.Key("bpri:"+priProp.ID)).Padding(2, 5).Radius(5).Gap(4).
		Background(barCol.Alpha(0.11)).AlignItems(ui.Center).Shrink(0)
	if !opts.ReadOnly {
		badge.Cursor(ui.CursorPointer)
	}
	badge.Children(func() {
		// Linear/DayFlow 3 ascending priority signal bars.
		bars := ui.Box(c).Size(12, 11).Shrink(0)
		bars.Draw(func(p *ui.Painter, r ui.Rect) {
			for b := 0; b < 3; b++ {
				h := float32(3 + b*3)
				x := r.X + 1 + float32(b)*4
				y := r.Y + r.H - 1 - h
				col := barCol
				if b >= level {
					col = barCol.Alpha(0.28)
				}
				p.Fill(ui.Rect{X: x, Y: y, W: 2.5, H: h}, col, 0.75)
			}
		})
		ui.Text(c, val.Text).FontSize(10.5).FontWeight(500).TextColor(barCol).NoWrap()
	})
	if badge.Clicked() && !opts.ReadOnly {
		st.OpenCellPopover(rec, priProp, opts.Now)
		consumed = true
	}
	if popoverHere && !opts.ReadOnly {
		cellPopover(c, badge, st, rec, priProp, opts, pal)
		consumed = true
	}
	return consumed
}

func renderDayflowCardFooter(
	c *ui.Context,
	st *State,
	db *database.Database,
	rec *database.Record,
	dateProp *database.Property,
	personProp *database.Property,
	opts Options,
	pal Palette,
) (consumed bool) {
	hasDate := dateProp != nil && !rec.ValueFor(*dateProp).IsEmpty()
	people := extractAssigneeNames(rec, personProp)
	if !hasDate && len(people) == 0 {
		return false
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	today := dateOnly(now)

	ui.Row(c.Key("bfoot:" + rec.ID)).Gap(6).AlignItems(ui.Center).Justify(ui.SpaceBetween).MinWidth(0).Children(func() {
		if hasDate {
			dv := rec.ValueFor(*dateProp)
			canEditDate := !opts.ReadOnly && dateProp.Type == database.PropDate
			popoverHere := canEditDate && st.EditPopoverOpen && st.ActivePopoverRecordID == rec.ID && st.ActivePopoverPropID == dateProp.ID

			dueBg := pal.Surface
			dueFg := pal.TextMuted
			label := database.FormatValue(*dateProp, dv)
			if t, _, ok := database.ParseDateString(dv.Date.Start, now.Location()); ok {
				d := dateOnly(t)
				if d.Year() == today.Year() {
					label = fmt.Sprintf(i18n.T("%d/%d"), int(d.Month()), d.Day())
				} else {
					label = d.Format("2006/01/02")
				}
				if dv.Date.End != "" {
					if et, _, okE := database.ParseDateString(dv.Date.End, now.Location()); okE {
						ed := dateOnly(et)
						if ed.After(d) {
							label = fmt.Sprintf("%s–%d/%d", label, int(ed.Month()), ed.Day())
						}
					}
				}
				completed := isRecordCompleted(db, rec)
				if !completed {
					if d.Before(today) {
						dueBg = pal.Danger.Alpha(0.12)
						dueFg = pal.Danger
					} else if d.Equal(today) {
						dueBg = pal.AccentSoft
						dueFg = pal.Accent
					}
				}
			}

			chip := ui.Row(c.Key("bdue:"+dateProp.ID)).Height(20).Padding(0, 6).Radius(5).Gap(4).
				Background(dueBg).AlignItems(ui.Center)
			if canEditDate {
				chip.Cursor(ui.CursorPointer)
			}
			chip.Children(func() {
				ui.Icon(c, iconViewCalendar).FontSize(10.5).TextColor(dueFg)
				ui.Text(c, label).FontSize(11).FontWeight(500).TextColor(dueFg).NoWrap()
			})
			if chip.Clicked() && canEditDate {
				st.OpenCellPopover(rec, *dateProp, opts.Now)
				consumed = true
			}
			if popoverHere {
				cellPopover(c, chip, st, rec, *dateProp, opts, pal)
				consumed = true
			}
		} else {
			ui.Box(c)
		}

		if len(people) > 0 && personProp != nil {
			popoverHere := st.EditPopoverOpen && st.ActivePopoverRecordID == rec.ID && st.ActivePopoverPropID == personProp.ID
			stack := ui.Row(c.Key("bpeople:" + personProp.ID)).AlignItems(ui.Center)
			if !opts.ReadOnly {
				stack.Cursor(ui.CursorPointer)
			}
			stack.Children(func() {
				shown := min(len(people), 3)
				for idx := 0; idx < shown; idx++ {
					av := renderPersonAvatarCircle(c, people[idx], 20, pal.Paper)
					if idx > 0 {
						av.Margin(0, 0, 0, -5)
					}
				}
				if len(people) > 3 {
					ui.Text(c, fmt.Sprintf("+%d", len(people)-3)).FontSize(10).TextColor(pal.TextMuted).Margin(0, 0, 0, 3)
				}
			})
			if stack.Clicked() && !opts.ReadOnly {
				st.OpenCellPopover(rec, *personProp, opts.Now)
				consumed = true
			}
			if popoverHere && !opts.ReadOnly {
				cellPopover(c, stack, st, rec, *personProp, opts, pal)
				consumed = true
			}
		}
	})
	return consumed
}

func isRecordCompleted(db *database.Database, rec *database.Record) bool {
	if db == nil || rec == nil {
		return false
	}
	for _, p := range db.Properties {
		if p.Type == database.PropStatus {
			v := rec.ValueFor(p)
			if !v.IsEmpty() {
				opt := findOption(p, v.Text)
				if opt.Group == database.StatusComplete {
					return true
				}
			}
		}
	}
	return false
}
