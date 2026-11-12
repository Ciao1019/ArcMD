package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

func TestSelectionScrollSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render cross-screen selection")
	for _, dark := range []bool{false, true} {
		a, tt := newTestApp(t)
		var text strings.Builder
		text.WriteString("# 跨屏选择\n\n## 连续选中内容\n\n")
		for i := 1; i <= 60; i++ {
			fmt.Fprintf(&text, "第 %d 段：按住鼠标拖选，同时滚动页面，选区跟随指针延伸。松开后页面保持当前位置，可以复制跨屏选中的文字。\n\n", i)
		}
		encoded, err := editor.ImportMarkdown(text.String()).Encode()
		if err != nil {
			t.Fatal(err)
		}
		if err := vault.Write(root(a), "Selection.arcmd", encoded); err != nil {
			t.Fatal(err)
		}
		a.refreshTree()
		a.open("Selection.arcmd")
		tt.SetSize(1240, 940)
		tt.SetDark(dark)
		tt.Frame()
		surface, ok := tt.Find("文档")
		if !ok {
			t.Fatal("missing writing surface")
		}
		x, y := surface.X+320, surface.Y+400
		tt.Press(x, surface.Y+220)
		tt.Move(x, y)
		tt.Scroll(x, y, 0, 900)
		selection, scroll := a.doc.ed.Selection().Range, a.doc.ed.Scroll.Y
		if selection.Start == selection.End || scroll != 900 {
			t.Fatal("cross-screen selection failed")
		}
		tt.Release(x, y)
		if a.doc.ed.Selection().Range != selection || a.doc.ed.Scroll.Y != scroll {
			t.Fatal("release changed selection or viewport")
		}
		name := "selection-scroll.png"
		if dark {
			name = "selection-scroll-dark.png"
		}
		savePNG(t, filepath.Join(dir, name), tt)
	}
}
