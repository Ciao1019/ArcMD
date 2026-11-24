package app

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/fuzzy"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
	uiglass "github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

type newModalState struct {
	open      bool
	dir       string
	query     string
	lastQuery string
	index     int
	reveal    bool
}

type newModalTagTone uint8

const (
	tagToneNeutral newModalTagTone = iota
	tagToneBlue
	tagToneAmber
)

type newModalItem struct {
	id         string
	icon       *ui.SVG
	iconAccent bool
	title      string
	detail     string
	tag        string
	tagTone    newModalTagTone
	actionText string
	visitHint  string
	keywords   string
	run        func()
}

func (a *App) openNewModal(dir string) {
	sp := a.state.ActiveSpace()
	if sp == nil {
		return
	}
	if sp.IsExternal() {
		a.chooseExternalFiles()
		return
	}
	a.newModal = newModalState{
		open: true,
		dir:  dir,
	}
}

// newDatabaseNamed creates a new amphibious .db.md database with title (or the
// default untitled database name when empty) and expands its sidebar view tree.
func (a *App) newDatabaseNamed(dir, title string) {
	sp := a.state.ActiveSpace()
	if sp == nil {
		return
	}
	if sp.IsExternal() {
		a.notify("请在普通空间中新建数据库。")
		return
	}
	if !a.flush() {
		return
	}
	baseTitle := strings.TrimSpace(title)
	if baseTitle == "" {
		baseTitle = i18n.T(untitledDatabase)
	}
	db := database.NewDefault(baseTitle, database.SourceConfig{Type: database.SourceFolder})
	content, err := database.EncodeDatabaseMarkdownRelative(dir, db, nil)
	if err != nil {
		a.notify("新建数据库失败。%v", err)
		return
	}
	rel, err := vault.CreateFile(sp.Root, dir, baseTitle, vault.ExtDatabase, content)
	if err != nil {
		a.notify("新建数据库失败。%v", err)
		return
	}
	actualTitle := vault.Title(rel)
	if actualTitle != "" && actualTitle != db.Title {
		db.Title = actualTitle
		if updated, encErr := database.EncodeDatabaseMarkdownRelative(dir, db, nil); encErr == nil {
			_ = writeFile(sp, rel, updated)
		}
	}
	if dir != "" {
		sp.SetExpanded(dir, true)
	}
	sp.SetExpanded(rel, true)
	a.saveState()
	a.clearPicks("")
	a.refreshTree()
	a.refreshSearch()
	a.open(rel)
	if d := a.doc; d != nil && d.rel == rel && d.isDatabase() {
		d.dbView.RequestFocus = true
	}
}

// newWhiteboardNamed creates a new whiteboard with title (or the default
// untitled whiteboard name when empty).
func (a *App) newWhiteboardNamed(dir, title string) {
	title = strings.TrimSpace(title)
	if title == "" {
		a.newWhiteboard(dir)
		return
	}
	sp := a.state.ActiveSpace()
	if sp == nil {
		return
	}
	if sp.IsExternal() {
		a.notify("请在普通空间中新建白板。")
		return
	}
	if !a.flush() {
		return
	}
	content, err := whiteboard.ExportEmbeddedSVG(whiteboard.NewScene(), whiteboardSVGPalette(pageStyle(false).Whiteboard))
	if err != nil {
		a.notify("新建白板失败。%v", err)
		return
	}
	rel, err := vault.CreateFile(sp.Root, dir, title, vault.ExtWhiteboard, content)
	if err != nil {
		a.notify("新建白板失败。%v", err)
		return
	}
	if dir != "" {
		sp.SetExpanded(dir, true)
		a.saveState()
	}
	a.clearPicks("")
	a.refreshTree()
	a.open(rel)
	if d := a.doc; d != nil && d.rel == rel && d.board != nil {
		d.board.RequestFocus = true
	}
}

// newFolderNamed creates a folder named title in dir (or falls back to
// newFolder with inline rename when title is empty).
func (a *App) newFolderNamed(dir, title string) {
	title = strings.TrimSpace(title)
	if title == "" {
		a.newFolder(dir)
		return
	}
	sp := a.state.ActiveSpace()
	if sp == nil || sp.IsExternal() {
		return
	}
	rel, err := vault.Mkdir(sp.Root, dir, title)
	if err != nil {
		a.notify("新建文件夹失败。%v", err)
		return
	}
	if dir != "" {
		sp.SetExpanded(dir, true)
	}
	sp.SetExpanded(rel, true)
	a.saveState()
	a.clearPicks("")
	a.refreshTree()
}

func (a *App) newModalItems(dir, rawQuery string) []newModalItem {
	q := strings.TrimSpace(rawQuery)
	buildCore := func(named string) []newModalItem {
		labelFor := func(base string) string {
			if named != "" {
				return fmt.Sprintf(i18n.T("%s「%s」"), base, named)
			}
			return base
		}
		visitOrKey := func(cmdID string) string {
			if k := a.keyLabel(cmdID); k != "" {
				return k
			}
			return i18n.T("创建") + " →"
		}
		return []newModalItem{
			{
				id:         "doc",
				icon:       iconFile,
				iconAccent: true,
				title:      labelFor(i18n.T("新建文档")),
				detail:     i18n.T("Markdown 富文本文档"),
				tag:        "Markdown",
				tagTone:    tagToneBlue,
				actionText: i18n.T("创建"),
				visitHint:  visitOrKey("new-doc"),
				keywords:   "doc document markdown md note wendang biji 文档 笔记",
				run: func() {
					if named == "" {
						a.newDoc(dir)
					} else {
						a.newDocNamed(dir, named)
					}
				},
			},
			{
				id:         "db",
				icon:       iconDatabase,
				title:      labelFor(i18n.T("新建数据库")),
				detail:     i18n.T("多维表格 · 表格、甘特、看板等 9 种视图"),
				tag:        i18n.T("多维表格"),
				tagTone:    tagToneAmber,
				actionText: i18n.T("创建"),
				visitHint:  visitOrKey("new-database"),
				keywords:   "db database table gantt shujuku biaoge gante duoweibiaoge 数据库 多维表格 表格 甘特",
				run:        func() { a.newDatabaseNamed(dir, named) },
			},
			{
				id:         "board",
				icon:       iconWhiteboard,
				title:      labelFor(i18n.T("新建白板")),
				detail:     i18n.T("Excalidraw 手绘白板"),
				tag:        "Excalidraw",
				tagTone:    tagToneNeutral,
				actionText: i18n.T("创建"),
				visitHint:  visitOrKey("new-board"),
				keywords:   "board whiteboard excalidraw sketch draw baiban huaban 白板 画板",
				run:        func() { a.newWhiteboardNamed(dir, named) },
			},
			{
				id:         "folder",
				icon:       iconFolderPlus,
				title:      labelFor(i18n.T("新建文件夹")),
				detail:     i18n.T("在当前空间新建文件夹"),
				tag:        i18n.T("文件夹"),
				tagTone:    tagToneNeutral,
				actionText: i18n.T("创建"),
				visitHint:  visitOrKey("new-folder"),
				keywords:   "folder directory dir wenjianjia mulu 文件夹 目录",
				run:        func() { a.newFolderNamed(dir, named) },
			},
		}
	}

	all := buildCore("")
	for _, tpl := range a.spaceTemplates(a.state.ActiveSpace()) {
		t := tpl
		all = append(all, newModalItem{
			id:         "tpl:" + t.Name,
			icon:       iconSparkles,
			title:      t.Name,
			detail:     i18n.T("从模板新建文档"),
			tag:        i18n.T("模板"),
			tagTone:    tagToneBlue,
			actionText: i18n.T("使用模板"),
			visitHint:  i18n.T("使用模板") + " →",
			keywords:   t.Name + " template moban 模板",
			run:        func() { a.newFromTemplate(dir, t) },
		})
	}
	all = append(all, newModalItem{
		id:         "import-csv",
		icon:       iconDatabase,
		title:      i18n.T("从 CSV 导入数据库…"),
		detail:     i18n.T("将表格文件转换为数据库"),
		tag:        "CSV",
		tagTone:    tagToneNeutral,
		actionText: i18n.T("导入"),
		visitHint:  i18n.T("导入") + " →",
		keywords:   "csv import daoru 导入",
		run:        func() { a.pickAndImportCSV(nil) },
	})

	if q == "" {
		return all
	}

	type scored struct {
		item  newModalItem
		score int
		idx   int
	}
	var matched []scored
	for i, it := range all {
		if sc, ok := fuzzy.ScoreWithHint(q, it.title, it.detail+" "+it.keywords); ok {
			matched = append(matched, scored{item: it, score: sc, idx: i})
		}
	}
	if len(matched) > 0 {
		sort.SliceStable(matched, func(i, j int) bool {
			if matched[i].score != matched[j].score {
				return matched[i].score > matched[j].score
			}
			return matched[i].idx < matched[j].idx
		})
		out := make([]newModalItem, len(matched))
		for i, m := range matched {
			out[i] = m.item
		}
		return out
	}

	// No type keyword matched: treat query as the desired name for any core type.
	return buildCore(q)
}

func renderQuickModalKbd(c *ui.Context, key string, pal quickModalPalette) {
	ui.Box(c).MinWidth(18).Height(18).Padding(0, 5).Radius(5).Center().
		Background(pal.kbdBg).Border(1, pal.kbdBorder).
		Shadow(0, 1, 0, 0, pal.kbdShadow).Children(func() {
		ui.Text(c, key).FontSize(10.5).FontWeight(600).TextColor(pal.subtle).NoWrap()
	})
}

// newModalView renders the Lumno / Petrichor QuickAsk-style frosted-glass
// floating creation bar opened by "+ 新建".
func (a *App) newModalView(c *ui.Context) {
	m := &a.newModal
	if !m.open {
		return
	}
	winW, winH := c.Size()
	width := min(float32(520), max(float32(340), min(winW-48, winW*0.48)))
	topPad := min(winH*0.29, max(float32(48), (winH-324)*0.46))
	topPad = min(topPad, max(float32(36), winH-348))
	pal := quickModalColors(a.paperDark)
	dt := documentTheme(a.paperDark)
	const radius float32 = 22

	ui.DialogBase(c.Key("new-modal"), &m.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.Transparent).
			Material(uiglass.Blur{Radius: 4}).
			Draw(func(pt *ui.Painter, r ui.Rect) {
				pt.Fill(r, pal.backdrop, 0)
			}).
			AlignItems(ui.Center).Justify(ui.Start).
			Padding(topPad, 24, 24, 24)
		panel.Width(width).Background(ui.Transparent).Radius(radius).
			Material(uiglass.Blur{Radius: 28}).
			BorderWidth(1).BorderLinearGradient(quickModalRimGradient(pal)).
			Shadow(0, 22, 48, -10, pal.shadow).
			Draw(func(pt *ui.Painter, r ui.Rect) {
				paintQuickModalGlass(pt, r, radius, pal)
			}).Clip()
		panel.Children(func() {
			ui.Themed(c.Key("new-modal-theme"), dt).Width(width).Background(ui.Transparent).Children(func() {
				items := a.newModalItems(m.dir, m.query)
				if m.query != m.lastQuery {
					m.lastQuery, m.index, m.reveal = m.query, 0, true
				}
				if c.Shortcut(0, ui.KeyDown) && len(items) > 0 {
					m.index = (m.index + 1) % len(items)
					m.reveal = true
				}
				if c.Shortcut(0, ui.KeyUp) && len(items) > 0 {
					m.index = (m.index - 1 + len(items)) % len(items)
					m.reveal = true
				}
				if len(items) > 0 {
					m.index = max(0, min(m.index, len(items)-1))
				} else {
					m.index = 0
				}

				execItem := func(it newModalItem) {
					m.open = false
					a.newDocPulse = c.Now()
					if it.run != nil {
						it.run()
					}
				}

				ui.Column(c).Width(width).Children(func() {
					// 1. Top input bar (50px height, 16px search icon, right space chip, 18px inset hairline).
					ui.Row(c.Key("new-modal-head")).Height(50).Padding(0, 12, 0, 18).Gap(12).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, iconSearch).FontSize(16).TextColor(pal.muted)
						in := ui.TextInput(c.Key("new-modal-input"), &m.query).
							Placeholder(i18n.T("输入名称，或选择要新建的类型…")).
							AutoFocus().Label(i18n.T("新建")).FontSize(15).FontWeight(500).
							TextColor(pal.text).Background(ui.Transparent).BorderWidth(0).
							Padding(5, 0).Grow(1).MinWidth(0)
						if in.Submitted() && len(items) > 0 {
							execItem(items[m.index])
							return
						}
						if sp := a.state.ActiveSpace(); sp != nil {
							chipText := sp.Name
							if m.dir != "" {
								chipText = path.Base(m.dir)
							}
							ui.Row(c.Key("new-modal-chip")).Height(28).Padding(0, 9, 0, 5).Radius(9).Gap(5).
								AlignItems(ui.Center).Shrink(0).Background(pal.chipBg).Children(func() {
								ui.Box(c).Size(18, 18).Radius(5).Background(pal.chipMarkBg).Center().Shrink(0).Children(func() {
									if sp.Icon != "" {
										ui.Text(c, sp.Icon).FontSize(10.5).NoWrap()
									} else {
										ui.Icon(c, iconFolder).FontSize(10.5).TextColor(pal.chipMarkInk)
									}
								})
								ui.Text(c, chipText).FontSize(12).FontWeight(600).TextColor(pal.accent).SingleLine()
							})
						}
					})

					// Inset 18px divider matching .quick-ask-divider.
					ui.Box(c).Height(1).Margin(0, 18).Background(pal.line)

					// 2. Results list (padding 8px 10px, gap 3px, 42px rows, radius 11px, soft translucent active pill).
					ui.Scroll(c.Key("new-modal-list")).MaxHeight(min(float32(340), max(120, winH-topPad-140))).MinHeight(0).Padding(8, 10).Children(func() {
						ui.Column(c).Gap(3).Children(func() {
							for i, it := range items {
								item := it
								selected := i == m.index
								// Use RoleButton (never RoleOption+Selected) so MyGo does not paint
								// a harsh solid blue system listbox highlight over our glass pill.
								row := ui.Row(c.Key("nm:"+item.id)).Height(42).Padding(0, 12).Radius(11).Gap(10).
									AlignItems(ui.Center).Cursor(ui.CursorPointer).
									Role(ui.RoleButton).Label(item.title)
								if row.Hovered() && !selected {
									m.index = i
									selected = true
								}
								if selected {
									row.Background(pal.rowActive)
								}
								if selected && m.reveal {
									row.ScrollIntoView()
								}
								row.Children(func() {
									// Left 24x24 rounded-7 icon tile (luminous glass tile when active).
									iconBox := ui.Box(c).Size(24, 24).Radius(7).Center().Shrink(0)
									iconInk := pal.muted
									if item.iconAccent {
										iconInk = pal.accent
									}
									if selected {
										iconBox.Background(pal.iconActiveBg)
										if !item.iconAccent {
											iconInk = pal.text
										}
									}
									iconBox.Children(func() {
										ui.Icon(c, item.icon).FontSize(14).TextColor(iconInk)
									})

									// Middle single-line text flow: Title + subtle detail + colored tag pill.
									ui.Row(c).Grow(1).MinWidth(0).Gap(7).AlignItems(ui.Center).Children(func() {
										titleEl := ui.Text(c, item.title).FontSize(13.5).TextColor(pal.text).SingleLine()
										if selected {
											titleEl.FontWeight(600)
										}
										if item.detail != "" {
											ui.Text(c, item.detail).FontSize(11.5).TextColor(pal.subtle).SingleLine().Shrink(1).MinWidth(0)
										}
										if item.tag != "" {
											tagBg, tagInk := pal.tagBg, pal.tagText
											switch item.tagTone {
											case tagToneBlue:
												tagBg, tagInk = pal.tagBlueBg, pal.tagBlueText
											case tagToneAmber:
												tagBg, tagInk = pal.tagAmberBg, pal.tagAmberText
											}
											ui.Box(c).Padding(2, 6).Radius(6).Background(tagBg).Shrink(0).Children(func() {
												ui.Text(c, item.tag).FontSize(10).TextColor(tagInk).NoWrap()
											})
										}
									})

									// Right action pill (active) or muted hint (inactive).
									if selected {
										ui.Box(c).Padding(3, 8).Radius(7).Background(pal.actionBg).Shrink(0).Children(func() {
											ui.Text(c, item.actionText).FontSize(11).FontWeight(500).TextColor(pal.actionText).NoWrap()
										})
									} else if item.visitHint != "" {
										ui.Text(c, item.visitHint).FontSize(11.5).TextColor(pal.muted).NoWrap()
									}
								})
								if row.Clicked() {
									execItem(item)
									return
								}
							}
						})
					})
					m.reveal = false

					// 3. Bottom keyboard-hint footer bar (34px height, right-aligned <kbd> keycaps).
					activeVerb := i18n.T("创建")
					if len(items) > 0 && m.index < len(items) && items[m.index].actionText != "" {
						activeVerb = items[m.index].actionText
					}
					ui.Row(c.Key("new-modal-footer")).Height(34).Padding(6, 16, 7, 16).Gap(14).
						Justify(ui.End).AlignItems(ui.Center).
						BorderWidth(1, 0, 0, 0).BorderColor(pal.line).Children(func() {
						ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
							renderQuickModalKbd(c, "↑", pal)
							renderQuickModalKbd(c, "↓", pal)
							ui.Text(c, i18n.T("选择")).FontSize(11.5).TextColor(pal.subtle).NoWrap()
						})
						ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
							renderQuickModalKbd(c, "↵", pal)
							ui.Text(c, activeVerb).FontSize(11.5).TextColor(pal.subtle).NoWrap()
						})
						ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
							renderQuickModalKbd(c, "esc", pal)
							ui.Text(c, i18n.T("关闭")).FontSize(11.5).TextColor(pal.subtle).NoWrap()
						})
					})
				})
			})
		})
	})
}
