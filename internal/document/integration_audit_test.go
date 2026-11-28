package document

import (
	"strings"
	"testing"
)

func TestUnifiedV11MigrationAndCellBackgroundCompatibility(t *testing.T) {
	// v4–v10 table cell backgrounds remain valid when migrating to v11.
	v10TableJSON := `{
		"format": "arcmd",
		"version": 10,
		"blocks": [{
			"id": "t1",
			"kind": "table",
			"children": [{
				"id": "r1",
				"kind": "row",
				"children": [{
					"id": "c1",
					"kind": "cell",
					"background": "blue",
					"runs": [{"text": "ok"}]
				}]
			}]
		}]
	}`
	doc, err := Decode(v10TableJSON)
	if err != nil {
		t.Fatalf("v10 table cell background should migrate cleanly to v11: %v", err)
	}
	if doc.Version != CurrentVersion || doc.Blocks[0].Children[0].Children[0].Background != "blue" {
		t.Fatalf("migrated doc = v%d bg=%q", doc.Version, doc.Blocks[0].Children[0].Children[0].Background)
	}

	// Pre-v11 documents smuggling v11-only fields (inline image, ImageHeight, or text block Background) must be rejected.
	invalidV10Cases := []string{
		`{"format":"arcmd","version":10,"blocks":[{"id":"p1","kind":"paragraph","background":"blue","runs":[{"text":"x"}]}]}`,
		`{"format":"arcmd","version":10,"blocks":[{"id":"i1","kind":"image","url":"a.png","imageHeight":40,"runs":[{"text":""}]}]}`,
		`{"format":"arcmd","version":10,"blocks":[{"id":"p2","kind":"paragraph","runs":[{"text":"\ufffc","image":{"url":"a.svg"}}]}]}`,
	}
	for i, raw := range invalidV10Cases {
		if _, err := Decode(raw); err == nil {
			t.Fatalf("case %d: expected pre-v11 rejection for v11 feature, got nil", i)
		}
	}
}

func TestVisiblePlainTextAndPasteInlineImagesIntoCellAndCode(t *testing.T) {
	frag := New()
	p1 := NewBlock("paragraph", "")
	p1.Runs = []Run{
		{Text: "Status "},
		InlineImageRun(InlineImage{URL: "ci.svg", Alt: "PASS"}, Style{}),
		{Text: " "},
		InlineImageRun(InlineImage{URL: " silent.svg", Alt: ""}, Style{}),
		{Text: "end"},
	}
	p2 := NewBlock("paragraph", "")
	p2.Runs = []Run{
		InlineImageRun(InlineImage{URL: "v.svg", Alt: "v1.0"}, Style{}),
	}
	frag.Blocks = []*Block{p1, p2}

	if got, want := p1.VisibleText(), "Status PASS end"; got != want {
		t.Fatalf("VisibleText() = %q; want %q", got, want)
	}
	if got, want := frag.VisiblePlainText(), "Status PASS end\nv1.0"; got != want {
		t.Fatalf("VisiblePlainText() = %q; want %q", got, want)
	}
	if !strings.Contains(frag.PlainText(), InlineImagePlaceholder) {
		t.Fatal("PlainText() must retain UTF-16 placeholder alignment for editor buffer")
	}

	// Multi-block paste into a table cell expands inline images to Alt without leaking \ufffc.
	eng := NewEngine(New())
	tbl := Table(1, 1)
	eng.Doc.Blocks = []*Block{tbl}
	leaves := eng.Doc.Leaves()
	eng.SetSelection(leaves[0].Start, leaves[0].Start)
	eng.Paste(frag)
	if err := eng.Doc.Validate(); err != nil {
		t.Fatalf("cell paste produced invalid document: %v", err)
	}
	cellTxt := tbl.Children[0].Children[0].Text()
	if strings.Contains(cellTxt, InlineImagePlaceholder) || cellTxt != "Status PASS end\nv1.0" {
		t.Fatalf("cell text after multi-block paste = %q", cellTxt)
	}

	// Single-block paste with inline images into a code block expands Alt cleanly and keeps Caret valid.
	singleFrag := New()
	singleFrag.Blocks = []*Block{p1.Clone()}
	codeEng := NewEngine(&Document{
		Format:  "arcmd",
		Version: CurrentVersion,
		Blocks:  []*Block{NewBlock("code", "prefix ")},
	})
	codeLeaf := codeEng.Doc.Leaves()[0]
	codeEng.SetSelection(codeLeaf.End, codeLeaf.End)
	codeEng.Paste(singleFrag)
	if err := codeEng.Doc.Validate(); err != nil {
		t.Fatalf("code paste produced invalid document: %v", err)
	}
	codeTxt := codeEng.Doc.Blocks[0].Text()
	if strings.Contains(codeTxt, InlineImagePlaceholder) || codeTxt != "prefix Status PASS end" {
		t.Fatalf("code text after paste = %q", codeTxt)
	}
	if codeEng.Caret != Len(codeTxt) {
		t.Fatalf("Caret = %d; want %d", codeEng.Caret, Len(codeTxt))
	}
}
