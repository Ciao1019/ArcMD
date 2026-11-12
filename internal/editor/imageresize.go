package editor

import (
	"fmt"
	"math"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

type imageResize struct {
	id    string
	width float32
}

func (e *Editor) imageHandles(c *ui.Context, r row) {
	if e.ReadOnly || r.picture == nil || r.picture.resource.Bitmap == nil {
		return
	}
	id := r.leaf.Block.ID
	rect := r.picture.bounds
	if e.imageResize.id != id && !e.hovered(rect, 12) && e.LeafAt(e.Caret).Block.ID != id {
		return
	}
	for _, side := range []int{-1, 1} {
		x := rect.X - 5
		if side > 0 {
			x = rect.X + rect.W - 5
		}
		h := ui.Box(c.Key(fmt.Sprintf("resize-image:%s:%d", id, side))).Absolute().Left(x).Top(rect.Y+rect.H/2-18).Size(10, 36).
			Focusable().Role(ui.RoleSlider).Label(i18n.T("调整图片宽度")).Value(fmt.Sprintf("%.0f", rect.W)).Cursor(ui.CursorResizeEW).
			Radius(5).Background(e.page.Popover).Border(1, c.Theme().Accent)
		if h.FocusVisible() {
			h.Border(2, c.Theme().Accent)
		}
		if dx, _, held := h.Dragged(); held {
			if e.imageResize.id != id {
				e.imageResize = imageResize{id, rect.W}
			}
			e.imageResize.width = max(48, min(r.rect.W, e.imageResize.width+float32(side)*2*dx))
			e.ready = false
		}
		if _, _, ended := h.DragEnded(); ended && e.imageResize.id == id {
			e.SetImageWidth(id, e.imageResize.width)
			e.imageResize = imageResize{}
			e.ready = false
		}
		h.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputKeyDown {
				return false
			}
			switch ev.Key {
			case ui.KeyLeft, ui.KeyDown:
				e.SetImageWidth(id, max(48, rect.W-10))
			case ui.KeyRight, ui.KeyUp:
				e.SetImageWidth(id, min(r.rect.W, rect.W+10))
			case ui.KeyHome:
				e.SetImageWidth(id, 0)
			case ui.KeyEscape:
				e.imageResize = imageResize{}
				e.ready = false
			default:
				return false
			}
			return true
		})
	}
}

func (v *imageViewer) scaleFor(w, h float32) float32 {
	if v.zoom > 0 {
		return v.zoom
	}
	if v.actual {
		return 1
	}
	iw, ih := v.bitmap.Size()
	return min(float32(1), w/float32(max(1, iw)), h/float32(max(1, ih)))
}
func (v *imageViewer) zoomAt(scale, x, y, w, h float32) {
	old := v.scaleFor(w, h)
	next := max(float32(.05), min(8, scale))
	ratio := next / old
	v.pan.X = (v.pan.X-(x-w/2))*ratio + (x - w/2)
	v.pan.Y = (v.pan.Y-(y-h/2))*ratio + (y - h/2)
	v.zoom, v.actual = next, next == 1
	v.clampPan(w, h)
}
func (v *imageViewer) clampPan(w, h float32) {
	iw, ih := v.bitmap.Size()
	scale := v.scaleFor(w, h)
	x, y := max(0, (float32(iw)*scale-w)/2), max(0, (float32(ih)*scale-h)/2)
	v.pan.X = max(-x, min(x, v.pan.X))
	v.pan.Y = max(-y, min(y, v.pan.Y))
}
func (e *Editor) imageCanvas(c *ui.Context, w, h float32) {
	v := &e.imageViewer
	canvas := ui.Box(c.Key("preview-canvas")).Size(w, h).Clip().Focusable().Role(ui.RoleImage).Label(i18n.T("图片预览画布：滚轮缩放，拖动平移")).Cursor(ui.CursorGrab)
	if dx, dy, held := canvas.Dragged(); held {
		v.pan.X += dx
		v.pan.Y += dy
		v.clampPan(w, h)
		canvas.Cursor(ui.CursorGrabbing)
	}
	canvas.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind == ui.InputZoom {
			v.zoomAt(v.scaleFor(w, h)*ev.Scale, ev.X, ev.Y, w, h)
			return true
		}
		if ev.Kind == ui.InputScroll {
			v.zoomAt(v.scaleFor(w, h)*float32(math.Exp(-float64(ev.DY)*.003)), ev.X, ev.Y, w, h)
			return true
		}
		if ev.Kind == ui.InputKeyDown {
			switch ev.Key {
			case ui.KeyEqual:
				v.zoomAt(v.scaleFor(w, h)*1.15, w/2, h/2, w, h)
			case ui.KeyMinus:
				v.zoomAt(v.scaleFor(w, h)/1.15, w/2, h/2, w, h)
			case ui.KeyLeft:
				v.pan.X += 32
			case ui.KeyRight:
				v.pan.X -= 32
			case ui.KeyUp:
				v.pan.Y += 32
			case ui.KeyDown:
				v.pan.Y -= 32
			case ui.Key0:
				v.zoom, v.actual, v.pan = 0, false, ui.Point{}
			default:
				return false
			}
			v.clampPan(w, h)
			return true
		}
		return false
	})
	canvas.Draw(func(p *ui.Painter, r ui.Rect) {
		v.clampPan(w, h)
		scale := v.scaleFor(w, h)
		iw, ih := v.bitmap.Size()
		pw, ph := float32(iw)*scale, float32(ih)*scale
		p.Image(v.bitmap, ui.Rect{X: r.X + (w-pw)/2 + v.pan.X, Y: r.Y + (h-ph)/2 + v.pan.Y, W: pw, H: ph}, ui.Contain)
	})
}
