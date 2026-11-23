package editor

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/bookdoc"
	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/egoist/mygo/ui"
)

func makeBookTestPNG(t testing.TB, w, h int, top, body color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: body}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 0, w, max(12, h/4)), &image.Uniform{C: top}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func buildTestEPUBBytes(t testing.TB) []byte {
	t.Helper()
	coverPNG := makeBookTestPNG(t, 120, 160, color.RGBA{34, 88, 210, 255}, color.RGBA{242, 246, 255, 255})
	chartPNG := makeBookTestPNG(t, 200, 96, color.RGBA{24, 148, 92, 255}, color.RGBA{244, 250, 246, 255})

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	addStored := func(name, content string) {
		hdr := &zip.FileHeader{Name: name, Method: zip.Store}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	addBytes := func(name string, data []byte) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}

	addStored("mimetype", "application/epub+zip")
	addBytes("META-INF/container.xml", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`))

	addBytes("OEBPS/content.opf", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="uid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="uid">urn:uuid:arcmd-editor-test</dc:identifier>
    <dc:title>ArcMD 架构手册</dc:title>
    <dc:creator>Prism Engineering</dc:creator>
    <meta name="cover" content="cover-img"/>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="cover-img" href="images/cover.png" media-type="image/png" properties="cover-image"/>
    <item id="arch-img" href="images/arch.png" media-type="image/png"/>
    <item id="ch1" href="ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="ch2" href="ch2.xhtml" media-type="application/xhtml+xml"/>
    <item id="ch3" href="ch3.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>
    <itemref idref="ch1"/>
    <itemref idref="ch2"/>
    <itemref idref="ch3"/>
  </spine>
</package>`))

	addBytes("OEBPS/nav.xhtml", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<body>
  <nav epub:type="toc">
    <ol>
      <li>
        <a href="ch1.xhtml">第一章 概览</a>
        <ol><li><a href="ch1.xhtml#principles">1.1 设计原则</a></li></ol>
      </li>
      <li><a href="ch2.xhtml">第二章 存储与索引</a></li>
      <li><a href="ch3.xhtml">第三章 协同编辑</a></li>
    </ol>
  </nav>
</body>
</html>`))

	addBytes("OEBPS/images/cover.png", coverPNG)
	addBytes("OEBPS/images/arch.png", chartPNG)

	addBytes("OEBPS/ch1.xhtml", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>第一章 概览</title></head><body>
  <h1>第一章 概览</h1>
  <p>ArcMD 阅读引擎使用纯 Go 解析 HTML 与 EPUB，零 CGO 跨平台运行。</p>
  <h2 id="principles">1.1 设计原则</h2>
  <p>点击前往 <a href="ch2.xhtml#bench">存储性能基准</a>，或查看 <a href="https://example.com/spec">外部规范</a>。</p>
</body></html>`))

	addBytes("OEBPS/ch2.xhtml", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>第二章 存储与索引</title></head><body>
  <h1>第二章 存储与索引</h1>
  <h2 id="bench">存储性能基准</h2>
  <figure>
    <img src="images/arch.png" alt="存储架构图"/>
    <figcaption>StorageBenchmark 吞吐对比</figcaption>
  </figure>
  <p>采用增量索引与 StorageBenchmark 基准测试，支持全文快速检索。</p>
</body></html>`))

	addBytes("OEBPS/ch3.xhtml", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>第三章 协同编辑</title></head><body>
  <h1>第三章 协同编辑</h1>
  <p>灰度发布阶段支持三栏 Diff 与实时历史快照。</p>
</body></html>`))

	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func buildTestEPUBDoc(t testing.TB) *bookdoc.Document {
	t.Helper()
	doc, err := bookdoc.OpenBytes("handbook.epub", buildTestEPUBBytes(t))
	if err != nil {
		t.Fatalf("bookdoc.OpenBytes(epub): %v", err)
	}
	t.Cleanup(func() { doc.Close() })
	return doc
}

func buildTestHTMLDoc(t testing.TB) *bookdoc.Document {
	t.Helper()
	htmlSrc := []byte(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>HTML 阅读器指南</title></head>
<body>
  <h1>HTML 阅读器指南</h1>
  <p>介绍纯 Go 原生排版与 <a href="#details">跳转锚点</a>。</p>
  <h2 id="arch">系统结构</h2>
  <table>
    <caption>核心模块指标</caption>
    <thead><tr><th>模块</th><th>职责</th></tr></thead>
    <tbody><tr><td>bookdoc</td><td>编码探测与目录解析</td></tr></tbody>
  </table>
  <h2 id="details">细节说明</h2>
  <p>支持查看 HTML 源码与一键另存为 Markdown。</p>
</body>
</html>`)
	doc, err := bookdoc.OpenBytes("guide.html", htmlSrc)
	if err != nil {
		t.Fatalf("bookdoc.OpenBytes(html): %v", err)
	}
	t.Cleanup(func() { doc.Close() })
	return doc
}

func TestBookURLHelpersAndMarkdownRoundTrip(t *testing.T) {
	if !IsHTMLURL("assets/report%202026.html#intro") || !IsHTMLURL(`docs\page.HTM?v=1`) {
		t.Fatal("IsHTMLURL rejected valid HTML URLs")
	}
	if !IsEPUBURL("books/Spec%202026.epub#chapter=2") || !IsEPUBURL(`books\guide.EPUB`) {
		t.Fatal("IsEPUBURL rejected valid EPUB URLs")
	}
	if IsBookURL("assets/photo.png") || IsBookURL(".html") || IsBookURL(".epub") || IsHTMLURL("assets/spec.epub") {
		t.Fatal("IsBookURL/IsHTMLURL accepted mismatched URLs")
	}

	c1, ch1, a1 := ParseBookURLRef("assets/book.epub#chapter=2")
	if c1 != "assets/book.epub" || ch1 != 2 || a1 != "" {
		t.Fatalf("ParseBookURLRef(#chapter=2) = (%q, %d, %q)", c1, ch1, a1)
	}
	c2, ch2, a2 := ParseBookURLRef("assets/book.epub#3&anchor=sec%201")
	if c2 != "assets/book.epub" || ch2 != 3 || a2 != "sec 1" {
		t.Fatalf("ParseBookURLRef(#3&anchor=sec%%201) = (%q, %d, %q)", c2, ch2, a2)
	}
	c3, ch3, a3 := ParseBookURLRef("docs/guide.html#details")
	if c3 != "docs/guide.html" || ch3 != 0 || a3 != "details" {
		t.Fatalf("ParseBookURLRef(#details) = (%q, %d, %q)", c3, ch3, a3)
	}
	if got := WithBookURLChapter("assets/book.epub#chapter=2", 1); got != "assets/book.epub" {
		t.Fatalf("WithBookURLChapter(..., 1) should strip fragment, got %q", got)
	}
	if got := WithBookURLChapter("assets/book.epub", 4); got != "assets/book.epub#chapter=4" {
		t.Fatalf("WithBookURLChapter(..., 4) = %q", got)
	}

	src := "# 阅读笔记\n\n![体系手册](assets/handbook.epub#chapter=2)\n\n![[slides.epub#chapter=3|520]]\n\n![网页说明](assets/guide.html#details)\n"
	doc := ImportMarkdown(src)
	if len(doc.Blocks) != 4 {
		t.Fatalf("expected 4 blocks, got %d", len(doc.Blocks))
	}
	b1, b2, b3 := doc.Blocks[1], doc.Blocks[2], doc.Blocks[3]
	if !isBookEmbed(b1) || b1.URL != "assets/handbook.epub#chapter=2" {
		t.Fatalf("unexpected EPUB embed block 1: %+v", b1)
	}
	if !isBookEmbed(b2) || b2.URL != "slides.epub#chapter=3" || b2.ImageWidth != 520 {
		t.Fatalf("unexpected Obsidian EPUB embed block 2: %+v", b2)
	}
	if !isBookEmbed(b3) || b3.URL != "assets/guide.html#details" {
		t.Fatalf("unexpected HTML embed block 3: %+v", b3)
	}
	if out := ExportMarkdown(doc); out != src {
		t.Fatalf("unmodified ExportMarkdown changed source:\ngot:  %q\nwant: %q", out, src)
	}
}

func TestBookViewNavigationSearchLinksAndSource(t *testing.T) {
	t.Run("EPUB", func(t *testing.T) {
		doc := buildTestEPUBDoc(t)
		v := NewBookViewer(doc, nil)
		defer v.Close()

		var extOpened string
		tt := ui.NewTester(func(c *ui.Context) {
			BookView(c, v, BookOptions{
				Theme:           c.Theme(),
				Title:           "handbook.epub",
				OpenExternalURL: func(u string) { extOpened = u },
			})
		}, 1040, 720)
		tt.SetLocale("zh-Hans")
		v.RequestFocus = true
		tt.Frame()

		if v.CurrentChapter != 0 || !tt.HasText("第 1 / 3 章") {
			t.Fatalf("expected initial chapter '第 1 / 3 章', got ch=%d texts=%v", v.CurrentChapter, tt.Texts())
		}
		if v.CoverBitmap() == nil {
			t.Fatal("expected EPUB cover bitmap to decode")
		}

		// Open TOC sidebar with ⇧⌘O and click Chapter 2.
		tt.Key(ui.Cmd|ui.Shift, ui.KeyO)
		tt.Frame()
		if !v.SidebarOpen || !tt.HasText("第二章 存储与索引") || !tt.HasText("1.1 设计原则") {
			t.Fatalf("expected ⇧⌘O to open EPUB TOC sidebar, open=%v texts=%v", v.SidebarOpen, tt.Texts())
		}
		if err := tt.Click("第二章 存储与索引"); err != nil {
			t.Fatalf("Click TOC item: %v", err)
		}
		tt.Frame()
		if v.CurrentChapter != 1 || !tt.HasText("第 2 / 3 章") {
			t.Fatalf("expected click to switch to chapter 2, got ch=%d", v.CurrentChapter)
		}

		// Step chapter forward via StepChapter and back via BracketLeft ('[').
		v.StepChapter(1)
		tt.Frame()
		if v.CurrentChapter != 2 || !tt.HasText("第 3 / 3 章") {
			t.Fatalf("expected StepChapter(1) -> chapter 3, got ch=%d", v.CurrentChapter)
		}
		tt.Key(0, ui.KeyBracketLeft)
		tt.Frame()
		if v.CurrentChapter != 1 {
			t.Fatalf("expected BracketLeft -> chapter 2 (idx 1), got ch=%d", v.CurrentChapter)
		}

		// Find across chapters jumps to matching chapter and highlights result; Esc closes find.
		v.FindText("灰度发布")
		tt.Frame()
		if !v.FindOpen || len(v.Matches) == 0 || v.CurrentChapter != 2 {
			t.Fatalf("expected FindText('灰度发布') -> ch 2, open=%v matches=%+v ch=%d", v.FindOpen, v.Matches, v.CurrentChapter)
		}
		tt.Key(0, ui.KeyEscape)
		tt.Frame()
		if v.FindOpen {
			t.Fatal("expected Escape to close find bar")
		}

		// Internal relative link and external HTTPS link resolution inside BookView.
		targetCh, anchor, extURL, ok := doc.ResolveLink(0, "ch2.xhtml#bench")
		if !ok || targetCh != 1 || anchor != "bench" || extURL != "" {
			t.Fatalf("ResolveLink(ch2.xhtml#bench) = (%d, %q, %q, %v)", targetCh, anchor, extURL, ok)
		}
		v.GoToChapterAnchor(targetCh, anchor, true)
		tt.Frame()
		if v.CurrentChapter != 1 || v.activeAnchor != "bench" {
			t.Fatalf("expected GoToChapterAnchor(1, bench), got ch=%d anchor=%q", v.CurrentChapter, v.activeAnchor)
		}

		targetCh2, _, extURL2, ok2 := doc.ResolveLink(0, "https://example.com/spec")
		if !ok2 || targetCh2 >= 0 || extURL2 != "https://example.com/spec" {
			t.Fatalf("ResolveLink external = (%d, %q, %v)", targetCh2, extURL2, ok2)
		}
		_ = extOpened

		mdOut := v.ExportCurrentChapterMarkdown()
		if !strings.Contains(mdOut, "存储与索引") || !strings.Contains(mdOut, "StorageBenchmark") {
			t.Fatalf("unexpected EPUB chapter Markdown export: %q", mdOut)
		}
	})

	t.Run("HTML", func(t *testing.T) {
		doc := buildTestHTMLDoc(t)
		v := NewBookViewer(doc, nil)
		defer v.Close()

		tt := ui.NewTester(func(c *ui.Context) {
			BookView(c, v, BookOptions{
				Theme: c.Theme(),
				Title: "guide.html",
			})
		}, 1040, 720)
		tt.SetLocale("zh-Hans")
		v.RequestFocus = true
		tt.Frame()

		// Toggle TOC outline and jump to #details.
		tt.Key(ui.Cmd|ui.Shift, ui.KeyO)
		tt.Frame()
		if !v.SidebarOpen || !tt.HasText("系统结构") || !tt.HasText("细节说明") {
			t.Fatalf("expected HTML TOC items in sidebar, open=%v texts=%v", v.SidebarOpen, tt.Texts())
		}
		if err := tt.Click("细节说明"); err != nil {
			t.Fatalf("Click HTML TOC item: %v", err)
		}
		tt.Frame()
		if v.activeAnchor != "details" {
			t.Fatalf("expected activeAnchor 'details' after TOC click, got %q", v.activeAnchor)
		}

		// Toggle HTML source view and return to reader view via UI button.
		v.ToggleSource()
		tt.Frame()
		if !v.ShowSource || !tt.HasText("返回阅读视图") {
			t.Fatalf("expected source view active with '返回阅读视图', showSource=%v texts=%v", v.ShowSource, tt.Texts())
		}
		srcText := v.sourceEditor().Doc.PlainText()
		if !strings.Contains(srcText, `<h2 id="details">`) {
			t.Fatalf("expected raw HTML source editor content, got %q", srcText)
		}
		if err := tt.Click("返回阅读视图"); err != nil {
			t.Fatalf("Click '返回阅读视图': %v", err)
		}
		tt.Frame()
		if v.ShowSource {
			t.Fatal("expected clicking '返回阅读视图' to exit source view")
		}

		mdOut := v.ExportCurrentChapterMarkdown()
		if !strings.Contains(mdOut, "HTML 阅读器指南") || !strings.Contains(mdOut, "bookdoc") {
			t.Fatalf("unexpected HTML Markdown export: %q", mdOut)
		}
	})
}

func TestInlineBookBlockRestBrowsingModalAndExport(t *testing.T) {
	epubDoc := buildTestEPUBDoc(t)
	doc := ImportMarkdown("# 架构评审\n\n![体系手册](assets/handbook.epub)\n\n正文段落继续保持顺滑滚动。\n")
	e := FromDocument(doc)
	blockID := e.Doc.Blocks[1].ID

	var splitTarget string
	tt := ui.NewTester(func(c *ui.Context) {
		View(c, e, Options{
			Theme: c.Theme(),
			Width: 880,
			LoadBook: func(src string) (*bookdoc.Document, uint64, error) {
				if src == "assets/handbook.epub" {
					return epubDoc, 1, nil
				}
				return nil, 0, os.ErrNotExist
			},
			OpenBook: func(target string, split bool) {
				if split {
					splitTarget = target
				}
			},
		})
	}, 960, 760)
	tt.SetLocale("zh-Hans")
	tt.Frame()

	if !tt.HasText("第 1 / 3 章") {
		t.Fatalf("expected rest-mode book card stepper '第 1 / 3 章', got %v", tt.Texts())
	}
	revBefore := e.Revision()

	// Step rest card to chapter 2 without mutating Markdown document.
	e.stepRestBookChapter(e.Doc.Blocks[1], epubDoc, 1)
	tt.Frame()
	if !tt.HasText("第 2 / 3 章") {
		t.Fatalf("expected rest-mode card to step to '第 2 / 3 章', got %v", tt.Texts())
	}
	if e.Revision() != revBefore {
		t.Fatal("stepping preview chapter in rest mode must not mutate Markdown document")
	}

	// Activate inline interactive browsing and pin chapter 2 into Markdown URL (undoable).
	e.BrowseBookBlock(blockID)
	tt.Frame()
	if e.bookBrowsing != blockID {
		t.Fatal("expected inline book browsing to be active")
	}
	e.pinBookBlockChapter(blockID, 2)
	tt.Frame()
	if got := e.Doc.Blocks[1].URL; got != "assets/handbook.epub#chapter=2" {
		t.Fatalf("expected pinned URL 'assets/handbook.epub#chapter=2', got %q", got)
	}
	if md := ExportMarkdown(e.Doc); !strings.Contains(md, "![体系手册](assets/handbook.epub#chapter=2)") {
		t.Fatalf("expected ExportMarkdown to persist #chapter=2, got %q", md)
	}
	e.Undo()
	tt.Frame()
	if got := e.Doc.Blocks[1].URL; got != "assets/handbook.epub" {
		t.Fatalf("expected Undo to restore 'assets/handbook.epub', got %q", got)
	}

	// Open modal reader, Esc closes when find bar is closed, then reopen and trigger split read.
	e.OpenBookModal(blockID)
	tt.Frame()
	if e.bookModalID != blockID || !tt.HasText("完成") {
		t.Fatalf("expected book modal open, modalID=%q texts=%v", e.bookModalID, tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if e.bookModalID != "" {
		t.Fatalf("expected Escape to close book modal, still %q", e.bookModalID)
	}

	e.OpenBookModal(blockID)
	tt.Advance(80 * time.Millisecond)
	tt.Frame()
	if err := tt.Click("在分屏中阅读"); err != nil {
		t.Fatalf("Click split button in modal: %v", err)
	}
	tt.Frame()
	if splitTarget != "assets/handbook.epub#chapter=2" || e.bookModalID != "" {
		t.Fatalf("expected modal split button -> 'assets/handbook.epub#chapter=2' & close modal, got target=%q modal=%q", splitTarget, e.bookModalID)
	}

	// HTML & Vector PDF exports include the embedded EPUB cleanly.
	htmlOut := ExportHTML(e.Doc)
	if !strings.Contains(htmlOut, "assets/handbook.epub") {
		t.Fatalf("ExportHTML missing EPUB link: %q", htmlOut)
	}
	th := ui.LightTheme()
	opts := Options{
		Theme: th,
		Width: 680,
		LoadBook: func(src string) (*bookdoc.Document, uint64, error) {
			return epubDoc, 1, nil
		},
	}
	var tops []float32
	ui.Render(func(c *ui.Context) {
		tops = e.Paginate(opts, 848)
	}, 1, 1, 1)
	if len(tops) == 0 {
		t.Fatal("Paginate returned no pages")
	}
	var pdfBuf bytes.Buffer
	vw := pdf.NewWriter(&pdfBuf, "Exported Note")
	vw.ReservePages(len(tops))
	vp := vw.NewVectorPage(pdf.A4Width, pdf.A4Height)
	e.ExportVectorPage(vp, opts, tops[0], tops[0]+848, 0, len(tops), tops, 680, 920, 36)
	if err := vw.FinishVectorPage(vp); err != nil {
		t.Fatalf("FinishVectorPage: %v", err)
	}
	if err := vw.Close(); err != nil {
		t.Fatalf("Close exported PDF: %v", err)
	}
	if pdfBuf.Len() < 512 {
		t.Fatalf("exported vector PDF suspiciously small: %d bytes", pdfBuf.Len())
	}
}

type mockHTMLWebView struct {
	scrolledAnchor  string
	scrolledHeading int
	findCalls       []string
	lastForward     bool
	lastFindNext    bool
	stopFindCount   int
	reloads         int
	destroyed       bool
}

func (m *mockHTMLWebView) Native() ui.NativeWebView { return nil }
func (m *mockHTMLWebView) ScrollToAnchor(anchor string, headingIndex int) {
	m.scrolledAnchor, m.scrolledHeading = anchor, headingIndex
}
func (m *mockHTMLWebView) Find(query string, forward bool, findNext bool, onResult func(active, total int)) {
	m.findCalls = append(m.findCalls, query)
	m.lastForward, m.lastFindNext = forward, findNext
	if onResult != nil {
		onResult(1, 3)
	}
}
func (m *mockHTMLWebView) StopFind() { m.stopFindCount++ }
func (m *mockHTMLWebView) Reload()   { m.reloads++ }
func (m *mockHTMLWebView) Destroy()  { m.destroyed = true }

func TestBookViewWebViewCallbacksAndInlineSlots(t *testing.T) {
	t.Run("standalone-webview-toc-find-and-source-toggle", func(t *testing.T) {
		doc := buildTestHTMLDoc(t)
		v := NewBookViewer(doc, nil)
		defer v.Close()
		mw := &mockHTMLWebView{scrolledHeading: -1}

		tt := ui.NewTester(func(c *ui.Context) {
			BookView(c, v, BookOptions{
				Theme:   c.Theme(),
				Title:   "guide.html",
				WebView: func() HTMLWebView { return mw },
			})
		}, 1040, 720)
		tt.SetLocale("zh-Hans")
		v.RequestFocus = true
		tt.Frame()

		if v.activeWebView != mw {
			t.Fatalf("expected activeWebView bound to mock")
		}

		// TOC click routes anchor & heading index to HTMLWebView.
		v.ToggleOutline()
		tt.Frame()
		if err := tt.Click("细节说明"); err != nil {
			t.Fatalf("Click HTML TOC item: %v", err)
		}
		tt.Frame()
		if mw.scrolledAnchor != "details" || mw.scrolledHeading != 2 {
			t.Fatalf("expected ScrollToAnchor('details', 2), got (%q, %d)", mw.scrolledAnchor, mw.scrolledHeading)
		}

		// Find routes through HTMLWebView and closing find invokes StopFind.
		v.FindText("原生")
		tt.Frame()
		if len(mw.findCalls) == 0 || mw.findCalls[0] != "原生" || v.webFindActive != 1 || v.webFindTotal != 3 || !tt.HasText("1/3") {
			t.Fatalf("expected WebView find 1/3, calls=%v active=%d total=%d texts=%v", mw.findCalls, v.webFindActive, v.webFindTotal, tt.Texts())
		}
		v.syncWebFind(mw, true, true)
		if !mw.lastForward || !mw.lastFindNext {
			t.Fatalf("expected FindNext forward call")
		}
		v.closeFind()
		tt.Frame()
		if mw.stopFindCount == 0 || v.webFindTotal != 0 {
			t.Fatalf("expected StopFind on closeFind, count=%d total=%d", mw.stopFindCount, v.webFindTotal)
		}

		// ToggleSource suppresses activeWebView and displays raw source view.
		v.ToggleSource()
		tt.Frame()
		if !v.ShowSource || v.activeWebView != nil || !tt.HasText("返回阅读视图") {
			t.Fatalf("expected ToggleSource to hide WebView, showSource=%v activeWV=%v", v.ShowSource, v.activeWebView)
		}
		v.ToggleSource()
		tt.Frame()
		if v.ShowSource || v.activeWebView != mw {
			t.Fatalf("expected exiting source view to restore activeWebView")
		}
	})

	t.Run("inline-html-slots-rest-browse-modal", func(t *testing.T) {
		htmlDoc := buildTestHTMLDoc(t)
		doc := ImportMarkdown("# 笔记\n\n![指南](assets/guide.html#details)\n\n结尾段落。\n")
		e := FromDocument(doc)
		blockID := e.Doc.Blocks[1].ID

		seenSlots := map[string]bool{}
		mw := &mockHTMLWebView{scrolledHeading: -1}
		tt := ui.NewTester(func(c *ui.Context) {
			View(c, e, Options{
				Theme: c.Theme(),
				Width: 880,
				LoadBook: func(src string) (*bookdoc.Document, uint64, error) {
					return htmlDoc, 1, nil
				},
				HTMLWebView: func(bookURL, slot string, scrollToContainer bool) HTMLWebView {
					seenSlots[slot] = scrollToContainer
					return mw
				},
			})
		}, 960, 760)
		tt.SetLocale("zh-Hans")
		tt.Frame()

		if scrollContainer, ok := seenSlots["rest:"+blockID]; !ok || !scrollContainer {
			t.Fatalf("expected rest slot with ScrollToContainer=true, got seen=%+v", seenSlots)
		}
		if r := e.rowAt(e.Engine.LeafAt(0).End + 1); r != nil && r.bookEmbed && r.bookRect.H != htmlRestH {
			t.Fatalf("expected HTML rest card height %v, got %v", htmlRestH, r.bookRect.H)
		}

		e.BrowseBookBlock(blockID)
		tt.Frame()
		if scrollContainer, ok := seenSlots["browse:"+blockID]; !ok || scrollContainer {
			t.Fatalf("expected browse slot with ScrollToContainer=false, got seen=%+v", seenSlots)
		}

		e.OpenBookModal(blockID)
		tt.Frame()
		if scrollContainer, ok := seenSlots["modal:"+blockID]; !ok || scrollContainer {
			t.Fatalf("expected modal slot with ScrollToContainer=false, got seen=%+v", seenSlots)
		}
	})
}
