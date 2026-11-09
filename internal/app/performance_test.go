//go:build perfbench

package app

import (
	"encoding/json"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

type perfSample struct {
	StepMS  float64 `json:"step_ms"`
	FrameMS float64 `json:"frame_ms"`
	TotalMS float64 `json:"total_ms"`
}

// TestPerformance is opt-in because the million-character fixture and its
// undo history intentionally use substantial time and memory. Run it through
// tools/perf/overlay.go to exclude software rasterization from CPU timing.
func TestPerformance(t *testing.T) {
	fixture := os.Getenv("ARCMD_PERF_FIXTURE")
	output := os.Getenv("ARCMD_PERF_OUTPUT")
	if fixture == "" || output == "" {
		t.Fatal("set ARCMD_PERF_FIXTURE and ARCMD_PERF_OUTPUT")
	}
	count := 100
	if value := os.Getenv("ARCMD_PERF_SAMPLES"); value != "" {
		var err error
		count, err = strconv.Atoi(value)
		if err != nil || count < 1 || count > 1000 {
			t.Fatal("ARCMD_PERF_SAMPLES must be between 1 and 1000")
		}
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := model.Decode(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	a, tt := newTestApp(t)
	tt.PerfPrepare()
	tt.SetSize(1280, 800)
	if err := vault.Write(root(a), "Performance.arcmd", string(raw)); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	start := time.Now()
	a.open("Performance.arcmd")
	tt.PerfFrame()
	openMS := float64(time.Since(start)) / float64(time.Millisecond)
	t.Logf("Open and first CPU scene: %.3f ms", openMS)
	if a.doc.loadErr != nil {
		t.Fatal(a.doc.loadErr)
	}
	ed := a.doc.ed
	var caret int
	for _, leaf := range ed.Doc.Leaves() {
		if leaf.Block.Kind == "paragraph" {
			caret = leaf.End
			break
		}
	}
	if caret == 0 {
		t.Fatal("fixture has no first paragraph")
	}
	ed.SetSelection(caret, caret)
	ed.RequestFocus = true
	tt.PerfFrame()
	if active, _ := tt.TextInputActive(); !active {
		t.Fatal("editor did not receive text input focus")
	}
	before := ed.Text()
	snapshot := func(name string) {
		f, err := os.Create(filepath.Join(output, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.PerfSnapshot()); err != nil {
			t.Fatal(err)
		}
	}
	snapshot("before")
	if path := os.Getenv("ARCMD_PERF_PROFILE"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			t.Fatal(err)
		}
		defer pprof.StopCPUProfile()
	}
	results := map[string]any{"fixture": fixture, "characters_with_separators": utf8.RuneCountInString(before),
		"top_level_blocks": len(doc.Blocks), "leaves": len(doc.Leaves()), "open_cpu_ms": openMS,
		"go": runtime.Version(), "gomaxprocs": runtime.GOMAXPROCS(0), "width": 1280, "height": 800, "scale": 2,
		"samples_per_operation": count,
		"timing":                "one event + one CPU scene; no rasterization, GPU presentation or display latency"}
	partial, err := os.Create(filepath.Join(output, "samples.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer partial.Close()
	encoder := json.NewEncoder(partial)
	for _, action := range []string{"type", "backspace", "scroll"} {
		samples := make([]perfSample, count)
		var m0, m1 runtime.MemStats
		runtime.ReadMemStats(&m0)
		for i := range samples {
			step, frame := tt.PerfOperation(action)
			samples[i] = perfSample{float64(step) / 1e6, float64(frame) / 1e6, float64(step+frame) / 1e6}
			if err := encoder.Encode(map[string]any{"operation": action, "index": i, "sample": samples[i]}); err != nil {
				t.Fatal(err)
			}
		}
		runtime.ReadMemStats(&m1)
		results[action] = map[string]any{"samples": samples, "allocated_bytes": m1.TotalAlloc - m0.TotalAlloc,
			"gc_cycles": m1.NumGC - m0.NumGC, "heap_alloc_bytes_after": m1.HeapAlloc}
		values := make([]float64, len(samples))
		for i, s := range samples {
			values[i] = s.FrameMS
		}
		slices.Sort(values)
		q := func(p float64) float64 { return values[int(math.Round(float64(len(values)-1)*p))] }
		if count >= 100 {
			t.Logf("%s frame p50 %.3f p95 %.3f max %.3f ms; %.1f MB allocated; %d GC cycles", action, q(.5), q(.95), q(1), float64(m1.TotalAlloc-m0.TotalAlloc)/1e6, m1.NumGC-m0.NumGC)
		} else {
			t.Logf("%s diagnostic n=%d frame median %.3f range %.3f–%.3f ms; %.1f MB allocated; %d GC cycles", action, count, q(.5), q(0), q(1), float64(m1.TotalAlloc-m0.TotalAlloc)/1e6, m1.NumGC-m0.NumGC)
		}
		switch action {
		case "type":
			at := model.ByteOffset(before, caret)
			if ed.Text() != before[:at]+strings.Repeat("x", count)+before[at:] {
				t.Fatal("typing did not insert the expected characters at the first paragraph end")
			}
			snapshot("typed")
		case "backspace":
			if ed.Text() != before {
				t.Fatal("backspace did not restore the original text")
			}
		case "scroll":
			results["scroll_y"] = ed.Scroll.Y
			want := float32(count * 40)
			if ed.Scroll.Y < want-1 || ed.Scroll.Y > want+1 {
				t.Fatalf("scroll distance = %v, want %v DIP", ed.Scroll.Y, want)
			}
		}
	}
	snapshot("scrolled")
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "results.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
