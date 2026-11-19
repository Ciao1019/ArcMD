package app

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	docpkg "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/history"
	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestEditorIMEAutosaveAndModeHistory(t *testing.T) {
	a, tt := newTestApp(t)
	const source = "# Note\n\nKeep this exact.\n"
	if err := vault.Write(root(a), "Note.md", source); err != nil {
		t.Fatal(err)
	}
	a.open("Note.md")
	a.doc.ed.SetSelection(a.doc.ed.Len(), a.doc.ed.Len())
	a.doc.ed.RequestFocus = true
	tt.Frame()
	tt.Compose("中文", 2)
	tt.Advance(2 * autosaveDelay)
	if text, _ := vault.Read(root(a), "Note.md"); text != source {
		t.Fatal("autosave wrote preedit")
	}
	tt.Type("中文")
	tt.Frame()
	tt.Advance(autosaveDelay + time.Millisecond)
	if a.doc.rel != "Note.md" {
		t.Fatal("Markdown file changed path", a.doc.rel)
	}
	if _, err := os.Stat(filepath.Join(root(a), "Note.arcmd")); !os.IsNotExist(err) {
		t.Fatal("saving Markdown created a duplicate .arcmd file")
	}
	stored, _ := vault.Read(root(a), "Note.md")
	parsed := editor.ImportMarkdown(stored)
	if !strings.HasSuffix(parsed.PlainText(), "中文") {
		t.Fatalf("in-place Markdown commit failed: %q", stored)
	}

	ed := a.doc.ed
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	if a.doc.ed != ed {
		t.Fatal("mode switch replaced the editor")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	tt.Key(ui.Cmd, ui.KeyZ)
	if a.doc.ed.Text() != "Note\nKeep this exact." {
		t.Fatal("mode switch lost composition history")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	if a.doc.ed.Text() != "Note\nKeep this exact.中文" {
		t.Fatal("redo failed across modes")
	}
}

func TestAttachmentCommandPreservesSourceAndCopiesAsset(t *testing.T) {
	a, tt := newTestApp(t)
	before := a.doc.text()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 24, 16))); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "picture one.png")
	if err := os.WriteFile(source, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	a.doc.ed.SetSelection(a.doc.ed.Len(), a.doc.ed.Len())
	a.pickFiles = func(done func([]string)) { done([]string{source, source}) }
	a.pickImages()
	tt.Frame()
	if !strings.HasPrefix(a.doc.text(), before) || !strings.Contains(a.doc.text(), "assets/picture-one-2.png") {
		t.Fatal(a.doc.text())
	}
	for _, name := range []string{"picture-one.png", "picture-one-2.png"} {
		stored, err := os.ReadFile(filepath.Join(root(a), "assets", name))
		if err != nil || !bytes.Equal(stored, data.Bytes()) {
			t.Fatal("asset copy failed", err)
		}
	}
	if a.doc.loadImage("assets/picture-one.png") == nil {
		t.Fatal("local image was not decoded")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if a.doc.text() != before {
		t.Fatal("attachment insertion did not undo atomically")
	}
}

func TestCommandBarFormatsCustomEditor(t *testing.T) {
	a, tt := newTestApp(t)
	a.doc.ed.SetSelection(0, 7)
	before := a.doc.text()
	tt.Key(ui.Cmd, ui.KeyK)
	a.palette.query = "加粗"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.palette.open || !tt.Focused("文档") || !strings.HasPrefix(a.doc.text(), "# **Welcome**") {
		t.Fatal("command did not format and refocus text")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if a.doc.text() != before {
		t.Fatal("palette formatting did not undo")
	}
}

func TestSaveFailureKeepsUnsavedSource(t *testing.T) {
	a, tt := newTestApp(t)
	a.doc.ed.ReplaceText(nil, "unsaved")
	doc := a.doc
	originalRoot := doc.space.Root
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	doc.space.Root = filepath.Join(blocker, "space")
	tt.Frame()
	tt.Advance(autosaveDelay + time.Millisecond)
	if doc.saveErr == nil || !doc.dirty() || !tt.HasText("保存失败") {
		t.Fatal("save failure state missing")
	}
	doc.space.Root = originalRoot
	tt.Key(ui.Cmd, ui.KeyS)
	if doc.saveErr != nil || doc.dirty() {
		t.Fatal("explicit retry failed")
	}
}

func TestInvalidNativeDocumentCannotOverwriteFile(t *testing.T) {
	a, tt := newTestApp(t)
	const source = `{"format":"arcmd","version":99,"blocks":[]}`
	if err := vault.Write(root(a), "Future.arcmd", source); err != nil {
		t.Fatal(err)
	}
	a.open("Future.arcmd")
	tt.Frame()
	if a.doc.loadErr == nil || !tt.HasText("文档无法打开") {
		t.Fatal("invalid format was editable")
	}
	tt.Advance(3 * autosaveDelay)
	got, _ := vault.Read(root(a), "Future.arcmd")
	if got != source {
		t.Fatal("future format overwritten")
	}
}
func TestMarkdownSavesInPlaceAndExports(t *testing.T) {
	a, tt := newTestApp(t)
	const original = "# Original\n\nText\n"
	if err := vault.Write(root(a), "Import.md", original); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root(a), "Import.arcmd", "occupied"); err != nil {
		t.Fatal(err)
	}
	a.open("Import.md")
	a.doc.ed.ReplaceText(nil, "Hello ")
	if !a.flush() || a.doc.rel != "Import.md" {
		t.Fatal("in-place save failed", a.doc.rel)
	}
	a.exportMarkdown()
	tt.Frame()
	if got, _ := vault.Read(root(a), "Import.md"); !strings.Contains(got, "Hello Original") {
		t.Fatalf("did not save Markdown in place: %q", got)
	}
	if got, _ := vault.Read(root(a), "Import.arcmd"); got != "occupied" {
		t.Fatal("overwrote neighbor")
	}
	if _, err := os.Stat(filepath.Join(root(a), "Import Export.md")); err != nil {
		t.Fatal("export missing", err)
	}
	encoded, _ := vault.Read(root(a), a.doc.rel)
	restored := openDocument(a.doc.space, a.doc.rel, encoded, false)
	if restored.loadErr != nil || restored.text() != a.doc.text() {
		t.Fatal("rich restore failed")
	}
}

// A document switched to draws its text in the first frame: the editor
// used to cull every row in that frame, as its viewport was not measured
// yet, and nothing asked for another, so the page stayed blank until the
// pointer moved.
func TestSwitchDrawsTextInFirstFrame(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1240, 760)
	for range 3 {
		tt.Frame()
	}
	a.open("Journal/Ideas.md")
	tt.Frame()
	img := tt.Image()
	b := img.Bounds()
	scale := b.Dx() / 1240
	dark := 0
	// The text column, below the page's top margin and its first line.
	for y := 160 * scale; y < 200*scale; y++ {
		for x := 300 * scale; x < 700*scale; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r>>8 < 100 && g>>8 < 100 && bl>>8 < 100 {
				dark++
			}
		}
	}
	if dark < 50 {
		t.Fatalf("the first frame drew no text (%d dark pixels)", dark)
	}
}

func TestRealtimeInPlaceMarkdownPreservesCustomStylesAndLegacyArcmd(t *testing.T) {
	a, tt := newTestApp(t)
	const initial = "---\ntags: [design]\n---\n\n# Styled\n\nPlain body.\n"
	if err := vault.Write(root(a), "Styled.md", initial); err != nil {
		t.Fatal(err)
	}
	a.open("Styled.md")
	tt.Frame()

	// Apply custom ArcMD page metadata, inline styles/annotations, callout, table, and columns.
	a.doc.ed.Engine.SetPageIcon("🎨")
	a.doc.ed.Engine.SetCover(".assets/banner.png")
	a.doc.ed.Engine.SetCoverY(0.42)
	a.doc.ed.Engine.SetGoal(1200)

	styledPara := docpkg.NewBlock("paragraph", "")
	styledPara.Runs = []docpkg.Run{
		{Text: "Color ", Style: docpkg.Style{Color: "red", Bold: true}},
		{Text: "drawn", Style: docpkg.Style{Highlight: "light-yellow", Annotation: "circle", AnnotationColor: "purple"}},
		{Text: " link [[Ideas]] #roadmap"},
	}
	callout := docpkg.NewBlock("callout", "Custom callout")
	callout.Color = "green"
	callout.Emoji = "🌱"

	table := docpkg.Table(2, 2)
	table.Widths = []float32{220, 180}
	table.Children[0].Children[0].Runs = []docpkg.Run{{Text: "Merged header", Style: docpkg.Style{Bold: true}}}
	table.Children[0].Children[0].ColSpan = 2
	table.Children[0].Children[0].Background = "light-blue"
	table.Children[1].Children[0].Runs = []docpkg.Run{{Text: "A"}}
	table.Children[1].Children[1].Runs = []docpkg.Run{{Text: "B"}}

	cols := docpkg.Columns()
	cols.Children[0].Children = []*docpkg.Block{docpkg.NewBlock("paragraph", "Left col")}
	cols.Children[1].Children = []*docpkg.Block{docpkg.NewBlock("paragraph", "Right col")}

	next := a.doc.ed.Doc.Clone()
	next.Blocks = []*docpkg.Block{next.Blocks[0], styledPara, callout, table, cols}
	a.doc.ed.Engine.ReplaceDocument(next)
	tt.Frame()
	tt.Advance(autosaveDelay + time.Millisecond)

	if a.doc.rel != "Styled.md" {
		t.Fatalf("expected Styled.md in place, got %q", a.doc.rel)
	}
	if _, err := os.Stat(filepath.Join(root(a), "Styled.arcmd")); !os.IsNotExist(err) {
		t.Fatal("editing Styled.md spawned Styled.arcmd")
	}
	diskMD, err := vault.Read(root(a), "Styled.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"tags: [design]", "[[Ideas]]", "#roadmap"} {
		if !strings.Contains(diskMD, want) {
			t.Fatalf("missing %q in saved Styled.md:\n%s", want, diskMD)
		}
	}

	reopened := openDocument(a.doc.space, "Styled.md", diskMD, false)
	if reopened.loadErr != nil {
		t.Fatal(reopened.loadErr)
	}
	rdoc := reopened.ed.Doc
	if rdoc.Icon != "🎨" || rdoc.Cover != ".assets/banner.png" || rdoc.Goal != 1200 {
		t.Fatalf("page metadata lost: icon=%q cover=%q goal=%d", rdoc.Icon, rdoc.Cover, rdoc.Goal)
	}
	if len(rdoc.Blocks) < 5 {
		t.Fatalf("expected 5 blocks on reload, got %d", len(rdoc.Blocks))
	}
	if rdoc.Blocks[1].Runs[0].Style.Color != "red" || rdoc.Blocks[1].Runs[1].Style.Annotation != "circle" || rdoc.Blocks[1].Runs[1].Style.AnnotationColor != "purple" {
		t.Fatalf("inline custom styles lost: %+v", rdoc.Blocks[1].Runs)
	}
	if rdoc.Blocks[2].Kind != "callout" || rdoc.Blocks[2].Color != "green" || rdoc.Blocks[2].Emoji != "🌱" {
		t.Fatalf("callout lost: %+v", rdoc.Blocks[2])
	}
	if rdoc.Blocks[3].Kind != "table" || rdoc.Blocks[3].Children[0].Children[0].ColSpan != 2 || rdoc.Blocks[3].Children[0].Children[0].Background != "light-blue" {
		t.Fatalf("table metadata lost: %+v", rdoc.Blocks[3].Children[0].Children[0])
	}
	if rdoc.Blocks[4].Kind != "columns" || len(rdoc.Blocks[4].Children) != 2 {
		t.Fatalf("columns lost: %+v", rdoc.Blocks[4])
	}

	// External .md file edits also autosave directly in place without creating .arcmd.
	extDir := t.TempDir()
	extPath := filepath.Join(extDir, "Outside.md")
	if err := os.WriteFile(extPath, []byte("# Outside\n\nHello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openSystemFile(extPath)
	tt.Frame()
	a.doc.ed.SetSelection(a.doc.ed.Len(), a.doc.ed.Len())
	a.doc.ed.ReplaceText(nil, " world")
	tt.Frame()
	tt.Advance(autosaveDelay + time.Millisecond)
	if _, err := os.Stat(filepath.Join(extDir, "Outside.arcmd")); !os.IsNotExist(err) {
		t.Fatal("external .md created Outside.arcmd")
	}
	extSaved, err := os.ReadFile(extPath)
	if err != nil || !strings.Contains(string(extSaved), "Hello world") {
		t.Fatalf("external .md not updated in place: %q (%v)", string(extSaved), err)
	}

	// Legacy .arcmd files remain readable and editable in place.
	a.switchSpace(a.state.Spaces[0].ID)
	legacyDoc := editor.ImportMarkdown("# Legacy\n\nStill works.\n")
	legacyEncoded, err := legacyDoc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root(a), "Legacy.arcmd", legacyEncoded); err != nil {
		t.Fatal(err)
	}
	a.open("Legacy.arcmd")
	tt.Frame()
	if a.doc.loadErr != nil {
		t.Fatalf("legacy .arcmd failed to open: %v", a.doc.loadErr)
	}
	a.doc.ed.SetSelection(a.doc.ed.Len(), a.doc.ed.Len())
	a.doc.ed.ReplaceText(nil, " Updated!")
	tt.Frame()
	tt.Advance(autosaveDelay + time.Millisecond)
	legacySaved, err := vault.Read(root(a), "Legacy.arcmd")
	if err != nil {
		t.Fatal(err)
	}
	decodedLegacy, err := docpkg.Decode(legacySaved)
	if err != nil || !strings.Contains(decodedLegacy.PlainText(), "Still works. Updated!") {
		t.Fatalf("legacy .arcmd round-trip failed: %v", err)
	}
}

func TestBottomBlockHandleMenuAndIndentSubmenu(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1100, 720)
	var lines []string
	lines = append(lines, "# Bottom Handle Test")
	for i := 1; i <= 26; i++ {
		lines = append(lines, fmt.Sprintf("Paragraph %d with enough text to fill the viewport.", i))
	}
	if err := vault.Write(root(a), "Bottom.md", strings.Join(lines, "\n\n")+"\n"); err != nil {
		t.Fatal(err)
	}
	a.open("Bottom.md")
	tt.Frame()

	// Append a trailing empty block with Indent=1, matching Issue #2's trailing block scenario.
	end := a.doc.ed.Len()
	a.doc.ed.SetSelection(end, end)
	a.doc.ed.Engine.Edit(func() {
		a.doc.ed.Engine.Insert(docpkg.NewBlock("paragraph", ""))
		a.doc.ed.Engine.Indent(1)
	})
	tt.Frame()

	end = a.doc.ed.Len()
	a.doc.ed.SetSelection(end, end)
	a.doc.ed.RequestFocus = true
	tt.Frame()

	// Scroll so the trailing empty block sits near the bottom edge of the window.
	caret, _, ok := a.doc.ed.BoundsForRange(ui.TextInputRange{Start: end, End: end})
	if !ok {
		t.Fatal("missing caret bounds for trailing block")
	}
	docBounds, ok := tt.Find("文档")
	if !ok {
		t.Fatal("missing 文档 surface")
	}
	a.doc.ed.Scroll.Y = max(0, caret.Y-(docBounds.H-44))
	tt.Frame()
	tt.Frame()

	docBounds, ok = tt.Find("文档")
	if !ok {
		t.Fatal("missing 文档 surface after scroll")
	}
	caret, _, ok = a.doc.ed.BoundsForRange(ui.TextInputRange{Start: end, End: end})
	if !ok {
		t.Fatal("missing caret bounds after scroll")
	}
	// Paragraph with Indent=1 shifts text right by 20px; its 24x24 handle sits at padding - 30 = caret.X - 50.
	handleWinX := docBounds.X + caret.X - 50 + 12
	handleWinY := docBounds.Y - a.doc.ed.Scroll.Y + caret.Y + max(caret.H, 20)/2
	tt.Move(docBounds.X+caret.X, handleWinY)
	tt.ClickAt(handleWinX, handleWinY)
	tt.Advance(150 * time.Millisecond)

	assertWithin := func(label string, minH float32) ui.Rect {
		t.Helper()
		r, found := tt.Find(label)
		if !found {
			t.Fatalf("missing %q in menu", label)
		}
		if r.W <= 0 || r.H < minH || r.X < 0 || r.Y < 0 || r.X+r.W > 1100 || r.Y+r.H > 720 {
			t.Fatalf("%q rect %+v clips outside 1100x720 (want H>=%g)", label, r, minH)
		}
		return r
	}

	bookmarkKey := a.shortKey("bookmark")
	for _, label := range []string{"正文", "一级标题", "五级标题", "缩进和对齐", "剪切", "复制", "创建副本", "删除", "上移", "下移", bookmarkKey, "在下方添加"} {
		minH := float32(32)
		switch label {
		case "正文", "一级标题", "五级标题":
			minH = 24
		case bookmarkKey:
			minH = 12
		}
		assertWithin(label, minH)
	}

	// Hover "缩进和对齐" to expand the side submenu and verify all items (including 减少缩进) stay visible and clickable.
	indentRow := assertWithin("缩进和对齐", 32)
	tt.Move(indentRow.X+indentRow.W/2, indentRow.Y+indentRow.H/2)
	tt.Advance(150 * time.Millisecond)

	for _, label := range []string{"左对齐", "居中对齐", "右对齐", "增加缩进", "减少缩进"} {
		assertWithin(label, 32)
	}
	if err := tt.Click("减少缩进"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	last := a.doc.ed.Doc.Blocks[len(a.doc.ed.Doc.Blocks)-1]
	if last.Indent != 0 {
		t.Fatalf("expected trailing block indent to decrease to 0, got %d", last.Indent)
	}
}

func TestPaletteFollowsArrowKeysInShortWindow(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(900, 320)
	tt.Frame()

	tt.Key(ui.Cmd, ui.KeyK)
	tt.Advance(150 * time.Millisecond)
	if !a.palette.open {
		t.Fatal("expected command palette to open")
	}
	res := a.results()
	if len(res) < 5 {
		t.Fatalf("expected enough palette items to overflow 320px window, got %d", len(res))
	}
	for range len(res) - 1 {
		tt.Key(0, ui.KeyDown)
	}
	lastTitle := res[len(res)-1].title
	r, ok := tt.Find(lastTitle)
	if !ok {
		t.Fatalf("missing last palette item %q", lastTitle)
	}
	if r.W <= 0 || r.H < 28 || r.Y < 0 || r.Y+r.H > 320 {
		t.Fatalf("last palette item %q rect %+v not visible in 900x320 (want H>=28)", lastTitle, r)
	}
}

func TestAppHTMLEndToEndWorkflow(t *testing.T) {
	a, tt := newTestApp(t)
	const initialMD = "# HTML E2E\n\n<div align=\"center\">\n\n居中内容 <kbd>Tab</kbd>\n\n</div>\n\n<!-- 隐藏审稿注释 -->\n\n<hr>\n\n正文末尾\n"
	if err := vault.Write(root(a), "HtmlFlow.md", initialMD); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("HtmlFlow.md")
	ed := a.doc.ed
	ed.SetSelection(ed.Len(), ed.Len())
	ed.RequestFocus = true
	tt.Frame()

	// Append a new paragraph below the trailing comment block via Enter + live HTML tags.
	tt.Key(0, ui.KeyEnter)
	for _, r := range "<kbd>Alt</kbd>" {
		tt.Type(string(r))
	}
	// Rollback -> Ctrl+Z -> Enter -> '<' must stay plain text.
	tt.Key(0, ui.KeyBackspace)
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Key(0, ui.KeyEnter)
	tt.Type("<")
	lastBlk := ed.Doc.Blocks[len(ed.Doc.Blocks)-1]
	if lastBlk.Text() != "<" || lastBlk.Runs[0].Style.Kbd {
		t.Fatalf("trailing new paragraph leaked Kbd onto <: %+v", lastBlk)
	}

	// Type additional live HTML inline formats on that new line.
	tt.Key(0, ui.KeyBackspace)
	for _, r := range "<u>重点</u> x<sup>2</sup> H<sub>2</sub>O <mark>hi</mark> <q>引</q> line1<br>line2 " {
		tt.Type(string(r))
	}
	tt.Type("EnterKey")
	end := ed.Len()
	ed.SetSelection(end-len("EnterKey"), end)
	tt.Frame()
	ed.Wrap("<kbd>")
	tt.Frame()

	// Autosave and reload .md from disk to verify persistence and round-trip.
	tt.Advance(autosaveDelay + time.Millisecond)
	stored, err := vault.Read(root(a), "HtmlFlow.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored, `<kbd>Alt</kbd>`) || !strings.Contains(stored, `<kbd>EnterKey</kbd>`) {
		t.Fatalf("saved Markdown missing <kbd> tags:\n%s", stored)
	}
	if !strings.Contains(stored, `<div align="center">`) || !strings.Contains(stored, `<!-- 隐藏审稿注释 -->`) {
		t.Fatalf("saved Markdown lost container or comment:\n%s", stored)
	}

	// Toggle Markdown preview and back without losing live editor document state.
	a.toggleSource()
	if !a.doc.source {
		t.Fatal("expected Markdown preview mode to be active")
	}
	a.toggleSource()
	if a.doc.source {
		t.Fatal("expected return to rich-text editing mode")
	}

	// Verify HTML export preserves <kbd> styling and omits standalone HTML comment block.
	htmlFrag := editor.ExportHTML(a.doc.ed.Doc)
	if !strings.Contains(htmlFrag, `<kbd>Alt</kbd>`) || strings.Contains(htmlFrag, "隐藏审稿注释") {
		t.Fatalf("ExportHTML unexpected output:\n%s", htmlFrag)
	}
	fullHTML := htmlPage("HtmlFlow", htmlFrag)
	if !strings.Contains(fullHTML, "kbd {") {
		t.Fatal("htmlPage missing kbd CSS rules")
	}

	// Verify vector PDF export renders without error.
	o := editor.Options{Width: paperWidth, Theme: documentTheme(false), Page: pageStyle(false)}
	var tops []float32
	ui.Render(func(c *ui.Context) {
		tops = a.doc.ed.Paginate(o, float32(paperHeight-2*paperMargin))
	}, paperWidth, paperHeight, 1)
	if len(tops) == 0 {
		t.Fatal("expected at least one PDF page")
	}
	var pdfBuf bytes.Buffer
	w := pdf.NewWriter(&pdfBuf, "HtmlFlow")
	vp := w.NewVectorPage(pdf.A4Width, pdf.A4Height)
	a.doc.ed.ExportVectorPage(vp, o, tops[0], tops[0]+float32(paperHeight-2*paperMargin), 0, len(tops), tops, paperWidth, paperHeight, paperMargin)
	if err := w.FinishVectorPage(vp); err != nil {
		t.Fatalf("FinishVectorPage failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("pdf close failed: %v", err)
	}
	if !bytes.HasPrefix(pdfBuf.Bytes(), []byte("%PDF-")) {
		t.Fatal("expected valid PDF output")
	}

	// Reload saved .md file from disk into a fresh editor instance.
	if err := vault.Write(root(a), "NoteCheckFallback.md", "# tmp\n"); err != nil {
		t.Fatal(err)
	}
	a.open("NoteCheckFallback.md")
	a.open("HtmlFlow.md")
	reloaded := a.doc.ed.Doc.PlainText()
	if !strings.Contains(reloaded, "Alt") || !strings.Contains(reloaded, "EnterKey") {
		t.Fatalf("reloaded .md missing keycap text: %q", reloaded)
	}

	// Verify app-level full-text search indexes text inside <kbd> and <div align="center">.
	a.refreshSearch()
	if hits := a.search.index.SearchMulti("EnterKey", nil, 5); len(hits) == 0 {
		t.Fatal("full-text search failed to index text inside <kbd>")
	}
	if hits := a.search.index.SearchMulti("居中内容", nil, 5); len(hits) == 0 {
		t.Fatal("full-text search failed to index unwrapped <div align=center> content")
	}

	// Verify Find & Replace updates adjacent text without corrupting <kbd> styling.
	ed = a.doc.ed
	ed.OpenFind(true)
	tt.Frame()
	tt.Type("正文末尾")
	if err := tt.Click("替换为"); err != nil {
		t.Fatalf("click replace input failed: %v", err)
	}
	tt.Type("正尾替换")
	if err := tt.Click("全部替换"); err != nil {
		t.Fatalf("click replace-all failed: %v", err)
	}
	if !strings.Contains(ed.Doc.PlainText(), "正尾替换") {
		t.Fatalf("Find & Replace did not update text adjacent to <kbd>: %q", ed.Doc.PlainText())
	}
	foundAltKbd := false
	for _, b := range ed.Doc.Blocks {
		for _, r := range b.Runs {
			if r.Text == "Alt" && r.Style.Kbd {
				foundAltKbd = true
			}
		}
	}
	if !foundAltKbd {
		t.Fatal("Find & Replace corrupted <kbd>Alt</kbd> run")
	}

	// Verify v9 .arcmd version history milestone works cleanly with Style.Kbd.
	encodedV9, err := ed.Doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := history.SaveMilestone(root(a), "HtmlFlow.md", encodedV9, "v9-kbd", time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("history.SaveMilestone failed on v9 document: %v", err)
	}
	a.openVersions()
	tt.Frame()
	if len(a.versions.versions) == 0 {
		t.Fatal("expected v9 milestone version entry")
	}
}
