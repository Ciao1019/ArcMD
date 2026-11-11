package vault

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func symlinkForTest(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}
}

func TestSpaceBoundaryRejectsEscapingLinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := Write(outside, "victim.arcmd", "outside original"); err != nil {
		t.Fatal(err)
	}
	if err := Write(root, "local.arcmd", "local original"); err != nil {
		t.Fatal(err)
	}
	symlinkForTest(t, outside, filepath.Join(root, "escape"))
	symlinkForTest(t, filepath.Join(outside, "victim.arcmd"), filepath.Join(root, "linked.arcmd"))
	for _, rel := range []string{"escape/victim.arcmd", "escape/new/deep.arcmd", "linked.arcmd"} {
		if _, err := Resolve(root, rel); !errors.Is(err, ErrOutside) {
			t.Errorf("Resolve(%s) = %v", rel, err)
		}
		if err := Write(root, rel, "changed"); !errors.Is(err, ErrOutside) {
			t.Errorf("Write(%s) = %v", rel, err)
		}
		if _, err := Read(root, rel); !errors.Is(err, ErrOutside) {
			t.Errorf("Read(%s) = %v", rel, err)
		}
	}
	if _, err := CreateFile(root, "escape", "created", ".arcmd", "x"); !errors.Is(err, ErrOutside) {
		t.Errorf("CreateFile = %v", err)
	}
	if _, err := Mkdir(root, "escape", "folder"); !errors.Is(err, ErrOutside) {
		t.Errorf("Mkdir = %v", err)
	}
	if _, err := Rename(root, "escape/victim.arcmd", "renamed"); !errors.Is(err, ErrOutside) {
		t.Errorf("Rename = %v", err)
	}
	if _, err := Move(root, "local.arcmd", "escape"); !errors.Is(err, ErrOutside) {
		t.Errorf("Move = %v", err)
	}
	if _, _, err := MoveTo(root, "local.arcmd", root, "escape"); !errors.Is(err, ErrOutside) {
		t.Errorf("MoveTo = %v", err)
	}
	nodes, err := Tree(root)
	if err != nil || Find(nodes, "escape") != nil || Find(nodes, "linked.arcmd") != nil {
		t.Fatal("tree exposed an outside link", err)
	}
	docs, err := List(root)
	if err != nil || len(docs) != 1 || docs[0].Path != "local.arcmd" {
		t.Fatal("list exposed an outside link", docs, err)
	}
	if got, err := Read(outside, "victim.arcmd"); err != nil || got != "outside original" {
		t.Fatal("outside file changed", err)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 1 {
		t.Fatal("files were created outside the space", err)
	}
}

func TestSpaceBoundaryKeepsInternalLinksAndRootAliases(t *testing.T) {
	for _, absolute := range []bool{false, true} {
		t.Run(map[bool]string{false: "relative", true: "absolute"}[absolute], func(t *testing.T) {
			root := t.TempDir()
			if err := Write(root, "target/note.arcmd", "original"); err != nil {
				t.Fatal(err)
			}
			target := "target"
			if absolute {
				target = filepath.Join(root, target)
			}
			symlinkForTest(t, target, filepath.Join(root, "alias"))
			if err := Write(root, "alias/note.arcmd", "edited"); err != nil {
				t.Fatal("internal directory link stopped working", err)
			}
			if got, err := Read(root, "target/note.arcmd"); err != nil || got != "edited" {
				t.Fatal("internal link did not reach its target", err)
			}
			if _, err := CreateFile(root, "alias/new", "created", ".arcmd", "new"); err != nil {
				t.Fatal("creation through an internal link failed", err)
			}
			// A link to an ancestor must not recurse indefinitely, while the
			// non-cyclic alias remains usable in the tree.
			symlinkForTest(t, "..", filepath.Join(root, "target", "loop"))
			nodes, err := Tree(root)
			if err != nil || Find(nodes, "alias/note.arcmd") == nil || Find(nodes, "alias/loop") != nil {
				t.Fatal("internal tree links or cycle protection failed", err)
			}
			aliasRoot := filepath.Join(t.TempDir(), "space")
			symlinkForTest(t, root, aliasRoot)
			if err := Write(aliasRoot, "alias/note.arcmd", "root alias"); err != nil {
				t.Fatal("a Space rooted at a symbolic link stopped working", err)
			}
		})
	}
}

func TestResolveRejectsDanglingEscape(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing")
	symlinkForTest(t, missing, filepath.Join(root, "escape"))
	if err := Write(root, "escape/new.arcmd", "x"); err == nil {
		t.Fatal("wrote through a dangling outside link")
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("created the dangling link target", err)
	}
}

func TestInternalLinkMayUseRootCaseAlias(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "Space")
	if err := Write(root, "target/note.arcmd", "original"); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "space")
	if _, err := os.Stat(alias); errors.Is(err, os.ErrNotExist) {
		t.Skip("case-insensitive filesystem required")
	} else if err != nil {
		t.Fatal(err)
	}
	symlinkForTest(t, filepath.Join(alias, "target"), filepath.Join(root, "alias"))
	if err := Write(root, "alias/note.arcmd", "edited"); err != nil {
		t.Fatal("an internal link with different root casing was rejected", err)
	}
	if got, err := Read(root, "target/note.arcmd"); err != nil || got != "edited" {
		t.Fatal("case alias did not reach the internal target", err)
	}
}

func TestCaseOnlyRename(t *testing.T) {
	root := t.TempDir()
	if err := Write(root, "Note.arcmd", "original"); err != nil {
		t.Fatal(err)
	}
	if got, err := Rename(root, "Note.arcmd", "note"); err != nil || got != "note.arcmd" {
		t.Fatal("case-only rename failed", got, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "note.arcmd" {
		t.Fatal("case-only rename did not update the name", err)
	}
	if got, err := Read(root, "note.arcmd"); err != nil || got != "original" {
		t.Fatal("case-only rename changed the content", err)
	}
}

func TestCaseOnlyRenamePreservesDistinctTarget(t *testing.T) {
	root := t.TempDir()
	if err := Write(root, "Note.arcmd", "source"); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(root, "note.arcmd"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		t.Skip("case-sensitive filesystem required; also run on Linux")
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("target"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Rename(root, "Note.arcmd", "note"); !errors.Is(err, ErrExists) {
		t.Fatal("case collision was not refused", err)
	}
	for rel, want := range map[string]string{"Note.arcmd": "source", "note.arcmd": "target"} {
		if got, err := Read(root, rel); err != nil || got != want {
			t.Fatalf("%s was changed: %q, %v", rel, got, err)
		}
	}
}
