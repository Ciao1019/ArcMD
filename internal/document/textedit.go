package document

import (
	"slices"
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
		e.undo[0] = historyEntry{}
		e.undo = e.undo[1:]
	}
	e.redo = nil
}

func (e *Engine) captureHistory(id string) historyEntry {
	if id != "" {
		if leaf, ok := e.LeafByID(id); ok {
			return historyEntry{Snapshot{Anchor: e.Anchor, Caret: e.Caret, Typing: e.Typing, Explicit: e.Explicit}, id, slices.Clone(leaf.Block.Runs)}
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
	e.ensureIndex()
	i, ok := e.leafIDs[entry.blockID]
	if !ok {
		panic("document: text history lost its block")
	}
	leaf := e.leafAtIndex(i)
	before := leaf.Block.Text()
	leaf.Block.Runs = slices.Clone(entry.runs)
	e.publishTextChange(i, before, leaf.Block.Text())
	e.Anchor, e.Caret, e.Typing, e.Explicit = entry.state.Anchor, entry.state.Caret, entry.state.Typing, entry.state.Explicit
}

// replaceText handles changes entirely inside one leaf, preserving the existing
// transaction/selection semantics without copying unrelated blocks for undo.
func (e *Engine) replaceText(a, z int, text string) bool { return e.replaceTextMode(a, z, text, true) }

func (e *Engine) replaceTextMode(a, z int, text string, committed bool) bool {
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
	style.NoMath = false
	runs := Compact(append(append(SliceRuns(b.Runs, 0, a-first.Start), Run{Text: text, Style: style}), SliceRuns(b.Runs, z-first.Start, first.End-first.Start)...))
	// Short Markdown prefixes still take the structural transaction path.
	if text == " " && b.Kind == "paragraph" && first.End-first.Start-(z-a)+1 <= 7 {
		return false
	}
	var entry historyEntry
	if committed {
		entry = historyEntry{Snapshot{Anchor: e.Anchor, Caret: e.Caret, Typing: e.Typing, Explicit: e.Explicit}, b.ID, slices.Clone(b.Runs)}
	}
	e.Anchor, e.Caret = a+Len(text), a+Len(text)
	e.Typing, e.Explicit = style, true
	if slices.Equal(b.Runs, runs) {
		return true
	}
	i := e.leafIDs[first.Block.ID]
	b.Runs = runs
	if committed {
		e.recordUndo(entry)
		e.version++
	}
	e.publishTextChange(i, before, b.Text())
	return true
}

func (e *Engine) publishTextChange(index int, before, after string) {
	leaf := e.leafAtIndex(index)
	delta := Len(after) - (leaf.End - leaf.Start)
	byteDelta := len(after) - len(before)
	if e.hiddenDoc == e.Doc && e.hiddenRevision == e.revision {
		e.hiddenRevision++
	}
	e.revision++
	e.textChange = TextChange{leaf.Block.ID, before, after, leaf.Start, leaf.End, leaf.End + delta, e.revision}
	e.leaves[index].End += delta
	e.leaves[index].ByteEnd += byteDelta
	e.unitIndex.add(index, delta)
	e.byteIndex.add(index, byteDelta)
	e.indexDoc, e.indexRevision = e.Doc, e.revision
}
