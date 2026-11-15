package main

import (
	"bytes"
	"fmt"
	"html"
	"strconv"
	"strings"
	"unicode"
)

func esc(s string) string { return html.EscapeString(s) }

func pct(t, dur float64) string { return fmt.Sprintf("%.3f%%", t/dur*100) }

func rect(b *bytes.Buffer, x, y, w, h, r float64, attrs string) {
	if attrs != "" {
		attrs = " " + attrs
	}
	fmt.Fprintf(b, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="%.2f"%s/>`+"\n", x, y, w, h, r, attrs)
}

func text(b *bytes.Buffer, x, y, size float64, ink, weight, s, extra string) {
	if extra != "" {
		extra = " " + extra
	}
	fmt.Fprintf(b, `<text class="t" xml:space="preserve" x="%.2f" y="%.2f" font-size="%.2f" fill="%s" font-weight="%s"%s>%s</text>`+"\n", x, y, size, ink, weight, extra, esc(s))
}

func approxWidth(s string, size float64) float64 {
	w := 0.0
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r) || r > 127:
			w += size
		case r == ' ':
			w += size * .27
		case strings.ContainsRune("ilI.,'|:!", r):
			w += size * .26
		case strings.ContainsRune("mwMW", r):
			w += size * .78
		default:
			w += size * .52
		}
	}
	return w
}

// baseFontCSS returns the shared font classes used across all README SVGs.
func baseFontCSS() string {
	return `.t{font-family:-apple-system,BlinkMacSystemFont,"SF Pro Text","Segoe UI","PingFang SC","Hiragino Sans GB","Microsoft YaHei","Noto Sans CJK SC",Helvetica,Arial,sans-serif}` + "\n" +
		`.m{font-family:"SF Mono",ui-monospace,Menlo,Consolas,monospace}` + "\n" +
		`.math{font-family:Georgia,"Times New Roman",Times,serif}` + "\n"
}

// spacePalette defines the shell gradient, ink, and folder colors of an ArcMD Space.
type spacePalette struct {
	id         string
	top, mid   string
	bot        string
	ink        string
	muted      string
	accent     string
	folderBack string
	folderFore string
	folderLine string
}

var (
	palRose = spacePalette{
		id: "rose", top: "#f8cddc", mid: "#f7d6cf", bot: "#f1caee",
		ink: "#493844", muted: "#755968", accent: "#b03e74",
		folderBack: "#eab2c9", folderFore: "#f9dde9", folderLine: "#a3486f",
	}
	palAmber = spacePalette{
		id: "amber", top: "#f8c9b5", mid: "#f9ddb9", bot: "#f6ebb8",
		ink: "#4a3628", muted: "#785d49", accent: "#c25420",
		folderBack: "#f3bc95", folderFore: "#fce0cb", folderLine: "#b85421",
	}
	palSky = spacePalette{
		id: "sky", top: "#c6e3f4", mid: "#d2e9ee", bot: "#dce0f8",
		ink: "#2d3f52", muted: "#586e85", accent: "#2864b8",
		folderBack: "#9ec9e8", folderFore: "#d9eefb", folderLine: "#356ba6",
	}
)

type sideItem struct {
	folder bool
	glyph  string
	color  string
	title  string
	active bool
	pinned bool
}

type shellSpec struct {
	pal        spacePalette
	spaceIcon  string
	spaceName  string
	addressDoc string
	docGlyph   string
	docColor   string
	wordCount  string
	goalRatio  float64 // 0..1; >0 draws the 2px word-goal progress bar
	newDocText string
	activeFav  int // -1 for none, 0..2 for active favorite tile
	activeSw   int // 0..2 active space icon in bottom switcher
	favs       [3]struct{ glyph, color string }
	items      []sideItem
}

func defaultFavs(zh bool) [3]struct{ glyph, color string } {
	if zh {
		return [3]struct{ glyph, color string }{
			{"周", "#7048e8"},
			{"路", "#0c8599"},
			{"✦", "#c74b83"},
		}
	}
	return [3]struct{ glyph, color string }{
		{"W", "#7048e8"},
		{"R", "#0c8599"},
		{"✦", "#c74b83"},
	}
}

// writeCommonDefs emits stage/window/card clipPaths, stage radial glows, cursor,
// folder glyphs, and Space shell linearGradients.
func writeCommonDefs(b *bytes.Buffer, extraDefs string) {
	b.WriteString(`<defs>
<clipPath id="stage"><rect width="1280" height="820" rx="28"/></clipPath>
<clipPath id="win"><rect width="1240" height="760" rx="12"/></clipPath>
<clipPath id="card"><rect x="230" y="46" width="1002" height="706" rx="10"/></clipPath>
<linearGradient id="stageBg" x1="0" y1="0" x2="1" y2="1"><stop stop-color="#fff6f8"/><stop offset=".52" stop-color="#f7f6fc"/><stop offset="1" stop-color="#f2f6fd"/></linearGradient>
<radialGradient id="glowPink"><stop stop-color="#ed78a9" stop-opacity=".5"/><stop offset=".55" stop-color="#ed78a9" stop-opacity=".18"/><stop offset="1" stop-color="#ed78a9" stop-opacity="0"/></radialGradient>
<radialGradient id="glowBlue"><stop stop-color="#6ec1e4" stop-opacity=".56"/><stop offset=".55" stop-color="#6ec1e4" stop-opacity=".2"/><stop offset="1" stop-color="#6ec1e4" stop-opacity="0"/></radialGradient>
<radialGradient id="glowPurple"><stop stop-color="#9c85db" stop-opacity=".5"/><stop offset=".55" stop-color="#9c85db" stop-opacity=".18"/><stop offset="1" stop-color="#9c85db" stop-opacity="0"/></radialGradient>
<radialGradient id="glowAmber"><stop stop-color="#f59f5b" stop-opacity=".5"/><stop offset=".55" stop-color="#f59f5b" stop-opacity=".18"/><stop offset="1" stop-color="#f59f5b" stop-opacity="0"/></radialGradient>
<g id="cursor"><path d="M0 0v22l5.5-5.5 4.9 9.6 4.3-2.2-4.8-9.1H18.4z" fill="#23242b" stroke="#fff" stroke-width="1.9" stroke-linejoin="round"/></g>
`)
	for _, p := range []spacePalette{palRose, palAmber, palSky} {
		fmt.Fprintf(b, `<linearGradient id="sh-%s" x1="0" y1="0" x2="0" y2="1"><stop stop-color="%s"/><stop offset=".5" stop-color="%s"/><stop offset="1" stop-color="%s"/></linearGradient>`+"\n", p.id, p.top, p.mid, p.bot)
		fmt.Fprintf(b, `<g id="folder-%s"><path d="M1 3.5a1.5 1.5 0 0 1 1.5-1.5h3.2l1.6 1.6h6.2A1.5 1.5 0 0 1 15 5.1V12.5A1.5 1.5 0 0 1 13.5 14h-11A1.5 1.5 0 0 1 1 12.5z" fill="%s" stroke="%s" stroke-width="1.2"/><path d="M1.5 7.1h13.6l-1.1 5.7a1.4 1.4 0 0 1-1.4 1.2H2.8a1.4 1.4 0 0 1-1.4-1.2z" fill="%s" stroke="%s" stroke-width="1.2"/></g>`+"\n",
			p.id, p.folderBack, p.folderLine, p.folderFore, p.folderLine)
	}
	if extraDefs != "" {
		b.WriteString(extraDefs)
	}
	b.WriteString("</defs>\n")
}

// writeStageBackdrop opens <g clip-path="url(#stage)">, draws the pastel radial
// backdrop, and opens the scaled window wrapper with crisp multi-layer shadows.
func writeStageBackdrop(b *bytes.Buffer, warm bool) {
	b.WriteString(`<g clip-path="url(#stage)">` + "\n")
	rect(b, 0, 0, 1280, 820, 0, `fill="url(#stageBg)"`)
	if warm {
		b.WriteString(`<circle cx="140" cy="120" r="370" fill="url(#glowPink)" opacity=".42"/>` + "\n")
		b.WriteString(`<circle cx="1150" cy="150" r="390" fill="url(#glowAmber)" opacity=".34"/>` + "\n")
		b.WriteString(`<circle cx="1160" cy="690" r="410" fill="url(#glowPurple)" opacity=".28"/>` + "\n")
		b.WriteString(`<circle cx="120" cy="750" r="360" fill="url(#glowBlue)" opacity=".28"/>` + "\n")
	} else {
		b.WriteString(`<circle cx="130" cy="125" r="370" fill="url(#glowPink)" opacity=".38"/>` + "\n")
		b.WriteString(`<circle cx="1170" cy="675" r="410" fill="url(#glowPurple)" opacity=".28"/>` + "\n")
		b.WriteString(`<circle cx="110" cy="750" r="370" fill="url(#glowBlue)" opacity=".34"/>` + "\n")
	}
	b.WriteString(`<g transform="translate(70 34) scale(.919)">` + "\n")
	rect(b, -16, 8.0, 1272, 774, 28, `fill="#2c2233" opacity=".018"`)
	rect(b, -10, 5.0, 1260, 769, 22, `fill="#2c2233" opacity=".026"`)
	rect(b, -5, 2.5, 1250, 764, 17, `fill="#2c2233" opacity=".036"`)
	rect(b, -1, 0.5, 1242, 761, 13, `fill="#2c2233" opacity=".052"`)
}

// drawDocIcon renders ArcMD's automatic rounded 28% corner document favicon.
func drawDocIcon(b *bytes.Buffer, x, y, size float64, glyph, color string) {
	r := size * 0.28
	rect(b, x, y, size, size, r, fmt.Sprintf(`fill="%s" fill-opacity=".16"`, color))
	text(b, x+size/2, y+size*0.73, size*0.60, color, "600", glyph, `text-anchor="middle"`)
}

// drawLibraryGlyph renders Arc's Library box icon with slot + floating cloud, dot & triangle.
func drawLibraryGlyph(b *bytes.Buffer, x, y float64, stroke string) {
	fmt.Fprintf(b, `<g transform="translate(%.1f %.1f)" fill="none" stroke="%s" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round">`+"\n", x, y, stroke)
	b.WriteString(`<rect x="1.5" y="7.5" width="15" height="10.5" rx="2.4"/>` + "\n")
	b.WriteString(`<path d="M5.5 11.2h7"/>` + "\n")
	b.WriteString(`<circle cx="5" cy="4" r="1.2"/>` + "\n")
	b.WriteString(`<path d="M9.2 4.8h4.2a1.4 1.4 0 0 0 0-2.8 1.9 1.9 0 0 0-3.5-.4 1.3 1.3 0 0 0-.7 3.2z"/>` + "\n")
	b.WriteString(`</g>` + "\n")
}

// writeToolbarChrome renders traffic lights, nav arrows, address pill, word count +
// goal bar, T formatting, session lock, and ⋯ menu (matching current ArcMD v0.0.11+).
func writeToolbarChrome(b *bytes.Buffer, s shellSpec, shellInkAttr bool) {
	inkExtra := ""
	strokeAttr := ""
	if shellInkAttr {
		inkExtra = `data-shell-ink="true"`
		strokeAttr = ` data-shell-stroke="true"`
	}
	b.WriteString(`<circle cx="21" cy="22" r="6" fill="#ff5f57"/><circle cx="41" cy="22" r="6" fill="#febc2e"/><circle cx="61" cy="22" r="6" fill="#28c840"/>` + "\n")
	fmt.Fprintf(b, `<g opacity=".74"%s fill="none" stroke="%s" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">`+"\n", strokeAttr, s.pal.ink)
	b.WriteString(`<rect x="196" y="15" width="16" height="14" rx="3"/><path d="M201.5 15v14"/>` + "\n")
	b.WriteString(`<path d="M251 22h-12m5-5-5 5 5 5"/>` + "\n")
	b.WriteString(`<path d="M273 22h12m-5-5 5 5-5 5" opacity=".42"/>` + "\n")
	b.WriteString(`<path d="M314 18a6 6 0 1 0 1.5 5m-.2-8v4h-4"/>` + "\n")
	// Right-side T (format), Lock (read-only toggle), and ⋯ menu.
	b.WriteString(`<path d="M1134 16h10m-5 0v12"/>` + "\n")
	b.WriteString(`<rect x="1163" y="20.5" width="11" height="8.5" rx="2"/><path d="M1165.5 20.5v-2.5a3 3 0 0 1 6 0v2.5"/>` + "\n")
	b.WriteString(`</g>` + "\n")
	fmt.Fprintf(b, `<g fill="%s" opacity=".74" %s><circle cx="1197" cy="22" r="1.5"/><circle cx="1203" cy="22" r="1.5"/><circle cx="1209" cy="22" r="1.5"/></g>`+"\n", s.pal.ink, inkExtra)

	// Address pill.
	rect(b, 336, 7, 694, 31, 9, `fill="#fff" fill-opacity=".52"`)
	drawDocIcon(b, 348, 14.5, 16, s.docGlyph, s.docColor)
	parts := strings.SplitN(s.addressDoc, "/", 2)
	if len(parts) == 2 {
		fmt.Fprintf(b, `<text x="372" y="27.2" class="t" font-size="13"><tspan fill="%s">%s/</tspan><tspan fill="%s" font-weight="500">%s</tspan></text>`+"\n",
			s.pal.muted, esc(parts[0]), s.pal.ink, esc(parts[1]))
	} else {
		text(b, 372, 27.2, 13, s.pal.ink, "500", s.addressDoc, "")
	}

	// Word count & optional 2px word-goal progress bar.
	text(b, 1112, 26.8, 12, s.pal.muted, "500", s.wordCount, strings.TrimSpace(`text-anchor="end" `+inkExtra))
	if s.goalRatio > 0 {
		rect(b, 1064, 32, 48, 2, 1, fmt.Sprintf(`fill="%s" opacity=".18"`, s.pal.ink))
		rect(b, 1064, 32, 48*s.goalRatio, 2, 1, fmt.Sprintf(`fill="%s" opacity=".82"`, s.pal.accent))
	}
}

// writeSidebarContent renders Favorites grid, Space header, Pinned + separator +
// Today items, rounded dashed "+ New document" slot, and bottom Space switcher.
func writeSidebarContent(b *bytes.Buffer, s shellSpec, shellInkAttr bool) {
	inkExtra := ""
	if shellInkAttr {
		inkExtra = `data-shell-ink="true"`
	}
	// 3 Favorites tiles.
	for i := 0; i < 3; i++ {
		fx := 10.0 + float64(i)*72.5
		if i == s.activeFav {
			rect(b, fx, 50, 65, 47, 13, `fill="#fff" fill-opacity=".42" stroke="#000" stroke-opacity=".17" stroke-width="1.8"`)
		} else {
			rect(b, fx, 50, 65, 47, 13, `fill="#000" fill-opacity=".048"`)
		}
		drawDocIcon(b, fx+22.5, 63.5, 20, s.favs[i].glyph, s.favs[i].color)
	}

	// Space header row.
	text(b, 19, 134, 15, s.pal.accent, "600", s.spaceIcon, "")
	text(b, 44, 134, 13, s.pal.muted, "600", s.spaceName, inkExtra)
	text(b, 205, 134, 13, s.pal.muted, "600", "•••", `text-anchor="middle" opacity=".75"`)

	// Pinned & Today items with hairline separator between them.
	y := 154.0
	drewSep := false
	hasPinned := false
	for _, it := range s.items {
		if it.pinned {
			hasPinned = true
		}
	}
	for _, it := range s.items {
		if hasPinned && !it.pinned && !drewSep {
			fmt.Fprintf(b, `<path d="M18 %.1fh194" stroke="%s" stroke-opacity=".14" stroke-width="1" stroke-linecap="round"/>`+"\n", y+9, s.pal.ink)
			y += 19
			drewSep = true
		}
		if it.active {
			rect(b, 8, y, 214, 36, 10, `fill="#fff" fill-opacity=".76" stroke="#000" stroke-opacity=".045"`)
		}
		if it.folder {
			fmt.Fprintf(b, `<use href="#folder-%s" x="18" y="%.1f"/>`+"\n", s.pal.id, y+10)
			text(b, 44, y+23.5, 14, s.pal.ink, "500", it.title, inkExtra)
		} else {
			drawDocIcon(b, 18, y+10, 16, it.glyph, it.color)
			w := "400"
			if it.active {
				w = "500"
			}
			extra := inkExtra
			if it.active {
				extra = ""
			}
			text(b, 44, y+23.5, 14, s.pal.ink, w, it.title, extra)
		}
		y += 40
	}

	// Arc's rounded 10px dashed "+ 新建文档" / "+ New document" slot.
	y += 6
	rect(b, 10, y, 210, 36, 10, fmt.Sprintf(`fill="#fff" fill-opacity=".12" stroke="%s" stroke-opacity=".24" stroke-width="1" stroke-dasharray="4 3.5"`, s.pal.ink))
	text(b, 115, y+23, 13, s.pal.muted, "500", "+  "+s.newDocText, strings.TrimSpace(`text-anchor="middle" `+inkExtra))

	// Bottom Space switcher bar: Library glyph + 3 Space icons + "+".
	drawLibraryGlyph(b, 16, 720, s.pal.muted)
	icons := [3]string{"✦", "◈", "🌿"}
	xs := [3]float64{86, 118, 150}
	for i := 0; i < 3; i++ {
		if i == s.activeSw {
			text(b, xs[i], 736, 17, s.pal.accent, "700", icons[i], `text-anchor="middle"`)
		} else {
			text(b, xs[i], 735, 14.5, s.pal.muted, "500", icons[i], `text-anchor="middle" opacity=".52"`)
		}
	}
	text(b, 206, 737, 19, s.pal.muted, "400", "+", `text-anchor="middle" opacity=".75"`)
}

// writeCaptionBar renders the numbered step pills below the window at y=762.
func writeCaptionBar(b *bytes.Buffer, animClassPrefix string, labels []string, xs []float64) {
	for i, label := range labels {
		x := xs[i]
		fmt.Fprintf(b, `<g class="%s cap%d">`+"\n", animClassPrefix, i)
		rect(b, x, 761, 22, 22, 6.5, `fill="#b96e9c" fill-opacity=".15"`)
		text(b, x+11, 776.5, 12, "#6b4161", "600", strconv.Itoa(i+1), `text-anchor="middle"`)
		text(b, x+30, 777, 13.5, "#4c3948", "500", label, "")
		b.WriteString("</g>\n")
	}
}

// defaultRoseSpec builds the standard Personal (Rose) shellSpec for single-space demos.
func defaultRoseSpec(zh bool, docTitle, docGlyph, docColor, wordCount string) shellSpec {
	if zh {
		return shellSpec{
			pal:        palRose,
			spaceIcon:  "✦",
			spaceName:  "Personal",
			addressDoc: "Personal / " + docTitle + ".md",
			docGlyph:   docGlyph,
			docColor:   docColor,
			wordCount:  wordCount,
			goalRatio:  0.72,
			newDocText: "新建文档",
			activeFav:  -1,
			activeSw:   0,
			favs:       defaultFavs(true),
			items: []sideItem{
				{folder: true, title: "日记", pinned: true},
				{glyph: "今", color: "#0c8599", title: "今日笔记", pinned: true},
				{folder: true, title: "灵感"},
				{glyph: docGlyph, color: docColor, title: docTitle, active: true},
				{glyph: "摘", color: "#7048e8", title: "摘抄"},
				{glyph: "书", color: "#d6336c", title: "书单"},
			},
		}
	}
	return shellSpec{
		pal:        palRose,
		spaceIcon:  "✦",
		spaceName:  "Personal",
		addressDoc: "Personal / " + docTitle + ".md",
		docGlyph:   docGlyph,
		docColor:   docColor,
		wordCount:  wordCount,
		goalRatio:  0.72,
		newDocText: "New document",
		activeFav:  -1,
		activeSw:   0,
		favs:       defaultFavs(false),
		items: []sideItem{
			{folder: true, title: "Journal", pinned: true},
			{glyph: "T", color: "#0c8599", title: "Today's note", pinned: true},
			{folder: true, title: "Ideas"},
			{glyph: docGlyph, color: docColor, title: docTitle, active: true},
			{glyph: "Q", color: "#7048e8", title: "Quotes"},
			{glyph: "B", color: "#d6336c", title: "Book list"},
		},
	}
}
