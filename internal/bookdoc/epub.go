package bookdoc

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	algIDPFEmbedding  = "http://www.idpf.org/2008/embedding"
	algAdobeFontObfus = "http://ns.adobe.com/pdf/enc#RC"
)

func parseEPUB(filePath string, zr *zip.Reader, f *os.File) (*Document, error) {
	if zr == nil {
		return nil, ErrInvalid
	}
	d := &Document{
		kind:         KindEPUB,
		path:         filePath,
		file:         f,
		zr:           zr,
		entries:      make(map[string]*zip.File, len(zr.File)),
		lowerEntries: make(map[string]*zip.File, len(zr.File)),
		spineByPath:  make(map[string]int),
		spineLower:   make(map[string]int),
	}
	var firstOPF string
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		norm := normalizeZipPath(zf.Name)
		if norm == "" {
			continue
		}
		d.entries[norm] = zf
		low := strings.ToLower(norm)
		if _, exists := d.lowerEntries[low]; !exists {
			d.lowerEntries[low] = zf
		}
		if firstOPF == "" && strings.HasSuffix(low, ".opf") {
			firstOPF = norm
		}
	}

	opfPath := ""
	if containerRaw, err := d.readZipLocked("META-INF/container.xml", 1<<20); err == nil {
		opfPath = parseContainerOPFPath(containerRaw)
	}
	if opfPath == "" || d.findZipFileLocked(opfPath) == nil {
		opfPath = firstOPF
	}
	if opfPath == "" {
		return nil, ErrInvalid
	}
	opfRaw, err := d.readZipLocked(opfPath, 4<<20)
	if err != nil {
		return nil, ErrInvalid
	}
	pkg, err := parseOPF(opfRaw)
	if err != nil {
		return nil, ErrInvalid
	}

	d.title = firstNonEmpty(pkg.Titles)
	if d.title == "" {
		d.title = strings.TrimSuffix(path.Base(strings.ReplaceAll(filePath, `\`, "/")), path.Ext(filePath))
	}
	d.author = firstNonEmpty(pkg.Creators)
	d.language = firstNonEmpty(pkg.Languages)
	d.description = strings.TrimSpace(firstNonEmpty(pkg.Descriptions))

	opfDir := path.Dir(opfPath)
	if opfDir == "." {
		opfDir = ""
	}
	type manifestEntry struct {
		id         string
		zipPath    string
		mediaType  string
		properties []string
	}
	manifestByID := make(map[string]manifestEntry, len(pkg.Manifest))
	var navEntry, ncxEntry, coverEntry *manifestEntry
	for _, item := range pkg.Manifest {
		zp, ok := resolveRelativeZipPath(opfPath, item.Href)
		if !ok {
			continue
		}
		props := strings.Fields(strings.ToLower(item.Properties))
		ent := manifestEntry{
			id:         item.ID,
			zipPath:    zp,
			mediaType:  strings.ToLower(strings.TrimSpace(item.MediaType)),
			properties: props,
		}
		manifestByID[item.ID] = ent
		copyEnt := ent
		if navEntry == nil && slices.Contains(props, "nav") {
			navEntry = &copyEnt
		}
		if ncxEntry == nil && ent.mediaType == "application/x-dtbncx+xml" {
			ncxEntry = &copyEnt
		}
		if coverEntry == nil && slices.Contains(props, "cover-image") && isImageManifest(ent.mediaType, ent.zipPath) {
			coverEntry = &copyEnt
		}
	}

	if pkg.Spine.Toc != "" {
		if tocEnt, ok := manifestByID[pkg.Spine.Toc]; ok {
			ncxEntry = &tocEnt
		}
	}

	// Cover discovery fallbacks: <meta name="cover" content="id">, then manifest heuristic.
	if coverEntry == nil {
		for _, m := range pkg.Meta {
			if strings.EqualFold(strings.TrimSpace(m.Name), "cover") {
				cid := strings.TrimSpace(m.Content)
				if ent, ok := manifestByID[cid]; ok && isImageManifest(ent.mediaType, ent.zipPath) {
					c := ent
					coverEntry = &c
					break
				}
			}
		}
	}
	if coverEntry == nil {
		for _, item := range pkg.Manifest {
			ent, ok := manifestByID[item.ID]
			if !ok || !isImageManifest(ent.mediaType, ent.zipPath) {
				continue
			}
			if strings.Contains(strings.ToLower(ent.id), "cover") || strings.Contains(strings.ToLower(ent.zipPath), "cover") {
				c := ent
				coverEntry = &c
				break
			}
		}
	}
	if coverEntry != nil {
		d.coverPath = coverEntry.zipPath
		d.coverMIME = coverEntry.mediaType
	}

	for _, ref := range pkg.Spine.Itemrefs {
		ent, ok := manifestByID[ref.IDRef]
		if !ok {
			continue
		}
		if d.findZipFileLocked(ent.zipPath) == nil {
			continue
		}
		idx := len(d.chapters)
		d.chapters = append(d.chapters, Chapter{
			Index: idx,
			ID:    ent.id,
			Href:  ent.zipPath,
		})
		d.spineByPath[ent.zipPath] = idx
		low := strings.ToLower(ent.zipPath)
		if _, exists := d.spineLower[low]; !exists {
			d.spineLower[low] = idx
		}
	}
	if len(d.chapters) == 0 {
		return nil, ErrInvalid
	}

	// Check META-INF/encryption.xml for DRM on content documents while ignoring
	// IDPF/Adobe font obfuscation.
	if encRaw, err := d.readZipLocked("META-INF/encryption.xml", 1<<20); err == nil {
		if d.hasDRMContent(encRaw) {
			return nil, ErrEncrypted
		}
	}

	// Parse TOC: EPUB 3 nav document -> EPUB 2 NCX -> fallback spine headings.
	if navEntry != nil {
		if navRaw, err := d.readZipLocked(navEntry.zipPath, 4<<20); err == nil {
			d.toc = d.parseEPUB3Nav(navEntry.zipPath, decodeHTMLBytes(navRaw))
		}
	}
	if len(d.toc) == 0 && ncxEntry != nil {
		if ncxRaw, err := d.readZipLocked(ncxEntry.zipPath, 4<<20); err == nil {
			d.toc = d.parseNCX(ncxEntry.zipPath, ncxRaw)
		}
	}

	// Populate chapter titles from TOC entries first, then lightweight heading fallback.
	d.populateChapterTitles()
	if len(d.toc) == 0 {
		for i, ch := range d.chapters {
			d.toc = append(d.toc, TOCItem{
				Title:        ch.Title,
				Href:         ch.Href,
				Chapter:      i,
				HeadingIndex: -1,
			})
		}
	}
	return d, nil
}

func (d *Document) findZipFileLocked(zipPath string) *zip.File {
	norm := normalizeZipPath(zipPath)
	if zf, ok := d.entries[norm]; ok {
		return zf
	}
	if zf, ok := d.lowerEntries[strings.ToLower(norm)]; ok {
		return zf
	}
	return nil
}

func (d *Document) readZipPrefixLocked(zipPath string, limit int64) ([]byte, error) {
	zf := d.findZipFileLocked(zipPath)
	if zf == nil {
		return nil, os.ErrNotExist
	}
	rc, err := zf.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(io.LimitReader(rc, limit))
}

func (d *Document) readZipLocked(zipPath string, limit int64) ([]byte, error) {
	zf := d.findZipFileLocked(zipPath)
	if zf == nil {
		return nil, os.ErrNotExist
	}
	if zf.UncompressedSize64 > uint64(limit)*4 && zf.UncompressedSize64 > 64<<20 {
		return nil, ErrInvalid
	}
	rc, err := zf.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrInvalid
	}
	return data, nil
}

func (d *Document) lookupSpineIndex(zipPath string) (int, bool) {
	norm := normalizeZipPath(zipPath)
	if idx, ok := d.spineByPath[norm]; ok {
		return idx, true
	}
	if idx, ok := d.spineLower[strings.ToLower(norm)]; ok {
		return idx, true
	}
	return -1, false
}

func parseContainerOPFPath(raw []byte) string {
	type rootfile struct {
		FullPath  string `xml:"full-path,attr"`
		MediaType string `xml:"media-type,attr"`
	}
	type container struct {
		Rootfiles []rootfile `xml:"rootfiles>rootfile"`
	}
	var c container
	if err := xml.Unmarshal(raw, &c); err != nil {
		return ""
	}
	fallback := ""
	for _, rf := range c.Rootfiles {
		fp := normalizeZipPath(rf.FullPath)
		if fp == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(rf.MediaType), "application/oebps-package+xml") {
			return fp
		}
		if fallback == "" {
			fallback = fp
		}
	}
	return fallback
}

type opfPackage struct {
	Titles       []string      `xml:"metadata>title"`
	Creators     []string      `xml:"metadata>creator"`
	Languages    []string      `xml:"metadata>language"`
	Descriptions []string      `xml:"metadata>description"`
	Meta         []opfMeta     `xml:"metadata>meta"`
	Manifest     []opfItem     `xml:"manifest>item"`
	Spine        opfSpine      `xml:"spine"`
	Guide        []opfGuideRef `xml:"guide>reference"`
}

type opfMeta struct {
	Name     string `xml:"name,attr"`
	Content  string `xml:"content,attr"`
	Property string `xml:"property,attr"`
	Value    string `xml:",chardata"`
}

type opfItem struct {
	ID         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}

type opfSpine struct {
	Toc      string       `xml:"toc,attr"`
	Itemrefs []opfItemref `xml:"itemref"`
}

type opfItemref struct {
	IDRef  string `xml:"idref,attr"`
	Linear string `xml:"linear,attr"`
}

type opfGuideRef struct {
	Type  string `xml:"type,attr"`
	Title string `xml:"title,attr"`
	Href  string `xml:"href,attr"`
}

func parseOPF(raw []byte) (*opfPackage, error) {
	var pkg opfPackage
	dec := xml.NewDecoder(bytes.NewReader(raw))
	dec.Strict = false
	if err := dec.Decode(&pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
}

func (d *Document) hasDRMContent(raw []byte) bool {
	type encMethod struct {
		Algorithm string `xml:"Algorithm,attr"`
	}
	type cipherRef struct {
		URI string `xml:"URI,attr"`
	}
	type cipherData struct {
		Ref cipherRef `xml:"CipherReference"`
	}
	type encData struct {
		Method encMethod  `xml:"EncryptionMethod"`
		Cipher cipherData `xml:"CipherData"`
	}
	type encryptionDoc struct {
		Items []encData `xml:"EncryptedData"`
	}
	var doc encryptionDoc
	dec := xml.NewDecoder(bytes.NewReader(raw))
	dec.Strict = false
	if err := dec.Decode(&doc); err != nil {
		return false
	}
	for _, it := range doc.Items {
		alg := strings.TrimSpace(it.Method.Algorithm)
		if alg == algIDPFEmbedding || alg == algAdobeFontObfus {
			continue
		}
		uri := normalizeZipPath(it.Cipher.Ref.URI)
		ext := strings.ToLower(path.Ext(uri))
		switch ext {
		case ".ttf", ".otf", ".woff", ".woff2", ".eot":
			continue
		}
		if uri == "" {
			return true
		}
		if _, isSpine := d.lookupSpineIndex(uri); isSpine {
			return true
		}
		switch ext {
		case ".xhtml", ".html", ".htm", ".xml", ".svg":
			return true
		}
	}
	return false
}

func (d *Document) parseEPUB3Nav(navHref, markup string) []TOCItem {
	root, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return nil
	}
	var tocNav, fallbackNav *html.Node
	var findNav func(*html.Node)
	findNav = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Nav {
			isTOC := false
			for _, a := range n.Attr {
				key := strings.ToLower(a.Key)
				val := strings.ToLower(a.Val)
				if (key == "epub:type" || key == "type" || strings.HasSuffix(key, ":type")) && slices.Contains(strings.Fields(val), "toc") {
					isTOC = true
					break
				}
				if key == "role" && strings.Contains(val, "doc-toc") {
					isTOC = true
					break
				}
			}
			if isTOC && tocNav == nil {
				tocNav = n
				return
			}
			if fallbackNav == nil && hasChildAtom(n, atom.Ol) {
				fallbackNav = n
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findNav(c)
		}
	}
	findNav(root)
	target := tocNav
	if target == nil {
		target = fallbackNav
	}
	if target == nil {
		return nil
	}
	ol := firstDescendantAtom(target, atom.Ol)
	if ol == nil {
		return nil
	}
	return d.walkNavList(navHref, ol)
}

func (d *Document) walkNavList(baseHref string, listNode *html.Node) []TOCItem {
	var out []TOCItem
	for li := listNode.FirstChild; li != nil; li = li.NextSibling {
		if li.Type != html.ElementNode || li.DataAtom != atom.Li {
			continue
		}
		var title, rawHref string
		var nestedOL *html.Node
		for c := li.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.DataAtom {
			case atom.A:
				if title == "" {
					title = cleanInlineText(c)
					rawHref = nodeAttr(c, "href")
				}
			case atom.Span:
				if title == "" {
					title = cleanInlineText(c)
				}
			case atom.Ol, atom.Ul:
				if nestedOL == nil {
					nestedOL = c
				}
			default:
				if title == "" {
					if a := firstDescendantAtom(c, atom.A); a != nil {
						title = cleanInlineText(a)
						rawHref = nodeAttr(a, "href")
					}
				}
			}
		}
		if title == "" && rawHref == "" {
			if nestedOL != nil {
				out = append(out, d.walkNavList(baseHref, nestedOL)...)
			}
			continue
		}
		chIdx, anchor := d.resolveTOCHref(baseHref, rawHref)
		item := TOCItem{
			Title:        title,
			Href:         rawHref,
			Chapter:      chIdx,
			Anchor:       anchor,
			HeadingIndex: -1,
		}
		if nestedOL != nil {
			item.Children = d.walkNavList(baseHref, nestedOL)
		}
		if item.Title != "" || len(item.Children) > 0 {
			if item.Title == "" {
				item.Title = "…"
			}
			out = append(out, item)
		}
	}
	return out
}

func (d *Document) parseNCX(ncxHref string, raw []byte) []TOCItem {
	type ncxContent struct {
		Src string `xml:"src,attr"`
	}
	type ncxLabel struct {
		Text string `xml:"text"`
	}
	type navPoint struct {
		Label    ncxLabel   `xml:"navLabel"`
		Content  ncxContent `xml:"content"`
		Children []navPoint `xml:"navPoint"`
	}
	type ncxDoc struct {
		NavMap []navPoint `xml:"navMap>navPoint"`
	}
	var doc ncxDoc
	dec := xml.NewDecoder(bytes.NewReader(raw))
	dec.Strict = false
	if err := dec.Decode(&doc); err != nil {
		return nil
	}
	var convert func([]navPoint) []TOCItem
	convert = func(pts []navPoint) []TOCItem {
		if len(pts) == 0 {
			return nil
		}
		out := make([]TOCItem, 0, len(pts))
		for _, pt := range pts {
			title := strings.Join(strings.Fields(pt.Label.Text), " ")
			rawHref := strings.TrimSpace(pt.Content.Src)
			chIdx, anchor := d.resolveTOCHref(ncxHref, rawHref)
			children := convert(pt.Children)
			if title == "" && len(children) == 0 {
				continue
			}
			out = append(out, TOCItem{
				Title:        title,
				Href:         rawHref,
				Chapter:      chIdx,
				Anchor:       anchor,
				HeadingIndex: -1,
				Children:     children,
			})
		}
		return out
	}
	return convert(doc.NavMap)
}

func (d *Document) resolveTOCHref(baseHref, rawHref string) (chapter int, anchor string) {
	rawHref = strings.TrimSpace(rawHref)
	if rawHref == "" {
		return -1, ""
	}
	filePart, frag, _ := strings.Cut(rawHref, "#")
	filePart, _, _ = strings.Cut(filePart, "?")
	filePart = strings.TrimSpace(filePart)
	if filePart == "" {
		return -1, unescapeFragment(frag)
	}
	zp, ok := resolveRelativeZipPath(baseHref, filePart)
	if !ok {
		return -1, ""
	}
	idx, found := d.lookupSpineIndex(zp)
	if !found {
		return -1, ""
	}
	return idx, unescapeFragment(frag)
}

func (d *Document) populateChapterTitles() {
	var walk func([]TOCItem)
	walk = func(items []TOCItem) {
		for _, it := range items {
			if it.Chapter >= 0 && it.Chapter < len(d.chapters) && d.chapters[it.Chapter].Title == "" && it.Title != "" {
				d.chapters[it.Chapter].Title = it.Title
			}
			walk(it.Children)
		}
	}
	walk(d.toc)

	for i := range d.chapters {
		if d.chapters[i].Title != "" {
			continue
		}
		if raw, err := d.readZipPrefixLocked(d.chapters[i].Href, 64<<10); err == nil && len(raw) > 0 {
			if t := sniffHTMLTitle(decodeHTMLBytes(raw)); t != "" {
				d.chapters[i].Title = t
			}
		}
		if d.chapters[i].Title == "" {
			d.chapters[i].Title = fmt.Sprintf("第 %d 章", i+1)
		}
	}
}

func isImageManifest(mediaType, zipPath string) bool {
	if strings.HasPrefix(mediaType, "image/") {
		return true
	}
	switch strings.ToLower(path.Ext(zipPath)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp", ".avif":
		return true
	}
	return false
}

func firstNonEmpty(items []string) string {
	for _, s := range items {
		if t := strings.Join(strings.Fields(s), " "); t != "" {
			return t
		}
	}
	return ""
}

func hasChildAtom(n *html.Node, a atom.Atom) bool {
	return firstDescendantAtom(n, a) != nil
}

func firstDescendantAtom(n *html.Node, a atom.Atom) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.DataAtom == a {
			return c
		}
		if found := firstDescendantAtom(c, a); found != nil {
			return found
		}
	}
	return nil
}
