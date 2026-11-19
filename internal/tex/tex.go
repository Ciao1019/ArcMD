// Package tex typesets the TeX of math blocks: the common subset of LaTeX
// math people write in notes — letters, numbers and operators, Greek and
// symbols, scripts, fractions, roots, big operators with limits, function
// names, text, accents, \left…\right delimiters, matrices, cases and
// several lines. It lays a formula out into boxes and drawing operations;
// the caller measures text and draws, so the package has no dependency on
// the window.
package tex

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Measure returns the advance width of text at size, and how far its
// glyphs reach above and below the baseline. Italic is a variable's style;
// Upright text (numbers, names, \text) is not. Bold selects the matching
// bold face for both measurement and drawing.
type Measure func(text string, size float32, italic, bold bool) (w, ascent, descent float32)

// Op is one thing a formula draws, in DIPs from the origin of its box, the
// left end of its baseline, y growing downwards.
type Op struct {
	// Kind is "text", "rule" (a filled rectangle) or "path" (a polyline).
	Kind string
	// X and Y place text by its baseline's left end, and a rule by its top
	// left corner.
	X, Y   float32
	Text   string
	Size   float32
	Italic bool
	Bold   bool
	// Error marks text TeX did not understand, drawn as a warning.
	Error bool
	// W and H are a rule's size.
	W, H float32
	// Points are a path's, Width its stroke.
	Points [][2]float32
	Width  float32
}

// Box is a laid out formula: its width, how far it reaches above and below
// its baseline, and what it draws.
type Box struct {
	W, Ascent, Descent float32
	Ops                []Op
}

// Layout typesets src at size DIPs, as display math. It fails on TeX it
// cannot read, as unbalanced braces; commands it does not know show as
// written, marked as errors.
func Layout(src string, size float32, m Measure) (Box, error) {
	return layoutMode(src, size, m, true)
}

// LayoutWidth typesets src as display math within a column of lineWidth DIPs,
// placing equation tags (\tag, \tag*) flush right while keeping the formula
// centered. When lineWidth <= 0 or no tag is present, the box keeps its
// natural width.
func LayoutWidth(src string, size, lineWidth float32, m Measure) (Box, error) {
	return layoutModeWidth(src, size, lineWidth, m, true)
}

// LayoutInline uses text-style operators and side limits within a sentence.
func LayoutInline(src string, size float32, m Measure) (Box, error) {
	return layoutMode(src, size, m, false)
}

func layoutMode(src string, size float32, m Measure, display bool) (Box, error) {
	return layoutModeWidth(src, size, 0, m, display)
}

func layoutModeWidth(src string, size, lineWidth float32, m Measure, display bool) (Box, error) {
	p := &parser{src: src}
	lines, err := p.lines(nil)
	if err != nil {
		return Box{}, err
	}
	if p.pos < len(p.src) {
		return Box{}, fmt.Errorf("tex: unexpected %q", p.src[p.pos:])
	}
	l := &layouter{m: m}
	return l.lines(lines, size, lineWidth, display), nil
}

// node is a parsed piece of a formula.
type node struct {
	kind string // "ord", "op", "bin", "rel", "open", "close", "punct", "text", "space", "group", "bold", "frac", "sqrt", "big", "func", "accent", "delim", "matrix", "error"
	text string
	// italic is a variable's style.
	italic bool
	// args are a fraction's numerator and denominator, a root's radicand
	// and index, an accent's base, a group's or delimited list's content.
	args [][]*node
	sup  []*node
	sub  []*node
	// hasSup and hasSub tell an empty script ("x^{}") from none.
	hasSup, hasSub bool
	// limits puts a big operator's scripts above and below it.
	limits bool
	// rows are a matrix's cells; env its environment.
	rows [][][]*node
	env  string
	// open and close are a delimited list's delimiters; space a space's
	// width in em.
	open, close string
	space       float32
}

type parser struct {
	src string
	pos int
}

func (p *parser) peek() byte {
	if p.pos < len(p.src) {
		return p.src[p.pos]
	}
	return 0
}

func (p *parser) skipSpace() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t' || p.src[p.pos] == '\n' || p.src[p.pos] == '\r') {
		p.pos++
	}
}

// command reads the name of the command whose backslash is at pos.
func (p *parser) command() string {
	p.pos++ // the backslash
	start := p.pos
	for p.pos < len(p.src) && (p.src[p.pos] >= 'a' && p.src[p.pos] <= 'z' || p.src[p.pos] >= 'A' && p.src[p.pos] <= 'Z') {
		p.pos++
	}
	if p.pos == start && p.pos < len(p.src) {
		_, n := utf8.DecodeRuneInString(p.src[p.pos:])
		p.pos += n
	}
	return p.src[start:p.pos]
}

// lines reads lists separated by \\ until the end, a closing brace, \right
// or \end, which stop is set to.
func (p *parser) lines(stop *string) ([][]*node, error) {
	var out [][]*node
	for {
		list, end, err := p.list()
		if err != nil {
			return nil, err
		}
		out = append(out, list)
		if end != `\\` {
			if stop != nil {
				*stop = end
			} else if end != "" {
				return nil, fmt.Errorf("tex: unexpected %s", end)
			}
			return out, nil
		}
	}
}

// list reads atoms until the end of the source or a terminator: "}",
// "&", `\\`, `\right`, `\end`, which it returns (consumed but for \right
// and \end, whose arguments the caller reads).
func (p *parser) list() ([]*node, string, error) {
	var out []*node
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			return out, "", nil
		}
		switch c := p.peek(); c {
		case '}':
			p.pos++
			return out, "}", nil
		case '&':
			p.pos++
			return out, "&", nil
		case '^', '_':
			p.pos++
			script, err := p.argument()
			if err != nil {
				return nil, "", err
			}
			if len(out) == 0 {
				out = append(out, &node{kind: "ord"})
			}
			last := out[len(out)-1]
			if c == '^' {
				last.sup, last.hasSup = script, true
			} else {
				last.sub, last.hasSub = script, true
			}
			continue
		case '\\':
			save := p.pos
			name := p.command()
			switch name {
			case `\`:
				return out, `\\`, nil
			case "right", "end":
				p.pos = save
				return out, `\` + name, nil
			case "limits", "nolimits", "displaystyle", "textstyle", "notag", "nonumber":
				if name == "limits" && len(out) > 0 {
					out[len(out)-1].limits = true
				}
				if name == "nolimits" && len(out) > 0 {
					out[len(out)-1].limits = false
				}
				continue
			}
			n, err := p.commandNode(name)
			if err != nil {
				return nil, "", err
			}
			out = append(out, n)
		case '{':
			p.pos++
			inner, end, err := p.list()
			if err != nil {
				return nil, "", err
			}
			if end != "}" {
				return nil, "", errors.New("tex: unbalanced braces")
			}
			out = append(out, &node{kind: "group", args: [][]*node{inner}})
		default:
			r, n := utf8.DecodeRuneInString(p.src[p.pos:])
			p.pos += n
			out = append(out, charNode(r))
		}
	}
}

// argument reads a command's argument: a group or one atom.
func (p *parser) argument() ([]*node, error) {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return nil, errors.New("tex: missing argument")
	}
	switch c := p.peek(); c {
	case '{':
		p.pos++
		inner, end, err := p.list()
		if err != nil {
			return nil, err
		}
		if end != "}" {
			return nil, errors.New("tex: unbalanced braces")
		}
		return inner, nil
	case '\\':
		name := p.command()
		n, err := p.commandNode(name)
		if err != nil {
			return nil, err
		}
		return []*node{n}, nil
	case '}', '^', '_', '&':
		return nil, errors.New("tex: missing argument")
	}
	r, n := utf8.DecodeRuneInString(p.src[p.pos:])
	p.pos += n
	return []*node{charNode(r)}, nil
}

// rawArgument reads a group's text as written, for \text and the names of
// environments.
func (p *parser) rawArgument() (string, error) {
	p.skipSpace()
	if p.peek() != '{' {
		if p.pos >= len(p.src) {
			return "", errors.New("tex: missing argument")
		}
		r, n := utf8.DecodeRuneInString(p.src[p.pos:])
		p.pos += n
		return string(r), nil
	}
	depth, start := 0, p.pos+1
	for i := p.pos; i < len(p.src); i++ {
		switch p.src[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				p.pos = i + 1
				return p.src[start:i], nil
			}
		}
	}
	return "", errors.New("tex: unbalanced braces")
}

// delimiter reads the delimiter after \left, \right or \big: a character,
// a command naming one, or "." for none.
func (p *parser) delimiter() (string, error) {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return "", errors.New("tex: missing delimiter")
	}
	if p.peek() == '\\' {
		name := p.command()
		if d, ok := delimiters[name]; ok {
			return d, nil
		}
		return "", fmt.Errorf("tex: unknown delimiter \\%s", name)
	}
	r, n := utf8.DecodeRuneInString(p.src[p.pos:])
	p.pos += n
	if r == '.' {
		return "", nil
	}
	return string(r), nil
}

var delimiters = map[string]string{
	"{": "{", "}": "}", "|": "‖", "langle": "⟨", "rangle": "⟩", "lfloor": "⌊", "rfloor": "⌋",
	"lceil": "⌈", "rceil": "⌉", "vert": "|", "Vert": "‖", "lbrace": "{", "rbrace": "}", "lvert": "|", "rvert": "|",
}

func charNode(r rune) *node {
	s := string(r)
	switch {
	case unicode.IsLetter(r) && r < 0x2000:
		return &node{kind: "ord", text: s, italic: r < 0x370 || r >= 0x3b1 && r <= 0x3c9}
	case strings.ContainsRune("+−-*±∓×÷·∘", r):
		if r == '-' {
			s = "−"
		}
		if r == '*' {
			s = "∗"
		}
		return &node{kind: "bin", text: s}
	case strings.ContainsRune("=<>≤≥≠≈≡∼:", r):
		return &node{kind: "rel", text: s}
	case strings.ContainsRune("([", r):
		return &node{kind: "open", text: s}
	case strings.ContainsRune(")]", r):
		return &node{kind: "close", text: s}
	case strings.ContainsRune(",;", r):
		return &node{kind: "punct", text: s}
	case r == '\'':
		return &node{kind: "ord", text: "′"}
	}
	return &node{kind: "ord", text: s}
}

// commandNode reads what follows a command's name.
func (p *parser) commandNode(name string) (*node, error) {
	if s, ok := symbols[name]; ok {
		n := &node{kind: s.kind, text: s.text, italic: s.italic}
		return n, nil
	}
	if s, ok := bigOps[name]; ok {
		return &node{kind: "big", text: s, limits: !strings.Contains("∫∬∭∮", s)}, nil
	}
	if functions[name] {
		return &node{kind: "func", text: name, limits: limitFunctions[name]}, nil
	}
	if w, ok := spaces[name]; ok {
		return &node{kind: "space", space: w}, nil
	}
	if a, ok := accents[name]; ok {
		base, err := p.argument()
		if err != nil {
			return nil, err
		}
		return &node{kind: "accent", text: a, args: [][]*node{base}}, nil
	}
	switch name {
	case "boldsymbol":
		body, err := p.argument()
		if err != nil {
			return nil, err
		}
		n := &node{kind: "bold", args: [][]*node{body}}
		if len(body) == 1 && !body[0].hasSup && !body[0].hasSub {
			n.italic = body[0].italic
			n.limits = body[0].limits
		}
		return n, nil
	case "tag":
		return p.tagNode()
	case "frac", "dfrac", "tfrac", "binom", "cfrac":
		num, err := p.argument()
		if err != nil {
			return nil, err
		}
		den, err := p.argument()
		if err != nil {
			return nil, err
		}
		n := &node{kind: "frac", args: [][]*node{num, den}}
		if name == "binom" {
			n.text = "binom"
		}
		return n, nil
	case "sqrt":
		var index []*node
		p.skipSpace()
		if p.peek() == '[' {
			p.pos++
			end := strings.IndexByte(p.src[p.pos:], ']')
			if end < 0 {
				return nil, errors.New("tex: unclosed [")
			}
			sub := &parser{src: p.src[p.pos : p.pos+end]}
			var err error
			if index, _, err = sub.list(); err != nil {
				return nil, err
			}
			p.pos += end + 1
		}
		radicand, err := p.argument()
		if err != nil {
			return nil, err
		}
		return &node{kind: "sqrt", args: [][]*node{radicand, index}}, nil
	case "text", "textrm", "mathrm", "operatorname", "textit", "mbox", "mathbf", "textbf", "mathit", "mathsf", "mathtt", "mathcal", "mathbb":
		raw, err := p.rawArgument()
		if err != nil {
			return nil, err
		}
		n := &node{kind: "text", text: raw}
		switch name {
		case "operatorname":
			n.kind = "func"
		case "mathit", "textit":
			n.italic = true
		case "mathbb":
			n.text = mapRunes(raw, blackboard)
		case "mathcal":
			n.text = mapRunes(raw, calligraphic)
		}
		return n, nil
	case "left":
		open, err := p.delimiter()
		if err != nil {
			return nil, err
		}
		var stop string
		inner, err := p.lines(&stop)
		if err != nil {
			return nil, err
		}
		if stop != `\right` {
			return nil, errors.New(`tex: \left without \right`)
		}
		p.command()
		closing, err := p.delimiter()
		if err != nil {
			return nil, err
		}
		n := &node{kind: "delim", open: open, close: closing}
		if len(inner) == 1 {
			n.args = [][]*node{inner[0]}
		} else {
			n.args = [][]*node{{&node{kind: "matrix", env: "lines", rows: linesToRows(inner)}}}
		}
		return n, nil
	case "big", "Big", "bigg", "Bigg", "bigl", "bigr", "Bigl", "Bigr":
		d, err := p.delimiter()
		if err != nil {
			return nil, err
		}
		return &node{kind: "ord", text: d}, nil
	case "begin":
		env, err := p.rawArgument()
		if err != nil {
			return nil, err
		}
		if env == "array" {
			if _, err := p.rawArgument(); err != nil {
				return nil, err
			}
		}
		var rows [][][]*node
		var row [][]*node
		for {
			cell, end, err := p.list()
			if err != nil {
				return nil, err
			}
			row = append(row, cell)
			if end == "&" {
				continue
			}
			rows = append(rows, row)
			row = nil
			if end == `\\` {
				continue
			}
			if end != `\end` {
				return nil, fmt.Errorf(`tex: \begin{%s} without \end`, env)
			}
			p.command()
			if closing, err := p.rawArgument(); err != nil || closing != env {
				return nil, fmt.Errorf(`tex: \begin{%s} ended by \end{%s}`, env, closing)
			}
			break
		}
		// A trailing \\ leaves an empty last row.
		if last := rows[len(rows)-1]; len(rows) > 1 && len(last) == 1 && len(last[0]) == 0 {
			rows = rows[:len(rows)-1]
		}
		return &node{kind: "matrix", env: env, rows: rows}, nil
	}
	return &node{kind: "error", text: `\` + name}, nil
}

func linesToRows(lines [][]*node) [][][]*node {
	var rows [][][]*node
	for _, l := range lines {
		rows = append(rows, [][]*node{l})
	}
	return rows
}

func mapRunes(s string, table map[rune]rune) string {
	return strings.Map(func(r rune) rune {
		if m, ok := table[r]; ok {
			return m
		}
		return r
	}, s)
}

// tagNode parses \tag{...} or \tag*{...} in upright text mode (supporting
// spaces, escaped punctuation, text styling commands, and embedded $...$ math).
func (p *parser) tagNode() (*node, error) {
	starred := false
	p.skipSpace()
	if p.peek() == '*' {
		p.pos++
		starred = true
	}
	inner, err := p.textArgument()
	if err != nil {
		return nil, err
	}
	if !starred {
		wrapped := make([]*node, 0, len(inner)+2)
		wrapped = append(wrapped, &node{kind: "text", text: "("})
		wrapped = append(wrapped, inner...)
		wrapped = append(wrapped, &node{kind: "text", text: ")"})
		inner = wrapped
	}
	return &node{kind: "tag", args: [][]*node{inner}}, nil
}

func (p *parser) textArgument() ([]*node, error) {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return nil, errors.New("tex: missing argument")
	}
	switch p.peek() {
	case '{':
		p.pos++
		return p.textGroup()
	case '}', '^', '_', '&':
		return nil, errors.New("tex: missing argument")
	case '\\':
		return p.textEscape()
	default:
		r, n := utf8.DecodeRuneInString(p.src[p.pos:])
		p.pos += n
		return []*node{{kind: "text", text: string(r)}}, nil
	}
}

func (p *parser) textGroup() ([]*node, error) {
	var out []*node
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, &node{kind: "text", text: buf.String()})
			buf.Reset()
		}
	}
	for {
		if p.pos >= len(p.src) {
			return nil, errors.New("tex: unbalanced braces")
		}
		switch p.peek() {
		case '}':
			p.pos++
			flush()
			return out, nil
		case '{':
			p.pos++
			flush()
			sub, err := p.textGroup()
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
		case '$':
			p.pos++
			flush()
			start := p.pos
			closed := false
			for p.pos < len(p.src) {
				if p.src[p.pos] == '\\' && p.pos+1 < len(p.src) {
					p.pos += 2
					continue
				}
				if p.src[p.pos] == '$' {
					closed = true
					break
				}
				p.pos++
			}
			if !closed {
				return nil, errors.New("tex: unclosed $ in text")
			}
			mathSrc := p.src[start:p.pos]
			p.pos++ // closing $
			sub := &parser{src: mathSrc}
			mathNodes, end, err := sub.list()
			if err != nil {
				return nil, err
			}
			if end != "" || sub.pos < len(sub.src) {
				return nil, fmt.Errorf("tex: unexpected %q in tag math", end)
			}
			out = append(out, &node{kind: "group", args: [][]*node{mathNodes}})
		case '\\':
			flush()
			esc, err := p.textEscape()
			if err != nil {
				return nil, err
			}
			out = append(out, esc...)
		default:
			r, n := utf8.DecodeRuneInString(p.src[p.pos:])
			p.pos += n
			if r == '\n' || r == '\r' || r == '\t' {
				r = ' '
			}
			buf.WriteRune(r)
		}
	}
}

func (p *parser) textEscape() ([]*node, error) {
	cmd := p.command()
	switch cmd {
	case "{", "}", "#", "%", "&", "_", "$":
		return []*node{{kind: "text", text: cmd}}, nil
	case " ":
		return []*node{{kind: "text", text: " "}}, nil
	case "textbf", "mathbf":
		sub, err := p.textArgument()
		if err != nil {
			return nil, err
		}
		return []*node{{kind: "bold", args: [][]*node{sub}}}, nil
	case "textit", "emph", "mathit":
		sub, err := p.textArgument()
		if err != nil {
			return nil, err
		}
		for _, sn := range sub {
			if sn.kind == "text" {
				sn.italic = true
			}
		}
		return sub, nil
	case "text", "textrm", "mathrm", "mbox", "mathsf", "mathtt":
		return p.textArgument()
	default:
		if w, ok := spaces[cmd]; ok {
			return []*node{{kind: "space", space: w}}, nil
		}
		cn, err := p.commandNode(cmd)
		if err != nil {
			return nil, err
		}
		return []*node{cn}, nil
	}
}
