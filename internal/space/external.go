package space

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ExternalID identifies the single built-in collection of opened files.
const ExternalID = "external"

// ErrBuiltIn prevents removing the built-in space, even when it is empty.
var ErrBuiltIn = errors.New("space: built-in spaces cannot be removed")

// IsExternal reports whether this is the built-in collection, not a folder.
func (s *Space) IsExternal() bool { return s != nil && s.ID == ExternalID }

// EnsureExternal creates the collection on first use and keeps it thereafter.
// root is a private metadata directory; it never contains the user's files.
func (st *State) EnsureExternal(name, root string) *Space {
	if s := st.Get(ExternalID); s != nil {
		return s
	}
	s := &Space{ID: ExternalID, Name: name, Root: root, Symbol: "file", ExternalFiles: map[string]string{}}
	s.SetTheme(NewTheme(0))
	st.Spaces = append(st.Spaces, s)
	return s
}

// AddExternal registers an absolute file without scanning or moving anything.
// Local names stay flat and unambiguous even when two folders contain README.md.
func (s *Space) AddExternal(full string) string {
	if !s.IsExternal() || !filepath.IsAbs(full) {
		return ""
	}
	full = filepath.Clean(full)
	for rel, file := range s.ExternalFiles {
		if file == full {
			return rel
		}
	}
	if s.ExternalFiles == nil {
		s.ExternalFiles = map[string]string{}
	}
	base := filepath.Base(full)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for n := 1; ; n++ {
		rel := base
		if n > 1 {
			rel = fmt.Sprintf("%s %d%s", stem, n, ext)
		}
		if _, exists := s.ExternalFiles[rel]; !exists {
			s.ExternalFiles[rel] = full
			return rel
		}
	}
}
