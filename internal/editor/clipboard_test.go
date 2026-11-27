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
	for _, want := range []string{"剪切", "复制", "粘贴", "粘贴为纯文本", "全选", "加粗", "斜体", "清除样式", "插入表格"} {
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
	for _, hidden := range []string{"剪切", "粘贴", "粘贴为纯文本", "加粗", "斜体", "清除样式", "插入表格"} {
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

func TestSpreadsheetPasteOverPNGAndImagePastePreserved(t *testing.T) {
	fakePNG := []byte("\x89PNG\r\n\x1a\nfake")
	newPasteTester := func(markdown string, onImage func([]byte)) (*Editor, *ui.Tester) {
		e := New(markdown)
		tt := ui.NewTester(func(c *ui.Context) {
			w, _ := c.Size()
			View(c, e, Options{Width: w, Theme: c.Theme(), PasteImage: onImage})
		}, 900, 650)
		tt.Frame()
		focus(e, tt)
		return e, tt
	}

	t.Run("macOS Excel HTML with CSS classes beats PNG snapshot", func(t *testing.T) {
		pastedImages := 0
		e, tt := newPasteTester("", func([]byte) { pastedImages++ })

		excelHTML := `<html xmlns:o="urn:schemas-microsoft-com:office:office" xmlns:x="urn:schemas-microsoft-com:office:excel">
<head>
<meta http-equiv=Content-Type content="text/html; charset=utf-8">
<style>
<!--table {mso-displayed-decimal-separator:"\.";}
@page {margin:.75in;}
td {color:black;font-weight:400;font-style:normal;text-decoration:none;text-align:general;}
.xl65 {font-weight:700;text-align:center;background:#D9E1F2 none;mso-pattern:black none;}
.xl66 {color:#C00000;font-weight:700;font-style:italic;text-decoration:underline;}
.font0 {color:black;font-weight:400;font-style:normal;text-decoration:none;}
.font5 {color:#0070C0;font-weight:700;}
-->
</style>
</head>
<body>
<table border=0 cellpadding=0 cellspacing=0 width=261>
 <col width=87><col width=87><col width=87>
 <tr>
  <td class=xl65 colspan=2>项目概览</td>
  <td class=xl65>进度</td>
 </tr>
 <tr>
  <td>ArcMD</td>
  <td class=xl66>紧急 <font class=font0>普通</font></td>
  <td align=right>100%</td>
 </tr>
 <tr>
  <td><font color="#245BDB">蓝字</font></td>
  <td>第一行<br>  第二行</td>
  <td align=right>50%</td>
 </tr>
</table>
</body>
</html>`
		tt.SetClipboardData(transfer.New(transfer.NewItem(
			transfer.Bytes(transfer.PNG, fakePNG),
			transfer.Bytes(transfer.HTML, []byte(excelHTML)),
			transfer.Bytes(transfer.Text, []byte("项目概览\t\t进度\nArcMD\t紧急 普通\t100%\n蓝字\t\"第一行\n第二行\"\t50%\n")),
		)))
		tt.Key(ui.Cmd, ui.KeyV)

		if pastedImages != 0 {
			t.Fatalf("expected table paste, not image (pastedImages=%d)", pastedImages)
		}
		if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "table" {
			t.Fatalf("expected single table block, got %+v", e.Doc.Blocks)
		}
		tbl := e.Doc.Blocks[0]
		if len(tbl.Widths) != 0 {
			t.Fatalf("Excel grid <col width> should not fix table widths: %v", tbl.Widths)
		}
		if len(tbl.Children) != 3 || len(tbl.Children[0].Children) != 3 {
			t.Fatalf("table shape = %dx%d; want 3x3", len(tbl.Children), len(tbl.Children[0].Children))
		}
		h0 := tbl.Children[0].Children[0]
		if h0.Text() != "项目概览" || h0.ColSpan != 2 || h0.Align != "center" || h0.Background != "light-blue" || !h0.Runs[0].Style.Bold {
			t.Fatalf("header cell = %+v (runs=%+v)", h0, h0.Runs)
		}
		c11 := tbl.Children[1].Children[1]
		if c11.Text() != "紧急 普通" || len(c11.Runs) != 2 {
			t.Fatalf("mixed styled cell runs = %+v", c11.Runs)
		}
		if !c11.Runs[0].Style.Bold || !c11.Runs[0].Style.Italic || !c11.Runs[0].Style.Underline || c11.Runs[0].Style.Color != "red" {
			t.Fatalf("expected xl66 bold/italic/underline/red, got %+v", c11.Runs[0].Style)
		}
		if c11.Runs[1].Style.Bold || c11.Runs[1].Style.Italic || c11.Runs[1].Style.Underline || c11.Runs[1].Style.Color != "" {
			t.Fatalf("expected font0 to reset inherited cell style, got %+v", c11.Runs[1].Style)
		}
		if got := tbl.Children[2].Children[0].Runs[0].Style.Color; got != "blue" {
			t.Fatalf("font color attribute = %q; want blue", got)
		}
		if got := tbl.Children[2].Children[1].Text(); got != "第一行\n第二行" {
			t.Fatalf("multiline cell = %q; want %q", got, "第一行\n第二行")
		}
		if err := e.Doc.Validate(); err != nil {
			t.Fatalf("invalid pasted table: %v", err)
		}
	})

	t.Run("plain Excel table with numeric column exports clean Markdown pipe table", func(t *testing.T) {
		pastedImages := 0
		e, tt := newPasteTester("", func([]byte) { pastedImages++ })

		plainExcel := `<html><body><table>
<col width=72><col width=72><col width=72>
<tr><td>产品</td><td>数量</td><td>单价</td></tr>
<tr><td>苹果</td><td align=right>10</td><td align=right>5.5</td></tr>
<tr><td>香蕉</td><td></td><td align=right>3.2</td></tr>
</table></body></html>`
		tt.SetClipboardData(transfer.New(transfer.NewItem(
			transfer.Bytes(transfer.PNG, fakePNG),
			transfer.Bytes(transfer.HTML, []byte(plainExcel)),
			transfer.Bytes(transfer.Text, []byte("产品\t数量\t单价\n苹果\t10\t5.5\n香蕉\t\t3.2\n")),
		)))
		tt.Key(ui.Cmd, ui.KeyV)

		if pastedImages != 0 {
			t.Fatalf("expected table paste, got image")
		}
		md := ExportMarkdown(e.Doc)
		want := "| 产品 | 数量 | 单价 |\n| --- | ---: | ---: |\n| 苹果 | 10 | 5.5 |\n| 香蕉 |  | 3.2 |\n"
		if md != want {
			t.Fatalf("ExportMarkdown =\n%s\nwant:\n%s", md, want)
		}
	})

	t.Run("Windows Excel bare CF_HTML tr fragment beats PNG", func(t *testing.T) {
		pastedImages := 0
		e, tt := newPasteTester("", func([]byte) { pastedImages++ })

		// mygo's Windows CF_HTML reader slices StartFragment..EndFragment,
		// which for Excel starts directly at <tr ...> without an outer <table>.
		winFrag := `<tr height=19><td height=19 width=72>姓名</td><td width=72>分数</td></tr>
<tr height=19><td height=19>张三</td><td align=right>98</td></tr>`
		tt.SetClipboardData(transfer.New(transfer.NewItem(
			transfer.Bytes(transfer.PNG, fakePNG),
			transfer.Bytes(transfer.HTML, []byte(winFrag)),
			transfer.Bytes(transfer.Text, []byte("姓名\t分数\r\n张三\t98\r\n")),
		)))
		tt.Key(ui.Cmd, ui.KeyV)

		if pastedImages != 0 {
			t.Fatalf("expected table paste, got image")
		}
		if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "table" {
			t.Fatalf("expected table block from Windows CF_HTML fragment, got %+v", e.Doc.Blocks)
		}
		tbl := e.Doc.Blocks[0]
		if tbl.Children[0].Children[0].Text() != "姓名" || tbl.Children[1].Children[1].Text() != "98" {
			t.Fatalf("unexpected Windows Excel table contents: %+v", tbl.Children)
		}
	})

	t.Run("Apple Numbers TSV with PNG and no HTML pastes as table", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			tsv      string
			wantRows int
			wantCols int
		}{
			{"multi-row", "名称\t数量\n苹果\t3\n香蕉\t12\n", 3, 2},
			{"single-row", "名称\t数量\t备注\n", 1, 3},
		} {
			t.Run(tc.name, func(t *testing.T) {
				pastedImages := 0
				e, tt := newPasteTester("", func([]byte) { pastedImages++ })
				tt.SetClipboardData(transfer.New(transfer.NewItem(
					transfer.Bytes(transfer.PNG, fakePNG),
					transfer.Bytes(transfer.Text, []byte(tc.tsv)),
				)))
				tt.Key(ui.Cmd, ui.KeyV)
				if pastedImages != 0 {
					t.Fatalf("expected table paste, got image")
				}
				if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "table" {
					t.Fatalf("expected table block, got %+v", e.Doc.Blocks)
				}
				tbl := e.Doc.Blocks[0]
				if len(tbl.Children) != tc.wantRows || len(tbl.Children[0].Children) != tc.wantCols {
					t.Fatalf("shape = %dx%d; want %dx%d", len(tbl.Children), len(tbl.Children[0].Children), tc.wantRows, tc.wantCols)
				}
			})
		}
	})

	t.Run("Google Sheets inline td styles preserved without background leak", func(t *testing.T) {
		e, tt := newTester("")
		focus(e, tt)
		gsHTML := `<google-sheets-html-origin><table><colgroup><col width="100"/><col width="100"/></colgroup><tbody>
<tr><td style="font-weight:bold;color:#d83931;background-color:#cfe2f3;text-align:center;">Name</td><td style="font-weight:bold;">Score</td></tr>
<tr><td>Alice</td><td style="text-align:right;">95</td></tr>
</tbody></table></google-sheets-html-origin>`
		tt.SetClipboardData(transfer.New(transfer.NewItem(
			transfer.Bytes(transfer.HTML, []byte(gsHTML)),
			transfer.Bytes(transfer.Text, []byte("Name\tScore\nAlice\t95")),
		)))
		tt.Key(ui.Cmd, ui.KeyV)
		tbl := e.Doc.Blocks[0]
		if tbl.Kind != "table" || len(tbl.Widths) != 0 {
			t.Fatalf("unexpected Google Sheets table: kind=%s widths=%v", tbl.Kind, tbl.Widths)
		}
		c00 := tbl.Children[0].Children[0]
		if c00.Background != "light-blue" || c00.Align != "center" || !c00.Runs[0].Style.Bold || c00.Runs[0].Style.Color != "red" || c00.Runs[0].Style.Highlight != "" {
			t.Fatalf("c00 = %+v runs=%+v", c00, c00.Runs)
		}
	})

	t.Run("Excel single cell unwraps to inline text without PNG or stray newline", func(t *testing.T) {
		pastedImages := 0
		e, tt := newPasteTester("销售额： 元", func([]byte) { pastedImages++ })
		pos := ui.UTF16Len("销售额：")
		e.SetSelection(pos, pos)
		tt.Frame()
		tt.SetClipboardData(transfer.New(transfer.NewItem(
			transfer.Bytes(transfer.PNG, fakePNG),
			transfer.Bytes(transfer.HTML, []byte(`<table><tr><td align=right style="font-weight:bold">12,345</td></tr></table>`)),
			transfer.Bytes(transfer.Text, []byte("12,345\n")),
		)))
		tt.Key(ui.Cmd, ui.KeyV)
		if pastedImages != 0 {
			t.Fatalf("single cell should not paste as image")
		}
		if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "paragraph" || e.Doc.Blocks[0].Text() != "销售额：12,345 元" {
			t.Fatalf("single cell paste = %+v", e.Doc.Blocks)
		}
		if e.Doc.Blocks[0].Align != "" {
			t.Fatalf("single cell align leaked into paragraph: %q", e.Doc.Blocks[0].Align)
		}
	})

	t.Run("Excel table pasted in code block inserts plain TSV, not PNG", func(t *testing.T) {
		pastedImages := 0
		e, tt := newPasteTester("```\n```", func([]byte) { pastedImages++ })
		tt.SetClipboardData(transfer.New(transfer.NewItem(
			transfer.Bytes(transfer.PNG, fakePNG),
			transfer.Bytes(transfer.HTML, []byte("<table><tr><td>A</td><td>B</td></tr><tr><td>1</td><td>2</td></tr></table>")),
			transfer.Bytes(transfer.Text, []byte("A\tB\n1\t2")),
		)))
		tt.Key(ui.Cmd, ui.KeyV)
		if pastedImages != 0 {
			t.Fatalf("code block paste should not paste PNG")
		}
		if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "code" || e.Doc.Blocks[0].Text() != "A\tB\n1\t2" {
			t.Fatalf("code block paste = %+v", e.Doc.Blocks)
		}
	})

	t.Run("browser Copy Image, chat image, Figma, and Excel chart still paste as PNG", func(t *testing.T) {
		cases := []struct {
			name string
			html string
			text string
		}{
			{"pure screenshot", "", ""},
			{"screenshot with filename text", "", "Screenshot 2026-11-27.png"},
			{"Chrome Copy Image", `<meta charset='utf-8'><img src="https://example.com/photo.png" alt="Sunset"/>`, "https://example.com/photo.png"},
			{"Safari linked image", `<html><body><a href="https://example.com"><img src="https://example.com/photo.png" alt="Sunset"></a></body></html>`, ""},
			{"WeChat Copy Image", `<img src="file:///Users/test/pic.png">`, ""},
			{"Figma metadata HTML", `<meta charset='utf-8'><span data-metadata="<!--(figmeta)abc-->"></span><span data-buffer="<!--(figma)xyz-->"></span>`, ""},
			{"Excel Chart", `<html><body><img src="file:///tmp/image001.png"></body></html>`, ""},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				pastedImages := 0
				e, tt := newPasteTester("", func([]byte) { pastedImages++ })
				reps := []transfer.Representation{transfer.Bytes(transfer.PNG, fakePNG)}
				if tc.html != "" {
					reps = append(reps, transfer.Bytes(transfer.HTML, []byte(tc.html)))
				}
				if tc.text != "" {
					reps = append(reps, transfer.Bytes(transfer.Text, []byte(tc.text)))
				}
				tt.SetClipboardData(transfer.New(transfer.NewItem(reps...)))
				tt.Key(ui.Cmd, ui.KeyV)
				if pastedImages != 1 {
					t.Fatalf("%s: pastedImages = %d; want 1 (blocks=%+v)", tc.name, pastedImages, e.Doc.Blocks)
				}
			})
		}
	})

	t.Run("PasteAsTable reads HTML tables including single-row and merged cells", func(t *testing.T) {
		e, tt := newTester("")
		focus(e, tt)
		tt.SetClipboardData(transfer.New(transfer.NewItem(
			transfer.Bytes(transfer.PNG, fakePNG),
			transfer.Bytes(transfer.HTML, []byte(`<tr><td colspan="2">Merged</td><td>End</td></tr>`)),
			transfer.Bytes(transfer.Text, []byte("Merged\t\tEnd")),
		)))
		if !e.PasteAsTable() {
			t.Fatal("PasteAsTable returned false for HTML table")
		}
		if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "table" || e.Doc.Blocks[0].Children[0].Children[0].ColSpan != 2 {
			t.Fatalf("PasteAsTable blocks = %+v", e.Doc.Blocks)
		}
	})
}
