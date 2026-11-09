package editor

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/ui"
)

type Buffer struct {
	*document.Engine
	text        string
	textDoc     *document.Document
	textVersion uint64
	textLength  int
	mark        *ui.TextInputRange
	composition *document.Snapshot
}

func newBuffer(doc *document.Document) *Buffer {
	b := &Buffer{Engine: document.NewEngine(doc)}
	b.sync()
	return b
}
func (b *Buffer) sync() {
	if b.textDoc == b.Doc && b.textVersion == b.Revision() {
		return
	}
	if change, ok := b.TextChangeSince(b.textVersion); ok && b.textDoc == b.Doc {
		start := document.ByteOffset(b.text, change.Start)
		b.text = b.text[:start] + change.After + b.text[start+len(change.Before):]
		b.textLength += change.AfterEnd - change.BeforeEnd
	} else {
		b.text = b.Doc.PlainText()
		b.textLength = document.Len(b.text)
	}
	b.textDoc, b.textVersion = b.Doc, b.Revision()
}
func (b *Buffer) Text() string {
	if b.composition != nil {
		return b.composition.Doc.PlainText()
	}
	b.sync()
	return b.text
}
func (b *Buffer) Committed() *document.Document {
	if b.composition != nil {
		return b.composition.Doc
	}
	return b.Doc
}
func (b *Buffer) Len() int { b.sync(); return b.textLength }
func (b *Buffer) Selection() ui.TextInputSelection {
	return ui.TextInputSelection{Range: ui.TextInputRange{Start: min(b.Anchor, b.Caret), End: max(b.Anchor, b.Caret)}, Reversed: b.Caret < b.Anchor}
}
func (b *Buffer) MarkedRange() (ui.TextInputRange, bool) {
	if b.mark != nil {
		return *b.mark, true
	}
	return ui.TextInputRange{}, false
}
func (b *Buffer) TextForRange(r ui.TextInputRange) (string, ui.TextInputRange) {
	b.sync()
	r = b.validRange(r)
	return b.slice(r.Start, r.End), r
}
func (b *Buffer) clamp(at int) int { return ui.UTF16Len(b.text[:ui.UTF16ByteOffset(b.text, at)]) }
func (b *Buffer) validRange(r ui.TextInputRange) ui.TextInputRange {
	return ui.TextInputRange{Start: b.clamp(min(r.Start, r.End)), End: b.clamp(max(r.Start, r.End))}
}
func (b *Buffer) slice(a, z int) string {
	return b.text[ui.UTF16ByteOffset(b.text, a):ui.UTF16ByteOffset(b.text, z)]
}
func (b *Buffer) ReplaceText(r *ui.TextInputRange, text string) {
	b.sync()
	target := b.Selection().Range
	if b.mark != nil {
		target = *b.mark
	}
	if r != nil {
		target = b.validRange(*r)
	}
	if b.composition != nil {
		preview := b.Engine.Capture()
		b.Engine.Restore(*b.composition)
		b.composition, b.mark = nil, nil
		b.Engine.Edit(func() {
			b.Engine.Restore(preview)
			b.Engine.Replace(target.Start, target.End, text)
		})
	} else {
		b.Engine.Replace(target.Start, target.End, text)
	}
	b.sync()
}
func (b *Buffer) SetMarkedText(r *ui.TextInputRange, text string, selected ui.TextInputRange) {
	b.sync()
	target := b.Selection().Range
	if b.mark != nil {
		target = *b.mark
	}
	if r != nil {
		target = b.validRange(*r)
	}
	if b.composition == nil {
		before := b.Engine.Capture()
		b.composition = &before
	}
	b.Engine.PreviewReplace(target.Start, target.End, text)
	marked := ui.TextInputRange{Start: target.Start, End: target.Start + ui.UTF16Len(text)}
	b.mark = &marked
	b.Engine.SetSelection(marked.Start+max(0, min(selected.Start, ui.UTF16Len(text))), marked.Start+max(0, min(selected.End, ui.UTF16Len(text))))
	b.sync()
}
func (b *Buffer) UnmarkText() {
	if b.mark != nil {
		text := b.slice(b.mark.Start, b.mark.End)
		b.ReplaceText(nil, text)
	}
}
func (b *Buffer) cancelComposition() {
	if b.composition != nil {
		b.Engine.Restore(*b.composition)
	}
	b.composition, b.mark = nil, nil
	b.sync()
}
func (b *Buffer) Undo() {
	if b.composition != nil {
		b.cancelComposition()
		return
	}
	b.Engine.Undo()
	b.sync()
}
func (b *Buffer) Redo() { b.UnmarkText(); b.Engine.Redo(); b.sync() }
func (b *Buffer) Wrap(marker string) {
	b.UnmarkText()
	marks := map[string]string{"**": "bold", "*": "italic", "~~": "strike", "`": "code", "_": "underline"}
	b.Engine.Toggle(marks[marker])
	b.sync()
}
func (b *Buffer) Prefix(prefix string) {
	b.UnmarkText()
	kind, level := "paragraph", 0
	switch prefix {
	case "- ":
		kind = "bullet"
	case "1. ":
		kind = "numbered"
	case "- [ ] ":
		kind = "task"
	case "> ":
		kind = "quote"
	default:
		if strings.HasPrefix(prefix, "#") {
			kind, level = "heading", len(strings.TrimSpace(prefix))
		}
	}
	b.Engine.SetKind(kind, level)
	b.sync()
}
func (b *Buffer) newline() { b.UnmarkText(); b.Engine.Enter(false); b.sync() }

func (b *Buffer) lineRange(at int) (int, int) {
	i := ui.UTF16ByteOffset(b.text, at)
	a := strings.LastIndexByte(b.text[:i], '\n') + 1
	z := len(b.text)
	if n := strings.IndexByte(b.text[i:], '\n'); n >= 0 {
		z = i + n
	}
	if z > a && b.text[z-1] == '\r' {
		z--
	}
	return ui.UTF16Len(b.text[:a]), ui.UTF16Len(b.text[:z])
}

func (b *Buffer) boundary(at, direction int) int {
	a, z := b.lineRange(at)
	if direction < 0 && at <= a {
		if a >= 2 && b.slice(a-2, a) == "\r\n" {
			return a - 2
		}
		return max(0, a-1)
	}
	if direction > 0 && at >= z {
		if z+2 <= b.Len() && b.slice(z, z+2) == "\r\n" {
			return z + 2
		}
		return min(b.Len(), z+1)
	}
	line := ui.ShapeText(b.slice(a, z), ui.Font{Size: 16}, 0)
	if direction < 0 {
		return a + line.PreviousBoundary(at-a)
	}
	return a + line.NextBoundary(at-a)
}

func (b *Buffer) wordBoundary(at, direction int) int {
	pos := ui.UTF16ByteOffset(b.text, at)
	if direction < 0 {
		for pos > 0 {
			r, n := utf8.DecodeLastRuneInString(b.text[:pos])
			if !unicode.IsSpace(r) {
				break
			}
			pos -= n
		}
		for pos > 0 {
			r, n := utf8.DecodeLastRuneInString(b.text[:pos])
			if unicode.IsSpace(r) {
				break
			}
			pos -= n
		}
	} else {
		for pos < len(b.text) {
			r, n := utf8.DecodeRuneInString(b.text[pos:])
			if unicode.IsSpace(r) {
				break
			}
			pos += n
		}
		for pos < len(b.text) {
			r, n := utf8.DecodeRuneInString(b.text[pos:])
			if !unicode.IsSpace(r) {
				break
			}
			pos += n
		}
	}
	return ui.UTF16Len(b.text[:pos])
}
