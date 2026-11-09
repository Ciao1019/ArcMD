package vault

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestResolve(t *testing.T) {
	root := t.TempDir()
	for _, bad := range []string{"", ".", "../x.md", "a/../../x.md", "/etc/passwd", "a/.."} {
		if _, err := Resolve(root, bad); err != ErrOutside {
			t.Errorf("Resolve(%q) = %v, want ErrOutside", bad, err)
		}
	}
	got, err := Resolve(root, "a/./b.md")
	if err != nil || got != filepath.Join(root, "a", "b.md") {
		t.Errorf("Resolve(a/./b.md) = %q, %v", got, err)
	}
}

func TestListSkipsHiddenAndNonMarkdown(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"b.md", "A.md", "notes/z.MD", "notes/deep/y.md", ".git/c.md", ".hidden.md", "img.png", "node_modules/x.md"} {
		full := filepath.Join(root, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte("x"), 0o644)
	}
	docs, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, d := range docs {
		paths = append(paths, d.Path)
	}
	want := []string{"A.md", "b.md", "notes/z.MD", "notes/deep/y.md"}
	if !slices.Equal(paths, want) {
		t.Errorf("List = %q, want %q", paths, want)
	}
}

func TestCreateWriteRead(t *testing.T) {
	root := t.TempDir()
	a, err := Create(root, "", "Idea: one/two")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Create(root, "", "Idea: one/two")
	if err != nil {
		t.Fatal(err)
	}
	if a != "Idea- one-two.md" || b != "Idea- one-two 2.md" {
		t.Errorf("Create = %q, %q", a, b)
	}
	if err := Write(root, "sub/n.md", "hello"); err != nil {
		t.Fatal(err)
	}
	if got, err := Read(root, "sub/n.md"); err != nil || got != "hello" {
		t.Errorf("Read = %q, %v", got, err)
	}
	if Title("sub/n.md") != "n" {
		t.Errorf("Title = %q", Title("sub/n.md"))
	}
}

func TestTreeAndMoves(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"b.md", "Zed/a.md", "alpha/x.md", "alpha/deep/y.md", ".git/c.md", "img.png"} {
		full := filepath.Join(root, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte("x"), 0o644)
	}
	os.Mkdir(filepath.Join(root, "empty"), 0o755)
	nodes, err := Tree(root)
	if err != nil {
		t.Fatal(err)
	}
	var top []string
	for _, n := range nodes {
		top = append(top, n.Name)
	}
	if !slices.Equal(top, []string{"alpha", "empty", "Zed", "b.md"}) {
		t.Errorf("top = %q", top)
	}
	if n := Find(nodes, "alpha/deep/y.md"); n == nil || n.Title() != "y" {
		t.Errorf("Find = %+v", n)
	}

	dir, err := Mkdir(root, "alpha", "")
	if err != nil || dir != "alpha/New Folder" {
		t.Fatalf("Mkdir = %q, %v", dir, err)
	}
	moved, err := Move(root, "b.md", "alpha")
	if err != nil || moved != "alpha/b.md" {
		t.Fatalf("Move = %q, %v", moved, err)
	}
	if _, err := Move(root, "alpha", "alpha/deep"); err == nil {
		t.Error("moved a folder into itself")
	}
	renamed, err := Rename(root, "alpha/b.md", "Better")
	if err != nil || renamed != "alpha/Better.md" {
		t.Fatalf("Rename = %q, %v", renamed, err)
	}
	if _, err := Rename(root, "alpha/Better.md", "x"); err != ErrExists {
		t.Errorf("Rename onto x.md = %v, want ErrExists", err)
	}
	if renamed, err := Rename(root, "Zed", "Zeta"); err != nil || renamed != "Zeta" {
		t.Errorf("Rename folder = %q, %v", renamed, err)
	}
}

func TestNativeDocumentsAndExclusiveCreation(t *testing.T) {
	root := t.TempDir()
	first, err := CreateFile(root, "", "Note", ".arcmd", `{"version":1}`)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateFile(root, "", "Note", ".arcmd", `{"version":2}`)
	if err != nil || first == second {
		t.Fatal("collision", err)
	}
	docs, err := List(root)
	if err != nil || len(docs) != 2 {
		t.Fatal("native listing", err)
	}
	renamed, err := Rename(root, first, "Changed")
	if err != nil || renamed != "Changed.arcmd" {
		t.Fatal("native rename", renamed, err)
	}
	if IsMarkdown(renamed) || !IsDocument(renamed) {
		t.Fatal("native format classification")
	}
}
