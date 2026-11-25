package dbview_test

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/dbview"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func saveDBViewSnapshot(t *testing.T, path string, tt *ui.Tester) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestDayflowSnapshots(t *testing.T) {
	dir := os.Getenv("ARCMD_SNAPSHOT")
	if dir == "" {
		t.Skip("set ARCMD_SNAPSHOT to render DayFlow UI screenshots")
	}

	root := t.TempDir()
	db := database.NewDefault("DayFlow 日程与研发", database.SourceConfig{Type: database.SourceFolder})
	db.Properties = append(db.Properties,
		database.Property{
			ID: "prop_priority", Key: "priority", Name: "优先级", Type: database.PropSelect,
			Options: []database.SelectOption{
				{ID: "p0", Name: "P0 紧急", Color: "red"},
				{ID: "p1", Name: "P1 高", Color: "orange"},
				{ID: "p2", Name: "P2 中", Color: "blue"},
			},
		},
		database.Property{ID: "prop_assignee", Key: "assignee", Name: "负责人", Type: database.PropPerson},
	)
	for i := range db.Views {
		db.Views[i].Properties = append(db.Views[i].Properties,
			database.ViewProperty{PropertyID: "prop_priority", Visible: true},
			database.ViewProperty{PropertyID: "prop_assignee", Visible: true},
		)
	}

	// Extra colorful status categories for DayFlow sidebar matching MonthView.png.
	for i := range db.Properties {
		if db.Properties[i].ID == "prop_status" {
			db.Properties[i].Options = []database.SelectOption{
				{ID: "st_team", Name: "产品团队", Color: "blue", Group: database.StatusInProgress},
				{ID: "st_learn", Name: "技术调研", Color: "purple", Group: database.StatusInProgress},
				{ID: "st_travel", Name: "外部交流", Color: "orange", Group: database.StatusTodo},
				{ID: "st_well", Name: "版本发布", Color: "green", Group: database.StatusComplete},
				{ID: "st_mkt", Name: "设计评审", Color: "pink", Group: database.StatusInProgress},
			}
		}
	}

	calView := dbview.NewViewConfig(database.ViewCalendar, "日历", db)
	boardView := dbview.NewViewConfig(database.ViewBoard, "看板", db)
	db.Views = append(db.Views, calView, boardView)

	rawDB, err := database.EncodeDatabaseMarkdown(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(root, "DayFlow 日程与研发.db.md", rawDB); err != nil {
		t.Fatal(err)
	}

	records := []struct{ file, body string }{
		{"e1.md", "---\nstatus: 产品团队\npriority: P0 紧急\nassignee:\n  - Alice\n  - 周默\ndate: 2026-11-18\ndate_end: 2026-11-21\n---\n\n# Q4 战略对齐会\n"},
		{"e2.md", "---\nstatus: 技术调研\npriority: P1 高\nassignee:\n  - 林晓\ndate: 2026-11-24T09:00\ndate_end: 2026-11-24T11:30\n---\n\n# 引擎架构头脑风暴\n"},
		{"e3.md", "---\nstatus: 产品团队\npriority: P0 紧急\nassignee:\n  - Alice\ndate: 2026-11-24T10:00\ndate_end: 2026-11-24T12:30\n---\n\n# 核心代码走查\n"},
		{"e4.md", "---\nstatus: 设计评审\npriority: P1 高\nassignee:\n  - 顾清\n  - Bob\ndate: 2026-11-24T14:00\ndate_end: 2026-11-24T16:30\n---\n\n# 交互视觉评审\n"},
		{"e5.md", "---\nstatus: 版本发布\npriority: P2 中\nassignee:\n  - 周默\ndate: 2026-11-25\ndate_end: 2026-11-28\n---\n\n# 研发冲刺周\n"},
		{"e6.md", "---\nstatus: 外部交流\npriority: P1 高\nassignee:\n  - 沈辞\ndate: 2026-11-26T09:30\ndate_end: 2026-11-26T11:00\n---\n\n# 客户深度访谈\n"},
		{"e7.md", "---\nstatus: 技术调研\npriority: P2 中\nassignee:\n  - 林晓\ndate: 2026-11-26\ndate_end: 2026-11-29\n---\n\n# 技术大会\n"},
		{"e8.md", "---\nstatus: 产品团队\npriority: P2 中\nassignee:\n  - Alice\ndate: 2026-11-27T15:00\ndate_end: 2026-11-27T17:00\n---\n\n# 每周同步会\n"},
	}
	for _, r := range records {
		if err := vault.Write(root, filepath.ToSlash(filepath.Join("DayFlow 日程与研发", r.file)), r.body); err != nil {
			t.Fatal(err)
		}
	}

	st, err := database.Open(root, "DayFlow 日程与研发.db.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	state := dbview.NewState(st)
	state.SetActiveView(calView.ID)
	now := time.Date(2026, 11, 24, 15, 36, 0, 0, time.UTC)

	dark := false
	tt := ui.NewTester(func(c *ui.Context) {
		if dark {
			c.SetTheme(ui.DarkTheme())
		} else {
			c.SetTheme(ui.LightTheme())
		}
		ui.Column(c.Key("host")).Fill().Background(c.Theme().Background).Children(func() {
			dbview.View(c, state, dbview.Options{Now: now})
		})
	}, 1220, 780)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	tt.Frame()
	saveDBViewSnapshot(t, filepath.Join(dir, "dayflow-cal-month-light.png"), tt)

	dark = true
	tt.Frame()
	tt.Advance(250 * time.Millisecond)
	tt.Frame()
	saveDBViewSnapshot(t, filepath.Join(dir, "dayflow-cal-month-dark.png"), tt)
	dark = false
	tt.Frame()
	tt.Advance(250 * time.Millisecond)
	tt.Frame()

	// Week view.
	_ = tt.Click("周")
	tt.Frame()
	tt.Frame()
	saveDBViewSnapshot(t, filepath.Join(dir, "dayflow-cal-week.png"), tt)

	// Day view.
	_ = tt.Click("日")
	tt.Frame()
	tt.Frame()
	saveDBViewSnapshot(t, filepath.Join(dir, "dayflow-cal-day.png"), tt)

	// Year (Fixed-week & Canvas).
	_ = tt.Click("年")
	tt.Frame()
	tt.Frame()
	saveDBViewSnapshot(t, filepath.Join(dir, "dayflow-cal-year-fixed.png"), tt)

	_ = tt.Click("连续画布")
	tt.Frame()
	tt.Frame()
	saveDBViewSnapshot(t, filepath.Join(dir, "dayflow-cal-year-canvas.png"), tt)

	// Month view with Detail Popup + Split DateTime picker expanded.
	_ = tt.Click("月")
	tt.Frame()
	tt.Frame()
	if err := tt.Click("引擎架构头脑风暴"); err != nil {
		t.Fatalf("click event for popup failed: %v", err)
	}
	tt.Frame()
	if err := tt.Click("开始时间"); err != nil {
		t.Fatalf("open datetime picker for popup failed: %v", err)
	}
	tt.Frame()
	tt.Frame()
	saveDBViewSnapshot(t, filepath.Join(dir, "dayflow-cal-popup.png"), tt)
	tt.Key(0, ui.KeyEscape)
	tt.Frame()

	// Board view.
	state.SetActiveView(boardView.ID)
	tt.Frame()
	tt.Frame()
	saveDBViewSnapshot(t, filepath.Join(dir, "dayflow-board-light.png"), tt)

	// beUI Motion Table views (Light & Dark, with selected row + active sort chevron + hover handle).
	state.SetActiveView(db.Views[0].ID)
	tt.Frame()
	_ = tt.Click("排期")
	tt.Advance(240 * time.Millisecond)
	tt.Frame()
	for _, info := range tt.AccessAll() {
		if info.Role == ui.RoleCheckBox && info.Name == "Q4 战略对齐会" {
			tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
			break
		}
	}
	tt.Advance(320 * time.Millisecond)
	tt.Frame()
	if rowBox, ok := tt.Find("引擎架构头脑风暴"); ok {
		tt.Move(rowBox.X+40, rowBox.Y+rowBox.H/2)
		tt.Advance(160 * time.Millisecond)
		tt.Frame()
	}
	saveDBViewSnapshot(t, filepath.Join(dir, "beui-table-light.png"), tt)

	dark = true
	tt.Frame()
	tt.Advance(160 * time.Millisecond)
	tt.Frame()
	saveDBViewSnapshot(t, filepath.Join(dir, "beui-table-dark.png"), tt)
}
