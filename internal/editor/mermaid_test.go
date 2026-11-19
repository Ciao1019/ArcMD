package editor

import (
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/ui"
)

const flowMermaidMD = "```mermaid\nflowchart LR\n    A[Begin] --> B[End]\n```"
const seqMermaidMD = "```mermaid\nsequenceDiagram\n    Alice->>Bob: Ping\n    Bob-->>Alice: Pong\n```"

func TestMermaidReadingAndEditing(t *testing.T) {
	e, tt := newTester("Before\n\n" + flowMermaidMD + "\n\nAfter")
	r := e.rows[1]
	if !isMermaidBlock(r.leaf.Block) || r.mermaid == nil || r.mermaidErr != "" {
		t.Fatalf("mermaid block did not lay out: row=%+v", r)
	}
	if !r.mermaidHidden || r.mermaidPanel != 0 {
		t.Fatalf("expected collapsed mermaid diagram at rest, hidden=%v panel=%v", r.mermaidHidden, r.mermaidPanel)
	}

	bounds, _ := tt.Find("文档")
	tt.ClickAt(bounds.X+r.rect.X+r.rect.W/2, bounds.Y+r.rect.Y+r.mermaidTop+12)
	tt.Frame()

	r = e.rows[1]
	if r.mermaidHidden || r.mermaidPanel == 0 || r.y <= r.rect.Y+r.mermaidTop {
		t.Fatalf("click did not expand mermaid preview + source well: hidden=%v panel=%v r.y=%v", r.mermaidHidden, r.mermaidPanel, r.y)
	}

	e.SetSelection(r.leaf.End, r.leaf.End)
	tt.Frame()
	tt.Type("\n    B --> C[Done]")
	if !strings.Contains(e.mermaids[r.leaf.Block.ID].src, "C[Done]") {
		t.Fatalf("live preview did not re-parse updated source: %q", e.mermaids[r.leaf.Block.ID].src)
	}

	// Moving caret outside collapses the source well back to centered vector diagram.
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	if !e.rows[1].mermaidHidden || e.rows[1].mermaidPanel != 0 {
		t.Fatalf("expected mermaid to collapse after leaving block")
	}
}

func TestMermaidTabAndDoubleEnterExit(t *testing.T) {
	e, tt := newTester("Intro\n\n" + seqMermaidMD)
	focus(e, tt)
	r := e.rows[1]
	e.SetSelection(r.leaf.Start, r.leaf.Start)
	tt.Frame()
	if e.mermaidEditing != r.leaf.Block.ID {
		t.Fatalf("expected mermaidEditing=%q, got %q", r.leaf.Block.ID, e.mermaidEditing)
	}

	// Tab exits to a new paragraph below and collapses the sequence diagram.
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if e.mermaidEditing != "" || !e.rows[1].mermaidHidden {
		t.Fatalf("Tab did not exit & collapse mermaid block: editing=%q hidden=%v", e.mermaidEditing, e.rows[1].mermaidHidden)
	}
	if len(e.Doc.Blocks) != 3 || e.Doc.Blocks[2].Kind != "paragraph" {
		t.Fatalf("Tab did not create/focus paragraph below: blocks=%+v", e.Doc.Blocks)
	}

	// Re-enter at end and exit via double Enter (Enter to trailing blank line, Enter again).
	e.SetSelection(e.rows[1].leaf.End, e.rows[1].leaf.End)
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if e.mermaidEditing != "" || !e.rows[1].mermaidHidden {
		t.Fatalf("double Enter did not exit & collapse mermaid block: editing=%q hidden=%v", e.mermaidEditing, e.rows[1].mermaidHidden)
	}
}

func TestMermaidErrorStaysVisibleAndInsertHelpers(t *testing.T) {
	badMD := "```mermaid\nunknownDiagram\nfoo -> bar\n```"
	e, _ := newTester(badMD)
	r := e.rows[0]
	if r.mermaidErr == "" || r.mermaidHidden {
		t.Fatalf("malformed mermaid should remain visible with error: err=%q hidden=%v", r.mermaidErr, r.mermaidHidden)
	}

	e2, tt2 := newTester("")
	focus(e2, tt2)
	e2.InsertMermaidFlowchart()
	tt2.Frame()
	if !isMermaidBlock(e2.Doc.Blocks[0]) || e2.Doc.Blocks[0].Kind != "code" {
		t.Fatalf("InsertMermaidFlowchart did not convert empty block: %+v", e2.Doc.Blocks[0])
	}
	if !e2.rows[0].mermaidHidden && e2.rows[0].mermaid == nil {
		t.Fatalf("inserted flowchart did not parse: err=%q", e2.rows[0].mermaidErr)
	}

	e3, tt3 := newTester("First")
	focus(e3, tt3)
	e3.SetSelection(e3.Len(), e3.Len())
	e3.InsertMermaidSequence()
	tt3.Frame()
	if len(e3.Doc.Blocks) != 2 || !isMermaidBlock(e3.Doc.Blocks[1]) || e3.mermaidEditing != e3.Doc.Blocks[1].ID {
		t.Fatalf("InsertMermaidSequence did not insert and focus block: blocks=%+v editing=%q", e3.Doc.Blocks, e3.mermaidEditing)
	}
	for _, it := range turnItems(document.NewBlock("paragraph", "hi")) {
		if it.kind == "mermaid-flow" || it.kind == "mermaid-seq" {
			t.Fatalf("mermaid insert item %q leaked into turnItems", it.kind)
		}
	}
}

func TestTallSequenceDiagramRendersFullWidthAndZoomPreview(t *testing.T) {
	tallSeq := "```mermaid\nsequenceDiagram\n" +
		"  participant User as Client\n" +
		"  participant CoreSvc as CoreService\n" +
		"  participant Gateway as Gateway\n" +
		"  participant MQ as MessageQueue\n" +
		"  participant Worker as RemoteWorker\n" +
		"  rect rgb(200,250,200)\n" +
		"    note over User,CoreSvc: Authoring Phase\n" +
		"    User ->> CoreSvc: 1.1 Create item\n" +
		"    CoreSvc ->> CoreSvc: 1.2 Attach resources / configure parameters\n" +
		"    User ->> CoreSvc: 1.3 Save draft\n" +
		"    User ->> CoreSvc: 2.1 Submit for review\n" +
		"    alt Restricted pattern detected\n" +
		"      ScriptSvc ->> User: 2.2 Reject publication\n" +
		"    else Passed check\n" +
		"      CoreSvc ->> User: 2.3 Publish succeeded\n" +
		"    end\n" +
		"  end\n" +
		"  rect rgb(200,200,250)\n" +
		"    note over User,Worker: Testing Phase\n" +
		"    User ->> CoreSvc: 3.1 Trigger test run\n" +
		"    CoreSvc ->> Gateway: 3.2 RPC invoke\n" +
		"    Gateway ->> Worker: 3.3 HTTP request\n" +
		"    Worker -->> Worker: 3.4 Execute asynchronously\n" +
		"    Worker ->> Gateway: 3.5 RPC response\n" +
		"    Gateway ->> MQ: 3.6 Publish test result\n" +
		"    MQ -->> CoreSvc: 3.7 Consume message\n" +
		"    CoreSvc ->> CoreSvc: 3.8 Update test status\n" +
		"    CoreSvc ->> User: 3.9 View test result in monitor\n" +
		"  end\n" +
		"  rect rgb(134, 190, 220)\n" +
		"    note over User,CoreSvc: Release Phase\n" +
		"    User ->> CoreSvc: 4.1 Export bundle\n" +
		"    CoreSvc ->> User: 4.2 Download archive\n" +
		"    User ->> CoreSvc: 4.3 Import bundle to target environment\n" +
		"    CoreSvc ->> CoreSvc: 4.4 Parse, persist, and auto-publish\n" +
		"  end\n" +
		"  rect rgb(200,250,250)\n" +
		"    note over User,Worker: Production Execution Phase\n" +
		"    User ->> CoreSvc: 5.1 Submit execution job\n" +
		"    CoreSvc ->> Gateway: 5.2 RPC invoke\n" +
		"    Gateway ->> Worker: 5.3 HTTP request\n" +
		"    Worker ->> Worker: 5.4 Execute asynchronously\n" +
		"    Worker ->> Gateway: 5.5 RPC response\n" +
		"    Gateway ->> MQ: 5.6 Publish job result\n" +
		"    MQ -->> CoreSvc: 5.7 Consume message\n" +
		"    CoreSvc ->> CoreSvc: 5.8 Update job status\n" +
		"    CoreSvc ->> User: 5.9 View execution result in monitor\n" +
		"  end\n```"

	e, tt := newTester(tallSeq)
	r := e.rows[0]
	if r.mermaid == nil || r.mermaidErr != "" || !r.mermaidHidden {
		t.Fatalf("tall sequence failed to lay out cleanly at rest: err=%q hidden=%v", r.mermaidErr, r.mermaidHidden)
	}
	// Previously mermaidMaxH=520 shrank this diagram below 35% scale.
	if r.mermaidScale < 0.80 || r.rect.H < 1000 {
		t.Fatalf("tall sequence diagram was overly shrunk at rest: scale=%.3f rectH=%.1f diag=%vx%v", r.mermaidScale, r.rect.H, r.mermaid.W, r.mermaid.H)
	}

	// Hover the diagram and click its Zoom button to open the vector preview modal.
	bounds, _ := tt.Find("文档")
	var dec decoration
	for _, d := range e.decorations {
		if d.kind == "mermaid" {
			dec = d
			break
		}
	}
	zb := mermaidZoomButton(dec, false)
	zx, zy := bounds.X+zb.X+zb.W/2, bounds.Y+zb.Y+zb.H/2
	tt.Move(zx, zy)
	tt.Frame()
	tt.ClickAt(zx, zy)
	tt.Frame()
	if !e.mermaidViewer.open || !tt.HasText("时序图") {
		t.Fatalf("clicking zoom button did not open diagram preview: open=%v", e.mermaidViewer.open)
	}
	if err := tt.Click("放大图表"); err != nil {
		t.Fatal(err)
	}
	if e.mermaidViewer.zoom <= 1 {
		t.Fatalf("expected zoom > 1 after clicking zoom in, got %v", e.mermaidViewer.zoom)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if e.mermaidViewer.open {
		t.Fatal("Escape did not close diagram preview")
	}

	// Paginate for A4 paper still constrains height so a single diagram fits a page.
	ePrint := New(tallSeq)
	ePrint.Paginate(Options{Width: 794, Theme: ui.LightTheme()}, 1027)
	if ePrint.rows[0].mermaid.H*ePrint.rows[0].mermaidScale > 851 {
		t.Fatalf("printed diagram exceeded A4 height cap: %.1f", ePrint.rows[0].mermaid.H*ePrint.rows[0].mermaidScale)
	}
}
