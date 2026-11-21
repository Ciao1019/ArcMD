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
	"github.com/Ciao1019/ArcMD/internal/pdfdoc"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type pdfExport struct {
	source                          *document
	title                           string
	ed                              *editor.Editor
	opts                            editor.Options
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
	if d == nil || d.loadErr != nil || d.isPDF() || a.pdfExport != nil {
		return
	}
	if d.isWhiteboard() {
		a.exportWhiteboardPDF(d, printing)
		return
	}
	d.ed.UnmarkText()
	root, rel, err := fileLocation(d.space, d.rel)
	if err != nil {
		a.notify("导出 PDF 失败。%v", err)
		return
	}
	source := &document{space: &space.Space{ID: d.space.ID, Root: root}, rel: rel}
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
	w := pdf.NewWriter(file, d.title())
	if reg, bold, it, boldIt := editor.MathFontData(); len(reg) > 0 {
		w.RegisterFontData(pdf.FontMath, false, false, reg)
		w.RegisterFontData(pdf.FontMath, true, false, bold)
		w.RegisterFontData(pdf.FontMathItalic, false, true, it)
		w.RegisterFontData(pdf.FontMathItalic, true, true, boldIt)
	}
	a.pdfExport = &pdfExport{source: source, title: d.title(), ed: editor.FromDocument(d.ed.Committed()), file: file,
		writer: w, open: true, printing: printing, next: a.now.Add(time.Millisecond)}
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
		job.opts = editor.Options{
			Width:          paperWidth,
			Theme:          documentTheme(false),
			Page:           pageStyle(false),
			LoadImage:      job.source.imageResource,
			LoadRawImage:   job.source.rawImage,
			LoadWhiteboard: func(url string) (*whiteboard.Scene, uint64, error) { return a.loadWhiteboardFor(job.source, url) },
			LoadPDF:        func(url string) (*pdfdoc.Document, uint64, error) { return a.loadPDFFor(job.source, url) },
		}
		ui.Render(func(c *ui.Context) { job.tops = job.ed.Paginate(job.opts, paperHeight-2*paperMargin) }, 1, 1, 1)
		job.writer.ReservePages(len(job.tops))
		job.writer.SetOutlines(job.ed.ExportOutlines(job.tops, paperHeight, paperMargin, pdf.A4Height))
		c.After(time.Millisecond)
		return
	}
	if job.page >= len(job.tops) {
		a.finishPDF(job)
		return
	}
	vp := job.buildVectorPage()
	// Vector drawing stays on the UI thread for font/layout reads; zlib
	// stream compression and disk IO run between frames.
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
		done(job.writer.FinishVectorPage(vp))
		c.After(time.Millisecond)
	} else {
		go func() { err := job.writer.FinishVectorPage(vp); a.win.Update(func() { done(err) }) }()
	}
}

func (job *pdfExport) buildVectorPage() *pdf.VectorPage {
	top := job.tops[job.page]
	body := float32(paperHeight - 2*paperMargin)
	bottom := top + body
	if job.page+1 < len(job.tops) {
		bottom = job.tops[job.page+1]
	}
	vp := job.writer.NewVectorPage(pdf.A4Width, pdf.A4Height)
	job.ed.ExportVectorPage(vp, job.opts, top, bottom, job.page, len(job.tops), job.tops, paperWidth, paperHeight, paperMargin)
	return vp
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
		full, resolveErr := resolveFile(job.source.space, rel)
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
		if file, err := resolveFile(job.source.space, rel); err == nil {
			mygo.Shell.ShowItemInFolder(file)
		}
	}
}
