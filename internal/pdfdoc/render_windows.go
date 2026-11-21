//go:build windows

package pdfdoc

import (
	"bytes"
	"errors"
	"image"
	_ "image/png"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows 10/11 built-in WinRT Windows.Data.Pdf.PdfDocument rasterizer via pure
// syscall COM vtables (zero cgo). Falls back to the pure-Go parser if WinRT is
// unavailable.

type winGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	// {D33A30EB-2C41-4D8E-AE76-A90E7D0B9388} IPdfDocumentStatics
	iidIPdfDocumentStatics = winGUID{0x433A0B5F, 0xC007, 0x4788, [8]byte{0x90, 0xF2, 0x08, 0x14, 0x3D, 0x92, 0x25, 0x99}}
	// {AC7EBEDD-80FA-4089-846E-81B77FF5A86C} IPdfDocument
	iidIPdfDocument = winGUID{0xAC7EBEDD, 0x80FA, 0x4089, [8]byte{0x84, 0x6E, 0x81, 0xB7, 0x7F, 0xF5, 0xA8, 0x6C}}
	// {9DB4B0C8-5320-4CFC-AD76-493FDAD0E594} IPdfPage
	iidIPdfPage = winGUID{0x9DB4B0C8, 0x5320, 0x4CFC, [8]byte{0xAD, 0x76, 0x49, 0x3F, 0xDA, 0xD0, 0xE5, 0x94}}
	// {905A0FE1-BC53-11DF-8C49-001E4FC686DA} IRandomAccessStream
	iidIRandomAccessStream = winGUID{0x905A0FE1, 0xBC53, 0x11DF, [8]byte{0x8C, 0x49, 0x00, 0x1E, 0x4F, 0xC6, 0x86, 0xDA}}
	// {0000000C-0000-0000-C000-000000000046} IStream
	iidIStream = winGUID{0x0000000C, 0x0000, 0x0000, [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	// {00000036-0000-0000-C000-000000000046} IAsyncInfo
	iidIAsyncInfo = winGUID{0x00000036, 0x0000, 0x0000, [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	// {C7D2D82F-F789-41A4-B0D0-EE703E47ADAD} IPdfPageRenderOptions
	iidIPdfPageRenderOptions = winGUID{0x3C98056F, 0xB7CF, 0x4C29, [8]byte{0x9A, 0x04, 0x52, 0xD9, 0x02, 0x67, 0xF4, 0x25}}

	winOnce             sync.Once
	winReady            bool
	combaseDLL          = windows.NewLazySystemDLL("combase.dll")
	shcoreDLL           = windows.NewLazySystemDLL("shcore.dll")
	kernel32DLL         = windows.NewLazySystemDLL("kernel32.dll")
	procRoInitialize    = combaseDLL.NewProc("RoInitialize")
	procRoGetFactory    = combaseDLL.NewProc("RoGetActivationFactory")
	procRoActivateInst  = combaseDLL.NewProc("RoActivateInstance")
	procWinCreateString = combaseDLL.NewProc("WindowsCreateString")
	procWinDeleteString = combaseDLL.NewProc("WindowsDeleteString")
	procCreateRASOnFile = shcoreDLL.NewProc("CreateRandomAccessStreamOnFile")
	procCreateStreamRAS = shcoreDLL.NewProc("CreateStreamOverRandomAccessStream")
	procSleep           = kernel32DLL.NewProc("Sleep")
	procRtlMoveMemory   = kernel32DLL.NewProc("RtlMoveMemory")
)

func initWinPDF() bool {
	winOnce.Do(func() {
		if combaseDLL.Load() != nil || shcoreDLL.Load() != nil {
			return
		}
		if procRoInitialize.Find() != nil || procRoGetFactory.Find() != nil ||
			procRoActivateInst.Find() != nil || procWinCreateString.Find() != nil ||
			procCreateRASOnFile.Find() != nil || procCreateStreamRAS.Find() != nil {
			return
		}
		// RO_INIT_MULTITHREADED = 1; RPC_E_CHANGED_MODE (0x80010106) is also fine.
		hr, _, _ := procRoInitialize.Call(1)
		if hr != 0 && hr != 1 && uint32(hr) != 0x80010106 {
			return
		}
		winReady = true
	})
	return winReady
}

func makeHString(s string) (uintptr, error) {
	u16, err := syscall.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var hs uintptr
	hr, _, _ := procWinCreateString.Call(
		uintptr(unsafe.Pointer(&u16[0])),
		uintptr(len(u16)-1),
		uintptr(unsafe.Pointer(&hs)),
	)
	runtime.KeepAlive(u16)
	if int32(hr) < 0 {
		return 0, syscall.Errno(hr)
	}
	return hs, nil
}

func freeHString(hs uintptr) {
	if hs != 0 {
		_, _, _ = procWinDeleteString.Call(hs)
	}
}

func comVtbl(obj uintptr, idx int) uintptr {
	ptrSize := unsafe.Sizeof(uintptr(0))
	var vtbl uintptr
	_, _, _ = procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&vtbl)), obj, ptrSize)
	var fn uintptr
	_, _, _ = procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&fn)), vtbl+uintptr(idx)*ptrSize, ptrSize)
	return fn
}

func comRelease(obj uintptr) {
	if obj != 0 {
		_, _, _ = syscall.SyscallN(comVtbl(obj, 2), obj)
	}
}

func comQueryInterface(obj uintptr, iid *winGUID) (uintptr, bool) {
	var out uintptr
	hr, _, _ := syscall.SyscallN(comVtbl(obj, 0), obj, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	return out, int32(hr) >= 0 && out != 0
}

// awaitWinRTAsync polls IAsyncInfo.Status (slot 7) until Completed (1),
// Canceled (2), or Error (3), then calls GetResults (slot 8 on the operation).
func awaitWinRTAsync(asyncOp uintptr, hasResult bool) (uintptr, error) {
	info, ok := comQueryInterface(asyncOp, &iidIAsyncInfo)
	if !ok {
		return 0, errors.New("winrt: QueryInterface IAsyncInfo failed")
	}
	defer comRelease(info)
	for range 2000 {
		var status uint32
		hr, _, _ := syscall.SyscallN(comVtbl(info, 7), info, uintptr(unsafe.Pointer(&status)))
		if int32(hr) < 0 {
			return 0, syscall.Errno(hr)
		}
		switch status {
		case 0: // Started
			_, _, _ = procSleep.Call(2)
		case 1: // Completed
			if !hasResult {
				_, _, _ = syscall.SyscallN(comVtbl(asyncOp, 8), asyncOp)
				return 0, nil
			}
			var res uintptr
			hr2, _, _ := syscall.SyscallN(comVtbl(asyncOp, 8), asyncOp, uintptr(unsafe.Pointer(&res)))
			if int32(hr2) < 0 {
				return 0, syscall.Errno(hr2)
			}
			return res, nil
		default:
			var errCode int32
			_, _, _ = syscall.SyscallN(comVtbl(info, 8), info, uintptr(unsafe.Pointer(&errCode)))
			return 0, errors.New("winrt: async operation failed")
		}
	}
	return 0, errors.New("winrt: async timeout")
}

type winPDF struct {
	mu     sync.Mutex
	doc    uintptr
	stream uintptr
	cnt    int
}

func openPlatformFile(path string) (platformDoc, error) {
	if !initWinPDF() {
		return nil, errors.New("winrt pdf unavailable")
	}
	u16, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	var ras uintptr
	// FileAccessMode.Read = 0
	hr, _, _ := procCreateRASOnFile.Call(
		uintptr(unsafe.Pointer(u16)),
		0,
		uintptr(unsafe.Pointer(&iidIRandomAccessStream)),
		uintptr(unsafe.Pointer(&ras)),
	)
	runtime.KeepAlive(u16)
	if int32(hr) < 0 || ras == 0 {
		return nil, ErrInvalid
	}

	hs, err := makeHString("Windows.Data.Pdf.PdfDocument")
	if err != nil {
		comRelease(ras)
		return nil, err
	}
	defer freeHString(hs)

	var statics uintptr
	hr, _, _ = procRoGetFactory.Call(hs, uintptr(unsafe.Pointer(&iidIPdfDocumentStatics)), uintptr(unsafe.Pointer(&statics)))
	if int32(hr) < 0 || statics == 0 {
		comRelease(ras)
		return nil, errors.New("winrt PdfDocument factory unavailable")
	}
	defer comRelease(statics)

	// IPdfDocumentStatics::LoadFromStreamAsync is slot 8 (IInspectable 0..5, LoadFromFileAsync 6, LoadFromFileWithPasswordAsync 7, LoadFromStreamAsync 8).
	var asyncOp uintptr
	hr, _, _ = syscall.SyscallN(comVtbl(statics, 8), statics, ras, uintptr(unsafe.Pointer(&asyncOp)))
	if int32(hr) < 0 || asyncOp == 0 {
		comRelease(ras)
		return nil, ErrInvalid
	}
	docObj, err := awaitWinRTAsync(asyncOp, true)
	comRelease(asyncOp)
	if err != nil || docObj == 0 {
		comRelease(ras)
		return nil, ErrInvalid
	}

	pdfDoc, ok := comQueryInterface(docObj, &iidIPdfDocument)
	comRelease(docObj)
	if !ok {
		comRelease(ras)
		return nil, ErrInvalid
	}

	// IPdfDocument::get_IsPasswordProtected is slot 8, get_PageCount is slot 7, GetPage is slot 6.
	var protected byte
	_, _, _ = syscall.SyscallN(comVtbl(pdfDoc, 8), pdfDoc, uintptr(unsafe.Pointer(&protected)))
	var cnt uint32
	_, _, _ = syscall.SyscallN(comVtbl(pdfDoc, 7), pdfDoc, uintptr(unsafe.Pointer(&cnt)))
	if cnt == 0 {
		comRelease(pdfDoc)
		comRelease(ras)
		if protected != 0 {
			return nil, ErrEncrypted
		}
		return nil, ErrInvalid
	}
	return &winPDF{doc: pdfDoc, stream: ras, cnt: int(cnt)}, nil
}

func openPlatformBytes(data []byte) (platformDoc, error) {
	return nil, errors.New("winrt bytes uses pure-Go or file path")
}

func (w *winPDF) PageCount() int { return w.cnt }

func (w *winPDF) PageInfo(page int) (PageInfo, bool) {
	if page < 0 || page >= w.cnt {
		return PageInfo{}, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.doc == 0 {
		return PageInfo{}, false
	}
	var pageObj uintptr
	hr, _, _ := syscall.SyscallN(comVtbl(w.doc, 6), w.doc, uintptr(page), uintptr(unsafe.Pointer(&pageObj)))
	if int32(hr) < 0 || pageObj == 0 {
		return PageInfo{}, false
	}
	defer comRelease(pageObj)
	// IPdfPage::get_Size is slot 9 (Size of two float32s: Width, Height in DIPs 96 DPI).
	var size struct{ W, H float32 }
	hr, _, _ = syscall.SyscallN(comVtbl(pageObj, 9), pageObj, uintptr(unsafe.Pointer(&size)))
	if int32(hr) < 0 || size.W <= 0 || size.H <= 0 {
		return PageInfo{}, false
	}
	return PageInfo{Width: float64(size.W) * 72.0 / 96.0, Height: float64(size.H) * 72.0 / 96.0}, true
}

func (w *winPDF) RenderPage(page int, widthPx, heightPx int, extraRotate int) (*image.RGBA, error) {
	rot := ((extraRotate % 360) + 360) % 360
	baseW, baseH := widthPx, heightPx
	if rot == 90 || rot == 270 {
		baseW, baseH = heightPx, widthPx
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.doc == 0 {
		return nil, errors.New("pdfdoc: document closed")
	}

	var pageObj uintptr
	hr, _, _ := syscall.SyscallN(comVtbl(w.doc, 6), w.doc, uintptr(page), uintptr(unsafe.Pointer(&pageObj)))
	if int32(hr) < 0 || pageObj == 0 {
		return nil, ErrInvalid
	}
	defer comRelease(pageObj)

	hsMem, err := makeHString("Windows.Storage.Streams.InMemoryRandomAccessStream")
	if err != nil {
		return nil, err
	}
	defer freeHString(hsMem)
	var memInspectable uintptr
	hr, _, _ = procRoActivateInst.Call(hsMem, uintptr(unsafe.Pointer(&memInspectable)))
	if int32(hr) < 0 || memInspectable == 0 {
		return nil, errors.New("winrt InMemoryRandomAccessStream failed")
	}
	memRAS, ok := comQueryInterface(memInspectable, &iidIRandomAccessStream)
	comRelease(memInspectable)
	if !ok {
		return nil, errors.New("winrt IRandomAccessStream failed")
	}
	defer comRelease(memRAS)

	// Optional IPdfPageRenderOptions for exact pixel dimensions.
	var renderAsync uintptr
	hsOpt, optErr := makeHString("Windows.Data.Pdf.PdfPageRenderOptions")
	if optErr == nil {
		defer freeHString(hsOpt)
		var optInsp uintptr
		if hrOpt, _, _ := procRoActivateInst.Call(hsOpt, uintptr(unsafe.Pointer(&optInsp))); int32(hrOpt) >= 0 && optInsp != 0 {
			if opt, okOpt := comQueryInterface(optInsp, &iidIPdfPageRenderOptions); okOpt {
				// put_DestinationWidth = slot 9, put_DestinationHeight = slot 11
				_, _, _ = syscall.SyscallN(comVtbl(opt, 9), opt, uintptr(baseW))
				_, _, _ = syscall.SyscallN(comVtbl(opt, 11), opt, uintptr(baseH))
				// RenderWithOptionsToStreamAsync = slot 7
				_, _, _ = syscall.SyscallN(comVtbl(pageObj, 7), pageObj, memRAS, opt, uintptr(unsafe.Pointer(&renderAsync)))
				comRelease(opt)
			}
			comRelease(optInsp)
		}
	}
	if renderAsync == 0 {
		// RenderToStreamAsync = slot 6
		hr, _, _ = syscall.SyscallN(comVtbl(pageObj, 6), pageObj, memRAS, uintptr(unsafe.Pointer(&renderAsync)))
		if int32(hr) < 0 || renderAsync == 0 {
			return nil, errors.New("winrt RenderToStreamAsync failed")
		}
	}
	_, err = awaitWinRTAsync(renderAsync, false)
	comRelease(renderAsync)
	if err != nil {
		return nil, err
	}

	// Seek IRandomAccessStream to 0 (slot 11) and read via IStream.
	_, _, _ = syscall.SyscallN(comVtbl(memRAS, 11), memRAS, 0)
	var istream uintptr
	hr, _, _ = procCreateStreamRAS.Call(memRAS, uintptr(unsafe.Pointer(&iidIStream)), uintptr(unsafe.Pointer(&istream)))
	if int32(hr) < 0 || istream == 0 {
		return nil, errors.New("winrt CreateStreamOverRandomAccessStream failed")
	}
	defer comRelease(istream)

	var pngBuf bytes.Buffer
	chunk := make([]byte, 32768)
	for {
		var readN uint32
		hrR, _, _ := syscall.SyscallN(comVtbl(istream, 3), istream, uintptr(unsafe.Pointer(&chunk[0])), uintptr(len(chunk)), uintptr(unsafe.Pointer(&readN)))
		if readN > 0 {
			pngBuf.Write(chunk[:readN])
		}
		if int32(hrR) < 0 || readN == 0 {
			break
		}
	}
	img, _, err := image.Decode(&pngBuf)
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

func (w *winPDF) PageText(page int) (string, bool)  { return "", false }
func (w *winPDF) Outlines() ([]OutlineItem, bool)   { return nil, false }
func (w *winPDF) Find(query string) ([]Match, bool) { return nil, false }

func (w *winPDF) Close() {
	w.mu.Lock()
	doc, ras := w.doc, w.stream
	w.doc, w.stream = 0, 0
	w.mu.Unlock()
	comRelease(doc)
	comRelease(ras)
}
