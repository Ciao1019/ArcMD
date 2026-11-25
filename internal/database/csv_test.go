package database_test

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

func TestCSVExportAndSchemaInferenceImport(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	csvInput := strings.Join([]string{
		"\xEF\xBB\xBF名称,状态,预算,完成,截止日期,标签,链接",
		"\"Alpha 研发\",进行中,\"$1,250.50\",Yes,2026-11-30,\"核心, 前端\",https://example.com/a",
		"\"Beta, 压测\",已完成,$800,No,\"December 1, 2026 → December 5, 2026\",测试,https://example.com/b",
		"\"Gamma 发布\",未开始,¥500,是,2026-12-10,发布,https://example.com/c",
	}, "\n")

	dbRel, res, err := database.ImportCSVAsNewDatabase(root, "", "项目预算表", strings.NewReader(csvInput))
	if err != nil {
		t.Fatalf("ImportCSVAsNewDatabase: %v", err)
	}
	if dbRel != "项目预算表.db.md" {
		t.Fatalf("unexpected dbRel %q", dbRel)
	}
	if res.CreatedRecords != 3 {
		t.Fatalf("expected 3 created records, got %+v", res)
	}

	store, err := database.Open(root, dbRel, nil)
	if err != nil {
		t.Fatalf("Open imported db: %v", err)
	}
	db := store.Database()
	wantTypes := map[string]database.PropertyType{
		"名称":   database.PropTitle,
		"状态":   database.PropStatus,
		"预算":   database.PropNumber,
		"完成":   database.PropCheckbox,
		"截止日期": database.PropDate,
		"标签":   database.PropMultiSelect,
		"链接":   database.PropURL,
	}
	for name, wantType := range wantTypes {
		var found *database.Property
		for i := range db.Properties {
			if db.Properties[i].Name == name {
				found = &db.Properties[i]
				break
			}
		}
		if found == nil || found.Type != wantType {
			t.Fatalf("property %q inferred as %+v, want %q", name, found, wantType)
		}
	}

	// Verify Formula evaluation flows through ExportViewCSV with BOM and sort order.
	if err := store.AddProperty(database.Property{
		ID:      "p_double",
		Key:     "double_budget",
		Name:    "双倍预算",
		Type:    database.PropFormula,
		Formula: &database.FormulaConfig{Expression: `prop("预算") * 2`},
	}); err != nil {
		t.Fatal(err)
	}
	csvBytes, err := store.ExportViewCSV("", database.QueryOptions{Now: now}, database.CSVExportOptions{IncludeBOM: true})
	if err != nil {
		t.Fatalf("ExportViewCSV: %v", err)
	}
	if !bytes.HasPrefix(csvBytes, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("expected UTF-8 BOM prefix on exported CSV")
	}
	parsedRows, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(csvBytes, []byte{0xEF, 0xBB, 0xBF}))).ReadAll()
	if err != nil {
		t.Fatalf("parse exported CSV: %v", err)
	}
	if len(parsedRows) != 4 {
		t.Fatalf("expected header + 3 rows, got %d", len(parsedRows))
	}
	joined := string(csvBytes)
	for _, want := range []string{"Beta, 压测", "2026-12-01 → 2026-12-05", "2501"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in exported CSV:\n%s", want, joined)
		}
	}

	// Test appending additional CSV rows into an existing store (including new column inference).
	addRes, err := database.ImportCSVIntoStore(store, strings.NewReader("名称,负责人\n\"Delta 收尾\",alice@example.com\n"))
	if err != nil {
		t.Fatalf("ImportCSVIntoStore: %v", err)
	}
	if addRes.CreatedRecords != 1 || addRes.AddedProperties != 1 {
		t.Fatalf("unexpected ImportCSVIntoStore result: %+v", addRes)
	}
	if len(store.Records()) != 4 {
		t.Fatalf("expected 4 total records after append, got %d", len(store.Records()))
	}
}

func TestNotionZIPImportAndZipSlipProtection(t *testing.T) {
	spaceRoot := t.TempDir()
	zipDir := t.TempDir()

	// Build a Notion-style export archive (nested ZIP inside outer ZIP) with UUID suffixes,
	// both summary CSV and _all CSV, per-row Markdown body + local image asset, and standalone note.
	innerBuf := &bytes.Buffer{}
	innerZip := zip.NewWriter(innerBuf)
	files := map[string]string{
		"Tasks 0123456789abcdef0123456789abcdef.csv": "Name,Status,Related\nOldSummary,Todo,\n",
		"Tasks 0123456789abcdef0123456789abcdef_all.csv": strings.Join([]string{
			"Name,Status,Related,Due Date",
			"Ship V2,In Progress,\"Parent Task (https://www.notion.so/abcdef0123456789abcdef0123456789)\",January 15, 2027",
		}, "\n"),
		"Tasks 0123456789abcdef0123456789abcdef/Ship V2 11111111222222223333333344444444.md": strings.Join([]string{
			"# Ship V2",
			"",
			"Status: In Progress",
			"Related: Parent Task (https://www.notion.so/abcdef0123456789abcdef0123456789)",
			"Due Date: January 15, 2027",
			"",
			"Detailed launch checklist:",
			"",
			"![Arch Diagram](Ship%20V2%2011111111222222223333333344444444/arch.png)",
		}, "\n"),
		"Tasks 0123456789abcdef0123456789abcdef/Ship V2 11111111222222223333333344444444/arch.png": "PNGDATA",
		"Meeting Notes 99999999888888887777777766666666.md":                                        "# Meeting Notes\n\nDiscussed roadmap.\n",
	}
	for name, content := range files {
		w, err := innerZip.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := innerZip.Close(); err != nil {
		t.Fatal(err)
	}

	outerZipPath := filepath.Join(zipDir, "notion-export.zip")
	outerFile, err := os.Create(outerZipPath)
	if err != nil {
		t.Fatal(err)
	}
	outerZip := zip.NewWriter(outerFile)
	w, err := outerZip.Create("Export-0000.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(innerBuf.Bytes()); err != nil {
		t.Fatal(err)
	}
	_ = outerZip.Close()
	_ = outerFile.Close()

	res, err := database.ImportNotionZIP(spaceRoot, "", outerZipPath)
	if err != nil {
		t.Fatalf("ImportNotionZIP: %v", err)
	}
	if len(res.Databases) != 1 || res.Databases[0] != "Tasks.db.md" {
		t.Fatalf("expected [Tasks.db.md] from _all.csv, got %+v", res)
	}
	if res.Records != 1 || res.Assets < 1 || len(res.StandaloneNotes) != 1 {
		t.Fatalf("unexpected notion import counts: %+v", res)
	}

	store, err := database.Open(spaceRoot, "Tasks.db.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	recs := store.Records()
	if len(recs) != 1 || recs[0].Title != "Ship V2" {
		t.Fatalf("expected Ship V2 record, got %+v", recs)
	}
	mdRaw, err := vault.Read(spaceRoot, recs[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(mdRaw, "Status: In Progress\nRelated:") {
		t.Fatalf("expected Notion duplicate page metadata stripped from body:\n%s", mdRaw)
	}
	if !strings.Contains(mdRaw, "![Arch Diagram](assets/arch.png)") {
		t.Fatalf("expected local asset link rewritten to assets/arch.png:\n%s", mdRaw)
	}
	if !strings.Contains(mdRaw, "related: Parent Task") {
		t.Fatalf("expected stripped Notion relation title in frontmatter:\n%s", mdRaw)
	}
	if _, err := os.Stat(filepath.Join(spaceRoot, "Tasks", "assets", "arch.png")); err != nil {
		t.Fatalf("expected copied asset at Tasks/assets/arch.png: %v", err)
	}

	// Verify zip-slip rejection.
	evilPath := filepath.Join(zipDir, "evil.zip")
	ef, _ := os.Create(evilPath)
	ez := zip.NewWriter(ef)
	ew, _ := ez.Create("../../evil.md")
	_, _ = ew.Write([]byte("pwn"))
	_ = ez.Close()
	_ = ef.Close()
	if _, err := database.ImportNotionZIP(spaceRoot, "", evilPath); err == nil {
		t.Fatal("expected zip-slip archive to be rejected")
	}

	// Verify symlink entry rejection.
	symZipPath := filepath.Join(zipDir, "symlink.zip")
	sf, err := os.Create(symZipPath)
	if err != nil {
		t.Fatal(err)
	}
	sz := zip.NewWriter(sf)
	hdr := &zip.FileHeader{
		Name:   "secret-link.md",
		Method: zip.Store,
	}
	hdr.SetMode(os.ModeSymlink | 0o777)
	sw, err := sz.CreateHeader(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sw.Write([]byte("/etc/hosts")); err != nil {
		t.Fatal(err)
	}
	if err := sz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := sf.Close(); err != nil {
		t.Fatal(err)
	}
	symSpace := t.TempDir()
	if _, err := database.ImportNotionZIP(symSpace, "", symZipPath); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink zip entry to be rejected, got err=%v", err)
	}
}
