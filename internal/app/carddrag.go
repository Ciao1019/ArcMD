package app

import (
	"slices"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo/ui"
)

// The cards of the library's Spaces page reorder the spaces, as Arc's: the
// grip at a card's foot drags it. The card leaves the row and follows the
// pointer above the others, lifted a little; an empty slot opens where it
// would land and the cards between slide aside (listAnim). Let go, it
// glides into the slot and the spaces take that order, the switcher's
// too. Escape gives up and it glides back. The grip's ← and → move it
// one place by keyboard.

// cardSettle is how long a card let go takes to glide into its slot.
const cardSettle = 220 * time.Millisecond

// cardMove is the card the pointer drags, or one gliding into its slot.
type cardMove struct {
	id   string
	from int     // its index as the drag began
	dx   float32 // how far the pointer moved since
	// canceled is a drag Escape gave up, waiting for the press to end.
	canceled bool
	// settling is the card let go, gliding from x into slot since at.
	settling bool
	x        float32
	slot     int
	at       time.Time
}

// cardStride is the distance between two cards' left edges.
const cardStride = libraryCardW + libraryCardGap

// cardSlotX is where the card in slot i starts in the row.
func cardSlotX(i int) float32 { return libraryCardLead + float32(i)*cardStride }

// cardTarget is the slot the dragged card would land in.
func (m *cardMove) cardTarget(n int) int {
	if m.canceled {
		return m.from
	}
	step := m.dx / cardStride
	if step < 0 {
		step -= 0.5
	} else {
		step += 0.5
	}
	return min(max(m.from+int(step), 0), n-1)
}

// cardOrder is the spaces in the order the row shows them: the dragged one
// left out, its slot at the index returned (-1 without a drag). It is a
// copy: a grip moving a space as the row builds changes the next frame's.
func (a *App) cardOrder() ([]*space.Space, int) {
	m := a.library.move
	if m == nil {
		return slices.Clone(a.state.Spaces), -1
	}
	i := slices.IndexFunc(a.state.Spaces, func(s *space.Space) bool { return s.ID == m.id })
	if i < 0 {
		a.library.move = nil
		return slices.Clone(a.state.Spaces), -1
	}
	rest := slices.Delete(slices.Clone(a.state.Spaces), i, i+1)
	if m.settling {
		return rest, m.slot
	}
	return rest, m.cardTarget(len(a.state.Spaces))
}

// movedCardX is where the card dragged or gliding shows in the row, and
// whether it is still on its way.
func (a *App) movedCardX(now time.Time) (float32, bool) {
	m := a.library.move
	if !m.settling {
		return cardSlotX(m.from) + m.dx, true
	}
	to := cardSlotX(m.slot)
	p := float32(now.Sub(m.at)) / float32(cardSettle)
	if a.reduceMotion || p >= 1 {
		return to, false
	}
	return m.x + (to-m.x)*listAnim.Ease(max(p, 0)), true
}

// cardGrip is the handle at a card's foot that drags it: pressed and
// moved, the card follows; ← and → move it one place.
func (a *App) cardGrip(c *ui.Context, s *space.Space, index int) {
	t := c.Theme()
	l := &a.library
	grip := ui.Box(c.Key("grip")).Size(28, 28).Radius(8).Center().Cursor(ui.CursorGrab).Focusable().
		Role(ui.RoleButton).Label("移动空间 " + s.Name).Tooltip("拖动以调整空间顺序")
	m := l.move
	held := m != nil && m.id == s.ID && !m.settling
	if held {
		grip.Cursor(ui.CursorGrabbing)
	}
	if grip.Hovered() || held {
		grip.Background(hoverFill(t))
	}
	grip.Children(func() { ui.Icon(c, iconMove).FontSize(16).TextColor(t.Text.Alpha(0.72)) })
	dx, _, pressed := grip.Dragged()
	switch {
	case pressed && (m == nil || m.settling && m.id != s.ID):
		if dx != 0 {
			l.move = &cardMove{id: s.ID, from: index, dx: dx}
		}
	case pressed && held:
		m.dx += dx
	case !pressed && held:
		a.dropCard(c)
	}
	// The grip's own keys come before the row's arrows moving the focus.
	switch {
	case grip.Shortcut(0, ui.KeyLeft) && index > 0:
		a.moveSpaceTo(s.ID, index-1)
	case grip.Shortcut(0, ui.KeyRight) && index < len(a.state.Spaces)-1:
		a.moveSpaceTo(s.ID, index+1)
	}
}

// dropCard lets the dragged card go: the spaces take the order shown, and
// it glides from where it is into its slot.
func (a *App) dropCard(c *ui.Context) {
	m := a.library.move
	slot := m.cardTarget(len(a.state.Spaces))
	m.x, _ = a.movedCardX(c.Now())
	m.settling, m.slot, m.at = true, slot, c.Now()
	if slot != m.from {
		a.moveSpaceTo(m.id, slot)
	}
	c.AnimationFrame()
}

// moveSpaceTo puts the space with the ID at index to and keeps the order.
func (a *App) moveSpaceTo(id string, to int) {
	if a.state.Move(id, to) == nil {
		a.saveState()
	}
}

// cardSlot is the empty slot the dragged card would land in: a faint
// dashed outline where it goes.
func cardSlot(c *ui.Context) {
	t := c.Theme()
	ui.Box(c.Key("card-slot")).Width(libraryCardW).Shrink(0).Radius(14).
		Transition(ui.ElementTransition{Animation: &listAnim, Position: true}).
		Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(r, libraryInk(t).Alpha(0.04), 14)
			p.StrokeDashed(ui.Rect{X: r.X + 0.5, Y: r.Y + 0.5, W: r.W - 1, H: r.H - 1}, libraryInk(t).Alpha(0.18), 14, 1)
		})
}
