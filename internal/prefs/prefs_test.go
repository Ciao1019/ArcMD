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
	p.TourSeen = true
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	q, err := s.Load()
	if err != nil || !q.Typewriter || q.Keys["palette"] != "Cmd+J" || !q.TourSeen {
		t.Fatalf("loaded %+v %v", q, err)
	}
	os.WriteFile(s.Path, []byte(`{"version": 9}`), 0o644)
	if _, err := s.Load(); err == nil {
		t.Error("newer version accepted")
	}
}

func TestReadingPreferencesDefaultsAndRoundTrip(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	p, _ := s.Load()
	if p.ReadingSize() != 16 || p.ReadingLine() != 1.625 {
		t.Fatal("reading defaults")
	}
	p.FontSize, p.LineHeight, p.NoAutoPair = 22, 1.9, true
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	q, err := s.Load()
	if err != nil || q.FontSize != 22 || q.LineHeight != 1.9 || !q.NoAutoPair {
		t.Fatal(q, err)
	}
	os.WriteFile(s.Path, []byte(`{"version":1,"fontSize":9999,"lineHeight":-2}`), 0600)
	q, err = s.Load()
	if err != nil || q.ReadingSize() != 16 || q.ReadingLine() != 1.625 {
		t.Fatal("invalid reading values", q, err)
	}
}
