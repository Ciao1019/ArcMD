package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	a.open("Journal/Ideas.md")
	a.open("Welcome.md")
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
	id := addSpace(t, a, "Work", map[string]string{"Trips/Travel Plan.md": "# Travel Plan\n\nPack light."})
	typeQuery(a, tt, "travel")
	res := a.results()
	if len(res) == 0 || res[0].title != "Travel Plan" || res[0].hint != "Work / Trips" {
		t.Fatalf("results = %v, hint %q", kinds(res), res[0].hint)
	}
	tt.Key(0, ui.KeyEnter)
	if a.state.ActiveSpace().ID != id || a.doc == nil || a.doc.rel != "Trips/Travel Plan.md" {
		t.Fatalf("not opened in its space: %+v", a.doc)
	}
}

// Text inside documents is found, in every space, and opens with the find
// box on it.
func TestPaletteFindsText(t *testing.T) {
	a, tt := newTestApp(t)
	id := addSpace(t, a, "Work", map[string]string{"Plan.md": "# Plan\n\nThe harbor opens at dawn."})
	typeQuery(a, tt, "harbor")
	res := a.results()
	if len(res) != 1 || res[0].kind != "内容" || res[0].title != "Plan" || res[0].snippet != "The harbor opens at dawn." {
		t.Fatalf("results = %v", kinds(res))
	}
	if !tt.HasText("内容") || !tt.HasText("The harbor opens at dawn.") {
		t.Errorf("hit not shown: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	if a.state.ActiveSpace().ID != id || a.doc == nil || a.doc.rel != "Plan.md" {
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
	writeElsewhere(t, a, "Journal/Ideas.md", "# Ideas\n\nA lighthouse at the end.")
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
				"Plan.md":       "# Plan\n\nThe folders open at dawn, and the harbor is quiet.",
				"Notes/Ship.md": "# Ship\n\nFolders of letters, folders of maps.",
			})
			a.open("Journal/Ideas.md")
			a.open("Welcome.md")
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

func TestAsyncPaletteRefreshAndHistoryMicroGlide(t *testing.T) {
	a, tt := newTestApp(t)
	workID := addSpace(t, a, "Work", map[string]string{
		"Roadmap.md": "# Roadmap\n\nInitial plan.",
	})
	workSpace := a.state.Get(workID)
	if workSpace == nil {
		t.Fatal("missing Work space")
	}

	updates := make(chan func(), 8)
	a.search.async = true
	a.runAsync = func(fn func()) { updates <- fn }
	defer func() {
		a.search.async = false
		a.runAsync = nil
	}()

	// Write a new document in the inactive Work space and open the command bar.
	if err := writeNative(workSpace.Root, "AsyncNote.md", "# AsyncNote\n\nZero blocking."); err != nil {
		t.Fatal(err)
	}
	a.openPalette()
	tt.Frame()
	if !a.palette.open {
		t.Fatal("palette did not open immediately on frame 0")
	}
	// Receive both async callbacks (pageTrees scan + search index refresh).
	for range 2 {
		fn := <-updates
		fn()
	}
	a.palette.query = "AsyncNote"
	tt.Frame()
	res := a.results()
	if len(res) == 0 || res[0].title != "AsyncNote" {
		t.Fatalf("async pageTrees refresh did not discover AsyncNote: %v", res)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()

	// History Back/Forward triggers directional 90ms micro-glide and settles cleanly.
	a.open("Journal/Ideas.md")
	tt.Frame()
	a.open("Welcome.md")
	tt.Frame()
	a.goHistory(-1)
	tt.Frame()
	if a.navDir != -1 {
		t.Fatalf("expected back navigation micro-glide dir=-1, got %d", a.navDir)
	}
	tt.Advance(36 * time.Millisecond)
	if a.navDir != -1 {
		t.Fatal("history micro-glide ended prematurely")
	}
	tt.Advance(100 * time.Millisecond)
	tt.Frame()
	if a.navDir != 0 {
		t.Fatalf("history micro-glide did not settle: navDir=%d", a.navDir)
	}
}
