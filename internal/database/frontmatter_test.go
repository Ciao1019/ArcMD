package database

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestFrontmatterLosslessRoundTrip(t *testing.T) {
	source := "---\n" +
		"# File metadata\n" +
		"icon: 🚀\n" +
		"cover: gradient:sea\n" +
		"coverY: 0.25\n" +
		"goal: 1500\n" +
		"\n" +
		"# Nested custom config must remain untouched\n" +
		"author:\n" +
		"  name: Alice\n" +
		"  roles:\n" +
		"    - maintainer\n" +
		"summary: |\n" +
		"  First line\n" +
		"  Second line\n" +
		"status: 未开始\n" +
		"tags:\n" +
		"- UI\n" +
		"- 核心\n" +
		"---\n" +
		"\n" +
		"# My Document Title  \n" +
		"\n" +
		"Body paragraph with   trailing spaces  \n" +
		"```yaml\n" +
		"# Fake heading inside fence\n" +
		"status: ignored\n" +
		"```\n"

	updated := ApplyFrontmatter(source, func(fm *FrontmatterDoc) {
		fm.Set("status", FMValue{Kind: FMScalar, Scalar: "进行中"})
		fm.Set("priority", FMValue{Kind: FMNumber, Number: 3})
	})

	if !strings.Contains(updated, "# File metadata\n") {
		t.Fatalf("lost top comment:\n%s", updated)
	}
	if !strings.Contains(updated, "author:\n  name: Alice\n  roles:\n    - maintainer\n") {
		t.Fatalf("lost nested YAML:\n%s", updated)
	}
	if !strings.Contains(updated, "summary: |\n  First line\n  Second line\n") {
		t.Fatalf("lost block scalar:\n%s", updated)
	}
	if !strings.Contains(updated, "status: 进行中\n") || !strings.Contains(updated, "priority: 3\n") {
		t.Fatalf("missing updated keys:\n%s", updated)
	}

	_, bodyBefore, hasFM, le := SplitFrontmatter(source)
	fmAfter, bodyAfter, hasFMAfter, leAfter := SplitFrontmatter(updated)
	if !hasFM || !hasFMAfter || le != "\n" || leAfter != "\n" {
		t.Fatalf("frontmatter flags mismatch: %v %v %q %q", hasFM, hasFMAfter, le, leAfter)
	}
	if bodyBefore != bodyAfter {
		t.Fatalf("body bytes changed:\nBEFORE:\n%q\nAFTER:\n%q", bodyBefore, bodyAfter)
	}
	if v, ok := fmAfter.Get("icon"); !ok || v.Scalar != "🚀" {
		t.Fatalf("icon = %+v, %v", v, ok)
	}
	if v, ok := fmAfter.Get("tags"); !ok || !slices.Equal(v.List, []string{"UI", "核心"}) {
		t.Fatalf("0-indent tags = %+v, %v", v, ok)
	}
	if v, ok := fmAfter.Get("goal"); !ok || v.Kind != FMNumber || v.Number != 1500 {
		t.Fatalf("goal = %+v, %v", v, ok)
	}
	if !strings.Contains(updated, "\n---\n\n# My Document Title") {
		t.Fatalf("separating blank line after --- was not preserved:\n%q", updated)
	}
}

func TestFrontmatterCRLFAndNoBlankLine(t *testing.T) {
	// Existing FM with CRLF and NO blank line before body.
	src := "---\r\nstatus: todo\r\n---\r\n# Heading\r\nLine 2\r\n"
	out := ApplyFrontmatter(src, func(fm *FrontmatterDoc) {
		fm.Set("done", FMValue{Kind: FMBool, Bool: true})
	})
	want := "---\r\nstatus: todo\r\ndone: true\r\n---\r\n# Heading\r\nLine 2\r\n"
	if out != want {
		t.Fatalf("CRLF no-blank-line:\ngot:  %q\nwant: %q", out, want)
	}

	// File without FM using CRLF gets CRLF frontmatter prepended cleanly.
	plainCRLF := "# Heading\r\nBody\r\n"
	added := ApplyFrontmatter(plainCRLF, func(fm *FrontmatterDoc) {
		fm.Set("status", FMValue{Kind: FMScalar, Scalar: "active"})
	})
	wantAdded := "---\r\nstatus: active\r\n---\r\n\r\n# Heading\r\nBody\r\n"
	if added != wantAdded {
		t.Fatalf("added CRLF:\ngot:  %q\nwant: %q", added, wantAdded)
	}

	// Deleting all keys removes the frontmatter fence cleanly.
	stripped := ApplyFrontmatter(out, func(fm *FrontmatterDoc) {
		fm.Delete("status")
		fm.Delete("done")
	})
	if stripped != "# Heading\r\nLine 2\r\n" {
		t.Fatalf("stripped FM = %q", stripped)
	}
}

func TestFrontmatterArraysQuotedAndComments(t *testing.T) {
	src := "---\n" +
		"flow: [alpha, \"beta, gamma\", 'it''s fine']\n" +
		"url: https://example.com#section # trailing comment\n" +
		"quoted_bool: \"true\"\n" +
		"quoted_num: \"007\"\n" +
		"literal_colon: \"key: value\"\n" +
		"---\n" +
		"# Test\n"

	fm, _, _, _ := SplitFrontmatter(src)
	if v, ok := fm.Get("flow"); !ok || !slices.Equal(v.List, []string{"alpha", "beta, gamma", "it's fine"}) {
		t.Fatalf("flow = %+v, %v", v, ok)
	}
	if v, ok := fm.Get("url"); !ok || v.Scalar != "https://example.com#section" {
		t.Fatalf("url = %+v, %v", v, ok)
	}
	if v, ok := fm.Get("quoted_bool"); !ok || v.Kind != FMScalar || v.Scalar != "true" {
		t.Fatalf("quoted_bool = %+v, %v", v, ok)
	}
	if v, ok := fm.Get("quoted_num"); !ok || v.Scalar != "007" {
		t.Fatalf("quoted_num = %+v, %v", v, ok)
	}
	if v, ok := fm.Get("literal_colon"); !ok || v.Scalar != "key: value" {
		t.Fatalf("literal_colon = %+v, %v", v, ok)
	}

	// Unmodified call returns exact input.
	if same := ApplyFrontmatter(src, func(fm *FrontmatterDoc) {}); same != src {
		t.Fatalf("noop ApplyFrontmatter altered string")
	}
}

func TestFrontmatterRecordMappingDateRangesAndRelations(t *testing.T) {
	schema := []Property{
		{ID: "p_title", Key: "title", Name: "Name", Type: PropTitle},
		{ID: "p_status", Key: "status", Name: "Status", Type: PropStatus},
		{ID: "p_due", Key: "due", Name: "Due", Type: PropDate, Date: &DateConfig{EndKey: "due_end"}},
		{ID: "p_iso", Key: "sprint", Name: "Sprint", Type: PropDate},
		{ID: "p_proj", Key: "project", Name: "Project", Type: PropRelation, Relation: &RelationConfig{LimitOne: true}},
		{ID: "p_deps", Key: "deps", Name: "Deps", Type: PropRelation},
		{ID: "p_done", Key: "done", Name: "Done", Type: PropCheckbox},
		{ID: "p_pts", Key: "points", Name: "Points", Type: PropNumber},
		{ID: "p_note", Key: "note", Name: "Note", Type: PropText},
	}

	md := "---\n" +
		"icon: 📌\n" +
		"status: 进行中\n" +
		"due: 2026-11-30\n" +
		"due_end: 2026-12-05\n" +
		"sprint: 2026-11-01/2026-11-14\n" +
		"project: \"[[Projects/ArcMD 2.0|ArcMD]]\"\n" +
		"deps:\n" +
		"  - \"[[Task A]]\"\n" +
		"  - \"[[Task B]]\"\n" +
		"done: false\n" +
		"points: 8\n" +
		"note: \"true\"\n" +
		"tags:\n" +
		"  - Core\n" +
		"---\n" +
		"\n" +
		"<!-- # Commented Heading -->\n" +
		"```\n" +
		"# Code Heading\n" +
		"```\n" +
		"# Real Task Title\n" +
		"\n" +
		"First paragraph with #ui/kanban tag.\n"

	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	rec := ExtractRecordFromMarkdown(schema, "Tasks/real.md", md, now, now)

	if rec.Title != "Real Task Title" {
		t.Fatalf("Title = %q, want Real Task Title", rec.Title)
	}
	if rec.Icon != "📌" {
		t.Fatalf("Icon = %q", rec.Icon)
	}
	if !slices.Equal(rec.Tags, []string{"Core", "ui/kanban"}) {
		t.Fatalf("Tags = %q", rec.Tags)
	}
	if !strings.Contains(rec.Excerpt, "First paragraph") {
		t.Fatalf("Excerpt = %q", rec.Excerpt)
	}
	if got := rec.Props["p_due"]; got.Kind != ValDate || got.Date.Start != "2026-11-30" || got.Date.End != "2026-12-05" {
		t.Fatalf("p_due = %+v", got)
	}
	if got := rec.Props["p_iso"]; got.Kind != ValDate || got.Date.Start != "2026-11-01" || got.Date.End != "2026-11-14" {
		t.Fatalf("p_iso = %+v", got)
	}
	if got := rec.Props["p_proj"]; !slices.Equal(got.List, []string{"Projects/ArcMD 2.0"}) {
		t.Fatalf("p_proj = %+v", got)
	}
	if got := rec.Props["p_deps"]; !slices.Equal(got.List, []string{"Task A", "Task B"}) {
		t.Fatalf("p_deps = %+v", got)
	}
	if got := rec.Props["p_done"]; got.Kind != ValBool || got.Bool {
		t.Fatalf("p_done = %+v", got)
	}
	if got := rec.Props["p_pts"]; got.Kind != ValNumber || got.Number != 8 {
		t.Fatalf("p_pts = %+v", got)
	}
	if got := rec.Props["p_note"]; got.Kind != ValText || got.Text != "true" {
		t.Fatalf("p_note = %+v", got)
	}

	// Write single date (clear End) -> deletes due_end cleanly, preserves body.
	rec.Props["p_due"] = DateRangeValue("2026-12-01", "", false)
	rec.Props["p_done"] = BoolValue(true)
	delete(rec.Props, "p_pts")

	out := ApplyFrontmatter(md, func(fm *FrontmatterDoc) {
		WritePropsToFrontmatter(schema, fm, rec.Props)
	})
	if strings.Contains(out, "due_end:") {
		t.Fatalf("due_end should have been deleted:\n%s", out)
	}
	if strings.Contains(out, "points:") {
		t.Fatalf("points should have been deleted:\n%s", out)
	}
	if !strings.Contains(out, "done: true\n") {
		t.Fatalf("done should be true:\n%s", out)
	}
	if !strings.Contains(out, "note: \"true\"\n") {
		t.Fatalf("note should stay quoted \"true\":\n%s", out)
	}

	rec2 := ExtractRecordFromMarkdown(schema, "Tasks/real.md", out, now, now)
	if !rec2.Props["p_due"].Equal(DateRangeValue("2026-12-01", "", false)) {
		t.Fatalf("round-trip p_due = %+v", rec2.Props["p_due"])
	}
	if !rec2.Props["p_done"].Equal(BoolValue(true)) {
		t.Fatalf("round-trip p_done = %+v", rec2.Props["p_done"])
	}
}
