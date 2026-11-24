package database

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/pinyin"
)

// FilterOp identifies the comparison operator used by a FilterRule.
type FilterOp string

const (
	OpIsEmpty    FilterOp = "is_empty"
	OpIsNotEmpty FilterOp = "is_not_empty"

	OpEquals      FilterOp = "equals"
	OpNotEquals   FilterOp = "not_equals"
	OpContains    FilterOp = "contains"
	OpNotContains FilterOp = "does_not_contain"
	OpStartsWith  FilterOp = "starts_with"
	OpEndsWith    FilterOp = "ends_with"

	OpGT  FilterOp = "gt"
	OpGTE FilterOp = "gte"
	OpLT  FilterOp = "lt"
	OpLTE FilterOp = "lte"

	OpChecked   FilterOp = "checked"
	OpUnchecked FilterOp = "unchecked"

	OpContainsAny    FilterOp = "contains_any"
	OpContainsAll    FilterOp = "contains_all"
	OpNotContainsAny FilterOp = "not_contains_any"
	OpStatusGroupIs  FilterOp = "status_group_is"
	OpStatusGroupNot FilterOp = "status_group_not"

	OpDateIs         FilterOp = "date_is"
	OpDateBefore     FilterOp = "date_before"
	OpDateAfter      FilterOp = "date_after"
	OpDateOnOrBefore FilterOp = "date_on_or_before"
	OpDateOnOrAfter  FilterOp = "date_on_or_after"
	OpDateWithin     FilterOp = "date_within"
)

// QueryOptions parameterizes Store.Query execution.
type QueryOptions struct {
	Search      string
	Now         time.Time
	AdHocFilter *FilterGroup
	AdHocSorts  []SortRule
	Offset      int
	Limit       int
}

// AggregationResult holds a formatted and typed column/group summary value.
type AggregationResult struct {
	Type     AggregationType `json:"type"`
	Number   float64         `json:"number,omitempty"`
	Date     string          `json:"date,omitempty"`
	Display  string          `json:"display,omitempty"`
	HasValue bool            `json:"hasValue,omitempty"`
}

// RecordGroup holds one bucket of grouped records and optional sub-groups.
type RecordGroup struct {
	Key          string                       `json:"key"`
	Label        string                       `json:"label"`
	Color        string                       `json:"color,omitempty"`
	Value        Value                        `json:"value,omitempty"`
	Collapsed    bool                         `json:"collapsed,omitempty"`
	Count        int                          `json:"count"`
	Rows         []*Record                    `json:"rows,omitempty"`
	SubGroups    []RecordGroup                `json:"subGroups,omitempty"`
	Aggregations map[string]AggregationResult `json:"aggregations,omitempty"`
}

// QueryResult is the evaluated view snapshot returned by Store.Query.
type QueryResult struct {
	ViewID       string                       `json:"viewID"`
	Revision     uint64                       `json:"revision"`
	TotalCount   int                          `json:"totalCount"`
	Rows         []*Record                    `json:"rows,omitempty"`
	Groups       []RecordGroup                `json:"groups,omitempty"`
	Aggregations map[string]AggregationResult `json:"aggregations,omitempty"`
	RowColors    map[string]string            `json:"rowColors,omitempty"`
}

// Query executes the unified filter -> search -> sort -> group -> aggregate -> paginate
// pipeline for viewID (or the active/first view when viewID is empty).
func (s *Store) Query(viewID string, opts QueryOptions) *QueryResult {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	s.ensureComputed(now)

	s.mu.RLock()
	db := s.db
	rev := s.revision
	allRecords := s.records
	propMap := s.propMap
	s.mu.RUnlock()

	if db == nil {
		return &QueryResult{Revision: rev}
	}

	var view *ViewConfig
	if viewID != "" {
		view, _ = db.ViewByID(viewID)
	}
	if view == nil && db.ActiveView != "" {
		view, _ = db.ViewByID(db.ActiveView)
	}
	if view == nil && len(db.Views) > 0 {
		view = &db.Views[0]
	}

	effectiveViewID := viewID
	if view != nil {
		effectiveViewID = view.ID
	}

	normSearch := strings.ToLower(strings.TrimSpace(opts.Search))
	cacheable := opts.AdHocFilter == nil && len(opts.AdHocSorts) == 0 && opts.Offset <= 0 && opts.Limit <= 0
	var ckey queryCacheKey
	if cacheable {
		ckey = queryCacheKey{
			revision: rev,
			viewID:   effectiveViewID,
			search:   normSearch,
			dateDay:  now.Format("2006-01-02"),
		}
		s.cacheMu.RLock()
		if cached, ok := s.queryCache[ckey]; ok {
			s.cacheMu.RUnlock()
			return cached
		}
		s.cacheMu.RUnlock()
	}

	res := executeQuery(db, propMap, allRecords, view, rev, now, normSearch, opts)

	if cacheable {
		s.cacheMu.Lock()
		if s.queryCache == nil {
			s.queryCache = make(map[queryCacheKey]*QueryResult)
		}
		// Keep cache bounded per database store.
		if len(s.queryCache) >= 64 {
			clear(s.queryCache)
		}
		s.queryCache[ckey] = res
		s.cacheMu.Unlock()
	}
	return res
}

func executeQuery(
	db *Database,
	propMap map[string]Property,
	records []*Record,
	view *ViewConfig,
	rev uint64,
	now time.Time,
	normSearch string,
	opts QueryOptions,
) *QueryResult {
	var viewFilter *FilterGroup
	var sorts []SortRule
	var groupCfg *GroupConfig
	var subGroupCfg *GroupConfig
	var manualOrder []string
	var tableAggs map[string]AggregationType
	var searchProps []Property

	if view != nil {
		viewFilter = view.Filter
		sorts = view.Sorts
		groupCfg = view.Group
		manualOrder = view.ManualOrder
		if view.Type == ViewBoard && view.Board != nil {
			subGroupCfg = view.Board.SubGroup
		}
		if view.Table != nil {
			tableAggs = view.Table.Aggregations
		}
		searchProps = visiblePropertiesForSearch(db, view, propMap)
	} else {
		searchProps = db.Properties
	}
	if len(opts.AdHocSorts) > 0 {
		sorts = opts.AdHocSorts
	}

	var compiledViewFilter *compiledFilterGroup
	if viewFilter != nil {
		compiledViewFilter = compileFilterGroup(*viewFilter, propMap, now)
	}
	var compiledAdHoc *compiledFilterGroup
	if opts.AdHocFilter != nil {
		compiledAdHoc = compileFilterGroup(*opts.AdHocFilter, propMap, now)
	}

	var searchTokens []string
	if normSearch != "" {
		searchTokens = strings.Fields(normSearch)
	}

	// 1 & 2. Filter and search in a single pass.
	matched := make([]*Record, 0, len(records))
	for _, r := range records {
		if r == nil {
			continue
		}
		if compiledViewFilter != nil && !compiledViewFilter.eval(r) {
			continue
		}
		if compiledAdHoc != nil && !compiledAdHoc.eval(r) {
			continue
		}
		if len(searchTokens) > 0 && !recordMatchesSearch(r, searchProps, searchTokens) {
			continue
		}
		matched = append(matched, r)
	}

	// 3. Sort matched records using pre-extracted keys to keep 10k-row sorts fast.
	sortRecords(matched, sorts, manualOrder, propMap, now.Location())

	totalCount := len(matched)

	// 4. Compute top-level aggregations across all matched records.
	var aggs map[string]AggregationResult
	if len(tableAggs) > 0 {
		aggs = computeAggregationsForSet(matched, tableAggs, propMap)
	}

	// 5. Group (and optionally sub-group) before pagination, or paginate flat rows.
	var groups []RecordGroup
	if groupCfg != nil && groupCfg.PropertyID != "" {
		if gProp, ok := propMap[groupCfg.PropertyID]; ok {
			gCfg := *groupCfg
			if view != nil && view.Type == ViewBoard && view.Board != nil && view.Board.HideEmptyCols {
				gCfg.HideEmpty = true
			}
			groups = buildRecordGroups(matched, gProp, gCfg, tableAggs, propMap, now)
			if subGroupCfg != nil && subGroupCfg.PropertyID != "" {
				if subProp, okSub := propMap[subGroupCfg.PropertyID]; okSub {
					for i := range groups {
						groups[i].SubGroups = buildRecordGroups(groups[i].Rows, subProp, *subGroupCfg, tableAggs, propMap, now)
					}
				}
			}
		}
	}

	// 6. Conditional row/card colors across matched records.
	var rowColors map[string]string
	if view != nil && len(view.Colors) > 0 {
		type compiledColor struct {
			color string
			grp   *compiledFilterGroup
		}
		rules := make([]compiledColor, 0, len(view.Colors))
		for _, cr := range view.Colors {
			if strings.TrimSpace(cr.Color) == "" {
				continue
			}
			rules = append(rules, compiledColor{
				color: cr.Color,
				grp:   compileFilterGroup(cr.Filter, propMap, now),
			})
		}
		if len(rules) > 0 {
			for _, r := range matched {
				if r == nil {
					continue
				}
				for _, rl := range rules {
					if rl.grp.eval(r) {
						if rowColors == nil {
							rowColors = make(map[string]string)
						}
						rowColors[r.ID] = rl.color
						break
					}
				}
			}
		}
	}

	// 7. Pagination slice for Rows.
	rows := matched
	if opts.Offset > 0 || opts.Limit > 0 {
		start := min(max(opts.Offset, 0), len(matched))
		end := len(matched)
		if opts.Limit > 0 && start+opts.Limit < end {
			end = start + opts.Limit
		}
		rows = matched[start:end]
	}

	viewID := ""
	if view != nil {
		viewID = view.ID
	}
	return &QueryResult{
		ViewID:       viewID,
		Revision:     rev,
		TotalCount:   totalCount,
		Rows:         rows,
		Groups:       groups,
		Aggregations: aggs,
		RowColors:    rowColors,
	}
}

func visiblePropertiesForSearch(db *Database, view *ViewConfig, propMap map[string]Property) []Property {
	if view == nil || len(view.Properties) == 0 {
		return db.Properties
	}
	out := make([]Property, 0, len(view.Properties))
	hasTitle := false
	for _, vp := range view.Properties {
		if !vp.Visible {
			continue
		}
		if p, ok := propMap[vp.PropertyID]; ok {
			out = append(out, p)
			if p.Type == PropTitle {
				hasTitle = true
			}
		}
	}
	if !hasTitle {
		if tp := db.TitleProperty(); tp != nil {
			out = append([]Property{*tp}, out...)
		}
	}
	return out
}

func recordMatchesSearch(r *Record, props []Property, tokens []string) bool {
	for _, tok := range tokens {
		if !recordMatchesToken(r, props, tok) {
			return false
		}
	}
	return true
}

func recordMatchesToken(r *Record, props []Property, tok string) bool {
	if matchTextOrPinyin(r.Title, tok) {
		return true
	}
	for _, p := range props {
		if p.Type == PropTitle {
			continue
		}
		val := r.ValueFor(p)
		if val.IsEmpty() {
			continue
		}
		text := FormatValue(p, val)
		if text != "" && matchTextOrPinyin(text, tok) {
			return true
		}
	}
	return false
}

func matchTextOrPinyin(haystack, tok string) bool {
	if haystack == "" {
		return false
	}
	if asciiLowerContains(haystack, tok) {
		return true
	}
	if pinyin.HasHanzi(haystack) {
		_, ok := pinyin.ScoreToken(tok, haystack)
		return ok
	}
	return false
}

func asciiLowerContains(s, lowerNeedle string) bool {
	if len(lowerNeedle) == 0 {
		return true
	}
	if len(s) < len(lowerNeedle) {
		return false
	}
	// Fast check without allocating when strings.ToLower isn't needed for ASCII.
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return strings.Contains(strings.ToLower(s), lowerNeedle)
		}
	}
	for i := 0; i < len(lowerNeedle); i++ {
		if lowerNeedle[i] >= 0x80 {
			return false
		}
	}
	n := len(lowerNeedle)
	for i := 0; i <= len(s)-n; i++ {
		matched := true
		for j := 0; j < n; j++ {
			c := s[i+j]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != lowerNeedle[j] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// --- Compiled Filter Evaluation ---

type compiledFilterRule struct {
	prop        Property
	hasProp     bool
	op          FilterOp
	val         Value
	targetLow   string
	targetsLow  []string
	numTarget   float64
	hasNum      bool
	dateWinFrom time.Time
	dateWinTo   time.Time
	hasDateWin  bool
	loc         *time.Location
}

type compiledFilterGroup struct {
	conj   string
	rules  []compiledFilterRule
	groups []compiledFilterGroup
}

func compileFilterGroup(fg FilterGroup, propMap map[string]Property, now time.Time) *compiledFilterGroup {
	conj := strings.ToLower(strings.TrimSpace(fg.Conjunction))
	if conj != "or" {
		conj = "and"
	}
	out := &compiledFilterGroup{
		conj:   conj,
		rules:  make([]compiledFilterRule, 0, len(fg.Rules)),
		groups: make([]compiledFilterGroup, 0, len(fg.Groups)),
	}
	loc := now.Location()
	if loc == nil {
		loc = time.UTC
	}
	for _, r := range fg.Rules {
		prop, ok := propMap[r.PropertyID]
		cr := compiledFilterRule{
			prop:    prop,
			hasProp: ok,
			op:      r.Op,
			val:     r.Value,
			loc:     loc,
		}
		cr.targetLow = strings.ToLower(strings.TrimSpace(r.Value.PlainString()))
		if r.Value.Kind == ValStringList {
			cr.targetsLow = make([]string, 0, len(r.Value.List))
			for _, x := range r.Value.List {
				if lx := strings.ToLower(strings.TrimSpace(x)); lx != "" {
					cr.targetsLow = append(cr.targetsLow, lx)
				}
			}
		} else if cr.targetLow != "" {
			cr.targetsLow = []string{cr.targetLow}
		}

		if r.Value.Kind == ValNumber {
			cr.numTarget, cr.hasNum = r.Value.Number, true
		} else if r.Value.Kind == ValText {
			cr.numTarget, cr.hasNum = parseNumericText(r.Value.Text)
		}

		if r.Op == OpDateWithin {
			cr.dateWinFrom, cr.dateWinTo, cr.hasDateWin = relativeDateBounds(r.Value.Text, now)
		} else if r.Op == OpDateIs || r.Op == OpDateBefore || r.Op == OpDateAfter || r.Op == OpDateOnOrBefore || r.Op == OpDateOnOrAfter {
			cr.dateWinFrom, cr.dateWinTo, cr.hasDateWin = parseFilterDateTarget(r.Value, loc)
		}
		out.rules = append(out.rules, cr)
	}
	for _, sub := range fg.Groups {
		if compiledSub := compileFilterGroup(sub, propMap, now); compiledSub != nil {
			out.groups = append(out.groups, *compiledSub)
		}
	}
	return out
}

func (g *compiledFilterGroup) eval(r *Record) bool {
	if g == nil || (len(g.rules) == 0 && len(g.groups) == 0) {
		return true
	}
	isOr := g.conj == "or"
	for i := range g.rules {
		matched := g.rules[i].eval(r)
		if isOr && matched {
			return true
		}
		if !isOr && !matched {
			return false
		}
	}
	for i := range g.groups {
		matched := g.groups[i].eval(r)
		if isOr && matched {
			return true
		}
		if !isOr && !matched {
			return false
		}
	}
	return !isOr
}

func (cr *compiledFilterRule) eval(r *Record) bool {
	if !cr.hasProp {
		return true
	}
	v := r.ValueFor(cr.prop)

	switch cr.op {
	case OpIsEmpty:
		return v.IsEmpty()
	case OpIsNotEmpty:
		return !v.IsEmpty()
	case OpChecked:
		return v.Kind == ValBool && v.Bool
	case OpUnchecked:
		return v.Kind != ValBool || !v.Bool
	}

	switch cr.prop.Type {
	case PropNumber:
		return cr.evalNumber(v)
	case PropDate, PropCreatedTime, PropEditedTime:
		return cr.evalDate(v)
	case PropCheckbox:
		b := v.Kind == ValBool && v.Bool
		target := cr.val.Kind == ValBool && cr.val.Bool
		if cr.op == OpNotEquals {
			return b != target
		}
		return b == target
	case PropStatus:
		if cr.op == OpStatusGroupIs || cr.op == OpStatusGroupNot {
			grp := statusGroupForValue(cr.prop, v.Text)
			match := strings.EqualFold(string(grp), strings.TrimSpace(cr.val.Text))
			if cr.op == OpStatusGroupNot {
				return !match
			}
			return match
		}
		return cr.evalTextOrList(v)
	case PropMultiSelect, PropFiles, PropPerson, PropRelation:
		return cr.evalList(v)
	case PropRollup, PropFormula:
		switch v.Kind {
		case ValNumber:
			return cr.evalNumber(v)
		case ValDate:
			return cr.evalDate(v)
		case ValBool:
			b := v.Bool
			target := (cr.val.Kind == ValBool && cr.val.Bool) || strings.EqualFold(cr.targetLow, "true")
			if cr.op == OpNotEquals {
				return b != target
			}
			return b == target
		case ValStringList:
			return cr.evalList(v)
		default:
			return cr.evalTextOrList(v)
		}
	default:
		return cr.evalTextOrList(v)
	}
}

func (cr *compiledFilterRule) evalNumber(v Value) bool {
	if !cr.hasNum {
		return false
	}
	n, ok := numericSortVal(v)
	if !ok {
		return cr.op == OpNotEquals
	}
	switch cr.op {
	case OpEquals:
		return n == cr.numTarget
	case OpNotEquals:
		return n != cr.numTarget
	case OpGT:
		return n > cr.numTarget
	case OpGTE:
		return n >= cr.numTarget
	case OpLT:
		return n < cr.numTarget
	case OpLTE:
		return n <= cr.numTarget
	default:
		return false
	}
}

func (cr *compiledFilterRule) evalTextOrList(v Value) bool {
	if v.Kind == ValStringList {
		return cr.evalList(v)
	}
	text := ""
	if !v.IsEmpty() {
		text = strings.ToLower(FormatValue(cr.prop, v))
	}
	switch cr.op {
	case OpEquals:
		if len(cr.targetsLow) > 1 {
			return slices.Contains(cr.targetsLow, text)
		}
		return text == cr.targetLow
	case OpNotEquals:
		if len(cr.targetsLow) > 1 {
			return !slices.Contains(cr.targetsLow, text)
		}
		return text != cr.targetLow
	case OpContains, OpContainsAny:
		if cr.targetLow == "" {
			return true
		}
		if len(cr.targetsLow) > 1 {
			for _, t := range cr.targetsLow {
				if strings.Contains(text, t) {
					return true
				}
			}
			return false
		}
		return strings.Contains(text, cr.targetLow)
	case OpNotContains, OpNotContainsAny:
		if cr.targetLow == "" {
			return false
		}
		if len(cr.targetsLow) > 1 {
			for _, t := range cr.targetsLow {
				if strings.Contains(text, t) {
					return false
				}
			}
			return true
		}
		return !strings.Contains(text, cr.targetLow)
	case OpStartsWith:
		return strings.HasPrefix(text, cr.targetLow)
	case OpEndsWith:
		return strings.HasSuffix(text, cr.targetLow)
	default:
		return false
	}
}

func (cr *compiledFilterRule) evalList(v Value) bool {
	itemsLow := make([]string, len(v.List))
	for i, s := range v.List {
		itemsLow[i] = strings.ToLower(strings.TrimSpace(s))
	}
	switch cr.op {
	case OpEquals:
		if len(itemsLow) != len(cr.targetsLow) {
			return false
		}
		for _, t := range cr.targetsLow {
			if !slices.Contains(itemsLow, t) {
				return false
			}
		}
		return true
	case OpNotEquals:
		if len(itemsLow) != len(cr.targetsLow) {
			return true
		}
		for _, t := range cr.targetsLow {
			if !slices.Contains(itemsLow, t) {
				return true
			}
		}
		return false
	case OpContains, OpContainsAny:
		for _, t := range cr.targetsLow {
			if slices.Contains(itemsLow, t) {
				return true
			}
		}
		return false
	case OpContainsAll:
		if len(cr.targetsLow) == 0 {
			return true
		}
		for _, t := range cr.targetsLow {
			if !slices.Contains(itemsLow, t) {
				return false
			}
		}
		return true
	case OpNotContains, OpNotContainsAny:
		for _, t := range cr.targetsLow {
			if slices.Contains(itemsLow, t) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func (cr *compiledFilterRule) evalDate(v Value) bool {
	if !cr.hasDateWin || v.IsEmpty() || v.Kind != ValDate {
		return false
	}
	recStart, recEnd, ok := recordDateBounds(v.Date, cr.loc)
	if !ok {
		return false
	}
	switch cr.op {
	case OpDateIs, OpDateWithin:
		// Inclusive interval overlap.
		return !recEnd.Before(cr.dateWinFrom) && !recStart.After(cr.dateWinTo)
	case OpDateBefore:
		return recStart.Before(cr.dateWinFrom)
	case OpDateOnOrBefore:
		return !recStart.After(cr.dateWinTo)
	case OpDateAfter:
		return recEnd.After(cr.dateWinTo)
	case OpDateOnOrAfter:
		return !recEnd.Before(cr.dateWinFrom)
	default:
		return false
	}
}

func parseFilterDateTarget(v Value, loc *time.Location) (start, end time.Time, ok bool) {
	if v.Kind == ValDate && !v.Date.IsEmpty() {
		return recordDateBounds(v.Date, loc)
	}
	if v.Kind == ValText && strings.TrimSpace(v.Text) != "" {
		dv := parseDateTextValue(v.Text)
		if dv.Kind == ValDate {
			return recordDateBounds(dv.Date, loc)
		}
	}
	return time.Time{}, time.Time{}, false
}

func recordDateBounds(dv DateValue, loc *time.Location) (start, end time.Time, ok bool) {
	if dv.TimeZone != "" {
		if loaded, err := time.LoadLocation(dv.TimeZone); err == nil {
			loc = loaded
		}
	}
	if loc == nil {
		loc = time.UTC
	}
	st, stHasTime, ok := fastParseDate(dv.Start, loc)
	if !ok {
		return time.Time{}, time.Time{}, false
	}
	if dv.End != "" {
		et, etHasTime, okEnd := fastParseDate(dv.End, loc)
		if okEnd {
			if !etHasTime {
				et = dayEnd(et)
			}
			return st, et, true
		}
	}
	if !stHasTime {
		return st, dayEnd(st), true
	}
	return st, st, true
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func dayEnd(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 23, 59, 59, 999999999, t.Location())
}

func relativeDateBounds(token string, now time.Time) (start, end time.Time, ok bool) {
	loc := now.Location()
	if loc == nil {
		loc = time.UTC
	}
	now = now.In(loc)
	key := strings.ToLower(strings.TrimSpace(token))
	key = strings.ReplaceAll(key, " ", "_")
	switch key {
	case "today":
		return dayStart(now), dayEnd(now), true
	case "yesterday":
		d := now.AddDate(0, 0, -1)
		return dayStart(d), dayEnd(d), true
	case "tomorrow":
		d := now.AddDate(0, 0, 1)
		return dayStart(d), dayEnd(d), true
	case "past_week", "past_7_days":
		return dayStart(now.AddDate(0, 0, -6)), dayEnd(now), true
	case "next_week", "next_7_days":
		return dayStart(now), dayEnd(now.AddDate(0, 0, 7)), true
	case "this_week":
		offset := (int(now.Weekday()) + 6) % 7 // Monday = 0
		mon := now.AddDate(0, 0, -offset)
		sun := mon.AddDate(0, 0, 6)
		return dayStart(mon), dayEnd(sun), true
	case "past_month", "past_30_days":
		return dayStart(now.AddDate(0, 0, -30)), dayEnd(now), true
	case "next_month", "next_30_days":
		return dayStart(now), dayEnd(now.AddDate(0, 0, 30)), true
	case "this_month":
		y, m, _ := now.Date()
		first := time.Date(y, m, 1, 0, 0, 0, 0, loc)
		last := first.AddDate(0, 1, -1)
		return first, dayEnd(last), true
	case "last_month", "prev_month":
		y, m, _ := now.Date()
		firstThis := time.Date(y, m, 1, 0, 0, 0, 0, loc)
		firstPrev := firstThis.AddDate(0, -1, 0)
		lastPrev := firstThis.AddDate(0, 0, -1)
		return firstPrev, dayEnd(lastPrev), true
	case "next_calendar_month":
		y, m, _ := now.Date()
		firstNext := time.Date(y, m, 1, 0, 0, 0, 0, loc).AddDate(0, 1, 0)
		lastNext := firstNext.AddDate(0, 1, -1)
		return firstNext, dayEnd(lastNext), true
	case "this_year":
		y := now.Year()
		return time.Date(y, 1, 1, 0, 0, 0, 0, loc), time.Date(y, 12, 31, 23, 59, 59, 999999999, loc), true
	case "past_year":
		return dayStart(now.AddDate(-1, 0, 0)), dayEnd(now), true
	case "next_year":
		return dayStart(now), dayEnd(now.AddDate(1, 0, 0)), true
	default:
		return time.Time{}, time.Time{}, false
	}
}

// fastParseDate provides a fast path for YYYY-MM-DD and YYYY-MM-DDTHH:MM without
// layout-scanning overhead on large tables.
func fastParseDate(raw string, loc *time.Location) (t time.Time, hasTime bool, ok bool) {
	if len(raw) == 10 && raw[4] == '-' && raw[7] == '-' {
		y, okY := parseASCIIInt(raw[0:4])
		m, okM := parseASCIIInt(raw[5:7])
		d, okD := parseASCIIInt(raw[8:10])
		if okY && okM && okD && m >= 1 && m <= 12 && d >= 1 && d <= 31 {
			dt := time.Date(y, time.Month(m), d, 0, 0, 0, 0, loc)
			if dt.Month() == time.Month(m) && dt.Day() == d {
				return dt, false, true
			}
		}
	}
	if len(raw) == 16 && raw[4] == '-' && raw[7] == '-' && (raw[10] == 'T' || raw[10] == ' ') && raw[13] == ':' {
		y, okY := parseASCIIInt(raw[0:4])
		m, okM := parseASCIIInt(raw[5:7])
		d, okD := parseASCIIInt(raw[8:10])
		hh, okH := parseASCIIInt(raw[11:13])
		mm, okMin := parseASCIIInt(raw[14:16])
		if okY && okM && okD && okH && okMin && m >= 1 && m <= 12 && d >= 1 && d <= 31 && hh <= 23 && mm <= 59 {
			dt := time.Date(y, time.Month(m), d, hh, mm, 0, 0, loc)
			if dt.Month() == time.Month(m) && dt.Day() == d {
				return dt, true, true
			}
		}
	}
	return ParseDateString(raw, loc)
}

func parseASCIIInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// --- High-Speed Precomputed Sorting ---

type sortCellKey struct {
	empty bool
	num   float64
	nano  int64
	nano2 int64
	opt   int
	boolV bool
	pfx   string
	raw   string
}

func sortRecords(rows []*Record, sorts []SortRule, manualOrder []string, propMap map[string]Property, loc *time.Location) {
	if len(rows) <= 1 {
		return
	}
	if loc == nil {
		loc = time.UTC
	}

	var activeSorts []SortRule
	var sortProps []Property
	for _, sr := range sorts {
		if prop, ok := propMap[sr.PropertyID]; ok {
			activeSorts = append(activeSorts, sr)
			sortProps = append(sortProps, prop)
		}
	}

	var manualRank map[string]int
	if len(activeSorts) == 0 && len(manualOrder) > 0 {
		manualRank = make(map[string]int, len(manualOrder))
		for i, id := range manualOrder {
			manualRank[id] = i
		}
	}

	n := len(rows)
	k := len(activeSorts)
	indices := make([]int, n)
	for i := range indices {
		indices[i] = i
	}

	var keys []sortCellKey
	if k > 0 {
		keys = make([]sortCellKey, n*k)
		for i, r := range rows {
			base := i * k
			for si := 0; si < k; si++ {
				prop := sortProps[si]
				keys[base+si] = extractSortKey(prop, r.ValueFor(prop), loc)
			}
		}
	}

	var ranks []int
	if manualRank != nil {
		ranks = make([]int, n)
		for i, r := range rows {
			if rk, ok := manualRank[r.ID]; ok {
				ranks[i] = rk
			} else {
				ranks[i] = len(manualOrder)
			}
		}
	}

	slices.SortStableFunc(indices, func(a, b int) int {
		if k > 0 {
			ba, bb := a*k, b*k
			for si := 0; si < k; si++ {
				if c := compareSortKeys(sortProps[si].Type, keys[ba+si], keys[bb+si], activeSorts[si].Desc); c != 0 {
					return c
				}
			}
		}
		if ranks != nil && ranks[a] != ranks[b] {
			return cmp.Compare(ranks[a], ranks[b])
		}
		if c := compareCaseFold(rows[a].Title, rows[b].Title); c != 0 {
			return c
		}
		return cmp.Compare(rows[a].ID, rows[b].ID)
	})

	sorted := make([]*Record, n)
	for i, origIdx := range indices {
		sorted[i] = rows[origIdx]
	}
	copy(rows, sorted)
}

func extractSortKey(prop Property, v Value, loc *time.Location) sortCellKey {
	if isEmptyForSort(prop, v) {
		return sortCellKey{empty: true}
	}
	switch prop.Type {
	case PropNumber:
		n, ok := numericSortVal(v)
		if !ok {
			return sortCellKey{empty: true}
		}
		return sortCellKey{num: n}
	case PropCheckbox:
		return sortCellKey{boolV: v.Kind == ValBool && v.Bool}
	case PropDate, PropCreatedTime, PropEditedTime:
		st, _, ok := fastParseDate(v.Date.Start, loc)
		if !ok {
			return sortCellKey{raw: v.Date.Start}
		}
		var en int64 = math.MinInt64
		if v.Date.End != "" {
			if et, _, okEnd := fastParseDate(v.Date.End, loc); okEnd {
				en = et.UnixNano()
			}
		}
		return sortCellKey{nano: st.UnixNano(), nano2: en, raw: v.Date.Start}
	case PropSelect, PropStatus:
		return sortCellKey{
			opt: optionIndex(prop, v.Text),
			raw: v.Text,
		}
	case PropUniqueID:
		pfx, num, ok := splitUniqueID(v.Text)
		if ok {
			return sortCellKey{opt: num, pfx: pfx, raw: v.Text}
		}
		return sortCellKey{opt: -1, raw: v.Text}
	default:
		return sortCellKey{raw: FormatValue(prop, v)}
	}
}

func compareSortKeys(propType PropertyType, a, b sortCellKey, desc bool) int {
	if a.empty && b.empty {
		return 0
	}
	if a.empty {
		return 1
	}
	if b.empty {
		return -1
	}

	var c int
	switch propType {
	case PropNumber:
		c = cmp.Compare(a.num, b.num)
	case PropCheckbox:
		if a.boolV != b.boolV {
			if !a.boolV {
				c = -1
			} else {
				c = 1
			}
		}
	case PropDate, PropCreatedTime, PropEditedTime:
		if a.nano != b.nano {
			c = cmp.Compare(a.nano, b.nano)
		} else if a.nano2 != b.nano2 {
			c = cmp.Compare(a.nano2, b.nano2)
		} else {
			c = cmp.Compare(a.raw, b.raw)
		}
	case PropSelect, PropStatus:
		switch {
		case a.opt >= 0 && b.opt >= 0 && a.opt != b.opt:
			c = cmp.Compare(a.opt, b.opt)
		case a.opt >= 0 && b.opt < 0:
			c = -1
		case a.opt < 0 && b.opt >= 0:
			c = 1
		default:
			c = compareCaseFold(a.raw, b.raw)
		}
	case PropUniqueID:
		if a.opt >= 0 && b.opt >= 0 && strings.EqualFold(a.pfx, b.pfx) {
			c = cmp.Compare(a.opt, b.opt)
		}
		if c == 0 {
			c = compareCaseFold(a.raw, b.raw)
		}
	default:
		c = compareCaseFold(a.raw, b.raw)
	}
	if desc && c != 0 {
		return -c
	}
	return c
}

// compareCaseFold compares two strings case-insensitively (with exact tie-break)
// using a zero-allocation ASCII fast path.
func compareCaseFold(a, b string) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		ca, cb := a[i], b[i]
		if ca >= 0x80 || cb >= 0x80 {
			la, lb := strings.ToLower(a), strings.ToLower(b)
			if c := cmp.Compare(la, lb); c != 0 {
				return c
			}
			return cmp.Compare(a, b)
		}
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return cmp.Compare(ca, cb)
		}
	}
	if len(a) != len(b) {
		return cmp.Compare(len(a), len(b))
	}
	return cmp.Compare(a, b)
}

// --- Grouping ---

func buildRecordGroups(
	rows []*Record,
	prop Property,
	cfg GroupConfig,
	aggs map[string]AggregationType,
	propMap map[string]Property,
	now time.Time,
) []RecordGroup {
	type bucket struct {
		key   string
		label string
		color string
		val   Value
		empty bool
		order int
		rows  []*Record
	}

	buckets := map[string]*bucket{}
	var insertionOrder []string

	ensureBucket := func(key, label, color string, val Value, empty bool, order int) *bucket {
		if b, ok := buckets[key]; ok {
			return b
		}
		b := &bucket{
			key:   key,
			label: label,
			color: color,
			val:   val,
			empty: empty,
			order: order,
		}
		buckets[key] = b
		insertionOrder = append(insertionOrder, key)
		return b
	}

	// Pre-populate canonical buckets when HideEmpty is false so empty columns
	// (such as Kanban status columns) appear in schema order.
	if !cfg.HideEmpty {
		switch prop.Type {
		case PropStatus:
			if strings.EqualFold(cfg.StatusBy, "group") {
				ensureBucket(string(StatusTodo), statusGroupLabel(StatusTodo), "gray", TextValue(string(StatusTodo)), false, 0)
				ensureBucket(string(StatusInProgress), statusGroupLabel(StatusInProgress), "blue", TextValue(string(StatusInProgress)), false, 1)
				ensureBucket(string(StatusComplete), statusGroupLabel(StatusComplete), "green", TextValue(string(StatusComplete)), false, 2)
			} else {
				for i, opt := range prop.Options {
					ensureBucket(opt.Name, opt.Name, opt.Color, TextValue(opt.Name), false, i)
				}
			}
		case PropSelect, PropMultiSelect:
			for i, opt := range prop.Options {
				ensureBucket(opt.Name, opt.Name, opt.Color, TextValue(opt.Name), false, i)
			}
		case PropCheckbox:
			ensureBucket("true", "已勾选", "green", BoolValue(true), false, 0)
			ensureBucket("false", "未勾选", "gray", BoolValue(false), false, 1)
		}
	}

	optColor := make(map[string]string, len(prop.Options))
	optIdx := make(map[string]int, len(prop.Options))
	for i, opt := range prop.Options {
		optColor[strings.ToLower(opt.Name)] = opt.Color
		optIdx[strings.ToLower(opt.Name)] = i
	}

	loc := now.Location()
	if loc == nil {
		loc = time.UTC
	}

	for _, r := range rows {
		v := r.ValueFor(prop)
		switch prop.Type {
		case PropStatus:
			if strings.EqualFold(cfg.StatusBy, "group") {
				if v.IsEmpty() {
					b := ensureBucket("", "无状态", "default", EmptyValue(), true, math.MaxInt)
					b.rows = append(b.rows, r)
				} else {
					grp := statusGroupForValue(prop, v.Text)
					ord := statusGroupOrder(grp)
					b := ensureBucket(string(grp), statusGroupLabel(grp), statusGroupColor(grp), TextValue(string(grp)), false, ord)
					b.rows = append(b.rows, r)
				}
			} else {
				if v.IsEmpty() {
					b := ensureBucket("", "无状态", "default", EmptyValue(), true, math.MaxInt)
					b.rows = append(b.rows, r)
				} else {
					name := v.Text
					if b, ok := buckets[name]; ok {
						b.rows = append(b.rows, r)
					} else {
						low := strings.ToLower(name)
						ord := len(prop.Options)
						if idx, ok := optIdx[low]; ok {
							ord = idx
						}
						b := ensureBucket(name, name, optColor[low], TextValue(name), false, ord)
						b.rows = append(b.rows, r)
					}
				}
			}

		case PropSelect:
			if v.IsEmpty() {
				b := ensureBucket("", "未选择", "default", EmptyValue(), true, math.MaxInt)
				b.rows = append(b.rows, r)
			} else {
				name := v.Text
				if b, ok := buckets[name]; ok {
					b.rows = append(b.rows, r)
				} else {
					low := strings.ToLower(name)
					ord := len(prop.Options)
					if idx, ok := optIdx[low]; ok {
						ord = idx
					}
					b := ensureBucket(name, name, optColor[low], TextValue(name), false, ord)
					b.rows = append(b.rows, r)
				}
			}

		case PropMultiSelect, PropPerson, PropRelation, PropFiles:
			if v.IsEmpty() || len(v.List) == 0 {
				b := ensureBucket("", "空", "default", EmptyValue(), true, math.MaxInt)
				b.rows = append(b.rows, r)
			} else {
				for _, item := range v.List {
					if b, ok := buckets[item]; ok {
						b.rows = append(b.rows, r)
					} else {
						low := strings.ToLower(item)
						ord := len(prop.Options)
						if idx, ok := optIdx[low]; ok {
							ord = idx
						}
						b := ensureBucket(item, item, optColor[low], ListValue(item), false, ord)
						b.rows = append(b.rows, r)
					}
				}
			}

		case PropCheckbox:
			if v.Kind == ValBool && v.Bool {
				b := ensureBucket("true", "已勾选", "green", BoolValue(true), false, 0)
				b.rows = append(b.rows, r)
			} else {
				b := ensureBucket("false", "未勾选", "gray", BoolValue(false), false, 1)
				b.rows = append(b.rows, r)
			}

		case PropDate, PropCreatedTime, PropEditedTime:
			key, label, ord, empty := dateGroupBucket(v, cfg.DateBy, now, loc)
			b := ensureBucket(key, label, "default", TextValue(key), empty, ord)
			b.rows = append(b.rows, r)

		case PropNumber:
			if v.IsEmpty() {
				b := ensureBucket("", "空", "default", EmptyValue(), true, math.MaxInt)
				b.rows = append(b.rows, r)
			} else if n, ok := numericSortVal(v); ok {
				key, label := numberGroupBucket(prop, n, cfg.NumberStep)
				b := ensureBucket(key, label, "default", NumberValue(n), false, int(n))
				b.rows = append(b.rows, r)
			} else {
				b := ensureBucket("", "空", "default", EmptyValue(), true, math.MaxInt)
				b.rows = append(b.rows, r)
			}

		default:
			if v.IsEmpty() {
				b := ensureBucket("", "空", "default", EmptyValue(), true, math.MaxInt)
				b.rows = append(b.rows, r)
			} else {
				s := FormatValue(prop, v)
				b := ensureBucket(s, s, "default", v, false, 0)
				b.rows = append(b.rows, r)
			}
		}
	}

	// Build list respecting HideEmpty, manual Order, and group Sort.
	manualPos := make(map[string]int, len(cfg.Order))
	for i, k := range cfg.Order {
		manualPos[k] = i
	}
	collapsedSet := make(map[string]bool, len(cfg.Collapsed))
	for _, k := range cfg.Collapsed {
		collapsedSet[k] = true
	}

	list := make([]*bucket, 0, len(buckets))
	for _, k := range insertionOrder {
		b := buckets[k]
		if cfg.HideEmpty && len(b.rows) == 0 {
			continue
		}
		list = append(list, b)
	}

	sortMode := strings.ToLower(strings.TrimSpace(cfg.Sort))
	slices.SortStableFunc(list, func(a, b *bucket) int {
		if a.empty != b.empty {
			if a.empty {
				return 1
			}
			return -1
		}
		switch sortMode {
		case "asc", "desc":
			c := cmp.Compare(strings.ToLower(a.label), strings.ToLower(b.label))
			if c == 0 {
				c = cmp.Compare(a.label, b.label)
			}
			if sortMode == "desc" && c != 0 {
				return -c
			}
			return c
		default: // "manual" or default schema/natural order
			if len(manualPos) > 0 {
				ap, aok := manualPos[a.key]
				bp, bok := manualPos[b.key]
				switch {
				case aok && bok && ap != bp:
					return cmp.Compare(ap, bp)
				case aok && !bok:
					return -1
				case !aok && bok:
					return 1
				}
			}
			if a.order != b.order {
				return cmp.Compare(a.order, b.order)
			}
			return cmp.Compare(strings.ToLower(a.label), strings.ToLower(b.label))
		}
	})

	out := make([]RecordGroup, len(list))
	for i, b := range list {
		var groupAggs map[string]AggregationResult
		if len(aggs) > 0 && len(b.rows) > 0 {
			groupAggs = computeAggregationsForSet(b.rows, aggs, propMap)
		}
		out[i] = RecordGroup{
			Key:          b.key,
			Label:        b.label,
			Color:        b.color,
			Value:        b.val,
			Collapsed:    collapsedSet[b.key],
			Count:        len(b.rows),
			Rows:         b.rows,
			Aggregations: groupAggs,
		}
	}
	return out
}

func statusGroupForValue(prop Property, val string) StatusGroup {
	for _, opt := range prop.Options {
		if opt.ID == val || strings.EqualFold(opt.Name, val) {
			if opt.Group != "" {
				return opt.Group
			}
			break
		}
	}
	return StatusTodo
}

func statusGroupOrder(g StatusGroup) int {
	switch g {
	case StatusTodo:
		return 0
	case StatusInProgress:
		return 1
	case StatusComplete:
		return 2
	default:
		return 3
	}
}

func statusGroupLabel(g StatusGroup) string {
	switch g {
	case StatusTodo:
		return "未开始"
	case StatusInProgress:
		return "进行中"
	case StatusComplete:
		return "已完成"
	default:
		return "无状态"
	}
}

func statusGroupColor(g StatusGroup) string {
	switch g {
	case StatusTodo:
		return "gray"
	case StatusInProgress:
		return "blue"
	case StatusComplete:
		return "green"
	default:
		return "default"
	}
}

func dateGroupBucket(v Value, dateBy string, now time.Time, loc *time.Location) (key, label string, order int, empty bool) {
	if v.IsEmpty() || v.Kind != ValDate {
		return "", "无日期", math.MaxInt, true
	}
	t, _, ok := fastParseDate(v.Date.Start, loc)
	if !ok {
		return "", "无日期", math.MaxInt, true
	}
	switch strings.ToLower(strings.TrimSpace(dateBy)) {
	case "year":
		y := t.Format("2006")
		return y, y, t.Year(), false
	case "month":
		m := t.Format("2006-01")
		return m, m, t.Year()*100 + int(t.Month()), false
	case "week":
		offset := (int(t.Weekday()) + 6) % 7
		mon := t.AddDate(0, 0, -offset)
		isoYear, isoWeek := mon.ISOWeek()
		k := mon.Format("2006-01-02")
		return k, fmt.Sprintf("%d-W%02d (%s)", isoYear, isoWeek, k), isoYear*100 + isoWeek, false
	case "relative":
		todayStart := dayStart(now.In(loc))
		todayEnd := dayEnd(now.In(loc))
		wOffset := (int(todayStart.Weekday()) + 6) % 7
		weekStart := todayStart.AddDate(0, 0, -wOffset)
		weekEnd := dayEnd(weekStart.AddDate(0, 0, 6))
		monthStart := time.Date(todayStart.Year(), todayStart.Month(), 1, 0, 0, 0, 0, loc)
		monthEnd := dayEnd(monthStart.AddDate(0, 1, -1))
		switch {
		case t.Before(todayStart):
			return "past", "今天之前", 0, false
		case !t.After(todayEnd):
			return "today", "今天", 1, false
		case !t.After(weekEnd):
			return "this_week", "本周", 2, false
		case !t.After(monthEnd):
			return "this_month", "本月", 3, false
		default:
			return "later", "未来", 4, false
		}
	default: // "day"
		d := t.Format("2006-01-02")
		return d, d, t.Year()*10000 + int(t.Month())*100 + t.Day(), false
	}
}

func numberGroupBucket(prop Property, n float64, step float64) (key, label string) {
	if step > 0 {
		lo := math.Floor(n/step) * step
		hi := lo + step
		k := fmt.Sprintf("%s-%s", formatFloatCompact(lo), formatFloatCompact(hi))
		return k, k
	}
	s := formatNumberValue(prop.Number, n)
	return s, s
}

// --- Aggregations ---

func computeAggregationsForSet(records []*Record, aggs map[string]AggregationType, propMap map[string]Property) map[string]AggregationResult {
	out := make(map[string]AggregationResult, len(aggs))
	for propID, aggType := range aggs {
		if aggType == "" || aggType == AggNone {
			continue
		}
		if prop, ok := propMap[propID]; ok {
			out[propID] = ComputeAggregation(prop, records, aggType)
		}
	}
	return out
}

// ComputeAggregation calculates agg over records for a single property.
func ComputeAggregation(prop Property, records []*Record, agg AggregationType) AggregationResult {
	res := AggregationResult{Type: agg}
	if agg == "" || agg == AggNone {
		return res
	}
	total := len(records)

	switch agg {
	case AggCountAll:
		res.Number = float64(total)
		res.Display = fmt.Sprintf("%d", total)
		res.HasValue = true
		return res

	case AggCountEmpty:
		cnt := 0
		for _, r := range records {
			if r.ValueFor(prop).IsEmpty() {
				cnt++
			}
		}
		res.Number = float64(cnt)
		res.Display = fmt.Sprintf("%d", cnt)
		res.HasValue = true
		return res

	case AggCountNotEmpty:
		cnt := 0
		for _, r := range records {
			if !r.ValueFor(prop).IsEmpty() {
				cnt++
			}
		}
		res.Number = float64(cnt)
		res.Display = fmt.Sprintf("%d", cnt)
		res.HasValue = true
		return res

	case AggCountValues:
		cnt := 0
		for _, r := range records {
			v := r.ValueFor(prop)
			if v.IsEmpty() {
				continue
			}
			if v.Kind == ValStringList {
				cnt += len(v.List)
			} else {
				cnt++
			}
		}
		res.Number = float64(cnt)
		res.Display = fmt.Sprintf("%d", cnt)
		res.HasValue = true
		return res

	case AggCountUnique:
		seen := make(map[string]struct{})
		for _, r := range records {
			v := r.ValueFor(prop)
			if v.IsEmpty() {
				continue
			}
			if v.Kind == ValStringList {
				for _, item := range v.List {
					seen[item] = struct{}{}
				}
			} else {
				seen[FormatValue(prop, v)] = struct{}{}
			}
		}
		res.Number = float64(len(seen))
		res.Display = fmt.Sprintf("%d", len(seen))
		res.HasValue = true
		return res

	case AggPercentEmpty, AggPercentNotEmpty:
		if total == 0 {
			res.Display = "0%"
			res.HasValue = true
			return res
		}
		emptyCnt := 0
		for _, r := range records {
			if r.ValueFor(prop).IsEmpty() {
				emptyCnt++
			}
		}
		cnt := emptyCnt
		if agg == AggPercentNotEmpty {
			cnt = total - emptyCnt
		}
		pct := float64(cnt) * 100 / float64(total)
		res.Number = pct
		res.Display = formatPercentAgg(pct)
		res.HasValue = true
		return res

	case AggChecked, AggUnchecked, AggPercentChecked, AggPercentUnchecked:
		checked := 0
		for _, r := range records {
			v := r.ValueFor(prop)
			if v.Kind == ValBool && v.Bool {
				checked++
			}
		}
		unchecked := total - checked
		switch agg {
		case AggChecked:
			res.Number = float64(checked)
			res.Display = fmt.Sprintf("%d", checked)
		case AggUnchecked:
			res.Number = float64(unchecked)
			res.Display = fmt.Sprintf("%d", unchecked)
		case AggPercentChecked:
			if total > 0 {
				res.Number = float64(checked) * 100 / float64(total)
			}
			res.Display = formatPercentAgg(res.Number)
		case AggPercentUnchecked:
			if total > 0 {
				res.Number = float64(unchecked) * 100 / float64(total)
			}
			res.Display = formatPercentAgg(res.Number)
		}
		res.HasValue = true
		return res

	case AggSum, AggAverage, AggMedian, AggMin, AggMax, AggRange:
		var nums []float64
		for _, r := range records {
			v := r.ValueFor(prop)
			if v.IsEmpty() {
				continue
			}
			if n, ok := numericSortVal(v); ok {
				nums = append(nums, n)
			}
		}
		if len(nums) == 0 {
			return res
		}
		res.HasValue = true
		switch agg {
		case AggSum:
			sum := 0.0
			for _, n := range nums {
				sum += n
			}
			res.Number = sum
		case AggAverage:
			sum := 0.0
			for _, n := range nums {
				sum += n
			}
			res.Number = sum / float64(len(nums))
		case AggMedian:
			slices.Sort(nums)
			mid := len(nums) / 2
			if len(nums)%2 == 1 {
				res.Number = nums[mid]
			} else {
				res.Number = (nums[mid-1] + nums[mid]) / 2
			}
		case AggMin:
			res.Number = slices.Min(nums)
		case AggMax:
			res.Number = slices.Max(nums)
		case AggRange:
			res.Number = slices.Max(nums) - slices.Min(nums)
		}
		if agg == AggAverage || agg == AggMedian {
			res.Display = formatAggNumber(prop, res.Number)
		} else {
			res.Display = formatNumberValue(prop.Number, res.Number)
		}
		return res

	case AggEarliestDate, AggLatestDate, AggDateRange:
		var minT, maxT time.Time
		var minStr, maxStr string
		hasAny := false
		for _, r := range records {
			v := r.ValueFor(prop)
			if v.IsEmpty() || v.Kind != ValDate {
				continue
			}
			st, _, ok := fastParseDate(v.Date.Start, time.UTC)
			if !ok {
				continue
			}
			et := st
			eStr := v.Date.Start
			if v.Date.End != "" {
				if parsedEnd, _, okEnd := fastParseDate(v.Date.End, time.UTC); okEnd {
					et = parsedEnd
					eStr = v.Date.End
				}
			}
			if !hasAny {
				minT, maxT = st, et
				minStr, maxStr = v.Date.Start, eStr
				hasAny = true
				continue
			}
			if st.Before(minT) {
				minT = st
				minStr = v.Date.Start
			}
			if et.After(maxT) {
				maxT = et
				maxStr = eStr
			}
		}
		if !hasAny {
			return res
		}
		res.HasValue = true
		switch agg {
		case AggEarliestDate:
			res.Date = minStr
			res.Display = minStr
		case AggLatestDate:
			res.Date = maxStr
			res.Display = maxStr
		case AggDateRange:
			days := int(math.Round(maxT.Sub(minT).Hours() / 24))
			res.Number = float64(days)
			res.Date = minStr + " → " + maxStr
			res.Display = fmt.Sprintf("%d 天", days)
		}
		return res
	}
	return res
}

func formatPercentAgg(pct float64) string {
	if pct == math.Trunc(pct) {
		return fmt.Sprintf("%.0f%%", pct)
	}
	return fmt.Sprintf("%.1f%%", pct)
}

func formatAggNumber(prop Property, n float64) string {
	if prop.Number != nil && prop.Number.Format != "" {
		return formatNumberValue(prop.Number, n)
	}
	if n == math.Trunc(n) {
		return formatFloatCompact(n)
	}
	return strconv.FormatFloat(math.Round(n*100)/100, 'f', -1, 64)
}
