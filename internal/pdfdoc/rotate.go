package pdfdoc

import "image"

// rotateRGBA rotates src clockwise by 0, 90, 180, or 270 degrees.
func rotateRGBA(src *image.RGBA, deg int) *image.RGBA {
	if src == nil {
		return nil
	}
	deg = ((deg % 360) + 360) % 360
	if deg == 0 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	switch deg {
	case 90:
		dst := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := range h {
			srow := src.Pix[y*src.Stride:]
			dx := h - 1 - y
			for x := range w {
				so := x * 4
				do := x*dst.Stride + dx*4
				copy(dst.Pix[do:do+4], srow[so:so+4])
			}
		}
		return dst
	case 180:
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := range h {
			srow := src.Pix[y*src.Stride:]
			drow := dst.Pix[(h-1-y)*dst.Stride:]
			for x := range w {
				so := x * 4
				do := (w - 1 - x) * 4
				copy(drow[do:do+4], srow[so:so+4])
			}
		}
		return dst
	case 270:
		dst := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := range h {
			srow := src.Pix[y*src.Stride:]
			for x := range w {
				so := x * 4
				dy := w - 1 - x
				do := dy*dst.Stride + y*4
				copy(dst.Pix[do:do+4], srow[so:so+4])
			}
		}
		return dst
	default:
		return src
	}
}

// RotateNormRect rotates a normalized [0..1] top-left highlight box clockwise
// by extraRotate degrees (0, 90, 180, 270).
func RotateNormRect(r NormRect, extraRotate int) NormRect {
	rot := ((extraRotate % 360) + 360) % 360
	switch rot {
	case 90:
		return NormRect{
			X: 1.0 - (r.Y + r.H),
			Y: r.X,
			W: r.H,
			H: r.W,
		}
	case 180:
		return NormRect{
			X: 1.0 - (r.X + r.W),
			Y: 1.0 - (r.Y + r.H),
			W: r.W,
			H: r.H,
		}
	case 270:
		return NormRect{
			X: r.Y,
			Y: 1.0 - (r.X + r.W),
			W: r.H,
			H: r.W,
		}
	default:
		return r
	}
}
