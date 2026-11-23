package document

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
)

func TestVersionSixMigrationAndAttributes(t *testing.T) {
	old := `{"format":"arcmd","version":5,"icon":"📖","cover":"gradient:sea","blocks":[{"id":"p","kind":"paragraph","runs":[{"text":"H2O"}]}]}`
	d, err := Decode(old)
	if err != nil || d.Version != CurrentVersion || d.Icon != "📖" {
		t.Fatal(d, err)
	}
	e := NewEngine(d)
	e.SetSelection(1, 2)
	e.SetScript("sub")
	image := NewBlock("image", "picture")
	image.URL = "assets/p.png"
	e.Insert(image)
	e.SetImageWidth(image.ID, 320)
	e.SetSelection(0, 0)
	e.InsertFootnote()
	s, err := e.Doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(s)
	if err != nil || !reflect.DeepEqual(decoded, e.Doc) {
		t.Fatal("v6 round trip", err)
	}
	if _, err := Decode(strings.Replace(s, fmt.Sprintf(`"version": %d`, CurrentVersion), `"version": 5`, 1)); err == nil {
		t.Fatal("accepted new attributes in v5")
	}
	e.Undo()
	if strings.Contains(e.Doc.PlainText(), "[^") {
		t.Fatal("footnote undo left reference")
	}
	e.Redo()
	if e.Doc.Validate() != nil {
		t.Fatal("footnote redo invalid")
	}
	for _, mutate := range []func(*Document){func(d *Document) { d.Blocks[0].ImageWidth = 20 }, func(d *Document) { d.Blocks[0].Runs[0].Style.Script = "other" }, func(d *Document) { d.Blocks[0].NoteLabel = "bad" }} {
		bad := d.Clone()
		mutate(bad)
		if bad.Validate() == nil {
			t.Fatal("invalid new attributes accepted")
		}
	}
}

func TestReadingLockAndRandomNewEdits(t *testing.T) {
	e := NewEngine(New())
	e.Replace(0, 0, "H2O😀")
	rng := rand.New(rand.NewPCG(8, 61))
	for range 180 {
		n := Len(e.Doc.PlainText())
		a := rng.IntN(n + 1)
		e.SetSelection(a, min(n, a+1))
		switch rng.IntN(6) {
		case 0:
			e.SetScript("sup")
		case 1:
			e.SetScript("sub")
		case 2:
			e.InsertFootnote()
		case 3:
			e.Undo()
		case 4:
			e.Redo()
		case 5:
			e.Replace(e.Anchor, e.Caret, "字")
		}
		if err := e.Doc.Validate(); err != nil {
			t.Fatal(err)
		}
		s, _ := e.Doc.Encode()
		d, err := Decode(s)
		if err != nil || !reflect.DeepEqual(d, e.Doc) {
			t.Fatal("round trip", err)
		}
	}
	before := e.Capture()
	version := e.Version()
	e.ReadOnly = true
	e.Replace(0, 1, "changed")
	e.Enter(false)
	e.PreviewReplace(0, 1, "preedit")
	e.SetScript("sup")
	e.InsertFootnote()
	e.Undo()
	e.Redo()
	e.Edit(func() { e.Doc.Blocks = nil })
	if !reflect.DeepEqual(before.Doc, e.Doc) || e.Version() != version {
		t.Fatal("locked model changed")
	}
}

func TestClipboardKeepsTablesAndRebasesFootnotes(t *testing.T) {
	d := New()
	table := Table(2, 2)
	table.Children[0].Children[0].Runs = []Run{{Text: "a"}}
	table.Children[1].Children[1].Runs = []Run{{Text: "b"}}
	table.Children[0].Children[0].Background = "red"
	d.Blocks = []*Block{NewBlock("paragraph", "Before"), table, NewBlock("paragraph", "After")}
	e := NewEngine(d)
	leaves := e.Doc.Leaves()
	e.SetSelection(leaves[1].Start, leaves[4].End)
	fragment := e.Fragment()
	if len(fragment.Blocks) != 1 || fragment.Blocks[0].Kind != "table" || fragment.Validate() != nil {
		t.Fatal("table fragment flattened")
	}
	e.SetSelection(leaves[1].Start, leaves[1].End)
	if _, err := e.Fragment().Encode(); err != nil {
		t.Fatal("partial colored cell invalid", err)
	}
	note := NewBlock("footnote", "old note")
	note.NoteLabel = "n"
	e.Edit(func() { e.Doc.Blocks = append(e.Doc.Blocks, note) })
	f := New()
	next := NewBlock("footnote", "new note")
	next.NoteLabel = "n"
	f.Blocks = []*Block{NewBlock("paragraph", "New[^n]"), next}
	e.SetSelection(0, 0)
	before := e.Doc.Clone()
	e.Paste(f)
	if e.Doc.Blocks[0].Text() != "New[^n-2]" || e.Doc.Blocks[1].NoteLabel != "n-2" {
		t.Fatal("pasted note captured old definition")
	}
	e.Undo()
	if !reflect.DeepEqual(before, e.Doc) {
		t.Fatal("paste undo")
	}
}

func TestKbdStyleToggleMigrationAndUndo(t *testing.T) {
	v8 := `{"format":"arcmd","version":8,"goal":500,"blocks":[{"id":"t","kind":"toggle","runs":[{"text":"Details"}]}]}`
	migrated, err := Decode(v8)
	if err != nil || migrated.Version != CurrentVersion || migrated.Goal != 500 || migrated.Blocks[0].Kind != "toggle" {
		t.Fatalf("v8 migration failed: doc=%+v err=%v", migrated, err)
	}

	e := NewEngine(New())
	e.Replace(0, 0, "Press Ctrl")
	e.SetSelection(6, 10)
	e.Toggle("kbd")
	if runs := e.Doc.Blocks[0].Runs; len(runs) != 2 || !runs[1].Style.Kbd || runs[1].Style.Code {
		t.Fatalf("expected kbd on selection without code, got %+v", runs)
	}

	// Toggling code clears kbd; toggling kbd clears code.
	e.Toggle("code")
	if runs := e.Doc.Blocks[0].Runs; !runs[1].Style.Code || runs[1].Style.Kbd {
		t.Fatalf("expected code to clear kbd, got %+v", runs)
	}
	e.Toggle("kbd")
	if runs := e.Doc.Blocks[0].Runs; !runs[1].Style.Kbd || runs[1].Style.Code {
		t.Fatalf("expected kbd to clear code, got %+v", runs)
	}

	// Collapsed caret inherits and toggles kbd cleanly.
	e.SetSelection(10, 10)
	e.Toggle("kbd")
	if st := e.CurrentStyle(); st.Kbd || st.Code {
		t.Fatalf("expected collapsed toggle to turn kbd off, got %+v", st)
	}
	e.Replace(10, 10, " now")
	if last := e.Doc.Blocks[0].Runs[len(e.Doc.Blocks[0].Runs)-1]; last.Text != " now" || last.Style.Kbd {
		t.Fatalf("expected trailing text outside kbd, got %+v", last)
	}

	s, err := e.Doc.Encode()
	if err != nil || !strings.Contains(s, `"kbd": true`) {
		t.Fatalf("encode failed: err=%v s=%s", err, s)
	}
	decoded, err := Decode(s)
	if err != nil || !reflect.DeepEqual(decoded, e.Doc) {
		t.Fatalf("kbd round trip mismatch: err=%v", err)
	}
	if _, err := Decode(strings.Replace(s, fmt.Sprintf(`"version": %d`, CurrentVersion), `"version": 8`, 1)); err == nil {
		t.Fatal("accepted kbd in v8 document")
	}

	e.Undo()
	e.Undo()
	if runs := e.Doc.Blocks[0].Runs; len(runs) != 2 || !runs[1].Style.Code || runs[1].Style.Kbd {
		t.Fatalf("expected undo to restore previous code state, got %+v", runs)
	}

	// Pressing Enter or deleting back from a trailing <kbd> span must not leak Kbd.
	k := NewEngine(New())
	k.Replace(0, 0, "Alt")
	k.SetSelection(0, 3)
	k.Toggle("kbd")
	k.SetSelection(3, 3) // simulated caret landing at end of <kbd>
	k.Enter(false)
	k.Replace(k.Caret, k.Caret, "<")
	if len(k.Doc.Blocks) != 2 || k.Doc.Blocks[1].Runs[0].Style.Kbd {
		t.Fatalf("Enter at end of <kbd> leaked Kbd onto new line: %+v", k.Doc.Blocks)
	}

	d := NewEngine(New())
	d.Replace(0, 0, "Prefix ")
	d.SetSelection(7, 7)
	d.Toggle("kbd")
	d.Replace(7, 7, "Alt")
	d.Replace(7, 10, "") // delete whole <kbd> span back to "Prefix "
	d.Replace(d.Caret, d.Caret, "<")
	if runs := d.Doc.Blocks[0].Runs; len(runs) != 1 || runs[0].Style.Kbd || runs[0].Text != "Prefix <" {
		t.Fatalf("deleting <kbd> span left Kbd active: %+v", runs)
	}

	empty := NewEngine(New())
	empty.Toggle("kbd")
	empty.Replace(0, 0, "X")
	empty.Replace(0, 1, "") // delete sole <kbd> char to empty paragraph
	empty.Replace(0, 0, "<")
	if runs := empty.Doc.Blocks[0].Runs; len(runs) != 1 || runs[0].Style.Kbd || runs[0].Text != "<" {
		t.Fatalf("emptied paragraph leaked Kbd onto <: %+v", runs)
	}
}
