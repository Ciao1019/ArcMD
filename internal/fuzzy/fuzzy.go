// Package fuzzy ranks strings against a typed query, for the command bar.
package fuzzy

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Score returns how well query matches s, and whether it matches at all:
// every rune of the query must appear in s in order, ignoring case. Higher
// is better: runs of consecutive runes, matches at the start of words and
// a match at the very start score more; an empty query matches everything
// with 0.
func Score(query, s string) (int, bool) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return 0, true
	}
	ls := strings.ToLower(s)
	score, run, qi := 0, 0, 0
	prev := ' '
	for i, r := range ls {
		if qi >= len(query) {
			break
		}
		q, size := utf8.DecodeRuneInString(query[qi:])
		if r == q {
			qi += size
			run++
			score += 1 + run*2
			if i == 0 {
				score += 8
			} else if !unicode.IsLetter(prev) && !unicode.IsDigit(prev) {
				score += 5
			}
		} else {
			run = 0
		}
		prev = r
	}
	if qi < len(query) {
		return 0, false
	}
	// Prefer shorter strings among equal matches.
	return score*100 - len(s), true
}
