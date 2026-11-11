package app

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestBookmarksSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render bookmarks")
	source := "# 读书笔记\n\n## 第一章\n\n这一段写得最好，回头还要再看一遍。\n\n普通的段落，没有书签。\n\n## 第二章\n\n| 指标 | 数值 |\n| --- | --- |\n| 速度 | 12 |\n\n结尾的总结。"
	for _, name := range []string{"page", "dark", "flash", "list", "menu", "hover"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetSize(1180, 760)
			if name == "dark" {
				tt.SetDark(true)
			}
			tt.Frame()
			ids := bookmarkDoc(t, a, tt, "读书笔记.arcmd", source)
			focusDocument(a, tt)
			for _, i := range []int{2, 5} {
				caretOn(a, ids[i])
				tt.Key(ui.Cmd, ui.KeyD)
			}
			a.labelBookmark(a.doc.space.ID, a.doc.rel, ids[5], "速度表")
			bookmarkDoc(t, a, tt, "Other.arcmd", "# 另一篇\n\nSomewhere")
			focusDocument(a, tt)
			caretOn(a, a.doc.ed.Doc.Blocks[1].ID)
			tt.Key(ui.Cmd, ui.KeyD)
			a.open("读书笔记.arcmd")
			tt.Frame()
			switch name {
			case "flash":
				caretOn(a, ids[0])
				tt.Key(ui.Ctrl|ui.Cmd, ui.KeyDown)
				tt.Frame()
				tt.Advance(100 * time.Millisecond)
			case "list":
				tt.Key(ui.Cmd|ui.Shift, ui.KeyD)
			case "hover":
				// The pointer on the plain paragraph shows its hollow mark.
				r, _ := tt.Find("文档")
				for _, l := range a.doc.ed.Doc.Leaves() {
					if l.Block.ID == ids[3] {
						b, _, _ := a.doc.ed.BoundsForRange(ui.TextInputRange{Start: l.Start, End: l.Start})
						tt.Move(r.X+b.X+40, r.Y+b.Y+b.H/2-a.doc.ed.Scroll.Y)
					}
				}
			case "menu":
				a.doc.ed.OpenBlockMenu(ids[2], "")
			}
			tt.Frame()
			f, err := os.Create(filepath.Join(dir, "bookmarks-"+name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err := png.Encode(f, tt.Image()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFormulaBookmarksSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render formula bookmarks")
	source := "# 数学笔记\n\n公式书签保留公式的排版。\n\n$$\n" + bookmarkIntegral +
		"\n$$\n\n$$\n\\frac{a+b}{2} + \\sqrt{x^2+y^2}\n$$\n\n$$\n" + strings.Repeat("x+", 60) +
		"\\frac{a}{b}\n$$\n\n$$\n\\begin{pmatrix}a & b \\\\ c & d\\end{pmatrix}\n$$\n\n$$\n\\frac{a}\n$$\n\n$$\n\n$$"
	for _, mode := range []string{"light", "dark", "narrow", "hover"} {
		t.Run(mode, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetSize(1180, 760)
			tt.SetDark(mode == "dark")
			tt.Frame()
			ids := bookmarkDoc(t, a, tt, "Math.arcmd", source)
			for _, id := range ids[2:] {
				a.toggleBookmark(a.doc, id)
			}
			caretOn(a, ids[0])
			tt.Frame()
			tt.Advance(300 * time.Millisecond)
			if mode == "narrow" {
				tt.SetSize(540, 760)
				tt.Frame()
				if err := tt.Click("展开目录"); err != nil {
					t.Fatal(err)
				}
				tt.Advance(300 * time.Millisecond)
			}
			if mode == "hover" {
				r, _ := tt.Find("跳到书签 " + bookmarkIntegral)
				tt.Move(r.X+r.W/2, r.Y+r.H/2)
			}
			tt.Frame()
			savePNG(t, filepath.Join(dir, "bookmark-formulas-"+mode+".png"), tt)
		})
	}
}
