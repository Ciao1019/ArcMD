package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/document"
)

func TestIssue94InlineAndHTMLImageImportExport(t *testing.T) {
	// 1. Floated / aligned image followed by a paragraph (with and without blank line).
	for _, tc := range []struct {
		name  string
		md    string
		align string
	}{
		{
			name:  "right-no-blank-line",
			md:    "<img src=\"a.png\" width=\"360\" align=\"right\">\nLorem ipsum dolor sit amet.\n",
			align: "right",
		},
		{
			name:  "left-same-line",
			md:    "<img src=\"a.png\" width=\"360\" align=\"left\"> Lorem ipsum dolor sit amet.\n",
			align: "left",
		},
		{
			name:  "right-with-blank-line",
			md:    "<img src=\"a.png\" width=\"360\" align=\"right\">\n\nLorem ipsum dolor sit amet.\n",
			align: "right",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := ImportMarkdown(tc.md)
			if len(doc.Blocks) != 2 {
				t.Fatalf("got %d blocks: %+v", len(doc.Blocks), doc.Blocks)
			}
			img := doc.Blocks[0]
			if img.Kind != "image" || img.URL != "a.png" || img.ImageWidth != 360 || img.Align != tc.align {
				t.Fatalf("unexpected image block: %+v", img)
			}
			if doc.Blocks[1].Kind != "paragraph" || doc.Blocks[1].Text() != "Lorem ipsum dolor sit amet." {
				t.Fatalf("unexpected trailing paragraph: %+v", doc.Blocks[1])
			}
			if got := ExportMarkdown(doc); got != tc.md {
				t.Fatalf("unedited round-trip mismatch:\ngot:  %q\nwant: %q", got, tc.md)
			}
			// Editing image width preserves explicit align on export.
			edited := doc.Clone()
			edited.Blocks[0].ImageWidth = 320
			out := ExportMarkdown(edited)
			if !strings.Contains(out, `width="320"`) || !strings.Contains(out, `align="`+tc.align+`"`) {
				t.Fatalf("edited aligned image lost attrs: %q", out)
			}
		})
	}

	// 2. Inline image in a sentence preserving width & height.
	t.Run("inline-in-sentence", func(t *testing.T) {
		md := "文字 <img src=\"icon.png\" width=\"20\" height=\"18\" alt=\"ico\"> 文字\n"
		doc := ImportMarkdown(md)
		if len(doc.Blocks) != 1 || doc.Blocks[0].Kind != "paragraph" || len(doc.Blocks[0].Runs) != 3 {
			t.Fatalf("unexpected blocks/runs: %+v", doc.Blocks)
		}
		imgRun := doc.Blocks[0].Runs[1]
		if !imgRun.IsImage() || imgRun.Text != document.InlineImagePlaceholder ||
			imgRun.Image.URL != "icon.png" || imgRun.Image.Alt != "ico" ||
			imgRun.Image.Width != 20 || imgRun.Image.Height != 18 {
			t.Fatalf("unexpected inline image run: %+v", imgRun)
		}
		if got := ExportMarkdown(doc); got != md {
			t.Fatalf("unedited round-trip mismatch: got %q want %q", got, md)
		}
		edited := doc.Clone()
		edited.Blocks[0].Runs[2].Text = " 更多文字"
		exported := ExportMarkdown(edited)
		if !strings.Contains(exported, `<img src="icon.png" alt="ico" width="20" height="18">`) || !strings.Contains(exported, "更多文字") {
			t.Fatalf("edited inline image export failed: %q", exported)
		}
		reimported := ImportMarkdown(exported)
		if len(reimported.Blocks) != 1 || len(reimported.Blocks[0].Runs) != 3 || reimported.Blocks[0].Runs[1].Image != imgRun.Image {
			t.Fatalf("reimport mismatch: %+v", reimported.Blocks)
		}
	})

	// 3. Multiple images on one line + multi-line stacked badges collapsing soft breaks.
	t.Run("multiple-images-one-line", func(t *testing.T) {
		md := "<img src=\"a.png\"> <img src=\"b.png\">\n"
		doc := ImportMarkdown(md)
		if len(doc.Blocks) != 1 || len(doc.Blocks[0].Runs) != 3 ||
			doc.Blocks[0].Runs[0].Image.URL != "a.png" || doc.Blocks[0].Runs[2].Image.URL != "b.png" {
			t.Fatalf("unexpected multi-image paragraph: %+v", doc.Blocks)
		}

		stacked := "<a href=\"https://a.test\"><img src=\"a.svg\" alt=\"A\"></a>\n<a href=\"https://b.test\"><img src=\"b.svg\" alt=\"B\"></a>\n"
		sdoc := ImportMarkdown(stacked)
		if len(sdoc.Blocks) != 1 || len(sdoc.Blocks[0].Runs) != 3 || sdoc.Blocks[0].Runs[1].Text != " " {
			t.Fatalf("stacked badges should join with space, got: %+v", sdoc.Blocks[0].Runs)
		}
		if got := ExportMarkdown(sdoc); got != stacked {
			t.Fatalf("stacked badges round-trip mismatch: got %q want %q", got, stacked)
		}

		// Bare <img> on consecutive lines opens a CommonMark type-7 HTML block.
		bareStacked := "<img src=\"a.png\">\n<img src=\"b.png\">\n"
		bdoc := ImportMarkdown(bareStacked)
		if len(bdoc.Blocks) != 1 || bdoc.Blocks[0].Kind != "paragraph" || len(bdoc.Blocks[0].Runs) != 3 ||
			bdoc.Blocks[0].Runs[0].Image.URL != "a.png" || bdoc.Blocks[0].Runs[1].Text != " " || bdoc.Blocks[0].Runs[2].Image.URL != "b.png" {
			t.Fatalf("consecutive bare <img> lines failed: %+v", bdoc.Blocks)
		}
		if got := ExportMarkdown(bdoc); got != bareStacked {
			t.Fatalf("consecutive bare <img> round-trip mismatch: got %q want %q", got, bareStacked)
		}
	})

	// 4. HTML linked image (single-line, multi-line, and inside <p align="center">).
	t.Run("html-linked-image", func(t *testing.T) {
		md := "<a href=\"https://example.com\"><img src=\"badge.svg\" alt=\"CI\" style=\"width: 120px; height: 24px;\"></a>\n"
		doc := ImportMarkdown(md)
		if len(doc.Blocks) != 1 || len(doc.Blocks[0].Runs) != 1 {
			t.Fatalf("unexpected blocks: %+v", doc.Blocks)
		}
		r := doc.Blocks[0].Runs[0]
		if !r.IsImage() || r.Style.Link != "https://example.com" || r.Image.URL != "badge.svg" ||
			r.Image.Alt != "CI" || r.Image.Width != 120 || r.Image.Height != 24 {
			t.Fatalf("unexpected linked image run: %+v", r)
		}
		htmlOut := ExportHTML(doc)
		if !strings.Contains(htmlOut, `<a href="https://example.com"><img src="badge.svg" alt="CI" width="120" height="24"></a>`) {
			t.Fatalf("ExportHTML unexpected: %s", htmlOut)
		}
	})

	// 5. Markdown linked image [![alt](badge.svg)](https://...).
	t.Run("markdown-linked-image", func(t *testing.T) {
		md := "[![Listed on Awesome MyGo](https://awesomemygo.dev/badge.svg)](https://awesomemygo.dev/apps/arcmd)\n"
		doc := ImportMarkdown(md)
		if len(doc.Blocks) != 1 || doc.Blocks[0].Kind != "paragraph" || len(doc.Blocks[0].Runs) != 1 {
			t.Fatalf("unexpected blocks: %+v", doc.Blocks)
		}
		r := doc.Blocks[0].Runs[0]
		if !r.IsImage() || r.Style.Link != "https://awesomemygo.dev/apps/arcmd" ||
			r.Image.URL != "https://awesomemygo.dev/badge.svg" || r.Image.Alt != "Listed on Awesome MyGo" {
			t.Fatalf("unexpected markdown linked image run: %+v", r)
		}
		if got := ExportMarkdown(doc); got != md {
			t.Fatalf("unedited round-trip mismatch: got %q want %q", got, md)
		}
		edited := doc.Clone()
		edited.Blocks[0].Runs[0].Style.Link = "https://awesomemygo.dev/apps/arcmd-v2"
		if got := ExportMarkdown(edited); got != "[![Listed on Awesome MyGo](https://awesomemygo.dev/badge.svg)](https://awesomemygo.dev/apps/arcmd-v2)\n" {
			t.Fatalf("edited markdown linked image export unexpected: %q", got)
		}
	})

	// 6. <picture> element (standalone, inline, and wrapped in <a>).
	t.Run("picture-element", func(t *testing.T) {
		standalone := "<picture><source srcset=\"a.webp\"><img src=\"a.png\" alt=\"Demo\" width=\"300\"></picture>\n"
		doc := ImportMarkdown(standalone)
		if len(doc.Blocks) != 1 || doc.Blocks[0].Kind != "image" || doc.Blocks[0].URL != "a.png" || doc.Blocks[0].ImageWidth != 300 || doc.Blocks[0].Text() != "Demo" {
			t.Fatalf("standalone <picture> unexpected: %+v", doc.Blocks)
		}
		if got := ExportMarkdown(doc); got != standalone {
			t.Fatalf("standalone <picture> round-trip mismatch: got %q", got)
		}

		inline := "Before <picture><source srcset=\"a.webp\"><img src=\"a.png\" width=\"24\"></picture> after\n"
		idoc := ImportMarkdown(inline)
		if len(idoc.Blocks) != 1 || len(idoc.Blocks[0].Runs) != 3 ||
			!idoc.Blocks[0].Runs[1].IsImage() || idoc.Blocks[0].Runs[1].Image.URL != "a.png" || idoc.Blocks[0].Runs[1].Image.Width != 24 {
			t.Fatalf("inline <picture> unexpected: %+v", idoc.Blocks)
		}

		linkedMultiline := "<a href=\"https://www.star-history.com/\">\n <picture>\n   <source media=\"(prefers-color-scheme: dark)\" srcset=\"dark.svg\" />\n   <img alt=\"Star History\" src=\"light.svg\" />\n </picture>\n</a>\n"
		ldoc := ImportMarkdown(linkedMultiline)
		if len(ldoc.Blocks) != 1 || ldoc.Blocks[0].Kind != "paragraph" || len(ldoc.Blocks[0].Runs) != 1 ||
			!ldoc.Blocks[0].Runs[0].IsImage() || ldoc.Blocks[0].Runs[0].Style.Link != "https://www.star-history.com/" ||
			ldoc.Blocks[0].Runs[0].Image.URL != "light.svg" || ldoc.Blocks[0].Runs[0].Image.Alt != "Star History" {
			t.Fatalf("linked multiline <picture> unexpected: %+v", ldoc.Blocks)
		}
		if got := ExportMarkdown(ldoc); got != linkedMultiline {
			t.Fatalf("linked multiline <picture> round-trip mismatch: got %q", got)
		}
	})
}

func TestRepositoryReadmeImportsWithoutRawHTMLBadges(t *testing.T) {
	for _, name := range []string{"README.md", "README.en.md"} {
		path := filepath.Join("..", "..", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("%s not readable: %v", name, err)
		}
		src := string(data)
		doc := ImportMarkdown(src)
		if err := doc.Validate(); err != nil {
			t.Fatalf("%s Validate failed: %v", name, err)
		}
		for i, b := range doc.Blocks {
			if b.Kind == "raw" {
				t.Errorf("%s block %d stayed raw: %q", name, i, b.Text())
			}
		}
		if len(doc.Blocks) == 0 || doc.Blocks[0].Kind != "image" || doc.Blocks[0].Align != "center" || doc.Blocks[0].ImageWidth != 400 {
			t.Fatalf("%s first block should be centered logo image, got %+v", name, doc.Blocks[0])
		}
		inlineImages := 0
		linkedInlineImages := 0
		for _, b := range doc.Blocks {
			for _, r := range b.Runs {
				if r.IsImage() {
					inlineImages++
					if r.Style.Link != "" {
						linkedInlineImages++
					}
				}
			}
		}
		if inlineImages < 10 || linkedInlineImages < 9 {
			t.Fatalf("%s expected >=10 inline images (>=9 linked), got %d (%d linked)", name, inlineImages, linkedInlineImages)
		}
		if got := ExportMarkdown(doc); got != src {
			t.Fatalf("%s unedited round-trip differed (len got=%d want=%d)", name, len(got), len(src))
		}
		// Editing the last block must preserve unrelated table rows' trailing pipes verbatim.
		edited := doc.Clone()
		last := edited.Blocks[len(edited.Blocks)-1]
		last.Runs = append(last.Runs, document.Run{Text: " X"})
		partial := ExportMarkdown(edited)
		if name == "README.md" && !strings.Contains(partial, "| Linux | ARM64 | `.deb`、`.tar.gz` |") {
			t.Fatalf("%s partial edit truncated trailing pipe on unchanged table row", name)
		}
		// Appending a new paragraph at the end must preserve exact inter-chunk blank lines
		// everywhere else (e.g. the two blank lines between Star History </a> and ## 致谢).
		appended := doc.Clone()
		appended.Blocks = append(appended.Blocks, document.NewBlock("paragraph", "Appended verification line."))
		wantAppended := strings.TrimRight(src, "\n") + "\n\nAppended verification line.\n"
		if got := ExportMarkdown(appended); got != wantAppended {
			t.Fatalf("%s appending paragraph altered unrelated lines (got len=%d want len=%d)", name, len(got), len(wantAppended))
		}
	}
}
