package database

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
)

const (
	schemaStartMarker   = "<!-- arcmd-db:schema"
	schemaEndMarker     = "-->"
	snapshotStartMarker = "<!-- arcmd-db:snapshot:start -->"
	snapshotEndMarker   = "<!-- arcmd-db:snapshot:end -->"
)

// EncodeDatabaseMarkdown serializes db and a GFM snapshot table into the
// amphibious .db.md Markdown format, assuming the database sits at the space root.
func EncodeDatabaseMarkdown(db *Database, snapshot *QueryResult) (string, error) {
	return EncodeDatabaseMarkdownRelative("", db, snapshot)
}

// RenderGFMSnapshot renders a clean GFM snapshot table for view links written
// relative to docDir. Used by full-page .db.md files and embedded Linked Views.
func RenderGFMSnapshot(docDir string, db *Database, snapshot *QueryResult) string {
	if db == nil {
		return ""
	}
	return renderGFMSnapshot(docDir, db, snapshot)
}

// EncodeDatabaseMarkdownRelative serializes db and a GFM snapshot table into
// the amphibious .db.md Markdown format, writing relative record links from dbDir.
func EncodeDatabaseMarkdownRelative(dbDir string, db *Database, snapshot *QueryResult) (string, error) {
	if err := db.Validate(); err != nil {
		return "", err
	}

	var out strings.Builder

	// Keep YAML frontmatter minimal so external tools (GitHub, Obsidian) stay uncluttered.
	if db.Icon != "" || db.Cover != "" {
		fm := &FrontmatterDoc{}
		if db.Icon != "" {
			fm.Set("icon", FMValue{Kind: FMScalar, Scalar: db.Icon})
		}
		if db.Cover != "" {
			fm.Set("cover", FMValue{Kind: FMScalar, Scalar: db.Cover})
			if db.CoverY != 0 {
				fm.Set("coverY", FMValue{Kind: FMNumber, Number: db.CoverY})
			}
		}
		out.WriteString("---\n")
		out.WriteString(fm.Format())
		out.WriteString("\n---\n\n")
	}

	out.WriteString("# ")
	out.WriteString(db.Title)
	out.WriteByte('\n')

	if desc := strings.TrimSpace(db.Description); desc != "" {
		out.WriteByte('\n')
		out.WriteString(desc)
		out.WriteByte('\n')
	}

	rawJSON, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return "", fmt.Errorf("database: encode schema: %w", err)
	}
	// Escape HTML comment sequences inside JSON string literals so the outer comment cannot close prematurely.
	safeJSON := strings.ReplaceAll(string(rawJSON), "-->", `--\u003e`)
	safeJSON = strings.ReplaceAll(safeJSON, "<!--", `\u003c!--`)

	out.WriteByte('\n')
	out.WriteString(schemaStartMarker)
	out.WriteByte('\n')
	out.WriteString(safeJSON)
	out.WriteByte('\n')
	out.WriteString(schemaEndMarker)
	out.WriteByte('\n')

	out.WriteByte('\n')
	out.WriteString(snapshotStartMarker)
	out.WriteByte('\n')
	out.WriteString(renderGFMSnapshot(dbDir, db, snapshot))
	out.WriteString(snapshotEndMarker)
	out.WriteByte('\n')

	return out.String(), nil
}

// DecodeDatabaseMarkdown parses an amphibious .db.md document into a validated Database.
func DecodeDatabaseMarkdown(text string) (*Database, error) {
	fm, body, _, _ := SplitFrontmatter(text)

	startIdx := strings.Index(body, schemaStartMarker)
	if startIdx < 0 {
		return nil, errors.New("database: missing arcmd-db:schema block")
	}
	payloadStart := startIdx + len(schemaStartMarker)
	endRel := strings.Index(body[payloadStart:], schemaEndMarker)
	if endRel < 0 {
		return nil, errors.New("database: unterminated arcmd-db:schema block")
	}
	jsonText := strings.TrimSpace(body[payloadStart : payloadStart+endRel])
	if jsonText == "" {
		return nil, errors.New("database: empty arcmd-db:schema block")
	}

	var db Database
	dec := json.NewDecoder(bytes.NewBufferString(jsonText))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&db); err != nil {
		return nil, fmt.Errorf("database: decode schema: %w", err)
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, errors.New("database: trailing data in schema block")
	}

	if db.Title == "" {
		if h1 := extractFirstH1(body); h1 != "" {
			db.Title = h1
		}
	}
	if db.Icon == "" {
		if v, ok := fm.Get("icon"); ok && v.Scalar != "" {
			db.Icon = v.Scalar
		}
	}
	if db.Cover == "" {
		if v, ok := fm.Get("cover"); ok && v.Scalar != "" {
			db.Cover = v.Scalar
		}
		if db.Cover != "" {
			if v, ok := fm.Get("coverY"); ok {
				if y, err := strconv.ParseFloat(v.Scalar, 64); err == nil && y >= -1 && y <= 1 {
					db.CoverY = y
				}
			}
		}
	}

	if err := db.Validate(); err != nil {
		return nil, err
	}
	return &db, nil
}

func renderGFMSnapshot(dbDir string, db *Database, snapshot *QueryResult) string {
	cols := snapshotColumns(db, snapshot)
	rows := snapshotRows(db, snapshot)

	var b strings.Builder
	b.WriteByte('|')
	for _, col := range cols {
		b.WriteByte(' ')
		b.WriteString(escapeGFMCell(col.Name))
		b.WriteString(" |")
	}
	b.WriteByte('\n')

	b.WriteByte('|')
	for range cols {
		b.WriteString(" --- |")
	}
	b.WriteByte('\n')

	for _, rec := range rows {
		if rec == nil {
			continue
		}
		b.WriteByte('|')
		for _, col := range cols {
			b.WriteByte(' ')
			if col.Type == PropTitle {
				title := rec.Title
				if title == "" {
					title = "Untitled"
				}
				cell := escapeGFMCell(title)
				if rec.Path != "" {
					rel := relSlashPath(dbDir, rec.Path)
					cell = fmt.Sprintf("[%s](%s)", cell, encodeMarkdownLinkPath(rel))
				}
				b.WriteString(cell)
			} else {
				val := rec.ValueFor(col)
				b.WriteString(escapeGFMCell(FormatValue(col, val)))
			}
			b.WriteString(" |")
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func snapshotColumns(db *Database, snapshot *QueryResult) []Property {
	var view *ViewConfig
	if snapshot != nil && snapshot.ViewID != "" {
		view, _ = db.ViewByID(snapshot.ViewID)
	}
	if view == nil && db.ActiveView != "" {
		view, _ = db.ViewByID(db.ActiveView)
	}
	if view == nil && len(db.Views) > 0 {
		view = &db.Views[0]
	}

	if view == nil || len(view.Properties) == 0 {
		return slices.Clone(db.Properties)
	}

	var cols []Property
	seen := make(map[string]bool, len(view.Properties))
	for _, vp := range view.Properties {
		if !vp.Visible {
			continue
		}
		if p, idx := db.PropertyByID(vp.PropertyID); idx >= 0 && !seen[p.ID] {
			seen[p.ID] = true
			cols = append(cols, *p)
		}
	}
	if tp := db.TitleProperty(); tp != nil && !seen[tp.ID] {
		cols = append([]Property{*tp}, cols...)
	}
	if len(cols) == 0 {
		return slices.Clone(db.Properties)
	}
	return cols
}

func snapshotRows(db *Database, snapshot *QueryResult) []*Record {
	if snapshot == nil {
		return db.InlineRows
	}
	if len(snapshot.Rows) > 0 {
		return snapshot.Rows
	}
	if len(snapshot.Groups) == 0 {
		return nil
	}
	var rows []*Record
	seen := make(map[string]bool)
	var walk func([]RecordGroup)
	walk = func(groups []RecordGroup) {
		for _, g := range groups {
			for _, r := range g.Rows {
				if r != nil && !seen[r.ID] {
					seen[r.ID] = true
					rows = append(rows, r)
				}
			}
			if len(g.SubGroups) > 0 {
				walk(g.SubGroups)
			}
		}
	}
	walk(snapshot.Groups)
	return rows
}

func escapeGFMCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "\n", "<br>")
	return s
}

func relSlashPath(fromDir, targetPath string) string {
	fromDir = path.Clean(strings.TrimPrefix(strings.ReplaceAll(fromDir, `\`, "/"), "/"))
	targetPath = path.Clean(strings.TrimPrefix(strings.ReplaceAll(targetPath, `\`, "/"), "/"))
	if fromDir == "" || fromDir == "." {
		return targetPath
	}
	fromParts := strings.Split(fromDir, "/")
	toParts := strings.Split(targetPath, "/")
	common := 0
	for common < len(fromParts) && common < len(toParts) && fromParts[common] == toParts[common] {
		common++
	}
	var rel []string
	for i := common; i < len(fromParts); i++ {
		rel = append(rel, "..")
	}
	rel = append(rel, toParts[common:]...)
	if len(rel) == 0 {
		return "."
	}
	return strings.Join(rel, "/")
}

func encodeMarkdownLinkPath(rel string) string {
	parts := strings.Split(rel, "/")
	for i, seg := range parts {
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		parts[i] = url.PathEscape(seg)
	}
	return strings.Join(parts, "/")
}
