package mermaid

import (
	"strings"
	"testing"
)

func testPalette() Palette {
	return Palette{
		Text:             "#18181b",
		Caption:          "#71717a",
		Paper:            "#ffffff",
		NodeFill:         "#f4f4f5",
		NodeBorder:       "rgba(37,99,235,0.450)",
		ContainerFill:    "rgba(244,244,245,0.500)",
		ContainerBorder:  "#d9d9de",
		KeywordFill:      "rgba(37,99,235,0.280)",
		KeywordBorder:    "#d9d9de",
		ActorFill:        "rgba(37,99,235,0.080)",
		ActorBorder:      "rgba(37,99,235,0.250)",
		NoteFill:         "rgba(217,119,6,0.080)",
		NoteBorder:       "rgba(217,119,6,0.250)",
		ActivationFill:   "rgba(37,99,235,0.180)",
		ActivationBorder: "rgba(37,99,235,0.680)",
	}
}

func TestExportSVGDrawsFlowchart(t *testing.T) {
	d, err := ParseAndLayout("flowchart LR\n  A[开始] --> B{条件判断}\n  B -- 通过 --> C[执行]", nil)
	if err != nil {
		t.Fatal(err)
	}
	svg := ExportSVG(d, testPalette(), "Diagram")
	for _, want := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg"`,
		`viewBox="0 0 `,
		`role="img"`,
		`aria-label="Diagram"`,
		`font-family="` + FontFamily + `,`,
		`<rect x="0" y="0" width="`,
		`fill="#ffffff"/>`,
		`>开始</text>`,
		`>条件判断</text>`,
		`<path`,
		`</svg>`,
	} {
		if !strings.Contains(svg, want) {
			t.Fatalf("ExportSVG missing %q:\n%s", want, svg)
		}
	}
}

func TestExportSVGOmitsEmptyAlt(t *testing.T) {
	d, err := ParseAndLayout("flowchart TD\n  A[a] --> B[b]", nil)
	if err != nil {
		t.Fatal(err)
	}
	if svg := ExportSVG(d, testPalette(), ""); strings.Contains(svg, "aria-label") {
		t.Fatalf("empty alt should not name the picture:\n%s", svg)
	}
}

func TestExportSVGPaintsDarkPaper(t *testing.T) {
	d, err := ParseAndLayout("flowchart TD\n  A[a] --> B[b]", nil)
	if err != nil {
		t.Fatal(err)
	}
	pal := testPalette()
	pal.Paper = "#1f2023"
	svg := ExportSVG(d, pal, "")
	if !strings.Contains(svg, `fill="#1f2023"/>`) {
		t.Fatalf("paper is not painted:\n%s", svg)
	}
}

func TestExportSVGEscapesLabels(t *testing.T) {
	d, err := ParseAndLayout("flowchart TD\n  A[a & b < c] --> B[ok]", nil)
	if err != nil {
		t.Fatal(err)
	}
	svg := ExportSVG(d, testPalette(), "")
	if !strings.Contains(svg, "a &amp; b &lt; c") {
		t.Fatalf("label is not escaped:\n%s", svg)
	}
	if strings.Contains(svg, "< c") {
		t.Fatalf("label leaked raw markup:\n%s", svg)
	}
	if !strings.Contains(svg, ">ok</text>") {
		t.Fatalf("labels lost:\n%s", svg)
	}
}

func TestExportSVGDrawsSequenceDiagram(t *testing.T) {
	src := "sequenceDiagram\n  Alice->>Bob: Hello\n  Bob-->>Alice: Hi\n  Note right of Bob: Thinking"
	d, err := ParseAndLayout(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	svg := ExportSVG(d, testPalette(), "Sequence")
	for _, want := range []string{"<rect", "<path", ">Alice</text>", ">Hello</text>", ">Thinking</text>"} {
		if !strings.Contains(svg, want) {
			t.Fatalf("sequence SVG missing %q:\n%s", want, svg)
		}
	}
	// Lifelines are dashed like the editor draws them.
	if !strings.Contains(svg, `stroke-dasharray="5 4"`) {
		t.Fatalf("lifelines are not dashed:\n%s", svg)
	}
}

func TestExportSVGMultiLineLabel(t *testing.T) {
	d, err := ParseAndLayout("flowchart TD\n  A[\"one<br/>two\"] --> B[next]", nil)
	if err != nil {
		t.Fatal(err)
	}
	svg := ExportSVG(d, testPalette(), "")
	if !strings.Contains(svg, ">one</text>") || !strings.Contains(svg, ">two</text>") {
		t.Fatalf("multi-line label is not split into lines:\n%s", svg)
	}
}

// Every shape kind has its own SVG branch; draw one of each and look for the
// geometry that makes it distinct.
func TestExportSVGDrawsEveryShape(t *testing.T) {
	shapes := []struct {
		name  string
		shape ShapeOp
		want  string
	}{
		{"rect", ShapeOp{Kind: ShapeRect}, `rx="0.00"`},
		{"round", ShapeOp{Kind: ShapeRound, Radius: 2}, `rx="6.00"`},
		{"stadium", ShapeOp{Kind: ShapeStadium}, `rx="10.00"`},
		{"subroutine", ShapeOp{Kind: ShapeSubroutine}, `M5.60 0.00 V20.00 M34.40 0.00 V20.00`},
		{"cylinder", ShapeOp{Kind: ShapeCylinder}, `C0.00 19.64 11.20 20.00 20.00 20.00`},
		{"circle", ShapeOp{Kind: ShapeCircle}, `<circle cx="20.00" cy="10.00" r="10.00"`},
		{"diamond", ShapeOp{Kind: ShapeDiamond}, `M20.00 0.00 L40.00 10.00`},
		{"hexagon", ShapeOp{Kind: ShapeHexagon}, `L32.80 0.00`},
		{"parallelogram", ShapeOp{Kind: ShapeParallelogram}, `L32.80 20.00`},
		{"pentagon", ShapeOp{Kind: ShapePentagon}, `L40.00 13.00`},
		{"actor", ShapeOp{Kind: ShapeActor}, `<circle cx="20.00" cy="5.40" r="4.40"`},
	}
	for _, tc := range shapes {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.shape
			s.W, s.H = 40, 20
			svg := ExportSVG(Diagram{W: 60, H: 40, Shapes: []ShapeOp{s}}, testPalette(), "")
			if !strings.Contains(svg, tc.want) {
				t.Fatalf("%s shape missing %q:\n%s", tc.name, tc.want, svg)
			}
		})
	}
}

func TestExportSVGDrawsMarkers(t *testing.T) {
	cases := []struct {
		name   string
		marker MarkerKind
		want   string
	}{
		{"arrow", MarkerArrow, `M90.00 50.00 L`},
		{"open arrow", MarkerOpenArrow, `L90.00 50.00`},
		{"circle", MarkerCircle, `<circle cx="86.20" cy="50.00" r="3.80"`},
		{"cross", MarkerCross, `M82.15 47.15`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := PathOp{Points: [][2]float32{{10, 50}, {90, 50}}, EndMarker: tc.marker, Role: RoleEdge}
			svg := ExportSVG(Diagram{W: 100, H: 100, Paths: []PathOp{op}}, testPalette(), "")
			if !strings.Contains(svg, tc.want) {
				t.Fatalf("%s marker missing %q:\n%s", tc.name, tc.want, svg)
			}
		})
	}
}

func TestExportSVGFragmentTint(t *testing.T) {
	s := ShapeOp{
		Kind: ShapeRect, Role: RoleFragment, W: 100, H: 50, Radius: 4,
		Fill: [4]uint8{255, 0, 0, 255},
	}
	svg := ExportSVG(Diagram{W: 120, H: 70, Shapes: []ShapeOp{s}}, testPalette(), "")
	if !strings.Contains(svg, `fill="rgba(255,0,0,0.450)"`) || !strings.Contains(svg, `stroke="rgba(255,0,0,0.550)"`) {
		t.Fatalf("fragment tint is missing:\n%s", svg)
	}
}
