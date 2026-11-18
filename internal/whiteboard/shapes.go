package whiteboard

import "math"

func isExtendedShapeKind(k ElementKind) bool {
	switch k {
	case KindTriangle, KindParallelogram, KindHexagon, KindCylinder, KindCloud,
		KindStar, KindCallout, KindDocument, KindStadium, KindActor:
		return true
	}
	return false
}

func isValidKind(k ElementKind) bool {
	switch k {
	case KindRectangle, KindDiamond, KindEllipse, KindArrow, KindLine,
		KindFreedraw, KindText, KindNote, KindFrame:
		return true
	}
	return isExtendedShapeKind(k)
}

func excalidrawBaseType(k ElementKind) string {
	switch k {
	case KindTriangle, KindParallelogram, KindHexagon:
		return "diamond"
	case KindCloud, KindStar, KindStadium:
		return "ellipse"
	case KindCylinder, KindCallout, KindDocument, KindActor:
		return "rectangle"
	default:
		return string(k)
	}
}

// ShapeTextBox returns the preferred interior box in scene coordinates for
// positioning labels inside el (accounting for cylinder tops, callout tails,
// triangle apexes, and stick-figure captions).
func ShapeTextBox(el *Element) Box {
	b := ElementBounds(el)
	if el == nil {
		return b
	}
	switch el.Kind {
	case KindCylinder:
		ry := clamp32(b.H*0.14, 8, min(24, b.H*0.35))
		top := min(b.H*0.42, ry*1.75)
		bottom := ry * 0.65
		return Box{X: b.X, Y: b.Y + top, W: b.W, H: max(18, b.H-top-bottom)}
	case KindTriangle:
		top := b.H * 0.28
		return Box{X: b.X + b.W*0.12, Y: b.Y + top, W: max(20, b.W*0.76), H: max(18, b.H-top-4)}
	case KindCallout:
		return Box{X: b.X, Y: b.Y, W: b.W, H: max(20, b.H*0.80)}
	case KindDocument:
		wave := clamp32(b.H*0.10, 5, 16)
		return Box{X: b.X, Y: b.Y, W: b.W, H: max(20, b.H-wave*1.5)}
	case KindActor:
		return Box{X: b.X, Y: b.Y + b.H*0.74, W: b.W, H: max(20, b.H*0.26)}
	default:
		return b
	}
}

// shapePolygon returns a closed local (0..w, 0..h) polygon approximating closed shapes.
func shapePolygon(k ElementKind, w, h float32, round bool) [][2]float32 {
	w = max(4, w)
	h = max(4, h)
	switch k {
	case KindRectangle, KindNote, KindFrame:
		if k == KindNote {
			fold := min(22, min(w, h)*0.24)
			return notePolygon(w, h, fold)
		}
		if round && min(w, h) >= 8 {
			return roundedRectPolygon(w, h, min(min(w, h)*0.22, 16), 5)
		}
		return [][2]float32{{0, 0}, {w, 0}, {w, h}, {0, h}}
	case KindDiamond:
		return [][2]float32{{w / 2, 0}, {w, h / 2}, {w / 2, h}, {0, h / 2}}
	case KindEllipse:
		return ellipsePolygon(w, h, 28)
	case KindTriangle:
		return [][2]float32{{w / 2, 0}, {w, h}, {0, h}}
	case KindParallelogram:
		skew := min(w*0.20, 28)
		return [][2]float32{{skew, 0}, {w, 0}, {w - skew, h}, {0, h}}
	case KindHexagon:
		inset := min(w*0.22, max(10, h*0.42))
		return [][2]float32{{inset, 0}, {w - inset, 0}, {w, h / 2}, {w - inset, h}, {inset, h}, {0, h / 2}}
	case KindStadium:
		r := min(w/2, h/2)
		return roundedRectPolygon(w, h, r, 6)
	case KindStar:
		return starPolygon(w, h)
	case KindCallout:
		return calloutPolygon(w, h)
	case KindDocument:
		return documentPolygon(w, h)
	case KindCylinder:
		return cylinderOutlinePolygon(w, h)
	case KindCloud:
		return cloudPolygon(w, h)
	case KindActor:
		return [][2]float32{{0, 0}, {w, 0}, {w, h}, {0, h}}
	}
	return nil
}

func buildExtendedShapePrimitives(out []Primitive, el *Element, ox, oy, w, h, sw float32, dash []float32, op float32, rough int, singlePass bool, rng *prng) []Primitive {
	switch el.Kind {
	case KindTriangle:
		poly := shapePolygon(KindTriangle, w, h, el.Roundness)
		outline := roughPolygonCmds(poly, rough, singlePass, rng)
		return appendFillAndStroke(out, el, ox, oy, poly, outline, sw, dash, op, rough, rng)

	case KindParallelogram:
		poly := shapePolygon(KindParallelogram, w, h, el.Roundness)
		outline := roughPolygonCmds(poly, rough, singlePass, rng)
		return appendFillAndStroke(out, el, ox, oy, poly, outline, sw, dash, op, rough, rng)

	case KindHexagon:
		poly := shapePolygon(KindHexagon, w, h, el.Roundness)
		outline := roughPolygonCmds(poly, rough, singlePass, rng)
		return appendFillAndStroke(out, el, ox, oy, poly, outline, sw, dash, op, rough, rng)

	case KindStar:
		poly := starPolygon(w, h)
		outline := roughPolygonCmds(poly, rough, singlePass, rng)
		return appendFillAndStroke(out, el, ox, oy, poly, outline, sw, dash, op, rough, rng)

	case KindStadium:
		r := min(w/2, h/2)
		poly := roundedRectPolygon(w, h, r, 6)
		outline := roughRoundedRectCmds(w, h, r, rough, singlePass, rng)
		return appendFillAndStroke(out, el, ox, oy, poly, outline, sw, dash, op, rough, rng)

	case KindCallout:
		poly := calloutPolygon(w, h)
		outline := roughPolygonCmds(poly, rough, singlePass, rng)
		if el.Roundness {
			outline = roughCalloutCmds(w, h, rough, singlePass, rng)
		}
		return appendFillAndStroke(out, el, ox, oy, poly, outline, sw, dash, op, rough, rng)

	case KindDocument:
		poly := documentPolygon(w, h)
		var outline []PathCmd
		wave := clamp32(h*0.10, 5, 16)
		yBot := h - wave
		outline = append(outline, roughSegment(0, 0, w, 0, rough, singlePass, rng)...)
		outline = append(outline, roughSegment(w, 0, w, yBot, rough, singlePass, rng)...)
		wavePts := make([][2]float32, 0, 18)
		for i := 0; i <= 16; i++ {
			t := float32(i) / 16
			px := w * (1 - t)
			py := yBot - wave*float32(math.Sin(float64(t)*2*math.Pi))
			wavePts = append(wavePts, [2]float32{px, py})
		}
		outline = append(outline, roughOpenChainCmds(wavePts, rough, singlePass, rng)...)
		outline = append(outline, roughSegment(0, yBot, 0, 0, rough, singlePass, rng)...)
		return appendFillAndStroke(out, el, ox, oy, poly, outline, sw, dash, op, rough, rng)

	case KindCylinder:
		ry := clamp32(h*0.14, 8, min(24, h*0.35))
		poly := cylinderOutlinePolygon(w, h)
		var outline []PathCmd
		// Top elliptical lid.
		outline = append(outline, roughEllipseCmds(w, ry*2, rough, singlePass, rng)...)
		// Side walls down to bottom cap.
		outline = append(outline, roughSegment(0, ry, 0, h-ry, rough, singlePass, rng)...)
		outline = append(outline, roughSegment(w, ry, w, h-ry, rough, singlePass, rng)...)
		// Lower elliptical arc across the base.
		const arcSteps = 16
		botPts := make([][2]float32, 0, arcSteps+1)
		cx, cy := w/2, h-ry
		rx := max(1, w/2)
		for i := 0; i <= arcSteps; i++ {
			ang := math.Pi * float64(i) / float64(arcSteps)
			botPts = append(botPts, [2]float32{
				cx + rx*float32(math.Cos(ang)),
				cy + ry*float32(math.Sin(ang)),
			})
		}
		outline = append(outline, roughOpenChainCmds(botPts, rough, singlePass, rng)...)
		return appendFillAndStroke(out, el, ox, oy, poly, outline, sw, dash, op, rough, rng)

	case KindCloud:
		poly := cloudPolygon(w, h)
		outline := roughCloudCmds(w, h, rough, singlePass, rng)
		return appendFillAndStroke(out, el, ox, oy, poly, outline, sw, dash, op, rough, rng)

	case KindActor:
		figH := h * 0.94
		if el.Text != "" {
			figH = h * 0.72
		}
		headR := min(w*0.20, max(7, figH*0.16))
		headCx := w / 2
		headCy := headR + 2
		// Head circle (optional soft fill + rough stroke).
		headPoly := ellipsePolygon(headR*2, headR*2, 18)
		headOutline := roughEllipseCmds(headR*2, headR*2, rough, singlePass, rng)
		out = appendFillAndStroke(out, el, ox+headCx-headR, oy+2, headPoly, headOutline, sw, dash, op, rough, rng)

		neckY := headCy + headR
		hipY := max(neckY+8, figH*0.64)
		armY := neckY + (hipY-neckY)*0.34
		footY := max(hipY+8, figH)
		armSpan := min(w*0.42, max(16, figH*0.44))
		legSpan := min(w*0.36, max(14, figH*0.38))

		var bodyCmds []PathCmd
		// Spine
		bodyCmds = append(bodyCmds, roughSegment(headCx, neckY, headCx, hipY, rough, singlePass, rng)...)
		// Arms
		bodyCmds = append(bodyCmds, roughSegment(headCx-armSpan, armY, headCx+armSpan, armY, rough, singlePass, rng)...)
		// Left & right legs
		bodyCmds = append(bodyCmds, roughSegment(headCx, hipY, headCx-legSpan, footY, rough, singlePass, rng)...)
		bodyCmds = append(bodyCmds, roughSegment(headCx, hipY, headCx+legSpan, footY, rough, singlePass, rng)...)
		translateCmds(bodyCmds, ox, oy)
		out = append(out, Primitive{
			Cmds:        bodyCmds,
			StrokeColor: el.StrokeColor,
			StrokeWidth: sw,
			Dash:        dash,
			Opacity:     op,
			Stroked:     true,
		})
		return out
	}
	return out
}

func starPolygon(w, h float32) [][2]float32 {
	cx, cy := w/2, h/2
	rxOuter, ryOuter := max(2, w*0.49), max(2, h*0.49)
	rxInner, ryInner := rxOuter*0.42, ryOuter*0.42
	pts := make([][2]float32, 0, 10)
	for i := range 10 {
		ang := -math.Pi/2 + float64(i)*math.Pi/5
		rx, ry := rxOuter, ryOuter
		if i%2 == 1 {
			rx, ry = rxInner, ryInner
		}
		pts = append(pts, [2]float32{
			cx + rx*float32(math.Cos(ang)),
			cy + ry*float32(math.Sin(ang)),
		})
	}
	return pts
}

func calloutPolygon(w, h float32) [][2]float32 {
	bodyH := h * 0.80
	tx0 := min(w*0.22, max(10, w*0.18))
	tipX := w * 0.14
	tx1 := min(w-10, tx0+max(16, w*0.16))
	if tx1 <= tx0 {
		return [][2]float32{{0, 0}, {w, 0}, {w, bodyH}, {0, bodyH}}
	}
	r := min(min(w, bodyH)*0.18, 14)
	top := roundedRectPolygon(w, bodyH, r, 3)
	// Insert tail vertices along the bottom edge before returning up the left edge.
	var poly [][2]float32
	tailInserted := false
	for _, pt := range top {
		if !tailInserted && pt[1] >= bodyH-0.5 && pt[0] <= tx1 {
			poly = append(poly, [2]float32{tx1, bodyH}, [2]float32{tipX, h}, [2]float32{tx0, bodyH})
			tailInserted = true
		}
		if tailInserted && pt[1] >= bodyH-0.5 && pt[0] >= tx0 && pt[0] <= tx1 {
			continue
		}
		poly = append(poly, pt)
	}
	if !tailInserted {
		poly = append(poly, [2]float32{tx1, bodyH}, [2]float32{tipX, h}, [2]float32{tx0, bodyH})
	}
	return poly
}

func roughCalloutCmds(w, h float32, rough int, singlePass bool, rng *prng) []PathCmd {
	bodyH := h * 0.80
	r := min(min(w, bodyH)*0.20, 14)
	tx0 := min(w*0.24, max(r+4, w*0.18))
	tipX := w * 0.14
	tx1 := min(w-r-4, tx0+max(16, w*0.16))
	if tx1 <= tx0+2 {
		return roughPolygonCmds(calloutPolygon(w, h), rough, singlePass, rng)
	}
	var cmds []PathCmd
	cmds = append(cmds, roughSegment(r, 0, w-r, 0, rough, singlePass, rng)...)
	cmds = append(cmds, roughCornerCmds([2]float32{w - r, 0}, [2]float32{w, 0}, [2]float32{w, r}, rough, singlePass, rng)...)
	cmds = append(cmds, roughSegment(w, r, w, bodyH-r, rough, singlePass, rng)...)
	cmds = append(cmds, roughCornerCmds([2]float32{w, bodyH - r}, [2]float32{w, bodyH}, [2]float32{w - r, bodyH}, rough, singlePass, rng)...)
	cmds = append(cmds, roughSegment(w-r, bodyH, tx1, bodyH, rough, singlePass, rng)...)
	cmds = append(cmds, roughSegment(tx1, bodyH, tipX, h, rough, singlePass, rng)...)
	cmds = append(cmds, roughSegment(tipX, h, tx0, bodyH, rough, singlePass, rng)...)
	cmds = append(cmds, roughSegment(tx0, bodyH, r, bodyH, rough, singlePass, rng)...)
	cmds = append(cmds, roughCornerCmds([2]float32{r, bodyH}, [2]float32{0, bodyH}, [2]float32{0, bodyH - r}, rough, singlePass, rng)...)
	cmds = append(cmds, roughSegment(0, bodyH-r, 0, r, rough, singlePass, rng)...)
	cmds = append(cmds, roughCornerCmds([2]float32{0, r}, [2]float32{0, 0}, [2]float32{r, 0}, rough, singlePass, rng)...)
	return cmds
}

func documentPolygon(w, h float32) [][2]float32 {
	wave := clamp32(h*0.10, 5, 16)
	yBot := h - wave
	pts := [][2]float32{{0, 0}, {w, 0}, {w, yBot}}
	for i := 1; i <= 16; i++ {
		t := float32(i) / 16
		px := w * (1 - t)
		py := yBot - wave*float32(math.Sin(float64(t)*2*math.Pi))
		pts = append(pts, [2]float32{px, py})
	}
	return pts
}

func cylinderOutlinePolygon(w, h float32) [][2]float32 {
	ry := clamp32(h*0.14, 8, min(24, h*0.35))
	cx, rx := w/2, max(1, w/2)
	const steps = 12
	pts := make([][2]float32, 0, steps*2+4)
	for i := 0; i <= steps; i++ {
		ang := math.Pi + math.Pi*float64(i)/float64(steps)
		pts = append(pts, [2]float32{
			cx + rx*float32(math.Cos(ang)),
			ry + ry*float32(math.Sin(ang)),
		})
	}
	for i := 0; i <= steps; i++ {
		ang := math.Pi * float64(i) / float64(steps)
		pts = append(pts, [2]float32{
			cx + rx*float32(math.Cos(ang)),
			(h - ry) + ry*float32(math.Sin(ang)),
		})
	}
	return pts
}

type cubicSeg struct {
	p0, c1, c2, p1 [2]float32
}

func cloudSegments(w, h float32) []cubicSeg {
	sc := func(x, y float32) [2]float32 { return [2]float32{x * w, y * h} }
	return []cubicSeg{
		{sc(0.18, 0.85), sc(0.38, 0.96), sc(0.62, 0.96), sc(0.80, 0.85)},
		{sc(0.80, 0.85), sc(0.99, 0.83), sc(1.00, 0.62), sc(0.92, 0.50)},
		{sc(0.92, 0.50), sc(0.99, 0.31), sc(0.84, 0.16), sc(0.69, 0.22)},
		{sc(0.69, 0.22), sc(0.62, 0.04), sc(0.38, 0.04), sc(0.32, 0.20)},
		{sc(0.32, 0.20), sc(0.17, 0.10), sc(0.03, 0.25), sc(0.10, 0.44)},
		{sc(0.10, 0.44), sc(-0.01, 0.60), sc(0.02, 0.83), sc(0.18, 0.85)},
	}
}

func cloudPolygon(w, h float32) [][2]float32 {
	segs := cloudSegments(w, h)
	pts := make([][2]float32, 0, len(segs)*6)
	for _, s := range segs {
		for i := 0; i < 6; i++ {
			t := float32(i) / 6
			u := 1 - t
			px := u*u*u*s.p0[0] + 3*u*u*t*s.c1[0] + 3*u*t*t*s.c2[0] + t*t*t*s.p1[0]
			py := u*u*u*s.p0[1] + 3*u*u*t*s.c1[1] + 3*u*t*t*s.c2[1] + t*t*t*s.p1[1]
			pts = append(pts, [2]float32{px, py})
		}
	}
	return pts
}

func roughCloudCmds(w, h float32, rough int, singlePass bool, rng *prng) []PathCmd {
	segs := cloudSegments(w, h)
	passes := 2
	if singlePass || rough == 0 {
		passes = 1
	}
	var cmds []PathCmd
	for pass := range passes {
		jit := float32(rough) * 1.15
		if pass > 0 {
			jit *= 0.85
		}
		startX := segs[0].p0[0] + rng.jitter(jit*0.4)
		startY := segs[0].p0[1] + rng.jitter(jit*0.4)
		cmds = append(cmds, PathCmd{Kind: CmdMoveTo, X: startX, Y: startY})
		for i, s := range segs {
			endX := s.p1[0] + rng.jitter(jit*0.4)
			endY := s.p1[1] + rng.jitter(jit*0.4)
			if i == len(segs)-1 {
				endX, endY = startX, startY
			}
			cmds = append(cmds, PathCmd{
				Kind: CmdCubeTo,
				C1X:  s.c1[0] + rng.jitter(jit),
				C1Y:  s.c1[1] + rng.jitter(jit),
				C2X:  s.c2[0] + rng.jitter(jit),
				C2Y:  s.c2[1] + rng.jitter(jit),
				X:    endX,
				Y:    endY,
			})
		}
		cmds = append(cmds, PathCmd{Kind: CmdClose})
	}
	return cmds
}

// RouteElbowArrow computes clean 90-degree orthogonal waypoints for el.Points
// between (0,0) and its current endpoint when el.Elbowed is true.
func RouteElbowArrow(scene *Scene, el *Element) {
	if el == nil || (el.Kind != KindArrow && el.Kind != KindLine) || !el.Elbowed {
		return
	}
	dx, dy := el.W, el.H
	if n := len(el.Points); n >= 2 {
		dx = el.Points[n-1][0] - el.Points[0][0]
		dy = el.Points[n-1][1] - el.Points[0][1]
	}

	faceIsHoriz := func(binding *Binding, sx, sy float32, fallbackHoriz bool) bool {
		if scene == nil || binding == nil {
			return fallbackHoriz
		}
		target := scene.ElementByID(binding.ElementID)
		if target == nil {
			return fallbackHoriz
		}
		tb := ElementBounds(target)
		cx, cy := tb.Center()
		nx := abs32(sx-cx) / max(1, tb.W/2)
		ny := abs32(sy-cy) / max(1, tb.H/2)
		return nx >= ny
	}

	domHoriz := abs32(dx) >= abs32(dy)
	startHoriz := faceIsHoriz(el.StartBinding, el.X, el.Y, domHoriz)
	endHoriz := faceIsHoriz(el.EndBinding, el.X+dx, el.Y+dy, domHoriz)

	raw := [][2]float32{{0, 0}}
	switch {
	case abs32(dx) < 2 || abs32(dy) < 2:
		raw = append(raw, [2]float32{dx, dy})
	case startHoriz && endHoriz:
		midX := dx * 0.5
		raw = append(raw, [2]float32{midX, 0}, [2]float32{midX, dy}, [2]float32{dx, dy})
	case !startHoriz && !endHoriz:
		midY := dy * 0.5
		raw = append(raw, [2]float32{0, midY}, [2]float32{dx, midY}, [2]float32{dx, dy})
	case startHoriz && !endHoriz:
		raw = append(raw, [2]float32{dx, 0}, [2]float32{dx, dy})
	default:
		raw = append(raw, [2]float32{0, dy}, [2]float32{dx, dy})
	}

	cleaned := make([][2]float32, 0, len(raw))
	for _, pt := range raw {
		if len(cleaned) > 0 {
			prev := cleaned[len(cleaned)-1]
			if abs32(pt[0]-prev[0]) < 0.5 && abs32(pt[1]-prev[1]) < 0.5 {
				continue
			}
		}
		cleaned = append(cleaned, pt)
	}
	if len(cleaned) < 2 {
		cleaned = [][2]float32{{0, 0}, {dx, dy}}
	}
	el.Points = cleaned
	el.W = dx
	el.H = dy
}
