package editor

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFrontmatterYAMLHighlightURLNotKey(t *testing.T) {
	pg := pageOf(Page{}, ui.LightTheme())
	src := "name: demo\nlinks:\n  - https://example.com/a:b\n"
	spans := codeSpansFor("fm-url", "yaml", src, pg, nil)

	var got strings.Builder
	hasKey := func(sub string, want ui.Color) bool {
		for _, sp := range spans {
			if strings.Contains(sp.Text, sub) && sp.Color == want {
				return true
			}
		}
		return false
	}
	for _, sp := range spans {
		got.WriteString(sp.Text)
	}
	if got.String() != src {
		t.Fatalf("spans altered text: got %q want %q", got.String(), src)
	}
	if !hasKey("name", pg.Tokens.Type) {
		t.Fatalf("expected 'name' highlighted as key: %+v", spans)
	}
	if hasKey("https", pg.Tokens.Type) {
		t.Fatalf("URL scheme 'https' was misclassified as YAML key: %+v", spans)
	}
}

func TestFrontmatterCardDisplayCollapseCopyAndLayout(t *testing.T) {
	md := `---
name: pg-to-iceberg-ddl
description: 把 PostgreSQL（含达梦、瀚高等 PG 方言库）的 CREATE TABLE 建表语句转换成 Iceberg on Hive 建表语句。只输出 CREATE TABLE 括号内的字段定义，不输出 NOT NULL、DEFAULT。
agent_created: true
flowix_property_types:
  agent_created: Boolean
---

# PG / 达梦建表语句 -> Iceberg on Hive 建表语句

正文内容。
`
	e, tt := newTester(md)
	if e.header.fmRect.H <= 0 || e.header.fmLayout == nil {
		t.Fatalf("expected visible expanded frontmatter card, rect=%+v layout=%v", e.header.fmRect, e.header.fmLayout)
	}
	if e.header.fmCollapsed {
		t.Fatal("frontmatter should be expanded by default")
	}
	if e.header.titleTop < e.header.fmRect.Y+e.header.fmRect.H {
		t.Fatalf("titleTop=%v overlaps fmRect=%+v", e.header.titleTop, e.header.fmRect)
	}
	lw, _ := e.header.fmLayout.Size()
	if lw > e.header.fmRect.W {
		t.Fatalf("wrapped frontmatter layout width %v exceeds card width %v", lw, e.header.fmRect.W)
	}
	if !tt.HasText("元数据") || !tt.HasText("4 项") {
		t.Fatalf("expected frontmatter header and key summary, texts=%v", tt.Texts())
	}

	expandedTitleTop := e.header.titleTop
	if err := tt.Click("元数据"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !e.header.fmCollapsed || e.header.fmRect.H != e.header.fmBarH {
		t.Fatalf("expected collapsed card height=%v, got collapsed=%v rect=%+v", e.header.fmBarH, e.header.fmCollapsed, e.header.fmRect)
	}
	if e.header.titleTop >= expandedTitleTop {
		t.Fatalf("collapsing frontmatter did not shift titleTop up: collapsed=%v expanded=%v", e.header.titleTop, expandedTitleTop)
	}

	if err := tt.Click("元数据"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if e.header.fmCollapsed || e.header.titleTop != expandedTitleTop {
		t.Fatalf("expanding frontmatter did not restore layout: collapsed=%v titleTop=%v want=%v", e.header.fmCollapsed, e.header.titleTop, expandedTitleTop)
	}

	if err := tt.Click("复制元数据"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	wantClip := "---\n" + e.Doc.Frontmatter + "\n---"
	if tt.Clipboard() != wantClip {
		t.Fatalf("clipboard = %q, want %q", tt.Clipboard(), wantClip)
	}
}

func TestFrontmatterInlineEditCommitCancelStripRemoveAndUndo(t *testing.T) {
	md := "---\nname: alpha\ntags:\n  - go\n---\n\n# Title\n\nBody"
	e, tt := newTester(md)

	if err := tt.Click("编辑元数据"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !e.header.fmEditing {
		t.Fatal("expected fmEditing after clicking 编辑元数据")
	}
	if !tt.HasText("完成") || !tt.HasText("取消") || !tt.HasText("移除") {
		t.Fatalf("missing edit action buttons, texts=%v", tt.Texts())
	}

	e.header.fmDraft = "---\nname: beta\nowner: team\n---\n"
	tt.Frame()
	if err := tt.Click("完成"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if e.header.fmEditing {
		t.Fatal("expected fmEditing closed after 完成")
	}
	if e.Doc.Frontmatter != "name: beta\nowner: team" {
		t.Fatalf("Frontmatter = %q, want fences stripped", e.Doc.Frontmatter)
	}
	if exported := ExportMarkdown(e.Doc); !strings.Contains(exported, "---\nname: beta\nowner: team\n---") {
		t.Fatalf("exported markdown missing updated frontmatter:\n%s", exported)
	}

	e.Undo()
	tt.Frame()
	if e.Doc.Frontmatter != "name: alpha\ntags:\n  - go" {
		t.Fatalf("after Undo Frontmatter = %q", e.Doc.Frontmatter)
	}
	e.Redo()
	tt.Frame()
	if e.Doc.Frontmatter != "name: beta\nowner: team" {
		t.Fatalf("after Redo Frontmatter = %q", e.Doc.Frontmatter)
	}

	// Clicking body hotzone enters edit mode, Cmd+Enter commits.
	if err := tt.Click("编辑元数据内容"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !e.header.fmEditing {
		t.Fatal("clicking body hotzone did not start editing")
	}
	e.header.fmDraft = "name: gamma"
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyEnter)
	tt.Frame()
	if e.header.fmEditing || e.Doc.Frontmatter != "name: gamma" {
		t.Fatalf("Cmd+Enter did not commit: editing=%v fm=%q", e.header.fmEditing, e.Doc.Frontmatter)
	}

	// Escape cancels without saving changes.
	e.EditFrontmatter()
	tt.Frame()
	e.header.fmDraft = "name: discarded"
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if e.header.fmEditing || e.Doc.Frontmatter != "name: gamma" {
		t.Fatalf("Escape did not cancel: editing=%v fm=%q", e.header.fmEditing, e.Doc.Frontmatter)
	}

	// Clicking writing surface auto-commits active draft.
	e.EditFrontmatter()
	tt.Frame()
	e.header.fmDraft = "name: delta"
	tt.Frame()
	tt.ClickAt(e.padding+10, e.header.titleTop+10)
	tt.Frame()
	if e.header.fmEditing || e.Doc.Frontmatter != "name: delta" {
		t.Fatalf("clicking body did not auto-commit: editing=%v fm=%q", e.header.fmEditing, e.Doc.Frontmatter)
	}

	// Remove clears frontmatter and hides the card.
	e.EditFrontmatter()
	tt.Frame()
	if err := tt.Click("移除"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if e.Doc.Frontmatter != "" || e.header.fmRect.H != 0 {
		t.Fatalf("remove left fm=%q rect=%+v", e.Doc.Frontmatter, e.header.fmRect)
	}
}

func TestFrontmatterReadOnlyAndHidden(t *testing.T) {
	e := New("---\nstatus: draft\n---\n\n# Locked\n\nBody")
	e.SetReadOnly(true)
	hide := false
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), HideFrontmatter: hide})
	}, 900, 650)
	tt.Frame()

	if e.header.fmRect.H <= 0 || !tt.HasText("元数据") || !tt.HasText("复制元数据") {
		t.Fatalf("read-only mode should still display and allow copying frontmatter, texts=%v", tt.Texts())
	}
	if tt.HasText("编辑元数据") {
		t.Fatal("read-only mode should hide edit button")
	}
	e.EditFrontmatter()
	tt.Frame()
	if e.header.fmEditing {
		t.Fatal("EditFrontmatter should be ignored in read-only mode")
	}
	if err := tt.Click("元数据"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !e.header.fmCollapsed {
		t.Fatal("collapse toggle should work in read-only mode")
	}
	if err := tt.Click("复制元数据"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.Clipboard() != "---\nstatus: draft\n---" {
		t.Fatalf("read-only copy got %q", tt.Clipboard())
	}

	hide = true
	tt.Frame()
	if e.header.fmRect.H != 0 || tt.HasText("元数据") {
		t.Fatalf("HideFrontmatter=true did not hide card: rect=%+v texts=%v", e.header.fmRect, tt.Texts())
	}
}

func TestFrontmatterAddButtonWithCoverAndIcon(t *testing.T) {
	e, tt := newTester("# Title\n\nBody")
	e.Engine.SetCover("gradient:dawn")
	e.Engine.SetPageIcon("🚀")
	tt.Frame()

	tt.Move(e.padding+120, e.header.coverRect.H+20)
	tt.Frame()
	btn, ok := tt.Find("添加元数据")
	if !ok {
		t.Fatalf("expected 添加元数据 button on top hover, texts=%v", tt.Texts())
	}
	if btn.X < e.header.iconRect.X+e.header.iconRect.W {
		t.Fatalf("添加元数据 button %+v overlaps icon %+v", btn, e.header.iconRect)
	}
	if err := tt.Click("添加元数据"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !e.header.fmEditing || e.header.fmRect.H <= 0 {
		t.Fatalf("clicking 添加元数据 did not enter edit mode: editing=%v rect=%+v", e.header.fmEditing, e.header.fmRect)
	}
}
