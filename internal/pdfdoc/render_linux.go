//go:build linux

package pdfdoc

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	linuxOnce   sync.Once
	popplerOK   bool
	pdftoppmBin string

	gObjectUnref                  func(obj uintptr)
	gFree                         func(ptr uintptr)
	gErrorFree                    func(err uintptr)
	gErrorMatches                 func(err uintptr, domain uint32, code int32) int32
	gBytesNew                     func(data unsafe.Pointer, size uintptr) uintptr
	gBytesUnref                   func(b uintptr)
	popplerErrorQuark             func() uint32
	popplerDocNewFromFile         func(uri *byte, pwd *byte, gerr *uintptr) uintptr
	popplerDocNewFromBytes        func(gbytes uintptr, pwd *byte, gerr *uintptr) uintptr
	popplerDocGetNPages           func(doc uintptr) int32
	popplerDocGetPage             func(doc uintptr, index int32) uintptr
	popplerPageGetSize            func(page uintptr, w, h *float64)
	popplerPageRender             func(page uintptr, cr uintptr)
	popplerPageGetText            func(page uintptr) uintptr
	cairoImageSurfaceCreateForBuf func(data *byte, format int32, w, h, stride int32) uintptr
	cairoSurfaceDestroy           func(surf uintptr)
	cairoSurfaceFlush             func(surf uintptr)
	cairoCreate                   func(surf uintptr) uintptr
	cairoDestroy                  func(cr uintptr)
	cairoScale                    func(cr uintptr, sx, sy float64)
	cairoSetSourceRGB             func(cr uintptr, r, g, b float64)
	cairoPaint                    func(cr uintptr)
	cStrlen                       func(s uintptr) uintptr
	cMemmove                      func(dst *byte, src uintptr, n uintptr) uintptr
)

func initLinuxPDF() {
	linuxOnce.Do(func() {
		if p, err := exec.LookPath("pdftoppm"); err == nil {
			pdftoppmBin = p
		}
		libPoppler, err := openAnyLib("libpoppler-glib.so.8", "libpoppler-glib.so")
		if err != nil {
			return
		}
		libCairo, err := openAnyLib("libcairo.so.2", "libcairo.so")
		if err != nil {
			return
		}
		libGObj, err := openAnyLib("libgobject-2.0.so.0", "libgobject-2.0.so")
		if err != nil {
			return
		}
		libGLib, err := openAnyLib("libglib-2.0.so.0", "libglib-2.0.so")
		if err != nil {
			return
		}
		libC, err := openAnyLib("libc.so.6", "libc.so")
		if err != nil {
			return
		}

		purego.RegisterLibFunc(&gObjectUnref, libGObj, "g_object_unref")
		purego.RegisterLibFunc(&gFree, libGLib, "g_free")
		purego.RegisterLibFunc(&gErrorFree, libGLib, "g_error_free")
		purego.RegisterLibFunc(&gErrorMatches, libGLib, "g_error_matches")
		purego.RegisterLibFunc(&gBytesNew, libGLib, "g_bytes_new")
		purego.RegisterLibFunc(&gBytesUnref, libGLib, "g_bytes_unref")

		purego.RegisterLibFunc(&popplerErrorQuark, libPoppler, "poppler_error_quark")
		purego.RegisterLibFunc(&popplerDocNewFromFile, libPoppler, "poppler_document_new_from_file")
		if _, err := purego.Dlsym(libPoppler, "poppler_document_new_from_bytes"); err == nil {
			purego.RegisterLibFunc(&popplerDocNewFromBytes, libPoppler, "poppler_document_new_from_bytes")
		}
		purego.RegisterLibFunc(&popplerDocGetNPages, libPoppler, "poppler_document_get_n_pages")
		purego.RegisterLibFunc(&popplerDocGetPage, libPoppler, "poppler_document_get_page")
		purego.RegisterLibFunc(&popplerPageGetSize, libPoppler, "poppler_page_get_size")
		purego.RegisterLibFunc(&popplerPageRender, libPoppler, "poppler_page_render")
		purego.RegisterLibFunc(&popplerPageGetText, libPoppler, "poppler_page_get_text")

		purego.RegisterLibFunc(&cairoImageSurfaceCreateForBuf, libCairo, "cairo_image_surface_create_for_data")
		purego.RegisterLibFunc(&cairoSurfaceDestroy, libCairo, "cairo_surface_destroy")
		purego.RegisterLibFunc(&cairoSurfaceFlush, libCairo, "cairo_surface_flush")
		purego.RegisterLibFunc(&cairoCreate, libCairo, "cairo_create")
		purego.RegisterLibFunc(&cairoDestroy, libCairo, "cairo_destroy")
		purego.RegisterLibFunc(&cairoScale, libCairo, "cairo_scale")
		purego.RegisterLibFunc(&cairoSetSourceRGB, libCairo, "cairo_set_source_rgb")
		purego.RegisterLibFunc(&cairoPaint, libCairo, "cairo_paint")

		purego.RegisterLibFunc(&cStrlen, libC, "strlen")
		purego.RegisterLibFunc(&cMemmove, libC, "memmove")

		popplerOK = true
	})
}

func openAnyLib(names ...string) (uintptr, error) {
	var last error
	for _, n := range names {
		h, err := purego.Dlopen(n, purego.RTLD_LAZY|purego.RTLD_LOCAL)
		if err == nil {
			return h, nil
		}
		last = err
	}
	return 0, last
}

type linuxPDF struct {
	mu      sync.Mutex
	doc     uintptr
	cnt     int
	path    string
	tmpPath string
}

func openPlatformFile(path string) (platformDoc, error) {
	initLinuxPDF()
	if popplerOK {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		u := url.URL{Scheme: "file", Path: abs}
		uri := append([]byte(u.String()), 0)
		var gerr uintptr
		doc := popplerDocNewFromFile(&uri[0], nil, &gerr)
		runtime.KeepAlive(uri)
		if doc == 0 {
			errOut := classifyGLibErr(gerr)
			if gerr != 0 {
				gErrorFree(gerr)
			}
			return nil, errOut
		}
		cnt := int(popplerDocGetNPages(doc))
		if cnt <= 0 {
			gObjectUnref(doc)
			return nil, ErrInvalid
		}
		return &linuxPDF{doc: doc, cnt: cnt, path: abs}, nil
	}
	if pdftoppmBin != "" {
		rawData, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		raw, err := parseRawPDF(rawData)
		if err != nil {
			return nil, err
		}
		return &cliLinuxPDF{path: path, raw: raw}, nil
	}
	return nil, errors.New("poppler unavailable")
}

func openPlatformBytes(data []byte) (platformDoc, error) {
	initLinuxPDF()
	if popplerOK && popplerDocNewFromBytes != nil && len(data) > 0 {
		gb := gBytesNew(unsafe.Pointer(&data[0]), uintptr(len(data)))
		runtime.KeepAlive(data)
		if gb != 0 {
			defer gBytesUnref(gb)
			var gerr uintptr
			doc := popplerDocNewFromBytes(gb, nil, &gerr)
			if doc == 0 {
				errOut := classifyGLibErr(gerr)
				if gerr != 0 {
					gErrorFree(gerr)
				}
				return nil, errOut
			}
			cnt := int(popplerDocGetNPages(doc))
			if cnt <= 0 {
				gObjectUnref(doc)
				return nil, ErrInvalid
			}
			return &linuxPDF{doc: doc, cnt: cnt}, nil
		}
	}
	return nil, errors.New("poppler bytes unavailable")
}

func classifyGLibErr(gerr uintptr) error {
	if gerr == 0 {
		return ErrInvalid
	}
	// POPPLER_ERROR_ENCRYPTED == 3 in poppler-glib.
	if popplerErrorQuark != nil && gErrorMatches != nil && gErrorMatches(gerr, popplerErrorQuark(), 3) != 0 {
		return ErrEncrypted
	}
	return ErrInvalid
}

func (l *linuxPDF) PageCount() int { return l.cnt }

func (l *linuxPDF) PageInfo(page int) (PageInfo, bool) {
	if page < 0 || page >= l.cnt {
		return PageInfo{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.doc == 0 {
		return PageInfo{}, false
	}
	pg := popplerDocGetPage(l.doc, int32(page))
	if pg == 0 {
		return PageInfo{}, false
	}
	defer gObjectUnref(pg)
	var w, h float64
	popplerPageGetSize(pg, &w, &h)
	if w <= 0 || h <= 0 {
		return PageInfo{}, false
	}
	return PageInfo{Width: w, Height: h}, true
}

func (l *linuxPDF) RenderPage(page int, widthPx, heightPx int, extraRotate int) (*image.RGBA, error) {
	rot := ((extraRotate % 360) + 360) % 360
	baseW, baseH := widthPx, heightPx
	if rot == 90 || rot == 270 {
		baseW, baseH = heightPx, widthPx
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.doc == 0 {
		return nil, errors.New("pdfdoc: document closed")
	}
	pg := popplerDocGetPage(l.doc, int32(page))
	if pg == 0 {
		return nil, ErrInvalid
	}
	defer gObjectUnref(pg)

	var pw, ph float64
	popplerPageGetSize(pg, &pw, &ph)
	if pw <= 0 || ph <= 0 {
		return nil, ErrInvalid
	}

	// CAIRO_FORMAT_ARGB32 == 0 (native-endian ARGB; BGRA on little-endian).
	stride := baseW * 4
	bgra := make([]byte, stride*baseH)
	surf := cairoImageSurfaceCreateForBuf(&bgra[0], 0, int32(baseW), int32(baseH), int32(stride))
	if surf == 0 {
		return nil, errors.New("cairo surface failed")
	}
	defer func() {
		cairoSurfaceDestroy(surf)
		runtime.KeepAlive(bgra)
	}()

	cr := cairoCreate(surf)
	if cr == 0 {
		return nil, errors.New("cairo context failed")
	}
	defer cairoDestroy(cr)

	cairoSetSourceRGB(cr, 1, 1, 1)
	cairoPaint(cr)
	cairoScale(cr, float64(baseW)/pw, float64(baseH)/ph)
	popplerPageRender(pg, cr)
	cairoSurfaceFlush(surf)

	rgba := image.NewRGBA(image.Rect(0, 0, baseW, baseH))
	for y := range baseH {
		srow := bgra[y*stride:]
		drow := rgba.Pix[y*rgba.Stride:]
		for x := range baseW {
			b := srow[x*4+0]
			g := srow[x*4+1]
			r := srow[x*4+2]
			a := srow[x*4+3]
			drow[x*4+0] = r
			drow[x*4+1] = g
			drow[x*4+2] = b
			drow[x*4+3] = a
		}
	}
	return rotateRGBA(rgba, rot), nil
}

func (l *linuxPDF) PageText(page int) (string, bool) {
	if page < 0 || page >= l.cnt {
		return "", false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.doc == 0 {
		return "", false
	}
	pg := popplerDocGetPage(l.doc, int32(page))
	if pg == 0 {
		return "", false
	}
	defer gObjectUnref(pg)
	cstr := popplerPageGetText(pg)
	if cstr == 0 {
		return "", false
	}
	defer gFree(cstr)
	const maxLen = 16 << 20
	n := int(cStrlen(cstr))
	if n <= 0 {
		return "", true
	}
	if n > maxLen {
		n = maxLen
	}
	buf := make([]byte, n)
	cMemmove(&buf[0], cstr, uintptr(n))
	runtime.KeepAlive(buf)
	return string(buf), true
}

func (l *linuxPDF) Outlines() ([]OutlineItem, bool)   { return nil, false }
func (l *linuxPDF) Find(query string) ([]Match, bool) { return nil, false }

func (l *linuxPDF) Close() {
	l.mu.Lock()
	doc := l.doc
	l.doc = 0
	tmp := l.tmpPath
	l.tmpPath = ""
	l.mu.Unlock()
	if doc != 0 {
		gObjectUnref(doc)
	}
	if tmp != "" {
		_ = os.Remove(tmp)
	}
}

type cliLinuxPDF struct {
	path string
	raw  *rawPDF
}

func (c *cliLinuxPDF) PageCount() int { return len(c.raw.pageInfos) }

func (c *cliLinuxPDF) PageInfo(page int) (PageInfo, bool) {
	if page < 0 || page >= len(c.raw.pageInfos) {
		return PageInfo{}, false
	}
	return c.raw.pageInfos[page], true
}

func (c *cliLinuxPDF) RenderPage(page int, widthPx, heightPx int, extraRotate int) (*image.RGBA, error) {
	rot := ((extraRotate % 360) + 360) % 360
	baseW := widthPx
	if rot == 90 || rot == 270 {
		baseW = heightPx
	}
	pNum := fmt.Sprintf("%d", page+1)
	cmd := exec.Command(pdftoppmBin, "-png", "-singlefile", "-f", pNum, "-l", pNum, "-scale-to-x", fmt.Sprintf("%d", baseW), "-scale-to-y", "-1", c.path)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := range b.Dy() {
		for x := range b.Dx() {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			off := y*rgba.Stride + x*4
			rgba.Pix[off] = uint8(r >> 8)
			rgba.Pix[off+1] = uint8(g >> 8)
			rgba.Pix[off+2] = uint8(bl >> 8)
			rgba.Pix[off+3] = uint8(a >> 8)
		}
	}
	return rotateRGBA(rgba, rot), nil
}

func (c *cliLinuxPDF) PageText(page int) (string, bool) { return c.raw.extractPageText(page), true }
func (c *cliLinuxPDF) Outlines() ([]OutlineItem, bool) {
	return c.raw.outlines, len(c.raw.outlines) > 0
}
func (c *cliLinuxPDF) Find(query string) ([]Match, bool) { return nil, false }
func (c *cliLinuxPDF) Close()                            {}
