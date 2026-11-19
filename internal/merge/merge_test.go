package merge

import (
	"strings"
	"testing"
)

func TestUnifiedDiffHunksAndSkipBlocks(t *testing.T) {
	var oldB, newB []string
	for i := 1; i <= 20; i++ {
		line := "line " + string(rune('A'+i-1))
		oldB = append(oldB, line)
		if i == 10 {
			newB = append(newB, "line J modified")
		} else {
			newB = append(newB, line)
		}
	}
	fd := UnifiedDiff(strings.Join(oldB, "\n")+"\n", strings.Join(newB, "\n")+"\n", 2)
	if fd.Added != 1 || fd.Removed != 1 {
		t.Fatalf("got +%d -%d, want +1 -1", fd.Added, fd.Removed)
	}
	if len(fd.Blocks) != 3 || !fd.Blocks[0].IsSkip || fd.Blocks[1].IsSkip || !fd.Blocks[2].IsSkip {
		t.Fatalf("expected [Skip, Hunk, Skip], got %+v", fd.Blocks)
	}
	if fd.Blocks[0].HiddenCount != 7 || fd.Blocks[2].HiddenCount != 8 {
		t.Fatalf("unexpected skip counts: %d and %d", fd.Blocks[0].HiddenCount, fd.Blocks[2].HiddenCount)
	}
	hasSpans := false
	for _, l := range fd.Blocks[1].Lines {
		if len(l.Spans) > 0 {
			hasSpans = true
		}
	}
	if !hasSpans {
		t.Fatal("expected intra-line grapheme spans on modified line pair")
	}
}

func TestThreeWayAutoMergeAndConflictResolution(t *testing.T) {
	base := "# 标题\n\n第一段原文\n\n中间公共段\n\n最后一段原文\n"
	local := "# 标题（本地微调）\n\n第一段本地修改\n\n中间公共段\n\n最后一段原文\n"
	remote := "# 标题\n\n第一段云端修改\n\n中间公共段\n\n最后一段云端补充\n"

	s := NewSession(base, local, remote)
	if s.UnresolvedConflicts() != 1 {
		t.Fatalf("expected 1 conflict, got %d (chunks=%+v)", s.UnresolvedConflicts(), s.Chunks)
	}
	if s.PendingChanges() != 2 {
		t.Fatalf("expected 2 non-conflicting changes, got %d", s.PendingChanges())
	}

	applied := s.ApplyNonConflicting()
	if applied != 2 || s.PendingChanges() != 0 || s.UnresolvedConflicts() != 1 {
		t.Fatalf("after ApplyNonConflicting: applied=%d pending=%d conflicts=%d", applied, s.PendingChanges(), s.UnresolvedConflicts())
	}

	// Locate the conflict chunk and accept Left then append Right (keep both).
	conflictIdx := -1
	for i := range s.Chunks {
		if s.Chunks[i].Kind == ChunkConflict {
			conflictIdx = i
			break
		}
	}
	if conflictIdx < 0 {
		t.Fatal("missing conflict chunk")
	}
	s.AcceptLeft(conflictIdx)
	if !s.Chunks[conflictIdx].Resolved() || s.UnresolvedConflicts() != 0 {
		t.Fatal("expected conflict to resolve after AcceptLeft")
	}
	// Clicking AcceptRight after AcceptLeft appends Right's lines (IDEA keep-both).
	s.AcceptRight(conflictIdx)

	res := s.ResultText()
	for _, want := range []string{
		"# 标题（本地微调）",
		"第一段本地修改",
		"第一段云端修改",
		"中间公共段",
		"最后一段云端补充",
	} {
		if !strings.Contains(res, want) {
			t.Fatalf("merged result missing %q:\n%s", want, res)
		}
	}
}

func TestThreeWayBaselessSyntheticAnchors(t *testing.T) {
	local := "# Project\n\nShared intro\n\nLocal bullet\n\nShared footer\n"
	remote := "# Project\n\nShared intro\n\nRemote bullet\n\nShared footer\n"

	s := NewSession("", local, remote)
	if s.UnresolvedConflicts() != 1 {
		t.Fatalf("expected 1 pinpoint conflict without base, got %d", s.UnresolvedConflicts())
	}
	s.AcceptAllRight()
	if s.UnresolvedConflicts() != 0 || s.ResultText() != remote {
		t.Fatalf("AcceptAllRight mismatch:\n%s", s.ResultText())
	}
}
