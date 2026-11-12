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
	Version    int     `json:"version"`
	FontSize   float32 `json:"fontSize,omitempty"`
	LineHeight float32 `json:"lineHeight,omitempty"`
	NoAutoPair bool    `json:"noAutoPair,omitempty"`
	// AutoSpace puts a space between Chinese and Latin text as it is typed.
	AutoSpace bool `json:"autoSpace,omitempty"`
	// RecentCommands are the command bar's commands used last, latest
	// first, which it lists first.
	RecentCommands []string `json:"recentCommands,omitempty"`
	// Language is the interface's language, "zh-Hans" or "en"; "" follows
	// the system.
	Language string `json:"language,omitempty"`
	// Font is the body text's typeface, one of Fonts ("" the system's);
	// Page the text column's width, one of Pages ("" the standard 820).
	Font string `json:"font,omitempty"`
	Page string `json:"page,omitempty"`
	// Typewriter keeps the line being written in the middle of the page.
	Typewriter bool `json:"typewriter,omitempty"`
	// DrawAnnotations lets the hand-drawn marks in view draw themselves,
	// one after another, when a document opens.
	DrawAnnotations bool `json:"drawAnnotations,omitempty"`
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
	// TourSeen is set once the tour of the window was finished or skipped.
	// Until then every launch shows it: the first of a new installation,
	// and the first after an update from a version that had no tour.
	TourSeen bool `json:"tourSeen,omitempty"`
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
	p.normalizeReading()
	return &p, nil
}

// Save writes the preferences.
func (s Store) Save(p *Prefs) error {
	p.normalizeReading()
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

func (p *Prefs) ReadingSize() float32 {
	if p.FontSize >= 12 && p.FontSize <= 28 {
		return p.FontSize
	}
	return 16
}
func (p *Prefs) ReadingLine() float32 {
	if p.LineHeight >= 1.2 && p.LineHeight <= 2.2 {
		return p.LineHeight
	}
	return 26.0 / 16
}

// Fonts are the typefaces body text may be set in, and Pages the widths
// of the text column, by the names prefs.json keeps them under.
var (
	Fonts = []string{"", "serif", "kai", "mono"}
	Pages = []string{"narrow", "", "wide"}
)

// fontFamilies are the family lists of Fonts: the first a system has sets
// the text, Latin before CJK so each script gets a face of its kind.
var fontFamilies = map[string]string{
	"serif": "Georgia, Songti SC, STSong, Noto Serif CJK SC, Source Han Serif SC, SimSun, serif",
	"kai":   "Kaiti SC, STKaiti, KaiTi, Noto Serif CJK SC, serif",
	"mono":  "monospace",
}

// BodyFamily is the family list body text is set in, "" for the system's.
func (p *Prefs) BodyFamily() string { return fontFamilies[p.Font] }

// Column is the width of the text column: 680 narrow, 1040 wide, 0 for
// the standard 820.
func (p *Prefs) Column() float32 {
	switch p.Page {
	case "narrow":
		return 680
	case "wide":
		return 1040
	}
	return 0
}

func (p *Prefs) normalizeReading() {
	if p.Language != "zh-Hans" && p.Language != "en" {
		p.Language = ""
	}
	if _, ok := fontFamilies[p.Font]; !ok {
		p.Font = ""
	}
	if p.Page != "narrow" && p.Page != "wide" {
		p.Page = ""
	}
	if !(p.FontSize >= 12 && p.FontSize <= 28) {
		p.FontSize = 0
	}
	if !(p.LineHeight >= 1.2 && p.LineHeight <= 2.2) {
		p.LineHeight = 0
	}
}
