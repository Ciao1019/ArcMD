package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/agent"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/egoist/mygo/ui"
)

func TestQAActivityPrecedesToolHistory(t *testing.T) {
	a, tt := qaApp(t, &scripted{})
	tt.Key(ui.Cmd, ui.KeyJ)
	tt.Advance(slide)
	s := a.qaSession(a.state.ActiveSpace().ID)
	turn := &qaTurn{question: "阅读这份说明", running: true, steps: []*qaStep{
		{call: agent.ToolCall{ID: "read", Name: agent.ToolRead, Args: `{"path":"Fastfetch 使用说明.md","start_line":1,"end_line":80}`}, done: true},
	}}
	s.turns = []*qaTurn{turn}
	s.cancel = func() {}
	for _, label := range []string{"正在查找…", "思考中…"} {
		turn.thinking = label == "思考中…"
		tt.Frame()
		status, hasStatus := tt.Find(label)
		steps, hasSteps := tt.Find("查找过程")
		if !hasStatus || !hasSteps || status.Y+status.H > steps.Y {
			t.Fatalf("activity must precede its tools: status=%+v (%v), steps=%+v (%v)", status, hasStatus, steps, hasSteps)
		}
	}
	turn.apply(agent.Event{Kind: agent.TextDelta, Text: "这份说明介绍了 Fastfetch。"})
	tt.Frame()
	qaAdvance(tt, 700*time.Millisecond)
	if tt.HasText("思考中…") || tt.HasText("正在查找…") || !tt.HasText("这份说明介绍了 Fastfetch。") {
		t.Fatal("streamed answer did not replace activity")
	}
	a.finishQA(s, turn, nil, context.Canceled)
	tt.Frame()
	answer, _ := tt.Find("这份说明介绍了 Fastfetch。")
	stopped, ok := tt.Find("已停止")
	if !ok || stopped.Y < answer.Y+answer.H {
		t.Fatal("cancellation should remain below the partial answer")
	}
}

func TestQALongAnswerScrollAndStreaming(t *testing.T) {
	a, tt := newTestApp(t)
	var text strings.Builder
	for i := range 240 {
		fmt.Fprintf(&text, "Paragraph %03d: a separate block of the answer.\n\n", i)
	}
	turn := &qaTurn{question: "A long answer", answer: text.String(), running: true}
	s := a.qaSession(a.state.ActiveSpace().ID)
	s.turns = []*qaTurn{turn}
	a.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	first, last := s.list.Visible()
	if first < 200 || last-first > 40 || tt.HasText("Paragraph 000: a separate block of the answer.") {
		t.Fatalf("long answer was not virtualized at its end: %d..%d", first, last)
	}
	blocks := &turn.view.blocks[0]
	rows := &s.rows[0]
	// Scrolling up hands the reading position to the user, even while the
	// answer keeps arriving. Compare the actual text's position as well as
	// the logical row: the same row jumping a screen is also a regression.
	tt.Scroll(1050, 350, 0, -600)
	first, _ = s.list.Visible()
	label := s.rows[first+1].turn.view.blocks[s.rows[first+1].index].text
	before, ok := tt.Find(label)
	if !ok {
		t.Fatalf("visible paragraph missing: %q", label)
	}
	if blocks != &turn.view.blocks[0] || rows != &s.rows[0] {
		t.Fatal("scrolling reparsed the answer or rebuilt its row index")
	}
	turn.apply(agent.Event{Kind: agent.TextDelta, Text: "A new streamed paragraph.\n\n"})
	tt.Frame()
	after, ok := tt.Find(label)
	if !ok || after.Y != before.Y || tt.HasText("A new streamed paragraph.") {
		t.Fatalf("streaming moved the reading anchor: %+v -> %+v, found=%v", before, after, ok)
	}
	// Closing and reopening retains the same conversation's reading anchor.
	a.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	a.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	if got, ok := tt.Find(label); !ok || got.Y != before.Y {
		t.Fatalf("reopening moved the reading anchor: %+v -> %+v", before, got)
	}
	// Scrolling back to the end resumes following, including when the last
	// paragraph grows rather than adding another row.
	tt.Scroll(1050, 350, 0, 1000000)
	turn.apply(agent.Event{Kind: agent.TextDelta, Text: "The newest paragraph"})
	tt.Frame()
	qaAdvance(tt, 700*time.Millisecond)
	if !tt.HasText("The newest paragraph") {
		t.Fatal("scrolling back to the end did not resume following")
	}
	turn.apply(agent.Event{Kind: agent.TextDelta, Text: " continues."})
	tt.Frame()
	qaAdvance(tt, 700*time.Millisecond)
	if !tt.HasText("The newest paragraph continues.") {
		t.Fatal("growing the final paragraph stopped following")
	}
	a.clearQA()
	tt.Frame()
	if !tt.HasText("向「Personal」提问") {
		t.Fatal("new conversation kept the old rows or scroll position")
	}
}

func TestQARowsRefreshAfterStreamReset(t *testing.T) {
	turn := &qaTurn{answer: "# Title\n\n> Quote\n\n- **Item** [A.md#L2]\n\n---\n\n```go\nfirst\n", running: true}
	s := &qaSession{turns: []*qaTurn{turn}}
	s.visibleRows()
	if len(turn.view.blocks) != 5 || turn.view.blocks[4].text != "first\n" || len(turn.view.refs) != 1 {
		t.Fatalf("Markdown blocks: %+v; refs: %+v", turn.view.blocks, turn.view.refs)
	}
	turn.apply(agent.Event{Kind: agent.TextDelta, Text: "second\n```\n\nDone [A.md#L2]"})
	s.visibleRows()
	if len(turn.view.blocks) != 6 || turn.view.blocks[4].text != "first\nsecond" || len(turn.view.refs) != 1 {
		t.Fatalf("closed streamed code or deduplicated citations: %+v", turn.view)
	}
	turn.running, turn.revealing = false, false
	rows := s.visibleRows()
	if rows[len(rows)-1].kind != qaSourcesRow {
		t.Fatal("finishing did not append sources")
	}
	// Thinking aloud is cleared on a tool call; cached blocks and references
	// must disappear too, including replacement text of the same length.
	turn.apply(agent.Event{Kind: agent.StepDone, Message: agent.Message{Calls: []agent.ToolCall{{Name: agent.ToolRead}}}})
	s.visibleRows()
	if len(turn.view.blocks) != 0 || len(turn.view.refs) != 0 {
		t.Fatal("stream reset retained the previous answer")
	}
	turn.answer = "[B.md#L3]"
	s.visibleRows()
	turn.answer = "[C.md#L4]"
	s.visibleRows()
	if turn.view.refs[0].cite.Path != "C.md" {
		t.Fatal("same-length answer replacement kept stale citations")
	}
}

func TestQAAnimationUsesResizeLayoutUntilSettled(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		t.Run(fmt.Sprintf("reduced=%v", reduced), func(t *testing.T) {
			a, tt := newTestApp(t)
			a.doc.ed = editor.New(strings.Repeat("A paragraph with enough text to wrap as the question panel opens.\n\n", 100))
			tt.SetPreferences(ui.Preferences{ReduceMotion: reduced})
			tt.Frame()
			for _, open := range []bool{true, false} {
				a.toggleQA()
				tt.Frame()
				tt.Advance(40 * time.Millisecond)
				if !reduced && (!a.qa.resizing || a.qa.shown <= 0 || a.qa.shown >= qaWidth) {
					t.Fatalf("moving panel did not defer offscreen layout: %+v", a.qa)
				}
				tt.Advance(slide)
				want := float32(0)
				if open {
					want = qaWidth
				}
				if a.qa.resizing || a.qa.shown != want {
					t.Fatal("settled panel kept approximate document layout")
				}
			}
		})
	}
}
