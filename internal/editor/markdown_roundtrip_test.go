package editor

import (
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/document"
)

func TestRichMarkdownLosslessRoundTripAllFeatures(t *testing.T) {
	doc := document.New()
	doc.Icon = "🚀"
	doc.Cover = "assets/banner.png"
	doc.CoverY = -0.35
	doc.Goal = 1200
	doc.Frontmatter = "title: Native Markdown\ntags:\n  - design\n  - notes"

	h1 := document.NewBlock("heading", "")
	h1.Level = 1
	h1.Align = "center"
	h1.Runs = []document.Run{
		{Text: "ArcMD ", Style: document.Style{Bold: true}},
		{Text: "Native", Style: document.Style{Bold: true, Color: "blue", Annotation: "wavy", AnnotationColor: "purple"}},
	}

	para := document.NewBlock("paragraph", "")
	para.Runs = []document.Run{
		{Text: "Plain, "},
		{Text: "marked", Style: document.Style{Highlight: "light-yellow"}},
		{Text: ", "},
		{Text: "colored", Style: document.Style{Color: "green", Highlight: "light-green", Italic: true}},
		{Text: ", "},
		{Text: "circled", Style: document.Style{Annotation: "box", AnnotationColor: "orange"}},
		{Text: ", price $100", Style: document.Style{NoMath: true}},
		{Text: " and $E=mc^2$[^n1]."},
	}

	indented := document.NewBlock("paragraph", "Indented right-aligned note")
	indented.Indent = 2
	indented.Align = "right"

	callout := document.NewBlock("callout", "Custom emoji callout")
	callout.Color = "purple"
	callout.Emoji = "🎯"
	callout.Align = "center"

	img := document.NewBlock("image", "Architecture diagram")
	img.URL = "assets/arch.png"
	img.ImageWidth = 420
	img.Align = "center"

	task0 := document.NewBlock("task", "Parent task")
	task1 := document.NewBlock("task", "Nested done task")
	task1.Indent = 1
	task1.Checked = true

	toggle := document.NewBlock("toggle", "")
	toggle.Folded = false
	toggle.Runs = []document.Run{
		{Text: "Expand ", Style: document.Style{Bold: true}},
		{Text: "details", Style: document.Style{Color: "red"}},
	}
	toggleBody := document.NewBlock("paragraph", "Inside toggle")
	toggleBody.Indent = 1

	// Rich table with column widths, colspan, rowspan, cell background and multiline text.
	table := document.Table(3, 3)
	table.Widths = []float32{160, 220, 180}
	c00 := table.Children[0].Children[0]
	c00.Runs = []document.Run{{Text: "Merged Header", Style: document.Style{Bold: true}}}
	c00.ColSpan = 2
	c00.Align = "center"
	c00.Background = "light-blue"
	table.Children[0].Children[2].Runs = []document.Run{{Text: "Status"}}
	table.Children[0].Children[2].Align = "right"

	c10 := table.Children[1].Children[0]
	c10.Runs = []document.Run{{Text: "RowSpan\nLine 2", Style: document.Style{Color: "purple"}}}
	c10.RowSpan = 2
	c10.Background = "light-purple"
	table.Children[1].Children[1].Runs = []document.Run{{Text: "Alpha"}}
	table.Children[1].Children[2].Runs = []document.Run{{Text: "Done", Style: document.Style{Highlight: "light-green"}}}
	table.Children[2].Children[1].Runs = []document.Run{{Text: "Beta"}}
	table.Children[2].Children[2].Runs = []document.Run{{Text: "Todo"}}

	// Multi-column block with one populated column and one empty column.
	cols := document.Columns()
	cols.Children[0].Children = []*document.Block{
		document.NewBlock("paragraph", "Left column paragraph"),
		document.NewBlock("bullet", "Left bullet"),
	}

	fn := document.NewBlock("footnote", "Footnote explanation")
	fn.NoteLabel = "n1"

	doc.Blocks = []*document.Block{
		h1, para, indented, callout, img, task0, task1, toggle, toggleBody, table, cols, fn,
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("fixture invalid: %v", err)
	}

	md := ExportMarkdown(doc)
	if !strings.Contains(md, "<mark>marked</mark>") {
		t.Fatalf("expected semantic <mark> for yellow highlight, got:\n%s", md)
	}

	got := ImportMarkdown(md)
	if err := got.Validate(); err != nil {
		t.Fatalf("re-imported document invalid: %v\nmarkdown:\n%s", err, md)
	}
	if !sameDocumentSemantics(doc, got) {
		for i := 0; i < max(len(doc.Blocks), len(got.Blocks)); i++ {
			if i >= len(doc.Blocks) || i >= len(got.Blocks) {
				t.Fatalf("block count %d vs %d", len(doc.Blocks), len(got.Blocks))
			}
			if !sameBlockSemantics(doc.Blocks[i], got.Blocks[i]) {
				t.Fatalf("block %d (%s vs %s) mismatch:\n doc: %+v\n got: %+v", i, doc.Blocks[i].Kind, got.Blocks[i].Kind, doc.Blocks[i], got.Blocks[i])
			}
		}
		t.Fatalf("doc metadata mismatch: doc=%+v got=%+v", doc, got)
	}
}

func TestAllPaletteColorsAndAnnotationsRoundTrip(t *testing.T) {
	doc := document.New()
	var runs []document.Run
	for _, c := range document.TextColors {
		runs = append(runs, document.Run{Text: c, Style: document.Style{Color: c}}, document.Run{Text: " "})
	}
	for _, h := range document.TextHighlights {
		runs = append(runs, document.Run{Text: h, Style: document.Style{Highlight: h}}, document.Run{Text: " "})
	}
	for i, a := range document.Annotations {
		ac := document.TextColors[i%len(document.TextColors)]
		runs = append(runs, document.Run{Text: a, Style: document.Style{Annotation: a, AnnotationColor: ac}}, document.Run{Text: " "})
	}
	// Intraword CJK + bold + color + annotation without spaces.
	runs = append(runs,
		document.Run{Text: "前缀"},
		document.Run{Text: "高亮重点", Style: document.Style{Bold: true, Italic: true, Color: "red", Highlight: "yellow", Annotation: "underline", AnnotationColor: "blue"}},
		document.Run{Text: "后缀"},
	)
	p := document.NewBlock("paragraph", "")
	p.Runs = document.Compact(runs)
	doc.Blocks = []*document.Block{p}

	md := ExportMarkdown(doc)
	got := ImportMarkdown(md)
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	if !sameDocumentSemantics(doc, got) {
		t.Fatalf("palette/annotation round-trip mismatch:\n%s", md)
	}
}

func TestExternalMarkdownExtensionsImport(t *testing.T) {
	src := "External ==important text== and a == b and ![diagram|280](pic.png)\n\n![solo|360](hero.png)\n"
	doc := ImportMarkdown(src)
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
	runs := doc.Blocks[0].Runs
	if len(runs) < 3 || runs[1].Text != "important text" || runs[1].Style.Highlight != "light-yellow" {
		t.Fatalf("expected ==important text== highlight, got %+v", runs)
	}
	if !strings.Contains(doc.Blocks[0].Text(), "a == b") {
		t.Fatalf("equality comparison corrupted: %q", doc.Blocks[0].Text())
	}
	if doc.Blocks[1].Kind != "image" || doc.Blocks[1].ImageWidth != 360 || doc.Blocks[1].Text() != "solo" {
		t.Fatalf("expected Obsidian width 360 on solo image, got %+v", doc.Blocks[1])
	}
}

func TestPartialEditPreservesUneditedAuthorMarkdown(t *testing.T) {
	original := "---\ntitle: Keep Me\n---\n\nSetext Heading\n==============\n\nFirst paragraph to edit.\n\n* asterisk item one\n* asterisk item two\n\nParagraph with _underscore italic_ kept.\n"
	doc := ImportMarkdown(original)
	if got := ExportMarkdown(doc); got != original {
		t.Fatalf("unedited export changed bytes:\n got: %q\nwant: %q", got, original)
	}
	e := document.NewEngine(doc)
	// Edit only the first paragraph ("First paragraph to edit.").
	leaf := e.Doc.Leaves()[1]
	e.Replace(leaf.Start, leaf.End, "Updated first paragraph.")
	exported := ExportMarkdown(e.Doc)
	for _, want := range []string{
		"title: Keep Me",
		"Setext Heading\n==============",
		"Updated first paragraph.",
		"* asterisk item one\n* asterisk item two",
		"Paragraph with _underscore italic_ kept.",
	} {
		if !strings.Contains(exported, want) {
			t.Fatalf("partial edit lost author formatting %q in:\n%s", want, exported)
		}
	}
}

func TestWikilinksTagsAndBracketsRoundTripCleanly(t *testing.T) {
	doc := document.New()
	text := "See [[Ideas]], [[Ideas|my notes]], [[Journal/2026_11_14#Plan|note_1]], [[Ideas]](ref), #work/2026, and [TODO] array[0]."
	doc.Blocks = []*document.Block{document.NewBlock("paragraph", text)}

	md := ExportMarkdown(doc)
	for _, plain := range []string{
		"[[Ideas]]",
		"[[Ideas|my notes]]",
		"[[Journal/2026_11_14#Plan|note_1]]",
		"#work/2026",
	} {
		if !strings.Contains(md, plain) {
			t.Fatalf("expected unescaped %q in exported Markdown:\n%s", plain, md)
		}
	}

	round := ImportMarkdown(md)
	if got := round.Blocks[0].Text(); got != text {
		t.Fatalf("round-trip text mismatch:\n got: %q\nwant: %q\n  md: %q", got, text, md)
	}
}

func TestBlockSemanticHashMatchesSemanticsAndLargeExport(t *testing.T) {
	pairs := [][2]*document.Block{
		{
			{Kind: "paragraph", Runs: []document.Run{{Text: "hello world", Style: document.Style{NoMath: true}}}},
			{Kind: "paragraph", Runs: []document.Run{{Text: "hello "}, {Text: "world"}}},
		},
		{
			{Kind: "paragraph", Runs: []document.Run{{Text: "price $5 + $6", Style: document.Style{NoMath: true}}}},
			{Kind: "paragraph", Runs: []document.Run{{Text: "price "}, {Text: "$", Style: document.Style{NoMath: true}}, {Text: "5 + "}, {Text: "$", Style: document.Style{NoMath: true}}, {Text: "6"}}},
		},
		{
			{Kind: "paragraph", Align: "left", Folded: true, Runs: []document.Run{{Text: "aligned"}}},
			{Kind: "paragraph", Align: "", Folded: false, Runs: []document.Run{{Text: "aligned"}}},
		},
		{
			func() *document.Block {
				b := document.NewBlock("toggle", "toggle title")
				b.Folded = true
				return b
			}(),
			func() *document.Block {
				b := document.NewBlock("toggle", "toggle title")
				b.Folded = true
				return b
			}(),
		},
		{document.Table(2, 2), document.Table(2, 2)},
	}
	for idx, pair := range pairs {
		if !sameBlockSemantics(pair[0], pair[1]) {
			t.Fatalf("pair %d expected sameBlockSemantics == true", idx)
		}
		if blockSemanticHash(pair[0]) != blockSemanticHash(pair[1]) {
			t.Fatalf("pair %d semantic hash mismatch: %x vs %x", idx, blockSemanticHash(pair[0]), blockSemanticHash(pair[1]))
		}
	}

	var b strings.Builder
	b.Grow(280_000)
	for i := 0; i < 2000; i++ {
		b.WriteString("* duplicate author bullet with _underscore_\n\n")
		b.WriteString("Unedited author paragraph _kept verbatim_ across **many** sections.\n\n")
	}
	src := b.String()
	doc := ImportMarkdown(src)
	if len(doc.Blocks) < 4000 {
		t.Fatalf("expected >= 4000 blocks, got %d", len(doc.Blocks))
	}
	doc.Blocks[1].Runs = []document.Run{{Text: "Edited first paragraph cleanly."}}
	mid := len(doc.Blocks) / 2
	doc.Blocks[mid].Runs = []document.Run{{Text: "Edited middle block cleanly."}}

	out := ExportMarkdown(doc)
	if !strings.Contains(out, "Edited first paragraph cleanly.") || !strings.Contains(out, "Edited middle block cleanly.") {
		t.Fatal("ExportMarkdown dropped edited blocks")
	}
	if !strings.Contains(out, "* duplicate author bullet with _underscore_") || !strings.Contains(out, "Unedited author paragraph _kept verbatim_ across **many** sections.") {
		t.Fatal("ExportMarkdown failed to preserve unedited author-formatted chunks")
	}
}
