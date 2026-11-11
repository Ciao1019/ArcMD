package editor

import (
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/ui"
)

func (e *Editor) input(ev ui.InputEvent, read func() string, write func(string), open func(string)) bool {
	e.lastInput = ev.Time
	e.sync()
	if e.structuralPointer(ev) {
		return true
	}
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button != 0 {
			return false
		}
		e.UnmarkText()
		at, ok := e.IndexForPoint(ui.Point{X: ev.X, Y: ev.Y})
		if !ok {
			return false
		}
		if ev.Mods&ui.Cmd != 0 {
			if link := e.linkAt(at); link != "" && open != nil {
				open(link)
				return true
			}
			if name := e.wikiAt(at); name != "" && e.openWiki != nil {
				e.openWiki(name)
				return true
			}
			if tag := e.tagAt(at); tag != "" && e.openTag != nil {
				e.openTag(tag)
				return true
			}
		}
		if e.inlineClick(ev.X, ev.Y) {
			return true
		}
		if !e.ReadOnly && ev.Mods == 0 && ev.Clicks < 2 && e.linkAt(at) != "" {
			e.SetSelection(at, at)
			e.EditLink()
			return true
		}
		if e.toggleTask(at, ev.X) {
			return true
		}
		e.dragging = true
		if ev.Mods&ui.Shift != 0 {
			e.SetSelection(e.Anchor, at)
		} else {
			e.SetSelection(at, at)
		}
		if ev.Clicks == 2 {
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
		if at, ok := e.IndexForPoint(ui.Point{X: ev.X, Y: ev.Y}); ok {
			e.SetSelection(e.Anchor, at)
		}
		e.reveal = true
		return true
	case ui.InputPointerUp:
		if !e.dragging {
			return false
		}
		e.dragging, e.reveal = false, true
		return true
	case ui.InputSelect:
		runes := []rune(e.text)
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

func (e *Editor) command(command string, read func() string, write func(string)) bool {
	e.reveal, e.vertical = true, false
	switch command {
	case "selectAll":
		e.SetSelection(0, e.Len())
	case "copy":
		e.copySelection(write)
	case "cut":
		e.copySelection(write)
		e.ReplaceText(nil, "")
	case "paste":
		e.pasteSelection(read)
	case "pasteAndMatchStyle":
		e.Buffer.ReplaceText(nil, read())
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
	case "delete":
		e.delete(-1, false, false)
	default:
		return false
	}
	return true
}

func (e *Editor) key(ev ui.InputEvent, read func() string, write func(string)) bool {
	mods, key := ev.Mods, ev.Key
	for _, r := range e.reserved {
		if r.Mods == mods && r.Key == key {
			return false
		}
	}
	if e.slashKeys(ev) || e.wikiKeys(ev) {
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
	if e.colorPanel && key == ui.KeyEscape {
		e.colorPanel = false
		return true
	}
	if e.menuOpen && key == ui.KeyEscape {
		e.menuOpen, e.menuSub = false, ""
		return true
	}
	if e.colorOpen || e.langOpen {
		// Any key puts a picker away; Escape does nothing else.
		e.colorOpen, e.langOpen = false, false
		if key == ui.KeyEscape {
			return true
		}
	}
	if _, composing := e.MarkedRange(); composing {
		if key == ui.KeyEscape {
			e.cancelComposition()
			return true
		}
		return false
	}
	switch e.boundCommand(mods, key) {
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
		if mods == ui.Cmd {
			if name, ok := commands[key]; ok {
				return e.command(name, read, write)
			}
		}
		if mods == ui.Cmd|ui.Alt {
			for i, k := range []ui.Key{ui.Key0, ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6} {
				if key == k {
					prefix := ""
					if i > 0 {
						prefix = strings.Repeat("#", i) + " "
					}
					e.Prefix(prefix)
					e.reveal = true
					return true
				}
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
			r, _, ok := e.BoundsForRange(ui.TextInputRange{Start: position, End: position})
			if !ok {
				return false
			}
			if !e.vertical {
				e.preferredX = r.X
			}
			distance := r.H
			if key == ui.KeyPageUp || key == ui.KeyPageDown {
				distance = max(r.H, e.viewport-48)
			}
			if key == ui.KeyUp || key == ui.KeyPageUp {
				distance = -distance
			}
			position, _ = e.IndexForPoint(ui.Point{X: e.preferredX, Y: r.Y + r.H/2 + distance})
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
		e.Engine.Enter(shift)
		e.sync()
		e.reveal, e.vertical = true, false
		return true
	case ui.KeyTab:
		if e.cellTab(shift) {
			return true
		}
		if mods&ui.Ctrl != 0 || mods&ui.Alt != 0 {
			return false
		}
		if mods & ^ui.Shift == 0 && e.columnTab(shift) {
			return true
		}
		if shift || e.Anchor != e.Caret {
			e.indent(shift)
		} else {
			e.ReplaceText(nil, "    ")
		}
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

func (e *Editor) delete(direction int, word, line bool) {
	if e.ReadOnly {
		return
	}
	if direction < 0 && !word && !line && e.deletePair() {
		return
	}
	r := e.Selection().Range
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
	}
	e.ReplaceText(&r, "")
}

func (e *Editor) indent(remove bool) {
	delta := 1
	if remove {
		delta = -1
	}
	e.Engine.Indent(delta)
}

func (e *Editor) selectWord(at int) {
	a, z := e.lineRange(at)
	pos := ui.UTF16ByteOffset(e.text, at)
	start, end := pos, pos
	for start > ui.UTF16ByteOffset(e.text, a) {
		r, n := utf8.DecodeLastRuneInString(e.text[:start])
		if strings.ContainsRune(" \t\r\n", r) {
			break
		}
		start -= n
	}
	for end < ui.UTF16ByteOffset(e.text, z) {
		r, n := utf8.DecodeRuneInString(e.text[end:])
		if strings.ContainsRune(" \t\r\n", r) {
			break
		}
		end += n
	}
	e.SetSelection(ui.UTF16Len(e.text[:start]), ui.UTF16Len(e.text[:end]))
}
