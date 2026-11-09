package app

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// PDF export and printing: the document is laid out on A4 paper by the
// editor (Paginate), each page is drawn without a window (ui.Render) on
// white paper, and the pages go into a PDF (package pdf). Printing opens
// that PDF in the system's viewer, where it prints.

// Paper in DIPs, 96 to the inch: A4, with a margin above and below the
// page's text (the editor keeps its own at the sides).
const (
	paperWidth  = 794
	paperHeight = 1123
	paperMargin = 48
	paperScale  = 2
)

// renderPDF draws the open document's pages and returns the PDF.
func (a *App) renderPDF() ([]byte, int, error) {
	d := a.doc
	if d == nil || d.loadErr != nil {
		return nil, 0, errors.New("没有打开的文档")
	}
	d.ed.UnmarkText()
	ed := editor.FromDocument(d.ed.Committed())
	theme := documentTheme(false)
	o := editor.Options{Width: paperWidth, Theme: theme, Page: pageStyle(false), LoadImage: d.loadImage}
	body := float32(paperHeight - 2*paperMargin)
	var tops []float32
	ui.Render(func(c *ui.Context) { tops = ed.Paginate(o, body) }, paperWidth, paperHeight, 1)
	var pages []pdf.Page
	for i, top := range tops {
		bottom := top + body
		if i+1 < len(tops) {
			bottom = tops[i+1]
		}
		img := ui.Render(func(c *ui.Context) {
			ui.Box(c).Fill().Background(theme.Background).Draw(func(p *ui.Painter, r ui.Rect) {
				ed.PaintPage(p, ui.Rect{X: r.X, Y: r.Y + paperMargin, W: r.W, H: body}, top, bottom, theme)
				// The page number at the foot.
				label := fmt.Sprintf("%d / %d", i+1, len(tops))
				span := ui.Span{Text: label, Size: 10, Color: theme.TextMuted}
				w, _ := p.MeasureText(0, span)
				p.RichText(r.X+(r.W-w)/2, r.Y+r.H-paperMargin/2-6, 0, span)
			})
		}, paperWidth, paperHeight, paperScale)
		pages = append(pages, pdf.Page{Image: img, Width: pdf.A4Width, Height: pdf.A4Height})
	}
	var out bytes.Buffer
	if err := pdf.Write(&out, d.title(), pages); err != nil {
		return nil, 0, err
	}
	return out.Bytes(), len(pages), nil
}

// exportPDF writes the open document as a PDF beside it, under a name not
// taken yet.
func (a *App) exportPDF() {
	d := a.doc
	if d == nil || d.loadErr != nil {
		return
	}
	data, n, err := a.renderPDF()
	if err == nil {
		var rel string
		if rel, err = createNumbered(d.space.Root, path.Dir(d.rel), d.title(), ".pdf", data); err == nil {
			a.notify("已导出 %s（%d 页）", rel, n)
			if a.win != nil {
				if p, err := vault.Resolve(d.space.Root, rel); err == nil {
					mygo.Shell.ShowItemInFolder(p)
				}
			}
			return
		}
	}
	a.notify("导出 PDF 失败。%v", err)
}

// printDoc opens the document as a PDF in the system's viewer, to print.
func (a *App) printDoc() {
	d := a.doc
	if d == nil || d.loadErr != nil {
		return
	}
	data, _, err := a.renderPDF()
	if err == nil {
		dir := filepath.Join(os.TempDir(), "ArcMD 打印")
		if err = os.MkdirAll(dir, 0o755); err == nil {
			file := filepath.Join(dir, sanitizeName(d.title())+".pdf")
			if err = fsutil.WriteFileAtomic(file, data, 0o644); err == nil {
				if a.printFile != nil {
					a.printFile(file)
				} else {
					err = mygo.Shell.OpenPath(file)
				}
				if err == nil {
					a.notify("已在系统预览中打开，可从那里打印（⌘P）。")
					return
				}
			}
		}
	}
	a.notify("准备打印失败。%v", err)
}

// createNumbered writes data as a new file named title and ext in the
// folder dir of the space's folder root, numbered when the name is taken,
// and returns its path. It never replaces a file.
func createNumbered(root, dir, title, ext string, data []byte) (string, error) {
	if dir == "." {
		dir = ""
	}
	stem := sanitizeName(title)
	for i := 1; i < 1000; i++ {
		name := stem + ext
		if i > 1 {
			name = fmt.Sprintf("%s %d%s", stem, i, ext)
		}
		rel := path.Join(dir, name)
		full, err := vault.Resolve(root, rel)
		if err != nil {
			return "", err
		}
		if _, err := os.Lstat(full); err == nil {
			continue
		}
		if err := fsutil.CreateFileAtomic(full, data, 0o644); err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			return "", err
		}
		return rel, nil
	}
	return "", errors.New("no free name")
}

// sanitizeName makes a title a file name.
func sanitizeName(title string) string {
	name := strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(title))
	if name == "" || name == "." || name == ".." {
		return "文档"
	}
	return name
}
