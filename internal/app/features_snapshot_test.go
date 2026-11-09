package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

const linksSample = "# 旅行计划\n\n这次去 [[Ideas]] 里提到的海边，顺便整理 [[Welcome|欢迎页]]。#旅行 #计划/2026\n\n## 预算\n\n每天的花费大约是：\n\n$$\n\\sum_{i=1}^{n} c_i = \\frac{a+b}{2} + \\sqrt{x^2+y^2}\n$$\n\n## 公式\n\n$$\nf(x) = \\begin{cases} x^2 & x > 0 \\\\ 0 & \\text{否则} \\end{cases}\n$$\n\n$$\n\\lim_{x \\to 0} \\frac{\\sin x}{x} = 1, \\quad \\int_0^1 e^{-x}\\,dx, \\quad \\begin{pmatrix} a & b \\\\ c & d \\end{pmatrix}\n$$\n"

// TestFeaturesSnapshot renders the new features for review.
func TestFeaturesSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the features")
	for _, name := range []string{"links-math", "links-math-dark", "wiki-complete", "focus", "versions", "trash", "shortcuts", "backlinks", "pdf-page"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			if name == "links-math-dark" {
				tt.SetDark(true)
			}
			doc := editor.ImportMarkdown(linksSample)
			doc.Icon, doc.Cover = "🏖️", "gradient:sea"
			text, err := doc.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if err := vault.Write(root(a), "旅行计划.arcmd", text); err != nil {
				t.Fatal(err)
			}
			a.refreshTree()
			a.refreshSearch()
			a.open("旅行计划.arcmd")
			switch name {
			case "wiki-complete":
				a.doc.ed.Engine.InsertAt(len(a.doc.ed.Doc.Blocks), model.NewBlock("paragraph", ""))
				caretAtEnd(a, tt)
				tt.Type("下一站 [[")
			case "focus":
				a.prefs.Typewriter = true
				a.toggleFocus()
				focusDocument(a, tt)
				a.doc.ed.SetSelection(30, 30)
				a.doc.ed.RequestFocus = true
			case "versions":
				caretAtEnd(a, tt)
				tt.Type("再加一句。")
				saveSettle(tt)
				tt.Type("又改了一处。")
				saveSettle(tt)
				a.openVersions()
				tt.Frame()
				a.selectVersion(1)
			case "trash":
				a.trashFile = os.RemoveAll
				a.trash("Journal")
				a.openTrash()
			case "shortcuts":
				a.openSettings(tabShortcuts)
			case "backlinks":
				a.open("Journal/Ideas.arcmd")
			case "pdf-page":
				ed := editor.FromDocument(a.doc.ed.Committed())
				theme := documentTheme(false)
				o := editor.Options{Width: paperWidth, Theme: theme, Page: pageStyle(false), LoadImage: a.doc.loadImage}
				body := float32(paperHeight - 2*paperMargin)
				var tops []float32
				ui.Render(func(c *ui.Context) { tops = ed.Paginate(o, body) }, paperWidth, paperHeight, 1)
				img := ui.Render(func(c *ui.Context) {
					ui.Box(c).Fill().Background(theme.Background).Draw(func(p *ui.Painter, r ui.Rect) {
						ed.PaintPage(p, ui.Rect{X: r.X, Y: r.Y + paperMargin, W: r.W, H: body}, tops[0], tops[0]+body, theme)
					})
				}, paperWidth, paperHeight, 1)
				f, err := os.Create(filepath.Join(dir, "feature-pdf-page.png"))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				png.Encode(f, img)
				return
			}
			for range 4 {
				tt.Frame()
				tt.Advance(200 * time.Millisecond)
			}
			tt.Frame()
			savePNG(t, filepath.Join(dir, "feature-"+name+".png"), tt)
		})
	}
}
