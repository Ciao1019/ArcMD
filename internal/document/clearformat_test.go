package document

import "testing"

func TestClearFormattingStripsAllInlineStylesAndLinks(t *testing.T) {
	doc := New()
	h := NewBlock("heading", "")
	h.Level = 2
	h.Runs = []Run{
		{Text: "keep ", Style: Style{Bold: true, Color: "red"}},
		{
			Text: "clear",
			Style: Style{
				NoMath:          true,
				Script:          "sup",
				Bold:            true,
				Italic:          true,
				Underline:       true,
				Strike:          true,
				Code:            true,
				Kbd:             true,
				Link:            "https://example.com",
				Color:           "red",
				Highlight:       "light-yellow",
				Annotation:      "wavy",
				AnnotationColor: "blue",
			},
		},
		{Text: " tail", Style: Style{Italic: true, Link: "https://example.org"}},
	}
	p := NewBlock("paragraph", "")
	p.Runs = []Run{{Text: "colored", Style: Style{Color: "blue", Highlight: "yellow"}}}
	doc.Blocks = []*Block{h, p}

	e := NewEngine(doc)
	h = e.Doc.Blocks[0]
	p = e.Doc.Blocks[1]
	// Partial selection inside a heading: clears only "clear", keeps NoMath and block kind.
	e.SetSelection(5, 10)
	v0 := e.Version()
	e.ClearFormatting()

	if e.Version() == v0 {
		t.Fatal("expected ClearFormatting to record a change")
	}
	if h.Kind != "heading" || h.Level != 2 {
		t.Fatalf("block structure changed: %s L%d", h.Kind, h.Level)
	}
	if len(h.Runs) != 3 {
		t.Fatalf("unexpected runs after partial clear: %+v", h.Runs)
	}
	if !h.Runs[0].Style.Bold || h.Runs[0].Style.Color != "red" {
		t.Fatalf("prefix style changed: %+v", h.Runs[0].Style)
	}
	if h.Runs[1].Style != (Style{NoMath: true}) {
		t.Fatalf("cleared run style = %+v; want only NoMath preserved", h.Runs[1].Style)
	}
	if !h.Runs[2].Style.Italic || h.Runs[2].Style.Link != "https://example.org" {
		t.Fatalf("suffix style changed: %+v", h.Runs[2].Style)
	}

	// Re-running on already plain selection is a no-op.
	v1 := e.Version()
	e.ClearFormatting()
	if e.Version() != v1 {
		t.Fatal("ClearFormatting on plain text should be a no-op")
	}

	// Single undo restores all stripped styles atomically.
	e.Undo()
	if h.Runs[1].Style.Link != "https://example.com" || h.Runs[1].Style.Color != "red" || h.Runs[1].Style.Annotation != "wavy" {
		t.Fatalf("undo did not restore cleared styles: %+v", h.Runs[1].Style)
	}

	// Cross-block selection clears both heading and paragraph while keeping block kinds.
	leaves := e.Doc.Leaves()
	e.SetSelection(0, leaves[len(leaves)-1].End)
	e.ClearFormatting()
	if h.Kind != "heading" || p.Kind != "paragraph" {
		t.Fatalf("cross-block clear changed kinds: %s, %s", h.Kind, p.Kind)
	}
	for _, b := range []*Block{h, p} {
		for _, r := range b.Runs {
			if r.Style != (Style{NoMath: r.Style.NoMath}) {
				t.Fatalf("leftover style in %s: %+v", b.Kind, r.Style)
			}
		}
	}
}

func TestClearFormattingCollapsedCaretAndReadOnly(t *testing.T) {
	doc := New()
	doc.Blocks = []*Block{{
		ID:   NewID(),
		Kind: "paragraph",
		Runs: []Run{{Text: "red", Style: Style{Bold: true, Color: "red", Link: "https://example.com"}}},
	}}
	e := NewEngine(doc)
	e.SetSelection(3, 3)
	if !e.CurrentStyle().Bold || e.CurrentStyle().Color != "red" {
		t.Fatalf("expected styled caret before clear: %+v", e.CurrentStyle())
	}

	// ReadOnly refuses to clear typing style or selection.
	e.ReadOnly = true
	e.ClearFormatting()
	if !e.CurrentStyle().Bold {
		t.Fatal("ReadOnly should not clear caret typing style")
	}
	e.SetSelection(0, 3)
	e.ClearFormatting()
	if !e.Doc.Blocks[0].Runs[0].Style.Bold {
		t.Fatal("ReadOnly should not clear selection style")
	}
	e.ReadOnly = false

	// Collapsed caret resets typing style so subsequent typing is plain.
	e.SetSelection(3, 3)
	e.ClearFormatting()
	if e.CurrentStyle() != (Style{}) || !e.Explicit {
		t.Fatalf("collapsed caret clear did not reset typing style: %+v explicit=%v", e.CurrentStyle(), e.Explicit)
	}
	e.Replace(3, 3, " plain")
	runs := e.Doc.Blocks[0].Runs
	if len(runs) != 2 || runs[1].Text != " plain" || runs[1].Style != (Style{}) {
		t.Fatalf("typed text after clear carried style: %+v", runs)
	}
}

func TestClearBlocksFormatting(t *testing.T) {
	doc := New()
	h := NewBlock("heading", "")
	h.Level = 1
	h.Folded = true
	h.Runs = []Run{{Text: "Title", Style: Style{Color: "red", Bold: true}}}
	hidden := NewBlock("paragraph", "")
	hidden.Runs = []Run{{Text: "Hidden", Style: Style{Color: "green"}}}
	tbl := Table(1, 2)
	tbl.Children[0].Children[0].Runs = []Run{{Text: "Cell", Style: Style{Highlight: "yellow", Link: "https://example.com", NoMath: true}}}
	unselected := NewBlock("paragraph", "")
	unselected.Runs = []Run{{Text: "Keep", Style: Style{Italic: true}}}
	doc.Blocks = []*Block{h, hidden, tbl, unselected}

	e := NewEngine(doc)
	h = e.Doc.Blocks[0]
	hidden = e.Doc.Blocks[1]
	tbl = e.Doc.Blocks[2]
	unselected = e.Doc.Blocks[3]
	e.ClearBlocksFormatting([]string{h.ID, tbl.ID})

	if h.Runs[0].Style != (Style{}) {
		t.Fatalf("heading style not cleared: %+v", h.Runs[0].Style)
	}
	if tbl.Children[0].Children[0].Runs[0].Style != (Style{NoMath: true}) {
		t.Fatalf("table cell style not cleared cleanly: %+v", tbl.Children[0].Children[0].Runs[0].Style)
	}
	// Folded hidden block and unselected block remain untouched.
	if hidden.Runs[0].Style.Color != "green" {
		t.Fatalf("folded hidden block should stay untouched: %+v", hidden.Runs[0].Style)
	}
	if !unselected.Runs[0].Style.Italic {
		t.Fatalf("unselected block should stay untouched: %+v", unselected.Runs[0].Style)
	}

	// Single undo restores both heading and table cell.
	e.Undo()
	h = e.Doc.Blocks[0]
	tbl = e.Doc.Blocks[2]
	if h.Runs[0].Style.Color != "red" || tbl.Children[0].Children[0].Runs[0].Style.Highlight != "yellow" {
		t.Fatalf("undo did not restore multi-block formatting: h=%+v cell=%+v", h.Runs[0].Style, tbl.Children[0].Children[0].Runs[0].Style)
	}
}
