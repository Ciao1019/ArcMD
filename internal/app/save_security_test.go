package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/vault"
)

func TestUnreadableDiskStopsAutosaveAndFlush(t *testing.T) {
	a, tt := newTestApp(t)
	d := a.doc
	d.ed.SetSelection(0, 0)
	d.ed.ReplaceText(nil, "Mine ")
	writeElsewhere(t, a, d.rel, "# External changes")
	theirs, err := vault.Read(root(a), d.rel)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root(a), filepath.FromSlash(d.rel))
	if err := os.Chmod(file, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(file, 0o600) })
	if _, err := os.ReadFile(file); err == nil {
		t.Skip("this filesystem or user does not enforce read permissions")
	} else if !errors.Is(err, os.ErrPermission) {
		t.Fatal(err)
	}
	baseline := d.baseline
	tt.Frame()
	tt.Advance(autosaveDelay + 50*time.Millisecond)
	tt.Frame()
	if !errors.Is(d.saveErr, os.ErrPermission) || !d.dirty() || d.baseline != baseline {
		t.Fatalf("failed autosave lost unsaved state: error %v, dirty %v", d.saveErr, d.dirty())
	}
	if a.flush() {
		t.Fatal("flush allowed leaving an unreadable document")
	}
	if err := os.Chmod(file, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := vault.Read(root(a), d.rel); err != nil || got != theirs {
		t.Fatal("external changes were overwritten", err)
	}
	// Restoring access resumes the existing conflict-resolution flow.
	if err := d.save(); !errors.Is(err, errConflict) || !d.conflict {
		t.Fatal("retry failed to detect external changes", err)
	}
	a.keepMine()
	if d.saveErr != nil || d.dirty() || d.conflict {
		t.Fatal("explicitly keeping edits no longer works", d.saveErr)
	}
}

func TestMalformedTableLoadKeepsOriginal(t *testing.T) {
	a, tt := newTestApp(t)
	const original = `{"format":"arcmd","version":6,"blocks":[{"id":"t","kind":"table","children":[{"id":"r","kind":"row","children":[null]}]}]}`
	if err := vault.Write(root(a), "Malformed.arcmd", original); err != nil {
		t.Fatal(err)
	}
	a.open("Malformed.arcmd")
	tt.Frame()
	if a.doc.loadErr == nil || a.doc.dirty() {
		t.Fatal("a malformed table did not enter the existing load-error state")
	}
	if err := a.doc.save(); err == nil {
		t.Fatal("a malformed document was saved")
	}
	if got, err := vault.Read(root(a), "Malformed.arcmd"); err != nil || got != original {
		t.Fatal("a malformed document was overwritten", err)
	}
}
