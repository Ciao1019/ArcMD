package tex

import (
	"math"
	"strings"
	"testing"
)

func TestBoldsymbolParsesMath(t *testing.T) {
	for _, tc := range []struct {
		source, want string
	}{
		{`\boldsymbol{Z}`, "Z"},
		{`\boldsymbol{\mu}`, "μ"},
		{`\boldsymbol\mu`, "μ"},
		{`\boldsymbol{x_i}+y`, "xi+y"},
		{`\boldsymbol{\frac{x}{\mu}}`, "xμ"},
		{`\boldsymbol{\begin{pmatrix}x\\\mu\end{pmatrix}}`, "xμ"},
		{`\boldsymbol{\boldsymbol{x}+y}+z`, "x+y+z"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			for _, render := range []func(string, float32, Measure) (Box, error){Layout, LayoutInline} {
				b, err := render(tc.source, 20, fixed)
				if err != nil {
					t.Fatal(err)
				}
				for _, op := range b.Ops {
					if op.Error {
						t.Fatalf("unsupported token %q", op.Text)
					}
				}
				if got := strings.Join(texts(b), ""); got != tc.want {
					t.Fatalf("rendered %q, want %q", got, tc.want)
				}
			}
		})
	}
}

func TestBoldsymbolStyleScope(t *testing.T) {
	for _, tc := range []struct {
		source string
		bold   []bool
		italic []bool
	}{
		{`\boldsymbol{x}_i+y`, []bool{true, false, false, false}, []bool{true, true, false, true}},
		{`\boldsymbol{x_i}+y`, []bool{true, true, false, false}, []bool{true, true, false, true}},
		{`\boldsymbol{\mu+1}+\Sigma`, []bool{true, true, true, false, false}, []bool{true, false, false, false, false}},
		{`\boldsymbol{\frac{x}{y}}+z`, []bool{true, true, false, false}, []bool{true, true, false, true}},
		{`\boldsymbol{\begin{pmatrix}x\\\mu\end{pmatrix}}+z`, []bool{true, true, false, false}, []bool{true, true, false, true}},
		{`\boldsymbol{\boldsymbol{x}+y}+z`, []bool{true, true, true, false, false}, []bool{true, false, true, false, true}},
	} {
		t.Run(tc.source, func(t *testing.T) {
			b := layout(t, tc.source)
			i := 0
			for _, op := range b.Ops {
				if op.Kind != "text" {
					continue
				}
				if i >= len(tc.bold) || op.Error || op.Bold != tc.bold[i] || op.Italic != tc.italic[i] {
					t.Fatalf("unexpected style at text %d: %+v", i, op)
				}
				i++
			}
			if i != len(tc.bold) {
				t.Fatalf("got %d text operations, want %d", i, len(tc.bold))
			}
		})
	}
}

func TestBoldsymbolMeasuresBeforePlacement(t *testing.T) {
	measure := func(s string, size float32, italic, bold bool) (float32, float32, float32) {
		w, a, d := fixed(s, size, italic, bold)
		if bold {
			w *= 2
		}
		return w, a, d
	}
	b, err := Layout(`\boldsymbol{x}y`, 20, measure)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Ops) != 2 || math.Abs(float64(b.Ops[1].X-20.8)) > .001 || math.Abs(float64(b.W-31.6)) > .001 {
		t.Fatalf("placement did not use bold advance: %+v", b)
	}
}

func TestBoldsymbolPreservesOperatorSpacing(t *testing.T) {
	for _, op := range []string{"+", "="} {
		plain := layout(t, "a"+op+"b")
		bold := layout(t, `a\boldsymbol{`+op+`}b`)
		if plain.W != bold.W || plain.Ops[1].X != bold.Ops[1].X || plain.Ops[2].X != bold.Ops[2].X {
			t.Fatalf("spacing changed around %s: plain %+v, bold %+v", op, plain, bold)
		}
	}
}

func TestBoldsymbolRequiresArgument(t *testing.T) {
	for _, source := range []string{`\boldsymbol`, `\boldsymbol{x`, `\boldsymbol^2`} {
		if _, err := Layout(source, 20, fixed); err == nil {
			t.Errorf("accepted malformed formula %q", source)
		}
	}
}
