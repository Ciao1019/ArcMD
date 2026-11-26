package editor

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

const maxHighlightBytes = 512 * 1024

// SourceHighlightCache computes and caches per-line rune-offset TextRanges
// for raw Markdown source editing, allowing callers to submit only the
// currently visible lines to ui.TextArea.TextRanges.
type SourceHighlightCache struct {
	text       string
	tokens     CodeTokens
	baseText   ui.Color
	caption    ui.Color
	marker     ui.Color
	lineRanges [][]ui.TextRange
	valid      bool
}

// VisibleRanges returns the cached syntax-highlight TextRanges covering lines
// [firstLine, lastLine] inclusive. Calling it on unchanged text and theme is
// O(visible lines).
func (c *SourceHighlightCache) VisibleRanges(text string, pg Page, firstLine, lastLine int) []ui.TextRange {
	if c == nil || text == "" || len(text) > maxHighlightBytes {
		return nil
	}
	if !c.valid || c.text != text || c.tokens != pg.Tokens || c.baseText != pg.CodeText || c.caption != pg.Caption || c.marker != pg.Marker {
		c.rebuild(text, pg)
	}
	n := len(c.lineRanges)
	if n == 0 {
		return nil
	}
	firstLine = max(0, min(firstLine, n-1))
	lastLine = max(firstLine, min(lastLine, n-1))
	total := 0
	for i := firstLine; i <= lastLine; i++ {
		total += len(c.lineRanges[i])
	}
	if total == 0 {
		return nil
	}
	out := make([]ui.TextRange, 0, total)
	for i := firstLine; i <= lastLine; i++ {
		out = append(out, c.lineRanges[i]...)
	}
	return out
}

func (c *SourceHighlightCache) rebuild(text string, pg Page) {
	c.text = text
	c.tokens = pg.Tokens
	c.baseText = pg.CodeText
	c.caption = pg.Caption
	c.marker = pg.Marker
	c.valid = true

	lineCount := 1 + strings.Count(text, "\n")
	lines := make([][]ui.TextRange, lineCount)

	// Detect leading YAML frontmatter block (`---` on line 0 closed by `---` or `...`).
	fmEndLine := -1
	if strings.HasPrefix(text, "---\n") || strings.HasPrefix(text, "---\r\n") {
		pos := strings.IndexByte(text, '\n') + 1
		for ln := 1; pos < len(text); ln++ {
			nl := strings.IndexByte(text[pos:], '\n')
			var seg string
			if nl < 0 {
				seg = text[pos:]
				pos = len(text)
			} else {
				seg = text[pos : pos+nl]
				pos += nl + 1
			}
			trimmed := strings.TrimSpace(seg)
			if trimmed == "---" || trimmed == "..." {
				fmEndLine = ln
				break
			}
		}
	}

	inFence := false
	var fenceChar byte
	fenceLen := 0
	fenceLang := langPlain
	fenceState := lexNormal
	inMathBlock := false
	inHTMLComment := false

	pos := 0
	runeOff := 0
	for lineIdx := 0; lineIdx < lineCount; lineIdx++ {
		nl := strings.IndexByte(text[pos:], '\n')
		var line string
		hasNL := false
		if nl < 0 {
			line = text[pos:]
			pos = len(text)
		} else {
			line = text[pos : pos+nl]
			pos += nl + 1
			hasNL = true
		}
		lineRunes := utf8.RuneCountInString(line)
		var ranges []ui.TextRange

		switch {
		case fmEndLine >= 0 && lineIdx <= fmEndLine:
			if lineIdx == 0 || lineIdx == fmEndLine {
				if lineRunes > 0 {
					ranges = append(ranges, ui.TextRange{Start: runeOff, End: runeOff + lineRunes, Color: pg.Tokens.Comment})
				}
			} else {
				spans := lexConfigLine(langYAML, line, pg)
				ranges = appendSpansAsRanges(ranges, spans, runeOff, pg.CodeText)
			}

		case inFence:
			if isClosingFence(line, fenceChar, fenceLen) {
				inFence = false
				fenceLang = langPlain
				fenceState = lexNormal
				if lineRunes > 0 {
					ranges = append(ranges, ui.TextRange{Start: runeOff, End: runeOff + lineRunes, Color: pg.Tokens.Comment})
				}
			} else if fenceLang != langPlain {
				spans, nextState := lexLine(fenceLang, line, fenceState, pg)
				fenceState = nextState
				ranges = appendSpansAsRanges(ranges, spans, runeOff, pg.CodeText)
			} else if lineRunes > 0 {
				ranges = append(ranges, ui.TextRange{Start: runeOff, End: runeOff + lineRunes, Color: pg.Tokens.String})
			}

		case inMathBlock:
			if strings.TrimSpace(line) == "$$" {
				inMathBlock = false
				if lineRunes > 0 {
					ranges = append(ranges, ui.TextRange{Start: runeOff, End: runeOff + lineRunes, Color: pg.Tokens.Comment})
				}
			} else if lineRunes > 0 {
				ranges = append(ranges, ui.TextRange{Start: runeOff, End: runeOff + lineRunes, Color: pg.Tokens.String})
			}

		case inHTMLComment:
			if end := strings.Index(line, "-->"); end >= 0 {
				endRune := runeOff + utf8.RuneCountInString(line[:end+3])
				if endRune > runeOff {
					ranges = append(ranges, ui.TextRange{Start: runeOff, End: endRune, Color: pg.Tokens.Comment})
				}
				inHTMLComment = false
				if end+3 < len(line) {
					ranges, inHTMLComment = appendMarkdownInlineRanges(ranges, line[end+3:], endRune, pg)
				}
			} else if lineRunes > 0 {
				ranges = append(ranges, ui.TextRange{Start: runeOff, End: runeOff + lineRunes, Color: pg.Tokens.Comment})
			}

		default:
			if ch, n, info, ok := parseOpeningFence(line); ok {
				inFence = true
				fenceChar, fenceLen = ch, n
				fenceLang = normalizeLexLang(info)
				fenceState = lexNormal
				ranges = appendFenceHeaderRanges(ranges, line, runeOff, pg)
			} else if strings.TrimSpace(line) == "$$" {
				inMathBlock = true
				if lineRunes > 0 {
					ranges = append(ranges, ui.TextRange{Start: runeOff, End: runeOff + lineRunes, Color: pg.Tokens.Comment})
				}
			} else {
				ranges, inHTMLComment = highlightMarkdownSourceLine(line, runeOff, pg)
			}
		}

		lines[lineIdx] = ranges
		runeOff += lineRunes
		if hasNL {
			runeOff++
		}
	}
	c.lineRanges = lines
}

func appendSpansAsRanges(dst []ui.TextRange, spans []ui.Span, startRune int, base ui.Color) []ui.TextRange {
	at := startRune
	for _, sp := range spans {
		n := utf8.RuneCountInString(sp.Text)
		if n > 0 && sp.Color.A > 0 && sp.Color != base {
			dst = append(dst, ui.TextRange{Start: at, End: at + n, Color: sp.Color})
		}
		at += n
	}
	return dst
}

func parseOpeningFence(line string) (ch byte, count int, info string, ok bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if len(trimmed) < 3 {
		return 0, 0, "", false
	}
	if trimmed[0] != '`' && trimmed[0] != '~' {
		return 0, 0, "", false
	}
	ch = trimmed[0]
	n := 0
	for n < len(trimmed) && trimmed[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0, "", false
	}
	rest := trimmed[n:]
	// CommonMark: backtick info strings cannot contain backticks.
	if ch == '`' && strings.IndexByte(rest, '`') >= 0 {
		return 0, 0, "", false
	}
	lang := strings.TrimSpace(rest)
	if sp := strings.IndexAny(lang, " \t{"); sp >= 0 {
		lang = lang[:sp]
	}
	return ch, n, lang, true
}

func isClosingFence(line string, ch byte, minLen int) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < minLen {
		return false
	}
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] != ch {
			return false
		}
	}
	return true
}

func appendFenceHeaderRanges(dst []ui.TextRange, line string, runeOff int, pg Page) []ui.TextRange {
	lead := len(line) - len(strings.TrimLeft(line, " \t"))
	fenceStart := lead
	ch := line[fenceStart]
	i := fenceStart
	for i < len(line) && line[i] == ch {
		i++
	}
	rStart := runeOff + utf8.RuneCountInString(line[:fenceStart])
	rFenceEnd := rStart + (i - fenceStart)
	dst = append(dst, ui.TextRange{Start: rStart, End: rFenceEnd, Color: pg.Tokens.Comment})
	if i < len(line) {
		rLineEnd := runeOff + utf8.RuneCountInString(line)
		if rLineEnd > rFenceEnd {
			dst = append(dst, ui.TextRange{Start: rFenceEnd, End: rLineEnd, Color: pg.Tokens.Number})
		}
	}
	return dst
}

func highlightMarkdownSourceLine(line string, runeOff int, pg Page) ([]ui.TextRange, bool) {
	if line == "" {
		return nil, false
	}
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return nil, false
	}
	leadBytes := len(line) - len(trimmed)
	leadRunes := utf8.RuneCountInString(line[:leadBytes])
	lineRunes := utf8.RuneCountInString(line)

	// Thematic break / horizontal rule.
	if isThematicBreakLine(trimmed) {
		return []ui.TextRange{{Start: runeOff + leadRunes, End: runeOff + lineRunes, Color: pg.Tokens.Comment}}, false
	}

	// ATX Heading: # .. ###### followed by space or end of line.
	if trimmed[0] == '#' {
		hashes := 0
		for hashes < len(trimmed) && hashes < 6 && trimmed[hashes] == '#' {
			hashes++
		}
		if hashes > 0 && (hashes == len(trimmed) || trimmed[hashes] == ' ' || trimmed[hashes] == '\t') {
			return []ui.TextRange{
				{Start: runeOff + leadRunes, End: runeOff + leadRunes + hashes, Color: pg.Tokens.Comment, Weight: 600},
				{Start: runeOff + leadRunes + hashes, End: runeOff + lineRunes, Color: pg.Tokens.Keyword, Weight: 600},
			}, false
		}
	}

	var ranges []ui.TextRange
	curByte := leadBytes
	curRune := runeOff + leadRunes

	// Blockquote prefixes (`> `), plus callout markers (`[!NOTE]`).
	if trimmed[0] == '>' {
		for curByte < len(line) && line[curByte] == '>' {
			ranges = append(ranges, ui.TextRange{Start: curRune, End: curRune + 1, Color: pg.Tokens.Comment, Weight: 600})
			curByte++
			curRune++
			for curByte < len(line) && (line[curByte] == ' ' || line[curByte] == '\t') {
				curByte++
				curRune++
			}
		}
		rest := line[curByte:]
		if strings.HasPrefix(rest, "[!") {
			if closeIdx := strings.IndexByte(rest, ']'); closeIdx > 2 {
				tagRunes := utf8.RuneCountInString(rest[:closeIdx+1])
				ranges = append(ranges, ui.TextRange{Start: curRune, End: curRune + tagRunes, Color: pg.Tokens.Function, Weight: 600})
				curByte += closeIdx + 1
				curRune += tagRunes
			}
		}
		if curByte < len(line) {
			ranges = append(ranges, ui.TextRange{Start: curRune, End: runeOff + lineRunes, Color: pg.Caption})
			var openComment bool
			ranges, openComment = appendMarkdownInlineRanges(ranges, line[curByte:], curRune, pg)
			return ranges, openComment
		}
		return ranges, false
	}

	// Bullet / task list markers (`- `, `* `, `+ `, `- [ ] `, `- [x] `).
	if (trimmed[0] == '-' || trimmed[0] == '*' || trimmed[0] == '+') && len(trimmed) >= 2 && (trimmed[1] == ' ' || trimmed[1] == '\t') {
		ranges = append(ranges, ui.TextRange{Start: curRune, End: curRune + 1, Color: pg.Tokens.Type, Weight: 600})
		curByte += 2
		curRune += 2
		rest := line[curByte:]
		if len(rest) >= 3 && rest[0] == '[' && (rest[1] == ' ' || rest[1] == 'x' || rest[1] == 'X') && rest[2] == ']' {
			ranges = append(ranges, ui.TextRange{Start: curRune, End: curRune + 3, Color: pg.Tokens.Function, Weight: 600})
			curByte += 3
			curRune += 3
		}
		return appendMarkdownInlineRanges(ranges, line[curByte:], curRune, pg)
	}

	// Ordered list markers (`1. `, `12) `).
	if trimmed[0] >= '0' && trimmed[0] <= '9' {
		j := 0
		for j < len(trimmed) && j < 9 && trimmed[j] >= '0' && trimmed[j] <= '9' {
			j++
		}
		if j < len(trimmed)-1 && (trimmed[j] == '.' || trimmed[j] == ')') && (trimmed[j+1] == ' ' || trimmed[j+1] == '\t') {
			ranges = append(ranges, ui.TextRange{Start: curRune, End: curRune + j + 1, Color: pg.Tokens.Number, Weight: 600})
			curByte += j + 2
			curRune += j + 2
			return appendMarkdownInlineRanges(ranges, line[curByte:], curRune, pg)
		}
	}

	// Footnote definition (`[^id]:`).
	if strings.HasPrefix(trimmed, "[^") {
		if closeIdx := strings.Index(trimmed, "]:"); closeIdx > 2 {
			n := utf8.RuneCountInString(trimmed[:closeIdx+2])
			ranges = append(ranges, ui.TextRange{Start: curRune, End: curRune + n, Color: pg.Tokens.Function, Weight: 600})
			curByte += closeIdx + 2
			curRune += n
			return appendMarkdownInlineRanges(ranges, line[curByte:], curRune, pg)
		}
	}

	// Pipe table separator / pipes.
	if trimmed[0] == '|' {
		for i := curByte; i < len(line); {
			r, sz := utf8.DecodeRuneInString(line[i:])
			if r == '|' {
				posR := runeOff + utf8.RuneCountInString(line[:i])
				ranges = append(ranges, ui.TextRange{Start: posR, End: posR + 1, Color: pg.Tokens.Comment})
			}
			i += sz
		}
	}

	return appendMarkdownInlineRanges(ranges, line[curByte:], curRune, pg)
}

func isThematicBreakLine(trimmed string) bool {
	if len(trimmed) < 3 {
		return false
	}
	ch := trimmed[0]
	if ch != '-' && ch != '*' && ch != '_' {
		return false
	}
	count := 0
	for i := 0; i < len(trimmed); i++ {
		switch trimmed[i] {
		case ch:
			count++
		case ' ', '\t', '\r':
		default:
			return false
		}
	}
	return count >= 3
}

func appendMarkdownInlineRanges(dst []ui.TextRange, s string, baseRune int, pg Page) ([]ui.TextRange, bool) {
	n := len(s)
	rPos := baseRune
	for i := 0; i < n; {
		ch := s[i]

		// Backslash escape.
		if ch == '\\' && i+1 < n {
			_, sz := utf8.DecodeRuneInString(s[i+1:])
			dst = append(dst, ui.TextRange{Start: rPos, End: rPos + 2, Color: pg.Tokens.Comment})
			i += 1 + sz
			rPos += 2
			continue
		}

		// HTML comment `<!-- ... -->`.
		if ch == '<' && strings.HasPrefix(s[i:], "<!--") {
			if end := strings.Index(s[i+4:], "-->"); end >= 0 {
				segLen := 4 + end + 3
				segRunes := utf8.RuneCountInString(s[i : i+segLen])
				dst = append(dst, ui.TextRange{Start: rPos, End: rPos + segRunes, Color: pg.Tokens.Comment})
				i += segLen
				rPos += segRunes
				continue
			}
			restRunes := utf8.RuneCountInString(s[i:])
			dst = append(dst, ui.TextRange{Start: rPos, End: rPos + restRunes, Color: pg.Tokens.Comment})
			return dst, true
		}

		// HTML tag `<tag ...>` or `</tag>`.
		if ch == '<' && i+1 < n && (s[i+1] == '/' || isASCIIAlpha(s[i+1])) {
			if closeIdx := strings.IndexByte(s[i+1:], '>'); closeIdx > 0 {
				seg := s[i : i+1+closeIdx+1]
				if !strings.ContainsRune(seg, '\n') {
					segRunes := utf8.RuneCountInString(seg)
					dst = append(dst, ui.TextRange{Start: rPos, End: rPos + segRunes, Color: pg.Tokens.Keyword})
					i += len(seg)
					rPos += segRunes
					continue
				}
			}
		}

		// Inline code `` `...` `` (handles single or multi-backtick spans).
		if ch == '`' {
			ticks := 0
			for i+ticks < n && s[i+ticks] == '`' {
				ticks++
			}
			delim := s[i : i+ticks]
			if end := strings.Index(s[i+ticks:], delim); end >= 0 {
				segLen := ticks + end + ticks
				segRunes := utf8.RuneCountInString(s[i : i+segLen])
				dst = append(dst, ui.TextRange{Start: rPos, End: rPos + segRunes, Color: pg.Tokens.String})
				i += segLen
				rPos += segRunes
				continue
			}
		}

		// Inline / display TeX math `$$...$$` or `$...$`.
		if ch == '$' {
			if i+1 < n && s[i+1] == '$' {
				if end := strings.Index(s[i+2:], "$$"); end > 0 {
					segLen := 2 + end + 2
					segRunes := utf8.RuneCountInString(s[i : i+segLen])
					dst = append(dst, ui.TextRange{Start: rPos, End: rPos + segRunes, Color: pg.Tokens.String})
					i += segLen
					rPos += segRunes
					continue
				}
			} else if i+1 < n && s[i+1] != ' ' && s[i+1] != '\t' {
				if end := findClosingDollar(s[i+1:]); end > 0 {
					segLen := 1 + end + 1
					segRunes := utf8.RuneCountInString(s[i : i+segLen])
					dst = append(dst, ui.TextRange{Start: rPos, End: rPos + segRunes, Color: pg.Tokens.String})
					i += segLen
					rPos += segRunes
					continue
				}
			}
		}

		// Wikilink `[[...]]` or embedded `![[...]]`.
		if (ch == '[' && i+1 < n && s[i+1] == '[') || (ch == '!' && i+2 < n && s[i+1] == '[' && s[i+2] == '[') {
			prefix := 2
			if ch == '!' {
				prefix = 3
			}
			if end := strings.Index(s[i+prefix:], "]]"); end > 0 {
				segLen := prefix + end + 2
				segRunes := utf8.RuneCountInString(s[i : i+segLen])
				dst = append(dst, ui.TextRange{Start: rPos, End: rPos + segRunes, Color: pg.Tokens.Function})
				i += segLen
				rPos += segRunes
				continue
			}
		}

		// Markdown link `[text](url)` or image `![alt](url)`.
		if ch == '[' || (ch == '!' && i+1 < n && s[i+1] == '[') {
			openBr := i
			if ch == '!' {
				openBr = i + 1
			}
			if closeBr := strings.IndexByte(s[openBr+1:], ']'); closeBr >= 0 {
				after := openBr + 1 + closeBr + 1
				if after < n && s[after] == '(' {
					if closePar := strings.IndexByte(s[after+1:], ')'); closePar >= 0 {
						labelEnd := after
						urlEnd := after + 1 + closePar + 1
						labelRunes := utf8.RuneCountInString(s[i:labelEnd])
						urlRunes := utf8.RuneCountInString(s[labelEnd:urlEnd])
						dst = append(dst,
							ui.TextRange{Start: rPos, End: rPos + labelRunes, Color: pg.Tokens.Function},
							ui.TextRange{Start: rPos + labelRunes, End: rPos + labelRunes + urlRunes, Color: pg.Tokens.Comment},
						)
						i = urlEnd
						rPos += labelRunes + urlRunes
						continue
					}
				}
			}
		}

		// Bold `**...**` or `__...__`.
		if (ch == '*' && i+1 < n && s[i+1] == '*') || (ch == '_' && i+1 < n && s[i+1] == '_') {
			delim := s[i : i+2]
			if end := strings.Index(s[i+2:], delim); end > 0 {
				segLen := 2 + end + 2
				segRunes := utf8.RuneCountInString(s[i : i+segLen])
				dst = append(dst, ui.TextRange{Start: rPos, End: rPos + segRunes, Color: pg.Tokens.Type, Weight: 600})
				i += segLen
				rPos += segRunes
				continue
			}
		}

		// Strikethrough `~~...~~`.
		if ch == '~' && i+1 < n && s[i+1] == '~' {
			if end := strings.Index(s[i+2:], "~~"); end > 0 {
				segLen := 2 + end + 2
				segRunes := utf8.RuneCountInString(s[i : i+segLen])
				dst = append(dst, ui.TextRange{Start: rPos, End: rPos + segRunes, Color: pg.Tokens.Comment})
				i += segLen
				rPos += segRunes
				continue
			}
		}

		// Inline `#tag` preceded by start-of-string or whitespace.
		if ch == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t') && i+1 < n {
			nextR, _ := utf8.DecodeRuneInString(s[i+1:])
			if unicode.IsLetter(nextR) || unicode.IsDigit(nextR) || nextR == '_' {
				j := i + 1
				for j < n {
					rr, sz := utf8.DecodeRuneInString(s[j:])
					if !unicode.IsLetter(rr) && !unicode.IsDigit(rr) && rr != '_' && rr != '-' && rr != '/' {
						break
					}
					j += sz
				}
				segRunes := utf8.RuneCountInString(s[i:j])
				dst = append(dst, ui.TextRange{Start: rPos, End: rPos + segRunes, Color: pg.Tokens.Function})
				i = j
				rPos += segRunes
				continue
			}
		}

		_, sz := utf8.DecodeRuneInString(s[i:])
		i += sz
		rPos++
	}
	return dst, false
}

func findClosingDollar(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			continue
		}
		if s[i] == '$' {
			if i == 0 || s[i-1] == ' ' || s[i-1] == '\t' {
				return -1
			}
			return i
		}
	}
	return -1
}

func isASCIIAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
