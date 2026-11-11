package editor

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/ui"
)

type autoPair struct {
	start, end  int
	open, close string
}

var bracketPairs = map[string]string{"(": ")", "[": "]", "{": "}", "\"": "\"", "'": "'", "「": "」", "『": "』", "（": "）", "【": "】", "《": "》", "“": "”", "‘": "’"}

func (e *Editor) pairInput(r *ui.TextInputRange, text string) bool {
	if e.noAutoPair || e.ReadOnly {
		e.pairs = nil
		return false
	}
	if e.pairRevision != e.Revision() {
		e.pairs = nil
	}
	target := e.Selection().Range
	if e.mark != nil {
		target = *e.mark
	}
	if r != nil {
		target = e.validRange(*r)
	}
	l := e.LeafAt(target.Start)
	if literalBlock(l.Block.Kind) || e.CurrentStyle().Code {
		e.pairs = nil
		return false
	}
	// An IME owns its preedit range. Pair only the final committed character,
	// never the provisional candidate or an arbitrary replacement range.
	if e.mark == nil && r == nil && target.Start == target.End {
		for i := len(e.pairs) - 1; i >= 0; i-- {
			p := e.pairs[i]
			if p.end == target.Start && p.close == text && e.slice(p.end, p.end+document.Len(text)) == text {
				e.SetSelection(p.end+document.Len(text), p.end+document.Len(text))
				e.pairs = append(e.pairs[:i], e.pairs[i+1:]...)
				return true
			}
		}
	}
	close, ok := bracketPairs[text]
	if utf8.RuneCountInString(text) != 1 {
		ok = false
	}
	if !ok {
		e.shiftPairs(target, document.Len(text))
		e.Buffer.ReplaceText(r, text)
		e.pairRevision = e.Revision()
		return true
	}
	if target.Start > l.Start {
		before := e.slice(l.Start, target.Start)
		prev, _ := utf8.DecodeLastRuneInString(before)
		if prev == '\\' || (text == "'" || text == "\"") && (unicode.IsLetter(prev) || unicode.IsDigit(prev)) || strings.Contains(before, "://") {
			e.pairs = nil
			return false
		}
	}
	// Quotes within a word are literal; selected words are still wrapped.
	if target.Start == target.End && target.End < l.End && (text == "'" || text == "\"") {
		next, _ := utf8.DecodeRuneInString(e.slice(target.End, l.End))
		if unicode.IsLetter(next) || unicode.IsDigit(next) {
			return false
		}
	}
	a, z := target.Start, target.End
	n := document.Len(text)
	selected := e.slice(a, z)
	if e.mark != nil {
		selected = ""
	}
	e.shiftPairs(target, 2*n+document.Len(selected))
	if selected != "" && e.mark == nil {
		// Insert at the ends so each selected run retains its own marks.
		e.Engine.Edit(func() { e.Engine.Replace(z, z, close); e.Engine.Replace(a, a, text); e.SetSelection(a+n, z+n) })
		e.sync()
	} else {
		e.Buffer.ReplaceText(r, text+close)
		e.SetSelection(a+n, a+n)
	}
	end := a + n + document.Len(selected)
	e.pairs = append(e.pairs, autoPair{a, end, text, close})
	e.pairRevision = e.Revision()
	return true
}

func (e *Editor) shiftPairs(r ui.TextInputRange, inserted int) {
	delta := inserted - (r.End - r.Start)
	kept := e.pairs[:0]
	for _, p := range e.pairs {
		if r.Start <= p.start && r.End > p.start || r.Start <= p.end && r.End > p.end {
			continue
		}
		if p.start >= r.End {
			p.start += delta
		}
		if p.end >= r.End {
			p.end += delta
		}
		kept = append(kept, p)
	}
	e.pairs = kept
}

func (e *Editor) deletePair() bool {
	if e.mark != nil || e.Anchor != e.Caret || e.pairRevision != e.Revision() {
		e.pairs = nil
		return false
	}
	for i, p := range e.pairs {
		if e.Caret == p.end && p.end == p.start+document.Len(p.open) && e.slice(p.start, p.end+document.Len(p.close)) == p.open+p.close {
			r := ui.TextInputRange{Start: p.start, End: p.end + document.Len(p.close)}
			e.pairs = append(e.pairs[:i], e.pairs[i+1:]...)
			e.shiftPairs(r, 0)
			e.Buffer.ReplaceText(&r, "")
			e.pairRevision = e.Revision()
			return true
		}
	}
	return false
}
