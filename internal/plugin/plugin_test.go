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
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(b) }))
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

func TestInstallRejectsZipBombAndUint64SizeOverflow(t *testing.T) {
	// 1. uint64 UncompressedSize64 >= 1<<63 must not overflow int64 to negative.
	var overflowBuf bytes.Buffer
	zw := zip.NewWriter(&overflowBuf)
	mw, err := zw.Create(ManifestFile)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = mw.Write([]byte(goodManifest))
	rw, err := zw.CreateRaw(&zip.FileHeader{
		Name:               "overflow.bin",
		Method:             zip.Store,
		CompressedSize64:   4,
		UncompressedSize64: (1 << 63) + 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = rw.Write([]byte("abcd"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	overflowZip := overflowBuf.Bytes()
	if _, _, err := InspectArchive(overflowZip); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("InspectArchive failed to reject uint64 overflow: %v", err)
	}
	sum := sha256.Sum256(overflowZip)
	mgr := NewManager(t.TempDir())
	if _, err := mgr.InstallArchive(overflowZip, Listing{ID: "dev.arcmd.ai-writer", Version: "0.1.0", SHA256: hex.EncodeToString(sum[:])}); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("InstallArchive failed to reject uint64 overflow: %v", err)
	}

	// 2. Compressed multi-entry zip bomb (~68 KiB zip expanding to 66 MiB > MaxPackageSize).
	var bombBuf bytes.Buffer
	bzw := zip.NewWriter(&bombBuf)
	bmw, err := bzw.Create(ManifestFile)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = bmw.Write([]byte(goodManifest))
	chunk := bytes.Repeat([]byte{'Z'}, 64<<10)
	for _, entryName := range []string{"part1.bin", "part2.bin"} {
		bw, err := bzw.Create(entryName)
		if err != nil {
			t.Fatal(err)
		}
		for written := 0; written < 33<<20; written += len(chunk) {
			if _, err := bw.Write(chunk); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := bzw.Close(); err != nil {
		t.Fatal(err)
	}

	bombZip := bombBuf.Bytes()
	if _, _, err := InspectArchive(bombZip); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("InspectArchive failed to reject compressed zip bomb: %v", err)
	}
	bombSum := sha256.Sum256(bombZip)
	installDir := t.TempDir()
	mgr2 := NewManager(installDir)
	if _, err := mgr2.InstallArchive(bombZip, Listing{ID: "dev.arcmd.ai-writer", Version: "0.1.0", SHA256: hex.EncodeToString(bombSum[:])}); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("InstallArchive failed to stop compressed zip bomb: %v", err)
	}
	entries, err := os.ReadDir(installDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging or partial plugin left behind after zip bomb rejection: %v, %v", entries, err)
	}

	// 3. Direct unpack actual decompressed-byte tracking (defeating forged-header bombs
	// and cleaning up partial output as soon as written > remaining).
	smallZip := zipOf(t, map[string]string{
		"a.bin": strings.Repeat("A", 4096),
		"b.bin": strings.Repeat("B", 4096),
	})
	zr, err := zip.NewReader(bytes.NewReader(smallZip), int64(len(smallZip)))
	if err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	remaining := int64(6000)
	dst0 := filepath.Join(outDir, "a.bin")
	if err := unpack(zr.File[0], dst0, &remaining); err != nil {
		t.Fatalf("first unpack within budget failed: %v", err)
	}
	if remaining != 6000-4096 {
		t.Fatalf("remaining = %d, want %d", remaining, 6000-4096)
	}
	dst1 := filepath.Join(outDir, "b.bin")
	if err := unpack(zr.File[1], dst1, &remaining); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("second unpack exceeding remaining budget should fail, got %v", err)
	}
	if _, err := os.Stat(dst1); !os.IsNotExist(err) {
		t.Fatalf("partial output file was not removed on size limit overflow: %v", err)
	}
}
