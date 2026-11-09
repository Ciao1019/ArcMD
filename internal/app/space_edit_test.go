package app

import (
	"path/filepath"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo/ui"
)

func TestSpaceNameEditsInPlace(t *testing.T) {
	a, tt := newTestApp(t)
	settle(tt)
	sp := a.state.ActiveSpace()
	before, _ := tt.Find("Personal")
	row, _ := tt.Find("Journal")
	if err := tt.Click("Personal"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	field, ok := tt.Find("空间名称")
	after, _ := tt.Find("Journal")
	if !ok || field.X != before.X || field.Y != before.Y || after != row || sp.PinsCollapsed {
		t.Fatalf("rename moved the sidebar: title %v, field %v, row %v -> %v", before, field, row, after)
	}
	tt.Type("工作空间")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if sp.Name != "工作空间" || a.spaceForm.open {
		t.Fatalf("Enter did not rename: %q", sp.Name)
	}
	tt.Click("工作空间")
	tt.Frame()
	tt.Type("取消这个名称")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if sp.Name != "工作空间" || a.spaceForm.open {
		t.Fatal("Escape did not cancel the rename")
	}
	tt.Click("工作空间")
	tt.Frame()
	tt.Type("阅读")
	tt.Click("Journal")
	tt.Frame()
	if sp.Name != "阅读" || a.spaceForm.open {
		t.Fatal("blur did not save the name")
	}
	tt.Click("阅读")
	tt.Frame()
	tt.Type("  ")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if sp.Name != "阅读" || !a.spaceForm.open {
		t.Fatal("an empty name must not be saved")
	}
	tt.Click("Journal")
	tt.Frame()
	if a.spaceForm.open || sp.Name != "阅读" {
		t.Fatal("leaving an empty name should restore the original")
	}
	saved, err := a.store.Load()
	if err != nil || saved.Get(sp.ID).Name != "阅读" {
		t.Fatalf("name was not persisted: %v", err)
	}
}

func TestSpaceThemeKeepsSidebar(t *testing.T) {
	a, tt := newTestApp(t)
	settle(tt)
	before, _ := tt.Find("Journal")
	tt.RightClick("Personal")
	if err := tt.ChooseMenuItem("编辑主题颜色…"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	after, _ := tt.Find("Journal")
	card, ok := tt.Find("空间主题")
	if !ok || card.X < sidebarWidth || before != after || tt.HasText("保存") {
		t.Fatalf("theme editor replaced the sidebar: %v -> %v; card %v", before, after, card)
	}
	tt.Click("深色")
	tt.ClickAt(1000, 600)
	tt.Frame()
	if a.spaceForm.open || space.ThemeOf(a.state.ActiveSpace()).Appearance != space.AppearanceDark {
		t.Fatal("clicking away should save and close the theme editor")
	}
}

func TestSpaceIconPickerAnchoredAndImmediate(t *testing.T) {
	a, tt := newTestApp(t)
	settle(tt)
	sp := a.state.ActiveSpace()
	before, _ := tt.Find("Journal")
	if err := tt.Click("切换到 Personal"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	panel, ok := tt.Find("选择空间图标")
	anchor, _ := tt.Find("切换到 Personal")
	after, _ := tt.Find("Journal")
	if !ok || panel.Y+panel.H > anchor.Y || after != before || a.spaceForm.cardOpen {
		t.Fatalf("picker should sit above its icon: %v, anchor %v", panel, anchor)
	}
	tt.Click("搜索表情")
	tt.Type("books")
	tt.Frame()
	if err := tt.Click("📚 books"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.spaceForm.open || sp.Icon != "📚" || sp.Symbol != "" {
		t.Fatal("choosing an emoji did not save and close")
	}
	tt.Click("切换到 Personal")
	tt.Frame()
	tt.Click("图标集")
	tt.Frame()
	if err := tt.Click("图标 leaf"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.spaceForm.open || sp.Symbol != "leaf" || sp.Icon != "" {
		t.Fatal("choosing a symbol did not save and close")
	}
	tt.Click("切换到 Personal")
	tt.Frame()
	tt.Click("移除图标")
	tt.Frame()
	if a.spaceForm.open || sp.Symbol != "" || sp.Icon != "" {
		t.Fatal("removing the icon did not save and close")
	}
	tt.Click("切换到 Personal")
	tt.Frame()
	tt.Click("搜索表情")
	tt.Type("books")
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if sp.Icon != "📚" || a.spaceForm.open {
		t.Fatal("keyboard did not select the emoji")
	}
	saved, err := a.store.Load()
	if err != nil || saved.Get(sp.ID).Icon != "📚" {
		t.Fatalf("icon was not persisted: %v", err)
	}
}

func TestSpaceEditDismissAndSwitch(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	a.editSpaceProperty(sp, "icon")
	settle(tt)
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.spaceForm.open || sp.Icon != "🌱" {
		t.Fatal("Escape should dismiss the icon picker without a change")
	}
	next := a.state.Add("Work", t.TempDir())
	a.editSpace(sp)
	settle(tt)
	tt.Click("深色")
	tt.Key(ui.Cmd, ui.Key2)
	settle(tt)
	if a.spaceForm.open || a.state.ActiveSpace() != next || space.ThemeOf(sp).Appearance != space.AppearanceDark {
		t.Fatal("switching spaces should save the old space's theme and close its editor")
	}
}

func TestSpaceEditingSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render independent space editors")
	for _, mode := range []string{"theme", "icon", "name"} {
		for _, size := range []struct {
			name string
			w, h int
		}{{"desktop", 1240, 760}, {"narrow", 640, 600}, {"short", 640, 420}} {
			for _, dark := range []bool{false, true} {
				name := "space-edit-" + mode + "-" + size.name
				if dark {
					name += "-dark"
				}
				t.Run(name, func(t *testing.T) {
					a, tt := newTestApp(t)
					tt.SetSize(size.w, size.h)
					tt.SetDark(dark)
					settle(tt)
					a.editSpaceProperty(a.state.ActiveSpace(), mode)
					settle(tt)
					label := map[string]string{"theme": "空间主题", "icon": "选择空间图标", "name": "空间名称"}[mode]
					r, ok := tt.Find(label)
					if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > float32(size.w) || r.Y+r.H > float32(size.h) {
						t.Fatalf("editor outside window: %v", r)
					}
					writePNG(t, filepath.Join(dir, name+".png"), tt.Image())
				})
			}
		}
	}
}
