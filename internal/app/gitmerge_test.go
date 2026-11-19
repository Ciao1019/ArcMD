package app

import (
	"crypto/md5"
	"encoding/hex"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func setupSyncedWebDAVApp(t *testing.T) (*App, *ui.Tester, *appDAVServer) {
	t.Helper()
	dav, srv := newAppDAVServer(t, "alice", "app-pass")
	a, tt := newTestApp(t)
	a.pluginsUI.httpClient = srv.Client()
	a.trashFile = os.RemoveAll

	installCloudSyncViaUI(t, a, tt)
	if err := tt.Click("WebDAV"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	*a.pluginsUI.settings.strBufs["webdavURL"] = srv.URL + "/dav/"
	*a.pluginsUI.settings.strBufs["webdavUsername"] = "alice"
	*a.pluginsUI.settings.secretBufs["webdavPassword"] = "app-pass"
	*a.pluginsUI.settings.spaceBufs[a.state.ActiveSpace().ID] = true
	a.savePluginSettings()
	a.savePendingPluginSecrets()
	tt.Frame()

	// Close settings + library modals.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	return a, tt, dav
}

func TestGitChangesDiffViewerAndSkipExpand(t *testing.T) {
	a, tt, _ := setupSyncedWebDAVApp(t)
	sp := a.state.ActiveSpace()

	// Seed a multi-section document with >14 unchanged middle lines so UnifiedDiff produces a SkipBlockRow.
	var baseLines []string
	baseLines = append(baseLines, "# Architecture Overview", "Status: Draft")
	for i := 1; i <= 14; i++ {
		baseLines = append(baseLines, "Paragraph line "+string(rune('A'+i-1))+" — unchanged specification detail.")
	}
	baseLines = append(baseLines, "Footer: v0.1")
	if err := vault.Write(sp.Root, "Architecture.md", strings.Join(baseLines, "\n")+"\n"); err != nil {
		t.Fatal(err)
	}

	p := a.findInstalledPlugin(cloudSyncPluginID)
	a.runPluginCommand(p, "sync-now", nil, false)
	tt.Frame()

	// Modify top & bottom of Architecture.md and add Roadmap.md locally.
	modLines := append([]string(nil), baseLines...)
	modLines[1] = "Status: Approved for Release"
	modLines[len(modLines)-1] = "Footer: v1.0"
	if err := vault.Write(sp.Root, "Architecture.md", strings.Join(modLines, "\n")+"\n"); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Roadmap.md", "# Roadmap\n\n- 3-Way Merge\n"); err != nil {
		t.Fatal(err)
	}

	// Open Git Changes modal via top toolbar button.
	if err := tt.Click("变更与冲突"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	if !a.gitChanges.open || !tt.HasText("2 个文件变更") || !tt.HasText("修改") || !tt.HasText("新增") {
		t.Fatalf("unexpected Git Changes header/cards: %q", tt.Texts())
	}
	// Hidden middle lines are collapsed behind SkipBlockRow initially.
	if tt.HasText("Paragraph line G — unchanged specification detail.") {
		t.Fatal("expected middle unchanged lines to be collapsed initially")
	}
	if !tt.HasText("展开隐藏的 8 行未改动内容") {
		t.Fatalf("expected SkipBlockRow prompt, got: %q", tt.Texts())
	}

	// Expand SkipBlockRow and verify hidden lines appear.
	if err := tt.Click("展开隐藏的 8 行"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Paragraph line G — unchanged specification detail.") {
		t.Fatalf("SkipBlockRow did not expand middle context: %q", tt.Texts())
	}

	// Copy file path button copies path to clipboard.
	if err := tt.Click("复制路径 Architecture.md"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := tt.Clipboard(); got != "Architecture.md" {
		t.Fatalf("clipboard = %q, want Architecture.md", got)
	}

	// Collapsing the card hides its diff body.
	if err := tt.Click("折叠或展开 Architecture.md"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("Paragraph line G — unchanged specification detail.") {
		t.Fatal("expected diff body to hide after collapsing card")
	}
}

func TestThreeWayMergeCloudSyncConflict(t *testing.T) {
	a, tt, dav := setupSyncedWebDAVApp(t)
	sp := a.state.ActiveSpace()

	// 1. Write initial shared Base version of Welcome.md and sync to WebDAV.
	baseDoc := strings.Join([]string{
		"# Product Sync Plan",
		"",
		"Owner: Core Team",
		"",
		"## Milestone",
		"Target: Q3 Beta",
		"",
		"## Notes",
		"Keep calm and write docs.",
	}, "\n") + "\n"
	if err := vault.Write(sp.Root, "Welcome.md", baseDoc); err != nil {
		t.Fatal(err)
	}
	a.openIn(sp, "Welcome.md")
	a.checkDisk()
	tt.Frame()

	p := a.findInstalledPlugin(cloudSyncPluginID)
	a.runPluginCommand(p, "sync-now", nil, false)
	tt.Frame()

	// 2. Modify Section 1 (non-conflicting) + Section 2 (conflicting) locally.
	localDoc := strings.Join([]string{
		"# Product Sync Plan",
		"",
		"Owner: Core & Desktop Team",
		"",
		"## Milestone",
		"Target: October Local Cut",
		"",
		"## Notes",
		"Keep calm and write docs.",
	}, "\n") + "\n"
	if err := vault.Write(sp.Root, "Welcome.md", localDoc); err != nil {
		t.Fatal(err)
	}
	a.checkDisk()
	tt.Frame()

	// 3. Modify Section 2 (conflicting) + Section 3 (non-conflicting) remotely on WebDAV.
	remoteDoc := strings.Join([]string{
		"# Product Sync Plan",
		"",
		"Owner: Core Team",
		"",
		"## Milestone",
		"Target: November Cloud Release",
		"",
		"## Notes",
		"Synced across S3 and WebDAV.",
	}, "\n") + "\n"
	sum := md5.Sum([]byte(remoteDoc))
	dav.mu.Lock()
	dav.files["/dav/ArcMD/Personal/Welcome.md"] = appDAVObj{
		data:    []byte(remoteDoc),
		etag:    hex.EncodeToString(sum[:]),
		modTime: time.Now().Add(5 * time.Second).UTC(),
	}
	dav.mu.Unlock()

	// 4. Sync -> detects 3-way conflict, creates ZERO "(冲突副本)" files, and opens IDEA 3-Way Merge modal directly!
	a.runPluginCommand(p, "sync-now", nil, false)
	tt.Frame()

	matches, _ := filepath.Glob(filepath.Join(sp.Root, "Welcome (冲突副本*).md"))
	if len(matches) != 0 {
		t.Fatalf("expected ZERO conflict copy files on disk, got %v", matches)
	}
	if !a.mergeUI.open || a.mergeUI.session == nil {
		t.Fatal("3-way merge modal did not open automatically upon sync conflict")
	}
	if !tt.HasText("剩余 1 处冲突") {
		t.Fatalf("expected exactly 1 unresolved conflict (non-conflicting sections auto-applied), got: %q", tt.Texts())
	}
	// Non-conflicting edits from both sides are already in ResultText!
	initialMerged := a.mergeUI.session.ResultText()
	if !strings.Contains(initialMerged, "Owner: Core & Desktop Team") || !strings.Contains(initialMerged, "Synced across S3 and WebDAV.") {
		t.Fatalf("non-conflicting changes were not auto-merged:\n%s", initialMerged)
	}

	// 5. Accept Yours (») then Append Theirs («) on the conflicting Milestone chunk!
	if err := tt.Click("采纳左侧修改"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("✓ 所有冲突已解决") {
		t.Fatalf("conflict badge should turn resolved after accepting left: %q", tt.Texts())
	}
	if err := tt.Click("追加右侧修改（保留双方）"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	// 6. Commit merge & save -> writes merged document locally, clears conflicts.json, AND pushes merged result to WebDAV!
	if err := tt.Click("完成合并并保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	if a.mergeUI.open {
		t.Fatal("merge modal remained open after save")
	}
	saved, err := vault.Read(sp.Root, "Welcome.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Owner: Core & Desktop Team",
		"Target: October Local Cut",
		"Target: November Cloud Release",
		"Synced across S3 and WebDAV.",
	} {
		if !strings.Contains(saved, want) {
			t.Fatalf("saved Welcome.md missing %q:\n%s", want, saved)
		}
	}
	if len(a.loadPersistedConflicts()) != 0 {
		t.Fatalf("persisted conflicts not cleared: %+v", a.loadPersistedConflicts())
	}

	// Verify WebDAV remote ALSO received the merged result automatically!
	dav.mu.Lock()
	remAfter := string(dav.files["/dav/ArcMD/Personal/Welcome.md"].data)
	dav.mu.Unlock()
	if !strings.Contains(remAfter, "Target: October Local Cut") || !strings.Contains(remAfter, "Target: November Cloud Release") {
		t.Fatalf("merged document was not pushed back to WebDAV:\n%s", remAfter)
	}
}

func TestThreeWayMergeDiskConflict(t *testing.T) {
	a, tt, w := watchApp(t)
	// Base is initial Welcome.md. Make an unsaved edit in editor, and write conflicting edit on disk.
	a.doc.ed.SetSelection(0, 0)
	a.doc.ed.ReplaceText(nil, "Local Header Note\n\n")
	tt.Frame()

	writeElsewhere(t, a, "Welcome.md", "# Welcome\n\nEdited externally by another tool.\n")
	w.fire(t, a, tt, written("Welcome.md"))

	if !a.doc.conflict || !tt.HasText("三栏合并…") {
		t.Fatalf("disk conflict bar with 3-way merge button not shown: conflict=%v texts=%q", a.doc.conflict, tt.Texts())
	}

	if err := tt.Click("三栏合并…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.mergeUI.open {
		t.Fatal("3-way merge modal did not open for disk conflict")
	}

	// Resolve any remaining conflict by accepting left then appending right if conflict exists.
	if a.mergeUI.session.UnresolvedConflicts() > 0 {
		_ = tt.Click("采纳左侧修改")
		tt.Frame()
		_ = tt.Click("追加右侧修改（保留双方）")
		tt.Frame()
	}

	if err := tt.Click("完成合并并保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	if a.doc.conflict {
		t.Fatal("document conflict flag was not cleared after 3-way merge save")
	}
	diskAfter, err := vault.Read(root(a), "Welcome.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diskAfter, "Local Header Note") || !strings.Contains(diskAfter, "Edited externally by another tool.") {
		t.Fatalf("merged disk document missing expected content:\n%s", diskAfter)
	}
}

func TestGitMergeSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render Git Diff & 3-Way Merge snapshots")

	saveShot := func(tt *ui.Tester, name string) {
		t.Helper()
		for range 6 {
			tt.Frame()
			tt.Advance(200 * time.Millisecond)
		}
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}

	a, tt, dav := setupSyncedWebDAVApp(t)
	tt.SetSize(1280, 800)
	if sp := a.state.ActiveSpace(); sp != nil {
		sp.SetTheme(space.Theme{
			Colors:     []string{"#EAB8C2", "#F1D4C2"},
			Intensity:  0.72,
			Noise:      0.20,
			Appearance: space.AppearanceLight,
		})
		a.paperDark = false
	}

	sp := a.state.ActiveSpace()
	baseArch := strings.Join([]string{
		"# Cloud Sync & Merge Architecture",
		"",
		"Transport: S3 SigV4 + WebDAV",
		"Sandbox: wazero pure-Go WASM (CGO_ENABLED=0)",
		"",
		"## Storage Layout",
		"- Manifest stored in plugin state directory",
		"- Credentials isolated in OS Keychain / 0600 store",
		"- Unsynced drafts stay in memory",
		"- Deleted files move into 30-day Trashbin",
		"- Conflict copies keep both sides intact",
		"- Snapshots enable 3-way structural merge",
		"- Atomic rename prevents partial writes",
		"- Deterministic line-level LCS anchors",
		"- Grapheme-level intra-line spans",
		"",
		"## Merge Strategy",
		"Mode: Manual copy inspection",
	}, "\n") + "\n"
	_ = vault.Write(sp.Root, "Architecture.md", baseArch)

	baseWelcome := strings.Join([]string{
		"# Sprint 14 Release Notes",
		"",
		"Lead: Design & Core Runtime",
		"Status: In Review",
		"",
		"## Highlights",
		"- Translucent frosted glass plugin cards",
		"- Incremental S3 and WebDAV sync",
		"",
		"## Conflict Resolution",
		"- Preserve conflict copy on divergence",
		"",
		"## Verification",
		"- macOS, Linux, and Windows builds pass",
	}, "\n") + "\n"
	_ = vault.Write(sp.Root, "Welcome.md", baseWelcome)
	a.openIn(sp, "Welcome.md")
	a.checkDisk()
	tt.Frame()

	p := a.findInstalledPlugin(cloudSyncPluginID)
	a.runPluginCommand(p, "sync-now", nil, false)
	tt.Frame()

	// Create 3-way conflict on Welcome.md (local edits Status + Conflict Resolution; remote edits Conflict Resolution + Verification).
	localWelcome := strings.Replace(baseWelcome, "Status: In Review", "Status: Ready to Ship", 1)
	localWelcome = strings.Replace(localWelcome, "- Preserve conflict copy on divergence", "- Interactive 3-pane merge with Bezier ribbons\n- Clickable » / « chunk actions", 1)
	_ = vault.Write(sp.Root, "Welcome.md", localWelcome)
	a.checkDisk()

	remoteWelcome := strings.Replace(baseWelcome, "- Preserve conflict copy on divergence", "- Automatic non-conflicting hunk merge\n- Move resolved conflict copies to Trashbin", 1)
	remoteWelcome = strings.Replace(remoteWelcome, "- macOS, Linux, and Windows builds pass", "- macOS, Linux, Windows, and WASM builds pass", 1)
	sum := md5.Sum([]byte(remoteWelcome))
	dav.mu.Lock()
	dav.files["/dav/ArcMD/Personal/Welcome.md"] = appDAVObj{
		data:    []byte(remoteWelcome),
		etag:    hex.EncodeToString(sum[:]),
		modTime: time.Now().Add(5 * time.Second).UTC(),
	}
	dav.mu.Unlock()
	a.runPluginCommand(p, "sync-now", nil, false)

	// Modify Architecture.md locally after sync so Git Changes shows both Conflict + Modified (with SkipBlockRow).
	modArch := strings.Replace(baseArch, "Transport: S3 SigV4 + WebDAV", "Transport: S3 SigV4, R2, MinIO + WebDAV", 1)
	modArch = strings.Replace(modArch, "Mode: Manual copy inspection", "Mode: IntelliJ IDEA 3-Way Merge + Git Diff", 1)
	_ = vault.Write(sp.Root, "Architecture.md", modArch)
	a.undos = nil
	a.notices = nil
	tt.Frame()

	// Snapshot 1: Git Changes & Diff Viewer (fredrikalindh/ui style — collapse 1st card slightly or show both).
	a.mergeUI.open = false
	a.openGitChanges()
	if len(a.gitChanges.items) > 1 {
		a.gitChanges.items[0].Collapsed = true
	}
	saveShot(tt, "git-changes-diff.png")

	// Snapshot 2: IDEA 3-Way Merge (Light).
	a.gitChanges.open = false
	if cf, ok := a.findConflictForDoc(a.doc); ok {
		a.openMergeForConflict(cf)
	}
	saveShot(tt, "idea-three-way-merge.png")

	// Snapshot 3: IDEA 3-Way Merge (Dark).
	tt.SetDark(true)
	if sp != nil {
		sp.SetTheme(space.Theme{
			Colors:     []string{"#EAB8C2", "#F1D4C2"},
			Intensity:  0.72,
			Noise:      0.20,
			Appearance: space.AppearanceDark,
		})
		a.paperDark = true
	}
	saveShot(tt, "idea-three-way-merge-dark.png")
}
