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

// pngBytes returns the bytes of a small PNG.
func pngBytes(t *testing.T) []byte {
	t.Helper()
	_, data := pngFile(t, "canary.png")
	return data
}

// TestPasteDoesNotRelinkClosedDocuments runs the watcher exactly as a paste
// or drag-in delivers it: ArcMD wrote the picture itself, so the closed
// documents of the space must not be scanned and rewritten. An unrelated
// picture copied by another application also provides no move evidence.
func TestPasteDoesNotRelinkClosedDocuments(t *testing.T) {
	a, tt := dropApp(t)
	sp := a.state.ActiveSpace()
	testAttachmentFolder(t, a, "附件")
	// A closed document still points at the legacy assets location, and the
	// picture now lives under the chosen folder, so a scan would rewrite it.
	if err := vault.Write(sp.Root, "Other.md", "# 其他\n\n![pic](assets/pic.png)\n"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "附件/assets/pic.png", "picture"); err != nil {
		t.Fatal(err)
	}
	// ArcMD pastes a screenshot: the file lands in the attachments folder.
	img, _ := pngFile(t, "paste.png")
	a.insertImages([]string{img}, -1)
	if !a.flush() {
		t.Fatal("save failed")
	}
	// The watcher reports ArcMD's own write, the way the platform does.
	a.filesChanged(sp.ID, []mygo.FileEvent{
		{Op: mygo.FileCreate, Path: "附件/paste.png"},
		{Op: mygo.FileRename, OldPath: ".paste.png.1.tmp", Path: "附件/paste.png"},
	})
	if text, err := vault.Read(sp.Root, "Other.md"); err != nil || !strings.Contains(text, "](assets/pic.png)") {
		t.Fatalf("ArcMD's own paste rewrote a closed document: %q, %v", text, err)
	}
	settle(tt)
	// Somebody else copies a picture in: unrelated references stay untouched.
	if err := vault.Write(sp.Root, "附件/stray.png", "stray"); err != nil {
		t.Fatal(err)
	}
	a.filesChanged(sp.ID, []mygo.FileEvent{{Op: mygo.FileCreate, Path: "附件/stray.png"}})
	text, err := vault.Read(sp.Root, "Other.md")
	if err != nil || !strings.Contains(text, "](assets/pic.png)") {
		t.Fatalf("an unrelated outside copy rewrote the closed document: %q, %v", text, err)
	}
}

// TestMissingImageNotReResolvedEveryFrame keeps asking for a broken
// picture the way every frame of the editor does: the answer is cached, so
// the attachments folder is not walked again and again. The cache clears
// when files change.
func TestMissingImageNotReResolvedEveryFrame(t *testing.T) {
	a, _ := dropApp(t)
	sp := a.state.ActiveSpace()
	testAttachmentFolder(t, a, "附件")
	if err := vault.Write(sp.Root, "Ghost.md", "# Ghost\n\n![g](附件/ghost.png)\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Ghost.md")
	// The first frame resolves the miss — and walks the folder doing so.
	if res := a.imageResourceFor(a.doc, "附件/ghost.png"); res.Bitmap != nil {
		t.Fatal("the missing picture resolved")
	}
	key := imageKey{doc: a.doc, url: "附件/ghost.png"}
	if _, ok := a.imgResolved[key]; !ok {
		t.Fatal("the resolution was not cached")
	}
	// A file of that name turns up deeper in the folder, without a file
	// event. The frames keep asking; the cached answer stands: the folder
	// is not walked again, or the canary would load from its new place.
	if err := vault.Write(sp.Root, "附件/sub/ghost.png", string(pngBytes(t))); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if res := a.imageResourceFor(a.doc, "附件/ghost.png"); res.Bitmap != nil {
			t.Fatal("a cached answer was resolved again without a file change")
		}
	}
	// A file change drops the cache and the picture shows up.
	a.filesChanged(sp.ID, []mygo.FileEvent{{Op: mygo.FileCreate, Path: "附件/sub/ghost.png"}})
	if res := a.imageResourceFor(a.doc, "附件/ghost.png"); res.Bitmap == nil {
		t.Fatal("the cached miss survived the file change")
	}
}

// TestRelinkFollowsRecordedMoveDespiteSameNames relinks a closed document
// from the rename the watcher recorded, not by guessing a name: another
// file of the same name already sits in the folder, and the exact new
// location must win.
func TestRelinkFollowsRecordedMoveDespiteSameNames(t *testing.T) {
	a, _ := dropApp(t)
	sp := a.state.ActiveSpace()
	testAttachmentFolder(t, a, "附件")
	if err := vault.Write(sp.Root, "notes/pic.png", string(pngBytes(t))); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Other.md", "# 其他\n\n![p](notes/pic.png)\n"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "附件/other/kept.png", "decoy"); err != nil {
		t.Fatal(err)
	}
	// The reader drags the picture into the attachments folder, where its
	// name collides with the decoy's.
	_, data := pngFile(t, "kept.png")
	if err := os.WriteFile(filepath.Join(root(a), "附件", "kept.png"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root(a), "notes", "pic.png")); err != nil {
		t.Fatal(err)
	}
	a.filesChanged(sp.ID, []mygo.FileEvent{{Op: mygo.FileRename, OldPath: "notes/pic.png", Path: "附件/kept.png"}})
	text, err := vault.Read(sp.Root, "Other.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "](附件/kept.png)") {
		t.Fatalf("the recorded move was not followed: %q", text)
	}
	if strings.Contains(text, "other/kept.png") || strings.Contains(text, "notes/pic.png") {
		t.Fatalf("the document was bound to another file's picture: %q", text)
	}
	if a.doc.loadImage("附件/kept.png") == nil {
		t.Fatal("the relinked picture does not load")
	}
}

// TestExportHTMLDocumentRelativeImagePaths exports a subfolder document and
// a root document as HTML: the src the browser resolves is the one the
// Markdown holds, relative to the document, so no picture breaks.
func TestExportHTMLDocumentRelativeImagePaths(t *testing.T) {
	a, tt := dropApp(t)
	testAttachmentFolder(t, a, "附件")
	a.open("Journal/Ideas.md")
	settle(tt)
	img, _ := pngFile(t, "nested.png")
	a.insertImages([]string{img}, -1)
	if !a.flush() {
		t.Fatal("save failed")
	}
	if got := blockList(a); !slices.Contains(got, "image:../附件/nested.png") {
		t.Fatalf("blocks = %q", got)
	}
	a.exportHTML()
	page, err := os.ReadFile(filepath.Join(root(a), "Journal", "Ideas.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `src="../附件/nested.png"`) {
		t.Fatalf("subfolder HTML = %s", page)
	}
	// The browser resolves the src against the document's folder.
	if _, err := os.Stat(filepath.Join(root(a), "Journal", "..", "附件", "nested.png")); err != nil {
		t.Fatal("the referenced picture does not sit where the HTML points", err)
	}
	// A root document keeps the folder-free URL.
	a.open("Drop.arcmd")
	settle(tt)
	one, _ := pngFile(t, "one.png")
	a.insertImages([]string{one}, -1)
	if !a.flush() {
		t.Fatal("save failed")
	}
	a.exportHTML()
	page, err = os.ReadFile(filepath.Join(root(a), "Drop.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `src="附件/one.png"`) {
		t.Fatalf("root HTML = %s", page)
	}
}

// TestRootAttachmentFolderMovesWithinSpace keeps the space's root as the
// attachments folder and moves a document into a folder: the picture stays
// put and only the relative URL follows.
func TestRootAttachmentFolderMovesWithinSpace(t *testing.T) {
	a, _ := dropApp(t)
	sp := a.state.ActiveSpace()
	if !sp.SetAttachmentsDir(".") {
		t.Fatal("the space root was refused as the attachments folder")
	}
	img, _ := pngFile(t, "top.png")
	a.insertImages([]string{img}, -1)
	if !a.flush() {
		t.Fatal("save failed")
	}
	if got := blockList(a); !slices.Contains(got, "image:top.png") {
		t.Fatalf("blocks = %q", got)
	}
	if err := os.Mkdir(filepath.Join(root(a), "Archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.moveInto(a.doc.rel, "Archive")
	if got := blockList(a); !slices.Contains(got, "image:../top.png") {
		t.Fatalf("blocks after the move = %q", got)
	}
	if _, err := os.Stat(filepath.Join(root(a), "Archive", "top.png")); !os.IsNotExist(err) {
		t.Fatal("the picture was copied beside the moved document")
	}
	if _, err := os.Stat(filepath.Join(root(a), "top.png")); err != nil {
		t.Fatal("the picture left the space root", err)
	}
	if a.doc.loadImage("../top.png") == nil {
		t.Fatal("the moved document does not load its picture")
	}
}

// TestRootAttachmentFolderCrossSpaceMove moves a subfolder document whose
// picture sits at the old space's root into a new space that keeps its
// attachments in a folder of their own: the copy must land in that folder,
// as it does for every other document.
func TestRootAttachmentFolderCrossSpaceMove(t *testing.T) {
	a, _ := dropApp(t)
	sp := a.state.ActiveSpace()
	if !sp.SetAttachmentsDir(".") {
		t.Fatal("the space root was refused as the attachments folder")
	}
	_, data := pngFile(t, "top.png")
	if err := vault.Write(sp.Root, "top.png", string(data)); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Notes/Deep.md", "# Deep\n\n![t](../top.png)\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Notes/Deep.md")
	dst := a.state.Add("Other", filepath.Join(t.TempDir(), "space"))
	if err := os.MkdirAll(filepath.Join(dst.Root, "素材"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst.SetAttachmentsDir("素材")
	if a.moveAcross(sp.ID, "Notes/Deep.md", dst.ID, "") == "" {
		t.Fatal("move failed")
	}
	if got := blockList(a); !slices.Contains(got, "image:素材/top.png") {
		t.Fatalf("blocks after the cross-space move = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dst.Root, "素材", "top.png")); err != nil {
		t.Fatal("the copy did not land in the destination's attachments folder", err)
	}
	if _, err := os.Stat(filepath.Join(dst.Root, "top.png")); err == nil {
		t.Fatal("the copy landed at the destination's root instead")
	}
	if a.doc.loadImage("素材/top.png") == nil {
		t.Fatal("the moved document does not load its picture")
	}
	// The window followed the document to the destination; the source's
	// copy stays in the source space.
	srcRoot := sp.Root
	if _, err := os.Stat(filepath.Join(srcRoot, "top.png")); err != nil {
		t.Fatal("the source's copy was removed", err)
	}
}

// TestAttachmentsFolderResolvesSymbolicLinks chooses a folder through a
// symbolic link and a spelled-differently root, the way macOS hands out
// /var for /private/var: the stored path is the real folder inside the
// space. node_modules is refused outright.
func TestAttachmentsFolderResolvesSymbolicLinks(t *testing.T) {
	a, _ := newTestApp(t)
	sp := a.state.ActiveSpace()
	if err := os.Mkdir(filepath.Join(sp.Root, "真实目录"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(sp.Root, "真实目录"), filepath.Join(sp.Root, "链接")); err != nil {
		t.Fatal(err)
	}
	a.setAttachmentsFolder(sp, filepath.Join(sp.Root, "链接"))
	if sp.AttachmentsDir != "真实目录" {
		t.Fatalf("the symlink was not resolved: %q", sp.AttachmentsDir)
	}
	if saved, err := a.store.Load(); err != nil || saved.Get(sp.ID).AttachmentsDir != "真实目录" {
		t.Fatalf("the resolved folder was not saved: %+v, %v", saved, err)
	}
	nm := filepath.Join(sp.Root, "node_modules", "pkg")
	if err := os.MkdirAll(nm, 0o755); err != nil {
		t.Fatal(err)
	}
	a.setAttachmentsFolder(sp, nm)
	if sp.AttachmentsDir != "真实目录" {
		t.Fatalf("node_modules was accepted: %q", sp.AttachmentsDir)
	}
}
