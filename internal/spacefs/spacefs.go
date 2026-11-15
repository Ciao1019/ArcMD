// Package spacefs is a read-only view of one space's documents for an
// agent that answers questions about them: every document is seen as
// Markdown, numbered by line, whatever its file format, and the tools an
// agent explores with (Glob, Grep, Read, Outline, Links, Overview) return
// text written for a model to read. Nothing here writes a file.
//
// A line of a page maps back to the top-level block it came from, so an
// answer that cites "path#L12" can open the document at that block.
//
// The package does not depend on the window: the app supplies how a file
// becomes a Page (the document model and Markdown export live above it),
// and the open documents' unsaved text as live pages.
package spacefs

import (
	"errors"
	"fmt"
	"html"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/vault"
)

// Part is one top-level block's Markdown and the block's ID ("" when the
// file has no stable IDs, as Markdown files do not).
type Part struct {
	Block string
	Text  string
}

// Span is the lines of a page one top-level block takes, 1-based and
// inclusive. Index is the block's position among the top-level blocks,
// which finds it again when its ID changes, as a Markdown file's do each
// time it is read.
type Span struct {
	Block    string
	Index    int
	From, To int
}

// Page is a document as the agent reads it.
type Page struct {
	// Title is the page's first heading, or the file's name.
	Title      string
	Lines      []string
	PlainLines []string
	Spans      []Span
}

// Location describes a line range in human terms: the document's title, its
// enclosing section heading (if distinct from Title), a plain-text excerpt of
// the recalled passage, and the top-level block Span it maps to.
type Location struct {
	Title   string
	Section string
	Excerpt string
	From    int
	To      int
	Span    Span
	OK      bool
}

// PageOf joins the parts with blank lines, as Markdown export does, and
// keeps which lines each came from.
func PageOf(name string, parts []Part) *Page {
	p := &Page{Title: vault.Title(name)}
	for i, part := range parts {
		if i > 0 {
			p.Lines = append(p.Lines, "")
		}
		lines := strings.Split(strings.TrimRight(part.Text, "\n"), "\n")
		from := len(p.Lines) + 1
		p.Lines = append(p.Lines, lines...)
		p.Spans = append(p.Spans, Span{Block: part.Block, Index: i, From: from, To: len(p.Lines)})
	}
	p.PlainLines = make([]string, len(p.Lines))
	for i, l := range p.Lines {
		p.PlainLines[i] = stripInlineMarkup(l)
	}
	for _, l := range p.Lines {
		if t, ok := strings.CutPrefix(l, "# "); ok {
			if clean := strings.TrimSpace(stripInlineMarkup(t)); clean != "" {
				p.Title = clean
				break
			}
		}
	}
	return p
}

// Locate is the top-level block that holds line n, if any.
func (p *Page) Locate(n int) (Span, bool) {
	for _, s := range p.Spans {
		if n >= s.From && n <= s.To {
			return s, true
		}
		if n < s.From {
			// A blank line between blocks belongs to the one after it.
			return s, true
		}
	}
	return Span{}, false
}

// SectionAt returns the nearest Markdown heading at or above 1-based line n
// outside fenced code and math blocks.
func (p *Page) SectionAt(n int) string {
	if len(p.Lines) == 0 {
		return ""
	}
	limit := min(max(n, 1), len(p.Lines))
	fence := ""
	heading := ""
	for i := 0; i < limit; i++ {
		l := p.Lines[i]
		t := strings.TrimSpace(l)
		if fence != "" {
			if strings.HasPrefix(t, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(t, "```") {
			fence = t[:strings.IndexFunc(t+"x", func(r rune) bool { return r != '`' })]
			continue
		}
		if t == "$$" {
			fence = "$$"
			continue
		}
		if strings.HasPrefix(l, "#") {
			level := len(l) - len(strings.TrimLeft(l, "#"))
			if level >= 1 && level <= 6 && strings.HasPrefix(l[level:], " ") {
				if h := strings.TrimSpace(stripInlineMarkup(l[level:])); h != "" {
					heading = h
				}
			}
		}
	}
	return heading
}

// Describe summarizes lines [from, to] (1-based, inclusive) with their
// enclosing section heading and a clean plain-text excerpt.
func (p *Page) Describe(from, to int) Location {
	if p == nil || len(p.Lines) == 0 {
		title := ""
		if p != nil {
			title = p.Title
		}
		return Location{Title: title, From: from, To: to}
	}
	if from < 1 {
		from = 1
	}
	if to < from {
		to = from
	}
	clampedFrom := min(from, len(p.Lines))
	clampedTo := min(to, len(p.Lines))
	span, ok := p.Locate(clampedFrom)
	sec := p.SectionAt(clampedFrom)
	if strings.EqualFold(sec, p.Title) {
		sec = ""
	}
	var pieces []string
	collect := func(a, b int, skipHeadings bool) {
		for i := a; i <= b && i <= len(p.Lines); i++ {
			raw := strings.TrimSpace(p.Lines[i-1])
			if raw == "" || raw == "---" || strings.HasPrefix(raw, "```") || raw == "$$" {
				continue
			}
			if skipHeadings && strings.HasPrefix(raw, "#") {
				continue
			}
			clean := excerptLine(p.Lines[i-1])
			if clean != "" {
				pieces = append(pieces, clean)
			}
			if len(pieces) >= 2 {
				break
			}
		}
	}
	collect(clampedFrom, clampedTo, true)
	if len(pieces) == 0 && ok {
		collect(span.From, span.To, true)
	}
	if len(pieces) == 0 {
		// Cited a heading line directly: look right below the heading for context.
		collect(clampedFrom+1, min(len(p.Lines), clampedFrom+4), true)
	}
	if len(pieces) == 0 {
		collect(clampedFrom, clampedTo, false)
	}
	excerpt := strings.Join(pieces, " ")
	if utf8.RuneCountInString(excerpt) > 96 {
		rs := []rune(excerpt)
		excerpt = strings.TrimSpace(string(rs[:96])) + "…"
	}
	return Location{
		Title:   p.Title,
		Section: sec,
		Excerpt: excerpt,
		From:    clampedFrom,
		To:      clampedTo,
		Span:    span,
		OK:      ok,
	}
}

var (
	htmlTagStripRe  = regexp.MustCompile(`<[^>]+>`)
	mdLinkStripRe   = regexp.MustCompile(`!?\[([^\[\]]*)\]\([^()\n]*\)`)
	wikiAliasRe     = regexp.MustCompile(`\[\[([^\[\]|]+)\|([^\[\]]+)\]\]`)
	wikiSimpleRe    = regexp.MustCompile(`\[\[([^\[\]]+)\]\]`)
	alertPrefixRe   = regexp.MustCompile(`^\[![A-Za-z]+\]\s*`)
	orderedMarkerRe = regexp.MustCompile(`^\d+[.)]\s+`)
	multiSpaceRe    = regexp.MustCompile(`\s+`)
)

func stripInlineMarkup(s string) string {
	if s == "" {
		return ""
	}
	out := htmlTagStripRe.ReplaceAllString(s, "")
	out = html.UnescapeString(out)
	out = mdLinkStripRe.ReplaceAllString(out, "$1")
	out = wikiAliasRe.ReplaceAllString(out, "$2")
	out = wikiSimpleRe.ReplaceAllString(out, "$1")
	out = strings.NewReplacer("**", "", "__", "", "~~", "", "==", "", "`", "").Replace(out)
	return out
}

func excerptLine(s string) string {
	t := strings.TrimSpace(s)
	for strings.HasPrefix(t, ">") {
		t = strings.TrimSpace(t[1:])
	}
	t = alertPrefixRe.ReplaceAllString(t, "")
	if strings.HasPrefix(t, "#") {
		lvl := len(t) - len(strings.TrimLeft(t, "#"))
		if lvl >= 1 && lvl <= 6 && strings.HasPrefix(t[lvl:], " ") {
			t = strings.TrimSpace(t[lvl:])
		}
	}
	switch {
	case strings.HasPrefix(t, "- [ ] "), strings.HasPrefix(t, "* [ ] "):
		t = "☐ " + t[6:]
	case strings.HasPrefix(t, "- [x] "), strings.HasPrefix(t, "- [X] "), strings.HasPrefix(t, "* [x] "), strings.HasPrefix(t, "* [X] "):
		t = "☑ " + t[6:]
	case strings.HasPrefix(t, "- "), strings.HasPrefix(t, "* "), strings.HasPrefix(t, "+ "):
		t = t[2:]
	default:
		t = orderedMarkerRe.ReplaceAllString(t, "")
	}
	if strings.HasPrefix(t, "|") && strings.HasSuffix(t, "|") {
		cells := strings.Split(strings.Trim(t, "|"), "|")
		var kept []string
		for _, cell := range cells {
			c := strings.TrimSpace(cell)
			if c != "" && strings.Trim(c, "-: ") != "" {
				kept = append(kept, c)
			}
		}
		t = strings.Join(kept, " · ")
	}
	t = stripInlineMarkup(t)
	return strings.TrimSpace(multiSpaceRe.ReplaceAllString(t, " "))
}

// Render turns a document file's content into a Page; name is the file's
// name, telling .arcmd from Markdown.
type Render func(name, content string) (*Page, error)

// Doc is a document of the space.
type Doc struct {
	Path     string
	Modified time.Time
	Size     int64
}

type cached struct {
	size int64
	mod  time.Time
	page *Page
	err  error
}

// FS is the documents of one space folder. It is safe for concurrent use:
// the agent reads on its own goroutine while the window updates the live
// pages.
type FS struct {
	Root   string
	render Render

	mu    sync.Mutex
	cache map[string]cached
	live  map[string]*Page
	files vault.Files
}

// New returns the view of the space folder at root.
func New(root string, render Render) *FS {
	return &FS{Root: root, render: render, cache: map[string]cached{}}
}

// SetFiles limits this view to explicitly opened files. Passing an empty map
// gives an empty collection, not access to a directory. nil restores a folder.
func (f *FS) SetFiles(files map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !maps.Equal(f.files, files) || (f.files == nil) != (files == nil) {
		clear(f.cache)
	}
	f.files = maps.Clone(files)
}

func (f *FS) selection() vault.Files {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.files // immutable after SetFiles publishes it
}

func (f *FS) location(rel string) (string, string, error) {
	if files := f.selection(); files != nil {
		full, err := files.Resolve(rel)
		return filepath.Dir(full), filepath.Base(full), err
	}
	return f.Root, rel, nil
}

// SetLive replaces the pages of the documents open with unsaved edits, by
// path, which the tools read in place of the files.
func (f *FS) SetLive(pages map[string]*Page) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.live = pages
}

// Docs are the space's documents, in the vault's order, with their
// modification times. A folder that cannot be read is an error; files that
// cannot be stat'ed are left out.
func (f *FS) Docs() ([]Doc, error) {
	var list []vault.Doc
	var err error
	if files := f.selection(); files != nil {
		list = files.List()
	} else {
		list, err = vault.List(f.Root)
	}
	if err != nil {
		return nil, err
	}
	docs := make([]Doc, 0, len(list))
	for _, d := range list {
		root, rel, err := f.location(d.Path)
		if err != nil {
			continue
		}
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		docs = append(docs, Doc{Path: d.Path, Modified: info.ModTime(), Size: info.Size()})
	}
	f.mu.Lock()
	for _, p := range sortedKeys(f.live) {
		if !hasDoc(docs, p) {
			docs = append(docs, Doc{Path: p, Modified: time.Now()})
		}
	}
	f.mu.Unlock()
	return docs, nil
}

func hasDoc(docs []Doc, p string) bool {
	for _, d := range docs {
		if d.Path == p {
			return true
		}
	}
	return false
}

// ErrNotDocument is a path that is not one of the space's documents.
var ErrNotDocument = errors.New("not a document of this space")

// Page reads the document at rel (slash-separated, relative to the
// space), its unsaved text when it is open with edits. Paths that leave the
// space, or are not documents, are refused.
func (f *FS) Page(rel string) (*Page, error) {
	rel = strings.TrimPrefix(path.Clean("/"+strings.TrimSpace(rel)), "/")
	if rel == "" || !vault.IsDocument(rel) || hidden(rel) {
		return nil, fmt.Errorf("%s: %w", rel, ErrNotDocument)
	}
	f.mu.Lock()
	if p := f.live[rel]; p != nil {
		f.mu.Unlock()
		return p, nil
	}
	f.mu.Unlock()
	root, name, err := f.location(rel)
	if err != nil {
		return nil, err
	}
	abs, err := vault.Resolve(root, name)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: no such document", rel)
		}
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", rel, ErrNotDocument)
	}
	f.mu.Lock()
	c, ok := f.cache[rel]
	f.mu.Unlock()
	if ok && c.size == info.Size() && c.mod.Equal(info.ModTime()) {
		return c.page, c.err
	}
	content, err := vault.Read(root, name)
	var page *Page
	if err == nil {
		page, err = f.render(rel, content)
	}
	f.mu.Lock()
	f.cache[rel] = cached{size: info.Size(), mod: info.ModTime(), page: page, err: err}
	f.mu.Unlock()
	return page, err
}

// hidden reports a path inside a hidden folder, as the space's own
// .arcmd history, which the vault never lists.
func hidden(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if strings.HasPrefix(seg, ".") || seg == "node_modules" {
			return true
		}
	}
	return false
}
