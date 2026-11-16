package spacefs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// paragraphs renders a file as one part per paragraph, IDs b0, b1…, which
// is enough to test the tools without the document model.
func paragraphs(name, content string) (*Page, error) {
	if strings.Contains(content, "BROKEN") {
		return nil, errors.New("cannot decode")
	}
	var parts []Part
	for i, p := range strings.Split(strings.TrimSpace(content), "\n\n") {
		parts = append(parts, Part{Block: fmt.Sprintf("b%d", i), Text: p})
	}
	return PageOf(name, parts), nil
}

func TestExplicitCollectionRestrictsAllReads(t *testing.T) {
	f := space(t, map[string]string{"chosen.md": "# Allowed", "sibling.md": "# Private"})
	files := map[string]string{"chosen.md": filepath.Join(f.Root, "chosen.md")}
	f.SetFiles(files)
	files["sibling.md"] = filepath.Join(f.Root, "sibling.md")
	docs, err := f.Docs()
	if err != nil || len(docs) != 1 || docs[0].Path != "chosen.md" {
		t.Fatal(docs, err)
	}
	if _, err := f.Page("sibling.md"); err == nil {
		t.Fatal("read an unopened sibling")
	}
	if p, err := f.Page("chosen.md"); err != nil || p.Title != "Allowed" {
		t.Fatal(p, err)
	}
	f.SetFiles(map[string]string{})
	if docs, err := f.Docs(); err != nil || len(docs) != 0 {
		t.Fatal(docs, err)
	}
	if _, err := f.Page("chosen.md"); err == nil {
		t.Fatal("read a removed file from cache")
	}
}

func space(t *testing.T, files map[string]string) *FS {
	t.Helper()
	root := t.TempDir()
	for rel, text := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return New(root, paragraphs)
}

func TestPageOfMapsLinesToBlocks(t *testing.T) {
	p := PageOf("plan.md", []Part{{"a", "# 发布计划"}, {"b", "第一行\n第二行"}, {"c", "结尾"}})
	if p.Title != "发布计划" {
		t.Fatalf("title %q", p.Title)
	}
	want := []string{"# 发布计划", "", "第一行", "第二行", "", "结尾"}
	if strings.Join(p.Lines, "|") != strings.Join(want, "|") {
		t.Fatalf("lines %q", p.Lines)
	}
	for line, block := range map[int]string{1: "a", 2: "b", 3: "b", 4: "b", 5: "c", 6: "c"} {
		if s, ok := p.Locate(line); !ok || s.Block != block {
			t.Errorf("line %d in %q, want %q", line, s.Block, block)
		}
	}
	if _, ok := p.Locate(7); ok {
		t.Error("a line past the end has a block")
	}
}

func TestGlob(t *testing.T) {
	fs := space(t, map[string]string{
		"工作/周报 1.md":   "# 周报一",
		"工作/计划.md":     "# 计划",
		"生活/旅行周报.md":   "x",
		"readme.md":    "y",
		".arcmd/h.md":  "hidden",
		"notes.txt":    "not a document",
		"工作/深/周报 9.md": "z",
	})
	out, err := fs.Glob("*周报*", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "3 documents.") || !strings.Contains(out, "工作/周报 1.md\t周报一") || !strings.Contains(out, "生活/旅行周报.md") {
		t.Fatalf("glob by name:\n%s", out)
	}
	out, _ = fs.Glob("工作/**", false, 0)
	if !strings.HasPrefix(out, "3 documents.") || strings.Contains(out, "生活") {
		t.Fatalf("glob by folder:\n%s", out)
	}
	out, _ = fs.Glob("工作", false, 0)
	if !strings.HasPrefix(out, "3 documents.") {
		t.Fatalf("folder scope:\n%s", out)
	}
	out, _ = fs.Glob("", false, 0)
	if !strings.HasPrefix(out, "5 documents.") || strings.Contains(out, ".arcmd") || strings.Contains(out, "notes.txt") {
		t.Fatalf("every document:\n%s", out)
	}
	out, _ = fs.Glob("*.pdf", false, 0)
	if !strings.HasPrefix(out, "No documents match") {
		t.Fatalf("no match: %s", out)
	}
}

func TestGrepContextPagingAndFailures(t *testing.T) {
	var long []string
	for i := range 70 {
		long = append(long, fmt.Sprintf("第 %d 项：发布阻塞", i))
	}
	fs := space(t, map[string]string{
		"a.md":      "# 评审\n\n决定延期发布\n\n原因是支付联调",
		"b.md":      strings.Join(long, "\n\n"),
		"broken.md": "BROKEN",
		"其他/c.md":   "Release 延期",
	})
	out, err := fs.Grep(GrepArgs{Pattern: "延期", Context: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2 matches in 2 documents.", "a.md-2-\n", "a.md:3:决定延期发布", "a.md-4-", "其他/c.md:1:Release 延期", "Could not read 1 documents, not searched: broken.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	out, _ = fs.Grep(GrepArgs{Pattern: "阻塞", Scope: "b.md"})
	if !strings.Contains(out, "70 matches in 1 documents; showing 1-60.") || !strings.Contains(out, "offset=60") {
		t.Fatalf("first page:\n%s", out)
	}
	out, _ = fs.Grep(GrepArgs{Pattern: "阻塞", Scope: "b.md", Offset: 60})
	if !strings.Contains(out, "showing 61-70") || strings.Contains(out, "offset=") || !strings.Contains(out, "第 69 项") {
		t.Fatalf("second page:\n%s", out)
	}
	out, _ = fs.Grep(GrepArgs{Pattern: `release\s+延`, Regex: true})
	if !strings.Contains(out, "1 matches") {
		t.Fatalf("regex, case-insensitive:\n%s", out)
	}
	out, _ = fs.Grep(GrepArgs{Pattern: "无此内容"})
	if !strings.HasPrefix(out, "No matches") {
		t.Fatalf("no match:\n%s", out)
	}
	if _, err := fs.Grep(GrepArgs{Pattern: "(", Regex: true}); err == nil {
		t.Fatal("a bad regular expression is not an error")
	}
}

func TestReadPagesAndRefusesOutside(t *testing.T) {
	var paras []string
	for i := range 500 {
		paras = append(paras, fmt.Sprintf("p%d", i))
	}
	fs := space(t, map[string]string{"long.md": strings.Join(paras, "\n\n"), "short.md": "一\n\n二"})
	out, err := fs.Read("short.md", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if out != "     1→一\n     2→\n     3→二\n" {
		t.Fatalf("whole short document: %q", out)
	}
	out, _ = fs.Read("long.md", 0, 0)
	if !strings.Contains(out, "(lines 1-400 of 999; read on with start_line=401)") {
		t.Fatalf("long document:\n%s", out[len(out)-200:])
	}
	out, _ = fs.Read("long.md", 997, 0)
	if !strings.Contains(out, "   999→p499") || !strings.Contains(out, "the end") {
		t.Fatalf("tail:\n%s", out)
	}
	if _, err := fs.Read("long.md", 2000, 0); err == nil {
		t.Fatal("reading past the end is not an error")
	}
	for _, bad := range []string{"../outside.md", "/etc/passwd", ".arcmd/history/x.md", "notes.txt", "missing.md"} {
		if _, err := fs.Read(bad, 0, 0); err == nil {
			t.Errorf("%s was read", bad)
		}
	}
}

func TestReadStaysInsideAgainstDotDot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "space")
	os.MkdirAll(root, 0o755)
	os.WriteFile(filepath.Join(parent, "secret.md"), []byte("secret"), 0o644)
	fs := New(root, paragraphs)
	// Cleaning "../secret.md" from the root keeps it inside the space.
	if out, err := fs.Read("../secret.md", 0, 0); err == nil {
		t.Fatalf("read outside the space: %q", out)
	}
	if err := os.Symlink(filepath.Join(parent, "secret.md"), filepath.Join(root, "link.md")); err == nil {
		if out, err := fs.Read("link.md", 0, 0); err == nil {
			t.Fatalf("followed a link outside the space: %q", out)
		}
	}
}

func TestOutlineSkipsCode(t *testing.T) {
	fs := space(t, map[string]string{"a.md": "# 标题\n\n## 背景\n\n```sh\n# not a heading\n```\n\n### 细节"})
	out, err := fs.Outline("a.md")
	if err != nil {
		t.Fatal(err)
	}
	want := "a.md: 9 lines.\n     1 标题\n     3   背景\n     9     细节\n"
	if out != want {
		t.Fatalf("outline:\n%q\nwant\n%q", out, want)
	}
}

func TestLinks(t *testing.T) {
	fs := space(t, map[string]string{
		"计划.md":   "见 [[评审纪要]] 和 [[不存在]] #发布 #工作/2026",
		"评审纪要.md": "回到 [[计划]]",
		"无关.md":   "nothing",
	})
	out, err := fs.Links("计划.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[[评审纪要]] → 评审纪要.md", "[[不存在]] (no such document yet)", "评审纪要.md:1:回到 [[计划]]", "Tags: #发布, #工作/2026"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestLiveOverlayAndCache(t *testing.T) {
	fs := space(t, map[string]string{"a.md": "已保存"})
	out, _ := fs.Grep(GrepArgs{Pattern: "未保存"})
	if !strings.HasPrefix(out, "No matches") {
		t.Fatal(out)
	}
	fs.SetLive(map[string]*Page{"a.md": PageOf("a.md", []Part{{"x", "未保存的修改"}}), "new.md": PageOf("new.md", []Part{{"y", "未保存的新文档"}})})
	out, _ = fs.Grep(GrepArgs{Pattern: "未保存"})
	if !strings.Contains(out, "2 matches in 2 documents") {
		t.Fatalf("live pages:\n%s", out)
	}
	fs.SetLive(nil)
	os.WriteFile(filepath.Join(fs.Root, "a.md"), []byte("改过了，更长的内容"), 0o644)
	out, _ = fs.Read("a.md", 0, 0)
	if !strings.Contains(out, "改过了") {
		t.Fatalf("a changed file is read again: %s", out)
	}
}

func TestOverviewFitsBudget(t *testing.T) {
	files := map[string]string{}
	for i := range 300 {
		files[fmt.Sprintf("d/%03d.md", i)] = fmt.Sprintf("# 文档 %d\n\n## 第一节\n\n## 第二节", i)
	}
	fs := space(t, files)
	out, err := fs.Overview(4000)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 4600 || !strings.HasPrefix(out, "300 documents.") || !strings.Contains(out, "more documents not listed") || !strings.Contains(out, "    第一节") {
		t.Fatalf("overview (%d bytes):\n%s", len(out), out[:600])
	}
}

func TestSearchRanksPassagesDescribeAndFormattedGrep(t *testing.T) {
	fs := space(t, map[string]string{
		"项目/发布计划.md": "# 发布计划\n\n## 决定\n\n因为**安全**与<mark data-bg=\"amber\">证书</mark>还没下来，发布延期到 12 月 2 日。\n\n## 风险\n\n支付联调超时",
		"项目/周报.md":   "# 周报\n\n本周完成了登录页改版。",
		"归档/旧记录.md":  "# 旧记录\n\n去年提到过证书续期。",
	})

	// Grep matches across inline formatting (**...** and <mark>) and multi-term fallback.
	out, err := fs.Grep(GrepArgs{Pattern: "安全与证书"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "项目/发布计划.md:5:") {
		t.Fatalf("grep across inline markup missed:\n%s", out)
	}
	out, err = fs.Grep(GrepArgs{Pattern: "证书 延期"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "项目/发布计划.md:5:") {
		t.Fatalf("multi-term grep fallback missed:\n%s", out)
	}

	// Ranked passage search surfaces the decision passage first with section breadcrumb.
	res, err := fs.Search(SearchArgs{Query: "为什么发布延期 证书"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "relevant passages in") || !strings.Contains(res, "[1] 项目/发布计划.md (发布计划 · § 决定, L5)") {
		t.Fatalf("search ranking:\n%s", res)
	}

	// Page.Describe extracts section heading and clean plain-text excerpt.
	p, err := fs.Page("项目/发布计划.md")
	if err != nil {
		t.Fatal(err)
	}
	loc := p.Describe(5, 5)
	if loc.Section != "决定" || !strings.Contains(loc.Excerpt, "因为安全与证书还没下来") || strings.Contains(loc.Excerpt, "<mark") {
		t.Fatalf("Describe: %+v", loc)
	}
}
