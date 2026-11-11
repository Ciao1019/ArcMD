package editor

import (
	"fmt"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/tex"
	"github.com/egoist/mygo/ui"
)

// Math follows Textus: an unboxed formula at rest, a centered source panel
// while editing, and a separate live preview with room below that panel.
const (
	mathSize     = 16 * 1.21 // KaTeX's size relative to the document body.
	mathMargin   = 18
	mathPaddingX = 12
	mathPaddingY = 6
	mathFenceH   = 20
	mathPreviewY = 16
	mathPreviewW = 420
)

// mathFont is the family formulas are set in: a book face, as TeX's.
const mathFont = "KaTeX_Main, Times New Roman, STIX Two Text, Noto Serif, serif"

// mathShape is a math block's formula, laid out for its TeX.
type mathShape struct {
	src string
	box tex.Box
	err error
}

// watchMath notes the math block the caret is in, which shows its TeX.
func (e *Editor) watchMath() {
	e.mathEditing = ""
	if !e.focused || e.printing || e.ReadOnly {
		return
	}
	if leaf := e.Engine.LeafAt(e.Caret); leaf.Block.Kind == "math" && e.Anchor >= leaf.Start && e.Anchor <= leaf.End {
		e.mathEditing = leaf.Block.ID
	}
}

// glyphs shapes a piece of a formula's text, kept for drawing.
func (e *Editor) glyphs(text string, size float32, italic bool) *ui.TextLayout {
	// A registration failure still leaves the system serif fallbacks usable.
	_ = loadMathFonts()
	key := fmt.Sprintf("%s|%g|%v", text, size, italic)
	if l, ok := e.mathGlyphs[key]; ok {
		return l
	}
	if e.mathGlyphs == nil || len(e.mathGlyphs) > 4096 {
		e.mathGlyphs = map[string]*ui.TextLayout{}
	}
	family := mathFont
	if italic {
		family = "KaTeX_Math, " + family
	}
	l := ui.ShapeRichText([]ui.Span{{Text: text, Size: size, Italic: italic}}, ui.Font{Family: family, Size: size, Italic: italic}, 0)
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

// mathRow reserves a preview's room so it never covers the following block.
// All formula geometry stays relative to the row, including narrow columns.
func (e *Editor) mathRow(r *row, textH float32) float32 {
	b := r.leaf.Block
	src := strings.TrimSpace(b.Text())
	r.mathScale = 1
	r.mathHidden = e.mathEditing != b.ID
	r.mathTop = mathMargin
	r.y = r.rect.Y + mathMargin
	if !r.mathHidden {
		r.mathPanel = textH + 2*mathPaddingY + 2*mathFenceH
		r.y += mathPaddingY + mathFenceH
		r.mathTop += r.mathPanel + 10 + mathPreviewY
	}
	if src == "" {
		return 2*mathMargin + max(26, r.mathPanel)
	}
	shape := e.formula(b.ID, src)
	if shape.err != nil {
		r.mathErr = "公式有误：" + strings.TrimPrefix(shape.err.Error(), "tex: ")
		r.mathError = ui.ShapeText(r.mathErr, ui.Font{Family: "system-ui", Size: 12}, max(24, r.rect.W-2*mathPaddingX))
		_, errorH := r.mathError.Size()
		if r.mathHidden {
			r.mathHidden = false // Malformed TeX always remains editable and visible.
			r.mathTop = mathMargin + textH + 8
		}
		return r.mathTop + errorH + mathMargin
	}
	box := shape.box
	r.math = &box
	width := r.rect.W
	if !r.mathHidden {
		width = min(width, mathPreviewW) - 2*mathPaddingX
	}
	if box.W > width {
		r.mathScale = max(1, width) / box.W
	}
	h := (box.Ascent + box.Descent) * r.mathScale
	if !r.mathHidden {
		return r.mathTop + h + mathPreviewY + mathMargin
	}
	return 2*mathMargin + max(26, h)
}

// paintMath draws a math block's formula, and reports whether it stands
// for the block's TeX, which then is not drawn.
func (e *Editor) paintMath(p *ui.Painter, bounds ui.Rect, r row, t *ui.Theme) bool {
	if r.leaf.Block.Kind != "math" {
		return false
	}
	x, y := bounds.X+r.rect.X, bounds.Y+r.rect.Y
	if r.mathPanel > 0 {
		panel := ui.Rect{X: x, Y: y + mathMargin, W: r.rect.W, H: r.mathPanel}
		p.Stroke(panel, e.page.MathBorder, 8, 1)
		fence := ui.ShapeText("$$", ui.Font{Family: "monospace", Size: 14.4}, 0)
		fw, fh := fence.Size()
		p.TextLayout(fence, x+(r.rect.W-fw)/2, panel.Y+mathPaddingY+(mathFenceH-fh)/2, e.page.Caption)
		p.TextLayout(fence, x+(r.rect.W-fw)/2, panel.Y+panel.H-mathPaddingY-mathFenceH+(mathFenceH-fh)/2, e.page.Caption)
	}
	if r.mathError != nil {
		ew, _ := r.mathError.Size()
		p.TextLayout(r.mathError, x+(r.rect.W-ew)/2, y+r.mathTop, t.Danger)
		return false
	}
	if r.math == nil {
		if !e.printing {
			hint := ui.ShapeText("输入 LaTeX 公式", ui.Font{Family: "system-ui", Size: 14}, 0)
			hw, hh := hint.Size()
			p.TextLayout(hint, x+(r.rect.W-hw)/2, bounds.Y+r.y+(22-hh)/2, e.page.Placeholder)
		}
		return r.mathHidden
	}
	box, scale := r.math, r.mathScale
	w, h := box.W*scale, (box.Ascent+box.Descent)*scale
	if !r.mathHidden {
		previewW := min(r.rect.W, w+2*mathPaddingX)
		preview := ui.Rect{X: x + (r.rect.W-previewW)/2, Y: y + r.mathTop - mathPreviewY, W: previewW, H: h + 2*mathPreviewY}
		p.Shadow(preview, 12, 0, 4, 16, 0, e.page.MathShadow)
		p.Fill(preview, e.page.Popover, 12)
		p.Stroke(preview, e.page.MathBorder, 12, 1)
	}
	x += (r.rect.W - w) / 2
	y += r.mathTop
	p.Transform(ui.Scaling(scale, scale), x, y, func() {
		e.paintFormula(p, *box, x, y+box.Ascent, t)
	})
	return r.mathHidden
}

func (e *Editor) paintFormula(p *ui.Painter, box tex.Box, x, y float32, t *ui.Theme) {
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
}
