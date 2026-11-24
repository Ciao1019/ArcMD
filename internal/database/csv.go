package database

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Ciao1019/ArcMD/internal/vault"
)

// CSVExportOptions controls CSV serialization of a database view.
type CSVExportOptions struct {
	IncludeBOM    bool
	IncludeHidden bool
}

// CSVImportResult summarizes records and inferred columns added during CSV import.
type CSVImportResult struct {
	CreatedRecords  int
	AddedProperties int
	Warnings        []string
}

// NotionImportResult summarizes a Notion export folder/ZIP import.
type NotionImportResult struct {
	Databases       []string
	Records         int
	Assets          int
	StandaloneNotes []string
}

// ExportCSV serializes the records and columns of res (or all schema columns
// when opts.IncludeHidden is set) into RFC 4180 CSV bytes.
func ExportCSV(db *Database, res *QueryResult, opts CSVExportOptions) ([]byte, error) {
	if db == nil {
		return nil, errors.New("database: nil database")
	}
	var cols []Property
	if opts.IncludeHidden {
		cols = slices.Clone(db.Properties)
	} else {
		cols = snapshotColumns(db, res)
	}
	rows := snapshotRows(db, res)

	var buf bytes.Buffer
	if opts.IncludeBOM {
		buf.Write([]byte{0xEF, 0xBB, 0xBF})
	}
	w := csv.NewWriter(&buf)

	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = c.Name
	}
	if err := w.Write(headers); err != nil {
		return nil, err
	}

	line := make([]string, len(cols))
	for _, rec := range rows {
		if rec == nil {
			continue
		}
		for i, col := range cols {
			if col.Type == PropTitle {
				line[i] = rec.Title
			} else {
				line[i] = FormatValue(col, rec.ValueFor(col))
			}
		}
		if err := w.Write(line); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ExportViewCSV queries viewID (or the database's active view when empty) and
// serializes the resulting filtered/sorted records and computed columns to CSV.
func (s *Store) ExportViewCSV(viewID string, qopts QueryOptions, opts CSVExportOptions) ([]byte, error) {
	if s == nil {
		return nil, errors.New("database: nil store")
	}
	db := s.Database()
	if db == nil {
		return nil, errors.New("database: nil schema")
	}
	res := s.Query(viewID, qopts)
	return ExportCSV(db, res, opts)
}

var (
	emailRe              = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	notionRelSuffixRe    = regexp.MustCompile(`\s*\(https?://(?:www\.)?notion\.so/[^\s)]*\)\s*$`)
	notionUUIDSuffixRe   = regexp.MustCompile(`(?i)\s+[0-9a-f]{32}$`)
	mdLocalLinkRe        = regexp.MustCompile(`(!?\[[^\]]*\])\(([^()\r\n]+)\)|(!\[\[)([^\]\r\n]+)(\]\])`)
	importDateLayouts    = []string{"2006-01-02", "2006/01/02", "Jan 2, 2006", "January 2, 2006", "2006年1月2日", "2006-1-2"}
	importDateTimeLayout = []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04",
		"2006/01/02 15:04",
		"Jan 2, 2006 15:04",
		"January 2, 2006 15:04",
		"Jan 2, 2006 3:04 PM",
		"January 2, 2006 3:04 PM",
	}
)

// InferPropertyType inspects a CSV column's header and sample cell values and
// chooses the closest Notion-style Property definition.
func InferPropertyType(header string, values []string, isFirst bool) Property {
	name := strings.TrimSpace(header)
	if name == "" {
		name = "列"
	}
	prop := Property{
		ID:    "prop_" + NewID()[:8],
		Name:  name,
		Type:  PropText,
		Width: 160,
	}
	if isFirst {
		prop.ID = "prop_title"
		prop.Key = "title"
		prop.Type = PropTitle
		prop.Width = 260
		return prop
	}

	var nonEmpty []string
	hasNotionRel := false
	for _, raw := range values {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		if notionRelSuffixRe.MatchString(t) {
			hasNotionRel = true
		}
		nonEmpty = append(nonEmpty, t)
	}
	if len(nonEmpty) == 0 {
		return prop
	}
	if hasNotionRel {
		prop.Width = 180
		return prop
	}

	lowHeader := strings.ToLower(name)
	allBool := true
	allNum := true
	allDate := true
	allURL := true
	allEmail := true
	currency := ""
	hasPercent := false
	hasCommas := false

	for _, s := range nonEmpty {
		if _, ok := parseImportBool(s); !ok {
			allBool = false
		}
		if _, fmtKind, usedComma, ok := parseImportNumber(s); !ok {
			allNum = false
		} else {
			if fmtKind == "percent" {
				hasPercent = true
			} else if fmtKind != "" && currency == "" {
				currency = fmtKind
			}
			if usedComma {
				hasCommas = true
			}
		}
		if dv := parseFlexibleDateValue(s); dv.Date.IsEmpty() {
			allDate = false
		}
		low := strings.ToLower(s)
		if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
			allURL = false
		}
		if !emailRe.MatchString(s) {
			allEmail = false
		}
	}

	switch {
	case allBool:
		prop.Type = PropCheckbox
		prop.Width = 84
		return prop
	case allNum:
		prop.Type = PropNumber
		prop.Width = 120
		cfg := &NumberConfig{Format: "number"}
		if hasPercent {
			cfg.Format = "percent"
		} else if currency != "" {
			cfg.Format = currency
		} else if hasCommas {
			cfg.Format = "number_with_commas"
		}
		prop.Number = cfg
		return prop
	case allDate:
		prop.Type = PropDate
		prop.Width = 156
		return prop
	case allURL:
		prop.Type = PropURL
		prop.Width = 200
		return prop
	case allEmail:
		prop.Type = PropEmail
		prop.Width = 180
		return prop
	}

	hasCommaTag := false
	for _, s := range nonEmpty {
		if strings.Contains(s, ",") {
			hasCommaTag = true
			break
		}
	}
	isStatusHeader := containsAnyWord(lowHeader, "status", "状态", "stage", "阶段", "state", "进度")
	isTagHeader := containsAnyWord(lowHeader, "tag", "tags", "标签", "multi", "category", "分类", "label")

	if hasCommaTag || isTagHeader {
		seen := map[string]bool{}
		var opts []SelectOption
		for _, s := range nonEmpty {
			for _, part := range strings.Split(s, ",") {
				tok := strings.TrimSpace(part)
				if tok == "" || seen[strings.ToLower(tok)] || len([]rune(tok)) > 48 {
					continue
				}
				seen[strings.ToLower(tok)] = true
				opts = append(opts, SelectOption{
					ID:    "opt_" + NewID()[:6],
					Name:  tok,
					Color: OptionColors[1+(len(opts)%(len(OptionColors)-1))],
				})
			}
		}
		if len(opts) > 0 && len(opts) <= 24 {
			if hasCommaTag || strings.Contains(lowHeader, "tag") || strings.Contains(lowHeader, "标签") || strings.Contains(lowHeader, "multi") {
				prop.Type = PropMultiSelect
				prop.Width = 170
			} else {
				prop.Type = PropSelect
				prop.Width = 130
			}
			prop.Options = opts
			return prop
		}
	}

	distinct := uniqueOrderedTokens(nonEmpty)
	if len(distinct) > 0 && (isStatusHeader || (len(distinct) <= 12 && (len(nonEmpty) >= 2 && (len(distinct)*2 <= len(nonEmpty) || len(distinct) <= 6)))) {
		maxLen := 0
		for _, tok := range distinct {
			if n := len([]rune(tok)); n > maxLen {
				maxLen = n
			}
		}
		if maxLen <= 40 {
			opts := make([]SelectOption, 0, len(distinct))
			for i, tok := range distinct {
				opt := SelectOption{
					ID:    "opt_" + NewID()[:6],
					Name:  tok,
					Color: OptionColors[1+(i%(len(OptionColors)-1))],
				}
				if isStatusHeader {
					opt.Group = inferStatusGroup(tok)
					switch opt.Group {
					case StatusComplete:
						opt.Color = "green"
					case StatusInProgress:
						opt.Color = "blue"
					default:
						opt.Color = "gray"
					}
				}
				opts = append(opts, opt)
			}
			if isStatusHeader {
				prop.Type = PropStatus
				prop.Width = 128
			} else {
				prop.Type = PropSelect
				prop.Width = 130
			}
			prop.Options = opts
			return prop
		}
	}

	return prop
}

// ParseImportedCellValue converts a raw imported CSV string according to prop.
func ParseImportedCellValue(prop Property, raw string) Value {
	s := strings.TrimSpace(raw)
	if s == "" {
		return EmptyValue()
	}

	switch prop.Type {
	case PropTitle, PropText, PropURL, PropEmail, PropPhone:
		cleaned := cleanNotionRelationText(s)
		return TextValue(cleaned)

	case PropRelation:
		parts := splitCleanRelationRefs(s)
		return ListValue(parts...)

	case PropMultiSelect, PropPerson, PropFiles:
		var items []string
		for _, part := range strings.Split(s, ",") {
			if p := strings.TrimSpace(cleanNotionRelationText(part)); p != "" {
				items = append(items, p)
			}
		}
		return dedupeStrings(items)

	case PropSelect, PropStatus:
		return TextValue(strings.TrimSpace(cleanNotionRelationText(s)))

	case PropCheckbox:
		if b, ok := parseImportBool(s); ok {
			return BoolValue(b)
		}
		return CoerceValue(prop, TextValue(s))

	case PropNumber:
		if n, _, _, ok := parseImportNumber(s); ok {
			return NumberValue(n)
		}
		return CoerceValue(prop, TextValue(s))

	case PropDate:
		return parseFlexibleDateValue(s)

	default:
		return CoerceValue(prop, TextValue(cleanNotionRelationText(s)))
	}
}

// ImportCSVIntoStore reads CSV rows from r, adds missing inferred columns to s,
// and creates records (persisting folder .md files automatically).
func ImportCSVIntoStore(s *Store, r io.Reader) (CSVImportResult, error) {
	var res CSVImportResult
	if s == nil || r == nil {
		return res, errors.New("database: nil store or reader")
	}
	headers, rows, err := parseCSVTable(r)
	if err != nil {
		return res, err
	}
	if len(headers) == 0 {
		return res, errors.New("database: empty CSV header")
	}

	props, added, colMap, err := alignCSVColumnsToStore(s, headers, rows)
	if err != nil {
		return res, err
	}
	res.AddedProperties = added

	for _, row := range rows {
		title := ""
		cellProps := make(map[string]Value, len(props))
		nonEmpty := false
		for idx, val := range row {
			if strings.TrimSpace(val) != "" {
				nonEmpty = true
			}
			if idx >= len(colMap) || colMap[idx] < 0 {
				continue
			}
			p := props[colMap[idx]]
			if p.Type == PropTitle {
				if title == "" {
					title = cleanNotionRelationText(strings.TrimSpace(val))
				}
				continue
			}
			v := ParseImportedCellValue(p, val)
			if !v.IsEmpty() {
				cellProps[p.ID] = v
			}
		}
		if !nonEmpty {
			continue
		}
		if title == "" {
			title = "未命名"
		}
		if _, cerr := s.CreateRecord(title, cellProps, ""); cerr != nil {
			res.Warnings = append(res.Warnings, cerr.Error())
			continue
		}
		res.CreatedRecords++
	}
	return res, nil
}

// ImportCSVAsNewDatabase creates a new folder-backed .db.md database in
// spaceRoot/targetDir populated from CSV data in r.
func ImportCSVAsNewDatabase(spaceRoot, targetDir, title string, r io.Reader) (string, CSVImportResult, error) {
	var res CSVImportResult
	if spaceRoot == "" || r == nil {
		return "", res, errors.New("database: empty space root or CSV reader")
	}
	headers, rows, err := parseCSVTable(r)
	if err != nil {
		return "", res, err
	}
	if len(headers) == 0 {
		return "", res, errors.New("database: empty CSV header")
	}

	title = strings.TrimSpace(title)
	if title == "" {
		title = "导入数据库"
	}

	db, _ := buildInferredDatabaseFromCSV(title, headers, rows)
	placeholder, err := EncodeDatabaseMarkdownRelative(targetDir, db, nil)
	if err != nil {
		return "", res, err
	}
	dbRel, err := vault.CreateFile(spaceRoot, targetDir, title, vault.ExtDatabase, placeholder)
	if err != nil {
		return "", res, err
	}
	dbRel = filepath.ToSlash(dbRel)
	actualTitle := vault.Title(dbRel)
	if actualTitle != "" {
		db.Title = actualTitle
	}

	store := NewStore(spaceRoot, dbRel, db)
	for _, row := range rows {
		recTitle, propsMap, anyVal := extractCSVRowValues(db.Properties, row)
		if !anyVal {
			continue
		}
		if recTitle == "" {
			recTitle = "未命名"
		}
		if _, cerr := store.CreateRecord(recTitle, propsMap, ""); cerr != nil {
			res.Warnings = append(res.Warnings, cerr.Error())
			continue
		}
		res.CreatedRecords++
	}
	encoded, err := store.EncodeMarkdown(time.Now())
	if err != nil {
		return dbRel, res, err
	}
	if err := vault.Write(spaceRoot, dbRel, encoded); err != nil {
		return dbRel, res, err
	}
	res.AddedProperties = len(db.Properties)
	return dbRel, res, nil
}

// ImportNotionDir imports databases (CSV + per-row Markdown body + local
// assets) and standalone Markdown pages from an extracted Notion export folder.
func ImportNotionDir(spaceRoot, targetDir, srcDir string) (NotionImportResult, error) {
	var res NotionImportResult
	if spaceRoot == "" || srcDir == "" {
		return res, errors.New("database: empty space root or source dir")
	}
	info, err := os.Stat(srcDir)
	if err != nil || !info.IsDir() {
		return res, fmt.Errorf("database: source %q is not a directory", srcDir)
	}

	var csvFiles []string
	var mdFiles []string
	err = filepath.WalkDir(srcDir, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") || strings.EqualFold(name, "__MACOSX") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		low := strings.ToLower(name)
		switch {
		case strings.HasSuffix(low, ".csv"):
			csvFiles = append(csvFiles, p)
		case strings.HasSuffix(low, ".md") || strings.HasSuffix(low, ".markdown"):
			mdFiles = append(mdFiles, p)
		}
		return nil
	})
	if err != nil {
		return res, err
	}

	chosenCSVs := selectPreferredNotionCSVs(csvFiles)
	claimedDirs := make([]string, 0, len(chosenCSVs))
	claimedMDs := map[string]bool{}

	for _, csvPath := range chosenCSVs {
		rawBytes, rerr := os.ReadFile(csvPath)
		if rerr != nil {
			continue
		}
		headers, rows, perr := parseCSVTable(bytes.NewReader(rawBytes))
		if perr != nil || len(headers) == 0 {
			continue
		}

		csvBase := filepath.Base(csvPath)
		rawStem := trimNotionCSVStem(csvBase)
		cleanTitle := stripNotionUUID(rawStem)
		if cleanTitle == "" {
			cleanTitle = "Notion 数据库"
		}

		rowDir := findNotionRowDir(filepath.Dir(csvPath), rawStem, cleanTitle)
		var rowIndex map[string]string
		if rowDir != "" {
			claimedDirs = append(claimedDirs, rowDir)
			rowIndex = indexNotionMarkdownFiles(rowDir, claimedMDs)
		}

		db, _ := buildInferredDatabaseFromCSV(cleanTitle, headers, rows)
		placeholder, encErr := EncodeDatabaseMarkdownRelative(targetDir, db, nil)
		if encErr != nil {
			continue
		}
		dbRel, cerr := vault.CreateFile(spaceRoot, targetDir, cleanTitle, vault.ExtDatabase, placeholder)
		if cerr != nil {
			continue
		}
		dbRel = filepath.ToSlash(dbRel)
		if vt := vault.Title(dbRel); vt != "" {
			db.Title = vt
		}

		store := NewStore(spaceRoot, dbRel, db)
		recDir := store.ResolveSourceDir()
		assetDestDir := path.Join(recDir, "assets")

		for _, row := range rows {
			recTitle, propsMap, anyVal := extractCSVRowValues(db.Properties, row)
			if !anyVal {
				continue
			}
			if recTitle == "" {
				recTitle = "未命名"
			}
			body := ""
			if rowIndex != nil {
				if mdPath := matchNotionRowMarkdown(rowIndex, recTitle); mdPath != "" {
					claimedMDs[mdPath] = true
					if rawMD, merr := os.ReadFile(mdPath); merr == nil {
						body = stripNotionPageHeader(string(rawMD), headers)
						rewritten, copied := copyRewriteLocalAssets(srcDir, filepath.Dir(mdPath), spaceRoot, recDir, assetDestDir, body)
						body = rewritten
						res.Assets += copied
					}
				}
			}
			if _, rcerr := store.CreateRecord(recTitle, propsMap, body); rcerr == nil {
				res.Records++
			}
		}

		if encoded, eerr := store.EncodeMarkdown(time.Now()); eerr == nil {
			_ = vault.Write(spaceRoot, dbRel, encoded)
		}
		res.Databases = append(res.Databases, dbRel)
	}

	// Import remaining standalone Markdown notes outside database row directories.
	for _, mdPath := range mdFiles {
		if claimedMDs[mdPath] || isInsideAnyDir(mdPath, claimedDirs) {
			continue
		}
		rawMD, rerr := os.ReadFile(mdPath)
		if rerr != nil {
			continue
		}
		stem := strings.TrimSuffix(filepath.Base(mdPath), filepath.Ext(mdPath))
		cleanTitle := stripNotionUUID(stem)
		if cleanTitle == "" {
			cleanTitle = stem
		}
		assetDest := path.Join(targetDir, "assets")
		body, copied := copyRewriteLocalAssets(srcDir, filepath.Dir(mdPath), spaceRoot, targetDir, assetDest, string(rawMD))
		res.Assets += copied
		if rel, cerr := vault.CreateFile(spaceRoot, targetDir, cleanTitle, ".md", body); cerr == nil {
			res.StandaloneNotes = append(res.StandaloneNotes, filepath.ToSlash(rel))
		}
	}

	return res, nil
}

// ImportNotionZIP extracts a Notion export archive safely (blocking zip-slip
// and unbounded archives, supporting Notion's nested wrapper ZIP) and imports
// its databases and Markdown pages into spaceRoot/targetDir.
func ImportNotionZIP(spaceRoot, targetDir, zipPath string) (NotionImportResult, error) {
	var res NotionImportResult
	if spaceRoot == "" || zipPath == "" {
		return res, errors.New("database: empty space root or zip path")
	}
	tmpDir, err := os.MkdirTemp("", "arcmd-notion-*")
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(tmpDir)

	const maxExtractBytes int64 = 256 << 20 // 256 MB guard
	var used int64
	if err := extractZipSecure(zipPath, tmpDir, &used, maxExtractBytes, 0); err != nil {
		return res, err
	}
	return ImportNotionDir(spaceRoot, targetDir, tmpDir)
}

func extractZipSecure(zipPath, destDir string, totalWritten *int64, limit int64, depth int) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	cleanDest := filepath.Clean(destDir)
	var nestedZips []string
	hasContent := false

	for _, f := range zr.File {
		relName := filepath.Clean(filepath.FromSlash(strings.ReplaceAll(f.Name, `\`, "/")))
		if relName == "." || relName == "" {
			continue
		}
		if filepath.IsAbs(relName) || !filepath.IsLocal(relName) {
			return fmt.Errorf("database: unsafe zip entry %q", f.Name)
		}
		target := filepath.Join(cleanDest, relName)
		if !strings.HasPrefix(target, cleanDest+string(filepath.Separator)) {
			return fmt.Errorf("database: zip entry escapes root %q", f.Name)
		}
		base := filepath.Base(target)
		if strings.HasPrefix(base, "._") || strings.Contains(f.Name, "__MACOSX/") {
			continue
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		rem := limit - *totalWritten
		if rem <= 0 {
			out.Close()
			rc.Close()
			return errors.New("database: zip extract exceeded size limit")
		}
		n, copyErr := io.Copy(out, io.LimitReader(rc, rem+1))
		*totalWritten += n
		outErr := out.Close()
		rcErr := rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if *totalWritten > limit {
			return errors.New("database: zip extract exceeded size limit")
		}
		if outErr != nil {
			return outErr
		}
		if rcErr != nil {
			return rcErr
		}
		low := strings.ToLower(base)
		if strings.HasSuffix(low, ".zip") {
			nestedZips = append(nestedZips, target)
		} else {
			hasContent = true
		}
	}

	if depth < 2 && !hasContent && len(nestedZips) > 0 {
		for _, nz := range nestedZips {
			sub := strings.TrimSuffix(nz, filepath.Ext(nz))
			if err := os.MkdirAll(sub, 0o755); err != nil {
				return err
			}
			if err := extractZipSecure(nz, sub, totalWritten, limit, depth+1); err != nil {
				return err
			}
			_ = os.Remove(nz)
		}
	}
	return nil
}

func parseCSVTable(r io.Reader) ([]string, [][]string, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	all, err := cr.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(all) == 0 {
		return nil, nil, nil
	}
	headers := make([]string, len(all[0]))
	for i, h := range all[0] {
		if i == 0 {
			h = strings.TrimPrefix(h, "\xEF\xBB\xBF")
		}
		headers[i] = strings.TrimSpace(h)
	}
	return headers, all[1:], nil
}

func buildInferredDatabaseFromCSV(title string, headers []string, rows [][]string) (*Database, []int) {
	props := make([]Property, 0, len(headers))
	usedKeys := map[string]bool{}
	colMap := make([]int, len(headers))

	for i, h := range headers {
		if h == "" && i > 0 {
			colMap[i] = -1
			continue
		}
		vals := make([]string, 0, len(rows))
		for _, row := range rows {
			if i < len(row) {
				vals = append(vals, row[i])
			}
		}
		p := InferPropertyType(h, vals, i == 0)
		baseKey := uniqueSchemaPropKey(&Database{Properties: props}, p.Name)
		if p.Type == PropTitle {
			baseKey = "title"
		}
		for usedKeys[strings.ToLower(baseKey)] {
			baseKey += "_col"
		}
		usedKeys[strings.ToLower(baseKey)] = true
		p.Key = baseKey
		colMap[i] = len(props)
		props = append(props, p)
	}
	if len(props) == 0 {
		props = []Property{{ID: "prop_title", Key: "title", Name: "名称", Type: PropTitle, Width: 260}}
	}

	vis := make([]ViewProperty, 0, len(props))
	statusPropID := ""
	datePropID := ""
	for _, p := range props {
		w := p.Width
		if w <= 0 {
			w = 150
		}
		vis = append(vis, ViewProperty{PropertyID: p.ID, Visible: true, Width: w})
		if statusPropID == "" && (p.Type == PropStatus || p.Type == PropSelect) {
			statusPropID = p.ID
		}
		if datePropID == "" && p.Type == PropDate {
			datePropID = p.ID
		}
	}

	views := []ViewConfig{
		{
			ID:         "view_table",
			Name:       "表格",
			Type:       ViewTable,
			Properties: slices.Clone(vis),
			OpenMode:   "side",
			Table: &TableViewConfig{
				FreezeFirst: true,
				ShowIcon:    true,
				Aggregations: map[string]AggregationType{
					props[0].ID: AggCountAll,
				},
			},
		},
	}
	if statusPropID != "" {
		views = append(views, ViewConfig{
			ID:         "view_board",
			Name:       "看板",
			Type:       ViewBoard,
			Group:      &GroupConfig{PropertyID: statusPropID, StatusBy: "option"},
			Properties: slices.Clone(vis),
			OpenMode:   "side",
			Board:      &BoardViewConfig{CardSize: "medium", ColorColumn: true, ShowCount: true},
		})
	}
	if datePropID != "" {
		views = append(views, ViewConfig{
			ID:         "view_calendar",
			Name:       "日历",
			Type:       ViewCalendar,
			Properties: slices.Clone(vis),
			OpenMode:   "side",
			Calendar:   &CalendarViewConfig{DatePropID: datePropID, Mode: "month", ShowWeekends: true},
		})
	}

	return &Database{
		Version:    CurrentVersion,
		ID:         NewID(),
		Title:      title,
		Source:     SourceConfig{Type: SourceFolder},
		Properties: props,
		Views:      views,
		ActiveView: views[0].ID,
	}, colMap
}

func alignCSVColumnsToStore(s *Store, headers []string, rows [][]string) ([]Property, int, []int, error) {
	db := s.Database()
	if db == nil {
		return nil, 0, nil, errors.New("database: nil schema")
	}
	colMap := make([]int, len(headers))
	added := 0

	findProp := func(name string) int {
		low := strings.ToLower(strings.TrimSpace(name))
		for i, p := range s.Database().Properties {
			if strings.ToLower(p.Name) == low || strings.ToLower(p.Key) == low {
				return i
			}
		}
		return -1
	}

	for i, h := range headers {
		trimmed := strings.TrimSpace(h)
		if trimmed == "" {
			colMap[i] = -1
			continue
		}
		if i == 0 {
			idx := -1
			for j, p := range s.Database().Properties {
				if p.Type == PropTitle {
					idx = j
					break
				}
			}
			if idx >= 0 {
				colMap[i] = idx
				continue
			}
		}
		if idx := findProp(trimmed); idx >= 0 {
			colMap[i] = idx
			continue
		}
		vals := make([]string, 0, len(rows))
		for _, row := range rows {
			if i < len(row) {
				vals = append(vals, row[i])
			}
		}
		inferred := InferPropertyType(trimmed, vals, false)
		inferred.Key = uniqueSchemaPropKey(s.Database(), trimmed)
		if err := s.AddProperty(inferred); err != nil {
			colMap[i] = -1
			continue
		}
		added++
		colMap[i] = len(s.Database().Properties) - 1
	}
	return slices.Clone(s.Database().Properties), added, colMap, nil
}

func extractCSVRowValues(props []Property, row []string) (title string, out map[string]Value, nonEmpty bool) {
	out = make(map[string]Value, len(props))
	for i, p := range props {
		if i >= len(row) {
			continue
		}
		raw := strings.TrimSpace(row[i])
		if raw != "" {
			nonEmpty = true
		}
		if p.Type == PropTitle {
			title = cleanNotionRelationText(raw)
			continue
		}
		v := ParseImportedCellValue(p, raw)
		if !v.IsEmpty() {
			out[p.ID] = v
		}
	}
	return title, out, nonEmpty
}

func parseImportBool(s string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "yes", "1", "x", "[x]", "on", "是", "☑", "checked", "✅":
		return true, true
	case "false", "no", "0", "[ ]", "[]", "off", "否", "☐", "unchecked":
		return false, true
	}
	return false, false
}

func parseImportNumber(s string) (val float64, fmtKind string, usedComma bool, ok bool) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, "", false, false
	}
	switch {
	case strings.HasPrefix(t, "$"):
		fmtKind = "dollar"
		t = strings.TrimPrefix(t, "$")
	case strings.HasPrefix(t, "¥"):
		fmtKind = "yuan"
		t = strings.TrimPrefix(t, "¥")
	case strings.HasPrefix(t, "€"):
		fmtKind = "euro"
		t = strings.TrimPrefix(t, "€")
	case strings.HasPrefix(t, "£"):
		fmtKind = "pound"
		t = strings.TrimPrefix(t, "£")
	}
	pct := false
	if strings.HasSuffix(t, "%") {
		pct = true
		fmtKind = "percent"
		t = strings.TrimSpace(strings.TrimSuffix(t, "%"))
	}
	t = strings.TrimSpace(t)
	if strings.Contains(t, ",") {
		usedComma = true
		t = strings.ReplaceAll(t, ",", "")
	}
	n, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return 0, "", false, false
	}
	if pct {
		n /= 100
	}
	return n, fmtKind, usedComma, true
}

func parseFlexibleDateSingle(raw string) (string, bool, bool) {
	if t, hasTime, ok := ParseDateString(raw, time.UTC); ok {
		return formatNormalizedDate(raw, t, hasTime), hasTime, true
	}
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false, false
	}
	for _, layout := range importDateLayouts {
		if parsed, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return parsed.Format("2006-01-02"), false, true
		}
	}
	for _, layout := range importDateTimeLayout {
		if parsed, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return parsed.Format("2006-01-02T15:04"), true, true
		}
	}
	return "", false, false
}

func parseFlexibleDateValue(raw string) Value {
	s := strings.TrimSpace(raw)
	if s == "" {
		return EmptyValue()
	}
	for _, sep := range []string{" → ", " -> ", "->", " to "} {
		if a, b, ok := strings.Cut(s, sep); ok {
			sa, ha, oka := parseFlexibleDateSingle(a)
			sb, hb, okb := parseFlexibleDateSingle(b)
			if oka && okb {
				return DateRangeValue(sa, sb, ha || hb)
			}
		}
	}
	if start, hasTime, ok := parseFlexibleDateSingle(s); ok {
		return DateRangeValue(start, "", hasTime)
	}
	return EmptyValue()
}

func cleanNotionRelationText(s string) string {
	if !strings.Contains(s, "notion.so/") {
		return s
	}
	if !strings.Contains(s, ",") {
		return strings.TrimSpace(notionRelSuffixRe.ReplaceAllString(s, ""))
	}
	parts := strings.Split(s, ",")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(notionRelSuffixRe.ReplaceAllString(strings.TrimSpace(part), ""))
	}
	return strings.Join(parts, ", ")
}

func splitCleanRelationRefs(s string) []string {
	rawParts := strings.Split(s, ",")
	var out []string
	seen := map[string]bool{}
	for _, part := range rawParts {
		cleaned := strings.TrimSpace(notionRelSuffixRe.ReplaceAllString(strings.TrimSpace(part), ""))
		if cleaned == "" || seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		out = append(out, cleaned)
	}
	return out
}

func containsAnyWord(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

func uniqueOrderedTokens(vals []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range vals {
		t := strings.TrimSpace(v)
		if t == "" || seen[strings.ToLower(t)] {
			continue
		}
		seen[strings.ToLower(t)] = true
		out = append(out, t)
	}
	return out
}

func inferStatusGroup(name string) StatusGroup {
	low := strings.ToLower(strings.TrimSpace(name))
	switch {
	case containsAnyWord(low, "done", "complete", "finished", "closed", "resolved", "已完成", "完成", "已结束", "已关闭", "已发布"):
		return StatusComplete
	case containsAnyWord(low, "doing", "progress", "active", "review", "testing", "进行中", "开发中", "测试中", "审阅", "阻塞"):
		return StatusInProgress
	default:
		return StatusTodo
	}
}

func stripNotionUUID(name string) string {
	s := strings.TrimSpace(name)
	s = notionUUIDSuffixRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func trimNotionCSVStem(csvBase string) string {
	stem := strings.TrimSuffix(csvBase, filepath.Ext(csvBase))
	if strings.HasSuffix(strings.ToLower(stem), "_all") {
		stem = stem[:len(stem)-4]
	}
	return stem
}

func selectPreferredNotionCSVs(paths []string) []string {
	type group struct {
		dir      string
		stemKey  string
		regular  string
		allMatch string
	}
	groups := map[string]*group{}
	var order []string
	for _, p := range paths {
		dir := filepath.Dir(p)
		base := filepath.Base(p)
		stemNoExt := strings.TrimSuffix(base, filepath.Ext(base))
		isAll := strings.HasSuffix(strings.ToLower(stemNoExt), "_all")
		rawStem := trimNotionCSVStem(base)
		key := dir + "\x00" + strings.ToLower(stripNotionUUID(rawStem))
		g, ok := groups[key]
		if !ok {
			g = &group{dir: dir, stemKey: key}
			groups[key] = g
			order = append(order, key)
		}
		if isAll {
			g.allMatch = p
		} else {
			g.regular = p
		}
	}
	out := make([]string, 0, len(order))
	for _, key := range order {
		g := groups[key]
		if g.allMatch != "" {
			out = append(out, g.allMatch)
		} else if g.regular != "" {
			out = append(out, g.regular)
		}
	}
	return out
}

func findNotionRowDir(parentDir, rawStem, cleanTitle string) string {
	exact := filepath.Join(parentDir, rawStem)
	if info, err := os.Stat(exact); err == nil && info.IsDir() {
		return exact
	}
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return ""
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		if strings.EqualFold(stripNotionUUID(ent.Name()), cleanTitle) {
			return filepath.Join(parentDir, ent.Name())
		}
	}
	return ""
}

func indexNotionMarkdownFiles(rowDir string, skip map[string]bool) map[string]string {
	idx := map[string]string{}
	_ = filepath.WalkDir(rowDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || skip[p] {
			return nil
		}
		low := strings.ToLower(d.Name())
		if !strings.HasSuffix(low, ".md") && !strings.HasSuffix(low, ".markdown") {
			return nil
		}
		stem := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
		clean := normalizeLookupKey(stripNotionUUID(stem))
		if clean != "" {
			if _, exists := idx[clean]; !exists {
				idx[clean] = p
			}
		}
		return nil
	})
	return idx
}

func matchNotionRowMarkdown(idx map[string]string, title string) string {
	key := normalizeLookupKey(title)
	if key == "" {
		return ""
	}
	if p, ok := idx[key]; ok {
		return p
	}
	return ""
}

func normalizeLookupKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func stripNotionPageHeader(md string, headers []string) string {
	norm := strings.ReplaceAll(strings.ReplaceAll(md, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(norm, "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || !strings.HasPrefix(lines[i], "# ") {
		return strings.TrimRight(norm, "\n") + "\n"
	}
	i++ // drop Notion's duplicate top H1 line
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	known := make(map[string]bool, len(headers))
	for _, h := range headers {
		if k := strings.ToLower(strings.TrimSpace(h)); k != "" {
			known[k] = true
		}
	}
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			break
		}
		k, _, ok := strings.Cut(trimmed, ":")
		if !ok || !known[strings.ToLower(strings.TrimSpace(k))] {
			break
		}
		i++
	}
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	body := strings.Join(lines[i:], "\n")
	return strings.TrimRight(body, "\n")
}

func isInsideAnyDir(pathStr string, dirs []string) bool {
	cleanPath := filepath.Clean(pathStr)
	for _, d := range dirs {
		cleanDir := filepath.Clean(d)
		if cleanPath == cleanDir || strings.HasPrefix(cleanPath, cleanDir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func copyRewriteLocalAssets(srcRoot, mdDir, spaceRoot, docDir, assetDir, md string) (string, int) {
	if md == "" || assetDir == "" {
		return md, 0
	}
	copied := 0
	cache := map[string]string{}
	cleanSrcRoot := filepath.Clean(srcRoot)

	rewriteTarget := func(rawTarget string) (string, bool) {
		target := strings.TrimSpace(rawTarget)
		if target == "" || strings.HasPrefix(target, "#") || strings.Contains(target, "://") || strings.HasPrefix(strings.ToLower(target), "mailto:") || strings.HasPrefix(strings.ToLower(target), "data:") {
			return "", false
		}
		if prev, ok := cache[target]; ok {
			return prev, true
		}
		decoded, err := url.PathUnescape(target)
		if err != nil {
			decoded = target
		}
		decoded = strings.TrimPrefix(filepath.FromSlash(strings.ReplaceAll(decoded, `\`, "/")), string(filepath.Separator))
		cand := filepath.Clean(filepath.Join(mdDir, decoded))
		if !strings.HasPrefix(cand, cleanSrcRoot+string(filepath.Separator)) {
			return "", false
		}
		info, err := os.Stat(cand)
		if err != nil || info.IsDir() {
			return "", false
		}
		ext := strings.ToLower(filepath.Ext(cand))
		if ext == ".md" || ext == ".markdown" || ext == ".csv" {
			return "", false
		}
		rawBytes, err := os.ReadFile(cand)
		if err != nil {
			return "", false
		}
		cleanBase := stripNotionUUID(strings.TrimSuffix(filepath.Base(cand), ext))
		if cleanBase == "" {
			cleanBase = "asset"
		}
		destRel, aerr := writeUniqueBinaryAsset(spaceRoot, assetDir, cleanBase+ext, rawBytes)
		if aerr != nil {
			return "", false
		}
		relToDoc := relSlashPath(docDir, destRel)
		encodedRel := encodeMarkdownLinkPath(relToDoc)
		cache[target] = encodedRel
		copied++
		return encodedRel, true
	}

	out := mdLocalLinkRe.ReplaceAllStringFunc(md, func(full string) string {
		subs := mdLocalLinkRe.FindStringSubmatch(full)
		if subs == nil {
			return full
		}
		if subs[1] != "" {
			if next, ok := rewriteTarget(subs[2]); ok {
				return subs[1] + "(" + next + ")"
			}
			return full
		}
		if subs[3] != "" {
			if next, ok := rewriteTarget(subs[4]); ok {
				return subs[3] + next + subs[5]
			}
		}
		return full
	})
	return out, copied
}

func writeUniqueBinaryAsset(spaceRoot, assetDir, fileName string, data []byte) (string, error) {
	ext := filepath.Ext(fileName)
	stem := strings.TrimSuffix(fileName, ext)
	stem = strings.TrimSpace(stem)
	if stem == "" {
		stem = "asset"
	}
	for i := 1; i < 1000; i++ {
		name := stem + ext
		if i > 1 {
			name = fmt.Sprintf("%s %d%s", stem, i, ext)
		}
		rel := path.Join(assetDir, name)
		full, err := vault.Resolve(spaceRoot, rel)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return "", err
		}
		f, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(data)
		cerr := f.Close()
		if werr != nil {
			return "", werr
		}
		if cerr != nil {
			return "", cerr
		}
		return filepath.ToSlash(rel), nil
	}
	return "", errors.New("database: too many asset collisions")
}
