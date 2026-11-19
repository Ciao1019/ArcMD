package app

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestRenameTagEverywhere(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	for name, md := range map[string]string{"A.md": "# 甲\n\n今天 #工作 很忙", "Journal/B.md": "# 乙\n\n#工作/2026 和 #职场\n\n```\n#工作 in code\n```"} {
		vault.Write(root(a), name, md)
	}
	a.refreshTree()
	a.open("A.md")
	a.refreshSearch()
	tt.Frame()
	a.openPalette()
	a.palette.query = "#工作"
	tt.Frame()
	if err := tt.Click("重命名或合并标签 #工作…"); err != nil {
		t.Fatal(err, tt.Texts())
	}
	tt.Frame()
	a.tagRename.to = "职场"
	tt.Frame()
	if !tt.HasText("已有标签 #职场（1 篇），将合并为一个。") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("重命名"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !strings.Contains(a.doc.ed.Text(), "#职场 很忙") {
		t.Fatal("open document", a.doc.ed.Text())
	}
	b, _ := vault.Read(root(a), "Journal/B.md")
	if !strings.Contains(b, "#职场/2026") || !strings.Contains(b, "#工作 in code") {
		t.Fatal("other document; code unchanged", b)
	}
	if !tt.HasText("已合并到 #职场，共 2 篇文档") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("撤销"); err != nil {
		t.Fatal(err)
	}
	b, _ = vault.Read(root(a), "Journal/B.md")
	if !strings.Contains(b, "#工作/2026") || !strings.Contains(a.doc.ed.Text(), "#工作 很忙") {
		t.Fatal("undo")
	}
}

func TestTagRenameUpdatesFooterAfterAutosaveAndReload(t *testing.T) {
	a, tt := newTestApp(t)
	doc := editor.ImportMarkdown("# 标签测试\n\n😀 #very-long-tag-name <!-- note -->。\n")
	raw, err := doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root(a), "TagReview.arcmd", raw); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.refreshSearch()
	a.open("TagReview.arcmd")
	tt.Frame()
	if !slices.Contains(a.doc.footer.tags, "very-long-tag-name") {
		t.Fatalf("expected initial footer tag, got %v", a.doc.footer.tags)
	}

	// Simulate real desktop async background saves & search refreshes.
	posted := make(chan func(), 16)
	a.search.async = true
	a.runAsync = func(fn func()) { posted <- fn }

	a.renameTag("very-long-tag-name", "short")
	tt.Frame()
	if !slices.Contains(a.doc.footer.tags, "short") || slices.Contains(a.doc.footer.tags, "very-long-tag-name") {
		t.Fatalf("footer did not update right after renameTag: %v", a.doc.footer.tags)
	}

	// Drain initial refreshSearch (which read the pre-save disk file), then let async autosave write.
	(<-posted)()
	tt.Advance(autosaveDelay + 100*time.Millisecond)
	tt.Frame()
	if !a.doc.saving {
		t.Fatal("expected async autosave to start")
	}
	// Apply async autosave completion WITHOUT yet finishing the post-save refreshSearch.
	(<-posted)()
	tt.Frame()
	if a.doc.dirty() {
		t.Fatal("expected document clean after async autosave")
	}
	if !slices.Contains(a.doc.footer.tags, "short") || slices.Contains(a.doc.footer.tags, "very-long-tag-name") {
		t.Fatalf("footer reverted to old tag after autosave before search refresh completed: %v", a.doc.footer.tags)
	}

	// Press Ctrl+R (reload from disk) and confirm footer still shows the new tag.
	tt.Key(ui.Cmd, ui.KeyR)
	tt.Frame()
	if !slices.Contains(a.doc.footer.tags, "short") || slices.Contains(a.doc.footer.tags, "very-long-tag-name") {
		t.Fatalf("footer showed stale tag after Ctrl+R reload: %v", a.doc.footer.tags)
	}
	// Drain every pending background search refresh before exiting so TempDir cleanup is clean.
	for a.search.busy || len(posted) > 0 {
		(<-posted)()
	}
	tt.Frame()
	if !slices.Contains(a.doc.footer.tags, "short") || slices.Contains(a.doc.footer.tags, "very-long-tag-name") {
		t.Fatalf("footer stale after background search refresh settled: %v", a.doc.footer.tags)
	}
}
