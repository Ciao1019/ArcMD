package app

import (
	"path"
	"strings"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/search"
)

// searchState is the command bar's full-text search: the index of every
// space's documents, and the hits last found, which the bar asks for
// every frame.
type searchState struct {
	index *search.Index
	// A pointer move or viewport resize does not change searchable text.
	liveBase, overlay *search.Index
	liveDocs          map[*document]liveStamp
	overlayReady      bool
	// busy is set while a refresh runs, again when another was asked for
	// meanwhile.
	busy, again bool
	// async refreshes on a goroutine, as the running app does; tests
	// refresh at once.
	async bool
	// hits were found for hitsKey.
	hits    []search.Hit
	hitsKey hitsKey
}

type liveStamp struct {
	revision uint64
	location place
}

type hitsKey struct {
	query string
	index *search.Index
	doc   *document
	rev   uint64
}

// extractText is the plain text a document file is searched by: its
// blocks' text, a line each, as the editor's find box sees it, and its
// page icon.
func extractText(name, content string) (search.Extracted, error) {
	if strings.EqualFold(path.Ext(name), ".arcmd") {
		doc, err := model.Decode(content)
		if err != nil {
			return search.Extracted{}, err
		}
		return search.Extracted{Text: doc.PlainText(), Icon: doc.Icon}, nil
	}
	return search.Extracted{Text: editor.ImportMarkdown(content).PlainText()}, nil
}

// refreshSearch reads the spaces' documents into the index again, only
// the files changed since. The bar shows title matches meanwhile, and the
// text hits when it is done.
func (a *App) refreshSearch() {
	s := &a.search
	if s.busy {
		s.again = true
		return
	}
	roots := make([]search.Root, 0, len(a.state.Spaces))
	for _, sp := range a.state.Spaces {
		roots = append(roots, search.Root{Space: sp.ID, Path: sp.Root})
	}
	prev := s.index
	s.busy = true
	done := func(ix *search.Index) {
		s.index, s.busy = ix, false
		if s.again {
			s.again = false
			a.refreshSearch()
		}
	}
	if !s.async {
		done(search.Refresh(prev, roots, extractText))
		return
	}
	go func() {
		ix := search.Refresh(prev, roots, extractText)
		a.win.Update(func() { done(ix) })
	}()
}

// textHits are the documents holding the query, the open one searched as
// it is now, edits included.
func (a *App) textHits(query string) []search.Hit {
	s := &a.search
	k := hitsKey{query: query, index: a.liveIndex()}
	if k != s.hitsKey {
		s.hits, s.hitsKey = k.index.Search(query, nil, searchLimit), k
	}
	return s.hits
}

// searchLimit is how many documents the bar lists for their text.
const searchLimit = 5

// plainText is the document's text as search sees it, kept for the
// revision.
func (d *document) plainText() string {
	if rev := d.ed.Revision(); !d.plainKnown || rev != d.plainRev {
		d.plain, d.plainRev, d.plainKnown = d.ed.Committed().PlainText(), rev, true
	}
	return d.plain
}

// openAnywhere opens the document at rel of the space with the ID,
// entering that space unless the document is a favorite, which opens
// where the window is. It reports whether the document is now open.
func (a *App) openAnywhere(id, rel string) bool {
	sp := a.state.Get(id)
	if sp == nil {
		return false
	}
	if a.paneNavigation && a.split != nil {
		a.openIn(sp, rel)
		return a.doc != nil && a.doc.space == sp && a.doc.rel == rel
	}
	if sp != a.state.ActiveSpace() && !a.state.IsFavorite(id, rel) {
		a.switchSpaceOnly(id)
		if sp != a.state.ActiveSpace() {
			return false
		}
	} else if sp != a.state.ActiveSpace() && !a.canEnter(sp.Root) {
		return false
	}
	a.openIn(sp, rel)
	return a.doc != nil && a.doc.space == sp && a.doc.rel == rel
}

// openAtText opens a document the bar found text in, with the find box on
// that text.
func (a *App) openAtText(id, rel, query string) {
	if !a.openAnywhere(id, rel) || a.doc.loadErr != nil {
		return
	}
	if a.doc.source {
		a.doc.toggleMode()
	}
	a.doc.ed.FindText(strings.TrimSpace(query))
}

// docHint is where a document is, for the bar: its folder, and its space
// when that is not the active one.
func (a *App) docHint(id, rel string) string {
	var parts []string
	if active := a.state.ActiveSpace(); active == nil || active.ID != id {
		if sp := a.state.Get(id); sp != nil {
			parts = append(parts, sp.Name)
		}
	}
	if dir := path.Dir(rel); dir != "." {
		parts = append(parts, dir)
	}
	return strings.Join(parts, " / ")
}

// recentPlaces are the documents last shown, newest first, without the
// open one, at most n.
func (a *App) recentPlaces(n int) []place {
	var out []place
	seen := map[place]bool{}
	if a.doc != nil {
		seen[place{a.doc.space.ID, a.doc.rel}] = true
	}
	for i := len(a.history) - 1; i >= 0 && len(out) < n; i-- {
		p := a.history[i]
		if seen[p] || a.state.Get(p.space) == nil {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func (a *App) liveIndex() *search.Index {
	s := &a.search
	visible := map[*document]bool{}
	for _, d := range a.visibleDocuments() {
		visible[d] = true
	}
	var docs []*document
	unchanged := s.overlayReady && s.liveBase == s.index
	for _, d := range a.residentDocuments() {
		if d.loadErr != nil || !visible[d] && !d.dirty() {
			continue
		}
		docs = append(docs, d)
		stamp := liveStamp{d.ed.Revision(), place{d.space.ID, d.rel}}
		if previous, ok := s.liveDocs[d]; !ok || previous != stamp {
			unchanged = false
		}
	}
	if unchanged && len(docs) == len(s.liveDocs) {
		return s.overlay
	}
	s.liveDocs = make(map[*document]liveStamp, len(docs))
	live := make([]search.Live, 0, len(docs))
	for _, d := range docs {
		s.liveDocs[d] = liveStamp{d.ed.Revision(), place{d.space.ID, d.rel}}
		live = append(live, search.Live{Space: d.space.ID, Path: d.rel, Text: d.plainText()})
	}
	s.overlay, s.liveBase, s.overlayReady = s.index.WithLive(live), s.index, true
	return s.overlay
}
