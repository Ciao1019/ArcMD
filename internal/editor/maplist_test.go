package editor

import (
	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/ui"
	"strings"
	"testing"
)

func TestMapListCodePlacement(t *testing.T) {
	fence := "```python\nnumpy A = X.numpy()\nB = torch.\ttensor(A)\n```"
	for _, tc := range []struct {
		name, source string
		depths       []int
	}{
		{"adjacent", "- 转换为其他Python对象：\n" + fence, []int{0, 1}},
		{"nested", "- 转换为其他Python对象：\n  - 多少啊\n" + fence + "\n\n- dfas", []int{0, 1, 2, 0}},
		{"separate", "- 转换为其他Python对象：\n  - 多少啊\n\n" + fence + "\n\n- dfas", []int{0, 1, 0, 0}},
		{"indented child", "- Outer\n  - Inner\n\n    " + strings.ReplaceAll(fence, "\n", "\n    ") + "\n\n- Tail", []int{0, 1, 2, 0}},
		{"indented outer", "- Outer\n  - Inner\n\n  " + strings.ReplaceAll(fence, "\n", "\n  ") + "\n\n- Tail", []int{0, 1, 1, 0}},
		{"empty item", "- `pd.read_csv(data_file)`\n-\n```python\ninputs, outputs = data.iloc[:, 0:2], data.iloc[:, 2]\ninputs = inputs.fillna(inputs.mean())\n```\n\n\n", []int{0, 0}},
		{"nested empty item", "- Outer\n  - Inner\n  -\n" + fence + "\n\n- Tail", []int{0, 1, 1, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := ImportMarkdown(tc.source)
			for _, reopen := range []bool{false, true} {
				if reopen {
					encoded, err := d.Encode()
					if err != nil {
						t.Fatal(err)
					}
					d, err = document.Decode(encoded)
					if err != nil {
						t.Fatal(err)
					}
				}
				src := ExportMarkdown(d)
				ed := FromDocument(d)
				m := &MindMap{}
				m.sync(ed, Options{Theme: ui.LightTheme()})
				if len(m.items) != len(tc.depths) {
					t.Fatalf("items=%d want=%d", len(m.items), len(tc.depths))
				}
				for i, want := range tc.depths {
					if m.items[i].Depth != want {
						t.Fatalf("reopen=%v node %d %q depth %d want %d", reopen, i, m.items[i].Text, m.items[i].Depth, want)
					}
				}
				if got := ExportMarkdown(ed.Doc); got != src {
					t.Fatal("view changed the source")
				}
			}
		})
	}
}

func TestMapNumberedListPreservesNumbers(t *testing.T) {
	source := strings.Join([]string{
		"# 测试",
		"",
		"导图的有序列表显示不正确",
		"",
		"1. 你好",
		"2. 他好",
		"   1. 子项一",
		"   2. 子项二",
		"3. 大家好",
		"",
		"中间分隔段落",
		"",
		"1. 第二组第一",
		"2. 第二组第二",
		"3. 三",
		"4. 四",
		"5. 五",
		"6. 六",
		"7. 七",
		"8. 八",
		"9. 九",
		"10. 十",
	}, "\n")

	ed := New(source)
	m := &MindMap{}
	opts := Options{Theme: ui.LightTheme()}
	m.sync(ed, opts)
	for _, it := range m.items {
		if body := m.bodies[it.ID]; body != nil {
			body.measure(opts)
		}
	}

	want := map[string]struct {
		num    int
		indent int
	}{
		"你好":    {1, 0},
		"他好":    {2, 0},
		"子项一":   {1, 1},
		"子项二":   {2, 1},
		"大家好":   {3, 0},
		"第二组第一": {1, 0},
		"第二组第二": {2, 0},
		"十":     {10, 0},
	}
	seen := 0
	for _, b := range ed.Doc.Blocks {
		exp, ok := want[b.Text()]
		if !ok {
			continue
		}
		seen++
		body := m.bodies[b.ID]
		if body == nil || body.editor == nil || len(body.editor.rows) != 1 {
			t.Fatalf("missing measured numbered body for %q: %+v", b.Text(), body)
		}
		gotNum := body.editor.rows[0].number
		if gotNum != exp.num || body.editor.mapListIndent != exp.indent {
			t.Fatalf("item %q got number=%d indent=%d, want number=%d indent=%d",
				b.Text(), gotNum, body.editor.mapListIndent, exp.num, exp.indent)
		}
		if b.Text() == "十" && body.span.X > body.editor.padding {
			t.Fatalf("multi-digit marker shifted wrong way: span.X=%v padding=%v", body.span.X, body.editor.padding)
		}
	}
	if seen != len(want) {
		t.Fatalf("checked %d items, want %d", seen, len(want))
	}

	// Removing item "1. 你好" must invalidate cached numbers and renumber "他好" -> 1, "大家好" -> 2.
	for _, b := range ed.Doc.Blocks {
		if b.Text() == "你好" {
			ed.Engine.DeleteBlocks([]string{b.ID})
			break
		}
	}
	m.sync(ed, opts)
	afterWant := map[string]int{
		"他好":  1,
		"大家好": 2,
	}
	for _, b := range ed.Doc.Blocks {
		exp, ok := afterWant[b.Text()]
		if !ok {
			continue
		}
		body := m.bodies[b.ID]
		if body == nil {
			t.Fatalf("missing body after renumber for %q", b.Text())
		}
		body.measure(opts)
		if got := body.editor.rows[0].number; got != exp {
			t.Fatalf("after deletion %q got number=%d, want %d", b.Text(), got, exp)
		}
	}
}
