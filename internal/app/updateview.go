package app

import (
	"fmt"
	"math"
	"time"

	"github.com/Ciao1019/ArcMD/internal/update"
	"github.com/egoist/mygo/ui"
)

// The update banners, toast and window, ported from Cra's UpdateBanner,
// ShinyToast and UpdaterWindow (MIT) with their measurements of Arc 1.171:
//
//   - Ready ("更新已准备就绪！" / "点击重新启动", or "ArcMD 有新版本"): a 56
//     DIP card above the spaces, title 13 semibold and subtitle 11, a 40×37
//     heart with a download arrow knocked out of it 10 from the right. The
//     arrow lifts, dips and springs back while a heart-shaped ripple grows
//     to 129% and fades (Arc's 2 s timing); it plays when the card appears
//     and loops while it is hovered.
//   - Updated ("ArcMD 已更新！"): a 102 DIP card, title 14 medium with
//     twinkling sparkles, a ✕, and two rows with an icon on a faint tile
//     and a drawn arrow. It pops in with a slight tilt that settles.
//   - The toast says the ready card's news at the top right of the page
//     while the sidebar is hidden, with its button and a ✕.
//   - The update window: the name and version, a status line or the
//     release notes, and one wide button.

const (
	readyCardHeight   = 56
	updatedCardHeight = 102
	// heartCycle is one beat of the heart, Arc's 120 frames at 60 fps.
	heartCycle = 2 * time.Second
	// heartDelay holds the first beat until the card has slid in.
	heartDelay = 250 * time.Millisecond
)

// bannerEnter is the ready card sliding up from under the spaces.
var bannerEnter = ui.Animation{Duration: 350 * time.Millisecond, Ease: ui.CubicBezier(0.2, 0.9, 0.3, 1)}

// updateBanner is the banner above the spaces at the foot of the sidebar.
func (a *App) updateBanner(c *ui.Context) {
	u := &a.updates
	n := a.updateNotice()
	kind := ""
	switch {
	case n != nil:
		kind = "ready"
	case u.updatedTo != "":
		kind = "updated"
	}
	if kind != u.shown {
		u.shown, u.shownSince = kind, a.now
	}
	if kind == "" {
		return
	}
	bl := bannerColors(a.look)
	entering := a.now.Sub(u.shownSince) < time.Millisecond
	ui.Column(c.Key("update-banner")).PaddingX(8).Children(func() {
		if n != nil {
			a.readyCard(c, *n, bl, entering)
		} else {
			a.updatedCard(c, bl, entering)
		}
	})
	if entering {
		c.AnimationFrame()
	}
}

// bannerCard is a banner's card: its graded fill, hover and corners.
func bannerCard(c *ui.Context, key string, height float32, bl bannerLook) ui.Element {
	card := ui.Box(c.Key(key)).Height(height).Radius(11).Clip()
	f := bl.fill
	if card.Hovered() {
		f = bl.hover
	}
	// Arc's fill falls 0.15% a DIP across and 0.05% a DIP down from the
	// top-left corner: nearly across, a little down.
	card.Draw(func(p *ui.Painter, r ui.Rect) {
		p.FillGradient(r, ui.LinearGradient{From: f[0], To: f[1], Angle: 108}, 11)
	})
	return card
}

// readyCard says a new version is downloaded, or out.
func (a *App) readyCard(c *ui.Context, n update.Notice, bl bannerLook, entering bool) {
	u := &a.updates
	card := bannerCard(c, "update-ready", readyCardHeight, bl).Cursor(ui.CursorPointer).
		Focusable().Role(ui.RoleButton).Label(n.Title() + "，" + n.Subtitle()).Tooltip(n.Subtitle())
	pose := ui.Pose{}
	if entering {
		pose = ui.Pose{Y: 14, Fade: 1}
	}
	card.AnimatePose(pose, bannerEnter)
	hovered := card.Hovered()
	start := u.shownSince.Add(heartDelay)
	if hovered {
		// Hovered, the heart beats again and again, from where it is.
		if a.now.Sub(start) >= heartCycle {
			start = a.now.Add(-a.now.Sub(start) % heartCycle)
		}
	}
	still := a.reduceMotion
	heart := bl.heart
	card.Children(func() {
		ui.Column(c).Absolute().Left(10).Top(11.5).Right(56).Gap(2).Children(func() {
			ui.Text(c, n.Title()).FontSize(13).FontWeight(600).TextColor(bl.title).SingleLine()
			ui.Text(c, n.Subtitle()).FontSize(11).TextColor(bl.secondary).SingleLine()
		})
		ui.Box(c.Key("heart")).Absolute().Right(10).Top((readyCardHeight-37)/2).Size(40, 37).
			Draw(func(p *ui.Painter, r ui.Rect) {
				t := p.Now().Sub(start)
				if hovered && t >= heartCycle {
					t %= heartCycle
				}
				if still || t < 0 || t >= heartCycle {
					if t < 0 && !still {
						p.After(-t)
					}
					t = 0
				} else {
					p.AnimationFrame()
				}
				paintUpdateHeart(p, r, heart, float32(t.Seconds()/heartCycle.Seconds()))
			})
		if hovered {
			closeX := ui.Box(c.Key("dismiss")).Absolute().Right(4).Top(4).Size(14, 14).Center().
				Role(ui.RoleButton).Label("关闭").Tooltip("关闭").Cursor(ui.CursorPointer)
			closeX.Children(func() { ui.Icon(c, iconX).FontSize(10).TextColor(bl.tertiary) })
			if closeX.Clicked() {
				a.dismissBanner()
				return
			}
		}
	})
	if card.Clicked() {
		a.actOnNotice(n)
	}
}

// updatedCard says this launch runs the new version.
func (a *App) updatedCard(c *ui.Context, bl bannerLook, entering bool) {
	u := &a.updates
	card := bannerCard(c, "update-done", updatedCardHeight, bl).TransformOrigin(0.5, 0.5)
	if entering {
		card.AnimatePose(ui.Pose{Scale: 0.9, Rotate: -2.9, Fade: 1}, ui.Animation{})
	} else {
		card.AnimatePose(ui.Pose{}, ui.Animation{Spring: ui.SpringOptions{Mass: 1, Stiffness: 220, Damping: 16}})
	}
	title := "ArcMD 已更新！"
	since := u.shownSince
	still := a.reduceMotion
	card.Children(func() {
		ui.Text(c, title).Absolute().Left(12).Top(9.5).FontSize(14).FontWeight(500).TextColor(bl.title).SingleLine().
			DrawOver(func(p *ui.Painter, r ui.Rect) { paintSparkles(p, r, bl.accent, since, still) })
		closeX := ui.Box(c.Key("dismiss")).Absolute().Right(10).Top(12).Size(14, 14).Center().
			Role(ui.RoleButton).Label("关闭").Tooltip("关闭").Cursor(ui.CursorPointer)
		closeX.Children(func() { ui.Icon(c, iconX).FontSize(12).TextColor(bl.tertiary) })
		if closeX.Clicked() {
			a.dismissBanner()
			return
		}
		version := u.updatedTo
		for i, row := range []struct {
			key, label string
			icon       *ui.SVG
			url        string
		}{
			{"whats-new", "新功能", iconHeart, update.ReleaseURL(version)},
			{"star", "在 GitHub 上点星", iconStar, update.Repository},
		} {
			r := ui.Row(c.Key(row.key)).Absolute().Left(4).Right(4).Top(49.4 + float32(i)*31.7 - 15).Height(30).
				AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleLink).Label(row.label)
			hovered := r.Hovered()
			icon, ink := bl.icon, bl.secondary
			if hovered {
				icon, ink = bl.hoverIcon, bl.title
			}
			r.Children(func() {
				ui.Box(c).Margin(0, 0, 0, 8.6).Size(21, 21).Radius(5).Background(bl.tile).Center().Children(func() {
					ui.Icon(c, row.icon).FontSize(12.75).TextColor(icon)
				})
				ui.Text(c, row.label).Margin(0, 0, 0, 8.4).FontSize(13).FontWeight(500).TextColor(ink).SingleLine()
				ui.Box(c).Margin(0, 0, 0, 5.5).Size(12, 10.25).Draw(func(p *ui.Painter, r ui.Rect) {
					paintRowArrow(p, r, ink)
				})
			})
			if r.Clicked() {
				a.openUpdatePage(row.url)
			}
		}
	})
}

// paintRowArrow is the updated card's arrow, larger than the font's →:
// 12 × 10.25 with a 1.5 stroke.
func paintRowArrow(p *ui.Painter, r ui.Rect, c ui.Color) {
	const mid, arm = 5.125, 4.375
	path := &ui.Path{} // round caps and joins are the default
	path.MoveTo(r.X+0.75, r.Y+mid).LineTo(r.X+11.25, r.Y+mid)
	path.MoveTo(r.X+11.25-arm, r.Y+mid-arm).LineTo(r.X+11.25, r.Y+mid).LineTo(r.X+11.25-arm, r.Y+mid+arm)
	p.StrokePath(path, 1.5, c)
}

// sparkles are the stars around the updated card's title, as fractions of
// the box from the card's 4 DIP to the big star, their sizes, and when
// each starts to twinkle.
var sparkles = []struct {
	x, y, size float32
	delay      time.Duration
}{
	{1.0, 0.2333, 8, 0},
	{0.06, 0.88, 2.5, 500 * time.Millisecond},
	{0.42, 0.12, 2, 900 * time.Millisecond},
	{0.62, 0.84, 1.8, 1300 * time.Millisecond},
	{0.24, 0.1, 1.6, 250 * time.Millisecond},
}

// paintSparkles draws the four-point stars that twinkle in turn around the
// title r (Arc's SparklesView): each pulses in scale and opacity on its own
// delay, 1.6 s a pulse. The big one's center sits 6.4 past the title.
func paintSparkles(p *ui.Painter, r ui.Rect, color ui.Color, since time.Time, still bool) {
	box := ui.Rect{X: r.X - 8, Y: r.Y + r.H/2 - 18, W: r.W + 8 + 6.4 + 4, H: 36}
	now := p.Now()
	for _, s := range sparkles {
		k := float32(1)
		if !still {
			t := now.Sub(since) - s.delay
			if t > 0 {
				f := float64(t%(1600*time.Millisecond)) / float64(1600*time.Millisecond)
				// Out and back, eased: 0 at the ends, 1 halfway.
				k = float32(0.5 - 0.5*math.Cos(2*math.Pi*f))
			} else {
				k = 0
			}
		}
		scale, alpha := 0.75+0.25*k, 0.55+0.45*k
		size := s.size * scale
		cx, cy := box.X+box.W*s.x-s.size/2, box.Y+box.H*s.y
		star(p, cx, cy, size, color.Alpha(alpha*float32(color.A)/255))
	}
	if !still {
		p.AnimationFrame()
	}
}

// star fills a four-point star with concave sides, size across.
func star(p *ui.Painter, cx, cy, size float32, c ui.Color) {
	r := size / 2
	k := r * 0.1
	path := &ui.Path{}
	path.MoveTo(cx, cy-r)
	path.QuadTo(cx+k, cy-k, cx+r, cy)
	path.QuadTo(cx+k, cy+k, cx, cy+r)
	path.QuadTo(cx-k, cy+k, cx-r, cy)
	path.QuadTo(cx-k, cy-k, cx, cy-r)
	path.Close()
	p.FillPath(path, c)
}

// The heart is drawn in Arc's 247 × 229 design box: its top cleft at
// (123.5, 36), its point at (123.5, 229).
var heartBox = [2]float32{247, 229}

// heartSegments are the heart's outline, as cubic segments from its point
// round the left lobe and back: start, two controls, end.
var heartSegments = [][4][2]float32{
	{{123.5, 229}, {62, 184}, {0, 136}, {0, 72}},
	{{0, 72}, {0, 30}, {28, 0}, {66, 0}},
	{{66, 0}, {94, 0}, {114, 16}, {123.5, 36}},
	{{123.5, 36}, {133, 16}, {153, 0}, {181, 0}},
	{{181, 0}, {219, 0}, {247, 30}, {247, 72}},
	{{247, 72}, {247, 136}, {185, 184}, {123.5, 229}},
}

// heartContour adds the heart, through at, to path; reverse runs it the
// other way round, which cuts it out of a shape drawn the first way.
func heartContour(path *ui.Path, at func(x, y float32) (float32, float32), reverse bool) {
	if !reverse {
		path.MoveTo(at(heartSegments[0][0][0], heartSegments[0][0][1]))
		for _, s := range heartSegments {
			c1x, c1y := at(s[1][0], s[1][1])
			c2x, c2y := at(s[2][0], s[2][1])
			x, y := at(s[3][0], s[3][1])
			path.CubeTo(c1x, c1y, c2x, c2y, x, y)
		}
		path.Close()
		return
	}
	last := heartSegments[len(heartSegments)-1]
	path.MoveTo(at(last[3][0], last[3][1]))
	for i := len(heartSegments) - 1; i >= 0; i-- {
		s := heartSegments[i]
		c1x, c1y := at(s[2][0], s[2][1])
		c2x, c2y := at(s[1][0], s[1][1])
		x, y := at(s[0][0], s[0][1])
		path.CubeTo(c1x, c1y, c2x, c2y, x, y)
	}
	path.Close()
}

// arrowOutline is the download arrow knocked out of the heart, dy design
// units below its resting place: a 17-unit stroke, round capped and
// joined, its stem from 73 to the head, its head from (85, 123.5) down to
// the tip at (123.5, 155) and up to (162, 123.5), as Cra measures Arc's.
// The outline runs the other way round from heartSegments, so that filling
// both together leaves the arrow empty.
func arrowOutline(dy float32) [][2]float32 {
	const (
		cx   = 123.5
		half = 8.5
		top  = 73
		tipY = 155
		armY = 123.5
		lx   = 85
		rx   = 162
	)
	// The arms' directions from the tip and their normals.
	l := float32(math.Hypot(rx-cx, tipY-armY))
	ux, uy := (rx-cx)/l, (armY-tipY)/l // up the right arm
	var pts [][2]float32
	add := func(x, y float32) { pts = append(pts, [2]float32{x, y + dy}) }
	arc := func(x, y float32, from, to float64) {
		const steps = 8
		for i := 0; i <= steps; i++ {
			a := from + (to-from)*float64(i)/steps
			add(x+half*float32(math.Cos(a)), y+half*float32(math.Sin(a)))
		}
	}
	// Where the stem's sides meet the arms' inner edges.
	inner := func(side float32) (float32, float32) {
		// The inner edge of the arm on side (+1 right, -1 left) runs from
		// the tip, shifted toward the stem, along the arm.
		nx, ny := -uy*side, ux*side // perpendicular, toward the stem
		if ny > 0 {
			nx, ny = -nx, -ny
		}
		ox, oy := cx+half*nx, tipY+half*ny
		dx, dy := ux*side, uy
		s := (cx + side*half - ox) / dx
		return cx + side*half, oy + dy*s
	}
	rad := func(x, y float32) float64 { return math.Atan2(float64(y), float64(x)) }
	inR, inL := rad(-uy, -ux), rad(uy, -ux) // the arms' inner normals (up, toward the stem)
	outR, outL := rad(uy, ux), rad(-uy, ux) // and outer ones (down)
	if outR < 0 {
		outR += 2 * math.Pi
	}
	// Clockwise on screen: over the stem's cap, down its right side, out
	// along the right arm and round its end, back under it, round the tip,
	// out under the left arm and round its end, back along its top, up the
	// stem's left side.
	arc(cx, top, math.Pi, 2*math.Pi)
	add(inner(1))
	add(rx+half*float32(math.Cos(inR)), armY+half*float32(math.Sin(inR)))
	arc(rx, armY, inR+2*math.Pi, outR+2*math.Pi)
	arc(cx, tipY, outR, outL)
	arc(lx, armY, outL, inL+2*math.Pi)
	add(inner(-1))
	// Reverse: the heart runs the other way.
	for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
		pts[i], pts[j] = pts[j], pts[i]
	}
	return pts
}

// heartKeys are the arrow's offsets through a beat: it lifts 10 units by
// frame 15, dips 17.5 below its rest by 45 and is home by 80, each step
// on its own curve (Arc's update-heart-animation).
var heartKeys = []struct {
	at, dy float32
	ease   ui.Easing
}{
	{0, 0, nil},
	{15.0 / 120, -10, ui.CubicBezier(0.333, 0, 0.288, 1)},
	{45.0 / 120, 17.5, ui.CubicBezier(0.522, 0, 0.29, 1)},
	{80.0 / 120, 0, ui.CubicBezier(0.333, 0, 0.066, 1)},
	{1, 0, ui.Linear},
}

var (
	rippleGrow = ui.CubicBezier(0.245, 0, 0.671, 1)
	rippleFade = ui.CubicBezier(0.167, 0, 0.357, 1)
)

// heartPose is the arrow's offset and the ripple's scale and opacity at f,
// from 0 to 1 of a beat.
func heartPose(f float32) (dy, scale, alpha float32) {
	for i := 1; i < len(heartKeys); i++ {
		a, b := heartKeys[i-1], heartKeys[i]
		if f <= b.at {
			k := b.ease((f - a.at) / (b.at - a.at))
			dy = a.dy + (b.dy-a.dy)*k
			break
		}
	}
	// The ripple grows from 100% to 129% and fades from 80% between
	// frames 26 and 107.
	const from, to = 26.0 / 120, 107.0 / 120
	switch {
	case f <= from:
		scale, alpha = 1, 0.8
	case f >= to:
		scale, alpha = 1.29, 0
	default:
		k := (f - from) / (to - from)
		scale, alpha = 1+0.29*rippleGrow(k), 0.8*(1-rippleFade(k))
	}
	if f <= 0 {
		alpha = 0 // at rest, no ripple
	}
	return dy, scale, alpha
}

// paintUpdateHeart draws the heart with its arrow knocked out, and the
// ripple, at f of a beat, fitted into r.
func paintUpdateHeart(p *ui.Painter, r ui.Rect, color ui.Color, f float32) {
	s := min(r.W/heartBox[0], r.H/heartBox[1])
	ox, oy := r.X+(r.W-heartBox[0]*s)/2, r.Y+(r.H-heartBox[1]*s)/2
	dy, scale, alpha := heartPose(f)
	if alpha > 0 {
		// The ripple: a band along the heart's edge, the heart less a 75%
		// heart, grown about the heart's middle.
		const mx, my = 123.5, 114.5
		at := func(k float32) func(x, y float32) (float32, float32) {
			return func(x, y float32) (float32, float32) {
				return ox + (mx+(x-mx)*k*scale)*s, oy + (my+(y-my)*k*scale)*s
			}
		}
		band := &ui.Path{}
		heartContour(band, at(1), false)
		heartContour(band, at(0.75), true)
		p.FillPath(band, color.Alpha(alpha*float32(color.A)/255))
	}
	at := func(x, y float32) (float32, float32) { return ox + x*s, oy + y*s }
	heart := &ui.Path{}
	heartContour(heart, at, false)
	for i, pt := range arrowOutline(dy) {
		if i == 0 {
			heart.MoveTo(at(pt[0], pt[1]))
		} else {
			heart.LineTo(at(pt[0], pt[1]))
		}
	}
	heart.Close()
	p.FillPath(heart, color)
}

// toastSpring is the toast springing in from its top, ω = 2π / 0.3 s with
// a damping ratio of 0.67: Arc's 4% overshoot.
var toastSpring = ui.Animation{Spring: ui.SpringOptions{Mass: 1, Stiffness: 440, Damping: 28}}

// updateToast says the banner's news at the top right of the page while
// the sidebar is hidden (Cra's update toast), until its ✕ or its button.
func (a *App) updateToast(c *ui.Context) {
	u := &a.updates
	if u.toast == nil {
		return
	}
	n := *u.toast
	tl := toastColors(a.look)
	top := float32(8)
	if !a.focus {
		top += max(c.TitleBar().Height, 46)
	}
	ui.Overlay(c, func() {
		pill := ui.Row(c.Key("update-toast")).Absolute().Right(8+9).Top(top).Height(42).Radius(10).
			PaddingStart(12).PaddingEnd(8).Gap(10).AlignItems(ui.Center).
			Border(1, tl.outline).Shadow(0, 4, 14, 0, black.Alpha(0.12)).TransformOrigin(0.5, 0).
			Role(ui.RoleStatus).Label(n.Title())
		pill.Draw(func(p *ui.Painter, r ui.Rect) {
			p.FillGradient(r, ui.LinearGradient{From: tl.top, To: tl.bottom, Angle: 180}, 10)
		})
		if u.toastFrames == 0 {
			pill.AnimatePose(ui.Pose{Scale: 0.4}, toastSpring)
			c.AnimationFrame()
		} else {
			pill.AnimatePose(ui.Pose{}, toastSpring)
		}
		u.toastFrames++
		pill.Children(func() {
			ui.Text(c, n.Title()).FontSize(14).FontWeight(500).TextColor(tl.text).SingleLine()
			b := ui.Box(c.Key("act")).Height(26).PaddingX(10).Radius(13).Center().Cursor(ui.CursorPointer).
				Focusable().Role(ui.RoleButton).Label(n.Button())
			fill := float32(0.1)
			if b.Hovered() {
				fill = 0.16
			}
			b.Background(tl.text.Alpha(fill)).Children(func() {
				ui.Text(c, n.Button()).FontSize(13).FontWeight(600).TextColor(tl.text)
			})
			x := ui.Box(c.Key("close")).Size(26, 26).Radius(13).Center().Cursor(ui.CursorPointer).
				Focusable().Role(ui.RoleButton).Label("关闭").Tooltip("关闭")
			if x.Hovered() {
				x.Background(tl.text.Alpha(0.1))
			}
			x.Children(func() { ui.Icon(c, iconX).FontSize(11).TextColor(tl.text) })
			if b.Clicked() {
				a.actOnNotice(n)
			}
			if x.Clicked() {
				u.toast = nil
			}
		})
	})
}

// updateWindowView is the update window (Arc's UpdaterWindowController, as
// Cra measures it): ArcMD's name and version, what the updater is doing,
// and one wide button, with "以后再说" under it when there is a choice.
func (a *App) updateWindowView(c *ui.Context) {
	u := &a.updates
	if !u.window {
		return
	}
	t := c.Theme()
	open := true
	ui.Modal(c.Key("updater"), &open, func() {
		ui.Column(c.Key("updater-body")).Width(270).AlignItems(ui.Center).Padding(20, 0, 26, 0).Children(func() {
			ui.Box(c.Key("updater-heart")).Size(84, 78).Draw(func(p *ui.Painter, r ui.Rect) {
				paintUpdateHeart(p, ui.Rect{X: r.X + 10, Y: r.Y + 10, W: r.W - 20, H: r.H - 20}, t.Accent, 0)
			})
			title := "ArcMD"
			sub := "版本 " + Version
			if u.release != nil && (u.phase == phaseFound || u.phase == phaseDownloading || u.phase == phaseReady) {
				title = "ArcMD " + u.release.Version()
				sub = "当前版本 " + Version
			}
			ui.Text(c, title).Margin(10, 0, 0, 0).FontSize(24).FontWeight(700)
			ui.Text(c, sub).FontSize(11).TextColor(t.Text.Alpha(0.5))
			status := func(s string) {
				ui.Text(c.Key("status"), s).Width(208).Margin(18, 0, 10, 0).FontSize(13).TextAlign(ui.Center).
					TextColor(t.Text.Alpha(0.5))
			}
			progress := func(v float64, label string) {
				ui.Column(c.Key("progress")).Width(170).Gap(6).Margin(18, 0, 10, 0).Children(func() {
					ui.Progress(c, v)
					ui.Text(c, label).FontSize(11).TextColor(t.Text.Alpha(0.5)).AlignSelf(ui.Center)
				})
			}
			switch u.phase {
			case phaseChecking:
				status("正在检查更新…")
				if updaterButtonView(c, "取消", false) {
					a.closeUpdateWindow()
				}
			case phaseUpToDate:
				status("ArcMD 已是最新版本")
				if updaterButtonView(c, "好", false) {
					a.closeUpdateWindow()
				}
			case phaseFound:
				lines := update.Lines(u.release.Notes())
				if len(lines) > 0 {
					releaseNotes(c, lines)
				} else {
					status("有新版本可用")
				}
				if updaterButtonView(c, "下载并安装", false) {
					a.downloadUpdate()
				}
				if laterButton(c, "以后再说") {
					a.closeUpdateWindow()
				}
			case phaseDownloading:
				if u.total > 0 {
					progress(float64(u.received)/float64(u.total), byteSize(u.received)+" / "+byteSize(u.total))
				} else {
					progress(-1, "正在下载…")
				}
				if updaterButtonView(c, "取消", false) {
					a.closeUpdateWindow()
				}
			case phaseReady:
				status("更新已准备就绪，重新启动即可安装")
				if updaterButtonView(c, "安装并重新启动", false) {
					a.installUpdate()
				}
				if laterButton(c, "以后再说") {
					a.closeUpdateWindow()
				}
			case phaseFailed:
				status(u.failure)
				if updaterButtonView(c, "手动下载", false) {
					a.closeUpdateWindow()
					a.openUpdatePage(update.ReleaseURL(""))
				}
				if laterButton(c, "关闭") {
					a.closeUpdateWindow()
				}
			case phaseDisabled:
				status("这个版本无法自动更新：开发版本，或安装在无法写入的位置。可以从 GitHub 下载新版本。")
				if updaterButtonView(c, "前往下载页面", false) {
					a.closeUpdateWindow()
					a.openUpdatePage(update.ReleaseURL(""))
				}
				if laterButton(c, "关闭") {
					a.closeUpdateWindow()
				}
			}
		})
	})
	if !open {
		a.closeUpdateWindow()
	}
}

// updaterButtonView is the window's wide button: 170 × 48, radius 6, white
// (black in the dark) with a faint shadow, its label 13 bold.
func updaterButtonView(c *ui.Context, label string, disabled bool) bool {
	t := c.Theme()
	b := ui.Box(c.Key("updater-button")).Margin(19, 0, 0, 0).Size(170, 48).Radius(6).Center().
		Background(updaterButton(t)).Shadow(0, 0, 10, 0, black.Alpha(0.1)).
		Focusable().Role(ui.RoleButton).Label(label).Disabled(disabled).Cursor(ui.CursorPointer)
	if b.Pressed() {
		b.Opacity(0.75)
	}
	b.Children(func() { ui.Text(c, label).FontSize(13).FontWeight(700).TextColor(t.Text.Alpha(0.94)) })
	return !disabled && (b.Clicked() || c.Shortcut(0, ui.KeyEnter))
}

// laterButton is the plain choice under the wide button.
func laterButton(c *ui.Context, label string) bool {
	t := c.Theme()
	b := ui.Box(c.Key("updater-later")).Margin(12, 0, 0, 0).PaddingX(8).Height(20).Center().
		Focusable().Role(ui.RoleButton).Label(label).Cursor(ui.CursorPointer)
	ink := t.Text.Alpha(0.5)
	if b.Hovered() {
		ink = t.Text.Alpha(0.75)
	}
	b.Children(func() { ui.Text(c, label).FontSize(13).TextColor(ink) })
	return b.Clicked() || c.Shortcut(0, ui.KeyEscape)
}

// releaseNotes are the new version's notes, 230 × 150 on a faint fill.
func releaseNotes(c *ui.Context, lines []string) {
	t := c.Theme()
	ui.Scroll(c.Key("release-notes")).Margin(18, 0, 0, 0).Size(230, 150).Radius(8).Background(t.Text.Alpha(0.05)).
		Padding(10).Gap(2).Children(func() {
		for i, l := range lines {
			if l == "" {
				ui.Box(c.Key(i)).Height(6)
				continue
			}
			ui.Text(c.Key(i), l).FontSize(12).LineHeight(1.6).Selectable()
		}
	})
}

// byteSize is a size for people: "1.4 MB".
func byteSize(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, suffix := float64(n), []string{"KB", "MB", "GB"}
	i := -1
	for v >= unit && i < len(suffix)-1 {
		v /= unit
		i++
	}
	return fmt.Sprintf("%.1f %s", v, suffix[i])
}
