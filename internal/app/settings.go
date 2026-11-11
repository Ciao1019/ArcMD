package app

import (
	"fmt"
	"os"

	"github.com/egoist/mygo/ui"
)

// Settings tabs.
const (
	tabGeneral   = "general"
	tabSpaces    = "spaces"
	tabShortcuts = "shortcuts"
	tabAbout     = "about"
)

// openSettings shows the settings on a tab.
func (a *App) openSettings(tab string) {
	a.settingsOpen = true
	a.settingsTab = tab
}

// moreMenu keeps document actions at hand. Library and settings lead to
// app-wide tools without repeating their individual pages here.
func (a *App) moreMenu(c *ui.Context) {
	b := iconButton(c, "more", iconEllipsis, "更多", "更多", false).Role(ui.RoleMenuButton)
	b.PopoverMenuButton(func(m *ui.Menu) {
		noDoc := a.doc == nil || a.doc.loadErr != nil
		if a.menuKey(m.Item("专注模式"), "focus").Checked(a.focus).Chosen() {
			a.toggleFocus()
		}
		if a.menuKey(m.Item("目录"), "outline").Checked(a.outlineOpen()).Disabled(noDoc || a.doc.source).Chosen() {
			a.toggleOutline()
		}
		mode := "预览 Markdown 导出"
		if a.doc != nil && a.doc.source {
			mode = "返回富文本编辑"
		}
		if a.menuKey(m.Item(mode), "source").Disabled(noDoc).Chosen() {
			a.toggleSource()
		}
		m.Separator()
		if a.menuKey(m.Item("版本历史…"), "history").Disabled(noDoc).Chosen() {
			a.openVersions()
		}
		m.Submenu("导出", func(sub *ui.Menu) {
			if sub.Item("导出 Markdown 文件").Disabled(noDoc).Chosen() {
				a.exportMarkdown()
			}
			if a.menuKey(sub.Item("导出 PDF"), "export-pdf").Disabled(noDoc).Chosen() {
				a.exportPDF()
			}
		}).Disabled(noDoc)
		if m.Item("打印…").Disabled(noDoc).Chosen() {
			a.printDoc()
		}
		if m.Item("在访达中显示文档").Disabled(noDoc).Chosen() {
			a.revealIn(a.doc.space, a.doc.rel)
		}
		m.Separator()
		if a.menuKey(m.Item("资料库"), "library").Chosen() {
			a.toggleLibrary()
		}
		if a.menuKey(m.Item("设置…"), "settings").Chosen() {
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
			ui.Column(c.Key("settings-tabs")).Width(156).Gap(2).Padding(4, 12, 4, 0).Role(ui.RoleTabList).Label("设置").
				FocusGroup(ui.Vertical).Children(func() {
				ui.Text(c, "设置").FontSize(16).FontWeight(600).Padding(4, 10, 12, 10)
				for _, tab := range []struct {
					id, label string
					icon      string
				}{
					{tabGeneral, "通用", "settings"},
					{tabSpaces, "空间", "layers"},
					{tabShortcuts, "快捷键", "keyboard"},
					{tabAbout, "关于", "info"},
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
				case tabSpaces:
					a.settingsSpaces(c)
				case tabShortcuts:
					a.settingsShortcuts(c)
				case tabAbout:
					settingsAbout(c)
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
		if ui.Button(c.Key("open"), "在访达中显示").Clicked() {
			os.MkdirAll(dir, 0o755)
			revealDir(dir)
		}
	})
}

func (a *App) settingsGeneral(c *ui.Context) {
	t := c.Theme()
	settingsHeading(c, "通用", "ArcMD 把文档保存在你的文件夹里，设置与空间列表保存在配置文件夹。")
	settingsGroup(c, "places", func() {
		folderRow(c, "documents", "新空间的默认位置", a.cfg.DocumentsDir)
		ui.Box(c).Height(1).Margin(0, 12).Background(t.Text.Alpha(0.06))
		folderRow(c, "config", "配置文件夹（spaces.json、插件）", a.cfg.ConfigDir)
	})
	ui.Text(c, "颜色、外观与图标按空间设置：在侧边栏的空间名称上右键，选择要修改的属性。").
		FontSize(12).TextColor(t.TextMuted)
	settingsHeading(c, "写作", "")
	settingsGroup(c, "writing", func() {
		toggleRow(c, "typewriter", "打字机滚动", "正在写的一行始终停在页面中间。", &a.prefs.Typewriter, a.savePrefs)
		ui.Box(c).Height(1).Margin(0, 12).Background(t.Text.Alpha(0.06))
		dim := !a.prefs.NoDim
		toggleRow(c, "dim", "专注模式淡化其他段落", "专注模式（"+a.keyLabel("focus")+"）下只突出光标所在的段落。", &dim, nil)
		if dim == a.prefs.NoDim {
			a.toggleDim()
		}
	})
	settingsHeading(c, "阅读与输入", "")
	settingsGroup(c, "reading", func() {
		for _, item := range []struct {
			label string
			value float32
			step  float32
			size  bool
		}{{"正文字号", a.prefs.ReadingSize(), 1, true}, {"行距", a.prefs.ReadingLine(), .1, false}} {
			ui.Row(c.Key(item.label)).Padding(10, 12).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, item.label).Grow(1)
				change := float32(0)
				if ui.Button(c, "−").Label("减小" + item.label).Clicked() {
					change = -item.step
				}
				ui.Text(c, fmt.Sprintf("%.2g", item.value)).Width(42)
				if ui.Button(c, "+").Label("增大" + item.label).Clicked() {
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
		pair := !a.prefs.NoAutoPair
		toggleRow(c, "auto-pair", "括号与引号自动配对", "支持中文符号、包裹选区和成对删除。", &pair, nil)
		if pair == a.prefs.NoAutoPair {
			a.prefs.NoAutoPair = !pair
			a.savePrefs()
		}
		if ui.Button(c, "恢复阅读默认值").Margin(8, 12).Clicked() {
			a.prefs.FontSize, a.prefs.LineHeight = 0, 0
			a.savePrefs()
		}
	})
	a.settingsUpdates(c)
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
	settingsHeading(c, "空间", "从 ArcMD 移除空间只删除记录，不会删除文件夹。")
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
				if ui.Button(c.Key("edit"), "编辑").Label("编辑 " + s.Name).Clicked() {
					a.settingsOpen = false
					a.editSpace(s)
				}
				if ui.Button(c.Key("reveal"), "显示").Label("在访达中显示 " + s.Name).Clicked() {
					revealDir(s.Root)
				}
				if ui.Button(c.Key("remove"), "移除").Label("从 ArcMD 移除 " + s.Name).Disabled(len(a.state.Spaces) < 2).
					Tooltip("只删除记录，文件夹保留").Clicked() {
					a.removeSpace(s.ID)
				}
			})
		}
	})
	if ui.Button(c.Key("settings-new-space"), "新建空间…").AlignSelf(ui.Start).Clicked() {
		a.settingsOpen = false
		a.newSpaceForm()
	}
}

// settingsUpdates is the general tab's updates: whether ArcMD downloads
// new versions by itself, and the check of "检查更新…".
func (a *App) settingsUpdates(c *ui.Context) {
	t := c.Theme()
	settingsHeading(c, "更新", "")
	settingsGroup(c, "updates", func() {
		auto := !a.prefs.NoAutoUpdate
		toggleRow(c, "auto-update", "自动更新 ArcMD", "在后台下载新版本，重新启动后安装。关闭后只提示有新版本。", &auto, nil)
		if auto == a.prefs.NoAutoUpdate {
			a.toggleAutoUpdate()
		}
		ui.Box(c).Height(1).Margin(0, 12).Background(t.Text.Alpha(0.06))
		ui.Row(c.Key("check")).Padding(10, 12).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, "当前版本 "+Version)
				ui.Text(c, a.updateSummary()).FontSize(12).TextColor(t.TextMuted)
			})
			if ui.Button(c.Key("check-now"), "检查更新…").Clicked() {
				a.checkForUpdates()
			}
		})
	})
}

func settingsAbout(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Gap(6).AlignItems(ui.Center).Padding(28, 0, 8, 0).Children(func() {
		ui.Text(c, "ArcMD").FontSize(28).FontWeight(600)
		ui.Text(c, "版本 "+Version).TextColor(t.TextMuted)
		ui.Text(c, "安静地写作，有序地整理。").TextColor(t.TextMuted).Padding(8, 0, 0, 0)
	})
	ui.Column(c.Key("about-intro")).Gap(20).Padding(8, 12).Children(func() {
		for _, section := range [][2]string{
			{"专注写作", "从随手笔记到完整长文，用标题、清单、表格和图片自然表达想法。"},
			{"按空间整理", "为工作、生活和项目建立空间，用收藏、置顶和搜索快速找到文档。"},
			{"文档留在本地", "内容自动保存在你的文件夹里，支持 Markdown 导入导出，也能导出 PDF。"},
		} {
			ui.Column(c.Key(section[0])).Gap(5).Children(func() {
				ui.Text(c, section[0]).FontSize(14).FontWeight(600)
				ui.Text(c, section[1]).FontSize(13).LineHeight(1.6).TextColor(t.TextMuted)
			})
		}
		ui.Column(c.Key("about-github")).Gap(5).Children(func() {
			ui.Link(c.Key("repository"), "GitHub 开源仓库", "https://github.com/Ciao1019/ArcMD").
				FontSize(13).AlignSelf(ui.Start).Tooltip("在浏览器中打开 GitHub 开源仓库")
			ui.Text(c, "github.com/Ciao1019/ArcMD").FontSize(12).TextColor(t.TextMuted)
		})
	})
}
