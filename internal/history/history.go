// Package history keeps earlier versions of a space's documents, so that
// what was written can be seen and brought back: each document's versions
// are files in the space's hidden .arcmd/history folder, at the document's
// own path, named by the time they were taken. The package has no
// dependency on the window.
package history

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Ciao1019/ArcMD/internal/fsutil"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

// Dir is the folder of a space holding its documents' versions.
const Dir = ".arcmd/history"

// Keep is how many versions of a document are kept; Gap how long a version
// takes in the edits after it before another is taken: edits within Gap of
// the last version replace it.
const (
	Keep = 100
	Gap  = 10 * time.Minute
)

// stamp names a version file by its time, so that names sort as times.
const stamp = "20060102-150405.000"

// Version is an earlier version of a document.
type Version struct {
	Time time.Time
	// File is the version's file name in the document's history folder.
	File string
	Size int64
	// Kept versions are never replaced by later edits: the version a
	// document had when first changed, or any named milestone.
	Kept  bool
	Label string
}

const labelsFile = "labels.json"

func loadLabelsFromDir(dir string) map[string]string {
	data, err := os.ReadFile(filepath.Join(dir, labelsFile))
	if err != nil {
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func saveLabelsToDir(dir string, labels map[string]string) error {
	path := filepath.Join(dir, labelsFile)
	cleaned := make(map[string]string, len(labels))
	for k, v := range labels {
		if t := strings.TrimSpace(v); k != "" && t != "" {
			cleaned[k] = t
		}
	}
	if len(cleaned) == 0 {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	data, err := json.MarshalIndent(cleaned, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data, 0o600)
}

// folder is the history folder of the document at rel of the space at
// root.
func folder(root, rel string) (string, error) {
	if _, err := vault.Resolve(root, rel); err != nil {
		return "", err
	}
	return filepath.Join(root, filepath.FromSlash(Dir), filepath.FromSlash(rel)), nil
}

func snapshotStem(name string) (base string, ok bool) {
	switch {
	case strings.HasSuffix(name, ".md"):
		return strings.TrimSuffix(name, ".md"), true
	case strings.HasSuffix(name, ".arcmd"):
		return strings.TrimSuffix(name, ".arcmd"), true
	}
	return "", false
}

func snapshotExt(rel string) string {
	if strings.EqualFold(filepath.Ext(rel), ".arcmd") {
		return ".arcmd"
	}
	return ".md"
}

// List returns the versions of the document at rel, newest first.
func List(root, rel string) ([]Version, error) {
	dir, err := folder(root, rel)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	labels := loadLabelsFromDir(dir)
	var out []Version
	for _, e := range entries {
		name := e.Name()
		base, ok := snapshotStem(name)
		if e.IsDir() || !ok {
			continue
		}
		kept := strings.HasSuffix(base, "~")
		at, err := time.ParseInLocation(stamp, strings.TrimSuffix(base, "~"), time.Local)
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		lbl := strings.TrimSpace(labels[name])
		out = append(out, Version{Time: at, File: name, Size: info.Size(), Kept: kept || lbl != "", Label: lbl})
	}
	slices.SortFunc(out, func(a, b Version) int { return b.Time.Compare(a.Time) })
	return out, nil
}

// Read returns the content of a version.
func Read(root, rel string, v Version) (string, error) {
	dir, err := folder(root, rel)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(v.File, `/\`) || v.File == ".." {
		return "", errors.New("history: invalid version")
	}
	data, err := os.ReadFile(filepath.Join(dir, v.File))
	return string(data), err
}

// Record takes a version of the document at rel holding content, at now.
// A content the last version already holds is not taken again; one within
// Gap of the last version replaces it, unless keep is set, as for the
// version a document had before this session first changed it. The oldest
// versions beyond Keep go.
func Record(root, rel, content string, now time.Time, keep bool) error {
	dir, err := folder(root, rel)
	if err != nil {
		return err
	}
	versions, err := List(root, rel)
	if err != nil {
		return err
	}
	if len(versions) > 0 {
		last := versions[0]
		if old, err := Read(root, rel, last); err == nil && old == content {
			return nil
		}
		if !keep && !last.Kept && now.Sub(last.Time) < Gap && !last.Time.After(now) {
			if err := os.Remove(filepath.Join(dir, last.File)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			versions = versions[1:]
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ext := snapshotExt(rel)
	// Never overwrite a kept or labeled snapshot on millisecond collision.
	for {
		stampStr := now.Format(stamp)
		protected := false
		for _, v := range versions {
			if base, ok := snapshotStem(v.File); ok && strings.TrimSuffix(base, "~") == stampStr {
				if v.Kept || v.Label != "" {
					protected = true
					break
				}
			}
		}
		if !protected {
			break
		}
		now = now.Add(time.Millisecond)
	}
	stampStr := now.Format(stamp)
	name := stampStr + ext
	if keep {
		name = stampStr + "~" + ext
	}
	for _, v := range versions {
		if base, ok := snapshotStem(v.File); ok && strings.TrimSuffix(base, "~") == stampStr && !v.Kept && v.Label == "" {
			// Two non-milestone versions in one millisecond: the later wins.
			_ = os.Remove(filepath.Join(dir, v.File))
		}
	}
	if err := fsutil.WriteFileAtomic(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		return err
	}
	unkeptAllowed := Keep
	if !keep {
		unkeptAllowed = Keep - 1
	}
	unkeptSeen := 0
	for _, v := range versions {
		if v.Kept || v.Label != "" {
			continue
		}
		unkeptSeen++
		if unkeptSeen > unkeptAllowed {
			_ = os.Remove(filepath.Join(dir, v.File))
		}
	}
	return nil
}

// SetLabel names an existing version as a milestone (or clears its label when
// label is empty) inside labels.json.
func SetLabel(root, rel, file, label string) error {
	dir, err := folder(root, rel)
	if err != nil {
		return err
	}
	if file == "" || file == "." || file == ".." || strings.ContainsAny(file, `/\`) {
		return errors.New("history: invalid version")
	}
	if _, err := os.Stat(filepath.Join(dir, file)); err != nil {
		return err
	}
	labels := loadLabelsFromDir(dir)
	if labels == nil {
		labels = map[string]string{}
	}
	trimmed := strings.TrimSpace(label)
	if trimmed == "" {
		delete(labels, file)
	} else {
		labels[file] = trimmed
	}
	return saveLabelsToDir(dir, labels)
}

// SaveMilestone records content as a named, non-coalescing milestone version.
// If the newest version already matches content and has no label, it is
// promoted to a milestone in place.
func SaveMilestone(root, rel, content, label string, now time.Time) (Version, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return Version{}, errors.New("history: empty milestone label")
	}
	dir, err := folder(root, rel)
	if err != nil {
		return Version{}, err
	}
	versions, err := List(root, rel)
	if err != nil {
		return Version{}, err
	}
	if len(versions) > 0 {
		last := versions[0]
		if last.Label == "" {
			if old, err := Read(root, rel, last); err == nil && old == content {
				if err := SetLabel(root, rel, last.File, label); err != nil {
					return Version{}, err
				}
				last.Kept = true
				last.Label = label
				return last, nil
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Version{}, err
	}
	for {
		stampStr := now.Format(stamp)
		collision := false
		for _, v := range versions {
			if base, ok := snapshotStem(v.File); ok && strings.TrimSuffix(base, "~") == stampStr {
				collision = true
				break
			}
		}
		if !collision {
			break
		}
		now = now.Add(time.Millisecond)
	}
	name := now.Format(stamp) + "~" + snapshotExt(rel)
	if err := fsutil.WriteFileAtomic(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		return Version{}, err
	}
	labels := loadLabelsFromDir(dir)
	if labels == nil {
		labels = map[string]string{}
	}
	labels[name] = label
	if err := saveLabelsToDir(dir, labels); err != nil {
		return Version{}, err
	}
	size := int64(len(content))
	if info, err := os.Stat(filepath.Join(dir, name)); err == nil {
		size = info.Size()
	}
	return Version{Time: now, File: name, Size: size, Kept: true, Label: label}, nil
}

// Move takes the versions of the file or folder at from along to to, as
// the document moves or is renamed.
func Move(root, from, to string) error {
	src, err := folder(root, from)
	if err != nil {
		return err
	}
	dst, err := folder(root, to)
	if err != nil {
		return err
	}
	if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if _, err := os.Stat(dst); err == nil {
		return nil // the name's history stays with the name
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

// Op is a line's part in a difference.
type Op int8

const (
	Same Op = iota
	Added
	Removed
)

// Span is a grapheme-aligned segment inside a changed line.
type Span struct {
	Text    string
	Changed bool
}

// Line is a line of a difference between two texts.
type Line struct {
	Op    Op
	Text  string
	Spans []Span
}

// Diff returns the lines that turn a into b: unchanged, removed from a
// and added in b, in order, by the longest common subsequence of lines,
// with intra-line grapheme-cluster diffs annotated for modified pairs.
// Very long texts compare their changed middle only.
func Diff(a, b []string) []Line {
	// Common head and tail first: most edits touch a few lines.
	head := 0
	for head < len(a) && head < len(b) && a[head] == b[head] {
		head++
	}
	tail := 0
	for tail < len(a)-head && tail < len(b)-head && a[len(a)-1-tail] == b[len(b)-1-tail] {
		tail++
	}
	var out []Line
	for _, s := range a[:head] {
		out = append(out, Line{Op: Same, Text: s})
	}
	ma, mb := a[head:len(a)-tail], b[head:len(b)-tail]
	if len(ma)*len(mb) > 4_000_000 {
		for _, s := range ma {
			out = append(out, Line{Op: Removed, Text: s})
		}
		for _, s := range mb {
			out = append(out, Line{Op: Added, Text: s})
		}
	} else {
		// lcs[i][j] is the common length of ma[i:] and mb[j:].
		lcs := make([][]int32, len(ma)+1)
		for i := range lcs {
			lcs[i] = make([]int32, len(mb)+1)
		}
		for i := len(ma) - 1; i >= 0; i-- {
			for j := len(mb) - 1; j >= 0; j-- {
				if ma[i] == mb[j] {
					lcs[i][j] = lcs[i+1][j+1] + 1
				} else {
					lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
				}
			}
		}
		i, j := 0, 0
		for i < len(ma) || j < len(mb) {
			switch {
			case i < len(ma) && j < len(mb) && ma[i] == mb[j]:
				out = append(out, Line{Op: Same, Text: ma[i]})
				i++
				j++
			case j < len(mb) && (i == len(ma) || lcs[i][j+1] >= lcs[i+1][j]):
				out = append(out, Line{Op: Added, Text: mb[j]})
				j++
			default:
				out = append(out, Line{Op: Removed, Text: ma[i]})
				i++
			}
		}
	}
	for _, s := range a[len(a)-tail:] {
		out = append(out, Line{Op: Same, Text: s})
	}
	return AnnotateInlineDiff(out)
}

// AnnotateInlineDiff pairs corresponding Removed and Added lines within each
// non-Same hunk and populates Spans with grapheme-cluster diff highlights.
// Lines with less than 20% grapheme similarity are left without Spans.
func AnnotateInlineDiff(lines []Line) []Line {
	for i := 0; i < len(lines); {
		if lines[i].Op == Same {
			i++
			continue
		}
		j := i
		var remIdx, addIdx []int
		for j < len(lines) && lines[j].Op != Same {
			switch lines[j].Op {
			case Removed:
				remIdx = append(remIdx, j)
			case Added:
				addIdx = append(addIdx, j)
			}
			j++
		}
		n := min(len(remIdx), len(addIdx))
		for k := 0; k < n; k++ {
			ri, ai := remIdx[k], addIdx[k]
			rs, as := inlineGraphemeDiff(lines[ri].Text, lines[ai].Text)
			lines[ri].Spans = rs
			lines[ai].Spans = as
		}
		i = j
	}
	return lines
}

func inlineGraphemeDiff(aText, bText string) (remSpans, addSpans []Span) {
	ga := splitGraphemes(aText)
	gb := splitGraphemes(bText)
	if len(ga) == 0 || len(gb) == 0 || len(ga)*len(gb) > 250_000 {
		return nil, nil
	}
	pre := 0
	for pre < len(ga) && pre < len(gb) && ga[pre] == gb[pre] {
		pre++
	}
	suf := 0
	for suf < len(ga)-pre && suf < len(gb)-pre && ga[len(ga)-1-suf] == gb[len(gb)-1-suf] {
		suf++
	}
	ma := ga[pre : len(ga)-suf]
	mb := gb[pre : len(gb)-suf]
	lcs := make([][]int32, len(ma)+1)
	for i := range lcs {
		lcs[i] = make([]int32, len(mb)+1)
	}
	for i := len(ma) - 1; i >= 0; i-- {
		for j := len(mb) - 1; j >= 0; j-- {
			if ma[i] == mb[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	common := pre + int(lcs[0][0]) + suf
	if common == 0 || 2*common*5 < len(ga)+len(gb) || common >= max(len(ga), len(gb)) {
		return nil, nil
	}
	push := func(out *[]Span, text string, changed bool) {
		if text == "" {
			return
		}
		if n := len(*out); n > 0 && (*out)[n-1].Changed == changed {
			(*out)[n-1].Text += text
			return
		}
		*out = append(*out, Span{Text: text, Changed: changed})
	}
	for k := 0; k < pre; k++ {
		push(&remSpans, ga[k], false)
		push(&addSpans, gb[k], false)
	}
	i, j := 0, 0
	for i < len(ma) || j < len(mb) {
		switch {
		case i < len(ma) && j < len(mb) && ma[i] == mb[j]:
			push(&remSpans, ma[i], false)
			push(&addSpans, mb[j], false)
			i++
			j++
		case i < len(ma) && (j == len(mb) || lcs[i+1][j] >= lcs[i][j+1]):
			push(&remSpans, ma[i], true)
			i++
		default:
			push(&addSpans, mb[j], true)
			j++
		}
	}
	for k := 0; k < suf; k++ {
		push(&remSpans, ga[len(ga)-suf+k], false)
		push(&addSpans, gb[len(gb)-suf+k], false)
	}
	return remSpans, addSpans
}

func splitGraphemes(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	i := 0
	for i < len(s) {
		start := i
		r, w := utf8.DecodeRuneInString(s[i:])
		i += w
		if r == '\r' && i < len(s) && s[i] == '\n' {
			i++
			out = append(out, s[start:i])
			continue
		}
		if r == '\r' || r == '\n' {
			out = append(out, s[start:i])
			continue
		}
		riPair := isRegionalIndicator(r)
		hangulState := hangulClass(r)
		for i < len(s) {
			nr, nw := utf8.DecodeRuneInString(s[i:])
			if nr == '\r' || nr == '\n' {
				break
			}
			if nr == 0x200D { // ZWJ joins the next base cluster too.
				i += nw
				riPair = false
				if i < len(s) {
					jr, jw := utf8.DecodeRuneInString(s[i:])
					if jr != '\r' && jr != '\n' {
						i += jw
						hangulState = hangulClass(jr)
					}
				}
				continue
			}
			if riPair && isRegionalIndicator(nr) {
				i += nw
				riPair = false
				continue
			}
			if nc := hangulClass(nr); nc != 0 && canJoinHangul(hangulState, nc) {
				i += nw
				hangulState = nc
				continue
			}
			if isExtendRune(nr) {
				i += nw
				continue
			}
			break
		}
		out = append(out, s[start:i])
	}
	return out
}

func isRegionalIndicator(r rune) bool {
	return r >= 0x1F1E6 && r <= 0x1F1FF
}

func isExtendRune(r rune) bool {
	return unicode.In(r, unicode.Mn, unicode.Mc, unicode.Me) ||
		(r >= 0xFE00 && r <= 0xFE0F) ||
		(r >= 0xE0100 && r <= 0xE01EF) ||
		(r >= 0x1F3FB && r <= 0x1F3FF) ||
		r == 0x200C
}

// hangulClass: 1=L, 2=V, 3=T, 4=LV, 5=LVT, 0=none.
func hangulClass(r rune) int {
	switch {
	case (r >= 0x1100 && r <= 0x115F) || (r >= 0xA960 && r <= 0xA97C):
		return 1
	case (r >= 0x1160 && r <= 0x11A7) || (r >= 0xD7B0 && r <= 0xD7C6):
		return 2
	case (r >= 0x11A8 && r <= 0x11FF) || (r >= 0xD7CB && r <= 0xD7FB):
		return 3
	case r >= 0xAC00 && r <= 0xD7A3:
		if (r-0xAC00)%28 == 0 {
			return 4
		}
		return 5
	}
	return 0
}

func canJoinHangul(prev, next int) bool {
	switch prev {
	case 1: // L -> L, V, LV, LVT
		return next == 1 || next == 2 || next == 4 || next == 5
	case 2, 4: // V or LV -> V, T
		return next == 2 || next == 3
	case 3, 5: // T or LVT -> T
		return next == 3
	}
	return false
}
