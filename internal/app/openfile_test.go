package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSystemOpenUsesDeepestSpace(t *testing.T) {
	a, _ := newTestApp(t)
	nested := a.state.Add("Journal", filepath.Join(root(a), "Journal"))
	count := len(a.state.Spaces)
	if !a.openSystemFile(filepath.Join(nested.Root, "Ideas.arcmd")) {
		t.Fatal("open")
	}
	if a.doc.space != nested || a.doc.rel != "Ideas.arcmd" || len(a.state.Spaces) != count {
		t.Fatal("wrong space")
	}
}

func TestSystemOpenExternalMarkdownPreservesSource(t *testing.T) {
	a, _ := newTestApp(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "External.md")
	text := "# External\n\nUntouched Markdown.\n"
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	if !a.openSystemFile(file) || a.doc.rel != "External.md" {
		t.Fatal("open")
	}
	count := len(a.state.Spaces)
	if !a.openSystemFile(file) || len(a.state.Spaces) != count {
		t.Fatal("duplicate space")
	}
	if data, _ := os.ReadFile(file); string(data) != text {
		t.Fatal("source changed")
	}
	if _, err := os.Stat(filepath.Join(dir, "External.arcmd")); !os.IsNotExist(err) {
		t.Fatal("opening converted Markdown")
	}
	if a.openSystemFile(dir) || a.openSystemFile(file+".missing") {
		t.Fatal("invalid input accepted")
	}
}

func TestSystemOpenRespectsUnsavedConflict(t *testing.T) {
	a, _ := newTestApp(t)
	old := a.doc
	a.doc.ed.ReplaceText(nil, "unsaved")
	a.doc.conflict = true
	count := len(a.state.Spaces)
	dir := t.TempDir()
	file := filepath.Join(dir, "External.md")
	if err := os.WriteFile(file, []byte("# Test"), 0600); err != nil {
		t.Fatal(err)
	}
	if a.openSystemFile(file) || a.doc != old || len(a.state.Spaces) != count {
		t.Fatal("conflict was bypassed")
	}
}

func TestForwardedArgumentsKeepFirstFile(t *testing.T) {
	dir := t.TempDir()
	absolute := filepath.Join(dir, "Absolute.arcmd")
	files := documentArguments([]string{"First.md", "--flag", absolute}, dir)
	if len(files) != 2 || files[0] != filepath.Join(dir, "First.md") || files[1] != absolute {
		t.Fatal(files)
	}
	if files := documentArguments(nil, dir); len(files) != 0 {
		t.Fatal(files)
	}
}
