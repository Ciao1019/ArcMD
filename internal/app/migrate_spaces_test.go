package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo/ui"
)

func TestDefaultConfigUsesHiddenArcmdSpaces(t *testing.T) {
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig failed: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}
	wantDocs := filepath.Join(home, ".arcmd", "spaces")
	wantLegacy := filepath.Join(home, "Documents", "ArcMD")
	if cfg.DocumentsDir != wantDocs {
		t.Fatalf("DocumentsDir = %q, want %q", cfg.DocumentsDir, wantDocs)
	}
	if cfg.LegacyDocumentsDir != wantLegacy {
		t.Fatalf("LegacyDocumentsDir = %q, want %q", cfg.LegacyDocumentsDir, wantLegacy)
	}
	if filepath.Base(cfg.ConfigDir) != "ArcMD" {
		t.Fatalf("ConfigDir = %q, want base ArcMD", cfg.ConfigDir)
	}
}

func TestMigrateLegacySpacesMovesFoldersAndPreservesState(t *testing.T) {
	base := t.TempDir()
	cfgDir := filepath.Join(base, "config")
	legacyDir := filepath.Join(base, "Documents", "ArcMD")
	newDir := filepath.Join(base, ".arcmd", "spaces")
	customRoot := filepath.Join(base, "Projects", "Notes")

	personalOld := filepath.Join(legacyDir, "Personal")
	ossOld := filepath.Join(legacyDir, "开源")
	for _, dir := range []string{
		filepath.Join(personalOld, ".arcmd", "history"),
		filepath.Join(personalOld, "assets"),
		ossOld,
		customRoot,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(personalOld, "Welcome.md"), []byte("# Welcome\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(personalOld, ".arcmd", "history", "snap.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(personalOld, "assets", "pic.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ossOld, "Notes.md"), []byte("# 开源\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(customRoot, "Custom.md"), []byte("# Custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := space.NewStore(filepath.Join(cfgDir, "spaces.json"))
	st := &space.State{Version: space.CurrentVersion}
	spPersonal := st.Add("Personal", personalOld)
	spPersonal.LastOpen = "Welcome.md"
	spPersonal.Pin("Welcome.md", 0)
	spOSS := st.Add("开源", ossOld)
	spOSS.LastOpen = "Notes.md"
	spCustom := st.Add("Custom", customRoot)
	ext := st.EnsureExternal("外部空间", filepath.Join(cfgDir, "external"))
	ext.AddExternal(filepath.Join(customRoot, "Custom.md"))
	st.ToggleFavorite(spPersonal.ID, "Welcome.md")
	st.Active = spOSS.ID
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		ConfigDir:          cfgDir,
		DocumentsDir:       newDir,
		LegacyDocumentsDir: legacyDir,
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	wantPersonal := filepath.Join(newDir, "Personal")
	wantOSS := filepath.Join(newDir, "开源")
	if got := a.state.Get(spPersonal.ID).Root; got != wantPersonal {
		t.Fatalf("personal Root = %q, want %q", got, wantPersonal)
	}
	if got := a.state.Get(spOSS.ID).Root; got != wantOSS {
		t.Fatalf("oss Root = %q, want %q", got, wantOSS)
	}
	if got := a.state.Get(spCustom.ID).Root; got != customRoot {
		t.Fatalf("custom Root = %q, want unchanged %q", got, customRoot)
	}
	if got := a.state.Get(space.ExternalID).Root; got != filepath.Join(cfgDir, "external") {
		t.Fatalf("external Root = %q", got)
	}

	// Files, hidden .arcmd history, and assets all moved intact.
	if data, err := os.ReadFile(filepath.Join(wantPersonal, "Welcome.md")); err != nil || string(data) != "# Welcome\n" {
		t.Fatalf("migrated Welcome.md = %q, err=%v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(wantPersonal, ".arcmd", "history", "snap.json")); err != nil || string(data) != `{"ok":true}` {
		t.Fatalf("migrated history = %q, err=%v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(wantPersonal, "assets", "pic.png")); err != nil || string(data) != "png" {
		t.Fatalf("migrated asset = %q, err=%v", data, err)
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatalf("expected empty legacyDir to be removed, stat err = %v", err)
	}
	if a.doc == nil || a.doc.rel != "Notes.md" || a.doc.loadErr != nil {
		t.Fatalf("active space LastOpen did not open cleanly after migration: doc=%+v", a.doc)
	}
	if !a.state.IsFavorite(spPersonal.ID, "Welcome.md") {
		t.Fatal("favorite lost after migration")
	}
	if len(a.deferredNotices) != 1 || !strings.Contains(a.deferredNotices[0].format, "已将原有空间目录迁移至") {
		t.Fatalf("expected migration deferred notice, got %+v", a.deferredNotices)
	}

	// Verify persisted spaces.json and idempotency on second launch.
	reloaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Get(spPersonal.ID).Root != wantPersonal || reloaded.Get(spOSS.ID).Root != wantOSS {
		t.Fatalf("spaces.json was not persisted with migrated roots")
	}

	a2, err := New(cfg)
	if err != nil {
		t.Fatalf("second New failed: %v", err)
	}
	if len(a2.deferredNotices) != 0 || len(a2.notices) != 0 {
		t.Fatalf("expected no migration notices on second launch, got notices=%v deferred=%+v", a2.notices, a2.deferredNotices)
	}
}

func TestMigrateLegacySpacesHandlesCollisionWithoutOverwriting(t *testing.T) {
	base := t.TempDir()
	cfgDir := filepath.Join(base, "config")
	legacyDir := filepath.Join(base, "Documents", "ArcMD")
	newDir := filepath.Join(base, ".arcmd", "spaces")

	oldRoot := filepath.Join(legacyDir, "Personal")
	existingDst := filepath.Join(newDir, "Personal")
	if err := os.MkdirAll(oldRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(existingDst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldRoot, "Old.md"), []byte("# Old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existingDst, "Existing.md"), []byte("# Existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := space.NewStore(filepath.Join(cfgDir, "spaces.json"))
	st := &space.State{Version: space.CurrentVersion}
	sp := st.Add("Personal", oldRoot)
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}

	a, err := New(Config{
		ConfigDir:          cfgDir,
		DocumentsDir:       newDir,
		LegacyDocumentsDir: legacyDir,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	wantRoot := filepath.Join(newDir, "Personal 2")
	if got := a.state.Get(sp.ID).Root; got != wantRoot {
		t.Fatalf("migrated Root = %q, want %q", got, wantRoot)
	}
	if data, err := os.ReadFile(filepath.Join(existingDst, "Existing.md")); err != nil || string(data) != "# Existing\n" {
		t.Fatalf("existing destination overwritten: data=%q err=%v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(wantRoot, "Old.md")); err != nil || string(data) != "# Old\n" {
		t.Fatalf("migrated file missing at numbered destination: data=%q err=%v", data, err)
	}
}

func TestMigrateLegacySpacesCrashRecovery(t *testing.T) {
	base := t.TempDir()
	cfgDir := filepath.Join(base, "config")
	legacyDir := filepath.Join(base, "Documents", "ArcMD")
	newDir := filepath.Join(base, ".arcmd", "spaces")

	oldRoot := filepath.Join(legacyDir, "Personal")
	newRoot := filepath.Join(newDir, "Personal")
	if err := os.MkdirAll(newRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newRoot, "Recovered.md"), []byte("# Recovered\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := space.NewStore(filepath.Join(cfgDir, "spaces.json"))
	st := &space.State{Version: space.CurrentVersion}
	sp := st.Add("Personal", oldRoot)
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}

	a, err := New(Config{
		ConfigDir:          cfgDir,
		DocumentsDir:       newDir,
		LegacyDocumentsDir: legacyDir,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if got := a.state.Get(sp.ID).Root; got != newRoot {
		t.Fatalf("recovered Root = %q, want %q", got, newRoot)
	}
}

func TestMigrateLegacySpacesCrashRecoveryDoesNotHijackClaimedSpace(t *testing.T) {
	base := t.TempDir()
	cfgDir := filepath.Join(base, "config")
	legacyDir := filepath.Join(base, "Documents", "ArcMD")
	newDir := filepath.Join(base, ".arcmd", "spaces")

	// Space A's old folder was deleted; Space B already owns newDir/Personal.
	oldMissing := filepath.Join(legacyDir, "Personal")
	claimedExisting := filepath.Join(newDir, "Personal")
	if err := os.MkdirAll(claimedExisting, 0o755); err != nil {
		t.Fatal(err)
	}
	// Space C has a real old folder "Work"; Space D already claims newDir/Work
	// in spaces.json even though newDir/Work does not yet exist on disk.
	oldWork := filepath.Join(legacyDir, "Work")
	claimedMissing := filepath.Join(newDir, "Work")
	if err := os.MkdirAll(oldWork, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldWork, "Task.md"), []byte("# Task\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := space.NewStore(filepath.Join(cfgDir, "spaces.json"))
	st := &space.State{Version: space.CurrentVersion}
	spA := st.Add("Old Personal", oldMissing)
	spB := st.Add("Current Personal", claimedExisting)
	spC := st.Add("Old Work", oldWork)
	spD := st.Add("Claimed Work", claimedMissing)
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}

	a, err := New(Config{
		ConfigDir:          cfgDir,
		DocumentsDir:       newDir,
		LegacyDocumentsDir: legacyDir,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if got := a.state.Get(spA.ID).Root; got != oldMissing {
		t.Fatalf("missing space A hijacked claimed folder: got %q, want %q", got, oldMissing)
	}
	if got := a.state.Get(spB.ID).Root; got != claimedExisting {
		t.Fatalf("space B root changed: got %q, want %q", got, claimedExisting)
	}
	wantWorkDst := filepath.Join(newDir, "Work 2")
	if got := a.state.Get(spC.ID).Root; got != wantWorkDst {
		t.Fatalf("space C collided with claimed space D: got %q, want %q", got, wantWorkDst)
	}
	if got := a.state.Get(spD.ID).Root; got != claimedMissing {
		t.Fatalf("space D root changed: got %q, want %q", got, claimedMissing)
	}
}

func TestMigrateLegacySpacesNestedAndDuplicateRoots(t *testing.T) {
	base := t.TempDir()
	cfgDir := filepath.Join(base, "config")
	legacyDir := filepath.Join(base, "Documents", "ArcMD")
	newDir := filepath.Join(base, ".arcmd", "spaces")

	workOld := filepath.Join(legacyDir, "Work")
	subOld := filepath.Join(workOld, "Sub")
	if err := os.MkdirAll(subOld, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subOld, "Doc.md"), []byte("# Sub\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := space.NewStore(filepath.Join(cfgDir, "spaces.json"))
	st := &space.State{Version: space.CurrentVersion}
	// Put child before parent intentionally to verify depth sorting.
	spSub := st.Add("Sub", subOld)
	spWork := st.Add("Work", workOld)
	spDup := st.Add("Work Copy", workOld)
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}

	a, err := New(Config{
		ConfigDir:          cfgDir,
		DocumentsDir:       newDir,
		LegacyDocumentsDir: legacyDir,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	wantWork := filepath.Join(newDir, "Work")
	wantSub := filepath.Join(wantWork, "Sub")
	if got := a.state.Get(spWork.ID).Root; got != wantWork {
		t.Fatalf("work Root = %q, want %q", got, wantWork)
	}
	if got := a.state.Get(spDup.ID).Root; got != wantWork {
		t.Fatalf("dup Root = %q, want %q", got, wantWork)
	}
	if got := a.state.Get(spSub.ID).Root; got != wantSub {
		t.Fatalf("sub Root = %q, want %q", got, wantSub)
	}
	if _, err := os.Stat(filepath.Join(wantSub, "Doc.md")); err != nil {
		t.Fatalf("nested file missing after migration: %v", err)
	}
}

func TestMigrateLegacySpacesKeepsUnrelatedFilesAndSkipsSymlink(t *testing.T) {
	base := t.TempDir()
	cfgDir := filepath.Join(base, "config")
	legacyDir := filepath.Join(base, "Documents", "ArcMD")
	newDir := filepath.Join(base, ".arcmd", "spaces")
	outside := filepath.Join(base, "Outside")

	realOld := filepath.Join(legacyDir, "Real")
	if err := os.MkdirAll(realOld, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	keepFile := filepath.Join(legacyDir, "keep.txt")
	if err := os.WriteFile(keepFile, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	linkOld := filepath.Join(legacyDir, "Linked")
	hasSymlink := false
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, linkOld); err == nil {
			hasSymlink = true
		}
	}

	store := space.NewStore(filepath.Join(cfgDir, "spaces.json"))
	st := &space.State{Version: space.CurrentVersion}
	spReal := st.Add("Real", realOld)
	var spLink *space.Space
	if hasSymlink {
		spLink = st.Add("Linked", linkOld)
	}
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}

	a, err := New(Config{
		ConfigDir:          cfgDir,
		DocumentsDir:       newDir,
		LegacyDocumentsDir: legacyDir,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if got := a.state.Get(spReal.ID).Root; got != filepath.Join(newDir, "Real") {
		t.Fatalf("real Root = %q", got)
	}
	if hasSymlink && a.state.Get(spLink.ID).Root != linkOld {
		t.Fatalf("symlink space should stay untouched, got %q", a.state.Get(spLink.ID).Root)
	}
	if data, err := os.ReadFile(keepFile); err != nil || string(data) != "keep" {
		t.Fatalf("unrelated file in legacyDir was lost: data=%q err=%v", data, err)
	}
}

func TestMigrateLegacySpacesBlocksSymlinkLegacyAndParentSegments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}

	// Case 1: LegacyDocumentsDir itself is a symlink to an external repo.
	t.Run("symlink legacy root", func(t *testing.T) {
		base := t.TempDir()
		cfgDir := filepath.Join(base, "config")
		outsideRepo := filepath.Join(base, "ExternalRepo")
		outsideSpace := filepath.Join(outsideRepo, "Personal")
		if err := os.MkdirAll(outsideSpace, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(outsideSpace, "Keep.md"), []byte("# Keep\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		docsParent := filepath.Join(base, "Documents")
		if err := os.MkdirAll(docsParent, 0o755); err != nil {
			t.Fatal(err)
		}
		legacyLink := filepath.Join(docsParent, "ArcMD")
		if err := os.Symlink(outsideRepo, legacyLink); err != nil {
			t.Fatal(err)
		}
		newDir := filepath.Join(base, ".arcmd", "spaces")

		store := space.NewStore(filepath.Join(cfgDir, "spaces.json"))
		st := &space.State{Version: space.CurrentVersion}
		oldRoot := filepath.Join(legacyLink, "Personal")
		sp := st.Add("Personal", oldRoot)
		if err := store.Save(st); err != nil {
			t.Fatal(err)
		}

		a, err := New(Config{ConfigDir: cfgDir, DocumentsDir: newDir, LegacyDocumentsDir: legacyLink})
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}
		if got := a.state.Get(sp.ID).Root; got != oldRoot {
			t.Fatalf("symlink legacy root was traversed and moved: got %q, want %q", got, oldRoot)
		}
		if _, err := os.Stat(filepath.Join(outsideSpace, "Keep.md")); err != nil {
			t.Fatalf("external repo content was moved out: %v", err)
		}
	})

	// Case 2: Intermediate directory is a symlink; nested child must not be ripped out.
	t.Run("symlink intermediate segment", func(t *testing.T) {
		base := t.TempDir()
		cfgDir := filepath.Join(base, "config")
		legacyDir := filepath.Join(base, "Documents", "ArcMD")
		newDir := filepath.Join(base, ".arcmd", "spaces")
		outsideTeam := filepath.Join(base, "CompanyRepo")
		outsideSub := filepath.Join(outsideTeam, "Docs")
		if err := os.MkdirAll(outsideSub, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(legacyDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(outsideSub, "Spec.md"), []byte("# Spec\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		teamLink := filepath.Join(legacyDir, "Team")
		if err := os.Symlink(outsideTeam, teamLink); err != nil {
			t.Fatal(err)
		}

		store := space.NewStore(filepath.Join(cfgDir, "spaces.json"))
		st := &space.State{Version: space.CurrentVersion}
		subRoot := filepath.Join(teamLink, "Docs")
		spParent := st.Add("Team", teamLink)
		spChild := st.Add("Docs", subRoot)
		if err := store.Save(st); err != nil {
			t.Fatal(err)
		}

		a, err := New(Config{ConfigDir: cfgDir, DocumentsDir: newDir, LegacyDocumentsDir: legacyDir})
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}
		if got := a.state.Get(spParent.ID).Root; got != teamLink {
			t.Fatalf("parent symlink changed: got %q", got)
		}
		if got := a.state.Get(spChild.ID).Root; got != subRoot {
			t.Fatalf("child inside symlink was ripped out: got %q, want %q", got, subRoot)
		}
		if _, err := os.Stat(filepath.Join(outsideSub, "Spec.md")); err != nil {
			t.Fatalf("company repo subfolder was moved out: %v", err)
		}
	})
}

func TestMigrateLegacySpacesPartialFailureNotifiesUser(t *testing.T) {
	base := t.TempDir()
	cfgDir := filepath.Join(base, "config")
	legacyDir := filepath.Join(base, "Documents", "ArcMD")
	newDir := filepath.Join(base, ".arcmd", "spaces")

	goodOld := filepath.Join(legacyDir, "Good")
	badOld := filepath.Join(legacyDir, "Blocked", "Child")
	if err := os.MkdirAll(goodOld, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(badOld, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Place a regular file at newDir/Blocked so os.MkdirAll(newDir/Blocked) fails
	// deterministically on all operating systems.
	if err := os.WriteFile(filepath.Join(newDir, "Blocked"), []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := space.NewStore(filepath.Join(cfgDir, "spaces.json"))
	st := &space.State{Version: space.CurrentVersion}
	spGood := st.Add("Good", goodOld)
	spBad := st.Add("Child", badOld)
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}

	a, err := New(Config{
		ConfigDir:          cfgDir,
		DocumentsDir:       newDir,
		LegacyDocumentsDir: legacyDir,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if got := a.state.Get(spGood.ID).Root; got != filepath.Join(newDir, "Good") {
		t.Fatalf("good space not migrated: got %q", got)
	}
	if got := a.state.Get(spBad.ID).Root; got != badOld {
		t.Fatalf("blocked space should stay at old root: got %q", got)
	}
	if len(a.deferredNotices) != 1 || !strings.Contains(a.deferredNotices[0].format, "部分空间已迁移") {
		t.Fatalf("expected partial-failure notice, got %+v", a.deferredNotices)
	}
}

func TestMigrateLegacySpacesDeferredNoticeTranslatesToEnglish(t *testing.T) {
	base := t.TempDir()
	cfgDir := filepath.Join(base, "config")
	legacyDir := filepath.Join(base, "Documents", "ArcMD")
	newDir := filepath.Join(base, ".arcmd", "spaces")
	oldRoot := filepath.Join(legacyDir, "Personal")
	if err := os.MkdirAll(oldRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	store := space.NewStore(filepath.Join(cfgDir, "spaces.json"))
	st := &space.State{Version: space.CurrentVersion}
	st.Add("Personal", oldRoot)
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}

	a, err := New(Config{
		ConfigDir:          cfgDir,
		DocumentsDir:       newDir,
		LegacyDocumentsDir: legacyDir,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	// Simulate Run() applying English system language after New().
	a.systemLanguage = "en"
	a.applyLanguage()
	defer func() {
		a.systemLanguage = "zh-Hans"
		a.applyLanguage()
	}()

	tt := ui.NewTester(a.View, 1100, 720)
	if !tt.HasText("Migrated existing spaces to") {
		t.Fatalf("expected English migration toast after system language switch")
	}
}

func TestHeadlessAppDoesNotMigrateLegacySpaces(t *testing.T) {
	base := t.TempDir()
	cfgDir := filepath.Join(base, "config")
	legacyDir := filepath.Join(base, "Documents", "ArcMD")
	newDir := filepath.Join(base, ".arcmd", "spaces")
	oldRoot := filepath.Join(legacyDir, "Personal")
	if err := os.MkdirAll(oldRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	store := space.NewStore(filepath.Join(cfgDir, "spaces.json"))
	st := &space.State{Version: space.CurrentVersion}
	sp := st.Add("Personal", oldRoot)
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}

	h, err := newHeadlessApp(Config{
		ConfigDir:          cfgDir,
		DocumentsDir:       newDir,
		LegacyDocumentsDir: legacyDir,
	})
	if err != nil {
		t.Fatalf("newHeadlessApp failed: %v", err)
	}
	if got := h.state.Get(sp.ID).Root; got != oldRoot {
		t.Fatalf("headless app unexpectedly migrated space: got %q, want %q", got, oldRoot)
	}
	if _, err := os.Stat(oldRoot); err != nil {
		t.Fatalf("legacy folder moved by headless app: %v", err)
	}
}

func TestMigrateLegacySpacesSkipsUnmatchedSpacesWithoutTouchingLegacy(t *testing.T) {
	base := t.TempDir()
	cfgDir := filepath.Join(base, "config")
	newDir := filepath.Join(base, ".arcmd", "spaces")
	legacyDir := filepath.Join(base, "Documents", "ArcMD")

	spaceRoot := filepath.Join(newDir, "Personal")
	if err := os.MkdirAll(spaceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	store := space.NewStore(filepath.Join(cfgDir, "spaces.json"))
	st := &space.State{Version: space.CurrentVersion}
	st.Add("Personal", spaceRoot)
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}

	a, err := New(Config{
		ConfigDir:          cfgDir,
		DocumentsDir:       newDir,
		LegacyDocumentsDir: legacyDir,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if len(a.deferredNotices) != 0 || len(a.notices) != 0 {
		t.Fatalf("unexpected notices: %v %+v", a.notices, a.deferredNotices)
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatalf("legacyDir should not be created or touched: err=%v", err)
	}
}

func TestRelUnderDirSafety(t *testing.T) {
	base := filepath.Join(t.TempDir(), "Documents", "ArcMD")
	if rel, ok := relUnderDir(base, filepath.Join(base, "Personal")); !ok || rel != "Personal" {
		t.Fatalf("expected Personal, got %q ok=%v", rel, ok)
	}
	if _, ok := relUnderDir(base, base); ok {
		t.Fatal("base itself must not match as strictly inside base")
	}
	if _, ok := relUnderDir(base, base+"2"); ok {
		t.Fatal("sibling prefix must not match")
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		// Kelvin sign (3 UTF-8 bytes) folds to ASCII 'k' (1 UTF-8 byte).
		kelvinBase := filepath.Join(t.TempDir(), "\u212a", "ArcMD")
		asciiTarget := filepath.Join(filepath.Dir(filepath.Dir(kelvinBase)), "k", "arcmd", "Notes")
		rel, ok := relUnderDir(kelvinBase, asciiTarget)
		if !ok || rel != "Notes" {
			t.Fatalf("Unicode case-fold segment match failed: rel=%q ok=%v", rel, ok)
		}
	}
}
