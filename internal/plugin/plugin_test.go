package plugin

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodManifest = `{
  "id": "dev.arcmd.ai-writer",
  "name": "AI Writer",
  "version": "0.1.0",
  "runtime": "wasm",
  "main": "plugin.wasm",
  "permissions": ["document:read", "document:write", "network", "ui:panel"],
  "contributes": {
    "commands": [{"id": "rewrite", "title": "Rewrite selection", "when": "selection"}],
    "panels": [{"id": "chat", "title": "Chat"}]
  }
}`

func TestParseManifest(t *testing.T) {
	m, err := ParseManifest([]byte(goodManifest))
	if err != nil {
		t.Fatal(err)
	}
	if m.Contributes.Commands[0].Title != "Rewrite selection" {
		t.Errorf("commands = %+v", m.Contributes.Commands)
	}
	bad := `{"id": "Bad", "name": "", "version": "1", "runtime": "lua", "main": "../x",
	  "permissions": ["root"], "contributes": {"panels": [{"id": "p"}]}}`
	_, err = ParseManifest([]byte(bad))
	if err == nil {
		t.Fatal("bad manifest accepted")
	}
	for _, want := range []string{"id", "name", "version", "runtime", "main", "unknown permission", "panels need"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func serve(t *testing.T, b []byte) (Listing, *http.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(b) }))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(b)
	return Listing{ID: "dev.arcmd.ai-writer", Version: "0.1.0", Download: srv.URL + "/p.zip", SHA256: hex.EncodeToString(sum[:])}, srv.Client()
}

func TestInstallScanToggleUninstall(t *testing.T) {
	m := NewManager(filepath.Join(t.TempDir(), "plugins"))
	l, client := serve(t, zipOf(t, map[string]string{ManifestFile: goodManifest, "plugin.wasm": "\x00asm"}))
	if _, err := m.Install(context.Background(), client, l); err != nil {
		t.Fatal(err)
	}
	// A broken folder sits next to it.
	os.MkdirAll(filepath.Join(m.Dir(), "junk"), 0o755)
	got, broken, err := m.Scan()
	if err != nil || len(got) != 1 || len(broken) != 1 || !got[0].Enabled {
		t.Fatalf("Scan = %v, %v, %v", got, broken, err)
	}
	if err := m.SetEnabled(l.ID, false); err != nil {
		t.Fatal(err)
	}
	if got, _, _ = m.Scan(); got[0].Enabled {
		t.Error("still enabled")
	}
	// Reinstalling replaces the folder and keeps the choice.
	if p, err := m.Install(context.Background(), client, l); err != nil || p.Enabled {
		t.Fatalf("reinstall = %+v, %v", p, err)
	}
	if err := m.Uninstall(l.ID); err != nil {
		t.Fatal(err)
	}
	if got, _, _ = m.Scan(); len(got) != 0 {
		t.Errorf("after Uninstall, Scan = %v", got)
	}
}

func TestInstallRejects(t *testing.T) {
	m := NewManager(t.TempDir())
	ctx := context.Background()

	l, client := serve(t, zipOf(t, map[string]string{ManifestFile: goodManifest}))
	l.SHA256 = strings.Repeat("0", 64)
	if _, err := m.Install(ctx, client, l); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Errorf("wrong digest: %v", err)
	}

	l, client = serve(t, zipOf(t, map[string]string{ManifestFile: goodManifest, "../evil.txt": "x"}))
	if _, err := m.Install(ctx, client, l); err == nil || !strings.Contains(err.Error(), "unsafe path") {
		t.Errorf("zip slip: %v", err)
	}

	l, client = serve(t, zipOf(t, map[string]string{ManifestFile: goodManifest}))
	l.Version = "9.9.9"
	if _, err := m.Install(ctx, client, l); err == nil || !strings.Contains(err.Error(), "index says") {
		t.Errorf("version mismatch: %v", err)
	}

	if _, err := FetchIndex(ctx, http.DefaultClient, "http://example.com/index.json"); err == nil {
		t.Error("plain http index accepted")
	}
}
