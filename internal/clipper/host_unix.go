//go:build !windows

package clipper

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/fsutil"
)

type unixBrowserSpec struct {
	id       string
	name     string
	baseRel  string
	fallback bool
}

func unixBrowserSpecs(goos string) []unixBrowserSpec {
	if goos == "darwin" {
		return []unixBrowserSpec{
			{id: "chrome", name: "Chrome", baseRel: filepath.Join("Library", "Application Support", "Google", "Chrome"), fallback: true},
			{id: "arc", name: "Arc", baseRel: filepath.Join("Library", "Application Support", "Arc", "User Data")},
			{id: "edge", name: "Edge", baseRel: filepath.Join("Library", "Application Support", "Microsoft Edge")},
			{id: "brave", name: "Brave", baseRel: filepath.Join("Library", "Application Support", "BraveSoftware", "Brave-Browser")},
			{id: "vivaldi", name: "Vivaldi", baseRel: filepath.Join("Library", "Application Support", "Vivaldi")},
			{id: "chromium", name: "Chromium", baseRel: filepath.Join("Library", "Application Support", "Chromium")},
		}
	}
	return []unixBrowserSpec{
		{id: "chrome", name: "Chrome", baseRel: filepath.Join(".config", "google-chrome"), fallback: true},
		{id: "chromium", name: "Chromium", baseRel: filepath.Join(".config", "chromium"), fallback: true},
		{id: "edge", name: "Edge", baseRel: filepath.Join(".config", "microsoft-edge")},
		{id: "brave", name: "Brave", baseRel: filepath.Join(".config", "BraveSoftware", "Brave-Browser")},
		{id: "vivaldi", name: "Vivaldi", baseRel: filepath.Join(".config", "vivaldi")},
	}
}

// DetectBrowsers checks Chromium-family browser directories on macOS and Linux.
func DetectBrowsers(homeDir, _ string) []BrowserTarget {
	homeDir = strings.TrimSpace(homeDir)
	if homeDir == "" {
		return nil
	}
	specs := unixBrowserSpecs(runtime.GOOS)
	out := make([]BrowserTarget, 0, len(specs))
	for _, spec := range specs {
		base := filepath.Join(homeDir, spec.baseRel)
		manifestPath := filepath.Join(base, "NativeMessagingHosts", HostName+".json")
		out = append(out, BrowserTarget{
			ID:           spec.id,
			Name:         spec.name,
			Detected:     dirExists(base),
			Installed:    manifestFileMatches(manifestPath),
			ManifestPath: manifestPath,
		})
	}
	return out
}

// InstallHost writes the Native Messaging manifest JSON into every detected
// browser's NativeMessagingHosts folder (falling back to default browsers when
// none exist yet).
func InstallHost(homeDir, configDir, binPath string, extraExtIDs []string) (InstallResult, error) {
	homeDir = strings.TrimSpace(homeDir)
	if homeDir == "" {
		return InstallResult{}, fmt.Errorf("clipper: home directory is empty")
	}
	m, err := BuildManifest(binPath, extraExtIDs)
	if err != nil {
		return InstallResult{}, err
	}
	raw, err := MarshalManifest(m)
	if err != nil {
		return InstallResult{}, err
	}
	specs := unixBrowserSpecs(runtime.GOOS)
	targets := DetectBrowsers(homeDir, configDir)
	anyDetected := false
	for _, t := range targets {
		if t.Detected {
			anyDetected = true
			break
		}
	}
	installedCount := 0
	firstPath := ""
	var errs []error
	var warnings []string
	for i, spec := range specs {
		if anyDetected && !targets[i].Detected && !targets[i].Installed {
			continue
		}
		if !anyDetected && !spec.fallback {
			continue
		}
		mp := targets[i].ManifestPath
		if err := fsutil.WriteFileAtomic(mp, raw, 0o644); err != nil {
			wrapErr := fmt.Errorf("clipper: install %s manifest: %w", spec.name, err)
			errs = append(errs, wrapErr)
			warnings = append(warnings, wrapErr.Error())
			continue
		}
		if firstPath == "" {
			firstPath = mp
		}
		installedCount++
	}
	if installedCount == 0 && len(errs) > 0 {
		return InstallResult{}, errors.Join(errs...)
	}
	return InstallResult{
		ManifestPath:   firstPath,
		BinaryPath:     m.Path,
		AllowedOrigins: m.AllowedOrigins,
		Browsers:       DetectBrowsers(homeDir, configDir),
		InstalledCount: installedCount,
		Warnings:       warnings,
	}, nil
}

// UninstallHost removes ArcMD's Native Messaging manifest files from all known browsers.
func UninstallHost(homeDir, configDir string) (int, error) {
	targets := DetectBrowsers(homeDir, configDir)
	removed := 0
	for _, t := range targets {
		if !t.Installed {
			continue
		}
		if err := os.Remove(t.ManifestPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, fmt.Errorf("clipper: remove %s manifest: %w", t.Name, err)
		}
		removed++
	}
	return removed, nil
}
