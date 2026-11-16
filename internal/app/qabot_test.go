package app

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/agent"
	"github.com/egoist/mygo/ui"
)

func TestGhostMatchesUpstreamPictures(t *testing.T) {
	data, err := os.ReadFile("testdata/qabot/poses.json")
	if err != nil {
		t.Fatal(err)
	}
	var frames []struct {
		Name string
		Pose botPose
	}
	if err = json.Unmarshal(data, &frames); err != nil {
		t.Fatal(err)
	}
	for _, f := range frames {
		t.Run(f.Name, func(t *testing.T) {
			var renderer botPainter
			tt := ui.NewTester(func(c *ui.Context) {
				c.Root().Background(white)
				ui.Box(c).Size(48, 48).Draw(func(p *ui.Painter, r ui.Rect) {
					renderer.draw(p, ui.Rect{X: r.X + 8, Y: r.Y + 11.2, W: 32, H: 32}, f.Pose)
				})
			}, 48, 48)
			tt.SetScale(2)
			got := tt.Image()
			file, err := os.Open(filepath.Join("testdata/qabot", f.Name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			want, err := png.Decode(file)
			if err != nil {
				t.Fatal(err)
			}
			if got.Bounds() != want.Bounds() {
				t.Fatal("reference size differs")
			}
			var diff, ink float64
			for y := range 96 {
				for x := range 96 {
					gr, gg, gb, _ := got.At(x, y).RGBA()
					wr, wg, wb, _ := want.At(x, y).RGBA()
					diff += (math.Abs(float64(gr)-float64(wr)) + math.Abs(float64(gg)-float64(wg)) + math.Abs(float64(gb)-float64(wb))) / 3 / 257
					ink += (65535 - float64(wr) + 65535 - float64(wg) + 65535 - float64(wb)) / 3 / 257
				}
			}
			t.Logf("pixel MAE %.4f/255, relative ink difference %.4f", diff/(96*96), diff/ink)
			if dir := os.Getenv("ARCMD_SNAPSHOT"); dir != "" {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				savePNG(t, filepath.Join(dir, "ghost-"+f.Name+".png"), tt)
			}
			// Material views interpolate; native path/image antialiasing also differs
			// from Canvas. Fail if the shape, eyes or pose materially diverge.
			if diff/(96*96) > 1.3 || diff/ink > .1 {
				t.Fatal("ghost differs from original renderer")
			}
		})
	}
}

func TestGhostPointerClickAndReducedMotion(t *testing.T) {
	var s qaBotState
	hidden, clipped := false, false
	builds := 0
	tt := ui.NewTester(func(c *ui.Context) {
		builds++
		c.Root().Background(white)
		ui.Column(c).Fill().Clip().Padding(70).Children(func() {
			if clipped {
				ui.Box(c).Height(400).Shrink(0)
			}
			qaGhostAvatar(c, &s).Hidden(hidden)
		})
	}, 220, 220)
	label := "小幽灵，点击跳跃"
	r, ok := tt.Find(label)
	if !ok || r.W != 32 || r.H != 32 {
		t.Fatalf("ghost must be 32×32: %+v", r)
	}
	advance := func(n int) {
		for range n {
			tt.Advance(time.Second / 60)
		}
	}
	tt.Move(r.X+r.W+12, r.Y+4)
	advance(60)
	if s.motion.ptrS < .9 || s.motion.ptrX < .5 || s.motion.pose.LookX <= 0 {
		t.Fatalf("pointer did not attract head and eyes: %+v", s.motion.pose)
	}
	right := s.motion.pose.Yaw
	tt.Move(r.X-12, r.Y+4)
	advance(60)
	if s.motion.pose.Yaw >= right || s.motion.ptrX > -.5 {
		t.Fatal("head did not turn left with pointer")
	}
	tt.Move(215, 215)
	advance(60)
	if s.motion.ptrS > .01 {
		t.Fatal("pointer outside three head widths still attracts the ghost")
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	if !s.motion.flip.active() || !s.motion.flipPoked {
		t.Fatal("click did not start the original hop")
	}
	advance(32)
	if s.motion.pose.Y > -15 {
		t.Fatalf("click did not jump: y=%v", s.motion.pose.Y)
	}
	advance(90)
	if s.motion.flip.active() {
		t.Fatal("jump failed to land")
	}
	// The same action is keyboard accessible.
	tt.Key(0, ui.KeyEnter)
	if !s.motion.flip.active() {
		t.Fatal("Enter did not activate the focused ghost")
	}
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	before := bytes.Clone(tt.Image().Pix)
	built := builds
	advance(60)
	if !bytes.Equal(before, tt.Image().Pix) || tt.FramePending() || builds != built {
		t.Fatal("reduced-motion avatar did not rest")
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	if tt.FramePending() {
		t.Fatal("reduced-motion click scheduled animation")
	}
	tt.SetPreferences(ui.Preferences{})
	advance(1)
	built = builds
	advance(10)
	if builds != built || !tt.FramePending() {
		t.Fatal("animation must repaint without rebuilding the view")
	}
	for _, kind := range []string{"hidden", "clipped"} {
		hidden, clipped = kind == "hidden", kind == "clipped"
		tt.Frame()
		if tt.FramePending() {
			t.Fatalf("%s ghost kept requesting frames", kind)
		}
	}
}

func TestGhostFollowsWaitingTurn(t *testing.T) {
	a, tt := qaApp(t, &scripted{})
	tt.Key(ui.Cmd, ui.KeyJ)
	tt.Advance(slide)
	s := a.qaSession(a.state.ActiveSpace().ID)
	turn := &qaTurn{question: "查找空间里的任务", running: true}
	s.turns = append(s.turns, turn)
	stopped := false
	s.cancel = func() { stopped = true }
	tt.Frame()
	if !turn.avatar.initialized || !tt.HasText("正在查找…") || !tt.HasText("停止") {
		t.Fatal("waiting ghost missing")
	}
	turn.apply(agent.Event{Kind: agent.ReasoningDelta, Text: "reasoning"})
	tt.Frame()
	if !tt.HasText("思考中…") {
		t.Fatal("reasoning status missing")
	}
	turn.apply(agent.Event{Kind: agent.TextDelta, Text: "正在整理结果"})
	tt.Frame()
	qaAdvance(tt, 700*time.Millisecond)
	if tt.HasText("思考中…") || !tt.HasText("正在整理结果") {
		t.Fatal("streaming did not replace status")
	}
	turn.apply(agent.Event{Kind: agent.StepDone, Message: agent.Message{Calls: []agent.ToolCall{{Name: agent.ToolRead}}}})
	tt.Frame()
	if !tt.HasText("正在查找…") {
		t.Fatal("tool round did not restore ghost")
	}
	if err := tt.Click("停止"); err != nil || !stopped {
		t.Fatal("stop did not cancel")
	}
	a.finishQA(s, turn, nil, context.Canceled)
	tt.Frame()
	if tt.HasText("正在查找…") || !tt.HasText("已停止") {
		t.Fatal("stopped turn retained its ghost")
	}
}
