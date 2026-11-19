package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestExclusiveAtomicPublication(t *testing.T) {
	name := filepath.Join(t.TempDir(), "note.arcmd")
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for range 12 {
		wg.Go(func() { results <- CreateFileAtomic(name, []byte("complete document"), 0o644) })
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, os.ErrExist) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatal("exclusive creation", successes)
	}
	text, err := os.ReadFile(name)
	if err != nil || string(text) != "complete document" {
		t.Fatal("partial file", err)
	}
	entries, err := os.ReadDir(filepath.Dir(name))
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary files leaked")
	}
}

func TestIsMissingFile(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.txt")
	_, err := os.Stat(missing)
	if !IsMissingFile(missing, err) {
		t.Fatalf("expected missing file under directory to report true (err=%v)", err)
	}
	nestedMissing := filepath.Join(dir, "sub", "missing.txt")
	_, err = os.Stat(nestedMissing)
	if !IsMissingFile(nestedMissing, err) {
		t.Fatalf("expected missing file under missing subdir to report true (err=%v)", err)
	}
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(blocker, "nested", "secret")
	if IsMissingFile(blocked, os.ErrNotExist) {
		t.Fatal("expected regular-file ancestor to return false even for os.ErrNotExist")
	}
	if IsMissingFile(missing, nil) {
		t.Fatal("expected nil error to return false")
	}
}

func TestWriteFileAtomicPreservesHardLinks(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "primary.md")
	linked := filepath.Join(dir, "linked.md")

	if err := WriteFileAtomic(primary, []byte("initial content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(primary, linked); err != nil {
		t.Skipf("hard links are unavailable on this filesystem: %v", err)
	}

	// Test both expanding and truncating in-place writes so no trailing bytes leak.
	for _, want := range []string{
		"much longer updated content across both hard-linked entries\nsecond line\n",
		"short\n",
		"",
		"restored after empty\n",
	} {
		if err := WriteFileAtomic(primary, []byte(want), 0o644); err != nil {
			t.Fatalf("WriteFileAtomic(%q): %v", want, err)
		}
		fi1, err := os.Stat(primary)
		if err != nil {
			t.Fatal(err)
		}
		fi2, err := os.Stat(linked)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(fi1, fi2) {
			t.Fatalf("hard link broke after saving %q", want)
		}
		for _, p := range []string{primary, linked} {
			got, err := os.ReadFile(p)
			if err != nil || string(got) != want {
				t.Fatalf("read %s = %q (%v), want %q", p, string(got), err, want)
			}
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("unexpected directory entries: %v, %v", entries, err)
	}
}
