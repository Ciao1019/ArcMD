package app

import (
	"reflect"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

// Only paths and geometry are persisted; editor contents use the ordinary
// document save path. Missing files restore as an empty, replaceable pane.
func (a *App) captureSplits() bool {
	if a.documentContext > 0 || a.splitResizing {
		return false
	}
	a.syncSplit()
	changed := false
	for _, sp := range a.state.Spaces {
		session := a.sessions[sp.ID]
		if session == nil || session.splitDeferred {
			continue
		}
		var saved *space.Split
		if s := session.split; s != nil && len(s.panes) >= 2 {
			shown := session.splitShown
			if sp == a.state.ActiveSpace() {
				shown = a.split == s
			}
			saved = &space.Split{Active: s.active, Vertical: s.vertical, Shown: shown}
			for _, p := range s.panes {
				ref := space.SplitPane{Share: p.factor}
				if p.doc != nil {
					ref.Space, ref.Path = p.doc.space.ID, p.doc.rel
				}
				saved.Panes = append(saved.Panes, ref)
			}
		}
		if !reflect.DeepEqual(sp.Split, saved) {
			sp.Split = saved
			changed = true
		}
	}
	return changed
}

func (a *App) restoreSpaceSplit(sp *space.Space) {
	if sp == nil {
		return
	}
	saved := sp.Split
	if saved == nil || len(saved.Panes) < 2 || len(saved.Panes) > splitLimit {
		return
	}
	s := &splitGroup{active: min(max(saved.Active, 0), len(saved.Panes)-1), vertical: saved.Vertical}
	seen := map[place]bool{}
	for _, ref := range saved.Panes {
		p := &splitPane{factor: max(0, ref.Share)}
		key := place{ref.Space, ref.Path}
		if source := a.state.Get(ref.Space); source != nil && vault.IsDocument(ref.Path) && !seen[key] {
			if res := a.residentDocument(ref.Space, ref.Path); res != nil {
				p.doc = res
				p.history = []place{key}
				seen[key] = true
			} else if text, err := readDocSource(source, ref.Path); err == nil {
				p.doc = openDocument(source, ref.Path, text, false)
				p.history = []place{key}
				seen[key] = true
			}
		}
		s.panes = append(s.panes, p)
	}
	session := a.session(sp.ID)
	session.split, session.splitShown = s, saved.Shown
}

func (a *App) ensureSplitRestored(sp *space.Space) {
	if sp == nil {
		return
	}
	session := a.sessions[sp.ID]
	if session == nil || !session.splitDeferred {
		return
	}
	session.splitDeferred = false
	a.restoreSpaceSplit(sp)
}

func (a *App) restoreSplits() {
	active := a.state.ActiveSpace()
	for _, sp := range a.state.Spaces {
		saved := sp.Split
		if saved == nil || len(saved.Panes) < 2 || len(saved.Panes) > splitLimit {
			continue
		}
		if sp != active {
			a.session(sp.ID).splitDeferred = true
			continue
		}
		a.restoreSpaceSplit(sp)
	}
	if active != nil {
		session := a.session(active.ID)
		if session.splitShown && session.split != nil {
			a.split = session.split
			a.selectSplitActive()
		}
	}
}
