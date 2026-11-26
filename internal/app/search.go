package app

import (
	"maps"
	"path"
	"path/filepath"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/bookdoc"
	"github.com/Ciao1019/ArcMD/internal/database"
	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/mdz"
	"github.com/Ciao1019/ArcMD/internal/pdfdoc"
	"github.com/Ciao1019/ArcMD/internal/search"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
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
	// writeSeq increments when an open document writes or reloads from disk;
	// indexedSeq records the writeSeq reflected by index.
	writeSeq, indexedSeq uint64
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
	if vault.IsPDF(name) {
		doc, err := pdfdoc.OpenBytes([]byte(content))
		if err != nil {
			return search.Extracted{}, nil
		}
		defer doc.Close()
		return search.Extracted{Text: doc.PlainText(200)}, nil
	}
	if vault.IsBook(name) {
		doc, err := bookdoc.OpenBytes(name, []byte(content))
		if err != nil {
			return search.Extracted{}, nil
		}
		defer doc.Close()
		return search.Extracted{Text: doc.PlainText(200)}, nil
	}
	if vault.IsMDZ(name) {
		arc, err := mdz.OpenBytes([]byte(content))
		if arc == nil {
			_ = err
			return search.Extracted{}, nil
		}
		defer arc.Close()
		return search.Extracted{Text: arc.PlainText(2 << 20)}, nil
	}
	if vault.IsWhiteboard(name) {
		scene, err := whiteboard.Decode(content)
		if err != nil {
			return search.Extracted{}, err
		}
		return search.Extracted{Text: scene.PlainText()}, nil
	}
	if vault.IsDatabase(name) {
		return extractDatabaseSearchText(name, content), nil
	}
	if strings.EqualFold(path.Ext(name), ".arcmd") {
		doc, err := model.Decode(content)
		if err != nil {
			return search.Extracted{}, err
		}
		return search.Extracted{Text: doc.PlainText(), Icon: doc.Icon}, nil
	}
	text, icon := editor.ExtractMarkdownText(content)
	if fm, _, hasFM, _ := database.SplitFrontmatter(content); hasFM && fm != nil && !fm.IsEmpty() {
		if fmText := extractFrontmatterSearchText(fm); fmText != "" {
			if text != "" {
				text += "\n" + fmText
			} else {
				text = fmText
			}
		}
	}
	return search.Extracted{Text: text, Icon: icon}, nil
}

func extractFrontmatterSearchText(fm *database.FrontmatterDoc) string {
	if fm == nil || fm.IsEmpty() {
		return ""
	}
	var out []string
	skipKey := false
	for _, rawLine := range strings.Split(fm.Format(), "\n") {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if len(rawLine) > 0 && rawLine[0] != ' ' && rawLine[0] != '\t' {
			if k, v, ok := strings.Cut(trimmed, ":"); ok {
				key := strings.TrimSpace(k)
				switch key {
				case "icon", "cover", "coverY", "goal":
					skipKey = true
					continue
				default:
					skipKey = false
				}
				val := strings.Trim(strings.TrimSpace(v), `"'`)
				if val != "" && val != "[]" && val != "null" && val != "~" {
					out = append(out, val)
				}
				continue
			}
		}
		if skipKey {
			continue
		}
		item := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		item = strings.Trim(item, `"'`)
		if strings.HasPrefix(item, "[[") && strings.HasSuffix(item, "]]") && len(item) > 4 {
			item = item[2 : len(item)-2]
		}
		if item != "" {
			out = append(out, item)
		}
	}
	return strings.Join(out, "\n")
}

func (a *App) searchCachePath() string {
	if a.cfg.ConfigDir == "" {
		return ""
	}
	return filepath.Join(a.cfg.ConfigDir, "search-cache.bin")
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
		r := search.Root{Space: sp.ID, Path: sp.Root}
		if sp.IsExternal() {
			r.Files = vault.Files(maps.Clone(sp.ExternalFiles))
			if r.Files == nil {
				r.Files = vault.Files{}
			}
		}
		roots = append(roots, r)
	}
	prev := s.index
	snapSeq := s.writeSeq
	cachePath := a.searchCachePath()
	s.busy = true
	done := func(ix *search.Index) {
		s.index, s.indexedSeq, s.busy = ix, snapSeq, false
		if s.again {
			s.again = false
			a.refreshSearch()
		}
	}
	post := a.runAsync
	if post == nil && a.win != nil {
		post = a.win.Update
	}
	if !s.async || post == nil {
		ix, changed := search.RefreshChanged(prev, roots, extractText)
		if changed && cachePath != "" {
			_ = search.SaveCache(cachePath, ix)
		}
		done(ix)
		return
	}
	go func() {
		ix, changed := search.RefreshChanged(prev, roots, extractText)
		if changed && cachePath != "" {
			_ = search.SaveCache(cachePath, ix)
		}
		post(func() { done(ix) })
	}()
}

// textHits are the documents holding the query, the open one searched as
// it is now, edits included.
func (a *App) textHits(query string) []search.Hit {
	s := &a.search
	k := hitsKey{query: query, index: a.liveIndex()}
	if k != s.hitsKey {
		s.hits, s.hitsKey = k.index.SearchMulti(query, nil, searchLimit), k
	}
	return s.hits
}

// searchLimit is how many documents the bar lists for their text.
const searchLimit = 5

// plainText is the document's text as search sees it, kept for the
// revision.
func (d *document) plainText() string {
	if d.isPDF() {
		if !d.plainKnown {
			if d.pdf != nil && d.pdf.Doc != nil {
				d.plain = d.pdf.Doc.PlainText(200)
			}
			d.plainKnown = true
		}
		return d.plain
	}
	if d.isBook() {
		if !d.plainKnown {
			if d.book != nil && d.book.Doc != nil {
				d.plain = d.book.Doc.PlainText(200)
			}
			d.plainKnown = true
		}
		return d.plain
	}
	if d.isMDZ() {
		if !d.plainKnown {
			if d.mdz != nil && d.mdz.archive != nil {
				d.plain = d.mdz.archive.PlainText(2 << 20)
			}
			d.plainKnown = true
		}
		return d.plain
	}
	if d.isWhiteboard() {
		if rev := d.board.Revision(); !d.plainKnown || rev != d.plainRev {
			d.plain, d.plainRev, d.plainKnown = d.board.Scene.PlainText(), rev, true
		}
		return d.plain
	}
	if d.isDatabase() {
		if rev := d.dbView.Store.Revision(); !d.plainKnown || rev != d.plainRev {
			d.plain = databasePlainText(d.dbView.Store)
			d.plainRev, d.plainKnown = rev, true
		}
		return d.plain
	}
	if rev := d.ed.Revision(); !d.plainKnown || rev != d.plainRev {
		d.plain, d.plainRev, d.plainKnown = d.ed.Text(), rev, true
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
	a.openAtTextTerm(id, rel, query, "")
}

func (a *App) openAtTextTerm(id, rel, query, term string) {
	if !a.openAnywhere(id, rel) || a.doc.loadErr != nil {
		return
	}
	if a.doc.isDatabase() {
		needle := strings.TrimSpace(term)
		if needle == "" {
			needle = strings.TrimSpace(query)
		}
		if needle != "" {
			a.doc.dbView.SearchQuery = needle
			a.doc.dbView.SearchOpen = true
		}
		return
	}
	if a.doc.isPDF() {
		needle := strings.TrimSpace(term)
		if needle == "" {
			needle = strings.TrimSpace(query)
		}
		if needle != "" {
			a.doc.pdf.FindText(needle)
		}
		return
	}
	if a.doc.isBook() {
		needle := strings.TrimSpace(term)
		if needle == "" {
			needle = strings.TrimSpace(query)
		}
		if needle != "" {
			a.doc.book.FindText(needle)
		}
		return
	}
	if a.doc.isMDZ() {
		needle := strings.TrimSpace(term)
		if needle == "" {
			needle = strings.TrimSpace(query)
		}
		if needle != "" && a.doc.mdz.archive != nil {
			low := strings.ToLower(needle)
			for _, p := range a.doc.mdz.pages {
				if md, err := a.doc.mdz.archive.ReadMarkdown(p); err == nil && strings.Contains(strings.ToLower(md), low) {
					a.doc.mdz.openPage(p, true, "")
					break
				}
			}
			if ed := a.doc.mdz.activeEditor(); ed != nil {
				ed.FindText(needle)
			}
		}
		return
	}
	if a.doc.isWhiteboard() {
		needle := strings.ToLower(strings.TrimSpace(term))
		if needle == "" {
			needle = strings.ToLower(strings.TrimSpace(query))
		}
		if needle != "" {
			for _, el := range a.doc.board.Scene.Elements {
				if el == nil {
					continue
				}
				if strings.Contains(strings.ToLower(el.Text), needle) || strings.Contains(strings.ToLower(el.Name), needle) || strings.Contains(strings.ToLower(el.Link), needle) {
					a.doc.board.RevealElement(el.ID)
					break
				}
			}
		}
		return
	}
	if a.doc.source {
		a.doc.toggleMode()
	}
	a.doc.ed.FindTextWithFallback(strings.TrimSpace(query), term)
}

// docHint is where a document is, for the bar: its folder, and its space
// when that is not the active one.
func (a *App) docHint(id, rel string) string {
	if sp := a.state.Get(id); sp.IsExternal() {
		return shortPath(filepath.Dir(sp.ExternalFiles[rel]))
	}
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

func (a *App) syncSavedSearch(d *document) {
	if d == nil || d.savedWrites == d.notedWrites {
		return
	}
	d.notedWrites = d.savedWrites
	a.search.writeSeq++
	d.savedSeq = a.search.writeSeq
	a.refreshSearch()
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
		if d.loadErr != nil {
			continue
		}
		_, indexed := s.index.TextOf(d.space.ID, d.rel)
		stale := d.savedSeq > s.indexedSeq
		if !d.dirty() && !stale && (indexed || !visible[d]) {
			continue
		}
		docs = append(docs, d)
		stamp := liveStamp{docSearchRevision(d), place{d.space.ID, d.rel}}
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
		s.liveDocs[d] = liveStamp{docSearchRevision(d), place{d.space.ID, d.rel}}
		live = append(live, search.Live{Space: d.space.ID, Path: d.rel, Text: d.plainText()})
	}
	s.overlay, s.liveBase, s.overlayReady = s.index.WithLive(live), s.index, true
	return s.overlay
}

func docSearchRevision(d *document) uint64 {
	if d == nil {
		return 0
	}
	if d.isDatabase() {
		return d.dbView.Store.Revision()
	}
	if d.isWhiteboard() {
		return d.board.Revision()
	}
	return d.ed.Revision()
}

func extractDatabaseSearchText(name, content string) search.Extracted {
	db, err := database.DecodeDatabaseMarkdown(content)
	if err != nil {
		return search.Extracted{Text: vault.Title(name)}
	}
	var lines []string
	if strings.TrimSpace(db.Title) != "" {
		lines = append(lines, db.Title)
	} else {
		lines = append(lines, vault.Title(name))
	}
	if desc := strings.TrimSpace(db.Description); desc != "" {
		lines = append(lines, desc)
	}
	for _, p := range db.Properties {
		if strings.TrimSpace(p.Name) != "" {
			lines = append(lines, p.Name)
		}
		for _, opt := range p.Options {
			if strings.TrimSpace(opt.Name) != "" {
				lines = append(lines, opt.Name)
			}
		}
	}
	for _, v := range db.Views {
		if strings.TrimSpace(v.Name) != "" {
			lines = append(lines, v.Name)
		}
	}
	for _, r := range db.InlineRows {
		if r == nil {
			continue
		}
		if r.Title != "" {
			lines = append(lines, r.Title)
		}
		for _, p := range db.Properties {
			if p.Type == database.PropTitle {
				continue
			}
			if s := database.FormatValue(p, r.ValueFor(p)); s != "" {
				lines = append(lines, s)
			}
		}
		if r.Body != "" {
			lines = append(lines, r.Body)
		}
	}
	const snapStart = "<!-- arcmd-db:snapshot:start -->"
	const snapEnd = "<!-- arcmd-db:snapshot:end -->"
	if sIdx := strings.Index(content, snapStart); sIdx >= 0 {
		rest := content[sIdx+len(snapStart):]
		if eIdx := strings.Index(rest, snapEnd); eIdx >= 0 {
			rest = rest[:eIdx]
		}
		if txt := strings.TrimSpace(rest); txt != "" {
			lines = append(lines, txt)
		}
	}
	return search.Extracted{Text: strings.Join(lines, "\n"), Icon: db.Icon}
}

func databasePlainText(store *database.Store) string {
	if store == nil {
		return ""
	}
	db := store.Database()
	if db == nil {
		return ""
	}
	var lines []string
	if strings.TrimSpace(db.Title) != "" {
		lines = append(lines, db.Title)
	}
	if desc := strings.TrimSpace(db.Description); desc != "" {
		lines = append(lines, desc)
	}
	for _, p := range db.Properties {
		if p.Name != "" {
			lines = append(lines, p.Name)
		}
	}
	for _, v := range db.Views {
		if v.Name != "" {
			lines = append(lines, v.Name)
		}
	}
	for _, rec := range store.Records() {
		if rec == nil {
			continue
		}
		var parts []string
		if rec.Title != "" {
			parts = append(parts, rec.Title)
		}
		for _, p := range db.Properties {
			if p.Type == database.PropTitle {
				continue
			}
			if s := database.FormatValue(p, rec.ValueFor(p)); s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			lines = append(lines, strings.Join(parts, " "))
		}
	}
	return strings.Join(lines, "\n")
}
