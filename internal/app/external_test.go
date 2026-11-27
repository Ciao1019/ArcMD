package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func externalFile(t *testing.T, name, content string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return full
}

func TestExternalCollectionDoesNotImportFolders(t *testing.T) {
	a, tt := newTestApp(t)
	first := externalFile(t, "README.md", "# First external\n\nOpened unique content")
	second := externalFile(t, "README.md", "# Second external")
	parent := filepath.Dir(first)
	if err := os.Mkdir(filepath.Join(parent, "Empty folder"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "Unopened.md"), []byte("secret sibling text"), 0600); err != nil {
		t.Fatal(err)
	}
	count := len(a.state.Spaces)
	for _, file := range []string{first, second, first} {
		if !a.openSystemFile(file) {
			t.Fatalf("open %s", file)
		}
	}
	sp := a.doc.space
	if !sp.IsExternal() || len(a.state.Spaces) != count+1 || len(sp.ExternalFiles) != 2 || len(a.tree) != 2 {
		t.Fatalf("unexpected collection: %+v tree=%v", sp, a.tree)
	}
	for _, n := range a.tree {
		if n.Dir {
			t.Fatal("imported a folder")
		}
	}
	if a.doc.fullPath() != first {
		t.Fatal("reopening selected the wrong same-named file")
	}
	a.openSystemFile(second)
	a.renameIn(sp, a.doc.rel)
	if a.newName != "README" {
		t.Fatal("rename used the list alias as a disk filename")
	}
	a.commitRename()
	if a.doc.fullPath() != second || len(sp.ExternalFiles) != 2 {
		t.Fatal("unchanged rename altered the file")
	}
	a.openSystemFile(first)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if !tt.HasText("README") || !tt.HasText("README 2") || tt.HasText("Empty folder") || !tt.HasText("打开文件…") {
		t.Fatalf("wrong sidebar: %v", tt.Texts())
	}
	a.refreshSearch()
	if len(a.textHits("unique content")) != 1 || len(a.textHits("secret sibling")) != 0 {
		t.Fatal("search escaped collection")
	}
	fs := a.qaSpace(sp.ID)
	docs, err := fs.Docs()
	if err != nil || len(docs) != 2 {
		t.Fatalf("QA docs %v: %v", docs, err)
	}
	if _, err := fs.Page("Unopened.md"); err == nil {
		t.Fatal("QA read an unopened sibling")
	}
	if _, err := resolveFile(sp, "Unopened.md"); err == nil {
		t.Fatal("unregistered file resolved")
	}
	a.loadLibrary()
	if len(a.library.trees[sp.ID]) != 2 {
		t.Fatal("library did not use collection")
	}
	loaded, err := New(a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.doc == nil || loaded.doc.fullPath() != first || len(loaded.tree) != 2 {
		t.Fatal("collection did not survive restart")
	}
}

func TestExternalInPlaceSaveAndRename(t *testing.T) {
	a, _ := newTestApp(t)
	file := externalFile(t, "External.md", "# Original\n\nBody\n")
	if !a.openSystemFile(file) {
		t.Fatal("open")
	}
	sp := a.doc.space
	a.toggleFavorite(sp, a.doc.rel)
	a.doc.ed.ReplaceText(nil, "Edited ")
	if !a.flush() {
		t.Fatal("save")
	}
	if a.doc.fullPath() != file || len(sp.ExternalFiles) != 1 || !a.state.IsFavorite(sp.ID, a.doc.rel) {
		t.Fatal("in-place save lost identity")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(file), "External.arcmd")); !os.IsNotExist(err) {
		t.Fatal("external Markdown save created a .arcmd file")
	}
	if b, err := os.ReadFile(file); err != nil || !strings.Contains(string(b), "Edited") {
		t.Fatalf("external file %s: %v", b, err)
	}
	a.renameIn(sp, a.doc.rel)
	a.newName = "Renamed"
	a.commitRename()
	if a.doc.fullPath() != filepath.Join(filepath.Dir(file), "Renamed.md") || len(sp.ExternalFiles) != 1 {
		t.Fatal("rename lost file")
	}
	a.exportMarkdown()
	if _, err := os.Stat(filepath.Join(filepath.Dir(file), "Renamed Export.md")); err != nil {
		t.Fatal(err)
	}
}

func TestExternalLinkedFileOpensOnlyWhenFollowed(t *testing.T) {
	a, _ := newTestApp(t)
	file := externalFile(t, "Start.md", "[Next](Next.md)")
	next := filepath.Join(filepath.Dir(file), "Next.md")
	if err := os.WriteFile(next, []byte("# Next"), 0600); err != nil {
		t.Fatal(err)
	}
	if !a.openSystemFile(file) || len(a.tree) != 1 {
		t.Fatal("opened siblings automatically")
	}
	a.followLink("Next.md")
	if a.doc.fullPath() != next || len(a.tree) != 2 {
		t.Fatal("linked file did not open in collection")
	}
}

func TestExternalRenameSavesDirtyMarkdown(t *testing.T) {
	a, _ := newTestApp(t)
	file := externalFile(t, "External.md", "# Original")
	if !a.openSystemFile(file) {
		t.Fatal("open")
	}
	a.doc.ed.ReplaceText(nil, "Edited ")
	a.renameIn(a.doc.space, a.doc.rel)
	a.newName = "Renamed"
	a.commitRename()
	if a.doc.fullPath() != filepath.Join(filepath.Dir(file), "Renamed.md") || len(a.doc.space.ExternalFiles) != 1 {
		t.Fatal("rename did not follow the saved document")
	}
	if data, err := os.ReadFile(a.doc.fullPath()); err != nil || !strings.Contains(string(data), "Edited") {
		t.Fatal("renamed file lost changes", err)
	}
}

func TestExternalRemoveFromMenuKeepsFilesAndSpace(t *testing.T) {
	a, tt := newTestApp(t)
	file := externalFile(t, "External.md", "# Original")
	if !a.openSystemFile(file) {
		t.Fatal("open")
	}
	sp := a.doc.space
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if err := tt.RightClick("External"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("从外部空间移除"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("removed original", err)
	}
	if a.doc != nil || len(sp.ExternalFiles) != 0 || !tt.HasText("打开的外部文档会显示在这里，文件保留在原位置。") {
		t.Fatal("remove did not close entry")
	}
	a.removeSpace(sp.ID)
	if a.state.Get(sp.ID) != sp {
		t.Fatal("built-in space removed")
	}
	if err := a.state.Remove(sp.ID); err != space.ErrBuiltIn {
		t.Fatal("model allowed removal", err)
	}
	a.openSettings(tabSpaces)
	tt.Frame()
	if err := tt.Click("从 ArcMD 移除 外部空间"); err == nil && a.state.Get(sp.ID) == nil {
		t.Fatal("settings removed built-in")
	}
	loaded, err := New(a.cfg)
	if err != nil || loaded.state.Get(sp.ID) == nil {
		t.Fatal("empty collection not persisted", err)
	}
}

func TestExternalRemoveSavesDirtyMarkdown(t *testing.T) {
	a, _ := newTestApp(t)
	file := externalFile(t, "External.md", "# Original")
	a.openSystemFile(file)
	sp, rel := a.doc.space, a.doc.rel
	a.doc.ed.ReplaceText(nil, "Saved ")
	a.forgetExternal(sp, rel)
	if a.doc != nil || len(sp.ExternalFiles) != 0 {
		t.Fatal("forget left an entry open")
	}
	if b, err := os.ReadFile(file); err != nil || !strings.Contains(string(b), "Saved") {
		t.Fatal("edits lost", err)
	}
}

func TestExternalMoveToFolderSpaceKeepsDocument(t *testing.T) {
	a, _ := newTestApp(t)
	dst := a.state.ActiveSpace()
	file := externalFile(t, "External.md", "# Original")
	if !a.openSystemFile(file) {
		t.Fatal("open")
	}
	sp, rel := a.doc.space, a.doc.rel
	a.doc.ed.ReplaceText(nil, "Edited ")
	to := a.moveAcross(sp.ID, rel, dst.ID, "")
	if to != "External.md" || a.doc.space != dst || len(sp.ExternalFiles) != 0 {
		t.Fatal("failed to move edited external document")
	}
	if !strings.Contains(a.doc.text(), "Edited") {
		t.Fatal("lost edits")
	}
	if data, err := os.ReadFile(filepath.Join(dst.Root, "External.md")); err != nil || !strings.Contains(string(data), "Edited") {
		t.Fatal("moved file missing edits", err)
	}
	if a.moveAcross(dst.ID, to, sp.ID, "") != "" {
		t.Fatal("collection accepted a physical move")
	}
}

func TestExternalWatchAndConflict(t *testing.T) {
	a, tt, w := watchApp(t)
	file := externalFile(t, "External.md", "")
	if err := writeNative(filepath.Dir(file), filepath.Base(file), "# Original"); err != nil {
		t.Fatal(err)
	}
	if !a.openSystemFile(file) {
		t.Fatal("open")
	}
	tt.Frame()
	changed := w.changed[file]
	if changed == nil || w.changed[filepath.Dir(file)] != nil {
		t.Fatal("must watch file, not parent directory")
	}
	if err := writeNative(filepath.Dir(file), filepath.Base(file), "# Changed elsewhere"); err != nil {
		t.Fatal(err)
	}
	changed([]mygo.FileEvent{{Op: mygo.FileWrite, Path: filepath.Base(file)}})
	if !strings.Contains(a.doc.text(), "Changed elsewhere") {
		t.Fatal("did not reload")
	}
	a.doc.ed.ReplaceText(nil, "Mine ")
	if err := writeNative(filepath.Dir(file), filepath.Base(file), "# New disk version"); err != nil {
		t.Fatal(err)
	}
	changed([]mygo.FileEvent{{Op: mygo.FileWrite, Path: filepath.Base(file)}})
	if !a.doc.conflict {
		t.Fatal("conflict not detected")
	}
	sp, rel := a.doc.space, a.doc.rel
	a.forgetExternal(sp, rel)
	if a.doc == nil || len(sp.ExternalFiles) != 1 {
		t.Fatal("conflict bypassed on close")
	}
	a.useDisk()
	if !strings.Contains(a.doc.text(), "New disk version") {
		t.Fatal("disk version not used")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	changed([]mygo.FileEvent{{Op: mygo.FileRemove, Path: filepath.Base(file)}})
	if a.doc != nil || len(sp.ExternalFiles) != 1 {
		t.Fatal("missing entry not retained")
	}
	a.forgetExternal(sp, rel)
	if len(sp.ExternalFiles) != 0 {
		t.Fatal("cannot remove missing entry")
	}
}

func TestExternalSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT for external space")
	for _, size := range []struct {
		name string
		w, h int
		dark bool
	}{{"external", 1240, 800, false}, {"external-dark", 1240, 800, true}, {"external-narrow", 760, 600, false}, {"external-empty", 1240, 800, false}} {
		t.Run(size.name, func(t *testing.T) {
			a, tt := newTestApp(t)
			file := externalFile(t, "旅行计划.md", "# 旅行计划\n\n从任何文件夹打开的文档，都可以在这里继续阅读和编辑。\n\n## 周末安排\n\n- 沿着海边散步\n- 找一家安静的咖啡馆\n")
			a.openSystemFile(file)
			a.openSystemFile(externalFile(t, "README.md", "# 项目说明\n\n一份来自下载文件夹的文档。"))
			a.openSystemFile(file)
			if size.name == "external-empty" {
				sp := a.doc.space
				for rel := range sp.ExternalFiles {
					a.forgetExternal(sp, rel)
				}
			}
			tt.SetSize(size.w, size.h)
			tt.SetDark(size.dark)
			if size.dark {
				th := space.ThemeOf(a.state.ActiveSpace())
				th.Appearance = space.AppearanceDark
				a.state.ActiveSpace().SetTheme(th)
			}
			tt.Frame()
			tt.Advance(time.Second)
			tt.Frame()
			tt.Frame()
			savePNG(t, filepath.Join(dir, size.name+".png"), tt)
		})
	}
}

func TestExternalCleanMissingBarAndBatch(t *testing.T) {
	a, tt := newTestApp(t)
	kept := externalFile(t, "旅行计划.md", "# 旅行计划\n\nSurvivingBodyToken")
	readme := externalFile(t, "README.md", "# README\n\nAnotherSurvivor")
	goneMD := externalFile(t, "临时草稿.md", "# 临时草稿\n\nDeletedDraftToken")
	gonePin := externalFile(t, "会议纪要.md", "# 会议纪要\n\nDeletedPinToken")
	goneHTML := externalFile(t, "规格说明书.html", "<html><body><h1>Spec</h1><p>DeletedSpecToken</p></body></html>")

	for _, f := range []string{kept, readme, goneMD, gonePin, goneHTML} {
		if !a.openSystemFile(f) {
			t.Fatalf("open %s", f)
		}
	}
	sp := a.state.Get(space.ExternalID)
	if sp == nil || len(sp.ExternalFiles) != 5 {
		t.Fatalf("unexpected external space: %+v", sp)
	}
	a.togglePin(sp, "会议纪要.md")
	a.toggleFavorite(sp, "规格说明书.html")
	// Keep an existing document open while the others vanish.
	a.openSystemFile(kept)
	a.refreshSearch()
	if len(a.textHits("DeletedDraftToken")) != 1 {
		t.Fatal("deleted draft not indexed before removal")
	}

	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if tt.HasText("个文件已不存在") || tt.HasText("清理") {
		t.Fatalf("cleanup bar shown when no files are missing: %v", tt.Texts())
	}

	for _, f := range []string{goneMD, gonePin, goneHTML} {
		if err := os.Remove(f); err != nil {
			t.Fatal(err)
		}
	}
	a.refreshTree()
	if got := countMissingNodes(a.tree); got != 3 {
		t.Fatalf("missing count = %d, want 3", got)
	}
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if !tt.HasText("3 个文件已不存在") || !tt.HasText("清理") {
		t.Fatalf("missing cleanup bar not visible: %v", tt.Texts())
	}

	a.notices = nil
	if err := tt.Click("清理不存在的文件"); err != nil {
		t.Fatalf("click cleanup button: %v", err)
	}
	if len(sp.ExternalFiles) != 2 {
		t.Fatalf("ExternalFiles after clean = %v, want 2", sp.ExternalFiles)
	}
	if sp.IsPinned("会议纪要.md") {
		t.Fatal("missing pinned file was not unpinned")
	}
	if a.state.IsFavorite(sp.ID, "规格说明书.html") {
		t.Fatal("missing favorite file was not removed from favorites")
	}
	if a.doc == nil || a.doc.fullPath() != kept {
		t.Fatalf("active surviving document changed: %v", a.doc)
	}
	for _, f := range []string{kept, readme} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("surviving file touched on disk: %s: %v", f, err)
		}
	}
	if len(a.textHits("DeletedDraftToken")) != 0 || len(a.textHits("SurvivingBodyToken")) != 1 {
		t.Fatal("search index not updated after cleanup")
	}
	tt.Frame()
	if !tt.HasText("已清理外部空间中 3 个不存在的文件。") {
		t.Fatalf("expected cleanup toast in UI: %v", tt.Texts())
	}

	tt.Advance(time.Second)
	tt.Frame()
	if tt.HasText("个文件已不存在") || tt.HasText("清理不存在的文件") {
		t.Fatalf("cleanup bar stayed visible after clean: %v", tt.Texts())
	}

	loaded, err := New(a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if ext := loaded.state.Get(space.ExternalID); ext == nil || len(ext.ExternalFiles) != 2 {
		t.Fatalf("cleaned state did not persist: %+v", ext)
	}
}

func TestExternalCleanMissingSkipsDirtyOpenDocument(t *testing.T) {
	a, _ := newTestApp(t)
	dirtyFile := externalFile(t, "Dirty.md", "# Dirty")
	goneFile := externalFile(t, "Gone.md", "# Gone")

	if !a.openSystemFile(goneFile) || !a.openSystemFile(dirtyFile) {
		t.Fatal("open external files")
	}
	sp := a.state.Get(space.ExternalID)
	a.doc.ed.ReplaceText(nil, "Unsaved ")
	if !a.doc.dirty() {
		t.Fatal("expected dirty document")
	}

	if err := os.Remove(dirtyFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(goneFile); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()

	a.notices = nil
	removed, skipped := a.cleanMissingExternal(sp)
	if removed != 1 || skipped != 1 {
		t.Fatalf("cleanMissingExternal = (%d, %d), want (1, 1)", removed, skipped)
	}
	if a.doc == nil || a.doc.rel != "Dirty.md" || !strings.Contains(a.doc.text(), "Unsaved") {
		t.Fatalf("dirty open document was not preserved: %v", a.doc)
	}
	if _, ok := sp.ExternalFiles["Dirty.md"]; !ok {
		t.Fatal("dirty entry removed from ExternalFiles")
	}
	if _, ok := sp.ExternalFiles["Gone.md"]; ok {
		t.Fatal("clean missing entry not removed")
	}
	if len(a.notices) == 0 || !strings.Contains(a.notices[len(a.notices)-1], "已清理外部空间中 1 个不存在的文件，另有 1 个因存在未保存修改已跳过。") {
		t.Fatalf("unexpected mixed toast: %v", a.notices)
	}

	a.notices = nil
	removed, skipped = a.cleanMissingExternal(sp)
	if removed != 0 || skipped != 1 {
		t.Fatalf("second cleanMissingExternal = (%d, %d), want (0, 1)", removed, skipped)
	}
	if len(a.notices) == 0 || !strings.Contains(a.notices[len(a.notices)-1], "1 个不存在的文件有未保存修改，已跳过。") {
		t.Fatalf("unexpected skip-only toast: %v", a.notices)
	}
}

func TestExternalCleanMissingFromMenuAndPalette(t *testing.T) {
	a, tt := newTestApp(t)
	normalSpace := a.state.ActiveSpace()
	kept := externalFile(t, "Keep.md", "# Keep")
	gone1 := externalFile(t, "Stale1.md", "# Stale 1")
	gone2 := externalFile(t, "Stale2.md", "# Stale 2")
	gone3 := externalFile(t, "Stale3.pdf", "%PDF-1.4\n%%EOF\n")

	for _, f := range []string{kept, gone1, gone2} {
		if !a.openSystemFile(f) {
			t.Fatalf("open %s", f)
		}
	}
	sp := a.state.Get(space.ExternalID)
	sp.AddExternal(gone3)
	a.openSystemFile(kept)
	a.refreshTree()

	// 1. Delete gone1 WITHOUT refreshTree: space menu live-scans and cleans it.
	if err := os.Remove(gone1); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if err := tt.RightClick("切换到 " + sp.Name); err != nil {
		t.Fatalf("right-click external space icon: %v", err)
	}
	a.notices = nil
	if err := tt.ChooseMenuItem("清理不存在的文件"); err != nil {
		t.Fatalf("choose clean from space bar menu: %v", err)
	}
	if _, ok := sp.ExternalFiles["Stale1.md"]; ok {
		t.Fatal("live scan did not remove Stale1.md")
	}
	tt.Frame()
	if !tt.HasText("已清理外部空间中 1 个不存在的文件。") {
		t.Fatalf("expected menu cleanup toast in UI: %v", tt.Texts())
	}

	// 2. When nothing is missing, menu reports no missing files cleanly.
	a.notices = nil
	if err := tt.RightClick("切换到 " + sp.Name); err != nil {
		t.Fatalf("right-click external space icon again: %v", err)
	}
	if err := tt.ChooseMenuItem("清理不存在的文件"); err != nil {
		t.Fatalf("choose clean when none missing: %v", err)
	}
	tt.Frame()
	if !tt.HasText("外部空间中没有不存在的文件。") {
		t.Fatalf("expected none-missing toast in UI: %v", tt.Texts())
	}

	// 3. Switch away to normal space, delete gone2 & gone3, invoke via Command Palette.
	a.switchSpace(normalSpace.ID)
	if err := os.Remove(gone2); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gone3); err != nil {
		t.Fatal(err)
	}
	a.notices = nil
	a.openPalette()
	a.palette.query = "清理外部空间中不存在的文件"
	tt.Frame()
	if !tt.HasText("清理外部空间中不存在的文件") {
		t.Fatalf("palette missing cleanup command: %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	if len(sp.ExternalFiles) != 1 || sp.ExternalFiles["Keep.md"] != kept {
		t.Fatalf("palette cleanup left unexpected entries: %+v", sp.ExternalFiles)
	}
	tt.Frame()
	if !tt.HasText("已清理外部空间中 2 个不存在的文件。") {
		t.Fatalf("expected palette cleanup toast in UI: %v", tt.Texts())
	}
	a.switchSpace(sp.ID)
	if countMissingNodes(a.tree) != 0 || len(a.tree) != 1 {
		t.Fatalf("external space tree stale after cross-space cleanup: %+v", a.tree)
	}
}

func TestExternalCleanMissingUpdatesOpenLibrary(t *testing.T) {
	a, tt := newTestApp(t)
	normalSpace := a.state.ActiveSpace()
	kept := externalFile(t, "Keep.md", "# Keep")
	gone1 := externalFile(t, "Gone1.md", "# Gone 1")
	gone2 := externalFile(t, "Gone2.md", "# Gone 2")

	for _, f := range []string{kept, gone1, gone2} {
		if !a.openSystemFile(f) {
			t.Fatalf("open %s", f)
		}
	}
	sp := a.state.Get(space.ExternalID)
	a.openSystemFile(kept)

	// 1. Delete gone1, open Library Spaces while External is active, and clean via the card menu.
	if err := os.Remove(gone1); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.openLibrary(libSpaces)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if got := a.cachedMissingExternal(sp); got != 1 {
		t.Fatalf("cachedMissingExternal = %d, want 1", got)
	}
	if err := tt.Click(sp.Name + " 的菜单"); err != nil {
		t.Fatalf("open external card menu in library: %v", err)
	}
	if err := tt.ChooseMenuItem("清理不存在的文件 (1)"); err != nil {
		t.Fatalf("choose clean from library card menu: %v", err)
	}
	tt.Frame()
	if got := len(a.library.trees[sp.ID]); got != 2 {
		t.Fatalf("library tree len after card-menu clean = %d, want 2", got)
	}
	if !tt.HasText("Keep") || !tt.HasText("Gone2") || tt.HasText("Gone1") {
		t.Fatalf("library card stale after card-menu clean: %v", tt.Texts())
	}

	// 2. Switch to a normal space, keep Library Spaces open, delete gone2, and clean via Command Palette.
	a.closeLibrary()
	a.switchSpace(normalSpace.ID)
	if err := os.Remove(gone2); err != nil {
		t.Fatal(err)
	}
	a.openLibrary(libSpaces)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if got := a.cachedMissingExternal(sp); got != 1 {
		t.Fatalf("cachedMissingExternal from library cache = %d, want 1", got)
	}
	a.openPalette()
	a.palette.query = "清理外部空间中不存在的文件"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got := len(a.library.trees[sp.ID]); got != 1 || a.library.trees[sp.ID][0].Path != "Keep.md" {
		t.Fatalf("library tree after palette clean = %+v, want [Keep.md]", a.library.trees[sp.ID])
	}
	if !tt.HasText("Keep") || tt.HasText("Gone2") {
		t.Fatalf("library card blanked or stale after palette clean: %v", tt.Texts())
	}
}

func TestExternalOpenMissingRefreshesTreeAndBar(t *testing.T) {
	a, tt := newTestApp(t)
	kept := externalFile(t, "Keep.md", "# Keep")
	goneMD := externalFile(t, "Vanished.md", "# Vanished")
	replacedPDF := externalFile(t, "Replaced.pdf", "%PDF-1.4\n%%EOF\n")

	if !a.openSystemFile(goneMD) || !a.openSystemFile(kept) {
		t.Fatal("open external files")
	}
	sp := a.state.Get(space.ExternalID)
	sp.AddExternal(replacedPDF)
	a.refreshTree()

	// Remove the Markdown file and replace the PDF with a directory without calling refreshTree.
	if err := os.Remove(goneMD); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(replacedPDF); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(replacedPDF, 0700); err != nil {
		t.Fatal(err)
	}
	if got := countMissingNodes(a.tree); got != 0 {
		t.Fatalf("expected stale cached tree before click, got %d missing", got)
	}

	a.notices = nil
	a.open("Vanished.md")
	if a.doc == nil || a.doc.fullPath() != kept {
		t.Fatalf("surviving open document changed after failed open: %v", a.doc)
	}
	if len(a.notices) == 0 || !strings.Contains(a.notices[len(a.notices)-1], "文件已被移动或删除：") {
		t.Fatalf("expected missing notice, got %v", a.notices)
	}
	if got := countMissingNodes(a.tree); got != 2 {
		t.Fatalf("missing count after failed open = %d, want 2", got)
	}
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if !tt.HasText("2 个文件已不存在") || !tt.HasText("清理") {
		t.Fatalf("cleanup bar not shown after failed open: %v", tt.Texts())
	}

	a.notices = nil
	a.open("Replaced.pdf")
	if a.doc == nil || a.doc.fullPath() != kept {
		t.Fatalf("surviving open document changed after failed dir-replaced PDF open: %v", a.doc)
	}
	if len(a.notices) == 0 || !strings.Contains(a.notices[len(a.notices)-1], "文件已被移动或删除：") {
		t.Fatalf("expected missing notice for dir-replaced PDF, got %v", a.notices)
	}
}

func TestExternalMissingSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT for external missing snapshot")
	for _, tc := range []struct {
		name string
		dark bool
	}{{"external-missing", false}, {"external-missing-dark", true}} {
		t.Run(tc.name, func(t *testing.T) {
			a, tt := newTestApp(t)
			kept := externalFile(t, "旅行计划.md", "# 旅行计划\n\n从任何文件夹打开的文档，都可以在这里继续阅读和编辑。\n\n当外部文件被移动或删除后，侧边栏会自动淡显失效条目并提示一键清理。\n")
			readme := externalFile(t, "README.md", "# 项目说明\n\n一份来自下载文件夹的文档。")
			gone1 := externalFile(t, "临时草稿.md", "# 临时草稿")
			gone2 := externalFile(t, "会议纪要-旧版.md", "# 会议纪要")
			gone3 := externalFile(t, "产品规格.pdf", "%PDF-1.4\n%%EOF\n")

			for _, f := range []string{kept, gone1, gone2, readme} {
				a.openSystemFile(f)
			}
			sp := a.state.Get(space.ExternalID)
			sp.AddExternal(gone3)
			a.openSystemFile(kept)

			for _, f := range []string{gone1, gone2, gone3} {
				if err := os.Remove(f); err != nil {
					t.Fatal(err)
				}
			}
			a.refreshTree()

			tt.SetSize(1240, 800)
			tt.SetDark(tc.dark)
			if tc.dark {
				th := space.ThemeOf(sp)
				th.Appearance = space.AppearanceDark
				sp.SetTheme(th)
			}
			tt.Frame()
			tt.Advance(time.Second)
			tt.Frame()
			tt.Frame()
			savePNG(t, filepath.Join(dir, tc.name+".png"), tt)
		})
	}
}
