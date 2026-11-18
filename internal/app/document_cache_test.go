package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestDirtyCacheTracksTextStructureAndHistory(t *testing.T) {
	a, tt := newTestApp(t)
	if err := vault.Write(root(a), "Cache.md", "first\n\nsecond\n"); err != nil {
		t.Fatal(err)
	}
	a.open("Cache.md")
	d := a.doc
	d.ed.RequestFocus = true
	tt.Frame()
	check := func(want bool) {
		t.Helper()
		if got := d.dirty(); got != want || got != d.changed() {
			t.Fatalf("dirty = %v, full comparison = %v, want %v", got, d.changed(), want)
		}
	}
	check(false)
	d.ed.SetSelection(5, 5)
	tt.Type("😀")
	check(true)
	d.ed.SetSelection(d.ed.Len(), d.ed.Len())
	tt.Type("中文")
	check(true)
	tt.Key(ui.Cmd, ui.KeyZ)
	check(true)
	tt.Key(ui.Cmd, ui.KeyZ)
	check(false)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	check(true)
	// A text replacement can return exactly to the baseline without undoing.
	d.ed.ReplaceText(&ui.TextInputRange{Start: 5, End: 7}, "")
	check(false)
	d.ed.SetSelection(0, 5)
	tt.Key(ui.Cmd, ui.KeyB)
	check(true)
	d.ed.SetSelection(d.ed.Len(), d.ed.Len())
	tt.Type("tail")
	check(true)
	tt.Key(ui.Cmd, ui.KeyZ)
	check(true)
	tt.Key(ui.Cmd, ui.KeyZ)
	check(false)
	d.ed.Engine.Insert(model.Table(2, 2))
	tt.Frame()
	check(true)
	tt.Key(ui.Cmd, ui.KeyZ)
	check(false)
	// Skipping a revision must force a full comparison, then recover when clean.
	d.ed.ReplaceText(nil, "a")
	d.ed.ReplaceText(nil, "b")
	check(true)
	d.ed.Undo()
	d.ed.Undo()
	check(false)
}

func TestSaveDuringCompositionRefreshesCommittedBaseline(t *testing.T) {
	a, tt := newTestApp(t)
	d := a.doc
	d.ed.SetSelection(d.ed.Len(), d.ed.Len())
	d.ed.RequestFocus = true
	tt.Frame()
	tt.Type(" committed")
	committed := d.ed.Text()
	tt.Compose("候选", 2)
	if !d.dirty() {
		t.Fatal("committed edit lost dirty state during composition")
	}
	if err := d.save(); err != nil {
		t.Fatal(err)
	}
	stored, err := vault.Read(root(a), d.rel)
	if err != nil {
		t.Fatal(err)
	}
	parsed := editor.ImportMarkdown(stored)
	if parsed.PlainText() != committed {
		t.Fatalf("save included preedit: got %q, want %q", parsed.PlainText(), committed)
	}
	if d.dirty() || d.changed() {
		t.Fatal("saved committed baseline is dirty")
	}
	tt.Compose("新候选", 3)
	if d.dirty() || d.changed() {
		t.Fatal("preedit dirtied committed baseline")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if d.ed.Text() != committed || d.dirty() {
		t.Fatal("cancel composition changed saved content")
	}
	tt.Compose("新候选", 3)
	tt.Type("中文")
	if !d.dirty() || !d.changed() {
		t.Fatal("committing composition lost dirty state")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if d.ed.Text() != committed || d.dirty() {
		t.Fatal("undo composition did not restore saved baseline")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	if !d.dirty() {
		t.Fatal("redo composition lost dirty state")
	}
}

func TestMillionCharDocumentMemoryAndDirtyTracking(t *testing.T) {
	var b strings.Builder
	b.Grow(2_400_000)
	for sec := 1; utf8.RuneCountInString(b.String()) < 1_000_000; sec++ {
		fmt.Fprintf(&b, "## Section %d 百万字符测试\n\n", sec)
		for p := 0; p < 6; p++ {
			fmt.Fprintf(&b, "第 %d 节第 %d 段内容：验证 **加粗**、`code`、[[Welcome]] 链接以及百万字文档打开与增量保存的内存表现。\n\n", sec, p+1)
		}
	}
	md := b.String()
	b.Reset()

	a, tt := newTestApp(t)
	if err := vault.Write(root(a), "Million.md", md); err != nil {
		t.Fatal(err)
	}
	md = ""
	a.refreshTree()
	a.refreshSearch()
	tt.Frame()
	runtime.GC()
	var m0 runtime.MemStats
	runtime.ReadMemStats(&m0)

	a.open("Million.md")
	tt.Frame()
	d := a.doc
	if d.loadErr != nil {
		t.Fatalf("failed to load million-char doc: %v", d.loadErr)
	}
	if d.dirty() || d.changed() {
		t.Fatal("newly opened million-char doc reported dirty")
	}

	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	if m1.HeapAlloc > m0.HeapAlloc && m1.HeapAlloc-m0.HeapAlloc > 50<<20 {
		t.Fatalf("app heap delta too high for 1M-char document: %.1f MB", float64(m1.HeapAlloc-m0.HeapAlloc)/(1024*1024))
	}

	focusDocument(a, tt)
	d.ed.SetSelection(0, 0)
	tt.Type("Pre ")
	if !d.dirty() || !d.changed() {
		t.Fatal("typing did not mark million-char document dirty")
	}
	if err := d.save(); err != nil {
		t.Fatal(err)
	}
	if d.dirty() || d.changed() {
		t.Fatal("saving did not clear dirty baseline on million-char document")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if !d.dirty() || !d.changed() {
		t.Fatal("undo after save did not mark million-char document dirty")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	if d.dirty() || d.changed() {
		t.Fatal("redo back to saved state did not restore clean baseline")
	}
	runtime.KeepAlive(tt)
}

func TestLarge200kDocumentEditingFooterDeferralAndAsyncSave(t *testing.T) {
	var b strings.Builder
	b.Grow(650_000)
	b.WriteString("# Large Document #toptag\n\n")
	for sec := 1; utf8.RuneCountInString(b.String()) < 210_000; sec++ {
		fmt.Fprintf(&b, "## Section %d 长文编辑测试\n\n", sec)
		for p := 0; p < 5; p++ {
			fmt.Fprintf(&b, "第 %d 节第 %d 段：验证 **加粗**、*斜体*、`code`、中文输入法与结构编辑在二十万字规模下的响应速度。\n\n", sec, p+1)
		}
	}
	md := b.String()

	a, tt := newTestApp(t)
	if err := vault.Write(root(a), "Huge200k.md", md); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.refreshSearch()
	posted := make(chan func(), 8)
	a.search.async = true
	a.runAsync = func(fn func()) { posted <- fn }
	a.open("Huge200k.md")
	// Drain the background refreshSearch callback kicked off by showIn when search.async is enabled.
	(<-posted)()
	focusDocument(a, tt)
	tt.Frame()

	d := a.doc
	if d.loadErr != nil {
		t.Fatalf("loadErr = %v", d.loadErr)
	}
	if d.dirty() || d.changed() {
		t.Fatal("newly opened 200k doc reported dirty")
	}
	if d.plainKnown {
		t.Fatal("clean indexed 200k doc unexpectedly flattened plainText on open")
	}

	// Active typing (ASCII + Chinese IME + structural Enter) must keep d.plainKnown == false
	// via footerSettle and maintain O(1) dirty tracking via skeletonDirty.
	pLeaf := d.ed.Doc.Leaves()[2]
	d.ed.SetSelection(pLeaf.End, pLeaf.End)
	tt.Type(" #newtag ")
	tt.Compose("zhongwen", 8)
	tt.Type("中文")
	if !d.dirty() || d.plainKnown {
		t.Fatalf("expected dirty=true and plainKnown=false during typing, got dirty=%v plainKnown=%v", d.dirty(), d.plainKnown)
	}
	tt.Key(0, ui.KeyEnter)
	if !d.dirty() || !d.skeletonDirty {
		t.Fatalf("expected skeletonDirty=true after Enter, got dirty=%v skeletonDirty=%v", d.dirty(), d.skeletonDirty)
	}
	tt.Type("after enter ")
	tt.Compose("shurufa", 7)
	tt.Type("输入法")
	if !d.dirty() || !d.skeletonDirty || d.plainKnown {
		t.Fatalf("post-Enter typing failed O(1) dirty/deferral invariants: dirty=%v skeletonDirty=%v plainKnown=%v", d.dirty(), d.skeletonDirty, d.plainKnown)
	}

	// Pausing for footerSettle refreshes footer tags on the 200k doc.
	tt.Advance(footerSettle)
	tt.Frame()
	tt.Frame()
	if !slices.Contains(d.footer.tags, "toptag") || !slices.Contains(d.footer.tags, "newtag") {
		t.Fatalf("expected footer tags to include toptag and newtag after settle, got %v", d.footer.tags)
	}

	// Trigger async autosave off-thread and apply completion on the UI thread.
	if len(posted) != 0 {
		t.Fatalf("unexpected pre-autosave async callbacks: %d", len(posted))
	}
	tt.Advance(autosaveDelay + 100*time.Millisecond)
	if !d.saving {
		t.Fatal("expected async autosave to start after autosaveDelay")
	}
	fn := <-posted
	fn()
	if d.saving || d.dirty() || d.changed() || d.skeletonDirty || !d.textDirtyKnown {
		dig, derr := d.ed.Committed().Digest()
		t.Fatalf("async autosave did not restore clean O(1) baseline: saving=%v dirty=%v changed=%v derr=%v digEq=%v saveErr=%v conflict=%v missing=%v",
			d.saving, d.dirty(), d.changed(), derr, dig == d.baseline, d.saveErr, d.conflict, d.missing)
	}
	runtime.KeepAlive(tt)
}

func TestColdOpenDefersBaselineHashingUntilFirstEditOrSave(t *testing.T) {
	doc := model.New()
	doc.Blocks = make([]*model.Block, 0, 1200)
	for i := 0; i < 1200; i++ {
		doc.Blocks = append(doc.Blocks, model.NewBlock("paragraph", "Cold open paragraph line for lazy baseline verification."))
	}
	raw, err := doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	sp := &space.Space{ID: "cold", Name: "Cold", Root: dir}
	if err := os.WriteFile(filepath.Join(dir, "long.arcmd"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	d := openDocument(sp, "long.arcmd", raw, false)
	if !d.baselinePending || d.dirty() || d.changed() {
		t.Fatalf("expected lazy pending clean baseline on open: pending=%v dirty=%v changed=%v", d.baselinePending, d.dirty(), d.changed())
	}
	if err := d.save(); err != nil {
		t.Fatalf("save on clean cold doc failed: %v", err)
	}
	if d.ed.Engine.OnBeforeChange == nil || d.ed.Engine.OnBeforeLeafChange == nil {
		t.Fatal("expected lazy baseline hooks registered while baseline is pending")
	}

	// Single-leaf text edits record only the edited leaf instead of hashing all 1,200 blocks.
	d.ed.SetSelection(0, 0)
	d.ed.ReplaceText(nil, "Edited ")
	if !d.baselinePending || len(d.baselineOrigRuns) != 1 || len(d.baselineRuns) != 1 {
		t.Fatalf("expected single-leaf baseline tracking without full materialization: pending=%v orig=%d runs=%d",
			d.baselinePending, len(d.baselineOrigRuns), len(d.baselineRuns))
	}
	if !d.dirty() || !d.baselinePending {
		t.Fatalf("expected doc dirty with baselinePending still true: dirty=%v pending=%v", d.dirty(), d.baselinePending)
	}
	d.ed.Undo()
	if d.dirty() || !d.baselinePending {
		t.Fatalf("expected undo on pending leaf edit to clear dirty while remaining lazy: dirty=%v pending=%v", d.dirty(), d.baselinePending)
	}
	d.ed.Redo()
	if !d.dirty() || !d.changed() {
		t.Fatalf("expected doc dirty after redo and changed() check: dirty=%v changed=%v", d.dirty(), d.changed())
	}
	if d.baselinePending || len(d.baselineOrigRuns) != 0 {
		t.Fatalf("expected changed() to materialize full baseline: pending=%v orig=%d", d.baselinePending, len(d.baselineOrigRuns))
	}
	d.ed.Undo()
	if d.dirty() || d.changed() {
		t.Fatalf("expected undo back to reconstructed pre-edit baseline to clear dirty")
	}

	// Verify structural edit after a leaf edit materializes the pre-leaf-edit baseline intact.
	d2 := openDocument(sp, "long.arcmd", raw, false)
	d2.ed.SetSelection(0, 0)
	d2.ed.ReplaceText(nil, "Leaf ")
	if !d2.baselinePending {
		t.Fatal("expected d2 baseline to remain lazy after leaf edit")
	}
	d2.ed.Engine.Insert(model.NewBlock("paragraph", "new"))
	if d2.baselinePending || !d2.dirty() || !d2.changed() {
		t.Fatalf("expected structural edit to materialize baseline: pending=%v dirty=%v changed=%v", d2.baselinePending, d2.dirty(), d2.changed())
	}
	d2.ed.Undo()
	d2.ed.Undo()
	if d2.dirty() || d2.changed() {
		t.Fatalf("expected two undos back to cold pre-edit state to clear dirty: dirty=%v changed=%v", d2.dirty(), d2.changed())
	}
}
