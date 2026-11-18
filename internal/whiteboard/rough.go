package whiteboard

import (
	"math"
	"sort"
)

// CmdKind identifies a vector path operation in scene coordinates.
type CmdKind uint8

const (
	CmdMoveTo CmdKind = iota
	CmdLineTo
	CmdQuadTo
	CmdCubeTo
	CmdClose
)

// PathCmd is one platform-neutral vector path command.
type PathCmd struct {
	Kind     CmdKind
	X, Y     float32
	C1X, C1Y float32
	C2X, C2Y float32
}

// Primitive is a styled vector path produced for an element in scene coordinates.
type Primitive struct {
	Cmds        []PathCmd
	StrokeColor string
	FillColor   string
	StrokeWidth float32
	Dash        []float32
	Opacity     float32
	Filled      bool
	Stroked     bool
	IsHachure   bool
}

type prng struct {
	state uint32
}

func newPRNG(seed uint32) *prng {
	if seed == 0 {
		seed = 1
	}
	return &prng{state: seed}
}

// next returns a deterministic float32 in [0, 1) using mulberry32.
func (r *prng) next() float32 {
	r.state += 0x6D2B79F5
	t := r.state
	t = (t ^ (t >> 15)) * (t | 1)
	t ^= t + (t^(t>>7))*(t|61)
	return float32((t^(t>>14))&0xFFFFFFFF) / 4294967296.0
}

func (r *prng) offset(minVal, maxVal float32) float32 {
	return minVal + r.next()*(maxVal-minVal)
}

func (r *prng) jitter(scale float32) float32 {
	return (r.next()*2 - 1) * scale
}

// BuildPrimitives synthesizes Rough.js-inspired deterministic hand-drawn
// primitives for el in scene coordinates. Computing wobble in local (0,0)
// coordinates before adding (el.X, el.Y) guarantees zero flicker when moving.
func BuildPrimitives(scene *Scene, el *Element) []Primitive {
	if el == nil {
		return nil
	}
	op := float32(el.Opacity) / 100.0
	if op <= 0 {
		op = 1
	}
	sw := el.StrokeWidth
	if sw <= 0 {
		sw = 2
	}
	rough := max(0, min(2, el.Roughness))
	rng := newPRNG(el.Seed)

	var dash []float32
	switch el.StrokeStyle {
	case StrokeDashed:
		dash = []float32{max(6, sw*3.5), max(5, sw*2.8)}
	case StrokeDotted:
		dash = []float32{max(1.5, sw*0.9), max(4, sw*2.4)}
	}
	singlePass := rough == 0 || len(dash) > 0

	var out []Primitive

	// Normalize positive local rectangle dimensions while translating to top-left.
	ox, oy := el.X, el.Y
	w, h := el.W, el.H
	if el.Kind != KindArrow && el.Kind != KindLine && el.Kind != KindFreedraw {
		if w < 0 {
			ox += w
			w = -w
		}
		if h < 0 {
			oy += h
			h = -h
		}
	}

	switch el.Kind {
	case KindRectangle, KindNote:
		var poly [][2]float32
		var outline []PathCmd
		fold := float32(0)
		if el.Kind == KindNote {
			fold = min(22, min(w, h)*0.24)
			poly = notePolygon(w, h, fold)
			outline = roughPolygonCmds(poly, rough, singlePass, rng)
			// Add folded dog-ear flap strokes.
			if fold > 4 {
				flap := [][2]float32{{w - fold, 0}, {w - fold, fold}, {w, fold}}
				outline = append(outline, roughOpenChainCmds(flap, rough, true, rng)...)
			}
		} else if el.Roundness && min(w, h) >= 8 {
			rad := min(min(w, h)*0.22, 16)
			poly = roundedRectPolygon(w, h, rad, 5)
			outline = roughRoundedRectCmds(w, h, rad, rough, singlePass, rng)
		} else {
			poly = [][2]float32{{0, 0}, {w, 0}, {w, h}, {0, h}}
			outline = roughPolygonCmds(poly, rough, singlePass, rng)
		}
		out = appendFillAndStroke(out, el, ox, oy, poly, outline, sw, dash, op, rough, rng)

	case KindDiamond:
		poly := [][2]float32{{w / 2, 0}, {w, h / 2}, {w / 2, h}, {0, h / 2}}
		var outline []PathCmd
		if el.Roundness && min(w, h) >= 16 {
			outline = roughRoundedDiamondCmds(w, h, rough, singlePass, rng)
		} else {
			outline = roughPolygonCmds(poly, rough, singlePass, rng)
		}
		out = appendFillAndStroke(out, el, ox, oy, poly, outline, sw, dash, op, rough, rng)

	case KindEllipse:
		poly := ellipsePolygon(w, h, 28)
		outline := roughEllipseCmds(w, h, rough, singlePass, rng)
		out = appendFillAndStroke(out, el, ox, oy, poly, outline, sw, dash, op, rough, rng)

	case KindFrame:
		var cmds []PathCmd
		cmds = append(cmds,
			PathCmd{Kind: CmdMoveTo, X: ox, Y: oy},
			PathCmd{Kind: CmdLineTo, X: ox + w, Y: oy},
			PathCmd{Kind: CmdLineTo, X: ox + w, Y: oy + h},
			PathCmd{Kind: CmdLineTo, X: ox, Y: oy + h},
			PathCmd{Kind: CmdClose},
		)
		if el.FillColor != "" {
			out = append(out, Primitive{
				Cmds:      cmds,
				FillColor: el.FillColor,
				Opacity:   op * 0.35,
				Filled:    true,
			})
		}
		out = append(out, Primitive{
			Cmds:        cmds,
			StrokeColor: el.StrokeColor,
			StrokeWidth: sw,
			Dash:        dash,
			Opacity:     op,
			Stroked:     true,
		})

	case KindLine, KindArrow:
		pts := el.Points
		if len(pts) < 2 {
			pts = [][2]float32{{0, 0}, {el.W, el.H}}
		}
		smooth := el.Smooth && !el.Elbowed
		var cmds []PathCmd
		if smooth && len(pts) >= 2 {
			cmds = roughSmoothCurveCmds(pts, rough, singlePass, rng)
		} else {
			cmds = roughOpenChainCmds(pts, rough, singlePass, rng)
		}
		var fillHeadCmds []PathCmd
		appendHead := func(tip [2]float32, ang float64, head Arrowhead) {
			if head == ArrowTriangle || head == ArrowDot {
				if fc := filledArrowheadCmds(tip, ang, head, sw); len(fc) > 0 {
					fillHeadCmds = append(fillHeadCmds, fc...)
				}
			}
			cmds = append(cmds, arrowheadCmds(tip, ang, head, sw, rough, rng)...)
		}
		if el.StartArrowhead != ArrowNone && len(pts) >= 2 {
			appendHead(pts[0], arrowStartAngle(pts, smooth), el.StartArrowhead)
		}
		if el.EndArrowhead != ArrowNone && len(pts) >= 2 {
			appendHead(pts[len(pts)-1], arrowEndAngle(pts, smooth), el.EndArrowhead)
		}
		translateCmds(cmds, ox, oy)
		if len(fillHeadCmds) > 0 {
			translateCmds(fillHeadCmds, ox, oy)
			out = append(out, Primitive{
				Cmds:      fillHeadCmds,
				FillColor: el.StrokeColor,
				Opacity:   op,
				Filled:    true,
			})
		}
		out = append(out, Primitive{
			Cmds:        cmds,
			StrokeColor: el.StrokeColor,
			StrokeWidth: sw,
			Dash:        dash,
			Opacity:     op,
			Stroked:     true,
		})

	case KindFreedraw:
		cmds := smoothFreedrawCmds(el.Points)
		if len(cmds) > 0 {
			translateCmds(cmds, ox, oy)
			out = append(out, Primitive{
				Cmds:        cmds,
				StrokeColor: el.StrokeColor,
				StrokeWidth: sw,
				Dash:        dash,
				Opacity:     op,
				Stroked:     true,
			})
		}

	default:
		if isExtendedShapeKind(el.Kind) {
			out = buildExtendedShapePrimitives(out, el, ox, oy, w, h, sw, dash, op, rough, singlePass, rng)
		}
	}

	return out
}

func appendFillAndStroke(out []Primitive, el *Element, ox, oy float32, poly [][2]float32, outline []PathCmd, sw float32, dash []float32, op float32, rough int, rng *prng) []Primitive {
	if el.FillColor != "" {
		switch el.FillStyle {
		case FillSolid:
			fillCmds := polygonCmds(poly)
			translateCmds(fillCmds, ox, oy)
			out = append(out, Primitive{
				Cmds:      fillCmds,
				FillColor: el.FillColor,
				Opacity:   op,
				Filled:    true,
			})
		case FillHachure, FillCrossHatch:
			gap := max(6.5, sw*3.6)
			hCmds := hachurePolygonCmds(poly, gap, -41, rough, rng)
			if el.FillStyle == FillCrossHatch {
				hCmds = append(hCmds, hachurePolygonCmds(poly, gap*1.15, 49, rough, rng)...)
			}
			if len(hCmds) > 0 {
				translateCmds(hCmds, ox, oy)
				out = append(out, Primitive{
					Cmds:        hCmds,
					StrokeColor: el.FillColor,
					StrokeWidth: max(1, sw*0.58),
					Opacity:     op * 0.85,
					Stroked:     true,
					IsHachure:   true,
				})
			}
		}
	}
	if len(outline) > 0 {
		translateCmds(outline, ox, oy)
		out = append(out, Primitive{
			Cmds:        outline,
			StrokeColor: el.StrokeColor,
			StrokeWidth: sw,
			Dash:        dash,
			Opacity:     op,
			Stroked:     true,
		})
	}
	return out
}

func translateCmds(cmds []PathCmd, dx, dy float32) {
	for i := range cmds {
		if cmds[i].Kind == CmdClose {
			continue
		}
		cmds[i].X += dx
		cmds[i].Y += dy
		cmds[i].C1X += dx
		cmds[i].C1Y += dy
		cmds[i].C2X += dx
		cmds[i].C2Y += dy
	}
}

func polygonCmds(poly [][2]float32) []PathCmd {
	if len(poly) < 3 {
		return nil
	}
	cmds := make([]PathCmd, 0, len(poly)+1)
	cmds = append(cmds, PathCmd{Kind: CmdMoveTo, X: poly[0][0], Y: poly[0][1]})
	for i := 1; i < len(poly); i++ {
		cmds = append(cmds, PathCmd{Kind: CmdLineTo, X: poly[i][0], Y: poly[i][1]})
	}
	cmds = append(cmds, PathCmd{Kind: CmdClose})
	return cmds
}

func roughLinePass(x1, y1, x2, y2 float32, rough int, rng *prng, overlay bool) []PathCmd {
	dx, dy := x2-x1, y2-y1
	length := float32(math.Hypot(float64(dx), float64(dy)))
	if length < 0.5 {
		return nil
	}
	if rough == 0 {
		return []PathCmd{
			{Kind: CmdMoveTo, X: x1, Y: y1},
			{Kind: CmdLineTo, X: x2, Y: y2},
		}
	}
	maxOffset := float32(1.4)
	if rough >= 2 {
		maxOffset = 2.6
	}
	if length < maxOffset*10 {
		maxOffset = length / 10
	}
	if overlay {
		maxOffset *= 0.85
	}
	div := float32(0.2)
	if overlay {
		div = 0.25
	}
	midDisp := float32(1.0)
	if length > 200 {
		midDisp = 1.35
	}
	sx := x1 + rng.jitter(maxOffset*0.5)
	sy := y1 + rng.jitter(maxOffset*0.5)
	ex := x2 + rng.jitter(maxOffset*0.5)
	ey := y2 + rng.jitter(maxOffset*0.5)

	// Perpendicular vector for subtle hand-drawn bow.
	nx, ny := -dy/length, dx/length
	bow1 := rng.jitter(maxOffset * midDisp)
	bow2 := rng.jitter(maxOffset * midDisp)
	t1 := 0.25 + rng.offset(-div*0.3, div*0.3)
	t2 := 0.72 + rng.offset(-div*0.3, div*0.3)
	c1x := sx + (ex-sx)*t1 + nx*bow1
	c1y := sy + (ey-sy)*t1 + ny*bow1
	c2x := sx + (ex-sx)*t2 + nx*bow2
	c2y := sy + (ey-sy)*t2 + ny*bow2

	return []PathCmd{
		{Kind: CmdMoveTo, X: sx, Y: sy},
		{Kind: CmdCubeTo, C1X: c1x, C1Y: c1y, C2X: c2x, C2Y: c2y, X: ex, Y: ey},
	}
}

func roughSegment(x1, y1, x2, y2 float32, rough int, singlePass bool, rng *prng) []PathCmd {
	cmds := roughLinePass(x1, y1, x2, y2, rough, rng, false)
	if !singlePass && rough > 0 {
		cmds = append(cmds, roughLinePass(x1, y1, x2, y2, rough, rng, true)...)
	}
	return cmds
}

func roughPolygonCmds(poly [][2]float32, rough int, singlePass bool, rng *prng) []PathCmd {
	if len(poly) < 2 {
		return nil
	}
	if rough == 0 {
		return polygonCmds(poly)
	}
	var cmds []PathCmd
	for i := range poly {
		a := poly[i]
		b := poly[(i+1)%len(poly)]
		cmds = append(cmds, roughSegment(a[0], a[1], b[0], b[1], rough, singlePass, rng)...)
	}
	return cmds
}

func roughOpenChainCmds(pts [][2]float32, rough int, singlePass bool, rng *prng) []PathCmd {
	if len(pts) < 2 {
		return nil
	}
	if rough == 0 {
		cmds := make([]PathCmd, 0, len(pts))
		cmds = append(cmds, PathCmd{Kind: CmdMoveTo, X: pts[0][0], Y: pts[0][1]})
		for i := 1; i < len(pts); i++ {
			cmds = append(cmds, PathCmd{Kind: CmdLineTo, X: pts[i][0], Y: pts[i][1]})
		}
		return cmds
	}
	var cmds []PathCmd
	for i := 0; i < len(pts)-1; i++ {
		cmds = append(cmds, roughSegment(pts[i][0], pts[i][1], pts[i+1][0], pts[i+1][1], rough, singlePass, rng)...)
	}
	return cmds
}

func roughSmoothCurveCmds(pts [][2]float32, rough int, singlePass bool, rng *prng) []PathCmd {
	if len(pts) == 2 {
		dx := pts[1][0] - pts[0][0]
		dy := pts[1][1] - pts[0][1]
		// Purely straight 2-point segments stay straight unless noticeably diagonal.
		if abs32(dx) < 8 || abs32(dy) < 8 {
			return roughOpenChainCmds(pts, rough, singlePass, rng)
		}
		// Subtle Excalidraw-style gentle curve when straight 2-pt smooth arrow is diagonal.
		ctrl := [2]float32{(pts[0][0] + pts[1][0]) * 0.5, pts[0][1] + dy*0.18}
		if abs32(dy) > abs32(dx) {
			ctrl = [2]float32{pts[0][0] + dx*0.18, (pts[0][1] + pts[1][1]) * 0.5}
		}
		return roughQuadPasses(pts[0], ctrl, pts[1], rough, singlePass, rng)
	}
	passes := 2
	if singlePass || rough == 0 {
		passes = 1
	}
	var cmds []PathCmd
	for p := range passes {
		jit := float32(rough) * 0.85
		if p > 0 {
			jit *= 0.8
		}
		cmds = append(cmds, PathCmd{Kind: CmdMoveTo, X: pts[0][0] + rng.jitter(jit), Y: pts[0][1] + rng.jitter(jit)})
		for i := 1; i < len(pts)-1; i++ {
			mx := (pts[i][0] + pts[i+1][0]) * 0.5
			my := (pts[i][1] + pts[i+1][1]) * 0.5
			cmds = append(cmds, PathCmd{
				Kind: CmdQuadTo,
				C1X:  pts[i][0] + rng.jitter(jit),
				C1Y:  pts[i][1] + rng.jitter(jit),
				X:    mx + rng.jitter(jit*0.5),
				Y:    my + rng.jitter(jit*0.5),
			})
		}
		last := pts[len(pts)-1]
		cmds = append(cmds, PathCmd{Kind: CmdLineTo, X: last[0] + rng.jitter(jit*0.5), Y: last[1] + rng.jitter(jit*0.5)})
	}
	return cmds
}

func roughQuadPasses(p0, ctrl, p1 [2]float32, rough int, singlePass bool, rng *prng) []PathCmd {
	passes := 2
	if singlePass || rough == 0 {
		passes = 1
	}
	var cmds []PathCmd
	for range passes {
		jit := float32(rough) * 1.1
		cmds = append(cmds,
			PathCmd{Kind: CmdMoveTo, X: p0[0] + rng.jitter(jit*0.5), Y: p0[1] + rng.jitter(jit*0.5)},
			PathCmd{
				Kind: CmdQuadTo,
				C1X:  ctrl[0] + rng.jitter(jit*1.2),
				C1Y:  ctrl[1] + rng.jitter(jit*1.2),
				X:    p1[0] + rng.jitter(jit*0.5),
				Y:    p1[1] + rng.jitter(jit*0.5),
			},
		)
	}
	return cmds
}

func roughRoundedRectCmds(w, h, r float32, rough int, singlePass bool, rng *prng) []PathCmd {
	r = min(r, min(w, h)/2)
	if rough == 0 {
		return polygonCmds(roundedRectPolygon(w, h, r, 6))
	}
	var cmds []PathCmd
	// 4 straight edges + 4 hand-drawn quadratic corners.
	cmds = append(cmds, roughSegment(r, 0, w-r, 0, rough, singlePass, rng)...)
	cmds = append(cmds, roughCornerCmds([2]float32{w - r, 0}, [2]float32{w, 0}, [2]float32{w, r}, rough, singlePass, rng)...)
	cmds = append(cmds, roughSegment(w, r, w, h-r, rough, singlePass, rng)...)
	cmds = append(cmds, roughCornerCmds([2]float32{w, h - r}, [2]float32{w, h}, [2]float32{w - r, h}, rough, singlePass, rng)...)
	cmds = append(cmds, roughSegment(w-r, h, r, h, rough, singlePass, rng)...)
	cmds = append(cmds, roughCornerCmds([2]float32{r, h}, [2]float32{0, h}, [2]float32{0, h - r}, rough, singlePass, rng)...)
	cmds = append(cmds, roughSegment(0, h-r, 0, r, rough, singlePass, rng)...)
	cmds = append(cmds, roughCornerCmds([2]float32{0, r}, [2]float32{0, 0}, [2]float32{r, 0}, rough, singlePass, rng)...)
	return cmds
}

func roughCornerCmds(p0, corner, p1 [2]float32, rough int, singlePass bool, rng *prng) []PathCmd {
	return roughQuadPasses(p0, corner, p1, rough, singlePass, rng)
}

func roughRoundedDiamondCmds(w, h float32, rough int, singlePass bool, rng *prng) []PathCmd {
	t := float32(0.08)
	pts := [][2]float32{{w / 2, 0}, {w, h / 2}, {w / 2, h}, {0, h / 2}}
	lerp := func(a, b [2]float32, f float32) [2]float32 {
		return [2]float32{a[0] + (b[0]-a[0])*f, a[1] + (b[1]-a[1])*f}
	}
	var cmds []PathCmd
	for i := range 4 {
		prev := pts[(i+3)%4]
		curr := pts[i]
		next := pts[(i+1)%4]
		in := lerp(curr, prev, t)
		out := lerp(curr, next, t)
		nextIn := lerp(next, curr, t)
		cmds = append(cmds, roughCornerCmds(in, curr, out, rough, singlePass, rng)...)
		cmds = append(cmds, roughSegment(out[0], out[1], nextIn[0], nextIn[1], rough, singlePass, rng)...)
	}
	return cmds
}

func roughEllipseCmds(w, h float32, rough int, singlePass bool, rng *prng) []PathCmd {
	cx, cy := w/2, h/2
	rx, ry := max(1, w/2), max(1, h/2)
	if rough == 0 {
		return polygonCmds(ellipsePolygon(w, h, 32))
	}
	steps := 20
	passes := 2
	if singlePass {
		passes = 1
	}
	var cmds []PathCmd
	for pass := range passes {
		radAmp := float32(rough) * 1.4
		if pass > 0 {
			radAmp *= 0.85
		}
		pts := make([][2]float32, 0, steps+3)
		startAngle := rng.offset(-0.2, 0.2)
		for i := 0; i <= steps+1; i++ {
			ang := startAngle + float32(i)*2*math.Pi/float32(steps)
			px := cx + (rx+rng.jitter(radAmp))*float32(math.Cos(float64(ang)))
			py := cy + (ry+rng.jitter(radAmp))*float32(math.Sin(float64(ang)))
			pts = append(pts, [2]float32{px, py})
		}
		cmds = append(cmds, roughSmoothClosedLoop(pts)...)
	}
	return cmds
}

func roughSmoothClosedLoop(pts [][2]float32) []PathCmd {
	if len(pts) < 4 {
		return nil
	}
	m0x := (pts[0][0] + pts[1][0]) * 0.5
	m0y := (pts[0][1] + pts[1][1]) * 0.5
	cmds := []PathCmd{{Kind: CmdMoveTo, X: m0x, Y: m0y}}
	for i := 1; i < len(pts)-1; i++ {
		mx := (pts[i][0] + pts[i+1][0]) * 0.5
		my := (pts[i][1] + pts[i+1][1]) * 0.5
		cmds = append(cmds, PathCmd{Kind: CmdQuadTo, C1X: pts[i][0], C1Y: pts[i][1], X: mx, Y: my})
	}
	cmds = append(cmds, PathCmd{Kind: CmdQuadTo, C1X: pts[len(pts)-1][0], C1Y: pts[len(pts)-1][1], X: m0x, Y: m0y})
	return cmds
}

func roundedRectPolygon(w, h, r float32, cornerSteps int) [][2]float32 {
	r = min(r, min(w, h)/2)
	if r <= 0.5 {
		return [][2]float32{{0, 0}, {w, 0}, {w, h}, {0, h}}
	}
	corners := []struct {
		cx, cy, a0, a1 float64
	}{
		{float64(w - r), float64(r), -math.Pi / 2, 0},
		{float64(w - r), float64(h - r), 0, math.Pi / 2},
		{float64(r), float64(h - r), math.Pi / 2, math.Pi},
		{float64(r), float64(r), math.Pi, 1.5 * math.Pi},
	}
	var pts [][2]float32
	for _, c := range corners {
		for s := 0; s <= cornerSteps; s++ {
			a := c.a0 + (c.a1-c.a0)*float64(s)/float64(cornerSteps)
			pts = append(pts, [2]float32{
				float32(c.cx + float64(r)*math.Cos(a)),
				float32(c.cy + float64(r)*math.Sin(a)),
			})
		}
	}
	return pts
}

func notePolygon(w, h, fold float32) [][2]float32 {
	if fold <= 1 {
		return [][2]float32{{0, 0}, {w, 0}, {w, h}, {0, h}}
	}
	return [][2]float32{{0, 0}, {w - fold, 0}, {w, fold}, {w, h}, {0, h}}
}

func ellipsePolygon(w, h float32, steps int) [][2]float32 {
	cx, cy := w/2, h/2
	rx, ry := max(1, w/2), max(1, h/2)
	pts := make([][2]float32, steps)
	for i := range steps {
		a := float64(i) * 2 * math.Pi / float64(steps)
		pts[i] = [2]float32{
			cx + rx*float32(math.Cos(a)),
			cy + ry*float32(math.Sin(a)),
		}
	}
	return pts
}

// hachurePolygonCmds fills a closed polygon with Rough.js-style parallel
// diagonal hand-drawn scanlines at angleDeg.
func hachurePolygonCmds(poly [][2]float32, gap, angleDeg float32, rough int, rng *prng) []PathCmd {
	if len(poly) < 3 || gap <= 1 {
		return nil
	}
	rad := float64(angleDeg) * math.Pi / 180
	cosA, sinA := float32(math.Cos(rad)), float32(math.Sin(rad))

	// Rotate polygon by -angle so scanlines are horizontal in rotated space.
	rot := make([][2]float32, len(poly))
	minY, maxY := float32(math.MaxFloat32), float32(-math.MaxFloat32)
	for i, p := range poly {
		rx := p[0]*cosA + p[1]*sinA
		ry := -p[0]*sinA + p[1]*cosA
		rot[i] = [2]float32{rx, ry}
		minY = min(minY, ry)
		maxY = max(maxY, ry)
	}
	var cmds []PathCmd
	for y := minY + gap*0.6; y < maxY-gap*0.3; y += gap {
		var xs []float32
		for i := range rot {
			a := rot[i]
			b := rot[(i+1)%len(rot)]
			if (a[1] <= y && b[1] > y) || (b[1] <= y && a[1] > y) {
				t := (y - a[1]) / (b[1] - a[1])
				xs = append(xs, a[0]+t*(b[0]-a[0]))
			}
		}
		sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
		for i := 0; i+1 < len(xs); i += 2 {
			x0, x1 := xs[i], xs[i+1]
			if x1-x0 < 2 {
				continue
			}
			// Inset slightly from edge so hachure stays softly inside the stroke.
			inset := min(2.0, (x1-x0)*0.12)
			x0 += inset
			x1 -= inset
			// Rotate endpoints back to local shape coordinates.
			p0x := x0*cosA - y*sinA
			p0y := x0*sinA + y*cosA
			p1x := x1*cosA - y*sinA
			p1y := x1*sinA + y*cosA
			cmds = append(cmds, roughLinePass(p0x, p0y, p1x, p1y, rough, rng, false)...)
		}
	}
	return cmds
}

func arrowStartAngle(pts [][2]float32, smooth bool) float64 {
	if len(pts) < 2 {
		return 0
	}
	if smooth && len(pts) == 2 {
		dx, dy := pts[1][0]-pts[0][0], pts[1][1]-pts[0][1]
		if abs32(dx) >= 8 && abs32(dy) >= 8 {
			ctrl := [2]float32{(pts[0][0] + pts[1][0]) * 0.5, pts[0][1] + dy*0.18}
			if abs32(dy) > abs32(dx) {
				ctrl = [2]float32{pts[0][0] + dx*0.18, (pts[0][1] + pts[1][1]) * 0.5}
			}
			return math.Atan2(float64(pts[0][1]-ctrl[1]), float64(pts[0][0]-ctrl[0]))
		}
	}
	return math.Atan2(float64(pts[0][1]-pts[1][1]), float64(pts[0][0]-pts[1][0]))
}

func arrowEndAngle(pts [][2]float32, smooth bool) float64 {
	if len(pts) < 2 {
		return 0
	}
	if smooth && len(pts) == 2 {
		dx, dy := pts[1][0]-pts[0][0], pts[1][1]-pts[0][1]
		if abs32(dx) >= 8 && abs32(dy) >= 8 {
			ctrl := [2]float32{(pts[0][0] + pts[1][0]) * 0.5, pts[0][1] + dy*0.18}
			if abs32(dy) > abs32(dx) {
				ctrl = [2]float32{pts[0][0] + dx*0.18, (pts[0][1] + pts[1][1]) * 0.5}
			}
			return math.Atan2(float64(pts[1][1]-ctrl[1]), float64(pts[1][0]-ctrl[0]))
		}
	}
	n := len(pts)
	return math.Atan2(float64(pts[n-1][1]-pts[n-2][1]), float64(pts[n-1][0]-pts[n-2][0]))
}

func filledArrowheadCmds(tip [2]float32, angle float64, head Arrowhead, sw float32) []PathCmd {
	switch head {
	case ArrowTriangle:
		size := max(10, sw*4.8)
		spread := 0.44
		p1 := [2]float32{tip[0] - size*float32(math.Cos(angle-spread)), tip[1] - size*float32(math.Sin(angle-spread))}
		p2 := [2]float32{tip[0] - size*float32(math.Cos(angle+spread)), tip[1] - size*float32(math.Sin(angle+spread))}
		return polygonCmds([][2]float32{tip, p1, p2})
	case ArrowDot:
		r := max(3.5, sw*1.9)
		poly := ellipsePolygon(r*2, r*2, 12)
		for i := range poly {
			poly[i][0] += tip[0] - r
			poly[i][1] += tip[1] - r
		}
		return polygonCmds(poly)
	}
	return nil
}

func arrowheadCmds(tip [2]float32, angle float64, head Arrowhead, sw float32, rough int, rng *prng) []PathCmd {
	size := max(10, sw*4.8)
	switch head {
	case ArrowOpen, ArrowTriangle:
		spread := 0.44
		p1 := [2]float32{
			tip[0] - size*float32(math.Cos(angle-spread)),
			tip[1] - size*float32(math.Sin(angle-spread)),
		}
		p2 := [2]float32{
			tip[0] - size*float32(math.Cos(angle+spread)),
			tip[1] - size*float32(math.Sin(angle+spread)),
		}
		cmds := roughOpenChainCmds([][2]float32{p1, tip, p2}, rough, true, rng)
		if head == ArrowTriangle {
			cmds = append(cmds, roughSegment(p2[0], p2[1], p1[0], p1[1], rough, true, rng)...)
		}
		return cmds
	case ArrowBar:
		half := size * 0.52
		perp := angle + math.Pi/2
		p1 := [2]float32{tip[0] + half*float32(math.Cos(perp)), tip[1] + half*float32(math.Sin(perp))}
		p2 := [2]float32{tip[0] - half*float32(math.Cos(perp)), tip[1] - half*float32(math.Sin(perp))}
		return roughSegment(p1[0], p1[1], p2[0], p2[1], rough, true, rng)
	case ArrowDot:
		r := max(3, sw*1.8)
		poly := ellipsePolygon(r*2, r*2, 12)
		for i := range poly {
			poly[i][0] += tip[0] - r
			poly[i][1] += tip[1] - r
		}
		return polygonCmds(poly)
	}
	return nil
}

func smoothFreedrawCmds(pts [][2]float32) []PathCmd {
	if len(pts) == 0 {
		return nil
	}
	if len(pts) == 1 {
		p := pts[0]
		return []PathCmd{
			{Kind: CmdMoveTo, X: p[0], Y: p[1]},
			{Kind: CmdLineTo, X: p[0] + 0.5, Y: p[1] + 0.5},
		}
	}
	if len(pts) == 2 {
		return []PathCmd{
			{Kind: CmdMoveTo, X: pts[0][0], Y: pts[0][1]},
			{Kind: CmdLineTo, X: pts[1][0], Y: pts[1][1]},
		}
	}
	cmds := make([]PathCmd, 0, len(pts)+1)
	cmds = append(cmds, PathCmd{Kind: CmdMoveTo, X: pts[0][0], Y: pts[0][1]})
	for i := 1; i < len(pts)-1; i++ {
		mx := (pts[i][0] + pts[i+1][0]) * 0.5
		my := (pts[i][1] + pts[i+1][1]) * 0.5
		cmds = append(cmds, PathCmd{Kind: CmdQuadTo, C1X: pts[i][0], C1Y: pts[i][1], X: mx, Y: my})
	}
	last := pts[len(pts)-1]
	cmds = append(cmds, PathCmd{Kind: CmdLineTo, X: last[0], Y: last[1]})
	return cmds
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
