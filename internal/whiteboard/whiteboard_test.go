package whiteboard

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestEncodeDecodeRoundTripAndExternalCompat(t *testing.T) {
	s := StarterScene()
	s.Elements[0].Link = "[[系统架构]]"
	note := NewElement(KindNote, 100, 220, 200, 120)
	note.Text = "参考 [[API设计#鉴权|鉴权文档]] #架构"
	s.Elements = append(s.Elements, note)

	raw, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Elements) != len(s.Elements) {
		t.Fatalf("got %d elements, want %d", len(got.Elements), len(s.Elements))
	}
	if got.Digest() != s.Digest() {
		t.Fatalf("digest mismatch across round-trip")
	}

	// External Excalidraw v2 JSON with containerId text, roundness object, hex colors, and deleted element.
	external := `{
	  "type": "excalidraw",
	  "version": 2,
	  "source": "https://excalidraw.com",
	  "elements": [
	    {
	      "id": "rect-1",
	      "type": "rectangle",
	      "x": 10,
	      "y": 20,
	      "width": 160,
	      "height": 80,
	      "strokeColor": "#1971c2",
	      "backgroundColor": "#a5d8ff",
	      "fillStyle": "hachure",
	      "strokeWidth": 2,
	      "roughness": 1,
	      "opacity": 100,
	      "seed": 42,
	      "version": 3,
	      "roundness": {"type": 3},
	      "isDeleted": false
	    },
	    {
	      "id": "txt-1",
	      "type": "text",
	      "x": 30,
	      "y": 40,
	      "width": 120,
	      "height": 30,
	      "text": "Service",
	      "fontSize": 20,
	      "fontFamily": 1,
	      "containerId": "rect-1",
	      "seed": 7,
	      "version": 1
	    },
	    {
	      "id": "gone",
	      "type": "ellipse",
	      "x": 0,
	      "y": 0,
	      "width": 10,
	      "height": 10,
	      "isDeleted": true
	    }
	  ]
	}`
	ext, err := Decode(external)
	if err != nil {
		t.Fatal(err)
	}
	if len(ext.Elements) != 1 {
		t.Fatalf("expected deleted and bound text folded into 1 element, got %d", len(ext.Elements))
	}
	el := ext.Elements[0]
	if el.Text != "Service" || el.StrokeColor != "blue" || el.FillColor != "blue" || !el.Roundness {
		t.Fatalf("unexpected folded element: %+v", el)
	}

	if _, err := Decode("{broken json"); err == nil {
		t.Fatal("expected error for broken JSON")
	}
	if _, err := Decode(`{"type":"excalidraw","version":99,"elements":[]}`); err == nil {
		t.Fatal("expected error for future version")
	}
}

func TestRoughDeterminismAndTranslationInvariance(t *testing.T) {
	el1 := NewElement(KindRectangle, 50, 80, 180, 90)
	el1.Seed = 98765
	el1.FillColor = "green"
	el1.FillStyle = FillCrossHatch

	el2 := el1.Clone()
	dx, dy := float32(240), float32(-30)
	el2.X += dx
	el2.Y += dy

	p1 := BuildPrimitives(nil, el1)
	p2 := BuildPrimitives(nil, el2)
	if len(p1) != len(p2) || len(p1) < 2 {
		t.Fatalf("expected fill + stroke primitives, got %d and %d", len(p1), len(p2))
	}
	for i := range p1 {
		if len(p1[i].Cmds) != len(p2[i].Cmds) {
			t.Fatalf("primitive %d cmd count mismatch: %d vs %d", i, len(p1[i].Cmds), len(p2[i].Cmds))
		}
		for j := range p1[i].Cmds {
			c1, c2 := p1[i].Cmds[j], p2[i].Cmds[j]
			if c1.Kind != c2.Kind {
				t.Fatalf("cmd kind mismatch at %d.%d", i, j)
			}
			if c1.Kind == CmdClose {
				continue
			}
			if math.Abs(float64((c2.X-c1.X)-dx)) > 0.01 || math.Abs(float64((c2.Y-c1.Y)-dy)) > 0.01 {
				t.Fatalf("wobble shifted on translation at %d.%d: (%v,%v) vs (%v,%v)", i, j, c1.X, c1.Y, c2.X, c2.Y)
			}
		}
	}
}

func TestHachureEllipseAndBindingsAndFrameMove(t *testing.T) {
	s := NewScene()
	frame := NewElement(KindFrame, 0, 0, 500, 300)
	frame.ID = "frame-1"
	frame.Name = "Backend"

	boxA := NewElement(KindEllipse, 40, 60, 120, 80)
	boxA.ID = "box-a"
	boxA.FillColor = "blue"
	boxA.FillStyle = FillHachure

	boxB := NewElement(KindDiamond, 300, 50, 120, 100)
	boxB.ID = "box-b"
	boxB.FillColor = "orange"
	boxB.FillStyle = FillCrossHatch

	arr := NewElement(KindArrow, 0, 0, 10, 10)
	arr.ID = "arr-1"
	arr.StartBinding = &Binding{ElementID: boxA.ID, Gap: 6}
	arr.EndBinding = &Binding{ElementID: boxB.ID, Gap: 6}

	s.Elements = []*Element{frame, boxA, boxB, arr}
	UpdateBindings(s)

	if len(BuildPrimitives(s, boxA)) < 2 {
		t.Fatal("expected hachure primitive on ellipse")
	}
	origEndX := arr.X + arr.Points[len(arr.Points)-1][0]

	// Move boxB down and right; bound arrow end must follow.
	MoveElements(s, []string{boxB.ID}, 50, 100)
	newEndX := arr.X + arr.Points[len(arr.Points)-1][0]
	newEndY := arr.Y + arr.Points[len(arr.Points)-1][1]
	if newEndX <= origEndX || newEndY <= 120 {
		t.Fatalf("arrow binding did not follow target move: end=(%v,%v) origEndX=%v", newEndX, newEndY, origEndX)
	}

	// Moving the frame must move boxA inside it too.
	beforeAX := boxA.X
	MoveElements(s, []string{frame.ID}, 30, 40)
	if boxA.X != beforeAX+30 {
		t.Fatalf("expected frame move to move child boxA by +30, got %v -> %v", beforeAX, boxA.X)
	}
}

func TestHistoryUndoRedoAndSnap(t *testing.T) {
	s := StarterScene()
	h := NewHistory()

	h.Begin(s)
	MoveElements(s, []string{"starter-a"}, 100, 40)
	if !h.Commit(s) {
		t.Fatal("expected commit after move")
	}
	if !h.CanUndo() {
		t.Fatal("expected CanUndo")
	}
	prev, ok := h.Undo(s)
	if !ok || prev.ElementByID("starter-a").X != 40 {
		t.Fatalf("undo failed: ok=%v", ok)
	}
	next, ok := h.Redo(prev)
	if !ok || next.ElementByID("starter-a").X != 140 {
		t.Fatalf("redo failed: ok=%v", ok)
	}

	guides, dx, _ := ComputeSnapGuides(s, map[string]bool{"starter-c": true}, Box{X: 142, Y: 200, W: 170, H: 76}, 6)
	if len(guides) == 0 || dx != -2 {
		t.Fatalf("expected snap guide aligning X=140, got guides=%v dx=%v", guides, dx)
	}
}

func TestWikiLinksSummaryAndSVG(t *testing.T) {
	s := StarterScene()
	s.ElementByID("starter-a").Link = "[[设计文档]]"
	s.ElementByID("starter-c").Text = "对接 [[后端接口|API]] #开发/v2"

	links := s.WikiLinks()
	if len(links) != 2 || links[0].Target != "设计文档" || links[1].Target != "后端接口" {
		t.Fatalf("unexpected wiki links: %+v", links)
	}
	tags := s.Tags()
	if len(tags) != 1 || tags[0] != "开发/v2" {
		t.Fatalf("unexpected tags: %+v", tags)
	}
	if !s.RewriteWikiLinks("设计文档", "新设计文档") {
		t.Fatal("expected RewriteWikiLinks to report change")
	}
	if s.ElementByID("starter-a").Link != "[[新设计文档]]" {
		t.Fatalf("link not rewritten: %q", s.ElementByID("starter-a").Link)
	}
	if !s.RewriteTags("开发", "研发") {
		t.Fatal("expected RewriteTags to report change")
	}
	if !strings.Contains(s.ElementByID("starter-c").Text, "#研发/v2") {
		t.Fatalf("tag not rewritten: %q", s.ElementByID("starter-c").Text)
	}
	sum := s.MarkdownSummary()
	if len(sum) < 5 {
		t.Fatalf("expected summary lines for nodes and arrows, got %d", len(sum))
	}
	svg := ExportSVG(s, DefaultLightPalette())
	if !strings.HasPrefix(svg, "<svg ") || !strings.Contains(svg, "<path d=") || !strings.Contains(svg, "核心想法") {
		t.Fatalf("invalid SVG output:\n%s", svg)
	}
}

func TestAmphibiousExcalidrawSVGRoundTrip(t *testing.T) {
	s := StarterScene()
	s.ElementByID("starter-a").Link = "[[设计文档]]"
	s.ElementByID("starter-c").Text = "对接 [[后端接口]]"

	svg, err := ExportEmbeddedSVG(s, DefaultLightPalette())
	if err != nil {
		t.Fatalf("ExportEmbeddedSVG: %v", err)
	}
	if !strings.Contains(svg, "<svg") || !strings.Contains(svg, "<path") || !strings.Contains(svg, "<text") || !strings.Contains(svg, svgPayloadStart) {
		t.Fatalf("unexpected embedded SVG output:\n%s", svg)
	}

	decoded, err := Decode(svg)
	if err != nil {
		t.Fatalf("Decode(embeddedSVG): %v", err)
	}
	if len(decoded.Elements) != len(s.Elements) {
		t.Fatalf("element count = %d, want %d", len(decoded.Elements), len(s.Elements))
	}
	if decoded.ElementByID("starter-a").Link != "[[设计文档]]" {
		t.Fatalf("lost Link after SVG round-trip: %q", decoded.ElementByID("starter-a").Link)
	}
	if links := decoded.WikiLinks(); len(links) != 2 {
		t.Fatalf("expected 2 wiki links after SVG decode, got %+v", links)
	}
	if _, err := Decode("<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>"); err == nil {
		t.Fatal("expected plain SVG without payload to fail Decode")
	}
}

func TestExtendedShapesRoundTripAndPrimitives(t *testing.T) {
	extended := []struct {
		kind ElementKind
		base string
	}{
		{KindTriangle, "diamond"},
		{KindParallelogram, "diamond"},
		{KindHexagon, "diamond"},
		{KindCylinder, "rectangle"},
		{KindCloud, "ellipse"},
		{KindStar, "ellipse"},
		{KindCallout, "rectangle"},
		{KindDocument, "rectangle"},
		{KindStadium, "ellipse"},
		{KindActor, "rectangle"},
	}

	s := NewScene()
	for i, tc := range extended {
		el := NewElement(tc.kind, float32(i%5)*160, float32(i/5)*140, 130, 90)
		el.ID = "ext-" + string(tc.kind)
		el.Text = "Node " + string(tc.kind)
		el.FillColor = "blue"
		el.FillStyle = FillHachure
		s.Elements = append(s.Elements, el)

		prims := BuildPrimitives(s, el)
		if len(prims) == 0 {
			t.Fatalf("BuildPrimitives(%q) returned 0 primitives", tc.kind)
		}
	}

	raw, err := s.Encode()
	if err != nil {
		t.Fatalf("Scene.Encode: %v", err)
	}
	var doc rawScene
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("Unmarshal rawScene: %v", err)
	}
	for i, tc := range extended {
		re := doc.Elements[i]
		if re.Type != tc.base {
			t.Fatalf("%q exported Excalidraw base type=%q, want %q", tc.kind, re.Type, tc.base)
		}
		if re.CustomData["arcmdShape"] != string(tc.kind) {
			t.Fatalf("%q missing arcmdShape customData: %+v", tc.kind, re.CustomData)
		}
	}

	svg, err := ExportEmbeddedSVG(s, DefaultLightPalette())
	if err != nil {
		t.Fatalf("ExportEmbeddedSVG: %v", err)
	}
	decoded, err := Decode(svg)
	if err != nil {
		t.Fatalf("Decode(embeddedSVG): %v", err)
	}
	if len(decoded.Elements) != len(s.Elements) {
		t.Fatalf("decoded element count=%d, want %d", len(decoded.Elements), len(s.Elements))
	}
	for _, tc := range extended {
		got := decoded.ElementByID("ext-" + string(tc.kind))
		if got == nil || got.Kind != tc.kind || got.Text != "Node "+string(tc.kind) {
			t.Fatalf("round-trip mismatch for %q: got=%+v", tc.kind, got)
		}
	}
}

func TestElbowRoutingMidBendAndGroupAlign(t *testing.T) {
	s := NewScene()
	a := NewElement(KindCylinder, 40, 40, 100, 80)
	a.ID = "shape-a"
	b := NewElement(KindHexagon, 280, 180, 120, 80)
	b.ID = "shape-b"
	c := NewElement(KindCloud, 520, 90, 130, 84)
	c.ID = "shape-c"

	arr := NewElement(KindArrow, 0, 0, 100, 100)
	arr.ID = "elbow-arr"
	arr.Elbowed = true
	arr.StartBinding = &Binding{ElementID: a.ID, Gap: 6}
	arr.EndBinding = &Binding{ElementID: b.ID, Gap: 6}

	s.Elements = []*Element{a, b, c, arr}
	UpdateBindings(s)

	if len(arr.Points) < 3 {
		t.Fatalf("expected elbow arrow to have >=3 routed points, got %+v", arr.Points)
	}
	for i := 1; i < len(arr.Points); i++ {
		dx := math.Abs(float64(arr.Points[i][0] - arr.Points[i-1][0]))
		dy := math.Abs(float64(arr.Points[i][1] - arr.Points[i-1][1]))
		if dx > 1e-3 && dy > 1e-3 {
			t.Fatalf("elbow segment %d is not orthogonal: %v -> %v", i, arr.Points[i-1], arr.Points[i])
		}
	}

	// Test HandleMid promotes a 2-pt connector to 3-pt curved arc.
	straight := NewElement(KindArrow, 100, 300, 200, 0)
	straight.ID = "mid-arr"
	straight.Points = [][2]float32{{0, 0}, {200, 0}}
	orig := straight.Clone()
	ResizeElement(straight, orig, HandleMid, 0, -45, false)
	if !straight.Smooth || len(straight.Points) != 3 {
		t.Fatalf("expected HandleMid to promote smooth 3-point arrow, got smooth=%v pts=%+v", straight.Smooth, straight.Points)
	}
	if math.Abs(float64(straight.Points[1][0]-100)) > 1e-3 || math.Abs(float64(straight.Points[1][1]-(-45))) > 1e-3 {
		t.Fatalf("unexpected HandleMid bend point: %+v", straight.Points)
	}

	// Grouping & expansion
	gid := GroupElements(s, []string{a.ID, b.ID})
	if gid == "" {
		t.Fatal("expected non-empty GroupElements ID")
	}
	expanded := ExpandGroupSelection(s, map[string]bool{a.ID: true})
	if !expanded[a.ID] || !expanded[b.ID] || expanded[c.ID] {
		t.Fatalf("unexpected ExpandGroupSelection: %+v", expanded)
	}
	if !UngroupElements(s, []string{a.ID, b.ID}) {
		t.Fatal("expected UngroupElements to report change")
	}
	if len(a.GroupIDs) != 0 || len(b.GroupIDs) != 0 {
		t.Fatalf("expected GroupIDs cleared after UngroupElements, got a=%v b=%v", a.GroupIDs, b.GroupIDs)
	}

	// Left align & horizontal distribute
	if !AlignElements(s, []string{a.ID, b.ID, c.ID}, "left") {
		t.Fatal("expected AlignElements(left) to adjust positions")
	}
	if a.X != 40 || b.X != 40 || c.X != 40 {
		t.Fatalf("left alignment mismatch: X=(%v, %v, %v)", a.X, b.X, c.X)
	}
	a.X, b.X, c.X = 20, 120, 420
	if !DistributeElements(s, []string{a.ID, b.ID, c.ID}, true) {
		t.Fatal("expected DistributeElements(horizontal) to report change")
	}
	gap1 := b.X - (a.X + a.W)
	gap2 := c.X - (b.X + b.W)
	if math.Abs(float64(gap1-gap2)) > 0.5 {
		t.Fatalf("expected equal horizontal gaps after distribute, got gap1=%v gap2=%v", gap1, gap2)
	}
}

func TestLibraryBuiltinsAndCodec(t *testing.T) {
	builtins := BuiltinLibrary()
	if len(builtins) != 12 {
		t.Fatalf("expected 12 built-in library items, got %d", len(builtins))
	}
	for _, item := range builtins {
		elems := InstantiateLibraryItem(item, 400, 300)
		if len(elems) == 0 {
			t.Fatalf("InstantiateLibraryItem(%q) returned empty slice", item.Name)
		}
		var sharedGroup string
		for _, el := range elems {
			if len(el.GroupIDs) == 0 {
				t.Fatalf("instantiated item %q missing shared GroupIDs on %q", item.Name, el.ID)
			}
			if sharedGroup == "" {
				sharedGroup = el.GroupIDs[len(el.GroupIDs)-1]
			}
		}
	}

	encoded, err := EncodeLibrary(builtins[:2])
	if err != nil {
		t.Fatalf("EncodeLibrary: %v", err)
	}
	decodedV2, err := DecodeLibrary(encoded)
	if err != nil {
		t.Fatalf("DecodeLibrary(v2): %v", err)
	}
	if len(decodedV2) != 2 || decodedV2[0].Name != builtins[0].Name || len(decodedV2[0].Elements) != len(builtins[0].Elements) {
		t.Fatalf("unexpected decoded v2 library items: %+v", decodedV2)
	}

	v1 := `{"type":"excalidrawlib","version":1,"library":[[{"id":"v1a","type":"rectangle","x":0,"y":0,"width":80,"height":40,"customData":{"arcmdShape":"cylinder"}}]]}`
	decodedV1, err := DecodeLibrary(v1)
	if err != nil {
		t.Fatalf("DecodeLibrary(v1): %v", err)
	}
	if len(decodedV1) != 1 || len(decodedV1[0].Elements) != 1 || decodedV1[0].Elements[0].Kind != KindCylinder {
		t.Fatalf("unexpected decoded v1 library items: %+v", decodedV1)
	}
}

func TestClipboardCodecAndInstantiate(t *testing.T) {
	a := NewElement(KindCylinder, 40, 40, 100, 90)
	a.ID = "src-a"
	a.Text = "Primary DB"
	a.GroupIDs = []string{"grp-old"}
	b := NewElement(KindHexagon, 220, 45, 110, 80)
	b.ID = "src-b"
	b.GroupIDs = []string{"grp-old"}
	arrInternal := NewElement(KindArrow, 140, 85, 80, 0)
	arrInternal.ID = "src-arr1"
	arrInternal.StartBinding = &Binding{ElementID: a.ID, Gap: 6}
	arrInternal.EndBinding = &Binding{ElementID: b.ID, Gap: 6}
	arrExternal := NewElement(KindArrow, 10, 85, 30, 0)
	arrExternal.ID = "src-arr2"
	arrExternal.StartBinding = &Binding{ElementID: "outside-node", Gap: 6}
	arrExternal.EndBinding = &Binding{ElementID: a.ID, Gap: 6}

	encoded, err := EncodeClipboard([]*Element{a, b, arrInternal, arrExternal})
	if err != nil {
		t.Fatalf("EncodeClipboard: %v", err)
	}
	if !strings.Contains(encoded, `"excalidraw/clipboard"`) {
		t.Fatalf("unexpected clipboard payload: %s", encoded)
	}

	decoded, ok := DecodeClipboard(encoded)
	if !ok || len(decoded) != 4 {
		t.Fatalf("DecodeClipboard failed: ok=%v len=%d", ok, len(decoded))
	}
	if _, plainOK := DecodeClipboard("普通文本，不是白板 JSON"); plainOK {
		t.Fatal("DecodeClipboard should return false for plain text")
	}

	pasted := InstantiateClipboard(decoded, 500, 400)
	if len(pasted) != 4 {
		t.Fatalf("InstantiateClipboard len=%d, want 4", len(pasted))
	}
	pa, pb, pi, pe := pasted[0], pasted[1], pasted[2], pasted[3]
	if pa.ID == a.ID || pb.ID == b.ID || pi.ID == arrInternal.ID || pe.ID == arrExternal.ID {
		t.Fatalf("expected fresh IDs after InstantiateClipboard")
	}
	if len(pa.GroupIDs) != 1 || len(pb.GroupIDs) != 1 || pa.GroupIDs[0] != pb.GroupIDs[0] || pa.GroupIDs[0] == "grp-old" {
		t.Fatalf("expected remapped shared group ID, got a=%v b=%v", pa.GroupIDs, pb.GroupIDs)
	}
	if pi.StartBinding == nil || pi.StartBinding.ElementID != pa.ID || pi.EndBinding == nil || pi.EndBinding.ElementID != pb.ID {
		t.Fatalf("internal binding not remapped cleanly: start=%+v end=%+v", pi.StartBinding, pi.EndBinding)
	}
	if pe.StartBinding != nil {
		t.Fatalf("expected dangling external StartBinding cleared, got %+v", pe.StartBinding)
	}
	if pe.EndBinding == nil || pe.EndBinding.ElementID != pa.ID {
		t.Fatalf("expected internal EndBinding on pe remapped to pa, got %+v", pe.EndBinding)
	}
}

func TestResizeLibraryComponentsAndGroups(t *testing.T) {
	builtins := BuiltinLibrary()
	byID := map[string]LibraryItem{}
	for _, it := range builtins {
		byID[it.ID] = it
	}

	// 1) Desktop browser wireframe: verify proportional scaling of boxes, zero-height divider line, and URL font size.
	browser := byID["builtin-browser"]
	elems := InstantiateLibraryItem(browser, 400, 300)
	s := &Scene{Elements: elems}
	origBox := SelectionBounds(elems)
	origMap := make(map[string]*Element, len(elems))
	for _, el := range elems {
		origMap[el.ID] = el.Clone()
	}

	// Scale via HandleSE to 1.5x width and 1.5x height (with Shift aspect lock).
	ResizeElements(s, origBox, origMap, HandleSE, origBox.W*0.5, origBox.H*0.5, true)
	scaledBox := SelectionBounds(s.Elements)
	if math.Abs(float64(scaledBox.X-origBox.X)) > 0.5 || math.Abs(float64(scaledBox.Y-origBox.Y)) > 0.5 {
		t.Fatalf("HandleSE moved anchored top-left corner: got (%v,%v), want (%v,%v)", scaledBox.X, scaledBox.Y, origBox.X, origBox.Y)
	}
	if math.Abs(float64(scaledBox.W-origBox.W*1.5)) > 1 || math.Abs(float64(scaledBox.H-origBox.H*1.5)) > 1 {
		t.Fatalf("HandleSE 1.5x size mismatch: got %.1fx%.1f, want %.1fx%.1f", scaledBox.W, scaledBox.H, origBox.W*1.5, origBox.H*1.5)
	}

	var bar, urlPill, hero *Element
	var origURLFS float32
	for _, el := range s.Elements {
		switch {
		case el.Kind == KindLine:
			bar = el
		case el.Kind == KindStadium:
			urlPill = el
			origURLFS = origMap[el.ID].FontSize
			if origURLFS <= 0 {
				origURLFS = 18
			}
		case el.Text == "Hero Section":
			hero = el
		}
	}
	if bar == nil || len(bar.Points) < 2 || math.Abs(float64(bar.Points[1][0]-scaledBox.W)) > 1 || math.Abs(float64(bar.Points[1][1])) > 0.5 {
		t.Fatalf("divider line did not scale proportionally with browser: bar=%+v scaledW=%v", bar, scaledBox.W)
	}
	if urlPill == nil || urlPill.FontSize <= origURLFS {
		t.Fatalf("expected URL bar fontSize to scale up from %v, got %+v", origURLFS, urlPill)
	}
	if hero == nil || math.Abs(float64(hero.W-origMap[hero.ID].W*1.5)) > 1 {
		t.Fatalf("Hero Section width mismatch after 1.5x scale: got %v", hero)
	}

	// Verify HandleNW anchors the bottom-right corner and shrinks proportionally.
	ResizeElements(s, origBox, origMap, HandleNW, origBox.W*0.25, origBox.H*0.25, true)
	shrunk := SelectionBounds(s.Elements)
	if math.Abs(float64((shrunk.X+shrunk.W)-(origBox.X+origBox.W))) > 1 || math.Abs(float64((shrunk.Y+shrunk.H)-(origBox.Y+origBox.H))) > 1 {
		t.Fatalf("HandleNW moved anchored bottom-right corner: shrunk=%+v origBox=%+v", shrunk, origBox)
	}
	if math.Abs(float64(shrunk.W-origBox.W*0.75)) > 1 || math.Abs(float64(shrunk.H-origBox.H*0.75)) > 1 {
		t.Fatalf("HandleNW 0.75x size mismatch: got %.1fx%.1f, want %.1fx%.1f", shrunk.W, shrunk.H, origBox.W*0.75, origBox.H*0.75)
	}

	// 2) Quadrant matrix with negative-delta upward arrow (axV) & database cluster with bound WAL arrow.
	qElems := InstantiateLibraryItem(byID["builtin-quadrant"], 500, 400)
	qScene := &Scene{Elements: qElems}
	qOrigBox := SelectionBounds(qElems)
	qOrigMap := make(map[string]*Element, len(qElems))
	for _, el := range qElems {
		qOrigMap[el.ID] = el.Clone()
	}
	ResizeElements(qScene, qOrigBox, qOrigMap, HandleSE, qOrigBox.W*0.5, qOrigBox.H*0.5, false)
	qScaled := SelectionBounds(qScene.Elements)
	if math.Abs(float64(qScaled.H-qOrigBox.H*1.5)) > 2 {
		t.Fatalf("quadrant matrix height mismatch: got %v, want %v", qScaled.H, qOrigBox.H*1.5)
	}

	dbElems := InstantiateLibraryItem(byID["builtin-db-cluster"], 400, 300)
	dbScene := &Scene{Elements: dbElems}
	dbOrigBox := SelectionBounds(dbElems)
	dbOrigMap := make(map[string]*Element, len(dbElems))
	for _, el := range dbElems {
		dbOrigMap[el.ID] = el.Clone()
	}
	ResizeElements(dbScene, dbOrigBox, dbOrigMap, HandleSE, dbOrigBox.W, dbOrigBox.H*0.5, false)
	var dbArrow, dbM, dbR *Element
	for _, el := range dbScene.Elements {
		switch el.Kind {
		case KindArrow:
			dbArrow = el
		case KindCylinder:
			if dbM == nil {
				dbM = el
			} else {
				dbR = el
			}
		}
	}
	if dbArrow == nil || dbM == nil || dbR == nil {
		t.Fatal("missing database cluster elements")
	}
	startWorldX := dbArrow.X + dbArrow.Points[0][0]
	endWorldX := dbArrow.X + dbArrow.Points[len(dbArrow.Points)-1][0]
	if !(startWorldX > dbM.X+dbM.W && endWorldX < dbR.X) {
		t.Fatalf("bound WAL arrow did not re-anchor between scaled cylinders: arrow=[%v..%v] dbM.Right=%v dbR.Left=%v",
			startWorldX, endWorldX, dbM.X+dbM.W, dbR.X)
	}
}
