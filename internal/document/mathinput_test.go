package document

import (
	"reflect"
	"testing"
)

func TestMathFencesAreTransactions(t *testing.T) {
	e := textEngine("$$")
	e.SetSelection(2, 2)
	before := e.Doc.Clone()
	e.Enter(false)
	if e.Doc.Blocks[0].Kind != "math" || e.Doc.PlainText() != "" || e.Caret != 0 {
		t.Fatal("opening fence did not start math")
	}
	e.Undo()
	if !reflect.DeepEqual(before, e.Doc) {
		t.Fatal("opening fence undo lost text")
	}
	e.Redo()
	e.Replace(0, 0, "x^2\n$$")
	before = e.Doc.Clone()
	e.Enter(false)
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Text() != "x^2" || e.LeafAt(e.Caret).Block.Kind != "paragraph" {
		t.Fatal("closing fence did not return to text")
	}
	if err := e.Doc.Validate(); err != nil {
		t.Fatal(err)
	}
	e.Undo()
	if !reflect.DeepEqual(before, e.Doc) {
		t.Fatal("closing fence was not one undo step")
	}
}

func TestMathFencesDoNotConvertLiteralOrSelectedText(t *testing.T) {
	for _, kind := range []string{"code", "raw", "cell", "heading"} {
		e := textEngine("$$")
		e.Doc.Blocks[0].Kind = kind
		e.SetSelection(2, 2)
		e.Enter(false)
		if e.Doc.Blocks[0].Kind != kind {
			t.Fatalf("converted %s to math", kind)
		}
	}
	e := textEngine("$$")
	e.SetSelection(0, 2)
	e.Enter(false)
	if e.Doc.Blocks[0].Kind == "math" {
		t.Fatal("replacing selection started math")
	}
}

func TestPasteMathPreservesItsBlockAndSurroundingText(t *testing.T) {
	e := textEngine("BeforeAfter")
	e.SetSelection(6, 6)
	before := e.Doc.Clone()
	fragment := New()
	fragment.Blocks = []*Block{NewBlock("math", `\int_0^\infty x^2 e^{-x} dx = 2`)}
	e.Paste(fragment)
	blocks := e.Doc.Blocks
	if len(blocks) != 3 || blocks[0].Text() != "Before" || blocks[1].Kind != "math" || blocks[1].ID == fragment.Blocks[0].ID || blocks[2].Text() != "After" {
		t.Fatal("formula paste lost structure or surrounding text")
	}
	if err := e.Doc.Validate(); err != nil {
		t.Fatal(err)
	}
	e.Undo()
	if !reflect.DeepEqual(before, e.Doc) {
		t.Fatal("formula paste was not a single undo step")
	}
}
