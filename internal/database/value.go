// Package database implements ArcMD's local-first Notion-style database engine:
// schema definitions, strongly-typed property values, lossless YAML frontmatter
// mapping, amphibious .db.md serialization, multi-view query pipelines, and
// bounded undo/redo history. It has no dependency on the window or UI toolkit.
package database

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ValueKind identifies the runtime variant held by a Value.
type ValueKind string

const (
	ValEmpty      ValueKind = ""
	ValText       ValueKind = "text"
	ValNumber     ValueKind = "number"
	ValBool       ValueKind = "bool"
	ValStringList ValueKind = "list"
	ValDate       ValueKind = "date"
	ValError      ValueKind = "error"
)

// DateValue represents a single date/datetime or an inclusive start..end range.
type DateValue struct {
	Start       string `json:"start,omitempty"`
	End         string `json:"end,omitempty"`
	IncludeTime bool   `json:"includeTime,omitempty"`
	TimeZone    string `json:"timeZone,omitempty"`
}

// IsEmpty reports whether the date has no start boundary.
func (d DateValue) IsEmpty() bool {
	return strings.TrimSpace(d.Start) == ""
}

// IsRange reports whether both start and end boundaries are present.
func (d DateValue) IsRange() bool {
	return !d.IsEmpty() && strings.TrimSpace(d.End) != ""
}

// Clone returns a copy of DateValue.
func (d DateValue) Clone() DateValue { return d }

// Equal reports whether two DateValues have identical fields.
func (d DateValue) Equal(other DateValue) bool {
	return d.Start == other.Start &&
		d.End == other.End &&
		d.IncludeTime == other.IncludeTime &&
		d.TimeZone == other.TimeZone
}

// StartTime parses Start in UTC (or TimeZone when valid).
func (d DateValue) StartTime() (time.Time, bool) {
	return d.parseBoundary(d.Start, time.UTC)
}

// EndTime parses End in UTC (or TimeZone when valid).
func (d DateValue) EndTime() (time.Time, bool) {
	return d.parseBoundary(d.End, time.UTC)
}

func (d DateValue) parseBoundary(raw string, fallback *time.Location) (time.Time, bool) {
	loc := fallback
	if loc == nil {
		loc = time.UTC
	}
	if d.TimeZone != "" {
		if loaded, err := time.LoadLocation(d.TimeZone); err == nil {
			loc = loaded
		}
	}
	t, _, ok := ParseDateString(raw, loc)
	return t, ok
}

// ParseDateString parses an ISO-like date or datetime string in loc.
// Date-only values return midnight in loc with hasTime=false.
func ParseDateString(raw string, loc *time.Location) (t time.Time, hasTime bool, ok bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, false, false
	}
	if loc == nil {
		loc = time.UTC
	}
	if len(s) == 10 {
		if parsed, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
			return parsed, false, true
		}
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04",
	} {
		if layout == time.RFC3339Nano || layout == time.RFC3339 {
			if parsed, err := time.Parse(layout, s); err == nil {
				return parsed, true, true
			}
			continue
		}
		if parsed, err := time.ParseInLocation(layout, s, loc); err == nil {
			return parsed, true, true
		}
	}
	// Accept YYYY/MM/DD as a convenient fallback when coercing user input.
	if len(s) == 10 && s[4] == '/' && s[7] == '/' {
		if parsed, err := time.ParseInLocation("2006/01/02", s, loc); err == nil {
			return parsed, false, true
		}
	}
	return time.Time{}, false, false
}

// Value is a tagged union holding a record's value for one database property.
type Value struct {
	Kind   ValueKind `json:"kind,omitempty"`
	Text   string    `json:"text,omitempty"`
	Number float64   `json:"number,omitempty"`
	Bool   bool      `json:"bool,omitempty"`
	List   []string  `json:"list,omitempty"`
	Date   DateValue `json:"date,omitempty"`
	Error  string    `json:"error,omitempty"`
}

// EmptyValue returns an unset Value.
func EmptyValue() Value { return Value{} }

// TextValue returns a text Value. Empty string maps to EmptyValue for canonical identity.
func TextValue(s string) Value {
	if s == "" {
		return Value{}
	}
	return Value{Kind: ValText, Text: s}
}

// NumberValue returns a numeric Value. NaN/Inf map to EmptyValue.
func NumberValue(n float64) Value {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return Value{}
	}
	return Value{Kind: ValNumber, Number: n}
}

// BoolValue returns a boolean Value.
func BoolValue(b bool) Value {
	return Value{Kind: ValBool, Bool: b}
}

// ListValue returns a string-list Value (copying xs and filtering empties).
func ListValue(xs ...string) Value {
	var clean []string
	for _, x := range xs {
		if x != "" {
			clean = append(clean, x)
		}
	}
	if len(clean) == 0 {
		return Value{}
	}
	return Value{Kind: ValStringList, List: clean}
}

// DateRangeValue returns a date Value from start and optional end strings.
func DateRangeValue(start, end string, includeTime bool) Value {
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	if start == "" {
		return Value{}
	}
	return Value{
		Kind: ValDate,
		Date: DateValue{
			Start:       start,
			End:         end,
			IncludeTime: includeTime,
		},
	}
}

// ErrorValue returns a formula/validation error Value.
func ErrorValue(msg string) Value {
	return Value{Kind: ValError, Error: msg}
}

// IsEmpty reports whether v carries no data. Bool(false) and Error are non-empty.
func (v Value) IsEmpty() bool {
	switch v.Kind {
	case ValEmpty:
		return true
	case ValText:
		return v.Text == ""
	case ValNumber:
		return math.IsNaN(v.Number) || math.IsInf(v.Number, 0)
	case ValBool:
		return false
	case ValStringList:
		return len(v.List) == 0
	case ValDate:
		return v.Date.IsEmpty()
	case ValError:
		return false
	default:
		return true
	}
}

// Clone returns a deep copy of v.
func (v Value) Clone() Value {
	out := v
	if v.List != nil {
		out.List = append([]string(nil), v.List...)
	}
	out.Date = v.Date.Clone()
	return out
}

// Equal reports whether v and other represent the same value.
func (v Value) Equal(other Value) bool {
	if v.IsEmpty() && other.IsEmpty() {
		return true
	}
	if v.Kind != other.Kind {
		return false
	}
	switch v.Kind {
	case ValText:
		return v.Text == other.Text
	case ValNumber:
		return v.Number == other.Number
	case ValBool:
		return v.Bool == other.Bool
	case ValStringList:
		return slices.Equal(v.List, other.List)
	case ValDate:
		return v.Date.Equal(other.Date)
	case ValError:
		return v.Error == other.Error
	default:
		return true
	}
}

// CoerceValue safely converts v into a Value compatible with prop's type.
// Incompatible or blank inputs degrade cleanly to EmptyValue (or Bool(false)).
func CoerceValue(prop Property, v Value) Value {
	if v.Kind == ValError {
		return v
	}
	switch prop.Type {
	case PropTitle, PropText, PropURL, PropEmail, PropPhone, PropUniqueID, PropFormula:
		if v.IsEmpty() {
			return EmptyValue()
		}
		if v.Kind == ValText {
			return v
		}
		return TextValue(FormatValue(Property{Type: PropText}, v))

	case PropSelect, PropStatus:
		if v.IsEmpty() {
			return EmptyValue()
		}
		switch v.Kind {
		case ValText:
			return TextValue(strings.TrimSpace(v.Text))
		case ValStringList:
			if len(v.List) > 0 {
				return TextValue(strings.TrimSpace(v.List[0]))
			}
			return EmptyValue()
		case ValNumber:
			return TextValue(formatFloatCompact(v.Number))
		case ValBool:
			if v.Bool {
				return TextValue("true")
			}
			return TextValue("false")
		case ValDate:
			return TextValue(v.Date.Start)
		default:
			return EmptyValue()
		}

	case PropMultiSelect, PropFiles, PropPerson, PropRelation:
		if v.IsEmpty() {
			return EmptyValue()
		}
		switch v.Kind {
		case ValStringList:
			return dedupeStrings(v.List)
		case ValText:
			if strings.TrimSpace(v.Text) == "" {
				return EmptyValue()
			}
			if prop.Type == PropMultiSelect {
				parts := strings.Split(v.Text, ",")
				var xs []string
				for _, p := range parts {
					if trimmed := strings.TrimSpace(p); trimmed != "" {
						xs = append(xs, trimmed)
					}
				}
				return dedupeStrings(xs)
			}
			return ListValue(strings.TrimSpace(v.Text))
		case ValNumber:
			return ListValue(formatFloatCompact(v.Number))
		case ValDate:
			return ListValue(v.Date.Start)
		default:
			return EmptyValue()
		}

	case PropNumber:
		if v.IsEmpty() {
			return EmptyValue()
		}
		switch v.Kind {
		case ValNumber:
			return v
		case ValBool:
			if v.Bool {
				return NumberValue(1)
			}
			return NumberValue(0)
		case ValText:
			if n, ok := parseNumericText(v.Text); ok {
				return NumberValue(n)
			}
			return EmptyValue()
		case ValStringList:
			if len(v.List) == 1 {
				if n, ok := parseNumericText(v.List[0]); ok {
					return NumberValue(n)
				}
			}
			return EmptyValue()
		default:
			return EmptyValue()
		}

	case PropCheckbox:
		switch v.Kind {
		case ValEmpty:
			return EmptyValue()
		case ValBool:
			return v
		case ValNumber:
			return BoolValue(v.Number != 0)
		case ValText:
			s := strings.ToLower(strings.TrimSpace(v.Text))
			switch s {
			case "true", "yes", "1", "x", "[x]", "on", "是":
				return BoolValue(true)
			case "false", "no", "0", "", "[ ]", "[]", "off", "否":
				return BoolValue(false)
			default:
				return BoolValue(true)
			}
		case ValStringList:
			return BoolValue(len(v.List) > 0)
		case ValDate:
			return BoolValue(!v.Date.IsEmpty())
		default:
			return EmptyValue()
		}

	case PropDate, PropCreatedTime, PropEditedTime:
		if v.IsEmpty() {
			return EmptyValue()
		}
		switch v.Kind {
		case ValDate:
			return v
		case ValText:
			return parseDateTextValue(v.Text)
		case ValStringList:
			if len(v.List) > 0 {
				return parseDateTextValue(v.List[0])
			}
			return EmptyValue()
		default:
			return EmptyValue()
		}

	default:
		return v.Clone()
	}
}

func dedupeStrings(in []string) Value {
	seen := make(map[string]struct{}, len(in))
	var out []string
	for _, s := range in {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return ListValue(out...)
}

func parseNumericText(raw string) (float64, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, false
	}
	pct := false
	if strings.HasSuffix(s, "%") {
		pct = true
		s = strings.TrimSpace(strings.TrimSuffix(s, "%"))
	}
	s = strings.TrimLeft(s, "$¥€£")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	if pct {
		n /= 100
	}
	return n, true
}

func parseDateTextValue(raw string) Value {
	s := strings.TrimSpace(raw)
	if s == "" {
		return EmptyValue()
	}
	for _, sep := range []string{" → ", "->", "/", " to "} {
		if a, b, ok := strings.Cut(s, sep); ok {
			// Avoid splitting YYYY/MM/DD by checking both halves parse as dates.
			ta, ha, oka := ParseDateString(a, time.UTC)
			tb, hb, okb := ParseDateString(b, time.UTC)
			if oka && okb {
				inc := ha || hb
				return DateRangeValue(formatNormalizedDate(a, ta, ha), formatNormalizedDate(b, tb, hb), inc)
			}
		}
	}
	t, hasTime, ok := ParseDateString(s, time.UTC)
	if !ok {
		return EmptyValue()
	}
	return DateRangeValue(formatNormalizedDate(s, t, hasTime), "", hasTime)
}

func formatNormalizedDate(orig string, t time.Time, hasTime bool) string {
	orig = strings.TrimSpace(orig)
	if !hasTime {
		return t.Format("2006-01-02")
	}
	if strings.ContainsAny(orig, "Zz") || strings.Contains(orig[min(len(orig), 10):], "+") || (len(orig) > 19 && strings.Contains(orig[19:], "-")) {
		return orig
	}
	return t.Format("2006-01-02T15:04")
}

// FormatValue returns a clean human-readable representation of v for prop.
func FormatValue(prop Property, v Value) string {
	if v.Kind == ValError {
		if strings.HasPrefix(v.Error, "#") {
			return v.Error
		}
		return "#ERR: " + v.Error
	}
	if v.IsEmpty() {
		return ""
	}
	switch v.Kind {
	case ValNumber:
		if prop.Type == PropRollup && prop.Rollup != nil && prop.Number == nil &&
			strings.HasPrefix(string(prop.Rollup.Calculate), "percent_") {
			return strconv.FormatFloat(math.Round(v.Number*10)/10, 'f', -1, 64) + "%"
		}
		return formatNumberValue(prop.Number, v.Number)
	case ValBool:
		if prop.Type == PropFormula || prop.Type == PropRollup {
			if v.Bool {
				return "true"
			}
			return "false"
		}
		if v.Bool {
			return "[x]"
		}
		return "[ ]"
	case ValStringList:
		return strings.Join(v.List, ", ")
	case ValDate:
		if v.Date.End != "" {
			return v.Date.Start + " → " + v.Date.End
		}
		return v.Date.Start
	case ValText:
		return v.Text
	default:
		return ""
	}
}

func formatFloatCompact(n float64) string {
	if n == 0 {
		return "0"
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

func formatNumberValue(cfg *NumberConfig, n float64) string {
	if cfg == nil {
		return formatFloatCompact(n)
	}
	format := strings.ToLower(strings.TrimSpace(cfg.Format))
	decimals := cfg.Decimals
	switch format {
	case "percent":
		pct := n * 100
		if decimals > 0 {
			return strconv.FormatFloat(pct, 'f', decimals, 64) + "%"
		}
		return formatFloatCompact(pct) + "%"
	case "number_with_commas":
		return formatWithCommas(n, decimals)
	case "dollar", "usd":
		return "$" + formatWithCommas(n, maxDecimals(decimals, 2, n))
	case "yuan", "cny", "rmb":
		return "¥" + formatWithCommas(n, maxDecimals(decimals, 2, n))
	case "euro", "eur":
		return "€" + formatWithCommas(n, maxDecimals(decimals, 2, n))
	case "pound", "gbp":
		return "£" + formatWithCommas(n, maxDecimals(decimals, 2, n))
	case "yen", "jpy":
		return "¥" + formatWithCommas(n, decimals)
	case "currency":
		sym := cfg.Currency
		if sym == "" {
			sym = "$"
		}
		return sym + formatWithCommas(n, maxDecimals(decimals, 2, n))
	default:
		if decimals > 0 {
			return strconv.FormatFloat(n, 'f', decimals, 64)
		}
		return formatFloatCompact(n)
	}
}

func maxDecimals(explicit, currencyDefault int, n float64) int {
	if explicit > 0 {
		return explicit
	}
	if n == math.Trunc(n) {
		return 0
	}
	return currencyDefault
}

func formatWithCommas(n float64, decimals int) string {
	var raw string
	if decimals > 0 {
		raw = strconv.FormatFloat(n, 'f', decimals, 64)
	} else {
		raw = formatFloatCompact(n)
	}
	sign := ""
	if strings.HasPrefix(raw, "-") {
		sign = "-"
		raw = raw[1:]
	}
	intPart, fracPart, hasFrac := strings.Cut(raw, ".")
	if len(intPart) <= 3 {
		if hasFrac {
			return sign + intPart + "." + fracPart
		}
		return sign + intPart
	}
	var b strings.Builder
	b.WriteString(sign)
	lead := len(intPart) % 3
	if lead == 0 {
		lead = 3
	}
	b.WriteString(intPart[:lead])
	for i := lead; i < len(intPart); i += 3 {
		b.WriteByte(',')
		b.WriteString(intPart[i : i+3])
	}
	if hasFrac {
		b.WriteByte('.')
		b.WriteString(fracPart)
	}
	return b.String()
}

func isEmptyForSort(prop Property, v Value) bool {
	if prop.Type == PropCheckbox {
		return false
	}
	return v.IsEmpty()
}

// CompareValuesForSort compares a and b under prop's semantics. Empty values
// always sort last regardless of desc.
func CompareValuesForSort(prop Property, a, b Value, desc bool) int {
	ae, be := isEmptyForSort(prop, a), isEmptyForSort(prop, b)
	if ae && be {
		return 0
	}
	if ae {
		return 1
	}
	if be {
		return -1
	}

	var c int
	switch prop.Type {
	case PropNumber:
		an, aok := numericSortVal(a)
		bn, bok := numericSortVal(b)
		switch {
		case aok && bok:
			c = cmp.Compare(an, bn)
		case aok:
			c = -1
		case bok:
			c = 1
		}

	case PropCheckbox:
		ab := a.Kind == ValBool && a.Bool
		bb := b.Kind == ValBool && b.Bool
		if ab != bb {
			if !ab {
				c = -1
			} else {
				c = 1
			}
		}

	case PropDate, PropCreatedTime, PropEditedTime:
		c = compareDateValues(a, b)

	case PropSelect, PropStatus:
		c = compareOptionOrder(prop, a.Text, b.Text)

	case PropMultiSelect, PropFiles, PropPerson, PropRelation:
		c = compareStringLists(prop, a.List, b.List)

	case PropUniqueID:
		c = compareUniqueIDs(a.Text, b.Text)

	default:
		switch {
		case a.Kind == ValNumber && b.Kind == ValNumber:
			c = cmp.Compare(a.Number, b.Number)
		case a.Kind == ValDate && b.Kind == ValDate:
			c = compareDateValues(a, b)
		case a.Kind == ValBool && b.Kind == ValBool:
			if a.Bool != b.Bool {
				if !a.Bool {
					c = -1
				} else {
					c = 1
				}
			}
		case a.Kind == ValStringList && b.Kind == ValStringList:
			c = compareStringLists(prop, a.List, b.List)
		default:
			as := FormatValue(prop, a)
			bs := FormatValue(prop, b)
			c = cmp.Compare(strings.ToLower(as), strings.ToLower(bs))
			if c == 0 {
				c = cmp.Compare(as, bs)
			}
		}
	}

	if desc && c != 0 {
		return -c
	}
	return c
}

func numericSortVal(v Value) (float64, bool) {
	if v.Kind == ValNumber && !math.IsNaN(v.Number) {
		return v.Number, true
	}
	if v.Kind == ValText {
		return parseNumericText(v.Text)
	}
	return 0, false
}

func compareDateValues(a, b Value) int {
	at, aok := a.Date.StartTime()
	bt, bok := b.Date.StartTime()
	if aok && bok {
		if !at.Equal(bt) {
			if at.Before(bt) {
				return -1
			}
			return 1
		}
		ae, aeok := a.Date.EndTime()
		be, beok := b.Date.EndTime()
		switch {
		case aeok && beok:
			if !ae.Equal(be) {
				if ae.Before(be) {
					return -1
				}
				return 1
			}
		case !aeok && beok:
			return -1
		case aeok && !beok:
			return 1
		}
	}
	return cmp.Compare(a.Date.Start, b.Date.Start)
}

func optionIndex(prop Property, val string) int {
	for i, opt := range prop.Options {
		if opt.ID == val || strings.EqualFold(opt.Name, val) {
			return i
		}
	}
	return -1
}

func compareOptionOrder(prop Property, a, b string) int {
	ai := optionIndex(prop, a)
	bi := optionIndex(prop, b)
	switch {
	case ai >= 0 && bi >= 0 && ai != bi:
		return cmp.Compare(ai, bi)
	case ai >= 0 && bi < 0:
		return -1
	case ai < 0 && bi >= 0:
		return 1
	}
	c := cmp.Compare(strings.ToLower(a), strings.ToLower(b))
	if c == 0 {
		c = cmp.Compare(a, b)
	}
	return c
}

func compareStringLists(prop Property, a, b []string) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		var c int
		if prop.Type == PropMultiSelect && len(prop.Options) > 0 {
			c = compareOptionOrder(prop, a[i], b[i])
		} else {
			c = cmp.Compare(strings.ToLower(a[i]), strings.ToLower(b[i]))
			if c == 0 {
				c = cmp.Compare(a[i], b[i])
			}
		}
		if c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a), len(b))
}

func compareUniqueIDs(a, b string) int {
	ap, an, aok := splitUniqueID(a)
	bp, bn, bok := splitUniqueID(b)
	if aok && bok && strings.EqualFold(ap, bp) {
		if an != bn {
			return cmp.Compare(an, bn)
		}
	}
	c := cmp.Compare(strings.ToLower(a), strings.ToLower(b))
	if c == 0 {
		c = cmp.Compare(a, b)
	}
	return c
}

func splitUniqueID(s string) (prefix string, num int, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0, false
	}
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	if i == len(s) {
		return "", 0, false
	}
	n, err := strconv.Atoi(s[i:])
	if err != nil {
		return "", 0, false
	}
	return s[:i], n, true
}

// PropValueText returns a plain string helper for formula/search consumers.
func (v Value) PlainString() string {
	return FormatValue(Property{}, v)
}

func (v Value) String() string {
	return fmt.Sprintf("Value(%s:%s)", v.Kind, v.PlainString())
}
