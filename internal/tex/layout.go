package tex

import (
	"math"
	"strings"
)

type layouter struct {
	m    Measure
	bold bool
}

// Metrics in em, after TeX's: the math axis, where fraction bars and
// operators center, above the baseline, and the size scripts and fractions
// shrink to.
const (
	axisHeight  = 0.25
	scriptScale = 0.72
	fracScale   = 0.86
	minSize     = 6
)

// moved returns ops moved by dx, dy.
func moved(ops []Op, dx, dy float32) []Op {
	out := make([]Op, len(ops))
	for i, op := range ops {
		op.X += dx
		op.Y += dy
		if op.Points != nil {
			pts := make([][2]float32, len(op.Points))
			for j, p := range op.Points {
				pts[j] = [2]float32{p[0] + dx, p[1] + dy}
			}
			op.Points = pts
		}
		out[i] = op
	}
	return out
}

// place adds b's ops to box with b's baseline origin at x, y.
func (b *Box) place(c Box, x, y float32) {
	b.Ops = append(b.Ops, moved(c.Ops, x, y)...)
	b.Ascent = max(b.Ascent, c.Ascent-y)
	b.Descent = max(b.Descent, c.Descent+y)
	b.W = max(b.W, x+c.W)
}

func (l *layouter) text(s string, size float32, italic, bad bool) Box {
	if s == "" {
		return Box{}
	}
	w, a, d := l.m(s, size, italic, l.bold)
	if italic {
		// Room for the slant of the last letter.
		w += size * 0.04
	}
	return Box{W: w, Ascent: a, Descent: d, Ops: []Op{{Kind: "text", Text: s, Size: size, Italic: italic, Bold: l.bold, Error: bad}}}
}

func smaller(size, scale float32) float32 { return max(minSize, size*scale) }

// class is the spacing class of an atom, after TeX's.
func class(n *node) string {
	if n.kind == "bold" && len(n.args[0]) == 1 {
		return class(n.args[0][0])
	}
	switch n.kind {
	case "bin", "rel", "open", "close", "punct":
		return n.kind
	case "xarrow":
		return "rel"
	case "not":
		if len(n.args) > 0 && len(n.args[0]) == 1 {
			return class(n.args[0][0])
		}
		return "rel"
	case "big", "func":
		return "op"
	case "space":
		return "space"
	case "delim", "frac":
		return "inner"
	}
	return "ord"
}

// spacing is the space between atoms of classes a and b, in em.
func spacing(a, b string) float32 {
	switch {
	case a == "" || a == "space" || b == "space":
		return 0
	case a == "rel" && b == "rel":
		return 0
	case a == "rel" || b == "rel":
		return 5.0 / 18
	case a == "bin" || b == "bin":
		return 4.0 / 18
	case a == "punct":
		return 3.0 / 18
	case a == "op" && (b == "ord" || b == "op" || b == "inner"), b == "op" && (a == "ord" || a == "close" || a == "op" || a == "inner"):
		return 3.0 / 18
	case a == "inner" && b != "close" && b != "punct", b == "inner" && a != "open":
		return 3.0 / 18
	}
	return 0
}

// atomClasses classifies top-level atoms and turns unary signs into ordinals.
func atomClasses(nodes []*node) []string {
	classes := make([]string, len(nodes))
	for i, n := range nodes {
		classes[i] = class(n)
	}
	// A binary operator with nothing to join on its left or right is a
	// sign: "-x", "(+1)", "a = -b".
	for i, c := range classes {
		if c != "bin" {
			continue
		}
		prev, next := "", ""
		if i > 0 {
			prev = classes[i-1]
		}
		if i+1 < len(classes) {
			next = classes[i+1]
		}
		if prev == "" || prev == "bin" || prev == "rel" || prev == "open" || prev == "punct" || prev == "op" || next == "" || next == "rel" || next == "close" || next == "punct" {
			classes[i] = "ord"
		}
	}
	return classes
}

func (l *layouter) placeAtoms(atoms []Box, classes []string, size float32) Box {
	var out Box
	x, prev := float32(0), ""
	for i, b := range atoms {
		x += spacing(prev, classes[i]) * size
		out.place(b, x, 0)
		x += b.W
		out.W = max(out.W, x)
		prev = classes[i]
	}
	if len(atoms) == 0 {
		// An empty list keeps the height of a line, as "{}" does.
		_, a, d := l.m("x", size, false, l.bold)
		out.Ascent, out.Descent = a*0.6, d*0.2
	}
	return out
}

// list lays a list of atoms out on a line.
func (l *layouter) list(nodes []*node, size float32, display bool) Box {
	classes := atomClasses(nodes)
	atoms := make([]Box, len(nodes))
	for i, n := range nodes {
		atoms[i] = l.atom(n, size, display)
	}
	return l.placeAtoms(atoms, classes, size)
}

// listWrapped lays a list of atoms out, breaking before top-level relations or
// binary operators when the single-line width exceeds maxWidth.
func (l *layouter) listWrapped(nodes []*node, size, maxWidth float32, display bool) Box {
	classes := atomClasses(nodes)
	atoms := make([]Box, len(nodes))
	for i, n := range nodes {
		atoms[i] = l.atom(n, size, display)
	}
	single := l.placeAtoms(atoms, classes, size)
	if maxWidth <= 0 || maxWidth < size*3.5 || single.W <= maxWidth*1.05 || len(nodes) < 2 {
		return single
	}
	indent := size * 0.9
	type span struct{ a, b int }
	var spans []span
	start, lastRel, lastBin := 0, -1, -1
	curX := float32(0)
	for i := 0; i < len(nodes); i++ {
		if i == start {
			curX = 0
			if len(spans) > 0 {
				curX = min(indent, maxWidth*0.2)
			}
		} else {
			if classes[i] == "rel" {
				lastRel = i
			} else if classes[i] == "bin" {
				lastBin = i
			}
			curX += spacing(classes[i-1], classes[i]) * size
		}
		curX += atoms[i].W
		if curX > maxWidth {
			brk := lastBin
			if brk <= start {
				brk = lastRel
			} else if lastRel > start && len(spans) == 0 {
				// Prefer keeping the left-hand side with the first relation if
				// a binary break exists; otherwise break before the relation.
				_ = lastRel
			}
			if brk > start {
				spans = append(spans, span{start, brk})
				start = brk
				lastRel, lastBin = -1, -1
				i = start - 1
				continue
			}
		}
	}
	spans = append(spans, span{start, len(nodes)})
	if len(spans) <= 1 {
		return single
	}
	lines := make([]Box, len(spans))
	maxW := float32(0)
	for idx, sp := range spans {
		b := l.placeAtoms(atoms[sp.a:sp.b], classes[sp.a:sp.b], size)
		if idx > 0 {
			pad := min(indent, max(0, maxWidth-b.W))
			if pad > 0 {
				shifted := Box{}
				shifted.place(b, pad, 0)
				b = shifted
			}
		}
		lines[idx] = b
		maxW = max(maxW, b.W)
	}
	if maxW >= single.W {
		return single
	}
	gap := size * 0.26
	if display {
		gap = size * 0.3
	}
	out := Box{}
	y := float32(0)
	for i, b := range lines {
		if i > 0 {
			y += lines[i-1].Descent + gap + b.Ascent
		}
		out.place(b, 0, y)
	}
	out.W = maxW
	return out
}

// atom lays one atom out with its scripts.
func (l *layouter) atom(n *node, size float32, display bool) Box {
	var base Box
	switch n.kind {
	case "ord", "bin", "rel", "open", "close", "punct":
		base = l.text(n.text, size, n.italic, false)
		if n.text == "" {
			_, a, d := l.m("x", size, false, l.bold)
			base = Box{Ascent: a * .6, Descent: d * .2}
		}
	case "error":
		base = l.text(n.text, size, false, true)
	case "text":
		base = l.text(n.text, size, n.italic, false)
	case "func":
		base = l.text(n.text, size, false, false)
	case "group":
		base = l.list(n.args[0], size, display)
	case "bold":
		// Keep the style inside the argument, including nested structures,
		// without changing following atoms or scripts outside the group.
		styled := *l
		styled.bold = true
		base = styled.list(n.args[0], size, display)
	case "space":
		return Box{W: n.space * size}
	case "frac":
		base = l.fraction(n, size, display)
	case "sqrt":
		base = l.root(n, size, display)
	case "big":
		base = l.bigOperator(n.text, size, display)
	case "accent":
		base = l.accent(n, size, display)
	case "delim":
		inner := l.list(n.args[0], size, display)
		base = l.delimited(inner, n.open, n.close, size)
	case "matrix":
		base = l.matrixWithWidth(n, size, 0, display)
	case "not":
		base = l.list(n.args[0], size, display)
		th := max(1, size*0.055)
		base.Ops = append(base.Ops, Op{
			Kind:   "path",
			Width:  th,
			Points: [][2]float32{{base.W * 0.18, base.Descent * 0.5}, {base.W * 0.82, -base.Ascent * 0.82}},
		})
	case "xarrow":
		base = l.xarrow(n, size)
	case "tag":
		tb := l.list(n.args[0], size, false)
		base.place(tb, size*0.8, 0)
	}
	if !n.hasSup && !n.hasSub {
		return base
	}
	ss := smaller(size, scriptScale)
	var sup, sub *Box
	if n.hasSup {
		b := l.list(n.sup, ss, false)
		sup = &b
	}
	if n.hasSub {
		b := l.list(n.sub, ss, false)
		sub = &b
	}
	// A styled single atom keeps its operator geometry. Stop at an inner
	// script: outer scripts then belong to the whole already-scripted box.
	baseAtom := n
	for baseAtom.kind == "bold" && len(baseAtom.args[0]) == 1 {
		inner := baseAtom.args[0][0]
		if inner.hasSup || inner.hasSub {
			break
		}
		baseAtom = inner
	}
	if n.limits && (display || baseAtom.kind == "func") && (baseAtom.kind == "big" || baseAtom.kind == "func") {
		return l.limits(base, sup, sub, size)
	}
	correction := float32(0)
	if n.italic {
		correction = size * .03
	}
	if baseAtom.kind == "big" && strings.ContainsAny(baseAtom.text, "∫∬∭∮") {
		// An integral leans beyond its advance at the upper hook. Its
		// upper limit needs clearance while the lower one stays close.
		correction = size * .22
	}
	return l.scripts(base, sup, sub, size, correction)
}

func (l *layouter) xarrow(n *node, size float32) Box {
	ss := smaller(size, scriptScale)
	var above, below Box
	if len(n.args[0]) > 0 {
		above = l.list(n.args[0], ss, false)
	}
	if len(n.args[1]) > 0 {
		below = l.list(n.args[1], ss, false)
	}
	hasLeft := n.text == "xleftarrow" || n.text == "xleftrightarrow"
	hasRight := n.text == "xrightarrow" || n.text == "xleftrightarrow"
	axisY := -size * axisHeight
	th := max(1, size*0.055)
	head := size * 0.22
	padL, padR := size*0.22, size*0.22
	if hasLeft {
		padL += head
	}
	if hasRight {
		padR += head
	}
	w := max(size*1.4, max(above.W, below.W)+padL+padR)
	out := Box{}
	out.Ops = append(out.Ops, Op{Kind: "path", Width: th, Points: [][2]float32{{0, axisY}, {w, axisY}}})
	if hasRight {
		out.Ops = append(out.Ops, Op{Kind: "path", Width: th, Points: [][2]float32{{w - head, axisY - head*0.7}, {w, axisY}, {w - head, axisY + head*0.7}}})
	}
	if hasLeft {
		out.Ops = append(out.Ops, Op{Kind: "path", Width: th, Points: [][2]float32{{head, axisY - head*0.7}, {0, axisY}, {head, axisY + head*0.7}}})
	}
	gap := size * 0.14
	midX := padL + (w-padL-padR)/2
	if len(n.args[0]) > 0 {
		out.place(above, midX-above.W/2, axisY-gap-above.Descent*0.75)
	}
	if len(n.args[1]) > 0 {
		out.place(below, midX-below.W/2, axisY+gap+below.Ascent*0.85)
	}
	out.Ascent = max(out.Ascent, -axisY+head*0.7)
	out.Descent = max(out.Descent, max(0, axisY+head*0.7)+size*0.1)
	out.W = w
	return out
}

// scripts sets a superscript and a subscript beside a base.
func (l *layouter) scripts(base Box, sup, sub *Box, size, correction float32) Box {
	out := Box{}
	out.place(base, 0, 0)
	x := base.W
	up := max(size*0.4, base.Ascent-size*0.48)
	down := max(size*0.2, base.Descent-size*0.12)
	if sup != nil && sub != nil {
		// Keep a little room between them.
		gap := (down - sub.Ascent*0.7) - (-up + sup.Descent*0.5)
		if want := size * 0.12; gap < want {
			down += want - gap
		}
	}
	w := float32(0)
	if sup != nil {
		out.place(*sup, x+correction, -up)
		w = max(w, sup.W+correction)
	}
	if sub != nil {
		out.place(*sub, x, down)
		w = max(w, sub.W)
	}
	out.W = x + w + size*0.03
	return out
}

// limits sets scripts above and below a big operator, centered.
func (l *layouter) limits(base Box, sup, sub *Box, size float32) Box {
	w := base.W
	if sup != nil {
		w = max(w, sup.W)
	}
	if sub != nil {
		w = max(w, sub.W)
	}
	out := Box{}
	out.place(base, (w-base.W)/2, 0)
	gap := size * 0.1
	if sup != nil {
		out.place(*sup, (w-sup.W)/2, -(base.Ascent + gap + sup.Descent*0.6))
	}
	if sub != nil {
		out.place(*sub, (w-sub.W)/2, base.Descent+gap+sub.Ascent*0.7)
	}
	out.W = w
	return out
}

// fraction sets a numerator over a denominator, a bar between them on
// the math axis, or a binomial in parentheses.
func (l *layouter) fraction(n *node, size float32, display bool) Box {
	fs := size
	if !display {
		fs = smaller(size, fracScale)
	}
	num := l.list(n.args[0], fs, false)
	den := l.list(n.args[1], fs, false)
	axis := size * axisHeight
	th := max(1, size*0.055)
	gap := size * 0.06
	pad := size * 0.12
	w := max(num.W, den.W) + 2*pad
	out := Box{}
	out.place(num, (w-num.W)/2, -axis-th/2-gap-num.Descent*0.75)
	out.place(den, (w-den.W)/2, -axis+th/2+gap+den.Ascent*0.85)
	if n.text == "binom" {
		return l.delimited(out, "(", ")", size)
	}
	out.Ops = append(out.Ops, Op{Kind: "rule", X: pad * 0.4, Y: -axis - th/2, W: w - pad*0.8, H: th})
	out.W = w
	return out
}

// root sets a radicand under a radical sign, an index in its crook.
func (l *layouter) root(n *node, size float32, display bool) Box {
	inner := l.list(n.args[0], size, display)
	th := max(1, size*0.06)
	pad := size * 0.1
	top := -(inner.Ascent*0.85 + pad)
	bottom := inner.Descent*0.6 + size*0.04
	sign := size * 0.55
	mid := top + (bottom-top)*0.58
	lead := float32(0)
	var index Box
	if len(n.args[1]) > 0 {
		index = l.list(n.args[1], smaller(size, 0.55), false)
		lead = max(0, index.W-sign*0.45)
	}
	x0 := lead
	end := x0 + sign + pad*0.6 + inner.W + pad*0.4
	out := Box{}
	out.Ops = append(out.Ops, Op{Kind: "path", Width: th, Points: [][2]float32{
		{x0, mid + size*0.02}, {x0 + sign*0.22, mid - size*0.06}, {x0 + sign*0.52, bottom}, {x0 + sign, top}, {end, top},
	}})
	out.place(inner, x0+sign+pad*0.6, 0)
	if len(n.args[1]) > 0 {
		out.place(index, x0+sign*0.5-index.W, mid-size*0.12-index.Descent)
	}
	out.Ascent = max(out.Ascent, -top+th)
	out.Descent = max(out.Descent, bottom)
	out.W = end + th
	return out
}

// bigOperator draws a sum, a product or an integral, larger in display
// math, centered on the math axis.
func (l *layouter) bigOperator(s string, size float32, display bool) Box {
	gs := size * 1.2
	if display {
		gs = size * 1.6
		if strings.ContainsAny(s, "∫∬∭∮") {
			gs = size * 1.9
		}
	}
	w, a, d := l.m(s, gs, false, l.bold)
	axis := size * axisHeight
	// The glyph's ink is about its em box's middle 80%, centered there.
	dy := -axis - (d-a)/2 - gs*0.08
	return Box{W: w, Ascent: a - dy - gs*0.1, Descent: dy + d, Ops: []Op{{Kind: "text", Text: s, Size: gs, Y: dy, Bold: l.bold}}}
}

// accent draws a mark over (or a line under) its base.
func (l *layouter) accent(n *node, size float32, display bool) Box {
	base := l.list(n.args[0], size, display)
	out := Box{}
	out.place(base, 0, 0)
	th := max(1, size*0.05)
	switch n.text {
	case "line":
		y := -(base.Ascent*0.82 + size*0.06)
		out.Ops = append(out.Ops, Op{Kind: "rule", X: size * 0.02, Y: y - th, W: base.W - size*0.04, H: th})
		out.Ascent = max(out.Ascent, -y+th)
	case "under":
		y := base.Descent*0.6 + size*0.06
		out.Ops = append(out.Ops, Op{Kind: "rule", X: 0, Y: y, W: base.W, H: th})
		out.Descent = max(out.Descent, y+th)
	case "arrow":
		y := -(base.Ascent*0.82 + size*0.1)
		head := size * 0.14
		w := max(base.W, size*0.5)
		out.Ops = append(out.Ops,
			Op{Kind: "path", Width: th, Points: [][2]float32{{0, y}, {w, y}}},
			Op{Kind: "path", Width: th, Points: [][2]float32{{w - head, y - head*0.7}, {w, y}, {w - head, y + head*0.7}}})
		out.Ascent = max(out.Ascent, -y+head)
		out.W = max(out.W, w)
	default:
		mark := l.text(n.text, size*0.9, false, false)
		out.place(mark, (base.W-mark.W)/2+size*0.04, -(base.Ascent - size*0.55))
	}
	return out
}

// delimited puts delimiters either side of a box, as tall as it is around
// the math axis: glyphs while that is about a line, drawn beyond.
func (l *layouter) delimited(inner Box, open, closing string, size float32) Box {
	axis := size * axisHeight
	ext := max(inner.Ascent*0.8-axis, inner.Descent*0.8+axis, size*0.5)
	out := Box{}
	x := float32(0)
	draw := func(d string, left bool) {
		if d == "" {
			x += size * 0.05
			return
		}
		b := l.delimiter(d, ext, size, left)
		out.place(b, x, 0)
		x += b.W
	}
	draw(open, true)
	out.place(inner, x, 0)
	x += inner.W
	draw(closing, false)
	out.W = x
	return out
}

// delimiter is a delimiter reaching ext above and below the math axis.
func (l *layouter) delimiter(d string, ext, size float32, left bool) Box {
	axis := size * axisHeight
	if ext <= size*0.62 {
		b := l.text(d, size, false, false)
		return b
	}
	top, bottom := -axis-ext, -axis+ext
	th := max(1, size*0.06)
	w := min(size*0.45, size*0.25+ext*0.08)
	var pts [][2]float32
	edge := func(x float32) float32 {
		if left {
			return x
		}
		return w - x
	}
	switch d {
	case "(", ")":
		// A shallow arc.
		for i := 0; i <= 16; i++ {
			t := float64(i) / 16
			y := float64(top) + (float64(bottom)-float64(top))*t
			bulge := float32(math.Sin(t*math.Pi)) * w * 0.7
			pts = append(pts, [2]float32{edge(w*0.85 - bulge), float32(y)})
		}
	case "[", "]", "⌊", "⌋", "⌈", "⌉":
		in, out := edge(w*0.25), edge(w*0.85)
		switch d {
		case "[", "]":
			pts = [][2]float32{{out, top}, {in, top}, {in, bottom}, {out, bottom}}
		case "⌊", "⌋":
			pts = [][2]float32{{in, top}, {in, bottom}, {out, bottom}}
		default:
			pts = [][2]float32{{out, top}, {in, top}, {in, bottom}}
		}
	case "{", "}":
		mid := -axis
		in, tip, out := edge(w*0.45), edge(w*0.1), edge(w*0.9)
		pts = [][2]float32{{out, top}, {in, top + ext*0.12}, {in, mid - ext*0.15}, {tip, mid}, {in, mid + ext*0.15}, {in, bottom - ext*0.12}, {out, bottom}}
	case "⟨", "⟩":
		pts = [][2]float32{{edge(w * 0.85), top}, {edge(w * 0.2), -axis}, {edge(w * 0.85), bottom}}
	case "|", "‖":
		x := w * 0.5
		pts = [][2]float32{{x, top}, {x, bottom}}
		if d == "‖" {
			b := Box{W: w, Ascent: -top, Descent: bottom}
			b.Ops = []Op{{Kind: "path", Width: th, Points: [][2]float32{{x - size*0.07, top}, {x - size*0.07, bottom}}},
				{Kind: "path", Width: th, Points: [][2]float32{{x + size*0.07, top}, {x + size*0.07, bottom}}}}
			return b
		}
	default:
		return l.text(d, size, false, false)
	}
	return Box{W: w, Ascent: -top, Descent: bottom, Ops: []Op{{Kind: "path", Width: th, Points: pts}}}
}

type alignment int

const (
	alignCenter alignment = iota
	alignLeft
	alignRight
)

// extractTags splits \tag / \tag* nodes out of a node list so the equation body
// and its right-hand tag can be positioned independently.
func extractTags(nodes []*node) (clean, tag []*node) {
	hasTag := false
	for _, n := range nodes {
		if n.kind == "tag" {
			hasTag = true
			break
		}
	}
	if !hasTag {
		return nodes, nil
	}
	clean = make([]*node, 0, len(nodes))
	for _, n := range nodes {
		if n.kind == "tag" {
			if len(tag) > 0 {
				tag = append(tag, &node{kind: "text", text: " "})
			}
			tag = append(tag, n.args[0]...)
			continue
		}
		clean = append(clean, n)
	}
	return clean, tag
}

func tagLineColumns(mathW, tagW, size, lineWidth float32, display bool) (totalW, mathX float32) {
	minGap := size * 1.5
	if display && lineWidth > 0 && mathW+minGap+tagW <= lineWidth {
		return lineWidth, min((lineWidth-mathW)/2, lineWidth-tagW-minGap-mathW)
	}
	return mathW + size*2 + tagW, 0
}

// lines lays out top-level lines separated by \\, placing any \tag flush right.
func (l *layouter) lines(rawLines []parsedLine, size, lineWidth float32, display bool) Box {
	if len(rawLines) == 1 {
		clean0, tag0 := extractTags(rawLines[0].nodes)
		if len(tag0) == 0 && len(clean0) == 1 && clean0[0].kind == "matrix" && !clean0[0].hasSup && !clean0[0].hasSub {
			return l.matrixWithWidth(clean0[0], size, lineWidth, display)
		}
	}

	mathBoxes := make([]Box, len(rawLines))
	tagBoxes := make([]Box, len(rawLines))
	gaps := make([]float32, len(rawLines))
	hasTags := make([]bool, len(rawLines))
	anyTag := false
	for i, line := range rawLines {
		gaps[i] = line.gapAfter
		clean, tagNodes := extractTags(line.nodes)
		avail := lineWidth
		if len(tagNodes) > 0 {
			tagBoxes[i] = l.list(tagNodes, size, false)
			hasTags[i] = true
			anyTag = true
			if avail > 0 {
				avail = max(size*3.5, avail-tagBoxes[i].W-size*1.5)
			}
		}
		mathBoxes[i] = l.listWrapped(clean, size, avail, display)
	}
	if !anyTag {
		return l.stack(mathBoxes, gaps, size, alignCenter)
	}

	maxMathW, maxTagW := float32(0), float32(0)
	for i, mb := range mathBoxes {
		maxMathW = max(maxMathW, mb.W)
		if hasTags[i] {
			maxTagW = max(maxTagW, tagBoxes[i].W)
		}
	}
	totalW, blockX := tagLineColumns(maxMathW, maxTagW, size, lineWidth, display)

	out := Box{}
	y := float32(0)
	prevD := float32(0)
	for i, mb := range mathBoxes {
		curA, curD := mb.Ascent, mb.Descent
		if hasTags[i] {
			curA = max(curA, tagBoxes[i].Ascent)
			curD = max(curD, tagBoxes[i].Descent)
		}
		if i > 0 {
			y += max(0, prevD+size*0.3+gaps[i-1]*size+curA)
		}
		out.place(mb, blockX+(maxMathW-mb.W)/2, y)
		if hasTags[i] {
			tb := tagBoxes[i]
			out.place(tb, totalW-tb.W, y)
		}
		prevD = curD
	}
	out.W = totalW
	return out
}

// stack sets lines one above the other, baseline of the box the first's.
func (l *layouter) stack(lines []Box, gaps []float32, size float32, align alignment) Box {
	if len(lines) == 1 {
		return lines[0]
	}
	w := float32(0)
	for _, b := range lines {
		w = max(w, b.W)
	}
	out := Box{}
	y := float32(0)
	for i, b := range lines {
		if i > 0 {
			extra := float32(0)
			if i-1 < len(gaps) {
				extra = gaps[i-1] * size
			}
			y += max(0, lines[i-1].Descent+size*0.3+extra+b.Ascent)
		}
		x := (w - b.W) / 2
		switch align {
		case alignLeft:
			x = 0
		case alignRight:
			x = w - b.W
		}
		out.place(b, x, y)
	}
	out.W = w
	return out
}

// matrixWithWidth lays a matrix, cases or aligned equations out, centered on
// the math axis, in its delimiters, with optional per-row \tag labels.
func (l *layouter) matrixWithWidth(n *node, size, lineWidth float32, display bool) Box {
	rows := make([][]Box, len(n.rows))
	rowTags := make([]Box, len(n.rows))
	hasRowTag := make([]bool, len(n.rows))
	anyTag := false
	cols := 0
	for i, row := range n.rows {
		var rowTagNodes []*node
		for _, cell := range row {
			cleanCell, tagNodes := extractTags(cell)
			if len(tagNodes) > 0 {
				if len(rowTagNodes) > 0 {
					rowTagNodes = append(rowTagNodes, &node{kind: "text", text: " "})
				}
				rowTagNodes = append(rowTagNodes, tagNodes...)
			}
			rows[i] = append(rows[i], l.list(cleanCell, size, display))
		}
		if len(rowTagNodes) > 0 {
			rowTags[i] = l.list(rowTagNodes, size, false)
			hasRowTag[i] = true
			anyTag = true
		}
		cols = max(cols, len(row))
	}
	widths := make([]float32, cols)
	for _, row := range rows {
		for j, b := range row {
			widths[j] = max(widths[j], b.W)
		}
	}
	gap := size
	align := func(j int) alignment { return alignCenter }
	switch n.env {
	case "aligned", "align", "align*", "split", "gathered", "gather", "gather*", "equation", "equation*":
		gap = 0
		align = func(j int) alignment {
			if j%2 == 0 {
				return alignRight
			}
			return alignLeft
		}
		switch n.env {
		case "gathered", "gather", "gather*", "equation", "equation*":
			align = func(int) alignment { return alignCenter }
		}
	case "cases", "lines":
		align = func(int) alignment { return alignLeft }
	}
	gridW := gap * float32(max(0, cols-1))
	for _, w := range widths {
		gridW += w
	}
	totalW, gridX := gridW, float32(0)
	canFlushTags := anyTag && n.env != "pmatrix" && n.env != "bmatrix" && n.env != "Bmatrix" && n.env != "vmatrix" && n.env != "Vmatrix" && n.env != "cases"
	if anyTag {
		maxTagW := float32(0)
		for i, tb := range rowTags {
			if hasRowTag[i] {
				maxTagW = max(maxTagW, tb.W)
			}
		}
		lw := float32(0)
		if canFlushTags {
			lw = lineWidth
		}
		totalW, gridX = tagLineColumns(gridW, maxTagW, size, lw, display)
	}
	out := Box{}
	y := float32(0)
	for i, row := range rows {
		a, d := size*0.7, size*0.25
		for _, b := range row {
			a, d = max(a, b.Ascent), max(d, b.Descent)
		}
		if hasRowTag[i] {
			a = max(a, rowTags[i].Ascent)
			d = max(d, rowTags[i].Descent)
		}
		if i > 0 {
			extra := float32(0)
			if i-1 < len(n.rowGaps) {
				extra = n.rowGaps[i-1] * size
			}
			y += max(0, a+size*0.25+extra)
		}
		x := gridX
		for j, b := range row {
			cx := x
			switch align(j) {
			case alignCenter:
				cx += (widths[j] - b.W) / 2
			case alignRight:
				cx += widths[j] - b.W
			}
			out.place(b, cx, y)
			x += widths[j] + gap
		}
		if hasRowTag[i] {
			tb := rowTags[i]
			out.place(tb, totalW-tb.W, y)
		}
		y += d
	}
	out.W = totalW
	// Center the rows on the math axis.
	shift := (out.Descent-out.Ascent)/2 + size*axisHeight
	centered := Box{}
	centered.place(out, 0, -shift)
	centered.W = totalW
	switch n.env {
	case "pmatrix":
		return l.delimited(centered, "(", ")", size)
	case "bmatrix":
		return l.delimited(centered, "[", "]", size)
	case "Bmatrix":
		return l.delimited(centered, "{", "}", size)
	case "vmatrix":
		return l.delimited(centered, "|", "|", size)
	case "Vmatrix":
		return l.delimited(centered, "‖", "‖", size)
	case "cases":
		return l.delimited(centered, "{", "", size)
	}
	return centered
}
