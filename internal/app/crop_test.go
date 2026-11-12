package app

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

func TestCropImageKeepsTheOriginal(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 40, 20)))
	os.MkdirAll(filepath.Join(root(a), "assets"), 0o755)
	os.WriteFile(filepath.Join(root(a), "assets", "photo.png"), buf.Bytes(), 0o644)
	doc := model.New()
	img := model.NewBlock("image", "")
	img.URL = "assets/photo.png"
	doc.Blocks = []*model.Block{model.NewBlock("paragraph", "x"), img}
	text, _ := doc.Encode()
	vault.Write(root(a), "Pic.arcmd", text)
	a.refreshTree()
	a.open("Pic.arcmd")
	tt.Frame()
	a.cropImage(a.doc, img.ID, img.URL, image.Rect(5, 5, 25, 15))
	got := a.doc.ed.Doc.Blocks[1].URL
	if got != "assets/photo-裁剪.png" {
		t.Fatal("the picture points at the cut", got)
	}
	f, err := os.Open(filepath.Join(root(a), "assets", "photo-裁剪.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil || cfg.Width != 20 || cfg.Height != 10 {
		t.Fatal("cut size", cfg, err)
	}
	if _, err := os.Stat(filepath.Join(root(a), "assets", "photo.png")); err != nil {
		t.Fatal("the original stays")
	}
	a.doc.ed.Undo()
	if a.doc.ed.Doc.Blocks[1].URL != "assets/photo.png" {
		t.Fatal("undo")
	}
}
