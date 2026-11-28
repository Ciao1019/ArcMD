package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestHeadingShortcutsCustomizableAndTyporaPreset(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()

	// Heading 0..6 are first-class customizable bindings, not fixed shortcuts.
	keys := []ui.Key{ui.Key0, ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6}
	for i, k := range keys {
		id := fmt.Sprintf("heading-%d", i)
		b, ok := bindingOf(id)
		if !ok || !b.editor {
			t.Fatalf("missing editor binding %q: %+v, ok=%v", id, b, ok)
		}
		want := combo(ui.Cmd|ui.Alt, k)
		if got := a.combos(id); len(got) != 1 || got[0] != want {
			t.Fatalf("default combo(%q) = %+v; want %+v", id, got, want)
		}
	}
	for _, f := range fixedShortcuts {
		if f.label == "正文 / 标题 1–6" {
			t.Fatal("fixedShortcuts still lists 正文 / 标题 1–6")
		}
	}

	// Open Settings -> Shortcuts and apply Typora preset via UI click.
	a.openSettings(tabShortcuts)
	tt.Frame()
	if ui.Cmd != ui.Ctrl && !tt.HasText("⌃1…⌃9 / ⌘1…⌘9") {
		t.Fatalf("expected default space switch label before rebinding Cmd+1..6: %q", tt.Texts())
	}
	if err := tt.Click("Typora 标题键"); err != nil {
		t.Fatalf("click Typora 标题键: %v", err)
	}
	tt.Frame()

	for i := 1; i <= 6; i++ {
		id := fmt.Sprintf("heading-%d", i)
		want := combo(ui.Cmd, keys[i])
		if got := a.combos(id); len(got) != 1 || got[0] != want {
			t.Fatalf("Typora preset %q = %+v; want %+v", id, got, want)
		}
		if got := a.editorBindings()[id]; got != want {
			t.Fatalf("editorBindings()[%q] = %+v; want %+v", id, got, want)
		}
		if !a.isComboBound(want) {
			t.Fatalf("isComboBound(%+v) = false; want true", want)
		}
	}
	// zoom-reset holds Cmd+0 by default, so heading-0 stays on Opt+Cmd+0 without conflict.
	if got := a.combos("zoom-reset"); len(got) != 1 || got[0] != combo(ui.Cmd, ui.Key0) {
		t.Fatalf("zoom-reset changed unexpectedly: %+v", got)
	}
	if got := a.combos("heading-0"); len(got) != 1 || got[0] != combo(ui.Cmd|ui.Alt, ui.Key0) {
		t.Fatalf("heading-0 changed unexpectedly: %+v", got)
	}
	if ui.Cmd != ui.Ctrl {
		if !tt.HasText("⌃1…⌃9") || tt.HasText("⌃1…⌃9 / ⌘1…⌘9") {
			t.Fatalf("expected space switch label to narrow to ⌃1…⌃9 after Cmd+1..6 bound: %q", tt.Texts())
		}
	}

	reloaded, err := a.prefStore.Load()
	if err != nil {
		t.Fatalf("reload prefs: %v", err)
	}
	for i := 1; i <= 6; i++ {
		id := fmt.Sprintf("heading-%d", i)
		if reloaded.Keys[id] != fmt.Sprintf("Cmd+%d", i) {
			t.Fatalf("persisted Keys[%q] = %q", id, reloaded.Keys[id])
		}
	}
	a.settingsOpen = false
	tt.Frame()

	// Create a second space and verify Cmd+1..6 formats headings instead of switching spaces,
	// while Ctrl+1..9 still switches spaces on macOS.
	a.state.Favorites = nil
	first := a.state.ActiveSpace()
	a.newSpaceForm()
	a.spaceForm.name = "Work"
	a.submitSpace()
	work := a.state.ActiveSpace()
	if err := vault.Write(work.Root, "Notes.md", "# Title\n\nFirst paragraph\n\nSecond paragraph\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Notes.md")
	focusDocument(a, tt)

	leaves := a.doc.ed.Doc.Leaves()
	if len(leaves) < 3 {
		t.Fatalf("expected 3 leaves, got %d", len(leaves))
	}
	a.doc.ed.SetSelection(leaves[1].Start, leaves[1].Start)
	tt.Frame()

	// Cmd+2 converts the paragraph to H2 without switching spaces.
	tt.Key(ui.Cmd, ui.Key2)
	tt.Frame()
	if a.state.ActiveSpace() != work {
		t.Fatalf("Cmd+2 switched space to %q instead of formatting H2", a.state.ActiveSpace().Name)
	}
	if b := a.doc.ed.Doc.Blocks[1]; b.Kind != "heading" || b.Level != 2 {
		t.Fatalf("after Cmd+2: kind=%q level=%d; want heading level 2", b.Kind, b.Level)
	}

	// Opt+Cmd+0 converts back to paragraph.
	tt.Key(ui.Cmd|ui.Alt, ui.Key0)
	tt.Frame()
	if b := a.doc.ed.Doc.Blocks[1]; b.Kind != "paragraph" {
		t.Fatalf("after heading-0: kind=%q; want paragraph", b.Kind)
	}

	// Multi-block selection also honors rebound Cmd+3 without switching spaces.
	a.doc.ed.SelectBlocks(a.doc.ed.Doc.Blocks[1].ID, a.doc.ed.Doc.Blocks[2].ID)
	tt.Frame()
	tt.Key(ui.Cmd, ui.Key3)
	tt.Frame()
	if a.state.ActiveSpace() != work {
		t.Fatal("Cmd+3 in block selection switched spaces")
	}
	for _, idx := range []int{1, 2} {
		if b := a.doc.ed.Doc.Blocks[idx]; b.Kind != "heading" || b.Level != 3 {
			t.Fatalf("block[%d] after multi-block Cmd+3: kind=%q level=%d; want heading 3", idx, b.Kind, b.Level)
		}
	}

	// Unbound Cmd+7 still switches to space 7 if it existed, and on macOS Ctrl+1 always switches to space 1.
	if ui.Cmd != ui.Ctrl {
		tt.Key(ui.Ctrl, ui.Key1)
		tt.Frame()
		if a.state.ActiveSpace() != first {
			t.Fatalf("Ctrl+1 did not switch back to first space: got %q", a.state.ActiveSpace().Name)
		}
		ctrlOne := combo(ui.Ctrl, ui.Key1)
		if a.rebind("heading-1", &ctrlOne) {
			t.Fatal("Ctrl+1 should remain reserved as Space switcher when Cmd != Ctrl")
		}
	}
}

func TestTyporaPresetConflictAndUnboundZoomReset(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()

	// Bind bold to Cmd+1 and unbind zoom-reset so Cmd+0 is free.
	cmd1 := combo(ui.Cmd, ui.Key1)
	if !a.rebind("bold", &cmd1) {
		t.Fatal("expected Cmd+1 to be available for custom rebinding")
	}
	if !a.rebind("zoom-reset", nil) {
		t.Fatal("unbind zoom-reset failed")
	}

	a.openSettings(tabShortcuts)
	tt.Frame()
	if err := tt.Click("Typora 标题键"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	if got := a.combos("bold"); len(got) != 0 {
		t.Fatalf("conflicting bold binding was not cleared: %+v", got)
	}
	if got := a.combos("heading-1"); len(got) != 1 || got[0] != cmd1 {
		t.Fatalf("heading-1 = %+v; want Cmd+1", got)
	}
	if got := a.combos("heading-0"); len(got) != 1 || got[0] != combo(ui.Cmd, ui.Key0) {
		t.Fatalf("heading-0 = %+v; want Cmd+0 when zoom-reset is unbound", got)
	}

	// Reset all restores defaults cleanly.
	if err := tt.Click("全部恢复默认"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.isComboBound(cmd1) {
		t.Fatal("Cmd+1 still bound after 全部恢复默认")
	}
	if got := a.combos("heading-1"); len(got) != 1 || got[0] != combo(ui.Cmd|ui.Alt, ui.Key1) {
		t.Fatalf("heading-1 after reset = %+v", got)
	}
}

func TestRecordCustomHeadingShortcutInSettingsUI(t *testing.T) {
	a, tt := newTestApp(t)
	a.openSettings(tabShortcuts)
	scrollSettingsTo(t, tt, "更改快捷键：标题 2")
	if err := tt.Click("更改快捷键：标题 2"); err != nil {
		t.Fatalf("click key field for 标题 2: %v", err)
	}
	tt.Frame()
	tt.Key(ui.Cmd, ui.Key2)
	tt.Frame()

	want := combo(ui.Cmd, ui.Key2)
	if got := a.combos("heading-2"); len(got) != 1 || got[0] != want {
		t.Fatalf("recorded heading-2 = %+v; want %+v", got, want)
	}

	t.Cleanup(func() { i18n.SetLanguage("zh-Hans") })
	i18n.SetLanguage("en")
	tt.Frame()
	if !tt.HasText("Heading 2") || !tt.HasText("Typora Heading Keys") {
		t.Fatalf("English translations missing in shortcuts settings: %q", tt.Texts())
	}
}

func TestShortcutsTyporaSnapshot(t *testing.T) {
	out := os.Getenv("ARCMD_SHORTCUTS_PNG")
	if out == "" {
		dir := snapshotDir(t, "set ARCMD_SNAPSHOT or ARCMD_SHORTCUTS_PNG to render shortcuts snapshot")
		out = filepath.Join(dir, "shortcuts-typora.png")
	}
	a, tt := newTestApp(t)
	tt.SetSize(1240, 820)
	settle(tt)
	a.openSettings(tabShortcuts)
	tt.Frame()
	if err := tt.Click("Typora 标题键"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if r, ok := tt.Find("命令栏"); ok {
		tt.Scroll(r.X+r.W/2, r.Y+140, 0, 1590)
	}
	for range 4 {
		tt.Frame()
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	savePNG(t, out, tt)
}
