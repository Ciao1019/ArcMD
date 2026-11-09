package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMoveTo(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	write := func(root, rel, text string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(a, "Notes.arcmd", "a")
	write(b, "Notes.arcmd", "b")
	write(a, "Trip/Plan.arcmd", "plan")
	write(b, "Inbox/keep.txt", "")

	// A name taken there is numbered.
	got, copied, err := MoveTo(a, "Notes.arcmd", b, "")
	if err != nil || got != "Notes 2.arcmd" || copied {
		t.Fatalf("MoveTo = %q, %v, %v", got, copied, err)
	}
	if text, _ := os.ReadFile(filepath.Join(b, "Notes 2.arcmd")); string(text) != "a" {
		t.Errorf("moved text %q", text)
	}
	if _, err := os.Stat(filepath.Join(a, "Notes.arcmd")); !os.IsNotExist(err) {
		t.Errorf("the original stayed: %v", err)
	}
	// A folder goes with what it holds, into a folder there.
	got, _, err = MoveTo(a, "Trip", b, "Inbox")
	if err != nil || got != "Inbox/Trip" {
		t.Fatalf("MoveTo folder = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(b, "Inbox", "Trip", "Plan.arcmd")); err != nil {
		t.Error(err)
	}
	if _, _, err := MoveTo(a, "Missing.arcmd", b, ""); err == nil {
		t.Error("moved a file that does not exist")
	}
	if _, _, err := MoveTo(b, "Inbox", a, "../x"); err == nil {
		t.Error("moved out of the vault")
	}
	if _, _, err := MoveTo(b, "Notes.arcmd", a, "Nowhere"); err == nil {
		t.Error("moved into a folder that does not exist")
	}
}

func TestCopyTree(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "out")
	os.MkdirAll(filepath.Join(src, "assets"), 0o755)
	os.WriteFile(filepath.Join(src, "a.arcmd"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(src, "assets", "p.png"), []byte("png"), 0o644)
	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "assets", "p.png")); string(b) != "png" {
		t.Errorf("copied %q", b)
	}
}
