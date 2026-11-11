package app

import (
	"fmt"
	"slices"
	"strings"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/fuzzy"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/update"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// paletteLimit is how many results the command bar shows.
const paletteLimit = 9

// palette is the command bar's state.
type palette struct {
	open      bool
	query     string
	lastQuery string
	index     int
	pane      *splitPane // document picker for one split pane
	// bookmarks lists the bookmarks instead (bookmarks.go); renaming is
	// the one being named while renameOn, newLabel what is typed.
	bookmarks     bool
	renaming      space.Bookmark
	renameOn      bool
	renameFocused bool
	newLabel      string
}

// action is a result of the command bar.
type action struct {
	kind  string // "文档", "最近", "内容", "空间", "命令", "插件"
	title string
	hint  string
	run   func()
	// doc is the document a document's action opens, to leave its text hit
	// out; snippet and match show where a text hit found the query.
	doc     place
	snippet string
	match   [2]int
	// bookmark is the bookmark a bookmark's action goes to; faded marks one
	// whose block is gone.
	bookmark *space.Bookmark
	faded    bool
}

func (a *App) openPalette() {
	a.palette = palette{open: true}
	// Other spaces' folders are read again for their documents' names,
	// and the index takes in what changed since.
	for id := range a.pageTrees {
		if active := a.state.ActiveSpace(); active == nil || id != active.ID {
			delete(a.pageTrees, id)
		}
	}
	a.refreshSearch()
}

// actions lists everything the command bar can do, before filtering.
func (a *App) actions() []action {
	var out []action
	doc := func(id string, n *vault.Node) {
		rel := n.Path
		out = append(out, action{kind: "文档", title: n.Title(), hint: a.docHint(id, rel),
			doc: place{id, rel}, run: func() { a.openAnywhere(id, rel) }})
	}
	active := a.state.ActiveSpace()
	if active != nil {
		for _, n := range a.docs {
			doc(active.ID, n)
		}
	}
	// Then every other space's, so that a document is found wherever it is.
	for _, sp := range a.state.Spaces {
		if active != nil && sp.ID == active.ID {
			continue
		}
		nodes, _ := a.treeOf(sp)
		var walk func([]*vault.Node)
		walk = func(nodes []*vault.Node) {
			for _, n := range nodes {
				if n.Dir {
					walk(n.Children)
				} else {
					doc(sp.ID, n)
				}
			}
		}
		walk(nodes)
	}
	if a.palette.pane != nil {
		return out
	}
	for _, s := range a.state.Spaces {
		if active != nil && s.ID == active.ID {
			continue
		}
		id := s.ID
		out = append(out, action{kind: "空间", title: "切换到 " + s.Name, hint: s.Icon, run: func() { a.switchSpace(id) }})
	}
	cmd := func(title, hint string, run func()) {
		out = append(out, action{kind: "命令", title: title, hint: hint, run: run})
	}
	if a.doc != nil {
		cmd("添加分屏", a.keyLabel("split-add"), a.addSplitPane)
	}
	if a.split != nil {
		cmd("关闭此分屏", a.keyLabel("split-close"), func() { a.closePane(a.split.active) })
		cmd("切换到下一个分屏", a.keyLabel("split-next"), a.cyclePane)
		cmd("切换分屏排列方向", "", func() { a.split.vertical = !a.split.vertical })
		cmd("均分所有分屏", "", a.equalSplit)
		cmd("从分屏中分离", "", a.separateSplit)
	}
	dir := a.currentDir()
	cmd("新建文档", a.keyLabel("new-doc"), func() { a.newDoc(dir) })
	cmd("新建文件夹", a.keyLabel("new-folder"), func() { a.newFolder(dir) })
	cmd("新建空间…", "", a.newSpaceForm)
	if active != nil {
		cmd("编辑主题颜色…", "", func() { a.editSpace(active) })
		cmd("更改空间图标…", "", func() { a.editSpaceProperty(active, "icon") })
		cmd("重命名空间…", "", func() { a.editSpaceProperty(active, "name") })
	}
	cmd("切换侧边栏", a.keyLabel("sidebar"), a.toggleSidebar)
	cmd("资料库", a.keyLabel("library"), a.toggleLibrary)
	cmd("所有空间", a.keyLabel("overview"), a.toggleOverview)
	focus := "进入专注模式"
	if a.focus {
		focus = "退出专注模式"
	}
	cmd(focus, a.keyLabel("focus"), a.toggleFocus)
	if a.doc != nil && a.doc.loadErr == nil && !a.doc.source {
		outline := "展开目录"
		if a.outlineOpen() {
			outline = "收起目录"
		}
		cmd(outline, a.keyLabel("outline"), a.toggleOutline)
	}
	typewriter := "开启打字机滚动"
	if a.prefs.Typewriter {
		typewriter = "关闭打字机滚动"
	}
	cmd(typewriter, "", a.toggleTypewriter)
	dim := "专注模式下不淡化其他段落"
	if a.prefs.NoDim {
		dim = "专注模式下淡化其他段落"
	}
	cmd(dim, "", a.toggleDim)
	cmd("书签…", a.keyLabel("bookmarks"), a.openBookmarks)
	cmd("按标签浏览", "#", func() { a.openPalette(); a.palette.query = "#" })
	cmd("最近删除…", "", a.openTrash)
	if len(a.state.Spaces) > 1 {
		cmd("下一个空间", a.keyLabel("next-space"), func() { a.stepSpace(1) })
		cmd("上一个空间", a.keyLabel("prev-space"), func() { a.stepSpace(-1) })
	}
	cmd("插件…", "", a.openPlugins)
	cmd("设置…", a.keyLabel("settings"), func() { a.openSettings(tabGeneral) })
	cmd("键盘快捷键", "", func() { a.openSettings(tabShortcuts) })
	cmd("关于 ArcMD", "", func() { a.openSettings(tabAbout) })
	cmd("检查更新…", "", a.checkForUpdates)
	if n := a.updateNotice(); n != nil && n.Kind == update.Ready {
		cmd("重新启动以安装更新", "", a.installUpdate)
	}
	auto := "关闭自动更新"
	if a.prefs.NoAutoUpdate {
		auto = "开启自动更新"
	}
	cmd(auto, "", a.toggleAutoUpdate)
	cmd("刷新文档列表", "", a.refreshTree)
	if a.canGo(-1) {
		cmd("后退", "⌘[", func() { a.goHistory(-1) })
	}
	if a.canGo(1) {
		cmd("前进", "⌘]", func() { a.goHistory(1) })
	}
	if a.doc != nil {
		rel := a.doc.rel
		sp := a.doc.space
		fav := "添加到收藏"
		if a.state.IsFavorite(sp.ID, rel) {
			fav = "从收藏中移除"
		}
		cmd(fav, "", func() { a.toggleFavorite(sp, rel) })
		pin := "置顶文档"
		if sp.IsPinned(rel) {
			pin = "取消置顶文档"
		}
		cmd(pin, "", func() { a.togglePin(sp, rel) })
		if d := a.bookmarkable(); d != nil {
			mark := "添加书签"
			if a.caretMarked(d) {
				mark = "移除书签"
			}
			cmd(mark, a.keyLabel("bookmark"), a.toggleBookmarkAtCaret)
			cmd("上一个书签", a.keyLabel("prev-bookmark"), func() { a.stepBookmark(-1) })
			cmd("下一个书签", a.keyLabel("next-bookmark"), func() { a.stepBookmark(1) })
		}
		if inPins(sp, rel) {
			cmd("关闭并保持置顶", "", func() { a.closeOpen(sp, rel) })
		}
		switch {
		case a.doc.conflict:
			cmd("保留我的修改", "", a.keepMine)
			cmd("使用磁盘上的版本", "⌘R", a.useDisk)
		case a.doc.missing:
			cmd("重新保存到原位置", "", a.keepMine)
			cmd("放弃修改并关闭", "", a.useDisk)
		}
		mode := "预览 Markdown 导出"
		if a.doc.source {
			mode = "返回富文本编辑"
		}
		cmd(mode, a.keyLabel("source"), a.toggleSource)
		cmd("版本历史…", a.keyLabel("history"), a.openVersions)
		cmd("导出 PDF", a.keyLabel("export-pdf"), a.exportPDF)
		cmd("打印…", "", a.printDoc)
		cmd("重命名文档", "", func() { a.renameIn(sp, rel) })
		if a.doc.loadErr == nil && !a.doc.source {
			a.editorActions(cmd)
		}
		cmd("导出 Markdown 文件", "", a.exportMarkdown)
		cmd("在访达中显示文档", "", func() { a.revealIn(sp, rel) })
	}
	for _, p := range a.installed {
		if !p.Enabled {
			continue
		}
		for _, pc := range p.Manifest.Contributes.Commands {
			name := p.Manifest.Name
			out = append(out, action{kind: "插件", title: pc.Title, hint: name, run: func() {
				a.notify("%s 需要插件运行时支持，该功能正在开发中。", name)
			}})
		}
	}
	return out
}

// results filters and ranks the actions by the query. With no query, the
// documents last shown come first, then commands. With one, names match
// fuzzily, active space first among equals, and below them come the
// documents whose text holds the query.
func (a *App) results() []action {
	q := a.palette.query
	if a.palette.bookmarks {
		return a.bookmarkResults(q)
	}
	if strings.TrimSpace(q) == "" {
		return a.idleResults()
	}
	if strings.HasPrefix(strings.TrimSpace(q), "#") {
		return a.tagResults(q)
	}
	type scored struct {
		action
		score int
	}
	var ms []scored
	for _, act := range a.actions() {
		s, ok := fuzzy.Score(q, act.title)
		if !ok {
			continue
		}
		ms = append(ms, scored{act, s})
	}
	slices.SortStableFunc(ms, func(x, y scored) int { return y.score - x.score })
	hits := a.textHits(q)
	limit := paletteLimit
	if len(hits) > 0 {
		limit = paletteLimit - min(len(hits), 3) // room for a few text hits
	}
	out := make([]action, 0, paletteLimit+searchLimit)
	named := map[place]bool{}
	for _, m := range ms[:min(len(ms), limit)] {
		out = append(out, m.action)
		if m.kind == "文档" {
			named[m.doc] = true
		}
	}
	query := q
	for _, h := range hits {
		p := place{h.Space, h.Path}
		if named[p] {
			continue // already listed by its name
		}
		hint := a.docHint(h.Space, h.Path)
		if h.Count > 1 {
			hint = strings.TrimPrefix(hint+fmt.Sprintf(" · %d 处", h.Count), " · ")
		}
		id, rel := h.Space, h.Path
		out = append(out, action{kind: "内容", title: vault.Title(rel), hint: hint, doc: p,
			snippet: h.Snippet, match: h.Match, run: func() { a.openAtText(id, rel, query) }})
	}
	return out
}

// recentLimit is how many documents last shown the idle bar lists.
const recentLimit = 4

// idleResults are the bar's results before anything is typed: the
// documents last shown, then commands, then the rest of the documents.
func (a *App) idleResults() []action {
	var out []action
	for _, p := range a.recentPlaces(recentLimit) {
		id, rel := p.space, p.rel
		out = append(out, action{kind: "最近", title: vault.Title(rel), hint: a.docHint(id, rel), doc: p,
			run: func() { a.openAnywhere(id, rel) }})
	}
	all := a.actions()
	for _, act := range all {
		if act.kind == "命令" {
			out = append(out, act)
		}
	}
	for _, act := range all {
		if act.kind != "命令" {
			out = append(out, act)
		}
	}
	return out[:min(len(out), paletteLimit)]
}

// paletteView is the command bar, over the dimmed window.
func (a *App) paletteView(c *ui.Context) {
	p := &a.palette
	if !p.open {
		return
	}
	t := c.Theme()
	winW, winH := c.Size()
	width := min(float32(560), winW-80)
	var bounds ui.Rect
	if p.pane != nil && p.pane.bounds.W > 0 {
		bounds = p.pane.bounds
		width = min(width, max(float32(280), bounds.W-64))
	}
	panel := ui.Modal(c.Key("palette"), &p.open, func() {
		res := a.results()
		if p.query != p.lastQuery {
			p.lastQuery, p.index = p.query, 0
		}
		if c.Shortcut(0, ui.KeyDown) {
			p.index++
		}
		if c.Shortcut(0, ui.KeyUp) {
			p.index--
		}
		p.index = max(0, min(p.index, len(res)-1))
		run := func(act action) {
			a.runPaletteAction(act)
		}
		placeholder := "搜索文档、内容或执行命令"
		if p.pane != nil {
			placeholder = "选择分屏文档"
		}
		if p.bookmarks {
			placeholder = "搜索书签"
		}
		ui.Column(c).Width(width).Gap(6).Children(func() {
			input := ui.TextInput(c.Key("query"), &p.query).Placeholder(placeholder).
				AutoFocus().Label("命令栏").FontSize(16).Padding(10, 12)
			if input.Submitted() && len(res) > 0 {
				run(res[p.index])
				return
			}
			if len(res) == 0 {
				empty := "没有匹配的结果"
				switch {
				case p.bookmarks && len(a.state.Bookmarks) == 0:
					empty = "还没有书签 · 按 " + a.firstKey("bookmark") + " 标记当前段落"
				case p.bookmarks:
					empty = "没有匹配的书签"
				}
				ui.Text(c, empty).TextColor(t.TextMuted).Padding(10, 12)
				return
			}
			ui.Scroll(c.Key("palette-results")).MaxHeight(max(80, winH-190)).MinHeight(0).Children(func() {
				for i, act := range res {
					row := ui.Row(c.Key(i)).Gap(10).Padding(7, 12).Radius(7).AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(act.title)
					if i == p.index {
						row.Background(t.Accent.Alpha(0.16))
					} else if row.Hovered() {
						row.Background(t.Text.Alpha(0.06))
					}
					if act.faded {
						row.Opacity(.55)
					}
					if act.bookmark != nil && a.bookmarkRow(c, row, act) {
						continue
					}
					row.Children(func() {
						ui.Text(c, act.kind).FontSize(11).TextColor(t.TextMuted).Width(64).NoWrap()
						if act.snippet == "" {
							ui.Text(c, act.title).Grow(1).SingleLine().MinWidth(0)
							if act.hint != "" {
								ui.Text(c, act.hint).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
							}
							return
						}
						// A text hit: the name over the line it was found on, the
						// match marked as the find box marks it.
						ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
							ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
								ui.Text(c, act.title).Grow(1).SingleLine().MinWidth(0)
								if act.hint != "" {
									ui.Text(c, act.hint).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
								}
							})
							s, m := act.snippet, act.match
							ui.RichText(c,
								ui.Span{Text: s[:m[0]]},
								ui.Span{Text: s[m[0]:m[1]], Color: t.Text, Weight: 600, Background: searchMark(t), Radius: 3},
								ui.Span{Text: s[m[1]:]},
							).FontSize(12).TextColor(t.TextMuted).SingleLine().MinWidth(0)
						})
					})
					if row.Clicked() {
						run(act)
						return
					}
				}
			})
		})
	})
	if bounds.W > 0 {
		panel.Absolute().Left(min(max(20, bounds.X+(bounds.W-width-40)/2), winW-width-60)).Top(max(24, min(bounds.Y+46, winH-260)))
	}
}

// editorActions exposes Markdown operations without permanent formatting UI.
func (a *App) editorActions(cmd func(string, string, func())) {
	ed := a.doc.ed
	after := func(run func()) func() {
		return func() { run(); ed.RequestFocus = true }
	}
	lock := "锁定文档（只读）"
	if ed.ReadOnly {
		lock = "解锁文档"
	}
	cmd(lock, "", func() { ed.SetReadOnly(!ed.ReadOnly) })
	cmd("阅读设置…", "", func() { a.openSettings(tabGeneral) })
	cmd("跳转脚注 / 返回正文", "", func() { ed.FollowFootnote() })
	cmd("查找", a.keyLabel("find"), func() { ed.OpenFind(false) })
	if ed.ReadOnly {
		return
	}
	cmd("查找和替换", a.keyLabel("replace"), func() { ed.OpenFind(true) })
	cmd("插入双链…", "[[", after(ed.StartWikiLink))
	cmd("插入公式", "", after(ed.InsertMath))
	cmd("插入行内公式", "", after(ed.InsertInlineMath))
	cmd("插入脚注", "", after(ed.InsertFootnote))
	cmd("上标", "", after(func() { ed.SetScript("sup") }))
	cmd("下标", "", after(func() { ed.SetScript("sub") }))
	if ed.Doc.Icon == "" {
		cmd("添加页面图标", "", ed.EditPageIcon)
	} else {
		cmd("更换页面图标", "", ed.EditPageIcon)
		cmd("移除页面图标", "", after(func() { ed.Engine.SetPageIcon("") }))
	}
	if ed.Doc.Cover == "" {
		cmd("添加封面", "", ed.EditCover)
	} else {
		cmd("更换封面", "", ed.EditCover)
		cmd("移除封面", "", after(func() { ed.Engine.SetCover("") }))
	}
	cmd("章节上移", "", after(func() { ed.MoveSectionAtCaret(-1) }))
	cmd("章节下移", "", after(func() { ed.MoveSectionAtCaret(1) }))
	cmd("折叠/展开当前块", "", after(ed.ToggleFoldAtCaret))
	cmd("全部展开", "", after(ed.UnfoldAll))
	cmd("合并选中的单元格", "", after(ed.MergeSelectedCells))
	cmd("拆分单元格", "", after(ed.SplitCaretCell))
	cmd("撤销", "⌘Z", after(ed.Undo))
	cmd("重做", "⇧⌘Z", after(ed.Redo))
	cmd("加粗", a.keyLabel("bold"), after(func() { ed.Wrap("**") }))
	cmd("斜体", a.keyLabel("italic"), after(func() { ed.Wrap("*") }))
	cmd("删除线", a.keyLabel("strike"), after(func() { ed.Wrap("~~") }))
	cmd("行内代码", a.keyLabel("code"), after(func() { ed.Wrap("`") }))
	for _, item := range []struct{ name, prefix string }{
		{"正文", ""}, {"一级标题", "# "}, {"二级标题", "## "}, {"三级标题", "### "},
		{"四级标题", "#### "}, {"五级标题", "##### "}, {"六级标题", "###### "},
		{"引用", "> "}, {"无序列表", "- "}, {"有序列表", "1. "}, {"任务列表", "- [ ] "},
	} {
		cmd("转换为"+item.name, "", after(func() { ed.Prefix(item.prefix) }))
	}
	cmd("在下方新建段落", a.keyLabel("newline"), after(func() { ed.Engine.Insert(model.NewBlock("paragraph", "")) }))
	cmd("插入链接…", "", ed.EditLink)
	cmd("下划线", a.keyLabel("underline"), after(func() { ed.Wrap("_") }))
	cmd("插入表格", "", after(func() { ed.Engine.Insert(model.Table(3, 3)) }))
	cmd("插入双栏", "", after(func() { ed.Engine.Insert(model.Columns()) }))
	for _, item := range []struct{ name, kind string }{{"插入代码块", "code"}, {"插入高亮块", "callout"}, {"插入分隔线", "rule"}} {
		cmd(item.name, "", after(func() { ed.Engine.Insert(model.NewBlock(item.kind, "")) }))
	}
	for _, item := range []struct{ name, align string }{{"左对齐", "left"}, {"居中对齐", "center"}, {"右对齐", "right"}} {
		cmd(item.name, "", after(func() { ed.Engine.Align(item.align) }))
	}
	for _, tone := range model.CalloutColors {
		cmd("高亮块颜色："+editor.CalloutName(tone), "", after(func() { ed.Engine.SetColor("", tone) }))
	}
	for _, name := range model.TextColors {
		cmd("文字颜色："+editor.ColorName(name, false), "", after(func() { ed.Engine.SetTextColor(&name, nil) }))
	}
	for _, name := range model.TextHighlights[:8] {
		cmd("背景颜色："+editor.ColorName(name, true), "", after(func() { ed.Engine.SetTextColor(nil, &name) }))
	}
	cmd("清除文字颜色", "", after(func() { none := ""; ed.Engine.SetTextColor(&none, &none) }))
	cmd("增加表格行", "", after(func() { ed.Engine.ResizeTable(true, 1) }))
	cmd("增加表格列", "", after(func() { ed.Engine.ResizeTable(false, 1) }))
	cmd("删除表格末行", "", after(func() { ed.Engine.ResizeTable(true, -1) }))
	cmd("删除表格末列", "", after(func() { ed.Engine.ResizeTable(false, -1) }))
	for _, item := range []struct {
		name  string
		delta int
	}{{"上移内容块", -1}, {"下移内容块", 1}} {
		cmd(item.name, "", after(func() {
			id := ed.Engine.LeafAt(ed.Caret).Owner
			for i, b := range ed.Doc.Blocks {
				if b.ID == id {
					ed.Engine.MoveBlock(id, i+item.delta)
					break
				}
			}
		}))
	}
	cmd("复制内容块", "", after(func() { ed.Engine.Duplicate(ed.Engine.LeafAt(ed.Caret).Owner) }))
	cmd("删除内容块", "", after(func() { ed.Engine.DeleteBlock(ed.Engine.LeafAt(ed.Caret).Owner) }))
	cmd("增加缩进", "", after(func() { ed.Engine.Indent(1) }))
	cmd("减少缩进", "", after(func() { ed.Engine.Indent(-1) }))
	cmd("插入图片…", "", a.pickImages)
}
