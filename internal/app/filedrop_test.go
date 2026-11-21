package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func writeExternalFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDropImportMarkdownAndPDFOnSidebar(t *testing.T) {
	a, tt := newTestApp(t)
	a.trashFile = os.RemoveAll
	sp := a.state.ActiveSpace()
	settle(tt)

	srcDir := t.TempDir()
	mdPath := writeExternalFile(t, srcDir, "Roadmap.md", []byte("# Roadmap\n\nShip v1\n"))
	pdfPath := writeExternalFile(t, srcDir, "Manual.pdf", buildRichSamplePDF(t))

	// Find the Welcome row in the sidebar tree; document rows defer drops to the tree.
	row, ok := tt.Find("Welcome")
	if !ok {
		t.Fatalf("sidebar row not found: %q", tt.Texts())
	}
	x, y := row.X+row.W/2, row.Y+row.H/2
	if !tt.DropFiles(x, y, mdPath, pdfPath) {
		t.Fatal("sidebar tree did not accept dropped files")
	}
	tt.Frame()

	// Both files were copied into the space root and the first one opened.
	gotMD, err := vault.Read(sp.Root, "Roadmap.md")
	if err != nil || gotMD != "# Roadmap\n\nShip v1\n" {
		t.Fatalf("imported markdown = %q, err = %v", gotMD, err)
	}
	if _, err := os.Stat(filepath.Join(sp.Root, "Manual.pdf")); err != nil {
		t.Fatalf("imported pdf missing: %v", err)
	}
	if a.doc == nil || a.doc.rel != "Roadmap.md" {
		t.Fatalf("expected Roadmap.md to open, got %+v", a.doc)
	}
	if !tt.HasText("Roadmap") || !tt.HasText("Manual") {
		t.Fatalf("imported files not visible in sidebar: %q", tt.Texts())
	}
	if !tt.HasText("撤销") {
		t.Fatalf("expected undo toast after import: %q", tt.Texts())
	}

	// Row pulse and document landing animations were armed and clean up over time.
	if len(a.fileDrop.pulse) != 2 {
		t.Fatalf("expected 2 import pulse entries, got %d", len(a.fileDrop.pulse))
	}
	if a.fileDrop.landAt.IsZero() {
		t.Fatal("expected document landing animation to be armed")
	}
	tt.Advance(100 * time.Millisecond)
	tt.Frame()
	tt.Advance(fileDropPulseDur + 50*time.Millisecond)
	tt.Frame()
	if len(a.fileDrop.pulse) != 0 {
		t.Fatalf("expected import pulse map to drain, got %+v", a.fileDrop.pulse)
	}
	if !a.fileDrop.landAt.IsZero() {
		t.Fatal("expected landing animation timestamp to clear after duration")
	}

	// Let the first import's undo toast expire so the second import's Undo toast
	// is the active action button.
	tt.Advance(undoTimeout + 100*time.Millisecond)
	tt.Frame()

	// Dropping the same Markdown file again creates a numbered copy without overwriting.
	if !tt.DropFiles(x, y, mdPath) {
		t.Fatal("second sidebar drop was not accepted")
	}
	tt.Frame()
	if _, err := os.Stat(filepath.Join(sp.Root, "Roadmap 2.md")); err != nil {
		t.Fatalf("expected numbered collision copy Roadmap 2.md: %v", err)
	}
	if a.doc == nil || a.doc.rel != "Roadmap 2.md" {
		t.Fatalf("expected Roadmap 2.md to open, got %+v", a.doc)
	}

	// Clicking Undo moves the second import to trash.
	if err := tt.Click("撤销"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if _, err := os.Stat(filepath.Join(sp.Root, "Roadmap 2.md")); !os.IsNotExist(err) {
		t.Fatalf("expected Roadmap 2.md to be trashed on undo, err=%v", err)
	}
}

func TestDropImportIntoCollapsedFolder(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	if err := os.MkdirAll(filepath.Join(sp.Root, "Inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	sp.SetExpanded("Inbox", false)
	a.refreshTree()
	settle(tt)

	folder, ok := tt.Find("Inbox")
	if !ok {
		t.Fatalf("folder Inbox not found: %q", tt.Texts())
	}
	srcDir := t.TempDir()
	pdfPath := writeExternalFile(t, srcDir, "Paper.pdf", buildRichSamplePDF(t))

	if !tt.DropFiles(folder.X+folder.W/2, folder.Y+folder.H/2, pdfPath) {
		t.Fatal("folder row did not accept dropped pdf")
	}
	tt.Frame()

	if _, err := os.Stat(filepath.Join(sp.Root, "Inbox", "Paper.pdf")); err != nil {
		t.Fatalf("expected Paper.pdf inside Inbox: %v", err)
	}
	if !sp.IsExpanded("Inbox") {
		t.Fatal("expected Inbox folder to expand after drop")
	}
	if a.doc == nil || a.doc.rel != "Inbox/Paper.pdf" || !a.doc.isPDF() {
		t.Fatalf("expected Inbox/Paper.pdf to open as PDF, got %+v", a.doc)
	}
}

func TestDropImportOnWindowEmptyState(t *testing.T) {
	a, tt := newTestApp(t)
	a.trashFile = os.RemoveAll
	sp := a.state.ActiveSpace()
	// Remove Welcome.md so the window shows emptyState.
	a.trash("Welcome.md")
	settle(tt)

	if a.doc != nil {
		t.Fatalf("expected empty state with no open doc, got %+v", a.doc)
	}
	srcDir := t.TempDir()
	mdPath := writeExternalFile(t, srcDir, "Imported.md", []byte("# Imported\n\nBody\n"))

	// Drop onto the main content area (right half of window).
	if !tt.DropFiles(600, 320, mdPath) {
		t.Fatal("window empty state did not accept dropped md")
	}
	tt.Frame()

	if a.doc == nil || a.doc.rel != "Imported.md" {
		t.Fatalf("expected Imported.md to open from empty state drop, got %+v", a.doc)
	}
	if text, err := vault.Read(sp.Root, "Imported.md"); err != nil || !strings.Contains(text, "Body") {
		t.Fatalf("imported content = %q, err = %v", text, err)
	}
}

func TestDropImportRejectsNonMDAndNonPDF(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	settle(tt)

	row, ok := tt.Find("Welcome")
	if !ok {
		t.Fatal("Welcome row missing")
	}
	x, y := row.X+row.W/2, row.Y+row.H/2

	srcDir := t.TempDir()
	txtFile := writeExternalFile(t, srcDir, "notes.txt", []byte("plain text"))
	pngFilePath := writeExternalFile(t, srcDir, "shot.png", []byte("png"))
	zipFile := writeExternalFile(t, srcDir, "bundle.zip", []byte("PK"))
	arcmdFile := writeExternalFile(t, srcDir, "native.arcmd", []byte("{}"))
	markdownFile := writeExternalFile(t, srcDir, "alt.markdown", []byte("# alt"))
	subFolder := filepath.Join(srcDir, "docs.md")
	if err := os.Mkdir(subFolder, 0o755); err != nil {
		t.Fatal(err)
	}

	// Unsupported-only drop imports nothing and notifies the user.
	if !tt.DropFiles(x, y, txtFile, pngFilePath, zipFile, arcmdFile, markdownFile, subFolder) {
		t.Fatal("sidebar did not receive drop event")
	}
	tt.Frame()

	for _, name := range []string{"notes.txt", "shot.png", "bundle.zip", "native.arcmd", "alt.markdown", "docs.md"} {
		if _, err := os.Stat(filepath.Join(sp.Root, name)); !os.IsNotExist(err) {
			t.Errorf("unsupported entry %s should not be imported", name)
		}
	}
	if !tt.HasText("仅支持拖入 Markdown (.md) 和 PDF (.pdf) 文档") {
		t.Fatalf("expected unsupported notice, got %q", tt.Texts())
	}

	// Mixed drop imports only .md and .pdf while skipping .txt.
	mdPath := writeExternalFile(t, srcDir, "Valid.md", []byte("# Valid\n"))
	pdfPath := writeExternalFile(t, srcDir, "Valid.pdf", buildRichSamplePDF(t))
	if !tt.DropFiles(x, y, txtFile, mdPath, pdfPath) {
		t.Fatal("mixed drop failed")
	}
	tt.Frame()

	if _, err := os.Stat(filepath.Join(sp.Root, "Valid.md")); err != nil {
		t.Fatalf("Valid.md should be imported: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sp.Root, "Valid.pdf")); err != nil {
		t.Fatalf("Valid.pdf should be imported: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sp.Root, "notes.txt")); !os.IsNotExist(err) {
		t.Fatal("notes.txt should not be imported")
	}
	if !tt.HasText("notes.txt 未导入") {
		t.Fatalf("expected skipped notice naming notes.txt, got %q", tt.Texts())
	}
}

func TestDropMarkdownOntoOpenEditorImportsDocument(t *testing.T) {
	a, tt := dropApp(t)
	sp := a.state.ActiveSpace()
	srcDir := t.TempDir()
	mdPath := writeExternalFile(t, srcDir, "Chapter.md", []byte("# Chapter 1\n\nHello\n"))

	dropAt(t, a, tt, "beta", mdPath)
	if _, err := os.Stat(filepath.Join(sp.Root, "Chapter.md")); err != nil {
		t.Fatalf("expected Chapter.md to be imported into space: %v", err)
	}
	if a.doc == nil || a.doc.rel != "Chapter.md" {
		t.Fatalf("expected Chapter.md to open, got %+v", a.doc)
	}
	if !slices.Contains(blockList(a), "Hello") {
		t.Fatalf("expected imported markdown blocks, got %q", blockList(a))
	}
}

func TestFileDropOverlayAnimations(t *testing.T) {
	a, _ := newTestApp(t)
	holdOver := false
	kind := fileDropWindow
	targetName := "工作"

	tt := ui.NewTester(func(c *ui.Context) {
		a.now, a.reduceMotion = c.Now(), c.Preferences().ReduceMotion
		a.look = lookOf(space.ThemeOf(a.state.ActiveSpace()), c.Theme().Dark)
		a.fileDrop.over = false
		a.fileDrop.targetKind = fileDropNone
		a.fileDrop.targetName = ""
		if holdOver {
			a.setFileDropOver(kind, "", targetName)
		}
		ui.Box(c).Fill()
		a.fileDropOverlayView(c)
	}, 900, 620)
	tt.Frame()

	// Idle state shows no overlay text.
	if tt.HasText("松手导入到") {
		t.Fatal("overlay should be hidden at rest")
	}

	// Hovering over the window shows the centered card and advances fade-in.
	holdOver = true
	tt.Frame()
	tt.Advance(100 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("松手导入到「工作」") || !tt.HasText("仅支持 Markdown (.md) 和 PDF (.pdf)") {
		t.Fatalf("expected window drop card texts, got %q", tt.Texts())
	}

	// Hovering over a folder shows the compact sidebar pill.
	kind = fileDropFolder
	targetName = "Inbox"
	tt.Frame()
	tt.Advance(90 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("导入到「Inbox」") || !tt.HasText("仅支持 .md / .pdf") {
		t.Fatalf("expected sidebar pill texts, got %q", tt.Texts())
	}

	// Leaving fades out cleanly and resets overlay state after duration.
	holdOver = false
	tt.Frame()
	tt.Advance(fileDropFadeDur + 60*time.Millisecond)
	tt.Frame()
	if a.fileDrop.lastKind != fileDropNone || tt.HasText("导入到「Inbox」") {
		t.Fatalf("expected overlay to hide after fade-out, lastKind=%v texts=%q", a.fileDrop.lastKind, tt.Texts())
	}
}

func TestSnapshotFileDrop(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the file drop overlay")
	for _, tc := range []struct {
		name       string
		dark       bool
		kind       fileDropKind
		targetName string
	}{
		{name: "filedrop-window-light.png", dark: false, kind: fileDropWindow, targetName: "工作"},
		{name: "filedrop-window-dark.png", dark: true, kind: fileDropWindow, targetName: "工作"},
		{name: "filedrop-sidebar-light.png", dark: false, kind: fileDropFolder, targetName: "Inbox"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newTestApp(t)
			sp := a.state.ActiveSpace()
			if err := os.MkdirAll(filepath.Join(sp.Root, "Inbox"), 0o755); err != nil {
				t.Fatal(err)
			}
			a.refreshTree()
			tt := ui.NewTester(func(c *ui.Context) {
				a.now, a.reduceMotion = c.Now(), c.Preferences().ReduceMotion
				base := c.Theme()
				a.docTheme = base
				a.look = lookOf(space.ThemeOf(sp), base.Dark)
				a.paperDark = a.look.Dark
				th := spaceTheme(base, a.look)
				c.SetTheme(th)
				a.fileDrop.over = false
				a.setFileDropOver(tc.kind, "", tc.targetName)
				look := a.look
				frame := ui.Row(c).Fill().AlignItems(ui.Stretch)
				frame.Draw(func(p *ui.Painter, r ui.Rect) { look.paint(p, r, 0, false) })
				frame.Children(func() {
					ui.Column(c.Key("dock")).Width(sidebarWidth).Shrink(0).Clip().Children(func() {
						a.sidebar(c, false)
					})
					a.main(c, sidebarWidth)
				})
				a.fileDropOverlayView(c)
			}, 1100, 720)
			if tc.dark {
				tt.SetDark(true)
			}
			tt.Frame()
			tt.Advance(220 * time.Millisecond)
			tt.Frame()
			savePNG(t, filepath.Join(dir, tc.name), tt)
		})
	}
}
