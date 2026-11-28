package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/prefs"
	"github.com/egoist/mygo/ui"
)

const minToolbarCardWidth = 520

// customToolbarModal holds the UI state for the customizable document toolbar modal.
type customToolbarModal struct {
	open bool
}

var toolbarCommonPreset = []string{
	"table", "code-block", "link", "image", "math", "callout",
}

type toolbarItemDef struct {
	id       string
	label    string
	tip      string
	shortcut string
	icon     *ui.SVG
	group    string
}

var toolbarGroups = []string{
	"插入",
	"列表与结构",
	"文字与视图",
}

var toolbarItemDefs = []toolbarItemDef{
	// 插入
	{id: "table", label: "表格", tip: "插入表格", icon: iconTable, group: "插入"},
	{id: "code-block", label: "代码块", tip: "插入代码块", icon: iconCodeBlock, group: "插入"},
	{id: "link", label: "链接", tip: "插入链接…", icon: iconLink, group: "插入"},
	{id: "image", label: "图片", tip: "插入图片…", icon: iconImage, group: "插入"},
	{id: "math", label: "公式块", tip: "插入公式", icon: iconSigma, group: "插入"},
	{id: "inline-math", label: "行内公式", tip: "插入行内公式", icon: iconSquareSigma, group: "插入"},
	{id: "callout", label: "高亮块", tip: "插入高亮块", icon: iconLightbulb, group: "插入"},
	{id: "rule", label: "分隔线", tip: "插入分隔线", icon: iconMinus, group: "插入"},
	{id: "columns", label: "分栏", tip: "插入双栏", icon: iconColumns2, group: "插入"},
	{id: "mermaid", label: "流程图", tip: "插入流程图", icon: iconWorkflow, group: "插入"},
	{id: "whiteboard", label: "手绘白板", tip: "插入手绘白板", icon: iconWhiteboard, group: "插入"},
	// 列表与结构
	{id: "task", label: "任务列表", tip: "任务列表", icon: iconListTodo, group: "列表与结构"},
	{id: "bullet", label: "无序列表", tip: "无序列表", icon: iconListBullet, group: "列表与结构"},
	{id: "numbered", label: "有序列表", tip: "有序列表", icon: iconListOrdered, group: "列表与结构"},
	{id: "quote", label: "引用", tip: "引用", icon: iconTextQuote, group: "列表与结构"},
	{id: "toggle", label: "折叠块", tip: "插入折叠块", icon: iconSquareChevron, group: "列表与结构"},
	// 文字与视图
	{id: "bold", label: "粗体", tip: "加粗", shortcut: "bold", icon: iconBold, group: "文字与视图"},
	{id: "italic", label: "斜体", tip: "斜体", shortcut: "italic", icon: iconItalic, group: "文字与视图"},
	{id: "underline", label: "下划线", tip: "下划线", shortcut: "underline", icon: iconUnderline, group: "文字与视图"},
	{id: "strike", label: "删除线", tip: "删除线", shortcut: "strike", icon: iconStrikethrough, group: "文字与视图"},
	{id: "inline-code", label: "行内代码", tip: "行内代码", shortcut: "code", icon: iconCode, group: "文字与视图"},
	{id: "annotate", label: "手绘标注", tip: "手绘标注", icon: iconHighlighter, group: "文字与视图"},
	{id: "source", label: "源码模式", tip: "Markdown 源码模式", shortcut: "source", icon: iconFileCode, group: "文字与视图"},
}

var toolbarItemByID = func() map[string]toolbarItemDef {
	m := make(map[string]toolbarItemDef, len(toolbarItemDefs))
	for _, d := range toolbarItemDefs {
		m[d.id] = d
	}
	return m
}()

func (a *App) openCustomToolbar() {
	a.palette.open = false
	a.settingsOpen = false
	a.toolbarModal.open = true
}

func (a *App) activeToolbarItems() []string {
	if a.prefs == nil || len(a.prefs.ToolbarItems) == 0 {
		return nil
	}
	out := make([]string, 0, len(a.prefs.ToolbarItems))
	seen := make(map[string]bool, len(a.prefs.ToolbarItems))
	for _, id := range a.prefs.ToolbarItems {
		if !prefs.IsValidToolbarItem(id) || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (a *App) hasToolbarItem(id string) bool {
	if a.prefs == nil {
		return false
	}
	return slices.Contains(a.prefs.ToolbarItems, id)
}

func (a *App) setToolbarItems(items []string) {
	if a.prefs == nil {
		return
	}
	var cleaned []string
	seen := make(map[string]bool, len(items))
	for _, id := range items {
		if !prefs.IsValidToolbarItem(id) || seen[id] {
			continue
		}
		seen[id] = true
		cleaned = append(cleaned, id)
	}
	a.prefs.ToolbarItems = cleaned
	a.savePrefs()
}

func (a *App) toggleToolbarItem(id string) {
	if !prefs.IsValidToolbarItem(id) || a.prefs == nil {
		return
	}
	cur := a.activeToolbarItems()
	if idx := slices.Index(cur, id); idx >= 0 {
		cur = slices.Delete(cur, idx, idx+1)
	} else {
		cur = append(cur, id)
	}
	a.setToolbarItems(cur)
}

func (a *App) moveToolbarItem(index, delta int) {
	cur := a.activeToolbarItems()
	next := index + delta
	if index < 0 || index >= len(cur) || next < 0 || next >= len(cur) {
		return
	}
	cur[index], cur[next] = cur[next], cur[index]
	a.setToolbarItems(cur)
}

func (a *App) removeToolbarItem(id string) {
	cur := a.activeToolbarItems()
	if idx := slices.Index(cur, id); idx >= 0 {
		cur = slices.Delete(cur, idx, idx+1)
		a.setToolbarItems(cur)
	}
}

func (a *App) applyToolbarPreset(preset []string) {
	a.setToolbarItems(slices.Clone(preset))
}

func (a *App) clearToolbarItems() {
	a.setToolbarItems(nil)
}

func (a *App) toolbarItemTooltip(def toolbarItemDef) string {
	tip := i18n.T(def.tip)
	if def.shortcut != "" {
		return a.withKey(tip, def.shortcut)
	}
	return tip
}

func (a *App) toolbarItemActive(d *document, id string) bool {
	if d == nil {
		return false
	}
	if id == "source" {
		return d.source
	}
	if d.source || d.ed == nil || d.ed.ReadOnly {
		return false
	}
	style := d.ed.Engine.CurrentStyle()
	switch id {
	case "bold":
		return style.Bold
	case "italic":
		return style.Italic
	case "underline":
		return style.Underline
	case "strike":
		return style.Strike
	case "inline-code":
		return style.Code
	case "annotate":
		return style.Annotation != ""
	}
	leaf := d.ed.Engine.LeafAt(d.ed.Caret)
	if leaf.Block == nil {
		return false
	}
	switch id {
	case "task":
		return leaf.Block.Kind == "task"
	case "bullet":
		return leaf.Block.Kind == "bullet"
	case "numbered":
		return leaf.Block.Kind == "numbered"
	case "quote":
		return leaf.Block.Kind == "quote"
	case "callout":
		return leaf.Block.Kind == "callout"
	case "toggle":
		return leaf.Block.Kind == "toggle"
	case "math":
		return leaf.Block.Kind == "math"
	case "code-block":
		return leaf.Block.Kind == "code" && !strings.EqualFold(strings.TrimSpace(leaf.Block.Language), "mermaid")
	case "mermaid":
		return leaf.Block.Kind == "code" && strings.EqualFold(strings.TrimSpace(leaf.Block.Language), "mermaid")
	}
	return false
}

func (a *App) runToolbarItem(d *document, id string) {
	if d == nil || d.loadErr != nil || d.isPDF() || d.isBook() || d.isMDZ() || d.isDatabase() || d.isWhiteboard() {
		return
	}
	run := func(fn func()) {
		switch {
		case a.activateDocument(d):
			fn()
		case a.dbPeek.open && a.dbPeek.doc == d:
			a.withDocument(d, fn)
		case a.inWindow(d):
			a.withDocument(d, fn)
		default:
			a.withDocument(d, fn)
		}
	}
	if id == "source" {
		run(a.toggleSource)
		return
	}
	if d.source || d.ed == nil || d.ed.ReadOnly {
		return
	}
	ed := d.ed
	run(func() {
		switch id {
		case "table":
			ed.InsertTableBlock(3, 3)
		case "code-block":
			ed.InsertCodeBlock()
		case "link":
			ed.OpenLinkPopover()
		case "image":
			a.pickImages()
		case "math":
			ed.InsertMathBlock()
		case "inline-math":
			ed.InsertInlineMath()
			ed.RequestFocus = true
		case "callout":
			ed.InsertCalloutBlock()
		case "task":
			ed.SetBlockKind("task", 0)
		case "bullet":
			ed.SetBlockKind("bullet", 0)
		case "numbered":
			ed.SetBlockKind("numbered", 0)
		case "quote":
			ed.SetBlockKind("quote", 0)
		case "toggle":
			ed.InsertToggleBlock()
		case "rule":
			ed.InsertRuleBlock()
		case "columns":
			ed.InsertColumnsBlock()
		case "mermaid":
			ed.InsertMermaidFlowchart()
			ed.RequestFocus = true
		case "whiteboard":
			ed.InsertWhiteboard()
			ed.RequestFocus = true
		case "bold":
			ed.Wrap("**")
			ed.RequestFocus = true
		case "italic":
			ed.Wrap("*")
			ed.RequestFocus = true
		case "underline":
			ed.Wrap("_")
			ed.RequestFocus = true
		case "strike":
			ed.Wrap("~~")
			ed.RequestFocus = true
		case "inline-code":
			ed.Wrap("`")
			ed.RequestFocus = true
		case "annotate":
			ed.ToggleQuickAnnotation()
		}
	})
}

func (a *App) customToolbarContextMenu(m *ui.Menu) {
	if m.Item(i18n.T("自定义工具栏…")).Chosen() {
		a.openCustomToolbar()
	}
}

func (a *App) customToolbarCorner(c *ui.Context, d *document, cardWidth float32) {
	if cardWidth > 0 && cardWidth < minToolbarCardWidth {
		return
	}
	items := a.activeToolbarItems()
	if len(items) == 0 {
		return
	}
	maxInline := len(items)
	if cardWidth > 0 {
		avail := cardWidth - 310
		fit := max(1, int(avail/31))
		if fit < len(items) {
			maxInline = max(1, fit-1)
		}
	}
	t := c.Theme()
	row := ui.Row(c.Key("custom-toolbar-strip")).Gap(2).AlignItems(ui.Center)
	row.ContextMenu(a.customToolbarContextMenu)
	row.Children(func() {
		for _, id := range items[:maxInline] {
			def, ok := toolbarItemByID[id]
			if !ok {
				continue
			}
			disabled := id != "source" && (d.source || d.ed == nil || d.ed.ReadOnly)
			tip := a.toolbarItemTooltip(def)
			if id == "source" && d.source {
				tip = a.withKey(i18n.T("返回富文本编辑"), "source")
			} else if disabled && d.ed != nil && d.ed.ReadOnly {
				tip = i18n.T("只读时不能修改格式")
			}
			btn := iconButton(c, "quick-"+id, def.icon, i18n.T(def.label), tip, disabled)
			btn.ContextMenu(a.customToolbarContextMenu)
			if !disabled && a.toolbarItemActive(d, id) {
				if !a.inWindow(d) && a.split == nil {
					btn.Background(glass(t))
				} else {
					btn.Background(t.Accent.Alpha(0.14))
				}
			}
			if btn.Clicked() {
				a.runToolbarItem(d, id)
			}
		}
		if maxInline < len(items) {
			rest := items[maxInline:]
			more := iconButton(c, "quick-overflow", iconChevronsRight, i18n.T("更多快捷工具"), i18n.T("更多快捷工具"), false).Role(ui.RoleMenuButton)
			more.ContextMenu(a.customToolbarContextMenu)
			more.PopoverMenuButton(func(m *ui.Menu) {
				for _, id := range rest {
					def, ok := toolbarItemByID[id]
					if !ok {
						continue
					}
					disabled := id != "source" && (d.source || d.ed == nil || d.ed.ReadOnly)
					label := i18n.T(def.label)
					if id == "source" && d.source {
						label = i18n.T("返回富文本编辑")
					}
					item := m.Item(label).Disabled(disabled).Checked(!disabled && a.toolbarItemActive(d, id))
					if def.shortcut != "" {
						item = a.menuKey(item, def.shortcut)
					}
					if item.Chosen() {
						a.runToolbarItem(d, id)
					}
				}
				m.Separator()
				if m.Item(i18n.T("自定义工具栏…")).Chosen() {
					a.openCustomToolbar()
				}
			})
		}
		if !d.source {
			ui.Box(c).Size(1, 14).Margin(0, 3).Background(separator(t))
		}
	})
}

func (a *App) customToolbarView(c *ui.Context) {
	if !a.toolbarModal.open {
		return
	}
	t := c.Theme()
	winW, winH := c.Size()
	modalW := min(float32(576), winW-t.Space(20))
	modalH := min(float32(592), winH-96)

	ui.Modal(c.Key("custom-toolbar"), &a.toolbarModal.open, func() {
		items := a.activeToolbarItems()
		ui.Column(c.Key("custom-toolbar-body")).Size(modalW, modalH).Gap(12).Children(func() {
			// Header
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, i18n.T("自定义工具栏")).FontSize(17).FontWeight(600)
					ui.Text(c, i18n.T("将常用操作放在文档右上角，修改实时生效。")).FontSize(12).TextColor(t.TextMuted)
				})
				if ui.Button(c.Key("toolbar-preset-common"), i18n.T("常用写作工具")).Clicked() {
					a.applyToolbarPreset(toolbarCommonPreset)
				}
				if ui.Button(c.Key("toolbar-clear"), i18n.T("清空")).Disabled(len(items) == 0).Clicked() {
					a.clearToolbarItems()
				}
			})

			// Active toolbar strip with reorder & remove controls
			ui.Column(c.Key("toolbar-active-strip")).Radius(10).Background(t.Text.Alpha(0.04)).Border(1, t.Text.Alpha(0.07)).Padding(10, 12).Gap(8).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Children(func() {
					ui.Text(c, i18n.T("当前工具栏顺序")).FontSize(12).FontWeight(500).TextColor(t.TextMuted).Grow(1)
					ui.Text(c, fmt.Sprintf(i18n.T("已启用 %d 项"), len(items))).FontSize(11.5).TextColor(t.TextMuted)
				})
				if len(items) == 0 {
					ui.Row(c).Height(32).Center().Children(func() {
						ui.Text(c, i18n.T("暂未启用快捷按钮 · 点击下方项目或「常用写作工具」添加")).FontSize(12.5).TextColor(t.TextMuted)
					})
					return
				}
				const cols = 3
				ui.Scroll(c.Key("toolbar-active-scroll")).MaxHeight(108).MinHeight(0).Gap(6).Children(func() {
					for rowStart := 0; rowStart < len(items); rowStart += cols {
						rowEnd := min(rowStart+cols, len(items))
						ui.Row(c.Key(fmt.Sprintf("active-row-%d", rowStart))).Gap(6).AlignItems(ui.Center).Children(func() {
							for idx := rowStart; idx < rowEnd; idx++ {
								id := items[idx]
								def, ok := toolbarItemByID[id]
								if !ok {
									continue
								}
								pos := idx
								label := i18n.T(def.label)
								ui.Row(c.Key("active-chip:"+id)).Basis(0).Grow(1).MinWidth(0).Height(30).Padding(0, 6, 0, 8).Gap(4).
									Radius(7).Background(t.Text.Alpha(0.05)).Border(1, t.Text.Alpha(0.08)).AlignItems(ui.Center).Children(func() {
									ui.Icon(c, def.icon).FontSize(14).TextColor(t.Accent)
									ui.Text(c, label).FontSize(12.5).FontWeight(500).SingleLine().Grow(1).MinWidth(0)
									toolbarChipStepBtn(c, "move-left:"+id, "←", i18n.T("左移")+" "+label, pos == 0, func() {
										a.moveToolbarItem(pos, -1)
									})
									toolbarChipStepBtn(c, "move-right:"+id, "→", i18n.T("右移")+" "+label, pos == len(items)-1, func() {
										a.moveToolbarItem(pos, 1)
									})
									toolbarChipStepBtn(c, "remove:"+id, "×", i18n.T("移除")+" "+label, false, func() {
										a.removeToolbarItem(id)
									})
								})
							}
							for pad := rowEnd - rowStart; pad < cols; pad++ {
								ui.Box(c.Key(fmt.Sprintf("active-pad-%d-%d", rowStart, pad))).Basis(0).Grow(1)
							}
						})
					}
				})
			})

			// Grouped grid of all 23 actions
			ui.Scroll(c.Key("toolbar-catalog")).Grow(1).MinHeight(0).Gap(12).Children(func() {
				for _, g := range toolbarGroups {
					var groupDefs []toolbarItemDef
					for _, def := range toolbarItemDefs {
						if def.group == g {
							groupDefs = append(groupDefs, def)
						}
					}
					ui.Column(c.Key("group:" + g)).Gap(6).Children(func() {
						ui.Text(c, i18n.T(g)).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
						const gridCols = 4
						for r := 0; r < len(groupDefs); r += gridCols {
							end := min(r+gridCols, len(groupDefs))
							ui.Row(c.Key(fmt.Sprintf("row-%s-%d", g, r))).Gap(8).AlignItems(ui.Center).Children(func() {
								for _, def := range groupDefs[r:end] {
									itemDef := def
									on := slices.Contains(items, itemDef.id)
									label := i18n.T(itemDef.label)
									tile := ui.Row(c.Key("toggle:" + itemDef.id)).Basis(0).Grow(1).MinWidth(0).Height(34).PaddingX(9).Gap(7).
										Radius(8).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().
										Role(ui.RoleCheckBox).Label(label).Checked(on).Tooltip(a.toolbarItemTooltip(itemDef))
									if on {
										tile.Background(t.Accent.Alpha(0.14)).Border(1, t.Accent.Alpha(0.45))
									} else {
										tile.Background(t.Text.Alpha(0.035)).Border(1, t.Text.Alpha(0.06))
										if tile.Hovered() {
											tile.Background(hoverFill(t))
										}
									}
									tile.Children(func() {
										iconCol := t.Text.Alpha(0.72)
										if on {
											iconCol = t.Accent
										}
										ui.Icon(c, itemDef.icon).FontSize(15).TextColor(iconCol)
										txt := ui.Text(c, label).FontSize(12.5).SingleLine().Grow(1).MinWidth(0)
										if on {
											txt.FontWeight(500)
										}
										if on {
											ui.Icon(c, iconCheck).FontSize(13).TextColor(t.Accent)
										}
									})
									if tile.Clicked() {
										a.toggleToolbarItem(itemDef.id)
									}
								}
								for pad := end - r; pad < gridCols; pad++ {
									ui.Box(c.Key(fmt.Sprintf("pad-%s-%d-%d", g, r, pad))).Basis(0).Grow(1)
								}
							})
						}
					})
				}
			})

			// Footer
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Spacer(c)
				if ui.PrimaryButton(c.Key("toolbar-done"), i18n.T("完成")).Clicked() {
					a.toolbarModal.open = false
				}
			})
		})
	})
}

func toolbarChipStepBtn(c *ui.Context, key, symbol, label string, disabled bool, onClick func()) {
	t := c.Theme()
	b := ui.Box(c.Key(key)).Size(20, 20).Radius(5).Center().Focusable().Role(ui.RoleButton).
		Label(label).Tooltip(label).Disabled(disabled)
	col := t.Text.Alpha(0.68)
	if disabled {
		col = t.Text.Alpha(0.22)
	} else {
		b.Cursor(ui.CursorPointer)
		if b.Hovered() {
			b.Background(hoverFill(t))
			col = t.Text
		}
	}
	b.Children(func() {
		ui.Text(c, symbol).FontSize(11.5).TextColor(col).NoWrap()
	})
	if !disabled && b.Clicked() {
		onClick()
	}
}
