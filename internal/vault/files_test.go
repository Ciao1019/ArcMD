package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilesExplicitBoundary(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "chosen.md")
	if err := os.WriteFile(file, []byte("chosen"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sibling.md"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	f := Files{"chosen.md": file, "../escape.md": file, "bad.md": "relative.md"}
	if p, err := f.Resolve("chosen.md"); err != nil || p != file {
		t.Fatal(p, err)
	}
	for _, rel := range []string{"sibling.md", "../escape.md", "bad.md", file} {
		if _, err := f.Resolve(rel); err == nil {
			t.Fatalf("resolved %s", rel)
		}
	}
	if list := f.Tree(); len(list) != 1 || list[0].Dir || list[0].Name != "chosen.md" {
		t.Fatal(list)
	}
	if _, err := Resolve(t.TempDir(), file); err == nil {
		t.Fatal("ordinary boundary weakened")
	}
}
