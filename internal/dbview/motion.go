package dbview

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// High-FPS motion tokens tuned to match ArcMD's editor and sidebar physics:
// crisp critically/lightly-damped springs and smooth deceleration curves.
var (
	dbEaseOut       = ui.CubicBezier(0.16, 1, 0.3, 1)
	dbSpringSnappy  = ui.Animation{Spring: ui.SpringOptions{Duration: 220 * time.Millisecond, Bounce: 0.06}}
	dbSpringList    = ui.Animation{Spring: ui.SpringOptions{Duration: 240 * time.Millisecond, Bounce: 0.0}}
	dbSpringCard    = ui.Animation{Spring: ui.SpringOptions{Duration: 260 * time.Millisecond, Bounce: 0.05}}
	dbSpringPress   = ui.Animation{Spring: ui.SpringOptions{Stiffness: 520, Damping: 32, Mass: 0.55}}
	dbPopoverMotion = ui.ElementTransition{
		Duration:     150 * time.Millisecond,
		Ease:         dbEaseOut,
		ExitDuration: 100 * time.Millisecond,
		ExitEase:     ui.EaseIn,
		Colors:       true,
		Enter:        &ui.Motion{Y: -6, Scale: 0.97, Opacity: 0, Flip: true},
		Exit:         &ui.Motion{Y: -4, Scale: 0.98, Opacity: 0, Flip: true},
	}
)

const (
	dbDragLiftDur   = 130 * time.Millisecond
	dbDragSettleDur = 220 * time.Millisecond
	dbCreatePulse   = 620 * time.Millisecond
)

// Matched-geometry keys for MyGo's native Match / Fly spring transitions.
type viewTabPillMatch struct{}
type calModePillMatch struct{}
type chartKindPillMatch struct{}
type boardCardMatch struct{ id string }

type dbDragKind uint8

const (
	dragKindNone dbDragKind = iota
	dragKindCard
	dragKindRow
	dragKindCol
	dragKindGroup
	dragKindEvent
)

type dbDragPreview struct {
	active        bool
	seenThisFrame bool
	kind          dbDragKind
	id            string
	title         string
	subtitle      string
	accent        ui.Color
	hasAccent     bool
	cardW, cardH  float32
	grabX, grabY  float32
	x, y          float32
	prevX         float32
	tilt          float32
	started       time.Time
	lastAt        time.Time
}

type dbDragSettle struct {
	active       bool
	hasTarget    bool
	kind         dbDragKind
	id           string
	title        string
	subtitle     string
	accent       ui.Color
	hasAccent    bool
	fromX, fromY float32
	fromW, fromH float32
	toRect       ui.Rect
	started      time.Time
}

// dbMotionState holds transient 120fps motion state for one database workspace:
// lifted floating drag previews, drop settle glides, and creation pulses.
type dbMotionState struct {
	now          time.Time
	reduceMotion bool
	drag         dbDragPreview
	settle       dbDragSettle
	created      map[string]time.Time
}

// TrackCreated records a newly created record ID so views animate its entry
// and display a soft accent shimmer pulse.
func (m *dbMotionState) TrackCreated(recID string) {
	if m == nil || recID == "" || m.reduceMotion {
		return
	}
	now := m.now
	if now.IsZero() {
		now = time.Now()
	}
	if m.created == nil {
		m.created = make(map[string]time.Time, 8)
	}
	m.created[recID] = now
}

// IsRecentlyCreated reports whether recID was just created within the entry
// window (safe to trigger one-shot Enter animations inside virtualized lists).
func (m *dbMotionState) IsRecentlyCreated(recID string, now time.Time) bool {
	if m == nil || m.reduceMotion || recID == "" || len(m.created) == 0 {
		return false
	}
	at, ok := m.created[recID]
	if !ok {
		return false
	}
	if now.IsZero() {
		now = m.now
	}
	return now.Sub(at) < dbCreatePulse
}

// pulseAlpha returns a smooth 1->0 highlight intensity for a freshly created
// record and requests animation frames until settled.
func (m *dbMotionState) pulseAlpha(c *ui.Context, recID string) float32 {
	if m == nil || recID == "" || len(m.created) == 0 {
		return 0
	}
	if m.reduceMotion || c.Preferences().ReduceMotion {
		delete(m.created, recID)
		return 0
	}
	at, ok := m.created[recID]
	if !ok {
		return 0
	}
	now := c.Now()
	elapsed := now.Sub(at)
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed >= dbCreatePulse {
		delete(m.created, recID)
		return 0
	}
	c.AnimationFrame()
	p := float32(elapsed) / float32(dbCreatePulse)
	return (1 - dbEaseOut(p))
}

func (m *dbMotionState) beginFrame(c *ui.Context) {
	if m == nil {
		return
	}
	m.now = c.Now()
	m.reduceMotion = c.Preferences().ReduceMotion
	m.drag.seenThisFrame = false
	if m.reduceMotion {
		m.settle = dbDragSettle{}
		if len(m.created) > 0 {
			clear(m.created)
		}
		return
	}
	if m.settle.active {
		if m.now.Sub(m.settle.started) >= dbDragSettleDur {
			m.settle = dbDragSettle{}
		}
	}
}

// trackDrag monitors a draggable element and keeps the lifted floating preview
// synchronized with the live pointer at 120fps. Returns true while el is being dragged.
func (m *dbMotionState) trackDrag(
	c *ui.Context,
	el ui.Element,
	kind dbDragKind,
	id, title, subtitle string,
	accent ui.Color,
	hasAccent bool,
) bool {
	if m == nil {
		return false
	}
	if !el.Dragging() {
		// If this item just landed after a drag release, capture its target rect
		// so the floating card glides smoothly into place.
		if m.settle.active && m.settle.id == id && m.settle.kind == kind {
			if b := el.Bounds(); b.W > 0 && b.H > 0 {
				m.settle.toRect = b
				m.settle.hasTarget = true
			}
		}
		return false
	}

	now := c.Now()
	px, py, _ := c.Root().PointerPosition()
	g := &m.drag
	if !g.active || g.id != id || g.kind != kind {
		rb := el.Bounds()
		if rb.W <= 0 {
			rb.W = 240
		}
		if rb.H <= 0 {
			rb.H = 44
		}
		gx := clampF32(px-rb.X, 12, max(12, rb.W-12))
		gy := clampF32(py-rb.Y, 8, max(8, rb.H-8))
		*g = dbDragPreview{
			active:        true,
			seenThisFrame: true,
			kind:          kind,
			id:            id,
			title:         strings.TrimSpace(title),
			subtitle:      strings.TrimSpace(subtitle),
			accent:        accent,
			hasAccent:     hasAccent,
			cardW:         rb.W,
			cardH:         rb.H,
			grabX:         gx,
			grabY:         gy,
			x:             px - gx,
			y:             py - gy,
			prevX:         px,
			tilt:          0,
			started:       now,
			lastAt:        now,
		}
		m.settle = dbDragSettle{}
	} else {
		dt := float32(now.Sub(g.lastAt).Seconds())
		if m.reduceMotion {
			g.tilt = 0
		} else if dt > 0.001 && dt < 0.2 {
			vx := (px - g.prevX) / dt
			targetTilt := clampF32(vx*0.0032, -2.6, 2.6)
			alpha := 1 - float32(math.Exp(-float64(dt)/0.055))
			g.tilt += (targetTilt - g.tilt) * alpha
		} else {
			g.tilt *= 0.85
		}
		g.prevX = px
		g.lastAt = now
		g.x = px - g.grabX
		g.y = py - g.grabY
		g.seenThisFrame = true
		if t := strings.TrimSpace(title); t != "" {
			g.title = t
		}
	}
	return true
}

func (m *dbMotionState) isSettling(kind dbDragKind, id string) bool {
	return m != nil && m.settle.active && m.settle.kind == kind && m.settle.id == id
}

func (m *dbMotionState) isDragging(kind dbDragKind, id string) bool {
	return m != nil && m.drag.active && m.drag.kind == kind && m.drag.id == id
}

func (m *dbMotionState) finishFrame(c *ui.Context, pal Palette) {
	if m == nil {
		return
	}
	now := c.Now()

	// Release transition: drag ended this frame -> start smooth settle glide
	// (skipped immediately when ReduceMotion is enabled).
	if m.drag.active && !m.drag.seenThisFrame {
		if m.reduceMotion || c.Preferences().ReduceMotion {
			m.drag = dbDragPreview{}
			m.settle = dbDragSettle{}
			return
		}
		g := m.drag
		m.settle = dbDragSettle{
			active:    true,
			kind:      g.kind,
			id:        g.id,
			title:     g.title,
			subtitle:  g.subtitle,
			accent:    g.accent,
			hasAccent: g.hasAccent,
			fromX:     g.x,
			fromY:     g.y,
			fromW:     g.cardW,
			fromH:     g.cardH,
			started:   now,
		}
		m.drag = dbDragPreview{}
		c.AnimationFrame()
		return
	}

	switch {
	case m.drag.active:
		g := &m.drag
		lift := float32(1)
		tilt := g.tilt
		if m.reduceMotion || c.Preferences().ReduceMotion {
			lift, tilt = 0, 0
		} else {
			elapsed := now.Sub(g.started)
			if elapsed < dbDragLiftDur {
				lift = dbEaseOut(clampF32(float32(elapsed)/float32(dbDragLiftDur), 0, 1))
				c.AnimationFrame()
			}
			if math.Abs(float64(tilt)) > 0.05 {
				c.AnimationFrame()
			}
		}
		ui.Overlay(c, func() {
			renderFloatingDragOverlay(c, g.kind, g.x, g.y, g.cardW, g.cardH, lift, tilt, g.title, g.subtitle, g.accent, g.hasAccent, pal)
		})

	case m.settle.active:
		s := &m.settle
		elapsed := now.Sub(s.started)
		if !s.hasTarget {
			// Wait one frame for the landed element to report its new bounds.
			if elapsed > 80*time.Millisecond {
				m.settle = dbDragSettle{}
				return
			}
			c.AnimationFrame()
			ui.Overlay(c, func() {
				renderFloatingDragOverlay(c, s.kind, s.fromX, s.fromY, s.fromW, s.fromH, 0.85, 0, s.title, s.subtitle, s.accent, s.hasAccent, pal)
			})
			return
		}
		if elapsed >= dbDragSettleDur {
			m.settle = dbDragSettle{}
			return
		}
		c.AnimationFrame()
		k := dbEaseOut(clampF32(float32(elapsed)/float32(dbDragSettleDur), 0, 1))
		x := s.fromX + (s.toRect.X-s.fromX)*k
		y := s.fromY + (s.toRect.Y-s.fromY)*k
		w := s.fromW + (s.toRect.W-s.fromW)*k
		h := s.fromH + (s.toRect.H-s.fromH)*k
		lift := 1 - k
		ui.Overlay(c, func() {
			renderFloatingDragOverlay(c, s.kind, x, y, w, h, lift, 0, s.title, s.subtitle, s.accent, s.hasAccent, pal)
		})
	}
}

func renderFloatingDragOverlay(
	c *ui.Context,
	kind dbDragKind,
	x, y, w, h, lift, tilt float32,
	title, subtitle string,
	accent ui.Color,
	hasAccent bool,
	pal Palette,
) {
	if title == "" {
		title = "…"
	}
	fill := pal.PopoverBg
	if !pal.Dark {
		fill = ui.RGB(255, 255, 255).Alpha(0.96)
	} else {
		fill = pal.PopoverBg.Alpha(0.96)
	}
	rim := pal.BorderStrong
	if hasAccent {
		rim = accent.Alpha(0.45)
	}
	shadowCol := ui.RGBA(0, 0, 0, 0.16)
	if pal.Dark {
		shadowCol = ui.RGBA(0, 0, 0, 0.52)
	}

	switch kind {
	case dragKindCol, dragKindGroup:
		pillW := clampF32(w, 104, 240)
		pillH := float32(36)
		box := ui.Row(c.Key("db-drag-pill")).Absolute().Left(x).Top(y).Size(pillW, pillH).
			Padding(0, 12).Radius(9).Gap(7).AlignItems(ui.Center).IgnoreInput(true).
			Scale(1+0.025*lift, 1+0.025*lift).Rotate(tilt*0.5*lift).
			Background(fill).Border(1, rim).
			Shadow(0, 4+10*lift, 12+16*lift, -2, shadowCol)
		box.Children(func() {
			if hasAccent {
				ui.Box(c).Size(7, 7).Radius(3.5).Background(accent).Shrink(0)
			} else {
				ui.Icon(c, iconGrip).FontSize(12).TextColor(pal.TextMuted)
			}
			ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)
			if subtitle != "" {
				ui.Text(c, subtitle).FontSize(11).TextColor(pal.TextMuted).NoWrap()
			}
		})

	case dragKindRow, dragKindEvent:
		pillW := clampF32(w, 160, 340)
		pillH := clampF32(h, 34, 44)
		box := ui.Row(c.Key("db-drag-row")).Absolute().Left(x).Top(y).Size(pillW, pillH).
			Padding(0, 12).Radius(10).Gap(8).AlignItems(ui.Center).IgnoreInput(true).
			Scale(1+0.015*lift, 1+0.015*lift).Rotate(tilt*0.35*lift).
			Background(fill).Border(1, rim).
			Shadow(0, 6+12*lift, 16+18*lift, -3, shadowCol)
		box.Children(func() {
			if hasAccent {
				ui.Box(c).Size(3, 18).Radius(1.5).Background(accent).Shrink(0)
			} else {
				ui.Icon(c, iconFileDoc).FontSize(13).TextColor(pal.Accent)
			}
			ui.Text(c, title).FontSize(13.5).FontWeight(500).TextColor(pal.Text).SingleLine().Grow(1).MinWidth(0)
			if subtitle != "" {
				ui.Box(c).Padding(2, 7).Radius(999).Background(pal.Surface).Shrink(0).Children(func() {
					ui.Text(c, subtitle).FontSize(11).TextColor(pal.TextMuted).NoWrap()
				})
			}
		})

	default: // dragKindCard (Board / Gallery elevated floating card)
		cardW := clampF32(w, 180, 320)
		cardH := clampF32(h, 64, 150)
		box := ui.Column(c.Key("db-drag-card")).Absolute().Left(x).Top(y).Width(cardW).MinHeight(cardH).
			Padding(11, 13).Radius(12).Gap(7).IgnoreInput(true).
			TransformOrigin(0.5, 0.5).
			Scale(1+0.03*lift, 1+0.03*lift).Rotate(tilt*lift+0.9*lift).
			Background(fill).Border(1, rim).
			Shadow(0, 8+14*lift, 22+22*lift, -4, shadowCol)
		box.Children(func() {
			ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
				if hasAccent {
					ui.Box(c).Size(7, 7).Radius(3.5).Background(accent).Shrink(0)
				} else {
					ui.Icon(c, iconFileDoc).FontSize(13).TextColor(pal.Accent)
				}
				ui.Text(c, title).FontSize(13.5).FontWeight(600).TextColor(pal.Text).Wrap().Grow(1).MinWidth(0)
			})
			if subtitle != "" {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Padding(2, 8).Radius(999).Background(pal.AccentSoft).Children(func() {
						ui.Text(c, subtitle).FontSize(11).FontWeight(500).TextColor(pal.Accent).NoWrap()
					})
				})
			}
		})
	}
}

// applyPressSpring adds tactile spring press feedback (subtle scale-down on press)
// matching ArcMD's native controls.
func applyPressSpring(el ui.Element, disabled bool) {
	scale := float32(1)
	if !disabled && el.Pressed() {
		scale = 0.96
	}
	el.TransformOrigin(0.5, 0.5).AnimatePose(ui.Pose{Scale: scale}, dbSpringPress)
}

// applyCardHoverLift applies subtle physical elevation and press feedback to
// interactive cards (Board, Gallery, Feed).
func applyCardHoverLift(el ui.Element, hovered, dragging bool) {
	if dragging {
		el.TransformOrigin(0.5, 0.5).AnimatePose(ui.Pose{Scale: 0.98}, dbSpringSnappy)
		return
	}
	pose := ui.Pose{Scale: 1}
	switch {
	case el.Pressed():
		pose = ui.Pose{Scale: 0.985, Y: 0.5}
	case hovered:
		pose = ui.Pose{Scale: 1, Y: -1.5}
	}
	el.TransformOrigin(0.5, 0.5).AnimatePose(pose, dbSpringSnappy)
}

// applyPopoverMotion styles a popover container with crisp Arc/macOS glass
// geometry and spring-eased enter/exit motion.
func applyPopoverMotion(panel ui.Element, pal Palette) ui.Element {
	return panel.Radius(12).Background(pal.PopoverBg).Border(1, pal.PopoverBorder).
		Shadow(0, 12, 32, -4, pal.PopoverShadow).Transition(dbPopoverMotion)
}

// styledPopover renders an anchored popover panel with consistent Arc/Zen
// glass styling and spring enter/exit motion.
func styledPopover(c *ui.Context, key string, anchor ui.Element, open *bool, pal Palette, fn func()) ui.Element {
	if open == nil || !*open {
		return ui.Element{}
	}
	return ui.PopoverBase(c.Key(key), anchor, open, func(panel ui.Element) {
		applyPopoverMotion(panel, pal).Margin(4, 0, 0, 0).Padding(4).Children(fn)
	})
}

// paintDropRail draws a refined 2px insertion rail with a 6px glowing end-cap dot
// (Linear / Notion / ArcMD style) along the top or left edge of r.
func paintDropRail(p *ui.Painter, r ui.Rect, accent ui.Color, vertical bool) {
	if vertical {
		x := r.X + 1
		p.Fill(ui.Rect{X: x - 1, Y: r.Y + 6, W: 2, H: max(0, r.H-12)}, accent, 1)
		p.Fill(ui.Rect{X: x - 3, Y: r.Y + 3, W: 6, H: 6}, accent, 3)
		return
	}
	y := r.Y
	p.Fill(ui.Rect{X: r.X + 8, Y: y - 1, W: max(0, r.W-12), H: 2}, accent, 1)
	p.Fill(ui.Rect{X: r.X + 3, Y: y - 3, W: 6, H: 6}, accent, 3)
}

// paintDashedGhost draws a calm dashed drop-placeholder border inside r while
// an element is lifted for dragging.
func paintDashedGhost(p *ui.Painter, r ui.Rect, radius float32, accent ui.Color) {
	p.Fill(r, accent.Alpha(0.05), radius)
	p.StrokeDashed(rectInset(r, 0.75), accent.Alpha(0.42), radius, 1.2)
}

// paintCreationPulse overlays a fading accent highlight wash on a newly created
// row or card.
func paintCreationPulse(p *ui.Painter, r ui.Rect, radius float32, alpha float32, accent ui.Color) {
	if alpha <= 0.01 {
		return
	}
	p.Fill(r, accent.Alpha(0.16*alpha), radius)
	p.Stroke(rectInset(r, 0.5), accent.Alpha(0.38*alpha), radius, 1)
}

// renderAnimatedChevron draws a chevron icon that smoothly rotates between
// expanded (0 deg) and collapsed (-90 deg).
func renderAnimatedChevron(c *ui.Context, key string, collapsed bool, size float32, color ui.Color) {
	wrap := ui.Box(c.Key(key)).Size(size+2, size+2).Shrink(0).Center()
	rot := float32(0)
	if collapsed {
		rot = -90
	}
	wrap.TransformOrigin(0.5, 0.5).AnimatePose(ui.Pose{Rotate: rot}, ui.Animation{
		Duration: 180 * time.Millisecond,
		Ease:     dbEaseOut,
	})
	wrap.Children(func() {
		ui.Icon(c, iconChevronDown).FontSize(size).TextColor(color)
	})
}

func clampF32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// formatRecordDragSubtitle builds a concise secondary tag for floating drag cards.
func formatRecordDragSubtitle(count int) string {
	if count <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", count)
}
