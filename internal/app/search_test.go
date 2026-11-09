package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// typeQuery opens the command bar and types q into it.
func typeQuery(a *App, tt *ui.Tester, q string) {
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	a.palette.query = q
	tt.Frame()
}

// addSpace adds a space holding the documents, markdown by path.
func addSpace(t *testing.T, a *App, name string, docs map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, md := range docs {
		if err := writeNative(dir, rel, md); err != nil {
			t.Fatal(err)
		}
	}
	return a.state.Add(name, dir).ID
}

func kinds(res []action) []string {
	var out []string
	for _, r := range res {
		out = append(out, r.kind+":"+r.title)
	}
	return out
}

// Before typing, the bar lists the documents last shown.
func TestPaletteRecents(t *testing.T) {
	a, tt := newTestApp(t)
	a.open("Journal/Ideas.arcmd")
	a.open("Welcome.arcmd")
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	res := a.results()
	if len(res) == 0 || res[0].kind != "最近" || res[0].title != "Ideas" {
		t.Fatalf("results = %v", kinds(res))
	}
	for _, r := range res {
		if r.kind == "最近" && r.title == "Welcome" {
			t.Error("the open document listed as recent")
		}
	}
	if !tt.HasText("最近") || !tt.HasText("Journal") {
		t.Errorf("recent row not shown: %q", tt.Texts())
	}
}

// Documents of every space are found by name, and open in their space.
func TestPaletteFindsOtherSpaces(t *testing.T) {
	a, tt := newTestApp(t)
	id := addSpace(t, a, "Work", map[string]string{"Trips/Travel Plan.arcmd": "# Travel Plan\n\nPack light."})
	typeQuery(a, tt, "travel")
	res := a.results()
	if len(res) == 0 || res[0].title != "Travel Plan" || res[0].hint != "Work / Trips" {
		t.Fatalf("results = %v, hint %q", kinds(res), res[0].hint)
	}
	tt.Key(0, ui.KeyEnter)
	if a.state.ActiveSpace().ID != id || a.doc == nil || a.doc.rel != "Trips/Travel Plan.arcmd" {
		t.Fatalf("not opened in its space: %+v", a.doc)
	}
}

// Text inside documents is found, in every space, and opens with the find
// box on it.
func TestPaletteFindsText(t *testing.T) {
	a, tt := newTestApp(t)
	id := addSpace(t, a, "Work", map[string]string{"Plan.arcmd": "# Plan\n\nThe harbor opens at dawn."})
	typeQuery(a, tt, "harbor")
	res := a.results()
	if len(res) != 1 || res[0].kind != "内容" || res[0].title != "Plan" || res[0].snippet != "The harbor opens at dawn." {
		t.Fatalf("results = %v", kinds(res))
	}
	if !tt.HasText("内容") || !tt.HasText("The harbor opens at dawn.") {
		t.Errorf("hit not shown: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	if a.state.ActiveSpace().ID != id || a.doc == nil || a.doc.rel != "Plan.arcmd" {
		t.Fatalf("hit not opened: %+v", a.doc)
	}
	if q, open := a.doc.ed.FindQuery(); !open || q != "harbor" {
		t.Errorf("find box %v on %q", open, q)
	}
	tt.Frame()
	if !tt.HasText("1/1") {
		t.Errorf("match count not shown: %q", tt.Texts())
	}
}

// The open document is searched as it is, unsaved edits included, and a
// document listed by its name is not listed again for its text.
func TestPaletteSearchesLiveText(t *testing.T) {
	a, tt := newTestApp(t)
	a.doc.ed.SetSelection(0, 0)
	a.doc.ed.ReplaceText(nil, "zebra ")
	typeQuery(a, tt, "zebra")
	res := a.results()
	if len(res) != 1 || res[0].kind != "内容" || res[0].title != "Welcome" {
		t.Fatalf("unsaved text not found: %v", kinds(res))
	}
	typeQuery(a, tt, "ideas")
	for _, r := range a.results() {
		if r.kind == "内容" && r.title == "Ideas" {
			t.Errorf("Ideas listed twice: %v", kinds(a.results()))
		}
	}
	// One ASCII letter is too little to search text for.
	typeQuery(a, tt, "z")
	for _, r := range a.results() {
		if r.kind == "内容" {
			t.Errorf("text searched for one letter: %v", kinds(a.results()))
		}
	}
}

// A document changed on disk is found by its new text the next time the
// bar opens.
func TestPaletteIndexRefreshes(t *testing.T) {
	a, tt := newTestApp(t)
	typeQuery(a, tt, "lighthouse")
	if len(a.results()) != 0 {
		t.Fatal("found before written")
	}
	tt.Key(0, ui.KeyEscape)
	writeElsewhere(t, a, "Journal/Ideas.arcmd", "# Ideas\n\nA lighthouse at the end.")
	typeQuery(a, tt, "lighthouse")
	res := a.results()
	if len(res) != 1 || res[0].title != "Ideas" || !strings.Contains(res[0].snippet, "lighthouse") {
		t.Errorf("results = %v", kinds(res))
	}
}

// TestPaletteSnapshot renders the bar before typing and with text hits,
// for review.
func TestPaletteSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the command bar")
	for _, name := range []string{"idle", "text"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			addSpace(t, a, "Work", map[string]string{
				"Plan.arcmd":       "# Plan\n\nThe folders open at dawn, and the harbor is quiet.",
				"Notes/Ship.arcmd": "# Ship\n\nFolders of letters, folders of maps.",
			})
			a.open("Journal/Ideas.arcmd")
			a.open("Welcome.arcmd")
			q := ""
			if name == "text" {
				q = "folders"
			}
			typeQuery(a, tt, q)
			for range 3 {
				tt.Frame()
			}
			savePNG(t, filepath.Join(dir, "palette-"+name+".png"), tt)
		})
	}
}
