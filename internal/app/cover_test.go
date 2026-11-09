package app

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// coverApp opens a document whose cover is a picture of w×h pixels: a
// gradient with a white disc in the middle, so a stretched picture shows.
func coverApp(t *testing.T, w, h int) (*App, *ui.Tester) {
	t.Helper()
	a, tt := newTestApp(t)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	r := min(w, h) / 3
	for y := range h {
		for x := range w {
			c := color.RGBA{uint8(x * 255 / w), 120, uint8(y * 255 / h), 255}
			if dx, dy := x-w/2, y-h/2; dx*dx+dy*dy < r*r {
				c = color.RGBA{255, 255, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	if err := os.MkdirAll(filepath.Join(root(a), "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(root(a), "assets", "cover.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
	doc := editor.ImportMarkdown("# 封面\n\n正文")
	doc.Cover = "assets/cover.png"
	encoded, err := doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root(a), "Cover.arcmd", encoded); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Cover.arcmd")
	a.doc.ed.RequestFocus = true
	tt.SetSize(1240, 800)
	tt.Frame()
	tt.Frame()
	return a, tt
}

// A picture cover is as tall as its proportions ask, within bounds, and
// the part of a tall one shown is chosen by dragging it, kept with 保存位置
// as one step of the history, or dropped with Escape.
func TestCoverReposition(t *testing.T) {
	a, tt := coverApp(t, 800, 1200)
	tt.Move(700, 200)
	tt.Frame()
	// The band is at its tallest: its buttons sit at its foot, 360 DIPs
	// down the card, not at the 200 of a gradient.
	if b, ok := tt.Find("更换封面"); !ok || b.Y < 340 {
		t.Fatalf("cover buttons at %+v, the cover kept its gradient height", b)
	}
	if err := tt.Click("调整位置"); err != nil {
		t.Fatal(err, tt.Texts())
	}
	tt.Frame()
	if !tt.HasText("拖动图片以调整位置") || !tt.HasText("保存位置") {
		t.Fatalf("not repositioning: %q", tt.Texts())
	}
	pad, _ := tt.Find("封面位置")
	x, y := pad.X+40, pad.Y+40
	tt.Press(x, y)
	for i := 1; i <= 10; i++ {
		tt.Move(x, y+float32(i)*20)
		tt.Frame()
	}
	tt.Release(x, y+200)
	tt.Frame()
	if a.doc.ed.Doc.CoverY != 0 {
		t.Fatal("the position was saved before 保存位置")
	}
	if err := tt.Click("保存位置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	moved := a.doc.ed.Doc.CoverY
	if moved >= 0 || moved < -1 {
		t.Fatalf("dragging down showed y %v, want more of the top (< 0)", moved)
	}
	if tt.HasText("拖动图片以调整位置") {
		t.Error("still repositioning after saving")
	}
	saveSettle(tt)
	text, _ := vault.Read(root(a), "Cover.arcmd")
	if doc, err := model.Decode(text); err != nil || doc.CoverY != moved {
		t.Fatalf("saved y %+v %v", doc, err)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if a.doc.ed.Doc.CoverY != 0 {
		t.Errorf("undo left y %v", a.doc.ed.Doc.CoverY)
	}

	// Escape drops a position being chosen.
	tt.Move(700, 200)
	tt.Frame()
	if err := tt.Click("调整位置"); err != nil {
		t.Fatal(err)
	}
	tt.Press(x, y)
	tt.Move(x, y+100)
	tt.Frame()
	tt.Release(x, y+100)
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if tt.HasText("拖动图片以调整位置") || a.doc.ed.Doc.CoverY != 0 {
		t.Errorf("escape kept the position: %v", a.doc.ed.Doc.CoverY)
	}
}

// A wide picture shows whole, in a band lower than the tallest, with
// nothing to reposition.
func TestWideCoverHasNoReposition(t *testing.T) {
	_, tt := coverApp(t, 3000, 600)
	tt.Move(700, 100)
	tt.Frame()
	if !tt.HasText("更换封面") || tt.HasText("调整位置") {
		t.Fatalf("buttons %q", tt.Texts())
	}
}

func TestCoverSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the cover")
	_, tt := coverApp(t, 800, 1200)
	savePNG(t, filepath.Join(dir, "cover-tall.png"), tt)
	tt.Move(700, 200)
	tt.Frame()
	if err := tt.Click("调整位置"); err != nil {
		t.Fatal(err)
	}
	pad, _ := tt.Find("封面位置")
	tt.Press(pad.X+40, pad.Y+40)
	tt.Move(pad.X+40, pad.Y+240)
	tt.Frame()
	savePNG(t, filepath.Join(dir, "cover-moving.png"), tt)
	_, tt = coverApp(t, 3000, 600)
	savePNG(t, filepath.Join(dir, "cover-wide.png"), tt)
}
