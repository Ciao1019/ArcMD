package database

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/vault"
)

func TestFormula2EngineExpressionsBuiltinsAndCycles(t *testing.T) {
	root := t.TempDir()
	db := NewDefault("研发指标", SourceConfig{Type: SourceFolder})
	db.Properties = append(db.Properties,
		Property{ID: "p_score", Key: "score", Name: "权重", Type: PropNumber},
		Property{ID: "p_hours", Key: "hours", Name: "耗时", Type: PropNumber},
		Property{ID: "p_done", Key: "done", Name: "完成", Type: PropCheckbox},
		Property{ID: "p_note", Key: "note", Name: "备注", Type: PropText},
		Property{
			ID:      "p_f_ratio",
			Key:     "ratio",
			Name:    "效率",
			Type:    PropFormula,
			Formula: &FormulaConfig{Expression: `if(prop("耗时") == 0, 0, round(prop("权重") / prop("耗时"), 2))`},
		},
		Property{
			ID:      "p_f_cyc_a",
			Key:     "cyc_a",
			Name:    "循环A",
			Type:    PropFormula,
			Formula: &FormulaConfig{Expression: `prop("循环B") + 1`},
		},
		Property{
			ID:      "p_f_cyc_b",
			Key:     "cyc_b",
			Name:    "循环B",
			Type:    PropFormula,
			Formula: &FormulaConfig{Expression: `prop("循环A") + 1`},
		},
	)
	raw, err := EncodeDatabaseMarkdown(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "研发指标.db.md", raw); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "研发指标/item.md", "---\nstatus: 进行中\ntags:\n  - 重要\n  - 灵感\ndate: 2026-11-23\ndate_end: 2026-11-28\nscore: 100\nhours: 3\ndone: true\nnote: \"  ArcMD-2026  \"\n---\n\n# 引擎优化\n"); err != nil {
		t.Fatal(err)
	}

	st, err := Open(root, "研发指标.db.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := st.RecordByPath(filepath.ToSlash("研发指标/item.md"))
	if rec == nil {
		t.Fatal("missing item.md record")
	}
	now := time.Date(2026, 11, 23, 14, 30, 0, 0, time.UTC)

	assertNum := func(expr string, want float64) {
		t.Helper()
		v := EvaluateFormula(st, rec, expr, now)
		if v.Kind != ValNumber || v.Number != want {
			t.Fatalf("expr %q => %+v, want number %v", expr, v, want)
		}
	}
	assertStr := func(expr string, want string) {
		t.Helper()
		v := EvaluateFormula(st, rec, expr, now)
		if v.Kind != ValText || v.Text != want {
			t.Fatalf("expr %q => %+v, want text %q", expr, v, want)
		}
	}
	assertBool := func(expr string, want bool) {
		t.Helper()
		v := EvaluateFormula(st, rec, expr, now)
		if v.Kind != ValBool || v.Bool != want {
			t.Fatalf("expr %q => %+v, want bool %v", expr, v, want)
		}
	}
	assertErr := func(expr string, want string) {
		t.Helper()
		v := EvaluateFormula(st, rec, expr, now)
		if v.Kind != ValError || v.Error != want {
			t.Fatalf("expr %q => %+v, want error %q", expr, v, want)
		}
	}

	// Math, precedence & short-circuiting.
	assertNum(`2 ^ 3 ^ 2`, 512)
	assertNum(`10 % 3 + abs(-4) + ceil(1.2) + floor(2.9) + min(9, 4, 7) + max([1, 5, 2])`, 1+4+2+2+4+5)
	assertNum(`round(sqrt(16) + pow(2, 3), 0)`, 12)
	assertNum(`let(base, prop("权重"), base / 2 + 15)`, 65)
	assertNum(`lets(a, 10, b, a * 2, c, b + 5, c)`, 25)
	assertNum(`prop("完成") ? prop("效率") : -1`, 33.33)
	assertErr(`10 / 0`, "#DIV/0!")
	assertErr(`prop("名称" +`, "#SYNTAX!")
	if err := ValidateFormula(`if(prop("权重") > 10, "ok", "low")`); err != nil {
		t.Fatalf("ValidateFormula valid expr unexpected error: %v", err)
	}
	if err := ValidateFormula(`if(1, `); err == nil {
		t.Fatal("expected ValidateFormula syntax error")
	}

	// Logic & ifs.
	assertStr(`ifs(prop("权重") > 120, "S", prop("权重") >= 100, "A", "B")`, "A")
	assertBool(`and(not(empty(prop("名称"))), equal(prop("状态"), "进行中"), unequal(prop("耗时"), 0))`, true)

	// String functions + method chaining.
	assertStr(`prop("note").trim().lower().replaceAll("[0-9]+", "v2")`, "arcmd-v2")
	assertStr(`concat(prop("名称"), "-", format(prop("权重")))`, "引擎优化-100")
	assertStr(`slice("abcdef", 1, 4) + ":" + substring("hello", -2)`, "bcd:lo")
	assertStr(`padStart("7", 3, "0") + "/" + padEnd("a", 3, "_")`, "007/a__")
	assertBool(`test("task-42", "^task-\\d+$") && startsWith("ArcMD", "Arc") && endsWith("ArcMD", "MD")`, true)
	assertNum(`match("a1b22c333", "\\d+").length()`, 3)
	assertStr(`join(["x", "y", "z"], " | ")`, "x | y | z")

	// Date functions.
	assertStr(`formatDate(today(), "YYYY-MM-DD")`, "2026-11-23")
	assertStr(`formatDate(now(), "YYYY/MM/DD HH:mm")`, "2026/11/23 14:30")
	assertStr(`formatDate(dateAdd(prop("date"), 5, "days"), "YYYY-MM-DD")`, "2026-11-28")
	assertStr(`formatDate(dateSubtract(parseDate("2026-11-10"), 1, "months"), "YYYY-MM-DD")`, "2026-10-10")
	assertNum(`dateBetween(dateEnd(prop("date")), dateStart(prop("date")), "days")`, 5)
	assertNum(`year(today()) + month(today()) + date(today()) + hour(now()) + minute(now())`, 2026+11+23+14+30)

	// Lists & higher-order functions (current + index).
	assertNum(`[10, 20, 30].map(current + index).sum()`, 10+21+32)
	assertNum(`[1, 2, 3, 4, 5].filter(current % 2 == 1).mean()`, 3)
	assertNum(`[1, 5, 2, 9].median()`, 3.5)
	assertNum(`[4, 1, 3, 1].unique().sort().at(1)`, 3)
	assertNum(`flat([[1, 2], [3, 4]]).reverse().first()`, 4)
	assertStr(`prop("标签").filter(current == "重要").join(",")`, "重要")
	assertBool(`[2, 4, 6].every(current % 2 == 0) && [1, 3, 4].some(current == 4)`, true)
	assertNum(`[10, 20, 30].find(current > 15)`, 20)
	assertNum(`[10, 20, 30].findIndex(current == 30)`, 2)
	assertNum(`[10, 20, 30, 40].count(current >= 20)`, 3)

	// Cycle detection via Store.Query.
	res := st.Query("", QueryOptions{Now: now})
	if res.TotalCount != 1 {
		t.Fatalf("expected 1 row, got %d", res.TotalCount)
	}
	ratioVal := res.Rows[0].ValueFor(db.Properties[len(db.Properties)-3])
	if ratioVal.Kind != ValNumber || ratioVal.Number != 33.33 {
		t.Fatalf("expected computed 效率 = 33.33, got %+v", ratioVal)
	}
	cycVal := res.Rows[0].ValueFor(db.Properties[len(db.Properties)-2])
	if cycVal.Kind != ValError || cycVal.Error != "#CYCLE!" {
		t.Fatalf("expected #CYCLE! on cyclic formulas, got %+v", cycVal)
	}
}
