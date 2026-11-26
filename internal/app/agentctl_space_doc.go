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
	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/Ciao1019/ArcMD/internal/history"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/spacefs"
	"github.com/Ciao1019/ArcMD/internal/templates"
	"github.com/Ciao1019/ArcMD/internal/trashbin"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

type spaceSummary struct {
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	Icon           string      `json:"icon,omitempty"`
	Symbol         string      `json:"symbol,omitempty"`
	Color          string      `json:"color,omitempty"`
	Theme          space.Theme `json:"theme"`
	Root           string      `json:"root"`
	AttachmentsDir string      `json:"attachmentsDir,omitempty"`
	Active         bool        `json:"active"`
	IsExternal     bool        `json:"isExternal,omitempty"`
	Pins           []string    `json:"pins,omitempty"`
	LastOpen       string      `json:"lastOpen,omitempty"`
	DocCount       int         `json:"docCount"`
}

func (a *App) summarizeSpace(sp *space.Space, activeID string) spaceSummary {
	count := 0
	if sp.IsExternal() {
		count = len(sp.ExternalFiles)
	} else if docs, err := vault.List(sp.Root); err == nil {
		count = len(docs)
	}
	return spaceSummary{
		ID:             sp.ID,
		Name:           sp.Name,
		Icon:           sp.Icon,
		Symbol:         sp.Symbol,
		Color:          sp.Color,
		Theme:          space.ThemeOf(sp),
		Root:           sp.Root,
		AttachmentsDir: sp.AttachmentsDir,
		Active:         sp.ID == activeID,
		IsExternal:     sp.IsExternal(),
		Pins:           slices.Clone(sp.Pins),
		LastOpen:       sp.LastOpen,
		DocCount:       count,
	}
}

func (a *App) ctlStatus() (any, error) {
	active := a.state.ActiveSpace()
	activeID := ""
	var activeInfo *spaceSummary
	if active != nil {
		activeID = active.ID
		s := a.summarizeSpace(active, activeID)
		activeInfo = &s
	}
	var openDoc any
	if a.doc != nil {
		openDoc = map[string]any{
			"space": a.doc.space.Name,
			"path":  a.doc.rel,
			"kind":  docKind(a.doc.rel),
			"dirty": a.doc.dirty(),
		}
	}
	return map[string]any{
		"version":      Version,
		"liveWindow":   a.isLiveWindow(),
		"configDir":    a.cfg.ConfigDir,
		"documentsDir": a.cfg.DocumentsDir,
		"spaceCount":   len(a.state.Spaces),
		"activeSpace":  activeInfo,
		"openDocument": openDoc,
	}, nil
}

func (a *App) ctlSpaceList() (any, error) {
	activeID := ""
	if sp := a.state.ActiveSpace(); sp != nil {
		activeID = sp.ID
	}
	out := make([]spaceSummary, 0, len(a.state.Spaces))
	for _, sp := range a.state.Spaces {
		out = append(out, a.summarizeSpace(sp, activeID))
	}
	return map[string]any{
		"active":    activeID,
		"spaces":    out,
		"favorites": slices.Clone(a.state.Favorites),
		"bookmarks": slices.Clone(a.state.Bookmarks),
	}, nil
}

func (a *App) ctlSpaceCreate(raw []byte) (any, error) {
	p := struct {
		Name       string   `json:"name"`
		Root       string   `json:"root"`
		Icon       string   `json:"icon"`
		Symbol     string   `json:"symbol"`
		Colors     []string `json:"colors"`
		Appearance string   `json:"appearance"`
		Intensity  *float64 `json:"intensity"`
		Noise      *float64 `json:"noise"`
		Starter    bool     `json:"starter"`
		Switch     *bool    `json:"switch"`
	}{}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return nil, errors.New("space name is required")
	}
	root := strings.TrimSpace(p.Root)
	if root == "" {
		root = a.defaultRoot(name)
	} else {
		abs, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		root = abs
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	sp := a.state.Add(name, root)
	if p.Symbol != "" || p.Icon != "" {
		icon := strings.TrimSpace(p.Icon)
		if icon == "-" {
			icon = ""
		}
		sp.SetIcon(icon, strings.TrimSpace(p.Symbol))
	}
	th := space.ThemeOf(sp)
	if err := applyThemePatch(&th, p.Colors, p.Appearance, p.Intensity, p.Noise); err != nil {
		_ = a.state.Remove(sp.ID)
		return nil, err
	}
	sp.SetTheme(th)
	if p.Starter {
		if docs, err := vault.List(root); err == nil && len(docs) == 0 {
			if sf := starterFile(); writeStarter(root, sf) == nil {
				sp.LastOpen = sf
			}
		}
	}
	doSwitch := p.Switch == nil || *p.Switch
	if doSwitch {
		if a.isLiveWindow() {
			if err := a.saveStateStrict(); err != nil {
				return nil, err
			}
			a.switchSpace(sp.ID)
			a.syncWatches()
		} else {
			_ = a.state.SetActive(sp.ID)
			if err := a.saveStateStrict(); err != nil {
				return nil, err
			}
		}
	} else {
		if err := a.saveStateStrict(); err != nil {
			return nil, err
		}
	}
	activeID := ""
	if act := a.state.ActiveSpace(); act != nil {
		activeID = act.ID
	}
	return a.summarizeSpace(sp, activeID), nil
}

func (a *App) ctlSpaceUpdate(raw []byte) (any, error) {
	var p struct {
		Space          string   `json:"space"`
		Name           *string  `json:"name"`
		Icon           *string  `json:"icon"`
		Symbol         *string  `json:"symbol"`
		Colors         []string `json:"colors"`
		Appearance     string   `json:"appearance"`
		Intensity      *float64 `json:"intensity"`
		Noise          *float64 `json:"noise"`
		AttachmentsDir *string  `json:"attachmentsDir"`
		Switch         bool     `json:"switch"`
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	sp, err := a.resolveTargetSpace(p.Space)
	if err != nil {
		return nil, err
	}
	if p.Name != nil {
		if sp.IsExternal() {
			return nil, errors.New("cannot rename the built-in External space")
		}
		trimmed := strings.TrimSpace(*p.Name)
		if trimmed == "" {
			return nil, errors.New("space name cannot be empty")
		}
		sp.Name = trimmed
	}
	if p.Icon != nil || p.Symbol != nil {
		icon := sp.Icon
		sym := sp.Symbol
		if p.Icon != nil {
			icon = strings.TrimSpace(*p.Icon)
			if icon == "-" {
				icon = ""
			}
			if p.Symbol == nil {
				sym = ""
			}
		}
		if p.Symbol != nil {
			sym = strings.TrimSpace(*p.Symbol)
			if sym == "-" {
				sym = ""
			}
		}
		sp.SetIcon(icon, sym)
	}
	if len(p.Colors) > 0 || p.Appearance != "" || p.Intensity != nil || p.Noise != nil {
		th := space.ThemeOf(sp)
		if err := applyThemePatch(&th, p.Colors, p.Appearance, p.Intensity, p.Noise); err != nil {
			return nil, err
		}
		sp.SetTheme(th)
	}
	if p.AttachmentsDir != nil {
		if sp.IsExternal() {
			return nil, errors.New("cannot set attachments folder on External space")
		}
		dir := strings.TrimSpace(*p.AttachmentsDir)
		if dir == "-" {
			dir = ""
		}
		if !sp.SetAttachmentsDir(dir) {
			return nil, fmt.Errorf("invalid attachments folder %q", *p.AttachmentsDir)
		}
		if sp.AttachmentsDir != "" && sp.AttachmentsDir != "." {
			full, rErr := vault.Resolve(sp.Root, sp.AttachmentsDir)
			if rErr != nil {
				return nil, rErr
			}
			if mkErr := os.MkdirAll(full, 0o755); mkErr != nil {
				return nil, mkErr
			}
		}
		delete(a.pageTrees, sp.ID)
		a.invalidateResolvedImages()
	}
	if p.Switch {
		if a.isLiveWindow() {
			a.switchSpace(sp.ID)
			a.syncWatches()
		} else {
			_ = a.state.SetActive(sp.ID)
		}
	}
	if a.state.ActiveSpace() == sp {
		a.refreshTree()
	}
	if err := a.saveStateStrict(); err != nil {
		return nil, err
	}
	activeID := ""
	if act := a.state.ActiveSpace(); act != nil {
		activeID = act.ID
	}
	return a.summarizeSpace(sp, activeID), nil
}

func applyThemePatch(th *space.Theme, colors []string, appearance string, intensity, noise *float64) error {
	if len(colors) > 0 {
		if len(colors) > 3 {
			return errors.New("a space theme accepts at most 3 hex colors")
		}
		cleaned := make([]string, len(colors))
		for i, c := range colors {
			c = strings.TrimSpace(c)
			if !strings.HasPrefix(c, "#") && len(c) == 6 {
				c = "#" + c
			}
			if !space.ValidHex(c) {
				return fmt.Errorf("invalid hex color %q (use #RRGGBB)", colors[i])
			}
			cleaned[i] = strings.ToUpper(c)
		}
		th.Colors = cleaned
	}
	if appearance != "" {
		app := strings.ToLower(strings.TrimSpace(appearance))
		if app != space.AppearanceAuto && app != space.AppearanceLight && app != space.AppearanceDark {
			return fmt.Errorf("invalid appearance %q (use auto, light, or dark)", appearance)
		}
		th.Appearance = app
	}
	if intensity != nil {
		th.Intensity = min(1, max(0, *intensity))
	}
	if noise != nil {
		th.Noise = min(1, max(0, *noise))
	}
	return nil
}

func (a *App) ctlSpaceSwitch(raw []byte) (any, error) {
	var p struct {
		Space string `json:"space"`
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Space) == "" {
		return nil, errors.New("space is required")
	}
	sp, err := a.resolveTargetSpace(p.Space)
	if err != nil {
		return nil, err
	}
	if a.isLiveWindow() {
		a.switchSpace(sp.ID)
		a.syncWatches()
	} else {
		if err := a.state.SetActive(sp.ID); err != nil {
			return nil, err
		}
		if err := a.saveStateStrict(); err != nil {
			return nil, err
		}
	}
	activeID := ""
	if act := a.state.ActiveSpace(); act != nil {
		activeID = act.ID
	}
	return a.summarizeSpace(sp, activeID), nil
}

func (a *App) ctlSpaceRemove(raw []byte) (any, error) {
	var p struct {
		Space string `json:"space"`
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Space) == "" {
		return nil, errors.New("space is required")
	}
	sp, err := a.resolveTargetSpace(p.Space)
	if err != nil {
		return nil, err
	}
	if sp.IsExternal() {
		return nil, errors.New("cannot remove the built-in External space")
	}
	regularCount := 0
	for _, item := range a.state.Spaces {
		if !item.IsExternal() {
			regularCount++
		}
	}
	if regularCount < 2 {
		return nil, errors.New("cannot remove the last Space in ArcMD")
	}
	keptRoot := sp.Root
	removedID := sp.ID
	removedName := sp.Name
	wasActive := a.state.ActiveSpace() != nil && a.state.ActiveSpace().ID == removedID
	if a.isLiveWindow() {
		if !a.flush() {
			return nil, errors.New(i18n.T("请先保存文档并解决文件冲突"))
		}
		if wasActive {
			a.suspendSplit()
		}
		a.releasePDFHandles(sp, "")
		for _, d := range a.residentDocuments() {
			if d.space.ID == removedID {
				a.replaceReferences(d, nil)
			}
		}
	}
	if err := a.state.Remove(removedID); err != nil {
		return nil, err
	}
	a.forgetCopiedSpace(removedID)
	delete(a.sessions, removedID)
	delete(a.pageTrees, removedID)
	a.invalidateConflictsCache()
	if err := a.saveStateStrict(); err != nil {
		return nil, err
	}
	if a.isLiveWindow() {
		a.refreshTree()
		if act := a.state.ActiveSpace(); act != nil && (wasActive || a.doc == nil) && act.LastOpen != "" {
			a.open(act.LastOpen)
		}
		a.refreshSearch()
		a.syncWatches()
		if a.library.open {
			a.loadLibrary()
		}
	}
	return map[string]any{
		"removedID":   removedID,
		"removedName": removedName,
		"keptFolder":  keptRoot,
		"activeSpace": a.state.Active,
	}, nil
}

func docKind(rel string) string {
	switch {
	case vault.IsDatabase(rel):
		return "database"
	case vault.IsWhiteboard(rel):
		return "whiteboard"
	case vault.IsPDF(rel):
		return "pdf"
	case vault.IsBook(rel):
		return "book"
	case vault.IsMDZ(rel):
		return "mdz"
	default:
		return "markdown"
	}
}

type docEntrySummary struct {
	Path     string `json:"path"`
	Title    string `json:"title"`
	Dir      bool   `json:"dir,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Pinned   bool   `json:"pinned,omitempty"`
	Favorite bool   `json:"favorite,omitempty"`
	Open     bool   `json:"open,omitempty"`
	Dirty    bool   `json:"dirty,omitempty"`
}

func (a *App) ctlDocList(raw []byte) (any, error) {
	var p struct {
		Space string `json:"space"`
		Dir   string `json:"dir"`
		Kind  string `json:"kind"`
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	sp, err := a.resolveTargetSpace(p.Space)
	if err != nil {
		return nil, err
	}
	tree, err := readTree(sp)
	if err != nil {
		return nil, err
	}
	subDir := strings.Trim(strings.ReplaceAll(strings.TrimSpace(p.Dir), `\`, "/"), "/")
	if subDir == "." {
		subDir = ""
	}
	kindFilter := strings.ToLower(strings.TrimSpace(p.Kind))

	var items []docEntrySummary
	var walk func(nodes []*vault.Node)
	walk = func(nodes []*vault.Node) {
		for _, n := range nodes {
			inScope := subDir == "" || n.Path == subDir || strings.HasPrefix(n.Path, subDir+"/")
			if n.Dir {
				if inScope && n.Path != subDir && kindFilter == "" {
					items = append(items, docEntrySummary{
						Path:   n.Path,
						Title:  n.Name,
						Dir:    true,
						Pinned: sp.IsPinned(n.Path),
					})
				}
				if subDir == "" || inScope || strings.HasPrefix(subDir, n.Path+"/") {
					walk(n.Children)
				}
				continue
			}
			if !inScope {
				continue
			}
			k := docKind(n.Path)
			if kindFilter != "" && k != kindFilter {
				continue
			}
			res := a.residentDocument(sp.ID, n.Path)
			items = append(items, docEntrySummary{
				Path:     n.Path,
				Title:    n.Title(),
				Kind:     k,
				Pinned:   sp.IsPinned(n.Path),
				Favorite: a.state.IsFavorite(sp.ID, n.Path),
				Open:     res != nil,
				Dirty:    res != nil && res.dirty(),
			})
		}
	}
	walk(tree)
	return map[string]any{
		"space": sp.Name,
		"root":  sp.Root,
		"count": len(items),
		"items": items,
	}, nil
}

func (a *App) ctlDocSearch(raw []byte) (any, error) {
	var p struct {
		Space   string `json:"space"`
		Query   string `json:"query"`
		Mode    string `json:"mode"`
		Regex   bool   `json:"regex"`
		Scope   string `json:"scope"`
		Context int    `json:"context"`
		Limit   int    `json:"limit"`
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	sp, err := a.resolveTargetSpace(p.Space)
	if err != nil {
		return nil, err
	}
	vfs := a.qaSpace(sp.ID)
	if vfs == nil {
		return nil, fmt.Errorf("cannot open space %q", sp.Name)
	}
	mode := strings.ToLower(strings.TrimSpace(p.Mode))
	if mode == "" {
		mode = "search"
	}
	var out string
	switch mode {
	case "overview":
		out, err = vfs.Overview(8000)
	case "links":
		out, err = vfs.Links(strings.TrimSpace(p.Scope))
	case "grep":
		out, err = vfs.Grep(spacefs.GrepArgs{
			Pattern: p.Query,
			Regex:   p.Regex,
			Scope:   p.Scope,
			Context: p.Context,
		})
	case "search":
		out, err = vfs.Search(spacefs.SearchArgs{
			Query: p.Query,
			Scope: p.Scope,
			Limit: p.Limit,
		})
	default:
		return nil, fmt.Errorf("unknown search mode %q (use search, grep, links, or overview)", p.Mode)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"space":  sp.Name,
		"mode":   mode,
		"output": out,
	}, nil
}

func (a *App) ctlDocRead(raw []byte) (any, error) {
	var p struct {
		Space           string `json:"space"`
		Path            string `json:"path"`
		StartLine       int    `json:"startLine"`
		EndLine         int    `json:"endLine"`
		WithLineNumbers bool   `json:"withLineNumbers"`
		IncludeOutline  bool   `json:"includeOutline"`
		IncludeLinks    bool   `json:"includeLinks"`
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	sp, err := a.resolveTargetSpace(p.Space)
	if err != nil {
		return nil, err
	}
	rel, err := normalizeDocRel(sp, p.Path)
	if err != nil {
		return nil, err
	}
	k := docKind(rel)
	switch k {
	case "whiteboard":
		scene, err := a.loadSceneForControl(sp, rel)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"space":        sp.Name,
			"path":         rel,
			"kind":         k,
			"summary":      scene.PlainText(),
			"elementCount": len(scene.Elements),
			"elements":     scene.Elements,
		}, nil

	case "database":
		st, _, err := a.openDBStoreForControl(sp, rel)
		if err != nil {
			return nil, err
		}
		db := st.Database()
		snap := database.RenderGFMSnapshot(cleanRelDir(path.Dir(rel)), db, st.Query(db.ActiveView, database.QueryOptions{}))
		return map[string]any{
			"space":       sp.Name,
			"path":        rel,
			"kind":        k,
			"title":       db.Title,
			"icon":        db.Icon,
			"cover":       db.Cover,
			"source":      db.Source,
			"properties":  db.Properties,
			"views":       db.Views,
			"activeView":  db.ActiveView,
			"recordCount": len(st.Records()),
			"snapshot":    snap,
		}, nil

	case "pdf", "book", "mdz":
		vfs := a.qaSpace(sp.ID)
		if vfs == nil {
			return nil, errors.New("space reader unavailable")
		}
		txt, err := vfs.Read(rel, p.StartLine, p.EndLine)
		if err != nil {
			return nil, err
		}
		res := map[string]any{
			"space":   sp.Name,
			"path":    rel,
			"kind":    k,
			"content": txt,
		}
		if p.IncludeOutline {
			if ol, oErr := vfs.Outline(rel); oErr == nil {
				res["outline"] = ol
			}
		}
		return res, nil
	}

	text, liveDirty, err := a.readMarkdownForControl(sp, rel)
	if err != nil {
		return nil, err
	}
	doc := editor.ImportMarkdown(text)
	totalLines, sliced := sliceTextLines(text, p.StartLine, p.EndLine, p.WithLineNumbers)
	res := map[string]any{
		"space":      sp.Name,
		"path":       rel,
		"kind":       k,
		"title":      vault.Title(rel),
		"dirty":      liveDirty,
		"totalLines": totalLines,
		"content":    sliced,
		"metadata": map[string]any{
			"icon":        doc.Icon,
			"cover":       doc.Cover,
			"coverY":      doc.CoverY,
			"goal":        doc.Goal,
			"frontmatter": doc.Frontmatter,
		},
	}
	if p.IncludeOutline || p.IncludeLinks {
		if vfs := a.qaSpace(sp.ID); vfs != nil {
			if p.IncludeOutline {
				if ol, oErr := vfs.Outline(rel); oErr == nil {
					res["outline"] = ol
				}
			}
			if p.IncludeLinks {
				if lk, lErr := vfs.Links(rel); lErr == nil {
					res["links"] = lk
				}
			}
		}
	}
	return res, nil
}

func sliceTextLines(text string, startLine, endLine int, number bool) (total int, out string) {
	norm := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(strings.TrimSuffix(norm, "\n"), "\n")
	if norm == "" {
		lines = nil
	}
	total = len(lines)
	if startLine <= 0 && endLine <= 0 && !number {
		return total, text
	}
	from := max(1, startLine)
	to := total
	if endLine > 0 && endLine < to {
		to = endLine
	}
	if from > total || total == 0 {
		return total, ""
	}
	var b strings.Builder
	for i := from; i <= to; i++ {
		if i > from {
			b.WriteByte('\n')
		}
		if number {
			fmt.Fprintf(&b, "L%d: %s", i, lines[i-1])
		} else {
			b.WriteString(lines[i-1])
		}
	}
	return total, b.String()
}

func (a *App) readMarkdownForControl(sp *space.Space, rel string) (text string, dirty bool, err error) {
	if res := a.residentDocument(sp.ID, rel); res != nil && res.loadErr == nil && !res.isWhiteboard() && !res.isDatabase() && !res.isPDF() && !res.isBook() && !res.isMDZ() {
		return editor.ExportMarkdown(res.ed.Committed()), res.dirty(), nil
	}
	raw, err := readDocSource(sp, rel)
	if err != nil {
		return "", false, err
	}
	if strings.EqualFold(path.Ext(rel), ".arcmd") {
		d, decErr := model.Decode(raw)
		if decErr != nil {
			return "", false, decErr
		}
		return editor.ExportMarkdown(d), false, nil
	}
	return raw, false, nil
}

func normalizeDocRel(sp *space.Space, rawRel string) (string, error) {
	rel := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(rawRel), `\`, "/"), "/")
	if rel == "" {
		return "", errors.New("document path is required")
	}
	if sp.IsExternal() {
		if _, err := vault.Files(sp.ExternalFiles).Resolve(rel); err == nil {
			return rel, nil
		}
		return "", fmt.Errorf("document %q not found in External space", rel)
	}
	if _, err := vault.Resolve(sp.Root, rel); err == nil {
		if _, statErr := os.Stat(filepath.Join(sp.Root, filepath.FromSlash(rel))); statErr == nil {
			return path.Clean(rel), nil
		}
		if vault.FileExt(rel) == "" {
			withMD := path.Clean(rel + ".md")
			if _, err2 := vault.Resolve(sp.Root, withMD); err2 == nil {
				if _, statErr := os.Stat(filepath.Join(sp.Root, filepath.FromSlash(withMD))); statErr == nil {
					return withMD, nil
				}
			}
		}
		return path.Clean(rel), nil
	}
	return "", vault.ErrOutside
}

func (a *App) ctlDocCreate(raw []byte) (any, error) {
	var p struct {
		Space       string         `json:"space"`
		Path        string         `json:"path"`
		Dir         string         `json:"dir"`
		Title       string         `json:"title"`
		Content     string         `json:"content"`
		Template    string         `json:"template"`
		Daily       bool           `json:"daily"`
		Icon        string         `json:"icon"`
		Cover       string         `json:"cover"`
		CoverY      *float64       `json:"coverY"`
		Goal        *int           `json:"goal"`
		Frontmatter map[string]any `json:"frontmatter"`
		Overwrite   bool           `json:"overwrite"`
		Pin         bool           `json:"pin"`
		Favorite    bool           `json:"favorite"`
		Open        bool           `json:"open"`
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	sp, err := a.resolveTargetSpace(p.Space)
	if err != nil {
		return nil, err
	}
	if sp.IsExternal() {
		return nil, errors.New("cannot create new files inside External space")
	}

	rawPath := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(p.Path), `\`, "/"), "/")
	if rawPath != "" {
		lowerPath := strings.ToLower(rawPath)
		switch {
		case vault.IsWhiteboard(rawPath) || strings.HasSuffix(lowerPath, ".excalidraw"):
			return nil, errors.New("use whiteboard_write / 'arcmd board write' to create .excalidraw.svg whiteboards")
		case vault.IsDatabase(rawPath):
			return nil, errors.New("use database_schema / 'arcmd db create' to create .db.md databases")
		case vault.IsPDF(rawPath) || vault.IsBook(rawPath) || vault.IsMDZ(rawPath) || strings.HasSuffix(lowerPath, ".pdf") ||
			strings.HasSuffix(lowerPath, ".epub") || strings.HasSuffix(lowerPath, ".html") || strings.HasSuffix(lowerPath, ".htm") || strings.HasSuffix(lowerPath, ".mdz"):
			return nil, fmt.Errorf("%s cannot be created with doc_create; only Markdown documents are supported", rawPath)
		}
	}

	hasMeta := strings.TrimSpace(p.Icon) != "" || strings.TrimSpace(p.Cover) != "" || p.CoverY != nil || p.Goal != nil || len(p.Frontmatter) > 0
	now := time.Now()
	if p.Daily {
		rel := templates.PathFor(now)
		existed := false
		if full, rErr := vault.Resolve(sp.Root, rel); rErr == nil {
			if _, stErr := os.Stat(full); stErr == nil {
				existed = true
			}
		}
		if !existed {
			var tmplDoc *model.Document
			for _, t := range a.spaceTemplates(sp) {
				if strings.EqualFold(t.Name, "日记") || strings.EqualFold(t.Name, "Daily") {
					tmplDoc, _ = readTemplate(sp, t)
					break
				}
			}
			initial := editor.ExportMarkdown(templates.Daily(tmplDoc, now))
			if strings.TrimSpace(p.Content) != "" {
				initial = strings.TrimRight(initial, "\n") + "\n\n" + strings.TrimSpace(p.Content) + "\n"
			}
			initial = applyMetadataPatch(initial, strPtrIfNotEmpty(p.Icon), strPtrIfNotEmpty(p.Cover), p.CoverY, p.Goal, p.Frontmatter)
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
		if existed && (strings.TrimSpace(p.Content) != "" || hasMeta) {
			cur, _, rErr := a.readMarkdownForControl(sp, rel)
			if rErr != nil {
				return nil, rErr
			}
			next := cur
			if strings.TrimSpace(p.Content) != "" {
				next = strings.TrimRight(cur, "\n") + "\n\n" + strings.TrimSpace(p.Content) + "\n"
			}
			next = applyMetadataPatch(next, strPtrIfNotEmpty(p.Icon), strPtrIfNotEmpty(p.Cover), p.CoverY, p.Goal, p.Frontmatter)
			if err := a.commitMarkdownForControl(sp, rel, next); err != nil {
				return nil, err
			}
		}
		a.finishDocPostActions(sp, rel, p.Pin, p.Favorite, p.Open)
		return map[string]any{"space": sp.Name, "path": rel, "existed": existed}, nil
	}

	title := strings.TrimSpace(p.Title)
	relPath := rawPath
	if relPath != "" && vault.FileExt(relPath) == "" {
		relPath += ".md"
	}
	if title == "" && relPath != "" {
		title = vault.Title(relPath)
	}
	if title == "" {
		title = untitled
	}

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

	body = applyMetadataPatch(body, strPtrIfNotEmpty(p.Icon), strPtrIfNotEmpty(p.Cover), p.CoverY, p.Goal, p.Frontmatter)

	var finalRel string
	if relPath != "" {
		clean := path.Clean(relPath)
		full, rErr := vault.Resolve(sp.Root, clean)
		if rErr != nil {
			return nil, rErr
		}
		if p.Overwrite {
			if err := a.commitMarkdownForControl(sp, clean, body); err != nil {
				return nil, err
			}
		} else {
			if err := fsutil.CreateFileAtomic(full, []byte(body), 0o644); err != nil {
				return nil, err
			}
			a.noteExternalWrite(sp, clean)
		}
		finalRel = clean
	} else {
		dir := strings.Trim(strings.ReplaceAll(strings.TrimSpace(p.Dir), `\`, "/"), "/")
		if dir == "." {
			dir = ""
		}
		created, cErr := vault.CreateFile(sp.Root, dir, title, ".md", body)
		if cErr != nil {
			return nil, cErr
		}
		finalRel = created
		a.noteExternalWrite(sp, finalRel)
	}

	a.finishDocPostActions(sp, finalRel, p.Pin, p.Favorite, p.Open)
	return map[string]any{
		"space": sp.Name,
		"path":  finalRel,
		"title": vault.Title(finalRel),
	}, nil
}

func (a *App) finishDocPostActions(sp *space.Space, rel string, pin, fav, open bool) {
	stateChanged := false
	if fav && !a.state.IsFavorite(sp.ID, rel) {
		a.state.ToggleFavorite(sp.ID, rel)
		sp.Unpin(rel)
		stateChanged = true
	} else if pin && !sp.IsPinned(rel) {
		if a.state.IsFavorite(sp.ID, rel) {
			a.state.ToggleFavorite(sp.ID, rel)
		}
		sp.Pin(rel, len(sp.Pins))
		stateChanged = true
	}
	if dir := path.Dir(rel); dir != "." && dir != "" && !sp.IsExpanded(dir) {
		sp.SetExpanded(dir, true)
		stateChanged = true
	}
	if open && !a.isLiveWindow() && sp.LastOpen != rel {
		sp.LastOpen = rel
		stateChanged = true
	}
	if stateChanged {
		_ = a.saveStateStrict()
	}
	delete(a.pageTrees, sp.ID)
	a.invalidateResolvedImages()
	a.invalidateConflictsCache()
	a.invalidatePageDBCache(sp.ID)
	if a.state.ActiveSpace() == sp {
		a.refreshTree()
	}
	if a.isLiveWindow() {
		a.refreshOpenDatabases(sp.ID)
		a.refreshSearch()
		if open {
			if a.state.ActiveSpace() != sp {
				a.switchSpaceOnly(sp.ID)
			}
			a.openIn(sp, rel)
			a.showMainWindow()
		}
	}
}

func strPtrIfNotEmpty(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

func ensureTrailingNewlineStr(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	if !strings.HasSuffix(s, "\n") {
		return s + "\n"
	}
	return s
}

func applyMetadataPatch(source string, icon, cover *string, coverY *float64, goal *int, extra map[string]any) string {
	if icon == nil && cover == nil && coverY == nil && goal == nil && len(extra) == 0 {
		return source
	}
	return database.ApplyFrontmatter(source, func(fm *database.FrontmatterDoc) {
		if icon != nil {
			v := strings.TrimSpace(*icon)
			if v == "" || v == "-" {
				fm.Delete("icon")
			} else {
				fm.Set("icon", database.FMValue{Kind: database.FMScalar, Scalar: v})
			}
		}
		if cover != nil {
			v := strings.TrimSpace(*cover)
			if v == "" || v == "-" {
				fm.Delete("cover")
				fm.Delete("coverY")
			} else {
				fm.Set("cover", database.FMValue{Kind: database.FMScalar, Scalar: v})
			}
		}
		if coverY != nil {
			y := min(1, max(-1, *coverY))
			if y == 0 {
				fm.Delete("coverY")
			} else {
				fm.Set("coverY", database.FMValue{Kind: database.FMNumber, Number: y})
			}
		}
		if goal != nil {
			if *goal <= 0 {
				fm.Delete("goal")
			} else {
				fm.Set("goal", database.FMValue{Kind: database.FMNumber, Number: float64(*goal)})
			}
		}
		if len(extra) > 0 {
			keys := make([]string, 0, len(extra))
			for k := range extra {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			for _, k := range keys {
				rawV := extra[k]
				if rawV == nil {
					fm.Delete(k)
					continue
				}
				fm.Set(k, anyToFMValue(rawV))
			}
		}
	})
}

func anyToFMValue(v any) database.FMValue {
	switch tv := v.(type) {
	case bool:
		return database.FMValue{Kind: database.FMBool, Bool: tv}
	case float64:
		return database.FMValue{Kind: database.FMNumber, Number: tv}
	case int:
		return database.FMValue{Kind: database.FMNumber, Number: float64(tv)}
	case []string:
		return database.FMValue{Kind: database.FMList, List: slices.Clone(tv)}
	case []any:
		list := make([]string, 0, len(tv))
		for _, item := range tv {
			list = append(list, fmt.Sprint(item))
		}
		return database.FMValue{Kind: database.FMList, List: list}
	default:
		return database.FMValue{Kind: database.FMScalar, Scalar: fmt.Sprint(tv)}
	}
}

func (a *App) ctlDocEdit(raw []byte) (any, error) {
	var p struct {
		Space       string         `json:"space"`
		Path        string         `json:"path"`
		Mode        string         `json:"mode"`
		Content     string         `json:"content"`
		OldText     string         `json:"oldText"`
		NewText     string         `json:"newText"`
		ReplaceAll  bool           `json:"replaceAll"`
		Heading     string         `json:"heading"`
		Icon        *string        `json:"icon"`
		Cover       *string        `json:"cover"`
		CoverY      *float64       `json:"coverY"`
		Goal        *int           `json:"goal"`
		Frontmatter map[string]any `json:"frontmatter"`
		Open        bool           `json:"open"`
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	sp, err := a.resolveTargetSpace(p.Space)
	if err != nil {
		return nil, err
	}
	rel, err := normalizeDocRel(sp, p.Path)
	if err != nil {
		return nil, err
	}
	if vault.IsWhiteboard(rel) {
		return nil, errors.New("use whiteboard_write / 'arcmd board write' to edit .excalidraw.svg whiteboards")
	}
	if vault.IsDatabase(rel) {
		return nil, errors.New("use database_schema / database_records / 'arcmd db' to edit .db.md databases")
	}
	if vault.IsPDF(rel) || vault.IsBook(rel) || vault.IsMDZ(rel) {
		return nil, fmt.Errorf("%s is a read-only document", rel)
	}

	cur, _, err := a.readMarkdownForControl(sp, rel)
	if err != nil {
		return nil, err
	}

	mode := strings.ToLower(strings.TrimSpace(p.Mode))
	if mode == "" {
		mode = "replace"
	}
	var next string
	switch mode {
	case "replace":
		next = ensureTrailingNewlineStr(p.Content)
	case "patch":
		if p.OldText == "" {
			return nil, errors.New("oldText is required when mode='patch'")
		}
		count := strings.Count(cur, p.OldText)
		if count == 0 {
			return nil, errors.New("oldText was not found in document")
		}
		if count > 1 && !p.ReplaceAll {
			return nil, fmt.Errorf("oldText matched %d times; provide more surrounding context or set replaceAll=true", count)
		}
		if p.ReplaceAll {
			next = strings.ReplaceAll(cur, p.OldText, p.NewText)
		} else {
			next = strings.Replace(cur, p.OldText, p.NewText, 1)
		}
		next = ensureTrailingNewlineStr(next)
	case "append":
		next, err = insertMarkdownChunk(cur, p.Content, p.Heading, false)
		if err != nil {
			return nil, err
		}
	case "prepend":
		next, err = insertMarkdownChunk(cur, p.Content, p.Heading, true)
		if err != nil {
			return nil, err
		}
	case "frontmatter":
		next = cur
	default:
		return nil, fmt.Errorf("unknown edit mode %q (use replace, patch, append, prepend, or frontmatter)", p.Mode)
	}

	next = applyMetadataPatch(next, p.Icon, p.Cover, p.CoverY, p.Goal, p.Frontmatter)
	if err := a.commitMarkdownForControl(sp, rel, next); err != nil {
		return nil, err
	}
	if p.Open {
		a.finishDocPostActions(sp, rel, false, false, true)
	}
	return map[string]any{
		"space":      sp.Name,
		"path":       rel,
		"mode":       mode,
		"totalLines": strings.Count(strings.TrimSuffix(next, "\n"), "\n") + 1,
	}, nil
}

// insertMarkdownChunk inserts chunk either at the start/end of source (preserving
// YAML frontmatter at the top) or at the start/end of a named Markdown heading section.
func insertMarkdownChunk(source, chunk, heading string, atTop bool) (string, error) {
	trimmedChunk := strings.Trim(strings.ReplaceAll(strings.ReplaceAll(chunk, "\r\n", "\n"), "\r", "\n"), "\n")
	if trimmedChunk == "" {
		return ensureTrailingNewlineStr(source), nil
	}
	norm := strings.ReplaceAll(strings.ReplaceAll(source, "\r\n", "\n"), "\r", "\n")

	if strings.TrimSpace(heading) == "" {
		if !atTop {
			base := strings.TrimRight(norm, "\n")
			if base == "" {
				return trimmedChunk + "\n", nil
			}
			return base + "\n\n" + trimmedChunk + "\n", nil
		}
		fm, body, hasFM, _ := database.SplitFrontmatter(norm)
		cleanBody := strings.TrimLeft(body, "\n")
		var joinedBody string
		if cleanBody == "" {
			joinedBody = trimmedChunk + "\n"
		} else {
			joinedBody = trimmedChunk + "\n\n" + ensureTrailingNewlineStr(cleanBody)
		}
		if hasFM && !fm.IsEmpty() {
			return "---\n" + fm.Format() + "\n---\n\n" + joinedBody, nil
		}
		return joinedBody, nil
	}

	fm, body, hasFM, _ := database.SplitFrontmatter(norm)
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if strings.TrimSpace(body) == "" {
		lines = nil
	}
	wantRaw := strings.TrimSpace(heading)
	wantTitle := strings.TrimSpace(strings.TrimLeft(wantRaw, "#"))
	wantLevel := len(wantRaw) - len(strings.TrimLeft(wantRaw, "#"))

	headIdx := -1
	headLevel := 0
	fence := ""
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if f := fenceMarker(t); f != "" {
			if fence == "" {
				fence = f
			} else if strings.HasPrefix(t, fence) && strings.Trim(t, string(fence[0])) == "" {
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		lvl, title, ok := parseATXHeading(t)
		if !ok {
			continue
		}
		if (wantLevel > 0 && lvl == wantLevel && strings.EqualFold(title, wantTitle)) ||
			(wantLevel == 0 && strings.EqualFold(title, wantTitle)) {
			headIdx = i
			headLevel = lvl
			break
		}
	}
	if headIdx < 0 {
		return "", fmt.Errorf("heading %q not found in document", heading)
	}

	insertIdx := len(lines)
	if atTop {
		insertIdx = headIdx + 1
		for insertIdx < len(lines) && strings.TrimSpace(lines[insertIdx]) == "" {
			insertIdx++
		}
	} else {
		fence = ""
		for i := headIdx + 1; i < len(lines); i++ {
			t := strings.TrimSpace(lines[i])
			if f := fenceMarker(t); f != "" {
				if fence == "" {
					fence = f
				} else if strings.HasPrefix(t, fence) && strings.Trim(t, string(fence[0])) == "" {
					fence = ""
				}
				continue
			}
			if fence != "" {
				continue
			}
			if lvl, _, ok := parseATXHeading(t); ok && lvl <= headLevel {
				insertIdx = i
				break
			}
		}
	}

	before := strings.TrimRight(strings.Join(lines[:insertIdx], "\n"), "\n")
	after := strings.TrimLeft(strings.Join(lines[insertIdx:], "\n"), "\n")
	var reb strings.Builder
	if before != "" {
		reb.WriteString(before)
		reb.WriteString("\n\n")
	}
	reb.WriteString(trimmedChunk)
	if after != "" {
		reb.WriteString("\n\n")
		reb.WriteString(strings.TrimRight(after, "\n"))
	}
	reb.WriteByte('\n')
	newBody := reb.String()
	if hasFM && !fm.IsEmpty() {
		return "---\n" + fm.Format() + "\n---\n\n" + strings.TrimLeft(newBody, "\n"), nil
	}
	return newBody, nil
}

func fenceMarker(trimmed string) string {
	if strings.HasPrefix(trimmed, "```") {
		n := 0
		for n < len(trimmed) && trimmed[n] == '`' {
			n++
		}
		return trimmed[:n]
	}
	if strings.HasPrefix(trimmed, "~~~") {
		n := 0
		for n < len(trimmed) && trimmed[n] == '~' {
			n++
		}
		return trimmed[:n]
	}
	if trimmed == "$$" {
		return "$$"
	}
	return ""
}

func parseATXHeading(trimmed string) (level int, title string, ok bool) {
	if !strings.HasPrefix(trimmed, "#") {
		return 0, "", false
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == '#' {
		n++
	}
	if n < 1 || n > 6 || n >= len(trimmed) || (trimmed[n] != ' ' && trimmed[n] != '\t') {
		return 0, "", false
	}
	title = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(trimmed[n:]), "#"))
	return n, title, true
}

// commitMarkdownForControl saves nextText for (sp, rel). When the document is
// open in the GUI editor, it applies the edit as one undoable Engine step and
// flushes cleanly without triggering external-change conflict banners.
func (a *App) commitMarkdownForControl(sp *space.Space, rel, nextText string) error {
	if res := a.residentDocument(sp.ID, rel); res != nil && res.loadErr == nil && !res.isWhiteboard() && !res.isDatabase() && !res.isPDF() && !res.isBook() && !res.isMDZ() {
		res.waitAsyncSave()
		res.ed.UnmarkText()
		wasRO := res.ed.ReadOnly
		res.ed.ReadOnly = false
		res.ed.Engine.ReplaceDocument(editor.ImportMarkdown(nextText))
		res.ed.ReadOnly = wasRO
		if res.source {
			res.refreshSourceFromModel()
		}
		if err := res.write(true); err != nil {
			return err
		}
		a.syncSavedSearch(res)
		a.invalidatePageDBCache(sp.ID)
		a.refreshOpenDatabases(sp.ID)
		return nil
	}

	prevDisk, _ := readFile(sp, rel)
	if !sp.IsExternal() && prevDisk != "" {
		_ = history.Record(sp.Root, rel, prevDisk, time.Now().Add(-time.Second), true)
	}
	root, name, err := fileLocation(sp, rel)
	if err != nil {
		return err
	}
	if err := writeNative(root, name, nextText); err != nil {
		return err
	}
	if !sp.IsExternal() {
		_ = history.Record(sp.Root, rel, nextText, time.Now(), false)
	}
	a.noteExternalWrite(sp, rel)
	return nil
}

func (a *App) noteExternalWrite(sp *space.Space, rel string) {
	if full, err := resolveFile(sp, rel); err == nil && a.isLiveWindow() {
		if a.selfWrites == nil {
			a.selfWrites = map[string]time.Time{}
		}
		a.selfWrites[full] = time.Now()
	}
	delete(a.pageTrees, sp.ID)
	a.invalidateResolvedImages()
	a.invalidateConflictsCache()
	a.invalidatePageDBCache(sp.ID)
	if a.state.ActiveSpace() == sp {
		a.refreshTree()
	}
	if a.isLiveWindow() {
		a.refreshOpenDatabases(sp.ID)
		a.refreshSearch()
	}
}

func (a *App) ctlDocOrganize(raw []byte) (any, error) {
	var p struct {
		Space   string `json:"space"`
		Op      string `json:"op"`
		Path    string `json:"path"`
		NewName string `json:"newName"`
		ToSpace string `json:"toSpace"`
		ToDir   string `json:"toDir"`
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	sp, err := a.resolveTargetSpace(p.Space)
	if err != nil {
		return nil, err
	}
	op := strings.ToLower(strings.TrimSpace(p.Op))
	targetPath := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(p.Path), `\`, "/"), "/")

	switch op {
	case "mkdir":
		if sp.IsExternal() {
			return nil, errors.New("cannot create folders in External space")
		}
		if targetPath == "" || targetPath == "." {
			return nil, errors.New("folder path is required")
		}
		clean := path.Clean(targetPath)
		full, rErr := vault.Resolve(sp.Root, clean)
		if rErr != nil {
			return nil, rErr
		}
		if err := os.MkdirAll(full, 0o755); err != nil {
			return nil, err
		}
		sp.SetExpanded(clean, true)
		_ = a.saveStateStrict()
		a.noteExternalWrite(sp, clean)
		return map[string]any{"space": sp.Name, "path": clean}, nil

	case "open":
		rel, rErr := normalizeDocRel(sp, targetPath)
		if rErr != nil {
			return nil, rErr
		}
		full, fErr := resolveFile(sp, rel)
		if fErr != nil {
			return nil, fErr
		}
		info, stErr := os.Stat(full)
		if stErr != nil {
			if a.residentDocument(sp.ID, rel) == nil {
				return nil, fmt.Errorf("document %q not found in space %q", rel, sp.Name)
			}
		} else if info.IsDir() {
			return nil, fmt.Errorf("%q is a folder, not a document", rel)
		}
		if a.isLiveWindow() {
			if a.state.ActiveSpace() != sp && !a.state.IsFavorite(sp.ID, rel) {
				a.switchSpaceOnly(sp.ID)
			}
			a.openIn(sp, rel)
			if a.residentDocument(sp.ID, rel) == nil {
				return nil, fmt.Errorf("failed to open %s", rel)
			}
			a.showMainWindow()
			a.syncWatches()
		} else {
			sp.LastOpen = rel
			_ = a.state.SetActive(sp.ID)
			if err := a.saveStateStrict(); err != nil {
				return nil, err
			}
		}
		return map[string]any{"space": sp.Name, "path": rel, "opened": true}, nil

	case "rename":
		rel, rErr := normalizeDocRel(sp, targetPath)
		if rErr != nil {
			return nil, rErr
		}
		if !a.exists(sp, rel) {
			return nil, fmt.Errorf("%q not found in space %q", rel, sp.Name)
		}
		newName := strings.TrimSpace(p.NewName)
		if newName == "" {
			return nil, errors.New("newName is required for rename")
		}
		// Strip matching document extension if the caller passed "Title.md".
		if ext := vault.FileExt(rel); ext != "" && strings.EqualFold(vault.FileExt(newName), ext) {
			newName = strings.TrimSuffix(newName, vault.FileExt(newName))
		}
		if a.isLiveWindow() && !a.flush() {
			return nil, errors.New(i18n.T("请先保存文档并解决文件冲突"))
		}
		sources := a.renameSources(sp, rel)
		a.releasePDFHandles(sp, rel)
		to, renErr := renameFile(sp, rel, newName)
		if renErr != nil {
			a.reopenResidentPDFs(sp, rel)
			return nil, renErr
		}
		if to != rel {
			a.moved(sp, rel, to)
			if vault.IsWhiteboard(rel) || vault.IsPDF(rel) || vault.IsBook(rel) {
				a.relinkWhiteboardEmbed(sp, rel, to)
			}
			if vault.IsDocument(rel) {
				a.relink(vault.Title(rel), vault.Title(to), sources)
			}
		} else {
			a.reopenResidentPDFs(sp, rel)
		}
		return map[string]any{"space": sp.Name, "from": rel, "to": to}, nil

	case "move":
		rel, rErr := normalizeDocRel(sp, targetPath)
		if rErr != nil {
			return nil, rErr
		}
		if !a.exists(sp, rel) {
			return nil, fmt.Errorf("%q not found in space %q", rel, sp.Name)
		}
		if a.isLiveWindow() && !a.flush() {
			return nil, errors.New(i18n.T("请先保存文档并解决文件冲突"))
		}
		dst := sp
		if strings.TrimSpace(p.ToSpace) != "" {
			dst, err = a.resolveTargetSpace(p.ToSpace)
			if err != nil {
				return nil, err
			}
		}
		toDir := strings.Trim(strings.ReplaceAll(strings.TrimSpace(p.ToDir), `\`, "/"), "/")
		if toDir == "." {
			toDir = ""
		}
		if toDir != "" {
			fullDir, dErr := vault.Resolve(dst.Root, toDir)
			if dErr != nil {
				return nil, dErr
			}
			if mkErr := os.MkdirAll(fullDir, 0o755); mkErr != nil {
				return nil, mkErr
			}
		}
		to, copied, mvErr := a.moveWithResources(sp, rel, dst, toDir)
		if mvErr != nil {
			return nil, mvErr
		}
		if copied {
			if origFull, rErr := resolveFile(sp, rel); rErr == nil {
				_ = a.trashItem(origFull)
			}
		}
		if sp.ID == dst.ID {
			if toDir != "" {
				sp.SetExpanded(toDir, true)
			}
			a.moved(sp, rel, to)
		} else {
			_ = history.Forget(sp.Root, rel)
			a.state.MovedAcross(sp.ID, rel, dst.ID, to)
			a.followCopied(sp.ID, rel, dst.ID, to)
			a.followOpen(sp.ID, rel, dst.ID, to)
			a.followSplitPaths(sp.ID, rel, dst.ID, to)
			if toDir != "" {
				dst.SetExpanded(toDir, true)
			}
			delete(a.pageTrees, sp.ID)
			delete(a.pageTrees, dst.ID)
			a.invalidateConflictsCache()
			_ = a.saveStateStrict()
			a.refreshTree()
		}
		return map[string]any{
			"fromSpace": sp.Name,
			"from":      rel,
			"toSpace":   dst.Name,
			"to":        to,
		}, nil

	case "pin":
		rel, rErr := normalizeDocRel(sp, targetPath)
		if rErr != nil {
			return nil, rErr
		}
		if !a.exists(sp, rel) {
			return nil, fmt.Errorf("%q not found in space %q", rel, sp.Name)
		}
		if a.state.IsFavorite(sp.ID, rel) {
			a.state.ToggleFavorite(sp.ID, rel)
		}
		sp.Pin(rel, len(sp.Pins))
		if err := a.saveStateStrict(); err != nil {
			return nil, err
		}
		return map[string]any{"space": sp.Name, "path": rel, "pinned": true}, nil

	case "unpin":
		rel, rErr := normalizeDocRel(sp, targetPath)
		if rErr != nil {
			return nil, rErr
		}
		if !a.exists(sp, rel) {
			return nil, fmt.Errorf("%q not found in space %q", rel, sp.Name)
		}
		sp.Unpin(rel)
		if err := a.saveStateStrict(); err != nil {
			return nil, err
		}
		return map[string]any{"space": sp.Name, "path": rel, "pinned": false}, nil

	case "favorite":
		rel, rErr := normalizeDocRel(sp, targetPath)
		if rErr != nil {
			return nil, rErr
		}
		full, fErr := resolveFile(sp, rel)
		if fErr != nil {
			return nil, fErr
		}
		info, stErr := os.Stat(full)
		if stErr != nil {
			return nil, fmt.Errorf("document %q not found in space %q", rel, sp.Name)
		}
		if info.IsDir() {
			return nil, errors.New("cannot add a folder to Favorites (use pin instead)")
		}
		if !a.state.IsFavorite(sp.ID, rel) {
			a.state.ToggleFavorite(sp.ID, rel)
		}
		sp.Unpin(rel)
		if err := a.saveStateStrict(); err != nil {
			return nil, err
		}
		return map[string]any{"space": sp.Name, "path": rel, "favorite": true}, nil

	case "unfavorite":
		rel, rErr := normalizeDocRel(sp, targetPath)
		if rErr != nil {
			return nil, rErr
		}
		if a.state.IsFavorite(sp.ID, rel) {
			a.state.ToggleFavorite(sp.ID, rel)
		}
		if err := a.saveStateStrict(); err != nil {
			return nil, err
		}
		return map[string]any{"space": sp.Name, "path": rel, "favorite": false}, nil

	default:
		return nil, fmt.Errorf("unknown organize op %q (use open, mkdir, rename, move, pin, unpin, favorite, or unfavorite)", p.Op)
	}
}

func (a *App) ctlDocDelete(raw []byte) (any, error) {
	var p struct {
		Space   string `json:"space"`
		Op      string `json:"op"`
		Path    string `json:"path"`
		TrashID string `json:"trashID"`
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	op := strings.ToLower(strings.TrimSpace(p.Op))
	if op == "" {
		op = "trash"
	}
	switch op {
	case "list_trash":
		items, err := a.bin.List(time.Now())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"count": len(items),
			"items": items,
		}, nil

	case "restore":
		items, err := a.bin.List(time.Now())
		if err != nil {
			return nil, err
		}
		var chosen *trashbin.Item
		wantID := strings.TrimSpace(p.TrashID)
		wantPath := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(p.Path), `\`, "/"), "/")
		for i := range items {
			it := &items[i]
			if wantID != "" && it.ID == wantID {
				chosen = it
				break
			}
			if wantID == "" && wantPath != "" && (it.Path == wantPath || strings.EqualFold(it.Name(), wantPath)) {
				chosen = it
				break
			}
		}
		if chosen == nil {
			return nil, errors.New("matching deleted item not found in ArcMD Recently Deleted bin")
		}
		sp := a.state.Get(chosen.Space)
		if sp == nil {
			return nil, fmt.Errorf("original Space %q is no longer registered in ArcMD", chosen.SpaceName)
		}
		restoredRel, err := a.bin.Restore(*chosen, sp.Root)
		if err != nil {
			return nil, err
		}
		a.noteExternalWrite(sp, restoredRel)
		if a.library.open {
			a.loadLibrary()
		}
		return map[string]any{
			"space":        sp.Name,
			"restoredPath": restoredRel,
			"trashID":      chosen.ID,
		}, nil

	case "trash":
		sp, err := a.resolveTargetSpace(p.Space)
		if err != nil {
			return nil, err
		}
		rel, err := normalizeDocRel(sp, p.Path)
		if err != nil {
			return nil, err
		}
		if sp.IsExternal() {
			if _, ok := sp.ExternalFiles[rel]; !ok {
				return nil, fmt.Errorf("document %q not found in External space", rel)
			}
			a.releasePDFHandles(sp, rel)
			a.forgetExternal(sp, rel)
			return map[string]any{"space": sp.Name, "path": rel, "removedExternalRef": true}, nil
		}
		return a.safeTrashInSpace(sp, rel)

	default:
		return nil, fmt.Errorf("unknown delete op %q (use trash, list_trash, or restore)", p.Op)
	}
}

func (a *App) safeTrashInSpace(sp *space.Space, rel string) (map[string]any, error) {
	full, err := resolveFile(sp, rel)
	if err != nil {
		return nil, err
	}
	if _, stErr := os.Lstat(full); stErr != nil {
		return nil, stErr
	}
	if a.isLiveWindow() && !a.flush() {
		return nil, errors.New(i18n.T("请先保存文档并解决文件冲突"))
	}
	kept, backedUp := a.keepDeleted(sp.ID, sp.Name, sp.Root, rel)
	a.releasePDFHandles(sp, rel)
	if err := a.trashItem(full); err != nil {
		a.reopenResidentPDFs(sp, rel)
		if backedUp {
			_ = a.bin.Forget(kept)
		}
		return nil, err
	}
	if a.dbPeek.open && ((a.dbPeek.doc != nil && a.dbPeek.doc.space == sp && within(a.dbPeek.doc.rel, rel)) ||
		(a.dbPeek.dbDoc != nil && a.dbPeek.dbDoc.space == sp && (within(a.dbPeek.dbDoc.rel, rel) || within(a.dbPeek.recordID, rel)))) {
		a.closeRecordPeek()
	}
	for _, d := range a.residentDocuments() {
		if d.space == sp && within(d.rel, rel) {
			a.replaceReferences(d, nil)
		}
	}
	a.forgetPicks(sp.ID, rel)
	a.forgetCopied(sp.ID, rel)
	a.state.Forget(sp.ID, rel)
	_ = a.saveStateStrict()
	a.noteExternalWrite(sp, rel)
	if a.library.open {
		a.loadLibrary()
	}
	out := map[string]any{
		"space":    sp.Name,
		"trashed":  rel,
		"backedUp": backedUp,
	}
	if backedUp {
		out["trashID"] = kept.ID
	}
	return out, nil
}
