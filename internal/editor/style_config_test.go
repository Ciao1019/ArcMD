package editor

import (
	"image"
	"io"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/egoist/mygo/ui"
)

func TestPageStyleTokensComparisonAndTypography(t *testing.T) {
	theme := ui.LightTheme()
	base := pageOf(Page{}, theme)
	clone := clonePage(base)
	if !samePage(base, clone) {
		t.Fatal("cloned base Page did not compare equal")
	}

	checks := []func(*Page){
		func(p *Page) { p.BoldWeight = 700 },
		func(p *Page) { p.HeadingWeight = 700 },
		func(p *Page) { p.TableAlign = "center" },
		func(p *Page) { p.TableHeaderAlign = "right" },
		func(p *Page) { p.TableHeaderWeight = 700 },
		func(p *Page) { p.HeadingText = ui.RGB(20, 40, 120) },
		func(p *Page) { p.BoldText = ui.RGB(180, 30, 40) },
		func(p *Page) { p.TableHeaderText = ui.RGB(10, 90, 60) },
		func(p *Page) { p.TableStripe = ui.RGB(240, 244, 250) },
		func(p *Page) { p.HighlightDefault = ui.RGB(255, 224, 102) },
		func(p *Page) { p.InlineTexFill = ui.RGB(230, 243, 255) },
		func(p *Page) { p.TexBlockFill = ui.RGB(240, 248, 255) },
	}
	for i, mutate := range checks {
		p := clonePage(base)
		mutate(&p)
		if samePage(base, p) {
			t.Fatalf("mutation %d was ignored by samePage", i)
		}
		if !samePage(p, clonePage(p)) {
			t.Fatalf("mutation %d lost by clonePage", i)
		}
	}

	// Verify HeadingWeight, BoldWeight, HeadingText, BoldText, and explicit TextColor precedence.
	headingColor := ui.RGB(18, 52, 86)
	boldColor := ui.RGB(190, 40, 20)
	explicitRed := ui.RGB(216, 57, 49)

	doc := ImportMarkdown("## 章节标题\n\n普通 **加粗** 与 <span data-color=\"red\">**红色加粗**</span>")
	ed := FromDocument(doc)
	pg := Page{
		HeadingWeight: 750,
		BoldWeight:    800,
		HeadingText:   headingColor,
		BoldText:      boldColor,
		TextColors:    map[string]ui.Color{"red": explicitRed},
	}
	ed.layout(Options{Width: 900, Theme: theme, Page: pg})

	// Check heading block metrics & span colors.
	hBlock := ed.Doc.Blocks[0]
	hm := ed.blockMetrics(hBlock, 740, false)
	if hm.font.Weight != 750 {
		t.Fatalf("heading font.Weight = %d, want 750", hm.font.Weight)
	}
	hSpans := ed.blockSpans(hBlock, hm.font, false, theme)
	if len(hSpans) == 0 || hSpans[0].Color != headingColor {
		t.Fatalf("heading span color = %+v, want %+v", hSpans, headingColor)
	}

	// Check bold run weight/color and explicit run color override.
	pBlock := ed.Doc.Blocks[1]
	pm := ed.blockMetrics(pBlock, 740, false)
	pSpans := ed.blockSpans(pBlock, pm.font, false, theme)
	var sawBoldCustom, sawExplicitRed bool
	for _, sp := range pSpans {
		switch strings.TrimSpace(sp.Text) {
		case "加粗":
			if sp.Weight != 800 || sp.Color != boldColor {
				t.Fatalf("bold span = (weight=%d, color=%+v), want (800, %+v)", sp.Weight, sp.Color, boldColor)
			}
			sawBoldCustom = true
		case "红色加粗":
			if sp.Weight != 800 || sp.Color != explicitRed {
				t.Fatalf("explicit colored bold span = (weight=%d, color=%+v), want (800, %+v)", sp.Weight, sp.Color, explicitRed)
			}
			sawExplicitRed = true
		}
	}
	if !sawBoldCustom || !sawExplicitRed {
		t.Fatalf("missing expected spans in %+v", pSpans)
	}
}

func TestTableAlignHeaderStyleStripeAndHighlightDefault(t *testing.T) {
	theme := ui.LightTheme()
	doc := document.New()
	tbl := document.Table(4, 2)
	tbl.Children[0].Children[0].Runs = []document.Run{{Text: "H"}}
	tbl.Children[0].Children[1].Runs = []document.Run{{Text: "ExplicitLeft"}}
	tbl.Children[0].Children[1].Align = "left"
	tbl.Children[1].Children[0].Runs = []document.Run{{Text: "R1"}}
	tbl.Children[2].Children[0].Runs = []document.Run{{Text: "R2"}}
	tbl.Children[3].Children[0].Runs = []document.Run{{Text: "R3"}}
	tbl.Children[3].Children[1].Background = "light-blue"

	markPara := ImportMarkdown("<mark>标记文本</mark>").Blocks[0]
	hlPara := document.NewBlock("paragraph", "")
	hlPara.Runs = []document.Run{
		{Text: "默认荧光", Style: document.Style{Annotation: "highlight"}},
		{Text: "绿色荧光", Style: document.Style{Annotation: "highlight", AnnotationColor: "green"}},
	}
	doc.Blocks = []*document.Block{tbl, markPara, hlPara}

	ed := FromDocument(doc)
	// First layout with default left table alignment.
	ed.layout(Options{Width: 900, Theme: theme})
	leftHdrX := ed.rows[0].layout.Caret(0).X
	leftDataX := ed.rows[2].layout.Caret(0).X

	hdrColor := ui.RGB(35, 85, 165)
	stripeColor := ui.RGB(210, 232, 248)
	hlDefault := ui.RGB(255, 210, 80)
	greenInk := ui.RGB(46, 161, 33)
	cellBlue := ui.RGB(225, 234, 255)

	pg := Page{
		TableAlign:        "right",
		TableHeaderAlign:  "center",
		TableHeaderWeight: 700,
		TableHeaderText:   hdrColor,
		TableStripe:       stripeColor,
		HighlightDefault:  hlDefault,
		TextColors:        map[string]ui.Color{"green": greenInk},
		CellFills:         map[string]ui.Color{"light-blue": cellBlue},
	}
	ed.layout(Options{Width: 900, Theme: theme, Page: pg})

	// Row 0 col 0 ("H") is centered (Caret(0).X > leftHdrX), Row 0 col 1 keeps explicit "left", Row 1 col 0 ("R1") is right-aligned (> centered).
	centerHdrX := ed.rows[0].layout.Caret(0).X
	explicitHdrX := ed.rows[1].layout.Caret(0).X
	rightDataX := ed.rows[2].layout.Caret(0).X
	if !(centerHdrX > leftHdrX+20) {
		t.Fatalf("header cell did not center-align: left=%v center=%v", leftHdrX, centerHdrX)
	}
	if explicitHdrX > 2 {
		t.Fatalf("explicit cell Align='left' was overridden: x=%v", explicitHdrX)
	}
	if !(rightDataX > centerHdrX+20 && rightDataX > leftDataX+40) {
		t.Fatalf("data cell did not right-align: left=%v center=%v right=%v", leftDataX, centerHdrX, rightDataX)
	}

	// Verify TableHeaderWeight & TableHeaderText on row 0 header cell.
	hdrCell := tbl.Children[0].Children[0]
	hdrSpans := ed.blockSpans(hdrCell, ed.blockMetrics(hdrCell, 200, true).font, true, theme)
	if hdrSpans[0].Weight != 700 || hdrSpans[0].Color != hdrColor {
		t.Fatalf("header cell span = (weight=%d, color=%+v), want (700, %+v)", hdrSpans[0].Weight, hdrSpans[0].Color, hdrColor)
	}

	// Verify HighlightDefault on both <mark> and Annotation:"highlight" (with empty color vs explicit green).
	markSpans := ed.blockSpans(markPara, ed.blockMetrics(markPara, 600, false).font, false, theme)
	if markSpans[0].Background != hlDefault {
		t.Fatalf("<mark> fallback background = %+v, want HighlightDefault %+v", markSpans[0].Background, hlDefault)
	}
	if got := ed.annotationColor("highlight", ""); got != hlDefault {
		t.Fatalf("default highlight annotation color = %+v, want %+v", got, hlDefault)
	}
	if got := ed.annotationColor("highlight", "green"); got != greenInk {
		t.Fatalf("explicit green highlight annotation color = %+v, want %+v", got, greenInk)
	}

	// Render to an offscreen RGBA image and sample table cell backgrounds for TableStripe!
	renderImg := func() *image.RGBA {
		return ui.Render(func(c *ui.Context) {
			View(c, ed, Options{Width: 900, Theme: theme, Page: pg})
		}, 900, 600, 1)
	}
	img := renderImg()
	sampleCell := func(rIdx int) [3]uint8 {
		rc := ed.rows[rIdx].rect
		// Sample near bottom-left of cell interior, clear of right-aligned text.
		c := img.RGBAAt(int(rc.X+6), int(rc.Y+rc.H-6))
		return [3]uint8{c.R, c.G, c.B}
	}
	// Row indices in ed.rows: 0=(0,0), 1=(0,1), 2=(1,0), 3=(1,1), 4=(2,0), 5=(2,1), 6=(3,0), 7=(3,1 explicit light-blue).
	r1Col0 := sampleCell(2)
	r2Col0 := sampleCell(4)
	r3Col0 := sampleCell(6)
	r3Col1Explicit := sampleCell(7)
	wantStripe := [3]uint8{stripeColor.R, stripeColor.G, stripeColor.B}
	wantExplicit := [3]uint8{cellBlue.R, cellBlue.G, cellBlue.B}
	if r1Col0 != wantStripe || r3Col0 != wantStripe {
		t.Fatalf("odd data rows did not paint TableStripe: row1=%v row3=%v want=%v", r1Col0, r3Col0, wantStripe)
	}
	if r2Col0 == wantStripe {
		t.Fatalf("even data row 2 painted TableStripe: %v", r2Col0)
	}
	if r3Col1Explicit != wantExplicit {
		t.Fatalf("explicit cell Background overridden by TableStripe: got %v, want %v", r3Col1Explicit, wantExplicit)
	}

	// Selecting row 1 suppresses TableStripe so CellSelected paints cleanly without stripe tint underneath.
	ed.SelectTableLine(tbl.ID, true, 1)
	imgSel := renderImg()
	rc1 := ed.rows[2].rect
	selPixel := imgSel.RGBAAt(int(rc1.X+6), int(rc1.Y+rc1.H-6))
	if [3]uint8{selPixel.R, selPixel.G, selPixel.B} == wantStripe {
		t.Fatalf("selected row still showed raw TableStripe color: %+v", selPixel)
	}
	ed.tableSel = tableSelection{}

	// ExportHTMLPage & ExportVectorPage include style tokens cleanly.
	htmlTheme := HTMLTheme{
		Light: HTMLStyle{Theme: theme, Page: pageOf(pg, theme)},
		Dark:  HTMLStyle{Theme: ui.DarkTheme(), Page: pageOf(pg, ui.DarkTheme())},
	}
	htmlPage := ExportHTMLPage(ed.Doc, htmlTheme)
	if !strings.Contains(htmlPage, "text-align:center") || !strings.Contains(htmlPage, "text-align:right") {
		t.Fatalf("ExportHTMLPage missing table alignment styles:\n%s", htmlPage)
	}
	pw := pdf.NewWriter(io.Discard, "test")
	vp := pw.NewVectorPage(595, 842)
	ed.ExportVectorPage(vp, Options{Width: 794, Theme: theme, Page: pg}, 0, 1000, 0, 1, []float32{0}, 794, 1123, 48)
}

func TestQuickActionsAndCustomizeToolbarMenu(t *testing.T) {
	// 1. ReadOnly mode: all quick actions are strict no-ops.
	ro := New("初始段落")
	ro.SetReadOnly(true)
	v0 := ro.Version()
	ro.InsertTableBlock(3, 3)
	ro.InsertCodeBlock()
	ro.OpenLinkPopover()
	ro.InsertMathBlock()
	ro.InsertRuleBlock()
	ro.InsertColumnsBlock()
	ro.InsertToggleBlock()
	ro.InsertCalloutBlock()
	ro.SetBlockKind("heading", 2)
	ro.ToggleQuickAnnotation()
	if ro.Version() != v0 || ro.linkOpen || ro.Doc.Blocks[0].Kind != "paragraph" {
		t.Fatalf("ReadOnly quick actions mutated editor: version=%d->%d blocks=%+v", v0, ro.Version(), ro.Doc.Blocks)
	}

	// 2. Single-undo-step verification for each quick action!
	assertSingleUndo := func(name string, initMD string, act func(*Editor), verify func(*Editor)) {
		t.Helper()
		ed := New(initMD)
		before := ExportMarkdown(ed.Doc)
		vBefore := ed.Version()
		act(ed)
		if !ed.RequestFocus || !ed.reveal {
			t.Fatalf("%s did not set RequestFocus=%v and reveal=%v", name, ed.RequestFocus, ed.reveal)
		}
		verify(ed)
		if ed.Version() != vBefore+1 {
			t.Fatalf("%s recorded %d undo steps, want 1", name, ed.Version()-vBefore)
		}
		ed.Undo()
		if got := ExportMarkdown(ed.Doc); got != before {
			t.Fatalf("%s single Undo() = %q, want %q", name, got, before)
		}
	}

	assertSingleUndo("InsertTableBlock", "", func(e *Editor) {
		e.InsertTableBlock(2, 4)
	}, func(e *Editor) {
		if b := e.Doc.Blocks[0]; b.Kind != "table" || len(b.Children) != 2 || len(b.Children[0].Children) != 4 {
			t.Fatalf("unexpected table: %+v", b)
		}
	})

	assertSingleUndo("InsertCodeBlock", "", func(e *Editor) {
		e.InsertCodeBlock()
	}, func(e *Editor) {
		if e.Doc.Blocks[0].Kind != "code" {
			t.Fatalf("expected code block, got %q", e.Doc.Blocks[0].Kind)
		}
	})

	assertSingleUndo("InsertMathBlock", "", func(e *Editor) {
		e.InsertMathBlock()
	}, func(e *Editor) {
		if e.Doc.Blocks[0].Kind != "math" {
			t.Fatalf("expected math block, got %q", e.Doc.Blocks[0].Kind)
		}
	})

	assertSingleUndo("InsertRuleBlock", "", func(e *Editor) {
		e.InsertRuleBlock()
	}, func(e *Editor) {
		if e.Doc.Blocks[0].Kind != "rule" {
			t.Fatalf("expected rule block, got %q", e.Doc.Blocks[0].Kind)
		}
	})

	assertSingleUndo("InsertColumnsBlock", "", func(e *Editor) {
		e.InsertColumnsBlock()
	}, func(e *Editor) {
		if e.Doc.Blocks[0].Kind != "columns" || len(e.Doc.Blocks[0].Children) != 2 {
			t.Fatalf("expected 2-column block, got %+v", e.Doc.Blocks[0])
		}
	})

	assertSingleUndo("InsertToggleBlock", "# 标题", func(e *Editor) {
		e.InsertToggleBlock()
	}, func(e *Editor) {
		if len(e.Doc.Blocks) < 3 || e.Doc.Blocks[1].Kind != "toggle" || e.Doc.Blocks[2].Indent != 1 {
			t.Fatalf("expected open toggle with indented child below H1, got %+v", e.Doc.Blocks)
		}
	})

	assertSingleUndo("InsertCalloutBlock", "提示内容", func(e *Editor) {
		e.InsertCalloutBlock()
	}, func(e *Editor) {
		if e.Doc.Blocks[0].Kind != "callout" || e.Doc.Blocks[0].Text() != "提示内容" {
			t.Fatalf("expected callout keeping text, got %+v", e.Doc.Blocks[0])
		}
	})

	assertSingleUndo("SetBlockKind", "待办事项", func(e *Editor) {
		e.SetBlockKind("task", 0)
	}, func(e *Editor) {
		if e.Doc.Blocks[0].Kind != "task" {
			t.Fatalf("expected task block, got %q", e.Doc.Blocks[0].Kind)
		}
	})

	// 3. OpenLinkPopover, ToggleQuickAnnotation, and OnCustomizeToolbar in ui.Tester!
	ed := New("点击这里访问")
	ed.RequestFocus = true
	customizeCalled := false
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		ui.Column(c).Fill().Children(func() {
			ed.Corner(c)
			View(c, ed, Options{
				Width:              w,
				Theme:              c.Theme(),
				OnCustomizeToolbar: func() { customizeCalled = true },
			})
		})
	}, 960, 600)
	tt.Frame()

	// Select "这里" and open link popover via OpenLinkPopover().
	ed.SetSelection(2, 4)
	ed.OpenLinkPopover()
	tt.Frame()
	if !ed.linkOpen || !tt.HasText("编辑链接") {
		t.Fatal("OpenLinkPopover did not open link editor")
	}
	ed.linkURL = "https://example.com"
	tt.Frame()
	if err := tt.Click("应用链接"); err != nil {
		t.Fatalf("click 应用链接: %v", err)
	}
	tt.Frame()
	if ed.Doc.Blocks[0].Runs[1].Style.Link != "https://example.com" {
		t.Fatalf("link was not applied: %+v", ed.Doc.Blocks[0].Runs)
	}

	// ToggleQuickAnnotation on selection applies wavy, second call removes it, and collapsed caret on annotation opens markBar.
	ed.SetSelection(0, 2)
	ed.ToggleQuickAnnotation()
	tt.Frame()
	if ed.Doc.Blocks[0].Runs[0].Style.Annotation != "wavy" {
		t.Fatalf("expected wavy annotation on selection, got %+v", ed.Doc.Blocks[0].Runs[0].Style)
	}
	// Collapsed caret inside annotated span opens mark popover.
	ed.SetSelection(1, 1)
	ed.ToggleQuickAnnotation()
	tt.Frame()
	if !ed.markPop.open || !tt.HasText("移除标注") {
		t.Fatalf("ToggleQuickAnnotation at annotated caret did not open mark popover: open=%v", ed.markPop.open)
	}
	// Selecting annotated text and calling ToggleQuickAnnotation removes annotation.
	ed.SetSelection(0, 2)
	ed.ToggleQuickAnnotation()
	tt.Frame()
	if ed.Doc.Blocks[0].Runs[0].Style.Annotation != "" {
		t.Fatalf("second ToggleQuickAnnotation did not clear annotation: %+v", ed.Doc.Blocks[0].Runs[0].Style)
	}

	// More menu ("更多格式") exposes "自定义工具栏…" and invokes OnCustomizeToolbar.
	if err := tt.Click("更多格式"); err != nil {
		t.Fatalf("click 更多格式: %v", err)
	}
	if err := tt.ChooseMenuItem("自定义工具栏…"); err != nil {
		t.Fatalf("choose 自定义工具栏… from menu %v: %v", tt.Menu(), err)
	}
	tt.Frame()
	if !customizeCalled {
		t.Fatal("OnCustomizeToolbar callback was not invoked")
	}
}
