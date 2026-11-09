//go:build perfbench

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"slices"
	"strings"
	"testing"
	"time"
)

// CPU scene timing uses tools/perf/overlay.go, excluding software
// rasterization and GPU presentation just like TestPerformance.
func TestSplitResizePerformance(t *testing.T) {
	for _, blocks := range []int{8, 200} {
		t.Run(fmt.Sprint(blocks), func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.PerfPrepare()
			for _, name := range []string{"First.arcmd", "Second.arcmd"} {
				text := "# " + name + "\n\n" + strings.Repeat("这是一段用于测量分屏拖动的文本。调整面板大小时，排版应保持清晰，指针应持续跟手。\n\n", blocks)
				if err := writeNative(root(a), name, text); err != nil {
					t.Fatal(err)
				}
			}
			a.refreshTree()
			a.open("First.arcmd")
			a.openSplit(a.state.ActiveSpace(), "Second.arcmd", false, false)
			for range 5 {
				tt.PerfFrame()
			}
			if dir := os.Getenv("ARCMD_SPLIT_PROFILE"); dir != "" {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				f, err := os.Create(filepath.Join(dir, fmt.Sprintf("resize-%d.pprof", blocks)))
				if err != nil {
					t.Fatal(err)
				}
				if err := pprof.StartCPUProfile(f); err != nil {
					t.Fatal(err)
				}
				defer func() { pprof.StopCPUProfile(); f.Close() }()
			}
			s := a.split
			x := s.panes[0].bounds.X + s.panes[0].bounds.W + splitGap/2
			y := s.panes[0].bounds.Y + 160
			tt.PerfPointer("press", x, y)
			var timings []float64
			for i := range 160 {
				delta := float32(i % 20)
				if delta > 10 {
					delta = 20 - delta
				}
				_, frame := tt.PerfPointer("move", x+delta*12, y)
				timings = append(timings, float64(frame)/float64(time.Millisecond))
			}
			tt.PerfPointer("release", x, y)
			slices.Sort(timings)
			t.Logf("%d paragraphs/pane: CPU frame median %.3f ms, p95 %.3f ms, max %.3f ms", blocks, timings[len(timings)/2], timings[len(timings)*95/100], timings[len(timings)-1])
		})
	}
}
