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

func TestMathDoubleEnterExitAndPipeTableCompile(t *testing.T) {
	e := textEngine("$$")
	e.SetSelection(2, 2)
	e.Enter(false)
	e.Replace(0, 0, "F = ma")
	e.Enter(false)
	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "math" || e.Doc.Blocks[0].Text() != "F = ma\n" {
		t.Fatalf("first Enter inside math exited prematurely: %+v", e.Doc.Blocks)
	}
	e.Enter(false)
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Text() != "F = ma" || e.Doc.Blocks[1].Kind != "paragraph" {
		t.Fatalf("second Enter on blank trailing line did not exit math: %+v", e.Doc.Blocks)
	}
	e.Undo()
	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Text() != "F = ma\n" {
		t.Fatal("double-Enter exit was not one undo step")
	}

	p := textEngine("| Port | Low | High")
	p.SetSelection(Len("| Port | Low | High"), Len("| Port | Low | High"))
	before := p.Doc.Clone()
	p.Enter(false)
	if len(p.Doc.Blocks) != 1 || p.Doc.Blocks[0].Kind != "table" || len(p.Doc.Blocks[0].Children) != 2 || len(p.Doc.Blocks[0].Children[0].Children) != 3 {
		t.Fatalf("pipe header did not compile into 2x3 table: %+v", p.Doc.Blocks)
	}
	tr := p.Doc.Blocks[0].Children[0]
	if tr.Children[0].Text() != "Port" || tr.Children[1].Text() != "Low" || tr.Children[2].Text() != "High" {
		t.Fatalf("unexpected header cells: %q, %q, %q", tr.Children[0].Text(), tr.Children[1].Text(), tr.Children[2].Text())
	}
	if p.LeafAt(p.Caret).Block.ID != p.Doc.Blocks[0].Children[1].Children[0].ID {
		t.Fatal("caret did not land in first body cell")
	}
	p.Undo()
	if !reflect.DeepEqual(before, p.Doc) {
		t.Fatal("pipe table compile did not undo in one step")
	}

	tbl := p.Doc.Blocks[0]
	_ = tbl
	p.Redo()
	id := p.Doc.Blocks[0].ID
	p.Replace(p.Caret, p.Caret, "SSH")
	p.TableInsert(id, true, 2)
	p.Replace(p.Caret, p.Caret, "HTTPS")
	beforeMove := p.Doc.Clone()
	p.MoveTableRow(id, 2, 1)
	if p.Doc.Blocks[0].Children[1].Children[0].Text() != "HTTPS" || p.Doc.Blocks[0].Children[2].Children[0].Text() != "SSH" {
		t.Fatal("MoveTableRow did not reorder rows")
	}
	p.Undo()
	if !reflect.DeepEqual(beforeMove, p.Doc) {
		t.Fatal("MoveTableRow did not undo cleanly")
	}
}
