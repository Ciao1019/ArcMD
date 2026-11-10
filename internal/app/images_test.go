package app

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// parentImagesApp reproduces opening an article whose pictures live beside
// its folder, outside the Space created for the article.
func parentImagesApp(t *testing.T) (*App, *ui.Tester, string, string) {
	t.Helper()
	a, tt := newTestApp(t)
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 480, 160))
	for y := 0; y < 160; y++ {
		for x := 0; x < 480; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(50 + x/4), uint8(80 + y/2), 170, 255})
		}
	}
	for _, name := range []string{"png/window.png", "gif/demo.gif", "png/中文 截图.png"} {
		var data bytes.Buffer
		var err error
		if strings.HasSuffix(name, ".gif") {
			err = gif.Encode(&data, img, nil)
		} else {
			err = png.Encode(&data, img)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := vault.Write(dir, "images/"+name, data.String()); err != nil {
			t.Fatal(err)
		}
	}
	text := "# 本地图片\n\n![桌面窗口](../images/png/window.png)\n\n图片后的正文。\n\n![演示](../images/gif/demo.gif)\n\n![中文文件名](../images/png/%E4%B8%AD%E6%96%87%20%E6%88%AA%E5%9B%BE.png)\n"
	if err := vault.Write(dir, "articles/launch.md", text); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "articles", "launch.md")
	if !a.openSystemFile(file) {
		t.Fatal("open external Markdown")
	}
	return a, tt, file, text
}

func TestSystemOpenLoadsParentImages(t *testing.T) {
	a, tt, file, original := parentImagesApp(t)
	check := func() {
		t.Helper()
		count := 0
		for _, leaf := range a.doc.ed.Doc.Leaves() {
			if leaf.Block.Kind != "image" {
				continue
			}
			count++
			bitmap := a.doc.loadImage(leaf.Block.URL)
			if bitmap == nil {
				t.Fatalf("image did not load: %s", leaf.Block.URL)
			}
			if w, h := bitmap.Size(); w != 480 || h != 160 {
				t.Fatalf("wrong image dimensions: %dx%d", w, h)
			}
		}
		if count != 3 {
			t.Fatalf("imported %d images", count)
		}
	}
	check()
	tt.Frame()
	if _, err := vault.Resolve(a.doc.space.Root, "../images/png/window.png"); err == nil {
		t.Fatal("vault write boundary was relaxed")
	}
	// A real edit converts Markdown beside the source; reopening must retain
	// the same image base, without changing the imported Markdown.
	a.doc.ed.SetSelection(a.doc.ed.Len(), a.doc.ed.Len())
	focusDocument(a, tt)
	tt.Type("正文")
	if !a.flush() || vault.IsMarkdown(a.doc.rel) {
		t.Fatal("save edited import")
	}
	text, err := vault.Read(a.doc.space.Root, a.doc.rel)
	if err != nil {
		t.Fatal(err)
	}
	a.doc = openDocument(a.doc.space, a.doc.rel, text, false)
	check()
	if data, err := os.ReadFile(file); err != nil || string(data) != original {
		t.Fatal("opening or editing changed the Markdown source", err)
	}
}

func TestImageLoaderRejectsUnsupportedReferences(t *testing.T) {
	a, _, _, _ := parentImagesApp(t)
	for _, url := range []string{"", "https://example.com/image.png", "file:///tmp/image.png", "/tmp/image.png", "//server/image.png", "C:/image.png", "%2Ftmp/image.png", "C%3A/image.png", "..%5Cimages%5Cimage.png", "bad%zz.png", "../images/missing.png"} {
		if a.doc.loadImage(url) != nil {
			t.Errorf("unexpected image for %q", url)
		}
	}
}

func TestParentImagesSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render imported images")
	for _, mode := range []string{"light", "dark", "narrow-missing"} {
		t.Run(mode, func(t *testing.T) {
			a, tt, _, _ := parentImagesApp(t)
			tt.SetDark(mode == "dark")
			tt.SetSize(1200, 940)
			tt.Advance(time.Second)
			if mode == "narrow-missing" {
				a.doc = openDocument(a.doc.space, a.doc.rel, "# 图片占位\n\n![一段较长的图片说明，用来检查窄窗口里换行后的间距是否正确](../images/missing/long-picture-name.png)\n\n这段正文应当与图片提示分开显示。\n", false)
				tt.SetSize(640, 650)
			}
			savePNG(t, filepath.Join(dir, "import-images-"+mode+".png"), tt)
		})
	}
}

func TestImageComponentSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the image component")
	for _, mode := range []string{"light", "dark", "narrow", "preview"} {
		t.Run(mode, func(t *testing.T) {
			a, tt, _, _ := parentImagesApp(t)
			if err := vault.Write(a.doc.space.Root, "assets/app-window.png", "broken picture"); err != nil {
				t.Fatal(err)
			}
			text := "# 图片浏览\n\n![▲ Sylva 桌面端：点开引用，右侧打开原件并高亮引文](assets/app-window.png)\n\nSylva 是一款桌面文档库。图片加载失败时，可以重新选择图片，或在文件夹中定位原文件。\n\n"
			if err := writeNative(a.doc.space.Root, "Images.arcmd", text); err != nil {
				t.Fatal(err)
			}
			a.open("Images.arcmd")
			a.doc.ed.SetSelection(a.doc.ed.Len(), a.doc.ed.Len())
			focusDocument(a, tt)
			data, err := os.ReadFile(filepath.Join(a.doc.space.Root, "..", "images", "png", "window.png"))
			if err != nil {
				t.Fatal(err)
			}
			tt.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, data))))
			tt.Key(ui.Cmd, ui.KeyV)
			tt.SetDark(mode == "dark")
			tt.SetSize(1200, 900)
			if mode == "narrow" {
				tt.SetSize(640, 800)
			}
			tt.Advance(time.Second)
			tt.ClickAt(50, 700)
			if mode == "preview" {
				if err := tt.Click("查看图片：粘贴的图片.png"); err != nil {
					t.Fatal(err)
				}
				tt.Key(0, ui.KeyEnter)
			}
			savePNG(t, filepath.Join(dir, "image-component-"+mode+".png"), tt)
		})
	}
}
