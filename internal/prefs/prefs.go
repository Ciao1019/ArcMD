// Package prefs keeps ArcMD's preferences beyond the spaces: how focus
// mode writes and the keyboard shortcuts rebound, in prefs.json beside
// spaces.json. The package has no dependency on the window.
package prefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/Ciao1019/ArcMD/internal/fsutil"
)

// CurrentVersion is the version of prefs.json this ArcMD writes.
const CurrentVersion = 1

// Page zoom bounds and discrete keyboard/menu step (10%).
const (
	MinPageZoom     float32 = 0.50
	MaxPageZoom     float32 = 2.00
	DefaultPageZoom float32 = 1.00
	PageZoomStep    float32 = 0.10
)

// Prefs are the preferences.
type Prefs struct {
	Version    int     `json:"version"`
	FontSize   float32 `json:"fontSize,omitempty"`
	LineHeight float32 `json:"lineHeight,omitempty"`
	// PageZoom scales the entire document page proportionally (0.50–2.00);
	// 0 means 100% (DefaultPageZoom).
	PageZoom float32 `json:"pageZoom,omitempty"`
	// NoAutosave disables background automatic saving so edits stay in memory
	// until explicitly saved or confirmed when closing.
	NoAutosave bool `json:"noAutosave,omitempty"`
	NoAutoPair bool `json:"noAutoPair,omitempty"`
	// AutoSpace puts a space between Chinese and Latin text as it is typed.
	AutoSpace bool `json:"autoSpace,omitempty"`
	// CodeNoWrap disables soft wrapping in code blocks by default so long
	// lines scroll horizontally instead.
	CodeNoWrap bool `json:"codeNoWrap,omitempty"`
	// CodeMaxHeight and ImageMaxHeight cap the display height (DIP) of long
	// code blocks and images in the editor with internal scrolling; 0 means full height.
	CodeMaxHeight  float32 `json:"codeMaxHeight,omitempty"`
	ImageMaxHeight float32 `json:"imageMaxHeight,omitempty"`
	// RecentCommands are the command bar's commands used last, latest
	// first, which it lists first.
	RecentCommands []string `json:"recentCommands,omitempty"`
	// Language is the interface's language, "zh-Hans" or "en"; "" follows
	// the system.
	Language string `json:"language,omitempty"`
	// Font is the body text's typeface, one of Fonts ("" the system's);
	// Page the text column's width, one of Pages ("" the standard 820);
	// SidebarDensity the sidebar file tree row density, one of SidebarDensities
	// ("" the standard 32 DIP row height).
	Font           string `json:"font,omitempty"`
	Page           string `json:"page,omitempty"`
	SidebarDensity string `json:"sidebarDensity,omitempty"`
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
	OutlineClosed      bool `json:"outlineClosed,omitempty"`
	MindMapBothSides   bool `json:"mindMapBothSides,omitempty"`
	MindMapOpen        bool `json:"mindMapOpen,omitempty"`
	SourceSplitSwapped bool `json:"sourceSplitSwapped,omitempty"`
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
	// AI is the model the space's questions are asked of. Its API key is
	// not here but in a separate local file managed by Secrets.
	AI AI `json:"ai,omitzero"`
}

// AI selects a model service and its streaming protocol.
type AI struct {
	// Provider is a preset's ID (agent.Presets), "custom" for another.
	Provider string `json:"provider,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	BaseURL  string `json:"baseURL,omitempty"`
	Model    string `json:"model,omitempty"`
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
	p.normalize()
	return &p, nil
}

// Save writes the preferences.
func (s Store) Save(p *Prefs) error {
	p.normalize()
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

// NormalizePageZoom clamps zoom to [MinPageZoom, MaxPageZoom] rounded to whole
// percents, returning DefaultPageZoom for invalid or non-positive values.
func NormalizePageZoom(z float32) float32 {
	if math.IsNaN(float64(z)) || math.IsInf(float64(z), 0) || z <= 0 {
		return DefaultPageZoom
	}
	pct := math.Round(float64(z) * 100)
	if pct < float64(MinPageZoom*100) {
		pct = float64(MinPageZoom * 100)
	}
	if pct > float64(MaxPageZoom*100) {
		pct = float64(MaxPageZoom * 100)
	}
	return float32(pct / 100)
}

// StepPageZoom moves cur up (delta > 0) or down (delta < 0) in PageZoomStep
// (10%) stops within [MinPageZoom, MaxPageZoom].
func StepPageZoom(cur float32, delta int) float32 {
	z := NormalizePageZoom(cur)
	if delta == 0 {
		return z
	}
	pct := int(math.Round(float64(z) * 100))
	step := int(math.Round(float64(PageZoomStep) * 100))
	minPct := int(math.Round(float64(MinPageZoom) * 100))
	maxPct := int(math.Round(float64(MaxPageZoom) * 100))
	if delta > 0 {
		next := ((pct / step) + 1) * step
		if delta > 1 {
			next += (delta - 1) * step
		}
		if next > maxPct {
			next = maxPct
		}
		return float32(next) / 100
	}
	next := ((pct - 1) / step) * step
	if delta < -1 {
		next += (delta + 1) * step
	}
	if next < minPct {
		next = minPct
	}
	return float32(next) / 100
}

// Zoom returns the document page zoom factor (1.0 when unset).
func (p *Prefs) Zoom() float32 {
	if p == nil {
		return DefaultPageZoom
	}
	return NormalizePageZoom(p.PageZoom)
}

// Fonts are the typefaces body text may be set in, Pages the widths
// of the text column, and SidebarDensities the sidebar row densities,
// by the names prefs.json keeps them under.
var (
	Fonts            = []string{"", "serif", "kai", "mono"}
	Pages            = []string{"narrow", "", "wide", "full"}
	SidebarDensities = []string{"compact", "", "comfortable"}
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
// the standard 820 (FullWidth takes precedence when Page is "full").
func (p *Prefs) Column() float32 {
	switch p.Page {
	case "narrow":
		return 680
	case "wide":
		return 1040
	}
	return 0
}

// FullWidth reports whether the reader chose to span the full editor viewport.
func (p *Prefs) FullWidth() bool { return p.Page == "full" }

// SidebarRowHeight returns the sidebar file tree row height in DIPs:
// 28 compact, 32 standard (default), 40 comfortable.
func (p *Prefs) SidebarRowHeight() float32 {
	if p == nil {
		return 32
	}
	switch p.SidebarDensity {
	case "compact":
		return 28
	case "comfortable":
		return 40
	default:
		return 32
	}
}

func (p *Prefs) normalize() {
	if p.AI.Protocol != "responses" && p.AI.Protocol != "chat-completions" {
		p.AI.Protocol = ""
	}
	if p.Language != "zh-Hans" && p.Language != "en" {
		p.Language = ""
	}
	if _, ok := fontFamilies[p.Font]; !ok {
		p.Font = ""
	}
	if p.Page != "narrow" && p.Page != "wide" && p.Page != "full" {
		p.Page = ""
	}
	if p.SidebarDensity != "compact" && p.SidebarDensity != "comfortable" {
		p.SidebarDensity = ""
	}
	if !(p.FontSize >= 12 && p.FontSize <= 28) {
		p.FontSize = 0
	}
	if !(p.LineHeight >= 1.2 && p.LineHeight <= 2.2) {
		p.LineHeight = 0
	}
	if p.PageZoom != 0 {
		z := NormalizePageZoom(p.PageZoom)
		if z == DefaultPageZoom {
			p.PageZoom = 0
		} else {
			p.PageZoom = z
		}
	}
	p.CodeMaxHeight = sanitizeMaxHeight(p.CodeMaxHeight)
	p.ImageMaxHeight = sanitizeMaxHeight(p.ImageMaxHeight)
}

func sanitizeMaxHeight(h float32) float32 {
	if !(h > 0) {
		return 0
	}
	if h > 2000 {
		return 2000
	}
	return h
}
