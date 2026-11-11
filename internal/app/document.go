package app

import (
	"errors"
	"fmt"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

const autosaveDelay = 700 * time.Millisecond

// document owns a rich model, its persisted baseline and export preview.
type document struct {
	space *space.Space
	rel   string
	// named is the name this session gave the file from its title; the file
	// keeps following the title while it has that name or 未命名's.
	named         string
	namedFor      string // the title it was named for
	diskText      string
	source        bool
	ed            *editor.Editor
	version       uint64
	edited        time.Time
	saveErr       error
	failedVersion uint64
	baseline      string
	// dirtyRev, dirtyKnown and dirtyNow remember dirty for a revision.
	dirtyRev       uint64
	dirtyKnown     bool
	dirtyNow       bool
	textDirtyKnown bool
	textDirty      map[string]bool
	baselineRuns   map[string][]model.Run
	loadErr        error
	migratedFrom   string
	preview        string
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
	// plain is the text search sees, for plainRev (search.go).
	plain      string
	plainRev   uint64
	plainKnown bool
	footer     pageFooterCache
	// marks are the document's bookmarks resolved to its blocks
	// (bookmarks.go).
	marks docMarks
}

func openDocument(sp *space.Space, rel, text string, preferSource bool) *document {
	d := &document{space: sp, rel: rel, diskText: text, source: preferSource}
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
	d.ed = editor.FromDocument(doc)
	d.baseline, _ = doc.Encode()
	d.rememberBaseline(doc)
	if preferSource {
		d.preview = editor.ExportMarkdown(doc)
	}
	return d
}
func (d *document) title() string { return vault.Title(d.rel) }
func (d *document) text() string  { return editor.ExportMarkdown(d.ed.Committed()) }

// dirty reports whether the document differs from what was saved. Text-only
// revisions compare the affected runs; other changes compare the full committed
// model. The sidebar, address field and autosave share the result per revision.
// Saving always checks the full model afresh (changed).
func (d *document) dirty() bool {
	if d.loadErr != nil {
		return false
	}
	if rev := d.ed.Revision(); !d.dirtyKnown || rev != d.dirtyRev {
		if change, ok := d.ed.TextChangeSince(d.dirtyRev); ok && d.dirtyKnown && d.textDirtyKnown {
			current := d.ed.Engine.LeafAt(change.Start).Block
			if saved, exists := d.baselineRuns[change.ID]; exists && slices.Equal(saved, current.Runs) {
				delete(d.textDirty, change.ID)
			} else {
				d.textDirty[change.ID] = true
			}
			d.dirtyNow = len(d.textDirty) != 0
		} else {
			d.dirtyNow = d.changed()
			d.textDirtyKnown = !d.dirtyNow
			clear(d.textDirty)
		}
		d.dirtyRev, d.dirtyKnown = rev, true
	}
	return d.dirtyNow
}

// A run comparison is exact for a sequence of text-only edits. Structural
// edits still take the full comparison; saving always verifies the whole model.
func (d *document) rememberBaseline(doc *model.Document) {
	d.baselineRuns = make(map[string][]model.Run)
	for _, leaf := range doc.Leaves() {
		d.baselineRuns[leaf.Block.ID] = slices.Clone(leaf.Block.Runs)
	}
	d.textDirty = make(map[string]bool)
	d.textDirtyKnown = true
	d.dirtyRev, d.dirtyKnown, d.dirtyNow = d.ed.Revision(), true, false
}

// changed encodes the document and compares it with the saved baseline.
func (d *document) changed() bool {
	text, err := d.ed.Committed().Encode()
	return err != nil || text != d.baseline
}
func (d *document) toggleMode() {
	d.ed.UnmarkText()
	d.source = !d.source
	if d.source {
		d.preview = d.text()
	} else {
		d.ed.RequestFocus = true
	}
}
func (d *document) save() error { return d.write(false) }

// write saves the document. Unless force is set, it refuses to overwrite a
// file that changed since ArcMD last read or wrote it, or to bring back one
// that went: the file watch may be late or absent, so the file itself is
// compared, a cheap read of one document.
func (d *document) write(force bool) error {
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
	// A fresh comparison, not dirty's memory: saving before a switch or
	// closing must not trust a change that skipped the revision.
	if !d.changed() && !force {
		d.saveErr = nil
		return nil
	}
	if !force && !vault.IsMarkdown(d.rel) {
		disk, err := vault.Read(d.space.Root, d.rel)
		switch {
		case errors.Is(err, os.ErrNotExist):
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
	text, err := d.ed.Committed().Encode()
	if err == nil {
		if vault.IsMarkdown(d.rel) {
			var rel string
			rel, err = vault.CreateFile(d.space.Root, path.Dir(d.rel), d.title(), ".arcmd", text)
			if err == nil {
				d.migratedFrom, d.rel = d.rel, rel
			}
		} else {
			err = vault.Write(d.space.Root, d.rel, text)
		}
	}
	if err != nil {
		d.saveErr = err
		return err
	}
	before := d.diskText
	if d.migratedFrom != "" {
		before = "" // a Markdown file's first .arcmd: nothing earlier to keep
	}
	d.diskText, d.baseline, d.saveErr = text, text, nil
	d.recordVersion(before)
	d.conflict, d.missing, d.diskNow = false, false, ""
	d.rememberBaseline(d.ed.Committed())
	return nil
}

// viewKey identifies the document's editor view by the editor itself, not
// by its path: a path changes when the file follows the title, and a new
// key would make the view a new element, taking the focus and ending an
// input method's composition mid-word.
func (d *document) viewKey() string { return fmt.Sprintf("editor:%p", d.ed) }

func (d *document) autosave(c *ui.Context) {
	if version := d.ed.Version(); version != d.version {
		d.version, d.edited = version, c.Now()
	}
	if d.conflict || d.missing || !d.dirty() || d.saveErr != nil && d.version == d.failedVersion {
		return
	}
	if wait := autosaveDelay - c.Now().Sub(d.edited); wait > 0 {
		c.After(wait)
		return
	}
	if d.save() != nil {
		d.failedVersion = d.version
	}
}

func (d *document) loadImage(url string) *ui.Bitmap {
	return d.imageResource(url).Bitmap
}

func (d *document) imageResource(url string) editor.ImageResource {
	full, err := localImageFile(d.space.Root, d.rel, url)
	if err != nil {
		problem := editor.ImageInvalid
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			problem = editor.ImageRemote
		}
		return editor.ImageResource{Problem: problem}
	}
	data, err := os.ReadFile(full)
	if err != nil {
		problem := editor.ImageUnreadable
		if errors.Is(err, os.ErrNotExist) {
			problem = editor.ImageMissing
		}
		return editor.ImageResource{Problem: problem, Local: true}
	}
	image, err := ui.DecodeBitmap(data)
	if err != nil {
		return editor.ImageResource{Problem: editor.ImageUnsupported, Local: true}
	}
	return editor.ImageResource{Bitmap: image, Local: true}
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
	var rel string
	if strings.HasPrefix(url, "/") {
		rel = strings.TrimPrefix(path.Clean(url), "/")
	} else {
		rel = path.Join(path.Dir(d.rel), url)
	}
	if _, err := vault.Resolve(d.space.Root, rel); err != nil {
		return "", false
	}
	return rel, true
}
