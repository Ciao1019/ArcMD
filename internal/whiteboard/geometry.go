package whiteboard

import (
	"math"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// Box is an axis-aligned rectangle in scene coordinates.
type Box struct {
	X, Y, W, H float32
}

// Empty reports whether Box has no positive area.
func (b Box) Empty() bool { return b.W <= 0 || b.H <= 0 }

// Center returns the center point of b.
func (b Box) Center() (float32, float32) { return b.X + b.W/2, b.Y + b.H/2 }

// Contains reports whether (px, py) lies inside b.
func (b Box) Contains(px, py float32) bool {
	return px >= b.X && px <= b.X+b.W && py >= b.Y && py <= b.Y+b.H
}

// Intersects reports whether a and b overlap.
func (b Box) Intersects(other Box) bool {
	return b.X <= other.X+other.W && b.X+b.W >= other.X &&
		b.Y <= other.Y+other.H && b.Y+b.H >= other.Y
}

// HandleKind identifies a selection resize or endpoint handle.
type HandleKind uint8

const (
	HandleNone HandleKind = iota
	HandleNW
	HandleN
	HandleNE
	HandleE
	HandleSE
	HandleS
	HandleSW
	HandleW
	HandleStart
	HandleMid
	HandleEnd
)

// SnapGuide describes a dynamic alignment guide line shown while dragging.
type SnapGuide struct {
	Vertical bool
	Pos      float32
	From     float32
	To       float32
}

// EstimateTextBounds returns a conservative bounding box size for text when
// explicit measurements from a UI text shaper are not available.
func EstimateTextBounds(text string, fontSize float32) (w, h float32) {
	if fontSize <= 0 {
		fontSize = 18
	}
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	maxW := float32(28)
	for _, line := range lines {
		var lw float32
		for _, r := range line {
			if r > 0x7f {
				lw += fontSize * 1.02
			} else {
				lw += fontSize * 0.58
			}
		}
		if lw > maxW {
			maxW = lw
		}
	}
	lineH := fontSize * 1.32
	return maxW + 12, float32(len(lines))*lineH + 6
}

// ElementBounds returns the normalized axis-aligned bounding box of el.
func ElementBounds(el *Element) Box {
	if el == nil {
		return Box{}
	}
	switch el.Kind {
	case KindArrow, KindLine, KindFreedraw:
		pts := el.Points
		if len(pts) == 0 {
			x, y, w, h := normalizeRect(el.X, el.Y, el.W, el.H)
			return Box{x, y, max(w, 8), max(h, 8)}
		}
		minX, minY := pts[0][0], pts[0][1]
		maxX, maxY := pts[0][0], pts[0][1]
		for _, p := range pts[1:] {
			minX = min(minX, p[0])
			minY = min(minY, p[1])
			maxX = max(maxX, p[0])
			maxY = max(maxY, p[1])
		}
		return Box{
			X: el.X + minX,
			Y: el.Y + minY,
			W: max(1, maxX-minX),
			H: max(1, maxY-minY),
		}
	case KindText:
		w, h := el.W, el.H
		if w <= 0 || h <= 0 {
			ew, eh := EstimateTextBounds(el.Text, el.FontSize)
			if w <= 0 {
				w = ew
			}
			if h <= 0 {
				h = eh
			}
		}
		return Box{X: el.X, Y: el.Y, W: w, H: h}
	default:
		x, y, w, h := normalizeRect(el.X, el.Y, el.W, el.H)
		return Box{X: x, Y: y, W: w, H: h}
	}
}

// SceneBounds returns the tight bounding box covering all elements in scene.
func SceneBounds(scene *Scene) Box {
	if scene == nil || len(scene.Elements) == 0 {
		return Box{}
	}
	var out Box
	has := false
	for _, el := range scene.Elements {
		if el == nil {
			continue
		}
		b := ElementBounds(el)
		if el.Kind == KindFrame && el.Name != "" {
			b.Y -= 22
			b.H += 22
		}
		if !has {
			out, has = b, true
			continue
		}
		right := max(out.X+out.W, b.X+b.W)
		bottom := max(out.Y+out.H, b.Y+b.H)
		out.X = min(out.X, b.X)
		out.Y = min(out.Y, b.Y)
		out.W = right - out.X
		out.H = bottom - out.Y
	}
	return out
}

func normalizeRect(x, y, w, h float32) (float32, float32, float32, float32) {
	if w < 0 {
		x += w
		w = -w
	}
	if h < 0 {
		y += h
		h = -h
	}
	return x, y, w, h
}

// HitTest returns the topmost element under scene point (x, y) within tol.
// Frames are tested last so shapes placed inside a frame can be picked first.
func HitTest(scene *Scene, x, y, tol float32) *Element {
	if scene == nil {
		return nil
	}
	for i := len(scene.Elements) - 1; i >= 0; i-- {
		el := scene.Elements[i]
		if el == nil || el.Kind == KindFrame {
			continue
		}
		if hitElement(el, x, y, tol) {
			return el
		}
	}
	for i := len(scene.Elements) - 1; i >= 0; i-- {
		el := scene.Elements[i]
		if el != nil && el.Kind == KindFrame && hitElement(el, x, y, tol) {
			return el
		}
	}
	return nil
}

func hitElement(el *Element, px, py, tol float32) bool {
	b := ElementBounds(el)
	expanded := Box{b.X - tol, b.Y - tol, b.W + 2*tol, b.H + 2*tol}
	switch el.Kind {
	case KindFrame:
		// Frame hits on its border or its title tab at top-left.
		titleBox := Box{b.X, b.Y - 24, max(80, min(b.W, float32(utf8.RuneCountInString(el.Name)*9+20))), 24}
		if titleBox.Contains(px, py) {
			return true
		}
		if !expanded.Contains(px, py) {
			return false
		}
		inner := Box{b.X + tol + 3, b.Y + tol + 3, b.W - 2*(tol+3), b.H - 2*(tol+3)}
		return !inner.Contains(px, py)
	case KindText:
		return expanded.Contains(px, py)
	case KindArrow, KindLine, KindFreedraw:
		if !expanded.Contains(px, py) {
			return false
		}
		pts := el.Points
		if len(pts) < 2 {
			return expanded.Contains(px, py)
		}
		if el.Text != "" {
			cx, cy := b.Center()
			tw, th := EstimateTextBounds(el.Text, el.FontSize)
			if (Box{cx - tw/2 - tol, cy - th/2 - tol, tw + 2*tol, th + 2*tol}).Contains(px, py) {
				return true
			}
		}
		hitDist := max(tol, el.StrokeWidth*0.6+5)
		for i := 0; i < len(pts)-1; i++ {
			ax, ay := el.X+pts[i][0], el.Y+pts[i][1]
			bx, by := el.X+pts[i+1][0], el.Y+pts[i+1][1]
			if pointSegDist(px, py, ax, ay, bx, by) <= hitDist {
				return true
			}
		}
		return false
	case KindEllipse:
		if !expanded.Contains(px, py) {
			return false
		}
		rx, ry := max(1, b.W/2), max(1, b.H/2)
		cx, cy := b.Center()
		nx, ny := (px-cx)/rx, (py-cy)/ry
		d := nx*nx + ny*ny
		if el.FillColor != "" && el.FillStyle != FillNone || el.Text != "" {
			return d <= 1.25
		}
		// Hollow ellipse also allows easy click inside near perimeter or whole body.
		return d <= 1.25
	case KindDiamond:
		if !expanded.Contains(px, py) {
			return false
		}
		cx, cy := b.Center()
		dx := abs32(px-cx) / max(1, b.W/2)
		dy := abs32(py-cy) / max(1, b.H/2)
		return dx+dy <= 1.18
	default:
		if isExtendedShapeKind(el.Kind) {
			if !expanded.Contains(px, py) {
				return false
			}
			if el.Kind == KindActor {
				return expanded.Contains(px, py)
			}
			if poly := shapePolygon(el.Kind, b.W, b.H, el.Roundness); len(poly) >= 3 {
				lx, ly := px-b.X, py-b.Y
				if pointInPolygon(lx, ly, poly) {
					return true
				}
				hitDist := max(tol, 6)
				for i := range poly {
					a := poly[i]
					nxt := poly[(i+1)%len(poly)]
					if pointSegDist(lx, ly, a[0], a[1], nxt[0], nxt[1]) <= hitDist {
						return true
					}
				}
				return false
			}
		}
		return expanded.Contains(px, py)
	}
}

func pointInPolygon(x, y float32, poly [][2]float32) bool {
	inside := false
	j := len(poly) - 1
	for i := range poly {
		xi, yi := poly[i][0], poly[i][1]
		xj, yj := poly[j][0], poly[j][1]
		if ((yi > y) != (yj > y)) && (x < (xj-xi)*(y-yi)/(yj-yi+1e-6)+xi) {
			inside = !inside
		}
		j = i
	}
	return inside
}

// ElementsInRect returns IDs of all elements overlapping selBox.
func ElementsInRect(scene *Scene, selBox Box) []string {
	if scene == nil {
		return nil
	}
	var ids []string
	for _, el := range scene.Elements {
		if el == nil || el.Kind == KindFrame {
			continue
		}
		if ElementBounds(el).Intersects(selBox) {
			ids = append(ids, el.ID)
		}
	}
	return ids
}

// SelectionBounds returns the tight bounding box enclosing all non-nil elements in elems.
func SelectionBounds(elems []*Element) Box {
	var out Box
	has := false
	for _, el := range elems {
		if el == nil {
			continue
		}
		eb := ElementBounds(el)
		if !has {
			out, has = eb, true
			continue
		}
		right := max(out.X+out.W, eb.X+eb.W)
		bottom := max(out.Y+out.H, eb.Y+eb.H)
		out.X = min(out.X, eb.X)
		out.Y = min(out.Y, eb.Y)
		out.W = right - out.X
		out.H = bottom - out.Y
	}
	return out
}

// BoxHandlePoints returns the 8 interactive resize control points for box b.
func BoxHandlePoints(b Box) map[HandleKind][2]float32 {
	return map[HandleKind][2]float32{
		HandleNW: {b.X, b.Y},
		HandleN:  {b.X + b.W/2, b.Y},
		HandleNE: {b.X + b.W, b.Y},
		HandleE:  {b.X + b.W, b.Y + b.H/2},
		HandleSE: {b.X + b.W, b.Y + b.H},
		HandleS:  {b.X + b.W/2, b.Y + b.H},
		HandleSW: {b.X, b.Y + b.H},
		HandleW:  {b.X, b.Y + b.H/2},
	}
}

var boxHandleHitOrder = []HandleKind{
	HandleNW, HandleNE, HandleSE, HandleSW,
	HandleN, HandleE, HandleS, HandleW,
}

// HitBoxHandle returns which resize handle of box b lies under scene point (x, y).
func HitBoxHandle(b Box, x, y, zoom float32) HandleKind {
	if zoom <= 0 {
		zoom = 1
	}
	r := 8 / zoom
	pts := BoxHandlePoints(b)
	for _, k := range boxHandleHitOrder {
		pt := pts[k]
		if abs32(x-pt[0]) <= r && abs32(y-pt[1]) <= r {
			return k
		}
	}
	return HandleNone
}

// HandlePoints returns the interactive control points for a single selected element.
func HandlePoints(el *Element) map[HandleKind][2]float32 {
	if el == nil {
		return nil
	}
	if (el.Kind == KindArrow || el.Kind == KindLine) && len(el.Points) >= 2 {
		last := el.Points[len(el.Points)-1]
		pts := map[HandleKind][2]float32{
			HandleStart: {el.X + el.Points[0][0], el.Y + el.Points[0][1]},
			HandleEnd:   {el.X + last[0], el.Y + last[1]},
		}
		if !el.Elbowed && (len(el.Points) == 2 || len(el.Points) == 3) {
			mx := el.X + (el.Points[0][0]+last[0])*0.5
			my := el.Y + (el.Points[0][1]+last[1])*0.5
			if len(el.Points) == 3 {
				mx = el.X + el.Points[1][0]
				my = el.Y + el.Points[1][1]
			}
			pts[HandleMid] = [2]float32{mx, my}
		}
		return pts
	}
	return BoxHandlePoints(ElementBounds(el))
}

// HitHandle returns which handle of el is under scene point (x, y).
func HitHandle(el *Element, x, y, zoom float32) HandleKind {
	if el == nil {
		return HandleNone
	}
	if zoom <= 0 {
		zoom = 1
	}
	r := 8 / zoom
	for k, pt := range HandlePoints(el) {
		if abs32(x-pt[0]) <= r && abs32(y-pt[1]) <= r {
			return k
		}
	}
	return HandleNone
}

// MoveElements translates the listed elements (and elements contained in any
// moved Frame) by (dx, dy) and refreshes bound arrows.
func MoveElements(scene *Scene, ids []string, dx, dy float32) {
	if scene == nil || len(ids) == 0 || (dx == 0 && dy == 0) {
		return
	}
	moving := make(map[string]bool, len(ids))
	for _, id := range ids {
		moving[id] = true
	}
	// If a frame is moved, also move elements whose center sits inside it.
	for _, el := range scene.Elements {
		if el == nil || el.Kind != KindFrame || !moving[el.ID] {
			continue
		}
		fb := ElementBounds(el)
		for _, child := range scene.Elements {
			if child == nil || child.ID == el.ID || moving[child.ID] {
				continue
			}
			cx, cy := ElementBounds(child).Center()
			if child.FrameID == el.ID || fb.Contains(cx, cy) {
				moving[child.ID] = true
			}
		}
	}
	for _, el := range scene.Elements {
		if el == nil || !moving[el.ID] || el.Locked {
			continue
		}
		el.X += dx
		el.Y += dy
		el.Version++
	}
	UpdateBindings(scene)
}

func computeResizedBox(orig Box, handle HandleKind, dx, dy float32, preserveAspect bool, minDim float32) (nx0, ny0, nw, nh float32, anchorRight, anchorBottom bool, x1, y1 float32) {
	x0, y0 := orig.X, orig.Y
	x1, y1 = orig.X+orig.W, orig.Y+orig.H
	switch handle {
	case HandleNW:
		x0 += dx
		y0 += dy
	case HandleN:
		y0 += dy
	case HandleNE:
		x1 += dx
		y0 += dy
	case HandleE:
		x1 += dx
	case HandleSE:
		x1 += dx
		y1 += dy
	case HandleS:
		y1 += dy
	case HandleSW:
		x0 += dx
		y1 += dy
	case HandleW:
		x0 += dx
	}
	flippedX := x0 > x1
	if flippedX {
		x0, x1 = x1, x0
	}
	flippedY := y0 > y1
	if flippedY {
		y0, y1 = y1, y0
	}
	anchorRight = (handle == HandleNW || handle == HandleW || handle == HandleSW) != flippedX
	anchorBottom = (handle == HandleNW || handle == HandleN || handle == HandleNE) != flippedY
	nw = max(minDim, x1-x0)
	nh = max(minDim, y1-y0)
	if preserveAspect && orig.W > 0 && orig.H > 0 {
		ratio := orig.W / orig.H
		switch handle {
		case HandleE, HandleW:
			nh = max(minDim, nw/ratio)
		case HandleN, HandleS:
			nw = max(minDim, nh*ratio)
		default:
			if nw/nh > ratio {
				nh = nw / ratio
			} else {
				nw = nh * ratio
			}
		}
	}
	if (handle == HandleN || handle == HandleS) && preserveAspect {
		nx0 = orig.X + (orig.W-nw)*0.5
	} else if anchorRight {
		nx0 = x1 - nw
	} else {
		nx0 = x0
	}
	if (handle == HandleE || handle == HandleW) && preserveAspect {
		ny0 = orig.Y + (orig.H-nh)*0.5
	} else if anchorBottom {
		ny0 = y1 - nh
	} else {
		ny0 = y0
	}
	return nx0, ny0, nw, nh, anchorRight, anchorBottom, x1, y1
}

// ResizeElement resizes el from handle given a scene-space delta relative to
// the starting snapshot orig.
func ResizeElement(el, orig *Element, handle HandleKind, dx, dy float32, preserveAspect bool) {
	if el == nil || orig == nil || el.Locked {
		return
	}
	if handle == HandleStart && len(orig.Points) >= 2 {
		el.X = orig.X + dx
		el.Y = orig.Y + dy
		el.Points = make([][2]float32, len(orig.Points))
		el.Points[0] = [2]float32{0, 0}
		for i := 1; i < len(orig.Points); i++ {
			el.Points[i] = [2]float32{orig.Points[i][0] - dx, orig.Points[i][1] - dy}
		}
		el.Version++
		return
	}
	if handle == HandleMid && len(orig.Points) >= 2 && !orig.Elbowed {
		el.Smooth = true
		p0 := orig.Points[0]
		pLast := orig.Points[len(orig.Points)-1]
		origMidX := (p0[0] + pLast[0]) * 0.5
		origMidY := (p0[1] + pLast[1]) * 0.5
		if len(orig.Points) == 3 {
			origMidX = orig.Points[1][0]
			origMidY = orig.Points[1][1]
		}
		el.Points = [][2]float32{
			p0,
			{origMidX + dx, origMidY + dy},
			pLast,
		}
		el.Version++
		return
	}
	if handle == HandleEnd && len(orig.Points) >= 2 {
		el.Points = make([][2]float32, len(orig.Points))
		copy(el.Points, orig.Points)
		last := len(el.Points) - 1
		el.Points[last] = [2]float32{orig.Points[last][0] + dx, orig.Points[last][1] + dy}
		el.W = el.Points[last][0]
		el.H = el.Points[last][1]
		el.Version++
		return
	}

	b := ElementBounds(orig)
	nx0, ny0, nw, nh, anchorRight, anchorBottom, x1, y1 := computeResizedBox(b, handle, dx, dy, preserveAspect, 12)
	if el.Kind == KindText && b.H > 1 {
		origFS := orig.FontSize
		if origFS <= 0 {
			origFS = 18
		}
		el.FontSize = clamp32(origFS*(nh/b.H), 8, 128)
		if handle == HandleNW || handle == HandleNE || handle == HandleSE || handle == HandleSW {
			tw, th := EstimateTextBounds(el.Text, el.FontSize)
			nw, nh = max(12, tw), max(12, th)
			if anchorRight {
				nx0 = x1 - nw
			}
			if anchorBottom {
				ny0 = y1 - nh
			}
		}
	}
	el.X, el.Y, el.W, el.H = nx0, ny0, nw, nh
	if (el.Kind == KindFreedraw || el.Kind == KindLine || el.Kind == KindArrow) && len(orig.Points) >= 2 && b.W > 1 && b.H > 1 {
		sx, sy := nw/b.W, nh/b.H
		el.Points = make([][2]float32, len(orig.Points))
		for i, pt := range orig.Points {
			el.Points[i] = [2]float32{
				(orig.X + pt[0] - b.X) * sx,
				(orig.Y + pt[1] - b.Y) * sy,
			}
		}
	}
	el.Version++
}

// ResizeElements scales every element in origElems proportionally inside the
// resized bounding box derived from origBox, handle, and scene delta (dx, dy),
// then refreshes bound arrows across the scene.
func ResizeElements(scene *Scene, origBox Box, origElems map[string]*Element, handle HandleKind, dx, dy float32, preserveAspect bool) {
	if scene == nil || len(origElems) == 0 || (origBox.W <= 0 && origBox.H <= 0) {
		return
	}
	nx0, ny0, nw, nh, _, _, _, _ := computeResizedBox(origBox, handle, dx, dy, preserveAspect, 20)
	sx := float32(1)
	if origBox.W > 1e-3 {
		sx = nw / origBox.W
	}
	sy := float32(1)
	if origBox.H > 1e-3 {
		sy = nh / origBox.H
	}
	fontScale := min(sx, sy)

	for _, el := range scene.Elements {
		if el == nil || el.Locked {
			continue
		}
		orig, ok := origElems[el.ID]
		if !ok || orig == nil {
			continue
		}
		switch el.Kind {
		case KindArrow, KindLine, KindFreedraw:
			el.X = nx0 + (orig.X-origBox.X)*sx
			el.Y = ny0 + (orig.Y-origBox.Y)*sy
			if len(orig.Points) > 0 {
				el.Points = make([][2]float32, len(orig.Points))
				for i, pt := range orig.Points {
					el.Points[i] = [2]float32{pt[0] * sx, pt[1] * sy}
				}
				last := el.Points[len(el.Points)-1]
				el.W, el.H = last[0], last[1]
			} else {
				el.W, el.H = orig.W*sx, orig.H*sy
			}
		default:
			ob := ElementBounds(orig)
			el.X = nx0 + (ob.X-origBox.X)*sx
			el.Y = ny0 + (ob.Y-origBox.Y)*sy
			el.W = max(6, ob.W*sx)
			el.H = max(6, ob.H*sy)
		}
		if (orig.Text != "" || el.Kind == KindText || orig.FontSize > 0) && abs32(fontScale-1) > 1e-3 {
			baseFS := orig.FontSize
			if baseFS <= 0 {
				baseFS = 18
			}
			el.FontSize = clamp32(baseFS*fontScale, 8, 128)
		}
		el.Version++
	}
	UpdateBindings(scene)
}

// FindBindTarget finds the closest bindable shape near scene point (x, y).
func FindBindTarget(scene *Scene, excludeID string, x, y, maxDist float32) *Element {
	return FindBindTargetExcluding(scene, x, y, maxDist, excludeID)
}

// FindBindTargetExcluding finds the closest bindable shape near scene point (x, y),
// skipping any element whose ID appears in excludeIDs.
func FindBindTargetExcluding(scene *Scene, x, y, maxDist float32, excludeIDs ...string) *Element {
	if scene == nil {
		return nil
	}
	var best *Element
	bestDist := maxDist
	for i := len(scene.Elements) - 1; i >= 0; i-- {
		el := scene.Elements[i]
		if el == nil || !IsBindableKind(el.Kind) {
			continue
		}
		excluded := false
		for _, ex := range excludeIDs {
			if ex != "" && el.ID == ex {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		b := ElementBounds(el)
		cx, cy := clamp32(x, b.X, b.X+b.W), clamp32(y, b.Y, b.Y+b.H)
		d := float32(math.Hypot(float64(x-cx), float64(y-cy)))
		if d <= bestDist {
			bestDist = d
			best = el
		}
	}
	return best
}

// IsBindableKind reports whether arrows and lines can attach to shapes of kind k.
func IsBindableKind(k ElementKind) bool {
	switch k {
	case KindRectangle, KindDiamond, KindEllipse, KindNote, KindText:
		return true
	}
	return isExtendedShapeKind(k)
}

// HasConnectPorts reports whether el exposes interactive quick-connect ports
// when selected on the canvas.
func HasConnectPorts(el *Element) bool {
	return el != nil && !el.Locked && el.Kind != KindText && IsBindableKind(el.Kind)
}

// ConnectPortPoints returns the 4 quick-connect port positions (Top, Right, Bottom, Left)
// placed 20 screen DIP outside el's bounding box so they stay clear of resize handles.
func ConnectPortPoints(el *Element, zoom float32) [4][2]float32 {
	if el == nil {
		return [4][2]float32{}
	}
	if zoom <= 0 {
		zoom = 1
	}
	b := ElementBounds(el)
	off := 20 / zoom
	return [4][2]float32{
		{b.X + b.W/2, b.Y - off},
		{b.X + b.W + off, b.Y + b.H/2},
		{b.X + b.W/2, b.Y + b.H + off},
		{b.X - off, b.Y + b.H/2},
	}
}

// HitConnectPort checks whether scene point (x, y) hits one of el's 4 quick-connect ports.
func HitConnectPort(el *Element, x, y, zoom float32) ([2]float32, bool) {
	if !HasConnectPorts(el) {
		return [2]float32{}, false
	}
	if zoom <= 0 {
		zoom = 1
	}
	r := float64(8 / zoom)
	for _, pt := range ConnectPortPoints(el, zoom) {
		if math.Hypot(float64(x-pt[0]), float64(y-pt[1])) <= r {
			return pt, true
		}
	}
	return [2]float32{}, false
}

// UpdateBindings recomputes arrow/line start and end points so bound arrows stay
// neatly anchored to the outer perimeter of their connected shapes.
func UpdateBindings(scene *Scene) {
	if scene == nil {
		return
	}
	for _, el := range scene.Elements {
		if el == nil || (el.Kind != KindArrow && el.Kind != KindLine) {
			continue
		}
		if el.StartBinding == nil && el.EndBinding == nil {
			continue
		}
		if len(el.Points) < 2 {
			el.Points = [][2]float32{{0, 0}, {el.W, el.H}}
		}
		var startTarget, endTarget *Element
		if el.StartBinding != nil {
			startTarget = scene.ElementByID(el.StartBinding.ElementID)
		}
		if el.EndBinding != nil {
			endTarget = scene.ElementByID(el.EndBinding.ElementID)
		}
		// Compute world coordinates of opposite anchor points to aim rays accurately.
		lastIdx := len(el.Points) - 1
		startWorld := [2]float32{el.X + el.Points[0][0], el.Y + el.Points[0][1]}
		endWorld := [2]float32{el.X + el.Points[lastIdx][0], el.Y + el.Points[lastIdx][1]}
		aimFromStart := endWorld
		if endTarget != nil {
			cx, cy := ElementBounds(endTarget).Center()
			aimFromStart = [2]float32{cx, cy}
		} else if len(el.Points) > 2 {
			aimFromStart = [2]float32{el.X + el.Points[1][0], el.Y + el.Points[1][1]}
		}
		aimFromEnd := startWorld
		if startTarget != nil {
			cx, cy := ElementBounds(startTarget).Center()
			aimFromEnd = [2]float32{cx, cy}
		} else if len(el.Points) > 2 {
			aimFromEnd = [2]float32{el.X + el.Points[lastIdx-1][0], el.Y + el.Points[lastIdx-1][1]}
		}

		if startTarget != nil {
			gap := el.StartBinding.Gap
			if gap <= 0 {
				gap = 6
			}
			startWorld = shapePerimeterPoint(startTarget, aimFromStart[0], aimFromStart[1], gap)
		}
		if endTarget != nil {
			gap := el.EndBinding.Gap
			if gap <= 0 {
				gap = 6
			}
			endWorld = shapePerimeterPoint(endTarget, aimFromEnd[0], aimFromEnd[1], gap)
		}

		// Preserve intermediate waypoints if any while anchoring endpoints.
		oldOriginX, oldOriginY := el.X, el.Y
		el.X, el.Y = startWorld[0], startWorld[1]
		el.Points[0] = [2]float32{0, 0}
		for i := 1; i < lastIdx; i++ {
			el.Points[i][0] += oldOriginX - el.X
			el.Points[i][1] += oldOriginY - el.Y
		}
		el.Points[lastIdx] = [2]float32{endWorld[0] - el.X, endWorld[1] - el.Y}
		el.W = el.Points[lastIdx][0]
		el.H = el.Points[lastIdx][1]
		if el.Elbowed {
			RouteElbowArrow(scene, el)
		}
		el.Version++
	}
}

func shapePerimeterPoint(target *Element, towardX, towardY, gap float32) [2]float32 {
	b := ElementBounds(target)
	cx, cy := b.Center()
	dx, dy := towardX-cx, towardY-cy
	if dx == 0 && dy == 0 {
		return [2]float32{cx, cy - b.H/2 - gap}
	}
	rx, ry := max(1, b.W/2)+gap, max(1, b.H/2)+gap
	switch target.Kind {
	case KindEllipse:
		ang := math.Atan2(float64(dy)/float64(ry), float64(dx)/float64(rx))
		return [2]float32{
			cx + rx*float32(math.Cos(ang)),
			cy + ry*float32(math.Sin(ang)),
		}
	case KindDiamond:
		denom := abs32(dx)/rx + abs32(dy)/ry
		if denom <= 0 {
			return [2]float32{cx, cy - ry}
		}
		t := 1 / denom
		return [2]float32{cx + dx*t, cy + dy*t}
	default:
		if isExtendedShapeKind(target.Kind) && target.Kind != KindActor {
			if poly := shapePolygon(target.Kind, b.W, b.H, target.Roundness); len(poly) >= 3 {
				if pt, ok := rayPolygonPerimeter(b, poly, dx, dy, gap); ok {
					return pt
				}
			}
		}
		tx := float32(math.MaxFloat32)
		ty := float32(math.MaxFloat32)
		if abs32(dx) > 1e-4 {
			tx = rx / abs32(dx)
		}
		if abs32(dy) > 1e-4 {
			ty = ry / abs32(dy)
		}
		t := min(tx, ty)
		return [2]float32{cx + dx*t, cy + dy*t}
	}
}

func rayPolygonPerimeter(b Box, poly [][2]float32, dx, dy, gap float32) ([2]float32, bool) {
	cx, cy := b.W/2, b.H/2
	bestT := float32(0)
	for i := range poly {
		ax, ay := poly[i][0]-cx, poly[i][1]-cy
		nxt := poly[(i+1)%len(poly)]
		bx, by := nxt[0]-cx, nxt[1]-cy
		ex, ey := bx-ax, by-ay
		denom := dx*(-ey) - dy*(-ex)
		if abs32(denom) < 1e-5 {
			continue
		}
		t := (ax*(-ey) - ay*(-ex)) / denom
		u := (dx*ay - dy*ax) / denom
		if t > 0 && u >= -1e-4 && u <= 1+1e-4 {
			if bestT == 0 || t < bestT {
				bestT = t
			}
		}
	}
	if bestT <= 0 {
		return [2]float32{}, false
	}
	rLen := float32(math.Hypot(float64(dx), float64(dy)))
	if rLen < 1e-4 {
		return [2]float32{}, false
	}
	px := b.X + cx + dx*bestT + dx/rLen*gap
	py := b.Y + cy + dy*bestT + dy/rLen*gap
	return [2]float32{px, py}, true
}

// ElementInGroup reports whether el belongs to group gid.
func ElementInGroup(el *Element, gid string) bool {
	if el == nil || gid == "" {
		return false
	}
	return slices.Contains(el.GroupIDs, gid)
}

// GroupElementsList returns all non-nil elements in scene belonging to gid.
func GroupElementsList(scene *Scene, gid string) []*Element {
	if scene == nil || gid == "" {
		return nil
	}
	var out []*Element
	for _, el := range scene.Elements {
		if ElementInGroup(el, gid) {
			out = append(out, el)
		}
	}
	return out
}

// GroupBounds returns the tight bounding box enclosing all elements in group gid.
func GroupBounds(scene *Scene, gid string) Box {
	return SelectionBounds(GroupElementsList(scene, gid))
}

// NextGroupToEnter returns the next nested group ID inside scopeGroupID that
// contains el (or the outermost group when scopeGroupID is empty).
func NextGroupToEnter(el *Element, scopeGroupID string) string {
	if el == nil || len(el.GroupIDs) == 0 {
		return ""
	}
	if scopeGroupID == "" {
		for i := len(el.GroupIDs) - 1; i >= 0; i-- {
			if el.GroupIDs[i] != "" {
				return el.GroupIDs[i]
			}
		}
		return ""
	}
	idx := slices.Index(el.GroupIDs, scopeGroupID)
	if idx <= 0 {
		return ""
	}
	for i := idx - 1; i >= 0; i-- {
		if el.GroupIDs[i] != "" {
			return el.GroupIDs[i]
		}
	}
	return ""
}

// ParentGroupOf returns the enclosing parent group shared by all members of gid,
// or "" if gid is a top-level group.
func ParentGroupOf(scene *Scene, gid string) string {
	members := GroupElementsList(scene, gid)
	if len(members) == 0 {
		return ""
	}
	first := members[0]
	idx := slices.Index(first.GroupIDs, gid)
	if idx < 0 {
		return ""
	}
	for i := idx + 1; i < len(first.GroupIDs); i++ {
		cand := first.GroupIDs[i]
		if cand == "" {
			continue
		}
		allIn := true
		for _, m := range members[1:] {
			if !ElementInGroup(m, cand) {
				allIn = false
				break
			}
		}
		if allIn {
			return cand
		}
	}
	return ""
}

// GroupChainFor returns a cloned slice of group IDs from gid outward so newly
// created elements inside group isolation stay part of gid and its ancestors.
func GroupChainFor(scene *Scene, gid string) []string {
	if scene == nil || gid == "" {
		return nil
	}
	for _, el := range scene.Elements {
		if el == nil {
			continue
		}
		if idx := slices.Index(el.GroupIDs, gid); idx >= 0 {
			return slices.Clone(el.GroupIDs[idx:])
		}
	}
	return nil
}

// ExpandGroupSelection expands sel to include all sibling elements sharing
// any groupId with the currently selected elements.
func ExpandGroupSelection(scene *Scene, sel map[string]bool) map[string]bool {
	return ExpandGroupSelectionWithin(scene, sel, "")
}

// ExpandGroupSelectionWithin expands sel to include sibling group members while
// stopping expansion at scopeGroupID so direct children inside an isolated group
// can be selected individually.
func ExpandGroupSelectionWithin(scene *Scene, sel map[string]bool, scopeGroupID string) map[string]bool {
	out := make(map[string]bool, len(sel))
	for k, v := range sel {
		if v {
			out[k] = true
		}
	}
	if scene == nil || len(out) == 0 {
		return out
	}
	activeGroups := map[string]bool{}
	for _, el := range scene.Elements {
		if el == nil || !out[el.ID] {
			continue
		}
		if scopeGroupID != "" && ElementInGroup(el, scopeGroupID) {
			if sub := NextGroupToEnter(el, scopeGroupID); sub != "" {
				activeGroups[sub] = true
			}
			continue
		}
		for _, g := range el.GroupIDs {
			if g != "" {
				activeGroups[g] = true
			}
		}
	}
	if len(activeGroups) == 0 {
		return out
	}
	for _, el := range scene.Elements {
		if el == nil {
			continue
		}
		for _, g := range el.GroupIDs {
			if activeGroups[g] {
				out[el.ID] = true
				break
			}
		}
	}
	return out
}

// SharedGroupID returns the common immediate group ID below scopeGroupID shared
// by all elements in elems (if len(elems) >= 2), or "".
func SharedGroupID(elems []*Element, scopeGroupID string) string {
	if len(elems) < 2 {
		return ""
	}
	gid := NextGroupToEnter(elems[0], scopeGroupID)
	if gid == "" {
		return ""
	}
	for _, el := range elems[1:] {
		if NextGroupToEnter(el, scopeGroupID) != gid {
			return ""
		}
	}
	return gid
}

// GroupElements assigns a new shared groupId to all listed elements and returns it.
func GroupElements(scene *Scene, ids []string) string {
	if scene == nil || len(ids) < 2 {
		return ""
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	gid := NewID()
	var count int
	for _, el := range scene.Elements {
		if el != nil && set[el.ID] {
			el.GroupIDs = append(el.GroupIDs, gid)
			el.Version++
			count++
		}
	}
	if count < 2 {
		return ""
	}
	return gid
}

// UngroupElements detaches the outermost groupId shared by listed elements.
func UngroupElements(scene *Scene, ids []string) bool {
	if scene == nil || len(ids) == 0 {
		return false
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	drop := map[string]bool{}
	for _, el := range scene.Elements {
		if el != nil && set[el.ID] && len(el.GroupIDs) > 0 {
			drop[el.GroupIDs[len(el.GroupIDs)-1]] = true
		}
	}
	if len(drop) == 0 {
		return false
	}
	changed := false
	for _, el := range scene.Elements {
		if el == nil || !set[el.ID] || len(el.GroupIDs) == 0 {
			continue
		}
		kept := el.GroupIDs[:0]
		for _, g := range el.GroupIDs {
			if !drop[g] {
				kept = append(kept, g)
			} else {
				changed = true
			}
		}
		el.GroupIDs = kept
		if len(el.GroupIDs) == 0 {
			el.GroupIDs = nil
		}
		el.Version++
	}
	return changed
}

// AlignElements aligns 2+ selected elements along left, center-h, right, top, center-v, or bottom.
func AlignElements(scene *Scene, ids []string, mode string) bool {
	if scene == nil || len(ids) < 2 {
		return false
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	var targets []*Element
	var minX, minY, maxX, maxY float32
	for _, el := range scene.Elements {
		if el == nil || !set[el.ID] || el.Locked {
			continue
		}
		eb := ElementBounds(el)
		if len(targets) == 0 {
			minX, minY, maxX, maxY = eb.X, eb.Y, eb.X+eb.W, eb.Y+eb.H
		} else {
			minX = min(minX, eb.X)
			minY = min(minY, eb.Y)
			maxX = max(maxX, eb.X+eb.W)
			maxY = max(maxY, eb.Y+eb.H)
		}
		targets = append(targets, el)
	}
	if len(targets) < 2 {
		return false
	}
	midX := (minX + maxX) * 0.5
	midY := (minY + maxY) * 0.5
	changed := false
	for _, el := range targets {
		eb := ElementBounds(el)
		var dx, dy float32
		switch mode {
		case "left":
			dx = minX - eb.X
		case "center-h":
			dx = midX - (eb.X + eb.W*0.5)
		case "right":
			dx = maxX - (eb.X + eb.W)
		case "top":
			dy = minY - eb.Y
		case "center-v":
			dy = midY - (eb.Y + eb.H*0.5)
		case "bottom":
			dy = maxY - (eb.Y + eb.H)
		}
		if abs32(dx) > 0.01 || abs32(dy) > 0.01 {
			el.X += dx
			el.Y += dy
			el.Version++
			changed = true
		}
	}
	if changed {
		UpdateBindings(scene)
	}
	return changed
}

// DistributeElements spaces 3+ selected elements evenly horizontally or vertically.
func DistributeElements(scene *Scene, ids []string, horizontal bool) bool {
	if scene == nil || len(ids) < 3 {
		return false
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	type item struct {
		el *Element
		b  Box
	}
	var items []item
	for _, el := range scene.Elements {
		if el == nil || !set[el.ID] || el.Locked {
			continue
		}
		items = append(items, item{el: el, b: ElementBounds(el)})
	}
	if len(items) < 3 {
		return false
	}
	sort.Slice(items, func(i, j int) bool {
		if horizontal {
			return items[i].b.X < items[j].b.X
		}
		return items[i].b.Y < items[j].b.Y
	})
	var totalSpan, occupied float32
	if horizontal {
		totalSpan = (items[len(items)-1].b.X + items[len(items)-1].b.W) - items[0].b.X
		for _, it := range items {
			occupied += it.b.W
		}
	} else {
		totalSpan = (items[len(items)-1].b.Y + items[len(items)-1].b.H) - items[0].b.Y
		for _, it := range items {
			occupied += it.b.H
		}
	}
	gap := (totalSpan - occupied) / float32(len(items)-1)
	cursor := items[0].b.X
	if !horizontal {
		cursor = items[0].b.Y
	}
	changed := false
	for _, it := range items {
		if horizontal {
			if d := cursor - it.b.X; abs32(d) > 0.01 {
				it.el.X += d
				it.el.Version++
				changed = true
			}
			cursor += it.b.W + gap
		} else {
			if d := cursor - it.b.Y; abs32(d) > 0.01 {
				it.el.Y += d
				it.el.Version++
				changed = true
			}
			cursor += it.b.H + gap
		}
	}
	if changed {
		UpdateBindings(scene)
	}
	return changed
}

// ComputeSnapGuides finds horizontal and vertical alignment guides against
// stationary elements when moving box.
func ComputeSnapGuides(scene *Scene, moving map[string]bool, box Box, threshold float32) (guides []SnapGuide, snapDX, snapDY float32) {
	if scene == nil || threshold <= 0 {
		return nil, 0, 0
	}
	mx := [3]float32{box.X, box.X + box.W/2, box.X + box.W}
	my := [3]float32{box.Y, box.Y + box.H/2, box.Y + box.H}
	bestDX := threshold + 1
	bestDY := threshold + 1
	var bestXRef, bestYRef float32
	var bestXOther, bestYOther Box

	for _, el := range scene.Elements {
		if el == nil || moving[el.ID] || el.Kind == KindFrame || el.Kind == KindArrow || el.Kind == KindLine || el.Kind == KindFreedraw {
			continue
		}
		ob := ElementBounds(el)
		ox := [3]float32{ob.X, ob.X + ob.W/2, ob.X + ob.W}
		oy := [3]float32{ob.Y, ob.Y + ob.H/2, ob.Y + ob.H}
		for _, a := range mx {
			for _, b := range ox {
				if d := abs32(b - a); d < abs32(bestDX) {
					bestDX = b - a
					bestXRef = b
					bestXOther = ob
				}
			}
		}
		for _, a := range my {
			for _, b := range oy {
				if d := abs32(b - a); d < abs32(bestDY) {
					bestDY = b - a
					bestYRef = b
					bestYOther = ob
				}
			}
		}
	}
	if abs32(bestDX) <= threshold {
		snapDX = bestDX
		guides = append(guides, SnapGuide{
			Vertical: true,
			Pos:      bestXRef,
			From:     min(box.Y, bestXOther.Y) - 12,
			To:       max(box.Y+box.H, bestXOther.Y+bestXOther.H) + 12,
		})
	}
	if abs32(bestDY) <= threshold {
		snapDY = bestDY
		guides = append(guides, SnapGuide{
			Vertical: false,
			Pos:      bestYRef,
			From:     min(box.X, bestYOther.X) - 12,
			To:       max(box.X+box.W, bestYOther.X+bestYOther.W) + 12,
		})
	}
	return guides, snapDX, snapDY
}

func pointSegDist(px, py, ax, ay, bx, by float32) float32 {
	dx, dy := bx-ax, by-ay
	l2 := dx*dx + dy*dy
	if l2 < 1e-4 {
		return float32(math.Hypot(float64(px-ax), float64(py-ay)))
	}
	t := clamp32(((px-ax)*dx+(py-ay)*dy)/l2, 0, 1)
	cx, cy := ax+t*dx, ay+t*dy
	return float32(math.Hypot(float64(px-cx), float64(py-cy)))
}

func clamp32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
