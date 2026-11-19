package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestSidebarMultiSelectClickAndRange(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	for _, name := range []string{"Alpha.md", "Beta.md", "Gamma.md"} {
		if err := os.WriteFile(filepath.Join(sp.Root, name), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.refreshTree()
	settle(tt)

	// Welcome.md is open; Cmd-clicking Alpha adds Alpha while keeping Welcome
	// in the selection and keeping Welcome as the open editor document.
	if a.doc == nil || a.doc.rel != "Welcome.md" {
		t.Fatalf("expected Welcome.md open, got %+v", a.doc)
	}
	if err := tt.ClickWith(ui.Cmd, "Alpha"); err != nil {
		t.Fatal(err)
	}
	if a.doc.rel != "Welcome.md" {
		t.Fatalf("Cmd-click switched open document to %s", a.doc.rel)
	}
	if !a.picks["Welcome.md"] || !a.picks["Alpha.md"] || len(a.picks) != 2 {
		t.Fatalf("unexpected picks after first Cmd-click: %v", a.picks)
	}

	// Cmd-clicking a folder selects it without toggling its expanded state.
	wasExpanded := sp.IsExpanded("Journal")
	if err := tt.ClickWith(ui.Cmd, "Journal"); err != nil {
		t.Fatal(err)
	}
	if sp.IsExpanded("Journal") != wasExpanded || !a.picks["Journal"] {
		t.Fatalf("Cmd-click on folder toggled expansion or missed pick: expanded=%v picks=%v", sp.IsExpanded("Journal"), a.picks)
	}

	// Cmd-clicking Alpha again unpicks only Alpha.
	if err := tt.ClickWith(ui.Cmd, "Alpha"); err != nil {
		t.Fatal(err)
	}
	if a.picks["Alpha.md"] || len(a.picks) != 2 {
		t.Fatalf("Cmd-click did not toggle Alpha off: %v", a.picks)
	}

	// Escape clears the sidebar selection before anything else.
	tt.Key(0, ui.KeyEscape)
	if len(a.picks) != 0 {
		t.Fatalf("Escape did not clear picks: %v", a.picks)
	}

	// Shift-click from Alpha to Gamma selects Alpha, Beta and Gamma (plus open Welcome).
	if err := tt.Click("Alpha"); err != nil {
		t.Fatal(err)
	}
	if a.doc.rel != "Alpha.md" || len(a.picks) != 0 {
		t.Fatalf("plain click did not open Alpha cleanly: doc=%v picks=%v", a.doc.rel, a.picks)
	}
	if err := tt.ClickWith(ui.Shift, "Gamma"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Alpha.md", "Beta.md", "Gamma.md"} {
		if !a.picks[want] {
			t.Fatalf("Shift-click missed %s in %v", want, a.picks)
		}
	}

	// Plain click on a folder clears picks and toggles the folder.
	if err := tt.Click("Journal"); err != nil {
		t.Fatal(err)
	}
	if len(a.picks) != 0 || sp.IsExpanded("Journal") == wasExpanded {
		t.Fatalf("plain click on folder did not clear picks and toggle: picks=%v expanded=%v", a.picks, sp.IsExpanded("Journal"))
	}
}

func TestSidebarMultiSelectDragIntoFolderAndUndo(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	if err := os.MkdirAll(filepath.Join(sp.Root, "Archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sp.Root, "Notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sp.Root, "Notes", "Sub.md"), []byte("# Sub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sp.Root, "Alpha.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sp.SetExpanded("Notes", true)
	sp.SetExpanded("Archive", false)
	a.refreshTree()
	settle(tt)

	// Pick Alpha.md, Notes (expanded) and its child Notes/Sub.md, plus Welcome.md.
	// Moving them into Archive must move Alpha.md, Notes (keeping Sub.md inside Notes), and Welcome.md.
	if err := tt.ClickWith(ui.Cmd, "Alpha"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ClickWith(ui.Cmd, "Notes"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ClickWith(ui.Cmd, "Sub"); err != nil {
		t.Fatal(err)
	}

	dragTo(a, tt, "Alpha.md", "Archive", 0.5, func() {
		if !a.dragGrab.active || a.dragGrab.count != 3 {
			t.Fatalf("expected stacked drag count 3, got %+v", a.dragGrab)
		}
		if !tt.HasText("移到文件夹") {
			t.Fatal("missing Move to Folder badge during multi-drag")
		}
	})

	for _, rel := range []string{"Archive/Notes/Sub.md", "Archive/Alpha.md", "Archive/Welcome.md"} {
		if _, err := os.Stat(filepath.Join(sp.Root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s to exist after batch drop: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(sp.Root, "Archive", "Sub.md")); err == nil {
		t.Fatal("Notes/Sub.md was flattened out of Notes during folder+child multi-drag")
	}

	// Undo restores all three moved items back to root.
	if err := tt.Click("撤销"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	for _, rel := range []string{"Notes/Sub.md", "Alpha.md", "Welcome.md"} {
		if _, err := os.Stat(filepath.Join(sp.Root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s restored after undo: %v", rel, err)
		}
	}
}

func TestSidebarMultiSelectReorderAndPin(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	sp.SetExpanded("Journal", false)
	for _, name := range []string{"Alpha.md", "Beta.md", "Gamma.md"} {
		if err := os.WriteFile(filepath.Join(sp.Root, name), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.refreshTree()
	settle(tt)

	// Root starts as: Journal, Alpha.md, Beta.md, Gamma.md, Welcome.md.
	// Pick Beta.md and Welcome.md (non-contiguous) and drag Welcome before Alpha.
	if err := tt.ClickWith(ui.Cmd, "Beta"); err != nil {
		t.Fatal(err)
	}
	dragTo(a, tt, "Welcome.md", "Alpha.md", 0.2, nil)
	if got := rootNames(a); !slices.Equal(got, []string{"Journal", "Beta.md", "Welcome.md", "Alpha.md", "Gamma.md"}) {
		t.Fatalf("unexpected order after gathering multi-selection: %v", got)
	}

	// Drag the selected pair above the divider onto the pin placeholder.
	settle(tt)
	var s ui.Rect
	for _, r := range a.rows {
		if r.rel == "Beta.md" {
			s = r.r
		}
	}
	x0, y0 := s.X+20, s.Y+s.H/2
	tt.Press(x0, y0)
	tt.Move(x0, y0+6)
	settle(tt)
	var ph *rowFrame
	for i := range a.rows {
		if a.rows[i].placeholder {
			ph = &a.rows[i]
		}
	}
	if ph == nil {
		t.Fatal("missing pin placeholder during multi-drag")
	}
	x1, y1 := ph.r.X+20, ph.r.Y+ph.r.H/2
	for i := 1; i <= 6; i++ {
		k := float32(i) / 6
		tt.Move(x0+(x1-x0)*k, y0+(y1-y0)*k)
		tt.Frame()
	}
	tt.Release(x1, y1)
	settle(tt)

	if !slices.Equal(sp.Pins, []string{"Beta.md", "Welcome.md"}) {
		t.Fatalf("expected both picked docs pinned in order, got %v", sp.Pins)
	}
}

func TestSidebarMultiSelectNewFolderWithItems(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	sp.SetExpanded("Journal", false)
	if err := os.WriteFile(filepath.Join(sp.Root, "Alpha.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	settle(tt)

	// Pick Journal (folder), Alpha.md and Welcome.md, then create a new folder containing all 3.
	if err := tt.ClickWith(ui.Cmd, "Journal"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ClickWith(ui.Cmd, "Alpha"); err != nil {
		t.Fatal(err)
	}
	if err := tt.RightClick("Alpha"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("新建包含这 3 项的文件夹"); err != nil {
		t.Fatalf("%v; menu=%q", err, tt.Menu())
	}
	tt.Frame()
	if a.renaming != "新建文件夹" || !tt.Focused("名称") {
		t.Fatalf("expected inline rename on new folder, got renaming=%q", a.renaming)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("整理归档")
	tt.Key(0, ui.KeyEnter)
	settle(tt)

	for _, rel := range []string{"整理归档/Journal/Ideas.md", "整理归档/Alpha.md", "整理归档/Welcome.md"} {
		if _, err := os.Stat(filepath.Join(sp.Root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("missing %s in newly created folder: %v", rel, err)
		}
	}
}

func TestSidebarMultiSelectCrossSpaceSplitAndTrash(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	work := a.state.Add("Work", t.TempDir())
	for _, name := range []string{"Alpha.md", "Beta.md", "Gamma.md", "Delta.md", "Epsilon.md"} {
		if err := os.WriteFile(filepath.Join(sp.Root, name), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var trashed []string
	a.trashFile = func(p string) error {
		trashed = append(trashed, p)
		return os.RemoveAll(p)
	}
	a.refreshTree()
	settle(tt)

	// 5 picked documents: "在分屏中打开" stays visible but disabled (> splitLimit=4).
	for _, name := range []string{"Alpha", "Beta", "Gamma", "Delta"} {
		if err := tt.ClickWith(ui.Cmd, name); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.picks) != 5 {
		t.Fatalf("expected 5 picks, got %v", a.picks)
	}
	if err := tt.RightClick("Alpha"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tt.Menu(), "在分屏中打开") {
		t.Fatalf("expected Open in Split View shown in menu: %q", tt.Menu())
	}
	tt.CloseMenu()

	// Unpick Welcome and Delta so 3 docs remain (Alpha, Beta, Gamma) -> Open in Split View works!
	if err := tt.ClickWith(ui.Cmd, "Welcome"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ClickWith(ui.Cmd, "Delta"); err != nil {
		t.Fatal(err)
	}
	if err := tt.RightClick("Alpha"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("在分屏中打开"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if a.split == nil || len(a.split.panes) != 3 || len(a.picks) != 0 {
		t.Fatalf("expected 3 split panes and cleared picks, got split=%+v picks=%v", a.split, a.picks)
	}

	// Pick Delta and Epsilon and move them to Work space via context menu.
	if err := tt.Click("Delta"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ClickWith(ui.Cmd, "Epsilon"); err != nil {
		t.Fatal(err)
	}
	if err := tt.RightClick("Epsilon"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("移到空间", work.Icon+" "+work.Name); err != nil {
		t.Fatalf("%v; menu=%q", err, tt.Menu())
	}
	settle(tt)
	for _, name := range []string{"Delta.md", "Epsilon.md"} {
		if _, err := os.Stat(filepath.Join(work.Root, name)); err != nil {
			t.Fatalf("expected %s moved to Work: %v", name, err)
		}
	}

	// Switch back to Personal, let the cross-space toast expire, then pick
	// Journal and Welcome, trash them via menu, and undo.
	a.switchSpace(sp.ID)
	tt.Advance(undoTimeout + 2*time.Second)
	settle(tt)
	if err := tt.Click("Welcome"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ClickWith(ui.Cmd, "Journal"); err != nil {
		t.Fatal(err)
	}
	if err := tt.RightClick("Journal"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("移到废纸篓"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if len(trashed) != 2 {
		t.Fatalf("expected 2 items trashed, got %v", trashed)
	}
	if err := tt.Click("撤销"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	for _, rel := range []string{"Journal/Ideas.md", "Welcome.md"} {
		if _, err := os.Stat(filepath.Join(sp.Root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s restored from trashbin: %v", rel, err)
		}
	}
}

func TestSidebarMultiSelectCrossSpaceDragPaletteAndKeyboardTrash(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	work := a.state.Add("Work", t.TempDir())
	for _, name := range []string{"Alpha.md", "Beta.md", "Gamma.md"} {
		if err := os.WriteFile(filepath.Join(sp.Root, name), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.trashFile = os.RemoveAll
	a.refreshTree()
	settle(tt)

	// Pick Alpha and Beta (with open Welcome -> 3 picks), unpick Welcome, then
	// check Command Bar exposes multi-selection commands.
	if err := tt.ClickWith(ui.Cmd, "Alpha"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ClickWith(ui.Cmd, "Beta"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ClickWith(ui.Cmd, "Welcome"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	if !tt.HasText("新建包含这 2 项的文件夹") || !tt.HasText("取消侧边栏多选") {
		t.Fatalf("command bar missing multi-select commands: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape) // closes command bar first, keeping picks
	tt.Frame()
	if len(a.picks) != 2 {
		t.Fatalf("closing command bar should keep sidebar picks: %v", a.picks)
	}

	// Cross-space drag: drag Alpha onto Work's space icon, hold 0.5 s so Work
	// opens, and drop into Work's list -> both Alpha and Beta move to Work!
	var src ui.Rect
	for _, r := range a.rows {
		if r.rel == "Alpha.md" {
			src = r.r
		}
	}
	workBtn, ok := tt.Find("切换到 Work")
	if !ok {
		t.Fatal("missing Work space button")
	}
	x0, y0 := src.X+30, src.Y+src.H/2
	bx, by := workBtn.X+workBtn.W/2, workBtn.Y+workBtn.H/2
	tt.Press(x0, y0)
	tt.Move(x0, y0+6)
	tt.Frame()
	tt.Move(bx, by)
	tt.Frame()
	tt.Advance(spaceSpringDelay + 50*time.Millisecond)
	tt.Frame()
	if a.state.ActiveSpace() != work {
		t.Fatal("holding multi-drag over Work space icon did not switch spaces")
	}
	tt.Advance(time.Second) // let the pager settle so Work's list becomes live
	tt.Frame()
	// Move into Work's list area and release.
	glide(tt, bx, by, 110, 260)
	tt.Release(110, 260)
	settle(tt)
	for _, name := range []string{"Alpha.md", "Beta.md"} {
		if _, err := os.Stat(filepath.Join(work.Root, name)); err != nil {
			t.Fatalf("cross-space drag missed %s in Work: %v", name, err)
		}
	}

	// Back in Personal, Cmd+Backspace while sidebar rows are focused trashes
	// the multi-selection.
	a.switchSpace(sp.ID)
	settle(tt)
	if err := tt.ClickWith(ui.Cmd, "Gamma"); err != nil {
		t.Fatal(err)
	}
	if len(a.picks) != 2 { // Welcome.md + Gamma.md
		t.Fatalf("expected 2 picks before Cmd+Backspace, got %v", a.picks)
	}
	tt.Key(ui.Cmd, ui.KeyBackspace)
	settle(tt)
	for _, name := range []string{"Gamma.md", "Welcome.md"} {
		if _, err := os.Stat(filepath.Join(sp.Root, name)); !os.IsNotExist(err) {
			t.Fatalf("expected %s trashed by Cmd+Backspace, stat err=%v", name, err)
		}
	}
}

func TestMultiSelectSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render sidebar multi-selection snapshots")
	setup := func(t *testing.T, dark bool) (*App, *ui.Tester) {
		a, tt := newTestApp(t)
		tt.SetSize(920, 600)
		tt.SetDark(dark)
		sp := a.state.ActiveSpace()
		a.state.Favorites = nil
		for _, rel := range []string{"项目规划.md", "架构设计.md", "周报汇总.md", "Archive/旧方案.md"} {
			full := filepath.Join(sp.Root, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte("# "+filepath.Base(rel)+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		sp.SetExpanded("Archive", false)
		a.refreshTree()
		settle(tt)
		return a, tt
	}

	for _, dark := range []bool{false, true} {
		name := "multiselect-rows"
		if dark {
			name += "-dark"
		}
		t.Run(name, func(t *testing.T) {
			_, tt := setup(t, dark)
			_ = tt.ClickWith(ui.Cmd, "Journal")
			_ = tt.ClickWith(ui.Cmd, "项目规划")
			_ = tt.ClickWith(ui.Cmd, "架构设计")
			settle(tt)
			writePNG(t, filepath.Join(dir, name+".png"), tt.Image())
		})
	}

	t.Run("multiselect-drag-stack", func(t *testing.T) {
		a, tt := setup(t, false)
		_ = tt.ClickWith(ui.Cmd, "项目规划")
		_ = tt.ClickWith(ui.Cmd, "架构设计")
		settle(tt)
		var src ui.Rect
		for _, r := range a.rows {
			if r.rel == "项目规划.md" {
				src = r.r
			}
		}
		x0, y0 := src.X+36, src.Y+src.H/2
		tt.Press(x0, y0)
		tt.Move(x0, y0-6)
		settle(tt)
		var dst ui.Rect
		for _, r := range a.rows {
			if r.rel == "Archive" {
				dst = r.r
			}
		}
		x1, y1 := dst.X+42, dst.Y+dst.H*0.5
		for i := 1; i <= 6; i++ {
			k := float32(i) / 6
			tt.Move(x0+(x1-x0)*k, (y0-6)+(y1-(y0-6))*k)
			tt.Frame()
		}
		tt.Advance(180 * time.Millisecond)
		tt.Frame()
		writePNG(t, filepath.Join(dir, "multiselect-drag-stack.png"), tt.Image())
		tt.Release(x1, y1)
	})

	t.Run("multiselect-palette", func(t *testing.T) {
		_, tt := setup(t, false)
		_ = tt.ClickWith(ui.Cmd, "Journal")
		_ = tt.ClickWith(ui.Cmd, "项目规划")
		settle(tt)
		tt.Key(ui.Cmd, ui.KeyK)
		tt.Advance(200 * time.Millisecond)
		tt.Frame()
		writePNG(t, filepath.Join(dir, "multiselect-palette.png"), tt.Image())
	})
}
