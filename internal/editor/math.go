package editor

import (
	"fmt"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/tex"
	"github.com/egoist/mygo/ui"
)

// Math blocks show their formula, typeset by package tex, and their TeX
// only while the caret is in them, with the formula previewed below.

// mathSize is the size formulas are set at.
const mathSize = 18

// mathFont is the family formulas are set in: a book face, as TeX's.
const mathFont = "Times New Roman, STIX Two Text, Noto Serif, serif"

// mathShape is a math block's formula, laid out for its TeX.
type mathShape struct {
	src string
	box tex.Box
	err error
}

// watchMath notes the math block the caret is in, which shows its TeX.
func (e *Editor) watchMath() {
	e.mathEditing = ""
	if !e.focused || e.printing {
		return
	}
	if leaf := e.Engine.LeafAt(e.Caret); leaf.Block.Kind == "math" {
		e.mathEditing = leaf.Block.ID
	}
}

// glyphs shapes a piece of a formula's text, kept for drawing.
func (e *Editor) glyphs(text string, size float32, italic bool) *ui.TextLayout {
	key := fmt.Sprintf("%s|%g|%v", text, size, italic)
	if l, ok := e.mathGlyphs[key]; ok {
		return l
	}
	if e.mathGlyphs == nil || len(e.mathGlyphs) > 4096 {
		e.mathGlyphs = map[string]*ui.TextLayout{}
	}
	l := ui.ShapeRichText([]ui.Span{{Text: text, Size: size, Italic: italic}}, ui.Font{Family: mathFont, Size: size, Italic: italic}, 0)
	e.mathGlyphs[key] = l
	return l
}

func (e *Editor) measureMath(text string, size float32, italic bool) (w, ascent, descent float32) {
	l := e.glyphs(text, size, italic)
	w, h := l.Size()
	if lines := l.Lines(); len(lines) > 0 {
		return w, lines[0].Ascent, lines[0].Descent
	}
	return w, h * .8, h * .2
}

// formula lays out the formula of a math block, kept while its TeX is
// unchanged.
func (e *Editor) formula(id, src string) mathShape {
	if s, ok := e.maths[id]; ok && s.src == src {
		return s
	}
	if e.maths == nil {
		e.maths = map[string]mathShape{}
	}
	box, err := tex.Layout(src, mathSize, e.measureMath)
	s := mathShape{src: src, box: box, err: err}
	e.maths[id] = s
	return s
}

// mathRow lays a math block out beyond its TeX: the formula in place of
// it, or below it while the caret is in it. It returns the block's height.
func (e *Editor) mathRow(r *row, total, textH, top, bottom float32) float32 {
	b := r.leaf.Block
	src := strings.TrimSpace(b.Text())
	if src == "" {
		return total
	}
	shape := e.formula(b.ID, src)
	if shape.err != nil {
		r.mathErr = shape.err.Error()
		return total + 22
	}
	box := shape.box
	r.math = &box
	h := box.Ascent + box.Descent
	if e.mathEditing == b.ID {
		r.mathTop = r.y + textH + 14
		return total + h + 14
	}
	r.mathHidden = true
	r.mathTop = r.rect.Y + top
	return top + h + bottom
}

// paintMath draws a math block's formula, and reports whether it stands
// for the block's TeX, which then is not drawn.
func (e *Editor) paintMath(p *ui.Painter, bounds ui.Rect, r row, t *ui.Theme) bool {
	if r.mathErr != "" {
		_, h := r.layout.Size()
		p.Text(bounds.X+r.x, bounds.Y+r.y+h+4, "公式有误："+strings.TrimPrefix(r.mathErr, "tex: "), 12, t.Danger)
		return false
	}
	if r.math == nil {
		return false
	}
	box := r.math
	x := bounds.X + r.rect.X + (r.rect.W-box.W)/2
	y := bounds.Y + r.mathTop + box.Ascent
	if !r.mathHidden {
		// The preview below the TeX, after a hairline.
		p.Line(bounds.X+r.x, bounds.Y+r.mathTop-7, bounds.X+r.rect.X+r.rect.W-20, bounds.Y+r.mathTop-7, 1, e.page.TableLine)
	}
	for _, op := range box.Ops {
		switch op.Kind {
		case "text":
			l := e.glyphs(op.Text, op.Size, op.Italic)
			baseline := float32(0)
			if lines := l.Lines(); len(lines) > 0 {
				baseline = lines[0].Baseline
			}
			color := t.Text
			if op.Error {
				color = t.Danger
			}
			p.TextLayout(l, x+op.X, y+op.Y-baseline, color)
		case "rule":
			p.Fill(ui.Rect{X: x + op.X, Y: y + op.Y, W: op.W, H: op.H}, t.Text, 0)
		case "path":
			pts := make([][2]float32, len(op.Points))
			for i, pt := range op.Points {
				pts[i] = [2]float32{x + pt[0], y + pt[1]}
			}
			p.StrokePolyline(pts, op.Width, t.Text)
		}
	}
	return r.mathHidden
}
