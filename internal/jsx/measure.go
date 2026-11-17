package jsx

import (
	_ "embed"
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

//go:embed measure.js
var MeasureScript string

const (
	HeightMin  = 80
	HeightMax  = 2000
	HeightSlop = 2
)

func ParseMeasure(v any) (height float32, ready bool, ok bool) {
	switch n := v.(type) {
	case nil:
		return 0, false, false
	case float64:
		return finiteHeight(n)
	case float32:
		return finiteHeight(float64(n))
	case int:
		return finiteHeight(float64(n))
	case int32:
		return finiteHeight(float64(n))
	case int64:
		return finiteHeight(float64(n))
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, false, false
		}
		return finiteHeight(f)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return 0, false, false
		}
		return finiteHeight(f)
	case map[string]any:
		h, _, parsed := ParseMeasure(n["height"])
		if !parsed {
			return 0, false, false
		}
		ready, _ = n["ready"].(bool)
		return h, ready, true
	default:
		return 0, false, false
	}
}

func finiteHeight(n float64) (float32, bool, bool) {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false, false
	}
	return float32(math.Round(n)), true, true
}

func ClampHeight(h float32) float32 {
	if math.IsNaN(float64(h)) || math.IsInf(float64(h), 0) {
		return HeightMin
	}
	return min(HeightMax, max(HeightMin, h))
}

func HeightChanged(old, next float32) bool {
	d := old - next
	if d < 0 {
		d = -d
	}
	return d > HeightSlop
}

func ScriptIsExpression(script string) bool {
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || trimmed == "" {
			continue
		}
		// MyGo evalExpr wraps the source in await(\ncode\n). A top-level
		// statement is a SyntaxError; a parenthesized IIFE is not.
		return strings.HasPrefix(trimmed, "(")
	}
	return false
}
