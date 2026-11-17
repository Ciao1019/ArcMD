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
// its folder. The article is registered in the built-in external collection.
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
	// A real edit updates the Markdown file in place; reopening must retain
	// the same image base without creating any sidecar .arcmd file.
	a.doc.ed.SetSelection(a.doc.ed.Len(), a.doc.ed.Len())
	focusDocument(a, tt)
	tt.Type("正文")
	if !a.flush() || !vault.IsMarkdown(a.doc.rel) {
		t.Fatal("save edited Markdown in place")
	}
	if _, err := os.Stat(strings.TrimSuffix(file, ".md") + ".arcmd"); !os.IsNotExist(err) {
		t.Fatal("editing Markdown created a .arcmd file")
	}
	text, err := readFile(a.doc.space, a.doc.rel)
	if err != nil {
		t.Fatal(err)
	}
	a.doc = openDocument(a.doc.space, a.doc.rel, text, false)
	check()
	if data, err := os.ReadFile(file); err != nil || string(data) == original || !strings.Contains(string(data), "正文") {
		t.Fatal("editing did not update the Markdown source in place", err)
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
			a, tt, file, _ := parentImagesApp(t)
			root := filepath.Dir(file)
			if err := vault.Write(root, "assets/app-window.png", "broken picture"); err != nil {
				t.Fatal(err)
			}
			text := "# 图片浏览\n\n![▲ Sylva 桌面端：点开引用，右侧打开原件并高亮引文](assets/app-window.png)\n\nSylva 是一款桌面文档库。图片加载失败时，可以重新选择图片，或在文件夹中定位原文件。\n\n"
			if err := writeNative(root, "Images.arcmd", text); err != nil {
				t.Fatal(err)
			}
			a.openSystemFile(filepath.Join(root, "Images.arcmd"))
			a.doc.ed.SetSelection(a.doc.ed.Len(), a.doc.ed.Len())
			focusDocument(a, tt)
			data, err := os.ReadFile(filepath.Join(root, "..", "images", "png", "window.png"))
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

func encodeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(30 + x), uint8(60 + y), 180, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestObsidianImageReferencesInSpaceAndExternalIsolation(t *testing.T) {
	a, tt := newTestApp(t)
	src := a.state.ActiveSpace()
	pngData := string(encodeTestPNG(t, 120, 80))

	for _, rel := range []string{
		"attachments/Pasted image 20261027110938.png",
		"attachments/Pasted image 20261027111017.png",
		"notes/2026/local.png",
	} {
		if err := vault.Write(src.Root, rel, pngData); err != nil {
			t.Fatal(err)
		}
	}

	noteText := "# IELTS\n\n" +
		"![[Pasted image 20261027110938.png]]\n" +
		"![[attachments/Pasted image 20261027111017.png]]\n" +
		"The line chart shows percentages.\n\n" +
		"![[local.png]]\n\n" +
		"Tail.\n"
	if err := vault.Write(src.Root, "notes/2026/ielts.md", noteText); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("notes/2026/ielts.md")
	tt.Frame()

	loaded := 0
	for _, leaf := range a.doc.ed.Doc.Leaves() {
		if leaf.Block.Kind != "image" {
			continue
		}
		bmp := a.doc.loadImage(leaf.Block.URL)
		if bmp == nil {
			t.Fatalf("failed to load Obsidian image %q", leaf.Block.URL)
		}
		if w, h := bmp.Size(); w != 120 || h != 80 {
			t.Fatalf("image %q size = %dx%d, want 120x80", leaf.Block.URL, w, h)
		}
		loaded++
	}
	if loaded != 3 {
		t.Fatalf("loaded %d images, want 3", loaded)
	}

	// Edit only the trailing paragraph; the ![[...]] lines must stay untouched on disk.
	a.doc.ed.SetSelection(a.doc.ed.Len(), a.doc.ed.Len())
	focusDocument(a, tt)
	tt.Type(" End.")
	if !a.flush() {
		t.Fatal("flush edited Obsidian note failed")
	}
	saved, err := readFile(src, "notes/2026/ielts.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved, "![[Pasted image 20261027110938.png]]\n![[attachments/Pasted image 20261027111017.png]]") ||
		!strings.Contains(saved, "Tail. End.") {
		t.Fatalf("saved markdown lost Obsidian embeds:\n%s", saved)
	}

	// Relocating the note to another Space must bring its Space-resolved Obsidian attachments along.
	dst := a.state.Add("Archive", t.TempDir())
	moved, _, err := a.moveWithResources(src, "notes/2026/ielts.md", dst, "")
	if err != nil {
		t.Fatalf("moveWithResources failed: %v", err)
	}
	movedText, err := readFile(dst, moved)
	if err != nil {
		t.Fatal(err)
	}
	movedDoc := openDocument(dst, moved, movedText, false)
	for _, leaf := range movedDoc.ed.Doc.Leaves() {
		if leaf.Block.Kind == "image" && movedDoc.loadImage(leaf.Block.URL) == nil {
			t.Fatalf("moved Obsidian image %q failed to load in destination space", leaf.Block.URL)
		}
	}

	// External files must NEVER scan parent or sibling folders for bare Obsidian names.
	extDir := t.TempDir()
	if err := vault.Write(extDir, "attachments/secret.png", pngData); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(extDir, "outside.md", "![[secret.png]]\n"); err != nil {
		t.Fatal(err)
	}
	extFile := filepath.Join(extDir, "outside.md")
	if !a.openSystemFile(extFile) {
		t.Fatal("failed to open external markdown")
	}
	if a.doc.loadImage("secret.png") != nil {
		t.Fatal("external space scanned sibling folders for bare image filename")
	}
}
