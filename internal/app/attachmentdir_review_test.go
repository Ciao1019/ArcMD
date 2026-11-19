package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/history"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
)

// A direct basename candidate must not hide another candidate deeper in
// the attachment folder when no recorded move identifies the old picture.
func TestAttachmentReviewAmbiguousDirectAndNestedNames(t *testing.T) {
	a, _ := dropApp(t)
	sp := a.state.ActiveSpace()
	testAttachmentFolder(t, a, "附件")
	before := "# Old\n\n![shot](assets/shot.png)\n"
	for rel, text := range map[string]string{
		"Old/Note.md":            before,
		"附件/shot.png":            "another document's picture",
		"附件/Old/assets/shot.png": "the old document's picture",
	} {
		if err := vault.Write(sp.Root, rel, text); err != nil {
			t.Fatal(err)
		}
	}
	a.relinkAttachments(sp, nil)
	got, err := vault.Read(sp.Root, "Old/Note.md")
	if err != nil {
		t.Fatal(err)
	}
	if got != before {
		t.Fatalf("ambiguous reference was silently rewritten: %q", got)
	}
}

// A rename of B's picture provides no evidence that A's pre-existing broken
// reference should be repaired with it, even if its basename is unique.
func TestAttachmentReviewUnrelatedRenameKeepsBrokenReference(t *testing.T) {
	a, _ := dropApp(t)
	sp := a.state.ActiveSpace()
	testAttachmentFolder(t, a, "附件")
	before := "# Deleted picture\n\n![shot](assets/shot.png)\n"
	for rel, text := range map[string]string{
		"A/Note.md":     before,
		"附件/B/shot.png": "B's picture",
	} {
		if err := vault.Write(sp.Root, rel, text); err != nil {
			t.Fatal(err)
		}
	}
	a.filesChanged(sp.ID, []mygo.FileEvent{{Op: mygo.FileRename, OldPath: "B/assets/shot.png", Path: "附件/B/shot.png"}})
	got, err := vault.Read(sp.Root, "A/Note.md")
	if err != nil {
		t.Fatal(err)
	}
	if got != before {
		t.Fatalf("A was bound to B's unrelated picture: %q", got)
	}
}

// Automatic rewriting must leave the document alone if its recovery
// snapshot cannot be saved.
func TestAttachmentReviewHistoryFailurePreventsRewrite(t *testing.T) {
	a, _ := dropApp(t)
	sp := a.state.ActiveSpace()
	testAttachmentFolder(t, a, "附件")
	before := "# Original\n\n![shot](assets/shot.png)\n"
	if err := vault.Write(sp.Root, "Other.md", before); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "附件/shot.png", "picture"); err != nil {
		t.Fatal(err)
	}
	// Block only this document's history, leaving document writes possible.
	blocker := filepath.Join(sp.Root, ".arcmd", "history", "Other.md")
	if err := os.MkdirAll(filepath.Dir(blocker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocker, []byte("blocked"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.relinkClosedDoc(sp, "Other.md", before, map[string]string{"assets/shot.png": "附件/shot.png"})
	got, err := vault.Read(sp.Root, "Other.md")
	if err != nil {
		t.Fatal(err)
	}
	if got != before {
		versions, historyErr := history.List(sp.Root, "Other.md")
		t.Fatalf("rewritten without a recovery snapshot: %q; versions=%v, error=%v", got, versions, historyErr)
	}
}

// Deleting the configured folder through a filesystem event must clear the
// setting, just as deleting it inside ArcMD does.
func TestAttachmentReviewWatchClearsDeletedFolder(t *testing.T) {
	a, _ := dropApp(t)
	sp := a.state.ActiveSpace()
	folder := testAttachmentFolder(t, a, "附件")
	if err := os.Remove(folder); err != nil {
		t.Fatal(err)
	}
	a.filesChanged(sp.ID, []mygo.FileEvent{{Op: mygo.FileRemove, Path: "附件"}})
	if sp.AttachmentsDir != "" {
		t.Fatalf("deleted folder remains configured: %q", sp.AttachmentsDir)
	}
}

// Parent segments in standard Markdown must not preserve the old assets/
// prefix when copying into the destination space's configured folder.
func TestAttachmentReviewCrossSpaceStripsParentAssetsPrefix(t *testing.T) {
	a, _ := dropApp(t)
	src := a.state.ActiveSpace()
	_, data := pngFile(t, "shot.png")
	if err := vault.Write(src.Root, "assets/shot.png", string(data)); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(src.Root, "Notes/Deep.md", "# Deep\n\n![shot](../assets/shot.png)\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Notes/Deep.md")
	dst := a.state.Add("Other", filepath.Join(t.TempDir(), "space"))
	if err := os.MkdirAll(filepath.Join(dst.Root, "素材"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst.SetAttachmentsDir("素材")
	if a.moveAcross(src.ID, "Notes/Deep.md", dst.ID, "") == "" {
		t.Fatal("move failed")
	}
	got, err := vault.Read(dst.Root, "Deep.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "](素材/shot.png)") {
		t.Fatalf("old assets prefix leaked into destination: %q", got)
	}
}

// The cheap prefilter must retain a Markdown document whose only picture
// is its frontmatter cover.
func TestAttachmentReviewCoverOnlyDocumentRelinks(t *testing.T) {
	a, _ := dropApp(t)
	sp := a.state.ActiveSpace()
	testAttachmentFolder(t, a, "附件")
	before := "---\ncover: assets/cover.png\n---\n\n# Cover only\n"
	if err := vault.Write(sp.Root, "Cover.md", before); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "附件/cover.png", "picture"); err != nil {
		t.Fatal(err)
	}
	a.filesChanged(sp.ID, []mygo.FileEvent{{Op: mygo.FileRename, OldPath: "assets/cover.png", Path: "附件/cover.png"}})
	got, err := vault.Read(sp.Root, "Cover.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "cover: 附件/cover.png") {
		t.Fatalf("the cover was skipped despite its recorded move: %q", got)
	}
}

func TestAttachmentReviewSymlinkRootAcceptsCanonicalFolder(t *testing.T) {
	a, _ := dropApp(t)
	sp := a.state.ActiveSpace()
	canonical, err := filepath.EvalSymlinks(sp.Root)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "space-link")
	if err := os.Symlink(canonical, alias); err != nil {
		t.Fatal(err)
	}
	sp.Root = alias
	folder := filepath.Join(canonical, "附件")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	a.setAttachmentsFolder(sp, folder)
	if sp.AttachmentsDir != "附件" {
		t.Fatalf("canonical folder rejected for symlink root: %q", sp.AttachmentsDir)
	}
}

func TestAttachmentReviewRootSourceKeepsDestinationSubfolder(t *testing.T) {
	a, _ := dropApp(t)
	src := a.state.ActiveSpace()
	src.SetAttachmentsDir(".")
	img, _ := pngFile(t, "top.png")
	a.insertImages([]string{img}, -1)
	if !a.flush() {
		t.Fatal("save failed")
	}
	dst := a.state.Add("Other", filepath.Join(t.TempDir(), "space"))
	if err := os.MkdirAll(filepath.Join(dst.Root, "Archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dst.Root, "素材"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst.SetAttachmentsDir("素材")
	to := a.moveAcross(src.ID, a.doc.rel, dst.ID, "Archive")
	if to != "Archive/Drop.arcmd" {
		t.Fatalf("destination subfolder was lost: %q", to)
	}
	urls := a.doc.ed.ImageURLs()
	if len(urls) != 1 || urls[0] != "../素材/top.png" || a.doc.loadImage(urls[0]) == nil {
		t.Fatalf("moved image does not resolve from destination subfolder: %v", urls)
	}
}

func TestAttachmentReviewFolderSettingRefreshesVisibleTree(t *testing.T) {
	for _, reset := range []bool{false, true} {
		name := "choose"
		if reset {
			name = "reset"
		}
		t.Run(name, func(t *testing.T) {
			a, tt := dropApp(t)
			sp := a.state.ActiveSpace()
			if err := vault.Write(sp.Root, "附件/Sketch.excalidraw.svg", "<svg/>"); err != nil {
				t.Fatal(err)
			}
			if reset {
				sp.SetAttachmentsDir("附件")
			}
			a.refreshTree()
			settle(tt)
			if reset {
				a.setAttachmentsFolder(sp, "")
			} else {
				a.setAttachmentsFolder(sp, filepath.Join(sp.Root, "附件"))
			}
			settle(tt)
			visible := vault.Find(a.tree, "附件") != nil
			if visible != reset {
				t.Fatalf("visible tree did not follow setting: reset=%v, visible=%v", reset, visible)
			}
		})
	}
}

// A scan returns proposals to the UI thread, where documents opened or
// changed since the scan began must be left alone.
func TestAttachmentBackgroundRelinkGuards(t *testing.T) {
	for _, mode := range []string{"unchanged", "edited-on-disk", "opened", "folder-reset"} {
		t.Run(mode, func(t *testing.T) {
			a, _ := dropApp(t)
			sp := a.state.ActiveSpace()
			testAttachmentFolder(t, a, "附件")
			before := "# Other\n\n![shot](assets/shot.png)\n"
			if err := vault.Write(sp.Root, "Other.md", before); err != nil {
				t.Fatal(err)
			}
			if err := vault.Write(sp.Root, "附件/shot.png", "picture"); err != nil {
				t.Fatal(err)
			}
			updates := make(chan func(), 4)
			a.runAsync = func(fn func()) { updates <- fn }
			defer func() { a.runAsync = nil }()
			a.relinkAttachments(sp, map[string]string{"assets/shot.png": "附件/shot.png"})
			apply := attachmentUpdate(t, updates)
			want := before
			switch mode {
			case "unchanged":
				want = "# Other\n\n![shot](附件/shot.png)\n"
			case "edited-on-disk":
				want = "# Someone else's edit\n"
				if err := vault.Write(sp.Root, "Other.md", want); err != nil {
					t.Fatal(err)
				}
			case "opened":
				a.open("Other.md")
				a.doc.ed.ReplaceText(nil, "unsaved ")
			case "folder-reset":
				a.setAttachmentsFolder(sp, "")
			}
			apply()
			attachmentUpdate(t, updates)() // finish the background scan
			got, err := vault.Read(sp.Root, "Other.md")
			if err != nil || got != want {
				t.Fatalf("background result overwrote newer state: %q, want %q, error=%v", got, want, err)
			}
			if mode == "opened" && (!a.doc.dirty() || !strings.Contains(a.doc.ed.Text(), "unsaved")) {
				t.Fatal("the opened editor lost its unsaved edit")
			}
			versions, err := history.List(sp.Root, "Other.md")
			if err != nil || (len(versions) > 0) != (mode == "unchanged") {
				t.Fatalf("history changed without a valid rewrite: %v, %v", versions, err)
			}
			if len(a.attachmentRelinks) != 0 {
				t.Fatal("the completed job was retained")
			}
		})
	}
}

func attachmentUpdate(t *testing.T, updates <-chan func()) func() {
	t.Helper()
	select {
	case fn := <-updates:
		return fn
	case <-time.After(5 * time.Second):
		t.Fatal("background attachment scan did not return")
		return nil
	}
}

func TestAttachmentBackgroundRelinkCoalescesMoves(t *testing.T) {
	a, _ := dropApp(t)
	sp := a.state.ActiveSpace()
	testAttachmentFolder(t, a, "附件")
	if err := vault.Write(sp.Root, "Other.md", "# Other\n\n![shot](assets/shot.png)\n"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "附件/shot.png", "picture"); err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 8)
	a.runAsync = func(fn func()) { updates <- fn }
	defer func() { a.runAsync = nil }()
	a.relinkAttachments(sp, map[string]string{"assets/shot.png": "附件/shot.png"})
	first := attachmentUpdate(t, updates)
	if err := os.Rename(filepath.Join(sp.Root, "附件", "shot.png"), filepath.Join(sp.Root, "附件", "final.png")); err != nil {
		t.Fatal(err)
	}
	a.relinkAttachments(sp, map[string]string{"附件/shot.png": "附件/final.png"})
	first()
	for len(a.attachmentRelinks) > 0 {
		attachmentUpdate(t, updates)()
	}
	got, err := vault.Read(sp.Root, "Other.md")
	if err != nil || !strings.Contains(got, "](附件/final.png)") {
		t.Fatalf("the move received during a scan was lost: %q, %v", got, err)
	}
}

func TestAttachmentBackgroundRelinkDoesNotReuseSourcePath(t *testing.T) {
	a, _ := dropApp(t)
	sp := a.state.ActiveSpace()
	testAttachmentFolder(t, a, "附件")
	if err := vault.Write(sp.Root, "Other.md", "# Other\n\n![shot](assets/shot.png)\n"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first.png", "second.png"} {
		if err := vault.Write(sp.Root, "附件/"+name, name); err != nil {
			t.Fatal(err)
		}
	}
	updates := make(chan func(), 8)
	a.runAsync = func(fn func()) { updates <- fn }
	defer func() { a.runAsync = nil }()
	a.relinkAttachments(sp, map[string]string{"assets/shot.png": "附件/first.png"})
	first := attachmentUpdate(t, updates)
	// A different file appeared at the same old path and was moved too.
	a.relinkAttachments(sp, map[string]string{"assets/shot.png": "附件/second.png"})
	// A concurrent edit invalidates the first proposal without changing its URL.
	before := "# New title\n\n![shot](assets/shot.png)\n"
	if err := vault.Write(sp.Root, "Other.md", before); err != nil {
		t.Fatal(err)
	}
	first()
	for len(a.attachmentRelinks) > 0 {
		attachmentUpdate(t, updates)()
	}
	got, err := vault.Read(sp.Root, "Other.md")
	if err != nil || got != before {
		t.Fatalf("a reused source path was silently bound to a different file: %q, %v", got, err)
	}
}

func TestAttachmentBackgroundRelinkFollowsFolderRename(t *testing.T) {
	a, _ := dropApp(t)
	sp := a.state.ActiveSpace()
	testAttachmentFolder(t, a, "附件")
	if err := vault.Write(sp.Root, "Other.md", "# Other\n\n![shot](assets/shot.png)\n"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "附件/shot.png", "picture"); err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 8)
	a.runAsync = func(fn func()) { updates <- fn }
	defer func() { a.runAsync = nil }()
	a.relinkAttachments(sp, map[string]string{"assets/shot.png": "附件/shot.png"})
	first := attachmentUpdate(t, updates)
	if err := os.Rename(filepath.Join(sp.Root, "附件"), filepath.Join(sp.Root, "素材")); err != nil {
		t.Fatal(err)
	}
	a.filesChanged(sp.ID, []mygo.FileEvent{{Op: mygo.FileRename, OldPath: "附件", Path: "素材"}})
	first()
	for len(a.attachmentRelinks) > 0 {
		attachmentUpdate(t, updates)()
	}
	got, err := vault.Read(sp.Root, "Other.md")
	if err != nil || !strings.Contains(got, "](素材/shot.png)") || sp.AttachmentsDir != "素材" {
		t.Fatalf("the renamed folder lost pending moves: %q, folder=%q, error=%v", got, sp.AttachmentsDir, err)
	}
}

func TestAttachmentRecordedMovesUseDocumentDirectory(t *testing.T) {
	a, _ := dropApp(t)
	sp := a.state.ActiveSpace()
	testAttachmentFolder(t, a, "附件")
	if err := vault.Write(sp.Root, "附件/shot.png", "another picture"); err != nil {
		t.Fatal(err)
	}
	before := "# Lost local picture\n\n![shot](assets/shot.png)\n"
	if err := vault.Write(sp.Root, "A/Note.md", before); err != nil {
		t.Fatal(err)
	}
	a.relinkAttachments(sp, map[string]string{"assets/shot.png": "附件/shot.png"})
	got, err := vault.Read(sp.Root, "A/Note.md")
	if err != nil || got != before {
		t.Fatalf("a root-level move rebound a broken document-local reference: %q, %v", got, err)
	}
}

func TestAttachmentMoveTargetChainsAndCycles(t *testing.T) {
	for _, tc := range []struct {
		name, old, want string
		moves           map[string]string
	}{
		{"chain", "assets/shot.png", "附件/final.png", map[string]string{"assets": "附件/assets", "附件/assets/shot.png": "附件/final.png"}},
		{"nested-target", "assets/shot.png", "assets/sub/shot.png", map[string]string{"assets": "assets/sub"}},
		{"specific", "assets/shot.png", "附件/final.png", map[string]string{"assets": "附件/assets", "assets/shot.png": "附件/final.png"}},
		{"cycle", "a/shot.png", "", map[string]string{"a": "b", "b": "a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := moveTarget(tc.moves, tc.old)
			if got != tc.want || ok != (tc.want != "") {
				t.Fatalf("move target = %q, %v; want %q", got, ok, tc.want)
			}
		})
	}
}
