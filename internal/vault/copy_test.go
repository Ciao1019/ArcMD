package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyAndCopyTo(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	write := func(root, rel, text string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(a, "Meeting.md", "# Meeting\n")
	write(a, "Board.excalidraw.svg", "<svg/>")
	write(a, "Templates/Weekly.md", "# Weekly\n")
	write(a, "Templates/assets/logo.png", "pngdata")
	write(a, "Templates/.hidden.md", "secret")
	write(a, "Templates/.git/config", "git")
	write(a, "Templates/node_modules/pkg/index.js", "js")
	if err := os.MkdirAll(filepath.Join(a, "Templates", "Empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(a, "Archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(b, "Meeting.md", "# Existing\n")

	// Copying a document in the same folder picks an unused numbered name
	// and leaves the original intact.
	got, err := Copy(a, "Meeting.md", "")
	if err != nil || got != "Meeting 2.md" {
		t.Fatalf("Copy same dir = %q, %v", got, err)
	}
	got2, err := Copy(a, "Meeting.md", "")
	if err != nil || got2 != "Meeting 3.md" {
		t.Fatalf("second Copy same dir = %q, %v", got2, err)
	}
	if text, _ := Read(a, "Meeting.md"); text != "# Meeting\n" {
		t.Fatalf("original changed: %q", text)
	}
	if text, _ := Read(a, "Meeting 2.md"); text != "# Meeting\n" {
		t.Fatalf("copied text = %q", text)
	}

	// Compound whiteboard extensions preserve ".excalidraw.svg" before numbering.
	wb, err := Copy(a, "Board.excalidraw.svg", "")
	if err != nil || wb != "Board 2.excalidraw.svg" {
		t.Fatalf("Copy whiteboard = %q, %v", wb, err)
	}

	// Copying into a subfolder keeps the original filename when unused.
	sub, err := Copy(a, "Meeting.md", "Archive")
	if err != nil || sub != "Archive/Meeting.md" {
		t.Fatalf("Copy into subfolder = %q, %v", sub, err)
	}

	// Cross-vault copy numbers on collision without replacing destination.
	cross, err := CopyTo(a, "Meeting.md", b, "")
	if err != nil || cross != "Meeting 2.md" {
		t.Fatalf("CopyTo collision = %q, %v", cross, err)
	}
	if text, _ := Read(b, "Meeting.md"); text != "# Existing\n" {
		t.Fatalf("existing destination overwritten: %q", text)
	}
	if text, _ := Read(b, "Meeting 2.md"); text != "# Meeting\n" {
		t.Fatalf("cross-vault copy text = %q", text)
	}

	// Copying a folder preserves subfolders, documents, assets and empty dirs,
	// while skipping hidden files, .git and node_modules.
	folder, err := CopyTo(a, "Templates", b, "")
	if err != nil || folder != "Templates" {
		t.Fatalf("CopyTo folder = %q, %v", folder, err)
	}
	if text, err := Read(b, "Templates/Weekly.md"); err != nil || text != "# Weekly\n" {
		t.Fatalf("copied folder doc = %q, %v", text, err)
	}
	if data, err := os.ReadFile(filepath.Join(b, "Templates", "assets", "logo.png")); err != nil || string(data) != "pngdata" {
		t.Fatalf("copied folder asset = %q, %v", data, err)
	}
	if st, err := os.Stat(filepath.Join(b, "Templates", "Empty")); err != nil || !st.IsDir() {
		t.Fatalf("empty subfolder missing: %v", err)
	}
	for _, skipped := range []string{"Templates/.hidden.md", "Templates/.git", "Templates/node_modules"} {
		if _, err := os.Stat(filepath.Join(b, filepath.FromSlash(skipped))); !os.IsNotExist(err) {
			t.Errorf("expected %s to be skipped, stat err = %v", skipped, err)
		}
	}

	// Copying a folder into itself or a descendant must fail.
	if _, err := Copy(a, "Templates", "Templates"); err == nil {
		t.Error("copied a folder into itself")
	}
	if _, err := Copy(a, "Templates", "Templates/Empty"); err == nil {
		t.Error("copied a folder into its descendant")
	}

	// Invalid paths and missing destinations must fail.
	if _, err := Copy(a, "Missing.md", ""); err == nil {
		t.Error("copied missing file")
	}
	if _, err := CopyTo(a, "Meeting.md", b, "../escape"); err == nil {
		t.Error("copied outside vault")
	}
	if _, err := CopyTo(a, "Meeting.md", b, "NoSuchDir"); err == nil {
		t.Error("copied into non-existent directory")
	}
	if _, err := CopyTo(a, "Meeting.md", b, "Meeting.md"); err == nil {
		t.Error("copied into a file instead of a directory")
	}
}

func TestImportFile(t *testing.T) {
	srcDir, root := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	mdPath := filepath.Join(srcDir, "Notes.md")
	if err := os.WriteFile(mdPath, []byte("# Notes\n\nhello"), 0o644); err != nil {
		t.Fatal(err)
	}
	pdfPath := filepath.Join(srcDir, "Paper.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(srcDir, "Folder")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}

	// Importing .md and .pdf copies them into the vault root and preserves contents.
	gotMD, err := ImportFile(mdPath, root, "")
	if err != nil || gotMD != "Notes.md" {
		t.Fatalf("ImportFile md = %q, %v", gotMD, err)
	}
	if text, err := Read(root, "Notes.md"); err != nil || text != "# Notes\n\nhello" {
		t.Fatalf("imported md = %q, %v", text, err)
	}
	gotPDF, err := ImportFile(pdfPath, root, "")
	if err != nil || gotPDF != "Paper.pdf" {
		t.Fatalf("ImportFile pdf = %q, %v", gotPDF, err)
	}
	if raw, err := os.ReadFile(filepath.Join(root, "Paper.pdf")); err != nil || string(raw) != "%PDF-1.4\n" {
		t.Fatalf("imported pdf = %q, %v", raw, err)
	}

	// Collision numbering never overwrites existing files.
	gotMD2, err := ImportFile(mdPath, root, "")
	if err != nil || gotMD2 != "Notes 2.md" {
		t.Fatalf("ImportFile collision = %q, %v", gotMD2, err)
	}

	// Subfolder import places the file inside the folder.
	gotSub, err := ImportFile(pdfPath, root, "Sub")
	if err != nil || gotSub != "Sub/Paper.pdf" {
		t.Fatalf("ImportFile sub = %q, %v", gotSub, err)
	}

	// Directories, missing sources, and invalid destination folders must fail.
	if _, err := ImportFile(folder, root, ""); err == nil {
		t.Error("expected directory import to fail")
	}
	if _, err := ImportFile(filepath.Join(srcDir, "missing.md"), root, ""); err == nil {
		t.Error("expected missing source import to fail")
	}
	if _, err := ImportFile(mdPath, root, "../escape"); err == nil {
		t.Error("expected escape dir import to fail")
	}
	if _, err := ImportFile(mdPath, root, "NoSuchDir"); err == nil {
		t.Error("expected non-existent dir import to fail")
	}
}
