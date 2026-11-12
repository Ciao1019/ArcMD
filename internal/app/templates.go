package app

// Templates and the daily note (internal/templates): a space's templates
// are documents in its hidden .arcmd/templates folder, which the command
// bar and the folder menus make new documents from; the daily note is
// today's document in the space's 日记 folder, opened or made by ⌥⌘T.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/templates"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// today is the clock the daily note and templates read; tests set it.
var today = time.Now

// spaceTemplates lists the templates of sp, telling the user when the
// folder cannot be read.
func (a *App) spaceTemplates(sp *space.Space) []templates.Template {
	if sp == nil {
		return nil
	}
	list, err := templates.List(sp.Root)
	if err != nil {
		a.notify("读取模板失败。%v", err)
	}
	return list
}

// readTemplate reads and decodes a template of sp.
func readTemplate(sp *space.Space, t templates.Template) (*model.Document, error) {
	text, err := vault.Read(sp.Root, t.Path)
	if err != nil {
		return nil, err
	}
	return model.Decode(text)
}

// newFromTemplate makes a document from the template t in the folder dir
// of the active space and opens it, the caret at the end of its title. The
// file is untitled until its title names it, as a new document's is.
func (a *App) newFromTemplate(dir string, t templates.Template) {
	sp := a.state.ActiveSpace()
	if sp == nil || !a.flush() {
		return
	}
	tmpl, err := readTemplate(sp, t)
	if err != nil {
		a.notify("模板「%s」无法读取。%v", t.Name, err)
		return
	}
	doc := templates.Instantiate(tmpl, today(), "")
	text, err := doc.Encode()
	if err != nil {
		a.notify("模板「%s」无法使用。%v", t.Name, err)
		return
	}
	rel, err := vault.CreateFile(sp.Root, dir, untitled, ".arcmd", text)
	if err != nil {
		a.notify("从模板新建文档失败。%v", err)
		return
	}
	if dir != "" {
		sp.SetExpanded(dir, true)
	}
	a.refreshTree()
	a.open(rel)
	if d := a.doc; d != nil && d.rel == rel && d.loadErr == nil {
		leaves := d.ed.Doc.Leaves()
		d.ed.SetSelection(leaves[0].End, leaves[0].End)
		d.ed.RequestFocus = true
	}
}

// saveAsTemplate keeps a copy of the open document as a template of its
// space, named by its title.
func (a *App) saveAsTemplate() {
	d := a.doc
	if d == nil || d.loadErr != nil {
		return
	}
	doc := d.ed.Committed().Clone()
	doc.ImportSource = ""
	text, err := doc.Encode()
	if err == nil {
		name := docTitle(doc)
		if name == "" {
			name = vault.Title(d.rel)
		}
		var rel string
		if rel, err = vault.CreateFile(d.space.Root, templates.Dir, name, ".arcmd", text); err == nil {
			a.notify("已存为模板「%s」", vault.Title(rel))
			return
		}
	}
	a.notify("存为模板失败。%v", err)
}

// openTemplatesFolder shows the active space's templates folder in the
// file manager, making it first.
func (a *App) openTemplatesFolder() {
	sp := a.state.ActiveSpace()
	if sp == nil {
		return
	}
	dir := filepath.Join(sp.Root, filepath.FromSlash(templates.Dir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.notify("无法创建模板文件夹。%v", err)
		return
	}
	mygo.Shell.ShowItemInFolder(dir)
}

// openDaily opens today's note in the active space, making it — from the
// space's 日记 template when it has one — the first time.
func (a *App) openDaily() {
	sp := a.state.ActiveSpace()
	if sp == nil || !a.flush() {
		return
	}
	now := today()
	templates.English = i18n.English()
	rel := templates.PathFor(now)
	full, err := vault.Resolve(sp.Root, rel)
	if err != nil {
		a.notify("打开今日笔记失败。%v", err)
		return
	}
	if _, err := os.Stat(full); errors.Is(err, fs.ErrNotExist) {
		var tmpl *model.Document
		if t, ok := templates.Find(a.spaceTemplates(sp), templates.DailyTemplate); ok {
			if tmpl, err = readTemplate(sp, t); err != nil {
				a.notify("模板「%s」无法读取，今日笔记从空白开始。%v", t.Name, err)
				tmpl = nil
			}
		}
		text, err := templates.Daily(tmpl, now).Encode()
		if err == nil {
			rel, err = vault.CreateFile(sp.Root, templates.Folder(), templates.DailyName(now), ".arcmd", text)
		}
		if err != nil {
			a.notify("新建今日笔记失败。%v", err)
			return
		}
	} else if err != nil {
		a.notify("打开今日笔记失败。%v", err)
		return
	}
	sp.SetExpanded(templates.Folder(), true)
	a.refreshTree()
	a.open(rel)
	if d := a.doc; d != nil && d.rel == rel && d.loadErr == nil {
		end := d.ed.Len()
		d.ed.SetSelection(end, end)
		d.ed.RequestFocus = true
	}
}

// templateMenu adds 从模板新建 ▸ to a menu: the active space's templates,
// to make a document in dir, then where to keep them.
func (a *App) templateMenu(m *ui.Menu, dir string) {
	list := a.spaceTemplates(a.state.ActiveSpace())
	m.Submenu(i18n.T("从模板新建"), func(sub *ui.Menu) {
		for _, t := range list {
			if sub.Item(t.Name).Chosen() {
				a.newFromTemplate(dir, t)
			}
		}
		if len(list) > 0 {
			sub.Separator()
		}
		if sub.Item(i18n.T("打开模板文件夹")).Chosen() {
			a.openTemplatesFolder()
		}
	})
}

// starterName is the page a new, empty space starts with.
const starterName = "从这里开始.arcmd"

// starterFile is its name in the language shown.
func starterFile() string {
	if i18n.English() {
		return "Start here.arcmd"
	}
	return starterName
}

// starter is that page: the ways of writing worth knowing first, each
// tried where it is said.
const starter = `# 从这里开始

这个空间还没有其他文档。下面是几种常用的写法，可以直接在这一页试试。

- 输入「/」插入标题、列表、表格、折叠块和公式
- 输入「[[」链接到其他文档，被链接的文档底部会列出反向链接
- 输入「:」加英文名插入表情，例如 :tada:；输入「@」插入日期
- 按 ⌘D 给当前段落加书签，⌃⌘↑ / ⌃⌘↓ 在书签之间跳转
- 按 Esc 选中整块，再按 ⇧↑ / ⇧↓ 选中多块，可以一起移动、转换或删除
- 按 ⌥⌘T 打开今日笔记，按 ⌘K 打开命令栏找到所有功能

<details>
<summary>整理文档的小技巧</summary>

把文档拖到收藏或置顶区，常用的就在手边；文件夹右键可以按名称或修改时间排序，也可以从模板新建文档。

</details>

> [!TIP]
> 读完可以删掉这一页，它只是一个示例。
`

// starterEnglish is the starter page for an interface in English.
const starterEnglish = `# Start here

This space has no other documents yet. Here are the ways of writing worth knowing first; try them on this page.

- Type “/” to insert headings, lists, tables, toggles and formulas
- Type “[[” to link to another document; the linked one lists its backlinks at the bottom
- Type “:” and an English name for emoji, as :tada:; type “@” for a date
- Press ⌘D to bookmark the paragraph, ⌃⌘↑ / ⌃⌘↓ to go between bookmarks
- Press Esc to select the block, then ⇧↑ / ⇧↓ for more, to move, turn or delete them together
- Press ⌥⌘T for today's note, and ⌘K for the command bar, which has everything

<details>
<summary>Keeping documents in order</summary>

Drag documents into favorites or the pinned section to keep them at hand; right-click a folder to sort it by name or date, or to make a document from a template.

</details>

> [!TIP]
> Delete this page once you've read it; it is only an example.
`

// writeStarter writes the starter page at rel of the folder root.
func writeStarter(root, rel string) error {
	text := starter
	if i18n.English() {
		text = starterEnglish
	}
	doc := editor.ImportMarkdown(text)
	doc.ImportSource = ""
	text, err := doc.Encode()
	if err != nil {
		return err
	}
	return vault.Write(root, rel, text)
}
