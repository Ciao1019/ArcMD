package app

import (
	"cmp"
	"fmt"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/agent"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// Q&A inline source chips, supporting claim highlights, gliding preview card
// and collapsible stacked-monogram sources list, ported from Kokonut UI's
// ai-citations component (MIT).

const (
	qaPreviewOpenDelay  = 120 * time.Millisecond
	qaPreviewCloseDelay = 90 * time.Millisecond
	qaPreviewWarmWindow = 400 * time.Millisecond
	qaPreviewGlideDur   = 220 * time.Millisecond
	qaPreviewCardWidth  = float32(288)
	qaPreviewCardGuess  = float32(140)
	qaChipMaxRunes      = 12
	qaChipMonoSize      = float32(16)
	qaChipObjHeight     = float32(15.5)
	qaChipObjDescent    = float32(4.5)
)

type qaPreviewPlace uint8

const (
	qaPlaceLeft qaPreviewPlace = iota
	qaPlaceBelow
	qaPlaceAbove
)

var qaCiteEase = ui.CubicBezier(0.23, 1, 0.32, 1)

// citeRef is a cited range, numbered in the order the answer first cites it,
// enriched with its document title, folder breadcrumb, enclosing section and
// recalled plain-text excerpt.
type citeRef struct {
	n       int
	cite    agent.Citation
	title   string
	folder  string
	section string
	excerpt string
}

func citeKey(c agent.Citation) string { return fmt.Sprintf("%s#%d-%d", c.Path, c.From, c.To) }

type qaChipObservation struct {
	turn      *qaTurn
	part      int
	nums      []int
	anchor    ui.Rect
	immediate bool
}

type qaPreviewState struct {
	// Per-frame observation collected while building answer paragraphs.
	observed    *qaChipObservation
	panelRect   ui.Rect
	prevSources []int

	// Pending timers (driven by c.Now and c.After).
	pendingTurn *qaTurn
	pendingPart int
	openAt      time.Time
	closeAt     time.Time
	lastClosed  time.Time

	// Active preview card state.
	open   bool
	warm   bool
	turn   *qaTurn
	part   int
	nums   []int
	anchor ui.Rect
	place  qaPreviewPlace
	cardW  float32
	cardH  float32

	// Dismissed target (via Escape) stays suppressed until pointer/focus leaves it.
	dismissedTurn *qaTurn
	dismissedPart int

	// Smooth glide state when moving directly between chips while open.
	glideInit            bool
	glideAt              time.Time
	fromX, fromY         float32
	curX, curY           float32
	targetX, targetY     float32
	renderedX, renderedY float32
}

// beginCitationFrame resets per-frame hover observations before the QA list builds.
func (s *qaSession) beginCitationFrame(c *ui.Context) {
	p := &s.preview
	p.observed = nil
	if p.turn != nil && !slices.Contains(s.turns, p.turn) {
		*p = qaPreviewState{}
	}
	if p.pendingTurn != nil && !slices.Contains(s.turns, p.pendingTurn) {
		p.pendingTurn, p.pendingPart, p.openAt = nil, 0, time.Time{}
	}
	if p.dismissedTurn != nil && !slices.Contains(s.turns, p.dismissedTurn) {
		p.dismissedTurn, p.dismissedPart = nil, 0
	}
	if cap(p.prevSources) < len(s.turns) {
		p.prevSources = make([]int, len(s.turns))
	} else {
		p.prevSources = p.prevSources[:len(s.turns)]
	}
	for i, turn := range s.turns {
		p.prevSources[i] = turn.hoverSource
		turn.hoverSource = turn.nextHoverSource
		turn.nextHoverSource = 0
	}
	if (p.open || !p.openAt.IsZero()) && c.Shortcut(0, ui.KeyEscape) {
		if p.open {
			p.dismissedTurn, p.dismissedPart = p.turn, p.part
		} else {
			p.dismissedTurn, p.dismissedPart = p.pendingTurn, p.pendingPart
		}
		p.closeNow(c.Now())
	}
}

func (p *qaPreviewState) observeChip(turn *qaTurn, part int, nums []int, anchor ui.Rect, immediate bool) {
	if anchor.W <= 0 || len(nums) == 0 {
		return
	}
	// Prefer a freshly hovered/focused chip over anything already recorded.
	if p.observed == nil || immediate {
		p.observed = &qaChipObservation{
			turn:      turn,
			part:      part,
			nums:      nums,
			anchor:    anchor,
			immediate: immediate,
		}
	}
}

func (p *qaPreviewState) closeNow(now time.Time) {
	if p.open {
		p.lastClosed = now
	}
	p.open = false
	p.warm = false
	p.turn = nil
	p.part = 0
	p.nums = nil
	p.pendingTurn = nil
	p.pendingPart = 0
	p.openAt = time.Time{}
	p.closeAt = time.Time{}
	p.glideInit = false
	p.glideAt = time.Time{}
}

func (p *qaPreviewState) placeTarget(c *ui.Context, anchor ui.Rect) (x, y, cardW float32, place qaPreviewPlace) {
	winW, winH := c.Size()
	panelW := p.panelRect.W
	if panelW <= 0 {
		panelW = min(qaWidth-8, winW)
	}
	panelLeft := p.panelRect.X
	if p.panelRect.W <= 0 {
		panelLeft = max(0, winW-qaWidth)
	}

	estH := p.cardH
	if estH <= 0 {
		estH = qaPreviewCardGuess
	}
	topLimit := max(p.panelRect.Y, 46)

	// Prefer floating immediately to the left of the Q&A card so the preview
	// never covers the highlighted claim or neighboring lines in the answer.
	leftRoom := panelLeft - 8 - 12
	if leftRoom >= 236 {
		cardW = min(qaPreviewCardWidth, leftRoom)
		x = panelLeft - 8 - cardW
		midY := anchor.Y + anchor.H/2
		maxY := max(topLimit, winH-estH-18)
		y = min(max(midY-estH/2, topLimit), maxY)
		return x, y, cardW, qaPlaceLeft
	}

	// Ultra-narrow window fallback: open below the chip first so the
	// preceding highlighted claim remains readable above it.
	cardW = min(qaPreviewCardWidth, max(panelW-16, 200))
	cardW = min(cardW, max(winW-24, 160))

	minX := max(float32(12), panelLeft+8)
	maxX := min(winW-cardW-12, panelLeft+panelW-cardW-8)
	if maxX < minX {
		minX = max(12, winW-cardW-12)
		maxX = minX
	}
	x = min(max(anchor.X+anchor.W/2-cardW/2, minX), maxX)
	if anchor.Y+anchor.H+8+estH <= winH-16 {
		return x, anchor.Y + anchor.H + 8, cardW, qaPlaceBelow
	}
	return x, anchor.Y - 8, cardW, qaPlaceAbove
}

// finishCitationFrame advances the open/close/glide state machine after the
// answer list has reported pointer hover and focus state for every chip.
func (s *qaSession) finishCitationFrame(c *ui.Context) {
	now := c.Now()
	p := &s.preview
	reduce := c.Preferences().ReduceMotion
	prevOpen, prevTurn, prevPart := p.open, p.turn, p.part

	for i, turn := range s.turns {
		if turn.nextHoverSource != turn.hoverSource ||
			(i < len(p.prevSources) && turn.hoverSource != p.prevSources[i]) {
			c.After(time.Millisecond)
			break
		}
	}

	if obs := p.observed; obs != nil {
		if p.dismissedTurn == obs.turn && p.dismissedPart == obs.part {
			return
		}
		p.dismissedTurn, p.dismissedPart = nil, 0
		p.closeAt = time.Time{}
		isWarm := p.open || (!p.lastClosed.IsZero() && now.Sub(p.lastClosed) < qaPreviewWarmWindow)
		if !p.open && !obs.immediate && !isWarm {
			if p.pendingTurn != obs.turn || p.pendingPart != obs.part || p.openAt.IsZero() {
				p.pendingTurn = obs.turn
				p.pendingPart = obs.part
				p.openAt = now.Add(qaPreviewOpenDelay)
			}
			if rem := p.openAt.Sub(now); rem > 0 {
				c.After(rem)
				return
			}
		}

		// Open or update the active preview.
		wasOpen := p.open
		changedPart := !wasOpen || p.turn != obs.turn || p.part != obs.part
		prevPlace := p.place

		p.pendingTurn, p.pendingPart, p.openAt = nil, 0, time.Time{}
		p.open = true
		p.warm = wasOpen
		p.turn = obs.turn
		p.part = obs.part
		p.nums = obs.nums
		p.anchor = obs.anchor

		tx, ty, cardW, place := p.placeTarget(c, obs.anchor)
		p.cardW = cardW
		p.place = place

		if !p.glideInit || !wasOpen || reduce {
			p.glideInit = true
			p.fromX, p.fromY = tx, ty
			p.curX, p.curY = tx, ty
			p.targetX, p.targetY = tx, ty
			p.glideAt = time.Time{}
		} else if changedPart {
			startY := p.curY
			if (prevPlace == qaPlaceAbove) != (place == qaPlaceAbove) {
				h := p.cardH
				if h <= 0 {
					h = qaPreviewCardGuess
				}
				if place == qaPlaceAbove {
					startY += h
				} else {
					startY -= h
				}
			}
			p.fromX, p.fromY = p.curX, startY
			p.targetX, p.targetY = tx, ty
			p.glideAt = now
		} else {
			p.targetX, p.targetY = tx, ty
			if p.glideAt.IsZero() {
				p.curX, p.curY = tx, ty
			}
		}
	} else {
		p.dismissedTurn, p.dismissedPart = nil, 0
		p.pendingTurn, p.pendingPart, p.openAt = nil, 0, time.Time{}
		if p.open {
			if p.closeAt.IsZero() {
				p.closeAt = now.Add(qaPreviewCloseDelay)
			}
			if rem := p.closeAt.Sub(now); rem > 0 {
				c.After(rem)
			} else {
				p.closeNow(now)
			}
		}
	}

	if p.open && !p.glideAt.IsZero() && !reduce {
		elapsed := now.Sub(p.glideAt)
		if elapsed < qaPreviewGlideDur {
			t := qaCiteEase(float32(elapsed) / float32(qaPreviewGlideDur))
			p.curX = p.fromX + (p.targetX-p.fromX)*t
			p.curY = p.fromY + (p.targetY-p.fromY)*t
			c.AnimationFrame()
		} else {
			p.curX, p.curY = p.targetX, p.targetY
			p.glideAt = time.Time{}
		}
	}
	if p.open {
		p.renderedX, p.renderedY = p.curX, p.curY
	}
	if p.open != prevOpen || p.turn != prevTurn || p.part != prevPart {
		c.After(time.Millisecond)
	}
}

func (s *qaSession) isChipLit(turn *qaTurn, partID int, nums []int) bool {
	if s == nil || turn == nil {
		return false
	}
	if s.preview.open && s.preview.turn == turn && s.preview.part == partID {
		return true
	}
	if turn.hoverSource > 0 && slices.Contains(nums, turn.hoverSource) {
		return true
	}
	return false
}

func (s *qaSession) isClaimLit(turn *qaTurn, claimID int) bool {
	if s == nil || turn == nil || claimID <= 0 {
		return false
	}
	if s.preview.open && s.preview.turn == turn && s.preview.part == claimID {
		return true
	}
	if turn.hoverSource <= 0 {
		return false
	}
	for i := range turn.view.blocks {
		b := &turn.view.blocks[i]
		if claimID < b.partBase || claimID >= b.partBase+b.partCount {
			continue
		}
		if runsContainClaimSource(b.runs, claimID, turn.hoverSource, turn.view.numbers) {
			return true
		}
		for ci := range b.cells {
			if runsContainClaimSource(b.cells[ci].runs, claimID, turn.hoverSource, turn.view.numbers) {
				return true
			}
		}
	}
	return false
}

func runsContainClaimSource(runs []qaRun, claimID, sourceNum int, numbers map[string]int) bool {
	for _, r := range runs {
		if r.partID != claimID || len(r.cites) == 0 {
			continue
		}
		for _, ci := range r.cites {
			if numbers[citeKey(ci)] == sourceNum {
				return true
			}
		}
	}
	return false
}

// qaChipLabel shortens a document title for an inline chip so long note titles
// never force ugly line breaks or overflow narrow panels.
func qaChipLabel(title string) string {
	name := strings.TrimSpace(docName(title))
	if name == "" {
		name = strings.TrimSpace(title)
	}
	if utf8.RuneCountInString(name) <= qaChipMaxRunes {
		return name
	}
	rs := []rune(name)
	return strings.TrimSpace(string(rs[:qaChipMaxRunes-1])) + "…"
}

func qaChipTextWidth(c *ui.Context, turn *qaTurn, s string) float32 {
	if s == "" {
		return 0
	}
	if turn != nil && turn.view.chipWidths != nil {
		if w, ok := turn.view.chipWidths[s]; ok {
			return w
		}
	}
	w, _ := c.MeasureText(0, ui.Span{Text: s, Size: 11, Weight: 500})
	w = float32(math.Ceil(float64(w)))
	if turn != nil {
		if turn.view.chipWidths == nil {
			turn.view.chipWidths = make(map[string]float32, 8)
		}
		turn.view.chipWidths[s] = w
	}
	return w
}

type qaDrawnChip struct {
	at        int
	leftPad   float32
	rightPad  float32
	monogram  string
	isEmoji   bool
	label     string
	extra     string
	bg, ink   ui.Color
	monoBg    ui.Color
	monoInk   ui.Color
	focusRing bool
}

func qaChipWidth(c *ui.Context, turn *qaTurn, label, extra string, leftPad, rightPad float32) float32 {
	// leftPad + 2px left inset + 16px monogram + 4px gap + label + (4px gap + extra) + 6px right inset + rightPad
	w := leftPad + 2 + qaChipMonoSize + 4 + qaChipTextWidth(c, turn, label) + 6 + rightPad
	if extra != "" {
		w += 4 + qaChipTextWidth(c, turn, extra)
	}
	return w
}

func paintQAChipObject(p *ui.Painter, chips []qaDrawnChip, at int, r ui.Rect) {
	k, ok := slices.BinarySearchFunc(chips, at, func(ch qaDrawnChip, at int) int {
		return cmp.Compare(ch.at, at)
	})
	if !ok {
		return
	}
	ch := chips[k]
	pill := ui.Rect{
		X: r.X + ch.leftPad,
		Y: r.Y,
		W: max(0, r.W-ch.leftPad-ch.rightPad),
		H: r.H,
	}
	if pill.W <= 0 || pill.H <= 0 {
		return
	}
	rad := pill.H / 2
	p.Fill(pill, ch.bg, rad)
	if ch.focusRing {
		p.FocusRing(pill, [4]float32{rad, rad, rad, rad})
	}

	mono := ui.Rect{
		X: pill.X + 2,
		Y: pill.Y + (pill.H-qaChipMonoSize)/2,
		W: qaChipMonoSize,
		H: qaChipMonoSize,
	}
	if ch.isEmoji {
		sp := ui.Span{Text: ch.monogram, Size: 10.5, Color: ch.ink}
		mw, mh := p.MeasureText(0, sp)
		p.RichText(mono.X+(mono.W-mw)/2, mono.Y+(mono.H-mh)/2, 0, sp)
	} else {
		p.Fill(mono, ch.monoBg, qaChipMonoSize/2)
		sp := ui.Span{Text: ch.monogram, Size: 9.5, Weight: 600, Color: ch.monoInk}
		mw, mh := p.MeasureText(0, sp)
		p.RichText(mono.X+(mono.W-mw)/2, mono.Y+(mono.H-mh)/2, 0, sp)
	}

	x := mono.X + mono.W + 4
	lbl := ui.Span{Text: ch.label, Size: 11, Weight: 500, Color: ch.ink}
	lw, lh := p.MeasureText(0, lbl)
	p.RichText(x, pill.Y+(pill.H-lh)/2, 0, lbl)
	x += float32(math.Ceil(float64(lw)))

	if ch.extra != "" {
		x += 4
		ext := ui.Span{Text: ch.extra, Size: 11, Weight: 500, Color: scaleAlpha(ch.ink, 0.60)}
		_, eh := p.MeasureText(0, ext)
		p.RichText(x, pill.Y+(pill.H-eh)/2, 0, ext)
	}
}

func qaMonogramGlyph(title string) (glyph string, isEmoji bool) {
	if e := leadingEmoji(title); e != "" {
		return e, true
	}
	return docInitial(title), false
}

func qaMonogram(c *ui.Context, title string, size, radius float32, cols qaCitationLook) ui.Element {
	glyph, isEmoji := qaMonogramGlyph(title)
	if isEmoji {
		return ui.Box(c).Size(size, size).Shrink(0).Center().Children(func() {
			ui.Text(c, glyph).FontSize(size * 0.72).NoWrap()
		})
	}
	return ui.Box(c).Size(size, size).Radius(radius).Background(cols.monoFill).Shrink(0).Center().Children(func() {
		ui.Text(c, glyph).FontSize(max(9, size*0.56)).FontWeight(600).TextColor(cols.monoInk).NoWrap()
	})
}

// qaCitationPreview renders Kokonut UI's warm-gliding floating preview card
// above (or below) the active inline source chip.
func (a *App) qaCitationPreview(c *ui.Context, s *qaSession) {
	p := &s.preview
	if !p.open || p.turn == nil || len(p.nums) == 0 {
		return
	}
	var refs []citeRef
	for _, n := range p.nums {
		if r, ok := p.turn.view.byNum[n]; ok {
			refs = append(refs, r)
		}
	}
	if len(refs) == 0 {
		return
	}
	t := c.Theme()
	cols := qaCitationColors(t)
	_, winH := c.Size()
	cardW := p.cardW
	if cardW <= 0 {
		cardW = qaPreviewCardWidth
	}

	ui.Overlay(c, func() {
		card := ui.Column(c.Key("qa-cite-preview")).Absolute().Left(p.renderedX).Width(cardW).
			Padding(4).Radius(12).Background(cols.cardFill).
			Border(1, cols.cardBorder).Shadow(0, 8, 24, -4, cols.cardShadow).
			IgnoreInput(true).Transition(ui.ElementTransition{
			Duration: 140 * time.Millisecond,
			Ease:     qaCiteEase,
			Enter:    &ui.Motion{Scale: 0.97, Opacity: 0},
			Exit:     &ui.Motion{Scale: 0.97, Opacity: 0},
		})
		switch p.place {
		case qaPlaceLeft:
			card.Top(p.renderedY).TransformOrigin(1, 0.5)
		case qaPlaceBelow:
			card.Top(min(p.renderedY, max(winH-120, 8))).TransformOrigin(0.5, 0)
		default:
			card.Bottom(max(winH-p.renderedY, 8)).TransformOrigin(0.5, 1)
		}
		card.Draw(func(_ *ui.Painter, r ui.Rect) {
			p.cardH = r.H
		})
		card.Children(func() {
			ui.Column(c.Key("body")).Children(func() {
				for _, ref := range refs {
					ui.Column(c.Key(fmt.Sprintf("src-%d", ref.n))).Padding(10).Radius(8).Gap(5).Children(func() {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							qaMonogram(c, ref.title, 16, 4, cols)
							ui.Text(c, qaPreviewOrigin(ref)).FontSize(11.5).TextColor(cols.cardMuted).
								SingleLine().Shrink(1).MinWidth(0)
							ui.Spacer(c)
							ui.Text(c, strconv.Itoa(ref.n)).FontSize(11.5).FontFeatures("tnum").
								TextColor(cols.cardMuted).NoWrap().Shrink(0)
						})
						ui.Text(c, qaPreviewHeading(ref)).FontSize(13.5).FontWeight(550).
							LineHeight(1.35).TextColor(cols.cardTitle).MaxLines(2)
						if ref.excerpt != "" {
							ui.Text(c, ref.excerpt).FontSize(12).LineHeight(1.5).
								TextColor(cols.cardMuted).MaxLines(3)
						}
					})
				}
			})
		})
	})
}

func qaPreviewOrigin(ref citeRef) string {
	title := docName(ref.title)
	if title == "" {
		title = vault.Title(ref.cite.Path)
	}
	loc := humanLines(ref.cite.From, ref.cite.To)
	if ref.folder != "" {
		return ref.folder + " / " + title + " · " + loc
	}
	return title + " · " + loc
}

func qaPreviewHeading(ref citeRef) string {
	if ref.section != "" {
		return ref.section
	}
	title := docName(ref.title)
	if title == "" {
		return vault.Title(ref.cite.Path)
	}
	return title
}

func qaSourcesCountLabel(n int) string {
	if n == 1 {
		return i18n.T("1 个来源")
	}
	return fmt.Sprintf(i18n.T("%d 个来源"), n)
}

func qaSourceRowTitle(ref citeRef) string {
	title := docName(ref.title)
	if title == "" {
		title = vault.Title(ref.cite.Path)
	}
	if ref.section != "" {
		return title + " · " + ref.section
	}
	return title
}

func qaSourceShortFolder(folder string) string {
	if folder == "" {
		return ""
	}
	return path.Base(strings.ReplaceAll(folder, " / ", "/"))
}

// qaSources renders Kokonut UI's stacked-monogram toggle pill and collapsible
// numbered sources list at the bottom of a finished answer.
func (a *App) qaSources(c *ui.Context, spaceID string, turn *qaTurn) {
	refs := turn.view.refs
	if len(refs) == 0 {
		return
	}
	t := c.Theme()
	cols := qaCitationColors(t)
	countLabel := qaSourcesCountLabel(len(refs))

	ui.Column(c.Key("sources")).Padding(4, 0, 0, 0).Children(func() {
		ui.Row(c).Children(func() {
			btn := ui.Row(c.Key("sources-toggle")).Padding(4, 10, 4, 4).Gap(8).Radius(999).
				AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().
				Role(ui.RoleButton).Label(countLabel)
			if btn.Hovered() {
				btn.Background(cols.toggleHover)
			}
			btn.Children(func() {
				k := min(len(refs), 4)
				stackW := float32(20 + max(0, k-1)*16)
				ui.Box(c.Key("stack")).Size(stackW, 20).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
					for i := range k {
						cx := r.X + float32(i)*16
						p.Fill(ui.Rect{X: cx - 2, Y: r.Y - 2, W: 24, H: 24}, cols.stackRing, 12)
						circle := ui.Rect{X: cx, Y: r.Y, W: 20, H: 20}
						glyph, isEmoji := qaMonogramGlyph(refs[i].title)
						if isEmoji {
							p.Fill(circle, cols.monoFill, 10)
							sp := ui.Span{Text: glyph, Size: 11, Color: cols.monoInk}
							mw, mh := p.MeasureText(0, sp)
							p.RichText(circle.X+(circle.W-mw)/2, circle.Y+(circle.H-mh)/2, 0, sp)
						} else {
							p.Fill(circle, cols.monoFill, 10)
							sp := ui.Span{Text: glyph, Size: 9.5, Weight: 600, Color: cols.monoInk}
							mw, mh := p.MeasureText(0, sp)
							p.RichText(circle.X+(circle.W-mw)/2, circle.Y+(circle.H-mh)/2, 0, sp)
						}
					}
				})
				ui.Text(c, countLabel).FontSize(12).FontWeight(500).TextColor(cols.toggleInk).NoWrap()
				rot := float32(0)
				if turn.sourcesOpen {
					rot = 180
				}
				ui.Box(c.Key("chev")).Size(14, 14).Center().Shrink(0).
					AnimatePose(ui.Pose{Rotate: rot}, ui.Animation{Duration: 200 * time.Millisecond, Ease: qaCiteEase}).
					Children(func() {
						ui.Icon(c, iconChevronDown).FontSize(13.5).TextColor(cols.chevInk)
					})
			})
			if btn.Clicked() {
				turn.sourcesOpen = !turn.sourcesOpen
			}
		})

		if !turn.sourcesOpen {
			return
		}

		ui.Column(c.Key("sources-list")).Padding(6, 0, 0, 0).Gap(2).Clip().
			Transition(ui.ElementTransition{
				Duration: 220 * time.Millisecond,
				Ease:     qaCiteEase,
				Enter:    &ui.Motion{Collapse: true, Opacity: 0},
				Exit:     &ui.Motion{Collapse: true, Opacity: 0},
			}).Children(func() {
			for _, ref := range refs {
				title := qaSourceRowTitle(ref)
				lines := humanLines(ref.cite.From, ref.cite.To)
				row := ui.Row(c.Key(fmt.Sprintf("src-%d", ref.n))).Padding(6, 8).Radius(8).Gap(9).
					AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().
					Role(ui.RoleLink).Label(fmt.Sprintf("%d %s %s", ref.n, title, lines))
				focused := row.Focused() && row.FocusVisible()
				active := row.Hovered() || focused
				if active {
					row.Background(cols.rowHover)
					turn.nextHoverSource = ref.n
				}
				row.Children(func() {
					ui.Text(c, strconv.Itoa(ref.n)).Width(14).TextAlign(ui.End).FontSize(12).
						FontFeatures("tnum").TextColor(cols.rowMeta).NoWrap().Shrink(0)
					qaMonogram(c, ref.title, 16, 4, cols)
					ui.Text(c, title).FontSize(13).TextColor(cols.rowTitle).
						SingleLine().Grow(1).Shrink(1).MinWidth(0)
					if sf := qaSourceShortFolder(ref.folder); sf != "" {
						ui.Row(c).Gap(3).AlignItems(ui.Center).Shrink(0).Children(func() {
							ui.Text(c, sf).FontSize(11.5).TextColor(cols.rowMeta).SingleLine().MaxWidth(56).Shrink(1).MinWidth(0)
							ui.Text(c, "·").FontSize(11.5).TextColor(cols.rowMeta).NoWrap().Shrink(0)
							ui.Text(c, lines).FontSize(11.5).TextColor(cols.rowMeta).NoWrap().Shrink(0)
						})
					} else {
						ui.Text(c, lines).FontSize(11.5).TextColor(cols.rowMeta).NoWrap().Shrink(0)
					}
					arrowCol := cols.arrowInk
					if active {
						arrowCol = cols.arrowHover
					}
					ui.Icon(c, iconArrowUpRight).FontSize(13.5).TextColor(arrowCol).Shrink(0)
				})
				if row.Clicked() {
					a.openCitation(spaceID, ref.cite)
				}
			}
		})
	})
}
