package editor

import (
	"math"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/mermaid"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

const defaultMermaidToBoardSample = `flowchart LR
  C[客户端 Client] --> G{API 网关}
  G -->|通过| S([Worker 服务])
  S --> D[(Vault 存储)]`

// ConvertMermaidToWhiteboard parses a Mermaid flowchart or sequence diagram
// and converts it into editable hand-drawn whiteboard elements centered at (cx, cy).
func ConvertMermaidToWhiteboard(src string, cx, cy float32) ([]*whiteboard.Element, error) {
	measure := func(text string, size float32, bold bool) (w, h float32) {
		weight := 400
		if bold {
			weight = 600
		}
		tl := ui.ShapeRichTextWith(
			[]ui.Span{{Text: text, Size: size, Weight: weight}},
			ui.Font{Family: "system-ui", Size: size, Weight: weight},
			ui.TextShape{},
		)
		return tl.Size()
	}
	diag, err := mermaid.ParseAndLayout(src, measure)
	if err != nil {
		return nil, err
	}

	temp := whiteboard.NewScene()
	palette := []string{"blue", "green", "orange", "purple"}
	colorIdx := 0

	var nodeElems []*whiteboard.Element
	var frameElems []*whiteboard.Element

	for _, sh := range diag.Shapes {
		if sh.W <= 1 || sh.H <= 1 {
			continue
		}
		var kind whiteboard.ElementKind
		switch {
		case sh.Role == mermaid.RoleSubgraph || sh.Role == mermaid.RoleFragment:
			kind = whiteboard.KindFrame
		case sh.Role == mermaid.RoleNote:
			kind = whiteboard.KindNote
		case sh.Kind == mermaid.ShapeCylinder:
			kind = whiteboard.KindCylinder
		case sh.Kind == mermaid.ShapeDiamond:
			kind = whiteboard.KindDiamond
		case sh.Kind == mermaid.ShapeHexagon || sh.Kind == mermaid.ShapePentagon:
			kind = whiteboard.KindHexagon
		case sh.Kind == mermaid.ShapeParallelogram:
			kind = whiteboard.KindParallelogram
		case sh.Kind == mermaid.ShapeStadium:
			kind = whiteboard.KindStadium
		case sh.Kind == mermaid.ShapeCircle:
			kind = whiteboard.KindEllipse
		case sh.Kind == mermaid.ShapeActor:
			kind = whiteboard.KindActor
		default:
			kind = whiteboard.KindRectangle
		}

		el := whiteboard.NewElement(kind, sh.X, sh.Y, sh.W, sh.H)
		if kind == whiteboard.KindFrame {
			el.StrokeColor = "gray"
			if sh.Dashed {
				el.StrokeStyle = whiteboard.StrokeDashed
			}
			frameElems = append(frameElems, el)
		} else if kind == whiteboard.KindNote {
			el.StrokeColor = "orange"
			el.FillColor = "orange"
			el.TextAlign = "center"
			nodeElems = append(nodeElems, el)
		} else if kind == whiteboard.KindActor {
			el.StrokeColor = "ink"
			nodeElems = append(nodeElems, el)
		} else {
			col := palette[colorIdx%len(palette)]
			colorIdx++
			if sh.Role == mermaid.RoleActivation {
				col = "gray"
				el.Roughness = 0
			}
			el.StrokeColor = col
			el.FillColor = col
			el.FillStyle = whiteboard.FillHachure
			el.Roundness = sh.Kind == mermaid.ShapeRound || sh.Kind == mermaid.ShapeStadium || sh.Radius > 0
			if sh.Dashed {
				el.StrokeStyle = whiteboard.StrokeDashed
			}
			nodeElems = append(nodeElems, el)
		}
		temp.Elements = append(temp.Elements, el)
	}

	var edgeLabels []mermaid.LabelOp
	for _, lb := range diag.Labels {
		txt := strings.TrimSpace(lb.Text)
		if txt == "" {
			continue
		}
		if lb.Role == mermaid.RoleEdgeLabel {
			edgeLabels = append(edgeLabels, lb)
			continue
		}
		lx := lb.X + lb.W*0.5
		ly := lb.Y + lb.H*0.5
		// Find smallest enclosing non-frame shape first, then frame.
		var host *whiteboard.Element
		bestArea := float32(math.MaxFloat32)
		for _, el := range nodeElems {
			eb := whiteboard.ElementBounds(el)
			if eb.Contains(lx, ly) {
				area := eb.W * eb.H
				if area < bestArea {
					bestArea = area
					host = el
				}
			}
		}
		if host != nil && host.Text == "" {
			host.Text = txt
			if lb.Size > 0 {
				host.FontSize = max(12, lb.Size)
			}
			continue
		}
		for _, fr := range frameElems {
			fb := whiteboard.ElementBounds(fr)
			if fb.Contains(lx, ly) && fr.Name == "" {
				fr.Name = txt
				host = fr
				break
			}
		}
		if host != nil {
			continue
		}
		tEl := whiteboard.NewElement(whiteboard.KindText, lb.X, lb.Y, max(40, lb.W+12), max(24, lb.H+6))
		tEl.Text = txt
		if lb.Size > 0 {
			tEl.FontSize = max(12, lb.Size)
		}
		tEl.TextAlign = "center"
		temp.Elements = append(temp.Elements, tEl)
	}

	mapMarker := func(m mermaid.MarkerKind) whiteboard.Arrowhead {
		switch m {
		case mermaid.MarkerArrow:
			return whiteboard.ArrowTriangle
		case mermaid.MarkerOpenArrow:
			return whiteboard.ArrowOpen
		case mermaid.MarkerCircle:
			return whiteboard.ArrowDot
		case mermaid.MarkerCross:
			return whiteboard.ArrowBar
		default:
			return whiteboard.ArrowNone
		}
	}

	var edgeElems []*whiteboard.Element
	for _, path := range diag.Paths {
		if len(path.Points) < 2 {
			continue
		}
		kind := whiteboard.KindArrow
		if path.StartMarker == mermaid.MarkerNone && path.EndMarker == mermaid.MarkerNone {
			kind = whiteboard.KindLine
		}
		p0 := path.Points[0]
		pts := make([][2]float32, len(path.Points))
		for i, pt := range path.Points {
			pts[i] = [2]float32{pt[0] - p0[0], pt[1] - p0[1]}
		}
		last := pts[len(pts)-1]
		el := whiteboard.NewElement(kind, p0[0], p0[1], last[0], last[1])
		el.Points = pts
		el.Smooth = path.Smooth
		el.StartArrowhead = mapMarker(path.StartMarker)
		el.EndArrowhead = mapMarker(path.EndMarker)
		el.StrokeColor = "ink"
		switch path.Style {
		case mermaid.EdgeDashed:
			el.StrokeStyle = whiteboard.StrokeDashed
		case mermaid.EdgeThick:
			el.StrokeWidth = 3.2
		}
		if diag.Kind == "flowchart" {
			startAbs := path.Points[0]
			endAbs := path.Points[len(path.Points)-1]
			if target := whiteboard.FindBindTarget(temp, el.ID, startAbs[0], startAbs[1], 18); target != nil {
				el.StartBinding = &whiteboard.Binding{ElementID: target.ID, Gap: 6}
			}
			if target := whiteboard.FindBindTarget(temp, el.ID, endAbs[0], endAbs[1], 18); target != nil {
				el.EndBinding = &whiteboard.Binding{ElementID: target.ID, Gap: 6}
			}
		}
		edgeElems = append(edgeElems, el)
		temp.Elements = append(temp.Elements, el)
	}

	// Attach edge labels to their nearest connector element.
	for _, lb := range edgeLabels {
		txt := strings.TrimSpace(lb.Text)
		if txt == "" {
			continue
		}
		lx := lb.X + lb.W*0.5
		ly := lb.Y + lb.H*0.5
		var bestEdge *whiteboard.Element
		bestDist := float32(72)
		for _, edge := range edgeElems {
			if edge.Text != "" {
				continue
			}
			eb := whiteboard.ElementBounds(edge)
			ex, ey := eb.Center()
			if d := float32(math.Hypot(float64(lx-ex), float64(ly-ey))); d < bestDist {
				bestDist = d
				bestEdge = edge
			}
		}
		if bestEdge != nil {
			bestEdge.Text = txt
		} else {
			tEl := whiteboard.NewElement(whiteboard.KindText, lb.X, lb.Y, max(40, lb.W+12), max(24, lb.H+6))
			tEl.Text = txt
			tEl.FontSize = max(12, lb.Size)
			temp.Elements = append(temp.Elements, tEl)
		}
	}

	whiteboard.UpdateBindings(temp)
	bounds := whiteboard.SceneBounds(temp)
	bx, by := bounds.Center()
	dx, dy := cx-bx, cy-by
	for _, el := range temp.Elements {
		el.X += dx
		el.Y += dy
	}
	whiteboard.UpdateBindings(temp)
	return temp.Elements, nil
}
