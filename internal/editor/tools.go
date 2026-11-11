package editor

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/ui"
)

func (e *Editor) insertItem(item insertion) { e.insertItemAfter(nil, item) }

// insertItemAfter runs prepare, then inserts item at the caret, as one step
// of the history.
func (e *Editor) insertItemAfter(prepare func(), item insertion) {
	e.UnmarkText()
	e.Engine.Edit(func() {
		if prepare != nil {
			prepare()
		}
		leaf := e.Engine.LeafAt(e.Caret)
		if e.slashOpen {
			e.Engine.Replace(leaf.Start, leaf.End, "")
		}
		switch item.kind {
		case "table":
			e.Engine.Insert(document.Table(3, 3))
		case "columns":
			e.Engine.Insert(document.Columns())
		case "rule":
			e.Engine.Insert(document.NewBlock("rule", ""))
		case "image":
			// The host picks the images and inserts it at the caret.
		case "wiki":
			e.Engine.Replace(e.Caret, e.Caret, "[[")
		default:
			e.Engine.SetKind(item.kind, item.level)
		}
	})
	e.slashOpen, e.slashDismissed = false, true
	e.RequestFocus, e.reveal = true, true
	e.sync()
	if item.kind == "image" && e.attach != nil {
		e.attach()
	}
}

// Corner holds the word count and the format menu, which the host draws
// in its toolbar beside the address. The page has no format bar: text is
// formatted from the bar over a selection, blocks from "/", the "+" and the
// handle beside each block, and everything from the command bar.
func (e *Editor) Corner(c *ui.Context) {
	t := c.Theme()
	leaf := e.Engine.LeafAt(e.Caret)
	ui.Row(c.Key("page-corner")).Gap(2).AlignItems(ui.Center).Children(func() {
		ui.Text(c, fmt.Sprintf("%d 字", e.wordCount())).FontSize(12).TextColor(t.TextMuted).NoWrap().Padding(0, 6).Tooltip("字数（中文按字、英文按词）")
		label := "编辑中"
		if e.ReadOnly {
			label = "只读"
		}
		if ui.Button(c.Key("reading-lock"), label).FontSize(12).Tooltip("切换文档只读模式").Clicked() {
			e.SetReadOnly(!e.ReadOnly)
		}
		if !e.ReadOnly {
			e.moreMenu(c, leaf)
		}
	})
}

// moreMenu holds the formats and actions of the caret's block.
func (e *Editor) moreMenu(c *ui.Context, leaf document.Leaf) {
	toolButton(c, "more", iconFormat, "更多格式", false).Menu(func(m *ui.Menu) {
		menuAction(m, "下划线", func() { e.Wrap("_") })
		menuAction(m, "删除线", func() { e.Wrap("~~") })
		menuAction(m, "行内代码", func() { e.Wrap("`") })
		menuAction(m, "上标", func() { e.SetScript("sup") })
		menuAction(m, "下标", func() { e.SetScript("sub") })
		menuAction(m, "插入行内公式", e.InsertInlineMath)
		menuAction(m, "插入脚注", func() { e.InsertFootnote(); e.RequestFocus, e.reveal = true, true })
		menuAction(m, "跳转脚注 / 返回正文", func() { e.FollowFootnote() })
		m.Separator()
		menuAction(m, "撤销", func() { e.Undo(); e.RequestFocus = true })
		menuAction(m, "重做", func() { e.Redo(); e.RequestFocus = true })
		m.Separator()
		for _, align := range []struct{ name, value string }{{"左对齐", "left"}, {"居中", "center"}, {"右对齐", "right"}} {
			menuAction(m, align.name, func() { e.Engine.Align(align.value); e.RequestFocus = true })
		}
		menuAction(m, "增加缩进", func() { e.Engine.Indent(1) })
		menuAction(m, "减少缩进", func() { e.Engine.Indent(-1) })
		if leaf.Block.Kind == "callout" {
			m.Separator()
			for _, tone := range document.CalloutColors {
				if m.Item("高亮块：" + CalloutName(tone)).Checked(leaf.Block.Color == tone).Chosen() {
					e.Engine.SetColor(leaf.Block.ID, tone)
					e.RequestFocus = true
				}
			}
		}
		if leaf.Block.Kind == "code" {
			m.Separator()
			menuAction(m, "复制代码", func() { e.copyBlock(leaf.Block.ID) })
		}
		if leaf.Block.Kind == "cell" {
			m.Separator()
			if table, row, col, ok := e.Doc.CellAt(leaf.Block.ID); ok {
				m.Submenu("单元格背景颜色", func(sub *ui.Menu) {
					for _, name := range append([]string{""}, document.TextHighlights...) {
						label := "无"
						if name != "" {
							label = ColorName(name, true)
						}
						if sub.Item(label).Checked(leaf.Block.Background == name).Chosen() {
							e.Engine.SetCellBackground(table.ID, document.CellRange{Row0: row, Col0: col, Row1: row, Col1: col}, name)
						}
					}
				})
				if cols, rows := document.Span(leaf.Block); cols > 1 || rows > 1 {
					menuAction(m, "拆分单元格", func() { e.Engine.SplitCell(leaf.Block.ID) })
				}
			}
			menuAction(m, "增加表格行", func() { e.Engine.ResizeTable(true, 1) })
			menuAction(m, "增加表格列", func() { e.Engine.ResizeTable(false, 1) })
			menuAction(m, "删除末行", func() { e.Engine.ResizeTable(true, -1) })
			menuAction(m, "删除末列", func() { e.Engine.ResizeTable(false, -1) })
		}
	})
}

// calloutEmoji are the icons a callout can take.
var calloutEmoji = []string{"💡", "👍", "🌟", "🎉", "📌", "📚", "✏️", "✍️", "❤️", "📍", "🥖", "⚽", "🎹", "🎼", "⛱️", "🎨", "🥇", "🏆", "🎁", "🌅",
	"🏕️", "🏖️", "🏝️", "🚅", "🥛", "🌰", "🍰", "🍞", "🦄", "🐵"}

// emojiGrid lets a callout take an emoji as its icon, or go back to its
// color's icon; done runs after a pick.
func (e *Editor) emojiGrid(c *ui.Context, id string, done func()) {
	b := e.blockByID(id)
	if b == nil {
		return
	}
	pick := func(emoji string) {
		e.Engine.SetEmoji(id, emoji)
		e.RequestFocus = true
		done()
	}
	items := append([]string{""}, calloutEmoji...)
	ui.Column(c).Gap(2).Children(func() {
		for row := 0; row < len(items); row += 8 {
			ui.Row(c.Key(row)).Justify(ui.SpaceBetween).Children(func() {
				for _, emoji := range items[row:min(row+8, len(items))] {
					label := "使用颜色图标"
					if emoji != "" {
						label = "图标：" + emoji
					}
					cell := ui.ButtonBase(c.Key("emoji:"+emoji)).Size(26, 26).Radius(4).Center().Label(label).Tooltip(label).KeepFocus().Cursor(ui.CursorPointer)
					if b.Emoji == emoji {
						cell.Background(e.page.TurnActive)
					} else if cell.Hovered() {
						cell.Background(e.page.Hover)
					}
					cell.Children(func() {
						if emoji == "" {
							ui.Icon(c, calloutIcon(b.Color)).FontSize(16).TextColor(e.calloutHue(b.Color, c.Theme()))
						} else {
							ui.Text(c, emoji).FontSize(17)
						}
					})
					if cell.Clicked() {
						pick(emoji)
					}
				}
				for i := len(items); i < row+8; i++ {
					ui.Box(c.Key(i)).Size(26, 26)
				}
			})
		}
	})
}

// colorSwatches lets a callout's color be picked: a round swatch per tone,
// named for assistive technology and tooltips; done runs after a pick.
func (e *Editor) colorSwatches(c *ui.Context, id string, done func()) {
	t := c.Theme()
	b := e.blockByID(id)
	if b == nil {
		return
	}
	ui.Row(c).Gap(6).Children(func() {
		for _, tone := range document.CalloutColors {
			hue := e.calloutHue(tone, t)
			name := CalloutName(tone)
			s := ui.ButtonBase(c.Key("swatch:"+tone)).Size(28, 28).Radius(14).Center().Label("高亮块颜色："+name).Tooltip(name).
				KeepFocus().Cursor(ui.CursorPointer).Background(hue.Alpha(.14)).Border(1, hue.Alpha(.4))
			if b.Color == tone {
				s.Border(2, hue)
			} else if s.Hovered() {
				s.Background(hue.Alpha(.24))
			}
			s.Children(func() { ui.Icon(c, calloutIcon(tone)).FontSize(14).TextColor(hue) })
			if s.Clicked() {
				e.Engine.SetColor(id, tone)
				e.RequestFocus = true
				done()
			}
		}
	})
}

// OpenColorPicker opens the color picker of callout id, as a click on its
// icon does.
// OpenBlockMenu opens the menu of top-level block id, as a click on its
// handle does, with the side panel sub ("indent", "color" or "") open.
func (e *Editor) OpenBlockMenu(id, sub string) {
	e.menuOpen, e.menuID, e.menuSub = true, id, sub
}

func (e *Editor) OpenColorPicker(id string) {
	for _, d := range e.decorations {
		if d.kind == "callout" && d.id == id {
			e.colorOpen, e.colorID, e.colorAt = true, id, calloutIconRect(d)
		}
	}
}

// blockByID finds a block anywhere in the document.
func (e *Editor) blockByID(id string) *document.Block {
	var walk func([]*document.Block) *document.Block
	walk = func(blocks []*document.Block) *document.Block {
		for _, b := range blocks {
			if b.ID == id {
				return b
			}
			if found := walk(b.Children); found != nil {
				return found
			}
		}
		return nil
	}
	return walk(e.Doc.Blocks)
}

// wordCount counts the document as Chinese writers do: each Han
// character, kana or hangul is a word, as is each run of other letters and
// digits. It is recounted only when the document changes.
func (e *Editor) wordCount() int {
	if e.wordsReady && e.wordsRevision == e.Revision() {
		return e.words
	}
	if change, ok := e.TextChangeSince(e.wordsRevision); ok && e.wordsReady {
		e.words += countWords(change.After) - countWords(change.Before)
	} else {
		e.sync()
		e.words = countWords(e.text)
	}
	e.wordsRevision, e.wordsReady = e.Revision(), true
	return e.words
}

func countWords(text string) int {
	n, inWord := 0, false
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
			n++
			inWord = false
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if !inWord {
				n++
			}
			inWord = true
		default:
			inWord = false
		}
	}
	return n
}

// toolButton is a square icon button; on marks a format
// the selection already has.
func toolButton(c *ui.Context, key string, icon *ui.SVG, tip string, on bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Size(30, 30).Radius(7).Center().Label(tip).Tooltip(tip).KeepFocus().Cursor(ui.CursorPointer)
	color := t.Text.Alpha(.68)
	if b.Hovered() {
		b.Background(t.Text.Alpha(.06))
		color = t.Text.Alpha(.9)
	}
	if on {
		b.Background(t.Accent.Alpha(.12))
		color = t.Accent
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(16).TextColor(color) })
	return b
}

// toolSeparator parts groups of a bar of tools.
func toolSeparator(c *ui.Context) {
	ui.Box(c).Width(1).Height(16).Margin(0, 6).Background(c.Theme().Border)
}

func (e *Editor) watchSlash() {
	leaf := e.Engine.LeafAt(e.Caret)
	text := leaf.Block.Text()
	show := !e.ReadOnly && !e.slashDismissed && e.Anchor == e.Caret && leaf.Block.Kind == "paragraph" && strings.HasPrefix(text, "/") && !strings.Contains(text, "\n")
	if !show {
		e.slashOpen = false
		return
	}
	query := strings.TrimPrefix(text, "/")
	if query != e.slashQuery {
		e.slashIndex = 0
	}
	e.slashQuery, e.slashOpen = query, true
}
func (e *Editor) popups(c *ui.Context, surface ui.Element) {
	t := c.Theme()
	e.slashMenu(c, surface)
	e.selectionBar(c, surface)
	if e.colorOpen && e.colorID != "" {
		bounds := surface.Bounds()
		ui.Overlay(c, func() {
			panel := ui.Column(c.Key("callout-colors")).Absolute().Left(bounds.X + e.colorAt.X - 6).Top(bounds.Y + e.colorAt.Y + e.colorAt.H + 6).Width(230).Padding(10).Gap(6)
			e.popover(c, panel).Children(func() {
				ui.Text(c, "高亮块颜色").FontSize(12).TextColor(t.Text)
				e.colorSwatches(c, e.colorID, func() { e.colorOpen = false })
				ui.Text(c, "图标").FontSize(12).TextColor(t.Text).Margin(6, 0, 0, 0)
				e.emojiGrid(c, e.colorID, func() { e.colorOpen = false })
			})
		})
	}
	if e.langOpen && e.langID != "" {
		bounds := surface.Bounds()
		current := ""
		if b := e.blockByID(e.langID); b != nil {
			current = b.Language
		}
		ui.Overlay(c, func() {
			ui.Column(c.Key("code-languages")).Absolute().Left(bounds.X+e.langAt.X).Top(bounds.Y+e.langAt.Y+e.langAt.H+4).Width(180).
				Padding(6).Radius(10).Background(t.Background).Border(1, t.Border).Shadow(0, 8, 24, 0, t.Text.Alpha(.14)).KeepFocus().Children(func() {
				ui.Scroll(c).MaxHeight(280).Children(func() {
					for _, l := range codeLanguages {
						row := ui.ButtonBase(c.Key("lang:" + l.name)).Height(30).PaddingX(8).Radius(6).KeepFocus().Label("代码语言：" + l.label).Cursor(ui.CursorPointer)
						if row.Hovered() {
							row.Background(t.Text.Alpha(.05))
						}
						row.Children(func() {
							ui.Row(c).Grow(1).AlignItems(ui.Center).Children(func() {
								ui.Text(c, l.label).FontSize(13).Grow(1)
								if strings.EqualFold(current, l.name) {
									ui.Icon(c, iconCheck).FontSize(14).TextColor(t.Accent)
								}
							})
						})
						if row.Clicked() {
							e.Engine.SetLanguage(e.langID, l.name)
							e.langOpen = false
							e.sync()
							e.RequestFocus = true
						}
					}
				})
			})
		})
	}
	e.blockMenu(c, surface)
	e.tableToolbar(c, surface)
	e.linkPopover(c, surface)
}

func (e *Editor) cellTab(back bool) bool {
	leaf := e.Engine.LeafAt(e.Caret)
	if leaf.Block.Kind != "cell" {
		return false
	}
	cells := e.Doc.Leaves()
	for i, cell := range cells {
		if cell.Block.ID != leaf.Block.ID {
			continue
		}
		next := i + 1
		if back {
			next = i - 1
		}
		if next >= 0 && next < len(cells) && cells[next].Owner == leaf.Owner {
			e.SetSelection(cells[next].Start, cells[next].Start)
			e.reveal = true
			return true
		}
		if !back {
			e.Engine.ResizeTable(true, 1)
			cells = e.Doc.Leaves()
			if i+1 < len(cells) {
				e.SetSelection(cells[i+1].Start, cells[i+1].Start)
			}
		}
		return true
	}
	return true
}

func menuAction(m *ui.Menu, label string, run func()) {
	if m.Item(label).Chosen() {
		run()
	}
}
