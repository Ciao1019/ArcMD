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

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/pdf"
	"github.com/Ciao1019/ArcMD/internal/search"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/Ciao1019/ArcMD/internal/whiteboard"
	"github.com/Ciao1019/ArcMD/internal/wiki"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	untitledBoard        = "未命名白板"
	maxCachedWhiteboards = 16
)

type cachedWhiteboard struct {
	modNano int64
	size    int64
	rev     uint64
	scene   *whiteboard.Scene
	err     error
}

func (a *App) putCachedWhiteboard(key string, entry cachedWhiteboard) {
	if a.wbCache == nil {
		a.wbCache = make(map[string]cachedWhiteboard, maxCachedWhiteboards)
	}
	a.wbCache[key] = entry
	if idx := slices.Index(a.wbOrder, key); idx >= 0 {
		a.wbOrder = slices.Delete(a.wbOrder, idx, idx+1)
	}
	a.wbOrder = append(a.wbOrder, key)
	evicted := false
	for len(a.wbCache) > maxCachedWhiteboards && len(a.wbOrder) > 0 {
		oldest := a.wbOrder[0]
		a.wbOrder = a.wbOrder[1:]
		if _, ok := a.wbCache[oldest]; ok {
			delete(a.wbCache, oldest)
			evicted = true
		}
	}
	if evicted {
		compact := make(map[string]cachedWhiteboard, len(a.wbCache))
		for k, v := range a.wbCache {
			compact[k] = v
		}
		a.wbCache = compact
	}
}

func (d *document) isWhiteboard() bool {
	return d != nil && d.board != nil
}

func encodeWhiteboardFile(rel string, scene *whiteboard.Scene) (string, error) {
	if vault.IsWhiteboardSVG(rel) {
		return whiteboard.ExportEmbeddedSVG(scene, whiteboardSVGPalette(pageStyle(false).Whiteboard))
	}
	return scene.Encode()
}

func (a *App) newWhiteboard(dir string) {
	sp := a.state.ActiveSpace()
	if sp == nil {
		return
	}
	if sp.IsExternal() {
		a.notify("请在普通空间中新建白板。")
		return
	}
	if !a.flush() {
		return
	}
	content, err := whiteboard.ExportEmbeddedSVG(whiteboard.NewScene(), whiteboardSVGPalette(pageStyle(false).Whiteboard))
	if err != nil {
		a.notify("新建白板失败。%v", err)
		return
	}
	rel, err := vault.CreateFile(sp.Root, dir, untitledBoard, vault.ExtWhiteboard, content)
	if err != nil {
		a.notify("新建白板失败。%v", err)
		return
	}
	if dir != "" {
		sp.SetExpanded(dir, true)
		a.saveState()
	}
	a.clearPicks("")
	a.refreshTree()
	a.open(rel)
	if d := a.doc; d != nil && d.rel == rel && d.board != nil {
		d.board.RequestFocus = true
	}
}

func (a *App) boardOptionsFor(c *ui.Context, d *document) editor.BoardOptions {
	return editor.BoardOptions{
		Theme:             c.Theme(),
		Page:              pageStyle(a.paperDark),
		ReadOnly:          d.boardReadOnly,
		WikiSuggest:       func(q string) []string { return a.boardWikiNames(d, q) },
		OpenWiki:          func(name string, split bool) { a.openWikiFromBoard(d, name, split) },
		LinkedMentions:    func() []string { return a.linkedMentionsFor(d) },
		UserLibrary:       a.userWhiteboardLibrary,
		SaveUserLibrary:   a.saveWhiteboardLibrary,
		ImportLibraryFile: a.importWhiteboardLibrary,
		ExportSVG:         func() { a.exportWhiteboardSVG(d) },
	}
}

func (a *App) boardWikiNames(d *document, q string) []string {
	items := a.wikiSuggestFor(d, q)
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Name)
	}
	return out
}

func (a *App) openWikiFromBoard(d *document, name string, split bool) {
	if d == nil {
		return
	}
	if sp, rel, ok := a.findNamedFrom(d.space.ID, name); ok {
		if split {
			a.openSplit(sp, rel, false, false)
		} else {
			a.navigateInPane(d, func() { a.openAnywhere(sp.ID, rel) })
		}
		return
	}
	// Create missing note beside the whiteboard and optionally open in split.
	if d.space.IsExternal() {
		a.notify("外部空间中找不到「%s」。", name)
		return
	}
	dir := path.Dir(d.rel)
	if dir == "." {
		dir = ""
	}
	doc := model.New()
	title := model.NewBlock("heading", strings.TrimSpace(name))
	title.Level = 1
	doc.Blocks = []*model.Block{title, model.NewBlock("paragraph", "")}
	rel, err := vault.CreateFile(d.space.Root, dir, strings.TrimSpace(name), ".md", editor.ExportMarkdown(doc))
	if err != nil {
		a.notify("新建「%s」失败。%v", name, err)
		return
	}
	a.refreshTree()
	if split {
		a.openSplit(d.space, rel, false, false)
	} else {
		a.navigateInPane(d, func() { a.openIn(d.space, rel) })
	}
}

func (a *App) linkedMentionsFor(d *document) []string {
	if d == nil {
		return nil
	}
	ix := a.liveIndex()
	hits := ix.Backlinks(d.title(), search.Live{Space: d.space.ID, Path: d.rel}, nil)
	var out []string
	seen := map[string]bool{}
	for _, h := range hits {
		name := vault.Title(h.Path)
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	// Also scan markdown documents embedding ![[title.excalidraw]] or ![](...).
	base := path.Base(d.rel)
	a.eachDoc(func(sp *space.Space, n *vault.Node) bool {
		if n.Path == d.rel && sp.ID == d.space.ID {
			return true
		}
		if vault.IsWhiteboard(n.Path) || vault.IsPDF(n.Path) {
			return true
		}
		text, ok := ix.TextOf(sp.ID, n.Path)
		if !ok {
			raw, err := readFile(sp, n.Path)
			if err != nil {
				return true
			}
			text = raw
		}
		if strings.Contains(text, base) || strings.Contains(text, "[["+d.title()) {
			name := n.Title()
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
		return true
	})
	return out
}

func (a *App) documentCorner(c *ui.Context, d *document) {
	if d == nil || d.loadErr != nil {
		return
	}
	if d.isPDF() {
		a.pdfCorner(c, d)
		return
	}
	if d.isWhiteboard() {
		d.board.Corner(c, a.boardOptionsFor(c, d), func(ro bool) {
			d.boardReadOnly = ro
		})
		return
	}
	if !d.source {
		b := iconButton(c, "document-map", iconListTree, i18n.T("思维导图"), a.withKey(i18n.T("思维导图"), "mindmap"), false)
		if d.mindMap != nil {
			if !a.inWindow(d) && a.split == nil {
				b.Background(glass(c.Theme()))
			} else {
				b.Background(c.Theme().Accent.Alpha(.14))
			}
		}
		if b.Clicked() {
			a.toggleDocumentMap(d)
		}
		d.ed.Corner(c)
	}
}

func (a *App) loadWhiteboardFor(d *document, url string) (*whiteboard.Scene, uint64, error) {
	if d == nil || d.space == nil || url == "" {
		return nil, 0, errors.New("invalid whiteboard reference")
	}
	sp, rel, ok := a.resolveWhiteboardRef(d, url)
	if !ok {
		return nil, 0, errors.New(i18n.T("这个路径下没有文件。"))
	}
	// Prefer live resident whiteboard so split edits show up immediately.
	if res := a.residentDocument(sp.ID, rel); res != nil && res.loadErr == nil && res.isWhiteboard() {
		return res.board.Scene, res.board.Revision(), nil
	}
	full, err := resolveFile(sp, rel)
	if err != nil {
		return nil, 0, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil, 0, err
	}
	key := sp.ID + "/" + rel
	modNano, size := info.ModTime().UnixNano(), info.Size()
	if c, exists := a.wbCache[key]; exists && c.modNano == modNano && c.size == size {
		if idx := slices.Index(a.wbOrder, key); idx >= 0 {
			a.wbOrder = slices.Delete(a.wbOrder, idx, idx+1)
			a.wbOrder = append(a.wbOrder, key)
		}
		return c.scene, c.rev, c.err
	}
	text, err := readFile(sp, rel)
	var scene *whiteboard.Scene
	if err == nil {
		scene, err = whiteboard.Decode(text)
	}
	rev := uint64(modNano) ^ uint64(size<<12)
	a.putCachedWhiteboard(key, cachedWhiteboard{modNano: modNano, size: size, rev: rev, scene: scene, err: err})
	return scene, rev, err
}

func (a *App) resolveWhiteboardRef(d *document, url string) (*space.Space, string, bool) {
	if d == nil || d.space == nil {
		return nil, "", false
	}
	sp := d.space
	clean := unescapeImagePath(strings.TrimSpace(url))
	if clean == "" {
		return nil, "", false
	}
	if !sp.IsExternal() {
		// Relative to doc directory, then vault root, then space filename match.
		var candidates []string
		if dir := path.Dir(d.rel); dir != "." && !strings.HasPrefix(clean, "/") {
			candidates = append(candidates, path.Clean(path.Join(dir, clean)))
		}
		candidates = append(candidates, strings.TrimPrefix(path.Clean(clean), "/"))
		for _, cand := range candidates {
			if full, err := vault.Resolve(sp.Root, cand); err == nil {
				if info, stErr := os.Stat(full); stErr == nil && !info.IsDir() {
					return sp, cand, true
				}
			}
		}
		var foundRel string
		a.eachDoc(func(s *space.Space, n *vault.Node) bool {
			if s == sp && vault.IsWhiteboard(n.Path) && (strings.EqualFold(path.Base(n.Path), path.Base(clean)) || wiki.Same(n.Title(), vault.Title(clean))) {
				foundRel = n.Path
				return false
			}
			return true
		})
		if foundRel != "" {
			return sp, foundRel, true
		}
	}
	return nil, "", false
}

func (a *App) openWhiteboardFrom(d *document, url string, split bool) {
	sp, rel, ok := a.resolveWhiteboardRef(d, url)
	if !ok {
		a.notify("找不到白板「%s」。", url)
		return
	}
	if split {
		a.openSplit(sp, rel, false, false)
	} else {
		a.navigateInPane(d, func() { a.openIn(sp, rel) })
	}
}

func (a *App) saveBlockAsWhiteboard(d *document, blockID string, scene *whiteboard.Scene) {
	if d == nil || d.space == nil || d.space.IsExternal() || scene == nil {
		return
	}
	encoded, err := whiteboard.ExportEmbeddedSVG(scene, whiteboardSVGPalette(pageStyle(false).Whiteboard))
	if err != nil {
		a.notify("保存白板失败。%v", err)
		return
	}
	dir := path.Dir(d.rel)
	if dir == "." {
		dir = ""
	}
	baseTitle := d.title() + "-白板"
	rel, err := vault.CreateFile(d.space.Root, dir, baseTitle, vault.ExtWhiteboard, encoded)
	if err != nil {
		a.notify("保存白板失败。%v", err)
		return
	}
	// Replace inline block with standard image syntax referencing .excalidraw.svg
	// so external viewers (Typora, GitHub) render the SVG natively.
	ref := path.Base(rel)
	if !d.ed.ReadOnly {
		d.ed.Engine.Edit(func() {
			for i, blk := range d.ed.Doc.Blocks {
				if blk.ID == blockID {
					img := model.NewBlock("image", vault.Title(rel))
					img.URL = ref
					d.ed.Doc.Blocks[i] = img
					d.ed.Engine.FocusBlock(img.ID)
					break
				}
			}
		})
		d.ed.RequestFocus = true
	}
	a.refreshTree()
	a.refreshSearch()
	a.notify("已存为 %s", rel)
}

func (a *App) createWhiteboardAssetFor(d *document, scene *whiteboard.Scene) (string, error) {
	if d == nil || d.space == nil || scene == nil {
		return "", errors.New("no open document")
	}
	dir := path.Dir(d.rel)
	if dir == "." {
		dir = ""
	}
	encoded, err := whiteboard.ExportEmbeddedSVG(scene, whiteboardSVGPalette(pageStyle(false).Whiteboard))
	if err != nil {
		return "", err
	}
	name := sanitizeName(d.title()+"-白板") + vault.ExtWhiteboard
	rel, err := a.writeAssetFor(d, dir, name, []byte(encoded))
	if err == nil {
		a.refreshSearch()
	}
	return rel, err
}

func (a *App) saveWhiteboardFor(d *document, url string, scene *whiteboard.Scene) uint64 {
	if d == nil || d.space == nil || url == "" || scene == nil {
		return 0
	}
	sp, rel, ok := a.resolveWhiteboardRef(d, url)
	if !ok {
		return 0
	}
	if res := a.residentDocument(sp.ID, rel); res != nil && res.loadErr == nil && res.isWhiteboard() {
		res.board.ReplaceScene(scene, true)
		_ = res.write(false)
		a.refreshSearch()
		return res.board.Revision()
	}
	encoded, err := encodeWhiteboardFile(rel, scene)
	if err != nil {
		return 0
	}
	if err := writeFile(sp, rel, encoded); err != nil {
		a.notify("保存白板失败。%v", err)
		return 0
	}
	key := sp.ID + "/" + rel
	prevRev := a.wbCache[key].rev
	var modNano, size int64
	if full, err := resolveFile(sp, rel); err == nil {
		if info, stErr := os.Stat(full); stErr == nil {
			modNano, size = info.ModTime().UnixNano(), info.Size()
		}
	}
	rev := max(prevRev+1, uint64(modNano)^uint64(size<<12))
	a.putCachedWhiteboard(key, cachedWhiteboard{modNano: modNano, size: size, rev: rev, scene: scene.Clone(), err: nil})
	a.refreshSearch()
	return rev
}

func (a *App) exportWhiteboardSVG(d *document) {
	if d == nil || !d.isWhiteboard() || d.loadErr != nil {
		return
	}
	pal := whiteboardSVGPalette(pageStyle(false).Whiteboard)
	svg := whiteboard.ExportSVG(d.board.SceneSnapshot(), pal)
	root, nameOnDisk, err := fileLocation(d.space, d.rel)
	if err != nil {
		a.notify("导出失败。%v", err)
		return
	}
	dir := path.Dir(nameOnDisk)
	if dir == "." {
		dir = ""
	}
	for i := 1; i < 1000; i++ {
		name := d.title()
		if i > 1 {
			name = fmt.Sprintf("%s %d", name, i)
		}
		rel := path.Join(dir, name+".svg")
		full, err := vault.Resolve(root, rel)
		if err != nil {
			a.notify("导出失败。%v", err)
			return
		}
		err = fsutil.CreateFileAtomic(full, []byte(svg), 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			a.notify("导出失败。%v", err)
			return
		}
		a.notify("已导出 %s", rel)
		a.refreshTree()
		if a.win != nil {
			mygo.Shell.ShowItemInFolder(full)
		}
		return
	}
}

func (a *App) boardDropDocCard(paneDoc *document, localX, localY float32, srcSpaceID, srcRel string) {
	if paneDoc == nil || !paneDoc.isWhiteboard() || paneDoc.boardReadOnly {
		return
	}
	title := vault.Title(srcRel)
	excerpt := ""
	if src := a.residentDocument(srcSpaceID, srcRel); src != nil && src.loadErr == nil {
		excerpt = strings.TrimSpace(src.plainText())
	} else if sp := a.state.Get(srcSpaceID); sp != nil {
		if raw, err := readFile(sp, srcRel); err == nil {
			if ex, err := extractText(srcRel, raw); err == nil {
				excerpt = strings.TrimSpace(ex.Text)
			}
		}
	}
	// Strip leading title line and condense excerpt to 2 short lines.
	lines := strings.Split(excerpt, "\n")
	var body []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || l == title {
			continue
		}
		body = append(body, l)
		if len(body) >= 2 {
			break
		}
	}
	short := strings.Join(body, "\n")
	if r := []rune(short); len(r) > 68 {
		short = string(r[:68]) + "…"
	}
	// Account for 36 DIP pane bar when dropping into a split pane card.
	ly := localY
	if a.split != nil {
		ly -= 36
	}
	paneDoc.board.InsertDocCardAt(localX, max(20, ly), title, short)
}

func (a *App) exportWhiteboardPDF(d *document, printing bool) {
	if d == nil || !d.isWhiteboard() || d.loadErr != nil {
		return
	}
	dir := filepath.Dir(d.fullPath())
	if printing {
		dir = filepath.Join(os.TempDir(), i18n.T("ArcMD 打印"))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			a.notify("准备打印失败。%v", err)
			return
		}
	}
	file, err := os.CreateTemp(dir, ".arcmd-board-*.pdf")
	if err != nil {
		a.notify("导出 PDF 失败。%v", err)
		return
	}
	w := pdf.NewWriter(file, d.title())
	w.ReservePages(1)
	vp := w.NewVectorPage(pdf.A4Width, pdf.A4Height)
	margin := float64(36)
	theme := documentTheme(false)
	pg := pageStyle(false)
	vp.FillRect(0, 0, vp.Width, vp.Height, 0, [4]uint8{255, 255, 255, 255})
	editor.ExportWhiteboardPDF(vp, d.board.SceneSnapshot(), pg, theme, margin, margin, vp.Width-2*margin, vp.Height-2*margin, 1)
	err = w.FinishVectorPage(vp)
	if err == nil {
		err = w.Close()
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(file.Name())
		a.notify("导出 PDF 失败。%v", err)
		return
	}
	if printing {
		if a.win != nil {
			mygo.Shell.OpenExternal(file.Name())
		}
		return
	}
	outDir := path.Dir(d.rel)
	if outDir == "." {
		outDir = ""
	}
	root, _, locErr := fileLocation(d.space, d.rel)
	if locErr != nil {
		_ = os.Remove(file.Name())
		a.notify("导出 PDF 失败。%v", locErr)
		return
	}
	for i := 1; i < 1000; i++ {
		name := d.title()
		if i > 1 {
			name = fmt.Sprintf("%s %d", name, i)
		}
		rel := path.Join(outDir, name+".pdf")
		full, rErr := vault.Resolve(root, rel)
		if rErr != nil {
			_ = os.Remove(file.Name())
			a.notify("导出 PDF 失败。%v", rErr)
			return
		}
		if _, stErr := os.Stat(full); stErr == nil {
			continue
		}
		if mvErr := os.Rename(file.Name(), full); mvErr != nil {
			_ = os.Remove(file.Name())
			a.notify("导出 PDF 失败。%v", mvErr)
			return
		}
		a.notify("已导出 %s", rel)
		a.refreshTree()
		if a.win != nil {
			mygo.Shell.ShowItemInFolder(full)
		}
		return
	}
	_ = os.Remove(file.Name())
}

type whiteboardLibState struct {
	loaded bool
	items  []whiteboard.LibraryItem
}

func (a *App) userWhiteboardLibraryPath() string {
	if a.cfg.ConfigDir == "" {
		return ""
	}
	return filepath.Join(a.cfg.ConfigDir, "whiteboard-library.excalidrawlib")
}

func (a *App) userWhiteboardLibrary() []whiteboard.LibraryItem {
	if !a.wbLib.loaded {
		a.wbLib.loaded = true
		if libPath := a.userWhiteboardLibraryPath(); libPath != "" {
			if data, err := os.ReadFile(libPath); err == nil {
				if items, decErr := whiteboard.DecodeLibrary(string(data)); decErr == nil {
					a.wbLib.items = items
				}
			}
		}
	}
	return a.wbLib.items
}

func (a *App) saveWhiteboardLibrary(items []whiteboard.LibraryItem) {
	a.wbLib.loaded = true
	a.wbLib.items = items
	libPath := a.userWhiteboardLibraryPath()
	if libPath == "" {
		return
	}
	encoded, err := whiteboard.EncodeLibrary(items)
	if err != nil {
		a.notify("保存素材库失败。%v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(libPath), 0o700); err != nil {
		a.notify("保存素材库失败。%v", err)
		return
	}
	if err := fsutil.WriteFileAtomic(libPath, []byte(encoded), 0o644); err != nil {
		a.notify("保存素材库失败。%v", err)
	}
}

func (a *App) importWhiteboardLibrary(done func([]whiteboard.LibraryItem)) {
	loadPaths := func(paths []string) {
		var collected []whiteboard.LibraryItem
		for _, p := range paths {
			data, err := os.ReadFile(p)
			if err != nil {
				a.notify("导入素材库失败。%v", err)
				continue
			}
			items, err := whiteboard.DecodeLibrary(string(data))
			if err != nil {
				a.notify("导入素材库失败。%v", err)
				continue
			}
			collected = append(collected, items...)
		}
		if len(collected) > 0 {
			if done != nil {
				done(collected)
			}
			a.notify("已导入 %d 个白板组件", len(collected))
		}
	}
	if a.pickFiles != nil {
		a.pickFiles(loadPaths)
		return
	}
	if a.win == nil {
		return
	}
	win := a.win
	go func() {
		files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:   win,
			Title:    i18n.T("导入 .excalidrawlib 素材包"),
			Multiple: true,
			Filters:  []mygo.FileFilter{{Name: "Excalidraw Library", Extensions: []string{"excalidrawlib", "json"}}},
		})
		if err != nil || len(files) == 0 {
			return
		}
		win.Update(func() {
			loadPaths(files)
		})
	}()
}
