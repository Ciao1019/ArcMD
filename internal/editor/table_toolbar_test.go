package editor

import "testing"

// TestTableToolbarRowBeyondColumnCount selects rows of a table that has more
// rows than columns. The line toolbar used to index the column edges with the
// selected row number, so selecting any row past the column count panicked
// with "index out of range" and took the whole app down.
func TestTableToolbarRowBeyondColumnCount(t *testing.T) {
	e, tt := newTester("| A | B | C |\n| --- | --- | --- |\n| 1 | 2 | 3 |\n| 4 | 5 | 6 |\n| 7 | 8 | 9 |\n|  |  |  |\n\ntail")
	focus(e, tt)
	tbl := e.Doc.Blocks[0]
	if tbl.Kind != "table" {
		t.Fatalf("want a table, got %q", tbl.Kind)
	}
	if len(tbl.Children) != 5 {
		t.Fatalf("want 5 rows (header, 3 data, blank), got %d", len(tbl.Children))
	}
	for i := 0; i < len(tbl.Children); i++ {
		e.SelectTableLine(tbl.ID, true, i)
		tt.Frame()
		if !e.tableSel.ok {
			t.Fatalf("row %d: selection dropped", i)
		}
	}
	for i := 0; i < len(tbl.Children[0].Children); i++ {
		e.SelectTableLine(tbl.ID, false, i)
		tt.Frame()
		if !e.tableSel.ok {
			t.Fatalf("column %d: selection dropped", i)
		}
	}
}
