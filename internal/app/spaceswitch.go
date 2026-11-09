package app

import (
	"math"
	"os"
	"time"

	"github.com/egoist/mygo/ui"
)

// Switching spaces pages the sidebar as Cra (MIT) pages Arc's and Dia's:
// every space's header and folders sit side by side on a strip, which
// follows a sideways two-finger swipe over the sidebar one to one, with a
// rubber band past the first and last space, and settles on a page with a
// zero-bounce spring that picks up the release speed. A click, ⌃1–⌃9,
// ⌥⌘← / ⌥⌘→ or the mouse's side buttons spring the strip from wherever it
// shows. The window's tint mixes the two spaces either side at the strip's
// place. The favorites, the top row and the space bar stay.

const (
	// pagerOmega is the spring's angular frequency: Dia's perceptual
	// duration of 0.4 s with no bounce, critically damped.
	pagerOmega = 2 * math.Pi / 0.4
	// swipeDecide is how far the fingers go before a gesture is a swipe or
	// a scroll; a swipe goes sideways 1.2 times as far as down.
	swipeDecide = 6
	// swipeLift is how long without events tells that the fingers lifted
	// with no momentum after them.
	swipeLift = 80 * time.Millisecond
	// swipeSampleSpan is how much of the swipe's end its release speed is
	// measured over.
	swipeSampleSpan = 80 * time.Millisecond
)

// The axis a gesture over the sidebar turned out to take.
const (
	axisNone = iota
	axisUndecided
	axisHorizontal
	axisVertical
	// axisDone is a swipe the fingers lifted from: its momentum is
	// swallowed.
	axisDone
)

// pagerSpring settles the strip on the page of index: the offset from the
// page, in DIPs to the right, is (x0 + (v0 + ω·x0)·t)·e^(−ω·t).
type pagerSpring struct {
	index  int
	start  time.Time
	x0, v0 float64
}

func (s pagerSpring) offset(at time.Time) float64 {
	t := max(0, at.Sub(s.start).Seconds())
	return (s.x0 + (s.v0+pagerOmega*s.x0)*t) * math.Exp(-pagerOmega*t)
}

type swipeSample struct {
	at time.Time
	dx float32
}

// pager is the strip of spaces: the live swipe, the gesture reading it,
// and the spring settling it.
type pager struct {
	// swipe is how many pages the strip shows away from the active space,
	// toward the next; 0 at rest.
	swipe    float32
	axis     int
	sx, sy   float32
	travel   float32
	samples  []swipeSample
	last     time.Time
	momentum bool
	spring   *pagerSpring
	// velocity is the next settle's starting speed, relative to the
	// distance it goes.
	velocity float64
	// width is the page's width as the strip was last laid out.
	width float32
}

// moving reports whether the strip is off its page.
func (p *pager) moving() bool { return p.swipe != 0 || p.spring != nil }

func (p *pager) pageWidth() float64 {
	if p.width > 0 {
		return float64(p.width)
	}
	return sidebarWidth
}

// pagerShown is where the strip shows, in pages from the first space.
func (a *App) pagerShown(at time.Time) float64 {
	p := &a.pager
	idx := float64(a.activeIndex())
	switch {
	case p.swipe != 0:
		return idx + float64(p.swipe)
	case p.spring != nil:
		return float64(p.spring.index) - p.spring.offset(at)/p.pageWidth()
	}
	return idx
}

// pagerSettle springs the strip from where it shows, from, onto the page of
// index, with the release speed a swipe left.
func (a *App) pagerSettle(index int, from float64) {
	p := &a.pager
	x0 := -(from - float64(index)) * p.pageWidth()
	v0 := -p.velocity * x0
	p.velocity = 0
	if a.reduceMotion || math.Abs(x0) < 0.25 {
		p.spring = nil
		return
	}
	p.spring = &pagerSpring{index: index, start: a.now, x0: x0, v0: v0}
}

// pagerOffset is the strip's offset from the active page this frame, in
// DIPs to the right. pagerTick settles it before the frame is built.
func (a *App) pagerOffset() float64 {
	p := &a.pager
	if p.swipe != 0 {
		return -float64(p.swipe) * p.pageWidth()
	}
	s := p.spring
	if s == nil {
		return 0
	}
	return s.offset(a.now)
}

// pagerScroll reads the scrolling over the sidebar: a gesture that goes
// sideways first is a swipe between spaces, which it takes; one that goes
// down first scrolls the list. It reports whether it took the event.
func (a *App) pagerScroll(ev ui.InputEvent) bool {
	if ev.Kind != ui.InputScroll {
		return false
	}
	p := &a.pager
	fresh := p.axis == axisNone || ev.Time.Sub(p.last) > swipeLift || !ev.Momentum && p.momentum
	p.last, p.momentum = ev.Time, ev.Momentum
	if fresh {
		if p.axis == axisHorizontal {
			a.endSwipe()
		}
		if ev.Momentum {
			p.axis = axisNone // the coasting of a gesture someone else took
			return false
		}
		p.axis, p.sx, p.sy = axisUndecided, 0, 0
	}
	switch p.axis {
	case axisHorizontal:
		if ev.Momentum {
			// Fingers up: the swipe lands and its momentum goes nowhere.
			a.endSwipe()
			p.axis = axisDone
			return true
		}
		a.trackSwipe(ev.DX, ev.Time)
		return true
	case axisDone:
		return true
	case axisUndecided:
		if !ev.Precise || ev.Momentum {
			p.axis = axisVertical // a mouse wheel scrolls
			return false
		}
		p.sx += ev.DX
		p.sy += ev.DY
		if max(abs32(p.sx), abs32(p.sy)) <= swipeDecide {
			return false
		}
		if abs32(p.sx) > abs32(p.sy)*1.2 && len(a.state.Spaces) > 1 {
			p.axis = axisHorizontal
			p.travel, p.samples, p.velocity, p.spring = 0, p.samples[:0], 0, nil
			a.trackSwipe(p.sx, ev.Time)
			return true
		}
		p.axis = axisVertical
	}
	return false
}

// trackSwipe moves the strip with the fingers: content follows them, a
// swipe to the left showing the next space, with the resistance AppKit's
// scroll views put past either end.
func (a *App) trackSwipe(dx float32, at time.Time) {
	p := &a.pager
	p.travel += dx
	p.samples = append(p.samples, swipeSample{at, dx})
	for len(p.samples) > 0 && at.Sub(p.samples[0].at) > swipeSampleSpan {
		p.samples = p.samples[1:]
	}
	idx := a.activeIndex()
	first, last := float64(-idx), float64(len(a.state.Spaces)-1-idx)
	pages := min(1, max(-1, float64(p.travel)/p.pageWidth()))
	band := func(x float64) float64 { return 1 - 1/(x*0.55+1) }
	if pages < first {
		pages = first - band(first-pages)*0.5
	}
	if pages > last {
		pages = last + band(pages-last)*0.5
	}
	p.swipe = float32(pages)
	if p.swipe == 0 {
		p.swipe = 1e-6 // still under the fingers
	}
}

// endSwipe lands the swipe: on the next or previous space when it was fast
// enough or went over half way, else back where it was, springing from
// where it shows with the release speed.
func (a *App) endSwipe() {
	p := &a.pager
	if p.axis != axisHorizontal {
		return
	}
	p.axis = axisDone
	pages := float64(p.swipe)
	var moved float32
	for _, s := range p.samples {
		moved += s.dx
	}
	var velocity float64
	if n := len(p.samples); n > 1 {
		if span := p.samples[n-1].at.Sub(p.samples[0].at).Seconds(); span > 0.004 {
			velocity = float64(moved) / span / p.pageWidth()
		}
	}
	idx := a.activeIndex()
	step := 0
	switch {
	case math.Abs(velocity) > 0.8:
		step = int(math.Copysign(1, velocity))
	case math.Abs(pages) > 0.5:
		step = int(math.Copysign(1, pages))
	}
	if idx+step < 0 || idx+step >= len(a.state.Spaces) {
		step = 0
	}
	if remaining := float64(step) - pages; math.Abs(remaining) > 0.001 {
		p.velocity = max(-4, min(12, velocity/remaining))
	}
	if step != 0 {
		a.switchSpace(a.state.Spaces[idx+step].ID) // settles from where the strip shows
	}
	if p.swipe != 0 { // no switch: back to the page
		from := float64(a.activeIndex()) + float64(p.swipe)
		p.swipe = 0
		a.pagerSettle(a.activeIndex(), from)
	}
	p.velocity = 0
}

// pagerTick lands a swipe whose fingers lifted with no momentum after it,
// and ends a spring before the frame derives its look. It runs even when
// the sidebar is hidden, so the window's tint can still reach its target.
func (a *App) pagerTick(c *ui.Context) {
	p := &a.pager
	if p.axis == axisHorizontal {
		if wait := swipeLift - c.Now().Sub(p.last); wait > 0 {
			c.After(wait)
		} else {
			a.endSwipe()
		}
	}
	s := p.spring
	if s == nil {
		return
	}
	now := c.Now()
	x := s.offset(now)
	speed := math.Abs(x-s.offset(now.Add(-10*time.Millisecond))) * 100
	if s.index != a.activeIndex() || math.Abs(x) < 0.25 && speed < 5 || a.reduceMotion {
		p.spring = nil
		return
	}
	c.AnimationFrame()
}

// activeIndex is the place of the active space in the list.
func (a *App) activeIndex() int {
	if sp := a.state.ActiveSpace(); sp != nil {
		return max(0, a.spaceIndex(sp.ID))
	}
	return 0
}

// spaceIndex is the place of the space with the ID in the list, or -1.
func (a *App) spaceIndex(id string) int {
	for i, s := range a.state.Spaces {
		if s.ID == id {
			return i
		}
	}
	return -1
}

// canEnter reports whether the space's folder can be opened, telling the
// user why not: a switch leaves the window as it was then.
func (a *App) canEnter(root string) bool {
	info, err := os.Stat(root)
	if err == nil && !info.IsDir() {
		err = &os.PathError{Op: "open", Path: root, Err: os.ErrInvalid}
	}
	if err != nil {
		a.notify("无法打开空间：%v", err)
		return false
	}
	return true
}

// stepSpace switches by spaces along, wrapping around.
func (a *App) stepSpace(by int) {
	n := len(a.state.Spaces)
	cur := a.state.ActiveSpace()
	if n < 2 || cur == nil {
		return
	}
	i := a.spaceIndex(cur.ID)
	a.switchSpace(a.state.Spaces[((i+by)%n+n)%n].ID)
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// mixLooks is the window's tint between two spaces, f of the way from x to
// y: their gradients mixed color by color at five points, as Cra's
// PagedGradientView mixes them while paging.
func mixLooks(x, y spaceLook, f float32) spaceLook {
	if f <= 0 {
		return x
	}
	if f >= 1 {
		return y
	}
	out := x
	if f > 0.5 {
		out = y
	}
	// Mixed as they are, see-through and all, and painted as a solid look
	// is, over the material, when either is: an opaque look's colors
	// cover it whole, so each end paints as that look alone does.
	xs, ys := resample(x.stops, 5), resample(y.stops, 5)
	out.stops = make([]ui.Color, len(xs))
	for i := range xs {
		out.stops[i] = mixColor(xs[i], ys[i], f)
	}
	out.Solid = x.Solid || y.Solid
	out.darkness = x.darkness + (y.darkness-x.darkness)*f
	out.Noise = x.Noise + (y.Noise-x.Noise)*float64(f)
	out.text = mixColor(x.text, y.text, f)
	out.muted = mixColor(x.muted, y.muted, f)
	out.accent = mixColor(x.accent, y.accent, f)
	return out
}

// mixColor mixes two colors and their opacity, premultiplied, so that
// laid over anything the mix moves evenly from the one to the other: a
// see-through light look mixed with an opaque dark one does not linger
// light and then darken all at once.
func mixColor(x, y ui.Color, f float32) ui.Color {
	if f <= 0 {
		return x
	}
	if f >= 1 {
		return y
	}
	ax, ay := float32(x.A)/255, float32(y.A)/255
	a := ax + (ay-ax)*f
	if a <= 0 {
		return ui.RGBA(0, 0, 0, 0)
	}
	ch := func(cx, cy uint8) uint8 {
		v := (float32(cx)*ax + (float32(cy)*ay-float32(cx)*ax)*f) / a
		return uint8(min(max(v+0.5, 0), 255))
	}
	return ui.RGBA(ch(x.R, y.R), ch(x.G, y.G), ch(x.B, y.B), a)
}

// resample is a gradient's colors at n points spread from top to bottom.
func resample(stops []ui.Color, n int) []ui.Color {
	out := make([]ui.Color, n)
	for k := range out {
		if len(stops) == 1 {
			out[k] = stops[0]
			continue
		}
		t := float32(k) / float32(n-1) * float32(len(stops)-1)
		i := min(int(t), len(stops)-2)
		out[k] = mixColor(stops[i], stops[i+1], t-float32(i))
	}
	return out
}
