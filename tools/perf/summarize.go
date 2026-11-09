package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"

	"github.com/Ciao1019/ArcMD/tools/internal/toolutil"
)

type quantile struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	Max float64 `json:"max"`
	Min float64 `json:"min,omitempty"`
}
type quietOperation struct {
	Step  quantile `json:"step"`
	Frame quantile `json:"frame"`
}
type frameSample struct {
	FrameMS *float64 `json:"frame_ms"`
}
type arcOperation struct {
	Samples []frameSample `json:"samples"`
}

var operationNames = []string{"type", "backspace", "scroll"}
var benchPattern = regexp.MustCompile(`bench "([^"]+)" x(\d+): step \[p50 ([\d.]+) p95 ([\d.]+) max ([\d.]+)\] frame \[p50 ([\d.]+) p95 ([\d.]+) max ([\d.]+)\]`)

func parseQuietLog(log string, samples int) (map[string]quietOperation, error) {
	operations := map[string]string{"type x": "type", "key backspace": "backspace", "wheel 900 500 40": "scroll"}
	result := map[string]quietOperation{}
	for _, match := range benchPattern.FindAllStringSubmatch(log, -1) {
		name, ok := operations[match[1]]
		if !ok {
			return nil, fmt.Errorf("unexpected benchmark %q", match[1])
		}
		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("duplicate benchmark %s", name)
		}
		count, _ := strconv.Atoi(match[2])
		if count != samples {
			return nil, fmt.Errorf("unexpected Quiet.md sample count: %d", count)
		}
		values := make([]float64, 6)
		for i, s := range match[3:] {
			v, err := strconv.ParseFloat(s, 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, fmt.Errorf("invalid benchmark value %q", s)
			}
			values[i] = v
		}
		result[name] = quietOperation{quantile{P50: values[0], P95: values[1], Max: values[2]}, quantile{P50: values[3], P95: values[4], Max: values[5]}}
	}
	if len(result) != 3 {
		return nil, fmt.Errorf("missing Quiet.md benchmark operations")
	}
	return result, nil
}
func quantiles(values []float64) (quantile, error) {
	if len(values) == 0 {
		return quantile{}, fmt.Errorf("empty benchmark samples")
	}
	values = slices.Clone(values)
	for _, v := range values {
		if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return quantile{}, fmt.Errorf("invalid frame duration")
		}
	}
	slices.Sort(values)
	at := func(q float64) float64 { return values[int(math.Round(float64(len(values)-1)*q))] }
	return quantile{P50: at(.5), P95: at(.95), Max: values[len(values)-1], Min: values[0]}, nil
}
func median(values []float64) float64 {
	values = slices.Clone(values)
	slices.Sort(values)
	n := len(values)
	if n%2 == 1 {
		return values[n/2]
	}
	return (values[n/2-1] + values[n/2]) / 2
}
func summarizeDirectory(dir string) (map[string]any, error) {
	var metadata map[string]json.RawMessage
	var fixture any
	if err := toolutil.ReadJSON(filepath.Join(dir, "metadata.json"), &metadata); err != nil {
		return nil, err
	}
	var runs, samples int
	var size string
	if err := json.Unmarshal(metadata["runs"], &runs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(metadata["samples"], &samples); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(metadata["size"], &size); err != nil {
		return nil, err
	}
	if runs < 1 || samples < 1 {
		return nil, fmt.Errorf("invalid run or sample count")
	}
	if !slices.Contains([]string{"10k", "100k", "1m"}, size) {
		return nil, fmt.Errorf("unknown fixture size %q", size)
	}
	if err := toolutil.ReadJSON(filepath.Join(dir, "fixtures/manifest.json"), &fixture); err != nil {
		return nil, err
	}
	var rounds []map[string]any
	all := map[string]map[string][]quantile{}
	for _, op := range operationNames {
		all[op] = map[string][]quantile{"arcmd": {}, "quiet": {}}
	}
	for i := 1; i <= runs; i++ {
		var arcmd map[string]json.RawMessage
		if err := toolutil.ReadJSON(filepath.Join(dir, fmt.Sprintf("arcmd-%d/results.json", i)), &arcmd); err != nil {
			return nil, err
		}
		var count int
		if err := json.Unmarshal(arcmd["samples_per_operation"], &count); err != nil {
			return nil, err
		}
		if count != samples {
			return nil, fmt.Errorf("unexpected ArcMD sample count")
		}
		quietLog, err := readText(filepath.Join(dir, fmt.Sprintf("quiet-%d/log", i)))
		if err != nil {
			return nil, err
		}
		quiet, err := parseQuietLog(quietLog, samples)
		if err != nil {
			return nil, err
		}
		arcLog, err := readText(filepath.Join(dir, fmt.Sprintf("arcmd-%d.log", i)))
		if err != nil {
			return nil, err
		}
		for _, op := range operationNames {
			var a arcOperation
			if err := json.Unmarshal(arcmd[op], &a); err != nil {
				return nil, err
			}
			if len(a.Samples) != samples {
				return nil, fmt.Errorf("%s has %d samples, expected %d", op, len(a.Samples), samples)
			}
			values := make([]float64, len(a.Samples))
			for j, s := range a.Samples {
				if s.FrameMS == nil {
					return nil, fmt.Errorf("%s sample %d is missing frame_ms", op, j)
				}
				values[j] = *s.FrameMS
			}
			q, err := quantiles(values)
			if err != nil {
				return nil, err
			}
			all[op]["arcmd"] = append(all[op]["arcmd"], q)
			all[op]["quiet"] = append(all[op]["quiet"], quiet[op].Frame)
		}
		rounds = append(rounds, map[string]any{"arcmd": arcmd, "quiet": quiet, "quiet_log": quietLog, "arcmd_log": arcLog})
	}
	summary := map[string]map[string]quantile{}
	for _, op := range operationNames {
		summary[op] = map[string]quantile{}
		for _, product := range []string{"arcmd", "quiet"} {
			var p50, p95, max []float64
			for _, q := range all[op][product] {
				p50 = append(p50, q.P50)
				p95 = append(p95, q.P95)
				max = append(max, q.Max)
			}
			summary[op][product] = quantile{P50: median(p50), P95: median(p95), Max: median(max)}
		}
	}
	return map[string]any{"metadata": metadata, "fixture": fixture, "rounds": rounds, "summary": summary}, nil
}
func summarizeCommand(args []string) error {
	flags := flag.NewFlagSet("summarize", flag.ContinueOnError)
	out := flags.String("out", "", "report JSON path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *out == "" || flags.NArg() == 0 {
		return fmt.Errorf("summarize requires --out and at least one run directory")
	}
	sizes := map[string]any{}
	for _, dir := range flags.Args() {
		entry, err := summarizeDirectory(dir)
		if err != nil {
			return err
		}
		var size string
		metadata := entry["metadata"].(map[string]json.RawMessage)
		if err := json.Unmarshal(metadata["size"], &size); err != nil {
			return err
		}
		if _, ok := sizes[size]; ok {
			return fmt.Errorf("duplicate fixture size %s", size)
		}
		sizes[size] = entry
	}
	report := map[string]any{"aggregation": "Median of the per-round quantiles; not a pooled P95. Ignore P95 for short diagnostics.", "sizes": sizes}
	if err := toolutil.WriteJSON(*out, report); err != nil {
		return err
	}
	fmt.Println("| Size | Operation | ArcMD P50 | Quiet.md P50 | ArcMD P95 | Quiet.md P95 |\n|---|---|---:|---:|---:|---:|")
	for _, size := range []string{"10k", "100k", "1m"} {
		if raw, ok := sizes[size]; ok {
			entry := raw.(map[string]any)
			var count int
			if err := json.Unmarshal(entry["metadata"].(map[string]json.RawMessage)["samples"], &count); err != nil {
				return err
			}
			for _, op := range operationNames {
				products := entry["summary"].(map[string]map[string]quantile)[op]
				a, q := products["arcmd"], products["quiet"]
				tail := "short sample | short sample"
				if count >= 100 {
					tail = fmt.Sprintf("%.2f | %.2f", a.P95, q.P95)
				}
				fmt.Printf("| %s | %s | %.2f | %.2f | %s |\n", size, op, a.P50, q.P50, tail)
			}
		}
	}
	return nil
}
