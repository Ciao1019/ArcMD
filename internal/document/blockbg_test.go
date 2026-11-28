package document

import (
	"fmt"
	"strings"
	"testing"
)

func TestBlockBackgroundValidationAndMigration(t *testing.T) {
	if CurrentVersion < 11 {
		t.Fatalf("CurrentVersion = %d, want >= 11", CurrentVersion)
	}

	supported := []struct {
		kind  string
		setup func(*Block)
	}{
		{"paragraph", nil},
		{"heading", func(b *Block) { b.Level = 2 }},
		{"bullet", nil},
		{"numbered", nil},
		{"task", nil},
		{"quote", nil},
		{"toggle", nil},
		{"footnote", func(b *Block) { b.NoteLabel = "fn1" }},
	}
	for _, tc := range supported {
		if !AllowsBlockBackground(tc.kind) || !AllowsBackground(tc.kind) {
			t.Fatalf("expected %q to allow block background", tc.kind)
		}
		b := NewBlock(tc.kind, "hello")
		b.Background = "light-blue"
		if tc.setup != nil {
			tc.setup(b)
		}
		doc := New()
		doc.Blocks = []*Block{b}
		encoded, err := doc.Encode()
		if err != nil {
			t.Fatalf("%s Encode: %v", tc.kind, err)
		}
		decoded, err := Decode(encoded)
		if err != nil {
			t.Fatalf("%s Decode: %v", tc.kind, err)
		}
		if decoded.Blocks[0].Background != "light-blue" {
			t.Fatalf("%s Background = %q, want light-blue", tc.kind, decoded.Blocks[0].Background)
		}
	}

	// Unsupported block kinds reject Background in Validate.
	for _, kind := range []string{"callout", "code", "rule", "raw", "math", "image"} {
		if AllowsBlockBackground(kind) || AllowsBackground(kind) {
			t.Fatalf("expected %q not to allow background", kind)
		}
		b := NewBlock(kind, "x")
		if kind == "image" {
			b.URL = "pic.png"
		}
		if kind == "rule" {
			b.Runs = []Run{{Text: ""}}
		}
		b.Background = "light-blue"
		doc := New()
		doc.Blocks = []*Block{b}
		if err := doc.Validate(); err == nil {
			t.Fatalf("kind %q with background should fail Validate", kind)
		}
	}

	// Unknown palette name is rejected.
	badColor := New()
	badColor.Blocks[0].Background = "neon-pink"
	if err := badColor.Validate(); err == nil {
		t.Fatal("unknown background name should fail Validate")
	}

	// Version 10 document with non-cell background is rejected; v10 table cell background still migrates.
	v10Block := `{"format":"arcmd","version":10,"blocks":[{"id":"p1","kind":"paragraph","background":"light-blue","runs":[{"text":"hi"}]}]}`
	if _, err := Decode(v10Block); err == nil {
		t.Fatal("v10 document with paragraph background should be rejected")
	}
	v10Cell := `{"format":"arcmd","version":10,"blocks":[{"id":"t1","kind":"table","children":[{"id":"r1","kind":"row","children":[{"id":"c1","kind":"cell","background":"light-yellow","runs":[{"text":"cell"}]}]}]}]}`
	migrated, err := Decode(v10Cell)
	if err != nil {
		t.Fatalf("v10 table cell background should migrate cleanly: %v", err)
	}
	if migrated.Version != CurrentVersion || migrated.Blocks[0].Children[0].Children[0].Background != "light-yellow" {
		t.Fatalf("unexpected migrated cell: version=%d bg=%q", migrated.Version, migrated.Blocks[0].Children[0].Children[0].Background)
	}
	if _, err := Decode(strings.Replace(v10Block, `"version":10`, fmt.Sprintf(`"version":%d`, CurrentVersion), 1)); err != nil {
		t.Fatalf("v%d paragraph background should decode: %v", CurrentVersion, err)
	}
}

func TestBlockBackgroundEngineTransactions(t *testing.T) {
	p1 := NewBlock("paragraph", "Alpha Beta")
	h2 := NewBlock("heading", "Heading")
	h2.Level = 2
	code := NewBlock("code", "fmt.Println(1)")
	q := NewBlock("quote", "Quoted")
	cols := Columns()
	cols.Children[0].Children[0].Runs = []Run{{Text: "Left col"}}
	doc := New()
	doc.Blocks = []*Block{p1, h2, code, q, cols}
	e := NewEngine(doc)
	p1ID, h2ID, codeID, qID, colChildID := p1.ID, h2.ID, code.ID, q.ID, cols.Children[0].Children[0].ID

	// Single block set + undo/redo + column child support.
	e.SetBlockBackground(p1ID, "light-blue")
	if e.Doc.Blocks[0].Background != "light-blue" {
		t.Fatalf("SetBlockBackground got %q, want light-blue", e.Doc.Blocks[0].Background)
	}
	e.Undo()
	if e.Doc.Blocks[0].Background != "" {
		t.Fatalf("Undo did not clear background: %q", e.Doc.Blocks[0].Background)
	}
	e.Redo()
	if e.Doc.Blocks[0].Background != "light-blue" {
		t.Fatalf("Redo did not restore background: %q", e.Doc.Blocks[0].Background)
	}

	// Column child block supports background.
	e.SetBlockBackground(colChildID, "red")
	if e.Doc.Blocks[4].Children[0].Children[0].Background != "red" {
		t.Fatalf("column child background got %q, want red", e.Doc.Blocks[4].Children[0].Children[0].Background)
	}

	// Unsupported block and unknown color are ignored.
	e.SetBlockBackground(codeID, "light-blue")
	if e.Doc.Blocks[2].Background != "" {
		t.Fatalf("code block should ignore SetBlockBackground, got %q", e.Doc.Blocks[2].Background)
	}
	e.SetBlockBackground(p1ID, "invalid-color")
	if e.Doc.Blocks[0].Background != "light-blue" {
		t.Fatalf("invalid color mutated background to %q", e.Doc.Blocks[0].Background)
	}

	// Empty ID targets caret block.
	e.FocusBlock(qID)
	e.SetBlockBackground("", "purple")
	if e.Doc.Blocks[3].Background != "purple" {
		t.Fatalf("empty ID did not target caret block: %q", e.Doc.Blocks[3].Background)
	}

	// Batch set across mixed blocks skips unsupported code block and undoes in one step.
	e.SetBlocksBackground([]string{p1ID, h2ID, codeID, qID}, "green")
	if e.Doc.Blocks[0].Background != "green" || e.Doc.Blocks[1].Background != "green" || e.Doc.Blocks[3].Background != "green" || e.Doc.Blocks[2].Background != "" {
		t.Fatalf("unexpected batch backgrounds: p1=%q h2=%q code=%q q=%q", e.Doc.Blocks[0].Background, e.Doc.Blocks[1].Background, e.Doc.Blocks[2].Background, e.Doc.Blocks[3].Background)
	}
	e.Undo()
	if e.Doc.Blocks[0].Background != "light-blue" || e.Doc.Blocks[1].Background != "" || e.Doc.Blocks[3].Background != "purple" {
		t.Fatalf("single undo did not restore all batch targets: p1=%q h2=%q q=%q", e.Doc.Blocks[0].Background, e.Doc.Blocks[1].Background, e.Doc.Blocks[3].Background)
	}

	// Turning a tinted block into callout or code clears stray Background.
	e.FocusBlock(p1ID)
	e.SetKind("callout", 0)
	if e.Doc.Blocks[0].Background != "" {
		t.Fatalf("converting tinted paragraph to callout left stray background %q", e.Doc.Blocks[0].Background)
	}
	if err := e.Doc.Validate(); err != nil {
		t.Fatalf("Validate after kind conversion: %v", err)
	}
	e.Undo()
	if e.Doc.Blocks[0].Kind != "paragraph" || e.Doc.Blocks[0].Background != "light-blue" {
		t.Fatalf("Undo kind conversion got kind=%q bg=%q", e.Doc.Blocks[0].Kind, e.Doc.Blocks[0].Background)
	}

	// Splitting at end of tinted paragraph leaves new empty paragraph untinted.
	leaf, _ := e.LeafByID(p1ID)
	e.SetSelection(leaf.End, leaf.End)
	e.Enter(false)
	if len(e.Doc.Blocks) != 6 || e.Doc.Blocks[0].Background != "light-blue" || e.Doc.Blocks[1].Background != "" {
		t.Fatalf("Enter at end should clear new block bg: b0=%q b1=%q", e.Doc.Blocks[0].Background, e.Doc.Blocks[1].Background)
	}
	e.Undo()

	// Splitting in middle of tinted paragraph preserves background on both halves.
	leaf, _ = e.LeafByID(p1ID)
	mid := leaf.Start + 5
	e.SetSelection(mid, mid)
	e.Enter(false)
	if e.Doc.Blocks[0].Background != "light-blue" || e.Doc.Blocks[1].Background != "light-blue" {
		t.Fatalf("Enter mid-paragraph should keep bg on both halves: b0=%q b1=%q", e.Doc.Blocks[0].Background, e.Doc.Blocks[1].Background)
	}
	e.Undo()

	// Splitting heading clears background on trailing paragraph.
	e.SetBlockBackground(h2ID, "blue")
	hLeaf, _ := e.LeafByID(h2ID)
	e.SetSelection(hLeaf.Start+4, hLeaf.Start+4)
	e.Enter(false)
	if e.Doc.Blocks[1].Background != "blue" || e.Doc.Blocks[2].Kind != "paragraph" || e.Doc.Blocks[2].Background != "" {
		t.Fatalf("splitting tinted heading should leave new paragraph untinted: h=%q next=(%q,%q)", e.Doc.Blocks[1].Background, e.Doc.Blocks[2].Kind, e.Doc.Blocks[2].Background)
	}
	e.Undo()

	// Whole-block Fragment + Paste into empty paragraph carries Background.
	leaf, _ = e.LeafByID(p1ID)
	e.SetSelection(leaf.Start, leaf.End)
	frag := e.Fragment()
	if len(frag.Blocks) != 1 || frag.Blocks[0].Background != "light-blue" {
		t.Fatalf("whole-block fragment lost background: %+v", frag.Blocks)
	}
	empty := NewBlock("paragraph", "")
	e2 := NewEngine(&Document{Format: "arcmd", Version: CurrentVersion, Blocks: []*Block{empty}})
	e2.Paste(frag)
	if e2.Doc.Blocks[0].Background != "light-blue" {
		t.Fatalf("Paste into empty paragraph lost background: %q", e2.Doc.Blocks[0].Background)
	}
}
