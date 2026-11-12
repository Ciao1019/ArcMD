package app

import (
	"path"
	"time"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// Files and folders move to another space as Cra's tabs do: a row dragged
// onto a space's icon in the space bar lights it up and, held there for
// half a second, opens that space, the drag going on into its list to land
// where it is let go; let go on the icon, it goes to the end of that
// space's root. The row's menu has 「移到空间 ▸」 too. The space they left
// forgets them; their favorites go with them.

// spaceSpringDelay is how long a drag rests on a space's icon before that
// space opens under it, as Cra's 0.5 s.
const spaceSpringDelay = 500 * time.Millisecond

// spaceSpring is the space icon held under a drag, and since when.
type spaceSpring struct {
	id    string
	since time.Time
}

// foreignKey stands for a dragged row of another space in resolveDrop,
// whose checks against the row's own place must not match this space's
// rows: no path holds a NUL.
func foreignKey(rel string) string { return "\x00" + rel }

// foreign reports whether d is a row of a space other than the active one.
func (a *App) foreign(d dragged) bool {
	sp := a.state.ActiveSpace()
	return sp != nil && d.space != sp.ID
}

// sees records that the row d is being dragged this frame. Once its space
// pages away its page stays built for it (pagerView), as MyGo keeps a drag
// only while its source is built, and draws the drag's copy from it.
func (a *App) sees(d dragged) { a.carrying = &d }

// spaceIconDrag makes the space bar's icon b of the space s take rows:
// it reports whether one is over it, opens s after it rests there, and
// moves it to the end of s's root when let go.
func (a *App) spaceIconDrag(c *ui.Context, b ui.Element, s *space.Space, on bool) bool {
	if d, ok := ui.Drop[dragged](b); ok {
		a.spaceSpring = spaceSpring{}
		if d.space != s.ID {
			a.moveAcross(d.space, d.rel, s.ID, "")
		}
		return false
	}
	d, over := ui.DragOver[dragged](b)
	if !over {
		if a.spaceSpring.id == s.ID {
			a.spaceSpring = spaceSpring{}
		}
		return false
	}
	if on {
		return d.space != s.ID
	}
	switch wait := spaceSpringDelay - c.Now().Sub(a.spaceSpring.since); {
	case a.spaceSpring.id != s.ID:
		a.spaceSpring = spaceSpring{s.ID, c.Now()}
		c.After(spaceSpringDelay)
	case wait <= 0:
		a.spaceSpring = spaceSpring{}
		a.switchSpace(s.ID)
	default:
		c.After(wait)
	}
	return true
}

// spacesMenu is 「移到空间 ▸」: the other spaces, each taking the row at rel
// of the space sp to the end of its root.
func (a *App) spacesMenu(m *ui.Menu, sp *space.Space, rel string) {
	if len(a.state.Spaces) < 2 {
		return
	}
	m.Submenu(i18n.T("移到空间"), func(sub *ui.Menu) {
		for _, s := range a.state.Spaces {
			name := s.Name
			if s.Icon != "" {
				name = s.Icon + " " + s.Name
			}
			if sub.Item(name).Disabled(s.ID == sp.ID).Chosen() {
				a.moveAcross(sp.ID, rel, s.ID, "")
			}
		}
	})
}

// moveAcross moves the file or folder at rel of the space fromID into the
// folder dir of the space toID and returns its path there, "" when it did
// not move. The open document going along stays open, now of that space,
// and the window follows it there unless it is a favorite, which shows in
// every space.
func (a *App) moveAcross(fromID, rel, toID, dir string) string {
	src, dst := a.state.Get(fromID), a.state.Get(toID)
	if src == nil || dst == nil || src == dst {
		return ""
	}
	if !a.flush() {
		return ""
	}
	to, copied, err := a.moveWithResources(src, rel, dst, dir)
	if err != nil {
		a.notify("移动 %s 到「%s」失败。%v", path.Base(rel), dst.Name, err)
		return ""
	}
	if copied {
		// Another disk: the copy is there; the original goes to the trash.
		if p, err := vault.Resolve(src.Root, rel); err == nil {
			if err := a.trashItem(p); err != nil {
				a.notify("已复制 %s 到「%s」，但未能将原件移到废纸篓。%v", path.Base(rel), dst.Name, err)
			}
		}
	}
	a.state.MovedAcross(fromID, rel, toID, to)
	if dir != "" {
		dst.SetExpanded(dir, true)
	}
	follow := a.split == nil && a.doc != nil && a.doc.space == src && within(a.doc.rel, rel)
	a.followOpen(fromID, rel, toID, to)
	a.followSplitPaths(fromID, rel, toID, to)
	a.saveState()
	if a.doc != nil && a.state.IsFavorite(a.doc.space.ID, a.doc.rel) {
		follow = false
	}
	if session := a.sessions[fromID]; session != nil && session.doc != nil && session.doc.space == dst {
		session.doc = nil
	}
	delete(a.pageTrees, fromID)
	delete(a.pageTrees, toID)
	a.refreshTree()
	if follow && a.state.ActiveSpace() != dst {
		a.followTo(dst)
	}
	a.offerUndo(i18n.T("已移到空间「")+dst.Name+"」", path.Base(to), func() { a.moveAcross(toID, to, fromID, parentDir(rel)) })
	return to
}

// followTo makes sp active, keeping the open document, which moved there.
func (a *App) followTo(sp *space.Space) {
	from := a.pagerShown(a.now)
	if cur := a.state.ActiveSpace(); cur != nil {
		a.session(cur.ID).doc = nil
	}
	a.state.SetActive(sp.ID)
	sp.LastOpen = a.doc.rel
	a.saveState()
	a.renaming = ""
	a.syncTree()
	a.pager.swipe = 0
	a.pagerSettle(a.spaceIndex(sp.ID), from)
	delete(a.pageTrees, sp.ID)
}

// dropForeign moves the row d of another space where the list's drop says,
// into this space.
func (a *App) dropForeign(d dragged, drop *listDrop) {
	sp := a.state.ActiveSpace()
	if drop == nil || drop.kind == 0 {
		drop = &listDrop{kind: dropInsert, parent: "", index: len(a.tree)}
	}
	dir := drop.parent
	if dir == pinRoot {
		dir = "" // pinned, it sits in the root
	}
	to := a.moveAcross(d.space, d.rel, sp.ID, dir)
	if to == "" {
		return
	}
	drop.rel = to
	a.applyDrop(drop)
}
