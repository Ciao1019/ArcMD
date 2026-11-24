package dbview

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

func formView(
	c *ui.Context,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	opts Options,
	pal Palette,
) {
	_ = res
	ensureFormDraftState(st, view, db)
	fcfg := ensureFormConfig(&view, db)
	fields := resolvedFormFields(db, fcfg)

	ui.Scroll(c.Key("form-scroll")).TrackScroll(&st.FormScroll).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
		ui.Column(c.Key("form-center")).FillWidth().AlignItems(ui.Center).Padding(20, 22, 32, 22).Children(func() {
			ui.Column(c.Key("form-shell")).Width(640).MaxWidthPercent(100).Gap(12).Children(func() {
				if !opts.ReadOnly {
					ui.Row(c.Key("form-mode-bar")).Justify(ui.End).Children(func() {
						ui.Row(c).Gap(2).Padding(2).Radius(7).Background(pal.Surface).Children(func() {
							for _, m := range []struct {
								edit  bool
								label string
							}{
								{false, i18n.T("填写表单")},
								{true, i18n.T("编辑表单")},
							} {
								cand := m
								active := st.FormEditMode == cand.edit
								btn := ui.ButtonBase(c.Key("fm:"+cand.label)).Height(26).Padding(0, 10).Radius(5).
									AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().
									Role(ui.RoleTab).Selected(active).Label(cand.label)
								if active {
									btn.Background(pal.Paper).Shadow(0, 1, 2, 0, pal.ShadowEdge)
								} else if btn.Hovered() {
									btn.Background(pal.RowHover)
								}
								btn.Children(func() {
									ink := pal.TextMuted
									if active {
										ink = pal.Text
									}
									ui.Text(c, cand.label).FontSize(12).FontWeight(500).TextColor(ink).SingleLine()
								})
								if btn.Clicked() && st.FormEditMode != cand.edit {
									st.FormEditMode = cand.edit
									if cand.edit {
										st.FormTitleDraft = fcfg.Title
										st.FormDescDraft = fcfg.Description
										st.FormSubmitTextDraft = fcfg.SubmitText
									}
								}
							}
						})
					})
				}

				if st.FormEditMode && !opts.ReadOnly {
					renderFormBuilderCard(c, st, view, db, *fcfg, fields, opts, pal)
					return
				}

				renderFormFillCard(c, st, view, db, *fcfg, fields, opts, pal)
			})
		})
	})
}

func formDraftPtr(m map[string]*string, key string) *string {
	if p := m[key]; p != nil {
		return p
	}
	p := new(string)
	m[key] = p
	return p
}

func formDraftVal(m map[string]*string, key string) string {
	if p := m[key]; p != nil {
		return *p
	}
	return ""
}

func setFormDraftVal(m map[string]*string, key, val string) {
	*formDraftPtr(m, key) = val
}

func ensureFormDraftState(st *State, view database.ViewConfig, db *database.Database) {
	if st.FormDraft == nil {
		st.FormDraft = make(map[string]database.Value)
	}
	if st.FormTextDrafts == nil {
		st.FormTextDrafts = make(map[string]*string)
	}
	if st.FormDateEndDrafts == nil {
		st.FormDateEndDrafts = make(map[string]*string)
	}
	if st.FormErrors == nil {
		st.FormErrors = make(map[string]string)
	}
	if st.FormViewID == view.ID {
		return
	}
	st.FormViewID = view.ID
	st.FormSubmittedTitle = ""
	st.FormLastRecordID = ""
	resetFormInputs(st, view.Form, db)
	if view.Form != nil {
		st.FormTitleDraft = view.Form.Title
		st.FormDescDraft = view.Form.Description
		st.FormSubmitTextDraft = view.Form.SubmitText
	} else if db != nil {
		st.FormTitleDraft = db.Title
		st.FormSubmitTextDraft = i18n.T("提交")
	}
}

func resetFormInputs(st *State, fcfg *database.FormViewConfig, db *database.Database) {
	clear(st.FormDraft)
	for _, p := range st.FormTextDrafts {
		if p != nil {
			*p = ""
		}
	}
	for _, p := range st.FormDateEndDrafts {
		if p != nil {
			*p = ""
		}
	}
	clear(st.FormErrors)
	if fcfg == nil || db == nil {
		return
	}
	for _, f := range fcfg.Fields {
		if f.Default == nil || f.Default.IsEmpty() {
			continue
		}
		prop, idx := db.PropertyByID(f.PropertyID)
		if idx < 0 {
			continue
		}
		switch prop.Type {
		case database.PropCheckbox, database.PropSelect, database.PropStatus, database.PropMultiSelect:
			st.FormDraft[prop.ID] = f.Default.Clone()
		case database.PropDate:
			setFormDraftVal(st.FormTextDrafts, prop.ID, f.Default.Date.Start)
			setFormDraftVal(st.FormDateEndDrafts, prop.ID, f.Default.Date.End)
		default:
			setFormDraftVal(st.FormTextDrafts, prop.ID, f.Default.PlainString())
		}
	}
}

func resolvedFormFields(db *database.Database, fcfg *database.FormViewConfig) []database.FormFieldConfig {
	if db == nil {
		return nil
	}
	var out []database.FormFieldConfig
	seen := make(map[string]bool, len(db.Properties))
	if fcfg != nil {
		for _, f := range fcfg.Fields {
			if p, idx := db.PropertyByID(f.PropertyID); idx >= 0 && isFormEditableProp(p.Type) && !seen[p.ID] {
				seen[p.ID] = true
				cf := f
				if p.Type == database.PropTitle {
					cf.Required = true
					cf.Hidden = false
				}
				out = append(out, cf)
			}
		}
	}
	for _, p := range db.Properties {
		if !isFormEditableProp(p.Type) || seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		out = append(out, database.FormFieldConfig{
			PropertyID: p.ID,
			Required:   p.Type == database.PropTitle,
		})
	}
	return out
}

func renderFormFillCard(
	c *ui.Context,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	fcfg database.FormViewConfig,
	fields []database.FormFieldConfig,
	opts Options,
	pal Palette,
) {
	title := strings.TrimSpace(fcfg.Title)
	if title == "" {
		title = db.Title
	}
	submitText := strings.TrimSpace(fcfg.SubmitText)
	if submitText == "" {
		submitText = i18n.T("提交")
	}

	ui.Column(c.Key("form-card")).Padding(24, 28).Radius(12).Background(pal.Paper).
		Border(1, pal.Border).Shadow(0, 2, 8, 0, pal.ShadowEdge).Gap(16).Children(func() {
		// Form Title + Description.
		ui.Column(c.Key("form-hdr")).Gap(6).Children(func() {
			ui.Text(c, title).FontSize(20).FontWeight(700).TextColor(pal.Text).Wrap()
			if desc := strings.TrimSpace(fcfg.Description); desc != "" {
				ui.Text(c, desc).FontSize(13).LineHeight(1.5).TextColor(pal.TextMuted).Wrap()
			}
		})

		// Submission Confirmation Banner.
		if st.FormSubmittedTitle != "" {
			okBg, okFg := TagColors(pal.Dark, "green")
			ui.Row(c.Key("form-done-banner")).Padding(10, 12).Radius(8).Background(okBg).
				Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconCheck).FontSize(14).TextColor(okFg)
				ui.Text(c, fmt.Sprintf(i18n.T("已提交「%s」"), st.FormSubmittedTitle)).
					FontSize(13).FontWeight(500).TextColor(okFg).SingleLine().Grow(1).MinWidth(0)
				if opts.OnOpenRecord != nil && st.FormLastRecordID != "" {
					if rec := st.Store.Record(st.FormLastRecordID); rec != nil {
						if ui.Button(c.Key("form-view-rec"), i18n.T("查看记录")).Height(24).Padding(0, 8).Clicked() {
							opts.OnOpenRecord(rec, resolveCardOpenMode(view))
						}
					}
				}
				if ui.Button(c.Key("form-done-close"), i18n.T("关闭")).Height(24).Padding(0, 8).Clicked() {
					st.FormSubmittedTitle = ""
				}
			})
		}

		ui.Box(c).Height(1).Background(pal.Border)

		// Visible Form Fields.
		for _, fld := range fields {
			if fld.Hidden {
				continue
			}
			prop, idx := db.PropertyByID(fld.PropertyID)
			if idx < 0 {
				continue
			}
			renderFormFieldInput(c, st, *prop, fld, opts, pal)
		}

		// Optional Body Note.
		ui.Column(c.Key("ff-body-box")).Gap(6).Children(func() {
			ui.Text(c, i18n.T("正文内容")).FontSize(13).FontWeight(600).TextColor(pal.Text)
			ui.TextArea(c.Key("ff-body"), formDraftPtr(st.FormTextDrafts, "_body")).MinHeight(88).Padding(8, 10).Radius(7).
				Background(pal.Surface).Border(1, pal.Border).TextColor(pal.Text).
				Placeholder(i18n.T("可选补充正文（Markdown）…")).Label(i18n.T("正文内容")).Disabled(opts.ReadOnly)
		})

		if !opts.ReadOnly {
			ui.Row(c.Key("form-actions")).Gap(8).Justify(ui.End).Padding(6, 0, 0, 0).Children(func() {
				if ui.Button(c.Key("form-reset"), i18n.T("清空")).Height(32).Padding(0, 14).Clicked() {
					resetFormInputs(st, &fcfg, db)
					st.FormSubmittedTitle = ""
				}
				if ui.PrimaryButton(c.Key("form-submit"), submitText).Height(32).Padding(0, 18).Clicked() {
					submitDatabaseForm(st, db, fcfg, fields, opts)
				}
			})
		}
	})
}

func renderFormFieldInput(
	c *ui.Context,
	st *State,
	prop database.Property,
	fld database.FormFieldConfig,
	opts Options,
	pal Palette,
) {
	lbl := strings.TrimSpace(fld.Label)
	if lbl == "" {
		lbl = prop.Name
	}
	errMsg := st.FormErrors[prop.ID]
	borderCol := pal.Border
	if errMsg != "" {
		borderCol = pal.Danger
	}

	ui.Column(c.Key("ff-box:" + prop.ID)).Gap(6).Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, PropertyIcon(prop.Type)).FontSize(12.5).TextColor(pal.TextMuted)
			ui.Text(c, lbl).FontSize(13).FontWeight(600).TextColor(pal.Text).SingleLine()
			if fld.Required {
				ui.Text(c, "*").FontSize(13).FontWeight(700).TextColor(pal.Danger).NoWrap()
			}
		})
		if help := strings.TrimSpace(fld.Help); help != "" {
			ui.Text(c, help).FontSize(11.5).TextColor(pal.TextMuted).Wrap()
		}

		switch prop.Type {
		case database.PropCheckbox:
			val := st.FormDraft[prop.ID]
			checked := val.Kind == database.ValBool && val.Bool
			row := ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				cb := ui.CheckboxBase(c.Key("ff-chk:"+prop.ID), &checked).Size(18, 18).Radius(4).Center().
					Disabled(opts.ReadOnly).Label(lbl)
				if !opts.ReadOnly {
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
				if cb.Changed() && !opts.ReadOnly {
					st.FormDraft[prop.ID] = database.BoolValue(checked)
					delete(st.FormErrors, prop.ID)
				}
				ui.Text(c, i18n.T("是 / 勾选")).FontSize(12.5).TextColor(pal.TextMuted).SingleLine()
			})
			_ = row

		case database.PropSelect, database.PropStatus:
			cur := st.FormDraft[prop.ID].Text
			ui.Row(c.Key("ff-opts:" + prop.ID)).Wrap().Gap(6).Children(func() {
				for _, opt := range prop.Options {
					cand := opt
					selected := strings.EqualFold(cur, cand.Name)
					bg, fg := TagColors(pal.Dark, cand.Color)
					chip := ui.Row(c.Key("fopt:"+cand.ID)).Height(28).Padding(0, 10).Radius(6).
						Gap(5).AlignItems(ui.Center).Background(bg).Cursor(ui.CursorPointer).
						Focusable().Role(ui.RoleOption).Selected(selected).Label(cand.Name)
					if selected {
						chip.Border(1.5, pal.Accent)
					} else {
						chip.Border(1, pal.Border)
					}
					chip.Children(func() {
						if prop.Type == database.PropStatus {
							dot := StatusDotColor(pal.Dark, cand.Color)
							ui.Box(c).Size(6, 6).Radius(3).Background(dot).Shrink(0)
						}
						ui.Text(c, cand.Name).FontSize(12).FontWeight(500).TextColor(fg).SingleLine()
						if selected {
							ui.Icon(c, iconCheck).FontSize(12).TextColor(pal.Accent)
						}
					})
					if chip.Clicked() && !opts.ReadOnly {
						if selected {
							delete(st.FormDraft, prop.ID)
						} else {
							st.FormDraft[prop.ID] = database.TextValue(cand.Name)
						}
						delete(st.FormErrors, prop.ID)
					}
				}
			})

		case database.PropMultiSelect:
			curList := slices.Clone(st.FormDraft[prop.ID].List)
			ui.Row(c.Key("ff-mopts:" + prop.ID)).Wrap().Gap(6).Children(func() {
				for _, opt := range prop.Options {
					cand := opt
					selected := slices.ContainsFunc(curList, func(s string) bool { return strings.EqualFold(s, cand.Name) })
					bg, fg := TagColors(pal.Dark, cand.Color)
					chip := ui.Row(c.Key("fmopt:"+cand.ID)).Height(28).Padding(0, 10).Radius(6).
						Gap(5).AlignItems(ui.Center).Background(bg).Cursor(ui.CursorPointer).
						Focusable().Role(ui.RoleOption).Selected(selected).Label(cand.Name)
					if selected {
						chip.Border(1.5, pal.Accent)
					} else {
						chip.Border(1, pal.Border)
					}
					chip.Children(func() {
						ui.Text(c, cand.Name).FontSize(12).FontWeight(500).TextColor(fg).SingleLine()
						if selected {
							ui.Icon(c, iconCheck).FontSize(12).TextColor(pal.Accent)
						}
					})
					if chip.Clicked() && !opts.ReadOnly {
						if idx := slices.IndexFunc(curList, func(s string) bool { return strings.EqualFold(s, cand.Name) }); idx >= 0 {
							curList = slices.Delete(curList, idx, idx+1)
						} else {
							curList = append(curList, cand.Name)
						}
						if len(curList) == 0 {
							delete(st.FormDraft, prop.ID)
						} else {
							st.FormDraft[prop.ID] = database.ListValue(curList...)
						}
						delete(st.FormErrors, prop.ID)
					}
				}
			})

		case database.PropDate:
			startPtr := formDraftPtr(st.FormTextDrafts, prop.ID)
			endPtr := formDraftPtr(st.FormDateEndDrafts, prop.ID)
			ui.Row(c.Key("ff-date:" + prop.ID)).Gap(6).AlignItems(ui.Center).Children(func() {
				sin := ui.TextInput(c.Key("ff-ds:"+prop.ID), startPtr).
					Placeholder("YYYY-MM-DD").Label(lbl).
					Grow(1).MinWidth(0).Height(32).Padding(4, 10).Radius(7).
					Background(pal.Surface).Border(1, borderCol).TextColor(pal.Text).Disabled(opts.ReadOnly)
				if sin.Changed() {
					delete(st.FormErrors, prop.ID)
				}
				ui.Text(c, "–").FontSize(13).TextColor(pal.TextMuted)
				ein := ui.TextInput(c.Key("ff-de:"+prop.ID), endPtr).
					Placeholder(i18n.T("结束日期（可选）")).Label(lbl+" "+i18n.T("结束日期")).
					Grow(1).MinWidth(0).Height(32).Padding(4, 10).Radius(7).
					Background(pal.Surface).Border(1, pal.Border).TextColor(pal.Text).Disabled(opts.ReadOnly)
				if ein.Changed() {
					delete(st.FormErrors, prop.ID)
				}
				if !opts.ReadOnly {
					if ui.Button(c.Key("ff-dtoday:"+prop.ID), i18n.T("今天")).Height(32).Padding(0, 10).Clicked() {
						now := opts.Now
						if now.IsZero() {
							now = time.Now()
						}
						setFormDraftVal(st.FormTextDrafts, prop.ID, now.Format("2006-01-02"))
						delete(st.FormErrors, prop.ID)
					}
				}
			})

		default:
			ph := i18n.T("请输入…")
			if prop.Type == database.PropNumber {
				ph = "0"
			} else if prop.Type == database.PropPerson || prop.Type == database.PropFiles || prop.Type == database.PropRelation {
				ph = i18n.T("多个条目可用逗号分隔…")
			}
			in := ui.TextInput(c.Key("ff-in:"+prop.ID), formDraftPtr(st.FormTextDrafts, prop.ID)).
				Placeholder(ph).Label(lbl).
				Height(34).Padding(4, 10).Radius(7).
				Background(pal.Surface).Border(1, borderCol).TextColor(pal.Text).Disabled(opts.ReadOnly)
			if in.Changed() {
				delete(st.FormErrors, prop.ID)
			}
		}

		if errMsg != "" {
			ui.Text(c, errMsg).FontSize(11.5).TextColor(pal.Danger)
		}
	})
}

func submitDatabaseForm(
	st *State,
	db *database.Database,
	fcfg database.FormViewConfig,
	fields []database.FormFieldConfig,
	opts Options,
) {
	if st == nil || st.Store == nil || db == nil || opts.ReadOnly {
		return
	}
	clear(st.FormErrors)

	title := ""
	props := make(map[string]database.Value)

	for _, fld := range fields {
		if fld.Hidden {
			continue
		}
		prop, idx := db.PropertyByID(fld.PropertyID)
		if idx < 0 {
			continue
		}
		switch prop.Type {
		case database.PropTitle:
			raw := strings.TrimSpace(formDraftVal(st.FormTextDrafts, prop.ID))
			if fld.Required && raw == "" {
				st.FormErrors[prop.ID] = i18n.T("此项为必填")
				continue
			}
			title = raw

		case database.PropCheckbox:
			if v, ok := st.FormDraft[prop.ID]; ok {
				props[prop.ID] = v.Clone()
			}

		case database.PropSelect, database.PropStatus:
			v := st.FormDraft[prop.ID]
			if fld.Required && v.IsEmpty() {
				st.FormErrors[prop.ID] = i18n.T("请选择一项")
				continue
			}
			if !v.IsEmpty() {
				props[prop.ID] = v.Clone()
			}

		case database.PropMultiSelect:
			v := st.FormDraft[prop.ID]
			if fld.Required && v.IsEmpty() {
				st.FormErrors[prop.ID] = i18n.T("请至少选择一项")
				continue
			}
			if !v.IsEmpty() {
				props[prop.ID] = v.Clone()
			}

		case database.PropNumber:
			raw := strings.TrimSpace(formDraftVal(st.FormTextDrafts, prop.ID))
			if raw == "" {
				if fld.Required {
					st.FormErrors[prop.ID] = i18n.T("此项为必填")
				}
				continue
			}
			n, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				st.FormErrors[prop.ID] = i18n.T("请输入有效数字")
				continue
			}
			props[prop.ID] = database.NumberValue(n)

		case database.PropDate:
			startStr := strings.TrimSpace(formDraftVal(st.FormTextDrafts, prop.ID))
			endStr := strings.TrimSpace(formDraftVal(st.FormDateEndDrafts, prop.ID))
			if startStr == "" {
				if fld.Required {
					st.FormErrors[prop.ID] = i18n.T("请填写日期")
				}
				continue
			}
			_, incTime, ok := database.ParseDateString(startStr, time.UTC)
			if !ok {
				st.FormErrors[prop.ID] = i18n.T("日期格式应为 YYYY-MM-DD")
				continue
			}
			if endStr != "" {
				if _, _, okEnd := database.ParseDateString(endStr, time.UTC); !okEnd {
					st.FormErrors[prop.ID] = i18n.T("结束日期格式应为 YYYY-MM-DD")
					continue
				}
			}
			props[prop.ID] = database.DateRangeValue(startStr, endStr, incTime)

		case database.PropPerson, database.PropFiles, database.PropRelation:
			raw := strings.TrimSpace(formDraftVal(st.FormTextDrafts, prop.ID))
			if raw == "" {
				if fld.Required {
					st.FormErrors[prop.ID] = i18n.T("此项为必填")
				}
				continue
			}
			var xs []string
			for _, part := range strings.Split(raw, ",") {
				if s := strings.TrimSpace(part); s != "" {
					xs = append(xs, s)
				}
			}
			if len(xs) > 0 {
				props[prop.ID] = database.ListValue(xs...)
			}

		default:
			raw := strings.TrimSpace(formDraftVal(st.FormTextDrafts, prop.ID))
			if raw == "" {
				if fld.Required {
					st.FormErrors[prop.ID] = i18n.T("此项为必填")
				}
				continue
			}
			props[prop.ID] = database.TextValue(raw)
		}
	}

	if len(st.FormErrors) > 0 {
		return
	}

	body := formDraftVal(st.FormTextDrafts, "_body")
	rec, err := st.Store.CreateRecord(title, props, body)
	if err != nil {
		if opts.OnNotify != nil {
			opts.OnNotify(err.Error())
		}
		return
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

	st.FormSubmittedTitle = rec.Title
	st.FormLastRecordID = rec.ID
	if fcfg.ContinueOnSubmit {
		resetFormInputs(st, &fcfg, db)
	}
}

func renderFormBuilderCard(
	c *ui.Context,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	fcfg database.FormViewConfig,
	fields []database.FormFieldConfig,
	opts Options,
	pal Palette,
) {
	ui.Column(c.Key("form-builder")).Padding(22, 26).Radius(12).Background(pal.Paper).
		Border(1, pal.Border).Shadow(0, 2, 8, 0, pal.ShadowEdge).Gap(14).Children(func() {
		ui.Text(c, i18n.T("编辑表单配置")).FontSize(17).FontWeight(700).TextColor(pal.Text)

		saveMeta := func(mutate func(*database.FormViewConfig)) {
			vc := view.Clone()
			fc := ensureFormConfig(&vc, db)
			mutate(fc)
			saveViewAndNotify(st, vc, opts)
		}

		ui.Column(c).Gap(6).Children(func() {
			ui.Text(c, i18n.T("表单标题")).FontSize(12.5).FontWeight(600).TextColor(pal.Text)
			in := ui.TextInput(c.Key("fb-title"), &st.FormTitleDraft).
				Height(32).Padding(4, 10).Radius(6).Background(pal.Surface).TextColor(pal.Text)
			if in.Changed() || in.Submitted() {
				next := st.FormTitleDraft
				saveMeta(func(fc *database.FormViewConfig) { fc.Title = next })
			}
		})

		ui.Column(c).Gap(6).Children(func() {
			ui.Text(c, i18n.T("表单说明")).FontSize(12.5).FontWeight(600).TextColor(pal.Text)
			in := ui.TextInput(c.Key("fb-desc"), &st.FormDescDraft).
				Placeholder(i18n.T("向填写者展示的说明文字…")).
				Height(32).Padding(4, 10).Radius(6).Background(pal.Surface).TextColor(pal.Text)
			if in.Changed() || in.Submitted() {
				next := st.FormDescDraft
				saveMeta(func(fc *database.FormViewConfig) { fc.Description = next })
			}
		})

		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, i18n.T("提交按钮文案")).FontSize(12.5).FontWeight(600).TextColor(pal.Text)
				in := ui.TextInput(c.Key("fb-subtxt"), &st.FormSubmitTextDraft).
					Height(30).Padding(4, 8).Radius(6).Background(pal.Surface).TextColor(pal.Text)
				if in.Changed() || in.Submitted() {
					next := st.FormSubmitTextDraft
					saveMeta(func(fc *database.FormViewConfig) { fc.SubmitText = next })
				}
			})
			cont := fcfg.ContinueOnSubmit
			ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(18, 0, 0, 0).Children(func() {
				ui.Text(c, i18n.T("提交后继续填写")).FontSize(12.5).TextColor(pal.Text)
				sw := ui.Switch(c.Key("fb-cont"), &cont).Label(i18n.T("提交后继续填写"))
				if sw.Changed() {
					saveMeta(func(fc *database.FormViewConfig) { fc.ContinueOnSubmit = cont })
				}
			})
		})

		ui.Box(c).Height(1).Background(pal.Border)
		ui.Text(c, i18n.T("表单字段（顺序、必填与显隐）")).FontSize(13).FontWeight(600).TextColor(pal.Text)

		for i, f := range fields {
			idx := i
			fld := f
			prop, pIdx := db.PropertyByID(fld.PropertyID)
			if pIdx < 0 {
				continue
			}
			isTitle := prop.Type == database.PropTitle
			ui.Row(c.Key("fbf:"+prop.ID)).Height(36).Padding(0, 10).Radius(7).
				Background(pal.Surface).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, PropertyIcon(prop.Type)).FontSize(13).TextColor(pal.TextMuted)
				ui.Text(c, prop.Name).FontSize(13).FontWeight(500).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)

				// Required checkbox.
				req := fld.Required
				ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
					cb := ui.CheckboxBase(c.Key("fbreq"), &req).Size(15, 15).Radius(4).Center().
						Disabled(isTitle).Label(i18n.T("必填") + ": " + prop.Name)
					if !isTitle {
						cb.Cursor(ui.CursorPointer)
					}
					cb.Draw(func(p *ui.Painter, r ui.Rect) {
						if req {
							p.Fill(r, pal.Accent, 3.5)
							p.Icon(iconCheck, rectInset(r, 2.5), ui.RGB(255, 255, 255))
						} else {
							p.Stroke(rectInset(r, 0.5), pal.BorderStrong, 3.5, 1.2)
						}
					})
					ui.Text(c, i18n.T("必填")).FontSize(11.5).TextColor(pal.TextMuted)
					if cb.Changed() && !isTitle {
						next := slices.Clone(fields)
						next[idx].Required = req
						saveMeta(func(fc *database.FormViewConfig) { fc.Fields = next })
					}
				})

				// Up / Down arrows.
				if idx > 0 {
					up := ui.Box(c.Key("fbup")).Size(24, 24).Radius(5).Center().Cursor(ui.CursorPointer).
						Role(ui.RoleButton).Label(i18n.T("上移") + ": " + prop.Name)
					if up.Hovered() {
						up.Background(pal.RowHover)
					}
					up.Children(func() { ui.Icon(c, iconArrowUp).FontSize(12).TextColor(pal.TextMuted) })
					if up.Clicked() {
						next := slices.Clone(fields)
						next[idx-1], next[idx] = next[idx], next[idx-1]
						saveMeta(func(fc *database.FormViewConfig) { fc.Fields = next })
					}
				}
				if idx+1 < len(fields) {
					dn := ui.Box(c.Key("fbdn")).Size(24, 24).Radius(5).Center().Cursor(ui.CursorPointer).
						Role(ui.RoleButton).Label(i18n.T("下移") + ": " + prop.Name)
					if dn.Hovered() {
						dn.Background(pal.RowHover)
					}
					dn.Children(func() { ui.Icon(c, iconArrowDown).FontSize(12).TextColor(pal.TextMuted) })
					if dn.Clicked() {
						next := slices.Clone(fields)
						next[idx], next[idx+1] = next[idx+1], next[idx]
						saveMeta(func(fc *database.FormViewConfig) { fc.Fields = next })
					}
				}

				// Hidden eye toggle.
				visIcon := iconEye
				if fld.Hidden {
					visIcon = iconEyeOff
				}
				eye := ui.Box(c.Key("fbeye")).Size(24, 24).Radius(5).Center().Cursor(ui.CursorPointer).
					Disabled(isTitle).Role(ui.RoleButton).Label(i18n.T("切换显示") + ": " + prop.Name)
				if eye.Hovered() && !isTitle {
					eye.Background(pal.RowHover)
				}
				eye.Children(func() { ui.Icon(c, visIcon).FontSize(13).TextColor(pal.TextMuted) })
				if eye.Clicked() && !isTitle {
					next := slices.Clone(fields)
					next[idx].Hidden = !next[idx].Hidden
					saveMeta(func(fc *database.FormViewConfig) { fc.Fields = next })
				}
			})
		}
	})
}
