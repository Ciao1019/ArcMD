package database

import (
	"slices"
	"strings"
	"time"
)

const maxComputedEvalDepth = 4

type evalContext struct {
	store      *Store
	db         *Database
	propMap    map[string]Property
	records    []*Record
	now        time.Time
	evaluating map[string]bool // "<recID>\x00<propID>" cycle guard
	cache      map[string]Value
}

func newEvalContext(s *Store, db *Database, propMap map[string]Property, records []*Record, now time.Time) *evalContext {
	if now.IsZero() {
		now = time.Now()
	}
	return &evalContext{
		store:      s,
		db:         db,
		propMap:    propMap,
		records:    records,
		now:        now,
		evaluating: make(map[string]bool),
		cache:      make(map[string]Value),
	}
}

func (ctx *evalContext) propValue(rec *Record, prop Property, depth int) Value {
	if rec == nil {
		return EmptyValue()
	}
	if prop.Type != PropRollup && prop.Type != PropFormula {
		return rec.ValueFor(prop)
	}
	if depth > maxComputedEvalDepth {
		return ErrorValue("#CYCLE!")
	}
	ckey := rec.ID + "\x00" + prop.ID
	if v, ok := ctx.cache[ckey]; ok {
		return v
	}
	if ctx.evaluating[ckey] {
		return ErrorValue("#CYCLE!")
	}
	ctx.evaluating[ckey] = true
	var val Value
	if prop.Type == PropRollup {
		val = evaluateRollupWithContext(ctx, rec, prop, depth)
	} else {
		expr := ""
		if prop.Formula != nil {
			expr = prop.Formula.Expression
		}
		val = evaluateFormulaWithContext(ctx, rec, expr, depth)
	}
	delete(ctx.evaluating, ckey)
	ctx.cache[ckey] = val
	return val
}

// EvaluateRollup computes the aggregated Value of a PropRollup property on rec.
func EvaluateRollup(s *Store, rec *Record, prop Property, now time.Time, depth int) Value {
	if s == nil || rec == nil || prop.Type != PropRollup {
		return EmptyValue()
	}
	s.mu.RLock()
	db := s.db
	propMap := s.propMap
	records := s.records
	s.mu.RUnlock()
	ctx := newEvalContext(s, db, propMap, records, now)
	return evaluateRollupWithContext(ctx, rec, prop, depth)
}

func evaluateRollupWithContext(ctx *evalContext, rec *Record, prop Property, depth int) Value {
	if depth > maxComputedEvalDepth {
		return ErrorValue("#CYCLE!")
	}
	if ctx == nil || rec == nil || prop.Rollup == nil || prop.Rollup.RelationPropID == "" {
		return EmptyValue()
	}
	relProp, ok := ctx.propMap[prop.Rollup.RelationPropID]
	if !ok || relProp.Type != PropRelation {
		return EmptyValue()
	}

	relVal := rec.ValueFor(relProp)
	refs := relVal.List
	agg := prop.Rollup.Calculate
	if agg == "" {
		agg = AggShowOriginal
	}

	targetDBName := ""
	if relProp.Relation != nil {
		targetDBName = relProp.Relation.TargetDB
	}

	var targetDB *Database
	var targetRecs []*Record
	isSelf := ctx.store == nil || ctx.store.isSelfTargetLocked(targetDBName)
	if isSelf {
		targetDB = ctx.db
		targetRecs = ctx.records
	} else {
		var err error
		targetDB, targetRecs, err = ctx.store.ResolveTargetDatabase(targetDBName)
		if err != nil {
			return ErrorValue("#REF!")
		}
	}
	if targetDB == nil {
		return EmptyValue()
	}

	var targetProp Property
	hasTargetProp := false
	if prop.Rollup.TargetPropID != "" {
		if tp, idx := targetDB.PropertyByID(prop.Rollup.TargetPropID); idx >= 0 {
			targetProp = *tp
			hasTargetProp = true
		}
	}
	if !hasTargetProp && targetDB.TitleProperty() != nil {
		targetProp = *targetDB.TitleProperty()
		hasTargetProp = true
	}
	if !hasTargetProp {
		return EmptyValue()
	}

	linked := make([]*Record, 0, len(refs))
	for _, ref := range refs {
		if tr := MatchRelationRecord(targetRecs, ref); tr != nil {
			linked = append(linked, tr)
		}
	}

	if len(linked) == 0 {
		switch agg {
		case AggCountAll, AggCountValues, AggCountUnique, AggCountEmpty, AggCountNotEmpty,
			AggChecked, AggUnchecked, AggSum:
			return NumberValue(0)
		case AggPercentEmpty, AggPercentNotEmpty, AggPercentChecked, AggPercentUnchecked:
			return NumberValue(0)
		default:
			return EmptyValue()
		}
	}

	// Build a lightweight target evaluation context if linked records contain computed target fields.
	var tCtx *evalContext
	needCompute := targetProp.Type == PropRollup || targetProp.Type == PropFormula
	evalRecs := linked
	if needCompute {
		if isSelf {
			tCtx = ctx
		} else {
			tPropMap := make(map[string]Property, len(targetDB.Properties))
			for _, p := range targetDB.Properties {
				tPropMap[p.ID] = p
			}
			tCtx = newEvalContext(ctx.store, targetDB, tPropMap, targetRecs, ctx.now)
		}
		evalRecs = make([]*Record, len(linked))
		for i, tr := range linked {
			cp := tr.Clone()
			if cp.Computed == nil {
				cp.Computed = make(map[string]Value)
			}
			cp.Computed[targetProp.ID] = tCtx.propValue(tr, targetProp, depth+1)
			evalRecs[i] = cp
		}
	}

	switch agg {
	case AggShowOriginal:
		var vals []string
		for _, r := range evalRecs {
			v := r.ValueFor(targetProp)
			if v.IsEmpty() {
				continue
			}
			if s := FormatValue(targetProp, v); s != "" {
				vals = append(vals, s)
			}
		}
		if len(vals) == 0 {
			return EmptyValue()
		}
		return ListValue(vals...)

	case AggShowUnique:
		var vals []string
		seen := make(map[string]bool)
		for _, r := range evalRecs {
			v := r.ValueFor(targetProp)
			if v.IsEmpty() {
				continue
			}
			if v.Kind == ValStringList {
				for _, item := range v.List {
					low := strings.ToLower(item)
					if item != "" && !seen[low] {
						seen[low] = true
						vals = append(vals, item)
					}
				}
			} else if s := FormatValue(targetProp, v); s != "" {
				low := strings.ToLower(s)
				if !seen[low] {
					seen[low] = true
					vals = append(vals, s)
				}
			}
		}
		if len(vals) == 0 {
			return EmptyValue()
		}
		return ListValue(vals...)
	}

	res := ComputeAggregation(targetProp, evalRecs, agg)
	if !res.HasValue {
		return EmptyValue()
	}
	switch agg {
	case AggEarliestDate, AggLatestDate:
		if res.Date == "" {
			return EmptyValue()
		}
		return DateRangeValue(res.Date, "", false)
	case AggDateRange:
		if res.Display == "" {
			return EmptyValue()
		}
		return TextValue(res.Display)
	default:
		if prop.Number != nil && strings.EqualFold(prop.Number.Format, "percent") &&
			strings.HasPrefix(string(agg), "percent_") {
			return NumberValue(res.Number / 100.0)
		}
		return NumberValue(res.Number)
	}
}

func resolveLinkedRecords(ctx *evalContext, rec *Record, relProp Property) (*Database, []*Record) {
	if ctx == nil || rec == nil || relProp.Type != PropRelation {
		return nil, nil
	}
	refs := rec.ValueFor(relProp).List
	if len(refs) == 0 {
		return nil, nil
	}
	targetDBName := ""
	if relProp.Relation != nil {
		targetDBName = relProp.Relation.TargetDB
	}
	var targetDB *Database
	var targetRecs []*Record
	if ctx.store == nil || ctx.store.isSelfTargetLocked(targetDBName) {
		targetDB = ctx.db
		targetRecs = ctx.records
	} else {
		var err error
		targetDB, targetRecs, err = ctx.store.ResolveTargetDatabase(targetDBName)
		if err != nil {
			return nil, nil
		}
	}
	if targetDB == nil {
		return nil, nil
	}
	var matched []*Record
	seenID := map[string]bool{}
	for _, ref := range refs {
		if tr := MatchRelationRecord(targetRecs, ref); tr != nil && !seenID[tr.ID] {
			seenID[tr.ID] = true
			matched = append(matched, tr)
		}
	}
	_ = slices.Clip(matched)
	return targetDB, matched
}
