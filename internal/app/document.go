package app

import (
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io/fs"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/Ciao1019/ArcMD/internal/history"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

const autosaveDelay = 700 * time.Millisecond

// document owns a rich model, its persisted baseline and export preview.
type document struct {
	space *space.Space
	rel   string
	// named is the name this session gave the file from its title; the file
	// keeps following the title while it has that name or 未命名's.
	named            string
	namedFor         string // the title it was named for
	diskText         string
	source           bool
	ed               *editor.Editor
	mindMap          *editor.MindMap
	mapInitialized   bool
	mapFraction      float32
	mapVFraction     float32
	board            *editor.Board
	boardReadOnly    bool
	boardBaseline    [32]byte
	pdf              *editor.PDFViewer
	pdfModNano       int64
	pdfSize          int64
	version          uint64
	edited           time.Time
	saveErr          error
	failedVersion    uint64
	baseline         [32]byte
	baselineSkeleton [32]byte
	baselinePending  bool
	// dirtyRev, dirtyKnown and dirtyNow remember dirty for a revision.
	dirtyRev            uint64
	dirtyVersion        uint64
	dirtyKnown          bool
	dirtyNow            bool
	textDirtyKnown      bool
	skeletonDirty       bool
	textDirty           map[string]bool
	baselineRuns        map[string][16]byte
	baselineOrigRuns    map[string][]model.Run
	loadErr             error
	preview             string
	previewScroll       ui.ScrollState
	previewCaret        int
	previewCaretPending bool
	previewEnteredY     float32
	modeSwitchPending   bool
	modeAt              time.Time
	// conflict is set when the file changed elsewhere while the document
	// has unsaved edits, diskNow holding what the file says; missing when
	// the file went. Saving waits for the user either way (conflictBar).
	conflict bool
	diskNow  string
	missing  bool
	// historyStarted is set once the version the file had before this
	// session is kept; historyOff turns versions off (versions.go).
	historyStarted bool
	historyOff     bool
	// Background autosave state: only touched on the main thread except the
	// buffered saveDone channel written once by the background worker.
	saving    bool
	saveDone  chan asyncSaveResult
	justSaved bool
	// Async image decode cache keyed by document-relative URL.
	imgCache map[string]cachedImage
	// closed is set once the document is no longer resident, cancelling any
	// queued background image decodes and releasing cached bitmaps.
	closed atomic.Bool
	// plain is the text search sees, for plainRev (search.go).
	plain      string
	plainRev   uint64
	plainKnown bool
	// savedWrites counts successful disk writes of this document; notedWrites
	// and savedSeq keep search/footer views fresh while refreshSearch catches up.
	savedWrites uint64
	notedWrites uint64
	savedSeq    uint64
	// typedRev and typedAt are the last revision footers saw and when they
	// first saw it (liveTyping).
	typedRev uint64
	typedAt  time.Time
	footer   pageFooterCache
	// marks are the document's bookmarks resolved to its blocks
	// (bookmarks.go).
	marks docMarks
}

func openDocument(sp *space.Space, rel, text string, preferSource bool) *document {
	if vault.IsPDF(rel) {
		return openPDFDocument(sp, rel, text)
	}
	d := &document{space: sp, rel: rel, diskText: text, source: preferSource}
	if vault.IsWhiteboard(rel) {
		scene, err := whiteboard.Decode(text)
		if err != nil {
			d.loadErr = err
			scene = whiteboard.NewScene()
		}
		d.board = editor.NewBoard(scene)
		d.boardBaseline = scene.Digest()
		d.ed = editor.FromOwnedDocument(model.New())
		d.initLazyBaseline()
		d.source = false
		return d
	}
	var doc *model.Document
	if strings.EqualFold(path.Ext(rel), ".arcmd") {
		var err error
		doc, err = model.Decode(text)
		if err != nil {
			d.loadErr = err
			doc = model.New()
		}
	} else {
		doc = editor.ImportMarkdown(text)
	}
	d.ed = editor.FromOwnedDocument(doc)
	d.initLazyBaseline()
	if preferSource {
		d.preview = editor.ExportMarkdown(d.ed.Doc)
	}
	return d
}
func (d *document) title() string { return vault.Title(d.rel) }
func (d *document) text() string {
	if d.isPDF() {
		return ""
	}
	if d.isWhiteboard() {
		s, _ := encodeWhiteboardFile(d.rel, d.board.SceneSnapshot())
		return s
	}
	return editor.ExportMarkdown(d.ed.Committed())
}

// ensureBodyParagraph gives a newly created heading-only Markdown page an
// empty body paragraph below its title without marking the document dirty.
func (d *document) ensureBodyParagraph() {
	if len(d.ed.Doc.Blocks) == 1 && d.ed.Doc.Blocks[0].Kind == "heading" {
		d.ed.Doc.Blocks = append(d.ed.Doc.Blocks, model.NewBlock("paragraph", ""))
		d.initLazyBaseline()
	}
}

// dirty reports whether the document differs from what was saved. Text-only
// revisions compare only the affected leaf (or stay O(1) when the skeleton is
// already dirty); other changes re-evaluate against the baseline once and
// re-arm leaf-level tracking when the skeleton matches.
func (d *document) dirty() bool {
	if d.loadErr != nil || d.isPDF() {
		return false
	}
	if d.isWhiteboard() {
		if rev := d.board.Revision(); !d.dirtyKnown || rev != d.dirtyRev {
			d.dirtyNow = d.board.Scene.Digest() != d.boardBaseline
			d.dirtyRev, d.dirtyKnown = rev, true
			d.dirtyVersion = d.board.Version()
		}
		return d.dirtyNow
	}
	if rev := d.ed.Revision(); !d.dirtyKnown || rev != d.dirtyRev {
		// Preedit revisions update display indexes but leave committed content
		// unchanged. In particular, saving during composition establishes a
		// clean baseline that subsequent candidate updates must preserve.
		composing := d.ed.Composing()
		if composing && d.dirtyKnown && d.dirtyVersion == d.ed.Version() {
			d.dirtyRev = rev
			return d.dirtyNow
		}
		if change, ok := d.ed.TextChangeSince(d.dirtyRev); ok && !composing && d.dirtyKnown {
			if d.skeletonDirty {
				// Text-only edits cannot restore a modified document skeleton,
				// so dirtyNow remains true without re-hashing the document.
				d.dirtyNow = true
			} else if d.textDirtyKnown {
				current := d.ed.Engine.LeafAt(change.Start).Block
				if saved, exists := d.baselineRuns[change.ID]; exists && saved == model.LeafDigest(current) {
					delete(d.textDirty, change.ID)
				} else {
					if d.textDirty == nil {
						d.textDirty = make(map[string]bool)
					}
					d.textDirty[change.ID] = true
				}
				d.dirtyNow = len(d.textDirty) != 0
			} else {
				d.recomputeDirtySlow()
			}
		} else {
			d.recomputeDirtySlow()
		}
		d.dirtyRev, d.dirtyKnown = rev, true
		d.dirtyVersion = d.ed.Version()
	}
	return d.dirtyNow
}

func (d *document) recomputeDirtySlow() {
	if d.baselinePending && !d.ed.Composing() {
		clear(d.textDirty)
		for id := range d.baselineOrigRuns {
			leaf, ok := d.ed.Engine.LeafByID(id)
			if !ok || model.LeafDigest(leaf.Block) != d.baselineRuns[id] {
				if d.textDirty == nil {
					d.textDirty = make(map[string]bool, max(1, len(d.baselineOrigRuns)))
				}
				d.textDirty[id] = true
			}
		}
		d.dirtyNow = len(d.textDirty) != 0
		d.skeletonDirty = false
		d.textDirtyKnown = true
		return
	}
	baseline := d.ensureBaseline()
	committed := d.ed.Committed()
	digest, err := committed.Digest()
	d.dirtyNow = err != nil || digest != baseline
	clear(d.textDirty)
	if !d.dirtyNow {
		d.skeletonDirty = false
		d.textDirtyKnown = true
		return
	}
	if err == nil && committed.SkeletonDigest() == d.baselineSkeleton {
		d.rebuildTextDirty(committed)
		d.skeletonDirty = false
		d.textDirtyKnown = true
		return
	}
	d.skeletonDirty = true
	d.textDirtyKnown = false
}

func (d *document) rebuildTextDirty(doc *model.Document) {
	if d.textDirty == nil {
		d.textDirty = make(map[string]bool)
	} else {
		clear(d.textDirty)
	}
	var walk func([]*model.Block, map[*model.Block]bool)
	walk = func(blocks []*model.Block, covered map[*model.Block]bool) {
		for _, block := range blocks {
			if covered[block] {
				continue
			}
			if len(block.Children) > 0 {
				inner := covered
				if block.Kind == "table" {
					inner = model.CoveredCells(block)
				}
				walk(block.Children, inner)
				continue
			}
			if saved, ok := d.baselineRuns[block.ID]; !ok || saved != model.LeafDigest(block) {
				d.textDirty[block.ID] = true
			}
		}
	}
	walk(doc.Blocks, nil)
}

func computeBaselineRuns(doc *model.Document, overrides map[string][]model.Run, dst map[string][16]byte) map[string][16]byte {
	if dst == nil {
		dst = make(map[string][16]byte, max(16, len(doc.Blocks)))
	} else {
		clear(dst)
	}
	var walk func([]*model.Block, map[*model.Block]bool)
	walk = func(blocks []*model.Block, covered map[*model.Block]bool) {
		for _, block := range blocks {
			if covered[block] {
				continue
			}
			if len(block.Children) > 0 {
				inner := covered
				if block.Kind == "table" {
					inner = model.CoveredCells(block)
				}
				walk(block.Children, inner)
				continue
			}
			if len(overrides) > 0 {
				if orig, ok := overrides[block.ID]; ok {
					dst[block.ID] = model.RunsDigest(orig)
					continue
				}
			}
			dst[block.ID] = model.LeafDigest(block)
		}
	}
	walk(doc.Blocks, nil)
	return dst
}

// A run comparison is exact for a sequence of text-only edits. Structural
// edits still take the full comparison; saving always verifies the whole model.
func (d *document) initLazyBaseline() {
	d.baselinePending = true
	d.baseline = [32]byte{}
	d.baselineSkeleton = [32]byte{}
	clear(d.baselineRuns)
	clear(d.baselineOrigRuns)
	d.skeletonDirty = false
	clear(d.textDirty)
	d.textDirtyKnown = true
	d.dirtyRev, d.dirtyKnown, d.dirtyNow = d.ed.Revision(), true, false
	d.dirtyVersion = d.ed.Version()
	d.ed.Engine.OnBeforeChange = func() { d.ensureBaseline() }
	d.ed.Engine.OnBeforeLeafChange = d.recordLeafBaseline
}

func (d *document) recordLeafBaseline(id string, runs []model.Run) {
	if !d.baselinePending {
		return
	}
	if leaf, ok := d.ed.Engine.LeafByID(id); ok && len(leaf.Block.Comments) > 0 {
		d.ensureBaseline()
		return
	}
	if _, exists := d.baselineOrigRuns[id]; exists {
		return
	}
	if d.baselineOrigRuns == nil {
		d.baselineOrigRuns = make(map[string][]model.Run, 4)
	}
	if d.baselineRuns == nil {
		d.baselineRuns = make(map[string][16]byte, 4)
	}
	cloned := slices.Clone(runs)
	d.baselineOrigRuns[id] = cloned
	d.baselineRuns[id] = model.RunsDigest(cloned)
}

func (d *document) ensureBaseline() [32]byte {
	if d.baselinePending {
		d.baselinePending = false
		d.ed.Engine.OnBeforeChange = nil
		d.ed.Engine.OnBeforeLeafChange = nil
		d.baseline, _ = d.ed.Doc.DigestWithLeafRuns(d.baselineOrigRuns)
		d.baselineRuns = computeBaselineRuns(d.ed.Doc, d.baselineOrigRuns, d.baselineRuns)
		d.baselineSkeleton = d.ed.Doc.SkeletonDigest()
		clear(d.baselineOrigRuns)
	}
	return d.baseline
}

func (d *document) rememberBaseline(doc *model.Document) {
	d.applyBaselineState(computeBaselineRuns(doc, nil, d.baselineRuns), doc.SkeletonDigest())
}

func (d *document) applyBaselineState(runs map[string][16]byte, skeleton [32]byte) {
	d.baselinePending = false
	d.ed.Engine.OnBeforeChange = nil
	d.ed.Engine.OnBeforeLeafChange = nil
	clear(d.baselineOrigRuns)
	d.baselineRuns = runs
	d.baselineSkeleton = skeleton
	d.skeletonDirty = false
	clear(d.textDirty)
	d.textDirtyKnown = true
	d.dirtyRev, d.dirtyKnown, d.dirtyNow = d.ed.Revision(), true, false
	d.dirtyVersion = d.ed.Version()
}

type asyncSaveResult struct {
	text             string
	baseline         [32]byte
	baselineSkeleton [32]byte
	baselinePending  bool
	baselineRuns     map[string][16]byte
	committed        *model.Document
	unchanged        bool
	arcmd            bool
	board            bool
	historyRecorded  bool
	missing          bool
	conflict         bool
	diskNow          string
	err              error
	version          uint64
	revision         uint64
}

type cachedImage struct {
	modNano        int64
	size           int64
	loading        bool
	pixelsReleased bool
	resource       editor.ImageResource
}

// changed hashes the document and compares it with the saved baseline.
func (d *document) changed() bool {
	if d.isPDF() {
		return false
	}
	if d.isWhiteboard() {
		return d.board.Scene.Digest() != d.boardBaseline
	}
	if d.baselinePending && len(d.baselineOrigRuns) == 0 {
		return false
	}
	prev := d.ensureBaseline()
	digest, err := d.ed.Committed().Digest()
	return err != nil || digest != prev
}

func (d *document) toggleMode() {
	if d.isWhiteboard() || d.isPDF() {
		return
	}
	d.ed.UnmarkText()
	d.modeSwitchPending = true
	d.modeAt = time.Time{}
	if !d.source {
		targetID := d.ed.ActiveOrTopBlockID()
		doc := d.ed.Committed()
		d.preview = editor.ExportMarkdown(doc)
		d.previewCaret = markdownBlockRuneOffset(doc, d.preview, targetID)
		d.previewCaretPending = true
		d.previewEnteredY = -1
		d.source = true
		return
	}
	if d.previewScroll.MaxY > 0 && (d.previewEnteredY < 0 || abs32Doc(d.previewScroll.Y-d.previewEnteredY) > 4) {
		frac := min(max(d.previewScroll.Y/d.previewScroll.MaxY, 0), 1)
		if id := markdownBlockAtFraction(d.ed.Committed(), d.preview, frac); id != "" {
			d.ed.ScrollToBlock(id)
		}
	}
	d.source = false
	d.ed.RequestFocus = true
}

func abs32Doc(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func markdownBlockRuneOffset(doc *model.Document, md, blockID string) int {
	if blockID == "" || md == "" || doc == nil {
		return 0
	}
	parts := editor.ExportMarkdownParts(doc)
	if len(parts) == 0 {
		return 0
	}
	partByID := make(map[string]int, len(parts))
	for i, p := range parts {
		partByID[p.Block] = i
	}
	targetPart := -1
	if idx, ok := partByID[blockID]; ok {
		targetPart = idx
	} else {
		for _, b := range doc.Blocks {
			if idx, ok := partByID[b.ID]; ok {
				targetPart = idx
			}
			if b.ID == blockID {
				break
			}
		}
	}
	if targetPart <= 0 {
		return 0
	}
	byteAt := 0
	for i := 0; i <= targetPart && i < len(parts); i++ {
		pos := strings.Index(md[byteAt:], parts[i].Text)
		if pos < 0 {
			continue
		}
		if i == targetPart {
			return utf8.RuneCountInString(md[:byteAt+pos])
		}
		byteAt += pos + len(parts[i].Text)
	}
	return 0
}

func markdownBlockAtFraction(doc *model.Document, md string, frac float32) string {
	if doc == nil || len(doc.Blocks) == 0 {
		return ""
	}
	parts := editor.ExportMarkdownParts(doc)
	if len(parts) == 0 {
		return ""
	}
	totalRunes := utf8.RuneCountInString(md)
	targetRune := int(float32(totalRunes) * frac)
	byteAt := 0
	chosen := parts[0].Block
	for _, p := range parts {
		pos := strings.Index(md[byteAt:], p.Text)
		if pos < 0 {
			continue
		}
		startRune := utf8.RuneCountInString(md[:byteAt+pos])
		if startRune <= targetRune {
			chosen = p.Block
		} else {
			break
		}
		byteAt += pos + len(p.Text)
	}
	return chosen
}

func (d *document) pollAsyncSave() {
	if !d.saving || d.saveDone == nil {
		return
	}
	select {
	case res := <-d.saveDone:
		d.applyAsyncSave(res)
	default:
	}
}

func (d *document) waitAsyncSave() {
	if !d.saving || d.saveDone == nil {
		return
	}
	res := <-d.saveDone
	d.applyAsyncSave(res)
}

func (d *document) applyAsyncSave(res asyncSaveResult) {
	d.saving = false
	d.saveDone = nil
	switch {
	case res.unchanged:
		d.saveErr = nil
		if res.board {
			if d.isWhiteboard() && d.board.Version() == res.version && d.board.Revision() == res.revision {
				d.dirtyRev, d.dirtyKnown, d.dirtyNow = d.board.Revision(), true, false
				d.dirtyVersion = d.board.Version()
			} else {
				d.dirtyKnown = false
			}
			return
		}
		if res.baselineRuns != nil {
			d.baselineRuns = res.baselineRuns
			d.baselineSkeleton = res.baselineSkeleton
		}
		if d.ed.Committed() == res.committed && d.ed.Version() == res.version && d.ed.Revision() == res.revision {
			d.applyBaselineState(d.baselineRuns, d.baselineSkeleton)
		} else {
			d.dirtyKnown = false
			d.textDirtyKnown = false
			d.skeletonDirty = false
		}
	case res.missing:
		d.missing = true
		d.failedVersion = res.version
	case res.conflict:
		d.conflict, d.diskNow = true, res.diskNow
		d.failedVersion = res.version
	case res.err != nil:
		d.saveErr = res.err
		d.failedVersion = res.version
	default:
		if res.board {
			d.diskText, d.boardBaseline, d.saveErr = res.text, res.baseline, nil
			if res.historyRecorded {
				d.historyStarted = true
			}
			d.conflict, d.missing, d.diskNow = false, false, ""
			if d.isWhiteboard() && d.board.Version() == res.version && d.board.Revision() == res.revision {
				d.dirtyRev, d.dirtyKnown, d.dirtyNow = d.board.Revision(), true, false
				d.dirtyVersion = d.board.Version()
			} else {
				d.dirtyKnown = false
			}
			d.justSaved = true
			return
		}
		if !res.arcmd {
			res.committed.ImportSource = res.text
			if d.ed.Doc != res.committed {
				d.ed.Doc.ImportSource = res.text
			}
			if cur := d.ed.Committed(); cur != res.committed && cur != d.ed.Doc && d.ed.Version() == res.version && d.ed.Revision() == res.revision {
				cur.ImportSource = res.text
			}
		}
		d.diskText, d.baseline, d.saveErr = res.text, res.baseline, nil
		d.savedWrites++
		if res.baselineRuns != nil {
			d.baselineRuns = res.baselineRuns
			d.baselineSkeleton = res.baselineSkeleton
		}
		if res.historyRecorded {
			d.historyStarted = true
		}
		d.conflict, d.missing, d.diskNow = false, false, ""
		if d.ed.Committed() == res.committed && d.ed.Version() == res.version && d.ed.Revision() == res.revision {
			d.applyBaselineState(d.baselineRuns, d.baselineSkeleton)
		} else {
			d.dirtyKnown = false
			d.textDirtyKnown = false
			d.skeletonDirty = false
		}
		if d.source {
			d.preview = d.text()
		}
		d.justSaved = true
	}
}

func (d *document) save() error { return d.write(false) }

// write saves the document. Unless force is set, it refuses to overwrite a
// file that changed since ArcMD last read or wrote it, or to bring back one
// that went: the file watch may be late or absent, so the file itself is
// compared, a cheap read of one document.
func (d *document) pendingDirtyState() (dirty, known bool) {
	if !d.baselinePending || d.ed.Composing() {
		return false, false
	}
	for id, orig := range d.baselineOrigRuns {
		leaf, ok := d.ed.Engine.LeafByID(id)
		if !ok || !slices.Equal(leaf.Block.Runs, orig) {
			return true, true
		}
	}
	return false, true
}

func (d *document) write(force bool) error {
	d.waitAsyncSave()
	if d.loadErr != nil {
		return d.loadErr
	}
	if !force {
		if d.conflict {
			return errConflict
		}
		if d.missing {
			return errMissing
		}
	}
	if d.isPDF() {
		return nil
	}
	if d.isWhiteboard() {
		return d.writeWhiteboard(force)
	}
	var baseline [32]byte
	if dirty, known := d.pendingDirtyState(); known {
		if !dirty && !force {
			d.saveErr = nil
			return nil
		}
	} else {
		prevBaseline := d.ensureBaseline()
		// Fingerprint the committed model without full JSON serialization on clean
		// checks; only .arcmd saves serialize JSON.
		committed := d.ed.Committed()
		var digErr error
		baseline, digErr = committed.Digest()
		changed := digErr != nil || baseline != prevBaseline
		if !changed && !force {
			d.saveErr = nil
			return nil
		}
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
	committed := d.ed.Committed()
	var text string
	var err error
	arcmd := strings.EqualFold(path.Ext(d.rel), ".arcmd")
	if arcmd {
		text, err = committed.Encode()
		if err == nil && d.baselinePending {
			baseline, _ = committed.Digest()
		}
	} else {
		err = committed.Validate()
		if err == nil {
			text = editor.ExportMarkdown(committed)
		}
	}
	if err == nil {
		err = writeFile(d.space, d.rel, text)
	}
	if err != nil {
		d.saveErr = err
		return err
	}
	before := d.diskText
	if !arcmd {
		committed.ImportSource = text
		if d.ed.Doc != committed {
			d.ed.Doc.ImportSource = text
		}
		baseline, _ = committed.Digest()
	}
	d.diskText, d.baseline, d.saveErr = text, baseline, nil
	d.savedWrites++
	if d.source {
		d.preview = d.text()
	}
	d.recordVersion(before)
	d.conflict, d.missing, d.diskNow = false, false, ""
	d.rememberBaseline(committed)
	d.justSaved = true
	return nil
}

func (d *document) writeWhiteboard(force bool) error {
	baseline := d.board.Scene.Digest()
	if baseline == d.boardBaseline && !force {
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
	text, err := encodeWhiteboardFile(d.rel, d.board.Scene)
	if err == nil {
		err = writeFile(d.space, d.rel, text)
	}
	if err != nil {
		d.saveErr = err
		return err
	}
	before := d.diskText
	d.diskText, d.boardBaseline, d.saveErr = text, baseline, nil
	d.recordVersion(before)
	d.conflict, d.missing, d.diskNow = false, false, ""
	d.dirtyRev, d.dirtyKnown, d.dirtyNow = d.board.Revision(), true, false
	d.dirtyVersion = d.board.Version()
	d.justSaved = true
	return nil
}

// viewKey identifies the document's editor view by the editor itself, not
// by its path: a path changes when the file follows the title, and a new
// key would make the view a new element, taking the focus and ending an
// input method's composition mid-word.
func (d *document) viewKey() string {
	if d.isPDF() {
		return fmt.Sprintf("pdf:%p", d.pdf)
	}
	if d.isWhiteboard() {
		return fmt.Sprintf("board:%p", d.board)
	}
	return fmt.Sprintf("editor:%p", d.ed)
}

func (d *document) autosave(c *ui.Context) { d.autosaveWith(c, nil) }

func (d *document) autosaveWith(c *ui.Context, post func(func())) {
	if d.isPDF() {
		return
	}
	d.pollAsyncSave()
	ver := d.ed.Version()
	if d.isWhiteboard() {
		ver = d.board.Version()
	}
	if ver != d.version {
		d.version, d.edited = ver, c.Now()
	}
	if d.saving || d.conflict || d.missing || !d.dirty() || d.saveErr != nil && d.version == d.failedVersion {
		return
	}
	if wait := autosaveDelay - c.Now().Sub(d.edited); wait > 0 {
		c.After(wait)
		return
	}
	if post == nil {
		if d.save() != nil {
			d.failedVersion = d.version
		}
		return
	}
	d.startAsyncAutosave(post)
}

func (d *document) startAsyncAutosave(post func(func())) {
	if d.loadErr != nil {
		return
	}
	if d.isWhiteboard() {
		d.startAsyncWhiteboardAutosave(post)
		return
	}
	committed := d.ed.Committed()
	snapshot := committed.Clone()
	arcmd := strings.EqualFold(path.Ext(d.rel), ".arcmd")
	prevBaseline := d.ensureBaseline()

	spClone := *d.space
	if d.space.ExternalFiles != nil {
		spClone.ExternalFiles = make(map[string]string, len(d.space.ExternalFiles))
		for k, v := range d.space.ExternalFiles {
			spClone.ExternalFiles[k] = v
		}
	}
	rel := d.rel
	expectedDisk := d.diskText
	historyOff := d.historyOff
	needInitialHistory := !d.historyOff && !d.historyStarted && expectedDisk != ""
	fullPath := d.fullPath()
	ver := d.version
	rev := d.ed.Revision()
	ch := make(chan asyncSaveResult, 1)
	d.saving = true
	d.saveDone = ch

	go func() {
		// Run Digest, Validate, ExportMarkdown, disk I/O and baseline hashing off the UI thread.
		preBaseline, digErr := snapshot.Digest()
		if digErr == nil && preBaseline == prevBaseline {
			ch <- asyncSaveResult{
				unchanged:        true,
				baseline:         preBaseline,
				baselineRuns:     computeBaselineRuns(snapshot, nil, nil),
				baselineSkeleton: snapshot.SkeletonDigest(),
				committed:        committed,
				version:          ver,
				revision:         rev,
			}
			post(func() { d.pollAsyncSave() })
			return
		}
		var outText string
		var baseline [32]byte
		var prepErr error
		if arcmd {
			outText, prepErr = snapshot.Encode()
			baseline = preBaseline
		} else {
			prepErr = snapshot.Validate()
			if prepErr == nil {
				outText = editor.ExportMarkdown(snapshot)
				snapshot.ImportSource = outText
				baseline, _ = snapshot.Digest()
			}
		}
		res := asyncSaveResult{
			text:      outText,
			baseline:  baseline,
			committed: committed,
			arcmd:     arcmd,
			version:   ver,
			revision:  rev,
		}
		if prepErr != nil {
			res.err = prepErr
			ch <- res
			post(func() { d.pollAsyncSave() })
			return
		}
		disk, err := readFile(&spClone, rel)
		switch {
		case isMissingDocFile(&spClone, rel, err):
			res.missing = true
		case err != nil:
			res.err = err
		case disk != expectedDisk:
			res.conflict, res.diskNow = true, disk
		default:
			var initialAt time.Time
			if needInitialHistory {
				initialAt = time.Now().Add(-time.Second)
				if info, stErr := os.Stat(fullPath); stErr == nil && info.ModTime().Before(initialAt) {
					initialAt = info.ModTime()
				}
			}
			if wErr := writeFile(&spClone, rel, outText); wErr != nil {
				res.err = wErr
			} else {
				res.baselineRuns = computeBaselineRuns(snapshot, nil, nil)
				res.baselineSkeleton = snapshot.SkeletonDigest()
				if !historyOff {
					if needInitialHistory {
						_ = history.Record(spClone.Root, rel, expectedDisk, initialAt, true)
					}
					_ = history.Record(spClone.Root, rel, outText, time.Now(), false)
					res.historyRecorded = true
				}
			}
		}
		ch <- res
		post(func() { d.pollAsyncSave() })
	}()
}

func (d *document) startAsyncWhiteboardAutosave(post func(func())) {
	snap := d.board.SceneSnapshot()
	prevBaseline := d.boardBaseline

	spClone := *d.space
	if d.space.ExternalFiles != nil {
		spClone.ExternalFiles = make(map[string]string, len(d.space.ExternalFiles))
		for k, v := range d.space.ExternalFiles {
			spClone.ExternalFiles[k] = v
		}
	}
	rel := d.rel
	expectedDisk := d.diskText
	historyOff := d.historyOff
	needInitialHistory := !d.historyOff && !d.historyStarted && expectedDisk != ""
	fullPath := d.fullPath()
	ver := d.version
	rev := d.board.Revision()
	ch := make(chan asyncSaveResult, 1)
	d.saving = true
	d.saveDone = ch

	go func() {
		preBaseline := snap.Digest()
		if preBaseline == prevBaseline {
			ch <- asyncSaveResult{
				unchanged: true,
				board:     true,
				baseline:  preBaseline,
				version:   ver,
				revision:  rev,
			}
			post(func() { d.pollAsyncSave() })
			return
		}
		outText, prepErr := encodeWhiteboardFile(rel, snap)
		res := asyncSaveResult{
			text:     outText,
			baseline: preBaseline,
			board:    true,
			version:  ver,
			revision: rev,
		}
		if prepErr != nil {
			res.err = prepErr
			ch <- res
			post(func() { d.pollAsyncSave() })
			return
		}
		disk, err := readFile(&spClone, rel)
		switch {
		case isMissingDocFile(&spClone, rel, err):
			res.missing = true
		case err != nil:
			res.err = err
		case disk != expectedDisk:
			res.conflict, res.diskNow = true, disk
		default:
			var initialAt time.Time
			if needInitialHistory {
				initialAt = time.Now().Add(-time.Second)
				if info, stErr := os.Stat(fullPath); stErr == nil && info.ModTime().Before(initialAt) {
					initialAt = info.ModTime()
				}
			}
			if wErr := writeFile(&spClone, rel, outText); wErr != nil {
				res.err = wErr
			} else if !historyOff {
				if needInitialHistory {
					_ = history.Record(spClone.Root, rel, expectedDisk, initialAt, true)
				}
				_ = history.Record(spClone.Root, rel, outText, time.Now(), false)
				res.historyRecorded = true
			}
		}
		ch <- res
		post(func() { d.pollAsyncSave() })
	}()
}

// imageDecodeSem bounds concurrent background image decodes so rapid navigation
// across image-heavy documents cannot fan out hundreds of simultaneous decoders.
var (
	imageDecodeSem     = make(chan struct{}, 4)
	activeImageDecodes atomic.Int32
)

func (d *document) close() {
	if d == nil {
		return
	}
	d.closed.Store(true)
	clear(d.imgCache)
	d.imgCache = nil
	if d.pdf != nil {
		d.pdf.Close()
	}
	if d.ed != nil {
		d.ed.ClearImages()
	}
}

// releaseShownImagePixels drops retained CPU RGBA buffers once GPU upload has
// had a chance to consume decoded bitmaps, keeping compressed source bytes in
// Bitmap's remake closure for on-demand restoration if evicted from VRAM.
func (d *document) releaseShownImagePixels(now time.Time) {
	if d == nil || len(d.imgCache) == 0 {
		return
	}
	for url, c := range d.imgCache {
		if c.pixelsReleased || c.loading || c.resource.Bitmap == nil || c.resource.LoadedAt.IsZero() {
			continue
		}
		if now.Sub(c.resource.LoadedAt) >= 200*time.Millisecond {
			c.resource.Bitmap.ReleasePixels()
			c.pixelsReleased = true
			d.imgCache[url] = c
		}
	}
}

func (d *document) loadImage(url string) *ui.Bitmap {
	return d.imageResource(url).Bitmap
}

func (d *document) imageResource(url string) editor.ImageResource {
	return d.imageResourceWith(url, nil)
}

// thumbImageResource decodes local images scaled down to thumbnail dimensions
// so sidebar hover previews never allocate full-resolution multi-megabyte
// bitmaps or multi-frame GIF buffers.
func (d *document) thumbImageResource(url string) editor.ImageResource {
	full, err := d.localImage(url)
	if err != nil {
		problem := editor.ImageInvalid
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			problem = editor.ImageRemote
		}
		return editor.ImageResource{Problem: problem}
	}
	info, err := os.Stat(full)
	if err != nil {
		problem := editor.ImageUnreadable
		if errors.Is(err, os.ErrNotExist) {
			problem = editor.ImageMissing
		}
		return editor.ImageResource{Problem: problem, Local: true}
	}
	modNano, size := info.ModTime().UnixNano(), info.Size()
	if d.imgCache == nil {
		d.imgCache = make(map[string]cachedImage)
	}
	if c, ok := d.imgCache[url]; ok && c.modNano == modNano && c.size == size {
		return c.resource
	}
	res := decodeLocalImageThumb(full)
	d.imgCache[url] = cachedImage{modNano: modNano, size: size, resource: res}
	return res
}

func (d *document) rawImage(url string) image.Image {
	full, err := d.localImage(url)
	if err != nil {
		return nil
	}
	f, err := os.Open(full)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	return img
}

func (d *document) imageResourceWith(url string, post func(func())) editor.ImageResource {
	full, err := d.localImage(url)
	if err != nil {
		problem := editor.ImageInvalid
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			problem = editor.ImageRemote
		}
		return editor.ImageResource{Problem: problem}
	}
	return d.imageResourceFrom(full, url, post)
}

// imageResourceFrom builds the resource of a picture whose file has been
// resolved already.
func (d *document) imageResourceFrom(full, url string, post func(func())) editor.ImageResource {
	if d == nil || d.closed.Load() {
		return editor.ImageResource{}
	}
	if post == nil {
		return decodeLocalImageSync(full)
	}
	info, err := os.Stat(full)
	if err != nil {
		problem := editor.ImageUnreadable
		if errors.Is(err, os.ErrNotExist) {
			problem = editor.ImageMissing
		}
		return editor.ImageResource{Problem: problem, Local: true}
	}
	modNano, size := info.ModTime().UnixNano(), info.Size()
	if d.imgCache == nil {
		d.imgCache = make(map[string]cachedImage)
	}
	if c, ok := d.imgCache[url]; ok && c.modNano == modNano && c.size == size {
		return c.resource
	}

	w, h := 0, 0
	if f, openErr := os.Open(full); openErr == nil {
		if cfg, _, cfgErr := image.DecodeConfig(f); cfgErr == nil {
			w, h = cfg.Width, cfg.Height
		}
		_ = f.Close()
	}
	placeholder := editor.ImageResource{
		Width:   w,
		Height:  h,
		Loading: true,
		Local:   true,
	}
	d.imgCache[url] = cachedImage{
		modNano:  modNano,
		size:     size,
		loading:  true,
		resource: placeholder,
	}
	activeImageDecodes.Add(1)
	go func() {
		defer activeImageDecodes.Add(-1)
		if d.closed.Load() {
			return
		}
		imageDecodeSem <- struct{}{}
		if d.closed.Load() {
			<-imageDecodeSem
			return
		}
		decoded := decodeLocalImageSync(full)
		<-imageDecodeSem
		if d.closed.Load() {
			return
		}
		post(func() {
			if d.closed.Load() {
				return
			}
			if decoded.Bitmap != nil {
				decoded.LoadedAt = time.Now()
			}
			if d.imgCache == nil {
				d.imgCache = make(map[string]cachedImage)
			}
			d.imgCache[url] = cachedImage{
				modNano:  modNano,
				size:     size,
				resource: decoded,
			}
			if d.ed != nil {
				d.ed.InvalidateImage(url)
			}
		})
	}()
	return placeholder
}

func decodeLocalImageThumb(full string) editor.ImageResource {
	data, err := os.ReadFile(full)
	if err != nil {
		problem := editor.ImageUnreadable
		if errors.Is(err, os.ErrNotExist) {
			problem = editor.ImageMissing
		}
		return editor.ImageResource{Problem: problem, Local: true}
	}
	bmp, err := ui.DecodeBitmapScaled(data, 640)
	if err != nil {
		return editor.ImageResource{Problem: editor.ImageUnsupported, Local: true}
	}
	w, h := bmp.Size()
	return editor.ImageResource{Bitmap: bmp, Width: w, Height: h, Local: true}
}

func decodeLocalImageSync(full string) editor.ImageResource {
	data, err := os.ReadFile(full)
	if err != nil {
		problem := editor.ImageUnreadable
		if errors.Is(err, os.ErrNotExist) {
			problem = editor.ImageMissing
		}
		return editor.ImageResource{Problem: problem, Local: true}
	}
	bmp, err := ui.DecodeBitmap(data)
	if err != nil {
		return editor.ImageResource{Problem: editor.ImageUnsupported, Local: true}
	}
	w, h := bmp.Size()
	res := editor.ImageResource{Bitmap: bmp, Width: w, Height: h, Local: true}
	if frames, delays, ok := gifFrames(data); ok {
		res.Frames, res.Delays = frames, delays
	}
	return res
}

// isMissingDocFile distinguishes a deleted document file from a non-directory
// path blocker (which Windows reports as ERROR_PATH_NOT_FOUND == os.ErrNotExist).
func isMissingDocFile(sp *space.Space, rel string, err error) bool {
	if !errors.Is(err, os.ErrNotExist) {
		return false
	}
	root, name, locErr := fileLocation(sp, rel)
	if locErr != nil {
		return false
	}
	return fsutil.IsMissingFile(filepath.Join(root, filepath.FromSlash(name)), err)
}

func unescapeImagePath(raw string) string {
	if decoded, err := neturl.PathUnescape(raw); err == nil {
		return decoded
	}
	return raw
}

func hasParentSegment(rel string) bool {
	for seg := range strings.SplitSeq(filepath.ToSlash(rel), "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// localImageFile resolves read-only resources against the document, not the
// Space boundary. Imported Markdown often uses ../images beside its folder.
// Writes still use vault.Resolve and may never escape the destination Space.
func localImageFile(root, rel, url string) (string, error) {
	decoded, err := neturl.PathUnescape(url)
	if err != nil {
		return "", err
	}
	if decoded == "" || strings.ContainsAny(decoded, ":\\\x00") || path.IsAbs(decoded) || filepath.IsAbs(decoded) {
		return "", errors.New("image reference must be a relative local path")
	}
	file, err := vault.Resolve(root, rel)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(file), filepath.FromSlash(decoded)), nil
}

// resolveSpaceImage resolves local images relative to the document first, and
// for non-external Spaces falls back to Obsidian-style vault-root and Space
// attachment lookup when that path does not exist on disk.
func resolveSpaceImage(sp *space.Space, rel, url string) (string, error) {
	root, docRel, err := fileLocation(sp, rel)
	if err != nil {
		return "", err
	}
	direct, err := localImageFile(root, docRel, url)
	if err != nil || sp == nil || sp.IsExternal() {
		return direct, err
	}
	if info, stErr := os.Stat(direct); stErr == nil && !info.IsDir() {
		return direct, nil
	}
	decoded := unescapeImagePath(url)
	if hasParentSegment(decoded) {
		if matches := attachmentMatches(sp, root, docRel, url); len(matches) > 0 {
			if full, ok := existingFile(root, matches[0]); ok {
				return full, nil
			}
		}
		return direct, nil
	}
	if cand, rErr := vault.Resolve(root, decoded); rErr == nil {
		if info, stErr := os.Stat(cand); stErr == nil && !info.IsDir() {
			return cand, nil
		}
	}
	if matches := attachmentMatches(sp, root, docRel, url); len(matches) > 0 {
		if full, ok := existingFile(root, matches[0]); ok {
			return full, nil
		}
	}
	if !strings.Contains(decoded, "/") {
		docDir := path.Dir(filepath.ToSlash(docRel))
		for _, candRel := range []string{path.Join(docDir, "assets", decoded), path.Join("assets", decoded)} {
			if cand, rErr := vault.Resolve(root, candRel); rErr == nil {
				if info, stErr := os.Stat(cand); stErr == nil && !info.IsDir() {
					return cand, nil
				}
			}
		}
		if found, ok := findSpaceAttachment(root, decoded); ok {
			return found, nil
		}
	}
	return direct, nil
}

func findSpaceAttachment(root, name string) (string, bool) {
	var bestPath, bestRel string
	bestExact := false
	bestDepth := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || !strings.EqualFold(d.Name(), name) {
			return nil
		}
		if info, stErr := os.Stat(p); stErr != nil || info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		resolved, resErr := vault.Resolve(root, rel)
		if resErr != nil {
			return nil
		}
		exact := d.Name() == name
		depth := strings.Count(rel, "/")
		if bestPath == "" ||
			(exact && !bestExact) ||
			(exact == bestExact && (depth < bestDepth || (depth == bestDepth && rel < bestRel))) {
			bestPath, bestRel, bestExact, bestDepth = resolved, rel, exact, depth
		}
		return nil
	})
	return bestPath, bestPath != ""
}

// resolveLink returns the space path a link of the document points to,
// when it is a relative link to a Markdown file.
func (d *document) resolveLink(url string) (string, bool) {
	if url == "" || strings.Contains(url, "://") || strings.HasPrefix(url, "#") || strings.HasPrefix(url, "mailto:") {
		return "", false
	}
	url, _, _ = strings.Cut(url, "#")
	if !vault.IsDocument(url) {
		return "", false
	}
	if d.space.IsExternal() {
		// Only links to already opened files belong to this collection.
		full, err := d.localImage(url)
		if err != nil {
			return "", false
		}
		for rel, file := range d.space.ExternalFiles {
			if filepath.Clean(file) == filepath.Clean(full) {
				return rel, true
			}
		}
		return "", false
	}
	var rel string
	if strings.HasPrefix(url, "/") {
		rel = strings.TrimPrefix(path.Clean(url), "/")
	} else {
		rel = path.Join(path.Dir(d.rel), url)
	}
	if _, err := resolveFile(d.space, rel); err != nil {
		return "", false
	}
	return rel, true
}
