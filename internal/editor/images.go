package editor

import (
	"net/url"
	"path"
	"strings"

	"github.com/egoist/mygo/ui"
)

// ImageProblem distinguishes recovery actions from an opaque decode failure.
type ImageProblem uint8

const (
	ImageInvalid ImageProblem = iota
	ImageMissing
	ImageUnsupported
	ImageUnreadable
	ImageRemote
)

// ImageResource is a decoded picture or the reason it could not be loaded.
// Local means the host can reveal its file or the nearest existing folder.
type ImageResource struct {
	Bitmap  *ui.Bitmap
	Problem ImageProblem
	Local   bool
}

// imageFigure owns the geometry shared by drawing, hit targets and printing.
// Like Textus, figures are centered at their natural size, constrained only by
// the column; failed pictures become a compact, actionable dashed card.
type imageFigure struct {
	resource           ImageResource
	bounds             ui.Rect
	name               string
	title, alt, reason *ui.TextLayout
	altY, reasonY      float32
	actions            []imageAction
}

type imageAction struct {
	key, label string
	bounds     ui.Rect
}

type imageViewer struct {
	open, actual bool
	name         string
	bitmap       *ui.Bitmap
	scroll       ui.ScrollState
}

// ReplaceImage preserves the current composition and makes the replacement a
// separate undo step. Invalidating both URLs also clears a cached load failure.
func (e *Editor) ReplaceImage(id, expected, next string) bool {
	e.UnmarkText()
	if !e.Engine.ReplaceImage(id, expected, next) {
		return false
	}
	delete(e.images, expected)
	delete(e.images, next)
	e.ready = false
	e.sync()
	return true
}

func imageName(src string) string {
	if u, err := url.Parse(src); err == nil && u.Host != "" {
		src = u.EscapedPath()
		if src == "" || src == "/" {
			return u.Host
		}
	}
	if decoded, err := url.PathUnescape(src); err == nil {
		src = decoded
	}
	if name := path.Base(src); name != "." && name != "/" && name != "" {
		return name
	}
	return "未命名图片"
}

func (p ImageProblem) text() string {
	switch p {
	case ImageMissing:
		return "这个路径下没有文件。"
	case ImageUnsupported:
		return "文件存在，但格式不受支持或图片已损坏。"
	case ImageUnreadable:
		return "无法读取图片，请检查文件及访问权限。"
	case ImageRemote:
		return "暂不加载网络图片，请选择本地图片。"
	default:
		return "图片路径无效，请重新选择图片。"
	}
}

func (e *Editor) imageResource(src string, o Options) ImageResource {
	if resource, ok := e.images[src]; ok {
		return resource
	}
	resource := ImageResource{}
	if o.LoadImage != nil {
		resource = o.LoadImage(src)
		e.images[src] = resource
	}
	return resource
}

func (e *Editor) layoutImage(r *row, o Options) float32 {
	b := r.leaf.Block
	f := &imageFigure{resource: e.imageResource(b.URL, o), name: imageName(b.URL)}
	r.picture = f
	x, y, w := r.rect.X, r.rect.Y+16, r.rect.W
	if bitmap := f.resource.Bitmap; bitmap != nil {
		iw, ih := bitmap.Size()
		scale := min(float32(1), w/float32(max(1, iw)))
		if e.printing {
			// A whole picture fits an A4 page; the interactive view has no
			// fixed height limit, so tall screenshots remain readable.
			scale = min(scale, 850/float32(max(1, ih)))
		}
		pw, ph := float32(iw)*scale, float32(ih)*scale
		f.bounds = ui.Rect{X: x + (w-pw)/2, Y: y, W: pw, H: ph}
		r.x, r.y = x, y+ph+8
		bottom := y + ph
		if !e.printing && e.imageCaptionID == b.ID {
			_, h := r.layout.Size()
			bottom = r.y + h
		}
		r.rect.H = bottom - r.rect.Y + 18
		return r.rect.H
	}
	title := "无法显示图片：" + f.name
	if f.resource.Problem == ImageMissing {
		title = "找不到图片：" + f.name
	}
	alt := strings.TrimSpace(b.Text())
	if alt == f.name {
		alt = ""
	}
	headFont := ui.Font{Family: "system-ui", Size: 14, Weight: 600, LineHeight: 1.45}
	altFont := ui.Font{Family: "system-ui", Size: 14, Italic: true, LineHeight: 1.45}
	reasonFont := ui.Font{Family: "system-ui", Size: 13, LineHeight: 1.45}
	natural := func(text string, font ui.Font) float32 {
		l := ui.ShapeText(text, font, 0)
		width, _ := l.Size()
		return width
	}
	cardW := min(w, max(natural(title, headFont)+22, natural(alt, altFont), natural(f.resource.Problem.text(), reasonFont), 250)+28)
	cardW = min(cardW, 640)
	f.bounds = ui.Rect{X: x + (w-cardW)/2, Y: y, W: cardW}
	inner := max(1, cardW-28)
	f.title = ui.ShapeText(title, headFont, max(1, inner-22))
	_, headH := f.title.Size()
	cursor := float32(10) + max(18, headH)
	if alt != "" {
		cursor += 2
		f.altY = cursor
		f.alt = ui.ShapeText(alt, altFont, inner)
		_, h := f.alt.Size()
		cursor += h
	}
	f.reasonY = cursor + 2
	f.reason = ui.ShapeText(f.resource.Problem.text(), reasonFont, inner)
	_, reasonH := f.reason.Size()
	cursor = f.reasonY + reasonH
	if !e.printing {
		var actions []imageAction
		if o.ReplaceImage != nil {
			actions = append(actions, imageAction{key: "choose", label: "重新选择图片…"})
		}
		if o.RevealImage != nil && f.resource.Local {
			actions = append(actions, imageAction{key: "reveal", label: "在文件夹中显示"})
		}
		if len(actions) > 0 {
			cursor += 8
			x := float32(14)
			for _, action := range actions {
				bw := min(inner, natural(action.label, ui.Font{Family: "system-ui", Size: 13})+20)
				if x > 14 && x+bw > cardW-14 {
					x = 14
					cursor += 34
				}
				action.bounds = ui.Rect{X: f.bounds.X + x, Y: y + cursor, W: bw, H: 28}
				f.actions = append(f.actions, action)
				x += bw + 6
			}
			cursor += 28
		}
	}
	f.bounds.H = cursor + 10
	r.x, r.y = f.bounds.X+14, y+f.altY
	if e.imageCaptionID == b.ID && !e.printing {
		// Captions are editable through the block's text input. Keep the
		// editing line outside the recovery card and its action targets.
		r.x, r.y = x, y+f.bounds.H+8
		_, h := r.layout.Size()
		r.rect.H = r.y + h - r.rect.Y + 18
	} else {
		r.rect.H = y + f.bounds.H - r.rect.Y + 18
	}
	return r.rect.H
}

func (e *Editor) paintImage(p *ui.Painter, bounds ui.Rect, row row, t *ui.Theme) {
	f := row.picture
	r := f.bounds
	r.X, r.Y = r.X+bounds.X, r.Y+bounds.Y
	if f.resource.Bitmap != nil {
		p.Shadow(r, 8, 0, 2, 10, 0, e.page.ImageShadow)
		p.Clip(r, 8, func() { p.Image(f.resource.Bitmap, r, ui.Contain) })
	} else {
		p.StrokeDashed(r, e.page.Caption, 8, 1)
		p.Icon(iconImage, ui.Rect{X: r.X + 14, Y: r.Y + 12, W: 16, H: 16}, e.page.Caption)
		p.TextLayout(f.title, r.X+36, r.Y+10, t.Text)
		if f.alt != nil {
			p.TextLayout(f.alt, r.X+14, r.Y+f.altY, e.page.Caption)
		}
		p.TextLayout(f.reason, r.X+14, r.Y+f.reasonY, e.page.Caption)
	}
	selection := e.Selection().Range
	if !e.printing && e.focused && (selection.Start == selection.End && e.Engine.LeafAt(e.Caret).Block.ID == row.leaf.Block.ID || selection.End > row.leaf.Start && selection.Start <= row.leaf.End) {
		p.Stroke(ui.Rect{X: r.X - 3, Y: r.Y - 3, W: r.W + 6, H: r.H + 6}, t.Accent, 10, 2)
	}
}

func (e *Editor) imageControls(c *ui.Context, o Options) {
	if e.printing {
		return
	}
	for _, row := range e.rows {
		f := row.picture
		if f == nil || row.rect.Y+row.rect.H < e.Scroll.Y || row.rect.Y > e.Scroll.Y+e.viewport {
			continue
		}
		id, src := row.leaf.Block.ID, row.leaf.Block.URL
		key := c.Key("image:" + id)
		if f.resource.Bitmap == nil {
			r := f.bounds
			ui.Box(key.Key("status")).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).
				Role(ui.RoleImage).Label("无法显示图片：" + f.name + "。" + row.leaf.Block.Text() + "。" + f.resource.Problem.text()).Tooltip(src)
		}
		for _, action := range f.actions {
			b := e.imageButton(key.Key(action.key), action.label, action.bounds)
			if b.Clicked() {
				if action.key == "choose" {
					o.ReplaceImage(id, src)
				} else {
					o.RevealImage(src)
				}
			}
		}
		if f.resource.Bitmap == nil {
			continue
		}
		r := f.bounds
		picture := ui.Box(key).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).
			Focusable().Role(ui.RoleButton).Label("查看图片：" + f.name).Tooltip(row.leaf.Block.Text()).Cursor(ui.CursorPointer)
		if picture.Clicked() {
			e.FocusBlock(id)
			if e.imageCaptionID != "" {
				e.imageCaptionID, e.ready = "", false
			}
		}
		picture.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputKeyDown {
				return false
			}
			switch ev.Key {
			case ui.KeyBackspace, ui.KeyDelete:
				e.DeleteBlock(id)
			case ui.KeyLeft, ui.KeyUp:
				e.SetSelection(max(0, row.leaf.Start-1), max(0, row.leaf.Start-1))
			case ui.KeyRight, ui.KeyDown:
				e.SetSelection(min(e.Len(), row.leaf.End+1), min(e.Len(), row.leaf.End+1))
			default:
				return false
			}
			e.RequestFocus = true
			return true
		})
		open := func() { e.imageViewer = imageViewer{open: true, name: f.name, bitmap: f.resource.Bitmap} }
		if picture.DoubleClicked() || picture.Focused() && (picture.Shortcut(0, ui.KeyEnter) || picture.Shortcut(0, ui.KeySpace)) {
			open()
		}
		if picture.Focused() {
			picture.Border(2, c.Theme().Accent).Radius(8)
		}
		picture.ContextMenu(func(m *ui.Menu) {
			menuAction(m, "放大查看", open)
			menuAction(m, "编辑图片说明…", func() {
				e.FocusBlock(id)
				e.imageCaptionID, e.ready, e.RequestFocus = id, false, true
			})
			if o.ReplaceImage != nil {
				menuAction(m, "重新选择图片…", func() { o.ReplaceImage(id, src) })
			}
			if o.RevealImage != nil && f.resource.Local {
				menuAction(m, "在文件夹中显示", func() { o.RevealImage(src) })
			}
		})
		if (picture.Hovered() || picture.Focused() || e.hovered(r, 0)) && r.W >= 100 && r.H >= 56 {
			button := e.imageButton(key.Key("zoom"), "放大查看", ui.Rect{X: r.X + r.W - 82, Y: r.Y + 8, W: 74, H: 28})
			button.Background(e.page.Popover)
			if button.Clicked() {
				open()
			}
		}
	}
}

func (e *Editor) imageButton(c *ui.Context, label string, r ui.Rect) ui.Element {
	b := ui.ButtonBase(c).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).
		Radius(14).Border(1, e.page.TableLine).Label(label).Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(e.page.Hover)
	}
	if b.FocusVisible() {
		b.Border(2, c.Theme().Accent)
	}
	b.Children(func() { ui.Text(c, label).FontSize(13).MinWidth(0).MaxLines(1).TextColor(c.Theme().Text) })
	return b
}

// Explicit typing reveals the editable description; reading and pasting keep
// filenames and alt text out of a successfully loaded picture's layout.
func (e *Editor) editImageCaption() {
	if b := e.Engine.LeafAt(e.Caret).Block; b.Kind == "image" && e.imageCaptionID != b.ID {
		e.imageCaptionID, e.ready = b.ID, false
	}
}

func (e *Editor) imagePreview(c *ui.Context) {
	v := &e.imageViewer
	if !v.open || v.bitmap == nil {
		return
	}
	w, h := c.Size()
	w, h = max(200, w-48), max(160, h-48)
	ui.DialogBase(c.Key("image-preview"), &v.open, func(backdrop, panel ui.Element) {
		backdrop.Background(e.page.Scrim)
		panel.Size(w, h).Padding(12).Gap(10).Radius(12).Background(c.Theme().Background).Label("图片预览")
		ui.Row(c).Height(32).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, v.name).Grow(1).MinWidth(0).MaxLines(1).FontSize(14).FontWeight(600)
			label := "原始大小"
			if v.actual {
				label = "适应窗口"
			}
			if ui.Button(c, label).Clicked() {
				v.actual = !v.actual
				v.scroll = ui.ScrollState{}
			}
			if ui.Button(c, "关闭").Tooltip("关闭 (Esc)").Clicked() {
				v.open = false
			}
		})
		iw, ih := v.bitmap.Size()
		vw, vh := w-24, h-66
		scale := min(float32(1), vw/float32(max(1, iw)), vh/float32(max(1, ih)))
		if v.actual {
			scale = 1
		}
		pw, ph := float32(iw)*scale, float32(ih)*scale
		ui.ScrollBoth(c.Key("preview-scroll")).Grow(1).MinHeight(0).TrackScroll(&v.scroll).Children(func() {
			ui.Box(c).Size(max(vw, pw), max(vh, ph)).Children(func() {
				ui.Image(c, v.bitmap).Absolute().Left(max(0, (vw-pw)/2)).Top(max(0, (vh-ph)/2)).Size(pw, ph)
			})
		})
	})
}
