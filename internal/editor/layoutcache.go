package editor

import "github.com/Ciao1019/ArcMD/internal/document"

// Width changes do not change the leaf offsets or folded structure. Reuse
// these inputs while the model is unchanged instead of walking all its text
// and allocating new indexes at the pointer's event rate. A forced layout
// (ready=false) always refreshes, including callers that directly edit a model.
type layoutInputs struct {
	doc      *document.Document
	revision uint64
	leaves   map[string]document.Leaf
	hidden   map[string]string
	foldable map[string]bool
}

func (in *layoutInputs) refresh(doc *document.Document, revision uint64, ready bool) {
	if ready && in.doc == doc && in.revision == revision {
		return
	}
	in.doc, in.revision = doc, revision
	if in.leaves == nil {
		in.leaves = make(map[string]document.Leaf)
	}
	clear(in.leaves)
	for _, leaf := range doc.Leaves() {
		in.leaves[leaf.Block.ID] = leaf
	}
	in.hidden, in.foldable = doc.HiddenBlocks(), doc.FoldableBlocks()
}
