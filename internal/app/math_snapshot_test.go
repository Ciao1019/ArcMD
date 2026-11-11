package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

func TestMathSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render formulas")
	for _, name := range []string{"reading", "editing", "editing-dark", "editing-narrow", "error", "empty"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			source := `\int_0^\infty x^2 e^{-x} dx = 2`
			if name == "error" {
				source = `\frac{a}`
			}
			if name == "empty" {
				source = ""
			}
			doc := editor.ImportMarkdown("# 数学笔记\n\n从一个积分开始。\n\n$$\n" + source + "\n$$\n\n公式与文字一起，留在纸面上。\n\n$$\ne^{ix} = \\cos x + i \\sin x\n$$\n\n$$\n\\frac{a+b}{2} + \\sqrt{x^2+y^2}\n$$")
			encoded, err := doc.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if err := vault.Write(root(a), "Math.arcmd", encoded); err != nil {
				t.Fatal(err)
			}
			a.refreshTree()
			a.open("Math.arcmd")
			tt.Frame()
			if strings.Contains(name, "dark") {
				tt.SetDark(true)
			}
			if strings.Contains(name, "narrow") {
				tt.SetSize(540, 780)
			}
			if name != "reading" {
				focusDocument(a, tt)
				for _, leaf := range a.doc.ed.Doc.Leaves() {
					if leaf.Block.Kind == "math" {
						a.doc.ed.SetSelection(leaf.End, leaf.End)
						break
					}
				}
			}
			for range 3 {
				tt.Frame()
				tt.Advance(200 * time.Millisecond)
			}
			tt.Frame()
			savePNG(t, filepath.Join(dir, "math-"+name+".png"), tt)
		})
	}
}
