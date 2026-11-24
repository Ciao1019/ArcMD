package database

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/vault"
)

// SpaceDBInfo describes an amphibious .db.md database discovered in a space.
type SpaceDBInfo struct {
	RelPath    string     `json:"relPath"`
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Icon       string     `json:"icon,omitempty"`
	Properties []Property `json:"properties,omitempty"`
}

// RelationCandidate represents one pickable record inside a target database.
type RelationCandidate struct {
	Ref   string `json:"ref"`
	Title string `json:"title"`
	Path  string `json:"path,omitempty"`
	Icon  string `json:"icon,omitempty"`
}

// RelationResolver resolves an external target database's schema and records.
// Implementations must avoid re-entering the caller Store's mutex.
type RelationResolver interface {
	ResolveDatabase(spaceRoot, targetDB string) (*Database, []*Record, error)
}

// RelationSyncer propagates symmetric link edits to another open or disk-backed database.
type RelationSyncer interface {
	SyncExternalRelation(spaceRoot, targetDB, syncedPropID, sourceRef string, addTargets, removeTargets []string) ([]string, error)
}

type extDBCacheEntry struct {
	rel     string
	modTime time.Time
	size    int64
	db      *Database
	records []*Record
}

type pendingExternalRelationSync struct {
	targetDB      string
	syncedPropID  string
	sourceRef     string
	addTargets    []string
	removeTargets []string
}

// ListSpaceDatabases discovers all *.db.md database files inside spaceRoot.
func ListSpaceDatabases(spaceRoot string) ([]SpaceDBInfo, error) {
	if strings.TrimSpace(spaceRoot) == "" {
		return nil, nil
	}
	var out []SpaceDBInfo
	err := filepath.WalkDir(spaceRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if p != spaceRoot {
				name := d.Name()
				if strings.HasPrefix(name, ".") || name == "assets" || name == "node_modules" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !vault.IsDatabase(d.Name()) || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		rel, err := filepath.Rel(spaceRoot, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.Type()&fs.ModeSymlink != 0 {
			if _, rerr := vault.Resolve(spaceRoot, rel); rerr != nil {
				return nil
			}
		}
		raw, rerr := vault.Read(spaceRoot, rel)
		if rerr != nil {
			return nil
		}
		db, derr := DecodeDatabaseMarkdown(raw)
		if derr != nil || db == nil {
			return nil
		}
		out = append(out, SpaceDBInfo{
			RelPath:    rel,
			ID:         db.ID,
			Title:      db.Title,
			Icon:       db.Icon,
			Properties: append([]Property(nil), db.Properties...),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if strings.ToLower(out[i].Title) != strings.ToLower(out[j].Title) {
			return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
		}
		return out[i].RelPath < out[j].RelPath
	})
	return out, nil
}

// RecordLinkRef returns the canonical stable reference string written into a
// Relation cell / frontmatter entry for rec.
func RecordLinkRef(rec *Record) string {
	if rec == nil {
		return ""
	}
	if rec.Path != "" {
		return strings.TrimSuffix(filepath.ToSlash(rec.Path), ".md")
	}
	if rec.Title != "" {
		return rec.Title
	}
	return rec.ID
}

// MatchRelationRecord resolves ref against records using path, link target,
// ID, filename stem, and page title (exact match before case-insensitive match).
func MatchRelationRecord(records []*Record, ref string) *Record {
	clean := strings.TrimSpace(ref)
	if strings.HasPrefix(clean, "[[") && strings.HasSuffix(clean, "]]") {
		clean = clean[2 : len(clean)-2]
		if target, _, hasAlias := strings.Cut(clean, "|"); hasAlias {
			clean = target
		}
		clean = strings.TrimSpace(clean)
	}
	if clean == "" {
		return nil
	}
	cleanSlash := strings.TrimSuffix(filepath.ToSlash(clean), ".md")
	baseClean := path.Base(cleanSlash)

	for _, r := range records {
		if r == nil {
			continue
		}
		stem := strings.TrimSuffix(r.Path, ".md")
		if r.Path == clean || stem == cleanSlash || r.ID == clean || r.Title == clean || (r.Path != "" && vault.Title(r.Path) == baseClean) {
			return r
		}
	}
	for _, r := range records {
		if r == nil {
			continue
		}
		stem := strings.TrimSuffix(r.Path, ".md")
		if strings.EqualFold(r.Path, clean) || strings.EqualFold(stem, cleanSlash) ||
			strings.EqualFold(r.ID, clean) || strings.EqualFold(r.Title, clean) ||
			(r.Path != "" && strings.EqualFold(vault.Title(r.Path), baseClean)) {
			return r
		}
	}
	return nil
}

// SetRelationResolver attaches a host resolver for cross-database reads.
func (s *Store) SetRelationResolver(r RelationResolver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.relationResolver = r
	clear(s.extCache)
	s.computedRev = 0
}

// HasRelationResolver reports whether a host relation resolver is attached.
func (s *Store) HasRelationResolver() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.relationResolver != nil
}

// SetRelationSyncer attaches a host syncer for cross-database two-way relations.
func (s *Store) SetRelationSyncer(syncer RelationSyncer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.relationSyncer = syncer
}

func (s *Store) isSelfTargetLocked(targetDB string) bool {
	targetDB = strings.TrimSpace(targetDB)
	if targetDB == "" {
		return true
	}
	if filepath.ToSlash(targetDB) == s.dbRel && s.dbRel != "" {
		return true
	}
	return s.db != nil && targetDB == s.db.ID
}

// ResolveTargetDatabase loads the target database schema and its records.
// Empty targetDB, matching s.dbRel, or matching s.db.ID resolves to s itself.
func (s *Store) ResolveTargetDatabase(targetDB string) (*Database, []*Record, error) {
	s.mu.RLock()
	self := s.isSelfTargetLocked(targetDB)
	if self {
		db := s.db
		recs := s.records
		s.mu.RUnlock()
		return db, recs, nil
	}
	spaceRoot := s.spaceRoot
	resolver := s.relationResolver
	cached, hasCached := s.extCache[targetDB]
	s.mu.RUnlock()

	if resolver != nil {
		return resolver.ResolveDatabase(spaceRoot, targetDB)
	}
	if spaceRoot == "" {
		return nil, nil, fmt.Errorf("database: empty space root resolving %q", targetDB)
	}

	targetRel := filepath.ToSlash(strings.TrimSpace(targetDB))
	if !vault.IsDatabase(targetRel) {
		if infos, err := ListSpaceDatabases(spaceRoot); err == nil {
			for _, info := range infos {
				if info.ID == targetDB || info.Title == targetDB || strings.TrimSuffix(info.RelPath, vault.ExtDatabase) == targetRel {
					targetRel = info.RelPath
					break
				}
			}
		}
	}
	if targetRel == "" {
		return nil, nil, fmt.Errorf("database: relation target %q not found", targetDB)
	}

	full, err := vault.Resolve(spaceRoot, targetRel)
	if err != nil {
		return nil, nil, err
	}
	fi, err := os.Stat(full)
	if err != nil {
		return nil, nil, err
	}
	if hasCached && cached.modTime.Equal(fi.ModTime()) && cached.size == fi.Size() && cached.db != nil {
		return cached.db, cached.records, nil
	}

	extStore, err := Open(spaceRoot, targetRel, nil)
	if err != nil {
		return nil, nil, err
	}
	tdb := extStore.db
	trecs := extStore.records

	s.mu.Lock()
	if s.extCache == nil {
		s.extCache = make(map[string]extDBCacheEntry)
	}
	if len(s.extCache) >= 32 {
		clear(s.extCache)
	}
	s.extCache[targetDB] = extDBCacheEntry{
		rel:     targetRel,
		modTime: fi.ModTime(),
		size:    fi.Size(),
		db:      tdb,
		records: trecs,
	}
	s.mu.Unlock()
	return tdb, trecs, nil
}

// RelationCandidates lists pickable records for a PropRelation property.
func (s *Store) RelationCandidates(prop Property) []RelationCandidate {
	target := ""
	if prop.Relation != nil {
		target = prop.Relation.TargetDB
	}
	_, recs, err := s.ResolveTargetDatabase(target)
	if err != nil || len(recs) == 0 {
		return nil
	}
	out := make([]RelationCandidate, 0, len(recs))
	for _, r := range recs {
		if r == nil {
			continue
		}
		ref := RecordLinkRef(r)
		if ref == "" {
			continue
		}
		title := r.Title
		if title == "" {
			title = ref
		}
		out = append(out, RelationCandidate{
			Ref:   ref,
			Title: title,
			Path:  r.Path,
			Icon:  r.Icon,
		})
	}
	return out
}

// SetupTwoWayRelation configures propID to point at targetDBRel and ensures a
// paired reciprocal PropRelation property exists on the target database.
func (s *Store) SetupTwoWayRelation(propID, targetDBRel, backPropName string) (string, error) {
	s.mu.Lock()
	if s.db == nil {
		s.mu.Unlock()
		return "", fmt.Errorf("database: nil schema")
	}
	srcPropPtr, pIdx := s.db.PropertyByID(propID)
	if pIdx < 0 {
		s.mu.Unlock()
		return "", fmt.Errorf("database: property %q not found", propID)
	}
	if srcPropPtr.Type != PropRelation {
		s.mu.Unlock()
		return "", fmt.Errorf("database: property %q is not a relation", propID)
	}
	srcDBRel := s.dbRel
	if srcDBRel == "" {
		srcDBRel = s.db.ID
	}
	targetClean := filepath.ToSlash(strings.TrimSpace(targetDBRel))
	isSelf := targetClean == "" || s.isSelfTargetLocked(targetClean)
	if isSelf {
		targetClean = srcDBRel
	}
	backName := strings.TrimSpace(backPropName)
	if backName == "" {
		backName = "关联的" + s.db.Title
	}

	if isSelf {
		// Check existing synced self-relation column first.
		existingID := ""
		if srcPropPtr.Relation != nil && srcPropPtr.Relation.SyncedPropID != "" {
			if bp, bIdx := s.db.PropertyByID(srcPropPtr.Relation.SyncedPropID); bIdx >= 0 && bp.Type == PropRelation {
				existingID = bp.ID
			}
		}
		if existingID == "" {
			backProp := Property{
				ID:   "prop_" + NewID()[:8],
				Key:  uniqueSchemaPropKey(s.db, backName),
				Name: backName,
				Type: PropRelation,
				Relation: &RelationConfig{
					TargetDB:     targetClean,
					SyncedPropID: propID,
				},
				Width: 140,
			}
			s.mu.Unlock()
			if err := s.AddProperty(backProp); err != nil {
				return "", err
			}
			existingID = backProp.ID
			s.mu.Lock()
		}
		updated := srcPropPtr.Clone()
		if updated.Relation == nil {
			updated.Relation = &RelationConfig{}
		}
		updated.Relation.TargetDB = targetClean
		updated.Relation.SyncedPropID = existingID
		s.mu.Unlock()
		if _, err := s.UpdateProperty(updated); err != nil {
			return "", err
		}
		return existingID, nil
	}

	spaceRoot := s.spaceRoot
	srcRelCfg := RelationConfig{}
	if srcPropPtr.Relation != nil {
		srcRelCfg = *srcPropPtr.Relation
	}
	s.mu.Unlock()

	if spaceRoot == "" {
		return "", fmt.Errorf("database: empty space root for cross-database relation")
	}
	rawTarget, err := vault.Read(spaceRoot, targetClean)
	if err != nil {
		return "", err
	}
	targetDB, err := DecodeDatabaseMarkdown(rawTarget)
	if err != nil {
		return "", err
	}

	syncedID := ""
	if srcRelCfg.SyncedPropID != "" {
		if bp, idx := targetDB.PropertyByID(srcRelCfg.SyncedPropID); idx >= 0 && bp.Type == PropRelation {
			syncedID = bp.ID
			targetDB.Properties[idx].Relation = &RelationConfig{
				TargetDB:     srcDBRel,
				SyncedPropID: propID,
			}
		}
	}
	if syncedID == "" {
		backProp := Property{
			ID:   "prop_" + NewID()[:8],
			Key:  uniqueSchemaPropKey(targetDB, backName),
			Name: backName,
			Type: PropRelation,
			Relation: &RelationConfig{
				TargetDB:     srcDBRel,
				SyncedPropID: propID,
			},
			Width: 140,
		}
		targetDB.Properties = append(targetDB.Properties, backProp)
		for vi := range targetDB.Views {
			targetDB.Views[vi].Properties = append(targetDB.Views[vi].Properties, ViewProperty{
				PropertyID: backProp.ID,
				Visible:    true,
				Width:      backProp.Width,
			})
		}
		syncedID = backProp.ID
	}

	targetDir := path.Dir(targetClean)
	if targetDir == "." {
		targetDir = ""
	}
	encodedTarget, err := EncodeDatabaseMarkdownRelative(targetDir, targetDB, nil)
	if err != nil {
		return "", err
	}
	if err := vault.Write(spaceRoot, targetClean, encodedTarget); err != nil {
		return "", err
	}

	// Clear cached external target so subsequent resolution picks up the new schema.
	s.mu.Lock()
	delete(s.extCache, targetClean)
	delete(s.extCache, targetDB.ID)
	curProp, curIdx := s.db.PropertyByID(propID)
	if curIdx < 0 {
		s.mu.Unlock()
		return "", fmt.Errorf("database: property %q disappeared", propID)
	}
	updated := curProp.Clone()
	if updated.Relation == nil {
		updated.Relation = &RelationConfig{}
	}
	updated.Relation.TargetDB = targetClean
	updated.Relation.SyncedPropID = syncedID
	s.mu.Unlock()

	if _, err := s.UpdateProperty(updated); err != nil {
		return "", err
	}
	return syncedID, nil
}

func uniqueSchemaPropKey(db *Database, base string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(base)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' {
			b.WriteByte('_')
		}
	}
	cand := strings.Trim(b.String(), "_")
	if cand == "" {
		cand = "rel_" + NewID()[:6]
	}
	used := map[string]bool{}
	if db != nil {
		for _, p := range db.Properties {
			used[strings.ToLower(p.Key)] = true
		}
	}
	if !used[cand] {
		return cand
	}
	for i := 2; ; i++ {
		next := fmt.Sprintf("%s_%d", cand, i)
		if !used[next] {
			return next
		}
	}
}

func diffStringSets(before, after []string) (added, removed []string) {
	inBefore := make(map[string]bool, len(before))
	for _, x := range before {
		if s := strings.TrimSpace(x); s != "" {
			inBefore[s] = true
		}
	}
	inAfter := make(map[string]bool, len(after))
	for _, x := range after {
		if s := strings.TrimSpace(x); s != "" {
			if !inBefore[s] && !slices.Contains(added, s) {
				added = append(added, s)
			}
			inAfter[s] = true
		}
	}
	for _, x := range before {
		if s := strings.TrimSpace(x); s != "" && !inAfter[s] && !slices.Contains(removed, s) {
			removed = append(removed, s)
		}
	}
	return added, removed
}

// ApplySymmetricRelationDelta updates syncedPropID links on matching target records
// to add or remove sourceRef without triggering reciprocal relation sync back.
func (s *Store) ApplySymmetricRelationDelta(syncedPropID, sourceRef string, addTargets, removeTargets []string) ([]string, error) {
	s.mu.Lock()
	if s.db == nil || strings.TrimSpace(sourceRef) == "" {
		s.mu.Unlock()
		return nil, nil
	}
	prop, ok := s.propMap[syncedPropID]
	if !ok || prop.Type != PropRelation {
		s.mu.Unlock()
		return nil, fmt.Errorf("database: synced relation property %q not found", syncedPropID)
	}
	var edits []CellEdit
	editedRec := map[string]bool{}
	plan := func(ref string, add bool) {
		tr := MatchRelationRecord(s.records, ref)
		if tr == nil || editedRec[tr.ID] {
			return
		}
		cur := slices.Clone(tr.ValueFor(prop).List)
		hasIdx := slices.IndexFunc(cur, func(x string) bool { return strings.EqualFold(x, sourceRef) })
		if add {
			if hasIdx >= 0 {
				return
			}
			if prop.Relation != nil && prop.Relation.LimitOne {
				cur = []string{sourceRef}
			} else {
				cur = append(cur, sourceRef)
			}
		} else {
			if hasIdx < 0 {
				return
			}
			cur = slices.Delete(cur, hasIdx, hasIdx+1)
		}
		editedRec[tr.ID] = true
		edits = append(edits, CellEdit{
			RecordID: tr.ID,
			PropID:   syncedPropID,
			Value:    ListValue(cur...),
		})
	}
	for _, t := range addTargets {
		plan(t, true)
	}
	for _, t := range removeTargets {
		plan(t, false)
	}
	s.mu.Unlock()

	if len(edits) == 0 {
		return nil, nil
	}
	return s.setCellsBatchInternal(edits, false)
}

func (s *Store) runExternalRelationSync(syncs []pendingExternalRelationSync) {
	if len(syncs) == 0 {
		return
	}
	s.mu.RLock()
	spaceRoot := s.spaceRoot
	syncer := s.relationSyncer
	s.mu.RUnlock()

	for _, ps := range syncs {
		if ps.targetDB == "" || ps.syncedPropID == "" || ps.sourceRef == "" {
			continue
		}
		if syncer != nil {
			_, _ = syncer.SyncExternalRelation(spaceRoot, ps.targetDB, ps.syncedPropID, ps.sourceRef, ps.addTargets, ps.removeTargets)
			s.mu.Lock()
			delete(s.extCache, ps.targetDB)
			s.computedRev = 0
			s.mu.Unlock()
			continue
		}
		if spaceRoot == "" {
			continue
		}
		targetRel := filepath.ToSlash(ps.targetDB)
		if !vault.IsDatabase(targetRel) {
			if infos, err := ListSpaceDatabases(spaceRoot); err == nil {
				for _, info := range infos {
					if info.ID == ps.targetDB {
						targetRel = info.RelPath
						break
					}
				}
			}
		}
		extStore, err := Open(spaceRoot, targetRel, nil)
		if err != nil {
			continue
		}
		affected, err := extStore.ApplySymmetricRelationDelta(ps.syncedPropID, ps.sourceRef, ps.addTargets, ps.removeTargets)
		if err == nil {
			for _, p := range affected {
				if rec := extStore.RecordByPath(p); rec != nil {
					_ = extStore.FlushRecord(rec.ID)
				}
			}
		}
		s.mu.Lock()
		delete(s.extCache, ps.targetDB)
		delete(s.extCache, targetRel)
		s.computedRev = 0
		s.mu.Unlock()
	}
}
