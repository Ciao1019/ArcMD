// Package fuzzy ranks strings against a typed query, for the command bar.
package fuzzy

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/pinyin"
)

// Score returns how well query matches s, and whether it matches at all:
// whitespace-separated tokens are combined with AND semantics, and each token
// matches either as a case-insensitive ordered subsequence or via Mandarin
// pinyin syllables/initials.
func Score(query, s string) (int, bool) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return 0, true
	}
	fields := strings.Fields(query)
	if len(fields) <= 1 {
		return scoreToken(query, s)
	}
	total := 0
	for _, tok := range fields {
		sc, ok := scoreToken(tok, s)
		if !ok {
			return 0, false
		}
		total += sc
	}
	// Reward sequential full-query matches over out-of-order token matches.
	if seq, ok := scoreToken(query, s); ok {
		total += 500 + seq/4
	}
	return total, true
}

// ScoreWithHint matches query against title first, falling back to splitting
// query tokens across title and hint (e.g. "work zb" matching hint "work" +
// title "周报"). Matches satisfied by title alone always rank higher.
func ScoreWithHint(query, title, hint string) (int, bool) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return 0, true
	}
	if sc, ok := Score(query, title); ok {
		return sc + 5000, true
	}
	if strings.TrimSpace(hint) == "" {
		return 0, false
	}
	fields := strings.Fields(query)
	total := 0
	for _, tok := range fields {
		st, okT := scoreToken(tok, title)
		sh, okH := scoreToken(tok, hint)
		if !okT && !okH {
			return 0, false
		}
		if okT {
			total += st + 300
		} else {
			total += sh / 2
		}
	}
	return total, true
}

func scoreToken(token, s string) (int, bool) {
	litScore, litOK := scoreLiteral(token, s)
	if !pinyin.HasHanzi(s) {
		return litScore, litOK
	}
	pyScore, pyOK := pinyin.ScoreToken(token, s)
	if !litOK {
		return pyScore, pyOK
	}
	// Literal rune matches (e.g. exact Chinese or exact ASCII) outrank pinyin.
	litBoosted := litScore + 1000
	if !pyOK || litBoosted >= pyScore {
		return litBoosted, true
	}
	return pyScore, true
}

func scoreLiteral(query, s string) (int, bool) {
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
