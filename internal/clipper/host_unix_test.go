//go:build !windows

package clipper

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInstallDetectAndUninstallUnix(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, "config")
	specs := unixBrowserSpecs(runtime.GOOS)
	if err := os.MkdirAll(filepath.Join(home, specs[0].baseRel), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, "arcmd")
	res, err := InstallHost(home, cfgDir, bin, nil)
	if err != nil {
		t.Fatalf("InstallHost failed: %v", err)
	}
	if res.InstalledCount != 1 {
		t.Fatalf("InstalledCount = %d, want 1", res.InstalledCount)
	}
	if !manifestFileMatches(res.ManifestPath) {
		t.Fatalf("manifest file invalid: %s", res.ManifestPath)
	}
	removed, err := UninstallHost(home, cfgDir)
	if err != nil || removed != 1 {
		t.Fatalf("UninstallHost = (%d, %v), want (1, nil)", removed, err)
	}
}

func TestInstallHostUnixMultiBrowserAndPartialPermissionError(t *testing.T) {
	specs := unixBrowserSpecs(runtime.GOOS)
	if len(specs) < 2 {
		t.Skip("need at least 2 browser specs")
	}
	home := t.TempDir()
	cfgDir := filepath.Join(home, "config")
	firstDir := filepath.Join(home, specs[0].baseRel)
	secondHostsDir := filepath.Join(home, specs[1].baseRel, "NativeMessagingHosts")
	if err := os.MkdirAll(firstDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secondHostsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(home, "ArcMD")
	res, err := InstallHost(home, cfgDir, bin, nil)
	if err != nil {
		t.Fatalf("InstallHost multi-browser failed: %v", err)
	}
	if res.InstalledCount != 2 || len(res.Warnings) != 0 {
		t.Fatalf("InstallHost multi-browser = (count=%d, warnings=%v), want (2, none)", res.InstalledCount, res.Warnings)
	}
	if runtime.GOOS == "darwin" {
		wantArc := filepath.Join(home, "Library", "Application Support", "Arc", "User Data", "NativeMessagingHosts", HostName+".json")
		if !manifestFileMatches(wantArc) {
			t.Fatalf("expected Arc manifest at %s", wantArc)
		}
	}
	if _, err := UninstallHost(home, cfgDir); err != nil {
		t.Fatalf("UninstallHost cleanup failed: %v", err)
	}

	if os.Geteuid() == 0 {
		t.Skip("permission test skipped as root")
	}
	// Make the second browser's NativeMessagingHosts read-only (simulating a
	// TCC-protected or locked browser folder on macOS).
	if err := os.Chmod(secondHostsDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(secondHostsDir, 0o755) })

	res, err = InstallHost(home, cfgDir, bin, nil)
	if err != nil {
		t.Fatalf("InstallHost should succeed for writable browser when another fails: %v", err)
	}
	if res.InstalledCount != 1 || len(res.Warnings) != 1 {
		t.Fatalf("partial install = (count=%d, warnings=%v), want (1, 1 warning)", res.InstalledCount, res.Warnings)
	}

	// If every detected browser is unwritable, InstallHost must still return an error.
	firstHostsDir := filepath.Join(firstDir, "NativeMessagingHosts")
	if err := os.Chmod(firstHostsDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(firstHostsDir, 0o755) })
	if _, err := InstallHost(home, cfgDir, bin, nil); err == nil {
		t.Fatal("expected error when all target browser directories are unwritable")
	}
}
