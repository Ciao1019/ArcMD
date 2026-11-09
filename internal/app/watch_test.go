package app

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// fakeWatches stands in for mygo.WatchFiles: it keeps each folder's
// callback, which tests call as the system would.
type fakeWatches struct {
	changed map[string]func([]mygo.FileEvent)
	stopped []string
}

func watchApp(t *testing.T) (*App, *ui.Tester, *fakeWatches) {
	t.Helper()
	a, tt := newTestApp(t)
	w := &fakeWatches{changed: map[string]func([]mygo.FileEvent){}}
	a.watchFiles = func(root string, changed func([]mygo.FileEvent)) func() {
		w.changed[root] = changed
		return func() {
			delete(w.changed, root)
			w.stopped = append(w.stopped, root)
		}
	}
	tt.Frame()
	return a, tt, w
}

// fire reports events in the active space's folder, then draws a frame.
func (w *fakeWatches) fire(t *testing.T, a *App, tt *ui.Tester, events ...mygo.FileEvent) {
	t.Helper()
	changed := w.changed[root(a)]
	if changed == nil {
		t.Fatalf("%s not watched: %v", root(a), w.changed)
	}
	changed(events)
	tt.Frame()
}

// writeElsewhere writes markdown to rel as another app would.
func writeElsewhere(t *testing.T, a *App, rel, markdown string) {
	t.Helper()
	text, err := editor.ImportMarkdown(markdown).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root(a), rel, text); err != nil {
		t.Fatal(err)
	}
}

func written(rel string) mygo.FileEvent { return mygo.FileEvent{Op: mygo.FileWrite, Path: rel} }

// conflictText is the bar's, not the toast's, which HasText would match too.
const conflictText = "这里还有未保存的修改"

// A document without edits follows its file.
func TestWatchReloadsCleanDocument(t *testing.T) {
	a, tt, w := watchApp(t)
	writeElsewhere(t, a, "Welcome.arcmd", "# Welcome\n\nChanged in another editor.")
	w.fire(t, a, tt, written("Welcome.arcmd"))
	if !strings.Contains(a.doc.text(), "Changed in another editor.") || a.doc.dirty() || a.doc.conflict {
		t.Fatalf("not reloaded: %q", tt.Texts())
	}
	// ArcMD's own save is reported too, and changes nothing.
	a.doc.ed.SetSelection(0, 0)
	a.doc.ed.ReplaceText(nil, "Hi ")
	tt.Frame()
	tt.Advance(autosaveDelay + 50*time.Millisecond)
	tt.Frame()
	w.fire(t, a, tt, written("Welcome.arcmd"))
	if a.doc.conflict || a.doc.dirty() || tt.HasText(conflictText) {
		t.Error("own save seen as a change elsewhere")
	}
}

// Unsaved edits are never overwritten by, nor overwrite, a change elsewhere
// until the user chooses.
func TestWatchGuardsUnsavedEdits(t *testing.T) {
	for _, keep := range []bool{true, false} {
		a, tt, w := watchApp(t)
		a.doc.ed.SetSelection(0, 0)
		a.doc.ed.ReplaceText(nil, "Mine ")
		writeElsewhere(t, a, "Welcome.arcmd", "# Theirs")
		theirs, _ := vault.Read(root(a), "Welcome.arcmd")
		w.fire(t, a, tt, written("Welcome.arcmd"))
		if !a.doc.conflict || !tt.HasText(conflictText) {
			t.Fatalf("no conflict bar: %q", tt.Texts())
		}
		tt.Advance(3 * autosaveDelay)
		tt.Frame()
		if got, _ := vault.Read(root(a), "Welcome.arcmd"); got != theirs {
			t.Fatal("autosave overwrote the change elsewhere")
		}
		// Leaving the document waits too.
		if a.flush() {
			t.Fatal("flush let the conflict go")
		}
		if keep {
			if err := tt.Click("保留我的修改"); err != nil {
				t.Fatal(err)
			}
			got, _ := vault.Read(root(a), "Welcome.arcmd")
			if !strings.Contains(got, "Mine Welcome") || a.doc.dirty() {
				t.Errorf("mine not kept: dirty %v", a.doc.dirty())
			}
		} else {
			if err := tt.Click("使用磁盘上的版本"); err != nil {
				t.Fatal(err)
			}
			if text := a.doc.text(); !strings.Contains(text, "Theirs") || strings.Contains(text, "Mine") || a.doc.dirty() {
				t.Errorf("theirs not shown: %q", tt.Texts())
			}
		}
		tt.Frame()
		if a.doc.conflict || tt.HasText(conflictText) {
			t.Error("bar left after choosing")
		}
	}
}

// Without a watch, saving still notices the file changed.
func TestSaveNoticesChangeWithoutWatch(t *testing.T) {
	a, tt := newTestApp(t)
	a.doc.ed.SetSelection(0, 0)
	a.doc.ed.ReplaceText(nil, "Mine ")
	writeElsewhere(t, a, "Welcome.arcmd", "# Theirs")
	theirs, _ := vault.Read(root(a), "Welcome.arcmd")
	tt.Frame()
	tt.Advance(autosaveDelay + 50*time.Millisecond)
	tt.Frame()
	if got, _ := vault.Read(root(a), "Welcome.arcmd"); got != theirs {
		t.Fatal("save overwrote the change elsewhere")
	}
	if !a.doc.conflict || !tt.HasText(conflictText) {
		t.Errorf("no conflict bar: %q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyR) // reloading takes the file
	if a.doc.conflict || !strings.Contains(a.doc.text(), "Theirs") {
		t.Errorf("⌘R did not take the file: %q", tt.Texts())
	}
}

// Files appearing and going redraw the sidebar; the open document's file
// going closes it, or keeps its edits.
func TestWatchFollowsFiles(t *testing.T) {
	a, tt, w := watchApp(t)
	writeElsewhere(t, a, "Fresh.arcmd", "# Fresh")
	w.fire(t, a, tt, mygo.FileEvent{Op: mygo.FileCreate, Path: "Fresh.arcmd"})
	if !tt.HasText("Fresh") {
		t.Fatalf("new file not listed: %q", tt.Texts())
	}

	if err := tt.Click("Fresh"); err != nil {
		t.Fatal(err)
	}
	a.doc.ed.SetSelection(0, 0)
	a.doc.ed.ReplaceText(nil, "Mine ")
	os.Remove(filepath.Join(root(a), "Fresh.arcmd"))
	w.fire(t, a, tt, mygo.FileEvent{Op: mygo.FileRemove, Path: "Fresh.arcmd"})
	if a.doc == nil || !a.doc.missing || !tt.HasText("已被移动或删除") {
		t.Fatalf("edits of a removed file not kept: %q", tt.Texts())
	}
	if err := tt.Click("重新保存到原位置"); err != nil {
		t.Fatal(err)
	}
	if got, err := vault.Read(root(a), "Fresh.arcmd"); err != nil || !strings.Contains(got, "Mine Fresh") {
		t.Fatalf("not saved again: %v", err)
	}

	os.Remove(filepath.Join(root(a), "Fresh.arcmd"))
	w.fire(t, a, tt, mygo.FileEvent{Op: mygo.FileRemove, Path: "Fresh.arcmd"})
	if a.doc != nil {
		t.Error("removed document still open")
	}
	for _, d := range a.docs {
		if d.Path == "Fresh.arcmd" {
			t.Error("removed document still listed")
		}
	}
}

// A document moved by another app stays open where it went, its favorite
// along.
func TestWatchFollowsRename(t *testing.T) {
	a, tt, w := watchApp(t)
	sp := a.state.ActiveSpace()
	if err := os.Rename(filepath.Join(root(a), "Welcome.arcmd"), filepath.Join(root(a), "Hello.arcmd")); err != nil {
		t.Fatal(err)
	}
	w.fire(t, a, tt, mygo.FileEvent{Op: mygo.FileRename, Path: "Hello.arcmd", OldPath: "Welcome.arcmd"})
	if a.doc == nil || a.doc.rel != "Hello.arcmd" || !a.state.IsFavorite(sp.ID, "Hello.arcmd") {
		t.Fatalf("rename not followed: %+v", a.doc)
	}
	if !tt.HasText("Hello.arcmd") {
		t.Errorf("address not updated: %q", tt.Texts())
	}
}

// The open document's folder stays watched when the window enters another
// space; a folder neither shown nor open is not.
func TestWatchFollowsSpaces(t *testing.T) {
	a, tt, w := watchApp(t)
	first := root(a)
	other := t.TempDir()
	sp := a.state.Add("Work", other)
	a.switchSpace(sp.ID)
	a.openIn(a.state.Spaces[0], "Welcome.arcmd") // the favorite, from its tile
	tt.Frame()
	if w.changed[other] == nil || w.changed[first] == nil {
		t.Fatalf("want both folders watched (the favorite is open): %v", w.changed)
	}
	a.doc = nil
	tt.Frame()
	if w.changed[first] != nil || len(w.stopped) != 1 {
		t.Errorf("first folder still watched: %v, stopped %v", w.changed, w.stopped)
	}
}

// TestConflictSnapshot renders the bar over a document in conflict, for
// review.
func TestConflictSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the conflict bar")
	for _, name := range []string{"conflict", "missing"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			a.doc.ed.SetSelection(0, 0)
			a.doc.ed.ReplaceText(nil, "Mine ")
			a.doc.conflict = name == "conflict"
			a.doc.missing = name == "missing"
			for range 3 {
				tt.Frame()
			}
			file, err := os.Create(filepath.Join(dir, "watch-"+name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := png.Encode(file, tt.Image()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
