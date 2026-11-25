package dbview

import (
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/egoist/mygo/ui"
)

// Board drag-and-drop physics matching ArcMD's sidebar treedrag.go:
// dragging lifts the real card onto a crisp floating overlay while the source
// collapses to zero footprint (eliminating MyGo's duplicate translucent ghost)
// and a dashed slot glides live across columns/rows so neighboring cards part
// smoothly; releasing glides the floating card into its slot as the outline
// fades away.

const (
	boardDragLiftDur   = 120 * time.Millisecond
	boardDragSettleDur = 220 * time.Millisecond
)

var (
	boardListEase = ui.CubicBezier(0.2, 0.9, 0.25, 1)
	boardListAnim = ui.Animation{Duration: 220 * time.Millisecond, Ease: boardListEase}
)

type boardCardFrame struct {
	recordID string
	groupKey string
	subKey   string
	index    int // resting index in this bucket (excluding dragged card)
	r        ui.Rect
	target   ui.Rect
	slot     bool
	slotSelf bool
}

type boardBucketFrame struct {
	groupKey  string
	groupVal  database.Value
	subKey    string
	subVal    database.Value
	collapsed bool
	r         ui.Rect
	accent    ui.Color
	count     int // resting cards count (excluding dragged card)
}

type boardDropTarget struct {
	active         bool
	groupKey       string
	groupVal       database.Value
	subKey         string
	subVal         database.Value
	index          int
	beforeRecordID string
	afterRecordID  string
	self           bool
	collapsed      bool
}

type boardDragGrab struct {
	active        bool
	seen          bool
	dropped       bool
	hasDropTarget bool
	recordID      string
	fromGroupKey  string
	fromSubKey    string
	toGroupKey    string
	toSubKey      string
	fromIndex     int
	cardW, cardH  float32
	grabX, grabY  float32
	x, y          float32
	accent        ui.Color
	started       time.Time
}

func (g boardDragGrab) matchesCard(recordID, groupKey, subKey string) bool {
	return g.active && g.recordID == recordID && g.fromGroupKey == groupKey && g.fromSubKey == subKey
}

type boardDragSettle struct {
	active       bool
	hasTarget    bool
	recordID     string
	toGroupKey   string
	toSubKey     string
	fromX, fromY float32
	fromW, fromH float32
	toRect       ui.Rect
	accent       ui.Color
	started      time.Time
}

func (s boardDragSettle) matchesCard(recordID, groupKey, subKey string) bool {
	return s.active && s.recordID == recordID && s.toGroupKey == groupKey && s.toSubKey == subKey
}

type boardOverlayContext struct {
	view      database.ViewConfig
	db        *database.Database
	res       *database.QueryResult
	bcfg      database.BoardViewConfig
	covH      float32
	cardProps []database.Property
	opts      Options
	pal       Palette
}

type boardDragState struct {
	grab        boardDragGrab
	settle      boardDragSettle
	slot        boardDropTarget
	emitted     bool
	prevCards   []boardCardFrame
	curCards    []boardCardFrame
	prevBuckets []boardBucketFrame
	curBuckets  []boardBucketFrame
	overlayCtx  *boardOverlayContext
}

func (b *boardDragState) reset() {
	if b == nil {
		return
	}
	b.grab = boardDragGrab{}
	b.settle = boardDragSettle{}
	b.slot = boardDropTarget{}
	b.emitted = false
	b.prevCards = nil
	b.curCards = nil
	b.prevBuckets = nil
	b.curBuckets = nil
	b.overlayCtx = nil
}

func (b *boardDragState) beginBoardFrame(c *ui.Context, st *State, view database.ViewConfig, ctx *boardOverlayContext) {
	b.overlayCtx = ctx
	b.prevCards = append(b.prevCards[:0], b.curCards...)
	b.curCards = b.curCards[:0]
	b.prevBuckets = append(b.prevBuckets[:0], b.curBuckets...)
	b.curBuckets = b.curBuckets[:0]
	b.grab.seen = false
	b.grab.dropped = false
	b.emitted = false

	reduce := c.Preferences().ReduceMotion
	if reduce {
		b.settle = boardDragSettle{}
	} else if b.settle.active {
		elapsed := c.Now().Sub(b.settle.started)
		if elapsed >= boardDragSettleDur || (!b.settle.hasTarget && elapsed > 80*time.Millisecond) {
			b.settle = boardDragSettle{}
		} else {
			st.Motion.suppressOverlay = true
		}
	}

	if b.grab.active {
		px, py, inside := c.Root().PointerPosition()
		if inside {
			b.grab.x = px - b.grab.grabX
			b.grab.y = py - b.grab.grabY
		}
		b.slot = resolveBoardDrop(b.grab, b.slot, px, py, len(view.Sorts) > 0, b.prevBuckets, b.prevCards)
		// Keep st.Motion.drag synchronized for tests and shared motion queries.
		st.Motion.drag.active = true
		st.Motion.drag.seenThisFrame = true
		st.Motion.drag.kind = dragKindCard
		st.Motion.drag.id = b.grab.recordID
		st.Motion.drag.x = b.grab.x
		st.Motion.drag.y = b.grab.y
		st.Motion.drag.cardW = b.grab.cardW
		st.Motion.drag.cardH = b.grab.cardH
		st.Motion.suppressOverlay = true
	} else {
		b.slot = boardDropTarget{}
	}
}

func resolveBoardDrop(
	grab boardDragGrab,
	prevSlot boardDropTarget,
	px, py float32,
	hasSorts bool,
	buckets []boardBucketFrame,
	cards []boardCardFrame,
) boardDropTarget {
	selfSlot := func() boardDropTarget {
		return boardDropTarget{
			active:   true,
			groupKey: grab.fromGroupKey,
			subKey:   grab.fromSubKey,
			index:    grab.fromIndex,
			self:     true,
		}
	}
	if len(buckets) == 0 {
		return selfSlot()
	}

	// Find bucket under pointer, allowing a small 6 DIP tolerance across column gaps
	// so the slot never flickers off while crossing between adjacent columns.
	var bucket *boardBucketFrame
	bestDist := float32(1e9)
	for i := range buckets {
		bk := &buckets[i]
		if bk.r.W <= 0 || bk.r.H <= 0 {
			continue
		}
		if px >= bk.r.X && px <= bk.r.X+bk.r.W && py >= bk.r.Y && py <= bk.r.Y+bk.r.H {
			bucket = bk
			break
		}
		if px >= bk.r.X-7 && px <= bk.r.X+bk.r.W+7 && py >= bk.r.Y-7 && py <= bk.r.Y+bk.r.H+7 {
			cx := clampF32(px, bk.r.X, bk.r.X+bk.r.W)
			cy := clampF32(py, bk.r.Y, bk.r.Y+bk.r.H)
			dx, dy := px-cx, py-cy
			dist := dx*dx + dy*dy
			if dist < bestDist {
				bestDist = dist
				bucket = bk
			}
		}
	}
	if bucket == nil {
		return selfSlot()
	}

	sameBucket := bucket.groupKey == grab.fromGroupKey && bucket.subKey == grab.fromSubKey
	if bucket.collapsed {
		if sameBucket {
			return selfSlot()
		}
		return boardDropTarget{
			active:    true,
			groupKey:  bucket.groupKey,
			groupVal:  bucket.groupVal,
			subKey:    bucket.subKey,
			subVal:    bucket.subVal,
			collapsed: true,
		}
	}

	// Collect resting cards and any currently open slot in this bucket.
	var resting []boardCardFrame
	var activeSlot *boardCardFrame
	for i := range cards {
		cf := cards[i]
		if cf.groupKey != bucket.groupKey || cf.subKey != bucket.subKey {
			continue
		}
		if cf.slot {
			cCopy := cf
			activeSlot = &cCopy
			continue
		}
		if cf.recordID == grab.recordID && cf.groupKey == grab.fromGroupKey && cf.subKey == grab.fromSubKey {
			continue
		}
		resting = append(resting, cf)
	}

	// When the view has active sorts, manual intra-column ordering is disabled.
	if hasSorts {
		if sameBucket {
			return selfSlot()
		}
		return boardDropTarget{
			active:   true,
			groupKey: bucket.groupKey,
			groupVal: bucket.groupVal,
			subKey:   bucket.subKey,
			subVal:   bucket.subVal,
			index:    0,
		}
	}

	makeTarget := func(idx int) boardDropTarget {
		idx = min(max(idx, 0), len(resting))
		var beforeID, afterID string
		if idx < len(resting) {
			beforeID = resting[idx].recordID
		}
		if idx > 0 {
			afterID = resting[idx-1].recordID
		}
		isSelf := sameBucket && idx == grab.fromIndex
		return boardDropTarget{
			active:         true,
			groupKey:       bucket.groupKey,
			groupVal:       bucket.groupVal,
			subKey:         bucket.subKey,
			subVal:         bucket.subVal,
			index:          idx,
			beforeRecordID: beforeID,
			afterRecordID:  afterID,
			self:           isSelf,
		}
	}

	if len(resting) == 0 {
		return makeTarget(0)
	}

	// Hysteresis: if the pointer rests inside the already-open slot for this
	// bucket (before crossing a neighboring resting card), hold the slot steady
	// so cards of different heights never ping-pong back and forth.
	const pad = float32(4)
	if activeSlot != nil && prevSlot.active && prevSlot.groupKey == bucket.groupKey && prevSlot.subKey == bucket.subKey {
		sr := activeSlot.target
		if sr.W <= 0 || sr.H <= 0 {
			sr = activeSlot.r
		}
		if sr.H > 0 && py >= sr.Y-pad && py <= sr.Y+sr.H+pad {
			return makeTarget(prevSlot.index)
		}
	}

	// Direct hit on a resting card: top half inserts before, bottom half after.
	for _, cf := range resting {
		r := cf.r
		if r.H <= 0 {
			r = cf.target
		}
		if r.H <= 0 {
			continue
		}
		if py >= r.Y-pad && py < r.Y+r.H+pad {
			if py < r.Y+r.H*0.5 {
				return makeTarget(cf.index)
			}
			return makeTarget(cf.index + 1)
		}
	}

	// Above first card -> top; below last card -> bottom; in a gap -> nearest split.
	for _, cf := range resting {
		r := cf.r
		if r.H <= 0 {
			r = cf.target
		}
		if r.H <= 0 {
			continue
		}
		if py < r.Y+r.H*0.5 {
			return makeTarget(cf.index)
		}
	}
	return makeTarget(len(resting))
}

func (b *boardDragState) startOrUpdateGrab(
	c *ui.Context,
	st *State,
	rec *database.Record,
	card ui.Element,
	fromGroupKey, fromSubKey string,
	fromIndex int,
	colAccent ui.Color,
) (justStarted bool) {
	now := c.Now()
	px, py, _ := c.Root().PointerPosition()
	g := &b.grab
	if !g.active || g.recordID != rec.ID || g.fromGroupKey != fromGroupKey || g.fromSubKey != fromSubKey {
		var rb ui.Rect
		for _, pf := range b.prevCards {
			if !pf.slot && pf.recordID == rec.ID && pf.groupKey == fromGroupKey && pf.subKey == fromSubKey {
				rb = pf.r
				break
			}
		}
		if rb.W <= 0 || rb.H <= 0 {
			rb = card.Bounds()
		}
		if rb.W <= 0 {
			rb.W = 252
		}
		if rb.H <= 0 {
			rb.H = 96
		}
		gx := clampF32(px-rb.X, 12, max(12, rb.W-12))
		gy := clampF32(py-rb.Y, 10, max(10, rb.H-10))
		*g = boardDragGrab{
			active:       true,
			seen:         true,
			recordID:     rec.ID,
			fromGroupKey: fromGroupKey,
			fromSubKey:   fromSubKey,
			toGroupKey:   fromGroupKey,
			toSubKey:     fromSubKey,
			fromIndex:    fromIndex,
			cardW:        rb.W,
			cardH:        rb.H,
			grabX:        gx,
			grabY:        gy,
			x:            px - gx,
			y:            py - gy,
			accent:       colAccent,
			started:      now,
		}
		b.settle = boardDragSettle{}
		b.slot = boardDropTarget{
			active:   true,
			groupKey: fromGroupKey,
			subKey:   fromSubKey,
			index:    fromIndex,
			self:     true,
		}
		justStarted = true
	} else {
		g.seen = true
		g.x = px - g.grabX
		g.y = py - g.grabY
		g.accent = colAccent
	}

	// Keep st.Motion.drag mirrored so existing tests and shared motion state agree.
	st.Motion.drag = dbDragPreview{
		active:        true,
		seenThisFrame: true,
		kind:          dragKindCard,
		id:            rec.ID,
		title:         rec.Title,
		subtitle:      fromGroupKey,
		accent:        colAccent,
		hasAccent:     true,
		cardW:         g.cardW,
		cardH:         g.cardH,
		grabX:         g.grabX,
		grabY:         g.grabY,
		x:             g.x,
		y:             g.y,
		started:       g.started,
		lastAt:        now,
	}
	st.Motion.settle = dbDragSettle{}
	st.Motion.suppressOverlay = true
	return justStarted
}

func (b *boardDragState) startSettle(c *ui.Context, st *State) {
	if !b.grab.active {
		return
	}
	g := b.grab
	b.grab = boardDragGrab{}
	b.slot = boardDropTarget{}
	st.Motion.drag = dbDragPreview{}

	if c.Preferences().ReduceMotion || st.Motion.reduceMotion {
		b.settle = boardDragSettle{}
		st.Motion.settle = dbDragSettle{}
		st.Motion.suppressOverlay = false
		return
	}

	now := c.Now()
	toGroupKey := g.fromGroupKey
	toSubKey := g.fromSubKey
	if g.dropped && g.hasDropTarget {
		toGroupKey = g.toGroupKey
		toSubKey = g.toSubKey
	}
	b.settle = boardDragSettle{
		active:     true,
		recordID:   g.recordID,
		toGroupKey: toGroupKey,
		toSubKey:   toSubKey,
		fromX:      g.x,
		fromY:      g.y,
		fromW:      g.cardW,
		fromH:      g.cardH,
		accent:     g.accent,
		started:    now,
	}
	st.Motion.settle = dbDragSettle{
		active:    true,
		kind:      dragKindCard,
		id:        g.recordID,
		accent:    g.accent,
		hasAccent: true,
		fromX:     g.x,
		fromY:     g.y,
		fromW:     g.cardW,
		fromH:     g.cardH,
		started:   now,
	}
	st.Motion.suppressOverlay = true
	c.AnimationFrame()
}

func (b *boardDragState) handleBoardDrop(
	c *ui.Context,
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	drag boardCardDrag,
	fallbackGroupKey string,
	fallbackGroupVal database.Value,
	fallbackSubKey string,
	fallbackSubVal database.Value,
	fallbackCollapsed bool,
	opts Options,
) {
	if drag.state != nil && drag.state != st {
		return
	}
	if b.grab.dropped || opts.ReadOnly {
		return
	}
	b.grab.dropped = true

	target := b.slot
	if !target.active && fallbackGroupKey != "" {
		target = boardDropTarget{
			active:    true,
			groupKey:  fallbackGroupKey,
			groupVal:  fallbackGroupVal,
			subKey:    fallbackSubKey,
			subVal:    fallbackSubVal,
			collapsed: fallbackCollapsed,
		}
	}
	if target.active {
		b.grab.hasDropTarget = true
		b.grab.toGroupKey = target.groupKey
		b.grab.toSubKey = target.subKey
	}

	if target.active && !target.self {
		applyBoardCardDropAtSlot(st, view, db, res, drag, target, opts)
	}
	// Keep b.slot alive for the rest of this release frame so the target column's
	// cards stay rock-steady until the next frame renders the newly landed card;
	// finishBoardFrame transitions grab -> settle at the end of this frame.
}

func (b *boardDragState) finishBoardFrame(c *ui.Context, st *State) {
	if b.grab.active && (b.grab.dropped || !b.grab.seen) {
		b.startSettle(c, st)
	}
	if !b.grab.active && !b.settle.active {
		if st.Motion.suppressOverlay {
			st.Motion.suppressOverlay = false
		}
		return
	}
	if b.settle.active {
		elapsed := c.Now().Sub(b.settle.started)
		if c.Preferences().ReduceMotion || elapsed >= boardDragSettleDur {
			b.settle = boardDragSettle{}
			st.Motion.settle = dbDragSettle{}
			st.Motion.suppressOverlay = false
			return
		}
		if !b.settle.hasTarget && elapsed > 80*time.Millisecond {
			b.settle = boardDragSettle{}
			st.Motion.settle = dbDragSettle{}
			st.Motion.suppressOverlay = false
			return
		}
		st.Motion.settle.active = true
		st.Motion.settle.hasTarget = b.settle.hasTarget
		st.Motion.settle.toRect = b.settle.toRect
	}
	st.Motion.suppressOverlay = true

	b.renderOverlay(c, st)
}

func (b *boardDragState) renderDragSlot(c *ui.Context, accent ui.Color, groupKey, subKey string, index int, self bool) {
	b.emitted = true
	h := b.grab.cardH
	if h < 44 {
		h = 84
	}
	slot := ui.Box(c.Key("board-drag-slot")).Height(h).Radius(11).Background(ui.Transparent)
	var enter *ui.Motion
	if !self {
		enter = &ui.Motion{Scale: 0.97, Opacity: 0}
	}
	slot.Transition(ui.ElementTransition{
		Animation: &boardListAnim,
		Position:  true,
		Match:     "board-drag-slot",
		Enter:     enter,
	})
	b.curCards = append(b.curCards, boardCardFrame{
		groupKey: groupKey,
		subKey:   subKey,
		index:    index,
		r:        slot.Bounds(),
		target:   slot.LayoutBounds(),
		slot:     true,
		slotSelf: self,
	})
	fillCol, borderCol := boardSlotColors(accent, 1, b.overlayCtx != nil && b.overlayCtx.pal.Dark)
	slot.Draw(func(p *ui.Painter, r ui.Rect) {
		fr := rectInset(r, 0.5)
		if fillCol.A > 0 {
			p.Fill(fr, fillCol, 11)
		}
		p.StrokeDashed(fr, borderCol, 11, 1.25)
	})
}

func boardSlotColors(accent ui.Color, fade float32, dark bool) (fill, border ui.Color) {
	if fade <= 0.01 {
		return ui.Transparent, ui.Transparent
	}
	fillAlpha := float32(0.055)
	borderAlpha := float32(0.42)
	if dark {
		fillAlpha = 0.09
		borderAlpha = 0.50
	}
	return accent.Alpha(fillAlpha * fade), accent.Alpha(borderAlpha * fade)
}

func (b *boardDragState) renderOverlay(c *ui.Context, st *State) {
	ctx := b.overlayCtx
	if ctx == nil || st.Store == nil {
		return
	}
	now := c.Now()
	reduce := c.Preferences().ReduceMotion || st.Motion.reduceMotion

	switch {
	case b.grab.active:
		g := &b.grab
		rec := st.Store.Record(g.recordID)
		if rec == nil {
			return
		}
		lift := float32(1)
		if reduce {
			lift = 0
		} else {
			elapsed := now.Sub(g.started)
			if elapsed < boardDragLiftDur {
				lift = ui.EaseOut(clampF32(float32(elapsed)/float32(boardDragLiftDur), 0, 1))
				c.AnimationFrame()
			}
		}
		ui.Overlay(c, func() {
			renderFloatingBoardCard(c, st, ctx, rec, g.x, g.y, g.cardW, g.cardH, lift, g.accent)
		})

	case b.settle.active:
		s := &b.settle
		rec := st.Store.Record(s.recordID)
		if rec == nil {
			b.settle = boardDragSettle{}
			st.Motion.settle = dbDragSettle{}
			st.Motion.suppressOverlay = false
			return
		}
		elapsed := now.Sub(s.started)
		if !s.hasTarget {
			c.AnimationFrame()
			ui.Overlay(c, func() {
				renderFloatingBoardCard(c, st, ctx, rec, s.fromX, s.fromY, s.fromW, s.fromH, 0.85, s.accent)
			})
			return
		}
		c.AnimationFrame()
		k := boardListEase(clampF32(float32(elapsed)/float32(boardDragSettleDur), 0, 1))
		x := s.fromX + (s.toRect.X-s.fromX)*k
		y := s.fromY + (s.toRect.Y-s.fromY)*k
		w := s.fromW + (s.toRect.W-s.fromW)*k
		h := s.fromH + (s.toRect.H-s.fromH)*k
		lift := 1 - k
		ui.Overlay(c, func() {
			renderFloatingBoardCard(c, st, ctx, rec, x, y, w, h, lift, s.accent)
		})
	}
}

func renderFloatingBoardCard(
	c *ui.Context,
	st *State,
	ctx *boardOverlayContext,
	rec *database.Record,
	x, y, w, h, lift float32,
	colAccent ui.Color,
) {
	pal := ctx.pal
	cardBg := pal.Paper
	tintBg, tintAccent, tinted := tintedRowBackground(pal, ctx.res, rec.ID)
	if tinted {
		cardBg = tintBg
	}
	rimCol := pal.BorderStrong
	if lift > 0.01 {
		rimCol = pal.BorderStrong.Mix(colAccent, 0.35*lift)
	}
	shadowCol := pal.ShadowEdge.Mix(pal.PopoverShadow, clampF32(lift, 0, 1))

	roOpts := ctx.opts
	roOpts.ReadOnly = true

	grow := 1.5 * lift
	card := ui.Column(c.Key("board-floating-card")).
		Absolute().
		Left(x-grow).
		Top(y-grow*0.6).
		Width(max(120, w+2*grow)).
		MinHeight(max(44, h+1.2*grow)).
		Radius(11).
		Background(cardBg).
		Border(1, rimCol).
		Shadow(0, 2+12*lift, 6+20*lift, -2, shadowCol).
		IgnoreInput(true).
		Clip()

	if tinted {
		card.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 3, H: r.H}, tintAccent, 0)
		})
	}

	card.Children(func() {
		renderBoardCardContent(c, st, ctx.db, rec, ctx.bcfg, ctx.covH, ctx.cardProps, roOpts, pal)
	})
}

func computeManualOrderForSlot(
	st *State,
	view *database.ViewConfig,
	res *database.QueryResult,
	recordID, beforeRecordID, afterRecordID string,
) []string {
	if recordID == "" {
		return nil
	}
	var existing []string
	if view != nil {
		existing = view.ManualOrder
	}
	var allRecords []*database.Record
	if st != nil && st.Store != nil {
		allRecords = st.Store.Records()
	} else if res != nil {
		allRecords = res.Rows
	}
	if len(existing) == 0 && len(allRecords) == 0 {
		return nil
	}
	return database.SpliceManualOrder(existing, allRecords, recordID, beforeRecordID, afterRecordID)
}

func applyBoardCardDropAtSlot(
	st *State,
	view *database.ViewConfig,
	db *database.Database,
	res *database.QueryResult,
	drag boardCardDrag,
	target boardDropTarget,
	opts Options,
) {
	if st == nil || st.Store == nil || view == nil || db == nil || opts.ReadOnly {
		return
	}
	rec := st.Store.Record(drag.recordID)
	if rec == nil {
		return
	}

	var edits []database.CellEdit
	if view.Group != nil && view.Group.PropertyID != "" && drag.fromGroupKey != target.groupKey {
		if prop, idx := db.PropertyByID(view.Group.PropertyID); idx >= 0 {
			if nextVal, ok := resolveGroupDropValue(*prop, *view.Group, rec, drag.fromGroupKey, target.groupKey, target.groupVal, opts.Now); ok {
				edits = append(edits, database.CellEdit{
					RecordID: drag.recordID,
					PropID:   prop.ID,
					Value:    nextVal,
				})
			}
		}
	}

	if view.Board != nil && view.Board.SubGroup != nil && view.Board.SubGroup.PropertyID != "" && drag.fromSubKey != target.subKey {
		if subProp, idx := db.PropertyByID(view.Board.SubGroup.PropertyID); idx >= 0 {
			if nextVal, ok := resolveGroupDropValue(*subProp, *view.Board.SubGroup, rec, drag.fromSubKey, target.subKey, target.subVal, opts.Now); ok {
				edits = append(edits, database.CellEdit{
					RecordID: drag.recordID,
					PropID:   subProp.ID,
					Value:    nextVal,
				})
			}
		}
	}

	var nextOrder []string
	if len(view.Sorts) == 0 && !target.collapsed && (target.beforeRecordID != "" || target.afterRecordID != "") {
		nextOrder = computeManualOrderForSlot(st, view, res, drag.recordID, target.beforeRecordID, target.afterRecordID)
	}

	if len(edits) == 0 && len(nextOrder) == 0 {
		return
	}

	affected, err := st.Store.SetCellsAndManualOrder(edits, view.ID, nextOrder)
	if err != nil {
		if opts.OnNotify != nil {
			opts.OnNotify(err.Error())
		}
		return
	}
	if opts.OnChanged != nil {
		opts.OnChanged(affected)
	}
}
