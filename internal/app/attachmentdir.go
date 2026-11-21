package app

import (
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/internal/history"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
)

// imageKey is a picture resolution's cache key: the document showing it and
// the URL it is referenced by.
type imageKey struct {
	doc *document
	url string
}

// resolvedImage is where a URL's picture file was found, or why it could
// not be resolved at all.
type resolvedImage struct {
	full string
	err  error
}

// attachmentRelink serializes scans for a space. Moves received during a
// scan are retained together, so a subsequent scan can follow chained moves.
type attachmentRelink struct {
	root    string
	moves   map[string]string
	running bool
	again   bool
}

type attachmentRewrite struct {
	rel, before, after string
}

// attachmentFolder is the folder the space keeps its attachments in, as a
// slash path relative to the space's root. Empty keeps the older
// arrangement: an assets folder beside each document.
func attachmentFolder(sp *space.Space) string {
	if sp == nil || sp.IsExternal() {
		return ""
	}
	return sp.AttachmentsDir
}

// docRelativeURL is the Markdown URL of a space-relative file as the
// document at docDir writes it: GitHub, VS Code and the HTML export resolve
// image paths against the document, so a document in a subfolder points at
// the folder with ../ segments.
func docRelativeURL(docDir, fileRel string) string {
	rel, ok := slashRel(docDir, fileRel)
	if !ok {
		rel = fileRel
	}
	return escapeImagePath(rel)
}

// slashRel is a clean slash path target seen from the clean slash folder
// base ("" for the root), sharing common directory segments before adding ../
// once per remaining folder level.
func slashRel(base, target string) (string, bool) {
	if target == "" || target == "." || target == ".." || strings.HasPrefix(target, "../") {
		return "", false
	}
	target = path.Clean(target)
	if target == "" || target == "." || target == ".." || strings.HasPrefix(target, "../") {
		return "", false
	}
	if base == "" || base == "." {
		return target, true
	}
	base = path.Clean(base)
	if base == "." {
		return target, true
	}
	if base == ".." || strings.HasPrefix(base, "../") {
		return "", false
	}
	bSegs := strings.Split(base, "/")
	tSegs := strings.Split(target, "/")
	common := 0
	for common < len(bSegs) && common < len(tSegs)-1 && bSegs[common] == tSegs[common] {
		common++
	}
	return strings.Repeat("../", len(bSegs)-common) + strings.Join(tSegs[common:], "/"), true
}

// escapeImagePath keeps URLs Markdown links can hold: spaces become %20,
// which reading unescapes again.
func escapeImagePath(rel string) string {
	return strings.ReplaceAll(rel, " ", "%20")
}

// folderPath returns the disk path of a slash folder relative to root; "."
// is the root itself, which vault.Resolve does not accept.
func folderPath(root, folder string) (string, bool) {
	if folder == "." {
		return root, true
	}
	full, err := vault.Resolve(root, folder)
	if err != nil {
		return "", false
	}
	return full, true
}

// existingFile returns the disk path of a slash file relative to root when
// it exists as a file.
func existingFile(root, rel string) (string, bool) {
	full, err := vault.Resolve(root, rel)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		return "", false
	}
	return full, true
}

// chooseAttachmentsFolder asks where the space keeps its attachments, off
// the main thread as the dialog blocks.
func (a *App) chooseAttachmentsFolder(sp *space.Space) {
	if sp == nil || sp.IsExternal() || a.win == nil {
		return
	}
	start := sp.Root
	if f := attachmentFolder(sp); f != "" && f != "." {
		start = filepath.Join(sp.Root, filepath.FromSlash(f))
	}
	win := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: win, Title: i18n.T("选择附件目录"), Directory: true,
			DefaultPath: start, CreateDirectories: true,
		})
		if err != nil || len(paths) == 0 {
			return
		}
		win.Update(func() { a.setAttachmentsFolder(sp, paths[0]) })
	}()
}

// setAttachmentsFolder records dir as the space's attachment folder: an
// absolute path is turned into one inside the space (both sides resolved
// through symbolic links, as macOS hands out /var for /private/var), and ""
// goes back to an assets folder beside each document. Documents already
// pointing where the folder moved to are relinked, then spaces.json keeps
// the choice.
func (a *App) setAttachmentsFolder(sp *space.Space, dir string) {
	if sp == nil || sp.IsExternal() {
		return
	}
	root := sp.Root
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if filepath.IsAbs(dir) {
		resolved := dir
		if through, err := filepath.EvalSymlinks(dir); err == nil {
			resolved = through
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			a.notify("附件目录必须在空间文件夹内。")
			return
		}
		dir = filepath.ToSlash(rel)
	}
	if !sp.SetAttachmentsDir(dir) {
		a.notify("附件目录必须在空间文件夹内。")
		return
	}
	a.saveState()
	a.invalidateResolvedImages()
	delete(a.attachmentRelinks, sp.ID)
	delete(a.pageTrees, sp.ID)
	if sp == a.state.ActiveSpace() {
		a.refreshTree()
	}
	label := sp.AttachmentsDir
	if label == "" {
		label = i18n.T("文档旁的 assets")
	}
	a.notify("附件目录已设为「%s」。", label)
}

// attachmentMatches lists where url may live under the space's attachment
// folder now, as space-relative paths of existing files, best first: the
// path the URL names under the folder, its bare name, an assets/ prefix
// stripped, and finally any file of that name in the folder, shallowest and
// exact-case first. These candidates are for reading only; rewriting needs
// a recorded move, since a unique basename does not establish ownership.
func attachmentMatches(sp *space.Space, root, docRel, url string) []string {
	folder := attachmentFolder(sp)
	if folder == "" || sp.IsExternal() {
		return nil
	}
	decoded := unescapeImagePath(url)
	decoded = strings.ReplaceAll(decoded, `\`, "/")
	if decoded == "" || path.IsAbs(decoded) || strings.ContainsAny(decoded, `:`+"\x00") {
		return nil
	}
	clean := path.Clean(decoded)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		clean = path.Clean(path.Join(path.Dir(filepath.ToSlash(docRel)), decoded))
	}
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return nil
	}
	base := path.Base(clean)
	if base == "." || base == "/" || base == "" {
		return nil
	}
	var matches []string
	add := func(rel string) {
		rel = path.Clean(rel)
		if rel == "." || slices.Contains(matches, rel) {
			return
		}
		if _, ok := existingFile(root, rel); ok {
			matches = append(matches, rel)
		}
	}
	add(path.Join(folder, clean))
	if folder != "." && strings.HasPrefix(clean, folder+"/") {
		add(path.Join(folder, strings.TrimPrefix(clean, folder+"/")))
	}
	add(path.Join(folder, base))
	if strings.HasPrefix(clean, assetsDir+"/") {
		add(path.Join(folder, strings.TrimPrefix(clean, assetsDir+"/")))
	}
	if len(matches) > 0 {
		return matches
	}
	// The file sits deeper in the attachment folder: match it by name.
	for _, rel := range findInAttachmentFolder(root, folder, base) {
		add(rel)
	}
	return matches
}

// findInAttachmentFolder lists the files named name anywhere under folder,
// exact case matches before the rest, shallower paths before deeper ones.
func findInAttachmentFolder(root, folder, name string) []string {
	if name == "" || name == "." || name == "/" {
		return nil
	}
	base, ok := folderPath(root, folder)
	if !ok {
		return nil
	}
	prefix := ""
	if folder != "." {
		prefix = folder + "/"
	}
	type hit struct {
		rel   string
		exact bool
		depth int
	}
	var hits []hit
	_ = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != base && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || !strings.EqualFold(d.Name(), name) {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, prefix) {
			return nil
		}
		hits = append(hits, hit{rel, d.Name() == name, strings.Count(rel, "/")})
		return nil
	})
	slices.SortFunc(hits, func(a, b hit) int {
		if a.exact != b.exact {
			if a.exact {
				return -1
			}
			return 1
		}
		if a.depth != b.depth {
			return a.depth - b.depth
		}
		return strings.Compare(a.rel, b.rel)
	})
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.rel
	}
	return out
}

// renameMoves collects the real file moves a batch of watch events made, as
// space-relative old → new paths, so relocations follow where files went
// instead of guessing by name. The dot-named temporaries of ArcMD's own
// atomic writes are left out.
func renameMoves(sp *space.Space, events []mygo.FileEvent) map[string]string {
	if sp == nil || sp.IsExternal() {
		return nil
	}
	var moves map[string]string
	for _, e := range events {
		if e.Op != mygo.FileRename || e.OldPath == "" {
			continue
		}
		if strings.HasPrefix(path.Base(e.OldPath), ".") {
			continue
		}
		from, to := path.Clean(filepath.ToSlash(e.OldPath)), path.Clean(filepath.ToSlash(e.Path))
		if from == "." || to == "." || path.IsAbs(from) || path.IsAbs(to) ||
			from == ".." || to == ".." || strings.HasPrefix(from, "../") || strings.HasPrefix(to, "../") {
			continue
		}
		if from == to {
			continue
		}
		if moves == nil {
			moves = map[string]string{}
		}
		if previous, ok := moves[from]; ok && previous != to {
			moves[from] = "" // a reused source path cannot identify one old file
		} else {
			moves[from] = to
		}
	}
	return moves
}

// moveTarget follows a space-relative path through the moves: the path
// itself, or the folder holding it, went from one place to another. The
// most specific move wins.
func moveTarget(moves map[string]string, rel string) (string, bool) {
	seen := map[string]bool{}
	used := map[string]bool{}
	current := rel
	for !seen[current] {
		seen[current] = true
		next, from := singleMoveTarget(moves, current, used)
		if from == "" {
			return current, current != rel
		}
		if next == "" {
			return "", false
		}
		used[from] = true
		current = next
	}
	return "", false // a cyclic batch cannot identify a destination
}

func singleMoveTarget(moves map[string]string, rel string, used map[string]bool) (string, string) {
	if len(moves) == 0 || rel == "" {
		return "", ""
	}
	bestFrom, bestTo := "", ""
	for from, to := range moves {
		if used[from] {
			continue
		}
		var cand string
		switch {
		case rel == from:
			cand = to
		case strings.HasPrefix(rel, from+"/"):
			cand = to + rel[len(from):]
		default:
			continue
		}
		if bestFrom == "" || len(from) > len(bestFrom) {
			bestFrom, bestTo = from, cand
		}
	}
	return bestTo, bestFrom
}

// attachmentRelocations follows recorded moves only. A pre-existing broken
// reference must never be repaired using another document's same-named file.
func attachmentRelocations(sp *space.Space, rel string, urls []string, moves map[string]string) map[string]string {
	if attachmentFolder(sp) == "" || sp.IsExternal() || len(moves) == 0 {
		return nil
	}
	root, docRel, err := fileLocation(sp, rel)
	if err != nil {
		return nil
	}
	if canonical, err := filepath.EvalSymlinks(root); err == nil {
		root = canonical
	}
	docDir := path.Dir(filepath.ToSlash(docRel))
	var moved map[string]string
	set := func(url, to string) {
		if moved == nil {
			moved = map[string]string{}
		}
		moved[url] = to
	}
	for _, url := range urls {
		direct, err := localImageFile(root, docRel, url)
		if err != nil {
			continue
		}
		if info, err := os.Stat(direct); err == nil && !info.IsDir() {
			continue
		}
		// Only the document-relative path identifies the original file.
		// A legacy root-relative or basename fallback is safe for reading,
		// but cannot distinguish a missing local picture from another one.
		old, err := filepath.Rel(root, direct)
		if err != nil {
			continue
		}
		old = filepath.ToSlash(old)
		if old == ".." || strings.HasPrefix(old, "../") {
			continue
		}
		to, ok := moveTarget(moves, old)
		if !ok {
			continue
		}
		if _, ok := existingFile(root, to); !ok {
			continue
		}
		if next := docRelativeURL(docDir, to); next != url {
			set(url, next)
		}
	}
	return moved
}

// relinkAttachments rewrites the image references of the space's documents
// to the space's attachment folder once their files moved there by hand.
// The documents kept open are rebased in place (their autosave writes the
// text). Enumeration, reading and parsing of closed documents run off the
// UI thread; each proposed write is checked again on the UI thread so a
// document opened meanwhile cannot be changed behind its editor.
func (a *App) relinkAttachments(sp *space.Space, moves map[string]string) {
	if sp == nil || sp.IsExternal() || attachmentFolder(sp) == "" || len(moves) == 0 {
		return
	}
	if folder, ok := folderPath(sp.Root, attachmentFolder(sp)); !ok || !isDir(folder) {
		return
	}
	resident := map[string]bool{}
	for _, d := range a.residentDocuments() {
		if d.space != sp {
			continue
		}
		resident[d.rel] = true
		if d.loadErr != nil || d.isWhiteboard() || d.isPDF() || d.source || d.ed.ReadOnly {
			continue
		}
		if moved := attachmentRelocations(sp, d.rel, d.ed.ImageURLs(), moves); len(moved) > 0 {
			d.ed.RelocateImages(moved)
		}
	}
	if a.attachmentRelinks == nil {
		a.attachmentRelinks = map[string]*attachmentRelink{}
	}
	job := a.attachmentRelinks[sp.ID]
	if job == nil || job.root != sp.Root {
		job = &attachmentRelink{root: sp.Root, moves: map[string]string{}}
		a.attachmentRelinks[sp.ID] = job
	}
	for from, to := range moves {
		if previous, ok := job.moves[from]; ok && previous != to {
			job.moves[from] = ""
		} else {
			job.moves[from] = to
		}
	}
	if job.running {
		job.again = true
		return
	}
	job.running = true
	// Workers own immutable inputs and never access live application state.
	snapshot := space.Space{ID: sp.ID, Root: sp.Root, AttachmentsDir: sp.AttachmentsDir}
	moves = maps.Clone(job.moves)
	post := a.runAsync
	if post == nil && a.win != nil {
		post = a.win.Update
	}
	valid := func() bool {
		return a.attachmentRelinks[sp.ID] == job && a.state.Get(sp.ID) == sp &&
			sp.Root == snapshot.Root && sp.AttachmentsDir == snapshot.AttachmentsDir
	}
	deliver := func(change attachmentRewrite) {
		if !valid() {
			return
		}
		a.applyAttachmentRewrite(sp, change)
	}
	done := func() {
		if a.attachmentRelinks[sp.ID] != job {
			return
		}
		delete(a.attachmentRelinks, sp.ID)
		if job.again && a.state.Get(sp.ID) == sp && sp.Root == snapshot.Root {
			a.relinkAttachments(sp, job.moves)
		}
	}
	scan := func() {
		docs, _ := vault.List(snapshot.Root)
		for _, info := range docs {
			if resident[info.Path] || vault.IsWhiteboard(info.Path) || vault.IsPDF(info.Path) {
				continue
			}
			text, err := readFile(&snapshot, info.Path)
			if err != nil {
				continue
			}
			if change, ok := prepareAttachmentRewrite(&snapshot, info.Path, text, moves); ok {
				if post != nil {
					post(func() { deliver(change) })
				} else {
					deliver(change)
				}
			}
		}
		if post != nil {
			post(done)
		} else {
			done()
		}
	}
	if post != nil {
		go scan()
	} else {
		scan() // headless tests without a main-thread dispatcher
	}
}

// relinkClosedDoc rewrites one closed document on disk. It is cheap to
// decline: documents without picture syntax are skipped before any
// Markdown is parsed, the file is compared again right before writing so a
// change made meanwhile is never clobbered, and the content as it was is
// kept as a version first.
func (a *App) relinkClosedDoc(sp *space.Space, rel, text string, moves map[string]string) {
	if change, ok := prepareAttachmentRewrite(sp, rel, text, moves); ok {
		a.applyAttachmentRewrite(sp, change)
	}
}

func prepareAttachmentRewrite(sp *space.Space, rel, text string, moves map[string]string) (attachmentRewrite, bool) {
	// Pictures are ![alt](url), ![[obsidian]] or, in legacy .arcmd JSON,
	// "image" blocks.
	if !strings.Contains(text, "![") && !strings.Contains(text, `"image"`) &&
		!strings.Contains(text, "cover:") && !strings.Contains(text, `"cover"`) {
		return attachmentRewrite{}, false
	}
	doc, err := imageDocument(rel, text)
	if err != nil {
		return attachmentRewrite{}, false
	}
	var urls []string
	doc.MapImages(func(url string) string {
		if url != "" && !slices.Contains(urls, url) {
			urls = append(urls, url)
		}
		return url
	})
	if len(urls) == 0 {
		return attachmentRewrite{}, false
	}
	moved := attachmentRelocations(sp, rel, urls, moves)
	if len(moved) == 0 {
		return attachmentRewrite{}, false
	}
	out, err := relocateText(rel, text, moved)
	if err != nil || out == text {
		return attachmentRewrite{}, false
	}
	return attachmentRewrite{rel: rel, before: text, after: out}, true
}

func (a *App) applyAttachmentRewrite(sp *space.Space, change attachmentRewrite) {
	if a.residentDocument(sp.ID, change.rel) != nil {
		return
	}
	if now, err := readFile(sp, change.rel); err != nil || now != change.before {
		return // the reader changed it meanwhile
	}
	root, name, err := fileLocation(sp, change.rel)
	if err != nil {
		return
	}
	if err := history.Record(root, change.rel, change.before, time.Now(), true); err != nil {
		a.notify("更新图片引用失败。%v", err)
		return
	}
	if now, err := readFile(sp, change.rel); err != nil || now != change.before {
		return
	}
	if err := vault.Write(root, name, change.after); err != nil {
		a.notify("更新图片引用失败。%v", err)
		return
	}
	a.invalidateResolvedImages()
}

func isDir(full string) bool {
	info, err := os.Stat(full)
	return err == nil && info.IsDir()
}

// underAttachmentFolder reports whether a space-relative path names folder
// or a file inside it. "." stands for the space's root, which holds every
// path.
func underAttachmentFolder(clean, folder string) bool {
	if clean == "" || folder == "" {
		return false
	}
	if folder == "." {
		return true
	}
	if clean == folder || strings.HasPrefix(clean, folder+"/") {
		return true
	}
	return strings.Contains(clean, "/"+folder+"/")
}

// markSelfWrite remembers a file ArcMD just wrote, so the watch events of
// its own asset writes do not start attachment relinking.
func (a *App) markSelfWrite(full string) {
	if a.selfWrites == nil {
		a.selfWrites = map[string]time.Time{}
	}
	a.selfWrites[full] = time.Now()
}

// wroteRecently reports whether ArcMD wrote the file a moment ago.
func (a *App) wroteRecently(full string) bool {
	at, ok := a.selfWrites[full]
	return ok && time.Since(at) < 15*time.Second
}

func (a *App) pruneSelfWrites() {
	for p, at := range a.selfWrites {
		if time.Since(at) > time.Minute {
			delete(a.selfWrites, p)
		}
	}
}

// attachmentsTouched reports whether a batch of watch events may have moved
// attachments into the space's folder, so the documents should be relinked.
// Only real renames establish the old-to-new relationship. Creates, plain
// saves and ArcMD's atomic publishes never initiate a scan.
func (a *App) attachmentsTouched(sp *space.Space, events []mygo.FileEvent) bool {
	folder := attachmentFolder(sp)
	if folder == "" || sp.IsExternal() {
		return false
	}
	under := func(p string) bool {
		if p == "" {
			return false
		}
		p = path.Clean(filepath.ToSlash(p))
		if p == "." {
			return folder == "."
		}
		if p == ".." || strings.HasPrefix(p, "../") || path.IsAbs(p) {
			return false
		}
		if folder == "." {
			return true
		}
		return p == folder || strings.HasPrefix(p, folder+"/")
	}
	a.pruneSelfWrites()
	for _, e := range events {
		switch e.Op {
		case mygo.FileRename:
			if e.OldPath == "" || strings.HasPrefix(path.Base(e.OldPath), ".") {
				continue
			}
			if full, err := vault.Resolve(sp.Root, filepath.ToSlash(e.Path)); err == nil && a.wroteRecently(full) {
				continue
			}
		default:
			continue // creation alone cannot identify the reference's old file
		}
		if under(e.Path) || under(e.OldPath) {
			return true
		}
		if e.Op == mygo.FileRename && path.Base(e.OldPath) == assetsDir {
			return true
		}
	}
	return false
}
