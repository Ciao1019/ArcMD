package document

import (
	"math/rand"
	"reflect"
	"testing"
	"unicode/utf16"
)

func TestTextHistoryMatchesFullTransactions(t *testing.T) {
	doc := New()
	doc.Blocks = []*Block{NewBlock("paragraph", "中文😀 hello"), Table(2, 2), Columns(), NewBlock("paragraph", "last")}
	fast, full := NewEngine(doc), NewEngine(doc)
	rng := rand.New(rand.NewSource(42))
	for step := 0; step < 600; step++ {
		leaves := fast.Doc.Leaves()
		leaf := leaves[rng.Intn(len(leaves))]
		a, z := leaf.Start+rng.Intn(leaf.End-leaf.Start+1), leaf.Start+rng.Intn(leaf.End-leaf.Start+1)
		fast.SetSelection(a, z)
		full.SetSelection(a, z)
		switch rng.Intn(10) {
		case 0:
			fast.Undo()
			full.Undo()
		case 1:
			fast.Redo()
			full.Redo()
		case 2:
			fast.Toggle("bold")
			full.Toggle("bold")
		case 3:
			fast.ToggleFold(fast.Doc.Blocks[0].ID)
			full.ToggleFold(full.Doc.Blocks[0].ID)
		default:
			text := []string{"a", " ", "", "中文😀", "e\u0301", "\u2028", "#"}[rng.Intn(7)]
			fast.Replace(a, z, text)
			full.Edit(func() { full.Replace(a, z, text) })
		}
		if !reflect.DeepEqual(fast.Doc, full.Doc) || fast.Anchor != full.Anchor || fast.Caret != full.Caret || fast.Typing != full.Typing || fast.Explicit != full.Explicit {
			t.Fatalf("step %d diverged from full-snapshot transactions", step)
		}
		if err := fast.Doc.Validate(); err != nil {
			t.Fatalf("step %d: %v", step, err)
		}
		for _, want := range fast.Doc.Leaves() {
			for _, at := range []int{want.Start, want.End} {
				got := fast.LeafAt(at)
				if got.Block.ID != want.Block.ID || got.Start != want.Start || got.End != want.End {
					t.Fatalf("step %d: stale leaf index at %d", step, at)
				}
			}
		}
		assertLeafOffsets(t, fast)
	}
	encoded, err := fast.Doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || !reflect.DeepEqual(decoded, fast.Doc) {
		t.Fatal("text history changed the saved format", err)
	}
}

func TestTextHistoryDoesNotAliasAndKeepsFoldState(t *testing.T) {
	e := textEngine("a")
	e.Replace(1, 1, "b")
	e.SetKind("heading", 2)
	e.ToggleFold(e.Doc.Blocks[0].ID)
	e.Replace(2, 2, "c")
	snapshot := e.Capture()
	e.Undo()
	if e.Doc.PlainText() != "ab" || !e.Doc.Blocks[0].Folded {
		t.Fatal("text undo changed folding")
	}
	e.Redo()
	e.Doc.Blocks[0].Runs[0].Text = "external"
	if snapshot.Doc.PlainText() != "abc" {
		t.Fatal("capture aliases current runs")
	}
	e.Undo()
	if e.Doc.PlainText() != "ab" {
		t.Fatal("history aliases current runs")
	}
}

func TestUTF16LengthAndBatchFoldControls(t *testing.T) {
	for _, text := range []string{"", "ASCII", "中文😀", "e\u0301", "👨‍👩‍👧‍👦", "\xff\xfe"} {
		if Len(text) != len(utf16.Encode([]rune(text))) {
			t.Fatalf("UTF-16 length changed for %q", text)
		}
	}
	d := New()
	d.Blocks = []*Block{NewBlock("heading", "Title"), NewBlock("paragraph", "body"), NewBlock("bullet", "parent"), NewBlock("bullet", "child"), Columns(), Table(2, 2)}
	d.Blocks[0].Level = 1
	d.Blocks[3].Indent = 1
	d.Blocks[4].Children[0].Children = []*Block{NewBlock("heading", "inner")}
	d.Blocks[4].Children[0].Children[0].Level = 2
	batch := d.FoldableBlocks()
	for _, list := range siblingLists(d) {
		for _, b := range list {
			if batch[b.ID] != d.CanFold(b.ID) {
				t.Fatal("batch fold controls differ", b.ID)
			}
		}
	}
}
