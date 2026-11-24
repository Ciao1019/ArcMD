package database

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/Ciao1019/ArcMD/internal/wiki"
)

// FMKind identifies the YAML value form parsed for a top-level frontmatter key.
type FMKind uint8

const (
	FMScalar FMKind = iota
	FMList
	FMBool
	FMNumber
)

// FMValue holds a parsed top-level YAML frontmatter value.
type FMValue struct {
	Kind   FMKind
	Scalar string
	Bool   bool
	Number float64
	List   []string
	Quoted bool
}

type fmEntry struct {
	key      string
	rawLines []string
	val      FMValue
	opaque   bool
}

// FrontmatterDoc preserves raw YAML frontmatter lines, comments, blank lines,
// and unknown nested mappings while supporting structured top-level updates.
type FrontmatterDoc struct {
	entries      []fmEntry
	hasFM        bool
	closeFenceLE string // line ending immediately after closing "---"
	endSep       string // optional single blank line between closing fence and body
	dirty        bool
}

// IsEmpty reports whether no frontmatter entries remain.
func (f *FrontmatterDoc) IsEmpty() bool {
	if f == nil {
		return true
	}
	for _, e := range f.entries {
		if e.key != "" {
			return false
		}
		for _, line := range e.rawLines {
			if strings.TrimSpace(line) != "" {
				return false
			}
		}
	}
	return true
}

// Get returns the parsed top-level value for key, if present and non-opaque.
func (f *FrontmatterDoc) Get(key string) (FMValue, bool) {
	if f == nil {
		return FMValue{}, false
	}
	for _, e := range f.entries {
		if e.key == key && !e.opaque {
			val := e.val
			if val.List != nil {
				val.List = append([]string(nil), val.List...)
			}
			return val, true
		}
	}
	return FMValue{}, false
}

// Set updates an existing top-level key in place or appends a new entry.
func (f *FrontmatterDoc) Set(key string, val FMValue) {
	if f == nil || strings.TrimSpace(key) == "" {
		return
	}
	lines := formatFMEntry(key, val)
	for i := range f.entries {
		if f.entries[i].key == key {
			if slices.Equal(f.entries[i].rawLines, lines) && !f.entries[i].opaque {
				return
			}
			f.entries[i].rawLines = lines
			f.entries[i].val = cloneFMValue(val)
			f.entries[i].opaque = false
			f.dirty = true
			return
		}
	}
	f.entries = append(f.entries, fmEntry{
		key:      key,
		rawLines: lines,
		val:      cloneFMValue(val),
	})
	f.dirty = true
}

// Delete removes key from the frontmatter if present.
func (f *FrontmatterDoc) Delete(key string) {
	if f == nil || key == "" {
		return
	}
	for i := range f.entries {
		if f.entries[i].key == key {
			f.entries = slices.Delete(f.entries, i, i+1)
			f.dirty = true
			return
		}
	}
}

// Format returns the inner YAML text without enclosing "---" fences, using "\n".
func (f *FrontmatterDoc) Format() string {
	return f.formatWith("\n")
}

func (f *FrontmatterDoc) formatWith(le string) string {
	if f == nil || len(f.entries) == 0 {
		return ""
	}
	var lines []string
	for _, e := range f.entries {
		lines = append(lines, e.rawLines...)
	}
	return strings.TrimRight(strings.Join(lines, le), "\r\n")
}

func cloneFMValue(v FMValue) FMValue {
	out := v
	if v.List != nil {
		out.List = append([]string(nil), v.List...)
	}
	return out
}

// SplitFrontmatter splits a Markdown source into its lossless FrontmatterDoc,
// body text (with up to one separating blank line trimmed), whether frontmatter
// was present, and the detected line ending ("\r\n" or "\n").
func SplitFrontmatter(source string) (fm *FrontmatterDoc, body string, hasFM bool, lineEnding string) {
	lineEnding = "\n"
	if strings.HasPrefix(source, "---\r\n") || (!strings.HasPrefix(source, "---\n") && strings.Contains(source, "\r\n")) {
		lineEnding = "\r\n"
	}
	if !strings.HasPrefix(source, "---\n") && !strings.HasPrefix(source, "---\r\n") {
		return &FrontmatterDoc{}, source, false, lineEnding
	}

	lines := strings.SplitAfter(source, "\n")
	end := len(lines[0])
	closed := false
	closeFenceLE := ""
	var fmLines []string
	for _, line := range lines[1:] {
		end += len(line)
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" || trimmed == "..." {
			closed = true
			if strings.HasSuffix(line, "\r\n") {
				closeFenceLE = "\r\n"
			} else if strings.HasSuffix(line, "\n") {
				closeFenceLE = "\n"
			}
			break
		}
		fmLines = append(fmLines, strings.TrimRight(line, "\r\n"))
	}
	if !closed {
		return &FrontmatterDoc{}, source, false, lineEnding
	}

	rest := source[end:]
	endSep := ""
	if strings.HasPrefix(rest, "\r\n") {
		endSep = "\r\n"
		body = rest[2:]
	} else if strings.HasPrefix(rest, "\n") {
		endSep = "\n"
		body = rest[1:]
	} else {
		body = rest
	}

	doc := &FrontmatterDoc{
		entries:      parseFrontmatterLines(fmLines),
		hasFM:        true,
		closeFenceLE: closeFenceLE,
		endSep:       endSep,
	}
	return doc, body, true, lineEnding
}

// ApplyFrontmatter parses source's frontmatter, calls mutate, and returns the
// updated Markdown preserving untouched frontmatter entries, line endings, and
// exact body bytes.
func ApplyFrontmatter(source string, mutate func(fm *FrontmatterDoc)) string {
	fm, body, hasFM, le := SplitFrontmatter(source)
	if mutate != nil {
		mutate(fm)
	}
	if !fm.dirty {
		return source
	}
	if fm.IsEmpty() {
		if !hasFM {
			return source
		}
		return body
	}
	inner := fm.formatWith(le)
	if !hasFM {
		if body == "" {
			return "---" + le + inner + le + "---" + le
		}
		sep := le
		if !strings.HasPrefix(body, "\n") && !strings.HasPrefix(body, "\r\n") {
			sep = le + le
		}
		return "---" + le + inner + le + "---" + sep + body
	}
	closeLE := fm.closeFenceLE
	if closeLE == "" && (fm.endSep != "" || body != "") {
		closeLE = le
	}
	return "---" + le + inner + le + "---" + closeLE + fm.endSep + body
}

func parseFrontmatterLines(lines []string) []fmEntry {
	var out []fmEntry
	for i := 0; i < len(lines); {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			out = append(out, fmEntry{rawLines: []string{line}, opaque: true})
			i++
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			j := i + 1
			for j < len(lines) && (len(lines[j]) == 0 || lines[j][0] == ' ' || lines[j][0] == '\t') {
				j++
			}
			out = append(out, fmEntry{rawLines: slices.Clone(lines[i:j]), opaque: true})
			i = j
			continue
		}

		rawKey, afterColon, hasColon := strings.Cut(line, ":")
		key := unquoteYAMLKey(strings.TrimSpace(rawKey))
		if !hasColon || key == "" || (afterColon != "" && afterColon[0] != ' ' && afterColon[0] != '\t') {
			out = append(out, fmEntry{rawLines: []string{line}, opaque: true})
			i++
			continue
		}

		valPart, _ := splitYAMLComment(strings.TrimSpace(afterColon))
		valPart = strings.TrimSpace(valPart)

		// Collect indented or sequence continuation lines owned by this key.
		j := i + 1
		if valPart == "" {
			for j < len(lines) {
				next := lines[j]
				nt := strings.TrimSpace(next)
				if nt == "" {
					// Keep trailing blank lines only if another indented/sequence line follows.
					k := j + 1
					for k < len(lines) && strings.TrimSpace(lines[k]) == "" {
						k++
					}
					if k < len(lines) && (lines[k][0] == ' ' || lines[k][0] == '\t' || strings.HasPrefix(lines[k], "- ") || lines[k] == "-") {
						j++
						continue
					}
					break
				}
				if next[0] == ' ' || next[0] == '\t' || strings.HasPrefix(next, "- ") || next == "-" {
					j++
					continue
				}
				break
			}
		} else {
			// Check for indented continuation (e.g. block scalar or broken multiline).
			for j < len(lines) && len(lines[j]) > 0 && (lines[j][0] == ' ' || lines[j][0] == '\t') {
				j++
			}
		}

		raw := slices.Clone(lines[i:j])
		if valPart == "" {
			if j > i+1 {
				items, ok := parseBlockSequence(lines[i+1 : j])
				if ok {
					out = append(out, fmEntry{
						key:      key,
						rawLines: raw,
						val:      FMValue{Kind: FMList, List: items},
					})
				} else {
					out = append(out, fmEntry{
						key:      key,
						rawLines: raw,
						opaque:   true,
					})
				}
			} else {
				out = append(out, fmEntry{
					key:      key,
					rawLines: raw,
					val:      FMValue{Kind: FMScalar},
				})
			}
			i = j
			continue
		}

		// Block scalar indicators ("|", "|-", ">", ">-") stay opaque for lossless round-trip,
		// while also extracting their text content for read-only database display.
		if isBlockScalarHeader(valPart) {
			out = append(out, fmEntry{
				key:      key,
				rawLines: raw,
				val:      FMValue{Kind: FMScalar, Scalar: joinBlockScalar(lines[i+1:j], valPart)},
				opaque:   true,
			})
			i = j
			continue
		}

		if j > i+1 {
			out = append(out, fmEntry{key: key, rawLines: raw, opaque: true})
			i = j
			continue
		}

		if strings.HasPrefix(valPart, "[") && strings.HasSuffix(valPart, "]") {
			if items, ok := parseFlowSequence(valPart); ok {
				out = append(out, fmEntry{
					key:      key,
					rawLines: raw,
					val:      FMValue{Kind: FMList, List: items},
				})
				i = j
				continue
			}
		}

		out = append(out, fmEntry{
			key:      key,
			rawLines: raw,
			val:      parseYAMLTypedScalar(valPart),
		})
		i = j
	}
	return out
}

func isBlockScalarHeader(s string) bool {
	if s == "" {
		return false
	}
	if s[0] != '|' && s[0] != '>' {
		return false
	}
	rest := s[1:]
	for _, r := range rest {
		if r != '+' && r != '-' && (r < '1' || r > '9') {
			return false
		}
	}
	return true
}

func joinBlockScalar(lines []string, header string) string {
	if len(lines) == 0 {
		return ""
	}
	minIndent := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		ind := len(l) - len(strings.TrimLeft(l, " "))
		if minIndent < 0 || ind < minIndent {
			minIndent = ind
		}
	}
	if minIndent < 0 {
		minIndent = 0
	}
	trimmed := make([]string, len(lines))
	for i, l := range lines {
		if len(l) >= minIndent {
			trimmed[i] = l[minIndent:]
		} else {
			trimmed[i] = strings.TrimSpace(l)
		}
	}
	if strings.HasPrefix(header, ">") {
		return strings.TrimSpace(strings.Join(trimmed, " "))
	}
	return strings.TrimRight(strings.Join(trimmed, "\n"), "\n")
}

func parseBlockSequence(lines []string) ([]string, bool) {
	var items []string
	itemIndent := -1
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		ind := len(line) - len(strings.TrimLeft(line, " \t"))
		rest := line[ind:]
		if itemIndent < 0 {
			if rest != "-" && !strings.HasPrefix(rest, "- ") {
				return nil, false
			}
			itemIndent = ind
		} else if ind != itemIndent || (rest != "-" && !strings.HasPrefix(rest, "- ")) {
			// Nested mappings or sub-blocks stay opaque.
			return nil, false
		}
		itemRaw := ""
		if len(rest) > 2 {
			itemRaw, _ = splitYAMLComment(strings.TrimSpace(rest[2:]))
			itemRaw = strings.TrimSpace(itemRaw)
		}
		if strings.HasPrefix(itemRaw, "{") || strings.HasPrefix(itemRaw, "[") {
			return nil, false
		}
		if k, after, ok := strings.Cut(itemRaw, ":"); ok && k != "" && (after == "" || after[0] == ' ') && !isQuotedScalar(itemRaw) {
			return nil, false
		}
		items = append(items, parseYAMLTypedScalar(itemRaw).Scalar)
	}
	return items, true
}

func parseFlowSequence(s string) ([]string, bool) {
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return nil, true
	}
	var items []string
	var cur strings.Builder
	var quote byte
	escaped := false
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if quote != 0 {
			cur.WriteByte(c)
			if quote == '"' {
				if escaped {
					escaped = false
				} else if c == '\\' {
					escaped = true
				} else if c == '"' {
					quote = 0
				}
			} else if c == '\'' {
				if i+1 < len(inner) && inner[i+1] == '\'' {
					cur.WriteByte(inner[i+1])
					i++
				} else {
					quote = 0
				}
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
			cur.WriteByte(c)
		case '[', '{':
			return nil, false
		case ',':
			item := strings.TrimSpace(cur.String())
			if item != "" {
				items = append(items, parseYAMLTypedScalar(item).Scalar)
			}
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	if quote != 0 {
		return nil, false
	}
	if last := strings.TrimSpace(cur.String()); last != "" {
		items = append(items, parseYAMLTypedScalar(last).Scalar)
	}
	return items, true
}

func splitYAMLComment(s string) (val, comment string) {
	var quote byte
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if quote == '"' {
				if escaped {
					escaped = false
				} else if c == '\\' {
					escaped = true
				} else if c == '"' {
					quote = 0
				}
			} else if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
				} else {
					quote = 0
				}
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t') {
			return strings.TrimRight(s[:i], " \t"), s[i:]
		}
	}
	return s, ""
}

func isQuotedScalar(s string) bool {
	return len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\''))
}

func unquoteYAMLKey(s string) string {
	if isQuotedScalar(s) {
		return parseYAMLTypedScalar(s).Scalar
	}
	return s
}

func parseYAMLTypedScalar(raw string) FMValue {
	s := strings.TrimSpace(raw)
	if s == "" {
		return FMValue{Kind: FMScalar}
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return FMValue{Kind: FMScalar, Scalar: u, Quoted: true}
		}
		return FMValue{Kind: FMScalar, Scalar: s[1 : len(s)-1], Quoted: true}
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return FMValue{Kind: FMScalar, Scalar: strings.ReplaceAll(s[1:len(s)-1], "''", "'"), Quoted: true}
	}
	switch strings.ToLower(s) {
	case "null", "~":
		return FMValue{Kind: FMScalar, Scalar: ""}
	case "true":
		return FMValue{Kind: FMBool, Bool: true, Scalar: s}
	case "false":
		return FMValue{Kind: FMBool, Bool: false, Scalar: s}
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && !looksLikeDateOrPhone(s) {
		return FMValue{Kind: FMNumber, Number: n, Scalar: s}
	}
	return FMValue{Kind: FMScalar, Scalar: s}
}

func looksLikeDateOrPhone(s string) bool {
	// Reject "+123", "001", or underscore numbers from turning into plain float if leading zero.
	if strings.ContainsRune(s, '_') {
		return true
	}
	if len(s) > 1 && s[0] == '0' && s[1] >= '0' && s[1] <= '9' {
		return true
	}
	if strings.HasPrefix(s, "+") {
		return true
	}
	return false
}

func formatFMEntry(key string, val FMValue) []string {
	switch val.Kind {
	case FMList:
		if len(val.List) == 0 {
			return []string{key + ": []"}
		}
		lines := make([]string, 0, len(val.List)+1)
		lines = append(lines, key+":")
		for _, item := range val.List {
			lines = append(lines, "  - "+formatYAMLStringScalar(item, false))
		}
		return lines
	case FMBool:
		if val.Bool {
			return []string{key + ": true"}
		}
		return []string{key + ": false"}
	case FMNumber:
		return []string{key + ": " + formatFloatCompact(val.Number)}
	default:
		return []string{key + ": " + formatYAMLStringScalar(val.Scalar, val.Quoted)}
	}
}

func formatYAMLStringScalar(s string, forceQuote bool) string {
	if s == "" {
		return `""`
	}
	needsQuote := forceQuote ||
		strings.TrimSpace(s) != s ||
		strings.ContainsAny(s, "\n\r\t#\"'\\") ||
		strings.Contains(s, ": ") ||
		strings.HasSuffix(s, ":") ||
		strings.HasPrefix(s, "[") || strings.HasPrefix(s, "{") ||
		strings.HasPrefix(s, "&") || strings.HasPrefix(s, "*") ||
		strings.HasPrefix(s, "!") || strings.HasPrefix(s, "%") ||
		strings.HasPrefix(s, "@") || strings.HasPrefix(s, "`") ||
		strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "? ") ||
		s == "-" || s == "?"
	if needsQuote {
		return strconv.Quote(s)
	}
	return s
}

func looksLikeYAMLSpecialScalar(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "false", "null", "~", "yes", "no", "on", "off":
		return true
	}
	if _, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
		return true
	}
	return false
}

// ExtractRecordFromMarkdown builds a database Record from a Markdown file's
// YAML frontmatter, body headings/tags/excerpt, and file timestamps.
func ExtractRecordFromMarkdown(schema []Property, relPath, source string, created, updated time.Time) *Record {
	fm, body, _, _ := SplitFrontmatter(source)
	rec := &Record{
		ID:        relPath,
		Path:      relPath,
		Title:     vault.Title(relPath),
		Props:     make(map[string]Value),
		CreatedAt: created,
		UpdatedAt: updated,
	}

	if v, ok := fm.Get("icon"); ok && v.Scalar != "" && len(v.Scalar) <= 32 && !strings.ContainsAny(v.Scalar, "\n\r") {
		rec.Icon = v.Scalar
	}
	if v, ok := fm.Get("cover"); ok && v.Scalar != "" {
		rec.Cover = v.Scalar
	}
	if v, ok := fm.Get("coverY"); ok {
		if y, err := strconv.ParseFloat(v.Scalar, 64); err == nil && y >= -1 && y <= 1 {
			rec.CoverY = y
		}
	}

	if h1 := extractFirstH1(body); h1 != "" {
		rec.Title = h1
	}
	rec.Excerpt = extractExcerpt(body, 200)
	rec.Tags = collectRecordTags(fm, body)

	for _, prop := range schema {
		switch prop.Type {
		case PropTitle, PropCreatedTime, PropEditedTime, PropRollup, PropFormula:
			continue
		case PropDate:
			val := extractDatePropFromFM(fm, prop)
			if !val.IsEmpty() {
				rec.Props[prop.ID] = val
			}
		case PropRelation:
			val := extractRelationPropFromFM(fm, prop)
			if !val.IsEmpty() {
				rec.Props[prop.ID] = val
			}
		case PropCheckbox:
			if fv, ok := fm.Get(prop.Key); ok {
				if fv.Kind == FMBool {
					rec.Props[prop.ID] = BoolValue(fv.Bool)
				} else {
					rec.Props[prop.ID] = CoerceValue(prop, TextValue(fv.Scalar))
				}
			}
		case PropNumber:
			if fv, ok := fm.Get(prop.Key); ok {
				if fv.Kind == FMNumber {
					rec.Props[prop.ID] = NumberValue(fv.Number)
				} else if n, nok := parseNumericText(fv.Scalar); nok {
					rec.Props[prop.ID] = NumberValue(n)
				}
			}
		case PropMultiSelect, PropFiles, PropPerson:
			if fv, ok := fm.Get(prop.Key); ok {
				if fv.Kind == FMList {
					if lv := dedupeStrings(fv.List); !lv.IsEmpty() {
						rec.Props[prop.ID] = lv
					}
				} else if fv.Scalar != "" {
					if lv := CoerceValue(prop, TextValue(fv.Scalar)); !lv.IsEmpty() {
						rec.Props[prop.ID] = lv
					}
				}
			}
		default:
			if fv, ok := fm.Get(prop.Key); ok {
				if fv.Kind == FMList && len(fv.List) > 0 {
					rec.Props[prop.ID] = CoerceValue(prop, ListValue(fv.List...))
				} else if fv.Scalar != "" {
					rec.Props[prop.ID] = CoerceValue(prop, TextValue(fv.Scalar))
				}
			}
		}
	}
	return rec
}

func dateEndKey(prop Property) string {
	if prop.Date != nil && strings.TrimSpace(prop.Date.EndKey) != "" {
		return strings.TrimSpace(prop.Date.EndKey)
	}
	return prop.Key + "_end"
}

func extractDatePropFromFM(fm *FrontmatterDoc, prop Property) Value {
	sv, ok := fm.Get(prop.Key)
	if !ok || strings.TrimSpace(sv.Scalar) == "" {
		return EmptyValue()
	}
	endKey := dateEndKey(prop)
	ev, hasEnd := fm.Get(endKey)
	startStr := strings.TrimSpace(sv.Scalar)
	endStr := ""
	if hasEnd {
		endStr = strings.TrimSpace(ev.Scalar)
	}
	if endStr == "" {
		parsed := parseDateTextValue(startStr)
		if !parsed.IsEmpty() {
			if prop.Date != nil && prop.Date.IncludeTime {
				parsed.Date.IncludeTime = true
			}
			return parsed
		}
	}
	_, startHasTime, okStart := ParseDateString(startStr, time.UTC)
	_, endHasTime, _ := ParseDateString(endStr, time.UTC)
	if !okStart {
		return EmptyValue()
	}
	incTime := startHasTime || endHasTime || (prop.Date != nil && prop.Date.IncludeTime)
	return DateRangeValue(startStr, endStr, incTime)
}

func extractRelationPropFromFM(fm *FrontmatterDoc, prop Property) Value {
	fv, ok := fm.Get(prop.Key)
	if !ok {
		return EmptyValue()
	}
	var raw []string
	if fv.Kind == FMList {
		raw = fv.List
	} else if strings.TrimSpace(fv.Scalar) != "" {
		raw = []string{fv.Scalar}
	}
	var out []string
	for _, item := range raw {
		if t := unwrapWikiTarget(item); t != "" {
			out = append(out, t)
		}
	}
	return dedupeStrings(out)
}

func unwrapWikiTarget(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "[[") && strings.HasSuffix(s, "]]") {
		target, _ := wiki.Parse(s[2 : len(s)-2])
		return strings.TrimSpace(target)
	}
	if links := wiki.Links(s); len(links) > 0 {
		return links[0].Target
	}
	return s
}

func wrapWikiTarget(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "[[") && strings.HasSuffix(s, "]]") {
		return s
	}
	return "[[" + s + "]]"
}

// WritePropsToFrontmatter writes schema-defined property values back into fm,
// deleting empty properties while leaving any non-schema frontmatter untouched.
func WritePropsToFrontmatter(schema []Property, fm *FrontmatterDoc, props map[string]Value) {
	if fm == nil {
		return
	}
	for _, prop := range schema {
		switch prop.Type {
		case PropTitle, PropCreatedTime, PropEditedTime, PropRollup, PropFormula:
			continue
		}
		val, exists := props[prop.ID]
		if !exists || val.IsEmpty() {
			fm.Delete(prop.Key)
			if prop.Type == PropDate {
				fm.Delete(dateEndKey(prop))
			}
			continue
		}
		switch prop.Type {
		case PropNumber:
			if val.Kind == ValNumber {
				fm.Set(prop.Key, FMValue{Kind: FMNumber, Number: val.Number})
			} else if n, ok := parseNumericText(val.Text); ok {
				fm.Set(prop.Key, FMValue{Kind: FMNumber, Number: n})
			} else {
				fm.Delete(prop.Key)
			}
		case PropCheckbox:
			fm.Set(prop.Key, FMValue{Kind: FMBool, Bool: val.Kind == ValBool && val.Bool})
		case PropDate:
			fm.Set(prop.Key, FMValue{Kind: FMScalar, Scalar: val.Date.Start})
			endKey := dateEndKey(prop)
			if val.Date.End != "" {
				fm.Set(endKey, FMValue{Kind: FMScalar, Scalar: val.Date.End})
			} else {
				fm.Delete(endKey)
			}
		case PropMultiSelect, PropFiles, PropPerson:
			fm.Set(prop.Key, FMValue{Kind: FMList, List: slices.Clone(val.List)})
		case PropRelation:
			if len(val.List) == 1 && prop.Relation != nil && prop.Relation.LimitOne {
				fm.Set(prop.Key, FMValue{Kind: FMScalar, Scalar: wrapWikiTarget(val.List[0]), Quoted: true})
			} else {
				wrapped := make([]string, 0, len(val.List))
				for _, item := range val.List {
					if w := wrapWikiTarget(item); w != "" {
						wrapped = append(wrapped, w)
					}
				}
				fm.Set(prop.Key, FMValue{Kind: FMList, List: wrapped})
			}
		default:
			text := FormatValue(prop, val)
			if text == "" {
				fm.Delete(prop.Key)
				continue
			}
			fm.Set(prop.Key, FMValue{
				Kind:   FMScalar,
				Scalar: text,
				Quoted: prop.Type == PropText && looksLikeYAMLSpecialScalar(text),
			})
		}
	}
}

func collectRecordTags(fm *FrontmatterDoc, body string) []string {
	seen := map[string]struct{}{}
	var tags []string
	add := func(t string) {
		t = strings.TrimSpace(strings.TrimPrefix(t, "#"))
		if t == "" {
			return
		}
		low := strings.ToLower(t)
		if _, exists := seen[low]; exists {
			return
		}
		seen[low] = struct{}{}
		tags = append(tags, t)
	}
	for _, key := range []string{"tags", "tag"} {
		if fv, ok := fm.Get(key); ok {
			if fv.Kind == FMList {
				for _, item := range fv.List {
					add(item)
				}
			} else if fv.Scalar != "" {
				for _, item := range strings.Split(fv.Scalar, ",") {
					add(item)
				}
			}
		}
	}
	for _, t := range wiki.Tags(body) {
		add(t.Name)
	}
	return tags
}

func extractFirstH1(body string) string {
	_, title := findFirstH1Line(body)
	return title
}

func findFirstH1Line(body string) (lineIdx int, title string) {
	fence := ""
	inComment := false
	for i, rawLine := range strings.Split(body, "\n") {
		line := strings.TrimRight(rawLine, "\r")
		trimmed := strings.TrimSpace(line)
		if inComment {
			if strings.Contains(trimmed, "-->") {
				inComment = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") {
			if !strings.Contains(trimmed, "-->") {
				inComment = true
			}
			continue
		}
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[:3]
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			h := strings.TrimSpace(trimmed[2:])
			h = strings.TrimRight(h, " \t#")
			if h != "" {
				return i, h
			}
		}
	}
	return -1, ""
}

func syncMarkdownH1(source, relPath, title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return source
	}
	_, body, hasFM, le := SplitFrontmatter(source)
	if le == "" {
		le = "\n"
	}
	prefix := source[:len(source)-len(body)]
	if idx, cur := findFirstH1Line(body); idx >= 0 {
		if cur == title {
			return source
		}
		lines := strings.SplitAfter(body, "\n")
		if idx < len(lines) {
			lineLE := ""
			if strings.HasSuffix(lines[idx], "\r\n") {
				lineLE = "\r\n"
			} else if strings.HasSuffix(lines[idx], "\n") {
				lineLE = "\n"
			}
			lines[idx] = "# " + title + lineLE
			return prefix + strings.Join(lines, "")
		}
	}
	if title == vault.Title(relPath) {
		return source
	}
	lead := prefix
	if hasFM && lead != "" && !strings.HasSuffix(lead, "\n") {
		lead += le
	}
	trimmedBody := strings.TrimLeft(body, "\r\n")
	if trimmedBody == "" {
		return lead + "# " + title + le
	}
	return lead + "# " + title + le + le + trimmedBody
}

func extractExcerpt(body string, maxRunes int) string {
	fence := ""
	inComment := false
	skippedH1 := false
	var parts []string
	runeCount := 0
	for _, rawLine := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(strings.TrimRight(rawLine, "\r"))
		if inComment {
			if strings.Contains(trimmed, "-->") {
				inComment = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") {
			if !strings.Contains(trimmed, "-->") {
				inComment = true
			}
			continue
		}
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[:3]
			continue
		}
		if trimmed == "" || trimmed == "---" || trimmed == "***" {
			continue
		}
		if !skippedH1 && strings.HasPrefix(trimmed, "# ") {
			skippedH1 = true
			continue
		}
		plain := stripMarkdownSnippet(trimmed)
		if plain == "" {
			continue
		}
		parts = append(parts, plain)
		runeCount += utf8.RuneCountInString(plain) + 1
		if runeCount >= maxRunes {
			break
		}
	}
	joined := strings.Join(parts, " ")
	if utf8.RuneCountInString(joined) <= maxRunes {
		return joined
	}
	runes := []rune(joined)
	return strings.TrimSpace(string(runes[:maxRunes])) + "…"
}

func stripMarkdownSnippet(s string) string {
	s = strings.TrimLeft(s, "#>-+* ")
	// Strip leading numbered list marker like "1. ".
	if dot := strings.Index(s, ". "); dot > 0 && dot <= 4 {
		digits := true
		for i := 0; i < dot; i++ {
			if !unicode.IsDigit(rune(s[i])) {
				digits = false
				break
			}
		}
		if digits {
			s = s[dot+2:]
		}
	}
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "~~", "")
	return strings.TrimSpace(s)
}

func (f *FrontmatterDoc) String() string {
	return fmt.Sprintf("FrontmatterDoc(entries=%d, hasFM=%v)", len(f.entries), f.hasFM)
}
