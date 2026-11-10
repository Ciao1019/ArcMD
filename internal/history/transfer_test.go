package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCopyHistoryMergesCollisionsAndKeepsOriginal(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	now := time.Date(2026, 11, 10, 1, 0, 0, 0, time.Local)
	if err := Record(src, "Folder/Note.arcmd", "original", now, true); err != nil {
		t.Fatal(err)
	}
	if err := Record(dst, "Moved/Note.arcmd", "existing", now, true); err != nil {
		t.Fatal(err)
	}
	if err := CopyTo(src, "Folder", dst, "Moved", func(rel, text string) (string, error) {
		if rel != "Folder/Note.arcmd" {
			t.Fatal(rel)
		}
		return text + " rewritten", nil
	}); err != nil {
		t.Fatal(err)
	}
	versions, err := List(dst, "Moved/Note.arcmd")
	if err != nil || len(versions) != 2 {
		t.Fatal(versions, err)
	}
	contents := map[string]bool{}
	for _, v := range versions {
		text, _ := Read(dst, "Moved/Note.arcmd", v)
		contents[text] = true
	}
	if !contents["existing"] || !contents["original rewritten"] {
		t.Fatal(contents)
	}
	if v, _ := List(src, "Folder/Note.arcmd"); len(v) != 1 {
		t.Fatal("source removed before commit")
	}
}

func TestCopyHistoryFailureKeepsSource(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	if err := Record(src, "Note.arcmd", "original", time.Now(), true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, ".arcmd"), []byte("occupied"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CopyTo(src, "Note.arcmd", dst, "Note.arcmd", nil); err == nil {
		t.Fatal("expected failure")
	}
	if v, _ := List(src, "Note.arcmd"); len(v) != 1 {
		t.Fatal("source lost")
	}
}
