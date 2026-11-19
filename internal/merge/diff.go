// Package merge provides Git-style unified diff hunks (with collapsible context
// skip blocks and grapheme-accurate inline spans) and an interactive three-way
// merge engine modeled after JetBrains IntelliJ IDEA's three-pane merge tool.
// It has no dependency on MyGo or any UI package.
package merge

import (
	"strings"

	"github.com/Ciao1019/ArcMD/internal/history"
)

// FileStatus describes how a file changed relative to its synced baseline.
type FileStatus uint8

const (
	StatusModified FileStatus = iota
	StatusAdded
	StatusDeleted
	StatusConflict
)

// LineKind classifies a single row in a unified diff hunk.
type LineKind uint8

const (
	LineNormal LineKind = iota
	LineInsert
	LineDelete
)

// DiffLine is one row in a unified diff hunk.
type DiffLine struct {
	Kind   LineKind
	OldNum int // 1-based line number in old text, 0 for insertions
	NewNum int // 1-based line number in new text, 0 for deletions
	Text   string
	Spans  []history.Span
}

// DiffBlock is either a visible Hunk of lines or a collapsed SkipBlock of
// unchanged context lines between hunks (matching fredrikalindh/ui's DiffViewer).
type DiffBlock struct {
	IsSkip      bool
	Lines       []DiffLine // populated for Hunk, or hidden lines for SkipBlock
	HiddenCount int
}

// FileDiff holds the structured unified diff and summary counts for one file.
type FileDiff struct {
	Blocks  []DiffBlock
	Added   int
	Removed int
}

// SplitLines splits text into normalized lines without trailing '\r'.
// An empty string yields nil (zero lines).
func SplitLines(text string) []string {
	if text == "" {
		return nil
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return []string{""}
	}
	return strings.Split(text, "\n")
}

// JoinLines joins lines back into a LF-terminated document string.
func JoinLines(lines []string, trailingNewline bool) string {
	if len(lines) == 0 {
		return ""
	}
	s := strings.Join(lines, "\n")
	if trailingNewline {
		return s + "\n"
	}
	return s
}

// UnifiedDiff computes a Git-style unified diff from oldText to newText,
// grouping unchanged lines farther than contextLines from any edit into
// collapsible SkipBlocks.
func UnifiedDiff(oldText, newText string, contextLines int) FileDiff {
	if contextLines < 0 {
		contextLines = 3
	}
	oldLines := SplitLines(oldText)
	newLines := SplitLines(newText)
	raw := history.Diff(oldLines, newLines)

	all := make([]DiffLine, 0, len(raw))
	oldNum, newNum := 0, 0
	added, removed := 0, 0

	for _, l := range raw {
		switch l.Op {
		case history.Same:
			oldNum++
			newNum++
			all = append(all, DiffLine{
				Kind:   LineNormal,
				OldNum: oldNum,
				NewNum: newNum,
				Text:   l.Text,
			})
		case history.Removed:
			oldNum++
			removed++
			all = append(all, DiffLine{
				Kind:   LineDelete,
				OldNum: oldNum,
				Text:   l.Text,
				Spans:  l.Spans,
			})
		case history.Added:
			newNum++
			added++
			all = append(all, DiffLine{
				Kind:   LineInsert,
				NewNum: newNum,
				Text:   l.Text,
				Spans:  l.Spans,
			})
		}
	}

	if len(all) == 0 {
		return FileDiff{}
	}

	// Mark lines within contextLines of any Insert/Delete as visible.
	keep := make([]bool, len(all))
	hasChanges := false
	for i, dl := range all {
		if dl.Kind != LineNormal {
			hasChanges = true
			lo := max(0, i-contextLines)
			hi := min(len(all)-1, i+contextLines)
			for j := lo; j <= hi; j++ {
				keep[j] = true
			}
		}
	}
	if !hasChanges {
		return FileDiff{
			Blocks: []DiffBlock{{
				IsSkip:      true,
				Lines:       all,
				HiddenCount: len(all),
			}},
		}
	}

	// Avoid collapsing tiny 1-line gaps between two hunks.
	for i := 1; i < len(all)-1; i++ {
		if !keep[i] && keep[i-1] && keep[i+1] {
			keep[i] = true
		}
	}

	var blocks []DiffBlock
	for i := 0; i < len(all); {
		if keep[i] {
			j := i + 1
			for j < len(all) && keep[j] {
				j++
			}
			blocks = append(blocks, DiffBlock{
				IsSkip: false,
				Lines:  all[i:j],
			})
			i = j
		} else {
			j := i + 1
			for j < len(all) && !keep[j] {
				j++
			}
			blocks = append(blocks, DiffBlock{
				IsSkip:      true,
				Lines:       all[i:j],
				HiddenCount: j - i,
			})
			i = j
		}
	}

	return FileDiff{
		Blocks:  blocks,
		Added:   added,
		Removed: removed,
	}
}
