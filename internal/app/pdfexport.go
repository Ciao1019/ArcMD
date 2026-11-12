package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type pdfExport struct {
	source                          *document
	title                           string
	ed                              *editor.Editor
	tops                            []float32
	file                            *os.File
	writer                          *pdf.Writer
	page                            int
	open, printing, busy, cancelled bool
	next                            time.Time
}

func (a *App) exportPDF() { a.startPDF(false) }
func (a *App) printDoc()  { a.startPDF(true) }

func (a *App) startPDF(printing bool) {
	d := a.doc
	if d == nil || d.loadErr != nil || a.pdfExport != nil {
		return
	}
	d.ed.UnmarkText()
	source := &document{space: &space.Space{Root: d.space.Root}, rel: d.rel}
	dir := filepath.Dir(d.fullPath())
	if printing {
		dir = filepath.Join(os.TempDir(), i18n.T("ArcMD 打印"))
		if err := os.MkdirAll(dir, 0700); err != nil {
			a.notify("准备打印失败。%v", err)
			return
		}
	}
	file, err := os.CreateTemp(dir, ".arcmd-export-*.pdf")
	if err != nil {
		a.notify("导出 PDF 失败。%v", err)
		return
	}
	a.pdfExport = &pdfExport{source: source, title: d.title(), ed: editor.FromDocument(d.ed.Committed()), file: file,
		writer: pdf.NewWriter(file, d.title()), open: true, printing: printing, next: a.now.Add(time.Millisecond)}
}

func (a *App) cancelPDF() {
	job := a.pdfExport
	if job == nil {
		return
	}
	job.open, job.cancelled = false, true
	if !job.busy {
		a.discardPDF(job)
	}
}

func (a *App) discardPDF(job *pdfExport) {
	_ = job.file.Close()
	_ = os.Remove(job.file.Name())
	if a.pdfExport == job {
		a.pdfExport = nil
	}
}

func (a *App) pdfExportView(c *ui.Context) {
	job := a.pdfExport
	if job == nil {
		return
	}
	if job.open {
		ui.Modal(c.Key("pdf-export"), &job.open, func() {
			ui.Column(c).Width(300).Gap(12).Children(func() {
				ui.Text(c, i18n.T("正在导出 PDF")).FontSize(18).FontWeight(600)
				ui.Text(c, job.title).SingleLine().MinWidth(0).TextColor(c.Theme().TextMuted)
				status := i18n.T("正在准备页面…")
				if job.tops != nil {
					status = fmt.Sprintf(i18n.T("已完成 %d / %d 页"), job.page, len(job.tops))
				}
				ui.Text(c, status).Label(i18n.T("导出进度"))
				if ui.Button(c.Key("cancel-pdf"), i18n.T("取消导出")).Clicked() {
					job.open = false
				}
			})
		})
	}
	if !job.open {
		a.cancelPDF()
		return
	}
	if job.busy {
		return
	}
	if wait := job.next.Sub(c.Now()); wait > 0 {
		c.After(wait)
		return
	}
	job.next = c.Now().Add(time.Millisecond)
	if job.tops == nil {
		o := editor.Options{Width: paperWidth, Theme: documentTheme(false), Page: pageStyle(false), LoadImage: job.source.imageResource}
		ui.Render(func(c *ui.Context) { job.tops = job.ed.Paginate(o, paperHeight-2*paperMargin) }, 1, 1, 1)
		c.After(time.Millisecond)
		return
	}
	if job.page >= len(job.tops) {
		a.finishPDF(job)
		return
	}
	page := job.renderPage()
	// Native drawing stays on the UI thread. JPEG encoding and page IO run
	// between frames; at most one page bitmap is retained by the worker.
	job.busy = true
	done := func(err error) {
		job.busy = false
		if job.cancelled {
			a.discardPDF(job)
			return
		}
		if err != nil {
			a.discardPDF(job)
			a.notify("导出 PDF 失败。%v", err)
			return
		}
		job.page++
	}
	if a.win == nil {
		done(job.writer.AddPage(page))
		c.After(time.Millisecond)
	} else {
		go func() { err := job.writer.AddPage(page); a.win.Update(func() { done(err) }) }()
	}
}

func (job *pdfExport) renderPage() pdf.Page {
	top := job.tops[job.page]
	body := float32(paperHeight - 2*paperMargin)
	bottom := top + body
	if job.page+1 < len(job.tops) {
		bottom = job.tops[job.page+1]
	}
	theme := documentTheme(false)
	img := ui.Render(func(c *ui.Context) {
		ui.Box(c).Fill().Background(theme.Background).Draw(func(p *ui.Painter, r ui.Rect) {
			job.ed.PaintPage(p, ui.Rect{X: r.X, Y: r.Y + paperMargin, W: r.W, H: body}, top, bottom, theme)
			span := ui.Span{Text: fmt.Sprintf("%d / %d", job.page+1, len(job.tops)), Size: 10, Color: theme.TextMuted}
			w, _ := p.MeasureText(0, span)
			p.RichText(r.X+(r.W-w)/2, r.Y+r.H-paperMargin/2-6, 0, span)
		})
	}, paperWidth, paperHeight, paperScale)
	page := pdf.Page{Image: img, Width: pdf.A4Width, Height: pdf.A4Height}
	sx, sy := pdf.A4Width/paperWidth, pdf.A4Height/paperHeight
	for _, text := range job.ed.PageText(top, bottom) {
		r := text.Bounds
		page.Text = append(page.Text, pdf.Text{Value: text.Text, X: float64(r.X) * sx, Y: float64(r.Y-top+paperMargin) * sy, Width: float64(r.W) * sx, Height: float64(r.H) * sy})
	}
	return page
}

func (a *App) finishPDF(job *pdfExport) {
	err := job.writer.Close()
	if err == nil {
		err = job.file.Sync()
	}
	if closeErr := job.file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		a.discardPDF(job)
		a.notify("导出 PDF 失败。%v", err)
		return
	}
	if job.printing {
		a.pdfExport = nil
		if a.printFile != nil {
			a.printFile(job.file.Name())
		} else {
			err = mygo.Shell.OpenPath(job.file.Name())
		}
		if err != nil {
			_ = os.Remove(job.file.Name())
			a.notify("准备打印失败。%v", err)
		} else {
			a.notify("已在系统预览中打开，可从那里打印。")
		}
		return
	}
	var rel string
	for i := 1; i < 1000; i++ {
		name := sanitizeName(job.title)
		if i > 1 {
			name = fmt.Sprintf("%s %d", name, i)
		}
		rel = path.Join(path.Dir(job.source.rel), name+".pdf")
		full, resolveErr := vault.Resolve(job.source.space.Root, rel)
		if resolveErr != nil {
			err = resolveErr
			break
		}
		err = os.Link(job.file.Name(), full)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		break
	}
	a.discardPDF(job)
	if err != nil {
		a.notify("导出 PDF 失败。%v", err)
		return
	}
	a.notify("已导出 %s（%d 页）", rel, len(job.tops))
	if a.win != nil {
		if file, err := vault.Resolve(job.source.space.Root, rel); err == nil {
			mygo.Shell.ShowItemInFolder(file)
		}
	}
}
