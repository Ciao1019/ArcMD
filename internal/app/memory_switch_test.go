package app

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
	"weak"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/history"
	"github.com/Ciao1019/ArcMD/internal/merge"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func makeTestPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func runGC() {
	for range 5 {
		runtime.GC()
		runtime.Gosched()
	}
}

func setupAsyncHarness(a *App) (drain func()) {
	var mu sync.Mutex
	var queue []func()
	a.search.async = true
	a.runAsync = func(fn func()) {
		mu.Lock()
		queue = append(queue, fn)
		mu.Unlock()
	}
	return func() {
		for {
			mu.Lock()
			batch := queue
			queue = nil
			mu.Unlock()
			for _, fn := range batch {
				fn()
			}
			if !a.search.busy && activeImageDecodes.Load() == 0 {
				mu.Lock()
				pending := len(queue)
				mu.Unlock()
				if pending == 0 {
					return
				}
			}
			runtime.Gosched()
		}
	}
}

// TestSwitchMarkdownFilesReclaimsClosedDocumentsAndBitmaps verifies Issue #33:
// repeatedly switching back and forth across Markdown files with local images,
// missing images, and remote image links must release closed documents, editors,
// and decoded bitmaps instead of accumulating them in imgResolved.
func TestSwitchMarkdownFilesReclaimsClosedDocumentsAndBitmaps(t *testing.T) {
	a, tt := newTestApp(t)
	drain := setupAsyncHarness(a)

	pngData := makeTestPNG(720, 540)
	if err := vault.Write(root(a), "assets/shot1.png", string(pngData)); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root(a), "assets/shot2.png", string(pngData)); err != nil {
		t.Fatal(err)
	}
	for i := range 4 {
		content := fmt.Sprintf(
			"# Note %d\n\n![one](assets/shot1.png)\n\nParagraph %d with [[Welcome]].\n\n![two](assets/shot2.png)\n\n![missing](assets/missing.png)\n\n![remote](https://example.com/pic.png)\n",
			i, i,
		)
		if err := vault.Write(root(a), fmt.Sprintf("Doc%d.md", i), content); err != nil {
			t.Fatal(err)
		}
	}
	a.refreshTree()
	a.refreshSearch()
	drain()
	tt.Frame()

	// Warm up one cycle before taking the baseline heap measurement.
	a.open("Doc0.md")
	tt.Frame()
	drain()
	tt.Frame()
	runGC()

	var mBefore runtime.MemStats
	runtime.ReadMemStats(&mBefore)

	var seenDocs []weak.Pointer[document]
	var seenEds []weak.Pointer[editor.Editor]
	var seenBitmaps []weak.Pointer[ui.Bitmap]

	const switches = 28
	for step := range switches {
		a.open(fmt.Sprintf("Doc%d.md", (step+1)%4))
		tt.Frame()
		drain()
		tt.Advance(24 * time.Millisecond)
		tt.Frame()

		seenDocs = append(seenDocs, weak.Make(a.doc))
		seenEds = append(seenEds, weak.Make(a.doc.ed))
		for _, c := range a.doc.imgCache {
			if c.resource.Bitmap != nil {
				seenBitmaps = append(seenBitmaps, weak.Make(c.resource.Bitmap))
			}
		}
	}

	// Settle any UI transitions before checking weak references.
	for range 8 {
		tt.Advance(50 * time.Millisecond)
		tt.Frame()
	}
	runGC()

	var mAfter runtime.MemStats
	runtime.ReadMemStats(&mAfter)

	for k := range a.imgResolved {
		if k.doc != a.doc {
			t.Fatalf("imgResolved retained closed document %q while %q is open", k.doc.rel, a.doc.rel)
		}
	}

	aliveDocs, aliveEds, aliveBitmaps := 0, 0, 0
	for _, w := range seenDocs {
		if w.Value() != nil {
			aliveDocs++
		}
	}
	for _, w := range seenEds {
		if w.Value() != nil {
			aliveEds++
		}
	}
	for _, w := range seenBitmaps {
		if w.Value() != nil {
			aliveBitmaps++
		}
	}

	if len(seenBitmaps) < switches*2 {
		t.Fatalf("expected at least %d decoded bitmaps across switches, saw %d", switches*2, len(seenBitmaps))
	}
	if aliveDocs != 1 {
		t.Fatalf("expected only 1 live document after %d switches, got %d", switches, aliveDocs)
	}
	if aliveEds != 1 {
		t.Fatalf("expected only 1 live editor after %d switches, got %d", switches, aliveEds)
	}
	if aliveBitmaps > 2 {
		t.Fatalf("expected at most 2 live bitmaps (for the active document), got %d/%d", aliveBitmaps, len(seenBitmaps))
	}
	heapDeltaMB := float64(int64(mAfter.HeapAlloc)-int64(mBefore.HeapAlloc)) / (1024 * 1024)
	if heapDeltaMB > 12 {
		t.Fatalf("expected bounded heap delta after %d switches, got +%.2f MB", switches, heapDeltaMB)
	}
	runtime.KeepAlive(tt)
}

func TestRapidSwitchCancelsPendingImageDecodesForClosedDocuments(t *testing.T) {
	a, tt := newTestApp(t)
	drain := setupAsyncHarness(a)

	pngData := makeTestPNG(640, 480)
	if err := vault.Write(root(a), "assets/rapid.png", string(pngData)); err != nil {
		t.Fatal(err)
	}
	for i := range 8 {
		if err := vault.Write(root(a), fmt.Sprintf("Fast%d.md", i), fmt.Sprintf("# Fast %d\n\n![](assets/rapid.png)\n", i)); err != nil {
			t.Fatal(err)
		}
	}
	a.refreshTree()

	var closedRefs []*document
	for i := range 8 {
		a.open(fmt.Sprintf("Fast%d.md", i))
		tt.Frame()
		if i < 7 {
			closedRefs = append(closedRefs, a.doc)
		}
	}
	for _, d := range closedRefs {
		if !d.closed.Load() {
			t.Fatalf("expected closed flag set on superseded document %q", d.rel)
		}
	}

	drain()
	tt.Frame()

	for _, d := range closedRefs {
		if len(d.imgCache) != 0 {
			t.Fatalf("expected superseded document %q to hold 0 cached images after async drain, got %d", d.rel, len(d.imgCache))
		}
	}
	if len(a.doc.imgCache) != 1 {
		t.Fatalf("expected active document %q to hold its decoded image, got %d", a.doc.rel, len(a.doc.imgCache))
	}
}

func TestSidebarPreviewThumbsBoundedAndScaled(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()

	largePNG := makeTestPNG(1280, 960)
	if err := vault.Write(root(a), "assets/hero.png", string(largePNG)); err != nil {
		t.Fatal(err)
	}
	for i := range 14 {
		content := fmt.Sprintf("# Card %d\n\n![](assets/hero.png)\n\nBody text %d.\n", i, i)
		if err := vault.Write(root(a), fmt.Sprintf("Card%02d.md", i), content); err != nil {
			t.Fatal(err)
		}
	}
	a.refreshTree()
	a.refreshSearch()
	a.open("Card00.md")
	tt.Frame()

	// Wiki hover preview for non-resident documents must not populate preview.thumbs.
	wp := a.previewWikiFor(a.doc, "Card05")
	if wp.Missing || wp.Text == "" {
		t.Fatalf("expected wiki preview text for Card05, got %+v", wp)
	}
	if len(a.preview.thumbs) != 0 {
		t.Fatalf("expected previewWikiFor not to populate preview.thumbs, got %d entries", len(a.preview.thumbs))
	}

	var thumbDocs []weak.Pointer[document]
	for i := range 14 {
		rel := fmt.Sprintf("Card%02d.md", i)
		a.openPreview(previewTarget{kind: previewDoc, space: sp, rel: rel, anchor: ui.Rect{X: 16, Y: 120, W: 200, H: 36}})
		tt.Frame()
		d := a.thumbOf(sp, rel)
		if d == nil {
			t.Fatalf("expected thumbnail document for %s", rel)
		}
		// Non-open thumbnails decode images scaled down (<= 640px) rather than 1280x960.
		if i > 0 {
			c, ok := d.imgCache["assets/hero.png"]
			if !ok || c.resource.Bitmap == nil {
				t.Fatalf("expected scaled thumbnail bitmap for %s", rel)
			}
			bw, bh := c.resource.Bitmap.Size()
			if bw > 640 || bh > 640 {
				t.Fatalf("expected thumbnail bitmap scaled <= 640px, got %dx%d", bw, bh)
			}
		}
		thumbDocs = append(thumbDocs, weak.Make(d))
	}

	if len(a.preview.thumbs) > maxPreviewThumbs {
		t.Fatalf("expected preview.thumbs capped at %d, got %d", maxPreviewThumbs, len(a.preview.thumbs))
	}

	// Closing the preview drops any open/... clone immediately.
	a.preview.shown = nil
	a.preview.closing = false
	tt.Frame()
	for k := range a.preview.thumbs {
		if len(k) >= 5 && k[:5] == "open/" {
			t.Fatalf("expected open/ thumbnail entry %q dropped when preview closed", k)
		}
	}

	runGC()
	aliveThumbs := 0
	for _, w := range thumbDocs {
		if w.Value() != nil {
			aliveThumbs++
		}
	}
	if aliveThumbs > maxPreviewThumbs {
		t.Fatalf("expected at most %d live thumbnail documents after eviction, got %d", maxPreviewThumbs, aliveThumbs)
	}
	runtime.KeepAlive(tt)
}

func TestSpaceSwitchClearsActiveSessionDocAndReclaimsOldNotes(t *testing.T) {
	a, tt := newTestApp(t)
	personal := a.state.ActiveSpace()
	if err := vault.Write(personal.Root, "Alpha.md", "# Alpha\n"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(personal.Root, "Beta.md", "# Beta\n"); err != nil {
		t.Fatal(err)
	}
	workRoot := filepath.Join(t.TempDir(), "Work")
	work := &space.Space{ID: "work", Name: "Work", Root: workRoot}
	if err := vault.Write(workRoot, "Task.md", "# Task\n"); err != nil {
		t.Fatal(err)
	}
	a.state.Spaces = append(a.state.Spaces, work)
	a.refreshTree()

	a.open("Alpha.md")
	tt.Frame()
	alphaWeak := weak.Make(a.doc)

	a.switchSpace(work.ID)
	a.open("Task.md")
	tt.Frame()

	// Returning to Personal restores Alpha.md and clears Personal's inactive session.doc slot.
	a.switchSpace(personal.ID)
	tt.Frame()
	if s := a.sessions[personal.ID]; s != nil && s.doc != nil {
		t.Fatalf("expected active space session.doc to be nil after returning, got %q", s.doc.rel)
	}

	// Switching to Beta.md in Personal must now allow Alpha.md's document to be GC'd.
	a.open("Beta.md")
	for range 8 {
		tt.Advance(50 * time.Millisecond)
		tt.Frame()
	}
	runGC()

	if alphaWeak.Value() != nil {
		t.Fatalf("expected Alpha.md document to be reclaimed after switching to Beta.md")
	}
	runtime.KeepAlive(tt)
}

func TestClosedModalsReleaseDocumentAndDiffState(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	if err := vault.Write(sp.Root, "ModalDoc.md", "# Modal\n\nLine 1\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("ModalDoc.md")
	tt.Frame()

	conflictDoc := openDocument(sp, "ModalDoc.md", "# Modal\n\nConflict\n", false)
	conflictWeak := weak.Make(conflictDoc)

	a.versions = versionsState{
		open: false,
		for_: conflictDoc,
		diff: []history.Line{{Text: "old"}},
	}
	a.gitChanges = gitChangesState{
		open:  false,
		items: []gitChangeEntry{{Path: "ModalDoc.md", OldText: "old", NewText: "new"}},
	}
	a.mergeUI = threeWayMergeState{
		open:            false,
		diskConflictDoc: conflictDoc,
		session:         merge.NewSession("base", "yours", "theirs"),
		rawResult:       "merged",
	}
	conflictDoc = nil

	tt.Frame()
	runGC()

	if a.versions.for_ != nil || a.versions.diff != nil {
		t.Fatalf("expected closed versions modal state cleared")
	}
	if a.gitChanges.items != nil {
		t.Fatalf("expected closed gitChanges items cleared")
	}
	if a.mergeUI.session != nil || a.mergeUI.diskConflictDoc != nil || a.mergeUI.rawResult != "" {
		t.Fatalf("expected closed mergeUI state cleared")
	}
	if conflictWeak.Value() != nil {
		t.Fatalf("expected conflict document referenced only by closed modals to be reclaimed")
	}
	runtime.KeepAlive(tt)
}
