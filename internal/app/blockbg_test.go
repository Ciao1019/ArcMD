package app

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestBlockBackgroundPaletteAndExportHTML(t *testing.T) {
	a, tt := newTestApp(t)
	settle(tt)
	source := "# 块背景测试\n\n第一段文字\n\n第二段文字\n\n> 引用文字\n"
	if err := vault.Write(root(a), "BlockBg.md", source); err != nil {
		t.Fatal(err)
	}
	a.open("BlockBg.md")
	settle(tt)
	focusDocument(a, tt)

	// Apply block background via Command Palette on caret block.
	caretOn(a, a.doc.ed.Doc.Blocks[1].ID)
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	a.palette.query = "块背景色：深蓝"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got := a.doc.ed.Doc.Blocks[1].Background; got != "blue" {
		t.Fatalf("palette set single block bg = %q, want blue", got)
	}

	// Apply via Command Palette across a multi-block selection.
	a.doc.ed.SelectBlocks(a.doc.ed.Doc.Blocks[1].ID, a.doc.ed.Doc.Blocks[3].ID)
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	a.palette.query = "块背景色：深绿"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	for i := 1; i <= 3; i++ {
		if got := a.doc.ed.Doc.Blocks[i].Background; got != "green" {
			t.Fatalf("palette multi-block[%d] bg = %q, want green", i, got)
		}
	}

	// Export HTML page includes both light and dark adaptive CSS rules.
	a.exportHTML()
	pageBytes, err := os.ReadFile(filepath.Join(root(a), "BlockBg.html"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	for _, want := range []string{
		`.arcmd-bg { border-radius: 6px; padding: 4px 10px; }`,
		`.arcmd-bg-green { background-color: #d9f5d6; }`,
		`.arcmd-bg-green { background-color: #264d2d; }`,
		`class="arcmd-bg arcmd-bg-green" data-bg="green"`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("exported HTML missing %q", want)
		}
	}

	// Clear via Command Palette.
	a.doc.ed.SelectBlocks(a.doc.ed.Doc.Blocks[1].ID, a.doc.ed.Doc.Blocks[3].ID)
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	a.palette.query = "清除块背景色"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	for i := 1; i <= 3; i++ {
		if got := a.doc.ed.Doc.Blocks[i].Background; got != "" {
			t.Fatalf("palette clear block[%d] bg = %q, want empty", i, got)
		}
	}
}

func TestBlockBackgroundSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render block backgrounds")
	source := strings.Join([]string{
		`# 块背景色视觉验收`,
		``,
		`<h2 style="background-color:#E1EAFF" data-bg="blue">二级标题 · 跳过段前大空白</h2>`,
		``,
		`<p style="background-color:#FFFFCC" data-bg="yellow">重点段落：外扩 8px 圆角底色条，正文左基线与折行位置完全不跳动。</p>`,
		``,
		`无背景色的对照普通段落，与上方色块正文左缘严格对齐。`,
		``,
		`<div style="background-color:#D9F5D6" data-bg="green">`,
		``,
		`- 无序列表项独立着色`,
		``,
		`</div>`,
		``,
		`<div style="background-color:#F0FBEF" data-bg="light-green" data-indent="1">`,
		``,
		`- 缩进子列表项随圆点左缘起始`,
		``,
		`</div>`,
		``,
		`<div style="background-color:#F0F4FF" data-bg="light-blue">`,
		``,
		`- [x] 已完成任务项着色区分`,
		``,
		`</div>`,
		``,
		`<div style="background-color:#ECE2FE" data-bg="purple">`,
		``,
		`> 引用块：2px 左侧竖条自然落在圆角色块内侧。`,
		``,
		`</div>`,
		``,
		`<details open style="background-color:#FEEAD2" data-bg="orange">`,
		`<summary>折叠块标题（仅自身着色）</summary>`,
		``,
		`折叠体内的子段落保持独立透明，不被误染。`,
		`</details>`,
		``,
		`<div class="arcmd-columns" style="display:flex;gap:24px">`,
		``,
		`<div class="arcmd-column" style="flex:1;min-width:0">`,
		``,
		`<p style="background-color:#FDE2E2" data-bg="red">左栏子块（浅红），外扩严格留在栏内。</p>`,
		``,
		`</div>`,
		``,
		`<div class="arcmd-column" style="flex:1;min-width:0">`,
		``,
		`<p style="background-color:#E1EAFF" data-bg="blue">右栏子块（浅蓝），视觉均衡清爽。</p>`,
		``,
		`</div>`,
		``,
		`</div>`,
	}, "\n")

	for _, mode := range []string{"light", "dark", "menu", "multi"} {
		t.Run(mode, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetSize(1180, 820)
			if mode == "dark" {
				tt.SetDark(true)
			}
			tt.Frame()
			if err := vault.Write(root(a), "BlockBg.md", source); err != nil {
				t.Fatal(err)
			}
			a.open("BlockBg.md")
			settle(tt)
			focusDocument(a, tt)
			switch mode {
			case "menu":
				a.doc.ed.OpenBlockMenu(a.doc.ed.Doc.Blocks[2].ID, "bg")
			case "multi":
				a.doc.ed.SelectBlocks(a.doc.ed.Doc.Blocks[2].ID, a.doc.ed.Doc.Blocks[4].ID)
				a.doc.ed.OpenBlockMenu(a.doc.ed.Doc.Blocks[2].ID, "bg")
			}
			settle(tt)
			f, err := os.Create(filepath.Join(dir, "blockbg-"+mode+".png"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err := png.Encode(f, tt.Image()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
