package editor

import (
	"slices"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/ui"
)

func TestSelectionBarClearFormatting(t *testing.T) {
	e := FromDocument(ImportHTML(`<p>Keep <span style="color:red;background-color:yellow"><b><i><u><a href="https://example.com">styled</a></u></i></b></span> end</p>`))
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme()})
	}, 900, 600)
	e.RequestFocus = true
	tt.Frame()

	start := ui.UTF16Len("Keep ")
	end := ui.UTF16Len("Keep styled")
	e.SetSelection(start, end)
	tt.Frame()

	if err := tt.Click("清除样式"); err != nil {
		t.Fatalf("click 清除样式 on selection bar: %v", err)
	}
	tt.Frame()

	if got := e.Doc.Blocks[0].Text(); got != "Keep styled end" {
		t.Fatalf("text altered: %q", got)
	}
	for _, r := range e.Doc.Blocks[0].Runs {
		if r.Style != (document.Style{}) {
			t.Fatalf("expected all styles cleared, got %+v", e.Doc.Blocks[0].Runs)
		}
	}

	// Undo restores the pasted formatting atomically.
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	restored := false
	for _, r := range e.Doc.Blocks[0].Runs {
		if r.Text == "styled" && r.Style.Color == "red" && r.Style.Bold && r.Style.Link == "https://example.com" {
			restored = true
		}
	}
	if !restored {
		t.Fatalf("undo did not restore styles: %+v", e.Doc.Blocks[0].Runs)
	}
}

func TestContextMenuClearFormattingAndReadOnly(t *testing.T) {
	e := FromDocument(ImportHTML(`<p><span style="color:blue">colored</span> plain</p>`))
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme()})
	}, 800, 500)
	e.RequestFocus = true
	tt.Frame()

	end := ui.UTF16Len("colored")
	e.SetSelection(0, end)
	e.Annotate("wavy", "red")
	e.OpenAnnotations()
	tt.Frame()

	rect, _, ok := e.BoundsForRange(ui.TextInputRange{Start: 0, End: 0})
	if !ok {
		t.Fatal("missing bounds for selection start")
	}
	tt.RightClickAt(rect.X+4, rect.Y+rect.H/2)
	if !slices.Contains(tt.Menu(), "清除样式") {
		t.Fatalf("context menu missing 清除样式: %v", tt.Menu())
	}
	if err := tt.ChooseMenuItem("清除样式"); err != nil {
		t.Fatalf("choose 清除样式: %v", err)
	}
	tt.Frame()

	if e.annotatePanel {
		t.Fatal("expected annotation panel to close on ClearFormatting")
	}
	for _, r := range e.Doc.Blocks[0].Runs {
		if r.Style != (document.Style{}) {
			t.Fatalf("expected cleared runs after context menu, got %+v", e.Doc.Blocks[0].Runs)
		}
	}

	// Read-only mode hides 清除样式 from the context menu and refuses the shortcut.
	e.SetSelection(0, end)
	e.Wrap("**")
	e.ReadOnly = true
	tt.Frame()
	tt.RightClickAt(rect.X+4, rect.Y+rect.H/2)
	if slices.Contains(tt.Menu(), "清除样式") {
		t.Fatalf("read-only menu should hide 清除样式: %v", tt.Menu())
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBackslash)
	tt.Frame()
	if !e.Doc.Blocks[0].Runs[0].Style.Bold {
		t.Fatal("read-only shortcut should not clear formatting")
	}
}

func TestClearFormattingShortcutCaretMultiBlockAndRebind(t *testing.T) {
	e := FromDocument(ImportHTML(`<h2><span style="color:red"><b>Title</b></span></h2><p><a href="https://example.com">link</a></p>`))
	var customBindings map[string]Combo
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), Bindings: customBindings})
	}, 800, 500)
	e.RequestFocus = true
	tt.Frame()

	// Collapsed caret inside bold red heading resets typing style on ⇧⌘\.
	e.SetSelection(ui.UTF16Len("Title"), ui.UTF16Len("Title"))
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBackslash)
	tt.Type(" plain")
	tt.Frame()
	runs := e.Doc.Blocks[0].Runs
	if len(runs) < 2 || runs[len(runs)-1].Text != " plain" || runs[len(runs)-1].Style != (document.Style{}) {
		t.Fatalf("typing after collapsed caret clear carried style: %+v", runs)
	}

	// Multi-block selection + ⇧⌘\ clears inline styles across both blocks while keeping heading level.
	e.SelectBlocks(e.Doc.Blocks[0].ID, e.Doc.Blocks[1].ID)
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBackslash)
	tt.Frame()
	if e.Doc.Blocks[0].Kind != "heading" || e.Doc.Blocks[0].Level != 2 {
		t.Fatalf("multi-block clear broke heading: %s L%d", e.Doc.Blocks[0].Kind, e.Doc.Blocks[0].Level)
	}
	for i, b := range e.Doc.Blocks {
		for _, r := range b.Runs {
			if r.Style != (document.Style{}) {
				t.Fatalf("block %d still styled: %+v", i, b.Runs)
			}
		}
	}
	if len(e.BlockSelection()) != 2 {
		t.Fatalf("expected multi-block selection to stay active, got %v", e.BlockSelection())
	}

	// Rebinding clear-format changes which shortcut activates it.
	e.clearBlocks()
	e.SetSelection(0, ui.UTF16Len("Title"))
	e.Wrap("**")
	customBindings = map[string]Combo{"clear-format": {ui.Cmd | ui.Alt, ui.KeyBackslash}}
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBackslash)
	if !e.Doc.Blocks[0].Runs[0].Style.Bold {
		t.Fatal("old shortcut should not trigger after rebind")
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyBackslash)
	if e.Doc.Blocks[0].Runs[0].Style.Bold {
		t.Fatal("rebound shortcut did not clear formatting")
	}
}
