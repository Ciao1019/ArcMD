package editor

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestOutlineRendersInlineMath(t *testing.T) {
	if err := loadMathFonts(); err != nil {
		t.Fatal(err)
	}
	const headingMath = `8. $a_{ij} = \pm A_{ij}$ 与正交矩阵`
	const headingCode = "`$x$` 源码"
	const headingLong = `$\dfrac{1}{2}$ 长标题测试超宽省略号长标题测试超宽省略号`

	md := "# Title\n\n" +
		strings.Repeat("text\n\n", 25) +
		"### " + headingMath + "\n\n" +
		strings.Repeat("body\n\n", 25) +
		"## " + headingCode + "\n\n" +
		"## " + headingLong + "\n\nend"

	e := New(md)
	open := true
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), Outline: open, SetOutline: func(o bool) { open = o }})
	}, 1400, 650)
	tt.Frame()

	if !e.OutlineOpen() || len(e.outline) != 4 {
		t.Fatalf("unexpected outline: open=%v items=%+v", e.OutlineOpen(), e.outline)
	}
	if _, raw := tt.Find(headingMath); raw {
		t.Fatalf("outline rendered raw TeX source %q instead of formula", headingMath)
	}
	if _, ok := tt.Find("$x$ 源码"); !ok {
		t.Fatal("inline code inside heading should remain literal text in outline")
	}
	if _, raw := tt.Find(headingLong); raw {
		t.Fatalf("long math heading %q leaked raw TeX in outline", headingLong)
	}
	om := e.outlineMathRow(e.outline[1], 400)
	if om == nil || len(om.objects) != 1 || om.objects[0].rect.W <= 0 || len(om.objects[0].box.Ops) == 0 {
		t.Fatalf("unexpected outline math layout for %q: %+v", headingMath, om)
	}
	for _, op := range om.objects[0].box.Ops {
		if op.Error {
			t.Fatalf("outline math had error op: %+v", op)
		}
	}
	if omCode := e.outlineMathRow(e.outline[2], 400); omCode != nil {
		t.Fatalf("inline code heading should not create outline math layout: %+v", omCode)
	}
	omLong := e.outlineMathRow(e.outline[3], 400)
	if omLong == nil || len(omLong.objects) != 1 || omLong.objects[0].rect.W <= 0 {
		t.Fatalf("unexpected outline math layout for %q: %+v", headingLong, omLong)
	}
	row, ok := tt.Find("跳到 " + headingMath)
	if !ok || row.H != outlineRow {
		t.Fatalf("outline math row missing or wrong height: ok=%v rect=%+v", ok, row)
	}
	if err := tt.Click("跳到 " + headingMath); err != nil {
		t.Fatal(err)
	}
	tt.Advance(jumpDuration)
	tt.Frame()
	tt.Frame()
	if e.Scroll.Y < e.outline[1].y-100 {
		t.Fatalf("outline math heading click did not scroll: scrollY=%v targetY=%v", e.Scroll.Y, e.outline[1].y)
	}
}
