package app

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"

	"github.com/egoist/mygo/ui"
	"golang.org/x/image/vector"
)

// These are views of the original fabric material, not animation frames.
// The native rig selects and interpolates them from its live yaw and pitch;
// eyes, blinking, squash, breathing, roll and jumps are rendered separately.
// Only two adjacent pitch sheets are retained, shared across conversations.
//
//go:embed assets/ghost/pitch*.png
var botMaterials embed.FS

var botMaterialCache struct {
	pitch  [2]int
	images [2]*image.RGBA
}

const botTile = 80
const botYawCount = 72
const botPitchCount = 16

func botMaterial(pitch int) *image.RGBA {
	for i, img := range botMaterialCache.images {
		if img != nil && botMaterialCache.pitch[i] == pitch {
			return img
		}
	}
	data, err := botMaterials.ReadFile(fmt.Sprintf("assets/ghost/pitch%02d.png", pitch))
	if err != nil {
		panic(err)
	} // Embedded, validated build assets.
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		panic(err)
	}
	img := image.NewRGBA(src.Bounds())
	draw.Draw(img, img.Bounds(), src, src.Bounds().Min, draw.Src)
	botMaterialCache.pitch[0], botMaterialCache.images[0] = botMaterialCache.pitch[1], botMaterialCache.images[1]
	botMaterialCache.pitch[1], botMaterialCache.images[1] = pitch, img
	return img
}

type botPainter struct {
	bitmap    *ui.Bitmap
	pixels    [2]*image.RGBA
	buffer    int
	clip, eye *image.Alpha
	raster    vector.Rasterizer
	last      botPose
	ready     bool
}

func (b *botPainter) image(pose botPose) *ui.Bitmap {
	if b.ready && b.last == pose {
		return b.bitmap
	}
	b.last, b.ready = pose, true
	b.buffer ^= 1
	img := b.pixels[b.buffer]
	if img == nil {
		img = image.NewRGBA(image.Rect(0, 0, botTile, botTile))
		b.pixels[b.buffer] = img
	}
	yaw := math.Mod(math.Mod(pose.Yaw/botDeg, 360)+360, 360) / 5
	pitch := botClamp((pose.Pitch/botDeg+45)/5, 0, botPitchCount-1)
	x0, y0 := int(yaw), int(pitch)
	x1, y1 := (x0+1)%botYawCount, min(y0+1, botPitchCount-1)
	fx, fy := yaw-float64(x0), pitch-float64(y0)
	lo, hi := botMaterial(y0), botMaterial(y1)
	weights := [4]float64{(1 - fx) * (1 - fy), fx * (1 - fy), (1 - fx) * fy, fx * fy}
	bases := [4]int{lo.PixOffset(x0%18*botTile, x0/18*botTile), lo.PixOffset(x1%18*botTile, x1/18*botTile), hi.PixOffset(x0%18*botTile, x0/18*botTile), hi.PixOffset(x1%18*botTile, x1/18*botTile)}
	for y := range botTile {
		for x := range botTile * 4 {
			a, c := y*lo.Stride+x, y*hi.Stride+x
			v := float64(lo.Pix[bases[0]+a])*weights[0] + float64(lo.Pix[bases[1]+a])*weights[1] + float64(hi.Pix[bases[2]+c])*weights[2] + float64(hi.Pix[bases[3]+c])*weights[3]
			img.Pix[y*img.Stride+x] = uint8(math.Floor(v + .5))
		}
	}
	b.face(img, pose)
	if b.bitmap == nil {
		b.bitmap = ui.NewBitmap(img)
	} else {
		b.bitmap.Update(img)
	}
	return b.bitmap
}

// The face is the upstream spherical projection and eight-segment eye
// curves, clipped to the turned ghost outline. Both eyes morph continuously.
func (b *botPainter) face(dst *image.RGBA, pose botPose) {
	cy, sy, cp, sp := math.Cos(pose.Yaw), math.Sin(pose.Yaw), math.Cos(pose.Pitch), math.Sin(pose.Pitch)
	facing := cy * cp
	if facing <= -.2 {
		return
	}
	floor := func(v float64) float64 {
		if math.Abs(v) < .22 {
			return math.Copysign(.22, v)
		}
		return v
	}
	cy, cp = floor(cy), floor(cp)
	if b.clip == nil {
		b.clip = image.NewAlpha(dst.Bounds())
		b.eye = image.NewAlpha(dst.Bounds())
	}
	clear(b.clip.Pix)
	z := .55 * 25.88276786624261 // Upstream surfaceAt("ghost", 9.75, 50, 48, 1).
	if facing < 0 {
		z = -z
	}
	clipPoint := func(x, y float64) (float32, float32) {
		return float32(40 + .64*(cy*(x-50)+sy*z)), float32(46.4 + .64*(sy*sp*(x-50)+cp*(y-50)-cy*sp*z))
	}
	r := &b.raster
	r.Reset(botTile, botTile)
	r.MoveTo(clipPoint(17, 50))
	cube := func(a, b, c, d, e, f float64) {
		x1, y1 := clipPoint(a, b)
		x2, y2 := clipPoint(c, d)
		x3, y3 := clipPoint(e, f)
		r.CubeTo(x1, y1, x2, y2, x3, y3)
	}
	quad := func(a, b, c, d float64) {
		x1, y1 := clipPoint(a, b)
		x2, y2 := clipPoint(c, d)
		r.QuadTo(x1, y1, x2, y2)
	}
	cube(17, 31.78, 31.78, 17, 50, 17)
	cube(68.22, 17, 83, 31.78, 83, 50)
	r.LineTo(clipPoint(83, 81.5))
	quad(72, 93.5, 61, 81.5)
	quad(50, 93.5, 39, 81.5)
	quad(28, 93.5, 17, 81.5)
	r.ClosePath()
	r.Draw(b.clip, b.clip.Bounds(), image.Opaque, image.Point{})
	wd, ww, ws := pose.W[0], pose.W[1], pose.W[2]
	past := func(v, d float64) float64 {
		if math.Abs(v) <= d {
			return 0
		}
		return math.Copysign((math.Abs(v)-d)/(1-d), v)
	}
	up := past(botClamp(-pose.Pitch/.26-pose.LookY/7, -1, 1), .34)
	side := math.Abs(past(botClamp(pose.LookX/4.5, -1, 1), .4))
	tall, wide := max(.3, 1+.55*up-.1*side), 1-.05*up+.12*side
	open, laugh := wd+ww*(1-pose.Laugh), ww*pose.Laugh
	lift, sag := max(0, -pose.Y)/26, .5+.5*pose.Breath
	for _, side := range []float64{-1, 1} {
		lid := pose.BlinkL
		if side > 0 {
			lid = pose.BlinkR
		}
		e := botClamp(pose.EyeOpen*(1-lid), 0, 1)
		ko, ks, kl := open*e, open*(1-e), laugh
		x0 := ko*.01 + ks*5.4 + kl*6.2 + ws*6
		y0 := ko*1.1*tall + ks*.6 + kl*(2.2-lift*1.5) + ws*(-1.4+sag)
		control := ko*-3.3*tall + ks*.6 + kl*(-11.4-4*lift) + ws*(5.4+2*sag)
		width := ko*12.6*wide + ks*2.8 + kl*4.4 + ws*4
		dx, dy := pose.LookX*(ko+.5*(ks+kl)), pose.LookY*(ko+.5*ks)
		lon := math.Asin(botClamp((side*12.5+dx)/30, -1, 1)) + pose.Yaw
		lat := math.Asin(botClamp(-(1+dy)/30, -1, 1)) + pose.Pitch
		sx, sy := math.Cos(lon), math.Cos(lat)
		z := sx * sy
		if z <= .02 {
			continue
		}
		cx, cy := 30*math.Sin(lon)*sy, -30*math.Sin(lat)
		point := func(x, y float64) (float32, float32) {
			return float32(40 + .64*.95*(cx+max(.02, sx)*x)), float32(46.4 + .64*(-2+.95*(cy+max(.02, sy)*y)))
		}
		x0 = math.Floor(x0*50+.5) / 50
		y0 = math.Floor(y0*50+.5) / 50
		control = math.Floor(control*50+.5) / 50
		var points [9][2]float64
		points[0] = [2]float64{-x0, y0}
		for i := 1; i <= 8; i++ {
			t := float64(i) / 8
			mt := 1 - t
			points[i] = [2]float64{math.Round((mt*mt*-x0+t*t*x0)*1000) / 1000, math.Round(((mt*mt+t*t)*y0+2*mt*t*control)*1000) / 1000}
		}
		clear(b.eye.Pix)
		r.Reset(botTile, botTile)
		for i, p := range points {
			botEllipse(r, point, p[0], p[1], width/2)
			if i == 0 {
				continue
			}
			prev := points[i-1]
			vx, vy := p[0]-prev[0], p[1]-prev[1]
			l := math.Hypot(vx, vy)
			if l < 1e-9 {
				continue
			}
			nx, ny := -vy/l*width/2, vx/l*width/2
			r.MoveTo(point(prev[0]+nx, prev[1]+ny))
			r.LineTo(point(prev[0]-nx, prev[1]-ny))
			r.LineTo(point(p[0]-nx, p[1]-ny))
			r.LineTo(point(p[0]+nx, p[1]+ny))
			r.ClosePath()
		}
		r.Draw(b.eye, b.eye.Bounds(), image.Opaque, image.Point{})
		alpha := min(1, z*5)
		ink := qaBotInk()
		for i, a := range b.eye.Pix {
			coverage := float64(a) * float64(b.clip.Pix[i]) / 65025 * alpha
			if coverage == 0 {
				continue
			}
			j := i * 4
			dst.Pix[j] = uint8(math.Round(float64(ink.R)*coverage + float64(dst.Pix[j])*(1-coverage)))
			dst.Pix[j+1] = uint8(math.Round(float64(ink.G)*coverage + float64(dst.Pix[j+1])*(1-coverage)))
			dst.Pix[j+2] = uint8(math.Round(float64(ink.B)*coverage + float64(dst.Pix[j+2])*(1-coverage)))
			dst.Pix[j+3] = uint8(math.Round(255*coverage + float64(dst.Pix[j+3])*(1-coverage)))
		}
	}
}

func botEllipse(r *vector.Rasterizer, at func(float64, float64) (float32, float32), x, y, radius float64) {
	const k = .5522847498
	cube := func(a, b, c, d, e, f float64) {
		x1, y1 := at(a, b)
		x2, y2 := at(c, d)
		x3, y3 := at(e, f)
		r.CubeTo(x1, y1, x2, y2, x3, y3)
	}
	r.MoveTo(at(x+radius, y))
	cube(x+radius, y+radius*k, x+radius*k, y+radius, x, y+radius)
	cube(x-radius*k, y+radius, x-radius, y+radius*k, x-radius, y)
	cube(x-radius, y-radius*k, x-radius*k, y-radius, x, y-radius)
	cube(x+radius*k, y-radius, x+radius, y-radius*k, x+radius, y)
	r.ClosePath()
}

func (b *botPainter) draw(p *ui.Painter, r ui.Rect, pose botPose) {
	bitmap := b.image(pose)
	transform := ui.Scaling(float32(pose.SX), float32(pose.SY)).Then(ui.Translation(0, float32(16*(1-pose.SY)))).
		Then(ui.Rotation(float32(pose.Roll / botDeg))).Then(ui.Translation(r.X+16+float32(pose.X*.32), r.Y+16+float32(pose.Y*.32)))
	p.Transform(transform, 0, 0, func() {
		p.ImageRegion(bitmap, ui.Rect{}, ui.Rect{X: -20, Y: -23.2, W: 40, H: 40}, ui.ImageOptions{})
	})
}
