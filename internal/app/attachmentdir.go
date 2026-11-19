package app

import (
	"io/fs"
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
// base ("" for the root): inside it without segments, otherwise ../ once
// per folder level.
func slashRel(base, target string) (string, bool) {
	if target == "" || strings.HasPrefix(target, "../") || target == ".." {
		return "", false
	}
	if base == "" || base == "." {
		return target, true
	}
	if strings.HasPrefix(target, base+"/") {
		return target[len(base)+1:], true
	}
	if base == ".." || strings.HasPrefix(base, "../") {
		return "", false
	}
	return strings.Repeat("../", strings.Count(base, "/")+1) + target, true
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
	a.relinkAttachments(sp, nil)
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
// exact-case first. A document is rewritten only when the list holds
// exactly one file, so two same-named pictures never bind a document to
// another document's screenshot.
func attachmentMatches(sp *space.Space, root, url string) []string {
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

// literalImageExists reports whether url still loads where the document
// points, without the attachment-folder fallback: relative to the document,
// or (with no parent segment) relative to the space's root.
func literalImageExists(root, docRel, url string) bool {
	if direct, err := localImageFile(root, docRel, url); err == nil {
		if info, stErr := os.Stat(direct); stErr == nil && !info.IsDir() {
			return true
		}
	}
	decoded := unescapeImagePath(url)
	decoded = strings.ReplaceAll(decoded, `\`, "/")
	if decoded == "" || path.IsAbs(decoded) || strings.ContainsAny(decoded, `:`+"\x00") || hasParentSegment(decoded) {
		return false
	}
	if _, ok := existingFile(root, decoded); ok {
		return true
	}
	return false
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
		moves[from] = to
	}
	return moves
}

// moveTarget follows a space-relative path through the moves: the path
// itself, or the folder holding it, went from one place to another. The
// most specific move wins.
func moveTarget(moves map[string]string, rel string) (string, bool) {
	if len(moves) == 0 || rel == "" {
		return "", false
	}
	bestFrom, bestTo := "", ""
	for from, to := range moves {
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
	return bestTo, bestFrom != ""
}

// attachmentRelocations maps the image URLs of a document that no longer
// load where they point to the place they now live under the space's
// attachment folder. A URL is rewritten only on solid ground: a recorded
// move that carried its old file there, or exactly one file of its name in
// the folder. Empty when nothing moved or no folder is configured.
func (a *App) attachmentRelocations(sp *space.Space, rel string, urls []string, moves map[string]string) map[string]string {
	if attachmentFolder(sp) == "" || sp.IsExternal() {
		return nil
	}
	root, docRel, err := fileLocation(sp, rel)
	if err != nil {
		return nil
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
		if url == "" || literalImageExists(root, docRel, url) {
			continue
		}
		// Where the reference pointed before the move: relative to the
		// document, or (as some older documents write it) to the root.
		var oldPlaces []string
		if direct, dErr := localImageFile(root, docRel, url); dErr == nil {
			if asset, rErr := filepath.Rel(root, direct); rErr == nil {
				if asset = filepath.ToSlash(asset); asset != ".." && !strings.HasPrefix(asset, "../") {
					oldPlaces = append(oldPlaces, asset)
				}
			}
		}
		decoded := unescapeImagePath(url)
		if !hasParentSegment(decoded) {
			if full, rErr := vault.Resolve(root, decoded); rErr == nil {
				if asset, rErr := filepath.Rel(root, full); rErr == nil {
					oldPlaces = append(oldPlaces, filepath.ToSlash(asset))
				}
			}
		}
		relocated := false
		for _, old := range oldPlaces {
			to, ok := moveTarget(moves, old)
			if !ok {
				continue
			}
			if _, ok := existingFile(root, to); ok {
				if next := docRelativeURL(docDir, to); next != url {
					set(url, next)
				}
				relocated = true
				break
			}
		}
		if relocated {
			continue
		}
		if matches := attachmentMatches(sp, root, url); len(matches) == 1 {
			if next := docRelativeURL(docDir, matches[0]); next != url {
				set(url, next)
			}
		}
	}
	return moved
}

// relinkAttachments rewrites the image references of the space's documents
// to the space's attachment folder once their files moved there by hand.
// The documents kept open are rebased in place (their autosave writes the
// text); the others are scanned in the background and rewritten on disk,
// keeping a version of the previous content in the document's history.
// Nothing is written when no reference needs it.
func (a *App) relinkAttachments(sp *space.Space, moves map[string]string) {
	if sp == nil || sp.IsExternal() || attachmentFolder(sp) == "" {
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
		if d.loadErr != nil || d.isWhiteboard() || d.source || d.ed.ReadOnly {
			continue
		}
		if moved := a.attachmentRelocations(sp, d.rel, d.ed.ImageURLs(), moves); len(moved) > 0 {
			d.ed.RelocateImages(moved)
		}
	}
	docs, err := vault.List(sp.Root)
	if err != nil {
		return
	}
	type closed struct{ rel, text string }
	var pending []closed
	for _, info := range docs {
		if resident[info.Path] || vault.IsWhiteboard(info.Path) {
			continue
		}
		text, err := readFile(sp, info.Path)
		if err != nil {
			continue
		}
		pending = append(pending, closed{info.Path, text})
	}
	apply := func() {
		for _, p := range pending {
			a.relinkClosedDoc(sp, p.rel, p.text, moves)
		}
	}
	if a.win != nil {
		go apply()
	} else {
		apply()
	}
}

// relinkClosedDoc rewrites one closed document on disk. It is cheap to
// decline: documents without picture syntax are skipped before any
// Markdown is parsed, the file is compared again right before writing so a
// change made meanwhile is never clobbered, and the content as it was is
// kept as a version first.
func (a *App) relinkClosedDoc(sp *space.Space, rel, text string, moves map[string]string) {
	// Pictures are ![alt](url), ![[obsidian]] or, in legacy .arcmd JSON,
	// "image" blocks.
	if !strings.Contains(text, "![") && !strings.Contains(text, `"image"`) {
		return
	}
	doc, err := imageDocument(rel, text)
	if err != nil {
		return
	}
	var urls []string
	doc.MapImages(func(url string) string {
		if url != "" && !slices.Contains(urls, url) {
			urls = append(urls, url)
		}
		return url
	})
	if len(urls) == 0 {
		return
	}
	moved := a.attachmentRelocations(sp, rel, urls, moves)
	if len(moved) == 0 {
		return
	}
	out, err := relocateText(rel, text, moved)
	if err != nil || out == text {
		return
	}
	if now, err := readFile(sp, rel); err != nil || now != text {
		return // the reader changed it meanwhile
	}
	root, name, err := fileLocation(sp, rel)
	if err != nil {
		return
	}
	_ = history.Record(root, rel, text, time.Now(), true)
	_ = vault.Write(root, name, out)
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

// isAttachmentName reports whether a created file is the kind an attachment
// folder holds: a picture or a whiteboard. Documents are left out, so
// making one never starts a relink scan.
func isAttachmentName(p string) bool {
	ext := strings.ToLower(vault.FileExt(p))
	if slices.Contains(imageExtensions, strings.TrimPrefix(ext, ".")) {
		return true
	}
	return ext == vault.ExtWhiteboard || ext == vault.ExtWhiteboardRaw
}

// attachmentsTouched reports whether a batch of watch events may have moved
// attachments into the space's folder, so the documents should be relinked.
// ArcMD's own writes are left out: plain saves (FileWrite), its atomic
// publishes (renames of dot-named temporaries) and the attachment files it
// just wrote.
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
		case mygo.FileWrite, mygo.FileRemove:
			continue
		case mygo.FileRename:
			if e.OldPath == "" || strings.HasPrefix(path.Base(e.OldPath), ".") {
				continue
			}
		case mygo.FileCreate:
			if !isAttachmentName(e.Path) {
				continue
			}
			if full, err := vault.Resolve(sp.Root, filepath.ToSlash(e.Path)); err == nil && a.wroteRecently(full) {
				continue
			}
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
