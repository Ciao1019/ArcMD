package mermaid

import (
	"strings"
	"testing"
)

const issue89StateDiagram = `stateDiagram-v2
    [*] --> 已捕获原消息
    已捕获原消息 --> 媒体处理中
    媒体处理中 --> 已缓存: 校验通过
    媒体处理中 --> 缓存失败: 下载失败或内容不完整
    媒体处理中 --> 已撤回等待缓存: 撤回通知先到
    已撤回等待缓存 --> 单次补发: 缓存完成且连接仍有效
    已缓存 --> 单次补发: 随后收到撤回
    单次补发 --> [*]
    缓存失败 --> [*]
`

func assertNoOverlap(t *testing.T, boxes []ShapeOp) {
	t.Helper()
	for i := 0; i < len(boxes); i++ {
		for j := i + 1; j < len(boxes); j++ {
			a, b := boxes[i], boxes[j]
			overlapX := a.X < b.X+b.W && b.X < a.X+a.W
			overlapY := a.Y < b.Y+b.H && b.Y < a.Y+a.H
			if overlapX && overlapY {
				t.Fatalf("overlapping shapes %+v and %+v", a, b)
			}
		}
	}
}

func assertNoLabelOverlap(t *testing.T, labels []LabelOp) {
	t.Helper()
	for i := 0; i < len(labels); i++ {
		for j := i + 1; j < len(labels); j++ {
			a, b := labels[i], labels[j]
			overlapX := a.X < b.X+b.W && b.X < a.X+a.W
			overlapY := a.Y < b.Y+b.H && b.Y < a.Y+a.H
			if overlapX && overlapY {
				t.Fatalf("overlapping labels %q (%+v) and %q (%+v)", a.Text, a, b.Text, b)
			}
		}
	}
}

func TestStateDiagramIssue89Layout(t *testing.T) {
	d, err := ParseAndLayout(issue89StateDiagram, nil)
	if err != nil {
		t.Fatalf("ParseAndLayout failed: %v", err)
	}
	if d.Kind != "state" {
		t.Fatalf("Kind = %q, want state", d.Kind)
	}
	assertDiagramBounds(t, d)

	var starts, ends, rounds int
	var nodeShapes []ShapeOp
	for _, s := range d.Shapes {
		switch s.Kind {
		case ShapeStartState:
			starts++
			nodeShapes = append(nodeShapes, s)
		case ShapeEndState:
			ends++
			nodeShapes = append(nodeShapes, s)
		case ShapeRound:
			rounds++
			nodeShapes = append(nodeShapes, s)
		}
	}
	if starts != 1 || ends != 1 || rounds != 6 {
		t.Fatalf("unexpected shapes: starts=%d ends=%d rounds=%d", starts, ends, rounds)
	}
	if len(d.Paths) != 9 {
		t.Fatalf("expected 9 transition paths, got %d", len(d.Paths))
	}
	assertNoOverlap(t, nodeShapes)

	var edgeLabels []LabelOp
	labelsSeen := map[string]bool{}
	for _, lb := range d.Labels {
		labelsSeen[lb.Text] = true
		if lb.Role == RoleEdgeLabel {
			edgeLabels = append(edgeLabels, lb)
		}
	}
	for _, want := range []string{
		"已捕获原消息", "媒体处理中", "已缓存", "缓存失败", "已撤回等待缓存", "单次补发",
		"校验通过", "下载失败或内容不完整", "撤回通知先到", "缓存完成且连接仍有效", "随后收到撤回",
	} {
		if !labelsSeen[want] {
			t.Fatalf("missing label %q in %v", want, labelsSeen)
		}
	}
	assertNoLabelOverlap(t, edgeLabels)
	assertNoLabelOverlap(t, d.Labels)
}

func TestStateDiagramV1DirectionsAliasesAndDescriptions(t *testing.T) {
	src := `---
title: Order Flow
---
stateDiagram
    direction LR
    hide empty description
    state "Waiting for Payment" as Pending
    Pending : entry / start timer
    Pending : step 1 -> step 2
    [*] -> Pending
    Pending -> Paid : pay
    Paid --> Paid : refresh
    Paid --> Pending : retry
    Paid -> [*]
`
	d, err := ParseAndLayout(src, nil)
	if err != nil {
		t.Fatalf("ParseAndLayout failed: %v", err)
	}
	assertDiagramBounds(t, d)

	var hasDivider bool
	for _, s := range d.Shapes {
		if s.Kind == ShapeRound && s.DividerY > 0 {
			hasDivider = true
		}
	}
	if !hasDivider {
		t.Fatal("expected titled state box with DividerY > 0 for state descriptions")
	}

	labels := map[string]bool{}
	for _, lb := range d.Labels {
		labels[lb.Text] = true
	}
	for _, want := range []string{"Waiting for Payment", "entry / start timer\nstep 1 -> step 2", "pay", "refresh", "retry"} {
		if !labels[want] {
			t.Fatalf("missing label %q in %v", want, labels)
		}
	}
}

func TestStateDiagramCompositeConcurrencyStereotypesAndNotes(t *testing.T) {
	src := `stateDiagram-v2
    state if_ok <<choice>>
    state fork_work <<fork>>
    state join_work <<join>>

    [*] --> if_ok
    if_ok --> Active : yes
    if_ok --> Failed : no

    state "Active Phase" as Active {
        [*] --> Running
        Running --> Paused : pause
        Paused --> Running : resume
        --
        [*] --> Syncing
        Syncing --> Synced
    }
    note right of Failed : Terminal error state
    note left of Active
        Handles parallel
        background tasks
    end note

    Active --> fork_work
    fork_work --> WorkerA
    fork_work --> WorkerB
    WorkerA --> join_work
    WorkerB --> join_work
    join_work --> [*]
    Failed --> [*]
`
	d, err := ParseAndLayout(src, nil)
	if err != nil {
		t.Fatalf("ParseAndLayout failed: %v", err)
	}
	assertDiagramBounds(t, d)

	var hasSubgraph, hasChoice, hasBar, hasNote, hasDashedConc bool
	for _, s := range d.Shapes {
		switch {
		case s.Role == RoleSubgraph && s.DividerY > 0:
			hasSubgraph = true
		case s.Kind == ShapeDiamond:
			hasChoice = true
		case s.Kind == ShapeBar:
			hasBar = true
		case s.Role == RoleNote:
			hasNote = true
		}
	}
	for _, p := range d.Paths {
		if p.Role == RoleFragment && p.Style == EdgeDashed {
			hasDashedConc = true
		}
	}
	if !hasSubgraph || !hasChoice || !hasBar || !hasNote || !hasDashedConc {
		t.Fatalf("missing expected features: subgraph=%v choice=%v bar=%v note=%v concurrency=%v",
			hasSubgraph, hasChoice, hasBar, hasNote, hasDashedConc)
	}

	var foundMultilineNote bool
	for _, lb := range d.Labels {
		if strings.Contains(lb.Text, "Handles parallel\nbackground tasks") {
			foundMultilineNote = true
		}
	}
	if !foundMultilineNote {
		t.Fatal("missing multiline note text")
	}
}

func TestStateDiagramInlineCompositeAndCrossBoundary(t *testing.T) {
	src := `stateDiagram-v2
    state Outer { [*] --> InnerA; InnerA --> InnerB }
    state Done
    InnerB --> Done : finish
    Done --> [*]
`
	d, err := ParseAndLayout(src, nil)
	if err != nil {
		t.Fatalf("ParseAndLayout failed: %v", err)
	}
	assertDiagramBounds(t, d)

	labels := map[string]bool{}
	for _, lb := range d.Labels {
		labels[lb.Text] = true
	}
	for _, want := range []string{"Outer", "InnerA", "InnerB", "Done", "finish"} {
		if !labels[want] {
			t.Fatalf("missing label %q in %v", want, labels)
		}
	}
}

func TestStateDiagramErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"empty", "stateDiagram-v2\n"},
		{"unclosed_composite", "stateDiagram-v2\nstate A {\n  [*] --> B\n"},
		{"extra_brace", "stateDiagram-v2\n[*] --> A\n}\n"},
		{"unclosed_note", "stateDiagram-v2\n[*] --> A\nnote right of A\n  dangling\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseAndLayout(tc.src, nil); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}
