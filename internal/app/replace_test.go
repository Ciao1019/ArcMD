package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestReplaceFold(t *testing.T) {
	if s, n := replaceFold("Go go GO 狗", "go", "Rust"); s != "Rust Rust Rust 狗" || n != 3 {
		t.Fatal(s, n)
	}
	if s, n := replaceFold("abc", "x", "y"); s != "abc" || n != 0 {
		t.Fatal(s, n)
	}
}

func TestReplaceAcrossDocumentsAndUndo(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	for name, md := range map[string]string{"A.md": "# 甲\n\n旧名字出现一次", "Journal/B.md": "# 乙\n\n**旧名字**和旧名字"} {
		if err := vault.Write(root(a), name, md); err != nil {
			t.Fatal(err)
		}
	}
	if err := vault.Write(root(a), "Broken.arcmd", `{"format":"arcmd","version":99,"blocks":[]}`); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("A.md")
	a.refreshSearch()
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyR)
	tt.Frame()
	if !a.replace.open {
		t.Fatal("⇧⌘R opens the window")
	}
	a.replace.find, a.replace.with = "旧名字", "新名字"
	tt.Frame()
	if !tt.HasText("2 篇文档，共 3 处") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("全部替换"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !strings.Contains(a.doc.ed.Text(), "新名字出现一次") {
		t.Fatal("the open document", a.doc.ed.Text())
	}
	b, _ := vault.Read(root(a), "Journal/B.md")
	if strings.Contains(b, "旧名字") || !strings.Contains(b, "**新名字**") {
		t.Fatal("the other document keeps its marks", b)
	}
	if !tt.HasText("已在 2 篇文档中替换 3 处") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("撤销"); err != nil {
		t.Fatal(err)
	}
	b, _ = vault.Read(root(a), "Journal/B.md")
	if !strings.Contains(b, "**旧名字**") || !strings.Contains(a.doc.ed.Text(), "旧名字出现一次") {
		t.Fatal("undo puts both back")
	}
}

func TestExportHTML(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	a.exportHTML()
	a.exportHTML()
	page, err := os.ReadFile(filepath.Join(root(a), "Welcome.html"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	if !strings.HasPrefix(s, "<!doctype html>") || !strings.Contains(s, "<title>Welcome</title>") || !strings.Contains(s, "<h1>Welcome to ArcMD</h1>") {
		t.Fatal(s[:200])
	}
	if _, err := os.Stat(filepath.Join(root(a), "Welcome 2.html")); err != nil {
		t.Fatal("a second export is numbered, never replacing the first")
	}
}
