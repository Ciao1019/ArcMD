package database

import (
	"strings"
	"testing"
	"time"
)

func TestAmphibiousRoundTripAndEscaping(t *testing.T) {
	db := NewDefault("产品路线图", SourceConfig{Type: SourceFolder, Dir: "产品路线图"})
	db.Description = "包含 --> 注释闭合符与 <!-- 注释起始符的说明"
	db.Icon = "🗺️"
	db.Cover = "gradient:dawn"
	db.CoverY = -0.2
	db.Properties = append(db.Properties,
		Property{ID: "p_done", Key: "done", Name: "完成", Type: PropCheckbox},
		Property{ID: "p_score", Key: "score", Name: "评分", Type: PropNumber, Number: &NumberConfig{Format: "percent"}},
		Property{
			ID:      "p_formula",
			Key:     "summary",
			Name:    "摘要",
			Type:    PropFormula,
			Formula: &FormulaConfig{Expression: `"a --> b"`},
		},
	)
	db.Views[0].Properties = append(db.Views[0].Properties,
		ViewProperty{PropertyID: "p_done", Visible: true},
		ViewProperty{PropertyID: "p_score", Visible: true},
	)

	now := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	snap := &QueryResult{
		ViewID: db.ActiveView,
		Rows: []*Record{
			{
				ID:    "Projects/Task 1.md",
				Path:  "Projects/Task 1.md",
				Title: "Pipe | & Line\nBreak",
				Props: map[string]Value{
					"prop_status": TextValue("进行中"),
					"prop_tags":   ListValue("UI", "核心"),
					"prop_date":   DateRangeValue("2026-11-30", "2026-12-05", false),
					"p_done":      BoolValue(true),
					"p_score":     NumberValue(0.85),
				},
				CreatedAt: now,
				UpdatedAt: now,
			},
			{
				ID:    "Other/Sibling Note.md",
				Path:  "Other/Sibling Note.md",
				Title: "Plain Row",
				Props: map[string]Value{
					"prop_status": TextValue("未开始"),
					"p_done":      BoolValue(false),
				},
			},
		},
	}

	encoded, err := EncodeDatabaseMarkdownRelative("Projects", db, snap)
	if err != nil {
		t.Fatalf("EncodeDatabaseMarkdownRelative: %v", err)
	}

	// Ensure schema comment never broke prematurely on literal "-->" inside description/formula.
	if strings.Contains(encoded, `"a --> b"`) {
		t.Fatalf("literal --> was not escaped inside schema block:\n%s", encoded)
	}
	if !strings.Contains(encoded, "# 产品路线图\n") {
		t.Fatalf("missing heading:\n%s", encoded)
	}

	// Check GFM snapshot table formatting & relative URL-encoded links.
	if !strings.Contains(encoded, `[Pipe \| & Line<br>Break](Task%201.md)`) {
		t.Fatalf("missing escaped relative link for Task 1.md:\n%s", encoded)
	}
	if !strings.Contains(encoded, `[Plain Row](../Other/Sibling%20Note.md)`) {
		t.Fatalf("missing ../ relative link for Sibling Note.md:\n%s", encoded)
	}
	if !strings.Contains(encoded, "| [x] | 85% |") {
		t.Fatalf("missing formatted checkbox/percent cells:\n%s", encoded)
	}
	if !strings.Contains(encoded, "2026-11-30 → 2026-12-05") {
		t.Fatalf("missing formatted date range:\n%s", encoded)
	}

	decoded, err := DecodeDatabaseMarkdown(encoded)
	if err != nil {
		t.Fatalf("DecodeDatabaseMarkdown: %v", err)
	}
	if decoded.Digest() != db.Digest() {
		t.Fatalf("decoded database digest mismatch:\ngot:  %+v\nwant: %+v", decoded, db)
	}
	if decoded.Description != db.Description {
		t.Fatalf("description = %q, want %q", decoded.Description, db.Description)
	}
}

func TestAmphibiousDecodeRejectsCorruptAndFutureVersions(t *testing.T) {
	db := NewDefault("Valid", SourceConfig{Type: SourceInline})
	encoded, err := EncodeDatabaseMarkdown(db, nil)
	if err != nil {
		t.Fatal(err)
	}

	future := strings.Replace(encoded, `"version": 1`, `"version": 99`, 1)
	if _, err := DecodeDatabaseMarkdown(future); err == nil {
		t.Fatal("expected error for future schema version")
	}

	unknownField := strings.Replace(encoded, `"version": 1`, `"version": 1, "unknownField": true`, 1)
	if _, err := DecodeDatabaseMarkdown(unknownField); err == nil {
		t.Fatal("expected error for unknown JSON field")
	}

	if _, err := DecodeDatabaseMarkdown("# Just Markdown\n\nNo schema here.\n"); err == nil {
		t.Fatal("expected error when schema block is missing")
	}

	unterminated := "<!-- arcmd-db:schema\n{\"version\":1}"
	if _, err := DecodeDatabaseMarkdown(unterminated); err == nil {
		t.Fatal("expected error when schema block is unterminated")
	}

	bad := NewDefault("Bad", SourceConfig{Type: SourceFolder, Dir: "../outside"})
	if err := bad.Validate(); err == nil {
		t.Fatal("expected unsafe Source.Dir to fail validation")
	}
}
