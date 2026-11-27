package editor

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestRightMarginClickBlursWithoutMovingCaret(t *testing.T) {
	e := New("Hello world\n\nSecond paragraph")
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme()})
	}, 1300, 650)
	tt.Frame()
	focus(e, tt)
	e.SetSelection(2, 2)
	tt.Frame()

	bounds, ok := tt.Find("文档")
	if !ok {
		t.Fatal("missing writing surface")
	}
	if !tt.Focused("文档") || !e.Focused() {
		t.Fatal("editor should start focused")
	}

	rightMarginX := bounds.X + e.padding + e.BodyWidth() + 40
	lineY := bounds.Y + e.handles[0].top + 10
	tt.ClickAt(rightMarginX, lineY)
	tt.Frame()

	if tt.Focused("文档") || e.Focused() {
		t.Fatalf("clicking right margin should blur editor: tt.Focused=%v e.Focused=%v", tt.Focused("文档"), e.Focused())
	}
	if e.Caret != 2 || e.Anchor != 2 {
		t.Fatalf("clicking right margin moved caret to (%d, %d), want (2, 2)", e.Anchor, e.Caret)
	}
	before := e.Text()
	tt.Type("XYZ")
	if e.Text() != before {
		t.Fatalf("typing while blurred edited document: got %q, want %q", e.Text(), before)
	}

	// Clicking right margin again while already unfocused must not re-focus.
	tt.ClickAt(rightMarginX, lineY)
	tt.Frame()
	if tt.Focused("文档") || e.Focused() {
		t.Fatalf("clicking right margin while blurred re-focused editor: tt.Focused=%v e.Focused=%v", tt.Focused("文档"), e.Focused())
	}

	// Clicking blank space after a short line inside the prose column still focuses and moves caret to line end.
	insideColumnX := bounds.X + e.padding + 260
	tt.ClickAt(insideColumnX, lineY)
	tt.Frame()
	if !tt.Focused("文档") || !e.Focused() {
		t.Fatal("clicking inside prose column should re-focus editor")
	}
	if e.Caret != len("Hello world") {
		t.Fatalf("expected caret at line end %d, got %d", len("Hello world"), e.Caret)
	}
	tt.Type("!")
	if !strings.HasPrefix(e.Text(), "Hello world!") {
		t.Fatalf("typing after re-focus failed: %q", e.Text())
	}
}

func TestLeftMarginClickBlursAndDragSelectsBlocks(t *testing.T) {
	e := New("# Title\n\n## Section\n\nAlpha line\n\nBeta line\n\nGamma line")
	outlineOpen := true
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{
			Width:      w,
			Theme:      c.Theme(),
			Outline:    outlineOpen,
			SetOutline: func(v bool) { outlineOpen = v },
		})
	}, 1500, 650)
	tt.Frame()
	if e.outlineState.mode != outlineBeside || !e.OutlineOpen() {
		t.Fatalf("expected outline beside page, mode=%v open=%v", e.outlineState.mode, e.OutlineOpen())
	}

	focus(e, tt)
	leaves := e.Doc.Leaves()
	mid := leaves[2].Start + 3
	e.SetSelection(mid, mid)
	tt.Frame()

	bounds, ok := tt.Find("文档")
	if !ok {
		t.Fatal("missing writing surface")
	}
	if e.padding <= 80 {
		t.Fatalf("expected wide left margin between outline and prose, got padding=%v", e.padding)
	}

	// 1. Plain click far left (X < e.padding - 60) blurs without selecting any block.
	farLeftX := bounds.X + e.padding - 80
	alphaY := bounds.Y + e.handles[2].top + 10
	tt.ClickAt(farLeftX, alphaY)
	tt.Frame()
	if tt.Focused("文档") || e.Focused() {
		t.Fatalf("left-margin click did not blur: tt.Focused=%v e.Focused=%v", tt.Focused("文档"), e.Focused())
	}
	if len(e.BlockSelection()) != 0 {
		t.Fatalf("left-margin single click should not trigger marquee selection: %v", e.BlockSelection())
	}
	if e.Caret != mid {
		t.Fatalf("left-margin click moved caret from %d to %d", mid, e.Caret)
	}

	// 2. Micro-jitter (<= 4px) on left margin still counts as a click and blurs.
	focus(e, tt)
	tt.Press(farLeftX, alphaY)
	tt.Move(farLeftX+2, alphaY+2)
	tt.Release(farLeftX+2, alphaY+2)
	tt.Frame()
	if tt.Focused("文档") || e.Focused() || len(e.BlockSelection()) != 0 {
		t.Fatalf("micro-jitter left click should blur without marquee: focused=%v blocks=%v", e.Focused(), e.BlockSelection())
	}

	// 3. Click in handle gutter zone (e.padding - 60 <= X < e.padding) away from handles/fold button blurs.
	// Handle grip sits at [e.padding-30, e.padding-6]; [e.padding-5, e.padding) is empty gutter beside text.
	focus(e, tt)
	e.SetSelection(mid, mid)
	tt.Frame()
	gutterX := bounds.X + e.padding - 3
	tt.ClickAt(gutterX, alphaY)
	tt.Frame()
	if tt.Focused("文档") || e.Focused() {
		t.Fatalf("handle-gutter empty click did not blur: tt.Focused=%v e.Focused=%v", tt.Focused("文档"), e.Focused())
	}
	if e.Caret != mid {
		t.Fatalf("handle-gutter empty click snapped caret to %d, want %d", e.Caret, mid)
	}

	// Clicking directly on the block grip handle (⋮⋮) still opens the block menu instead of blurring.
	hAlpha := e.handles[2]
	tt.Move(bounds.X+e.padding+10, alphaY)
	tt.Frame()
	tt.ClickAt(bounds.X+hAlpha.rect.X+hAlpha.rect.W/2, bounds.Y+hAlpha.rect.Y+hAlpha.rect.H/2)
	tt.Frame()
	if !e.menuOpen || e.menuID != hAlpha.id {
		t.Fatalf("clicking block grip handle should open block menu: menuOpen=%v menuID=%q", e.menuOpen, e.menuID)
	}
	// Clicking left margin while block menu overlay is open dismisses menu and blurs.
	tt.ClickAt(farLeftX, alphaY)
	tt.Frame()
	if e.menuOpen || tt.Focused("文档") || e.Focused() {
		t.Fatalf("left-margin click should dismiss block menu and blur: menuOpen=%v focused=%v", e.menuOpen, e.Focused())
	}

	// 4. Dragging past threshold in left margin from unfocused state selects blocks and focuses for keyboard actions.
	betaY := bounds.Y + e.handles[3].top + 10
	tt.Press(farLeftX, alphaY)
	tt.Move(farLeftX, betaY)
	tt.Release(farLeftX, betaY)
	tt.Frame()
	if got := e.BlockSelection(); len(got) != 2 || got[0] != e.Doc.Blocks[2].ID || got[1] != e.Doc.Blocks[3].ID {
		t.Fatalf("left-margin drag did not select Alpha+Beta: %v", got)
	}
	if !tt.Focused("文档") || !e.Focused() {
		t.Fatal("marquee block selection should focus editor for keyboard operations")
	}

	// 5. Clicking left margin while blocks are selected clears block selection and blurs.
	tt.ClickAt(farLeftX, alphaY)
	tt.Frame()
	if len(e.BlockSelection()) != 0 || tt.Focused("文档") || e.Focused() {
		t.Fatalf("margin click should clear block selection and blur: blocks=%v focused=%v", e.BlockSelection(), e.Focused())
	}

	// 6. Clicking fold triangle in the left margin still toggles folding rather than blurring.
	var sectionFold *foldButton
	for i := range e.foldButtons {
		if e.foldButtons[i].id == e.Doc.Blocks[1].ID {
			sectionFold = &e.foldButtons[i]
			break
		}
	}
	if sectionFold == nil {
		t.Fatal("expected foldable heading button for ## Section")
	}
	tt.Move(bounds.X+e.padding+10, bounds.Y+sectionFold.rect.Y+sectionFold.rect.H/2)
	tt.Frame()
	tt.ClickAt(bounds.X+sectionFold.rect.X+sectionFold.rect.W/2, bounds.Y+sectionFold.rect.Y+sectionFold.rect.H/2)
	tt.Frame()
	if !e.Doc.Blocks[1].Folded {
		t.Fatal("clicking heading fold triangle in left margin did not fold section")
	}
}

func TestSideMarginClickDismissesFormulasAndKeepsTextDrag(t *testing.T) {
	e := New("Formula $a+b$ here\n\n$$\nx^2 + y^2\n$$\n\nPlain text line")
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme()})
	}, 1300, 650)
	tt.Frame()
	focus(e, tt)

	bounds, ok := tt.Find("文档")
	if !ok {
		t.Fatal("missing writing surface")
	}
	rightX := bounds.X + e.padding + e.BodyWidth() + 40
	leftX := bounds.X + e.padding - 80

	// Active inline formula collapses on right-margin click.
	e.SetSelection(10, 10) // inside $a+b$
	tt.Frame()
	if e.inlineEditing == "" || e.inlineMath.blockID == "" {
		t.Fatal("expected inline math editor to be active")
	}
	tt.ClickAt(rightX, bounds.Y+e.handles[0].top+10)
	tt.Frame()
	if tt.Focused("文档") || e.Focused() || e.inlineEditing != "" || e.inlineMath.blockID != "" {
		t.Fatalf("right-margin click did not blur and collapse inline math: focused=%v inline=%q", e.Focused(), e.inlineEditing)
	}

	// Active display math block collapses on left-margin click.
	focus(e, tt)
	mathLeaf := e.Doc.Leaves()[1]
	e.SetSelection(mathLeaf.Start+1, mathLeaf.Start+1)
	tt.Frame()
	if e.mathEditing != mathLeaf.Block.ID {
		t.Fatalf("expected display math editing %q, got %q", mathLeaf.Block.ID, e.mathEditing)
	}
	tt.ClickAt(leftX, bounds.Y+e.handles[0].top+10)
	tt.Frame()
	if tt.Focused("文档") || e.Focused() || e.mathEditing != "" {
		t.Fatalf("left-margin click did not blur and collapse display math: focused=%v math=%q", e.Focused(), e.mathEditing)
	}

	// Text selection (with floating selection bar overlay) collapses and stays blurred across subsequent frames.
	focus(e, tt)
	plainLeaf := e.Doc.Leaves()[2]
	e.SetSelection(plainLeaf.Start, plainLeaf.Start+5)
	tt.Frame()
	tt.ClickAt(rightX, bounds.Y+e.handles[2].top+10)
	tt.Frame()
	tt.Frame()
	if tt.Focused("文档") || e.Focused() || e.Anchor != e.Caret {
		t.Fatalf("margin click did not collapse text selection and blur: focused=%v sel=(%d,%d)", e.Focused(), e.Anchor, e.Caret)
	}

	// Dragging a text selection from inside the prose column out into the right margin keeps focus and selection.
	plainY := bounds.Y + e.handles[2].top + 10
	tt.Press(bounds.X+e.padding+4, plainY)
	tt.Move(rightX, plainY)
	tt.Frame()
	if tt.Cursor() != ui.CursorText {
		t.Fatalf("expected CursorText while dragging selection into margin, got %v", tt.Cursor())
	}
	tt.Release(rightX, plainY)
	tt.Frame()
	if !tt.Focused("文档") || !e.Focused() || e.Anchor == e.Caret {
		t.Fatalf("dragging selection into right margin should keep focus and selection: focused=%v sel=(%d,%d)", e.Focused(), e.Anchor, e.Caret)
	}
}

func TestSideMarginCursorWideTableAndBookmarks(t *testing.T) {
	md := strings.Join([]string{
		"Short line",
		"",
		"| Column Alpha Long | Column Beta Long | Column Gamma Long | Column Delta Long | Column Epsilon Long | Column Zeta Long |",
		"|---|---|---|---|---|---|",
		"| alpha value one | beta value two | gamma value three | delta value four | epsilon value five | zeta value six |",
	}, "\n")
	e := New(md)
	bookmarked := map[string]string{}
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{
			Width:     w,
			Theme:     c.Theme(),
			Bookmarks: bookmarked,
			ToggleBookmark: func(id string) {
				if _, ok := bookmarked[id]; ok {
					delete(bookmarked, id)
				} else {
					bookmarked[id] = "mark"
				}
			},
		})
	}, 1400, 650)
	tt.Frame()
	focus(e, tt)

	bounds, ok := tt.Find("文档")
	if !ok {
		t.Fatal("missing writing surface")
	}
	paraY := bounds.Y + e.handles[0].top + 10

	// Cursor is text inside prose column and default arrow in both side margins.
	tt.Move(bounds.X+e.padding+20, paraY)
	tt.Frame()
	if tt.Cursor() != ui.CursorText {
		t.Fatalf("expected CursorText inside prose column, got %v", tt.Cursor())
	}
	tt.Move(bounds.X+e.padding-40, paraY)
	tt.Frame()
	if tt.Cursor() != ui.CursorDefault {
		t.Fatalf("expected CursorDefault in left margin, got %v", tt.Cursor())
	}
	tt.Move(bounds.X+e.padding+e.BodyWidth()+48, paraY)
	tt.Frame()
	if tt.Cursor() != ui.CursorDefault {
		t.Fatalf("expected CursorDefault in right margin, got %v", tt.Cursor())
	}

	// Hovering the right-margin bookmark button keeps pointer cursor and clicking it keeps editor focus.
	tt.Move(bounds.X+e.padding+20, paraY)
	tt.Frame()
	mark, ok := tt.Find("添加书签")
	if !ok {
		t.Fatal("missing bookmark hover button")
	}
	tt.Move(mark.X+mark.W/2, mark.Y+mark.H/2)
	tt.Frame()
	if tt.Cursor() != ui.CursorPointer {
		t.Fatalf("expected CursorPointer over bookmark button, got %v", tt.Cursor())
	}
	tt.ClickAt(mark.X+mark.W/2, mark.Y+mark.H/2)
	tt.Frame()
	if _, ok := bookmarked[e.Doc.Blocks[0].ID]; !ok || !tt.Focused("文档") {
		t.Fatalf("bookmark button click should toggle bookmark and preserve focus: bookmarked=%v focused=%v", ok, tt.Focused("文档"))
	}

	// Wide table extending past the standard prose column remains editable in its extended cells.
	tblDec, ok := e.tableDecoration(e.Doc.Blocks[1].ID)
	if !ok {
		t.Fatal("missing table decoration")
	}
	if tblDec.rect.X >= e.padding || tblDec.rect.X+tblDec.rect.W <= e.padding+e.BodyWidth() {
		t.Fatalf("expected wide table to extend past prose column: table=%+v prose=[%v, %v]", tblDec.rect, e.padding, e.padding+e.BodyWidth())
	}
	extCellX := bounds.X + tblDec.rect.X + tblDec.rect.W - 20
	extCellY := bounds.Y + tblDec.rect.Y + tblDec.rect.H/2
	tt.Move(extCellX, extCellY)
	tt.Frame()
	if tt.Cursor() != ui.CursorText {
		t.Fatalf("expected CursorText over wide table cell, got %v", tt.Cursor())
	}
	tt.ClickAt(extCellX, extCellY)
	tt.Frame()
	if !tt.Focused("文档") || !e.Focused() {
		t.Fatal("clicking extended wide table cell should keep focus")
	}

	// Pure margin outside the wide table still blurs and shows default cursor.
	outsideTableX := bounds.X + tblDec.rect.X + tblDec.rect.W + 40
	tt.Move(outsideTableX, extCellY)
	tt.Frame()
	if tt.Cursor() != ui.CursorDefault {
		t.Fatalf("expected CursorDefault outside wide table, got %v", tt.Cursor())
	}
	tt.ClickAt(outsideTableX, extCellY)
	tt.Frame()
	if tt.Focused("文档") || e.Focused() {
		t.Fatal("clicking margin outside wide table should blur editor")
	}
}

func TestBlurredEditorAllowsSpaceScrolling(t *testing.T) {
	e := New(strings.Repeat("Reading paragraph line.\n\n", 80))
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme()})
	}, 1300, 650)
	tt.Frame()
	focus(e, tt)
	bounds, ok := tt.Find("文档")
	if !ok {
		t.Fatal("missing writing surface")
	}

	rightMarginX := bounds.X + e.padding + e.BodyWidth() + 40
	tt.ClickAt(rightMarginX, bounds.Y+100)
	tt.Frame()
	if tt.Focused("文档") || e.Focused() {
		t.Fatal("expected editor to be blurred")
	}

	beforeText := e.Text()
	beforeScroll := e.Scroll.Y
	tt.TypeKey(0, ui.KeySpace, " ")
	tt.Frame()
	if e.Text() != beforeText {
		t.Fatal("Space while blurred should not edit text")
	}
	if e.Scroll.Y <= beforeScroll {
		t.Fatalf("Space while blurred should scroll page down: before=%v after=%v", beforeScroll, e.Scroll.Y)
	}
}
