package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
)

// Keep both long histories and one long answer in the workload: virtualizing
// only whole turns does not bound the work of scrolling a long response.
func BenchmarkQA(b *testing.B) {
	for _, turns := range []int{1, 40} {
		b.Run(fmt.Sprintf("Scroll/%d", turns), func(b *testing.B) {
			a, tt := newTestApp(b)
			s := a.qaSession(a.state.ActiveSpace().ID)
			for i := range turns {
				s.turns = append(s.turns, &qaTurn{question: fmt.Sprintf("Question %d", i), answer: strings.Repeat("- **进展**：支付联调已完成，证书审批仍在进行，下一步需要验证回调 [计划.md#L5]。\n\n", 60)})
			}
			a.toggleQA()
			tt.Frame()
			tt.Advance(slide)
			tt.Frame()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				dy := float32(-24)
				if i%80 >= 40 {
					dy = -dy
				}
				tt.Scroll(1050, 350, 0, dy)
			}
		})
	}
	b.Run("ToggleLongDocument", func(b *testing.B) {
		a, tt := newTestApp(b)
		var source strings.Builder
		for i := range 240 {
			fmt.Fprintf(&source, "Paragraph %d：展开问答时正文继续换行，屏幕外的段落应复用排版。这里添加更多文字来模拟真实的长文档。\n\n", i)
		}
		a.doc.ed = editor.New(source.String())
		tt.Frame()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			a.toggleQA()
			tt.Frame()
			for elapsed := time.Duration(0); elapsed <= slide; elapsed += 16 * time.Millisecond {
				tt.Advance(16 * time.Millisecond)
			}
		}
	})
}

func BenchmarkQAStreaming(b *testing.B) {
	a, tt := newTestApp(b)
	s := a.qaSession(a.state.ActiveSpace().ID)
	turn := &qaTurn{question: "Stream a long answer", running: true, answer: strings.Repeat("A completed paragraph stays stable while the tail streams.\n\n", 200)}
	s.turns = []*qaTurn{turn}
	a.toggleQA()
	tt.Frame()
	tt.Advance(time.Second)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		if i%4 == 0 {
			turn.answer += "逐字输出。"
		}
		tt.Advance(16 * time.Millisecond)
	}
}

// Resizing the native window is a different path from toggling the panel.
// Keep a book-sized document open behind a short, already finished answer.
func BenchmarkQAWindowResize(b *testing.B) {
	a, tt := newTestApp(b)
	a.doc.ed = editor.New(qaResizeDocument())
	s := a.qaSession(a.state.ActiveSpace().ID)
	s.turns = []*qaTurn{{question: "总结一下", answer: strings.Repeat("- **主要内容**：这是一篇长文档的简短回答。\n\n", 10)}}
	a.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	a.doc.ed.Scroll.Y = 100000
	tt.Frame()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tt.SetSize(1200+(i%20)*4, 760)
		tt.Advance(16 * time.Millisecond)
	}
}

func qaResizeDocument() string {
	var source strings.Builder
	for i := range 2200 {
		fmt.Fprintf(&source, "第 %d 段：生成式人工智能与实践。调整窗口大小时，可见段落应立即换行，屏幕外的段落不应反复排版。文章很长时仍然需要保持当前阅读位置，避免在每次宽度变化时重建整篇文章的文字样式、语法高亮与字形。\n\n", i)
	}
	return source.String()
}

func BenchmarkQARapidResize(b *testing.B) {
	for _, pause := range []bool{false, true} {
		b.Run(fmt.Sprintf("pause=%v", pause), func(b *testing.B) {
			a, tt := newTestApp(b)
			a.doc.ed = editor.New(qaResizeDocument())
			a.toggleQA()
			tt.Frame()
			tt.Advance(slide)
			a.doc.ed.Scroll.Y = 100000
			tt.Frame()
			widths := []int{1100, 1360, 1040, 1280, 1160, 1400}
			var worst time.Duration
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				start := time.Now()
				tt.SetSize(widths[i%len(widths)], 760)
				elapsed := 8 * time.Millisecond
				if pause {
					elapsed = 130 * time.Millisecond
				}
				tt.Advance(elapsed)
				worst = max(worst, time.Since(start))
			}
			b.ReportMetric(float64(worst.Nanoseconds())/1e6, "max-ms/op")
		})
	}
}
