package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/Ciao1019/ArcMD/internal/history"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/merge"
	"github.com/Ciao1019/ArcMD/internal/plugin"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	uiglass "github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

const mergeRowH float32 = 22

var conflictCopyPattern = regexp.MustCompile(`^(.*) \(冲突副本 [^)]+\)(\.[^./]+)$`)

// persistedConflict matches plugins/cloud-sync's conflicts.json schema.
type persistedConflict struct {
	SpaceID          string `json:"spaceId"`
	SpaceName        string `json:"spaceName"`
	Path             string `json:"path"`
	ConflictCopyPath string `json:"conflictCopyPath,omitempty"`
	BaseText         string `json:"baseText"`
	LocalText        string `json:"localText"`
	RemoteText       string `json:"remoteText"`
	RemoteSize       int64  `json:"remoteSize,omitempty"`
	RemoteETag       string `json:"remoteEtag,omitempty"`
	RemoteModUnix    int64  `json:"remoteModUnix,omitempty"`
	CreatedUnix      int64  `json:"createdUnix"`
}

type persistedBaseline struct {
	Version int                             `json:"version"`
	Files   map[string]persistedBaselineRow `json:"files"`
}

type persistedBaselineRow struct {
	LocalSize     int64  `json:"localSize"`
	LocalModNano  int64  `json:"localModNano"`
	LocalSHA256   string `json:"localSha256"`
	RemoteSize    int64  `json:"remoteSize"`
	RemoteETag    string `json:"remoteEtag,omitempty"`
	RemoteModUnix int64  `json:"remoteModUnix,omitempty"`
}

// gitChangeEntry is one changed or conflicted file shown in the Git Changes modal.
type gitChangeEntry struct {
	Key              string
	SpaceID          string
	SpaceName        string
	Path             string
	ConflictCopyPath string
	RemoteSize       int64
	RemoteETag       string
	RemoteModUnix    int64
	Status           merge.FileStatus
	BaseText         string
	OldText          string
	NewText          string
	RemoteText       string
	Diff             merge.FileDiff
	Collapsed        bool
	ExpandedSkips    map[int]bool
}

type gitChangesState struct {
	open        bool
	items       []gitChangeEntry
	copiedKey   string
	copiedUntil time.Time
	scroll      ui.ScrollState
}

type threeWayMergeState struct {
	open             bool
	spaceID          string
	spaceName        string
	rel              string
	conflictCopyPath string
	remoteSize       int64
	remoteETag       string
	remoteModUnix    int64
	hasRemoteMeta    bool
	diskConflictDoc  *document

	leftTitle  string
	rightTitle string

	session     *merge.Session
	editMode    int // 0 = 3-pane IDEA merge, 1 = full result editor
	rawResult   string
	editingLine int
	lineBuf     string
	focusChunk  int
	scroll      ui.ScrollState
}

// parseConflictCopyPath checks whether rel is a "(冲突副本 ...)" file and
// returns the original target document path if so.
func parseConflictCopyPath(rel string) (origRel string, ok bool) {
	dir := path.Dir(rel)
	base := path.Base(rel)
	// Handle compound extensions like .excalidraw.svg first.
	if strings.HasSuffix(strings.ToLower(base), ".excalidraw.svg") {
		stem := base[:len(base)-len(".excalidraw.svg")]
		if idx := strings.LastIndex(stem, " (冲突副本 "); idx > 0 && strings.HasSuffix(stem, ")") {
			orig := stem[:idx] + ".excalidraw.svg"
			if dir != "." {
				return dir + "/" + orig, true
			}
			return orig, true
		}
	}
	m := conflictCopyPattern.FindStringSubmatch(base)
	if len(m) != 3 {
		return "", false
	}
	orig := m[1] + m[2]
	if dir != "." {
		return dir + "/" + orig, true
	}
	return orig, true
}

func (a *App) cloudSyncStateDir() string {
	if a.pluginsUI.cfg == nil {
		return ""
	}
	p, err := a.pluginsUI.cfg.StatePath(cloudSyncPluginID, "conflicts.json")
	if err != nil {
		return ""
	}
	return filepath.Dir(p)
}

func (a *App) loadPersistedConflicts() []persistedConflict {
	dir := a.cloudSyncStateDir()
	if dir == "" {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, "conflicts.json"))
	if err != nil || len(raw) == 0 {
		return nil
	}
	var items []persistedConflict
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	// Filter out entries whose conflict copy was already deleted manually and
	// whose main file is clean.
	var valid []persistedConflict
	for _, it := range items {
		sp := a.state.Get(it.SpaceID)
		if sp == nil || sp.Root == "" {
			continue
		}
		if it.ConflictCopyPath != "" {
			cfFull, err := vault.Resolve(sp.Root, it.ConflictCopyPath)
			if err != nil {
				continue
			}
			if _, statErr := os.Stat(cfFull); os.IsNotExist(statErr) {
				continue
			}
		} else {
			mainFull, err := vault.Resolve(sp.Root, it.Path)
			if err != nil {
				continue
			}
			if _, statErr := os.Stat(mainFull); os.IsNotExist(statErr) {
				continue
			}
		}
		valid = append(valid, it)
	}
	return valid
}

func (a *App) removePersistedConflict(spaceID, rel string) {
	dir := a.cloudSyncStateDir()
	if dir == "" {
		return
	}
	file := filepath.Join(dir, "conflicts.json")
	raw, err := os.ReadFile(file)
	if err != nil || len(raw) == 0 {
		return
	}
	var items []persistedConflict
	if json.Unmarshal(raw, &items) != nil {
		return
	}
	var kept []persistedConflict
	for _, it := range items {
		if it.SpaceID == spaceID && (it.Path == rel || it.ConflictCopyPath == rel) {
			continue
		}
		kept = append(kept, it)
	}
	if len(kept) == 0 {
		_ = os.Remove(file)
		return
	}
	if out, err := json.Marshal(kept); err == nil {
		_ = fsutil.WriteFileAtomic(file, out, 0o600)
	}
}

func readCloudBaseSnapshot(stateDir, spaceID, rel string) (string, bool) {
	if stateDir == "" {
		return "", false
	}
	sum := sha256.Sum256([]byte(spaceID + "\x00" + rel))
	key := "base_" + hex.EncodeToString(sum[:])[:32] + ".txt"
	b, err := os.ReadFile(filepath.Join(stateDir, key))
	if err != nil {
		return "", false
	}
	return string(b), true
}

func writeCloudBaseSnapshot(stateDir, spaceID, rel, content string) {
	if stateDir == "" {
		return
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return
	}
	sum := sha256.Sum256([]byte(spaceID + "\x00" + rel))
	key := "base_" + hex.EncodeToString(sum[:])[:32] + ".txt"
	_ = fsutil.WriteFileAtomic(filepath.Join(stateDir, key), []byte(content), 0o600)
}

func readCloudBaseline(stateDir, spaceID string) (persistedBaseline, bool) {
	if stateDir == "" {
		return persistedBaseline{}, false
	}
	safe := strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, spaceID)
	b, err := os.ReadFile(filepath.Join(stateDir, "baseline_"+safe+".json"))
	if err != nil || len(b) == 0 {
		return persistedBaseline{}, false
	}
	var pb persistedBaseline
	if json.Unmarshal(b, &pb) != nil || pb.Files == nil {
		return persistedBaseline{}, false
	}
	return pb, true
}

// markBaselineResolvedAgainstRemote updates baseline_<spaceID>.json so that the
// immediate post-merge sync sees Remote as unchanged (unless a newer ETag arrived)
// and uploads the freshly merged local document cleanly.
func markBaselineResolvedAgainstRemote(stateDir, spaceID, rel string, rSize int64, rETag string, rModUnix int64) {
	if stateDir == "" {
		return
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return
	}
	safe := strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, spaceID)
	file := filepath.Join(stateDir, "baseline_"+safe+".json")
	pb, ok := readCloudBaseline(stateDir, spaceID)
	if !ok || pb.Files == nil {
		pb = persistedBaseline{Version: 1, Files: map[string]persistedBaselineRow{}}
	}
	row := pb.Files[rel]
	row.LocalSHA256 = "resolved-pending-upload"
	row.RemoteSize = rSize
	row.RemoteETag = rETag
	row.RemoteModUnix = rModUnix
	pb.Files[rel] = row
	if out, err := json.Marshal(pb); err == nil {
		_ = fsutil.WriteFileAtomic(file, out, 0o600)
	}
}

func isDiffableDocPath(rel string) bool {
	ext := strings.ToLower(path.Ext(rel))
	switch ext {
	case ".md", ".markdown", ".mdx", ".arcmd", ".txt":
		return true
	}
	return false
}

func (a *App) currentDocumentText(sp *space.Space, rel string) (string, bool) {
	for _, d := range a.residentDocuments() {
		if d.space != nil && d.space.ID == sp.ID && d.rel == rel && d.loadErr == nil && !d.isWhiteboard() {
			return d.text(), true
		}
	}
	full, err := vault.Resolve(sp.Root, rel)
	if err != nil {
		return "", false
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// findConflictForDoc returns a Cloud Sync conflict or orphan "(冲突副本 ...)"
// match for the given document if one exists.
func (a *App) findConflictForDoc(d *document) (persistedConflict, bool) {
	if d == nil || d.space == nil || d.space.IsExternal() || d.isWhiteboard() || d.isPDF() {
		return persistedConflict{}, false
	}
	for _, it := range a.loadPersistedConflicts() {
		if it.SpaceID == d.space.ID && (it.Path == d.rel || (it.ConflictCopyPath != "" && it.ConflictCopyPath == d.rel)) {
			// Refresh LocalText from live editor / current main file so edits made since sync are kept.
			if cur, ok := a.currentDocumentText(d.space, it.Path); ok {
				it.LocalText = cur
			}
			if it.ConflictCopyPath != "" {
				if rem, ok := a.currentDocumentText(d.space, it.ConflictCopyPath); ok {
					it.RemoteText = rem
				}
			}
			return it, true
		}
	}

	// Also detect any standalone "(冲突副本 ...)" file or its sibling main file on disk.
	if origRel, ok := parseConflictCopyPath(d.rel); ok {
		origText, hasOrig := a.currentDocumentText(d.space, origRel)
		copyText, hasCopy := a.currentDocumentText(d.space, d.rel)
		if hasOrig && hasCopy {
			baseText, _ := readCloudBaseSnapshot(a.cloudSyncStateDir(), d.space.ID, origRel)
			return persistedConflict{
				SpaceID:          d.space.ID,
				SpaceName:        d.space.Name,
				Path:             origRel,
				ConflictCopyPath: d.rel,
				BaseText:         baseText,
				LocalText:        origText,
				RemoteText:       copyText,
			}, true
		}
	}
	return persistedConflict{}, false
}

// openGitChanges scans synced / active Spaces and opens the Git-style Changes & Diff modal.
func (a *App) openGitChanges() {
	a.refreshGitChanges()
	a.gitChanges.open = true
}

func (a *App) refreshGitChanges() {
	prevCollapsed := map[string]bool{}
	prevSkips := map[string]map[int]bool{}
	for _, old := range a.gitChanges.items {
		prevCollapsed[old.Key] = old.Collapsed
		prevSkips[old.Key] = old.ExpandedSkips
	}

	stateDir := a.cloudSyncStateDir()
	var entries []gitChangeEntry
	seenConflict := map[string]bool{}

	addEntry := func(e gitChangeEntry) {
		if c, ok := prevCollapsed[e.Key]; ok {
			e.Collapsed = c
		}
		if sk, ok := prevSkips[e.Key]; ok && sk != nil {
			e.ExpandedSkips = sk
		} else {
			e.ExpandedSkips = map[int]bool{}
		}
		entries = append(entries, e)
	}

	// 1. Open editor external-disk conflicts (watch.go d.conflict).
	for _, d := range a.residentDocuments() {
		if d.space == nil || !d.conflict || d.isWhiteboard() {
			continue
		}
		key := "disk-conflict:" + d.space.ID + ":" + d.rel
		seenConflict[d.space.ID+":"+d.rel] = true
		mine := d.text()
		fd := merge.UnifiedDiff(d.diskNow, mine, 3)
		addEntry(gitChangeEntry{
			Key:        key,
			SpaceID:    d.space.ID,
			SpaceName:  d.space.Name,
			Path:       d.rel,
			Status:     merge.StatusConflict,
			BaseText:   d.diskText,
			OldText:    d.diskNow,
			NewText:    mine,
			RemoteText: d.diskNow,
			Diff:       fd,
		})
	}

	// 2. Cloud Sync persisted conflicts.
	for _, cf := range a.loadPersistedConflicts() {
		sp := a.state.Get(cf.SpaceID)
		if sp == nil {
			continue
		}
		localText := cf.LocalText
		if cur, ok := a.currentDocumentText(sp, cf.Path); ok {
			localText = cur
		}
		remoteText := cf.RemoteText
		if cf.ConflictCopyPath != "" {
			if curRem, ok := a.currentDocumentText(sp, cf.ConflictCopyPath); ok {
				remoteText = curRem
			}
		}
		seenConflict[cf.SpaceID+":"+cf.Path] = true
		if cf.ConflictCopyPath != "" {
			seenConflict[cf.SpaceID+":"+cf.ConflictCopyPath] = true
		}
		key := "sync-conflict:" + cf.SpaceID + ":" + cf.Path
		addEntry(gitChangeEntry{
			Key:              key,
			SpaceID:          cf.SpaceID,
			SpaceName:        sp.Name,
			Path:             cf.Path,
			ConflictCopyPath: cf.ConflictCopyPath,
			RemoteSize:       cf.RemoteSize,
			RemoteETag:       cf.RemoteETag,
			RemoteModUnix:    cf.RemoteModUnix,
			Status:           merge.StatusConflict,
			BaseText:         cf.BaseText,
			OldText:          remoteText,
			NewText:          localText,
			RemoteText:       remoteText,
			Diff:             merge.UnifiedDiff(remoteText, localText, 3),
		})
	}

	// 3. Per-space Git working-tree changes against Cloud Sync baseline (or session history fallback).
	for _, sp := range a.state.Spaces {
		if sp.IsExternal() || sp.Root == "" {
			continue
		}
		baseManifest, hasCloudBase := readCloudBaseline(stateDir, sp.ID)

		// Walk current syncable text documents in the Space.
		var curPaths []string
		_ = filepath.WalkDir(sp.Root, func(full string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			relOS, rerr := filepath.Rel(sp.Root, full)
			if rerr != nil || relOS == "." {
				return nil
			}
			rel := filepath.ToSlash(relOS)
			if d.IsDir() {
				if rel == ".git" || rel == "node_modules" || rel == ".arcmd/history" ||
					(strings.HasPrefix(path.Base(rel), ".") && rel != ".arcmd") {
					return filepath.SkipDir
				}
				return nil
			}
			if plugin.IsSyncPath(rel) && isDiffableDocPath(rel) {
				curPaths = append(curPaths, rel)
			}
			return nil
		})
		sort.Strings(curPaths)
		curSet := make(map[string]bool, len(curPaths))
		for _, rel := range curPaths {
			curSet[rel] = true
		}

		// Detect standalone "(冲突副本 ...)" files not in conflicts.json.
		for _, rel := range curPaths {
			origRel, isCopy := parseConflictCopyPath(rel)
			if !isCopy || seenConflict[sp.ID+":"+origRel] || seenConflict[sp.ID+":"+rel] {
				continue
			}
			origText, hasOrig := a.currentDocumentText(sp, origRel)
			copyText, hasCopy := a.currentDocumentText(sp, rel)
			if !hasOrig || !hasCopy {
				continue
			}
			seenConflict[sp.ID+":"+origRel] = true
			seenConflict[sp.ID+":"+rel] = true
			baseText, _ := readCloudBaseSnapshot(stateDir, sp.ID, origRel)
			addEntry(gitChangeEntry{
				Key:              "copy-conflict:" + sp.ID + ":" + origRel,
				SpaceID:          sp.ID,
				SpaceName:        sp.Name,
				Path:             origRel,
				ConflictCopyPath: rel,
				Status:           merge.StatusConflict,
				BaseText:         baseText,
				OldText:          copyText,
				NewText:          origText,
				RemoteText:       copyText,
				Diff:             merge.UnifiedDiff(copyText, origText, 3),
			})
		}

		if hasCloudBase {
			for _, rel := range curPaths {
				if seenConflict[sp.ID+":"+rel] {
					continue
				}
				curText, ok := a.currentDocumentText(sp, rel)
				if !ok {
					continue
				}
				bRow, tracked := baseManifest.Files[rel]
				if !tracked {
					addEntry(gitChangeEntry{
						Key:       "added:" + sp.ID + ":" + rel,
						SpaceID:   sp.ID,
						SpaceName: sp.Name,
						Path:      rel,
						Status:    merge.StatusAdded,
						OldText:   "",
						NewText:   curText,
						Diff:      merge.UnifiedDiff("", curText, 3),
					})
					continue
				}
				sum := sha256.Sum256([]byte(curText))
				if hex.EncodeToString(sum[:]) == bRow.LocalSHA256 {
					continue
				}
				oldText, _ := readCloudBaseSnapshot(stateDir, sp.ID, rel)
				addEntry(gitChangeEntry{
					Key:       "mod:" + sp.ID + ":" + rel,
					SpaceID:   sp.ID,
					SpaceName: sp.Name,
					Path:      rel,
					Status:    merge.StatusModified,
					BaseText:  oldText,
					OldText:   oldText,
					NewText:   curText,
					Diff:      merge.UnifiedDiff(oldText, curText, 3),
				})
			}

			// Deleted locally since last Cloud Sync.
			var basePaths []string
			for rel := range baseManifest.Files {
				if isDiffableDocPath(rel) && !curSet[rel] && !seenConflict[sp.ID+":"+rel] {
					basePaths = append(basePaths, rel)
				}
			}
			sort.Strings(basePaths)
			for _, rel := range basePaths {
				oldText, _ := readCloudBaseSnapshot(stateDir, sp.ID, rel)
				addEntry(gitChangeEntry{
					Key:       "del:" + sp.ID + ":" + rel,
					SpaceID:   sp.ID,
					SpaceName: sp.Name,
					Path:      rel,
					Status:    merge.StatusDeleted,
					BaseText:  oldText,
					OldText:   oldText,
					NewText:   "",
					Diff:      merge.UnifiedDiff(oldText, "", 3),
					Collapsed: true,
				})
			}
			continue
		}

		// Local-only Space fallback: compare active/edited documents against their
		// session-opening snapshot in .arcmd/history so Git Diff works even before
		// setting up Cloud Sync.
		if a.state.ActiveSpace() == nil || a.state.ActiveSpace().ID != sp.ID {
			continue
		}
		for _, rel := range curPaths {
			if seenConflict[sp.ID+":"+rel] {
				continue
			}
			curText, ok := a.currentDocumentText(sp, rel)
			if !ok {
				continue
			}
			vers, err := history.List(sp.Root, rel)
			if err != nil || len(vers) == 0 {
				continue
			}
			// Compare against the oldest kept session-start version or latest milestone.
			pickVer := vers[0]
			for _, v := range vers {
				if v.Kept {
					pickVer = v
					break
				}
			}
			oldText, rerr := history.Read(sp.Root, rel, pickVer)
			if rerr != nil || oldText == curText {
				continue
			}
			addEntry(gitChangeEntry{
				Key:       "hist:" + sp.ID + ":" + rel,
				SpaceID:   sp.ID,
				SpaceName: sp.Name,
				Path:      rel,
				Status:    merge.StatusModified,
				BaseText:  oldText,
				OldText:   oldText,
				NewText:   curText,
				Diff:      merge.UnifiedDiff(oldText, curText, 3),
			})
		}
	}

	a.gitChanges.items = entries
}

// openThreeWayMerge initializes the IntelliJ IDEA-style 3-way merge workspace.
func (a *App) openThreeWayMerge(spaceID, spaceName, rel, conflictCopyRel, baseText, localText, remoteText, leftTitle, rightTitle string, diskConflictDoc *document) {
	sess := merge.NewSession(baseText, localText, remoteText)
	sess.ApplyNonConflicting()

	if leftTitle == "" {
		leftTitle = i18n.T("本地版本 (Yours)")
	}
	if rightTitle == "" {
		rightTitle = i18n.T("云端版本 (Theirs)")
	}

	firstConflict := 0
	for i := range sess.Chunks {
		if sess.Chunks[i].Kind == merge.ChunkConflict {
			firstConflict = i
			break
		}
	}

	a.mergeUI = threeWayMergeState{
		open:             true,
		spaceID:          spaceID,
		spaceName:        spaceName,
		rel:              rel,
		conflictCopyPath: conflictCopyRel,
		diskConflictDoc:  diskConflictDoc,
		leftTitle:        leftTitle,
		rightTitle:       rightTitle,
		session:          sess,
		rawResult:        sess.ResultText(),
		editingLine:      -1,
		focusChunk:       firstConflict,
	}
}

func (a *App) openMergeForConflict(cf persistedConflict) {
	sp := a.state.Get(cf.SpaceID)
	spName := cf.SpaceName
	localText := cf.LocalText
	remoteText := cf.RemoteText
	if sp != nil {
		spName = sp.Name
		if cur, ok := a.currentDocumentText(sp, cf.Path); ok {
			localText = cur
		}
		if cf.ConflictCopyPath != "" {
			if rem, ok := a.currentDocumentText(sp, cf.ConflictCopyPath); ok {
				remoteText = rem
			}
		}
	}
	a.openThreeWayMerge(
		cf.SpaceID,
		spName,
		cf.Path,
		cf.ConflictCopyPath,
		cf.BaseText,
		localText,
		remoteText,
		i18n.T("本地版本 (Yours)"),
		i18n.T("云端版本 (Theirs)"),
		nil,
	)
	if cf.RemoteETag != "" || cf.RemoteSize > 0 || cf.RemoteModUnix > 0 {
		a.mergeUI.remoteSize = cf.RemoteSize
		a.mergeUI.remoteETag = cf.RemoteETag
		a.mergeUI.remoteModUnix = cf.RemoteModUnix
		a.mergeUI.hasRemoteMeta = true
	}
}

func (a *App) openMergeForDiskConflict(d *document) {
	if d == nil || d.space == nil || !d.conflict {
		return
	}
	a.openThreeWayMerge(
		d.space.ID,
		d.space.Name,
		d.rel,
		"",
		d.diskText,
		d.text(),
		d.diskNow,
		i18n.T("编辑器修改 (Yours)"),
		i18n.T("磁盘版本 (Theirs)"),
		d,
	)
}

func (a *App) commitThreeWayMerge() {
	ms := &a.mergeUI
	if !ms.open || ms.session == nil {
		return
	}
	if ms.editingLine >= 0 {
		a.commitInlineMergeLine()
	}
	mergedText := ms.session.ResultText()
	if ms.editMode == 1 {
		mergedText = ms.rawResult
	}

	// Case 1: External disk conflict on a live document.
	if ms.diskConflictDoc != nil {
		d := ms.diskConflictDoc
		a.activateDocument(d)
		if !d.isWhiteboard() && d.ed != nil {
			doc := editor.ImportMarkdown(mergedText)
			d.ed.Engine.ReplaceDocument(doc)
		}
		if err := d.write(true); err != nil {
			a.notify("保存合并结果失败。%v", err)
			return
		}
		ms.open = false
		a.refreshTree()
		if a.gitChanges.open {
			a.refreshGitChanges()
		}
		a.notify("已完成「%s」的三栏合并并保存。", d.title())
		return
	}

	// Case 2: Space / Cloud Sync conflict.
	sp := a.state.Get(ms.spaceID)
	if sp == nil || sp.Root == "" {
		ms.open = false
		return
	}

	var openTarget *document
	for _, d := range a.residentDocuments() {
		if d.space != nil && d.space.ID == sp.ID && d.rel == ms.rel {
			openTarget = d
			break
		}
	}

	if openTarget != nil && openTarget.loadErr == nil && !openTarget.isWhiteboard() && openTarget.ed != nil {
		a.withDocument(openTarget, func() {
			doc := editor.ImportMarkdown(mergedText)
			openTarget.ed.Engine.ReplaceDocument(doc)
			_ = openTarget.write(true)
		})
	} else {
		if err := vault.Write(sp.Root, ms.rel, mergedText); err != nil {
			a.notify("保存合并结果失败。%v", err)
			return
		}
	}

	// Clean up the companion "(冲突副本 ...)" file via Trashbin + system Trash.
	if ms.conflictCopyPath != "" && ms.conflictCopyPath != ms.rel {
		wasViewingCopy := a.doc != nil && a.doc.space != nil && a.doc.space.ID == sp.ID && a.doc.rel == ms.conflictCopyPath
		for _, d := range a.residentDocuments() {
			if d.space != nil && d.space.ID == sp.ID && d.rel == ms.conflictCopyPath {
				a.replaceReferences(d, nil)
			}
		}
		a.state.Forget(sp.ID, ms.conflictCopyPath)
		if cfFull, err := vault.Resolve(sp.Root, ms.conflictCopyPath); err == nil {
			if _, statErr := os.Stat(cfFull); statErr == nil {
				kept, addErr := a.bin.Add(sp.ID, sp.Name, sp.Root, ms.conflictCopyPath, time.Now())
				if terr := a.trashItem(cfFull); terr != nil && addErr == nil {
					_ = a.bin.Forget(kept)
				}
			}
		}
		if wasViewingCopy {
			a.openIn(sp, ms.rel)
		}
	}

	hasRemoteMeta := ms.hasRemoteMeta
	rSize, rETag, rModUnix := ms.remoteSize, ms.remoteETag, ms.remoteModUnix
	mergedRel := ms.rel

	a.removePersistedConflict(sp.ID, mergedRel)
	stateDir := a.cloudSyncStateDir()
	if hasRemoteMeta {
		markBaselineResolvedAgainstRemote(stateDir, sp.ID, mergedRel, rSize, rETag, rModUnix)
	}
	writeCloudBaseSnapshot(stateDir, sp.ID, mergedRel, mergedText)

	delete(a.pageTrees, sp.ID)
	a.saveState()
	a.refreshTree()
	a.refreshSearch()
	if a.gitChanges.open {
		a.refreshGitChanges()
	}

	// If more synced files are waiting in the conflict queue, advance straight to the next one!
	if remaining := a.loadPersistedConflicts(); len(remaining) > 0 {
		a.openMergeForConflict(remaining[0])
		a.notify("已保存「%s」，继续合并下一处冲突（剩余 %d 篇）。", vault.Title(mergedRel), len(remaining))
		return
	}

	ms.open = false
	a.notify("已完成「%s」的三栏合并并保存。", vault.Title(mergedRel))

	// Push resolved result back to cloud automatically when Cloud Sync is active.
	if hasRemoteMeta {
		if p := a.findInstalledPlugin(cloudSyncPluginID); p != nil && p.Enabled {
			a.runPluginCommand(p, "sync-now", map[string]any{"trigger": "post-merge"}, true)
		}
	}
}

// spaceModal opens a Space-themed frosted glass modal dialog instead of a generic
// pure-white / pure-black surface, keeping buttons, inputs, and scrollbars tinted
// to the active Space's palette.
func (a *App) spaceModal(c *ui.Context, key string, open *bool, fn func(mt *ui.Theme)) ui.Element {
	look := a.look
	mt := spaceModalTheme(c.Theme(), look)
	const radius float32 = 16
	panel := ui.Modal(c.Key(key), open, func() {
		ui.Themed(c, mt).Background(ui.Transparent).Children(func() {
			fn(mt)
		})
	})
	panel.Radius(radius).
		Background(ui.Transparent).
		Material(uiglass.Blur{Radius: 22}).
		BorderWidth(1).
		BorderLinearGradient(pluginCardRimGradient(mt)).
		Draw(func(pt *ui.Painter, r ui.Rect) {
			paintSpaceModal(pt, r, radius, mt, look)
		})
	return panel
}

// syncConflictBarFor renders a top banner on documents that have an unresolved
// Cloud Sync conflict (or are themselves a "(冲突副本 ...)" document).
func (a *App) syncConflictBarFor(c *ui.Context, d *document) {
	if d == nil || d.conflict || d.missing {
		return
	}
	cf, ok := a.findConflictForDoc(d)
	if !ok {
		return
	}
	t := c.Theme()
	dl := gitDiffColors(t, a.look)
	isCopy := cf.ConflictCopyPath != "" && d.rel == cf.ConflictCopyPath
	msg := fmt.Sprintf(i18n.T("「%s」存在同步冲突（本地与云端均已修改）。"), vault.Title(cf.Path))
	if isCopy {
		msg = fmt.Sprintf(i18n.T("这是「%s」的云端冲突副本，可通过三栏合并将改动归并回原文档。"), vault.Title(cf.Path))
	}

	ui.Row(c.Key("sync-conflict-bar")).Padding(9, 16).Gap(10).AlignItems(ui.Center).
		Background(dl.conflictBadgeBg).Role(ui.RoleStatus).Children(func() {
		ui.Icon(c, iconGitMerge).FontSize(15).TextColor(dl.conflictInk)
		ui.Text(c, msg).FontSize(13).Grow(1).MinWidth(0)
		if ui.Button(c.Key("view-diff"), i18n.T("查看差异")).Clicked() {
			a.openGitChanges()
		}
		if ui.PrimaryButton(c.Key("open-3way"), i18n.T("三栏合并…")).Clicked() {
			a.openMergeForConflict(cf)
		}
	})
}

// gitToolbarButton renders a quiet Git Changes & Merge button in the top toolbar
// with a conflict indicator dot when unresolved sync conflicts exist.
func (a *App) gitToolbarButton(c *ui.Context) {
	p := a.findInstalledPlugin(cloudSyncPluginID)
	conflicts := a.loadPersistedConflicts()
	hasDocConflict := a.doc != nil && a.doc.conflict
	if (p == nil || !p.Enabled) && len(conflicts) == 0 && !hasDocConflict {
		return
	}
	t := c.Theme()
	tip := i18n.T("变更与冲突 (Git 视图)")
	if len(conflicts) > 0 {
		tip = fmt.Sprintf(i18n.T("变更与冲突 · %d 处待合并冲突"), len(conflicts))
	}
	btn := iconButton(c, "git-changes", iconGitCompare, i18n.T("变更与冲突"), tip, false)
	if a.gitChanges.open || a.mergeUI.open {
		btn.Background(glass(t))
	}
	if len(conflicts) > 0 || hasDocConflict {
		danger := t.Danger
		btn.DrawOver(func(pt *ui.Painter, r ui.Rect) {
			pt.FillPath((&ui.Path{}).Circle(r.X+r.W-7, r.Y+7, 3.2), danger)
		})
	}
	if btn.Clicked() {
		a.openGitChanges()
	}
}

// gitChangesView renders the Git-style Changes & Diff Viewer modal inspired by
// fredrikalindh/ui (CollapsibleCard + DiffViewer + SkipBlockRow + ScrollEdge).
func (a *App) gitChangesView(c *ui.Context) {
	gc := &a.gitChanges
	if !gc.open {
		if len(gc.items) > 0 {
			gc.items = nil
		}
		return
	}
	winW, winH := c.Size()

	conflictCount, changeCount, totalAdd, totalDel := 0, 0, 0, 0
	for _, it := range gc.items {
		if it.Status == merge.StatusConflict {
			conflictCount++
		} else {
			changeCount++
		}
		totalAdd += it.Diff.Added
		totalDel += it.Diff.Removed
	}

	a.spaceModal(c, "git-changes-modal", &gc.open, func(t *ui.Theme) {
		dl := gitDiffColors(t, a.look)
		ui.Column(c.Key("git-changes-body")).
			Size(min(920, winW-t.Space(16)), min(640, winH-t.Space(16))).Gap(12).Children(func() {
			// Header
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(32, 32).Shrink(0).Radius(8).Center().Background(t.Accent.Alpha(0.14)).Children(func() {
					ui.Icon(c, iconGitCompare).FontSize(16).TextColor(t.Accent)
				})
				ui.Text(c, i18n.T("变更与冲突")).FontSize(17).FontWeight(600)
				if conflictCount > 0 {
					ui.Row(c).Padding(2, 8).Radius(6).Gap(4).AlignItems(ui.Center).Background(dl.conflictBadgeBg).Children(func() {
						ui.Text(c, fmt.Sprintf(i18n.T("%d 处冲突"), conflictCount)).FontSize(11.5).FontWeight(600).TextColor(dl.conflictInk)
					})
				}
				if changeCount > 0 {
					ui.Row(c).Padding(2, 8).Radius(6).Gap(4).AlignItems(ui.Center).Background(dl.modBadgeBg).Children(func() {
						ui.Text(c, fmt.Sprintf(i18n.T("%d 个文件变更"), changeCount)).FontSize(11.5).FontWeight(500).TextColor(dl.modBadgeInk)
					})
				}
				if totalAdd > 0 || totalDel > 0 {
					ui.Text(c, fmt.Sprintf("+%d", totalAdd)).Font("monospace").FontSize(12).FontWeight(600).TextColor(dl.addInk)
					ui.Text(c, fmt.Sprintf("−%d", totalDel)).Font("monospace").FontSize(12).FontWeight(600).TextColor(dl.delInk)
				}
				ui.Spacer(c)
				if iconButton(c, "refresh-git", iconRefresh, i18n.T("刷新变更"), i18n.T("刷新变更列表"), false).Size(28, 28).Clicked() {
					a.refreshGitChanges()
				}
				if p := a.findInstalledPlugin(cloudSyncPluginID); p != nil && p.Enabled {
					st := a.pluginRunState(cloudSyncPluginID)
					if ui.PrimaryButton(c.Key("git-sync-now"), i18n.T("立即同步")).Disabled(st.busy).Clicked() {
						a.runPluginCommand(p, "sync-now", nil, false)
						a.refreshGitChanges()
					}
				}
				if iconButton(c, "close-git", iconX, i18n.T("关闭"), i18n.T("关闭"), false).Size(28, 28).Clicked() {
					gc.open = false
				}
			})

			ui.Box(c).Height(1).Background(dl.cardBorder)

			if len(gc.items) == 0 {
				ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
					ui.Icon(c, iconCheck).FontSize(28).TextColor(dl.addInk)
					ui.Text(c, i18n.T("工作区干净，暂无未同步的变更或冲突")).FontSize(14).FontWeight(500)
					ui.Text(c, i18n.T("修改文档或同步遇到双端冲突时，这里会以 Git 差异卡片展示每处新增与删除。")).
						FontSize(12).TextColor(t.TextMuted)
				})
				return
			}

			scrollWrap := ui.Box(c.Key("git-cards-wrap")).Grow(1).MinHeight(0)
			scrollWrap.Children(func() {
				ui.Scroll(c.Key("git-cards-scroll")).Fill().TrackScroll(&gc.scroll).Padding(2, 4, 14, 0).Gap(12).Children(func() {
					for idx := range gc.items {
						a.gitCollapsibleDiffCard(c, idx, dl)
					}
				})
				if gc.scroll.Y > 2 {
					ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(18).PassThrough().
						Material(uiglass.ScrollEdge{Background: t.Background})
				}
			})
		})
	})
}

func (a *App) gitCollapsibleDiffCard(c *ui.Context, idx int, dl gitDiffLook) {
	t := c.Theme()
	gc := &a.gitChanges
	item := &gc.items[idx]

	badgeText, badgeBg, badgeInk := i18n.T("修改"), dl.modBadgeBg, dl.modBadgeInk
	switch item.Status {
	case merge.StatusConflict:
		badgeText, badgeBg, badgeInk = i18n.T("冲突"), dl.conflictBadgeBg, dl.conflictInk
	case merge.StatusAdded:
		badgeText, badgeBg, badgeInk = i18n.T("新增"), dl.addRow, dl.addInk
	case merge.StatusDeleted:
		badgeText, badgeBg, badgeInk = i18n.T("删除"), dl.delRow, dl.delInk
	}

	borderCol := dl.cardBorder
	if item.Status == merge.StatusConflict {
		borderCol = dl.conflictInk.Alpha(0.35)
	}

	ui.Column(c.Key("diff-card-"+item.Key)).Radius(12).Background(dl.cardBg).Border(1, borderCol).Clip().Children(func() {
		// CollapsibleCardHeader
		hdr := ui.Row(c.Key("hdr")).Height(44).PaddingX(12).Gap(8).AlignItems(ui.Center)
		hoveredHdr := hdr.Hovered()
		if hoveredHdr {
			hdr.Background(dl.headerHover)
		}
		hdr.Children(func() {
			chev := iconChevronDown
			if item.Collapsed {
				chev = iconChevronRight
			}
			toggleBtn := ui.Box(c.Key("collapse")).Size(26, 26).Shrink(0).Radius(6).Center().
				Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).
				Label(i18n.T("折叠或展开 ") + item.Path)
			if toggleBtn.Hovered() {
				toggleBtn.Background(hoverFill(t))
			}
			toggleBtn.Children(func() {
				ui.Icon(c, chev).FontSize(14).TextColor(t.TextMuted)
			})
			if toggleBtn.Clicked() {
				item.Collapsed = !item.Collapsed
			}

			ui.Box(c).Padding(2, 6).Radius(5).Background(badgeBg).Children(func() {
				ui.Text(c, badgeText).FontSize(11).FontWeight(600).TextColor(badgeInk).NoWrap()
			})

			ui.Text(c, item.SpaceName+" /").FontSize(12.5).TextColor(t.TextMuted).NoWrap()
			ui.Text(c, item.Path).Font("monospace").FontSize(12.5).FontWeight(500).SingleLine().Shrink(1).MinWidth(0)

			// CopyButton (matches fredrikalindh/ui CopyButton)
			copied := gc.copiedKey == item.Key && c.Now().Before(gc.copiedUntil)
			if copied {
				c.After(gc.copiedUntil.Sub(c.Now()))
			}
			copyBtn := ui.Box(c.Key("copy-path")).Size(24, 24).Shrink(0).Radius(5).Center().
				Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).
				Label(i18n.T("复制路径 ") + item.Path).Tooltip(i18n.T("复制文件路径"))
			if copyBtn.Hovered() {
				copyBtn.Background(hoverFill(t))
			}
			if !hoveredHdr && !copied && !copyBtn.Focused() {
				copyBtn.Opacity(0)
			}
			copyBtn.Children(func() {
				if copied {
					ui.Icon(c, iconCheck).FontSize(13).TextColor(dl.addInk)
				} else {
					ui.Icon(c, iconCopy).FontSize(12.5).TextColor(t.TextMuted)
				}
			})
			if copyBtn.Clicked() {
				c.WriteClipboard(item.Path)
				gc.copiedKey = item.Key
				gc.copiedUntil = c.Now().Add(1500 * time.Millisecond)
			}

			ui.Spacer(c)

			if item.Diff.Added > 0 {
				ui.Text(c, fmt.Sprintf("+%d", item.Diff.Added)).Font("monospace").FontSize(12).FontWeight(600).TextColor(dl.addInk).NoWrap()
			}
			if item.Diff.Removed > 0 {
				ui.Text(c, fmt.Sprintf("−%d", item.Diff.Removed)).Font("monospace").FontSize(12).FontWeight(600).TextColor(dl.delInk).NoWrap()
			}

			if item.Status == merge.StatusConflict {
				if ui.PrimaryButton(c.Key("merge-btn"), i18n.T("三栏合并…")).Clicked() {
					if strings.HasPrefix(item.Key, "disk-conflict:") {
						for _, d := range a.residentDocuments() {
							if d.space != nil && d.space.ID == item.SpaceID && d.rel == item.Path && d.conflict {
								a.openMergeForDiskConflict(d)
								break
							}
						}
					} else {
						a.openThreeWayMerge(
							item.SpaceID,
							item.SpaceName,
							item.Path,
							item.ConflictCopyPath,
							item.BaseText,
							item.NewText,
							item.RemoteText,
							i18n.T("本地版本 (Yours)"),
							i18n.T("云端版本 (Theirs)"),
							nil,
						)
						if item.RemoteETag != "" || item.RemoteSize > 0 || item.RemoteModUnix > 0 {
							a.mergeUI.remoteSize = item.RemoteSize
							a.mergeUI.remoteETag = item.RemoteETag
							a.mergeUI.remoteModUnix = item.RemoteModUnix
							a.mergeUI.hasRemoteMeta = true
						}
						gc.open = false
					}
				}
			} else if item.Status != merge.StatusDeleted {
				if ui.Button(c.Key("open-doc"), i18n.T("打开")).Clicked() {
					if sp := a.state.Get(item.SpaceID); sp != nil {
						gc.open = false
						a.openIn(sp, item.Path)
					}
				}
			}
		})

		if item.Collapsed {
			return
		}
		ui.Box(c).Height(1).Background(dl.cardBorder)

		// Diff Table Body (fredrikalindh/ui style)
		ui.Column(c.Key("diff-table")).Padding(4, 0).Children(func() {
			for bIdx, blk := range item.Diff.Blocks {
				if blk.IsSkip && !item.ExpandedSkips[bIdx] {
					blockIndex := bIdx
					skipRow := ui.Row(c.Key("skip-" + strconv.Itoa(bIdx))).Height(30).AlignItems(ui.Center).
						Background(dl.skipBg).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).
						Label(fmt.Sprintf(i18n.T("展开隐藏的 %d 行"), blk.HiddenCount))
					if skipRow.Hovered() {
						skipRow.Background(dl.skipHover)
					}
					skipRow.Children(func() {
						ui.Box(c).Width(3).Shrink(0)
						ui.Box(c).Width(44).Shrink(0).Center().Children(func() {
							ui.Icon(c, iconChevronsUpDown).FontSize(13).TextColor(dl.skipText)
						})
						ui.Text(c, fmt.Sprintf(i18n.T("展开隐藏的 %d 行未改动内容"), blk.HiddenCount)).
							Font("monospace").FontSize(11.5).TextColor(dl.skipText)
					})
					if skipRow.Clicked() {
						item.ExpandedSkips[blockIndex] = true
					}
					continue
				}

				for lIdx, line := range blk.Lines {
					a.gitDiffLineRow(c, strconv.Itoa(bIdx)+"-"+strconv.Itoa(lIdx), line, item.Status, dl)
				}
			}
		})
	})
}

func (a *App) gitDiffLineRow(c *ui.Context, key string, line merge.DiffLine, fileStatus merge.FileStatus, dl gitDiffLook) {
	rowBg := ui.Transparent
	barCol := ui.Transparent
	sign := " "
	signCol := dl.lineNum
	numStr := strconv.Itoa(line.NewNum)

	switch line.Kind {
	case merge.LineInsert:
		barCol = dl.addBar
		if fileStatus != merge.StatusAdded {
			rowBg = dl.addRow
		}
		sign = "+"
		signCol = dl.addInk
	case merge.LineDelete:
		barCol = dl.delBar
		if fileStatus != merge.StatusDeleted {
			rowBg = dl.delRow
		}
		sign = "−"
		signCol = dl.delInk
		numStr = "–"
	}

	ui.Row(c.Key("dl-" + key)).MinHeight(22).AlignItems(ui.Stretch).Background(rowBg).Children(func() {
		// 3px left accent border bar (fredrikalindh/ui td:first-child border-l-3)
		ui.Box(c).Width(3).Shrink(0).Background(barCol)
		// Tabular line number gutter
		ui.Box(c).Width(44).Shrink(0).Padding(2, 6).AlignItems(ui.End).Justify(ui.Center).Children(func() {
			ui.Text(c, numStr).Font("monospace").FontSize(11.5).TextColor(dl.lineNum).NoWrap()
		})
		// +/- indicator
		ui.Box(c).Width(16).Shrink(0).Padding(2, 0).Center().Children(func() {
			ui.Text(c, sign).Font("monospace").FontSize(12).TextColor(signCol).NoWrap()
		})
		// Code / Markdown line with intra-line grapheme highlights (no strikethrough)
		ui.Row(c).Grow(1).MinWidth(0).Padding(2, 14, 2, 2).AlignItems(ui.Center).Children(func() {
			if len(line.Spans) > 0 {
				wordBg := dl.addWord
				if line.Kind == merge.LineDelete {
					wordBg = dl.delWord
				}
				spans := make([]ui.Span, 0, len(line.Spans))
				for _, sp := range line.Spans {
					us := ui.Span{
						Text:  sp.Text,
						Font:  "monospace",
						Color: dl.codeText,
					}
					if sp.Changed {
						us.Background = wordBg
						us.Radius = 3
					}
					spans = append(spans, us)
				}
				ui.RichText(c.Key("rt"), spans...).FontSize(12.5).Grow(1).MinWidth(0)
			} else {
				txt := line.Text
				if txt == "" {
					txt = " "
				}
				ui.Text(c, txt).Font("monospace").FontSize(12.5).TextColor(dl.codeText).Grow(1).MinWidth(0)
			}
		})
	})
}

func (a *App) commitInlineMergeLine() {
	ms := &a.mergeUI
	if ms.session == nil || ms.editingLine < 0 {
		ms.editingLine = -1
		return
	}
	target := ms.editingLine
	ms.editingLine = -1
	for i := range ms.session.Chunks {
		ch := &ms.session.Chunks[i]
		if target >= ch.CenterStart && target < ch.CenterEnd {
			relIdx := target - ch.CenterStart
			ch.ResultLines[relIdx] = ms.lineBuf
			if ch.Kind == merge.ChunkConflict {
				ch.LeftAccepted = true
			}
			ms.session.ResultLines[target] = ms.lineBuf
			break
		}
	}
}

func (a *App) stepMergeChunk(delta int) {
	ms := &a.mergeUI
	if ms.session == nil || len(ms.session.Chunks) == 0 {
		return
	}
	var actionable []int
	for i := range ms.session.Chunks {
		if ms.session.Chunks[i].Kind != merge.ChunkUnchanged {
			actionable = append(actionable, i)
		}
	}
	if len(actionable) == 0 {
		return
	}
	pos := 0
	for i, cIdx := range actionable {
		if cIdx == ms.focusChunk {
			pos = i
			break
		}
	}
	pos = (pos + delta + len(actionable)) % len(actionable)
	ms.focusChunk = actionable[pos]
	ch := ms.session.Chunks[ms.focusChunk]
	ms.scroll.Y = max(0, float32(ch.CenterStart)*mergeRowH-66)
}

// mergeView renders the JetBrains IntelliJ IDEA-style 3-Way Merge modal:
// Left (Yours) | Bezier Gutter (» ×) | Center (Editable Result) | Bezier Gutter (« ×) | Right (Theirs).
func (a *App) mergeView(c *ui.Context) {
	ms := &a.mergeUI
	if !ms.open || ms.session == nil {
		if !ms.open && (ms.session != nil || ms.diskConflictDoc != nil || ms.rawResult != "") {
			old := ms.diskConflictDoc
			*ms = threeWayMergeState{}
			a.retireClosedDocuments(old)
		}
		return
	}
	winW, winH := c.Size()
	sess := ms.session
	unresolved := sess.UnresolvedConflicts()
	pending := sess.PendingChanges()
	queuedTotal := 0
	if ms.diskConflictDoc == nil {
		queuedTotal = len(a.loadPersistedConflicts())
	}

	a.spaceModal(c, "three-way-merge", &ms.open, func(t *ui.Theme) {
		ml := ideaMergeColors(t, a.look)
		ui.Column(c.Key("merge-body")).
			Size(min(1180, winW-t.Space(12)), min(700, winH-t.Space(12))).Gap(10).Children(func() {
			// Top IDEA Merge Toolbar
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(30, 30).Shrink(0).Radius(8).Center().Background(t.Accent.Alpha(0.14)).Children(func() {
					ui.Icon(c, iconGitMerge).FontSize(16).TextColor(t.Accent)
				})
				ui.Text(c, i18n.T("三栏合并 · ")+vault.Title(ms.rel)).FontSize(16).FontWeight(600).SingleLine().Shrink(1).MinWidth(0)

				if queuedTotal > 1 {
					ui.Box(c).Padding(3, 8).Radius(6).Background(t.Accent.Alpha(0.14)).Children(func() {
						ui.Text(c, fmt.Sprintf(i18n.T("待合并 %d 篇"), queuedTotal)).
							FontSize(11.5).FontWeight(600).TextColor(t.Accent).NoWrap()
					})
				}

				if unresolved > 0 {
					ui.Box(c).Padding(3, 9).Radius(6).Background(ml.conflictRow).Children(func() {
						ui.Text(c, fmt.Sprintf(i18n.T("剩余 %d 处冲突"), unresolved)).
							FontSize(11.5).FontWeight(600).TextColor(ml.conflictInk).NoWrap()
					})
				} else {
					ui.Box(c).Padding(3, 9).Radius(6).Background(ml.insRow).Children(func() {
						ui.Text(c, i18n.T("✓ 所有冲突已解决")).
							FontSize(11.5).FontWeight(600).TextColor(ml.insInk).NoWrap()
					})
				}

				if iconButton(c, "prev-chunk", iconArrowUp, i18n.T("上一处差异"), i18n.T("跳转到上一处冲突或变更"), false).Size(28, 28).Clicked() {
					a.stepMergeChunk(-1)
				}
				if iconButton(c, "next-chunk", iconArrowDown, i18n.T("下一处差异"), i18n.T("跳转到下一处冲突或变更"), false).Size(28, 28).Clicked() {
					a.stepMergeChunk(1)
				}

				ui.Spacer(c)

				if ms.editMode == 0 {
					if ui.Button(c.Key("auto-non-conflict"), i18n.T("自动合并非冲突项")).Disabled(pending == 0).Clicked() {
						sess.ApplyNonConflicting()
						ms.rawResult = sess.ResultText()
					}
					if ui.Button(c.Key("accept-all-left"), i18n.T("全部采用本地")).Clicked() {
						sess.AcceptAllLeft()
						ms.rawResult = sess.ResultText()
					}
					if ui.Button(c.Key("accept-all-right"), i18n.T("全部采用云端")).Clicked() {
						sess.AcceptAllRight()
						ms.rawResult = sess.ResultText()
					}
				}

				prevMode := ms.editMode
				ui.Segmented(c.Key("merge-mode"), &ms.editMode, i18n.T("三栏对比"), i18n.T("自由编辑结果")).Label(i18n.T("合并视图模式"))
				if ms.editMode != prevMode && ms.editMode == 1 {
					if ms.editingLine >= 0 {
						a.commitInlineMergeLine()
					}
					ms.rawResult = sess.ResultText()
				}
			})

			if ms.editMode == 1 {
				ui.Column(c.Key("merge-raw-editor")).Grow(1).MinHeight(0).Radius(10).
					Border(1, ml.border).Background(ml.centerBg).Padding(10).Children(func() {
					ui.TextArea(c.Key("merge-raw-textarea"), &ms.rawResult).
						Font("monospace").FontSize(13).Grow(1).Label(i18n.T("合并结果全文编辑"))
				})
			} else {
				a.mergeThreePanesView(c, ml)
			}

			// Footer Bar
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T("点击中间连接带上的 » / « 采纳对应修改（冲突处连续点两侧可同时保留两段）；单击中间任意行可直接微调文字。")).
					FontSize(12).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
				if ui.Button(c.Key("cancel-merge"), i18n.T("取消")).Clicked() {
					ms.open = false
				}
				if ui.PrimaryButton(c.Key("apply-merge"), i18n.T("完成合并并保存")).Clicked() {
					a.commitThreeWayMerge()
				}
			})
		})
	})
}

func chunkPalette(ch *merge.Chunk, ml mergeLook) (rowBg, wordBg, ribbonFill, stroke ui.Color) {
	switch ch.VisualTag() {
	case merge.TagConflict:
		return ml.conflictRow, ml.conflictWord, ml.conflictRibbon, ml.conflictStroke
	case merge.TagInsert:
		return ml.insRow, ml.insWord, ml.insRibbon, ml.insStroke
	case merge.TagDelete:
		return ml.delRow, ml.modWord, ml.delRibbon, ml.delStroke
	case merge.TagModify:
		if ch.Kind == merge.ChunkConflict && ch.Resolved() {
			return ml.insRow, ml.insWord, ml.insRibbon, ml.insStroke
		}
		return ml.modRow, ml.modWord, ml.modRibbon, ml.modStroke
	default:
		return ui.Transparent, ui.Transparent, ui.Transparent, ui.Transparent
	}
}

func paintPaneChunks(pt *ui.Painter, r ui.Rect, chunks []merge.Chunk, which int, ml mergeLook) {
	for i := range chunks {
		ch := &chunks[i]
		if ch.Kind == merge.ChunkUnchanged {
			continue
		}
		rowBg, _, _, stroke := chunkPalette(ch, ml)
		var sLine, eLine int
		switch which {
		case 0:
			sLine, eLine = ch.LeftStart, ch.LeftEnd
		case 1:
			sLine, eLine = ch.CenterStart, ch.CenterEnd
		case 2:
			sLine, eLine = ch.RightStart, ch.RightEnd
		}
		y0 := r.Y + float32(sLine)*mergeRowH
		y1 := r.Y + float32(eLine)*mergeRowH
		if eLine > sLine {
			pt.Fill(ui.Rect{X: r.X, Y: y0, W: r.W, H: y1 - y0}, rowBg, 0)
			pt.Fill(ui.Rect{X: r.X, Y: y0, W: r.W, H: 1}, stroke.Alpha(0.35), 0)
			pt.Fill(ui.Rect{X: r.X, Y: y1 - 1, W: r.W, H: 1}, stroke.Alpha(0.35), 0)
		} else {
			// Empty insertion anchor line (2px bar as in IDEA).
			pt.Fill(ui.Rect{X: r.X, Y: max(r.Y, y0-1), W: r.W, H: 2}, stroke, 0)
		}
	}
}

func paintBezierRibbon(pt *ui.Painter, x0, ly0, ly1, x1, ry0, ry1 float32, fill, stroke ui.Color) {
	xm := (x0 + x1) * 0.5
	if ly1-ly0 < 2 {
		ly1 = ly0 + 2
	}
	if ry1-ry0 < 2 {
		ry1 = ry0 + 2
	}
	ribbon := &ui.Path{}
	ribbon.MoveTo(x0, ly0)
	ribbon.CubeTo(xm, ly0, xm, ry0, x1, ry0)
	ribbon.LineTo(x1, ry1)
	ribbon.CubeTo(xm, ry1, xm, ly1, x0, ly1)
	ribbon.Close()
	pt.FillPath(ribbon, fill)

	topEdge := &ui.Path{}
	topEdge.MoveTo(x0, ly0)
	topEdge.CubeTo(xm, ly0, xm, ry0, x1, ry0)
	pt.StrokePath(topEdge, 1, stroke)

	botEdge := &ui.Path{}
	botEdge.MoveTo(x0, ly1)
	botEdge.CubeTo(xm, ly1, xm, ry1, x1, ry1)
	pt.StrokePath(botEdge, 1, stroke)
}

func (a *App) mergeThreePanesView(c *ui.Context, ml mergeLook) {
	ms := &a.mergeUI
	sess := ms.session
	t := c.Theme()
	_, winH := c.Size()

	maxLines := max(len(sess.LeftLines), max(len(sess.ResultLines), len(sess.RightLines)))
	modalH := min(700, winH-t.Space(12))
	viewportMinH := max(float32(380), modalH-118)
	contentH := max(float32(maxLines)*mergeRowH+28, viewportMinH)

	ui.Column(c.Key("merge-3pane-frame")).Grow(1).MinHeight(0).Radius(10).Border(1, ml.border).Clip().Children(func() {
		// Column Titles Header
		ui.Row(c.Key("merge-col-headers")).Height(30).Shrink(0).Background(ml.headerBg).
			AlignItems(ui.Center).Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).PaddingX(12).Children(func() {
				ui.Text(c, ms.leftTitle).FontSize(12).FontWeight(600).TextColor(ml.muted).SingleLine()
			})
			ui.Box(c).Width(50).Shrink(0).Height(30).Background(ml.gutterBg).
				BorderStartWidth(1).BorderEndWidth(1).BorderColor(ml.border)
			ui.Row(c).Grow(1).MinWidth(0).PaddingX(12).Children(func() {
				ui.Text(c, i18n.T("合并结果 (Result · 单击行可微调)")).FontSize(12).FontWeight(600).TextColor(t.Accent).SingleLine()
			})
			ui.Box(c).Width(50).Shrink(0).Height(30).Background(ml.gutterBg).
				BorderStartWidth(1).BorderEndWidth(1).BorderColor(ml.border)
			ui.Row(c).Grow(1).MinWidth(0).PaddingX(12).Children(func() {
				ui.Text(c, ms.rightTitle).FontSize(12).FontWeight(600).TextColor(ml.muted).SingleLine()
			})
		})
		ui.Box(c).Height(1).Background(ml.border)

		// Synchronized 3-pane + 2 Bezier-gutter scroll body
		ui.Scroll(c.Key("merge-panes-scroll")).Grow(1).MinHeight(0).TrackScroll(&ms.scroll).Children(func() {
			ui.Row(c.Key("merge-panes-row")).Height(contentH).AlignItems(ui.Stretch).Children(func() {
				// 1. LEFT PANE (Yours)
				leftCol := ui.Column(c.Key("pane-left")).Grow(1).MinWidth(0).Background(ml.paneBg).Clip()
				leftCol.Draw(func(pt *ui.Painter, r ui.Rect) {
					paintPaneChunks(pt, r, sess.Chunks, 0, ml)
				})
				leftCol.Children(func() {
					a.renderReadOnlyMergeLines(c, "L", sess.Chunks, true, ml)
				})

				// 2. LEFT BEZIER GUTTER (Left -> Center)
				leftGut := ui.Box(c.Key("gutter-left")).Width(50).Shrink(0).Background(ml.gutterBg).
					BorderStartWidth(1).BorderEndWidth(1).BorderColor(ml.border)
				leftGut.Draw(func(pt *ui.Painter, r ui.Rect) {
					for i := range sess.Chunks {
						ch := &sess.Chunks[i]
						if ch.Kind == merge.ChunkUnchanged || ch.Kind == merge.ChunkRightOnly {
							continue
						}
						_, _, ribFill, stroke := chunkPalette(ch, ml)
						ly0 := r.Y + float32(ch.LeftStart)*mergeRowH
						ly1 := r.Y + float32(ch.LeftEnd)*mergeRowH
						cy0 := r.Y + float32(ch.CenterStart)*mergeRowH
						cy1 := r.Y + float32(ch.CenterEnd)*mergeRowH
						paintBezierRibbon(pt, r.X, ly0, ly1, r.X+r.W, cy0, cy1, ribFill, stroke)
					}
				})
				leftGut.Children(func() {
					for i := range sess.Chunks {
						ch := sess.Chunks[i]
						if !ch.LeftActionable() {
							continue
						}
						chunkIdx := i
						yTop := float32(ch.LeftStart) * mergeRowH
						ui.Row(c.Key("lg-act-" + strconv.Itoa(chunkIdx))).Absolute().Left(3).Top(yTop + 1).Gap(2).Children(func() {
							if mergeGutterIconBtn(c, "ignore-l-"+strconv.Itoa(chunkIdx), iconX, i18n.T("忽略左侧修改"), ml).Clicked() {
								sess.IgnoreLeft(chunkIdx)
							}
							tip := i18n.T("采纳左侧修改")
							if ch.Kind == merge.ChunkConflict && ch.RightAccepted {
								tip = i18n.T("同时保留左侧修改")
							}
							if mergeGutterIconBtn(c, "accept-l-"+strconv.Itoa(chunkIdx), iconChevronsRight, tip, ml).Clicked() {
								sess.AcceptLeft(chunkIdx)
							}
						})
					}
				})

				// 3. CENTER PANE (Editable Result)
				centerCol := ui.Column(c.Key("pane-center")).Grow(1).MinWidth(0).Background(ml.centerBg).Clip()
				centerCol.Draw(func(pt *ui.Painter, r ui.Rect) {
					paintPaneChunks(pt, r, sess.Chunks, 1, ml)
				})
				centerCol.Children(func() {
					a.renderCenterResultLines(c, sess.ResultLines, ml)
				})

				// 4. RIGHT BEZIER GUTTER (Center <- Right)
				rightGut := ui.Box(c.Key("gutter-right")).Width(50).Shrink(0).Background(ml.gutterBg).
					BorderStartWidth(1).BorderEndWidth(1).BorderColor(ml.border)
				rightGut.Draw(func(pt *ui.Painter, r ui.Rect) {
					for i := range sess.Chunks {
						ch := &sess.Chunks[i]
						if ch.Kind == merge.ChunkUnchanged || ch.Kind == merge.ChunkLeftOnly {
							continue
						}
						_, _, ribFill, stroke := chunkPalette(ch, ml)
						cy0 := r.Y + float32(ch.CenterStart)*mergeRowH
						cy1 := r.Y + float32(ch.CenterEnd)*mergeRowH
						ry0 := r.Y + float32(ch.RightStart)*mergeRowH
						ry1 := r.Y + float32(ch.RightEnd)*mergeRowH
						paintBezierRibbon(pt, r.X, cy0, cy1, r.X+r.W, ry0, ry1, ribFill, stroke)
					}
				})
				rightGut.Children(func() {
					for i := range sess.Chunks {
						ch := sess.Chunks[i]
						if !ch.RightActionable() {
							continue
						}
						chunkIdx := i
						yTop := float32(ch.RightStart) * mergeRowH
						ui.Row(c.Key("rg-act-" + strconv.Itoa(chunkIdx))).Absolute().Left(3).Top(yTop + 1).Gap(2).Children(func() {
							tip := i18n.T("采纳右侧修改")
							if ch.Kind == merge.ChunkConflict && ch.LeftAccepted {
								tip = i18n.T("追加右侧修改（保留双方）")
							}
							if mergeGutterIconBtn(c, "accept-r-"+strconv.Itoa(chunkIdx), iconChevronsLeft, tip, ml).Clicked() {
								sess.AcceptRight(chunkIdx)
							}
							if mergeGutterIconBtn(c, "ignore-r-"+strconv.Itoa(chunkIdx), iconX, i18n.T("忽略右侧修改"), ml).Clicked() {
								sess.IgnoreRight(chunkIdx)
							}
						})
					}
				})

				// 5. RIGHT PANE (Theirs)
				rightCol := ui.Column(c.Key("pane-right")).Grow(1).MinWidth(0).Background(ml.paneBg).Clip()
				rightCol.Draw(func(pt *ui.Painter, r ui.Rect) {
					paintPaneChunks(pt, r, sess.Chunks, 2, ml)
				})
				rightCol.Children(func() {
					a.renderReadOnlyMergeLines(c, "R", sess.Chunks, false, ml)
				})
			})
		})
	})
}

func mergeGutterIconBtn(c *ui.Context, key string, icon *ui.SVG, label string, ml mergeLook) ui.Element {
	b := ui.Box(c.Key(key)).Size(21, 20).Radius(5).Center().
		Background(ml.actionBg).Border(0.5, ml.border).
		Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(label).Tooltip(label)
	if b.Hovered() {
		b.Background(ml.actionHover)
	}
	b.Children(func() {
		ui.Icon(c, icon).FontSize(12).TextColor(ml.actionInk)
	})
	return b
}

func (a *App) renderReadOnlyMergeLines(c *ui.Context, prefix string, chunks []merge.Chunk, isLeft bool, ml mergeLook) {
	lineNum := 0
	for cIdx := range chunks {
		ch := &chunks[cIdx]
		lines := ch.RightLines
		spansSlice := ch.RightSpans
		if isLeft {
			lines = ch.LeftLines
			spansSlice = ch.LeftSpans
		}
		_, wordBg, _, _ := chunkPalette(ch, ml)

		for k, text := range lines {
			lineNum++
			ln := lineNum
			var spans []ui.Span
			if k < len(spansSlice) && len(spansSlice[k]) > 0 {
				spans = make([]ui.Span, 0, len(spansSlice[k]))
				for _, sp := range spansSlice[k] {
					us := ui.Span{
						Text:  sp.Text,
						Font:  "monospace",
						Color: ml.text,
					}
					if sp.Changed && wordBg.A > 0 {
						us.Background = wordBg
						us.Radius = 3
					}
					spans = append(spans, us)
				}
			}
			ui.Row(c.Key(prefix + "-" + strconv.Itoa(ln))).Height(mergeRowH).Shrink(0).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Width(34).Shrink(0).PaddingEnd(6).AlignItems(ui.End).Children(func() {
					ui.Text(c, strconv.Itoa(ln)).Font("monospace").FontSize(11).TextColor(ml.lineNum).NoWrap()
				})
				if len(spans) > 0 {
					ui.RichText(c.Key("rt"), spans...).FontSize(12).SingleLine().Grow(1).MinWidth(0).PaddingEnd(8)
				} else {
					disp := text
					if disp == "" {
						disp = " "
					}
					ui.Text(c, disp).Font("monospace").FontSize(12).TextColor(ml.text).SingleLine().Grow(1).MinWidth(0).PaddingEnd(8)
				}
			})
		}
	}
}

func (a *App) renderCenterResultLines(c *ui.Context, lines []string, ml mergeLook) {
	ms := &a.mergeUI
	t := c.Theme()

	for idx, text := range lines {
		lineIndex := idx
		ln := idx + 1
		if ms.editingLine == lineIndex {
			ui.Row(c.Key("C-edit-" + strconv.Itoa(ln))).Height(mergeRowH).Shrink(0).AlignItems(ui.Center).
				Background(t.Accent.Alpha(0.08)).Children(func() {
				ui.Box(c).Width(34).Shrink(0).PaddingEnd(6).AlignItems(ui.End).Children(func() {
					ui.Text(c, strconv.Itoa(ln)).Font("monospace").FontSize(11).TextColor(t.Accent).NoWrap()
				})
				in := ui.TextInput(c.Key("C-in-"+strconv.Itoa(ln)), &ms.lineBuf).
					Font("monospace").FontSize(12).Height(20).Padding(0, 4).Grow(1).MinWidth(0).
					AutoFocus().Label(fmt.Sprintf(i18n.T("编辑第 %d 行"), ln))
				if in.Submitted() {
					a.commitInlineMergeLine()
				}
			})
			continue
		}

		row := ui.Row(c.Key("C-" + strconv.Itoa(ln))).Height(mergeRowH).Shrink(0).AlignItems(ui.Center).
			Cursor(ui.CursorText)
		if row.Hovered() {
			row.Background(hoverFill(t))
		}
		row.Children(func() {
			ui.Box(c).Width(34).Shrink(0).PaddingEnd(6).AlignItems(ui.End).Children(func() {
				ui.Text(c, strconv.Itoa(ln)).Font("monospace").FontSize(11).TextColor(ml.lineNum).NoWrap()
			})
			disp := text
			if disp == "" {
				disp = " "
			}
			ui.Text(c, disp).Font("monospace").FontSize(12).TextColor(ml.text).SingleLine().Grow(1).MinWidth(0).PaddingEnd(8)
		})
		if row.Clicked() {
			if ms.editingLine >= 0 {
				a.commitInlineMergeLine()
			}
			ms.editingLine = lineIndex
			ms.lineBuf = strings.Clone(text)
		}
	}
}
