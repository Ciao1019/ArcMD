package editor

import (
	"math"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/ui"
)

func (e *Editor) handleZoomInput(ev ui.InputEvent) bool {
	if e.onZoom == nil || e.printing || e.HasZoomModal() {
		return false
	}
	if ev.Kind == ui.InputZoom {
		if ev.Scale > 0 && !math.IsNaN(float64(ev.Scale)) && !math.IsInf(float64(ev.Scale), 0) {
			next := clampPageZoom(e.zoomFactor() * ev.Scale)
			if next != e.zoomFactor() {
				e.onZoom(next)
			}
		}
		return true
	}
	if ev.Kind == ui.InputScroll && ev.Mods&(ui.Cmd|ui.Ctrl) != 0 {
		delta := -ev.DY
		if delta == 0 {
			delta = ev.DX
		}
		if delta == 0 {
			return true
		}
		if e.zoomWheelAccum != 0 && (delta > 0) != (e.zoomWheelAccum > 0) {
			e.zoomWheelAccum = 0
		}
		e.zoomWheelAccum += delta
		const notch = float32(24)
		steps := 0
		switch {
		case e.zoomWheelAccum >= notch:
			steps = 1
			e.zoomWheelAccum = 0
		case e.zoomWheelAccum <= -notch:
			steps = -1
			e.zoomWheelAccum = 0
		}
		if steps != 0 {
			curPct := int(math.Round(float64(e.zoomFactor()) * 100))
			var targetPct int
			if steps > 0 {
				targetPct = ((curPct / 10) + 1) * 10
			} else {
				targetPct = ((curPct - 1) / 10) * 10
			}
			next := clampPageZoom(float32(targetPct) / 100)
			if next != e.zoomFactor() {
				e.onZoom(next)
			}
		}
		return true
	}
	return false
}

func (e *Editor) input(ev ui.InputEvent, read func() string, write func(string), open func(string)) bool {
	e.lastInput = ev.Time
	if e.handleZoomInput(ev) {
		return true
	}
	e.sync()
	if e.blockPointer(ev) {
		return true
	}
	if e.handleCodeScrollInput(ev) {
		return true
	}
	if e.handleCompactScrollInput(ev) {
		return true
	}
	if e.structuralPointer(ev) {
		return true
	}
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button == 1 {
			if e.Anchor == e.Caret && len(e.blocks.ids) == 0 && !e.inSideMargin(ev.X, ev.Y) {
				if at, ok := e.IndexForPoint(ui.Point{X: ev.X, Y: ev.Y}); ok {
					e.UnmarkText()
					e.SetSelection(at, at)
					e.vertical = false
				}
			}
			return false
		}
		if ev.Button != 0 {
			return false
		}
		if left, right := e.contentSpanAt(ev.Y); ev.X < left || ev.X > right {
			if e.clickInlineMathPopover(ev.X, ev.Y) {
				e.blurStage = blurIdle
				return true
			}
			if e.clickNumberedMarker(ev.X, ev.Y) {
				e.blurStage = blurIdle
				return true
			}
			if ev.X < left && ev.Mods == 0 && len(e.handles) > 0 && ev.Y >= e.indexedHandle(0).top-8 && ev.Y <= e.contentEnd {
				e.blocks.marqueeArm = true
				e.blocks.marquee = false
				e.blocks.x0, e.blocks.y0 = ev.X, ev.Y
				return true
			}
			e.blur()
			return true
		}
		e.blurStage = blurIdle
		e.pendingLink = linkSpan{}
		e.UnmarkText()
		e.clearBlocks()
		at, ok := e.IndexForPoint(ui.Point{X: ev.X, Y: ev.Y})
		if !ok {
			return false
		}
		if ev.Mods&ui.Cmd != 0 {
			if span, ok := e.linkAtPoint(ev.X, ev.Y, false); ok && open != nil && safeLinkURL(span.url) {
				open(strings.TrimSpace(span.url))
				return true
			}
			if name := e.wikiAtPoint(ev.X, ev.Y); name != "" && e.openWiki != nil {
				e.openWiki(name)
				return true
			}
			if tag := e.tagAtPoint(ev.X, ev.Y); tag != "" && e.openTag != nil {
				e.openTag(tag)
				return true
			}
		}
		if e.inlineClick(ev.X, ev.Y) {
			return true
		}
		if e.toggleTask(at, ev.X) {
			return true
		}
		if e.clickNumberedMarker(ev.X, ev.Y) {
			return true
		}
		if (!e.ReadOnly || e.directLinks || e.isLinkedInlineImagePoint(ev.X, ev.Y)) && ev.Mods == 0 && ev.Clicks < 2 {
			if span, ok := e.linkAtPoint(ev.X, ev.Y, !e.directLinks); ok {
				e.pendingLink = span
				e.pendingLinkDown = ui.Point{X: ev.X, Y: ev.Y}
			}
		}
		e.markPop = markPopover{}
		if ev.Mods == 0 && ev.Clicks < 2 {
			if run, ok := e.annotationAt(at); ok && e.pointInTextRange(ev.X, ev.Y, run.start, run.end) {
				e.openMarkPopover(at)
			}
		}
		e.dragging, e.reveal = true, false
		e.selectionDrag = selectionDrag{scrollY: e.Scroll.Y, at: ev.Time}
		e.outlineState.jump = outlineJump{}
		if ev.Mods&ui.Shift != 0 {
			e.SetSelection(e.Anchor, at)
		} else {
			e.SetSelection(at, at)
		}
		if ev.Clicks == 2 {
			if r := e.rowAt(at); r != nil && isPDFEmbed(r.leaf.Block) && !r.pdfBrowsing && r.pdfError == nil {
				e.openPDFModal(r.leaf.Block.ID)
				return true
			}
			if r := e.rowAt(at); r != nil && isBookEmbed(r.leaf.Block) && !r.bookBrowsing && r.bookError == nil {
				e.openBookModal(r.leaf.Block.ID)
				return true
			}
			if !e.ReadOnly {
				if r := e.rowAt(at); r != nil && !r.boardEditing &&
					(isWhiteboardBlock(r.leaf.Block) || (isWhiteboardEmbed(r.leaf.Block) && e.saveWhiteboard != nil)) {
					e.startBoardEditing(r.leaf.Block.ID)
					return true
				}
			}
			e.selectWord(at)
		}
		if ev.Clicks >= 3 {
			a, z := e.lineRange(at)
			e.SetSelection(a, z)
		}
		e.vertical = false
		return true
	case ui.InputPointerMove:
		if !e.dragging {
			return false
		}
		if !e.selectionDrag.moved {
			e.selectionDrag.at = ev.Time
		}
		e.selectionDrag.moved = true
		dx, dy := ev.X-e.pendingLinkDown.X, ev.Y-e.pendingLinkDown.Y
		if dx*dx+dy*dy > 9 {
			e.pendingLink = linkSpan{}
		}
		e.extendSelection(ev.X, min(max(ev.Y, e.Scroll.Y), e.Scroll.Y+e.viewport))
		if e.Anchor != e.Caret {
			e.pendingLink = linkSpan{}
		}
		return true
	case ui.InputPointerUp:
		if !e.dragging {
			return false
		}
		e.dragging = false
		if span := e.pendingLink; span.url != "" {
			e.pendingLink = linkSpan{}
			if e.Anchor == e.Caret {
				if hit, ok := e.linkAtPoint(ev.X, ev.Y, !e.directLinks); ok && hit == span {
					if e.ReadOnly && (e.directLinks || e.isLinkedInlineImagePoint(ev.X, ev.Y)) && open != nil && safeLinkURL(span.url) {
						open(strings.TrimSpace(span.url))
					} else if !e.ReadOnly {
						e.openLinkSpan(span)
					}
				}
			}
		}
		return true
	case ui.InputSelect:
		runes := []rune(e.displayText())
		a, z := min(ev.Start, len(runes)), min(ev.End, len(runes))
		e.SetSelection(ui.UTF16Len(string(runes[:max(0, a)])), ui.UTF16Len(string(runes[:max(0, z)])))
		e.reveal = true
		return true
	case ui.InputCommand:
		return e.command(ev.Text, read, write)
	case ui.InputKeyDown:
		return e.key(ev, read, write)
	}
	return false
}

// ClearFormatting strips inline formatting (including links, colors,
// highlights and hand-drawn annotations) from the active text selection,
// multi-block selection, or typing style at a collapsed caret.
func (e *Editor) ClearFormatting() {
	if e.ReadOnly {
		e.refused = true
		return
	}
	e.UnmarkText()
	if len(e.blocks.ids) > 0 {
		e.Engine.ClearBlocksFormatting(e.BlockSelection())
	} else {
		e.Engine.ClearFormatting()
	}
	e.annotatePanel = false
	e.markPop.open = false
	e.drawIn = annotationReveal{}
	e.sync()
	e.reveal = true
	e.RequestFocus = true
}

// CopyPlainText copies the active text or block selection to the clipboard
// as plain text with Markdown syntax and inline styling stripped.
func (e *Editor) CopyPlainText() {
	var write func(string)
	if e.services != (ui.Services{}) {
		write = e.services.WriteClipboard
	}
	e.command("copyPlain", nil, write)
}

func (e *Editor) command(command string, read func() string, write func(string)) bool {
	// Copy leaves the page where the reader scrolled, even when the
	// selected text is now offscreen.
	if command == "copy" {
		if len(e.blocks.ids) > 0 {
			e.copyBlocks(e.BlockSelection(), write)
			return true
		}
		if e.Anchor == e.Caret {
			if leaf := e.Engine.LeafAt(e.Caret); isAtomicVisualBlock(leaf.Block) {
				e.copySingleBlock(leaf.Block, write)
				return true
			}
		}
		e.copySelection(write)
		return true
	}
	if command == "copyPlain" {
		if len(e.blocks.ids) > 0 {
			e.copyBlocksPlain(e.BlockSelection(), write)
			return true
		}
		e.copyPlainSelection(write)
		return true
	}
	if command == "cut" && len(e.blocks.ids) > 0 {
		e.copyBlocks(e.BlockSelection(), write)
		if !e.ReadOnly {
			e.Engine.DeleteBlocks(e.BlockSelection())
			e.clearBlocks()
			e.sync()
		}
		return true
	}
	if command == "cut" && e.Anchor == e.Caret && len(e.blocks.ids) == 0 {
		if leaf := e.Engine.LeafAt(e.Caret); isAtomicVisualBlock(leaf.Block) {
			e.copySingleBlock(leaf.Block, write)
			if e.ReadOnly {
				e.refused = true
			} else {
				e.deleteAtomicBlock(leaf.Block.ID, true)
			}
			return true
		}
	}
	if command == "selectAll" {
		if len(e.blocks.ids) > 0 {
			e.SelectAllBlocks()
			return true
		}
		e.selectAll()
		e.vertical = false
		return true
	}
	e.reveal, e.vertical = true, false
	switch command {
	case "cut":
		e.copySelection(write)
		e.ReplaceText(nil, "")
	case "paste":
		startID := e.Engine.LeafAt(e.Caret).Block.ID
		e.pasteSelection(read)
		e.dismissInlineMenusOnPaste(startID)
	case "pasteAndMatchStyle":
		if e.ReadOnly {
			return true
		}
		startID := e.Engine.LeafAt(e.Caret).Block.ID
		e.UnmarkText()
		e.clearBlocks()
		e.Buffer.ReplaceText(nil, read())
		e.dismissInlineMenusOnPaste(startID)
	case "undo":
		e.Undo()
	case "redo":
		e.Redo()
	case "bold":
		e.Wrap("**")
	case "italic":
		e.Wrap("*")
	case "underline":
		e.Wrap("_")
	case "strikethrough":
		e.Wrap("~~")
	case "clearFormatting":
		e.ClearFormatting()
	case "delete":
		e.delete(-1, false, false)
	default:
		return false
	}
	return true
}

func (e *Editor) key(ev ui.InputEvent, read func() string, write func(string)) bool {
	mods, key := ev.Mods, ev.Key
	e.readOnlySpace = false
	for _, r := range e.reserved {
		if r.Mods == mods && r.Key == key {
			return false
		}
	}
	if e.slashKeys(ev) || e.wikiKeys(ev) || e.completeKeys(ev) {
		return true
	}
	if e.blockKeys(ev, write) {
		return true
	}
	if key == ui.KeyEscape && e.closeHeader() {
		return true
	}
	if e.tableSel.ok {
		// A selected row or column takes Backspace and Delete; any other
		// key puts the selection away.
		switch key {
		case ui.KeyBackspace, ui.KeyDelete:
			e.deleteTableLine()
			return true
		case ui.KeyEscape:
			e.tableSel = tableSelection{}
			return true
		}
		e.tableSel = tableSelection{}
	}
	if (e.annotatePanel || e.markPop.open) && key == ui.KeyEscape {
		e.annotatePanel, e.markPop.open = false, false
		return true
	}
	if e.menuOpen && key == ui.KeyEscape {
		e.menuOpen, e.menuSub = false, ""
		return true
	}
	if e.listNumOpen && key == ui.KeyEscape {
		e.listNumOpen = false
		return true
	}
	if e.colorOpen || e.langOpen {
		// Any key puts a picker away; Escape does nothing else.
		e.colorOpen, e.langOpen = false, false
		if key == ui.KeyEscape {
			return true
		}
	}
	if _, composing := e.MarkedRange(); !composing && key == ui.KeyEscape && mods == 0 && e.focused && e.rollbackMathFence() {
		return true
	}
	if _, composing := e.MarkedRange(); !composing && key == ui.KeyEscape && mods == 0 && e.focused && e.escapeInlineMath() {
		return true
	}
	if _, composing := e.MarkedRange(); !composing && key == ui.KeyEscape && mods == 0 && e.focused && e.SelectCaretBlock() {
		// Escape with nothing open selects the caret's block, as Notion's.
		return true
	}
	// TextInput supplies the space (or an IME commit). Consume its key here
	// so MyGo does not also interpret Space/Shift+Space as page scrolling;
	// in read-only mode, page the viewport instead of refusing a space edit.
	if key == ui.KeySpace && mods & ^ui.Shift == 0 {
		if e.ReadOnly {
			dy := max(e.viewport-40, e.viewport/2)
			if mods == ui.Shift {
				dy = -dy
			}
			e.scrollGlide = scrollGlideState{}
			e.outlineState.jump = outlineJump{}
			e.Scroll.Y = min(e.outlineState.scrollMax, max(0, e.Scroll.Y+dy))
			e.readOnlySpace = true
		}
		return true
	}
	if _, composing := e.MarkedRange(); composing {
		if key == ui.KeyEscape {
			e.cancelComposition()
			return true
		}
		return false
	}
	cmd := e.boundCommand(mods, key)
	if level, ok := headingCommandLevel(cmd); ok {
		if e.ReadOnly {
			e.refused = true
			return true
		}
		prefix := ""
		if level > 0 {
			prefix = strings.Repeat("#", level) + " "
		}
		e.Prefix(prefix)
		e.reveal = true
		return true
	}
	switch cmd {
	case "bold":
		return e.command("bold", read, write)
	case "italic":
		return e.command("italic", read, write)
	case "underline":
		return e.command("underline", read, write)
	case "strike":
		return e.command("strikethrough", read, write)
	case "code":
		e.Wrap("`")
		e.reveal = true
		return true
	case "clear-format":
		e.ClearFormatting()
		return true
	case "copy-plain":
		return e.command("copyPlain", read, write)
	case "newline":
		e.Engine.Insert(document.NewBlock("paragraph", ""))
		e.reveal = true
		return true
	case "find", "replace":
		// The find box takes them through the window's shortcuts.
		return false
	}
	if mods&ui.Cmd != 0 {
		commands := map[ui.Key]string{ui.KeyA: "selectAll", ui.KeyC: "copy", ui.KeyX: "cut", ui.KeyV: "paste", ui.KeyZ: "undo"}
		if mods == ui.Cmd|ui.Shift && key == ui.KeyZ {
			return e.command("redo", read, write)
		}
		if mods == ui.Cmd|ui.Shift && key == ui.KeyV {
			return e.command("pasteAndMatchStyle", read, write)
		}
		if mods == ui.Cmd {
			if name, ok := commands[key]; ok {
				return e.command(name, read, write)
			}
		}
	}
	shift := mods&ui.Shift != 0
	word := mods&ui.Alt != 0
	line := mods&ui.Cmd != 0
	if runtime.GOOS != "darwin" {
		word, line = mods&ui.Ctrl != 0, false
	}
	position := e.Caret
	switch key {
	case ui.KeyLeft, ui.KeyRight:
		direction := -1
		if key == ui.KeyRight {
			direction = 1
		}
		selection := e.Selection().Range
		if !shift && !word && !line && selection.Start == selection.End {
			l := e.LeafAt(position)
			if direction < 0 && position == l.Start {
				if next, changed := exitCapsuleMarks(e.Engine.CurrentStyle(), document.Style{}); changed {
					e.Engine.Typing, e.Engine.Explicit = next, true
					e.reveal = true
					return true
				}
			} else if direction > 0 {
				var nextAt document.Style
				if position < l.End {
					nextAt = runStyleAfter(l, position)
				}
				if next, changed := exitCapsuleMarks(e.Engine.CurrentStyle(), nextAt); changed {
					e.Engine.Typing, e.Engine.Explicit = next, true
					e.reveal = true
					return true
				}
			}
		}
		switch {
		case !shift && selection.Start != selection.End:
			position = selection.Start
			if direction > 0 {
				position = selection.End
			}
		case line:
			a, z := e.lineRange(position)
			position = a
			if direction > 0 {
				position = z
			}
		case word:
			position = e.wordBoundary(position, direction)
		default:
			position = e.boundary(position, direction)
		}
		e.vertical = false
	case ui.KeyUp, ui.KeyDown, ui.KeyPageUp, ui.KeyPageDown:
		if line {
			position = 0
			if key == ui.KeyDown {
				position = e.Len()
			}
		} else {
			dir := 1
			if key == ui.KeyUp || key == ui.KeyPageUp {
				dir = -1
			}
			page := key == ui.KeyPageUp || key == ui.KeyPageDown
			next, ok := e.moveVertical(position, dir, page)
			if !ok {
				return false
			}
			position = next
			e.vertical = true
		}
	case ui.KeyHome, ui.KeyEnd:
		a, z := e.lineRange(position)
		if mods&ui.Ctrl != 0 {
			a, z = 0, e.Len()
		}
		position = a
		if key == ui.KeyEnd {
			position = z
		}
		e.vertical = false
	case ui.KeyBackspace, ui.KeyDelete:
		direction := -1
		if key == ui.KeyDelete {
			direction = 1
		}
		e.delete(direction, word, line)
		e.reveal, e.vertical = true, false
		return true
	case ui.KeyEnter:
		e.UnmarkText()
		e.refused = e.refused || e.ReadOnly
		if !shift && mods == 0 && !e.ReadOnly {
			if e.consumeMathFenceEnter() || e.exitInlineMath() || e.cellEnter() {
				e.vertical = false
				return true
			}
		}
		e.Engine.Enter(shift)
		e.sync()
		e.reveal, e.vertical = true, false
		return true
	case ui.KeyTab:
		if !shift && mods == 0 && e.Anchor == e.Caret && !e.ReadOnly {
			if e.exitInlineMath() || e.tabOutPair() || e.exitDisplayMath() || e.exitMermaid() {
				e.vertical = false
				return true
			}
		}
		if e.cellTab(shift) {
			return true
		}
		if mods&ui.Ctrl != 0 || mods&ui.Alt != 0 {
			return false
		}
		if mods & ^ui.Shift == 0 && e.columnTab(shift) {
			return true
		}
		if e.ReadOnly {
			e.refused = true
			return true
		}
		e.tab(shift)
		e.reveal = true
		return true
	default:
		return false
	}
	if shift {
		e.SetSelection(e.Anchor, position)
	} else {
		e.SetSelection(position, position)
	}
	e.reveal = true
	return true
}

func (e *Editor) isAtomicOrHiddenImage(b *document.Block) bool {
	return isAtomicVisualBlock(b) || (b != nil && b.Kind == "image" && !e.captionVisible(b))
}

func (e *Editor) delete(direction int, word, line bool) {
	if e.ReadOnly {
		e.refused = true
		return
	}
	if direction < 0 && !word && !line && (e.rollbackSmartInput() || e.deletePair()) {
		return
	}
	r := e.Selection().Range
	first := e.Engine.LeafAt(r.Start)
	last := e.Engine.LeafAt(r.End)
	if first.Block.ID == last.Block.ID && e.isAtomicOrHiddenImage(first.Block) {
		e.deleteAtomicBlock(first.Block.ID, direction < 0)
		return
	}
	if r.Start != r.End && first.Block.ID != last.Block.ID {
		if e.isAtomicOrHiddenImage(first.Block) {
			r.Start = first.Start
		}
		if e.isAtomicOrHiddenImage(last.Block) {
			r.End = last.End
		}
	}
	if r.Start == r.End {
		// At a block edge the engine decides: tables, columns and attachments
		// are removed when empty or stepped over, never merged into text.
		if _, composing := e.MarkedRange(); !composing && e.Engine.Join(direction < 0) {
			e.sync()
			return
		}
		at := e.boundary(e.Caret, direction)
		if word {
			at = e.wordBoundary(e.Caret, direction)
		}
		if line {
			a, z := e.lineRange(e.Caret)
			at = a
			if direction > 0 {
				at = z
			}
		}
		r.Start, r.End = min(e.Caret, at), max(e.Caret, at)
		nextFirst, nextLast := e.Engine.LeafAt(r.Start), e.Engine.LeafAt(r.End)
		if nextFirst.Block.ID != nextLast.Block.ID {
			if e.isAtomicOrHiddenImage(nextFirst.Block) {
				r.Start = nextFirst.Start
			}
			if e.isAtomicOrHiddenImage(nextLast.Block) {
				r.End = nextLast.End
			}
		}
	}
	e.ReplaceText(&r, "")
}

func (e *Editor) indent(remove bool) {
	delta := 1
	if remove {
		delta = -1
	}
	e.Engine.Indent(delta)
	e.sync()
}

func (e *Editor) tab(shift bool) {
	a, z := min(e.Anchor, e.Caret), max(e.Anchor, e.Caret)
	first := e.Engine.LeafAt(a)
	last := e.Engine.LeafAt(z)
	if (first.Block.Kind == "code" || first.Block.Kind == "raw") && first.Block.ID == last.Block.ID && !isWhiteboardBlock(first.Block) {
		if shift || (a < z && strings.ContainsRune(e.slice(a, z), '\n')) {
			if e.Engine.IndentCode(shift) {
				e.sync()
			}
			return
		}
		e.ReplaceText(nil, "\t")
		return
	}
	if a == z {
		switch first.Block.Kind {
		case "bullet", "numbered", "task", "toggle":
			e.indent(shift)
			return
		}
		if !shift {
			e.ReplaceText(nil, "\t")
			return
		}
		if first.Block.Indent > 0 {
			e.indent(true)
			return
		}
		if a > first.Start && e.slice(a-1, a) == "\t" {
			r := ui.TextInputRange{Start: a - 1, End: a}
			e.ReplaceText(&r, "")
			return
		}
		ls, _ := e.lineRange(a)
		if ls < first.End && e.slice(ls, ls+1) == "\t" {
			r := ui.TextInputRange{Start: ls, End: ls + 1}
			e.ReplaceText(&r, "")
			next := max(ls, a-1)
			e.SetSelection(next, next)
		}
		return
	}
	e.indent(shift)
}

func blockContainsID(b *document.Block, id string) bool {
	if b.ID == id {
		return true
	}
	for _, c := range b.Children {
		if blockContainsID(c, id) {
			return true
		}
	}
	return false
}

// selectAll widens the selection step by step: inline math formula, current
// leaf block, enclosing column or table, and finally the entire document.
func (e *Editor) selectAll() {
	total := e.Len()
	if total == 0 {
		e.SetSelection(0, 0)
		return
	}
	sel := e.Selection().Range
	var candidates []ui.TextInputRange

	if e.inlineEditing != "" && e.inlineMath.blockID != "" {
		if l, ok := e.Engine.LeafByID(e.inlineMath.blockID); ok {
			bodyStart := l.Start + e.inlineMath.start + 1
			bodyEnd := l.Start + e.inlineMath.end - 1
			if bodyStart < bodyEnd && sel.Start >= l.Start+e.inlineMath.start && sel.End <= l.Start+e.inlineMath.end {
				candidates = append(candidates, ui.TextInputRange{Start: bodyStart, End: bodyEnd})
			}
		}
	}

	first := e.Engine.LeafAt(sel.Start)
	last := e.Engine.LeafAt(sel.End)
	if first.Block.ID == last.Block.ID && first.Start < first.End {
		b := first.Block
		editable := !isWhiteboardBlock(b) && !isWhiteboardEmbed(b) && !isPDFEmbed(b) && !isBookEmbed(b) && !isDatabaseBlock(b) && !(b.Kind == "image" && !e.captionVisible(b)) && !(e.ReadOnly && (b.Kind == "math" || isMermaidBlock(b)))
		if editable {
			candidates = append(candidates, ui.TextInputRange{Start: first.Start, End: first.End})
		}
	}

	if first.Owner != "" && first.Owner == last.Owner {
		var owner *document.Block
		for _, b := range e.Doc.Blocks {
			if b.ID == first.Owner {
				owner = b
				break
			}
		}
		if owner != nil {
			switch owner.Kind {
			case "columns":
				for _, col := range owner.Children {
					if !blockContainsID(col, first.Block.ID) || !blockContainsID(col, last.Block.ID) {
						continue
					}
					colStart, colEnd := -1, -1
					for _, leaf := range e.Doc.Leaves() {
						if leaf.Owner == owner.ID && blockContainsID(col, leaf.Block.ID) {
							if colStart < 0 {
								colStart = leaf.Start
							}
							colEnd = leaf.End
						}
					}
					if colStart >= 0 && colStart < colEnd {
						candidates = append(candidates, ui.TextInputRange{Start: colStart, End: colEnd})
					}
					break
				}
			case "table":
				tableStart, tableEnd := -1, -1
				for _, leaf := range e.Doc.Leaves() {
					if leaf.Owner == owner.ID {
						if tableStart < 0 {
							tableStart = leaf.Start
						}
						tableEnd = leaf.End
					}
				}
				if tableStart >= 0 && tableStart < tableEnd {
					candidates = append(candidates, ui.TextInputRange{Start: tableStart, End: tableEnd})
				}
			}
		}
	}

	candidates = append(candidates, ui.TextInputRange{Start: 0, End: total})
	for _, c := range candidates {
		if c.Start <= sel.Start && c.End >= sel.End && (c.Start < sel.Start || c.End > sel.End) {
			e.SetSelection(c.Start, c.End)
			return
		}
	}
	e.SetSelection(0, total)
}

func (e *Editor) selectWord(at int) {
	at = e.clamp(at)
	a, z := e.lineRange(at)
	// lineRange excludes a trailing CR. A click between CR and LF keeps
	// that position, just as selection in the flattened text did.
	if at > z {
		e.SetSelection(at, at)
		return
	}
	text := e.slice(a, z)
	start := ui.UTF16ByteOffset(text, at-a)
	end := start
	for start > 0 {
		r, n := utf8.DecodeLastRuneInString(text[:start])
		if strings.ContainsRune(" \t\r\n", r) {
			break
		}
		start -= n
	}
	for end < len(text) {
		r, n := utf8.DecodeRuneInString(text[end:])
		if strings.ContainsRune(" \t\r\n", r) {
			break
		}
		end += n
	}
	e.SetSelection(a+ui.UTF16Len(text[:start]), a+ui.UTF16Len(text[:end]))
}

func runStyleAfter(l document.Leaf, docPos int) document.Style {
	local := docPos - l.Start
	at := 0
	for _, r := range l.Block.Runs {
		n := document.Len(r.Text)
		if at+n > local {
			return r.Style
		}
		at += n
	}
	return document.Style{}
}

func exitCapsuleMarks(cur, nextAfter document.Style) (document.Style, bool) {
	changed := false
	if cur.Kbd && !nextAfter.Kbd {
		cur.Kbd = false
		changed = true
	}
	if cur.Code && !nextAfter.Code {
		cur.Code = false
		changed = true
	}
	if cur.Script != "" && nextAfter.Script == "" {
		cur.Script = ""
		changed = true
	}
	if cur.Link != "" && nextAfter.Link != cur.Link {
		cur.Link = ""
		changed = true
	}
	return cur, changed
}

func (e *Editor) moveVertical(position, dir int, page bool) (int, bool) {
	r, _, ok := e.BoundsForRange(ui.TextInputRange{Start: position, End: position})
	if !ok {
		return position, false
	}
	if !e.vertical {
		e.preferredX = r.X
	}
	if page {
		distance := max(r.H, e.viewport-48)
		if dir < 0 {
			distance = -distance
		}
		next, _ := e.IndexForPoint(ui.Point{X: e.preferredX, Y: r.Y + r.H/2 + distance})
		if next != position {
			return next, true
		}
	}
	idx := e.rowIndexAt(position)
	if idx < 0 {
		return position, false
	}
	cur := e.ensureRow(idx)
	opaque := func(row *row) bool {
		return (row.picture != nil && !e.captionVisible(row.leaf.Block)) || row.mermaidHidden || isWhiteboardBlock(row.leaf.Block) || row.boardEmbed
	}
	if !opaque(cur) && cur.layout != nil {
		lines := cur.layout.Lines()
		if len(lines) > 1 {
			local := max(0, min(position-cur.leaf.Start, cur.leaf.End-cur.leaf.Start))
			caret := cur.layout.Caret(local)
			cy := caret.Y + caret.H/2
			lineIdx := 0
			bestDist := float32(math.MaxFloat32)
			for i, ln := range lines {
				d := max(ln.Rect.Y-cy, cy-(ln.Rect.Y+ln.Rect.H), 0)
				if d < bestDist {
					bestDist = d
					lineIdx = i
				}
			}
			targetLine := lineIdx + dir
			if targetLine >= 0 && targetLine < len(lines) {
				px := e.preferredX - cur.x
				if _, _, _, _, scrollX, noWrap := e.codeScrollFor(*cur); noWrap {
					px += scrollX
				}
				ln := lines[targetLine]
				return cur.leaf.Start + cur.layout.IndexAt(ui.Point{X: px, Y: ln.Rect.Y + ln.Rect.H/2}), true
			}
		}
	}
	boundY := cur.rect.Y
	if dir > 0 {
		boundY = cur.rect.Y + cur.rect.H
	}
	nextIdx := e.rowSpace.nearestVertical(e.preferredX, boundY, dir, idx)
	if nextIdx < 0 {
		return position, true
	}
	next := e.ensureRow(nextIdx)
	if opaque(next) || next.layout == nil {
		return next.leaf.Start, true
	}
	px := e.preferredX - next.x
	if _, _, _, _, scrollX, noWrap := e.codeScrollFor(*next); noWrap {
		px += scrollX
	}
	lines := next.layout.Lines()
	var py float32
	if len(lines) > 0 {
		ln := lines[0]
		if dir < 0 {
			ln = lines[len(lines)-1]
		}
		py = ln.Rect.Y + ln.Rect.H/2
	} else if dir < 0 {
		_, h := next.layout.Size()
		py = max(0, h-1)
	}
	return next.leaf.Start + next.layout.IndexAt(ui.Point{X: px, Y: py}), true
}
