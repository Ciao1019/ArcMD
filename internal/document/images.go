package document

import "slices"

// ReplaceImage changes one picture without losing its description or ID.
// expected prevents a delayed file picker from overwriting a newer choice.
func (e *Engine) ReplaceImage(id, expected, next string) bool {
	if e.ReadOnly {
		return false
	}
	parent, index := e.find(id)
	if parent == nil || (*parent)[index].Kind != "image" || (*parent)[index].URL != expected || next == "" {
		return false
	}
	e.Edit(func() { (*parent)[index].URL = next })
	return true
}

// SetInlineImageAlt updates the alt text of the inline image run at UTF-16
// document offset at without changing its placeholder, URL, dimensions, or link.
func (e *Engine) SetInlineImageAlt(at int, alt string) bool {
	if e.ReadOnly {
		return false
	}
	leaf := e.LeafAt(at)
	if leaf.Block == nil || at < leaf.Start || at >= leaf.End {
		return false
	}
	local := at - leaf.Start
	pos := 0
	found := -1
	for i, r := range leaf.Block.Runs {
		n := Len(r.Text)
		if local >= pos && local < pos+n {
			if r.IsImage() {
				found = i
			}
			break
		}
		pos += n
	}
	if found < 0 || leaf.Block.Runs[found].Image.Alt == alt {
		return false
	}
	e.Edit(func() {
		if l, ok := e.LeafByID(leaf.Block.ID); ok && found < len(l.Block.Runs) && l.Block.Runs[found].IsImage() {
			l.Block.Runs[found].Image.Alt = alt
		}
	})
	return true
}

// MapImages changes image and cover URLs without touching text or selection.
func (d *Document) MapImages(change func(string) string) {
	if d.Cover != "" {
		d.Cover = change(d.Cover)
	}
	var walk func([]*Block)
	walk = func(blocks []*Block) {
		for _, b := range blocks {
			if b.Kind == "image" {
				b.URL = change(b.URL)
			}
			for i := range b.Runs {
				if b.Runs[i].Image.URL != "" {
					b.Runs[i].Image.URL = change(b.Runs[i].Image.URL)
				}
			}
			walk(b.Children)
		}
	}
	walk(d.Blocks)
}

// ImageURLs includes images reachable through undo and redo, so moving a
// document does not strand an attachment that an earlier edit can restore.
func (e *Engine) ImageURLs() []string {
	var urls []string
	addURL := func(url string) {
		if url != "" && !slices.Contains(urls, url) {
			urls = append(urls, url)
		}
	}
	collect := func(d *Document) {
		if d == nil {
			return
		}
		d.MapImages(func(url string) string {
			addURL(url)
			return url
		})
	}
	collectRuns := func(runs []Run) {
		for _, r := range runs {
			addURL(r.Image.URL)
		}
	}
	collect(e.Doc)
	for _, h := range e.undo {
		collect(h.state.Doc)
		collectRuns(h.runs)
	}
	for _, h := range e.redo {
		collect(h.state.Doc)
		collectRuns(h.runs)
	}
	return urls
}

// RelocateImages rebases all retained states as a file-system operation, not
// an editing step. Undo must never restore URLs relative to the old folder.
func (e *Engine) RelocateImages(urls map[string]string) {
	change := func(url string) string {
		if to, ok := urls[url]; ok {
			return to
		}
		return url
	}
	e.notifyBeforeChange()
	e.Doc.MapImages(change)
	for _, entries := range [][]historyEntry{e.undo, e.redo} {
		for ei := range entries {
			h := &entries[ei]
			if h.state.Doc != nil {
				h.state.Doc.MapImages(change)
			}
			for ri := range h.runs {
				if h.runs[ri].Image.URL != "" {
					h.runs[ri].Image.URL = change(h.runs[ri].Image.URL)
				}
			}
		}
	}
	e.revision++
}
