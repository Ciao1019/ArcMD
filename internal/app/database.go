package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/dbview"
	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const untitledDatabase = "未命名数据库"

type dbPeekState struct {
	open           bool
	mode           string // dbview.OpenSidePeek | dbview.OpenCenterPeek
	pendingMode    string
	dbDoc          *document
	recordID       string
	doc            *document
	inlineBody     string
	inlineTargetID string
	propsCollapsed bool
	panelWidth     float32
	lastDocVer     uint64
	resizing       bool
}

type pageDBEntry struct {
	dbRel string
	store *database.Store
	state *dbview.State
}

func (d *document) isDatabase() bool {
	return d != nil && d.dbView != nil && d.dbView.Store != nil
}

func databaseSpaceRoot(sp *space.Space, rel string) (spaceRoot, dbRel string) {
	if sp == nil {
		return "", rel
	}
	if sp.IsExternal() {
		if full, ok := sp.ExternalFiles[rel]; ok && full != "" {
			return filepath.Dir(full), filepath.Base(full)
		}
		return "", filepath.Base(rel)
	}
	return sp.Root, filepath.ToSlash(rel)
}

func openDatabaseDocument(sp *space.Space, rel, text string) *document {
	d := &document{space: sp, rel: rel, diskText: text}
	db, err := database.DecodeDatabaseMarkdown(text)
	if err != nil {
		d.loadErr = err
		db = database.NewDefault(vault.Title(rel), database.SourceConfig{Type: database.SourceFolder})
	}
	spaceRoot, dbRel := databaseSpaceRoot(sp, rel)
	store := database.NewStore(spaceRoot, dbRel, db)
	if d.loadErr == nil && spaceRoot != "" {
		if _, rerr := store.Refresh(nil); rerr != nil {
			d.saveErr = rerr
		}
	}
	d.dbView = dbview.NewState(store)
	d.dbBaseline = db.Digest()
	d.dbSavedRev = store.Revision()
	d.dbDirtySnapshot = false
	d.ed = editor.FromOwnedDocument(model.New())
	d.initLazyBaseline()
	d.source = false
	return d
}

func (d *document) databaseDigest() [32]byte {
	if !d.isDatabase() {
		return [32]byte{}
	}
	return d.dbView.Store.Database().Digest()
}

func (d *document) databaseDirty() bool {
	if !d.isDatabase() {
		return false
	}
	return d.dbDirtySnapshot || d.databaseDigest() != d.dbBaseline
}

func (d *document) writeDatabase(force bool) error {
	if !d.isDatabase() {
		return nil
	}
	if !d.databaseDirty() && !force {
		d.saveErr = nil
		return nil
	}
	if !force {
		disk, err := readFile(d.space, d.rel)
		switch {
		case isMissingDocFile(d.space, d.rel, err):
			d.missing = true
			return errMissing
		case err != nil:
			d.saveErr = err
			return err
		case disk != d.diskText:
			d.conflict, d.diskNow = true, disk
			return errConflict
		}
	}
	text, err := d.dbView.Store.EncodeMarkdown(time.Now())
	if err == nil {
		err = writeFile(d.space, d.rel, text)
	}
	if err != nil {
		d.saveErr = err
		return err
	}
	before := d.diskText
	d.diskText = text
	d.dbBaseline = d.databaseDigest()
	d.dbSavedRev = d.dbView.Store.Revision()
	d.dbDirtySnapshot = false
	d.saveErr = nil
	d.recordVersion(before)
	d.conflict, d.missing, d.diskNow = false, false, ""
	d.dirtyRev, d.dirtyKnown, d.dirtyNow = d.dbView.Store.Revision(), true, false
	d.dirtyVersion = d.dbView.Version()
	d.justSaved = true
	return nil
}

// newDatabase creates a new amphibious .db.md database in dir of the active
// space and opens it immediately.
func (a *App) newDatabase(dir string) {
	sp := a.state.ActiveSpace()
	if sp == nil {
		return
	}
	if sp.IsExternal() {
		a.notify("请在普通空间中新建数据库。")
		return
	}
	if !a.flush() {
		return
	}
	baseTitle := i18n.T(untitledDatabase)
	db := database.NewDefault(baseTitle, database.SourceConfig{Type: database.SourceFolder})
	content, err := database.EncodeDatabaseMarkdownRelative(dir, db, nil)
	if err != nil {
		a.notify("新建数据库失败。%v", err)
		return
	}
	rel, err := vault.CreateFile(sp.Root, dir, baseTitle, vault.ExtDatabase, content)
	if err != nil {
		a.notify("新建数据库失败。%v", err)
		return
	}
	actualTitle := vault.Title(rel)
	if actualTitle != "" && actualTitle != db.Title {
		db.Title = actualTitle
		if updated, encErr := database.EncodeDatabaseMarkdownRelative(dir, db, nil); encErr == nil {
			_ = writeFile(sp, rel, updated)
		}
	}
	if dir != "" {
		sp.SetExpanded(dir, true)
		a.saveState()
	}
	a.clearPicks("")
	a.refreshTree()
	a.refreshSearch()
	a.open(rel)
	if d := a.doc; d != nil && d.rel == rel && d.isDatabase() {
		d.dbView.RequestFocus = true
	}
}

func (a *App) liveDocsForSpace(sp *space.Space) []database.LiveDoc {
	if sp == nil {
		return nil
	}
	now := time.Now()
	var out []database.LiveDoc
	for _, res := range a.residentDocuments() {
		if res == nil || res.space != sp || res.loadErr != nil {
			continue
		}
		if res.isDatabase() || res.isWhiteboard() || res.isPDF() {
			continue
		}
		if !vault.IsMarkdown(res.rel) {
			continue
		}
		if !res.dirty() && !(a.dbPeek.open && a.dbPeek.doc == res) && res != a.doc {
			continue
		}
		out = append(out, database.LiveDoc{
			Path:      filepath.ToSlash(res.rel),
			Text:      res.text(),
			UpdatedAt: now,
		})
	}
	return out
}

func (a *App) refreshOpenDatabases(spaceID string) {
	a.refreshOpenDatabasesExcept(spaceID)
}

func (a *App) refreshOpenDatabasesExcept(spaceID string, skip ...*database.Store) {
	sp := a.state.Get(spaceID)
	if sp == nil {
		return
	}
	live := a.liveDocsForSpace(sp)
	refreshStore := func(st *database.Store) {
		if st == nil {
			return
		}
		if !slices.Contains(skip, st) {
			_, _ = st.Refresh(live)
		}
		st.InvalidateComputed()
	}
	for _, res := range a.residentDocuments() {
		if res != nil && res.space == sp && res.loadErr == nil && res.isDatabase() {
			refreshStore(res.dbView.Store)
		}
	}
	if a.dbPeek.open && a.dbPeek.dbDoc != nil && a.dbPeek.dbDoc.space == sp && a.dbPeek.dbDoc.isDatabase() {
		refreshStore(a.dbPeek.dbDoc.dbView.Store)
	}
	for k, entry := range a.pageDBCache {
		if entry != nil && entry.store != nil && strings.HasPrefix(k, spaceID+"\x00") {
			refreshStore(entry.store)
		}
	}
	for k, entry := range a.embedDBs {
		if entry != nil && entry.store != nil && strings.HasPrefix(k, spaceID+"\x00") {
			refreshStore(entry.store)
		}
	}
}

type appRelationHost struct {
	app *App
	sp  *space.Space
	src *database.Store
}

func (h *appRelationHost) resolveTargetRel(spaceRoot, targetDB string) string {
	rel := filepath.ToSlash(strings.TrimSpace(targetDB))
	if rel == "" {
		return ""
	}
	if vault.IsDatabase(rel) {
		return rel
	}
	if infos, err := database.ListSpaceDatabases(spaceRoot); err == nil {
		for _, info := range infos {
			if info.ID == targetDB || info.Title == targetDB || strings.TrimSuffix(info.RelPath, vault.ExtDatabase) == rel {
				return info.RelPath
			}
		}
	}
	return rel
}

func (h *appRelationHost) findOpenStore(targetDB string) (*database.Store, *document, string) {
	if h == nil || h.app == nil || h.sp == nil {
		return nil, nil, ""
	}
	targetClean := filepath.ToSlash(strings.TrimSpace(targetDB))
	for _, res := range h.app.residentDocuments() {
		if res != nil && res.space == h.sp && res.loadErr == nil && res.isDatabase() {
			db := res.dbView.Store.Database()
			if filepath.ToSlash(res.rel) == targetClean ||
				(db != nil && (db.ID == targetDB || db.Title == targetDB)) ||
				strings.TrimSuffix(filepath.ToSlash(res.rel), vault.ExtDatabase) == targetClean {
				return res.dbView.Store, res, res.rel
			}
		}
	}
	for k, entry := range h.app.pageDBCache {
		if entry != nil && entry.store != nil && strings.HasPrefix(k, h.sp.ID+"\x00") {
			db := entry.store.Database()
			if filepath.ToSlash(entry.dbRel) == targetClean ||
				(db != nil && (db.ID == targetDB || db.Title == targetDB)) ||
				strings.TrimSuffix(filepath.ToSlash(entry.dbRel), vault.ExtDatabase) == targetClean {
				return entry.store, nil, entry.dbRel
			}
		}
	}
	for k, entry := range h.app.embedDBs {
		if entry != nil && entry.store != nil && strings.HasPrefix(k, h.sp.ID+"\x00") {
			db := entry.store.Database()
			if filepath.ToSlash(entry.dbRel) == targetClean ||
				(db != nil && (db.ID == targetDB || db.Title == targetDB)) ||
				strings.TrimSuffix(filepath.ToSlash(entry.dbRel), vault.ExtDatabase) == targetClean {
				return entry.store, nil, entry.dbRel
			}
		}
	}
	return nil, nil, ""
}

func (h *appRelationHost) ResolveDatabase(spaceRoot, targetDB string) (*database.Database, []*database.Record, error) {
	if st, _, _ := h.findOpenStore(targetDB); st != nil {
		return st.Database(), st.Records(), nil
	}
	targetRel := h.resolveTargetRel(spaceRoot, targetDB)
	if targetRel == "" {
		return nil, nil, fmt.Errorf("database: relation target %q not found", targetDB)
	}
	extStore, err := database.Open(spaceRoot, targetRel, h.app.liveDocsForSpace(h.sp))
	if err != nil {
		return nil, nil, err
	}
	return extStore.Database(), extStore.Records(), nil
}

func (h *appRelationHost) SyncExternalRelation(spaceRoot, targetDB, syncedPropID, sourceRef string, addTargets, removeTargets []string) ([]string, error) {
	if extStore, extDoc, _ := h.findOpenStore(targetDB); extStore != nil {
		affected, err := extStore.ApplySymmetricRelationDelta(syncedPropID, sourceRef, addTargets, removeTargets)
		if err == nil && len(affected) > 0 {
			h.app.flushDatabaseEditsToDocs(h.sp, extDoc, extStore, affected)
			h.app.refreshOpenDatabasesExcept(h.sp.ID, h.src, extStore)
		}
		return affected, err
	}
	targetRel := h.resolveTargetRel(spaceRoot, targetDB)
	if targetRel == "" || spaceRoot == "" {
		return nil, nil
	}
	extStore, err := database.Open(spaceRoot, targetRel, h.app.liveDocsForSpace(h.sp))
	if err != nil {
		return nil, err
	}
	affected, err := extStore.ApplySymmetricRelationDelta(syncedPropID, sourceRef, addTargets, removeTargets)
	if err == nil && len(affected) > 0 {
		h.app.flushDatabaseEditsToDocs(h.sp, nil, extStore, affected)
		h.app.refreshOpenDatabasesExcept(h.sp.ID, h.src)
	}
	return affected, err
}

func (a *App) syncDatabaseEditsToDocs(sp *space.Space, dbDoc *document, store *database.Store, affectedPaths []string) {
	if sp == nil || store == nil {
		return
	}
	a.flushDatabaseEditsToDocs(sp, dbDoc, store, affectedPaths)
	a.refreshOpenDatabases(sp.ID)
	a.refreshSearch()
}

func (a *App) flushDatabaseEditsToDocs(sp *space.Space, dbDoc *document, store *database.Store, affectedPaths []string) {
	if sp == nil || store == nil {
		return
	}
	if dbDoc != nil {
		dbDoc.dbDirtySnapshot = true
	}
	db := store.Database()
	var schema []database.Property
	if db != nil {
		schema = db.Properties
	}
	for _, rel := range affectedPaths {
		rel = filepath.ToSlash(rel)
		rec := store.RecordByPath(rel)
		if rec == nil {
			continue
		}
		if res := a.residentDocument(sp.ID, rel); res != nil && res.loadErr == nil && !res.isDatabase() && !res.isWhiteboard() && !res.isPDF() && res.ed != nil {
			if !res.ed.ReadOnly {
				wasDirty := res.dirty()
				var fm *database.FrontmatterDoc
				if strings.TrimSpace(res.ed.Doc.Frontmatter) != "" {
					fm, _, _, _ = database.SplitFrontmatter("---\n" + res.ed.Doc.Frontmatter + "\n---\n")
				} else {
					fm = &database.FrontmatterDoc{}
				}
				database.WritePropsToFrontmatter(schema, fm, rec.Props)
				res.ed.Engine.SetFrontmatter(fm.Format())

				if rec.Title != "" && len(res.ed.Doc.Blocks) > 0 &&
					res.ed.Doc.Blocks[0].Kind == "heading" &&
					res.ed.Doc.Blocks[0].Level == 1 &&
					res.ed.Doc.Blocks[0].Text() != rec.Title {
					blk := res.ed.Doc.Blocks[0]
					anchor, caret := res.ed.Engine.Anchor, res.ed.Engine.Caret
					res.ed.Engine.ReplaceBlockRuns(blk.ID, []model.Run{{Text: rec.Title}}, anchor, caret, res.ed.Engine.Typing, res.ed.Engine.Explicit)
				}
				if (a.autosaveEnabled() || !wasDirty) && !res.ed.Composing() {
					_ = res.write(false)
				}
				if a.dbPeek.open && a.dbPeek.doc == res {
					a.dbPeek.lastDocVer = res.ed.Version()
				}
			}
			continue
		}
		if err := store.FlushRecord(rec.ID); err != nil {
			a.notify("保存失败。%v", err)
		}
	}
	if dbDoc == nil && !sp.IsExternal() {
		a.flushCachedDatabaseMarkdown(sp, store)
	}
}

func (a *App) flushCachedDatabaseMarkdown(sp *space.Space, store *database.Store) {
	if sp == nil || sp.IsExternal() || store == nil {
		return
	}
	flushRel := func(dbRel string) {
		encoded, err := store.EncodeMarkdown(time.Now())
		if err != nil {
			return
		}
		disk, err := readFile(sp, dbRel)
		if err != nil || disk == encoded {
			return
		}
		_ = writeFile(sp, dbRel, encoded)
	}
	for _, entry := range a.pageDBCache {
		if entry != nil && entry.store == store && entry.dbRel != "" {
			flushRel(entry.dbRel)
			return
		}
	}
	for _, entry := range a.embedDBs {
		if entry != nil && entry.store == store && entry.dbRel != "" {
			flushRel(entry.dbRel)
			return
		}
	}
}

func (a *App) followDatabaseRecordMove(sp *space.Space, from, to string) {
	if sp == nil || from == to {
		return
	}
	a.invalidatePageDBCache(sp.ID)

	if vault.IsDatabase(from) && vault.IsDatabase(to) {
		oldDir := strings.TrimSuffix(from, vault.ExtDatabase)
		newDir := strings.TrimSuffix(to, vault.ExtDatabase)
		if oldDir != "" && newDir != "" && oldDir != newDir {
			if fullOld, err := resolveFile(sp, oldDir); err == nil {
				if info, serr := os.Stat(fullOld); serr == nil && info.IsDir() {
					if fullNew, nerr := resolveFile(sp, newDir); nerr == nil {
						if _, existErr := os.Stat(fullNew); errors.Is(existErr, fs.ErrNotExist) {
							var movedDir string
							var mvErr error
							if parentDir(oldDir) == parentDir(newDir) {
								movedDir, mvErr = vault.Rename(sp.Root, oldDir, path.Base(newDir))
							} else {
								movedDir, mvErr = vault.Move(sp.Root, oldDir, parentDir(newDir))
							}
							if mvErr == nil && movedDir != "" {
								a.state.Renamed(sp.ID, oldDir, movedDir)
								a.followOpenDocMove(sp, oldDir, movedDir)
								a.followCopied(sp.ID, oldDir, sp.ID, movedDir)
								a.saveState()
							}
						}
					}
				}
			}
		}
		for _, res := range a.residentDocuments() {
			if res != nil && res.space == sp && res.isDatabase() && res.rel == to {
				db := res.dbView.Store.Database()
				oldTitle := vault.Title(from)
				newTitle := vault.Title(to)
				if db != nil && db.Title == oldTitle && newTitle != "" && newTitle != db.Title {
					_ = res.dbView.Store.SetTitle(newTitle)
				}
				spaceRoot, dbRel := databaseSpaceRoot(sp, to)
				fresh := database.NewStore(spaceRoot, dbRel, res.dbView.Store.Database())
				_, _ = fresh.Refresh(a.liveDocsForSpace(sp))
				res.dbView.Store = fresh
				res.dbDirtySnapshot = true
			}
		}
		return
	}

	for _, res := range a.residentDocuments() {
		if res != nil && res.space == sp && res.loadErr == nil && res.isDatabase() {
			res.dbView.Store.RenameRecordPath(from, to)
			_, _ = res.dbView.Store.Refresh(a.liveDocsForSpace(sp))
			res.dbDirtySnapshot = true
		}
	}
	if a.dbPeek.open && a.dbPeek.dbDoc != nil && a.dbPeek.dbDoc.space == sp && a.dbPeek.doc != nil && under(a.dbPeek.doc.rel, from) {
		relTail := a.dbPeek.doc.rel[len(from):]
		a.dbPeek.doc.rel = to + relTail
		if a.dbPeek.recordID == from {
			a.dbPeek.recordID = to + relTail
		}
	}
}

func (a *App) followOpenDocMove(sp *space.Space, from, to string) {
	for p, od := range a.openDocs {
		if od != nil && od.space == sp && under(od.rel, from) {
			delete(a.openDocs, p)
			od.rel = to + od.rel[len(from):]
			a.openDocs[place{sp.ID, od.rel}] = od
		}
	}
	if a.doc != nil && a.doc.space == sp && under(a.doc.rel, from) {
		a.doc.rel = to + a.doc.rel[len(from):]
	}
}

func (a *App) invalidatePageDBCache(spaceID string) {
	if spaceID == "" {
		if a.pageDBCache != nil {
			clear(a.pageDBCache)
		}
		if a.embedDBs != nil {
			clear(a.embedDBs)
		}
		return
	}
	prefix := spaceID + "\x00"
	for k := range a.pageDBCache {
		if strings.HasPrefix(k, prefix) {
			delete(a.pageDBCache, k)
		}
	}
	for k := range a.embedDBs {
		if strings.HasPrefix(k, prefix) {
			delete(a.embedDBs, k)
		}
	}
}

func (a *App) trashDatabaseBackingPath(sp *space.Space, rel string) bool {
	if sp == nil || sp.IsExternal() || rel == "" {
		return false
	}
	if a.autosaveEnabled() {
		_ = a.flush()
	}
	p, err := resolveFile(sp, rel)
	if err != nil {
		a.notify("将 %s 移到废纸篓失败。%v", path.Base(rel), err)
		return false
	}
	if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
		return true
	}
	if a.renameSpace == sp.ID && a.renaming != "" && under(a.renaming, rel) {
		a.renaming = ""
		a.openAfterRename = ""
	}
	kept, ok := a.keepDeleted(sp.ID, sp.Name, sp.Root, rel)
	if err := a.trashItem(p); err != nil {
		if ok {
			a.bin.Forget(kept)
		}
		a.notify("将 %s 移到废纸篓失败。%v", path.Base(rel), err)
		return false
	}
	for _, d := range a.residentDocuments() {
		if d.space == sp && within(d.rel, rel) {
			if d == a.dbPeek.doc {
				a.closeRecordPeek()
			} else {
				a.closeOpen(sp, d.rel)
			}
			a.replaceReferences(d, nil)
		}
	}
	a.forgetPicks(sp.ID, rel)
	a.forgetCopied(sp.ID, rel)
	a.state.Forget(sp.ID, rel)
	delete(a.pageTrees, sp.ID)
	a.saveState()
	return true
}

func (a *App) databaseOptionsFor(c *ui.Context, d *document) dbview.Options {
	if d == nil || !d.isDatabase() {
		return dbview.Options{}
	}
	return a.databaseOptionsForStore(c, d.space, d.dbView.Store, d)
}

func (a *App) databaseOptionsForStore(c *ui.Context, sp *space.Space, store *database.Store, dbDoc *document) dbview.Options {
	var now time.Time
	if c != nil {
		now = c.Now()
	} else {
		now = time.Now()
	}
	ro := sp == nil || sp.IsExternal()
	if !ro && store != nil && !store.HasRelationResolver() {
		host := &appRelationHost{app: a, sp: sp, src: store}
		store.SetRelationResolver(host)
		store.SetRelationSyncer(host)
	}
	return dbview.Options{
		ReadOnly: ro,
		Now:      now,
		ListSpaceDBs: func() []database.SpaceDBInfo {
			if sp == nil || sp.IsExternal() || sp.Root == "" {
				return nil
			}
			dbs, _ := database.ListSpaceDatabases(sp.Root)
			return dbs
		},
		LoadImage: func(recPath, imgURL string) *ui.Bitmap {
			return loadDatabaseRecordImage(sp, dbDoc, recPath, imgURL)
		},
		OnChanged: func(affected []string) {
			a.syncDatabaseEditsToDocs(sp, dbDoc, store, affected)
		},
		OnAfterCreate: func(rec *database.Record) {
			if dbDoc != nil {
				dbDoc.dbDirtySnapshot = true
			} else if sp != nil {
				a.flushCachedDatabaseMarkdown(sp, store)
			}
			a.invalidatePageDBCache(spaceIDOf(sp))
			a.refreshTree()
			a.refreshSearch()
		},
		OnOpenRecord: func(rec *database.Record, mode string) {
			a.openRecordPeek(dbDoc, store, sp, rec, mode)
		},
		OnOpenRelationTarget: func(targetDB, ref string) {
			if sp == nil {
				return
			}
			_, tRecs, rerr := store.ResolveTargetDatabase(targetDB)
			if rerr == nil {
				if tr := database.MatchRelationRecord(tRecs, ref); tr != nil {
					if targetDB == "" || (dbDoc != nil && filepath.ToSlash(targetDB) == filepath.ToSlash(dbDoc.rel)) {
						a.openRecordPeek(dbDoc, store, sp, tr, dbview.OpenSidePeek)
						return
					}
					if tr.Path != "" {
						a.openIn(sp, tr.Path)
						return
					}
				}
			}
			if !strings.HasSuffix(ref, ".md") {
				cand := filepath.ToSlash(strings.TrimSpace(ref) + ".md")
				if cand != ".md" {
					a.openIn(sp, cand)
				}
			}
		},
		OnTrashRecords: func(backingPaths []string) {
			if sp == nil || sp.IsExternal() {
				return
			}
			anyDeleted := false
			for _, rel := range backingPaths {
				if a.trashDatabaseBackingPath(sp, rel) {
					anyDeleted = true
				}
			}
			if a.dbPeek.open && a.dbPeek.dbDoc == dbDoc && dbDoc != nil {
				if store.Record(a.dbPeek.recordID) == nil {
					a.closeRecordPeek()
				}
			}
			if dbDoc != nil {
				dbDoc.dbDirtySnapshot = true
			}
			if anyDeleted {
				a.refreshTree()
				a.refreshSearch()
			}
			a.refreshOpenDatabases(spaceIDOf(sp))
		},
		OnOpenURL: func(rawURL string) {
			if rawURL != "" && a.win != nil {
				mygo.Shell.OpenExternal(rawURL)
			}
		},
		OnNotify: func(msg string) {
			a.notify("%s", msg)
		},
	}
}

func spaceIDOf(sp *space.Space) string {
	if sp == nil {
		return ""
	}
	return sp.ID
}

func loadDatabaseRecordImage(sp *space.Space, dbDoc *document, recPath, imgURL string) *ui.Bitmap {
	imgURL = strings.TrimSpace(imgURL)
	if imgURL == "" || sp == nil || sp.Root == "" {
		return nil
	}
	if strings.HasPrefix(imgURL, "http://") || strings.HasPrefix(imgURL, "https://") || strings.HasPrefix(imgURL, "data:") {
		return nil
	}

	rel := filepath.ToSlash(recPath)
	if rel == "" && dbDoc != nil {
		rel = filepath.ToSlash(dbDoc.rel)
	}
	var full string
	var err error
	if rel != "" {
		full, err = resolveSpaceImage(sp, rel, imgURL)
	} else {
		full, err = vault.Resolve(sp.Root, filepath.ToSlash(imgURL))
	}
	if err != nil || full == "" {
		return nil
	}
	relToRoot, relErr := filepath.Rel(sp.Root, full)
	if relErr != nil || !filepath.IsLocal(relToRoot) || relToRoot == "." {
		return nil
	}
	resolved, resErr := vault.Resolve(sp.Root, filepath.ToSlash(relToRoot))
	if resErr != nil {
		return nil
	}
	full = resolved
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		return nil
	}

	modNano, size := info.ModTime().UnixNano(), info.Size()
	if dbDoc != nil {
		if dbDoc.closed.Load() {
			return nil
		}
		if dbDoc.imgCache == nil {
			dbDoc.imgCache = make(map[string]cachedImage)
		}
		cacheKey := "dbthumb:" + full
		if c, ok := dbDoc.imgCache[cacheKey]; ok && c.modNano == modNano && c.size == size && c.resource.Bitmap != nil {
			return c.resource.Bitmap
		}
		res := decodeLocalImageThumb(full)
		if res.Bitmap == nil {
			return nil
		}
		res.LoadedAt = time.Now()
		dbDoc.imgCache[cacheKey] = cachedImage{
			modNano:  modNano,
			size:     size,
			resource: res,
		}
		return res.Bitmap
	}

	return decodeLocalImageThumb(full).Bitmap
}

func (a *App) openRecordPeek(dbDoc *document, store *database.Store, sp *space.Space, rec *database.Record, mode string) {
	if rec == nil || sp == nil {
		return
	}
	if mode == "" {
		mode = dbview.OpenSidePeek
	}
	if dbDoc == nil && rec.Path != "" {
		a.closeRecordPeek()
		a.openIn(sp, rec.Path)
		return
	}
	if mode == dbview.OpenFullPage {
		targetPath := rec.Path
		a.closeRecordPeek()
		if targetPath != "" {
			a.openIn(sp, targetPath)
		}
		return
	}

	// Switching records: detach previous peek doc cleanly without leaking bitmaps.
	if a.dbPeek.doc != nil && a.dbPeek.doc.rel != rec.Path {
		a.detachPeekDoc()
	}

	a.dbPeek.open = true
	a.dbPeek.mode = mode
	a.dbPeek.dbDoc = dbDoc
	a.dbPeek.recordID = rec.ID

	if rec.Path == "" {
		a.dbPeek.doc = nil
		a.dbPeek.inlineBody = rec.Body
		a.dbPeek.inlineTargetID = rec.ID
		a.dbPeek.lastDocVer = 0
		return
	}

	if a.dbPeek.doc == nil || a.dbPeek.doc.space != sp || a.dbPeek.doc.rel != rec.Path {
		if existing := a.residentDocument(sp.ID, rec.Path); existing != nil && existing.loadErr == nil {
			a.dbPeek.doc = existing
		} else {
			text, err := readFile(sp, rec.Path)
			if err != nil {
				a.notify("打开记录失败。%v", err)
				a.closeRecordPeek()
				return
			}
			doc := openDocument(sp, rec.Path, text, false)
			a.dbPeek.doc = doc
		}
	}
	if a.dbPeek.doc != nil && a.dbPeek.doc.ed != nil {
		a.dbPeek.lastDocVer = a.dbPeek.doc.ed.Version()
		if a.dbPeek.doc.loadErr == nil {
			a.dbPeek.doc.ed.RequestFocus = true
		}
	}
}

func (a *App) detachPeekDoc() {
	prev := a.dbPeek.doc
	a.dbPeek.doc = nil
	a.dbPeek.lastDocVer = 0
	if prev == nil {
		return
	}
	if a.autosaveEnabled() && prev.dirty() && prev.loadErr == nil {
		_ = prev.save()
	}
	for _, r := range a.residentDocuments() {
		if r == prev {
			return
		}
	}
	prev.close()
}

func (a *App) closeRecordPeek() {
	if !a.dbPeek.open && a.dbPeek.doc == nil {
		return
	}
	a.detachPeekDoc()
	a.dbPeek.open = false
	a.dbPeek.mode = ""
	a.dbPeek.pendingMode = ""
	a.dbPeek.dbDoc = nil
	a.dbPeek.recordID = ""
	a.dbPeek.inlineBody = ""
	a.dbPeek.inlineTargetID = ""
	a.dbPeek.resizing = false
}

func (a *App) applyPendingDatabasePeekMode() {
	if a.dbPeek.pendingMode == "" {
		return
	}
	a.dbPeek.mode = a.dbPeek.pendingMode
	a.dbPeek.pendingMode = ""
}

func (a *App) syncPeekBodyEdits(c *ui.Context) {
	if !a.dbPeek.open || a.dbPeek.doc == nil {
		return
	}
	pd := a.dbPeek.doc
	if pd.loadErr != nil || pd.ed == nil {
		return
	}
	if ver := pd.ed.Version(); ver != a.dbPeek.lastDocVer {
		a.dbPeek.lastDocVer = ver
		if pd.space != nil {
			a.refreshOpenDatabases(pd.space.ID)
		}
	}
	if pd.justSaved {
		pd.justSaved = false
		if pd.space != nil {
			a.refreshOpenDatabases(pd.space.ID)
		}
		a.refreshSearch()
	}
}

func (a *App) databaseWorkspace(c *ui.Context, d *document, cardWidth float32) {
	if d == nil || !d.isDatabase() {
		return
	}
	opts := a.databaseOptionsFor(c, d)
	peekOpen := a.dbPeek.open && a.dbPeek.dbDoc == d && a.dbPeek.mode == dbview.OpenSidePeek
	if !peekOpen {
		dbview.View(c.Key(d.viewKey()), d.dbView, opts)
		return
	}

	a.syncPeekBodyEdits(c)

	if a.dbPeek.panelWidth <= 0 {
		a.dbPeek.panelWidth = min(float32(500), max(float32(340), cardWidth*0.44))
	}
	maxSide := max(float32(280), cardWidth-280)
	minSide := min(maxSide, float32(300))
	sideW := min(maxSide, max(minSide, a.dbPeek.panelWidth))

	ui.Row(c.Key(d.viewKey() + ":ws")).Grow(1).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch).Clip().Children(func() {
		ui.Column(c.Key(d.viewKey() + ":main")).Grow(1).MinWidth(0).MinHeight(0).Clip().Children(func() {
			dbview.View(c.Key(d.viewKey()), d.dbView, opts)
		})

		t := c.Theme()
		div := ui.Box(c.Key("db-peek-divider")).Width(6).HeightPercent(100).Center().Focusable().Role(ui.RoleSplitter).
			Label(i18n.T("调整预览面板宽度")).Tooltip(i18n.T("调整预览面板宽度")).Cursor(ui.CursorResizeEW)
		dx, _, held := div.Dragged()
		a.dbPeek.resizing = held
		if held && dx != 0 {
			a.dbPeek.panelWidth = min(maxSide, max(minSide, sideW-dx))
			c.After(0)
		}
		active := held || div.Hovered()
		lineCol := t.Border
		if active {
			lineCol = t.Accent.Alpha(0.55)
		}
		div.Children(func() {
			ui.Box(c).Width(1).HeightPercent(100).Background(lineCol)
		})

		dt := documentTheme(a.paperDark)
		drawer := ui.Themed(c.Key("db-side-peek"), dt).Width(sideW).Shrink(0).MinHeight(0).
			BorderWidth(0, 0, 0, 1).BorderColor(dt.Border).Clip()
		drawer.Children(func() {
			a.databasePeekContent(c, d, sideW)
		})
	})
}

func (a *App) databasePeekModal(c *ui.Context) {
	if !a.dbPeek.open || a.dbPeek.mode != dbview.OpenCenterPeek || a.dbPeek.dbDoc == nil {
		return
	}
	d := a.dbPeek.dbDoc
	if !d.isDatabase() {
		a.closeRecordPeek()
		return
	}
	a.syncPeekBodyEdits(c)

	open := true
	dt := documentTheme(a.paperDark)
	ui.DialogBase(c.Key("db-center-peek"), &open, func(backdrop, panel ui.Element) {
		backdrop.Background(black.Alpha(0.38)).Padding(28, 32)
		panel.Width(760).MaxWidthPercent(90).HeightPercent(86).MinHeight(360).
			Background(dt.Background).Radius(12).
			Border(1, dt.Border).
			Shadow(0, 18, 48, 0, black.Alpha(0.24)).Clip()
		panel.Children(func() {
			ui.Themed(c.Key("db-center-peek-theme"), dt).Grow(1).MinWidth(0).MinHeight(0).
				Background(ui.Transparent).Children(func() {
				a.databasePeekContent(c, d, 760)
			})
		})
	})
	if !open {
		a.closeRecordPeek()
	}
}

func (a *App) databasePeekContent(c *ui.Context, dbDoc *document, width float32) {
	if dbDoc == nil || !dbDoc.isDatabase() {
		return
	}
	store := dbDoc.dbView.Store
	rec := store.Record(a.dbPeek.recordID)
	if rec == nil {
		a.closeRecordPeek()
		return
	}
	t := c.Theme()
	opts := a.databaseOptionsFor(c, dbDoc)

	// Peek header bar with mode switcher (Side / Center / Full Page) & Close.
	ui.Row(c.Key("peek-header")).Height(40).Padding(0, 12, 0, 16).Gap(6).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		title := rec.Title
		if title == "" {
			title = vault.Title(rec.Path)
		}
		if title == "" {
			title = i18n.T("无标题")
		}
		ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)

		if iconButton(c, "peek-side", iconSidebar, i18n.T("侧边预览"), i18n.T("侧边预览"), a.dbPeek.mode == dbview.OpenSidePeek).Size(26, 26).Clicked() {
			if a.dbPeek.mode != dbview.OpenSidePeek {
				a.dbPeek.pendingMode = dbview.OpenSidePeek
				c.After(0)
			}
		}
		if iconButton(c, "peek-center", iconChevronsUpDown, i18n.T("居中弹窗"), i18n.T("居中弹窗"), a.dbPeek.mode == dbview.OpenCenterPeek).Size(26, 26).Clicked() {
			if a.dbPeek.mode != dbview.OpenCenterPeek {
				a.dbPeek.pendingMode = dbview.OpenCenterPeek
				c.After(0)
			}
		}
		if rec.Path != "" {
			if iconButton(c, "peek-full", iconMaximize2, i18n.T("打开为整页"), i18n.T("打开为整页"), false).Size(26, 26).Clicked() {
				a.openRecordPeek(dbDoc, store, dbDoc.space, rec, dbview.OpenFullPage)
			}
		}
		if iconButton(c, "peek-close", iconX, i18n.T("关闭预览"), i18n.T("关闭预览"), false).Size(26, 26).Clicked() {
			a.closeRecordPeek()
		}
	})

	// Collapsible property table above document body.
	dbview.PagePropertiesView(c.Key("peek-props"), store, rec.ID, dbDoc.dbView, &a.dbPeek.propsCollapsed, opts)

	// Record body editor or inline note editor.
	if pd := a.dbPeek.doc; pd != nil {
		if pd.loadErr != nil {
			ui.Box(c).Grow(1).Center().Padding(24).Children(func() {
				ui.Text(c, pd.loadErr.Error()).FontSize(13).TextColor(t.Danger).Wrap()
			})
			return
		}
		edOpts := a.editorOptionsFor(c, max(float32(220), width-24), false, pd)
		edOpts.Outline = false
		edOpts.SetOutline = nil
		edOpts.FullWidth = true
		edOpts.Column = max(float32(220), width-36)
		ui.Column(c.Key("peek-body")).Grow(1).MinWidth(0).MinHeight(0).Clip().Children(func() {
			editor.View(c.Key(pd.viewKey()+":peek"), pd.ed, edOpts)
		})
		return
	}

	// Inline database record body editor.
	ui.Column(c.Key("peek-inline-body")).Grow(1).MinWidth(0).MinHeight(0).Padding(16).Children(func() {
		ta := ui.TextArea(c.Key("inline-body"), &a.dbPeek.inlineBody).Grow(1).MinHeight(120).
			Placeholder(i18n.T("行内笔记内容…")).Padding(10).Radius(8).
			Background(t.Text.Alpha(0.03)).Border(1, t.Border).TextColor(t.Text)
		if ta.Changed() && !opts.ReadOnly {
			if r := store.Record(rec.ID); r != nil {
				r.Body = a.dbPeek.inlineBody
				dbDoc.dbDirtySnapshot = true
			}
		}
	})
}

func (a *App) databaseCorner(c *ui.Context, d *document) {
	if !d.isDatabase() {
		return
	}
	res := d.dbView.Store.Query(d.dbView.Store.Database().ActiveView, database.QueryOptions{Now: c.Now()})
	label := fmt.Sprintf(i18n.T("%d 条记录"), res.TotalCount)
	ui.Box(c.Key("db-corner-count")).Height(22).PaddingX(8).Radius(11).Center().
		Background(c.Theme().Text.Alpha(0.06)).Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(c.Theme().TextMuted).NoWrap()
	})
}

func (a *App) owningDatabaseFor(d *document) (*database.Store, string, *dbview.State, *document) {
	if d == nil || d.space == nil || d.loadErr != nil || d.isDatabase() || d.isWhiteboard() || d.isPDF() {
		return nil, "", nil, nil
	}
	if !vault.IsMarkdown(d.rel) {
		return nil, "", nil, nil
	}
	rel := filepath.ToSlash(d.rel)

	// 1. Prefer any open resident database in memory.
	for _, res := range a.residentDocuments() {
		if res != nil && res.space == d.space && res.loadErr == nil && res.isDatabase() {
			if rec := res.dbView.Store.RecordByPath(rel); rec != nil {
				return res.dbView.Store, rec.ID, res.dbView, res
			}
		}
	}

	// 2. Consult cached lookup for this document.
	key := d.space.ID + "\x00" + rel
	if entry, ok := a.pageDBCache[key]; ok {
		if entry == nil || entry.store == nil {
			return nil, "", nil, nil
		}
		if rec := entry.store.RecordByPath(rel); rec != nil {
			return entry.store, rec.ID, entry.state, nil
		}
		return nil, "", nil, nil
	}

	// 3. Probe candidate .db.md databases: first sibling <parentDir>.db.md, then any .db.md in space tree.
	var candidates []string
	if dir := parentDir(rel); dir != "" {
		candidates = append(candidates, dir+vault.ExtDatabase)
	}
	var walkTree func(nodes []*vault.Node)
	walkTree = func(nodes []*vault.Node) {
		for _, n := range nodes {
			if n.Dir {
				walkTree(n.Children)
			} else if vault.IsDatabase(n.Path) && !slices.Contains(candidates, n.Path) {
				candidates = append(candidates, n.Path)
			}
		}
	}
	tree, _ := a.treeOf(d.space)
	walkTree(tree)

	if a.pageDBCache == nil {
		a.pageDBCache = make(map[string]*pageDBEntry)
	}
	if len(candidates) == 0 {
		a.pageDBCache[key] = &pageDBEntry{}
		return nil, "", nil, nil
	}

	var live []database.LiveDoc
	for _, cand := range candidates {
		raw, err := readFile(d.space, cand)
		if err != nil {
			continue
		}
		db, err := database.DecodeDatabaseMarkdown(raw)
		if err != nil {
			continue
		}
		spaceRoot, dbRel := databaseSpaceRoot(d.space, cand)
		store := database.NewStore(spaceRoot, dbRel, db)
		srcDir := store.ResolveSourceDir()
		if db.Source.Type != database.SourceSpace && srcDir != "" && !strings.HasPrefix(rel, srcDir+"/") {
			continue
		}
		if live == nil {
			live = []database.LiveDoc{{Path: rel, Text: d.text(), UpdatedAt: time.Now()}}
		}
		_, _ = store.Refresh(live)
		if rec := store.RecordByPath(rel); rec != nil {
			st := dbview.NewState(store)
			a.pageDBCache[key] = &pageDBEntry{dbRel: cand, store: store, state: st}
			return store, rec.ID, st, nil
		}
	}

	a.pageDBCache[key] = &pageDBEntry{}
	return nil, "", nil, nil
}

func (a *App) pagePropertiesHeader(c *ui.Context, d *document) {
	if d == nil || d.source || d.loadErr != nil || d.isDatabase() || d.isWhiteboard() || d.isPDF() {
		return
	}
	store, recID, st, dbDoc := a.owningDatabaseFor(d)
	if store == nil || recID == "" {
		return
	}
	if a.pagePropsCollapsed == nil {
		a.pagePropsCollapsed = make(map[string]bool)
	}
	key := d.space.ID + "\x00" + d.rel
	collapsed := a.pagePropsCollapsed[key]
	opts := a.databaseOptionsForStore(c, d.space, store, dbDoc)
	ui.Themed(c.Key("page-props:"+key), documentTheme(a.paperDark)).Background(ui.Transparent).Children(func() {
		dbview.PagePropertiesView(c.Key("page-props-view:"+key), store, recID, st, &collapsed, opts)
	})
	a.pagePropsCollapsed[key] = collapsed
}

func siblingsInNodes(tree []*vault.Node, dir string) []*vault.Node {
	if dir == "" || dir == "." {
		return tree
	}
	if parent := vault.Find(tree, dir); parent != nil && parent.Dir {
		return parent.Children
	}
	return nil
}

// managedFolderFor returns the sibling folder managed by dbNode (i.e. "<stem>/"
// alongside "<stem>.db.md") so the sidebar can fold it cleanly underneath.
func (a *App) managedFolderFor(sp *space.Space, dbNode *vault.Node) *vault.Node {
	if sp == nil || dbNode == nil || dbNode.Dir || !vault.IsDatabase(dbNode.Path) {
		return nil
	}
	tree, _ := a.treeOf(sp)
	sibs := siblingsInNodes(tree, parentDir(dbNode.Path))
	target := strings.TrimSuffix(dbNode.Path, vault.ExtDatabase)
	for _, s := range sibs {
		if s.Dir && s.Path == target {
			return s
		}
	}
	return nil
}

// isFoldedDBFolder reports whether folder n is hidden at its own level because
// its sibling "<name>.db.md" row is visible in the tree and folds it underneath.
func (a *App) isFoldedDBFolder(sp *space.Space, n *vault.Node) bool {
	if sp == nil || n == nil || !n.Dir || sp.IsPinned(n.Path) {
		return false
	}
	tree, _ := a.treeOf(sp)
	sibs := siblingsInNodes(tree, parentDir(n.Path))
	dbPath := n.Path + vault.ExtDatabase
	for _, s := range sibs {
		if !s.Dir && s.Path == dbPath {
			if a.state.IsFavorite(sp.ID, dbPath) || a.grouped(sp, dbPath) {
				return false
			}
			return true
		}
	}
	return false
}

type embedDBEntry struct {
	dbRel string
	store *database.Store
}

func (a *App) resolveDatabaseRef(d *document, target string) (*space.Space, string, bool) {
	if d == nil || d.space == nil || d.space.IsExternal() {
		return nil, "", false
	}
	sp := d.space
	clean := path.Clean(strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(target)), "/"))
	if clean == "" || clean == "." {
		return nil, "", false
	}
	var cands []string
	if dir := path.Dir(filepath.ToSlash(d.rel)); dir != "" && dir != "." {
		relCand := path.Clean(path.Join(dir, clean))
		if filepath.IsLocal(filepath.FromSlash(relCand)) {
			cands = append(cands, relCand)
		}
	}
	if filepath.IsLocal(filepath.FromSlash(clean)) {
		cands = append(cands, clean)
	}
	for _, cand := range cands {
		if !vault.IsDatabase(cand) {
			continue
		}
		if full, err := resolveFile(sp, cand); err == nil {
			if st, serr := os.Stat(full); serr == nil && !st.IsDir() {
				return sp, cand, true
			}
		}
	}
	if infos, err := database.ListSpaceDatabases(sp.Root); err == nil {
		for _, info := range infos {
			if info.ID == target || info.Title == target || info.RelPath == clean || strings.TrimSuffix(info.RelPath, vault.ExtDatabase) == clean {
				return sp, info.RelPath, true
			}
		}
	}
	return nil, "", false
}

func (a *App) loadDatabaseFor(d *document, target string) (*database.Store, dbview.Options, uint64, error) {
	sp, dbRel, ok := a.resolveDatabaseRef(d, target)
	if !ok {
		return nil, dbview.Options{}, 0, fmt.Errorf(i18n.T("找不到关联数据库：%s"), target)
	}
	if res := a.residentDocument(sp.ID, dbRel); res != nil && res.loadErr == nil && res.isDatabase() {
		opts := a.databaseOptionsForStore(nil, sp, res.dbView.Store, res)
		opts.Embedded = true
		return res.dbView.Store, opts, res.dbView.Store.Revision(), nil
	}
	key := sp.ID + "\x00" + dbRel
	if a.embedDBs != nil {
		if entry, exists := a.embedDBs[key]; exists && entry != nil && entry.store != nil {
			opts := a.databaseOptionsForStore(nil, sp, entry.store, nil)
			opts.Embedded = true
			return entry.store, opts, entry.store.Revision(), nil
		}
	}
	store, err := database.Open(sp.Root, dbRel, a.liveDocsForSpace(sp))
	if err != nil {
		return nil, dbview.Options{}, 0, err
	}
	if a.embedDBs == nil {
		a.embedDBs = make(map[string]*embedDBEntry)
	}
	a.embedDBs[key] = &embedDBEntry{dbRel: dbRel, store: store}
	opts := a.databaseOptionsForStore(nil, sp, store, nil)
	opts.Embedded = true
	return store, opts, store.Revision(), nil
}

func (a *App) openDatabaseFrom(d *document, target, viewID string, split bool) {
	sp, rel, ok := a.resolveDatabaseRef(d, target)
	if !ok {
		a.notify("找不到关联数据库：%s", target)
		return
	}
	applyView := func(td *document) {
		if td != nil && td.isDatabase() && viewID != "" {
			td.dbView.SetActiveView(viewID)
		}
	}
	if split {
		a.openSplit(sp, rel, false, false)
		applyView(a.residentDocument(sp.ID, rel))
		return
	}
	a.navigateInPane(d, func() {
		a.openIn(sp, rel)
		if a.doc != nil && a.doc.rel == rel {
			applyView(a.doc)
		}
	})
}

func (a *App) createEmbeddedDatabaseFor(d *document, title string) (string, string, string, error) {
	if d == nil || d.space == nil || d.space.IsExternal() {
		return "", "", "", errors.New("external space is read-only")
	}
	sp := d.space
	docDir := path.Dir(filepath.ToSlash(d.rel))
	if docDir == "." {
		docDir = ""
	}
	baseTitle := strings.TrimSpace(title)
	if baseTitle == "" {
		baseTitle = i18n.T(untitledDatabase)
	}
	db := database.NewDefault(baseTitle, database.SourceConfig{Type: database.SourceFolder})
	content, err := database.EncodeDatabaseMarkdownRelative(docDir, db, nil)
	if err != nil {
		return "", "", "", err
	}
	rel, err := vault.CreateFile(sp.Root, docDir, baseTitle, vault.ExtDatabase, content)
	if err != nil {
		return "", "", "", err
	}
	rel = filepath.ToSlash(rel)
	if actualTitle := vault.Title(rel); actualTitle != "" && actualTitle != db.Title {
		db.Title = actualTitle
		if updated, encErr := database.EncodeDatabaseMarkdownRelative(docDir, db, nil); encErr == nil {
			_ = writeFile(sp, rel, updated)
		}
	}
	if a.embedDBs == nil {
		a.embedDBs = make(map[string]*embedDBEntry)
	}
	store := database.NewStore(sp.Root, rel, db)
	a.embedDBs[sp.ID+"\x00"+rel] = &embedDBEntry{dbRel: rel, store: store}
	a.refreshTree()
	a.refreshSearch()
	snap := database.RenderGFMSnapshot(docDir, db, nil)
	return rel, db.ActiveView, snap, nil
}

func (a *App) databaseSnapshotFor(d *document, target, viewID string) string {
	store, _, _, err := a.loadDatabaseFor(d, target)
	if err != nil || store == nil || store.Database() == nil {
		return ""
	}
	docDir := ""
	if d != nil {
		docDir = path.Dir(filepath.ToSlash(d.rel))
		if docDir == "." {
			docDir = ""
		}
	}
	db := store.Database()
	res := store.Query(viewID, database.QueryOptions{Now: time.Now()})
	return database.RenderGFMSnapshot(docDir, db, res)
}

func (a *App) exportDatabaseCSV(d *document) (string, error) {
	if d == nil || !d.isDatabase() || d.space == nil || d.loadErr != nil {
		return "", errors.New("no active database")
	}
	viewID := ""
	if v := d.dbView.ActiveView(); v != nil {
		viewID = v.ID
	}
	csvBytes, err := d.dbView.Store.ExportViewCSV(viewID, database.QueryOptions{Now: time.Now()}, database.CSVExportOptions{IncludeBOM: true})
	if err != nil {
		a.notify("导出失败。%v", err)
		return "", err
	}
	root, nameOnDisk, err := fileLocation(d.space, d.rel)
	if err != nil {
		a.notify("导出失败。%v", err)
		return "", err
	}
	dir := path.Dir(nameOnDisk)
	if dir == "." {
		dir = ""
	}
	baseName := sanitizeName(strings.TrimSuffix(d.title(), vault.ExtDatabase))
	if baseName == "" {
		baseName = "database"
	}
	for i := 1; i < 1000; i++ {
		name := baseName
		if i > 1 {
			name = fmt.Sprintf("%s %d", baseName, i)
		}
		rel := path.Join(dir, name+".csv")
		full, rerr := vault.Resolve(root, rel)
		if rerr != nil {
			a.notify("导出失败。%v", rerr)
			return "", rerr
		}
		err = fsutil.CreateFileAtomic(full, csvBytes, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			a.notify("导出失败。%v", err)
			return "", err
		}
		a.notify("已导出 %s", rel)
		a.refreshTree()
		if a.win != nil {
			mygo.Shell.ShowItemInFolder(full)
		}
		return filepath.ToSlash(rel), nil
	}
	err = errors.New("too many csv exports")
	a.notify("导出失败。%v", err)
	return "", err
}

func (a *App) importCSVFromPath(sp *space.Space, dir, csvPath string, intoDoc *document) (string, database.CSVImportResult, error) {
	var res database.CSVImportResult
	if sp == nil || sp.IsExternal() || sp.Root == "" {
		return "", res, errors.New("external space is read-only")
	}
	f, err := os.Open(csvPath)
	if err != nil {
		a.notify("导入 CSV 失败。%v", err)
		return "", res, err
	}
	defer f.Close()

	if intoDoc != nil && intoDoc.isDatabase() && intoDoc.space == sp && intoDoc.loadErr == nil {
		res, err = database.ImportCSVIntoStore(intoDoc.dbView.Store, f)
		if err != nil {
			a.notify("导入 CSV 失败。%v", err)
			return intoDoc.rel, res, err
		}
		intoDoc.dbDirtySnapshot = true
		_ = intoDoc.writeDatabase(false)
		a.refreshOpenDatabases(sp.ID)
		a.refreshTree()
		a.refreshSearch()
		a.notify(fmt.Sprintf(i18n.T("已导入 %d 条记录"), res.CreatedRecords))
		return intoDoc.rel, res, nil
	}

	title := strings.TrimSuffix(filepath.Base(csvPath), filepath.Ext(csvPath))
	dbRel, res, err := database.ImportCSVAsNewDatabase(sp.Root, dir, title, f)
	if err != nil {
		a.notify("导入 CSV 失败。%v", err)
		return "", res, err
	}
	a.invalidatePageDBCache(sp.ID)
	a.refreshTree()
	a.refreshSearch()
	if dir != "" {
		sp.SetExpanded(dir, true)
		a.saveState()
	}
	a.openIn(sp, dbRel)
	a.notify(fmt.Sprintf(i18n.T("已导入 %d 条记录"), res.CreatedRecords))
	return dbRel, res, nil
}

func (a *App) importNotionFromPath(sp *space.Space, dir, srcPath string) (database.NotionImportResult, error) {
	var res database.NotionImportResult
	if sp == nil || sp.IsExternal() || sp.Root == "" {
		return res, errors.New("external space is read-only")
	}
	info, err := os.Stat(srcPath)
	if err != nil {
		a.notify("导入 Notion 失败。%v", err)
		return res, err
	}
	if info.IsDir() {
		res, err = database.ImportNotionDir(sp.Root, dir, srcPath)
	} else {
		res, err = database.ImportNotionZIP(sp.Root, dir, srcPath)
	}
	if err != nil {
		a.notify("导入 Notion 失败。%v", err)
		return res, err
	}
	a.invalidatePageDBCache(sp.ID)
	a.refreshTree()
	a.refreshSearch()
	if len(res.Databases) > 0 {
		a.openIn(sp, res.Databases[0])
	} else if len(res.StandaloneNotes) > 0 {
		a.openIn(sp, res.StandaloneNotes[0])
	}
	a.notify(fmt.Sprintf(i18n.T("已导入 %d 个数据库，%d 条记录，%d 篇独立文档"), len(res.Databases), res.Records, len(res.StandaloneNotes)))
	return res, nil
}

func (a *App) pickAndImportCSV(intoDoc *document) {
	sp := a.state.ActiveSpace()
	if sp == nil || sp.IsExternal() || a.win == nil {
		return
	}
	dir := a.currentDir()
	win := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:  win,
			Title:   i18n.T("选择 CSV 文件…"),
			Filters: []mygo.FileFilter{{Name: "CSV", Extensions: []string{"csv"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		p := paths[0]
		win.Update(func() {
			_, _, _ = a.importCSVFromPath(sp, dir, p, intoDoc)
		})
	}()
}

func (a *App) pickAndImportNotionZIP() {
	sp := a.state.ActiveSpace()
	if sp == nil || sp.IsExternal() || a.win == nil {
		return
	}
	dir := a.currentDir()
	win := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:  win,
			Title:   i18n.T("选择 Notion 导出 ZIP…"),
			Filters: []mygo.FileFilter{{Name: "ZIP", Extensions: []string{"zip"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		p := paths[0]
		win.Update(func() {
			_, _ = a.importNotionFromPath(sp, dir, p)
		})
	}()
}

func (a *App) pickAndImportNotionFolder() {
	sp := a.state.ActiveSpace()
	if sp == nil || sp.IsExternal() || a.win == nil {
		return
	}
	dir := a.currentDir()
	win := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:    win,
			Title:     i18n.T("选择 Notion 导出文件夹…"),
			Directory: true,
		})
		if err != nil || len(paths) == 0 {
			return
		}
		p := paths[0]
		win.Update(func() {
			_, _ = a.importNotionFromPath(sp, dir, p)
		})
	}()
}
