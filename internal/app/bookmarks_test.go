package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// bookmarkDoc writes a native document of a title and paragraphs, opens
// it and returns its top-level block IDs.
func bookmarkDoc(t *testing.T, a *App, tt *ui.Tester, rel, markdown string) []string {
	t.Helper()
	data, err := editor.ImportMarkdown(markdown).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root(a), rel, data); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open(rel)
	tt.Frame()
	var ids []string
	for _, b := range a.doc.ed.Doc.Blocks {
		ids = append(ids, b.ID)
	}
	return ids
}

// caretOn puts the caret at the start of the top-level block.
func caretOn(a *App, id string) {
	a.doc.ed.Engine.FocusBlock(id)
}

func TestBookmarkToggleAndMark(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	ids := bookmarkDoc(t, a, tt, "Long.arcmd", "# Long\n\nFirst part\n\nSecond part\n\nThird part")
	focusDocument(a, tt)
	caretOn(a, ids[2])
	tt.Key(ui.Cmd, ui.KeyD)
	tt.Frame()
	if a.state.BookmarkIndex(a.doc.space.ID, "Long.arcmd", ids[2]) < 0 {
		t.Fatalf("⌘D left no bookmark: %+v", a.state.Bookmarks)
	}
	if a.doc.dirty() {
		t.Fatal("a bookmark must not edit the document")
	}
	saved, err := space.NewStore(a.store.Path()).Load()
	if err != nil || len(saved.Bookmarks) != 1 || saved.Bookmarks[0].Excerpt != "Second part" {
		t.Fatalf("spaces.json bookmarks = %+v, %v", saved, err)
	}
	// The first bookmark of a page without headings slides the outline in.
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText("书签 · 1") {
		t.Fatal("the outline lists no bookmarks")
	}
	// The mark beside the block takes the bookmark away.
	if err := tt.Click("移除书签"); err != nil {
		t.Fatal(err)
	}
	if len(a.state.Bookmarks) != 0 {
		t.Fatalf("the mark left the bookmark: %+v", a.state.Bookmarks)
	}
	// ⌘D twice is no bookmark.
	caretOn(a, ids[1])
	tt.Key(ui.Cmd, ui.KeyD)
	tt.Key(ui.Cmd, ui.KeyD)
	if len(a.state.Bookmarks) != 0 {
		t.Fatalf("⌘D twice = %+v", a.state.Bookmarks)
	}
}

func TestBookmarkStepping(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	ids := bookmarkDoc(t, a, tt, "Steps.arcmd", "# Steps\n\nOne\n\nTwo\n\nThree\n\nFour")
	focusDocument(a, tt)
	for _, i := range []int{1, 3} {
		caretOn(a, ids[i])
		tt.Key(ui.Cmd, ui.KeyD)
	}
	caretOn(a, ids[2])
	step := func(key ui.Key, want int) {
		t.Helper()
		tt.Key(ui.Ctrl|ui.Cmd, key)
		tt.Frame()
		if got := a.doc.ed.CaretBlock(); got != want {
			t.Fatalf("caret in block %d, want %d", got, want)
		}
	}
	step(ui.KeyDown, 3)
	step(ui.KeyDown, 1) // round from the end
	step(ui.KeyUp, 3)   // round from the start
	step(ui.KeyUp, 1)
	// The jump scrolls and lights the block up, then the light fades.
	if a.doc.ed.Engine.LeafAt(a.doc.ed.Caret).Owner != ids[1] {
		t.Fatal("caret not on the bookmarked block")
	}
	tt.Advance(2 * time.Second)
}

func TestBookmarkListOpensOtherDocument(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	ids := bookmarkDoc(t, a, tt, "Far.arcmd", "# Far\n\nIntro\n\nThe place to come back to")
	focusDocument(a, tt)
	caretOn(a, ids[2])
	tt.Key(ui.Cmd, ui.KeyD)
	bookmarkDoc(t, a, tt, "Near.arcmd", "# Near\n\nElsewhere")
	tt.Key(ui.Cmd|ui.Shift, ui.KeyD)
	tt.Frame()
	if !a.palette.open || !a.palette.bookmarks || !tt.HasText("The place to come back to") {
		t.Fatal("the bookmark list does not show the bookmark")
	}
	tt.Type("come back")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.doc.rel != "Far.arcmd" || a.doc.ed.Engine.LeafAt(a.doc.ed.Caret).Owner != ids[2] {
		t.Fatalf("opened %s at %d", a.doc.rel, a.doc.ed.CaretBlock())
	}
	// Back goes to where the jump left.
	a.goHistory(-1)
	if a.doc.rel != "Near.arcmd" {
		t.Fatalf("back went to %s", a.doc.rel)
	}
}

func TestBookmarkLostAndUndo(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	ids := bookmarkDoc(t, a, tt, "Lost.arcmd", "# Lost\n\nKeep\n\nGoes away")
	focusDocument(a, tt)
	caretOn(a, ids[2])
	tt.Key(ui.Cmd, ui.KeyD)
	a.doc.ed.Engine.DeleteBlock(ids[2])
	tt.Frame()
	if len(a.marksOf(a.doc)) != 0 || len(a.state.Bookmarks) != 1 {
		t.Fatal("a deleted block's bookmark must wait, not resolve or go")
	}
	a.openBookmarks()
	tt.Frame()
	if !tt.HasText("位置已失效") {
		t.Fatal("the lost bookmark is not marked")
	}
	a.palette.open = false
	a.doc.ed.Undo()
	tt.Frame()
	if _, ok := a.marksOf(a.doc)[ids[2]]; !ok {
		t.Fatal("undo did not bring the bookmark back")
	}
	// A copy of the block, with the same text, is not the bookmarked one.
	a.doc.ed.Engine.Duplicate(ids[2])
	a.doc.ed.Engine.DeleteBlock(ids[2])
	tt.Frame()
	if len(a.marksOf(a.doc)) != 0 {
		t.Fatal("the bookmark moved to a copy of its block")
	}
}

func TestBookmarkMarkdownFindsBlockByExcerpt(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	if err := vault.Write(root(a), "Plain.md", "# Plain\n\nAlpha\n\nBeta"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Plain.md")
	tt.Frame()
	focusDocument(a, tt)
	caretOn(a, a.doc.ed.Doc.Blocks[2].ID)
	tt.Key(ui.Cmd, ui.KeyD)
	if a.doc.dirty() {
		t.Fatal("a bookmark converted the Markdown file")
	}
	if _, err := os.Stat(filepath.Join(root(a), "Plain.arcmd")); err == nil {
		t.Fatal("a bookmark wrote a native copy")
	}
	// Read again, the file's blocks have new IDs; the excerpt finds Beta.
	old := a.state.Bookmarks[0].Block
	a.open("Welcome.arcmd")
	tt.Frame()
	if a.residentDoc(a.doc.space.ID, "Plain.md") != nil {
		t.Fatal("the Markdown document stayed in memory")
	}
	a.open("Plain.md")
	tt.Frame()
	beta := a.doc.ed.Doc.Blocks[2]
	if beta.ID == old {
		t.Fatal("the Markdown file kept its block IDs; the test proves nothing")
	}
	if _, ok := a.marksOf(a.doc)[beta.ID]; !ok {
		t.Fatalf("the bookmark did not find Beta again: %+v", a.state.Bookmarks)
	}
}

func TestBookmarkRenameInOutline(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	ids := bookmarkDoc(t, a, tt, "Named.arcmd", "# Named\n\n## Part\n\nThe figure below")
	focusDocument(a, tt)
	caretOn(a, ids[2])
	tt.Key(ui.Cmd, ui.KeyD)
	tt.Frame()
	if err := tt.RightClick("跳到书签 The figure below"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("重命名…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("Figure 1")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	b := a.state.Bookmarks[0]
	if b.Label != "Figure 1" || b.Title() != "Figure 1" || !tt.HasText("Figure 1") {
		t.Fatalf("bookmark after the rename = %+v", b)
	}
	// Clicking the entry goes to the block.
	caretOn(a, ids[1])
	if err := tt.Click("跳到书签 Figure 1"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.doc.ed.CaretBlock() != 2 {
		t.Fatalf("the outline entry went to block %d", a.doc.ed.CaretBlock())
	}
}

const bookmarkIntegral = `\int_0^\infty x^2 e^{-x} dx = 2`

func TestFormulaBookmarkInOutline(t *testing.T) {
	for _, mode := range []string{"light", "dark", "narrow"} {
		t.Run(mode, func(t *testing.T) {
			a, tt := newTestApp(t)
			if mode == "dark" {
				tt.SetDark(true)
			}
			tt.Frame()
			ids := bookmarkDoc(t, a, tt, "Math.arcmd", "# Math\n\nBefore\n\n$$\n"+bookmarkIntegral+"\n$$\n\nAfter")
			focusDocument(a, tt)
			caretOn(a, ids[2])
			tt.Key(ui.Cmd, ui.KeyD)
			caretOn(a, ids[0])
			tt.Frame()
			tt.Advance(300 * time.Millisecond)
			if mode == "narrow" {
				tt.SetSize(540, 760)
				tt.Frame()
				if err := tt.Click("展开目录"); err != nil {
					t.Fatal(err)
				}
				tt.Advance(300 * time.Millisecond)
			}
			assertFormula := func() {
				t.Helper()
				tt.Frame()
				if _, raw := tt.Find(bookmarkIntegral); raw {
					t.Fatal("formula bookmark shows raw TeX instead of the rendered formula")
				}
				row, ok := tt.Find("跳到书签 " + bookmarkIntegral)
				if !ok || row.H <= 28 {
					t.Fatalf("formula bookmark has no room for integral limits: %+v", row)
				}
			}
			assertFormula()
			if a.doc.dirty() || a.state.Bookmarks[0].Excerpt != bookmarkIntegral {
				t.Fatal("rendering a bookmark changed the document or its recovery excerpt")
			}
			if err := tt.Click("跳到书签 " + bookmarkIntegral); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if a.doc.ed.CaretBlock() != 2 {
				t.Fatal("formula bookmark did not reveal its block")
			}
			for _, label := range []string{"Integral", ""} {
				title := a.state.Bookmarks[0].Title()
				if err := tt.RightClick("跳到书签 " + title); err != nil {
					t.Fatal(err)
				}
				if err := tt.ChooseMenuItem("重命名…"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				tt.Key(ui.Cmd, ui.KeyA)
				tt.Key(0, ui.KeyBackspace)
				tt.Type(label)
				tt.Key(0, ui.KeyEnter)
				tt.Frame()
				if a.state.Bookmarks[0].Label != label {
					t.Fatal("formula bookmark did not keep its custom name")
				}
				if label != "" && !tt.HasText(label) {
					t.Fatal("a custom name should replace the formula preview")
				}
			}
			assertFormula()
		})
	}
}

func TestFormulaBookmarkUpdatesAndFallbacks(t *testing.T) {
	for _, source := range []string{bookmarkIntegral, strings.Repeat("x+", 60) + `\frac{a}{b}`, `\frac{a}`, ""} {
		t.Run(source, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.Frame()
			ids := bookmarkDoc(t, a, tt, "Math.arcmd", "# Math\n\n$$\n"+source+"\n$$")
			focusDocument(a, tt)
			caretOn(a, ids[1])
			tt.Key(ui.Cmd, ui.KeyD)
			tt.Frame()
			tt.Advance(300 * time.Millisecond)
			excerpt := editor.BlockExcerpt(a.doc.ed.Doc.Blocks[1])
			_, raw := tt.Find(excerpt)
			if wantRaw := source == "" || source == `\frac{a}`; raw != wantRaw {
				t.Fatalf("raw excerpt visible = %v, want %v", raw, wantRaw)
			}
			if source != `\frac{a}` {
				return
			}
			// Correcting the source updates the preview without toggling the
			// bookmark; undo returns to the editable text fallback.
			a.doc.ed.SetSelection(a.doc.ed.Len(), a.doc.ed.Len())
			tt.Frame()
			tt.Type("{b}")
			tt.Frame()
			if _, raw := tt.Find(`\frac{a}{b}`); raw || !tt.HasText(`跳到书签 \frac{a}{b}`) {
				t.Fatal("correcting TeX did not update the bookmark preview")
			}
			tt.Key(ui.Cmd, ui.KeyZ)
			tt.Frame()
			if _, raw := tt.Find(source); !raw {
				t.Fatal("undo did not restore the bookmark fallback")
			}
		})
	}
}

func TestBookmarksFollowRename(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	ids := bookmarkDoc(t, a, tt, "Before.arcmd", "# Before\n\nText")
	focusDocument(a, tt)
	caretOn(a, ids[1])
	tt.Key(ui.Cmd, ui.KeyD)
	a.renameIn(a.doc.space, "Before.arcmd")
	a.newName = "After"
	a.commitRename()
	tt.Frame()
	if a.state.BookmarkIndex(a.doc.space.ID, "After.arcmd", ids[1]) < 0 {
		t.Fatalf("bookmarks after the rename = %+v", a.state.Bookmarks)
	}
}

func TestBookmarkHoverToAdd(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	ids := bookmarkDoc(t, a, tt, "Hover.arcmd", "# Hover\n\nFirst part\n\nSecond part")
	if tt.HasText("添加书签") {
		t.Fatal("the hollow mark shows with no pointer on the page")
	}
	r, ok := tt.Find("文档")
	if !ok {
		t.Fatal("no page")
	}
	// Over the second paragraph: its hollow mark shows, and stays while
	// the pointer goes over to it in the margin.
	var line ui.Rect
	for _, l := range a.doc.ed.Doc.Leaves() {
		if l.Block.ID == ids[2] {
			b, _, _ := a.doc.ed.BoundsForRange(ui.TextInputRange{Start: l.Start, End: l.Start})
			line = b
		}
	}
	tt.Move(r.X+line.X+20, r.Y+line.Y+line.H/2-a.doc.ed.Scroll.Y)
	tt.Frame()
	mark, ok := tt.Find("添加书签")
	if !ok {
		t.Fatal("hovering a block shows no hollow mark")
	}
	tt.Move(mark.X+mark.W/2, mark.Y+mark.H/2)
	tt.Frame()
	tt.ClickAt(mark.X+mark.W/2, mark.Y+mark.H/2)
	tt.Frame()
	if a.state.BookmarkIndex(a.doc.space.ID, "Hover.arcmd", ids[2]) < 0 {
		t.Fatalf("the hollow mark added no bookmark: %+v", a.state.Bookmarks)
	}
	if a.doc.dirty() {
		t.Fatal("adding a bookmark edited the document")
	}
	if _, ok := tt.Find("移除书签"); !ok {
		t.Fatal("the mark did not turn solid")
	}
	// Read-only pages take bookmarks too.
	a.doc.ed.SetReadOnly(true)
	tt.Frame()
	solid, ok := tt.Find("移除书签")
	if !ok {
		t.Fatal("a read-only page shows no mark")
	}
	tt.ClickAt(solid.X+solid.W/2, solid.Y+solid.H/2)
	tt.Frame()
	if len(a.state.Bookmarks) != 0 {
		t.Fatalf("a read-only page kept the bookmark: %+v", a.state.Bookmarks)
	}
}
