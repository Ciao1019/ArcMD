package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// saveSettle lets autosave write the document, and the file follow its title.
func saveSettle(tt *ui.Tester) {
	tt.Frame()
	tt.Advance(autosaveDelay + 50*time.Millisecond)
	tt.Frame()
	tt.Frame()
}

// A new document starts in its empty title, with the starter below, and
// its file is named by the title written, and renamed while it changes.
func TestNewDocumentIsNamedByItsTitle(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Key(ui.Cmd, ui.KeyN)
	tt.Frame()
	if a.doc == nil || a.doc.rel != "未命名.arcmd" {
		t.Fatalf("new document %+v", a.doc)
	}
	if !tt.Focused("文档") || a.doc.ed.Caret != 0 || !a.doc.ed.Blank() {
		t.Fatal("the new page is not blank with the caret in its title")
	}
	if !tt.HasText("快速开始") {
		t.Errorf("starter missing: %q", tt.Texts())
	}
	tt.Type("Trip")
	settle(tt)
	if a.doc.rel != "Trip.arcmd" || !tt.HasText("Trip") {
		t.Fatalf("file %q, want Trip.arcmd", a.doc.rel)
	}
	if _, err := os.Stat(filepath.Join(root(a), "未命名.arcmd")); !os.IsNotExist(err) {
		t.Error("the untitled file is still there")
	}
	tt.Type(" Plan")
	settle(tt)
	if a.doc.rel != "Trip Plan.arcmd" || !tt.Focused("文档") {
		t.Fatalf("file %q after the title grew", a.doc.rel)
	}
	// A name given by hand stays, whatever the title becomes.
	a.renameIn(a.doc.space, a.doc.rel)
	a.newName = "Mine"
	a.commitRename()
	a.doc.ed.SetSelection(9, 9)
	tt.Type(" 2026")
	settle(tt)
	if a.doc.rel != "Mine.arcmd" {
		t.Errorf("file %q, want the name given by hand", a.doc.rel)
	}
}

// A title another file has is numbered, and an existing document is
// never renamed for its title.
func TestTitleNameTakenAndOldDocuments(t *testing.T) {
	a, tt := newTestApp(t)
	// Welcome's title is "Welcome to ArcMD", its file Welcome.
	a.doc.ed.SetSelection(0, 0)
	tt.Type("Hi ")
	settle(tt)
	if a.doc.rel != "Welcome.arcmd" {
		t.Fatalf("an existing document was renamed to %q", a.doc.rel)
	}
	tt.Key(ui.Cmd, ui.KeyN)
	tt.Frame()
	tt.Type("Welcome")
	settle(tt)
	if a.doc.rel != "Welcome 2.arcmd" {
		t.Errorf("file %q, want Welcome 2.arcmd", a.doc.rel)
	}
	if !isUntitled("未命名") || !isUntitled("未命名 12") || isUntitled("未命名 a") || isUntitled("未命名的") || isUntitled("未命名 ") {
		t.Error("isUntitled")
	}
}

// The starter's buttons begin the body with a block, as one step of the
// history, and the starter goes once something is written.
func TestStarterBeginsTheBody(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Key(ui.Cmd, ui.KeyN)
	tt.Frame()
	tt.Type("Plan")
	tt.Frame()
	if err := tt.Click("插入任务"); err != nil {
		t.Fatalf("no task button: %v %q", err, tt.Texts())
	}
	tt.Frame()
	blocks := a.doc.ed.Doc.Blocks
	if len(blocks) != 2 || blocks[1].Kind != "task" || a.doc.ed.Engine.LeafAt(a.doc.ed.Caret).Block.ID != blocks[1].ID {
		t.Fatalf("blocks %v", kindsOf(a))
	}
	if tt.HasText("快速开始") {
		t.Error("starter still shown under a task")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	if got := kindsOf(a); len(got) != 2 || got[1] != "paragraph" || !tt.HasText("快速开始") {
		t.Errorf("undo left %v", got)
	}
	// With only a title, the button makes the body.
	a.doc.ed.Engine.DeleteBlock(a.doc.ed.Doc.Blocks[1].ID)
	tt.Frame()
	tt.Click("插入表格")
	tt.Frame()
	if got := kindsOf(a); len(got) != 2 || got[1] != "table" {
		t.Errorf("title only: %v", got)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	if got := kindsOf(a); len(got) != 1 {
		t.Errorf("one undo did not take the table back: %v", got)
	}
	// Writing in the body puts the starter away.
	a.doc.ed.Engine.InsertAt(1)
	end := a.doc.ed.Len()
	a.doc.ed.SetSelection(end, end)
	tt.Key(0, ui.KeyEnter)
	tt.Type("text")
	tt.Frame()
	if tt.HasText("快速开始") {
		t.Error("starter shown over written text")
	}
}

func kindsOf(a *App) []string {
	var out []string
	for _, b := range a.doc.ed.Doc.Blocks {
		out = append(out, b.Kind)
	}
	return out
}

// Typing a title with an input method: the file is not renamed while a
// word is being composed, and when it is renamed the editor stays the same
// element, so the focus and the next composition carry on.
func TestTitleFollowKeepsComposition(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Key(ui.Cmd, ui.KeyN)
	tt.Frame()
	tt.Compose("lv", 2)
	tt.Type("旅行")
	tt.Compose("ji", 2)
	saveSettle(tt)
	if a.doc.rel != "未命名.arcmd" {
		t.Fatalf("renamed to %q mid-composition", a.doc.rel)
	}
	if _, composing := a.doc.ed.MarkedRange(); !composing || !tt.Focused("文档") {
		t.Fatal("the composition ended while autosave ran")
	}
	tt.Type("计划")
	saveSettle(tt)
	if a.doc.rel != "旅行计划.arcmd" {
		t.Fatalf("file %q, want 旅行计划.arcmd", a.doc.rel)
	}
	if !tt.Focused("文档") {
		t.Fatal("the editor lost the focus when the file followed the title")
	}
	tt.Compose("a", 1)
	saveSettle(tt)
	tt.Type("啊")
	tt.Frame()
	if got := docTitle(a.doc.ed.Doc); got != "旅行计划啊" {
		t.Fatalf("title %q", got)
	}
}
