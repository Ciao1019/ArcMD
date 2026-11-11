package app

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo/ui"
)

// The README's animations of space themes and bookmarks are frames of the
// window as ArcMD draws it, laid on a desktop the window's material blurs,
// and cross-faded by CSS inside an SVG, so they play wherever the README
// shows. Set ARCMD_README to the folder to write them to:
//
//	ARCMD_README=docs/images/readme go test -run ReadmeArt ./internal/app

const (
	artCanvasW, artCanvasH = 1280, 872
	artWinX, artWinY       = 40, 24
	artWinW, artWinH       = 1200, 760
	artWinRadius           = 12
	artCaptionY            = 814
)

// artFrame is a frame of an animation: the picture, how long it holds,
// and which caption is lit while it shows.
type artFrame struct {
	img     *image.RGBA
	hold    float64
	caption int
}

// artPointer is the mouse's arrow drawn into a frame, in window DIPs; a
// zero one draws none.
type artPointer struct {
	x, y float32
	on   bool
}

func TestReadmeArt(t *testing.T) {
	dir := os.Getenv("ARCMD_README")
	if dir == "" {
		t.Skip("set ARCMD_README to render the README's animations")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	desk := artDesktop()
	blurred := artBlur(desk, 22)

	themes := artThemes(t, desk, blurred)
	writeArt(t, filepath.Join(dir, "themes-zh-CN.svg"), themes, "ArcMD 的空间主题：在主题卡上换颜色，加到三种颜色的渐变，降低强度让窗口透出背后模糊的桌面，加上颗粒，再切到深色；整个窗口实时预览",
		[]string{"换一种颜色", "多色渐变", "透明与模糊", "颗粒质感", "深色外观"})
	writeArt(t, filepath.Join(dir, "themes-en.svg"), themes, "ArcMD space themes: pick a color on the theme card, blend up to three colors, lower the intensity so the window shows the blurred desktop behind it, add grain, then switch to dark; the whole window previews live",
		[]string{"Pick a color", "Multi-color gradient", "Translucency & blur", "Grain", "Dark appearance"})

	marks := artBookmarks(t, desk, blurred)
	writeArt(t, filepath.Join(dir, "bookmarks-zh-CN.svg"), marks, "ArcMD 的书签：指针移到段落上，页边出现书签；点击加上书签，目录底部列出本文档的书签；按 Shift Command D 打开所有文档的书签列表",
		[]string{"悬停出现书签", "点击添加", "目录里的书签", "⇧⌘D 全部书签"})
	writeArt(t, filepath.Join(dir, "bookmarks-en.svg"), marks, "ArcMD bookmarks: hover a paragraph and a bookmark appears in the margin; click to add it, and the outline lists the document's bookmarks; Shift Command D lists the bookmarks of every document",
		[]string{"Hover a paragraph", "Click to bookmark", "Bookmarks in the outline", "⇧⌘D every bookmark"})
}

// artApp is the app the frames show: a few documents, one pinned and one
// a favorite, the window over a desktop.
func artApp(t *testing.T) (*App, *ui.Tester) {
	t.Helper()
	a, tt := newTestApp(t)
	tt.SetVibrancy(true)
	sp := a.state.ActiveSpace()
	for rel, md := range map[string]string{
		"旅行计划.arcmd":    "# 旅行计划\n\n十月去京都，住在鸭川边。\n\n- 清水寺的清晨\n- 哲学之道\n- 锦市场",
		"工作/周报.arcmd":   "# 周报\n\n本周完成了书签与新手教程。",
		"工作/会议记录.arcmd": "# 会议记录\n\n下周发布 0.0.7。",
	} {
		bookmarkDoc(t, a, tt, rel, md)
	}
	bookmarkDoc(t, a, tt, "读书笔记.arcmd", artReading)
	sp.Pin("旅行计划.arcmd", 0)
	a.state.ToggleFavorite(sp.ID, "读书笔记.arcmd")
	sp.Expanded = append(sp.Expanded, "工作")
	a.saveState()
	a.refreshTree()
	a.open("读书笔记.arcmd")
	settleArt(tt)
	return a, tt
}

const artReading = `# 读书笔记

《瓦尔登湖》第二章，梭罗写他为什么搬到湖边。

## 我为何生活

我到林中去，因为我希望从容地生活，只面对生活中最基本的事实，看看能否学到生活要教给我的东西。

简单，简单，再简单！让你的事情只有两三件，而不是成百上千件。

## 湖

湖是风景中最美丽、最有表情的部分。它是大地的眼睛，望着它，人可以测量自己天性的深浅。

| 季节 | 湖的颜色 |
| --- | --- |
| 春 | 蓝绿 |
| 冬 | 冰面下的深蓝 |

## 结语

只有我们醒着的那一天，才是真正破晓。
`

func settleArt(tt *ui.Tester) {
	tt.Frame()
	tt.Advance(1500 * time.Millisecond)
	tt.Frame()
}

// artThemes edits the space's theme on its card, step by step.
func artThemes(t *testing.T, desk, blurred *image.RGBA) []artFrame {
	a, tt := artApp(t)
	sp := a.state.ActiveSpace()
	a.editSpace(sp)
	settleArt(tt)
	type state struct {
		hue        float64
		n          int
		intensity  float64
		noise      float64
		appearance string
		caption    int
		hold       float64
		point      string // the control under the pointer
	}
	states := []state{
		{345, 1, 0.85, 0.12, space.AppearanceLight, 0, 1.4, "knob"},
		{25, 1, 0.85, 0.12, space.AppearanceLight, 0, 0.9, "knob"},
		{95, 1, 0.85, 0.12, space.AppearanceLight, 0, 0.9, "knob"},
		{185, 1, 0.85, 0.12, space.AppearanceLight, 0, 1.0, "knob"},
		{185, 2, 0.85, 0.12, space.AppearanceLight, 1, 1.1, "more"},
		{185, 3, 0.85, 0.12, space.AppearanceLight, 1, 1.1, "more"},
		{240, 3, 0.85, 0.12, space.AppearanceLight, 1, 0.9, "knob"},
		{300, 3, 0.85, 0.12, space.AppearanceLight, 1, 1.2, "knob"},
		{205, 1, 0.85, 0.12, space.AppearanceLight, 2, 1.0, "less"},
		{205, 1, 0.5, 0.12, space.AppearanceLight, 2, 0.9, "intensity"},
		{205, 1, 0.12, 0.12, space.AppearanceLight, 2, 1.6, "intensity"},
		{205, 1, 0.55, 0.6, space.AppearanceLight, 3, 0.9, "grain"},
		{205, 1, 0.55, 1, space.AppearanceLight, 3, 1.3, "grain"},
		{300, 3, 0.85, 0.3, space.AppearanceDark, 4, 1.2, "dark"},
		{20, 3, 0.85, 0.3, space.AppearanceDark, 4, 1.5, "knob"},
	}
	var frames []artFrame
	for _, s := range states {
		th := space.Theme{Intensity: s.intensity, Noise: s.noise, Appearance: s.appearance}
		for j := range s.n {
			th.Colors = append(th.Colors, space.WheelColor(s.hue+space.WheelOffset(j), 0.72))
		}
		a.spaceForm.theme = th
		settleArt(tt)
		frames = append(frames, artFrame{img: artCompose(tt.Image(), desk, blurred, artThemePointer(a, tt, s.point)), hold: s.hold, caption: s.caption})
	}
	a.cancelSpaceForm()
	return frames
}

// artThemePointer is where the pointer rests on the theme card's control.
func artThemePointer(a *App, tt *ui.Tester, control string) artPointer {
	find := func(label string) (ui.Rect, bool) { return tt.Find(label) }
	switch control {
	case "knob":
		pad, ok := find("空间颜色")
		if !ok || len(a.spaceForm.theme.Colors) == 0 {
			return artPointer{}
		}
		x, y := knobAt(a.spaceForm.theme.Colors[0])
		s := pad.W / padSize
		return artPointer{pad.X + pad.W/2 + x*s + 4, pad.Y + pad.H/2 + y*s + 6, true}
	case "intensity":
		r, ok := find("强度")
		if !ok {
			return artPointer{}
		}
		v := float32(a.spaceForm.theme.Intensity)
		return artPointer{r.X + 6 + (r.W-12)*v, r.Y + r.H/2 + 4, true}
	}
	label := map[string]string{"more": "增加颜色", "less": "减少颜色", "grain": "颗粒", "dark": "深色"}[control]
	if r, ok := find(label); ok {
		return artPointer{r.X + r.W/2, r.Y + r.H/2, true}
	}
	return artPointer{}
}

// artBookmarks adds two bookmarks with the pointer, then lists them.
func artBookmarks(t *testing.T, desk, blurred *image.RGBA) []artFrame {
	a, tt := artApp(t)
	sp := a.state.ActiveSpace()
	sp.Theme = &space.Theme{Colors: []string{space.WheelColor(330, 0.62), space.WheelColor(330+space.WheelOffset(1), 0.62)}, Intensity: 0.85, Noise: 0.15, Appearance: space.AppearanceLight}
	a.saveState()
	settleArt(tt)
	var frames []artFrame
	shot := func(p artPointer, hold float64, caption int) {
		frames = append(frames, artFrame{img: artCompose(tt.Image(), desk, blurred, p), hold: hold, caption: caption})
	}
	// The middle of a top-level block's first line, in window DIPs.
	blockAt := func(i int) (x, y float32) {
		r, _ := tt.Find("文档")
		id := a.doc.ed.Doc.Blocks[i].ID
		for _, l := range a.doc.ed.Doc.Leaves() {
			if l.Block.ID == id {
				b, _, _ := a.doc.ed.BoundsForRange(ui.TextInputRange{Start: l.Start, End: l.Start})
				return r.X + b.X + 120, r.Y + b.Y + b.H/2 - a.doc.ed.Scroll.Y
			}
		}
		return 0, 0
	}
	mark := func(i int) {
		x, y := blockAt(i)
		tt.Move(x, y)
		settleArt(tt)
		shot(artPointer{x, y, true}, 1.0, 0)
		r, ok := tt.Find("添加书签")
		if !ok {
			t.Fatalf("no bookmark in the margin of block %d: %v", i, tt.Texts())
		}
		mx, my := r.X+r.W/2, r.Y+r.H/2
		tt.Move(mx, my)
		settleArt(tt)
		// The arrow's tip just below the mark, which it would hide.
		px, py := mx+3, my+7
		shot(artPointer{px, py, true}, 0.8, 0)
		tt.ClickAt(mx, my)
		tt.Frame()
		tt.Advance(120 * time.Millisecond)
		tt.Frame()
		shot(artPointer{px, py, true}, 0.7, 1)
		tt.Advance(1500 * time.Millisecond)
		tt.Frame()
		shot(artPointer{px, py, true}, 1.3, 2)
	}
	settleArt(tt)
	shot(artPointer{760, 640, true}, 1.2, 0)
	mark(3)
	mark(6)
	tt.Move(150, 660)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyD)
	settleArt(tt)
	shot(artPointer{}, 2.4, 3)
	return frames
}

// artCompose lays the window on the desktop, the desktop blurred behind
// it as the window's material shows it, with the window's corners and
// shadow, and the pointer over it.
func artCompose(win *image.RGBA, desk, blurred *image.RGBA, p artPointer) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, artCanvasW, artCanvasH))
	draw.Draw(out, out.Bounds(), desk, image.Point{}, draw.Src)
	frame := image.Rect(artWinX, artWinY, artWinX+artWinW, artWinY+artWinH)
	// The shadow: a soft dark edge under the window.
	shadow := image.NewAlpha(out.Bounds())
	artRoundRect(shadow, frame.Add(image.Pt(0, 16)), artWinRadius, 255)
	soft := artBlurAlpha(shadow, 26)
	draw.DrawMask(out, out.Bounds(), image.NewUniform(color.RGBA{0, 0, 0, 255}), image.Point{}, artScaleAlpha(soft, 0.38), image.Point{}, draw.Over)
	mask := image.NewAlpha(out.Bounds())
	artRoundRect(mask, frame, artWinRadius, 255)
	draw.DrawMask(out, out.Bounds(), blurred, image.Point{}, mask, image.Point{}, draw.Over)
	draw.DrawMask(out, frame, win, image.Point{}, mask, frame.Min, draw.Over)
	// A hairline round the window, as macOS draws.
	edge := image.NewAlpha(out.Bounds())
	artRoundRect(edge, frame.Inset(-1), artWinRadius+1, 255)
	artRoundRect(edge, frame, artWinRadius, 0)
	draw.DrawMask(out, out.Bounds(), image.NewUniform(color.RGBA{0, 0, 0, 255}), image.Point{}, artScaleAlpha(edge, 0.18), image.Point{}, draw.Over)
	if p.on {
		artArrow(out, float64(p.x)+artWinX, float64(p.y)+artWinY)
	}
	return out
}

// artDesktop is a desktop picture with hard edges, which show how much
// the window's material blurs it.
func artDesktop() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, artCanvasW, artCanvasH))
	top, bottom := [3]float64{38, 44, 92}, [3]float64{246, 170, 128}
	for y := range artCanvasH {
		f := float64(y) / artCanvasH
		for x := range artCanvasW {
			g := f*0.8 + float64(x)/artCanvasW*0.2
			img.Set(x, y, color.RGBA{uint8(top[0] + (bottom[0]-top[0])*g), uint8(top[1] + (bottom[1]-top[1])*g), uint8(top[2] + (bottom[2]-top[2])*g), 255})
		}
	}
	disc := func(cx, cy, r float64, c color.RGBA) {
		for y := int(cy - r - 1); y <= int(cy+r+1); y++ {
			for x := int(cx - r - 1); x <= int(cx+r+1); x++ {
				if !(image.Point{x, y}.In(img.Bounds())) {
					continue
				}
				d := math.Hypot(float64(x)-cx, float64(y)-cy)
				if k := min(max(r-d+0.5, 0), 1); k > 0 {
					artBlend(img, x, y, c, k)
				}
			}
		}
	}
	disc(1080, 180, 260, color.RGBA{255, 94, 120, 230})
	disc(260, 640, 300, color.RGBA{64, 196, 170, 220})
	disc(760, 520, 190, color.RGBA{255, 205, 86, 235})
	disc(520, 140, 150, color.RGBA{136, 108, 255, 225})
	disc(1180, 760, 160, color.RGBA{90, 160, 255, 220})
	for i := range 14 {
		x0 := float64(i*110 - 300)
		for y := range artCanvasH {
			for w := range 6 {
				x := int(x0 + float64(y)*0.55 + float64(w))
				if x >= 0 && x < artCanvasW {
					artBlend(img, x, y, color.RGBA{255, 255, 255, 255}, 0.16)
				}
			}
		}
	}
	return img
}

func artBlend(img *image.RGBA, x, y int, c color.RGBA, k float64) {
	i := img.PixOffset(x, y)
	a := k * float64(c.A) / 255
	img.Pix[i] = uint8(float64(img.Pix[i])*(1-a) + float64(c.R)*a)
	img.Pix[i+1] = uint8(float64(img.Pix[i+1])*(1-a) + float64(c.G)*a)
	img.Pix[i+2] = uint8(float64(img.Pix[i+2])*(1-a) + float64(c.B)*a)
}

// artBlur blurs img about as a Gaussian of the radius does, by three box
// blurs each way.
func artBlur(img *image.RGBA, radius int) *image.RGBA {
	out := image.NewRGBA(img.Bounds())
	copy(out.Pix, img.Pix)
	r := max(radius/2, 1)
	for range 3 {
		artBoxBlur(out.Pix, out.Rect.Dx(), out.Rect.Dy(), 4, r)
	}
	return out
}

func artBlurAlpha(img *image.Alpha, radius int) *image.Alpha {
	out := image.NewAlpha(img.Bounds())
	copy(out.Pix, img.Pix)
	r := max(radius/2, 1)
	for range 3 {
		artBoxBlur(out.Pix, out.Rect.Dx(), out.Rect.Dy(), 1, r)
	}
	return out
}

// artBoxBlur averages each channel over 2r+1 pixels across, then down.
func artBoxBlur(pix []uint8, w, h, n, r int) {
	line := make([]int, max(w, h)*n)
	pass := func(count, length int, at func(i, j int) int) {
		for i := range count {
			for j := range length {
				for c := range n {
					line[j*n+c] = int(pix[at(i, j)+c])
				}
			}
			for j := range length {
				for c := range n {
					sum := 0
					for k := j - r; k <= j+r; k++ {
						sum += line[min(max(k, 0), length-1)*n+c]
					}
					pix[at(i, j)+c] = uint8(sum / (2*r + 1))
				}
			}
		}
	}
	pass(h, w, func(y, x int) int { return (y*w + x) * n })
	pass(w, h, func(x, y int) int { return (y*w + x) * n })
}

func artScaleAlpha(m *image.Alpha, k float64) *image.Alpha {
	out := image.NewAlpha(m.Bounds())
	for i, v := range m.Pix {
		out.Pix[i] = uint8(float64(v) * k)
	}
	return out
}

// artRoundRect sets the rounded rectangle r of m to v, its edge smoothed.
func artRoundRect(m *image.Alpha, r image.Rectangle, radius float64, v uint8) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if !(image.Point{x, y}.In(m.Bounds())) {
				continue
			}
			px, py := float64(x)+0.5, float64(y)+0.5
			cx := min(max(px, float64(r.Min.X)+radius), float64(r.Max.X)-radius)
			cy := min(max(py, float64(r.Min.Y)+radius), float64(r.Max.Y)-radius)
			k := min(max(radius-math.Hypot(px-cx, py-cy)+0.5, 0), 1)
			if k <= 0 {
				continue
			}
			i := m.PixOffset(x, y)
			m.Pix[i] = uint8(float64(m.Pix[i])*(1-k) + float64(v)*k)
		}
	}
}

// artArrow draws the macOS pointer with its tip at x, y.
func artArrow(img *image.RGBA, x, y float64) {
	shape := [][2]float64{{0, 0}, {0, 17}, {4.6, 12.8}, {7.6, 19.4}, {10.4, 18.2}, {7.4, 11.7}, {13.2, 11.7}}
	scale := 1.15
	inside := func(px, py float64, grow float64) bool {
		// The point is inside the polygon grown by grow, tested on its
		// edges' distances for the outline.
		in := false
		n := len(shape)
		minD := math.Inf(1)
		for i := range n {
			ax, ay := x+shape[i][0]*scale, y+shape[i][1]*scale
			bx, by := x+shape[(i+1)%n][0]*scale, y+shape[(i+1)%n][1]*scale
			if (ay > py) != (by > py) && px < (bx-ax)*(py-ay)/(by-ay)+ax {
				in = !in
			}
			dx, dy := bx-ax, by-ay
			tt := min(max(((px-ax)*dx+(py-ay)*dy)/(dx*dx+dy*dy), 0), 1)
			minD = min(minD, math.Hypot(px-ax-tt*dx, py-ay-tt*dy))
		}
		return in || minD <= grow
	}
	for py := int(y) - 3; py < int(y)+28; py++ {
		for px := int(x) - 3; px < int(x)+20; px++ {
			if !(image.Point{px, py}.In(img.Bounds())) {
				continue
			}
			var outer, inner float64
			for sy := range 3 {
				for sx := range 3 {
					fx, fy := float64(px)+(float64(sx)+0.5)/3, float64(py)+(float64(sy)+0.5)/3
					if inside(fx, fy, 1.4) {
						outer++
					}
					if inside(fx, fy, 0) {
						inner++
					}
				}
			}
			if outer > 0 {
				artBlend(img, px, py, color.RGBA{255, 255, 255, 255}, outer/9)
			}
			if inner > 0 {
				artBlend(img, px, py, color.RGBA{0, 0, 0, 255}, inner/9)
			}
		}
	}
}

// writeArt writes the frames as an SVG that cross-fades them in a loop,
// with the captions under the window lit in turn.
func writeArt(t *testing.T, file string, frames []artFrame, label string, captions []string) {
	t.Helper()
	const fade = 0.45
	total := 0.0
	starts := make([]float64, len(frames)+1)
	for i, f := range frames {
		starts[i] = total
		total += f.hold + fade
	}
	starts[len(frames)] = total
	pct := func(s float64) string { return fmt.Sprintf("%.3f%%", min(max(s/total*100, 0), 100)) }
	var css, body strings.Builder
	fmt.Fprintf(&css, ".f { animation: %.2fs linear infinite both; opacity: 0; }\n", total)
	fmt.Fprintf(&css, ".c { animation: %.2fs ease-in-out infinite both; }\n", total)
	css.WriteString(".f0 { opacity: 1; }\n")
	css.WriteString("@media (prefers-reduced-motion: reduce) { .f, .c { animation: none; } }\n")
	// Frame 0 lies under the rest. Frame i fades in over the one before
	// from its start and stays until the next has faded in over it; the
	// last stays to the end, when a copy of frame 0 fades in over it, so
	// the loop has no seam.
	for i := 1; i < len(frames); i++ {
		in, full, out := starts[i], starts[i]+fade, starts[i+1]+fade
		end := fmt.Sprintf("%s, 100%% { opacity: 0 }", pct(out))
		if i == len(frames)-1 {
			end = "100% { opacity: 1 }"
		}
		fmt.Fprintf(&css, "@keyframes f%d { 0%%, %s { opacity: 0 } %s, %s { opacity: 1 } %s }\n", i, pct(in), pct(full), pct(out-0.01), end)
	}
	fmt.Fprintf(&css, "@keyframes fz { 0%%, %s { opacity: 0 } 100%% { opacity: 1 } }\n", pct(total-fade))
	// A caption is lit while a frame of it shows, fading as the frames do.
	level := func(i, k int) string {
		if frames[i%len(frames)].caption == k {
			return "1"
		}
		return ".42"
	}
	for k := range captions {
		var steps []string
		for i := 1; i <= len(frames); i++ {
			at := starts[i]
			if i == len(frames) {
				at = total - fade
			}
			steps = append(steps, fmt.Sprintf("%s { opacity: %s } %s { opacity: %s }", pct(at), level(i-1, k), pct(at+fade), level(i, k)))
		}
		fmt.Fprintf(&css, "@keyframes c%d { 0%% { opacity: %s } %s }\n", k, level(0, k), strings.Join(steps, " "))
	}
	enc := func(img *image.RGBA) string {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
			t.Fatal(err)
		}
		return base64.StdEncoding.EncodeToString(buf.Bytes())
	}
	if dir := os.Getenv("ARCMD_README_FRAMES"); dir != "" {
		for i, f := range frames {
			out, err := os.Create(filepath.Join(dir, fmt.Sprintf("%s-%02d.jpg", strings.TrimSuffix(filepath.Base(file), ".svg"), i)))
			if err != nil {
				t.Fatal(err)
			}
			jpeg.Encode(out, f.img, &jpeg.Options{Quality: 80})
			out.Close()
		}
	}
	for i, f := range frames {
		if i == 0 {
			fmt.Fprintf(&body, `<image id="first" class="f0" width="%d" height="%d" href="data:image/jpeg;base64,%s"/>`+"\n", artCanvasW, artCanvasH, enc(f.img))
			continue
		}
		fmt.Fprintf(&body, `<image class="f" style="animation-name:f%d" width="%d" height="%d" href="data:image/jpeg;base64,%s"/>`+"\n", i, artCanvasW, artCanvasH, enc(f.img))
	}
	fmt.Fprintf(&body, `<use class="f" style="animation-name:fz" href="#first"/>`+"\n")
	// The captions: pills spread under the window.
	pills := make([]float64, len(captions))
	width := 0.0
	for i, c := range captions {
		pills[i] = artTextWidth(c)*14 + 36
		width += pills[i]
	}
	gap := 12.0
	x := (artCanvasW - width - gap*float64(len(captions)-1)) / 2
	for i, c := range captions {
		fmt.Fprintf(&body, `<g class="c" style="animation-name:c%d"><rect x="%.1f" y="%d" width="%.1f" height="38" rx="19" fill="#ffffff" fill-opacity=".9"/><text x="%.1f" y="%d" text-anchor="middle" class="t">%s</text></g>`+"\n",
			i, x, artCaptionY, pills[i], x+pills[i]/2, artCaptionY+24, svgEscape(c))
		x += pills[i] + gap
	}
	firstID := body.String()
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 %d %d" width="%d" height="%d" role="img" aria-label="%s">
<style>
.t { font-family: -apple-system, BlinkMacSystemFont, "SF Pro Text", "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", "Noto Sans CJK SC", Helvetica, Arial, sans-serif; font-size: 14px; font-weight: 600; fill: #1f2329; }
%s</style>
%s</svg>
`, artCanvasW, artCanvasH, artCanvasW, artCanvasH, svgEscape(label), css.String(), firstID)
	if err := os.WriteFile(file, []byte(svg), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %d frames, %.1fs, %d KB", filepath.Base(file), len(frames), total, len(svg)/1024)
}

// artTextWidth is the text's width in ems, a CJK character one and the
// rest about half.
func artTextWidth(s string) float64 {
	w := 0.0
	for _, r := range s {
		if r > 0x2e80 {
			w++
		} else {
			w += 0.56
		}
	}
	return w
}

func svgEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}
