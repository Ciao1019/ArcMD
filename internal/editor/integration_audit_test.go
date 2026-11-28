package editor

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

func TestPlainTextCopyNeverLeaksInlineImagePlaceholder(t *testing.T) {
	md := strings.Join([]string{
		"Build <img src=\"ci.svg\" alt=\"CI-OK\" width=\"60\"> and <img src=\"icon.svg\" alt=\"\" width=\"16\"> done",
		"",
		"- Item <img src=\"tag.svg\" alt=\"v2\" width=\"40\">",
		"",
		"| Col A | Col B |",
		"| --- | --- |",
		"| cell <img src=\"a.svg\" alt=\"A1\" width=\"20\"> | <img src=\"b.svg\" alt=\"\" width=\"20\">only |",
		"",
		"---",
	}, "\n")

	copyPlainOpt := false
	e := New(md)
	tt := ui.NewTester(func(c *ui.Context) {
		View(c, e, Options{Width: 720, Theme: c.Theme(), CopyPlainText: copyPlainOpt})
	}, 760, 600)
	focus(e, tt)

	// 1. Single-block explicit CopyPlainText expands Alt and drops empty-alt \ufffc.
	leaves := e.Doc.Leaves()
	e.SetSelection(leaves[0].Start, leaves[0].End)
	tt.Frame()
	e.CopyPlainText()
	if got, want := tt.Clipboard(), "Build CI-OK and  done"; got != want || strings.Contains(got, document.InlineImagePlaceholder) {
		t.Fatalf("single-block CopyPlainText = %q; want %q", got, want)
	}

	// 2. Single table cell normal Cmd+C (CopyPlainText=false) also strips \ufffc.
	e.SetSelection(leaves[4].Start, leaves[4].End)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyC)
	if got, want := tt.Clipboard(), "cell A1"; got != want || strings.Contains(got, document.InlineImagePlaceholder) {
		t.Fatalf("single-cell Cmd+C = %q; want %q", got, want)
	}

	// 3. Multi-block selection with CopyPlainText=true across paragraph, list, table, and rule.
	copyPlainOpt = true
	e.SetSelection(0, e.Len())
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyC)
	gotAll := tt.Clipboard()
	if strings.Contains(gotAll, document.InlineImagePlaceholder) {
		t.Fatalf("multi-block plain copy leaked \\ufffc: %q", gotAll)
	}
	for _, wantSub := range []string{"Build CI-OK and  done", "Item v2", "Col A\tCol B\ncell A1\tonly"} {
		if !strings.Contains(gotAll, wantSub) {
			t.Fatalf("multi-block plain copy missing %q in %q", wantSub, gotAll)
		}
	}

	// 4. Pasting rich clipboard data with inline images into a code block never inserts \ufffc.
	clipData := tt.ClipboardData()
	codeEd, codeTT := newTester("```go\n\n```")
	focus(codeEd, codeTT)
	codeLeaf := codeEd.Doc.Leaves()[0]
	codeEd.SetSelection(codeLeaf.Start, codeLeaf.Start)
	codeTT.SetClipboardData(clipData)
	codeTT.Key(ui.Cmd, ui.KeyV)
	if strings.Contains(codeEd.Doc.Blocks[0].Text(), document.InlineImagePlaceholder) {
		t.Fatalf("paste into code block leaked \\ufffc: %q", codeEd.Doc.Blocks[0].Text())
	}

	// 5. Single rule block CopyPlainText produces empty text consistently with multi-block.
	ruleLeaf := leaves[len(leaves)-1]
	e.SetSelection(ruleLeaf.Start, ruleLeaf.End)
	tt.Frame()
	if plain := e.selectionPlainText(ui.TextInputRange{Start: ruleLeaf.Start, End: ruleLeaf.End}, e.Engine.Fragment(), ""); plain != "" {
		t.Fatalf("rule selectionPlainText = %q; want empty", plain)
	}
}

func TestBlockBackgroundWithSingleInlineImageMarkdownRoundTrip(t *testing.T) {
	cases := []struct {
		name      string
		md        string
		wantBg    string
		wantAlign string
	}{
		{
			name:   "tinted paragraph with single sized img",
			md:     `<p style="background-color:#E1EAFF" data-bg="blue"><img src="badge.svg" alt="CI" width="88"></p>`,
			wantBg: "blue",
		},
		{
			name:      "centered tinted div with picture",
			md:        `<div align="center" style="background-color:#FEFFF0" data-bg="light-yellow"><picture><source media="(prefers-color-scheme: dark)" srcset="dark.svg"><img src="light.svg" alt="Logo" width="120"></picture></div>`,
			wantBg:    "light-yellow",
			wantAlign: "center",
		},
		{
			name:      "outer tinted div with child p containing single img",
			md:        "<div align=\"right\" data-bg=\"green\">\n  <p><img src=\"ok.png\" alt=\"OK\" width=\"48\"></p>\n</div>",
			wantBg:    "green",
			wantAlign: "right",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := ImportMarkdown(tc.md)
			if len(doc.Blocks) != 1 {
				t.Fatalf("block count = %d; want 1", len(doc.Blocks))
			}
			b := doc.Blocks[0]
			if b.Kind != "paragraph" || b.Background != tc.wantBg || b.Align != tc.wantAlign {
				t.Fatalf("imported block = kind=%q bg=%q align=%q; want paragraph bg=%q align=%q",
					b.Kind, b.Background, b.Align, tc.wantBg, tc.wantAlign)
			}
			if len(b.Runs) != 1 || !b.Runs[0].IsImage() {
				t.Fatalf("expected single InlineImageRun, got %+v", b.Runs)
			}

			// Force fresh export by clearing ImportSource and re-importing.
			doc.ImportSource = ""
			exported := ExportMarkdown(doc)
			reloaded := ImportMarkdown(exported)
			if len(reloaded.Blocks) != 1 || reloaded.Blocks[0].Kind != "paragraph" ||
				reloaded.Blocks[0].Background != tc.wantBg || !reloaded.Blocks[0].Runs[0].IsImage() {
				t.Fatalf("round-trip lost background or inline image:\nexported:\n%s\ngot:%+v", exported, reloaded.Blocks[0])
			}
		})
	}
}

func TestNumberedListBackgroundRoundTripPreservesAutoNumbering(t *testing.T) {
	ed := New("1. 第一步\n2. 第二步\n3. 第三步\n")
	ed.Engine.SetBlockBackground(ed.Doc.Blocks[1].ID, "orange")
	md := ExportMarkdown(ed.Doc)

	reloaded := ImportMarkdown(md)
	if len(reloaded.Blocks) != 3 {
		t.Fatalf("reloaded blocks = %d; want 3\nMD:\n%s", len(reloaded.Blocks), md)
	}
	for i, b := range reloaded.Blocks {
		if b.Kind != "numbered" || b.Start != 0 {
			t.Fatalf("block[%d]: kind=%q Start=%d; want numbered Start=0\nMD:\n%s", i, b.Kind, b.Start, md)
		}
	}
	if reloaded.Blocks[1].Background != "orange" {
		t.Fatalf("block[1].Background = %q; want orange", reloaded.Blocks[1].Background)
	}

	// Inserting a new item before item 0 must renumber the whole list cleanly to 1, 2, 3, 4.
	ed2 := New(md)
	ed2.SetSelection(0, 0)
	ed2.Engine.Replace(0, 0, "零\n")
	nums := document.ListNumbers(ed2.Doc.Blocks)
	wantNums := []int{1, 2, 3, 4}
	for i, w := range wantNums {
		if i >= len(nums) || nums[i] != w {
			t.Fatalf("list numbers after prepend = %v; want %v", nums, wantNums)
		}
	}
}

func TestImportHTMLDetailsBackgroundScopingAndEmbedImages(t *testing.T) {
	rawHTML := `<style>.hl-red { color: #d83931; }</style>` +
		`<details open style="background-color:#FEEAD2" data-bg="orange">` +
		`<summary>折叠标题</summary><p>内部普通正文</p></details>` +
		`<div class="arcmd-columns"><div class="arcmd-column"><table><tr><td class="hl-red">左栏红字</td></tr></table></div>` +
		`<div class="arcmd-column"><p data-bg="blue">右栏</p></div></div>`

	doc := ImportHTML(rawHTML)
	if len(doc.Blocks) < 3 {
		t.Fatalf("ImportHTML blocks = %d; want >= 3", len(doc.Blocks))
	}
	if doc.Blocks[0].Background != "orange" {
		t.Fatalf("summary block background = %q; want orange", doc.Blocks[0].Background)
	}
	if doc.Blocks[1].Background != "" {
		t.Fatalf("details body paragraph inherited background %q; want empty", doc.Blocks[1].Background)
	}
	for bi, b := range doc.Blocks[:2] {
		for _, r := range b.Runs {
			if r.Style.Highlight != "" {
				t.Fatalf("block[%d] leaked inline Highlight=%q", bi, r.Style.Highlight)
			}
		}
	}
	cols := doc.Blocks[2]
	leftCell := cols.Children[0].Children[0].Children[0].Children[0]
	if cols.Kind != "columns" || leftCell.Runs[0].Style.Color != "red" || cols.Children[1].Children[0].Background != "blue" {
		t.Fatalf("columnsOfHTML dropped stylesheet classes or column bg: runs=%+v rightBg=%q", leftCell.Runs, cols.Children[1].Children[0].Background)
	}

	embedded := ImportHTMLDocument(`<details open><summary style="background-color:#FEEAD2" data-bg="orange">章节摘要</summary><p>正文</p></details>`, HTMLImportOptions{EmbedImages: true}).Doc
	if len(embedded.Blocks) < 2 || embedded.Blocks[0].Background != "orange" || embedded.Blocks[1].Background != "" {
		t.Fatalf("EmbedImages lost <summary> background: %+v", embedded.Blocks)
	}
}

func TestVirtualizedHeadingAndFootnoteBlockBackgroundHeight(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 160; i++ {
		fmt.Fprintf(&sb, "Paragraph %d filler line to trigger viewport virtualization.\n\n", i)
	}
	sb.WriteString("<h2 style=\"background-color:#E1EAFF\" data-bg=\"blue\">Offscreen Tinted Heading</h2>\n\n")
	sb.WriteString("Trailing paragraph with ref[^fn1].\n\n")
	sb.WriteString("<div style=\"background-color:#EEeff0\" data-bg=\"gray\">\n\n[^fn1]: Offscreen tinted footnote body.\n\n</div>\n")

	e := New(sb.String())
	tt := ui.NewTester(func(c *ui.Context) {
		View(c, e, Options{Width: 760, Theme: c.Theme()})
	}, 800, 500)
	tt.Frame()

	// Scroll to the bottom so offscreen heading and footnote materialize via virtualize.go.
	e.Scroll.Y = 1e6
	tt.Frame()
	tt.Frame()

	for i := range e.decorations {
		d := *e.indexedDecoration(i)
		if d.kind != "block-bg" {
			continue
		}
		ri, ok := e.rowIDs[d.id]
		if !ok {
			t.Fatalf("missing row for decoration %q", d.id)
		}
		r := *e.indexedRow(ri)
		if d.rect.Y+d.rect.H > r.rect.Y+r.rect.H+0.5 {
			t.Fatalf("%s block-bg rect (%+v) extends past row bounds (%+v)", r.leaf.Block.Kind, d.rect, r.rect)
		}
		if r.leaf.Block.Kind == "heading" && d.rect.Y <= r.rect.Y {
			t.Fatalf("virtualized heading block-bg did not skip top margin: dec=%+v row=%+v", d.rect, r.rect)
		}
	}
}

func TestSVGStableCachingMalformedDimensionsAndPDFAspectRatio(t *testing.T) {
	validSVG := ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="3000" height="300" viewBox="0 0 3000 300"><rect width="3000" height="300" fill="currentColor"/></svg>`))
	infSVG, _ := ui.ParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1e40" height="1e40"><rect width="10" height="10"/></svg>`))
	zeroSVG, _ := ui.ParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="0" height="0" viewBox="0 0 0 0"><rect width="0" height="0"/></svg>`))

	if infRes := SVGImageResource(infSVG, true); infRes.HasVisual() || infRes.Problem != ImageUnsupported {
		t.Fatalf("expected +Inf SVG rejected as ImageUnsupported, got %+v", infRes)
	}
	if zeroRes := SVGImageResource(zeroSVG, true); zeroRes.HasVisual() || zeroRes.Problem != ImageUnsupported {
		t.Fatalf("expected 0x0 SVG rejected as ImageUnsupported, got %+v", zeroRes)
	}

	e := New("Inline <img src=\"wide.svg\" width=\"120\"> <img src=\"inf.svg\" alt=\"InfAlt\" width=\"60\"> <img src=\"zero.svg\" alt=\"ZeroAlt\" width=\"60\">")
	loadImage := func(src string) ImageResource {
		switch src {
		case "wide.svg":
			return ImageResource{SVG: validSVG, Width: 3000, Height: 300, Local: true}
		case "inf.svg":
			return ImageResource{SVG: infSVG, Local: true}
		case "zero.svg":
			return ImageResource{SVG: zeroSVG, Local: true}
		}
		return ImageResource{Problem: ImageMissing}
	}
	dark := false
	tt := ui.NewTester(func(c *ui.Context) {
		th := ui.LightTheme()
		if dark {
			th = ui.DarkTheme()
		}
		View(c, e, Options{Width: 720, Theme: th, LoadImage: loadImage})
	}, 760, 480)
	tt.Frame()
	lightPtr1 := e.images["wide.svg"].SVGFor(false)
	tt.Frame()
	lightPtr2 := e.images["wide.svg"].SVGFor(false)
	dark = true
	tt.Frame()
	darkPtr1 := e.images["wide.svg"].SVGFor(true)
	tt.Frame()
	darkPtr2 := e.images["wide.svg"].SVGFor(true)

	if lightPtr1 == nil || lightPtr1 != lightPtr2 {
		t.Fatalf("light SVG pointer changed across frames: %p vs %p", lightPtr1, lightPtr2)
	}
	if darkPtr1 == nil || darkPtr1 != darkPtr2 || darkPtr1 == lightPtr1 {
		t.Fatalf("dark SVG pointer unstable: %p vs %p (light=%p)", darkPtr1, darkPtr2, lightPtr1)
	}
	if e.images["inf.svg"].HasVisual() || e.images["zero.svg"].HasVisual() {
		t.Fatal("malformed inf/zero SVGs must not report HasVisual() == true")
	}

	// rasterizeSVGForPDF preserves 10:1 aspect ratio when clamping to 4096px max edge.
	wideRes := SVGImageResource(validSVG, true)
	img := rasterizeSVGForPDF(wideRes, 3000, 300, ui.LightTheme())
	if img == nil || img.Bounds().Dx() != 4096 || img.Bounds().Dy() != 410 {
		t.Fatalf("unexpected PDF SVG raster bounds: %v; want 4096x410", img.Bounds())
	}
	if rasterizeSVGForPDF(ImageResource{SVG: infSVG}, 100, 100, ui.LightTheme()) != nil {
		t.Fatal("expected nil PDF raster for +Inf SVG")
	}
}

func TestDangerousURLSchemesBlockedAndBarePercentRelativeImagePreserved(t *testing.T) {
	// 1. Bare '%' in local relative image path survives edit + ExportMarkdown.
	src := "<p>Badge <img src=\"assets/100%_pass.svg\" alt=\"100%\" width=\"90\"></p>\n"
	ed := New(src)
	leaf := ed.Doc.Leaves()[0]
	ed.Engine.Replace(leaf.End, leaf.End, "!")
	exported := ExportMarkdown(ed.Doc)
	if !strings.Contains(exported, `src="assets/100%_pass.svg"`) {
		t.Fatalf("bare-percent relative image was dropped on ExportMarkdown:\n%s", exported)
	}

	// 2. Dangerous schemes blocked across Markdown/HTML import, HTML export, click/OpenLink, and PDF /URI.
	maliciousMD := strings.Join([]string{
		`[xss](javascript:alert(1))`,
		`[vbs](vbscript:msgbox(1))`,
		`[local](file:///etc/passwd)`,
		`<a href="&#10;javascript:alert(2)"><img src="ok.svg" width="40"></a>`,
		`<img src="file:///etc/passwd" width="40">`,
	}, "\n\n")
	malDoc := ImportMarkdown(maliciousMD)
	for _, b := range malDoc.Blocks {
		if b.Kind == "image" && strings.HasPrefix(strings.ToLower(b.URL), "file:") {
			t.Fatalf("file: image block was imported: %+v", b)
		}
		for _, r := range b.Runs {
			if r.Style.Link != "" {
				t.Fatalf("dangerous link survived ImportMarkdown: %q", r.Style.Link)
			}
		}
	}

	// Construct an unvalidated in-memory block with dangerous Link & Background to test export defense-in-depth.
	memDoc := document.New()
	badPara := document.NewBlock("paragraph", "")
	badPara.Background = `blue" onclick="alert(1)`
	badPara.Runs = []document.Run{
		{Text: "bad", Style: document.Style{Link: "file:///System/Applications/Calculator.app"}},
		{Text: "js", Style: document.Style{Link: "javascript:alert(1)"}},
		{Text: "good", Style: document.Style{Link: "https://example.com/ok"}},
	}
	memDoc.Blocks = []*document.Block{badPara}

	htmlOut := ExportHTML(memDoc)
	if strings.Contains(htmlOut, "onclick=") || strings.Contains(htmlOut, "file://") || strings.Contains(htmlOut, "javascript:") {
		t.Fatalf("ExportHTML leaked dangerous attribute or scheme:\n%s", htmlOut)
	}
	mdOut := ExportMarkdown(memDoc)
	if strings.Contains(mdOut, "onclick=") || strings.Contains(mdOut, "file://") || strings.Contains(mdOut, "javascript:") {
		t.Fatalf("ExportMarkdown leaked dangerous attribute or scheme:\n%s", mdOut)
	}

	pdfEd := New("")
	pdfEd.Engine = document.NewEngine(memDoc)
	pdfEd.sync()
	opts := Options{Width: 794, Theme: ui.LightTheme()}
	pdfEd.layout(opts)
	var pdfBuf bytes.Buffer
	pw := pdf.NewWriter(&pdfBuf, "url-safety")
	vp := pw.NewVectorPage(595, 842)
	pdfEd.ExportVectorPage(vp, opts, 0, 1000, 0, 1, []float32{0}, 794, 1123, 48)
	if err := pw.FinishVectorPage(vp); err != nil {
		t.Fatal(err)
	}
	if err := pw.Close(); err != nil {
		t.Fatal(err)
	}
	pdfStr := pdfBuf.String()
	if strings.Contains(pdfStr, "javascript:") || strings.Contains(pdfStr, "file://") || !strings.Contains(pdfStr, "https://example.com/ok") {
		t.Fatalf("unexpected PDF /URI annotations in output")
	}
}

func TestInlineImageCaretHalvesMixedLinkPopoverOutlineAndRelocateIME(t *testing.T) {
	svg := ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="80" height="24" viewBox="0 0 80 24"><rect width="80" height="24" fill="#2563eb"/></svg>`))
	md := strings.Join([]string{
		"# <img src=\"logo.svg\" alt=\"ArcMD\" width=\"40\"> Release $v_1$",
		"",
		"<img src=\"plain.svg\" alt=\"Plain\" width=\"80\"> tail",
		"",
		"<a href=\"https://old.example\"><img src=\"logo.svg\" alt=\"Icon\" width=\"24\"> Docs</a>",
	}, "\n")
	ed := New(md)
	var opened []string
	tt := ui.NewTester(func(c *ui.Context) {
		View(c, ed, Options{
			Width:      920,
			Theme:      c.Theme(),
			Outline:    true,
			SetOutline: func(bool) {},
			OpenLink:   func(u string) { opened = append(opened, u) },
			LoadImage: func(string) ImageResource {
				return SVGImageResource(svg, true)
			},
		})
	}, 960, 560)
	focus(ed, tt)

	// 1. Outline heading strips \ufffc into Alt while keeping math rendering non-nil.
	if len(ed.outline) != 1 || strings.Contains(ed.outline[0].text, document.InlineImagePlaceholder) || ed.outline[0].text != "ArcMD Release $v_1$" {
		t.Fatalf("unexpected outline item: %+v", ed.outline)
	}
	if om := ed.outlineMathRow(ed.outline[0], 400); om == nil {
		t.Fatal("expected non-nil outlineMathRow for heading with inline image + math")
	}

	// 2. Clicking left vs right half of non-linked inline image places caret before vs after image.
	rPlain := *ed.indexedRow(1)
	objPlain := rPlain.layout.objects[0]
	leftIdx := rPlain.layout.IndexAt(ui.Point{X: objPlain.rect.X + 2, Y: objPlain.rect.Y + objPlain.rect.H/2})
	rightIdx := rPlain.layout.IndexAt(ui.Point{X: objPlain.rect.X + objPlain.rect.W - 2, Y: objPlain.rect.Y + objPlain.rect.H/2})
	if leftIdx != 0 || rightIdx != 1 {
		t.Fatalf("inline image half-click indices = (%d, %d); want (0, 1)", leftIdx, rightIdx)
	}

	// 3. Mixed image+text link opens URL-only popover (hiding raw \ufffc label) and updates URL cleanly.
	mixedLeaf := ed.Doc.Leaves()[2]
	ed.SetSelection(mixedLeaf.Start+1, mixedLeaf.Start+1)
	ed.EditLink()
	tt.Frame()
	if !ed.linkOpen || !ed.linkHasInlineImage || strings.Contains(ed.linkText, document.InlineImagePlaceholder) {
		t.Fatalf("mixed link popover state: open=%v hasImg=%v text=%q", ed.linkOpen, ed.linkHasInlineImage, ed.linkText)
	}
	ed.linkURL = "https://new.example"
	ed.applyLink(false)
	for _, r := range ed.Doc.Blocks[2].Runs {
		if r.Style.Link != "https://new.example" {
			t.Fatalf("mixed link URL was not updated on all runs: %+v", ed.Doc.Blocks[2].Runs)
		}
	}

	// 4. RelocateImages updates active single-leaf IME composition snapshot runs.
	ed.SetSelection(rPlain.leaf.End, rPlain.leaf.End)
	ed.SetMarkedText(nil, "pin", ui.TextInputRange{Start: 0, End: 3})
	if ed.leafComp == nil {
		t.Fatal("expected active leafComp during IME preedit")
	}
	ed.RelocateImages(map[string]string{"plain.svg": "moved/plain.svg"})
	ed.UnmarkText()
	if got := ed.Doc.Blocks[1].Runs[0].Image.URL; got != "moved/plain.svg" {
		t.Fatalf("after UnmarkText, inline image URL = %q; want moved/plain.svg", got)
	}
	_ = transfer.Text
	_ = opened
}
