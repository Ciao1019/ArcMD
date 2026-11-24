package dbview

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

func templatesPopover(
	c *ui.Context,
	anchor ui.Element,
	st *State,
	db *database.Database,
	view database.ViewConfig,
	opts Options,
	pal Palette,
) {
	styledPopover(c, "pop-templates", anchor, &st.TemplatesOpen, pal, func() {
		openEditor := func(tpl database.Template) {
			st.EditingTemplateID = tpl.ID
			st.TplNameDraft = tpl.Name
			st.TplIconDraft = tpl.Icon
			st.TplBodyDraft = tpl.Body
			st.TplStatusDraft = ""
			for _, p := range db.Properties {
				if p.Type == database.PropStatus {
					if v, ok := tpl.Props[p.ID]; ok {
						st.TplStatusDraft = v.Text
					}
					break
				}
			}
		}

		ui.Column(c.Key("tpl-box")).Width(320).Gap(8).Padding(8).Children(func() {
			if st.EditingTemplateID != "" {
				var curTpl *database.Template
				for i := range db.Templates {
					if db.Templates[i].ID == st.EditingTemplateID {
						curTpl = &db.Templates[i]
						break
					}
				}
				if curTpl == nil {
					st.EditingTemplateID = ""
					return
				}

				ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
					if ui.Button(c.Key("tpl-back"), i18n.T("返回")).Height(24).Padding(0, 8).Clicked() {
						st.EditingTemplateID = ""
						return
					}
					ui.Text(c, i18n.T("编辑记录模板")).FontSize(13).FontWeight(600).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)
				})

				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.TextInput(c.Key("tpl-icon"), &st.TplIconDraft).
						Placeholder("🚀").Label(i18n.T("图标")).
						Width(52).Height(30).Padding(4, 8).Radius(6).Background(pal.Surface).Border(1, pal.Border).TextColor(pal.Text)
					ui.TextInput(c.Key("tpl-name"), &st.TplNameDraft).
						Placeholder(i18n.T("模板名称（支持 {{date}}）")).Label(i18n.T("模板名称")).
						Grow(1).MinWidth(0).Height(30).Padding(4, 8).Radius(6).Background(pal.Surface).Border(1, pal.Border).TextColor(pal.Text)
				})

				for _, p := range db.Properties {
					if p.Type == database.PropStatus && len(p.Options) > 0 {
						statusProp := p
						curStatus := st.TplStatusDraft
						lbl := i18n.T("不设置")
						if curStatus != "" {
							lbl = curStatus
						}
						ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
							ui.Text(c, i18n.T("默认")+statusProp.Name).FontSize(12).TextColor(pal.TextMuted)
							sb := ui.Button(c.Key("tpl-st"), lbl).Height(26).Padding(0, 8)
							sb.PopoverMenuButton(func(m *ui.Menu) {
								if m.Item(i18n.T("不设置")).Checked(st.TplStatusDraft == "").Chosen() {
									st.TplStatusDraft = ""
								}
								m.Separator()
								for _, opt := range statusProp.Options {
									cand := opt
									if m.Item(cand.Name).Checked(st.TplStatusDraft == cand.Name).Chosen() {
										st.TplStatusDraft = cand.Name
									}
								}
							})
						})
						break
					}
				}

				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, i18n.T("模板正文")).FontSize(12).FontWeight(600).TextColor(pal.TextMuted)
					ui.TextArea(c.Key("tpl-body"), &st.TplBodyDraft).
						MinHeight(92).Padding(8, 10).Radius(6).
						Background(pal.Surface).Border(1, pal.Border).TextColor(pal.Text).
						Placeholder("## " + i18n.T("目标") + "\n{{title}} - {{date}}").Label(i18n.T("模板正文"))
					ui.Text(c, "{{date}} · {{time}} · {{weekday}} · {{title}}").FontSize(11).TextColor(pal.TextMuted)
				})

				ui.Row(c).Justify(ui.End).Gap(6).Children(func() {
					if ui.PrimaryButton(c.Key("tpl-save"), i18n.T("保存模板")).Height(28).Padding(0, 14).Clicked() {
						next := curTpl.Clone()
						next.Name = strings.TrimSpace(st.TplNameDraft)
						next.Icon = strings.TrimSpace(st.TplIconDraft)
						next.Body = st.TplBodyDraft
						if next.Props == nil {
							next.Props = make(map[string]database.Value)
						}
						for _, p := range db.Properties {
							if p.Type == database.PropStatus {
								if st.TplStatusDraft == "" {
									delete(next.Props, p.ID)
								} else {
									next.Props[p.ID] = database.TextValue(st.TplStatusDraft)
								}
								break
							}
						}
						if err := st.Store.UpdateTemplate(next); err != nil {
							if opts.OnNotify != nil {
								opts.OnNotify(err.Error())
							}
						} else if opts.OnChanged != nil {
							opts.OnChanged(nil)
						}
						st.EditingTemplateID = ""
					}
				})
				return
			}

			ui.Text(c, i18n.T("记录模板")).FontSize(13.5).FontWeight(600).TextColor(pal.Text)
			if len(db.Templates) == 0 {
				ui.Text(c, i18n.T("暂无模板，可新建带有默认属性与正文的模板。")).FontSize(12).TextColor(pal.TextMuted).Wrap()
			} else {
				ui.Scroll(c.Key("tpl-scroll")).MaxHeight(240).Children(func() {
					ui.Column(c).Gap(3).Children(func() {
						for i, tpl := range db.Templates {
							cur := tpl
							row := ui.Row(c.Key(fmt.Sprintf("tpl:%d:%s", i, cur.ID))).Height(32).Padding(0, 8).Radius(6).
								Gap(6).AlignItems(ui.Center).Cursor(ui.CursorPointer)
							if row.Hovered() {
								row.Background(pal.RowHover)
							}
							var consumedAct bool
							row.Children(func() {
								if cur.Icon != "" {
									ui.Text(c, cur.Icon).FontSize(13).NoWrap()
								} else {
									ui.Icon(c, iconTemplate).FontSize(13).TextColor(pal.TextMuted)
								}
								ui.Text(c, cur.Name).FontSize(12.5).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)

								if !opts.ReadOnly {
									ed := ui.Box(c.Key("edit")).Size(22, 22).Radius(4).Center().Cursor(ui.CursorPointer).
										Role(ui.RoleButton).Label(i18n.T("编辑模板") + ": " + cur.Name)
									if ed.Hovered() {
										ed.Background(pal.Surface)
									}
									ed.Children(func() { ui.Icon(c, iconSettings).FontSize(12).TextColor(pal.TextMuted) })
									if ed.Clicked() {
										consumedAct = true
										openEditor(cur)
									}

									del := ui.Box(c.Key("del")).Size(22, 22).Radius(4).Center().Cursor(ui.CursorPointer).
										Role(ui.RoleButton).Label(i18n.T("删除模板") + ": " + cur.Name)
									if del.Hovered() {
										del.Background(pal.Surface)
									}
									del.Children(func() { ui.Icon(c, iconX).FontSize(12).TextColor(pal.TextMuted) })
									if del.Clicked() {
										consumedAct = true
										if err := st.Store.DeleteTemplate(cur.ID); err == nil && opts.OnChanged != nil {
											opts.OnChanged(nil)
										}
									}
								}
							})
							if row.Clicked() && !consumedAct && !opts.ReadOnly {
								rec, err := st.Store.CreateRecordFromTemplate(cur.ID, "", nil, opts.Now)
								if err != nil {
									if opts.OnNotify != nil {
										opts.OnNotify(err.Error())
									}
								} else {
									st.Motion.TrackCreated(rec.ID)
									st.TemplatesOpen = false
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
									if opts.OnOpenRecord != nil {
										opts.OnOpenRecord(rec, resolveCardOpenMode(view))
									}
								}
							}
						}
					})
				})
			}

			if !opts.ReadOnly {
				if ui.PrimaryButton(c.Key("tpl-new"), "+ "+i18n.T("新建模板")).Height(28).Padding(0, 12).Clicked() {
					nt := database.Template{
						ID:   "tpl_" + database.NewID()[:6],
						Name: i18n.T("新记录模板") + " {{date}}",
						Body: "## " + i18n.T("记录内容") + "\n",
					}
					if err := st.Store.AddTemplate(nt); err == nil {
						if opts.OnChanged != nil {
							opts.OnChanged(nil)
						}
						openEditor(nt)
					}
				}
			}
		})
	})
	_ = slices.Clip([]string{})
}
