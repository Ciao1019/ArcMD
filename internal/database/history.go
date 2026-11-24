package database

const maxHistoryEntries = 100

type historyStep struct {
	label string
	undo  func(s *Store) []string
	redo  func(s *Store) []string
}

// History maintains a bounded undo/redo stack for database cell edits,
// schema changes, view updates, and inline row mutations.
type History struct {
	entries []historyStep
	pos     int
}

// CanUndo reports whether there is a step to undo.
func (h *History) CanUndo() bool {
	return h != nil && h.pos > 0
}

// CanRedo reports whether there is a step to redo.
func (h *History) CanRedo() bool {
	return h != nil && h.pos < len(h.entries)
}

// Len returns the number of recorded undo steps.
func (h *History) Len() int {
	if h == nil {
		return 0
	}
	return h.pos
}

func (h *History) push(step historyStep) {
	if h.pos < len(h.entries) {
		h.entries = h.entries[:h.pos]
	}
	h.entries = append(h.entries, step)
	if len(h.entries) > maxHistoryEntries {
		drop := len(h.entries) - maxHistoryEntries
		copy(h.entries, h.entries[drop:])
		h.entries = h.entries[:maxHistoryEntries]
	}
	h.pos = len(h.entries)
}

func (h *History) undo(s *Store) ([]string, bool) {
	if !h.CanUndo() {
		return nil, false
	}
	h.pos--
	paths := h.entries[h.pos].undo(s)
	return paths, true
}

func (h *History) redo(s *Store) ([]string, bool) {
	if !h.CanRedo() {
		return nil, false
	}
	paths := h.entries[h.pos].redo(s)
	h.pos++
	return paths, true
}
