package app

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/trashbin"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// Sidebar multi-selection follows Cra's SpaceListView / SidebarUIState (MIT):
// Cmd-click (Ctrl-click on Windows and Linux) toggles an item while keeping
// the open document in the selection when one starts; Shift-click adds the
// contiguous run of visible rows from the anchor; Escape or an unmodified
// click clears the selection. Dragging or right-clicking any picked row acts
// on every picked item in sidebar order, deduplicating descendants whose
// parent folder is already moving with them.

// clearPicks drops the sidebar's multi-selection and sets the anchor for the
// next Shift-click range.
func (a *App) clearPicks(anchor string) {
	if len(a.picks) > 0 {
		clear(a.picks)
	}
	a.pickAnchor = anchor
	if anchor != "" {
		if sp := a.state.ActiveSpace(); sp != nil {
			a.picksSpace = sp.ID
		}
	} else {
		a.picksSpace = ""
	}
}

// hasPicks reports whether the space sp has an active sidebar multi-selection.
func (a *App) hasPicks(sp *space.Space) bool {
	return sp != nil && a.picksSpace == sp.ID && len(a.picks) > 0
}

// rowHighlighted reports whether the row at rel of sp draws with the chosen
// glass fill: while rows are picked with Cmd/Shift-click, the picked set
// owns the highlight instead of the single open document, as in Cra.
func (a *App) rowHighlighted(sp *space.Space, rel string, chosen bool) bool {
	if a.hasPicks(sp) {
		return a.picks[rel]
	}
	return chosen
}

// listedNodes returns the visible folder and document nodes of sp's sidebar
// list (pinned then today) in top-to-bottom visual order, descending into
// expanded folders and closed folders' peeked open document.
func (a *App) listedNodes(sp *space.Space) []*vault.Node {
	if sp == nil {
		return nil
	}
	nodes, err := a.treeOf(sp)
	if err != nil {
		return nil
	}
	var out []*vault.Node
	var walk func(items []*vault.Node, rootPins bool)
	walk = func(items []*vault.Node, rootPins bool) {
		for _, n := range items {
			if rootPins {
				if sp.PinsCollapsed && !a.holdsOpen(sp, n.Path) {
					continue
				}
			} else if a.hidden(sp, n) {
				continue
			}
			out = append(out, n)
			if !n.Dir {
				continue
			}
			if sp.IsExpanded(n.Path) {
				walk(n.Children, false)
			} else if a.holdsOpen(sp, n.Path) && a.doc.rel != n.Path {
				if doc := vault.Find(n.Children, a.doc.rel); doc != nil && !a.hidden(sp, doc) {
					out = append(out, doc)
				}
			}
		}
	}
	walk(pinnedNodes(sp, nodes), true)
	walk(nodes, false)
	return out
}

// listedPaths returns the paths of sp's visible sidebar rows in top-to-bottom
// visual order.
func (a *App) listedPaths(sp *space.Space) []string {
	nodes := a.listedNodes(sp)
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Path
	}
	return out
}

// prunePicks removes paths from the multi-selection that are no longer
// present in their space's tree.
func (a *App) prunePicks(sp *space.Space, nodes []*vault.Node) {
	if sp == nil || a.picksSpace != sp.ID || len(a.picks) == 0 {
		return
	}
	for p := range a.picks {
		n := vault.Find(nodes, p)
		if n == nil || (!n.Dir && (a.state.IsFavorite(sp.ID, p) || a.grouped(sp, p))) {
			delete(a.picks, p)
		}
	}
	if len(a.picks) == 0 {
		a.picksSpace = ""
	}
}

// followPicks rewrites picked paths and the Shift-click anchor when a file
// or folder moves from one path to another within sp.
func (a *App) followPicks(spID, from, to string) {
	if a.picksSpace != spID || from == to {
		return
	}
	if under(a.pickAnchor, from) {
		a.pickAnchor = to + a.pickAnchor[len(from):]
	}
	if len(a.picks) == 0 {
		return
	}
	updated := make(map[string]bool, len(a.picks))
	for p := range a.picks {
		if under(p, from) {
			updated[to+p[len(from):]] = true
		} else {
			updated[p] = true
		}
	}
	a.picks = updated
}

// forgetPicks drops rel and anything inside it from the sidebar selection.
func (a *App) forgetPicks(spID, rel string) {
	if a.picksSpace != spID {
		return
	}
	if under(a.pickAnchor, rel) {
		a.pickAnchor = ""
	}
	for p := range a.picks {
		if under(p, rel) {
			delete(a.picks, p)
		}
	}
	if len(a.picks) == 0 {
		a.picksSpace = ""
	}
}

// pickRow handles Cmd-click (range == false) and Shift-click (range == true)
// on the sidebar row rel of sp. As in Finder and Cra, the open document in
// the list counts as already picked when a selection begins.
func (a *App) pickRow(sp *space.Space, rel string, spanRange bool) {
	if sp == nil {
		return
	}
	a.closePreview(a.now)
	if a.picksSpace != sp.ID {
		a.picks = nil
		a.pickAnchor = ""
	}
	order := a.listedPaths(sp)
	open := ""
	if a.doc != nil && a.doc.space == sp && slices.Contains(order, a.doc.rel) {
		open = a.doc.rel
	}
	if len(a.picks) == 0 && open != "" {
		a.picks = map[string]bool{open: true}
	}
	if a.picks == nil {
		a.picks = make(map[string]bool)
	}
	a.picksSpace = sp.ID
	if spanRange {
		anchor := a.pickAnchor
		if anchor == "" || !slices.Contains(order, anchor) {
			anchor = open
		}
		if anchor == "" {
			anchor = rel
		}
		ia := slices.Index(order, anchor)
		ib := slices.Index(order, rel)
		if ia >= 0 && ib >= 0 {
			for i := min(ia, ib); i <= max(ia, ib); i++ {
				a.picks[order[i]] = true
			}
			if a.pickAnchor == "" {
				a.pickAnchor = anchor
			}
		} else {
			a.picks[rel] = true
			a.pickAnchor = rel
		}
	} else {
		if a.picks[rel] {
			delete(a.picks, rel)
		} else {
			a.picks[rel] = true
		}
		a.pickAnchor = rel
	}
	if len(a.picks) == 0 {
		a.picksSpace = ""
	}
}

// rawGroupFor returns every picked path of spID in sidebar order when rel is
// one of several picks, or just [rel] otherwise.
func (a *App) rawGroupFor(spID, rel string) []string {
	if a.picksSpace != spID || len(a.picks) <= 1 || !a.picks[rel] {
		return []string{rel}
	}
	sp := a.state.Get(spID)
	if sp == nil {
		return []string{rel}
	}
	var out []string
	for _, p := range a.listedPaths(sp) {
		if a.picks[p] {
			out = append(out, p)
		}
	}
	// Include any picked path currently hidden inside a collapsed folder.
	for p := range a.picks {
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	if len(out) <= 1 {
		return []string{rel}
	}
	return out
}

// topLevelPaths drops paths that already live inside another folder in the
// slice so moving or trashing a folder together with its children keeps the
// folder's hierarchy intact.
func topLevelPaths(rels []string) []string {
	if len(rels) <= 1 {
		return rels
	}
	out := make([]string, 0, len(rels))
	for _, p := range rels {
		nested := false
		for _, other := range rels {
			if other != p && under(p, other) {
				nested = true
				break
			}
		}
		if !nested {
			out = append(out, p)
		}
	}
	return out
}

// dragGroup returns the top-level items a drag or batch action on rel of
// spID moves together, in sidebar order.
func (a *App) dragGroup(spID, rel string) []string {
	return topLevelPaths(a.rawGroupFor(spID, rel))
}

// activePickedRaw returns all picked paths in the active space in sidebar
// order when at least two rows are picked.
func (a *App) activePickedRaw() []string {
	sp := a.state.ActiveSpace()
	if sp == nil || a.picksSpace != sp.ID || len(a.picks) <= 1 {
		return nil
	}
	var out []string
	for _, p := range a.listedPaths(sp) {
		if a.picks[p] {
			out = append(out, p)
		}
	}
	for p := range a.picks {
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	if len(out) <= 1 {
		return nil
	}
	return out
}

// activePickedGroup returns the top-level picked items of the active space
// when a multi-selection of more than one row is active.
func (a *App) activePickedGroup() []string {
	raw := a.activePickedRaw()
	if len(raw) == 0 {
		return nil
	}
	return topLevelPaths(raw)
}

// inDraggedGroup reports whether rel in spID belongs to the currently lifted
// drag bundle (excluding the primary grabbed row itself, which collapses to
// zero size) so companion rows dim in place while dragged.
func (a *App) inDraggedGroup(spID, rel string) bool {
	if !a.dragGrab.active || a.dragGrab.space != spID || rel == a.dragGrab.rel {
		return false
	}
	for _, m := range a.dragGroup(spID, a.dragGrab.rel) {
		if under(rel, m) {
			return true
		}
	}
	return false
}

// modalBlocksSidebarKeys reports whether a modal dialog, rename input or drag
// is active that should block sidebar file shortcuts (Cmd+C / Cmd+V / Cmd+⌫).
// Passive hover previews do not block focused sidebar shortcuts.
func (a *App) modalBlocksSidebarKeys() bool {
	return a.palette.open || a.settingsOpen || a.replace.open || a.tagRename.open ||
		a.versions.open || a.updates.window || a.updates.communityOpen || a.pdfExport != nil || a.pluginModalOpen() ||
		a.unsaved.open || a.library.open || a.spaceForm.open || a.renaming != "" || a.tour.open ||
		a.carrying != nil || a.carried != nil
}

// modalOrOverlayBlocksEsc reports whether an overlay, rename field or dialog
// is open that should handle Escape before sidebar multi-selection clears.
func (a *App) modalOrOverlayBlocksEsc() bool {
	return a.modalBlocksSidebarKeys() || (a.preview.shown != nil && !a.preview.closing)
}

// countLabel formats a concise item count description for batch undo toasts.
func countLabel(rels []string) string {
	if len(rels) == 1 {
		return path.Base(rels[0])
	}
	return fmt.Sprintf(i18n.T("%d 个项目"), len(rels))
}

// pinGroup pins every item in group starting at visual index among sp's
// shown pins, preserving their relative order and removing any from
// Favorites.
func (a *App) pinGroup(sp *space.Space, group []string, index int) {
	if sp == nil || len(group) == 0 {
		return
	}
	if len(group) == 1 {
		a.pinIn(sp, group[0], index)
		return
	}
	shown := pinnedNodes(sp, a.tree)
	inGroup := make(map[string]bool, len(group))
	for _, p := range group {
		inGroup[p] = true
	}
	anchor := ""
	if index < 0 {
		index = 0
	}
	for i := index; i < len(shown); i++ {
		if !inGroup[shown[i].Path] {
			anchor = shown[i].Path
			break
		}
	}
	for _, p := range group {
		if fi := a.state.FavoriteIndex(sp.ID, p); fi >= 0 {
			a.state.ToggleFavorite(sp.ID, p)
		}
		sp.Unpin(p)
	}
	at := len(sp.Pins)
	if anchor != "" {
		if idx := slices.Index(sp.Pins, anchor); idx >= 0 {
			at = idx
		}
	}
	sp.Pins = slices.Insert(sp.Pins, at, group...)
	a.saveState()
}

type batchMoved struct {
	from, to string
	oldDir   string
	oldBack  int
	wasPin   bool
}

// placeEntries moves and orders every item of group into dir starting at
// visual index among dir's children, preserving group order and offering a
// single undo toast when files move across folders.
func (a *App) placeEntries(group []string, dir string, index int) {
	a.placeEntriesWithUndo(group, dir, index, true)
}

func (a *App) placeEntriesWithUndo(group []string, dir string, index int, withUndo bool) {
	if len(group) == 0 {
		return
	}
	if len(group) == 1 && withUndo {
		sp := a.state.ActiveSpace()
		if sp != nil && sp.IsPinned(group[0]) {
			sp.Unpin(group[0])
			a.saveState()
		}
		a.placeEntry(group[0], dir, index)
		return
	}
	sp := a.state.ActiveSpace()
	if sp == nil || !a.flush() {
		return
	}
	kids := a.childrenOf(dir)
	inGroup := make(map[string]bool, len(group))
	for _, p := range group {
		inGroup[p] = true
	}
	anchorName := ""
	if index < 0 {
		index = 0
	}
	for i := index; i < len(kids); i++ {
		if !inGroup[kids[i].Path] {
			anchorName = kids[i].Name
			break
		}
	}

	var movedItems []batchMoved
	finalNames := make([]string, 0, len(group))
	for _, rel := range group {
		wasPin := sp.IsPinned(rel)
		if wasPin {
			sp.Unpin(rel)
		}
		if parentDir(rel) == dir {
			finalNames = append(finalNames, path.Base(rel))
			continue
		}
		oldDir := parentDir(rel)
		back := slices.IndexFunc(a.childrenOf(oldDir), func(n *vault.Node) bool { return n.Path == rel })
		to, _, err := a.moveWithResources(sp, rel, sp, dir)
		if err != nil {
			a.notify("移动 %s 失败。%v", path.Base(rel), err)
			continue
		}
		a.moved(sp, rel, to)
		movedItems = append(movedItems, batchMoved{from: rel, to: to, oldDir: oldDir, oldBack: max(0, back), wasPin: wasPin})
		finalNames = append(finalNames, path.Base(to))
	}

	if dir != "" && len(movedItems) > 0 {
		sp.SetExpanded(dir, true)
	}

	// Build the destination folder's new manual order with finalNames
	// placed contiguously before anchorName.
	a.refreshTree()
	curKids := a.childrenOf(dir)
	inFinal := make(map[string]bool, len(finalNames))
	for _, name := range finalNames {
		inFinal[name] = true
	}
	rest := make([]string, 0, len(curKids))
	for _, k := range curKids {
		if !inFinal[k.Name] {
			rest = append(rest, k.Name)
		}
	}
	insertAt := len(rest)
	if anchorName != "" {
		if idx := slices.Index(rest, anchorName); idx >= 0 {
			insertAt = idx
		}
	}
	delete(sp.Sort, dir)
	if sp.Order == nil {
		sp.Order = map[string][]string{}
	}
	sp.Order[dir] = slices.Insert(rest, insertAt, finalNames...)
	a.saveState()
	a.refreshTree()

	if withUndo && len(movedItems) > 0 {
		movedPaths := make([]string, len(movedItems))
		for i, m := range movedItems {
			movedPaths[i] = m.to
		}
		a.offerUndo(i18n.T("已移到「")+folderName(sp, dir)+"」", countLabel(movedPaths), func() {
			if a.state.ActiveSpace() != sp {
				a.notify("已不在原空间，无法撤销移动。")
				return
			}
			for i := len(movedItems) - 1; i >= 0; i-- {
				m := movedItems[i]
				a.placeEntry(m.to, m.oldDir, m.oldBack)
				if m.wasPin {
					sp.Pin(path.Join(m.oldDir, path.Base(m.to)), len(sp.Pins))
				}
			}
			a.saveState()
			a.refreshTree()
		})
	}
}

// moveIntoGroup moves rel (and its companion picks when part of a
// multi-selection) to the end of dir.
func (a *App) moveIntoGroup(rel, dir string) {
	group := a.dragGroup(a.state.ActiveSpace().ID, rel)
	if len(group) == 1 {
		a.moveInto(rel, dir)
		return
	}
	a.placeEntries(group, dir, len(a.childrenOf(dir)))
}

// moveGroupAcross moves every item in group from fromID into dir of toID,
// preserving order and offering a batch undo toast.
func (a *App) moveGroupAcross(fromID string, group []string, toID, dir string) []string {
	if len(group) == 0 {
		return nil
	}
	if len(group) == 1 {
		to := a.moveAcross(fromID, group[0], toID, dir)
		a.clearPicks("")
		if to == "" {
			return nil
		}
		return []string{to}
	}
	src, dst := a.state.Get(fromID), a.state.Get(toID)
	if src == nil || dst == nil || src == dst || !a.flush() {
		return nil
	}
	type crossMoved struct {
		oldDir string
		to     string
	}
	var movedList []crossMoved
	var out []string
	follow := false
	for _, rel := range group {
		if moving := a.residentDocument(src.ID, rel); moving != nil {
			rel = moving.rel
		}
		oldDir := parentDir(rel)
		to, copied, err := a.moveWithResources(src, rel, dst, dir)
		if err != nil {
			a.notify("移动 %s 到「%s」失败。%v", path.Base(rel), dst.Name, err)
			continue
		}
		if copied {
			if p, err := resolveFile(src, rel); err == nil {
				if err := a.trashItem(p); err != nil {
					a.notify("已复制 %s 到「%s」，但未能将原件移到废纸篓。%v", path.Base(rel), dst.Name, err)
				}
			}
		}
		if a.split == nil && a.doc != nil && a.doc.space == src && within(a.doc.rel, rel) {
			follow = true
		}
		a.state.MovedAcross(fromID, rel, toID, to)
		if a.dragGrab.active && a.dragGrab.space == fromID && a.dragGrab.rel == rel {
			a.dragGrab.space, a.dragGrab.rel = toID, to
		}
		if src.IsExternal() {
			delete(src.ExternalFiles, rel)
		}
		a.followOpen(fromID, rel, toID, to)
		a.followCopied(fromID, rel, toID, to)
		a.followSplitPaths(fromID, rel, toID, to)
		movedList = append(movedList, crossMoved{oldDir: oldDir, to: to})
		out = append(out, to)
	}
	a.clearPicks("")
	if len(out) == 0 {
		return nil
	}
	if dir != "" {
		dst.SetExpanded(dir, true)
	}
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
	if !src.IsExternal() {
		a.offerUndo(i18n.T("已移到空间「")+dst.Name+"」", countLabel(out), func() {
			for i := len(movedList) - 1; i >= 0; i-- {
				m := movedList[i]
				a.moveAcross(toID, m.to, fromID, m.oldDir)
			}
		})
	}
	return out
}

// commonParentDir returns the deepest folder containing every path in rels,
// "" when they span the space root.
func commonParentDir(rels []string) string {
	if len(rels) == 0 {
		return ""
	}
	dir := parentDir(rels[0])
	for _, r := range rels[1:] {
		d := parentDir(r)
		for dir != "" && dir != d && !strings.HasPrefix(d, dir+"/") {
			dir = parentDir(dir)
		}
	}
	return dir
}

// newFolderWithGroup creates a new folder in the active space, moves every
// item in group into it in sidebar order, expands it, and starts inline
// renaming, as Cra's "New Folder with N Items".
func (a *App) newFolderWithGroup(group []string) {
	if len(group) == 0 {
		return
	}
	if len(group) == 1 {
		a.clearPicks("")
		a.newFolderWith(group[0])
		return
	}
	sp := a.state.ActiveSpace()
	if sp == nil || sp.IsExternal() || !a.flush() {
		return
	}
	allPinned := true
	firstPinIdx := -1
	for _, r := range group {
		if !sp.IsPinned(r) {
			allPinned = false
			break
		}
		if idx := slices.Index(sp.Pins, r); idx >= 0 && (firstPinIdx < 0 || idx < firstPinIdx) {
			firstPinIdx = idx
		}
	}
	dir := commonParentDir(group)
	if allPinned {
		dir = ""
	}
	// Remember where the first sibling in dir sat so the new folder takes
	// its spot instead of jumping to the end of the list.
	insertIdx := -1
	kids := a.childrenOf(dir)
	for _, r := range group {
		if parentDir(r) == dir && !sp.IsPinned(r) {
			if idx := slices.IndexFunc(kids, func(n *vault.Node) bool { return n.Path == r }); idx >= 0 {
				insertIdx = idx
				break
			}
		}
	}

	folder, err := vault.Mkdir(sp.Root, dir, i18n.T("新建文件夹"))
	if err != nil {
		a.notify("新建文件夹失败。%v", err)
		return
	}
	a.refreshTree()
	a.clearPicks("")
	a.placeEntriesWithUndo(group, folder, 0, false)
	if allPinned && firstPinIdx >= 0 {
		sp.Pin(folder, firstPinIdx)
	} else if insertIdx >= 0 {
		cur := a.childrenOf(dir)
		names := make([]string, len(cur))
		for i, n := range cur {
			names[i] = n.Name
		}
		sp.Place(dir, names, path.Base(folder), insertIdx)
	}
	sp.SetExpanded(folder, true)
	a.saveState()
	a.refreshTree()
	a.startRename(folder)
}

// trashGroup moves every item in group of the active space to the system's
// trash, keeping copies in trashbin so a single undo restores them all.
func (a *App) trashGroup(group []string) {
	if len(group) == 0 {
		return
	}
	if len(group) == 1 {
		a.clearPicks("")
		a.trash(group[0])
		return
	}
	sp := a.state.ActiveSpace()
	if sp == nil {
		return
	}
	if sp.IsExternal() {
		for _, rel := range group {
			a.forgetExternal(sp, rel)
		}
		a.clearPicks("")
		return
	}
	if !a.flush() {
		return
	}
	// Close or trash the active open document last so the editor does not
	// hop through siblings that are about to be trashed too.
	ordered := make([]string, 0, len(group))
	var activeItems []string
	for _, rel := range group {
		if a.doc != nil && a.doc.space == sp && within(a.doc.rel, rel) {
			activeItems = append(activeItems, rel)
		} else {
			ordered = append(ordered, rel)
		}
	}
	ordered = append(ordered, activeItems...)

	var keptItems []trashbin.Item
	var trashed []string
	for _, rel := range ordered {
		p, err := resolveFile(sp, rel)
		if err != nil {
			continue
		}
		kept, ok := a.keepDeleted(sp.ID, sp.Name, sp.Root, rel)
		if err := a.trashItem(p); err != nil {
			if ok {
				a.bin.Forget(kept)
			}
			a.notify("将 %s 移到废纸篓失败。%v", path.Base(rel), err)
			continue
		}
		if ok {
			keptItems = append(keptItems, kept)
		}
		for _, d := range a.residentDocuments() {
			if d.space == sp && within(d.rel, rel) {
				a.replaceReferences(d, nil)
			}
		}
		a.forgetCopied(sp.ID, rel)
		a.state.Forget(sp.ID, rel)
		trashed = append(trashed, rel)
	}
	a.clearPicks("")
	a.saveState()
	a.refreshTree()
	if len(keptItems) > 0 {
		a.offerUndo(i18n.T("已移到废纸篓"), countLabel(trashed), func() {
			for _, it := range keptItems {
				a.restoreDeleted(it)
			}
		})
	}
}

// openPickedInSplit opens 2..splitLimit picked documents together in a fresh
// equal-width split group, focusing the first document, as Cra's "Open in
// Split View" on picked tabs.
func (a *App) openPickedInSplit(sp *space.Space, docRels []string) {
	if sp == nil || len(docRels) < 2 || len(docRels) > splitLimit || !a.flush() {
		return
	}
	panes := make([]*splitPane, 0, len(docRels))
	share := 1 / float32(len(docRels))
	for _, rel := range docRels {
		if !vault.IsDocument(rel) {
			continue
		}
		d := a.residentDocument(sp.ID, rel)
		if d == nil {
			text, err := readDocSource(sp, rel)
			if err != nil {
				a.notify("打开 %s 失败。%v", vault.Title(rel), err)
				return
			}
			d = openDocument(sp, rel, text, false)
		}
		panes = append(panes, &splitPane{
			doc:         d,
			history:     []place{{sp.ID, rel}},
			at:          0,
			factor:      share,
			shownFactor: share,
			shownReady:  true,
		})
	}
	if len(panes) < 2 {
		return
	}
	a.suspendSplit()
	a.syncOpen()
	group := &splitGroup{panes: panes, active: 0}
	a.split = group
	a.session(a.state.ActiveSpace().ID).split = group
	a.focus = false
	a.clearPicks("")
	a.selectSplitActive()
	if a.doc != nil {
		a.doc.ed.RequestFocus = true
	}
}

// collectFolders lists every folder in nodes depth-first so nested folders
// can also be chosen in "移到文件夹 ▸".
func collectFolders(nodes []*vault.Node) []*vault.Node {
	var out []*vault.Node
	var walk func([]*vault.Node)
	walk = func(items []*vault.Node) {
		for _, n := range items {
			if n.Dir {
				out = append(out, n)
				walk(n.Children)
			}
		}
	}
	walk(nodes)
	return out
}

// folderMenuLabel formats a folder's path for the "移到文件夹 ▸" submenu:
// root folders show their name, nested folders show "Parent / Child".
func folderMenuLabel(rel string) string {
	return strings.ReplaceAll(rel, "/", " / ")
}

// pickedMenu builds the context menu when right-clicking one of several
// picked sidebar rows, mirroring Cra's PickedTabsMenu adapted to documents
// and folders.
func (a *App) pickedMenu(m *ui.Menu, sp *space.Space, rawGroup []string) {
	group := topLevelPaths(rawGroup)
	n := len(group)
	if n == 0 {
		return
	}

	anyDir := false
	var docPaths []string
	for _, p := range rawGroup {
		node := vault.Find(a.tree, p)
		if node != nil && node.Dir {
			anyDir = true
		} else if vault.IsDocument(p) {
			docPaths = append(docPaths, p)
		}
	}

	if !anyDir && len(docPaths) >= 2 {
		if m.Item(i18n.T("在分屏中打开")).Disabled(len(docPaths) > splitLimit).Chosen() {
			a.openPickedInSplit(sp, docPaths)
		}
	}

	var openPins []string
	for _, p := range rawGroup {
		if inPins(sp, p) && a.isOpen(sp, p) {
			openPins = append(openPins, p)
		}
	}
	if len(openPins) > 0 {
		if m.Item(i18n.T("关闭并保持置顶")).Chosen() {
			// Close the active document last so focus hops once at the end.
			for _, p := range openPins {
				if !a.isShown(sp, p) {
					a.closeOpen(sp, p)
					delete(a.picks, p)
				}
			}
			for _, p := range openPins {
				if a.isShown(sp, p) {
					a.closeOpen(sp, p)
					delete(a.picks, p)
				}
			}
			if len(a.picks) == 0 {
				a.picksSpace = ""
			}
		}
	}

	allPinned := true
	for _, p := range group {
		if !sp.IsPinned(p) {
			allPinned = false
			break
		}
	}
	pinLabel := i18n.T("置顶")
	if allPinned {
		pinLabel = i18n.T("取消置顶")
	}
	if m.Item(pinLabel).Chosen() {
		if allPinned {
			for _, p := range group {
				sp.Unpin(p)
			}
			a.saveState()
		} else {
			var toPin []string
			for _, p := range group {
				if !sp.IsPinned(p) {
					toPin = append(toPin, p)
				}
			}
			a.pinGroup(sp, toPin, len(pinnedNodes(sp, a.tree)))
		}
	}

	if !anyDir && len(docPaths) > 0 {
		allFav := true
		for _, p := range docPaths {
			if !a.state.IsFavorite(sp.ID, p) {
				allFav = false
				break
			}
		}
		favLabel := i18n.T("添加到收藏")
		if allFav {
			favLabel = i18n.T("从收藏中移除")
		}
		if m.Item(favLabel).Chosen() {
			for _, p := range docPaths {
				if allFav {
					if a.state.IsFavorite(sp.ID, p) {
						a.state.ToggleFavorite(sp.ID, p)
					}
				} else if !a.state.IsFavorite(sp.ID, p) {
					a.state.ToggleFavorite(sp.ID, p)
					sp.Unpin(p)
				}
			}
			a.clearPicks("")
			a.saveState()
		}
	}

	m.Separator()
	if m.Item(i18n.T("复制")).Shortcut(ui.Cmd, ui.KeyC).Chosen() {
		a.copyEntries(sp, group)
	}
	if sp.IsExternal() {
		a.spacesGroupMenu(m, sp, group)
		m.Separator()
		if m.Item(i18n.T("在访达中显示")).Chosen() {
			for _, p := range group {
				a.revealIn(sp, p)
			}
		}
		if m.Item(i18n.T("从外部空间移除")).Chosen() {
			a.trashGroup(group)
		}
		return
	}

	folders := collectFolders(a.tree)
	anyNested := false
	for _, p := range group {
		if parentDir(p) != "" {
			anyNested = true
			break
		}
	}
	if len(folders) > 0 || anyNested {
		m.Submenu(i18n.T("移到文件夹"), func(sub *ui.Menu) {
			if anyNested {
				if sub.Item(i18n.T("根目录")).Chosen() {
					a.placeEntries(group, "", len(a.childrenOf("")))
				}
				if len(folders) > 0 {
					sub.Separator()
				}
			}
			for _, f := range folders {
				invalid := false
				allAlreadyThere := true
				for _, p := range group {
					if under(f.Path, p) {
						invalid = true
						break
					}
					if parentDir(p) != f.Path || sp.IsPinned(p) {
						allAlreadyThere = false
					}
				}
				if sub.Item(folderMenuLabel(f.Path)).Disabled(invalid || allAlreadyThere).Chosen() {
					a.placeEntries(group, f.Path, len(a.childrenOf(f.Path)))
				}
			}
		})
	}
	if m.Item(fmt.Sprintf(i18n.T("新建包含这 %d 项的文件夹"), n)).Chosen() {
		a.newFolderWithGroup(group)
	}
	a.spacesGroupMenu(m, sp, group)

	m.Separator()
	if m.Item(i18n.T("在访达中显示")).Chosen() {
		for _, p := range group {
			a.revealIn(sp, p)
		}
	}
	if m.Item(i18n.T("移到废纸篓")).Chosen() {
		a.trashGroup(group)
	}
}

// spacesGroupMenu adds "移到空间 ▸" for moving a multi-selection to another
// space.
func (a *App) spacesGroupMenu(m *ui.Menu, sp *space.Space, group []string) {
	if len(a.state.Spaces) < 2 {
		return
	}
	m.Submenu(i18n.T("移到空间"), func(sub *ui.Menu) {
		for _, s := range a.state.Spaces {
			if s.IsExternal() {
				continue
			}
			name := s.Name
			if s.Icon != "" {
				name = s.Icon + " " + s.Name
			}
			if sub.Item(name).Disabled(s.ID == sp.ID).Chosen() {
				a.moveGroupAcross(sp.ID, group, s.ID, "")
			}
		}
	})
}
