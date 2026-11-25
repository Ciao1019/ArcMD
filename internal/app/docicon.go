package app

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// A document's icon uses a leading emoji when the author puts one in the
// title; otherwise it shows a clean monochrome Lucide glyph tailored to the
// file type (Markdown, Database, Whiteboard, PDF, EPUB, HTML).

// fileIconGlyph returns the monochrome Lucide icon for the document path rel.
func fileIconGlyph(rel string) *ui.SVG {
	switch {
	case vault.IsDatabase(rel):
		return iconDatabase
	case vault.IsWhiteboard(rel):
		return iconWhiteboard
	case vault.IsPDF(rel):
		return iconFilePDF
	case vault.IsEPUB(rel):
		return iconBookOpen
	case vault.IsHTML(rel):
		return iconFileCode
	default:
		return iconFile
	}
}

// fileIcon draws the icon of rel/title: a leading emoji if present, otherwise
// the file type's monochrome Lucide glyph in a size×size box.
func fileIcon(c *ui.Context, rel, title string, size float32) ui.Element {
	if e := leadingEmoji(title); e != "" {
		return ui.Box(c).Size(size, size).Shrink(0).Center().Children(func() {
			ui.Text(c, e).FontSize(size * 0.86).NoWrap()
		})
	}
	ink := docIconInk(c.Theme())
	return ui.Box(c).Size(size, size).Shrink(0).Center().Children(func() {
		ui.Icon(c, fileIconGlyph(rel)).FontSize(size * 0.94).TextColor(ink)
	})
}

// docIcon draws the icon of a Markdown document titled title, size DIPs square.
func docIcon(c *ui.Context, title string, size float32) ui.Element {
	return fileIcon(c, "", title, size)
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

// docInitial returns the first letter or digit of title in upper case.
func docInitial(title string) string {
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return string(unicode.ToUpper(r))
		}
	}
	return "·"
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
