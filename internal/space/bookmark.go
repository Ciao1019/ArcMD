package space

import (
	"slices"
	"time"
)

// Bookmark marks a place in a document: one of its top-level blocks. It is
// the reader's, not the document's, so it lives here rather than in the
// file: marking a place neither changes the document nor converts a
// Markdown file.
type Bookmark struct {
	// Space is the ID of the space holding the document, and Path its
	// slash-separated path in the space's folder.
	Space string `json:"space"`
	Path  string `json:"path"`
	// Block is the stable ID of the block marked.
	Block string `json:"block"`
	// Excerpt is the start of the block's text when it was last seen: what
	// lists show for a document that is not open, and what finds the block
	// again in a Markdown file, whose blocks get new IDs each time it is
	// read.
	Excerpt string `json:"excerpt,omitempty"`
	// Label is the name the reader gave the bookmark, shown instead of the
	// excerpt.
	Label   string    `json:"label,omitempty"`
	Created time.Time `json:"created"`
}

// Title is what lists call the bookmark: its label, else its excerpt.
func (b Bookmark) Title() string {
	if b.Label != "" {
		return b.Label
	}
	return b.Excerpt
}

// BookmarkIndex is the place of the bookmark on the block of the document,
// or -1.
func (st *State) BookmarkIndex(spaceID, rel, block string) int {
	return slices.IndexFunc(st.Bookmarks, func(b Bookmark) bool {
		return b.Space == spaceID && b.Path == rel && b.Block == block
	})
}

// AddBookmark marks a block, unless it is marked already.
func (st *State) AddBookmark(b Bookmark) {
	if st.BookmarkIndex(b.Space, b.Path, b.Block) < 0 {
		st.Bookmarks = append(st.Bookmarks, b)
	}
}

// RemoveBookmark drops the bookmark on the block of the document and
// reports whether there was one.
func (st *State) RemoveBookmark(spaceID, rel, block string) bool {
	i := st.BookmarkIndex(spaceID, rel, block)
	if i < 0 {
		return false
	}
	st.Bookmarks = slices.Delete(st.Bookmarks, i, i+1)
	return true
}

// BookmarksIn are the bookmarks of the document, oldest first.
func (st *State) BookmarksIn(spaceID, rel string) []Bookmark {
	var out []Bookmark
	for _, b := range st.Bookmarks {
		if b.Space == spaceID && b.Path == rel {
			out = append(out, b)
		}
	}
	return out
}
