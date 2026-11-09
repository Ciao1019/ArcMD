package app

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// pngFile writes a small PNG named name in a new folder and returns its
// path and bytes.
func pngFile(t *testing.T, name string) (string, []byte) {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 24, 16))); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p, data.Bytes()
}

// dropApp opens a short document of paragraphs to drop on.
func dropApp(t *testing.T) (*App, *ui.Tester) {
	t.Helper()
	a, tt := newTestApp(t)
	encoded, err := editor.ImportMarkdown("# Drop\n\nalpha\n\nbeta\n\ngamma").Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root(a), "Drop.arcmd", encoded); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Drop.arcmd")
	tt.Frame()
	tt.Frame()
	return a, tt
}

// blockList names the document's top-level blocks: their kind, and their
// text or, for images, their URL.
func blockList(a *App) []string {
	var out []string
	for _, b := range a.doc.ed.Doc.Blocks {
		if b.Kind == "image" {
			out = append(out, "image:"+b.URL)
			continue
		}
		out = append(out, b.Text())
	}
	return out
}

// dropAt drops files from Finder on the line holding text.
func dropAt(t *testing.T, a *App, tt *ui.Tester, text string, files ...string) {
	t.Helper()
	surface, ok := tt.Find("文档")
	if !ok {
		t.Fatal("writing surface missing")
	}
	ed := a.doc.ed
	for _, leaf := range ed.Doc.Leaves() {
		if leaf.Block.Text() != text {
			continue
		}
		r, _, ok := ed.BoundsForRange(ui.TextInputRange{Start: leaf.Start, End: leaf.Start})
		if !ok {
			t.Fatal("no bounds for", text)
		}
		// The line's top part: the block's upper half.
		x, y := surface.X+r.X+4, surface.Y+r.Y+2
		tt.Move(x, y)
		if !tt.DropFiles(x, y, files...) {
			t.Fatal("the page did not take the files")
		}
		tt.Frame()
		return
	}
	t.Fatal("no line", text)
}

// Pictures dropped from Finder go where they fall, copied beside the
// document, as one step of the history.
func TestDropImagesWhereTheyFall(t *testing.T) {
	a, tt := dropApp(t)
	one, data := pngFile(t, "one.png")
	two, _ := pngFile(t, "Two Shot.JPG")
	before := blockList(a)
	dropAt(t, a, tt, "beta", one, two)
	want := []string{"Drop", "alpha", "image:assets/one.png", "image:assets/Two-Shot.JPG", "beta", "gamma"}
	if got := blockList(a); !slices.Equal(got, want) {
		t.Fatalf("blocks = %q, want %q", got, want)
	}
	stored, err := os.ReadFile(filepath.Join(root(a), "assets", "one.png"))
	if err != nil || !bytes.Equal(stored, data) {
		t.Fatal("picture not copied", err)
	}
	if !tt.Focused("文档") {
		t.Error("the page did not take the keyboard")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if got := blockList(a); !slices.Equal(got, before) {
		t.Errorf("undo left %q", got)
	}
	// Dropped on the last line, it goes in before it; on the title, first.
	dropAt(t, a, tt, "Drop", one)
	if got := blockList(a); got[0] != "image:assets/one-2.png" {
		t.Errorf("dropped on the title: %q", got)
	}
}

// Files that are no pictures are left out, with a notice naming them.
func TestDropSkipsOtherFiles(t *testing.T) {
	a, tt := dropApp(t)
	pic, _ := pngFile(t, "pic.png")
	notes := filepath.Join(t.TempDir(), "notes.pdf")
	if err := os.WriteFile(notes, []byte("%PDF"), 0o600); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(t.TempDir(), "photos.png") // a folder named as a picture
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	dropAt(t, a, tt, "gamma", notes, pic, folder)
	if got := blockList(a); !slices.Equal(got, []string{"Drop", "alpha", "beta", "image:assets/pic.png", "gamma"}) {
		t.Fatalf("blocks = %q", got)
	}
	if !tt.HasText("notes.pdf、photos.png 未插入") {
		t.Errorf("no notice: %q", tt.Texts())
	}
	// No pictures among them: nothing changes.
	before := a.doc.ed.Revision()
	dropAt(t, a, tt, "gamma", notes)
	if a.doc.ed.Revision() != before {
		t.Errorf("revision %d→%d", before, a.doc.ed.Revision())
	}
}

// A picture on the clipboard, as a screenshot, is saved beside the
// document and pasted at the caret; files copied in Finder paste as
// dropped there.
func TestPasteImages(t *testing.T) {
	a, tt := dropApp(t)
	_, data := pngFile(t, "shot.png")
	ed := a.doc.ed
	end := ed.Len()
	ed.SetSelection(end, end)
	ed.RequestFocus = true
	tt.Frame()
	tt.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, data))))
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	want := []string{"Drop", "alpha", "beta", "gamma", "image:assets/粘贴的图片.png"}
	if got := blockList(a); !slices.Equal(got, want) {
		t.Fatalf("pasted PNG: %q", got)
	}
	stored, err := os.ReadFile(filepath.Join(root(a), "assets", "粘贴的图片.png"))
	if err != nil || !bytes.Equal(stored, data) {
		t.Fatal("pasted picture not saved", err)
	}
	// A second paste never replaces the first file.
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	if got := blockList(a); got[len(got)-1] != "image:assets/粘贴的图片-2.png" {
		t.Errorf("second paste: %q", got)
	}

	pic, _ := pngFile(t, "copied.png")
	files, err := transfer.FileData(pic)
	if err != nil {
		t.Fatal(err)
	}
	tt.SetClipboardData(files)
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	if got := blockList(a); got[len(got)-1] != "image:assets/copied.png" {
		t.Errorf("pasted file: %q", got)
	}
	// Plain text still pastes as text.
	ed.SetSelection(ed.Doc.Leaves()[1].Start, ed.Doc.Leaves()[1].Start)
	tt.SetClipboard("new ")
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	if !slices.Contains(blockList(a), "new alpha") {
		t.Errorf("text paste: %q", blockList(a))
	}
}
