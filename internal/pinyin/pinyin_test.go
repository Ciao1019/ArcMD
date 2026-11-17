package pinyin

import (
	"slices"
	"testing"
)

func TestSyllablesAndVariants(t *testing.T) {
	if got := Syllables('周'); len(got) == 0 || got[0] != "zhou" {
		t.Fatalf("Syllables('周') = %v", got)
	}
	if got := Syllables('〇'); !slices.Equal(got, []string{"ling"}) {
		t.Fatalf("Syllables('〇') = %v", got)
	}
	if got := Syllables('A'); got != nil {
		t.Fatalf("Syllables('A') = %v, want nil", got)
	}
	lv := Syllables('旅')
	if !slices.Contains(lv, "lv") || !slices.Contains(lv, "lu") {
		t.Fatalf("Syllables('旅') missing v/u variants: %v", lv)
	}
	nv := Syllables('女')
	if !slices.Contains(nv, "nv") || !slices.Contains(nv, "nu") {
		t.Fatalf("Syllables('女') missing v/u variants: %v", nv)
	}
	lve := Syllables('略')
	if !slices.Contains(lve, "lve") || !slices.Contains(lve, "lue") {
		t.Fatalf("Syllables('略') missing v/u variants: %v", lve)
	}
}

func TestScoreTokenPinyinCases(t *testing.T) {
	mustMatch := []struct {
		token, target string
	}{
		{"zhoubao", "周报"},
		{"zb", "周报"},
		{"zhoub", "周报"},
		{"zho", "周报"},
		{"zbao", "周报"},
		{"zhb", "周报"},
		{"lvxing", "旅行日记"},
		{"luxing", "旅行日记"},
		{"lx", "旅行日记"},
		{"nv", "女"},
		{"nu", "女"},
		{"lve", "策略"},
		{"lue", "策略"},
		{"chongfu", "重复"},
		{"zhongyao", "重要"},
		{"yinyue", "音乐"},
		{"kuaile", "快乐"},
		{"hangye", "行业"},
		{"xingdong", "行动"},
		{"tiaoshi", "调试"},
		{"diaodu", "调度"},
		{"cangku", "仓库"},
		{"xizang", "西藏"},
		{"v2fb", "v2 发布"},
		{"arcmd", "ArcMD"},
		{"gozb", "Go 周报"},
		{"arc设置", "ArcMD 设置"},
		{"en", "嗯哼"},
	}
	for _, tc := range mustMatch {
		if _, ok := ScoreToken(tc.token, tc.target); !ok {
			t.Errorf("ScoreToken(%q, %q) = false, want true", tc.token, tc.target)
		}
	}

	mustReject := []struct {
		token, target string
	}{
		{"ou", "周"},
		{"ao", "报"},
		{"xyz", "周报"},
		{"zhoubaox", "周报"},
		{"en", "苹果"},
	}
	for _, tc := range mustReject {
		if _, ok := ScoreToken(tc.token, tc.target); ok {
			t.Errorf("ScoreToken(%q, %q) = true, want false", tc.token, tc.target)
		}
	}
}

func TestScoreTokenRanking(t *testing.T) {
	fullScore, ok1 := ScoreToken("zhoubao", "周报")
	longScore, ok2 := ScoreToken("zhoubao", "本周的工作周报总结")
	if !ok1 || !ok2 || fullScore <= longScore {
		t.Fatalf("exact short title should outrank longer title: %d vs %d", fullScore, longScore)
	}

	directScore, ok3 := ScoreToken("zb", "周报")
	skipScore, ok4 := ScoreToken("zb", "自动备份")
	if !ok3 || !ok4 || directScore <= skipScore {
		t.Fatalf("consecutive match should outrank skipped match: %d vs %d", directScore, skipScore)
	}

	litScore, ok5 := ScoreToken("go", "Go 笔记")
	pyScore, ok6 := ScoreToken("go", "狗笔记")
	if !ok5 || pyScore > litScore && ok6 {
		// Note: "狗" is "gou", wait "go" matches trailing prefix of "gou"; literal "Go" must beat "狗"
		t.Fatalf("literal match %d should outrank pinyin prefix %d", litScore, pyScore)
	}
}
