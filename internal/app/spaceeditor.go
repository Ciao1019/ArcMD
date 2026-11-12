package app

import (
	"fmt"
	"image"
	"math"
	"slices"
	"time"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo/ui"
)

// The space's theme card floats beside the sidebar while a space is made
// or edited: Arc's theme editor as Cra (MIT) measured it, a 355-wide card
// painted on softly tinted glass, a dotted square pad of 340 with Auto /
// Light / Dark at its top and − / + (one to three colors) at its foot,
// nine preset swatches between paging arrows, a wave for the intensity and
// a dial for the grain. Every change goes into the form's theme at once,
// which the window previews.

const (
	editorCardW = 355
	padSize     = 340
	// padReach is the distance from the pad's center at full saturation.
	padReach = 153
	// knobMain and knobOther are the diameters of the first color's knob
	// and the others'.
	knobMain, knobOther = 44, 27
)

// padDots is the pad's dotted face: an opaque white dot every 4 DIPs,
// drawn at twice the size for sharp dots on a Retina screen and tinted
// with the card's ink when painted.
var padDots = func() *ui.Bitmap {
	const n = padSize * 2
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := 8; y < n; y += 8 {
		for x := 8; x < n; x += 8 {
			for _, d := range [4][2]int{{-1, -1}, {0, -1}, {-1, 0}, {0, 0}} {
				i := img.PixOffset(x+d[0], y+d[1])
				img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 255, 255, 255, 255
			}
		}
	}
	return ui.NewBitmap(img)
}()

// knobAt is where a key color sits on the pad, from its center.
func knobAt(hex string) (x, y float32) {
	h, s, _ := space.HSB(space.Components(hex))
	a := h * math.Pi / 180
	return float32(math.Cos(a) * s * padReach), float32(math.Sin(a) * s * padReach)
}

// placeKnob puts key color i under the point (from the pad's center); the
// others keep their places round the wheel from it.
func placeKnob(th *space.Theme, i int, x, y float32) {
	angle := math.Atan2(float64(y), float64(x))*180/math.Pi - space.WheelOffset(i)
	sat := min(1, math.Hypot(float64(x), float64(y))/padReach)
	for j := range th.Colors {
		th.Colors[j] = space.WheelColor(angle+space.WheelOffset(j), sat)
	}
}

// addKey adds a color round the wheel from the first, up to three.
func addKey(th *space.Theme) {
	if len(th.Colors) == 0 || len(th.Colors) >= 3 {
		return
	}
	h, s, _ := space.HSB(space.Components(th.Colors[0]))
	th.Colors = append(th.Colors, space.WheelColor(h+space.WheelOffset(len(th.Colors)), s))
}

// knobUnder returns the key color whose knob is under the point, from the
// wheel's center, the ones drawn on top first; -1 for none.
func knobUnder(colors []string, x, y float32) int {
	for i := len(colors) - 1; i >= 0; i-- {
		kx, ky := knobAt(colors[i])
		r := float32(knobOther) / 2
		if i == 0 {
			r = knobMain / 2
		}
		if math.Hypot(float64(x-kx), float64(y-ky)) <= float64(r+2) {
			return i
		}
	}
	return -1
}

// themeCard is the card editing f.theme, beside anchor (the sidebar's
// header). Closing it commits an existing space; a new draft keeps its
// preview until the space is created or canceled.
func (a *App) themeCard(c *ui.Context, f *spaceForm, anchor ui.Element) {
	fill, ink, rim := spaceEditorSurface(f.theme, false)
	_, height := c.Size()
	size := min(float32(padSize), max(220, height-140))
	ui.Overlay(c, func() {
		pop := ui.PopoverBase(c.Key("theme-card"), anchor, &f.cardOpen, func(card ui.Element) {
			if f.id == "" {
				// A new space's card stands at the middle of the draft
				// column, over the content, as Arc's does.
				card.AttachTo(anchor, ui.AnchorRight, ui.AnchorLeft).MarginStart(26)
			} else {
				card.AttachTo(anchor, ui.AnchorTopRight, ui.AnchorTopLeft).MarginStart(16)
			}
			card.
				Width(editorCardW).Radius(22).Clip().Padding(9, 0, 14, 0).Gap(14).AlignItems(ui.Center).
				Shadow(0, 8, 24, 0, black.Alpha(0.20)).Border(0.5, rim).
				Role(ui.RoleDialog).Label(i18n.T("空间主题"))
			a.tourMark("theme-card", card)
			card.Draw(func(p *ui.Painter, r ui.Rect) {
				p.FillGradient(r, fill, 22)
			})
			card.Children(func() {
				a.colorPad(c, f, ink, size)
				swatches(c, f, ink)
				ui.Row(c).Width(editorCardW).Padding(0, 20, 0, 14).Gap(12).AlignItems(ui.Center).Children(func() {
					waveSlider(c, &f.theme.Intensity, ink)
					grainDial(c, &f.theme.Noise, ink)
				})
			})
		})
		if pop.Valid() && pop.Dismissed() && f.iconsOpen {
			// The press went to the icon picker, which is part of the form.
			f.cardOpen = true
		}
		if a.tour.themeHeld {
			f.cardOpen = true // the tour's, which keeps nothing of it
		} else if f.id != "" && !f.cardOpen {
			a.submitSpace()
		}
	})
}

// appearanceSwitch is the segmented control of Auto / Light / Dark.
func appearanceSwitch(c *ui.Context, th *space.Theme, ink ui.Color) ui.Element {
	modes := []struct {
		value, label string
		icon         *ui.SVG
	}{
		{space.AppearanceAuto, i18n.T("跟随系统"), iconSparkles},
		{space.AppearanceLight, i18n.T("浅色"), iconSun},
		{space.AppearanceDark, i18n.T("深色"), iconMoon},
	}
	return ui.Row(c.Key("appearance")).Padding(3).Gap(2).Radius(10).Background(ink.Alpha(0.08)).
		Role(ui.RoleRadioGroup).Label(i18n.T("外观")).FocusGroup(ui.Horizontal).Children(func() {
		for _, m := range modes {
			on := th.Appearance == m.value
			b := ui.Box(c.Key("mode-"+m.value)).Size(40, 26).Radius(7).Center().Cursor(ui.CursorPointer).
				Focusable().Role(ui.RoleRadio).Label(m.label).Tooltip(m.label).Checked(on)
			alpha := float32(0.55)
			switch {
			case on:
				b.Background(white.Alpha(0.85)).Shadow(0, 1, 2, 0, black.Alpha(0.12))
				alpha = 1
			case b.Hovered():
				b.Background(ink.Alpha(0.06))
				alpha = 0.85
			}
			color := ink.Alpha(alpha)
			if on {
				color = ui.RGB(32, 32, 36)
			}
			b.Children(func() { ui.Icon(c, m.icon).FontSize(14).TextColor(color) })
			if b.Clicked() {
				th.Appearance = m.value
			}
		}
	})
}

// colorPad is the dotted pad with the key colors' knobs, Auto / Light /
// Dark at its top and − / + at its foot. Pressing on it brings the knob
// there, or the first color's when none is there, and the knob follows the
// drag; the arrows turn the hue and the saturation.
func (a *App) colorPad(c *ui.Context, f *spaceForm, ink ui.Color, size float32) {
	th := &f.theme
	half, scale := size/2, size/padSize
	ui.Box(c.Key("pad")).Size(size, size).Shrink(0).Children(func() {
		pad := ui.Box(c.Key("wheel")).Absolute().Left(0).Top(0).Size(size, size).Radius(12).
			Focusable().Role(ui.RoleSlider).Label(i18n.T("空间颜色")).
			Value(fmt.Sprint(th.Colors)).Tooltip(i18n.T("拖动选择颜色；方向键调整色相与饱和度"))
		pad.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(r, ink.Alpha(0.06), 12)
			bw, bh := padDots.Size()
			p.Clip(r, 12, func() {
				p.ImageRegion(padDots, ui.Rect{W: float32(bw), H: float32(bh)}, r, ui.ImageOptions{Tint: ink.Alpha(0.2)})
			})
		})
		px, py, over := pad.PointerPosition()
		px, py = (px-half)/scale, (py-half)/scale
		if _, _, held := pad.Dragged(); held {
			if f.knob < 0 {
				f.knob = max(0, knobUnder(th.Colors, px, py))
			}
			placeKnob(th, f.knob, px, py)
		} else {
			f.knob = -1
		}
		hovered := -1
		if over {
			hovered = knobUnder(th.Colors, px, py)
		}
		switch {
		case f.knob >= 0:
			pad.Cursor(ui.CursorGrabbing)
		case hovered >= 0:
			pad.Cursor(ui.CursorGrab)
		default:
			pad.Cursor(ui.CursorPointer)
		}
		turn := func(dh, ds float64) {
			h, s, _ := space.HSB(space.Components(th.Colors[0]))
			s = min(1, max(0, s+ds))
			a := (h + dh) * math.Pi / 180
			placeKnob(th, 0, float32(math.Cos(a)*s*padReach), float32(math.Sin(a)*s*padReach))
		}
		switch {
		case pad.Shortcut(0, ui.KeyRight):
			turn(5, 0)
		case pad.Shortcut(0, ui.KeyLeft):
			turn(-5, 0)
		case pad.Shortcut(0, ui.KeyUp):
			turn(0, 0.05)
		case pad.Shortcut(0, ui.KeyDown):
			turn(0, -0.05)
		}

		for i, hex := range th.Colors {
			d, ring := float32(knobOther), float32(2.5)
			if i == 0 {
				d, ring = knobMain, 3.5
			}
			k := ui.Box(c.Key(fmt.Sprintf("knob%d", i))).Absolute().PassThrough()
			d *= scale * k.Animate("scale", knobScale(f.knob == i, hovered == i && f.knob < 0), 120*time.Millisecond)
			x, y := knobAt(hex)
			k.Left(half+x*scale-d/2).Top(half+y*scale-d/2).Size(d, d).Radius(d/2).Background(ui.Hex(hex)).
				Border(ring, white).Shadow(0, 2, 6, 0, black.Alpha(0.28))
		}

		appearanceSwitch(c, th, ink).Absolute().Left(half - 66).Top(16)
		ui.Row(c.Key("keys")).Absolute().Left(half - 29).Top(size - 42).Gap(6).Children(func() {
			if editorButton(c, "less", iconMinus, i18n.T("减少颜色"), 26, ink, false, len(th.Colors) > 1).Clicked() {
				th.Colors = th.Colors[:len(th.Colors)-1]
			}
			if editorButton(c, "more", iconPlus, i18n.T("增加颜色"), 26, ink, false, len(th.Colors) < 3).Clicked() {
				addKey(th)
			}
		})
	})
}

// knobScale grows a knob a little under the pointer and shrinks it while
// it is held, as Arc's.
func knobScale(held, hovered bool) float32 {
	switch {
	case held:
		return 0.92
	case hovered:
		return 1.08
	}
	return 1
}

// editorButton is a glyph in the card's ink: brighter under the pointer,
// on a faint tile when on, faint when it cannot be pressed.
func editorButton(c *ui.Context, key string, icon *ui.SVG, label string, size float32, ink ui.Color, on, enabled bool) ui.Element {
	b := ui.Box(c.Key(key)).Size(size, size).Radius(size / 2).Center().Role(ui.RoleButton).Label(label).Tooltip(label).
		Disabled(!enabled)
	alpha := float32(0.22)
	if enabled {
		b.Focusable().Cursor(ui.CursorPointer)
		alpha = 0.7
		if on {
			alpha = 0.9
		}
		if b.Hovered() {
			alpha = 1
			b.Background(ink.Alpha(0.08))
		}
	}
	if on {
		b.Background(ink.Alpha(0.12))
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(14).TextColor(ink.Alpha(alpha)) })
	return b
}

// swatches is the page of preset colors between its arrows.
func swatches(c *ui.Context, f *spaceForm, ink ui.Color) {
	page := space.PresetPages[f.page]
	ui.Row(c.Key("swatches")).AlignItems(ui.Center).Gap(2).Children(func() {
		if editorButton(c, "prev", iconChevronLeft, i18n.T("上一页预设颜色"), 24, ink, false, f.page > 0).Clicked() {
			f.page--
		}
		ui.Row(c).Gap(6).Children(func() {
			for i, colors := range page.Swatches {
				sw := ui.Box(c.Key(fmt.Sprintf("swatch%d", i))).Size(20, 20).Radius(10).Cursor(ui.CursorPointer).
					Focusable().Role(ui.RoleButton).Label(fmt.Sprintf(i18n.T("将空间颜色设为 %s %d"), page.Name, i+1))
				to := colors[len(colors)-1]
				sw.LinearGradient(ui.LinearGradient{From: ui.Hex(colors[0]), To: ui.Hex(to), Angle: 135})
				s := sw.Animate("scale", knobScale(sw.Pressed(), sw.Hovered()), 120*time.Millisecond)
				sw.Scale(s, s)
				if slices.Equal(colors, f.theme.Colors) {
					sw.Border(2, white)
					sw.Shadow(0, 0, 0, 1.5, ink.Alpha(0.35))
				} else {
					sw.Border(1, ink.Alpha(0.12))
				}
				if sw.Clicked() {
					f.theme.Colors = slices.Clone(colors)
				}
			}
		})
		if editorButton(c, "next", iconChevronRight, i18n.T("下一页预设颜色"), 24, ink, false, f.page < len(space.PresetPages)-1).Clicked() {
			f.page++
		}
	})
}

// waveSlider sets the intensity: a wave over a faint track that grows with
// the value, solid up to the thumb and faint after it.
func waveSlider(c *ui.Context, value *float64, ink ui.Color) {
	s := ui.SliderBase(c.Key("intensity"), value, 0, 1).Height(40).Grow(1).PaddingX(6).Label(i18n.T("强度")).FocusRing(false).
		Tooltip(i18n.T("强度"))
	v := float32(*value)
	focused := s.FocusVisible()
	s.Draw(func(p *ui.Painter, r ui.Rect) {
		w, h := r.W, r.H
		x := r.X + 6 + (w-12)*v
		p.Fill(ui.Rect{X: r.X, Y: r.Y + h/2 - 6, W: w, H: 12}, ink.Alpha(0.08), 6)
		amp := 2 + (h/2-9)*v
		var wave ui.Path
		for px := float32(6); px <= w-6; px++ {
			y := r.Y + h/2 - amp*float32(math.Sin(float64((px-6)/(w-12))*math.Pi*12))
			if px == 6 {
				wave.MoveTo(r.X+px, y)
			} else {
				wave.LineTo(r.X+px, y)
			}
		}
		p.StrokePath(&wave, 3, ink.Alpha(0.2))
		p.Clip(ui.Rect{X: r.X, Y: r.Y, W: x - r.X, H: h}, 0, func() { p.StrokePath(&wave, 3, ink.Alpha(0.8)) })
		thumb := ui.Rect{X: x - 6, Y: r.Y + h/2 - 16, W: 12, H: 32}
		p.Fill(thumb, white, 6)
		p.Stroke(thumb, black.Alpha(0.12), 6, 1)
		if focused {
			p.FocusRing(thumb, [4]float32{6, 6, 6, 6})
		}
	})
}

// grainDial sets the grain: a disc showing it in a ring of dots lit from
// twelve o'clock round to the knob; dragging round it turns the knob.
func grainDial(c *ui.Context, value *float64, ink ui.Color) {
	const (
		n    = 20
		size = 50
		half = size / 2
	)
	d := ui.Box(c.Key("grain")).Size(size, size).Shrink(0).Focusable().Role(ui.RoleSlider).Label(i18n.T("颗粒")).
		Value(fmt.Sprintf("%.0f%%", *value*100)).Cursor(ui.CursorPointer).Tooltip(i18n.T("颗粒"))
	if _, _, held := d.Dragged(); held {
		x, y, _ := d.PointerPosition()
		a := math.Atan2(float64(y-half), float64(x-half)) + math.Pi/2
		if a < 0 {
			a += 2 * math.Pi
		}
		*value = a / (2 * math.Pi)
	}
	switch {
	case d.Shortcut(0, ui.KeyRight), d.Shortcut(0, ui.KeyUp):
		*value = min(1, *value+0.05)
	case d.Shortcut(0, ui.KeyLeft), d.Shortcut(0, ui.KeyDown):
		*value = max(0, *value-0.05)
	}
	v := *value
	focused := d.FocusVisible()
	d.Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		for i := range n {
			a := float64(i)/n*2*math.Pi - math.Pi/2
			col := ink.Alpha(0.18)
			if float64(i)/n <= v {
				col = ink.Alpha(0.7)
			}
			x, y := cx+23*float32(math.Cos(a)), cy+23*float32(math.Sin(a))
			p.Fill(ui.Rect{X: x - 1.5, Y: y - 1.5, W: 3, H: 3}, col, 1.5)
		}
		disc := ui.Rect{X: cx - 15, Y: cy - 15, W: 30, H: 30}
		p.Fill(disc, white.Alpha(0.85), 15)
		paintGrainTint(p, disc, 15, black.Alpha(float32(0.1+0.5*v)), false)
		p.Stroke(disc, black.Alpha(0.1), 15, 1)
		k := v*2*math.Pi - math.Pi/2
		var knob ui.Path
		knob.MoveTo(cx+8*float32(math.Cos(k)), cy+8*float32(math.Sin(k)))
		knob.LineTo(cx+13*float32(math.Cos(k)), cy+13*float32(math.Sin(k)))
		p.StrokePath(&knob, 2.5, ui.RGB(32, 32, 36))
		if focused {
			p.FocusRing(r, [4]float32{half, half, half, half})
		}
	})
}
