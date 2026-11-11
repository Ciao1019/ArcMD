package document

import "strings"

// enterMath recognizes display fences without reparsing the document or
// changing ordinary Enter inside the formula's multiline source.
func (e *Engine) enterMath(leaf Leaf) bool {
	if e.Anchor != e.Caret || e.Caret != leaf.End || e.CurrentStyle().Code {
		return false
	}
	b := leaf.Block
	source := b.Text()
	if b.Kind == "paragraph" && source == "$$" {
		e.Edit(func() {
			b.Kind, b.Runs = "math", nil
			e.Anchor, e.Caret = leaf.Start, leaf.Start
			e.Typing, e.Explicit = Style{}, false
		})
		return true
	}
	if b.Kind != "math" || !strings.HasSuffix(source, "\n$$") && source != "$$" {
		return false
	}
	e.Edit(func() {
		body := strings.TrimSuffix(strings.TrimSuffix(source, "$$"), "\n")
		b.Runs = SliceRuns(b.Runs, 0, Len(body))
		next := NewBlock("paragraph", "")
		parent, index := leaf.Parent, leaf.Index+1
		*parent = append(append(append([]*Block(nil), (*parent)[:index]...), next), (*parent)[index:]...)
		e.FocusBlock(next.ID)
	})
	return true
}
