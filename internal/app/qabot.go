package app

import (
	"math"
	"time"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

const qaBotSize = 32

type qaBotState struct {
	motion      botMotion
	painter     botPainter
	last        time.Time
	initialized bool
}

// qaGhostAvatar is BotAvatar type="ghost" size={32}, with the default
// idle behavior. Its native rig follows the pointer within three head
// widths and jumps on click. State belongs to the turn, not the UI tree.
func qaGhostAvatar(c *ui.Context, s *qaBotState) ui.Element {
	if !s.initialized {
		s.motion = newBotMotion(.32, botIdle)
		s.initialized = true
	}
	reduced := c.Preferences().ReduceMotion
	px, py, inside := c.Root().PointerPosition()
	box := ui.Box(c.Key("qa-ghost-avatar")).Size(qaBotSize, qaBotSize).Shrink(0).Radius(8).
		Focusable().Role(ui.RoleButton).Label(i18n.T("小幽灵，点击跳跃")).Cursor(ui.CursorPointer)
	if box.Focused() {
		box.Border(1, c.Theme().Accent)
	}
	if box.Clicked() && !reduced {
		s.motion.poke()
	}
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		now := p.Now()
		pose := botRestPose(s.motion.state)
		if !reduced {
			dx, dy := (float64(px-r.X)-16)/qaBotSize, (float64(py-r.Y)-16)/qaBotSize
			d := math.Hypot(dx, dy)
			strength := botClamp(1-(d-1)/2, 0, 1)
			if !inside {
				strength = 0
			}
			s.motion.setPointer(dx/max(1, d), dy/max(1, d), strength)
			if !s.last.IsZero() {
				s.motion.update(max(0, now.Sub(s.last).Seconds()))
			}
			pose = s.motion.pose
			p.AnimationFrame()
		}
		s.last = now
		s.painter.draw(p, r, pose)
	})
	return box
}
