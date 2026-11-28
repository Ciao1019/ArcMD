// Package document owns ArcMD's versioned rich document model and editing
// transactions. It has no dependency on the window or drawing toolkit.
package document

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

// CurrentVersion is the format this ArcMD writes. Version 2 added the color
// of callouts, version 3 the color and highlight of text, version 4 folded
// headings and list items, cell backgrounds, merged cells and callout
// emoji, version 5 the page's icon and cover, version 6 scripts, image widths
// and footnotes, version 7 hand-drawn text annotations, version 8 toggle
// blocks and the word goal, version 9 keyboard key marks, version 10 ordered
// list start numbers, version 11 inline images and image heights. Older
// documents are read and migrated in memory, and written back as the current
// version only when edited.
const CurrentVersion = 11

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

// Annotations are the hand-drawn marks text may carry, after Opensource UI's
// AnnotatedText: drawn over the text, except a highlight, drawn under it.
var Annotations = []string{"wavy", "circle", "highlight", "underline", "line", "dotted", "double",
	"strike", "cross", "arrow", "bracket", "box"}

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
	NoMath    bool   `json:"noMath,omitempty"` // Literal dollar signs from Markdown escapes.
	Script    string `json:"script,omitempty"` // "sup" or "sub" (version 6).
	Bold      bool   `json:"bold,omitempty"`
	Italic    bool   `json:"italic,omitempty"`
	Underline bool   `json:"underline,omitempty"`
	Strike    bool   `json:"strike,omitempty"`
	Code      bool   `json:"code,omitempty"`
	Kbd       bool   `json:"kbd,omitempty"` // Keyboard key mark (version 9).
	Link      string `json:"link,omitempty"`
	// Color and Highlight name a text color and a highlight, one of
	// TextColors and TextHighlights (version 3).
	Color     string `json:"color,omitempty"`
	Highlight string `json:"highlight,omitempty"`
	// Annotation is a hand-drawn mark, one of Annotations, in
	// AnnotationColor, one of TextColors, or its own color when ""
	// (version 7).
	Annotation      string `json:"annotation,omitempty"`
	AnnotationColor string `json:"annotationColor,omitempty"`
}

// InlineImagePlaceholder is the single UTF-16 object replacement character
// carried by a Run that represents an inline image (version 11).
const InlineImagePlaceholder = "\ufffc"

// InlineImage describes a picture embedded inline within a text block (version
// 11). Keeping it as a comparable value field on Run (outside Style) keeps Run
// slices value-comparable and prevents typed text next to an image from
// inheriting image attributes.
type InlineImage struct {
	URL    string  `json:"url,omitempty"`
	Alt    string  `json:"alt,omitempty"`
	Width  float32 `json:"width,omitempty"`
	Height float32 `json:"height,omitempty"`
}

func (img InlineImage) IsZero() bool { return img == (InlineImage{}) }

type Run struct {
	Text  string      `json:"text"`
	Style Style       `json:"style,omitempty"`
	Image InlineImage `json:"image,omitzero"`
}

// IsImage reports whether this run holds an inline image.
func (r Run) IsImage() bool { return r.Image.URL != "" }

// InlineImageRun creates a canonical single-placeholder Run for an inline image.
func InlineImageRun(img InlineImage, style Style) Run {
	return Run{Text: InlineImagePlaceholder, Style: style, Image: img}
}

// ListPlacement retains view-only Markdown container information when the
// editor flattens a list. Native documents retain the original ImportSource.
type ListPlacement struct {
	Depth int
	Item  bool // an otherwise empty item represented by its content block
}

// InlineComment preserves an inline HTML comment ("<!-- ... -->") hidden from
// rendered prose at UTF-16 Offset within a leaf block's text so edits elsewhere
// in the block never drop the comment on Markdown export (version 9).
type InlineComment struct {
	Offset int    `json:"offset"`
	Text   string `json:"text"`
}

type Block struct {
	MapList     *ListPlacement  `json:"-"`
	ImageWidth  float32         `json:"imageWidth,omitempty"`  // Preferred DIP width; zero is natural.
	ImageHeight float32         `json:"imageHeight,omitempty"` // Preferred DIP height; zero is natural (version 11).
	NoteLabel   string          `json:"noteLabel,omitempty"`   // Footnote definition label (version 6).
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Runs        []Run           `json:"runs,omitempty"`
	Comments    []InlineComment `json:"comments,omitempty"` // Preserved inline HTML comments (version 9).
	Level       int             `json:"level,omitempty"`
	Indent      int             `json:"indent,omitempty"`
	Start       int             `json:"start,omitempty"`
	Checked     bool            `json:"checked,omitempty"`
	Language    string          `json:"language,omitempty"`
	URL         string          `json:"url,omitempty"`
	Align       string          `json:"align,omitempty"`
	Widths      []float32       `json:"widths,omitempty"`
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
	// Goal is how many words the writer means the page to reach, 0 for
	// none (version 8).
	Goal int `json:"goal,omitempty"`
	// Frontmatter holds user YAML frontmatter keys preserved across Markdown
	// round-trips outside ArcMD's own page metadata.
	Frontmatter string `json:"frontmatter,omitempty"`
	// ImportSource preserves the original interchange file for recovery.
	ImportSource string `json:"importSource,omitempty"`
}

var (
	idMu   sync.Mutex
	idPool [4080]byte
	idUsed = len(idPool)
)

func NewID() string {
	var raw [12]byte
	idMu.Lock()
	if idUsed+len(raw) > len(idPool) {
		if _, err := rand.Read(idPool[:]); err != nil {
			idMu.Unlock()
			panic(err)
		}
		idUsed = 0
	}
	copy(raw[:], idPool[idUsed:idUsed+len(raw)])
	idUsed += len(raw)
	idMu.Unlock()
	var encoded [24]byte
	hex.Encode(encoded[:], raw[:])
	return string(encoded[:])
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
	out.Comments = append([]InlineComment(nil), b.Comments...)
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
	if d.Version < 5 && (d.Icon != "" || d.Cover != "" || d.CoverY != 0) {
		return fmt.Errorf("document: version %d has no page icon or cover", d.Version)
	}
	var v6 func([]*Block) bool
	v6 = func(blocks []*Block) bool {
		for _, b := range blocks {
			if b != nil {
				if b.ImageWidth != 0 || b.NoteLabel != "" || b.Kind == "footnote" || v6(b.Children) {
					return true
				}
				for _, r := range b.Runs {
					if r.Style.Script != "" || r.Style.NoMath {
						return true
					}
				}
			}
		}
		return false
	}
	if d.Version < 6 && v6(d.Blocks) {
		return errors.New("document: scripts, image widths and footnotes require version 6")
	}
	var v7 func([]*Block) bool
	v7 = func(blocks []*Block) bool {
		for _, b := range blocks {
			if b == nil {
				continue
			}
			for _, r := range b.Runs {
				if r.Style.Annotation != "" || r.Style.AnnotationColor != "" {
					return true
				}
			}
			if v7(b.Children) {
				return true
			}
		}
		return false
	}
	if d.Version < 7 && v7(d.Blocks) {
		return errors.New("document: annotations require version 7")
	}
	var v8 func([]*Block) bool
	v8 = func(blocks []*Block) bool {
		for _, b := range blocks {
			if b != nil && (b.Kind == "toggle" || v8(b.Children)) {
				return true
			}
		}
		return false
	}
	if d.Version < 8 && (v8(d.Blocks) || d.Goal != 0) {
		return errors.New("document: toggle blocks and word goals require version 8")
	}
	var v9 func([]*Block) bool
	v9 = func(blocks []*Block) bool {
		for _, b := range blocks {
			if b == nil {
				continue
			}
			if len(b.Comments) > 0 {
				return true
			}
			for _, r := range b.Runs {
				if r.Style.Kbd {
					return true
				}
			}
			if v9(b.Children) {
				return true
			}
		}
		return false
	}
	if d.Version < 9 && v9(d.Blocks) {
		return errors.New("document: keyboard keys and inline comments require version 9")
	}
	var v10 func([]*Block) bool
	v10 = func(blocks []*Block) bool {
		for _, b := range blocks {
			if b != nil && (b.Start != 0 || v10(b.Children)) {
				return true
			}
		}
		return false
	}
	if d.Version < 10 && v10(d.Blocks) {
		return errors.New("document: ordered list start values require version 10")
	}
	var v11 func([]*Block) bool
	v11 = func(blocks []*Block) bool {
		for _, b := range blocks {
			if b == nil {
				continue
			}
			if b.ImageHeight != 0 {
				return true
			}
			for _, r := range b.Runs {
				if !r.Image.IsZero() {
					return true
				}
			}
			if v11(b.Children) {
				return true
			}
		}
		return false
	}
	if d.Version < 11 && v11(d.Blocks) {
		return errors.New("document: inline images and image heights require version 11")
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

// docHasher serializes document fields into a pooled SHA-256 digest without
// heap-allocating string copies or per-leaf hash state.
type docHasher struct {
	h   hash.Hash
	buf [256]byte
}

var docHasherPool = sync.Pool{
	New: func() any { return &docHasher{h: sha256.New()} },
}

func acquireHasher() *docHasher {
	dh := docHasherPool.Get().(*docHasher)
	dh.h.Reset()
	return dh
}

func releaseHasher(dh *docHasher) {
	docHasherPool.Put(dh)
}

func (dh *docHasher) uint64(v uint64) {
	binary.LittleEndian.PutUint64(dh.buf[:8], v)
	_, _ = dh.h.Write(dh.buf[:8])
}

func (dh *docHasher) float32(v float32) {
	if v == 0 {
		v = 0
	}
	binary.LittleEndian.PutUint32(dh.buf[:4], math.Float32bits(v))
	_, _ = dh.h.Write(dh.buf[:4])
}

func (dh *docHasher) float64(v float64) {
	if v == 0 {
		v = 0
	}
	binary.LittleEndian.PutUint64(dh.buf[:8], math.Float64bits(v))
	_, _ = dh.h.Write(dh.buf[:8])
}

func (dh *docHasher) bool(v bool) {
	if v {
		dh.buf[0] = 1
	} else {
		dh.buf[0] = 0
	}
	_, _ = dh.h.Write(dh.buf[:1])
}

func (dh *docHasher) str(s string) {
	dh.uint64(uint64(len(s)))
	for len(s) > 0 {
		n := copy(dh.buf[:], s)
		_, _ = dh.h.Write(dh.buf[:n])
		s = s[n:]
	}
}

func (dh *docHasher) runs(runs []Run) {
	dh.uint64(uint64(len(runs)))
	for i := range runs {
		r := &runs[i]
		dh.str(r.Text)
		st := &r.Style
		dh.bool(st.NoMath)
		dh.str(st.Script)
		dh.bool(st.Bold)
		dh.bool(st.Italic)
		dh.bool(st.Underline)
		dh.bool(st.Strike)
		dh.bool(st.Code)
		dh.bool(st.Kbd)
		dh.str(st.Link)
		dh.str(st.Color)
		dh.str(st.Highlight)
		dh.str(st.Annotation)
		dh.str(st.AnnotationColor)
		img := &r.Image
		dh.str(img.URL)
		dh.str(img.Alt)
		dh.float32(img.Width)
		dh.float32(img.Height)
	}
}

func (dh *docHasher) comments(comments []InlineComment) {
	dh.uint64(uint64(len(comments)))
	for i := range comments {
		dh.uint64(uint64(comments[i].Offset))
		dh.str(comments[i].Text)
	}
}

func (dh *docHasher) docHeader(d *Document) {
	dh.str(d.Format)
	dh.uint64(uint64(d.Version))
	dh.str(d.Icon)
	dh.str(d.Cover)
	dh.float64(d.CoverY)
	dh.uint64(uint64(d.Goal))
	dh.str(d.Frontmatter)
	dh.str(d.ImportSource)
	dh.uint64(uint64(len(d.Blocks)))
}

func (dh *docHasher) block(b *Block, includeLeafRuns bool, leafOverrides map[string][]Run) {
	if b == nil {
		dh.bool(false)
		return
	}
	dh.bool(true)
	dh.float32(b.ImageWidth)
	dh.float32(b.ImageHeight)
	dh.str(b.NoteLabel)
	dh.str(b.ID)
	dh.str(b.Kind)
	if len(b.Children) > 0 {
		dh.runs(b.Runs)
		if len(b.Comments) > 0 {
			dh.comments(b.Comments)
		}
	} else if includeLeafRuns {
		runs := b.Runs
		if len(leafOverrides) > 0 {
			if orig, ok := leafOverrides[b.ID]; ok {
				runs = orig
			}
		}
		dh.runs(runs)
		if len(b.Comments) > 0 {
			dh.comments(b.Comments)
		}
	}
	dh.uint64(uint64(b.Level))
	dh.uint64(uint64(b.Indent))
	dh.uint64(uint64(b.Start))
	dh.bool(b.Checked)
	dh.str(b.Language)
	dh.str(b.URL)
	dh.str(b.Align)
	dh.uint64(uint64(len(b.Widths)))
	for _, w := range b.Widths {
		dh.float32(w)
	}
	dh.str(b.Color)
	dh.bool(b.Folded)
	dh.str(b.Background)
	dh.uint64(uint64(b.ColSpan))
	dh.uint64(uint64(b.RowSpan))
	dh.str(b.Emoji)
	dh.uint64(uint64(len(b.Children)))
	for _, child := range b.Children {
		dh.block(child, includeLeafRuns, leafOverrides)
	}
}

// RunsDigest returns a 128-bit fingerprint of a block's styled runs without
// allocating or cloning run slices.
func RunsDigest(runs []Run) [16]byte {
	dh := acquireHasher()
	dh.runs(runs)
	sum := dh.h.Sum(dh.buf[:0])
	var out [16]byte
	copy(out[:], sum[:16])
	releaseHasher(dh)
	return out
}

// LeafDigest returns a 128-bit fingerprint of a leaf block's editable content
// (styled runs and inline comments), matching RunsDigest when the block has no
// inline comments.
func LeafDigest(b *Block) [16]byte {
	dh := acquireHasher()
	if b != nil {
		dh.runs(b.Runs)
		if len(b.Comments) > 0 {
			dh.comments(b.Comments)
		}
	}
	sum := dh.h.Sum(dh.buf[:0])
	var out [16]byte
	copy(out[:], sum[:16])
	releaseHasher(dh)
	return out
}

// Digest validates the document and computes a SHA-256 fingerprint over all
// persisted document, block, and run fields without serializing JSON.
func (d *Document) Digest() ([32]byte, error) {
	return d.DigestWithLeafRuns(nil)
}

// DigestWithLeafRuns validates and hashes the document while substituting any
// leaf block runs supplied in leafOverrides, reconstructing pre-edit baselines
// without cloning the document tree.
func (d *Document) DigestWithLeafRuns(leafOverrides map[string][]Run) ([32]byte, error) {
	if err := d.validateWithLeafRuns(leafOverrides); err != nil {
		return [32]byte{}, err
	}
	dh := acquireHasher()
	dh.docHeader(d)
	for _, b := range d.Blocks {
		dh.block(b, true, leafOverrides)
	}
	var out [32]byte
	dh.h.Sum(out[:0])
	releaseHasher(dh)
	return out, nil
}

// SkeletonDigest hashes the document's structure and metadata while omitting
// leaf blocks' Runs, matching the exact subset of fields that text-only edits
// cannot change. Thus Digest(a) == Digest(b) implies identical SkeletonDigests.
func (d *Document) SkeletonDigest() [32]byte {
	dh := acquireHasher()
	dh.docHeader(d)
	for _, b := range d.Blocks {
		dh.block(b, false, nil)
	}
	var out [32]byte
	dh.h.Sum(out[:0])
	releaseHasher(dh)
	return out
}

// SameDocument reports whether a and b hold identical document metadata and
// block trees without reflection or allocations.
func SameDocument(a, b *Document) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Format != b.Format || a.Version != b.Version || a.Icon != b.Icon ||
		a.Cover != b.Cover || a.CoverY != b.CoverY || a.Goal != b.Goal ||
		a.Frontmatter != b.Frontmatter || a.ImportSource != b.ImportSource ||
		len(a.Blocks) != len(b.Blocks) {
		return false
	}
	for i := range a.Blocks {
		if !SameBlock(a.Blocks[i], b.Blocks[i]) {
			return false
		}
	}
	return true
}

// SameBlock reports whether two blocks and their descendants match exactly.
func SameBlock(a, b *Block) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.ImageWidth != b.ImageWidth || a.ImageHeight != b.ImageHeight || a.NoteLabel != b.NoteLabel ||
		a.ID != b.ID || a.Kind != b.Kind ||
		a.Level != b.Level || a.Indent != b.Indent || a.Start != b.Start ||
		a.Checked != b.Checked || a.Language != b.Language ||
		a.URL != b.URL || a.Align != b.Align ||
		a.Color != b.Color || a.Folded != b.Folded ||
		a.Background != b.Background || a.ColSpan != b.ColSpan ||
		a.RowSpan != b.RowSpan || a.Emoji != b.Emoji ||
		len(a.Widths) != len(b.Widths) || len(a.Runs) != len(b.Runs) ||
		len(a.Comments) != len(b.Comments) || len(a.Children) != len(b.Children) {
		return false
	}
	if !slices.Equal(a.Widths, b.Widths) || !slices.Equal(a.Runs, b.Runs) || !slices.Equal(a.Comments, b.Comments) {
		return false
	}
	for i := range a.Children {
		if !SameBlock(a.Children[i], b.Children[i]) {
			return false
		}
	}
	return true
}

func countBlocks(blocks []*Block) int {
	n := 0
	for _, b := range blocks {
		if b == nil {
			continue
		}
		n += 1 + countBlocks(b.Children)
	}
	return n
}

func runsHasText(runs []Run) bool {
	for i := range runs {
		if runs[i].Text != "" {
			return true
		}
	}
	return false
}

func (d *Document) Validate() error {
	return d.validateWithLeafRuns(nil)
}

func (d *Document) validateWithLeafRuns(leafOverrides map[string][]Run) error {
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
	if d.Goal < 0 || d.Goal > 1000000 {
		return errors.New("document: invalid word goal")
	}
	ids := make(map[string]struct{}, countBlocks(d.Blocks))
	var visit func(*Block, string, int) error
	visit = func(b *Block, parent string, depth int) error {
		if b == nil || depth > 24 {
			return errors.New("document: invalid block nesting")
		}
		if b.ID == "" {
			return errors.New("document: missing or duplicate block ID")
		}
		if _, dup := ids[b.ID]; dup {
			return errors.New("document: missing or duplicate block ID")
		}
		ids[b.ID] = struct{}{}
		if b.Level < 0 || b.Level > 6 || b.Indent < 0 || b.Indent > 12 {
			return errors.New("document: invalid block attributes")
		}
		if b.Start < 0 || b.Start > 999999 || (b.Start != 0 && b.Kind != "numbered") {
			return errors.New("document: invalid list start")
		}
		if b.Align != "" && b.Align != "left" && b.Align != "center" && b.Align != "right" {
			return errors.New("document: invalid alignment")
		}
		if b.Color != "" && (b.Kind != "callout" || !validCalloutColor(b.Color)) {
			return errors.New("document: invalid block color")
		}
		if b.Folded && b.Kind != "heading" && b.Kind != "bullet" && b.Kind != "numbered" && b.Kind != "task" && b.Kind != "toggle" {
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
		if !(b.ImageWidth >= 0 && b.ImageWidth <= 10000) || b.ImageWidth != 0 && b.Kind != "image" {
			return errors.New("document: invalid image width")
		}
		if !(b.ImageHeight >= 0 && b.ImageHeight <= 10000) || b.ImageHeight != 0 && b.Kind != "image" {
			return errors.New("document: invalid image height")
		}
		if b.Kind == "footnote" && (b.NoteLabel == "" || len(b.NoteLabel) > 1000 || strings.ContainsAny(b.NoteLabel, "[]\n\r\t ")) || b.Kind != "footnote" && b.NoteLabel != "" {
			return errors.New("document: invalid footnote label")
		}
		runs := b.Runs
		if len(leafOverrides) > 0 && len(b.Children) == 0 {
			if orig, ok := leafOverrides[b.ID]; ok {
				runs = orig
			}
		}
		for _, r := range runs {
			if r.Style.Script != "" && r.Style.Script != "sup" && r.Style.Script != "sub" {
				return errors.New("document: invalid script")
			}
			if r.Style.Color != "" && !oneOf(r.Style.Color, TextColors) || r.Style.Highlight != "" && !oneOf(r.Style.Highlight, TextHighlights) {
				return errors.New("document: invalid text color")
			}
			if r.Style.Annotation != "" && !oneOf(r.Style.Annotation, Annotations) ||
				r.Style.AnnotationColor != "" && (r.Style.Annotation == "" || !oneOf(r.Style.AnnotationColor, TextColors)) {
				return errors.New("document: invalid annotation")
			}
			if !r.Image.IsZero() {
				if len(b.Children) > 0 || !AllowsInlineComments(b.Kind) {
					return errors.New("document: inline image not allowed in block")
				}
				if r.Image.URL == "" || r.Text != InlineImagePlaceholder ||
					!(r.Image.Width >= 0 && r.Image.Width <= 10000) ||
					!(r.Image.Height >= 0 && r.Image.Height <= 10000) {
					return errors.New("document: invalid inline image")
				}
			}
		}
		if len(b.Comments) > 0 {
			if len(b.Children) > 0 || !AllowsInlineComments(b.Kind) {
				return errors.New("document: invalid inline comments")
			}
			textLen := Len(blockText(runs))
			for _, c := range b.Comments {
				if c.Offset < 0 || c.Offset > textLen || !ValidInlineComment(c.Text) {
					return errors.New("document: invalid inline comment")
				}
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
			if len(b.Widths) != 0 && len(b.Widths) != len(b.Children) {
				return errors.New("document: invalid column widths")
			}
			for _, width := range b.Widths {
				if width <= 0 || width > 10000 {
					return errors.New("document: invalid column width")
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
		case "paragraph", "heading", "quote", "bullet", "numbered", "task", "code", "callout", "image", "rule", "raw", "math", "footnote", "toggle":
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
			return validSpansWithOverrides(b, leafOverrides)
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

// AdvanceListCounters updates ordered-list counters across sibling blocks and
// returns the 1-based number for b when b.Kind == "numbered".
func AdvanceListCounters(b *Block, counters []int) (next []int, num int) {
	if b == nil {
		return nil, 0
	}
	switch b.Kind {
	case "bullet", "task":
		return counters[:min(len(counters), b.Indent)], 0
	case "numbered":
		counters = counters[:min(len(counters), b.Indent+1)]
		for len(counters) <= b.Indent {
			counters = append(counters, 0)
		}
		if b.Start > 0 {
			counters[b.Indent] = b.Start
		} else {
			counters[b.Indent]++
		}
		return counters, counters[b.Indent]
	default:
		return nil, 0
	}
}

// ListNumbers computes the displayed 1-based number for every block in blocks
// (0 for blocks that are not numbered list items).
func ListNumbers(blocks []*Block) []int {
	out := make([]int, len(blocks))
	var counters []int
	for i, b := range blocks {
		counters, out[i] = AdvanceListCounters(b, counters)
	}
	return out
}

// PreviousListNumber returns the resolved 1-based number of the most recent
// preceding numbered item at blocks[index]'s indent level within the same parent
// slice, and reports whether only deeper list items lie between them.
func PreviousListNumber(blocks []*Block, index int) (prevNum int, adjacent bool) {
	if index <= 0 || index >= len(blocks) || blocks[index] == nil || blocks[index].Kind != "numbered" {
		return 0, false
	}
	indent := blocks[index].Indent
	nums := ListNumbers(blocks[:index])
	onlyDeeper := true
	for i := index - 1; i >= 0; i-- {
		b := blocks[i]
		if b == nil {
			continue
		}
		isListItem := b.Kind == "bullet" || b.Kind == "numbered" || b.Kind == "task"
		if !isListItem {
			onlyDeeper = false
			continue
		}
		if b.Indent < indent {
			break
		}
		if b.Indent > indent {
			continue
		}
		if b.Kind == "numbered" {
			return nums[i], onlyDeeper
		}
		onlyDeeper = false
	}
	return 0, false
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
			out = append(out, Run{Text: run.Text[ByteOffset(run.Text, a):ByteOffset(run.Text, z)], Style: run.Style, Image: run.Image})
		}
		position += n
	}
	return out
}

func Compact(runs []Run) []Run {
	if len(runs) == 0 {
		return nil
	}
	clean := true
	for i, run := range runs {
		if run.Text == "" || (i > 0 && runs[i-1].Style == run.Style && runs[i-1].Image.IsZero() && run.Image.IsZero()) {
			clean = false
			break
		}
	}
	if clean {
		return slices.Clone(runs)
	}
	var out []Run
	for i := 0; i < len(runs); {
		if runs[i].Text == "" {
			i++
			continue
		}
		if !runs[i].Image.IsZero() {
			out = append(out, runs[i])
			i++
			continue
		}
		style := runs[i].Style
		j := i + 1
		nonEmpty := 1
		for j < len(runs) && runs[j].Image.IsZero() && (runs[j].Text == "" || runs[j].Style == style) {
			if runs[j].Text != "" {
				nonEmpty++
			}
			j++
		}
		if nonEmpty == 1 {
			out = append(out, Run{Text: runs[i].Text, Style: style})
		} else {
			var sb strings.Builder
			total := 0
			for k := i; k < j; k++ {
				total += len(runs[k].Text)
			}
			sb.Grow(total)
			for k := i; k < j; k++ {
				sb.WriteString(runs[k].Text)
			}
			out = append(out, Run{Text: sb.String(), Style: style})
		}
		i = j
	}
	return out
}

type Leaf struct {
	Block      *Block
	Parent     *[]*Block
	Index      int
	Owner      string
	Start, End int
	// ByteStart and ByteEnd address the same leaf in PlainText's UTF-8 string.
	ByteStart, ByteEnd int
}

func (d *Document) Leaves() []Leaf {
	out := make([]Leaf, 0, len(d.Blocks))
	position, bytePosition := 0, 0
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
			text := block.Text()
			end, byteEnd := position+Len(text), bytePosition+len(text)
			out = append(out, Leaf{Block: block, Parent: blocks, Index: i, Owner: top, Start: position, End: end, ByteStart: bytePosition, ByteEnd: byteEnd})
			position = end + 1
			bytePosition = byteEnd + 1
		}
	}
	walk(&d.Blocks, "", nil)
	return out
}

func (d *Document) PlainText() string {
	// Walked directly rather than through Leaves: search rereads the text
	// of an open page as it changes, and on a long page the leaf list and
	// the slice of parts would be most of that work.
	var out strings.Builder
	first := true
	var walk func([]*Block, map[*Block]bool)
	walk = func(blocks []*Block, covered map[*Block]bool) {
		for _, block := range blocks {
			if covered[block] {
				continue
			}
			if len(block.Children) > 0 {
				inner := covered
				if block.Kind == "table" {
					inner = CoveredCells(block)
				}
				walk(block.Children, inner)
				continue
			}
			if !first {
				out.WriteByte('\n')
			}
			first = false
			for _, run := range block.Runs {
				out.WriteString(run.Text)
			}
		}
	}
	walk(d.Blocks, nil)
	return out.String()
}

// HasKind reports whether any block of the document, at any depth, is of
// the kind, without building the leaf list.
func (d *Document) HasKind(kind string) bool {
	var walk func([]*Block) bool
	walk = func(blocks []*Block) bool {
		for _, block := range blocks {
			if block.Kind == kind || walk(block.Children) {
				return true
			}
		}
		return false
	}
	return walk(d.Blocks)
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
	return validSpansWithOverrides(table, nil)
}

func validSpansWithOverrides(table *Block, leafOverrides map[string][]Run) error {
	hasMerged := false
	for _, row := range table.Children {
		for _, cell := range row.Children {
			if cell.ColSpan > 1 || cell.RowSpan > 1 {
				hasMerged = true
				break
			}
		}
		if hasMerged {
			break
		}
	}
	if !hasMerged {
		return nil
	}
	taken := make(map[[2]int]struct{})
	for r, row := range table.Children {
		for c, cell := range row.Children {
			if _, ok := taken[[2]int{r, c}]; ok {
				cellRuns := cell.Runs
				if len(leafOverrides) > 0 {
					if orig, has := leafOverrides[cell.ID]; has {
						cellRuns = orig
					}
				}
				if cell.ColSpan > 1 || cell.RowSpan > 1 || runsHasText(cellRuns) {
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
					key := [2]int{rr, cc}
					if rr != r || cc != c {
						if _, ok := taken[key]; ok {
							return errors.New("document: overlapping merged cells")
						}
					}
					taken[key] = struct{}{}
				}
			}
		}
	}
	return nil
}

// AllowsInlineComments reports whether a leaf block of the given kind can carry
// preserved inline HTML comments.
func AllowsInlineComments(kind string) bool {
	switch kind {
	case "code", "raw", "math", "rule", "image", "table", "row", "columns", "column":
		return false
	default:
		return true
	}
}

// ValidInlineComment reports whether s is a single well-formed HTML comment
// ("<!-- ... -->") with no embedded closing delimiter.
func ValidInlineComment(s string) bool {
	return len(s) >= 7 && strings.HasPrefix(s, "<!--") && strings.HasSuffix(s, "-->") && !strings.Contains(s[4:len(s)-3], "-->")
}

// SplitCommentsAt partitions inline comments at local UTF-16 offset split
// without duplicating comments on the boundary: offsets <= split stay in left,
// and offsets > split move to right (rebased so split becomes 0).
func SplitCommentsAt(comments []InlineComment, split int) (left, right []InlineComment) {
	if len(comments) == 0 {
		return nil, nil
	}
	for _, c := range comments {
		if c.Offset <= split {
			left = append(left, c)
		} else {
			right = append(right, InlineComment{Offset: c.Offset - split, Text: c.Text})
		}
	}
	return left, right
}

func runeU16Len(r rune) int {
	if r >= 0x10000 {
		return 2
	}
	return 1
}

func commonPrefixSuffixU16(before, after string) (prefU16, suffU16 int) {
	bi, ai := 0, 0
	for bi < len(before) && ai < len(after) {
		br, bn := utf8.DecodeRuneInString(before[bi:])
		ar, an := utf8.DecodeRuneInString(after[ai:])
		if br != ar {
			break
		}
		bi += bn
		ai += an
		prefU16 += runeU16Len(br)
	}
	bEnd, aEnd := len(before), len(after)
	for bEnd > bi && aEnd > ai {
		br, bn := utf8.DecodeLastRuneInString(before[bi:bEnd])
		ar, an := utf8.DecodeLastRuneInString(after[ai:aEnd])
		if br != ar {
			break
		}
		bEnd -= bn
		aEnd -= an
		suffU16 += runeU16Len(br)
	}
	return prefU16, suffU16
}

// MapBlockRunText applies change to each run of b for which filter returns true
// (or all runs when filter is nil) and shifts b.Comments so every inline
// comment stays anchored and within valid UTF-16 bounds.
func MapBlockRunText(b *Block, filter func(Run) bool, change func(string) string) {
	if b == nil {
		return
	}
	runStart := 0
	changed := false
	for i := range b.Runs {
		oldText := b.Runs[i].Text
		oldLen := Len(oldText)
		if !b.Runs[i].Image.IsZero() || (filter != nil && !filter(b.Runs[i])) {
			runStart += oldLen
			continue
		}
		newText := change(oldText)
		if newText == oldText {
			runStart += oldLen
			continue
		}
		changed = true
		newLen := Len(newText)
		b.Runs[i].Text = newText
		if len(b.Comments) > 0 {
			delta := newLen - oldLen
			pre, suf := commonPrefixSuffixU16(oldText, newText)
			a, z := pre, oldLen-suf
			for ci := range b.Comments {
				off := b.Comments[ci].Offset
				switch {
				case off <= runStart+a:
				case off >= runStart+z:
					b.Comments[ci].Offset = off + delta
				default:
					localOff := off - runStart
					prefixText := oldText[:ByteOffset(oldText, localOff)]
					mapped := Len(change(prefixText))
					b.Comments[ci].Offset = runStart + max(0, min(newLen, mapped))
				}
			}
		}
		runStart += newLen
	}
	if changed && len(b.Comments) > 0 {
		totalLen := Len(b.Text())
		for ci := range b.Comments {
			b.Comments[ci].Offset = max(0, min(totalLen, b.Comments[ci].Offset))
		}
	}
}

// ShiftComments updates inline comment offsets when local UTF-16 range [a, z)
// is replaced by inserted UTF-16 code units, producing a block of maxLen units.
func ShiftComments(comments []InlineComment, a, z, inserted, maxLen int) []InlineComment {
	if len(comments) == 0 {
		return nil
	}
	delta := inserted - (z - a)
	var out []InlineComment
	for _, c := range comments {
		if z > a && c.Offset > a && c.Offset < z {
			continue
		}
		if c.Offset > a || (z > a && c.Offset >= z) {
			c.Offset += delta
		}
		c.Offset = max(0, min(maxLen, c.Offset))
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SliceComments returns inline comments inside local UTF-16 range [a, z],
// rebased so offset a becomes 0.
func SliceComments(comments []InlineComment, a, z int) []InlineComment {
	if len(comments) == 0 || z < a {
		return nil
	}
	var out []InlineComment
	for _, c := range comments {
		if c.Offset >= a && c.Offset <= z {
			out = append(out, InlineComment{Offset: c.Offset - a, Text: c.Text})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// MergeComments appends right's comments shifted by leftLen onto left.
func MergeComments(left []InlineComment, leftLen int, right []InlineComment) []InlineComment {
	if len(left) == 0 && len(right) == 0 {
		return nil
	}
	out := make([]InlineComment, 0, len(left)+len(right))
	out = append(out, left...)
	for _, c := range right {
		out = append(out, InlineComment{Offset: c.Offset + leftLen, Text: c.Text})
	}
	return out
}

// SplitCommentsByNewline distributes inline comments across '\n'-separated
// lines of text, rebasing each comment to its destination line's start.
func SplitCommentsByNewline(text string, comments []InlineComment) [][]InlineComment {
	type seg struct{ start, end int }
	var segs []seg
	pos := 0
	for {
		nl := strings.IndexByte(text, '\n')
		if nl < 0 {
			segs = append(segs, seg{pos, pos + Len(text)})
			break
		}
		end := pos + Len(text[:nl])
		segs = append(segs, seg{pos, end})
		pos = end + 1
		text = text[nl+1:]
	}
	out := make([][]InlineComment, len(segs))
	for _, c := range comments {
		idx := len(segs) - 1
		for i, s := range segs {
			if c.Offset <= s.end {
				idx = i
				break
			}
		}
		off := max(0, min(segs[idx].end-segs[idx].start, c.Offset-segs[idx].start))
		out[idx] = append(out[idx], InlineComment{Offset: off, Text: c.Text})
	}
	return out
}
