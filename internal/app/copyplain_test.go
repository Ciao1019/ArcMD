package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/mdz"
	"github.com/egoist/mygo/ui"
)

func TestCopyPlainTextCommandBarSettingsShortcutAndMDZ(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	listRel := "list-copy.md"
	if err := os.WriteFile(filepath.Join(sp.Root, listRel), []byte("# 标题\n\n- **第一条**\n- 第二条\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open(listRel)
	tt.Frame()

	ed := a.doc.ed
	leaves := ed.Doc.Leaves()
	if len(leaves) < 3 {
		t.Fatalf("unexpected leaves: %+v", leaves)
	}
	// Select the two bullet items and focus the editor.
	ed.SetSelection(leaves[1].Start, leaves[2].End)
	ed.RequestFocus = true
	tt.Frame()

	// Default Cmd+C keeps Markdown on transfer.Text.
	tt.Key(ui.Cmd, ui.KeyC)
	if got := tt.Clipboard(); got != "- **第一条**\n- 第二条" {
		t.Fatalf("default Cmd+C = %q; want Markdown list", got)
	}

	// Command bar runs "复制为纯文本" in editable mode and refocuses editor.
	tt.Key(ui.Cmd, ui.KeyK)
	a.palette.query = "复制为纯文本"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.palette.open || !tt.Focused("文档") {
		t.Fatal("command bar did not close and refocus editor")
	}
	if got, want := tt.Clipboard(), "第一条\n第二条"; got != want {
		t.Fatalf("command bar copy-plain = %q; want %q", got, want)
	}

	// Read-only locked .md still exposes "复制为纯文本" in command bar and via shortcut.
	ed.SetReadOnly(true)
	tt.SetClipboard("stale-ro")
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyK)
	a.palette.query = "复制为纯文本"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got, want := tt.Clipboard(), "第一条\n第二条"; got != want {
		t.Fatalf("read-only command bar copy-plain = %q; want %q", got, want)
	}
	ed.SetReadOnly(false)
	tt.Frame()

	// Command bar toggles default plain-text copy preference and persists it.
	tt.Key(ui.Cmd, ui.KeyK)
	a.palette.query = "开启默认复制去除 Markdown 格式"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !a.prefs.CopyPlainText {
		t.Fatal("command bar did not enable CopyPlainText")
	}
	if loaded, err := a.prefStore.Load(); err != nil || !loaded.CopyPlainText {
		t.Fatalf("CopyPlainText did not persist to prefs.json: loaded=%+v err=%v", loaded, err)
	}

	// Standard Cmd+C now puts stripped plain text on transfer.Text.
	ed.SetSelection(leaves[1].Start, leaves[2].End)
	ed.RequestFocus = true
	tt.SetClipboard("stale-default")
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyC)
	if got, want := tt.Clipboard(), "第一条\n第二条"; got != want {
		t.Fatalf("Cmd+C with CopyPlainText=true = %q; want %q", got, want)
	}

	// Settings › Writing renders the toggle and Settings › Shortcuts renders & rebinds copy-plain.
	a.openSettings(tabGeneral)
	tt.Frame()
	if !tt.HasText("默认复制去除 Markdown 格式") {
		t.Fatalf("general/writing settings missing CopyPlainText toggle: %q", tt.Texts())
	}

	a.openSettings(tabShortcuts)
	tt.Frame()
	if !tt.HasText("复制为纯文本") {
		t.Fatalf("shortcuts settings missing 复制为纯文本: %q", tt.Texts())
	}
	custom := editor.Combo{Mods: ui.Cmd | ui.Alt, Key: ui.KeyC}
	if !a.rebind("copy-plain", &custom) {
		t.Fatal("rebind copy-plain failed")
	}
	if got := a.editorBindings()["copy-plain"]; got != custom {
		t.Fatalf("rebound copy-plain = %+v; want %+v", got, custom)
	}

	t.Cleanup(func() { i18n.SetLanguage("zh-Hans") })
	i18n.SetLanguage("en")
	tt.Frame()
	if !tt.HasText("Copy as Plain Text") {
		t.Fatalf("English shortcuts settings missing Copy as Plain Text: %q", tt.Texts())
	}
	a.openSettings(tabGeneral)
	tt.Frame()
	if !tt.HasText("Copy without Markdown formatting by default") {
		t.Fatalf("English writing settings missing CopyPlainText label: %q", tt.Texts())
	}
	i18n.SetLanguage("zh-Hans")
	a.settingsOpen = false
	a.resetKeys("copy-plain")
	a.prefs.CopyPlainText = false
	a.savePrefs()
	tt.Frame()

	// MDZip archive viewer supports Copy as Plain Text via shortcut and command bar.
	mdzRel := "list-sample.mdz"
	archiveBytes, err := mdz.Build(mdz.WriteOptions{
		Manifest: mdz.Manifest{
			EntryPoint: "index.md",
			Mode:       mdz.ModeDocument,
		},
		Files: []mdz.File{
			{Path: "index.md", Data: []byte("- **MDZ 第一行**\n- MDZ 第二行\n")},
		},
	})
	if err != nil {
		t.Fatalf("mdz.Build: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sp.Root, mdzRel), archiveBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open(mdzRel)
	tt.Frame()

	mdzEd := a.doc.mdz.editors[a.doc.mdz.currentPage]
	if mdzEd == nil {
		t.Fatal("expected active MDZ page editor")
	}
	mdzEd.SetSelection(0, mdzEd.Len())
	mdzEd.RequestFocus = true
	tt.SetClipboard("stale-mdz")
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyC)
	if got, want := tt.Clipboard(), "MDZ 第一行\nMDZ 第二行"; got != want {
		t.Fatalf("MDZ shortcut copy-plain = %q; want %q", got, want)
	}
	tt.SetClipboard("stale-mdz-2")
	tt.Key(ui.Cmd, ui.KeyK)
	a.palette.query = "复制为纯文本"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got, want := tt.Clipboard(), "MDZ 第一行\nMDZ 第二行"; got != want {
		t.Fatalf("MDZ command bar copy-plain = %q; want %q", got, want)
	}
}

func TestEnglishPaperColorLabelDoesNotWrap(t *testing.T) {
	a, tt := newTestApp(t)
	t.Cleanup(func() { i18n.SetLanguage("zh-Hans") })
	i18n.SetLanguage("en")
	a.openSettings(tabAppearance)
	tt.Frame()

	labelRect, ok := tt.Find("Paper color")
	if !ok {
		t.Fatalf("English appearance settings missing Paper color label: %v", tt.Texts())
	}
	if labelRect.H > 28 || labelRect.W < 55 {
		t.Fatalf("Paper color label wrapped unexpectedly: rect=%+v", labelRect)
	}
	prevRight := float32(-1)
	for _, swatch := range []string{"Default", "Warm", "Sepia", "Sage", "Slate"} {
		r, ok := tt.Find(swatch)
		if !ok {
			t.Fatalf("missing paper swatch %q", swatch)
		}
		if r.Y < labelRect.Y+labelRect.H {
			t.Fatalf("swatch %q (%+v) overlaps Paper color label (%+v)", swatch, r, labelRect)
		}
		if prevRight >= 0 && r.X < prevRight {
			t.Fatalf("swatch %q (%+v) overlapped previous swatch right=%v", swatch, r, prevRight)
		}
		prevRight = r.X + r.W
	}
	if dir := os.Getenv("ARCMD_SNAPSHOT"); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
		savePNG(t, filepath.Join(dir, "settings-appearance-en.png"), tt)
	}
}
