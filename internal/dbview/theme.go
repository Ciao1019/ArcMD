// Package dbview draws ArcMD's native Notion-style database views using MyGo's
// immediate-mode UI toolkit. It communicates with the application host solely
// through Options callbacks and never imports internal/app or internal/editor.
package dbview

import (
	"strings"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/egoist/mygo/ui"
)

// Palette holds colors derived from the active paper/chrome ui.Theme so every
// table cell, pill, popover, and border adapts automatically to light and dark mode.
type Palette struct {
	Dark          bool
	Paper         ui.Color
	Surface       ui.Color
	HeaderBg      ui.Color
	RowHover      ui.Color
	RowSelected   ui.Color
	RowAlt        ui.Color
	Border        ui.Color
	BorderStrong  ui.Color
	Text          ui.Color
	TextMuted     ui.Color
	Accent        ui.Color
	AccentSoft    ui.Color
	Danger        ui.Color
	PopoverBg     ui.Color
	PopoverBorder ui.Color
	PopoverShadow ui.Color
	ShadowEdge    ui.Color
}

func paletteOf(t *ui.Theme) Palette {
	if t == nil {
		return Palette{
			Paper:     ui.RGB(255, 255, 255),
			Surface:   ui.RGB(247, 247, 248),
			HeaderBg:  ui.RGB(249, 249, 250),
			Text:      ui.RGB(30, 31, 35),
			TextMuted: ui.RGB(115, 118, 128),
			Accent:    ui.RGB(59, 130, 246),
			Danger:    ui.RGB(220, 38, 38),
		}
	}
	if t.Dark {
		paper := t.Background
		if paper.A == 0 {
			paper = ui.Hex("#1F2023")
		}
		return Palette{
			Dark:          true,
			Paper:         paper,
			Surface:       paper.Mix(ui.RGB(255, 255, 255), 0.045),
			HeaderBg:      paper.Mix(ui.RGB(255, 255, 255), 0.032),
			RowHover:      paper.Mix(ui.RGB(255, 255, 255), 0.055),
			RowSelected:   paper.Mix(t.Accent, 0.15),
			RowAlt:        ui.RGBA(255, 255, 255, 0.018),
			Border:        ui.RGBA(255, 255, 255, 0.085),
			BorderStrong:  ui.RGBA(255, 255, 255, 0.16),
			Text:          t.Text,
			TextMuted:     t.TextMuted,
			Accent:        t.Accent,
			AccentSoft:    t.Accent.Alpha(0.16),
			Danger:        t.Danger,
			PopoverBg:     ui.Hex("#26282D"),
			PopoverBorder: ui.RGBA(255, 255, 255, 0.12),
			PopoverShadow: ui.RGBA(0, 0, 0, 0.52),
			ShadowEdge:    ui.RGBA(0, 0, 0, 0.28),
		}
	}
	paper := t.Background
	if paper.A == 0 {
		paper = ui.RGB(255, 255, 255)
	}
	return Palette{
		Dark:          false,
		Paper:         paper,
		Surface:       ui.RGBA(15, 23, 42, 0.038),
		HeaderBg:      paper.Mix(ui.RGB(15, 23, 42), 0.024),
		RowHover:      paper.Mix(ui.RGB(15, 23, 42), 0.032),
		RowSelected:   paper.Mix(t.Accent, 0.085),
		RowAlt:        ui.RGBA(15, 23, 42, 0.014),
		Border:        ui.RGBA(15, 23, 42, 0.075),
		BorderStrong:  ui.RGBA(15, 23, 42, 0.15),
		Text:          t.Text,
		TextMuted:     t.TextMuted,
		Accent:        t.Accent,
		AccentSoft:    t.Accent.Alpha(0.11),
		Danger:        t.Danger,
		PopoverBg:     ui.RGB(255, 255, 255),
		PopoverBorder: ui.RGBA(15, 23, 42, 0.10),
		PopoverShadow: ui.RGBA(15, 23, 42, 0.14),
		ShadowEdge:    ui.RGBA(15, 23, 42, 0.075),
	}
}

type colorPair struct {
	lightBg, lightFg ui.Color
	darkBg, darkFg   ui.Color
	dotLight         ui.Color
	dotDark          ui.Color
}

var optionColorMap = map[string]colorPair{
	"default": {
		lightBg: ui.RGBA(120, 124, 134, 0.14), lightFg: ui.Hex("#474A52"),
		darkBg: ui.RGBA(255, 255, 255, 0.10), darkFg: ui.Hex("#D4D6DC"),
		dotLight: ui.Hex("#8B909A"), dotDark: ui.Hex("#9EA3AE"),
	},
	"gray": {
		lightBg: ui.RGBA(120, 124, 134, 0.16), lightFg: ui.Hex("#4B5563"),
		darkBg: ui.RGBA(148, 163, 184, 0.18), darkFg: ui.Hex("#CBD5E1"),
		dotLight: ui.Hex("#6B7280"), dotDark: ui.Hex("#94A3B8"),
	},
	"brown": {
		lightBg: ui.RGBA(146, 102, 74, 0.16), lightFg: ui.Hex("#6B442E"),
		darkBg: ui.RGBA(180, 128, 96, 0.22), darkFg: ui.Hex("#E4C1AA"),
		dotLight: ui.Hex("#9A6547"), dotDark: ui.Hex("#C58D6B"),
	},
	"orange": {
		lightBg: ui.RGBA(234, 120, 34, 0.15), lightFg: ui.Hex("#9A4709"),
		darkBg: ui.RGBA(251, 146, 60, 0.22), darkFg: ui.Hex("#FDBA74"),
		dotLight: ui.Hex("#EA580C"), dotDark: ui.Hex("#FB923C"),
	},
	"yellow": {
		lightBg: ui.RGBA(234, 179, 8, 0.18), lightFg: ui.Hex("#854D0E"),
		darkBg: ui.RGBA(250, 204, 21, 0.20), darkFg: ui.Hex("#FDE047"),
		dotLight: ui.Hex("#CA8A04"), dotDark: ui.Hex("#FACC15"),
	},
	"green": {
		lightBg: ui.RGBA(34, 154, 92, 0.14), lightFg: ui.Hex("#17643B"),
		darkBg: ui.RGBA(52, 211, 153, 0.20), darkFg: ui.Hex("#6EE7B7"),
		dotLight: ui.Hex("#16A34A"), dotDark: ui.Hex("#34D399"),
	},
	"blue": {
		lightBg: ui.RGBA(59, 130, 246, 0.14), lightFg: ui.Hex("#1D4ED8"),
		darkBg: ui.RGBA(96, 165, 250, 0.22), darkFg: ui.Hex("#93C5FD"),
		dotLight: ui.Hex("#2563EB"), dotDark: ui.Hex("#60A5FA"),
	},
	"purple": {
		lightBg: ui.RGBA(139, 92, 246, 0.14), lightFg: ui.Hex("#5B21B6"),
		darkBg: ui.RGBA(167, 139, 250, 0.22), darkFg: ui.Hex("#C4B5FD"),
		dotLight: ui.Hex("#7C3AED"), dotDark: ui.Hex("#A78BFA"),
	},
	"pink": {
		lightBg: ui.RGBA(236, 72, 153, 0.14), lightFg: ui.Hex("#9D174D"),
		darkBg: ui.RGBA(244, 114, 182, 0.22), darkFg: ui.Hex("#F9A8D4"),
		dotLight: ui.Hex("#DB2777"), dotDark: ui.Hex("#F472B6"),
	},
	"red": {
		lightBg: ui.RGBA(239, 68, 68, 0.14), lightFg: ui.Hex("#B91C1C"),
		darkBg: ui.RGBA(248, 113, 113, 0.22), darkFg: ui.Hex("#FCA5A5"),
		dotLight: ui.Hex("#DC2626"), dotDark: ui.Hex("#F87171"),
	},
}

// TagColors returns the pill background and foreground colors for a Notion option color name.
func TagColors(dark bool, name string) (bg, fg ui.Color) {
	p, ok := optionColorMap[name]
	if !ok {
		p = optionColorMap["default"]
	}
	if dark {
		return p.darkBg, p.darkFg
	}
	return p.lightBg, p.lightFg
}

// StatusDotColor returns a crisp dot indicator color for PropStatus pills.
func StatusDotColor(dark bool, name string) ui.Color {
	p, ok := optionColorMap[name]
	if !ok {
		p = optionColorMap["default"]
	}
	if dark {
		return p.dotDark
	}
	return p.dotLight
}

// RowTintColor returns a subtle conditional background tint and matching accent
// color for a Notion color token.
func RowTintColor(dark bool, name string) (bg, accent ui.Color) {
	accent = StatusDotColor(dark, name)
	if dark {
		return accent.Alpha(0.16), accent
	}
	return accent.Alpha(0.10), accent
}

func tintedRowBackground(pal Palette, res *database.QueryResult, recID string) (bg, accent ui.Color, ok bool) {
	if res == nil || len(res.RowColors) == 0 || recID == "" {
		return ui.Color{}, ui.Color{}, false
	}
	colName := strings.TrimSpace(res.RowColors[recID])
	if colName == "" || colName == "default" {
		return ui.Color{}, ui.Color{}, false
	}
	accent = StatusDotColor(pal.Dark, colName)
	ratio := float32(0.09)
	if pal.Dark {
		ratio = 0.15
	}
	// Keep row/card fill opaque so floating frozen table columns never bleed through.
	return pal.Paper.Mix(accent, ratio), accent, true
}
