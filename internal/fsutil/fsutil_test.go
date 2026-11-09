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
