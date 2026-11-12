package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFolderSortByNameAndTime(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	dir := filepath.Join(root(a), "Journal")
	base := time.Now().Add(-time.Hour)
	for i, name := range []string{"b.arcmd", "a.arcmd", "c.arcmd"} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(`{"format":"arcmd","version":1,"blocks":[{"id":"x","kind":"paragraph"}]}`), 0o644)
		at := base.Add(time.Duration(i) * time.Minute)
		os.Chtimes(p, at, at)
	}
	idea := filepath.Join(dir, "Ideas.arcmd")
	os.Chtimes(idea, base.Add(-time.Hour), base.Add(-time.Hour))
	a.refreshTree()
	names := func() string {
		out := ""
		for _, n := range a.childrenOf("Journal") {
			out += n.Title() + ","
		}
		return out
	}
	sp := a.state.ActiveSpace()
	if err := tt.RightClick("Journal"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("排序方式", "按修改时间（新→旧）"); err != nil {
		t.Fatal(err, tt.Menu())
	}
	tt.Frame()
	if names() != "c,a,b,Ideas," {
		t.Fatal("newest first", names())
	}
	a.setSort(sp, "Journal", "name")
	if names() != "a,b,c,Ideas," {
		t.Fatal("by name", names())
	}
	if sp.SortOf("Journal") != "name" {
		t.Fatal("kept")
	}
}

func TestPaletteRecentCommandsAndConfirm(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	a.openPalette()
	a.palette.query = "开启打字机滚动"
	tt.Frame()
	a.runPaletteAction(a.results()[0])
	if !a.prefs.Typewriter || len(a.prefs.RecentCommands) != 1 || a.prefs.RecentCommands[0] != "开启打字机滚动" {
		t.Fatal("remembered", a.prefs.RecentCommands)
	}
	a.prefs.Typewriter = false
	a.openPalette()
	tt.Frame()
	idle := a.idleResults()
	first := -1
	for i, act := range idle {
		if act.kind == "命令" {
			first = i
			break
		}
	}
	if first < 0 || idle[first].title != "开启打字机滚动" {
		t.Fatal("the command used last comes first", idle)
	}
	// A dangerous command asks twice.
	ran := 0
	act := action{kind: "命令", title: "放弃修改并关闭", danger: true, run: func() { ran++ }}
	a.runPaletteAction(act)
	if ran != 0 || a.palette.confirm != act.title || !a.palette.open {
		t.Fatal("the first Enter asks")
	}
	a.runPaletteAction(act)
	if ran != 1 {
		t.Fatal("the second runs it")
	}
}
