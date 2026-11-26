package vault

import (
	"errors"
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
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(filepath.Join(blocker, "space"), "Welcome.md"); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Errorf("Read through file ancestor = %v, want non-ErrNotExist error", err)
	}
}

func TestListSkipsHiddenAndNonMarkdown(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"b.md", "A.md", "notes/z.MD", "notes/deep/y.md", "pack.mdz", ".git/c.md", ".hidden.md", "img.png", "node_modules/x.md"} {
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
	want := []string{"A.md", "b.md", "pack.mdz", "notes/z.MD", "notes/deep/y.md"}
	if !slices.Equal(paths, want) {
		t.Errorf("List = %q, want %q", paths, want)
	}
	if !IsMDZ("pack.mdz") || !IsMDZ("dir/PACK.MDZ") || IsMDZ(".mdz") || IsMDZ("note.md") {
		t.Errorf("IsMDZ classification unexpected")
	}
	if !IsDocument("pack.mdz") {
		t.Errorf("IsDocument(pack.mdz) = false, want true")
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

func TestWhiteboardDocumentsCreateListRenameMove(t *testing.T) {
	root := t.TempDir()
	first, err := CreateFile(root, "Design", "System", ".excalidraw", `{"type":"excalidraw","version":2,"elements":[]}`)
	if err != nil || first != "Design/System.excalidraw" {
		t.Fatalf("CreateFile = %q, %v", first, err)
	}
	second, err := CreateFile(root, "Design", "System", ".excalidraw", `{"type":"excalidraw","version":2,"elements":[]}`)
	if err != nil || second != "Design/System 2.excalidraw" {
		t.Fatalf("CreateFile collision = %q, %v", second, err)
	}
	if !IsWhiteboard(first) || !IsDocument(first) || IsMarkdown(first) {
		t.Fatalf("classification wrong for %q", first)
	}
	docs, err := List(root)
	if err != nil || len(docs) != 2 {
		t.Fatalf("List = %+v, %v", docs, err)
	}
	renamed, err := Rename(root, first, "Arch")
	if err != nil || renamed != "Design/Arch.excalidraw" {
		t.Fatalf("Rename = %q, %v", renamed, err)
	}
	moved, err := Move(root, renamed, "")
	if err != nil || moved != "Arch.excalidraw" {
		t.Fatalf("Move = %q, %v", moved, err)
	}
}

func TestAmphibiousWhiteboardSVGVaultAndAssetsTree(t *testing.T) {
	root := t.TempDir()
	svgPayload := `<svg xmlns="http://www.w3.org/2000/svg"></svg>`
	rel, err := CreateFile(root, "Design", "Flow", ExtWhiteboard, svgPayload)
	if err != nil || rel != "Design/Flow.excalidraw.svg" {
		t.Fatalf("CreateFile = %q, %v", rel, err)
	}
	if !IsWhiteboard(rel) || !IsWhiteboardSVG(rel) || !IsDocument(rel) || Title(rel) != "Flow" {
		t.Fatalf("classification/Title failed for %q", rel)
	}
	if err := Write(root, "assets/Inline-Board.excalidraw.svg", svgPayload); err != nil {
		t.Fatal(err)
	}
	// Tree hides assets/ from the sidebar while List still includes assets/*.excalidraw.svg for search/QA.
	tree, err := Tree(root)
	if err != nil {
		t.Fatal(err)
	}
	if Find(tree, "assets") != nil {
		t.Fatalf("expected Tree to skip assets folder, got %+v", tree)
	}
	docs, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	foundAsset := false
	for _, d := range docs {
		if d.Path == "assets/Inline-Board.excalidraw.svg" {
			foundAsset = true
		}
	}
	if !foundAsset {
		t.Fatalf("expected List to include assets whiteboard: %+v", docs)
	}

	renamed, err := Rename(root, rel, "Architecture")
	if err != nil || renamed != "Design/Architecture.excalidraw.svg" {
		t.Fatalf("Rename = %q, %v", renamed, err)
	}
	dst := t.TempDir()
	if err := Write(dst, "Architecture.excalidraw.svg", svgPayload); err != nil {
		t.Fatal(err)
	}
	dest, _, err := MoveTo(root, renamed, dst, "")
	if err != nil || dest != "Architecture 2.excalidraw.svg" {
		t.Fatalf("MoveTo = %q, %v", dest, err)
	}
}

func TestWritePreservesHardLinks(t *testing.T) {
	root, peerDir := t.TempDir(), t.TempDir()
	if err := Write(root, "Note.md", "# Original\n\nLong initial paragraph.\n"); err != nil {
		t.Fatal(err)
	}
	primary := filepath.Join(root, "Note.md")
	peer := filepath.Join(peerDir, "Mirror.md")
	if err := os.Link(primary, peer); err != nil {
		t.Skipf("hard links are unavailable: %v", err)
	}
	if err := Write(root, "Note.md", "# Short\n"); err != nil {
		t.Fatal(err)
	}
	fi1, err := os.Stat(primary)
	if err != nil {
		t.Fatal(err)
	}
	fi2, err := os.Stat(peer)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(fi1, fi2) {
		t.Fatal("vault.Write broke external hard link")
	}
	got, err := os.ReadFile(peer)
	if err != nil || string(got) != "# Short\n" {
		t.Fatalf("peer content = %q (%v), want %q", string(got), err, "# Short\n")
	}
}

func TestPDFDocuments(t *testing.T) {
	root := t.TempDir()
	if err := Write(root, "Papers/Attention.pdf", "%PDF-1.4"); err != nil {
		t.Fatal(err)
	}
	if err := Write(root, "assets/Embedded.pdf", "%PDF-1.4"); err != nil {
		t.Fatal(err)
	}
	if !IsPDF("Papers/Attention.pdf") || !IsDocument("Papers/Attention.pdf") || IsMarkdown("Papers/Attention.pdf") || IsPDF(".pdf") {
		t.Fatal("IsPDF classification mismatch")
	}
	nodes, err := Tree(root)
	if err != nil {
		t.Fatal(err)
	}
	if Find(nodes, "Papers/Attention.pdf") == nil || Find(nodes, "assets/Embedded.pdf") != nil {
		t.Fatalf("unexpected Tree visibility for PDFs: %+v", nodes)
	}
	renamed, err := Rename(root, "Papers/Attention.pdf", "Transformers")
	if err != nil || renamed != "Papers/Transformers.pdf" {
		t.Fatalf("Rename PDF = %q, %v", renamed, err)
	}
	dup, err := Copy(root, renamed, "Papers")
	if err != nil || dup != "Papers/Transformers 2.pdf" {
		t.Fatalf("Copy PDF = %q, %v", dup, err)
	}
}

func TestBookDocumentsHTMLAndEPUB(t *testing.T) {
	root := t.TempDir()
	if err := Write(root, "Books/Guide.epub", "PK\x03\x04"); err != nil {
		t.Fatal(err)
	}
	if err := Write(root, "Web/Article.html", "<h1>Hi</h1>"); err != nil {
		t.Fatal(err)
	}
	if err := Write(root, "Web/Legacy.HTM", "<p>Hi</p>"); err != nil {
		t.Fatal(err)
	}
	if err := Write(root, "assets/Embedded.epub", "PK\x03\x04"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"Books/Guide.epub", "Web/Article.html", "Web/Legacy.HTM"} {
		if !IsBook(p) || !IsDocument(p) || IsMarkdown(p) || IsPDF(p) {
			t.Fatalf("classification mismatch for %q", p)
		}
	}
	if !IsEPUB("Books/Guide.epub") || IsHTML("Books/Guide.epub") {
		t.Fatal("EPUB vs HTML mismatch")
	}
	if !IsHTML("Web/Article.html") || !IsHTML("Web/Legacy.HTM") || IsEPUB("Web/Article.html") {
		t.Fatal("HTML vs EPUB mismatch")
	}
	if IsBook(".html") || IsBook(".htm") || IsBook(".epub") {
		t.Fatal("dotfiles without stem must not be classified as books")
	}
	nodes, err := Tree(root)
	if err != nil {
		t.Fatal(err)
	}
	if Find(nodes, "Books/Guide.epub") == nil || Find(nodes, "Web/Article.html") == nil || Find(nodes, "assets/Embedded.epub") != nil {
		t.Fatalf("unexpected Tree visibility for books: %+v", nodes)
	}
	renamed, err := Rename(root, "Web/Article.html", "Report")
	if err != nil || renamed != "Web/Report.html" {
		t.Fatalf("Rename HTML = %q, %v", renamed, err)
	}
	dup, err := Copy(root, "Books/Guide.epub", "Books")
	if err != nil || dup != "Books/Guide 2.epub" {
		t.Fatalf("Copy EPUB = %q, %v", dup, err)
	}
}

func TestDatabaseDocuments(t *testing.T) {
	root := t.TempDir()
	rel, err := CreateFile(root, "Projects", "Roadmap", ExtDatabase, "# Roadmap\n")
	if err != nil || rel != "Projects/Roadmap.db.md" {
		t.Fatalf("CreateFile = %q, %v", rel, err)
	}
	if FileExt(rel) != ".db.md" {
		t.Fatalf("FileExt(%q) = %q, want .db.md", rel, FileExt(rel))
	}
	if !IsDatabase(rel) || !IsDocument(rel) || IsMarkdown(rel) || Title(rel) != "Roadmap" {
		t.Fatalf("classification/Title failed for %q", rel)
	}
	if IsDatabase(".db.md") || IsDocument(".db.md") || IsMarkdown(".db.md") {
		t.Fatal("bare .db.md should not be classified as a document")
	}
	docs, err := List(root)
	if err != nil || len(docs) != 1 || docs[0].Path != rel {
		t.Fatalf("List = %+v, %v", docs, err)
	}
	renamed, err := Rename(root, rel, "Backlog")
	if err != nil || renamed != "Projects/Backlog.db.md" {
		t.Fatalf("Rename = %q, %v", renamed, err)
	}
	if !IsDatabase(renamed) {
		t.Fatalf("renamed file lost database classification: %q", renamed)
	}
}

func TestSanitizeName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"CON", "_CON"},
		{"con", "_con"},
		{"PRN", "_PRN"},
		{"aux.txt", "_aux.txt"},
		{"NUL.tar.gz", "_NUL.tar.gz"},
		{"lpt1.md", "_lpt1.md"},
		{"COM0", "_COM0"},
		{"com9.arcmd", "_com9.arcmd"},
		{"COM\u00b9", "_COM\u00b9"},
		{"com\u00b2.md", "_com\u00b2.md"},
		{"lpt\u00b3.txt", "_lpt\u00b3.txt"},
		{"CON.", "_CON"},
		{"CON .md", "_CON .md"},
		{"Note .", "Note"},
		{"Note . .   ", "Note"},
		{"  ...  ", ""},
		{" . . ", ""},
		{"hello\x00\n\tworld", "helloworld"},
		{"Idea: one/two", "Idea- one-two"},
		{"Connect.md", "Connect.md"},
	}
	for _, tc := range cases {
		if got := SanitizeName(tc.in); got != tc.want {
			t.Errorf("SanitizeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
