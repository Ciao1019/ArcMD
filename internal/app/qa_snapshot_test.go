package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/agent"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/prefs"
	"github.com/egoist/mygo/ui"
)

// StreamingPlayground's default production rate and chunk sizes, with a
// deterministic clock instead of network requests or sleeps.
func TestQAStreamingSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render Lobe-style streaming")
	for _, mode := range []string{"light", "dark", "narrow"} {
		t.Run(mode, func(t *testing.T) {
			a, tt := newTestApp(t)
			if mode == "dark" {
				tt.SetDark(true)
			}
			if mode == "narrow" {
				tt.SetSize(640, 500)
			}
			a.toggleQA()
			tt.Frame()
			qaAdvance(tt, 500*time.Millisecond)
			s := a.qaSession(a.state.ActiveSpace().ID)
			turn := &qaTurn{question: "介绍一下这种流式阅读效果", running: true}
			s.turns = []*qaTurn{turn}
			s.cancel = func() {}
			tt.Frame()
			content := []rune("## 平滑的流式回答\n\n文字会**逐字淡入**，已经显示的内容保持稳定。中文、English 和 emoji 👻 都跟随同一节奏，不会一整块突然跳出来。\n\n> 让每一段内容自然地接上，阅读时不打断视线。\n\n- 支持 **粗体**、*斜体*与 `inline code`\n- 保留可点击的文档引用 [计划.md#L5]\n\n```go\nfmt.Println(\"Hello, world!\")\n```\n\n| 特性 | 表现 |\n| --- | --- |\n| 淡入 | 180ms |\n| 缓冲 | balanced |\n")
			pos := 0
			for frame := 0; frame < 360; frame++ {
				if frame%4 == 0 && pos < len(content) {
					next := min(len(content), pos+5)
					turn.apply(agent.Event{Kind: agent.TextDelta, Text: string(content[pos:next])})
					pos = next
				}
				tt.Advance(16 * time.Millisecond)
				if frame == 40 || frame == 80 || frame == 112 || frame == 184 {
					savePNG(t, filepath.Join(dir, fmt.Sprintf("qa-stream-%s-%03d.png", mode, frame)), tt)
				}
			}
			turn.running = false
			s.cancel = nil
			tt.Frame()
			savePNG(t, filepath.Join(dir, "qa-stream-"+mode+"-complete.png"), tt)
		})
	}
}

// Regression for real providers that batch the full response, including the
// done event, before one frame. This uses the Agent/provider completion path.
func TestQABatchedCompletionSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to inspect completed-response animation")
	answer := "## 快速返回，也会逐字显示\n\n模型已经一次性返回完整回答，但界面还会把**剩余文字逐字淡入**。网络请求结束不会再突然展开整段文字。\n\n- 新字符依次出现，已经显示的文字保持稳定。\n- 引用、列表和代码会随内容自然展开。\n\n完成后才展示来源，并恢复发送按钮 [计划.md#L5]。"
	for _, mode := range []string{"light", "dark", "narrow"} {
		t.Run(mode, func(t *testing.T) {
			a, tt := qaApp(t, &scripted{steps: []func(agent.Request) agent.Message{reply(answer)}})
			if mode == "dark" {
				tt.SetDark(true)
			}
			if mode == "narrow" {
				tt.SetSize(640, 500)
			}
			a.toggleQA()
			tt.Frame()
			qaAdvance(tt, 500*time.Millisecond)
			a.qa.input = "快速返回的回答也要有动画"
			a.askQA()
			tt.Frame()
			elapsed := time.Duration(0)
			for _, ms := range []int{0, 160, 320, 640, 960} {
				next := time.Duration(ms) * time.Millisecond
				qaAdvance(tt, next-elapsed)
				elapsed = next
				savePNG(t, filepath.Join(dir, fmt.Sprintf("qa-batched-%s-%04d.png", mode, ms)), tt)
			}
			qaSettle(t, tt, a.qaSession(a.state.ActiveSpace().ID).turns[0])
			savePNG(t, filepath.Join(dir, "qa-batched-"+mode+"-complete.png"), tt)
		})
	}
}

func TestQAWindowResizeSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render native window resizing")
	a, tt := newTestApp(t)
	a.doc.ed = editor.New(qaResizeDocument())
	s := a.qaSession(a.state.ActiveSpace().ID)
	s.turns = []*qaTurn{{question: "这篇长文章主要讲什么？", answer: "这篇文章介绍**生成式人工智能**的基本原理与实践。\n\n- 阅读时可以调整窗口大小。\n- 正文应实时换行，问答保持独立滚动。"}}
	a.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	a.doc.ed.Scroll.Y = 100000
	tt.Frame()
	tt.SetSize(1120, 760)
	tt.Advance(40 * time.Millisecond)
	savePNG(t, filepath.Join(dir, "qa-window-resizing.png"), tt)
	tt.Advance(slide)
	savePNG(t, filepath.Join(dir, "qa-window-resized.png"), tt)
}

func TestQASnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the question panel")
	for _, name := range []string{"empty", "unset", "answer", "answer-dark", "answer-narrow", "answer-right", "steps", "steps-dark", "steps-hits", "error", "running", "running-dark", "running-narrow", "ghost", "ghost-dark", "ghost-narrow", "thinking", "settings", "settings-empty", "settings-models", "settings-models-narrow", "settings-models-dark", "settings-models-en"} {
		t.Run(name, func(t *testing.T) {
			model := &scripted{steps: []func(agent.Request) agent.Message{
				call(agent.ToolSearch, `{"query":"发布延期 阻塞 证书"}`),
				call(agent.ToolGrep, `{"pattern":"延期"}`),
				call(agent.ToolRead, `{"path":"发布计划.md","start_line":1,"end_line":12}`),
				reply("发布**延期到 12 月 2 日**，主要有两个原因 [发布计划.md#L5]：\n\n- 支付联调需要排查回调 [发布计划.md#L7]，目前已在周报确认修复 [项目/周报.md#L5]\n- 证书审批还在走流程 [发布计划.md#L11]\n\n目前唯一的阻塞是证书，`payments` 模块已经修好。"),
			}}
			a, tt := qaApp(t, model)
			if strings.HasSuffix(name, "-dark") {
				tt.SetDark(true)
			}
			if strings.HasSuffix(name, "-narrow") {
				tt.SetSize(640, 500)
			}
			if name == "answer-right" {
				tt.SetTitleBar(ui.TitleBar{Height: 46, Right: 138})
			}
			if strings.HasSuffix(name, "-en") {
				i18n.SetLanguage("en")
				t.Cleanup(func() { i18n.SetLanguage("zh-Hans") })
			}
			os.WriteFile(filepath.Join(root(a), "发布计划.md"), []byte("# 发布计划\n\n## 决定\n\n发布延期到 12 月 2 日。\n\n支付联调还没完成。\n\n## 风险与阻塞\n\n证书审批还在走流程。\n"), 0o644)
			os.MkdirAll(filepath.Join(root(a), "项目"), 0o755)
			os.WriteFile(filepath.Join(root(a), "项目/周报.md"), []byte("# 周报\n\n## 进展\n\npayments 支付回调已联调通过。\n"), 0o644)
			if name == "unset" {
				a.prefs.AI.Model = ""
			}
			a.toggleQA()
			tt.Frame()
			tt.Advance(slide)
			sp := a.state.ActiveSpace().ID
			switch name {
			case "answer", "answer-dark", "answer-narrow", "answer-right", "steps", "steps-dark", "steps-hits":
				a.qa.input = "为什么发布延期了？现在还卡在哪？"
				a.askQA()
				if strings.HasPrefix(name, "steps") {
					a.qaSession(sp).turns[0].expanded = true
				}
			case "error":
				s := a.qaSession(sp)
				s.turns = append(s.turns, &qaTurn{question: "总结一下本周的笔记", err: &agent.APIError{Status: 401, Message: "Incorrect API key provided"}})
			case "running", "running-dark", "running-narrow":
				s := a.qaSession(sp)
				s.cancel = func() {}
				s.turns = append(s.turns, &qaTurn{question: "有哪些未完成的任务？", running: true, steps: []*qaStep{
					{call: agent.ToolCall{Name: agent.ToolSearch, Args: `{"query":"未完成 任务 待办"}`}, done: true, result: "4 relevant passages in 3 documents."},
					{call: agent.ToolCall{Name: agent.ToolGrep, Args: `{"pattern":"- [ ]"}`}, done: true, result: "12 matches in 4 documents."},
					{call: agent.ToolCall{Name: agent.ToolRead, Args: `{"path":"项目/周报.md","start_line":20,"end_line":60}`}},
				}})
			case "ghost", "ghost-dark", "ghost-narrow", "thinking":
				s := a.qaSession(sp)
				s.cancel = func() {}
				s.turns = append(s.turns, &qaTurn{question: "这个空间里有哪些还没完成的任务？", running: true, thinking: name == "thinking"})
			case "settings":
				a.openSettings(tabAI)
			case "settings-empty":
				a.prefs.AI = prefs.AI{Provider: agent.Custom}
				a.qa.key = ""
				a.openSettings(tabAI)
			case "settings-models", "settings-models-narrow", "settings-models-dark", "settings-models-en":
				a.prefs.AI = prefs.AI{Provider: "deepseek", BaseURL: "https://api.deepseek.com"}
				a.qa.listModels = func(agent.Settings) ([]string, error) {
					return []string{"deepseek-flash", "deepseek-v4-pro"}, nil
				}
				a.openSettings(tabAI)
				tt.Frame()
				tt.Click(i18n.T("获取模型"))
				tt.Frame()
				tt.Click(i18n.T("选择模型"))
			}
			tt.Frame()
			tt.Advance(slide)
			tt.Frame()
			if strings.HasPrefix(name, "answer") || strings.HasPrefix(name, "steps") {
				qaSettle(t, tt, a.qaSession(sp).turns[0])
				if strings.HasPrefix(name, "steps") {
					turn := a.qaSession(sp).turns[0]
					turn.expanded = true
					if name == "steps-hits" && len(turn.steps) > 0 {
						turn.steps[0].open = true
					}
					tt.Frame()
					tt.Advance(220 * time.Millisecond)
					tt.Frame()
				}
			}
			savePNG(t, filepath.Join(dir, "qa-"+name+".png"), tt)
		})
	}
}
