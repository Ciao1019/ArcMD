package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/agent"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
)

func parseTestRuns(md string) []qaRun {
	doc := parser.New(parser.GFM)
	doc.Set(md)
	var runs []qaRun
	for i := range doc.Children(0) {
		if b := doc.Block(i); b.Kind == parser.Paragraph || b.Kind == parser.Heading {
			r, _ := qaMarkdownRuns(doc.Inlines(i))
			runs = append(runs, r...)
		}
	}
	return runs
}

func TestQACitationMergeAndClaimSegmentation(t *testing.T) {
	// Adjacent and comma-separated citations merge into a single chip, and only
	// the sentence/clause directly preceding a chip is tagged with claimID.
	runs := parseTestRuns("Keep it fast. Most motion finishes under 300ms [A.md#L1], [B.md#L2]. Next sentence [C.md#L3][C.md#L3]!")
	var chips []qaRun
	var claimTexts []string
	for _, r := range runs {
		if len(r.cites) > 0 {
			chips = append(chips, r)
		} else if r.claimID > 0 {
			claimTexts = append(claimTexts, r.text)
		}
	}
	if len(chips) != 2 {
		t.Fatalf("expected 2 merged chips, got %d (%+v)", len(chips), chips)
	}
	if len(chips[0].cites) != 2 || chips[0].cites[0].Path != "A.md" || chips[0].cites[1].Path != "B.md" {
		t.Fatalf("first chip did not merge A and B: %+v", chips[0].cites)
	}
	if len(chips[1].cites) != 1 || chips[1].cites[0].Path != "C.md" {
		t.Fatalf("second chip did not deduplicate C: %+v", chips[1].cites)
	}
	joined := strings.Join(claimTexts, "|")
	if strings.Contains(joined, "Keep it fast") {
		t.Fatalf("prior sentence leaked into claim highlights: %q", joined)
	}
	if !strings.Contains(joined, "Most motion finishes under 300ms") || !strings.Contains(joined, "Next sentence") {
		t.Fatalf("supporting claims missing: %q", joined)
	}
	// Leading punctuation after chip 1 must not be part of chip 2's claim.
	for _, r := range runs {
		if r.claimID == 2 && strings.HasPrefix(r.text, ".") {
			t.Fatalf("leading period after previous chip should not highlight: %q", r.text)
		}
	}

	// Turn-wide partID numbering remains unique across multiple blocks even when
	// streaming appends new blocks and earlier blocks reuse cached runs.
	var view qaTurnView
	view.parse("第一段 [A.md#L1]\n\n", true)
	view.parse("第一段 [A.md#L1]\n\n第二段 [B.md#L2] 和补充 [C.md#L3]", false)
	var seenIDs []int
	for _, b := range view.blocks {
		for _, r := range b.runs {
			if r.partID > 0 {
				seenIDs = append(seenIDs, r.partID)
			}
		}
	}
	if len(seenIDs) != 3 || seenIDs[0] != 1 || seenIDs[1] != 2 || seenIDs[2] != 3 {
		t.Fatalf("unexpected turn-wide partIDs: %v", seenIDs)
	}
}

func TestQACitationNoOverlapGeometry(t *testing.T) {
	a, tt := newTestApp(t)
	os.WriteFile(filepath.Join(root(a), "Alpha.md"), []byte("# Alpha\n\nAlpha line.\n"), 0o644)
	os.WriteFile(filepath.Join(root(a), "Beta.md"), []byte("# Beta\n\nBeta line.\n"), 0o644)
	os.WriteFile(filepath.Join(root(a), "Gamma.md"), []byte("# Gamma\n\nGamma line.\n"), 0o644)

	s := a.qaSession(a.state.ActiveSpace().ID)
	turn := &qaTurn{
		question:    "Check non-overlapping geometry",
		expanded:    true,
		userToggled: true,
		steps: []*qaStep{
			{
				call:   agent.ToolCall{ID: "1", Name: agent.ToolSearch, Args: `{"query":"非常非常长的检索词用来测试徽章不重叠","path":"项目/超长子目录名称/归档/2026"}`},
				done:   true,
				result: "2 relevant passages in 2 documents.\n[1] Alpha.md (Alpha, L3)\n     3→Alpha line.\n",
			},
		},
		answer: "结论一 [Alpha.md#L3]，结论二继续说明并且自动换行到下一行 [Alpha.md#L3], [Beta.md#L3]，以及结论三 [Gamma.md#L3]。",
	}
	s.turns = []*qaTurn{turn}
	a.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	tt.Frame()

	// 1. Check step row scope badge vs result pill ("召回 2 段") do not overlap.
	scopeRect, ok1 := tt.Find("项目 / 超长子目录名称 / 归档 / 2026")
	resRect, ok2 := tt.Find("召回 2 段")
	if !ok1 || !ok2 {
		t.Fatalf("missing step badges: scope=%v res=%v texts=%q", ok1, ok2, tt.Texts())
	}
	if scopeRect.X+scopeRect.W > resRect.X+0.5 {
		t.Fatalf("step scope badge %+v overlaps result badge %+v", scopeRect, resRect)
	}

	// 2. Check all three inline citation chips have non-overlapping bounds.
	c1, okA := tt.Find("来源：Alpha")
	c2, okB := tt.Find("来源：Alpha, Beta")
	c3, okC := tt.Find("来源：Gamma")
	if !okA || !okB || !okC {
		t.Fatalf("missing inline chips: okA=%v okB=%v okC=%v texts=%q", okA, okB, okC, tt.Texts())
	}
	rects := []ui.Rect{c1, c2, c3}
	for i := range rects {
		for j := i + 1; j < len(rects); j++ {
			aR, bR := rects[i], rects[j]
			horizOverlap := aR.X < bR.X+bR.W && bR.X < aR.X+aR.W
			vertOverlap := aR.Y < bR.Y+bR.H && bR.Y < aR.Y+aR.H
			if horizOverlap && vertOverlap {
				t.Fatalf("inline citation chips %d (%+v) and %d (%+v) overlap!", i, aR, j, bR)
			}
		}
	}
}

func TestQACitationPreviewTimingGlideAndDismiss(t *testing.T) {
	a, tt := newTestApp(t)
	os.WriteFile(filepath.Join(root(a), "Alpha.md"), []byte("# Alpha\n\n## First Section\n\nFirst line content.\n"), 0o644)
	os.WriteFile(filepath.Join(root(a), "Beta.md"), []byte("# Beta\n\n## Second Section\n\nSecond line content.\n"), 0o644)

	s := a.qaSession(a.state.ActiveSpace().ID)
	turn := &qaTurn{
		question: "Preview test",
		answer:   "First claim [Alpha.md#L5].\n\nSecond claim [Beta.md#L5].",
	}
	s.turns = []*qaTurn{turn}
	a.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	tt.Frame()

	c1, ok1 := tt.Find("来源：Alpha")
	c2, ok2 := tt.Find("来源：Beta")
	if !ok1 || !ok2 {
		t.Fatalf("chips missing: %v %v", ok1, ok2)
	}

	// Cold hover: not open at 80ms, open after 120ms delay.
	tt.Move(c1.X+c1.W/2, c1.Y+c1.H/2)
	tt.Advance(80 * time.Millisecond)
	if s.preview.open || tt.HasText("First Section") {
		t.Fatal("preview opened before 120ms cold hover delay")
	}
	tt.Advance(60 * time.Millisecond)
	if !s.preview.open || s.preview.part != 1 || !tt.HasText("First Section") {
		t.Fatalf("preview did not open after 120ms: %+v texts=%q", s.preview, tt.Texts())
	}
	if !s.isChipLit(turn, 1, []int{1}) || !s.isClaimLit(turn, 1) {
		t.Fatal("stationary hover open did not light inline chip and supporting claim")
	}

	// Moving directly to chip 2 while open glides smoothly (warm = true) over 220ms.
	startY := s.preview.curY
	tt.Move(c2.X+c2.W/2, c2.Y+c2.H/2)
	tt.Frame()
	if !s.preview.open || !s.preview.warm || s.preview.part != 2 || !tt.HasText("Second Section") {
		t.Fatalf("preview did not switch cleanly to chip 2: %+v", s.preview)
	}
	if s.preview.targetY != startY {
		tt.Advance(80 * time.Millisecond)
		midY := s.preview.curY
		if midY == startY || midY == s.preview.targetY {
			t.Fatalf("expected mid-glide Y between %v and %v, got %v", startY, s.preview.targetY, midY)
		}
		tt.Advance(180 * time.Millisecond)
		if s.preview.curY != s.preview.targetY {
			t.Fatalf("glide did not settle at %v, got %v", s.preview.targetY, s.preview.curY)
		}
	}

	// Moving off chip starts 90ms close delay.
	tt.Move(10, 10)
	tt.Advance(50 * time.Millisecond)
	if !s.preview.open {
		t.Fatal("preview closed before 90ms close delay")
	}
	tt.Advance(60 * time.Millisecond)
	if s.preview.open {
		t.Fatal("preview did not close after 90ms delay")
	}
	if s.isChipLit(turn, 1, []int{1}) {
		t.Fatal("chip remained lit after preview closed")
	}

	// Within 400ms warm window after closing, hovering chip 1 opens immediately!
	tt.Move(c1.X+c1.W/2, c1.Y+c1.H/2)
	tt.Frame()
	if !s.preview.open || s.preview.part != 1 {
		t.Fatalf("warm re-open did not open immediately within 400ms: %+v", s.preview)
	}

	// Escape dismisses preview immediately.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if s.preview.open {
		t.Fatal("Escape did not dismiss preview")
	}

	// Clicking chip opens the cited document and focuses the cited block.
	tt.ClickAt(c2.X+c2.W/2, c2.Y+c2.H/2)
	tt.Frame()
	tt.Advance(slide)
	if a.doc == nil || a.doc.rel != "Beta.md" {
		t.Fatalf("clicking chip did not open Beta.md: %+v", a.doc)
	}
	if got := a.doc.ed.Doc.Blocks[a.doc.ed.CaretBlock()].Text(); got != "Second line content." {
		t.Fatalf("caret landed in %q", got)
	}
}

func TestQASourcesCollapsibleAndRowHighlight(t *testing.T) {
	a, tt := newTestApp(t)
	os.WriteFile(filepath.Join(root(a), "One.md"), []byte("# One\n\n## Alpha\n\nContent one.\n"), 0o644)
	os.WriteFile(filepath.Join(root(a), "Two.md"), []byte("# Two\n\n## Beta\n\nContent two.\n"), 0o644)

	s := a.qaSession(a.state.ActiveSpace().ID)
	turn := &qaTurn{
		question: "Sources list test",
		answer:   "Claim one [One.md#L5] and claim two [Two.md#L5].",
	}
	s.turns = []*qaTurn{turn}
	a.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	tt.Frame()

	// Collapsed by default.
	if !tt.HasText("2 个来源") || tt.HasText("One · Alpha") {
		t.Fatalf("sources list should start collapsed with toggle button: %q", tt.Texts())
	}

	// Expand sources list.
	if err := tt.Click("2 个来源"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Advance(240 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("One · Alpha") || !tt.HasText("Two · Beta") {
		t.Fatalf("expanded sources list missing items: %q", tt.Texts())
	}

	// Hovering source row 2 highlights linked chip and claim in the answer.
	row2, ok := tt.Find("2 Two · Beta 第 5 行")
	if !ok {
		t.Fatalf("source row 2 missing: %q", tt.Texts())
	}
	tt.Move(row2.X+row2.W/2, row2.Y+row2.H/2)
	tt.Advance(24 * time.Millisecond)
	if turn.hoverSource != 2 || !s.isChipLit(turn, 2, []int{2}) || !s.isClaimLit(turn, 2) {
		t.Fatalf("hovering source row 2 did not light up chip/claim: hoverSource=%d", turn.hoverSource)
	}
	if s.isChipLit(turn, 1, []int{1}) || s.isClaimLit(turn, 1) {
		t.Fatal("hovering source row 2 lit unrelated chip/claim 1")
	}

	// Clicking toggle collapses list again.
	if err := tt.Click("2 个来源"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Advance(240 * time.Millisecond)
	tt.Frame()
	if tt.HasText("One · Alpha") {
		t.Fatal("sources list did not collapse on second click")
	}

	// English localization check.
	i18n.SetLanguage("en")
	t.Cleanup(func() { i18n.SetLanguage("zh-Hans") })
	tt.Frame()
	if !tt.HasText("2 sources") {
		t.Fatalf("missing English sources toggle label: %q", tt.Texts())
	}
}

func TestQACitationPreviewDoesNotOccludeAnswerText(t *testing.T) {
	a, tt := newTestApp(t)
	os.WriteFile(filepath.Join(root(a), "Welcome.md"), []byte("# Welcome to ArcMD\n\nArcMD is a calm place for writing, in the spirit of Arc and Zen.\n"), 0o644)

	s := a.qaSession(a.state.ActiveSpace().ID)
	s.turns = []*qaTurn{{
		question: "介绍一下 ArcMD",
		answer:   "你的空间里有两条关于 ArcMD 的描述。\n\n- 一个安静、克制的开源桌面文档编辑器，交互借鉴 Arc 与 Zen，文档以结构化的 `.arcmd` 文件保存，支持 Markdown 导入导出 [Welcome.md#L3]。\n\n- 起因：作者用纯 Go 原生把审美与手感移植成文档编辑器。",
	}}
	a.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	tt.Frame()

	// 1. Standard 1280x800 window: card docks outside the left edge of the Q&A card.
	chip, ok := tt.Find("来源：Welcome to ArcMD")
	if !ok {
		t.Fatalf("citation chip missing: %q", tt.Texts())
	}
	tt.Move(chip.X+chip.W/2, chip.Y+chip.H/2)
	tt.Advance(140 * time.Millisecond)
	if !s.preview.open || s.preview.place != qaPlaceLeft {
		t.Fatalf("expected side placement qaPlaceLeft, got open=%v place=%v", s.preview.open, s.preview.place)
	}
	if s.preview.renderedX+s.preview.cardW > s.preview.panelRect.X {
		t.Fatalf("preview card (%v + %v) overlaps Q&A panel starting at %v", s.preview.renderedX, s.preview.cardW, s.preview.panelRect.X)
	}

	// Dismiss before resizing to ultra-narrow.
	tt.Move(10, 10)
	tt.Advance(110 * time.Millisecond)

	// 2. Ultra-narrow 560x600 window: falls back below the chip so the preceding claim stays unobstructed.
	tt.SetSize(560, 600)
	tt.Frame()
	chip, ok = tt.Find("来源：Welcome to ArcMD")
	if !ok {
		t.Fatalf("citation chip missing in narrow window: %q", tt.Texts())
	}
	tt.Move(chip.X+chip.W/2, chip.Y+chip.H/2)
	tt.Advance(140 * time.Millisecond)
	if !s.preview.open || s.preview.place != qaPlaceBelow {
		t.Fatalf("expected narrow fallback qaPlaceBelow, got open=%v place=%v", s.preview.open, s.preview.place)
	}
	if s.preview.renderedY < chip.Y+chip.H {
		t.Fatalf("narrow preview (%v) overlaps chip (%+v) or preceding claim", s.preview.renderedY, chip)
	}
}
