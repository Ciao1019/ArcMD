// Package space models ArcMD's spaces: separate worlds of Markdown files,
// each with its own folder, name, icon and color, as Arc's spaces keep
// separate sets of tabs. The package knows nothing of the UI.
package space

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
)

// Space is one workspace: a folder of Markdown files and how it looks.
// Split remembers one space's document group. Empty paths are empty panes.
type Split struct {
	Panes    []SplitPane `json:"panes"`
	Active   int         `json:"active"`
	Vertical bool        `json:"vertical,omitempty"`
	Shown    bool        `json:"shown,omitempty"`
}
type SplitPane struct {
	Space string  `json:"space,omitempty"`
	Path  string  `json:"path,omitempty"`
	Share float32 `json:"share"`
}

type Space struct {
	Split *Split `json:"split,omitempty"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	// Icon is a short glyph shown in the space switcher, usually an emoji;
	// empty with a Symbol, or for a space without an icon.
	Icon string `json:"icon"`
	// Symbol is the Lucide name of the space's glyph, chosen on the Icon
	// tab of the icon picker, as Cra keeps Arc's symbols beside its emoji
	// (never both). It wins over Icon.
	Symbol string `json:"symbol,omitempty"`
	// Color is the space's main hex color (#RRGGBB), the first of its
	// theme's; spaces saved before themes have only this.
	Color string `json:"color"`
	// Theme is how the space paints the window; nil for a space saved
	// before themes, which ThemeOf makes from Color.
	Theme *Theme `json:"theme,omitempty"`
	// Root is the absolute path of the folder holding the space's files.
	Root string `json:"root"`
	// Pinned is what version 3 kept of the space's pinned documents; Load
	// moves it to State.Favorites, which every space shows.
	Pinned []string `json:"pinned,omitempty"`
	// Pins are the documents and folders pinned at the top of the space's
	// list, above its line, as Arc's pinned tabs and folders: paths in
	// order.
	Pins []string `json:"pins,omitempty"`
	// PinsCollapsed folds the pinned section away, as a click on Arc's
	// space header does: only the pin holding the open document shows.
	PinsCollapsed bool `json:"pinsCollapsed,omitempty"`
	// LastOpen is the document shown when the space was left.
	LastOpen string `json:"lastOpen,omitempty"`
	// Expanded lists the folders open in the sidebar.
	Expanded []string `json:"expanded,omitempty"`
	// Order is the order the sidebar shows each folder's files and
	// folders in, by the folder's path ("" for the root) and their names,
	// as dragging them leaves it. What a list leaves out follows it, in
	// the folder's own order.
	Order map[string][]string `json:"order,omitempty"`
	// Sort is how a folder, by its path ("" for the root), sorts its files
	// and folders instead of the order dragging leaves: "name" or
	// "modified", newest first (version 8).
	Sort map[string]string `json:"sort,omitempty"`
}

// Sorts are the ways a folder may sort, beside its own order ("").
var Sorts = []string{"", "name", "modified"}

// SortOf is how the folder dir sorts: "" for the order dragging leaves.
func (s *Space) SortOf(dir string) string { return s.Sort[dir] }

// SetSort sets how the folder dir sorts; "" goes back to its own order.
func (s *Space) SetSort(dir, mode string) {
	if mode != "name" && mode != "modified" {
		delete(s.Sort, dir)
		return
	}
	if s.Sort == nil {
		s.Sort = map[string]string{}
	}
	s.Sort[dir] = mode
}

// Arrange orders the names of the folder dir's files and folders as the
// space keeps them: those it lists first, in its order, then the rest in
// theirs.
func (s *Space) Arrange(dir string, names []string) []string {
	kept := s.Order[dir]
	if len(kept) == 0 {
		return names
	}
	have := make(map[string]bool, len(names))
	for _, n := range names {
		have[n] = true
	}
	out := make([]string, 0, len(names))
	listed := make(map[string]bool, len(kept))
	for _, n := range kept {
		if have[n] && !listed[n] {
			out = append(out, n)
			listed[n] = true
		}
	}
	for _, n := range names {
		if !listed[n] {
			out = append(out, n)
		}
	}
	return out
}

// Place puts name at index among the folder dir's names, shown now as
// shown, and keeps that order.
func (s *Space) Place(dir string, shown []string, name string, index int) {
	list := slices.DeleteFunc(slices.Clone(shown), func(n string) bool { return n == name })
	index = min(max(index, 0), len(list))
	// Placing by hand is the folder's own order again.
	delete(s.Sort, dir)
	if s.Order == nil {
		s.Order = map[string][]string{}
	}
	s.Order[dir] = slices.Insert(list, index, name)
}

// parentOf splits rel into its folder ("" for the root) and its name.
func parentOf(rel string) (dir, name string) {
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		return rel[:i], rel[i+1:]
	}
	return "", rel
}

// reorderRenamed follows a file or folder moved from one path to another
// in the order: in its folder its name changes in place; moved to another
// folder it leaves the old one's list (the mover places it in the new
// one); the lists of the folders inside it follow it.
func (s *Space) reorderRenamed(from, to string) {
	if len(s.Order) == 0 {
		return
	}
	fd, fn := parentOf(from)
	td, tn := parentOf(to)
	if list := s.Order[fd]; list != nil {
		if fd == td {
			for i, n := range list {
				if n == fn {
					list[i] = tn
				}
			}
		} else {
			s.Order[fd] = slices.DeleteFunc(list, func(n string) bool { return n == fn })
		}
	}
	re := renamer(from, to)
	for k, v := range s.Order {
		if under(k, from) {
			delete(s.Order, k)
			s.Order[re(k)] = v
		}
	}
}

// IsExpanded reports whether the folder at rel is open in the sidebar.
func (s *Space) IsExpanded(rel string) bool { return slices.Contains(s.Expanded, rel) }

// SetExpanded opens or closes the folder at rel in the sidebar.
func (s *Space) SetExpanded(rel string, open bool) {
	s.Expanded = slices.DeleteFunc(s.Expanded, func(p string) bool { return p == rel })
	if open {
		s.Expanded = append(s.Expanded, rel)
	}
}

// renamer follows a path under from to its place under to.
func renamer(from, to string) func(string) string {
	return func(p string) string {
		if p == from {
			return to
		}
		if strings.HasPrefix(p, from+"/") {
			return to + p[len(from):]
		}
		return p
	}
}

// under reports whether p is rel or inside it.
func under(p, rel string) bool { return p == rel || strings.HasPrefix(p, rel+"/") }

// Renamed follows a file or folder moved from one path to another in the
// open folders and LastOpen.
func (s *Space) Renamed(from, to string) {
	s.reorderRenamed(from, to)
	re := renamer(from, to)
	sorts := s.Sort
	s.Sort = nil
	for k, v := range sorts {
		s.SetSort(re(k), v)
	}
	for i, p := range s.Pins {
		s.Pins[i] = re(p)
	}
	for i, p := range s.Expanded {
		s.Expanded[i] = re(p)
	}
	s.LastOpen = re(s.LastOpen)
}

// IsPinned reports whether rel is pinned at the top of the space's list.
func (s *Space) IsPinned(rel string) bool { return slices.Contains(s.Pins, rel) }

// Pin pins rel at index among the pins, or moves it there.
func (s *Space) Pin(rel string, index int) {
	s.Unpin(rel)
	index = min(max(index, 0), len(s.Pins))
	s.Pins = slices.Insert(s.Pins, index, rel)
}

// Unpin unpins rel.
func (s *Space) Unpin(rel string) {
	s.Pins = slices.DeleteFunc(s.Pins, func(p string) bool { return p == rel })
}

// Forget drops rel, and what is under it, from the open folders and
// LastOpen, as after it is gone.
func (s *Space) Forget(rel string) {
	s.Pins = slices.DeleteFunc(s.Pins, func(p string) bool { return under(p, rel) })
	if dir, name := parentOf(rel); s.Order[dir] != nil {
		s.Order[dir] = slices.DeleteFunc(s.Order[dir], func(n string) bool { return n == name })
	}
	for k := range s.Order {
		if under(k, rel) {
			delete(s.Order, k)
		}
	}
	for k := range s.Sort {
		if under(k, rel) {
			delete(s.Sort, k)
		}
	}
	s.Expanded = slices.DeleteFunc(s.Expanded, func(p string) bool { return under(p, rel) })
	if under(s.LastOpen, rel) {
		s.LastOpen = ""
	}
}

// Favorite is a document pinned to the top of the sidebar, as Arc's
// favorites: every space shows the same ones, and switching spaces keeps
// an open favorite open.
type Favorite struct {
	// Space is the ID of the space holding the document, and Path its
	// slash-separated path in the space's folder.
	Space string `json:"space"`
	Path  string `json:"path"`
}

// FavoriteIndex is the place of the document among the favorites, or -1.
func (st *State) FavoriteIndex(spaceID, rel string) int {
	return slices.Index(st.Favorites, Favorite{spaceID, rel})
}

// IsFavorite reports whether the document is a favorite.
func (st *State) IsFavorite(spaceID, rel string) bool { return st.FavoriteIndex(spaceID, rel) >= 0 }

// ToggleFavorite pins the document at the end of the favorites, or unpins
// it.
func (st *State) ToggleFavorite(spaceID, rel string) {
	if i := st.FavoriteIndex(spaceID, rel); i >= 0 {
		st.Favorites = slices.Delete(st.Favorites, i, i+1)
		return
	}
	st.Favorites = append(st.Favorites, Favorite{spaceID, rel})
}

// MoveFavorite moves the favorite at index i to index to, clamped.
func (st *State) MoveFavorite(i, to int) {
	if i < 0 || i >= len(st.Favorites) {
		return
	}
	f := st.Favorites[i]
	st.Favorites = slices.Delete(st.Favorites, i, i+1)
	to = min(max(to, 0), len(st.Favorites))
	st.Favorites = slices.Insert(st.Favorites, to, f)
}

// Renamed follows a file or folder of the space with the ID moved from one
// path to another, in the space, the favorites and the bookmarks.
func (st *State) Renamed(spaceID, from, to string) {
	if s := st.Get(spaceID); s != nil {
		s.Renamed(from, to)
	}
	re := renamer(from, to)
	for i, f := range st.Favorites {
		if f.Space == spaceID {
			st.Favorites[i].Path = re(f.Path)
		}
	}
	for i, b := range st.Bookmarks {
		if b.Space == spaceID {
			st.Bookmarks[i].Path = re(b.Path)
		}
	}
}

// Forget drops rel of the space with the ID, and what is under it, from the
// space, the favorites and the bookmarks, as after it is gone.
func (st *State) Forget(spaceID, rel string) {
	if s := st.Get(spaceID); s != nil {
		s.Forget(rel)
	}
	st.Favorites = slices.DeleteFunc(st.Favorites, func(f Favorite) bool { return f.Space == spaceID && under(f.Path, rel) })
	st.Bookmarks = slices.DeleteFunc(st.Bookmarks, func(b Bookmark) bool { return b.Space == spaceID && under(b.Path, rel) })
}

// MovedAcross follows a file or folder that moved from the path from of
// the space with the ID fromID to the path to of the space toID: its
// favorites and bookmarks go with it, as do the open folders and the order inside a
// folder, and the space it left forgets it.
func (st *State) MovedAcross(fromID, from, toID, to string) {
	re := renamer(from, to)
	src, dst := st.Get(fromID), st.Get(toID)
	if src != nil && dst != nil {
		for _, p := range src.Expanded {
			if under(p, from) {
				dst.SetExpanded(re(p), true)
			}
		}
		for k, names := range src.Order {
			if under(k, from) {
				if dst.Order == nil {
					dst.Order = map[string][]string{}
				}
				dst.Order[re(k)] = slices.Clone(names)
			}
		}
		for k, mode := range src.Sort {
			if under(k, from) {
				dst.SetSort(re(k), mode)
			}
		}
	}
	if src != nil {
		src.Forget(from)
	}
	for i, f := range st.Favorites {
		if f.Space == fromID && under(f.Path, from) {
			st.Favorites[i] = Favorite{toID, re(f.Path)}
		}
	}
	for i, b := range st.Bookmarks {
		if b.Space == fromID && under(b.Path, from) {
			st.Bookmarks[i].Space, st.Bookmarks[i].Path = toID, re(b.Path)
		}
	}
}

// migrate brings a state read from an older version up to date: version 3
// kept pins in each space, which become the favorites in space order.
func (st *State) migrate() {
	for _, s := range st.Spaces {
		for _, rel := range s.Pinned {
			if !st.IsFavorite(s.ID, rel) {
				st.Favorites = append(st.Favorites, Favorite{s.ID, rel})
			}
		}
		s.Pinned = nil
	}
}

// Icons are the default glyphs of new spaces, in turn.
var Icons = []string{"🌱", "📚", "💼", "🎨", "🏠", "✈️", "💡", "🌊"}

// State is everything the store keeps: the spaces in their order and the
// active one.
type State struct {
	Version int      `json:"version"`
	Active  string   `json:"active"`
	Spaces  []*Space `json:"spaces"`
	// Favorites are the documents pinned to the top of the sidebar in
	// every space.
	Favorites []Favorite `json:"favorites,omitempty"`
	// FavoritesHintDismissed hides the card that stands for no favorites,
	// as its × leaves it.
	FavoritesHintDismissed bool `json:"favoritesHintDismissed,omitempty"`
	// Bookmarks are the places marked in documents, oldest first.
	Bookmarks []Bookmark `json:"bookmarks,omitempty"`
}

// CurrentVersion is the version of the state's JSON format. Version 2
// added Space.Theme; a version 1 space reads as one without a theme.
// Version 3 added Space.Symbol; older spaces have only their Icon.
// Version 4 moved each space's Pinned to the State's Favorites, which every
// space shows. Version 5 added Space.Order, the order dragging leaves the
// sidebar in, and State.FavoritesHintDismissed; older states have neither.
// Version 6 added Space.Pins, what is pinned at the top of a space's list,
// and Space.PinsCollapsed. Version 7 added State.Bookmarks, the places
// marked in documents; older states have none. Version 8 added
// Space.Sort, folders sorted by name or time.
const CurrentVersion = 8

// ErrNotFound reports a space ID the state does not have.
var ErrNotFound = errors.New("space: not found")

// Get returns the space with the ID, or nil.
func (st *State) Get(id string) *Space {
	for _, s := range st.Spaces {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// ActiveSpace returns the active space, falling back to the first one, or
// nil when there are none.
func (st *State) ActiveSpace() *Space {
	if s := st.Get(st.Active); s != nil {
		return s
	}
	if len(st.Spaces) == 0 {
		return nil
	}
	st.Active = st.Spaces[0].ID
	return st.Spaces[0]
}

// SetActive makes the space with the ID active.
func (st *State) SetActive(id string) error {
	if st.Get(id) == nil {
		return ErrNotFound
	}
	st.Active = id
	return nil
}

// Add appends a space for the folder root, with the next icon and the
// next theme of NewTheme, and returns it.
func (st *State) Add(name, root string) *Space {
	n := len(st.Spaces)
	s := &Space{
		ID:   NewID(),
		Name: strings.TrimSpace(name),
		Icon: Icons[n%len(Icons)],
		Root: root,
	}
	s.SetTheme(NewTheme(n))
	if s.Name == "" {
		s.Name = "Space"
	}
	st.Spaces = append(st.Spaces, s)
	return s
}

// Remove deletes the space with the ID from the state; its folder stays on
// disk. When it was active, the space before it becomes active.
func (st *State) Remove(id string) error {
	i := slices.IndexFunc(st.Spaces, func(s *Space) bool { return s.ID == id })
	if i < 0 {
		return ErrNotFound
	}
	st.Spaces = slices.Delete(st.Spaces, i, i+1)
	st.Favorites = slices.DeleteFunc(st.Favorites, func(f Favorite) bool { return f.Space == id })
	st.Bookmarks = slices.DeleteFunc(st.Bookmarks, func(b Bookmark) bool { return b.Space == id })
	if st.Active == id {
		st.Active = ""
		if len(st.Spaces) > 0 {
			st.Active = st.Spaces[max(0, i-1)].ID
		}
	}
	return nil
}

// Move moves the space with the ID to index to, clamped to the list.
func (st *State) Move(id string, to int) error {
	i := slices.IndexFunc(st.Spaces, func(s *Space) bool { return s.ID == id })
	if i < 0 {
		return ErrNotFound
	}
	s := st.Spaces[i]
	st.Spaces = slices.Delete(st.Spaces, i, i+1)
	to = min(max(to, 0), len(st.Spaces))
	st.Spaces = slices.Insert(st.Spaces, to, s)
	return nil
}

// NewID returns a random ID of 12 hex digits.
func NewID() string {
	var b [6]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// SetIcon gives the space an emoji or a symbol, or neither, never both.
func (s *Space) SetIcon(emoji, symbol string) {
	if symbol != "" {
		emoji = ""
	}
	s.Icon, s.Symbol = emoji, symbol
}
