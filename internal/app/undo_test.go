package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/vault"
)

func TestUndoToastRestoresTrashedDocument(t *testing.T) {
	a, tt := newTestApp(t)
	a.trashFile = os.RemoveAll
	tt.Frame()
	a.trash("Journal/Ideas.arcmd")
	tt.Frame()
	if _, err := os.Stat(filepath.Join(root(a), "Journal", "Ideas.arcmd")); !os.IsNotExist(err) {
		t.Fatal("not trashed")
	}
	if !tt.HasText("已移到废纸篓") {
		t.Fatal("no toast", tt.Texts())
	}
	if err := tt.Click("撤销"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, err := os.Stat(filepath.Join(root(a), "Journal", "Ideas.arcmd")); err != nil {
		t.Fatal("undo did not put it back", err)
	}
}

func TestUndoToastMovesBack(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	a.placeEntry("Journal/Ideas.arcmd", "", 0)
	tt.Frame()
	if _, err := vault.Read(root(a), "Ideas.arcmd"); err != nil {
		t.Fatal("not moved", err)
	}
	if !tt.HasText("已移到「Personal」") {
		t.Fatal("no toast", tt.Texts())
	}
	if err := tt.Click("撤销"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, err := vault.Read(root(a), "Journal/Ideas.arcmd"); err != nil {
		t.Fatal("not moved back", err)
	}
	// Left alone, the toast goes; it waits while the pointer rests on it.
	tt.Move(600, 100)
	a.placeEntry("Journal/Ideas.arcmd", "", 0)
	tt.Frame()
	tt.Advance(undoTimeout + time.Second)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if tt.HasText("撤销") {
		t.Fatal("the toast stays")
	}
}
