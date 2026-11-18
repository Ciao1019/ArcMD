package editor

import (
	"fmt"
	"image"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/ui"
)

func TestMapLongBlocksAreBounded(t *testing.T) {
	tallBmp := ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 200, 1400)))

	// Build a document containing every kind of oversized single block.
	var sb strings.Builder
	sb.WriteString("# Long Blocks\n\n")
	// 1. 300-line code block
	sb.WriteString("```go\n")
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&sb, "line_%03d := computeValue(%d)\n", i, i)
	}
	sb.WriteString("```\n\n")
	// 2. 5000-rune minified 1-line code block
	sb.WriteString("```json\n{\"data\":\"")
	sb.WriteString(strings.Repeat("x", 5000))
	sb.WriteString("\"}\n```\n\n")
	// 3. 2500-rune paragraph with inline math near the 300-rune truncation boundary
	sb.WriteString(strings.Repeat("word ", 57)) // ~285 bytes
	sb.WriteString("$a_{1} + b_{2} + c_{3} + d_{4} + e_{5}$ ")
	sb.WriteString(strings.Repeat("tail ", 400))
	sb.WriteString("\n\n")
	// 4. 100-row x 10-col table
	sb.WriteString("| C0 | C1 | C2 | C3 | C4 | C5 | C6 | C7 | C8 | C9 |\n")
	sb.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for r := 0; r < 99; r++ {
		fmt.Fprintf(&sb, "| r%d | v1 | v2 | v3 | v4 | v5 | v6 | v7 | v8 | v9 |\n", r)
	}
	sb.WriteString("\n")
	// 5. Tall 200x1400 image
	sb.WriteString("![Tall](assets/tall.png)\n")

	ed := New(sb.String())
	m := &MindMap{}
	opts := Options{
		Theme: ui.LightTheme(),
		LoadImage: func(src string) ImageResource {
			return ImageResource{Bitmap: tallBmp, Width: 200, Height: 1400, Local: true}
		},
	}
	tt := ui.NewTester(func(c *ui.Context) {
		opts.Theme = c.Theme()
		m.View(c, ed, opts, 520, nil, false, func(bool) {}, "", nil)
	}, 520, 640)
	for range 4 {
		tt.Frame()
	}

	if m.zoom < 0.50 {
		t.Fatalf("long blocks shrank initial zoom too far: %v", m.zoom)
	}
	if len(m.items) != 5 {
		t.Fatalf("expected 5 items, got %d", len(m.items))
	}
	for i, it := range m.items {
		body := m.bodies[it.ID]
		if body == nil {
			t.Fatalf("item %d missing body", i)
		}
		if body.h > 210 {
			t.Fatalf("item %d (%s) height %v exceeds cap 210", i, body.source.Kind, body.h)
		}
	}

	codeBody := m.bodies[m.items[0].ID]
	if !codeBody.clipped || !strings.Contains(codeBody.moreHint, "300") {
		t.Fatalf("300-line code block hint=%q clipped=%v", codeBody.moreHint, codeBody.clipped)
	}
	minBody := m.bodies[m.items[1].ID]
	if !minBody.clipped || minBody.moreHint == "" {
		t.Fatalf("minified code block not clipped: hint=%q clipped=%v", minBody.moreHint, minBody.clipped)
	}
	proseBody := m.bodies[m.items[2].ID]
	if !proseBody.clipped {
		t.Fatal("2500-rune prose was not clipped")
	}
	if proseBody.editor != nil && len(proseBody.editor.Doc.Blocks) == 1 {
		txt := proseBody.editor.Doc.Blocks[0].Text()
		// Truncation must not leave an odd unescaped '$'.
		unescaped := 0
		for j := 0; j < len(txt); j++ {
			if txt[j] == '$' && (j == 0 || txt[j-1] != '\\') {
				unescaped++
			}
		}
		if unescaped%2 != 0 {
			t.Fatalf("truncated prose left unmatched '$' delimiter: %q", txt)
		}
	}
	tblBody := m.bodies[m.items[3].ID]
	if !tblBody.clipped || !strings.Contains(tblBody.moreHint, "93") {
		t.Fatalf("100-row table hint=%q clipped=%v", tblBody.moreHint, tblBody.clipped)
	}
	if origRows := len(ed.Doc.Blocks[4].Children); origRows != 100 {
		t.Fatalf("source table mutated: rows=%d", origRows)
	}
	imgBody := m.bodies[m.items[4].ID]
	if imgBody.h > mapMaxVisualHeight || imgBody.fitScale >= 1 {
		t.Fatalf("tall image not proportionally scaled: h=%v fitScale=%v", imgBody.h, imgBody.fitScale)
	}
}

func TestMapLargeDocAutoFoldBudgetAndIncrementalSync(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("# Large Document\n\n")
	for sec := 0; sec < 150; sec++ {
		fmt.Fprintf(&sb, "## Section %d\n\n", sec)
		for j := 0; j < 9; j++ {
			fmt.Fprintf(&sb, "- Item %d.%d with enough descriptive content to become rich\n", sec, j)
		}
		sb.WriteString("\n")
	}
	ed := New(sb.String()) // 1500+ blocks
	m := &MindMap{}
	tt := ui.NewTester(func(c *ui.Context) {
		m.View(c, ed, Options{Theme: c.Theme()}, 600, nil, false, func(bool) {}, "", nil)
	}, 600, 520)
	tt.Frame()
	tt.Frame()

	if len(m.items) < 1400 {
		t.Fatalf("expected 1400+ map items, got %d", len(m.items))
	}
	if !m.foldInitialized || !m.HasFolded() {
		t.Fatal("large document should auto-fold deeper branches on initial open")
	}
	if m.zoom < 0.55 {
		t.Fatalf("large document initial zoom dropped below 0.55: %v", m.zoom)
	}
	measured := 0
	for _, b := range m.bodies {
		if b.editor != nil || b.code != nil {
			measured++
		}
	}
	if measured > mapPrepareBudget*3 {
		t.Fatalf("too many rich nodes measured synchronously on initial frames: %d", measured)
	}

	// Incremental typing on an individual node reuses untouched bodies without re-measuring.
	targetBlock := ed.Doc.Blocks[2]
	keptBody := m.bodies[ed.Doc.Blocks[12].ID]
	leaf, ok := ed.Engine.LeafByID(targetBlock.ID)
	if !ok {
		t.Fatal("missing target leaf")
	}
	ed.SetSelection(leaf.End, leaf.End)
	ed.ReplaceText(nil, "!")
	tt.Frame()
	if m.bodies[ed.Doc.Blocks[12].ID] != keptBody {
		t.Fatal("unrelated block body was not reused during incremental edit")
	}
	if !strings.HasSuffix(ed.Doc.Blocks[2].Text(), "!") {
		t.Fatal("edit did not apply")
	}
	if !strings.HasSuffix(m.items[1].Text, "!") {
		t.Fatalf("incremental sync did not update edited item: %q", m.items[1].Text)
	}
}

func TestMapHeaderControlsEmptyStateAndArrowNav(t *testing.T) {
	// 1. Blank document shows clean empty hint and no "空白段落".
	blankEd := New("")
	blankMap := &MindMap{}
	btt := ui.NewTester(func(c *ui.Context) {
		blankMap.View(c, blankEd, Options{Theme: c.Theme(), DocTitle: "日记"}, 480, nil, false, func(bool) {}, "", nil)
	}, 480, 420)
	btt.Frame()
	if len(blankMap.items) != 0 {
		t.Fatalf("expected 0 items on blank doc, got %d", len(blankMap.items))
	}
	if btt.HasText("空白段落") {
		t.Fatal("blank document showed empty paragraph node")
	}
	if !btt.HasText("输入标题、列表或段落，实时生成思维导图") {
		t.Fatal("missing blank-doc empty hint")
	}
	if !btt.HasText("日记") {
		t.Fatal("missing fallback document title on blank root")
	}

	// 2. Empty paragraphs and horizontal rules are skipped; header buttons and arrow nav work.
	ed := New("# Root\n\n## Alpha\n\n- Leaf A\n\n---\n\n\n\n## Beta\n\nParagraph B")
	// Insert an empty paragraph inside Alpha to verify ownerMap fallback.
	ed.Engine.Edit(func() {
		ed.Doc.Blocks = append(ed.Doc.Blocks[:3], append([]*document.Block{document.NewBlock("paragraph", "")}, ed.Doc.Blocks[3:]...)...)
	})
	ed.sync()
	emptyID := ed.Doc.Blocks[3].ID

	m := &MindMap{}
	closed := false
	tt := ui.NewTester(func(c *ui.Context) {
		m.View(c, ed, Options{Theme: c.Theme()}, 560, nil, false, func(bool) {}, "关闭", func() { closed = true })
	}, 560, 480)
	tt.Frame()

	for _, it := range m.items {
		if it.ID == emptyID || it.Text == "分隔线" || it.Text == "空白段落" {
			t.Fatalf("skipped block leaked into map: %+v", it)
		}
	}
	// Caret inside skipped empty paragraph selects Alpha (preceding visible item).
	ed.Engine.FocusBlock(emptyID)
	tt.Frame()
	if m.selected != ed.Doc.Blocks[2].ID {
		t.Fatalf("caret in skipped block selected %q; want Alpha %q", m.selected, ed.Doc.Blocks[2].ID)
	}

	// Zoom in then click 100% reset button.
	if err := tt.Click("放大"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.zoom <= 1.05 {
		t.Fatalf("zoom in failed: %v", m.zoom)
	}
	if err := tt.Click("重置缩放"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.zoom != 1 {
		t.Fatalf("zoom reset got %v; want 1", m.zoom)
	}

	// Collapse branches and expand all from header.
	if err := tt.Click("折叠分支"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !m.HasFolded() {
		t.Fatal("折叠分支 did not fold")
	}
	if err := tt.Click("全部展开"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.HasFolded() {
		t.Fatal("全部展开 did not unfold")
	}

	// Arrow navigation from Alpha down to Beta, then right/left.
	r, ok := tt.Find("Alpha")
	if !ok {
		t.Fatal("missing Alpha")
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	// Focus Alpha button explicitly to drive arrow navigation inside the map.
	m.focusID = ed.Doc.Blocks[1].ID
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	betaID := ed.Doc.Blocks[5].ID
	if m.selected != betaID {
		t.Fatalf("ArrowDown from Alpha selected %q; want Beta %q", m.selected, betaID)
	}
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if m.selected != ed.Doc.Blocks[1].ID {
		t.Fatalf("ArrowUp back to Alpha got %q", m.selected)
	}
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	if m.selected != ed.Doc.Blocks[2].ID {
		t.Fatalf("ArrowRight into Leaf A got %q; want %q", m.selected, ed.Doc.Blocks[2].ID)
	}
	tt.Key(0, ui.KeyLeft)
	tt.Frame()
	if m.selected != ed.Doc.Blocks[1].ID {
		t.Fatalf("ArrowLeft back to Alpha got %q", m.selected)
	}

	// Close button callback fires.
	if err := tt.Click("关闭思维导图"); err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("close button did not invoke callback")
	}
}

func BenchmarkLargeDocTypingWithMindMap(b *testing.B) {
	var sb strings.Builder
	sb.WriteString("# Large Benchmark Doc\n\n")
	for sec := 0; sec < 150; sec++ {
		fmt.Fprintf(&sb, "## Section %d\n\n", sec)
		for j := 0; j < 9; j++ {
			fmt.Fprintf(&sb, "- Item %d.%d short note\n", sec, j)
		}
		sb.WriteString("\n")
	}
	ed := New(sb.String())
	m := &MindMap{}
	tt := ui.NewTester(func(c *ui.Context) {
		m.View(c, ed, Options{Theme: c.Theme()}, 600, nil, false, func(bool) {}, "", nil)
	}, 600, 520)
	tt.Frame()
	leaf, _ := ed.Engine.LeafByID(ed.Doc.Blocks[2].ID)
	ed.SetSelection(leaf.End, leaf.End)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ed.ReplaceText(nil, "a")
		tt.Frame()
	}
}
