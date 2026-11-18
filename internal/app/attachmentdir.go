package app

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
)

// attachmentFolder is the folder the space keeps its attachments in, as a
// slash path relative to the space's root. Empty keeps the older
// arrangement: an assets folder beside each document.
func attachmentFolder(sp *space.Space) string {
	if sp == nil || sp.IsExternal() {
		return ""
	}
	return sp.AttachmentsDir
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
// absolute path is turned into one inside the space, and "" goes back to an
// assets folder beside each document. Documents already pointing where the
// folder moved to are relinked, then spaces.json keeps the choice.
func (a *App) setAttachmentsFolder(sp *space.Space, dir string) {
	if sp == nil || sp.IsExternal() {
		return
	}
	rel := dir
	if filepath.IsAbs(dir) {
		r, err := filepath.Rel(sp.Root, dir)
		if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			a.notify("附件目录必须在空间文件夹内。")
			return
		}
		rel = filepath.ToSlash(r)
	}
	if !sp.SetAttachmentsDir(rel) {
		a.notify("附件目录必须在空间文件夹内。")
		return
	}
	a.saveState()
	a.relinkAttachments(sp)
	label := sp.AttachmentsDir
	if label == "" {
		label = i18n.T("文档旁的 assets")
	}
	a.notify("附件目录已设为「%s」。", label)
}

// attachmentCandidate finds url under the space's attachment folder, which
// is where the reader put attachments after moving them by hand. It returns
// the URL the document may point to (relative to the space's root) and the
// file on disk.
func attachmentCandidate(sp *space.Space, root, url string) (relURL, full string, ok bool) {
	folder := attachmentFolder(sp)
	if folder == "" {
		return "", "", false
	}
	decoded := unescapeImagePath(url)
	decoded = strings.ReplaceAll(decoded, `\`, "/")
	if decoded == "" || path.IsAbs(decoded) || strings.ContainsAny(decoded, `:`+"\x00") {
		return "", "", false
	}
	clean := path.Clean(decoded)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", "", false
	}
	base := path.Base(clean)
	if base == "." || base == "/" || base == "" {
		return "", "", false
	}
	var candidates []string
	add := func(rel string) {
		rel = path.Clean(rel)
		if rel == "." || slices.Contains(candidates, rel) {
			return
		}
		candidates = append(candidates, rel)
	}
	// The folder itself moved under the attachment folder, or only its
	// contents did; an assets/ prefix is the common old arrangement.
	add(path.Join(folder, clean))
	add(path.Join(folder, base))
	if strings.HasPrefix(clean, assetsDir+"/") {
		add(path.Join(folder, strings.TrimPrefix(clean, assetsDir+"/")))
	}
	for _, rel := range candidates {
		if file, ok := existingFile(root, rel); ok {
			return rel, file, true
		}
	}
	// The file sits deeper in the attachment folder: match it by name.
	if rel, ok := findInAttachmentFolder(root, folder, base); ok {
		if file, ok := existingFile(root, rel); ok {
			return rel, file, true
		}
	}
	return "", "", false
}

// findInAttachmentFolder looks for a file named name anywhere under folder,
// preferring an exact case match and then the shallowest path.
func findInAttachmentFolder(root, folder, name string) (string, bool) {
	if name == "" || name == "." || name == "/" {
		return "", false
	}
	base, ok := folderPath(root, folder)
	if !ok {
		return "", false
	}
	prefix := ""
	if folder != "." {
		prefix = folder + "/"
	}
	var bestRel string
	bestExact, bestDepth := false, 0
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
		exact := d.Name() == name
		depth := strings.Count(rel, "/")
		if bestRel == "" || (exact && !bestExact) ||
			(exact == bestExact && (depth < bestDepth || (depth == bestDepth && rel < bestRel))) {
			bestRel, bestExact, bestDepth = rel, exact, depth
		}
		return nil
	})
	return bestRel, bestRel != ""
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

// attachmentRelocations maps the image URLs of a document that no longer
// load where they point to the place they now live under the space's
// attachment folder. Empty when nothing moved or no folder is configured.
func (a *App) attachmentRelocations(sp *space.Space, rel string, urls []string) map[string]string {
	if attachmentFolder(sp) == "" || sp.IsExternal() {
		return nil
	}
	root, docRel, err := fileLocation(sp, rel)
	if err != nil {
		return nil
	}
	var moved map[string]string
	for _, url := range urls {
		if url == "" || literalImageExists(root, docRel, url) {
			continue
		}
		to, _, ok := attachmentCandidate(sp, root, url)
		if !ok || to == url {
			continue
		}
		if moved == nil {
			moved = map[string]string{}
		}
		moved[url] = to
	}
	return moved
}

// relinkAttachments rewrites the image references of the space's documents
// to the space's attachment folder once their files moved there by hand.
// Open documents keep their edits: their URLs are rebased in memory and
// saved by autosave, while documents closed in the background are rewritten
// in place. Nothing is written when no reference needs it.
func (a *App) relinkAttachments(sp *space.Space) {
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
		if moved := a.attachmentRelocations(sp, d.rel, d.ed.ImageURLs()); len(moved) > 0 {
			d.ed.RelocateImages(moved)
		}
	}
	docs, err := vault.List(sp.Root)
	if err != nil {
		return
	}
	for _, info := range docs {
		if resident[info.Path] || vault.IsWhiteboard(info.Path) {
			continue
		}
		text, err := readFile(sp, info.Path)
		if err != nil {
			continue
		}
		d := openDocument(sp, info.Path, text, false)
		if d.loadErr != nil {
			continue
		}
		moved := a.attachmentRelocations(sp, info.Path, d.ed.ImageURLs())
		if len(moved) == 0 {
			continue
		}
		out, err := relocateText(info.Path, text, moved)
		if err != nil || out == text {
			continue
		}
		_ = writeFile(sp, info.Path, out)
	}
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

// attachmentsTouched reports whether file events may have moved attachments
// into the space's folder, so the documents should be relinked.
func attachmentsTouched(sp *space.Space, events []mygo.FileEvent) bool {
	folder := attachmentFolder(sp)
	if folder == "" {
		return false
	}
	under := func(p string) bool {
		if p == "" || p == "." {
			return true
		}
		if folder == "." {
			return true
		}
		return p == folder || strings.HasPrefix(p, folder+"/")
	}
	for _, e := range events {
		if e.Op == mygo.FileWrite {
			continue // saving a document never moves an attachment
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
