package app

import (
	"fmt"
	"os"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/prefs"
	"github.com/egoist/mygo/ui"
)

// Settings tabs.
const (
	tabGeneral    = "general"
	tabAppearance = "appearance"
	tabSpaces     = "spaces"
	tabShortcuts  = "shortcuts"
	tabAbout      = "about"
)

// openSettings shows the settings on a tab.
func (a *App) openSettings(tab string) {
	a.settingsOpen = true
	a.settingsTab = tab
}

// moreMenu keeps document actions at hand. Library and settings lead to
// app-wide tools without repeating their individual pages here.
func (a *App) moreMenu(c *ui.Context) {
	b := iconButton(c, "more", iconEllipsis, i18n.T("更多"), i18n.T("更多"), false).Role(ui.RoleMenuButton)
	b.PopoverMenuButton(func(m *ui.Menu) {
		noDoc := a.doc == nil || a.doc.loadErr != nil
		if a.menuKey(m.Item(i18n.T("专注模式")), "focus").Checked(a.focus).Chosen() {
			a.toggleFocus()
		}
		if a.menuKey(m.Item(i18n.T("目录")), "outline").Checked(a.outlineOpen()).Disabled(noDoc || a.doc.source).Chosen() {
			a.toggleOutline()
		}
		mode := i18n.T("预览 Markdown 导出")
		if a.doc != nil && a.doc.source {
			mode = i18n.T("返回富文本编辑")
		}
		if a.menuKey(m.Item(mode), "source").Disabled(noDoc).Chosen() {
			a.toggleSource()
		}
		if a.menuKey(m.Item(i18n.T("添加分屏")), "split-add").Disabled(a.doc == nil).Chosen() {
			a.addSplitPane()
		}
		m.Separator()
		if a.menuKey(m.Item(i18n.T("版本历史…")), "history").Disabled(noDoc).Chosen() {
			a.openVersions()
		}
		m.Submenu(i18n.T("导出"), func(sub *ui.Menu) {
			if sub.Item(i18n.T("导出 Markdown 文件")).Disabled(noDoc).Chosen() {
				a.exportMarkdown()
			}
			if a.menuKey(sub.Item(i18n.T("导出 PDF")), "export-pdf").Disabled(noDoc).Chosen() {
				a.exportPDF()
			}
			if sub.Item(i18n.T("导出 HTML 网页")).Disabled(noDoc).Chosen() {
				a.exportHTML()
			}
		}).Disabled(noDoc)
		if m.Item(i18n.T("打印…")).Disabled(noDoc).Chosen() {
			a.printDoc()
		}
		if m.Item(i18n.T("在访达中显示文档")).Disabled(noDoc).Chosen() {
			a.revealIn(a.doc.space, a.doc.rel)
		}
		m.Separator()
		if a.menuKey(m.Item(i18n.T("资料库")), "library").Chosen() {
			a.toggleLibrary()
		}
		if a.menuKey(m.Item(i18n.T("设置…")), "settings").Chosen() {
			a.openSettings(tabGeneral)
		}
	})
}

// settingsView is the settings window: its tabs down the left, as Zen's
// settings, and the tab chosen on the right.
func (a *App) settingsView(c *ui.Context) {
	if !a.settingsOpen {
		return
	}
	t := c.Theme()
	w, h := c.Size()
	ui.Modal(c.Key("settings"), &a.settingsOpen, func() {
		// Leave room for the modal's outer margins and inner padding.
		ui.Row(c.Key("settings-body")).Size(min(660, w-t.Space(20)), min(460, h-t.Space(20))).Gap(0).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c.Key("settings-tabs")).Width(156).Gap(2).Padding(4, 12, 4, 0).Role(ui.RoleTabList).Label(i18n.T("设置")).
				FocusGroup(ui.Vertical).Children(func() {
				ui.Text(c, i18n.T("设置")).FontSize(16).FontWeight(600).Padding(4, 10, 12, 10)
				for _, tab := range []struct {
					id, label string
					icon      string
				}{
					{tabGeneral, i18n.T("通用"), "settings"},
					{tabAppearance, i18n.T("外观"), "type"},
					{tabSpaces, i18n.T("空间"), "layers"},
					{tabShortcuts, i18n.T("快捷键"), "keyboard"},
					{tabAbout, i18n.T("关于"), "info"},
				} {
					on := a.settingsTab == tab.id
					row := ui.Row(c.Key("settings-tab-" + tab.id)).Height(32).PaddingX(10).Gap(10).Radius(8).
						AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleTab).Label(tab.label).Selected(on)
					switch {
					case on:
						row.Background(t.Accent.Alpha(0.14))
					case row.Hovered():
						row.Background(t.Text.Alpha(0.05))
					}
					row.Children(func() {
						color := t.Text.Alpha(0.6)
						if on {
							color = t.Accent
						}
						ui.Icon(c, settingsIcons[tab.icon]).FontSize(15).TextColor(color)
						label := ui.Text(c, tab.label).TextColor(t.Text)
						if on {
							label.FontWeight(600)
						}
					})
					if row.Clicked() {
						a.settingsTab = tab.id
					}
				}
			})
			ui.Box(c).Width(1).Background(t.Text.Alpha(0.08))
			ui.Scroll(c.Key("settings-page-"+a.settingsTab)).Grow(1).MinWidth(0).Padding(8, 8, 8, 24).Gap(14).Children(func() {
				switch a.settingsTab {
				case tabAppearance:
					a.settingsAppearance(c)
				case tabSpaces:
					a.settingsSpaces(c)
				case tabShortcuts:
					a.settingsShortcuts(c)
				case tabAbout:
					a.settingsAbout(c)
				default:
					a.settingsGeneral(c)
				}
			})
		})
	})
}

// settingsHeading is a tab's title and what it is for.
func settingsHeading(c *ui.Context, title, note string) {
	t := c.Theme()
	ui.Column(c).Gap(4).Children(func() {
		ui.Text(c, title).FontSize(18).FontWeight(600)
		if note != "" {
			ui.Text(c, note).FontSize(12).TextColor(t.TextMuted)
		}
	})
}

// settingsGroup is a rounded group of rows, as macOS' settings.
func settingsGroup(c *ui.Context, key string, rows func()) {
	t := c.Theme()
	ui.Column(c.Key(key)).Radius(10).Background(t.Text.Alpha(0.04)).Border(1, t.Text.Alpha(0.06)).Children(rows)
}

// folderRow is a labeled folder with a button that opens it.
func folderRow(c *ui.Context, key, label, dir string) {
	t := c.Theme()
	ui.Row(c.Key(key)).Padding(10, 12).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Clip().Gap(2).Children(func() {
			ui.Text(c, label)
			ui.Text(c, shortPath(dir)).FontSize(12).TextColor(t.TextMuted).SingleLine().Tooltip(dir)
		})
		if ui.Button(c.Key("open"), i18n.T("在访达中显示")).Clicked() {
			os.MkdirAll(dir, 0o755)
			revealDir(dir)
		}
	})
}

func (a *App) settingsGeneral(c *ui.Context) {
	t := c.Theme()
	settingsHeading(c, i18n.T("通用"), i18n.T("ArcMD 把文档保存在你的文件夹里，设置与空间列表保存在配置文件夹。"))
	settingsGroup(c, "places", func() {
		folderRow(c, "documents", i18n.T("新空间的默认位置"), a.cfg.DocumentsDir)
		ui.Box(c).Height(1).Margin(0, 12).Background(t.Text.Alpha(0.06))
		folderRow(c, "config", i18n.T("配置文件夹（spaces.json、插件）"), a.cfg.ConfigDir)
	})
	ui.Text(c, i18n.T("颜色、外观与图标按空间设置：在侧边栏的空间名称上右键，选择要修改的属性。")).
		FontSize(12).TextColor(t.TextMuted)
	settingsHeading(c, i18n.T("写作"), "")
	settingsGroup(c, "writing", func() {
		toggleRow(c, "typewriter", i18n.T("打字机滚动"), i18n.T("正在写的一行始终停在页面中间。"), &a.prefs.Typewriter, a.savePrefs)
		ui.Box(c).Height(1).Margin(0, 12).Background(t.Text.Alpha(0.06))
		toggleRow(c, "draw-annotations", i18n.T("打开文档时画出手绘标注"), i18n.T("页面上的标注依次画出来；减弱动态效果时直接显示。"), &a.prefs.DrawAnnotations, a.savePrefs)
		ui.Box(c).Height(1).Margin(0, 12).Background(t.Text.Alpha(0.06))
		dim := !a.prefs.NoDim
		toggleRow(c, "dim", i18n.T("专注模式淡化其他段落"), i18n.T("专注模式（")+a.keyLabel("focus")+i18n.T("）下只突出光标所在的段落。"), &dim, nil)
		if dim == a.prefs.NoDim {
			a.toggleDim()
		}
		ui.Box(c).Height(1).Margin(0, 12).Background(t.Text.Alpha(0.06))
		pair := !a.prefs.NoAutoPair
		toggleRow(c, "auto-pair", i18n.T("括号与引号自动配对"), i18n.T("支持中文符号、包裹选区和成对删除。"), &pair, nil)
		if pair == a.prefs.NoAutoPair {
			a.prefs.NoAutoPair = !pair
			a.savePrefs()
		}
		ui.Box(c).Height(1).Margin(0, 12).Background(t.Text.Alpha(0.06))
		toggleRow(c, "auto-space", i18n.T("中英文之间自动加空格"), i18n.T("输入时在汉字与英文字母、数字之间加一个空格；代码与公式不受影响。"), &a.prefs.AutoSpace, a.savePrefs)
	})
	a.settingsUpdates(c)
}

// settingsAppearance is how pages read: the body's typeface, the width
// of the text column, the size and leading of body text, with a sample
// set as the page sets it.
func (a *App) settingsAppearance(c *ui.Context) {
	t := c.Theme()
	settingsHeading(c, i18n.T("外观"), i18n.T("正文的字体、页面宽度、字号与行距，所有文档共用。代码块保持等宽字体。"))
	sep := func() { ui.Box(c).Height(1).Margin(0, 12).Background(t.Text.Alpha(0.06)) }
	settingsGroup(c, "appearance", func() {
		language := func() {
			at := map[string]int{"": 0, "zh-Hans": 1, "en": 2}[a.prefs.Language]
			ui.Row(c.Key("language")).Padding(10, 12).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, i18n.T("界面语言"))
					ui.Text(c, i18n.T("菜单栏在重新启动后切换。")).FontSize(12).TextColor(t.TextMuted)
				})
				before := at
				ui.Segmented(c.Key("choice"), &at, i18n.T("跟随系统"), "简体中文", "English").Label(i18n.T("界面语言"))
				if at != before {
					a.prefs.Language = []string{"", "zh-Hans", "en"}[at]
					a.savePrefs()
					a.applyLanguage()
				}
			})
		}
		language()
		sep()
		choice := func(key, label string, values []string, names []string, current *string) {
			at := 0
			for i, v := range values {
				if v == *current {
					at = i
				}
			}
			ui.Row(c.Key(key)).Padding(10, 12).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Text(c, label).Grow(1)
				before := at
				ui.Segmented(c.Key("choice"), &at, names...).Label(label)
				if at != before {
					*current = values[at]
					a.savePrefs()
				}
			})
		}
		choice("font", i18n.T("正文字体"), prefs.Fonts, []string{i18n.T("系统"), i18n.T("衬线"), i18n.T("楷体"), i18n.T("等宽")}, &a.prefs.Font)
		sep()
		choice("page", i18n.T("页面宽度"), prefs.Pages, []string{i18n.T("窄"), i18n.T("标准"), i18n.T("宽")}, &a.prefs.Page)
		sep()
		for _, item := range []struct {
			label string
			value float32
			step  float32
			size  bool
		}{{i18n.T("正文字号"), a.prefs.ReadingSize(), 1, true}, {i18n.T("行距"), a.prefs.ReadingLine(), .1, false}} {
			ui.Row(c.Key(item.label)).Padding(10, 12).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, item.label).Grow(1)
				change := float32(0)
				if ui.Button(c, "−").Label(i18n.T("减小") + item.label).Clicked() {
					change = -item.step
				}
				ui.Text(c, fmt.Sprintf("%.2g", item.value)).Width(42).TextAlign(ui.Center)
				if ui.Button(c, "+").Label(i18n.T("增大") + item.label).Clicked() {
					change = item.step
				}
				if change != 0 {
					if item.size {
						a.prefs.FontSize = max(12, min(28, item.value+change))
					} else {
						a.prefs.LineHeight = max(1.2, min(2.2, item.value+change))
					}
					a.savePrefs()
				}
			})
		}
	})
	// The sample, set in the chosen face, size and leading.
	family := a.prefs.BodyFamily()
	if family == "" {
		family = "system-ui"
	}
	ui.Column(c.Key("appearance-sample")).Padding(14, 16).Gap(6).Radius(10).Border(1, t.Text.Alpha(0.08)).Children(func() {
		ui.Text(c, i18n.T("预览")).FontSize(11).TextColor(t.TextMuted)
		size := a.prefs.ReadingSize()
		ui.Text(c, i18n.T("留一点空白")).Font(family).FontSize(size * 1.3).FontWeight(500)
		ui.Text(c, i18n.T("写作可以很简单。打开一页，记下此刻的想法，让文字慢慢成为自己的形状。Quiet words, written slowly.")).
			Font(family).FontSize(size).LineHeight(a.prefs.ReadingLine())
	})
	if ui.Button(c.Key("appearance-reset"), i18n.T("恢复默认外观")).AlignSelf(ui.Start).Clicked() {
		a.prefs.FontSize, a.prefs.LineHeight, a.prefs.Font, a.prefs.Page = 0, 0, "", ""
		a.savePrefs()
	}
}

// toggleRow is a labeled switch; changed runs when it is flipped.
func toggleRow(c *ui.Context, key, label, note string, on *bool, changed func()) {
	t := c.Theme()
	ui.Row(c.Key(key)).Padding(10, 12).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, label)
			ui.Text(c, note).FontSize(12).TextColor(t.TextMuted)
		})
		before := *on
		ui.Switch(c.Key("switch"), on).Label(label)
		if *on != before && changed != nil {
			changed()
		}
	})
}

func (a *App) settingsSpaces(c *ui.Context) {
	t := c.Theme()
	settingsHeading(c, i18n.T("空间"), i18n.T("从 ArcMD 移除空间只删除记录，不会删除文件夹。"))
	settingsGroup(c, "space-list", func() {
		for i, s := range a.state.Spaces {
			if i > 0 {
				ui.Box(c.Key("sep-"+s.ID)).Height(1).Margin(0, 12).Background(t.Text.Alpha(0.06))
			}
			ui.Row(c.Key("space-row-"+s.ID)).Padding(10, 12).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(30, 30).Radius(8).Center().Background(t.Text.Alpha(0.06)).Children(func() {
					spaceGlyph(c, s.Icon, s.Symbol, s.Name, 15, t.Text)
				})
				ui.Column(c).Grow(1).MinWidth(0).Clip().Gap(2).Children(func() {
					ui.Text(c, s.Name).FontWeight(600).SingleLine()
					ui.Text(c, shortPath(s.Root)).FontSize(12).TextColor(t.TextMuted).SingleLine().Tooltip(s.Root)
				})
				if ui.Button(c.Key("edit"), i18n.T("编辑")).Label(i18n.T("编辑 ") + s.Name).Clicked() {
					a.settingsOpen = false
					a.editSpace(s)
				}
				if ui.Button(c.Key("reveal"), i18n.T("显示")).Label(i18n.T("在访达中显示 ") + s.Name).Clicked() {
					revealDir(s.Root)
				}
				if ui.Button(c.Key("remove"), i18n.T("移除")).Label(i18n.T("从 ArcMD 移除 ") + s.Name).Disabled(len(a.state.Spaces) < 2).
					Tooltip(i18n.T("只删除记录，文件夹保留")).Clicked() {
					a.removeSpace(s.ID)
				}
			})
		}
	})
	if ui.Button(c.Key("settings-new-space"), i18n.T("新建空间…")).AlignSelf(ui.Start).Clicked() {
		a.settingsOpen = false
		a.newSpaceForm()
	}
}

// settingsUpdates is the general tab's updates: whether ArcMD downloads
// new versions by itself, and the check of "检查更新…".
func (a *App) settingsUpdates(c *ui.Context) {
	t := c.Theme()
	settingsHeading(c, i18n.T("更新"), "")
	settingsGroup(c, "updates", func() {
		auto := !a.prefs.NoAutoUpdate
		toggleRow(c, "auto-update", i18n.T("自动更新 ArcMD"), i18n.T("在后台下载新版本，重新启动后安装。关闭后只提示有新版本。"), &auto, nil)
		if auto == a.prefs.NoAutoUpdate {
			a.toggleAutoUpdate()
		}
		ui.Box(c).Height(1).Margin(0, 12).Background(t.Text.Alpha(0.06))
		ui.Row(c.Key("check")).Padding(10, 12).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, i18n.T("当前版本 ")+Version)
				ui.Text(c, a.updateSummary()).FontSize(12).TextColor(t.TextMuted)
			})
			if ui.Button(c.Key("check-now"), i18n.T("检查更新…")).Clicked() {
				a.checkForUpdates()
			}
		})
	})
}

func (a *App) settingsAbout(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Gap(6).AlignItems(ui.Center).Padding(28, 0, 8, 0).Children(func() {
		ui.Text(c, "ArcMD").FontSize(28).FontWeight(600)
		ui.Text(c, i18n.T("版本 ")+Version).TextColor(t.TextMuted)
		ui.Text(c, i18n.T("安静地写作，有序地整理。")).TextColor(t.TextMuted).Padding(8, 0, 0, 0)
	})
	ui.Column(c.Key("about-intro")).Gap(20).Padding(8, 12).Children(func() {
		for _, section := range [][2]string{
			{i18n.T("专注写作"), i18n.T("从随手笔记到完整长文，用标题、清单、表格和图片自然表达想法。")},
			{i18n.T("按空间整理"), i18n.T("为工作、生活和项目建立空间，用收藏、置顶和搜索快速找到文档。")},
			{i18n.T("文档留在本地"), i18n.T("内容自动保存在你的文件夹里，支持 Markdown 导入导出，也能导出 PDF。")},
		} {
			ui.Column(c.Key(section[0])).Gap(5).Children(func() {
				ui.Text(c, section[0]).FontSize(14).FontWeight(600)
				ui.Text(c, section[1]).FontSize(13).LineHeight(1.6).TextColor(t.TextMuted)
			})
		}
		ui.Column(c.Key("about-github")).Gap(5).Children(func() {
			ui.Link(c.Key("repository"), i18n.T("GitHub 开源仓库"), "https://github.com/Ciao1019/ArcMD").
				FontSize(13).AlignSelf(ui.Start).Tooltip(i18n.T("在浏览器中打开 GitHub 开源仓库"))
			ui.Text(c, "github.com/Ciao1019/ArcMD").FontSize(12).TextColor(t.TextMuted)
		})
		ui.Row(c.Key("about-tour")).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
				ui.Text(c, i18n.T("新手教程")).FontSize(14).FontWeight(600)
				ui.Text(c, i18n.T("再看一遍空间、主题配色、左右滑动、书签、置顶和收藏的介绍。")).FontSize(13).LineHeight(1.6).TextColor(t.TextMuted)
			})
			if ui.Button(c.Key("replay-tour"), i18n.T("重新观看")).Clicked() {
				a.startTour()
			}
		})
	})
}
