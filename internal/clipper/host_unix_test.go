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
