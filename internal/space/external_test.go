package space

import (
	"path/filepath"
	"testing"
)

func TestExternalSpacePersistsAndCannotBeRemoved(t *testing.T) {
	st := &State{}
	root := t.TempDir()
	sp := st.EnsureExternal("External", root)
	first := filepath.Join(root, "one", "README.md")
	second := filepath.Join(root, "two", "README.md")
	a, b := sp.AddExternal(first), sp.AddExternal(second)
	if a == b || a == "" || b == "" || sp.AddExternal(first) != a {
		t.Fatal("file identity was not stable")
	}
	if st.EnsureExternal("External", root) != sp || len(st.Spaces) != 1 {
		t.Fatal("duplicate built-in")
	}
	if err := st.Remove(sp.ID); err != ErrBuiltIn {
		t.Fatal("removed built-in", err)
	}
	store := NewStore(filepath.Join(root, "spaces.json"))
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Get(ExternalID).ExternalFiles[b] != second {
		t.Fatal("file reference not persisted")
	}
	if sp.AddExternal("relative.md") != "" {
		t.Fatal("accepted relative file")
	}
}
