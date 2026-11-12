package app

import (
	"errors"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
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
		return i18n.T("文档")
	}
	return name
}

// htmlPage is a document exported as a web page: semantic HTML in a page
// of its own, readable without ArcMD, its images beside it as the
// document's are.
func htmlPage(title, body string) string {
	return `<!doctype html>
<html lang="` + htmlLang() + `">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="generator" content="ArcMD">
<title>` + html.EscapeString(title) + `</title>
<style>
:root { color-scheme: light dark; }
body { margin: 0; background: #fff; color: #1f2329; font: 16px/1.65 -apple-system, BlinkMacSystemFont, "PingFang SC", "Microsoft YaHei", sans-serif; }
article { max-width: 760px; margin: 0 auto; padding: 48px 24px 96px; }
h1 { font-size: 2.1em; font-weight: 600; margin: .2em 0 .6em; }
h2, h3, h4, h5, h6 { font-weight: 600; margin: 1.4em 0 .5em; }
p { margin: .5em 0; }
a { color: #336df4; }
blockquote { margin: .8em 0; padding: .2em 1em; border-left: 3px solid #dee0e3; color: #646a73; }
pre { background: #f5f6f7; border-radius: 8px; padding: 14px 16px; overflow: auto; font-size: 14px; }
code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; background: #f2f3f5; border-radius: 4px; padding: .1em .3em; }
pre code { background: none; padding: 0; }
table { border-collapse: collapse; margin: .8em 0; }
th, td { border: 1px solid #dee0e3; padding: 6px 12px; text-align: left; }
th { background: #f5f6f7; }
img { max-width: 100%; border-radius: 8px; }
hr { border: 0; border-top: 1px solid #dee0e3; margin: 1.5em 0; }
details { margin: .5em 0; }
summary { cursor: pointer; }
@media (prefers-color-scheme: dark) {
  body { background: #1f2023; color: #e3e4e7; }
  pre, th, code { background: #2b2c30; }
  blockquote { border-color: #3a3b40; color: #a6a8ad; }
  th, td, hr { border-color: #3a3b40; }
  a { color: #6b9bff; }
}
</style>
</head>
<body>
<article>
` + body + `
</article>
</body>
</html>
`
}

// htmlLang is the page's language as HTML names it.
func htmlLang() string {
	if i18n.English() {
		return "en"
	}
	return "zh-CN"
}

// exportHTML writes the open document beside it as a web page, numbered
// when the name is taken, never replacing a file.
func (a *App) exportHTML() {
	d := a.doc
	if d == nil || d.loadErr != nil {
		return
	}
	d.ed.UnmarkText()
	doc := d.ed.Committed()
	page := htmlPage(docName(d.title()), editor.ExportHTML(doc))
	dir := path.Dir(d.rel)
	if dir == "." {
		dir = ""
	}
	for i := 1; i < 1000; i++ {
		name := d.title()
		if i > 1 {
			name = fmt.Sprintf("%s %d", name, i)
		}
		rel := path.Join(dir, name+".html")
		full, err := vault.Resolve(d.space.Root, rel)
		if err != nil {
			a.notify("导出失败。%v", err)
			return
		}
		err = fsutil.CreateFileAtomic(full, []byte(page), 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			a.notify("导出失败。%v", err)
			return
		}
		a.notify("已导出 %s", rel)
		a.refreshTree()
		if a.win != nil {
			mygo.Shell.ShowItemInFolder(full)
		}
		return
	}
}
