package space

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestStateLifecycle(t *testing.T) {
	st := &State{}
	if st.ActiveSpace() != nil {
		t.Fatal("empty state has an active space")
	}
	a := st.Add("Work", "/w")
	b := st.Add("  ", "/b")
	if b.Name != "Space" || a.Color == b.Color || a.Icon == b.Icon {
		t.Errorf("defaults not applied: %+v %+v", a, b)
	}
	if st.ActiveSpace() != a {
		t.Error("first space should be active by default")
	}
	if err := st.SetActive(b.ID); err != nil || st.ActiveSpace() != b {
		t.Fatalf("SetActive: %v", err)
	}
	if err := st.SetActive("nope"); err != ErrNotFound {
		t.Errorf("SetActive(nope) = %v", err)
	}
	if err := st.Move(b.ID, 0); err != nil || st.Spaces[0] != b {
		t.Fatalf("Move: %v", err)
	}
	if err := st.Remove(b.ID); err != nil || st.Active != a.ID {
		t.Fatalf("Remove: %v, active %q", err, st.Active)
	}
}

func TestFavorites(t *testing.T) {
	st := &State{}
	a, b := st.Add("A", "/a"), st.Add("B", "/b")
	st.ToggleFavorite(a.ID, "x.md")
	st.ToggleFavorite(b.ID, "x.md")
	st.ToggleFavorite(a.ID, "dir/y.md")
	st.ToggleFavorite(a.ID, "x.md")
	if st.IsFavorite(a.ID, "x.md") || !st.IsFavorite(b.ID, "x.md") || !st.IsFavorite(a.ID, "dir/y.md") {
		t.Fatalf("favorites = %v", st.Favorites)
	}
	st.MoveFavorite(1, 0)
	if st.Favorites[0] != (Favorite{a.ID, "dir/y.md"}) {
		t.Fatalf("after MoveFavorite: %v", st.Favorites)
	}
	st.Renamed(a.ID, "dir", "z")
	if !st.IsFavorite(a.ID, "z/y.md") || !st.IsFavorite(b.ID, "x.md") {
		t.Fatalf("after Renamed: %v", st.Favorites)
	}
	a.LastOpen = "z/y.md"
	st.Forget(a.ID, "z")
	if len(st.Favorites) != 1 || a.LastOpen != "" {
		t.Fatalf("after Forget: %v %q", st.Favorites, a.LastOpen)
	}
	st.Remove(b.ID)
	if len(st.Favorites) != 0 {
		t.Fatalf("a removed space kept its favorites: %v", st.Favorites)
	}
}

// Version 3 kept pins in each space; they become the favorites.
func TestStoreMigratesPins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spaces.json")
	os.WriteFile(path, []byte(`{"version":3,"active":"a","spaces":[
		{"id":"a","name":"A","root":"/a","pinned":["x.md","y.md"]},
		{"id":"b","name":"B","root":"/b","pinned":["z.md"]}]}`), 0o644)
	st, err := NewStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []Favorite{{"a", "x.md"}, {"a", "y.md"}, {"b", "z.md"}}
	if !slices.Equal(st.Favorites, want) || st.Spaces[0].Pinned != nil || st.Version != CurrentVersion {
		t.Fatalf("migrated = %+v, %+v", st.Favorites, st.Spaces[0])
	}
}

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "spaces.json")
	store := NewStore(path)
	st, err := store.Load()
	if err != nil || len(st.Spaces) != 0 {
		t.Fatalf("Load of a missing file = %v, %v", st, err)
	}
	s := st.Add("Notes", "/n")
	st.ToggleFavorite(s.ID, "x.md")
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Spaces) != 1 || got.Spaces[0].Name != "Notes" || !got.IsFavorite(s.ID, "x.md") {
		t.Errorf("round trip = %+v", got.Spaces[0])
	}
}

func TestStoreRejectsNewerVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spaces.json")
	os.WriteFile(path, []byte(`{"version": 99}`), 0o644)
	if _, err := NewStore(path).Load(); err == nil {
		t.Error("Load accepted a newer format")
	}
}

func TestRenamedAndExpanded(t *testing.T) {
	s := &Space{LastOpen: "a/x.md"}
	s.SetExpanded("a", true)
	s.SetExpanded("a/sub", true)
	s.Renamed("a", "z")
	if s.LastOpen != "z/x.md" || !s.IsExpanded("z/sub") || s.IsExpanded("a") {
		t.Errorf("after Renamed: %+v", s)
	}
	s.Forget("z")
	if len(s.Expanded) != 0 || s.LastOpen != "" {
		t.Errorf("after Forget: %+v", s)
	}
}

func TestSetIcon(t *testing.T) {
	var s Space
	s.SetIcon("📚", "")
	if s.Icon != "📚" || s.Symbol != "" {
		t.Errorf("emoji: %+v", s)
	}
	s.SetIcon("📚", "star")
	if s.Icon != "" || s.Symbol != "star" {
		t.Errorf("a symbol should replace the emoji: %+v", s)
	}
	s.SetIcon("", "")
	if s.Icon != "" || s.Symbol != "" {
		t.Errorf("no icon: %+v", s)
	}
}

func TestOrder(t *testing.T) {
	s := &Space{}
	names := []string{"a", "b", "c"}
	if got := s.Arrange("", names); !slices.Equal(got, names) {
		t.Fatalf("no order: %v", got)
	}
	s.Place("", names, "c", 0)
	if got := s.Arrange("", []string{"a", "b", "c", "new"}); !slices.Equal(got, []string{"c", "a", "b", "new"}) {
		t.Fatalf("placed: %v", got)
	}
	// Renamed in place keeps its place; folders inside follow.
	s.Place("dir", []string{"x", "y"}, "y", 0)
	s.Renamed("a", "z")
	s.Renamed("dir", "folder")
	if got := s.Arrange("", []string{"b", "c", "z"}); !slices.Equal(got, []string{"c", "z", "b"}) {
		t.Fatalf("after rename: %v", got)
	}
	if got := s.Arrange("folder", []string{"x", "y"}); !slices.Equal(got, []string{"y", "x"}) {
		t.Fatalf("folder's order did not follow: %v %v", got, s.Order)
	}
	// Moved to another folder, it leaves the old list; gone, it leaves too.
	s.Renamed("b", "folder/b")
	if slices.Contains(s.Order[""], "b") {
		t.Fatalf("moved name kept: %v", s.Order[""])
	}
	s.Forget("folder")
	if s.Order["folder"] != nil || slices.Contains(s.Order[""], "folder") {
		t.Fatalf("after Forget: %v", s.Order)
	}
}

func TestPins(t *testing.T) {
	s := &Space{}
	s.Pin("a.md", 0)
	s.Pin("dir", 0)
	s.Pin("b.md", 5)
	s.Pin("a.md", 0) // moves
	if !slices.Equal(s.Pins, []string{"a.md", "dir", "b.md"}) {
		t.Fatalf("pins %v", s.Pins)
	}
	s.Renamed("dir", "folder")
	s.Unpin("b.md")
	if !s.IsPinned("folder") || s.IsPinned("b.md") {
		t.Fatalf("after rename/unpin %v", s.Pins)
	}
	s.Forget("folder")
	if !slices.Equal(s.Pins, []string{"a.md"}) {
		t.Fatalf("after Forget %v", s.Pins)
	}
}

func TestMovedAcross(t *testing.T) {
	st := &State{}
	a := st.Add("A", t.TempDir())
	b := st.Add("B", t.TempDir())
	a.Pin("Trip", 0)
	a.SetExpanded("Trip", true)
	a.SetExpanded("Trip/Days", true)
	a.Order = map[string][]string{"": {"Trip", "Notes.arcmd"}, "Trip": {"b.arcmd", "a.arcmd"}}
	a.LastOpen = "Trip/a.arcmd"
	st.Favorites = []Favorite{{a.ID, "Trip/a.arcmd"}, {a.ID, "Notes.arcmd"}}

	st.MovedAcross(a.ID, "Trip", b.ID, "Inbox/Trip")
	if a.IsPinned("Trip") || a.IsExpanded("Trip") || a.LastOpen != "" || a.Order["Trip"] != nil {
		t.Errorf("A kept the folder: %+v", a)
	}
	if got := a.Order[""]; len(got) != 1 || got[0] != "Notes.arcmd" {
		t.Errorf("A's order = %v", got)
	}
	if !b.IsExpanded("Inbox/Trip") || !b.IsExpanded("Inbox/Trip/Days") || b.Order["Inbox/Trip"][0] != "b.arcmd" {
		t.Errorf("B did not take the folder's state: %+v", b)
	}
	if st.Favorites[0] != (Favorite{b.ID, "Inbox/Trip/a.arcmd"}) || st.Favorites[1] != (Favorite{a.ID, "Notes.arcmd"}) {
		t.Errorf("favorites = %v", st.Favorites)
	}
}

func TestFolderSortFollowsAndClears(t *testing.T) {
	s := &Space{}
	s.SetSort("Notes", "modified")
	s.SetSort("", "name")
	s.SetSort("x", "bogus")
	if s.SortOf("Notes") != "modified" || s.SortOf("") != "name" || s.SortOf("x") != "" {
		t.Fatal(s.Sort)
	}
	s.Renamed("Notes", "Journal")
	if s.SortOf("Journal") != "modified" || s.SortOf("Notes") != "" {
		t.Fatal("renaming follows", s.Sort)
	}
	s.Place("", []string{"a", "b"}, "b", 0)
	if s.SortOf("") != "" {
		t.Fatal("placing by hand ends the sort")
	}
	s.Forget("Journal")
	if len(s.Sort) != 0 {
		t.Fatal("forgetting drops it", s.Sort)
	}
}
