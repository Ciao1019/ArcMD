package app

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestReadingFeaturesSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render reading features")
	source := "# 让想法自然流动\n\n句子里的公式 $x^2 + y^2 = r^2$ 和文字共享基线，水写作 H<sub>2</sub>O，面积写作 m<sup>2</sup>。\n\n$$\n\\int_0^\\infty x^2 e^{-x} dx = 2\n$$\n\n结论来自这条脚注[^proof]，也可以打开 [参考资料](https://example.com)。\n\n中文输入支持「包裹选区」，配对与撤销保持自然。\n\n[^proof]: 这是可以编辑的脚注正文。点击编号跳转，再点击返回箭头回到引用处。"
	for _, name := range []string{"light", "dark", "narrow", "link", "inline-source", "read-only", "reading-settings"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			doc := editor.ImportMarkdown(source)
			data, err := doc.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if err := vault.Write(root(a), "Reading.arcmd", data); err != nil {
				t.Fatal(err)
			}
			a.refreshTree()
			a.open("Reading.arcmd")
			tt.SetSize(1120, 780)
			if name == "dark" {
				tt.SetDark(true)
			}
			if name == "narrow" {
				tt.SetSize(540, 780)
			}
			tt.Frame()
			if name == "link" {
				for _, l := range a.doc.ed.Doc.Leaves() {
					if i := strings.Index(l.Block.Text(), "参考资料"); i >= 0 {
						at := l.Start + len([]rune(l.Block.Text()[:i])) + 1
						a.doc.ed.SetSelection(at, at)
						a.doc.ed.EditLink()
						break
					}
				}
			}
			if name == "inline-source" {
				focusDocument(a, tt)
				for _, l := range a.doc.ed.Doc.Leaves() {
					if i := strings.Index(l.Block.Text(), "$x"); i >= 0 {
						at := l.Start + len([]rune(l.Block.Text()[:i])) + 2
						a.doc.ed.SetSelection(at, at)
						break
					}
				}
			}
			if name == "read-only" {
				a.doc.ed.SetReadOnly(true)
				a.prefs.FontSize = 20
				a.prefs.LineHeight = 1.8
			}
			if name == "reading-settings" {
				a.openSettings(tabGeneral)
			}
			for range 3 {
				tt.Frame()
				tt.Advance(200 * time.Millisecond)
			}
			tt.Frame()
			if name == "reading-settings" {
				tt.Scroll(820, 560, 0, 270)
				tt.Frame()
			}
			savePNG(t, filepath.Join(dir, "reading-"+name+".png"), tt)
		})
	}
}

func TestReadingImageSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render image controls")
	for _, name := range []string{"handles", "preview", "narrow"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			fixture := image.NewRGBA(image.Rect(0, 0, 800, 400))
			for y := 0; y < 400; y++ {
				for x := 0; x < 800; x++ {
					shade := color.RGBA{uint8(35 + x/10), uint8(85 + y/5), uint8(130 + x/12), 255}
					if (x-560)*(x-560)+(y-140)*(y-140) < 70*70 {
						shade = color.RGBA{230, 205, 160, 255}
					}
					fixture.SetRGBA(x, y, shade)
				}
			}
			file, err := os.Create(filepath.Join(root(a), "scene.png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(file, fixture)
			file.Close()
			if err != nil {
				t.Fatal(err)
			}
			doc := editor.ImportMarkdown("# 图片与文字\n\n拖动边缘调整大小，双击图片查看细节。\n\n![风景](scene.png)\n\n图片尺寸保存在文档里，撤销可以恢复。")
			doc.Blocks[2].ImageWidth = 560
			data, _ := doc.Encode()
			if err := vault.Write(root(a), "Images.arcmd", data); err != nil {
				t.Fatal(err)
			}
			a.refreshTree()
			a.open("Images.arcmd")
			tt.SetSize(1120, 780)
			if name == "narrow" {
				tt.SetSize(540, 780)
			}
			a.doc.ed.FocusBlock(a.doc.ed.Doc.Blocks[2].ID)
			tt.Frame()
			tt.Frame()
			if name == "preview" {
				if err := tt.Click("查看图片：scene.png"); err != nil {
					t.Fatal(err)
				}
				tt.Key(0, ui.KeyEnter)
				tt.Click("放大图片")
			}
			for range 3 {
				tt.Frame()
				tt.Advance(200 * time.Millisecond)
			}
			tt.Frame()
			savePNG(t, filepath.Join(dir, "reading-image-"+name+".png"), tt)
		})
	}
}
