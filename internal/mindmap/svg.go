package mindmap

import (
	"fmt"
	"html"
	"strings"

	"github.com/egoist/mygo/ui"
)

func svgColor(c ui.Color) string {
	if c.A == 255 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("rgba(%d,%d,%d,%.3f)", c.R, c.G, c.B, float64(c.A)/255.0)
}

// ExportSVG lays out the mind map using the provided context and returns a
// standalone vector SVG document.
func ExportSVG(c *ui.Context, s *State, rootItem Item, items []Item, o Options) string {
	exportState := &State{}
	if len(s.closed) > 0 {
		exportState.closed = make(map[string]bool, len(s.closed))
		for k, v := range s.closed {
			exportState.closed[k] = v
		}
	}
	// Measure plain text nodes cleanly without rich-canvas Draw callbacks so
	// every node has exact label dimensions for vector SVG export.
	plainRoot := rootItem
	plainRoot.Draw = nil
	plainRoot.Width, plainRoot.Height = 0, 0
	plainItems := make([]Item, len(items))
	for i, it := range items {
		it.Draw = nil
		it.Width, it.Height = 0, 0
		plainItems[i] = it
	}
	exportState.layout(c, plainRoot, plainItems, o)

	t := c.Theme()
	nodes, ox, oy := exportState.nodes, exportState.ox, exportState.oy
	n := len(plainItems)
	pad := float32(24)
	w := exportState.Width + 2*pad
	h := exportState.Height + 2*pad
	bg := t.Background.Mix(t.Text, .016)
	if t.Dark {
		bg = t.Background.Mix(t.Text, .028)
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.1f" height="%.1f" viewBox="0 0 %.1f %.1f">`+"\n", w, h, w, h)
	fmt.Fprintf(&b, `  <rect width="100%%" height="100%%" fill="%s"/>`+"\n", svgColor(bg))
	fmt.Fprintf(&b, `  <g transform="translate(%.2f,%.2f)" font-family="-apple-system, BlinkMacSystemFont, 'PingFang SC', 'Microsoft YaHei', sans-serif">`+"\n", pad, pad)

	// 1. Curves and underlines
	var drawLines func(i int)
	drawLines = func(i int) {
		nd := nodes[i]
		if !nd.shown {
			return
		}
		item := plainRoot
		if i < n {
			item = plainItems[i]
		}
		if i < n && item.Depth > 0 && !item.Panel {
			x, y := ox+nd.x, oy+nd.y+nd.h
			fmt.Fprintf(&b, `    <line x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f" stroke="%s" stroke-width="1"/>`+"\n",
				x, y, x+nd.w, y, svgColor(branchInk(t, o.Colors, nd.branch).Alpha(.42)))
		}
		if i < n && exportState.closed[item.ID] {
			return
		}
		for _, j := range nd.kids {
			kid := nodes[j]
			x0, x1 := nd.x+nd.w, kid.x
			if kid.left {
				x0, x1 = nd.x, kid.x+kid.w
			}
			y0, y1 := nd.y+nd.h/2, kid.y+kid.h/2
			if i < n && item.Depth > 0 && !item.Panel {
				y0 = nd.y + nd.h
			}
			if plainItems[j].Depth > 0 && !plainItems[j].Panel {
				y1 = kid.y + kid.h
			}
			px0, py0, px1, py1 := ox+x0, oy+y0, ox+x1, oy+y1
			cx := (px0 + px1) / 2
			sw := float32(1.75)
			if i < n {
				sw = 1.15
			}
			stroke := svgColor(t.Background.Mix(branchInk(t, o.Colors, kid.branch), .72))
			fmt.Fprintf(&b, `    <path d="M%.2f,%.2f C%.2f,%.2f %.2f,%.2f %.2f,%.2f" fill="none" stroke="%s" stroke-width="%.2f" stroke-linecap="round"/>`+"\n",
				px0, py0, cx, py0, cx, py1, px1, py1, stroke, sw)
			drawLines(j)
		}
	}
	drawLines(n)

	// 2. Nodes and labels
	var drawNodes func(i int)
	drawNodes = func(i int) {
		nd := nodes[i]
		if !nd.shown {
			return
		}
		item := plainRoot
		if i < n {
			item = plainItems[i]
		}
		fs, wt := labelFont(t, i, plainItems)
		x, y := ox+nd.x, oy+nd.y
		textColor := t.Text
		switch {
		case i == n:
			fill := t.Text.Mix(branchInk(t, o.Colors, 0), .50)
			rx := min(float32(18), nd.h/2)
			fmt.Fprintf(&b, `    <rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="%.2f" fill="%s"/>`+"\n",
				x, y, nd.w, nd.h, rx, svgColor(fill))
			textColor = t.Background
		case item.CodeBlock:
			fill := t.Background.Mix(t.Text, .045).Mix(branchInk(t, o.Colors, nd.branch), .025)
			fmt.Fprintf(&b, `    <rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="6" fill="%s" stroke="%s" stroke-width="1"/>`+"\n",
				x, y, nd.w, nd.h, svgColor(fill), svgColor(t.Border))
		case item.Depth == 0:
			ink := branchInk(t, o.Colors, nd.branch)
			fmt.Fprintf(&b, `    <rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="%.2f" fill="%s" stroke="%s" stroke-width="1"/>`+"\n",
				x, y, nd.w, nd.h, t.Space(1.5), svgColor(ink.Alpha(.09)), svgColor(ink.Alpha(.27)))
		}
		text := strings.TrimSpace(strings.ReplaceAll(item.Text, "\n", " "))
		fmt.Fprintf(&b, `    <text x="%.2f" y="%.2f" fill="%s" font-size="%.1f" font-weight="%d" text-anchor="middle" dominant-baseline="central">%s</text>`+"\n",
			x+nd.w/2, y+nd.h/2, svgColor(textColor), fs, wt, html.EscapeString(text))

		if i < n && len(nd.kids) > 0 && exportState.closed[item.ID] {
			color := branchInk(t, o.Colors, nd.branch)
			pillH := max(float32(15), t.Space(3.5))
			pillW := pillH
			if nd.descendants >= 100 {
				pillW = 26
			} else if nd.descendants >= 10 {
				pillW = 20
			}
			cx := x + nd.w + t.Space(.5) + pillW/2
			if nd.left {
				cx = x - t.Space(.5) - pillW/2
			}
			cy := y + nd.h
			if item.Depth == 0 || item.Panel {
				cy = y + nd.h/2
			}
			fmt.Fprintf(&b, `    <rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="%.2f" fill="%s"/>`+"\n",
				cx-pillW/2, cy-pillH/2, pillW, pillH, pillH/2, svgColor(color))
			fmt.Fprintf(&b, `    <text x="%.2f" y="%.2f" fill="%s" font-size="%.1f" font-weight="700" text-anchor="middle" dominant-baseline="central">%d</text>`+"\n",
				cx, cy, svgColor(t.Background), max(9, t.FontSize-3.5), nd.descendants)
			return
		}
		for _, j := range nd.kids {
			drawNodes(j)
		}
	}
	drawNodes(n)

	b.WriteString("  </g>\n</svg>\n")
	return b.String()
}
