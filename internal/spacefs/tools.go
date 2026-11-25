package spacefs

import (
	"fmt"
	"math"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/Ciao1019/ArcMD/internal/wiki"
)

// Limits of what one tool call returns, so a single call cannot fill a
// model's context. Each result says when it was cut and how to read on.
const (
	MaxGlob      = 200
	MaxGrep      = 60
	MaxSearch    = 20
	MaxReadLines = 400
	MaxReadChars = 24000
	maxLineRunes = 320
)

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// globRegexp compiles a glob: "**" crosses folders, "*" and "?" stay in
// one, case-insensitively. A pattern without a "/" matches a file's name
// anywhere, as "*周报*".
func globRegexp(pattern string) (*regexp.Regexp, bool, error) {
	pattern = strings.ReplaceAll(strings.TrimSpace(pattern), `\`, "/")
	pattern = strings.TrimPrefix(pattern, "/")
	base := !strings.Contains(pattern, "/")
	var b strings.Builder
	b.WriteString("(?i)^")
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; {
		case strings.HasPrefix(pattern[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(pattern[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		default:
			// Copy a whole character: a byte of a Chinese one is not text.
			_, size := utf8.DecodeRuneInString(pattern[i:])
			b.WriteString(regexp.QuoteMeta(pattern[i : i+size]))
			i += size - 1
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	return re, base, err
}

// within reports whether the document at p is in scope: a glob, a folder
// ("工作" or "工作/"), or "" for every document.
func within(scope string, p string) (bool, error) {
	scope = strings.ReplaceAll(strings.TrimSpace(scope), `\`, "/")
	p = strings.ReplaceAll(p, `\`, "/")
	if scope == "" || scope == "." || scope == "/" {
		return true, nil
	}
	if !strings.ContainsAny(scope, "*?") {
		dir := strings.Trim(scope, "/")
		return p == dir || strings.HasPrefix(strings.ToLower(p), strings.ToLower(dir)+"/"), nil
	}
	re, base, err := globRegexp(scope)
	if err != nil {
		return false, err
	}
	if base {
		return re.MatchString(path.Base(p)), nil
	}
	return re.MatchString(p), nil
}

// Glob lists the documents whose path matches the pattern ("" for all):
// path, title, lines and modification time, newest first when sorted by
// time. offset skips that many for the next page.
func (f *FS) Glob(pattern string, byTime bool, offset int) (string, error) {
	docs, err := f.Docs()
	if err != nil {
		return "", err
	}
	var matched []Doc
	for _, d := range docs {
		ok, err := within(pattern, d.Path)
		if err != nil {
			return "", fmt.Errorf("bad pattern: %w", err)
		}
		if ok {
			matched = append(matched, d)
		}
	}
	if byTime {
		slices.SortStableFunc(matched, func(a, b Doc) int { return b.Modified.Compare(a.Modified) })
	}
	if len(matched) == 0 {
		return fmt.Sprintf("No documents match %q.", pattern), nil
	}
	offset = max(0, offset)
	var b strings.Builder
	end := min(len(matched), offset+MaxGlob)
	fmt.Fprintf(&b, "%d documents", len(matched))
	if offset > 0 || end < len(matched) {
		fmt.Fprintf(&b, "; showing %d-%d", offset+1, end)
	}
	b.WriteString(".\n")
	for _, d := range matched[min(offset, len(matched)):end] {
		title, lines := "", 0
		if p, err := f.Page(d.Path); err == nil {
			title, lines = p.Title, len(p.Lines)
		} else {
			title = "(cannot be read: " + err.Error() + ")"
		}
		fmt.Fprintf(&b, "%s\t%s\t%d lines\tmodified %s\n", d.Path, title, lines, d.Modified.Format("2006-01-02 15:04"))
	}
	if end < len(matched) {
		fmt.Fprintf(&b, "(more: call again with offset=%d)\n", end)
	}
	return b.String(), nil
}

// GrepArgs are a text search's arguments.
type GrepArgs struct {
	// Pattern is text, or a regular expression when Regex is set; both
	// match case-insensitively.
	Pattern string
	Regex   bool
	// Scope limits the search to a folder or a glob of paths.
	Scope string
	// Context is how many lines around each match to show, 0-5.
	Context int
	Offset  int
}

// Grep finds the pattern in every document in scope and returns each
// match as "path:line:text", context lines as "path-line-text", groups
// apart with "--", as ripgrep prints them. It counts every match and says
// which documents could not be read, so "no match" means none.
func (f *FS) Grep(a GrepArgs) (string, error) {
	if strings.TrimSpace(a.Pattern) == "" {
		return "", fmt.Errorf("pattern is empty")
	}
	expr := regexp.QuoteMeta(a.Pattern)
	if a.Regex {
		expr = a.Pattern
	}
	re, err := regexp.Compile("(?i)" + expr)
	if err != nil {
		return "", fmt.Errorf("bad regular expression: %w", err)
	}
	ctx := min(max(a.Context, 0), 5)
	docs, err := f.Docs()
	if err != nil {
		return "", err
	}
	type match struct {
		path  string
		page  *Page
		lines []int
	}
	var found []match
	var failed []string
	total, files := 0, 0
	for _, d := range docs {
		ok, err := within(a.Scope, d.Path)
		if err != nil {
			return "", fmt.Errorf("bad scope: %w", err)
		}
		if !ok {
			continue
		}
		p, err := f.Page(d.Path)
		if err != nil {
			failed = append(failed, d.Path)
			continue
		}
		var hit []int
		for i, l := range p.Lines {
			plain := ""
			if i < len(p.PlainLines) {
				plain = p.PlainLines[i]
			}
			if re.MatchString(l) || (plain != "" && re.MatchString(plain)) {
				hit = append(hit, i+1)
			}
		}
		if len(hit) > 0 {
			total += len(hit)
			files++
			found = append(found, match{d.Path, p, hit})
		}
	}
	// Multi-term fallback when a non-regex phrase wasn't found verbatim:
	// match lines containing all whitespace-split terms.
	if total == 0 && !a.Regex {
		if terms := splitQueryTerms(a.Pattern); len(terms) > 1 {
			if alt, err := regexp.Compile("(?i)" + regexp.QuoteMeta(terms[0])); err == nil {
				re = alt
			}
			for _, d := range docs {
				ok, err := within(a.Scope, d.Path)
				if err != nil || !ok {
					continue
				}
				p, err := f.Page(d.Path)
				if err != nil {
					continue
				}
				var hit []int
				for i, l := range p.Lines {
					plain := l
					if i < len(p.PlainLines) && p.PlainLines[i] != "" {
						plain = p.PlainLines[i]
					}
					low := strings.ToLower(plain + " " + l)
					all := true
					for _, t := range terms {
						if !strings.Contains(low, t) {
							all = false
							break
						}
					}
					if all {
						hit = append(hit, i+1)
					}
				}
				if len(hit) > 0 {
					total += len(hit)
					files++
					found = append(found, match{d.Path, p, hit})
				}
			}
		}
	}
	var b strings.Builder
	if total == 0 {
		fmt.Fprintf(&b, "No matches for %q", a.Pattern)
		if a.Scope != "" {
			fmt.Fprintf(&b, " in %s", a.Scope)
		}
		b.WriteString(".\n")
	} else {
		offset := max(0, a.Offset)
		end := min(total, offset+MaxGrep)
		fmt.Fprintf(&b, "%d matches in %d documents", total, files)
		if offset > 0 || end < total {
			fmt.Fprintf(&b, "; showing %d-%d", offset+1, end)
		}
		b.WriteString(".\n")
		n := 0
		for _, m := range found {
			// The matches of this document within the page asked for.
			var shown []int
			for _, l := range m.lines {
				if n >= offset && n < end {
					shown = append(shown, l)
				}
				n++
			}
			if len(shown) == 0 {
				continue
			}
			last := -1
			for _, l := range shown {
				from, to := max(1, l-ctx), min(len(m.page.Lines), l+ctx)
				if last >= 0 && from > last+1 {
					b.WriteString("--\n")
				}
				for i := max(from, last+1); i <= to; i++ {
					sep := "-"
					if slices.Contains(m.lines, i) {
						sep = ":"
					}
					fmt.Fprintf(&b, "%s%s%d%s%s\n", m.path, sep, i, sep, clip(m.page.Lines[i-1], re))
				}
				last = to
			}
			b.WriteString("--\n")
		}
		if end < total {
			fmt.Fprintf(&b, "(more: call again with offset=%d)\n", end)
		}
	}
	if len(failed) > 0 {
		fmt.Fprintf(&b, "Could not read %d documents, not searched: %s\n", len(failed), strings.Join(failed, ", "))
	}
	return b.String(), nil
}

// clip shortens a long line around its first match.
func clip(line string, re *regexp.Regexp) string {
	if utf8.RuneCountInString(line) <= maxLineRunes {
		return line
	}
	runes := []rune(line)
	at := 0
	if loc := re.FindStringIndex(line); loc != nil {
		at = utf8.RuneCountInString(line[:loc[0]])
	}
	start := max(0, at-maxLineRunes/3)
	end := min(len(runes), start+maxLineRunes)
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}

// Read returns lines start to end of a document (1-based, inclusive; 0
// for its start or end), numbered, as "  12→text". It reads at most
// MaxReadLines lines and MaxReadChars characters, and says where to go on.
func (f *FS) Read(rel string, start, end int) (string, error) {
	rel = cleanRel(rel)
	p, err := f.Page(rel)
	if err != nil {
		return "", err
	}
	n := len(p.Lines)
	if n == 0 {
		return "(empty document)\n", nil
	}
	start = max(1, start)
	if start > n {
		return "", fmt.Errorf("the document has %d lines; start_line %d is past its end", n, start)
	}
	if end <= 0 || end > n {
		end = n
	}
	if end < start {
		return "", fmt.Errorf("end_line %d is before start_line %d", end, start)
	}
	end = min(end, start+MaxReadLines-1)
	var b strings.Builder
	chars := 0
	last := start - 1
	more := false
	for i := start; i <= end; i++ {
		line := p.Lines[i-1]
		rem := max(0, MaxReadChars-chars)
		if len(line) > rem {
			if chars > 0 && (len(line) <= MaxReadChars || rem < 256) {
				more = true
				break
			}
			cut := rem
			for cut > 0 && !utf8.RuneStart(line[cut]) {
				cut--
			}
			line = line[:cut] + "… [truncated]"
			chars += cut
			fmt.Fprintf(&b, "%6d→%s\n", i, line)
			last = i
			more = true
			break
		}
		chars += len(line)
		fmt.Fprintf(&b, "%6d→%s\n", i, line)
		last = i
	}
	if last < n {
		fmt.Fprintf(&b, "(lines %d-%d of %d; read on with start_line=%d)\n", start, last, n, last+1)
	} else if start > 1 && !more {
		fmt.Fprintf(&b, "(lines %d-%d of %d, the end)\n", start, last, n)
	}
	return b.String(), nil
}

// Outline lists a document's headings with their lines, outside code.
func (f *FS) Outline(rel string) (string, error) {
	rel = cleanRel(rel)
	p, err := f.Page(rel)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d lines.\n", rel, len(p.Lines))
	fence := ""
	count := 0
	for i, l := range p.Lines {
		t := strings.TrimSpace(l)
		if fence != "" {
			if strings.HasPrefix(t, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(t, "```") {
			fence = t[:strings.IndexFunc(t+"x", func(r rune) bool { return r != '`' })]
			continue
		}
		if t == "$$" {
			fence = "$$"
			continue
		}
		if strings.HasPrefix(l, "#") {
			level := len(l) - len(strings.TrimLeft(l, "#"))
			if level <= 6 && strings.HasPrefix(l[level:], " ") {
				fmt.Fprintf(&b, "%6d %s%s\n", i+1, strings.Repeat("  ", level-1), strings.TrimSpace(l[level:]))
				count++
			}
		}
	}
	if count == 0 {
		b.WriteString("(no headings)\n")
	}
	return b.String(), nil
}

// Links are a document's [[links]] (with the documents they open), the
// documents linking to it, and its #tags.
func (f *FS) Links(rel string) (string, error) {
	rel = cleanRel(rel)
	p, err := f.Page(rel)
	if err != nil {
		return "", err
	}
	docs, err := f.Docs()
	if err != nil {
		return "", err
	}
	text := strings.Join(p.Lines, "\n")
	resolve := func(name string) string {
		for _, d := range docs {
			if wiki.Same(vault.Title(d.Path), name) {
				return d.Path
			}
		}
		return ""
	}
	var b strings.Builder
	b.WriteString("Links from this document:\n")
	seen := map[string]bool{}
	out := 0
	for _, l := range wiki.Links(text) {
		key := strings.ToLower(l.Target)
		if seen[key] {
			continue
		}
		seen[key] = true
		out++
		if to := resolve(l.Target); to != "" {
			fmt.Fprintf(&b, "  [[%s]] → %s\n", l.Target, to)
		} else {
			fmt.Fprintf(&b, "  [[%s]] (no such document yet)\n", l.Target)
		}
	}
	if out == 0 {
		b.WriteString("  (none)\n")
	}
	b.WriteString("Documents linking here:\n")
	name := vault.Title(rel)
	in := 0
	for _, d := range docs {
		if d.Path == rel {
			continue
		}
		q, err := f.Page(d.Path)
		if err != nil {
			continue
		}
		for i, l := range q.Lines {
			if wiki.LinksTo(l, name) {
				fmt.Fprintf(&b, "  %s:%d:%s\n", d.Path, i+1, clip(l, regexp.MustCompile(regexp.QuoteMeta("[["))))
				in++
				break
			}
		}
	}
	if in == 0 {
		b.WriteString("  (none)\n")
	}
	tags := wiki.TagNames(text)
	b.WriteString("Tags: ")
	if len(tags) == 0 {
		b.WriteString("(none)")
	}
	for i, t := range tags {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("#" + t)
	}
	b.WriteString("\n")
	return b.String(), nil
}

// Overview is a map of the space for the start of a conversation: every
// document's path, title and modification date, and the top headings of
// each while they fit in maxChars. Past that, documents are listed without
// headings, then counted.
func (f *FS) Overview(maxChars int) (string, error) {
	docs, err := f.Docs()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d documents.\n", len(docs))
	withHeadings := true
	for i, d := range docs {
		if b.Len() > maxChars {
			fmt.Fprintf(&b, "(%d more documents not listed; use glob to list them)\n", len(docs)-i)
			break
		}
		p, err := f.Page(d.Path)
		if err != nil {
			fmt.Fprintf(&b, "- %s (cannot be read)\n", d.Path)
			continue
		}
		fmt.Fprintf(&b, "- %s — %s — %s, %d lines\n", d.Path, p.Title, d.Modified.Format("2006-01-02"), len(p.Lines))
		if withHeadings && b.Len() > maxChars*2/3 {
			withHeadings = false
		}
		if !withHeadings {
			continue
		}
		shown := 0
		for _, l := range p.Lines {
			if strings.HasPrefix(l, "## ") && shown < 8 {
				fmt.Fprintf(&b, "    %s\n", strings.TrimSpace(l[3:]))
				shown++
			}
		}
	}
	return b.String(), nil
}

// SearchArgs are arguments for ranked passage search across the space.
type SearchArgs struct {
	Query string
	Scope string
	Limit int
}

// Search ranks passages (blocks with surrounding heading context) across the
// space using BM25-style term specificity plus title, heading and exact-phrase
// boosts. Unlike Grep, it handles multi-word natural-language queries, Chinese
// n-grams and formatted spans without requiring exact phrasing.
func (f *FS) Search(a SearchArgs) (string, error) {
	q := strings.TrimSpace(a.Query)
	if q == "" {
		return "", fmt.Errorf("query is empty")
	}
	limit := a.Limit
	if limit <= 0 || limit > MaxSearch {
		limit = 10
	}
	docs, err := f.Docs()
	if err != nil {
		return "", err
	}
	tokens := searchTokens(q)
	phrase := strings.ToLower(stripInlineMarkup(q))
	phrases := splitQueryTerms(q)

	type candDoc struct {
		path string
		page *Page
	}
	var cands []candDoc
	var failed []string
	df := make(map[string]int, len(tokens))

	for _, d := range docs {
		ok, err := within(a.Scope, d.Path)
		if err != nil {
			return "", fmt.Errorf("bad scope: %w", err)
		}
		if !ok {
			continue
		}
		p, err := f.Page(d.Path)
		if err != nil {
			failed = append(failed, d.Path)
			continue
		}
		cands = append(cands, candDoc{d.Path, p})
		docText := strings.ToLower(d.Path + "\n" + p.Title + "\n" + strings.Join(p.PlainLines, "\n"))
		for _, tok := range tokens {
			if strings.Contains(docText, tok) {
				df[tok]++
			}
		}
	}

	type hit struct {
		path    string
		page    *Page
		from    int
		to      int
		section string
		score   float64
	}
	var hits []hit
	nDocs := float64(max(1, len(cands)))

	for _, cd := range cands {
		p := cd.page
		titleLow := strings.ToLower(p.Title + " " + vault.Title(cd.path))
		titleBoost := 0.0
		for _, tok := range tokens {
			if strings.Contains(titleLow, tok) {
				idf := math.Log(1 + (nDocs-float64(df[tok])+0.5)/(float64(df[tok])+0.5))
				titleBoost += 1.2 * max(0.35, idf)
			}
		}
		if phrase != "" && utf8.RuneCountInString(phrase) >= 2 && strings.Contains(titleLow, phrase) {
			titleBoost += 3.0
		}

		spans := p.Spans
		if len(spans) == 0 && len(p.Lines) > 0 {
			spans = []Span{{From: 1, To: len(p.Lines)}}
		}
		for _, sp := range spans {
			from := max(1, sp.From)
			to := min(len(p.Lines), sp.To)
			for to > from && strings.TrimSpace(p.Lines[to-1]) == "" {
				to--
			}
			// Standalone ATX heading blocks are already attached as § Section
			// breadcrumbs on the body blocks beneath them; skip them when the
			// document also has body blocks.
			if from == to && len(spans) > 1 && strings.HasPrefix(strings.TrimSpace(p.Lines[from-1]), "#") {
				continue
			}
			var plainBuf strings.Builder
			nonEmpty := false
			for i := from; i <= to; i++ {
				line := p.Lines[i-1]
				if i-1 < len(p.PlainLines) && p.PlainLines[i-1] != "" {
					line = p.PlainLines[i-1]
				}
				if strings.TrimSpace(line) != "" {
					nonEmpty = true
				}
				if plainBuf.Len() > 0 {
					plainBuf.WriteByte('\n')
				}
				plainBuf.WriteString(line)
			}
			if !nonEmpty {
				continue
			}
			sec := p.SectionAt(from)
			bodyLow := strings.ToLower(plainBuf.String())
			secLow := strings.ToLower(sec)

			score := 0.0
			matchedTokens := 0
			bodyRunes := float64(max(24, utf8.RuneCountInString(bodyLow)))

			for _, tok := range tokens {
				tf := strings.Count(bodyLow, tok)
				secHit := strings.Contains(secLow, tok)
				if tf == 0 && !secHit {
					continue
				}
				matchedTokens++
				idf := math.Log(1 + (nDocs-float64(df[tok])+0.5)/(float64(df[tok])+0.5))
				idf = max(0.25, idf)
				if tf > 0 {
					normTF := (float64(tf) * 2.2) / (float64(tf) + 1.2*(0.35+0.65*(bodyRunes/120.0)))
					score += idf * normTF * tokenWeight(tok)
				}
				if secHit {
					score += idf * 0.9 * tokenWeight(tok)
				}
			}
			if phrase != "" && utf8.RuneCountInString(phrase) >= 2 && strings.Contains(bodyLow, phrase) {
				score += 4.5
			}
			if len(phrases) > 1 {
				phraseHits := 0
				for _, pt := range phrases {
					if strings.Contains(bodyLow, pt) || strings.Contains(secLow, pt) || strings.Contains(titleLow, pt) {
						phraseHits++
					}
				}
				if phraseHits == len(phrases) {
					score += 3.5
				} else if phraseHits > 1 {
					score += float64(phraseHits) * 0.8
				}
			}
			if score <= 0 {
				continue
			}
			score += titleBoost * 0.65
			if matchedTokens >= 2 {
				score *= 1.0 + 0.12*float64(min(matchedTokens, 6))
			}
			hits = append(hits, hit{
				path:    cd.path,
				page:    p,
				from:    from,
				to:      to,
				section: sec,
				score:   score,
			})
		}
	}

	slices.SortStableFunc(hits, func(a, b hit) int {
		if a.score > b.score {
			return -1
		}
		if a.score < b.score {
			return 1
		}
		if a.path != b.path {
			return strings.Compare(a.path, b.path)
		}
		return a.from - b.from
	})

	// Cap per-document passages to at most 3 when other documents also match,
	// so one huge file does not crowd out complementary notes.
	var picked []hit
	perDoc := map[string]int{}
	docSet := map[string]bool{}
	for _, h := range hits {
		docSet[h.path] = true
	}
	for _, h := range hits {
		if len(picked) >= limit {
			break
		}
		if len(docSet) > 1 && perDoc[h.path] >= 3 {
			continue
		}
		perDoc[h.path]++
		picked = append(picked, h)
	}

	var b strings.Builder
	if len(picked) == 0 {
		fmt.Fprintf(&b, "No relevant passages for %q", q)
		if a.Scope != "" {
			fmt.Fprintf(&b, " in %s", a.Scope)
		}
		b.WriteString(".\n")
	} else {
		shownDocs := map[string]bool{}
		for _, h := range picked {
			shownDocs[h.path] = true
		}
		fmt.Fprintf(&b, "%d relevant passages in %d documents.\n", len(picked), len(shownDocs))
		firstTokRe := tokenHighlightRegexp(phrases, tokens)
		for idx, h := range picked {
			loc := fmt.Sprintf("L%d", h.from)
			if h.to > h.from {
				loc = fmt.Sprintf("L%d-%d", h.from, h.to)
			}
			sec := ""
			if h.section != "" && h.section != h.page.Title {
				sec = " · § " + h.section
			}
			fmt.Fprintf(&b, "[%d] %s (%s%s, %s)\n", idx+1, h.path, h.page.Title, sec, loc)
			shownLines := min(h.to, h.from+5)
			for lineNo := h.from; lineNo <= shownLines; lineNo++ {
				fmt.Fprintf(&b, "%6d→%s\n", lineNo, clip(h.page.Lines[lineNo-1], firstTokRe))
			}
			if h.to > shownLines {
				fmt.Fprintf(&b, "      … (%d more lines in block; call read on %s start_line=%d end_line=%d)\n", h.to-shownLines, h.path, h.from, h.to)
			}
		}
	}
	if len(failed) > 0 {
		fmt.Fprintf(&b, "Could not read %d documents, not searched: %s\n", len(failed), strings.Join(failed, ", "))
	}
	return b.String(), nil
}

func splitQueryTerms(q string) []string {
	raw := strings.FieldsFunc(strings.ToLower(stripInlineMarkup(q)), func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(",，。！？；：、()（）[]【】\"'“”‘’/|", r)
	})
	var out []string
	seen := map[string]bool{}
	for _, t := range raw {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

var cjkStopRunes = "的了是在有和与及或把被让给对从向到于着过吗呢吧啊呀么什么怎么为什么哪些哪个哪里多少"

func searchTokens(q string) []string {
	clean := strings.ToLower(stripInlineMarkup(q))
	var out []string
	seen := map[string]bool{}
	add := func(tok string) {
		tok = strings.TrimSpace(tok)
		if tok == "" || seen[tok] {
			return
		}
		seen[tok] = true
		out = append(out, tok)
	}

	var word []rune
	var cjk []rune
	flushWord := func() {
		if len(word) >= 2 || (len(word) == 1 && unicode.IsDigit(word[0])) {
			add(string(word))
		}
		word = word[:0]
	}
	flushCJK := func() {
		if len(cjk) == 1 {
			if !strings.ContainsRune(cjkStopRunes, cjk[0]) {
				add(string(cjk))
			}
		} else if len(cjk) >= 2 {
			if len(cjk) <= 6 {
				add(string(cjk))
			}
			for i := 0; i+1 < len(cjk); i++ {
				a, b := cjk[i], cjk[i+1]
				if strings.ContainsRune(cjkStopRunes, a) && strings.ContainsRune(cjkStopRunes, b) {
					continue
				}
				add(string(cjk[i : i+2]))
			}
		}
		cjk = cjk[:0]
	}

	for _, r := range clean {
		switch {
		case isCJK(r):
			flushWord()
			if strings.ContainsRune(cjkStopRunes, r) {
				flushCJK()
			} else {
				cjk = append(cjk, r)
			}
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-':
			flushCJK()
			word = append(word, r)
		default:
			flushWord()
			flushCJK()
		}
	}
	flushWord()
	flushCJK()
	if len(out) == 0 && strings.TrimSpace(clean) != "" {
		add(strings.TrimSpace(clean))
	}
	return out
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		(r >= 0x3040 && r <= 0x30FF) ||
		(r >= 0xAC00 && r <= 0xD7AF)
}

func tokenWeight(tok string) float64 {
	n := utf8.RuneCountInString(tok)
	if n >= 3 {
		return 1.35
	}
	if n == 2 {
		return 1.0
	}
	return 0.55
}

func tokenHighlightRegexp(phrases, tokens []string) *regexp.Regexp {
	for _, s := range append(phrases, tokens...) {
		if s != "" {
			if re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(s)); err == nil {
				return re
			}
		}
	}
	return regexp.MustCompile(".")
}
