package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/prefs"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestNarrowWindowWideningHysteresisKeepsBodyWide(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()

	// Narrow below 900 folds the sidebar away.
	tt.SetSize(840, 720)
	tt.Frame()
	tt.Advance(slide + 50*time.Millisecond)
	tt.Frame()
	if !a.folded() {
		t.Fatal("expected sidebar folded below narrowWidth")
	}
	surface840, ok := tt.Find("文档")
	if !ok {
		t.Fatal("missing document surface at 840")
	}

	// Widening into 900..1139 keeps the sidebar folded so the page grows wider
	// instead of shrinking by 230 DIP as soon as 900 is crossed.
	tt.SetSize(1020, 720)
	tt.Frame()
	tt.Advance(slide + 50*time.Millisecond)
	tt.Frame()
	if !a.folded() {
		t.Fatal("sidebar unfolded prematurely at 1020 and squeezed the body")
	}
	surface1020, _ := tt.Find("文档")
	if surface1020.W <= surface840.W+100 {
		t.Fatalf("widening window from 840 to 1020 did not widen document surface: %v -> %v", surface840.W, surface1020.W)
	}

	// Widening past narrowUnfoldWidth (1140+) brings the sidebar back while
	// keeping the document card wider than it was at 840.
	tt.SetSize(1200, 720)
	tt.Frame()
	tt.Advance(slide + 50*time.Millisecond)
	tt.Frame()
	if a.folded() {
		t.Fatal("sidebar did not unfold once wide enough at 1200")
	}
	surface1200, _ := tt.Find("文档")
	if surface1200.W < surface840.W {
		t.Fatalf("unfolding sidebar at 1200 made document narrower than at 840: %v < %v", surface1200.W, surface840.W)
	}
}

func TestAutosaveToggleAndUnsavedPrompt(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()

	origWelcome, err := vault.Read(root(a), "Welcome.md")
	if err != nil {
		t.Fatal(err)
	}

	// Turn off Autosave in Settings › General › Writing.
	a.openSettings(tabGeneral)
	tt.Frame()
	if err := tt.Click("自动保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.prefs.NoAutosave {
		t.Fatal("clicking 自动保存 switch did not set NoAutosave")
	}
	savedPrefs, err := prefs.Store{Path: a.prefStore.Path}.Load()
	if err != nil || !savedPrefs.NoAutosave {
		t.Fatalf("NoAutosave not persisted: %+v (%v)", savedPrefs, err)
	}
	a.settingsOpen = false
	tt.Frame()

	// Edit Welcome.md and advance well past autosaveDelay: disk file stays untouched.
	caretAtEnd(a, tt)
	tt.Type(" Draft without autosave.")
	tt.Frame()
	tt.Advance(4 * autosaveDelay)
	tt.Frame()
	if !a.doc.dirty() || !tt.HasText("已修改") {
		t.Fatalf("expected document to stay dirty with 已修改 badge; texts=%q", tt.Texts())
	}
	diskNow, _ := vault.Read(root(a), "Welcome.md")
	if diskNow != origWelcome {
		t.Fatalf("file was overwritten while NoAutosave was enabled: got %q", diskNow)
	}

	// Opening another document prompts Save / Don't Save / Cancel.
	a.open("Journal/Ideas.md")
	tt.Frame()
	if !a.unsaved.open || a.doc.rel != "Welcome.md" || !tt.HasText("保存对「Welcome」的修改吗？") {
		t.Fatalf("expected unsaved prompt before leaving Welcome.md: open=%v doc=%q texts=%q", a.unsaved.open, a.doc.rel, tt.Texts())
	}

	// 1. Cancel stays on Welcome.md with edits intact.
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.unsaved.open || a.doc.rel != "Welcome.md" || !strings.Contains(a.doc.ed.Text(), "Draft without autosave.") {
		t.Fatalf("Cancel did not preserve current dirty document: doc=%q", a.doc.rel)
	}

	// 2. Don't Save discards in-memory edits without touching disk and switches to Ideas.md.
	a.open("Journal/Ideas.md")
	tt.Frame()
	if err := tt.Click("不保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.doc.rel != "Journal/Ideas.md" {
		t.Fatalf("Don't Save did not continue to Journal/Ideas.md: got %q", a.doc.rel)
	}
	diskAfterDiscard, _ := vault.Read(root(a), "Welcome.md")
	if diskAfterDiscard != origWelcome {
		t.Fatalf("Don't Save modified Welcome.md on disk: got %q", diskAfterDiscard)
	}

	// 3. Explicit Cmd+S saves immediately when NoAutosave is enabled.
	caretAtEnd(a, tt)
	tt.Type(" Explicit manual save.")
	tt.Frame()
	tt.Advance(3 * autosaveDelay)
	tt.Frame()
	if !a.doc.dirty() {
		t.Fatal("Ideas.md should still be dirty before Cmd+S")
	}
	tt.Key(ui.Cmd, ui.KeyS)
	tt.Frame()
	if a.doc.dirty() {
		t.Fatal("Cmd+S did not save Ideas.md")
	}
	ideasDisk, _ := vault.Read(root(a), "Journal/Ideas.md")
	if !strings.Contains(ideasDisk, "Explicit manual save.") {
		t.Fatalf("Cmd+S did not write Ideas.md to disk: %q", ideasDisk)
	}

	// 4. Quitting / closing on non-darwin with unsaved edits prompts and Save writes before closing.
	caretAtEnd(a, tt)
	tt.Type(" Save on quit.")
	tt.Frame()
	if a.closeMain(nil, "linux") {
		t.Fatal("closeMain allowed exit with unsaved edits while NoAutosave was enabled")
	}
	tt.Frame()
	if !a.unsaved.open {
		t.Fatal("closeMain did not open unsaved prompt")
	}
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	ideasQuitDisk, _ := vault.Read(root(a), "Journal/Ideas.md")
	if !strings.Contains(ideasQuitDisk, "Save on quit.") {
		t.Fatalf("choosing Save on close prompt did not write edits: %q", ideasQuitDisk)
	}
}

func TestIssue32TableFormulaSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render issue 32 snapshots")
	a, tt := newTestApp(t)
	tt.SetDark(true)
	a.prefs.OutlineClosed = true

	md := strings.Join([]string{
		"# 系统名义模型参数",
		"",
		"| 个体 | 相对阶 | 名义输出方程 | 指令上限候选值 |",
		"| :---: | :---: | :---: | :---: |",
		`| 1 | 1 | $\dot{y}_1 = -0.6y_1 + a_1 u_1$ | 2.0 |`,
		`| 2 | 2 | $\ddot{y}_2 = -0.4y_2 - 0.8\dot{y}_2 + a_2 u_2$ | 2.5 |`,
		`| 3 | 3 | $y_3^{(3)} = -0.2y_3 - 0.6\dot{y}_3 - 0.9\ddot{y}_3 + a_3 u_3$ | 3.0 |`,
		"",
		"窄分屏下的长公式自动在关系符与加减号处折行，不再挤压发虚：",
		"",
		"$$",
		`y_3^{(3)}(t) = -0.2y_3(t) - 0.6\dot{y}_3(t) - 0.9\ddot{y}_3(t) + a_3 u_3(t) + d_3(t)`,
		"$$",
	}, "\n")
	if err := os.WriteFile(filepath.Join(root(a), "Welcome.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	a.reload()
	tt.SetSize(1180, 720)
	settle(tt)
	savePNG(t, filepath.Join(dir, "issue32-dark-table.png"), tt)

	// Also render at narrow 680px and 480px windows to verify sidebar stays folded and formulas wrap cleanly.
	tt.SetSize(680, 720)
	settle(tt)
	savePNG(t, filepath.Join(dir, "issue32-narrow-680.png"), tt)

	tt.SetSize(480, 720)
	settle(tt)
	savePNG(t, filepath.Join(dir, "issue32-narrow-480.png"), tt)
}
