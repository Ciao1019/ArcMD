package app

import (
	"bytes"
	"image"
	"image/draw"
	"image/gif"
	"time"

	"github.com/egoist/mygo/ui"
)

// GIF limits: larger animations show their first frame only, as one would
// hold every frame in memory.
const (
	gifMaxFrames = 300
	gifMaxPixels = 64 << 20
)

// gifFrames composes the frames of an animated GIF as a viewer shows them,
// honoring each frame's disposal, with their delays (10 ms at least, as
// browsers clamp faster ones). It reports false for a still picture, one
// that is not a GIF, or one too large to animate.
func gifFrames(data []byte) ([]*ui.Bitmap, []time.Duration, bool) {
	if !bytes.HasPrefix(data, []byte("GIF8")) {
		return nil, nil, false
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil || len(g.Image) < 2 || len(g.Image) > gifMaxFrames {
		return nil, nil, false
	}
	w, h := g.Config.Width, g.Config.Height
	if w <= 0 || h <= 0 || w*h*len(g.Image) > gifMaxPixels {
		return nil, nil, false
	}
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	var frames []*ui.Bitmap
	var delays []time.Duration
	for i, frame := range g.Image {
		var previous *image.RGBA
		disposal := byte(0)
		if i < len(g.Disposal) {
			disposal = g.Disposal[i]
		}
		if disposal == gif.DisposalPrevious {
			previous = image.NewRGBA(canvas.Bounds())
			copy(previous.Pix, canvas.Pix)
		}
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
		shot := image.NewRGBA(canvas.Bounds())
		copy(shot.Pix, canvas.Pix)
		frames = append(frames, ui.NewBitmap(shot))
		delay := 100 * time.Millisecond
		if i < len(g.Delay) && g.Delay[i] > 1 {
			delay = time.Duration(g.Delay[i]) * 10 * time.Millisecond
		}
		delays = append(delays, delay)
		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, frame.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			copy(canvas.Pix, previous.Pix)
		}
	}
	return frames, delays, true
}
