package editor

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// BoardTool selects the active pointer interaction on a whiteboard.
type BoardTool uint8

const (
	ToolSelect BoardTool = iota
	ToolHand
	ToolRect
	ToolDiamond
	ToolEllipse
	ToolArrow
	ToolLine
	ToolFreedraw
	ToolText
	ToolNote
	ToolFrame
	ToolEraser
	ToolTriangle
	ToolParallelogram
	ToolHexagon
	ToolCylinder
	ToolCloud
	ToolStar
	ToolCallout
	ToolDocument
	ToolStadium
	ToolActor
	ToolLaser
)

// BoardOptions configures BoardView and Board.Corner.
type BoardOptions struct {
	Theme             *ui.Theme
	Page              Page
	Compact           bool // true when embedded inline inside a Markdown document
	ReadOnly          bool
	WikiSuggest       func(query string) []string
	OpenWiki          func(name string, split bool)
	LinkedMentions    func() []string // other documents mentioning this whiteboard
	UserLibrary       func() []whiteboard.LibraryItem
	SaveUserLibrary   func([]whiteboard.LibraryItem)
	ImportLibraryFile func(onLoad func([]whiteboard.LibraryItem))
	SaveAsFile        func()
	ExportSVG         func()
	ExpandModal       func()
	Done              func()
	OnChange          func()
}

type laserSample struct {
	x, y float32
	at   time.Time
}

// Board holds the interactive Excalidraw-style whiteboard state and caches.
type Board struct {
	Scene      *whiteboard.Scene
	History    whiteboard.History
	ReadOnly   bool
	Tool       BoardTool
	ToolLocked bool
	ShowGrid   bool

	// Active drawing defaults applied to newly created elements and shown in the style panel.
	StrokeColor    string
	FillColor      string
	FillStyle      whiteboard.FillStyle
	StrokeWidth    float32
	StrokeStyle    whiteboard.StrokeStyle
	Roughness      int
	Roundness      bool
	Opacity        int
	FontFamily     string
	FontSize       float32
	TextAlign      string
	ArrowRouting   string // "sharp", "curved", "elbow"
	StartArrowhead whiteboard.Arrowhead
	EndArrowhead   whiteboard.Arrowhead

	// Viewport state in DIPs.
	PanX, PanY         float32
	Zoom               float32
	fitted             bool
	userViewport       bool
	pendingFit         bool
	autoFitW, autoFitH float32
	lastRect           ui.Rect

	RequestFocus bool

	// Selection & manipulation state.
	Selected            map[string]bool
	dragMode            boardDragMode
	dragStartScene      [2]float32
	dragLastScene       [2]float32
	dragStartScreen     [2]float32
	dragMoved           bool
	dragHandle          whiteboard.HandleKind
	resizeOrig          *whiteboard.Element
	resizeTargetID      string
	resizeOrigGroup     map[string]*whiteboard.Element
	resizeOrigBounds    whiteboard.Box
	creatingID          string
	marquee             *whiteboard.Box
	guides              []whiteboard.SnapGuide
	bindPreviewID       string
	hoverScene          [2]float32
	hoverValid          bool
	hoverMovedSinceCopy bool
	pasteCount          int
	spaceDown           bool
	panning             bool

	// Extended tool panels & laser pointer state.
	shapesPopoverOpen bool
	libraryOpen       bool
	libraryCat        string
	mermaidOpen       bool
	mermaidInput      string
	mermaidErr        string
	laserTrail        []laserSample

	// Text editing state (double-click on canvas or inside shape).
	editingID        string
	editText         string
	editBefore       *whiteboard.Scene
	editJustOpened   bool
	editWasFocused   bool
	editWikiOpen     bool
	editWikiQuery    string
	editWikiIndex    int
	linkPopoverOpen  bool
	linkSearchQuery  string
	linksPanelOpen   bool
	contextElementID string

	// Highlight pulse for RevealElement (from search / backlinks / QA citations).
	revealID string
	revealAt time.Time

	// Link pill and group action pill hit regions collected during the most recent frame's paint.
	linkPills []boardLinkPill
	groupPill boardGroupPill

	// Group isolation editing state ("组内编辑").
	editingGroupID string

	// Compiled path and text caches.
	pathCache map[string]cachedBoardElem
	textCache map[string]*ui.TextLayout

	version  uint64
	revision uint64
}

type boardDragMode uint8

const (
	dragNone boardDragMode = iota
	dragPan
	dragMove
	dragResize
	dragCreate
	dragMarquee
	dragErase
	dragLaser
)

type boardLinkPill struct {
	elementID string
	target    string
	rect      ui.Rect
}

type boardGroupPill struct {
	groupID string
	exiting bool
	rect    ui.Rect
}

type compiledBoardPrim struct {
	path        *ui.Path
	strokeColor string
	fillColor   string
	strokeWidth float32
	opacity     float32
	filled      bool
	stroked     bool
	isHachure   bool
}

type cachedBoardElem struct {
	key   boardElemKey
	prims []compiledBoardPrim
}

type boardElemKey struct {
	kind        whiteboard.ElementKind
	version     int
	x, y, w, h  float32
	strokeColor string
	fillColor   string
	fillStyle   whiteboard.FillStyle
	strokeWidth float32
	strokeStyle whiteboard.StrokeStyle
	roughness   int
	roundness   bool
	opacity     int
	smooth      bool
	elbowed     bool
	startHead   whiteboard.Arrowhead
	endHead     whiteboard.Arrowhead
	hasText     bool
	seed        uint32
	ptsLen      int
}

// NewBoard creates an interactive Board wrapping scene.
func NewBoard(scene *whiteboard.Scene) *Board {
	if scene == nil {
		scene = whiteboard.NewScene()
	}
	zoom := scene.AppState.Zoom
	if zoom <= 0 {
		zoom = 1
	}
	return &Board{
		Scene:        scene,
		History:      whiteboard.NewHistory(),
		Tool:         ToolSelect,
		ShowGrid:     true,
		StrokeColor:  "ink",
		FillColor:    "blue",
		FillStyle:    whiteboard.FillHachure,
		StrokeWidth:  2,
		StrokeStyle:  whiteboard.StrokeSolid,
		Roughness:    1,
		Roundness:    true,
		Opacity:      100,
		FontFamily:   "hand",
		FontSize:     18,
		TextAlign:    "center",
		ArrowRouting: "sharp",
		EndArrowhead: whiteboard.ArrowOpen,
		PanX:         scene.AppState.ScrollX,
		PanY:         scene.AppState.ScrollY,
		Zoom:         clampZoom(zoom),
		Selected:     map[string]bool{},
		pathCache:    map[string]cachedBoardElem{},
		textCache:    map[string]*ui.TextLayout{},
		version:      1,
		revision:     1,
	}
}

// Version increments on committed edits and undo/redo steps.
func (b *Board) Version() uint64 { return b.version }

// Revision increments on every live change (including ongoing drags).
func (b *Board) Revision() uint64 { return b.revision }

// SceneSnapshot returns a safe clone of the current scene with viewport metadata.
func (b *Board) SceneSnapshot() *whiteboard.Scene {
	s := b.Scene.Clone()
	s.AppState.Zoom = b.Zoom
	s.AppState.ScrollX = b.PanX
	s.AppState.ScrollY = b.PanY
	return s
}

// ReplaceScene replaces the scene (e.g., external reload or version restore).
func (b *Board) ReplaceScene(scene *whiteboard.Scene, recordUndo bool) {
	if scene == nil {
		scene = whiteboard.NewScene()
	}
	if recordUndo {
		b.History.Record(b.Scene)
	}
	b.Scene = scene.Clone()
	b.Selected = map[string]bool{}
	b.editingID = ""
	b.editingGroupID = ""
	b.pathCache = map[string]cachedBoardElem{}
	b.bump(true)
}

// PreserveViewport copies pan, zoom, and fitted state from another board.
func (b *Board) PreserveViewport(from *Board) {
	if b == nil || from == nil {
		return
	}
	b.PanX = from.PanX
	b.PanY = from.PanY
	b.Zoom = from.Zoom
	b.fitted = from.fitted
}

// RewriteWikiLinks updates [[from]] links across elements, recording an undo step.
func (b *Board) RewriteWikiLinks(from, to string) bool {
	if b == nil || b.Scene == nil {
		return false
	}
	before := b.Scene.Clone()
	if !b.Scene.RewriteWikiLinks(from, to) {
		return false
	}
	b.History.Record(before)
	b.pathCache = map[string]cachedBoardElem{}
	b.bump(true)
	return true
}

// RewriteTags renames #from tags across element text, recording an undo step.
func (b *Board) RewriteTags(from, to string) bool {
	if b == nil || b.Scene == nil {
		return false
	}
	before := b.Scene.Clone()
	if !b.Scene.RewriteTags(from, to) {
		return false
	}
	b.History.Record(before)
	b.pathCache = map[string]cachedBoardElem{}
	b.bump(true)
	return true
}

// MapText applies fn to every element's text, recording one undo step if changed.
func (b *Board) MapText(fn func(string) string) bool {
	if b == nil || b.Scene == nil || fn == nil {
		return false
	}
	before := b.Scene.Clone()
	changed := false
	for _, el := range b.Scene.Elements {
		if el == nil || el.Text == "" {
			continue
		}
		if next := fn(el.Text); next != el.Text {
			el.Text = next
			el.Version++
			changed = true
		}
	}
	if !changed {
		return false
	}
	b.History.Record(before)
	b.pathCache = map[string]cachedBoardElem{}
	b.bump(true)
	return true
}

func (b *Board) bump(committed bool) {
	b.revision++
	if committed {
		b.version++
	}
}

// RequestFit schedules a one-time FitToContent on the next valid canvas layout.
func (b *Board) RequestFit() {
	b.pendingFit = true
	b.userViewport = false
}

func (b *Board) ensureViewportFit(w, h float32) {
	if w < 160 || h < 120 || b.dragMode != dragNone || b.editingID != "" {
		return
	}
	if len(b.Scene.Elements) == 0 {
		if !b.fitted || b.pendingFit {
			b.fitted = true
			b.pendingFit = false
			b.autoFitW, b.autoFitH = w, h
		}
		return
	}
	if b.pendingFit || !b.fitted || (!b.userViewport && (absf(w-b.autoFitW) > 16 || absf(h-b.autoFitH) > 16)) {
		b.FitToContent(ui.Rect{W: w, H: h})
		b.pendingFit = false
		b.autoFitW, b.autoFitH = w, h
	}
}

// FitToContent centers and scales the scene inside rect (or lastRect when empty).
func (b *Board) FitToContent(rect ui.Rect) {
	if rect.W <= 0 || rect.H <= 0 {
		rect = b.lastRect
	}
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	bounds := whiteboard.SceneBounds(b.Scene)
	if bounds.Empty() {
		b.Zoom = 1
		b.PanX = 0
		b.PanY = 0
		b.fitted = true
		return
	}
	pad := float32(64)
	availW := max(40, rect.W-2*pad)
	availH := max(40, rect.H-2*pad)
	z := min(availW/bounds.W, availH/bounds.H)
	z = clampZoom(min(1.15, z))
	b.Zoom = z
	b.PanX = pad + (availW-bounds.W*z)/2 - bounds.X*z
	b.PanY = (rect.H-bounds.H*z)/2 - bounds.Y*z
	b.fitted = true
}

// RevealElement selects elID, centers it in the viewport, and flashes a soft highlight.
func (b *Board) RevealElement(elID string) {
	el := b.Scene.ElementByID(elID)
	if el == nil {
		return
	}
	b.Selected = map[string]bool{elID: true}
	eb := whiteboard.ElementBounds(el)
	cx, cy := eb.Center()
	if b.lastRect.W > 0 && b.lastRect.H > 0 {
		b.PanX = b.lastRect.W/2 - cx*b.Zoom
		b.PanY = b.lastRect.H/2 - cy*b.Zoom
		b.fitted = true
	}
	b.revealID = elID
	b.revealAt = time.Now()
}

// InsertDocCardAt creates a hand-drawn sticky note card linked to [[title]] at
// local canvas coordinates (lx, ly).
func (b *Board) InsertDocCardAt(lx, ly float32, title, excerpt string) *whiteboard.Element {
	if b.ReadOnly {
		return nil
	}
	b.userViewport = true
	sx, sy := b.localToScene(lx, ly)
	b.History.Record(b.Scene)
	card := whiteboard.NewElement(whiteboard.KindNote, sx-110, sy-65, 220, 130)
	card.StrokeColor = "blue"
	card.FillColor = "blue"
	card.FillStyle = whiteboard.FillHachure
	card.Link = "[[" + title + "]]"
	body := "[[" + title + "]]"
	if ex := strings.TrimSpace(excerpt); ex != "" {
		body += "\n" + ex
	}
	card.Text = body
	b.Scene.Elements = append(b.Scene.Elements, card)
	b.Selected = map[string]bool{card.ID: true}
	b.bump(true)
	return card
}

func (b *Board) localToScene(lx, ly float32) (float32, float32) {
	z := b.Zoom
	if z <= 0 {
		z = 1
	}
	return (lx - b.PanX) / z, (ly - b.PanY) / z
}

func (b *Board) sceneToScreen(rect ui.Rect, sx, sy float32) (float32, float32) {
	z := b.Zoom
	if z <= 0 {
		z = 1
	}
	return rect.X + b.PanX + sx*z, rect.Y + b.PanY + sy*z
}

// Undo reverts the last whiteboard edit.
func (b *Board) Undo() bool {
	if b.ReadOnly {
		return false
	}
	prev, ok := b.History.Undo(b.Scene)
	if !ok {
		return false
	}
	b.Scene = prev
	b.pruneSelection()
	b.editingID = ""
	b.bump(true)
	return true
}

// Redo reapplies an undone whiteboard edit.
func (b *Board) Redo() bool {
	if b.ReadOnly {
		return false
	}
	next, ok := b.History.Redo(b.Scene)
	if !ok {
		return false
	}
	b.Scene = next
	b.pruneSelection()
	b.editingID = ""
	b.bump(true)
	return true
}

func (b *Board) pruneSelection() {
	for id := range b.Selected {
		if b.Scene.ElementByID(id) == nil {
			delete(b.Selected, id)
		}
	}
	b.pruneEditingGroup()
}

func (b *Board) pruneEditingGroup() {
	if b.editingGroupID != "" && len(whiteboard.GroupElementsList(b.Scene, b.editingGroupID)) < 2 {
		b.editingGroupID = ""
	}
}

func (b *Board) enterGroup(gid, selectID string) {
	if b.ReadOnly || gid == "" {
		return
	}
	members := whiteboard.GroupElementsList(b.Scene, gid)
	if len(members) < 2 {
		return
	}
	b.editingGroupID = gid
	if selectID != "" && b.Scene.ElementByID(selectID) != nil {
		b.Selected = whiteboard.ExpandGroupSelectionWithin(b.Scene, map[string]bool{selectID: true}, gid)
	} else {
		b.Selected = map[string]bool{}
	}
	b.RequestFocus = true
}

func (b *Board) exitGroup(reselectGroup bool) {
	if b.editingGroupID == "" {
		return
	}
	exited := b.editingGroupID
	b.editingGroupID = whiteboard.ParentGroupOf(b.Scene, exited)
	if reselectGroup {
		members := whiteboard.GroupElementsList(b.Scene, exited)
		b.Selected = make(map[string]bool, len(members))
		for _, m := range members {
			b.Selected[m.ID] = true
		}
	} else {
		b.Selected = map[string]bool{}
	}
	b.RequestFocus = true
}

func (b *Board) selectedList() []*whiteboard.Element {
	var out []*whiteboard.Element
	for _, el := range b.Scene.Elements {
		if el != nil && b.Selected[el.ID] {
			out = append(out, el)
		}
	}
	return out
}

func (b *Board) deleteSelected() bool {
	return b.deleteElementIDs(b.Selected)
}

func (b *Board) deleteElementIDs(ids map[string]bool) bool {
	if b.ReadOnly || len(ids) == 0 {
		return false
	}
	b.History.Record(b.Scene)
	var kept []*whiteboard.Element
	for _, el := range b.Scene.Elements {
		if el == nil || ids[el.ID] {
			continue
		}
		if el.StartBinding != nil && ids[el.StartBinding.ElementID] {
			el.StartBinding = nil
		}
		if el.EndBinding != nil && ids[el.EndBinding.ElementID] {
			el.EndBinding = nil
		}
		if el.FrameID != "" && ids[el.FrameID] {
			el.FrameID = ""
		}
		kept = append(kept, el)
	}
	b.Scene.Elements = kept
	b.Selected = map[string]bool{}
	b.pruneEditingGroup()
	b.bump(true)
	return true
}

func (b *Board) selectedWithFrameChildren() ([]*whiteboard.Element, map[string]bool) {
	selectedFrames := map[string]bool{}
	ids := make(map[string]bool, len(b.Selected))
	for _, el := range b.selectedList() {
		ids[el.ID] = true
		if el.Kind == whiteboard.KindFrame {
			selectedFrames[el.ID] = true
		}
	}
	if len(selectedFrames) > 0 {
		for _, el := range b.Scene.Elements {
			if el != nil && el.FrameID != "" && selectedFrames[el.FrameID] {
				ids[el.ID] = true
			}
		}
	}
	var out []*whiteboard.Element
	for _, el := range b.Scene.Elements {
		if el != nil && ids[el.ID] {
			out = append(out, el)
		}
	}
	return out, ids
}

func (b *Board) copySelectedWithIDs(writeClip func(string)) (map[string]bool, bool) {
	if writeClip == nil || len(b.Selected) == 0 {
		return nil, false
	}
	items, ids := b.selectedWithFrameChildren()
	if len(items) == 0 {
		return nil, false
	}
	encoded, err := whiteboard.EncodeClipboard(items)
	if err != nil {
		return nil, false
	}
	writeClip(encoded)
	b.pasteCount = 0
	b.hoverMovedSinceCopy = false
	return ids, true
}

func (b *Board) copySelected(writeClip func(string)) bool {
	_, ok := b.copySelectedWithIDs(writeClip)
	return ok
}

func (b *Board) cutSelected(writeClip func(string), o BoardOptions) bool {
	if b.ReadOnly {
		return false
	}
	ids, ok := b.copySelectedWithIDs(writeClip)
	if !ok {
		return false
	}
	if b.deleteElementIDs(ids) && o.OnChange != nil {
		o.OnChange()
	}
	return true
}

func (b *Board) appendPastedElements(elems []*whiteboard.Element, o BoardOptions) {
	b.History.Record(b.Scene)
	b.Scene.Elements = append(b.Scene.Elements, elems...)
	b.Selected = make(map[string]bool, len(elems))
	for _, el := range elems {
		if el != nil {
			b.Selected[el.ID] = true
		}
	}
	b.userViewport = true
	b.Tool = ToolSelect
	b.bump(true)
	b.RequestFocus = true
	if o.OnChange != nil {
		o.OnChange()
	}
}

func (b *Board) pasteFromClipboard(readClip func() string, o BoardOptions) bool {
	if b.ReadOnly || readClip == nil {
		return false
	}
	raw := readClip()
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	if elems, ok := whiteboard.DecodeClipboard(trimmed); ok && len(elems) > 0 {
		var tx, ty float32
		if b.hoverValid && b.hoverMovedSinceCopy {
			tx, ty = b.hoverScene[0], b.hoverScene[1]
		} else {
			b.pasteCount++
			origBounds := whiteboard.SceneBounds(&whiteboard.Scene{Elements: elems})
			ox, oy := origBounds.Center()
			inView := b.lastRect.W > 0 && b.lastRect.H > 0
			if inView {
				sx, sy := b.sceneToScreen(b.lastRect, ox, oy)
				inView = sx >= b.lastRect.X && sx <= b.lastRect.X+b.lastRect.W &&
					sy >= b.lastRect.Y && sy <= b.lastRect.Y+b.lastRect.H
			}
			if inView {
				step := 24 * float32(b.pasteCount) / max(0.25, b.Zoom)
				tx, ty = ox+step, oy+step
			} else {
				vcx, vcy := b.viewportCenterScene()
				step := 24 * float32((b.pasteCount-1)%6) / max(0.25, b.Zoom)
				tx, ty = vcx+step, vcy+step
			}
		}
		pasted := whiteboard.InstantiateClipboard(elems, tx, ty)
		if len(pasted) == 0 {
			return false
		}
		b.appendPastedElements(pasted, o)
		return true
	}

	var tx, ty float32
	if b.hoverValid && b.hoverMovedSinceCopy {
		tx, ty = b.hoverScene[0], b.hoverScene[1]
	} else {
		tx, ty = b.viewportCenterScene()
	}
	if strings.HasPrefix(trimmed, "flowchart ") || strings.HasPrefix(trimmed, "graph ") || strings.HasPrefix(trimmed, "sequenceDiagram") {
		if mElems, err := ConvertMermaidToWhiteboard(trimmed, tx, ty); err == nil && len(mElems) > 0 {
			b.appendPastedElements(mElems, o)
			return true
		}
	}
	el := b.newElementFromDefaults(whiteboard.KindText, tx, ty)
	el.Text = trimmed
	tw, th := whiteboard.EstimateTextBounds(trimmed, el.FontSize)
	el.X = tx - tw/2
	el.Y = ty - th/2
	el.W = tw
	el.H = th
	b.appendPastedElements([]*whiteboard.Element{el}, o)
	return true
}

func (b *Board) duplicateSelected() bool {
	if b.ReadOnly || len(b.Selected) == 0 {
		return false
	}
	b.History.Record(b.Scene)
	nextSel := map[string]bool{}
	idMap := map[string]string{}
	var copies []*whiteboard.Element
	for _, el := range b.selectedList() {
		cp := el.Clone()
		cp.ID = whiteboard.NewID()
		cp.Seed = whiteboard.NewSeed()
		cp.X += 24
		cp.Y += 24
		cp.Version++
		idMap[el.ID] = cp.ID
		copies = append(copies, cp)
		nextSel[cp.ID] = true
	}
	for _, cp := range copies {
		if cp.StartBinding != nil {
			if mapped, ok := idMap[cp.StartBinding.ElementID]; ok {
				cp.StartBinding.ElementID = mapped
			}
		}
		if cp.EndBinding != nil {
			if mapped, ok := idMap[cp.EndBinding.ElementID]; ok {
				cp.EndBinding.ElementID = mapped
			}
		}
	}
	b.Scene.Elements = append(b.Scene.Elements, copies...)
	b.Selected = nextSel
	b.bump(true)
	return true
}

func (b *Board) zOrderSelected(forward bool) {
	if b.ReadOnly || len(b.Selected) == 0 {
		return
	}
	b.History.Record(b.Scene)
	els := b.Scene.Elements
	if forward {
		for i := len(els) - 2; i >= 0; i-- {
			if b.Selected[els[i].ID] && !b.Selected[els[i+1].ID] {
				els[i], els[i+1] = els[i+1], els[i]
			}
		}
	} else {
		for i := 1; i < len(els); i++ {
			if b.Selected[els[i].ID] && !b.Selected[elID(els[i-1])] {
				els[i], els[i-1] = els[i-1], els[i]
			}
		}
	}
	b.bump(true)
}

func elID(el *whiteboard.Element) string {
	if el == nil {
		return ""
	}
	return el.ID
}

func (b *Board) applyStyleToSelection(fn func(el *whiteboard.Element)) {
	sel := b.selectedList()
	if b.ReadOnly || len(sel) == 0 {
		return
	}
	b.History.Record(b.Scene)
	for _, el := range sel {
		fn(el)
		el.Version++
	}
	b.bump(true)
}

// PaintSceneFit draws scene scaled and centered inside rect without editing chrome.
func PaintSceneFit(p *ui.Painter, rect ui.Rect, scene *whiteboard.Scene, wb WhiteboardStyle, t *ui.Theme, pad float32) (scale, ox, oy float32) {
	if scene == nil || rect.W <= 0 || rect.H <= 0 {
		return 1, 0, 0
	}
	bounds := whiteboard.SceneBounds(scene)
	if bounds.Empty() {
		return 1, 0, 0
	}
	availW := max(8, rect.W-2*pad)
	availH := max(8, rect.H-2*pad)
	scale = min(availW/bounds.W, availH/bounds.H)
	scale = min(1.0, max(0.05, scale))
	ox = (rect.W-bounds.W*scale)/2 - bounds.X*scale
	oy = (rect.H-bounds.H*scale)/2 - bounds.Y*scale

	tmp := Board{
		Scene:     scene,
		Zoom:      scale,
		PanX:      ox,
		PanY:      oy,
		pathCache: map[string]cachedBoardElem{},
		textCache: map[string]*ui.TextLayout{},
	}
	p.Clip(rect, 0, func() {
		tmp.paintSceneContent(p, rect, wb, t, false)
	})
	return scale, ox, oy
}

func (b *Board) paintCanvas(p *ui.Painter, rect ui.Rect, wb WhiteboardStyle, t *ui.Theme, o BoardOptions) {
	p.Fill(rect, wb.Canvas, 0)
	b.linkPills = b.linkPills[:0]
	b.groupPill = boardGroupPill{}
	b.pruneEditingGroup()
	p.Clip(rect, 0, func() {
		if b.ShowGrid {
			b.paintDotGrid(p, rect, wb)
		}
		b.paintSceneContent(p, rect, wb, t, true)
		b.paintOverlay(p, rect, wb, t, o)
	})
}

func (b *Board) paintDotGrid(p *ui.Painter, rect ui.Rect, wb WhiteboardStyle) {
	grid := float32(20)
	if b.Scene.AppState.GridSize > 4 {
		grid = float32(b.Scene.AppState.GridSize)
	}
	step := grid * b.Zoom
	for step < 12 {
		step *= 2
	}
	for step > 64 {
		step /= 2
	}
	startX := rect.X + float32(math.Mod(float64(b.PanX), float64(step)))
	if startX < rect.X {
		startX += step
	}
	startY := rect.Y + float32(math.Mod(float64(b.PanY), float64(step)))
	if startY < rect.Y {
		startY += step
	}
	dotR := max(1, min(1.8, b.Zoom*1.25))
	for y := startY; y < rect.Y+rect.H; y += step {
		for x := startX; x < rect.X+rect.W; x += step {
			p.Fill(ui.Rect{X: x - dotR/2, Y: y - dotR/2, W: dotR, H: dotR}, wb.Grid, dotR/2)
		}
	}
}

func (b *Board) paintSceneContent(p *ui.Painter, rect ui.Rect, wb WhiteboardStyle, t *ui.Theme, interactive bool) {
	if b.Scene == nil {
		return
	}
	z := b.Zoom
	if z <= 0 {
		z = 1
	}

	// Pass 1 (Frames) and Pass 2 (all other elements) in scene coordinates.
	p.Transform(ui.Translation(rect.X+b.PanX, rect.Y+b.PanY), 0, 0, func() {
		p.Transform(ui.Scaling(z, z), 0, 0, func() {
			for _, passFrame := range []bool{true, false} {
				for _, el := range b.Scene.Elements {
					if el == nil || (el.Kind == whiteboard.KindFrame) != passFrame {
						continue
					}
					b.paintElementScene(p, el, wb, t, interactive)
				}
			}
		})
	})

	// Pass 3: Linked document badges in crisp screen coordinates so pill UI never distorts.
	if interactive {
		for _, el := range b.Scene.Elements {
			if el == nil {
				continue
			}
			target, _, ok := el.LinkTarget()
			if !ok {
				continue
			}
			eb := whiteboard.ElementBounds(el)
			sx, sy := b.sceneToScreen(rect, eb.X+eb.W, eb.Y)
			label := "↗ " + target
			tl := b.boardTextLayout(label, 11, "sans", ui.Start, 0, 500)
			tw, th := tl.Size()
			pw := min(140, tw+12)
			ph := max(18, th+4)
			pill := ui.Rect{X: sx - pw - 4, Y: sy - ph - 4, W: pw, H: ph}
			if pill.X < rect.X+4 || pill.Y < rect.Y+4 {
				pill.X = max(rect.X+4, sx-pw-4)
				pill.Y = max(rect.Y+4, sy+4)
			}
			if pill.X+pill.W < rect.X || pill.X > rect.X+rect.W || pill.Y+pill.H < rect.Y || pill.Y > rect.Y+rect.H {
				continue
			}
			p.Fill(pill, wb.IslandFill, 6)
			p.Stroke(pill, wb.ActiveInk.Alpha(0.38), 6, 1)
			p.Clip(ui.Rect{X: pill.X + 4, Y: pill.Y, W: pill.W - 8, H: pill.H}, 0, func() {
				p.TextLayout(tl, pill.X+6, pill.Y+(pill.H-th)/2, wb.ActiveInk)
			})
			b.linkPills = append(b.linkPills, boardLinkPill{elementID: el.ID, target: target, rect: pill})
		}
	}
}

func (b *Board) paintElementScene(p *ui.Painter, el *whiteboard.Element, wb WhiteboardStyle, t *ui.Theme, interactive bool) {
	if interactive && b.revealID == el.ID {
		elapsed := p.Now().Sub(b.revealAt)
		if elapsed >= 0 && elapsed < 1400*time.Millisecond {
			alpha := float32(0.28 * (1 - float64(elapsed)/float64(1400*time.Millisecond)))
			eb := whiteboard.ElementBounds(el)
			p.Fill(ui.Rect{X: eb.X - 6, Y: eb.Y - 6, W: eb.W + 12, H: eb.H + 12}, wb.Selection.Alpha(alpha), 10)
			p.AnimationFrame()
		} else if elapsed >= 1400*time.Millisecond {
			b.revealID = ""
		}
	}

	dimAlpha := float32(1.0)
	if interactive && b.editingGroupID != "" && !whiteboard.ElementInGroup(el, b.editingGroupID) {
		dimAlpha = 0.35
	}

	if el.Kind == whiteboard.KindFrame && el.Name != "" {
		eb := whiteboard.ElementBounds(el)
		tl := b.boardTextLayout(el.Name, 13, "sans", ui.Start, max(60, eb.W), 600)
		_, th := tl.Size()
		labelCol := wb.FrameLabel
		if dimAlpha < 1 {
			labelCol = labelCol.Alpha(float32(labelCol.A) / 255 * dimAlpha)
		}
		p.TextLayout(tl, eb.X+2, eb.Y-th-4, labelCol)
	}

	// Sticky notes / doc cards paint a soft note paper tint behind hachures.
	if el.Kind == whiteboard.KindNote {
		eb := whiteboard.ElementBounds(el)
		noteTint := wb.NoteFill
		if el.FillColor != "" && el.FillColor != "orange" {
			noteTint = resolveBoardFill(wb, el.FillColor, t).Alpha(0.28)
		}
		if dimAlpha < 1 {
			noteTint = noteTint.Alpha(float32(noteTint.A) / 255 * dimAlpha)
		}
		var bg ui.Path
		fold := min(22, min(eb.W, eb.H)*0.24)
		bg.MoveTo(eb.X, eb.Y)
		bg.LineTo(eb.X+eb.W-fold, eb.Y)
		bg.LineTo(eb.X+eb.W, eb.Y+fold)
		bg.LineTo(eb.X+eb.W, eb.Y+eb.H)
		bg.LineTo(eb.X, eb.Y+eb.H)
		bg.Close()
		p.FillPath(&bg, noteTint)
	}

	// Draw compiled rough primitives.
	for _, pr := range b.compiledPrimitives(el) {
		if pr.path == nil {
			continue
		}
		if pr.filled {
			fc := resolveBoardFill(wb, pr.fillColor, t)
			a := pr.opacity * dimAlpha
			if a < 1 {
				fc = fc.Alpha(float32(fc.A) / 255 * a)
			}
			p.FillPath(pr.path, fc)
		}
		if pr.stroked {
			sc := resolveBoardStroke(wb, pr.strokeColor, t)
			if pr.isHachure {
				fc := resolveBoardFill(wb, pr.strokeColor, t)
				sc = fc.Mix(resolveBoardStroke(wb, pr.strokeColor, t), 0.34)
			}
			a := pr.opacity * dimAlpha
			if a < 1 {
				sc = sc.Alpha(float32(sc.A) / 255 * a)
			}
			p.StrokePath(pr.path, pr.strokeWidth, sc)
		}
	}

	// Skip static text while the inline textarea is open on this element.
	if interactive && b.editingID == el.ID {
		return
	}
	b.paintElementText(p, el, wb, t, dimAlpha)
}

func (b *Board) paintElementText(p *ui.Painter, el *whiteboard.Element, wb WhiteboardStyle, t *ui.Theme, dimAlpha float32) {
	text := el.Text
	if strings.TrimSpace(text) == "" {
		return
	}
	eb := whiteboard.ElementBounds(el)
	fs := el.FontSize
	if fs <= 0 {
		fs = 18
	}
	fg := resolveBoardStroke(wb, el.StrokeColor, t)
	textOpacity := dimAlpha
	if el.Opacity > 0 && el.Opacity < 100 {
		textOpacity *= float32(el.Opacity) / 100
	}
	if textOpacity < 1 {
		fg = fg.Alpha(textOpacity)
	}
	align := ui.Center
	switch el.TextAlign {
	case "left":
		align = ui.Start
	case "right":
		align = ui.End
	}
	switch el.Kind {
	case whiteboard.KindText:
		if align != ui.Start {
			tw, _ := b.boardTextLayout(text, fs, el.FontFamily, ui.Start, 0, 400).Size()
			tl := b.boardTextLayout(text, fs, el.FontFamily, align, max(1, tw), 400)
			p.TextLayout(tl, eb.X, eb.Y, fg)
		} else {
			tl := b.boardTextLayout(text, fs, el.FontFamily, ui.Start, 0, 400)
			p.TextLayout(tl, eb.X, eb.Y, fg)
		}
	case whiteboard.KindNote:
		padX, padY := float32(12), float32(14)
		tl := b.boardTextLayout(text, fs, el.FontFamily, align, max(40, eb.W-2*padX), 400)
		p.TextLayout(tl, eb.X+padX, eb.Y+padY, fg)
	case whiteboard.KindArrow, whiteboard.KindLine:
		labelSize := max(13, fs-2)
		tw, th := b.boardTextLayout(text, labelSize, el.FontFamily, ui.Start, 200, 400).Size()
		tl := b.boardTextLayout(text, labelSize, el.FontFamily, ui.Center, max(1, tw), 400)
		cx, cy := eb.Center()
		halo := ui.Rect{X: cx - tw/2 - 5, Y: cy - th/2 - 2, W: tw + 10, H: th + 4}
		p.Fill(halo, wb.Canvas.Alpha(0.9), 4)
		p.TextLayout(tl, cx-tw/2, cy-th/2, fg)
	default:
		tb := whiteboard.ShapeTextBox(el)
		pad := float32(12)
		if el.Kind == whiteboard.KindDiamond {
			pad = tb.W * 0.18
		} else if el.Kind == whiteboard.KindActor || el.Kind == whiteboard.KindTriangle {
			pad = 6
		}
		maxW := max(24, tb.W-2*pad)
		tl := b.boardTextLayout(text, fs, el.FontFamily, align, maxW, 400)
		_, th := tl.Size()
		tx := tb.X + pad
		ty := tb.Y + (tb.H-th)/2
		p.TextLayout(tl, tx, ty, fg)
	}
}

func (b *Board) paintOverlay(p *ui.Painter, rect ui.Rect, wb WhiteboardStyle, t *ui.Theme, o BoardOptions) {
	// Smart alignment snap guides.
	for _, g := range b.guides {
		if g.Vertical {
			sx, y0 := b.sceneToScreen(rect, g.Pos, g.From)
			_, y1 := b.sceneToScreen(rect, g.Pos, g.To)
			p.Line(sx, y0, sx, y1, 1, wb.Guide.Alpha(0.75))
		} else {
			x0, sy := b.sceneToScreen(rect, g.From, g.Pos)
			x1, _ := b.sceneToScreen(rect, g.To, g.Pos)
			p.Line(x0, sy, x1, sy, 1, wb.Guide.Alpha(0.75))
		}
	}

	// Arrow bind target highlight ring (both pre-drag hover in connector mode and active drag).
	previewID := b.bindPreviewID
	if previewID == "" && !b.ReadOnly && b.dragMode == dragNone && b.hoverValid && (b.Tool == ToolArrow || b.Tool == ToolLine) {
		if target := whiteboard.FindBindTarget(b.Scene, "", b.hoverScene[0], b.hoverScene[1], 18/max(0.25, b.Zoom)); target != nil {
			previewID = target.ID
		}
	}
	if previewID != "" {
		if target := b.Scene.ElementByID(previewID); target != nil {
			tb := whiteboard.ElementBounds(target)
			x0, y0 := b.sceneToScreen(rect, tb.X-5, tb.Y-5)
			x1, y1 := b.sceneToScreen(rect, tb.X+tb.W+5, tb.Y+tb.H+5)
			p.Stroke(ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}, wb.Selection.Alpha(0.72), 10, 2)
			for _, pt := range [4][2]float32{
				{tb.X + tb.W/2, tb.Y},
				{tb.X + tb.W, tb.Y + tb.H/2},
				{tb.X + tb.W/2, tb.Y + tb.H},
				{tb.X, tb.Y + tb.H/2},
			} {
				px, py := b.sceneToScreen(rect, pt[0], pt[1])
				p.Fill(ui.Rect{X: px - 3, Y: py - 3, W: 6, H: 6}, wb.Selection, 3)
			}
		}
	}

	// Active group isolation boundary and floating action pill.
	if !b.ReadOnly && b.editingGroupID != "" {
		if igb := whiteboard.GroupBounds(b.Scene, b.editingGroupID); !igb.Empty() {
			dx0, dy0 := b.sceneToScreen(rect, igb.X-8, igb.Y-8)
			dx1, dy1 := b.sceneToScreen(rect, igb.X+igb.W+8, igb.Y+igb.H+8)
			var dash ui.Path
			dash.MoveTo(dx0, dy0)
			dash.LineTo(dx1, dy0)
			dash.LineTo(dx1, dy1)
			dash.LineTo(dx0, dy1)
			dash.Close()
			dash.Dash(0, 6, 4)
			p.StrokePath(&dash, 1.25, wb.Selection.Alpha(0.68))
			if b.dragMode == dragNone {
				pill := b.drawGroupActionPill(p, rect, dx0, dy0, dy1, i18n.T("组内编辑 · 完成 (Esc)"), true, wb, t)
				b.groupPill = boardGroupPill{groupID: b.editingGroupID, exiting: true, rect: pill}
			}
		}
	}

	// Selection boxes, resize handles, and quick-connect ports.
	sel := b.selectedList()
	if !b.ReadOnly && b.Tool != ToolFreedraw && len(sel) > 0 {
		memberAlpha := float32(0.82)
		memberStroke := float32(1.25)
		if len(sel) >= 2 {
			memberAlpha = 0.38
			memberStroke = 1
		}
		for _, el := range sel {
			eb := whiteboard.ElementBounds(el)
			x0, y0 := b.sceneToScreen(rect, eb.X-4, eb.Y-4)
			x1, y1 := b.sceneToScreen(rect, eb.X+eb.W+4, eb.Y+eb.H+4)
			sr := ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
			p.Stroke(sr, wb.Selection.Alpha(memberAlpha), 3, memberStroke)
		}
		if len(sel) == 1 && b.editingID != sel[0].ID {
			el := sel[0]
			isConn := el.Kind == whiteboard.KindArrow || el.Kind == whiteboard.KindLine
			handles := whiteboard.HandlePoints(el)
			for hk, pt := range handles {
				hx, hy := b.sceneToScreen(rect, pt[0], pt[1])
				if isConn && ((hk == whiteboard.HandleStart && el.StartBinding != nil) || (hk == whiteboard.HandleEnd && el.EndBinding != nil)) {
					halo := ui.Rect{X: hx - 8, Y: hy - 8, W: 16, H: 16}
					p.Fill(halo, wb.Selection.Alpha(0.22), 8)
					p.Stroke(halo, wb.Selection.Alpha(0.65), 8, 1)
				}
				if hk == whiteboard.HandleMid {
					hr := ui.Rect{X: hx - 4.5, Y: hy - 4.5, W: 9, H: 9}
					p.Fill(hr, wb.ActiveInk.Alpha(0.88), 4.5)
					p.Stroke(hr, wb.HandleFill, 4.5, 1.5)
					continue
				}
				hr := ui.Rect{X: hx - 4, Y: hy - 4, W: 8, H: 8}
				p.Fill(hr, wb.HandleFill, 2.5)
				p.Stroke(hr, wb.Selection, 2.5, 1.5)
			}
			if b.dragMode == dragNone && whiteboard.HasConnectPorts(el) {
				portTol := float64(8 / max(0.25, b.Zoom))
				for _, pt := range whiteboard.ConnectPortPoints(el, b.Zoom) {
					px, py := b.sceneToScreen(rect, pt[0], pt[1])
					hovered := b.hoverValid && math.Hypot(float64(b.hoverScene[0]-pt[0]), float64(b.hoverScene[1]-pt[1])) <= portTol
					if hovered {
						pr := ui.Rect{X: px - 7, Y: py - 7, W: 14, H: 14}
						p.Fill(pr, wb.Selection, 7)
						p.Line(px-3.5, py, px+3.5, py, 1.5, wb.HandleFill)
						p.Line(px, py-3.5, px, py+3.5, 1.5, wb.HandleFill)
					} else {
						pr := ui.Rect{X: px - 5, Y: py - 5, W: 10, H: 10}
						p.Fill(pr, wb.HandleFill, 5)
						p.Stroke(pr, wb.Selection.Alpha(0.85), 5, 1.5)
						p.Fill(ui.Rect{X: px - 2, Y: py - 2, W: 4, H: 4}, wb.Selection.Alpha(0.80), 2)
					}
				}
			}
		} else if len(sel) >= 2 {
			gb := whiteboard.SelectionBounds(sel)
			gx0, gy0 := b.sceneToScreen(rect, gb.X, gb.Y)
			gx1, gy1 := b.sceneToScreen(rect, gb.X+gb.W, gb.Y+gb.H)
			outerRect := ui.Rect{X: gx0, Y: gy0, W: max(1, gx1-gx0), H: max(1, gy1-gy0)}
			p.Stroke(outerRect, wb.Selection.Alpha(0.88), 3, 1.5)
			if b.editingID == "" && hasUnlockedElements(sel) {
				for _, pt := range whiteboard.BoxHandlePoints(gb) {
					hx, hy := b.sceneToScreen(rect, pt[0], pt[1])
					hr := ui.Rect{X: hx - 4, Y: hy - 4, W: 8, H: 8}
					p.Fill(hr, wb.HandleFill, 2.5)
					p.Stroke(hr, wb.Selection, 2.5, 1.5)
				}
			}
			if b.editingID == "" && b.dragMode == dragNone && b.editingGroupID == "" {
				if sharedGID := whiteboard.SharedGroupID(sel, ""); sharedGID != "" {
					pill := b.drawGroupActionPill(p, rect, gx0, gy0, gy1, i18n.T("✎ 编辑内部"), false, wb, t)
					b.groupPill = boardGroupPill{groupID: sharedGID, exiting: false, rect: pill}
				}
			}
		}
	}

	// Drag marquee box.
	if b.marquee != nil {
		x0, y0 := b.sceneToScreen(rect, b.marquee.X, b.marquee.Y)
		x1, y1 := b.sceneToScreen(rect, b.marquee.X+b.marquee.W, b.marquee.Y+b.marquee.H)
		mr := ui.Rect{X: min(x0, x1), Y: min(y0, y1), W: absf(x1 - x0), H: absf(y1 - y0)}
		p.Fill(mr, wb.Selection.Alpha(0.10), 2)
		p.Stroke(mr, wb.Selection.Alpha(0.65), 2, 1)
	}

	// Fading red laser pointer trail during presentation.
	b.paintLaserTrail(p, rect)
}

func (b *Board) paintLaserTrail(p *ui.Painter, rect ui.Rect) {
	if len(b.laserTrail) == 0 {
		return
	}
	const life = 1100 * time.Millisecond
	now := p.Now()
	kept := b.laserTrail[:0]
	for _, s := range b.laserTrail {
		if now.Sub(s.at) < life {
			kept = append(kept, s)
		}
	}
	b.laserTrail = kept
	if len(b.laserTrail) == 0 {
		return
	}
	laserCol := ui.Hex("#E03131")
	for i := 1; i < len(b.laserTrail); i++ {
		a, prev := b.laserTrail[i], b.laserTrail[i-1]
		if a.at.Sub(prev.at) > 160*time.Millisecond {
			continue
		}
		fade := float32(1 - float64(now.Sub(a.at))/float64(life))
		fade = max(0, min(1, fade))
		x0, y0 := b.sceneToScreen(rect, prev.x, prev.y)
		x1, y1 := b.sceneToScreen(rect, a.x, a.y)
		w := max(1.5, 5.5*fade)
		p.Line(x0, y0, x1, y1, w+4, laserCol.Alpha(0.22*fade))
		p.Line(x0, y0, x1, y1, w, laserCol.Alpha(0.92*fade))
	}
	last := b.laserTrail[len(b.laserTrail)-1]
	lx, ly := b.sceneToScreen(rect, last.x, last.y)
	fade := max(0, min(1, float32(1-float64(now.Sub(last.at))/float64(life))))
	p.Fill(ui.Rect{X: lx - 4, Y: ly - 4, W: 8, H: 8}, laserCol.Alpha(fade), 4)
	p.AnimationFrame()
}

func (b *Board) compiledPrimitives(el *whiteboard.Element) []compiledBoardPrim {
	key := boardElemKey{
		kind:        el.Kind,
		version:     el.Version,
		x:           el.X,
		y:           el.Y,
		w:           el.W,
		h:           el.H,
		strokeColor: el.StrokeColor,
		fillColor:   el.FillColor,
		fillStyle:   el.FillStyle,
		strokeWidth: el.StrokeWidth,
		strokeStyle: el.StrokeStyle,
		roughness:   el.Roughness,
		roundness:   el.Roundness,
		opacity:     el.Opacity,
		smooth:      el.Smooth,
		elbowed:     el.Elbowed,
		startHead:   el.StartArrowhead,
		endHead:     el.EndArrowhead,
		hasText:     el.Text != "",
		seed:        el.Seed,
		ptsLen:      len(el.Points),
	}
	if cached, ok := b.pathCache[el.ID]; ok && cached.key == key {
		return cached.prims
	}
	raw := whiteboard.BuildPrimitives(b.Scene, el)
	out := make([]compiledBoardPrim, 0, len(raw))
	for _, pr := range raw {
		up := &ui.Path{}
		for _, c := range pr.Cmds {
			switch c.Kind {
			case whiteboard.CmdMoveTo:
				up.MoveTo(c.X, c.Y)
			case whiteboard.CmdLineTo:
				up.LineTo(c.X, c.Y)
			case whiteboard.CmdQuadTo:
				up.QuadTo(c.C1X, c.C1Y, c.X, c.Y)
			case whiteboard.CmdCubeTo:
				up.CubeTo(c.C1X, c.C1Y, c.C2X, c.C2Y, c.X, c.Y)
			case whiteboard.CmdClose:
				up.Close()
			}
		}
		if len(pr.Dash) > 0 {
			up.Dash(0, pr.Dash...)
		}
		out = append(out, compiledBoardPrim{
			path:        up,
			strokeColor: pr.StrokeColor,
			fillColor:   pr.FillColor,
			strokeWidth: pr.StrokeWidth,
			opacity:     pr.Opacity,
			filled:      pr.Filled,
			stroked:     pr.Stroked,
			isHachure:   pr.IsHachure,
		})
	}
	if b.pathCache == nil || len(b.pathCache) > 2048 {
		b.pathCache = map[string]cachedBoardElem{}
	}
	b.pathCache[el.ID] = cachedBoardElem{key: key, prims: out}
	return out
}

func (b *Board) boardTextLayout(text string, size float32, fontFamily string, align ui.Align, maxW float32, weight int) *ui.TextLayout {
	if weight == 0 {
		weight = 400
	}
	family := boardFontFamily(fontFamily)
	key := fmt.Sprintf("%s|%g|%s|%d|%g|%d", text, size, fontFamily, align, maxW, weight)
	if tl, ok := b.textCache[key]; ok {
		return tl
	}
	if b.textCache == nil || len(b.textCache) > 4096 {
		b.textCache = map[string]*ui.TextLayout{}
	}
	tl := ui.ShapeRichTextWith(
		[]ui.Span{{Text: text, Size: size, Font: family, Weight: weight}},
		ui.Font{Family: family, Size: size, Weight: weight, LineHeight: 1.3},
		ui.TextShape{Width: maxW, Align: align},
	)
	b.textCache[key] = tl
	return tl
}

var boardHandFamily = sync.OnceValue(func() string {
	if runtime.GOOS == "darwin" && !darwinHasKaitiOnDisk() {
		return "Virgil, Bradley Hand, Segoe Print, system-ui"
	}
	return "Virgil, Bradley Hand, Segoe Print, Kaiti SC, STKaiti, KaiTi, system-ui"
})

func darwinHasKaitiOnDisk() bool {
	patterns := []string{
		"/System/Library/AssetsV2/com_apple_MobileAsset_Font*/*.asset/AssetData/Kaiti.ttc",
		"/Library/Fonts/*Kai*",
		"/Library/Fonts/*kai*",
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		patterns = append(patterns,
			filepath.Join(home, "Library/Fonts/*Kai*"),
			filepath.Join(home, "Library/Fonts/*kai*"),
		)
	}
	for _, pat := range patterns {
		if matches, err := filepath.Glob(pat); err == nil && len(matches) > 0 {
			return true
		}
	}
	return false
}

func boardFontFamily(name string) string {
	switch name {
	case "sans":
		return "system-ui"
	case "mono":
		return "monospace"
	default:
		return boardHandFamily()
	}
}

func resolveBoardStroke(wb WhiteboardStyle, name string, t *ui.Theme) ui.Color {
	if c, ok := wb.Strokes[name]; ok {
		return c
	}
	if strings.HasPrefix(name, "#") {
		return ui.Hex(name)
	}
	if c, ok := wb.Strokes["ink"]; ok {
		return c
	}
	if t != nil {
		return t.Text
	}
	return ui.Color{}
}

func resolveBoardFill(wb WhiteboardStyle, name string, t *ui.Theme) ui.Color {
	if c, ok := wb.Fills[name]; ok {
		return c
	}
	if strings.HasPrefix(name, "#") {
		return ui.Hex(name)
	}
	if c, ok := wb.Fills["blue"]; ok {
		return c
	}
	if t != nil {
		return t.Accent.Alpha(0.2)
	}
	return ui.Color{}
}

func clampZoom(z float32) float32 {
	if z < 0.1 {
		return 0.1
	}
	if z > 4.0 {
		return 4.0
	}
	return z
}

// SetLibraryOpen controls the component library drawer visibility.
func (b *Board) SetLibraryOpen(open bool) {
	b.libraryOpen = open
}

// SetShapesPopoverOpen controls the extended shapes picker popover visibility.
func (b *Board) SetShapesPopoverOpen(open bool) {
	b.shapesPopoverOpen = open
}

func (b *Board) drawGroupActionPill(p *ui.Painter, canvas ui.Rect, gx0, gy0, gy1 float32, label string, active bool, wb WhiteboardStyle, t *ui.Theme) ui.Rect {
	tl := b.boardTextLayout(label, 11, "sans", ui.Start, 0, 500)
	tw, th := tl.Size()
	pw := tw + 16
	ph := max(22, th+8)
	px := gx0
	py := gy0 - ph - 12
	minTop := canvas.Y + 56
	if b.lastRect.W > 0 && canvas.Y+94 < canvas.Y+canvas.H {
		minTop = canvas.Y + 94
	}
	if py < minTop {
		py = gy1 + 12
	}
	if px+pw > canvas.X+canvas.W-8 {
		px = canvas.X + canvas.W - 8 - pw
	}
	if px < canvas.X+8 {
		px = canvas.X + 8
	}
	if py+ph > canvas.Y+canvas.H-8 {
		py = max(canvas.Y+8, gy0-ph-8)
	}
	pill := ui.Rect{X: px, Y: py, W: pw, H: ph}
	fill := wb.IslandFill
	border := wb.Selection.Alpha(0.55)
	ink := wb.ActiveInk
	if active {
		fill = wb.Selection
		border = wb.Selection
		ink = wb.HandleFill
		if t != nil && t.AccentText != (ui.Color{}) {
			ink = t.AccentText
		}
	}
	p.Fill(pill, fill, 7)
	p.Stroke(pill, border, 7, 1)
	p.TextLayout(tl, pill.X+8, pill.Y+(pill.H-th)/2, ink)
	return pill
}

func hasUnlockedElements(elems []*whiteboard.Element) bool {
	for _, el := range elems {
		if el != nil && !el.Locked {
			return true
		}
	}
	return false
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
