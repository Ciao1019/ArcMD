package editor

import (
	"strings"
	"testing"
)

func TestImportHTML(t *testing.T) {
	doc := ImportHTML(`<meta charset="utf-8"><style>p{}</style>
<h1>Title</h1>
<p>Some <b>bold</b>, <span style="font-style: italic">italic</span>, <kbd>Ctrl</kbd>, <q>quoted</q> and <a href="https://go.dev">a link</a>.<br>Second line</p>
<ul><li>one<ul><li>nested</li></ul></li><li><input type="checkbox" checked> done</li></ul>
<ol><li>first</li></ol>
<blockquote><p>quoted</p></blockquote>
<pre><code class="language-go">fmt.Println("hi")
</code></pre>
<hr>
<table><tr><th>a</th><th>b</th></tr><tr><td><i>c</i></td></tr></table>
<img src="https://x/y.png" alt="pic"><script>alert(1)</script>`)
	var got []string
	for _, b := range doc.Blocks {
		got = append(got, b.Kind+":"+b.Text())
	}
	want := []string{
		"heading:Title", "paragraph:Some bold, italic, Ctrl, “quoted” and a link.", "paragraph:Second line",
		"bullet:one", "bullet:nested", "task:done", "numbered:first", "quote:quoted",
		`code:fmt.Println("hi")`, "rule:", "table:", "paragraph:pic",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("blocks\n got %q\nwant %q", got, want)
	}
	b := doc.Blocks
	if b[0].Level != 1 || b[4].Indent != 1 || !b[5].Checked || b[8].Language != "go" {
		t.Errorf("attributes: level %d indent %d checked %v lang %q", b[0].Level, b[4].Indent, b[5].Checked, b[8].Language)
	}
	runs := b[1].Runs
	if !runs[1].Style.Bold || !runs[3].Style.Italic || !runs[5].Style.Kbd || runs[5].Style.Code || runs[7].Style.Link != "https://go.dev" {
		t.Errorf("styles %+v", runs)
	}
	table := b[10]
	if len(table.Children) != 2 || len(table.Children[1].Children) != 2 || !table.Children[1].Children[0].Runs[0].Style.Italic {
		t.Errorf("table %+v", table.Children)
	}
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
	if rich(ImportHTML("<p>just text</p>")) {
		t.Error("plain text counted as rich")
	}
}

func TestImportHTMLDocumentReaderFeatures(t *testing.T) {
	src := `<html><head><title>Reader Title</title></head><body>
<h1 id="TopAnchor">Heading</h1>
<p hidden>Hidden paragraph</p>
<div style="display: none"><p>Also hidden</p></div>
<figure>
  <img src="../Images/photo.png" alt="Diagram">
  <figcaption>Figure 1 caption</figcaption>
</figure>
<svg viewBox="0 0 100 100"><image xlink:href="cover.jpg"/></svg>
<img src="pixel.gif" width="1" height="1">
<dl><dt>Term</dt><dd>Definition</dd></dl>
<a id="sec-table"></a>
<table><caption>Perf Table</caption><tr><th>K</th><th>V</th></tr><tr><td>A</td><td>1</td></tr></table>
<details><summary>Details</summary><p>Expanded body</p></details>
</body></html>`
	res := ImportHTMLDocument(src, HTMLImportOptions{
		EmbedImages: true,
		ResolveSrc: func(s string) string {
			if s == "../Images/photo.png" {
				return "epub:Images/photo.png"
			}
			return s
		},
	})
	if res.Title != "Reader Title" {
		t.Fatalf("Title = %q", res.Title)
	}
	if res.Anchors["TopAnchor"] == "" || res.Anchors["topanchor"] == "" || res.Anchors["sec-table"] == "" {
		t.Fatalf("missing anchor mappings: %+v", res.Anchors)
	}
	var got []string
	var imgURLs []string
	for _, b := range res.Doc.Blocks {
		got = append(got, b.Kind+":"+b.Text())
		if b.Kind == "image" {
			imgURLs = append(imgURLs, b.URL)
		}
	}
	joined := strings.Join(got, "|")
	if strings.Contains(joined, "Hidden paragraph") || strings.Contains(joined, "Also hidden") {
		t.Fatalf("hidden element leaked into blocks: %s", joined)
	}
	if len(imgURLs) != 2 || imgURLs[0] != "epub:Images/photo.png" || imgURLs[1] != "cover.jpg" {
		t.Fatalf("unexpected embedded image URLs: %+v (blocks=%s)", imgURLs, joined)
	}
	if !strings.Contains(joined, "Figure 1 caption") || !strings.Contains(joined, "Perf Table") || !strings.Contains(joined, "Expanded body") {
		t.Fatalf("missing reader blocks: %s", joined)
	}
	if err := res.Doc.Validate(); err != nil {
		t.Fatal(err)
	}
}
