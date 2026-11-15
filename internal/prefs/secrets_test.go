package prefs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSecretsPersistAndRemove(t *testing.T) {
	s := Secrets{Dir: filepath.Join(t.TempDir(), "config", "credentials")}
	const key = "ai-api-key"
	if _, err := s.Get(key); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing key: %v", err)
	}
	if err := s.Delete(key); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("reading or removing a missing key created a directory")
	}
	for _, value := range []string{"first-key", "replacement-key"} {
		if err := s.Set(key, []byte(value)); err != nil {
			t.Fatal(err)
		}
		// A fresh store, like a restarted app, reads the saved value.
		data, err := (Secrets{Dir: s.Dir}).Get(key)
		if err != nil || string(data) != value {
			t.Fatalf("saved key did not survive reopening: %v", err)
		}
		entries, err := os.ReadDir(s.Dir)
		if err != nil || len(entries) != 1 || entries[0].Name() != key {
			t.Fatalf("unexpected files after saving: %v, %v", entries, err)
		}
	}
	for range 2 {
		if err := s.Delete(key); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := (Secrets{Dir: s.Dir}).Get(key); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed key reappeared: %v", err)
	}
}

func TestSecretsRestrictPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not implement Unix permission bits")
	}
	s := Secrets{Dir: filepath.Join(t.TempDir(), "credentials")}
	const key = "ai-api-key"
	file := filepath.Join(s.Dir, key)
	for _, existing := range []bool{false, true} {
		if existing {
			if err := os.Chmod(s.Dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(file, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.Set(key, []byte("test-key")); err != nil {
			t.Fatal(err)
		}
		for path, want := range map[string]os.FileMode{s.Dir: 0o700, file: 0o600} {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != want {
				t.Fatalf("%s mode %o, want %o", path, info.Mode().Perm(), want)
			}
		}
	}
}

func TestSecretsRejectPathsOutsideStore(t *testing.T) {
	s := Secrets{Dir: filepath.Join(t.TempDir(), "credentials")}
	for _, key := range []string{"", ".", "..", "../outside", "nested/key", `..\outside`, filepath.Join(t.TempDir(), "absolute")} {
		if err := s.Set(key, []byte("test-key")); err == nil {
			t.Errorf("Set accepted %q", key)
		}
		if _, err := s.Get(key); err == nil {
			t.Errorf("Get accepted %q", key)
		}
		if err := s.Delete(key); err == nil {
			t.Errorf("Delete accepted %q", key)
		}
	}
	if err := (Secrets{}).Set("ai-api-key", []byte("test-key")); err == nil {
		t.Fatal("accepted an empty storage directory")
	}
}

func TestSecretsStorageErrors(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Secrets{Dir: filepath.Join(blocker, "credentials")}
	if err := s.Set("ai-api-key", []byte("test-key")); err == nil {
		t.Fatal("saved through a file instead of a directory")
	}
	if _, err := s.Get("ai-api-key"); err == nil {
		t.Fatal("read through a file instead of a directory")
	}
	if err := s.Delete("ai-api-key"); err == nil {
		t.Fatal("removal hid an inaccessible directory")
	}
	data, err := os.ReadFile(blocker)
	if err != nil || string(data) != "keep" {
		t.Fatalf("storage error changed another file: %v", err)
	}
}
