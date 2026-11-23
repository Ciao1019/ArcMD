package bookdoc

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func buildZipBytes(t testing.TB, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func tinyPNG(t testing.TB, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestEPUB3NavCoverAssetsLinksAndFind(t *testing.T) {
	coverPNG := tinyPNG(t, color.NRGBA{R: 40, G: 110, B: 220, A: 255})
	figPNG := tinyPNG(t, color.NRGBA{R: 220, G: 80, B: 60, A: 255})

	data := buildZipBytes(t, map[string][]byte{
		"mimetype": []byte("application/epub+zip"),
		"META-INF/container.xml": []byte(`<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/package.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`),
		"OEBPS/package.opf": []byte(`<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="uid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>原生阅读指南</dc:title>
    <dc:creator>ArcMD</dc:creator>
    <dc:language>zh-CN</dc:language>
    <dc:description>纯 Go EPUB 3 测试样本</dc:description>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="cover-img" href="Images/cover.png" media-type="image/png" properties="cover-image"/>
    <item id="fig1" href="Images/fig%201.png" media-type="image/png"/>
    <item id="ch1" href="Text/ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="ch2" href="Text/ch2.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>
    <itemref idref="ch1"/>
    <itemref idref="ch2"/>
  </spine>
</package>`),
		"OEBPS/nav.xhtml": []byte(`<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<body>
  <nav epub:type="toc">
    <ol>
      <li><a href="Text/ch1.xhtml">第一章 起步</a>
        <ol><li><a href="Text/ch1.xhtml#sec-arch">1.1 核心架构</a></li></ol>
      </li>
      <li><a href="Text/ch2.xhtml#sec-sync">第二章 同步与引用</a></li>
    </ol>
  </nav>
</body>
</html>`),
		"OEBPS/Text/ch1.xhtml": []byte(`<html><body>
<h1>第一章 起步</h1>
<p>ArcMD 采用纯 Go 渲染，无需 WebView。</p>
<h2 id="sec-arch">1.1 核心架构</h2>
<p>跳转到 <a href="ch2.xhtml#sec-sync">第二章</a> 或 <a href="https://arcmd.org">官网</a>。</p>
<p><img src="../Images/fig%201.png" alt="架构图"/></p>
</body></html>`),
		"OEBPS/Text/ch2.xhtml": []byte(`<html><body>
<h1 id="sec-sync">第二章 同步与引用</h1>
<p>支持章节内查找与搜索定位。查找关键词：纯 Go 与延迟测量。</p>
</body></html>`),
		"OEBPS/Images/cover.png": coverPNG,
		"OEBPS/Images/fig 1.png": figPNG,
	})

	epPath := filepath.Join(t.TempDir(), "guide.epub")
	if err := os.WriteFile(epPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := Open(epPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer doc.Close()

	if doc.Kind() != KindEPUB || doc.Title() != "原生阅读指南" || doc.Author() != "ArcMD" || doc.ChapterCount() != 2 {
		t.Fatalf("unexpected metadata: kind=%v title=%q author=%q chapters=%d", doc.Kind(), doc.Title(), doc.Author(), doc.ChapterCount())
	}
	toc := doc.TOC()
	if len(toc) != 2 || len(toc[0].Children) != 1 || toc[0].Children[0].Anchor != "sec-arch" || toc[1].Chapter != 1 || toc[1].Anchor != "sec-sync" {
		t.Fatalf("unexpected TOC: %+v", toc)
	}
	cov, mimeType, ok := doc.CoverImage()
	if !ok || !bytes.Equal(cov, coverPNG) || mimeType != "image/png" {
		t.Fatalf("CoverImage ok=%v mime=%q len=%d", ok, mimeType, len(cov))
	}
	asset, _, key, ok := doc.ReadAsset(0, "../Images/fig%201.png?v=1#top")
	if !ok || !bytes.Equal(asset, figPNG) || key != "epub:OEBPS/Images/fig 1.png" {
		t.Fatalf("ReadAsset ok=%v key=%q len=%d", ok, key, len(asset))
	}
	if _, _, _, ok := doc.ReadAsset(0, "../../../../etc/passwd"); ok {
		t.Fatal("expected traversal path to be rejected")
	}

	if ch, anc, ext, ok := doc.ResolveLink(0, "ch2.xhtml#sec-sync"); !ok || ch != 1 || anc != "sec-sync" || ext != "" {
		t.Fatalf("ResolveLink cross-chapter = (%d, %q, %q, %v)", ch, anc, ext, ok)
	}
	if ch, anc, ext, ok := doc.ResolveLink(0, "#sec-arch"); !ok || ch != 0 || anc != "sec-arch" || ext != "" {
		t.Fatalf("ResolveLink intra-chapter = (%d, %q, %q, %v)", ch, anc, ext, ok)
	}
	if ch, _, ext, ok := doc.ResolveLink(0, "https://arcmd.org"); !ok || ch != -1 || ext != "https://arcmd.org" {
		t.Fatalf("ResolveLink external = (%d, %q, %v)", ch, ext, ok)
	}
	if _, _, _, ok := doc.ResolveLink(0, "javascript:alert(1)"); ok {
		t.Fatal("expected javascript: URI to be rejected")
	}

	matches := doc.Find("纯 Go")
	if len(matches) != 2 || matches[0].Chapter != 0 || matches[0].Occurrence != 0 || matches[1].Chapter != 1 || matches[1].Occurrence != 0 {
		t.Fatalf("Find(纯 Go) = %+v", matches)
	}
	head, paras := doc.ChapterPreview(0, 3)
	if head != "第一章 起步" || len(paras) == 0 {
		t.Fatalf("ChapterPreview = (%q, %+v)", head, paras)
	}
	if !strings.Contains(doc.PlainText(10), "延迟测量") {
		t.Fatalf("PlainText missing chapter 2 text: %q", doc.PlainText(10))
	}
}

func TestEPUB2NCXFontObfuscationAndDRMCheck(t *testing.T) {
	coverJPG := tinyPNG(t, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	baseFiles := map[string][]byte{
		"META-INF/container.xml": []byte(`<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="content.opf"/></rootfiles></container>`),
		"content.opf": []byte(`<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>EPUB2 Book</dc:title>
    <meta name="cover" content="cimg"/>
  </metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="cimg" href="cover.png" media-type="image/png"/>
    <item id="p1" href="ch1.html" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="p1"/></spine>
</package>`),
		"toc.ncx": []byte(`<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/">
  <navMap>
    <navPoint id="n1" playOrder="1"><navLabel><text>Intro</text></navLabel><content src="ch1.html#top"/></navPoint>
  </navMap>
</ncx>`),
		"ch1.html":  []byte(`<html id="top"><body><p>Hello EPUB 2</p></body></html>`),
		"cover.png": coverJPG,
		"font.ttf":  []byte("obfuscated-font-bytes"),
	}

	// Font obfuscation MUST NOT trigger ErrEncrypted.
	baseFiles["META-INF/encryption.xml"] = []byte(`<?xml version="1.0"?>
<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container" xmlns:enc="http://www.w3.org/2001/04/xmlenc#">
  <enc:EncryptedData>
    <enc:EncryptionMethod Algorithm="http://www.idpf.org/2008/embedding"/>
    <enc:CipherData><enc:CipherReference URI="font.ttf"/></enc:CipherData>
  </enc:EncryptedData>
  <enc:EncryptedData>
    <enc:EncryptionMethod Algorithm="http://ns.adobe.com/pdf/enc#RC"/>
    <enc:CipherData><enc:CipherReference URI="font2.otf"/></enc:CipherData>
  </enc:EncryptedData>
</encryption>`)
	doc, err := OpenBytes("book.epub", buildZipBytes(t, baseFiles))
	if err != nil {
		t.Fatalf("font-obfuscated EPUB failed: %v", err)
	}
	if len(doc.TOC()) != 1 || doc.TOC()[0].Title != "Intro" || doc.TOC()[0].Anchor != "top" {
		t.Fatalf("unexpected NCX TOC: %+v", doc.TOC())
	}
	if _, _, ok := doc.CoverImage(); !ok {
		t.Fatal("expected <meta name=cover> image to load")
	}
	doc.Close()

	// Real DRM encryption on spine chapter MUST return ErrEncrypted.
	baseFiles["META-INF/encryption.xml"] = []byte(`<?xml version="1.0"?>
<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container" xmlns:enc="http://www.w3.org/2001/04/xmlenc#">
  <enc:EncryptedData>
    <enc:EncryptionMethod Algorithm="http://www.w3.org/2001/04/xmlenc#aes128-cbc"/>
    <enc:CipherData><enc:CipherReference URI="ch1.html"/></enc:CipherData>
  </enc:EncryptedData>
</encryption>`)
	if _, err := OpenBytes("drm.epub", buildZipBytes(t, baseFiles)); !errors.Is(err, ErrEncrypted) {
		t.Fatalf("expected ErrEncrypted for DRM spine chapter, got %v", err)
	}
}

func TestHTMLEncodingsOutlineDataURIAndMalformed(t *testing.T) {
	pngBytes := tinyPNG(t, color.NRGBA{R: 12, G: 200, B: 120, A: 255})
	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)
	utf8HTML := `<!doctype html>
<html lang="zh-CN">
<head>
  <title>技术白皮书</title>
  <meta name="author" content="Prism">
  <meta name="description" content="说明摘要">
</head>
<body>
  <h1 id="overview">总览</h1>
  <p>第一段正文。<img src="` + dataURI + `" alt="logo"></p>
  <h2>子章节无锚点</h2>
  <p style="display: none">隐藏噪声</p>
  <script>console.log("ignore-me")</script>
</body>
</html>`
	doc, err := OpenBytes("whitepaper.html", []byte(utf8HTML))
	if err != nil {
		t.Fatalf("OpenBytes HTML: %v", err)
	}
	defer doc.Close()
	if doc.Kind() != KindHTML || doc.Title() != "技术白皮书" || doc.Author() != "Prism" || doc.Description() != "说明摘要" {
		t.Fatalf("HTML meta mismatch: title=%q author=%q desc=%q", doc.Title(), doc.Author(), doc.Description())
	}
	toc := doc.TOC()
	if len(toc) != 1 || toc[0].Anchor != "overview" || len(toc[0].Children) != 1 || toc[0].Children[0].Title != "子章节无锚点" || toc[0].Children[0].HeadingIndex != 1 {
		t.Fatalf("unexpected HTML TOC: %+v", toc)
	}
	asset, mt, key, ok := doc.ReadAsset(0, dataURI)
	if !ok || mt != "image/png" || !bytes.Equal(asset, pngBytes) || !strings.HasPrefix(key, "data:") {
		t.Fatalf("data URI decode failed: ok=%v mt=%q len=%d key=%q", ok, mt, len(asset), key)
	}
	txt := doc.ChapterText(0)
	if !strings.Contains(txt, "第一段正文") || strings.Contains(txt, "隐藏噪声") || strings.Contains(txt, "ignore-me") {
		t.Fatalf("unexpected HTML plain text: %q", txt)
	}

	// Unlabeled GB18030 HTML bytes should decode cleanly to UTF-8.
	gbkRaw, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("<html><head><title>中文编码测试</title></head><body><p>这是无标签 GBK 网页正文。</p></body></html>"))
	if err != nil {
		t.Fatal(err)
	}
	gbkDoc, err := OpenBytes("legacy.htm", gbkRaw)
	if err != nil {
		t.Fatalf("OpenBytes GBK HTML: %v", err)
	}
	if gbkDoc.Title() != "中文编码测试" || !strings.Contains(gbkDoc.ChapterText(0), "这是无标签 GBK 网页正文") {
		t.Fatalf("GBK fallback failed: title=%q text=%q", gbkDoc.Title(), gbkDoc.ChapterText(0))
	}

	// Corrupted/empty inputs fail cleanly without panicking.
	if _, err := OpenBytes("bad.epub", []byte("not-a-zip")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid on corrupt epub, got %v", err)
	}
	if _, err := OpenBytes("empty.html", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid on empty html, got %v", err)
	}
}
