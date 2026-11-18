package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
)

// testAttachmentFolder points a space's attachments at a folder inside it
// and returns that folder's name.
func testAttachmentFolder(t *testing.T, a *App, folder string) string {
	t.Helper()
	sp := a.state.ActiveSpace()
	abs := filepath.Join(sp.Root, filepath.FromSlash(folder))
	if err := os.MkdirAll(abs, 0o755); err != nil {
		t.Fatal(err)
	}
	a.setAttachmentsFolder(sp, abs)
	if sp.AttachmentsDir != folder {
		t.Fatalf("attachments folder = %q, want %q", sp.AttachmentsDir, folder)
	}
	return abs
}

func TestAttachmentsFolderInSpaceMenu(t *testing.T) {
	a, tt := newTestApp(t)
	settle(tt)
	sp := a.state.ActiveSpace()
	if err := tt.RightClick(sp.Name); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tt.Menu(), "附件目录") {
		t.Fatalf("space menu has no attachments folder: %q", tt.Menu())
	}
	// Choosing the default folder resets a folder chosen before.
	testAttachmentFolder(t, a, "附件")
	if saved, err := a.store.Load(); err != nil || saved.Get(sp.ID).AttachmentsDir != "附件" {
		t.Fatalf("attachments folder was not saved: %+v, %v", saved, err)
	}
	settle(tt)
	if err := tt.RightClick(sp.Name); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("附件目录", "文档旁的 assets（默认）"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if sp.AttachmentsDir != "" {
		t.Fatalf("default did not reset the folder: %q", sp.AttachmentsDir)
	}
	if saved, err := a.store.Load(); err != nil || saved.Get(sp.ID).AttachmentsDir != "" {
		t.Fatalf("reset was not saved: %+v, %v", saved, err)
	}
}

func TestAttachmentsGoToConfiguredFolder(t *testing.T) {
	a, _ := dropApp(t)
	testAttachmentFolder(t, a, "附件")
	img, data := pngFile(t, "one.png")
	a.insertImages([]string{img}, -1)
	if !a.flush() {
		t.Fatal("save failed")
	}
	if got := blockList(a); !slices.Contains(got, "image:附件/one.png") {
		t.Fatalf("blocks = %q", got)
	}
	stored, err := os.ReadFile(filepath.Join(root(a), "附件", "one.png"))
	if err != nil || string(stored) != string(data) {
		t.Fatal("picture not copied into the attachments folder", err)
	}
	if a.doc.loadImage("附件/one.png") == nil {
		t.Fatal("picture does not load from the attachments folder")
	}
	// The legacy assets folder is not created when a folder is chosen.
	if _, err := os.Stat(filepath.Join(root(a), "assets")); !os.IsNotExist(err) {
		t.Fatalf("assets folder was created anyway: %v", err)
	}
}

func TestAttachmentsFolderKeepsDocumentsInSubfolders(t *testing.T) {
	a, tt := dropApp(t)
	testAttachmentFolder(t, a, "附件")
	if _, err := os.Stat(filepath.Join(root(a), "Journal", "Ideas.md")); err != nil {
		t.Fatal(err)
	}
	a.open("Journal/Ideas.md")
	settle(tt)
	if a.doc == nil || a.doc.rel != "Journal/Ideas.md" {
		t.Fatalf("open = %+v", a.doc)
	}
	img, _ := pngFile(t, "nested.png")
	a.insertImages([]string{img}, -1)
	if !a.flush() {
		t.Fatal("save failed")
	}
	if got := blockList(a); !slices.Contains(got, "image:附件/nested.png") {
		t.Fatalf("blocks = %q", got)
	}
	if a.doc.loadImage("附件/nested.png") == nil {
		t.Fatal("root-relative attachment does not load from a subfolder")
	}
}

func TestMovedAttachmentsRelinkDocuments(t *testing.T) {
	a, _ := dropApp(t)
	img, _ := pngFile(t, "pic.png")
	a.insertImages([]string{img}, -1)
	if !a.flush() {
		t.Fatal("save failed")
	}
	sp := a.state.ActiveSpace()
	folder := testAttachmentFolder(t, a, "附件")
	// The reader moves the old assets folder into the chosen folder.
	if err := os.Rename(filepath.Join(root(a), "assets"), filepath.Join(folder, "assets")); err != nil {
		t.Fatal(err)
	}
	// Reading finds the picture even before the links are rewritten.
	if a.doc.loadImage("assets/pic.png") == nil {
		t.Fatal("moved attachment does not resolve by its old URL")
	}
	a.filesChanged(sp.ID, []mygo.FileEvent{{Op: mygo.FileRename, OldPath: "assets", Path: "附件/assets"}})
	if got := blockList(a); !slices.Contains(got, "image:附件/assets/pic.png") {
		t.Fatalf("open document was not relinked: %q", got)
	}
	if a.doc.loadImage("附件/assets/pic.png") == nil {
		t.Fatal("relinked picture does not load")
	}
	if !a.flush() {
		t.Fatal("save failed")
	}
	text, err := vault.Read(sp.Root, a.doc.rel)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "附件/assets/pic.png") || strings.Contains(text, "(assets/pic.png)") {
		t.Fatalf("saved Markdown = %q", text)
	}
}

func TestMovedAttachmentsRelinkClosedDocuments(t *testing.T) {
	a, _ := dropApp(t)
	sp := a.state.ActiveSpace()
	if err := vault.Write(sp.Root, "assets/wide.png", "picture"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Other.md", "# 其他\n\n![wide](assets/wide.png)\n"); err != nil {
		t.Fatal(err)
	}
	folder := testAttachmentFolder(t, a, "附件")
	if err := os.Rename(filepath.Join(root(a), "assets"), filepath.Join(folder, "assets")); err != nil {
		t.Fatal(err)
	}
	a.filesChanged(sp.ID, []mygo.FileEvent{{Op: mygo.FileRename, OldPath: "assets", Path: "附件/assets"}})
	text, err := vault.Read(sp.Root, "Other.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "附件/assets/wide.png") || strings.Contains(text, "](assets/wide.png)") {
		t.Fatalf("closed document was not relinked: %q", text)
	}
}

func TestAttachmentsFolderRefusesOutsideTheSpace(t *testing.T) {
	a, _ := newTestApp(t)
	sp := a.state.ActiveSpace()
	outside := t.TempDir()
	a.setAttachmentsFolder(sp, outside)
	if sp.AttachmentsDir != "" {
		t.Fatalf("outside folder was kept: %q", sp.AttachmentsDir)
	}
}

func TestConfiguredAttachmentsFollowMovedDocument(t *testing.T) {
	for _, mode := range []string{"folder", "space"} {
		t.Run(mode, func(t *testing.T) {
			a, _ := dropApp(t)
			testAttachmentFolder(t, a, "附件")
			img, _ := pngFile(t, "moved.png")
			a.insertImages([]string{img}, -1)
			if !a.flush() {
				t.Fatal("save failed")
			}
			if mode == "folder" {
				if err := os.Mkdir(filepath.Join(root(a), "Archive"), 0o755); err != nil {
					t.Fatal(err)
				}
				a.moveInto(a.doc.rel, "Archive")
			} else {
				dst := a.state.Add("Other", filepath.Join(t.TempDir(), "space"))
				if err := os.MkdirAll(dst.Root, 0o700); err != nil {
					t.Fatal(err)
				}
				if a.moveAcross(a.doc.space.ID, a.doc.rel, dst.ID, "") == "" {
					t.Fatal("move failed")
				}
			}
			urls := a.doc.ed.ImageURLs()
			if len(urls) == 0 {
				t.Fatal("the picture was forgotten")
			}
			for _, url := range urls {
				if a.doc.loadImage(url) == nil {
					t.Fatalf("picture lost after move: %s", url)
				}
			}
		})
	}
}
