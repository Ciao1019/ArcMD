package mermaid

import (
	"math"
	"strings"
	"testing"
)

func assertDiagramBounds(t *testing.T, d Diagram) {
	t.Helper()
	if d.W <= 0 || d.H <= 0 {
		t.Fatalf("expected positive diagram dimensions, got %vx%v", d.W, d.H)
	}
	for i, s := range d.Shapes {
		if math.IsNaN(float64(s.X)) || math.IsNaN(float64(s.Y)) || s.X < -0.5 || s.Y < -0.5 || s.X+s.W > d.W+0.5 || s.Y+s.H > d.H+0.5 {
			t.Fatalf("shape[%d] out of bounds (%vx%v): %+v", i, d.W, d.H, s)
		}
		if s.W <= 0 || s.H <= 0 {
			t.Fatalf("shape[%d] non-positive size: %+v", i, s)
		}
	}
	for i, p := range d.Paths {
		if len(p.Points) < 2 {
			t.Fatalf("path[%d] has fewer than 2 points: %+v", i, p)
		}
		for _, pt := range p.Points {
			if pt[0] < -0.5 || pt[1] < -0.5 || pt[0] > d.W+0.5 || pt[1] > d.H+0.5 {
				t.Fatalf("path[%d] point %v out of bounds (%vx%v)", i, pt, d.W, d.H)
			}
		}
	}
	for i, lb := range d.Labels {
		if lb.X < -0.5 || lb.Y < -0.5 || lb.X+lb.W > d.W+0.5 || lb.Y+lb.H > d.H+0.5 {
			t.Fatalf("label[%d] %q out of bounds (%vx%v): %+v", i, lb.Text, d.W, d.H, lb)
		}
	}
}

func TestFlowchartLayoutsAndEdgeLabels(t *testing.T) {
	src := `
%%{init: {'theme':'base'}}%%
flowchart LR
	A([Start]) -- Init --> B{Ready?}
	B --Yes--> C[(Database)]
	B -.Maybe.-> D[[Subroutine]]
	C ==>|Commit| E((End))
	D ==Fast==> F[Notify]
	F --> E
	C -.-> A %% back edge cycle
	D --> D
	subgraph Core[Core Services]
		direction LR
		C
		D
	end
`
	d, err := ParseAndLayout(src, nil)
	if err != nil {
		t.Fatalf("ParseAndLayout failed: %v", err)
	}
	if d.Kind != "flowchart" {
		t.Fatalf("Kind = %q, want flowchart", d.Kind)
	}
	assertDiagramBounds(t, d)

	var shapes = map[ShapeKind]int{}
	for _, s := range d.Shapes {
		shapes[s.Kind]++
	}
	for _, want := range []ShapeKind{ShapeStadium, ShapeDiamond, ShapeCylinder, ShapeSubroutine, ShapeCircle, ShapeRect, ShapeRound} {
		if shapes[want] == 0 {
			t.Fatalf("expected shape kind %v to be present; counts=%v", want, shapes)
		}
	}

	labelsSeen := map[string]bool{}
	for _, lb := range d.Labels {
		labelsSeen[lb.Text] = true
	}
	for _, want := range []string{"Start", "Ready?", "Database", "Subroutine", "End", "Notify", "Init", "Yes", "Maybe", "Commit", "Fast", "Core Services"} {
		if !labelsSeen[want] {
			t.Fatalf("missing label %q in %v", want, labelsSeen)
		}
	}

	// Ensure nodes don't overlap each other.
	var nodeBoxes []ShapeOp
	for _, s := range d.Shapes {
		if s.Role == RoleNode {
			nodeBoxes = append(nodeBoxes, s)
		}
	}
	if len(nodeBoxes) != 6 {
		t.Fatalf("expected 6 node shapes, got %d", len(nodeBoxes))
	}
	for i := 0; i < len(nodeBoxes); i++ {
		for j := i + 1; j < len(nodeBoxes); j++ {
			a, b := nodeBoxes[i], nodeBoxes[j]
			overlapX := a.X < b.X+b.W && b.X < a.X+a.W
			overlapY := a.Y < b.Y+b.H && b.Y < a.Y+a.H
			if overlapX && overlapY {
				t.Fatalf("overlapping nodes %+v and %+v", a, b)
			}
		}
	}
}

func TestFlowchartAmpersandAndInlineMinusLinks(t *testing.T) {
	src := `graph TD
A[Input] --x B[Reject]
A --> C & D
C & D --> E{Merge<br/>Gate}`
	d, err := ParseAndLayout(src, nil)
	if err != nil {
		t.Fatalf("ParseAndLayout failed: %v", err)
	}
	assertDiagramBounds(t, d)

	var foundCross, foundMultiline bool
	for _, p := range d.Paths {
		if p.EndMarker == MarkerCross {
			foundCross = true
		}
	}
	for _, lb := range d.Labels {
		if strings.Contains(lb.Text, "Merge\nGate") {
			foundMultiline = true
		}
	}
	if !foundCross {
		t.Fatalf("expected MarkerCross on --x edge")
	}
	if !foundMultiline {
		t.Fatalf("expected multiline newline label for <br/>")
	}
}

func TestSequenceDiagramFeaturesAndLayout(t *testing.T) {
	src := `
sequenceDiagram
	autonumber
	actor U as User App
	participant API as Gateway
	participant DB as Storage

	U->>+API: POST /v1/notes (very long payload label to widen span)
	Note right of U: Client holds token
	alt Cache hit
		API-->>U: 200 OK (Cached)
	else Cache miss
		API->>+DB: SELECT * FROM notes
		Note over API,DB: Transactional read
		DB-->>-API: Rows
		API->>API: Serialize response
		API-->>-U: 200 OK
	end
	loop Every 30s
		U->API: Heartbeat
	end
`
	d, err := ParseAndLayout(src, nil)
	if err != nil {
		t.Fatalf("ParseAndLayout sequence failed: %v", err)
	}
	if d.Kind != "sequence" {
		t.Fatalf("Kind = %q, want sequence", d.Kind)
	}
	assertDiagramBounds(t, d)

	var actors, notes, activations, pentagons, frags int
	for _, s := range d.Shapes {
		switch s.Role {
		case RoleActor:
			actors++
		case RoleNote:
			notes++
		case RoleActivation:
			activations++
		case RoleFragment:
			frags++
			if s.Kind == ShapePentagon {
				pentagons++
			}
		}
	}
	// 3 participants drawn at both top and bottom => 6 ShapeOp with RoleActor.
	if actors != 6 {
		t.Fatalf("expected 6 actor/participant shapes (top+bottom), got %d", actors)
	}
	if notes != 2 {
		t.Fatalf("expected 2 note boxes, got %d", notes)
	}
	// API activated by ->>+ and DB activated by ->>+ => 2 activation bars.
	if activations != 2 {
		t.Fatalf("expected 2 activation bars, got %d", activations)
	}
	if pentagons != 2 || frags < 4 {
		t.Fatalf("expected 2 fragment pentagons (alt, loop) and boxes, got pentagons=%d frags=%d", pentagons, frags)
	}

	var numbered bool
	for _, lb := range d.Labels {
		if strings.HasPrefix(lb.Text, "1. POST /v1/notes") {
			numbered = true
		}
	}
	if !numbered {
		t.Fatalf("expected autonumber prefix on first message")
	}

	// Verify User-to-Gateway span expanded enough for the long message label.
	var actorCenters []float32
	for _, s := range d.Shapes {
		if s.Role == RoleActor {
			actorCenters = append(actorCenters, s.X+s.W/2)
		}
	}
	userX, apiX := actorCenters[0], actorCenters[2]
	if span := apiX - userX; span < 300 {
		t.Fatalf("expected wide span between User and Gateway for long label, got %.1f", span)
	}
}

func TestSequenceExplicitActivateAndParErrors(t *testing.T) {
	okSrc := `sequenceDiagram
	A->>B: Ping
	activate B
	par Background
		B-->>C: Log
	and Notify
		B-->>D: Webhook
	end
	deactivate B`
	d, err := ParseAndLayout(okSrc, nil)
	if err != nil {
		t.Fatalf("valid par sequence failed: %v", err)
	}
	assertDiagramBounds(t, d)

	var actCount int
	for _, s := range d.Shapes {
		if s.Role == RoleActivation {
			actCount++
		}
	}
	if actCount != 1 {
		t.Fatalf("expected 1 explicit activation, got %d", actCount)
	}

	for _, bad := range []struct {
		name string
		src  string
	}{
		{"empty", "   \n%% only comment\n"},
		{"unsupported", "gantt\n title X"},
		{"empty_seq", "sequenceDiagram\n"},
		{"unclosed_loop", "sequenceDiagram\nloop Retry\nA->>B: Try"},
		{"extra_end", "sequenceDiagram\nA->>B: Hi\nend"},
	} {
		t.Run(bad.name, func(t *testing.T) {
			if _, err := ParseAndLayout(bad.src, nil); err == nil {
				t.Fatalf("expected error for %s, got nil", bad.name)
			}
		})
	}
}

func TestComplexMultiPhaseSequenceCompactWidthAndRectColors(t *testing.T) {
	src := `sequenceDiagram
  participant User as Client
  participant CoreSvc as CoreService
  participant Gateway as Gateway
  participant MQ as MessageQueue
  participant Worker as RemoteWorker

  rect rgb(200,250,200)
  note over User,CoreSvc: Authoring Phase
    User ->> CoreSvc: 1.1 Create item
    CoreSvc ->> CoreSvc: 1.2 Attach resources / configure parameters
    User ->> CoreSvc: 1.3 Save draft

    User ->> CoreSvc: 2.1 Submit for review
    alt Restricted pattern detected
      ScriptSvc ->> User: 2.2 Reject publication
    else Passed check
      CoreSvc ->> User: 2.3 Publish succeeded
    end
  end
  rect rgb(200,200,250)
    note over User,Worker: Testing Phase
    User ->> CoreSvc: 3.1 Trigger test run
    CoreSvc ->> Gateway: 3.2 RPC invoke
    Gateway ->> Worker: 3.3 HTTP request
    Worker -->> Worker: 3.4 Execute asynchronously
    Worker ->> Gateway: 3.5 RPC response
    Gateway ->> MQ: 3.6 Publish test result
    MQ -->> CoreSvc: 3.7 Consume message
    CoreSvc ->> CoreSvc: 3.8 Update test status
    CoreSvc ->> User: 3.9 View test result in monitor
  end
  rect rgb(134, 190, 220)
    note over User,CoreSvc: Release Phase
    User ->> CoreSvc: 4.1 Export bundle
    CoreSvc ->> User: 4.2 Download archive
    User ->> CoreSvc: 4.3 Import bundle to target environment
    CoreSvc ->> CoreSvc: 4.4 Parse, persist, and auto-publish
  end
  rect rgb(200,250,250)
    note over User,Worker: Production Execution Phase
    User ->> CoreSvc: 5.1 Submit execution job
    CoreSvc ->> Gateway: 5.2 RPC invoke
    Gateway ->> Worker: 5.3 HTTP request
    Worker ->> Worker: 5.4 Execute asynchronously
    Worker ->> Gateway: 5.5 RPC response
    Gateway ->> MQ: 5.6 Publish job result
    MQ -->> CoreSvc: 5.7 Consume message
    CoreSvc ->> CoreSvc: 5.8 Update job status
    CoreSvc ->> User: 5.9 View execution result in monitor
  end`

	d, err := ParseAndLayout(src, nil)
	if err != nil {
		t.Fatalf("ParseAndLayout failed: %v", err)
	}
	assertDiagramBounds(t, d)

	// Wrapping self-loops and adjacent labels keeps the 6-participant diagram compact.
	if d.W > 1050 {
		t.Fatalf("expected compact diagram width <= 1050 for 6 participants, got %.1f", d.W)
	}
	if d.H < 1200 {
		t.Fatalf("expected full multi-phase vertical height >= 1200, got %.1f", d.H)
	}
	coloredRects := 0
	for _, s := range d.Shapes {
		if s.Role == RoleFragment && s.Fill[3] > 0 {
			coloredRects++
		}
	}
	if coloredRects != 4 {
		t.Fatalf("expected 4 colored rect phases, got %d", coloredRects)
	}
}
