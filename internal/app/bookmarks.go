package app

import (
	"slices"
	"strings"
	"time"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/fuzzy"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// Bookmarks mark places in documents: top-level blocks, kept in
// spaces.json rather than in the document, so marking a place neither
// edits the document nor converts a Markdown file. A bookmark finds its
// block by ID; a document read afresh from Markdown has new IDs, so the
// first time a document instance is seen a bookmark whose ID is gone finds
// the block by its excerpt and keeps the new ID. Later, a missing block is
// only missing — deleted, or merged away — and its bookmark waits, listed
// as lost, for an undo to bring the block back.

// docMarks is what a document's bookmarks resolve to, for a revision of
// the document and of the bookmarks.
type docMarks struct {
	rev, gen uint64
	known    bool
	// bound is set once the bookmarks were matched by excerpt, which only
	// the first resolution of a document does.
	bound bool
	m     map[string]string
	// excerptRev is the revision whose excerpts were written back.
	excerptRev uint64
	excerptGen uint64
}

// bookmarkKeys are the commands of bookmarks, which the page leaves to the
// window.
var bookmarkKeys = []string{"bookmark", "bookmarks", "prev-bookmark", "next-bookmark"}

// topBlock is the top-level block of the document with the ID, or nil.
func topBlock(d *document, id string) *model.Block {
	for _, b := range d.ed.Doc.Blocks {
		if b.ID == id {
			return b
		}
	}
	return nil
}

// marksOf are the blocks of the document with a bookmark, by ID, with
// their labels.
func (a *App) marksOf(d *document) map[string]string {
	if d == nil || d.loadErr != nil {
		return nil
	}
	mk := &d.marks
	rev := d.ed.Revision()
	if mk.known && mk.rev == rev && mk.gen == a.bookmarkGen {
		return mk.m
	}
	m := map[string]string{}
	rebound := false
	for i := range a.state.Bookmarks {
		b := &a.state.Bookmarks[i]
		if b.Space != d.space.ID || b.Path != d.rel {
			continue
		}
		blk := topBlock(d, b.Block)
		if blk == nil && !mk.bound && b.Excerpt != "" {
			for _, x := range d.ed.Doc.Blocks {
				if _, taken := m[x.ID]; !taken && a.state.BookmarkIndex(b.Space, b.Path, x.ID) < 0 && editor.BlockExcerpt(x) == b.Excerpt {
					blk, b.Block, rebound = x, x.ID, true
					break
				}
			}
		}
		if blk != nil {
			m[blk.ID] = b.Label
		}
	}
	mk.rev, mk.gen, mk.known, mk.bound, mk.m = rev, a.bookmarkGen, true, true, m
	if rebound {
		a.saveState()
	}
	return m
}

// syncExcerpts writes the current text of a document's bookmarked blocks
// to their bookmarks once the document is saved, so lists show what the
// block says now.
func (a *App) syncExcerpts(d *document) {
	if d == nil || d.loadErr != nil {
		return
	}
	mk := &d.marks
	rev := d.ed.Revision()
	if mk.excerptRev == rev && mk.excerptGen == a.bookmarkGen || d.dirty() {
		return
	}
	mk.excerptRev, mk.excerptGen = rev, a.bookmarkGen
	changed := false
	for i := range a.state.Bookmarks {
		b := &a.state.Bookmarks[i]
		if b.Space != d.space.ID || b.Path != d.rel {
			continue
		}
		if blk := topBlock(d, b.Block); blk != nil {
			if ex := editor.BlockExcerpt(blk); ex != b.Excerpt {
				b.Excerpt, changed = ex, true
			}
		}
	}
	if changed {
		a.saveState()
	}
}

// bookmarksChanged takes a change to the bookmarks: every document
// resolves its own again, and spaces.json keeps it.
func (a *App) bookmarksChanged() {
	a.bookmarkGen++
	a.saveState()
}

// toggleBookmark marks the top-level block of the document, or takes its
// bookmark away.
func (a *App) toggleBookmark(d *document, id string) {
	if d == nil || d.loadErr != nil {
		return
	}
	if !a.state.RemoveBookmark(d.space.ID, d.rel, id) {
		blk := topBlock(d, id)
		if blk == nil {
			return
		}
		a.state.AddBookmark(space.Bookmark{Space: d.space.ID, Path: d.rel, Block: id, Excerpt: editor.BlockExcerpt(blk), Created: time.Now()})
	}
	a.bookmarksChanged()
}

// labelBookmark names the bookmark on the block of the document; an empty
// label shows the excerpt again.
func (a *App) labelBookmark(spaceID, rel, id, label string) {
	if i := a.state.BookmarkIndex(spaceID, rel, id); i >= 0 && a.state.Bookmarks[i].Label != label {
		a.state.Bookmarks[i].Label = label
		a.bookmarksChanged()
	}
}

// bookmarkable is the open document when bookmarks work in it: loaded and
// shown as rich text.
func (a *App) bookmarkable() *document {
	if d := a.doc; d != nil && d.loadErr == nil && !d.source {
		return d
	}
	return nil
}

// toggleBookmarkAtCaret marks the block holding the caret, or unmarks it.
func (a *App) toggleBookmarkAtCaret() {
	d := a.bookmarkable()
	if d == nil {
		return
	}
	if blk := d.ed.BookmarkTarget(); blk != nil {
		a.toggleBookmark(d, blk.ID)
	}
}

// caretMarked reports whether the block holding the caret has a bookmark.
func (a *App) caretMarked(d *document) bool {
	blk := d.ed.BookmarkTarget()
	if blk == nil {
		return false
	}
	_, ok := a.marksOf(d)[blk.ID]
	return ok
}

// stepBookmark goes to the next bookmark of the open document after the
// caret's block (dir 1) or before it (-1), round from the end to the
// start.
func (a *App) stepBookmark(dir int) {
	d := a.bookmarkable()
	if d == nil {
		return
	}
	marks := a.marksOf(d)
	blocks := d.ed.Doc.Blocks
	if len(marks) == 0 || len(blocks) == 0 {
		a.notify("这篇文档还没有书签。按 %s 标记当前段落。", a.firstKey("bookmark"))
		return
	}
	at := d.ed.CaretBlock()
	for step := 1; step <= len(blocks); step++ {
		i := ((at+dir*step)%len(blocks) + len(blocks)) % len(blocks)
		if _, ok := marks[blocks[i].ID]; ok {
			d.ed.RevealBlock(blocks[i].ID)
			return
		}
	}
}

// firstKey is the first key of the command, or its name in the command
// bar when it has none.
func (a *App) firstKey(id string) string {
	if k := a.keyLabel(id); k != "" {
		return strings.Split(k, " / ")[0]
	}
	return i18n.T("命令栏「添加书签」")
}

// reservedKeys are the keys of the bookmark commands, which the page must
// not take for itself.
func (a *App) reservedKeys() []editor.Combo {
	var out []editor.Combo
	for _, id := range bookmarkKeys {
		out = append(out, a.combos(id)...)
	}
	return out
}

// sameBookmark reports whether two bookmarks are the same one, though its
// block may have been bound to a new ID since.
func sameBookmark(x, y space.Bookmark) bool {
	return x.Space == y.Space && x.Path == y.Path && x.Created.Equal(y.Created)
}

// liveBookmark is the bookmark as the state has it now.
func (a *App) liveBookmark(b space.Bookmark) (*space.Bookmark, bool) {
	for i := range a.state.Bookmarks {
		if sameBookmark(a.state.Bookmarks[i], b) {
			return &a.state.Bookmarks[i], true
		}
	}
	return nil, false
}

// openBookmark opens the bookmark's document and goes to its block; a
// bookmark whose block is gone opens the document at the top.
func (a *App) openBookmark(b space.Bookmark) {
	if d := a.doc; d == nil || d.space.ID != b.Space || d.rel != b.Path {
		if !a.openAnywhere(b.Space, b.Path) {
			return
		}
	}
	d := a.doc
	if d.loadErr != nil {
		return
	}
	if d.source {
		d.toggleMode()
	}
	marks := a.marksOf(d)
	live, ok := a.liveBookmark(b)
	if !ok {
		return
	}
	if _, found := marks[live.Block]; found {
		d.ed.RevealBlock(live.Block)
		return
	}
	a.notify("书签「%s」的位置已失效：那段内容已被删除或合并。", live.Title())
}

// removeBookmark takes the bookmark away, wherever its document is.
func (a *App) removeBookmark(b space.Bookmark) {
	if live, ok := a.liveBookmark(b); ok && a.state.RemoveBookmark(live.Space, live.Path, live.Block) {
		a.bookmarksChanged()
	}
}

// openBookmarks shows the bookmarks in the command bar.
func (a *App) openBookmarks() {
	a.openPalette()
	a.palette.bookmarks = true
}

// residentDoc is the document at the place if it is in memory.
func (a *App) residentDoc(spaceID, rel string) *document {
	for _, d := range a.residentDocuments() {
		if d.space.ID == spaceID && d.rel == rel {
			return d
		}
	}
	return nil
}

// bookmarkResults are the command bar's bookmarks: the open document's in
// the order of the page, then its lost ones, then every other document's,
// newest first; the query filters them by title and document.
func (a *App) bookmarkResults(q string) []action {
	q = strings.TrimSpace(q)
	var out []action
	add := func(b space.Bookmark, kind, hint string, lost bool) {
		title := b.Title()
		if q != "" {
			if _, ok := fuzzy.Score(q, title+" "+vault.Title(b.Path)); !ok {
				return
			}
		}
		if lost {
			hint = strings.TrimPrefix(hint+i18n.T(" · 位置已失效"), " · ")
		}
		bm := b
		out = append(out, action{kind: kind, title: title, hint: hint, bookmark: &bm, faded: lost,
			run: func() { a.openBookmark(bm) }})
	}
	var rest []space.Bookmark
	if d := a.bookmarkable(); d != nil {
		marks := a.marksOf(d)
		mine := a.state.BookmarksIn(d.space.ID, d.rel)
		for _, blk := range d.ed.Doc.Blocks {
			if _, ok := marks[blk.ID]; !ok {
				continue
			}
			for _, b := range mine {
				if b.Block == blk.ID {
					add(b, i18n.T("本文档"), "", false)
				}
			}
		}
		for _, b := range mine {
			if _, ok := marks[b.Block]; !ok {
				add(b, i18n.T("本文档"), "", true)
			}
		}
	}
	for _, b := range a.state.Bookmarks {
		if d := a.bookmarkable(); d != nil && b.Space == d.space.ID && b.Path == d.rel {
			continue
		}
		rest = append(rest, b)
	}
	slices.SortStableFunc(rest, func(x, y space.Bookmark) int { return y.Created.Compare(x.Created) })
	for _, b := range rest {
		hint := vault.Title(b.Path)
		if where := a.docHint(b.Space, b.Path); where != "" {
			hint = where + " / " + hint
		}
		lost := false
		if d := a.residentDoc(b.Space, b.Path); d != nil && d.loadErr == nil {
			_, found := a.marksOf(d)[b.Block]
			lost = !found
		}
		add(b, i18n.T("书签"), hint, lost)
	}
	return out
}

// bookmarkRow draws a bookmark's row in the command bar: its title, or the
// field naming it while it is being renamed, and its menu.
func (a *App) bookmarkRow(c *ui.Context, row ui.Element, act action) bool {
	p := &a.palette
	b := *act.bookmark
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item(i18n.T("重命名…")).Chosen() {
			p.renaming, p.renameOn, p.newLabel, p.renameFocused = b, true, b.Label, false
		}
		if m.Item(i18n.T("移除书签")).Chosen() {
			a.removeBookmark(b)
		}
	})
	if !p.renameOn || !sameBookmark(p.renaming, b) {
		return false
	}
	row.Children(func() {
		ui.Text(c, act.kind).FontSize(11).TextColor(c.Theme().TextMuted).Width(64).NoWrap()
		input := ui.TextInput(c.Key("bookmark-label"), &p.newLabel).Placeholder(b.Excerpt).Label(i18n.T("书签名称")).Grow(1).Padding(4, 8)
		// The field asks for the focus until it has it; only then does
		// leaving it keep the name.
		if !p.renameFocused {
			input.Focus()
		}
		left := p.renameFocused && !input.Focused()
		if input.Focused() {
			p.renameFocused = true
		}
		if input.Submitted() || left {
			if live, ok := a.liveBookmark(b); ok {
				a.labelBookmark(live.Space, live.Path, live.Block, strings.TrimSpace(p.newLabel))
			}
			p.renameOn = false
		}
	})
	return true
}

// shortKey is the first key of the command, "" when it has none.
func (a *App) shortKey(id string) string {
	return strings.Split(a.keyLabel(id), " / ")[0]
}
