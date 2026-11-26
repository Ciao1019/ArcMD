package app

import (
	"archive/zip"
	"bytes"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/mdz"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
)

func buildTestProjectMDZ(t testing.TB) []byte {
	t.Helper()
	banner := makeBookSnapshotPNG(t, 480, 180, color.RGBA{37, 99, 235, 255}, color.RGBA{239, 246, 255, 255}, color.RGBA{96, 165, 250, 255})
	diagram := makeBookSnapshotPNG(t, 360, 160, color.RGBA{16, 138, 92, 255}, color.RGBA{240, 253, 244, 255}, color.RGBA{52, 211, 153, 255})
	data, err := mdz.Build(mdz.WriteOptions{
		Manifest: mdz.Manifest{
			Title:       "ArcMD 架构手册",
			Mode:        mdz.ModeProject,
			EntryPoint:  "index.md",
			Language:    "zh-CN",
			Description: "多页只读项目归档",
			Cover:       "images/banner.png",
		},
		Files: []mdz.File{
			{
				Path: "index.md",
				Data: []byte("# ArcMD 架构手册\n\n欢迎阅读项目总览。前往 [快速开始](guide/setup.md) 或查看下方示意图。\n\n![架构图](images/diagram.png)\n"),
			},
			{
				Path: "guide/setup.md",
				Data: []byte("# 快速开始\n\n安装并运行原生应用。下一步查看 [API 参考](api.md#auth)。\n\n![流程图](../images/diagram.png)\n"),
			},
			{
				Path: "guide/api.md",
				Data: []byte("# API 参考\n\n核心调用说明。\n\n## Auth\n\n使用本地凭据隔离策略（MDZSearchToken99）。\n"),
			},
			{Path: "images/banner.png", Data: banner},
			{Path: "images/diagram.png", Data: diagram},
		},
	})
	if err != nil {
		t.Fatalf("mdz.Build project: %v", err)
	}
	return data
}

func makeCustomZIPForTest(t testing.TB, entries map[string][]byte, encrypted bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range entries {
		hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if encrypted {
			hdr.Flags |= 0x1
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("CreateHeader(%s): %v", name, err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatalf("Write(%s): %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close zip: %v", err)
	}
	return buf.Bytes()
}

func TestExportAndOpenMDZRoundTrip(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()

	coverPNG := makeBookSnapshotPNG(t, 320, 120, color.RGBA{45, 95, 220, 255}, color.RGBA{244, 247, 255, 255}, color.RGBA{99, 102, 241, 255})
	diagPNG := makeBookSnapshotPNG(t, 240, 120, color.RGBA{18, 138, 92, 255}, color.RGBA{243, 250, 247, 255}, color.RGBA{56, 189, 148, 255})
	wbScene := whiteboard.NewScene()
	wbSVG, err := encodeWhiteboardFile("Flow.excalidraw.svg", wbScene)
	if err != nil {
		t.Fatalf("encodeWhiteboardFile: %v", err)
	}
	assetsPath := filepath.Join(sp.Root, "assets")
	if err := os.MkdirAll(assetsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsPath, "Cover.png"), coverPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsPath, "Diagram.png"), diagPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsPath, "Flow.excalidraw.svg"), []byte(wbSVG), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsPath, "spec.txt"), []byte("attached spec"), 0o644); err != nil {
		t.Fatal(err)
	}

	mdSource := "---\ncover: assets/Cover.png\n---\n\n# 架构设计\n\n正文段落，带 [附件说明](assets/spec.txt)。\n\n![架构图](assets/Diagram.png)\n\n![白板](assets/Flow.excalidraw.svg)\n"
	if err := os.WriteFile(filepath.Join(sp.Root, "Architecture.md"), []byte(mdSource), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Architecture.md")
	if a.doc == nil || a.doc.rel != "Architecture.md" {
		t.Fatalf("failed to open Architecture.md")
	}
	settle(tt)

	// Export twice to verify auto-numbering never overwrites.
	a.exportMDZ()
	a.exportMDZ()
	mdz1 := filepath.Join(sp.Root, "Architecture.mdz")
	mdz2 := filepath.Join(sp.Root, "Architecture 2.mdz")
	if _, err := os.Stat(mdz1); err != nil {
		t.Fatalf("expected Architecture.mdz: %v", err)
	}
	if _, err := os.Stat(mdz2); err != nil {
		t.Fatalf("expected Architecture 2.mdz: %v", err)
	}

	// Verify archive contents conform to MDZip v1.2.0 and kept original assets/ paths intact.
	arc, err := mdz.Open(mdz1)
	if err != nil {
		t.Fatalf("mdz.Open(exported): %v", err)
	}
	if arc.EntryPoint() != "index.md" || arc.Mode() != mdz.ModeDocument || arc.CoverPath() != "assets/Cover.png" {
		t.Fatalf("unexpected exported manifest state: ep=%q mode=%q cover=%q", arc.EntryPoint(), arc.Mode(), arc.CoverPath())
	}
	for _, want := range []string{"index.md", "assets/Cover.png", "assets/Diagram.png", "assets/Flow.excalidraw.svg", "assets/spec.txt"} {
		if !arc.HasFile(want) {
			t.Fatalf("exported archive missing %q; files=%v", want, arc.Files())
		}
	}
	indexText, err := arc.ReadMarkdown("index.md")
	if err != nil || !strings.Contains(indexText, "assets/Diagram.png") {
		t.Fatalf("expected original relative path preserved in index.md, got %q (err=%v)", indexText, err)
	}
	_ = arc.Close()

	// Open exported .mdz inside ArcMD and verify read-only rendering + in-memory asset decode.
	a.open("Architecture.mdz")
	settle(tt)
	if a.doc == nil || !a.doc.isMDZ() || !a.doc.mdz.hasActivePage() {
		t.Fatalf("expected active MDZ document")
	}
	if a.doc.dirty() {
		t.Fatalf("MDZ document should never be dirty")
	}
	if ed := a.doc.mdz.activeEditor(); ed == nil || !ed.ReadOnly {
		t.Fatalf("MDZ active editor must be ReadOnly")
	}
	imgRes := a.mdzLoadImage(a.doc, "assets/Diagram.png")
	if imgRes.Bitmap == nil || imgRes.Problem != 0 {
		t.Fatalf("expected in-memory image bitmap, got %+v", imgRes)
	}
	if sc, _, wbErr := a.mdzLoadWhiteboard(a.doc, "assets/Flow.excalidraw.svg"); wbErr != nil || sc == nil {
		t.Fatalf("expected in-memory whiteboard scene, got err=%v", wbErr)
	}
	if !strings.Contains(a.doc.mdz.activeEditor().Text(), "架构设计") || !tt.HasText("MDZ") || !tt.HasText("解包另存为文档") {
		t.Fatalf("expected rendered MDZ content and controls in UI")
	}
}

func TestExportMDZCustomAttachmentsAndMissingTolerance(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	sp.AttachmentsDir = "media"
	if err := os.MkdirAll(filepath.Join(sp.Root, "media"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sp.Root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	pngBytes := makeBookSnapshotPNG(t, 120, 80, color.RGBA{200, 50, 50, 255}, color.RGBA{255, 245, 245, 255}, color.RGBA{240, 120, 120, 255})
	if err := os.WriteFile(filepath.Join(sp.Root, "media", "Chart Photo.png"), pngBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	md := "# 报告\n\n![图表](../media/Chart%20Photo.png)\n\n![缺失](assets/gone.png)\n"
	if err := os.WriteFile(filepath.Join(sp.Root, "notes", "Report.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("notes/Report.md")
	if a.doc == nil || a.doc.rel != "notes/Report.md" {
		t.Fatal("open notes/Report.md failed")
	}
	settle(tt)

	a.notices = nil
	a.exportMDZ()
	exported := filepath.Join(sp.Root, "notes", "Report.mdz")
	arc, err := mdz.Open(exported)
	if err != nil {
		t.Fatalf("mdz.Open(Report.mdz): %v", err)
	}
	defer func() { _ = arc.Close() }()

	if !arc.HasFile("assets/Chart Photo.png") {
		t.Fatalf("expected consolidated assets/Chart Photo.png preserving capitalization, got files=%v", arc.Files())
	}
	idx, _ := arc.ReadMarkdown("index.md")
	if !strings.Contains(idx, "assets/Chart%20Photo.png") {
		t.Fatalf("expected rewritten URL in index.md, got %q", idx)
	}
	// Live open document must remain untouched.
	if strings.Contains(a.doc.text(), "assets/Chart%20Photo.png") {
		t.Fatalf("live document should not have been mutated by export")
	}
	if len(a.notices) == 0 || !strings.Contains(a.notices[len(a.notices)-1], "缺失附件") {
		t.Fatalf("expected missing attachment hint in notices, got %v", a.notices)
	}
}

func TestOpenMultiPageProjectMDZAndNavigation(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	if err := os.WriteFile(filepath.Join(sp.Root, "Handbook.mdz"), buildTestProjectMDZ(t), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Handbook.mdz")
	settle(tt)

	if a.doc == nil || !a.doc.isMDZ() || !a.doc.mdz.sidebarOpen {
		t.Fatalf("expected multi-page project MDZ with rail open")
	}
	if !tt.HasText("项目文档") || !tt.HasText("1 / 3 页") || !tt.HasText("setup.md") {
		t.Fatalf("expected project mode rail and page indicator")
	}

	// Follow relative internal link to setup.md then api.md#auth.
	a.mdzOpenLink(a.doc, "guide/setup.md")
	settle(tt)
	if a.doc.mdz.currentPage != "guide/setup.md" {
		t.Fatalf("currentPage = %q, want guide/setup.md", a.doc.mdz.currentPage)
	}
	a.mdzOpenLink(a.doc, "api.md#auth")
	settle(tt)
	if a.doc.mdz.currentPage != "guide/api.md" {
		t.Fatalf("currentPage = %q, want guide/api.md", a.doc.mdz.currentPage)
	}
	if !a.doc.mdz.canBack() || !a.doc.mdz.goBack() || a.doc.mdz.currentPage != "guide/setup.md" {
		t.Fatalf("in-archive back navigation failed: cur=%q", a.doc.mdz.currentPage)
	}

	// Relative link escaping archive root must be blocked per Spec §9.3 / §9.6.
	a.notices = nil
	a.mdzOpenLink(a.doc, "../../outside.md")
	if a.doc.mdz.currentPage != "guide/setup.md" || len(a.notices) == 0 || !strings.Contains(a.notices[len(a.notices)-1], "归档外部") {
		t.Fatalf("expected outside-archive link blocked, cur=%q notices=%v", a.doc.mdz.currentPage, a.notices)
	}
}

func TestMDZDirectLinkClickInView(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1000, 680)
	sp := a.state.ActiveSpace()
	raw, err := mdz.Build(mdz.WriteOptions{
		Manifest: mdz.Manifest{
			Title:      "LinkClickTest",
			Mode:       mdz.ModeProject,
			EntryPoint: "index.md",
		},
		Files: []mdz.File{
			{Path: "index.md", Data: []byte("[ClickToOpenSetupPageLinkText](guide/setup.md)\n")},
			{Path: "guide/setup.md", Data: []byte("[ClickToAttemptEscapeLinkText](../../outside.md)\n")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sp.Root, "LinkClick.mdz"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("LinkClick.mdz")
	settle(tt)

	b, ok := tt.Find("文档")
	if !ok {
		t.Fatal("missing editor surface in MDZ view")
	}
	// Single-click the link on index.md without holding Cmd/Ctrl.
	for dy := float32(28); dy <= 140; dy += 10 {
		tt.ClickAt(b.X+b.W*0.32, b.Y+dy)
		settle(tt)
		if a.doc.mdz.currentPage == "guide/setup.md" {
			break
		}
	}
	if a.doc.mdz.currentPage != "guide/setup.md" {
		t.Fatalf("single-clicking link in MDZ view did not navigate; currentPage = %q", a.doc.mdz.currentPage)
	}

	// Single-click the out-of-archive link on guide/setup.md and verify blocked notice toast.
	b, ok = tt.Find("文档")
	if !ok {
		t.Fatal("missing editor surface after navigation")
	}
	blocked := false
	for dy := float32(28); dy <= 140; dy += 10 {
		tt.ClickAt(b.X+b.W*0.32, b.Y+dy)
		tt.Frame()
		if tt.HasText("该链接指向归档外部文件，已按安全策略拦截。") {
			blocked = true
			break
		}
	}
	if !blocked || a.doc.mdz.currentPage != "guide/setup.md" {
		t.Fatalf("single-clicking outside-archive link did not show blocked notice; blocked=%v page=%q", blocked, a.doc.mdz.currentPage)
	}
}

func TestOpenAmbiguousAndErrorMDZ(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()

	// Ambiguous entry point (no manifest, 2 root markdown files).
	ambig := makeCustomZIPForTest(t, map[string][]byte{
		"alpha.md": []byte("# Alpha Page\n"),
		"beta.md":  []byte("# Beta Page\n"),
	}, false)
	if err := os.WriteFile(filepath.Join(sp.Root, "Ambig.mdz"), ambig, 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Ambig.mdz")
	settle(tt)
	if a.doc == nil || !a.doc.isMDZ() || !errors.Is(a.doc.mdz.loadErr, mdz.ErrEntryPointUnresolved) || a.doc.mdz.hasActivePage() {
		t.Fatalf("expected unresolved entry point picker")
	}
	if !tt.HasText("请选择要打开的文档") || !tt.HasText("beta.md") {
		t.Fatalf("expected picker UI showing beta.md")
	}
	a.doc.mdz.openPage("beta.md", true, "")
	settle(tt)
	if !strings.Contains(a.doc.mdz.activeEditor().Text(), "Beta Page") {
		t.Fatalf("expected Beta Page rendered after picking")
	}

	// Encrypted / invalid / unsupported major / unsupported mode error cards.
	badCases := map[string][]byte{
		"Encrypted.mdz": makeCustomZIPForTest(t, map[string][]byte{"index.md": []byte("# Secret")}, true),
		"BadMajor.mdz":  makeCustomZIPForTest(t, map[string][]byte{"manifest.json": []byte(`{"spec":{"version":"2.0.0"},"entryPoint":"index.md"}`), "index.md": []byte("# Future")}, false),
		"BadMode.mdz":   makeCustomZIPForTest(t, map[string][]byte{"manifest.json": []byte(`{"mode":"slides","entryPoint":"index.md"}`), "index.md": []byte("# Slides")}, false),
		"Corrupt.mdz":   []byte("not-a-zip-at-all"),
	}
	for name, raw := range badCases {
		if err := os.WriteFile(filepath.Join(sp.Root, name), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		a.refreshTree()
		a.open(name)
		settle(tt)
		if !tt.HasText("无法打开 MDZip 文档") {
			t.Fatalf("%s: expected friendly error card", name)
		}
	}
}

func TestUnpackMDZAsEditableDocument(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	if err := os.WriteFile(filepath.Join(sp.Root, "Bundle.mdz"), buildTestProjectMDZ(t), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Bundle.mdz")
	settle(tt)

	a.unpackMDZAsDocument(a.doc)
	settle(tt)
	if a.doc == nil || a.doc.isMDZ() || !strings.HasSuffix(a.doc.rel, ".md") || a.doc.ed.ReadOnly {
		t.Fatalf("expected unpacked editable .md document open, got rel=%q mdz=%v", a.doc.rel, a.doc.isMDZ())
	}
	content := a.doc.text()
	if !strings.Contains(content, "assets/diagram.png") {
		t.Fatalf("expected unpacked image link in .md, got %q", content)
	}
	if _, err := os.Stat(filepath.Join(sp.Root, "assets", "diagram.png")); err != nil {
		t.Fatalf("expected unpacked asset file on disk: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sp.Root, "assets", "banner.png")); err != nil {
		t.Fatalf("expected unpacked cover file on disk: %v", err)
	}
}

func TestMDZSearchQAExternalAndI18n(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	mdzBytes := buildTestProjectMDZ(t)
	if err := os.WriteFile(filepath.Join(sp.Root, "Handbook.mdz"), mdzBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.refreshSearch()

	// Full-text search hits nested page inside .mdz and openAtTextTerm jumps to it.
	hits := a.search.index.Search("MDZSearchToken99", nil, 10)
	if len(hits) == 0 || hits[0].Path != "Handbook.mdz" {
		t.Fatalf("expected search hit inside Handbook.mdz, got %+v", hits)
	}
	a.openAtTextTerm(sp.ID, "Handbook.mdz", "MDZSearchToken99", "MDZSearchToken99")
	settle(tt)
	if !a.doc.isMDZ() || a.doc.mdz.currentPage != "guide/api.md" {
		t.Fatalf("expected openAtTextTerm to jump to guide/api.md, got %q", a.doc.mdz.currentPage)
	}

	// QAPage extracts archive pages.
	page, err := QAPage("Handbook.mdz", string(mdzBytes))
	if err != nil || page == nil || !strings.Contains(strings.Join(page.Lines, "\n"), "MDZSearchToken99") {
		t.Fatalf("QAPage failed for .mdz: err=%v", err)
	}

	// OS file association opens external .mdz cleanly.
	extDir := t.TempDir()
	extFile := filepath.Join(extDir, "ExternalPack.mdz")
	if err := os.WriteFile(extFile, mdzBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if !a.openSystemFile(extFile) {
		t.Fatalf("openSystemFile(.mdz) failed")
	}
	settle(tt)
	if a.doc == nil || !a.doc.isMDZ() || !a.doc.space.IsExternal() {
		t.Fatalf("expected external .mdz opened in External space")
	}

	// English UI strings render cleanly.
	prevLang := i18n.Language()
	defer i18n.SetLanguage(prevLang)
	i18n.SetLanguage("en")
	settle(tt)
	if !tt.HasText("Unpack to Document") {
		t.Fatalf("expected English MDZ corner label")
	}
}

func TestMDZSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render MDZip snapshots")

	// 1. Single-page .mdz light
	t.Run("mdz-single-light", func(t *testing.T) {
		a, tt := newTestApp(t)
		tt.SetSize(1120, 720)
		sp := a.state.ActiveSpace()
		cover := makeBookSnapshotPNG(t, 640, 180, color.RGBA{30, 88, 210, 255}, color.RGBA{238, 245, 255, 255}, color.RGBA{96, 165, 250, 255})
		diag := makeBookSnapshotPNG(t, 440, 180, color.RGBA{16, 138, 92, 255}, color.RGBA{240, 253, 244, 255}, color.RGBA{52, 211, 153, 255})
		raw, err := mdz.Build(mdz.WriteOptions{
			Manifest: mdz.Manifest{
				Title: "系统架构设计",
				Mode:  mdz.ModeDocument,
				Cover: "assets/cover.png",
			},
			Files: []mdz.File{
				{Path: "index.md", Data: []byte("# 系统架构设计\n\n这是一份单文件 `.mdz` 归档，正文与内嵌图片完全在内存中直接渲染，不向磁盘解压临时文件。\n\n![系统架构图](assets/diagram.png)\n\n## 核心特性\n\n- 遵循 MDZip 1.2.0 规范\n- 支持包内图片、白板与 PDF 原地预览\n- 右上角可一键解包另存为可编辑 `.md`\n")},
				{Path: "assets/cover.png", Data: cover},
				{Path: "assets/diagram.png", Data: diag},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sp.Root, "系统架构设计.mdz"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		a.refreshTree()
		a.open("系统架构设计.mdz")
		settle(tt)
		writePNG(t, filepath.Join(dir, "mdz-single-light.png"), tt.Image())
	})

	// 2 & 3. Multi-page project .mdz in light and dark
	for _, dark := range []bool{false, true} {
		name := "mdz-project-light"
		if dark {
			name = "mdz-project-dark"
		}
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetSize(1120, 720)
			tt.SetDark(dark)
			sp := a.state.ActiveSpace()
			if err := os.WriteFile(filepath.Join(sp.Root, "ArcMD 架构手册.mdz"), buildTestProjectMDZ(t), 0o644); err != nil {
				t.Fatal(err)
			}
			a.refreshTree()
			a.open("ArcMD 架构手册.mdz")
			settle(tt)
			writePNG(t, filepath.Join(dir, name+".png"), tt.Image())
		})
	}

	// 4. Ambiguous entry point picker
	t.Run("mdz-picker-light", func(t *testing.T) {
		a, tt := newTestApp(t)
		tt.SetSize(1120, 720)
		sp := a.state.ActiveSpace()
		ambig := makeCustomZIPForTest(t, map[string][]byte{
			"guide/getting-started.md": []byte("# Getting Started\n"),
			"notes/design.md":          []byte("# Design\n"),
			"notes/roadmap.md":         []byte("# Roadmap\n"),
		}, false)
		if err := os.WriteFile(filepath.Join(sp.Root, "未指定入口合集.mdz"), ambig, 0o644); err != nil {
			t.Fatal(err)
		}
		a.refreshTree()
		a.open("未指定入口合集.mdz")
		settle(tt)
		writePNG(t, filepath.Join(dir, "mdz-picker-light.png"), tt.Image())
	})
}
