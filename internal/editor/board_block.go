package editor

import (
	"path"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

const (
	boardDefaultEditH = float32(440)
	boardMinEditH     = float32(260)
	boardMaxEditH     = float32(760)
	boardRestMaxH     = float32(420)
	boardMargin       = float32(14)
)

type boardSceneCache struct {
	src   string
	scene *whiteboard.Scene
	err   error
}

type embedBoardCache struct {
	rev   uint64
	scene *whiteboard.Scene
	err   error
}

func isWhiteboardBlock(b *document.Block) bool {
	if b == nil || b.Kind != "code" {
		return false
	}
	lang := strings.TrimSpace(b.Language)
	return strings.EqualFold(lang, "excalidraw") || strings.EqualFold(lang, "whiteboard")
}

func isWhiteboardURL(raw string) bool {
	clean, _, _ := strings.Cut(strings.TrimSpace(raw), "#")
	clean, _, _ = strings.Cut(clean, "?")
	base := path.Base(strings.ReplaceAll(clean, `\`, "/"))
	low := strings.ToLower(base)
	return (len(low) > len(".excalidraw.svg") && strings.HasSuffix(low, ".excalidraw.svg")) ||
		(len(low) > len(".excalidraw") && strings.HasSuffix(low, ".excalidraw"))
}

func isWhiteboardEmbed(b *document.Block) bool {
	if b == nil || b.Kind != "image" {
		return false
	}
	return isWhiteboardURL(b.URL)
}

func isAtomicVisualBlock(b *document.Block) bool {
	return isWhiteboardEmbed(b) || isWhiteboardBlock(b) || isPDFEmbed(b) || isBookEmbed(b) || isDatabaseBlock(b)
}

func (e *Editor) deleteAtomicBlock(id string, backward bool) {
	if e.ReadOnly || id == "" {
		return
	}
	if e.boardEditing == id {
		e.boardEditing = ""
	}
	if e.boardModalID == id {
		e.boardModalID = ""
	}
	if e.pdfBrowsing == id {
		e.pdfBrowsing = ""
	}
	if e.pdfModalID == id {
		e.pdfModalID = ""
	}
	if e.bookBrowsing == id {
		e.bookBrowsing = ""
	}
	if e.bookModalID == id {
		e.bookModalID = ""
	}
	if e.imageCaptionID == id {
		e.imageCaptionID = ""
	}
	e.clearBlocks()
	e.Engine.RemoveBlock(id, backward)
	e.sync()
	e.ready = false
	e.reveal = true
	e.vertical = false
}

func (e *Editor) copySingleBlock(b *document.Block, write func(string)) {
	if b == nil {
		return
	}
	doc := document.New()
	doc.Blocks = []*document.Block{b.Clone()}
	e.writeFragment(doc, "", write)
}

func (e *Editor) startBoardEditing(id string) {
	e.boardEditing = id
	e.boardFocusPending = id != ""
	e.ready = false
}

func (e *Editor) openBoardModal(id string) {
	if e.ReadOnly || id == "" {
		return
	}
	e.boardModalID = id
	e.boardModalFocusPending = true
	e.ready = false
	if b := e.blockByID(id); b != nil {
		if board := e.boardForBlock(b); board != nil {
			board.RequestFit()
		}
	}
}

func (e *Editor) closeBoardModal(id string, board *Board) {
	if board != nil {
		e.commitBoardToBlock(id, board)
	}
	e.boardModalID = ""
	e.ready = false
	if e.boardEditing == id {
		if board != nil {
			board.RequestFit()
		}
		e.boardFocusPending = true
	} else {
		e.RequestFocus = true
	}
}

// EditWhiteboardBlock opens the inline interactive whiteboard editor for block id.
func (e *Editor) EditWhiteboardBlock(id string) {
	if e.ReadOnly {
		return
	}
	e.startBoardEditing(id)
}

// InsertWhiteboard inserts a starter hand-drawn whiteboard at the caret and
// opens it directly in interactive editing mode. When CreateWhiteboard is wired,
// it creates an amphibious *.excalidraw.svg image asset so standard Markdown
// viewers (Typora, GitHub, browsers) render the vector drawing directly.
func (e *Editor) InsertWhiteboard() {
	if e.ReadOnly {
		return
	}
	starter := whiteboard.StarterScene()
	var targetID string
	if e.createWhiteboard != nil {
		if url, err := e.createWhiteboard(starter); err == nil && url != "" {
			b := document.NewBlock("image", i18n.T("手绘白板"))
			b.URL = url
			targetID = b.ID
			e.Engine.Insert(b)
			e.startBoardEditing(targetID)
			e.sync()
			e.reveal = true
			return
		}
	}
	src, err := starter.Encode()
	if err != nil {
		return
	}
	b := document.NewBlock("code", src)
	b.Language = "excalidraw"
	targetID = b.ID
	e.Engine.Insert(b)
	e.startBoardEditing(targetID)
	e.sync()
	e.reveal = true
}

func (e *Editor) whiteboardSceneFor(id, src string) (*whiteboard.Scene, error) {
	if e.boardScenes == nil {
		e.boardScenes = map[string]boardSceneCache{}
	}
	if c, ok := e.boardScenes[id]; ok && c.src == src {
		return c.scene, c.err
	}
	scene, err := whiteboard.Decode(src)
	e.boardScenes[id] = boardSceneCache{src: src, scene: scene, err: err}
	return scene, err
}

func (e *Editor) boardForBlock(b *document.Block) *Board {
	if b == nil {
		return nil
	}
	if e.boards == nil {
		e.boards = map[string]*Board{}
	}
	if isWhiteboardEmbed(b) {
		if e.boardEmbedRev == nil {
			e.boardEmbedRev = map[string]uint64{}
		}
		var scene *whiteboard.Scene
		var rev uint64
		var err error
		if e.loadWhiteboard != nil {
			scene, rev, err = e.loadWhiteboard(b.URL)
		}
		if err != nil || scene == nil {
			scene = whiteboard.NewScene()
		}
		board, ok := e.boards[b.ID]
		if !ok {
			board = NewBoard(scene.Clone())
			e.boards[b.ID] = board
			e.boardEmbedRev[b.ID] = rev
		} else if e.boardEmbedRev[b.ID] != rev {
			board.ReplaceScene(scene.Clone(), false)
			e.boardEmbedRev[b.ID] = rev
		}
		return board
	}
	if e.boardSynced == nil {
		e.boardSynced = map[string]string{}
	}
	src := b.Text()
	board, ok := e.boards[b.ID]
	if !ok || e.boardSynced[b.ID] != src {
		scene, err := whiteboard.Decode(src)
		if err != nil || scene == nil {
			scene = whiteboard.NewScene()
		}
		if !ok {
			board = NewBoard(scene)
			e.boards[b.ID] = board
		} else {
			board.ReplaceScene(scene, false)
		}
		e.boardSynced[b.ID] = src
	}
	return board
}

func (e *Editor) commitBoardToBlock(id string, b *Board) {
	if b == nil || e.ReadOnly {
		return
	}
	blk := e.blockByID(id)
	if blk == nil {
		return
	}
	if isWhiteboardEmbed(blk) {
		if e.saveWhiteboard != nil {
			rev := e.saveWhiteboard(blk.URL, b.SceneSnapshot())
			if e.boardEmbedRev == nil {
				e.boardEmbedRev = map[string]uint64{}
			}
			e.boardEmbedRev[id] = rev
			delete(e.embedBoards, blk.URL)
		}
		return
	}
	encoded, err := b.SceneSnapshot().Encode()
	if err != nil {
		return
	}
	if e.boardSynced == nil {
		e.boardSynced = map[string]string{}
	}
	if e.boardSynced[id] == encoded {
		return
	}
	e.boardSynced[id] = encoded
	e.Engine.Edit(func() {
		blk.Runs = []document.Run{{Text: encoded}}
	})
	e.sync()
}

func (e *Editor) whiteboardRow(r *row, o Options, gap float32) float32 {
	b := r.leaf.Block
	src := b.Text()
	editing := !e.printing && !e.ReadOnly && e.boardEditing == b.ID && e.boardModalID != b.ID
	r.boardEditing = editing
	r.boardEmbed = false
	r.boardErr = ""
	r.boardError = nil
	r.boardScene = nil

	if editing {
		h := boardDefaultEditH
		if custom, ok := e.boardHeights[b.ID]; ok {
			h = max(boardMinEditH, min(boardMaxEditH, custom))
		}
		r.boardRect = ui.Rect{X: r.rect.X, Y: r.rect.Y + boardMargin, W: r.rect.W, H: h}
		_ = e.boardForBlock(b)
		r.rect.H = 2*boardMargin + h + gap
		return r.rect.H
	}

	scene, err := e.whiteboardSceneFor(b.ID, src)
	if err != nil {
		r.boardErr = i18n.T("白板无法打开") + ": " + err.Error()
		r.boardError = ui.ShapeText(r.boardErr, ui.Font{Family: "system-ui", Size: 13}, max(40, r.rect.W-24))
		_, eh := r.boardError.Size()
		h := max(48, eh+24)
		r.boardRect = ui.Rect{X: r.rect.X, Y: r.rect.Y + boardMargin, W: r.rect.W, H: h}
		r.rect.H = 2*boardMargin + h + gap
		return r.rect.H
	}
	r.boardScene = scene
	h := boardPreviewHeight(scene, r.rect.W, e.printing)
	r.boardRect = ui.Rect{X: r.rect.X, Y: r.rect.Y + boardMargin, W: r.rect.W, H: h}
	r.rect.H = 2*boardMargin + h + gap
	return r.rect.H
}

func (e *Editor) layoutWhiteboardEmbed(r *row, o Options, gap float32) float32 {
	b := r.leaf.Block
	r.boardEmbed = true
	editing := !e.printing && !e.ReadOnly && e.saveWhiteboard != nil && e.boardEditing == b.ID && e.boardModalID != b.ID
	r.boardEditing = editing
	r.boardErr = ""
	r.boardError = nil
	r.boardScene = nil

	if editing {
		availW := r.rect.W
		if b.ImageWidth > 120 {
			availW = min(availW, b.ImageWidth)
		}
		h := boardDefaultEditH
		if custom, ok := e.boardHeights[b.ID]; ok {
			h = max(boardMinEditH, min(boardMaxEditH, custom))
		}
		x := r.rect.X + (r.rect.W-availW)/2
		r.boardRect = ui.Rect{X: x, Y: r.rect.Y + boardMargin, W: availW, H: h}
		_ = e.boardForBlock(b)
		r.rect.H = 2*boardMargin + h + gap
		return r.rect.H
	}

	if e.loadWhiteboard == nil {
		r.boardErr = i18n.T("白板无法打开")
		r.boardError = ui.ShapeText(r.boardErr, ui.Font{Family: "system-ui", Size: 13}, max(40, r.rect.W-24))
		r.boardRect = ui.Rect{X: r.rect.X, Y: r.rect.Y + boardMargin, W: r.rect.W, H: 64}
		r.rect.H = 2*boardMargin + 64 + gap
		return r.rect.H
	}
	scene, rev, err := e.loadWhiteboard(b.URL)
	if err != nil || scene == nil {
		msg := i18n.T("白板无法打开")
		if err != nil {
			msg += ": " + err.Error()
		}
		r.boardErr = msg
		r.boardError = ui.ShapeText(msg, ui.Font{Family: "system-ui", Size: 13}, max(40, r.rect.W-24))
		_, eh := r.boardError.Size()
		h := max(64, eh+28)
		r.boardRect = ui.Rect{X: r.rect.X, Y: r.rect.Y + boardMargin, W: r.rect.W, H: h}
		r.rect.H = 2*boardMargin + h + gap
		return r.rect.H
	}
	if e.embedBoards == nil {
		e.embedBoards = map[string]embedBoardCache{}
	}
	e.embedBoards[b.URL] = embedBoardCache{rev: rev, scene: scene, err: nil}
	r.boardScene = scene

	availW := r.rect.W
	if b.ImageWidth > 120 {
		availW = min(availW, b.ImageWidth)
	}
	h := boardPreviewHeight(scene, availW, e.printing)
	x := r.rect.X + (r.rect.W-availW)/2
	r.boardRect = ui.Rect{X: x, Y: r.rect.Y + boardMargin, W: availW, H: h}
	r.rect.H = 2*boardMargin + h + gap
	return r.rect.H
}

func boardPreviewHeight(scene *whiteboard.Scene, width float32, printing bool) float32 {
	if scene == nil || len(scene.Elements) == 0 {
		return 140
	}
	sb := whiteboard.SceneBounds(scene)
	if sb.Empty() {
		return 140
	}
	pad := float32(24)
	availW := max(40, width-2*pad)
	scale := min(1.0, availW/sb.W)
	h := sb.H*scale + 2*pad
	if printing {
		return max(96, min(680, h))
	}
	return max(120, min(boardRestMaxH, h))
}

func (e *Editor) paintWhiteboardRow(p *ui.Painter, bounds ui.Rect, r row, t *ui.Theme) bool {
	if !isWhiteboardBlock(r.leaf.Block) && !r.boardEmbed {
		return false
	}
	if r.boardEditing {
		return true // painted by interactive BoardView overlay in boardControls
	}
	dx, dy := e.visualOffset(r)
	box := r.boardRect
	box.X += bounds.X + dx
	box.Y += bounds.Y + dy

	hovered := !e.printing && (e.hovered(r.boardRect, 0) || e.boardHoverID == r.leaf.Block.ID)
	caretSelected := !e.printing && e.focused && len(e.blocks.ids) == 0 && e.Anchor == e.Caret && e.Engine.LeafAt(e.Caret).Block.ID == r.leaf.Block.ID
	sel := e.Selection().Range
	rangeSelected := !e.printing && e.focused && len(e.blocks.ids) == 0 && e.Anchor != e.Caret && sel.End > r.leaf.Start && sel.Start < r.leaf.End

	if r.boardError != nil {
		p.StrokeDashed(box, e.page.Caption, 8, 1)
		ew, eh := r.boardError.Size()
		p.TextLayout(r.boardError, box.X+(box.W-ew)/2, box.Y+(box.H-eh)/2, t.Danger)
		if caretSelected {
			p.Stroke(box, t.Accent, 8, 2)
		}
		if rangeSelected {
			p.Fill(box, t.Selection, 8)
		}
		return true
	}

	wb := e.page.Whiteboard
	if r.boardEmbed || hovered || caretSelected {
		p.Fill(box, wb.Canvas, 10)
		border := e.page.CodeBorder
		strokeW := float32(1)
		switch {
		case caretSelected:
			border = t.Accent
			strokeW = 2
		case hovered:
			border = t.Accent.Alpha(0.45)
		}
		p.Stroke(box, border, 10, strokeW)
	}
	if r.boardScene != nil && len(r.boardScene.Elements) > 0 {
		PaintSceneFit(p, box, r.boardScene, wb, t, 20)
	} else if !e.printing {
		hint := ui.ShapeText(i18n.T("白板暂无图形"), ui.Font{Family: "system-ui", Size: 13}, 0)
		hw, hh := hint.Size()
		p.TextLayout(hint, box.X+(box.W-hw)/2, box.Y+(box.H-hh)/2, e.page.Placeholder)
	}

	if r.boardEmbed && !e.printing {
		badge := ui.ShapeText(path.Base(r.leaf.Block.URL), ui.Font{Family: "system-ui", Size: 11, Weight: 500}, 0)
		bw, bh := badge.Size()
		pill := ui.Rect{X: box.X + 10, Y: box.Y + box.H - bh - 14, W: min(box.W-20, bw+14), H: bh + 6}
		p.Fill(pill, wb.IslandFill, 6)
		p.Stroke(pill, wb.IslandBorder, 6, 1)
		p.TextLayout(badge, pill.X+7, pill.Y+3, e.page.Caption)
	}
	if rangeSelected {
		p.Fill(box, t.Selection, 10)
	}
	return true
}

func boardEditButtonRect(d decoration) ui.Rect {
	return ui.Rect{X: d.rect.X + d.rect.W - 244, Y: d.rect.Y + 8, W: 76, H: 24}
}

func boardExpandButtonRect(d decoration) ui.Rect {
	return ui.Rect{X: d.rect.X + d.rect.W - 162, Y: d.rect.Y + 8, W: 76, H: 24}
}

func boardSaveFileButtonRect(d decoration) ui.Rect {
	return ui.Rect{X: d.rect.X + d.rect.W - 350, Y: d.rect.Y + 8, W: 100, H: 24}
}

func boardCopySVGButtonRect(d decoration) ui.Rect {
	return ui.Rect{X: d.rect.X + d.rect.W - 80, Y: d.rect.Y + 8, W: 72, H: 24}
}

func boardEmbedOpenRect(d decoration) ui.Rect {
	return ui.Rect{X: d.rect.X + d.rect.W - 170, Y: d.rect.Y + 8, W: 74, H: 24}
}

func boardEmbedSplitRect(d decoration) ui.Rect {
	return ui.Rect{X: d.rect.X + d.rect.W - 90, Y: d.rect.Y + 8, W: 82, H: 24}
}

type boardHoverAction uint8

const (
	hoverNone boardHoverAction = iota
	hoverEdit
	hoverExpand
	hoverOpen
	hoverSplit
)

type boardHoverPill struct {
	rect   ui.Rect
	icon   *ui.SVG
	label  string
	action boardHoverAction
}

func (e *Editor) embedHoverPills(d decoration) []boardHoverPill {
	var pills []boardHoverPill
	rx := d.rect.X + d.rect.W - 8
	push := func(w float32, icon *ui.SVG, label string, act boardHoverAction) {
		if rx-w < d.rect.X+8 {
			return
		}
		rx -= w
		pills = append(pills, boardHoverPill{
			rect:   ui.Rect{X: rx, Y: d.rect.Y + 8, W: w, H: 24},
			icon:   icon,
			label:  label,
			action: act,
		})
		rx -= 6
	}
	if e.openWhiteboard != nil {
		push(84, iconExternalSplit, i18n.T("在分屏中编辑"), hoverSplit)
		push(74, iconWhiteboard, i18n.T("打开白板"), hoverOpen)
	}
	if !e.ReadOnly && e.saveWhiteboard != nil {
		push(76, iconMaximize, i18n.T("放大画布"), hoverExpand)
		push(76, iconPencil, i18n.T("编辑画布"), hoverEdit)
	}
	return pills
}

func (e *Editor) paintWhiteboardHover(p *ui.Painter, bounds ui.Rect, d decoration, t *ui.Theme) {
	// Hover action pills are rendered as interactive buttons in boardControls.
	_, _, _, _ = p, bounds, d, t
}

func (e *Editor) whiteboardControl(d decoration, x, y float32) bool {
	in := func(r ui.Rect) bool { return x >= r.X && x <= r.X+r.W && y >= r.Y && y <= r.Y+r.H }
	if d.boardEditing {
		return false
	}
	if d.boardEmbed {
		for _, pill := range e.embedHoverPills(d) {
			if !in(pill.rect) {
				continue
			}
			switch pill.action {
			case hoverEdit:
				e.startBoardEditing(d.id)
				return true
			case hoverExpand:
				e.openBoardModal(d.id)
				return true
			case hoverOpen:
				if e.openWhiteboard != nil {
					e.openWhiteboard(d.boardURL, false)
					return true
				}
			case hoverSplit:
				if e.openWhiteboard != nil {
					e.openWhiteboard(d.boardURL, true)
					return true
				}
			}
		}
		return false
	}
	if !e.ReadOnly && e.saveBlockAsWhiteboard != nil && d.rect.W >= 370 && in(boardSaveFileButtonRect(d)) {
		if b := e.blockByID(d.id); b != nil {
			if scene, err := e.whiteboardSceneFor(d.id, b.Text()); err == nil {
				e.saveBlockAsWhiteboard(d.id, scene.Clone())
			}
		}
		return true
	}
	if !e.ReadOnly && in(boardEditButtonRect(d)) {
		e.startBoardEditing(d.id)
		return true
	}
	if !e.ReadOnly && in(boardExpandButtonRect(d)) {
		e.openBoardModal(d.id)
		return true
	}
	if in(boardCopySVGButtonRect(d)) {
		if b := e.blockByID(d.id); b != nil {
			if scene, err := e.whiteboardSceneFor(d.id, b.Text()); err == nil && e.services != (ui.Services{}) {
				e.services.WriteClipboard(whiteboard.ExportSVG(scene, whiteboard.DefaultLightPalette()))
				e.copiedID, e.copiedAt = d.id, e.lastInput
			}
		}
		return true
	}
	return false
}

// boardControls renders interactive inline BoardView overlays and rest-mode hit
// boxes, context menus, and hover action pills for visible whiteboards.
func (e *Editor) boardControls(c *ui.Context, o Options) {
	if e.printing {
		return
	}
	t := o.Theme
	services := c.Services()

	for i := range e.visibleRows(1) {
		r := *e.indexedRow(i)
		if !isWhiteboardBlock(r.leaf.Block) && !r.boardEmbed {
			continue
		}
		if r.boardRect.W <= 0 {
			continue
		}
		b := r.leaf.Block
		id := b.ID
		dy := e.ownerVisualDY(r.leaf.Owner)
		box := r.boardRect
		box.Y += dy

		if !e.ReadOnly && r.boardEditing && id == e.boardEditing && e.boardModalID != id {
			board := e.boardForBlock(b)
			if board == nil {
				continue
			}
			if e.boardFocusPending {
				board.RequestFocus = true
				e.boardFocusPending = false
			}

			card := ui.Column(c.Key("inline-board:"+id)).Absolute().Left(box.X).Top(box.Y).Size(box.W, box.H).
				Radius(10).Background(e.page.Whiteboard.Canvas).Border(1.25, t.Accent.Alpha(0.45)).
				Shadow(0, 4, 18, 0, e.page.MathShadow).Clip()
			card.Children(func() {
				BoardView(c, board, BoardOptions{
					Theme:             t,
					Page:              e.page,
					Compact:           true,
					ReadOnly:          e.ReadOnly,
					WikiSuggest:       e.boardWikiSuggest,
					OpenWiki:          e.openWikiSplit,
					UserLibrary:       o.UserWhiteboardLib,
					SaveUserLibrary:   o.SaveWhiteboardLib,
					ImportLibraryFile: o.ImportWhiteboardLib,
					ExpandModal:       func() { e.openBoardModal(id) },
					Done: func() {
						e.commitBoardToBlock(id, board)
						e.boardEditing = ""
						e.ready = false
						e.RequestFocus = true
					},
					OnChange: func() {
						e.commitBoardToBlock(id, board)
					},
				})
			})

			// Bottom resize handle bar to adjust inline canvas height.
			grip := ui.Box(c.Key("board-resize:"+id)).Absolute().Left(box.X+box.W/2-36).Top(box.Y+box.H-5).
				Size(72, 10).Radius(5).Cursor(ui.CursorResizeNS).Tooltip(i18n.T("拖动调整画布高度"))
			if grip.Hovered() || e.boardDragID == id {
				grip.Background(t.Accent.Alpha(0.28))
			} else {
				grip.Background(e.page.CodeBorder)
			}
			_, dragDY, pressed := grip.Dragged()
			if pressed {
				if e.boardDragID != id {
					e.boardDragID = id
					e.boardDragStartH = box.H
				}
				if e.boardHeights == nil {
					e.boardHeights = map[string]float32{}
				}
				next := max(boardMinEditH, min(boardMaxEditH, e.boardHeights[id]+dragDY))
				if e.boardHeights[id] == 0 {
					next = max(boardMinEditH, min(boardMaxEditH, e.boardDragStartH+dragDY))
				}
				e.boardHeights[id] = next
				e.ready = false
			} else if e.boardDragID == id {
				e.boardDragID = ""
			}
			continue
		}

		label := i18n.T("手绘白板")
		if r.boardEmbed && b.URL != "" {
			label = i18n.T("手绘白板") + ": " + path.Base(b.URL)
		}
		hit := ui.Box(c.Key("board-rest:"+id)).Absolute().Left(box.X).Top(box.Y).
			Size(box.W, max(24, box.H)).Focusable().Role(ui.RoleButton).
			Label(label).Cursor(ui.CursorPointer)
		if hit.Clicked() {
			e.clearBlocks()
			if e.imageCaptionID != "" {
				e.imageCaptionID, e.ready = "", false
			}
			e.FocusBlock(id)
		}
		if hit.DoubleClicked() {
			if !e.ReadOnly && (isWhiteboardBlock(b) || (isWhiteboardEmbed(b) && e.saveWhiteboard != nil)) {
				e.startBoardEditing(id)
			} else if r.boardEmbed && e.openWhiteboard != nil {
				e.openWhiteboard(b.URL, false)
			}
		}
		if hit.Focused() {
			radius := float32(10)
			if r.boardError != nil {
				radius = 8
			}
			hit.Border(2, t.Accent).Radius(radius)
		}
		hit.HandleInput(func(ev ui.InputEvent) bool {
			switch ev.Kind {
			case ui.InputCommand:
				switch ev.Text {
				case "delete":
					if e.ReadOnly {
						e.refused = true
						return true
					}
					e.deleteAtomicBlock(id, true)
					e.RequestFocus = true
					return true
				case "cut":
					e.copySingleBlock(b, services.WriteClipboard)
					if e.ReadOnly {
						e.refused = true
					} else {
						e.deleteAtomicBlock(id, true)
					}
					e.RequestFocus = true
					return true
				case "copy":
					e.copySingleBlock(b, services.WriteClipboard)
					return true
				case "undo":
					e.Undo()
					e.RequestFocus = true
					return true
				case "redo":
					e.Redo()
					e.RequestFocus = true
					return true
				case "selectAll":
					e.selectAll()
					e.RequestFocus = true
					return true
				}
				return false
			case ui.InputKeyDown:
				for _, res := range e.reserved {
					if res.Mods == ev.Mods && res.Key == ev.Key {
						return false
					}
				}
				switch {
				case ev.Key == ui.KeyBackspace || ev.Key == ui.KeyDelete:
					if e.ReadOnly {
						e.refused = true
						return true
					}
					e.deleteAtomicBlock(id, ev.Key == ui.KeyBackspace)
					e.RequestFocus = true
					return true
				case ev.Key == ui.KeyEnter && ev.Mods == 0:
					if !e.ReadOnly && (isWhiteboardBlock(b) || (isWhiteboardEmbed(b) && e.saveWhiteboard != nil)) {
						e.startBoardEditing(id)
						return true
					}
					if r.boardEmbed && e.openWhiteboard != nil {
						e.openWhiteboard(b.URL, false)
						return true
					}
					return false
				case ev.Key == ui.KeyEscape && ev.Mods == 0:
					e.RequestFocus = true
					return true
				case (ev.Key == ui.KeyLeft || ev.Key == ui.KeyUp) && ev.Mods == 0:
					e.SetSelection(max(0, r.leaf.Start-1), max(0, r.leaf.Start-1))
					e.RequestFocus = true
					return true
				case (ev.Key == ui.KeyRight || ev.Key == ui.KeyDown) && ev.Mods == 0:
					e.SetSelection(min(e.Len(), r.leaf.End+1), min(e.Len(), r.leaf.End+1))
					e.RequestFocus = true
					return true
				case ev.Mods == ui.Cmd && ev.Key == ui.KeyX:
					e.copySingleBlock(b, services.WriteClipboard)
					if e.ReadOnly {
						e.refused = true
					} else {
						e.deleteAtomicBlock(id, true)
					}
					e.RequestFocus = true
					return true
				case ev.Mods == ui.Cmd && ev.Key == ui.KeyC:
					e.copySingleBlock(b, services.WriteClipboard)
					return true
				case ev.Mods == ui.Cmd && ev.Key == ui.KeyZ:
					e.Undo()
					e.RequestFocus = true
					return true
				case ev.Mods == ui.Cmd|ui.Shift && ev.Key == ui.KeyZ:
					e.Redo()
					e.RequestFocus = true
					return true
				case ev.Mods == ui.Cmd && ev.Key == ui.KeyA:
					e.selectAll()
					e.RequestFocus = true
					return true
				}
			}
			return false
		})
		hit.ContextMenu(func(m *ui.Menu) {
			hasItems := false
			if r.boardEmbed {
				if !e.ReadOnly && e.saveWhiteboard != nil {
					menuAction(m, i18n.T("编辑画布"), func() { e.startBoardEditing(id) })
					menuAction(m, i18n.T("放大画布"), func() { e.openBoardModal(id) })
					hasItems = true
				}
				if e.openWhiteboard != nil {
					menuAction(m, i18n.T("打开白板"), func() { e.openWhiteboard(b.URL, false) })
					menuAction(m, i18n.T("在分屏中编辑"), func() { e.openWhiteboard(b.URL, true) })
					hasItems = true
				}
				if o.RevealImage != nil {
					if hasItems {
						m.Separator()
					}
					menuAction(m, i18n.T("在文件夹中显示"), func() { o.RevealImage(b.URL) })
					hasItems = true
				}
			} else {
				if !e.ReadOnly {
					menuAction(m, i18n.T("编辑画布"), func() { e.startBoardEditing(id) })
					menuAction(m, i18n.T("放大画布"), func() { e.openBoardModal(id) })
					hasItems = true
					if e.saveBlockAsWhiteboard != nil && r.boardError == nil {
						menuAction(m, i18n.T("存为独立白板"), func() {
							if scene, err := e.whiteboardSceneFor(id, b.Text()); err == nil {
								e.saveBlockAsWhiteboard(id, scene.Clone())
							}
						})
					}
				}
				if r.boardError == nil {
					menuAction(m, i18n.T("复制 SVG"), func() {
						if scene, err := e.whiteboardSceneFor(id, b.Text()); err == nil && services != (ui.Services{}) {
							services.WriteClipboard(whiteboard.ExportSVG(scene, whiteboard.DefaultLightPalette()))
							e.copiedID, e.copiedAt = id, c.Now()
						}
					})
					hasItems = true
				}
			}
			if !e.ReadOnly {
				if hasItems {
					m.Separator()
				}
				menuAction(m, i18n.T("删除"), func() {
					e.deleteAtomicBlock(id, true)
					e.RequestFocus = true
				})
			}
		})

		type restPill struct {
			key    string
			rect   ui.Rect
			icon   *ui.SVG
			label  string
			active bool
			run    func()
		}
		var items []restPill
		copied := id == e.copiedID && c.Now().Sub(e.copiedAt) < 1500*time.Millisecond
		if copied {
			c.After(1500*time.Millisecond - c.Now().Sub(e.copiedAt))
		}
		dec := decoration{rect: box}
		if r.boardEmbed {
			embedPills := e.embedHoverPills(dec)
			for j := len(embedPills) - 1; j >= 0; j-- {
				pill := embedPills[j]
				key := "pill"
				var run func()
				switch pill.action {
				case hoverEdit:
					key = "edit"
					run = func() { e.startBoardEditing(id) }
				case hoverExpand:
					key = "expand"
					run = func() { e.openBoardModal(id) }
				case hoverOpen:
					key = "open"
					run = func() {
						if e.openWhiteboard != nil {
							e.openWhiteboard(b.URL, false)
						}
					}
				case hoverSplit:
					key = "split"
					run = func() {
						if e.openWhiteboard != nil {
							e.openWhiteboard(b.URL, true)
						}
					}
				}
				items = append(items, restPill{key: key, rect: pill.rect, icon: pill.icon, label: pill.label, run: run})
			}
		} else {
			if !e.ReadOnly {
				if e.saveBlockAsWhiteboard != nil && box.W >= 370 {
					items = append(items, restPill{
						key:   "save",
						rect:  boardSaveFileButtonRect(dec),
						icon:  iconWhiteboard,
						label: i18n.T("存为独立白板"),
						run: func() {
							if scene, err := e.whiteboardSceneFor(id, b.Text()); err == nil {
								e.saveBlockAsWhiteboard(id, scene.Clone())
							}
						},
					})
				}
				items = append(items,
					restPill{
						key:   "edit",
						rect:  boardEditButtonRect(dec),
						icon:  iconPencil,
						label: i18n.T("编辑画布"),
						run:   func() { e.startBoardEditing(id) },
					},
					restPill{
						key:   "expand",
						rect:  boardExpandButtonRect(dec),
						icon:  iconMaximize,
						label: i18n.T("放大画布"),
						run:   func() { e.openBoardModal(id) },
					},
				)
			}
			copyIcon, copyLabel := iconCopy, i18n.T("复制 SVG")
			if copied {
				copyIcon, copyLabel = iconCheck, i18n.T("已复制")
			}
			items = append(items, restPill{
				key:    "copy",
				rect:   boardCopySVGButtonRect(dec),
				icon:   copyIcon,
				label:  copyLabel,
				active: copied,
				run: func() {
					if scene, err := e.whiteboardSceneFor(id, b.Text()); err == nil && services != (ui.Services{}) {
						services.WriteClipboard(whiteboard.ExportSVG(scene, whiteboard.DefaultLightPalette()))
						e.copiedID, e.copiedAt = id, c.Now()
					}
				},
			})
		}

		wb := e.page.Whiteboard
		pillsHovered := false
		if e.boardModalID == "" && len(items) > 0 {
			minX := items[0].rect.X
			maxX := items[len(items)-1].rect.X + items[len(items)-1].rect.W
			pillsRow := ui.Row(c.Key("board-pills:" + id)).Absolute().Left(minX).Top(box.Y + 8).
				Width(maxX - minX).Height(24).Gap(6).AlignItems(ui.Center)
			showPills := hit.Hovered() || pillsRow.Hovered() || hit.Focused() || copied
			pillsHovered = pillsRow.Hovered()
			if !showPills {
				pillsRow.Invisible()
			}
			pillsRow.Children(func() {
				for _, item := range items {
					it := item
					btn := ui.ButtonBase(c.Key(it.key)).Size(it.rect.W, it.rect.H).PaddingX(6).Gap(3).Radius(6).
						AlignItems(ui.Center).Border(1, wb.IslandBorder).Cursor(ui.CursorPointer)
					bg := wb.IslandFill
					fg := e.page.Caption
					if it.active {
						bg = wb.ActiveFill
						fg = wb.ActiveInk
					} else if showPills && btn.Hovered() {
						bg = e.page.Hover
						fg = t.Text
					}
					btn.Background(bg)
					btn.Children(func() {
						ui.Icon(c, it.icon).FontSize(13).TextColor(fg)
						ui.Text(c, it.label).FontSize(11.5).FontWeight(500).TextColor(fg).MinWidth(0).SingleLine()
					})
					if showPills && btn.Clicked() && it.run != nil {
						it.run()
					}
				}
			})
		}
		if hit.Hovered() || pillsHovered {
			e.boardHoverID = id
			e.hoverID = r.leaf.Owner
		}
	}
}

// boardModalView opens a full-window Excalidraw modal over the document.
func (e *Editor) boardModalView(c *ui.Context, o Options) {
	if e.boardModalID == "" || e.printing {
		return
	}
	b := e.blockByID(e.boardModalID)
	if b == nil {
		e.boardModalID = ""
		return
	}
	board := e.boardForBlock(b)
	if board == nil {
		e.boardModalID = ""
		return
	}
	if e.boardModalFocusPending {
		board.RequestFocus = true
		e.boardModalFocusPending = false
	}
	t := o.Theme
	winW, winH := c.Size()
	w := min(float32(1240), max(320, winW-48))
	h := min(float32(840), max(280, winH-48))

	ui.Overlay(c, func() {
		backdrop := ui.Box(c.Key("board-modal-bg")).Fill().Background(e.page.Scrim).Center()
		if c.Shortcut(0, ui.KeyEscape) {
			e.closeBoardModal(b.ID, board)
			return
		}
		backdrop.Children(func() {
			panel := ui.Column(c.Key("board-modal-panel")).Size(w, h).Radius(14).
				Background(e.page.Whiteboard.Canvas).Border(1, e.page.Whiteboard.IslandBorder).
				Shadow(0, 16, 48, 0, e.page.MathShadow).Clip()
			panel.Children(func() {
				// Top bar with "Save as standalone file" and "Done".
				hdr := ui.Row(c).Height(42).PaddingX(14).Gap(8).AlignItems(ui.Center).
					Background(e.page.Whiteboard.IslandFill).Border(1, e.page.Whiteboard.IslandBorder)
				hdr.Children(func() {
					ui.Icon(c, iconWhiteboard).FontSize(16).TextColor(e.page.Whiteboard.ActiveInk)
					ui.Text(c, i18n.T("手绘白板")).FontSize(13).FontWeight(600).TextColor(t.Text).Grow(1)
					if isWhiteboardBlock(b) && e.saveBlockAsWhiteboard != nil && !e.ReadOnly {
						saveBtn := ui.ButtonBase(c.Key("board-save-as")).Height(28).PaddingX(10).Radius(7).
							Border(1, e.page.Whiteboard.IslandBorder).AlignItems(ui.Center).Cursor(ui.CursorPointer)
						if saveBtn.Hovered() {
							saveBtn.Background(t.SurfaceHover)
						}
						saveBtn.Children(func() {
							ui.Text(c, i18n.T("存为独立白板")).FontSize(12).TextColor(t.Text)
						})
						if saveBtn.Clicked() {
							e.commitBoardToBlock(b.ID, board)
							snap := board.SceneSnapshot()
							id := b.ID
							e.boardModalID = ""
							e.boardEditing = ""
							e.saveBlockAsWhiteboard(id, snap)
						}
					}
					done := ui.ButtonBase(c.Key("board-modal-done")).Height(28).PaddingX(12).Radius(7).
						Background(t.Accent).Center().Cursor(ui.CursorPointer)
					done.Children(func() {
						ui.Text(c, i18n.T("完成")).FontSize(12).FontWeight(600).TextColor(t.AccentText)
					})
					if done.Clicked() {
						e.closeBoardModal(b.ID, board)
					}
				})
				ui.Column(c).Grow(1).MinHeight(0).MinWidth(0).Children(func() {
					BoardView(c, board, BoardOptions{
						Theme:             t,
						Page:              e.page,
						Compact:           false,
						ReadOnly:          e.ReadOnly,
						WikiSuggest:       e.boardWikiSuggest,
						OpenWiki:          e.openWikiSplit,
						UserLibrary:       o.UserWhiteboardLib,
						SaveUserLibrary:   o.SaveWhiteboardLib,
						ImportLibraryFile: o.ImportWhiteboardLib,
						OnChange: func() {
							e.commitBoardToBlock(b.ID, board)
						},
					})
				})
			})
		})
	})
}

// ExportWhiteboardPDF draws scene into a vector PDF page mapped inside dst rect.
func ExportWhiteboardPDF(vp *pdf.VectorPage, scene *whiteboard.Scene, pg Page, t *ui.Theme, dstX, dstY, dstW, dstH, sc float64) {
	if vp == nil || scene == nil || dstW <= 0 || dstH <= 0 {
		return
	}
	wb := pg.Whiteboard
	sb := whiteboard.SceneBounds(scene)
	if sb.Empty() {
		return
	}
	pad := 16.0 * sc
	availW := max(8, dstW-2*pad)
	availH := max(8, dstH-2*pad)
	scale := min(availW/float64(sb.W), availH/float64(sb.H))
	ox := dstX + (dstW-float64(sb.W)*scale)/2 - float64(sb.X)*scale
	oy := dstY + (dstH-float64(sb.H)*scale)/2 - float64(sb.Y)*scale

	vp.PushClip(dstX, dstY, dstW, dstH, 6*sc)
	defer vp.PopClip()

	for _, passFrame := range []bool{true, false} {
		for _, el := range scene.Elements {
			if el == nil || (el.Kind == whiteboard.KindFrame) != passFrame {
				continue
			}
			for _, pr := range whiteboard.BuildPrimitives(scene, el) {
				segs := make([]pdf.PathSegment, 0, len(pr.Cmds))
				for _, cmd := range pr.Cmds {
					var s pdf.PathSegment
					switch cmd.Kind {
					case whiteboard.CmdMoveTo:
						s = pdf.PathSegment{Op: 0, X0: ox + float64(cmd.X)*scale, Y0: oy + float64(cmd.Y)*scale}
					case whiteboard.CmdLineTo:
						s = pdf.PathSegment{Op: 1, X0: ox + float64(cmd.X)*scale, Y0: oy + float64(cmd.Y)*scale}
					case whiteboard.CmdQuadTo:
						s = pdf.PathSegment{Op: 2, X0: ox + float64(cmd.C1X)*scale, Y0: oy + float64(cmd.C1Y)*scale, X1: ox + float64(cmd.X)*scale, Y1: oy + float64(cmd.Y)*scale}
					case whiteboard.CmdCubeTo:
						s = pdf.PathSegment{Op: 3, X0: ox + float64(cmd.C1X)*scale, Y0: oy + float64(cmd.C1Y)*scale, X1: ox + float64(cmd.C2X)*scale, Y1: oy + float64(cmd.C2Y)*scale, X2: ox + float64(cmd.X)*scale, Y2: oy + float64(cmd.Y)*scale}
					case whiteboard.CmdClose:
						s = pdf.PathSegment{Op: 4}
					}
					segs = append(segs, s)
				}
				var fill, stroke [4]uint8
				if pr.Filled {
					fill = pdfRGBA(resolveBoardFill(wb, pr.FillColor, t))
				}
				if pr.Stroked {
					if pr.IsHachure {
						fc := resolveBoardFill(wb, pr.StrokeColor, t)
						stroke = pdfRGBA(fc.Mix(resolveBoardStroke(wb, pr.StrokeColor, t), 0.34))
					} else {
						stroke = pdfRGBA(resolveBoardStroke(wb, pr.StrokeColor, t))
					}
				}
				vp.DrawPath(segs, fill, stroke, max(0.4, float64(pr.StrokeWidth)*scale), len(pr.Dash) > 0)
			}
			if txt := strings.TrimSpace(el.Text); txt != "" {
				eb := whiteboard.ShapeTextBox(el)
				fs := float64(max(11, el.FontSize)) * scale
				cx := ox + float64(eb.X+eb.W/2)*scale
				cy := oy + float64(eb.Y+eb.H/2)*scale
				lines := strings.Split(txt, "\n")
				lh := fs * 1.3
				startY := cy - float64(len(lines)-1)*lh/2 + fs*0.35
				col := pdfRGBA(resolveBoardStroke(wb, el.StrokeColor, t))
				for i, line := range lines {
					w := estimateVectorTextWidth(line, fs)
					x := cx - w/2
					if el.Kind == whiteboard.KindText || el.Kind == whiteboard.KindNote {
						x = ox + float64(eb.X+10)*scale
					}
					vp.DrawText(line, x, startY+float64(i)*lh, w, fs, pdf.TextStyle{Family: pdf.FontSans}, col)
				}
			}
		}
	}
}
