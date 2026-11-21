package editor

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

func TestMultilinePasteCaretAndContinuedTyping(t *testing.T) {
	first := strings.Repeat("第一段文字会在页面中自动换行。", 4)
	last := strings.Repeat("最后一段也会换行，光标应该停在整段末尾。", 6) + "😀"
	for _, width := range []int{420, 900} {
		for _, format := range []string{"native", "html", "markdown", "plain", "crlf"} {
			t.Run(fmt.Sprintf("%s/width-%d", format, width), func(t *testing.T) {
				e, tt := newTester(first + "\n\n" + last)
				tt.SetSize(width, 650)
				focus(e, tt)
				tt.Key(ui.Cmd, ui.KeyA)
				if got := e.Selection().Range; got != (ui.TextInputRange{Start: 0, End: ui.UTF16Len(first)}) {
					t.Fatalf("first Cmd+A selected %+v; want first block %d", got, ui.UTF16Len(first))
				}
				tt.Key(ui.Cmd, ui.KeyA)
				selection := e.Selection()
				tt.Key(ui.Cmd, ui.KeyC)
				if e.Selection() != selection {
					t.Fatal("copy moved the selection")
				}
				tt.Key(0, ui.KeyBackspace)
				switch format {
				case "html":
					tt.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.HTML, []byte("<p><strong>"+first+"</strong></p><p>"+last+"</p>")))))
				case "markdown":
					tt.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.Markdown, []byte("**"+first+"**\n\n"+last)))))
				case "plain":
					tt.SetClipboard(first + "\n" + last)
				case "crlf":
					tt.SetClipboard(first + "\r\n" + last)
				}
				tt.Key(ui.Cmd, ui.KeyV)
				want := first + "\n" + last
				if e.Text() != want {
					t.Fatal("paste changed the text")
				}
				end := ui.UTF16Len(want)
				if e.Anchor != end || e.Caret != end {
					t.Fatalf("paste selection = %d,%d; want %d,%d", e.Anchor, e.Caret, end, end)
				}
				start := ui.UTF16Len(first) + 1
				firstLine, _, _ := e.BoundsForRange(ui.TextInputRange{Start: start, End: start})
				caret, _, ok := e.BoundsForRange(e.Selection().Range)
				if !ok || caret.Y <= firstLine.Y {
					t.Fatal("caret did not reach the final wrapped line")
				}
				tt.Key(ui.Cmd, ui.KeyZ)
				if e.Text() != "" {
					t.Fatal("paste was not undone in one step")
				}
				tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
				if e.Text() != want || e.Anchor != end || e.Caret != end {
					t.Fatal("redo did not restore the pasted caret")
				}
				tt.Type("继续写")
				if e.Text() != want+"继续写" {
					t.Fatal("continued typing did not append to the pasted text")
				}
			})
		}
	}
}

func TestTableCellAndLiteralPlainTextCopy(t *testing.T) {
	md := strings.Join([]string{
		"| 服务 | 账号 | 密码 |",
		"| --- | :---: | ---: |",
		`| 微信 | admin_01 | Abc_123*456[7]\8<9>\$0 |`,
		"| 数据库 | `db_user` | `P@ss_w0rd*99` |",
		"| 后台 | **ops_user** | <span style=\"color:#D83931\" data-color=\"red\">Red_Pass*1</span> |",
	}, "\n")
	e, tt := newTester(md)
	focus(e, tt)

	wantPasswords := map[int]string{
		1: "Abc_123*456[7]\\8<9>$0",
		2: "P@ss_w0rd*99",
		3: "Red_Pass*1",
	}
	table := e.Doc.Blocks[0]
	if table.Kind != "table" || len(table.Children) != 4 {
		t.Fatalf("unexpected table blocks: %+v", e.Doc.Blocks)
	}
	for rowIdx, want := range wantPasswords {
		cell := table.Children[rowIdx].Children[2]
		leaf, ok := e.Engine.LeafByID(cell.ID)
		if !ok {
			t.Fatalf("missing leaf for row %d", rowIdx)
		}
		e.SetSelection(leaf.Start, leaf.End)
		tt.Frame()
		tt.Key(ui.Cmd, ui.KeyC)
		if got := tt.Clipboard(); got != want {
			t.Fatalf("row %d plain clipboard = %q; want exact %q", rowIdx, got, want)
		}
		// Centered/right-aligned cell fragments must not leak <p align="..."> wrappers into Markdown.
		rawMD, err := tt.ClipboardData().Read(transfer.Markdown)
		if err != nil {
			t.Fatalf("row %d missing Markdown clipboard: %v", rowIdx, err)
		}
		if strings.Contains(string(rawMD), "<p ") || strings.Contains(string(rawMD), "</p>") {
			t.Fatalf("row %d Markdown clipboard leaked paragraph alignment HTML: %q", rowIdx, string(rawMD))
		}
	}

	// Partial multi-cell copy across a row uses tab-separated literal text without backslash escapes.
	c1, _ := e.Engine.LeafByID(table.Children[1].Children[1].ID)
	c2, _ := e.Engine.LeafByID(table.Children[1].Children[2].ID)
	e.SetSelection(c1.Start, c2.End)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyC)
	if got, want := tt.Clipboard(), "admin_01\tAbc_123*456[7]\\8<9>$0"; got != want {
		t.Fatalf("multi-cell plain clipboard = %q; want %q", got, want)
	}

	// Plain prose and inline code selections also copy literal text without Markdown escapes or backticks.
	e2, tt2 := newTester("- 密码：Pass_123*456[0]\n\n命令：`sk_live_123*abc`")
	focus(e2, tt2)
	listLeaf := e2.Doc.Leaves()[0]
	prefixLen := document.Len("密码：")
	e2.SetSelection(listLeaf.Start+prefixLen, listLeaf.End)
	tt2.Frame()
	tt2.Key(ui.Cmd, ui.KeyC)
	if got, want := tt2.Clipboard(), "Pass_123*456[0]"; got != want {
		t.Fatalf("list slice plain clipboard = %q; want %q", got, want)
	}

	codeLeaf := e2.Doc.Leaves()[1]
	codePrefix := document.Len("命令：")
	e2.SetSelection(codeLeaf.Start+codePrefix, codeLeaf.End)
	tt2.Frame()
	tt2.Key(ui.Cmd, ui.KeyC)
	if got, want := tt2.Clipboard(), "sk_live_123*abc"; got != want {
		t.Fatalf("inline code plain clipboard = %q; want %q", got, want)
	}

	// Pasting a password containing Markdown punctuation into a table cell preserves every character literally.
	emptyCellTable, tt3 := newTester("| 账号 | 密码 |\n| --- | --- |\n| root |  |")
	focus(emptyCellTable, tt3)
	targetCell, _ := emptyCellTable.Engine.LeafByID(emptyCellTable.Doc.Blocks[0].Children[1].Children[1].ID)
	emptyCellTable.SetSelection(targetCell.Start, targetCell.End)
	tt3.Frame()
	rawPass := "#Abc*123*xyz_456~789~"
	tt3.SetClipboardData(transfer.New(transfer.NewItem(
		transfer.Bytes(transfer.Text, []byte(rawPass)),
		transfer.Bytes(transfer.Markdown, []byte(`\#Abc\*123\*xyz_456\~789\~`)),
	)))
	tt3.Key(ui.Cmd, ui.KeyV)
	if got := emptyCellTable.Doc.Blocks[0].Children[1].Children[1].Text(); got != rawPass {
		t.Fatalf("paste into table cell = %q; want %q", got, rawPass)
	}

	// Intraword underscores stay unescaped in exported Markdown while boundary emphasis underscores still escape.
	roundDoc := document.New()
	roundDoc.Blocks = []*document.Block{document.NewBlock("paragraph", "Abc_123 and _literal_")}
	exported := ExportMarkdown(roundDoc)
	if got, want := exported, "Abc_123 and \\_literal\\_\n"; got != want {
		t.Fatalf("ExportMarkdown underscore escaping = %q; want %q", got, want)
	}
	reimported := ImportMarkdown(exported)
	if got := reimported.Blocks[0].Text(); got != "Abc_123 and _literal_" || len(reimported.Blocks[0].Runs) != 1 || reimported.Blocks[0].Runs[0].Style != (document.Style{}) {
		t.Fatalf("roundtrip altered plain text: %+v", reimported.Blocks[0].Runs)
	}
}

func TestBodyContextMenuClipboardAndSelection(t *testing.T) {
	e, tt := newTester("Hello world\n\nSecond line")
	focus(e, tt)

	// Select "world" and copy via right-click context menu.
	start := ui.UTF16Len("Hello ")
	end := ui.UTF16Len("Hello world")
	e.SetSelection(start, end)
	tt.Frame()
	rect, _, ok := e.BoundsForRange(ui.TextInputRange{Start: start, End: start})
	if !ok {
		t.Fatal("missing bounds for selection start")
	}
	tt.RightClickAt(rect.X+4, rect.Y+rect.H/2)
	menu := tt.Menu()
	for _, want := range []string{"剪切", "复制", "粘贴", "粘贴为纯文本", "全选", "加粗", "斜体", "插入表格"} {
		if !slices.Contains(menu, want) {
			t.Fatalf("context menu %v missing %q", menu, want)
		}
	}
	if err := tt.ChooseMenuItem("复制"); err != nil {
		t.Fatalf("choose 复制: %v", err)
	}
	if got := tt.Clipboard(); got != "world" {
		t.Fatalf("clipboard after context copy = %q; want %q", got, "world")
	}
	// Selection stays intact on right-click copy.
	if e.Anchor != start || e.Caret != end {
		t.Fatalf("selection after copy = %d,%d; want %d,%d", e.Anchor, e.Caret, start, end)
	}

	// Cut via right-click context menu removes the selected text.
	tt.RightClickAt(rect.X+4, rect.Y+rect.H/2)
	if err := tt.ChooseMenuItem("剪切"); err != nil {
		t.Fatalf("choose 剪切: %v", err)
	}
	if got := e.Doc.Blocks[0].Text(); got != "Hello " {
		t.Fatalf("first block after cut = %q; want %q", got, "Hello ")
	}
	if got := tt.Clipboard(); got != "world" {
		t.Fatalf("clipboard after cut = %q; want %q", got, "world")
	}

	// With no selection, Cut and Copy are disabled, while Paste works at the right-clicked location.
	tt.RightClickAt(rect.X+4, rect.Y+rect.H/2)
	if err := tt.ChooseMenuItem("复制"); err == nil {
		t.Fatal("expected 复制 to be disabled with empty selection")
	}
	tt.CloseMenu()

	tt.SetClipboardData(transfer.New(transfer.NewItem(
		transfer.Bytes(transfer.Text, []byte("**bold**")),
		transfer.Bytes(transfer.HTML, []byte("<p><strong>bold</strong></p>")),
	)))
	endFirst := ui.UTF16Len("Hello ")
	caretRect, _, ok := e.BoundsForRange(ui.TextInputRange{Start: endFirst, End: endFirst})
	if !ok {
		t.Fatal("missing bounds for end of first line")
	}
	tt.RightClickAt(caretRect.X+2, caretRect.Y+caretRect.H/2)
	if err := tt.ChooseMenuItem("粘贴"); err != nil {
		t.Fatalf("choose 粘贴: %v", err)
	}
	if got := e.Doc.Blocks[0].Text(); got != "Hello bold" {
		t.Fatalf("first block after rich paste = %q; want %q", got, "Hello bold")
	}
	if len(e.Doc.Blocks[0].Runs) < 2 || !e.Doc.Blocks[0].Runs[1].Style.Bold {
		t.Fatalf("expected bold run after rich paste, got %+v", e.Doc.Blocks[0].Runs)
	}

	// Paste as plain text inserts literal text without rich formatting.
	e.SetSelection(0, 0)
	tt.Frame()
	topRect, _, ok := e.BoundsForRange(ui.TextInputRange{Start: 0, End: 0})
	if !ok {
		t.Fatal("missing bounds for start of document")
	}
	tt.RightClickAt(topRect.X+2, topRect.Y+topRect.H/2)
	if err := tt.ChooseMenuItem("粘贴为纯文本"); err != nil {
		t.Fatalf("choose 粘贴为纯文本: %v", err)
	}
	if got := e.Doc.Blocks[0].Text(); got != "**bold**Hello bold" {
		t.Fatalf("first block after plain paste = %q; want %q", got, "**bold**Hello bold")
	}
	if e.Doc.Blocks[0].Runs[0].Style.Bold {
		t.Fatalf("plain paste should not apply bold style: %+v", e.Doc.Blocks[0].Runs)
	}

	// Select All via context menu selects the entire document in one step.
	tt.RightClickAt(topRect.X+2, topRect.Y+topRect.H/2)
	if err := tt.ChooseMenuItem("全选"); err != nil {
		t.Fatalf("choose 全选: %v", err)
	}
	if got := e.Selection().Range; got != (ui.TextInputRange{Start: 0, End: e.Len()}) {
		t.Fatalf("selection after context 全选 = %+v; want 0..%d", got, e.Len())
	}

	// Read-only mode exposes Copy and Select All, and hides mutating actions.
	e.ReadOnly = true
	tt.Frame()
	tt.RightClickAt(topRect.X+2, topRect.Y+topRect.H/2)
	roMenu := tt.Menu()
	if !slices.Contains(roMenu, "复制") || !slices.Contains(roMenu, "全选") {
		t.Fatalf("read-only menu %v missing 复制 or 全选", roMenu)
	}
	for _, hidden := range []string{"剪切", "粘贴", "粘贴为纯文本", "加粗", "斜体", "插入表格"} {
		if slices.Contains(roMenu, hidden) {
			t.Fatalf("read-only menu %v should not include %q", roMenu, hidden)
		}
	}
	if err := tt.ChooseMenuItem("复制"); err != nil {
		t.Fatalf("choose read-only 复制: %v", err)
	}
	if got := tt.Clipboard(); !strings.Contains(got, "Second line") {
		t.Fatalf("read-only copy clipboard = %q; want document content", got)
	}
}
