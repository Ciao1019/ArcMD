package editor

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

func TestSourceHighlightRangesBoundsAndTokens(t *testing.T) {
	src := strings.Join([]string{
		"---",
		"title: 源码模式测试",
		"tags: [go, markdown]",
		"---",
		"",
		"# 标题 Heading",
		"",
		"> [!NOTE] 提示块",
		"> 引用内容 **加粗** 与 `code` 和 $x^2 + y^2$",
		"",
		"- [x] 任务项 [[双链页面]] #标签/子标签",
		"1. 有序列表 [链接](https://example.com)",
		"",
		"<!-- 多行注释开始",
		"注释第二行 --> 后续 <kbd>⌘S</kbd>",
		"",
		"```go",
		"func main() { return }",
		"```",
		"",
		"$$",
		"\\frac{a}{b}",
		"$$",
		"---",
	}, "\n")

	pg := pageOf(Page{}, ui.LightTheme())
	pg.Tokens = CodeTokens{
		Comment:  ui.Hex("#6A737D"),
		Keyword:  ui.Hex("#D73A49"),
		String:   ui.Hex("#032F62"),
		Number:   ui.Hex("#005CC5"),
		Type:     ui.Hex("#6F42C1"),
		Function: ui.Hex("#22863A"),
	}

	var cache SourceHighlightCache
	totalRunes := utf8.RuneCountInString(src)
	all := cache.VisibleRanges(src, pg, 0, 100)
	if len(all) == 0 {
		t.Fatal("expected syntax highlight ranges for Markdown source")
	}

	runes := []rune(src)
	hasColor := map[ui.Color]bool{}
	for _, r := range all {
		if r.Start < 0 || r.End <= r.Start || r.End > totalRunes {
			t.Fatalf("out-of-bounds range %+v (totalRunes=%d)", r, totalRunes)
		}
		hasColor[r.Color] = true
	}
	for name, col := range map[string]ui.Color{
		"keyword":  pg.Tokens.Keyword,
		"comment":  pg.Tokens.Comment,
		"string":   pg.Tokens.String,
		"type":     pg.Tokens.Type,
		"function": pg.Tokens.Function,
		"number":   pg.Tokens.Number,
	} {
		if !hasColor[col] {
			t.Errorf("missing token color %s in highlighted Markdown ranges", name)
		}
	}

	// Verify CJK rune offsets match exact substrings (not byte offsets).
	foundHeading := false
	foundKbd := false
	for _, r := range all {
		seg := string(runes[r.Start:r.End])
		if seg == " 标题 Heading" && r.Color == pg.Tokens.Keyword && r.Weight == 600 {
			foundHeading = true
		}
		if seg == "<kbd>" && r.Color == pg.Tokens.Keyword {
			foundKbd = true
		}
	}
	if !foundHeading {
		t.Error("heading range did not align with exact CJK rune offsets")
	}
	if !foundKbd {
		t.Error("inline <kbd> HTML tag after multiline comment was not highlighted")
	}

	// Visible slice subset returns strictly fewer ranges and matches cache hit.
	subset := cache.VisibleRanges(src, pg, 5, 7)
	if len(subset) == 0 || len(subset) >= len(all) {
		t.Fatalf("expected non-empty strict subset for lines 5..7, got %d of %d", len(subset), len(all))
	}
}
