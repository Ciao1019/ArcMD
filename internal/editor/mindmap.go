package editor

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/mindmap"
	"github.com/egoist/mygo/ui"
)

const (
	mapMaxCodeHeight   float32 = 186
	mapMaxProseRunes           = 300
	mapMaxProseLines           = 8
	mapMaxProseHeight  float32 = 168
	mapMaxTableRows            = 7
	mapMaxTableCols            = 6
	mapMaxTableCellR           = 100
	mapMaxTableHeight  float32 = 196
	mapMaxVisualHeight float32 = 196
	mapMaxColumnBlocks         = 3
	mapPrepareBudget           = 8
)

// MapActions supplies host-level capabilities (creating documents, PNG/SVG
// export, toast notifications) to the mind map without coupling editor to app.
type MapActions struct {
	ExtractBranch func(title, markdown string) (linkTarget string, ok bool)
	CopyPNG       func(branchID string)
	ExportPNG     func(branchID string)
	ExportSVG     func(branchID string)
	Notify        func(msg string)
}

// MindMap is a navigable projection of one editor. Geometry, scroll and folds
// belong to the view; the document engine owns all text and selection.
type MindMap struct {
	gestureChanged   bool
	movingUntil      time.Time
	moving           bool
	layers           map[*mapBody]bool
	layoutRevision   uint64
	wrapWidth        float32
	importSource     string
	importHints      map[string]document.ListPlacement
	docTitle         string
	rootEmpty        bool
	caret            int
	caretOwner       string
	selected         string
	focusID          string
	focusRoot        string
	headingsOnly     bool
	syncHeadingsOnly bool
	actions          MapActions
	locatePending    bool
	foldInitialized  bool
	fitRequested     bool
	tree             mindmap.State
	panX, panY       float32
	viewW, viewH     float32
	zoom             float32
	autoFit          bool
	doc              *document.Document
	revision         uint64
	theme            ui.Theme
	style            layoutPageKey
	title            mindmap.Item
	items            []mindmap.Item
	itemIndex        map[string]int
	ownerMap         map[string]string
	bodies           map[string]*mapBody
	cache            map[string]*cachedMapBlock
}

// SetActions attaches host callbacks for branch extraction, image export and notifications.
func (m *MindMap) SetActions(a MapActions) {
	m.actions = a
}

// FocusRoot returns the currently focused branch ID, or "" when showing the full map.
func (m *MindMap) FocusRoot() string { return m.focusRoot }

// HeadingsOnly reports whether the mind map currently hides non-heading blocks.
func (m *MindMap) HeadingsOnly() bool { return m.headingsOnly }

type cachedMapBlock struct {
	stamp      runsStamp
	metaHash   uint64
	listNumber int
	excerpt    string
	rich       bool
	body       *mapBody
}

type mapBody struct {
	layer         *ui.Canvas
	codeCaption   *ui.TextLayout
	moreLayout    *ui.TextLayout
	moreHint      string
	limit         float32
	code          *ui.TextLayout
	source        *document.Block
	editor        *Editor
	span          ui.Rect
	rawW, rawH    float32
	fitScale      float32
	w, h          float32
	listNumber    int
	clipped       bool
	heightClipped bool
	isCode        bool
	calloutTone   string
}

// CollapseBranches folds branch nodes while keeping ancestors of the active node open.
func (m *MindMap) CollapseBranches() {
	m.collapseBranches(false)
}

func (m *MindMap) collapseBranches(auto bool) {
	minDepth := 1
	if len(m.items) > 260 {
		topCount := 0
		for _, it := range m.items {
			if it.Depth == 0 {
				topCount++
			}
		}
		if topCount >= 6 {
			minDepth = 0
		}
	}
	before := m.tree.HasFolded()
	m.tree.CollapseBranches(m.items, minDepth, m.selected)
	if !auto && minDepth > 0 && !before && !m.tree.HasFolded() {
		m.tree.CollapseBranches(m.items, 0, m.selected)
	}
	m.locatePending = true
}

// UnfoldAll opens every folded branch in the mind map.
func (m *MindMap) UnfoldAll() {
	m.tree.UnfoldAll()
	m.locatePending = true
}

// HasFolded reports whether any branch in the map is currently folded.
func (m *MindMap) HasFolded() bool {
	return m.tree.HasFolded()
}

// HasFoldable reports whether the map has any branch that can be folded.
func (m *MindMap) HasFoldable() bool {
	return m.tree.HasFoldable()
}

// CenterAndFit centers and fits the map into its viewport.
func (m *MindMap) CenterAndFit() {
	m.autoFit = true
	m.fitRequested = true
	m.locatePending = true
}

func shouldSkipMapBlock(b *document.Block) bool {
	if b == nil {
		return true
	}
	if b.Kind == "rule" || (b.Kind == "raw" && isHTMLCommentBlock(b.Text())) {
		return true
	}
	return b.Kind == "paragraph" && strings.TrimSpace(b.Text()) == ""
}

// A short unformatted label gets the component's original typography. Other
// nodes use ArcMD's native block painter, including inline TeX and source colors.
func richMapBlock(b *document.Block) bool {
	if b.Kind != "heading" && b.Kind != "paragraph" {
		return true
	}
	for _, r := range b.Runs {
		if r.Style != (document.Style{}) || strings.ContainsAny(r.Text, "$\n") {
			return true
		}
	}
	return utf8.RuneCountInString(b.Text()) > 28
}

func blockStamp(b *document.Block) (runsStamp, uint64) {
	if b == nil {
		return runsStamp{}, 0
	}
	var st runsStamp
	if b.Kind == "table" {
		st = tableStamp(b)
	} else {
		st = makeRunsStamp(b.Runs)
	}
	h := uint64(14695981039346656037)
	h = fnvMixString(h, b.ID)
	h = fnvMixString(h, b.Kind)
	h = fnvMixUint64(h, uint64(b.Level))
	h = fnvMixUint64(h, uint64(b.Indent))
	if b.Checked {
		h = fnvMixByte(h, 1)
	} else {
		h = fnvMixByte(h, 0)
	}
	h = fnvMixString(h, b.Language)
	h = fnvMixString(h, b.URL)
	h = fnvMixString(h, b.Align)
	h = fnvMixString(h, b.Color)
	if b.Folded {
		h = fnvMixByte(h, 1)
	}
	h = fnvMixString(h, b.Background)
	h = fnvMixUint64(h, uint64(b.ColSpan)<<16|uint64(b.RowSpan))
	h = fnvMixString(h, b.Emoji)
	h = fnvMixUint32(h, math.Float32bits(b.ImageWidth))
	h = fnvMixString(h, b.NoteLabel)
	h = fnvMixUint64(h, uint64(len(b.Widths)))
	for _, w := range b.Widths {
		h = fnvMixUint32(h, math.Float32bits(w))
	}
	h = fnvMixUint64(h, uint64(len(b.Children)))
	if b.Kind == "table" {
		for _, tr := range b.Children {
			for _, cell := range tr.Children {
				h = fnvMixString(h, cell.Background)
			}
		}
	} else {
		for _, child := range b.Children {
			cs, cm := blockStamp(child)
			st.lo ^= cs.lo
			st.hi ^= cs.hi
			h = fnvMixUint64(h, cm)
		}
	}
	return st, h
}

func codeSourceSample(b *document.Block, maxLines, maxRunes int) (string, int, bool) {
	if b == nil || len(b.Runs) == 0 {
		return "", 1, false
	}
	if len(b.Runs) == 1 {
		s := b.Runs[0].Text
		totalLines := 1 + strings.Count(s, "\n")
		capped, clipped := capCodePreviewText(s, maxLines, maxRunes)
		return capped, totalLines, clipped
	}
	totalLines := 1
	var buf strings.Builder
	runes := 0
	clipped := false
	for _, r := range b.Runs {
		totalLines += strings.Count(r.Text, "\n")
		if clipped {
			continue
		}
		for i := 0; i < len(r.Text); {
			if r.Text[i] == '\n' {
				curLines := 1 + strings.Count(buf.String(), "\n")
				if maxLines > 0 && curLines >= maxLines {
					clipped = true
					break
				}
				buf.WriteByte('\n')
				i++
				runes++
				continue
			}
			if maxRunes > 0 && runes >= maxRunes {
				buf.WriteString("…")
				clipped = true
				break
			}
			rn, sz := utf8.DecodeRuneInString(r.Text[i:])
			buf.WriteRune(rn)
			i += sz
			runes++
		}
	}
	if clipped {
		return strings.TrimRight(buf.String(), "\r\n"), totalLines, true
	}
	return buf.String(), totalLines, false
}

// truncateMapRuns keeps at most maxRunes and maxNewlines across runs and trims
// back any unmatched inline math '$' delimiter on the cut line before adding "…".
func truncateMapRuns(runs []document.Run, maxRunes, maxNewlines int) ([]document.Run, bool) {
	if len(runs) == 0 {
		return nil, false
	}
	var kept []document.Run
	runes := 0
	lines := 0
	clipped := false
	for _, r := range runs {
		if r.Text == "" {
			continue
		}
		cut := len(r.Text)
		for i := 0; i < len(r.Text); {
			if r.Text[i] == '\n' {
				if maxNewlines > 0 && lines >= maxNewlines {
					cut = i
					clipped = true
					break
				}
				lines++
				i++
				runes++
				continue
			}
			if maxRunes > 0 && runes >= maxRunes {
				cut = i
				clipped = true
				break
			}
			_, sz := utf8.DecodeRuneInString(r.Text[i:])
			i += sz
			runes++
		}
		if cut > 0 {
			copyRun := r
			copyRun.Text = r.Text[:cut]
			kept = append(kept, copyRun)
		}
		if clipped {
			break
		}
	}
	if !clipped {
		return kept, false
	}
	// Close or back out of any open single-$ inline math span on the final line
	// so truncated previews never display malformed TeX syntax.
	inMath := false
	openRun, openByte := -1, -1
	for ri := range kept {
		r := kept[ri]
		if r.Style.Code || r.Style.Kbd || r.Style.Link != "" || r.Style.NoMath {
			continue
		}
		s := r.Text
		for i := 0; i < len(s); {
			if s[i] == '\n' {
				inMath = false
				openRun, openByte = -1, -1
				i++
				continue
			}
			if s[i] == '\\' {
				_, sz := utf8.DecodeRuneInString(s[min(i+1, len(s)):])
				i += 1 + sz
				continue
			}
			if s[i] == '$' {
				if (i+1 < len(s) && s[i+1] == '$') || (i > 0 && s[i-1] == '$') {
					i++
					continue
				}
				if !inMath {
					if i+1 < len(s) {
						next, _ := utf8.DecodeRuneInString(s[i+1:])
						if next != ' ' && next != '\t' && next != '\n' {
							inMath = true
							openRun, openByte = ri, i
						}
					} else {
						inMath = true
						openRun, openByte = ri, i
					}
				} else {
					inMath = false
					openRun, openByte = -1, -1
				}
			}
			_, sz := utf8.DecodeRuneInString(s[i:])
			i += sz
		}
	}
	if inMath && openRun >= 0 {
		kept = kept[:openRun+1]
		kept[openRun].Text = strings.TrimRight(kept[openRun].Text[:openByte], " \t")
		for len(kept) > 0 && kept[len(kept)-1].Text == "" {
			kept = kept[:len(kept)-1]
		}
	}
	if len(kept) == 0 {
		return []document.Run{{Text: "…"}}, true
	}
	last := &kept[len(kept)-1]
	last.Text = strings.TrimRight(last.Text, "\r\n")
	last.Text += "…"
	return kept, true
}

func truncateMapBlock(src *document.Block) (preview *document.Block, clipped bool, moreHint string) {
	if src == nil {
		return nil, false, ""
	}
	switch src.Kind {
	case "table":
		totalRows := len(src.Children)
		totalCols := 0
		if totalRows > 0 {
			totalCols = len(src.Children[0].Children)
		}
		keptRows := min(totalRows, mapMaxTableRows)
		keptCols := min(totalCols, mapMaxTableCols)
		rowsClipped := totalRows > keptRows
		colsClipped := totalCols > keptCols
		clipped = rowsClipped || colsClipped
		p := &document.Block{
			ID:       src.ID,
			Kind:     "table",
			Align:    src.Align,
			Children: make([]*document.Block, 0, keptRows),
		}
		for ri := 0; ri < keptRows; ri++ {
			tr := src.Children[ri]
			rowCols := min(len(tr.Children), keptCols)
			rowCopy := &document.Block{
				ID:       tr.ID,
				Kind:     tr.Kind,
				Children: make([]*document.Block, 0, rowCols),
			}
			for ci := 0; ci < rowCols; ci++ {
				cell := tr.Children[ci]
				runs, cellClipped := truncateMapRuns(cell.Runs, mapMaxTableCellR, 2)
				if cellClipped {
					clipped = true
				}
				colSpan := cell.ColSpan
				if colSpan > rowCols-ci {
					colSpan = max(1, rowCols-ci)
				}
				rowSpan := cell.RowSpan
				if rowSpan > keptRows-ri {
					rowSpan = max(1, keptRows-ri)
				}
				if colSpan == 1 {
					colSpan = 0
				}
				if rowSpan == 1 {
					rowSpan = 0
				}
				rowCopy.Children = append(rowCopy.Children, &document.Block{
					ID:         cell.ID,
					Kind:       cell.Kind,
					Runs:       runs,
					Align:      cell.Align,
					Background: cell.Background,
					ColSpan:    colSpan,
					RowSpan:    rowSpan,
				})
			}
			p.Children = append(p.Children, rowCopy)
		}
		if rowsClipped {
			moreHint = fmt.Sprintf(i18n.T("还有 %d 行"), totalRows-keptRows)
		} else if colsClipped {
			moreHint = i18n.T("部分列已省略")
		}
		return p, clipped, moreHint

	case "columns":
		p := &document.Block{
			ID:       src.ID,
			Kind:     "columns",
			Widths:   slices.Clone(src.Widths),
			Children: make([]*document.Block, 0, len(src.Children)),
		}
		for _, col := range src.Children {
			colCopy := &document.Block{
				ID:       col.ID,
				Kind:     col.Kind,
				Children: make([]*document.Block, 0, min(len(col.Children), mapMaxColumnBlocks)),
			}
			limit := min(len(col.Children), mapMaxColumnBlocks)
			if len(col.Children) > limit {
				clipped = true
			}
			for ci := 0; ci < limit; ci++ {
				child, cClipped, _ := truncateMapBlock(col.Children[ci])
				if cClipped {
					clipped = true
				}
				colCopy.Children = append(colCopy.Children, child)
			}
			p.Children = append(p.Children, colCopy)
		}
		if clipped {
			moreHint = i18n.T("内容较长")
		}
		return p, clipped, moreHint
	}

	p := &document.Block{
		ImageWidth: src.ImageWidth,
		NoteLabel:  src.NoteLabel,
		ID:         src.ID,
		Kind:       src.Kind,
		Level:      src.Level,
		Indent:     src.Indent,
		Checked:    src.Checked,
		Language:   src.Language,
		URL:        src.URL,
		Align:      src.Align,
		Color:      src.Color,
		Emoji:      src.Emoji,
		Background: src.Background,
		ColSpan:    src.ColSpan,
		RowSpan:    src.RowSpan,
	}
	if src.Kind == "math" || isMermaidBlock(src) || isWhiteboardBlock(src) || isWhiteboardEmbed(src) || src.Kind == "image" {
		p.Runs = slices.Clone(src.Runs)
		return p, false, ""
	}
	p.Runs, clipped = truncateMapRuns(src.Runs, mapMaxProseRunes, mapMaxProseLines)
	return p, clipped, ""
}

// Whiteboards have an independent revision even when their Markdown URL is unchanged.
func (b *mapBody) needsMeasure(o Options) bool {
	if b.code != nil {
		return false
	}
	if b.editor == nil {
		return true
	}
	if isWhiteboardEmbed(b.source) && o.LoadWhiteboard != nil {
		scene, rev, err := o.LoadWhiteboard(b.source.URL)
		cached, ok := b.editor.embedBoards[b.source.URL]
		if err != nil || scene == nil {
			return ok
		}
		return !ok || scene != cached.scene || rev != cached.rev
	}
	if isPDFEmbed(b.source) && o.LoadPDF != nil {
		clean, _ := ParsePDFURLPage(b.source.URL)
		doc, rev, err := o.LoadPDF(clean)
		cached, ok := b.editor.embedPDFs[clean]
		if err != nil || doc == nil {
			return ok
		}
		return !ok || doc != cached.doc || rev != cached.rev
	}
	if b.source.Kind == "image" && !isPDFEmbed(b.source) && o.LoadImage != nil && len(b.editor.rows) == 1 {
		fig := b.editor.rows[0].picture
		return fig != nil && fig.resource.Loading && !o.LoadImage(b.source.URL).Loading
	}
	return false
}

func (b *mapBody) measure(o Options) {
	b.layer = nil
	b.moreLayout = nil
	b.moreHint = ""
	b.clipped = false
	b.heightClipped = false
	b.fitScale = 1
	b.isCode = false
	b.calloutTone = ""
	if b.source.Kind == "code" && b.source.Language != "mermaid" && !isWhiteboardBlock(b.source) {
		b.isCode = true
		limit := max(36, b.limit-8)
		sample, totalLines, textClipped := codeSourceSample(b.source, mapMaxCodeLines, mapMaxCodeRunes)
		prev := mapCodePreviewLayout(sample, b.source.Language, pageOf(o.Page, o.Theme), limit)
		b.code, b.w, b.h = prev.layout, prev.w, prev.h
		label := "</>"
		if name := languageLabel(b.source.Language); name != "" {
			label += "  " + name
		}
		b.codeCaption = ui.ShapeText(label, ui.Font{Family: "monospace", Size: 10, Weight: 600, LineHeight: 1.1}, limit)
		cw, ch := b.codeCaption.Size()
		b.w = max(b.w, cw) + 8
		b.h += ch + 7
		if b.h > mapMaxCodeHeight {
			b.h = mapMaxCodeHeight
			b.heightClipped = true
			b.clipped = true
		}
		if textClipped || prev.clipped {
			b.clipped = true
			b.heightClipped = true
		}
		if b.clipped {
			if totalLines > 1 {
				b.moreHint = fmt.Sprintf(i18n.T("共 %d 行"), totalLines)
			} else {
				b.moreHint = i18n.T("内容较长")
			}
		}
		b.rawW, b.rawH = b.w, b.h
		return
	}
	block, textClipped, hint := truncateMapBlock(b.source)
	b.moreHint = hint
	b.clipped = textClipped
	// Depth is already expressed by the map. Give styled headings a compact
	// text layout instead of printing each standalone snapshot as a page title.
	if block.Kind == "heading" {
		block.Kind = "paragraph"
		for j := range block.Runs {
			block.Runs[j].Style.Bold = true
		}
	}
	origIndent := block.Indent
	block.Indent = 0
	b.w = b.limit
	if block.Kind == "table" {
		b.w = mapTableWidth(block, b.limit)
	}
	ed := FromOwnedDocument(&document.Document{Blocks: []*document.Block{block}})
	ed.mapTablePreview = block.Kind == "table"
	ed.mapMathPreview = block.Kind == "math"
	if block.Kind == "numbered" && b.listNumber > 0 {
		ed.mapListNumber = b.listNumber
		ed.mapListIndent = origIndent
	}
	o.Width = b.w + 92 // native page layout reserves 46 DIP on each side
	o.FontSize = 14
	ed.Paginate(o, 1e8)
	b.editor = ed
	b.span, _ = ed.blockSpan(block.ID)
	b.span.X = ed.padding
	if block.Kind == "math" && len(ed.rows) == 1 && ed.rows[0].math != nil {
		r := ed.rows[0]
		rawW := max(12, r.math.W*r.mathScale)
		rawH := max(16, (r.math.Ascent+r.math.Descent)*r.mathScale)
		b.rawW, b.rawH = rawW, rawH
		b.w, b.h = rawW, rawH
		if rawH > mapMaxVisualHeight {
			b.fitScale = mapMaxVisualHeight / rawH
			b.w = max(24, float32(math.Ceil(float64(rawW*b.fitScale))))
			b.h = mapMaxVisualHeight
		}
		return
	}
	if block.Kind == "table" && len(ed.decorations) > 0 {
		for _, d := range ed.decorations {
			if d.table.W > 0 && d.table.H > 0 {
				b.span.Y = d.table.Y
				b.span.H = d.table.H
				break
			}
		}
	}
	if block.Kind == "callout" {
		b.calloutTone = block.Color
		for _, d := range ed.decorations {
			if d.kind == "callout" && d.rect.W > 0 && d.rect.H > 0 {
				b.span = d.rect
				b.w = d.rect.W
				break
			}
		}
	}
	if (block.Kind == "paragraph" || isList(block.Kind) || block.Kind == "quote" || block.Kind == "toggle") && len(ed.rows) == 1 && ed.rows[0].layout != nil {
		r := ed.rows[0]
		_, h := r.layout.Size()
		var right float32
		for _, line := range r.layout.Lines() {
			right = max(right, line.Rect.X+line.Rect.W)
		}
		if block.Kind == "numbered" {
			label := listNumber(max(1, r.number), origIndent) + "."
			lw, _ := ui.ShapeText(label, ui.Font{Family: bodyFamily(ed.fontFamily), Size: 16}, 0).Size()
			if need := lw + 8; need > r.x-ed.padding {
				b.span.X = r.x - need
			}
		}
		b.span.Y = r.y
		b.span.H = h
		b.w = max(12, float32(math.Ceil(float64(r.x-b.span.X+right))))
	}
	mermaidVisual := false
	if isMermaidBlock(block) && len(ed.rows) == 1 {
		row := ed.rows[0]
		if row.mermaid != nil && row.mermaidHidden {
			mermaidVisual = true
			mw, mh := row.mermaid.W*row.mermaidScale, row.mermaid.H*row.mermaidScale
			b.span = ui.Rect{
				X: row.rect.X + (row.rect.W-mw)/2,
				Y: row.rect.Y + row.mermaidTop,
				W: max(24, mw),
				H: max(24, mh),
			}
			b.w = b.span.W
		}
	}
	if (isWhiteboardBlock(block) || isWhiteboardEmbed(block)) && len(ed.rows) == 1 && ed.rows[0].boardRect.W > 0 {
		b.span = ed.rows[0].boardRect
		b.w = b.span.W
	}
	if isPDFEmbed(block) && len(ed.rows) == 1 && ed.rows[0].pdfRect.W > 0 {
		b.span = ed.rows[0].pdfRect
		b.w = b.span.W
	}
	if block.Kind == "image" && !isWhiteboardEmbed(block) && !isPDFEmbed(block) && len(ed.rows) == 1 {
		row := ed.rows[0]
		if fig := row.picture; fig != nil && !ed.captionVisible(block) {
			b.span = fig.bounds
			b.w = b.span.W
		}
	}
	b.span.W = b.w
	b.span = mapAnnotationBounds(ed, b.span)
	b.w, b.h = b.span.W, max(b.span.H, 18)
	b.rawW, b.rawH = b.w, b.h

	isVisual := block.Kind == "image" || isWhiteboardBlock(block) || isWhiteboardEmbed(block) || mermaidVisual
	switch {
	case isVisual:
		if b.rawH > mapMaxVisualHeight {
			b.fitScale = mapMaxVisualHeight / b.rawH
			b.w = max(24, float32(math.Ceil(float64(b.rawW*b.fitScale))))
			b.h = mapMaxVisualHeight
		}
	case block.Kind == "table":
		if b.h > mapMaxTableHeight {
			b.h = mapMaxTableHeight
			b.heightClipped = true
			b.clipped = true
			if b.moreHint == "" {
				b.moreHint = i18n.T("内容较长")
			}
		} else if b.clipped && b.moreHint != "" {
			b.h = min(mapMaxTableHeight, b.h+20)
		}
	case block.Kind == "columns":
		if b.h > mapMaxVisualHeight {
			b.h = mapMaxVisualHeight
			b.heightClipped = true
			b.clipped = true
			if b.moreHint == "" {
				b.moreHint = i18n.T("内容较长")
			}
		}
	default:
		if b.h > mapMaxProseHeight {
			b.h = mapMaxProseHeight
			b.heightClipped = true
			b.clipped = true
			if b.moreHint == "" {
				b.moreHint = i18n.T("内容较长")
			}
		}
	}
}

// Hand-drawn marks may extend outside the text layout on all four sides.
func mapAnnotationBounds(ed *Editor, ink ui.Rect) ui.Rect {
	for _, r := range ed.rows {
		if r.layout == nil || r.leaf.Block == nil {
			continue
		}
		for _, run := range annotationRuns(r.leaf.Block.Runs) {
			look, ok := lookOf(run.variant)
			if !ok {
				continue
			}
			for _, line := range annotationLineRects(r, run, look, r.size) {
				if line.W < 1 {
					continue
				}
				mark := annotationBox(look, line, r.size)
				// Include the hand-drawn wobble and the pen's half width.
				edge := max(float32(3), r.size*.16)
				x, y := r.x+mark.X, r.y+mark.Y
				l, t := min(ink.X, x-edge), min(ink.Y, y-edge)
				ink = ui.Rect{X: l, Y: t, W: max(ink.X+ink.W, x+mark.W+edge) - l, H: max(ink.Y+ink.H, y+mark.H+edge) - t}
			}
		}
	}
	return ink
}

func (b *mapBody) paintClipOverlay(p *ui.Painter, r ui.Rect, showBadge bool, t *ui.Theme) {
	if b.heightClipped && b.h > 24 {
		fadeBg := t.Background.Mix(t.Text, .016)
		if t.Dark {
			fadeBg = t.Background.Mix(t.Text, .028)
		}
		if b.isCode {
			fadeBg = t.Background.Mix(t.Text, .045)
		} else if b.calloutTone != "" && b.editor != nil {
			if look, ok := b.editor.page.Callouts[b.calloutTone]; ok {
				fadeBg = look.Fill
			}
		}
		fadeH := min(b.h*0.42, 26)
		fadeRect := ui.Rect{X: r.X, Y: r.Y + b.h - fadeH, W: b.w, H: fadeH}
		p.FillGradient(fadeRect, ui.LinearGradient{
			Angle: 180,
			Stops: []ui.GradientStop{
				{Color: fadeBg.Alpha(0), At: 0},
				{Color: fadeBg.Alpha(0.86), At: 0.58},
				{Color: fadeBg, At: 1},
			},
		}, 0)
	}
	if showBadge && b.moreHint != "" {
		if b.moreLayout == nil {
			b.moreLayout = ui.ShapeText(b.moreHint, ui.Font{Family: "system-ui", Size: 10, Weight: 500}, 0)
		}
		mw, mh := b.moreLayout.Size()
		pillW := mw + 10
		pillH := mh + 4
		if pillW <= b.w-8 && pillH <= b.h-8 {
			pill := ui.Rect{X: r.X + b.w - pillW - 4, Y: r.Y + b.h - pillH - 3, W: pillW, H: pillH}
			p.Fill(pill, t.Surface.Alpha(0.94), pillH/2)
			p.Stroke(pill, t.Border.Alpha(0.75), pillH/2, 0.75)
			p.TextLayout(b.moreLayout, pill.X+5, pill.Y+2, t.TextMuted)
		}
	}
}

func (b *mapBody) paint(p *ui.Painter, r ui.Rect, z float32, showBadge bool, t *ui.Theme) {
	if b.code != nil {
		p.Clip(r, 0, func() {
			p.Transform(ui.Scaling(z, z), r.X, r.Y, func() {
				_, ch := b.codeCaption.Size()
				p.TextLayout(b.codeCaption, r.X+4, r.Y+2, t.TextMuted)
				p.TextLayout(b.code, r.X+4, r.Y+ch+5, t.Text)
				if b.clipped {
					b.paintClipOverlay(p, r, showBadge, t)
				}
			})
		})
		return
	}
	if b.editor == nil {
		return
	}
	ed := b.editor
	fit := b.fitScale
	if fit <= 0 {
		fit = 1
	}
	if b.source.Kind == "math" && len(ed.rows) == 1 && ed.rows[0].math != nil {
		row := ed.rows[0]
		scale := row.mathScale * z * fit
		p.Transform(ui.Scaling(scale, scale), r.X, r.Y, func() { ed.paintFormula(p, *row.math, r.X, r.Y+row.math.Ascent, t) })
		return
	}
	p.Clip(r, 0, func() {
		scale := z * fit
		paintH := b.h / fit
		drawW := b.rawW * fit
		offX := float32(0)
		if b.w > drawW && fit < 1 {
			offX = (b.w - drawW) * z / 2
		}
		p.Transform(ui.Scaling(scale, scale), r.X+offX, r.Y, func() {
			ed.PaintPage(p, ui.Rect{X: r.X + offX - b.span.X, Y: r.Y, W: ed.width, H: paintH}, b.span.Y, b.span.Y+paintH, t)
		})
		if b.clipped {
			p.Transform(ui.Scaling(z, z), r.X, r.Y, func() {
				b.paintClipOverlay(p, r, showBadge, t)
			})
		}
	})
}

func (m *MindMap) sync(e *Editor, o Options) {
	doc := e.Doc
	key := layoutPageKey{has: true, page: o.Page, fontFamily: o.FontFamily, lineHeight: o.LineHeight, column: m.wrapWidth}
	reuse := m.theme == *o.Theme && sameLayoutPageKey(key, m.style) && m.syncHeadingsOnly == m.headingsOnly
	if m.doc == doc && m.revision == e.Revision() && reuse && m.importSource == doc.ImportSource && m.docTitle == o.DocTitle {
		return
	}
	if m.cache == nil {
		m.cache = make(map[string]*cachedMapBlock, len(doc.Blocks))
	}
	if m.doc == doc && reuse && m.importSource == doc.ImportSource && m.docTitle == o.DocTitle && m.itemIndex != nil {
		if change, ok := e.TextChangeSince(m.revision); ok {
			if leaf, exists := e.Engine.LeafByID(change.ID); exists {
				if idx, known := m.itemIndex[leaf.Owner]; known {
					if blk := e.blockByID(leaf.Owner); blk != nil {
						if m.tryIncrementalUpdate(blk, idx, reuse, o) {
							m.revision = e.Revision()
							m.layoutRevision++
							return
						}
					}
				}
			}
		}
	}

	m.layoutRevision++
	m.syncHeadingsOnly = m.headingsOnly
	m.theme = *o.Theme
	m.style = key
	m.style.page = clonePage(o.Page)
	m.docTitle = o.DocTitle
	if m.importSource != doc.ImportSource {
		m.importSource = doc.ImportSource
		m.importHints = restoredListHints(doc)
	}
	m.doc, m.revision = doc, e.Revision()
	if m.bodies == nil {
		m.bodies = make(map[string]*mapBody)
	} else {
		clear(m.bodies)
	}
	if m.itemIndex == nil {
		m.itemIndex = make(map[string]int, len(doc.Blocks))
	} else {
		clear(m.itemIndex)
	}
	if m.ownerMap == nil {
		m.ownerMap = make(map[string]string)
	} else {
		clear(m.ownerMap)
	}
	m.items = m.items[:0]

	fallbackTitle := strings.TrimSpace(o.DocTitle)
	start := 0
	m.rootEmpty = false
	if len(doc.Blocks) > 0 && doc.Blocks[0].Kind == "heading" {
		start = 1
		rootBlk := doc.Blocks[0]
		m.itemIndex[rootBlk.ID] = -1
		if strings.TrimSpace(rootBlk.Text()) == "" {
			m.rootEmpty = true
			if fallbackTitle == "" {
				fallbackTitle = i18n.T("未命名")
			}
			m.title = mindmap.Item{ID: rootBlk.ID, Text: fallbackTitle}
		} else {
			m.title = m.buildItem(rootBlk, 0, 0, reuse, o.Theme)
			// The root is always visible.
			if body := m.bodies[m.title.ID]; body != nil && body.editor == nil && body.code == nil {
				body.measure(o)
				m.title.Width, m.title.Height = body.w, body.h
			}
		}
	} else {
		if fallbackTitle == "" {
			fallbackTitle = i18n.T("思维导图")
		}
		m.title = mindmap.Item{ID: "__map_root__", Text: fallbackTitle}
	}

	lastVisible := m.title.ID
	var headings, lists, counters []int
	for _, b := range doc.Blocks[start:] {
		listNum := 0
		switch b.Kind {
		case "bullet", "task":
			counters = counters[:min(len(counters), b.Indent)]
		case "numbered":
			counters = counters[:min(len(counters), b.Indent+1)]
			for len(counters) <= b.Indent {
				counters = append(counters, 0)
			}
			counters[b.Indent]++
			listNum = counters[b.Indent]
		default:
			counters = nil
		}
		if shouldSkipMapBlock(b) || (m.headingsOnly && b.Kind != "heading") {
			m.itemIndex[b.ID] = -2
			m.ownerMap[b.ID] = lastVisible
			if b.Kind == "paragraph" && b.Indent == 0 {
				lists = nil
			}
			continue
		}
		if b.Kind == "heading" {
			for len(headings) > 0 && headings[len(headings)-1] >= b.Level {
				headings = headings[:len(headings)-1]
			}
		}
		depth := len(headings)
		placement, known := m.importHints[b.ID]
		if b.MapList != nil {
			placement, known = *b.MapList, true
		}
		level, listItem, body := b.Indent, isList(b.Kind), false
		if known {
			level, listItem, body = placement.Depth, placement.Item, !placement.Item
		}
		if listItem {
			for len(lists) > 0 && lists[len(lists)-1] >= level {
				lists = lists[:len(lists)-1]
			}
			depth += len(lists)
			lists = append(lists, level)
		} else if body || (b.Kind != "heading" && b.Indent > 0) {
			for len(lists) > 0 && (lists[len(lists)-1] > level || (!body && lists[len(lists)-1] == level)) {
				lists = lists[:len(lists)-1]
			}
			depth += len(lists)
		} else {
			lists = nil
		}
		m.itemIndex[b.ID] = len(m.items)
		m.items = append(m.items, m.buildItem(b, depth, listNum, reuse, o.Theme))
		lastVisible = b.ID
		if b.Kind == "heading" {
			headings = append(headings, b.Level)
		}
	}
	if len(m.cache) > len(doc.Blocks)*2+64 {
		for id := range m.cache {
			if _, ok := m.itemIndex[id]; !ok {
				delete(m.cache, id)
			}
		}
	}
}

func (m *MindMap) tryIncrementalUpdate(blk *document.Block, idx int, reuse bool, o Options) bool {
	skippedNow := shouldSkipMapBlock(blk)
	if idx == -2 {
		return skippedNow
	}
	if idx == -1 {
		emptyNow := strings.TrimSpace(blk.Text()) == ""
		if emptyNow != m.rootEmpty {
			return false
		}
		if emptyNow {
			return true
		}
		m.title = m.buildItem(blk, 0, 0, reuse, o.Theme)
		if body := m.bodies[m.title.ID]; body != nil && body.editor == nil && body.code == nil {
			body.measure(o)
			m.title.Width, m.title.Height = body.w, body.h
		}
		return true
	}
	if skippedNow || idx < 0 || idx >= len(m.items) {
		return false
	}
	depth := m.items[idx].Depth
	listNum := 0
	if entry := m.cache[blk.ID]; entry != nil {
		listNum = entry.listNumber
	}
	item := m.buildItem(blk, depth, listNum, reuse, o.Theme)
	if body := m.bodies[blk.ID]; body != nil {
		body.measure(o)
		item.Width, item.Height = body.w, body.h
	}
	m.items[idx] = item
	return true
}

// Decode intentionally omits transient parse hints. Recover them once from the
// retained Markdown; bind to the currently opened IDs, never to label strings.
func restoredListHints(doc *document.Document) map[string]document.ListPlacement {
	if doc.ImportSource == "" || len(doc.ImportSource) > 512_000 {
		return nil
	}
	parsed, _ := parseMarkdownForExport(doc.ImportSource)
	bs := parsed.doc.Blocks
	live := doc.Blocks
	if len(live) > len(bs) && len(live) > 0 && live[0].Kind == "heading" && (len(bs) == 0 || bs[0].Kind != "heading") {
		live = live[1:]
	}
	if len(live) > len(bs) && live[len(live)-1].Kind == "paragraph" && live[len(live)-1].Text() == "" {
		live = live[:len(live)-1]
	}
	if len(bs) != len(live) {
		return nil
	}
	hints := map[string]document.ListPlacement{}
	for i, src := range bs {
		if src.Kind != live[i].Kind {
			return nil
		}
		if src.MapList != nil {
			hints[live[i].ID] = *src.MapList
		}
	}
	return hints
}

func (m *MindMap) buildItem(b *document.Block, depth int, listNumber int, reuse bool, t *ui.Theme) mindmap.Item {
	st, mh := blockStamp(b)
	if b != nil && b.Kind == "numbered" {
		mh = fnvMixUint64(mh, uint64(listNumber))
	}
	entry := m.cache[b.ID]
	unchanged := entry != nil && entry.stamp == st && entry.metaHash == mh
	var excerpt string
	var rich bool
	if unchanged {
		excerpt = entry.excerpt
		rich = entry.rich
	} else {
		rich = richMapBlock(b)
		excerpt = BlockExcerpt(b)
		if !rich {
			if text := b.Text(); text != "" {
				excerpt = text
			}
		}
		if entry == nil {
			entry = &cachedMapBlock{}
			m.cache[b.ID] = entry
		}
		entry.stamp, entry.metaHash, entry.listNumber = st, mh, listNumber
		entry.excerpt, entry.rich = excerpt, rich
		entry.body = nil
	}
	item := mindmap.Item{ID: b.ID, Text: excerpt, Depth: depth}
	if !rich {
		if entry != nil {
			entry.body = nil
		}
		return item
	}
	var body *mapBody
	if reuse && entry.body != nil {
		body = entry.body
	} else {
		limit := m.wrapWidth
		if limit <= 0 {
			limit = 35 * 14
		}
		body = &mapBody{limit: limit, w: limit, h: 32, fitScale: 1}
		entry.body = body
	}
	body.source = b
	body.listNumber = listNumber
	item.CodeBlock = b.Kind == "code" && b.Language != "mermaid" && !isWhiteboardBlock(b)
	m.bodies[b.ID] = body
	item.Image = b.Kind == "image" || b.Kind == "image_ref"
	item.Panel = item.CodeBlock ||
		b.Kind == "table" ||
		item.Image ||
		b.Kind == "callout" ||
		b.Kind == "columns" ||
		b.Kind == "raw" ||
		isWhiteboardBlock(b) ||
		(b.Kind == "code" && b.Language == "mermaid")
	item.Width, item.Height = body.w, body.h
	item.Draw = func(p *ui.Painter, r ui.Rect, z float32) { m.paintBody(body, p, r, z, t) }
	return item
}

// During a gesture each visible rich block is composited from its own texture.
// At rest the normal vector painter renders at the exact final scale.
func (m *MindMap) paintBody(b *mapBody, p *ui.Painter, r ui.Rect, z float32, t *ui.Theme) {
	showBadge := m.zoom >= 0.50
	if !m.moving || (b.editor == nil && b.code == nil) {
		b.paint(p, r, z, showBadge, t)
		return
	}
	if b.layer == nil {
		scale := max(.5, m.zoom) * p.Scale()
		w, h := int(math.Ceil(float64(b.w*scale))), int(math.Ceil(float64(b.h*scale)))
		if w <= 0 || h <= 0 || w > 4096 || h > 4096 || w*h > 2<<20 {
			b.paint(p, r, z, showBadge, t)
			return
		}
		b.layer = ui.NewCanvas(w, h)
		b.layer.Draw(p, func(cp *ui.Painter) { b.paint(cp, ui.Rect{W: b.w * scale, H: b.h * scale}, scale, showBadge, t) })
	}
	m.layers[b] = true
	p.Image(b.layer.Bitmap(), r, ui.FillBox)
}

func (m *MindMap) updateMotion(c *ui.Context) {
	now := c.Now()
	if m.gestureChanged {
		m.movingUntil = now.Add(140 * time.Millisecond)
		m.gestureChanged = false
	}
	m.moving = now.Before(m.movingUntil)
	if m.moving {
		c.After(m.movingUntil.Sub(now))
		if m.layers == nil {
			m.layers = make(map[*mapBody]bool)
		}
	}
	for b, visible := range m.layers {
		if !m.moving || !visible {
			b.layer = nil
			delete(m.layers, b)
		} else {
			m.layers[b] = false
		}
	}
}

const (
	mapCellX      float32 = 6
	mapCellY      float32 = 3
	mapCellMinRow float32 = 21
	mapCellFont   float32 = 12
)

// Map table columns follow their actual cell text; source widths remain untouched.
func mapTableWidth(b *document.Block, limit float32) float32 {
	if len(b.Children) == 0 {
		return 80
	}
	widths := make([]float32, len(b.Children[0].Children))
	font := ui.Font{Size: mapCellFont, Weight: 600}
	for _, row := range b.Children {
		for j, cell := range row.Children {
			if j >= len(widths) {
				break
			}
			sample := cell.Text()
			if len(sample) > 256 {
				sample = sample[:document.ByteOffset(sample, 64)]
			}
			w, _ := ui.ShapeText(sample, font, 0).Size()
			widths[j] = max(widths[j], max(24+2*mapCellX, float32(math.Ceil(float64(w)))+2*mapCellX+2))
		}
	}
	var total float32
	for _, w := range widths {
		total += w
	}
	// Each preview cell keeps its horizontal padding and at least 24
	// for its text. Shrink only the space above that readable minimum.
	floor := float32(len(widths)) * (24 + 2*mapCellX)
	target := max(floor, min(total, limit))
	if target < total && total > floor {
		for i, w := range widths {
			widths[i] = (24 + 2*mapCellX) + max(0, w-(24+2*mapCellX))*(target-floor)/(total-floor)
		}
	}
	b.Widths = widths
	return max(80, target)
}

func (m *MindMap) pan(dx, dy float32) {
	m.gestureChanged = true
	m.autoFit = false
	m.fitRequested = false
	m.panX += dx
	m.panY += dy
}

func (m *MindMap) zoomAt(x, y, factor float32) {
	if factor <= 0 || m.zoom <= 0 {
		return
	}
	next := min(3.0, max(.10, m.zoom*factor))
	ratio := next / m.zoom
	m.panX = x - (x-m.panX)*ratio
	m.panY = y - (y-m.panY)*ratio
	m.zoom = next
	m.gestureChanged = true
	m.autoFit = false
	m.fitRequested = false
}

func (m *MindMap) gesture(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputZoom:
		m.zoomAt(ev.X, ev.Y, ev.Scale)
		return true
	case ui.InputScroll:
		if ev.Mods&(ui.Cmd|ui.Ctrl) != 0 {
			m.zoomAt(ev.X, ev.Y, float32(math.Exp(-float64(ev.DY)*.008)))
		} else if ev.Mods&ui.Shift != 0 && ev.DX == 0 {
			m.pan(-ev.DY, 0)
		} else {
			m.pan(-ev.DX, -ev.DY)
		}
		return true
	}
	return false
}

func (m *MindMap) locate(v ui.Rect) {
	r := m.tree.Target
	if r.W <= 0 && r.H <= 0 {
		return
	}
	const margin = float32(20)
	x, y := m.panX+r.X*m.zoom, m.panY+r.Y*m.zoom
	w, h := r.W*m.zoom, r.H*m.zoom
	if w > v.W-2*margin {
		if x > margin || x+w < v.W-margin {
			m.panX = margin - r.X*m.zoom
		}
	} else if x < margin || x+w > v.W-margin {
		m.panX = (v.W-w)/2 - r.X*m.zoom
	}
	if h > v.H-2*margin {
		if y > margin || y+h < v.H-margin {
			m.panY = margin - r.Y*m.zoom
		}
	} else if y < margin || y+h > v.H-margin {
		m.panY = (v.H-h)/2 - r.Y*m.zoom
	}
}

func mapControlButton(c *ui.Context, key string, icon *ui.SVG, tip string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Size(26, 26).Radius(6).Center().Label(tip).Tooltip(tip).KeepFocus().Cursor(ui.CursorPointer)
	color := t.Text.Alpha(.68)
	if b.Hovered() {
		b.Background(t.Text.Alpha(.06))
		color = t.Text.Alpha(.9)
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(15).TextColor(color) })
	return b
}

func mapLayoutSwitch(c *ui.Context, both bool) bool {
	selected := 0
	if both {
		selected = 1
	}
	t := c.Theme()
	parts := ui.SegmentedBase(c, &selected, 2)
	parts.Track.Label(i18n.T("导图布局")).AlignItems(ui.Stretch).Padding(2).Radius(7).Background(t.Surface).Border(1, t.Border).Children(func() {
		for i, icon := range []*ui.SVG{iconMapOneSide, iconMapBothSides} {
			name := i18n.T("单侧布局")
			if i == 1 {
				name = i18n.T("双侧布局")
			}
			segment := parts.Segment(i).Size(27, 20).Radius(5).Label(name).Tooltip(name)
			ink := t.TextMuted
			if i == selected {
				fill := t.Background
				if t.Dark {
					fill = t.SurfacePressed
				}
				segment.Background(fill).Shadow(0, 1, 2, 0, t.Text.Alpha(.14))
				ink = t.Text
			}
			segment.Children(func() { ui.Icon(c, icon).FontSize(16).TextColor(ink) })
		}
	})
	return selected == 1
}

// View lays out a mygo-native-ui map inside a native two-axis scroll view.
// Switching between document blocks reveals their map nodes, even under a fold.
func (m *MindMap) View(c *ui.Context, e *Editor, o Options, width float32, colors []ui.Color, bothSides bool, setBothSides func(bool), closeTip string, onClose func()) {
	if m.zoom == 0 {
		m.zoom = 1
		m.autoFit = true
	}
	// Like StackMind's max-width:35em, this is a wrapping ceiling, not a
	// node width. Leave room in the dock for the root and branch connectors.
	share := float32(.68)
	if bothSides {
		share = .50
	}
	m.wrapWidth = max(80, min(35*14, float32(math.Floor(float64((width-12)*share)))))
	m.sync(e, o)
	caretOwner := e.Engine.LeafAt(e.Caret).Owner
	if mapped, ok := m.ownerMap[caretOwner]; ok {
		caretOwner = mapped
	}
	if caretOwner == "" {
		caretOwner = m.title.ID
	}
	reveal := caretOwner != m.caretOwner || e.Caret != m.caret
	m.caretOwner, m.caret = caretOwner, e.Caret
	if reveal || m.selected == "" {
		m.selected = caretOwner
	}
	id := m.selected
	if !m.foldInitialized && len(m.items) > 0 {
		m.foldInitialized = true
		if len(m.items) > 80 {
			m.collapseBranches(true)
		}
	}
	locate := m.locatePending
	m.locatePending = false
	focusID := m.focusID
	m.focusID = ""
	t := o.Theme
	ui.Column(c.Key("map-pane")).Width(width).HeightPercent(100).MinWidth(0).MinHeight(0).Clip().Children(func() {
		controls := ui.Row(c.Key("map-controls")).Height(36).Padding(0, 8, 0, 12).Gap(2).AlignItems(ui.Center)
		controls.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Line(r.X, r.Y+r.H-0.5, r.X+r.W, r.Y+r.H-0.5, 1, t.Border.Alpha(.45))
		})
		controls.Children(func() {
			if width >= 320 {
				ui.Text(c, i18n.T("思维导图")).Grow(1).MinWidth(0).SingleLine().FontSize(12).FontWeight(500).TextColor(t.TextMuted)
			} else {
				ui.Spacer(c)
			}
			if next := mapLayoutSwitch(c.Key("map-layout"), bothSides); next != bothSides {
				bothSides = next
				setBothSides(bothSides)
				locate = true
				c.After(0)
			}
			if m.tree.HasFoldable() {
				if m.tree.HasFolded() {
					if mapControlButton(c, "map-fold", iconMapExpand, i18n.T("全部展开")).Clicked() {
						m.UnfoldAll()
						locate = true
					}
				} else {
					if mapControlButton(c, "map-fold", iconMapCollapse, i18n.T("折叠分支")).Clicked() {
						m.CollapseBranches()
						locate = true
					}
				}
			}
			if mapControlButton(c, "map-minus", iconMinusLine, i18n.T("缩小")).Clicked() {
				m.zoomAt(m.viewW/2, m.viewH/2, 1/1.2)
			}
			if width >= 270 {
				pct := fmt.Sprintf("%d%%", int(math.Round(float64(m.zoom*100))))
				zb := ui.ButtonBase(c.Key("map-zoom-reset")).Height(24).PaddingX(5).Radius(5).Center().
					Label(i18n.T("重置缩放")).Tooltip(i18n.T("重置缩放 (100%)")).KeepFocus().Cursor(ui.CursorPointer)
				if zb.Hovered() {
					zb.Background(t.Text.Alpha(.06))
				}
				zb.Children(func() { ui.Text(c, pct).FontSize(11).FontWeight(500).TextColor(t.TextMuted).NoWrap() })
				if zb.Clicked() {
					m.autoFit = false
					m.fitRequested = false
					m.zoom = 1
					locate = true
					c.After(0)
				}
			}
			if mapControlButton(c, "map-plus", iconPlus, i18n.T("放大")).Clicked() {
				m.zoomAt(m.viewW/2, m.viewH/2, 1.2)
			}
			if mapControlButton(c, "map-fit", iconMapCenter, i18n.T("居中并适应画布")).Clicked() {
				m.autoFit = true
				m.fitRequested = true
				reveal = true
			}
			if onClose != nil {
				tip := closeTip
				if tip == "" {
					tip = i18n.T("关闭思维导图")
				}
				if mapControlButton(c, "map-close", iconClose, tip).Label(i18n.T("关闭思维导图")).Clicked() {
					onClose()
				}
			}
		})

		canvasBg := t.Background.Mix(t.Text, .016)
		if t.Dark {
			canvasBg = t.Background.Mix(t.Text, .028)
		}
		sc := ui.Box(c.Key("map-viewport")).Grow(1).MinHeight(0).MinWidth(0).Background(canvasBg).Clip().Cursor(ui.CursorGrab).Label(i18n.T("思维导图画布"))
		v := sc.LayoutBounds()
		m.viewW, m.viewH = v.W, v.H
		sc.HandleInput(m.gesture)
		if m.focusRoot != "" && sc.Shortcut(0, ui.KeyEscape) {
			m.setFocusRoot("")
			c.After(0)
		}
		viewRoot, viewItems, crumbs := m.viewTree()
		sc.ContextMenu(func(menu *ui.Menu) {
			m.buildCanvasMenu(menu, viewItems, bothSides, setBothSides, c)
		})
		if dx, dy, held := sc.Dragged(); held && (dx != 0 || dy != 0) {
			m.pan(dx, dy)
			sc.Cursor(ui.CursorGrabbing)
		}
		if sc.DoubleClicked() {
			m.autoFit = true
			m.fitRequested = true
			reveal = true
			c.After(0)
		}
		sc.Draw(func(p *ui.Painter, r ui.Rect) {
			if m.zoom < 0.45 || r.W <= 0 || r.H <= 0 {
				return
			}
			step := max(float32(16), 22*m.zoom)
			dotColor := t.Text.Alpha(.055)
			ox := float32(math.Mod(float64(m.panX), float64(step)))
			if ox < 0 {
				ox += step
			}
			oy := float32(math.Mod(float64(m.panY), float64(step)))
			if oy < 0 {
				oy += step
			}
			for y := r.Y + oy; y < r.Y+r.H; y += step {
				for x := r.X + ox; x < r.X+r.W; x += step {
					p.Fill(ui.Rect{X: x - .6, Y: y - .6, W: 1.2, H: 1.2}, dotColor, .6)
				}
			}
		})
		m.updateMotion(c)
		preparedThisFrame := 0
		moreToPrepare := false
		sc.Children(func() {
			tree := mindmap.View(c.Key("map-tree"), &m.tree, viewRoot, viewItems, mindmap.Options{
				LayoutRevision: m.layoutRevision, Colors: colors, MaxWidth: m.wrapWidth, Locate: locate, BothSides: bothSides, Zoom: m.zoom, Selected: id, FocusID: focusID, Reveal: reveal, Pan: m.pan,
				Visible: ui.Rect{X: -m.panX, Y: -m.panY, W: v.W, H: v.H},
				Prepare: func(i int) bool {
					item := &viewItems[i]
					body := m.bodies[item.ID]
					if body == nil || !body.needsMeasure(o) {
						return false
					}
					isSelected := item.ID == id
					if m.zoom < 0.30 && !isSelected {
						return false
					}
					if preparedThisFrame >= mapPrepareBudget && !isSelected {
						moreToPrepare = true
						return false
					}
					body.measure(o)
					item.Width, item.Height = body.w, body.h
					if fullIdx, ok := m.itemIndex[item.ID]; ok && fullIdx >= 0 && fullIdx < len(m.items) {
						m.items[fullIdx].Width, m.items[fullIdx].Height = body.w, body.h
					}
					preparedThisFrame++
					return true
				},
				Chosen: func(id string) {
					if id != "" {
						m.selected = id
						e.RevealBlock(id)
						c.After(0)
					}
				},
				Navigate: func(targetID string) {
					if targetID == "" {
						return
					}
					m.selected = targetID
					m.focusID = targetID
					m.locatePending = true
					e.ScrollToBlock(targetID)
					e.flashBlock(targetID)
					c.After(0)
				},
				NodeMenu: func(menu *ui.Menu, nodeID string) {
					m.buildNodeMenu(menu, c, e, nodeID, viewItems)
				},
				FoldLabel: i18n.T("折叠"), UnfoldLabel: i18n.T("展开"),
			})
			if moreToPrepare {
				c.After(0)
			}
			if m.autoFit && v.W > 0 && v.H > 0 && m.tree.Width > 0 && m.tree.Height > 0 {
				fit := min(1, min(v.W/m.tree.Width, v.H/m.tree.Height))
				floor := float32(0.55)
				if m.fitRequested {
					floor = 0.20
				}
				next := max(floor, fit)
				if next != m.zoom {
					m.zoom = next
					c.After(0)
				}
				m.panX = (v.W - m.tree.Width*next) / 2
				m.panY = (v.H - m.tree.Height*next) / 2
				if fit < floor {
					m.locate(v)
				}
			} else if v.W > 0 && (reveal || locate) {
				m.locate(v)
			}
			tree.Absolute().Left(0).Top(0).TransformOrigin(0, 0).Transform(ui.Scaling(m.zoom, m.zoom).Then(ui.Translation(m.panX, m.panY)))
			if len(crumbs) > 0 {
				m.focusBreadcrumbs(c, t, crumbs)
			}
			if len(m.items) == 0 && e.Blank() {
				ui.Column(c.Key("map-empty-hint")).Absolute().Left(16).Right(16).Bottom(24).AlignItems(ui.Center).Children(func() {
					ui.Text(c, i18n.T("输入标题、列表或段落，实时生成思维导图")).FontSize(12).TextColor(t.TextMuted).TextAlign(ui.Center)
				})
			}
		})
	})
}
