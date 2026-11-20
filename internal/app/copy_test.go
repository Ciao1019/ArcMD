package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestSidebarCopyPasteDocumentAndUndo(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	var trashed []string
	a.trashFile = func(p string) error {
		trashed = append(trashed, p)
		return os.RemoveAll(p)
	}
	if err := vault.Write(sp.Root, "会议纪要模板.md", "# 会议纪要\n\n- 议题\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	settle(tt)

	// Before copying anything, Paste is shown in the context menu but disabled.
	if err := tt.RightClick("会议纪要模板"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tt.Menu(), "复制") || !slices.Contains(tt.Menu(), "创建副本") || !slices.Contains(tt.Menu(), "粘贴") {
		t.Fatalf("expected copy/duplicate/paste items in menu: %q", tt.Menu())
	}
	if err := tt.ChooseMenuItem("复制"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if len(a.copied) != 1 || a.copied[0] != (place{sp.ID, "会议纪要模板.md"}) {
		t.Fatalf("unexpected clipboard after Copy: %+v", a.copied)
	}

	// Paste via document row context menu places the copy beside it in the same
	// folder while keeping the currently open document (Welcome.md) unchanged.
	if err := tt.RightClick("会议纪要模板"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("粘贴"); err != nil {
		t.Fatalf("Paste failed: %v; menu=%q", err, tt.Menu())
	}
	settle(tt)

	if a.doc == nil || a.doc.rel != "Welcome.md" {
		t.Fatalf("Paste should not switch active document, got %+v", a.doc)
	}
	got, err := vault.Read(sp.Root, "会议纪要模板 2.md")
	if err != nil || got != "# 会议纪要\n\n- 议题\n" {
		t.Fatalf("pasted copy content = %q, err = %v", got, err)
	}
	// Manual order places the pasted copy directly after the anchor row.
	names := rootNames(a)
	idxOrig := slices.Index(names, "会议纪要模板.md")
	idxCopy := slices.Index(names, "会议纪要模板 2.md")
	if idxOrig < 0 || idxCopy != idxOrig+1 {
		t.Fatalf("expected pasted copy directly after original, order=%v", names)
	}

	// Let the first paste's undo toast expire so the second paste's Undo toast
	// is the sole active action button.
	tt.Advance(undoTimeout + 100*time.Millisecond)
	tt.Frame()

	// Paste into the Journal folder via folder context menu.
	sp.SetExpanded("Journal", false)
	tt.Frame()
	if err := tt.RightClick("Journal"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("粘贴"); err != nil {
		t.Fatalf("Paste into folder failed: %v; menu=%q", err, tt.Menu())
	}
	settle(tt)
	if !sp.IsExpanded("Journal") {
		t.Fatal("pasting into a collapsed folder should expand it")
	}
	if text, err := vault.Read(sp.Root, "Journal/会议纪要模板.md"); err != nil || text != "# 会议纪要\n\n- 议题\n" {
		t.Fatalf("folder paste content = %q, err = %v", text, err)
	}

	// Clicking Undo on the toast trashes the newly pasted copy cleanly.
	if err := tt.Click("撤销"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if _, err := os.Stat(filepath.Join(sp.Root, "Journal", "会议纪要模板.md")); !os.IsNotExist(err) {
		t.Fatalf("undo did not remove pasted copy: %v", err)
	}
	if len(trashed) == 0 {
		t.Fatal("undo should route deletion through trashItem")
	}
	// Original and earlier copy remain untouched.
	if _, err := vault.Read(sp.Root, "会议纪要模板.md"); err != nil {
		t.Fatalf("original missing after undo: %v", err)
	}
	if _, err := vault.Read(sp.Root, "会议纪要模板 2.md"); err != nil {
		t.Fatalf("earlier copy missing after undo: %v", err)
	}
}

func TestSidebarDuplicateDocumentRenameAndOpen(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	var trashed []string
	a.trashFile = func(p string) error {
		trashed = append(trashed, p)
		return os.RemoveAll(p)
	}
	if err := vault.Write(sp.Root, "会议纪要模板.md", "# 会议纪要模板\n\n- 参会人\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	settle(tt)

	// Right-click -> 创建副本 creates a numbered copy and immediately focuses inline rename.
	if err := tt.RightClick("会议纪要模板"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("创建副本"); err != nil {
		t.Fatalf("Duplicate failed: %v; menu=%q", err, tt.Menu())
	}
	tt.Frame()
	if a.renaming != "会议纪要模板 2.md" || !tt.Focused("名称") {
		t.Fatalf("expected inline rename on duplicated doc, renaming=%q focused=%v", a.renaming, tt.Focused("名称"))
	}
	// Waiting longer than undoTimeout while typing the new name must not expire
	// the Undo toast prematurely: the toast appears once rename finishes.
	tt.Advance(undoTimeout + time.Second)
	tt.Frame()
	if tt.HasText("撤销") {
		t.Fatal("undo toast should wait until inline rename finishes")
	}
	// Confirming a new name with Enter renames the duplicate, opens it with caret
	// in the body (not the heading), and emits a fresh Undo toast with the new name.
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("周会 2026-11-19")
	tt.Key(0, ui.KeyEnter)
	settle(tt)

	if a.doc == nil || a.doc.rel != "周会 2026-11-19.md" {
		t.Fatalf("expected duplicated doc to open after Enter, got %+v", a.doc)
	}
	if !a.doc.ed.Focused() {
		t.Fatal("expected editor to take focus after opening confirmed duplicate")
	}
	if !tt.HasText("撤销") || !tt.HasText("周会 2026-11-19") {
		t.Fatalf("expected fresh Undo toast showing renamed title, texts=%q", tt.Texts())
	}
	// Typing immediately appends to the body rather than prepending to the H1 title.
	tt.Type("：张三")
	tt.Frame()
	if gotTitle := a.doc.ed.Doc.Blocks[0].Text(); gotTitle != "会议纪要模板" {
		t.Fatalf("typing after duplicate Enter modified heading: %q", gotTitle)
	}
	if gotBody := a.doc.ed.Doc.Blocks[1].Text(); gotBody != "参会人：张三" {
		t.Fatalf("typing after duplicate Enter did not land in body: %q", gotBody)
	}

	// Undo still works cleanly after inline rename (and flushes before trashing).
	if err := tt.Click("撤销"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if _, err := os.Stat(filepath.Join(sp.Root, "周会 2026-11-19.md")); !os.IsNotExist(err) {
		t.Fatalf("undo after inline rename did not trash renamed duplicate: %v", err)
	}
	if len(trashed) == 0 {
		t.Fatal("expected trashItem to be called on duplicate undo")
	}

	// Duplicate again and press Escape once: cancels rename immediately, keeps the
	// default numbered copy without switching open doc, and shows an Undo toast.
	a.open("Welcome.md")
	tt.Advance(undoTimeout + time.Second)
	settle(tt)
	if err := tt.RightClick("会议纪要模板"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("创建副本"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	settle(tt)
	if a.renaming != "" || a.openAfterRename != "" {
		t.Fatalf("single Escape did not cancel duplicate rename: renaming=%q openAfter=%q", a.renaming, a.openAfterRename)
	}
	if a.doc == nil || a.doc.rel != "Welcome.md" {
		t.Fatalf("canceling duplicate rename should not switch open doc, got %+v", a.doc)
	}
	if !tt.HasText("撤销") {
		t.Fatal("canceling duplicate rename should still emit an Undo toast")
	}
	if _, err := vault.Read(sp.Root, "会议纪要模板 2.md"); err != nil {
		t.Fatalf("duplicate missing after Escape: %v", err)
	}

	// Duplicate again and press Enter without changing the name: opens the numbered copy.
	if err := tt.RightClick("会议纪要模板"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("创建副本"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	settle(tt)
	if a.doc == nil || a.doc.rel != "会议纪要模板 3.md" {
		t.Fatalf("Enter without editing name should open duplicate, got %+v", a.doc)
	}
}

func TestSidebarCopyPasteFolderAndSelfPasteGuard(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	if err := os.MkdirAll(filepath.Join(sp.Root, "Target"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Journal/Sub/Note.md", "# Nested\n"); err != nil {
		t.Fatal(err)
	}
	sp.SetExpanded("Journal", true)
	sp.SetExpanded("Journal/Sub", true)
	a.refreshTree()
	settle(tt)

	// Copy the Journal folder.
	if err := tt.RightClick("Journal"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("复制"); err != nil {
		t.Fatal(err)
	}
	settle(tt)

	// Pasting Journal into itself or into Journal/Sub must be disallowed.
	if a.canPasteInto(sp, "Journal") {
		t.Fatal("should not allow pasting a folder into itself")
	}
	if a.canPasteInto(sp, "Journal/Sub") {
		t.Fatal("should not allow pasting a folder into its descendant")
	}
	if !a.canPasteInto(sp, "Target") || !a.canPasteInto(sp, "") {
		t.Fatal("should allow pasting folder into another folder or root")
	}

	// Paste Journal into Target.
	if err := tt.RightClick("Target"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("粘贴"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if text, err := vault.Read(sp.Root, "Target/Journal/Ideas.md"); err != nil || !strings.Contains(text, "# Ideas") {
		t.Fatalf("copied folder doc = %q, err = %v", text, err)
	}
	if text, err := vault.Read(sp.Root, "Target/Journal/Sub/Note.md"); err != nil || text != "# Nested\n" {
		t.Fatalf("copied nested folder doc = %q, err = %v", text, err)
	}

	// Duplicate a folder enters inline rename without hijacking open doc.
	a.open("Welcome.md")
	settle(tt)
	if err := tt.RightClick("Target"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("创建副本"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.renaming != "Target 2" || a.openAfterRename != "" {
		t.Fatalf("folder duplicate rename state invalid: renaming=%q openAfter=%q", a.renaming, a.openAfterRename)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Backup")
	tt.Key(0, ui.KeyEnter)
	settle(tt)
	if a.doc == nil || a.doc.rel != "Welcome.md" {
		t.Fatalf("folder duplicate should not change open doc, got %+v", a.doc)
	}
	if _, err := vault.Read(sp.Root, "Backup/Journal/Ideas.md"); err != nil {
		t.Fatalf("duplicated folder missing contents: %v", err)
	}
}

func TestSidebarMultiSelectCopyPasteAndUndo(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	var trashed []string
	a.trashFile = func(p string) error {
		trashed = append(trashed, p)
		return os.RemoveAll(p)
	}
	if err := os.MkdirAll(filepath.Join(sp.Root, "Dest"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Alpha.md", "# Alpha\n"); err != nil {
		t.Fatal(err)
	}
	sp.SetExpanded("Journal", true)
	a.refreshTree()
	settle(tt)

	// Pick Alpha.md, Journal, and Journal/Ideas.md (child of Journal) along with Welcome.md.
	// Copy should deduplicate Journal/Ideas.md so 3 top-level items are copied.
	for _, label := range []string{"Alpha", "Journal", "Ideas"} {
		if err := tt.ClickWith(ui.Cmd, label); err != nil {
			t.Fatal(err)
		}
	}
	if err := tt.RightClick("Alpha"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("复制"); err != nil {
		t.Fatalf("multi-select Copy failed: %v; menu=%q", err, tt.Menu())
	}
	settle(tt)
	if len(a.copied) != 3 {
		t.Fatalf("expected 3 deduplicated copied items, got %+v", a.copied)
	}

	// Right-click Dest shows "粘贴 3 项".
	if err := tt.RightClick("Dest"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("粘贴 3 项"); err != nil {
		t.Fatalf("multi-paste failed: %v; menu=%q", err, tt.Menu())
	}
	settle(tt)
	for _, rel := range []string{"Dest/Alpha.md", "Dest/Welcome.md", "Dest/Journal/Ideas.md"} {
		if _, err := vault.Read(sp.Root, rel); err != nil {
			t.Fatalf("expected %s after multi-paste: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(sp.Root, "Dest", "Ideas.md")); !os.IsNotExist(err) {
		t.Fatal("child Ideas.md should not be flattened out of Journal")
	}

	// Single Undo click trashes all 3 pasted copies.
	if err := tt.Click("撤销"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	for _, rel := range []string{"Dest/Alpha.md", "Dest/Welcome.md", "Dest/Journal"} {
		if _, err := os.Stat(filepath.Join(sp.Root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("expected %s removed by batch paste undo", rel)
		}
	}
	if len(trashed) != 3 {
		t.Fatalf("expected 3 trashed items on undo, got %d", len(trashed))
	}
}

func TestSidebarCopyPasteCrossSpaceAttachmentsAndExternal(t *testing.T) {
	a, _ := dropApp(t)
	src := a.state.ActiveSpace()
	img, data := pngFile(t, "diagram.png")
	a.insertImages([]string{img}, -1)
	a.doc.ed.Engine.SetCover("assets/diagram.png")
	// Leave an unsaved edit to confirm copy flushes before reading disk.
	a.doc.ed.SetSelection(0, 0)
	a.doc.ed.ReplaceText(nil, "Fresh ")

	// Destination space with a dedicated attachments folder and a colliding asset.
	dst := a.state.Add("Work", t.TempDir())
	if !dst.SetAttachmentsDir("attachments") {
		t.Fatal("failed to set destination attachmentsDir")
	}
	if err := vault.Write(dst.Root, "attachments/diagram.png", "different bytes"); err != nil {
		t.Fatal(err)
	}

	a.copyEntries(src, []string{a.doc.rel})
	a.pasteInto(dst, "", "")

	// Original document and original asset remain untouched and still in src.
	if _, err := vault.Read(src.Root, "Drop.arcmd"); err != nil {
		t.Fatalf("original missing: %v", err)
	}
	if got, _ := vault.Read(src.Root, "assets/diagram.png"); got != string(data) {
		t.Fatal("original image modified or removed")
	}
	// Existing colliding asset in dst must not be overwritten.
	if got, _ := vault.Read(dst.Root, "attachments/diagram.png"); got != "different bytes" {
		t.Fatal("existing destination attachment overwritten")
	}

	// Copied legacy .arcmd has fresh block IDs, latest flushed text, and rebased
	// cover/image URLs pointing to the numbered attachment in dst/attachments/.
	origText, _ := vault.Read(src.Root, "Drop.arcmd")
	origDoc, err := model.Decode(origText)
	if err != nil {
		t.Fatal(err)
	}
	copyText, err := vault.Read(dst.Root, "Drop.arcmd")
	if err != nil {
		t.Fatalf("copied doc missing in dst: %v", err)
	}
	copyDoc, err := model.Decode(copyText)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(copyDoc.Blocks[0].Text(), "Fresh ") {
		t.Fatalf("copied doc missed flushed edits: %q", copyDoc.Blocks[0].Text())
	}
	if origDoc.Blocks[0].ID == copyDoc.Blocks[0].ID {
		t.Fatal("copied legacy .arcmd kept original block ID")
	}
	if copyDoc.Cover == "" || copyDoc.Cover == "attachments/diagram.png" || !strings.HasPrefix(copyDoc.Cover, "attachments/diagram-") {
		t.Fatalf("unexpected rebased cover URL: %q", copyDoc.Cover)
	}
	if got, err := vault.Read(dst.Root, copyDoc.Cover); err != nil || got != string(data) {
		t.Fatalf("rebased cover asset missing or mismatched: err=%v", err)
	}

	// Same-space cross-folder paste with shared attachmentsDir rebases relative URL
	// without duplicating the image file.
	if err := os.MkdirAll(filepath.Join(dst.Root, "Sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.copyEntries(dst, []string{"Drop.arcmd"})
	a.pasteInto(dst, "Sub", "")
	subText, err := vault.Read(dst.Root, "Sub/Drop.arcmd")
	if err != nil {
		t.Fatalf("same-space subfolder paste missing: %v", err)
	}
	subDoc, err := model.Decode(subText)
	if err != nil {
		t.Fatal(err)
	}
	// Image blocks rebase to a document-relative path (../attachments/...),
	// while Cover keeps a ValidCover space-relative attachment path that
	// resolveSpaceImage still resolves cleanly without duplicating the asset.
	var imgURL string
	for _, b := range subDoc.Blocks {
		if b.Kind == "image" {
			imgURL = b.URL
		}
	}
	if imgURL != "../"+copyDoc.Cover {
		t.Fatalf("expected relative shared attachment image URL %q, got %q", "../"+copyDoc.Cover, imgURL)
	}
	if subDoc.Cover != copyDoc.Cover {
		t.Fatalf("expected ValidCover shared attachment cover %q, got %q", copyDoc.Cover, subDoc.Cover)
	}
	if _, err := resolveSpaceImage(dst, "Sub/Drop.arcmd", subDoc.Cover); err != nil {
		t.Fatalf("cover failed to resolve from subfolder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst.Root, "Sub", "assets")); !os.IsNotExist(err) {
		t.Fatal("shared attachment was unexpectedly copied into Sub/assets")
	}

	// Copy resource failure rolls back the newly created file cleanly.
	broken := a.state.Add("Broken", t.TempDir())
	if err := vault.Write(broken.Root, "assets", "not a directory"); err != nil {
		t.Fatal(err)
	}
	a.copyEntries(src, []string{"Drop.arcmd"})
	a.pasteInto(broken, "", "")
	if _, err := os.Stat(filepath.Join(broken.Root, "Drop.arcmd")); !os.IsNotExist(err) {
		t.Fatalf("failed copy was not rolled back: %v", err)
	}

	// External space: cannot paste or duplicate into External, but copying FROM
	// External into a normal space succeeds.
	extDir := t.TempDir()
	extFile := filepath.Join(extDir, "Spec.md")
	if err := os.WriteFile(extFile, []byte("# External Spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !a.openSystemFile(extFile) {
		t.Fatal("openSystemFile failed")
	}
	ext := a.state.Get("external")
	if ext == nil || !ext.IsExternal() {
		t.Fatal("missing external space")
	}
	if a.canPasteInto(ext, "") {
		t.Fatal("External space must not accept pastes")
	}
	a.copyEntries(ext, []string{a.doc.rel})
	a.pasteInto(dst, "", "")
	if got, err := vault.Read(dst.Root, "Spec.md"); err != nil || got != "# External Spec\n" {
		t.Fatalf("copy from external into normal space failed: %q, %v", got, err)
	}
}

func TestSidebarCopyPasteShortcutsAndFocusIsolation(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	if err := vault.Write(sp.Root, "Note.md", "# Note\n\nBody text\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	settle(tt)

	// Clicking a sidebar row focuses it; leaving the pointer resting on it past
	// previewOpenDelay must not open a preview or break Cmd+C / Cmd+V.
	if err := tt.Click("Note"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(previewOpenDelay + 200*time.Millisecond)
	tt.Frame()
	if a.preview.shown != nil && !a.preview.closing {
		t.Fatalf("hover preview opened unexpectedly on just-clicked document row: %+v", a.preview.shown)
	}
	tt.Key(ui.Cmd, ui.KeyC)
	settle(tt)
	if len(a.copied) != 1 || a.copied[0].rel != "Note.md" {
		t.Fatalf("Cmd+C on focused sidebar row failed: %+v", a.copied)
	}
	tt.Key(ui.Cmd, ui.KeyV)
	settle(tt)
	if _, err := vault.Read(sp.Root, "Note 2.md"); err != nil {
		t.Fatalf("Cmd+V on focused sidebar row did not paste copy: %v", err)
	}

	// Clicking Journal collapses it; leaving the pointer resting on it past
	// previewOpenDelay must not hijack focus into the folder preview search box,
	// so Cmd+C then Cmd+V still creates a sibling folder copy (Journal 2).
	if err := tt.Click("Journal"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(previewOpenDelay + 200*time.Millisecond)
	tt.Frame()
	if a.preview.shown != nil && !a.preview.closing {
		t.Fatalf("folder preview stole focus after clicking to collapse: %+v", a.preview.shown)
	}
	tt.Key(ui.Cmd, ui.KeyC)
	settle(tt)
	tt.Key(ui.Cmd, ui.KeyV)
	settle(tt)
	if _, err := vault.Read(sp.Root, "Journal 2/Ideas.md"); err != nil {
		t.Fatalf("Cmd+C / Cmd+V on focused folder after resting did not create sibling copy: %v", err)
	}

	// Moving away and hovering the closed Journal folder again still opens the
	// folder preview as usual.
	jr := rowRect(a, "Journal")
	tt.Move(jr.X+40, jr.Y+200)
	tt.Frame()
	tt.Move(jr.X+40, jr.Y+jr.H/2)
	tt.Frame()
	tt.Advance(previewOpenDelay + 200*time.Millisecond)
	tt.Frame()
	if s := a.preview.shown; s == nil || s.kind != previewFolder || s.rel != "Journal" {
		t.Fatalf("expected folder preview after leaving and re-hovering, got %+v", s)
	}
	tt.Key(0, ui.KeyEscape)
	settle(tt)

	// When the editor has focus, Cmd+C / Cmd+V must NOT trigger sidebar copy/paste.
	a.copied = nil
	a.open("Note.md")
	focusDocument(a, tt)
	tt.Key(ui.Cmd, ui.KeyC)
	settle(tt)
	if len(a.copied) != 0 {
		t.Fatalf("Cmd+C in editor leaked into sidebar clipboard: %+v", a.copied)
	}
	a.copied = []place{{sp.ID, "Note.md"}}
	tt.Key(ui.Cmd, ui.KeyV)
	settle(tt)
	if _, err := os.Stat(filepath.Join(sp.Root, "Note 3.md")); !os.IsNotExist(err) {
		t.Fatal("Cmd+V in editor triggered sidebar file paste")
	}

	// When inline rename input has focus, Cmd+C / Cmd+V must not trigger sidebar paste.
	a.renameIn(sp, "Note.md")
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyV)
	settle(tt)
	if _, err := os.Stat(filepath.Join(sp.Root, "Note 3.md")); !os.IsNotExist(err) {
		t.Fatal("Cmd+V during inline rename triggered sidebar file paste")
	}
}

func TestSidebarFavoriteCopyAndCommandPalette(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	work := a.state.Add("Work", t.TempDir())
	a.state.Favorites = nil
	a.toggleFavorite(sp, "Welcome.md")
	settle(tt)

	// Right-click favorite tile -> Copy, switch to Work, paste via command palette.
	if err := tt.RightClick("Welcome"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("复制"); err != nil {
		t.Fatalf("favorite tile Copy failed: %v; menu=%q", err, tt.Menu())
	}
	settle(tt)
	if len(a.copied) != 1 || a.copied[0] != (place{sp.ID, "Welcome.md"}) {
		t.Fatalf("favorite tile copy clipboard = %+v", a.copied)
	}

	a.switchSpace(work.ID)
	settle(tt)
	a.openPalette()
	a.palette.query = "粘贴到当前文件夹"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	settle(tt)
	if got, err := vault.Read(work.Root, "Welcome.md"); err != nil || !strings.Contains(got, "# Welcome to ArcMD") {
		t.Fatalf("palette paste into Work failed: %q, %v", got, err)
	}

	// Command bar Duplicate Current Document works in focus mode by opening the duplicate directly.
	a.open("Welcome.md")
	a.focus = true
	settle(tt)
	a.openPalette()
	a.palette.query = "创建当前文档副本"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	settle(tt)
	if a.doc == nil || a.doc.rel != "Welcome 2.md" {
		t.Fatalf("palette duplicate in focus mode should open Welcome 2.md, got %+v", a.doc)
	}
}

func TestSidebarCopyPasteMarkdownImagesAndEmptyListMenu(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	_, data := pngFile(t, "shot.png")
	if err := vault.Write(sp.Root, "assets/shot.png", string(data)); err != nil {
		t.Fatal(err)
	}
	rawMD := "---\ncover: assets/shot.png\n---\n\n# 会议模板\n\n![截图](assets/shot.png)\n"
	if err := vault.Write(sp.Root, "会议模板.md", rawMD); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sp.Root, "Archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Archive/assets/shot.png", "different"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	settle(tt)

	// Same-directory copy keeps Markdown raw bytes untouched and shares assets/shot.png.
	a.copyEntries(sp, []string{"会议模板.md"})
	a.pasteInto(sp, "", "")
	if got, err := vault.Read(sp.Root, "会议模板 2.md"); err != nil || got != rawMD {
		t.Fatalf("same-dir Markdown copy changed raw bytes:\ngot:  %q\nwant: %q\nerr:  %v", got, rawMD, err)
	}

	// Cross-folder copy with colliding asset name copies to a numbered image and
	// rewrites both cover and image block URLs in the copied Markdown file.
	a.pasteInto(sp, "Archive", "")
	movedMD, err := vault.Read(sp.Root, "Archive/会议模板.md")
	if err != nil {
		t.Fatalf("cross-folder Markdown copy missing: %v", err)
	}
	if !strings.Contains(movedMD, "assets/shot-1.png") {
		t.Fatalf("expected colliding asset rewritten to assets/shot-1.png, got:\n%s", movedMD)
	}
	if got, err := vault.Read(sp.Root, "Archive/assets/shot-1.png"); err != nil || got != string(data) {
		t.Fatalf("numbered asset missing or mismatched: err=%v", err)
	}
	if got, _ := vault.Read(sp.Root, "Archive/assets/shot.png"); got != "different" {
		t.Fatal("existing destination asset overwritten")
	}

	// Right-clicking the space header (listMenu) offers Paste into another space's root.
	work := a.state.Add("Work", t.TempDir())
	a.switchSpace(work.ID)
	settle(tt)
	if err := tt.RightClick("空间标题"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("粘贴"); err != nil {
		t.Fatalf("listMenu Paste into empty space failed: %v; menu=%q", err, tt.Menu())
	}
	settle(tt)
	if _, err := vault.Read(work.Root, "会议模板.md"); err != nil {
		t.Fatalf("space header Paste did not copy document into Work root: %v", err)
	}
	if got, err := vault.Read(work.Root, "assets/shot.png"); err != nil || got != string(data) {
		t.Fatalf("cross-space Markdown Paste did not copy local image: err=%v", err)
	}
}
