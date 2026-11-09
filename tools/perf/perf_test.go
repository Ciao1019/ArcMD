package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/tools/internal/toolutil"
)

func TestConvertPreservesRichContent(t *testing.T) {
	raw := `<title>你好 📝</title><h1>Chapter</h1><p>A<b>粗<i>斜</i>体</b><span text-color="red" background-color="yellow">色</span>Z</p><ul><li>one</li><li>two</li></ul><checkbox done="true">done</checkbox><callout border-color="orange" emoji="💡"><p>note</p></callout><pre lang="go"><code>  x &lt; y

</code></pre><table><colgroup><col width="120"/></colgroup><tbody><tr><td background-color="blue"><p>cell</p></td></tr></tbody></table>`
	dir := t.TempDir()
	source := filepath.Join(dir, "source.xml")
	if err := os.WriteFile(source, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "fixtures")
	m, err := convert(source, out)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "1m.arcmd"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := model.Decode(string(data))
	if err != nil {
		t.Fatal(err)
	}
	want := "你好 📝ChapterA粗斜体色Zonetwodonenote  x < y\n\ncell"
	if got := blockText(doc.Blocks); got != want || m.TextSHA != toolutil.Hash([]byte(want)) {
		t.Fatalf("changed text: %q", got)
	}
	runs := doc.Blocks[2].Runs
	if !runs[2].Style.Bold || !runs[2].Style.Italic || runs[3].Style.Italic || runs[4].Style.Color != "red" || runs[4].Style.Highlight != "yellow" {
		t.Fatalf("lost inherited inline styles: %+v", runs)
	}
	if !doc.Blocks[5].Checked || doc.Blocks[6].Color != "yellow" || doc.Blocks[6].Emoji != "💡" || doc.Blocks[7].Language != "go" {
		t.Fatal("lost block attributes")
	}
	table := doc.Blocks[8]
	if len(table.Widths) != 1 || table.Widths[0] != 120 || table.Children[0].Children[0].Background != "blue" || m.Chapters != 1 {
		t.Fatal("lost table or chapter data")
	}
	seen := map[string]bool{}
	walkBlocks(doc.Blocks, func(b *model.Block) {
		if seen[b.ID] || b.ID == "" {
			t.Fatalf("invalid stable ID %q", b.ID)
		}
		seen[b.ID] = true
	})
	if _, err := convert(source, out); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(filepath.Join(out, "1m.arcmd"))
	if string(again) != string(data) {
		t.Fatal("conversion is not deterministic")
	}
}

func TestConvertRejectsUnsupportedOrMalformedContent(t *testing.T) {
	for _, raw := range []string{`<p>broken`, `<video/>`, `<p><a href="x">link</a></p>`, `<pre><code><b>x</b></code></pre>`, `<callout border-color="unknown"><p>x</p></callout>`} {
		t.Run(raw, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "source.xml")
			if err := os.WriteFile(source, []byte(raw), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := convert(source, filepath.Join(dir, "out")); err == nil {
				t.Fatal("accepted unsupported fixture")
			}
		})
	}
}

func TestQuantilesAndQuietLogValidation(t *testing.T) {
	values := []float64{9, 1, 3, 2}
	got, err := quantiles(values)
	if err != nil || got != (quantile{P50: 3, P95: 9, Min: 1, Max: 9}) || values[0] != 9 {
		t.Fatalf("unexpected quantiles: %+v, %v", got, err)
	}
	for _, invalid := range [][]float64{nil, {-1}, {math.NaN()}, {math.Inf(1)}} {
		if _, err := quantiles(invalid); err == nil {
			t.Fatal("accepted invalid duration")
		}
	}
	log := testQuietLog(100)
	if operations, err := parseQuietLog(log, 100); err != nil || operations["scroll"].Frame.P95 != 2 {
		t.Fatalf("valid benchmark rejected: %v", err)
	}
	for _, invalid := range []string{log + log, log[:strings.LastIndex(log, "bench")], strings.Replace(log, "x100", "x99", 1)} {
		if _, err := parseQuietLog(invalid, 100); err == nil {
			t.Fatal("accepted incomplete or inconsistent log")
		}
	}
}

func testQuietLog(samples int) string {
	var b strings.Builder
	for _, name := range []string{"type x", "key backspace", "wheel 900 500 40"} {
		fmt.Fprintf(&b, "bench %q x%d: step [p50 1 p95 2 max 3] frame [p50 1 p95 2 max 3]\n", name, samples)
	}
	return b.String()
}

func TestSummaryUsesMedianOfRoundQuantiles(t *testing.T) {
	dir := t.TempDir()
	write := func(path string, value any) {
		t.Helper()
		if err := toolutil.WriteJSON(filepath.Join(dir, path), value); err != nil {
			t.Fatal(err)
		}
	}
	write("metadata.json", map[string]any{"runs": 3, "samples": 3, "size": "10k"})
	write("fixtures/manifest.json", map[string]any{"text_characters": 10000})
	for i, values := range [][]float64{{1, 2, 100}, {3, 4, 5}, {6, 7, 8}} {
		result := map[string]any{"samples_per_operation": 3}
		for _, op := range operationNames {
			var samples []frameSample
			for _, v := range values {
				samples = append(samples, frameSample{&v})
			}
			result[op] = arcOperation{samples}
		}
		write(fmt.Sprintf("arcmd-%d/results.json", i+1), result)
		quietDir := filepath.Join(dir, fmt.Sprintf("quiet-%d", i+1))
		if err := os.Mkdir(quietDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for path, data := range map[string]string{filepath.Join(quietDir, "log"): testQuietLog(3), filepath.Join(dir, fmt.Sprintf("arcmd-%d.log", i+1)): "PASS"} {
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	entry, err := summarizeDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := entry["summary"].(map[string]map[string]quantile)["type"]["arcmd"]
	if !reflect.DeepEqual(got, quantile{P50: 4, P95: 8, Max: 8}) {
		t.Fatalf("rounds were pooled or quantiles changed: %+v", got)
	}
	write("arcmd-1/results.json", map[string]any{"samples_per_operation": 3, "type": arcOperation{make([]frameSample, 3)}})
	if _, err := summarizeDirectory(dir); err == nil || !strings.Contains(err.Error(), "missing frame_ms") {
		t.Fatalf("accepted missing frame measurements: %v", err)
	}
}

func TestOverlayRejectsChangedAnchors(t *testing.T) {
	for _, source := range []string{"package ui", "type headless struct {\ntype headless struct {\n\th.img.Render(s)\nh := &headless{w:"} {
		if _, err := patchHeadless(source); err == nil {
			t.Fatal("accepted changed adapter anchors")
		}
	}
}
