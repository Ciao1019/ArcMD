package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestColumnsCommandStartsTyping(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Type("插入双栏")
	tt.Key(0, ui.KeyEnter)
	tt.Type("左边直接输入")
	tt.Key(0, ui.KeyTab)
	tt.Type("右边直接输入")
	if a.palette.open || !tt.Focused("文档") {
		t.Fatal("column command did not return focus to the document")
	}
	for _, b := range a.doc.ed.Doc.Blocks {
		if b.Kind == "columns" {
			if b.Children[0].Children[0].Text() != "左边直接输入" || b.Children[1].Children[0].Text() != "右边直接输入" {
				t.Fatal("typing after the command did not reach the columns")
			}
			return
		}
	}
	t.Fatal("command did not insert columns")
}

func TestColumnsSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render columns")
	for _, name := range []string{"empty", "empty-dark", "empty-narrow", "typing", "typing-dark"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			doc := editor.ImportMarkdown("# 并排记录\n\n")
			encoded, err := doc.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if err := vault.Write(root(a), "分栏.arcmd", encoded); err != nil {
				t.Fatal(err)
			}
			a.refreshTree()
			a.open("分栏.arcmd")
			tt.SetSize(1240, 760)
			if strings.HasSuffix(name, "dark") {
				tt.SetDark(true)
			}
			if name == "empty-narrow" {
				tt.SetSize(540, 650)
			}
			tt.Frame()
			tt.Advance(time.Second)
			if err := tt.Click("插入分栏"); err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(name, "typing") {
				tt.Type("啊")
			}
			tt.Move(1100, 720)
			tt.Frame()
			savePNG(t, filepath.Join(dir, "columns-"+name+".png"), tt)
		})
	}
}
