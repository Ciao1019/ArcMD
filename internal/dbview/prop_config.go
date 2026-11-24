package dbview

import (
	"fmt"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

func propertyConfigPopover(
	c *ui.Context,
	anchor ui.Element,
	st *State,
	db *database.Database,
	prop database.Property,
	opts Options,
	pal Palette,
) {
	styledPopover(c, "pop-prop-cfg:"+prop.ID, anchor, &st.EditPropOpen, pal, func() {
		saveProp := func(updated database.Property) {
			affected, err := st.Store.UpdateProperty(updated)
			if err != nil {
				if opts.OnNotify != nil {
					opts.OnNotify(err.Error())
				}
				return
			}
			if opts.OnChanged != nil {
				opts.OnChanged(affected)
			}
		}

		w := float32(316)
		if prop.Type == database.PropFormula {
			w = 372
		}

		ui.Column(c.Key("prop-cfg-box:" + prop.ID)).Width(w).Gap(10).Padding(8).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, PropertyIcon(prop.Type)).FontSize(14).TextColor(pal.Accent)
				ui.Text(c, prop.Name+" · "+propertyTypeLabel(prop.Type)).FontSize(13.5).FontWeight(600).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)
				cls := ui.Box(c.Key("cfg-close")).Size(22, 22).Radius(4).Center().Cursor(ui.CursorPointer).
					Role(ui.RoleButton).Label(i18n.T("关闭"))
				if cls.Hovered() {
					cls.Background(pal.RowHover)
				}
				cls.Children(func() { ui.Icon(c, iconX).FontSize(12).TextColor(pal.TextMuted) })
				if cls.Clicked() {
					st.EditPropOpen = false
				}
			})
			ui.Box(c).Height(1).Background(pal.Border)

			switch prop.Type {
			case database.PropRelation:
				renderRelationConfigSection(c, st, prop, saveProp, opts, pal)
			case database.PropRollup:
				renderRollupConfigSection(c, st, db, prop, saveProp, pal)
			case database.PropFormula:
				renderFormulaConfigSection(c, st, db, prop, saveProp, opts, pal)
				ui.Box(c).Height(1).Background(pal.Border)
				renderNumberFormatSection(c, prop, saveProp, pal)
			case database.PropNumber:
				renderNumberFormatSection(c, prop, saveProp, pal)
			default:
				ui.Text(c, i18n.T("此属性类型暂无额外配置项")).FontSize(12).TextColor(pal.TextMuted)
			}

			if prop.Type == database.PropRollup {
				ui.Box(c).Height(1).Background(pal.Border)
				renderNumberFormatSection(c, prop, saveProp, pal)
			}
		})
	})
}

func renderRelationConfigSection(
	c *ui.Context,
	st *State,
	prop database.Property,
	saveProp func(database.Property),
	opts Options,
	pal Palette,
) {
	cfg := database.RelationConfig{}
	if prop.Relation != nil {
		cfg = *prop.Relation
	}
	var dbs []database.SpaceDBInfo
	if opts.ListSpaceDBs != nil {
		dbs = opts.ListSpaceDBs()
	}

	targetLbl := i18n.T("当前数据库（自关联）")
	if cfg.TargetDB != "" {
		targetLbl = cfg.TargetDB
		for _, info := range dbs {
			if info.RelPath == cfg.TargetDB || info.ID == cfg.TargetDB {
				targetLbl = info.Title
				break
			}
		}
	}

	ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Text(c, i18n.T("目标数据库")).FontSize(12.5).TextColor(pal.Text)
		tb := ui.Button(c.Key("cfg-rel-target"), targetLbl).Height(28).Padding(0, 10)
		tb.PopoverMenuButton(func(m *ui.Menu) {
			if m.Item(i18n.T("当前数据库（自关联）")).Checked(cfg.TargetDB == "").Chosen() {
				next := prop.Clone()
				if next.Relation == nil {
					next.Relation = &database.RelationConfig{}
				}
				next.Relation.TargetDB = ""
				next.Relation.SyncedPropID = ""
				saveProp(next)
			}
			if len(dbs) > 0 {
				m.Separator()
			}
			for _, info := range dbs {
				cand := info
				checked := cfg.TargetDB == cand.RelPath || cfg.TargetDB == cand.ID
				if m.Item(cand.Title).Checked(checked).Chosen() {
					next := prop.Clone()
					if next.Relation == nil {
						next.Relation = &database.RelationConfig{}
					}
					next.Relation.TargetDB = cand.RelPath
					next.Relation.SyncedPropID = ""
					saveProp(next)
				}
			}
		})
	})

	limitOne := cfg.LimitOne
	renderLayoutSwitch(c, "cfg-rel-one", i18n.T("限制为单条关联"), &limitOne, pal, func(v bool) {
		next := prop.Clone()
		if next.Relation == nil {
			next.Relation = &database.RelationConfig{}
		}
		next.Relation.LimitOne = v
		saveProp(next)
	})

	ui.Column(c).Gap(6).Padding(6).Radius(7).Background(pal.Surface).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(c, i18n.T("双向关联")).FontSize(12.5).FontWeight(600).TextColor(pal.Text)
			if cfg.SyncedPropID != "" {
				bg, fg := TagColors(pal.Dark, "green")
				ui.Box(c).Padding(2, 7).Radius(5).Background(bg).Children(func() {
					ui.Text(c, i18n.T("已同步")).FontSize(11).FontWeight(500).TextColor(fg)
				})
			}
		})
		ui.TextInput(c.Key("cfg-rel-back"), &st.EditRelationBackName).
			Placeholder(i18n.T("对端属性名称…")).Label(i18n.T("对端属性名称")).
			Height(30).Padding(4, 8).Radius(6).Background(pal.Paper).Border(1, pal.Border).TextColor(pal.Text)
		if ui.PrimaryButton(c.Key("cfg-rel-sync"), i18n.T("启用双向关联")).Height(28).Padding(0, 12).Clicked() {
			if _, err := st.Store.SetupTwoWayRelation(prop.ID, cfg.TargetDB, st.EditRelationBackName); err != nil {
				if opts.OnNotify != nil {
					opts.OnNotify(err.Error())
				}
			} else if opts.OnChanged != nil {
				opts.OnChanged(nil)
			}
		}
	})
}

func renderRollupConfigSection(
	c *ui.Context,
	st *State,
	db *database.Database,
	prop database.Property,
	saveProp func(database.Property),
	pal Palette,
) {
	rcfg := database.RollupConfig{Calculate: database.AggShowOriginal}
	if prop.Rollup != nil {
		rcfg = *prop.Rollup
		if rcfg.Calculate == "" {
			rcfg.Calculate = database.AggShowOriginal
		}
	}

	var relProps []database.Property
	for _, p := range db.Properties {
		if p.Type == database.PropRelation {
			relProps = append(relProps, p)
		}
	}
	var curRel *database.Property
	relName := i18n.T("选择关联属性")
	for i := range relProps {
		if relProps[i].ID == rcfg.RelationPropID {
			curRel = &relProps[i]
			relName = relProps[i].Name
			break
		}
	}
	if curRel == nil && len(relProps) > 0 {
		curRel = &relProps[0]
		rcfg.RelationPropID = curRel.ID
		relName = curRel.Name
	}

	ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Text(c, i18n.T("关联属性")).FontSize(12.5).TextColor(pal.Text)
		rb := ui.Button(c.Key("cfg-ro-rel"), relName).Height(28).Padding(0, 10)
		rb.PopoverMenuButton(func(m *ui.Menu) {
			if len(relProps) == 0 {
				m.Item(i18n.T("暂无关联属性")).Disabled(true)
				return
			}
			for _, rp := range relProps {
				cand := rp
				if m.Item(cand.Name).Checked(cand.ID == rcfg.RelationPropID).Chosen() {
					next := prop.Clone()
					next.Rollup = &database.RollupConfig{
						RelationPropID: cand.ID,
						TargetPropID:   "",
						Calculate:      rcfg.Calculate,
					}
					saveProp(next)
				}
			}
		})
	})

	var targetProps []database.Property
	if curRel != nil {
		targetDBName := ""
		if curRel.Relation != nil {
			targetDBName = curRel.Relation.TargetDB
		}
		if tDB, _, err := st.Store.ResolveTargetDatabase(targetDBName); err == nil && tDB != nil {
			targetProps = tDB.Properties
		}
	}

	targetName := i18n.T("选择目标属性")
	for _, tp := range targetProps {
		if tp.ID == rcfg.TargetPropID {
			targetName = tp.Name
			break
		}
	}

	ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Text(c, i18n.T("目标属性")).FontSize(12.5).TextColor(pal.Text)
		tb := ui.Button(c.Key("cfg-ro-tgt"), targetName).Height(28).Padding(0, 10)
		tb.PopoverMenuButton(func(m *ui.Menu) {
			if len(targetProps) == 0 {
				m.Item(i18n.T("请先配置关联属性")).Disabled(true)
				return
			}
			for _, tp := range targetProps {
				cand := tp
				if m.Item(cand.Name).Checked(cand.ID == rcfg.TargetPropID).Chosen() {
					next := prop.Clone()
					next.Rollup = &database.RollupConfig{
						RelationPropID: rcfg.RelationPropID,
						TargetPropID:   cand.ID,
						Calculate:      rcfg.Calculate,
					}
					saveProp(next)
				}
			}
		})
	})

	rollupAggs := []database.AggregationType{
		database.AggShowOriginal,
		database.AggShowUnique,
		database.AggCountAll,
		database.AggCountValues,
		database.AggCountUnique,
		database.AggCountNotEmpty,
		database.AggSum,
		database.AggAverage,
		database.AggMedian,
		database.AggMin,
		database.AggMax,
		database.AggChecked,
		database.AggPercentChecked,
		database.AggEarliestDate,
		database.AggLatestDate,
		database.AggDateRange,
	}
	ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Text(c, i18n.T("计算方式")).FontSize(12.5).TextColor(pal.Text)
		ab := ui.Button(c.Key("cfg-ro-agg"), aggregationLabel(rcfg.Calculate)).Height(28).Padding(0, 10)
		ab.PopoverMenuButton(func(m *ui.Menu) {
			for _, agg := range rollupAggs {
				cand := agg
				if m.Item(aggregationLabel(cand)).Checked(cand == rcfg.Calculate).Chosen() {
					next := prop.Clone()
					next.Rollup = &database.RollupConfig{
						RelationPropID: rcfg.RelationPropID,
						TargetPropID:   rcfg.TargetPropID,
						Calculate:      cand,
					}
					saveProp(next)
				}
			}
		})
	})
}

func renderFormulaConfigSection(
	c *ui.Context,
	st *State,
	db *database.Database,
	prop database.Property,
	saveProp func(database.Property),
	opts Options,
	pal Palette,
) {
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, i18n.T("公式表达式")).FontSize(12.5).FontWeight(600).TextColor(pal.Text)
		ui.TextArea(c.Key("cfg-fm-expr"), &st.EditFormulaExpr).
			MinHeight(82).Padding(8, 10).Radius(7).
			Background(pal.Surface).Border(1, pal.Border).TextColor(pal.Text).
			Placeholder(i18n.T(`if(prop("权重") >= 80, "高优", "常规")`)).Label(i18n.T("公式表达式"))

		// Property quick-insert chips.
		ui.Row(c.Key("cfg-fm-chips")).Wrap().Gap(4).Children(func() {
			for _, p := range db.Properties {
				if p.ID == prop.ID {
					continue
				}
				cand := p
				chip := ui.Button(c.Key("fmc:"+cand.ID), cand.Name).Height(22).Padding(0, 7)
				if chip.Clicked() {
					st.EditFormulaExpr += fmt.Sprintf(`prop(%q)`, cand.Name)
				}
			}
			for _, snip := range []struct{ label, insert string }{
				{"if()", i18n.T(`if(条件, "是", "否")`)},
				{"dateBetween()", i18n.T(`dateBetween(prop("日期"), today(), "days")`)},
				{"round()", i18n.T(`round(值, 2)`)},
			} {
				sc := snip
				if ui.Button(c.Key("fms:"+sc.label), sc.label).Height(22).Padding(0, 7).Clicked() {
					st.EditFormulaExpr += sc.insert
				}
			}
		})

		// Live syntax check and record[0] evaluation preview.
		syntaxErr := database.ValidateFormula(st.EditFormulaExpr)
		if syntaxErr != nil {
			ui.Text(c, i18n.T("语法错误")+": "+syntaxErr.Error()).FontSize(11.5).TextColor(pal.Danger).Wrap()
		} else if expr := strings.TrimSpace(st.EditFormulaExpr); expr != "" {
			var sampleRec *database.Record
			if recs := st.Store.Records(); len(recs) > 0 {
				sampleRec = recs[0]
			}
			val := database.EvaluateFormula(st.Store, sampleRec, expr, opts.Now)
			if val.Kind == database.ValError {
				ui.Text(c, i18n.T("预览")+": "+val.Error).FontSize(11.5).TextColor(pal.Danger).SingleLine()
			} else {
				preview := database.FormatValue(prop, val)
				if preview == "" {
					preview = i18n.T("（空）")
				}
				ui.Text(c, i18n.T("预览")+": "+preview).FontSize(12).TextColor(pal.Accent).SingleLine()
			}
		}

		ui.Row(c).Justify(ui.End).Padding(2, 0, 0, 0).Children(func() {
			if ui.PrimaryButton(c.Key("cfg-fm-save"), i18n.T("保存公式")).Height(28).Padding(0, 14).
				Disabled(syntaxErr != nil).Clicked() {
				next := prop.Clone()
				next.Formula = &database.FormulaConfig{Expression: st.EditFormulaExpr}
				saveProp(next)
				st.EditPropOpen = false
			}
		})
	})
}

func renderNumberFormatSection(
	c *ui.Context,
	prop database.Property,
	saveProp func(database.Property),
	pal Palette,
) {
	nc := database.NumberConfig{Format: "number", ShowAs: "number", DivideBy: 100, BarColor: "blue"}
	if prop.Number != nil {
		nc = *prop.Number
		if nc.Format == "" {
			nc.Format = "number"
		}
		if nc.ShowAs == "" {
			nc.ShowAs = "number"
		}
	}

	formats := []struct{ id, label string }{
		{"number", i18n.T("标准数字")},
		{"number_with_commas", i18n.T("千分位")},
		{"percent", i18n.T("百分比")},
		{"yuan", i18n.T("人民币 ¥")},
		{"dollar", i18n.T("美元 $")},
	}
	fmtLabel := formats[0].label
	for _, f := range formats {
		if f.id == nc.Format {
			fmtLabel = f.label
			break
		}
	}
	ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Text(c, i18n.T("数字格式")).FontSize(12.5).TextColor(pal.Text)
		fb := ui.Button(c.Key("cfg-num-fmt"), fmtLabel).Height(26).Padding(0, 8)
		fb.PopoverMenuButton(func(m *ui.Menu) {
			for _, f := range formats {
				cand := f
				if m.Item(cand.label).Checked(nc.Format == cand.id).Chosen() {
					next := prop.Clone()
					cfg := nc
					cfg.Format = cand.id
					next.Number = &cfg
					saveProp(next)
				}
			}
		})
	})

	styles := []struct{ id, label string }{
		{"number", i18n.T("数字")},
		{"bar", i18n.T("进度条")},
		{"ring", i18n.T("圆环")},
	}
	ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Text(c, i18n.T("展示样式")).FontSize(12.5).TextColor(pal.Text)
		ui.Row(c.Key("cfg-num-show")).Gap(3).Children(func() {
			for _, s := range styles {
				cand := s
				btn := ui.Button(c.Key(cand.id), cand.label).Height(24).Padding(0, 8)
				if nc.ShowAs == cand.id {
					btn.Background(pal.AccentSoft).TextColor(pal.Accent)
				}
				if btn.Clicked() && nc.ShowAs != cand.id {
					next := prop.Clone()
					cfg := nc
					cfg.ShowAs = cand.id
					if cfg.DivideBy <= 0 {
						cfg.DivideBy = 100
					}
					if cfg.BarColor == "" {
						cfg.BarColor = "blue"
					}
					cfg.ShowNumber = true
					next.Number = &cfg
					saveProp(next)
				}
			}
		})
	})
}
