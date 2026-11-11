package app

import (
	"fmt"
	"path"
	"slices"
	"strings"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/fuzzy"
	"github.com/Ciao1019/ArcMD/internal/search"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/Ciao1019/ArcMD/internal/wiki"
	"github.com/egoist/mygo/ui"
)

// Links between documents are [[Name]] in their text (package wiki): ⌘-click
// opens the document named, or makes it when there is none; typing [[
// offers the documents; below each document are the ones linking to it;
// renaming a document renames the links to it.

// eachDoc calls fn with every document of every space, the active space's
// first, until fn returns false.
func (a *App) eachDoc(fn func(sp *space.Space, n *vault.Node) bool) {
	active := a.state.ActiveSpace()
	order := slices.Clone(a.state.Spaces)
	slices.SortStableFunc(order, func(x, y *space.Space) int {
		return b2i(y == active) - b2i(x == active)
	})
	for _, sp := range order {
		var nodes []*vault.Node
		if sp == active {
			nodes = a.tree
		} else {
			nodes, _ = a.treeOf(sp)
		}
		stop := false
		var walk func([]*vault.Node)
		walk = func(nodes []*vault.Node) {
			for _, n := range nodes {
				if stop {
					return
				}
				if n.Dir {
					walk(n.Children)
				} else if !fn(sp, n) {
					stop = true
				}
			}
		}
		walk(nodes)
		if stop {
			return
		}
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// findNamed resolves relative to the document's own space, even when a
// favorite is displayed in another space. Other spaces keep their saved order.
func (a *App) findNamed(name string) (*space.Space, string, bool) {
	id := a.state.ActiveSpace().ID
	if a.doc != nil {
		id = a.doc.space.ID
	}
	return a.findNamedFrom(id, name)
}

func (a *App) findNamedFrom(id, name string) (*space.Space, string, bool) {
	var candidates []place
	a.eachDoc(func(sp *space.Space, n *vault.Node) bool {
		if wiki.Same(n.Title(), name) {
			candidates = append(candidates, place{sp.ID, n.Path})
		}
		return true
	})
	priority := func(spaceID string) int {
		if spaceID == id {
			return -1
		}
		return slices.IndexFunc(a.state.Spaces, func(sp *space.Space) bool { return sp.ID == spaceID })
	}
	slices.SortFunc(candidates, func(x, y place) int {
		if d := priority(x.space) - priority(y.space); d != 0 {
			return d
		}
		return strings.Compare(x.rel, y.rel)
	})
	if len(candidates) == 0 {
		return nil, "", false
	}
	return a.state.Get(candidates[0].space), candidates[0].rel, true
}

// renameSources captures resolution before the file name changes. Identical
// labels in another space must keep referring to that space's own document.
func (a *App) renameSources(sp *space.Space, rel string) map[string]bool {
	allowed := map[string]bool{}
	for _, source := range a.state.Spaces {
		target, file, ok := a.findNamedFrom(source.ID, vault.Title(rel))
		allowed[source.ID] = ok && target == sp && file == rel
	}
	return allowed
}

// openWiki opens the document a [[link]] names, or makes it beside the
// open document when there is none.
func (a *App) openWiki(name string) {
	if sp, rel, ok := a.findNamed(name); ok {
		a.openAnywhere(sp.ID, rel)
		return
	}
	a.newDocNamed(a.currentDir(), name)
}

// newDocNamed makes a document titled name in the folder dir of the active
// space and opens it, its title written.
func (a *App) newDocNamed(dir, name string) {
	sp := a.state.ActiveSpace()
	if sp == nil || !a.flush() {
		return
	}
	doc := model.New()
	title := model.NewBlock("heading", strings.TrimSpace(name))
	title.Level = 1
	doc.Blocks = []*model.Block{title, model.NewBlock("paragraph", "")}
	text, err := doc.Encode()
	if err == nil {
		var rel string
		if rel, err = vault.CreateFile(sp.Root, dir, name, ".arcmd", text); err == nil {
			a.refreshTree()
			a.open(rel)
			if a.doc != nil && a.doc.rel == rel {
				end := a.doc.ed.Len()
				a.doc.ed.SetSelection(end, end)
				a.doc.ed.RequestFocus = true
			}
			return
		}
	}
	a.notify("新建「%s」失败。%v", name, err)
}

// wikiSuggest lists the documents whose names match what is typed of a
// [[link]], the best first.
func (a *App) wikiSuggest(typed string) []editor.WikiItem {
	return a.wikiSuggestFor(a.doc, typed)
}

func (a *App) wikiSuggestFor(d *document, typed string) []editor.WikiItem {
	type scored struct {
		item  editor.WikiItem
		score int
	}
	var out []scored
	seen := map[string]bool{}
	self := ""
	if d != nil {
		self = d.title()
	}
	a.eachDoc(func(sp *space.Space, n *vault.Node) bool {
		title := n.Title()
		key := strings.ToLower(title)
		if seen[key] || wiki.Same(title, self) && typed == "" {
			return true
		}
		s := 0
		if typed != "" {
			var ok bool
			if s, ok = fuzzy.Score(typed, title); !ok {
				return true
			}
		}
		seen[key] = true
		hint := sp.Name
		if dir := path.Dir(n.Path); dir != "." {
			hint += " / " + dir
		}
		out = append(out, scored{editor.WikiItem{Name: title, Hint: hint}, s})
		return true
	})
	slices.SortStableFunc(out, func(x, y scored) int { return y.score - x.score })
	items := make([]editor.WikiItem, 0, 8)
	for _, s := range out[:min(len(out), 8)] {
		items = append(items, s.item)
	}
	return items
}

// openTag shows the documents of a #tag in the command bar.
func (a *App) openTag(tag string) {
	a.openPalette()
	if a.split != nil {
		a.palette.pane = a.split.panes[a.split.active]
	}
	a.palette.query = "#" + tag
}

// live is the open document as search sees it.
func (a *App) live() *search.Live {
	if d := a.doc; d != nil && d.loadErr == nil {
		return &search.Live{Space: d.space.ID, Path: d.rel, Text: d.plainText()}
	}
	return nil
}

// tagResults are the command bar's results for a query starting with #:
// the documents of the tag typed, then the tags matching it.
func (a *App) tagResults(q string) []action {
	name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(q), "#"))
	var out []action
	if name != "" {
		for _, h := range a.liveIndex().Tagged(name, nil) {
			id, rel := h.Space, h.Path
			hint := a.docHint(id, rel)
			out = append(out, action{kind: "标签", title: vault.Title(rel), hint: hint, doc: place{id, rel},
				snippet: h.Snippet, match: h.Match, run: func() { a.openAnywhere(id, rel) }})
		}
	}
	for _, tc := range a.liveIndex().Tags(nil) {
		if name != "" {
			if _, ok := fuzzy.Score(name, tc.Name); !ok || strings.EqualFold(tc.Name, name) {
				continue
			}
		}
		tag := tc.Name
		out = append(out, action{kind: "标签", title: "#" + tag, hint: fmt.Sprintf("%d 篇文档", tc.Count), run: func() {
			a.palette.open = true
			a.palette.query = "#" + tag
		}})
	}
	return out[:min(len(out), paletteLimit+searchLimit)]
}

// Footer content follows document/index revisions, not redraws or pane widths.
type pageFooterCache struct {
	ready    bool
	index    *search.Index
	location place
	tags     []string
	hits     []search.Hit
}

func (a *App) footerData(d *document) *pageFooterCache {
	ix := a.liveIndex()
	location := place{d.space.ID, d.rel}
	cache := &d.footer
	if !cache.ready || cache.index != ix || cache.location != location {
		*cache = pageFooterCache{ready: true, index: ix, location: location,
			tags: wiki.TagNames(d.plainText()),
			hits: ix.Backlinks(d.title(), search.Live{Space: location.space, Path: location.rel}, nil)}
	}
	return cache
}

// pageFooter is drawn below the open document: its tags and the documents
// linking to it.
func (a *App) pageFooter(c *ui.Context) {
	a.pageFooterFor(c, a.doc)
}

func (a *App) pageFooterFor(c *ui.Context, d *document) {
	if d == nil {
		return
	}
	t := c.Theme()
	pg := pageStyle(a.paperDark)
	data := a.footerData(d)
	tags, hits := data.tags, data.hits
	if len(hits) == 0 && len(tags) == 0 && d.ed.Blank() {
		return // a blank page shows its starter alone
	}
	ui.Column(c).Gap(10).Children(func() {
		ui.Box(c).Height(1).Background(t.Border.Alpha(0.6))
		if len(tags) > 0 {
			ui.Row(c.Key("page-tags")).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconHash).FontSize(14).TextColor(t.TextMuted)
				for _, tag := range tags {
					chip := ui.ButtonBase(c.Key("tag:" + tag)).Height(24).PaddingX(8).Radius(6).Label("标签 " + tag).
						Cursor(ui.CursorPointer).Background(pg.TagFill)
					if chip.Hovered() {
						chip.Background(t.Accent.Alpha(0.18))
					}
					chip.Children(func() { ui.Text(c, "#"+tag).FontSize(13).TextColor(t.Accent) })
					if chip.Clicked() {
						a.activateDocument(d)
						a.openTag(tag)
					}
				}
			})
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconLink).FontSize(14).TextColor(t.TextMuted)
			label := "反向链接"
			if len(hits) > 0 {
				label = fmt.Sprintf("反向链接 · %d", len(hits))
			}
			ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(t.TextMuted)
		})
		if len(hits) == 0 {
			ui.Text(c, "还没有文档链接到这里。在其他文档中输入 [[ 即可链接到「"+d.title()+"」。").FontSize(13).TextColor(t.TextMuted.Alpha(0.8))
			return
		}
		for _, h := range hits {
			id, rel := h.Space, h.Path
			row := ui.ButtonBase(c.Key("backlink:"+id+"/"+rel)).Padding(8, 10).Radius(8).Gap(4).Label("打开 " + vault.Title(rel)).
				Cursor(ui.CursorPointer).AlignItems(ui.Stretch)
			if row.Hovered() {
				row.Background(pg.Hover)
			}
			row.Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						docIcon(c, a.iconTitle(id, rel), 16)
						ui.Text(c, docName(vault.Title(rel))).FontSize(14).SingleLine().Shrink(1).MinWidth(0)
						if hint := a.docHint(id, rel); hint != "" {
							ui.Text(c, hint).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
						}
					})
					s, m := h.Snippet, h.Match
					ui.RichText(c,
						ui.Span{Text: s[:m[0]]},
						ui.Span{Text: s[m[0]:m[1]], Color: t.Accent},
						ui.Span{Text: s[m[1]:]},
					).FontSize(13).TextColor(t.TextMuted).SingleLine().MinWidth(0)
				})
			})
			if row.Clicked() {
				a.navigateInPane(d, func() { a.openAnywhere(id, rel) })
			}
		}
	})
}

// iconTitle is a document's title with its page icon before it, which
// docIcon shows in place of the letter.
func (a *App) iconTitle(spaceID, rel string) string {
	title := vault.Title(rel)
	icon := ""
	if d := a.residentDocument(spaceID, rel); d != nil && d.loadErr == nil {
		icon = d.ed.Doc.Icon
	} else {
		icon = a.search.index.Icon(spaceID, rel)
	}
	if icon == "" || leadingEmoji(title) != "" {
		return title
	}
	return icon + " " + title
}

// relink renames the links to the document titled from in every document
// linking to it to to, the open one through its editor, and says how many
// changed.
func (a *App) relink(from, to string, allowed map[string]bool) {
	if wiki.Same(from, to) || from == "" || to == "" {
		return
	}
	rename := func(s string) string {
		out, _ := wiki.Rename(s, from, to)
		return out
	}
	changed := 0
	resident := map[place]bool{}
	for _, d := range a.residentDocuments() {
		resident[place{d.space.ID, d.rel}] = true
		if allowed[d.space.ID] && d.loadErr == nil && wiki.LinksTo(d.plainText(), from) && d.ed.Engine.MapText(rename) {
			changed++
		}
	}
	var files []place
	a.eachDoc(func(sp *space.Space, n *vault.Node) bool {
		if allowed[sp.ID] {
			files = append(files, place{sp.ID, n.Path})
		}
		return true
	})
	for _, file := range files {
		if resident[file] || !strings.EqualFold(path.Ext(file.rel), ".arcmd") {
			continue
		}
		sp := a.state.Get(file.space)
		text, err := vault.Read(sp.Root, file.rel)
		if err != nil || !wiki.LinksTo(text, from) {
			continue
		}
		d := openDocument(sp, file.rel, text, false)
		if d.loadErr != nil || !d.ed.Engine.MapText(rename) {
			continue
		}
		if err := d.save(); err != nil {
			a.notify("更新 %s 的双链失败。%v", file.rel, err)
			continue
		}
		changed++
	}

	if changed > 0 {
		a.refreshSearch()
		a.notify("已更新 %d 篇文档中指向「%s」的链接。", changed, to)
	}
}

// previewWikiFor resolves in the source note's space and prefers live editors,
// so previews include unsaved changes without changing navigation or selection.
func (a *App) previewWikiFor(source *document, name string) editor.WikiPreview {
	p := editor.WikiPreview{Title: name, Missing: true}
	if source == nil {
		return p
	}
	sp, rel, ok := a.findNamedFrom(source.space.ID, name)
	if !ok {
		return p
	}
	p.Location = sp.Name + " / " + rel
	d := a.residentDocument(sp.ID, rel)
	if d == nil {
		d = a.thumbOf(sp, rel)
	}
	if d == nil || d.loadErr != nil {
		p.Text = "无法读取这篇笔记"
		p.Missing = false
		return p
	}
	text := []rune(d.ed.Committed().PlainText())
	if len(text) > 500 {
		text = append(text[:500], '…')
	}
	p.Text, p.Missing = string(text), false
	return p
}
