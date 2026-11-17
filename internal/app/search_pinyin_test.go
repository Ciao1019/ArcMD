package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestPalettePinyinMultiKeywordAndDiskCache(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		ConfigDir:    filepath.Join(dir, "config"),
		DocumentsDir: filepath.Join(dir, "docs"),
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root(a), "工作/本周周报.md", "# 本周周报\n\n项目发布因为接口兼容延期两天。\n"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root(a), "随手记.md", "# 随手记\n\n日常记录。\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.refreshSearch()

	cachePath := filepath.Join(cfg.ConfigDir, "search-cache.bin")
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("search cache was not persisted: %v", err)
	}

	// Pinyin title + folder multi-keyword match in command bar.
	a.openPalette()
	a.palette.query = "gz zb"
	res := a.results()
	if len(res) == 0 || res[0].title != "本周周报" {
		t.Fatalf("expected 工作/本周周报 to match 'gz zb', got %+v", res)
	}

	// Multi-keyword content search across non-adjacent words and FindText fallback.
	a.palette.query = "延期 发布"
	res = a.results()
	foundContent := false
	for _, act := range res {
		if act.kind == "内容" && act.title == "本周周报" {
			foundContent = true
			act.run()
			break
		}
	}
	if !foundContent {
		t.Fatalf("expected multi-keyword content hit for '延期 发布', got %+v", res)
	}
	if a.doc == nil || a.doc.title() != "本周周报" {
		t.Fatalf("expected 本周周报 to open, got %v", a.doc)
	}
	if q, open := a.doc.ed.FindQuery(); !open || (q != "延期" && q != "发布") {
		t.Fatalf("expected FindText fallback to match one term, got open=%v q=%q", open, q)
	}

	// Cold start in a second App instance loads persisted index on New() before refreshSearch().
	a2, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if a2.search.index == nil || a2.search.index.Len() == 0 {
		t.Fatal("expected cold-start App to seed search index from disk cache")
	}
	if hits := a2.search.index.SearchMulti("延期 发布", nil, 5); len(hits) != 1 || hits[0].Path != "工作/本周周报.md" {
		t.Fatalf("expected cached SearchMulti hit on cold start, got %+v", hits)
	}
}

func TestWikiAndMentionPinyinCompletion(t *testing.T) {
	a, tt := newTestApp(t)
	if err := vault.Write(root(a), "项目周报.md", "# 项目周报\n\n内容。\n"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root(a), "草稿.md", "# 草稿\n\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("草稿.md")
	tt.Frame()

	ed := a.doc.ed
	ed.RequestFocus = true
	tt.Frame()
	ed.SetSelection(ed.Len(), ed.Len())
	tt.Type("[[xmzb")
	if !tt.HasText("项目周报") {
		t.Fatal("expected [[xmzb to suggest 项目周报")
	}
	tt.Key(0, ui.KeyEnter)
	if !strings.Contains(ed.Text(), "[[项目周报]]") {
		t.Fatalf("expected Enter to insert [[项目周报]], got %q", ed.Text())
	}

	tt.Type(" @zb")
	if !tt.HasText("项目周报") {
		t.Fatal("expected @zb to suggest 项目周报")
	}
}
