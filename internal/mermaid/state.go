package mermaid

import (
	"fmt"
	"slices"
	"strings"
)

type stateKind uint8

const (
	stateKindNormal stateKind = iota
	stateKindStart
	stateKindEnd
	stateKindChoice
	stateKindFork
	stateKindJoin
	stateKindComposite
)

type stateNoteSide uint8

const (
	stateNoteRight stateNoteSide = iota
	stateNoteLeft
)

type stateNote struct {
	target *stateNode
	side   stateNoteSide
	text   string
	w, h   float32
	lw, lh float32
}

type stateNode struct {
	id            string
	label         string
	explicitLabel bool
	kind          stateKind
	descriptions  []string
	ownerRegion   *stateRegion
	regions       []*stateRegion
	dir           flowDir
	dirSet        bool

	// Measured / laid out dimensions in ownerRegion coordinates.
	x, y       float32
	w, h       float32
	titleW     float32
	titleH     float32
	descW      float32
	descH      float32
	headerH    float32
	absX, absY float32 // Final coordinates relative to current ancestor during emission.

	// Composite interior layout cache.
	innerShapes []ShapeOp
	innerPaths  []PathOp
	innerLabels []LabelOp
}

func (n *stateNode) displayLabel() string {
	if n.label != "" {
		return n.label
	}
	return n.id
}

type stateEdge struct {
	from   *stateNode
	to     *stateNode
	label  string
	labelW float32
	labelH float32
}

type stateRegion struct {
	parent    *stateNode
	dir       flowDir
	nodes     []*stateNode
	edges     []*stateEdge
	cross     []*stateEdge
	notes     []*stateNote
	startNode *stateNode
	endNode   *stateNode
}

func newStateRegion(parent *stateNode, dir flowDir) *stateRegion {
	return &stateRegion{parent: parent, dir: dir}
}

func (r *stateRegion) ensureStart() *stateNode {
	if r.startNode != nil {
		return r.startNode
	}
	n := &stateNode{
		id:          "__start",
		kind:        stateKindStart,
		ownerRegion: r,
	}
	r.startNode = n
	r.nodes = append(r.nodes, n)
	return n
}

func (r *stateRegion) ensureEnd() *stateNode {
	if r.endNode != nil {
		return r.endNode
	}
	n := &stateNode{
		id:          "__end",
		kind:        stateKindEnd,
		ownerRegion: r,
	}
	r.endNode = n
	r.nodes = append(r.nodes, n)
	return n
}

const (
	stateFontSize          float32 = 13
	stateDescFontSize      float32 = 11.5
	stateEdgeFontSize      float32 = 11.5
	stateNoteFontSize      float32 = 11.5
	stateCompositeFontSize float32 = 12.5
)

func layoutStateDiagram(lines []string, measure MeasureFunc) (Diagram, error) {
	root, err := parseStateDiagram(lines[1:])
	if err != nil {
		return Diagram{}, err
	}
	// Collect non-empty top-level regions.
	var active []*stateRegion
	for _, r := range root.regions {
		if len(r.nodes) > 0 {
			active = append(active, r)
		}
	}
	if len(active) == 0 {
		return Diagram{}, fmt.Errorf("state diagram has no states")
	}

	const outerPad float32 = 16
	var diag Diagram
	diag.Kind = "state"

	if len(active) == 1 {
		rl := layoutStateRegion(active[0], measure)
		diag.Shapes = rl.shapes
		diag.Paths = rl.paths
		diag.Labels = rl.labels
		routeCrossEdges(active[0], &diag.Paths, &diag.Labels, measure)
	} else {
		horizontal := root.dir == dirLR || root.dir == dirRL
		const regionGap float32 = 24
		layouts := make([]regionLayout, len(active))
		var maxOpp float32
		for i, r := range active {
			layouts[i] = layoutStateRegion(r, measure)
			if horizontal {
				maxOpp = max(maxOpp, layouts[i].w)
			} else {
				maxOpp = max(maxOpp, layouts[i].h)
			}
		}
		var cursor float32
		for i, rl := range layouts {
			if i > 0 {
				sep := cursor + regionGap/2
				if horizontal {
					diag.Paths = append(diag.Paths, PathOp{
						Points: [][2]float32{{0, sep}, {maxOpp, sep}},
						Style:  EdgeDashed,
						Role:   RoleFragment,
					})
				} else {
					diag.Paths = append(diag.Paths, PathOp{
						Points: [][2]float32{{sep, 0}, {sep, maxOpp}},
						Style:  EdgeDashed,
						Role:   RoleFragment,
					})
				}
				cursor += regionGap
			}
			var ox, oy float32
			if horizontal {
				ox, oy = (maxOpp-rl.w)/2, cursor
				cursor += rl.h
			} else {
				ox, oy = cursor, (maxOpp-rl.h)/2
				cursor += rl.w
			}
			appendTranslated(&diag.Shapes, &diag.Paths, &diag.Labels, rl.shapes, rl.paths, rl.labels, ox, oy)
			shiftRegionAbs(active[i], ox, oy)
		}
		for _, r := range active {
			routeCrossEdges(r, &diag.Paths, &diag.Labels, measure)
		}
		// Route cross-edges between top-level concurrent regions (stored on root.regions[0]).
		routeCrossEdges(root.regions[0], &diag.Paths, &diag.Labels, measure)
	}

	normalizeDiagramBounds(&diag, outerPad)
	return diag, nil
}

func parseStateDiagram(rawLines []string) (*stateNode, error) {
	lines := expandStateLines(rawLines)

	root := &stateNode{id: "__root", kind: stateKindComposite, dir: dirTD}
	firstReg := newStateRegion(root, dirTD)
	root.regions = []*stateRegion{firstReg}

	allByID := map[string]*stateNode{}
	var compStack []*stateNode
	regStack := []*stateRegion{firstReg}
	var allEdges []*stateEdge
	var pendingNotes []struct {
		targetID string
		side     stateNoteSide
		text     string
		reg      *stateRegion
	}

	curReg := func() *stateRegion { return regStack[len(regStack)-1] }
	curComp := func() *stateNode {
		if len(compStack) == 0 {
			return root
		}
		return compStack[len(compStack)-1]
	}

	ensureNamed := func(reg *stateRegion, id string) *stateNode {
		id = stripStateClass(id)
		if id == "" {
			return nil
		}
		if n, ok := allByID[id]; ok {
			return n
		}
		n := &stateNode{
			id:          id,
			kind:        stateKindNormal,
			ownerRegion: reg,
		}
		reg.nodes = append(reg.nodes, n)
		allByID[id] = n
		return n
	}

	promoteComposite := func(n *stateNode, parentDir flowDir) {
		n.kind = stateKindComposite
		if !n.dirSet {
			n.dir = parentDir
		}
		reg := newStateRegion(n, n.dir)
		n.regions = append(n.regions, reg)
		compStack = append(compStack, n)
		regStack = append(regStack, reg)
	}

	var (
		inNote       bool
		noteTargetID string
		noteSide     stateNoteSide
		noteLines    []string
		noteReg      *stateRegion
		inAccDescr   bool
		lastDeclared *stateNode
	)

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		if inNote {
			if isEndNoteLine(line) {
				pendingNotes = append(pendingNotes, struct {
					targetID string
					side     stateNoteSide
					text     string
					reg      *stateRegion
				}{
					targetID: noteTargetID,
					side:     noteSide,
					text:     normalizeLabel(strings.Join(noteLines, "\n")),
					reg:      noteReg,
				})
				inNote = false
				noteLines = nil
				continue
			}
			noteLines = append(noteLines, line)
			continue
		}

		if inAccDescr {
			if line == "}" {
				inAccDescr = false
			}
			continue
		}

		low := strings.ToLower(line)

		if line == "}" {
			if len(compStack) == 0 {
				return nil, fmt.Errorf("unexpected '}'")
			}
			compStack = compStack[:len(compStack)-1]
			regStack = regStack[:len(regStack)-1]
			lastDeclared = nil
			continue
		}
		if line == "{" {
			if lastDeclared == nil {
				return nil, fmt.Errorf("unexpected '{'")
			}
			promoteComposite(lastDeclared, curReg().dir)
			lastDeclared = nil
			continue
		}
		if line == "--" {
			cc := curComp()
			reg := newStateRegion(cc, cc.dir)
			cc.regions = append(cc.regions, reg)
			regStack[len(regStack)-1] = reg
			lastDeclared = nil
			continue
		}

		// Ignored metadata / styling directives.
		if low == "hide empty description" ||
			strings.HasPrefix(low, "classdef ") ||
			strings.HasPrefix(low, "class ") ||
			strings.HasPrefix(low, "style ") ||
			strings.HasPrefix(low, "linkstyle ") ||
			strings.HasPrefix(low, "click ") ||
			strings.HasPrefix(low, "acctitle") {
			lastDeclared = nil
			continue
		}
		if strings.HasPrefix(low, "accdescr") {
			if strings.HasSuffix(line, "{") {
				inAccDescr = true
			}
			lastDeclared = nil
			continue
		}

		// Direction directive.
		if strings.HasPrefix(low, "direction ") {
			d := parseStateDir(strings.TrimSpace(line[len("direction "):]))
			reg := curReg()
			reg.dir = d
			cc := curComp()
			if !cc.dirSet && len(cc.regions) == 1 {
				cc.dir = d
				cc.dirSet = true
			}
			lastDeclared = nil
			continue
		}

		// Note statements: "note right of S : text" or multiline "note left of S ... end note".
		if side, target, text, hasColon, ok := parseStateNoteHeader(line); ok {
			lastDeclared = nil
			if hasColon {
				pendingNotes = append(pendingNotes, struct {
					targetID string
					side     stateNoteSide
					text     string
					reg      *stateRegion
				}{targetID: target, side: side, text: text, reg: curReg()})
			} else {
				inNote = true
				noteTargetID = target
				noteSide = side
				noteLines = nil
				noteReg = curReg()
			}
			continue
		}

		// Transition statements: A --> B [: label] or A -> B [: label] (supports chaining).
		if trans, ok := parseStateTransitions(line); ok {
			lastDeclared = nil
			reg := curReg()
			for _, tr := range trans {
				var fromNode, toNode *stateNode
				if tr.from == "[*]" {
					fromNode = reg.ensureStart()
				} else {
					fromNode = ensureNamed(reg, tr.from)
				}
				if tr.to == "[*]" {
					toNode = reg.ensureEnd()
				} else {
					toNode = ensureNamed(reg, tr.to)
				}
				if fromNode != nil && toNode != nil {
					allEdges = append(allEdges, &stateEdge{
						from:  fromNode,
						to:    toNode,
						label: tr.label,
					})
				}
			}
			continue
		}

		// State declarations: composite, stereotype, alias, or bare state.
		if strings.HasPrefix(low, "state ") {
			decl, err := parseStateDecl(strings.TrimSpace(line[len("state "):]))
			if err != nil {
				return nil, err
			}
			reg := curReg()
			n := ensureNamed(reg, decl.id)
			if n == nil {
				lastDeclared = nil
				continue
			}
			if decl.hasLabel {
				n.label = decl.label
				n.explicitLabel = true
			}
			if decl.stereotype != stateKindNormal {
				n.kind = decl.stereotype
			}
			if decl.openBrace {
				promoteComposite(n, reg.dir)
				lastDeclared = nil
			} else {
				lastDeclared = n
			}
			continue
		}

		// State description: "StateID : description text".
		if colon := indexUnquotedByte(line, ':'); colon > 0 {
			id := stripStateClass(strings.TrimSpace(line[:colon]))
			desc := normalizeLabel(line[colon+1:])
			if id != "" && id != "[*]" {
				n := ensureNamed(curReg(), id)
				if n != nil && desc != "" {
					n.descriptions = append(n.descriptions, desc)
				}
				lastDeclared = n
				continue
			}
		}

		// Bare state identifier line.
		if id := stripStateClass(line); id != "" && id != "[*]" && !strings.ContainsAny(id, " \t") {
			lastDeclared = ensureNamed(curReg(), id)
		}
	}

	if inNote {
		return nil, fmt.Errorf("unclosed note block")
	}
	if len(compStack) > 0 {
		return nil, fmt.Errorf("unclosed composite state %q", compStack[len(compStack)-1].id)
	}

	// Attach pending notes to their resolved target nodes.
	for _, pn := range pendingNotes {
		target := allByID[pn.targetID]
		if target == nil {
			target = ensureNamed(pn.reg, pn.targetID)
		}
		if target == nil || target.ownerRegion == nil {
			continue
		}
		target.ownerRegion.notes = append(target.ownerRegion.notes, &stateNote{
			target: target,
			side:   pn.side,
			text:   pn.text,
		})
	}

	// Classify edges into local region edges vs cross-boundary LCA edges.
	for _, e := range allEdges {
		if e.from.ownerRegion == e.to.ownerRegion {
			e.from.ownerRegion.edges = append(e.from.ownerRegion.edges, e)
			continue
		}
		lca := findLCARegion(e.from.ownerRegion, e.to.ownerRegion)
		if lca == nil {
			lca = root.regions[0]
		}
		lca.cross = append(lca.cross, e)
	}

	return root, nil
}

func findLCARegion(a, b *stateRegion) *stateRegion {
	if a == nil || b == nil {
		return nil
	}
	anc := map[*stateRegion]bool{}
	for r := a; r != nil; {
		anc[r] = true
		if r.parent == nil {
			break
		}
		r = r.parent.ownerRegion
	}
	for r := b; r != nil; {
		if anc[r] {
			return r
		}
		if r.parent == nil {
			break
		}
		r = r.parent.ownerRegion
	}
	return nil
}

// ancestorInRegion returns the direct child node of reg that contains n (or n itself).
func ancestorInRegion(n *stateNode, reg *stateRegion) *stateNode {
	for cur := n; cur != nil; {
		if cur.ownerRegion == reg {
			return cur
		}
		if cur.ownerRegion == nil {
			return nil
		}
		cur = cur.ownerRegion.parent
	}
	return nil
}

func expandStateLines(lines []string) []string {
	var out []string
	inNote := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if inNote {
			out = append(out, line)
			if isEndNoteLine(line) {
				inNote = false
			}
			continue
		}
		if _, _, _, hasColon, ok := parseStateNoteHeader(line); ok && !hasColon {
			inNote = true
			out = append(out, line)
			continue
		}
		out = append(out, splitStateBraces(line)...)
	}
	return out
}

// splitStateBraces splits inline composite state blocks like
// `state A { [*] --> B; B --> [*] }` into separate statement lines without
// disturbing braces that appear inside quotes or after a label colon.
func splitStateBraces(line string) []string {
	low := strings.ToLower(line)
	if strings.HasPrefix(low, "accdescr") {
		return []string{line}
	}
	// If the line is a simple `state ... {` or `}`, keep as-is.
	if line == "{" || line == "}" {
		return []string{line}
	}
	// Only split braces when an unquoted '{' or '}' appears before any unquoted ':'
	// (or on a line that starts with "state " and has content after '{').
	var parts []string
	cur := line
	for len(cur) > 0 {
		cur = strings.TrimSpace(cur)
		if cur == "" {
			break
		}
		colonIdx := indexUnquotedByte(cur, ':')
		openIdx := indexUnquotedByte(cur, '{')
		closeIdx := indexUnquotedByte(cur, '}')
		// Ignore braces that belong to a label after ':'.
		if colonIdx >= 0 {
			if openIdx > colonIdx {
				openIdx = -1
			}
			if closeIdx > colonIdx {
				closeIdx = -1
			}
		}
		if openIdx < 0 && closeIdx < 0 {
			for _, sub := range splitSemicolons(cur) {
				if s := strings.TrimSpace(sub); s != "" {
					parts = append(parts, s)
				}
			}
			break
		}
		// Pick earliest brace.
		if openIdx >= 0 && (closeIdx < 0 || openIdx < closeIdx) {
			head := strings.TrimSpace(cur[:openIdx+1])
			if head != "" {
				parts = append(parts, head)
			}
			cur = cur[openIdx+1:]
			continue
		}
		// closeIdx is earliest.
		before := strings.TrimSpace(cur[:closeIdx])
		if before != "" {
			for _, sub := range splitSemicolons(before) {
				if s := strings.TrimSpace(sub); s != "" {
					parts = append(parts, s)
				}
			}
		}
		parts = append(parts, "}")
		cur = cur[closeIdx+1:]
	}
	return parts
}

func parseStateDir(s string) flowDir {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "LR":
		return dirLR
	case "RL":
		return dirRL
	case "BT":
		return dirBT
	default:
		return dirTD
	}
}

func isEndNoteLine(line string) bool {
	fields := strings.Fields(strings.ToLower(line))
	return len(fields) == 2 && fields[0] == "end" && fields[1] == "note"
}

func parseStateNoteHeader(line string) (side stateNoteSide, target, text string, hasColon, ok bool) {
	low := strings.ToLower(line)
	if !strings.HasPrefix(low, "note ") {
		return 0, "", "", false, false
	}
	rest := strings.TrimSpace(line[len("note "):])
	restLow := strings.ToLower(rest)
	switch {
	case strings.HasPrefix(restLow, "right of "):
		side = stateNoteRight
		rest = strings.TrimSpace(rest[len("right of "):])
	case strings.HasPrefix(restLow, "left of "):
		side = stateNoteLeft
		rest = strings.TrimSpace(rest[len("left of "):])
	default:
		return 0, "", "", false, false
	}
	if colon := indexUnquotedByte(rest, ':'); colon >= 0 {
		target = stripStateClass(strings.TrimSpace(rest[:colon]))
		text = normalizeLabel(rest[colon+1:])
		if target == "" {
			return 0, "", "", false, false
		}
		return side, target, text, true, true
	}
	target = stripStateClass(strings.TrimSpace(rest))
	if target == "" {
		return 0, "", "", false, false
	}
	return side, target, "", false, true
}

type parsedStateTransition struct {
	from  string
	to    string
	label string
}

func parseStateTransitions(line string) ([]parsedStateTransition, bool) {
	// Separate optional trailing ": label" from the transition chain.
	// Only consider ':' that appears AFTER at least one arrow operator; a ':'
	// before any arrow belongs to a state description like `S : a -> b`.
	firstArrow, _ := findStateArrow(line)
	if firstArrow < 0 {
		return nil, false
	}
	if firstColon := indexUnquotedByte(line, ':'); firstColon >= 0 && firstColon < firstArrow {
		return nil, false
	}
	chain := line
	label := ""
	if relColon := indexUnquotedByte(line[firstArrow:], ':'); relColon >= 0 {
		colon := firstArrow + relColon
		chain = strings.TrimSpace(line[:colon])
		label = normalizeLabel(line[colon+1:])
	}

	var nodes []string
	cur := chain
	for {
		idx, opLen := findStateArrow(cur)
		if idx < 0 {
			tail := strings.TrimSpace(cur)
			if tail != "" {
				nodes = append(nodes, tail)
			}
			break
		}
		head := strings.TrimSpace(cur[:idx])
		if head != "" {
			nodes = append(nodes, head)
		}
		cur = cur[idx+opLen:]
	}
	if len(nodes) < 2 {
		return nil, false
	}
	out := make([]parsedStateTransition, 0, len(nodes)-1)
	for i := 0; i < len(nodes)-1; i++ {
		lbl := ""
		if i == len(nodes)-2 {
			lbl = label
		}
		out = append(out, parsedStateTransition{
			from:  stripStateClass(nodes[i]),
			to:    stripStateClass(nodes[i+1]),
			label: lbl,
		})
	}
	return out, true
}

func findStateArrow(s string) (idx, opLen int) {
	inQuote := byte(0)
	n := len(s)
	for i := 0; i < n; i++ {
		c := s[i]
		if inQuote != 0 {
			if c == '\\' && i+1 < n {
				i++
				continue
			}
			if c == inQuote {
				inQuote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inQuote = c
			continue
		}
		if c == '-' {
			if i+2 < n && s[i+1] == '-' && s[i+2] == '>' {
				return i, 3
			}
			if i+1 < n && s[i+1] == '>' {
				return i, 2
			}
		}
	}
	return -1, 0
}

type parsedStateDecl struct {
	id         string
	label      string
	hasLabel   bool
	stereotype stateKind
	openBrace  bool
}

func parseStateDecl(rest string) (parsedStateDecl, error) {
	var d parsedStateDecl
	rest = strings.TrimSpace(rest)
	if strings.HasSuffix(rest, "{") {
		d.openBrace = true
		rest = strings.TrimSpace(rest[:len(rest)-1])
	}

	// Check trailing stereotype <<choice>> / <<fork>> / <<join>> or [[choice]] / [[fork]] / [[join]].
	for _, st := range []struct {
		tok  string
		kind stateKind
	}{
		{"<<choice>>", stateKindChoice},
		{"[[choice]]", stateKindChoice},
		{"<<fork>>", stateKindFork},
		{"[[fork]]", stateKindFork},
		{"<<join>>", stateKindJoin},
		{"[[join]]", stateKindJoin},
	} {
		if len(rest) >= len(st.tok) && strings.EqualFold(rest[len(rest)-len(st.tok):], st.tok) {
			d.stereotype = st.kind
			rest = strings.TrimSpace(rest[:len(rest)-len(st.tok)])
			break
		}
	}

	// Check quoted alias: `"Display Label" as id`.
	if strings.HasPrefix(rest, "\"") {
		endQ := findClosingQuote(rest, '"')
		if endQ > 0 {
			lbl := normalizeLabel(rest[1:endQ])
			after := strings.TrimSpace(rest[endQ+1:])
			afterLow := strings.ToLower(after)
			if strings.HasPrefix(afterLow, "as ") {
				id := stripStateClass(strings.TrimSpace(after[len("as "):]))
				if id != "" {
					d.id = id
					d.label = lbl
					d.hasLabel = true
					return d, nil
				}
			}
			if after == "" {
				d.id = lbl
				d.label = lbl
				d.hasLabel = true
				return d, nil
			}
		}
	}

	d.id = stripStateClass(rest)
	if d.id == "" && d.openBrace {
		return d, fmt.Errorf("composite state missing name")
	}
	return d, nil
}

func findClosingQuote(s string, q byte) int {
	for i := 1; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			continue
		}
		if s[i] == q {
			return i
		}
	}
	return -1
}

func indexUnquotedByte(s string, target byte) int {
	inQuote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == inQuote {
				inQuote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inQuote = c
			continue
		}
		if c == target {
			return i
		}
	}
	return -1
}

func stripStateClass(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, ":::"); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	return s
}

// --- Recursive Layout Engine ---

type regionLayout struct {
	w, h   float32
	shapes []ShapeOp
	paths  []PathOp
	labels []LabelOp
}

// layoutItem wraps either a real *stateNode or a virtual dummy waypoint for a
// multi-rank edge.
type layoutItem struct {
	node      *stateNode // nil for virtual dummy waypoint
	dummyEdge int        // index into local edges when node == nil
	isLabel   bool
	rank      int
	order     int
	x, y      float32
	w, h      float32
	leftHalf  float32
	rightHalf float32
	crossPos  float32
}

type layoutEdgeMeta struct {
	edge       *stateEdge
	fromIdx    int
	toIdx      int
	reversed   bool
	synthetic  bool
	dummies    []int // indices into items slice in original edge direction (from -> to)
	labelDummy int   // index into items slice, or -1
}

func layoutCompositeState(comp *stateNode, measure MeasureFunc) {
	var active []*stateRegion
	for _, r := range comp.regions {
		if len(r.nodes) > 0 {
			active = append(active, r)
		}
	}
	tw, th := measure(comp.displayLabel(), stateCompositeFontSize, true)
	comp.titleW, comp.titleH = tw, th
	comp.headerH = max(28, th+12)

	const (
		pad       float32 = 14
		regionGap float32 = 30
	)

	if len(active) == 0 {
		comp.w = max(96, tw+28)
		comp.h = comp.headerH + 36
		return
	}

	horizontal := comp.dir == dirLR || comp.dir == dirRL
	layouts := make([]regionLayout, len(active))
	var maxOpp float32
	for i, r := range active {
		layouts[i] = layoutStateRegion(r, measure)
		if horizontal {
			maxOpp = max(maxOpp, layouts[i].w)
		} else {
			maxOpp = max(maxOpp, layouts[i].h)
		}
	}

	var totalAlong float32
	for i, rl := range layouts {
		if i > 0 {
			totalAlong += regionGap
		}
		if horizontal {
			totalAlong += rl.h
		} else {
			totalAlong += rl.w
		}
	}

	var innerW, innerH float32
	if horizontal {
		innerW, innerH = maxOpp, totalAlong
	} else {
		innerW, innerH = totalAlong, maxOpp
	}

	comp.w = max(tw+28, innerW+2*pad)
	comp.h = comp.headerH + innerH + 2*pad

	// Center inner content horizontally if title made the box wider.
	extraX := (comp.w - 2*pad - innerW) / 2
	var cursor float32
	for i, rl := range layouts {
		if i > 0 {
			sep := cursor + regionGap/2
			if horizontal {
				y := comp.headerH + pad + sep
				comp.innerPaths = append(comp.innerPaths, PathOp{
					Points: [][2]float32{{6, y}, {comp.w - 6, y}},
					Style:  EdgeDashed,
					Role:   RoleFragment,
				})
			} else {
				x := pad + extraX + sep
				comp.innerPaths = append(comp.innerPaths, PathOp{
					Points: [][2]float32{{x, comp.headerH + 6}, {x, comp.h - 6}},
					Style:  EdgeDashed,
					Role:   RoleFragment,
				})
			}
			cursor += regionGap
		}
		var ox, oy float32
		if horizontal {
			ox = pad + extraX + (maxOpp-rl.w)/2
			oy = comp.headerH + pad + cursor
			cursor += rl.h
		} else {
			ox = pad + extraX + cursor
			oy = comp.headerH + pad + (maxOpp-rl.h)/2
			cursor += rl.w
		}
		appendTranslated(&comp.innerShapes, &comp.innerPaths, &comp.innerLabels, rl.shapes, rl.paths, rl.labels, ox, oy)
		shiftRegionAbs(active[i], ox, oy)
	}
	for _, r := range active {
		routeCrossEdges(r, &comp.innerPaths, &comp.innerLabels, measure)
	}
}

func layoutStateRegion(reg *stateRegion, measure MeasureFunc) regionLayout {
	if len(reg.nodes) == 0 {
		return regionLayout{}
	}

	horizontal := reg.dir == dirLR || reg.dir == dirRL
	reversedAxis := reg.dir == dirBT || reg.dir == dirRL

	nodeIdx := make(map[*stateNode]int, len(reg.nodes))
	inDegCount := make([]int, len(reg.nodes))
	outDegCount := make([]int, len(reg.nodes))
	for i, n := range reg.nodes {
		nodeIdx[n] = i
	}
	for _, e := range reg.edges {
		u, okU := nodeIdx[e.from]
		v, okV := nodeIdx[e.to]
		if okU && okV && u != v {
			outDegCount[u]++
			inDegCount[v]++
		}
	}

	// 1. Measure all leaf nodes and recursively lay out composite child nodes.
	for i, n := range reg.nodes {
		switch n.kind {
		case stateKindStart:
			n.w, n.h = 16, 16
		case stateKindEnd:
			n.w, n.h = 18, 18
		case stateKindChoice:
			if n.explicitLabel && n.label != "" {
				lw, lh := measure(n.label, stateFontSize, false)
				n.titleW, n.titleH = lw, lh
				n.w, n.h = max(44, lw*1.4+24), max(36, lh*1.5+20)
			} else {
				n.w, n.h = 28, 28
			}
		case stateKindFork, stateKindJoin:
			deg := max(inDegCount[i], outDegCount[i])
			barLen := max(float32(72), float32(deg)*26)
			if horizontal {
				n.w, n.h = 8, barLen
			} else {
				n.w, n.h = barLen, 8
			}
		case stateKindComposite:
			layoutCompositeState(n, measure)
		default:
			lbl := n.displayLabel()
			if len(n.descriptions) > 0 {
				tw, th := measure(lbl, stateFontSize, true)
				descText := strings.Join(n.descriptions, "\n")
				dw, dh := measure(descText, stateDescFontSize, false)
				n.titleW, n.titleH = tw, th
				n.descW, n.descH = dw, dh
				n.headerH = th + 14
				n.w = max(84, max(tw, dw)+28)
				n.h = n.headerH + dh + 14
			} else {
				lw, lh := measure(lbl, stateFontSize, false)
				n.titleW, n.titleH = lw, lh
				n.w = max(68, lw+30)
				n.h = max(36, lh+18)
			}
		}
	}

	// 2. Measure notes attached to nodes in this region.
	const (
		noteGap   float32 = 18
		noteStack float32 = 8
	)
	notesByNode := make([][]*stateNote, len(reg.nodes))
	leftNoteSpan := make([]float32, len(reg.nodes))
	rightNoteSpan := make([]float32, len(reg.nodes))
	for _, nt := range reg.notes {
		idx, ok := nodeIdx[nt.target]
		if !ok {
			continue
		}
		lw, lh := measure(nt.text, stateNoteFontSize, false)
		nt.lw, nt.lh = lw, lh
		nt.w = max(48, lw+20)
		nt.h = max(26, lh+14)
		notesByNode[idx] = append(notesByNode[idx], nt)
		if nt.side == stateNoteLeft {
			leftNoteSpan[idx] = max(leftNoteSpan[idx], noteGap+nt.w)
		} else {
			rightNoteSpan[idx] = max(rightNoteSpan[idx], noteGap+nt.w)
		}
	}

	// 3. Measure edge labels & build combined edge list (local + synthetic cross deps).
	var edges []layoutEdgeMeta
	for _, e := range reg.edges {
		if e.label != "" {
			lw, lh := measure(e.label, stateEdgeFontSize, false)
			e.labelW, e.labelH = lw+12, lh+6
		}
		u, okU := nodeIdx[e.from]
		v, okV := nodeIdx[e.to]
		if !okU || !okV {
			continue
		}
		edges = append(edges, layoutEdgeMeta{
			edge:       e,
			fromIdx:    u,
			toIdx:      v,
			labelDummy: -1,
		})
	}
	for _, ce := range reg.cross {
		fc := ancestorInRegion(ce.from, reg)
		tc := ancestorInRegion(ce.to, reg)
		if fc == nil || tc == nil || fc == tc {
			continue
		}
		u, okU := nodeIdx[fc]
		v, okV := nodeIdx[tc]
		if okU && okV {
			edges = append(edges, layoutEdgeMeta{
				edge:       ce,
				fromIdx:    u,
				toIdx:      v,
				synthetic:  true,
				labelDummy: -1,
			})
		}
	}

	// 4. DFS cycle removal (visit start nodes first so edges leaving [*] stay forward).
	nCount := len(reg.nodes)
	adj := make([][]int, nCount)
	for ei, em := range edges {
		if em.fromIdx != em.toIdx {
			adj[em.fromIdx] = append(adj[em.fromIdx], ei)
		}
	}
	visited := make([]uint8, nCount)
	var dfs func(int)
	dfs = func(u int) {
		visited[u] = 1
		for _, ei := range adj[u] {
			v := edges[ei].toIdx
			if visited[v] == 1 {
				edges[ei].reversed = true
			} else if visited[v] == 0 {
				dfs(v)
			}
		}
		visited[u] = 2
	}
	for u, n := range reg.nodes {
		if n.kind == stateKindStart && visited[u] == 0 {
			dfs(u)
		}
	}
	for u := range reg.nodes {
		if visited[u] == 0 {
			dfs(u)
		}
	}

	// 5. Longest-path rank assignment on DAG.
	dagOut := make([][]int, nCount)
	dagInCount := make([]int, nCount)
	dagInDeg := make([]int, nCount)
	for _, em := range edges {
		if em.fromIdx == em.toIdx {
			continue
		}
		u, v := em.fromIdx, em.toIdx
		if em.reversed {
			u, v = v, u
		}
		dagOut[u] = append(dagOut[u], v)
		dagInCount[v]++
		dagInDeg[v]++
	}

	nodeRank := make([]int, nCount)
	var q []int
	for u := 0; u < nCount; u++ {
		if dagInCount[u] == 0 {
			q = append(q, u)
		}
	}
	var topo []int
	maxRank := 0
	for len(q) > 0 {
		u := q[0]
		q = q[1:]
		topo = append(topo, u)
		for _, v := range dagOut[u] {
			if nodeRank[v] < nodeRank[u]+1 {
				nodeRank[v] = nodeRank[u] + 1
				maxRank = max(maxRank, nodeRank[v])
			}
			dagInCount[v]--
			if dagInCount[v] == 0 {
				q = append(q, v)
			}
		}
	}

	// Place pure-sink end states [*] on maxRank so terminal branches converge at the end.
	for u, n := range reg.nodes {
		if n.kind == stateKindEnd && len(dagOut[u]) == 0 && dagInDeg[u] > 0 {
			nodeRank[u] = maxRank
		}
	}

	// Reverse-topological tightening: pull nodes toward their successors when doing
	// so does not increase total edge span (len(out) >= len(in)).
	for ti := len(topo) - 1; ti >= 0; ti-- {
		u := topo[ti]
		n := reg.nodes[u]
		if n.kind == stateKindStart || n.kind == stateKindEnd || len(dagOut[u]) == 0 {
			continue
		}
		if len(dagOut[u]) < dagInDeg[u] {
			continue
		}
		minSucc := maxRank
		for _, v := range dagOut[u] {
			minSucc = min(minSucc, nodeRank[v])
		}
		if minSucc-1 > nodeRank[u] {
			nodeRank[u] = minSucc - 1
		}
	}

	// 6. Create layout items: real nodes + virtual dummy waypoints for multi-rank edges.
	items := make([]*layoutItem, nCount)
	for i, n := range reg.nodes {
		items[i] = &layoutItem{
			node:      n,
			dummyEdge: -1,
			rank:      nodeRank[i],
			w:         n.w,
			h:         n.h,
		}
	}

	// Build adjacent-rank DAG for barycenter sweeps and cross-axis alignment.
	var virtAdj [][2]int
	for ei := range edges {
		em := &edges[ei]
		if em.fromIdx == em.toIdx {
			continue
		}
		if em.synthetic {
			// Synthetic edges help ranking only; skip creating virtual lanes for them.
			continue
		}
		u, v := em.fromIdx, em.toIdx
		if em.reversed {
			// Reversed back-edges detour outside the graph rather than threading through ranks.
			continue
		}
		rU, rV := items[u].rank, items[v].rank
		span := rV - rU
		if span <= 1 {
			if span == 1 {
				virtAdj = append(virtAdj, [2]int{u, v})
			}
			continue
		}
		labelRank := rU + span/2
		prev := u
		for r := rU + 1; r < rV; r++ {
			dw, dh := float32(16), float32(16)
			isLbl := false
			if r == labelRank && em.edge.label != "" {
				dw, dh = em.edge.labelW, em.edge.labelH
				isLbl = true
			}
			dIdx := len(items)
			items = append(items, &layoutItem{
				dummyEdge: ei,
				isLabel:   isLbl,
				rank:      r,
				w:         dw,
				h:         dh,
			})
			em.dummies = append(em.dummies, dIdx)
			if isLbl {
				em.labelDummy = dIdx
			}
			virtAdj = append(virtAdj, [2]int{prev, dIdx})
			prev = dIdx
		}
		virtAdj = append(virtAdj, [2]int{prev, v})
	}

	ranks := make([][]int, maxRank+1)
	for i, it := range items {
		ranks[it.rank] = append(ranks[it.rank], i)
	}
	for r := range ranks {
		for ord, idx := range ranks[r] {
			items[idx].order = ord
		}
	}

	preds := make([][]int, len(items))
	succs := make([][]int, len(items))
	for _, pair := range virtAdj {
		u, v := pair[0], pair[1]
		succs[u] = append(succs[u], v)
		preds[v] = append(preds[v], u)
	}

	// 7. Crossing reduction via alternating barycenter sweeps.
	for sweep := 0; sweep < 6; sweep++ {
		if sweep%2 == 0 {
			for r := 1; r <= maxRank; r++ {
				sortStateRankByBarycenter(ranks[r], preds, items)
			}
		} else {
			for r := maxRank - 1; r >= 0; r-- {
				sortStateRankByBarycenter(ranks[r], succs, items)
			}
		}
	}

	// 8. Primary-axis coordinates (rankCenter) & gaps.
	const (
		rankGapBaseV float32 = 48
		rankGapBaseH float32 = 54
		crossGap     float32 = 28
	)
	rankGap := make([]float32, maxRank+1)
	for r := 0; r < maxRank; r++ {
		if horizontal {
			rankGap[r] = rankGapBaseH
		} else {
			rankGap[r] = rankGapBaseV
		}
	}

	// Count forward outgoing/incoming degrees for adjacent-rank label column ownership.
	fwdOut := make([]int, nCount)
	fwdIn := make([]int, nCount)
	for _, em := range edges {
		if em.synthetic || em.reversed || em.fromIdx == em.toIdx {
			continue
		}
		fwdOut[em.fromIdx]++
		fwdIn[em.toIdx]++
	}

	// Track extra cross-axis half-width needed per item so adjacent-rank edge labels
	// centered on a column never collide with sibling columns.
	labelHalf := make([]float32, len(items))
	for _, em := range edges {
		if em.synthetic || em.reversed || em.fromIdx == em.toIdx || em.edge.label == "" {
			continue
		}
		rU, rV := items[em.fromIdx].rank, items[em.toIdx].rank
		if rV == rU+1 {
			if horizontal {
				rankGap[rU] = max(rankGap[rU], em.edge.labelW+40)
			} else {
				rankGap[rU] = max(rankGap[rU], em.edge.labelH+44)
			}
			// Determine which endpoint column carries this edge's label.
			owner := em.toIdx
			if fwdOut[em.fromIdx] == 1 && fwdIn[em.toIdx] > 1 {
				owner = em.fromIdx
			}
			span := em.edge.labelW / 2
			if horizontal {
				span = em.edge.labelH / 2
			}
			labelHalf[owner] = max(labelHalf[owner], span)
		}
	}

	// In LR/RL, notes attached left/right of nodes expand the horizontal gap before/after their rank.
	if horizontal {
		for i := 0; i < nCount; i++ {
			r := items[i].rank
			if leftNoteSpan[i] > 0 {
				if !reversedAxis && r > 0 {
					rankGap[r-1] = max(rankGap[r-1], leftNoteSpan[i]+24)
				} else if reversedAxis && r < maxRank {
					rankGap[r] = max(rankGap[r], leftNoteSpan[i]+24)
				}
			}
			if rightNoteSpan[i] > 0 {
				if !reversedAxis && r < maxRank {
					rankGap[r] = max(rankGap[r], rightNoteSpan[i]+24)
				} else if reversedAxis && r > 0 {
					rankGap[r-1] = max(rankGap[r-1], rightNoteSpan[i]+24)
				}
			}
		}
	}

	rankThick := make([]float32, maxRank+1)
	rankCenter := make([]float32, maxRank+1)
	for r := 0; r <= maxRank; r++ {
		for _, ii := range ranks[r] {
			it := items[ii]
			thick := it.h
			if horizontal {
				thick = it.w
			}
			rankThick[r] = max(rankThick[r], thick)
		}
	}
	var curPrimary float32
	for r := 0; r <= maxRank; r++ {
		rankCenter[r] = curPrimary + rankThick[r]/2
		curPrimary += rankThick[r]
		if r < maxRank {
			curPrimary += rankGap[r]
		}
	}
	totalPrimary := curPrimary
	if reversedAxis {
		for r := 0; r <= maxRank; r++ {
			rankCenter[r] = totalPrimary - rankCenter[r]
		}
	}

	// 9. Cross-axis extents and PAV alignment relaxation.
	for i, it := range items {
		span := it.w
		if horizontal {
			span = it.h
		}
		lh, rh := max(span/2, labelHalf[i]), max(span/2, labelHalf[i])
		if !horizontal && it.node != nil {
			lh = max(lh, span/2+leftNoteSpan[i])
			rh = max(rh, span/2+rightNoteSpan[i])
		}
		it.leftHalf = lh
		it.rightHalf = rh
	}

	// Initial packed placement centered around 0 per rank.
	for r := 0; r <= maxRank; r++ {
		row := ranks[r]
		if len(row) == 0 {
			continue
		}
		var total float32
		for idxPos, ii := range row {
			if idxPos > 0 {
				total += crossGap
			}
			total += items[ii].leftHalf + items[ii].rightHalf
		}
		cur := -total / 2
		for idxPos, ii := range row {
			if idxPos > 0 {
				cur += crossGap
			}
			items[ii].crossPos = cur + items[ii].leftHalf
			cur += items[ii].leftHalf + items[ii].rightHalf
		}
	}

	// Relax cross positions toward connected neighbors using Pool Adjacent Violators.
	// Ending on a top-down pass keeps children (like sink-adjacent states) cleanly
	// aligned under their parents/dummy lanes, then single-node ranks (like [*])
	// center over/under their neighbors.
	for pass := 0; pass < 5; pass++ {
		if pass%2 == 0 {
			for r := 1; r <= maxRank; r++ {
				relaxRankCross(ranks[r], preds, items, crossGap)
			}
		} else {
			for r := maxRank - 1; r >= 0; r-- {
				relaxRankCross(ranks[r], succs, items, crossGap)
			}
		}
	}
	// Align consecutive dummy nodes of each multi-rank edge to a shared straight lane
	// when possible without violating neighbor spacing.
	for _, em := range edges {
		if len(em.dummies) < 2 {
			continue
		}
		var maxLane float32 = -1e9
		for _, di := range em.dummies {
			maxLane = max(maxLane, items[di].crossPos)
		}
		for _, di := range em.dummies {
			r := items[di].rank
			ord := items[di].order
			row := ranks[r]
			if ord+1 < len(row) {
				next := items[row[ord+1]]
				limit := next.crossPos - next.leftHalf - crossGap - items[di].rightHalf
				items[di].crossPos = min(maxLane, limit)
			} else {
				items[di].crossPos = maxLane
			}
		}
	}
	// One final centering pass for root/sink single-node ranks (like [*] start/end).
	for r := 0; r <= maxRank; r++ {
		if len(ranks[r]) == 1 {
			ii := ranks[r][0]
			var nbs []int
			nbs = append(nbs, preds[ii]...)
			nbs = append(nbs, succs[ii]...)
			if len(nbs) > 0 {
				var sum float32
				for _, nb := range nbs {
					sum += items[nb].crossPos
				}
				items[ii].crossPos = sum / float32(len(nbs))
			}
		}
	}

	// Convert (rankCenter, crossPos) to (x, y) coordinates.
	for _, it := range items {
		if horizontal {
			it.x = rankCenter[it.rank] - it.w/2
			it.y = it.crossPos - it.h/2
		} else {
			it.x = it.crossPos - it.w/2
			it.y = rankCenter[it.rank] - it.h/2
		}
		if it.node != nil {
			it.node.x = it.x
			it.node.y = it.y
			it.node.absX = it.x
			it.node.absY = it.y
		}
	}

	// 10. Port allocation for forward edges (sorted by opposite side cross position).
	outLists := make([][]int, nCount)
	inLists := make([][]int, nCount)
	for ei, em := range edges {
		if em.synthetic || em.reversed || em.fromIdx == em.toIdx {
			continue
		}
		outLists[em.fromIdx] = append(outLists[em.fromIdx], ei)
		inLists[em.toIdx] = append(inLists[em.toIdx], ei)
	}

	edgeTargetCross := func(ei int) float32 {
		em := edges[ei]
		if len(em.dummies) > 0 {
			return items[em.dummies[0]].crossPos
		}
		return items[em.toIdx].crossPos
	}
	edgeSourceCross := func(ei int) float32 {
		em := edges[ei]
		if len(em.dummies) > 0 {
			return items[em.dummies[len(em.dummies)-1]].crossPos
		}
		return items[em.fromIdx].crossPos
	}

	startPort := make([][2]float32, len(edges))
	endPort := make([][2]float32, len(edges))
	outPortIdx := make([]int, len(edges))
	outPortTotal := make([]int, len(edges))
	inPortIdx := make([]int, len(edges))
	inPortTotal := make([]int, len(edges))

	dirSign := float32(1)
	if reversedAxis {
		dirSign = -1
	}

	for u := 0; u < nCount; u++ {
		list := outLists[u]
		slices.SortStableFunc(list, func(a, b int) int {
			ca, cb := edgeTargetCross(a), edgeTargetCross(b)
			if ca < cb {
				return -1
			}
			if ca > cb {
				return 1
			}
			return a - b
		})
		n := reg.nodes[u]
		for k, ei := range list {
			outPortIdx[ei] = k
			outPortTotal[ei] = len(list)
			off := statePortOffset(n, horizontal, k, len(list))
			if horizontal {
				px := n.x + n.w
				if reversedAxis {
					px = n.x
				}
				startPort[ei] = [2]float32{px, n.y + n.h/2 + off}
			} else {
				py := n.y + n.h
				if reversedAxis {
					py = n.y
				}
				startPort[ei] = [2]float32{n.x + n.w/2 + off, py}
			}
		}
	}

	for v := 0; v < nCount; v++ {
		list := inLists[v]
		slices.SortStableFunc(list, func(a, b int) int {
			ca, cb := edgeSourceCross(a), edgeSourceCross(b)
			if ca < cb {
				return -1
			}
			if ca > cb {
				return 1
			}
			return a - b
		})
		n := reg.nodes[v]
		for k, ei := range list {
			inPortIdx[ei] = k
			inPortTotal[ei] = len(list)
			off := statePortOffset(n, horizontal, k, len(list))
			if horizontal {
				px := n.x
				if reversedAxis {
					px = n.x + n.w
				}
				endPort[ei] = [2]float32{px, n.y + n.h/2 + off}
			} else {
				py := n.y
				if reversedAxis {
					py = n.y + n.h
				}
				endPort[ei] = [2]float32{n.x + n.w/2 + off, py}
			}
		}
	}

	// 11. Compute outer corridor bounds for back-edge detours.
	rankMaxCross := make([]float32, maxRank+1)
	for r := 0; r <= maxRank; r++ {
		m := float32(-1e9)
		for _, ii := range ranks[r] {
			it := items[ii]
			if horizontal {
				m = max(m, it.y+it.h)
			} else {
				extra := float32(0)
				if ii < nCount {
					extra = rightNoteSpan[ii]
				}
				m = max(m, it.x+it.w+extra)
			}
		}
		if m < -1e8 {
			m = 0
		}
		rankMaxCross[r] = m
	}

	// 12. Emit shapes, paths, and labels.
	var rl regionLayout

	// Route local edges.
	backEdgeCount := 0
	selfLoopSeen := map[int]int{}
	for ei, em := range edges {
		if em.synthetic {
			continue
		}
		u, v := reg.nodes[em.fromIdx], reg.nodes[em.toIdx]

		// Self-loop.
		if em.fromIdx == em.toIdx {
			k := selfLoopSeen[em.fromIdx]
			selfLoopSeen[em.fromIdx] = k + 1
			loopOff := float32(30 + k*18)
			var pts [][2]float32
			var lx, ly float32
			if horizontal {
				cx := u.x + u.w/2
				cy := u.y
				pts = [][2]float32{
					{cx - 8, cy},
					{cx - 20, cy - loopOff},
					{cx + 20, cy - loopOff},
					{cx + 8, cy},
				}
				lx, ly = cx, cy-loopOff-em.edge.labelH/2-4
			} else {
				cx := u.x + u.w
				cy := u.y + u.h/2
				pts = [][2]float32{
					{cx, cy - 8},
					{cx + loopOff, cy - 18},
					{cx + loopOff, cy + 18},
					{cx, cy + 8},
				}
				lx, ly = cx+loopOff+em.edge.labelW/2+6, cy
			}
			rl.paths = append(rl.paths, PathOp{
				Points:    pts,
				Smooth:    true,
				Style:     EdgeSolid,
				EndMarker: MarkerArrow,
				Role:      RoleEdge,
			})
			if em.edge.label != "" {
				rl.labels = append(rl.labels, stateEdgeLabel(em.edge, lx, ly))
			}
			continue
		}

		// Reversed back-edge detour outside intermediate ranks.
		if em.reversed {
			rLo := min(items[em.fromIdx].rank, items[em.toIdx].rank)
			rHi := max(items[em.fromIdx].rank, items[em.toIdx].rank)
			outer := float32(-1e9)
			for r := rLo; r <= rHi; r++ {
				outer = max(outer, rankMaxCross[r])
			}
			corridor := outer + 28 + float32(backEdgeCount)*20
			backEdgeCount++
			var pts [][2]float32
			var lx, ly float32
			if horizontal {
				ucx, vcx := u.x+u.w/2, v.x+v.w/2
				pts = [][2]float32{
					{ucx, u.y + u.h},
					{ucx, corridor},
					{vcx, corridor},
					{vcx, v.y + v.h},
				}
				lx, ly = (ucx+vcx)/2, corridor
			} else {
				ucy, vcy := u.y+u.h/2, v.y+v.h/2
				pts = [][2]float32{
					{u.x + u.w, ucy},
					{corridor, ucy},
					{corridor, vcy},
					{v.x + v.w, vcy},
				}
				lx, ly = corridor, (ucy+vcy)/2
			}
			rl.paths = append(rl.paths, PathOp{
				Points:    pts,
				Smooth:    true,
				Style:     EdgeSolid,
				EndMarker: MarkerArrow,
				Role:      RoleEdge,
			})
			if em.edge.label != "" {
				rl.labels = append(rl.labels, stateEdgeLabel(em.edge, lx, ly))
			}
			continue
		}

		sp, ep := startPort[ei], endPort[ei]
		var pts [][2]float32
		var lx, ly float32

		if len(em.dummies) > 0 {
			// Multi-rank forward edge threading through virtual dummy waypoints.
			pts = append(pts, sp)
			firstD := items[em.dummies[0]]
			if horizontal {
				bendX := sp[0] + dirSign*min(float32(20), abs32(firstD.x+firstD.w/2-sp[0])*0.3)
				if abs32(sp[1]-(firstD.y+firstD.h/2)) >= 1 {
					pts = append(pts, [2]float32{bendX, sp[1]}, [2]float32{bendX, firstD.y + firstD.h/2})
				}
				for idxD, di := range em.dummies {
					d := items[di]
					dcx, dcy := d.x+d.w/2, d.y+d.h/2
					if idxD > 0 {
						prev := items[em.dummies[idxD-1]]
						pcx, pcy := prev.x+prev.w/2, prev.y+prev.h/2
						if abs32(pcy-dcy) >= 1 {
							mx := (pcx + dcx) / 2
							pts = append(pts, [2]float32{mx, pcy}, [2]float32{mx, dcy})
						}
					}
					pts = append(pts, [2]float32{dcx, dcy})
				}
				lastD := items[em.dummies[len(em.dummies)-1]]
				if abs32((lastD.y+lastD.h/2)-ep[1]) >= 1 {
					bendX2 := ep[0] - dirSign*min(float32(20), abs32(ep[0]-(lastD.x+lastD.w/2))*0.3)
					pts = append(pts, [2]float32{bendX2, lastD.y + lastD.h/2}, [2]float32{bendX2, ep[1]})
				}
			} else {
				bendY := sp[1] + dirSign*min(float32(20), abs32(firstD.y+firstD.h/2-sp[1])*0.3)
				if abs32(sp[0]-(firstD.x+firstD.w/2)) >= 1 {
					pts = append(pts, [2]float32{sp[0], bendY}, [2]float32{firstD.x + firstD.w/2, bendY})
				}
				for idxD, di := range em.dummies {
					d := items[di]
					dcx, dcy := d.x+d.w/2, d.y+d.h/2
					if idxD > 0 {
						prev := items[em.dummies[idxD-1]]
						pcx, pcy := prev.x+prev.w/2, prev.y+prev.h/2
						if abs32(pcx-dcx) >= 1 {
							my := (pcy + dcy) / 2
							pts = append(pts, [2]float32{pcx, my}, [2]float32{dcx, my})
						}
					}
					pts = append(pts, [2]float32{dcx, dcy})
				}
				lastD := items[em.dummies[len(em.dummies)-1]]
				if abs32((lastD.x+lastD.w/2)-ep[0]) >= 1 {
					bendY2 := ep[1] - dirSign*min(float32(20), abs32(ep[1]-(lastD.y+lastD.h/2))*0.3)
					pts = append(pts, [2]float32{lastD.x + lastD.w/2, bendY2}, [2]float32{ep[0], bendY2})
				}
			}
			pts = append(pts, ep)
			if em.labelDummy >= 0 {
				ld := items[em.labelDummy]
				lx, ly = ld.x+ld.w/2, ld.y+ld.h/2
			} else {
				lx, ly = polylineMidpoint(pts)
			}
		} else {
			// Adjacent-rank forward edge (or same-rank fallback).
			pts, lx, ly = routeAdjacentStateEdge(
				sp, ep, horizontal, dirSign,
				fwdOut[em.fromIdx], fwdIn[em.toIdx],
				outPortIdx[ei], outPortTotal[ei],
				inPortIdx[ei], inPortTotal[ei],
			)
		}

		pts = compactPolyline(pts)
		rl.paths = append(rl.paths, PathOp{
			Points:    pts,
			Smooth:    true,
			Style:     EdgeSolid,
			EndMarker: MarkerArrow,
			Role:      RoleEdge,
		})
		if em.edge.label != "" {
			rl.labels = append(rl.labels, stateEdgeLabel(em.edge, lx, ly))
		}
	}

	// Emit nodes (composite interiors + leaf shapes & labels).
	for i, n := range reg.nodes {
		switch n.kind {
		case stateKindStart:
			rl.shapes = append(rl.shapes, ShapeOp{
				Kind: ShapeStartState,
				Role: RoleNode,
				X:    n.x,
				Y:    n.y,
				W:    n.w,
				H:    n.h,
			})
		case stateKindEnd:
			rl.shapes = append(rl.shapes, ShapeOp{
				Kind: ShapeEndState,
				Role: RoleNode,
				X:    n.x,
				Y:    n.y,
				W:    n.w,
				H:    n.h,
			})
		case stateKindChoice:
			rl.shapes = append(rl.shapes, ShapeOp{
				Kind: ShapeDiamond,
				Role: RoleNode,
				X:    n.x,
				Y:    n.y,
				W:    n.w,
				H:    n.h,
			})
			if n.explicitLabel && n.label != "" {
				rl.labels = append(rl.labels, LabelOp{
					Text:  n.label,
					X:     n.x + (n.w-n.titleW)/2,
					Y:     n.y + (n.h-n.titleH)/2,
					W:     n.titleW,
					H:     n.titleH,
					Size:  stateFontSize,
					Align: 0,
					Role:  RoleNode,
				})
			}
		case stateKindFork, stateKindJoin:
			rl.shapes = append(rl.shapes, ShapeOp{
				Kind: ShapeBar,
				Role: RoleNode,
				X:    n.x,
				Y:    n.y,
				W:    n.w,
				H:    n.h,
			})
		case stateKindComposite:
			rl.shapes = append(rl.shapes, ShapeOp{
				Kind:     ShapeRound,
				Role:     RoleSubgraph,
				X:        n.x,
				Y:        n.y,
				W:        n.w,
				H:        n.h,
				Radius:   10,
				DividerY: n.headerH,
			})
			rl.labels = append(rl.labels, LabelOp{
				Text:  n.displayLabel(),
				X:     n.x + (n.w-n.titleW)/2,
				Y:     n.y + (n.headerH-n.titleH)/2,
				W:     n.titleW,
				H:     n.titleH,
				Size:  stateCompositeFontSize,
				Bold:  true,
				Align: 0,
				Role:  RoleSubgraph,
			})
			appendTranslated(&rl.shapes, &rl.paths, &rl.labels, n.innerShapes, n.innerPaths, n.innerLabels, n.x, n.y)
			for _, cr := range n.regions {
				shiftRegionAbs(cr, n.x, n.y)
			}
		default:
			if len(n.descriptions) > 0 {
				rl.shapes = append(rl.shapes, ShapeOp{
					Kind:     ShapeRound,
					Role:     RoleNode,
					X:        n.x,
					Y:        n.y,
					W:        n.w,
					H:        n.h,
					Radius:   8,
					DividerY: n.headerH,
				})
				rl.labels = append(rl.labels, LabelOp{
					Text:  n.displayLabel(),
					X:     n.x + (n.w-n.titleW)/2,
					Y:     n.y + (n.headerH-n.titleH)/2,
					W:     n.titleW,
					H:     n.titleH,
					Size:  stateFontSize,
					Bold:  true,
					Align: 0,
					Role:  RoleNode,
				})
				rl.labels = append(rl.labels, LabelOp{
					Text:  strings.Join(n.descriptions, "\n"),
					X:     n.x + 14,
					Y:     n.y + n.headerH + 7,
					W:     n.w - 28,
					H:     n.descH,
					Size:  stateDescFontSize,
					Align: -1,
					Role:  RoleNode,
				})
			} else {
				rl.shapes = append(rl.shapes, ShapeOp{
					Kind:   ShapeRound,
					Role:   RoleNode,
					X:      n.x,
					Y:      n.y,
					W:      n.w,
					H:      n.h,
					Radius: 8,
				})
				rl.labels = append(rl.labels, LabelOp{
					Text:  n.displayLabel(),
					X:     n.x + (n.w-n.titleW)/2,
					Y:     n.y + (n.h-n.titleH)/2,
					W:     n.titleW,
					H:     n.titleH,
					Size:  stateFontSize,
					Align: 0,
					Role:  RoleNode,
				})
			}
		}

		// Emit attached notes for this node.
		if len(notesByNode[i]) > 0 {
			var leftNotes, rightNotes []*stateNote
			for _, nt := range notesByNode[i] {
				if nt.side == stateNoteLeft {
					leftNotes = append(leftNotes, nt)
				} else {
					rightNotes = append(rightNotes, nt)
				}
			}
			emitSideNotes := func(list []*stateNote, left bool) {
				if len(list) == 0 {
					return
				}
				var totalH float32
				for k, nt := range list {
					if k > 0 {
						totalH += noteStack
					}
					totalH += nt.h
				}
				ny := n.y + (n.h-totalH)/2
				for _, nt := range list {
					var nx, ax, bx float32
					if left {
						nx = n.x - noteGap - nt.w
						ax, bx = nx+nt.w, n.x
					} else {
						nx = n.x + n.w + noteGap
						ax, bx = n.x+n.w, nx
					}
					cy := ny + nt.h/2
					attachY := max(n.y+6, min(n.y+n.h-6, cy))
					rl.paths = append(rl.paths, PathOp{
						Points: [][2]float32{{ax, attachY}, {bx, cy}},
						Style:  EdgeDashed,
						Role:   RoleEdge,
					})
					rl.shapes = append(rl.shapes, ShapeOp{
						Kind:   ShapeRect,
						Role:   RoleNote,
						X:      nx,
						Y:      ny,
						W:      nt.w,
						H:      nt.h,
						Radius: 4,
					})
					rl.labels = append(rl.labels, LabelOp{
						Text:  nt.text,
						X:     nx + (nt.w-nt.lw)/2,
						Y:     ny + (nt.h-nt.lh)/2,
						W:     nt.lw,
						H:     nt.lh,
						Size:  stateNoteFontSize,
						Align: 0,
						Role:  RoleNote,
					})
					ny += nt.h + noteStack
				}
			}
			emitSideNotes(leftNotes, true)
			emitSideNotes(rightNotes, false)
		}
	}

	// Normalize region coordinates so its top-left bounding box starts at (0, 0).
	minX, minY, maxX, maxY := primitivesBounds(rl.shapes, rl.paths, rl.labels)
	if minX != 0 || minY != 0 {
		shiftPrimitives(rl.shapes, rl.paths, rl.labels, -minX, -minY)
		shiftRegionAbs(reg, -minX, -minY)
		maxX -= minX
		maxY -= minY
	}
	rl.w = max(1, maxX)
	rl.h = max(1, maxY)
	return rl
}

func routeCrossEdges(reg *stateRegion, paths *[]PathOp, labels *[]LabelOp, measure MeasureFunc) {
	if len(reg.cross) == 0 {
		return
	}
	horizontal := reg.dir == dirLR || reg.dir == dirRL
	for _, e := range reg.cross {
		if e.label != "" && e.labelW == 0 {
			lw, lh := measure(e.label, stateEdgeFontSize, false)
			e.labelW, e.labelH = lw+12, lh+6
		}
		u, v := e.from, e.to
		ucx, ucy := u.absX+u.w/2, u.absY+u.h/2
		vcx, vcy := v.absX+v.w/2, v.absY+v.h/2

		var pts [][2]float32
		if horizontal {
			var sx, tx float32
			if vcx >= ucx {
				sx, tx = u.absX+u.w, v.absX
			} else {
				sx, tx = u.absX, v.absX+v.w
			}
			mx := (sx + tx) / 2
			pts = [][2]float32{{sx, ucy}, {mx, ucy}, {mx, vcy}, {tx, vcy}}
		} else {
			var sy, ty float32
			if vcy >= ucy {
				sy, ty = u.absY+u.h, v.absY
			} else {
				sy, ty = u.absY, v.absY+v.h
			}
			my := (sy + ty) / 2
			pts = [][2]float32{{ucx, sy}, {ucx, my}, {vcx, my}, {vcx, ty}}
		}
		pts = compactPolyline(pts)
		*paths = append(*paths, PathOp{
			Points:    pts,
			Smooth:    true,
			Style:     EdgeSolid,
			EndMarker: MarkerArrow,
			Role:      RoleEdge,
		})
		if e.label != "" {
			lx, ly := polylineMidpoint(pts)
			*labels = append(*labels, stateEdgeLabel(e, lx, ly))
		}
	}
	// Avoid routing the same cross edges twice if called on root.
	reg.cross = nil
}

func routeAdjacentStateEdge(
	sp, ep [2]float32,
	horizontal bool,
	dirSign float32,
	outDeg, inDeg int,
	outIdx, outTotal int,
	inIdx, inTotal int,
) (pts [][2]float32, lx, ly float32) {
	if horizontal {
		if abs32(sp[1]-ep[1]) < 1 {
			return [][2]float32{sp, ep}, (sp[0] + ep[0]) / 2, sp[1]
		}
		spanX := abs32(ep[0] - sp[0])
		if outDeg == 1 && inDeg > 1 {
			// Converging fan-in: stay horizontal at source Y, bend near target X.
			stagger := sideStagger(inIdx, inTotal, sp[1] < ep[1])
			bendX := ep[0] - dirSign*min(max(float32(14), spanX*0.26), 16+stagger*7)
			pts = [][2]float32{sp, {bendX, sp[1]}, {bendX, ep[1]}, ep}
			return pts, (sp[0] + bendX) / 2, sp[1]
		}
		// Fan-out or 1-to-1: bend near source X, then run horizontally at target Y.
		stagger := sideStagger(outIdx, outTotal, ep[1] < sp[1])
		bendX := sp[0] + dirSign*min(max(float32(14), spanX*0.26), 16+stagger*7)
		pts = [][2]float32{sp, {bendX, sp[1]}, {bendX, ep[1]}, ep}
		return pts, (bendX + ep[0]) / 2, ep[1]
	}

	if abs32(sp[0]-ep[0]) < 1 {
		return [][2]float32{sp, ep}, sp[0], (sp[1] + ep[1]) / 2
	}
	spanY := abs32(ep[1] - sp[1])
	if outDeg == 1 && inDeg > 1 {
		// Converging fan-in: drop straight through label under source, bend near target.
		stagger := sideStagger(inIdx, inTotal, sp[0] < ep[0])
		bendY := ep[1] - dirSign*min(max(float32(14), spanY*0.28), 16+stagger*7)
		pts = [][2]float32{sp, {sp[0], bendY}, {ep[0], bendY}, ep}
		return pts, sp[0], (sp[1] + bendY) / 2
	}
	// Fan-out or 1-to-1: bend near source, then drop vertically through label into target.
	stagger := sideStagger(outIdx, outTotal, ep[0] < sp[0])
	bendY := sp[1] + dirSign*min(max(float32(14), spanY*0.28), 16+stagger*7)
	pts = [][2]float32{sp, {sp[0], bendY}, {ep[0], bendY}, ep}
	return pts, ep[0], (bendY + ep[1]) / 2
}

// sideStagger returns an ordering index (0 for outermost edge turning toward a side,
// increasing inward) so parallel bends toward the same side never overlap.
func sideStagger(idx, total int, goingNeg bool) float32 {
	if total <= 1 {
		return 0
	}
	if goingNeg {
		return float32(idx)
	}
	return float32(total - 1 - idx)
}

func statePortOffset(n *stateNode, horizontal bool, k, total int) float32 {
	if total <= 1 {
		return 0
	}
	// Tiny circular start/end pseudostates converge cleanly at center.
	if n.kind == stateKindStart || n.kind == stateKindEnd || n.kind == stateKindChoice {
		return 0
	}
	sideLen := n.w
	if horizontal {
		sideLen = n.h
	}
	usable := max(0, min(sideLen-20, sideLen*0.60))
	if n.kind == stateKindFork || n.kind == stateKindJoin {
		usable = max(0, sideLen-16)
	}
	step := usable / float32(total-1)
	step = min(step, 28)
	return (float32(k) - float32(total-1)/2) * step
}

func stateEdgeLabel(e *stateEdge, cx, cy float32) LabelOp {
	return LabelOp{
		Text:  e.label,
		X:     cx - e.labelW/2,
		Y:     cy - e.labelH/2,
		W:     e.labelW,
		H:     e.labelH,
		Size:  stateEdgeFontSize,
		Align: 0,
		Role:  RoleEdgeLabel,
		Pill:  true,
	}
}

func sortStateRankByBarycenter(row []int, neighbors [][]int, items []*layoutItem) {
	if len(row) <= 1 {
		return
	}
	bary := make(map[int]float64, len(row))
	for _, ii := range row {
		ns := neighbors[ii]
		if len(ns) == 0 {
			bary[ii] = float64(items[ii].order)
			continue
		}
		sum := 0.0
		for _, nb := range ns {
			sum += float64(items[nb].order)
		}
		bary[ii] = sum / float64(len(ns))
	}
	slices.SortStableFunc(row, func(a, b int) int {
		if bary[a] < bary[b] {
			return -1
		}
		if bary[a] > bary[b] {
			return 1
		}
		return items[a].order - items[b].order
	})
	for ord, ii := range row {
		items[ii].order = ord
	}
}

// relaxRankCross aligns each item in row toward the average cross position of
// its neighbors while strictly preserving rank order and minimum spacing via
// Pool Adjacent Violators (exact L2 projection onto the non-overlapping cone).
func relaxRankCross(row []int, neighbors [][]int, items []*layoutItem, gap float32) {
	k := len(row)
	if k == 0 {
		return
	}
	target := make([]float32, k)
	for i, ii := range row {
		ns := neighbors[ii]
		if len(ns) == 0 {
			target[i] = items[ii].crossPos
			continue
		}
		var sum float32
		for _, nb := range ns {
			sum += items[nb].crossPos
		}
		target[i] = sum / float32(len(ns))
	}
	if k == 1 {
		items[row[0]].crossPos = target[0]
		return
	}

	offset := make([]float32, k)
	for i := 1; i < k; i++ {
		prev, cur := items[row[i-1]], items[row[i]]
		offset[i] = offset[i-1] + prev.rightHalf + gap + cur.leftHalf
	}

	type pavBlock struct {
		sum   float32
		count int
	}
	blocks := make([]pavBlock, 0, k)
	for i := 0; i < k; i++ {
		blocks = append(blocks, pavBlock{sum: target[i] - offset[i], count: 1})
		for len(blocks) >= 2 {
			a := &blocks[len(blocks)-2]
			b := &blocks[len(blocks)-1]
			if a.sum/float32(a.count) <= b.sum/float32(b.count) {
				break
			}
			a.sum += b.sum
			a.count += b.count
			blocks = blocks[:len(blocks)-1]
		}
	}

	idx := 0
	for _, blk := range blocks {
		val := blk.sum / float32(blk.count)
		for j := 0; j < blk.count; j++ {
			items[row[idx]].crossPos = val + offset[idx]
			idx++
		}
	}
}

func compactPolyline(pts [][2]float32) [][2]float32 {
	if len(pts) <= 2 {
		return pts
	}
	out := make([][2]float32, 0, len(pts))
	for _, pt := range pts {
		if n := len(out); n > 0 && abs32(out[n-1][0]-pt[0]) < 0.5 && abs32(out[n-1][1]-pt[1]) < 0.5 {
			continue
		}
		// Drop collinear middle points on axis-aligned segments.
		if n := len(out); n >= 2 {
			a, b := out[n-2], out[n-1]
			sameX := abs32(a[0]-b[0]) < 0.5 && abs32(b[0]-pt[0]) < 0.5
			sameY := abs32(a[1]-b[1]) < 0.5 && abs32(b[1]-pt[1]) < 0.5
			if sameX || sameY {
				out[n-1] = pt
				continue
			}
		}
		out = append(out, pt)
	}
	if len(out) < 2 && len(pts) >= 2 {
		return [][2]float32{pts[0], pts[len(pts)-1]}
	}
	return out
}

func appendTranslated(
	dstShapes *[]ShapeOp, dstPaths *[]PathOp, dstLabels *[]LabelOp,
	srcShapes []ShapeOp, srcPaths []PathOp, srcLabels []LabelOp,
	dx, dy float32,
) {
	for _, s := range srcShapes {
		s.X += dx
		s.Y += dy
		*dstShapes = append(*dstShapes, s)
	}
	for _, p := range srcPaths {
		pts := make([][2]float32, len(p.Points))
		for i, pt := range p.Points {
			pts[i] = [2]float32{pt[0] + dx, pt[1] + dy}
		}
		p.Points = pts
		*dstPaths = append(*dstPaths, p)
	}
	for _, l := range srcLabels {
		l.X += dx
		l.Y += dy
		*dstLabels = append(*dstLabels, l)
	}
}

func shiftRegionAbs(reg *stateRegion, dx, dy float32) {
	for _, n := range reg.nodes {
		n.absX += dx
		n.absY += dy
		for _, cr := range n.regions {
			shiftRegionAbs(cr, dx, dy)
		}
	}
}

func primitivesBounds(shapes []ShapeOp, paths []PathOp, labels []LabelOp) (minX, minY, maxX, maxY float32) {
	minX, minY = float32(1e9), float32(1e9)
	maxX, maxY = float32(-1e9), float32(-1e9)
	has := false
	for _, s := range shapes {
		minX = min(minX, s.X)
		minY = min(minY, s.Y)
		maxX = max(maxX, s.X+s.W)
		maxY = max(maxY, s.Y+s.H)
		has = true
	}
	for _, p := range paths {
		for _, pt := range p.Points {
			minX = min(minX, pt[0])
			minY = min(minY, pt[1])
			maxX = max(maxX, pt[0])
			maxY = max(maxY, pt[1])
			has = true
		}
	}
	for _, lb := range labels {
		minX = min(minX, lb.X)
		minY = min(minY, lb.Y)
		maxX = max(maxX, lb.X+lb.W)
		maxY = max(maxY, lb.Y+lb.H)
		has = true
	}
	if !has {
		return 0, 0, 0, 0
	}
	return minX, minY, maxX, maxY
}

func shiftPrimitives(shapes []ShapeOp, paths []PathOp, labels []LabelOp, dx, dy float32) {
	for i := range shapes {
		shapes[i].X += dx
		shapes[i].Y += dy
	}
	for i := range paths {
		for j := range paths[i].Points {
			paths[i].Points[j][0] += dx
			paths[i].Points[j][1] += dy
		}
	}
	for i := range labels {
		labels[i].X += dx
		labels[i].Y += dy
	}
}

func normalizeDiagramBounds(diag *Diagram, pad float32) {
	minX, minY, maxX, maxY := primitivesBounds(diag.Shapes, diag.Paths, diag.Labels)
	dx, dy := pad-minX, pad-minY
	shiftPrimitives(diag.Shapes, diag.Paths, diag.Labels, dx, dy)
	diag.W = max(1, (maxX-minX)+2*pad)
	diag.H = max(1, (maxY-minY)+2*pad)
}
