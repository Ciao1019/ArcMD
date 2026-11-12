package app

// Find and replace across documents (⇧⌘R, the command bar): the text is
// found as the find box finds it, ignoring case, in the active space or
// every space, each document listed with how many times and where; 全部替换
// changes every native document, an open one through its editor as one
// step of its history, the others on disk past the conflict check, and a
// toast offers to put them back. Markdown documents are listed but kept
// byte for byte, as everywhere else: they change once converted.

import (
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/search"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// replaceState is the window of the find and replace across documents.
type replaceState struct {
	open       bool
	find, with string
	// everywhere searches every space, not only the active one.
	everywhere bool
}

// replaceLimit is how many documents the window lists and changes at most.
const replaceLimit = 500

// openReplaceAll shows the window, starting from the open document's
// selection when it has one.
func (a *App) openReplaceAll() {
	a.replace.open = true
	if d := a.doc; d != nil && d.loadErr == nil {
		if sel := d.ed.Selection().Range; sel.Start != sel.End {
			if text, _ := d.ed.TextForRange(sel); !strings.ContainsAny(text, "\n ") && utf8.RuneCountInString(text) <= 80 {
				a.replace.find = text
			}
		}
	}
}

// replaceHits are the documents holding what is to be found, in scope.
func (a *App) replaceHits() []search.Hit {
	r := &a.replace
	if !search.Worth(r.find) {
		return nil
	}
	active := a.state.ActiveSpace()
	var out []search.Hit
	for _, h := range a.liveIndex().Search(r.find, nil, replaceLimit) {
		if r.everywhere || active != nil && h.Space == active.ID {
			out = append(out, h)
		}
	}
	return out
}

// replaceFold replaces every match of find in s, ignoring case as the
// find box does, and says how many there were.
func replaceFold(s, find, with string) (string, int) {
	q := []rune(strings.Map(unicode.ToLower, find))
	if len(q) == 0 {
		return s, 0
	}
	runes := []rune(s)
	var out strings.Builder
	n := 0
	for i := 0; i < len(runes); {
		if i+len(q) <= len(runes) {
			ok := true
			for j, r := range q {
				if unicode.ToLower(runes[i+j]) != r {
					ok = false
					break
				}
			}
			if ok {
				out.WriteString(with)
				i += len(q)
				n++
				continue
			}
		}
		out.WriteRune(runes[i])
		i++
	}
	if n == 0 {
		return s, 0
	}
	return out.String(), n
}

// replacedFile is a document changed on disk, with its text before and
// after, to put back when nothing changed it since.
type replacedFile struct {
	space, rel    string
	before, after string
}

// replaceEverywhere replaces in every native document of the hits, and
// offers to undo it.
func (a *App) replaceEverywhere() {
	r := a.replace
	find := strings.TrimSpace(r.find)
	hits := a.replaceHits()
	if find == "" || len(hits) == 0 {
		return
	}
	resident := map[place]*document{}
	for _, d := range a.residentDocuments() {
		if d.loadErr == nil {
			resident[place{d.space.ID, d.rel}] = d
		}
	}
	count, docs, skipped, failed := 0, 0, 0, 0
	var files []replacedFile
	var edited []*document
	for _, h := range hits {
		sp := a.state.Get(h.Space)
		if sp == nil {
			continue
		}
		if vault.IsMarkdown(h.Path) {
			skipped++
			continue
		}
		n := 0
		change := func(s string) string {
			out, k := replaceFold(s, find, r.with)
			n += k
			return out
		}
		if d := resident[place{h.Space, h.Path}]; d != nil {
			if d.ed.ReadOnly {
				skipped++
				continue
			}
			if d.ed.Engine.MapText(change) {
				count, docs = count+n, docs+1
				edited = append(edited, d)
			}
			continue
		}
		before, err := vault.Read(sp.Root, h.Path)
		if err != nil {
			failed++
			continue
		}
		d := openDocument(sp, h.Path, before, false)
		if d.loadErr != nil || !d.ed.Engine.MapText(change) {
			continue
		}
		if err := d.save(); err != nil {
			failed++
			a.notify("替换 %s 失败。%v", h.Path, err)
			continue
		}
		after, _ := vault.Read(sp.Root, d.rel)
		files = append(files, replacedFile{sp.ID, d.rel, before, after})
		count, docs = count+n, docs+1
	}
	a.refreshSearch()
	note := ""
	if skipped > 0 {
		note = fmt.Sprintf(i18n.T("，跳过 %d 篇（Markdown 或只读）"), skipped)
	}
	if failed > 0 {
		note += fmt.Sprintf(i18n.T("，%d 篇失败"), failed)
	}
	if docs == 0 {
		a.notify("没有可替换的文档%s。", note)
		return
	}
	a.replace.open = false
	a.offerUndo(fmt.Sprintf(i18n.T("已在 %d 篇文档中替换 %d 处%s"), docs, count, note), "「"+find+"」→「"+r.with+"」", func() {
		a.undoReplace(files, edited)
	})
}

// undoReplace puts back the documents a replace changed: the open ones
// through their editors' undo, the others when they still hold what the
// replace wrote.
func (a *App) undoReplace(files []replacedFile, edited []*document) {
	for _, d := range edited {
		d.ed.Undo()
	}
	kept := 0
	for _, f := range files {
		sp := a.state.Get(f.space)
		if sp == nil {
			kept++
			continue
		}
		now, err := vault.Read(sp.Root, f.rel)
		if err != nil || now != f.after {
			kept++ // changed since: leave it
			continue
		}
		if err := vault.Write(sp.Root, f.rel, f.before); err != nil {
			kept++
		}
	}
	a.refreshSearch()
	if kept > 0 {
		a.notify("%d 篇文档在替换后又被修改，没有撤销。", kept)
	}
}

// replaceView is the window.
func (a *App) replaceView(c *ui.Context) {
	r := &a.replace
	if !r.open {
		return
	}
	t := c.Theme()
	w, h := c.Size()
	ui.Modal(c.Key("replace-all"), &r.open, func() {
		ui.Column(c.Key("replace-body")).Size(min(620, w-t.Space(20)), min(520, h-t.Space(20))).Gap(12).Children(func() {
			ui.Text(c, i18n.T("在所有文档中查找和替换")).FontSize(16).FontWeight(600)
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T("查找")).Width(36).TextColor(t.TextMuted)
				ui.TextInput(c.Key("replace-find"), &r.find).Grow(1).AutoFocus().Placeholder(i18n.T("要查找的文字（不区分大小写）")).Label(i18n.T("查找"))
			})
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T("替换")).Width(36).TextColor(t.TextMuted)
				ui.TextInput(c.Key("replace-with"), &r.with).Grow(1).Placeholder(i18n.T("替换为（留空即删除）")).Label(i18n.T("替换为"))
			})
			scope := 0
			if r.everywhere {
				scope = 1
			}
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T("范围")).Width(36).TextColor(t.TextMuted)
				ui.Segmented(c.Key("replace-scope"), &scope, i18n.T("当前空间"), i18n.T("所有空间")).Label(i18n.T("范围"))
			})
			r.everywhere = scope == 1
			hits := a.replaceHits()
			total := 0
			for _, hit := range hits {
				total += hit.Count
			}
			ui.Scroll(c.Key("replace-hits")).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					switch {
					case !search.Worth(r.find):
						ui.Text(c, i18n.T("输入至少两个字符（或一个汉字）开始查找。")).FontSize(13).TextColor(t.TextMuted).Padding(16, 4)
					case len(hits) == 0:
						ui.Text(c, i18n.T("没有找到「")+strings.TrimSpace(r.find)+"」。").FontSize(13).TextColor(t.TextMuted).Padding(16, 4)
					}
					for _, hit := range hits {
						a.replaceRow(c, hit)
					}
				})
			})
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				summary := ""
				if len(hits) > 0 {
					summary = fmt.Sprintf(i18n.T("%d 篇文档，共 %d 处"), len(hits), total)
				}
				ui.Text(c, summary).FontSize(12).TextColor(t.TextMuted).Grow(1)
				if ui.Button(c.Key("replace-cancel"), i18n.T("取消")).Clicked() {
					r.open = false
				}
				if ui.PrimaryButton(c.Key("replace-go"), i18n.T("全部替换")).Disabled(len(hits) == 0).Clicked() {
					a.replaceEverywhere()
				}
			})
		})
	})
}

// replaceRow is a document holding the text: its name, where it is, the
// count and the first match in its line; clicking opens it there.
func (a *App) replaceRow(c *ui.Context, hit search.Hit) {
	t := c.Theme()
	sp := a.state.Get(hit.Space)
	if sp == nil {
		return
	}
	row := ui.ButtonBase(c.Key("replace-hit:" + hit.Space + ":" + hit.Path)).PaddingX(10).PaddingY(6).Gap(10).Radius(8).
		AlignItems(ui.Center).Label(i18n.T("打开 ") + vault.Title(hit.Path)).Cursor(ui.CursorPointer)
	if row.Hovered() {
		row.Background(hoverFill(t))
	}
	row.Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, docName(vault.Title(hit.Path))).FontSize(14).SingleLine().Shrink(1).MinWidth(0)
				ui.Text(c, placeHint(sp, hit.Path)).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
				if vault.IsMarkdown(hit.Path) {
					ui.Text(c, i18n.T("Markdown · 不替换")).FontSize(11).TextColor(t.Warning).NoWrap()
				}
			})
			ui.RichText(c,
				ui.Span{Text: hit.Snippet[:hit.Match[0]]},
				ui.Span{Text: hit.Snippet[hit.Match[0]:hit.Match[1]], Color: t.Text, Weight: 600, Background: searchMark(t), Radius: 3},
				ui.Span{Text: hit.Snippet[hit.Match[1]:]},
			).FontSize(12).TextColor(t.TextMuted).SingleLine().MinWidth(0)
		})
		ui.Text(c, fmt.Sprintf(i18n.T("%d 处"), hit.Count)).FontSize(12).TextColor(t.TextMuted).NoWrap()
	})
	if row.Clicked() {
		a.replace.open = false
		a.openAtText(hit.Space, hit.Path, a.replace.find)
	}
}

// placeHint is where a document is, for a list: its space and folder.
func placeHint(sp *space.Space, rel string) string {
	if dir := path.Dir(rel); dir != "." {
		return sp.Name + " / " + dir
	}
	return sp.Name
}
