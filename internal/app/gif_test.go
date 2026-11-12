package app

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"testing"
	"time"
)

func TestGIFFrames(t *testing.T) {
	pal := color.Palette{color.Transparent, color.Black, color.White}
	var g gif.GIF
	for i := 0; i < 3; i++ {
		img := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
		img.SetColorIndex(i, i, 1)
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, []int{5, 0, 20}[i])
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &g); err != nil {
		t.Fatal(err)
	}
	frames, delays, ok := gifFrames(buf.Bytes())
	if !ok || len(frames) != 3 || delays[0] != 50*time.Millisecond || delays[1] != 100*time.Millisecond || delays[2] != 200*time.Millisecond {
		t.Fatal(ok, len(frames), delays)
	}
	still := gif.GIF{Image: g.Image[:1], Delay: g.Delay[:1]}
	buf.Reset()
	gif.EncodeAll(&buf, &still)
	if _, _, ok := gifFrames(buf.Bytes()); ok {
		t.Fatal("a still GIF is not animated")
	}
	if _, _, ok := gifFrames([]byte("\x89PNG")); ok {
		t.Fatal("not a GIF")
	}
}
