package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/agent"
	"github.com/Ciao1019/ArcMD/internal/clipper"
	"github.com/Ciao1019/ArcMD/internal/database"
	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/Ciao1019/ArcMD/internal/history"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/templates"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *App) ctlClipPing() (any, error) {
	activeID, activeName := "", ""
	if sp := a.state.ActiveSpace(); sp != nil {
		activeID, activeName = sp.ID, sp.Name
	}
	aiReady := a.qaReady() == nil
	aiModel := ""
	if a.prefs != nil {
		aiModel = strings.TrimSpace(a.prefs.AI.Model)
	}
	return map[string]any{
		"version":         Version,
		"protocol":        clipper.ProtocolVersion,
		"live":            a.isLiveWindow(),
		"aiReady":         aiReady,
		"aiModel":         aiModel,
		"activeSpaceId":   activeID,
		"activeSpaceName": activeName,
		"hostName":        clipper.HostName,
	}, nil
}

type clipSpaceContext struct {
	ID             string                `json:"id"`
	Name           string                `json:"name"`
	Icon           string                `json:"icon,omitempty"`
	Symbol         string                `json:"symbol,omitempty"`
	Color          string                `json:"color,omitempty"`
	AttachmentsDir string                `json:"attachmentsDir,omitempty"`
	Active         bool                  `json:"active"`
	Folders        []string              `json:"folders"`
	Templates      []string              `json:"templates"`
	Docs           []string              `json:"docs"`
	Databases      []clipDatabaseContext `json:"databases"`
}

type clipDatabaseContext struct {
	Path       string                `json:"path"`
	Title      string                `json:"title"`
	Icon       string                `json:"icon,omitempty"`
	RecordsDir string                `json:"recordsDir"`
	Properties []clipDatabasePropDTO `json:"properties"`
}

type clipDatabasePropDTO struct {
	ID       string                  `json:"id"`
	Key      string                  `json:"key"`
	Name     string                  `json:"name"`
	Type     string                  `json:"type"`
	ReadOnly bool                    `json:"readOnly,omitempty"`
	Options  []clipDatabaseOptionDTO `json:"options,omitempty"`
}

type clipDatabaseOptionDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

func (a *App) ctlClipContext() (any, error) {
	active := a.state.ActiveSpace()
	activeID := ""
	if active != nil {
		activeID = active.ID
	}
	spacesOut := make([]clipSpaceContext, 0, len(a.state.Spaces))
	for _, sp := range a.state.Spaces {
		if sp == nil || sp.IsExternal() {
			continue
		}
		th := space.ThemeOf(sp)
		primaryColor := ""
		if len(th.Colors) > 0 {
			primaryColor = th.Colors[0]
		}
		nodes, _ := a.treeOf(sp)
		var folders []string
		var docs []string
		var dbPaths []string
		var walk func(list []*vault.Node)
		walk = func(list []*vault.Node) {
			for _, n := range list {
				if n == nil {
					continue
				}
				if n.Dir {
					folders = append(folders, n.Path)
					walk(n.Children)
					continue
				}
				if vault.IsDatabase(n.Path) {
					dbPaths = append(dbPaths, n.Path)
					continue
				}
				if vault.IsMarkdown(n.Path) && len(docs) < 200 {
					docs = append(docs, n.Path)
				}
			}
		}
		walk(nodes)
		slices.Sort(folders)
		slices.Sort(docs)
		slices.Sort(dbPaths)

		tmpls := a.spaceTemplates(sp)
		tmplNames := make([]string, 0, len(tmpls))
		for _, t := range tmpls {
			tmplNames = append(tmplNames, t.Name)
		}

		dbs := make([]clipDatabaseContext, 0, len(dbPaths))
		for _, dbRel := range dbPaths {
			st, _, err := a.openDBStoreForControl(sp, dbRel)
			if err != nil || st == nil {
				continue
			}
			cur := st.Database()
			if cur == nil {
				continue
			}
			props := make([]clipDatabasePropDTO, 0, len(cur.Properties))
			for _, prop := range cur.Properties {
				readOnly := prop.Type == database.PropFormula ||
					prop.Type == database.PropRollup ||
					prop.Type == database.PropCreatedTime ||
					prop.Type == database.PropEditedTime ||
					prop.Type == database.PropUniqueID
				dto := clipDatabasePropDTO{
					ID:       prop.ID,
					Key:      prop.Key,
					Name:     prop.Name,
					Type:     string(prop.Type),
					ReadOnly: readOnly,
				}
				if len(prop.Options) > 0 {
					dto.Options = make([]clipDatabaseOptionDTO, 0, len(prop.Options))
					for _, opt := range prop.Options {
						dto.Options = append(dto.Options, clipDatabaseOptionDTO{
							ID:    opt.ID,
							Name:  opt.Name,
							Color: opt.Color,
						})
					}
				}
				props = append(props, dto)
			}
			recordsDir := st.ResolveSourceDir()
			if cur.Source.Type == database.SourceInline {
				recordsDir = path.Dir(dbRel)
				if recordsDir == "." {
					recordsDir = ""
				}
			}
			dbs = append(dbs, clipDatabaseContext{
				Path:       dbRel,
				Title:      cur.Title,
				Icon:       cur.Icon,
				RecordsDir: recordsDir,
				Properties: props,
			})
		}

		spacesOut = append(spacesOut, clipSpaceContext{
			ID:             sp.ID,
			Name:           sp.Name,
			Icon:           sp.Icon,
			Symbol:         sp.Symbol,
			Color:          primaryColor,
			AttachmentsDir: sp.AttachmentsDir,
			Active:         sp.ID == activeID,
			Folders:        folders,
			Templates:      tmplNames,
			Docs:           docs,
			Databases:      dbs,
		})
	}
	aiReady := a.qaReady() == nil
	aiModel := ""
	if a.prefs != nil {
		aiModel = strings.TrimSpace(a.prefs.AI.Model)
	}
	return map[string]any{
		"version":       Version,
		"protocol":      clipper.ProtocolVersion,
		"live":          a.isLiveWindow(),
		"activeSpaceId": activeID,
		"dailyDir":      templates.Folder(),
		"aiReady":       aiReady,
		"aiModel":       aiModel,
		"spaces":        spacesOut,
	}, nil
}

type clipURLMatch struct {
	SpaceID     string `json:"spaceId"`
	SpaceName   string `json:"spaceName"`
	Path        string `json:"path"`
	Title       string `json:"title"`
	Kind        string `json:"kind"`
	RecordID    string `json:"recordId,omitempty"`
	RecordTitle string `json:"recordTitle,omitempty"`
}

func (a *App) ctlClipCheckURL(raw []byte) (any, error) {
	var p struct {
		URL   string `json:"url"`
		Space string `json:"space"`
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	norm := clipper.NormalizeURL(p.URL)
	if norm == "" {
		return map[string]any{"normalizedUrl": "", "matches": []clipURLMatch{}}, nil
	}

	var spaces []*space.Space
	if strings.TrimSpace(p.Space) != "" {
		sp, err := a.resolveTargetSpace(p.Space)
		if err != nil {
			return nil, err
		}
		if !sp.IsExternal() {
			spaces = []*space.Space{sp}
		}
	} else {
		for _, sp := range a.state.Spaces {
			if sp != nil && !sp.IsExternal() {
				spaces = append(spaces, sp)
			}
		}
	}

	urlKeys := []string{"source", "url", "clipped_from", "link", "origin"}
	matches := make([]clipURLMatch, 0, 4)
	for _, sp := range spaces {
		if len(matches) >= 20 {
			break
		}
		docs, err := vault.List(sp.Root)
		if err != nil {
			continue
		}
		dbRecordPaths := map[string]bool{}
		for _, d := range docs {
			if len(matches) >= 20 {
				break
			}
			if !vault.IsDatabase(d.Path) {
				continue
			}
			st, _, err := a.openDBStoreForControl(sp, d.Path)
			if err != nil || st == nil {
				continue
			}
			curDB := st.Database()
			if curDB == nil {
				continue
			}
			for _, rec := range st.Records() {
				if rec == nil || len(matches) >= 20 {
					break
				}
				matched := false
				for _, prop := range curDB.Properties {
					if prop.Type == database.PropURL || slices.Contains(urlKeys, strings.ToLower(prop.Key)) {
						if v := rec.ValueFor(prop); clipper.NormalizeURL(v.Text) == norm {
							matched = true
							break
						}
					}
				}
				if matched {
					if rec.ID != "" {
						dbRecordPaths[rec.ID] = true
					}
					matches = append(matches, clipURLMatch{
						SpaceID:     sp.ID,
						SpaceName:   sp.Name,
						Path:        d.Path,
						Title:       curDB.Title,
						Kind:        "database",
						RecordID:    rec.ID,
						RecordTitle: rec.Title,
					})
				}
			}
		}
		for _, d := range docs {
			if len(matches) >= 20 {
				break
			}
			if !vault.IsMarkdown(d.Path) || dbRecordPaths[d.Path] {
				continue
			}
			text, err := readFile(sp, d.Path)
			if err != nil {
				continue
			}
			fm, _, hasFM, _ := database.SplitFrontmatter(text)
			if !hasFM || fm == nil {
				continue
			}
			for _, k := range urlKeys {
				if val, ok := fm.Get(k); ok && val.Kind == database.FMScalar && clipper.NormalizeURL(val.Scalar) == norm {
					matches = append(matches, clipURLMatch{
						SpaceID:   sp.ID,
						SpaceName: sp.Name,
						Path:      d.Path,
						Title:     vault.Title(d.Path),
						Kind:      "doc",
					})
					break
				}
			}
		}
	}
	return map[string]any{
		"normalizedUrl": norm,
		"matches":       matches,
	}, nil
}

func (a *App) ctlClipUploadAsset(raw []byte) (any, error) {
	var p struct {
		Space      string `json:"space"`
		DocDir     string `json:"docDir"`
		Filename   string `json:"filename"`
		MimeType   string `json:"mimeType"`
		DataBase64 string `json:"dataBase64"`
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	sp, err := a.resolveTargetSpace(p.Space)
	if err != nil {
		return nil, err
	}
	if sp.IsExternal() {
		return nil, errors.New("cannot upload clipped assets into External space")
	}
	b64 := strings.TrimSpace(p.DataBase64)
	if idx := strings.Index(b64, ";base64,"); idx >= 0 && strings.HasPrefix(strings.ToLower(b64), "data:") {
		b64 = b64[idx+len(";base64,"):]
	}
	if b64 == "" {
		return nil, errors.New("clipper: dataBase64 is empty")
	}
	if base64.StdEncoding.DecodedLen(len(b64)) > clipper.MaxAssetBytes+1024 {
		return nil, fmt.Errorf("clipper: image exceeds maximum size (%d bytes)", clipper.MaxAssetBytes)
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("clipper: invalid base64 image data: %w", err)
		}
	}
	_, safeName, err := clipper.ValidateImage(p.Filename, p.MimeType, data)
	if err != nil {
		return nil, err
	}

	docDir := strings.Trim(strings.ReplaceAll(strings.TrimSpace(p.DocDir), `\`, "/"), "/")
	if docDir == "." {
		docDir = ""
	}
	writeDir, urlDir := path.Join(docDir, assetsDir), assetsDir
	folderMode := false
	if folder := attachmentFolder(sp); folder != "" {
		writeDir, urlDir, folderMode = folder, folder, true
	}

	base := assetName(safeName)
	ext := vault.FileExt(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; ; i++ {
		cand := base
		if i > 1 {
			cand = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		storedRel := path.Join(writeDir, cand)
		full, rErr := vault.Resolve(sp.Root, storedRel)
		if rErr != nil {
			return nil, rErr
		}
		if fi, stErr := os.Lstat(full); stErr == nil {
			if fi.Mode().IsRegular() && fi.Size() == int64(len(data)) {
				if existing, rErr := os.ReadFile(full); rErr == nil && bytes.Equal(existing, data) {
					relURL := path.Join(urlDir, cand)
					if folderMode {
						relURL = docRelativeURL(docDir, path.Join(urlDir, cand))
					}
					return map[string]any{
						"space":      sp.Name,
						"relUrl":     relURL,
						"storedPath": storedRel,
						"bytes":      len(data),
					}, nil
				}
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return nil, err
		}
		if err := fsutil.CreateFileAtomic(full, data, 0o644); err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			return nil, err
		}
		a.markSelfWrite(full)
		a.invalidateResolvedImages()
		relURL := path.Join(urlDir, cand)
		if folderMode {
			relURL = docRelativeURL(docDir, path.Join(urlDir, cand))
		}
		return map[string]any{
			"space":      sp.Name,
			"relUrl":     relURL,
			"storedPath": storedRel,
			"bytes":      len(data),
		}, nil
	}
}

func (a *App) ctlClipSave(raw []byte) (any, error) {
	var p struct {
		Space          string         `json:"space"`
		Mode           string         `json:"mode"` // create | daily | append | database
		Dir            string         `json:"dir"`
		Path           string         `json:"path"`
		Title          string         `json:"title"`
		Content        string         `json:"content"`
		Frontmatter    map[string]any `json:"frontmatter"`
		Icon           string         `json:"icon"`
		Cover          string         `json:"cover"`
		Template       string         `json:"template"`
		DailyPosition  string         `json:"dailyPosition"`  // bottom | top
		AppendPosition string         `json:"appendPosition"` // bottom | top
		Fields         map[string]any `json:"fields"`
		Pin            bool           `json:"pin"`
		Favorite       bool           `json:"favorite"`
		Open           bool           `json:"open"`
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	sp, err := a.resolveTargetSpace(p.Space)
	if err != nil {
		return nil, err
	}
	if sp.IsExternal() {
		return nil, errors.New("cannot save web clips inside External space")
	}

	mode := strings.ToLower(strings.TrimSpace(p.Mode))
	if mode == "" {
		mode = "create"
	}

	switch mode {
	case "create":
		title := clipper.SafeDocTitle(p.Title)
		now := time.Now()
		var body string
		if tmplName := strings.TrimSpace(p.Template); tmplName != "" {
			list := a.spaceTemplates(sp)
			tmpl, ok := templates.Find(list, tmplName)
			if !ok {
				return nil, fmt.Errorf("template %q not found in .arcmd/templates", tmplName)
			}
			tmplDoc, tErr := readTemplate(sp, tmpl)
			if tErr != nil {
				return nil, tErr
			}
			body = editor.ExportMarkdown(templates.Instantiate(tmplDoc, now, title))
			if strings.TrimSpace(p.Content) != "" {
				body = strings.TrimRight(body, "\n") + "\n\n" + strings.TrimSpace(p.Content) + "\n"
			}
		} else if strings.TrimSpace(p.Content) != "" {
			body = ensureTrailingNewlineStr(p.Content)
		} else {
			body = "# " + title + "\n\n"
		}
		body = applyMetadataPatch(body, strPtrIfNotEmpty(p.Icon), strPtrIfNotEmpty(p.Cover), nil, nil, p.Frontmatter)

		dir := strings.Trim(strings.ReplaceAll(strings.TrimSpace(p.Dir), `\`, "/"), "/")
		if dir == "." {
			dir = ""
		}
		finalRel, cErr := vault.CreateFile(sp.Root, dir, title, ".md", body)
		if cErr != nil {
			return nil, cErr
		}
		_ = history.Record(sp.Root, finalRel, body, now, false)
		a.noteExternalWrite(sp, finalRel)
		a.finishDocPostActions(sp, finalRel, p.Pin, p.Favorite, p.Open)
		if a.isLiveWindow() && !p.Open {
			a.notify("已剪藏「%s」到 %s。", vault.Title(finalRel), sp.Name)
		}
		return map[string]any{
			"space":   sp.Name,
			"spaceId": sp.ID,
			"path":    finalRel,
			"title":   vault.Title(finalRel),
			"mode":    "create",
		}, nil

	case "daily":
		now := time.Now()
		rel := templates.PathFor(now)
		existed := false
		if full, rErr := vault.Resolve(sp.Root, rel); rErr == nil {
			if _, stErr := os.Stat(full); stErr == nil {
				existed = true
			}
		}
		clipChunk := strings.TrimSpace(p.Content)
		if !existed {
			var tmplDoc *model.Document
			for _, t := range a.spaceTemplates(sp) {
				if strings.EqualFold(t.Name, "日记") || strings.EqualFold(t.Name, "Daily") {
					tmplDoc, _ = readTemplate(sp, t)
					break
				}
			}
			initial := editor.ExportMarkdown(templates.Daily(tmplDoc, now))
			if clipChunk != "" {
				initial = insertClipChunk(initial, clipChunk, p.DailyPosition)
			}
			initial = applyMetadataPatch(initial, strPtrIfNotEmpty(p.Icon), strPtrIfNotEmpty(p.Cover), nil, nil, p.Frontmatter)
			full, rErr := vault.Resolve(sp.Root, rel)
			if rErr != nil {
				return nil, rErr
			}
			if err := fsutil.CreateFileAtomic(full, []byte(initial), 0o644); err != nil {
				if !errors.Is(err, fs.ErrExist) {
					return nil, err
				}
				existed = true
			} else {
				_ = history.Record(sp.Root, rel, initial, now, false)
				a.noteExternalWrite(sp, rel)
			}
		}
		if existed {
			cur, _, rErr := a.readMarkdownForControl(sp, rel)
			if rErr != nil {
				return nil, rErr
			}
			next := cur
			if clipChunk != "" {
				next = insertClipChunk(cur, clipChunk, p.DailyPosition)
			}
			next = applyMetadataPatch(next, strPtrIfNotEmpty(p.Icon), strPtrIfNotEmpty(p.Cover), nil, nil, p.Frontmatter)
			if err := a.commitMarkdownForControl(sp, rel, next); err != nil {
				return nil, err
			}
		}
		a.finishDocPostActions(sp, rel, p.Pin, p.Favorite, p.Open)
		if a.isLiveWindow() && !p.Open {
			a.notify("已剪藏到今日笔记（%s）。", sp.Name)
		}
		return map[string]any{
			"space":   sp.Name,
			"spaceId": sp.ID,
			"path":    rel,
			"title":   vault.Title(rel),
			"existed": existed,
			"mode":    "daily",
		}, nil

	case "append":
		rel := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(p.Path), `\`, "/"), "/")
		if rel == "" {
			return nil, errors.New("clip.save append mode requires target path")
		}
		if !vault.IsMarkdown(rel) {
			return nil, fmt.Errorf("cannot append to non-Markdown file %q", rel)
		}
		cur, _, rErr := a.readMarkdownForControl(sp, rel)
		if rErr != nil {
			return nil, rErr
		}
		next := cur
		if clipChunk := strings.TrimSpace(p.Content); clipChunk != "" {
			next = insertClipChunk(cur, clipChunk, p.AppendPosition)
		}
		next = applyMetadataPatch(next, strPtrIfNotEmpty(p.Icon), strPtrIfNotEmpty(p.Cover), nil, nil, p.Frontmatter)
		if err := a.commitMarkdownForControl(sp, rel, next); err != nil {
			return nil, err
		}
		a.finishDocPostActions(sp, rel, p.Pin, p.Favorite, p.Open)
		if a.isLiveWindow() && !p.Open {
			a.notify("已追加剪藏到「%s」。", vault.Title(rel))
		}
		return map[string]any{
			"space":   sp.Name,
			"spaceId": sp.ID,
			"path":    rel,
			"title":   vault.Title(rel),
			"existed": true,
			"mode":    "append",
		}, nil

	case "database":
		dbRel, err := normalizeDBRel(sp, p.Path, "")
		if err != nil {
			return nil, err
		}
		st, liveDoc, err := a.openDBStoreForControl(sp, dbRel)
		if err != nil {
			return nil, err
		}
		vals, vErr := coerceRecordFields(st, p.Fields)
		if vErr != nil {
			return nil, vErr
		}
		title := strings.TrimSpace(p.Title)
		if title == "" {
			title = "Untitled Clip"
		}
		rec, cErr := a.createDBRecordForControl(sp, liveDoc, st, title, vals, p.Content)
		if cErr != nil {
			return nil, cErr
		}
		if rec.Path != "" && (strings.TrimSpace(p.Icon) != "" || strings.TrimSpace(p.Cover) != "") {
			if rawMD, rErr := vault.Read(sp.Root, rec.Path); rErr == nil {
				updated := applyMetadataPatch(rawMD, strPtrIfNotEmpty(p.Icon), strPtrIfNotEmpty(p.Cover), nil, nil, nil)
				if updated != rawMD {
					if err := a.commitMarkdownForControl(sp, rec.Path, updated); err != nil {
						return nil, err
					}
					_, _ = st.Refresh(a.liveDocsForSpace(sp))
				}
			}
		}
		if err := a.saveDBStoreForControl(sp, dbRel, st, liveDoc); err != nil {
			return nil, err
		}
		_ = st.Query(st.Database().ActiveView, database.QueryOptions{})
		if latest := st.Record(rec.ID); latest != nil {
			rec = latest
		}
		if p.Open && a.isLiveWindow() {
			if a.state.ActiveSpace() != sp {
				a.switchSpaceOnly(sp.ID)
			}
			a.openIn(sp, dbRel)
			a.showMainWindow()
		} else if a.isLiveWindow() {
			a.notify("已写入数据库「%s」。", st.Database().Title)
		}
		return map[string]any{
			"space":    sp.Name,
			"spaceId":  sp.ID,
			"path":     dbRel,
			"title":    st.Database().Title,
			"recordId": rec.ID,
			"record":   formatDBRecordJSON(st.Database(), rec),
			"mode":     "database",
		}, nil

	default:
		return nil, fmt.Errorf("unsupported clip.save mode %q (use create, daily, append, or database)", p.Mode)
	}
}

// insertClipChunk inserts chunk at the bottom (default) or top (just below
// frontmatter and the leading H1 heading, if any) of existing Markdown content.
func insertClipChunk(existing, chunk, position string) string {
	pos := strings.ToLower(strings.TrimSpace(position))
	if pos != "top" {
		if strings.TrimSpace(existing) == "" {
			return ensureTrailingNewlineStr(chunk)
		}
		return strings.TrimRight(existing, "\n") + "\n\n" + strings.TrimSpace(chunk) + "\n"
	}
	fm, body, hasFM, lineEnding := database.SplitFrontmatter(existing)
	if lineEnding == "" {
		lineEnding = "\n"
	}
	trimmedBody := strings.TrimLeft(body, "\r\n")
	var nextBody string
	if strings.HasPrefix(trimmedBody, "# ") {
		if nl := strings.IndexByte(trimmedBody, '\n'); nl >= 0 {
			heading := strings.TrimRight(trimmedBody[:nl], "\r")
			rest := strings.TrimLeft(trimmedBody[nl+1:], "\r\n")
			if rest != "" {
				nextBody = heading + "\n\n" + strings.TrimSpace(chunk) + "\n\n" + rest
			} else {
				nextBody = heading + "\n\n" + strings.TrimSpace(chunk) + "\n"
			}
		} else {
			nextBody = trimmedBody + "\n\n" + strings.TrimSpace(chunk) + "\n"
		}
	} else if trimmedBody != "" {
		nextBody = strings.TrimSpace(chunk) + "\n\n" + trimmedBody
	} else {
		nextBody = strings.TrimSpace(chunk) + "\n"
	}
	if hasFM && fm != nil && !fm.IsEmpty() {
		return fm.Format() + "\n" + ensureTrailingNewlineStr(nextBody)
	}
	return ensureTrailingNewlineStr(nextBody)
}

// handleClipAIAsync runs LLM summarization/tagging off the main UI thread so
// the ArcMD desktop window remains responsive while the model completes.
func (a *App) handleClipAIAsync(ctx context.Context, raw []byte) (any, error) {
	var p struct {
		Title        string   `json:"title"`
		URL          string   `json:"url"`
		Content      string   `json:"content"`
		Mode         string   `json:"mode"` // both | summary | tags | custom
		Prompt       string   `json:"prompt"`
		Language     string   `json:"language"`
		ExistingTags []string `json:"existingTags"`
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Content) == "" && strings.TrimSpace(p.Title) == "" {
		return nil, errors.New("clip.ai requires content or title")
	}

	type aiSetup struct {
		provider agent.Provider
		model    string
	}
	setupAny, err := a.invokeOnMain(ctx, func() (any, error) {
		if err := a.qaReady(); err != nil {
			return nil, err
		}
		settings := a.qaSettings()
		var prov agent.Provider = settings.Provider()
		if a.qa.provider != nil {
			prov = a.qa.provider(settings)
		}
		return aiSetup{provider: prov, model: settings.Model}, nil
	})
	if err != nil {
		return nil, err
	}
	setup := setupAny.(aiSetup)

	mode := strings.ToLower(strings.TrimSpace(p.Mode))
	if mode == "" {
		mode = "both"
	}
	// Cap excerpt to keep prompt fast and within context limits.
	excerpt := strings.TrimSpace(p.Content)
	if runes := []rune(excerpt); len(runes) > 12000 {
		excerpt = string(runes[:12000]) + "\n...(truncated)"
	}

	sysPrompt := `You are ArcMD Web Clipper's assistant. Analyze the clipped web page and respond ONLY with a valid JSON object (no markdown fences, no commentary) matching:
{"summary":"2-3 concise sentences summarizing the core ideas in the article's primary language","tags":["3 to 5 concise lowercase or Chinese topic tags without # prefix"],"custom":"optional answer when a custom prompt is provided"}`
	var userMsg strings.Builder
	fmt.Fprintf(&userMsg, "Mode: %s\nTitle: %s\nURL: %s\n", mode, strings.TrimSpace(p.Title), strings.TrimSpace(p.URL))
	if lang := strings.TrimSpace(p.Language); lang != "" {
		fmt.Fprintf(&userMsg, "Preferred output language: %s\n", lang)
	}
	if len(p.ExistingTags) > 0 {
		fmt.Fprintf(&userMsg, "Prefer reusing relevant existing tags when appropriate: %s\n", strings.Join(p.ExistingTags, ", "))
	}
	if custom := strings.TrimSpace(p.Prompt); custom != "" {
		fmt.Fprintf(&userMsg, "Custom instruction (put output in \"custom\"): %s\n", custom)
	}
	fmt.Fprintf(&userMsg, "\nArticle Content:\n%s", excerpt)

	aiCtx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	resp, err := setup.provider.Complete(aiCtx, agent.Request{
		System: sysPrompt,
		Messages: []agent.Message{
			{Role: agent.User, Text: userMsg.String()},
		},
	}, nil)
	if err != nil {
		return nil, err
	}
	replyText := strings.TrimSpace(resp.Message.Text)
	parsed := parseClipAIReply(replyText)
	return map[string]any{
		"summary": parsed.Summary,
		"tags":    parsed.Tags,
		"custom":  parsed.Custom,
		"model":   setup.model,
	}, nil
}

type clipAIReply struct {
	Summary string   `json:"summary"`
	Tags    []string `json:"tags"`
	Custom  string   `json:"custom"`
}

func parseClipAIReply(raw string) clipAIReply {
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		if nl := strings.IndexByte(cleaned, '\n'); nl >= 0 {
			cleaned = cleaned[nl+1:]
		}
		if end := strings.LastIndex(cleaned, "```"); end >= 0 {
			cleaned = cleaned[:end]
		}
		cleaned = strings.TrimSpace(cleaned)
	}
	if start := strings.IndexByte(cleaned, '{'); start >= 0 {
		if end := strings.LastIndexByte(cleaned, '}'); end > start {
			cleaned = cleaned[start : end+1]
		}
	}
	var out clipAIReply
	if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
		return clipAIReply{Summary: strings.TrimSpace(raw), Tags: []string{}}
	}
	cleanTags := make([]string, 0, len(out.Tags))
	seen := map[string]bool{}
	for _, t := range out.Tags {
		t = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "#"))
		if t == "" || seen[strings.ToLower(t)] {
			continue
		}
		seen[strings.ToLower(t)] = true
		cleanTags = append(cleanTags, t)
	}
	out.Tags = cleanTags
	out.Summary = strings.TrimSpace(out.Summary)
	out.Custom = strings.TrimSpace(out.Custom)
	return out
}

// --- Settings UI for Web Clipper (Native Messaging Host) ---

func (a *App) refreshClipperTargets() {
	home, _ := a.resolveAgentHomeDir()
	a.clipperTargets = clipper.DetectBrowsers(home, a.cfg.ConfigDir)
	a.clipperChecked = true
	if a.prefs != nil && a.clipperExtInput == "" && len(a.prefs.ClipperExtensionIDs) > 0 {
		a.clipperExtInput = strings.Join(a.prefs.ClipperExtensionIDs, ", ")
	}
}

func parseClipperExtraIDs(input string) []string {
	parts := strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == ' ' || r == '\t'
	})
	var out []string
	seen := map[string]bool{clipper.DefaultExtensionID: true}
	for _, part := range parts {
		if id, ok := clipper.ParseExtensionID(part); ok && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func (a *App) installClipperHostAction() {
	home, err := a.resolveAgentHomeDir()
	if err != nil {
		a.notify("读取主目录失败。%v", err)
		return
	}
	extra := parseClipperExtraIDs(a.clipperExtInput)
	if a.prefs != nil {
		a.prefs.ClipperExtensionIDs = extra
		a.savePrefs()
	}
	binPath := a.agentCommandPath()
	res, err := clipper.InstallHost(home, a.cfg.ConfigDir, binPath, extra)
	if err != nil {
		a.notify("安装浏览器剪藏连接失败。%v", err)
		return
	}
	a.refreshClipperTargets()
	a.notify("已为 %d 个浏览器安装剪藏连接。", res.InstalledCount)
}

func (a *App) uninstallClipperHostAction() {
	home, err := a.resolveAgentHomeDir()
	if err != nil {
		a.notify("读取主目录失败。%v", err)
		return
	}
	removed, err := clipper.UninstallHost(home, a.cfg.ConfigDir)
	if err != nil {
		a.notify("卸载浏览器剪藏连接失败。%v", err)
		return
	}
	a.refreshClipperTargets()
	if removed == 0 {
		a.notify("未检测到已安装的浏览器剪藏连接。")
		return
	}
	a.notify("已移除 %d 个浏览器的剪藏连接。", removed)
}

func (a *App) settingsWebClipper(c *ui.Context, sep func(), row func(key, label, note string, control func())) {
	settingsHeading(c, i18n.T("浏览器剪藏 (Web Clipper)"), i18n.T("搭配 Chrome / Edge / Arc / Brave 扩展一键剪藏网页正文、高亮批注与防盗链图片到本地空间或多维数据库，离线亦可静默写入。"))
	if !a.clipperChecked {
		a.refreshClipperTargets()
	}
	var detectedNames, installedNames []string
	for _, tgt := range a.clipperTargets {
		if tgt.Detected {
			detectedNames = append(detectedNames, tgt.Name)
		}
		if tgt.Installed {
			installedNames = append(installedNames, tgt.Name)
		}
	}
	hostNote := i18n.T("写入用户态 Native Messaging 宿主配置（免管理员权限，零常驻端口）")
	switch {
	case len(installedNames) > 0:
		hostNote = fmt.Sprintf(i18n.T("已配置：%s（点击可更新路径或附加扩展 ID）"), strings.Join(installedNames, " / "))
	case len(detectedNames) > 0:
		hostNote = fmt.Sprintf(i18n.T("已检测到 %s，点击即可一键安装连接"), strings.Join(detectedNames, " / "))
	}

	settingsGroup(c, "web-clipper", func() {
		row("clipper-host", i18n.T("浏览器连接宿主"), hostNote, func() {
			if ui.Button(c.Key("install-clipper"), i18n.T("一键安装连接")).Clicked() {
				a.installClipperHostAction()
			}
			if len(installedNames) > 0 {
				if ui.Button(c.Key("uninstall-clipper"), i18n.T("卸载")).Clicked() {
					a.uninstallClipperHostAction()
				}
			}
			if ui.Button(c.Key("open-clipper-repo"), i18n.T("获取扩展")).Clicked() {
				_ = mygo.Shell.OpenExternal(clipper.ExtensionRepoURL)
			}
		})
		sep()
		row("clipper-ext-ids", i18n.T("附加扩展 ID（可选）"), i18n.T("官方扩展开箱即用；自定义打包时可填逗号分隔的 32 位扩展 ID 并重新安装"), func() {
			ui.TextInput(c.Key("clipper-ext-input"), &a.clipperExtInput).Width(320).
				Placeholder(clipper.DefaultExtensionID).Label(i18n.T("附加扩展 ID"))
		})
	})
}
