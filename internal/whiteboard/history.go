package whiteboard

const maxHistoryEntries = 100

// History tracks undo and redo snapshots for a whiteboard scene with gesture
// coalescing so dragging or freehand drawing forms a single undo step.
type History struct {
	undo    []*Scene
	redo    []*Scene
	pending *Scene
}

// NewHistory returns an empty undo/redo stack.
func NewHistory() History { return History{} }

// Begin captures the pre-gesture snapshot if no gesture is active.
func (h *History) Begin(cur *Scene) {
	if h.pending != nil || cur == nil {
		return
	}
	h.pending = cur.Clone()
}

// Cancel discards an active gesture baseline without recording an undo step.
func (h *History) Cancel() { h.pending = nil }

// Commit finishes an active gesture and pushes its baseline if cur changed.
func (h *History) Commit(cur *Scene) bool {
	if h.pending == nil {
		return false
	}
	base := h.pending
	h.pending = nil
	if cur != nil && base.Digest() == cur.Digest() {
		return false
	}
	h.pushUndo(base)
	return true
}

// Record pushes a pre-change snapshot for an atomic edit.
func (h *History) Record(before *Scene) {
	if before == nil {
		return
	}
	h.pending = nil
	h.pushUndo(before.Clone())
}

func (h *History) pushUndo(snap *Scene) {
	h.undo = append(h.undo, snap)
	if len(h.undo) > maxHistoryEntries {
		h.undo = append([]*Scene(nil), h.undo[len(h.undo)-maxHistoryEntries:]...)
	}
	h.redo = nil
}

// CanUndo reports whether an undo snapshot is available.
func (h *History) CanUndo() bool { return len(h.undo) > 0 }

// CanRedo reports whether a redo snapshot is available.
func (h *History) CanRedo() bool { return len(h.redo) > 0 }

// Undo reverts to the previous snapshot and returns it.
func (h *History) Undo(cur *Scene) (*Scene, bool) {
	h.pending = nil
	if len(h.undo) == 0 {
		return nil, false
	}
	prev := h.undo[len(h.undo)-1]
	h.undo = h.undo[:len(h.undo)-1]
	if cur != nil {
		h.redo = append(h.redo, cur.Clone())
	}
	return prev.Clone(), true
}

// Redo reapplies the next snapshot and returns it.
func (h *History) Redo(cur *Scene) (*Scene, bool) {
	h.pending = nil
	if len(h.redo) == 0 {
		return nil, false
	}
	next := h.redo[len(h.redo)-1]
	h.redo = h.redo[:len(h.redo)-1]
	if cur != nil {
		h.undo = append(h.undo, cur.Clone())
	}
	return next.Clone(), true
}
