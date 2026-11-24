package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestNewModalCreateDocumentDatabaseAndNamed(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1200, 760)
	tt.Frame()

	// 1. Clicking "+ 新建" opens the Cra-style creation modal listing Document, Database, Whiteboard, Folder.
	if err := tt.Click("新建"); err != nil {
		t.Fatalf("click 新建 failed: %v", err)
	}
	tt.Frame()
	if !a.newModal.open {
		t.Fatal("expected newModal open")
	}
	for _, want := range []string{"新建文档", "新建数据库", "新建白板", "新建文件夹"} {
		if !tt.HasText(want) {
			t.Fatalf("expected %q in creation modal, texts=%v", want, tt.Texts())
		}
	}

	// 2. Typing keyword "db" filters to database creation and Enter creates & opens it.
	a.newModal.query = "db"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.newModal.open {
		t.Fatal("expected newModal closed after Enter")
	}
	if a.doc == nil || !a.doc.isDatabase() {
		t.Fatalf("expected new database created and opened, got %+v", a.doc)
	}

	// 3. Typing a custom title offers named creation across core types and creates a named database.
	a.openNewModal("")
	a.newModal.query = "Q4 产品规划"
	tt.Frame()
	if !tt.HasText("新建数据库「Q4 产品规划」") {
		t.Fatalf("expected named database option, texts=%v", tt.Texts())
	}
	if err := tt.Click("新建数据库「Q4 产品规划」"); err != nil {
		t.Fatalf("click named database option failed: %v", err)
	}
	tt.Frame()
	if a.doc == nil || !vault.IsDatabase(a.doc.rel) || vault.Title(a.doc.rel) != "Q4 产品规划" {
		t.Fatalf("expected Q4 产品规划.db.md opened, got %+v", a.doc)
	}

	// 4. Escape dismisses the modal without creating anything.
	prevRel := a.doc.rel
	a.openNewModal("")
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.newModal.open {
		t.Fatal("expected Escape to close newModal")
	}
	if a.doc.rel != prevRel {
		t.Fatalf("Escape changed open doc from %q to %q", prevRel, a.doc.rel)
	}
}

func TestNewModalSnapshot(t *testing.T) {
	dir := os.Getenv("ARCMD_SNAPSHOT")
	if dir == "" {
		t.Skip("set ARCMD_SNAPSHOT to render new modal")
	}
	a, tt := newTestApp(t)
	tt.SetSize(1200, 760)
	a.openNewModal("")
	tt.Frame()
	tt.Frame()
	savePNG(t, filepath.Join(dir, "new-modal-light.png"), tt)

	if sp := a.state.ActiveSpace(); sp != nil {
		sp.Theme.Appearance = space.AppearanceDark
	}
	a.paperDark = true
	tt.Frame()
	tt.Frame()
	savePNG(t, filepath.Join(dir, "new-modal-dark.png"), tt)
}
