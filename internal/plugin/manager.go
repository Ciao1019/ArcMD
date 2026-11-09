package plugin

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/fsutil"
)

// Installed is a plugin in the plugins folder.
type Installed struct {
	Manifest *Manifest
	Dir      string // absolute folder holding the manifest
	Enabled  bool
}

// Broken is a folder of the plugins folder whose manifest does not load.
type Broken struct {
	Dir string
	Err error
}

// Manager keeps the plugins folder, <config dir>/ArcMD/plugins, with one
// folder per plugin named by its ID, and which of them are disabled.
type Manager struct {
	dir string
}

// NewManager returns the manager of the plugins folder dir.
func NewManager(dir string) *Manager { return &Manager{dir: dir} }

// Dir returns the plugins folder.
func (m *Manager) Dir() string { return m.dir }

// settings is plugins.json in the plugins folder.
type settings struct {
	Disabled []string `json:"disabled,omitempty"`
}

func (m *Manager) settingsPath() string { return filepath.Join(m.dir, "plugins.json") }

func (m *Manager) readSettings() settings {
	var s settings
	if b, err := os.ReadFile(m.settingsPath()); err == nil {
		json.Unmarshal(b, &s)
	}
	return s
}

// Scan lists the installed plugins, sorted by name, and the folders whose
// manifests are broken. A missing plugins folder is no plugins.
func (m *Manager) Scan() ([]*Installed, []Broken, error) {
	entries, err := os.ReadDir(m.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	disabled := m.readSettings().Disabled
	var out []*Installed
	var broken []Broken
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(m.dir, e.Name())
		b, err := os.ReadFile(filepath.Join(dir, ManifestFile))
		if err != nil {
			broken = append(broken, Broken{dir, err})
			continue
		}
		man, err := ParseManifest(b)
		if err == nil && man.ID != e.Name() {
			err = errors.New("plugin: folder name " + e.Name() + " differs from the manifest's id " + man.ID)
		}
		if err != nil {
			broken = append(broken, Broken{dir, err})
			continue
		}
		out = append(out, &Installed{Manifest: man, Dir: dir, Enabled: !slices.Contains(disabled, man.ID)})
	}
	slices.SortFunc(out, func(a, b *Installed) int {
		return strings.Compare(strings.ToLower(a.Manifest.Name), strings.ToLower(b.Manifest.Name))
	})
	return out, broken, nil
}

// SetEnabled enables or disables the plugin with the ID.
func (m *Manager) SetEnabled(id string, on bool) error {
	s := m.readSettings()
	s.Disabled = slices.DeleteFunc(s.Disabled, func(d string) bool { return d == id })
	if !on {
		s.Disabled = append(s.Disabled, id)
		slices.Sort(s.Disabled)
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(m.settingsPath(), append(b, '\n'), 0o644)
}

// Uninstall removes the plugin with the ID from the plugins folder.
func (m *Manager) Uninstall(id string) error {
	if !idPattern.MatchString(id) {
		return errors.New("plugin: invalid id " + id)
	}
	if err := m.SetEnabled(id, true); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(m.dir, id))
}
