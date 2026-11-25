package trashbin

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, root, rel, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAddListRestore(t *testing.T) {
	root := t.TempDir()
	bin := Bin{Dir: t.TempDir()}
	write(t, root, "Notes/Plan.arcmd", "plan")
	write(t, root, "Trips/Rome.arcmd", "rome")
	write(t, root, "Trips/assets/a.png", "png")
	now := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	doc, err := bin.Add("s1", "Personal", root, "Notes/Plan.arcmd", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bin.Add("s1", "Personal", root, "Trips", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := bin.Add("s1", "Personal", root, "../x", now); err == nil {
		t.Error("added outside the space")
	}
	items, err := bin.List(now.Add(time.Hour))
	if err != nil || len(items) != 2 || items[0].Path != "Trips" || !items[0].Dir || items[1].Name() != "Plan.arcmd" {
		t.Fatalf("items %+v %v", items, err)
	}
	// The original went to the trash; a new file took the name meanwhile.
	os.RemoveAll(filepath.Join(root, "Notes"))
	os.RemoveAll(filepath.Join(root, "Trips"))
	write(t, root, "Notes/Plan.arcmd", "new")
	rel, err := bin.Restore(items[1], root)
	if err != nil || rel != "Notes/Plan 2.arcmd" {
		t.Fatalf("restored to %q %v", rel, err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "Notes/Plan 2.arcmd")); string(data) != "plan" {
		t.Errorf("restored %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "Notes/Plan.arcmd")); string(data) != "new" {
		t.Error("restore overwrote a file")
	}
	rel, err = bin.Restore(items[0], root)
	if err != nil || rel != "Trips" {
		t.Fatal(rel, err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "Trips/assets/a.png")); string(data) != "png" {
		t.Error("folder not restored whole")
	}
	if items, _ := bin.List(now); len(items) != 0 {
		t.Errorf("restored items still listed: %+v", items)
	}
	_ = doc
}

func TestExpiryAndForget(t *testing.T) {
	root := t.TempDir()
	bin := Bin{Dir: t.TempDir()}
	write(t, root, "a.arcmd", "a")
	write(t, root, "b.arcmd", "b")
	now := time.Now()
	old, _ := bin.Add("s", "S", root, "a.arcmd", now.Add(-Keep-time.Hour))
	it, _ := bin.Add("s", "S", root, "b.arcmd", now)
	items, _ := bin.List(now)
	if len(items) != 1 || items[0].Path != "b.arcmd" {
		t.Fatalf("items %+v", items)
	}
	if _, err := os.Stat(filepath.Join(bin.Dir, old.ID)); !os.IsNotExist(err) {
		t.Error("expired copy kept")
	}
	if err := bin.Forget(it); err != nil {
		t.Fatal(err)
	}
	if items, _ := bin.List(now); len(items) != 0 {
		t.Error("forgotten item listed")
	}
	if err := bin.Forget(Item{ID: "../x"}); err == nil {
		t.Error("forgot outside the bin")
	}
}

func TestRestoreCleansUpPartialTargetOnFailure(t *testing.T) {
	origCopy := copyStream
	t.Cleanup(func() { copyStream = origCopy })

	root := t.TempDir()
	bin := Bin{Dir: t.TempDir()}
	write(t, root, "Trips/a.arcmd", "first file")
	write(t, root, "Trips/z.arcmd", "second file")
	write(t, root, "Single.arcmd", "single body")

	now := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	dirItem, err := bin.Add("s1", "Personal", root, "Trips", now)
	if err != nil {
		t.Fatal(err)
	}
	fileItem, err := bin.Add("s1", "Personal", root, "Single.arcmd", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "Trips")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "Single.arcmd")); err != nil {
		t.Fatal(err)
	}

	// Fail partway through restoring a multi-file directory (after first file copied).
	calls := 0
	copyStream = func(dst io.Writer, src io.Reader) (int64, error) {
		calls++
		if calls == 2 {
			_, _ = dst.Write([]byte("corrupt half"))
			return 12, errors.New("simulated disk full")
		}
		return io.Copy(dst, src)
	}
	if _, err := bin.Restore(dirItem, root); err == nil {
		t.Fatal("expected folder restore to fail on simulated disk error")
	}
	if _, err := os.Lstat(filepath.Join(root, "Trips")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial directory left in space after failed Restore: stat err = %v", err)
	}

	// Fail partway through restoring a single file.
	copyStream = func(dst io.Writer, _ io.Reader) (int64, error) {
		_, _ = dst.Write([]byte("half"))
		return 4, errors.New("simulated disk full")
	}
	if _, err := bin.Restore(fileItem, root); err == nil {
		t.Fatal("expected file restore to fail on simulated disk error")
	}
	if _, err := os.Lstat(filepath.Join(root, "Single.arcmd")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial file left in space after failed Restore: stat err = %v", err)
	}

	// Backup copies remain intact in the bin and succeed once the error clears.
	copyStream = origCopy
	if rel, err := bin.Restore(dirItem, root); err != nil || rel != "Trips" {
		t.Fatalf("retry folder restore = %q, %v", rel, err)
	}
	if rel, err := bin.Restore(fileItem, root); err != nil || rel != "Single.arcmd" {
		t.Fatalf("retry file restore = %q, %v", rel, err)
	}
}
