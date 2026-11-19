package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

func TestMathSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render formulas")
	for _, name := range []string{"reading", "editing", "editing-dark", "editing-narrow", "error", "empty"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			source := `\int_0^\infty x^2 e^{-x} dx = 2`
			if name == "error" {
				source = `\frac{a}`
			}
			if name == "empty" {
				source = ""
			}
			doc := editor.ImportMarkdown("# 数学笔记\n\n从一个积分开始。\n\n$$\n" + source + "\n$$\n\n公式与文字一起，留在纸面上。\n\n$$\ne^{ix} = \\cos x + i \\sin x\n$$\n\n$$\n\\frac{a+b}{2} + \\sqrt{x^2+y^2}\n$$")
			encoded, err := doc.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if err := vault.Write(root(a), "Math.arcmd", encoded); err != nil {
				t.Fatal(err)
			}
			a.refreshTree()
			a.open("Math.arcmd")
			tt.Frame()
			if strings.Contains(name, "dark") {
				tt.SetDark(true)
			}
			if strings.Contains(name, "narrow") {
				tt.SetSize(540, 780)
			}
			if name != "reading" {
				focusDocument(a, tt)
				for _, leaf := range a.doc.ed.Doc.Leaves() {
					if leaf.Block.Kind == "math" {
						a.doc.ed.SetSelection(leaf.End, leaf.End)
						break
					}
				}
			}
			for range 3 {
				tt.Frame()
				tt.Advance(200 * time.Millisecond)
			}
			tt.Frame()
			savePNG(t, filepath.Join(dir, "math-"+name+".png"), tt)
		})
	}
}

func TestBoldsymbolSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render bold matrix formulas")
	source, err := os.ReadFile("../editor/testdata/normal-distribution.md")
	if err != nil {
		t.Fatal(err)
	}
	source = append(source, []byte("\n$$\n"+`\sum_{i=1}^n x_i \quad \boldsymbol{\sum}_{i=1}^n x_i`+"\n$$\n\n$$\n"+
		`\lim_{x\to 0}\frac{\sin x}{x} \quad \boldsymbol{\lim}_{x\to 0}\frac{\sin x}{x}`+"\n$$\n\n$$\n"+
		`\int_0^\infty e^{-x}\,dx \quad \boldsymbol{\int}_0^\infty e^{-x}\,dx`+"\n$$\n")...)
	for _, name := range []string{"reading", "reading-dark", "editing", "narrow"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetSize(1380, 820)
			if err := vault.Write(root(a), "Normal.md", string(source)); err != nil {
				t.Fatal(err)
			}
			a.refreshTree()
			a.open("Normal.md")
			if name == "reading-dark" {
				tt.SetDark(true)
			}
			if name == "narrow" {
				tt.SetSize(620, 820)
			}
			if name == "editing" {
				focusDocument(a, tt)
				for _, leaf := range a.doc.ed.Doc.Leaves() {
					if leaf.Block.Kind == "math" {
						a.doc.ed.SetSelection(leaf.End, leaf.End)
						break
					}
				}
			}
			for range 3 {
				tt.Frame()
				tt.Advance(200 * time.Millisecond)
			}
			tt.Frame()
			savePNG(t, filepath.Join(dir, "boldsymbol-"+name+".png"), tt)
		})
	}
}

func TestIssueFixesSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render issue verification snapshots")

	// 1. Issue #30: active row inline rename visibility in light & dark themes.
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		a, tt := newTestApp(t)
		tt.SetSize(1100, 600)
		tt.SetDark(dark)
		if err := vault.Write(root(a), "从这里开始.md", "# 从这里开始\n\n欢迎使用 ArcMD。\n"); err != nil {
			t.Fatal(err)
		}
		a.refreshTree()
		a.open("从这里开始.md")
		tt.Frame()
		a.renameIn(a.state.ActiveSpace(), "从这里开始.md")
		for range 3 {
			tt.Frame()
			tt.Advance(200 * time.Millisecond)
		}
		savePNG(t, filepath.Join(dir, "rename-"+mode+".png"), tt)
	}

	// 2. Issue #31 comment: \tag and \tag* equation numbering.
	{
		a, tt := newTestApp(t)
		tt.SetSize(1180, 680)
		md := "# 公式编号示例\n\n质能方程与欧拉恒等式：\n\n$$\nE = mc^2 \\tag{1}\n$$\n\n$$\ne^{i\\pi} + 1 = 0 \\tag*{[Euler]}\n$$\n\n多行对齐方程组：\n\n$$\n\\begin{align}\n\\nabla \\cdot \\mathbf{E} &= \\frac{\\rho}{\\varepsilon_0} \\tag{2.1} \\\\\n\\nabla \\times \\mathbf{B} &= \\mu_0 \\mathbf{J} + \\mu_0 \\varepsilon_0 \\frac{\\partial \\mathbf{E}}{\\partial t} \\tag{2.2}\n\\end{align}\n$$\n"
		if err := vault.Write(root(a), "Tags.md", md); err != nil {
			t.Fatal(err)
		}
		a.refreshTree()
		a.open("Tags.md")
		for range 3 {
			tt.Frame()
			tt.Advance(200 * time.Millisecond)
		}
		savePNG(t, filepath.Join(dir, "math-tags.png"), tt)
	}

	// 3. Issue #29: complex multi-phase Mermaid sequence diagram inline & zoom preview.
	{
		a, tt := newTestApp(t)
		tt.SetSize(1280, 900)
		seqMD := "# 系统时序图\n\n```mermaid\nsequenceDiagram\n" +
			"  participant User as Client\n" +
			"  participant CoreSvc as CoreService\n" +
			"  participant Gateway as Gateway\n" +
			"  participant MQ as MessageQueue\n" +
			"  participant Worker as RemoteWorker\n\n" +
			"  rect rgb(200,250,200)\n" +
			"  note over User,CoreSvc: Authoring Phase\n" +
			"    User ->> CoreSvc: 1.1 Create item\n" +
			"    CoreSvc ->> CoreSvc: 1.2 Attach resources / configure parameters\n" +
			"    User ->> CoreSvc: 1.3 Save draft\n" +
			"    User ->> CoreSvc: 2.1 Submit for review\n" +
			"    alt Restricted pattern detected\n" +
			"      ScriptSvc ->> User: 2.2 Reject publication\n" +
			"    else Passed check\n" +
			"      CoreSvc ->> User: 2.3 Publish succeeded\n" +
			"    end\n" +
			"  end\n" +
			"  rect rgb(200,200,250)\n" +
			"    note over User,Worker: Testing Phase\n" +
			"    User ->> CoreSvc: 3.1 Trigger test run\n" +
			"    CoreSvc ->> Gateway: 3.2 RPC invoke\n" +
			"    Gateway ->> Worker: 3.3 HTTP request\n" +
			"    Worker -->> Worker: 3.4 Execute asynchronously\n" +
			"    Worker ->> Gateway: 3.5 RPC response\n" +
			"    Gateway ->> MQ: 3.6 Publish test result\n" +
			"    MQ -->> CoreSvc: 3.7 Consume message\n" +
			"    CoreSvc ->> CoreSvc: 3.8 Update test status\n" +
			"    CoreSvc ->> User: 3.9 View test result in monitor\n" +
			"  end\n" +
			"  rect rgb(134, 190, 220)\n" +
			"    note over User,CoreSvc: Release Phase\n" +
			"    User ->> CoreSvc: 4.1 Export bundle\n" +
			"    CoreSvc ->> User: 4.2 Download archive\n" +
			"    User ->> CoreSvc: 4.3 Import bundle to target environment\n" +
			"    CoreSvc ->> CoreSvc: 4.4 Parse, persist, and auto-publish\n" +
			"  end\n```\n"
		if err := vault.Write(root(a), "Sequence.md", seqMD); err != nil {
			t.Fatal(err)
		}
		a.refreshTree()
		a.open("Sequence.md")
		for range 3 {
			tt.Frame()
			tt.Advance(200 * time.Millisecond)
		}
		savePNG(t, filepath.Join(dir, "mermaid-sequence.png"), tt)
	}
}
