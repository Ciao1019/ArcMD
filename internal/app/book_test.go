package app

import (
	"archive/zip"
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/agent"
	"github.com/Ciao1019/ArcMD/internal/bookdoc"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/search"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func makeBookSnapshotPNG(t testing.TB, w, h int, top, body, accent color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: body}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 0, w, max(16, h/3)), &image.Uniform{C: top}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(16, max(20, h/3-10), w-16, max(28, h/3+4)), &image.Uniform{C: accent}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

// buildSampleEPUB builds a multi-chapter EPUB 3 sample used by app and filedrop tests.
func buildSampleEPUB(t testing.TB) []byte {
	t.Helper()
	coverPNG := makeBookSnapshotPNG(t, 160, 220, color.RGBA{34, 88, 212, 255}, color.RGBA{244, 247, 255, 255}, color.RGBA{99, 102, 241, 255})
	archPNG := makeBookSnapshotPNG(t, 280, 140, color.RGBA{18, 138, 92, 255}, color.RGBA{243, 250, 247, 255}, color.RGBA{56, 189, 148, 255})

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
    <dc:identifier id="uid">urn:uuid:arcmd-sample-epub</dc:identifier>
    <dc:title>ArcMD 引擎架构指南</dc:title>
    <dc:creator>ArcMD 工程团队</dc:creator>
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
        <a href="ch1.xhtml">第一章 架构概览</a>
        <ol><li><a href="ch1.xhtml#pipeline">1.1 纯 Go 解析管线</a></li></ol>
      </li>
      <li><a href="ch2.xhtml">第二章 存储与全文索引</a></li>
      <li><a href="ch3.xhtml">第三章 灰度发布与协同</a></li>
    </ol>
  </nav>
</body>
</html>`))

	addBytes("OEBPS/images/cover.png", coverPNG)
	addBytes("OEBPS/images/arch.png", archPNG)

	addBytes("OEBPS/ch1.xhtml", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>第一章 架构概览</title></head><body>
  <h1>第一章 架构概览</h1>
  <p>ArcMD 采用纯 Go 实现 HTML 与 EPUB 原生阅读体验，支持 Markdown 内联浏览卡片、目录大纲与全文检索。</p>
  <h2 id="pipeline">1.1 纯 Go 解析管线</h2>
  <p>点击前往 <a href="ch2.xhtml#bench">存储与全文索引基准</a>，了解 LRU 章节缓存策略。</p>
</body></html>`))

	addBytes("OEBPS/ch2.xhtml", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>第二章 存储与全文索引</title></head><body>
  <h1>第二章 存储与全文索引</h1>
  <h2 id="bench">存储基准测试</h2>
  <figure>
    <img src="images/arch.png" alt="存储与索引架构图"/>
    <figcaption>一致性哈希与增量倒排索引架构</figcaption>
  </figure>
  <p>采用一致性哈希与增量倒排索引实现毫秒级检索，虚拟滚动保持低内存占用。</p>
  <table>
    <caption>性能基线对比</caption>
    <thead><tr><th>指标</th><th>原方案</th><th>优化后</th></tr></thead>
    <tbody><tr><td>打开首帧</td><td>48 ms</td><td>12 ms</td></tr><tr><td>搜索响应</td><td>35 ms</td><td>6 ms</td></tr></tbody>
  </table>
</body></html>`))

	addBytes("OEBPS/ch3.xhtml", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>第三章 灰度发布与协同</title></head><body>
  <h1>第三章 灰度发布与协同</h1>
  <p>通过灰度发布、三栏 Merge 与版本快照实现多端可靠协同。</p>
</body></html>`))

	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func buildSampleHTML() []byte {
	return []byte(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>原生 HTML 阅读规范</title></head>
<body>
  <h1>原生 HTML 阅读规范</h1>
  <p>ArcMD 使用原生 Go 组件排版 HTML 文档，无须 WebView 或 CGO。</p>
  <h2 id="arch">架构设计</h2>
  <table>
    <caption>核心职责矩阵</caption>
    <thead><tr><th>包名</th><th>职责</th></tr></thead>
    <tbody><tr><td>bookdoc</td><td>ZIP/OPF/TOC 解析与编码探测</td></tr><tr><td>editor</td><td>只读排版、查找高亮与内联卡片</td></tr></tbody>
  </table>
  <pre><code class="language-go">doc, err := bookdoc.Open(path)</code></pre>
  <h2 id="details">零依赖渲染细节</h2>
  <p>通过零依赖渲染与 HTML 源码视图切换实现完整阅读体验，可随时在默认浏览器中打开或另存为 Markdown。</p>
</body>
</html>`)
}

func buildDRMEPUB(t testing.TB) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, content string, store bool) {
		hdr := &zip.FileHeader{Name: name}
		if store {
			hdr.Method = zip.Store
		} else {
			hdr.Method = zip.Deflate
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	add("mimetype", "application/epub+zip", true)
	add("META-INF/container.xml", `<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`, false)
	add("META-INF/encryption.xml", `<?xml version="1.0"?>
<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container" xmlns:enc="http://www.w3.org/2001/04/xmlenc#">
  <enc:EncryptedData>
    <enc:EncryptionMethod Algorithm="http://www.w3.org/2001/04/xmlenc#aes128-cbc"/>
    <enc:CipherData><enc:CipherReference URI="OEBPS/ch1.xhtml"/></enc:CipherData>
  </enc:EncryptedData>
</encryption>`, false)
	add("OEBPS/content.opf", `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>DRM 受保护书籍</dc:title></metadata><manifest><item id="ch1" href="ch1.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="ch1"/></spine></package>`, false)
	add("OEBPS/ch1.xhtml", "encrypted-payload", false)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestStandaloneBookOpenTOCSearchQAAndSaveMarkdown(t *testing.T) {
	a, tt := newTestApp(t)
	if err := os.WriteFile(filepath.Join(root(a), "系统架构手册.epub"), buildSampleEPUB(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root(a), "原生HTML规范.html"), buildSampleHTML(), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.refreshSearch()
	tt.Frame()

	// Click standalone EPUB in sidebar and verify read-only guarantees.
	if err := tt.Click("系统架构手册"); err != nil {
		t.Fatalf("Click EPUB in sidebar: %v", err)
	}
	tt.Frame()
	if a.doc == nil || !a.doc.isBook() || !a.doc.book.IsEPUB() || a.doc.rel != "系统架构手册.epub" {
		t.Fatalf("expected standalone EPUB open, got %+v", a.doc)
	}
	if !tt.HasText("第 1 / 3 章") {
		t.Fatalf("expected EPUB chapter indicator '第 1 / 3 章', got %v", tt.Texts())
	}

	tt.Advance(2 * autosaveDelay)
	tt.Frame()
	if a.doc.dirty() {
		t.Fatal("standalone EPUB must never become dirty")
	}
	if _, err := os.Stat(filepath.Join(root(a), ".arcmd", "history")); err == nil {
		t.Fatal("opening a read-only EPUB must not create .arcmd/history")
	}

	// Open TOC sidebar with ⇧⌘O and jump to Chapter 2.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyO)
	tt.Frame()
	if !a.doc.book.SidebarOpen || !tt.HasText("第二章 存储与全文索引") {
		t.Fatalf("expected ⇧⌘O to open EPUB TOC sidebar, open=%v texts=%v", a.doc.book.SidebarOpen, tt.Texts())
	}
	if err := tt.Click("第二章 存储与全文索引"); err != nil {
		t.Fatalf("Click EPUB TOC item: %v", err)
	}
	tt.Frame()
	if a.doc.book.CurrentChapter != 1 {
		t.Fatalf("expected chapter index 1 after TOC click, got %d", a.doc.book.CurrentChapter)
	}

	// Open standalone HTML, toggle HTML source view, return to reader, and save as Markdown note.
	if err := tt.Click("原生HTML规范"); err != nil {
		t.Fatalf("Click HTML in sidebar: %v", err)
	}
	tt.Frame()
	if a.doc == nil || !a.doc.isBook() || !a.doc.book.IsHTML() {
		t.Fatalf("expected standalone HTML open, got %+v", a.doc)
	}
	a.doc.book.ToggleSource()
	tt.Frame()
	if !a.doc.book.ShowSource || !tt.HasText("返回阅读视图") {
		t.Fatalf("expected HTML source view active, showSource=%v texts=%v", a.doc.book.ShowSource, tt.Texts())
	}
	if err := tt.Click("返回阅读视图"); err != nil {
		t.Fatalf("Click '返回阅读视图': %v", err)
	}
	tt.Frame()
	if a.doc.book.ShowSource {
		t.Fatal("expected returning from HTML source view")
	}

	a.saveBookAsMarkdown(a.doc)
	tt.Frame()
	if a.doc == nil || a.doc.isBook() || !strings.HasSuffix(a.doc.rel, ".md") {
		t.Fatalf("expected saveBookAsMarkdown to open generated .md note, got %+v", a.doc)
	}
	savedRaw, err := vault.Read(root(a), a.doc.rel)
	if err != nil || !strings.Contains(savedRaw, "零依赖渲染") {
		t.Fatalf("saved Markdown note missing expected content: err=%v raw=%q", err, savedRaw)
	}

	// Full-text search indexes both EPUB and HTML, and openAtText opens the target chapter.
	a.open("Welcome.md")
	tt.Frame()
	spID := a.state.ActiveSpace().ID
	epubHits := a.textHits("一致性哈希")
	if len(epubHits) == 0 || !slices.ContainsFunc(epubHits, func(h search.Hit) bool { return h.Path == "系统架构手册.epub" }) {
		t.Fatalf("expected full-text search hit in 系统架构手册.epub, got %+v", epubHits)
	}
	htmlHits := a.textHits("零依赖渲染")
	if len(htmlHits) == 0 || !slices.ContainsFunc(htmlHits, func(h search.Hit) bool { return h.Path == "原生HTML规范.html" }) {
		t.Fatalf("expected full-text search hit in 原生HTML规范.html, got %+v", htmlHits)
	}

	a.openAtText(spID, "系统架构手册.epub", "一致性哈希")
	tt.Frame()
	if !a.doc.isBook() || !a.doc.book.FindOpen || a.doc.book.CurrentChapter != 1 {
		t.Fatalf("expected openAtText to open EPUB chapter 2 (idx 1) with FindOpen, got ch=%d find=%v", a.doc.book.CurrentChapter, a.doc.book.FindOpen)
	}

	// AI QA projects EPUB chapters & HTML blocks and citation clicks jump accurately.
	qaFS := a.qaSpace(spID)
	epPage, err := qaFS.Page("系统架构手册.epub")
	if err != nil || len(epPage.Lines) == 0 {
		t.Fatalf("qaSpace.Page failed for EPUB: page=%+v err=%v", epPage, err)
	}
	ch2Line := 0
	for i, ln := range epPage.Lines {
		if strings.Contains(ln, "第二章 存储与全文索引") {
			ch2Line = i + 1
			break
		}
	}
	if ch2Line == 0 {
		t.Fatalf("expected chapter 2 heading in EPUB QA projection, got %v", epPage.Lines)
	}
	a.openCitation(spID, agent.Citation{Path: "系统架构手册.epub", From: ch2Line, To: ch2Line})
	tt.Frame()
	if !a.doc.isBook() || a.doc.book.CurrentChapter != 1 {
		t.Fatalf("expected QA citation click to jump EPUB to chapter 2 (idx 1), got %d", a.doc.book.CurrentChapter)
	}

	htmlPage, err := qaFS.Page("原生HTML规范.html")
	if err != nil || len(htmlPage.Lines) == 0 {
		t.Fatalf("qaSpace.Page failed for HTML: page=%+v err=%v", htmlPage, err)
	}
	detailLine := 0
	for i, ln := range htmlPage.Lines {
		if strings.Contains(ln, "零依赖渲染细节") {
			detailLine = i + 1
			break
		}
	}
	if detailLine == 0 {
		t.Fatalf("expected section heading in HTML QA projection, got %v", htmlPage.Lines)
	}
	a.openCitation(spID, agent.Citation{Path: "原生HTML规范.html", From: detailLine, To: detailLine})
	tt.Frame()
	if !a.doc.isBook() || !a.doc.book.IsHTML() || a.doc.rel != "原生HTML规范.html" {
		t.Fatalf("expected QA citation click to open 原生HTML规范.html, got %+v", a.doc)
	}
}

func TestDropBookIntoMarkdownRelocateRenameAndSplit(t *testing.T) {
	a, tt := dropApp(t)
	epubBytes := buildSampleEPUB(t)
	extEPUB := writeExternalFile(t, t.TempDir(), "Handbook Spec.epub", epubBytes)
	extHTML := writeExternalFile(t, t.TempDir(), "Design Note.html", buildSampleHTML())

	// Drop external EPUB onto "beta" paragraph: copies to assets/ and embeds in one undo step.
	dropAt(t, a, tt, "beta", extEPUB)
	want := []string{"Drop", "alpha", "image:assets/Handbook-Spec.epub", "beta", "gamma"}
	if got := blockList(a); !slices.Equal(got, want) {
		t.Fatalf("blocks after EPUB drop = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(root(a), "assets", "Handbook-Spec.epub")); err != nil {
		t.Fatalf("dropped EPUB was not copied to assets/: %v", err)
	}

	// Single-step Undo & Redo.
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	if got := blockList(a); slices.Contains(got, "image:assets/Handbook-Spec.epub") {
		t.Fatalf("Cmd+Z did not undo EPUB drop: %q", got)
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	tt.Frame()
	if got := blockList(a); !slices.Equal(got, want) {
		t.Fatalf("Shift+Cmd+Z did not redo EPUB drop: %q", got)
	}

	// Also drop HTML into assets/ for relocation check below.
	if err := os.WriteFile(filepath.Join(root(a), "assets", "Design-Note.html"), buildSampleHTML(), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = extHTML

	// Move a Markdown note containing #chapter=2 and #details into Journal/; verify assets + fragments relocate cleanly.
	noteMD := "# 架构周会\n\n![手册](assets/Handbook-Spec.epub#chapter=2)\n\n![网页](assets/Design-Note.html#details)\n"
	if err := writeNative(root(a), "架构周会.md", noteMD); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("架构周会.md")
	tt.Frame()

	sp := a.state.ActiveSpace()
	movedTo, _, err := a.moveWithResources(sp, "架构周会.md", sp, "Journal")
	if err != nil {
		t.Fatalf("moveWithResources: %v", err)
	}
	a.refreshTree()
	a.open(movedTo)
	tt.Frame()
	movedRaw, err := vault.Read(root(a), movedTo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(movedRaw, "Handbook-Spec.epub#chapter=2") || !strings.Contains(movedRaw, "Design-Note.html#details") {
		t.Fatalf("moving Markdown document lost #chapter=2 or #details fragment: %q", movedRaw)
	}
	if _, err := os.Stat(filepath.Join(root(a), "Journal", "assets", "Handbook-Spec.epub")); err != nil {
		t.Fatalf("moving Markdown document did not relocate EPUB asset into Journal/assets/: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root(a), "Journal", "assets", "Design-Note.html")); err != nil {
		t.Fatalf("moving Markdown document did not relocate HTML asset into Journal/assets/: %v", err)
	}

	// Renaming a space-root standalone EPUB relinks Markdown [[wiki]] & embed preserving #chapter=3.
	if err := os.WriteFile(filepath.Join(root(a), "指南手册.epub"), epubBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeNative(root(a), "阅读索引.md", "# 阅读索引\n\n详见 [[指南手册]]：\n\n![指南手册](指南手册.epub#chapter=3)\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("阅读索引.md")
	tt.Frame()

	a.startRename("指南手册.epub")
	a.newName = "新版手册"
	a.commitRename()
	saveSettle(tt)

	relinkedRaw, err := vault.Read(root(a), "阅读索引.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(relinkedRaw, "[[新版手册]]") || !strings.Contains(relinkedRaw, "新版手册.epub#chapter=3") {
		t.Fatalf("renaming standalone EPUB did not relink Markdown wiki & embed with #chapter=3: %q", relinkedRaw)
	}

	// Open in Split View jumped to chapter 3 (0-based index 2).
	a.open("阅读索引.md")
	tt.Frame()
	a.openBookFrom(a.doc, "新版手册.epub#chapter=3", true)
	settle(tt)
	tt.Advance(200 * time.Millisecond)
	tt.Frame()
	if a.split == nil || len(a.split.panes) != 2 {
		t.Fatalf("expected Split View with 2 panes, got %+v", a.split)
	}
	rightDoc := a.split.panes[1].doc
	if rightDoc == nil || !rightDoc.isBook() || rightDoc.rel != "新版手册.epub" || rightDoc.book.CurrentChapter != 2 {
		t.Fatalf("expected right split pane on 新版手册.epub chapter 3 (idx 2), got doc=%+v", rightDoc)
	}
}

func TestDRMandCorruptedBooksInSpaceAndPreview(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	r := root(a)

	if err := os.WriteFile(filepath.Join(r, "drm.epub"), buildDRMEPUB(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r, "broken.epub"), []byte("not-a-zip-archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r, "broken.html"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r, "guide.html"), buildSampleHTML(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r, "good.md"), []byte("# 正常笔记\n用于回归验证搜索索引。\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a.refreshTree()
	a.refreshSearch()
	settle(tt)

	// Opening DRM-protected EPUB renders friendly notice without crashing.
	a.open("drm.epub")
	tt.Frame()
	if a.doc == nil || !a.doc.isBook() || !errors.Is(a.doc.book.LoadErr, bookdoc.ErrEncrypted) {
		t.Fatalf("expected drm.epub to report ErrEncrypted, got doc=%+v", a.doc)
	}
	if !tt.HasText("此 EPUB 受 DRM 保护") {
		t.Fatalf("expected DRM notice in UI, got %v", tt.Texts())
	}

	// Opening corrupted EPUB renders friendly error card instead of crashing.
	a.open("broken.epub")
	tt.Frame()
	if a.doc == nil || !a.doc.isBook() || a.doc.book.LoadErr == nil || !tt.HasText("无法打开文档") {
		t.Fatalf("expected broken.epub error card, got %+v texts=%v", a.doc, tt.Texts())
	}

	// Valid document search continues working alongside DRM/corrupted files.
	hits := a.search.index.Search("回归验证", nil, 10)
	if len(hits) != 1 || hits[0].Path != "good.md" {
		t.Fatalf("expected search hit on good.md despite broken books, got %+v", hits)
	}

	// Sidebar hover preview works on guide.html without panicking.
	row := rowRect(a, "guide.html")
	tt.Move(row.X+40, row.Y+row.H/2)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if s := a.preview.shown; s == nil || s.kind != previewDoc || s.rel != "guide.html" {
		t.Fatalf("expected document preview for guide.html, got %+v", s)
	}
}

func TestBookSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render HTML/EPUB viewer snapshots")
	epubBytes := buildSampleEPUB(t)
	htmlBytes := buildSampleHTML()

	setupBookSpace := func(t *testing.T, dark bool) (*App, *ui.Tester) {
		t.Helper()
		a, tt := newTestApp(t)
		tt.SetSize(1440, 920)
		sp := a.state.ActiveSpace()
		if dark {
			tt.SetDark(true)
			sp.SetTheme(space.Theme{Colors: []string{"#3B5BDB"}, Appearance: space.AppearanceDark, Intensity: 0.52})
		} else {
			sp.SetTheme(space.Theme{Colors: []string{"#DCE8F8", "#E9E6F8"}, Appearance: space.AppearanceLight, Intensity: 0.45})
		}

		if err := os.WriteFile(filepath.Join(root(a), "ArcMD架构指南.epub"), epubBytes, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root(a), "原生HTML阅读规范.html"), htmlBytes, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root(a), "assets"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root(a), "assets", "ArcMD架构指南.epub"), epubBytes, 0o644); err != nil {
			t.Fatal(err)
		}

		noteMD := "# 架构评审纪要 Q4\n\n> [!IMPORTANT]\n> 完整电子书请参阅 [[ArcMD架构指南]]，以下内联嵌入了第 2 章核心存储与索引分析，支持直接切换「内联浏览」或「在分屏中阅读」。\n\n![ArcMD架构指南](assets/ArcMD架构指南.epub#chapter=2)\n\n## 评审结论与后续动作\n\n- [x] 纯 Go HTML/EPUB 解析（UTF-8 / GB18030 / OPF / NCX / nav）\n- [x] 支持 `![title](book.epub#chapter=2)` 与 `![[book.epub#chapter=2]]`\n- [ ] 完成灰度上线与压测报告归档\n"
		if err := writeNative(root(a), "架构评审纪要.md", noteMD); err != nil {
			t.Fatal(err)
		}

		a.refreshTree()
		a.refreshSearch()
		return a, tt
	}

	t.Run("epub-standalone-light", func(t *testing.T) {
		a, tt := setupBookSpace(t, false)
		a.open("ArcMD架构指南.epub")
		tt.Frame()
		a.doc.book.SidebarOpen = true
		a.doc.book.GoToChapter(1, true)
		for range 4 {
			tt.Frame()
			tt.Advance(120 * time.Millisecond)
		}
		savePNG(t, filepath.Join(dir, "book-epub-standalone-light.png"), tt)
	})

	t.Run("epub-standalone-dark-search", func(t *testing.T) {
		a, tt := setupBookSpace(t, true)
		a.open("ArcMD架构指南.epub")
		tt.Frame()
		a.doc.book.SidebarOpen = true
		a.doc.book.FindText("一致性哈希")
		for range 4 {
			tt.Frame()
			tt.Advance(120 * time.Millisecond)
		}
		savePNG(t, filepath.Join(dir, "book-epub-standalone-dark-search.png"), tt)
	})

	t.Run("html-standalone-light", func(t *testing.T) {
		a, tt := setupBookSpace(t, false)
		a.open("原生HTML阅读规范.html")
		tt.Frame()
		a.doc.book.SidebarOpen = true
		for range 4 {
			tt.Frame()
			tt.Advance(120 * time.Millisecond)
		}
		savePNG(t, filepath.Join(dir, "book-html-standalone-light.png"), tt)
	})

	t.Run("split-markdown-epub", func(t *testing.T) {
		a, tt := setupBookSpace(t, false)
		a.open("架构评审纪要.md")
		tt.Frame()
		a.openBookFrom(a.doc, "ArcMD架构指南.epub#chapter=2", true)
		settle(tt)
		for range 4 {
			tt.Frame()
			tt.Advance(120 * time.Millisecond)
		}
		savePNG(t, filepath.Join(dir, "book-split-markdown-epub.png"), tt)
	})

	t.Run("inline-browsing-markdown", func(t *testing.T) {
		a, tt := setupBookSpace(t, false)
		a.open("架构评审纪要.md")
		tt.Frame()
		for _, blk := range a.doc.ed.Doc.Blocks {
			if editor.IsBookURL(blk.URL) {
				a.doc.ed.BrowseBookBlock(blk.ID)
				break
			}
		}
		for range 4 {
			tt.Frame()
			tt.Advance(120 * time.Millisecond)
		}
		savePNG(t, filepath.Join(dir, "book-inline-browsing-markdown.png"), tt)
	})
}

type stubAppHTMLWebView struct {
	path              string
	rev               uint64
	anchor            string
	scrollToContainer bool
	destroyed         bool
}

func (s *stubAppHTMLWebView) Native() ui.NativeWebView                         { return nil }
func (s *stubAppHTMLWebView) ScrollToAnchor(anchor string, headingIndex int)   { s.anchor = anchor }
func (s *stubAppHTMLWebView) Find(string, bool, bool, func(active, total int)) {}
func (s *stubAppHTMLWebView) StopFind()                                        {}
func (s *stubAppHTMLWebView) Reload()                                          {}
func (s *stubAppHTMLWebView) Destroy()                                         { s.destroyed = true }

func TestAppHTMLWebViewHookAndPathSafety(t *testing.T) {
	a, tt := newTestApp(t)
	r := root(a)
	if err := os.MkdirAll(filepath.Join(r, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	htmlPath := filepath.Join(r, "guide.html")
	if err := os.WriteFile(htmlPath, buildSampleHTML(), 0o644); err != nil {
		t.Fatal(err)
	}
	embedPath := filepath.Join(r, "assets", "embed.html")
	if err := os.WriteFile(embedPath, buildSampleHTML(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeNative(r, "Note.md", "# Note\n\n![说明](assets/embed.html#details)\n"); err != nil {
		t.Fatal(err)
	}

	var calls []stubAppHTMLWebView
	a.makeHTMLWebView = func(_ *mygo.Window, fullPath string, rev uint64, anchor string, scrollToContainer bool) editor.HTMLWebView {
		stub := &stubAppHTMLWebView{
			path:              fullPath,
			rev:               rev,
			anchor:            anchor,
			scrollToContainer: scrollToContainer,
		}
		calls = append(calls, *stub)
		return stub
	}

	a.refreshTree()
	a.refreshSearch()
	settle(tt)

	// Standalone HTML triggers WebView acquisition with scrollToContainer=false.
	a.open("guide.html")
	tt.Frame()
	if !slices.ContainsFunc(calls, func(s stubAppHTMLWebView) bool {
		return filepath.Clean(s.path) == filepath.Clean(htmlPath) && !s.scrollToContainer
	}) {
		t.Fatalf("expected standalone HTML to request WebView(scrollToContainer=false), got calls=%+v", calls)
	}

	// Source mode hides live WebView cleanly.
	calls = nil
	a.doc.book.ToggleSource()
	tt.Frame()
	if len(calls) != 0 {
		t.Fatalf("expected source view not to request WebView, got %+v", calls)
	}

	// Inline HTML embed in Markdown note passes #details and scrollToContainer=true in rest mode.
	calls = nil
	a.open("Note.md")
	tt.Frame()
	if !slices.ContainsFunc(calls, func(s stubAppHTMLWebView) bool {
		return filepath.Clean(s.path) == filepath.Clean(embedPath) && s.anchor == "details" && s.scrollToContainer
	}) {
		t.Fatalf("expected inline HTML embed to request WebView(anchor=details, scrollToContainer=true), got calls=%+v", calls)
	}

	// Local navigation allow-list blocks parent/external directories while allowing siblings and subdirs.
	allowDir := filepath.Dir(embedPath)
	allowEval := allowDir
	if eval, err := filepath.EvalSymlinks(allowDir); err == nil {
		allowEval = eval
	}
	h := &htmlWebViewHandle{allowDir: allowDir, allowEval: allowEval}
	if !h.allowsLocalFile(embedPath) || !h.allowsLocalFile(filepath.Join(allowDir, "sub", "chart.html")) {
		t.Fatal("allowsLocalFile rejected same-dir or subdirectory file")
	}
	if h.allowsLocalFile(filepath.Join(r, "guide.html")) || h.allowsLocalFile("/etc/passwd") {
		t.Fatal("allowsLocalFile allowed path outside document directory")
	}
}

func TestBookCornerVerticalAlignment(t *testing.T) {
	a, tt := newTestApp(t)
	if err := os.WriteFile(filepath.Join(root(a), "guide.epub"), buildSampleEPUB(t), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("guide.epub")
	settle(tt)

	badgeRect, ok := tt.Find("第 1 / 3 章")
	if !ok {
		t.Fatalf("missing EPUB chapter badge in toolbar; texts=%q", tt.Texts())
	}
	outlineBtn, ok := tt.Access("目录")
	if !ok {
		t.Fatal("missing EPUB outline toolbar button")
	}
	addrBox, ok := tt.Access("文档路径")
	if !ok {
		t.Fatal("missing address bar")
	}

	badgeMid := badgeRect.Y + badgeRect.H/2
	btnMid := outlineBtn.Bounds.Y + outlineBtn.Bounds.H/2
	addrMid := addrBox.Bounds.Y + addrBox.Bounds.H/2
	if math.Abs(float64(badgeMid-btnMid)) > 2 {
		t.Fatalf("EPUB chapter badge vertical midpoint %.2f differs from outline button %.2f (badge=%+v, btn=%+v)", badgeMid, btnMid, badgeRect, outlineBtn.Bounds)
	}
	if math.Abs(float64(badgeMid-addrMid)) > 2 {
		t.Fatalf("EPUB chapter badge vertical midpoint %.2f differs from address bar %.2f (badge=%+v, addr=%+v)", badgeMid, addrMid, badgeRect, addrBox.Bounds)
	}
}
