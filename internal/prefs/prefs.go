// Package prefs keeps ArcMD's preferences beyond the spaces: how focus
// mode writes and the keyboard shortcuts rebound, in prefs.json beside
// spaces.json. The package has no dependency on the window.
package prefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Ciao1019/ArcMD/internal/fsutil"
)

// CurrentVersion is the version of prefs.json this ArcMD writes.
const CurrentVersion = 1

// Prefs are the preferences.
type Prefs struct {
	Version int `json:"version"`
	// Typewriter keeps the line being written in the middle of the page.
	Typewriter bool `json:"typewriter,omitempty"`
	// NoDim keeps the other paragraphs bright in focus mode, which fades
	// them by default.
	NoDim bool `json:"noDim,omitempty"`
	// OutlineClosed collapses the outline left of the page, which is open
	// by default.
	OutlineClosed bool `json:"outlineClosed,omitempty"`
	// Keys rebind commands, by their IDs, to shortcuts written as
	// "Cmd+Shift+K"; a command left out keeps its default.
	Keys map[string]string `json:"keys,omitempty"`
	// NoAutoUpdate leaves new versions to the user: a background check
	// then only says one is out, instead of downloading it.
	NoAutoUpdate bool `json:"noAutoUpdate,omitempty"`
	// PendingUpdate is the version ArcMD restarted to install; the launch
	// that runs it says ArcMD was updated, then clears it.
	PendingUpdate string `json:"pendingUpdate,omitempty"`
}

// Store reads and writes prefs.json.
type Store struct{ Path string }

// Load reads the preferences; none yet are the defaults. A newer version
// is refused rather than overwritten.
func (s Store) Load() (*Prefs, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return &Prefs{Version: CurrentVersion}, nil
	}
	if err != nil {
		return nil, err
	}
	var p Prefs
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("prefs: %w", err)
	}
	if p.Version > CurrentVersion || p.Version < 1 {
		return nil, fmt.Errorf("prefs: unsupported version %d", p.Version)
	}
	return &p, nil
}

// Save writes the preferences.
func (s Store) Save(p *Prefs) error {
	p.Version = CurrentVersion
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(s.Path, append(data, '\n'), 0o644)
}
