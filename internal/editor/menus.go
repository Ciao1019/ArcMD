package editor

// The slash menu, the block menu and the bar over a selection: a 247 wide
// popover with an icon grid of the basic blocks and tinted rows below, 32
// tall rows with an 18 icon, and a 40 tall bar of 24 buttons.

import (
	"strings"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/ui"
)

const (
	menuWidth = 247
	slashMaxH = 351
	gridCols  = 6
)

// Slash menu groups.
var slashGroups = []struct{ title, search string }{
	{"基础", "基础 basic jichu jc"},
	{"常用", "常用 common changyong cy"},
}

// insertion is an item of the slash menu and the block type menus.
type insertion struct {
	name, kind, search string
	level              int
	icon               *ui.SVG
	// group is the slash menu group; grid items of the basic group show as
	// icons while nothing is typed after "/".
	group int
	grid  bool
	// tint names the icon's color in the slash menu's rows.
	tint string
	// md is the Markdown shortcut, shown in tooltips.
	md string
}

var insertions = []insertion{
	{name: "正文", kind: "paragraph", search: "text paragraph zhengwen zw wenben", icon: iconParagraph, tint: "blue"},
	{name: "一级标题", kind: "heading", level: 1, search: "h1 heading title yijibiaoti yjbt", icon: iconHeading1, grid: true, tint: "blue", md: "# 空格"},
	{name: "二级标题", kind: "heading", level: 2, search: "h2 heading erjibiaoti ejbt", icon: iconHeading2, grid: true, tint: "blue", md: "## 空格"},
	{name: "三级标题", kind: "heading", level: 3, search: "h3 heading sanjibiaoti sjbt", icon: iconHeading3, grid: true, tint: "blue", md: "### 空格"},
	{name: "四级标题", kind: "heading", level: 4, search: "h4 heading sijibiaoti", icon: iconHeading4, grid: true, tint: "blue", md: "#### 空格"},
	{name: "五级标题", kind: "heading", level: 5, search: "h5 heading wujibiaoti wjbt", icon: iconHeading5, grid: true, tint: "blue", md: "##### 空格"},
	{name: "有序列表", kind: "numbered", search: "ordered numbered list youxuliebiao yxlb", icon: iconOrdered, grid: true, tint: "indigo", md: "1. 空格"},
	{name: "无序列表", kind: "bullet", search: "bullet unordered list wuxuliebiao wxlb", icon: iconList, grid: true, tint: "indigo", md: "- 空格"},
	{name: "任务", kind: "task", search: "todo task renwu rw", icon: iconTodo, group: 1, grid: true, tint: "indigo", md: "[] 空格"},
	{name: "代码块", kind: "code", search: "code daimakuai dmk", icon: iconCodeBlock, grid: true, tint: "green", md: "``` 空格"},
	{name: "引用", kind: "quote", search: "quote yinyong yy cite", icon: iconQuote, grid: true, tint: "blue", md: "> 空格"},
	{name: "高亮块", kind: "callout", search: "callout gaoliangkuai glk highlight", icon: iconBulb, group: 1, grid: true, tint: "orange"},
	{name: "分隔线", kind: "rule", search: "divider rule fengexian fgx line", icon: iconRule, grid: true, tint: "orange", md: "---"},
	{name: "图片", kind: "image", search: "image picture tupian tp", icon: iconImage, group: 1, tint: "yellow"},
	{name: "表格", kind: "table", search: "table biaoge bg", icon: iconTable, group: 1, tint: "teal"},
	{name: "分栏", kind: "columns", search: "columns grid fenlan fl shuanglan", icon: iconColumns, group: 1, tint: "indigo"},
	{name: "公式", kind: "math", search: "math formula equation latex tex gongshi gs", icon: iconSigma, group: 1, tint: "teal"},
	{name: "链接到文档", kind: "wiki", search: "wiki link backlink shuanglian sl lianjie wendang", icon: iconLink, group: 1, tint: "blue", md: "[["},
}

// slashEntry is a row or grid cell of the slash menu.
type slashEntry struct {
	item insertion
	grid bool
}

// slashEntries are the slash menu's items in order: with nothing typed,
// the basic grid then the common rows; else the matches, best first —
// name before group, prefix before substring.
func (e *Editor) slashEntries() []slashEntry {
	var out []slashEntry
	q := strings.ToLower(strings.TrimSpace(e.slashQuery))
	if q == "" {
		for _, it := range insertions {
			if it.grid {
				out = append(out, slashEntry{it, true})
			}
		}
		for _, it := range insertions {
			if it.group == 1 && (e.attach != nil || it.kind != "image") {
				out = append(out, slashEntry{it, false})
			}
		}
		return out
	}
	ranked := [4][]slashEntry{}
	for _, it := range insertions {
		if e.attach == nil && it.kind == "image" {
			continue
		}
		words := strings.Fields(strings.ToLower(it.name + " " + it.search))
		group := strings.Fields(strings.ToLower(slashGroups[it.group].search))
		prefix := func(ws []string) bool {
			for _, w := range ws {
				if strings.HasPrefix(w, q) {
					return true
				}
			}
			return false
		}
		contains := func(ws []string) bool { return strings.Contains(strings.Join(ws, " "), q) }
		switch {
		case prefix(words):
			ranked[0] = append(ranked[0], slashEntry{it, false})
		case prefix(group):
			ranked[1] = append(ranked[1], slashEntry{it, false})
		case contains(words):
			ranked[2] = append(ranked[2], slashEntry{it, false})
		case contains(group):
			ranked[3] = append(ranked[3], slashEntry{it, false})
		}
	}
	for _, r := range ranked {
		out = append(out, r...)
	}
	return out
}

// tint is the color of a slash row's icon.
func (e *Editor) tint(name string) ui.Color {
	if c, ok := e.page.Tints[name]; ok {
		return c
	}
	return e.page.Marker
}

// popover styles a floating panel : a hairline, radius 6, a
// soft drop.
func (e *Editor) popover(c *ui.Context, el ui.Element) ui.Element {
	t := c.Theme()
	return el.Background(e.page.Popover).Border(1, t.Border).Radius(6).Shadow(0, 4, 16, 0, t.Text.Alpha(.12)).KeepFocus()
}

// menuRow is a 32 tall row: an 18 icon, the label, an optional hint.
func (e *Editor) menuRow(c *ui.Context, key string, icon *ui.SVG, iconColor ui.Color, label, hint string, on bool) ui.Element {
	t := c.Theme()
	row := ui.ButtonBase(c.Key(key)).Height(32).PaddingX(8).Radius(4).KeepFocus().Label(label).Cursor(ui.CursorPointer)
	if on || row.Hovered() {
		row.Background(e.page.Hover)
	}
	row.Children(func() {
		ui.Row(c).Grow(1).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).FontSize(18).TextColor(iconColor)
			ui.Text(c, label).FontSize(14).TextColor(t.Text).Grow(1).NoWrap()
			switch hint {
			case "":
			case "›":
				ui.Icon(c, iconChevronRight).FontSize(14).TextColor(e.page.Caption)
			default:
				ui.Text(c, hint).FontSize(12).TextColor(e.page.Caption).NoWrap()
			}
		})
	})
	return row
}

// menuSeparator is a hairline between groups of rows.
func menuSeparator(c *ui.Context) {
	ui.Box(c).Height(1).Margin(4, 0).Background(c.Theme().Border)
}

// groupTitle names a group of the slash menu.
func (e *Editor) groupTitle(c *ui.Context, title string) {
	ui.Row(c).Height(32).PaddingX(4).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(14).TextColor(e.page.Caption)
	})
}

// gridCell is a 24 icon button of the slash and block menus' grids.
func (e *Editor) gridCell(c *ui.Context, key string, it insertion, on, active bool) ui.Element {
	tip := it.name
	if it.md != "" {
		tip += "\nMarkdown：" + it.md
	}
	cell := ui.ButtonBase(c.Key(key)).Size(24, 24).Radius(4).Center().Label(it.name).Tooltip(tip).KeepFocus().Cursor(ui.CursorPointer)
	switch {
	case active:
		cell.Background(e.page.TurnActive)
	case on || cell.Hovered():
		cell.Background(e.page.Hover)
	}
	color := e.page.CodeText
	if active {
		color = e.page.Marker
	}
	cell.Children(func() { ui.Icon(c, it.icon).FontSize(18).TextColor(color) })
	return cell
}

// iconGrid lays cells out six to a row, spread across the menu.
func iconGrid[T any](c *ui.Context, items []T, cell func(i int, item T)) {
	ui.Column(c).Gap(12).PaddingX(4).Margin(0, 0, 8, 0).Children(func() {
		for start := 0; start < len(items); start += gridCols {
			ui.Row(c.Key(start)).Justify(ui.SpaceBetween).Children(func() {
				for i := start; i < min(start+gridCols, len(items)); i++ {
					cell(i, items[i])
				}
				for i := len(items); i < start+gridCols; i++ {
					ui.Box(c.Key(i)).Size(24, 24)
				}
			})
		}
	})
}

// slashMenu is the "/" menu at the caret.
func (e *Editor) slashMenu(c *ui.Context, surface ui.Element) {
	if !e.slashOpen || !e.focused {
		return
	}
	t := c.Theme()
	bounds := surface.Bounds()
	caret, _, _ := e.BoundsForRange(e.Selection().Range)
	entries := e.slashEntries()
	e.slashIndex = max(0, min(e.slashIndex, len(entries)-1))
	windowW, windowH := c.Size()
	x := min(bounds.X+caret.X, windowW-menuWidth-12)
	y := bounds.Y + caret.Y + caret.H + 4
	height := float32(slashMaxH)
	if y+height > windowH-12 && bounds.Y+caret.Y-height-4 > 8 {
		y = bounds.Y + caret.Y - height - 4
	}
	ui.Overlay(c, func() {
		menu := ui.Column(c.Key("slash-menu")).Absolute().Left(max(8, x)).Top(y).Width(menuWidth).Padding(1)
		e.popover(c, menu).Children(func() {
			ui.Scroll(c.Key("slash-scroll")).MaxHeight(slashMaxH - 2).Children(func() {
				ui.Column(c).Padding(8).Children(func() {
					if len(entries) == 0 {
						ui.Row(c).Height(32).PaddingX(8).AlignItems(ui.Center).Children(func() {
							ui.Text(c, "未能找到相关结果").FontSize(14).TextColor(e.page.Caption)
						})
						return
					}
					run := func(it insertion) { e.insertItem(it) }
					var grid []int
					for i, en := range entries {
						if en.grid {
							grid = append(grid, i)
						}
					}
					if len(grid) > 0 {
						e.groupTitle(c, slashGroups[0].title)
						iconGrid(c, grid, func(_ int, i int) {
							it := entries[i].item
							if e.gridCell(c, "slash:"+it.name, it, i == e.slashIndex, false).Clicked() {
								run(it)
							}
						})
					}
					group := -1
					for i, en := range entries {
						if en.grid {
							continue
						}
						if en.item.group != group && e.slashQuery == "" {
							group = en.item.group
							e.groupTitle(c, slashGroups[group].title)
						}
						it := en.item
						row := ui.ButtonBase(c.Key("slash-row:" + it.name)).Height(32).PaddingX(4).Radius(4).KeepFocus().Label(it.name).Cursor(ui.CursorPointer)
						if i == e.slashIndex || row.Hovered() {
							row.Background(e.page.Hover)
						}
						row.Children(func() {
							ui.Row(c).Grow(1).AlignItems(ui.Center).Children(func() {
								ui.Box(c).Size(24, 24).Center().Children(func() {
									ui.Icon(c, it.icon).FontSize(18).TextColor(e.tint(it.tint))
								})
								ui.Text(c, it.name).FontSize(14).TextColor(t.Text).Margin(0, 0, 0, 16).Grow(1)
							})
						})
						if row.Clicked() {
							run(it)
						}
					}
				})
			})
		})
	})
}

// slashKeys moves through the slash menu: the arrows step through the
// grid and the rows in order, Enter runs, Escape closes.
func (e *Editor) slashKeys(ev ui.InputEvent) bool {
	if !e.slashOpen || e.mark != nil {
		return false
	}
	entries := e.slashEntries()
	switch ev.Key {
	case ui.KeyEscape:
		e.slashOpen, e.slashDismissed = false, true
	case ui.KeyDown, ui.KeyRight:
		e.slashIndex = min(len(entries)-1, e.slashIndex+1)
	case ui.KeyUp, ui.KeyLeft:
		e.slashIndex = max(0, e.slashIndex-1)
	case ui.KeyEnter:
		if len(entries) > 0 {
			e.insertItem(entries[max(0, min(e.slashIndex, len(entries)-1))].item)
		}
	default:
		return false
	}
	return true
}

// turnItems are the block types a block can turn into from its menu:
// none for a table or rule; code and callouts lose the kinds that cannot
// hold them.
func turnItems(b *document.Block) []insertion {
	var out []insertion
	switch b.Kind {
	case "paragraph", "heading", "bullet", "numbered", "task", "quote", "code", "callout":
	default:
		return nil
	}
	for _, it := range insertions {
		switch {
		case it.kind == "rule" || it.kind == "table" || it.kind == "columns" || it.kind == "image":
		case b.Kind == "code" && it.kind == "callout":
		case b.Kind == "callout" && (it.kind == "code" || it.kind == "quote"):
		default:
			out = append(out, it)
		}
	}
	return out
}

// sameKind reports whether b already is it.
func sameKind(b *document.Block, it insertion) bool {
	return b.Kind == it.kind && (it.kind != "heading" || b.Level == it.level)
}

// blockMenu is the menu of the block whose handle was clicked: its type as
// an icon grid, indent and alignment, color, then cut, copy and delete.
func (e *Editor) blockMenu(c *ui.Context, surface ui.Element) {
	if !e.menuOpen {
		return
	}
	b := e.blockByID(e.menuID)
	var handle *blockHandle
	for i := range e.handles {
		if e.handles[i].id == e.menuID {
			handle = &e.handles[i]
		}
	}
	if b == nil || handle == nil {
		e.menuOpen = false
		return
	}
	t := c.Theme()
	bounds := surface.Bounds()
	id := e.menuID
	done := func() { e.menuOpen, e.menuSub = false, ""; e.RequestFocus = true; e.sync() }
	act := func(run func()) func() { return func() { run(); done() } }
	x := bounds.X + handle.rect.X
	y := bounds.Y + handle.rect.Y + handle.rect.H + 4
	ui.Overlay(c, func() {
		menu := ui.Column(c.Key("block-menu")).Absolute().Left(x).Top(y).Width(menuWidth).Padding(8)
		e.popover(c, menu).Children(func() {
			if items := turnItems(b); len(items) > 0 {
				iconGrid(c, items, func(_ int, it insertion) {
					if e.gridCell(c, "turn:"+it.name, it, false, sameKind(b, it)).Clicked() {
						e.Engine.FocusBlock(id)
						e.sync()
						e.insertItem(it)
						done()
					}
				})
				menuSeparator(c)
			}
			hover := func(row ui.Element, sub string) ui.Element {
				if row.Hovered() {
					e.menuSub = sub
				}
				return row
			}
			hover(e.menuRow(c, "bm-indent", iconIndent, e.page.CodeText, "缩进和对齐", "›", e.menuSub == "indent"), "indent")
			if b.Kind == "callout" {
				hover(e.menuRow(c, "bm-color", iconPalette, e.page.CodeText, "颜色", "›", e.menuSub == "color"), "color")
			}
			menuSeparator(c)
			for _, row := range []struct {
				key, label string
				icon       *ui.SVG
				run        func()
			}{
				{"bm-cut", "剪切", iconScissors, func() { e.copyBlockContent(id); e.Engine.DeleteBlock(id) }},
				{"bm-copy", "复制", iconCopy, func() { e.copyBlockContent(id) }},
				{"bm-duplicate", "创建副本", iconDuplicate, func() { e.Engine.Duplicate(id) }},
				{"bm-delete", "删除", iconTrash, func() { e.Engine.DeleteBlock(id) }},
			} {
				if hover(e.menuRow(c, row.key, row.icon, e.page.CodeText, row.label, "", false), "").Clicked() {
					act(row.run)()
				}
			}
			menuSeparator(c)
			for _, row := range []struct {
				key, label string
				icon       *ui.SVG
				delta      int
			}{{"bm-up", "上移", iconArrowUp, -1}, {"bm-down", "下移", iconArrowDown, 1}} {
				if hover(e.menuRow(c, row.key, row.icon, e.page.CodeText, row.label, "", false), "").Clicked() {
					act(func() { e.Engine.MoveBlock(id, handle.index+row.delta) })()
				}
			}
			menuSeparator(c)
			if hover(e.menuRow(c, "bm-add", iconSquarePlus, e.page.CodeText, "在下方添加", "", false), "").Clicked() {
				e.menuOpen, e.menuSub = false, ""
				e.insertAfter(id)
			}
		})
		outside := menu.PressedOutside()
		// The side panel of a "›" row opens beside the menu.
		if e.menuSub != "" {
			top := y + 8
			if n := len(turnItems(b)); n > 0 {
				top += float32((n+gridCols-1)/gridCols)*36 + 9
			}
			if e.menuSub == "color" {
				top += 32
			}
			side := ui.Column(c.Key("block-menu-side")).Absolute().Left(x + menuWidth + 4).Top(top - 8).Width(menuWidth).Padding(8)
			e.popover(c, side).Children(func() {
				switch e.menuSub {
				case "indent":
					if b.Kind != "code" && b.Kind != "callout" && b.Kind != "rule" && b.Kind != "table" && b.Kind != "columns" {
						for _, a := range []struct{ key, label, value string }{{"al-l", "左对齐", "left"}, {"al-c", "居中对齐", "center"}, {"al-r", "右对齐", "right"}} {
							on := b.Align == a.value || a.value == "left" && b.Align == ""
							icon := map[string]*ui.SVG{"left": iconAlignLeft, "center": iconAlignCenter, "right": iconAlignRight}[a.value]
							if e.menuRow(c, a.key, icon, e.page.CodeText, a.label, "", on).Clicked() {
								act(func() { e.Engine.FocusBlock(id); e.Engine.Align(a.value) })()
							}
						}
						menuSeparator(c)
					}
					if e.menuRow(c, "in-inc", iconIndent, e.page.CodeText, "增加缩进", "Tab", false).Clicked() {
						act(func() { e.Engine.FocusBlock(id); e.Engine.Indent(1) })()
					}
					if e.menuRow(c, "in-dec", iconOutdent, e.page.CodeText, "减少缩进", "Shift + Tab", false).Clicked() {
						act(func() { e.Engine.FocusBlock(id); e.Engine.Indent(-1) })()
					}
				case "color":
					ui.Text(c, "高亮块颜色").FontSize(12).TextColor(t.Text).Margin(0, 0, 6, 0)
					e.colorSwatches(c, id, done)
				}
			})
			if !side.PressedOutside() {
				outside = false
			}
		}
		if outside {
			e.menuOpen, e.menuSub = false, ""
		}
	})
}

// copyBlockContent puts block id on the clipboard with ArcMD's structured
// fragment, as the block menu's copy does.
func (e *Editor) copyBlockContent(id string) {
	start, end := -1, -1
	for _, leaf := range e.Doc.Leaves() {
		if leaf.Block.ID == id || leaf.Owner == id {
			if start < 0 {
				start = leaf.Start
			}
			end = leaf.End
		}
	}
	if start < 0 || e.services == (ui.Services{}) {
		return
	}
	e.SetSelection(start, end)
	e.copySelection(e.services.WriteClipboard)
	e.SetSelection(start, start)
}

// selectionBar floats over selected text once the pointer lets go:
// block type and alignment menus, the marks, the
// link, then the block's other actions.
func (e *Editor) selectionBar(c *ui.Context, surface ui.Element) {
	sel := e.Selection().Range
	if !e.focused || sel.Start == sel.End || e.dragging || e.slashOpen || e.mark != nil || e.menuOpen || e.linkOpen {
		e.colorPanel = false
		return
	}
	if _, _, across := e.cellRange(); across {
		// Cells selected across get the cell toolbar instead.
		e.colorPanel = false
		return
	}
	first, _, ok := e.BoundsForRange(ui.TextInputRange{Start: sel.Start, End: sel.Start})
	if !ok {
		return
	}
	last, _, _ := e.BoundsForRange(ui.TextInputRange{Start: sel.End, End: sel.End})
	bounds := surface.Bounds()
	width, height := float32(380), float32(42)
	windowW, _ := c.Size()
	x := max(8, min(bounds.X+first.X-12, windowW-width-12))
	y := bounds.Y + first.Y - height - 5
	if y < bounds.Y {
		y = bounds.Y + last.Y + last.H + 5
	}
	leaf := e.Engine.LeafAt(e.Caret)
	style := e.Engine.CurrentStyle()
	ui.Overlay(c, func() {
		bar := ui.Row(c.Key("selection-bar")).Absolute().Left(x).Top(y).Height(height).PaddingX(8).Gap(8).AlignItems(ui.Center)
		e.popover(c, bar).Children(func() {
			drop := func(key, tip string, icon *ui.SVG, build func(m *ui.Menu)) {
				b := ui.ButtonBase(c.Key(key)).Size(44, 24).Radius(4).Label(tip).Tooltip(tip).KeepFocus().Cursor(ui.CursorPointer)
				if b.Hovered() {
					b.Background(e.page.Hover)
				}
				b.Menu(build)
				b.Children(func() {
					ui.Row(c).Grow(1).Gap(6).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, icon).FontSize(18).TextColor(e.page.CodeText)
						ui.Icon(c, iconChevron).FontSize(12).TextColor(e.page.Placeholder)
					})
				})
			}
			sep := func() { ui.Box(c).Size(1, 20).Background(c.Theme().Border) }
			drop("float:turn", "转换为", kindIcon(leaf.Block), func(m *ui.Menu) {
				for _, it := range turnItems(leaf.Block) {
					if m.Item(it.name).Checked(sameKind(leaf.Block, it)).Chosen() {
						e.insertItem(it)
					}
				}
			})
			sep()
			align := map[string]*ui.SVG{"center": iconAlignCenter, "right": iconAlignRight}[leaf.Block.Align]
			if align == nil {
				align = iconAlignLeft
			}
			drop("float:align", "对齐方式", align, func(m *ui.Menu) {
				for _, a := range []struct{ name, value string }{{"左对齐", "left"}, {"居中对齐", "center"}, {"右对齐", "right"}} {
					if m.Item(a.name).Checked(leaf.Block.Align == a.value || a.value == "left" && leaf.Block.Align == "").Chosen() {
						e.Engine.Align(a.value)
						e.RequestFocus = true
					}
				}
			})
			sep()
			mark := func(key string, icon *ui.SVG, tip, marker string, on bool) {
				if markButton(c, e.page, key, icon, tip, on).Clicked() {
					e.Wrap(marker)
					e.RequestFocus = true
				}
			}
			mark("float:**", iconBold, "加粗 (⌘B)\nMarkdown：**文本**", "**", style.Bold)
			mark("float:~~", iconStrike, "删除线 (⇧⌘X)\nMarkdown：~~文本~~", "~~", style.Strike)
			mark("float:*", iconItalic, "倾斜 (⌘I)\nMarkdown：*文本*", "*", style.Italic)
			mark("float:_", iconUnderline, "下划线 (⌘U)", "_", style.Underline)
			if markButton(c, e.page, "float:link", iconLink, "添加链接", style.Link != "").Clicked() {
				e.EditLink()
			}
			mark("float:`", iconCode, "行内代码 (⌘E)\nMarkdown：`代码`", "`", style.Code)
			sep()
			e.colorSplit(c)
		})
		if e.colorPanel {
			e.textColorPanel(c, x+width-230-8, y+height+4)
		}
	})
}

// colorNames name the text colors and highlights for people.
var colorNames = map[string]string{
	"gray": "灰色", "red": "红色", "orange": "橙色", "yellow": "黄色", "green": "绿色", "blue": "蓝色", "purple": "紫色",
	"light-gray": "浅灰", "light-red": "浅红", "light-orange": "浅橙", "light-yellow": "浅黄", "light-green": "浅绿",
	"light-blue": "浅蓝", "light-purple": "浅紫", "medium-gray": "中灰",
}

// ColorName names a text color or highlight; strong highlights read as
// "深红" and so on.
func ColorName(name string, highlight bool) string {
	if highlight && !strings.HasPrefix(name, "light-") && name != "medium-gray" {
		return "深" + strings.TrimSuffix(colorNames[name], "色")
	}
	return colorNames[name]
}

// colorSplit is the selection bar's color button: "A"
// applies the last color used, the arrow opens the color panel.
func (e *Editor) colorSplit(c *ui.Context) {
	pg := e.page
	ui.Row(c.Key("float:color")).Height(24).Radius(4).Children(func() {
		apply := ui.ButtonBase(c.Key("float:color-apply")).Size(24, 24).Radius(4).Center().Label("应用颜色").Tooltip("应用上次的颜色").KeepFocus().Cursor(ui.CursorPointer)
		if apply.Hovered() {
			apply.Background(pg.Hover)
		}
		apply.Children(func() {
			swatch := ui.Box(c).Size(20, 20).Radius(2).Center()
			if bg, ok := pg.Highlights[e.lastHighlight]; ok {
				swatch.Background(bg)
			}
			swatch.Children(func() {
				color := c.Theme().Text
				if tc, ok := pg.TextColors[e.lastColor]; ok {
					color = tc
				}
				ui.Text(c, "A").FontSize(16).TextColor(color)
			})
		})
		if apply.Clicked() {
			color, highlight := e.lastColor, e.lastHighlight
			e.Engine.SetTextColor(&color, &highlight)
			e.RequestFocus = true
		}
		open := ui.ButtonBase(c.Key("float:color-open")).Size(20, 24).Radius(4).Center().Label("文字颜色").Tooltip("文字颜色").KeepFocus().Cursor(ui.CursorPointer)
		if open.Hovered() || e.colorPanel {
			open.Background(pg.Hover)
		}
		open.Children(func() { ui.Icon(c, iconChevron).FontSize(12).TextColor(pg.Placeholder) })
		if open.Clicked() {
			e.colorPanel = !e.colorPanel
			e.colorPanelOpened = true
		}
	})
}

// OpenTextColors opens the color panel of the selection bar.
func (e *Editor) OpenTextColors() { e.colorPanel, e.colorPanelOpened = true, true }

// textColorPanel picks a text color and a highlight:
// eight font swatches, sixteen highlight swatches, and a reset.
func (e *Editor) textColorPanel(c *ui.Context, x, y float32) {
	pg := e.page
	t := c.Theme()
	style := e.Engine.CurrentStyle()
	apply := func(color, highlight *string) {
		if color != nil {
			e.lastColor = *color
		}
		if highlight != nil {
			e.lastHighlight = *highlight
		}
		e.Engine.SetTextColor(color, highlight)
		e.RequestFocus = true
	}
	swatch := func(key, label string, on bool, inner func()) ui.Element {
		s := ui.ButtonBase(c.Key(key)).Size(26, 26).Radius(4).Center().Label(label).Tooltip(label).KeepFocus().Cursor(ui.CursorPointer)
		if on {
			s.Border(2, pg.CodeHover)
		} else if s.Hovered() {
			s.Background(pg.Hover)
		}
		s.Children(inner)
		return s
	}
	title := func(text string) {
		ui.Text(c, text).FontSize(12).TextColor(t.Text).Margin(7, 0, 4, 0)
	}
	panel := ui.Column(c.Key("text-color-panel")).Absolute().Left(max(8, x)).Top(y).Width(230).Padding(4, 10, 12, 10)
	e.popover(c, panel).Children(func() {
		title("字体颜色")
		ui.Row(c).Justify(ui.SpaceBetween).Children(func() {
			for _, name := range append([]string{""}, document.TextColors...) {
				label := "默认字体颜色"
				color := t.Text
				if name != "" {
					label, color = "字体颜色："+ColorName(name, false), pg.TextColors[name]
				}
				if swatch("fc:"+name, label, style.Color == name, func() {
					ui.Box(c).Size(22, 22).Radius(2).Border(1, pg.Hover).Center().Children(func() {
						ui.Text(c, "A").FontSize(14).TextColor(color)
					})
				}).Clicked() {
					value := name
					if value != "" && style.Color == value {
						value = ""
					}
					apply(&value, nil)
				}
			}
		})
		title("背景颜色")
		names := append([]string{""}, document.TextHighlights...)
		ui.Column(c).Gap(4).Children(func() {
			for row := 0; row < len(names); row += 8 {
				ui.Row(c.Key(row)).Justify(ui.SpaceBetween).Children(func() {
					for _, name := range names[row:min(row+8, len(names))] {
						label := "无背景颜色"
						if name != "" {
							label = "背景颜色：" + ColorName(name, true)
						}
						if swatch("bc:"+name, label, style.Highlight == name, func() {
							box := ui.Box(c).Size(22, 22).Radius(2)
							if name == "" {
								box.Background(t.Background).Border(1, pg.Hover).Center().Children(func() {
									ui.Icon(c, iconSlash).FontSize(20).TextColor(pg.Placeholder)
								})
							} else {
								box.Background(pg.Highlights[name])
							}
						}).Clicked() {
							value := name
							if value != "" && style.Highlight == value {
								value = ""
							}
							apply(nil, &value)
						}
					}
				})
			}
		})
		reset := ui.ButtonBase(c.Key("color-reset")).Height(24).Margin(12, 0, 0, 0).Radius(6).Border(1, t.Border).Center().Label("恢复默认").KeepFocus().Cursor(ui.CursorPointer)
		if reset.Hovered() {
			reset.Background(pg.Hover)
		}
		reset.Children(func() { ui.Text(c, "恢复默认").FontSize(12).TextColor(t.Text) })
		if reset.Clicked() {
			none := ""
			e.Engine.SetTextColor(&none, &none)
			e.RequestFocus = true
		}
	})
	if panel.PressedOutside() && !e.colorPanelOpened {
		e.colorPanel = false
	}
	e.colorPanelOpened = false
}

// markButton is a 24 button of the selection bar; on marks a format the
// selection has.
func markButton(c *ui.Context, pg Page, key string, icon *ui.SVG, tip string, on bool) ui.Element {
	label := strings.SplitN(tip, " ", 2)[0]
	label = strings.SplitN(label, "\n", 2)[0]
	b := ui.ButtonBase(c.Key(key)).Size(24, 24).Radius(4).Center().Label(label).Tooltip(tip).KeepFocus().Cursor(ui.CursorPointer)
	color := pg.CodeText
	switch {
	case on:
		b.Background(pg.TurnActive)
		color = pg.Marker
	case b.Hovered():
		b.Background(pg.Hover)
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(18).TextColor(color) })
	return b
}

// kindIcon is the icon of a block's type.
func kindIcon(b *document.Block) *ui.SVG {
	for _, it := range insertions {
		if sameKind(b, it) {
			return it.icon
		}
	}
	return iconParagraph
}
