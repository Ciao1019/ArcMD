package app

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/history"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// caretAtEnd puts the keyboard in the open document, the caret at its end.
func caretAtEnd(a *App, tt *ui.Tester) {
	focusDocument(a, tt)
	end := a.doc.ed.Len()
	a.doc.ed.SetSelection(end, end)
	tt.Frame()
}

// Typing [[ offers the documents, Enter completes the link, ⌘-click
// follows it, and the document linked to lists the link below it.
func TestWikiLinks(t *testing.T) {
	a, tt := newTestApp(t)
	caretAtEnd(a, tt)
	tt.Key(0, ui.KeyEnter)
	tt.Type("see [[Ide")
	tt.Frame()
	if !tt.HasText("链接到文档") || !tt.HasText("Ideas") {
		t.Fatalf("no completion: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if text := a.doc.ed.Text(); !strings.HasSuffix(text, "see [[Ideas]]") {
		t.Fatalf("completed to %q", text[max(0, len(text)-30):])
	}
	if tt.HasText("链接到文档") {
		t.Error("completion still open")
	}
	saveSettle(tt)
	a.refreshSearch()
	// ⌘-click the link.
	ed := a.doc.ed
	leaves := ed.Doc.Leaves()
	last := leaves[len(leaves)-1]
	r, _, ok := ed.BoundsForRange(ui.TextInputRange{Start: last.End - 4, End: last.End - 4})
	surface, _ := tt.Find("文档")
	if !ok {
		t.Fatal("no bounds")
	}
	tt.ClickAtWith(ui.Cmd, surface.X+r.X, surface.Y+r.Y+r.H/2)
	tt.Frame()
	if a.doc.rel != "Journal/Ideas.arcmd" {
		t.Fatalf("⌘-click opened %q", a.doc.rel)
	}
	tt.Frame()
	if !tt.HasText("反向链接 · 1") || !tt.HasText("Welcome") {
		t.Errorf("backlinks missing: %q", tt.Texts())
	}
	if err := tt.Click("打开 Welcome"); err != nil {
		t.Fatal(err)
	}
	if a.doc.rel != "Welcome.arcmd" {
		t.Errorf("backlink opened %q", a.doc.rel)
	}
	// A link to a document that is not there makes it.
	a.openWiki("Brand New")
	if a.doc == nil || a.doc.rel != "Brand New.arcmd" || a.doc.ed.Doc.Blocks[0].Text() != "Brand New" {
		t.Fatalf("missing link made %+v", a.doc)
	}
}

// Renaming a document renames the links to it, in the open document and
// in files.
func TestRenameFollowsLinks(t *testing.T) {
	a, tt := newTestApp(t)
	writeElsewhere(t, a, "Plan.arcmd", "# Plan\n\nRead [[Ideas]] and [[ideas|my notes]].")
	a.refreshTree()
	caretAtEnd(a, tt)
	tt.Type(" [[Ideas]]")
	saveSettle(tt)
	a.refreshSearch()
	a.renameIn(a.state.ActiveSpace(), "Journal/Ideas.arcmd")
	a.newName = "Thoughts"
	a.commitRename()
	if !strings.Contains(a.doc.ed.Text(), "[[Thoughts]]") {
		t.Errorf("open document: %q", a.doc.ed.Text())
	}
	text, _ := vault.Read(root(a), "Plan.arcmd")
	doc, err := model.Decode(text)
	if err != nil || doc.PlainText() != "Plan\nRead [[Thoughts]] and [[Thoughts|my notes]]." {
		t.Fatalf("file: %q %v", doc.PlainText(), err)
	}
	tt.Frame()
	if !tt.HasText("指向「Thoughts」的链接") {
		t.Errorf("no notice: %q", tt.Texts())
	}
}

// #tags show the documents holding them in the command bar.
func TestTags(t *testing.T) {
	a, tt := newTestApp(t)
	writeElsewhere(t, a, "Plan.arcmd", "# Plan\n\nA #trip to #work/rome.")
	writeElsewhere(t, a, "Other.arcmd", "# Other\n\nmore #trip ideas")
	a.refreshTree()
	typeQuery(a, tt, "#trip")
	res := a.results()
	var titles []string
	for _, r := range res {
		titles = append(titles, r.kind+":"+r.title)
	}
	if len(res) < 2 || !slices.Contains(titles, "标签:Plan") || !slices.Contains(titles, "标签:Other") {
		t.Fatalf("tag results %q", titles)
	}
	typeQuery(a, tt, "#")
	if res := a.results(); len(res) == 0 || res[0].title != "#trip" || res[0].hint != "2 篇文档" {
		t.Fatalf("all tags %v", kinds(res))
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !a.palette.open || a.palette.query != "#trip" {
		t.Errorf("choosing a tag: open %v, query %q", a.palette.open, a.palette.query)
	}
}

// Focus mode leaves the document alone in the window; a narrow window
// folds the sidebar by itself.
func TestFocusModeAndNarrowWindow(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	if !tt.HasText("收藏") {
		t.Fatal("sidebar missing")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyF)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if !a.focus || tt.HasText("收藏") || tt.HasText("文档路径") {
		t.Fatalf("focus mode shows the window: %q", tt.Texts())
	}
	if !tt.HasText("退出专注模式") {
		t.Error("no way out of focus mode")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyF)
	tt.Frame()
	if a.focus || !tt.HasText("收藏") {
		t.Fatal("focus mode did not end")
	}
	tt.SetSize(800, 700)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if !a.folded() || !tt.HasText("展开侧边栏") {
		t.Fatalf("narrow window kept the sidebar")
	}
	tt.Key(ui.Cmd, ui.KeyBackslash)
	tt.Frame()
	if a.folded() {
		t.Fatal("⌘\\ did not show the sidebar in a narrow window")
	}
	tt.SetSize(1200, 760)
	tt.Frame()
	tt.SetSize(800, 700)
	tt.Frame()
	if !a.folded() {
		t.Error("narrowing again kept the sidebar shown")
	}
	tt.SetSize(1200, 760)
	tt.Frame()
	if a.folded() {
		t.Error("widening did not bring the sidebar back")
	}
}

// Saving keeps versions; the history window restores one as one step of
// the editor's history.
func TestVersionHistory(t *testing.T) {
	a, tt := newTestApp(t)
	caretAtEnd(a, tt)
	original := a.doc.ed.Committed().PlainText()
	tt.Type(" First edit.")
	saveSettle(tt)
	tt.Type(" Second.")
	saveSettle(tt)
	vs, err := history.List(root(a), "Welcome.arcmd")
	if err != nil || len(vs) != 2 || !vs[1].Kept {
		t.Fatalf("versions %+v %v", vs, err)
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyY)
	tt.Frame()
	if !a.versions.open || !tt.HasText("版本历史 · Welcome") || !tt.HasText("打开时的版本") {
		t.Fatalf("history window: %q", tt.Texts())
	}
	a.selectVersion(1)
	tt.Frame()
	if !tt.HasText("恢复后增加") {
		t.Errorf("no difference shown: %q", tt.Texts())
	}
	if err := tt.Click("恢复此版本"); err != nil {
		t.Fatal(err)
	}
	if got := a.doc.ed.Committed().PlainText(); got != original {
		t.Fatalf("restored %q", got)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if got := a.doc.ed.Committed().PlainText(); !strings.HasSuffix(got, "Second.") {
		t.Errorf("undo of the restore: %q", got)
	}
	// The history follows the document's new name.
	a.renameIn(a.state.ActiveSpace(), "Welcome.arcmd")
	a.newName = "Hello"
	a.commitRename()
	if vs, _ := history.List(root(a), "Hello.arcmd"); len(vs) < 2 {
		t.Errorf("history did not follow the rename: %d", len(vs))
	}
}

// What is deleted can be put back from the library's recently deleted page.
func TestRecentlyDeleted(t *testing.T) {
	a, tt := newTestApp(t)
	var trashed []string
	a.trashFile = func(p string) error { trashed = append(trashed, p); return os.RemoveAll(p) }
	a.trash("Journal/Ideas.arcmd")
	if len(trashed) != 1 {
		t.Fatal("not trashed")
	}
	if _, err := os.Stat(filepath.Join(root(a), "Journal/Ideas.arcmd")); !os.IsNotExist(err) {
		t.Fatal("still there")
	}
	a.openTrash()
	settle(tt)
	if !tt.HasText("最近删除") || !tt.HasText("Ideas") || !tt.HasText("Personal / Journal · 刚刚删除") {
		t.Fatalf("window: %q", tt.Texts())
	}
	if err := tt.Click("恢复 Ideas.arcmd"); err != nil {
		t.Fatal(err)
	}
	if a.doc == nil || a.doc.rel != "Journal/Ideas.arcmd" || a.library.open {
		t.Fatalf("restored doc %+v", a.doc)
	}
	if items, _ := a.bin.List(time.Now()); len(items) != 0 {
		t.Error("restored item still listed")
	}
	// A failed trash keeps no copy.
	a.trashFile = func(string) error { return os.ErrPermission }
	a.trash("Welcome.arcmd")
	if items, _ := a.bin.List(time.Now()); len(items) != 0 {
		t.Error("copy kept of what stayed")
	}
}

// The document exports as a PDF beside it, and prints through one.
func TestExportPDFAndPrint(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyE)
	tt.Frame()
	data, err := os.ReadFile(filepath.Join(root(a), "Welcome.pdf"))
	if err != nil || !bytes.HasPrefix(data, []byte("%PDF-1.4")) || !bytes.Contains(data, []byte("/Count 1")) {
		t.Fatalf("pdf %d bytes, %v", len(data), err)
	}
	if !tt.HasText("已导出 Welcome.pdf（1 页）") {
		t.Errorf("notice: %q", tt.Texts())
	}
	a.exportPDF()
	if _, err := os.Stat(filepath.Join(root(a), "Welcome 2.pdf")); err != nil {
		t.Error("second export replaced the first")
	}
	var printed string
	a.printFile = func(p string) { printed = p }
	a.printDoc()
	if data, _ := os.ReadFile(printed); !bytes.HasPrefix(data, []byte("%PDF")) {
		t.Errorf("printed %q", printed)
	}
}

// A long document breaks into pages at its lines.
func TestPaginate(t *testing.T) {
	var md strings.Builder
	md.WriteString("# Long\n\n")
	for i := range 80 {
		md.WriteString("Paragraph number " + strings.Repeat("words ", 20) + string(rune('a'+i%26)) + "\n\n")
	}
	ed := editor.New(md.String())
	o := editor.Options{Width: paperWidth, Theme: documentTheme(false), Page: pageStyle(false)}
	var tops []float32
	ui.Render(func(c *ui.Context) { tops = ed.Paginate(o, 1000) }, 100, 100, 1)
	if len(tops) < 3 {
		t.Fatalf("pages %v", tops)
	}
	for i := 1; i < len(tops); i++ {
		if d := tops[i] - tops[i-1]; d > 1000 || d < 300 {
			t.Errorf("page %d is %v tall", i, d)
		}
	}
}

// Shortcuts can be rebound, the window's and the editor's, and keep to
// one command each.
func TestRebindShortcuts(t *testing.T) {
	a, tt := newTestApp(t)
	j := editor.Combo{Mods: ui.Cmd, Key: ui.KeyJ}
	if !a.rebind("palette", &j) {
		t.Fatal("rebind refused")
	}
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	if a.palette.open {
		t.Fatal("old key still opens the bar")
	}
	tt.Key(ui.Cmd, ui.KeyJ)
	tt.Frame()
	if !a.palette.open {
		t.Fatal("new key does not open the bar")
	}
	tt.Key(0, ui.KeyEscape)
	n := editor.Combo{Mods: ui.Cmd, Key: ui.KeyN}
	if a.rebind("palette", &n) {
		t.Error("took new document's key")
	}
	z := editor.Combo{Mods: ui.Cmd, Key: ui.KeyZ}
	if a.rebind("bold", &z) {
		t.Error("took undo's key")
	}
	// Saved, and read again.
	b, err := New(a.cfg)
	if err != nil || b.prefs.Keys["palette"] != "Cmd+J" {
		t.Fatalf("prefs %+v %v", b.prefs, err)
	}
	// The editor's bold on ⇧⌘B.
	sb := editor.Combo{Mods: ui.Cmd | ui.Shift, Key: ui.KeyB}
	if !a.rebind("bold", &sb) {
		t.Fatal("bold rebind refused")
	}
	caretAtEnd(a, tt)
	tt.Type("x")
	a.doc.ed.SetSelection(a.doc.ed.Len()-1, a.doc.ed.Len())
	tt.Key(ui.Cmd, ui.KeyB)
	if a.doc.ed.Engine.CurrentStyle().Bold {
		t.Error("⌘B still bolds")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyB)
	if !a.doc.ed.Engine.CurrentStyle().Bold {
		t.Error("⇧⌘B does not bold")
	}
	// Recorded in the settings.
	a.openSettings(tabShortcuts)
	tt.Frame()
	if err := tt.Click("更改快捷键：新建文档"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	tt.Key(ui.Cmd|ui.Alt, ui.KeyD)
	tt.Frame()
	if got := a.keyLabel("new-doc"); got != (editor.Combo{Mods: ui.Cmd | ui.Alt, Key: ui.KeyD}).Label() {
		t.Errorf("recorded %q", got)
	}
	a.resetKeys("")
	if a.keyLabel("palette") != "⌘K / ⌘L / ⌘P" {
		t.Errorf("reset: %q", a.keyLabel("palette"))
	}
}

// The page's icon shows as the document's icon, its cover above it.
func TestPageIconAndCover(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	a.doc.ed.EditPageIcon()
	tt.Frame()
	if err := tt.Click("图标：🚀"); err != nil {
		t.Fatal(err)
	}
	if a.doc.ed.Doc.Icon != "🚀" || a.iconTitle(a.state.ActiveSpace().ID, "Welcome.arcmd") != "🚀 Welcome" {
		t.Fatalf("icon %q", a.doc.ed.Doc.Icon)
	}
	a.doc.ed.EditCover()
	tt.Frame()
	if err := tt.Click("封面：海"); err != nil {
		t.Fatal(err)
	}
	pic, _ := pngFile(t, "cover.png")
	a.pickFiles = func(done func([]string)) { done([]string{pic}) }
	a.doc.ed.EditCover()
	tt.Frame()
	if err := tt.Click("使用图片…"); err != nil {
		t.Fatal(err)
	}
	if a.doc.ed.Doc.Cover != "assets/cover.png" {
		t.Fatalf("cover %q", a.doc.ed.Doc.Cover)
	}
	saveSettle(tt)
	text, _ := vault.Read(root(a), "Welcome.arcmd")
	doc, err := model.Decode(text)
	if err != nil || doc.Icon != "🚀" || doc.Cover != "assets/cover.png" || doc.Version != 5 {
		t.Fatalf("saved %+v %v", doc, err)
	}
	a.refreshSearch()
	a.open("Journal/Ideas.arcmd")
	if got := a.iconTitle(a.state.ActiveSpace().ID, "Welcome.arcmd"); got != "🚀 Welcome" {
		t.Errorf("indexed icon %q", got)
	}
}

// HTML pasted from a browser keeps its structure.
func TestPasteHTML(t *testing.T) {
	a, tt := newTestApp(t)
	caretAtEnd(a, tt)
	tt.Key(0, ui.KeyEnter)
	tt.SetClipboardData(transfer.New(transfer.NewItem(
		transfer.Bytes(transfer.HTML, []byte("<h2>Pasted</h2><ul><li>one</li><li><b>two</b></li></ul>")),
		transfer.Bytes(transfer.Text, []byte("Pasted\none\ntwo")),
	)))
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	blocks := a.doc.ed.Doc.Blocks
	var tail []string
	for _, b := range blocks[len(blocks)-3:] {
		tail = append(tail, b.Kind+":"+b.Text())
	}
	if !slices.Equal(tail, []string{"heading:Pasted", "bullet:one", "bullet:two"}) {
		t.Fatalf("pasted %q", tail)
	}
	if !blocks[len(blocks)-1].Runs[0].Style.Bold {
		t.Error("bold lost")
	}
}

// Math shows its formula, and its TeX while the caret is in it.
func TestMathBlock(t *testing.T) {
	a, tt := newTestApp(t)
	caretAtEnd(a, tt)
	tt.Key(0, ui.KeyEnter)
	a.doc.ed.InsertMath()
	tt.Type(`\frac{a}{b}`)
	tt.Frame()
	math := a.doc.ed.Doc.Blocks[len(a.doc.ed.Doc.Blocks)-1]
	if math.Kind != "math" || math.Text() != `\frac{a}{b}` {
		t.Fatalf("block %+v", math)
	}
	// The section commands move headings with what they hold.
	a.doc.ed.SetSelection(0, 0)
	a.doc.ed.MoveSectionAtCaret(1) // the title does not move
	if a.doc.ed.Doc.Blocks[0].Text() != "Welcome to ArcMD" {
		t.Error("title moved")
	}
}
