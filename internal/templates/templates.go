// Package templates keeps a space's document templates and makes daily
// notes. Templates are ordinary .arcmd documents in the space's hidden
// .arcmd/templates folder; making a document from one gives every block a
// fresh ID and fills in the placeholders {{date}}, {{time}}, {{weekday}}
// and {{title}}. It does not depend on the UI.
package templates

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/document"
)

// Dir is the folder of a space's templates, relative to its root.
const Dir = ".arcmd/templates"

// DailyDir is the folder daily notes go in, and DailyTemplate the name of
// the template a daily note starts from when the space has one.
const (
	DailyDir      = "日记"
	DailyTemplate = "日记"
)

// Template is one template: its name (the file's, without .arcmd) and
// where it is in the space.
type Template struct {
	Name, Path string
}

// List returns the templates of the space at root, by name. A space
// without templates has none, which is not an error.
func List(root string) ([]Template, error) {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(Dir)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Template
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.EqualFold(path.Ext(name), ".arcmd") {
			continue
		}
		out = append(out, Template{Name: strings.TrimSuffix(name, path.Ext(name)), Path: path.Join(Dir, name)})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// Find returns the template called name, if the space has one.
func Find(list []Template, name string) (Template, bool) {
	for _, t := range list {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return Template{}, false
}

var weekdays = []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}

// Weekday names the day of now in Chinese.
func Weekday(now time.Time) string { return weekdays[now.Weekday()] }

// Fill replaces the placeholders in text: {{date}} as 2006-01-02,
// {{time}} as 15:04, {{weekday}} and {{title}}.
func Fill(text string, now time.Time, title string) string {
	if !strings.Contains(text, "{{") {
		return text
	}
	return strings.NewReplacer(
		"{{date}}", now.Format("2006-01-02"),
		"{{time}}", now.Format("15:04"),
		"{{weekday}}", Weekday(now),
		"{{title}}", title,
	).Replace(text)
}

// Instantiate makes a new document from a template: a copy with fresh
// IDs, its placeholders filled in, and no history of where it came from.
func Instantiate(tmpl *document.Document, now time.Time, title string) *document.Document {
	doc := tmpl.Clone()
	doc.ImportSource = ""
	doc.Version = document.CurrentVersion
	var walk func([]*document.Block)
	walk = func(blocks []*document.Block) {
		for _, b := range blocks {
			b.ID = document.NewID()
			for i := range b.Runs {
				b.Runs[i].Text = Fill(b.Runs[i].Text, now, title)
			}
			walk(b.Children)
		}
	}
	walk(doc.Blocks)
	if len(doc.Blocks) == 0 {
		doc.Blocks = []*document.Block{document.NewBlock("paragraph", "")}
	}
	return doc
}

// DailyName is the file name, without extension, of the daily note of now.
func DailyName(now time.Time) string { return now.Format("2006-01-02") }

// DailyPath is where the daily note of now goes in a space.
func DailyPath(now time.Time) string { return path.Join(DailyDir, DailyName(now)+".arcmd") }

// English names the daily notes' folder and titles in English, for an
// interface shown in English: the folder Daily, titles as Thursday,
// November 12, 2026.
var English = false

// Folder is the daily notes' folder in the language shown.
func Folder() string {
	if English {
		return "Daily"
	}
	return DailyDir
}

// PathFor is where the daily note of now goes, in the language shown.
func PathFor(now time.Time) string { return path.Join(Folder(), DailyName(now)+".arcmd") }

// DailyTitle is the title of the daily note of now, as 2026年11月12日 星期四.
func DailyTitle(now time.Time) string {
	if English {
		return now.Format("Monday, January 2, 2006")
	}
	return now.Format("2006年1月2日") + " " + Weekday(now)
}

// Daily makes the daily note of now, from tmpl when the space has a daily
// template (nil for none). Its title is the day's, unless the template
// gives a title of its own.
func Daily(tmpl *document.Document, now time.Time) *document.Document {
	title := DailyTitle(now)
	if tmpl != nil {
		doc := Instantiate(tmpl, now, title)
		if first := doc.Blocks[0]; first.Kind == "heading" && first.Text() == "" {
			first.Runs = []document.Run{{Text: title}}
		}
		return doc
	}
	doc := document.New()
	head := document.NewBlock("heading", title)
	head.Level = 1
	doc.Blocks = []*document.Block{head, document.NewBlock("paragraph", "")}
	return doc
}
