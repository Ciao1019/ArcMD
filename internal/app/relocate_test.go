package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/history"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

func TestMovedImportKeepsParentImages(t *testing.T) {
	for _, mode := range []string{"folder", "space"} {
		t.Run(mode, func(t *testing.T) {
			a, _, source, _ := parentImagesApp(t)
			if mode == "folder" {
				// Folder operations require explicitly adding a folder space;
				// opening one external article no longer imports its parent.
				a.state.Add("Articles", filepath.Dir(source))
				if !a.openSystemFile(source) {
					t.Fatal("open folder space")
				}
			}
			a.doc.ed.SetSelection(a.doc.ed.Len(), a.doc.ed.Len())
			a.doc.ed.ReplaceText(nil, " edited")
			if !a.flush() {
				t.Fatal("save import")
			}
			if mode == "folder" {
				if err := os.Mkdir(filepath.Join(a.doc.space.Root, "Archive"), 0700); err != nil {
					t.Fatal(err)
				}
				a.moveInto(a.doc.rel, "Archive")
				if !strings.HasPrefix(a.doc.rel, "Archive/") {
					t.Fatal("folder move failed")
				}
			} else {
				dst := a.state.Add("Other", filepath.Join(t.TempDir(), "space"))
				if err := os.MkdirAll(dst.Root, 0700); err != nil {
					t.Fatal(err)
				}
				if a.moveAcross(a.doc.space.ID, a.doc.rel, dst.ID, "") == "" {
					t.Fatal("space move failed")
				}
				if _, err := os.Stat(filepath.Join(dst.Root, "..", "images")); !os.IsNotExist(err) {
					t.Fatal("move wrote outside the destination Space")
				}
			}
			for _, url := range a.doc.ed.ImageURLs() {
				if a.doc.loadImage(url) == nil {
					t.Errorf("image lost after move: %s", url)
				}
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(source), "..", "images", "png", "window.png")); err != nil {
				t.Fatal("shared source image lost", err)
			}
		})
	}
}

func TestMarkdownParentImagesMoveRebasesIntoAssets(t *testing.T) {
	a, _, source, _ := parentImagesApp(t)
	dst := a.state.Add("Other", filepath.Join(t.TempDir(), "space"))
	if err := os.MkdirAll(dst.Root, 0700); err != nil {
		t.Fatal(err)
	}
	if a.moveAcross(a.doc.space.ID, a.doc.rel, dst.ID, "") == "" {
		t.Fatal("Markdown move should copy parent images into destination assets")
	}
	if _, err := os.Stat(filepath.Join(dst.Root, "..", "images")); !os.IsNotExist(err) {
		t.Fatal("move wrote outside the destination Space")
	}
	for _, url := range a.doc.ed.ImageURLs() {
		if a.doc.loadImage(url) == nil {
			t.Fatalf("rebased image failed to load: %s", url)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(source), "..", "images", "png", "window.png")); err != nil {
		t.Fatal("shared source image lost")
	}
}

func TestMovedDocumentKeepsImages(t *testing.T) {
	for _, mode := range []string{"folder", "space"} {
		t.Run(mode, func(t *testing.T) {
			a, _ := dropApp(t)
			img, _ := pngFile(t, "review.png")
			a.insertImages([]string{img}, -1)
			if !a.flush() {
				t.Fatal("save failed")
			}
			if a.doc.loadImage("assets/review.png") == nil {
				t.Fatal("image did not load before move")
			}
			if mode == "folder" {
				a.moveInto("Drop.arcmd", "Journal")
			} else {
				dst := a.state.Add("Review", t.TempDir())
				if a.moveAcross(a.doc.space.ID, a.doc.rel, dst.ID, "") == "" {
					t.Fatal("move failed")
				}
			}
			t.Logf("moved document: %s", a.doc.fullPath())
			if a.doc.loadImage("assets/review.png") == nil {
				t.Error("image no longer resolves after moving the document")
			}
		})
	}
}

func TestCrossSpaceKeepsHistory(t *testing.T) {
	a, _ := dropApp(t)
	src, rel := a.doc.space, a.doc.rel
	a.doc.ed.SetSelection(0, 0)
	a.doc.ed.ReplaceText(nil, "Changed ")
	if !a.flush() {
		t.Fatal("save failed")
	}
	before, err := history.List(src.Root, rel)
	if err != nil || len(before) == 0 {
		t.Fatalf("no original history: %v", err)
	}
	dst := a.state.Add("Review", t.TempDir())
	to := a.moveAcross(src.ID, rel, dst.ID, "")
	if to == "" {
		t.Fatal("move failed")
	}
	after, err := history.List(dst.Root, to)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("history before move: %d; after move: %d", len(before), len(after))
	if len(after) != len(before) {
		t.Error("version history did not follow cross-space move")
	}
}

func TestRenameRespectsWikiSpace(t *testing.T) {
	a, _ := newTestApp(t)
	home := a.state.ActiveSpace()
	other := a.state.Add("Other", t.TempDir())
	for _, entry := range []struct{ root, rel, body string }{
		{home.Root, "Notes.arcmd", "# Notes\n\nHome notes"},
		{other.Root, "Notes.arcmd", "# Notes\n\nOther notes"},
		{other.Root, "Reference.arcmd", "# Reference\n\nRead [[Notes]]"},
	} {
		if err := writeNative(entry.root, entry.rel, entry.body); err != nil {
			t.Fatal(err)
		}
	}
	a.refreshTree()
	a.refreshSearch()
	a.renameIn(home, "Notes.arcmd")
	a.newName = "Home Notes"
	a.commitRename()
	text, err := vault.Read(other.Root, "Reference.arcmd")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "[[Home Notes]]") {
		t.Error("renaming Home/Notes also rewrote the link to Other/Notes")
	}
}

func TestMoveImageCollisionPreservesUndoAndVersions(t *testing.T) {
	a, _ := dropApp(t)
	img, data := pngFile(t, "shared.png")
	a.insertImages([]string{img}, -1)
	a.doc.ed.Engine.SetCover("assets/shared.png")
	if !a.flush() {
		t.Fatal("save")
	}
	oldRoot := root(a)
	dst := a.state.Add("Other", t.TempDir())
	if err := vault.Write(dst.Root, "assets/shared.png", "other image"); err != nil {
		t.Fatal(err)
	}
	if a.moveAcross(a.doc.space.ID, a.doc.rel, dst.ID, "") == "" {
		t.Fatal("move")
	}
	url := a.doc.ed.Doc.Cover
	if url == "assets/shared.png" || a.doc.loadImage(url) == nil {
		t.Fatal("cover was not rebased")
	}
	if got, _ := vault.Read(dst.Root, "assets/shared.png"); got != "other image" {
		t.Fatal("existing asset replaced")
	}
	if got, _ := vault.Read(oldRoot, "assets/shared.png"); got != string(data) {
		t.Fatal("shared source image removed")
	}
	a.doc.ed.Undo()
	if a.doc.ed.Doc.Cover != "" {
		t.Fatal("cover undo")
	}
	a.doc.ed.Redo()
	if a.doc.ed.Doc.Cover != url || a.doc.loadImage(url) == nil {
		t.Fatal("redo restored the old URL")
	}
	versions, err := history.List(dst.Root, a.doc.rel)
	if err != nil || len(versions) == 0 {
		t.Fatal("versions", err)
	}
	found := false
	for _, v := range versions {
		text, err := history.Read(dst.Root, a.doc.rel, v)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := imageDocument(a.doc.rel, text)
		if err != nil {
			t.Fatal(err)
		}
		if doc.Cover != "" {
			found = true
			if doc.Cover != url {
				t.Fatal("snapshot kept old cover URL")
			}
		}
	}
	if !found {
		t.Fatal("cover snapshot missing")
	}
}

func TestMoveResourceFailureRollsBackDocument(t *testing.T) {
	a, _ := dropApp(t)
	img, _ := pngFile(t, "shared.png")
	a.insertImages([]string{img}, -1)
	if !a.flush() {
		t.Fatal("save")
	}
	original, _ := vault.Read(root(a), a.doc.rel)
	dst := a.state.Add("Other", t.TempDir())
	if err := vault.Write(dst.Root, "assets", "not a directory"); err != nil {
		t.Fatal(err)
	}
	if got := a.moveAcross(a.doc.space.ID, a.doc.rel, dst.ID, ""); got != "" {
		t.Fatal("move should fail")
	}
	if got, _ := vault.Read(root(a), "Drop.arcmd"); got != original {
		t.Fatal("original was not restored")
	}
	if a.doc.loadImage("assets/shared.png") == nil {
		t.Fatal("source image missing")
	}
	if got, _ := vault.Read(dst.Root, "assets"); got != "not a directory" {
		t.Fatal("existing destination changed")
	}
}

func TestMoveFolderKeepsNestedImagesAndHistory(t *testing.T) {
	a, _ := newTestApp(t)
	src := a.state.ActiveSpace()
	a.open("Journal/Ideas.md")
	img, _ := pngFile(t, "nested.png")
	a.insertImages([]string{img}, -1)
	if !a.flush() {
		t.Fatal("save")
	}
	before, _ := history.List(src.Root, "Journal/Ideas.md")
	dst := a.state.Add("Other", t.TempDir())
	if a.moveAcross(src.ID, "Journal", dst.ID, "") != "Journal" {
		t.Fatal("folder move")
	}
	if a.doc.loadImage("assets/nested.png") == nil {
		t.Fatal("nested image lost")
	}
	after, _ := history.List(dst.Root, "Journal/Ideas.md")
	if len(after) != len(before) {
		t.Fatal("nested history lost")
	}
	if original, _ := history.List(src.Root, "Journal/Ideas.md"); len(original) != 0 {
		t.Fatal("source history remains after commit")
	}
}

func TestMarkdownImageCollisionRebasesWithoutOverwriting(t *testing.T) {
	a, _ := newTestApp(t)
	src := a.state.ActiveSpace()
	_, data := pngFile(t, "md.png")
	text := "# Import\n\n![picture](assets/md.png)\n"
	if err := vault.Write(src.Root, "Import.md", text); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(src.Root, "assets/md.png", string(data)); err != nil {
		t.Fatal(err)
	}
	dst := a.state.Add("Other", t.TempDir())
	if err := vault.Write(dst.Root, "assets/md.png", "occupied"); err != nil {
		t.Fatal(err)
	}
	if a.moveAcross(src.ID, "Import.md", dst.ID, "") != "Import.md" {
		t.Fatal("Markdown move did not rebase colliding asset")
	}
	if got, _ := vault.Read(dst.Root, "assets/md.png"); got != "occupied" {
		t.Fatal("destination picture overwritten")
	}
	moved, _ := vault.Read(dst.Root, "Import.md")
	if !strings.Contains(moved, "assets/md-1.png") {
		t.Fatalf("Markdown did not rebase to numbered asset: %q", moved)
	}
	if got, _ := vault.Read(dst.Root, "assets/md-1.png"); got != string(data) {
		t.Fatal("numbered asset bytes mismatch")
	}
}
