package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestNoDefaultTitleStartsBlankParagraphAndFollowsLaterHeading(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()

	// Default behavior (NoDefaultTitle == false) preserves H1 + empty body paragraph.
	tt.Key(ui.Cmd, ui.KeyN)
	tt.Frame()
	if got := len(a.doc.ed.Doc.Blocks); got != 2 || a.doc.ed.Doc.Blocks[0].Kind != "heading" || a.doc.ed.Doc.Blocks[0].Level != 1 {
		t.Fatalf("default newDoc blocks = %v; want H1 + paragraph", kindsOf(a))
	}

	// Enable NoDefaultTitle: new documents start as a single empty paragraph at (0, 0).
	a.prefs.NoDefaultTitle = true
	a.savePrefs()
	tt.Key(ui.Cmd, ui.KeyN)
	tt.Frame()

	if a.doc == nil || !strings.HasPrefix(a.doc.rel, "未命名") {
		t.Fatalf("unexpected new document: %+v", a.doc)
	}
	blocks := a.doc.ed.Doc.Blocks
	if len(blocks) != 1 || blocks[0].Kind != "paragraph" || blocks[0].Text() != "" {
		t.Fatalf("NoDefaultTitle blocks = %v (len=%d, text=%q); want single empty paragraph", kindsOf(a), len(blocks), blocks[0].Text())
	}
	if a.doc.ed.Anchor != 0 || a.doc.ed.Caret != 0 || !tt.Focused("文档") {
		t.Fatalf("caret/focus: anchor=%d caret=%d focused=%v", a.doc.ed.Anchor, a.doc.ed.Caret, tt.Focused("文档"))
	}
	if a.doc.dirty() || !a.doc.ed.Blank() {
		t.Fatalf("expected clean blank doc: dirty=%v blank=%v", a.doc.dirty(), a.doc.ed.Blank())
	}

	// Plain paragraph prose keeps the untitled file name without inventing an H1.
	untitledRel := a.doc.rel
	tt.Type("先记一段随想")
	saveSettle(tt)
	if a.doc.rel != untitledRel {
		t.Fatalf("paragraph-only doc renamed to %q; want %q", a.doc.rel, untitledRel)
	}

	// Adding an H2 heading later names the untitled file while keeping the heading H2.
	tt.Key(0, ui.KeyEnter)
	tt.Type("## 美剧")
	saveSettle(tt)
	if a.doc.rel != "美剧.md" {
		t.Fatalf("file = %q; want 美剧.md", a.doc.rel)
	}
	if b := a.doc.ed.Doc.Blocks[1]; b.Kind != "heading" || b.Level != 2 || b.Text() != "美剧" {
		t.Fatalf("heading block = %+v (text=%q); want H2 美剧", b, b.Text())
	}
	disk, err := vault.Read(root(a), "美剧.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(disk, "## 美剧") || strings.Contains(disk, "\n# 美剧") || strings.HasPrefix(disk, "# 美剧") {
		t.Fatalf("saved markdown promoted H2 to H1: %q", disk)
	}
}

func TestFirstBlockH2AutoNamesUntitledDocumentAndStaysH2(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()

	// Default newDoc starts with empty H1 at Blocks[0]; typing "## " converts Blocks[0] to H2.
	tt.Key(ui.Cmd, ui.KeyN)
	tt.Frame()
	tt.Type("## 美剧")
	saveSettle(tt)

	if a.doc.rel != "美剧.md" {
		t.Fatalf("file = %q; want 美剧.md", a.doc.rel)
	}
	if _, err := os.Stat(filepath.Join(root(a), "未命名.md")); !os.IsNotExist(err) {
		t.Fatalf("未命名.md still exists: %v", err)
	}
	if b := a.doc.ed.Doc.Blocks[0]; b.Kind != "heading" || b.Level != 2 || b.Text() != "美剧" {
		t.Fatalf("Blocks[0] = kind=%q level=%d text=%q; want heading level 2 美剧", b.Kind, b.Level, b.Text())
	}

	// Updating the H2 text continues following the title until manually renamed.
	tt.Type("推荐")
	saveSettle(tt)
	if a.doc.rel != "美剧推荐.md" {
		t.Fatalf("file after editing H2 = %q; want 美剧推荐.md", a.doc.rel)
	}
	disk, err := vault.Read(root(a), "美剧推荐.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(disk, "## 美剧推荐") {
		t.Fatalf("disk markdown = %q; want leading '## 美剧推荐'", disk)
	}
}

func TestDocTitlePrefersH1AndFallsBackToAnyHeading(t *testing.T) {
	cases := []struct {
		name string
		md   string
		want string
	}{
		{name: "empty", md: "", want: ""},
		{name: "paragraph-only", md: "Just a paragraph.\n\nAnother paragraph.", want: ""},
		{name: "leading-h2", md: "## 美剧\n\n绝命毒师", want: "美剧"},
		{name: "paragraph-then-h3", md: "引子\n\n### 第三章 进展", want: "第三章 进展"},
		{name: "h1-beats-earlier-h2", md: "## 副标题\n\n# 主标题", want: "主标题"},
		{name: "empty-h1-falls-back-to-h2", md: "# \n\n## 备选标题", want: "备选标题"},
		{name: "long-h2-trimmed", md: "## " + strings.Repeat("字", 70) + " . ", want: strings.Repeat("字", 60)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var doc *model.Document
			if tc.name == "empty-h1-falls-back-to-h2" {
				doc = model.New()
				h1 := model.NewBlock("heading", "")
				h1.Level = 1
				h2 := model.NewBlock("heading", "备选标题")
				h2.Level = 2
				doc.Blocks = []*model.Block{h1, h2}
			} else {
				doc = editor.ImportMarkdown(tc.md)
			}
			if got := docTitle(doc); got != tc.want {
				t.Fatalf("docTitle(%q) = %q; want %q", tc.name, got, tc.want)
			}
		})
	}
	if got := docTitle(nil); got != "" {
		t.Fatalf("docTitle(nil) = %q; want empty", got)
	}
}
