package app

import (
	"hash/fnv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// A document's icon is made from its name, as a browser shows a site's
// favicon: nothing to set and nothing read from the file. A name starting
// with an emoji shows the emoji; any other shows its first letter in one of
// a few quiet colors, on a tint of that color. The color follows the name,
// so a document keeps it as it moves, and documents side by side seldom
// share one.

// docIcon draws the icon of the document titled title, size DIPs square.
func docIcon(c *ui.Context, title string, size float32) ui.Element {
	if e := leadingEmoji(title); e != "" {
		return ui.Box(c).Size(size, size).Shrink(0).Center().Children(func() {
			ui.Text(c, e).FontSize(size * 0.86).NoWrap()
		})
	}
	ink, fill := docIconColors(c.Theme(), title)
	return ui.Box(c).Size(size, size).Shrink(0).Radius(size * 0.28).Background(fill).Center().Children(func() {
		ui.Text(c, docInitial(title)).FontSize(size * 0.6).FontWeight(650).TextColor(ink).NoWrap()
	})
}

// docName is a title as it shows beside its icon: without the emoji the
// icon already shows.
func docName(title string) string {
	if e := leadingEmoji(title); e != "" {
		if rest := strings.TrimSpace(strings.TrimLeft(title, " ")[len(e):]); rest != "" {
			return rest
		}
	}
	return title
}

// docInitial is the letter of a document's icon: the first letter or digit
// of its title, in upper case.
func docInitial(title string) string {
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return string(unicode.ToUpper(r))
		}
	}
	return "·"
}

// docColorIndex picks the document's color from its title.
func docColorIndex(title string) int {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(strings.TrimSpace(title))))
	return int(h.Sum32() % uint32(len(docPalette)))
}

// leadingEmoji is the emoji a title starts with, with what joins and
// modifies it, or "".
func leadingEmoji(title string) string {
	s := strings.TrimLeft(title, " ")
	r, n := utf8.DecodeRuneInString(s)
	if !isEmoji(r) {
		return ""
	}
	end := n
	for end < len(s) {
		r, n := utf8.DecodeRuneInString(s[end:])
		switch {
		case r == 0xFE0F || r == 0x20E3 || r >= 0x1F3FB && r <= 0x1F3FF || r >= 0xE0020 && r <= 0xE007F:
			end += n // presentation, keycap, skin tone and tag marks
		case r == 0x200D:
			end += n // a joiner and the emoji after it
			if end < len(s) {
				_, m := utf8.DecodeRuneInString(s[end:])
				end += m
			}
		case r >= 0x1F1E6 && r <= 0x1F1FF && end == n:
			end += n // the second letter of a flag
		default:
			return s[:end]
		}
	}
	return s[:end]
}

// isEmoji reports whether r starts an emoji: the pictographs, the symbols
// and dingbats shown as emoji, and flags' letters.
func isEmoji(r rune) bool {
	return r >= 0x1F000 && r <= 0x1FAFF || r >= 0x2600 && r <= 0x27BF || r >= 0x2B00 && r <= 0x2BFF ||
		r == 0x2194 || r == 0x2139 || r >= 0x2300 && r <= 0x23FF || r == 0x00A9 || r == 0x00AE
}
