package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestSidebarWidthAndPinning(t *testing.T) {
	a, tt := newTestApp(t)
	back, ok := tt.Find("后退")
	if !ok || back.X != 230 || a.sidebarHidden {
		t.Fatalf("default sidebar: back %+v, hidden %v", back, a.sidebarHidden)
	}
	tt.Click("收起侧边栏")
	tt.Advance(time.Second)
	folded, _ := tt.Find("文档路径")
	tt.Move(3, 400)
	if !a.peek {
		t.Fatal("edge hover did not start opening without another input")
	}
	// Cross into the panel before its opening animation has finished.
	tt.Move(180, 400)
	tt.Advance(time.Second)
	if !a.peek || !tt.HasText("Journal") {
		t.Fatal("moving ahead of the animation closed the panel")
	}
	floating, _ := tt.Find("文档路径")
	if floating != folded {
		t.Fatalf("peek moved the document: %+v -> %+v", folded, floating)
	}
	tt.Move(500, 400)
	tt.Advance(peekDelay / 2)
	tt.Move(180, 400)
	tt.Advance(peekDelay)
	if !a.peek {
		t.Fatal("briefly leaving the floating panel made it disappear")
	}
	if err := tt.Click("固定侧边栏"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	tt.Move(900, 400)
	tt.Advance(time.Second)
	if a.sidebarHidden || a.peek || !tt.HasText("Journal") {
		t.Fatal("pinned sidebar did not remain open")
	}
}

func TestSidebarPeekRenameAndModal(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Key(ui.Cmd, ui.KeyBackslash)
	tt.Move(3, 400)
	if !a.peek || !tt.HasText("Journal") {
		t.Fatal("reduced-motion peek did not open immediately")
	}
	if err := tt.RightClick("Ideas"); err != nil {
		t.Fatal(err)
	}
	tt.Move(450, 400)
	tt.Advance(time.Second)
	if err := tt.ChooseMenuItem("重命名…"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	if !a.peek || !tt.Focused("名称") {
		t.Fatal("rename lost its floating sidebar or focus")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Advance(time.Second)
	if a.peek || a.renaming != "" {
		t.Fatal("canceling rename did not release the sidebar")
	}
	tt.Key(ui.Cmd, ui.KeyP)
	tt.Move(3, 400)
	if a.peek || !a.palette.open {
		t.Fatal("edge hover opened a sidebar behind the command bar")
	}
}

func TestSpaceSwitchWhilePeeking(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil // a favorite would stay open across the switch
	first := a.state.ActiveSpace()
	work := a.state.Add("Work", t.TempDir())
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyBackslash)
	tt.Advance(time.Second)
	tt.Move(3, 400)
	tt.Advance(time.Second)
	if err := tt.Click("切换到 Work"); err != nil {
		t.Fatal(err)
	}
	if a.state.ActiveSpace() != work || a.doc != nil || !a.sidebarHidden || !a.peek {
		t.Fatal("space switch changed the peek mode or showed another space's document")
	}
	if err := tt.Click("切换到 Personal"); err != nil {
		t.Fatal(err)
	}
	if a.state.ActiveSpace() != first || a.doc.rel != "Welcome.arcmd" || !a.peek {
		t.Fatal("switching back did not restore Personal in the floating sidebar")
	}
	tt.Move(900, 400)
	tt.Advance(time.Second)
	if a.peek || !a.sidebarHidden || tt.HasText("Journal") {
		t.Fatal("space switch pinned the floating sidebar")
	}
}

func TestSpaceBarOverflowAndKeyboard(t *testing.T) {
	a, tt := newTestApp(t)
	for i := 1; i < 12; i++ {
		a.state.Add(fmt.Sprintf("Space %d", i), t.TempDir())
	}
	tt.SetSize(640, 420)
	for _, label := range []string{"新建空间", "资料库"} {
		r, ok := tt.Find(label)
		if !ok || r.X < 0 || r.X+r.W > sidebarWidth {
			t.Fatalf("%s overflowed the sidebar: %+v", label, r)
		}
	}
	if err := tt.Click("切换到 Personal"); err != nil {
		t.Fatal(err)
	}
	// The active icon opens its picker. Dismissing it must return the
	// keyboard to the space bar, including its overflow navigation.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Key(0, ui.KeyEnd)
	tt.Key(0, ui.KeyEnter)
	if a.state.ActiveSpace().Name != "Space 11" || a.spaceScroll.X == 0 {
		t.Fatalf("keyboard could not reach the last space: %s, scroll %v", a.state.ActiveSpace().Name, a.spaceScroll.X)
	}
	tt.Key(ui.Cmd, ui.Key1)
	if a.state.ActiveSpace().Name != "Personal" || a.spaceScroll.X != 0 {
		t.Fatal("switching with a shortcut did not reveal the active space")
	}
}

func TestSpaceRestoresEditorAndTreeScroll(t *testing.T) {
	a, tt := newTestApp(t)
	first := a.state.ActiveSpace()
	work := a.state.Add("Work", t.TempDir())
	for i := 0; i < 35; i++ {
		if err := vault.Write(first.Root, fmt.Sprintf("Note %02d.md", i), "# Note\n"); err != nil {
			t.Fatal(err)
		}
	}
	a.refreshTree()
	tt.Frame()
	tt.Scroll(120, 400, 0, 350)
	tt.Advance(time.Second)
	scrolled := a.session(first.ID).tree.Y
	if scrolled == 0 {
		t.Fatal("test did not scroll the file tree")
	}
	original := a.doc
	original.ed.ReplaceText(nil, "Saved ")
	original.ed.SetSelection(2, 5)
	tt.Key(ui.Cmd, ui.Key2)
	if a.state.ActiveSpace() != work || original.dirty() {
		t.Fatal("leaving the space did not save the edited document")
	}
	tt.Key(ui.Cmd, ui.Key1)
	anchor, caret := a.doc.ed.Positions()
	if a.doc != original || !a.doc.ed.CanUndo() || anchor != 2 || caret != 5 {
		t.Fatal("space switch discarded the editor's undo or selection")
	}
	if a.session(first.ID).tree.Y != scrolled {
		t.Fatal("space switch discarded the file tree's scroll position")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	source := a.doc.ed
	tt.Key(ui.Cmd, ui.Key2)
	tt.Key(ui.Cmd, ui.Key1)
	if !a.doc.source || a.doc.ed != source {
		t.Fatal("space switch discarded source mode")
	}
	tt.Key(ui.Cmd, ui.KeyBackslash)
	tt.Advance(time.Second)
	tt.Move(3, 400)
	tt.Advance(time.Second)
	if a.session(first.ID).tree.Y != scrolled {
		t.Fatal("floating sidebar discarded the file tree's scroll position")
	}
}

func TestSpaceRestoreUsesChangedFile(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil // a favorite would stay open across the switch
	first := a.state.ActiveSpace()
	a.state.Add("Work", t.TempDir())
	original := a.doc
	tt.Key(ui.Cmd, ui.Key2)
	changed, _ := editor.ImportMarkdown("# Changed externally\n").Encode()
	if err := vault.Write(first.Root, "Welcome.arcmd", changed); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.Key1)
	if a.doc == original || a.doc.diskText != changed || a.doc.dirty() {
		t.Fatal("restored a stale editor over an externally changed file")
	}
	tt.Key(ui.Cmd, ui.Key2)
	if err := os.Remove(filepath.Join(first.Root, "Welcome.arcmd")); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.Key1)
	if a.doc != nil || first.LastOpen != "" {
		t.Fatal("restored an externally deleted document")
	}
}

func TestSpaceKeepsCollapsedFolders(t *testing.T) {
	a, tt := newTestApp(t)
	first := a.state.ActiveSpace()
	a.state.Add("Work", t.TempDir())
	if err := tt.Click("Ideas"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Journal"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.Key2)
	tt.Key(ui.Cmd, ui.Key1)
	if a.doc.rel != "Journal/Ideas.arcmd" || first.IsExpanded("Journal") {
		t.Fatal("restoring the last document changed the space's collapsed folders")
	}
}

func TestSpaceSwitchStopsOnSaveFailure(t *testing.T) {
	a, tt := newTestApp(t)
	first := a.state.ActiveSpace()
	a.state.Add("Work", t.TempDir())
	original, originalRoot, at := a.doc, first.Root, a.at
	original.ed.ReplaceText(nil, "Unsaved ")
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("blocks directory creation"), 0o600); err != nil {
		t.Fatal(err)
	}
	first.Root = filepath.Join(blocker, "space")
	tt.Key(ui.Cmd, ui.Key2)
	if a.state.ActiveSpace() != first || a.doc != original || !a.doc.dirty() || a.at != at || a.doc.saveErr == nil {
		t.Fatal("failed save discarded the document or changed the space")
	}
	first.Root = originalRoot
	tt.Key(ui.Cmd, ui.Key2)
	tt.Key(ui.Cmd, ui.Key1)
	if a.doc != original || a.doc.dirty() || a.doc.saveErr != nil {
		t.Fatal("retry did not save and restore the original editor")
	}
}
