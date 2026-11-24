package dbview

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

func colorRulesPopover(
	c *ui.Context,
	anchor ui.Element,
	st *State,
	view database.ViewConfig,
	db *database.Database,
	opts Options,
	pal Palette,
) {
	styledPopover(c, "pop-colors", anchor, &st.ColorRulesOpen, pal, func() {
		saveColors := func(next []database.ColorRule) {
			vc := view.Clone()
			vc.Colors = next
			saveViewAndNotify(st, vc, opts)
		}

		ui.Column(c.Key("colors-box")).Width(392).Gap(8).Padding(8).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, i18n.T("条件着色规则")).FontSize(13.5).FontWeight(600).TextColor(pal.Text)
				if len(view.Colors) > 0 {
					if ui.Button(c.Key("clr-clear"), i18n.T("清空")).Height(24).Padding(0, 8).Clicked() {
						saveColors(nil)
					}
				}
			})

			if len(view.Colors) == 0 {
				ui.Text(c, i18n.T("按条件自动高亮行或卡片背景颜色。")).FontSize(12).TextColor(pal.TextMuted).Padding(4, 2)
			} else {
				ui.Scroll(c.Key("colors-scroll")).MaxHeight(280).Children(func() {
					ui.Column(c).Gap(6).Children(func() {
						for i, rule := range view.Colors {
							idx := i
							cur := rule
							var r database.FilterRule
							if len(cur.Filter.Rules) > 0 {
								r = cur.Filter.Rules[0]
							} else if len(db.Properties) > 0 {
								r = database.FilterRule{
									ID:         "r_" + database.NewID()[:4],
									PropertyID: db.Properties[0].ID,
									Op:         defaultFilterOpFor(db.Properties[0].Type),
								}
							}
							prop := db.Properties[0]
							if p, pIdx := db.PropertyByID(r.PropertyID); pIdx >= 0 {
								prop = *p
							}

							ui.Column(c.Key("cr:"+cur.ID)).Padding(6, 8).Radius(7).Background(pal.Surface).
								Border(1, pal.Border).Gap(6).Children(func() {
								ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
									// Color swatch dropdown.
									cBtn := ui.ButtonBase(c.Key("col")).Height(26).Padding(0, 8).Radius(5).
										Background(pal.Paper).Border(1, pal.Border).Gap(5).AlignItems(ui.Center).
										Cursor(ui.CursorPointer).Role(ui.RoleMenuButton).Label(i18n.T("高亮颜色") + ": " + colorLabel(cur.Color))
									cBtn.Children(func() {
										ui.Box(c).Size(9, 9).Radius(4.5).Background(StatusDotColor(pal.Dark, cur.Color)).Shrink(0)
										ui.Text(c, colorLabel(cur.Color)).FontSize(11.5).TextColor(pal.Text).SingleLine()
									})
									cBtn.PopoverMenuButton(func(m *ui.Menu) {
										for _, cn := range database.OptionColors {
											if cn == "default" {
												continue
											}
											cand := cn
											if m.Item(colorLabel(cand)).Checked(cur.Color == cand).Chosen() {
												next := slices.Clone(view.Colors)
												next[idx] = cur.Clone()
												next[idx].Color = cand
												saveColors(next)
											}
										}
									})

									// Property picker.
									pBtn := ui.Button(c.Key("prop"), prop.Name).Height(26).Padding(0, 8)
									pBtn.PopoverMenuButton(func(m *ui.Menu) {
										for _, p := range db.Properties {
											cand := p
											if m.Item(cand.Name).Checked(cand.ID == prop.ID).Chosen() {
												next := slices.Clone(view.Colors)
												nr := next[idx].Clone()
												nr.Filter.Conjunction = "and"
												nr.Filter.Rules = []database.FilterRule{{
													ID:         r.ID,
													PropertyID: cand.ID,
													Op:         defaultFilterOpFor(cand.Type),
												}}
												next[idx] = nr
												saveColors(next)
											}
										}
									})

									// Operator picker.
									ops := filterOpsForType(prop.Type)
									if prop.Type == database.PropFormula || prop.Type == database.PropRollup {
										ops = []database.FilterOp{
											database.OpEquals, database.OpNotEquals,
											database.OpContains, database.OpNotContains,
											database.OpGT, database.OpGTE, database.OpLT, database.OpLTE,
											database.OpIsEmpty, database.OpIsNotEmpty,
										}
									}
									opBtn := ui.Button(c.Key("op"), filterOpLabel(r.Op)).Height(26).Padding(0, 8)
									opBtn.PopoverMenuButton(func(m *ui.Menu) {
										for _, cand := range ops {
											co := cand
											if m.Item(filterOpLabel(co)).Checked(r.Op == co).Chosen() {
												next := slices.Clone(view.Colors)
												nr := next[idx].Clone()
												nr.Filter.Conjunction = "and"
												nr.Filter.Rules = []database.FilterRule{{
													ID:         r.ID,
													PropertyID: prop.ID,
													Op:         co,
													Value:      r.Value.Clone(),
												}}
												next[idx] = nr
												saveColors(next)
											}
										}
									})

									ui.Spacer(c)

									if idx > 0 {
										up := ui.Box(c.Key("up")).Size(22, 22).Radius(4).Center().Cursor(ui.CursorPointer).
											Role(ui.RoleButton).Label(i18n.T("上移"))
										if up.Hovered() {
											up.Background(pal.RowHover)
										}
										up.Children(func() { ui.Icon(c, iconArrowUp).FontSize(11).TextColor(pal.TextMuted) })
										if up.Clicked() {
											next := slices.Clone(view.Colors)
											next[idx-1], next[idx] = next[idx], next[idx-1]
											saveColors(next)
										}
									}

									del := ui.Box(c.Key("del")).Size(22, 22).Radius(4).Center().Cursor(ui.CursorPointer).
										Role(ui.RoleButton).Label(i18n.T("移除"))
									if del.Hovered() {
										del.Background(pal.RowHover)
									}
									del.Children(func() { ui.Icon(c, iconX).FontSize(12).TextColor(pal.TextMuted) })
									if del.Clicked() {
										next := slices.Delete(slices.Clone(view.Colors), idx, idx+1)
										saveColors(next)
									}
								})

								// Condition value input when op takes an argument.
								if r.Op != database.OpIsEmpty && r.Op != database.OpIsNotEmpty &&
									r.Op != database.OpChecked && r.Op != database.OpUnchecked {
									slot := formDraftPtr(st.ColorRuleDrafts, cur.ID)
									if *slot == "" && !r.Value.IsEmpty() {
										*slot = r.Value.PlainString()
									}
									in := ui.TextInput(c.Key("val"), slot).
										Placeholder(i18n.T("条件匹配值…")).Label(fmt.Sprintf("%s: %s", i18n.T("条件匹配值"), cur.ID)).
										Height(28).Padding(2, 8).Radius(6).Background(pal.Paper).Border(1, pal.Border).TextColor(pal.Text)
									if in.Changed() || in.Submitted() {
										raw := strings.TrimSpace(*slot)
										val := database.TextValue(raw)
										if n, perr := strconv.ParseFloat(raw, 64); perr == nil && raw != "" &&
											(prop.Type == database.PropNumber || prop.Type == database.PropFormula || prop.Type == database.PropRollup) {
											val = database.NumberValue(n)
										}
										next := slices.Clone(view.Colors)
										nr := next[idx].Clone()
										nr.Filter.Conjunction = "and"
										nr.Filter.Rules = []database.FilterRule{{
											ID:         r.ID,
											PropertyID: prop.ID,
											Op:         r.Op,
											Value:      val,
										}}
										next[idx] = nr
										saveColors(next)
									}
								}
							})
						}
					})
				})
			}

			if !opts.ReadOnly && len(db.Properties) > 0 {
				if ui.Button(c.Key("cr-add"), "+ "+i18n.T("添加着色规则")).Height(28).Padding(0, 10).Clicked() {
					p := db.Properties[0]
					defVal := database.EmptyValue()
					for _, cand := range db.Properties {
						if cand.Type == database.PropStatus || cand.Type == database.PropSelect {
							p = cand
							if len(cand.Options) > 0 {
								defVal = database.TextValue(cand.Options[0].Name)
							}
							break
						}
					}
					nextCol := database.OptionColors[1+(len(view.Colors)%(len(database.OptionColors)-1))]
					next := append(slices.Clone(view.Colors), database.ColorRule{
						ID:    "cr_" + database.NewID()[:6],
						Color: nextCol,
						Filter: database.FilterGroup{
							Conjunction: "and",
							Rules: []database.FilterRule{{
								ID:         "r_" + database.NewID()[:6],
								PropertyID: p.ID,
								Op:         defaultFilterOpFor(p.Type),
								Value:      defVal,
							}},
						},
					})
					saveColors(next)
				}
			}
		})
	})
}
