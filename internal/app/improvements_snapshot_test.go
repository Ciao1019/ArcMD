package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// openSample writes markdown as a native document and opens it.
func openSample(t *testing.T, a *App, name, markdown string) {
	t.Helper()
	encoded, err := editor.ImportMarkdown(markdown).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root(a), name, encoded); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open(name)
}

// TestImprovementsSnapshot renders the block selection, the toggle block,
// the templates and the other additions, for review by eye.
func TestImprovementsSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the newer features")
	shots := map[string]func(*testing.T, *App, *ui.Tester){
		"blocks-selected": func(t *testing.T, a *App, tt *ui.Tester) {
			openSample(t, a, "Plan.arcmd", "# 本周计划\n\n先把想法写下来。\n\n## 要做的事\n\n- 读完一本书\n- 整理笔记\n\n最后留一点时间散步。")
			tt.Frame()
			ed := a.doc.ed
			ed.SelectBlocks(ed.Doc.Blocks[2].ID, ed.Doc.Blocks[4].ID)
		},
		"blocks-menu": func(t *testing.T, a *App, tt *ui.Tester) {
			openSample(t, a, "Plan.arcmd", "# 本周计划\n\n先把想法写下来。\n\n## 要做的事\n\n- 读完一本书\n- 整理笔记\n\n最后留一点时间散步。")
			tt.Frame()
			ed := a.doc.ed
			ed.SelectBlocks(ed.Doc.Blocks[2].ID, ed.Doc.Blocks[4].ID)
			tt.Frame()
			ed.OpenBlockMenu(ed.Doc.Blocks[2].ID, "")
		},
		"appearance-settings": func(t *testing.T, a *App, tt *ui.Tester) {
			a.prefs.Font, a.prefs.Page = "serif", "wide"
			a.openSettings(tabAppearance)
		},
		"page-serif-wide": func(t *testing.T, a *App, tt *ui.Tester) {
			a.prefs.Font, a.prefs.Page = "serif", "wide"
			openSample(t, a, "Essay.arcmd", writingSample)
		},
		"page-kai-narrow": func(t *testing.T, a *App, tt *ui.Tester) {
			a.prefs.Font, a.prefs.Page = "kai", "narrow"
			openSample(t, a, "Essay.arcmd", writingSample)
		},
		"undo-toast": func(t *testing.T, a *App, tt *ui.Tester) {
			tt.Frame()
			a.placeEntry("Journal/Ideas.arcmd", "", 0)
		},
		"toggle": func(t *testing.T, a *App, tt *ui.Tester) {
			openSample(t, a, "FAQ.arcmd", "# 常见问题\n\n<details open>\n<summary>文档保存在哪里？</summary>\n\n每篇文档都是空间文件夹里的一个 .arcmd 文件。\n\n- 可以随时在访达中打开\n\n</details>\n\n<details>\n<summary>可以导出 Markdown 吗？</summary>\n\n可以。\n\n</details>\n\n其他问题请在 GitHub 提出。")
		},
		"complete-emoji": func(t *testing.T, a *App, tt *ui.Tester) {
			openSample(t, a, "Note.arcmd", "# 今天\n\n心情不错")
			focusDocument(a, tt)
			end := a.doc.ed.Len()
			a.doc.ed.SetSelection(end, end)
			tt.Frame()
			tt.Type(" :smi")
		},
		"complete-mention": func(t *testing.T, a *App, tt *ui.Tester) {
			openSample(t, a, "Note.arcmd", "# 今天\n\n截止日期")
			focusDocument(a, tt)
			end := a.doc.ed.Len()
			a.doc.ed.SetSelection(end, end)
			tt.Frame()
			tt.Type(" @")
		},
		"replace-all": func(t *testing.T, a *App, tt *ui.Tester) {
			openSample(t, a, "Essay.arcmd", writingSample)
			openSample(t, a, "Journal/Notes.arcmd", "# 笔记\n\n写作是一种整理想法的方式，写作也需要留白。")
			a.refreshSearch()
			a.openReplaceAll()
			a.replace.find, a.replace.with = "写作", "书写"
		},
		"word-goal": func(t *testing.T, a *App, tt *ui.Tester) {
			openSample(t, a, "Essay.arcmd", writingSample)
			a.doc.ed.SetGoal(500)
		},
		"crop": func(t *testing.T, a *App, tt *ui.Tester) {
			img := image.NewRGBA(image.Rect(0, 0, 640, 400))
			for y := 0; y < 400; y++ {
				for x := 0; x < 640; x++ {
					img.Set(x, y, color.RGBA{uint8(80 + x/5), uint8(120 + y/4), 200, 255})
				}
			}
			var buf bytes.Buffer
			png.Encode(&buf, img)
			os.MkdirAll(filepath.Join(root(a), "assets"), 0o755)
			os.WriteFile(filepath.Join(root(a), "assets", "sky.png"), buf.Bytes(), 0o644)
			openSample(t, a, "Pic.arcmd", "# 照片\n\n![](assets/sky.png)")
			tt.Frame()
			ed := a.doc.ed
			ed.OpenCropper(ed.Doc.Blocks[1].ID)
			tt.Frame()
		},
		"starter": func(t *testing.T, a *App, tt *ui.Tester) {
			a.newSpaceForm()
			a.spaceForm.name = "Work"
			a.submitSpace()
		},
	}
	for name, setup := range shots {
		for _, dark := range []bool{false, true} {
			file := name
			if dark {
				file += "-dark"
			}
			t.Run(file, func(t *testing.T) {
				a, tt := newTestApp(t)
				tt.SetSize(1240, 760)
				tt.SetDark(dark)
				setup(t, a, tt)
				tt.Frame()
				tt.Advance(time.Second)
				tt.Frame()
				savePNG(t, filepath.Join(dir, file+".png"), tt)
			})
		}
	}
}

// TestWindowSnapshot renders a document in a window of its own.
func TestWindowSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render a document window")
	for _, dark := range []bool{false, true} {
		a, tt := newTestApp(t)
		var view func(*ui.Context)
		a.newWindow = func(title string, v func(*ui.Context), close func() bool) windowHandle {
			view = v
			return &fakeWindow{}
		}
		tt.Frame()
		openSample(t, a, "Essay.arcmd", writingSample)
		a.openInWindow(a.state.ActiveSpace(), "Essay.arcmd")
		wt := ui.NewTester(view, 900, 640)
		wt.SetDark(dark)
		wt.SetTitleBar(ui.TitleBar{Height: 44, Left: 78})
		wt.Frame()
		wt.Advance(time.Second)
		wt.Frame()
		name := "window"
		if dark {
			name += "-dark"
		}
		savePNG(t, filepath.Join(dir, name+".png"), wt)
	}
}

// TestEnglishSnapshot renders the window, the settings and the command
// bar in English.
func TestEnglishSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the interface in English")
	t.Cleanup(func() { i18n.SetLanguage("zh-Hans") })
	for _, name := range []string{"en-window", "en-settings", "en-palette", "en-menu"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			a.prefs.Language = "en"
			a.applyLanguage()
			tt.SetSize(1240, 760)
			openSample(t, a, "Essay.arcmd", writingSample)
			switch name {
			case "en-settings":
				a.openSettings(tabAppearance)
			case "en-palette":
				a.openPalette()
			case "en-menu":
				tt.Frame()
				a.doc.ed.SelectAllBlocks()
				tt.Frame()
				a.doc.ed.OpenBlockMenu(a.doc.ed.Doc.Blocks[0].ID, "")
			}
			tt.Frame()
			tt.Advance(time.Second)
			tt.Frame()
			savePNG(t, filepath.Join(dir, name+".png"), tt)
		})
	}
}
