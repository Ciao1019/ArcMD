package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/dbview"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/history"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
)

type declarativeBoardElement struct {
	ID             string       `json:"id"`
	Type           string       `json:"type"`
	Text           string       `json:"text"`
	Name           string       `json:"name"`
	X              *float32     `json:"x"`
	Y              *float32     `json:"y"`
	Width          float32      `json:"width"`
	Height         float32      `json:"height"`
	Color          string       `json:"color"`
	StrokeColor    string       `json:"strokeColor"`
	FillColor      string       `json:"fillColor"`
	FillStyle      string       `json:"fillStyle"`
	StrokeStyle    string       `json:"strokeStyle"`
	StrokeWidth    float32      `json:"strokeWidth"`
	Roughness      *int         `json:"roughness"`
	From           string       `json:"from"`
	To             string       `json:"to"`
	Points         [][2]float32 `json:"points"`
	Smooth         *bool        `json:"smooth"`
	Elbowed        bool         `json:"elbowed"`
	StartArrowhead string       `json:"startArrowhead"`
	EndArrowhead   string       `json:"endArrowhead"`
	Link           string       `json:"link"`
	FrameID        string       `json:"frameId"`
}

func (a *App) loadSceneForControl(sp *space.Space, rel string) (*whiteboard.Scene, error) {
	if res := a.residentDocument(sp.ID, rel); res != nil && res.isWhiteboard() && res.loadErr == nil {
		return res.board.SceneSnapshot(), nil
	}
	raw, err := readFile(sp, rel)
	if err != nil {
		return nil, err
	}
	return whiteboard.Decode(raw)
}

func (a *App) ctlWhiteboardWrite(raw []byte) (any, error) {
	var p struct {
		Space        string                    `json:"space"`
		Path         string                    `json:"path"`
		Mermaid      string                    `json:"mermaid"`
		Elements     []declarativeBoardElement `json:"elements"`
		Append       bool                      `json:"append"`
		EmbedInDoc   string                    `json:"embedInDoc"`
		EmbedHeading string                    `json:"embedHeading"`
		Caption      string                    `json:"caption"`
		Open         bool                      `json:"open"`
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	sp, err := a.resolveTargetSpace(p.Space)
	if err != nil {
		return nil, err
	}
	if sp.IsExternal() {
		return nil, errors.New("cannot write whiteboards inside External space")
	}

	embedDocRel := ""
	if strings.TrimSpace(p.EmbedInDoc) != "" {
		embedDocRel, err = normalizeDocRel(sp, p.EmbedInDoc)
		if err != nil {
			return nil, err
		}
	}

	boardRel := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(p.Path), `\`, "/"), "/")
	if boardRel == "" {
		stem := "diagram-" + space.NewID()[:6]
		attachDir := attachmentFolder(sp)
		if embedDocRel != "" {
			if attachDir != "" {
				boardRel = path.Join(attachDir, stem+vault.ExtWhiteboard)
			} else {
				docDir := cleanRelDir(path.Dir(embedDocRel))
				boardRel = path.Join(docDir, assetsDir, stem+vault.ExtWhiteboard)
			}
		} else {
			boardRel = stem + vault.ExtWhiteboard
		}
	} else if !vault.IsWhiteboard(boardRel) {
		boardRel = strings.TrimSuffix(boardRel, ".svg") + vault.ExtWhiteboard
	}
	boardRel = path.Clean(boardRel)
	if _, err := vault.Resolve(sp.Root, boardRel); err != nil {
		return nil, err
	}

	var scene *whiteboard.Scene
	if p.Append {
		if existing, loadErr := a.loadSceneForControl(sp, boardRel); loadErr == nil {
			scene = existing.Clone()
		}
	}
	if scene == nil {
		scene = whiteboard.NewScene()
	}

	if mSrc := strings.TrimSpace(p.Mermaid); mSrc != "" {
		cx, cy := float32(420), float32(260)
		if len(scene.Elements) > 0 {
			b := whiteboard.SceneBounds(scene)
			cx = b.X + b.W*0.5
			cy = b.Y + b.H + 220
		}
		elems, mErr := editor.ConvertMermaidToWhiteboard(mSrc, cx, cy)
		if mErr != nil {
			return nil, fmt.Errorf("mermaid to whiteboard: %w", mErr)
		}
		scene.Elements = append(scene.Elements, elems...)
	}

	if len(p.Elements) > 0 {
		built, bErr := buildDeclarativeElements(scene, p.Elements)
		if bErr != nil {
			return nil, bErr
		}
		// Replace matching IDs on append, or append new ones.
		for _, el := range built {
			replaced := false
			for i, cur := range scene.Elements {
				if cur != nil && cur.ID == el.ID {
					scene.Elements[i] = el
					replaced = true
					break
				}
			}
			if !replaced {
				scene.Elements = append(scene.Elements, el)
			}
		}
	}
	whiteboard.UpdateBindings(scene)

	encoded, err := encodeWhiteboardFile(boardRel, scene)
	if err != nil {
		return nil, err
	}

	if res := a.residentDocument(sp.ID, boardRel); res != nil && res.isWhiteboard() && res.loadErr == nil {
		res.board.ReplaceScene(scene.Clone(), true)
		if err := res.write(true); err != nil {
			return nil, err
		}
	} else {
		prevDisk, _ := readFile(sp, boardRel)
		if prevDisk != "" {
			_ = history.Record(sp.Root, boardRel, prevDisk, time.Now().Add(-time.Second), true)
		}
		if err := vault.Write(sp.Root, boardRel, encoded); err != nil {
			return nil, err
		}
		_ = history.Record(sp.Root, boardRel, encoded, time.Now(), false)
	}
	clear(a.wbCache)
	a.wbOrder = nil
	a.noteExternalWrite(sp, boardRel)

	embedMarkdown := ""
	if embedDocRel != "" {
		relURL := relativePathBetweenDocs(embedDocRel, boardRel)
		caption := strings.TrimSpace(p.Caption)
		if caption == "" {
			caption = i18n.T("手绘白板")
		}
		embedMarkdown = fmt.Sprintf("![%s](%s)", caption, relURL)
		curDoc, _, rErr := a.readMarkdownForControl(sp, embedDocRel)
		if rErr != nil {
			return nil, rErr
		}
		if !strings.Contains(curDoc, relURL) {
			nextDoc, iErr := insertMarkdownChunk(curDoc, embedMarkdown, p.EmbedHeading, false)
			if iErr != nil {
				return nil, iErr
			}
			if err := a.commitMarkdownForControl(sp, embedDocRel, nextDoc); err != nil {
				return nil, err
			}
		}
	}

	if p.Open {
		openTarget := boardRel
		if embedDocRel != "" {
			openTarget = embedDocRel
		}
		a.finishDocPostActions(sp, openTarget, false, false, true)
	}

	return map[string]any{
		"space":        sp.Name,
		"path":         boardRel,
		"elementCount": len(scene.Elements),
		"embeddedIn":   embedDocRel,
		"embedSyntax":  embedMarkdown,
	}, nil
}

func buildDeclarativeElements(existing *whiteboard.Scene, specs []declarativeBoardElement) ([]*whiteboard.Element, error) {
	byID := map[string]*whiteboard.Element{}
	for _, el := range existing.Elements {
		if el != nil && el.ID != "" {
			byID[el.ID] = el
		}
	}
	startY := float32(60)
	if len(existing.Elements) > 0 {
		b := whiteboard.SceneBounds(existing)
		if !b.Empty() {
			startY = b.Y + b.H + 80
		}
	}

	var out []*whiteboard.Element
	var pendingConnectors []struct {
		el   *whiteboard.Element
		spec declarativeBoardElement
	}
	autoIdx := 0

	for _, s := range specs {
		kind := normalizeBoardKind(s.Type)
		isConnector := kind == whiteboard.KindArrow || kind == whiteboard.KindLine

		w, h := s.Width, s.Height
		if !isConnector && (w <= 0 || h <= 0) {
			aw, ah := estimateShapeSize(kind, s.Text)
			if w <= 0 {
				w = aw
			}
			if h <= 0 {
				h = ah
			}
		}
		var x, y float32
		if s.X != nil {
			x = *s.X
		} else if !isConnector {
			col := autoIdx % 3
			x = 60 + float32(col)*250
		}
		if s.Y != nil {
			y = *s.Y
		} else if !isConnector {
			row := autoIdx / 3
			y = startY + float32(row)*150
		}
		if !isConnector && s.X == nil && s.Y == nil {
			autoIdx++
		}

		el := whiteboard.NewElement(kind, x, y, w, h)
		if id := strings.TrimSpace(s.ID); id != "" {
			el.ID = id
		}
		if s.Text != "" {
			el.Text = s.Text
		}
		if s.Name != "" {
			el.Name = s.Name
		} else if kind == whiteboard.KindFrame && s.Text != "" {
			el.Name = s.Text
			el.Text = ""
		}
		col := strings.TrimSpace(s.StrokeColor)
		if col == "" {
			col = strings.TrimSpace(s.Color)
		}
		if col != "" {
			el.StrokeColor = col
			if !isConnector && kind != whiteboard.KindText && kind != whiteboard.KindFrame && kind != whiteboard.KindActor && s.FillColor == "" {
				el.FillColor = col
			}
		}
		if s.FillColor != "" {
			if strings.EqualFold(s.FillColor, "none") {
				el.FillColor = ""
				el.FillStyle = whiteboard.FillNone
			} else {
				el.FillColor = strings.TrimSpace(s.FillColor)
			}
		}
		switch strings.ToLower(strings.TrimSpace(s.FillStyle)) {
		case "":
		case "none":
			el.FillStyle = whiteboard.FillNone
		case "hachure":
			el.FillStyle = whiteboard.FillHachure
		case "cross-hatch", "crosshatch":
			el.FillStyle = whiteboard.FillCrossHatch
		case "solid":
			el.FillStyle = whiteboard.FillSolid
		}
		switch strings.ToLower(strings.TrimSpace(s.StrokeStyle)) {
		case "solid":
			el.StrokeStyle = whiteboard.StrokeSolid
		case "dashed":
			el.StrokeStyle = whiteboard.StrokeDashed
		case "dotted":
			el.StrokeStyle = whiteboard.StrokeDotted
		}
		if s.StrokeWidth > 0 {
			el.StrokeWidth = s.StrokeWidth
		}
		if s.Roughness != nil {
			el.Roughness = max(0, min(2, *s.Roughness))
		}
		if s.Link != "" {
			el.Link = s.Link
		}
		if s.FrameID != "" {
			el.FrameID = s.FrameID
		}
		if isConnector {
			if s.Smooth != nil {
				el.Smooth = *s.Smooth
			}
			el.Elbowed = s.Elbowed
			if s.StartArrowhead != "" {
				el.StartArrowhead = parseArrowhead(s.StartArrowhead)
			}
			if s.EndArrowhead != "" {
				el.EndArrowhead = parseArrowhead(s.EndArrowhead)
			}
			if len(s.Points) >= 2 {
				el.Points = slices.Clone(s.Points)
				last := s.Points[len(s.Points)-1]
				el.W, el.H = last[0], last[1]
			}
			pendingConnectors = append(pendingConnectors, struct {
				el   *whiteboard.Element
				spec declarativeBoardElement
			}{el: el, spec: s})
		}
		byID[el.ID] = el
		out = append(out, el)
	}

	for _, item := range pendingConnectors {
		el := item.el
		s := item.spec
		var fromEl, toEl *whiteboard.Element
		if s.From != "" {
			fromEl = byID[s.From]
			if fromEl == nil {
				return nil, fmt.Errorf("whiteboard connector references unknown 'from' element %q", s.From)
			}
			el.StartBinding = &whiteboard.Binding{ElementID: fromEl.ID, Gap: 6}
		}
		if s.To != "" {
			toEl = byID[s.To]
			if toEl == nil {
				return nil, fmt.Errorf("whiteboard connector references unknown 'to' element %q", s.To)
			}
			el.EndBinding = &whiteboard.Binding{ElementID: toEl.ID, Gap: 6}
		}
		if fromEl != nil && toEl != nil && len(s.Points) < 2 {
			fcx, fcy := whiteboard.ElementBounds(fromEl).Center()
			tcx, tcy := whiteboard.ElementBounds(toEl).Center()
			el.X, el.Y = fcx, fcy
			dx, dy := tcx-fcx, tcy-fcy
			el.W, el.H = dx, dy
			el.Points = [][2]float32{{0, 0}, {dx, dy}}
		}
	}
	return out, nil
}

func normalizeBoardKind(raw string) whiteboard.ElementKind {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "rect", "rectangle", "box", "":
		return whiteboard.KindRectangle
	case "diamond", "decision":
		return whiteboard.KindDiamond
	case "ellipse", "circle", "oval":
		return whiteboard.KindEllipse
	case "stadium", "pill":
		return whiteboard.KindStadium
	case "cylinder", "db", "database":
		return whiteboard.KindCylinder
	case "hexagon", "hex":
		return whiteboard.KindHexagon
	case "parallelogram":
		return whiteboard.KindParallelogram
	case "triangle":
		return whiteboard.KindTriangle
	case "cloud":
		return whiteboard.KindCloud
	case "star":
		return whiteboard.KindStar
	case "callout":
		return whiteboard.KindCallout
	case "document", "doc":
		return whiteboard.KindDocument
	case "actor", "person":
		return whiteboard.KindActor
	case "note", "sticky":
		return whiteboard.KindNote
	case "frame", "group":
		return whiteboard.KindFrame
	case "text", "label":
		return whiteboard.KindText
	case "arrow", "connector":
		return whiteboard.KindArrow
	case "line":
		return whiteboard.KindLine
	default:
		return whiteboard.ElementKind(strings.ToLower(strings.TrimSpace(raw)))
	}
}

func parseArrowhead(raw string) whiteboard.Arrowhead {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "none":
		return whiteboard.ArrowNone
	case "triangle", "filled":
		return whiteboard.ArrowTriangle
	case "bar":
		return whiteboard.ArrowBar
	case "dot", "circle":
		return whiteboard.ArrowDot
	default:
		return whiteboard.ArrowOpen
	}
}

func estimateShapeSize(kind whiteboard.ElementKind, text string) (w, h float32) {
	if kind == whiteboard.KindFrame {
		return 480, 300
	}
	if kind == whiteboard.KindNote {
		return 190, 150
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	maxCols := 6
	for _, l := range lines {
		cols := 0
		for _, r := range l {
			if utf8.RuneLen(r) > 1 {
				cols += 2
			} else {
				cols++
			}
		}
		if cols > maxCols {
			maxCols = cols
		}
	}
	w = max(148, float32(maxCols)*10.5+44)
	h = max(72, float32(max(1, len(lines)))*26+36)
	if kind == whiteboard.KindDiamond {
		w = max(160, w*1.25)
		h = max(96, h*1.25)
	}
	return w, h
}

func cleanRelDir(dir string) string {
	dir = path.Clean(strings.ReplaceAll(dir, `\`, "/"))
	if dir == "." || dir == "/" {
		return ""
	}
	return strings.TrimPrefix(dir, "/")
}

func relativePathBetweenDocs(fromDocRel, targetRel string) string {
	fromDir := cleanRelDir(path.Dir(fromDocRel))
	targetClean := strings.TrimPrefix(path.Clean(strings.ReplaceAll(targetRel, `\`, "/")), "/")
	if fromDir == "" {
		return targetClean
	}
	fromSegs := strings.Split(fromDir, "/")
	toSegs := strings.Split(targetClean, "/")
	common := 0
	for common < len(fromSegs) && common < len(toSegs)-1 && fromSegs[common] == toSegs[common] {
		common++
	}
	var relSegs []string
	for i := common; i < len(fromSegs); i++ {
		relSegs = append(relSegs, "..")
	}
	relSegs = append(relSegs, toSegs[common:]...)
	return strings.Join(relSegs, "/")
}

// --- Database Operations ---

type dbPropertyInput struct {
	ID          string                   `json:"id"`
	Key         string                   `json:"key"`
	Name        string                   `json:"name"`
	Type        string                   `json:"type"`
	Options     []database.SelectOption  `json:"options"`
	Number      *database.NumberConfig   `json:"number"`
	Date        *database.DateConfig     `json:"date"`
	Relation    *database.RelationConfig `json:"relation"`
	Rollup      *database.RollupConfig   `json:"rollup"`
	Formula     *database.FormulaConfig  `json:"formula"`
	UniqueID    *database.UniqueIDConfig `json:"uniqueID"`
	Description string                   `json:"description"`
	Wrap        bool                     `json:"wrap"`
	Width       float32                  `json:"width"`
}

type dbSortInput struct {
	Property string `json:"property"`
	Desc     bool   `json:"desc"`
}

type dbChartInput struct {
	Kind       string `json:"kind"`
	XProp      string `json:"xProp"`
	YMode      string `json:"yMode"`
	YProp      string `json:"yProp"`
	YAgg       string `json:"yAgg"`
	StackProp  string `json:"stackProp"`
	ShowValues *bool  `json:"showValues"`
	ShowLegend *bool  `json:"showLegend"`
	Smooth     *bool  `json:"smooth"`
}

type dbViewInput struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Type      string        `json:"type"`
	Icon      string        `json:"icon"`
	GroupBy   string        `json:"groupBy"`
	Sorts     []dbSortInput `json:"sorts"`
	StartProp string        `json:"startProp"`
	EndProp   string        `json:"endProp"`
	DateProp  string        `json:"dateProp"`
	Chart     *dbChartInput `json:"chart"`
}

type dbRecordInput struct {
	Title  string         `json:"title"`
	Fields map[string]any `json:"fields"`
	Body   string         `json:"body"`
}

func normalizeDBRel(sp *space.Space, rawRel, fallbackTitle string) (string, error) {
	rel := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(rawRel), `\`, "/"), "/")
	if rel == "" {
		t := strings.TrimSpace(fallbackTitle)
		if t == "" {
			t = untitledDatabase
		}
		rel = t + vault.ExtDatabase
	} else if !vault.IsDatabase(rel) {
		rel = strings.TrimSuffix(rel, ".md") + vault.ExtDatabase
	}
	clean := path.Clean(rel)
	if !sp.IsExternal() {
		if _, err := vault.Resolve(sp.Root, clean); err != nil {
			return "", err
		}
	}
	return clean, nil
}

func (a *App) attachRelationHost(sp *space.Space, st *database.Store) {
	if sp == nil || sp.IsExternal() || st == nil {
		return
	}
	if !st.HasRelationResolver() {
		host := &appRelationHost{app: a, sp: sp, src: st}
		st.SetRelationResolver(host)
		st.SetRelationSyncer(host)
	}
}

func (a *App) openDBStoreForControl(sp *space.Space, rel string) (st *database.Store, liveDoc *document, err error) {
	if res := a.residentDocument(sp.ID, rel); res != nil && res.isDatabase() && res.loadErr == nil {
		a.attachRelationHost(sp, res.dbView.Store)
		_, _ = res.dbView.Store.Refresh(a.liveDocsForSpace(sp))
		res.dbView.Store.InvalidateComputed()
		return res.dbView.Store, res, nil
	}
	spaceRoot, dbRel := databaseSpaceRoot(sp, rel)
	st, err = database.Open(spaceRoot, dbRel, a.liveDocsForSpace(sp))
	if err != nil {
		return nil, nil, err
	}
	a.attachRelationHost(sp, st)
	st.InvalidateComputed()
	return st, nil, nil
}

func (a *App) saveDBStoreForControl(sp *space.Space, rel string, st *database.Store, liveDoc *document) error {
	if liveDoc != nil && liveDoc.isDatabase() {
		liveDoc.dbDirtySnapshot = true
		if err := liveDoc.writeDatabase(true); err != nil {
			return err
		}
		a.noteExternalWrite(sp, rel)
		return nil
	}
	md, err := st.EncodeMarkdown(time.Now())
	if err != nil {
		return err
	}
	prevDisk, _ := readFile(sp, rel)
	if !sp.IsExternal() && prevDisk != "" {
		_ = history.Record(sp.Root, rel, prevDisk, time.Now().Add(-time.Second), true)
	}
	if err := writeFile(sp, rel, md); err != nil {
		return err
	}
	if !sp.IsExternal() {
		_ = history.Record(sp.Root, rel, md, time.Now(), false)
	}
	a.noteExternalWrite(sp, rel)
	return nil
}

func (a *App) ctlDatabaseSchema(raw []byte) (any, error) {
	var p struct {
		Space      string            `json:"space"`
		Op         string            `json:"op"`
		Path       string            `json:"path"`
		Title      string            `json:"title"`
		Icon       string            `json:"icon"`
		Cover      string            `json:"cover"`
		SourceType string            `json:"sourceType"`
		SourceDir  string            `json:"sourceDir"`
		Properties []dbPropertyInput `json:"properties"`
		Property   *dbPropertyInput  `json:"property"`
		Views      []dbViewInput     `json:"views"`
		View       *dbViewInput      `json:"view"`
		Records    []dbRecordInput   `json:"records"`
		Overwrite  bool              `json:"overwrite"`
		EmbedInDoc string            `json:"embedInDoc"`
		Height     float32           `json:"height"`
		Open       bool              `json:"open"`
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	sp, err := a.resolveTargetSpace(p.Space)
	if err != nil {
		return nil, err
	}
	op := strings.ToLower(strings.TrimSpace(p.Op))
	if op == "" {
		op = "inspect"
	}

	if op == "create" {
		if sp.IsExternal() {
			return nil, errors.New("cannot create databases inside External space")
		}
		dbRel, rErr := normalizeDBRel(sp, p.Path, p.Title)
		if rErr != nil {
			return nil, rErr
		}
		if !p.Overwrite && a.exists(sp, dbRel) {
			return nil, fmt.Errorf("database %q already exists (set overwrite=true to replace it)", dbRel)
		}
		title := strings.TrimSpace(p.Title)
		if title == "" {
			title = vault.Title(dbRel)
		}
		srcType := database.SourceType(strings.ToLower(strings.TrimSpace(p.SourceType)))
		if srcType == "" {
			srcType = database.SourceFolder
		}
		if !database.ValidSourceType(srcType) {
			return nil, fmt.Errorf("invalid sourceType %q (use folder, inline, or space)", p.SourceType)
		}
		db := database.NewDefault(title, database.SourceConfig{
			Type: srcType,
			Dir:  strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(p.SourceDir), `\`, "/"), "/"),
		})
		db.Icon = strings.TrimSpace(p.Icon)
		db.Cover = strings.TrimSpace(p.Cover)

		if len(p.Properties) > 0 {
			titleProp := db.TitleProperty()
			var custom []database.Property
			if titleProp != nil {
				custom = append(custom, titleProp.Clone())
			}
			for i, inProp := range p.Properties {
				built, bErr := buildDBProperty(custom, inProp, i+1)
				if bErr != nil {
					return nil, bErr
				}
				if built.Type == database.PropTitle {
					custom[0] = built
					continue
				}
				custom = append(custom, built)
			}
			db.Properties = custom
			// Sync default view visible columns.
			if len(db.Views) > 0 {
				vps := make([]database.ViewProperty, len(db.Properties))
				for i, pr := range db.Properties {
					vps[i] = database.ViewProperty{PropertyID: pr.ID, Visible: true}
				}
				db.Views[0].Properties = vps
			}
		}

		st := database.NewStore(sp.Root, dbRel, db)
		a.attachRelationHost(sp, st)
		liveDoc := a.residentDocument(sp.ID, dbRel)
		if srcType == database.SourceFolder {
			if srcDir := st.ResolveSourceDir(); srcDir != "" {
				if fullDir, dErr := vault.Resolve(sp.Root, srcDir); dErr == nil {
					_ = os.MkdirAll(fullDir, 0o755)
				}
			}
		}
		_, _ = st.Refresh(a.liveDocsForSpace(sp))

		if len(p.Views) > 0 {
			for idx, vin := range p.Views {
				vcfg, vErr := buildDBViewConfig(st.Database(), vin)
				if vErr != nil {
					return nil, vErr
				}
				// Replace default table view when the first custom view is also a table.
				curDB := st.Database()
				if idx == 0 && len(curDB.Views) == 1 && vcfg.Type == database.ViewTable {
					vcfg.ID = curDB.Views[0].ID
					vcfg.Primary = true
					if err := st.UpdateView(vcfg); err != nil {
						return nil, err
					}
				} else {
					if err := st.AddView(vcfg, idx == 0); err != nil {
						return nil, err
					}
				}
			}
		}

		for _, recIn := range p.Records {
			vals, vErr := coerceRecordFields(st, recIn.Fields)
			if vErr != nil {
				return nil, vErr
			}
			if _, cErr := a.createDBRecordForControl(sp, liveDoc, st, recIn.Title, vals, recIn.Body); cErr != nil {
				return nil, cErr
			}
		}

		if liveDoc != nil {
			if a.dbPeek.open && a.dbPeek.dbDoc == liveDoc {
				a.closeRecordPeek()
			}
			liveDoc.loadErr = nil
			liveDoc.dbView = dbview.NewState(st)
		}
		if err := a.saveDBStoreForControl(sp, dbRel, st, liveDoc); err != nil {
			return nil, err
		}
		if p.Open {
			a.finishDocPostActions(sp, dbRel, false, false, true)
		}
		finalDB := st.Database()
		return map[string]any{
			"space":       sp.Name,
			"path":        dbRel,
			"title":       finalDB.Title,
			"properties":  finalDB.Properties,
			"views":       finalDB.Views,
			"recordCount": len(st.Records()),
		}, nil
	}

	dbRel, err := normalizeDBRel(sp, p.Path, p.Title)
	if err != nil {
		return nil, err
	}
	st, liveDoc, err := a.openDBStoreForControl(sp, dbRel)
	if err != nil {
		return nil, err
	}

	switch op {
	case "inspect":
		db := st.Database()
		return map[string]any{
			"space":       sp.Name,
			"path":        dbRel,
			"title":       db.Title,
			"icon":        db.Icon,
			"cover":       db.Cover,
			"source":      db.Source,
			"properties":  db.Properties,
			"views":       db.Views,
			"activeView":  db.ActiveView,
			"recordCount": len(st.Records()),
		}, nil

	case "add_property":
		if p.Property == nil {
			return nil, errors.New("property object is required for add_property")
		}
		curDB := st.Database()
		built, bErr := buildDBProperty(curDB.Properties, *p.Property, len(curDB.Properties)+1)
		if bErr != nil {
			return nil, bErr
		}
		if err := st.AddProperty(built); err != nil {
			return nil, err
		}
		if err := a.saveDBStoreForControl(sp, dbRel, st, liveDoc); err != nil {
			return nil, err
		}
		return map[string]any{"space": sp.Name, "path": dbRel, "property": built}, nil

	case "update_property":
		if p.Property == nil {
			return nil, errors.New("property object is required for update_property")
		}
		curDB := st.Database()
		existing, ok := findDBProperty(curDB, firstNonEmpty(p.Property.ID, p.Property.Key, p.Property.Name))
		if !ok {
			return nil, errors.New("target property not found")
		}
		merged, bErr := mergeDBProperty(existing, *p.Property)
		if bErr != nil {
			return nil, bErr
		}
		affected, err := st.UpdateProperty(merged)
		if err != nil {
			return nil, err
		}
		a.flushDatabaseEditsToDocs(sp, liveDoc, st, affected)
		if err := a.saveDBStoreForControl(sp, dbRel, st, liveDoc); err != nil {
			return nil, err
		}
		return map[string]any{"space": sp.Name, "path": dbRel, "property": merged}, nil

	case "delete_property":
		if p.Property == nil {
			return nil, errors.New("property object (with id, key, or name) is required for delete_property")
		}
		curDB := st.Database()
		existing, ok := findDBProperty(curDB, firstNonEmpty(p.Property.ID, p.Property.Key, p.Property.Name))
		if !ok {
			return nil, errors.New("target property not found")
		}
		affected, err := st.DeleteProperty(existing.ID)
		if err != nil {
			return nil, err
		}
		a.flushDatabaseEditsToDocs(sp, liveDoc, st, affected)
		if err := a.saveDBStoreForControl(sp, dbRel, st, liveDoc); err != nil {
			return nil, err
		}
		return map[string]any{"space": sp.Name, "path": dbRel, "deletedPropertyID": existing.ID}, nil

	case "add_view":
		if p.View == nil {
			return nil, errors.New("view object is required for add_view")
		}
		vcfg, vErr := buildDBViewConfig(st.Database(), *p.View)
		if vErr != nil {
			return nil, vErr
		}
		if err := st.AddView(vcfg, true); err != nil {
			return nil, err
		}
		if err := a.saveDBStoreForControl(sp, dbRel, st, liveDoc); err != nil {
			return nil, err
		}
		return map[string]any{"space": sp.Name, "path": dbRel, "view": vcfg}, nil

	case "update_view":
		if p.View == nil {
			return nil, errors.New("view object is required for update_view")
		}
		curDB := st.Database()
		existingView, ok := findDBView(curDB, firstNonEmpty(p.View.ID, p.View.Name))
		if !ok {
			return nil, errors.New("target view not found")
		}
		vcfg, vErr := mergeDBViewConfig(curDB, existingView, *p.View)
		if vErr != nil {
			return nil, vErr
		}
		if err := st.UpdateView(vcfg); err != nil {
			return nil, err
		}
		if err := a.saveDBStoreForControl(sp, dbRel, st, liveDoc); err != nil {
			return nil, err
		}
		return map[string]any{"space": sp.Name, "path": dbRel, "view": vcfg}, nil

	case "delete_view":
		if p.View == nil {
			return nil, errors.New("view object (id or name) is required for delete_view")
		}
		curDB := st.Database()
		existingView, ok := findDBView(curDB, firstNonEmpty(p.View.ID, p.View.Name))
		if !ok {
			return nil, errors.New("target view not found")
		}
		if err := st.DeleteView(existingView.ID); err != nil {
			return nil, err
		}
		if err := a.saveDBStoreForControl(sp, dbRel, st, liveDoc); err != nil {
			return nil, err
		}
		return map[string]any{"space": sp.Name, "path": dbRel, "deletedViewID": existingView.ID}, nil

	case "embed_view":
		if strings.TrimSpace(p.EmbedInDoc) == "" {
			return nil, errors.New("embedInDoc is required for embed_view")
		}
		docRel, dErr := normalizeDocRel(sp, p.EmbedInDoc)
		if dErr != nil {
			return nil, dErr
		}
		curDB := st.Database()
		viewID := curDB.ActiveView
		if p.View != nil && firstNonEmpty(p.View.ID, p.View.Name) != "" {
			v, ok := findDBView(curDB, firstNonEmpty(p.View.ID, p.View.Name))
			if !ok {
				return nil, errors.New("requested view not found in database")
			}
			viewID = v.ID
		}
		docDir := cleanRelDir(path.Dir(docRel))
		snapMD := database.RenderGFMSnapshot(docDir, curDB, st.Query(viewID, database.QueryOptions{}))
		relTarget := relativePathBetweenDocs(docRel, dbRel)
		spec := editor.DatabaseEmbedSpec{
			Target: relTarget,
			ViewID: viewID,
			Height: p.Height,
		}
		blockMD := editor.FormatDatabaseEmbed(spec, snapMD)
		curDoc, _, rErr := a.readMarkdownForControl(sp, docRel)
		if rErr != nil {
			return nil, rErr
		}
		nextDoc, iErr := insertMarkdownChunk(curDoc, blockMD, "", false)
		if iErr != nil {
			return nil, iErr
		}
		if err := a.commitMarkdownForControl(sp, docRel, nextDoc); err != nil {
			return nil, err
		}
		if p.Open {
			a.finishDocPostActions(sp, docRel, false, false, true)
		}
		return map[string]any{
			"space":      sp.Name,
			"database":   dbRel,
			"viewID":     viewID,
			"embeddedIn": docRel,
		}, nil

	default:
		return nil, fmt.Errorf("unknown database_schema op %q", p.Op)
	}
}

func (a *App) ctlDatabaseRecords(raw []byte) (any, error) {
	var p struct {
		Space  string         `json:"space"`
		Op     string         `json:"op"`
		Path   string         `json:"path"`
		View   string         `json:"view"`
		Search string         `json:"search"`
		Record string         `json:"record"`
		Title  string         `json:"title"`
		Fields map[string]any `json:"fields"`
		Body   *string        `json:"body"`
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	sp, err := a.resolveTargetSpace(p.Space)
	if err != nil {
		return nil, err
	}
	dbRel, err := normalizeDBRel(sp, p.Path, "")
	if err != nil {
		return nil, err
	}
	st, liveDoc, err := a.openDBStoreForControl(sp, dbRel)
	if err != nil {
		return nil, err
	}
	op := strings.ToLower(strings.TrimSpace(p.Op))
	if op == "" {
		op = "query"
	}

	switch op {
	case "query":
		curDB := st.Database()
		viewID := curDB.ActiveView
		if strings.TrimSpace(p.View) != "" {
			if v, ok := findDBView(curDB, p.View); ok {
				viewID = v.ID
			} else {
				return nil, fmt.Errorf("view %q not found", p.View)
			}
		}
		qres := st.Query(viewID, database.QueryOptions{Search: p.Search})
		rows := qres.Rows
		if len(rows) == 0 && len(qres.Groups) > 0 {
			for _, g := range qres.Groups {
				rows = append(rows, g.Rows...)
			}
		}
		formatted := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			if r == nil {
				continue
			}
			formatted = append(formatted, formatDBRecordJSON(curDB, r))
		}
		return map[string]any{
			"space":      sp.Name,
			"path":       dbRel,
			"viewID":     viewID,
			"totalCount": qres.TotalCount,
			"records":    formatted,
		}, nil

	case "add":
		vals, vErr := coerceRecordFields(st, p.Fields)
		if vErr != nil {
			return nil, vErr
		}
		body := ""
		if p.Body != nil {
			body = *p.Body
		}
		rec, cErr := a.createDBRecordForControl(sp, liveDoc, st, p.Title, vals, body)
		if cErr != nil {
			return nil, cErr
		}
		if err := a.saveDBStoreForControl(sp, dbRel, st, liveDoc); err != nil {
			return nil, err
		}
		_ = st.Query(st.Database().ActiveView, database.QueryOptions{})
		if latest := st.Record(rec.ID); latest != nil {
			rec = latest
		}
		return map[string]any{
			"space":  sp.Name,
			"path":   dbRel,
			"record": formatDBRecordJSON(st.Database(), rec),
		}, nil

	case "update":
		rec, fErr := resolveDBRecord(st, p.Record)
		if fErr != nil {
			return nil, fErr
		}
		var edits []database.CellEdit
		if strings.TrimSpace(p.Title) != "" {
			if tp := st.Database().TitleProperty(); tp != nil {
				edits = append(edits, database.CellEdit{
					RecordID: rec.ID,
					PropID:   tp.ID,
					Value:    database.TextValue(strings.TrimSpace(p.Title)),
				})
			}
		}
		if len(p.Fields) > 0 {
			vals, vErr := coerceRecordFields(st, p.Fields)
			if vErr != nil {
				return nil, vErr
			}
			for propID, val := range vals {
				edits = append(edits, database.CellEdit{
					RecordID: rec.ID,
					PropID:   propID,
					Value:    val,
				})
			}
		}
		if len(edits) > 0 {
			affected, sErr := st.SetCellsBatch(edits)
			if sErr != nil {
				return nil, sErr
			}
			a.flushDatabaseEditsToDocs(sp, liveDoc, st, affected)
		}
		updated := st.Record(rec.ID)
		if p.Body != nil && updated != nil {
			if updated.Path != "" {
				curText, _, rErr := a.readMarkdownForControl(sp, updated.Path)
				if rErr != nil {
					return nil, rErr
				}
				fm, _, hasFM, _ := database.SplitFrontmatter(curText)
				nextBody := ensureTrailingNewlineStr(*p.Body)
				if hasFM && !fm.IsEmpty() {
					nextBody = "---\n" + fm.Format() + "\n---\n\n" + strings.TrimLeft(nextBody, "\n")
				}
				if err := a.commitMarkdownForControl(sp, updated.Path, nextBody); err != nil {
					return nil, err
				}
				_, _ = st.Refresh(a.liveDocsForSpace(sp))
				updated = st.Record(rec.ID)
			} else {
				updated.Body = *p.Body
				updated.UpdatedAt = time.Now()
				st.InvalidateComputed()
				if liveDoc != nil {
					liveDoc.dbDirtySnapshot = true
				}
				if a.dbPeek.open && a.dbPeek.recordID == updated.ID &&
					(a.dbPeek.dbDoc == nil || a.dbPeek.dbDoc == liveDoc || (a.dbPeek.dbDoc.space == sp && a.dbPeek.dbDoc.rel == dbRel)) {
					a.dbPeek.inlineBody = *p.Body
				}
			}
		}
		if err := a.saveDBStoreForControl(sp, dbRel, st, liveDoc); err != nil {
			return nil, err
		}
		_ = st.Query(st.Database().ActiveView, database.QueryOptions{})
		var outRec any
		if updated != nil {
			if latest := st.Record(updated.ID); latest != nil {
				updated = latest
			}
			outRec = formatDBRecordJSON(st.Database(), updated)
		}
		return map[string]any{
			"space":  sp.Name,
			"path":   dbRel,
			"record": outRec,
		}, nil

	case "delete":
		rec, fErr := resolveDBRecord(st, p.Record)
		if fErr != nil {
			return nil, fErr
		}
		backingPath, dErr := st.DeleteRecord(rec.ID)
		if dErr != nil {
			return nil, dErr
		}
		if a.dbPeek.open && (a.dbPeek.recordID == rec.ID || (backingPath != "" && a.dbPeek.recordID == backingPath)) &&
			(a.dbPeek.dbDoc == nil || a.dbPeek.dbDoc == liveDoc || (a.dbPeek.dbDoc.space == sp && a.dbPeek.dbDoc.rel == dbRel)) {
			a.closeRecordPeek()
		}
		var trashInfo map[string]any
		if backingPath != "" && !sp.IsExternal() {
			tRes, tErr := a.safeTrashInSpace(sp, backingPath)
			if tErr != nil {
				return nil, tErr
			}
			trashInfo = tRes
		}
		if a.library.open {
			a.loadLibrary()
		}
		if err := a.saveDBStoreForControl(sp, dbRel, st, liveDoc); err != nil {
			return nil, err
		}
		return map[string]any{
			"space":           sp.Name,
			"path":            dbRel,
			"deletedRecordID": rec.ID,
			"trashedFile":     trashInfo,
		}, nil

	default:
		return nil, fmt.Errorf("unknown database_records op %q (use query, add, update, or delete)", p.Op)
	}
}

func (a *App) createDBRecordForControl(sp *space.Space, liveDoc *document, st *database.Store, title string, vals map[string]database.Value, body string) (*database.Record, error) {
	baseVals := make(map[string]database.Value, len(vals))
	var relPropIDs []string
	curDB := st.Database()
	for propID, val := range vals {
		if prop, ok := findDBProperty(curDB, propID); ok && prop.Type == database.PropRelation && len(val.List) > 0 {
			relPropIDs = append(relPropIDs, propID)
			continue
		}
		baseVals[propID] = val
	}
	rec, err := st.CreateRecord(title, baseVals, body)
	if err != nil {
		return nil, err
	}
	if len(relPropIDs) > 0 {
		edits := make([]database.CellEdit, 0, len(relPropIDs))
		for _, propID := range relPropIDs {
			edits = append(edits, database.CellEdit{
				RecordID: rec.ID,
				PropID:   propID,
				Value:    vals[propID],
			})
		}
		affected, sErr := st.SetCellsBatch(edits)
		if sErr != nil {
			return nil, sErr
		}
		a.flushDatabaseEditsToDocs(sp, liveDoc, st, affected)
		if updated := st.Record(rec.ID); updated != nil {
			rec = updated
		}
	}
	return rec, nil
}

func formatDBRecordJSON(db *database.Database, rec *database.Record) map[string]any {
	fields := make(map[string]any, len(db.Properties))
	display := make(map[string]string, len(db.Properties))
	for _, prop := range db.Properties {
		if prop.Type == database.PropTitle {
			continue
		}
		v := rec.ValueFor(prop)
		display[prop.Name] = database.FormatValue(prop, v)
		fields[prop.Name] = dbValueToJSON(prop, v)
	}
	out := map[string]any{
		"id":      rec.ID,
		"title":   rec.Title,
		"path":    rec.Path,
		"fields":  fields,
		"display": display,
	}
	if rec.Path == "" && rec.Body != "" {
		out["body"] = rec.Body
	}
	return out
}

func dbValueToJSON(prop database.Property, v database.Value) any {
	if v.IsEmpty() {
		return nil
	}
	switch prop.Type {
	case database.PropNumber:
		return v.Number
	case database.PropCheckbox:
		return v.Bool
	case database.PropMultiSelect, database.PropPerson, database.PropFiles, database.PropRelation:
		return slices.Clone(v.List)
	case database.PropDate:
		if v.Date.End != "" {
			return map[string]any{"start": v.Date.Start, "end": v.Date.End}
		}
		return v.Date.Start
	default:
		return v.PlainString()
	}
}

func resolveDBRecord(st *database.Store, selector string) (*database.Record, error) {
	sel := strings.TrimSpace(selector)
	if sel == "" {
		return nil, errors.New("record selector (ID, path, or title) is required")
	}
	if rec := st.Record(sel); rec != nil {
		return rec, nil
	}
	if rec := st.RecordByPath(sel); rec != nil {
		return rec, nil
	}
	var matches []*database.Record
	for _, r := range st.Records() {
		if r != nil && strings.EqualFold(strings.TrimSpace(r.Title), sel) {
			matches = append(matches, r)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("%d records share title %q; specify record ID or backing file path", len(matches), sel)
	}
	return nil, fmt.Errorf("record %q not found", sel)
}

func findDBProperty(db *database.Database, ref string) (database.Property, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" || db == nil {
		return database.Property{}, false
	}
	for _, p := range db.Properties {
		if p.ID == ref || strings.EqualFold(p.Key, ref) || strings.EqualFold(p.Name, ref) {
			return p, true
		}
	}
	return database.Property{}, false
}

func findDBView(db *database.Database, ref string) (database.ViewConfig, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" || db == nil {
		return database.ViewConfig{}, false
	}
	for _, v := range db.Views {
		if v.ID == ref || strings.EqualFold(v.Name, ref) {
			return v.Clone(), true
		}
	}
	return database.ViewConfig{}, false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func buildDBProperty(existing []database.Property, in dbPropertyInput, seq int) (database.Property, error) {
	pType := database.PropertyType(strings.ToLower(strings.TrimSpace(in.Type)))
	if pType == "" {
		pType = database.PropText
	}
	if !database.ValidPropertyType(pType) {
		return database.Property{}, fmt.Errorf("unsupported property type %q", in.Type)
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = string(pType) + "_" + strconv.Itoa(seq)
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = "prop_" + database.NewID()
	}
	key := strings.TrimSpace(in.Key)
	if key == "" {
		key = sanitizePropKey(name, seq, existing)
	}
	prop := database.Property{
		ID:          id,
		Key:         key,
		Name:        name,
		Type:        pType,
		Options:     normalizeSelectOptions(pType, in.Options),
		Number:      in.Number,
		Date:        in.Date,
		Relation:    in.Relation,
		Rollup:      in.Rollup,
		Formula:     in.Formula,
		UniqueID:    in.UniqueID,
		Description: in.Description,
		Wrap:        in.Wrap,
		Width:       in.Width,
	}
	if pType == database.PropFormula && (prop.Formula == nil || strings.TrimSpace(prop.Formula.Expression) == "") {
		prop.Formula = &database.FormulaConfig{Expression: `""`}
	}
	if pType == database.PropUniqueID && prop.UniqueID == nil {
		prop.UniqueID = &database.UniqueIDConfig{Prefix: "ID", Next: 1}
	}
	return prop, nil
}

func mergeDBProperty(cur database.Property, patch dbPropertyInput) (database.Property, error) {
	out := cur.Clone()
	if strings.TrimSpace(patch.Name) != "" {
		out.Name = strings.TrimSpace(patch.Name)
	}
	if strings.TrimSpace(patch.Type) != "" {
		pt := database.PropertyType(strings.ToLower(strings.TrimSpace(patch.Type)))
		if !database.ValidPropertyType(pt) {
			return out, fmt.Errorf("unsupported property type %q", patch.Type)
		}
		out.Type = pt
	}
	if len(patch.Options) > 0 {
		out.Options = normalizeSelectOptions(out.Type, patch.Options)
	}
	if patch.Number != nil {
		out.Number = patch.Number
	}
	if patch.Date != nil {
		out.Date = patch.Date
	}
	if patch.Relation != nil {
		out.Relation = patch.Relation
	}
	if patch.Rollup != nil {
		out.Rollup = patch.Rollup
	}
	if patch.Formula != nil {
		out.Formula = patch.Formula
	}
	if patch.UniqueID != nil {
		out.UniqueID = patch.UniqueID
	}
	if patch.Description != "" {
		out.Description = patch.Description
	}
	if patch.Width > 0 {
		out.Width = patch.Width
	}
	return out, nil
}

func normalizeSelectOptions(pType database.PropertyType, opts []database.SelectOption) []database.SelectOption {
	if pType != database.PropSelect && pType != database.PropMultiSelect && pType != database.PropStatus {
		return nil
	}
	if len(opts) == 0 && pType == database.PropStatus {
		return []database.SelectOption{
			{ID: "opt_todo", Name: "未开始", Color: "gray", Group: database.StatusTodo},
			{ID: "opt_doing", Name: "进行中", Color: "blue", Group: database.StatusInProgress},
			{ID: "opt_done", Name: "已完成", Color: "green", Group: database.StatusComplete},
		}
	}
	colors := []string{"blue", "green", "orange", "purple", "red", "yellow", "pink", "gray"}
	out := make([]database.SelectOption, 0, len(opts))
	for i, o := range opts {
		name := strings.TrimSpace(o.Name)
		if name == "" {
			continue
		}
		id := strings.TrimSpace(o.ID)
		if id == "" {
			id = "opt_" + database.NewID()
		}
		col := strings.TrimSpace(o.Color)
		if col == "" {
			col = colors[i%len(colors)]
		}
		grp := o.Group
		if pType == database.PropStatus && grp == "" {
			grp = database.StatusTodo
		}
		out = append(out, database.SelectOption{ID: id, Name: name, Color: col, Group: grp})
	}
	return out
}

func sanitizePropKey(name string, seq int, existing []database.Property) string {
	used := map[string]bool{"icon": true, "cover": true, "covery": true, "goal": true}
	for _, p := range existing {
		used[strings.ToLower(p.Key)] = true
	}
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-', r > 127:
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('_')
		}
	}
	cand := strings.Trim(b.String(), "_-")
	if cand == "" {
		cand = fmt.Sprintf("field_%d", seq)
	}
	base := cand
	for i := 1; used[strings.ToLower(cand)]; i++ {
		cand = fmt.Sprintf("%s_%d", base, i+1)
	}
	return cand
}

func buildDBViewConfig(db *database.Database, in dbViewInput) (database.ViewConfig, error) {
	vt := database.ViewType(strings.ToLower(strings.TrimSpace(in.Type)))
	if vt == "" {
		vt = database.ViewTable
	}
	if !database.ValidViewType(vt) {
		return database.ViewConfig{}, fmt.Errorf("unsupported view type %q", in.Type)
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = "v_" + database.NewID()
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = string(vt)
	}
	vps := make([]database.ViewProperty, len(db.Properties))
	for i, pr := range db.Properties {
		vps[i] = database.ViewProperty{PropertyID: pr.ID, Visible: true}
	}
	vcfg := database.ViewConfig{
		ID:         id,
		Name:       name,
		Type:       vt,
		Icon:       strings.TrimSpace(in.Icon),
		Properties: vps,
	}
	if strings.TrimSpace(in.GroupBy) != "" {
		pr, ok := findDBProperty(db, in.GroupBy)
		if !ok {
			return vcfg, fmt.Errorf("groupBy property %q not found", in.GroupBy)
		}
		vcfg.Group = &database.GroupConfig{PropertyID: pr.ID}
	} else if vt == database.ViewBoard {
		for _, pr := range db.Properties {
			if pr.Type == database.PropStatus || pr.Type == database.PropSelect {
				vcfg.Group = &database.GroupConfig{PropertyID: pr.ID}
				break
			}
		}
	}
	for _, s := range in.Sorts {
		pr, ok := findDBProperty(db, s.Property)
		if !ok {
			return vcfg, fmt.Errorf("sort property %q not found", s.Property)
		}
		vcfg.Sorts = append(vcfg.Sorts, database.SortRule{PropertyID: pr.ID, Desc: s.Desc})
	}
	if vt == database.ViewTimeline {
		tl := &database.TimelineViewConfig{Zoom: "month", ShowTable: true}
		if in.StartProp != "" {
			if pr, ok := findDBProperty(db, in.StartProp); ok {
				tl.StartPropID = pr.ID
			}
		}
		if in.EndProp != "" {
			if pr, ok := findDBProperty(db, in.EndProp); ok {
				tl.EndPropID = pr.ID
			}
		}
		vcfg.Timeline = tl
	}
	if vt == database.ViewCalendar {
		cal := &database.CalendarViewConfig{Mode: "month", ShowWeekends: true, WeekStart: 1}
		if in.DateProp != "" {
			if pr, ok := findDBProperty(db, in.DateProp); ok {
				cal.DatePropID = pr.ID
			}
		}
		vcfg.Calendar = cal
	}
	if vt == database.ViewChart && in.Chart != nil {
		ch := &database.ChartViewConfig{
			Kind:  firstNonEmpty(in.Chart.Kind, "bar"),
			YMode: firstNonEmpty(in.Chart.YMode, "count"),
			YAgg:  database.AggregationType(firstNonEmpty(in.Chart.YAgg, string(database.AggSum))),
		}
		if in.Chart.Smooth != nil {
			ch.Smooth = *in.Chart.Smooth
		}
		if in.Chart.XProp != "" {
			if pr, ok := findDBProperty(db, in.Chart.XProp); ok {
				ch.XPropID = pr.ID
			}
		}
		if in.Chart.YProp != "" {
			if pr, ok := findDBProperty(db, in.Chart.YProp); ok {
				ch.YPropID = pr.ID
				if in.Chart.YMode == "" {
					ch.YMode = "prop"
				}
			}
		}
		if in.Chart.StackProp != "" {
			if pr, ok := findDBProperty(db, in.Chart.StackProp); ok {
				ch.StackPropID = pr.ID
			}
		}
		if in.Chart.ShowValues != nil {
			ch.ShowValues = *in.Chart.ShowValues
		} else {
			ch.ShowValues = true
		}
		if in.Chart.ShowLegend != nil {
			ch.ShowLegend = *in.Chart.ShowLegend
		} else {
			ch.ShowLegend = true
		}
		vcfg.Chart = ch
	}
	return vcfg, nil
}

func mergeDBViewConfig(db *database.Database, existing database.ViewConfig, patch dbViewInput) (database.ViewConfig, error) {
	vcfg := existing.Clone()
	if name := strings.TrimSpace(patch.Name); name != "" {
		vcfg.Name = name
	}
	typeChanged := false
	if rawType := strings.TrimSpace(patch.Type); rawType != "" {
		vt := database.ViewType(strings.ToLower(rawType))
		if !database.ValidViewType(vt) {
			return vcfg, fmt.Errorf("unsupported view type %q", patch.Type)
		}
		typeChanged = vt != vcfg.Type
		vcfg.Type = vt
	}
	if icon := strings.TrimSpace(patch.Icon); icon != "" {
		if icon == "-" {
			vcfg.Icon = ""
		} else {
			vcfg.Icon = icon
		}
	}
	if gb := strings.TrimSpace(patch.GroupBy); gb != "" {
		if gb == "-" {
			vcfg.Group = nil
		} else {
			pr, ok := findDBProperty(db, gb)
			if !ok {
				return vcfg, fmt.Errorf("groupBy property %q not found", patch.GroupBy)
			}
			if vcfg.Group != nil {
				vcfg.Group.PropertyID = pr.ID
			} else {
				vcfg.Group = &database.GroupConfig{PropertyID: pr.ID}
			}
		}
	} else if typeChanged && vcfg.Type == database.ViewBoard && vcfg.Group == nil {
		for _, pr := range db.Properties {
			if pr.Type == database.PropStatus || pr.Type == database.PropSelect {
				vcfg.Group = &database.GroupConfig{PropertyID: pr.ID}
				break
			}
		}
	}
	if patch.Sorts != nil {
		sorts := make([]database.SortRule, 0, len(patch.Sorts))
		for _, s := range patch.Sorts {
			pr, ok := findDBProperty(db, s.Property)
			if !ok {
				return vcfg, fmt.Errorf("sort property %q not found", s.Property)
			}
			sorts = append(sorts, database.SortRule{PropertyID: pr.ID, Desc: s.Desc})
		}
		vcfg.Sorts = sorts
	}
	if vcfg.Type == database.ViewTimeline && (typeChanged || patch.StartProp != "" || patch.EndProp != "") {
		if vcfg.Timeline == nil {
			vcfg.Timeline = &database.TimelineViewConfig{Zoom: "month", ShowTable: true}
		}
		if patch.StartProp != "" {
			pr, ok := findDBProperty(db, patch.StartProp)
			if !ok {
				return vcfg, fmt.Errorf("startProp property %q not found", patch.StartProp)
			}
			vcfg.Timeline.StartPropID = pr.ID
		}
		if patch.EndProp != "" {
			pr, ok := findDBProperty(db, patch.EndProp)
			if !ok {
				return vcfg, fmt.Errorf("endProp property %q not found", patch.EndProp)
			}
			vcfg.Timeline.EndPropID = pr.ID
		}
	}
	if vcfg.Type == database.ViewCalendar && (typeChanged || patch.DateProp != "") {
		if vcfg.Calendar == nil {
			vcfg.Calendar = &database.CalendarViewConfig{Mode: "month", ShowWeekends: true, WeekStart: 1}
		}
		if patch.DateProp != "" {
			pr, ok := findDBProperty(db, patch.DateProp)
			if !ok {
				return vcfg, fmt.Errorf("dateProp property %q not found", patch.DateProp)
			}
			vcfg.Calendar.DatePropID = pr.ID
		}
	}
	if vcfg.Type == database.ViewChart && (typeChanged || patch.Chart != nil) {
		if vcfg.Chart == nil {
			vcfg.Chart = &database.ChartViewConfig{
				Kind:       "bar",
				YMode:      "count",
				YAgg:       database.AggSum,
				ShowValues: true,
				ShowLegend: true,
			}
		}
		if patch.Chart != nil {
			if patch.Chart.Kind != "" {
				vcfg.Chart.Kind = strings.TrimSpace(patch.Chart.Kind)
			}
			if patch.Chart.YMode != "" {
				vcfg.Chart.YMode = strings.TrimSpace(patch.Chart.YMode)
			}
			if patch.Chart.YAgg != "" {
				vcfg.Chart.YAgg = database.AggregationType(strings.TrimSpace(patch.Chart.YAgg))
			}
			if patch.Chart.XProp != "" {
				pr, ok := findDBProperty(db, patch.Chart.XProp)
				if !ok {
					return vcfg, fmt.Errorf("xProp property %q not found", patch.Chart.XProp)
				}
				vcfg.Chart.XPropID = pr.ID
			}
			if patch.Chart.YProp != "" {
				pr, ok := findDBProperty(db, patch.Chart.YProp)
				if !ok {
					return vcfg, fmt.Errorf("yProp property %q not found", patch.Chart.YProp)
				}
				vcfg.Chart.YPropID = pr.ID
				if patch.Chart.YMode == "" {
					vcfg.Chart.YMode = "prop"
				}
			}
			if patch.Chart.StackProp != "" {
				pr, ok := findDBProperty(db, patch.Chart.StackProp)
				if !ok {
					return vcfg, fmt.Errorf("stackProp property %q not found", patch.Chart.StackProp)
				}
				vcfg.Chart.StackPropID = pr.ID
			}
			if patch.Chart.ShowValues != nil {
				vcfg.Chart.ShowValues = *patch.Chart.ShowValues
			}
			if patch.Chart.ShowLegend != nil {
				vcfg.Chart.ShowLegend = *patch.Chart.ShowLegend
			}
			if patch.Chart.Smooth != nil {
				vcfg.Chart.Smooth = *patch.Chart.Smooth
			}
		}
	}
	return vcfg, nil
}

func coerceRecordFields(st *database.Store, fields map[string]any) (map[string]database.Value, error) {
	if len(fields) == 0 {
		return nil, nil
	}
	out := make(map[string]database.Value, len(fields))
	for k, rawVal := range fields {
		curDB := st.Database()
		prop, ok := findDBProperty(curDB, k)
		if !ok {
			return nil, fmt.Errorf("unknown database property %q", k)
		}
		if prop.Type == database.PropFormula || prop.Type == database.PropRollup ||
			prop.Type == database.PropCreatedTime || prop.Type == database.PropEditedTime || prop.Type == database.PropUniqueID {
			continue
		}
		val, err := rawJSONToDBValue(st, prop, rawVal)
		if err != nil {
			return nil, fmt.Errorf("property %q: %w", prop.Name, err)
		}
		out[prop.ID] = val
	}
	return out, nil
}

func rawJSONToDBValue(st *database.Store, prop database.Property, rawVal any) (database.Value, error) {
	if rawVal == nil {
		return database.EmptyValue(), nil
	}
	ensureOptions := func(names ...string) error {
		if prop.Type != database.PropSelect && prop.Type != database.PropMultiSelect {
			return nil
		}
		curDB := st.Database()
		latest, ok := findDBProperty(curDB, prop.ID)
		if !ok {
			return nil
		}
		changed := false
		colors := []string{"blue", "green", "orange", "purple", "red", "yellow", "pink", "gray"}
		for _, n := range names {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			exists := false
			for _, o := range latest.Options {
				if strings.EqualFold(o.Name, n) || o.ID == n {
					exists = true
					break
				}
			}
			if !exists {
				latest.Options = append(latest.Options, database.SelectOption{
					ID:    "opt_" + database.NewID(),
					Name:  n,
					Color: colors[len(latest.Options)%len(colors)],
				})
				changed = true
			}
		}
		if changed {
			_, err := st.UpdateProperty(latest)
			if err != nil {
				return err
			}
			prop = latest
		}
		return nil
	}

	switch tv := rawVal.(type) {
	case bool:
		return database.CoerceValue(prop, database.BoolValue(tv)), nil
	case float64:
		return database.CoerceValue(prop, database.NumberValue(tv)), nil
	case string:
		if prop.Type == database.PropSelect {
			if err := ensureOptions(tv); err != nil {
				return database.EmptyValue(), err
			}
			if latest, ok := findDBProperty(st.Database(), prop.ID); ok {
				prop = latest
			}
		}
		if prop.Type == database.PropMultiSelect {
			parts := strings.Split(tv, ",")
			if err := ensureOptions(parts...); err != nil {
				return database.EmptyValue(), err
			}
			if latest, ok := findDBProperty(st.Database(), prop.ID); ok {
				prop = latest
			}
		}
		return database.ParseImportedCellValue(prop, tv), nil
	case []any:
		strs := make([]string, 0, len(tv))
		for _, item := range tv {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s != "" {
				strs = append(strs, s)
			}
		}
		if err := ensureOptions(strs...); err != nil {
			return database.EmptyValue(), err
		}
		if latest, ok := findDBProperty(st.Database(), prop.ID); ok {
			prop = latest
		}
		return database.CoerceValue(prop, database.ListValue(strs...)), nil
	case map[string]any:
		if prop.Type == database.PropDate {
			start := strings.TrimSpace(fmt.Sprint(tv["start"]))
			end := ""
			if ev, ok := tv["end"]; ok && ev != nil {
				end = strings.TrimSpace(fmt.Sprint(ev))
			}
			incTime, _ := tv["includeTime"].(bool)
			return database.CoerceValue(prop, database.DateRangeValue(start, end, incTime)), nil
		}
		b, _ := json.Marshal(tv)
		return database.CoerceValue(prop, database.TextValue(string(b))), nil
	default:
		return database.ParseImportedCellValue(prop, fmt.Sprint(tv)), nil
	}
}
