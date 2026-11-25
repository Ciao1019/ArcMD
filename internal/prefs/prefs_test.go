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

func TestFontAndPageWidth(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	p := &Prefs{Font: "serif", Page: "wide"}
	if err := store.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil || got.BodyFamily() == "" || got.Column() != 1040 || got.FullWidth() {
		t.Fatal(got, err)
	}
	got.Page = "full"
	if err := store.Save(got); err != nil {
		t.Fatal(err)
	}
	got, err = store.Load()
	if err != nil || got.Page != "full" || !got.FullWidth() || got.Column() != 0 {
		t.Fatal("full width round-trip", got, err)
	}
	got.Font, got.Page = "comic", "huge"
	if err := store.Save(got); err != nil {
		t.Fatal(err)
	}
	if got.Font != "" || got.Page != "" || got.BodyFamily() != "" || got.Column() != 0 || got.FullWidth() {
		t.Fatal("unknown choices fall back to the defaults")
	}
}

func TestLoadUnknownAIProtocol(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	if err := os.WriteFile(s.Path, []byte(`{"version":1,"ai":{"model":"custom","protocol":"unknown"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := s.Load()
	if err != nil || p.AI.Protocol != "" || p.AI.Model != "custom" {
		t.Fatalf("loaded %+v, %v", p, err)
	}
}

func TestMaxHeightNormalizeAndRoundTrip(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	p := &Prefs{CodeMaxHeight: 360, ImageMaxHeight: 480}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil || got.CodeMaxHeight != 360 || got.ImageMaxHeight != 480 {
		t.Fatalf("roundtrip = %+v, err=%v", got, err)
	}

	if err := os.WriteFile(s.Path, []byte(`{"version":1,"codeMaxHeight":-50,"imageMaxHeight":9999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sanitized, err := s.Load()
	if err != nil || sanitized.CodeMaxHeight != 0 || sanitized.ImageMaxHeight != 2000 {
		t.Fatalf("sanitized = %+v, err=%v", sanitized, err)
	}
}

func TestPageZoomDefaultsStepAndRoundTrip(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	p, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if p.PageZoom != 0 || p.Zoom() != DefaultPageZoom {
		t.Fatalf("default zoom = (%v, %v); want (0, %v)", p.PageZoom, p.Zoom(), DefaultPageZoom)
	}

	if got := NormalizePageZoom(0); got != DefaultPageZoom {
		t.Fatalf("NormalizePageZoom(0) = %v; want 1", got)
	}
	if got := NormalizePageZoom(0.2); got != MinPageZoom {
		t.Fatalf("NormalizePageZoom(0.2) = %v; want %v", got, MinPageZoom)
	}
	if got := NormalizePageZoom(4.5); got != MaxPageZoom {
		t.Fatalf("NormalizePageZoom(4.5) = %v; want %v", got, MaxPageZoom)
	}

	if got := StepPageZoom(1.0, 1); got != 1.1 {
		t.Fatalf("StepPageZoom(1.0, +1) = %v; want 1.1", got)
	}
	if got := StepPageZoom(1.0, -1); got != 0.9 {
		t.Fatalf("StepPageZoom(1.0, -1) = %v; want 0.9", got)
	}
	if got := StepPageZoom(1.25, 1); got != 1.3 {
		t.Fatalf("StepPageZoom(1.25, +1) = %v; want 1.3", got)
	}
	if got := StepPageZoom(1.25, -1); got != 1.2 {
		t.Fatalf("StepPageZoom(1.25, -1) = %v; want 1.2", got)
	}
	if got := StepPageZoom(MaxPageZoom, 1); got != MaxPageZoom {
		t.Fatalf("StepPageZoom(max, +1) = %v; want %v", got, MaxPageZoom)
	}
	if got := StepPageZoom(MinPageZoom, -1); got != MinPageZoom {
		t.Fatalf("StepPageZoom(min, -1) = %v; want %v", got, MinPageZoom)
	}

	p.PageZoom = 1.3
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Load()
	if err != nil || loaded.PageZoom != 1.3 || loaded.Zoom() != 1.3 {
		t.Fatalf("loaded = %+v, err=%v", loaded, err)
	}

	// Default 1.0 normalizes to 0 and is omitted from prefs.json.
	loaded.PageZoom = DefaultPageZoom
	if err := s.Save(loaded); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PageZoom != 0 || string(raw) == "" {
		t.Fatalf("default zoom did not normalize to 0: %+v", loaded)
	}
	if stringContains(string(raw), "pageZoom") {
		t.Fatalf("default 100%% zoom should be omitted from json: %s", raw)
	}

	// Out-of-range JSON values clamp cleanly on load.
	if err := os.WriteFile(s.Path, []byte(`{"version":1,"pageZoom":9.9}`), 0o600); err != nil {
		t.Fatal(err)
	}
	clamped, err := s.Load()
	if err != nil || clamped.Zoom() != MaxPageZoom {
		t.Fatalf("clamped high = %+v, err=%v", clamped, err)
	}
	if err := os.WriteFile(s.Path, []byte(`{"version":1,"pageZoom":0.1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	clamped, err = s.Load()
	if err != nil || clamped.Zoom() != MinPageZoom {
		t.Fatalf("clamped low = %+v, err=%v", clamped, err)
	}
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestSidebarDensityRoundTrip(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	p, err := s.Load()
	if err != nil || p.SidebarRowHeight() != 32 {
		t.Fatalf("default sidebar row height = %v, err=%v", p.SidebarRowHeight(), err)
	}
	for _, tc := range []struct {
		density string
		want    float32
	}{
		{"compact", 28},
		{"", 32},
		{"comfortable", 40},
	} {
		p.SidebarDensity = tc.density
		if err := s.Save(p); err != nil {
			t.Fatal(err)
		}
		got, err := s.Load()
		if err != nil || got.SidebarDensity != tc.density || got.SidebarRowHeight() != tc.want {
			t.Fatalf("density %q: got %+v height=%v err=%v", tc.density, got, got.SidebarRowHeight(), err)
		}
	}
	p.SidebarDensity = "giant"
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil || got.SidebarDensity != "" || got.SidebarRowHeight() != 32 {
		t.Fatalf("invalid density did not fall back: %+v err=%v", got, err)
	}
}
