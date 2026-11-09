package app

import (
	"math"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo/ui"
)

// The folder glyph is Arc's, as Cra (MIT) draws it: a back panel with a
// tab and a front panel, 16 DIPs, in the theme's folder tones. Opening it
// hinges the panels on their bottom-left corner: in perspective, moved 3
// DIPs right and turned 32° about the vertical axis, the back leans 28°
// away and the front falls 30° toward the viewer, the shades deepening;
// closing turns them back. A closed folder keeping its open document in
// sight shows three dots on its front.

const (
	folderSide = 16
	// folderPerspective is Arc's m34: one over the eye's distance.
	folderPerspective = 0.0029129464
)

// folderTurn is how the panels turn: Core Animation's implicit 0.25 s,
// easing in and out.
var folderTurn = ui.Animation{Duration: 250 * time.Millisecond, Ease: ui.EaseInOut}

// folderColors are the glyph's outline, front and back for the look.
func folderColors(l spaceLook) (outline, front, back ui.Color) {
	o, f, b := space.FolderTones(l.Accent, l.Dark)
	return ui.Hex(o), ui.Hex(f), ui.Hex(b)
}

// folderGlyph draws the folder glyph, open or closed, turning between.
func folderGlyph(c *ui.Context, look spaceLook, open, peeked bool) ui.Element {
	outline, front, back := folderColors(look)
	box := ui.Box(c).Size(folderSide, 20).Shrink(0)
	target := float32(0)
	if open {
		target = 1
	}
	p := box.AnimateTo("open", target, folderTurn)
	box.Draw(func(pt *ui.Painter, r ui.Rect) {
		ox, oy := r.X, r.Y+(r.H-folderSide)/2
		paintFolder(pt, ox, oy, float64(p), outline, front, back, peeked && !open)
	})
	return box
}

// panelPoint projects a point of a panel hinged at (0, h) of its own box,
// as Arc's transforms move it at the opening p (0 closed, 1 open): turned
// tilt degrees about the horizontal axis, then 32° about the vertical one,
// moved 3 DIPs right, in perspective.
func panelPoint(x, y, h, tilt, p float64) (float64, float64) {
	// From the hinge, y up, z toward the viewer.
	up := h - y
	a := tilt * p * math.Pi / 180
	yy, z := up*math.Cos(a), up*math.Sin(a)
	b := 32 * p * math.Pi / 180
	xx := x*math.Cos(b) - z*math.Sin(b)
	z = x*math.Sin(b) + z*math.Cos(b)
	xx += 3 * p
	s := 1 / (1 - z*folderPerspective)
	return xx * s, h - yy*s
}

// paintFolder paints the glyph's 16 DIPs at (ox, oy).
func paintFolder(pt *ui.Painter, ox, oy float32, p float64, outline, front, back ui.Color, dots bool) {
	const w, h = folderSide, folderSide
	r := math.Round(w / 7.0)
	tabH := math.Round(h * 0.25)
	t := math.Floor(tabH / 1.5)
	line := float32(1.25)
	// A rounded corner as a quarter turn through c, from a to b.
	type pt2 struct{ x, y float64 }
	shape := func(points []pt2, corners []bool, tilt, panelH, dy float64) *ui.Path {
		path := &ui.Path{}
		proj := func(q pt2) (float32, float32) {
			x, y := panelPoint(q.x, q.y-dy, panelH, tilt, p)
			return ox + float32(x), oy + float32(y+dy)
		}
		n := len(points)
		for i := 0; i < n; i++ {
			q := points[i]
			if corners[i] {
				// q is a corner: cut it with a curve between its neighbours'
				// points r along the edges.
				prev, next := points[(i+n-1)%n], points[(i+1)%n]
				from := pt2{q.x + (prev.x-q.x)*r/dist(q.x, q.y, prev.x, prev.y), q.y + (prev.y-q.y)*r/dist(q.x, q.y, prev.x, prev.y)}
				to := pt2{q.x + (next.x-q.x)*r/dist(q.x, q.y, next.x, next.y), q.y + (next.y-q.y)*r/dist(q.x, q.y, next.x, next.y)}
				fx, fy := proj(from)
				cx, cy := proj(q)
				tx, ty := proj(to)
				if i == 0 {
					path.MoveTo(fx, fy)
				} else {
					path.LineTo(fx, fy)
				}
				path.QuadTo(cx, cy, tx, ty)
				continue
			}
			x, y := proj(q)
			if i == 0 {
				path.MoveTo(x, y)
			} else {
				path.LineTo(x, y)
			}
		}
		return path.Close()
	}
	// The back panel and its tab.
	backPts := []pt2{{0, 0}, {0.41 * w, 0}, {0.58 * w, t}, {w, t}, {w, h}, {0, h}}
	backCorners := []bool{true, false, false, true, true, true}
	backPath := shape(backPts, backCorners, 28, h, 0)
	pt.FillPath(backPath, back)
	shade := float32(0.2 + 0.8*p)
	pt.FillPathGradient(backPath, ui.LinearGradient{Angle: 150, Stops: []ui.GradientStop{
		{Color: black.Alpha(0), At: 0}, {Color: black.Alpha(0.36 * shade), At: 1}}})
	pt.StrokePath(backPath, line, outline)
	// The front panel, under the tab.
	fh := h - tabH
	frontPts := []pt2{{0, tabH}, {w, tabH}, {w, h}, {0, h}}
	frontPath := shape(frontPts, []bool{true, true, true, true}, -30, fh, tabH)
	pt.FillPath(frontPath, front)
	if p > 0 {
		pt.FillPathGradient(frontPath, ui.LinearGradient{Angle: 150, Stops: []ui.GradientStop{
			{Color: black.Alpha(0), At: 0}, {Color: black.Alpha(0.15 * float32(p)), At: 1}}})
	}
	pt.StrokePath(frontPath, line, outline)
	if dots {
		y := oy + float32(math.Floor(tabH+fh/2)) - 1
		for i := -1; i <= 1; i++ {
			pt.Fill(ui.Rect{X: ox + w/2 + float32(i*4) - 1, Y: y, W: 2, H: 2}, outline, 1)
		}
	}
}

func dist(ax, ay, bx, by float64) float64 { return math.Hypot(bx-ax, by-ay) }
