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
	AccentText    ui.Color
	AccentSoft    ui.Color
	Danger        ui.Color
	OnSolid       ui.Color
	PopoverBg     ui.Color
	PopoverBorder ui.Color
	PopoverShadow ui.Color
	ShadowEdge    ui.Color
}

func paletteOf(t *ui.Theme) Palette {
	if t == nil {
		return Palette{
			Paper:      ui.RGB(255, 255, 255),
			Surface:    ui.RGB(247, 247, 248),
			HeaderBg:   ui.RGB(249, 249, 250),
			Text:       ui.RGB(30, 31, 35),
			TextMuted:  ui.RGB(115, 118, 128),
			Accent:     ui.RGB(59, 130, 246),
			AccentText: ui.RGB(255, 255, 255),
			Danger:     ui.RGB(220, 38, 38),
			OnSolid:    ui.RGB(255, 255, 255),
		}
	}
	accent := t.Accent
	if accent.A == 0 {
		accent = ui.RGB(59, 130, 246)
	}
	accentText := t.AccentText
	if accentText.A == 0 {
		accentText = ui.RGB(255, 255, 255)
	}
	danger := t.Danger
	if danger.A == 0 {
		danger = StatusDotColor(t.Dark, "red")
	}
	onSolid := ui.RGB(255, 255, 255)
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
			RowSelected:   paper.Mix(accent, 0.15),
			RowAlt:        ui.RGBA(255, 255, 255, 0.018),
			Border:        ui.RGBA(255, 255, 255, 0.085),
			BorderStrong:  ui.RGBA(255, 255, 255, 0.16),
			Text:          t.Text,
			TextMuted:     t.TextMuted,
			Accent:        accent,
			AccentText:    accentText,
			AccentSoft:    accent.Alpha(0.16),
			Danger:        danger,
			OnSolid:       onSolid,
			PopoverBg:     paper.Mix(ui.RGB(255, 255, 255), 0.032),
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
		RowSelected:   paper.Mix(accent, 0.085),
		RowAlt:        ui.RGBA(15, 23, 42, 0.014),
		Border:        ui.RGBA(15, 23, 42, 0.075),
		BorderStrong:  ui.RGBA(15, 23, 42, 0.15),
		Text:          t.Text,
		TextMuted:     t.TextMuted,
		Accent:        accent,
		AccentText:    accentText,
		AccentSoft:    accent.Alpha(0.11),
		Danger:        danger,
		OnSolid:       onSolid,
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

type beTableColors struct {
	cardBg              ui.Color
	headerBg            ui.Color
	footerBg            ui.Color
	border              ui.Color
	rowBorder           ui.Color
	rowHover            ui.Color
	rowSelected         ui.Color
	primary             ui.Color
	primaryFg           ui.Color
	primaryHover        ui.Color
	checkboxBorder      ui.Color
	checkboxBorderHover ui.Color
	grip                ui.Color
	chevronMuted        ui.Color
	resizeHover         ui.Color
	metaInk             ui.Color
}

func beTablePalette(pal Palette) beTableColors {
	primary := pal.Accent
	if primary.A == 0 {
		primary = ui.RGB(59, 130, 246)
	}
	primaryFg := pal.AccentText
	if primaryFg.A == 0 {
		primaryFg = ui.RGB(255, 255, 255)
	}
	onSolid := pal.OnSolid
	if onSolid.A == 0 {
		onSolid = ui.RGB(255, 255, 255)
	}
	if pal.Dark {
		card := pal.Paper.Mix(ui.RGB(255, 255, 255), 0.018)
		return beTableColors{
			cardBg:              card,
			headerBg:            pal.Paper.Mix(ui.RGB(255, 255, 255), 0.042),
			footerBg:            pal.Paper.Mix(ui.RGB(255, 255, 255), 0.030),
			border:              pal.Border,
			rowBorder:           pal.Border.Alpha(0.05),
			rowHover:            card.Mix(ui.RGB(255, 255, 255), 0.042),
			rowSelected:         card.Mix(primary, 0.14),
			primary:             primary,
			primaryFg:           primaryFg,
			primaryHover:        primary.Mix(onSolid, 0.14),
			checkboxBorder:      ui.RGBA(161, 161, 170, 0.36),
			checkboxBorderHover: ui.Hex("#A1A1AA"),
			grip:                ui.RGBA(161, 161, 170, 0.45),
			chevronMuted:        ui.RGBA(161, 161, 170, 0.42),
			resizeHover:         primary.Alpha(0.42),
			metaInk:             pal.TextMuted,
		}
	}
	card := pal.Paper
	return beTableColors{
		cardBg:              card,
		headerBg:            card.Mix(ui.RGB(15, 23, 42), 0.026),
		footerBg:            card.Mix(ui.RGB(15, 23, 42), 0.018),
		border:              pal.Border,
		rowBorder:           pal.Border.Alpha(0.05),
		rowHover:            card.Mix(ui.RGB(15, 23, 42), 0.028),
		rowSelected:         card.Mix(primary, 0.08),
		primary:             primary,
		primaryFg:           primaryFg,
		primaryHover:        primary.Mix(onSolid, 0.14),
		checkboxBorder:      ui.RGBA(100, 116, 139, 0.36),
		checkboxBorderHover: ui.Hex("#64748B"),
		grip:                ui.RGBA(100, 116, 139, 0.48),
		chevronMuted:        ui.RGBA(100, 116, 139, 0.42),
		resizeHover:         primary.Alpha(0.38),
		metaInk:             pal.TextMuted,
	}
}

type bePillPair struct {
	lightBg, lightFg ui.Color
	darkBg, darkFg   ui.Color
}

var bePillColorMap = map[string]bePillPair{
	"default": {
		lightBg: ui.RGBA(113, 113, 122, 0.11), lightFg: ui.Hex("#3F3F46"),
		darkBg: ui.RGBA(161, 161, 170, 0.14), darkFg: ui.Hex("#D4D4D8"),
	},
	"gray": {
		lightBg: ui.RGBA(113, 113, 122, 0.12), lightFg: ui.Hex("#52525B"),
		darkBg: ui.RGBA(161, 161, 170, 0.16), darkFg: ui.Hex("#D4D4D8"),
	},
	"brown": {
		lightBg: ui.RGBA(180, 83, 9, 0.11), lightFg: ui.Hex("#92400E"),
		darkBg: ui.RGBA(217, 119, 6, 0.18), darkFg: ui.Hex("#FCD34D"),
	},
	"orange": {
		lightBg: ui.RGBA(245, 158, 11, 0.14), lightFg: ui.Hex("#B45309"),
		darkBg: ui.RGBA(245, 158, 11, 0.14), darkFg: ui.Hex("#FBBF24"),
	},
	"yellow": {
		lightBg: ui.RGBA(234, 179, 8, 0.15), lightFg: ui.Hex("#A16207"),
		darkBg: ui.RGBA(250, 204, 21, 0.15), darkFg: ui.Hex("#FDE047"),
	},
	"green": {
		lightBg: ui.RGBA(16, 185, 129, 0.12), lightFg: ui.Hex("#059669"),
		darkBg: ui.RGBA(16, 185, 129, 0.14), darkFg: ui.Hex("#34D399"),
	},
	"blue": {
		lightBg: ui.RGBA(59, 130, 246, 0.12), lightFg: ui.Hex("#2563EB"),
		darkBg: ui.RGBA(59, 130, 246, 0.16), darkFg: ui.Hex("#60A5FA"),
	},
	"purple": {
		lightBg: ui.RGBA(139, 92, 246, 0.12), lightFg: ui.Hex("#7C3AED"),
		darkBg: ui.RGBA(139, 92, 246, 0.18), darkFg: ui.Hex("#C4B5FD"),
	},
	"pink": {
		lightBg: ui.RGBA(236, 72, 153, 0.12), lightFg: ui.Hex("#DB2777"),
		darkBg: ui.RGBA(236, 72, 153, 0.16), darkFg: ui.Hex("#F472B6"),
	},
	"red": {
		lightBg: ui.RGBA(244, 63, 94, 0.12), lightFg: ui.Hex("#E11D48"),
		darkBg: ui.RGBA(244, 63, 94, 0.14), darkFg: ui.Hex("#FB7185"),
	},
}

func bePillColors(dark bool, colorName string) (bg, fg ui.Color) {
	key := strings.ToLower(strings.TrimSpace(colorName))
	if key == "" {
		key = "default"
	}
	pair, ok := bePillColorMap[key]
	if !ok {
		pair = bePillColorMap["default"]
	}
	if dark {
		return pair.darkBg, pair.darkFg
	}
	return pair.lightBg, pair.lightFg
}

type dfEventColor struct {
	bg           ui.Color
	selectedBg   ui.Color
	line         ui.Color
	text         ui.Color
	selectedText ui.Color
}

var dayflowEventPaletteMap = map[string]struct {
	light dfEventColor
	dark  dfEventColor
}{
	"blue": {
		light: dfEventColor{
			bg: ui.Hex("#EFF6FF"), selectedBg: ui.Hex("#3B82F6"),
			line: ui.Hex("#3B82F6"), text: ui.Hex("#1E3A8A"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(30, 64, 175, 0.42), selectedBg: ui.Hex("#2563EB"),
			line: ui.Hex("#60A5FA"), text: ui.Hex("#DBEAFE"), selectedText: ui.RGB(255, 255, 255),
		},
	},
	"green": {
		light: dfEventColor{
			bg: ui.Hex("#F0FDF4"), selectedBg: ui.Hex("#10B981"),
			line: ui.Hex("#10B981"), text: ui.Hex("#064E3B"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(4, 120, 87, 0.42), selectedBg: ui.Hex("#059669"),
			line: ui.Hex("#34D399"), text: ui.Hex("#D1FAE5"), selectedText: ui.RGB(255, 255, 255),
		},
	},
	"purple": {
		light: dfEventColor{
			bg: ui.Hex("#FAF5FF"), selectedBg: ui.Hex("#8B5CF6"),
			line: ui.Hex("#8B5CF6"), text: ui.Hex("#5B21B6"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(109, 40, 217, 0.42), selectedBg: ui.Hex("#7C3AED"),
			line: ui.Hex("#A78BFA"), text: ui.Hex("#EDE9FE"), selectedText: ui.RGB(255, 255, 255),
		},
	},
	"yellow": {
		light: dfEventColor{
			bg: ui.Hex("#FEFCE8"), selectedBg: ui.Hex("#F59E0B"),
			line: ui.Hex("#F59E0B"), text: ui.Hex("#78350F"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(180, 83, 9, 0.42), selectedBg: ui.Hex("#D97706"),
			line: ui.Hex("#FBBF24"), text: ui.Hex("#FEF3C7"), selectedText: ui.RGB(255, 255, 255),
		},
	},
	"red": {
		light: dfEventColor{
			bg: ui.Hex("#FEF2F2"), selectedBg: ui.Hex("#EF4444"),
			line: ui.Hex("#EF4444"), text: ui.Hex("#7F1D1D"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(185, 28, 28, 0.42), selectedBg: ui.Hex("#DC2626"),
			line: ui.Hex("#F87171"), text: ui.Hex("#FEE2E2"), selectedText: ui.RGB(255, 255, 255),
		},
	},
	"orange": {
		light: dfEventColor{
			bg: ui.Hex("#FFF7ED"), selectedBg: ui.Hex("#F97316"),
			line: ui.Hex("#F97316"), text: ui.Hex("#7C2D12"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(194, 65, 12, 0.42), selectedBg: ui.Hex("#EA580C"),
			line: ui.Hex("#FB923C"), text: ui.Hex("#FED7AA"), selectedText: ui.RGB(255, 255, 255),
		},
	},
	"pink": {
		light: dfEventColor{
			bg: ui.Hex("#FDF2F8"), selectedBg: ui.Hex("#EC4899"),
			line: ui.Hex("#EC4899"), text: ui.Hex("#831843"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(190, 24, 93, 0.42), selectedBg: ui.Hex("#DB2777"),
			line: ui.Hex("#F472B6"), text: ui.Hex("#FCE7F3"), selectedText: ui.RGB(255, 255, 255),
		},
	},
	"brown": {
		light: dfEventColor{
			bg: ui.Hex("#FBF6F1"), selectedBg: ui.Hex("#B45309"),
			line: ui.Hex("#B45309"), text: ui.Hex("#713F12"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(146, 64, 14, 0.42), selectedBg: ui.Hex("#B45309"),
			line: ui.Hex("#D97706"), text: ui.Hex("#FDE68A"), selectedText: ui.RGB(255, 255, 255),
		},
	},
	"gray": {
		light: dfEventColor{
			bg: ui.Hex("#F3F4F6"), selectedBg: ui.Hex("#64748B"),
			line: ui.Hex("#64748B"), text: ui.Hex("#334155"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(71, 85, 105, 0.42), selectedBg: ui.Hex("#475569"),
			line: ui.Hex("#94A3B8"), text: ui.Hex("#E2E8F0"), selectedText: ui.RGB(255, 255, 255),
		},
	},
	"default": {
		light: dfEventColor{
			bg: ui.Hex("#F0FDFA"), selectedBg: ui.Hex("#14B8A6"),
			line: ui.Hex("#14B8A6"), text: ui.Hex("#134E4A"), selectedText: ui.RGB(255, 255, 255),
		},
		dark: dfEventColor{
			bg: ui.RGBA(15, 118, 110, 0.42), selectedBg: ui.Hex("#0D9488"),
			line: ui.Hex("#2DD4BF"), text: ui.Hex("#CCFBF1"), selectedText: ui.RGB(255, 255, 255),
		},
	},
}

func dayflowEventColors(dark bool, colorName string) dfEventColor {
	key := strings.ToLower(strings.TrimSpace(colorName))
	if key == "" {
		key = "blue"
	}
	pair, ok := dayflowEventPaletteMap[key]
	if !ok {
		pair = dayflowEventPaletteMap["blue"]
	}
	if dark {
		return pair.dark
	}
	return pair.light
}

type dfCalChrome struct {
	todayBg         ui.Color
	todayFg         ui.Color
	weekendBg       ui.Color
	nowLine         ui.Color
	nowBadgeBg      ui.Color
	nowBadgeFg      ui.Color
	gridLine        ui.Color
	subGridLine     ui.Color
	segTrackBg      ui.Color
	segActiveBg     ui.Color
	headerBtnBg     ui.Color
	headerBtnBorder ui.Color
	cellOutBg       ui.Color
	cellSelectedBg  ui.Color
	sidebarBg       ui.Color
	catProp         *database.Property
}

func dfCalendarPalette(pal Palette) dfCalChrome {
	nowCol := pal.Danger
	if nowCol.A == 0 {
		nowCol = StatusDotColor(pal.Dark, "red")
	}
	onSolid := pal.OnSolid
	if onSolid.A == 0 {
		onSolid = ui.RGB(255, 255, 255)
	}
	todayBg := pal.Text
	if pal.Dark {
		if todayBg.A == 0 {
			todayBg = ui.Hex("#E3E4E7")
		}
		return dfCalChrome{
			todayBg:         todayBg,
			todayFg:         pal.Paper,
			weekendBg:       pal.Paper.Mix(ui.RGB(255, 255, 255), 0.024),
			nowLine:         nowCol,
			nowBadgeBg:      nowCol,
			nowBadgeFg:      onSolid,
			gridLine:        pal.Border,
			subGridLine:     pal.Border.Alpha(0.045),
			segTrackBg:      pal.Surface,
			segActiveBg:     pal.Paper.Mix(ui.RGB(255, 255, 255), 0.12),
			headerBtnBg:     pal.Surface,
			headerBtnBorder: pal.BorderStrong,
			cellOutBg:       pal.Paper.Mix(ui.RGB(0, 0, 0), 0.16),
			cellSelectedBg:  pal.RowSelected,
			sidebarBg:       pal.HeaderBg,
		}
	}
	if todayBg.A == 0 {
		todayBg = ui.Hex("#18181B")
	}
	return dfCalChrome{
		todayBg:         todayBg,
		todayFg:         pal.Paper,
		weekendBg:       pal.Accent.Alpha(0.032),
		nowLine:         nowCol,
		nowBadgeBg:      nowCol,
		nowBadgeFg:      onSolid,
		gridLine:        pal.Border,
		subGridLine:     pal.Border.Alpha(0.035),
		segTrackBg:      pal.Surface,
		segActiveBg:     pal.Paper,
		headerBtnBg:     pal.Paper,
		headerBtnBorder: pal.BorderStrong,
		cellOutBg:       pal.Paper.Mix(ui.RGB(15, 23, 42), 0.022),
		cellSelectedBg:  pal.RowSelected,
		sidebarBg:       pal.HeaderBg,
	}
}

var boardAvatarPalette = []ui.Color{
	ui.Hex("#3B82F6"),
	ui.Hex("#8B5CF6"),
	ui.Hex("#10B981"),
	ui.Hex("#F59E0B"),
	ui.Hex("#EC4899"),
	ui.Hex("#0EA5E9"),
	ui.Hex("#EA580C"),
	ui.Hex("#14B8A6"),
}

func personAvatarColor(name string) ui.Color {
	h := 0
	for _, r := range name {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	return boardAvatarPalette[h%len(boardAvatarPalette)]
}

func solidAvatarTextColor() ui.Color {
	return ui.RGB(255, 255, 255)
}

func priorityLevelAndColor(pal Palette, valText string, opt database.SelectOption) (bars int, color ui.Color) {
	low := strings.ToLower(strings.TrimSpace(valText))
	switch {
	case strings.Contains(low, "p0") || strings.Contains(low, "紧急") || strings.Contains(low, "high") || strings.Contains(low, "高"):
		return 3, StatusDotColor(pal.Dark, "red")
	case strings.Contains(low, "p1") || strings.Contains(low, "medium") || strings.Contains(low, "中"):
		return 2, StatusDotColor(pal.Dark, "yellow")
	case strings.Contains(low, "p2") || strings.Contains(low, "p3") || strings.Contains(low, "low") || strings.Contains(low, "低"):
		return 1, StatusDotColor(pal.Dark, "green")
	default:
		if opt.Color == "" || opt.Color == "default" {
			if pal.TextMuted.A != 0 {
				return 2, pal.TextMuted
			}
			return 2, StatusDotColor(pal.Dark, "gray")
		}
		return 2, StatusDotColor(pal.Dark, opt.Color)
	}
}
