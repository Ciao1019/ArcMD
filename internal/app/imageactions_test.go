package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func failedImageApp(t *testing.T) (*App, *ui.Tester) {
	t.Helper()
	a, tt := newTestApp(t)
	if err := writeNative(root(a), "Picture.arcmd", "# 图片\n\n![图片说明](assets/missing.png)\n\n图片后的正文"); err != nil {
		t.Fatal(err)
	}
	a.open("Picture.arcmd")
	tt.Frame()
	return a, tt
}

func TestReplaceMissingImageAndUndo(t *testing.T) {
	a, tt := failedImageApp(t)
	file, data := pngFile(t, "replacement.png")
	a.pickFiles = func(done func([]string)) { done([]string{file}) }
	original := a.doc.ed.Doc.Clone()
	if err := tt.Click("重新选择图片…"); err != nil {
		t.Fatal(err)
	}
	block := a.doc.ed.Doc.Blocks[1]
	if block.URL == original.Blocks[1].URL || block.ID != original.Blocks[1].ID || block.Text() != "图片说明" || a.doc.loadImage(block.URL) == nil {
		t.Fatal("replacement lost identity, description or image")
	}
	if got, err := os.ReadFile(file); err != nil || string(got) != string(data) {
		t.Fatal("replacement modified its source", err)
	}
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyZ)
	if a.doc.ed.Doc.Blocks[1].URL != "assets/missing.png" {
		t.Fatal("replacement was not one undo step")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	if a.doc.loadImage(a.doc.ed.Doc.Blocks[1].URL) == nil {
		t.Fatal("redo did not restore the picture")
	}
	if !a.flush() {
		t.Fatal("save replacement")
	}
}

func TestReplacementCancelInvalidAndStaleAreHarmless(t *testing.T) {
	for _, mode := range []string{"cancel", "invalid", "deleted", "newer-choice"} {
		t.Run(mode, func(t *testing.T) {
			a, tt := failedImageApp(t)
			var complete func([]string)
			a.pickFiles = func(done func([]string)) { complete = done }
			if err := tt.Click("重新选择图片…"); err != nil {
				t.Fatal(err)
			}
			file, _ := pngFile(t, "replacement.png")
			files := []string{file}
			switch mode {
			case "cancel":
				files = nil
			case "invalid":
				if err := os.WriteFile(file, []byte("not an image"), 0600); err != nil {
					t.Fatal(err)
				}
			case "deleted":
				a.doc.ed.DeleteBlock(a.doc.ed.Doc.Blocks[1].ID)
			case "newer-choice":
				a.doc.ed.ReplaceImage(a.doc.ed.Doc.Blocks[1].ID, "assets/missing.png", "assets/newer.png")
			}
			before, _ := a.doc.ed.Doc.Encode()
			complete(files)
			after, _ := a.doc.ed.Doc.Encode()
			if before != after {
				t.Fatal("dialog changed a stale or cancelled target")
			}
			if _, err := os.Stat(filepath.Join(root(a), "assets", "replacement.png")); !os.IsNotExist(err) {
				t.Fatal("dialog left an unused attachment")
			}
		})
	}
}

func TestImageFailureReasonsAndRevealNearest(t *testing.T) {
	a, tt := failedImageApp(t)
	var revealed string
	a.revealFile = func(file string) { revealed = file }
	if err := tt.Click("在文件夹中显示"); err != nil {
		t.Fatal(err)
	}
	if revealed != root(a) {
		t.Fatal("missing picture should reveal nearest existing directory", revealed)
	}
	if err := vault.Write(root(a), "assets/broken.png", "broken bytes"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		url     string
		problem editor.ImageProblem
		local   bool
	}{
		{"assets/missing.png", editor.ImageMissing, true},
		{"assets/broken.png", editor.ImageUnsupported, true},
		{"assets", editor.ImageUnreadable, true},
		{"https://example.com/photo.png", editor.ImageRemote, false},
		{"bad%zz", editor.ImageInvalid, false},
	} {
		got := a.doc.imageResource(test.url)
		if got.Bitmap != nil || got.Problem != test.problem || got.Local != test.local {
			t.Errorf("%s: %#v", test.url, got)
		}
	}
	a.revealImage(a.doc, "assets/broken.png")
	if revealed != filepath.Join(root(a), "assets", "broken.png") {
		t.Fatal("existing image was not revealed")
	}
}

func TestReplacementStaysWithOriginalSplitPane(t *testing.T) {
	a, tt := failedImageApp(t)
	original := a.doc
	a.openSplit(original.space, "Welcome.arcmd", false, false)
	if a.split == nil {
		t.Fatal("split")
	}
	a.activateDocument(original)
	var complete func([]string)
	a.pickFiles = func(done func([]string)) { complete = done }
	a.pickReplacementImage(original, original.ed.Doc.Blocks[1].ID, "assets/missing.png")
	for _, doc := range a.residentDocuments() {
		if doc != original {
			a.activateDocument(doc)
			break
		}
	}
	active := a.doc
	before, _ := active.ed.Doc.Encode()
	file, _ := pngFile(t, "replacement.png")
	complete([]string{file})
	tt.Frame()
	if original.loadImage(original.ed.Doc.Blocks[1].URL) == nil {
		t.Fatal("replacement did not update the originating pane")
	}
	after, _ := active.ed.Doc.Encode()
	if a.doc != active || before != after {
		t.Fatal("dialog changed the active pane")
	}
}
