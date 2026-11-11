package space

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBookmarks(t *testing.T) {
	st := &State{}
	a, b := st.Add("A", "/a"), st.Add("B", "/b")
	at := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	st.AddBookmark(Bookmark{Space: a.ID, Path: "notes/x.arcmd", Block: "b1", Excerpt: "First", Created: at})
	st.AddBookmark(Bookmark{Space: a.ID, Path: "notes/x.arcmd", Block: "b1", Excerpt: "Again", Created: at})
	st.AddBookmark(Bookmark{Space: a.ID, Path: "notes/x.arcmd", Block: "b2", Label: "Named", Excerpt: "Second", Created: at})
	st.AddBookmark(Bookmark{Space: a.ID, Path: "y.arcmd", Block: "b3", Created: at})
	if got := st.BookmarksIn(a.ID, "notes/x.arcmd"); len(got) != 2 || got[0].Title() != "First" || got[1].Title() != "Named" {
		t.Fatalf("bookmarks of the document = %+v", got)
	}

	// A renamed folder takes its documents' bookmarks along.
	st.Renamed(a.ID, "notes", "ideas")
	if st.BookmarkIndex(a.ID, "ideas/x.arcmd", "b1") < 0 || st.BookmarkIndex(a.ID, "notes/x.arcmd", "b1") >= 0 {
		t.Fatalf("bookmarks after the rename = %+v", st.Bookmarks)
	}
	// So does a document moved to another space.
	st.MovedAcross(a.ID, "ideas/x.arcmd", b.ID, "x 2.arcmd")
	if len(st.BookmarksIn(b.ID, "x 2.arcmd")) != 2 || len(st.BookmarksIn(a.ID, "ideas/x.arcmd")) != 0 {
		t.Fatalf("bookmarks after the move = %+v", st.Bookmarks)
	}
	if !st.RemoveBookmark(b.ID, "x 2.arcmd", "b2") || st.RemoveBookmark(b.ID, "x 2.arcmd", "b2") {
		t.Fatal("RemoveBookmark must report whether there was a bookmark")
	}
	// A document that is gone has no bookmarks, nor has a removed space.
	st.Forget(a.ID, "y.arcmd")
	if len(st.BookmarksIn(a.ID, "y.arcmd")) != 0 {
		t.Fatalf("bookmarks of a forgotten document = %+v", st.Bookmarks)
	}
	if err := st.Remove(b.ID); err != nil || len(st.Bookmarks) != 0 {
		t.Fatalf("bookmarks after removing the space = %+v, %v", st.Bookmarks, err)
	}
}

func TestStoreKeepsBookmarks(t *testing.T) {
	file := filepath.Join(t.TempDir(), "spaces.json")
	// A version 6 state has no bookmarks and reads as it is.
	old := `{"version": 6, "active": "s1", "spaces": [{"id": "s1", "name": "A", "root": "/a"}]}`
	if err := os.WriteFile(file, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(file)
	st, err := store.Load()
	if err != nil || len(st.Bookmarks) != 0 || len(st.Spaces) != 1 {
		t.Fatalf("version 6 state = %+v, %v", st, err)
	}
	at := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	st.AddBookmark(Bookmark{Space: "s1", Path: "x.arcmd", Block: "b1", Excerpt: "First", Label: "Start", Created: at})
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	again, err := store.Load()
	if err != nil || again.Version != 7 || len(again.Bookmarks) != 1 || again.Bookmarks[0] != st.Bookmarks[0] {
		t.Fatalf("saved state = %+v, %v", again, err)
	}
}
