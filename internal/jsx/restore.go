package jsx

import (
	"strings"
	"unicode"
)

// RestoreSource turns a compiled projection (fenced islands) back into the
// MDX the user should see on disk. Only fences tagged with this compile's
// namespace are unwrapped; ordinary code fences stay intact.
func RestoreSource(markdown, namespace string) string {
	if markdown == "" || namespace == "" {
		return markdown
	}
	var b strings.Builder
	b.Grow(len(markdown))
	i := 0
	for i < len(markdown) {
		start, body, end, ok := nextIslandFence(markdown, i, namespace)
		if !ok {
			b.WriteString(markdown[i:])
			break
		}
		b.WriteString(markdown[i:start])
		b.WriteString(body)
		i = end
	}
	return b.String()
}

func nextIslandFence(md string, from int, namespace string) (start int, body string, end int, ok bool) {
	for i := from; i < len(md); i++ {
		if md[i] != '`' {
			continue
		}
		if i > 0 && md[i-1] != '\n' {
			continue
		}
		n := 0
		for i+n < len(md) && md[i+n] == '`' {
			n++
		}
		if n < 3 {
			continue
		}
		lineEnd := strings.IndexByte(md[i+n:], '\n')
		if lineEnd < 0 {
			return 0, "", 0, false
		}
		lineEnd += i + n
		info := strings.TrimSpace(md[i+n : lineEnd])
		if info == "" || strings.ContainsFunc(info, unicode.IsSpace) {
			continue
		}
		if !IsIslandLanguage(info, namespace) {
			continue
		}
		bodyStart := lineEnd + 1
		closeAt := findFenceClose(md, bodyStart, n)
		if closeAt < 0 {
			return 0, "", 0, false
		}
		body := md[bodyStart:closeAt]
		body = strings.TrimSuffix(body, "\n")
		j := closeAt
		for j < len(md) && md[j] == '`' {
			j++
		}
		// Keep the newline after the closing fence: it belongs to the
		// surrounding document, not the unwrapped island.
		return i, body, j, true
	}
	return 0, "", 0, false
}

func findFenceClose(md string, from, ticks int) int {
	for i := from; i < len(md); i++ {
		if md[i] != '`' {
			continue
		}
		if i > 0 && md[i-1] != '\n' {
			continue
		}
		n := 0
		for i+n < len(md) && md[i+n] == '`' {
			n++
		}
		if n >= ticks {
			rest := md[i+n:]
			if rest == "" || rest[0] == '\n' || (len(rest) > 0 && rest[0] == '\r') {
				return i
			}
		}
	}
	return -1
}
