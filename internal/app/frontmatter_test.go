package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

const issue83SampleMarkdown = `---
name: pg-to-iceberg-ddl
description: 把 PostgreSQL（含达梦、瀚高等 PG 方言库）的 CREATE TABLE 建表语句转换成 Iceberg on Hive 建表语句。只输出 CREATE TABLE 括号内的字段定义，不输出 NOT NULL、DEFAULT。
agent_created: true
flowix_property_types:
  agent_created: Boolean
---

# PG / 达梦建表语句 -> Iceberg on Hive 建表语句

把 PostgreSQL 的建表语句转换成 Iceberg on Hive 可用的建表语句。

## 一、输出契约

1. 只输出括号内的字段定义。
2. 字段名用反引号包裹。
`

func TestAppFrontmatterDisplayEditAutosaveAndDatabaseExclusion(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	if err := vault.Write(sp.Root, "pg-to-iceberg-ddl.md", issue83SampleMarkdown); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("pg-to-iceberg-ddl.md")
	tt.Frame()

	if !tt.HasText("元数据") || !tt.HasText("4 项") {
		t.Fatalf("expected frontmatter card rendered in app view, texts=%v", tt.Texts())
	}

	// Open edit mode, update metadata, click 完成, and verify disk autosave.
	a.doc.ed.EditFrontmatter()
	tt.Frame()
	if err := tt.Click("元数据编辑器"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("name: pg-to-iceberg-ddl\nowner: data-team")
	tt.Frame()
	if err := tt.Click("完成"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	saveSettle(tt)

	disk, err := vault.Read(sp.Root, "pg-to-iceberg-ddl.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(disk, "owner: data-team") || !strings.Contains(disk, "# PG / 达梦建表语句") {
		t.Fatalf("updated frontmatter not persisted cleanly:\n%s", disk)
	}

	// Database-backed page should keep PagePropertiesView and hide the YAML frontmatter card.
	_, _ = seedAppDatabase(t, a, "Tasks")
	if err := vault.Write(sp.Root, "Tasks/item.md", "---\nstatus: 进行中\n---\n\n# 任务项\n\n详情。\n"); err != nil {
		t.Fatal(err)
	}
	a.pageDBCache = nil
	a.refreshTree()
	a.open("Tasks/item.md")
	tt.Frame()
	if !tt.HasText("页面属性") {
		t.Fatalf("expected database PagePropertiesView on backing record, texts=%v", tt.Texts())
	}
	if tt.HasText("复制元数据") {
		t.Fatalf("database backing record should not duplicate YAML frontmatter card, texts=%v", tt.Texts())
	}
}

func TestFrontmatterSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render frontmatter")
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	if err := vault.Write(sp.Root, "pg-to-iceberg-ddl.md", issue83SampleMarkdown); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("pg-to-iceberg-ddl.md")
	tt.SetSize(1200, 760)
	tt.Frame()
	tt.Frame()
	savePNG(t, filepath.Join(dir, "frontmatter-light.png"), tt)

	tt.SetDark(true)
	tt.Frame()
	tt.Frame()
	savePNG(t, filepath.Join(dir, "frontmatter-dark.png"), tt)

	tt.SetDark(false)
	a.doc.ed.EditFrontmatter()
	tt.Frame()
	tt.Frame()
	savePNG(t, filepath.Join(dir, "frontmatter-edit.png"), tt)
}
