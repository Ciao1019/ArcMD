package app

import (
	"github.com/egoist/mygo/ui"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func finishTestPDF(t *testing.T, a *App, tt *ui.Tester) {
	t.Helper()
	for i := 0; i < 500 && a.pdfExport != nil; i++ {
		tt.Advance(2 * time.Millisecond)
		tt.Frame()
	}
	if a.pdfExport != nil {
		t.Fatal("PDF export did not finish")
	}
}

func TestPDFCancelCleansPartialOutput(t *testing.T) {
	a, tt := newTestApp(t)
	if err := writeNative(root(a), "Long.arcmd", "# Long\n\n"+strings.Repeat("Long paragraph for export.\n\n", 200)); err != nil {
		t.Fatal(err)
	}
	a.open("Long.arcmd")
	a.exportPDF()
	file := a.pdfExport.file.Name()
	tt.Advance(2 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("正在导出 PDF") {
		t.Fatal("progress missing")
	}
	if err := tt.Click("取消导出"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.pdfExport != nil {
		t.Fatal("job remains after cancellation")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("partial file remains")
	}
	if _, err := os.Stat(filepath.Join(root(a), "Long.pdf")); !os.IsNotExist(err) {
		t.Fatal("cancel published a PDF")
	}
}

func TestPDFTextLayer(t *testing.T) {
	a, tt := newTestApp(t)
	if err := writeNative(root(a), "Searchable.arcmd", "# 搜索与复制\n\n中文 English 123 🚀\n\n| 名称 | 内容 |\n| --- | --- |\n| 表格 | 可选择 |\n"); err != nil {
		t.Fatal(err)
	}
	a.open("Searchable.arcmd")
	a.exportPDF()
	finishTestPDF(t, a, tt)
	data, err := os.ReadFile(filepath.Join(root(a), "Searchable.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "/ToUnicode") || !strings.Contains(s, "/Subtype /Type3") || !strings.Contains(s, "/Filter /FlateDecode") {
		t.Fatal("expected vector Type 3 Unicode text and Flate stream")
	}
	if strings.Contains(s, "/DCTDecode") {
		t.Fatal("unexpected raster JPEG page stream in vector PDF export")
	}
	if dir := os.Getenv("ARCMD_PDF_REVIEW"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "searchable.pdf"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVectorPDFOutlinesLinksMathAndMermaid(t *testing.T) {
	a, tt := newTestApp(t)
	md := `# 架构总览

参考 [外部链接](https://example.com/spec) 以及 [跳到核心流程](#核心流程)。

## 核心流程

| 模块 | 说明 |
| --- | --- |
| PDF | 纯 Go 矢量导出 |

` + "```go\nfunc main() { println(\"ok\") }\n```\n\nInline bold symbol $\\boldsymbol{\\mu}$ in prose.\n\n$$\n\\boldsymbol{\\sum}_{i=1}^{n} \\boldsymbol{z}_i + z_i\n$$\n\n```mermaid\nflowchart LR\n  A[输入] --> B(排版)\n```\n"
	if err := writeNative(root(a), "VectorSpec.arcmd", md); err != nil {
		t.Fatal(err)
	}
	a.open("VectorSpec.arcmd")
	a.exportPDF()
	finishTestPDF(t, a, tt)

	data, err := os.ReadFile(filepath.Join(root(a), "VectorSpec.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{
		"/Outlines",
		"/Subtype /Link",
		"/URI (https://example.com/spec)",
		"/Dest [",
		"/Subtype /Type3",
		"/ToUnicode",
		"/Filter /FlateDecode",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("vector PDF missing %q", want)
		}
	}
	if strings.Contains(s, "/DCTDecode") {
		t.Fatal("unexpected raster JPEG stream in vector PDF without opaque photos")
	}
}

func TestPDFProgressSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render export progress")
	a, tt := newTestApp(t)
	a.exportPDF()
	t.Cleanup(a.cancelPDF)
	tt.Advance(2 * time.Millisecond)
	tt.Frame()
	savePNG(t, filepath.Join(dir, "pdf-export-progress.png"), tt)
}
