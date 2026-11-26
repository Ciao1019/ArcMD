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

func TestFilesMissingDetection(t *testing.T) {
	root := t.TempDir()
	keptMD := filepath.Join(root, "kept.md")
	keptPDF := filepath.Join(root, "kept.pdf")
	goneMD := filepath.Join(root, "gone.md")
	gonePDF := filepath.Join(root, "gone.pdf")
	goneDir := filepath.Join(root, "vanished")
	goneNested := filepath.Join(goneDir, "nested.md")
	turnedDir := filepath.Join(root, "replaced.md")

	for _, p := range []string{keptMD, keptPDF} {
		if err := os.WriteFile(p, []byte("ok"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(turnedDir, 0700); err != nil {
		t.Fatal(err)
	}

	f := Files{
		"kept.md":     keptMD,
		"kept.pdf":    keptPDF,
		"gone.md":     goneMD,
		"gone.pdf":    gonePDF,
		"nested.md":   goneNested,
		"replaced.md": turnedDir,
	}

	wantMissing := map[string]bool{
		"kept.md":     false,
		"kept.pdf":    false,
		"gone.md":     true,
		"gone.pdf":    true,
		"nested.md":   true,
		"replaced.md": true,
	}

	for rel, want := range wantMissing {
		if got := f.Missing(rel); got != want {
			t.Fatalf("Missing(%q) = %v, want %v", rel, got, want)
		}
	}
	if f.Missing("unregistered.md") {
		t.Fatal("unregistered entry reported missing")
	}

	nodes := f.Tree()
	if len(nodes) != len(wantMissing) {
		t.Fatalf("Tree len = %d, want %d", len(nodes), len(wantMissing))
	}
	for _, n := range nodes {
		if n.Missing != wantMissing[n.Path] {
			t.Fatalf("node %q Missing = %v, want %v", n.Path, n.Missing, wantMissing[n.Path])
		}
		if !n.Missing && n.Modified.IsZero() {
			t.Fatalf("existing node %q has zero Modified time", n.Path)
		}
	}
}
