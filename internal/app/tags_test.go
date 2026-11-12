package app

import (
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

func TestRenameTagEverywhere(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	for name, md := range map[string]string{"A.arcmd": "# 甲\n\n今天 #工作 很忙", "Journal/B.arcmd": "# 乙\n\n#工作/2026 和 #职场\n\n```\n#工作 in code\n```"} {
		doc := editor.ImportMarkdown(md)
		doc.ImportSource = ""
		text, _ := doc.Encode()
		vault.Write(root(a), name, text)
	}
	a.refreshTree()
	a.open("A.arcmd")
	a.refreshSearch()
	tt.Frame()
	a.openPalette()
	a.palette.query = "#工作"
	tt.Frame()
	if err := tt.Click("重命名或合并标签 #工作…"); err != nil {
		t.Fatal(err, tt.Texts())
	}
	tt.Frame()
	a.tagRename.to = "职场"
	tt.Frame()
	if !tt.HasText("已有标签 #职场（1 篇），将合并为一个。") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("重命名"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !strings.Contains(a.doc.ed.Text(), "#职场 很忙") {
		t.Fatal("open document", a.doc.ed.Text())
	}
	b, _ := vault.Read(root(a), "Journal/B.arcmd")
	if !strings.Contains(b, "#职场/2026") || !strings.Contains(b, "#工作 in code") {
		t.Fatal("other document; code unchanged", b)
	}
	if !tt.HasText("已合并到 #职场，共 2 篇文档") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("撤销"); err != nil {
		t.Fatal(err)
	}
	b, _ = vault.Read(root(a), "Journal/B.arcmd")
	if !strings.Contains(b, "#工作/2026") || !strings.Contains(a.doc.ed.Text(), "#工作 很忙") {
		t.Fatal("undo")
	}
}
