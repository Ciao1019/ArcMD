package merge

import (
	"slices"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/history"
)

// ChunkKind classifies a 3-way merge region across Left (Yours), Base, and Right (Theirs).
type ChunkKind uint8

const (
	ChunkUnchanged ChunkKind = iota
	ChunkLeftOnly            // Only Left changed relative to Base
	ChunkRightOnly           // Only Right changed relative to Base
	ChunkBothSame            // Both Left and Right made the exact same change
	ChunkConflict            // Both Left and Right changed Base in different ways
)

// ChangeTag refines how a side changed relative to the Center result for IDEA color coding.
type ChangeTag uint8

const (
	TagNone ChangeTag = iota
	TagInsert
	TagDelete
	TagModify
	TagConflict
)

// Chunk is one aligned region across Left (Yours), Center (Result), and Right (Theirs).
type Chunk struct {
	Index int
	Kind  ChunkKind

	BaseLines   []string
	LeftLines   []string
	RightLines  []string
	ResultLines []string

	// Resolution flags for interactive IDEA gutter actions.
	LeftAccepted  bool
	LeftIgnored   bool
	RightAccepted bool
	RightIgnored  bool

	// Computed 0-based line ranges in each of the 3 panes (updated on rebuild).
	LeftStart, LeftEnd     int
	CenterStart, CenterEnd int
	RightStart, RightEnd   int

	// Intra-line grapheme spans comparing Left vs Center and Right vs Center.
	LeftSpans  [][]history.Span
	RightSpans [][]history.Span
}

// Resolved reports whether this chunk requires no further user conflict resolution.
func (ch *Chunk) Resolved() bool {
	if ch.Kind == ChunkUnchanged {
		return true
	}
	if ch.Kind != ChunkConflict {
		return (ch.LeftAccepted || ch.LeftIgnored || ch.Kind == ChunkRightOnly) &&
			(ch.RightAccepted || ch.RightIgnored || ch.Kind == ChunkLeftOnly)
	}
	// A conflict is resolved once the user explicitly accepts at least one side
	// or dismisses both sides.
	return ch.LeftAccepted || ch.RightAccepted || (ch.LeftIgnored && ch.RightIgnored)
}

// LeftActionable reports whether the Left gutter should show » / × buttons.
func (ch *Chunk) LeftActionable() bool {
	switch ch.Kind {
	case ChunkLeftOnly, ChunkBothSame:
		return !ch.LeftAccepted && !ch.LeftIgnored
	case ChunkConflict:
		return !ch.LeftAccepted && !ch.LeftIgnored
	default:
		return false
	}
}

// RightActionable reports whether the Right gutter should show « / × buttons.
func (ch *Chunk) RightActionable() bool {
	switch ch.Kind {
	case ChunkRightOnly:
		return !ch.RightAccepted && !ch.RightIgnored
	case ChunkBothSame:
		return !ch.LeftAccepted && !ch.RightAccepted && !ch.RightIgnored
	case ChunkConflict:
		return !ch.RightAccepted && !ch.RightIgnored
	default:
		return false
	}
}

// VisualTag returns IDEA's semantic color category for this chunk.
func (ch *Chunk) VisualTag() ChangeTag {
	if ch.Kind == ChunkUnchanged {
		return TagNone
	}
	if ch.Kind == ChunkConflict && !ch.Resolved() {
		return TagConflict
	}
	switch {
	case len(ch.BaseLines) == 0 && len(ch.ResultLines) > 0:
		return TagInsert
	case len(ch.BaseLines) > 0 && len(ch.ResultLines) == 0:
		return TagDelete
	default:
		return TagModify
	}
}

// Session holds the live state of a 3-way merge between Left (Yours), Base, and Right (Theirs).
type Session struct {
	BaseLines   []string
	LeftLines   []string
	RightLines  []string
	ResultLines []string

	Chunks          []Chunk
	TrailingNewline bool
}

// NewSession constructs a 3-way merge session from base, local (Left/Yours),
// and remote (Right/Theirs). Non-conflicting changes start unapplied until
// ApplyNonConflicting is called, or callers can invoke ApplyNonConflicting()
// immediately for IDEA's one-step conflict focus.
func NewSession(baseText, localText, remoteText string) *Session {
	trailingNL := strings.HasSuffix(localText, "\n") || strings.HasSuffix(remoteText, "\n") || strings.HasSuffix(baseText, "\n")
	base := SplitLines(baseText)
	local := SplitLines(localText)
	remote := SplitLines(remoteText)

	// When no prior baseline exists (e.g. two devices independently created the
	// same filename before their first sync), synthesize common LCS lines of
	// local and remote as the anchor spine so unchanged paragraphs are not
	// painted as one monolithic conflict block.
	if len(base) == 0 && len(local) > 0 && len(remote) > 0 {
		base = lcsLines(local, remote)
	}

	b2l := matchAnchors(base, local)
	b2r := matchAnchors(base, remote)

	// Common anchors are base indices matched monotonically in BOTH local and remote.
	type anchor struct {
		b, l, r int
	}
	var anchors []anchor
	for bi := 0; bi < len(base); bi++ {
		if b2l[bi] >= 0 && b2r[bi] >= 0 {
			anchors = append(anchors, anchor{b: bi, l: b2l[bi], r: b2r[bi]})
		}
	}

	var chunks []Chunk
	pb, pl, pr := 0, 0, 0

	emitChange := func(bEnd, lEnd, rEnd int) {
		bSlice := slices.Clone(base[pb:bEnd])
		lSlice := slices.Clone(local[pl:lEnd])
		rSlice := slices.Clone(remote[pr:rEnd])
		if len(bSlice) == 0 && len(lSlice) == 0 && len(rSlice) == 0 {
			return
		}
		lEqB := slices.Equal(lSlice, bSlice)
		rEqB := slices.Equal(rSlice, bSlice)
		lEqR := slices.Equal(lSlice, rSlice)

		var kind ChunkKind
		switch {
		case lEqB && rEqB:
			kind = ChunkUnchanged
		case !lEqB && rEqB:
			kind = ChunkLeftOnly
		case lEqB && !rEqB:
			kind = ChunkRightOnly
		case lEqR:
			kind = ChunkBothSame
		default:
			kind = ChunkConflict
		}

		initRes := slices.Clone(bSlice)
		chunks = append(chunks, Chunk{
			Index:       len(chunks),
			Kind:        kind,
			BaseLines:   bSlice,
			LeftLines:   lSlice,
			RightLines:  rSlice,
			ResultLines: initRes,
		})
	}

	i := 0
	for i < len(anchors) {
		a := anchors[i]
		if a.b > pb || a.l > pl || a.r > pr {
			emitChange(a.b, a.l, a.r)
		}
		// Coalesce contiguous unchanged anchors into one ChunkUnchanged run.
		j := i + 1
		for j < len(anchors) &&
			anchors[j].b == anchors[j-1].b+1 &&
			anchors[j].l == anchors[j-1].l+1 &&
			anchors[j].r == anchors[j-1].r+1 {
			j++
		}
		runLen := j - i
		sameSlice := slices.Clone(base[a.b : a.b+runLen])
		chunks = append(chunks, Chunk{
			Index:       len(chunks),
			Kind:        ChunkUnchanged,
			BaseLines:   sameSlice,
			LeftLines:   slices.Clone(sameSlice),
			RightLines:  slices.Clone(sameSlice),
			ResultLines: slices.Clone(sameSlice),
		})
		pb = a.b + runLen
		pl = a.l + runLen
		pr = a.r + runLen
		i = j
	}
	if pb < len(base) || pl < len(local) || pr < len(remote) {
		emitChange(len(base), len(local), len(remote))
	}

	s := &Session{
		BaseLines:       base,
		LeftLines:       local,
		RightLines:      remote,
		Chunks:          chunks,
		TrailingNewline: trailingNL,
	}
	s.rebuildGeometry()
	return s
}

// ApplyNonConflicting applies all unhandled LeftOnly, RightOnly, and BothSame
// chunks into ResultLines, leaving true Conflicts for user choice. Returns the
// number of chunks applied.
func (s *Session) ApplyNonConflicting() int {
	applied := 0
	for i := range s.Chunks {
		ch := &s.Chunks[i]
		switch ch.Kind {
		case ChunkLeftOnly:
			if !ch.LeftAccepted && !ch.LeftIgnored {
				ch.ResultLines = slices.Clone(ch.LeftLines)
				ch.LeftAccepted = true
				applied++
			}
		case ChunkRightOnly:
			if !ch.RightAccepted && !ch.RightIgnored {
				ch.ResultLines = slices.Clone(ch.RightLines)
				ch.RightAccepted = true
				applied++
			}
		case ChunkBothSame:
			if !ch.LeftAccepted && !ch.RightAccepted && !ch.LeftIgnored && !ch.RightIgnored {
				ch.ResultLines = slices.Clone(ch.LeftLines)
				ch.LeftAccepted = true
				ch.RightAccepted = true
				applied++
			}
		}
	}
	if applied > 0 {
		s.rebuildGeometry()
	}
	return applied
}

// AcceptLeft applies LeftLines to chunk idx. On a Conflict chunk where Right
// was already accepted, LeftLines are prepended before RightLines so accepting
// both sides preserves both edits.
func (s *Session) AcceptLeft(idx int) {
	if idx < 0 || idx >= len(s.Chunks) {
		return
	}
	ch := &s.Chunks[idx]
	if ch.Kind == ChunkUnchanged || ch.LeftAccepted {
		return
	}
	if ch.Kind == ChunkConflict && ch.RightAccepted {
		combined := make([]string, 0, len(ch.LeftLines)+len(ch.RightLines))
		combined = append(combined, ch.LeftLines...)
		combined = append(combined, ch.RightLines...)
		ch.ResultLines = combined
	} else {
		ch.ResultLines = slices.Clone(ch.LeftLines)
	}
	ch.LeftAccepted = true
	ch.LeftIgnored = false
	if ch.Kind == ChunkBothSame {
		ch.RightAccepted = true
	}
	s.rebuildGeometry()
}

// AcceptRight applies RightLines to chunk idx. On a Conflict chunk where Left
// was already accepted, RightLines are appended after LeftLines so accepting
// both sides keeps both contributions.
func (s *Session) AcceptRight(idx int) {
	if idx < 0 || idx >= len(s.Chunks) {
		return
	}
	ch := &s.Chunks[idx]
	if ch.Kind == ChunkUnchanged || ch.RightAccepted {
		return
	}
	if ch.Kind == ChunkConflict && ch.LeftAccepted {
		combined := make([]string, 0, len(ch.ResultLines)+len(ch.RightLines))
		combined = append(combined, ch.ResultLines...)
		combined = append(combined, ch.RightLines...)
		ch.ResultLines = combined
	} else {
		ch.ResultLines = slices.Clone(ch.RightLines)
	}
	ch.RightAccepted = true
	ch.RightIgnored = false
	if ch.Kind == ChunkBothSame {
		ch.LeftAccepted = true
	}
	s.rebuildGeometry()
}

// IgnoreLeft dismisses Left's change on chunk idx without modifying ResultLines.
func (s *Session) IgnoreLeft(idx int) {
	if idx < 0 || idx >= len(s.Chunks) {
		return
	}
	ch := &s.Chunks[idx]
	if ch.Kind == ChunkUnchanged {
		return
	}
	ch.LeftIgnored = true
	s.rebuildGeometry()
}

// IgnoreRight dismisses Right's change on chunk idx without modifying ResultLines.
func (s *Session) IgnoreRight(idx int) {
	if idx < 0 || idx >= len(s.Chunks) {
		return
	}
	ch := &s.Chunks[idx]
	if ch.Kind == ChunkUnchanged {
		return
	}
	ch.RightIgnored = true
	s.rebuildGeometry()
}

// ResetChunk restores chunk idx back to its initial unresolved Base state.
func (s *Session) ResetChunk(idx int) {
	if idx < 0 || idx >= len(s.Chunks) {
		return
	}
	ch := &s.Chunks[idx]
	if ch.Kind == ChunkUnchanged {
		return
	}
	ch.ResultLines = slices.Clone(ch.BaseLines)
	ch.LeftAccepted, ch.LeftIgnored = false, false
	ch.RightAccepted, ch.RightIgnored = false, false
	s.rebuildGeometry()
}

// AcceptAllLeft resolves every unresolved chunk using Left (Yours).
func (s *Session) AcceptAllLeft() {
	for i := range s.Chunks {
		ch := &s.Chunks[i]
		if ch.Kind == ChunkUnchanged {
			continue
		}
		switch ch.Kind {
		case ChunkLeftOnly, ChunkBothSame, ChunkConflict:
			if !ch.Resolved() {
				ch.ResultLines = slices.Clone(ch.LeftLines)
				ch.LeftAccepted = true
				ch.RightIgnored = true
			}
		case ChunkRightOnly:
			if !ch.RightAccepted && !ch.RightIgnored {
				ch.ResultLines = slices.Clone(ch.RightLines)
				ch.RightAccepted = true
			}
		}
	}
	s.rebuildGeometry()
}

// AcceptAllRight resolves every unresolved chunk using Right (Theirs).
func (s *Session) AcceptAllRight() {
	for i := range s.Chunks {
		ch := &s.Chunks[i]
		if ch.Kind == ChunkUnchanged {
			continue
		}
		switch ch.Kind {
		case ChunkRightOnly, ChunkBothSame, ChunkConflict:
			if !ch.Resolved() {
				ch.ResultLines = slices.Clone(ch.RightLines)
				ch.RightAccepted = true
				ch.LeftIgnored = true
			}
		case ChunkLeftOnly:
			if !ch.LeftAccepted && !ch.LeftIgnored {
				ch.ResultLines = slices.Clone(ch.LeftLines)
				ch.LeftAccepted = true
			}
		}
	}
	s.rebuildGeometry()
}

// UnresolvedConflicts returns the number of ChunkConflict regions not yet resolved.
func (s *Session) UnresolvedConflicts() int {
	n := 0
	for i := range s.Chunks {
		if s.Chunks[i].Kind == ChunkConflict && !s.Chunks[i].Resolved() {
			n++
		}
	}
	return n
}

// PendingChanges returns the number of non-conflicting change chunks not yet handled.
func (s *Session) PendingChanges() int {
	n := 0
	for i := range s.Chunks {
		ch := &s.Chunks[i]
		if ch.Kind != ChunkUnchanged && ch.Kind != ChunkConflict && !ch.Resolved() {
			n++
		}
	}
	return n
}

// ResultText returns the full merged document text.
func (s *Session) ResultText() string {
	return JoinLines(s.ResultLines, s.TrailingNewline)
}

func (s *Session) rebuildGeometry() {
	lPos, cPos, rPos := 0, 0, 0
	var res []string
	for i := range s.Chunks {
		ch := &s.Chunks[i]
		ch.LeftStart = lPos
		lPos += len(ch.LeftLines)
		ch.LeftEnd = lPos

		ch.CenterStart = cPos
		cPos += len(ch.ResultLines)
		ch.CenterEnd = cPos
		res = append(res, ch.ResultLines...)

		ch.RightStart = rPos
		rPos += len(ch.RightLines)
		ch.RightEnd = rPos

		if ch.Kind != ChunkUnchanged {
			ch.LeftSpans = sideSpans(ch.ResultLines, ch.LeftLines)
			ch.RightSpans = sideSpans(ch.ResultLines, ch.RightLines)
		} else {
			ch.LeftSpans = nil
			ch.RightSpans = nil
		}
	}
	s.ResultLines = res
}

func sideSpans(center, side []string) [][]history.Span {
	if len(side) == 0 {
		return nil
	}
	out := make([][]history.Span, len(side))
	if len(center) == 0 {
		return out
	}
	diff := history.Diff(center, side)
	addIdx := 0
	for _, dl := range diff {
		if dl.Op == history.Added {
			if addIdx < len(out) {
				out[addIdx] = dl.Spans
				addIdx++
			}
		} else if dl.Op == history.Same {
			if addIdx < len(out) {
				addIdx++
			}
		}
	}
	return out
}

// matchAnchors returns a mapping from each index in a to its matched LCS index
// in b, or -1 if unmatched.
func matchAnchors(a, b []string) []int {
	m := make([]int, len(a))
	for i := range m {
		m[i] = -1
	}
	if len(a) == 0 || len(b) == 0 {
		return m
	}

	head := 0
	for head < len(a) && head < len(b) && a[head] == b[head] {
		m[head] = head
		head++
	}
	tail := 0
	for tail < len(a)-head && tail < len(b)-head && a[len(a)-1-tail] == b[len(b)-1-tail] {
		m[len(a)-1-tail] = len(b) - 1 - tail
		tail++
	}

	ma := a[head : len(a)-tail]
	mb := b[head : len(b)-tail]
	if len(ma) == 0 || len(mb) == 0 || len(ma)*len(mb) > 4_000_000 {
		return m
	}

	dp := make([][]int32, len(ma)+1)
	for i := range dp {
		dp[i] = make([]int32, len(mb)+1)
	}
	for i := len(ma) - 1; i >= 0; i-- {
		for j := len(mb) - 1; j >= 0; j-- {
			if ma[i] == mb[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	i, j := 0, 0
	for i < len(ma) && j < len(mb) {
		switch {
		case ma[i] == mb[j]:
			m[head+i] = head + j
			i++
			j++
		case dp[i][j+1] >= dp[i+1][j]:
			j++
		default:
			i++
		}
	}
	return m
}

func lcsLines(a, b []string) []string {
	m := matchAnchors(a, b)
	var out []string
	for i, j := range m {
		if j >= 0 {
			out = append(out, a[i])
		}
	}
	return out
}
