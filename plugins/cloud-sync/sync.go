package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/merge"
)

const (
	historyStateKey      = "history.json"
	conflictsStateKey    = "conflicts.json"
	maxBaseSnapshotBytes = 2 << 20
)

type syncConflictItem struct {
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

type syncBaseline struct {
	Version int                      `json:"version"`
	Files   map[string]baselineEntry `json:"files"`
}

type baselineEntry struct {
	LocalSize     int64  `json:"localSize"`
	LocalModNano  int64  `json:"localModNano"`
	LocalSHA256   string `json:"localSha256"`
	RemoteSize    int64  `json:"remoteSize"`
	RemoteETag    string `json:"remoteEtag,omitempty"`
	RemoteModUnix int64  `json:"remoteModUnix,omitempty"`
}

type syncStats struct {
	Uploaded      int `json:"uploaded"`
	Downloaded    int `json:"downloaded"`
	Merged        int `json:"merged"`
	DeletedLocal  int `json:"deletedLocal"`
	DeletedRemote int `json:"deletedRemote"`
	Conflicts     int `json:"conflicts"`
	Skipped       int `json:"skipped"`
	Unchanged     int `json:"unchanged"`
}

type syncRecord struct {
	Time          string   `json:"time"`
	Provider      string   `json:"provider"`
	Trigger       string   `json:"trigger"`
	Spaces        []string `json:"spaces,omitempty"`
	Uploaded      int      `json:"uploaded"`
	Downloaded    int      `json:"downloaded"`
	Merged        int      `json:"merged"`
	DeletedLocal  int      `json:"deletedLocal"`
	DeletedRemote int      `json:"deletedRemote"`
	Conflicts     int      `json:"conflicts"`
	Skipped       int      `json:"skipped"`
	DurationMS    int64    `json:"durationMs"`
	Error         string   `json:"error,omitempty"`
}

func backendFromConfig(cfg map[string]any) (RemoteBackend, error) {
	provider := strCfg(cfg, "provider")
	if provider == "" {
		provider = "s3"
	}
	allowInsecure := boolCfg(cfg, "allowInsecureHTTP")
	switch provider {
	case "s3":
		secret, err := sdkGetSecret("s3SecretKey")
		if err != nil {
			return nil, fmt.Errorf("读取 S3 Secret Key 失败: %w", err)
		}
		return newS3Client(
			strCfg(cfg, "s3Endpoint"),
			strCfg(cfg, "s3Region"),
			strCfg(cfg, "s3Bucket"),
			strCfg(cfg, "s3AccessKey"),
			secret,
			strCfg(cfg, "s3Prefix"),
			boolCfg(cfg, "s3PathStyle"),
			allowInsecure,
		)
	case "webdav":
		pass, err := sdkGetSecret("webdavPassword")
		if err != nil {
			return nil, fmt.Errorf("读取 WebDAV 密码失败: %w", err)
		}
		return newWebDAVClient(
			strCfg(cfg, "webdavURL"),
			strCfg(cfg, "webdavUsername"),
			pass,
			strCfg(cfg, "webdavRoot"),
			allowInsecure,
		)
	default:
		return nil, fmt.Errorf("不支持的同步方式: %s", provider)
	}
}

func runSync(cfg map[string]any, trigger string) (syncRecord, error) {
	start := time.Now()
	rec := syncRecord{
		Time:     start.Format(time.RFC3339),
		Provider: strCfg(cfg, "provider"),
		Trigger:  trigger,
	}
	if rec.Provider == "" {
		rec.Provider = "s3"
	}

	selectedIDs := strSliceCfg(cfg, "spaces")
	if len(selectedIDs) == 0 {
		err := errors.New("尚未勾选要同步的空间")
		rec.Error = err.Error()
		return rec, err
	}

	backend, err := backendFromConfig(cfg)
	if err != nil {
		rec.Error = err.Error()
		return rec, err
	}

	allSpaces, err := sdkListSpaces()
	if err != nil {
		rec.Error = err.Error()
		return rec, err
	}
	selectedSet := make(map[string]bool, len(selectedIDs))
	for _, id := range selectedIDs {
		selectedSet[id] = true
	}
	var targets []spaceInfo
	for _, sp := range allSpaces {
		if selectedSet[sp.ID] {
			targets = append(targets, sp)
		}
	}
	if len(targets) == 0 {
		err := errors.New("所选同步空间不存在或均为外部空间")
		rec.Error = err.Error()
		return rec, err
	}

	prefixes := spacePrefixes(targets)
	var firstErr error
	for _, sp := range targets {
		rec.Spaces = append(rec.Spaces, sp.Name)
		st, err := syncOneSpace(backend, sp, prefixes[sp.ID])
		rec.Uploaded += st.Uploaded
		rec.Downloaded += st.Downloaded
		rec.Merged += st.Merged
		rec.DeletedLocal += st.DeletedLocal
		rec.DeletedRemote += st.DeletedRemote
		rec.Conflicts += st.Conflicts
		rec.Skipped += st.Skipped
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: %w", sp.Name, err)
		}
	}
	rec.DurationMS = time.Since(start).Milliseconds()
	if firstErr != nil {
		rec.Error = firstErr.Error()
	}
	appendSyncHistory(rec)
	return rec, firstErr
}

func spacePrefixes(spaces []spaceInfo) map[string]string {
	counts := map[string]int{}
	clean := make(map[string]string, len(spaces))
	for _, sp := range spaces {
		name := sanitizeSpaceFolder(sp.Name)
		clean[sp.ID] = name
		counts[name]++
	}
	out := make(map[string]string, len(spaces))
	for _, sp := range spaces {
		name := clean[sp.ID]
		if counts[name] > 1 {
			suffix := sp.ID
			if len(suffix) > 6 {
				suffix = suffix[:6]
			}
			name = name + "-" + suffix
		}
		out[sp.ID] = name
	}
	return out
}

func sanitizeSpaceFolder(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(name))
	name = strings.Trim(name, ". ")
	if name == "" {
		return "Space"
	}
	return name
}

func baselineKey(spaceID string) string {
	safe := strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, spaceID)
	return "baseline_" + safe + ".json"
}

func baseTextKey(spaceID, rel string) string {
	return "base_" + sha256HexBytes([]byte(spaceID + "\x00" + rel))[:32] + ".txt"
}

func isTextSyncFile(rel string) bool {
	ext := strings.ToLower(path.Ext(rel))
	switch ext {
	case ".md", ".markdown", ".mdx", ".arcmd", ".txt":
		return true
	}
	return false
}

func saveBaseSnapshot(spaceID, rel string, data []byte) {
	if !isTextSyncFile(rel) || len(data) > maxBaseSnapshotBytes {
		return
	}
	_ = sdkSetState(baseTextKey(spaceID, rel), data)
}

func loadBaseSnapshot(spaceID, rel string) string {
	if !isTextSyncFile(rel) {
		return ""
	}
	b, err := sdkGetState(baseTextKey(spaceID, rel))
	if err != nil || len(b) == 0 {
		return ""
	}
	return string(b)
}

func deleteBaseSnapshot(spaceID, rel string) {
	if !isTextSyncFile(rel) {
		return
	}
	_ = sdkDeleteState(baseTextKey(spaceID, rel))
}

func loadConflictItems() []syncConflictItem {
	raw, err := sdkGetState(conflictsStateKey)
	if err != nil || len(raw) == 0 {
		return nil
	}
	var items []syncConflictItem
	_ = json.Unmarshal(raw, &items)
	return items
}

func recordConflictItem(item syncConflictItem) {
	items := loadConflictItems()
	filtered := make([]syncConflictItem, 0, len(items)+1)
	filtered = append(filtered, item)
	for _, existing := range items {
		if existing.SpaceID == item.SpaceID && existing.Path == item.Path {
			continue
		}
		filtered = append(filtered, existing)
	}
	if len(filtered) > 50 {
		filtered = filtered[:50]
	}
	if b, err := json.Marshal(filtered); err == nil {
		_ = sdkSetState(conflictsStateKey, b)
	}
}

func removeConflictItem(spaceID, rel string) {
	items := loadConflictItems()
	if len(items) == 0 {
		return
	}
	kept := make([]syncConflictItem, 0, len(items))
	for _, existing := range items {
		if existing.SpaceID == spaceID && existing.Path == rel {
			continue
		}
		kept = append(kept, existing)
	}
	if len(kept) == len(items) {
		return
	}
	if len(kept) == 0 {
		_ = sdkDeleteState(conflictsStateKey)
		return
	}
	if b, err := json.Marshal(kept); err == nil {
		_ = sdkSetState(conflictsStateKey, b)
	}
}

func loadBaseline(spaceID string) syncBaseline {
	raw, err := sdkGetState(baselineKey(spaceID))
	if err != nil || len(raw) == 0 {
		return syncBaseline{Version: 1, Files: map[string]baselineEntry{}}
	}
	var b syncBaseline
	if json.Unmarshal(raw, &b) != nil || b.Files == nil {
		return syncBaseline{Version: 1, Files: map[string]baselineEntry{}}
	}
	return b
}

func saveBaseline(spaceID string, b syncBaseline) error {
	b.Version = 1
	if b.Files == nil {
		b.Files = map[string]baselineEntry{}
	}
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return sdkSetState(baselineKey(spaceID), data)
}

func syncOneSpace(backend RemoteBackend, sp spaceInfo, prefix string) (syncStats, error) {
	var stats syncStats
	localList, err := sdkListFiles(sp.ID)
	if err != nil {
		return stats, err
	}
	remoteList, err := backend.List(prefix)
	if err != nil {
		return stats, err
	}
	base := loadBaseline(sp.ID)
	next := syncBaseline{Version: 1, Files: make(map[string]baselineEntry, len(localList)+len(remoteList))}

	localMap := make(map[string]spaceFileEntry, len(localList))
	for _, f := range localList {
		localMap[f.Path] = f
	}
	remoteMap := make(map[string]RemoteObject, len(remoteList))
	for _, r := range remoteList {
		if !isSafeSyncPath(r.Path) {
			continue
		}
		remoteMap[r.Path] = r
	}

	pathSet := make(map[string]bool, len(localMap)+len(remoteMap)+len(base.Files))
	for p := range localMap {
		pathSet[p] = true
	}
	for p := range remoteMap {
		pathSet[p] = true
	}
	for p := range base.Files {
		pathSet[p] = true
	}
	paths := make([]string, 0, len(pathSet))
	for p := range pathSet {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var firstErr error
	keepErr := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	for _, rel := range paths {
		loc, hasLoc := localMap[rel]
		rem, hasRem := remoteMap[rel]
		prev, hasBase := base.Files[rel]

		keepOldBase := func() {
			if hasBase {
				next.Files[rel] = prev
			}
		}

		switch {
		case !hasBase && hasLoc && !hasRem:
			// Case A: new local file -> upload.
			if loc.Dirty {
				stats.Skipped++
				continue
			}
			data, meta, err := sdkReadFile(sp.ID, rel)
			if err != nil {
				keepErr(err)
				continue
			}
			rMeta, err := backend.Put(prefix, rel, data)
			if err != nil {
				keepErr(err)
				continue
			}
			next.Files[rel] = makeBaseline(meta, rMeta)
			saveBaseSnapshot(sp.ID, rel, data)
			stats.Uploaded++

		case !hasBase && !hasLoc && hasRem:
			// Case B: new remote file -> download.
			data, rMeta, err := backend.Get(prefix, rel)
			if err != nil {
				keepErr(err)
				continue
			}
			modNano := time.Unix(rMeta.ModTimeUnix, 0).UnixNano()
			if rMeta.ModTimeUnix <= 0 {
				modNano = 0
			}
			lMeta, err := sdkWriteFile(sp.ID, rel, data, "", modNano)
			if err != nil {
				var he *hostError
				if errors.As(err, &he) && (he.Code == "conflict" || he.Code == "dirty") {
					curData, curMeta, _ := sdkReadFile(sp.ID, rel)
					cf, cerr := mergeOrStageConflict(backend, sp, prefix, rel, rMeta, data, curData, curMeta.SHA256, true)
					if cerr != nil {
						keepErr(cerr)
					} else if cf.AutoMerged {
						next.Files[rel] = cf.MergedEntry
						stats.Merged++
					} else {
						if cf.BinaryCopyPath != "" {
							next.Files[cf.BinaryCopyPath] = cf.BinaryCopyEntry
						}
						stats.Conflicts++
					}
					continue
				}
				keepErr(err)
				continue
			}
			removeConflictItem(sp.ID, rel)
			next.Files[rel] = makeBaseline(lMeta, rMeta)
			saveBaseSnapshot(sp.ID, rel, data)
			stats.Downloaded++

		case !hasBase && hasLoc && hasRem:
			// Case C: both exist without baseline.
			rData, rMeta, err := backend.Get(prefix, rel)
			if err != nil {
				keepErr(err)
				continue
			}
			var lData []byte
			var lMeta spaceFileMeta
			if !loc.Dirty {
				lData, lMeta, err = sdkReadFile(sp.ID, rel)
				if err != nil {
					keepErr(err)
					continue
				}
				if lMeta.SHA256 == sha256HexBytes(rData) {
					removeConflictItem(sp.ID, rel)
					next.Files[rel] = makeBaseline(lMeta, rMeta)
					saveBaseSnapshot(sp.ID, rel, lData)
					stats.Unchanged++
					continue
				}
			} else {
				lData, lMeta, _ = sdkReadFile(sp.ID, rel)
			}
			cf, err := mergeOrStageConflict(backend, sp, prefix, rel, rMeta, rData, lData, lMeta.SHA256, loc.Dirty)
			if err != nil {
				keepErr(err)
				continue
			}
			if cf.AutoMerged {
				next.Files[rel] = cf.MergedEntry
				stats.Merged++
				continue
			}
			if cf.BinaryCopyPath != "" {
				next.Files[cf.BinaryCopyPath] = cf.BinaryCopyEntry
			}
			if cf.BinaryMainUploaded {
				next.Files[rel] = makeBaseline(lMeta, cf.BinaryMainRemote)
			}
			stats.Conflicts++

		case hasBase && hasLoc && hasRem:
			// Case D: tracked on both sides.
			remChanged := remoteChanged(rem, prev)
			locChanged := loc.Dirty || loc.Size != prev.LocalSize || loc.ModTimeUnixNano != prev.LocalModNano
			var lData []byte
			var lMeta spaceFileMeta
			if locChanged && !loc.Dirty {
				var rerr error
				lData, lMeta, rerr = sdkReadFile(sp.ID, rel)
				if rerr != nil {
					keepOldBase()
					keepErr(rerr)
					continue
				}
				if lMeta.SHA256 == prev.LocalSHA256 {
					locChanged = false
					// Refresh local stat cache in baseline if mtime moved without content change.
					prev.LocalSize = lMeta.Size
					prev.LocalModNano = lMeta.ModTimeUnixNano
				}
			}

			switch {
			case !locChanged && !remChanged:
				next.Files[rel] = prev
				stats.Unchanged++

			case locChanged && !remChanged:
				if loc.Dirty {
					keepOldBase()
					stats.Skipped++
					continue
				}
				rMeta, err := backend.Put(prefix, rel, lData)
				if err != nil {
					keepOldBase()
					keepErr(err)
					continue
				}
				removeConflictItem(sp.ID, rel)
				next.Files[rel] = makeBaseline(lMeta, rMeta)
				saveBaseSnapshot(sp.ID, rel, lData)
				stats.Uploaded++

			case !locChanged && remChanged:
				rData, rMeta, err := backend.Get(prefix, rel)
				if err != nil {
					keepOldBase()
					keepErr(err)
					continue
				}
				if sha256HexBytes(rData) == prev.LocalSHA256 {
					removeConflictItem(sp.ID, rel)
					next.Files[rel] = makeBaseline(spaceFileMeta{
						Path:            rel,
						Size:            prev.LocalSize,
						ModTimeUnixNano: prev.LocalModNano,
						SHA256:          prev.LocalSHA256,
					}, rMeta)
					saveBaseSnapshot(sp.ID, rel, rData)
					stats.Unchanged++
					continue
				}
				modNano := time.Unix(rMeta.ModTimeUnix, 0).UnixNano()
				if rMeta.ModTimeUnix <= 0 {
					modNano = 0
				}
				written, err := sdkWriteFile(sp.ID, rel, rData, prev.LocalSHA256, modNano)
				if err != nil {
					var he *hostError
					if errors.As(err, &he) && (he.Code == "conflict" || he.Code == "dirty") {
						curData, curMeta, rerr := sdkReadFile(sp.ID, rel)
						isDirty := rerr != nil || curMeta.Dirty
						cf, cerr := mergeOrStageConflict(backend, sp, prefix, rel, rMeta, rData, curData, curMeta.SHA256, isDirty)
						if cerr != nil {
							keepOldBase()
							keepErr(cerr)
							continue
						}
						if cf.AutoMerged {
							next.Files[rel] = cf.MergedEntry
							stats.Merged++
							continue
						}
						if cf.BinaryCopyPath != "" {
							next.Files[cf.BinaryCopyPath] = cf.BinaryCopyEntry
						}
						if cf.BinaryMainUploaded {
							next.Files[rel] = makeBaseline(curMeta, cf.BinaryMainRemote)
						} else {
							keepOldBase()
						}
						stats.Conflicts++
						continue
					}
					keepOldBase()
					keepErr(err)
					continue
				}
				removeConflictItem(sp.ID, rel)
				next.Files[rel] = makeBaseline(written, rMeta)
				saveBaseSnapshot(sp.ID, rel, rData)
				stats.Downloaded++

			default: // locChanged && remChanged
				rData, rMeta, err := backend.Get(prefix, rel)
				if err != nil {
					keepOldBase()
					keepErr(err)
					continue
				}
				if !loc.Dirty && lMeta.SHA256 == sha256HexBytes(rData) {
					removeConflictItem(sp.ID, rel)
					next.Files[rel] = makeBaseline(lMeta, rMeta)
					saveBaseSnapshot(sp.ID, rel, lData)
					stats.Unchanged++
					continue
				}
				if loc.Dirty && len(lData) == 0 {
					lData, lMeta, _ = sdkReadFile(sp.ID, rel)
				}
				cf, err := mergeOrStageConflict(backend, sp, prefix, rel, rMeta, rData, lData, lMeta.SHA256, loc.Dirty)
				if err != nil {
					keepOldBase()
					keepErr(err)
					continue
				}
				if cf.AutoMerged {
					next.Files[rel] = cf.MergedEntry
					stats.Merged++
					continue
				}
				if cf.BinaryCopyPath != "" {
					next.Files[cf.BinaryCopyPath] = cf.BinaryCopyEntry
				}
				if cf.BinaryMainUploaded {
					next.Files[rel] = makeBaseline(lMeta, cf.BinaryMainRemote)
				} else {
					keepOldBase()
				}
				stats.Conflicts++
			}

		case hasBase && !hasLoc && hasRem:
			// Case E: deleted locally.
			if remoteChanged(rem, prev) {
				// Remote was updated after local deleted; preserve remote edits.
				rData, rMeta, err := backend.Get(prefix, rel)
				if err != nil {
					keepOldBase()
					keepErr(err)
					continue
				}
				modNano := time.Unix(rMeta.ModTimeUnix, 0).UnixNano()
				if rMeta.ModTimeUnix <= 0 {
					modNano = 0
				}
				written, err := sdkWriteFile(sp.ID, rel, rData, "", modNano)
				if err != nil {
					keepOldBase()
					keepErr(err)
					continue
				}
				removeConflictItem(sp.ID, rel)
				next.Files[rel] = makeBaseline(written, rMeta)
				saveBaseSnapshot(sp.ID, rel, rData)
				stats.Downloaded++
				continue
			}
			if err := backend.Delete(prefix, rel); err != nil {
				keepOldBase()
				keepErr(err)
				continue
			}
			removeConflictItem(sp.ID, rel)
			deleteBaseSnapshot(sp.ID, rel)
			stats.DeletedRemote++

		case hasBase && hasLoc && !hasRem:
			// Case F: deleted remotely.
			locChanged := loc.Dirty || loc.Size != prev.LocalSize || loc.ModTimeUnixNano != prev.LocalModNano
			var lData []byte
			var lMeta spaceFileMeta
			if locChanged && !loc.Dirty {
				var rerr error
				lData, lMeta, rerr = sdkReadFile(sp.ID, rel)
				if rerr != nil {
					keepOldBase()
					keepErr(rerr)
					continue
				}
				if lMeta.SHA256 == prev.LocalSHA256 {
					locChanged = false
				}
			}
			if locChanged {
				if loc.Dirty {
					keepOldBase()
					stats.Skipped++
					continue
				}
				// Local was edited after remote deleted; re-upload local edits.
				rMeta, err := backend.Put(prefix, rel, lData)
				if err != nil {
					keepOldBase()
					keepErr(err)
					continue
				}
				removeConflictItem(sp.ID, rel)
				next.Files[rel] = makeBaseline(lMeta, rMeta)
				saveBaseSnapshot(sp.ID, rel, lData)
				stats.Uploaded++
				continue
			}
			if err := sdkDeleteFile(sp.ID, rel, prev.LocalSHA256); err != nil {
				var he *hostError
				if errors.As(err, &he) && he.Code == "not_found" {
					removeConflictItem(sp.ID, rel)
					deleteBaseSnapshot(sp.ID, rel)
					continue
				}
				keepOldBase()
				keepErr(err)
				continue
			}
			removeConflictItem(sp.ID, rel)
			deleteBaseSnapshot(sp.ID, rel)
			stats.DeletedLocal++

		case hasBase && !hasLoc && !hasRem:
			// Case G: deleted on both sides; drop from baseline.
			removeConflictItem(sp.ID, rel)
			deleteBaseSnapshot(sp.ID, rel)
		}
	}

	if err := saveBaseline(sp.ID, next); err != nil {
		keepErr(err)
	}
	return stats, firstErr
}

type conflictResult struct {
	AutoMerged         bool
	MergedEntry        baselineEntry
	BinaryCopyPath     string
	BinaryCopyEntry    baselineEntry
	BinaryMainUploaded bool
	BinaryMainRemote   RemoteObject
}

// mergeOrStageConflict handles diverged files in Git / JetBrains IDEA fashion:
//   - For text documents with a shared base and non-overlapping edits, it performs
//     a clean 3-way merge automatically and syncs the merged result to both sides.
//   - For text documents with overlapping edits (or unsaved editor changes), it
//     stages the Base / Local / Remote versions in conflicts.json WITHOUT creating
//     any "(冲突副本 ...)" clutter files, so ArcMD opens the 3-Way Merge dialog directly.
//   - Only non-text binary files fall back to a conflict copy.
func mergeOrStageConflict(backend RemoteBackend, sp spaceInfo, prefix, rel string, rMeta RemoteObject, remoteData, localData []byte, localSHA string, isDirty bool) (conflictResult, error) {
	if isTextSyncFile(rel) && len(remoteData) <= maxBaseSnapshotBytes && len(localData) <= maxBaseSnapshotBytes {
		baseText := loadBaseSnapshot(sp.ID, rel)
		if !isDirty && baseText != "" && localSHA != "" {
			sess := merge.NewSession(baseText, string(localData), string(remoteData))
			if sess.UnresolvedConflicts() == 0 {
				sess.ApplyNonConflicting()
				mergedBytes := []byte(sess.ResultText())
				written, werr := sdkWriteFile(sp.ID, rel, mergedBytes, localSHA, 0)
				if werr == nil {
					upMeta, uerr := backend.Put(prefix, rel, mergedBytes)
					if uerr == nil {
						removeConflictItem(sp.ID, rel)
						saveBaseSnapshot(sp.ID, rel, mergedBytes)
						return conflictResult{
							AutoMerged:  true,
							MergedEntry: makeBaseline(written, upMeta),
						}, nil
					}
				}
			}
		}

		// Stage for interactive IDEA 3-Way Merge — zero "(冲突副本 ...)" files!
		recordConflictItem(syncConflictItem{
			SpaceID:       sp.ID,
			SpaceName:     sp.Name,
			Path:          rel,
			BaseText:      baseText,
			LocalText:     string(localData),
			RemoteText:    string(remoteData),
			RemoteSize:    rMeta.Size,
			RemoteETag:    rMeta.ETag,
			RemoteModUnix: rMeta.ModTimeUnix,
			CreatedUnix:   time.Now().Unix(),
		})
		return conflictResult{}, nil
	}

	// Non-text binary attachment fallback.
	cfMeta, err := sdkWriteConflict(sp.ID, rel, remoteData)
	if err != nil {
		return conflictResult{}, err
	}
	cfRemote, err := backend.Put(prefix, cfMeta.Path, remoteData)
	if err != nil {
		return conflictResult{}, err
	}
	res := conflictResult{
		BinaryCopyPath:  cfMeta.Path,
		BinaryCopyEntry: makeBaseline(cfMeta, cfRemote),
	}
	if !isDirty && localData != nil {
		mainRemote, err := backend.Put(prefix, rel, localData)
		if err != nil {
			return res, err
		}
		res.BinaryMainUploaded = true
		res.BinaryMainRemote = mainRemote
	}
	return res, nil
}

func makeBaseline(local spaceFileMeta, remote RemoteObject) baselineEntry {
	return baselineEntry{
		LocalSize:     local.Size,
		LocalModNano:  local.ModTimeUnixNano,
		LocalSHA256:   local.SHA256,
		RemoteSize:    remote.Size,
		RemoteETag:    remote.ETag,
		RemoteModUnix: remote.ModTimeUnix,
	}
}

func remoteChanged(cur RemoteObject, prev baselineEntry) bool {
	if prev.RemoteETag != "" && cur.ETag != "" {
		return cur.ETag != prev.RemoteETag || cur.Size != prev.RemoteSize
	}
	return cur.Size != prev.RemoteSize || cur.ModTimeUnix != prev.RemoteModUnix
}

func loadSyncHistory() []syncRecord {
	raw, err := sdkGetState(historyStateKey)
	if err != nil || len(raw) == 0 {
		return nil
	}
	var out []syncRecord
	_ = json.Unmarshal(raw, &out)
	return out
}

func appendSyncHistory(rec syncRecord) {
	hist := append([]syncRecord{rec}, loadSyncHistory()...)
	if len(hist) > 20 {
		hist = hist[:20]
	}
	if b, err := json.Marshal(hist); err == nil {
		_ = sdkSetState(historyStateKey, b)
	}
}

func isSafeSyncPath(rel string) bool {
	if rel == "" || strings.Contains(rel, `\`) || strings.HasPrefix(rel, "/") {
		return false
	}
	if path.Clean(rel) != rel {
		return false
	}
	parts := strings.Split(rel, "/")
	for _, seg := range parts {
		if seg == "" || seg == "." || seg == ".." || seg == "node_modules" {
			return false
		}
	}
	base := parts[len(parts)-1]
	if strings.HasPrefix(base, ".") {
		return false
	}
	if parts[0] == ".arcmd" {
		if len(parts) < 3 || parts[1] != "templates" {
			return false
		}
		for _, seg := range parts[2:] {
			if strings.HasPrefix(seg, ".") {
				return false
			}
		}
		return true
	}
	for _, seg := range parts {
		if strings.HasPrefix(seg, ".") {
			return false
		}
	}
	if slices.Contains(parts[:len(parts)-1], "assets") {
		return true
	}
	low := strings.ToLower(base)
	if strings.HasSuffix(low, ".excalidraw.svg") || strings.HasSuffix(low, ".excalidraw") {
		return true
	}
	switch strings.ToLower(path.Ext(low)) {
	case ".md", ".markdown", ".mdx", ".arcmd", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
		return true
	}
	return false
}

func strCfg(cfg map[string]any, key string) string {
	v, _ := cfg[key].(string)
	return strings.TrimSpace(v)
}

func boolCfg(cfg map[string]any, key string) bool {
	v, _ := cfg[key].(bool)
	return v
}

func strSliceCfg(cfg map[string]any, key string) []string {
	raw, ok := cfg[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
