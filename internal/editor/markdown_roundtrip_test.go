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

func TestObsidianImageEmbedsImportAndRoundTrip(t *testing.T) {
	src := "# Notes\n\n" +
		"![[Pasted image 20261027110938.png]]\n" +
		"![[Pasted image 20261027111017.png]]\n" +
		"The line chart entitled \"Percentage of households\" shows trends.\n\n" +
		"![[attachments/diagram.png|360]]\n\n" +
		"![[charts/summary.webp|360x240]]\n\n" +
		"![[shots/overview.png|Overview caption|420]]\n\n" +
		"Edited later.\n\n" +
		"See [[Meeting Notes]] and `inline code\n![[literal-not-image.png]]\nstill code`.\n"

	doc := ImportMarkdown(src)
	if err := doc.Validate(); err != nil {
		t.Fatalf("imported Obsidian doc invalid: %v", err)
	}
	if ExportMarkdown(doc) != src {
		t.Fatalf("unedited Obsidian Markdown changed:\n got: %q\nwant: %q", ExportMarkdown(doc), src)
	}

	var imgs []*document.Block
	for _, b := range doc.Blocks {
		if b.Kind == "image" {
			imgs = append(imgs, b)
		}
	}
	if len(imgs) != 5 {
		t.Fatalf("imported %d image blocks, want 5: %+v", len(imgs), doc.Blocks)
	}
	if imgs[0].URL != "Pasted image 20261027110938.png" || imgs[0].ImageWidth != 0 || imgs[0].Text() != "" {
		t.Fatalf("img[0] = %+v", imgs[0])
	}
	if imgs[1].URL != "Pasted image 20261027111017.png" || imgs[2].URL != "attachments/diagram.png" || imgs[2].ImageWidth != 360 {
		t.Fatalf("img[1..2] = %+v, %+v", imgs[1], imgs[2])
	}
	if imgs[3].URL != "charts/summary.webp" || imgs[3].ImageWidth != 360 || imgs[3].Text() != "" {
		t.Fatalf("img[3] = %+v", imgs[3])
	}
	if imgs[4].URL != "shots/overview.png" || imgs[4].ImageWidth != 420 || imgs[4].Text() != "Overview caption" {
		t.Fatalf("img[4] = %+v", imgs[4])
	}

	// Ensure prose immediately after consecutive ![[...]] lines became a normal paragraph.
	if len(doc.Blocks) < 5 || doc.Blocks[3].Kind != "paragraph" || !strings.HasPrefix(doc.Blocks[3].Text(), "The line chart") {
		t.Fatalf("expected paragraph after consecutive Obsidian images, got blocks[3]=%+v", doc.Blocks[3])
	}

	// Ensure multi-line inline code containing ![[...]] remained a paragraph with code runs, not an image block.
	last := doc.Blocks[len(doc.Blocks)-1]
	if last.Kind != "paragraph" || !strings.Contains(last.Text(), "![[literal-not-image.png]]") {
		t.Fatalf("code span embed was unexpectedly split: %+v", last)
	}

	// Editing an unrelated block preserves untouched ![[...]] syntax verbatim.
	e := document.NewEngine(doc)
	for _, leaf := range e.Doc.Leaves() {
		if leaf.Block.Kind == "paragraph" && leaf.Block.Text() == "Edited later." {
			e.Replace(leaf.Start, leaf.End, "Updated separately.")
			break
		}
	}
	partial := ExportMarkdown(e.Doc)
	if !strings.Contains(partial, "![[Pasted image 20261027110938.png]]\n![[Pasted image 20261027111017.png]]") ||
		!strings.Contains(partial, "Updated separately.") {
		t.Fatalf("partial edit lost Obsidian embeds:\n%s", partial)
	}

	// Editing an image block (or its chunk) still exports valid CommonMark with spaces safely wrapped in <...>.
	rework := ImportMarkdown("![[Pasted image 20261027110938.png]]\n\nText\n")
	rework.Blocks[0].Runs = []document.Run{{Text: "Pasted chart"}}
	exported := ExportMarkdown(rework)
	if !strings.Contains(exported, "![Pasted chart](<Pasted image 20261027110938.png>)") {
		t.Fatalf("expected angle-bracketed space URL on re-export, got:\n%s", exported)
	}
	reimported := ImportMarkdown(exported)
	if reimported.Blocks[0].Kind != "image" || reimported.Blocks[0].URL != "Pasted image 20261027110938.png" || reimported.Blocks[0].Text() != "Pasted chart" {
		t.Fatalf("re-import of space URL failed: %+v", reimported.Blocks[0])
	}
}

func TestHTMLTagsKbdCenterHrAndCommentsRoundTrip(t *testing.T) {
	src := strings.Join([]string{
		"Press <kbd>Ctrl</kbd>+<kbd>K</kbd> or <kbd>&lt;</kbd>, read <small>note</small>, <abbr title=\"HyperText\">HTML</abbr>, <var>x</var>, <q>quoted</q>, and inline <!-- hidden --> text.",
		"",
		"<hr>",
		"",
		"<div align=\"center\">",
		"",
		"# Centered Title",
		"",
		"Centered paragraph <!-- inline -->.",
		"",
		"<!-- block comment -->",
		"",
		"</div>",
		"",
		"<center>",
		"",
		"Second centered line",
		"",
		"Tail line",
		"",
		"</center>",
		"",
		"<!-- standalone comment -->",
		"",
		"<div align=\"right\"><img src=\"assets/pic.png\" width=\"240\" alt=\"badge\"></div>",
		"",
	}, "\n")

	doc := ImportMarkdown(src)
	if err := doc.Validate(); err != nil {
		t.Fatalf("imported doc invalid: %v", err)
	}
	if ExportMarkdown(doc) != src {
		t.Fatalf("unedited HTML Markdown changed:\n got:\n%s\nwant:\n%s", ExportMarkdown(doc), src)
	}

	// Verify inline HTML tokens on paragraph 0.
	p0 := doc.Blocks[0]
	if p0.Kind != "paragraph" {
		t.Fatalf("block 0 = %+v", p0)
	}
	foundCtrl, foundLT, foundVar := false, false, false
	for _, r := range p0.Runs {
		if r.Style.Kbd && !r.Style.Code && r.Text == "Ctrl" {
			foundCtrl = true
		}
		if r.Style.Kbd && r.Text == "<" {
			foundLT = true
		}
		if r.Style.Italic && r.Text == "x" {
			foundVar = true
		}
	}
	if !foundCtrl || !foundLT || !foundVar {
		t.Fatalf("missing expected inline HTML styles in %+v", p0.Runs)
	}
	txt0 := p0.Text()
	for _, want := range []string{"note", "HTML", "“quoted”", "inline  text."} {
		if !strings.Contains(txt0, want) {
			t.Fatalf("expected %q in %q", want, txt0)
		}
	}
	if strings.Contains(txt0, "hidden") || strings.Contains(txt0, "<!--") {
		t.Fatalf("inline comment leaked into text: %q", txt0)
	}

	// Verify <hr> became a native rule block.
	if doc.Blocks[1].Kind != "rule" {
		t.Fatalf("expected rule block for <hr>, got %+v", doc.Blocks[1])
	}

	// Verify multi-block <div align="center"> and <center> applied alignment and kept block comment.
	if doc.Blocks[2].Kind != "heading" || doc.Blocks[2].Align != "center" || doc.Blocks[2].Text() != "Centered Title" {
		t.Fatalf("expected centered heading, got %+v", doc.Blocks[2])
	}
	if doc.Blocks[3].Kind != "paragraph" || doc.Blocks[3].Align != "center" || strings.Contains(doc.Blocks[3].Text(), "<!--") {
		t.Fatalf("expected centered paragraph without inline comment, got %+v", doc.Blocks[3])
	}
	if doc.Blocks[4].Kind != "raw" || !isHTMLCommentBlock(doc.Blocks[4].Text()) {
		t.Fatalf("expected inner comment raw block, got %+v", doc.Blocks[4])
	}
	if doc.Blocks[5].Kind != "paragraph" || doc.Blocks[5].Align != "center" || doc.Blocks[5].Text() != "Second centered line" {
		t.Fatalf("expected centered line from <center>, got %+v", doc.Blocks[5])
	}
	if doc.Blocks[6].Kind != "paragraph" || doc.Blocks[6].Align != "center" || doc.Blocks[6].Text() != "Tail line" {
		t.Fatalf("expected second centered line from <center>, got %+v", doc.Blocks[6])
	}
	if doc.Blocks[7].Kind != "raw" || !isHTMLCommentBlock(doc.Blocks[7].Text()) {
		t.Fatalf("expected standalone comment block, got %+v", doc.Blocks[7])
	}
	if doc.Blocks[8].Kind != "image" || doc.Blocks[8].Align != "right" || doc.Blocks[8].ImageWidth != 240 || doc.Blocks[8].URL != "assets/pic.png" {
		t.Fatalf("expected right-aligned single-block div image, got %+v", doc.Blocks[8])
	}

	// ExportHTML omits comment blocks and emits <kbd>.
	exportedHTML := ExportHTML(doc)
	if !strings.Contains(exportedHTML, "<kbd>Ctrl</kbd>") || !strings.Contains(exportedHTML, "<kbd>&lt;</kbd>") {
		t.Fatalf("ExportHTML missing <kbd>: %s", exportedHTML)
	}
	if strings.Contains(exportedHTML, "block comment") || strings.Contains(exportedHTML, "standalone comment") {
		t.Fatalf("ExportHTML should omit HTML comment blocks: %s", exportedHTML)
	}

	// Partial edit inside <div align="center"> exports individual aligned blocks while keeping <center> verbatim.
	e := document.NewEngine(doc)
	e.Replace(e.Doc.Leaves()[3].Start, e.Doc.Leaves()[3].End, "Edited centered paragraph.")
	partial := ExportMarkdown(e.Doc)
	if !strings.Contains(partial, `<h1 align="center">Centered Title</h1>`) ||
		!strings.Contains(partial, `<p align="center">Edited centered paragraph.</p>`) ||
		!strings.Contains(partial, "<center>\n\nSecond centered line\n\nTail line\n\n</center>") {
		t.Fatalf("unexpected partial export:\n%s", partial)
	}

	// Round-trip freshly created <kbd> without ImportSource (including nested bold and '<').
	fresh := document.New()
	fresh.Blocks = []*document.Block{{
		ID:   document.NewID(),
		Kind: "paragraph",
		Runs: []document.Run{
			{Text: "Hit "},
			{Text: "Cmd", Style: document.Style{Kbd: true, Bold: true}},
			{Text: "+"},
			{Text: "<", Style: document.Style{Kbd: true}},
		},
	}}
	md := ExportMarkdown(fresh)
	if !strings.Contains(md, "<kbd>**Cmd**</kbd>+<kbd>&lt;</kbd>") {
		t.Fatalf("unexpected fresh <kbd> markdown: %q", md)
	}
	reloaded := ImportMarkdown(md)
	if !sameDocumentSemantics(fresh, reloaded) {
		t.Fatalf("fresh <kbd> round-trip mismatch:\n md=%s\n got=%+v", md, reloaded.Blocks[0].Runs)
	}
}
