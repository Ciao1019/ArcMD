// Package document owns ArcMD's versioned rich document model and editing
// transactions. It has no dependency on the window or drawing toolkit.
package document

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// CurrentVersion is the format this ArcMD writes. Version 2 added the color
// of callouts, version 3 the color and highlight of text, version 4 folded
// headings and list items, cell backgrounds, merged cells and callout
// emoji, version 5 the page's icon and cover. Older documents are read and
// migrated in memory, and written back as the current version only when
// edited.
const CurrentVersion = 5

// CoverGradients are the covers drawn rather than pictured, as
// "gradient:name" in Document.Cover.
var CoverGradients = []string{"dawn", "sea", "forest", "dusk", "sand", "ink"}

// CalloutColors are the colors a callout may have, after GitHub's alerts so
// Markdown keeps them: "" is a note, the default.
var CalloutColors = []string{"", "green", "purple", "yellow", "red"}

// TextColors are the colors text may have; TextHighlights the colors it
// may be marked with, light before strong.
var (
	TextColors     = []string{"gray", "red", "orange", "yellow", "green", "blue", "purple"}
	TextHighlights = []string{"light-gray", "light-red", "light-orange", "light-yellow", "light-green", "light-blue", "light-purple",
		"medium-gray", "gray", "red", "orange", "yellow", "green", "blue", "purple"}
)

func oneOf(value string, set []string) bool {
	for _, v := range set {
		if v == value {
			return true
		}
	}
	return false
}

func validCalloutColor(color string) bool {
	for _, c := range CalloutColors {
		if c == color {
			return true
		}
	}
	return false
}

type Style struct {
	Bold      bool   `json:"bold,omitempty"`
	Italic    bool   `json:"italic,omitempty"`
	Underline bool   `json:"underline,omitempty"`
	Strike    bool   `json:"strike,omitempty"`
	Code      bool   `json:"code,omitempty"`
	Link      string `json:"link,omitempty"`
	// Color and Highlight name a text color and a highlight, one of
	// TextColors and TextHighlights (version 3).
	Color     string `json:"color,omitempty"`
	Highlight string `json:"highlight,omitempty"`
}

type Run struct {
	Text  string `json:"text"`
	Style Style  `json:"style,omitempty"`
}

type Block struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Runs     []Run     `json:"runs,omitempty"`
	Level    int       `json:"level,omitempty"`
	Indent   int       `json:"indent,omitempty"`
	Checked  bool      `json:"checked,omitempty"`
	Language string    `json:"language,omitempty"`
	URL      string    `json:"url,omitempty"`
	Align    string    `json:"align,omitempty"`
	Widths   []float32 `json:"widths,omitempty"`
	// Color is a callout's color, one of CalloutColors (version 2).
	Color string `json:"color,omitempty"`
	// Folded hides what a heading or list item holds (version 4).
	Folded bool `json:"folded,omitempty"`
	// Background is a table cell's fill, one of TextHighlights; ColSpan
	// and RowSpan merge it with the cells right of and below it, which then
	// stay empty and out of the text (version 4).
	Background string `json:"background,omitempty"`
	ColSpan    int    `json:"colspan,omitempty"`
	RowSpan    int    `json:"rowspan,omitempty"`
	// Emoji is a callout's icon in place of its color's (version 4).
	Emoji    string   `json:"emoji,omitempty"`
	Children []*Block `json:"children,omitempty"`
}

type Document struct {
	Format  string   `json:"format"`
	Version int      `json:"version"`
	Blocks  []*Block `json:"blocks"`
	// Icon is the page's icon, an emoji above its title; Cover the band
	// above both: "gradient:name" of CoverGradients, or a picture beside
	// the document, as an image block's URL (version 5).
	Icon  string `json:"icon,omitempty"`
	Cover string `json:"cover,omitempty"`
	// CoverY is which part of a picture cover taller than its band shows:
	// 0 its middle, -1 its top, 1 its bottom.
	CoverY float64 `json:"coverY,omitempty"`
	// ImportSource preserves the original interchange file for recovery.
	ImportSource string `json:"importSource,omitempty"`
}

func NewID() string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value[:])
}

func NewBlock(kind, text string) *Block {
	return &Block{ID: NewID(), Kind: kind, Runs: []Run{{Text: text}}}
}

func New() *Document {
	return &Document{Format: "arcmd", Version: CurrentVersion, Blocks: []*Block{NewBlock("paragraph", "")}}
}

func (b *Block) Text() string {
	if len(b.Runs) == 1 {
		return b.Runs[0].Text
	}
	var out strings.Builder
	for _, run := range b.Runs {
		out.WriteString(run.Text)
	}
	return out.String()
}

func (b *Block) Clone() *Block {
	out := *b
	out.Runs = append([]Run(nil), b.Runs...)
	out.Widths = append([]float32(nil), b.Widths...)
	if b.Children != nil {
		out.Children = make([]*Block, len(b.Children))
	}
	for i, child := range b.Children {
		out.Children[i] = child.Clone()
	}
	return &out
}

func (d *Document) Clone() *Document {
	out := *d
	out.Blocks = make([]*Block, len(d.Blocks))
	for i, block := range d.Blocks {
		out.Blocks[i] = block.Clone()
	}
	return &out
}

func Decode(text string) (*Document, error) {
	var d Document
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d); err != nil {
		return nil, fmt.Errorf("document: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("document: trailing data")
	}
	if err := d.migrate(); err != nil {
		return nil, err
	}
	d.linkFiles()
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return &d, nil
}

// linkFiles turns the file blocks older documents may hold into
// paragraphs with a link to the file, as Markdown writes them: ArcMD no
// longer has attachments other than images.
func (d *Document) linkFiles() {
	var walk func([]*Block)
	walk = func(blocks []*Block) {
		for _, b := range blocks {
			if b == nil {
				continue
			}
			if b.Kind == "file" {
				text := b.Text()
				if text == "" {
					text = b.URL
				}
				b.Kind = "paragraph"
				if text != "" {
					b.Runs = []Run{{Text: text, Style: Style{Link: b.URL}}}
				}
				b.URL = ""
			}
			walk(b.Children)
		}
	}
	walk(d.Blocks)
}

// migrate brings a document of an older version to CurrentVersion. A
// version 1 document has no colors; one that claims them is refused rather
// than guessed at.
func (d *Document) migrate() error {
	if d.Format != "arcmd" || d.Version < 1 || d.Version >= CurrentVersion {
		return nil
	}
	// A field an older version did not have means the file is damaged, not
	// newer: refuse it rather than guess.
	var blockColor, textColor func([]*Block) bool
	blockColor = func(blocks []*Block) bool {
		for _, b := range blocks {
			if b != nil && (b.Color != "" || blockColor(b.Children)) {
				return true
			}
		}
		return false
	}
	textColor = func(blocks []*Block) bool {
		for _, b := range blocks {
			if b == nil {
				continue
			}
			for _, r := range b.Runs {
				if r.Style.Color != "" || r.Style.Highlight != "" {
					return true
				}
			}
			if textColor(b.Children) {
				return true
			}
		}
		return false
	}
	if d.Version < 2 && blockColor(d.Blocks) {
		return errors.New("document: version 1 has no block colors")
	}
	if d.Version < 3 && textColor(d.Blocks) {
		return fmt.Errorf("document: version %d has no text colors", d.Version)
	}
	var newer func([]*Block) bool
	newer = func(blocks []*Block) bool {
		for _, b := range blocks {
			if b != nil && (b.Folded || b.Background != "" || b.ColSpan != 0 || b.RowSpan != 0 || b.Emoji != "" || newer(b.Children)) {
				return true
			}
		}
		return false
	}
	if d.Version < 4 && newer(d.Blocks) {
		return fmt.Errorf("document: version %d has no folds, cell styles or emoji", d.Version)
	}
	if d.Icon != "" || d.Cover != "" {
		return fmt.Errorf("document: version %d has no page icon or cover", d.Version)
	}
	d.Version = CurrentVersion
	return nil
}

func (d *Document) Encode() (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	err := encoder.Encode(d)
	return out.String(), err
}

func (d *Document) Validate() error {
	if d.Format != "arcmd" || d.Version != CurrentVersion {
		return fmt.Errorf("document: unsupported format %q version %d", d.Format, d.Version)
	}
	if len(d.Blocks) == 0 {
		return errors.New("document: missing content")
	}
	if len(d.Icon) > 32 || strings.ContainsAny(d.Icon, "\n\r") {
		return errors.New("document: invalid page icon")
	}
	if !ValidCover(d.Cover) || !(d.CoverY >= -1 && d.CoverY <= 1) {
		return errors.New("document: invalid cover")
	}
	ids := make(map[string]bool)
	var visit func(*Block, string, int) error
	visit = func(b *Block, parent string, depth int) error {
		if b == nil || depth > 24 {
			return errors.New("document: invalid block nesting")
		}
		if b.ID == "" || ids[b.ID] {
			return errors.New("document: missing or duplicate block ID")
		}
		ids[b.ID] = true
		if b.Level < 0 || b.Level > 6 || b.Indent < 0 || b.Indent > 12 {
			return errors.New("document: invalid block attributes")
		}
		if b.Align != "" && b.Align != "left" && b.Align != "center" && b.Align != "right" {
			return errors.New("document: invalid alignment")
		}
		if b.Color != "" && (b.Kind != "callout" || !validCalloutColor(b.Color)) {
			return errors.New("document: invalid block color")
		}
		if b.Folded && b.Kind != "heading" && b.Kind != "bullet" && b.Kind != "numbered" && b.Kind != "task" {
			return errors.New("document: only headings and list items fold")
		}
		if (b.Background != "" || b.ColSpan != 0 || b.RowSpan != 0) && b.Kind != "cell" {
			return errors.New("document: cell attributes outside a cell")
		}
		if b.Background != "" && !oneOf(b.Background, TextHighlights) {
			return errors.New("document: invalid cell background")
		}
		if b.ColSpan < 0 || b.RowSpan < 0 || b.ColSpan > 64 || b.RowSpan > 64 {
			return errors.New("document: invalid cell span")
		}
		if b.Emoji != "" && (b.Kind != "callout" || len(b.Emoji) > 32) {
			return errors.New("document: invalid callout emoji")
		}
		for _, r := range b.Runs {
			if r.Style.Color != "" && !oneOf(r.Style.Color, TextColors) || r.Style.Highlight != "" && !oneOf(r.Style.Highlight, TextHighlights) {
				return errors.New("document: invalid text color")
			}
		}
		switch b.Kind {
		case "table":
			if len(b.Children) == 0 || b.Children[0] == nil || len(b.Children[0].Children) == 0 {
				return errors.New("document: empty table")
			}
			cols := len(b.Children[0].Children)
			for _, row := range b.Children {
				if row == nil || row.Kind != "row" || len(row.Children) != cols {
					return errors.New("document: ragged table")
				}
			}
			if len(b.Widths) != 0 && len(b.Widths) != cols {
				return errors.New("document: invalid column widths")
			}
			for _, width := range b.Widths {
				if width <= 0 || width > 10000 {
					return errors.New("document: invalid column width")
				}
			}
		case "row":
			if parent != "table" {
				return errors.New("document: row outside table")
			}
			for _, cell := range b.Children {
				if cell == nil || cell.Kind != "cell" {
					return errors.New("document: invalid table cell")
				}
			}
		case "columns":
			if len(b.Children) < 2 || len(b.Children) > 5 {
				return errors.New("document: invalid columns")
			}
			for _, column := range b.Children {
				if column == nil || column.Kind != "column" {
					return errors.New("document: invalid column")
				}
			}
		case "column":
			if parent != "columns" || len(b.Children) == 0 {
				return errors.New("document: invalid column content")
			}
		case "cell":
			if parent != "row" {
				return errors.New("document: cell outside row")
			}
		case "paragraph", "heading", "quote", "bullet", "numbered", "task", "code", "callout", "image", "rule", "raw", "math":
		default:
			return fmt.Errorf("document: unsupported block %q", b.Kind)
		}
		if b.Kind != "table" && b.Kind != "row" && b.Kind != "columns" && b.Kind != "column" && len(b.Children) != 0 {
			return errors.New("document: text block contains children")
		}
		for _, child := range b.Children {
			if err := visit(child, b.Kind, depth+1); err != nil {
				return err
			}
		}
		// Span checks dereference cells, so validate their structure first.
		if b.Kind == "table" {
			return validSpans(b)
		}
		return nil
	}
	for _, b := range d.Blocks {
		if err := visit(b, "root", 0); err != nil {
			return err
		}
	}
	return nil
}

// ValidCover reports whether a cover is one Document.Cover may hold: none,
// a gradient of CoverGradients, or a relative path beside the document.
func ValidCover(cover string) bool {
	if cover == "" {
		return true
	}
	if name, ok := strings.CutPrefix(cover, "gradient:"); ok {
		return oneOf(name, CoverGradients)
	}
	if len(cover) > 1024 || strings.Contains(cover, ":") || strings.HasPrefix(cover, "/") || strings.ContainsAny(cover, "\n\r\\") {
		return false
	}
	for _, part := range strings.Split(cover, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

// Len and ByteOffset bridge the model's UTF-8 storage and native UTF-16 ranges.
func Len(text string) int {
	n := 0
	for _, r := range text {
		n++
		if r > 0xffff {
			n++
		}
	}
	return n
}
func ByteOffset(text string, at int) int {
	units := 0
	for i, r := range text {
		n := 1
		if r > 0xffff {
			n = 2
		}
		if units+n > at {
			return i
		}
		units += n
	}
	return len(text)
}

func SliceRuns(runs []Run, start, end int) []Run {
	var out []Run
	position := 0
	for _, run := range runs {
		n := Len(run.Text)
		a, z := max(0, start-position), min(n, end-position)
		if z > a {
			out = append(out, Run{Text: run.Text[ByteOffset(run.Text, a):ByteOffset(run.Text, z)], Style: run.Style})
		}
		position += n
	}
	return out
}

func Compact(runs []Run) []Run {
	var out []Run
	for _, run := range runs {
		if run.Text == "" {
			continue
		}
		if len(out) > 0 && out[len(out)-1].Style == run.Style {
			out[len(out)-1].Text += run.Text
		} else {
			out = append(out, run)
		}
	}
	return out
}

type Leaf struct {
	Block      *Block
	Parent     *[]*Block
	Index      int
	Owner      string
	Start, End int
}

func (d *Document) Leaves() []Leaf {
	var out []Leaf
	position := 0
	var walk func(*[]*Block, string, map[*Block]bool)
	walk = func(blocks *[]*Block, owner string, covered map[*Block]bool) {
		for i, block := range *blocks {
			top := owner
			if top == "" {
				top = block.ID
			}
			if covered[block] {
				// A merged-over cell is out of the text.
				continue
			}
			if len(block.Children) > 0 {
				inner := covered
				if block.Kind == "table" {
					inner = CoveredCells(block)
				}
				walk(&block.Children, top, inner)
				continue
			}
			end := position + Len(block.Text())
			out = append(out, Leaf{Block: block, Parent: blocks, Index: i, Owner: top, Start: position, End: end})
			position = end + 1
		}
	}
	walk(&d.Blocks, "", nil)
	return out
}

func (d *Document) PlainText() string {
	var parts []string
	for _, leaf := range d.Leaves() {
		parts = append(parts, leaf.Block.Text())
	}
	return strings.Join(parts, "\n")
}

func Table(rows, columns int) *Block {
	table := NewBlock("table", "")
	table.Runs = nil
	for row := 0; row < max(1, rows); row++ {
		line := &Block{ID: NewID(), Kind: "row"}
		for col := 0; col < max(1, columns); col++ {
			line.Children = append(line.Children, NewBlock("cell", ""))
		}
		table.Children = append(table.Children, line)
	}
	return table
}

func Columns() *Block {
	return &Block{ID: NewID(), Kind: "columns", Children: []*Block{
		{ID: NewID(), Kind: "column", Children: []*Block{NewBlock("paragraph", "")}},
		{ID: NewID(), Kind: "column", Children: []*Block{NewBlock("paragraph", "")}},
	}}
}

// Span is the number of columns and rows a cell spans: at least 1.
func Span(cell *Block) (cols, rows int) {
	return max(1, cell.ColSpan), max(1, cell.RowSpan)
}

// CoveredCells are the cells of table merged into another cell.
func CoveredCells(table *Block) map[*Block]bool {
	var out map[*Block]bool
	for r, row := range table.Children {
		for c, cell := range row.Children {
			cols, rows := Span(cell)
			if cols == 1 && rows == 1 {
				continue
			}
			for rr := r; rr < min(r+rows, len(table.Children)); rr++ {
				for cc := c; cc < min(c+cols, len(table.Children[rr].Children)); cc++ {
					if rr != r || cc != c {
						if out == nil {
							out = map[*Block]bool{}
						}
						out[table.Children[rr].Children[cc]] = true
					}
				}
			}
		}
	}
	return out
}

// validSpans checks a table's merged cells: inside the table, not
// overlapping, and the cells they cover empty.
func validSpans(table *Block) error {
	taken := map[[2]int]bool{}
	for r, row := range table.Children {
		for c, cell := range row.Children {
			if taken[[2]int{r, c}] {
				if cell.ColSpan > 1 || cell.RowSpan > 1 || cell.Text() != "" {
					return errors.New("document: merged cell over content")
				}
				continue
			}
			cols, rows := Span(cell)
			if r+rows > len(table.Children) || c+cols > len(row.Children) {
				return errors.New("document: merged cell outside its table")
			}
			for rr := r; rr < r+rows; rr++ {
				for cc := c; cc < c+cols; cc++ {
					if (rr != r || cc != c) && taken[[2]int{rr, cc}] {
						return errors.New("document: overlapping merged cells")
					}
					taken[[2]int{rr, cc}] = true
				}
			}
		}
	}
	return nil
}
