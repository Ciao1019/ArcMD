package editor

import (
	"math"
	"strings"

	"github.com/egoist/mygo/ui"
)

// Shape all the source. Width follows the longest visible line up to the wrap
// width; height follows every wrapped line. Tabs expand only for presentation.
func mapCodeLayout(text, language string, pg Page, limit float32) (*ui.TextLayout, float32, float32) {
	font := ui.Font{Family: "monospace", Size: 12, LineHeight: 1.25}
	spans := codeSpansFor("", language, strings.ReplaceAll(text, "\t", "    "), pg, nil)
	l := ui.ShapeRichText(spans, font, limit)
	_, h := l.Size()
	var w float32
	for _, line := range l.Lines() {
		w = max(w, line.Rect.X+line.Rect.W)
	}
	return l, max(12, float32(math.Ceil(float64(w)))), max(15, float32(math.Ceil(float64(h))))
}
