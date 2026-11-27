package clipper

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Manifest is the Chrome Native Messaging host manifest JSON schema.
type Manifest struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Path           string   `json:"path"`
	Type           string   `json:"type"`
	AllowedOrigins []string `json:"allowed_origins"`
}

// BrowserTarget describes one Chromium-family browser detected on the machine.
type BrowserTarget struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Detected     bool   `json:"detected"`
	Installed    bool   `json:"installed"`
	ManifestPath string `json:"manifestPath,omitempty"`
	RegistryKey  string `json:"registryKey,omitempty"`
}

// InstallResult summarizes a Native Messaging Host installation.
type InstallResult struct {
	ManifestPath   string          `json:"manifestPath,omitempty"`
	BinaryPath     string          `json:"binaryPath"`
	AllowedOrigins []string        `json:"allowedOrigins"`
	Browsers       []BrowserTarget `json:"browsers"`
	InstalledCount int             `json:"installedCount"`
}

// BuildManifest constructs the Chrome Native Messaging manifest pointing to binPath.
func BuildManifest(binPath string, extraExtIDs []string) (Manifest, error) {
	binPath = strings.TrimSpace(binPath)
	if binPath == "" {
		return Manifest{}, fmt.Errorf("clipper: binary path is empty")
	}
	if !filepath.IsAbs(binPath) {
		if !strings.ContainsAny(binPath, `/\`) {
			if looked, err := exec.LookPath(binPath); err == nil && looked != "" {
				binPath = looked
			}
		}
		if !filepath.IsAbs(binPath) {
			if abs, err := filepath.Abs(binPath); err == nil {
				binPath = abs
			}
		}
	}
	return Manifest{
		Name:           HostName,
		Description:    HostDescription,
		Path:           binPath,
		Type:           "stdio",
		AllowedOrigins: AllowedOrigins(extraExtIDs),
	}, nil
}

// MarshalManifest encodes m as indented UTF-8 JSON with a trailing newline.
func MarshalManifest(m Manifest) ([]byte, error) {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func manifestFileMatches(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	return m.Name == HostName && m.Type == "stdio" && strings.TrimSpace(m.Path) != ""
}

func dirExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
