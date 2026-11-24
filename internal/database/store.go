package database

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Ciao1019/ArcMD/internal/vault"
)

// CellEdit identifies one property assignment on a record for batch updates.
type CellEdit struct {
	RecordID string
	PropID   string
	Value    Value
}

type fileStatCacheEntry struct {
	size      int64
	modTime   time.Time
	schemaSig [32]byte
	record    *Record
}

type queryCacheKey struct {
	revision uint64
	viewID   string
	search   string
	dateDay  string
}

// Store owns an open Database schema, its indexed records (from folder, space,
// or inline rows), mtime incremental cache, query result cache, and undo history.
type Store struct {
	mu               sync.RWMutex
	spaceRoot        string
	dbRel            string
	db               *Database
	propMap          map[string]Property
	records          []*Record
	byID             map[string]*Record
	byPath           map[string]*Record
	fileCache        map[string]fileStatCacheEntry
	extCache         map[string]extDBCacheEntry
	relationResolver RelationResolver
	relationSyncer   RelationSyncer
	revision         uint64
	computedRev      uint64
	computedDay      string
	history          *History
	cacheMu          sync.RWMutex
	queryCache       map[queryCacheKey]*QueryResult
}

// Open reads the .db.md database file at dbRel under spaceRoot, parses its
// schema, and populates records (including unsaved LiveDoc overlays).
func Open(spaceRoot, dbRel string, live []LiveDoc) (*Store, error) {
	text, err := vault.Read(spaceRoot, dbRel)
	if err != nil {
		return nil, err
	}
	db, err := DecodeDatabaseMarkdown(text)
	if err != nil {
		return nil, err
	}
	st := NewStore(spaceRoot, dbRel, db)
	if _, err := st.Refresh(live); err != nil {
		return nil, err
	}
	return st, nil
}

// NewStore initializes a Store from an in-memory Database schema.
func NewStore(spaceRoot, dbRel string, db *Database) *Store {
	cloned := db.Clone()
	st := &Store{
		spaceRoot:  spaceRoot,
		dbRel:      filepath.ToSlash(dbRel),
		db:         cloned,
		byID:       make(map[string]*Record),
		byPath:     make(map[string]*Record),
		fileCache:  make(map[string]fileStatCacheEntry),
		extCache:   make(map[string]extDBCacheEntry),
		revision:   1,
		history:    &History{},
		queryCache: make(map[queryCacheKey]*QueryResult),
	}
	st.rebuildPropMap()
	if cloned != nil && cloned.Source.Type == SourceInline {
		st.syncInlineIndexLocked()
	}
	return st
}

// Database returns a deep copy of the current Database schema.
func (s *Store) Database() *Database {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db.Clone()
}

// Revision returns a monotonically increasing counter bumped on any state change.
func (s *Store) Revision() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revision
}

// History returns the store's undo/redo history tracker.
func (s *Store) History() *History {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.history
}

// Records returns a snapshot slice of all currently indexed records.
func (s *Store) Records() []*Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Record, len(s.records))
	copy(out, s.records)
	return out
}

// Record returns the record matching id, or nil if not found.
func (s *Store) Record(id string) *Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byID[id]
}

// RecordByPath returns the record backed by relPath, or nil.
func (s *Store) RecordByPath(relPath string) *Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byPath[filepath.ToSlash(relPath)]
}

// ResolveSourceDir computes the space-relative folder scanned by this database.
// For SourceFolder with empty Dir, it resolves to "<dbDir>/<dbTitle>".
func (s *Store) ResolveSourceDir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.resolveSourceDirLocked()
}

func (s *Store) resolveSourceDirLocked() string {
	if s.db == nil {
		return ""
	}
	switch s.db.Source.Type {
	case SourceInline:
		return ""
	case SourceSpace:
		dir := path.Clean(strings.TrimPrefix(strings.ReplaceAll(s.db.Source.Dir, `\`, "/"), "/"))
		if dir == "." {
			return ""
		}
		return dir
	case SourceFolder:
		dir := strings.TrimSpace(strings.ReplaceAll(s.db.Source.Dir, `\`, "/"))
		if dir == "" {
			dbDir := path.Dir(s.dbRel)
			if dbDir == "." {
				dbDir = ""
			}
			stem := vault.Title(s.dbRel)
			if stem == "" {
				stem = s.db.Title
			}
			return path.Clean(path.Join(dbDir, stem))
		}
		clean := path.Clean(strings.TrimPrefix(dir, "/"))
		if clean == "." {
			return ""
		}
		return clean
	default:
		return ""
	}
}

func (s *Store) rebuildPropMap() {
	if s.db == nil {
		s.propMap = map[string]Property{}
		return
	}
	m := make(map[string]Property, len(s.db.Properties))
	for _, p := range s.db.Properties {
		m[p.ID] = p
	}
	s.propMap = m
}

func (s *Store) schemaSigLocked() [32]byte {
	if s.db == nil {
		return [32]byte{}
	}
	data, _ := json.Marshal(s.db.Properties)
	return sha256.Sum256(data)
}

func (s *Store) bumpRevisionLocked() {
	s.revision++
	s.cacheMu.Lock()
	clear(s.queryCache)
	s.cacheMu.Unlock()
}

func (s *Store) syncInlineIndexLocked() {
	s.records = make([]*Record, len(s.db.InlineRows))
	clear(s.byID)
	clear(s.byPath)
	for i, r := range s.db.InlineRows {
		if r == nil {
			continue
		}
		s.records[i] = r
		s.byID[r.ID] = r
	}
}

// Refresh reloads backing files under the configured Source, reusing cached
// parsed records whenever file size, modification time, and schema match, and
// overlaying unsaved LiveDoc contents in memory without writing to disk.
func (s *Store) Refresh(live []LiveDoc) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return false, nil
	}
	if s.db.Source.Type == SourceInline {
		return false, nil
	}
	if s.spaceRoot == "" {
		return false, errors.New("database: empty space root")
	}

	sourceDir := s.resolveSourceDirLocked()
	liveMap := make(map[string]LiveDoc, len(live))
	for _, ld := range live {
		clean := filepath.ToSlash(ld.Path)
		if pathInsideSource(clean, sourceDir, s.db.Source.Type == SourceSpace || s.db.Source.Recursive) {
			liveMap[clean] = ld
		}
	}

	var scanRoot string
	if sourceDir == "" {
		scanRoot = s.spaceRoot
	} else {
		var err error
		scanRoot, err = vault.Resolve(s.spaceRoot, sourceDir)
		if err != nil {
			return false, err
		}
	}

	recursive := s.db.Source.Type == SourceSpace || s.db.Source.Recursive
	schemaSig := s.schemaSigLocked()

	type candidate struct {
		rel      string
		full     string
		size     int64
		modTime  time.Time
		hasStat  bool
		fromLive bool
		liveDoc  LiveDoc
	}
	var candidates []candidate
	seenRel := make(map[string]bool)

	info, statErr := os.Stat(scanRoot)
	if statErr == nil && info.IsDir() {
		walkErr := filepath.WalkDir(scanRoot, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != scanRoot {
					name := d.Name()
					if strings.HasPrefix(name, ".") || name == "node_modules" || name == "assets" {
						return filepath.SkipDir
					}
					if !recursive {
						return filepath.SkipDir
					}
				}
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") || !vault.IsMarkdown(name) {
				return nil
			}
			relFromRoot, err := filepath.Rel(s.spaceRoot, p)
			if err != nil {
				return nil
			}
			rel := filepath.ToSlash(relFromRoot)
			if rel == s.dbRel {
				return nil
			}
			if d.Type()&fs.ModeSymlink != 0 {
				if _, err := vault.Resolve(s.spaceRoot, rel); err != nil {
					return nil
				}
			}
			fi, err := os.Stat(p)
			if err != nil || !fi.Mode().IsRegular() {
				return nil
			}
			seenRel[rel] = true
			ld, isLive := liveMap[rel]
			candidates = append(candidates, candidate{
				rel:      rel,
				full:     p,
				size:     fi.Size(),
				modTime:  fi.ModTime(),
				hasStat:  true,
				fromLive: isLive,
				liveDoc:  ld,
			})
			return nil
		})
		if walkErr != nil {
			return false, walkErr
		}
	} else if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
		return false, statErr
	}

	// Include live docs that don't exist on disk yet.
	for rel, ld := range liveMap {
		if !seenRel[rel] && rel != s.dbRel && vault.IsMarkdown(rel) {
			candidates = append(candidates, candidate{
				rel:      rel,
				fromLive: true,
				liveDoc:  ld,
			})
		}
	}

	slices.SortFunc(candidates, func(a, b candidate) int {
		return strings.Compare(strings.ToLower(a.rel), strings.ToLower(b.rel))
	})

	nextRecords := make([]*Record, 0, len(candidates))
	nextFileCache := make(map[string]fileStatCacheEntry, len(candidates))
	tagFilter := s.db.Source.TagFilter

	for _, cand := range candidates {
		var rec *Record
		if cand.fromLive {
			created := cand.modTime
			updated := cand.liveDoc.UpdatedAt
			if updated.IsZero() {
				updated = cand.modTime
			}
			if created.IsZero() {
				created = updated
			}
			rec = ExtractRecordFromMarkdown(s.db.Properties, cand.rel, cand.liveDoc.Text, created, updated)
			rec.FileSize = cand.size
			rec.ModTime = cand.modTime
		} else {
			if cached, ok := s.fileCache[cand.rel]; ok &&
				cached.size == cand.size &&
				cached.modTime.Equal(cand.modTime) &&
				cached.schemaSig == schemaSig &&
				cached.record != nil {
				rec = cached.record
			} else {
				content, err := vault.Read(s.spaceRoot, cand.rel)
				if err != nil {
					continue
				}
				rec = ExtractRecordFromMarkdown(s.db.Properties, cand.rel, content, cand.modTime, cand.modTime)
				rec.FileSize = cand.size
				rec.ModTime = cand.modTime
			}
			nextFileCache[cand.rel] = fileStatCacheEntry{
				size:      cand.size,
				modTime:   cand.modTime,
				schemaSig: schemaSig,
				record:    rec,
			}
		}
		if len(tagFilter) > 0 && !matchesTagFilter(rec.Tags, tagFilter) {
			continue
		}
		nextRecords = append(nextRecords, rec)
	}

	changed := len(nextRecords) != len(s.records)
	if !changed {
		for i := range nextRecords {
			if !nextRecords[i].EqualContent(s.records[i]) {
				changed = true
				break
			}
		}
	}

	s.fileCache = nextFileCache
	if !changed {
		return false, nil
	}

	s.records = nextRecords
	clear(s.byID)
	clear(s.byPath)
	for _, r := range nextRecords {
		s.byID[r.ID] = r
		if r.Path != "" {
			s.byPath[r.Path] = r
		}
	}
	s.bumpRevisionLocked()
	return true, nil
}

func pathInsideSource(rel, sourceDir string, recursive bool) bool {
	if sourceDir == "" {
		if recursive {
			return true
		}
		return !strings.Contains(rel, "/")
	}
	prefix := sourceDir + "/"
	if !strings.HasPrefix(rel, prefix) {
		return false
	}
	if recursive {
		return true
	}
	rest := strings.TrimPrefix(rel, prefix)
	return !strings.Contains(rest, "/")
}

func matchesTagFilter(recordTags, filter []string) bool {
	for _, want := range filter {
		w := strings.TrimSpace(strings.TrimPrefix(want, "#"))
		if w == "" {
			continue
		}
		for _, have := range recordTags {
			if strings.EqualFold(have, w) || strings.HasPrefix(strings.ToLower(have), strings.ToLower(w)+"/") {
				return true
			}
		}
	}
	return false
}

// --- Cell Mutations ---

type cellDelta struct {
	recordID string
	propID   string
	path     string
	isTitle  bool
	oldTitle string
	newTitle string
	hadOld   bool
	oldVal   Value
	newVal   Value
}

// SetCell updates a single property cell on recordID, pushes an undo step, and
// returns affected backing file paths.
func (s *Store) SetCell(recordID, propID string, val Value) ([]string, error) {
	return s.SetCellsBatch([]CellEdit{{RecordID: recordID, PropID: propID, Value: val}})
}

// SetCellsBatch updates multiple property cells atomically as one undo step.
func (s *Store) SetCellsBatch(edits []CellEdit) ([]string, error) {
	return s.setCellsBatchInternal(edits, true)
}

func (s *Store) setCellsBatchInternal(edits []CellEdit, syncRelations bool) ([]string, error) {
	if len(edits) == 0 {
		return nil, nil
	}

	s.mu.Lock()
	var deltas []cellDelta
	editedKey := map[string]bool{}
	for _, ed := range edits {
		editedKey[ed.RecordID+"\x00"+ed.PropID] = true
	}
	var extSyncs []pendingExternalRelationSync

	for _, ed := range edits {
		rec := s.byID[ed.RecordID]
		if rec == nil {
			s.mu.Unlock()
			return nil, fmt.Errorf("database: record %q not found", ed.RecordID)
		}
		prop, ok := s.propMap[ed.PropID]
		if !ok {
			s.mu.Unlock()
			return nil, fmt.Errorf("database: property %q not found", ed.PropID)
		}
		if prop.Type == PropCreatedTime || prop.Type == PropEditedTime || prop.Type == PropRollup || prop.Type == PropFormula {
			s.mu.Unlock()
			return nil, fmt.Errorf("database: property %q (%s) is read-only", prop.ID, prop.Type)
		}
		if rec.Props == nil {
			rec.Props = make(map[string]Value)
		}
		if prop.Type == PropTitle {
			coerced := CoerceValue(prop, ed.Value)
			newTitle := coerced.Text
			oldVal, hadOld := rec.Props[prop.ID]
			if newTitle == rec.Title && (!s.isInlineLocked() || oldVal.Equal(coerced)) {
				continue
			}
			deltas = append(deltas, cellDelta{
				recordID: rec.ID,
				propID:   prop.ID,
				path:     rec.Path,
				isTitle:  true,
				oldTitle: rec.Title,
				newTitle: newTitle,
				hadOld:   hadOld,
				oldVal:   oldVal.Clone(),
				newVal:   coerced.Clone(),
			})
			continue
		}

		coerced := CoerceValue(prop, ed.Value)
		oldVal, hadOld := rec.Props[prop.ID]
		if oldVal.Equal(coerced) && (!coerced.IsEmpty() || !hadOld) {
			continue
		}
		deltas = append(deltas, cellDelta{
			recordID: rec.ID,
			propID:   prop.ID,
			path:     rec.Path,
			hadOld:   hadOld,
			oldVal:   oldVal.Clone(),
			newVal:   coerced.Clone(),
		})

		if syncRelations && prop.Type == PropRelation && prop.Relation != nil && prop.Relation.SyncedPropID != "" {
			addTargets, removeTargets := diffStringSets(oldVal.List, coerced.List)
			if len(addTargets) > 0 || len(removeTargets) > 0 {
				sourceRef := RecordLinkRef(rec)
				if s.isSelfTargetLocked(prop.Relation.TargetDB) {
					if syncedProp, hasSP := s.propMap[prop.Relation.SyncedPropID]; hasSP && syncedProp.Type == PropRelation && sourceRef != "" {
						queueSelfDelta := func(targetRef string, add bool) {
							tr := MatchRelationRecord(s.records, targetRef)
							if tr == nil || editedKey[tr.ID+"\x00"+syncedProp.ID] {
								return
							}
							tOld, tHadOld := tr.Props[syncedProp.ID]
							tCur := slices.Clone(tr.ValueFor(syncedProp).List)
							hasIdx := slices.IndexFunc(tCur, func(x string) bool { return strings.EqualFold(x, sourceRef) })
							if add {
								if hasIdx >= 0 {
									return
								}
								if syncedProp.Relation != nil && syncedProp.Relation.LimitOne {
									tCur = []string{sourceRef}
								} else {
									tCur = append(tCur, sourceRef)
								}
							} else {
								if hasIdx < 0 {
									return
								}
								tCur = slices.Delete(tCur, hasIdx, hasIdx+1)
							}
							tNew := ListValue(tCur...)
							if tOld.Equal(tNew) && (!tNew.IsEmpty() || !tHadOld) {
								return
							}
							editedKey[tr.ID+"\x00"+syncedProp.ID] = true
							deltas = append(deltas, cellDelta{
								recordID: tr.ID,
								propID:   syncedProp.ID,
								path:     tr.Path,
								hadOld:   tHadOld,
								oldVal:   tOld.Clone(),
								newVal:   tNew.Clone(),
							})
						}
						for _, t := range addTargets {
							queueSelfDelta(t, true)
						}
						for _, t := range removeTargets {
							queueSelfDelta(t, false)
						}
					}
				} else if sourceRef != "" {
					extSyncs = append(extSyncs, pendingExternalRelationSync{
						targetDB:      prop.Relation.TargetDB,
						syncedPropID:  prop.Relation.SyncedPropID,
						sourceRef:     sourceRef,
						addTargets:    addTargets,
						removeTargets: removeTargets,
					})
				}
			}
		}
	}

	if len(deltas) == 0 {
		s.mu.Unlock()
		return nil, nil
	}

	affected := s.applyCellDeltasLocked(deltas, true)
	s.history.push(historyStep{
		label: "edit cells",
		undo: func(st *Store) []string {
			return st.applyCellDeltasLocked(deltas, false)
		},
		redo: func(st *Store) []string {
			return st.applyCellDeltasLocked(deltas, true)
		},
	})
	s.bumpRevisionLocked()
	s.mu.Unlock()

	if len(extSyncs) > 0 {
		s.runExternalRelationSync(extSyncs)
	}
	return affected, nil
}

func (s *Store) isInlineLocked() bool {
	return s.db != nil && s.db.Source.Type == SourceInline
}

func (s *Store) applyCellDeltasLocked(deltas []cellDelta, redo bool) []string {
	var paths []string
	seenPath := map[string]bool{}
	var iter []cellDelta
	if redo {
		iter = deltas
	} else {
		iter = make([]cellDelta, len(deltas))
		for i := range deltas {
			iter[i] = deltas[len(deltas)-1-i]
		}
	}

	for _, d := range iter {
		rec := s.byID[d.recordID]
		if rec == nil {
			continue
		}
		if rec.Props == nil {
			rec.Props = make(map[string]Value)
		}
		if d.isTitle {
			if redo {
				rec.Title = d.newTitle
				if d.newVal.IsEmpty() {
					delete(rec.Props, d.propID)
				} else {
					rec.Props[d.propID] = d.newVal.Clone()
				}
			} else {
				rec.Title = d.oldTitle
				if d.hadOld {
					rec.Props[d.propID] = d.oldVal.Clone()
				} else {
					delete(rec.Props, d.propID)
				}
			}
		} else {
			if redo {
				if d.newVal.IsEmpty() {
					delete(rec.Props, d.propID)
				} else {
					rec.Props[d.propID] = d.newVal.Clone()
				}
			} else {
				if d.hadOld {
					rec.Props[d.propID] = d.oldVal.Clone()
				} else {
					delete(rec.Props, d.propID)
				}
			}
		}
		if rec.Path != "" {
			delete(s.fileCache, rec.Path)
			if !seenPath[rec.Path] {
				seenPath[rec.Path] = true
				paths = append(paths, rec.Path)
			}
		}
	}
	return paths
}

// --- Record Lifecycle ---

// CreateRecord creates a new record either as an inline row or as an exclusive
// Markdown file in the resolved source directory, auto-assigning UniqueIDs.
func (s *Store) CreateRecord(title string, initialProps map[string]Value, body string) (*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil, errors.New("database: nil schema")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "未命名"
	}

	props := make(map[string]Value, len(initialProps)+1)
	for pid, val := range initialProps {
		if prop, ok := s.propMap[pid]; ok {
			if prop.Type == PropCreatedTime || prop.Type == PropEditedTime || prop.Type == PropRollup || prop.Type == PropFormula || prop.Type == PropTitle {
				continue
			}
			coerced := CoerceValue(prop, val)
			if !coerced.IsEmpty() {
				props[pid] = coerced
			}
		}
	}

	// Auto-assign UniqueID values and track uniqueID increments for rollback.
	type uidBump struct {
		propIdx int
		oldNext int
		newNext int
	}
	var bumps []uidBump
	for i := range s.db.Properties {
		p := &s.db.Properties[i]
		if p.Type != PropUniqueID {
			continue
		}
		if p.UniqueID == nil {
			p.UniqueID = &UniqueIDConfig{Next: 1}
		}
		oldNext := p.UniqueID.Next
		seq := oldNext
		if seq <= 0 {
			seq = 1
		}
		idStr := fmt.Sprintf("%s%d", p.UniqueID.Prefix, seq)
		if p.UniqueID.Prefix != "" && !strings.HasSuffix(p.UniqueID.Prefix, "-") {
			idStr = fmt.Sprintf("%s-%d", p.UniqueID.Prefix, seq)
		}
		props[p.ID] = TextValue(idStr)
		p.UniqueID.Next = seq + 1
		bumps = append(bumps, uidBump{propIdx: i, oldNext: oldNext, newNext: seq + 1})
	}
	if len(bumps) > 0 {
		s.rebuildPropMap()
	}

	now := time.Now()
	if s.db.Source.Type == SourceInline {
		rec := &Record{
			ID:        NewID(),
			Title:     title,
			Props:     props,
			Body:      body,
			Excerpt:   extractExcerpt(body, 200),
			CreatedAt: now,
			UpdatedAt: now,
		}
		idx := len(s.db.InlineRows)
		s.db.InlineRows = append(s.db.InlineRows, rec)
		s.syncInlineIndexLocked()

		recSnap := rec.Clone()
		s.history.push(historyStep{
			label: "create record",
			undo: func(st *Store) []string {
				if idx < len(st.db.InlineRows) {
					st.db.InlineRows = slices.Delete(st.db.InlineRows, idx, idx+1)
				}
				for _, b := range bumps {
					if b.propIdx < len(st.db.Properties) && st.db.Properties[b.propIdx].UniqueID != nil {
						st.db.Properties[b.propIdx].UniqueID.Next = b.oldNext
					}
				}
				st.rebuildPropMap()
				st.syncInlineIndexLocked()
				return nil
			},
			redo: func(st *Store) []string {
				pos := min(idx, len(st.db.InlineRows))
				st.db.InlineRows = slices.Insert(st.db.InlineRows, pos, recSnap.Clone())
				for _, b := range bumps {
					if b.propIdx < len(st.db.Properties) && st.db.Properties[b.propIdx].UniqueID != nil {
						st.db.Properties[b.propIdx].UniqueID.Next = b.newNext
					}
				}
				st.rebuildPropMap()
				st.syncInlineIndexLocked()
				return nil
			},
		})
		s.bumpRevisionLocked()
		return rec, nil
	}

	if s.spaceRoot == "" {
		return nil, errors.New("database: empty space root")
	}
	dir := s.resolveSourceDirLocked()
	fm := &FrontmatterDoc{}
	WritePropsToFrontmatter(s.db.Properties, fm, props)

	var md strings.Builder
	if !fm.IsEmpty() {
		md.WriteString("---\n")
		md.WriteString(fm.Format())
		md.WriteString("\n---\n\n")
	}
	trimmedBody := strings.TrimLeft(body, "\r\n")
	if strings.HasPrefix(trimmedBody, "# ") {
		md.WriteString(trimmedBody)
		if !strings.HasSuffix(trimmedBody, "\n") {
			md.WriteByte('\n')
		}
	} else {
		md.WriteString("# ")
		md.WriteString(title)
		md.WriteString("\n\n")
		if trimmedBody != "" {
			md.WriteString(trimmedBody)
			if !strings.HasSuffix(trimmedBody, "\n") {
				md.WriteByte('\n')
			}
		}
	}

	content := md.String()
	rel, err := vault.CreateFile(s.spaceRoot, dir, title, ".md", content)
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(rel)

	var fiSize int64
	var fiMod time.Time
	if full, rerr := vault.Resolve(s.spaceRoot, rel); rerr == nil {
		if fi, serr := os.Stat(full); serr == nil {
			fiSize = fi.Size()
			fiMod = fi.ModTime()
		}
	}
	if fiMod.IsZero() {
		fiMod = now
	}

	rec := ExtractRecordFromMarkdown(s.db.Properties, rel, content, fiMod, fiMod)
	rec.FileSize = fiSize
	rec.ModTime = fiMod

	s.records = append(s.records, rec)
	s.byID[rec.ID] = rec
	s.byPath[rec.Path] = rec
	s.fileCache[rel] = fileStatCacheEntry{
		size:      fiSize,
		modTime:   fiMod,
		schemaSig: s.schemaSigLocked(),
		record:    rec,
	}
	s.bumpRevisionLocked()
	return rec, nil
}

// DeleteRecord removes recordID from the database index (and InlineRows when inline).
// For file-backed databases it never deletes disk files directly; it returns the
// space-relative backing path so the caller can move it safely to Trash.
func (s *Store) DeleteRecord(recordID string) (backingPath string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec := s.byID[recordID]
	if rec == nil {
		return "", fmt.Errorf("database: record %q not found", recordID)
	}

	if s.isInlineLocked() {
		idx := -1
		for i, r := range s.db.InlineRows {
			if r != nil && r.ID == recordID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return "", fmt.Errorf("database: record %q not found", recordID)
		}
		removed := s.db.InlineRows[idx].Clone()
		s.db.InlineRows = slices.Delete(s.db.InlineRows, idx, idx+1)
		s.syncInlineIndexLocked()
		s.history.push(historyStep{
			label: "delete record",
			undo: func(st *Store) []string {
				pos := min(idx, len(st.db.InlineRows))
				st.db.InlineRows = slices.Insert(st.db.InlineRows, pos, removed.Clone())
				st.syncInlineIndexLocked()
				return nil
			},
			redo: func(st *Store) []string {
				if idx < len(st.db.InlineRows) && st.db.InlineRows[idx].ID == removed.ID {
					st.db.InlineRows = slices.Delete(st.db.InlineRows, idx, idx+1)
				} else {
					for i, r := range st.db.InlineRows {
						if r.ID == removed.ID {
							st.db.InlineRows = slices.Delete(st.db.InlineRows, i, i+1)
							break
						}
					}
				}
				st.syncInlineIndexLocked()
				return nil
			},
		})
		s.bumpRevisionLocked()
		return "", nil
	}

	backingPath = rec.Path
	for i, r := range s.records {
		if r.ID == recordID {
			s.records = slices.Delete(s.records, i, i+1)
			break
		}
	}
	delete(s.byID, recordID)
	if backingPath != "" {
		delete(s.byPath, backingPath)
		delete(s.fileCache, backingPath)
	}
	s.bumpRevisionLocked()
	return backingPath, nil
}

// RenameRecordPath updates indexed path, manual view orders, and relation values
// when a backing record file is renamed or moved.
func (s *Store) RenameRecordPath(oldRel, newRel string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldRel = filepath.ToSlash(oldRel)
	newRel = filepath.ToSlash(newRel)
	if oldRel == "" || newRel == "" || oldRel == newRel {
		return
	}

	changed := false
	if rec, ok := s.byPath[oldRel]; ok {
		delete(s.byPath, oldRel)
		if rec.ID == oldRel {
			delete(s.byID, oldRel)
			rec.ID = newRel
			s.byID[newRel] = rec
		}
		rec.Path = newRel
		s.byPath[newRel] = rec
		if ce, okc := s.fileCache[oldRel]; okc {
			delete(s.fileCache, oldRel)
			s.fileCache[newRel] = ce
		}
		changed = true
	}

	if s.db != nil {
		for vi := range s.db.Views {
			mo := s.db.Views[vi].ManualOrder
			for i, id := range mo {
				if id == oldRel {
					mo[i] = newRel
					changed = true
				}
			}
		}

		oldTitle := vault.Title(oldRel)
		newTitle := vault.Title(newRel)
		for _, p := range s.db.Properties {
			if p.Type != PropRelation {
				continue
			}
			for _, rec := range s.records {
				v, ok := rec.Props[p.ID]
				if !ok || len(v.List) == 0 {
					continue
				}
				updated := false
				nextList := slices.Clone(v.List)
				for i, target := range nextList {
					switch {
					case target == oldRel:
						nextList[i] = newRel
						updated = true
					case !strings.Contains(target, "/") && strings.EqualFold(target, oldTitle) && oldTitle != newTitle:
						nextList[i] = newTitle
						updated = true
					}
				}
				if updated {
					rec.Props[p.ID] = ListValue(nextList...)
					changed = true
				}
			}
		}
	}

	if changed {
		s.bumpRevisionLocked()
	}
}

// FlushRecord writes recordID's current property values back into its backing
// .md file's YAML frontmatter while leaving the document body untouched.
func (s *Store) FlushRecord(recordID string) error {
	s.mu.RLock()
	rec := s.byID[recordID]
	if rec == nil {
		s.mu.RUnlock()
		return fmt.Errorf("database: record %q not found", recordID)
	}
	if s.isInlineLocked() || rec.Path == "" {
		s.mu.RUnlock()
		return nil
	}
	rel := rec.Path
	recTitle := rec.Title
	props := make(map[string]Value, len(rec.Props))
	for k, v := range rec.Props {
		props[k] = v.Clone()
	}
	schema := make([]Property, len(s.db.Properties))
	for i, p := range s.db.Properties {
		schema[i] = p.Clone()
	}
	schemaSig := s.schemaSigLocked()
	s.mu.RUnlock()

	diskText, err := vault.Read(s.spaceRoot, rel)
	if err != nil {
		return err
	}
	updated := ApplyFrontmatter(diskText, func(fm *FrontmatterDoc) {
		WritePropsToFrontmatter(schema, fm, props)
	})
	updated = syncMarkdownH1(updated, rel, recTitle)
	if updated == diskText {
		return nil
	}
	if err := vault.Write(s.spaceRoot, rel, updated); err != nil {
		return err
	}

	if full, rerr := vault.Resolve(s.spaceRoot, rel); rerr == nil {
		if fi, serr := os.Stat(full); serr == nil {
			s.mu.Lock()
			if cur := s.byPath[rel]; cur != nil {
				cur.FileSize = fi.Size()
				cur.ModTime = fi.ModTime()
				s.fileCache[rel] = fileStatCacheEntry{
					size:      fi.Size(),
					modTime:   fi.ModTime(),
					schemaSig: schemaSig,
					record:    cur,
				}
			}
			s.mu.Unlock()
		}
	}
	return nil
}

// InvalidateComputed clears cached external relation targets and forces computed
// Rollup and Formula columns to re-evaluate on the next query.
func (s *Store) InvalidateComputed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.extCache)
	s.computedRev = 0
	s.cacheMu.Lock()
	clear(s.queryCache)
	s.cacheMu.Unlock()
}

func (s *Store) ensureComputed(now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	day := now.Format("2006-01-02")

	s.mu.RLock()
	db := s.db
	if db == nil {
		s.mu.RUnlock()
		return
	}
	var computedProps []Property
	for _, p := range db.Properties {
		if p.Type == PropRollup || p.Type == PropFormula {
			computedProps = append(computedProps, p)
		}
	}
	if len(computedProps) == 0 {
		s.mu.RUnlock()
		return
	}
	if s.computedRev == s.revision && s.computedDay == day {
		s.mu.RUnlock()
		return
	}
	snapRev := s.revision
	propMap := make(map[string]Property, len(s.propMap))
	for k, v := range s.propMap {
		propMap[k] = v
	}
	records := make([]*Record, len(s.records))
	copy(records, s.records)
	dbSnap := db.Clone()
	s.mu.RUnlock()

	ctx := newEvalContext(s, dbSnap, propMap, records, now)
	computedByRec := make(map[string]map[string]Value, len(records))
	for _, rec := range records {
		if rec == nil {
			continue
		}
		m := make(map[string]Value, len(computedProps))
		// Evaluate Rollups first so dependent Formulas can read them immediately.
		for _, p := range computedProps {
			if p.Type == PropRollup {
				v := ctx.propValue(rec, p, 0)
				if !v.IsEmpty() || v.Kind == ValError {
					m[p.ID] = v
				}
			}
		}
		for _, p := range computedProps {
			if p.Type == PropFormula {
				v := ctx.propValue(rec, p, 0)
				if !v.IsEmpty() || v.Kind == ValError {
					m[p.ID] = v
				}
			}
		}
		computedByRec[rec.ID] = m
	}

	s.mu.Lock()
	if s.revision == snapRev {
		for _, rec := range s.records {
			if rec == nil {
				continue
			}
			rec.Computed = computedByRec[rec.ID]
		}
		s.computedRev = snapRev
		s.computedDay = day
	}
	s.mu.Unlock()
}

// EncodeMarkdown serializes the database schema and its active view's GFM
// snapshot table into the amphibious .db.md format.
func (s *Store) EncodeMarkdown(now time.Time) (string, error) {
	s.ensureComputed(now)

	s.mu.RLock()
	db := s.db.Clone()
	dbDir := path.Dir(s.dbRel)
	if dbDir == "." {
		dbDir = ""
	}
	s.mu.RUnlock()

	var snap *QueryResult
	if db != nil {
		snap = s.Query(db.ActiveView, QueryOptions{Now: now})
	}
	return EncodeDatabaseMarkdownRelative(dbDir, db, snap)
}

// --- Schema & View Mutations ---

// AddProperty appends prop to the schema and adds it to all views.
func (s *Store) AddProperty(prop Property) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return errors.New("database: nil schema")
	}
	if strings.TrimSpace(prop.ID) == "" {
		prop.ID = "prop_" + NewID()[:8]
	}
	if strings.TrimSpace(prop.Key) == "" {
		prop.Key = prop.ID
	}
	if prop.Type == PropTitle {
		return errors.New("database: cannot add a second title property")
	}

	nextDB := s.db.Clone()
	propSnap := prop.Clone()
	nextDB.Properties = append(nextDB.Properties, propSnap.Clone())
	for vi := range nextDB.Views {
		nextDB.Views[vi].Properties = append(nextDB.Views[vi].Properties, ViewProperty{
			PropertyID: propSnap.ID,
			Visible:    true,
			Width:      propSnap.Width,
			Wrap:       propSnap.Wrap,
		})
	}
	if err := nextDB.Validate(); err != nil {
		return err
	}

	oldViews := cloneViews(s.db.Views)
	newViews := cloneViews(nextDB.Views)
	idx := len(s.db.Properties)

	s.db.Properties = append(s.db.Properties, propSnap.Clone())
	s.db.Views = cloneViews(newViews)
	s.rebuildPropMap()
	s.history.push(historyStep{
		label: "add property",
		undo: func(st *Store) []string {
			st.db.Properties = slices.Delete(st.db.Properties, idx, idx+1)
			st.db.Views = cloneViews(oldViews)
			st.rebuildPropMap()
			return nil
		},
		redo: func(st *Store) []string {
			st.db.Properties = slices.Insert(st.db.Properties, idx, propSnap.Clone())
			st.db.Views = cloneViews(newViews)
			st.rebuildPropMap()
			return nil
		},
	})
	s.bumpRevisionLocked()
	return nil
}

func cloneViews(views []ViewConfig) []ViewConfig {
	if views == nil {
		return nil
	}
	out := make([]ViewConfig, len(views))
	for i, v := range views {
		out[i] = v.Clone()
	}
	return out
}

// UpdateProperty replaces property prop.ID, coercing existing record values if
// its PropertyType changed, and records a lossless undo step.
func (s *Store) UpdateProperty(prop Property) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil, errors.New("database: nil schema")
	}
	oldProp, idx := s.db.PropertyByID(prop.ID)
	if idx < 0 {
		return nil, fmt.Errorf("database: property %q not found", prop.ID)
	}
	if oldProp.Type == PropTitle && prop.Type != PropTitle {
		return nil, errors.New("database: cannot change title property type")
	}
	if oldProp.Type != PropTitle && prop.Type == PropTitle {
		return nil, errors.New("database: cannot convert property to title")
	}

	nextDB := s.db.Clone()
	nextDB.Properties[idx] = prop.Clone()
	if err := nextDB.Validate(); err != nil {
		return nil, err
	}

	var deltas []cellDelta
	if oldProp.Type != prop.Type {
		for _, rec := range s.records {
			if rec == nil {
				continue
			}
			oldVal, hadOld := rec.Props[prop.ID]
			newVal := CoerceValue(prop, rec.ValueFor(*oldProp))
			if oldVal.Equal(newVal) && (!newVal.IsEmpty() || !hadOld) {
				continue
			}
			deltas = append(deltas, cellDelta{
				recordID: rec.ID,
				propID:   prop.ID,
				path:     rec.Path,
				hadOld:   hadOld,
				oldVal:   oldVal.Clone(),
				newVal:   newVal.Clone(),
			})
		}
	}

	oldPropSnap := oldProp.Clone()
	newPropSnap := prop.Clone()
	s.db.Properties[idx] = newPropSnap.Clone()
	s.rebuildPropMap()
	affected := s.applyCellDeltasLocked(deltas, true)

	s.history.push(historyStep{
		label: "update property",
		undo: func(st *Store) []string {
			st.db.Properties[idx] = oldPropSnap.Clone()
			st.rebuildPropMap()
			return st.applyCellDeltasLocked(deltas, false)
		},
		redo: func(st *Store) []string {
			st.db.Properties[idx] = newPropSnap.Clone()
			st.rebuildPropMap()
			return st.applyCellDeltasLocked(deltas, true)
		},
	})
	s.bumpRevisionLocked()
	return affected, nil
}

// DeleteProperty removes property propID from the schema, view layouts, and all
// records, keeping an undo step that restores all cell values.
func (s *Store) DeleteProperty(propID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil, errors.New("database: nil schema")
	}
	oldProp, idx := s.db.PropertyByID(propID)
	if idx < 0 {
		return nil, fmt.Errorf("database: property %q not found", propID)
	}
	if oldProp.Type == PropTitle {
		return nil, errors.New("database: cannot delete title property")
	}

	oldViews := make([]ViewConfig, len(s.db.Views))
	for i, v := range s.db.Views {
		oldViews[i] = v.Clone()
	}

	nextDB := s.db.Clone()
	nextDB.Properties = slices.Delete(nextDB.Properties, idx, idx+1)
	for vi := range nextDB.Views {
		v := &nextDB.Views[vi]
		v.Properties = slices.DeleteFunc(v.Properties, func(vp ViewProperty) bool {
			return vp.PropertyID == propID
		})
		v.Sorts = slices.DeleteFunc(v.Sorts, func(sr SortRule) bool {
			return sr.PropertyID == propID
		})
		if v.Group != nil && v.Group.PropertyID == propID {
			v.Group = nil
		}
		if v.Board != nil && v.Board.SubGroup != nil && v.Board.SubGroup.PropertyID == propID {
			v.Board.SubGroup = nil
		}
		if v.Table != nil && v.Table.Aggregations != nil {
			delete(v.Table.Aggregations, propID)
		}
	}

	var deltas []cellDelta
	for _, rec := range s.records {
		if rec == nil {
			continue
		}
		if oldVal, ok := rec.Props[propID]; ok {
			deltas = append(deltas, cellDelta{
				recordID: rec.ID,
				propID:   propID,
				path:     rec.Path,
				hadOld:   true,
				oldVal:   oldVal.Clone(),
				newVal:   EmptyValue(),
			})
		}
	}

	oldPropSnap := oldProp.Clone()
	newViews := cloneViews(nextDB.Views)

	s.db.Properties = slices.Delete(s.db.Properties, idx, idx+1)
	s.db.Views = cloneViews(newViews)
	s.rebuildPropMap()
	affected := s.applyCellDeltasLocked(deltas, true)

	s.history.push(historyStep{
		label: "delete property",
		undo: func(st *Store) []string {
			st.db.Properties = slices.Insert(st.db.Properties, idx, oldPropSnap.Clone())
			st.db.Views = cloneViews(oldViews)
			st.rebuildPropMap()
			return st.applyCellDeltasLocked(deltas, false)
		},
		redo: func(st *Store) []string {
			st.db.Properties = slices.Delete(st.db.Properties, idx, idx+1)
			st.db.Views = cloneViews(newViews)
			st.rebuildPropMap()
			return st.applyCellDeltasLocked(deltas, true)
		},
	})
	s.bumpRevisionLocked()
	return affected, nil
}

// AddView adds a view to the database and optionally makes it active.
func (s *Store) AddView(v ViewConfig, activate bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return errors.New("database: nil schema")
	}
	if strings.TrimSpace(v.ID) == "" {
		v.ID = "view_" + NewID()[:8]
	}
	nextDB := s.db.Clone()
	nextDB.Views = append(nextDB.Views, v.Clone())
	if activate || nextDB.ActiveView == "" {
		nextDB.ActiveView = v.ID
	}
	if err := nextDB.Validate(); err != nil {
		return err
	}
	s.commitViewsLocked("add view", nextDB.Views, nextDB.ActiveView)
	return nil
}

// UpdateView replaces an existing ViewConfig with v.
func (s *Store) UpdateView(v ViewConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return errors.New("database: nil schema")
	}
	_, idx := s.db.ViewByID(v.ID)
	if idx < 0 {
		return fmt.Errorf("database: view %q not found", v.ID)
	}
	nextDB := s.db.Clone()
	nextDB.Views[idx] = v.Clone()
	if err := nextDB.Validate(); err != nil {
		return err
	}
	s.commitViewsLocked("update view", nextDB.Views, nextDB.ActiveView)
	return nil
}

// DeleteView removes viewID (preventing deletion of the last remaining view).
func (s *Store) DeleteView(viewID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return errors.New("database: nil schema")
	}
	if len(s.db.Views) <= 1 {
		return errors.New("database: cannot delete the last remaining view")
	}
	_, idx := s.db.ViewByID(viewID)
	if idx < 0 {
		return fmt.Errorf("database: view %q not found", viewID)
	}
	nextDB := s.db.Clone()
	nextDB.Views = slices.Delete(nextDB.Views, idx, idx+1)
	if nextDB.ActiveView == viewID {
		nextDB.ActiveView = nextDB.Views[0].ID
	}
	s.commitViewsLocked("delete view", nextDB.Views, nextDB.ActiveView)
	return nil
}

// DuplicateView clones viewID with a fresh ID and activates it.
func (s *Store) DuplicateView(viewID string) (*ViewConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil, errors.New("database: nil schema")
	}
	orig, idx := s.db.ViewByID(viewID)
	if idx < 0 {
		return nil, fmt.Errorf("database: view %q not found", viewID)
	}
	dup := orig.Clone()
	dup.ID = "view_" + NewID()[:8]
	dup.Name = orig.Name + " 副本"
	nextDB := s.db.Clone()
	nextDB.Views = slices.Insert(nextDB.Views, idx+1, dup)
	nextDB.ActiveView = dup.ID
	s.commitViewsLocked("duplicate view", nextDB.Views, nextDB.ActiveView)
	res := dup.Clone()
	return &res, nil
}

// SetActiveView selects viewID as the database's active view.
func (s *Store) SetActiveView(viewID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return errors.New("database: nil schema")
	}
	if _, idx := s.db.ViewByID(viewID); idx < 0 {
		return fmt.Errorf("database: view %q not found", viewID)
	}
	if s.db.ActiveView == viewID {
		return nil
	}
	s.db.ActiveView = viewID
	s.bumpRevisionLocked()
	return nil
}

// SetTitle updates the database's display title with an undoable history step.
func (s *Store) SetTitle(title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return errors.New("database: nil schema")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("database: empty title")
	}
	if title == s.db.Title {
		return nil
	}
	oldTitle := s.db.Title
	s.db.Title = title
	s.history.push(historyStep{
		label: "rename database",
		undo: func(st *Store) []string {
			st.db.Title = oldTitle
			return nil
		},
		redo: func(st *Store) []string {
			st.db.Title = title
			return nil
		},
	})
	s.bumpRevisionLocked()
	return nil
}

// SetManualOrder updates the manual row order of viewID.
func (s *Store) SetManualOrder(viewID string, order []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return errors.New("database: nil schema")
	}
	_, idx := s.db.ViewByID(viewID)
	if idx < 0 {
		return fmt.Errorf("database: view %q not found", viewID)
	}
	nextDB := s.db.Clone()
	nextDB.Views[idx].ManualOrder = slices.Clone(order)
	s.commitViewsLocked("manual order", nextDB.Views, nextDB.ActiveView)
	return nil
}

func (s *Store) commitViewsLocked(label string, nextViews []ViewConfig, nextActive string) {
	oldViews := make([]ViewConfig, len(s.db.Views))
	for i, v := range s.db.Views {
		oldViews[i] = v.Clone()
	}
	oldActive := s.db.ActiveView

	newViews := make([]ViewConfig, len(nextViews))
	for i, v := range nextViews {
		newViews[i] = v.Clone()
	}

	s.db.Views = newViews
	s.db.ActiveView = nextActive
	s.history.push(historyStep{
		label: label,
		undo: func(st *Store) []string {
			st.db.Views = make([]ViewConfig, len(oldViews))
			for i, v := range oldViews {
				st.db.Views[i] = v.Clone()
			}
			st.db.ActiveView = oldActive
			return nil
		},
		redo: func(st *Store) []string {
			st.db.Views = make([]ViewConfig, len(newViews))
			for i, v := range newViews {
				st.db.Views[i] = v.Clone()
			}
			st.db.ActiveView = nextActive
			return nil
		},
	})
	s.bumpRevisionLocked()
}

// Undo reverts the most recent recorded database mutation and returns affected backing paths.
func (s *Store) Undo() ([]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	paths, ok := s.history.undo(s)
	if ok {
		s.bumpRevisionLocked()
	}
	return paths, ok
}

// Redo re-applies the most recently undone mutation and returns affected backing paths.
func (s *Store) Redo() ([]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	paths, ok := s.history.redo(s)
	if ok {
		s.bumpRevisionLocked()
	}
	return paths, ok
}
