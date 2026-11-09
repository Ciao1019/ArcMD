package prefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "sub", "prefs.json")}
	p, err := s.Load()
	if err != nil || p.Typewriter || p.Version != CurrentVersion {
		t.Fatalf("defaults %+v %v", p, err)
	}
	p.Typewriter = true
	p.Keys = map[string]string{"palette": "Cmd+J"}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	q, err := s.Load()
	if err != nil || !q.Typewriter || q.Keys["palette"] != "Cmd+J" {
		t.Fatalf("loaded %+v %v", q, err)
	}
	os.WriteFile(s.Path, []byte(`{"version": 9}`), 0o644)
	if _, err := s.Load(); err == nil {
		t.Error("newer version accepted")
	}
}
