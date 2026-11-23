package bookdoc

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	neturl "net/url"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func parseHTMLBytes(filePath string, raw []byte) (*Document, error) {
	if len(raw) == 0 {
		return nil, ErrInvalid
	}
	text := decodeHTMLBytes(raw)
	root, err := html.Parse(strings.NewReader(text))
	if err != nil {
		return nil, ErrInvalid
	}
	meta := extractHTMLMeta(root)
	title := meta.title
	if title == "" && filePath != "" {
		base := path.Base(strings.ReplaceAll(filePath, `\`, "/"))
		title = strings.TrimSuffix(base, path.Ext(base))
	}
	toc := extractHTMLOutline(root)
	d := &Document{
		kind:        KindHTML,
		path:        filePath,
		title:       title,
		author:      meta.author,
		language:    meta.language,
		description: meta.description,
		rawSource:   text,
		chapters: []Chapter{{
			Index: 0,
			ID:    "html",
			Href:  filePath,
			Title: title,
		}},
		toc: toc,
	}
	return d, nil
}

// decodeHTMLBytes detects the character encoding of raw HTML/XHTML bytes (BOM,
// <meta charset>, XML encoding, and GB18030 heuristic for unlabeled legacy Chinese
// pages) and returns valid UTF-8 text with any leading UTF-8 BOM stripped.
func decodeHTMLBytes(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	if bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) {
		raw = raw[3:]
		if utf8.Valid(raw) {
			return string(raw)
		}
	}
	enc, encName, certain := charset.DetermineEncoding(raw, "text/html")
	if !certain && (encName == "windows-1252" || encName == "") && !utf8.Valid(raw) {
		if dec, err := simplifiedchinese.GB18030.NewDecoder().Bytes(raw); err == nil && utf8.Valid(dec) {
			s := string(dec)
			if !strings.ContainsRune(s, utf8.RuneError) && containsHan(s) {
				return s
			}
		}
	}
	if enc != nil {
		if decoded, err := io.ReadAll(enc.NewDecoder().Reader(bytes.NewReader(raw))); err == nil && len(decoded) > 0 {
			return strings.TrimPrefix(string(decoded), "\uFEFF")
		}
	}
	if utf8.Valid(raw) {
		return string(raw)
	}
	return strings.ToValidUTF8(string(raw), "\uFFFD")
}

func containsHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

type htmlMeta struct {
	title       string
	author      string
	language    string
	description string
}

func extractHTMLMeta(root *html.Node) htmlMeta {
	var m htmlMeta
	var firstH1 string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Html:
				if m.language == "" {
					m.language = strings.TrimSpace(nodeAttr(n, "lang"))
				}
			case atom.Title:
				if m.title == "" {
					m.title = cleanInlineText(n)
				}
			case atom.H1:
				if firstH1 == "" {
					firstH1 = cleanInlineText(n)
				}
			case atom.Meta:
				name := strings.ToLower(strings.TrimSpace(nodeAttr(n, "name")))
				prop := strings.ToLower(strings.TrimSpace(nodeAttr(n, "property")))
				content := strings.TrimSpace(nodeAttr(n, "content"))
				if content != "" {
					switch {
					case m.author == "" && (name == "author" || name == "dc.creator"):
						m.author = content
					case m.description == "" && (name == "description" || prop == "og:description"):
						m.description = content
					case m.title == "" && prop == "og:title":
						m.title = content
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if m.title == "" {
		m.title = firstH1
	}
	return m
}

func sniffHTMLTitle(src string) string {
	root, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return ""
	}
	var title, heading string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.H1, atom.H2, atom.H3:
				if heading == "" && !isNodeHidden(n) {
					heading = cleanInlineText(n)
				}
			case atom.Title:
				if title == "" {
					title = cleanInlineText(n)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if heading != "" {
		return heading
	}
	return title
}

func extractHTMLOutline(root *html.Node) []TOCItem {
	type rawHeading struct {
		level        int
		title        string
		anchor       string
		headingIndex int
	}
	var list []rawHeading
	headingIdx := 0
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.DocumentNode {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			return
		}
		if n.Type != html.ElementNode {
			return
		}
		switch n.DataAtom {
		case atom.Script, atom.Style, atom.Head, atom.Noscript, atom.Template:
			return
		}
		if isNodeHidden(n) {
			return
		}
		switch n.DataAtom {
		case atom.H1, atom.H2, atom.H3, atom.H4:
			title := cleanInlineText(n)
			if title != "" {
				anchor := strings.TrimSpace(nodeAttr(n, "id"))
				if anchor == "" {
					anchor = firstAnchorIn(n)
				}
				list = append(list, rawHeading{
					level:        int(n.Data[1] - '0'),
					title:        title,
					anchor:       anchor,
					headingIndex: headingIdx,
				})
				headingIdx++
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if len(list) == 0 {
		return nil
	}

	// Build hierarchical TOC tree using a stack of open pointers onto slice indices.
	type nodeBuilder struct {
		item     TOCItem
		level    int
		children []*nodeBuilder
	}
	var roots []*nodeBuilder
	var stack []*nodeBuilder
	for _, h := range list {
		nb := &nodeBuilder{
			level: h.level,
			item: TOCItem{
				Title:        h.title,
				Chapter:      0,
				Anchor:       h.anchor,
				HeadingIndex: h.headingIndex,
			},
		}
		for len(stack) > 0 && stack[len(stack)-1].level >= h.level {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			roots = append(roots, nb)
		} else {
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, nb)
		}
		stack = append(stack, nb)
	}
	var materialize func([]*nodeBuilder) []TOCItem
	materialize = func(nodes []*nodeBuilder) []TOCItem {
		if len(nodes) == 0 {
			return nil
		}
		out := make([]TOCItem, len(nodes))
		for i, nb := range nodes {
			out[i] = nb.item
			out[i].Children = materialize(nb.children)
		}
		return out
	}
	return materialize(roots)
}

func firstAnchorIn(n *html.Node) string {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			if id := strings.TrimSpace(nodeAttr(c, "id")); id != "" {
				return id
			}
			if c.DataAtom == atom.A {
				if name := strings.TrimSpace(nodeAttr(c, "name")); name != "" {
					return name
				}
			}
			if sub := firstAnchorIn(c); sub != "" {
				return sub
			}
		}
	}
	return ""
}

func extractHTMLPlainText(src string) string {
	root, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return strings.TrimSpace(src)
	}
	var out strings.Builder
	var cur strings.Builder
	flush := func() {
		s := strings.Join(strings.Fields(cur.String()), " ")
		cur.Reset()
		if s == "" {
			return
		}
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(s)
	}
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, pre bool) {
		switch n.Type {
		case html.TextNode:
			cur.WriteString(n.Data)
			return
		case html.DocumentNode:
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, pre)
			}
			return
		case html.ElementNode:
		default:
			return
		}
		switch n.DataAtom {
		case atom.Script, atom.Style, atom.Head, atom.Title, atom.Meta, atom.Link, atom.Noscript, atom.Template, atom.Svg:
			return
		}
		if isNodeHidden(n) {
			return
		}
		if n.DataAtom == atom.Br {
			flush()
			return
		}
		block := isBlockElement(n.DataAtom)
		if block {
			flush()
		}
		childPre := pre || n.DataAtom == atom.Pre
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, childPre)
		}
		if block {
			flush()
		}
	}
	walk(root, false)
	flush()
	return out.String()
}

func extractHTMLPreview(src string, maxParas int) (heading string, paras []string) {
	root, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return "", nil
	}
	var walk func(*html.Node) bool
	walk = func(n *html.Node) bool {
		if n.Type != html.ElementNode && n.Type != html.DocumentNode {
			return false
		}
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Script, atom.Style, atom.Head, atom.Title, atom.Meta, atom.Link, atom.Noscript, atom.Template, atom.Svg:
				return false
			}
			if isNodeHidden(n) {
				return false
			}
			switch n.DataAtom {
			case atom.H1, atom.H2, atom.H3:
				if t := cleanInlineText(n); t != "" {
					if heading == "" {
						heading = t
						return false
					}
					paras = append(paras, t)
					return len(paras) >= maxParas
				}
				return false
			case atom.P, atom.Li, atom.Blockquote, atom.Dd, atom.Figcaption:
				// Avoid duplicating nested p/li if a container wraps children.
				if n.DataAtom == atom.Blockquote && hasChildAtom(n, atom.P) {
					break
				}
				if t := cleanInlineText(n); t != "" {
					paras = append(paras, t)
					return len(paras) >= maxParas
				}
				return false
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if walk(c) {
				return true
			}
		}
		return false
	}
	walk(root)
	if heading == "" {
		heading = sniffHTMLTitle(src)
	}
	if len(paras) == 0 {
		for _, line := range strings.Split(extractHTMLPlainText(src), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || line == heading {
				continue
			}
			paras = append(paras, line)
			if len(paras) >= maxParas {
				break
			}
		}
	}
	return heading, paras
}

func isBlockElement(a atom.Atom) bool {
	switch a {
	case atom.P, atom.Div, atom.Section, atom.Article, atom.Header, atom.Footer,
		atom.Main, atom.Aside, atom.Nav, atom.Figure, atom.Figcaption,
		atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6,
		atom.Blockquote, atom.Pre, atom.Hr, atom.Li, atom.Dt, atom.Dd,
		atom.Tr, atom.Td, atom.Th, atom.Caption, atom.Summary, atom.Address, atom.Center:
		return true
	}
	return false
}

func isNodeHidden(n *html.Node) bool {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, "hidden") {
			return true
		}
		if strings.EqualFold(a.Key, "style") {
			css := strings.ToLower(strings.ReplaceAll(a.Val, " ", ""))
			if strings.Contains(css, "display:none") {
				return true
			}
		}
	}
	return false
}

func nodeAttr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}

func cleanInlineText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		switch cur.Type {
		case html.TextNode:
			b.WriteString(cur.Data)
		case html.ElementNode:
			switch cur.DataAtom {
			case atom.Script, atom.Style, atom.Noscript, atom.Template:
				return
			case atom.Br:
				b.WriteByte(' ')
				return
			}
			if isNodeHidden(cur) {
				return
			}
			for c := cur.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

func decodeDataURI(uri string) ([]byte, string, error) {
	if len(uri) < 5 || !strings.EqualFold(uri[:5], "data:") {
		return nil, "", errors.New("not a data URI")
	}
	rest := uri[5:]
	header, payload, ok := strings.Cut(rest, ",")
	if !ok {
		return nil, "", errors.New("malformed data URI")
	}
	parts := strings.Split(header, ";")
	mimeType := strings.TrimSpace(strings.ToLower(parts[0]))
	isBase64 := slices.ContainsFunc(parts[1:], func(p string) bool {
		return strings.EqualFold(strings.TrimSpace(p), "base64")
	})
	if len(payload) > maxAssetBytes*2 {
		return nil, "", errors.New("data URI exceeds limit")
	}
	if isBase64 {
		clean := strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, payload)
		b, err := base64.StdEncoding.DecodeString(clean)
		if err != nil {
			b, err = base64.RawStdEncoding.DecodeString(clean)
			if err != nil {
				b, err = base64.URLEncoding.DecodeString(clean)
			}
		}
		if err != nil || len(b) > maxAssetBytes {
			return nil, "", errors.New("invalid base64 data URI")
		}
		return b, mimeType, nil
	}
	decoded, err := neturl.PathUnescape(payload)
	if err != nil || len(decoded) > maxAssetBytes {
		return nil, "", errors.New("invalid escaped data URI")
	}
	return []byte(decoded), mimeType, nil
}
