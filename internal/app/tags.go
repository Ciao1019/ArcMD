package app

// Renaming a #tag in every document (the command bar's tag results, a
// tag chip's menu): the tag and those under it take the new name, in
// prose only; a name another tag has merges the two. The open documents
// change through their editors, the others on disk, and a toast offers to
// put them back, as the find and replace across documents does.

import (
	"fmt"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/Ciao1019/ArcMD/internal/wiki"
	"github.com/egoist/mygo/ui"
)

// tagRename is the window renaming a tag.
type tagRename struct {
	open     bool
	from, to string
}

// openTagRename shows the window for the tag from.
func (a *App) openTagRename(from string) {
	a.palette.open = false
	a.tagRename = tagRename{open: true, from: from, to: from}
}

// tagCount is how many documents hold the tag name.
func (a *App) tagCount(name string) int {
	for _, tc := range a.liveIndex().Tags(nil) {
		if strings.EqualFold(tc.Name, name) {
			return tc.Count
		}
	}
	return 0
}

// renameTag renames the tag from to to in every space's documents.
func (a *App) renameTag(from, to string) {
	from, to = strings.TrimPrefix(strings.TrimSpace(from), "#"), strings.TrimPrefix(strings.TrimSpace(to), "#")
	if from == "" || !wiki.ValidTag(to) || from == to {
		return
	}
	change := func(s string) string {
		out, _ := wiki.RenameTag(s, from, to)
		return out
	}
	merging := !strings.EqualFold(from, to) && a.tagCount(to) > 0
	resident := map[place]*document{}
	var edited []*document
	skipped := 0
	for _, d := range a.residentDocuments() {
		resident[place{d.space.ID, d.rel}] = d
		if d.loadErr != nil {
			continue
		}
		if d.isPDF() {
			continue
		}
		if d.isWhiteboard() {
			if d.boardReadOnly {
				if strings.Contains(strings.ToLower(d.board.Scene.PlainText()), "#"+strings.ToLower(from)) {
					skipped++
				}
				continue
			}
			if d.board.RewriteTags(from, to) {
				edited = append(edited, d)
			}
			continue
		}
		if d.ed.ReadOnly {
			if strings.Contains(strings.ToLower(d.ed.Doc.PlainText()), "#"+strings.ToLower(from)) {
				skipped++
			}
			continue
		}
		if d.ed.Engine.MapProse(change) {
			edited = append(edited, d)
		}
	}
	var files []replacedFile
	var places []place
	seenPlace := map[place]bool{}
	a.eachDoc(func(sp *space.Space, n *vault.Node) bool {
		p := place{sp.ID, n.Path}
		seenPlace[p] = true
		places = append(places, p)
		return true
	})
	for _, sp := range a.state.Spaces {
		if sp.IsExternal() {
			continue
		}
		if docs, err := vault.List(sp.Root); err == nil {
			for _, d := range docs {
				p := place{sp.ID, d.Path}
				if !seenPlace[p] {
					seenPlace[p] = true
					places = append(places, p)
				}
			}
		}
	}
	for _, p := range places {
		if resident[p] != nil || vault.IsPDF(p.rel) {
			continue
		}
		sp := a.state.Get(p.space)
		before, err := readFile(sp, p.rel)
		if err != nil || (!vault.IsWhiteboard(p.rel) && !strings.Contains(strings.ToLower(before), "#"+strings.ToLower(from))) {
			continue
		}
		d := openDocument(sp, p.rel, before, false)
		if d.loadErr != nil {
			continue
		}
		var ok bool
		if d.isPDF() {
			continue
		}
		if d.isWhiteboard() {
			ok = d.board.RewriteTags(from, to)
		} else {
			ok = d.ed.Engine.MapProse(change)
		}
		if !ok {
			continue
		}
		if err := d.save(); err != nil {
			a.notify("更新 %s 的标签失败。%v", p.rel, err)
			continue
		}
		after, _ := readFile(sp, d.rel)
		files = append(files, replacedFile{sp.ID, d.rel, before, after})
	}
	a.refreshSearch()
	n := len(edited) + len(files)
	if n == 0 {
		a.notify("没有文档使用 #%s。", from)
		return
	}
	verb := i18n.T("已重命名")
	if merging {
		verb = i18n.T("已合并到")
	}
	note := ""
	if skipped > 0 {
		note = fmt.Sprintf(i18n.T("，跳过 %d 篇只读文档"), skipped)
	}
	a.offerUndo(fmt.Sprintf(i18n.T("%s #%s，共 %d 篇文档%s"), verb, to, n, note), "#"+from+" → #"+to, func() { a.undoReplace(files, edited) })
}

// tagRenameView is the window.
func (a *App) tagRenameView(c *ui.Context) {
	r := &a.tagRename
	if !r.open {
		return
	}
	t := c.Theme()
	ui.Modal(c.Key("tag-rename"), &r.open, func() {
		ui.Column(c.Key("tag-rename-body")).Width(380).Gap(12).Children(func() {
			ui.Text(c, i18n.T("重命名标签 #")+r.from).FontSize(16).FontWeight(600)
			ui.Text(c, fmt.Sprintf(i18n.T("%d 篇文档使用这个标签；其下的标签（#%s/…）一起改名。代码中的不变。"), a.tagCount(r.from), r.from)).
				FontSize(12).TextColor(t.TextMuted)
			in := ui.TextInput(c.Key("tag-rename-to"), &r.to).AutoFocus().Placeholder(i18n.T("新的标签名")).Label(i18n.T("新的标签名"))
			name := strings.TrimPrefix(strings.TrimSpace(r.to), "#")
			valid := wiki.ValidTag(name) && name != r.from
			hint := ""
			switch {
			case name == "" || name == r.from:
			case !wiki.ValidTag(name):
				hint = i18n.T("标签只能包含文字、数字和 _ - /，且至少有一个文字。")
			case !strings.EqualFold(name, r.from) && a.tagCount(name) > 0:
				hint = fmt.Sprintf(i18n.T("已有标签 #%s（%d 篇），将合并为一个。"), name, a.tagCount(name))
			}
			if hint != "" {
				ui.Text(c, hint).FontSize(12).TextColor(t.Warning)
			}
			submit := func() {
				if valid {
					r.open = false
					a.renameTag(r.from, name)
				}
			}
			if in.Submitted() {
				submit()
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c.Key("tag-rename-cancel"), i18n.T("取消")).Clicked() {
					r.open = false
				}
				if ui.PrimaryButton(c.Key("tag-rename-go"), i18n.T("重命名")).Disabled(!valid).Clicked() {
					submit()
				}
			})
		})
	})
}
