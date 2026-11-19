package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/history"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
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
	// The URL is relative to the document, so the Markdown also shows the
	// picture in GitHub, VS Code and the HTML export.
	if got := blockList(a); !slices.Contains(got, "image:../附件/nested.png") {
		t.Fatalf("blocks = %q", got)
	}
	if a.doc.loadImage("../附件/nested.png") == nil {
		t.Fatal("document-relative attachment does not load from a subfolder")
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
			if got := blockList(a); !slices.Contains(got, "image:附件/moved.png") {
				t.Fatalf("blocks = %q", got)
			}
			if mode == "folder" {
				if err := os.Mkdir(filepath.Join(root(a), "Archive"), 0o755); err != nil {
					t.Fatal(err)
				}
				a.moveInto(a.doc.rel, "Archive")
				// The attachment stays in the folder; only the URL follows
				// the document's new directory. Nothing is copied.
				if got := blockList(a); !slices.Contains(got, "image:../附件/moved.png") {
					t.Fatalf("blocks after folder move = %q", got)
				}
				if _, err := os.Stat(filepath.Join(root(a), "Archive", "附件")); !os.IsNotExist(err) {
					t.Fatal("the attachment was copied beside the moved document")
				}
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

func TestCrossSpaceCopiesIntoDestinationFolder(t *testing.T) {
	a, _ := dropApp(t)
	src := a.state.ActiveSpace()
	testAttachmentFolder(t, a, "附件")
	img, _ := pngFile(t, "shared.png")
	a.insertImages([]string{img}, -1)
	if !a.flush() {
		t.Fatal("save failed")
	}
	dst := a.state.Add("Other", filepath.Join(t.TempDir(), "space"))
	if err := os.MkdirAll(filepath.Join(dst.Root, "素材"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst.SetAttachmentsDir("素材")
	if a.moveAcross(src.ID, a.doc.rel, dst.ID, "") == "" {
		t.Fatal("move failed")
	}
	if got := blockList(a); !slices.Contains(got, "image:素材/shared.png") {
		t.Fatalf("blocks = %q", got)
	}
	stored, err := os.ReadFile(filepath.Join(dst.Root, "素材", "shared.png"))
	if err != nil {
		t.Fatal("picture not copied into the destination's folder", err)
	}
	if len(stored) == 0 {
		t.Fatal("empty copy")
	}
	if _, err := os.Stat(filepath.Join(src.Root, "附件", "shared.png")); err != nil {
		t.Fatal("the source's copy was removed", err)
	}
	if a.doc.loadImage("素材/shared.png") == nil {
		t.Fatal("copied attachment does not load")
	}
}

func TestCrossSpaceNumbersConflictsInsideFolder(t *testing.T) {
	a, _ := dropApp(t)
	src := a.state.ActiveSpace()
	testAttachmentFolder(t, a, "附件")
	img, _ := pngFile(t, "shot.png")
	a.insertImages([]string{img}, -1)
	if !a.flush() {
		t.Fatal("save failed")
	}
	dst := a.state.Add("Other", filepath.Join(t.TempDir(), "space"))
	if err := os.MkdirAll(filepath.Join(dst.Root, "素材"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst.SetAttachmentsDir("素材")
	if err := os.WriteFile(filepath.Join(dst.Root, "素材", "shot.png"), []byte("occupied"), 0o644); err != nil {
		t.Fatal(err)
	}
	if a.moveAcross(src.ID, a.doc.rel, dst.ID, "") == "" {
		t.Fatal("move failed")
	}
	if got := blockList(a); !slices.Contains(got, "image:素材/shot-1.png") {
		t.Fatalf("blocks = %q", got)
	}
	if data, err := os.ReadFile(filepath.Join(dst.Root, "素材", "shot.png")); err != nil || string(data) != "occupied" {
		t.Fatal("the occupied picture was replaced")
	}
	if a.doc.loadImage("素材/shot-1.png") == nil {
		t.Fatal("numbered attachment does not load")
	}
}

func TestRelinkSkipsAmbiguousNames(t *testing.T) {
	a, _ := dropApp(t)
	sp := a.state.ActiveSpace()
	if err := vault.Write(sp.Root, "Other.md", "# 其他\n\n![pic](assets/pic.png)\n"); err != nil {
		t.Fatal(err)
	}
	// Two pictures of the same name in the folder: no document may be bound
	// to the other one's picture by guessing.
	for _, dir := range []string{"附件/a", "附件/b"} {
		if err := os.MkdirAll(filepath.Join(sp.Root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sp.Root, dir, "pic.png"), []byte(dir), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.setAttachmentsFolder(sp, filepath.Join(sp.Root, "附件"))
	if sp.AttachmentsDir != "附件" {
		t.Fatalf("folder = %q", sp.AttachmentsDir)
	}
	text, err := vault.Read(sp.Root, "Other.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "](assets/pic.png)") {
		t.Fatalf("an ambiguous name was written to disk: %q", text)
	}
}

func TestRelinkKeepsHistorySnapshot(t *testing.T) {
	a, _ := dropApp(t)
	sp := a.state.ActiveSpace()
	if err := vault.Write(sp.Root, "Other.md", "# 其他\n\n![pic](assets/pic.png)\n"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "附件/pic.png", "picture"); err != nil {
		t.Fatal(err)
	}
	a.setAttachmentsFolder(sp, filepath.Join(sp.Root, "附件"))
	a.filesChanged(sp.ID, []mygo.FileEvent{{Op: mygo.FileRename, OldPath: "assets/pic.png", Path: "附件/pic.png"}})
	text, err := vault.Read(sp.Root, "Other.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "](附件/pic.png)") {
		t.Fatalf("closed document was not relinked: %q", text)
	}
	versions, err := history.List(sp.Root, "Other.md")
	if err != nil || len(versions) == 0 {
		t.Fatalf("no version kept before the rewrite: %v, %v", versions, err)
	}
	before, err := history.Read(sp.Root, "Other.md", versions[0])
	if err != nil || !strings.Contains(before, "](assets/pic.png)") {
		t.Fatalf("the kept version is not the content as it was: %q, %v", before, err)
	}
}

func TestAttachmentsTouchedIgnoresOwnWrites(t *testing.T) {
	a, _ := newTestApp(t)
	sp := a.state.ActiveSpace()
	testAttachmentFolder(t, a, "附件")
	// ArcMD's own asset writes and atomic publishes never trigger a scan.
	a.markSelfWrite(filepath.Join(root(a), "附件", "one.png"))
	own := []mygo.FileEvent{
		{Op: mygo.FileCreate, Path: "附件/one.png"},
		{Op: mygo.FileWrite, Path: "附件/one.png"},
		{Op: mygo.FileRename, OldPath: ".Paste.png.123.tmp", Path: "附件/Paste.png"},
	}
	if a.attachmentsTouched(sp, own) {
		t.Fatal("ArcMD's own writes triggered a relink scan")
	}
	// A new picture does not identify where any old reference pointed.
	if a.attachmentsTouched(sp, []mygo.FileEvent{{Op: mygo.FileCreate, Path: "附件/other.png"}}) {
		t.Fatal("an external copy triggered a scan without a recorded move")
	}
	// A real move of the folder or of files into it does.
	if !a.attachmentsTouched(sp, []mygo.FileEvent{{Op: mygo.FileRename, OldPath: "assets", Path: "附件/assets"}}) {
		t.Fatal("the moved assets folder was missed")
	}
	if !a.attachmentsTouched(sp, []mygo.FileEvent{{Op: mygo.FileRename, OldPath: "附件", Path: "素材"}}) {
		t.Fatal("the renamed folder was missed")
	}
	// Documents being saved never trigger a scan, even at the space's root.
	sp.SetAttachmentsDir(".")
	defer sp.SetAttachmentsDir("")
	if a.attachmentsTouched(sp, []mygo.FileEvent{{Op: mygo.FileRename, OldPath: ".Welcome.md.1.tmp", Path: "Welcome.md"}}) {
		t.Fatal("an atomic save triggered a relink scan")
	}
	if a.attachmentsTouched(sp, []mygo.FileEvent{{Op: mygo.FileCreate, Path: "Welcome.md"}}) {
		t.Fatal("creating a document triggered a relink scan")
	}
}

func TestAttachmentFolderHiddenFromSidebar(t *testing.T) {
	a, _ := newTestApp(t)
	sp := a.state.ActiveSpace()
	testAttachmentFolder(t, a, "附件")
	if err := vault.Write(sp.Root, "附件/inside.md", "# Inside\n"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "附件/白板.excalidraw.svg", "<svg/>"); err != nil {
		t.Fatal(err)
	}
	nodes, err := readTree(sp)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.Path == "附件" || strings.HasPrefix(n.Path, "附件/") {
			t.Fatalf("the attachment folder shows in the sidebar: %+v", n)
		}
	}
}

func TestAttachmentFolderFollowsRenameAndForget(t *testing.T) {
	var s space.Space
	s.SetAttachmentsDir("Journal/附件")
	s.Renamed("Journal", "Notes")
	if s.AttachmentsDir != "Notes/附件" {
		t.Fatalf("after Renamed: %q", s.AttachmentsDir)
	}
	s.Renamed("Notes/附件", "Notes/素材")
	if s.AttachmentsDir != "Notes/素材" {
		t.Fatalf("after renaming the folder itself: %q", s.AttachmentsDir)
	}
	s.Forget("Notes/素材")
	if s.AttachmentsDir != "" {
		t.Fatalf("after Forget: %q", s.AttachmentsDir)
	}
}

func TestPaletteHasAttachmentCommands(t *testing.T) {
	a, tt := newTestApp(t)
	settle(tt)
	tt.Key(ui.Cmd, ui.KeyK)
	a.palette.query = "附件"
	tt.Frame()
	if !a.palette.open || !tt.HasText("选择附件目录…") || !tt.HasText("恢复默认附件目录") {
		t.Fatalf("command bar misses the attachment commands: %v", tt.Texts())
	}
}

func TestAttachmentsFolderRefusesHiddenAndOutside(t *testing.T) {
	a, _ := newTestApp(t)
	sp := a.state.ActiveSpace()
	inside := filepath.Join(sp.Root, ".git", "objects")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	a.setAttachmentsFolder(sp, inside)
	if sp.AttachmentsDir != "" {
		t.Fatalf("a hidden folder was kept: %q", sp.AttachmentsDir)
	}
	outside := t.TempDir()
	a.setAttachmentsFolder(sp, outside)
	if sp.AttachmentsDir != "" {
		t.Fatalf("an outside folder was kept: %q", sp.AttachmentsDir)
	}
}

func TestMissingImageResolutionRefreshes(t *testing.T) {
	a, _ := dropApp(t)
	sp := a.state.ActiveSpace()
	testAttachmentFolder(t, a, "附件")
	if err := vault.Write(sp.Root, "Ghost.md", "# Ghost\n\n![g](附件/ghost.png)\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Ghost.md")
	if res := a.imageResourceFor(a.doc, "附件/ghost.png"); res.Bitmap != nil {
		t.Fatal("the missing picture resolved")
	}
	// The picture turns up in the folder; the next file change must drop the
	// cached miss instead of hiding the picture forever.
	img, _ := pngFile(t, "ghost.png")
	data, err := os.ReadFile(img)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "附件/ghost.png", string(data)); err != nil {
		t.Fatal(err)
	}
	a.filesChanged(sp.ID, []mygo.FileEvent{{Op: mygo.FileCreate, Path: "附件/ghost.png"}})
	if res := a.imageResourceFor(a.doc, "附件/ghost.png"); res.Bitmap == nil {
		t.Fatal("the cached miss survived the file change")
	}
}
