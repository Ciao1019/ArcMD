package editor

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

const (
	mapMaxCodeLines = 14
	mapMaxCodeRunes = 1200
)

type mapCodePreview struct {
	layout     *ui.TextLayout
	w, h       float32
	totalLines int
	clipped    bool
}

// Shape all the source. Width follows the longest visible line up to the wrap
// width; height follows every wrapped line. Tabs expand only for presentation.
func mapCodeLayout(text, language string, pg Page, limit float32) (*ui.TextLayout, float32, float32) {
	return mapCodeLayoutBounded(text, language, pg, limit)
}

func mapCodePreviewLayout(text, language string, pg Page, limit float32) mapCodePreview {
	totalLines := 1 + strings.Count(text, "\n")
	capped, clipped := capCodePreviewText(text, mapMaxCodeLines, mapMaxCodeRunes)
	l, w, h := mapCodeLayoutBounded(capped, language, pg, limit)
	return mapCodePreview{layout: l, w: w, h: h, totalLines: totalLines, clipped: clipped}
}

func mapCodeLayoutBounded(text, language string, pg Page, limit float32) (*ui.TextLayout, float32, float32) {
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

func capCodePreviewText(text string, maxLines, maxRunes int) (string, bool) {
	if text == "" {
		return "", false
	}
	lines := 1
	runes := 0
	for i := 0; i < len(text); {
		if text[i] == '\n' {
			if maxLines > 0 && lines >= maxLines {
				return strings.TrimRight(text[:i], "\r"), true
			}
			lines++
			i++
			runes++
			continue
		}
		if maxRunes > 0 && runes >= maxRunes {
			return text[:i] + "…", true
		}
		_, sz := utf8.DecodeRuneInString(text[i:])
		i += sz
		runes++
	}
	return text, false
}
