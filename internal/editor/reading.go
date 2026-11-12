package editor

// SetReadOnly locks this editor instance, including commands and input methods.
// It cancels provisional composition and leaves copy, find and navigation alive.
func (e *Editor) SetReadOnly(on bool) {
	if on {
		e.cancelComposition()
	}
	e.ReadOnly = on
	e.ready = false
	e.slashOpen, e.linkOpen, e.menuOpen, e.colorOpen, e.langOpen = false, false, false, false, false
	e.colorPanel, e.cellFillOpen = false, false
	e.find.replace = false
	e.pairs = nil
	e.dragID = ""
	e.dragging = false
	e.resizing = nil
	e.imageResize = imageResize{}
	e.imageCaptionID = ""
	e.closeHeader()
}

func (e *Editor) InsertInlineMath() {
	if e.ReadOnly {
		return
	}
	e.UnmarkText()
	a, z := min(e.Anchor, e.Caret), max(e.Anchor, e.Caret)
	e.Engine.Edit(func() { e.Engine.Replace(z, z, "$"); e.Engine.Replace(a, a, "$"); e.SetSelection(a+1, z+1) })
	e.sync()
	e.RequestFocus, e.reveal = true, true
}

// InsertFootnote retains the new reference as the definition's return target.
func (e *Editor) InsertFootnote() {
	if e.ReadOnly {
		return
	}
	e.UnmarkText()
	at := min(e.Anchor, e.Caret)
	leaf := e.LeafAt(at)
	e.noteOriginID, e.noteOriginOffset = leaf.Block.ID, at-leaf.Start
	e.noteReturn, e.noteHasReturn = at, true
	e.Engine.InsertFootnote()
	e.sync()
	e.RequestFocus, e.reveal = true, true
}
