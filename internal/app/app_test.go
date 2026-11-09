package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// newTestApp makes an app whose files live in a temporary folder.
func newTestApp(t testing.TB) (*App, *ui.Tester) {
	t.Helper()
	dir := t.TempDir()
	a, err := New(Config{ConfigDir: filepath.Join(dir, "config"), DocumentsDir: filepath.Join(dir, "docs")})
	if err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.View, 1200, 760)
	tt.SetLocale("zh-Hans")
	tt.SetTitleBar(ui.TitleBar{Height: 46, Left: 78})
	return a, tt
}

func root(a *App) string { return a.state.ActiveSpace().Root }

// Documents open for reading. Editing tests explicitly focus the writing
// surface, preserving the test's chosen caret after the pointer click.
func focusDocument(a *App, tt *ui.Tester) {
	tt.Frame()
	anchor, caret := a.doc.ed.Positions()
	if r, ok := tt.Find("文档"); ok {
		tt.ClickAt(r.X+8, r.Y+8)
	}
	a.doc.ed.SetSelection(anchor, caret)
	tt.Frame()
	tt.Advance(350 * time.Millisecond)
}

func TestFirstRun(t *testing.T) {
	a, tt := newTestApp(t)
	if a.doc == nil || a.doc.rel != "Welcome.arcmd" || a.doc.source {
		t.Fatalf("welcome not open in the live editor: %+v", a.doc)
	}
	for _, s := range []string{"Personal", "Journal", "Ideas", "Welcome.arcmd"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in %q", s, tt.Texts())
		}
	}
}

// Opening a document never writes it; editing does, after a rest.
func TestAutosaveOnlyAfterEdits(t *testing.T) {
	a, tt := newTestApp(t)
	before, _ := vault.Read(root(a), "Welcome.arcmd")
	tt.Advance(2 * autosaveDelay)
	tt.Frame()
	if after, _ := vault.Read(root(a), "Welcome.arcmd"); after != before || a.doc.dirty() {
		t.Fatal("opening the document changed it")
	}
	a.doc.ed.SetSelection(0, 0)
	a.doc.ed.ReplaceText(nil, "Hello ")
	tt.Frame()
	if !a.doc.dirty() || !tt.HasText("已修改") {
		t.Fatal("edit not shown as unsaved")
	}
	tt.Advance(autosaveDelay + 50*time.Millisecond)
	tt.Frame()
	got, _ := vault.Read(root(a), "Welcome.arcmd")
	if a.doc.dirty() || !strings.Contains(got, "Hello Welcome to ArcMD") {
		t.Errorf("after the delay: dirty %v, file starts %q", a.doc.dirty(), got[:min(40, len(got))])
	}
}

func TestSourceModeToggle(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	if !a.doc.source || !strings.HasPrefix(a.doc.preview, "# Welcome to ArcMD") {
		t.Fatal("source mode not shown")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	if a.doc.source || a.doc.dirty() {
		t.Errorf("back to live: source %v, dirty %v", a.doc.source, a.doc.dirty())
	}
}

func TestFoldersRenameAndMove(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	if err := tt.Click("Journal"); err != nil {
		t.Fatal(err)
	}
	if sp.IsExpanded("Journal") || tt.HasText("Ideas") {
		t.Fatal("click did not close the folder")
	}
	tt.Advance(time.Second) // not a double click
	tt.Click("Journal")
	tt.Advance(time.Second)
	if err := tt.Click("Ideas"); err != nil {
		t.Fatal(err)
	}
	if a.doc.rel != "Journal/Ideas.arcmd" {
		t.Fatalf("open = %q", a.doc.rel)
	}
	a.startRename("Journal")
	a.newName = "Notes"
	a.commitRename()
	if a.doc.rel != "Notes/Ideas.arcmd" || !sp.IsExpanded("Notes") {
		t.Errorf("after rename: doc %q, expanded %v", a.doc.rel, sp.Expanded)
	}
	a.moveInto("Welcome.arcmd", "Notes")
	if !a.state.IsFavorite(sp.ID, "Notes/Welcome.arcmd") {
		t.Errorf("pin did not follow the move: %v", a.state.Favorites)
	}
	if _, err := os.Stat(filepath.Join(root(a), "Notes", "Welcome.arcmd")); err != nil {
		t.Error(err)
	}
	tt.Frame()
	if !tt.HasText("Notes") {
		t.Errorf("texts %q", tt.Texts())
	}
}

func TestHistory(t *testing.T) {
	a, tt := newTestApp(t)
	a.open("Journal/Ideas.arcmd")
	tt.Key(ui.Cmd, ui.KeyBracketLeft)
	if a.doc.rel != "Welcome.arcmd" {
		t.Fatalf("back = %q", a.doc.rel)
	}
	tt.Key(ui.Cmd, ui.KeyBracketRight)
	if a.doc.rel != "Journal/Ideas.arcmd" {
		t.Fatalf("forward = %q", a.doc.rel)
	}
}

func TestSidebarFolds(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Key(ui.Cmd, ui.KeyBackslash)
	tt.Advance(time.Second)
	tt.Frame()
	if !a.sidebarHidden || tt.HasText("Journal") {
		t.Fatalf("sidebar still shows: %v", a.sidebarHidden)
	}
	// Pointing at the window's left edge peeks at it.
	tt.Move(3, 400)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if !a.peek || !tt.HasText("Journal") {
		t.Errorf("no peek: %v", a.peek)
	}
	tt.Move(900, 400)
	tt.Advance(peekDelay + time.Millisecond)
	if a.peek {
		t.Error("peek stayed as the pointer left")
	}
}

func TestSpacesSwitchAndRemember(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil // a favorite would stay open across the switch
	first := a.state.ActiveSpace()
	a.newSpaceForm()
	a.spaceForm.name = "Work"
	a.submitSpace()
	work := a.state.ActiveSpace()
	if work.Name != "Work" || work.ID == first.ID {
		t.Fatalf("active = %+v", work)
	}
	tt.Frame()
	if !tt.HasText("此空间暂无文档。") {
		t.Errorf("empty state missing: %q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyN)
	if a.doc == nil || a.doc.space != work {
		t.Fatal("new document not in Work")
	}
	tt.Key(ui.Cmd, ui.Key1)
	if a.state.ActiveSpace() != first || a.doc == nil || a.doc.rel != "Welcome.arcmd" {
		t.Fatalf("Cmd+1 did not go back to Personal's last document: %+v", a.doc)
	}
	tt.Key(ui.Cmd, ui.KeyBracketLeft)
	if a.state.ActiveSpace() != work {
		t.Error("back did not cross into the other space")
	}
}

func TestCommandBar(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Key(ui.Cmd, ui.KeyK)
	if !a.palette.open || !tt.HasText("新建文档") {
		t.Fatalf("command bar not shown: %q", tt.Texts())
	}
	a.palette.query = "ideas"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	if a.palette.open || a.doc.rel != "Journal/Ideas.arcmd" {
		t.Errorf("running the result: open %v, doc %q", a.palette.open, a.doc.rel)
	}
}

func TestResolveLink(t *testing.T) {
	a, _ := newTestApp(t)
	d := openDocument(a.state.ActiveSpace(), "journal/today.md", "", false)
	for url, want := range map[string]string{"ideas.md": "journal/ideas.md", "../Welcome.arcmd#top": "Welcome.arcmd", "/a/b.md": "a/b.md"} {
		if got, ok := d.resolveLink(url); !ok || got != want {
			t.Errorf("resolveLink(%q) = %q, %v; want %q", url, got, ok, want)
		}
	}
	for _, url := range []string{"https://x.dev/a.md", "#h", "image.png", "../../out.md"} {
		if got, ok := d.resolveLink(url); ok {
			t.Errorf("resolveLink(%q) = %q, want none", url, got)
		}
	}
}

func TestBadSpaceColorFallsBack(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.ActiveSpace().Color = "not a color"
	tt.Frame() // must not panic
}
