package editor

import (
	"maps"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/ui"
)

// Page holds the colors of the page beyond ui.Theme. The page's look and
// metrics follow a Feishu-style theme. ArcMD's app passes them from internal/app/theme.go; a zero Page is derived from the
// ui.Theme, so tests and other hosts need none.
type Page struct {
	// Marker colors list bullets, numbers and a done task's check.
	Marker ui.Color
	// Caption is secondary text: quotes, line numbers, labels.
	Caption ui.Color
	// Placeholder is the hint of an empty block.
	Placeholder ui.Color
	// TaskBorder outlines an open task's box; TaskDone is a done task's text.
	TaskBorder, TaskDone ui.Color
	QuoteBar             ui.Color
	Rule                 ui.Color
	// Code blocks: fill, outline, text, and the outline under the pointer.
	CodeFill, CodeBorder, CodeText, CodeHover ui.Color
	// Inline code.
	InlineFill, InlineText ui.Color
	TableLine, TableHeader ui.Color
	// Hover is the fill of a small control under the pointer; TurnActive
	// marks a menu's current choice.
	Hover, TurnActive ui.Color
	// Table bars: at rest, under the pointer, selected; the selected
	// line's cells; and the red of a delete about to happen.
	BarFill, BarHover, BarSelected, CellSelected ui.Color
	Danger, DangerCell                           ui.Color
	// FindMatch marks a find match; FindCurrent the current one.
	FindMatch, FindCurrent ui.Color
	// OnMarker is drawn on Marker, as a pin's plus.
	OnMarker ui.Color
	// Popover is the fill of menus and floating bars.
	Popover ui.Color
	// Tints color the slash menu's row icons by block family: blue,
	// indigo, green, orange, teal, yellow, wathet.
	Tints map[string]ui.Color
	// TextColors and Highlights show the names of document.TextColors and
	// document.TextHighlights.
	TextColors, Highlights map[string]ui.Color
	// CellFills show the names of a cell's Background.
	CellFills map[string]ui.Color
	// TagFill is the fill behind a #tag.
	TagFill ui.Color
	// Scrim is the fill behind words over a picture, as the cover's hint
	// while it is repositioned, and OnScrim their color.
	Scrim, OnScrim ui.Color
	ImageShadow    ui.Color
	// MathBorder outlines formula blocks; MathShadow belongs to the source panel.
	MathBorder, MathShadow ui.Color
	// Covers are the two colors, top-left to bottom-right, of each of
	// document.CoverGradients.
	Covers     map[string][2]ui.Color
	Tokens     CodeTokens
	Callouts   map[string]Callout
	Whiteboard WhiteboardStyle

	// Typography, table alignment and custom color tokens from style.json.
	BoldWeight        int
	HeadingWeight     int
	TableAlign        string
	TableHeaderAlign  string
	TableHeaderWeight int
	HeadingText       ui.Color
	BoldText          ui.Color
	TableHeaderText   ui.Color
	TableStripe       ui.Color
	HighlightDefault  ui.Color
	InlineTexFill     ui.Color
	TexBlockFill      ui.Color
}

// WhiteboardStyle holds the Excalidraw-inspired light/dark adaptive design
// tokens for standalone and embedded hand-drawn whiteboards.
type WhiteboardStyle struct {
	Canvas       ui.Color
	Grid         ui.Color
	IslandFill   ui.Color
	IslandBorder ui.Color
	IslandShadow ui.Color
	ActiveFill   ui.Color
	ActiveInk    ui.Color
	Selection    ui.Color
	HandleFill   ui.Color
	Guide        ui.Color
	NoteFill     ui.Color
	NoteBorder   ui.Color
	FrameStroke  ui.Color
	FrameLabel   ui.Color
	Strokes      map[string]ui.Color
	Fills        map[string]ui.Color
}

// CodeTokens color the tokens of a code block.
type CodeTokens struct {
	Comment, Keyword, String, Number, Type, Function ui.Color
}

// Callout is the look of a callout's tone.
type Callout struct {
	Fill, Border, Icon ui.Color
}

// pageOf fills a Page from the theme where the host left it empty.
func pageOf(p Page, t *ui.Theme) Page {
	zero := ui.Color{}
	set := func(c *ui.Color, v ui.Color) {
		if *c == zero {
			*c = v
		}
	}
	set(&p.Marker, t.Accent)
	set(&p.Caption, t.TextMuted)
	set(&p.Placeholder, t.TextMuted.Alpha(.7))
	set(&p.TaskBorder, t.Text.Alpha(.35))
	set(&p.TaskDone, t.TextMuted)
	set(&p.QuoteBar, t.Border)
	set(&p.Rule, t.Border)
	set(&p.CodeFill, t.Surface)
	set(&p.CodeBorder, t.Border)
	set(&p.CodeText, t.Text)
	set(&p.CodeHover, t.Accent)
	set(&p.InlineFill, t.Text.Alpha(.06))
	set(&p.InlineText, t.Text)
	set(&p.TableLine, t.Border)
	set(&p.TableHeader, t.Surface)
	set(&p.Hover, t.Text.Alpha(.06))
	set(&p.ImageShadow, t.Text.Alpha(.08))
	set(&p.MathBorder, t.Border)
	set(&p.MathShadow, t.Text.Alpha(.12))
	set(&p.TurnActive, t.Accent.Alpha(.14))
	set(&p.Popover, t.Background)
	set(&p.BarFill, t.Text.Alpha(.06))
	set(&p.BarHover, t.Text.Alpha(.14))
	set(&p.BarSelected, t.Accent.Alpha(.35))
	set(&p.CellSelected, t.Accent.Alpha(.1))
	set(&p.Danger, t.Danger)
	set(&p.DangerCell, t.Danger.Alpha(.08))
	set(&p.OnMarker, t.Background)
	set(&p.FindMatch, t.Warning.Alpha(.25))
	set(&p.FindCurrent, t.Warning.Alpha(.6))
	set(&p.TagFill, t.Accent.Alpha(.1))
	set(&p.Scrim, ui.RGBA(0, 0, 0, .45))
	set(&p.OnScrim, ui.RGBA(255, 255, 255, 1))
	if p.Covers == nil {
		p.Covers = map[string][2]ui.Color{}
		for i, name := range document.CoverGradients {
			p.Covers[name] = [2]ui.Color{t.Accent.Alpha(.2 + .1*float32(i%3)), t.Accent.Alpha(.5)}
		}
	}
	set(&p.Tokens.Comment, t.TextMuted)
	set(&p.Tokens.Keyword, t.Accent)
	set(&p.Tokens.String, t.Success)
	set(&p.Tokens.Number, t.Warning)
	set(&p.Tokens.Type, t.Accent)
	set(&p.Tokens.Function, t.Text)
	if p.TextColors == nil {
		p.TextColors = map[string]ui.Color{"gray": t.TextMuted, "red": t.Danger, "orange": t.Warning, "yellow": t.Warning, "green": t.Success, "blue": t.Accent, "purple": t.Accent}
	}
	if p.Highlights == nil {
		p.Highlights = map[string]ui.Color{}
		for name, c := range p.TextColors {
			p.Highlights["light-"+name] = c.Alpha(.15)
			p.Highlights[name] = c.Alpha(.35)
		}
		p.Highlights["medium-gray"] = t.TextMuted.Alpha(.25)
		if p.HighlightDefault != zero {
			p.Highlights["light-yellow"] = p.HighlightDefault
		}
	} else if p.HighlightDefault != zero {
		if _, ok := p.Highlights["light-yellow"]; !ok {
			p.Highlights["light-yellow"] = p.HighlightDefault
		}
	}
	if p.CellFills == nil {
		p.CellFills = map[string]ui.Color{}
		for name, c := range p.Highlights {
			p.CellFills[name] = c.Alpha(.5)
		}
	}
	if p.Callouts == nil {
		p.Callouts = map[string]Callout{}
	}
	for _, tone := range []string{"", "green", "purple", "yellow", "red"} {
		if _, ok := p.Callouts[tone]; !ok {
			hue := map[string]ui.Color{"": t.Accent, "green": t.Success, "purple": t.Accent, "yellow": t.Warning, "red": t.Danger}[tone]
			p.Callouts[tone] = Callout{Fill: hue.Alpha(.08), Border: hue.Alpha(.25), Icon: hue}
		}
	}
	wb := &p.Whiteboard
	set(&wb.Canvas, t.Background)
	set(&wb.Grid, t.Text.Alpha(.09))
	set(&wb.IslandFill, p.Popover)
	set(&wb.IslandBorder, p.MathBorder)
	set(&wb.IslandShadow, p.MathShadow)
	set(&wb.ActiveFill, t.Accent.Alpha(.14))
	set(&wb.ActiveInk, t.Accent)
	set(&wb.Selection, t.Accent)
	set(&wb.HandleFill, t.Background)
	set(&wb.Guide, t.Danger)
	set(&wb.NoteFill, p.Callouts["yellow"].Fill)
	set(&wb.NoteBorder, p.Callouts["yellow"].Border)
	set(&wb.FrameStroke, t.Border)
	set(&wb.FrameLabel, p.Caption)
	if wb.Strokes == nil {
		wb.Strokes = map[string]ui.Color{
			"ink":    t.Text,
			"gray":   t.TextMuted,
			"red":    p.TextColors["red"],
			"orange": p.TextColors["orange"],
			"green":  p.TextColors["green"],
			"blue":   p.TextColors["blue"],
			"purple": p.TextColors["purple"],
		}
	}
	if wb.Fills == nil {
		wb.Fills = map[string]ui.Color{
			"ink":    t.Text.Alpha(.14),
			"gray":   t.TextMuted.Alpha(.22),
			"red":    p.Highlights["light-red"],
			"orange": p.Highlights["light-orange"],
			"green":  p.Highlights["light-green"],
			"blue":   p.Highlights["light-blue"],
			"purple": p.Highlights["light-purple"],
		}
	}
	return p
}

func samePage(a, b Page) bool {
	return a.Marker == b.Marker &&
		a.Caption == b.Caption &&
		a.Placeholder == b.Placeholder &&
		a.TaskBorder == b.TaskBorder &&
		a.TaskDone == b.TaskDone &&
		a.QuoteBar == b.QuoteBar &&
		a.Rule == b.Rule &&
		a.CodeFill == b.CodeFill &&
		a.CodeBorder == b.CodeBorder &&
		a.CodeText == b.CodeText &&
		a.CodeHover == b.CodeHover &&
		a.InlineFill == b.InlineFill &&
		a.InlineText == b.InlineText &&
		a.TableLine == b.TableLine &&
		a.TableHeader == b.TableHeader &&
		a.Hover == b.Hover &&
		a.TurnActive == b.TurnActive &&
		a.BarFill == b.BarFill &&
		a.BarHover == b.BarHover &&
		a.BarSelected == b.BarSelected &&
		a.CellSelected == b.CellSelected &&
		a.Danger == b.Danger &&
		a.DangerCell == b.DangerCell &&
		a.FindMatch == b.FindMatch &&
		a.FindCurrent == b.FindCurrent &&
		a.OnMarker == b.OnMarker &&
		a.Popover == b.Popover &&
		a.TagFill == b.TagFill &&
		a.Scrim == b.Scrim &&
		a.OnScrim == b.OnScrim &&
		a.ImageShadow == b.ImageShadow &&
		a.MathBorder == b.MathBorder &&
		a.MathShadow == b.MathShadow &&
		a.InlineTexFill == b.InlineTexFill &&
		a.TexBlockFill == b.TexBlockFill &&
		a.BoldWeight == b.BoldWeight &&
		a.HeadingWeight == b.HeadingWeight &&
		a.TableAlign == b.TableAlign &&
		a.TableHeaderAlign == b.TableHeaderAlign &&
		a.TableHeaderWeight == b.TableHeaderWeight &&
		a.HeadingText == b.HeadingText &&
		a.BoldText == b.BoldText &&
		a.TableHeaderText == b.TableHeaderText &&
		a.TableStripe == b.TableStripe &&
		a.HighlightDefault == b.HighlightDefault &&
		a.Tokens == b.Tokens &&
		maps.Equal(a.Tints, b.Tints) &&
		maps.Equal(a.TextColors, b.TextColors) &&
		maps.Equal(a.Highlights, b.Highlights) &&
		maps.Equal(a.CellFills, b.CellFills) &&
		maps.Equal(a.Covers, b.Covers) &&
		maps.Equal(a.Callouts, b.Callouts) &&
		sameWhiteboardStyle(a.Whiteboard, b.Whiteboard)
}

func sameWhiteboardStyle(a, b WhiteboardStyle) bool {
	return a.Canvas == b.Canvas &&
		a.Grid == b.Grid &&
		a.IslandFill == b.IslandFill &&
		a.IslandBorder == b.IslandBorder &&
		a.IslandShadow == b.IslandShadow &&
		a.ActiveFill == b.ActiveFill &&
		a.ActiveInk == b.ActiveInk &&
		a.Selection == b.Selection &&
		a.HandleFill == b.HandleFill &&
		a.Guide == b.Guide &&
		a.NoteFill == b.NoteFill &&
		a.NoteBorder == b.NoteBorder &&
		a.FrameStroke == b.FrameStroke &&
		a.FrameLabel == b.FrameLabel &&
		maps.Equal(a.Strokes, b.Strokes) &&
		maps.Equal(a.Fills, b.Fills)
}

func clonePage(p Page) Page {
	p.Tints = maps.Clone(p.Tints)
	p.TextColors = maps.Clone(p.TextColors)
	p.Highlights = maps.Clone(p.Highlights)
	p.CellFills = maps.Clone(p.CellFills)
	p.Covers = maps.Clone(p.Covers)
	p.Callouts = maps.Clone(p.Callouts)
	p.Whiteboard.Strokes = maps.Clone(p.Whiteboard.Strokes)
	p.Whiteboard.Fills = maps.Clone(p.Whiteboard.Fills)
	return p
}

func (e *Editor) boldWeight() int {
	if e.page.BoldWeight > 0 {
		return e.page.BoldWeight
	}
	return 600
}

func (e *Editor) headingWeight() int {
	if e.page.HeadingWeight > 0 {
		return e.page.HeadingWeight
	}
	return 500
}

func (e *Editor) tableHeaderWeight() int {
	if e.page.TableHeaderWeight > 0 {
		return e.page.TableHeaderWeight
	}
	return 500
}

func (e *Editor) effectiveBlockAlign(b *document.Block, header bool) string {
	if b.Align != "" {
		return b.Align
	}
	if b.Kind == "cell" {
		if header && e.page.TableHeaderAlign != "" {
			return e.page.TableHeaderAlign
		}
		if e.page.TableAlign != "" {
			return e.page.TableAlign
		}
	}
	return ""
}
