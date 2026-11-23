package editor

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/bookdoc"
	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/egoist/mygo/ui"
)

const (
	bookDefaultInlineH = float32(480)
	bookMinInlineH     = float32(260)
	bookMaxInlineH     = float32(780)
	bookRestH          = float32(244)
	htmlRestH          = float32(340)
	bookMargin         = float32(14)
)

type embedBookCache struct {
	rev uint64
	doc *bookdoc.Document
	err error
}

// IsHTMLURL reports whether raw points to an .html or .htm resource.
func IsHTMLURL(raw string) bool {
	clean := stripURLRef(raw)
	low := strings.ToLower(path.Base(clean))
	return (len(low) > len(".html") && strings.HasSuffix(low, ".html")) ||
		(len(low) > len(".htm") && strings.HasSuffix(low, ".htm"))
}

// IsEPUBURL reports whether raw points to an .epub resource.
func IsEPUBURL(raw string) bool {
	clean := stripURLRef(raw)
	low := strings.ToLower(path.Base(clean))
	return len(low) > len(".epub") && strings.HasSuffix(low, ".epub")
}

// IsBookURL reports whether raw points to an .html, .htm, or .epub resource.
func IsBookURL(raw string) bool {
	return IsHTMLURL(raw) || IsEPUBURL(raw)
}

func stripURLRef(raw string) string {
	clean, _, _ := strings.Cut(strings.TrimSpace(raw), "#")
	clean, _, _ = strings.Cut(clean, "?")
	if decoded, err := url.PathUnescape(clean); err == nil {
		clean = decoded
	}
	return strings.ReplaceAll(clean, `\`, "/")
}

// ParseBookURLRef splits a book/HTML URL into its file path, optional 1-based
// chapter number (#chapter=N, #ch=N, or #N for EPUB), and optional #anchor.
func ParseBookURLRef(raw string) (cleanURL string, chapter1Based int, anchor string) {
	base, frag, hasFrag := strings.Cut(strings.TrimSpace(raw), "#")
	cleanURL = strings.TrimSpace(base)
	if !hasFrag {
		return cleanURL, 0, ""
	}
	frag = strings.TrimSpace(frag)
	if frag == "" {
		return cleanURL, 0, ""
	}
	if left, right, hasSub := strings.Cut(frag, "#"); hasSub {
		_, ch, anc1 := ParseBookURLRef(cleanURL + "#" + left)
		if ch > 0 {
			return cleanURL, ch, unescapeBookFrag(right)
		}
		if anc1 != "" {
			return cleanURL, 0, anc1
		}
		return cleanURL, 0, unescapeBookFrag(right)
	}
	isEPUB := IsEPUBURL(cleanURL)
	for part := range strings.SplitSeq(frag, "&") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		low := strings.ToLower(part)
		if strings.HasPrefix(low, "chapter=") {
			if n, err := strconv.Atoi(strings.TrimSpace(part[8:])); err == nil && n >= 1 {
				chapter1Based = n
			}
			continue
		}
		if strings.HasPrefix(low, "ch=") {
			if n, err := strconv.Atoi(strings.TrimSpace(part[3:])); err == nil && n >= 1 {
				chapter1Based = n
			}
			continue
		}
		if strings.HasPrefix(low, "id=") {
			anchor = unescapeBookFrag(part[3:])
			continue
		}
		if strings.HasPrefix(low, "anchor=") {
			anchor = unescapeBookFrag(part[7:])
			continue
		}
		if isEPUB && chapter1Based == 0 {
			if n, err := strconv.Atoi(part); err == nil && n >= 1 {
				chapter1Based = n
				continue
			}
		}
		if anchor == "" {
			anchor = unescapeBookFrag(part)
		}
	}
	return cleanURL, chapter1Based, anchor
}

func unescapeBookFrag(s string) string {
	s = strings.TrimSpace(s)
	if dec, err := url.PathUnescape(s); err == nil {
		return dec
	}
	return s
}

// WithBookURLChapter returns raw with "#chapter=N" set (or stripped when <= 1).
func WithBookURLChapter(raw string, chapter1Based int) string {
	clean, _, _ := ParseBookURLRef(raw)
	if chapter1Based <= 1 {
		return clean
	}
	return fmt.Sprintf("%s#chapter=%d", clean, chapter1Based)
}

func isBookEmbed(b *document.Block) bool {
	return b != nil && b.Kind == "image" && IsBookURL(b.URL)
}

func (e *Editor) startBookBrowsing(id string) {
	e.bookBrowsing = id
	e.bookFocusPending = id != ""
	e.ready = false
}

func (e *Editor) openBookModal(id string) {
	if id == "" {
		return
	}
	e.bookModalID = id
	e.bookModalFocusPending = true
	e.ready = false
}

// BrowseBookBlock activates inline interactive scrolling for embedded book block id.
func (e *Editor) BrowseBookBlock(id string) {
	e.startBookBrowsing(id)
}

// OpenBookModal opens the full-window modal reader for embedded book block id.
func (e *Editor) OpenBookModal(id string) {
	e.openBookModal(id)
}

func (e *Editor) closeBookModal(id string) {
	e.bookModalID = ""
	e.ready = false
	if e.bookBrowsing == id {
		e.bookFocusPending = true
	} else {
		e.RequestFocus = true
	}
}

func (e *Editor) bookCurrentChapterForBlock(b *document.Block, doc *bookdoc.Document) int {
	if b == nil || doc == nil || doc.ChapterCount() == 0 {
		return 0
	}
	_, pinned, _ := ParseBookURLRef(b.URL)
	ch := 0
	if pinned >= 1 {
		ch = pinned - 1
	}
	if e.bookRestChapters != nil {
		if cur, ok := e.bookRestChapters[b.ID]; ok {
			ch = cur
		}
	}
	return min(max(ch, 0), doc.ChapterCount()-1)
}

func (e *Editor) bookViewerForBlock(b *document.Block) *BookViewer {
	if b == nil {
		return nil
	}
	cleanURL, pinned, anchor := ParseBookURLRef(b.URL)
	var doc *bookdoc.Document
	var rev uint64
	var err error
	if e.loadBook != nil {
		doc, rev, err = e.loadBook(cleanURL)
	}
	if e.books == nil {
		e.books = map[string]*BookViewer{}
	}
	if e.bookEmbedRev == nil {
		e.bookEmbedRev = map[string]uint64{}
	}
	v, ok := e.books[b.ID]
	if !ok || e.bookEmbedRev[b.ID] != rev || v.Doc != doc {
		if ok && v != nil {
			v.ClearBitmaps()
		}
		v = NewBookViewer(doc, err)
		if doc != nil && doc.ChapterCount() > 0 {
			startCh := 0
			if pinned >= 1 {
				startCh = min(pinned-1, doc.ChapterCount()-1)
			}
			if e.bookRestChapters != nil {
				if rp, has := e.bookRestChapters[b.ID]; has {
					startCh = min(max(rp, 0), doc.ChapterCount()-1)
				}
			}
			if anchor != "" {
				v.GoToChapterAnchor(startCh, anchor, true)
			} else {
				v.GoToChapter(startCh, true)
			}
		}
		e.books[b.ID] = v
		e.bookEmbedRev[b.ID] = rev
	}
	return v
}

func (e *Editor) pinBookBlockChapter(id string, ch1Based int) {
	if e.ReadOnly {
		return
	}
	b := e.blockByID(id)
	if b == nil || !isBookEmbed(b) {
		return
	}
	nextURL := WithBookURLChapter(b.URL, ch1Based)
	if nextURL != b.URL {
		e.ReplaceImage(id, b.URL, nextURL)
	}
	if e.bookRestChapters == nil {
		e.bookRestChapters = map[string]int{}
	}
	e.bookRestChapters[id] = max(0, ch1Based-1)
	e.ready = false
}

func (e *Editor) layoutBookEmbed(r *row, o Options, gap float32) float32 {
	b := r.leaf.Block
	r.bookEmbed = true
	browsing := !e.printing && e.bookBrowsing == b.ID && e.bookModalID != b.ID
	r.bookBrowsing = browsing
	r.bookErr = ""
	r.bookError = nil
	r.bookDoc = nil

	availW := r.rect.W
	if b.ImageWidth > 140 {
		availW = min(availW, b.ImageWidth)
	}
	x := r.rect.X + (r.rect.W-availW)/2

	if browsing {
		h := bookDefaultInlineH
		if custom, ok := e.bookHeights[b.ID]; ok {
			h = max(bookMinInlineH, min(bookMaxInlineH, custom))
		}
		r.bookRect = ui.Rect{X: x, Y: r.rect.Y + bookMargin, W: availW, H: h}
		_ = e.bookViewerForBlock(b)
		r.rect.H = 2*bookMargin + h + gap
		return r.rect.H
	}

	cleanURL, _, _ := ParseBookURLRef(b.URL)
	if e.loadBook == nil {
		r.bookErr = i18n.T("无法打开文档")
		r.bookError = ui.ShapeText(r.bookErr, ui.Font{Family: "system-ui", Size: 13}, max(40, availW-32))
		r.bookRect = ui.Rect{X: x, Y: r.rect.Y + bookMargin, W: availW, H: 84}
		r.rect.H = 2*bookMargin + 84 + gap
		return r.rect.H
	}

	doc, rev, err := e.loadBook(cleanURL)
	if e.embedBooks == nil {
		e.embedBooks = map[string]embedBookCache{}
	}
	e.embedBooks[cleanURL] = embedBookCache{rev: rev, doc: doc, err: err}

	if err != nil || doc == nil || doc.ChapterCount() == 0 {
		msg := i18n.T("无法打开文档")
		if err != nil {
			if errors.Is(err, bookdoc.ErrEncrypted) {
				msg = i18n.T("此 EPUB 受 DRM 保护，暂不支持直接预览")
			} else {
				msg += "：" + err.Error()
			}
		}
		r.bookErr = msg
		r.bookError = ui.ShapeText(msg, ui.Font{Family: "system-ui", Size: 13}, max(40, availW-32))
		_, eh := r.bookError.Size()
		h := max(96, eh+54)
		r.bookRect = ui.Rect{X: x, Y: r.rect.Y + bookMargin, W: availW, H: h}
		r.rect.H = 2*bookMargin + h + gap
		return r.rect.H
	}

	r.bookDoc = doc
	h := bookRestH
	if e.printing {
		h = 216
	} else if !IsEPUBURL(cleanURL) && e.htmlWebView != nil {
		h = htmlRestH
	}
	r.bookRect = ui.Rect{X: x, Y: r.rect.Y + bookMargin, W: availW, H: h}
	r.rect.H = 2*bookMargin + h + gap
	_ = o
	return r.rect.H
}

func (e *Editor) paintBookRow(p *ui.Painter, bounds ui.Rect, r row, t *ui.Theme) bool {
	if !r.bookEmbed {
		return false
	}
	if r.bookBrowsing {
		return true // painted by interactive BookView in bookControls
	}
	dx, dy := e.visualOffset(r)
	box := r.bookRect
	box.X += bounds.X + dx
	box.Y += bounds.Y + dy

	cleanURL, pinned, _ := ParseBookURLRef(r.leaf.Block.URL)
	fileName := imageName(cleanURL)
	isEPUB := IsEPUBURL(cleanURL)
	caretSelected := !e.printing && e.focused && len(e.blocks.ids) == 0 && e.Anchor == e.Caret && e.Engine.LeafAt(e.Caret).Block.ID == r.leaf.Block.ID

	if r.bookError != nil {
		p.Fill(box, t.Background.Mix(t.Text, 0.025), 10)
		p.StrokeDashed(box, e.page.Caption, 10, 1)
		ic := iconFileCode
		if isEPUB {
			ic = iconBookOpen
		}
		p.Icon(ic, ui.Rect{X: box.X + (box.W-20)/2, Y: box.Y + 14, W: 20, H: 20}, e.page.Caption)
		ew, _ := r.bookError.Size()
		p.TextLayout(r.bookError, box.X+(box.W-ew)/2, box.Y+42, t.TextMuted)
		if caretSelected {
			p.Stroke(box, t.Accent, 10, 2)
		}
		return true
	}

	if r.bookDoc == nil {
		return true
	}

	desk := t.Background.Mix(t.Text, 0.035)
	p.Fill(box, desk, 10)
	border := e.page.CodeBorder
	strokeW := float32(1)
	switch {
	case caretSelected:
		border = t.Accent
		strokeW = 2
	case !e.printing && e.hovered(r.bookRect, 0):
		border = t.Accent.Alpha(0.45)
	}
	p.Stroke(box, border, 10, strokeW)

	// Inner reading sheet.
	pad := float32(12)
	sheet := ui.Rect{
		X: box.X + pad,
		Y: box.Y + pad,
		W: max(40, box.W-2*pad),
		H: max(40, box.H-2*pad),
	}
	p.Shadow(sheet, 7, 0, 3, 12, 0, ui.RGBA(0, 0, 0, 0.08))
	p.Fill(sheet, t.Background, 7)
	p.Stroke(sheet, e.page.TableLine, 7, 1)

	v := e.bookViewerForBlock(r.leaf.Block)
	curCh := e.bookCurrentChapterForBlock(r.leaf.Block, r.bookDoc)

	p.Clip(sheet, 7, func() {
		innerX := sheet.X + 16
		innerY := sheet.Y + 14
		innerW := max(40, sheet.W-32)

		var covBmp *ui.Bitmap
		if isEPUB && v != nil && sheet.W >= 320 {
			covBmp = v.CoverBitmap()
		}
		if covBmp != nil {
			const covW, covH = float32(84), float32(118)
			covRect := ui.Rect{X: innerX, Y: innerY + 2, W: covW, H: covH}
			p.Shadow(covRect, 4, 0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.14))
			p.Fill(covRect, ui.RGB(255, 255, 255), 4)
			p.Clip(covRect, 4, func() {
				p.Image(covBmp, covRect, ui.Cover)
			})
			p.Stroke(covRect, ui.RGBA(0, 0, 0, 0.10), 4, 1)
			innerX += covW + 16
			innerW = max(40, innerW-covW-16)
		}

		// Format pill + title + author header.
		tag := "HTML"
		if isEPUB {
			tag = "EPUB"
		}
		tagLayout := ui.ShapeText(tag, ui.Font{Family: "system-ui", Size: 10, Weight: 650}, 0)
		tw, th := tagLayout.Size()
		tagBox := ui.Rect{X: innerX, Y: innerY + 1, W: tw + 12, H: th + 4}
		p.Fill(tagBox, t.Accent.Alpha(0.12), 5)
		p.TextLayout(tagLayout, tagBox.X+6, tagBox.Y+2, t.Accent)

		docTitle := strings.TrimSpace(r.bookDoc.Title())
		if docTitle == "" {
			docTitle = fileName
		}
		titleFont := ui.Font{Family: "system-ui", Size: 14, Weight: 650}
		titleLayout := ui.ShapeText(docTitle, titleFont, max(40, innerW-tagBox.W-8))
		titleW, titleH := titleLayout.Size()
		p.Clip(ui.Rect{X: tagBox.X + tagBox.W + 8, Y: innerY - 2, W: max(20, innerW-tagBox.W-8), H: titleH + 4}, 0, func() {
			p.TextLayout(titleLayout, tagBox.X+tagBox.W+8, innerY-1, t.Text)
		})
		_ = titleW

		y := innerY + max(tagBox.H, titleH) + 4
		if author := strings.TrimSpace(r.bookDoc.Author()); author != "" {
			sub := author
			if isEPUB && r.bookDoc.ChapterCount() > 1 {
				sub = fmt.Sprintf("%s · %s", author, fmt.Sprintf(i18n.T("共 %d 章"), r.bookDoc.ChapterCount()))
			}
			subLayout := ui.ShapeText(sub, ui.Font{Family: "system-ui", Size: 11.5}, innerW)
			_, sh := subLayout.Size()
			p.TextLayout(subLayout, innerX, y, t.TextMuted)
			y += sh + 6
		} else if isEPUB && r.bookDoc.ChapterCount() > 1 {
			subLayout := ui.ShapeText(fmt.Sprintf(i18n.T("共 %d 章"), r.bookDoc.ChapterCount()), ui.Font{Family: "system-ui", Size: 11.5}, innerW)
			_, sh := subLayout.Size()
			p.TextLayout(subLayout, innerX, y, t.TextMuted)
			y += sh + 6
		} else {
			y += 2
		}

		p.Line(innerX, y, innerX+innerW, y, 1, e.page.TableLine)
		y += 8

		heading, paras := r.bookDoc.ChapterPreview(curCh, 4)
		if isEPUB && heading != "" && heading != docTitle {
			hLayout := ui.ShapeText(heading, ui.Font{Family: "system-ui", Size: 12.5, Weight: 600}, innerW)
			_, hh := hLayout.Size()
			p.TextLayout(hLayout, innerX, y, t.Text)
			y += hh + 5
		}

		if len(paras) > 0 {
			excerpt := strings.Join(paras, "\n")
			bodyLayout := ui.ShapeText(excerpt, ui.Font{Family: "system-ui", Size: 12.5, LineHeight: 1.5}, innerW)
			p.Clip(ui.Rect{X: innerX, Y: y, W: innerW, H: max(0, sheet.Y+sheet.H-y-34)}, 0, func() {
				p.TextLayout(bodyLayout, innerX, y, t.TextMuted)
			})
		}
	})

	if !e.printing {
		wb := e.page.Whiteboard
		bg := wb.IslandFill
		bd := wb.IslandBorder
		if bg.A == 0 {
			bg = e.page.Popover
			bd = e.page.TableLine
		}
		label := fileName
		if isEPUB && pinned > 1 {
			label = fmt.Sprintf("%s · #%d", fileName, pinned)
		}
		badge := ui.ShapeText(label, ui.Font{Family: "system-ui", Size: 11.5, Weight: 500}, max(60, box.W*0.52))
		bw, bh := badge.Size()
		pill := ui.Rect{X: box.X + 18, Y: box.Y + box.H - bh - 20, W: min(box.W*0.56, bw+28), H: bh + 8}
		p.Fill(pill, bg, 7)
		p.Stroke(pill, bd, 7, 1)
		ic := iconFileCode
		if isEPUB {
			ic = iconBookOpen
		}
		p.Icon(ic, ui.Rect{X: pill.X + 7, Y: pill.Y + (pill.H-13)/2, W: 13, H: 13}, t.Accent)
		p.Clip(ui.Rect{X: pill.X + 23, Y: pill.Y, W: max(0, pill.W-28), H: pill.H}, 0, func() {
			p.TextLayout(badge, pill.X+23, pill.Y+4, t.Text)
		})
	}
	return true
}

func (e *Editor) paintBookHover(p *ui.Painter, bounds ui.Rect, d decoration, t *ui.Theme) {
	_, _, _, _ = p, bounds, d, t
}

func (e *Editor) bookURLAtCurrentChapter(id, rawURL string) string {
	b := e.blockByID(id)
	if b == nil {
		return rawURL
	}
	clean, _, anc := ParseBookURLRef(rawURL)
	if e.loadBook != nil {
		if doc, _, err := e.loadBook(clean); err == nil && doc != nil && doc.Kind() == bookdoc.KindEPUB {
			cur := e.bookCurrentChapterForBlock(b, doc)
			return WithBookURLChapter(clean, cur+1)
		}
	}
	if anc != "" {
		return clean + "#" + anc
	}
	return rawURL
}

func (e *Editor) bookControls(c *ui.Context, o Options) {
	if e.printing {
		return
	}
	t := o.Theme

	for i := range e.visibleRows(1) {
		r := *e.indexedRow(i)
		if !r.bookEmbed || r.bookRect.W <= 0 {
			continue
		}
		b := r.leaf.Block
		id := b.ID
		dy := e.ownerVisualDY(r.leaf.Owner)
		box := r.bookRect
		box.Y += dy

		clean, pinned, _ := ParseBookURLRef(b.URL)
		isEPUB := IsEPUBURL(clean)

		if id == e.bookBrowsing && e.bookModalID != id {
			v := e.bookViewerForBlock(b)
			if v == nil {
				continue
			}
			if e.bookFocusPending {
				v.RequestFocus = true
				e.bookFocusPending = false
			}
			var pinFn func(int)
			if !e.ReadOnly && isEPUB {
				pinFn = func(ch1 int) { e.pinBookBlockChapter(id, ch1) }
			}
			var splitFn func(int, string)
			if e.openBook != nil {
				splitFn = func(ch1 int, anc string) {
					target := clean
					if isEPUB {
						target = WithBookURLChapter(clean, ch1)
					} else if anc != "" {
						target = clean + "#" + anc
					}
					e.openBook(target, true)
				}
			}
			var revealFn func()
			if o.RevealImage != nil {
				revealFn = func() { o.RevealImage(clean) }
			}
			var browserFn func()
			if !isEPUB && e.openBookInBrowser != nil {
				browserFn = func() { e.openBookInBrowser(clean) }
			}
			var extImgFn func(string) ImageResource
			if e.loadBookExternalImg != nil {
				extImgFn = func(src string) ImageResource {
					return e.loadBookExternalImg(clean, src)
				}
			}

			var wvFn func() HTMLWebView
			if !isEPUB && e.htmlWebView != nil {
				url := b.URL
				slot := "browse:" + id
				wvFn = func() HTMLWebView { return e.htmlWebView(url, slot, false) }
			}

			card := ui.Column(c.Key("inline-book:"+id)).Absolute().Left(box.X).Top(box.Y).Size(box.W, box.H).
				Radius(10).Background(t.Background).Border(1.25, t.Accent.Alpha(0.45)).
				Shadow(0, 4, 18, 0, e.page.MathShadow).Clip()
			card.Children(func() {
				BookView(c, v, BookOptions{
					Theme:             t,
					Page:              e.page,
					Title:             imageName(clean),
					Compact:           true,
					Async:             e.asyncPDF,
					FontSize:          e.readingSize,
					LineHeight:        e.readingLine,
					FontFamily:        e.fontFamily,
					PinnedChapter:     pinned,
					PinChapter:        pinFn,
					ExpandModal:       func() { e.openBookModal(id) },
					OpenSplit:         splitFn,
					RevealFile:        revealFn,
					OpenInBrowser:     browserFn,
					OpenExternalURL:   e.openLink,
					LoadExternalImage: extImgFn,
					WebView:           wvFn,
					Done: func() {
						if e.bookRestChapters == nil {
							e.bookRestChapters = map[string]int{}
						}
						e.bookRestChapters[id] = v.CurrentChapter
						e.bookBrowsing = ""
						e.ready = false
						e.RequestFocus = true
					},
				})
			})

			grip := ui.Box(c.Key("book-resize:"+id)).Absolute().Left(box.X+box.W/2-36).Top(box.Y+box.H-5).
				Size(72, 10).Radius(5).Cursor(ui.CursorResizeNS).Tooltip(i18n.T("拖动调整阅读框高度"))
			if grip.Hovered() || e.bookDragID == id {
				grip.Background(t.Accent.Alpha(0.32))
			} else {
				grip.Background(e.page.CodeBorder)
			}
			_, dragDY, pressed := grip.Dragged()
			if pressed {
				if e.bookDragID != id {
					e.bookDragID = id
					e.bookDragStartH = box.H
				}
				if e.bookHeights == nil {
					e.bookHeights = map[string]float32{}
				}
				baseH := e.bookHeights[id]
				if baseH == 0 {
					baseH = e.bookDragStartH
				}
				e.bookHeights[id] = max(bookMinInlineH, min(bookMaxInlineH, baseH+dragDY))
				e.ready = false
			} else if e.bookDragID == id {
				e.bookDragID = ""
			}
			continue
		}

		// Error recovery buttons on failed book card.
		if r.bookError != nil {
			btnRow := ui.Row(c.Key("book-err-btns:" + id)).Absolute().Left(box.X).Top(box.Y + box.H - 38).
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

		if r.bookDoc == nil {
			continue
		}

		kindPrefix := "HTML: "
		openLabel := i18n.T("打开 HTML")
		if isEPUB {
			kindPrefix = "EPUB: "
			openLabel = i18n.T("打开 EPUB")
		}
		var liveRestWV HTMLWebView
		if !isEPUB && e.htmlWebView != nil && e.bookModalID != id {
			liveRestWV = e.htmlWebView(b.URL, "rest:"+id, true)
		}
		useLiveRest := liveRestWV != nil && liveRestWV.Native() != nil
		if useLiveRest {
			sheet := ui.Rect{
				X: box.X + 12,
				Y: box.Y + 12,
				W: max(40, box.W-24),
				H: max(40, box.H-24),
			}
			ui.WebView(c.Key("book-html-rest-wv:"+id), liveRestWV.Native()).
				Absolute().Left(sheet.X).Top(sheet.Y).Size(sheet.W, sheet.H).Radius(7)
		}
		hit := ui.Box(c.Key("book-rest:"+id)).Absolute().Left(box.X).Top(box.Y).
			Size(box.W, max(24, box.H)).Focusable().Role(ui.RoleButton).
			Label(kindPrefix + imageName(b.URL)).Cursor(ui.CursorPointer)
		if hit.Hovered() {
			e.hoverID = r.leaf.Owner
		}
		if hit.Clicked() {
			e.FocusBlock(id)
		}
		if hit.DoubleClicked() || (hit.Focused() && hit.Shortcut(0, ui.KeyEnter)) {
			e.openBookModal(id)
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
				if e.ReadOnly {
					e.refused = true
					return true
				}
				e.deleteAtomicBlock(id, ev.Key == ui.KeyBackspace)
				e.RequestFocus = true
				return true
			case ui.KeyLeft:
				if isEPUB {
					e.stepRestBookChapter(b, r.bookDoc, -1)
					return true
				}
			case ui.KeyRight:
				if isEPUB {
					e.stepRestBookChapter(b, r.bookDoc, 1)
					return true
				}
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
			curCh := e.bookCurrentChapterForBlock(b, r.bookDoc)
			menuAction(m, i18n.T("全屏阅读"), func() { e.openBookModal(id) })
			menuAction(m, i18n.T("内联浏览"), func() { e.startBookBrowsing(id) })
			if e.openBook != nil {
				menuAction(m, i18n.T("在分屏中阅读"), func() {
					e.openBook(e.bookURLAtCurrentChapter(id, b.URL), true)
				})
				menuAction(m, openLabel, func() {
					e.openBook(e.bookURLAtCurrentChapter(id, b.URL), false)
				})
			}
			if !e.ReadOnly && isEPUB {
				m.Separator()
				menuAction(m, fmt.Sprintf(i18n.T("在文档中固定第 %d 章"), curCh+1), func() {
					e.pinBookBlockChapter(id, curCh+1)
				})
			}
			if !isEPUB && e.openBookInBrowser != nil {
				m.Separator()
				menuAction(m, i18n.T("在默认浏览器中打开"), func() { e.openBookInBrowser(clean) })
			}
			if o.RevealImage != nil {
				m.Separator()
				menuAction(m, i18n.T("在文件夹中显示"), func() { o.RevealImage(clean) })
			}
			if !e.ReadOnly {
				m.Separator()
				menuAction(m, i18n.T("删除"), func() {
					e.deleteAtomicBlock(id, true)
					e.RequestFocus = true
				})
			}
		})

		wb := e.page.Whiteboard
		bg := wb.IslandFill
		bd := wb.IslandBorder
		if bg.A == 0 {
			bg = e.page.Popover
			bd = e.page.TableLine
		}

		if useLiveRest {
			badgeRow := ui.Row(c.Key("book-html-badge:"+id)).Absolute().
				Left(box.X+18).Top(box.Y+box.H-40).Height(26).PaddingX(8).Gap(5).Radius(7).
				AlignItems(ui.Center).Background(bg).Border(1, bd).PassThrough()
			badgeRow.Children(func() {
				ui.Icon(c, iconFileCode).FontSize(13).TextColor(t.Accent)
				ui.Text(c, imageName(clean)).FontSize(11.5).FontWeight(500).TextColor(t.Text).
					MaxWidth(max(60, box.W*0.52)).MaxLines(1)
			})
		}

		var stepperHover bool
		if isEPUB && r.bookDoc.ChapterCount() > 1 {
			cnt := r.bookDoc.ChapterCount()
			curCh := e.bookCurrentChapterForBlock(b, r.bookDoc)
			stepper := ui.Row(c.Key("book-step:"+id)).Absolute().Right(e.width-box.X-box.W+18).
				Top(box.Y+box.H-40).Height(26).PaddingX(4).Gap(2).Radius(7).
				AlignItems(ui.Center).Background(bg).Border(1, bd)
			stepper.Children(func() {
				prev := ui.ButtonBase(c.Key("prev")).Size(20, 20).Radius(5).Center().
					Disabled(curCh <= 0).Cursor(ui.CursorPointer).Tooltip(i18n.T("上一章"))
				if curCh > 0 && prev.Hovered() {
					prev.Background(e.page.Hover)
				}
				prevFg := t.Text
				if curCh <= 0 {
					prevFg = t.TextMuted.Alpha(0.35)
				}
				prev.Children(func() { ui.Icon(c, iconChevronLeft).FontSize(12).TextColor(prevFg) })
				if prev.Clicked() {
					e.stepRestBookChapter(b, r.bookDoc, -1)
				}

				ui.Text(c, fmt.Sprintf(i18n.T("第 %d / %d 章"), curCh+1, cnt)).
					FontSize(11.5).FontWeight(500).TextColor(t.TextMuted).PaddingX(5).NoWrap()

				next := ui.ButtonBase(c.Key("next")).Size(20, 20).Radius(5).Center().
					Disabled(curCh >= cnt-1).Cursor(ui.CursorPointer).Tooltip(i18n.T("下一章"))
				if curCh < cnt-1 && next.Hovered() {
					next.Background(e.page.Hover)
				}
				nextFg := t.Text
				if curCh >= cnt-1 {
					nextFg = t.TextMuted.Alpha(0.35)
				}
				next.Children(func() { ui.Icon(c, iconChevronRight).FontSize(12).TextColor(nextFg) })
				if next.Clicked() {
					e.stepRestBookChapter(b, r.bookDoc, 1)
				}
			})
			stepperHover = stepper.Hovered()
		}

		if e.bookModalID == "" {
			pillsRow := ui.Row(c.Key("book-pills:" + id)).Absolute().Right(e.width - box.X - box.W + 16).
				Top(box.Y + 14).Height(28).Gap(6).AlignItems(ui.Center)
			showPills := hit.Hovered() || stepperHover || pillsRow.Hovered() || hit.Focused()
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
				addPill("browse", iconOutline, i18n.T("内联浏览"), func() { e.startBookBrowsing(id) })
				addPill("expand", iconMaximize, i18n.T("全屏阅读"), func() { e.openBookModal(id) })
				if e.openBook != nil {
					ic := iconFileCode
					if isEPUB {
						ic = iconBookOpen
					}
					addPill("open", ic, openLabel, func() {
						e.openBook(e.bookURLAtCurrentChapter(id, b.URL), false)
					})
					addPill("split", iconExternalSplit, i18n.T("在分屏中阅读"), func() {
						e.openBook(e.bookURLAtCurrentChapter(id, b.URL), true)
					})
				}
			})
		}
	}
}

func (e *Editor) stepRestBookChapter(b *document.Block, doc *bookdoc.Document, delta int) {
	if b == nil || doc == nil || doc.ChapterCount() <= 1 {
		return
	}
	cur := e.bookCurrentChapterForBlock(b, doc)
	next := min(max(cur+delta, 0), doc.ChapterCount()-1)
	if next == cur {
		return
	}
	if e.bookRestChapters == nil {
		e.bookRestChapters = map[string]int{}
	}
	e.bookRestChapters[b.ID] = next
	if v, ok := e.books[b.ID]; ok && v != nil {
		v.GoToChapter(next, true)
	}
	e.ready = false
}

// bookModalView renders a full-window HTML/EPUB reader modal over the editor when e.bookModalID != "".
func (e *Editor) bookModalView(c *ui.Context, o Options) {
	if e.bookModalID == "" || e.printing {
		return
	}
	b := e.blockByID(e.bookModalID)
	if b == nil || !isBookEmbed(b) {
		e.bookModalID = ""
		return
	}
	v := e.bookViewerForBlock(b)
	if v == nil {
		e.bookModalID = ""
		return
	}
	if e.bookModalFocusPending {
		v.RequestFocus = true
		e.bookModalFocusPending = false
	}
	t := o.Theme
	winW, winH := c.Size()
	w := min(float32(1260), max(360, winW-44))
	h := min(float32(860), max(300, winH-44))
	clean, pinned, _ := ParseBookURLRef(b.URL)
	isEPUB := IsEPUBURL(clean)

	ui.Overlay(c, func() {
		backdrop := ui.Box(c.Key("book-modal-bg")).Fill().Background(e.page.Scrim).Center()
		if backdrop.Clicked() {
		}
		if c.Shortcut(0, ui.KeyEscape) && !v.FindOpen {
			if e.bookRestChapters == nil {
				e.bookRestChapters = map[string]int{}
			}
			e.bookRestChapters[b.ID] = v.CurrentChapter
			e.closeBookModal(b.ID)
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
			panel := ui.Column(c.Key("book-modal-panel")).Size(w, h).Radius(14).
				Background(t.Background).Border(1, hdrBd).
				Shadow(0, 16, 48, 0, e.page.MathShadow).Clip()
			panel.Children(func() {
				hdr := ui.Row(c.Key("book-modal-hdr")).Height(42).PaddingX(14).Gap(8).AlignItems(ui.Center).
					Background(hdrBg).BorderWidth(0, 0, 1, 0).BorderColor(hdrBd)
				hdr.Children(func() {
					ic := iconFileCode
					if isEPUB {
						ic = iconBookOpen
					}
					ui.Icon(c, ic).FontSize(16).TextColor(t.Accent)
					ui.Text(c, imageName(clean)).FontSize(13).FontWeight(600).TextColor(t.Text).Grow(1).MinWidth(0).MaxLines(1)
					if !isEPUB && e.openBookInBrowser != nil {
						brBtn := ui.ButtonBase(c.Key("book-modal-browser")).Height(28).PaddingX(10).Radius(7).
							Border(1, hdrBd).AlignItems(ui.Center).Gap(5).Cursor(ui.CursorPointer)
						if brBtn.Hovered() {
							brBtn.Background(e.page.Hover)
						}
						brBtn.Children(func() {
							ui.Icon(c, iconExternalLink).FontSize(13).TextColor(t.Text)
							ui.Text(c, i18n.T("在默认浏览器中打开")).FontSize(12).TextColor(t.Text)
						})
						if brBtn.Clicked() {
							e.openBookInBrowser(clean)
						}
					}
					if e.openBook != nil {
						spBtn := ui.ButtonBase(c.Key("book-modal-split")).Height(28).PaddingX(10).Radius(7).
							Border(1, hdrBd).AlignItems(ui.Center).Gap(5).Cursor(ui.CursorPointer)
						if spBtn.Hovered() {
							spBtn.Background(e.page.Hover)
						}
						spBtn.Children(func() {
							ui.Icon(c, iconExternalSplit).FontSize(13).TextColor(t.Text)
							ui.Text(c, i18n.T("在分屏中阅读")).FontSize(12).TextColor(t.Text)
						})
						if spBtn.Clicked() {
							target := clean
							if isEPUB {
								target = WithBookURLChapter(clean, v.CurrentChapter+1)
							}
							id := b.ID
							e.closeBookModal(id)
							e.openBook(target, true)
						}
					}
					done := ui.ButtonBase(c.Key("book-modal-done")).Height(28).PaddingX(12).Radius(7).
						Background(t.Accent).Center().Cursor(ui.CursorPointer)
					done.Children(func() {
						ui.Text(c, i18n.T("完成")).FontSize(12).FontWeight(600).TextColor(t.AccentText)
					})
					if done.Clicked() {
						if e.bookRestChapters == nil {
							e.bookRestChapters = map[string]int{}
						}
						e.bookRestChapters[b.ID] = v.CurrentChapter
						e.closeBookModal(b.ID)
					}
				})

				var pinFn func(int)
				if !e.ReadOnly && isEPUB {
					pinFn = func(ch1 int) { e.pinBookBlockChapter(b.ID, ch1) }
				}
				var revealFn func()
				if o.RevealImage != nil {
					revealFn = func() { o.RevealImage(clean) }
				}
				var browserFn func()
				if !isEPUB && e.openBookInBrowser != nil {
					browserFn = func() { e.openBookInBrowser(clean) }
				}
				var extImgFn func(string) ImageResource
				if e.loadBookExternalImg != nil {
					extImgFn = func(src string) ImageResource {
						return e.loadBookExternalImg(clean, src)
					}
				}
				var modalWVFn func() HTMLWebView
				if !isEPUB && e.htmlWebView != nil {
					url := b.URL
					slot := "modal:" + b.ID
					modalWVFn = func() HTMLWebView { return e.htmlWebView(url, slot, false) }
				}

				ui.Column(c.Key("book-modal-body")).Grow(1).MinHeight(0).MinWidth(0).Children(func() {
					BookView(c, v, BookOptions{
						Theme:             t,
						Page:              e.page,
						Title:             imageName(clean),
						Async:             e.asyncPDF,
						FontSize:          e.readingSize,
						LineHeight:        e.readingLine,
						FontFamily:        e.fontFamily,
						Column:            e.column,
						PinnedChapter:     pinned,
						PinChapter:        pinFn,
						RevealFile:        revealFn,
						OpenInBrowser:     browserFn,
						OpenExternalURL:   e.openLink,
						LoadExternalImage: extImgFn,
						WebView:           modalWVFn,
					})
				})
			})
		})
	})
}

// ExportBookEmbedToVectorPage renders an embedded HTML/EPUB card into an
// exported vector PDF page.
func ExportBookEmbedToVectorPage(vp *pdf.VectorPage, doc *bookdoc.Document, chIdx int, pg Page, t *ui.Theme, dstX, dstY, dstW, dstH, sc float64) {
	if vp == nil || doc == nil || doc.ChapterCount() == 0 || dstW <= 0 || dstH <= 0 {
		return
	}
	chIdx = min(max(chIdx, 0), doc.ChapterCount()-1)
	vp.FillRect(dstX, dstY, dstW, dstH, 6*sc, pdfRGBA(t.Background))
	vp.StrokeRect(dstX, dstY, dstW, dstH, 6*sc, max(0.5, 0.75*sc), pdfRGBA(pg.TableLine), false)

	pad := 14.0 * sc
	ox := dstX + pad
	oy := dstY + pad + 12*sc
	availW := max(24, dstW-2*pad)

	title := strings.TrimSpace(doc.Title())
	if title == "" {
		title = "Document"
	}
	tag := "HTML"
	if doc.Kind() == bookdoc.KindEPUB {
		tag = "EPUB"
	}
	vp.DrawText(tag+" · "+title, ox, oy, availW, 11*sc, pdf.TextStyle{Family: pdf.FontSans, Bold: true}, pdfRGBA(t.Text))
	oy += 18 * sc
	heading, paras := doc.ChapterPreview(chIdx, 3)
	if heading != "" && heading != title {
		vp.DrawText(heading, ox, oy, availW, 10*sc, pdf.TextStyle{Family: pdf.FontSans, Bold: true}, pdfRGBA(t.TextMuted))
		oy += 15 * sc
	}
	for _, p := range paras {
		if oy > dstY+dstH-pad {
			break
		}
		vp.DrawText(p, ox, oy, availW, 9.5*sc, pdf.TextStyle{Family: pdf.FontSans}, pdfRGBA(t.TextMuted))
		oy += 14 * sc
	}
}
