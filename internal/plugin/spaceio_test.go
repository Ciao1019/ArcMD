package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsSyncPath(t *testing.T) {
	allowed := []string{
		"Welcome.md",
		"Notes/Ideas.markdown",
		"Docs/post.mdx",
		"Old/legacy.arcmd",
		"Board.excalidraw.svg",
		"Sketch.excalidraw",
		"Report.pdf",
		"Papers/Spec.pdf",
		"assets/image.png",
		"Notes/assets/diagram.webp",
		"Notes/assets/custom.bin",
		"images/photo.jpg",
		".arcmd/templates/Weekly.md",
		".arcmd/templates/sub/Meeting.arcmd",
		".arcmd/templates/assets/logo.png",
	}
	for _, p := range allowed {
		if !IsSyncPath(p) {
			t.Errorf("IsSyncPath(%q) = false, want true", p)
		}
	}

	blocked := []string{
		"",
		".",
		"..",
		"/abs/path.md",
		"../escape.md",
		"Notes/../escape.md",
		`Notes\win.md`,
		"Notes//double.md",
		"Notes/",
		".DS_Store",
		"Notes/.DS_Store",
		".atomic-123.tmp",
		".git/config",
		".git/HEAD",
		"node_modules/pkg/README.md",
		"Notes/node_modules/a.md",
		".arcmd",
		".arcmd/templates",
		".arcmd/templates/.DS_Store",
		".arcmd/history/Welcome.md/20261119.md",
		".arcmd/history/labels.json",
		"Export.html",
		"Notes/archive.zip",
	}
	for _, p := range blocked {
		if IsSyncPath(p) {
			t.Errorf("IsSyncPath(%q) = true, want false", p)
		}
	}
}

func TestFolderSpacesOperations(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.md")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	mustWrite := func(rel string, data []byte) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mustWrite("Welcome.md", []byte("# Welcome\n"))
	mustWrite("Notes/Ideas.md", []byte("# Ideas\n"))
	mustWrite("assets/pic.png", []byte{0x89, 'P', 'N', 'G'})
	mustWrite(".arcmd/templates/Daily.md", []byte("# {{date}}\n"))
	mustWrite(".arcmd/history/Welcome.md/20261119-000000.000.md", []byte("old"))
	mustWrite(".git/HEAD", []byte("ref: refs/heads/main\n"))
	mustWrite(".DS_Store", []byte("junk"))
	mustWrite("node_modules/readme.md", []byte("skip"))
	if err := os.Symlink(outsideFile, filepath.Join(root, "escape.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	var trashed []string
	dirty := map[SpaceFileKey]bool{
		{SpaceID: "sp1", Path: "Notes/Ideas.md"}: true,
	}
	fs := NewFolderSpaces(
		[]SpaceSnapshot{{ID: "sp1", Name: "Personal", Root: root}},
		dirty,
		func(spaceID, spaceName, spRoot, rel string) error {
			trashed = append(trashed, spaceID+":"+rel)
			return os.Remove(filepath.Join(spRoot, filepath.FromSlash(rel)))
		},
	)
	fixedNow := time.Date(2026, 11, 19, 10, 30, 0, 0, time.Local)
	fs.now = func() time.Time { return fixedNow }

	files, err := fs.ListFiles("sp1")
	if err != nil {
		t.Fatal(err)
	}
	var gotPaths []string
	var sawDirty bool
	for _, f := range files {
		gotPaths = append(gotPaths, f.Path)
		if f.Path == "Notes/Ideas.md" && f.Dirty {
			sawDirty = true
		}
	}
	wantPaths := []string{
		".arcmd/templates/Daily.md",
		"Notes/Ideas.md",
		"Welcome.md",
		"assets/pic.png",
	}
	if strings.Join(gotPaths, ",") != strings.Join(wantPaths, ",") {
		t.Fatalf("ListFiles = %v, want %v", gotPaths, wantPaths)
	}
	if !sawDirty {
		t.Fatal("dirty flag missing on Notes/Ideas.md")
	}

	// Reading symlink escape or history must be rejected.
	if _, _, err := fs.ReadFile("sp1", "escape.md"); err == nil {
		t.Fatal("symlink file read succeeded")
	}
	if _, _, err := fs.ReadFile("sp1", ".arcmd/history/Welcome.md/20261119-000000.000.md"); err == nil {
		t.Fatal("history file read succeeded")
	}

	// Optimistic concurrency on WriteFile.
	b, meta, err := fs.ReadFile("sp1", "Welcome.md")
	if err != nil || string(b) != "# Welcome\n" || meta.SHA256 == "" {
		t.Fatalf("ReadFile = %q, %+v, %v", b, meta, err)
	}
	if _, err := fs.WriteFile("sp1", "Welcome.md", []byte("bad"), "deadbeef", 0); err == nil {
		t.Fatal("WriteFile succeeded with wrong expectedSHA256")
	} else {
		var se *SpaceError
		if !errors.As(err, &se) || se.Code != CodeConflict {
			t.Fatalf("expected conflict error, got %v", err)
		}
	}
	if _, err := fs.WriteFile("sp1", "Notes/Ideas.md", []byte("new"), "", 0); err == nil {
		t.Fatal("WriteFile overwrote dirty open document")
	} else {
		var se *SpaceError
		if !errors.As(err, &se) || se.Code != CodeDirty {
			t.Fatalf("expected dirty error, got %v", err)
		}
	}

	sum := sha256.Sum256(b)
	mt := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC).UnixNano()
	updated, err := fs.WriteFile("sp1", "Welcome.md", []byte("# Welcome v2\n"), hex.EncodeToString(sum[:]), mt)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ModTimeUnixNano != mt {
		t.Errorf("ModTime = %d, want %d", updated.ModTimeUnixNano, mt)
	}

	// WriteConflict creates numbered non-overwriting copies.
	c1, err := fs.WriteConflict("sp1", "Welcome.md", []byte("remote 1"))
	if err != nil {
		t.Fatal(err)
	}
	if c1.Path != "Welcome (冲突副本 2026-11-19 1030).md" {
		t.Fatalf("conflict 1 path = %q", c1.Path)
	}
	c2, err := fs.WriteConflict("sp1", "Welcome.md", []byte("remote 2"))
	if err != nil {
		t.Fatal(err)
	}
	if c2.Path != "Welcome (冲突副本 2026-11-19 1030)-2.md" {
		t.Fatalf("conflict 2 path = %q", c2.Path)
	}

	// DeleteFile uses TrashFunc and verifies expectedSHA256.
	if err := fs.DeleteFile("sp1", "Welcome.md", "wrong"); err == nil {
		t.Fatal("DeleteFile succeeded with wrong SHA")
	}
	if err := fs.DeleteFile("sp1", "Welcome.md", updated.SHA256); err != nil {
		t.Fatal(err)
	}
	if len(trashed) != 1 || trashed[0] != "sp1:Welcome.md" {
		t.Fatalf("trashed = %v", trashed)
	}
	if len(fs.Touched()) == 0 {
		t.Fatal("Touched() is empty")
	}
}
