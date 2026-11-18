package whiteboard

import (
	"encoding/base64"
	"fmt"
	"html"
	"strings"
)

const (
	svgPayloadStart = "<!-- payload-start -->"
	svgPayloadEnd   = "<!-- payload-end -->"
)

// Palette resolves semantic whiteboard colors into SVG CSS colors.
type Palette struct {
	Background string
	NoteFill   string
	NoteBorder string
	FrameLabel string
	Strokes    map[string]string
	Fills      map[string]string
}

// DefaultLightPalette returns Excalidraw's crisp paper palette.
func DefaultLightPalette() Palette {
	return Palette{
		Background: "#FFFFFF",
		NoteFill:   "#FFF9DB",
		NoteBorder: "#F08C00",
		FrameLabel: "#6B7280",
		Strokes: map[string]string{
			"ink":    "#1E1E1E",
			"gray":   "#6B7280",
			"red":    "#E03131",
			"orange": "#F08C00",
			"green":  "#2F9E44",
			"blue":   "#1971C2",
			"purple": "#6741D9",
		},
		Fills: map[string]string{
			"ink":    "#E9ECEF",
			"gray":   "#E5E7EB",
			"red":    "#FFC9C9",
			"orange": "#FFEC99",
			"green":  "#B2F2BB",
			"blue":   "#A5D8FF",
			"purple": "#D0BFFF",
		},
	}
}

// DefaultDarkPalette returns the Feishu-dark chalk whiteboard palette.
func DefaultDarkPalette() Palette {
	return Palette{
		Background: "#1F2023",
		NoteFill:   "#352B1D",
		NoteBorder: "#F59F00",
		FrameLabel: "#9CA3AF",
		Strokes: map[string]string{
			"ink":    "#E3E4E7",
			"gray":   "#9CA3AF",
			"red":    "#FF8787",
			"orange": "#FFC078",
			"green":  "#69DB7C",
			"blue":   "#74C0FC",
			"purple": "#B197FC",
		},
		Fills: map[string]string{
			"ink":    "#34363C",
			"gray":   "#3F4148",
			"red":    "#592B2B",
			"orange": "#523B1E",
			"green":  "#22462E",
			"blue":   "#213B58",
			"purple": "#3B2D59",
		},
	}
}

func (p Palette) resolveStroke(name string) string {
	if c, ok := p.Strokes[name]; ok {
		return c
	}
	if strings.HasPrefix(name, "#") {
		return name
	}
	if c, ok := p.Strokes["ink"]; ok {
		return c
	}
	return "#1E1E1E"
}

func (p Palette) resolveFill(name string) string {
	if c, ok := p.Fills[name]; ok {
		return c
	}
	if strings.HasPrefix(name, "#") {
		return name
	}
	return "none"
}

// ExportSVG renders scene into a standalone, self-contained SVG document.
func ExportSVG(scene *Scene, pal Palette) string {
	return renderSVG(scene, pal, "")
}

// ExportEmbeddedSVG renders scene into a valid standalone SVG image that also
// embeds the lossless Excalidraw v2 JSON payload inside <metadata> so external
// Markdown viewers (Typora, GitHub, browsers) render the vector drawing while
// ArcMD can reopen and edit it interactively.
func ExportEmbeddedSVG(scene *Scene, pal Palette) (string, error) {
	rawJSON, err := scene.Encode()
	if err != nil {
		return "", err
	}
	b64 := base64.StdEncoding.EncodeToString([]byte(rawJSON))
	meta := "  <metadata>\n" +
		"    <!-- payload-type:application/vnd.excalidraw+json -->\n" +
		"    <!-- payload-version:2 -->\n" +
		"    " + svgPayloadStart + b64 + svgPayloadEnd + "\n" +
		"  </metadata>\n"
	return renderSVG(scene, pal, meta), nil
}

func renderSVG(scene *Scene, pal Palette, metadata string) string {
	if len(pal.Strokes) == 0 {
		pal = DefaultLightPalette()
	}
	b := SceneBounds(scene)
	pad := float32(28)
	if b.Empty() {
		b = Box{0, 0, 320, 180}
	}
	vx, vy, vw, vh := b.X-pad, b.Y-pad, b.W+2*pad, b.H+2*pad

	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="%.2f %.2f %.2f %.2f" width="%.0f" height="%.0f">`,
		vx, vy, vw, vh, vw, vh)
	sb.WriteByte('\n')
	if metadata != "" {
		sb.WriteString(metadata)
	}
	fmt.Fprintf(&sb, `  <rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" fill="%s"/>`,
		vx, vy, vw, vh, html.EscapeString(pal.Background))
	sb.WriteByte('\n')

	if scene != nil {
		// Frames first, then regular elements.
		for _, passFrame := range []bool{true, false} {
			for _, el := range scene.Elements {
				if el == nil || (el.Kind == KindFrame) != passFrame {
					continue
				}
				writeSVGElement(&sb, scene, el, pal)
			}
		}
	}
	sb.WriteString("</svg>\n")
	return sb.String()
}

func writeSVGElement(sb *strings.Builder, scene *Scene, el *Element, pal Palette) {
	if el.Kind == KindFrame && el.Name != "" {
		b := ElementBounds(el)
		fmt.Fprintf(sb, `  <text x="%.2f" y="%.2f" font-family="system-ui, sans-serif" font-size="13" font-weight="600" fill="%s">%s</text>`,
			b.X+4, b.Y-6, html.EscapeString(pal.FrameLabel), html.EscapeString(el.Name))
		sb.WriteByte('\n')
	}

	// Note background tint under hachure/stroke.
	if el.Kind == KindNote && pal.NoteFill != "" {
		nb := ElementBounds(el)
		fold := min(22, min(nb.W, nb.H)*0.24)
		poly := notePolygon(nb.W, nb.H, fold)
		cmds := polygonCmds(poly)
		translateCmds(cmds, nb.X, nb.Y)
		fmt.Fprintf(sb, `  <path d="%s" fill="%s" stroke="none"/>`, svgPathData(cmds), html.EscapeString(pal.NoteFill))
		sb.WriteByte('\n')
	}

	prims := BuildPrimitives(scene, el)
	for _, pr := range prims {
		d := svgPathData(pr.Cmds)
		if d == "" {
			continue
		}
		fill := "none"
		if pr.Filled {
			fill = pal.resolveFill(pr.FillColor)
		}
		stroke := "none"
		if pr.Stroked {
			if pr.IsHachure {
				stroke = pal.resolveFill(pr.StrokeColor)
				if stroke == "none" {
					stroke = pal.resolveStroke(pr.StrokeColor)
				}
			} else {
				stroke = pal.resolveStroke(pr.StrokeColor)
			}
		}
		dashAttr := ""
		if len(pr.Dash) == 2 {
			dashAttr = fmt.Sprintf(` stroke-dasharray="%.2f %.2f"`, pr.Dash[0], pr.Dash[1])
		}
		opAttr := ""
		if pr.Opacity > 0 && pr.Opacity < 0.999 {
			opAttr = fmt.Sprintf(` opacity="%.2f"`, pr.Opacity)
		}
		fmt.Fprintf(sb, `  <path d="%s" fill="%s" stroke="%s" stroke-width="%.2f" stroke-linecap="round" stroke-linejoin="round"%s%s/>`,
			d, html.EscapeString(fill), html.EscapeString(stroke), pr.StrokeWidth, dashAttr, opAttr)
		sb.WriteByte('\n')
	}

	if text := strings.TrimSpace(el.Text); text != "" {
		b := ShapeTextBox(el)
		fs := el.FontSize
		if fs <= 0 {
			fs = 18
		}
		ff := `Virgil, "Kaiti SC", STKaiti, "Comic Sans MS", cursive, system-ui, sans-serif`
		switch el.FontFamily {
		case "sans":
			ff = `system-ui, -apple-system, sans-serif`
		case "mono":
			ff = `ui-monospace, monospace`
		}
		color := pal.resolveStroke(el.StrokeColor)
		lines := strings.Split(text, "\n")
		lineH := fs * 1.3
		totalH := float32(len(lines)) * lineH
		cx, cy := b.Center()
		anchor := "middle"
		tx := cx
		startY := cy - totalH/2 + fs*0.85
		if el.Kind == KindText || el.Kind == KindNote {
			padX := float32(10)
			if el.Kind == KindText {
				padX = 4
				startY = b.Y + fs*0.92
			} else {
				startY = b.Y + 14 + fs*0.92
			}
			switch el.TextAlign {
			case "center":
				anchor, tx = "middle", cx
			case "right":
				anchor, tx = "end", b.X+b.W-padX
			default:
				anchor, tx = "start", b.X+padX
			}
		}
		for i, line := range lines {
			y := startY + float32(i)*lineH
			if el.Kind == KindArrow || el.Kind == KindLine {
				// Subtle paper halo behind edge labels.
				fmt.Fprintf(sb, `  <text x="%.2f" y="%.2f" text-anchor="middle" font-family="%s" font-size="%.1f" stroke="%s" stroke-width="5" stroke-linejoin="round" fill="%s">%s</text>`,
					tx, y, html.EscapeString(ff), fs, html.EscapeString(pal.Background), html.EscapeString(pal.Background), html.EscapeString(line))
				sb.WriteByte('\n')
			}
			fmt.Fprintf(sb, `  <text x="%.2f" y="%.2f" text-anchor="%s" font-family="%s" font-size="%.1f" fill="%s">%s</text>`,
				tx, y, anchor, html.EscapeString(ff), fs, html.EscapeString(color), html.EscapeString(line))
			sb.WriteByte('\n')
		}
	}
}

func svgPathData(cmds []PathCmd) string {
	if len(cmds) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, c := range cmds {
		if i > 0 {
			sb.WriteByte(' ')
		}
		switch c.Kind {
		case CmdMoveTo:
			fmt.Fprintf(&sb, "M%.2f %.2f", c.X, c.Y)
		case CmdLineTo:
			fmt.Fprintf(&sb, "L%.2f %.2f", c.X, c.Y)
		case CmdQuadTo:
			fmt.Fprintf(&sb, "Q%.2f %.2f %.2f %.2f", c.C1X, c.C1Y, c.X, c.Y)
		case CmdCubeTo:
			fmt.Fprintf(&sb, "C%.2f %.2f %.2f %.2f %.2f %.2f", c.C1X, c.C1Y, c.C2X, c.C2Y, c.X, c.Y)
		case CmdClose:
			sb.WriteByte('Z')
		}
	}
	return sb.String()
}
