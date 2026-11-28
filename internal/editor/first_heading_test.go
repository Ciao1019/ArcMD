package editor

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/egoist/mygo/ui"
)

func TestHeadingDefaultAndCustomBindings(t *testing.T) {
	defs := DefaultBindings()
	for i, k := range []ui.Key{ui.Key0, ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6} {
		cmd := fmt.Sprintf("heading-%d", i)
		want := Combo{Mods: ui.Cmd | ui.Alt, Key: k}
		if got, ok := defs[cmd]; !ok || got != want {
			t.Fatalf("DefaultBindings()[%q] = %+v (ok=%v), want %+v", cmd, got, ok, want)
		}
	}

	// Rebind heading-0..6 to Cmd+0..6 (Typora style) and test both prose and multi-block selection.
	custom := map[string]Combo{
		"heading-0": {Mods: ui.Cmd, Key: ui.Key0},
		"heading-1": {Mods: ui.Cmd, Key: ui.Key1},
		"heading-2": {Mods: ui.Cmd, Key: ui.Key2},
		"heading-3": {Mods: ui.Cmd, Key: ui.Key3},
		"heading-4": {Mods: ui.Cmd, Key: ui.Key4},
		"heading-5": {Mods: ui.Cmd, Key: ui.Key5},
		"heading-6": {Mods: ui.Cmd, Key: ui.Key6},
	}

	e := New("# \n\n第一段\n\n第二段")
	e.RequestFocus = true
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), Bindings: custom})
	}, 960, 640)
	tt.Frame()

	// Prose mode on Blocks[0]: Cmd+2 turns H1 into H2; old Cmd+Alt+1 no longer triggers heading-1.
	tt.Key(ui.Cmd, ui.Key2)
	tt.Frame()
	if b := e.Doc.Blocks[0]; b.Kind != "heading" || b.Level != 2 {
		t.Fatalf("Blocks[0] after Cmd+2 = (%q, %d), want (heading, 2)", b.Kind, b.Level)
	}
	tt.Key(ui.Cmd|ui.Alt, ui.Key1)
	tt.Frame()
	if b := e.Doc.Blocks[0]; b.Kind != "heading" || b.Level != 2 {
		t.Fatalf("rebound Cmd+Alt+1 should not change level, got (%q, %d)", b.Kind, b.Level)
	}
	tt.Key(ui.Cmd, ui.Key6)
	tt.Frame()
	if b := e.Doc.Blocks[0]; b.Kind != "heading" || b.Level != 6 {
		t.Fatalf("Blocks[0] after Cmd+6 = (%q, %d), want (heading, 6)", b.Kind, b.Level)
	}
	tt.Key(ui.Cmd, ui.Key0)
	tt.Frame()
	if b := e.Doc.Blocks[0]; b.Kind != "paragraph" || b.Level != 0 {
		t.Fatalf("Blocks[0] after Cmd+0 = (%q, %d), want (paragraph, 0)", b.Kind, b.Level)
	}

	// Multi-block selection mode: select all blocks, press Cmd+3 -> all become H3, then Cmd+0 -> paragraphs.
	e.SelectAllBlocks()
	tt.Frame()
	tt.Key(ui.Cmd, ui.Key3)
	tt.Frame()
	for i, b := range e.Doc.Blocks {
		if b.Kind != "heading" || b.Level != 3 {
			t.Fatalf("multi-block Cmd+3 block[%d] = (%q, %d), want (heading, 3)", i, b.Kind, b.Level)
		}
	}
	tt.Key(ui.Cmd, ui.Key0)
	tt.Frame()
	for i, b := range e.Doc.Blocks {
		if b.Kind != "paragraph" || b.Level != 0 {
			t.Fatalf("multi-block Cmd+0 block[%d] = (%q, %d), want (paragraph, 0)", i, b.Kind, b.Level)
		}
	}
}

func TestFirstHeadingH2ToH6MetricsAndPlaceholders(t *testing.T) {
	theme := ui.LightTheme()

	// H1 on Blocks[0] remains the 34px page title.
	h1Ed := New("# ")
	h1Ed.layout(Options{Width: 900, Theme: theme})
	if len(h1Ed.rows) == 0 || h1Ed.rows[0].size != 34 {
		t.Fatalf("H1 first block size = %v, want 34", h1Ed.rows[0].size)
	}
	if !h1Ed.isPageTitle(h1Ed.Doc.Blocks[0]) || !h1Ed.Blank() {
		t.Fatal("empty H1 first block should be page title and Blank()")
	}

	// H2..H6 on Blocks[0] must use standard heading sizes (22, 20, 18, 16, 16), never 34px!
	wantSizes := map[int]float32{
		2: 22,
		3: 20,
		4: 18,
		5: 16,
		6: 16,
	}
	for level := 2; level <= 6; level++ {
		md := strings.Repeat("#", level) + " 美剧"
		ed := New(md)
		ed.layout(Options{Width: 900, Theme: theme})
		if ed.isPageTitle(ed.Doc.Blocks[0]) {
			t.Fatalf("H%d first block must not be treated as 34px page title", level)
		}
		if got := ed.rows[0].size; got != wantSizes[level] {
			t.Fatalf("H%d first block size = %v, want %v", level, got, wantSizes[level])
		}
		// Compare against the same heading level placed at Blocks[1] to verify identical font size.
		second := New("正文\n\n" + md)
		second.layout(Options{Width: 900, Theme: theme})
		if ed.rows[0].size != second.rows[1].size {
			t.Fatalf("H%d at Blocks[0] size %v != Blocks[1] size %v", level, ed.rows[0].size, second.rows[1].size)
		}

		// Empty H2..H6 at Blocks[0] must render without panicking and report Blank() == false.
		emptyDoc := document.New()
		emptyDoc.Blocks = []*document.Block{document.NewBlock("heading", "")}
		emptyDoc.Blocks[0].Level = level
		emptyEd := FromDocument(emptyDoc)
		emptyImg := ui.Render(func(c *ui.Context) {
			View(c, emptyEd, Options{Width: 640, Theme: theme})
		}, 640, 200, 1)
		if emptyImg == nil {
			t.Fatalf("render empty H%d failed", level)
		}
		if emptyEd.rows[0].size != wantSizes[level] {
			t.Fatalf("empty H%d first block size = %v, want %v", level, emptyEd.rows[0].size, wantSizes[level])
		}
		if emptyEd.Blank() {
			t.Fatalf("empty H%d first block should not report Blank() page-title starter state", level)
		}
	}
}

func TestFirstHeadingAllConversionPaths(t *testing.T) {
	// 1. Typing "## " .. "###### " inside an empty H1 (newly created doc state) converts Blocks[0] cleanly!
	for level := 2; level <= 6; level++ {
		doc := document.New()
		h1 := document.NewBlock("heading", "")
		h1.Level = 1
		doc.Blocks = []*document.Block{h1, document.NewBlock("paragraph", "")}
		e := FromDocument(doc)
		e.RequestFocus = true
		tt := ui.NewTester(func(c *ui.Context) {
			w, _ := c.Size()
			View(c, e, Options{Width: w, Theme: c.Theme()})
		}, 900, 500)
		tt.Frame()

		tt.Type(strings.Repeat("#", level) + " 美剧")
		tt.Type("推荐")
		tt.Frame()
		b := e.Doc.Blocks[0]
		if b.Kind != "heading" || b.Level != level || b.Text() != "美剧推荐" {
			t.Fatalf("typing %d hashes in empty H1 gave kind=%q level=%d text=%q", level, b.Kind, b.Level, b.Text())
		}
		if e.rows[0].size == 34 {
			t.Fatalf("converted H%d still laid out at 34px", level)
		}

		// Also verify key-by-key typing ("##" then " " then text) in empty H1 Blocks[0].
		stepDoc := document.New()
		sh := document.NewBlock("heading", "")
		sh.Level = 1
		stepDoc.Blocks = []*document.Block{sh, document.NewBlock("paragraph", "")}
		stepEd := FromDocument(stepDoc)
		stepEd.RequestFocus = true
		stepTT := ui.NewTester(func(c *ui.Context) {
			w, _ := c.Size()
			View(c, stepEd, Options{Width: w, Theme: c.Theme()})
		}, 900, 500)
		stepTT.Frame()
		stepTT.Type(strings.Repeat("#", level))
		stepTT.Type(" ")
		stepTT.Type("美剧")
		stepTT.Frame()
		if sb := stepEd.Doc.Blocks[0]; sb.Kind != "heading" || sb.Level != level || sb.Text() != "美剧" {
			t.Fatalf("stepwise typing %d hashes in empty H1 gave kind=%q level=%d text=%q", level, sb.Kind, sb.Level, sb.Text())
		}
	}

	// Typing "1. 引言" inside an H1 must keep the literal numbered heading text rather than turning into a list!
	numDoc := document.New()
	h1 := document.NewBlock("heading", "")
	h1.Level = 1
	numDoc.Blocks = []*document.Block{h1}
	numEd := FromDocument(numDoc)
	numEd.RequestFocus = true
	ntt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, numEd, Options{Width: w, Theme: c.Theme()})
	}, 900, 400)
	ntt.Frame()
	ntt.Type("1. 引言")
	ntt.Frame()
	if b := numEd.Doc.Blocks[0]; b.Kind != "heading" || b.Level != 1 || b.Text() != "1. 引言" {
		t.Fatalf("numbered heading text turned into (%q, %d, %q)", b.Kind, b.Level, b.Text())
	}

	// 2. Slash menu ("/") inside an empty H1 Blocks[0] converts Blocks[0] to H2!
	slashDoc := document.New()
	sh1 := document.NewBlock("heading", "")
	sh1.Level = 1
	slashDoc.Blocks = []*document.Block{sh1, document.NewBlock("paragraph", "")}
	se := FromDocument(slashDoc)
	se.RequestFocus = true
	stt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, se, Options{Width: w, Theme: c.Theme()})
	}, 900, 500)
	stt.Frame()
	stt.Type("/h2")
	stt.Frame()
	if !se.slashOpen {
		t.Fatal("slash menu did not open in empty Blocks[0] heading")
	}
	stt.Key(0, ui.KeyEnter)
	stt.Frame()
	if b := se.Doc.Blocks[0]; b.Kind != "heading" || b.Level != 2 || b.Text() != "" {
		t.Fatalf("slash H2 on Blocks[0] gave (%q, %d, %q)", b.Kind, b.Level, b.Text())
	}

	// 3. Block handle menu & Backspace at start of Blocks[0] heading!
	se.TypeTextPlaceholderHelper("美剧")
	stt.Frame()
	se.OpenBlockMenu(se.Doc.Blocks[0].ID, "")
	stt.Frame()
	if err := stt.Click("三级标题"); err != nil {
		t.Fatalf("click 三级标题 in block menu: %v", err)
	}
	stt.Frame()
	if b := se.Doc.Blocks[0]; b.Kind != "heading" || b.Level != 3 || b.Text() != "美剧" {
		t.Fatalf("block menu H3 on Blocks[0] gave (%q, %d, %q)", b.Kind, b.Level, b.Text())
	}

	// Backspace at start of Blocks[0] heading turns it into a paragraph.
	se.SetSelection(0, 0)
	stt.Frame()
	stt.Key(0, ui.KeyBackspace)
	stt.Frame()
	if b := se.Doc.Blocks[0]; b.Kind != "paragraph" || b.Text() != "美剧" {
		t.Fatalf("Backspace at start of Blocks[0] heading gave (%q, %q), want (paragraph, 美剧)", b.Kind, b.Text())
	}
}

func (e *Editor) TypeTextPlaceholderHelper(s string) {
	e.ReplaceText(nil, s)
}

func TestOutlineSectionMoveMindMapAndExportWithFirstH2(t *testing.T) {
	// 1. Single H2 at Blocks[0] shows the outline!
	single := New("## 美剧\n\n绝命毒师\n")
	open := true
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, single, Options{Width: w, Theme: c.Theme(), Outline: open, SetOutline: func(o bool) { open = o }})
	}, 1280, 600)
	tt.Frame()
	if !single.hasHeadings() || !single.OutlineOpen() || !tt.HasText("跳到 美剧") {
		t.Fatalf("single H2 doc did not show outline: has=%v open=%v outline=%+v", single.hasHeadings(), single.OutlineOpen(), single.outline)
	}

	// 2. Multiple H2 sections starting at Blocks[0] allow MoveSectionAtCaret up/down across index 0!
	multi := New("## 美剧\n\n绝命毒师\n\n## 电影\n\n盗梦空间\n")
	// Place caret in second H2 ("电影") and move it up before index 0 ("美剧").
	for _, leaf := range multi.Doc.Leaves() {
		if leaf.Block.Text() == "电影" {
			multi.SetSelection(leaf.Start, leaf.Start)
			break
		}
	}
	multi.MoveSectionAtCaret(-1)
	var texts []string
	for _, b := range multi.Doc.Blocks {
		if b.Text() != "" {
			texts = append(texts, b.Text())
		}
	}
	want := []string{"电影", "盗梦空间", "美剧", "绝命毒师"}
	if !slices.Equal(texts, want) {
		t.Fatalf("MoveSectionAtCaret(-1) to index 0 = %q, want %q", texts, want)
	}
	// Now caret is in "电影" at index 0; moving down (+1) moves it back after "美剧"!
	multi.MoveSectionAtCaret(1)
	texts = nil
	for _, b := range multi.Doc.Blocks {
		if b.Text() != "" {
			texts = append(texts, b.Text())
		}
	}
	wantOrig := []string{"美剧", "绝命毒师", "电影", "盗梦空间"}
	if !slices.Equal(texts, wantOrig) {
		t.Fatalf("MoveSectionAtCaret(+1) from index 0 = %q, want %q", texts, wantOrig)
	}

	// 3. MindMap keeps sibling H2s at depth 0 under DocTitle instead of promoting Blocks[0] H2 to root!
	mm := &MindMap{}
	mm.sync(multi, Options{Theme: ui.LightTheme(), DocTitle: "片单"})
	if mm.title.Text != "片单" {
		t.Fatalf("MindMap root title = %q, want 片单", mm.title.Text)
	}
	var h2Depths []int
	for _, it := range mm.items {
		if it.Text == "美剧" || it.Text == "电影" {
			h2Depths = append(h2Depths, it.Depth)
		}
	}
	if !slices.Equal(h2Depths, []int{0, 0}) {
		t.Fatalf("MindMap H2 depths = %v, want [0 0]", h2Depths)
	}

	// 4. HTML & Vector PDF export preserve H2 tag, level, and 22px metrics.
	htmlOut := ExportHTML(single.Doc)
	if !strings.Contains(htmlOut, "<h2>美剧</h2>") || strings.Contains(htmlOut, "<h1>") {
		t.Fatalf("ExportHTML should emit <h2>美剧</h2>, got: %s", htmlOut)
	}
	outlines := single.ExportOutlines([]float32{0}, 1123, 48, 842)
	if len(outlines) != 1 || outlines[0].Title != "美剧" || outlines[0].Level != 2 {
		t.Fatalf("ExportOutlines = %+v, want single level-2 美剧", outlines)
	}
	pw := pdf.NewWriter(io.Discard, "test")
	vp := pw.NewVectorPage(595, 842)
	single.ExportVectorPage(vp, Options{Width: 794, Theme: ui.LightTheme()}, 0, 1000, 0, 1, []float32{0}, 794, 1123, 48)
}
