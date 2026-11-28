package editor

import (
	"slices"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

func TestCopyPlainTextListsMixedBlocksTableAndColumns(t *testing.T) {
	md := strings.Join([]string{
		"## 标题 **加粗**",
		"",
		"段落含 [链接](https://example.com) 与 `code`。",
		"",
		"- 列表项一",
		"- **列表项二**",
		"- [列表链接](https://example.com/list)",
		"",
		"1. 有序一",
		"2. 有序二",
		"",
		"- [ ] 待办一",
		"- [x] 已完成二",
		"",
		"> 引用文本",
		"",
		"```go",
		"fmt.Println(1)",
		"```",
		"",
		"---",
		"",
		"| 名称 | 说明 |",
		"| --- | --- |",
		"| **ArcMD** | `纯 Go` |",
		"",
		"结尾段落",
	}, "\n")
	e, tt := newTester(md)
	focus(e, tt)

	// 1. Selecting a single full bullet item with Cmd+Shift+C strips "- " and inline styles.
	leaves := e.Doc.Leaves()
	bulletLeaf := leaves[2]
	if bulletLeaf.Block.Kind != "bullet" {
		t.Fatalf("expected bullet leaf at index 2, got %s", bulletLeaf.Block.Kind)
	}
	e.SetSelection(bulletLeaf.Start, bulletLeaf.End)
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyC)
	if got, want := tt.Clipboard(), "列表项一"; got != want {
		t.Fatalf("single bullet plain copy = %q; want %q", got, want)
	}
	// Explicit Copy as Plain Text must NOT write rich formats.
	data := tt.ClipboardData()
	if b, err := data.Read(fragmentFormat); err == nil && len(b) > 0 {
		t.Fatalf("copy-plain unexpectedly wrote ArcMD fragment format")
	}
	if b, err := data.Read(transfer.Markdown); err == nil && len(b) > 0 {
		t.Fatalf("copy-plain unexpectedly wrote Markdown format: %q", string(b))
	}
	if b, err := data.Read(transfer.HTML); err == nil && len(b) > 0 {
		t.Fatalf("copy-plain unexpectedly wrote HTML format: %q", string(b))
	}

	// 2. Selecting all 3 bullet items joins consecutive list items with single newline.
	e.SetSelection(leaves[2].Start, leaves[4].End)
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyC)
	if got, want := tt.Clipboard(), "列表项一\n列表项二\n列表链接"; got != want {
		t.Fatalf("multi-bullet plain copy = %q; want %q", got, want)
	}

	// 3. Select all and copy as plain text: lists use \n, non-list blocks use \n\n, rule skipped, table uses \t+\n.
	e.SetSelection(0, e.Len())
	tt.Frame()
	scrollBefore := e.Scroll.Y
	tt.Key(ui.Cmd|ui.Shift, ui.KeyC)
	wantAll := strings.Join([]string{
		"标题 加粗",
		"",
		"段落含 链接 与 code。",
		"",
		"列表项一",
		"列表项二",
		"列表链接",
		"有序一",
		"有序二",
		"待办一",
		"已完成二",
		"",
		"引用文本",
		"",
		"fmt.Println(1)",
		"",
		"名称\t说明",
		"ArcMD\t纯 Go",
		"",
		"结尾段落",
	}, "\n")
	if got := tt.Clipboard(); got != wantAll {
		t.Fatalf("full doc plain copy =\n%q\nwant:\n%q", got, wantAll)
	}
	if e.Scroll.Y != scrollBefore || e.Anchor != 0 || e.Caret != e.Len() {
		t.Fatalf("copy-plain altered scroll or selection")
	}

	// 4. Columns flatten child blocks cleanly without HTML wrappers.
	colDoc := document.New()
	cols := document.Columns()
	cols.Children[0].Children = []*document.Block{
		document.NewBlock("heading", "左栏"),
		document.NewBlock("bullet", "条目 A"),
		document.NewBlock("bullet", "条目 B"),
	}
	cols.Children[1].Children = []*document.Block{
		document.NewBlock("paragraph", "右栏正文"),
	}
	colDoc.Blocks = []*document.Block{cols}
	if got, want := exportFragmentPlainText(colDoc), "左栏\n\n条目 A\n条目 B\n\n右栏正文"; got != want {
		t.Fatalf("columns plain text = %q; want %q", got, want)
	}
}

func TestCopyPlainTextContextMenuBlockMenusReadOnlyAndRebind(t *testing.T) {
	e := New("- **第一项**\n- 第二项\n\n普通段落")
	customBindings := map[string]Combo(nil)
	tt := ui.NewTester(func(c *ui.Context) {
		View(c, e, Options{Width: 680, Theme: c.Theme(), Bindings: customBindings})
	}, 720, 480)
	focus(e, tt)

	// Empty selection disables 复制为纯文本 in right-click context menu.
	e.SetSelection(0, 0)
	tt.Frame()
	rect, _, ok := e.BoundsForRange(ui.TextInputRange{Start: 0, End: 0})
	if !ok {
		t.Fatal("missing bounds for selection start")
	}
	tt.RightClickAt(rect.X+4, rect.Y+rect.H/2)
	if !slices.Contains(tt.Menu(), "复制为纯文本") {
		t.Fatalf("context menu %v missing 复制为纯文本", tt.Menu())
	}
	if err := tt.ChooseMenuItem("复制为纯文本"); err == nil {
		t.Fatal("expected 复制为纯文本 to be disabled with empty selection")
	}
	tt.CloseMenu()

	// Selecting the two list items and choosing 复制为纯文本 strips "- " and "**".
	leaves := e.Doc.Leaves()
	e.SetSelection(leaves[0].Start, leaves[1].End)
	tt.Frame()
	tt.RightClickAt(rect.X+4, rect.Y+rect.H/2)
	if err := tt.ChooseMenuItem("复制为纯文本"); err != nil {
		t.Fatalf("choose 复制为纯文本: %v", err)
	}
	if got, want := tt.Clipboard(), "第一项\n第二项"; got != want {
		t.Fatalf("context menu plain copy = %q; want %q", got, want)
	}

	// Read-only mode keeps 复制为纯文本 available via both menu and shortcut.
	e.SetReadOnly(true)
	tt.SetClipboard("stale")
	tt.Frame()
	tt.RightClickAt(rect.X+4, rect.Y+rect.H/2)
	if !slices.Contains(tt.Menu(), "复制为纯文本") {
		t.Fatalf("read-only menu %v missing 复制为纯文本", tt.Menu())
	}
	if err := tt.ChooseMenuItem("复制为纯文本"); err != nil {
		t.Fatalf("read-only choose 复制为纯文本: %v", err)
	}
	if got, want := tt.Clipboard(), "第一项\n第二项"; got != want {
		t.Fatalf("read-only menu plain copy = %q; want %q", got, want)
	}

	tt.SetClipboard("stale2")
	tt.Key(ui.Cmd|ui.Shift, ui.KeyC)
	if got, want := tt.Clipboard(), "第一项\n第二项"; got != want {
		t.Fatalf("read-only shortcut plain copy = %q; want %q", got, want)
	}

	// Multi-block selection supports both shortcut and right-click context menu 复制为纯文本.
	e.SetReadOnly(false)
	e.SelectBlocks(e.Doc.Blocks[0].ID, e.Doc.Blocks[1].ID)
	tt.SetClipboard("stale4")
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyC)
	if got, want := tt.Clipboard(), "第一项\n第二项"; got != want {
		t.Fatalf("multi-block shortcut plain copy = %q; want %q", got, want)
	}
	tt.SetClipboard("stale5")
	tt.RightClickAt(rect.X+4, rect.Y+rect.H/2)
	if err := tt.ChooseMenuItem("复制为纯文本"); err != nil {
		t.Fatalf("multi-block context menu 复制为纯文本: %v", err)
	}
	if got, want := tt.Clipboard(), "第一项\n第二项"; got != want {
		t.Fatalf("multi-block context menu plain copy = %q; want %q", got, want)
	}

	// Rebinding copy-plain shifts which shortcut triggers it.
	e.clearBlocks()
	e.SetSelection(leaves[0].Start, leaves[1].End)
	customBindings = map[string]Combo{"copy-plain": {ui.Cmd | ui.Alt, ui.KeyC}}
	tt.SetClipboard("before-rebind")
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyC)
	if got := tt.Clipboard(); got != "before-rebind" {
		t.Fatalf("old shortcut still triggered copy-plain after rebind: %q", got)
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyC)
	if got, want := tt.Clipboard(), "第一项\n第二项"; got != want {
		t.Fatalf("rebound shortcut plain copy = %q; want %q", got, want)
	}
}

func TestCopyPlainTextDefaultOptionPreservesInAppRichPaste(t *testing.T) {
	e := New("- **alpha**\n- beta")
	copyPlainDefault := true
	tt := ui.NewTester(func(c *ui.Context) {
		View(c, e, Options{Width: 680, Theme: c.Theme(), CopyPlainText: copyPlainDefault})
	}, 720, 480)
	focus(e, tt)

	leaves := e.Doc.Leaves()
	e.SetSelection(leaves[0].Start, leaves[1].End)
	tt.Frame()

	// Standard Cmd+C puts stripped plain text on transfer.Text and retains ArcMD's
	// private fragmentFormat for in-app paste, while omitting external HTML/Markdown
	// formats so rich external apps (Word, Notes, Typora, Feishu) also paste plain text.
	tt.Key(ui.Cmd, ui.KeyC)
	if got, want := tt.Clipboard(), "alpha\nbeta"; got != want {
		t.Fatalf("Cmd+C with CopyPlainText=true gave transfer.Text = %q; want %q", got, want)
	}
	data := tt.ClipboardData()
	if frag, err := data.Read(fragmentFormat); err != nil || len(frag) == 0 {
		t.Fatalf("expected ArcMD fragment format retained for in-app paste: err=%v", err)
	}
	if md, err := data.Read(transfer.Markdown); err == nil && len(md) > 0 {
		t.Fatalf("expected transfer.Markdown omitted when CopyPlainText=true, got %q", string(md))
	}
	if htmlBytes, err := data.Read(transfer.HTML); err == nil && len(htmlBytes) > 0 {
		t.Fatalf("expected transfer.HTML omitted when CopyPlainText=true, got %q", string(htmlBytes))
	}

	// Pasting into another ArcMD editor restores the rich bullet list & bold formatting.
	dst, dstTT := newTester("")
	focus(dst, dstTT)
	dstTT.SetClipboardData(data)
	dstTT.Key(ui.Cmd, ui.KeyV)
	if len(dst.Doc.Blocks) != 2 || dst.Doc.Blocks[0].Kind != "bullet" || !dst.Doc.Blocks[0].Runs[0].Style.Bold {
		t.Fatalf("in-app paste lost formatting: %+v", dst.Doc.Blocks)
	}

	// Explicit Cmd+Shift+C writes ONLY plain text so even pasting into ArcMD yields plain paragraphs, not bullets.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyC)
	dst2, dst2TT := newTester("")
	focus(dst2, dst2TT)
	dst2TT.SetClipboardData(tt.ClipboardData())
	dst2TT.Key(ui.Cmd, ui.KeyV)
	for _, b := range dst2.Doc.Blocks {
		if b.Kind != "paragraph" {
			t.Fatalf("pasting explicit copy-plain created non-paragraph block %q", b.Kind)
		}
	}

	// Multi-block Cmd+C also honors CopyPlainText=true.
	e.SelectBlocks(e.Doc.Blocks[0].ID, e.Doc.Blocks[1].ID)
	tt.SetClipboard("stale")
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyC)
	if got, want := tt.Clipboard(), "alpha\nbeta"; got != want {
		t.Fatalf("multi-block Cmd+C with CopyPlainText=true = %q; want %q", got, want)
	}
}
