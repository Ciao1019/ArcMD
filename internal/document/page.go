package document

import "strings"

// SetPageIcon gives the page an icon, an emoji, or none, as one step of the
// history.
func (e *Engine) SetPageIcon(icon string) {
	e.Edit(func() { e.Doc.Icon = strings.TrimSpace(icon) })
}

// SetCover gives the page a cover, or none: a gradient as
// "gradient:name" or a picture beside the document. A cover the format
// cannot hold is ignored.
func (e *Engine) SetCover(cover string) {
	if !ValidCover(cover) {
		return
	}
	e.Edit(func() { e.Doc.Cover, e.Doc.CoverY = cover, 0 })
}

// SetCoverY shows another part of the picture cover, from -1 its top to 1
// its bottom, as one step of the history.
func (e *Engine) SetCoverY(y float64) {
	y = min(max(y, -1), 1)
	if y != y || y == e.Doc.CoverY {
		return
	}
	e.Edit(func() { e.Doc.CoverY = y })
}

// SetFrontmatter updates the document's preserved user YAML frontmatter as
// one step of the history.
func (e *Engine) SetFrontmatter(fm string) {
	trimmed := strings.TrimRight(fm, "\r\n")
	if trimmed == e.Doc.Frontmatter {
		return
	}
	e.Edit(func() { e.Doc.Frontmatter = trimmed })
}

// ReplaceDocument puts the blocks, icon and cover of doc in place of the
// document's, as one step of the history, as restoring an earlier version
// does; the caret goes to the start.
func (e *Engine) ReplaceDocument(doc *Document) {
	fresh := doc.Clone()
	e.Edit(func() {
		e.Doc.Blocks, e.Doc.Icon, e.Doc.Cover, e.Doc.CoverY = fresh.Blocks, fresh.Icon, fresh.Cover, fresh.CoverY
		e.Doc.Goal, e.Doc.Frontmatter, e.Doc.ImportSource = fresh.Goal, fresh.Frontmatter, fresh.ImportSource
		if len(e.Doc.Blocks) == 0 {
			e.Doc.Blocks = []*Block{NewBlock("paragraph", "")}
		}
		e.SetSelection(0, 0)
	})
}

// MapText runs change over the text of every run of the document, as one
// step of the history, and reports whether anything changed: links that
// follow a renamed document.
func (e *Engine) MapText(change func(string) string) bool {
	before := e.version
	e.Edit(func() {
		var walk func([]*Block)
		walk = func(blocks []*Block) {
			for _, b := range blocks {
				MapBlockRunText(b, nil, change)
				walk(b.Children)
			}
		}
		walk(e.Doc.Blocks)
		a, z := e.Anchor, e.Caret
		e.SetSelection(a, z)
	})
	return e.version != before
}

// Section is the top-level blocks a heading heads: the heading at index i
// and those after it up to the next heading of its level or higher. It
// returns the end, exclusive.
func (d *Document) Section(i int) int {
	if i < 0 || i >= len(d.Blocks) || d.Blocks[i].Kind != "heading" {
		return i + 1
	}
	level := max(1, d.Blocks[i].Level)
	j := i + 1
	for j < len(d.Blocks) {
		b := d.Blocks[j]
		if b.Kind == "heading" && max(1, b.Level) <= level {
			break
		}
		j++
	}
	return j
}

// HasPageTitle reports whether the document's first block is a level-1 page title.
func (d *Document) HasPageTitle() bool {
	return len(d.Blocks) > 0 && d.Blocks[0].Kind == "heading" && d.Blocks[0].Level <= 1
}

// MoveSection moves the heading with the ID and its section before the
// top-level block with the ID before ("" for the end), as one step of the
// history: the outline's drag. A move into the section itself does
// nothing; a level-1 page title (the first block) stays first.
func (e *Engine) MoveSection(id, before string) {
	from := e.topIndex(id)
	if from < 0 || e.Doc.Blocks[from].Kind != "heading" || (from == 0 && e.Doc.HasPageTitle()) {
		return
	}
	end := e.Doc.Section(from)
	to := len(e.Doc.Blocks)
	if before != "" {
		to = e.topIndex(before)
		if to < 0 {
			return
		}
	}
	if to >= from && to <= end || to == 0 && e.Doc.HasPageTitle() {
		return
	}
	e.Edit(func() {
		blocks := e.Doc.Blocks
		moving := append([]*Block(nil), blocks[from:end]...)
		rest := append(append([]*Block(nil), blocks[:from]...), blocks[end:]...)
		if to > from {
			to -= end - from
		}
		e.Doc.Blocks = append(append(append([]*Block(nil), rest[:to]...), moving...), rest[to:]...)
		e.FocusBlock(id)
	})
}

// SetGoal sets how many words the page means to reach, as one step of the
// history: 0 takes the goal away.
func (e *Engine) SetGoal(words int) {
	if words < 0 || words > 1000000 {
		return
	}
	e.Edit(func() { e.Doc.Goal = words })
}

// MapProse runs change over the text of every run that is prose, not
// code, a formula or source kept as written, as one step of the history,
// and reports whether anything changed: tags that are renamed.
func (e *Engine) MapProse(change func(string) string) bool {
	before := e.version
	e.Edit(func() {
		prose := func(r Run) bool { return !r.Style.Code && !r.Style.Kbd }
		var walk func([]*Block)
		walk = func(blocks []*Block) {
			for _, b := range blocks {
				if !isVerbatim(b) {
					MapBlockRunText(b, prose, change)
				}
				walk(b.Children)
			}
		}
		walk(e.Doc.Blocks)
		a, z := e.Anchor, e.Caret
		e.SetSelection(a, z)
	})
	return e.version != before
}
