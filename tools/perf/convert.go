package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/tools/internal/toolutil"
)

type xmlPart struct {
	text string
	node *xmlNode
}
type xmlNode struct {
	tag   string
	attrs map[string]string
	parts []xmlPart
	raw   string
}

func (n *xmlNode) children() []*xmlNode {
	var result []*xmlNode
	for _, p := range n.parts {
		if p.node != nil {
			result = append(result, p.node)
		}
	}
	return result
}
func (n *xmlNode) text() string {
	var b strings.Builder
	for _, p := range n.parts {
		if p.node != nil {
			b.WriteString(p.node.text())
		} else {
			b.WriteString(p.text)
		}
	}
	return b.String()
}
func parseXML(raw string) (*xmlNode, error) {
	source := "<document>" + raw + "</document>"
	d := xml.NewDecoder(strings.NewReader(source))
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	return readNode(d, token.(xml.StartElement), 0, source)
}
func readNode(d *xml.Decoder, start xml.StartElement, offset int64, source string) (*xmlNode, error) {
	if start.Name.Space != "" {
		return nil, fmt.Errorf("XML namespaces are unsupported")
	}
	n := &xmlNode{tag: start.Name.Local, attrs: map[string]string{}}
	for _, a := range start.Attr {
		n.attrs[a.Name.Local] = a.Value
	}
	for {
		before := d.InputOffset()
		token, err := d.Token()
		if err != nil {
			return nil, err
		}
		switch token := token.(type) {
		case xml.StartElement:
			child, err := readNode(d, token, before, source)
			if err != nil {
				return nil, err
			}
			n.parts = append(n.parts, xmlPart{node: child})
		case xml.CharData:
			n.parts = append(n.parts, xmlPart{text: string(token)})
		case xml.EndElement:
			n.raw = source[offset:d.InputOffset()]
			return n, nil
		default:
			return nil, fmt.Errorf("unsupported XML token in %s", n.tag)
		}
	}
}
func inlineRuns(n *xmlNode, style model.Style) ([]model.Run, error) {
	switch n.tag {
	case "b":
		style.Bold = true
	case "i":
		style.Italic = true
	case "u":
		style.Underline = true
	case "s":
		style.Strike = true
	case "span":
		if v, ok := n.attrs["text-color"]; ok {
			style.Color = v
		}
		if v, ok := n.attrs["background-color"]; ok {
			style.Highlight = v
		}
	}
	var result []model.Run
	for _, part := range n.parts {
		if part.node == nil {
			if part.text != "" {
				result = append(result, model.Run{Text: part.text, Style: style})
			}
			continue
		}
		switch part.node.tag {
		case "b", "i", "u", "s", "span":
		default:
			return nil, fmt.Errorf("unsupported inline tag: %s", part.node.tag)
		}
		runs, err := inlineRuns(part.node, style)
		if err != nil {
			return nil, err
		}
		result = append(result, runs...)
	}
	return result, nil
}

type converter struct{ serial int }

func (c *converter) block(kind string) *model.Block {
	c.serial++
	return &model.Block{ID: fmt.Sprintf("perf%020d", c.serial), Kind: kind}
}
func onlyChild(n *xmlNode, tag string) (*xmlNode, error) {
	children := n.children()
	if len(children) != 1 || children[0].tag != tag {
		return nil, fmt.Errorf("%s requires one %s child", n.tag, tag)
	}
	return children[0], nil
}
func (c *converter) blocks(n *xmlNode) ([]*model.Block, error) {
	textBlock := func(kind string, source *xmlNode) (*model.Block, error) {
		runs, err := inlineRuns(source, model.Style{})
		if err != nil {
			return nil, err
		}
		b := c.block(kind)
		b.Runs = runs
		return b, nil
	}
	var b *model.Block
	var err error
	switch n.tag {
	case "title", "h1", "h2", "h3", "h4", "h5", "h6":
		b, err = textBlock("heading", n)
		if err != nil {
			return nil, err
		}
		b.Level = 1
		if n.tag != "title" {
			b.Level = int(n.tag[1] - '0')
		}
	case "p":
		b, err = textBlock("paragraph", n)
	case "ul", "ol":
		var out []*model.Block
		kind := "bullet"
		if n.tag == "ol" {
			kind = "numbered"
		}
		for _, li := range n.children() {
			if li.tag != "li" {
				return nil, fmt.Errorf("%s requires li children", n.tag)
			}
			item, err := textBlock(kind, li)
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
		return out, nil
	case "checkbox":
		b, err = textBlock("task", n)
		if err == nil {
			b.Checked = n.attrs["done"] == "true"
		}
	case "callout", "blockquote":
		p, e := onlyChild(n, "p")
		if e != nil {
			return nil, e
		}
		kind := "quote"
		if n.tag == "callout" {
			kind = "callout"
		}
		b, err = textBlock(kind, p)
		if err == nil && n.tag == "callout" {
			colors := map[string]string{"blue": "", "yellow": "yellow", "orange": "yellow", "green": "green", "purple": "purple"}
			color, ok := colors[n.attrs["border-color"]]
			if !ok {
				return nil, fmt.Errorf("unsupported callout color %q", n.attrs["border-color"])
			}
			b.Color = color
			b.Emoji = n.attrs["emoji"]
		}
	case "pre":
		code, e := onlyChild(n, "code")
		if e != nil {
			return nil, e
		}
		if len(code.children()) != 0 {
			return nil, fmt.Errorf("code must contain plain text")
		}
		b = c.block("code")
		b.Language = n.attrs["lang"]
		b.Runs = []model.Run{{Text: code.text()}}
	case "table":
		var rows []*model.Block
		var widths []float32
		for _, group := range n.children() {
			switch group.tag {
			case "colgroup":
				for _, col := range group.children() {
					if col.tag != "col" {
						return nil, fmt.Errorf("invalid column")
					}
					v, e := strconv.ParseFloat(col.attrs["width"], 32)
					if e != nil {
						return nil, e
					}
					widths = append(widths, float32(v))
				}
			case "thead", "tbody":
				for _, tr := range group.children() {
					if tr.tag != "tr" {
						return nil, fmt.Errorf("invalid table row")
					}
					var cells []*model.Block
					for _, cell := range tr.children() {
						if cell.tag != "th" && cell.tag != "td" {
							return nil, fmt.Errorf("invalid table cell")
						}
						p, e := onlyChild(cell, "p")
						if e != nil {
							return nil, e
						}
						item, e := textBlock("cell", p)
						if e != nil {
							return nil, e
						}
						item.Background = cell.attrs["background-color"]
						cells = append(cells, item)
					}
					row := c.block("row")
					row.Children = cells
					rows = append(rows, row)
				}
			default:
				return nil, fmt.Errorf("unsupported table child %s", group.tag)
			}
		}
		b = c.block("table")
		b.Widths = widths
		b.Children = rows
	default:
		return nil, fmt.Errorf("unsupported block tag: %s", n.tag)
	}
	if err != nil {
		return nil, err
	}
	return []*model.Block{b}, nil
}
func walkBlocks(blocks []*model.Block, fn func(*model.Block)) {
	for _, b := range blocks {
		fn(b)
		walkBlocks(b.Children, fn)
	}
}
func blockText(blocks []*model.Block) string {
	var s strings.Builder
	walkBlocks(blocks, func(b *model.Block) {
		for _, r := range b.Runs {
			s.WriteString(r.Text)
		}
	})
	return s.String()
}
func printJSON(value any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(value)
}

type variant struct {
	Characters     int `json:"characters"`
	TopLevelBlocks int `json:"top_level_blocks"`
}
type fixtureManifest struct {
	SourceSHA      string             `json:"source_sha256"`
	TextSHA        string             `json:"text_sha256"`
	TextCharacters int                `json:"text_characters"`
	BodyCharacters int                `json:"body_characters"`
	Chapters       int                `json:"chapters"`
	Blocks         map[string]int     `json:"blocks"`
	Variants       map[string]variant `json:"variants"`
}

func convert(source, destination string) (fixtureManifest, error) {
	m := fixtureManifest{Blocks: map[string]int{}, Variants: map[string]variant{}}
	raw, err := os.ReadFile(source)
	if err != nil {
		return m, err
	}
	root, err := parseXML(string(raw))
	if err != nil {
		return m, err
	}
	nodes := root.children()
	if len(nodes) == 0 {
		return m, fmt.Errorf("empty fixture")
	}
	c := converter{}
	var all []*model.Block
	var groups [][]*model.Block
	var expected strings.Builder
	for _, n := range nodes {
		blocks, err := c.blocks(n)
		if err != nil {
			return m, err
		}
		groups = append(groups, blocks)
		all = append(all, blocks...)
		expected.WriteString(n.text())
		if n.tag == "h1" {
			m.Chapters++
		}
	}
	actual := blockText(all)
	if actual != expected.String() {
		return m, fmt.Errorf("conversion changed the text")
	}
	m.SourceSHA = toolutil.Hash(raw)
	m.TextSHA = toolutil.Hash([]byte(actual))
	m.TextCharacters = utf8.RuneCountInString(actual)
	m.BodyCharacters = m.TextCharacters - utf8.RuneCountInString(nodes[0].text())
	walkBlocks(all, func(b *model.Block) { m.Blocks[b.Kind]++ })
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return m, err
	}
	for _, size := range []struct {
		name  string
		limit int
	}{{"10k", 10000}, {"100k", 100000}, {"1m", int(^uint(0) >> 1)}} {
		var subset []*model.Block
		var xmlText strings.Builder
		count := 0
		for i, group := range groups {
			subset = append(subset, group...)
			xmlText.WriteString(nodes[i].raw)
			count += utf8.RuneCountInString(blockText(group))
			if count >= size.limit {
				break
			}
		}
		// Keep the fixture's original schema version for historical comparisons.
		doc := struct {
			Format  string         `json:"format"`
			Version int            `json:"version"`
			Blocks  []*model.Block `json:"blocks"`
		}{"arcmd", 4, subset}
		data, err := json.Marshal(doc)
		if err != nil {
			return m, err
		}
		if _, err := model.Decode(string(data)); err != nil {
			return m, fmt.Errorf("invalid converted fixture: %w", err)
		}
		if err := os.WriteFile(filepath.Join(destination, size.name+".arcmd"), data, 0o644); err != nil {
			return m, err
		}
		if err := os.WriteFile(filepath.Join(destination, size.name+".xml"), []byte(xmlText.String()), 0o644); err != nil {
			return m, err
		}
		m.Variants[size.name] = variant{count, len(subset)}
	}
	return m, toolutil.WriteJSON(filepath.Join(destination, "manifest.json"), m)
}

// fixtureText ignores serialization whitespace only in structural containers.
func fixtureText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	root, err := parseXML(string(data))
	if err != nil {
		return "", err
	}
	containers := map[string]bool{}
	for _, name := range strings.Fields("document ul ol callout blockquote pre table colgroup thead tbody tr th td") {
		containers[name] = true
	}
	var walk func(*xmlNode) string
	walk = func(n *xmlNode) string {
		var b strings.Builder
		for _, p := range n.parts {
			if p.node != nil {
				b.WriteString(walk(p.node))
			} else if !(containers[n.tag] && strings.TrimSpace(p.text) == "") {
				b.WriteString(p.text)
			}
		}
		return b.String()
	}
	return walk(root), nil
}
