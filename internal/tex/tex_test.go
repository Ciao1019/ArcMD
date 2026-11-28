package tex

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// fixed measures every rune half its size wide, reaching 0.8 of it up and
// 0.2 down, as a monospace font would.
func fixed(s string, size float32, italic, bold bool) (float32, float32, float32) {
	return float32(utf8.RuneCountInString(s)) * size * 0.5, size * 0.8, size * 0.2
}

func texts(b Box) []string {
	var out []string
	for _, op := range b.Ops {
		if op.Kind == "text" {
			out = append(out, op.Text)
		}
	}
	return out
}

func kinds(b Box, kind string) int {
	n := 0
	for _, op := range b.Ops {
		if op.Kind == kind {
			n++
		}
	}
	return n
}

func layout(t *testing.T, src string) Box {
	t.Helper()
	b, err := Layout(src, 20, fixed)
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	if b.W <= 0 || b.Ascent <= 0 {
		t.Fatalf("%s: empty box %+v", src, b)
	}
	return b
}

func TestSymbolsAndScripts(t *testing.T) {
	b := layout(t, `E = mc^2 + \alpha_i^{n} - \infty`)
	got := strings.Join(texts(b), " ")
	if got != "E = m c 2 + α n i − ∞" {
		t.Errorf("texts %q", got)
	}
	var sup, base Op
	for _, op := range b.Ops {
		switch op.Text {
		case "2":
			sup = op
		case "c":
			base = op
		}
	}
	if sup.Size >= base.Size || sup.Y >= base.Y || sup.X <= base.X {
		t.Errorf("superscript %+v beside %+v", sup, base)
	}
	// Variables are italic, numbers and operators not.
	for _, op := range b.Ops {
		if want := op.Text == "E" || op.Text == "m" || op.Text == "c" || op.Text == "α" || op.Text == "n" || op.Text == "i"; op.Italic != want {
			t.Errorf("%q italic %v", op.Text, op.Italic)
		}
	}
}

func TestStructures(t *testing.T) {
	frac := layout(t, `\frac{a+b}{2}`)
	if kinds(frac, "rule") != 1 {
		t.Error("fraction without its bar")
	}
	var num, den Op
	for _, op := range frac.Ops {
		if op.Text == "a" {
			num = op
		}
		if op.Text == "2" {
			den = op
		}
	}
	if num.Y >= den.Y {
		t.Errorf("numerator %v not above denominator %v", num.Y, den.Y)
	}
	if kinds(layout(t, `\sqrt[3]{x}`), "path") != 1 {
		t.Error("root without its sign")
	}
	sum := layout(t, `\sum_{i=1}^{n} i`)
	var op, lower Op
	for _, o := range sum.Ops {
		if o.Text == "∑" {
			op = o
		}
		if o.Text == "1" {
			lower = o
		}
	}
	if op.Size <= 20 || lower.Y <= op.Y {
		t.Errorf("sum %+v, lower limit %+v", op, lower)
	}
	m := layout(t, `\begin{pmatrix} 1 & 2 \\ 3 & 4 \end{pmatrix}`)
	if strings.Join(texts(m), "") != "1234" || kinds(m, "path") != 2 {
		t.Errorf("matrix %q, %d paths", texts(m), kinds(m, "path"))
	}
	cases := layout(t, `f(x) = \begin{cases} 1 & x > 0 \\ 0 & \text{otherwise} \end{cases}`)
	if !strings.Contains(strings.Join(texts(cases), ""), "otherwise") {
		t.Errorf("cases %q", texts(cases))
	}
	left := layout(t, `\left( \frac{1}{2} \right)`)
	if kinds(left, "path") != 2 {
		t.Errorf("tall delimiters drawn as %d paths", kinds(left, "path"))
	}
	lines := layout(t, `a = b \\ c = d`)
	if lines.Descent < 20 {
		t.Errorf("second line not below: %+v", lines)
	}
	layout(t, `\lim_{x \to 0} \frac{\sin x}{x} = 1, \hat{x}, \vec{v}, \overline{AB}, \mathbb{R}, \binom{n}{k}, \int_0^1 x\,dx`)
}

func TestBoxed(t *testing.T) {
	const src = `\frac{x}{2}`
	const inset = 7
	inner := layout(t, src)
	framed, err := LayoutInline(`\boxed{`+src+`}`, 20, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if framed.W != inner.W+2*inset || framed.Ascent != inner.Ascent+inset || framed.Descent != inner.Descent+inset {
		t.Fatalf("frame does not enclose the display-style fraction with padding: %+v", framed)
	}
	if len(framed.Ops) != len(inner.Ops)+1 || kinds(framed, "rule") != 1 {
		t.Fatalf("expected a frame and a fraction bar, got %+v", framed.Ops)
	}
	if outline := framed.Ops[len(inner.Ops)]; outline.Kind != "frame" || outline.X != 0 || outline.Y != -framed.Ascent || outline.W != framed.W || outline.H != framed.Ascent+framed.Descent || outline.Width != 1 {
		t.Fatalf("frame does not match the box bounds: %+v", outline)
	}
	for index, op := range inner.Ops {
		got := framed.Ops[index]
		if got.Kind != op.Kind || got.Text != op.Text || got.X != op.X+inset || got.Y != op.Y || got.Size != op.Size || got.Error {
			t.Fatalf("boxed content moved off its baseline or changed style: got %+v, inner %+v", got, op)
		}
	}
	nested := layout(t, `\boxed{\boxed{x}}^2`)
	if kinds(nested, "frame") != 2 || strings.Join(texts(nested), "") != "x2" {
		t.Fatalf("expected two frames and a superscript, got %+v", nested.Ops)
	}
	if got := nested.Ops[len(nested.Ops)-1]; got.X < layout(t, `\boxed{\boxed{x}}`).W || got.Y >= 0 {
		t.Fatalf("superscript is not placed above and outside the frame: %+v", got)
	}
}

func TestErrors(t *testing.T) {
	for _, src := range []string{`\frac{a}`, `\boxed`, `\boxed{x`, `{a`, `a}`, `\left( x`, `\begin{matrix} a`, `x^`, `\sqrt[2`} {
		if _, err := Layout(src, 20, fixed); err == nil {
			t.Errorf("%q laid out", src)
		}
	}
	b := layout(t, `\unknown + 1`)
	if b.Ops[0].Text != `\unknown` || !b.Ops[0].Error {
		t.Errorf("unknown command %+v", b.Ops[0])
	}
}

func TestMaxNestingDepthGuard(t *testing.T) {
	// Reasonable nested \boxed expressions continue to lay out cleanly.
	okSrc := strings.Repeat(`\boxed{`, 8) + "x" + strings.Repeat("}", 8)
	if box, err := Layout(okSrc, 20, fixed); err != nil || kinds(box, "frame") != 8 {
		t.Fatalf("8-deep boxed failed: err=%v frames=%d", err, kinds(box, "frame"))
	}

	// Pathological nesting must fail fast without stack overflow or O(N^2) op blowup.
	cases := []string{
		strings.Repeat(`\boxed{`, 200) + "x" + strings.Repeat("}", 200),
		strings.Repeat(`\boxed`, 200) + " x",
		strings.Repeat("{", 200) + "x" + strings.Repeat("}", 200),
		strings.Repeat(`\left(`, 120) + "x" + strings.Repeat(`\right)`, 120),
		strings.Repeat(`\frac{1}{`, 120) + "x" + strings.Repeat("}", 120),
	}
	for _, src := range cases {
		if _, err := Layout(src, 20, fixed); err == nil || !strings.Contains(err.Error(), "nested too deeply") {
			t.Fatalf("expected nesting depth error, got err=%v", err)
		}
	}
}

func TestIntegralLimitsClearUpperHook(t *testing.T) {
	b := layout(t, `\int_0^\infty x^2 e^{-x} dx = 2`)
	var upper, lower Op
	for _, op := range b.Ops {
		if op.Error {
			t.Fatalf("unrecognized token %q", op.Text)
		}
		if op.Text == "∞" {
			upper = op
		}
		if op.Text == "0" {
			lower = op
		}
	}
	if upper.X <= lower.X || upper.Y >= lower.Y {
		t.Fatal("integral limits do not follow its slant")
	}
}

func TestEquationTags(t *testing.T) {
	// 1. \tag{1.2a} wraps in upright parentheses and centers formula in LayoutWidth.
	b, err := LayoutWidth(`E = mc^2 \tag{1.2a}`, 20, 600, fixed)
	if err != nil {
		t.Fatalf("LayoutWidth tag failed: %v", err)
	}
	if b.W != 600 {
		t.Fatalf("expected full line width 600, got %v", b.W)
	}
	for _, op := range b.Ops {
		if op.Error {
			t.Fatalf("unexpected error token in tagged formula: %+v", op)
		}
	}
	got := strings.Join(texts(b), "")
	if got != "E=mc2(1.2a)" {
		t.Fatalf("tagged texts = %q, want %q", got, "E=mc2(1.2a)")
	}
	var eOp, closeParen Op
	for _, op := range b.Ops {
		switch op.Text {
		case "E":
			eOp = op
		case ")":
			closeParen = op
		case "1.2a":
			if op.Italic {
				t.Fatalf("tag label %q should be upright, got Italic=true", op.Text)
			}
		}
	}
	// Formula centered around 300, closing paren flush right at 600.
	if eOp.X < 200 || eOp.X > 300 {
		t.Fatalf("expected centered formula start around ~250, got E.X=%v", eOp.X)
	}
	if closeParen.X < 585 {
		t.Fatalf("expected flush-right tag near 600, got ').X=%v", closeParen.X)
	}

	// 2. \tag*{[A-1]} omits outer parentheses and preserves spaces/hyphens.
	starred := layout(t, `a + b = c \tag*{[eq. A-1]}`)
	if s := strings.Join(texts(starred), ""); s != "a+b=c[eq. A-1]" {
		t.Fatalf("starred tag = %q, want %q", s, "a+b=c[eq. A-1]")
	}

	// 3. Multi-row align with per-row \tag and \notag.
	aligned, err := LayoutWidth(`\begin{align} x + y &= 1 \tag{1} \\ x - y &= 3 \notag \\ 2x &= 4 \tag{2} \end{align}`, 20, 600, fixed)
	if err != nil {
		t.Fatalf("aligned tags failed: %v", err)
	}
	var tag1Y, tag2Y float32
	var saw1, saw2 bool
	for _, op := range aligned.Ops {
		if op.Error {
			t.Fatalf("unexpected error in aligned tags: %+v", op)
		}
		if op.Text == "1" && op.X > 500 {
			saw1, tag1Y = true, op.Y
		}
		if op.Text == "2" && op.X > 500 {
			saw2, tag2Y = true, op.Y
		}
	}
	if !saw1 || !saw2 || tag2Y <= tag1Y+20 {
		t.Fatalf("expected per-row right-aligned tags (1) above (2): saw1=%v y1=%v saw2=%v y2=%v", saw1, tag1Y, saw2, tag2Y)
	}

	// 4. Malformed \tag reports parse error.
	for _, bad := range []string{`x = 1 \tag`, `x = 1 \tag{1`, `x = 1 \tag*`} {
		if _, err := Layout(bad, 20, fixed); err == nil {
			t.Errorf("expected parse error for %q", bad)
		}
	}
}

func TestLongEquationWrapsBeforeShrinking(t *testing.T) {
	const eq = `y_3^{(3)} = -0.2y_3 - 0.6\dot{y}_3 - 0.9\ddot{y}_3 + a_3 u_3`
	single, err := LayoutInline(eq, 16, fixed)
	if err != nil {
		t.Fatal(err)
	}
	narrowW := single.W * 0.62
	wrapped, err := LayoutInlineWidth(eq, 16, narrowW, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if wrapped.W > narrowW+0.5 {
		t.Fatalf("wrapped width %v exceeds target %v (single=%v)", wrapped.W, narrowW, single.W)
	}
	if wrapped.Descent <= single.Descent+10 {
		t.Fatalf("expected multi-line descent > %v, got %v", single.Descent, wrapped.Descent)
	}
	// Plenty of width leaves the equation on one line unchanged.
	wide, err := LayoutInlineWidth(eq, 16, single.W+20, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if wide.W != single.W || wide.Descent != single.Descent {
		t.Fatalf("wide LayoutInlineWidth changed single-line geometry: got %+v, want %+v", wide, single)
	}
}

func TestLineBreakSpacing(t *testing.T) {
	// 1. \\[8pt], \\[0.5em], \\*[6pt], \\[-2pt] do not render dimension text and adjust row spacing.
	for _, tc := range []struct {
		name string
		base string
		gap  string
	}{
		{"cases", `\begin{cases} 1 & x > 0 \\ 0 & x \le 0 \end{cases}`, `\begin{cases} 1 & x > 0 \\[8pt] 0 & x \le 0 \end{cases}`},
		{"aligned", `\begin{aligned} a &= b \\ c &= d \end{aligned}`, `\begin{aligned} a &= b \\[0.5em] c &= d \end{aligned}`},
		{"pmatrix", `\begin{pmatrix} 1 & 2 \\ 3 & 4 \end{pmatrix}`, `\begin{pmatrix} 1 & 2 \\*[6pt] 3 & 4 \end{pmatrix}`},
		{"lines", `a = b \\ c = d`, `a = b \\[8pt] c = d`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b0 := layout(t, tc.base)
			b1 := layout(t, tc.gap)
			joined := strings.Join(texts(b1), "")
			if strings.Contains(joined, "8pt") || strings.Contains(joined, "0.5em") || strings.Contains(joined, "6pt") || strings.Contains(joined, "[") || strings.Contains(joined, "]") {
				t.Fatalf("dimension parameter leaked into text %q", joined)
			}
			h0 := b0.Ascent + b0.Descent
			h1 := b1.Ascent + b1.Descent
			if h1 <= h0+5 {
				t.Fatalf("expected extra row spacing to increase height: base=%v gap=%v", h0, h1)
			}
		})
	}

	// Negative dimension shrinks spacing without leaking text or inverting rows.
	neg := layout(t, `a = b \\[-2pt] c = d`)
	if got := strings.Join(texts(neg), ""); got != "a=bc=d" {
		t.Fatalf("negative dimen leaked text: %q", got)
	}

	// Non-dimension bracket after \\ must be preserved on the next line.
	interval := layout(t, `a \\ [b, c]`)
	if got := strings.Join(texts(interval), ""); got != "a[b,c]" {
		t.Fatalf("non-dimension bracket after \\\\ lost: %q", got)
	}
}

func TestStandaloneVertDelimiters(t *testing.T) {
	for _, src := range []string{
		`\dfrac{\lvert A \rvert}{\lambda}`,
		`\lVert x \rVert`,
		`\vert x \vert`,
		`\Vert x \Vert`,
		`\left\lVert x \right\rVert`,
	} {
		b := layout(t, src)
		for _, op := range b.Ops {
			if op.Error {
				t.Fatalf("%s had error op: %+v", src, op)
			}
		}
	}

	// \lvert A \rvert emits single vertical bars; \lVert x \rVert emits double bars.
	if got := strings.Join(texts(layout(t, `\lvert A \rvert`)), ""); got != "|A|" {
		t.Fatalf("\\lvert A \\rvert texts = %q, want %q", got, "|A|")
	}
	if got := strings.Join(texts(layout(t, `\lVert x \rVert`)), ""); got != "‖x‖" {
		t.Fatalf("\\lVert x \\rVert texts = %q, want %q", got, "‖x‖")
	}

	// Inside \lvert -x \rvert, '-' acts as unary sign (tighter than binary operator).
	unary := layout(t, `\lvert -x \rvert`)
	binary := layout(t, `a - x`)
	var ux, ul float32
	for _, op := range unary.Ops {
		if op.Text == "−" {
			ux = op.X
		}
		if op.Text == "|" && ul == 0 {
			ul = op.X
		}
	}
	var bx, ba float32
	for _, op := range binary.Ops {
		if op.Text == "−" {
			bx = op.X
		}
		if op.Text == "a" {
			ba = op.X
		}
	}
	if ux-ul >= bx-ba {
		t.Fatalf("expected unary minus after \\lvert to have less leading space than binary minus: unary=%v binary=%v", ux-ul, bx-ba)
	}
}

func TestNegatedSymbolsAndExtensibleArrows(t *testing.T) {
	for _, src := range []string{
		`A \nRightarrow B`,
		`A \not\Rightarrow B`,
		`a \not= b`,
		`x \not\in S`,
		`A \not\subset B`,
		`p \not\mid q`,
		`a \not\approx b`,
		`\not\alpha`,
	} {
		b := layout(t, src)
		for _, op := range b.Ops {
			if op.Error {
				t.Fatalf("%s had error op: %+v", src, op)
			}
		}
	}

	if got := strings.Join(texts(layout(t, `A \nRightarrow B`)), ""); got != "A⇏B" {
		t.Fatalf("\\nRightarrow = %q, want %q", got, "A⇏B")
	}
	if got := strings.Join(texts(layout(t, `A \not\Rightarrow B`)), ""); got != "A⇏B" {
		t.Fatalf("\\not\\Rightarrow = %q, want %q", got, "A⇏B")
	}
	if kinds(layout(t, `\not\alpha`), "path") != 1 {
		t.Fatal("expected fallback slash path for \\not\\alpha")
	}

	// Extensible arrows: above, below, empty, bidirectional.
	short := layout(t, `A \xrightarrow{} B`)
	long := layout(t, `A \xrightarrow{r_1 \leftrightarrow r_2} B`)
	if kinds(short, "path") < 2 || kinds(long, "path") < 2 {
		t.Fatalf("expected shaft + head paths for xrightarrow, got short=%d long=%d", kinds(short, "path"), kinds(long, "path"))
	}
	if long.W <= short.W {
		t.Fatalf("expected labeled xrightarrow to stretch wider: short=%v long=%v", short.W, long.W)
	}
	for _, op := range long.Ops {
		if op.Error {
			t.Fatalf("xrightarrow error op: %+v", op)
		}
	}

	both := layout(t, `A \xleftarrow[\text{below}]{\text{above}} B`)
	if kinds(both, "path") < 2 {
		t.Fatalf("expected shaft + head paths for xleftarrow, got %d", kinds(both, "path"))
	}
	var aboveY, belowY, shaftY float32
	for _, op := range both.Ops {
		if op.Error {
			t.Fatalf("xleftarrow error op: %+v", op)
		}
		if op.Text == "above" {
			aboveY = op.Y
		}
		if op.Text == "below" {
			belowY = op.Y
		}
		if op.Kind == "path" && len(op.Points) == 2 {
			shaftY = op.Points[0][1]
		}
	}
	if aboveY >= shaftY || belowY <= shaftY {
		t.Fatalf("expected aboveY (%v) < shaftY (%v) < belowY (%v)", aboveY, shaftY, belowY)
	}

	bi := layout(t, `X \xleftrightarrow[a]{b} Y`)
	if kinds(bi, "path") < 3 {
		t.Fatalf("expected shaft + 2 heads for xleftrightarrow, got %d paths", kinds(bi, "path"))
	}
}
