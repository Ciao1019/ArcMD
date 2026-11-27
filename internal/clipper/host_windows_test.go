//go:build windows

package clipper

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallDetectAndUninstallWindows(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, "config")
	localAppData := filepath.Join(home, "AppData", "Local")
	if err := os.MkdirAll(filepath.Join(localAppData, `Google\Chrome\User Data`), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(localAppData, `Microsoft\Edge\User Data`), 0o755); err != nil {
		t.Fatal(err)
	}

	mockReg := map[string]string{}
	origLocal, origGet, origSet, origDel := winLocalAppDataFn, winRegGetDefault, winRegSetDefault, winRegDeleteKey
	t.Cleanup(func() {
		winLocalAppDataFn, winRegGetDefault, winRegSetDefault, winRegDeleteKey = origLocal, origGet, origSet, origDel
	})
	winLocalAppDataFn = func(string) string { return localAppData }
	winRegGetDefault = func(subKey string) (string, bool) {
		v, ok := mockReg[subKey]
		return v, ok
	}
	winRegSetDefault = func(subKey, value string) error {
		mockReg[subKey] = value
		return nil
	}
	winRegDeleteKey = func(subKey string) error {
		delete(mockReg, subKey)
		return nil
	}

	bin := filepath.Join(home, "ArcMD.exe")
	customID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	res, err := InstallHost(home, cfgDir, bin, []string{customID})
	if err != nil {
		t.Fatalf("InstallHost failed: %v", err)
	}
	if res.InstalledCount != 2 {
		t.Fatalf("InstalledCount = %d, want 2 (Chrome + Edge)", res.InstalledCount)
	}
	if len(res.AllowedOrigins) != 2 {
		t.Fatalf("AllowedOrigins = %v", res.AllowedOrigins)
	}
	if !manifestFileMatches(res.ManifestPath) {
		t.Fatalf("manifest file did not validate: %s", res.ManifestPath)
	}

	detected := DetectBrowsers(home, cfgDir)
	installedCount := 0
	for _, b := range detected {
		if b.Installed {
			installedCount++
		}
	}
	if installedCount != 2 {
		t.Fatalf("DetectBrowsers installedCount = %d, want 2: %+v", installedCount, detected)
	}

	removed, err := UninstallHost(home, cfgDir)
	if err != nil {
		t.Fatalf("UninstallHost failed: %v", err)
	}
	if removed != 2 {
		t.Fatalf("UninstallHost removed = %d, want 2", removed)
	}
	if _, err := os.Stat(res.ManifestPath); !os.IsNotExist(err) {
		t.Fatalf("manifest file still exists after uninstall: %v", err)
	}
}
