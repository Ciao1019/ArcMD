package app

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/agent"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func buildRichSamplePDF(t testing.TB) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := pdf.NewWriter(&buf, "ArcMD 系统架构蓝皮书")
	w.ReservePages(3)

	// Page 1: Architecture Cover & Executive Summary (A4 Portrait)
	vp1 := w.NewVectorPage(pdf.A4Width, pdf.A4Height)
	vp1.FillRect(0, 0, pdf.A4Width, pdf.A4Height, 0, [4]uint8{255, 255, 255, 255})
	vp1.FillRect(36, 36, pdf.A4Width-72, 92, 10, [4]uint8{32, 68, 168, 255})
	vp1.DrawText("ARCMD TECHNICAL BLUEPRINT · 2026", 58, 68, 0, 10, pdf.TextStyle{Family: pdf.FontSans, Bold: true}, [4]uint8{195, 214, 255, 255})
	vp1.DrawText("第一章 统一渲染与虚拟滚动架构", 58, 102, 0, 20, pdf.TextStyle{Family: pdf.FontSans, Bold: true}, [4]uint8{255, 255, 255, 255})

	vp1.DrawText("1.1 设计目标与核心原则", 48, 170, 0, 14, pdf.TextStyle{Family: pdf.FontSans, Bold: true}, [4]uint8{24, 28, 36, 255})
	vp1.DrawText("纯 Go 零 CGO 原生客户端，兼顾独立 PDF 深度阅读与 Markdown 内联翻页。", 48, 198, 0, 11.5, pdf.TextStyle{Family: pdf.FontSans}, [4]uint8{62, 68, 80, 255})
	vp1.DrawText("默认卡片态零滚动陷阱（Zero Scroll-Trap），支持一键分屏与全屏沉浸阅读。", 48, 220, 0, 11.5, pdf.TextStyle{Family: pdf.FontSans}, [4]uint8{62, 68, 80, 255})

	// Diagram cards on Page 1
	cards := []struct {
		x     float64
		title string
		sub   string
		fill  [4]uint8
		bd    [4]uint8
	}{
		{48, "PDFKit / Poppler / WinRT", "原生矢量栅格化 · 零 CGO", [4]uint8{239, 246, 255, 255}, [4]uint8{59, 130, 246, 255}},
		{218, "多级 LRU 位图池", "渐进式缩放 · 自动释放内存", [4]uint8{240, 253, 244, 255}, [4]uint8{34, 197, 94, 255}},
		{388, "双向分屏与 AI 引用", "页码锚点同步 · 精准跳转", [4]uint8{255, 247, 237, 255}, [4]uint8{249, 115, 22, 255}},
	}
	for _, c := range cards {
		vp1.FillRect(c.x, 258, 158, 86, 8, c.fill)
		vp1.StrokeRect(c.x, 258, 158, 86, 8, 1.2, c.bd, false)
		vp1.DrawText(c.title, c.x+12, 286, 0, 10.5, pdf.TextStyle{Family: pdf.FontSans, Bold: true}, [4]uint8{24, 28, 36, 255})
		vp1.DrawText(c.sub, c.x+12, 312, 0, 9.5, pdf.TextStyle{Family: pdf.FontSans}, [4]uint8{75, 85, 99, 255})
	}

	vp1.DrawText("1.2 关键性能指标（P99 帧耗时）", 48, 388, 0, 14, pdf.TextStyle{Family: pdf.FontSans, Bold: true}, [4]uint8{24, 28, 36, 255})
	vp1.FillRect(48, 408, pdf.A4Width-96, 112, 8, [4]uint8{248, 250, 252, 255})
	vp1.StrokeRect(48, 408, pdf.A4Width-96, 112, 8, 1, [4]uint8{226, 232, 240, 255}, false)
	vp1.DrawText("• 500 页技术手册打开耗时 < 6 ms（按需虚拟化布局，零全量解码）", 64, 436, 0, 11, pdf.TextStyle{Family: pdf.FontSans}, [4]uint8{45, 55, 72, 255})
	vp1.DrawText("• 连续滚动稳定 120 fps，仅渲染视口及前后一屏预取缓冲页", 64, 464, 0, 11, pdf.TextStyle{Family: pdf.FontSans}, [4]uint8{45, 55, 72, 255})
	vp1.DrawText("• 侧边栏悬停预览与全文检索自动提取文本及章节书签", 64, 492, 0, 11, pdf.TextStyle{Family: pdf.FontSans}, [4]uint8{45, 55, 72, 255})
	if err := w.FinishVectorPage(vp1); err != nil {
		t.Fatalf("FinishVectorPage 1: %v", err)
	}

	// Page 2: Wide Slide Deck Page (16:9 Presentation Slide, 720x405)
	vp2 := w.NewVectorPage(720, 405)
	vp2.FillRect(0, 0, 720, 405, 0, [4]uint8{252, 253, 255, 255})
	vp2.FillRect(0, 0, 720, 68, 0, [4]uint8{18, 118, 110, 255})
	vp2.DrawText("第二章 多级缓存与分屏阅读体验 · Slide 02", 36, 42, 0, 18, pdf.TextStyle{Family: pdf.FontSans, Bold: true}, [4]uint8{255, 255, 255, 255})
	vp2.DrawText("左侧编写结构化笔记，右侧同步对照 PDF 指定页码（#page=2）", 36, 108, 0, 13, pdf.TextStyle{Family: pdf.FontSans}, [4]uint8{30, 41, 59, 255})

	bars := []struct {
		label string
		pct   float64
		col   [4]uint8
	}{
		{"冷启动首屏渲染 (4.2 ms)", 0.88, [4]uint8{16, 185, 129, 255}},
		{"高倍率无闪烁重采样 (6.8 ms)", 0.74, [4]uint8{59, 130, 246, 255}},
		{"全文检索高亮定位 (2.1 ms)", 0.94, [4]uint8{139, 92, 246, 255}},
	}
	for i, bar := range bars {
		y := 148.0 + float64(i)*68.0
		vp2.DrawText(bar.label, 36, y, 0, 11.5, pdf.TextStyle{Family: pdf.FontSans, Bold: true}, [4]uint8{51, 65, 85, 255})
		vp2.FillRect(36, y+10, 640, 22, 6, [4]uint8{226, 232, 240, 255})
		vp2.FillRect(36, y+10, 640*bar.pct, 22, 6, bar.col)
	}
	if err := w.FinishVectorPage(vp2); err != nil {
		t.Fatalf("FinishVectorPage 2: %v", err)
	}

	// Page 3: Disaster Recovery & Deployment (A4 Portrait)
	vp3 := w.NewVectorPage(pdf.A4Width, pdf.A4Height)
	vp3.FillRect(0, 0, pdf.A4Width, pdf.A4Height, 0, [4]uint8{255, 255, 255, 255})
	vp3.FillRect(36, 36, pdf.A4Width-72, 74, 10, [4]uint8{180, 83, 9, 255})
	vp3.DrawText("第三章 灰度容灾与跨平台校验", 56, 80, 0, 18, pdf.TextStyle{Family: pdf.FontSans, Bold: true}, [4]uint8{255, 255, 255, 255})
	vp3.DrawText("关键结论：灰度容灾演练通过率 100%，回滚窗口控制在 15 秒以内。", 48, 152, 0, 12, pdf.TextStyle{Family: pdf.FontSans}, [4]uint8{30, 41, 59, 255})
	if err := w.FinishVectorPage(vp3); err != nil {
		t.Fatalf("FinishVectorPage 3: %v", err)
	}

	w.SetOutlines([]pdf.OutlineItem{
		{Title: "第一章 统一渲染与虚拟滚动", Page: 0, Y: 48, Level: 1},
		{Title: "1.1 设计目标与原则", Page: 0, Y: 160, Level: 2},
		{Title: "1.2 关键性能指标", Page: 0, Y: 380, Level: 2},
		{Title: "第二章 多级缓存与分屏阅读", Page: 1, Y: 36, Level: 1},
		{Title: "第三章 灰度容灾与跨平台校验", Page: 2, Y: 48, Level: 1},
	})
	if err := w.Close(); err != nil {
		t.Fatalf("Writer.Close: %v", err)
	}
	return buf.Bytes()
}

func TestStandalonePDFOpenSearchAndQA(t *testing.T) {
	a, tt := newTestApp(t)
	pdfBytes := buildRichSamplePDF(t)
	pdfDiskPath := filepath.Join(root(a), "系统架构蓝皮书.pdf")
	if err := os.WriteFile(pdfDiskPath, pdfBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.refreshSearch()
	tt.Frame()

	// Sidebar shows the standalone PDF document without extension; clicking opens PDFView.
	if err := tt.Click("系统架构蓝皮书"); err != nil {
		t.Fatalf("Click PDF in sidebar: %v", err)
	}
	tt.Frame()
	if a.doc == nil || !a.doc.isPDF() || a.doc.rel != "系统架构蓝皮书.pdf" {
		t.Fatalf("expected standalone PDF open, got %+v", a.doc)
	}
	if !tt.HasText("共 3 页") || !tt.HasText("1 / 3") {
		t.Fatalf("expected PDF page indicators '共 3 页' and '1 / 3', got %v", tt.Texts())
	}

	// Read-only guarantee: advancing past autosave never marks dirty or writes history.
	tt.Advance(2 * autosaveDelay)
	tt.Frame()
	if a.doc.dirty() {
		t.Fatal("standalone PDF should never become dirty")
	}
	if _, err := os.Stat(filepath.Join(root(a), ".arcmd", "history")); err == nil {
		t.Fatal("opening a read-only PDF should not create .arcmd/history")
	}

	// Toggle outline/thumbnail sidebar with ⇧⌘O.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyO)
	tt.Frame()
	if !a.doc.pdf.SidebarOpen || !tt.HasText("第二章 多级缓存与分屏阅读") {
		t.Fatalf("expected ⇧⌘O to open PDF outline sidebar, open=%v", a.doc.pdf.SidebarOpen)
	}

	// Full-text search indexes PDF text and openAtText jumps directly to the matching PDF page.
	a.open("Welcome.md")
	tt.Frame()
	spID := a.state.ActiveSpace().ID
	hits := a.textHits("灰度容灾")
	if len(hits) == 0 || hits[0].Path != "系统架构蓝皮书.pdf" {
		t.Fatalf("expected full-text search hit in 系统架构蓝皮书.pdf, got %+v", hits)
	}
	a.openAtText(spID, "系统架构蓝皮书.pdf", "灰度容灾")
	tt.Advance(350 * time.Millisecond)
	tt.Frame()
	if !a.doc.isPDF() || !a.doc.pdf.FindOpen || a.doc.pdf.CurrentPage != 2 {
		t.Fatalf("expected openAtText to open PDF at page 3 (idx 2) with FindOpen, got page=%d find=%v", a.doc.pdf.CurrentPage, a.doc.pdf.FindOpen)
	}

	// Space AI QA reads PDF pages with [第 N 页] markers and citation clicks jump to the cited page.
	qaFS := a.qaSpace(spID)
	page, err := qaFS.Page("系统架构蓝皮书.pdf")
	if err != nil || len(page.Lines) == 0 {
		t.Fatalf("qaSpace.Page failed for PDF: page=%+v err=%v", page, err)
	}
	p2Line := 0
	for i, ln := range page.Lines {
		if strings.Contains(ln, "## Page 2") {
			p2Line = i + 1
			break
		}
	}
	if p2Line == 0 {
		t.Fatalf("expected ## Page 2 heading in QA projection, got %v", page.Lines)
	}
	a.openCitation(spID, agent.Citation{Path: "系统架构蓝皮书.pdf", From: p2Line, To: p2Line})
	tt.Advance(350 * time.Millisecond)
	tt.Frame()
	if !a.doc.isPDF() || a.doc.pdf.CurrentPage != 1 {
		t.Fatalf("expected QA citation click to jump PDF to page 2 (idx 1), got %d", a.doc.pdf.CurrentPage)
	}
}

func TestDropPDFIntoMarkdownRelocateRenameAndSplit(t *testing.T) {
	a, tt := dropApp(t)
	pdfBytes := buildRichSamplePDF(t)
	extPDF := filepath.Join(t.TempDir(), "Quarterly Slides.pdf")
	if err := os.WriteFile(extPDF, pdfBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// Drop external PDF from Finder onto "beta" paragraph: copies to assets/ and embeds in one undo step.
	dropAt(t, a, tt, "beta", extPDF)
	want := []string{"Drop", "alpha", "image:assets/Quarterly-Slides.pdf", "beta", "gamma"}
	if got := blockList(a); !slices.Equal(got, want) {
		t.Fatalf("blocks after PDF drop = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(root(a), "assets", "Quarterly-Slides.pdf")); err != nil {
		t.Fatalf("dropped PDF was not copied to assets/: %v", err)
	}

	// Single-step Undo & Redo.
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	if got := blockList(a); slices.Contains(got, "image:assets/Quarterly-Slides.pdf") {
		t.Fatalf("Cmd+Z did not undo PDF drop: %q", got)
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	tt.Frame()
	if got := blockList(a); !slices.Equal(got, want) {
		t.Fatalf("Shift+Cmd+Z did not redo PDF drop: %q", got)
	}

	// Pin #page=2 on a Markdown note, move the note into Journal/, and verify asset + #page=2 relocate cleanly.
	noteMD := "# 架构周会\n\n![季度汇报](assets/Quarterly-Slides.pdf#page=2)\n"
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
	if !strings.Contains(movedRaw, "#page=2") {
		t.Fatalf("moving Markdown document lost #page=2 fragment: %q", movedRaw)
	}
	if _, err := os.Stat(filepath.Join(root(a), "Journal", "assets", "Quarterly-Slides.pdf")); err != nil {
		t.Fatalf("moving Markdown document did not relocate PDF asset into Journal/assets/: %v", err)
	}

	// Space-root standalone PDF: renaming it updates Markdown embeds while preserving #page=3.
	if err := os.WriteFile(filepath.Join(root(a), "蓝皮书.pdf"), pdfBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeNative(root(a), "阅读索引.md", "# 阅读索引\n\n详见 [[蓝皮书]]：\n\n![蓝皮书](蓝皮书.pdf#page=3)\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("阅读索引.md")
	tt.Frame()

	a.startRename("蓝皮书.pdf")
	a.newName = "系统蓝皮书"
	a.commitRename()
	saveSettle(tt)

	relinkedRaw, err := vault.Read(root(a), "阅读索引.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(relinkedRaw, "[[系统蓝皮书]]") || !strings.Contains(relinkedRaw, "系统蓝皮书.pdf#page=3") {
		t.Fatalf("renaming standalone PDF did not relink Markdown wiki & embed with #page=3: %q", relinkedRaw)
	}

	// Clicking "在分屏中阅读" on the inline PDF opens a side-by-side Split Pane jumped to page 3 (idx 2).
	a.open("阅读索引.md")
	tt.Frame()
	a.openPDFFrom(a.doc, "系统蓝皮书.pdf#page=3", true)
	settle(tt)
	tt.Advance(350 * time.Millisecond)
	tt.Frame()
	if a.split == nil || len(a.split.panes) != 2 {
		t.Fatalf("expected Split View with 2 panes, got %+v", a.split)
	}
	rightDoc := a.split.panes[1].doc
	if rightDoc == nil || !rightDoc.isPDF() || rightDoc.rel != "系统蓝皮书.pdf" || rightDoc.pdf.CurrentPage != 2 {
		t.Fatalf("expected right split pane on 系统蓝皮书.pdf page 3 (idx 2), got doc=%+v", rightDoc)
	}
}

func TestPDFViewerSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render PDF viewer snapshots")
	pdfBytes := buildRichSamplePDF(t)

	setupPDFSpace := func(t *testing.T, dark bool) (*App, *ui.Tester) {
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

		if err := os.WriteFile(filepath.Join(root(a), "系统架构蓝皮书.pdf"), pdfBytes, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root(a), "assets"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root(a), "assets", "系统架构蓝皮书.pdf"), pdfBytes, 0o644); err != nil {
			t.Fatal(err)
		}

		noteMD := "# 架构评审纪要 Q4\n\n> [!IMPORTANT]\n> 完整技术细节请参阅 [[系统架构蓝皮书]]，以下内联嵌入了第 2 页关键性能对比，不会抢占正文滚动，也可一键分屏对照阅读。\n\n![系统架构蓝皮书](assets/系统架构蓝皮书.pdf#page=2)\n\n## 评审结论与后续动作\n\n- [x] 原生无 CGO 多平台 PDF 渲染链路验证通过\n- [x] 支持 `![title](doc.pdf#page=2)` 与 `![[doc.pdf#page=2]]` 双语法\n- [ ] 推进灰度发布与压测报告归档\n"
		if err := writeNative(root(a), "架构评审纪要.md", noteMD); err != nil {
			t.Fatal(err)
		}

		a.refreshTree()
		a.refreshSearch()
		return a, tt
	}

	t.Run("standalone-light", func(t *testing.T) {
		a, tt := setupPDFSpace(t, false)
		a.open("系统架构蓝皮书.pdf")
		tt.Frame()
		a.doc.pdf.SidebarOpen = true
		a.doc.pdf.SidebarTab = 1 // Outline tab
		for range 4 {
			tt.Frame()
			tt.Advance(120 * time.Millisecond)
		}
		savePNG(t, filepath.Join(dir, "pdf-standalone-light.png"), tt)
	})

	t.Run("standalone-dark-thumbs-search", func(t *testing.T) {
		a, tt := setupPDFSpace(t, true)
		a.open("系统架构蓝皮书.pdf")
		tt.Frame()
		a.doc.pdf.SidebarOpen = true
		a.doc.pdf.SidebarTab = 0 // Thumbnails tab
		a.doc.pdf.FindText("虚拟滚动")
		for range 4 {
			tt.Frame()
			tt.Advance(120 * time.Millisecond)
		}
		savePNG(t, filepath.Join(dir, "pdf-standalone-dark-search.png"), tt)
	})

	t.Run("split-markdown-pdf", func(t *testing.T) {
		a, tt := setupPDFSpace(t, false)
		a.open("架构评审纪要.md")
		tt.Frame()
		a.openPDFFrom(a.doc, "系统架构蓝皮书.pdf#page=2", true)
		settle(tt)
		for range 4 {
			tt.Frame()
			tt.Advance(120 * time.Millisecond)
		}
		savePNG(t, filepath.Join(dir, "pdf-split-markdown.png"), tt)
	})

	t.Run("inline-browsing-markdown", func(t *testing.T) {
		a, tt := setupPDFSpace(t, false)
		a.open("架构评审纪要.md")
		tt.Frame()
		for _, blk := range a.doc.ed.Doc.Blocks {
			if editor.IsPDFURL(blk.URL) {
				a.doc.ed.BrowsePDFBlock(blk.ID)
				break
			}
		}
		for range 4 {
			tt.Frame()
			tt.Advance(120 * time.Millisecond)
		}
		savePNG(t, filepath.Join(dir, "pdf-inline-browsing.png"), tt)
	})
}
