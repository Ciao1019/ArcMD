package app

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/vault"
)

func writeTestPNGFile(t *testing.T, path string, w, h int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: 22, G: 163, B: 74, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAsyncAutosaveAndAsyncImageHeaderDecode(t *testing.T) {
	a, tt := newTestApp(t)
	updates := make(chan func(), 16)
	a.search.async = true
	a.runAsync = func(fn func()) { updates <- fn }
	defer func() {
		a.search.async = false
		a.runAsync = nil
	}()

	// 1. Header-first async image decode returns exact dimensions immediately,
	// then populates cache and invalidates the editor once pixels finish decoding.
	writeTestPNGFile(t, filepath.Join(root(a), "assets", "banner.png"), 280, 140)
	res0 := a.imageResourceFor(a.doc, "assets/banner.png")
	if !res0.Loading || res0.Width != 280 || res0.Height != 140 || res0.Bitmap != nil {
		t.Fatalf("expected header-sized loading placeholder, got %+v", res0)
	}
	fn := <-updates
	fn()
	res1 := a.imageResourceFor(a.doc, "assets/banner.png")
	if res1.Loading || res1.Bitmap == nil || res1.Width != 280 || res1.Height != 140 {
		t.Fatalf("expected decoded cached bitmap after async post, got %+v", res1)
	}

	// 2. Background autosave writes Markdown + history off-thread and marks clean.
	d := a.doc
	d.ed.SetSelection(d.ed.Len(), d.ed.Len())
	d.ed.ReplaceText(nil, "\n\nBackground autosave line.")
	tt.Frame()
	if !d.dirty() {
		t.Fatal("document should be dirty before autosave delay")
	}
	tt.Advance(autosaveDelay + 50*time.Millisecond)
	if !d.saving {
		t.Fatal("autosave did not dispatch background save worker")
	}
	saveCallback := <-updates
	saveCallback()
	if d.saving || d.dirty() || d.changed() {
		t.Fatalf("async autosave did not reconcile clean baseline: saving=%v dirty=%v changed=%v", d.saving, d.dirty(), d.changed())
	}
	disk, err := vault.Read(root(a), d.rel)
	if err != nil || !strings.Contains(disk, "Background autosave line.") {
		t.Fatalf("async autosave did not write file: err=%v disk=%q", err, disk)
	}
}

func TestPreviewLazyTruncationAndModeSyncCrossFade(t *testing.T) {
	a, tt := newTestApp(t)
	var b strings.Builder
	b.WriteString("# Huge Note\n\n")
	for i := range 80 {
		fmt.Fprintf(&b, "Section %03d paragraph content for lazy preview and mode sync.\n\n", i)
	}
	b.WriteString("```go\n" + strings.Repeat("fmt.Println(\"fence\")\n", 180) + "```\n")
	raw := b.String()
	if err := vault.Write(root(a), "Huge.md", raw); err != nil {
		t.Fatal(err)
	}

	// Closed-document thumbnail parses only a truncated prefix and caps to previewMaxBlocks.
	thumb := a.thumbOf(a.state.ActiveSpace(), "Huge.md")
	if thumb == nil || len(thumb.ed.Doc.Blocks) > previewMaxBlocks {
		t.Fatalf("thumbnail document was not capped to %d blocks: got %d", previewMaxBlocks, len(thumb.ed.Doc.Blocks))
	}

	// Open-document thumbnail also clones only previewMaxBlocks blocks.
	a.open("Huge.md")
	tt.Frame()
	openThumb := a.thumbOf(a.state.ActiveSpace(), "Huge.md")
	if openThumb == nil || len(openThumb.ed.Doc.Blocks) != previewMaxBlocks {
		t.Fatalf("open thumbnail did not cap blocks: got %d", len(openThumb.ed.Doc.Blocks))
	}

	// Place caret in block 40 and toggle to Markdown preview (⇧⌘M): previewCaret maps to block 40!
	focusDocument(a, tt)
	leaf40, ok := a.doc.ed.Engine.LeafByID(a.doc.ed.Doc.Blocks[40].ID)
	if !ok {
		t.Fatal("missing block 40")
	}
	a.doc.ed.SetSelection(leaf40.Start, leaf40.Start)
	tt.Frame()
	tt.Advance(250 * time.Millisecond)
	a.doc.toggleMode()
	tt.Frame()
	if !a.doc.source || a.doc.previewCaret <= 0 {
		t.Fatalf("toggleMode to Markdown did not map reading offset: source=%v caret=%d", a.doc.source, a.doc.previewCaret)
	}
	if a.doc.modeAt.IsZero() {
		t.Fatal("toggleMode did not arm mode cross-fade")
	}
	tt.Advance(120 * time.Millisecond)
	tt.Frame()
	a.doc.toggleMode()
	tt.Frame()
	if a.doc.source {
		t.Fatal("second toggleMode did not return to rich editor")
	}
}

func TestSplitSmoothFactors(t *testing.T) {
	a, tt := splitTestApp(t)
	tt.Frame()
	// Adding a third pane starts at a compact shownFactor and eases smoothly toward 1/3.
	a.addSplitPane()
	if len(a.split.panes) != 3 {
		t.Fatalf("expected 3 split panes, got %d", len(a.split.panes))
	}
	added := a.split.panes[len(a.split.panes)-1]
	if !added.shownReady || added.shownFactor >= added.factor {
		t.Fatalf("new split pane did not start compact for smooth expansion: shown=%v target=%v", added.shownFactor, added.factor)
	}
	tt.Frame()
	tt.Advance(30 * time.Millisecond)
	if added.shownFactor <= 0.16 || added.shownFactor >= added.factor {
		t.Fatalf("split pane factor did not ease smoothly: shown=%v target=%v", added.shownFactor, added.factor)
	}
	tt.Advance(500 * time.Millisecond)
	if added.shownFactor != added.factor {
		t.Fatalf("split pane factor did not settle: shown=%v target=%v", added.shownFactor, added.factor)
	}
}
