package editor

import (
	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/ui"
	"strings"
	"testing"
)

func TestMapCodeWholeSourceAndIntrinsicWidth(t *testing.T) {
	pg := pageOf(Page{}, ui.LightTheme())
	_, w, h := mapCodeLayout("x = 1", "python", pg, 180)
	if w >= 100 || h > 35 {
		t.Fatalf("short snippet padded to %f x %f", w, h)
	}
	source := "inputs, outputs = data.iloc[:, 0:2], data.iloc[:, 2]\ninputs = inputs.fillna(inputs.mean())\n" + strings.Repeat("一行很长的中文🦊", 30) + "\n\nlast = 42"
	layout, longW, longH := mapCodeLayout(source, "python", pg, 180)
	lines := layout.Lines()
	if len(lines) <= 5 || longW > 182 || longH <= h*5 {
		t.Fatalf("not wrapped to width: %v %f x %f", len(lines), longW, longH)
	}
	if last := lines[len(lines)-1]; last.End != document.Len(source) {
		t.Fatalf("missing source end: %d vs %d", last.End, document.Len(source))
	}
}
