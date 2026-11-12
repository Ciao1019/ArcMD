package app

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

const writingSample = `# 留一点空白

写作可以很简单。打开一页，记下此刻的想法，让文字慢慢成为自己的形状。

## 从一个念头开始

好的工具会让人专注于内容。用 **清晰的文字** 表达想法，用 *适当的留白* 留住节奏。

> 不必急着写完，先把想说的话写下来。

### 今天的计划

- 读完一本书的最后一章
- 整理散落在笔记里的灵感
- [x] 留半小时，安静地写点东西

## 记录，也是一种思考

把复杂的事情拆成几步，往往就能找到开始的地方。

| 阶段 | 关注什么 |
| --- | --- |
| 观察 | 真实发生的细节 |
| 整理 | 想法之间的联系 |

` + "```go\nfunc write() string {\n    return \"Keep it simple.\"\n}\n```\n"

func TestSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the writing surface")
	for _, name := range []string{"writing", "writing-dark-system", "writing-narrow", "writing-editing", "writing-source", "writing-empty", "rich-blocks", "rich-lists", "writing-wide", "writing-selection", "rich-table-hover", "callouts", "block-menu", "table-select", "text-colors", "find", "fold", "table-merge", "outline-closed", "outline-float", "outline@dark",
		// The same on dark paper, as the system's dark appearance shows it.
		"writing-empty@dark", "writing-source@dark", "rich-blocks@dark", "rich-lists@dark", "writing-selection@dark", "callouts@dark",
		"block-menu@dark", "table-select@dark", "text-colors@dark", "find@dark", "table-merge@dark", "writing-paste", "writing-paste-narrow"} {
		name, dark := strings.CutSuffix(name, "@dark")
		file := name
		if dark {
			file += "-dark"
		}
		t.Run(file, func(t *testing.T) {
			a, tt := newTestApp(t)
			text := writingSample
			if name == "writing-empty" {
				text = "# "
			}
			if strings.HasPrefix(name, "writing-paste") {
				text = strings.Repeat("第一段文字会在页面中自动换行。", 4) + "\n\n" + strings.Repeat("最后一段也会换行，光标应该停在整段末尾。", 6) + "😀"
			}
			doc := editor.ImportMarkdown(text)
			if name == "rich-blocks" || name == "rich-table-hover" || name == "table-select" || name == "table-merge" {
				doc = editor.ImportMarkdown("# 把想法写成一页\n\n结构化内容，自然地编辑。选中文字可以直接加粗、倾斜和添加链接。\n\n## 留出思考的空间\n")
				callout := model.NewBlock("callout", "从一个念头开始，把复杂的事情拆成几步。")
				cols := model.Columns()
				cols.Children[0].Children[0].Runs = []model.Run{{Text: "写下灵感", Style: model.Style{Bold: true}}, {Text: "\u2028用自己的节奏组织内容。"}}
				cols.Children[1].Children[0].Runs = []model.Run{{Text: "整理思路", Style: model.Style{Bold: true}}, {Text: "\u2028文字、清单和表格都能直接编辑。"}}
				table := editor.ImportMarkdown("| 内容 | 状态 |\n|---|---|\n| 文档引擎 | 已接入 |\n| 原生表格 | 可编辑 |")
				doc.Blocks = append(doc.Blocks, callout, cols)
				doc.Blocks = append(doc.Blocks, table.Blocks...)
				doc.Blocks = append(doc.Blocks, model.NewBlock("task", "用一段文字，开始今天的写作。"))
			}
			if name == "callouts" {
				doc = editor.ImportMarkdown("# 高亮块\n\n> [!NOTE]\n> 说明：文档会自动保存。\n\n> [!TIP]\n> 提示：输入 / 可以插入任何内容。\n\n> [!IMPORTANT]\n> 重要：.arcmd 是完整保存格式。\n\n> [!WARNING]\n> 警告：导出 Markdown 会丢失表格宽度。\n\n> [!CAUTION]\n> 危险：删除会移到废纸篓。")
			}
			if name == "rich-lists" {
				doc = editor.ImportMarkdown("# 清单与引用\n\n用 `go test` 检查改动，详见 [文档](https://example.com)。\n\n1. 打开一页\n2. 写下标题\n   - 一个念头\n   - 另一个念头\n3. 整理成段落\n\n- [ ] 还没开始\n- [x] 已经完成\n\n/")
			}
			encoded, err := doc.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if err := vault.Write(root(a), "Writing.arcmd", encoded); err != nil {
				t.Fatal(err)
			}
			a.refreshTree()
			a.open("Writing.arcmd")
			tt.SetSize(1240, 940)
			if name == "writing-dark-system" || dark {
				tt.SetDark(true)
			}
			if name == "writing-wide" {
				tt.SetSize(1680, 940)
				tt.Frame()
				a.doc.ed.Scroll.Y = 260
			}
			if name == "writing-narrow" || name == "writing-paste-narrow" {
				tt.SetSize(640, 650)
			}
			if name == "outline-closed" {
				a.prefs.OutlineClosed = true
				tt.Frame()
				tt.Advance(time.Second)
			}
			if name == "outline-float" {
				tt.SetSize(760, 650)
				tt.Frame()
				a.toggleOutline()
				tt.Frame()
				tt.Advance(time.Second)
			}
			if name == "writing-source" {
				a.doc.toggleMode()
			}
			if name == "rich-lists" {
				end := len([]rune(a.doc.ed.Text()))
				a.doc.ed.SetSelection(end, end)
				a.doc.ed.RequestFocus = true
			}
			if name == "writing-selection" {
				leaf := a.doc.ed.Doc.Leaves()[3]
				a.doc.ed.SetSelection(leaf.Start+10, leaf.Start+15)
				a.doc.ed.RequestFocus = true
			}
			if name == "writing-editing" {
				a.doc.ed.SetSelection(8, 8)
				a.doc.ed.RequestFocus = true
			}
			for range 4 {
				tt.Frame()
			}
			if strings.HasPrefix(name, "writing-paste") {
				ed := a.doc.ed
				ed.RequestFocus = true
				tt.Frame()
				tt.Key(ui.Cmd, ui.KeyA)
				tt.Key(ui.Cmd, ui.KeyC)
				tt.Key(0, ui.KeyBackspace)
				tt.Key(ui.Cmd, ui.KeyV)
				if ed.Anchor != ed.Len() || ed.Caret != ed.Len() {
					t.Fatal("paste did not place the caret at the end")
				}
			}
			if name == "rich-blocks" {
				// Hover the callout to show its block handle.
				tt.Move(700, 340)
				tt.Frame()
			}
			if name == "callouts" {
				// Open the color picker of the warning.
				for _, b := range a.doc.ed.Doc.Blocks {
					if b.Color == "yellow" {
						a.doc.ed.OpenColorPicker(b.ID)
					}
				}
				tt.Frame()
			}
			if name == "find" {
				// Find "想法", seeded from a selection.
				ed := a.doc.ed
				leaf := ed.Doc.Leaves()[1]
				i := strings.Index(leaf.Block.Text(), "想法")
				start := leaf.Start + len([]rune(leaf.Block.Text()[:i]))
				ed.SetSelection(start, start+2)
				ed.OpenFind(true)
				tt.Frame()
				tt.Frame()
			}
			if name == "fold" {
				// Fold "今天的计划" and hover "记录，也是一种思考".
				ed := a.doc.ed
				for _, b := range ed.Doc.Blocks {
					if b.Kind == "heading" && b.Text() == "今天的计划" {
						ed.Engine.ToggleFold(b.ID)
					}
				}
				tt.Frame()
				tt.Move(700, 420)
				tt.Frame()
			}
			if name == "table-merge" {
				ed := a.doc.ed
				for _, b := range ed.Doc.Blocks {
					switch b.Kind {
					case "table":
						ed.Engine.MergeCells(b.ID, model.CellRange{Row0: 1, Col0: 0, Row1: 2, Col1: 0})
						ed.Engine.SetCellBackground(b.ID, model.CellRange{Row0: 1, Col0: 1, Row1: 1, Col1: 1}, "light-green")
						ed.Engine.SetCellBackground(b.ID, model.CellRange{Row0: 2, Col0: 1, Row1: 2, Col1: 1}, "light-yellow")
					case "callout":
						ed.Engine.SetEmoji(b.ID, "💡")
					}
				}
				tt.Frame()
			}
			if name == "text-colors" {
				// Color a few words, then open the panel over a selection.
				ed := a.doc.ed
				leaf := ed.Doc.Leaves()[3]
				red, yellow, blue, none := "red", "light-yellow", "blue", ""
				ed.SetSelection(leaf.Start+15, leaf.Start+20)
				ed.Engine.SetTextColor(&red, &none)
				ed.SetSelection(leaf.Start+26, leaf.Start+31)
				ed.Engine.SetTextColor(&blue, &yellow)
				ed.SetSelection(leaf.Start, leaf.Start+5)
				ed.RequestFocus = true
				tt.Frame()
				ed.OpenTextColors()
				tt.Frame()
			}
			if name == "table-select" {
				// Select the table's second column, with its toolbar.
				ed := a.doc.ed
				for _, b := range ed.Doc.Blocks {
					if b.Kind == "table" {
						ed.SelectTableLine(b.ID, false, 1)
					}
				}
				tt.Frame()
			}
			if name == "block-menu" {
				// The menu of the "今天的计划" list item, its indent panel open.
				ed := a.doc.ed
				ed.RequestFocus = true
				tt.Frame()
				ed.OpenBlockMenu(ed.Doc.Blocks[6].ID, "indent")
				tt.Frame()
				tt.Frame()
			}
			if name == "rich-table-hover" {
				tt.Move(700, 540)
				tt.Frame()
			}
			if name == "writing-selection" {
				// Hover the code block to show its copy button.
				tt.Move(900, 860)
				tt.Frame()
			}
			savePNG(t, filepath.Join(dir, file+".png"), tt)
		})
	}
}

// snapshotDir returns the directory ARCMD_SNAPSHOT names, created if
// missing, so each snapshot test can run on its own. It skips the test
// when the variable is unset.
func snapshotDir(t *testing.T, skip string) string {
	t.Helper()
	dir := os.Getenv("ARCMD_SNAPSHOT")
	if dir == "" {
		t.Skip(skip)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// savePNG writes the tester's last frame to file.
func savePNG(t *testing.T, file string, tt *ui.Tester) {
	t.Helper()
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}
