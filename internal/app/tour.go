package app

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo/ui"
)

// The tour introduces the window the first time ArcMD runs with it, on a
// new installation or after an update: a scrim over the
// window with a hole round what a step is about, and a card beside it
// with a small moving picture, a few lines and the way on. The hole and
// the card glide from one step's place to the next. Nothing behind the
// scrim acts while it shows; the tour only points, it changes no space
// and no document: the theme card its step on themes opens is put away
// as it was.

// tour is the tour's state.
type tour struct {
	open bool
	step int
	// dir is the way the step was reached, which its text slides in from.
	dir int
	// since is when the step began, the clock of its picture; frames and
	// stepFrames count the frames built since the tour and the step came.
	since              time.Time
	frames, stepFrames int
	// leaving fades the tour away before it closes.
	leaving bool
	// marks are the places of the sidebar's parts as this frame built
	// them, hole the last place pointed at and holed whether the frame
	// before pointed at one.
	marks map[string]ui.Rect
	hole  ui.Rect
	holed bool
	// themeHeld is whether the tour holds the theme card open for its
	// step, and themeBase the colors the card opened with, which the step
	// turns round the wheel to show the window following them.
	themeHeld bool
	themeBase []string
}

// tourStep is a step: id names its picture and what it points at.
type tourStep struct {
	id, label, title string
	body             func(a *App) string
	// keys are shown as key caps under the text.
	keys func(a *App) []string
}

var tourSteps = []tourStep{
	{id: "welcome", label: "新手教程", title: "欢迎来到 ArcMD",
		body: func(*App) string {
			return i18n.T("用一分钟认识 ArcMD：空间、主题配色、左右滑动、书签、置顶和收藏。看完就可以开始写作。")
		},
		keys: func(*App) []string { return []string{i18n.T("→ 下一步"), i18n.T("Esc 跳过")} }},
	{id: "spaces", label: "空间", title: "用空间分开不同的事",
		body: func(*App) string {
			return i18n.T("每个空间是一个文件夹，有自己的文档、置顶和配色，比如「工作」和「生活」。点这里右侧的「+」新建空间，给它起名、选图标和存放位置。")
		},
		keys: func(a *App) []string { return []string{a.tourKey("palette") + i18n.T(" 新建空间…")} }},
	{id: "theme", label: "主题配色", title: "给每个空间一身颜色",
		body: func(*App) string {
			return i18n.T("这就是主题画板：把指针移到空间名称上，点右侧的「•••」选择「编辑主题颜色…」打开它。拖动圆点选 1–3 种颜色，再调强度和颗粒，整个窗口像现在这样实时预览。")
		},
		keys: func(*App) []string {
			return []string{i18n.T("跟随系统"), i18n.T("浅色"), i18n.T("深色"), i18n.T("− / + 颜色数")}
		}},
	{id: "swipe", label: "左右滑动", title: "左右轻扫，切换空间",
		body: func(*App) string {
			return i18n.T("在侧边栏上双指左右轻扫，像翻页一样换到相邻的空间，窗口的颜色跟着渐变。用键盘也可以。")
		},
		keys: func(a *App) []string {
			return []string{a.tourKey("prev-space"), a.tourKey("next-space"), "⌃1 … ⌃9"}
		}},
	{id: "bookmarks", label: "书签", title: "用书签记住读到哪儿",
		body: func(*App) string {
			return i18n.T("指针移到任意一段上，点正文右侧页边的书签图标，这一段就有了书签。目录底部列出本文档的书签，点一下跳回去；书签列表里是所有文档的书签。")
		},
		keys: func(a *App) []string {
			return []string{a.tourKey("bookmark") + i18n.T(" 添加书签"), a.tourKey("bookmarks") + i18n.T(" 书签列表"), a.tourKey("prev-bookmark") + " " + a.tourKey("next-bookmark") + i18n.T(" 跳转")}
		}},
	{id: "pins", label: "置顶", title: "把常用的文档置顶",
		body: func(*App) string {
			return i18n.T("右键文档或文件夹选择「置顶」，或者直接把它拖过分隔线。置顶的文档一直保持打开，再点回来时光标和滚动位置都还在。")
		},
		keys: func(*App) []string { return []string{i18n.T("右键 › 置顶"), i18n.T("拖过分隔线")} }},
	{id: "favorites", label: "收藏", title: "收藏，在每个空间都看得到",
		body: func(*App) string {
			return i18n.T("把文档拖到侧边栏顶部，或右键选择「添加到收藏」。收藏是全局的：无论在哪个空间，点一下磁贴就能打开它。")
		},
		keys: func(*App) []string { return []string{i18n.T("右键 › 添加到收藏"), i18n.T("拖到顶部")} }},
	{id: "done", label: "完成", title: "准备好了，开始写吧",
		body: func(*App) string {
			return i18n.T("所有功能都能在命令栏里找到。想再看一遍，在命令栏输入「新手教程」，或打开 设置 › 关于。")
		},
		keys: func(a *App) []string {
			return []string{a.tourKey("palette") + i18n.T(" 命令栏"), a.tourKey("new-doc") + i18n.T(" 新建文档")}
		}},
}

// tourKey is the command's first shortcut, as the user has it.
func (a *App) tourKey(id string) string {
	if k := a.keyLabel(id); k != "" {
		return strings.Split(k, " / ")[0]
	}
	return i18n.T("命令栏")
}

const (
	tourCardWidth = 360
	tourArtHeight = 172
	// tourHolePad is the room the hole leaves round what it shows, and
	// tourHoleRadius its corners.
	tourHolePad    = 6
	tourHoleRadius = 14
	tourGap        = 18 // between the hole and the card
	tourMargin     = 16 // between the card and the window's edges
)

var (
	// tourGlide moves the hole and the card: a spring that settles in
	// about 0.4 s with the faintest overshoot.
	tourGlide = ui.Animation{Spring: ui.SpringOptions{Mass: 1, Stiffness: 240, Damping: 29}}
	tourFade  = ui.Animation{Duration: 240 * time.Millisecond, Ease: ui.EaseOut}
	tourEnter = ui.Animation{Duration: 300 * time.Millisecond, Ease: ui.EaseOut}
)

// startTour shows the tour from its first step, over the sidebar as it
// is at rest: docked, with nothing else open.
func (a *App) startTour() {
	a.finishSpaceEdit()
	a.closeLibrary()
	a.palette = palette{}
	a.settingsOpen = false
	a.revealSidebar()
	a.tour = tour{open: true}
}

// endTour fades the tour away, finished or skipped; it does not show by
// itself again.
func (a *App) endTour() {
	t := &a.tour
	if !t.open || t.leaving {
		return
	}
	t.leaving = true
	a.tourSeen()
}

// tourSeen remembers that the tour was shown, which keeps it from
// showing by itself again.
func (a *App) tourSeen() {
	if !a.prefs.TourSeen {
		a.prefs.TourSeen = true
		if !a.prefsBroken {
			a.savePrefs()
		}
	}
}

// tourGo steps the tour on or back; past the last step it ends.
func (a *App) tourGo(by int) {
	t := &a.tour
	if !t.open || t.leaving {
		return
	}
	next := t.step + by
	switch {
	case next < 0:
		return
	case next >= len(tourSteps):
		a.endTour()
		return
	}
	t.step, t.dir, t.since, t.stepFrames = next, by, a.now, 0
}

// tourTheme keeps the theme card open through the tour's step on themes,
// its colors turning slowly round the wheel so the window shows how it
// follows them, and puts it away, nothing kept, as the step is left.
func (a *App) tourTheme() {
	t := &a.tour
	sp := a.state.ActiveSpace()
	if !t.open || t.leaving || tourSteps[t.step].id != "theme" || sp == nil || a.focus {
		if t.themeHeld {
			t.themeHeld, t.themeBase = false, nil
			a.cancelSpaceForm()
		}
		return
	}
	f := &a.spaceForm
	if !t.themeHeld || !f.open || f.id != sp.ID || f.edit != "theme" {
		th := space.ThemeOf(sp)
		a.spaceForm = spaceForm{open: true, id: sp.ID, edit: "theme", name: sp.Name, icon: sp.Icon, symbol: sp.Symbol, root: sp.Root, theme: th, knob: -1}
		f.page = max(0, space.PageOf(th.Colors))
		t.themeHeld, t.themeBase = true, slices.Clone(th.Colors)
	}
	f.cardOpen = true
	if a.reduceMotion || len(t.themeBase) == 0 || len(f.theme.Colors) == 0 {
		return
	}
	// Out 50 degrees one way and the other, 7 s a turn, from where it was.
	turn := 50 * math.Sin(a.now.Sub(t.since).Seconds()/7*2*math.Pi) * math.Pi / 180
	x, y := knobAt(t.themeBase[0])
	sin, cos := float32(math.Sin(turn)), float32(math.Cos(turn))
	placeKnob(&f.theme, 0, x*cos-y*sin, x*sin+y*cos)
}

// tourMark notes where e, the part of the window called id, shows, for
// the step that points at it.
func (a *App) tourMark(id string, e ui.Element) {
	if !a.tour.open {
		return
	}
	if a.tour.marks == nil {
		a.tour.marks = make(map[string]ui.Rect)
	}
	a.tour.marks[id] = e.Bounds()
}

// tourTarget is what the step called id points at, in a window w by h:
// a little more than the part itself, kept inside the window. A part
// that does not show, as the sidebar's in focus mode, has none, and the
// step's card then stands alone in the middle.
func (a *App) tourTarget(id string, w, h float32) (ui.Rect, bool) {
	marks := a.tour.marks
	r := marks[id]
	if id == "theme" {
		// The theme card, once it stands beside the header.
		if card := marks["theme-card"]; card.H >= 8 {
			r = card
		}
	}
	if id == "pins" {
		// With nothing pinned yet, the first row stands for what to pin.
		if r = marks["pinned"]; r.H < 8 && len(a.rows) > 0 {
			r = a.rows[0].r
		}
		if r.H < 8 {
			r = marks["swipe"]
		}
	}
	if r.W < 8 || r.H < 8 {
		return ui.Rect{}, false
	}
	x0, y0 := max(r.X-tourHolePad, 4), max(r.Y-tourHolePad, 4)
	x1, y1 := min(r.X+r.W+tourHolePad, w-4), min(r.Y+r.H+tourHolePad, h-4)
	if x1-x0 < 8 || y1-y0 < 8 {
		return ui.Rect{}, false
	}
	return ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}, true
}

// tourPlace is where the card, cw by ch, goes in a window w by h: beside
// the target, on the first side with room for it (right, left, below,
// above), else in the target's bottom right corner; alone, in the
// middle of the window.
func tourPlace(target ui.Rect, has bool, cw, ch, w, h float32) (x, y float32) {
	clampX := func(v float32) float32 { return max(min(v, w-cw-tourMargin), tourMargin) }
	clampY := func(v float32) float32 { return max(min(v, h-ch-tourMargin), tourMargin) }
	if !has {
		return clampX((w - cw) / 2), clampY((h - ch) / 2)
	}
	midX, midY := target.X+target.W/2-cw/2, target.Y+target.H/2-ch/2
	right, bottom := target.X+target.W, target.Y+target.H
	switch {
	case right+tourGap+cw <= w-tourMargin:
		return right + tourGap, clampY(midY)
	case target.X-tourGap-cw >= tourMargin:
		return target.X - tourGap - cw, clampY(midY)
	case bottom+tourGap+ch <= h-tourMargin:
		return clampX(midX), bottom + tourGap
	case target.Y-tourGap-ch >= tourMargin:
		return clampX(midX), target.Y - tourGap - ch
	}
	return clampX(right - cw - 24), clampY(bottom - ch - 24)
}

// tourView builds the tour above everything else.
func (a *App) tourView(c *ui.Context) {
	t := &a.tour
	if !t.open {
		return
	}
	if t.since.IsZero() {
		t.since = a.now
	}
	w, h := c.Size()
	step := tourSteps[t.step]
	target, has := a.tourTarget(step.id, w, h)
	tl := tourColors(a.look)
	first, fresh := t.frames == 0, t.stepFrames == 0
	t.frames++
	t.stepFrames++
	if first || fresh {
		c.AnimationFrame() // the poses set this frame ease away from the next
	}
	ui.Overlay(c, func() {
		scrim := ui.Box(c.Key("tour")).Absolute().Left(0).Top(0).Right(0).Bottom(0).Modal().
			Role(ui.RoleDialog).Label(i18n.T("新手教程"))
		if first {
			scrim.Jump("in", 0)
		}
		in := scrim.AnimateTo("in", b2f(!t.leaving), tourFade)
		if t.leaving && in < 0.01 {
			*t = tour{}
			return
		}
		if has {
			if !t.holed {
				// A hole after none opens where it is, rather than
				// gliding in from the last one's place.
				scrim.Jump("x", target.X)
				scrim.Jump("y", target.Y)
				scrim.Jump("w", target.W)
				scrim.Jump("h", target.H)
			}
			t.hole = target
		}
		t.holed = has
		hole := ui.Rect{
			X: scrim.AnimateTo("x", t.hole.X, tourGlide), Y: scrim.AnimateTo("y", t.hole.Y, tourGlide),
			W: scrim.AnimateTo("w", t.hole.W, tourGlide), H: scrim.AnimateTo("h", t.hole.H, tourGlide),
		}
		open := scrim.AnimateTo("hole", b2f(has), tourFade)
		since, still := t.since, a.reduceMotion
		scrim.Draw(func(p *ui.Painter, r ui.Rect) { paintTourScrim(p, r, hole, open, in, tl, since, still) })
		scrim.Children(func() { a.tourCard(c, step, target, has, tl, first, fresh, w, h) })
		if t.leaving {
			return
		}
		switch {
		case scrim.OverlayShortcut(0, ui.KeyEscape):
			a.endTour()
		case scrim.OverlayShortcut(0, ui.KeyRight):
			a.tourGo(1)
		case scrim.OverlayShortcut(0, ui.KeyLeft):
			a.tourGo(-1)
		}
	})
}

// paintTourScrim dims the window r but for the hole, open of the way
// open, with a ring round it and a second ring spreading from it now and
// then; in fades the whole.
func paintTourScrim(p *ui.Painter, r, hole ui.Rect, open, in float32, tl tourLook, since time.Time, still bool) {
	dim := tl.scrimAlpha * in
	if hole.W < 1 || hole.H < 1 || open < 0.004 {
		p.Fill(r, tl.scrim.Alpha(dim), 0)
		return
	}
	path := &ui.Path{}
	path.MoveTo(r.X, r.Y).LineTo(r.X+r.W, r.Y).LineTo(r.X+r.W, r.Y+r.H).LineTo(r.X, r.Y+r.H).Close()
	holeContour(path, hole, tourHoleRadius)
	p.FillPath(path, tl.scrim.Alpha(dim))
	if open < 1 {
		p.Fill(hole, tl.scrim.Alpha(dim*(1-open)), tourHoleRadius)
	}
	p.Stroke(hole, tl.ring.Alpha(0.85*in*open), tourHoleRadius, 1.5)
	if still {
		return
	}
	// The second ring leaves every 2.2 s, spreading 12 DIPs as it fades.
	const period = 2200 * time.Millisecond
	f := float32(p.Now().Sub(since)%period) / float32(period)
	if f < 0.6 {
		k := f / 0.6
		g := 12 * (1 - (1-k)*(1-k))
		p.Stroke(ui.Rect{X: hole.X - g, Y: hole.Y - g, W: hole.W + 2*g, H: hole.H + 2*g},
			tl.ring.Alpha(0.45*(1-k)*in*open), tourHoleRadius+g, 1.5)
	}
	p.AnimationFrame()
}

// holeContour adds the rounded rectangle r to path the other way round
// from the rectangle about it, which cuts it out.
func holeContour(path *ui.Path, r ui.Rect, radius float32) {
	rad := min(radius, r.W/2, r.H/2)
	k := rad * 0.4477 // the control points of a quarter circle, from its corner
	x0, y0, x1, y1 := r.X, r.Y, r.X+r.W, r.Y+r.H
	path.MoveTo(x0+rad, y0)
	path.CubeTo(x0+k, y0, x0, y0+k, x0, y0+rad)
	path.LineTo(x0, y1-rad)
	path.CubeTo(x0, y1-k, x0+k, y1, x0+rad, y1)
	path.LineTo(x1-rad, y1)
	path.CubeTo(x1-k, y1, x1, y1-k, x1, y1-rad)
	path.LineTo(x1, y0+rad)
	path.CubeTo(x1, y0+k, x1-k, y0, x1-rad, y0)
	path.Close()
}

// tourCard is the step's card: its picture over the space's tint, what
// the step is, its text and key caps, and the dots and buttons.
func (a *App) tourCard(c *ui.Context, step tourStep, target ui.Rect, has bool, tl tourLook, first, fresh bool, w, h float32) {
	t := &a.tour
	th := documentTheme(a.paperDark)
	card := ui.Themed(c.Key("tour-card"), th).Absolute().Width(tourCardWidth).Radius(18).
		Background(th.Background).Border(1, th.Border.Alpha(0.7)).
		Shadow(0, 18, 48, 0, black.Alpha(0.3)).Clip().TransformOrigin(0.5, 0.5)
	ch := card.LayoutBounds().H
	if ch < 1 {
		ch = 404 // before the first layout
	}
	x, y := tourPlace(target, has, tourCardWidth, ch, w, h)
	if first {
		card.Jump("left", x)
		card.Jump("top", y)
	}
	card.Left(card.AnimateTo("left", x, tourGlide)).Top(card.AnimateTo("top", y, tourGlide))
	switch {
	case first:
		card.AnimatePose(ui.Pose{Y: 14, Scale: 0.96, Fade: 1}, ui.Animation{})
	case t.leaving:
		card.AnimatePose(ui.Pose{Scale: 0.97, Fade: 1}, tourFade)
	default:
		card.AnimatePose(ui.Pose{}, tourEnter)
	}
	// What changes with the step comes in from the side it was reached
	// from, fading.
	enter := func(e ui.Element, by float32) {
		if fresh && !first {
			e.AnimatePose(ui.Pose{X: by * float32(t.dir), Fade: 1}, ui.Animation{})
		} else {
			e.AnimatePose(ui.Pose{}, tourEnter)
		}
	}
	last := len(tourSteps) - 1
	look, since, still := a.look, t.since, a.reduceMotion
	card.Children(func() {
		art := ui.Box(c.Key("art")).Height(tourArtHeight)
		art.Draw(func(p *ui.Painter, r ui.Rect) { look.paint(p, r, 0, false) })
		art.Children(func() {
			pic := ui.Box(c.Key("art-" + step.id)).Absolute().Left(0).Top(0).Right(0).Bottom(0)
			enter(pic, 10)
			id := step.id
			pic.Draw(func(p *ui.Painter, r ui.Rect) { paintTourArt(p, r, id, tl, since, still) })
			if t.step < last {
				x := ui.Box(c.Key("tour-skip")).Absolute().Right(10).Top(10).Size(26, 26).Radius(13).Center().
					Focusable().Role(ui.RoleButton).Label(i18n.T("跳过教程")).Tooltip(i18n.T("跳过教程 (Esc)")).Cursor(ui.CursorPointer)
				fill := float32(0.1)
				if x.Hovered() {
					fill = 0.2
				}
				x.Background(tl.ink.Alpha(fill)).Children(func() { ui.Icon(c, iconX).FontSize(12).TextColor(tl.ink.Alpha(0.8)) })
				if x.Clicked() {
					a.endTour()
				}
			}
		})
		text := ui.Column(c.Key("text-"+step.id)).Padding(18, 22, 0, 22).Gap(8)
		enter(text, 18)
		text.Children(func() {
			label := i18n.T(step.label)
			if t.step > 0 && t.step < last {
				label = fmt.Sprintf("%s · %d / %d", i18n.T(step.label), t.step, last-1)
			}
			ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(th.Accent)
			ui.Text(c, i18n.T(step.title)).FontSize(20).FontWeight(650)
			ui.Text(c, step.body(a)).FontSize(13.5).LineHeight(1.65).TextColor(th.TextMuted)
			if step.keys != nil {
				ui.Row(c.Key("keys")).Gap(6).Padding(4, 0, 0, 0).Wrap().Children(func() {
					for _, k := range step.keys(a) {
						ui.Box(c.Key(k)).Height(24).PaddingX(8).Radius(7).Center().
							Background(th.Surface).Border(1, th.Border.Alpha(0.8)).Children(func() {
							ui.Text(c, k).FontSize(11.5).TextColor(th.Text.Alpha(0.72)).NoWrap()
						})
					}
				})
			}
		})
		ui.Row(c.Key("foot")).Padding(18, 22, 20, 22).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Row(c.Key("dots")).Gap(5).AlignItems(ui.Center).Children(func() {
				for i, s := range tourSteps {
					on := i == t.step
					dot := ui.Box(c.Key(i)).Height(6).Radius(3).Role(ui.RoleButton).Label(i18n.T("第 ") + fmt.Sprint(i+1) + i18n.T(" 步：") + i18n.T(s.label)).
						Tooltip(i18n.T(s.label)).Cursor(ui.CursorPointer)
					width, ink := float32(6), th.Text.Alpha(0.16)
					if on {
						width, ink = 18, th.Accent
					}
					dot.Width(dot.AnimateTo("w", width, tourGlide)).
						Background(dot.AnimateColor("ink", ink, tourFade))
					if dot.Clicked() && !on {
						a.tourGo(i - t.step)
					}
				}
			})
			ui.Spacer(c)
			if t.step == 0 {
				if tourButton(c, "tour-back", i18n.T("跳过"), false).Clicked() {
					a.endTour()
				}
			} else if tourButton(c, "tour-back", i18n.T("上一步"), false).Clicked() {
				a.tourGo(-1)
			}
			next := i18n.T("下一步")
			switch t.step {
			case 0:
				next = i18n.T("开始导览")
			case last:
				next = i18n.T("开始写作")
			}
			if tourButton(c, "tour-next", next, true).AutoFocus().Clicked() {
				a.tourGo(1)
			}
		})
	})
}

// tourButton is one of the card's buttons: the way on filled with the
// accent, the other quiet.
func tourButton(c *ui.Context, key, label string, primary bool) ui.Element {
	t := c.Theme()
	b := ui.Box(c.Key(key)).Height(34).PaddingX(16).Radius(10).Center().Cursor(ui.CursorPointer).
		Focusable().Role(ui.RoleButton).Label(label)
	ink := t.Text.Alpha(0.72)
	switch {
	case primary && b.Pressed():
		b.Background(t.AccentPressed)
	case primary && b.Hovered():
		b.Background(t.AccentHover)
	case primary:
		b.Background(t.Accent)
	case b.Hovered():
		b.Background(t.Text.Alpha(0.06))
	}
	if primary {
		ink = t.AccentText
	}
	b.Children(func() { ui.Text(c, label).FontSize(13).FontWeight(600).TextColor(ink).NoWrap() })
	return b
}
