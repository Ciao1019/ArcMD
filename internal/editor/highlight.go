package editor

import (
	"github.com/egoist/mygo/ui"
	"regexp"
	"strings"
)

var codeToken = regexp.MustCompile("(?s)/\\*.*?\\*/|//[^\\n]*|#[^\\n]*|\"(?:\\\\.|[^\"\\\\])*\"|'(?:\\\\.|[^'\\\\])*'|[A-Za-z_][A-Za-z_0-9]*|[0-9]+(?:\\.[0-9]+)?")
var codeKeywords = map[string]bool{}

func init() {
	for _, word := range strings.Fields("func function return if else for while range break continue switch case default type struct interface var const let import from package def class public private static fn pub impl enum use match true false nil null None True False async await try catch throw new select go defer") {
		codeKeywords[word] = true
	}
}

// Code spans change only paint. Their text and font metrics match the buffer.
func codeSpans(text string, pg Page) []ui.Span {
	var out []ui.Span
	start := 0
	for _, match := range codeToken.FindAllStringIndex(text, -1) {
		if match[0] > start {
			out = append(out, ui.Span{Text: text[start:match[0]]})
		}
		token := text[match[0]:match[1]]
		end := match[1]
		color := pg.CodeText
		switch {
		case strings.HasPrefix(token, "//") || strings.HasPrefix(token, "/*") || strings.HasPrefix(token, "#"):
			color = pg.Tokens.Comment
		case strings.HasPrefix(token, "\"") || strings.HasPrefix(token, "'"):
			color = pg.Tokens.String
		case codeKeywords[token]:
			color = pg.Tokens.Keyword
		case token[0] >= '0' && token[0] <= '9':
			color = pg.Tokens.Number
		case token[0] >= 'A' && token[0] <= 'Z':
			color = pg.Tokens.Type
		case end < len(text) && text[end] == '(':
			color = pg.Tokens.Function
		}
		out = append(out, ui.Span{Text: token, Color: color})
		start = match[1]
	}
	if start < len(text) {
		out = append(out, ui.Span{Text: text[start:]})
	}
	return out
}
