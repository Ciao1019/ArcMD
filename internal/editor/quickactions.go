package editor

import (
	"github.com/Ciao1019/ArcMD/internal/document"
)

// Quick toolbar actions invoked by the host's customizable document toolbar.
// Each mutating method is a no-op when ReadOnly is true, records a single undo
// step, focuses the editor, and reveals the caret.

// InsertTableBlock inserts a rows×cols table at the caret as one undo step.
func (e *Editor) InsertTableBlock(rows, cols int) {
	if e.ReadOnly {
		return
	}
	if rows <= 0 {
		rows = 3
	}
	if cols <= 0 {
		cols = 3
	}
	e.UnmarkText()
	e.clearBlocks()
	e.slashOpen = false
	e.Engine.Insert(document.Table(rows, cols))
	e.sync()
	e.RequestFocus, e.reveal = true, true
}

// InsertCodeBlock converts an empty paragraph at the caret into a code block,
// or inserts a new code block after the current block, as one undo step.
func (e *Editor) InsertCodeBlock() {
	if e.ReadOnly {
		return
	}
	e.UnmarkText()
	e.clearBlocks()
	e.slashOpen = false
	leaf := e.Engine.LeafAt(e.Caret)
	if leaf.Block.Kind == "paragraph" && leaf.Block.Text() == "" {
		e.Engine.Edit(func() {
			leaf.Block.Kind = "code"
			leaf.Block.Runs = nil
			leaf.Block.Comments = nil
			e.Engine.FocusBlock(leaf.Block.ID)
		})
	} else {
		e.Engine.Insert(document.NewBlock("code", ""))
	}
	e.sync()
	e.RequestFocus, e.reveal = true, true
}

// OpenLinkPopover opens the inline link editor popover on the active selection
// or link at the caret, matching the selection toolbar's link button.
func (e *Editor) OpenLinkPopover() {
	if e.ReadOnly {
		return
	}
	e.clearBlocks()
	e.EditLink()
	e.RequestFocus, e.reveal = true, true
}

// InsertMathBlock converts an empty paragraph at the caret into a display math
// block, or inserts a new math block after the current block, as one undo step.
func (e *Editor) InsertMathBlock() {
	if e.ReadOnly {
		return
	}
	e.UnmarkText()
	e.clearBlocks()
	e.slashOpen = false
	leaf := e.Engine.LeafAt(e.Caret)
	if leaf.Block.Kind == "paragraph" && leaf.Block.Text() == "" {
		e.Engine.Edit(func() {
			leaf.Block.Kind = "math"
			leaf.Block.Runs = nil
			leaf.Block.Comments = nil
			e.Engine.FocusBlock(leaf.Block.ID)
		})
	} else {
		e.Engine.Insert(document.NewBlock("math", ""))
	}
	e.sync()
	e.RequestFocus, e.reveal = true, true
}

// InsertRuleBlock inserts a horizontal divider rule at the caret as one undo step.
func (e *Editor) InsertRuleBlock() {
	if e.ReadOnly {
		return
	}
	e.UnmarkText()
	e.clearBlocks()
	e.slashOpen = false
	e.Engine.Insert(document.NewBlock("rule", ""))
	e.sync()
	e.RequestFocus, e.reveal = true, true
}

// InsertColumnsBlock inserts a two-column layout at the caret as one undo step.
func (e *Editor) InsertColumnsBlock() {
	if e.ReadOnly {
		return
	}
	e.UnmarkText()
	e.clearBlocks()
	e.slashOpen = false
	e.Engine.Insert(document.Columns())
	e.sync()
	e.RequestFocus, e.reveal = true, true
}

// InsertToggleBlock turns a paragraph into an open toggle block (with an
// indented child line) or inserts a new open toggle block, as one undo step.
func (e *Editor) InsertToggleBlock() {
	if e.ReadOnly {
		return
	}
	e.UnmarkText()
	e.clearBlocks()
	e.slashOpen = false
	e.Engine.Edit(func() {
		leaf := e.Engine.LeafAt(e.Caret)
		if leaf.Block.Kind == "paragraph" {
			e.Engine.SetKind("toggle", 0)
			e.Engine.OpenToggle(leaf.Block.ID)
		} else {
			b := document.NewBlock("toggle", "")
			e.Engine.Insert(b)
			e.Engine.OpenToggle(b.ID)
		}
	})
	e.sync()
	e.RequestFocus, e.reveal = true, true
}

// InsertCalloutBlock turns a paragraph or quote into a callout block, or
// inserts a new callout block after the current block, as one undo step.
func (e *Editor) InsertCalloutBlock() {
	if e.ReadOnly {
		return
	}
	e.UnmarkText()
	e.clearBlocks()
	e.slashOpen = false
	e.Engine.Edit(func() {
		leaf := e.Engine.LeafAt(e.Caret)
		if leaf.Block.Kind == "paragraph" || leaf.Block.Kind == "quote" {
			e.Engine.SetKind("callout", 0)
		} else {
			e.Engine.Insert(document.NewBlock("callout", ""))
		}
	})
	e.sync()
	e.RequestFocus, e.reveal = true, true
}

// SetBlockKind sets the block kind (and level for "heading") on the active
// multi-block selection or caret/text selection as one undo step.
func (e *Editor) SetBlockKind(kind string, level int) {
	if e.ReadOnly {
		return
	}
	if kind == "heading" {
		level = max(1, min(6, level))
	} else {
		level = 0
	}
	e.UnmarkText()
	e.slashOpen = false
	if len(e.blocks.ids) > 0 {
		e.Engine.SetBlocksKind(e.BlockSelection(), kind, level)
	} else {
		e.Engine.SetKind(kind, level)
	}
	e.sync()
	e.RequestFocus, e.reveal = true, true
}

// ToggleQuickAnnotation toggles the active/last hand-drawn annotation on a
// non-empty selection (defaulting to "wavy"), opens the mark popover when a
// collapsed caret rests on annotated text, or toggles the typing annotation.
func (e *Editor) ToggleQuickAnnotation() {
	if e.ReadOnly {
		return
	}
	e.UnmarkText()
	e.clearBlocks()
	variant := e.lastAnnotation
	if variant == "" {
		variant = "wavy"
	}
	sel := e.Selection().Range
	if sel.Start != sel.End {
		if e.Engine.CurrentStyle().Annotation != "" {
			e.Annotate("", "")
		} else {
			e.Annotate(variant, e.lastAnnotationColor)
		}
		e.sync()
		e.RequestFocus, e.reveal = true, true
		return
	}
	if _, ok := e.annotationAt(sel.Start); ok {
		if e.markPop.open {
			e.markPop = markPopover{}
		} else {
			e.openMarkPopover(sel.Start)
		}
		e.RequestFocus, e.reveal = true, true
		return
	}
	if e.Engine.CurrentStyle().Annotation != "" {
		e.Annotate("", "")
	} else {
		e.Annotate(variant, e.lastAnnotationColor)
	}
	e.sync()
	e.RequestFocus, e.reveal = true, true
}
