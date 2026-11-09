package document

import (
	"slices"
	"sort"
	"strings"
)

// TextChange describes one text-only edit without changing the block tree.
// Consumers may update their caches only if they observed the previous revision.
type TextChange struct {
	ID                         string
	Before, After              string
	Start, BeforeEnd, AfterEnd int
	Revision                   uint64
}

func (e *Engine) TextChangeSince(revision uint64) (TextChange, bool) {
	c := e.textChange
	return c, c.Revision == e.revision && revision+1 == c.Revision && c.ID != ""
}

// Text-only history owns a copy of the affected runs. Structural transactions
// retain full snapshots; neither representation aliases mutable document data.
type historyEntry struct {
	state   Snapshot
	blockID string
	runs    []Run
}

func (e *Engine) recordUndo(entry historyEntry) {
	e.undo = append(e.undo, entry)
	if len(e.undo) > 200 {
		e.undo = append([]historyEntry(nil), e.undo[len(e.undo)-200:]...)
	}
	e.redo = nil
}

func (e *Engine) captureHistory(id string) historyEntry {
	if id != "" {
		if parent, i := e.find(id); parent != nil {
			return historyEntry{Snapshot{Anchor: e.Anchor, Caret: e.Caret, Typing: e.Typing, Explicit: e.Explicit}, id, slices.Clone((*parent)[i].Runs)}
		}
	}
	return historyEntry{state: e.Capture()}
}

func (e *Engine) restoreHistory(entry historyEntry) {
	if entry.blockID == "" {
		folded := e.folds()
		e.Restore(entry.state)
		e.keepFolds(folded)
		return
	}
	// Undoing later structural entries has already restored this block.
	e.LeafAt(0)
	i := slices.IndexFunc(e.leaves, func(l Leaf) bool { return l.Block.ID == entry.blockID })
	if i < 0 {
		panic("document: text history lost its block")
	}
	leaf := e.leaves[i]
	before := leaf.Block.Text()
	leaf.Block.Runs = slices.Clone(entry.runs)
	e.publishTextChange(i, before, leaf.Block.Text())
	e.Anchor, e.Caret, e.Typing, e.Explicit = entry.state.Anchor, entry.state.Caret, entry.state.Typing, entry.state.Explicit
}

// replaceText handles changes entirely inside one leaf, preserving the existing
// transaction/selection semantics without copying unrelated blocks for undo.
func (e *Engine) replaceText(a, z int, text string) bool {
	if e.depth != 0 || strings.ContainsAny(text, "\r\n") {
		return false
	}
	a, z = max(0, min(a, z)), max(0, max(a, z))
	first := e.LeafAt(a)
	if z > first.End || a < first.Start {
		return false
	}
	b := first.Block
	before := b.Text()
	if strings.ContainsRune(before, '\n') && b.Kind != "code" && b.Kind != "cell" && b.Kind != "raw" && b.Kind != "math" {
		return false
	}
	a = first.Start + Len(before[:ByteOffset(before, a-first.Start)])
	z = first.Start + Len(before[:ByteOffset(before, z-first.Start)])
	style := e.CurrentStyle()
	runs := Compact(append(append(SliceRuns(b.Runs, 0, a-first.Start), Run{Text: text, Style: style}), SliceRuns(b.Runs, z-first.Start, first.End-first.Start)...))
	// Short Markdown prefixes still take the structural transaction path.
	if text == " " && b.Kind == "paragraph" && first.End-first.Start-(z-a)+1 <= 7 {
		return false
	}
	entry := historyEntry{Snapshot{Anchor: e.Anchor, Caret: e.Caret, Typing: e.Typing, Explicit: e.Explicit}, b.ID, slices.Clone(b.Runs)}
	e.Anchor, e.Caret = a+Len(text), a+Len(text)
	e.Typing, e.Explicit = style, true
	if slices.Equal(b.Runs, runs) {
		return true
	}
	i := sort.Search(len(e.leaves), func(i int) bool { return first.Start <= e.leaves[i].Start })
	b.Runs = runs
	e.recordUndo(entry)
	e.publishTextChange(i, before, b.Text())
	e.version++
	return true
}

func (e *Engine) publishTextChange(index int, before, after string) {
	leaf := e.leaves[index]
	delta := Len(after) - (leaf.End - leaf.Start)
	e.revision++
	e.textChange = TextChange{leaf.Block.ID, before, after, leaf.Start, leaf.End, leaf.End + delta, e.revision}
	e.leaves[index].End += delta
	for i := index + 1; i < len(e.leaves); i++ {
		e.leaves[i].Start += delta
		e.leaves[i].End += delta
	}
	e.indexDoc, e.indexRevision = e.Doc, e.revision
}
