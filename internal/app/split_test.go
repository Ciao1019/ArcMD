package app

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func splitTestApp(t *testing.T) (*App, *ui.Tester) {
	t.Helper()
	a, tt := newTestApp(t)
	for name, text := range map[string]string{"Left.arcmd": "# Left\n\nLeft body", "Right.arcmd": "# Right\n\nRight body", "Third.arcmd": "# Third\n\nThird body"} {
		if err := writeNative(root(a), name, text); err != nil {
			t.Fatal(err)
		}
	}
	a.refreshTree()
	a.open("Left.arcmd")
	a.openSplit(a.state.ActiveSpace(), "Right.arcmd", false, false)
	settle(tt)
	if a.split == nil || len(a.split.panes) != 2 {
		t.Fatal("split not created")
	}
	return a, tt
}

func TestSplitIndependentEditing(t *testing.T) {
	a, tt := splitTestApp(t)
	left, right := a.split.panes[0].doc, a.split.panes[1].doc
	original := []string{left.ed.Text(), right.ed.Text()}
	for i, text := range []string{"first ", "second "} {
		pane := a.split.panes[i]
		tt.ClickAt(pane.bounds.X+80, pane.bounds.Y+100)
		pane.doc.ed.SetSelection(0, 0)
		tt.Type(text)
		tt.Frame()
		if a.doc != pane.doc {
			t.Fatalf("typing did not focus pane %d", i)
		}
		if !strings.HasPrefix(pane.doc.ed.Text(), text) {
			t.Fatalf("pane %d lost input: %q", i, pane.doc.ed.Text())
		}
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	if right.ed.Text() != original[1] || !strings.HasPrefix(left.ed.Text(), "first ") {
		t.Fatal("undo crossed panes")
	}
	tt.Advance(autosaveDelay + time.Second)
	tt.Frame()
	if left.dirty() || right.dirty() {
		t.Fatal("both panes must autosave")
	}
	saved, _ := vault.Read(root(a), left.rel)
	if !strings.Contains(saved, "first ") {
		t.Fatal("inactive pane not written")
	}
	a.openSplit(left.space, left.rel, false, false)
	if a.doc != left || len(a.split.panes) != 2 {
		t.Fatal("same document duplicated")
	}
}

func TestSplitPickerAndControls(t *testing.T) {
	a, tt := newTestApp(t)
	first := a.doc
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyEqual)
	tt.Frame()
	if a.split == nil || len(a.split.panes) != 2 || a.doc != nil || !a.palette.open {
		t.Fatal("add split must focus an empty picker")
	}
	tt.Type("Ideas")
	tt.Key(0, ui.KeyEnter)
	settle(tt)
	if a.doc == nil || a.doc.rel != "Journal/Ideas.arcmd" || a.split == nil {
		t.Fatalf("picker failed: %+v", a.doc)
	}
	tt.Key(ui.Ctrl|ui.Shift, ui.Key1)
	tt.Frame()
	if a.doc != first {
		t.Fatal("pane shortcut")
	}
	tt.Click("分屏 1 选项")
	tt.Click("切换为上下分屏")
	settle(tt)
	if !a.split.vertical {
		t.Fatal("flip")
	}
	tt.Click("分屏 1 选项")
	tt.Click("向后移动此分屏")
	settle(tt)
	if a.split.active != 1 || a.split.panes[1].doc != first {
		t.Fatal("reorder lost active document")
	}
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyMinus)
	settle(tt)
	if a.split != nil || a.doc == first || a.doc == nil {
		t.Fatal("close should collapse into remaining document")
	}
}

func TestSplitDivider(t *testing.T) {
	a, tt := splitTestApp(t)
	r, ok := tt.Find("调整分屏 1 大小")
	if !ok {
		t.Fatal("no divider")
	}
	x, y := r.X+r.W/2, r.Y+r.H/2
	tt.Press(x, y)
	tt.Move(x+120, y)
	tt.Frame()
	tt.Release(x+120, y)
	settle(tt)
	if a.split.panes[0].factor <= 0.55 {
		t.Fatalf("drag failed: %g", a.split.panes[0].factor)
	}
	r, _ = tt.Find("调整分屏 1 大小")
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Key(0, ui.KeyEnter)
	settle(tt)
	if math.Abs(float64(a.split.panes[0].factor-.5)) > .001 {
		t.Fatal("keyboard reset")
	}
	for _, total := range []float32{300, 640, 1200} {
		lengths := splitLengths([]*splitPane{{factor: .01}, {factor: .99}}, total)
		if lengths[0] < min(splitMinimum, (total-splitGap)/2)-.01 {
			t.Fatalf("minimum: %v", lengths)
		}
		if math.Abs(float64(lengths[0]+lengths[1]+splitGap-total)) > .01 {
			t.Fatal("layout overflow")
		}
	}
}

func TestSplitSessionAndConflict(t *testing.T) {
	a, tt := splitTestApp(t)
	s := a.split
	left := s.panes[0].doc
	a.open("Third.arcmd")
	settle(tt)
	if a.split != nil {
		t.Fatal("ordinary sidebar navigation leaves group")
	}
	tt.Click("打开分屏 1：Left")
	settle(tt)
	if a.split != s || a.doc != left {
		t.Fatal("group not restored")
	}
	other := a.state.Add("Other", t.TempDir())
	a.switchSpace(other.ID)
	a.switchSpace(left.space.ID)
	settle(tt)
	if a.split != s {
		t.Fatal("space did not remember group")
	}
	left.ed.SetSelection(0, 0)
	left.ed.ReplaceText(nil, "local ")
	if err := writeNative(left.space.Root, left.rel, "# disk version"); err != nil {
		t.Fatal(err)
	}
	a.focusPane(1, false)
	a.filesChanged(left.space.ID, []mygo.FileEvent{{Op: mygo.FileWrite, Path: left.rel}})
	if !left.conflict {
		t.Fatal("inactive conflict not detected")
	}
	a.closePane(1)
	if a.split != s || len(s.panes) != 2 || a.doc != left {
		t.Fatal("close lost unsaved conflict")
	}
	a.useDisk()
	tt.Frame()
	if s.panes[0].doc == left || !strings.Contains(s.panes[0].doc.ed.Text(), "disk version") {
		t.Fatal("replacement did not reach pane")
	}
}

func TestSplitInactiveRenameAndDelete(t *testing.T) {
	a, tt := splitTestApp(t)
	left := a.split.panes[0].doc
	right := a.doc
	if err := os.Rename(filepath.Join(root(a), left.rel), filepath.Join(root(a), "Renamed.arcmd")); err != nil {
		t.Fatal(err)
	}
	a.filesChanged(left.space.ID, []mygo.FileEvent{{Op: mygo.FileRename, OldPath: left.rel, Path: "Renamed.arcmd"}})
	if left.rel != "Renamed.arcmd" || a.split.panes[0].history[a.split.panes[0].at].rel != "Renamed.arcmd" {
		t.Fatal("rename did not follow inactive pane")
	}
	if err := os.Remove(filepath.Join(root(a), left.rel)); err != nil {
		t.Fatal(err)
	}
	a.filesChanged(left.space.ID, []mygo.FileEvent{{Op: mygo.FileRemove, Path: left.rel}})
	tt.Frame()
	if a.split.panes[0].doc != nil || a.doc != right {
		t.Fatal("deletion closed wrong pane")
	}
}

func TestSplitSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT for split views")
	for _, name := range []string{"split-light", "split-dark", "split-vertical", "split-narrow", "split-empty", "split-picker"} {
		t.Run(name, func(t *testing.T) {
			a, tt := splitTestApp(t)
			for i, p := range a.split.panes {
				text := writingSample
				if i == 1 {
					text = "# 对照与修改\n\n左边保留原文，右边整理想法。每一篇文档都有自己的光标、滚动位置和撤销记录。\n\n## 摘要\n\n- 观察文本的结构\n- 提炼有价值的细节\n- 写下新的结论\n\n> 两页并排，让思考更连贯。"
				}
				if err := writeNative(p.doc.space.Root, p.doc.rel, text); err != nil {
					t.Fatal(err)
				}
				a.withDocument(p.doc, a.reload)
			}
			if name == "split-dark" {
				a.state.ActiveSpace().SetTheme(space.Theme{Colors: []string{"#683E9C"}, Appearance: "dark", Intensity: .5})
				tt.SetDark(true)
			}
			if name == "split-vertical" {
				a.split.vertical = true
			}
			if name == "split-narrow" {
				tt.SetSize(640, 420)
			}
			if name == "split-empty" || name == "split-picker" {
				a.closePane(1)
				a.addSplitPane()
				a.palette.open = name == "split-picker"
			}
			settle(tt)
			tt.Frame()
			tt.Frame()
			writePNG(t, filepath.Join(dir, name+".png"), tt.Image())
		})
	}
}

func TestSplitPersistsAndFollowsMigration(t *testing.T) {
	a, tt := splitTestApp(t)
	a.split.vertical = true
	a.split.panes[0].factor = .35
	a.split.panes[1].factor = .65
	a.focusPane(0, true)
	tt.Frame()
	reopened, err := New(a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.split == nil || !reopened.split.vertical || reopened.doc.rel != "Left.arcmd" || reopened.split.panes[0].factor != .35 {
		t.Fatalf("layout not restored: saved=%+v group=%+v doc=%v", a.state.ActiveSpace().Split, reopened.split, reopened.doc.rel)
	}
	// Saving Markdown from an inactive pane follows its native-format migration.
	if err := vault.Write(root(a), "Import.md", "# Imported\n\nNotes"); err != nil {
		t.Fatal(err)
	}
	a.fillPane(a.split.panes[0], a.state.ActiveSpace().ID, "Import.md")
	d := a.doc
	d.ed.SetSelection(0, 0)
	d.ed.ReplaceText(nil, "Updated ")
	a.focusPane(1, false)
	if !a.flush() {
		t.Fatal("save")
	}
	tt.Frame()
	if d.rel != "Import.arcmd" || a.split.panes[0].doc != d {
		t.Fatalf("migration lost pane: %s", d.rel)
	}
}

func TestSplitSearchAndRelink(t *testing.T) {
	a, _ := splitTestApp(t)
	left := a.split.panes[0].doc
	right := a.split.panes[1].doc
	left.ed.SetSelection(0, 0)
	left.ed.ReplaceText(nil, "Needle [[Before]] #split ")
	a.refreshSearch()
	if hits := a.textHits("Needle"); len(hits) != 1 || hits[0].Path != left.rel {
		t.Fatalf("inactive edits not searchable: %v", hits)
	}
	a.relink("Before", "After", map[string]bool{left.space.ID: true})
	if !strings.Contains(left.ed.Text(), "[[After]]") || a.doc != right {
		t.Fatal("link rewrite did not preserve inactive editor")
	}
	if !a.flush() {
		t.Fatal("flush")
	}
}

func TestSplitSidebarDrag(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	if err := writeNative(root(a), "Reference.arcmd", "# Reference\n\nSide by side"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	settle(tt)
	var row ui.Rect
	for _, r := range a.rows {
		if r.rel == "Reference.arcmd" {
			row = r.r
		}
	}
	if row.W == 0 {
		t.Fatal("no source row")
	}
	x, y := row.X+30, row.Y+row.H/2
	tt.Press(x, y)
	tt.Move(x+7, y)
	tt.Frame()
	tt.Move(1160, 390)
	tt.Frame()
	tt.Release(1160, 390)
	settle(tt)
	if a.split == nil || len(a.split.panes) != 2 || a.doc.rel != "Reference.arcmd" {
		t.Fatal("drop did not split")
	}
	if _, err := os.Stat(filepath.Join(root(a), "Reference.arcmd")); err != nil {
		t.Fatal("drag moved the file")
	}
}

func TestSplitSuspendedConflictCanBeResolved(t *testing.T) {
	a, tt := splitTestApp(t)
	s := a.split
	left := s.panes[0].doc
	a.open("Third.arcmd")
	left.ed.SetSelection(0, 0)
	left.ed.ReplaceText(nil, "pending ")
	if err := writeNative(left.space.Root, left.rel, "# External"); err != nil {
		t.Fatal(err)
	}
	a.filesChanged(left.space.ID, []mygo.FileEvent{written(left.rel)})
	if a.flush() {
		t.Fatal("conflict was not blocked")
	}
	if a.split != s || a.doc != left {
		t.Fatal("conflicting hidden pane is unreachable")
	}
	a.useDisk()
	tt.Frame()
	if !a.flush() {
		t.Fatal("resolved conflict still blocks")
	}
}

func TestSplitCloseFailureKeepsBothEditors(t *testing.T) {
	a, tt := splitTestApp(t)
	first := a.split.panes[0].doc
	first.ed.SetSelection(0, 0)
	first.ed.ReplaceText(nil, "edited ")
	// An unreadable destination represented portably by a directory at the file path.
	if err := os.Remove(filepath.Join(root(a), first.rel)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root(a), first.rel), 0755); err != nil {
		t.Fatal(err)
	}
	tt.Click("关闭分屏 2")
	tt.Frame()
	if a.split == nil || len(a.split.panes) != 2 || !first.dirty() {
		t.Fatal("failed save discarded a pane")
	}
}

func TestSplitFourPanesAndSeparation(t *testing.T) {
	a, tt := splitTestApp(t)
	a.openSplit(a.state.ActiveSpace(), "Third.arcmd", false, false)
	a.openSplit(a.state.ActiveSpace(), "Welcome.arcmd", false, false)
	if len(a.split.panes) != splitLimit {
		t.Fatal("four panes")
	}
	a.addSplitPane()
	if len(a.split.panes) != splitLimit || a.palette.open {
		t.Fatal("pane limit")
	}
	separated := a.doc
	a.separateSplit()
	settle(tt)
	if a.split != nil || a.doc != separated {
		t.Fatal("separate must show the selected document")
	}
	group := a.session(a.state.ActiveSpace().ID).split
	if group == nil || len(group.panes) != 3 {
		t.Fatal("separate discarded remaining group")
	}
	a.restoreSplit(group, 0)
	settle(tt)
	if a.split != group {
		t.Fatal("remaining group not restorable")
	}
}

func TestSplitLinkAndHistoryStayInPane(t *testing.T) {
	a, tt := splitTestApp(t)
	s := a.split
	left := s.panes[0].doc
	a.navigateInPane(a.doc, func() { a.openAnywhere(a.state.ActiveSpace().ID, "Third.arcmd") })
	settle(tt)
	if a.split != s || a.doc.rel != "Third.arcmd" || s.panes[0].doc != left {
		t.Fatal("link navigation left split")
	}
	a.goHistory(-1)
	tt.Frame()
	if a.split != s || a.doc.rel != "Right.arcmd" {
		t.Fatal("back left split")
	}
	a.navigateInPane(a.doc, func() { a.openAnywhere(left.space.ID, left.rel) })
	tt.Frame()
	if a.doc != left || s.panes[1].doc == left {
		t.Fatal("link made duplicate writable pane")
	}
}

func TestSplitClosePinnedAndBackgroundReload(t *testing.T) {
	a, tt := splitTestApp(t)
	sp := a.state.ActiveSpace()
	a.togglePin(sp, "Left.arcmd")
	a.focusPane(0, true)
	tt.Frame()
	a.closeOpen(sp, "Left.arcmd")
	tt.Frame()
	if a.split != nil || a.isOpen(sp, "Left.arcmd") {
		t.Fatal("close pinned left cached editor open")
	}
	// Reopening a file held by another space's group shares its reloaded editor.
	a.openSplit(sp, "Left.arcmd", false, false)
	original := a.doc
	other := a.state.Add("Other", t.TempDir())
	a.switchSpace(other.ID)
	if err := writeNative(sp.Root, "Left.arcmd", "# Changed externally"); err != nil {
		t.Fatal(err)
	}
	if !a.showIn(sp, "Left.arcmd") {
		t.Fatal("open")
	}
	group := a.session(sp.ID).split
	if a.doc == original || group.panes[1].doc != a.doc {
		t.Fatal("reload created two document instances")
	}
}

// Slow trackpad/mouse movement must escape the equal-share snap; the snap
// acts on the displayed divider, never on the accumulated pointer distance.
func TestSplitDividerSlowDragAndClamp(t *testing.T) {
	a, tt := splitTestApp(t)
	r, _ := tt.Find("调整分屏 1 大小")
	x, y := r.X+r.W/2, r.Y+r.H/2
	before := a.split.panes[0].bounds.W
	tt.Press(x, y)
	for i := 1; i <= 40; i++ {
		tt.Move(x+float32(i), y)
		tt.Advance(16 * time.Millisecond)
	}
	tt.Release(x+40, y)
	tt.Frame()
	if got := a.split.panes[0].bounds.W - before; math.Abs(float64(got-40)) > 1 {
		t.Fatalf("slow drag moved %.2f, want 40 DIP", got)
	}
	// A large overshoot followed by a small reverse must remain clamped
	// until the pointer itself returns inside the allowed range.
	r, _ = tt.Find("调整分屏 1 大小")
	x = r.X + r.W/2
	tt.Press(x, y)
	tt.Move(x-800, y)
	tt.Move(x-790, y)
	tt.Frame()
	if got := a.split.panes[0].bounds.W; math.Abs(float64(got-splitMinimum)) > 1 {
		t.Fatalf("clamp drifted to %.2f", got)
	}
	tt.Release(x-790, y)
}

func TestSplitResizeKeepsSearchAndSavedLayoutStable(t *testing.T) {
	a, tt := splitTestApp(t)
	left, right := a.split.panes[0].doc, a.split.panes[1].doc
	originalIndex := a.liveIndex()
	originalLayout, err := os.ReadFile(filepath.Join(a.cfg.ConfigDir, "spaces.json"))
	if err != nil {
		t.Fatal(err)
	}
	r, _ := tt.Find("调整分屏 1 大小")
	x, y := r.X+r.W/2, r.Y+r.H/2
	tt.Press(x, y)
	for i := 1; i <= 12; i++ {
		tt.Move(x+float32(i)*3, y)
	}
	if a.liveIndex() != originalIndex {
		t.Fatal("resize rebuilt searchable text")
	}
	onDisk, err := os.ReadFile(filepath.Join(a.cfg.ConfigDir, "spaces.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != string(originalLayout) {
		t.Fatal("drag synchronously saved layout")
	}
	tt.Release(x+36, y)
	tt.Frame()
	onDisk, err = os.ReadFile(filepath.Join(a.cfg.ConfigDir, "spaces.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) == string(originalLayout) {
		t.Fatal("released geometry was not saved")
	}
	right.ed.SetSelection(0, 0)
	right.ed.ReplaceText(nil, "[[Left]] #changed ")
	data := a.footerData(left)
	if a.liveIndex() == originalIndex || len(data.hits) != 1 {
		t.Fatal("inactive edit did not invalidate backlinks")
	}
	a.focusPane(0, true)
	tt.Frame()
	current := a.liveIndex()
	a.focusPane(1, true)
	tt.Frame()
	if a.liveIndex() != current {
		t.Fatal("focus-only change rebuilt search")
	}
}

// Adding a split lives in the "⋯" menu, not as its own toolbar button.
func TestSplitAddFromMoreMenu(t *testing.T) {
	a, tt := newTestApp(t)
	if err := writeNative(root(a), "Left.arcmd", "# Left\n\nLeft body"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Left.arcmd")
	settle(tt)
	if _, ok := tt.Find("添加分屏"); ok {
		t.Fatal("the split button is still in the toolbar")
	}
	if err := tt.Click("更多"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("添加分屏"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if a.split == nil || len(a.split.panes) != 2 {
		t.Fatal("the menu item did not add a split")
	}
}
