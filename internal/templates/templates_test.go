package templates

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/document"
)

var day = time.Date(2026, 11, 12, 9, 30, 0, 0, time.Local)

func TestListFindsTemplatesByName(t *testing.T) {
	root := t.TempDir()
	if got, err := List(root); err != nil || len(got) != 0 {
		t.Fatal("a space without templates has none", got, err)
	}
	dir := filepath.Join(root, filepath.FromSlash(Dir))
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	for _, name := range []string{"周报.arcmd", "Meeting.arcmd", "notes.md", ".hidden.arcmd"} {
		os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o644)
	}
	got, err := List(root)
	if err != nil || len(got) != 2 || got[0].Name != "Meeting" || got[1].Path != ".arcmd/templates/周报.arcmd" {
		t.Fatal(got, err)
	}
	if _, ok := Find(got, "meeting"); !ok {
		t.Fatal("find ignores case")
	}
}

func TestInstantiateFillsPlaceholdersWithFreshIDs(t *testing.T) {
	tmpl := document.New()
	head := document.NewBlock("heading", "{{title}}")
	head.Level = 1
	table := document.Table(1, 1)
	table.Children[0].Children[0].Runs = []document.Run{{Text: "{{date}} {{weekday}} {{time}}", Style: document.Style{Bold: true}}}
	tmpl.Blocks = []*document.Block{head, table}
	tmpl.ImportSource = "x"
	doc := Instantiate(tmpl, day, "周会")
	if doc.Blocks[0].Text() != "周会" || doc.Blocks[0].ID == head.ID {
		t.Fatal("title and fresh IDs", doc.Blocks[0].Text())
	}
	cell := doc.Blocks[1].Children[0].Children[0]
	if cell.Text() != "2026-11-12 星期四 09:30" || !cell.Runs[0].Style.Bold || cell.ID == table.Children[0].Children[0].ID {
		t.Fatal("cells are filled and keep their marks", cell.Text())
	}
	if doc.ImportSource != "" || doc.Validate() != nil {
		t.Fatal("a valid new document")
	}
	if head.Text() != "{{title}}" {
		t.Fatal("the template itself is unchanged")
	}
}

func TestDailyNote(t *testing.T) {
	if DailyPath(day) != "日记/2026-11-12.arcmd" || DailyTitle(day) != "2026年11月12日 星期四" {
		t.Fatal(DailyPath(day), DailyTitle(day))
	}
	doc := Daily(nil, day)
	if doc.Blocks[0].Text() != "2026年11月12日 星期四" || doc.Validate() != nil {
		t.Fatal("plain daily note")
	}
	tmpl := document.New()
	head := document.NewBlock("heading", "")
	head.Level = 1
	tmpl.Blocks = []*document.Block{head, document.NewBlock("task", "回顾 {{date}}")}
	doc = Daily(tmpl, day)
	if doc.Blocks[0].Text() != DailyTitle(day) || doc.Blocks[1].Text() != "回顾 2026-11-12" {
		t.Fatal("from the daily template", doc.Blocks[0].Text(), doc.Blocks[1].Text())
	}
}
