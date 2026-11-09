package editor

import (
	"bytes"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/ui"
)

func TestIncrementalLayoutMatchesCompleteLayout(t *testing.T) {
	for _, kind := range []string{"paragraph", "bullet", "numbered", "quote", "callout", "code", "heading", "cell"} {
		t.Run(kind, func(t *testing.T) {
			e, tt := newTester("# Title\n\nfirst paragraph\n\n- parent\n  - child\n\n1. first\n2. second\n\n> quote\n\n> [!TIP]\n> callout\n\n```go\ncode\n```\n\n## Heading\n\n| A | B |\n|---|---|\n| a | b |\n\nafter")
			e.Engine.Insert(document.Columns())
			for _, block := range e.Doc.Blocks {
				if block.Kind == "table" {
					e.Engine.MergeCells(block.ID, document.CellRange{Row0: 0, Col0: 0, Row1: 0, Col1: 1})
				}
			}
			tt.Frame()
			focus(e, tt)
			var target document.Leaf
			for _, leaf := range e.Doc.Leaves() {
				if leaf.Block.Kind == kind {
					target = leaf
					break
				}
			}
			e.SetSelection(target.End, target.End)
			tt.Frame()
			check := func() {
				t.Helper()
				pixels := slices.Clone(tt.Image().Pix)
				rows := slices.Clone(e.rows)
				height := e.height
				decorations := slices.Clone(e.decorations)
				handles := slices.Clone(e.handles)
				folds := slices.Clone(e.foldButtons)
				outline := slices.Clone(e.outline)
				resizers := slices.Clone(e.resizers)
				e.ready = false
				tt.Frame()
				if len(rows) != len(e.rows) || math.Abs(float64(height-e.height)) > .001 {
					t.Fatal("incremental layout changed the document extent")
				}
				for i, row := range rows {
					full := e.rows[i]
					if row.leaf.Start != full.leaf.Start || row.leaf.End != full.leaf.End || math.Abs(float64(row.y-full.y)) > .001 || math.Abs(float64(row.rect.H-full.rect.H)) > .001 {
						t.Fatalf("incremental row %d differs from complete layout", i)
					}
				}
				if !reflect.DeepEqual(decorations, e.decorations) || !reflect.DeepEqual(handles, e.handles) || !reflect.DeepEqual(folds, e.foldButtons) || !reflect.DeepEqual(outline, e.outline) || !reflect.DeepEqual(resizers, e.resizers) {
					t.Fatal("incremental decorations or controls differ")
				}
				if !bytes.Equal(pixels, tt.Image().Pix) {
					t.Fatal("incremental layout changed rendered pixels")
				}
				if e.wordCount() != countWords(e.Doc.PlainText()) {
					t.Fatalf("incremental word count differs: got %d, want %d", e.wordCount(), countWords(e.Doc.PlainText()))
				}
			}
			for _, text := range []string{"😀", strings.Repeat("中文 wrap ", 35), "\u2028next"} {
				tt.Type(text)
				// Bring the following merged table into view before comparing
				// against a full layout, including its bars and cell rules.
				for _, d := range e.decorations {
					if d.kind == "table" {
						e.Scroll.Y = d.rect.Y - 50
						break
					}
				}
				tt.Frame()
				check()
			}
			tt.Key(ui.Cmd, ui.KeyZ)
			check()
			tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
			check()
			tt.Compose("候选", 2)
			check()
			tt.Type("中文")
			check()
			// Multiple edits without a frame must take the complete-layout path.
			e.ReplaceText(nil, "one")
			e.ReplaceText(nil, "two")
			tt.Frame()
			check()
		})
	}
}
