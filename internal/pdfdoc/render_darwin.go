//go:build darwin && !ios

package pdfdoc

import (
	"errors"
	"image"
	"math"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

type nsPoint struct{ X, Y float64 }
type nsSize struct{ Width, Height float64 }
type nsRect struct {
	Origin nsPoint
	Size   nsSize
}

const (
	pdfDisplayBoxMedia = 0
	pdfDisplayBoxCrop  = 1
	nsCaseInsensitive  = 1
)

var (
	darwinOnce  sync.Once
	darwinReady bool

	msgSendFn       uintptr
	poolPushFn      uintptr
	poolPopFn       uintptr
	msgBoundsForBox func(obj objc.ID, sel objc.SEL, box int) nsRect
	msgBoundsPage   func(obj objc.ID, sel objc.SEL, page objc.ID) nsRect

	cgColorSpaceCreateDeviceRGB   func() uintptr
	cgColorSpaceRelease           func(cs uintptr)
	cgBitmapContextCreate         func(data unsafe.Pointer, w, h, bpc, bpr uintptr, cs uintptr, info uint32) uintptr
	cgContextRelease              func(ctx uintptr)
	cgContextSaveGState           func(ctx uintptr)
	cgContextRestoreGState        func(ctx uintptr)
	cgContextSetRGBFillColor      func(ctx uintptr, r, g, b, a float64)
	cgContextBeginPath            func(ctx uintptr)
	cgContextMoveToPoint          func(ctx uintptr, x, y float64)
	cgContextAddLineToPoint       func(ctx uintptr, x, y float64)
	cgContextClosePath            func(ctx uintptr)
	cgContextFillPath             func(ctx uintptr)
	cgContextScaleCTM             func(ctx uintptr, sx, sy float64)
	cgContextSetInterpolation     func(ctx uintptr, q int32)
	cgContextSetAllowsAntialias   func(ctx uintptr, b bool)
	cgContextSetShouldAntialias   func(ctx uintptr, b bool)
	cgContextSetAllowsFontSmooth  func(ctx uintptr, b bool)
	cgContextSetShouldSmoothFonts func(ctx uintptr, b bool)

	clsNSString    objc.Class
	clsNSURL       objc.Class
	clsNSData      objc.Class
	clsPDFDocument objc.Class

	selAlloc              objc.SEL
	selInitWithURL        objc.SEL
	selInitWithData       objc.SEL
	selRelease            objc.SEL
	selRetain             objc.SEL
	selDataUsingEncoding  objc.SEL
	selLength             objc.SEL
	selGetBytesLength     objc.SEL
	selStringWithUTF8     objc.SEL
	selFileURLWithPath    objc.SEL
	selDataWithBytes      objc.SEL
	selIsEncrypted        objc.SEL
	selIsLocked           objc.SEL
	selUnlockWithPassword objc.SEL
	selPageCount          objc.SEL
	selPageAtIndex        objc.SEL
	selIndexForPage       objc.SEL
	selBoundsForBox       objc.SEL
	selRotation           objc.SEL
	selDrawWithBox        objc.SEL
	selString             objc.SEL
	selOutlineRoot        objc.SEL
	selNumChildren        objc.SEL
	selChildAtIndex       objc.SEL
	selLabel              objc.SEL
	selDestination        objc.SEL
	selPage               objc.SEL
	selFindString         objc.SEL
	selCount              objc.SEL
	selObjectAtIndex      objc.SEL
	selPages              objc.SEL
	selBoundsForPage      objc.SEL
)

func initDarwinPDF() bool {
	darwinOnce.Do(func() {
		libObjC, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			return
		}
		if _, err := purego.Dlopen("/System/Library/Frameworks/Foundation.framework/Foundation", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
			return
		}
		libCG, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			return
		}
		if _, err := purego.Dlopen("/System/Library/Frameworks/PDFKit.framework/PDFKit", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
			if _, err2 := purego.Dlopen("/System/Library/Frameworks/Quartz.framework/Frameworks/PDFKit.framework/PDFKit", purego.RTLD_GLOBAL|purego.RTLD_NOW); err2 != nil {
				return
			}
		}

		msgSendFn, err = purego.Dlsym(libObjC, "objc_msgSend")
		if err != nil {
			return
		}
		stretFn := msgSendFn
		if runtime.GOARCH == "amd64" {
			if s, err := purego.Dlsym(libObjC, "objc_msgSend_stret"); err == nil {
				stretFn = s
			}
		}
		poolPushFn, err = purego.Dlsym(libObjC, "objc_autoreleasePoolPush")
		if err != nil {
			return
		}
		poolPopFn, err = purego.Dlsym(libObjC, "objc_autoreleasePoolPop")
		if err != nil {
			return
		}

		purego.RegisterFunc(&msgBoundsForBox, stretFn)
		purego.RegisterFunc(&msgBoundsPage, stretFn)

		purego.RegisterLibFunc(&cgColorSpaceCreateDeviceRGB, libCG, "CGColorSpaceCreateDeviceRGB")
		purego.RegisterLibFunc(&cgColorSpaceRelease, libCG, "CGColorSpaceRelease")
		purego.RegisterLibFunc(&cgBitmapContextCreate, libCG, "CGBitmapContextCreate")
		purego.RegisterLibFunc(&cgContextRelease, libCG, "CGContextRelease")
		purego.RegisterLibFunc(&cgContextSaveGState, libCG, "CGContextSaveGState")
		purego.RegisterLibFunc(&cgContextRestoreGState, libCG, "CGContextRestoreGState")
		purego.RegisterLibFunc(&cgContextSetRGBFillColor, libCG, "CGContextSetRGBFillColor")
		purego.RegisterLibFunc(&cgContextBeginPath, libCG, "CGContextBeginPath")
		purego.RegisterLibFunc(&cgContextMoveToPoint, libCG, "CGContextMoveToPoint")
		purego.RegisterLibFunc(&cgContextAddLineToPoint, libCG, "CGContextAddLineToPoint")
		purego.RegisterLibFunc(&cgContextClosePath, libCG, "CGContextClosePath")
		purego.RegisterLibFunc(&cgContextFillPath, libCG, "CGContextFillPath")
		purego.RegisterLibFunc(&cgContextScaleCTM, libCG, "CGContextScaleCTM")
		purego.RegisterLibFunc(&cgContextSetInterpolation, libCG, "CGContextSetInterpolationQuality")
		purego.RegisterLibFunc(&cgContextSetAllowsAntialias, libCG, "CGContextSetAllowsAntialiasing")
		purego.RegisterLibFunc(&cgContextSetShouldAntialias, libCG, "CGContextSetShouldAntialias")
		purego.RegisterLibFunc(&cgContextSetAllowsFontSmooth, libCG, "CGContextSetAllowsFontSmoothing")
		purego.RegisterLibFunc(&cgContextSetShouldSmoothFonts, libCG, "CGContextSetShouldSmoothFonts")

		clsNSString = objc.GetClass("NSString")
		clsNSURL = objc.GetClass("NSURL")
		clsNSData = objc.GetClass("NSData")
		clsPDFDocument = objc.GetClass("PDFDocument")
		if clsNSString == 0 || clsNSURL == 0 || clsNSData == 0 || clsPDFDocument == 0 {
			return
		}

		selAlloc = objc.RegisterName("alloc")
		selInitWithURL = objc.RegisterName("initWithURL:")
		selInitWithData = objc.RegisterName("initWithData:")
		selRelease = objc.RegisterName("release")
		selRetain = objc.RegisterName("retain")
		selDataUsingEncoding = objc.RegisterName("dataUsingEncoding:")
		selLength = objc.RegisterName("length")
		selGetBytesLength = objc.RegisterName("getBytes:length:")
		selStringWithUTF8 = objc.RegisterName("stringWithUTF8String:")
		selFileURLWithPath = objc.RegisterName("fileURLWithPath:")
		selDataWithBytes = objc.RegisterName("dataWithBytes:length:")
		selIsEncrypted = objc.RegisterName("isEncrypted")
		selIsLocked = objc.RegisterName("isLocked")
		selUnlockWithPassword = objc.RegisterName("unlockWithPassword:")
		selPageCount = objc.RegisterName("pageCount")
		selPageAtIndex = objc.RegisterName("pageAtIndex:")
		selIndexForPage = objc.RegisterName("indexForPage:")
		selBoundsForBox = objc.RegisterName("boundsForBox:")
		selRotation = objc.RegisterName("rotation")
		selDrawWithBox = objc.RegisterName("drawWithBox:toContext:")
		selString = objc.RegisterName("string")
		selOutlineRoot = objc.RegisterName("outlineRoot")
		selNumChildren = objc.RegisterName("numberOfChildren")
		selChildAtIndex = objc.RegisterName("childAtIndex:")
		selLabel = objc.RegisterName("label")
		selDestination = objc.RegisterName("destination")
		selPage = objc.RegisterName("page")
		selFindString = objc.RegisterName("findString:withOptions:")
		selCount = objc.RegisterName("count")
		selObjectAtIndex = objc.RegisterName("objectAtIndex:")
		selPages = objc.RegisterName("pages")
		selBoundsForPage = objc.RegisterName("boundsForPage:")

		darwinReady = true
	})
	return darwinReady
}

//go:uintptrescapes
func objcCall(obj uintptr, s objc.SEL, args ...uintptr) uintptr {
	var a [8]uintptr
	a[0], a[1] = obj, uintptr(s)
	n := copy(a[2:], args)
	r, _, _ := purego.SyscallN(msgSendFn, a[:n+2]...)
	return r
}

func withAutoreleasePool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool, _, _ := purego.SyscallN(poolPushFn)
	defer purego.SyscallN(poolPopFn, pool)
	fn()
}

func makeNSString(s string) objc.ID {
	b := append([]byte(s), 0)
	id := objc.ID(objcCall(uintptr(clsNSString), selStringWithUTF8, uintptr(unsafe.Pointer(&b[0]))))
	runtime.KeepAlive(b)
	return id
}

func fromNSString(ns objc.ID) string {
	if ns == 0 {
		return ""
	}
	const nsUTF8StringEncoding = 4
	data := objcCall(uintptr(ns), selDataUsingEncoding, nsUTF8StringEncoding)
	if data == 0 {
		return ""
	}
	const maxLen = 16 << 20
	n := int(objcCall(data, selLength))
	if n <= 0 {
		return ""
	}
	if n > maxLen {
		n = maxLen
	}
	buf := make([]byte, n)
	objcCall(data, selGetBytesLength, uintptr(unsafe.Pointer(&buf[0])), uintptr(n))
	runtime.KeepAlive(buf)
	return string(buf)
}

type darwinPDF struct {
	mu  sync.Mutex
	doc objc.ID
	cnt int
}

func openPlatformFile(path string) (platformDoc, error) {
	if !initDarwinPDF() {
		return nil, errors.New("pdfkit unavailable")
	}
	var out *darwinPDF
	var retErr error
	withAutoreleasePool(func() {
		nsPath := makeNSString(path)
		if nsPath == 0 {
			retErr = ErrInvalid
			return
		}
		nsURL := objc.ID(objcCall(uintptr(clsNSURL), selFileURLWithPath, uintptr(nsPath)))
		if nsURL == 0 {
			retErr = ErrInvalid
			return
		}
		rawDoc := objcCall(uintptr(clsPDFDocument), selAlloc)
		pdfDoc := objc.ID(objcCall(rawDoc, selInitWithURL, uintptr(nsURL)))
		out, retErr = wrapDarwinPDF(pdfDoc)
	})
	return out, retErr
}

func openPlatformBytes(data []byte) (platformDoc, error) {
	if !initDarwinPDF() {
		return nil, errors.New("pdfkit unavailable")
	}
	if len(data) == 0 {
		return nil, ErrInvalid
	}
	var out *darwinPDF
	var retErr error
	withAutoreleasePool(func() {
		nsData := objc.ID(objcCall(uintptr(clsNSData), selDataWithBytes, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data))))
		runtime.KeepAlive(data)
		if nsData == 0 {
			retErr = ErrInvalid
			return
		}
		rawDoc := objcCall(uintptr(clsPDFDocument), selAlloc)
		pdfDoc := objc.ID(objcCall(rawDoc, selInitWithData, uintptr(nsData)))
		out, retErr = wrapDarwinPDF(pdfDoc)
	})
	return out, retErr
}

func wrapDarwinPDF(pdfDoc objc.ID) (*darwinPDF, error) {
	if pdfDoc == 0 {
		return nil, ErrInvalid
	}
	locked := byte(objcCall(uintptr(pdfDoc), selIsLocked)) != 0
	if locked {
		empty := makeNSString("")
		unlocked := byte(objcCall(uintptr(pdfDoc), selUnlockWithPassword, uintptr(empty))) != 0
		if !unlocked {
			objcCall(uintptr(pdfDoc), selRelease)
			return nil, ErrEncrypted
		}
	}
	cnt := int(objcCall(uintptr(pdfDoc), selPageCount))
	if cnt <= 0 {
		objcCall(uintptr(pdfDoc), selRelease)
		return nil, ErrInvalid
	}
	return &darwinPDF{doc: pdfDoc, cnt: cnt}, nil
}

func (d *darwinPDF) PageCount() int {
	return d.cnt
}

func (d *darwinPDF) PageInfo(page int) (PageInfo, bool) {
	if page < 0 || page >= d.cnt {
		return PageInfo{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.doc == 0 {
		return PageInfo{}, false
	}
	var info PageInfo
	var ok bool
	withAutoreleasePool(func() {
		pg := objc.ID(objcCall(uintptr(d.doc), selPageAtIndex, uintptr(page)))
		if pg == 0 {
			return
		}
		rect := msgBoundsForBox(pg, selBoundsForBox, pdfDisplayBoxCrop)
		if rect.Size.Width <= 0 || rect.Size.Height <= 0 {
			rect = msgBoundsForBox(pg, selBoundsForBox, pdfDisplayBoxMedia)
		}
		rot := int(objcCall(uintptr(pg), selRotation))
		rot = ((rot % 360) + 360) % 360
		w, h := rect.Size.Width, rect.Size.Height
		if rot == 90 || rot == 270 {
			w, h = h, w
		}
		if w > 0 && h > 0 {
			info = PageInfo{Width: w, Height: h, Rotation: rot}
			ok = true
		}
	})
	return info, ok
}

func (d *darwinPDF) RenderPage(page int, widthPx, heightPx int, extraRotate int) (*image.RGBA, error) {
	if page < 0 || page >= d.cnt {
		return nil, ErrInvalid
	}
	rot := ((extraRotate % 360) + 360) % 360
	baseW, baseH := widthPx, heightPx
	if rot == 90 || rot == 270 {
		baseW, baseH = heightPx, widthPx
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.doc == 0 {
		return nil, errors.New("pdfdoc: document closed")
	}

	var outImg *image.RGBA
	var outErr error
	withAutoreleasePool(func() {
		pg := objc.ID(objcCall(uintptr(d.doc), selPageAtIndex, uintptr(page)))
		if pg == 0 {
			outErr = ErrInvalid
			return
		}
		box := pdfDisplayBoxCrop
		rect := msgBoundsForBox(pg, selBoundsForBox, box)
		if rect.Size.Width <= 0 || rect.Size.Height <= 0 {
			box = pdfDisplayBoxMedia
			rect = msgBoundsForBox(pg, selBoundsForBox, box)
		}
		pageRot := int(objcCall(uintptr(pg), selRotation))
		pageRot = ((pageRot % 360) + 360) % 360
		dispW, dispH := rect.Size.Width, rect.Size.Height
		if pageRot == 90 || pageRot == 270 {
			dispW, dispH = dispH, dispW
		}
		if dispW <= 0 || dispH <= 0 {
			outErr = ErrInvalid
			return
		}

		rgba := image.NewRGBA(image.Rect(0, 0, baseW, baseH))
		cs := cgColorSpaceCreateDeviceRGB()
		if cs == 0 {
			outErr = errors.New("pdfdoc: cannot create RGB color space")
			return
		}
		defer cgColorSpaceRelease(cs)

		// kCGImageAlphaPremultipliedLast (1) | kCGBitmapByteOrder32Big (4 << 12)
		const bitmapInfo = 1 | (4 << 12)
		ctx := cgBitmapContextCreate(
			unsafe.Pointer(&rgba.Pix[0]),
			uintptr(baseW),
			uintptr(baseH),
			8,
			uintptr(rgba.Stride),
			cs,
			bitmapInfo,
		)
		if ctx == 0 {
			outErr = errors.New("pdfdoc: cannot create bitmap context")
			return
		}
		defer cgContextRelease(ctx)

		// Fill crisp white paper background.
		cgContextSetRGBFillColor(ctx, 1, 1, 1, 1)
		cgContextBeginPath(ctx)
		cgContextMoveToPoint(ctx, 0, 0)
		cgContextAddLineToPoint(ctx, float64(baseW), 0)
		cgContextAddLineToPoint(ctx, float64(baseW), float64(baseH))
		cgContextAddLineToPoint(ctx, 0, float64(baseH))
		cgContextClosePath(ctx)
		cgContextFillPath(ctx)

		cgContextSaveGState(ctx)
		cgContextSetInterpolation(ctx, 3) // kCGInterpolationHigh
		cgContextSetAllowsAntialias(ctx, true)
		cgContextSetShouldAntialias(ctx, true)
		cgContextSetAllowsFontSmooth(ctx, true)
		cgContextSetShouldSmoothFonts(ctx, true)
		cgContextScaleCTM(ctx, float64(baseW)/dispW, float64(baseH)/dispH)
		objcCall(uintptr(pg), selDrawWithBox, uintptr(box), ctx)
		cgContextRestoreGState(ctx)

		runtime.KeepAlive(rgba)
		outImg = rotateRGBA(rgba, rot)
	})
	return outImg, outErr
}

func (d *darwinPDF) PageText(page int) (string, bool) {
	if page < 0 || page >= d.cnt {
		return "", false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.doc == 0 {
		return "", false
	}
	var text string
	var ok bool
	withAutoreleasePool(func() {
		pg := objc.ID(objcCall(uintptr(d.doc), selPageAtIndex, uintptr(page)))
		if pg == 0 {
			return
		}
		nsStr := objc.ID(objcCall(uintptr(pg), selString))
		if nsStr != 0 {
			text = fromNSString(nsStr)
			ok = true
		}
	})
	return text, ok
}

func (d *darwinPDF) Outlines() ([]OutlineItem, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.doc == 0 {
		return nil, false
	}
	var items []OutlineItem
	var ok bool
	withAutoreleasePool(func() {
		root := objc.ID(objcCall(uintptr(d.doc), selOutlineRoot))
		if root == 0 {
			return
		}
		items = d.readOutlineNode(root, 0)
		ok = len(items) > 0
	})
	return items, ok
}

func (d *darwinPDF) readOutlineNode(node objc.ID, depth int) []OutlineItem {
	if node == 0 || depth > 12 {
		return nil
	}
	n := int(objcCall(uintptr(node), selNumChildren))
	if n <= 0 {
		return nil
	}
	out := make([]OutlineItem, 0, n)
	for i := range n {
		child := objc.ID(objcCall(uintptr(node), selChildAtIndex, uintptr(i)))
		if child == 0 {
			continue
		}
		title := fromNSString(objc.ID(objcCall(uintptr(child), selLabel)))
		pageIdx := -1
		dest := objc.ID(objcCall(uintptr(child), selDestination))
		if dest != 0 {
			pg := objc.ID(objcCall(uintptr(dest), selPage))
			if pg != 0 {
				idx := int(objcCall(uintptr(d.doc), selIndexForPage, uintptr(pg)))
				if idx >= 0 && idx < d.cnt {
					pageIdx = idx
				}
			}
		}
		kids := d.readOutlineNode(child, depth+1)
		if title != "" || len(kids) > 0 {
			out = append(out, OutlineItem{Title: title, Page: pageIdx, Children: kids})
		}
	}
	return out
}

func (d *darwinPDF) Find(query string) ([]Match, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.doc == 0 {
		return nil, false
	}
	var out []Match
	withAutoreleasePool(func() {
		nsQuery := makeNSString(query)
		if nsQuery == 0 {
			return
		}
		arr := objc.ID(objcCall(uintptr(d.doc), selFindString, uintptr(nsQuery), nsCaseInsensitive))
		if arr == 0 {
			return
		}
		n := min(int(objcCall(uintptr(arr), selCount)), 2000)
		for i := range n {
			sel := objc.ID(objcCall(uintptr(arr), selObjectAtIndex, uintptr(i)))
			if sel == 0 {
				continue
			}
			pages := objc.ID(objcCall(uintptr(sel), selPages))
			if pages == 0 || int(objcCall(uintptr(pages), selCount)) == 0 {
				continue
			}
			pg := objc.ID(objcCall(uintptr(pages), selObjectAtIndex, 0))
			if pg == 0 {
				continue
			}
			pageIdx := int(objcCall(uintptr(d.doc), selIndexForPage, uintptr(pg)))
			if pageIdx < 0 || pageIdx >= d.cnt {
				continue
			}
			crop := msgBoundsForBox(pg, selBoundsForBox, pdfDisplayBoxCrop)
			if crop.Size.Width <= 0 || crop.Size.Height <= 0 {
				crop = msgBoundsForBox(pg, selBoundsForBox, pdfDisplayBoxMedia)
			}
			rot := ((int(objcCall(uintptr(pg), selRotation)) % 360) + 360) % 360
			sb := msgBoundsPage(sel, selBoundsForPage, pg)
			var rects []NormRect
			if crop.Size.Width > 0 && crop.Size.Height > 0 && sb.Size.Width > 0 && sb.Size.Height > 0 {
				nr := normalizePDFRect(sb, crop, rot)
				rects = []NormRect{nr}
			}
			snippet := fromNSString(objc.ID(objcCall(uintptr(sel), selString)))
			out = append(out, Match{
				Page:    pageIdx,
				Snippet: snippet,
				Rects:   rects,
			})
		}
	})
	return out, true
}

func normalizePDFRect(sb, crop nsRect, pageRot int) NormRect {
	// Convert from unrotated bottom-left crop box space to [0..1] top-left space,
	// then rotate into the page's natural display orientation.
	u0 := (sb.Origin.X - crop.Origin.X) / crop.Size.Width
	v0 := 1.0 - (sb.Origin.Y-crop.Origin.Y+sb.Size.Height)/crop.Size.Height
	uw := sb.Size.Width / crop.Size.Width
	vh := sb.Size.Height / crop.Size.Height

	var x, y, w, h float64
	switch pageRot {
	case 90:
		x = 1.0 - (v0 + vh)
		y = u0
		w = vh
		h = uw
	case 180:
		x = 1.0 - (u0 + uw)
		y = 1.0 - (v0 + vh)
		w = uw
		h = vh
	case 270:
		x = v0
		y = 1.0 - (u0 + uw)
		w = vh
		h = uw
	default:
		x, y, w, h = u0, v0, uw, vh
	}
	x = math.Max(0, math.Min(1, x))
	y = math.Max(0, math.Min(1, y))
	w = math.Max(0.002, math.Min(1-x, w))
	h = math.Max(0.002, math.Min(1-y, h))
	return NormRect{X: x, Y: y, W: w, H: h}
}

func (d *darwinPDF) Close() {
	d.mu.Lock()
	doc := d.doc
	d.doc = 0
	d.mu.Unlock()
	if doc != 0 {
		withAutoreleasePool(func() {
			objcCall(uintptr(doc), selRelease)
		})
	}
}
