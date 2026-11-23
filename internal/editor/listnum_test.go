package editor

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/ui"
)

func findRowByBlockID(e *Editor, id string) (row, bool) {
	for _, r := range e.rows {
		if r.leaf.Block != nil && r.leaf.Block.ID == id {
			return r, true
		}
	}
	return row{}, false
}

func clickMarker(t *testing.T, e *Editor, tt *ui.Tester, blockID string) {
	t.Helper()
	bounds, ok := tt.Find("文档")
	if !ok {
		t.Fatal("document surface not found")
	}
	r, ok := findRowByBlockID(e, blockID)
	if !ok {
		t.Fatalf("row for block %q not found", blockID)
	}
	mr := e.numberedMarkerRect(r)
	if mr.W <= 0 || mr.H <= 0 {
		t.Fatalf("empty numberedMarkerRect for block %q: %+v", blockID, mr)
	}
	dx, dy := e.visualOffset(r)
	wx := bounds.X + mr.X + dx + mr.W/2
	wy := bounds.Y + mr.Y + dy + mr.H/2
	tt.Press(wx, wy)
	tt.Release(wx, wy)
	tt.Frame()
}

func TestListNumPopoverMarkerClickSetRestartContinueAndUndo(t *testing.T) {
	e, tt := newTester("1. Alpha\n2. Beta\n3. Gamma\n4. Delta\n\nGap\n\n1. SecondA\n2. SecondB")
	focus(e, tt)

	id0 := e.Doc.Blocks[0].ID
	id2 := e.Doc.Blocks[2].ID
	idGapNext := e.Doc.Blocks[5].ID

	// Click first numbered marker -> popover opens with "修改编号值" and "确定".
	clickMarker(t, e, tt, id0)
	if !e.listNumOpen || e.listNumID != id0 {
		t.Fatalf("popover did not open on marker click: open=%v id=%q want=%q", e.listNumOpen, e.listNumID, id0)
	}

	// Click into input to the left of "确定", enter invalid value "0" -> warning shown, button disabled.
	okBtn, ok := tt.Find("确定")
	if !ok {
		t.Fatal("confirm button not found in listNumPopover")
	}
	tt.Advance(600 * time.Millisecond)
	tt.Press(okBtn.X-40, okBtn.Y+okBtn.H/2)
	tt.Release(okBtn.X-40, okBtn.Y+okBtn.H/2)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("0")
	tt.Frame()
	if !tt.HasText("请输入 1–999999 的正整数") {
		t.Fatalf("expected validation error for start=0: open=%v text=%q okBtn=%+v", e.listNumOpen, e.listNumText, okBtn)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !e.listNumOpen || e.Doc.Blocks[0].Start != 0 {
		t.Fatalf("invalid input applied unexpectedly: open=%v start=%d", e.listNumOpen, e.Doc.Blocks[0].Start)
	}

	// Enter "5" and press Enter -> items 1..4 renumber to 5..8.
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("5")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if e.listNumOpen {
		t.Fatal("popover remained open after Enter submit")
	}
	if got := document.ListNumbers(e.Doc.Blocks[:4]); !reflect.DeepEqual(got, []int{5, 6, 7, 8}) {
		t.Fatalf("after setting start=5: %v", got)
	}
	for i, want := range []int{5, 6, 7, 8} {
		r, ok := findRowByBlockID(e, e.Doc.Blocks[i].ID)
		if !ok || r.number != want {
			t.Fatalf("row %d number = %d (found=%v), want %d", i, r.number, ok, want)
		}
	}

	// Cmd+Z undoes custom start back to 1..4.
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	if got := document.ListNumbers(e.Doc.Blocks[:4]); !reflect.DeepEqual(got, []int{1, 2, 3, 4}) {
		t.Fatalf("after Cmd+Z undo: %v", got)
	}

	// Click 3rd item ("3. Gamma") -> "开始新列表" restarts it at 1 (3rd=1, 4th=2).
	tt.Advance(600 * time.Millisecond)
	clickMarker(t, e, tt, id2)
	if !tt.HasText("开始新列表") {
		t.Fatal("expected '开始新列表' option on item > 1")
	}
	if !tt.HasText("继续之前的编号") {
		// Before restart, item 3 is adjacent without Start, so "继续之前的编号" should NOT show yet.
	} else {
		t.Fatal("unexpected '继续之前的编号' on normal adjacent item")
	}
	if err := tt.Click("开始新列表"); err != nil {
		t.Fatalf("click 开始新列表: %v", err)
	}
	tt.Frame()
	if got := document.ListNumbers(e.Doc.Blocks[:4]); !reflect.DeepEqual(got, []int{1, 2, 1, 2}) {
		t.Fatalf("after restart at item 3: %v", got)
	}

	// Re-open on restarted 3rd item -> "继续之前的编号" now appears and restores 1,2,3,4.
	tt.Advance(600 * time.Millisecond)
	clickMarker(t, e, tt, id2)
	if !tt.HasText("继续之前的编号") {
		t.Fatal("expected '继续之前的编号' on mid-list restarted item")
	}
	if err := tt.Click("继续之前的编号"); err != nil {
		t.Fatalf("click 继续之前的编号 on mid-list restart: %v", err)
	}
	tt.Frame()
	if e.Doc.Blocks[2].Start != 0 {
		t.Fatalf("Start = %d after restore, want 0", e.Doc.Blocks[2].Start)
	}
	if got := document.ListNumbers(e.Doc.Blocks[:4]); !reflect.DeepEqual(got, []int{1, 2, 3, 4}) {
		t.Fatalf("after restoring dynamic numbering: %v", got)
	}

	// Click 2nd list first item ("SecondA") after gap paragraph -> "继续之前的编号" sets 5, 6.
	tt.Advance(600 * time.Millisecond)
	clickMarker(t, e, tt, idGapNext)
	if !tt.HasText("继续之前的编号") {
		t.Fatal("expected '继续之前的编号' on separated second list")
	}
	if err := tt.Click("继续之前的编号"); err != nil {
		t.Fatalf("click 继续之前的编号 across gap: %v", err)
	}
	tt.Frame()
	if got := document.ListNumbers(e.Doc.Blocks); !reflect.DeepEqual(got, []int{1, 2, 3, 4, 0, 5, 6}) {
		t.Fatalf("after continuing across gap: %v", got)
	}
}

func TestListNumBlockMenuSubmenuAndReadOnly(t *testing.T) {
	e, tt := newTester("1. One\n2. Two\n\nBreak\n\n1. Three")
	focus(e, tt)

	secondListID := e.Doc.Blocks[3].ID
	e.OpenBlockMenu(secondListID, "")
	tt.Frame()
	if !tt.HasText("编号设置") {
		t.Fatal("expected '编号设置' row in block menu for numbered item")
	}
	rowRect, ok := tt.Find("编号设置")
	if !ok {
		t.Fatal("missing '编号设置' row")
	}
	tt.Move(rowRect.X+rowRect.W/2, rowRect.Y+rowRect.H/2)
	tt.Frame()
	if e.menuSub != "listnum" {
		t.Fatalf("menuSub = %q, want listnum", e.menuSub)
	}
	if !tt.HasText("继续之前的编号") || !tt.HasText("修改编号值…") {
		t.Fatal("missing listnum submenu items")
	}
	if err := tt.Click("修改编号值…"); err != nil {
		t.Fatalf("click 修改编号值…: %v", err)
	}
	tt.Frame()
	if !e.listNumOpen || e.listNumID != secondListID {
		t.Fatalf("clicking 修改编号值… did not open listNumPopover: open=%v id=%q", e.listNumOpen, e.listNumID)
	}
	// Focus was placed on input immediately.
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("12")
	tt.Frame()
	if err := tt.Click("确定"); err != nil {
		t.Fatalf("click 确定: %v", err)
	}
	tt.Frame()
	if e.Doc.Blocks[3].Start != 12 {
		t.Fatalf("Start = %d, want 12", e.Doc.Blocks[3].Start)
	}

	// ReadOnly blocks marker clicks and does not show popover.
	e.ReadOnly = true
	tt.Frame()
	tt.Advance(600 * time.Millisecond)
	clickMarker(t, e, tt, secondListID)
	if e.listNumOpen {
		t.Fatal("ReadOnly editor opened listNumPopover")
	}
}

func TestListNumMarkdownHTMLAndMindMapRoundTrip(t *testing.T) {
	// Markdown: custom start + adjacent split lists using "." vs ")" delimiters.
	doc := document.New()
	b0 := document.NewBlock("numbered", "First")
	b0.Start = 3
	b1 := document.NewBlock("numbered", "Second")
	doc.Blocks = []*document.Block{b0, b1}
	md := ExportMarkdown(doc)
	if !strings.Contains(md, "3. First\n4. Second") {
		t.Fatalf("unexpected custom start markdown:\n%s", md)
	}
	reimported := ImportMarkdown(md)
	if got := document.ListNumbers(reimported.Blocks); !reflect.DeepEqual(got, []int{3, 4}) {
		t.Fatalf("reimported numbers = %v, want [3 4]", got)
	}

	split := document.New()
	s0 := document.NewBlock("numbered", "A")
	s1 := document.NewBlock("numbered", "B")
	s2 := document.NewBlock("numbered", "C")
	s2.Start = 1
	s3 := document.NewBlock("numbered", "D")
	s4 := document.NewBlock("numbered", "E")
	s4.Start = 10
	s5 := document.NewBlock("numbered", "F")
	split.Blocks = []*document.Block{s0, s1, s2, s3, s4, s5}
	splitMD := ExportMarkdown(split)
	if !strings.Contains(splitMD, "1) C") || !strings.Contains(splitMD, "10. E") {
		t.Fatalf("unexpected split markdown:\n%s", splitMD)
	}
	reSplit := ImportMarkdown(splitMD)
	if got := document.ListNumbers(reSplit.Blocks); !reflect.DeepEqual(got, []int{1, 2, 1, 2, 10, 11}) {
		t.Fatalf("reimported split numbers = %v, want [1 2 1 2 10 11]", got)
	}

	// HTML round-trip: <ol start="5">, <li value="8">, adjacent <ol>.
	htmlIn := `<ol start="5"><li>Five</li><li value="8">Eight</li></ol><ol><li>Restart</li></ol>`
	htmlDoc := ImportHTML(htmlIn)
	if got := document.ListNumbers(htmlDoc.Blocks); !reflect.DeepEqual(got, []int{5, 8, 1}) {
		t.Fatalf("ImportHTML numbers = %v, want [5 8 1]", got)
	}
	htmlOut := ExportHTML(htmlDoc)
	if !strings.Contains(htmlOut, `<ol start="5">`) || !strings.Contains(htmlOut, `<ol start="8">`) || !strings.Contains(htmlOut, `</ol><ol>`) {
		t.Fatalf("unexpected ExportHTML output:\n%s", htmlOut)
	}

	// MindMap reflects Start updates via AdvanceListCounters.
	ed := New("# Root\n\n1. One\n2. Two\n3. Three")
	ed.Engine.SetListStart(ed.Doc.Blocks[1].ID, 7)
	ed.Engine.SetListStart(ed.Doc.Blocks[2].ID, 1)
	m := &MindMap{}
	opts := Options{Theme: ui.LightTheme()}
	m.sync(ed, opts)
	for _, it := range m.items {
		if body := m.bodies[it.ID]; body != nil {
			body.measure(opts)
		}
	}
	if m.bodies[ed.Doc.Blocks[1].ID].editor.rows[0].number != 7 {
		t.Fatalf("mindmap item 1 number = %d, want 7", m.bodies[ed.Doc.Blocks[1].ID].editor.rows[0].number)
	}
	if m.bodies[ed.Doc.Blocks[2].ID].editor.rows[0].number != 1 {
		t.Fatalf("mindmap item 2 number = %d, want 1", m.bodies[ed.Doc.Blocks[2].ID].editor.rows[0].number)
	}
}
