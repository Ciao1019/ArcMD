// Package search finds text in the documents of every space, for the
// command bar. An Index is an immutable snapshot of the documents' plain
// text; Refresh makes the next one, reading again only the files whose
// size or modification time changed, so it can run on a goroutine while
// the window keeps using the last snapshot.
package search

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/Ciao1019/ArcMD/internal/wiki"
)

// Root is a space's folder to index.
type Root struct {
	Space string // the space's ID
	Path  string // its folder
}

// Extract turns a document file's content into the plain text searched,
// blocks separated by newlines, and the page's icon. name is the file's
// name, telling .arcmd from Markdown. The app supplies it, as the document
// model and the Markdown importer live above this package.
type Extract func(name, content string) (Extracted, error)

// Extracted is what the index keeps of a document: its plain text and its
// page icon, an emoji or "".
type Extracted struct {
	Text, Icon string
}

type key struct{ space, path string }

type entry struct {
	text  string
	icon  string
	size  int64
	mod   time.Time
	space string
	path  string
}

// Index is a snapshot of the documents' text. The zero value is empty.
type Index struct {
	entries map[key]*entry
	order   []key // spaces in the order given, then by path
}

// Refresh reads the roots' documents into a new Index, reusing what prev
// holds for files whose size and modification time are unchanged. A root
// that cannot be read leaves its documents out; files that cannot be read
// or extracted are left out too: search is a convenience, never the only
// way to a document.
func Refresh(prev *Index, roots []Root, extract Extract) *Index {
	next := &Index{entries: map[key]*entry{}}
	for _, r := range roots {
		docs, _ := vault.List(r.Path)
		for _, d := range docs {
			k := key{r.Space, d.Path}
			info, err := os.Stat(filepath.Join(r.Path, filepath.FromSlash(d.Path)))
			if err != nil {
				continue
			}
			if prev != nil {
				if e, ok := prev.entries[k]; ok && e.size == info.Size() && e.mod.Equal(info.ModTime()) {
					next.entries[k] = e
					next.order = append(next.order, k)
					continue
				}
			}
			content, err := vault.Read(r.Path, d.Path)
			if err != nil {
				continue
			}
			ex, err := extract(d.Path, content)
			if err != nil {
				continue
			}
			next.entries[k] = &entry{text: ex.Text, icon: ex.Icon, size: info.Size(), mod: info.ModTime(), space: r.Space, path: d.Path}
			next.order = append(next.order, k)
		}
	}
	return next
}

// Len is how many documents the index holds.
func (ix *Index) Len() int {
	if ix == nil {
		return 0
	}
	return len(ix.entries)
}

// Hit is a document whose text holds the query.
type Hit struct {
	Space, Path string
	// Count is how many times the query appears.
	Count int
	// Snippet is the text around the first match on one line, Match the
	// byte range of the match in it.
	Snippet string
	Match   [2]int
}

// Live replaces the indexed text of one document, the one open with
// unsaved edits, for a search.
type Live struct {
	Space, Path, Text string
}

// Worth reports whether a query is long enough to search text for: two
// runes, or one that is not ASCII, as a single Chinese character is a
// word.
func Worth(query string) bool {
	q := strings.TrimSpace(query)
	n := utf8.RuneCountInString(q)
	return n >= 2 || n == 1 && q[0] >= utf8.RuneSelf
}

// Search finds the query, case-insensitively, as the editor's find box
// does, in every document. Hits come in the index's order, documents with
// more matches first, at most limit of them.
func (ix *Index) Search(query string, live *Live, limit int) []Hit {
	q := []rune(strings.Map(unicode.ToLower, strings.TrimSpace(query)))
	if !Worth(query) || limit <= 0 {
		return nil
	}
	var hits []Hit
	seenLive := false
	look := func(space, path, text string) {
		count, first := find(text, q)
		if count == 0 {
			return
		}
		snippet, match := snip(text, first, len(q))
		hits = append(hits, Hit{Space: space, Path: path, Count: count, Snippet: snippet, Match: match})
	}
	if ix != nil {
		for _, k := range ix.order {
			e := ix.entries[k]
			if live != nil && live.Space == e.space && live.Path == e.path {
				seenLive = true
				look(e.space, e.path, live.Text)
				continue
			}
			look(e.space, e.path, e.text)
		}
	}
	if live != nil && !seenLive {
		look(live.Space, live.Path, live.Text)
	}
	slices.SortStableFunc(hits, func(a, b Hit) int { return b.Count - a.Count })
	return hits[:min(len(hits), limit)]
}

// find counts the non-overlapping matches of q (lowered) in text and
// returns the rune index of the first.
func find(text string, q []rune) (count, first int) {
	runes := []rune(text)
	first = -1
	for i := 0; i+len(q) <= len(runes); {
		ok := true
		for j, r := range q {
			if unicode.ToLower(runes[i+j]) != r {
				ok = false
				break
			}
		}
		if ok {
			if first < 0 {
				first = i
			}
			count++
			i += len(q)
			continue
		}
		i++
	}
	return count, first
}

// Context around a match in a snippet, in runes.
const (
	before = 18
	after  = 60
)

// snip cuts the text around the match at rune at, n runes long, onto one
// line, whitespace runs as one space, and returns it with the match's byte
// range in it.
func snip(text string, at, n int) (string, [2]int) {
	runes := []rune(text)
	start := max(0, at-before)
	// Start at the line the match is on when it begins close by.
	for i := at - 1; i >= start; i-- {
		if runes[i] == '\n' {
			start = i + 1
			break
		}
	}
	end := min(len(runes), at+n+after)
	head := strings.TrimLeft(collapse(runes[start:at]), " ")
	if start > 0 && runes[start-1] != '\n' {
		head = "…" + head // cut inside a block, not at its start
	}
	match := collapse(runes[at : at+n])
	tail := strings.TrimRight(collapse(runes[at+n:end]), " ")
	if end < len(runes) {
		tail += "…"
	}
	return head + match + tail, [2]int{len(head), len(head) + len(match)}
}

// collapse turns each run of whitespace, newlines included, into a space.
func collapse(rs []rune) string {
	var b strings.Builder
	space := false
	for _, r := range rs {
		if unicode.IsSpace(r) {
			if !space {
				b.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// each calls fn with every document's space, path and text, live's text in
// place of its indexed one, and live itself when the index lacks it.
func (ix *Index) each(live *Live, fn func(space, path, text string)) {
	seenLive := false
	if ix != nil {
		for _, k := range ix.order {
			e := ix.entries[k]
			if live != nil && live.Space == e.space && live.Path == e.path {
				seenLive = true
				fn(e.space, e.path, live.Text)
				continue
			}
			fn(e.space, e.path, e.text)
		}
	}
	if live != nil && !seenLive {
		fn(live.Space, live.Path, live.Text)
	}
}

// Icon is the page icon of the document at path of the space, as last
// indexed, or "".
func (ix *Index) Icon(space, path string) string {
	if ix == nil {
		return ""
	}
	if e, ok := ix.entries[key{space, path}]; ok {
		return e.icon
	}
	return ""
}

// Backlinks are the documents linking to the document named name with a
// [[name]], but for the one at self (its own space and path). Each hit's
// snippet is the line of the first link, Match the link in it.
func (ix *Index) Backlinks(name string, self Live, live *Live) []Hit {
	var hits []Hit
	ix.each(live, func(space, path, text string) {
		if space == self.Space && path == self.Path || !strings.Contains(text, "[[") {
			return
		}
		count, first, n := 0, -1, 0
		for _, l := range wiki.Links(text) {
			if wiki.Same(l.Target, name) {
				if first < 0 {
					first, n = utf8.RuneCountInString(text[:l.Start]), utf8.RuneCountInString(text[l.Start:l.End])
				}
				count++
			}
		}
		if count == 0 {
			return
		}
		snippet, match := snip(text, first, n)
		hits = append(hits, Hit{Space: space, Path: path, Count: count, Snippet: snippet, Match: match})
	})
	return hits
}

// Tagged are the documents holding the tag (case-insensitively; a tag
// holds its sub-tags, "#work" those with "#work/2026"), the snippet the
// line of the first.
func (ix *Index) Tagged(tag string, live *Live) []Hit {
	tag = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(tag), "#"))
	if tag == "" {
		return nil
	}
	var hits []Hit
	ix.each(live, func(space, path, text string) {
		if !strings.Contains(text, "#") {
			return
		}
		count, first, n := 0, -1, 0
		for _, t := range wiki.Tags(text) {
			name := strings.ToLower(t.Name)
			if name != tag && !strings.HasPrefix(name, tag+"/") {
				continue
			}
			if first < 0 {
				first, n = utf8.RuneCountInString(text[:t.Start]), utf8.RuneCountInString(text[t.Start:t.End])
			}
			count++
		}
		if count == 0 {
			return
		}
		snippet, match := snip(text, first, n)
		hits = append(hits, Hit{Space: space, Path: path, Count: count, Snippet: snippet, Match: match})
	})
	return hits
}

// TagCount is a tag and how many documents hold it.
type TagCount struct {
	Name  string
	Count int
}

// Tags are every tag of the documents, most used first, then by name,
// each spelled as first seen.
func (ix *Index) Tags(live *Live) []TagCount {
	counts := map[string]*TagCount{}
	var order []string
	ix.each(live, func(_, _, text string) {
		if !strings.Contains(text, "#") {
			return
		}
		for _, name := range wiki.TagNames(text) {
			k := strings.ToLower(name)
			if counts[k] == nil {
				counts[k] = &TagCount{Name: name}
				order = append(order, k)
			}
			counts[k].Count++
		}
	})
	out := make([]TagCount, 0, len(order))
	for _, k := range order {
		out = append(out, *counts[k])
	}
	slices.SortStableFunc(out, func(a, b TagCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out
}

// WithLive overlays several open documents without changing the indexed
// snapshot. Search, tags and backlinks all see the same unsaved text.
func (ix *Index) WithLive(live []Live) *Index {
	if len(live) == 0 {
		return ix
	}
	next := &Index{entries: map[key]*entry{}}
	if ix != nil {
		for k, e := range ix.entries {
			next.entries[k] = e
		}
		next.order = slices.Clone(ix.order)
	}
	for _, d := range live {
		k := key{d.Space, d.Path}
		e := entry{space: d.Space, path: d.Path}
		if old := next.entries[k]; old != nil {
			e = *old
		} else {
			next.order = append(next.order, k)
		}
		e.text = d.Text
		next.entries[k] = &e
	}
	return next
}
