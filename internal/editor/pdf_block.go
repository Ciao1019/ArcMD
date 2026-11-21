package editor

import (
	"fmt"
	"math"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/Ciao1019/ArcMD/internal/pdfdoc"
	"github.com/egoist/mygo/ui"
)

const (
	pdfDefaultInlineH = float32(500)
	pdfMinInlineH     = float32(260)
	pdfMaxInlineH     = float32(780)
	pdfRestMaxH       = float32(460)
	pdfMargin         = float32(14)
)

type embedPDFCache struct {
	rev uint64
	doc *pdfdoc.Document
	err error
}

// IsPDFURL reports whether raw points to a .pdf resource (ignoring query or #page= fragment).
func IsPDFURL(raw string) bool {
	clean, _, _ := strings.Cut(strings.TrimSpace(raw), "#")
	clean, _, _ = strings.Cut(clean, "?")
	if decoded, err := url.PathUnescape(clean); err == nil {
		clean = decoded
	}
	base := path.Base(strings.ReplaceAll(clean, `\`, "/"))
	low := strings.ToLower(base)
	return len(low) > len(".pdf") && strings.HasSuffix(low, ".pdf")
}

// ParsePDFURLPage splits a PDF link/embed URL into its file path part and an
// optional 1-based page number parsed from "#page=N" or "#N".
func ParsePDFURLPage(raw string) (cleanURL string, page1Based int) {
	base, frag, hasFrag := strings.Cut(strings.TrimSpace(raw), "#")
	cleanURL = strings.TrimSpace(base)
	if !hasFrag {
		return cleanURL, 0
	}
	frag = strings.TrimSpace(frag)
	low := strings.ToLower(frag)
	if strings.HasPrefix(low, "page=") {
		if n, err := strconv.Atoi(strings.TrimSpace(frag[5:])); err == nil && n >= 1 {
			return cleanURL, n
		}
	} else if n, err := strconv.Atoi(frag); err == nil && n >= 1 {
		return cleanURL, n
	}
	return cleanURL, 0
}

// WithPDFURLPage returns raw with "#page=N" set (or stripped when page1Based <= 1).
func WithPDFURLPage(raw string, page1Based int) string {
	clean, _ := ParsePDFURLPage(raw)
	if page1Based <= 1 {
		return clean
	}
	return fmt.Sprintf("%s#page=%d", clean, page1Based)
}

func isPDFEmbed(b *document.Block) bool {
	return b != nil && b.Kind == "image" && IsPDFURL(b.URL)
}

func (e *Editor) startPDFBrowsing(id string) {
	e.pdfBrowsing = id
	e.pdfFocusPending = id != ""
	e.ready = false
}

func (e *Editor) openPDFModal(id string) {
	if id == "" {
		return
	}
	e.pdfModalID = id
	e.pdfModalFocusPending = true
	e.ready = false
}

// BrowsePDFBlock activates inline interactive scrolling/zooming for PDF block id.
func (e *Editor) BrowsePDFBlock(id string) {
	e.startPDFBrowsing(id)
}

// OpenPDFModal opens the full-window modal reader for PDF block id.
func (e *Editor) OpenPDFModal(id string) {
	e.openPDFModal(id)
}

func (e *Editor) closePDFModal(id string) {
	e.pdfModalID = ""
	e.ready = false
	if e.pdfBrowsing == id {
		e.pdfFocusPending = true
	} else {
		e.RequestFocus = true
	}
}

func (e *Editor) pdfCurrentPageForBlock(b *document.Block, doc *pdfdoc.Document) int {
	if b == nil || doc == nil || doc.PageCount() == 0 {
		return 0
	}
	_, pinned := ParsePDFURLPage(b.URL)
	p := 0
	if pinned >= 1 {
		p = pinned - 1
	}
	if e.pdfRestPages != nil {
		if cur, ok := e.pdfRestPages[b.ID]; ok {
			p = cur
		}
	}
	return min(max(p, 0), doc.PageCount()-1)
}

func (e *Editor) pdfViewerForBlock(b *document.Block) *PDFViewer {
	if b == nil {
		return nil
	}
	cleanURL, pinned := ParsePDFURLPage(b.URL)
	var doc *pdfdoc.Document
	var rev uint64
	var err error
	if e.loadPDF != nil {
		doc, rev, err = e.loadPDF(cleanURL)
	}
	if e.pdfs == nil {
		e.pdfs = map[string]*PDFViewer{}
	}
	if e.pdfEmbedRev == nil {
		e.pdfEmbedRev = map[string]uint64{}
	}
	v, ok := e.pdfs[b.ID]
	if !ok || e.pdfEmbedRev[b.ID] != rev || v.Doc != doc {
		if ok && v != nil {
			v.ClearBitmaps()
		}
		v = NewPDFViewer(doc, err)
		if doc != nil && doc.PageCount() > 0 {
			startPage := 0
			if pinned >= 1 {
				startPage = min(pinned-1, doc.PageCount()-1)
			}
			if e.pdfRestPages != nil {
				if rp, has := e.pdfRestPages[b.ID]; has {
					startPage = min(max(rp, 0), doc.PageCount()-1)
				}
			}
			v.GoToPage(startPage, true)
		}
		e.pdfs[b.ID] = v
		e.pdfEmbedRev[b.ID] = rev
	}
	return v
}

func (e *Editor) pinPDFBlockPage(id string, page1Based int) {
	if e.ReadOnly {
		return
	}
	b := e.blockByID(id)
	if b == nil || !isPDFEmbed(b) {
		return
	}
	nextURL := WithPDFURLPage(b.URL, page1Based)
	if nextURL != b.URL {
		e.ReplaceImage(id, b.URL, nextURL)
	}
	if e.pdfRestPages == nil {
		e.pdfRestPages = map[string]int{}
	}
	e.pdfRestPages[id] = max(0, page1Based-1)
	e.ready = false
}

func (e *Editor) layoutPDFEmbed(r *row, o Options, gap float32) float32 {
	b := r.leaf.Block
	r.pdfEmbed = true
	browsing := !e.printing && e.pdfBrowsing == b.ID && e.pdfModalID != b.ID
	r.pdfBrowsing = browsing
	r.pdfErr = ""
	r.pdfError = nil
	r.pdfDoc = nil

	availW := r.rect.W
	if b.ImageWidth > 140 {
		availW = min(availW, b.ImageWidth)
	}
	x := r.rect.X + (r.rect.W-availW)/2

	if browsing {
		h := pdfDefaultInlineH
		if custom, ok := e.pdfHeights[b.ID]; ok {
			h = max(pdfMinInlineH, min(pdfMaxInlineH, custom))
		}
		r.pdfRect = ui.Rect{X: x, Y: r.rect.Y + pdfMargin, W: availW, H: h}
		_ = e.pdfViewerForBlock(b)
		r.rect.H = 2*pdfMargin + h + gap
		return r.rect.H
	}

	cleanURL, _ := ParsePDFURLPage(b.URL)
	if e.loadPDF == nil {
		r.pdfErr = i18n.T("无法打开 PDF 文档")
		r.pdfError = ui.ShapeText(r.pdfErr, ui.Font{Family: "system-ui", Size: 13}, max(40, availW-32))
		r.pdfRect = ui.Rect{X: x, Y: r.rect.Y + pdfMargin, W: availW, H: 84}
		r.rect.H = 2*pdfMargin + 84 + gap
		return r.rect.H
	}

	doc, rev, err := e.loadPDF(cleanURL)
	if e.embedPDFs == nil {
		e.embedPDFs = map[string]embedPDFCache{}
	}
	e.embedPDFs[cleanURL] = embedPDFCache{rev: rev, doc: doc, err: err}

	if err != nil || doc == nil || doc.PageCount() == 0 {
		msg := i18n.T("无法打开 PDF 文档")
		if err != nil {
			if strings.Contains(err.Error(), "password-protected") {
				msg = i18n.T("此 PDF 受密码保护，暂不支持直接预览")
			} else {
				msg += "：" + err.Error()
			}
		}
		r.pdfErr = msg
		r.pdfError = ui.ShapeText(msg, ui.Font{Family: "system-ui", Size: 13}, max(40, availW-32))
		_, eh := r.pdfError.Size()
		h := max(96, eh+54)
		r.pdfRect = ui.Rect{X: x, Y: r.rect.Y + pdfMargin, W: availW, H: h}
		r.rect.H = 2*pdfMargin + h + gap
		return r.rect.H
	}

	r.pdfDoc = doc
	pageIdx := e.pdfCurrentPageForBlock(b, doc)
	info := doc.PageInfo(pageIdx)
	h := pdfPreviewHeight(info, availW, e.printing)
	r.pdfRect = ui.Rect{X: x, Y: r.rect.Y + pdfMargin, W: availW, H: h}
	r.rect.H = 2*pdfMargin + h + gap
	return r.rect.H
}

func pdfPreviewHeight(info pdfdoc.PageInfo, width float32, printing bool) float32 {
	aspect := float32(info.Aspect())
	if aspect <= 0 {
		aspect = 595.28 / 841.89
	}
	pad := float32(20)
	innerW := max(60, width-2*pad)
	naturalH := innerW/aspect + 2*pad
	if printing {
		return max(120, min(680, naturalH))
	}
	return max(160, min(pdfRestMaxH, naturalH))
}

func (e *Editor) paintPDFRow(p *ui.Painter, bounds ui.Rect, r row, t *ui.Theme) bool {
	if !r.pdfEmbed {
		return false
	}
	if r.pdfBrowsing {
		return true // painted by interactive PDFView in pdfControls
	}
	dx, dy := e.visualOffset(r)
	box := r.pdfRect
	box.X += bounds.X + dx
	box.Y += bounds.Y + dy

	cleanURL, pinned := ParsePDFURLPage(r.leaf.Block.URL)
	fileName := imageName(cleanURL)

	if r.pdfError != nil {
		p.Fill(box, t.Background.Mix(t.Text, 0.025), 10)
		p.StrokeDashed(box, e.page.Caption, 10, 1)
		p.Icon(iconFileDoc, ui.Rect{X: box.X + (box.W-20)/2, Y: box.Y + 14, W: 20, H: 20}, e.page.Caption)
		ew, _ := r.pdfError.Size()
		p.TextLayout(r.pdfError, box.X+(box.W-ew)/2, box.Y+42, t.TextMuted)
		return true
	}

	if r.pdfDoc == nil {
		return true
	}

	desk := t.Background.Mix(t.Text, 0.035)
	p.Fill(box, desk, 10)
	border := e.page.CodeBorder
	if !e.printing && e.hovered(r.pdfRect, 0) {
		border = t.Accent.Alpha(0.45)
	}
	p.Stroke(box, border, 10, 1)

	v := e.pdfViewerForBlock(r.leaf.Block)
	pageIdx := e.pdfCurrentPageForBlock(r.leaf.Block, r.pdfDoc)
	info := r.pdfDoc.PageInfo(pageIdx)
	aspect := float32(info.Aspect())
	if aspect <= 0 {
		aspect = 595.28 / 841.89
	}

	pad := float32(16)
	maxW := max(40, box.W-2*pad)
	maxH := max(40, box.H-2*pad)
	pw, ph := maxW, maxW/aspect
	if ph > maxH {
		ph = maxH
		pw = ph * aspect
	}
	sheet := ui.Rect{
		X: box.X + (box.W-pw)/2,
		Y: box.Y + (box.H-ph)/2,
		W: pw,
		H: ph,
	}

	p.Shadow(sheet, 4, 0, 3, 12, 0, ui.RGBA(0, 0, 0, 0.10))
	p.Fill(sheet, ui.RGB(255, 255, 255), 4)

	if v != nil {
		v.drainRenderResults(p.Now())
		var invalidate func()
		if !e.printing && e.asyncPDF && !p.Preferences().ReduceMotion && e.services != (ui.Services{}) {
			invalidate = e.services.Invalidate
		}
		targetPx := int(math.Ceil(float64(pw * max(p.Scale(), 1.0))))
		if bmp := v.pageBitmap(pageIdx, targetPx, p.Now(), invalidate); bmp != nil {
			p.Clip(sheet, 4, func() {
				p.Image(bmp, sheet, ui.Contain)
			})
		}
	}
	p.Stroke(sheet, ui.RGBA(0, 0, 0, 0.08), 4, 1)

	if !e.printing {
		wb := e.page.Whiteboard
		bg := wb.IslandFill
		bd := wb.IslandBorder
		if bg.A == 0 {
			bg = e.page.Popover
			bd = e.page.TableLine
		}
		// File badge at bottom-left.
		label := fileName
		if pinned > 1 {
			label = fmt.Sprintf("%s · #%d", fileName, pinned)
		}
		badge := ui.ShapeText(label, ui.Font{Family: "system-ui", Size: 11.5, Weight: 500}, max(60, box.W*0.52))
		bw, bh := badge.Size()
		pill := ui.Rect{X: box.X + 10, Y: box.Y + box.H - bh - 16, W: min(box.W*0.56, bw+28), H: bh + 8}
		p.Fill(pill, bg, 7)
		p.Stroke(pill, bd, 7, 1)
		p.Icon(iconFileDoc, ui.Rect{X: pill.X + 7, Y: pill.Y + (pill.H-13)/2, W: 13, H: 13}, t.Accent)
		p.Clip(ui.Rect{X: pill.X + 23, Y: pill.Y, W: max(0, pill.W-28), H: pill.H}, 0, func() {
			p.TextLayout(badge, pill.X+23, pill.Y+4, t.Text)
		})
	}
	return true
}

type pdfHoverAction uint8

const (
	pdfHoverNone pdfHoverAction = iota
	pdfHoverBrowse
	pdfHoverExpand
	pdfHoverOpen
	pdfHoverSplit
)

type pdfHoverPill struct {
	rect   ui.Rect
	icon   *ui.SVG
	label  string
	action pdfHoverAction
}

func (e *Editor) pdfEmbedHoverPills(d decoration) []pdfHoverPill {
	var pills []pdfHoverPill
	rx := d.rect.X + d.rect.W - 8
	push := func(w float32, icon *ui.SVG, label string, act pdfHoverAction) {
		if rx-w < d.rect.X+8 {
			return
		}
		rx -= w
		pills = append(pills, pdfHoverPill{
			rect:   ui.Rect{X: rx, Y: d.rect.Y + 8, W: w, H: 26},
			icon:   icon,
			label:  label,
			action: act,
		})
		rx -= 6
	}
	if e.openPDF != nil {
		push(86, iconExternalSplit, i18n.T("在分屏中阅读"), pdfHoverSplit)
		push(74, iconFileDoc, i18n.T("打开 PDF"), pdfHoverOpen)
	}
	push(76, iconMaximize, i18n.T("全屏阅读"), pdfHoverExpand)
	push(76, iconOutline, i18n.T("内联浏览"), pdfHoverBrowse)
	return pills
}

func (e *Editor) paintPDFHover(p *ui.Painter, bounds ui.Rect, d decoration, t *ui.Theme) {
	// Hover action pills are rendered as interactive buttons in pdfControls.
	_, _, _, _ = p, bounds, d, t
}

func (e *Editor) pdfControl(d decoration, x, y float32) bool {
	if d.pdfBrowsing {
		return false
	}
	if idx, ok := e.rowIDs[d.id]; ok && e.indexedRow(idx).pdfError != nil {
		return false
	}
	in := func(r ui.Rect) bool { return x >= r.X && x <= r.X+r.W && y >= r.Y && y <= r.Y+r.H }
	for _, pill := range e.pdfEmbedHoverPills(d) {
		if !in(pill.rect) {
			continue
		}
		switch pill.action {
		case pdfHoverBrowse:
			e.startPDFBrowsing(d.id)
			return true
		case pdfHoverExpand:
			e.openPDFModal(d.id)
			return true
		case pdfHoverOpen:
			if e.openPDF != nil {
				e.openPDF(e.pdfURLAtCurrentPage(d.id, d.pdfURL), false)
				return true
			}
		case pdfHoverSplit:
			if e.openPDF != nil {
				e.openPDF(e.pdfURLAtCurrentPage(d.id, d.pdfURL), true)
				return true
			}
		}
	}
	return false
}

func (e *Editor) pdfURLAtCurrentPage(id, rawURL string) string {
	b := e.blockByID(id)
	if b == nil {
		return rawURL
	}
	clean, _ := ParsePDFURLPage(rawURL)
	if e.loadPDF != nil {
		if doc, _, err := e.loadPDF(clean); err == nil && doc != nil {
			cur := e.pdfCurrentPageForBlock(b, doc)
			return WithPDFURLPage(clean, cur+1)
		}
	}
	return rawURL
}

// pdfControls renders rest-mode page steppers, context menus, recovery buttons,
// or the live inline PDFView when e.pdfBrowsing is active.
func (e *Editor) pdfControls(c *ui.Context, o Options) {
	if e.printing {
		return
	}
	t := o.Theme

	// 1. Rest-mode controls for visible PDF embeds (page stepper, double-click to expand, context menu, error recovery).
	for i := range e.visibleRows(1) {
		r := *e.indexedRow(i)
		if !r.pdfEmbed || r.pdfRect.W <= 0 {
			continue
		}
		b := r.leaf.Block
		id := b.ID
		dy := e.ownerVisualDY(r.leaf.Owner)
		box := r.pdfRect
		box.Y += dy

		if id == e.pdfBrowsing && e.pdfModalID != id {
			v := e.pdfViewerForBlock(b)
			if v == nil {
				continue
			}
			if e.pdfFocusPending {
				v.RequestFocus = true
				e.pdfFocusPending = false
			}
			_, pinned := ParsePDFURLPage(b.URL)
			var pinFn func(int)
			if !e.ReadOnly {
				pinFn = func(p1 int) { e.pinPDFBlockPage(id, p1) }
			}
			var splitFn func(int)
			if e.openPDF != nil {
				splitFn = func(p1 int) {
					clean, _ := ParsePDFURLPage(b.URL)
					e.openPDF(WithPDFURLPage(clean, p1), true)
				}
			}
			var revealFn func()
			if o.RevealImage != nil {
				clean, _ := ParsePDFURLPage(b.URL)
				revealFn = func() { o.RevealImage(clean) }
			}

			card := ui.Column(c.Key("inline-pdf:"+id)).Absolute().Left(box.X).Top(box.Y).Size(box.W, box.H).
				Radius(10).Background(t.Background).Border(1.25, t.Accent.Alpha(0.45)).
				Shadow(0, 4, 18, 0, e.page.MathShadow).Clip()
			card.Children(func() {
				PDFView(c, v, PDFOptions{
					Theme:       t,
					Page:        e.page,
					Compact:     true,
					Async:       e.asyncPDF,
					PinnedPage:  pinned,
					PinPage:     pinFn,
					ExpandModal: func() { e.openPDFModal(id) },
					OpenSplit:   splitFn,
					RevealFile:  revealFn,
					Done: func() {
						if e.pdfRestPages == nil {
							e.pdfRestPages = map[string]int{}
						}
						e.pdfRestPages[id] = v.CurrentPage
						e.pdfBrowsing = ""
						e.ready = false
						e.RequestFocus = true
					},
				})
			})

			// Bottom drag handle to resize inline PDF viewport height.
			grip := ui.Box(c.Key("pdf-resize:"+id)).Absolute().Left(box.X+box.W/2-36).Top(box.Y+box.H-5).
				Size(72, 10).Radius(5).Cursor(ui.CursorResizeNS).Tooltip(i18n.T("拖动调整阅读框高度"))
			if grip.Hovered() || e.pdfDragID == id {
				grip.Background(t.Accent.Alpha(0.32))
			} else {
				grip.Background(e.page.CodeBorder)
			}
			_, dragDY, pressed := grip.Dragged()
			if pressed {
				if e.pdfDragID != id {
					e.pdfDragID = id
					e.pdfDragStartH = box.H
				}
				if e.pdfHeights == nil {
					e.pdfHeights = map[string]float32{}
				}
				baseH := e.pdfHeights[id]
				if baseH == 0 {
					baseH = e.pdfDragStartH
				}
				e.pdfHeights[id] = max(pdfMinInlineH, min(pdfMaxInlineH, baseH+dragDY))
				e.ready = false
			} else if e.pdfDragID == id {
				e.pdfDragID = ""
			}
			continue
		}

		// Error recovery buttons on failed PDF card.
		if r.pdfError != nil {
			clean, _ := ParsePDFURLPage(b.URL)
			btnRow := ui.Row(c.Key("pdf-err-btns:" + id)).Absolute().Left(box.X).Top(box.Y + box.H - 38).
				Width(box.W).Height(30).Gap(8).Justify(ui.Center).AlignItems(ui.Center)
			btnRow.Children(func() {
				if !e.ReadOnly && o.ReplaceImage != nil {
					rep := ui.ButtonBase(c.Key("rep")).Height(26).PaddingX(10).Radius(7).
						Background(e.page.Popover).Border(1, e.page.TableLine).Center().Cursor(ui.CursorPointer)
					if rep.Hovered() {
						rep.Background(e.page.Hover)
					}
					rep.Children(func() { ui.Text(c, i18n.T("重新选择文件…")).FontSize(12).TextColor(t.Text) })
					if rep.Clicked() {
						o.ReplaceImage(id, b.URL)
					}
				}
				if o.RevealImage != nil {
					rev := ui.ButtonBase(c.Key("rev")).Height(26).PaddingX(10).Radius(7).
						Background(e.page.Popover).Border(1, e.page.TableLine).Center().Cursor(ui.CursorPointer)
					if rev.Hovered() {
						rev.Background(e.page.Hover)
					}
					rev.Children(func() { ui.Text(c, i18n.T("在文件夹中显示")).FontSize(12).TextColor(t.Text) })
					if rev.Clicked() {
						o.RevealImage(clean)
					}
				}
			})
			continue
		}

		if r.pdfDoc == nil {
			continue
		}

		// Rest-mode hit box: supports focus, keyboard navigation, double-click to fullscreen, and context menu,
		// WITHOUT capturing wheel scroll (so scrolling the Markdown document stays 100% smooth!).
		hit := ui.Box(c.Key("pdf-rest:"+id)).Absolute().Left(box.X).Top(box.Y).
			Size(box.W, max(24, box.H)).Focusable().Role(ui.RoleButton).
			Label("PDF: " + imageName(b.URL)).Cursor(ui.CursorPointer)
		if hit.Clicked() {
			e.FocusBlock(id)
		}
		if hit.DoubleClicked() || (hit.Focused() && hit.Shortcut(0, ui.KeyEnter)) {
			e.openPDFModal(id)
		}
		if hit.Focused() {
			hit.Border(2, t.Accent).Radius(10)
		}
		hit.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputKeyDown {
				return false
			}
			switch ev.Key {
			case ui.KeyBackspace, ui.KeyDelete:
				if !e.ReadOnly {
					e.DeleteBlock(id)
					e.RequestFocus = true
					return true
				}
			case ui.KeyLeft:
				e.stepRestPDFPage(b, r.pdfDoc, -1)
				return true
			case ui.KeyRight:
				e.stepRestPDFPage(b, r.pdfDoc, 1)
				return true
			case ui.KeyUp:
				e.SetSelection(max(0, r.leaf.Start-1), max(0, r.leaf.Start-1))
				e.RequestFocus = true
				return true
			case ui.KeyDown:
				e.SetSelection(min(e.Len(), r.leaf.End+1), min(e.Len(), r.leaf.End+1))
				e.RequestFocus = true
				return true
			}
			return false
		})
		hit.ContextMenu(func(m *ui.Menu) {
			curPage := e.pdfCurrentPageForBlock(b, r.pdfDoc)
			clean, _ := ParsePDFURLPage(b.URL)
			menuAction(m, i18n.T("全屏阅读"), func() { e.openPDFModal(id) })
			menuAction(m, i18n.T("内联浏览"), func() { e.startPDFBrowsing(id) })
			if e.openPDF != nil {
				menuAction(m, i18n.T("在分屏中阅读"), func() {
					e.openPDF(WithPDFURLPage(clean, curPage+1), true)
				})
				menuAction(m, i18n.T("打开 PDF"), func() {
					e.openPDF(WithPDFURLPage(clean, curPage+1), false)
				})
			}
			if !e.ReadOnly {
				m.Separator()
				menuAction(m, fmt.Sprintf(i18n.T("在文档中固定第 %d 页"), curPage+1), func() {
					e.pinPDFBlockPage(id, curPage+1)
				})
			}
			if o.RevealImage != nil {
				m.Separator()
				menuAction(m, i18n.T("在文件夹中显示"), func() { o.RevealImage(clean) })
			}
			if !e.ReadOnly {
				m.Separator()
				menuAction(m, i18n.T("删除"), func() { e.DeleteBlock(id) })
			}
		})

		// Bottom-right quick page stepper on rest card (‹ 1 / N ›) so flipping slides
		// in Markdown takes a single click without entering interactive scroll mode.
		cnt := r.pdfDoc.PageCount()
		curPage := e.pdfCurrentPageForBlock(b, r.pdfDoc)
		wb := e.page.Whiteboard
		bg := wb.IslandFill
		bd := wb.IslandBorder
		if bg.A == 0 {
			bg = e.page.Popover
			bd = e.page.TableLine
		}
		stepper := ui.Row(c.Key("pdf-step:"+id)).Absolute().Right(e.width-box.X-box.W+10).
			Top(box.Y+box.H-34).Height(26).PaddingX(4).Gap(2).Radius(7).
			AlignItems(ui.Center).Background(bg).Border(1, bd)
		stepper.Children(func() {
			if cnt > 1 {
				prev := ui.ButtonBase(c.Key("prev")).Size(20, 20).Radius(5).Center().
					Disabled(curPage <= 0).Cursor(ui.CursorPointer).Tooltip(i18n.T("上一页"))
				if curPage > 0 && prev.Hovered() {
					prev.Background(e.page.Hover)
				}
				prevFg := t.Text
				if curPage <= 0 {
					prevFg = t.TextMuted.Alpha(0.35)
				}
				prev.Children(func() { ui.Icon(c, iconChevronLeft).FontSize(12).TextColor(prevFg) })
				if prev.Clicked() {
					e.stepRestPDFPage(b, r.pdfDoc, -1)
				}
			}
			ui.Text(c, fmt.Sprintf(i18n.T("第 %d / %d 页"), curPage+1, cnt)).
				FontSize(11.5).FontWeight(500).TextColor(t.TextMuted).PaddingX(5).NoWrap()
			if cnt > 1 {
				next := ui.ButtonBase(c.Key("next")).Size(20, 20).Radius(5).Center().
					Disabled(curPage >= cnt-1).Cursor(ui.CursorPointer).Tooltip(i18n.T("下一页"))
				if curPage < cnt-1 && next.Hovered() {
					next.Background(e.page.Hover)
				}
				nextFg := t.Text
				if curPage >= cnt-1 {
					nextFg = t.TextMuted.Alpha(0.35)
				}
				next.Children(func() { ui.Icon(c, iconChevronRight).FontSize(12).TextColor(nextFg) })
				if next.Clicked() {
					e.stepRestPDFPage(b, r.pdfDoc, 1)
				}
			}
		})

		if e.pdfModalID == "" {
			pillsRow := ui.Row(c.Key("pdf-pills:" + id)).Absolute().Right(e.width - box.X - box.W + 8).
				Top(box.Y + 8).Height(28).Gap(6).AlignItems(ui.Center)
			showPills := hit.Hovered() || stepper.Hovered() || pillsRow.Hovered() || hit.Focused()
			if !showPills {
				pillsRow.Opacity(0).PassThrough()
			}
			pillsRow.Children(func() {
				addPill := func(key string, icon *ui.SVG, label string, onClick func()) {
					btn := ui.ButtonBase(c.Key(key)).Height(26).PaddingX(9).Gap(5).Radius(7).
						AlignItems(ui.Center).Background(bg).Border(1, bd).Cursor(ui.CursorPointer)
					fg := e.page.Caption
					if btn.Hovered() {
						btn.Background(t.Background).Border(1, t.Accent.Alpha(0.45))
						fg = t.Accent
					}
					btn.Children(func() {
						ui.Icon(c, icon).FontSize(12.5).TextColor(fg)
						ui.Text(c, label).FontSize(11.5).FontWeight(500).TextColor(fg).NoWrap()
					})
					if showPills && btn.Clicked() {
						onClick()
					}
				}
				addPill("browse", iconOutline, i18n.T("内联浏览"), func() { e.startPDFBrowsing(id) })
				addPill("expand", iconMaximize, i18n.T("全屏阅读"), func() { e.openPDFModal(id) })
				if e.openPDF != nil {
					addPill("open", iconFileDoc, i18n.T("打开 PDF"), func() {
						e.openPDF(e.pdfURLAtCurrentPage(id, b.URL), false)
					})
					addPill("split", iconExternalSplit, i18n.T("在分屏中阅读"), func() {
						e.openPDF(e.pdfURLAtCurrentPage(id, b.URL), true)
					})
				}
			})
		}
	}
}

func (e *Editor) stepRestPDFPage(b *document.Block, doc *pdfdoc.Document, delta int) {
	if b == nil || doc == nil || doc.PageCount() <= 1 {
		return
	}
	cur := e.pdfCurrentPageForBlock(b, doc)
	next := min(max(cur+delta, 0), doc.PageCount()-1)
	if next == cur {
		return
	}
	if e.pdfRestPages == nil {
		e.pdfRestPages = map[string]int{}
	}
	e.pdfRestPages[b.ID] = next
	if v, ok := e.pdfs[b.ID]; ok && v != nil {
		v.GoToPage(next, true)
	}
	// Layout height may adapt if page orientation differs between pages.
	e.ready = false
}

// pdfModalView renders a full-window PDF reader modal over the editor when e.pdfModalID != "".
func (e *Editor) pdfModalView(c *ui.Context, o Options) {
	if e.pdfModalID == "" || e.printing {
		return
	}
	b := e.blockByID(e.pdfModalID)
	if b == nil || !isPDFEmbed(b) {
		e.pdfModalID = ""
		return
	}
	v := e.pdfViewerForBlock(b)
	if v == nil {
		e.pdfModalID = ""
		return
	}
	if e.pdfModalFocusPending {
		v.RequestFocus = true
		e.pdfModalFocusPending = false
	}
	t := o.Theme
	winW, winH := c.Size()
	w := min(float32(1260), max(360, winW-44))
	h := min(float32(860), max(300, winH-44))
	clean, pinned := ParsePDFURLPage(b.URL)

	ui.Overlay(c, func() {
		backdrop := ui.Box(c.Key("pdf-modal-bg")).Fill().Background(e.page.Scrim).Center()
		if backdrop.Clicked() {
			// Clicking outside panel closes modal cleanly.
		}
		if c.Shortcut(0, ui.KeyEscape) && !v.FindOpen && !v.PageInputOpen {
			if e.pdfRestPages == nil {
				e.pdfRestPages = map[string]int{}
			}
			e.pdfRestPages[b.ID] = v.CurrentPage
			e.closePDFModal(b.ID)
			return
		}
		backdrop.Children(func() {
			wb := e.page.Whiteboard
			hdrBg := wb.IslandFill
			hdrBd := wb.IslandBorder
			if hdrBg.A == 0 {
				hdrBg = e.page.Popover
				hdrBd = e.page.TableLine
			}
			panel := ui.Column(c.Key("pdf-modal-panel")).Size(w, h).Radius(14).
				Background(t.Background).Border(1, hdrBd).
				Shadow(0, 16, 48, 0, e.page.MathShadow).Clip()
			panel.Children(func() {
				hdr := ui.Row(c.Key("pdf-modal-hdr")).Height(42).PaddingX(14).Gap(8).AlignItems(ui.Center).
					Background(hdrBg).BorderWidth(0, 0, 1, 0).BorderColor(hdrBd)
				hdr.Children(func() {
					ui.Icon(c, iconFileDoc).FontSize(16).TextColor(t.Accent)
					ui.Text(c, imageName(clean)).FontSize(13).FontWeight(600).TextColor(t.Text).Grow(1).MinWidth(0).MaxLines(1)
					if e.openPDF != nil {
						spBtn := ui.ButtonBase(c.Key("pdf-modal-split")).Height(28).PaddingX(10).Radius(7).
							Border(1, hdrBd).AlignItems(ui.Center).Gap(5).Cursor(ui.CursorPointer)
						if spBtn.Hovered() {
							spBtn.Background(e.page.Hover)
						}
						spBtn.Children(func() {
							ui.Icon(c, iconExternalSplit).FontSize(13).TextColor(t.Text)
							ui.Text(c, i18n.T("在分屏中阅读")).FontSize(12).TextColor(t.Text)
						})
						if spBtn.Clicked() {
							target := WithPDFURLPage(clean, v.CurrentPage+1)
							id := b.ID
							e.closePDFModal(id)
							e.openPDF(target, true)
						}
					}
					done := ui.ButtonBase(c.Key("pdf-modal-done")).Height(28).PaddingX(12).Radius(7).
						Background(t.Accent).Center().Cursor(ui.CursorPointer)
					done.Children(func() {
						ui.Text(c, i18n.T("完成")).FontSize(12).FontWeight(600).TextColor(t.AccentText)
					})
					if done.Clicked() {
						if e.pdfRestPages == nil {
							e.pdfRestPages = map[string]int{}
						}
						e.pdfRestPages[b.ID] = v.CurrentPage
						e.closePDFModal(b.ID)
					}
				})

				var pinFn func(int)
				if !e.ReadOnly {
					pinFn = func(p1 int) { e.pinPDFBlockPage(b.ID, p1) }
				}
				var revealFn func()
				if o.RevealImage != nil {
					revealFn = func() { o.RevealImage(clean) }
				}

				ui.Column(c.Key("pdf-modal-body")).Grow(1).MinHeight(0).MinWidth(0).Children(func() {
					PDFView(c, v, PDFOptions{
						Theme:      t,
						Page:       e.page,
						Title:      imageName(clean),
						Async:      e.asyncPDF,
						PinnedPage: pinned,
						PinPage:    pinFn,
						RevealFile: revealFn,
					})
				})
			})
		})
	})
}

// ExportPDFEmbedToVectorPage renders the embedded PDF's pinned/current page at
// print resolution into an exported vector PDF document.
func ExportPDFEmbedToVectorPage(vp *pdf.VectorPage, doc *pdfdoc.Document, pageIdx int, pg Page, t *ui.Theme, dstX, dstY, dstW, dstH, sc float64) {
	if vp == nil || doc == nil || doc.PageCount() == 0 || dstW <= 0 || dstH <= 0 {
		return
	}
	pageIdx = min(max(pageIdx, 0), doc.PageCount()-1)
	info := doc.PageInfo(pageIdx)
	aspect := info.Aspect()
	if aspect <= 0 {
		aspect = 595.28 / 841.89
	}
	pad := 8.0 * sc
	availW := max(16, dstW-2*pad)
	availH := max(16, dstH-2*pad)
	pw, ph := availW, availW/aspect
	if ph > availH {
		ph = availH
		pw = ph * aspect
	}
	ox := dstX + (dstW-pw)/2
	oy := dstY + (dstH-ph)/2

	renderW := min(2400, max(600, int(math.Round(pw*2.5))))
	if img, err := doc.RenderPage(pageIdx, renderW, 0); err == nil && img != nil {
		vp.DrawImage(img, ox, oy, pw, ph, 3*sc)
		vp.StrokeRect(ox, oy, pw, ph, 3*sc, max(0.5, 0.75*sc), [4]uint8{0, 0, 0, 36}, false)
	}
	_ = pg
	_ = t
}
