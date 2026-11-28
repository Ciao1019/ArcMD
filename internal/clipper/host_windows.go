//go:build windows

package clipper

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"golang.org/x/sys/windows/registry"
)

type winBrowserSpec struct {
	id          string
	name        string
	userDataRel string
	regSubKey   string
	fallback    bool
}

var winBrowserSpecs = []winBrowserSpec{
	{
		id:          "chrome",
		name:        "Chrome",
		userDataRel: `Google\Chrome\User Data`,
		regSubKey:   `Software\Google\Chrome\NativeMessagingHosts\` + HostName,
		fallback:    true,
	},
	{
		id:          "edge",
		name:        "Edge",
		userDataRel: `Microsoft\Edge\User Data`,
		regSubKey:   `Software\Microsoft\Edge\NativeMessagingHosts\` + HostName,
		fallback:    true,
	},
	{
		id:          "brave",
		name:        "Brave",
		userDataRel: `BraveSoftware\Brave-Browser\User Data`,
		regSubKey:   `Software\BraveSoftware\Brave-Browser\NativeMessagingHosts\` + HostName,
	},
	{
		id:          "vivaldi",
		name:        "Vivaldi",
		userDataRel: `Vivaldi\User Data`,
		regSubKey:   `Software\Vivaldi\NativeMessagingHosts\` + HostName,
	},
	{
		id:          "chromium",
		name:        "Chromium",
		userDataRel: `Chromium\User Data`,
		regSubKey:   `Software\Chromium\NativeMessagingHosts\` + HostName,
	},
}

// Hooks for isolated unit tests so tests never mutate the real user registry.
var (
	winLocalAppDataFn = func(homeDir string) string {
		if v := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); v != "" && homeDir == "" {
			return v
		}
		if homeDir != "" {
			return filepath.Join(homeDir, "AppData", "Local")
		}
		return os.Getenv("LOCALAPPDATA")
	}
	winRegGetDefault = func(subKey string) (string, bool) {
		k, err := registry.OpenKey(registry.CURRENT_USER, subKey, registry.QUERY_VALUE)
		if err != nil {
			return "", false
		}
		defer k.Close()
		val, _, err := k.GetStringValue("")
		if err != nil {
			return "", false
		}
		return val, true
	}
	winRegSetDefault = func(subKey, value string) error {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, subKey, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer k.Close()
		return k.SetStringValue("", value)
	}
	winRegDeleteKey = func(subKey string) error {
		err := registry.DeleteKey(registry.CURRENT_USER, subKey)
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
)

func windowsManifestPath(configDir string) string {
	return filepath.Join(configDir, "native-messaging", HostName+".json")
}

// DetectBrowsers checks installed Chromium-family browsers on Windows and
// reports whether the ArcMD Native Messaging host is currently registered.
func DetectBrowsers(homeDir, configDir string) []BrowserTarget {
	localAppData := winLocalAppDataFn(homeDir)
	expectedManifest := ""
	if strings.TrimSpace(configDir) != "" {
		expectedManifest = windowsManifestPath(configDir)
	}
	out := make([]BrowserTarget, 0, len(winBrowserSpecs))
	for _, spec := range winBrowserSpecs {
		detected := false
		if localAppData != "" {
			detected = dirExists(filepath.Join(localAppData, spec.userDataRel))
		}
		regVal, ok := winRegGetDefault(spec.regSubKey)
		installed := ok && strings.TrimSpace(regVal) != "" && manifestFileMatches(regVal)
		if installed && expectedManifest != "" && !strings.EqualFold(filepath.Clean(regVal), filepath.Clean(expectedManifest)) {
			// Still installed, just pointing at another configDir's manifest.
			installed = manifestFileMatches(regVal)
		}
		out = append(out, BrowserTarget{
			ID:           spec.id,
			Name:         spec.name,
			Detected:     detected,
			Installed:    installed,
			ManifestPath: regVal,
			RegistryKey:  `HKCU\` + spec.regSubKey,
		})
	}
	return out
}

// InstallHost writes the shared Native Messaging manifest under configDir and
// registers HKCU NativeMessagingHosts keys for every detected browser (falling
// back to Chrome and Edge when no browser profile exists yet).
func InstallHost(homeDir, configDir, binPath string, extraExtIDs []string) (InstallResult, error) {
	if strings.TrimSpace(configDir) == "" {
		return InstallResult{}, fmt.Errorf("clipper: config directory is empty")
	}
	m, err := BuildManifest(binPath, extraExtIDs)
	if err != nil {
		return InstallResult{}, err
	}
	raw, err := MarshalManifest(m)
	if err != nil {
		return InstallResult{}, err
	}
	manifestPath := windowsManifestPath(configDir)
	if err := fsutil.WriteFileAtomic(manifestPath, raw, 0o644); err != nil {
		return InstallResult{}, fmt.Errorf("clipper: write manifest: %w", err)
	}

	targets := DetectBrowsers(homeDir, configDir)
	anyDetected := false
	for _, t := range targets {
		if t.Detected {
			anyDetected = true
			break
		}
	}
	installedCount := 0
	for i, spec := range winBrowserSpecs {
		if anyDetected && !targets[i].Detected && !targets[i].Installed {
			continue
		}
		if !anyDetected && !spec.fallback {
			continue
		}
		if err := winRegSetDefault(spec.regSubKey, manifestPath); err != nil {
			return InstallResult{}, fmt.Errorf("clipper: register %s: %w", spec.name, err)
		}
		installedCount++
	}
	return InstallResult{
		ManifestPath:   manifestPath,
		BinaryPath:     m.Path,
		AllowedOrigins: m.AllowedOrigins,
		Browsers:       DetectBrowsers(homeDir, configDir),
		InstalledCount: installedCount,
	}, nil
}

// UninstallHost removes ArcMD's HKCU Native Messaging registry keys and manifest file.
func UninstallHost(homeDir, configDir string) (int, error) {
	removed := 0
	for _, spec := range winBrowserSpecs {
		if _, ok := winRegGetDefault(spec.regSubKey); ok {
			if err := winRegDeleteKey(spec.regSubKey); err != nil {
				return removed, fmt.Errorf("clipper: unregister %s: %w", spec.name, err)
			}
			removed++
		}
	}
	if strings.TrimSpace(configDir) != "" {
		mp := windowsManifestPath(configDir)
		if err := os.Remove(mp); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, err
		}
	}
	return removed, nil
}
