package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo"
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
