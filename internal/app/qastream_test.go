package app

import (
	"context"
	"math"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/agent"
	"github.com/egoist/mygo/ui"
)

func qaAdvance(tt *ui.Tester, duration time.Duration) {
	for duration > 0 {
		dt := min(duration, 16*time.Millisecond)
		tt.Advance(dt)
		duration -= dt
	}
}

func TestQAStreamBurstsUnicodeAndFinish(t *testing.T) {
	start := time.Unix(100, 0)
	var stream qaStream
	stream.step("", start, true, 0)
	target := ""
	previous := ""
	commits := 0
	chunks := []string{"流式", "输出保留", "中文和", "emoji 👻 ", "é组合", "字符。", "每个字", "平滑淡入。"}
	for frame := range 140 {
		if frame%5 == 0 && frame/5 < len(chunks) {
			target += chunks[frame/5]
		}
		delay := stream.step(target, start.Add(time.Duration(frame)*16*time.Millisecond), true, 30)
		if !utf8.ValidString(stream.shown) || !strings.HasPrefix(target, stream.shown) || !strings.HasPrefix(stream.shown, previous) {
			t.Fatalf("stream corrupted or rolled back: %q -> %q / %q", previous, stream.shown, target)
		}
		if stream.shown != previous {
			commits++
		}
		if frame < 4 && stream.shown == target {
			t.Fatal("small network chunk bypassed the balanced buffer")
		}
		if frame == 139 && (stream.shown != target || delay != 0) {
			t.Fatal("idle tail did not drain and stop scheduling")
		}
		previous = stream.shown
	}
	if commits < 5 {
		t.Fatalf("chunks were not paced: %d commits", commits)
	}
	stream.step(target+" buffered ending", start.Add(3*time.Second), true, 30)
	stream.step(target+" buffered ending", start.Add(3*time.Second), false, 30)
	if stream.shown != target+" buffered ending" {
		t.Fatal("completion lost the buffered tail")
	}
	stream.step("replacement", start.Add(4*time.Second), true, 0)
	if stream.shown != "replacement" {
		t.Fatal("replacement retained the old buffer")
	}
	stream.step("", start.Add(5*time.Second), true, 0)
	if stream.shown != "" || stream.count != 0 {
		t.Fatal("tool reset retained the old buffer")
	}
}

func TestQAStreamPacesBatchedParagraphsAndCode(t *testing.T) {
	now := time.Unix(100, 0)
	for _, text := range []string{strings.Repeat("字", 321), strings.Repeat("一个完整的段落。\n\n", 25), "```html\n" + strings.Repeat("<p>hello</p>\n", 20) + "```"} {
		var s qaStream
		// No waiting frame first: the response may arrive before the UI
		// builds the turn for the first time.
		if delay := s.step(text, now, true, 0); s.shown == text || delay == 0 {
			t.Fatal("first large delivery skipped buffering")
		}
		for frame := 1; frame <= 300; frame++ {
			before := s.shownCount
			s.step(text, now.Add(time.Duration(frame)*16*time.Millisecond), true, 100)
			if s.shownCount-before > 34 {
				t.Fatal("complete block bypassed the reveal budget")
			}
		}
		if s.shown != text {
			t.Fatal("batched response did not finish revealing")
		}
	}
}

func TestQABatchedCompletionRetainsAnimation(t *testing.T) {
	answer := strings.Repeat("这段回答先全部到达，再在界面中逐字淡入。\n\n", 12) + "最后一行 [计划.md#L5]"
	a, tt := qaApp(t, &scripted{steps: []func(agent.Request) agent.Message{reply(answer)}})
	a.toggleQA()
	tt.Frame()
	qaAdvance(tt, 500*time.Millisecond)
	a.qa.input = "测试快速返回的完整回答"
	a.askQA() // scripted sends TextDelta + StepDone + finish before a frame
	s := a.qaSession(a.state.ActiveSpace().ID)
	turn := s.turns[0]
	tt.Frame()
	if turn.running || !turn.revealing || !s.running() || turn.displayAnswer() == answer || tt.HasText("来源") {
		t.Fatal("provider completion flushed the answer before it could animate")
	}
	qaAdvance(tt, 160*time.Millisecond)
	if turn.displayAnswer() == "" || turn.displayAnswer() == answer {
		t.Fatal("expected a partially revealed answer")
	}
	firstBorn := slices.Clone(turn.view.blocks[0].fade.births)
	if !turn.view.fading(tt.Now()) {
		t.Fatal("finished provider disabled character fades")
	}
	for range 400 {
		if !turn.revealing {
			break
		}
		tt.Advance(16 * time.Millisecond)
	}
	if turn.revealing || s.running() || turn.displayAnswer() != answer || !tt.HasText("来源") {
		t.Fatal("reveal failed to settle with sources and send control")
	}
	if len(firstBorn) == 0 {
		t.Fatal("no character animation was assigned")
	}
}

func TestQAStopFinishesBufferedPresentation(t *testing.T) {
	a, tt := qaApp(t, &scripted{steps: []func(agent.Request) agent.Message{reply(strings.Repeat("待显示的文字", 100))}})
	a.toggleQA()
	tt.Frame()
	qaAdvance(tt, 500*time.Millisecond)
	a.qa.input = "测试"
	a.askQA()
	tt.Frame()
	s := a.qaSession(a.state.ActiveSpace().ID)
	turn := s.turns[0]
	if s.cancel != nil || !tt.HasText("停止") {
		t.Fatal("presentation has no stop control after the request finishes")
	}
	tt.Click("停止")
	tt.Frame()
	if turn.revealing || s.running() || turn.displayAnswer() != turn.answer || turn.view.fading(tt.Now()) {
		t.Fatal("stop did not finish the display buffer")
	}
}

func TestQAToolResetDiscardsQueuedPresentation(t *testing.T) {
	a, tt := qaApp(t, &scripted{})
	a.toggleQA()
	tt.Frame()
	qaAdvance(tt, 500*time.Millisecond)
	s := a.qaSession(a.state.ActiveSpace().ID)
	turn := &qaTurn{running: true}
	s.turns = []*qaTurn{turn}
	tt.Frame()
	turn.apply(agent.Event{Kind: agent.TextDelta, Text: strings.Repeat("旧的思考", 60)})
	tt.Frame()
	qaAdvance(tt, 160*time.Millisecond)
	oldFade := turn.view.blocks[0].fade
	// A tool call and replacement answer can also share a single frame.
	turn.apply(agent.Event{Kind: agent.StepDone, Message: agent.Message{Calls: []agent.ToolCall{{Name: agent.ToolRead}}}})
	turn.apply(agent.Event{Kind: agent.TextDelta, Text: "新的答案"})
	tt.Frame()
	qaAdvance(tt, 160*time.Millisecond)
	if len(turn.view.blocks) > 0 && turn.view.blocks[0].fade == oldFade {
		t.Fatal("replacement reused the old animation clock")
	}
	qaAdvance(tt, time.Second)
	if turn.displayAnswer() != "新的答案" {
		t.Fatal("tool reset resurrected old buffered text")
	}
}

func qaSettle(t *testing.T, tt *ui.Tester, turn *qaTurn) {
	t.Helper()
	for range 800 {
		if !turn.revealing {
			tt.Frame()
			return
		}
		tt.Advance(16 * time.Millisecond)
	}
	t.Fatal("answer did not finish its presentation")
}

func qaRendered(v *qaTurnView) string {
	var out strings.Builder
	for _, b := range v.blocks {
		for _, r := range b.runs {
			out.WriteString(r.text)
		}
	}
	return out.String()
}

func TestQAStreamingMarkdownHealingAndStableBirths(t *testing.T) {
	var v qaTurnView
	now := time.Unix(100, 0)
	v.parse("**已经显示", true)
	v.animate(now, true)
	if got := qaRendered(&v); got != "已经显示" || v.blocks[0].runs[0].weight != 600 {
		t.Fatalf("unclosed strong: %q %+v", got, v.blocks)
	}
	births := slices.Clone(v.blocks[0].fade.births)
	v.parse("**已经显示**，然后是 `code` 和 *斜体", true)
	v.animate(now.Add(80*time.Millisecond), true)
	if !slices.Equal(births, v.blocks[0].fade.births[:len(births)]) {
		t.Fatal("closing strong replayed old characters")
	}
	if got := qaRendered(&v); got != "已经显示，然后是 code 和 斜体" {
		t.Fatalf("healed Markdown: %q", got)
	}
	oldFade := v.blocks[0].fade
	v.parse(v.source+"*\n\n## 下一段", true)
	v.animate(now.Add(160*time.Millisecond), true)
	if v.blocks[0].fade != oldFade || !slices.Equal(births, oldFade.births[:len(births)]) {
		t.Fatal("new block replayed completed paragraph")
	}
	for _, source := range []string{"[链接](https://exam", "[链接](https://example.com)"} {
		v.parse(source, true)
		v.animate(now.Add(time.Second), true)
		if qaRendered(&v) != "链接" || len(v.blocks[0].fade.births) != 2 {
			t.Fatalf("URL consumed visible character slots: %+v", v.blocks)
		}
	}
	v.parse("`**literal**`", true)
	if qaRendered(&v) != "**literal**" || !v.blocks[0].runs[0].code {
		t.Fatal("code was interpreted as emphasis")
	}
	v.parse("- first\n  - nested\n\n| A | B |\n| - | - |\n| C | D |", false)
	if len(v.blocks) != 4 || v.blocks[1].indent != 2 || v.blocks[2].kind != qaTableRow || !v.blocks[2].header {
		t.Fatalf("nested list/table: %+v", v.blocks)
	}
}

func TestQACharFadeTimingAndLayout(t *testing.T) {
	now := time.Unix(100, 0)
	for _, ink := range []ui.Color{ui.RGB(0, 0, 0), ui.RGB(255, 255, 255).Alpha(.5)} {
		if color := qaFadeInk(ink, 0); color.A != 1 {
			t.Fatal("zero alpha inherits opaque text on the first frame")
		}
	}
	var fade qaFade
	fade.update(now, 4, 4, true, true, 80*time.Millisecond)
	for i, b := range fade.births {
		if b != now.Add(time.Duration(i)*18*time.Millisecond) {
			t.Fatalf("birth %d: %s", i, b)
		}
	}
	for _, sample := range []struct {
		elapsed time.Duration
		opacity float32
	}{{0, 0}, {90 * time.Millisecond, 0.5}, {180 * time.Millisecond, 1}} {
		if got := qaFadeAlpha(now.Add(sample.elapsed), now); math.Abs(float64(got-sample.opacity)) > .001 {
			t.Fatalf("fade %s = %f", sample.elapsed, got)
		}
	}
	// A burst cannot build an invisible queue lasting seconds.
	fade.update(now.Add(time.Second), 500, 500, true, true, 96*time.Millisecond)
	if fade.births[499].After(now.Add(time.Second + 96*time.Millisecond + qaFadeDuration)) {
		t.Fatal("unbounded fade backlog")
	}
	a, tt := newTestApp(t)
	turn := &qaTurn{question: "Layout", answer: "A stable **bold** paragraph whose characters fade in without changing line breaks.\n\nNext paragraph.", running: true}
	s := a.qaSession(a.state.ActiveSpace().ID)
	s.turns = []*qaTurn{turn}
	a.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	before, ok := tt.Find("Next paragraph.")
	if !ok {
		t.Fatal("missing paragraph")
	}
	qaAdvance(tt, time.Second)
	after, ok := tt.Find("Next paragraph.")
	if !ok || before.Y != after.Y || before.W != after.W || before.H != after.H {
		t.Fatalf("fade changed text layout: %+v -> %+v", before, after)
	}
}

func TestQAStreamStopReducedMotionAndCopy(t *testing.T) {
	a, tt := qaApp(t, &scripted{})
	a.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	s := a.qaSession(a.state.ActiveSpace().ID)
	turn := &qaTurn{question: "Stop", running: true}
	s.turns = []*qaTurn{turn}
	s.cancel = func() {}
	tt.Frame()
	turn.apply(agent.Event{Kind: agent.TextDelta, Text: "还没有完全显示的回答"})
	tt.Frame()
	if turn.displayAnswer() == turn.answer {
		t.Fatal("expected a buffered tail")
	}
	a.finishQA(s, turn, nil, context.Canceled)
	tt.Frame()
	if turn.displayAnswer() != turn.answer || !tt.HasText("已停止") || turn.view.blocks[0].fade.active(tt.Now()) {
		t.Fatal("cancel lost text or kept animating")
	}
	turn.running, turn.stopped = true, false
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	turn.apply(agent.Event{Kind: agent.TextDelta, Text: "，减弱动态效果直接显示。"})
	tt.Frame()
	if turn.displayAnswer() != turn.answer {
		t.Fatal("reduced motion kept buffering")
	}
	turn.answer = "```go\nfmt.Println(\"hello\")\n```"
	turn.running = false
	tt.Frame()
	tt.Click("复制代码")
	if tt.Clipboard() != "fmt.Println(\"hello\")" {
		t.Fatalf("copy code: %q", tt.Clipboard())
	}
}
