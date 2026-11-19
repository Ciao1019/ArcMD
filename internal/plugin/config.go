package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/fsutil"
)

// ConfigStore persists non-secret plugin settings under <Dir>/<pluginID>/settings.json.
type ConfigStore struct {
	Dir string
}

// SecretKey returns the credential store key scoped strictly to pluginID and fieldID.
func SecretKey(pluginID, fieldID string) (string, error) {
	if !idPattern.MatchString(pluginID) || !settingID.MatchString(fieldID) {
		return "", fmt.Errorf("plugin: invalid secret reference %q/%q", pluginID, fieldID)
	}
	return "plugin-" + pluginID + "-" + fieldID, nil
}

func (s *ConfigStore) pluginDir(pluginID string) (string, error) {
	if s == nil || s.Dir == "" {
		return "", errors.New("plugin: config store directory is not set")
	}
	if !idPattern.MatchString(pluginID) {
		return "", fmt.Errorf("plugin: invalid id %q", pluginID)
	}
	return filepath.Join(s.Dir, pluginID), nil
}

// SettingsPath returns the path to pluginID's settings.json.
func (s *ConfigStore) SettingsPath(pluginID string) (string, error) {
	dir, err := s.pluginDir(pluginID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// StatePath resolves a sandboxed per-plugin state filename inside <Dir>/<pluginID>/state/.
func (s *ConfigStore) StatePath(pluginID, key string) (string, error) {
	dir, err := s.pluginDir(pluginID)
	if err != nil {
		return "", err
	}
	if key == "" || key == "." || key == ".." || !filepath.IsLocal(key) || strings.ContainsAny(key, `/\`) {
		return "", fmt.Errorf("plugin: invalid state key %q", key)
	}
	return filepath.Join(dir, "state", key), nil
}

// Load reads saved settings for pluginID merged with manifest defaults (excluding secrets).
func (s *ConfigStore) Load(m *Manifest) (map[string]any, error) {
	out := defaultSettings(m)
	if s == nil || s.Dir == "" || m == nil {
		return out, nil
	}
	p, err := s.SettingsPath(m.ID)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	var saved map[string]any
	if err := json.Unmarshal(b, &saved); err != nil {
		return nil, fmt.Errorf("plugin: settings for %s: %w", m.ID, err)
	}
	secretFields := secretFieldSet(m)
	for k, v := range saved {
		if !secretFields[k] {
			out[k] = v
		}
	}
	return out, nil
}

// Save writes non-secret settings for pluginID atomically with 0600 permissions.
func (s *ConfigStore) Save(m *Manifest, values map[string]any) error {
	if m == nil {
		return errors.New("plugin: nil manifest")
	}
	p, err := s.SettingsPath(m.ID)
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0o700)

	clean := maps.Clone(values)
	if clean == nil {
		clean = map[string]any{}
	}
	for k := range secretFieldSet(m) {
		delete(clean, k)
	}
	b, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(p, append(b, '\n'), 0o600)
}

func defaultSettings(m *Manifest) map[string]any {
	out := map[string]any{}
	if m == nil {
		return out
	}
	for _, f := range m.Contributes.Settings {
		if f.Type == "secret" {
			continue
		}
		if f.Default != nil {
			out[f.ID] = f.Default
			continue
		}
		switch f.Type {
		case "bool":
			out[f.ID] = false
		case "spaces":
			out[f.ID] = []string{}
		default:
			out[f.ID] = ""
		}
	}
	return out
}

func secretFieldSet(m *Manifest) map[string]bool {
	out := map[string]bool{}
	if m == nil {
		return out
	}
	for _, f := range m.Contributes.Settings {
		if f.Type == "secret" {
			out[f.ID] = true
		}
	}
	return out
}
