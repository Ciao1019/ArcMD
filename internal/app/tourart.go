package app

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// The tour's pictures: each step's card shows a small mock of the part
// of the window it is about, doing what the step says, drawn over the
// space's own tint. A picture runs in a loop that ends where it began,
// so it never jumps; with less motion wished for it shows one moment of
// the loop, the one that says most.

// tourArt is a picture: how long its loop takes, in seconds (0 plays it
// once and holds), and the moment shown without motion.
type tourArt struct {
	period, still float32
	paint         func(d *tourDraw)
}

var tourArts = map[string]tourArt{
	"welcome":   {4.8, 0.6, (*tourDraw).welcome},
	"spaces":    {4.6, 2.4, (*tourDraw).spaces},
	"theme":     {7, 1.2, (*tourDraw).theme},
	"swipe":     {4.4, 0.55, (*tourDraw).swipe},
	"bookmarks": {5.2, 3.2, (*tourDraw).bookmarks},
	"pins":      {5, 2.4, (*tourDraw).pins},
	"favorites": {5, 2.4, (*tourDraw).favorites},
	"done":      {0, 2, (*tourDraw).done},
}

// tourDraw draws a picture in r at t seconds into its loop.
type tourDraw struct {
	p  *ui.Painter
	r  ui.Rect
	t  float32
	tl tourLook
}

// paintTourArt draws the picture called id, its loop running from since.
func paintTourArt(p *ui.Painter, r ui.Rect, id string, tl tourLook, since time.Time, still bool) {
	art, ok := tourArts[id]
	if !ok {
		return
	}
	t := art.still
	if !still {
		t = float32(p.Now().Sub(since).Seconds())
		if art.period > 0 {
			t = float32(math.Mod(float64(t), float64(art.period)))
		}
		p.AnimationFrame()
	}
	art.paint(&tourDraw{p: p, r: r, t: t, tl: tl})
}

// seg is how far t is from a to b, from 0 to 1.
func seg(t, a, b float32) float32 { return min(max((t-a)/(b-a), 0), 1) }

// smooth eases x in and out.
func smooth(x float32) float32 { return x * x * (3 - 2*x) }

// pop eases x out past 1 and back, as something springing into place.
func pop(x float32) float32 {
	const c = 1.70158
	y := x - 1
	return 1 + (c+1)*y*y*y + c*y*y
}

// thereAndBack goes from 0 to 1 between a and b and back between c and d.
func thereAndBack(t, a, b, c, d float32) float32 {
	return smooth(seg(t, a, b)) * (1 - smooth(seg(t, c, d)))
}

func lerp(a, b, f float32) float32 { return a + (b-a)*f }

func lerpRect(a, b ui.Rect, f float32) ui.Rect {
	return ui.Rect{X: lerp(a.X, b.X, f), Y: lerp(a.Y, b.Y, f), W: lerp(a.W, b.W, f), H: lerp(a.H, b.H, f)}
}

// faded is c with k of its opacity.
func faded(c ui.Color, k float32) ui.Color { return c.Alpha(float32(c.A) / 255 * min(max(k, 0), 1)) }

// centered is a box w by h in the middle of the picture, dy lower.
func (d *tourDraw) centered(w, h, dy float32) ui.Rect {
	return ui.Rect{X: d.r.X + (d.r.W-w)/2, Y: d.r.Y + (d.r.H-h)/2 + dy, W: w, H: h}
}

func (d *tourDraw) dot(cx, cy, radius float32, c ui.Color) {
	d.p.Fill(ui.Rect{X: cx - radius, Y: cy - radius, W: 2 * radius, H: 2 * radius}, c, radius)
}

// panel is a glass surface of the mock, with its shadow.
func (d *tourDraw) panel(r ui.Rect, radius float32) {
	d.p.Shadow(r, radius, 0, 6, 20, 0, d.tl.shadow)
	d.p.Fill(r, d.tl.panel, radius)
}

// row is a row of the mock's list: a document's icon in color c and a
// bar for its name, alpha of the way opaque.
func (d *tourDraw) row(r ui.Rect, c ui.Color, alpha float32) {
	s := min(r.H-10, 12)
	d.p.Fill(ui.Rect{X: r.X + 7, Y: r.Y + (r.H-s)/2, W: s, H: s}, faded(c, alpha), 3.5)
	d.p.Fill(ui.Rect{X: r.X + 7 + s + 7, Y: r.Y + r.H/2 - 2.5, W: (r.W - s - 28) * 0.72, H: 5}, d.tl.ink.Alpha(0.24*alpha), 2.5)
}

// sheet is a sheet of writing: a title and lines.
func (d *tourDraw) sheet(r ui.Rect, radius float32, lines int) {
	d.p.Fill(r, d.tl.paper, radius)
	pad := min(r.W*0.12, 16)
	d.p.Fill(ui.Rect{X: r.X + pad, Y: r.Y + pad, W: (r.W - 2*pad) * 0.5, H: 6}, d.tl.paperInk.Alpha(0.7), 3)
	for i := range lines {
		w := (r.W - 2*pad) * []float32{1, 0.86, 0.94, 0.6}[i%4]
		d.p.Fill(ui.Rect{X: r.X + pad, Y: r.Y + pad + 16 + float32(i)*10, W: w, H: 4}, d.tl.paperInk.Alpha(0.14), 2)
	}
}

// pointer is the mouse's arrow with its tip at x, y.
func (d *tourDraw) pointer(x, y, alpha float32) {
	path := &ui.Path{}
	for i, pt := range [][2]float32{{0, 0}, {0, 15}, {4.2, 11.2}, {6.8, 17}, {9.2, 16}, {6.6, 10.3}, {11.6, 10.3}} {
		if i == 0 {
			path.MoveTo(x+pt[0], y+pt[1])
		} else {
			path.LineTo(x+pt[0], y+pt[1])
		}
	}
	path.Close()
	d.p.FillPath(path, black.Alpha(0.86*alpha))
	d.p.StrokePath(path, 1.2, white.Alpha(0.95*alpha))
}

// welcome is the whole window, its parts lighting up in the order the
// tour visits them: the spaces, the favorites, the pinned, the page.
func (d *tourDraw) welcome() {
	tl := d.tl
	win := d.centered(240, 132, 0)
	d.panel(win, 11)
	for i := range 3 {
		d.dot(win.X+12+float32(i)*9, win.Y+11, 2.6, tl.ink.Alpha(0.22))
	}
	glow := func(i int) float32 {
		at := float32(i) * 1.2
		return thereAndBack(d.t, at, at+0.35, at+0.9, at+1.2)
	}
	lit := func(k float32, base float32) ui.Color { return tl.ink.Alpha(base).Mix(tl.accent, k) }
	side := ui.Rect{X: win.X + 8, Y: win.Y + 22, W: 66, H: win.H - 30}
	// The favorites' tiles.
	k := glow(1)
	for i := range 2 {
		d.p.Fill(ui.Rect{X: side.X + float32(i)*35, Y: side.Y, W: 31, H: 17}, lit(k, 0.13), 5)
	}
	// The pinned rows, the line, the rest.
	k = glow(2)
	for i := range 2 {
		d.p.Fill(ui.Rect{X: side.X, Y: side.Y + 24 + float32(i)*11, W: side.W, H: 8}, lit(k, 0.16), 3)
	}
	d.p.Fill(ui.Rect{X: side.X + 4, Y: side.Y + 49, W: side.W - 8, H: 1}, tl.ink.Alpha(0.14), 0)
	for i := range 2 {
		d.p.Fill(ui.Rect{X: side.X, Y: side.Y + 55 + float32(i)*11, W: side.W * 0.8, H: 8}, tl.ink.Alpha(0.1), 3)
	}
	// The spaces at the foot.
	k = glow(0)
	for i := range 3 {
		c := tl.ink.Alpha(0.2)
		if i == 0 {
			c = tl.accent
		}
		d.dot(side.X+side.W/2+float32(i-1)*14, side.Y+side.H-6, 3.2+0.8*k, c.Mix(tl.accent, k))
	}
	page := ui.Rect{X: win.X + 82, Y: win.Y + 8, W: win.W - 90, H: win.H - 16}
	d.sheet(page, 7, 6)
	// A tag in the page's writing.
	k = glow(3)
	d.p.Fill(ui.Rect{X: page.X + 16, Y: page.Y + 92, W: 34, H: 10}, tl.accent.Alpha(0.16+0.5*k), 3)
}

// spaces is the foot of the sidebar: the pointer presses +, a new space
// takes its place among the others and the window takes its color.
func (d *tourDraw) spaces() {
	tl, t := d.tl, d.t
	k := thereAndBack(t, 1.25, 1.75, 3.7, 4.2)
	tint := tl.accent.Mix(tl.alt, k)
	win := d.centered(156, 78, -24)
	d.p.Shadow(win, 10, 0, 6, 20, 0, tl.shadow)
	d.p.FillGradient(win, ui.LinearGradient{From: tint.Alpha(0.9), To: tint.Alpha(0.55), Angle: 160}, 10)
	for i := range 3 {
		d.p.Fill(ui.Rect{X: win.X + 8, Y: win.Y + 14 + float32(i)*11, W: 34, H: 6}, white.Alpha(0.45), 3)
	}
	d.sheet(ui.Rect{X: win.X + 52, Y: win.Y + 7, W: win.W - 59, H: win.H - 14}, 6, 3)

	bar := d.centered(228, 38, 50)
	d.panel(bar, 12)
	cy := bar.Y + bar.H/2
	slot := func(i int) float32 { return bar.X + 34 + float32(i)*38 }
	d.dot(slot(0), cy, lerp(7.5, 6, k), tl.accent.Mix(tl.ink.Alpha(0.3), k))
	d.dot(slot(1), cy, 6, tl.ink.Alpha(0.3))
	d.dot(slot(2), cy, 6, tl.ink.Alpha(0.3))
	if k > 0 {
		d.dot(slot(3), cy, 7.5*pop(min(k, 1)), tl.alt)
	}
	// The + and the pointer pressing it.
	px := bar.X + bar.W - 24
	over := thereAndBack(t, 0.8, 1.0, 3.3, 3.6)
	d.dot(px, cy, 12, tl.ink.Alpha(0.1*over))
	d.p.Line(px-5, cy, px+5, cy, 1.8, tl.ink.Alpha(0.65))
	d.p.Line(px, cy-5, px, cy+5, 1.8, tl.ink.Alpha(0.65))
	if press := seg(t, 1.15, 1.6); press > 0 && press < 1 {
		g := 12 + 10*press
		d.p.Stroke(ui.Rect{X: px - g, Y: cy - g, W: 2 * g, H: 2 * g}, tl.ink.Alpha(0.4*(1-press)), g, 1.5)
	}
	move := thereAndBack(t, 0.25, 1.0, 3.4, 4.3)
	d.pointer(lerp(bar.X+bar.W/2-20, px-2, move), lerp(bar.Y-26, cy-1, move), 1)
}

// theme is the theme card's field of colors beside a window: as the
// handle wanders over the field, the window takes the color under it.
func (d *tourDraw) theme() {
	tl := d.tl
	pad := ui.Rect{X: d.r.X + 46, Y: d.r.Y + (d.r.H-108)/2, W: 108, H: 108}
	d.panel(pad, 14)
	at := func(fx, fy float32) ui.Color { return tl.accent.Mix(tl.alt, fx).Mix(tl.alt2, fy*0.85) }
	const n = 7
	for i := range n {
		for j := range n {
			fx, fy := float32(i)/(n-1), float32(j)/(n-1)
			d.dot(pad.X+14+fx*(pad.W-28), pad.Y+14+fy*(pad.H-28), 2.4, at(fx, fy).Alpha(0.7))
		}
	}
	a := float64(d.t) / 7 * 2 * math.Pi
	fx, fy := float32(0.5+0.36*math.Sin(a)), float32(0.5+0.36*math.Sin(2*a+0.6))
	col := at(fx, fy)
	hx, hy := pad.X+14+fx*(pad.W-28), pad.Y+14+fy*(pad.H-28)
	d.p.Shadow(ui.Rect{X: hx - 10, Y: hy - 10, W: 20, H: 20}, 10, 0, 2, 8, 0, black.Alpha(0.25))
	d.dot(hx, hy, 10, white)
	d.dot(hx, hy, 7.5, col)

	win := ui.Rect{X: pad.X + pad.W + 24, Y: pad.Y, W: 136, H: 108}
	d.p.Shadow(win, 12, 0, 6, 20, 0, tl.shadow)
	d.p.FillGradient(win, ui.LinearGradient{From: col, To: col.Mix(tl.alt2, 0.35).Alpha(0.7), Angle: 170}, 12)
	for i := range 4 {
		d.p.Fill(ui.Rect{X: win.X + 9, Y: win.Y + 22 + float32(i)*12, W: 30 - float32(i%2)*6, H: 6}, white.Alpha(0.5), 3)
	}
	d.sheet(ui.Rect{X: win.X + 50, Y: win.Y + 8, W: win.W - 58, H: win.H - 16}, 7, 5)
}

// swipe is the sidebar's list paging to the next space under two
// fingers, and back.
func (d *tourDraw) swipe() {
	tl, t := d.tl, d.t
	k := thereAndBack(t, 0.7, 1.5, 2.9, 3.7)
	box := d.centered(172, 116, -10)
	d.panel(box, 12)
	d.p.Fill(box, tl.accent.Mix(tl.alt, k).Alpha(0.14), 12)
	d.p.Clip(box, 12, func() {
		for i, c := range []ui.Color{tl.accent, tl.alt} {
			x := box.X + (float32(i)-k)*box.W
			d.dot(x+19, box.Y+18, 5.5, c)
			d.p.Fill(ui.Rect{X: x + 31, Y: box.Y + 15, W: 52, H: 6}, tl.ink.Alpha(0.42), 3)
			for j := range 3 {
				row := ui.Rect{X: x + 8, Y: box.Y + 32 + float32(j)*25, W: box.W - 16, H: 22}
				if j == 0 {
					d.p.Fill(row, tl.panel, 7)
				}
				d.row(row, c, 0.85)
			}
		}
	})
	for i, c := range []ui.Color{tl.accent, tl.alt} {
		on := k
		if i == 0 {
			on = 1 - k
		}
		d.dot(box.X+box.W/2+float32(i*2-1)*8, box.Y+box.H+13, lerp(3, 4.2, on), tl.ink.Alpha(0.25).Mix(c, on))
	}
	// Two fingers, going left to page on and right to page back.
	fingers := func(alpha, x float32) {
		if alpha <= 0 {
			return
		}
		for i := range 2 {
			cx, cy := box.X+box.W/2+x+float32(i)*17, box.Y+84-float32(i)*9
			d.p.Shadow(ui.Rect{X: cx - 9, Y: cy - 9, W: 18, H: 18}, 9, 0, 2, 8, 0, black.Alpha(0.18*alpha))
			d.dot(cx, cy, 9, white.Alpha(0.55*alpha))
			d.p.Stroke(ui.Rect{X: cx - 9, Y: cy - 9, W: 18, H: 18}, white.Alpha(0.9*alpha), 9, 1.2)
		}
	}
	fingers(seg(t, 0.4, 0.6)*(1-seg(t, 1.5, 1.7)), lerp(30, -44, smooth(seg(t, 0.65, 1.45))))
	fingers(seg(t, 2.6, 2.8)*(1-seg(t, 3.7, 3.9)), lerp(-44, 30, smooth(seg(t, 2.85, 3.65))))
}

// ribbon is a bookmark's mark, w by h with its top left at x, y: filled,
// or its outline.
func (d *tourDraw) ribbon(x, y, w, h float32, c ui.Color, filled bool) {
	path := &ui.Path{}
	path.MoveTo(x, y).LineTo(x+w, y).LineTo(x+w, y+h).LineTo(x+w/2, y+h*0.72).LineTo(x, y+h).Close()
	if filled {
		d.p.FillPath(path, c)
	} else {
		d.p.StrokePath(path, 1.3, c)
	}
}

// bookmarks is a sheet: the pointer goes to the margin beside a
// paragraph, where the hollow mark shows, and presses it; the mark fills
// and the outline's list of bookmarks comes with the paragraph in it.
func (d *tourDraw) bookmarks() {
	tl, t := d.tl, d.t
	page := d.centered(236, 124, -6)
	d.p.Shadow(page, 10, 0, 6, 20, 0, tl.shadow)
	d.p.Fill(page, tl.paper, 10)
	ink := tl.paperInk
	x, y := page.X+18, page.Y+18
	marked := thereAndBack(t, 1.3, 1.55, 4.5, 4.9)
	// The paragraph bookmarked, lit for a moment as the mark lands.
	para := ui.Rect{X: x - 6, Y: y + 31, W: 176, H: 26}
	d.p.Fill(para, tl.accent.Alpha(0.12*marked), 5)
	d.p.Fill(ui.Rect{X: x, Y: y, W: 96, H: 7}, ink.Alpha(0.7), 3.5)
	for i, w := range []float32{164, 150, 164, 112, 158, 90} {
		d.p.Fill(ui.Rect{X: x, Y: y + 20 + float32(i)*8 + float32(i/2)*8, W: w, H: 4}, ink.Alpha(0.14), 2)
	}
	mx, my := page.X+page.W-30, para.Y+5
	hover := thereAndBack(t, 0.75, 1.0, 4.5, 4.9)
	d.ribbon(mx, my, 11, 15, ink.Alpha(0.32*hover*(1-marked)), false)
	if marked > 0 {
		k := pop(min(marked, 1))
		d.ribbon(mx+5.5*(1-k), my+7.5*(1-k), 11*k, 15*k, tl.accent, true)
	}
	if press := seg(t, 1.2, 1.65); press > 0 && press < 1 {
		g := 8 + 10*press
		d.p.Stroke(ui.Rect{X: mx + 5.5 - g, Y: my + 7.5 - g, W: 2 * g, H: 2 * g}, tl.accent.Alpha(0.5*(1-press)), g, 1.5)
	}
	// The outline's bookmarks, with the paragraph listed.
	if k := thereAndBack(t, 2.1, 2.5, 4.4, 4.8); k > 0 {
		list := ui.Rect{X: page.X - 22, Y: page.Y + page.H - 50 + (1-k)*8, W: 150, H: 64}
		d.p.Shadow(list, 10, 0, 8, 24, 0, faded(tl.shadow, k*1.4))
		d.p.Fill(list, faded(tl.paper, k), 10)
		d.p.Stroke(list, ink.Alpha(0.12*k), 10, 1)
		d.p.Text(list.X+12, list.Y+9, "书签 · 1", 11, ink.Alpha(0.55*k))
		row := ui.Rect{X: list.X + 6, Y: list.Y + 31, W: list.W - 12, H: 24}
		d.p.Fill(row, tl.accent.Alpha(0.1*k), 6)
		d.ribbon(row.X+8, row.Y+6, 8.5, 12, faded(tl.accent, k), true)
		d.p.Fill(ui.Rect{X: row.X + 25, Y: row.Y + 9.5, W: 88, H: 5}, ink.Alpha(0.34*k), 2.5)
	}
	move := thereAndBack(t, 0.3, 1.0, 3.5, 4.3)
	d.pointer(lerp(page.X+96, mx+4, move), lerp(page.Y+page.H-22, my+6, move), 1)
}

// listBox is the mock list the pins and the favorites happen in.
func (d *tourDraw) listBox() ui.Rect {
	box := d.centered(200, 140, 0)
	d.panel(box, 12)
	return box
}

// lifted draws what the pointer carries, on the mock's paper with a
// shadow, lift of the way up.
func (d *tourDraw) lifted(r ui.Rect, radius, lift float32) {
	d.p.Shadow(r, radius, 0, 2+6*lift, 6+14*lift, 0, black.Alpha(0.1+0.14*lift))
	d.p.Fill(r, d.tl.paper, radius)
}

// pins is the list: a row is dragged over the line into the pinned,
// where it gets its pin, and later back.
func (d *tourDraw) pins() {
	tl, t := d.tl, d.t
	k := thereAndBack(t, 0.9, 1.7, 3.3, 4.1)
	lift := max(seg(t, 0.6, 0.9)*(1-seg(t, 1.7, 2.0)), seg(t, 3.0, 3.3)*(1-seg(t, 4.1, 4.4)))
	box := d.listBox()
	const rowH, pitch = 24, 27
	x, w := box.X+8, box.W-16
	top := box.Y + 9
	low := top + pitch + 13
	pin := func(r ui.Rect, alpha float32) {
		d.p.Icon(iconPin, ui.Rect{X: r.X + r.W - 19, Y: r.Y + (r.H-11)/2, W: 11, H: 11}, faded(tl.accent, alpha))
	}
	first := ui.Rect{X: x, Y: top, W: w, H: rowH}
	d.row(first, tl.accent, 0.9)
	pin(first, 1)
	lineY := top + pitch + 4 + pitch*k
	d.p.Fill(ui.Rect{X: x + 8, Y: lineY, W: w - 16, H: 1}, tl.ink.Alpha(0.16), 0)
	d.row(ui.Rect{X: x, Y: low + pitch*k, W: w, H: rowH}, tl.alt2, 0.8)
	d.row(ui.Rect{X: x, Y: low + 2*pitch, W: w, H: rowH}, tl.ink.Alpha(0.5), 0.8)
	moving := ui.Rect{X: x + 3*lift, Y: lerp(low+pitch, top+pitch, k), W: w, H: rowH}
	d.lifted(moving, 7, lift)
	d.row(moving, tl.alt, 1)
	pin(moving, k)
	if lift > 0 {
		d.pointer(moving.X+w*0.56, moving.Y+rowH*0.55, lift)
	}
}

// favorites is the list under the favorites' tiles: a row is dragged up
// among them and becomes a tile, the others making room, and later back.
func (d *tourDraw) favorites() {
	tl, t := d.tl, d.t
	k := thereAndBack(t, 0.9, 1.7, 3.3, 4.1)
	lift := max(seg(t, 0.6, 0.9)*(1-seg(t, 1.7, 2.0)), seg(t, 3.0, 3.3)*(1-seg(t, 4.1, 4.4)))
	box := d.listBox()
	const rowH, pitch, tileH, gap = 24, 27, 32, 6
	x, w := box.X+8, box.W-16
	top := box.Y + 9
	low := top + tileH + 14
	tileW := lerp((w-gap)/2, (w-2*gap)/3, k)
	tile := func(i int) ui.Rect { return ui.Rect{X: x + float32(i)*(tileW+gap), Y: top, W: tileW, H: tileH} }
	for i, c := range []ui.Color{tl.accent, tl.alt2} {
		r := tile(i)
		d.p.Fill(r, tl.ink.Alpha(0.08), 9)
		d.p.Fill(ui.Rect{X: r.X + r.W/2 - 6.5, Y: r.Y + r.H/2 - 6.5, W: 13, H: 13}, faded(c, 0.9), 4)
	}
	d.row(ui.Rect{X: x, Y: low + pitch - pitch*k, W: w, H: rowH}, tl.ink.Alpha(0.5), 0.8)
	d.row(ui.Rect{X: x, Y: low + 2*pitch - pitch*k, W: w, H: rowH}, tl.ink.Alpha(0.5), 0.8)
	// The row on its way to the third tile: its icon goes to the middle
	// as its name fades.
	from := ui.Rect{X: x + 3*lift, Y: low, W: w, H: rowH}
	r := lerpRect(from, tile(2), k)
	d.lifted(r, lerp(7, 9, k), lift)
	s := lerp(12, 13, k)
	d.p.Fill(ui.Rect{X: lerp(r.X+7, r.X+r.W/2-s/2, k), Y: r.Y + (r.H-s)/2, W: s, H: s}, tl.alt, 3.5)
	d.p.Fill(ui.Rect{X: r.X + 26, Y: r.Y + r.H/2 - 2.5, W: (w - 40) * 0.72 * (1 - k), H: 5}, tl.ink.Alpha(0.24*(1-k)), 2.5)
	if star := seg(t, 1.7, 2.1) * (1 - seg(t, 3.0, 3.25)); star > 0 {
		size := 12 * pop(star)
		d.p.Icon(iconStar, ui.Rect{X: r.X + r.W - 5 - size/2, Y: r.Y - 5 - size/2 + 6, W: size, H: size}, tl.accent)
	}
	if lift > 0 {
		d.pointer(r.X+r.W*0.56, r.Y+r.H*0.55, lift)
	}
}

// done is a check drawn in a disc of the accent, with stars twinkling
// round it.
func (d *tourDraw) done() {
	tl, t := d.tl, d.t
	cx, cy := d.r.X+d.r.W/2, d.r.Y+d.r.H/2
	radius := 34 * pop(seg(t, 0, 0.5))
	d.p.Shadow(ui.Rect{X: cx - radius, Y: cy - radius, W: 2 * radius, H: 2 * radius}, radius, 0, 8, 24, 0, tl.shadow)
	d.dot(cx, cy, radius+7, tl.accent.Alpha(0.18))
	d.dot(cx, cy, radius, tl.accent)
	if f := smooth(seg(t, 0.35, 0.85)); f > 0 {
		check := &ui.Path{}
		check.MoveTo(cx-14, cy+1).LineTo(cx-4, cy+11).LineTo(cx+15, cy-10)
		check.Trim(0, f)
		d.p.StrokePath(check, 5, white)
	}
	for i, s := range [][3]float32{{-78, -30, 22}, {72, -44, 16}, {90, 22, 26}, {-62, 40, 15}, {-112, 8, 12}, {46, 54, 12}} {
		k := smooth(seg(t, 0.5+float32(i)*0.08, 0.9+float32(i)*0.08))
		pulse := float32(0.75 + 0.25*math.Sin(float64(t)*2.4+float64(i)*1.3))
		star(d.p, cx+s[0], cy+s[1], s[2]*k*pulse, faded(tl.ink.Mix(tl.accent, 0.5), 0.75*k))
	}
}
