package app

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestEditorIMEAutosaveAndModeHistory(t *testing.T) {
	a, tt := newTestApp(t)
	const source = "# Note\n\nKeep this exact.\n"
	if err := vault.Write(root(a), "Note.md", source); err != nil {
		t.Fatal(err)
	}
	a.open("Note.md")
	a.doc.ed.SetSelection(a.doc.ed.Len(), a.doc.ed.Len())
	a.doc.ed.RequestFocus = true
	tt.Frame()
	tt.Compose("中文", 2)
	tt.Advance(2 * autosaveDelay)
	if text, _ := vault.Read(root(a), "Note.md"); text != source {
		t.Fatal("autosave wrote preedit")
	}
	tt.Type("中文")
	tt.Frame()
	tt.Advance(autosaveDelay + time.Millisecond)
	if a.doc.rel != "Note.arcmd" {
		t.Fatal("import did not migrate", a.doc.rel)
	}
	stored, _ := vault.Read(root(a), "Note.arcmd")
	native, err := model.Decode(stored)
	if err != nil || !strings.HasSuffix(native.PlainText(), "中文") {
		t.Fatal("native commit failed", err)
	}
	if original, _ := vault.Read(root(a), "Note.md"); original != source {
		t.Fatal("migration changed original")
	}

	ed := a.doc.ed
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	if a.doc.ed != ed {
		t.Fatal("mode switch replaced the editor")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	tt.Key(ui.Cmd, ui.KeyZ)
	if a.doc.ed.Text() != "Note\nKeep this exact." {
		t.Fatal("mode switch lost composition history")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	if a.doc.ed.Text() != "Note\nKeep this exact.中文" {
		t.Fatal("redo failed across modes")
	}
}

func TestAttachmentCommandPreservesSourceAndCopiesAsset(t *testing.T) {
	a, tt := newTestApp(t)
	before := a.doc.text()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 24, 16))); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "picture one.png")
	if err := os.WriteFile(source, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	a.doc.ed.SetSelection(a.doc.ed.Len(), a.doc.ed.Len())
	a.pickFiles = func(done func([]string)) { done([]string{source, source}) }
	a.pickImages()
	tt.Frame()
	if !strings.HasPrefix(a.doc.text(), before) || !strings.Contains(a.doc.text(), "assets/picture-one-2.png") {
		t.Fatal(a.doc.text())
	}
	for _, name := range []string{"picture-one.png", "picture-one-2.png"} {
		stored, err := os.ReadFile(filepath.Join(root(a), "assets", name))
		if err != nil || !bytes.Equal(stored, data.Bytes()) {
			t.Fatal("asset copy failed", err)
		}
	}
	if a.doc.loadImage("assets/picture-one.png") == nil {
		t.Fatal("local image was not decoded")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if a.doc.text() != before {
		t.Fatal("attachment insertion did not undo atomically")
	}
}

func TestCommandBarFormatsCustomEditor(t *testing.T) {
	a, tt := newTestApp(t)
	a.doc.ed.SetSelection(0, 7)
	before := a.doc.text()
	tt.Key(ui.Cmd, ui.KeyK)
	a.palette.query = "加粗"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.palette.open || !tt.Focused("文档") || !strings.HasPrefix(a.doc.text(), "# **Welcome**") {
		t.Fatal("command did not format and refocus text")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if a.doc.text() != before {
		t.Fatal("palette formatting did not undo")
	}
}

func TestSaveFailureKeepsUnsavedSource(t *testing.T) {
	a, tt := newTestApp(t)
	a.doc.ed.ReplaceText(nil, "unsaved")
	doc := a.doc
	originalRoot := doc.space.Root
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	doc.space.Root = filepath.Join(blocker, "space")
	tt.Frame()
	tt.Advance(autosaveDelay + time.Millisecond)
	if doc.saveErr == nil || !doc.dirty() || !tt.HasText("保存失败") {
		t.Fatal("save failure state missing")
	}
	doc.space.Root = originalRoot
	tt.Key(ui.Cmd, ui.KeyS)
	if doc.saveErr != nil || doc.dirty() {
		t.Fatal("explicit retry failed")
	}
}

func TestInvalidNativeDocumentCannotOverwriteFile(t *testing.T) {
	a, tt := newTestApp(t)
	const source = `{"format":"arcmd","version":9,"blocks":[]}`
	if err := vault.Write(root(a), "Future.arcmd", source); err != nil {
		t.Fatal(err)
	}
	a.open("Future.arcmd")
	tt.Frame()
	if a.doc.loadErr == nil || !tt.HasText("文档无法打开") {
		t.Fatal("invalid format was editable")
	}
	tt.Advance(3 * autosaveDelay)
	got, _ := vault.Read(root(a), "Future.arcmd")
	if got != source {
		t.Fatal("future format overwritten")
	}
}
func TestMigrationCollisionAndExport(t *testing.T) {
	a, tt := newTestApp(t)
	const original = "# Original\n\nText\n"
	if err := vault.Write(root(a), "Import.md", original); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root(a), "Import.arcmd", "occupied"); err != nil {
		t.Fatal(err)
	}
	a.open("Import.md")
	a.doc.ed.ReplaceText(nil, "Hello ")
	if !a.flush() || a.doc.rel != "Import 2.arcmd" {
		t.Fatal("migration collision")
	}
	a.exportMarkdown()
	tt.Frame()
	if got, _ := vault.Read(root(a), "Import.md"); got != original {
		t.Fatal("overwrote import")
	}
	if got, _ := vault.Read(root(a), "Import.arcmd"); got != "occupied" {
		t.Fatal("overwrote neighbor")
	}
	encoded, _ := vault.Read(root(a), a.doc.rel)
	restored := openDocument(a.doc.space, a.doc.rel, encoded, false)
	if restored.loadErr != nil || restored.text() != a.doc.text() {
		t.Fatal("rich restore failed")
	}
}

// A document switched to draws its text in the first frame: the editor
// used to cull every row in that frame, as its viewport was not measured
// yet, and nothing asked for another, so the page stayed blank until the
// pointer moved.
func TestSwitchDrawsTextInFirstFrame(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1240, 760)
	for range 3 {
		tt.Frame()
	}
	a.open("Journal/Ideas.arcmd")
	tt.Frame()
	img := tt.Image()
	b := img.Bounds()
	scale := b.Dx() / 1240
	dark := 0
	// The text column, below the page's top margin and its first line.
	for y := 160 * scale; y < 200*scale; y++ {
		for x := 300 * scale; x < 700*scale; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r>>8 < 100 && g>>8 < 100 && bl>>8 < 100 {
				dark++
			}
		}
	}
	if dark < 50 {
		t.Fatalf("the first frame drew no text (%d dark pixels)", dark)
	}
}
