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
