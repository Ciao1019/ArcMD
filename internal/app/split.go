package app

import (
	"slices"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// Split geometry follows Cra's SplitPaneLayout (MIT, Victor Kildahl):
// ten-point gaps, 200-point minimums, six-point equal-share snapping.
const splitGap, splitMinimum, splitSnap = float32(10), float32(200), float32(6)
const splitLimit = 4

type splitPane struct {
	doc     *document
	history []place
	at      int
	factor  float32
	bounds  ui.Rect
}

// A group belongs to a space session. Leaving it keeps its editors and
// ratios, so choosing its sidebar row restores the same workspace.
type splitGroup struct {
	panes    []*splitPane
	active   int
	vertical bool
	// The raw drag distance is independent of visual snapping/clamping.
	dragPane                 *splitPane
	dragOrigin, dragDistance float32
}

func (a *App) syncSplit() {
	if a.documentContext > 0 {
		return
	}
	if s := a.split; s != nil && s.active < len(s.panes) {
		p := s.panes[s.active]
		p.doc, p.history, p.at = a.doc, a.history, a.at
	}
}

func (a *App) focusPane(index int, keyboard bool) {
	s := a.split
	if s == nil || index < 0 || index >= len(s.panes) {
		return
	}
	a.syncSplit()
	if a.doc != nil && s.active != index {
		a.doc.ed.UnmarkText()
	}
	s.active = index
	p := s.panes[index]
	a.doc, a.history, a.at = p.doc, p.history, p.at
	if keyboard && a.doc != nil {
		a.doc.ed.RequestFocus = true
	}
}

func (a *App) activateDocument(d *document) bool {
	if a.doc == d {
		return true
	}
	if s := a.split; s != nil {
		for i, p := range s.panes {
			if p.doc == d {
				a.focusPane(i, false)
				return true
			}
		}
	}
	return false
}

func (a *App) cyclePane() {
	if a.split != nil {
		a.focusPane((a.split.active+1)%len(a.split.panes), true)
	}
}

func (a *App) beginSplit() bool {
	if a.split != nil {
		return true
	}
	if a.doc == nil || !a.flush() {
		return false
	}
	s := &splitGroup{panes: []*splitPane{{doc: a.doc, history: slices.Clone(a.history), at: a.at, factor: 1}}}
	a.split = s
	a.session(a.state.ActiveSpace().ID).split = s
	a.focus = false
	return true
}

func (a *App) addSplitPane() {
	if !a.beginSplit() {
		return
	}
	if len(a.split.panes) >= splitLimit {
		a.notify("最多同时打开四个分屏。")
		return
	}
	a.syncSplit()
	index := a.split.active + 1
	a.split.panes = slices.Insert(a.split.panes, index, &splitPane{})
	a.equalSplit()
	a.focusPane(index, false)
	a.openPalette()
	a.palette.pane = a.split.panes[index]
}

func (a *App) equalSplit() {
	if s := a.split; s != nil {
		for _, p := range s.panes {
			p.factor = 1 / float32(len(s.panes))
		}
	}
}

// openSplit adds a document without moving or duplicating its file. The
// same document never has two independently writable editor instances.
func (a *App) openSplit(sp *space.Space, rel string, leading bool, vertical bool) {
	if sp == nil || !vault.IsDocument(rel) {
		return
	}
	if s := a.split; s != nil {
		a.syncSplit()
		for i, p := range s.panes {
			if p.doc != nil && p.doc.space == sp && p.doc.rel == rel {
				a.focusPane(i, true)
				return
			}
		}
	}
	if a.doc != nil && a.doc.space == sp && a.doc.rel == rel {
		a.addSplitPane()
		return
	}
	if a.split != nil && a.doc == nil {
		a.fillPane(a.split.panes[a.split.active], sp.ID, rel)
		return
	}
	text, err := vault.Read(sp.Root, rel)
	if err != nil {
		a.notify("打开 %s 失败。%v", vault.Title(rel), err)
		return
	}
	if !a.flush() || !a.beginSplit() {
		return
	}
	s := a.split
	if len(s.panes) >= splitLimit {
		a.notify("最多同时打开四个分屏。")
		return
	}
	d := a.residentDocument(sp.ID, rel)
	if d == nil {
		d = openDocument(sp, rel, text, false)
	}
	index := s.active + 1
	if leading {
		index--
	}
	a.syncSplit()
	s.panes = slices.Insert(s.panes, index, &splitPane{doc: d, history: []place{{sp.ID, rel}}})
	if leading {
		s.active++
	}
	if len(s.panes) == 2 {
		s.vertical = vertical
	}
	a.equalSplit()
	a.focusPane(index, true)
}

func (a *App) closePane(index int) {
	s := a.split
	if s == nil || index < 0 || index >= len(s.panes) || !a.flush() {
		return
	}
	a.syncSplit()
	s.panes = slices.Delete(s.panes, index, index+1)
	if index < s.active {
		s.active--
	}
	s.active = min(s.active, len(s.panes)-1)
	a.selectSplitActive()
	if len(s.panes) < 2 {
		a.split = nil
		if sp := a.state.ActiveSpace(); sp != nil {
			a.session(sp.ID).split = nil
		}
	} else {
		a.equalSplit()
	}
	if a.doc != nil {
		a.doc.ed.RequestFocus = true
	}
}

func (a *App) selectSplitActive() {
	if s := a.split; s != nil && len(s.panes) > 0 {
		p := s.panes[s.active]
		a.doc, a.history, a.at = p.doc, p.history, p.at
	}
}

func (a *App) separateSplit() {
	s := a.split
	if s == nil || !a.flush() {
		return
	}
	a.syncSplit()
	s.panes = slices.Delete(s.panes, s.active, s.active+1)
	s.active = min(s.active, len(s.panes)-1)
	a.equalSplit()
	a.split = nil
	session := a.session(a.state.ActiveSpace().ID)
	session.splitShown = false
	if len(s.panes) < 2 {
		session.split = nil
	}
	if a.doc != nil {
		a.doc.ed.RequestFocus = true
	}
}

func (a *App) suspendSplit() { a.syncSplit(); a.split = nil }

func (a *App) restoreSplit(s *splitGroup, index int) bool {
	if s == nil || !a.flush() {
		return false
	}
	a.suspendSplit()
	a.split = s
	s.active = min(max(index, 0), len(s.panes)-1)
	a.selectSplitActive()
	return true
}

// residentDocuments includes the suspended groups and pinned editors, so
// file watches, moves and link rewrites cannot overlook a second editor.
func (a *App) residentDocuments() []*document {
	a.syncSplit()
	seen := map[*document]bool{}
	var out []*document
	add := func(d *document) {
		if d != nil && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	add(a.doc)
	for _, s := range a.sessions {
		add(s.doc)
		if s.split != nil {
			for _, p := range s.split.panes {
				add(p.doc)
			}
		}
	}
	for _, d := range a.openDocs {
		add(d)
	}
	return out
}

func (a *App) residentDocument(id, rel string) *document {
	for _, d := range a.residentDocuments() {
		if d.space.ID == id && d.rel == rel {
			return d
		}
	}
	return nil
}

func (a *App) visibleDocuments() []*document {
	a.syncSplit()
	if a.split == nil {
		if a.doc != nil {
			return []*document{a.doc}
		}
		return nil
	}
	var out []*document
	for _, p := range a.split.panes {
		if p.doc != nil {
			out = append(out, p.doc)
		}
	}
	return out
}

// withDocument runs a file operation in its document's context without
// changing the selected pane. Replacements propagate to every reference.
func (a *App) withDocument(d *document, fn func()) {
	a.syncSplit()
	selected := a.doc
	a.doc = d
	a.documentContext++
	fn()
	a.documentContext--
	replacement := a.doc
	for _, s := range a.sessions {
		if s.doc == d {
			s.doc = replacement
		}
		if s.split != nil {
			for _, p := range s.split.panes {
				if p.doc == d {
					p.doc = replacement
				}
			}
		}
	}
	for key, v := range a.openDocs {
		if v == d {
			if replacement == nil {
				delete(a.openDocs, key)
			} else {
				a.openDocs[key] = replacement
			}
		}
	}
	if selected != d {
		a.doc = selected
	}
}

func splitLengths(panes []*splitPane, total float32) []float32 {
	available := max(0, total-splitGap*float32(len(panes)-1))
	minimum := min(splitMinimum, available/float32(len(panes)))
	lengths := make([]float32, len(panes))
	sum := float32(0)
	for _, p := range panes {
		sum += max(0, p.factor)
	}
	for i, p := range panes {
		if sum == 0 {
			lengths[i] = available / float32(len(panes))
		} else {
			lengths[i] = available * max(0, p.factor) / sum
		}
	}
	for range panes {
		deficit, flex := float32(0), float32(0)
		for i, n := range lengths {
			if n < minimum {
				deficit += minimum - n
				lengths[i] = minimum
			} else {
				flex += n - minimum
			}
		}
		if deficit == 0 || flex == 0 {
			break
		}
		for i, n := range lengths {
			if n > minimum {
				lengths[i] -= deficit * (n - minimum) / flex
			}
		}
	}
	return lengths
}

// fillPane replaces just the selected pane, retaining its navigation history.
func (a *App) fillPane(pane *splitPane, id, rel string) bool {
	s := a.split
	if s == nil {
		return false
	}
	index := slices.Index(s.panes, pane)
	if index < 0 || a.state.Get(id) == nil {
		return false
	}
	a.focusPane(index, false)
	a.paneNavigation = true
	defer func() { a.paneNavigation = false; a.syncSplit() }()
	a.openIn(a.state.Get(id), rel)
	return a.doc != nil && a.doc.space.ID == id && a.doc.rel == rel
}

func (a *App) runPaletteAction(act action) {
	pane := a.palette.pane
	if pane != nil && act.doc.rel == "" {
		act.run()
		a.palette.pane = pane
		return
	}
	a.palette.open = false
	a.palette.pane = nil
	if pane != nil && act.doc.rel != "" {
		if a.fillPane(pane, act.doc.space, act.doc.rel) && act.kind == "内容" && a.doc.loadErr == nil {
			a.doc.ed.FindText(a.palette.query)
		}
		return
	}
	act.run()
}

func (a *App) replaceReferences(old, replacement *document) {
	if a.doc == old {
		a.doc = replacement
	}
	for _, session := range a.sessions {
		if session.doc == old {
			session.doc = replacement
		}
		if session.split != nil {
			for _, p := range session.split.panes {
				if p.doc == old {
					p.doc = replacement
				}
			}
		}
	}
	for key, d := range a.openDocs {
		if d == old {
			delete(a.openDocs, key)
			if replacement != nil {
				a.openDocs[place{replacement.space.ID, replacement.rel}] = replacement
			}
		}
	}
}

func (a *App) followSplitPaths(fromID, from, toID, to string) {
	for _, d := range a.residentDocuments() {
		if d.space.ID == fromID && within(d.rel, from) {
			d.space = a.state.Get(toID)
			d.rel = to + d.rel[len(from):]
		}
	}
	rewrite := func(h []place) {
		for i, p := range h {
			if p.space == fromID && within(p.rel, from) {
				h[i] = place{toID, to + p.rel[len(from):]}
			}
		}
	}
	rewrite(a.history)
	for _, session := range a.sessions {
		if session.split != nil {
			for _, p := range session.split.panes {
				rewrite(p.history)
			}
		}
	}
}

// Ordinary documents belong to their group's segmented sidebar row. Pins
// and favorites keep their established location, as in Cra.
func (a *App) grouped(sp *space.Space, rel string) bool {
	if inPins(sp, rel) {
		return false
	}
	if s := a.session(sp.ID).split; s != nil {
		for _, p := range s.panes {
			if p.doc != nil && p.doc.space == sp && p.doc.rel == rel {
				return true
			}
		}
	}
	return false
}

// A suspended group's conflict must be reachable even when it prevents
// navigation. Reveal it directly, without re-entering the save guard.
func (a *App) showSaveFailure(d *document) {
	if a.activateDocument(d) {
		d.ed.RequestFocus = true
		return
	}
	if a.documentContext > 0 {
		return
	}
	if sp := a.state.ActiveSpace(); sp != nil {
		session := a.session(sp.ID)
		session.doc, session.splitShown = a.doc, a.split != nil
	}
	a.suspendSplit()
	for id, session := range a.sessions {
		if s := session.split; s != nil {
			for i, p := range s.panes {
				if p.doc == d {
					a.state.SetActive(id)
					a.syncTree()
					a.split = s
					s.active = i
					a.selectSplitActive()
					return
				}
			}
		}
	}
	a.doc = d
	a.history = []place{{d.space.ID, d.rel}}
	a.at = 0
}

func (a *App) navigateInPane(d *document, fn func()) {
	if !a.activateDocument(d) {
		return
	}
	previous := a.paneNavigation
	a.paneNavigation = a.split != nil
	defer func() { a.paneNavigation = previous; a.syncSplit() }()
	fn()
}
