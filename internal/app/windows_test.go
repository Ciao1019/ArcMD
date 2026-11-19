package app

import (
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

type fakeWindow struct {
	focused, closed int
	title           string
	view            func(*ui.Context)
	close           func() bool
}

func (w *fakeWindow) Focus()            { w.focused++ }
func (w *fakeWindow) Close()            { w.closed++ }
func (w *fakeWindow) SetTitle(s string) { w.title = s }

func TestDocumentInItsOwnWindow(t *testing.T) {
	a, tt := newTestApp(t)
	var made []*fakeWindow
	a.newWindow = func(title string, view func(*ui.Context), close func() bool) windowHandle {
		w := &fakeWindow{title: title, view: view, close: close}
		made = append(made, w)
		return w
	}
	tt.Frame()
	a.open("Journal/Ideas.md")
	tt.Frame()
	a.doc.ed.SetSelection(0, 0)
	a.doc.ed.ReplaceText(nil, "窗口里")
	moved := a.doc
	if err := tt.RightClick("Ideas"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("在新窗口中打开"); err != nil {
		t.Fatal(err, tt.Menu())
	}
	tt.Frame()
	if len(made) != 1 || len(a.windows) != 1 || a.windows[0].doc != moved {
		t.Fatal("the document moves to its window, undo and all")
	}
	if a.doc == moved {
		t.Fatal("the main window shows the document before it")
	}
	// Its window shows it.
	wt := ui.NewTester(made[0].view, 900, 700)
	wt.Frame()
	if !wt.HasText("Ideas") || !strings.Contains(moved.ed.Text(), "窗口里") {
		t.Fatal(wt.Texts())
	}
	// Opening it from the main window brings its window forward.
	a.open("Journal/Ideas.md")
	if made[0].focused != 1 || a.doc == moved || len(made) != 1 {
		t.Fatal("one file has one editor")
	}
	// Closing the window saves it.
	if !made[0].close() || len(a.windows) != 0 {
		t.Fatal("close")
	}
	text, _ := vault.Read(root(a), "Journal/Ideas.md")
	if !strings.Contains(text, "窗口里") {
		t.Fatal("saved on close")
	}
	a.open("Journal/Ideas.md")
	if a.doc == nil || a.doc.rel != "Journal/Ideas.md" {
		t.Fatal("back in the main window")
	}
}

type fakeMainWindow struct {
	shown, hidden, focused int
	fullScreen             bool
}

func (w *fakeMainWindow) Show()                { w.shown++ }
func (w *fakeMainWindow) Hide()                { w.hidden++ }
func (w *fakeMainWindow) Focus()               { w.focused++ }
func (w *fakeMainWindow) IsFullScreen() bool   { return w.fullScreen }
func (w *fakeMainWindow) SetFullScreen(v bool) { w.fullScreen = v }

func TestMacOSMainWindowCloseHidesInsteadOfQuitting(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	a.doc.ed.SetSelection(0, 0)
	a.doc.ed.ReplaceText(nil, "自动保存后隐藏 ")

	mw := &fakeMainWindow{}
	// 1. On macOS, ⌘W / red close button flushes edits and hides the window without destroying/quitting.
	if a.closeMain(mw, "darwin") {
		t.Fatal("expected darwin closeMain without quit to prevent window destruction")
	}
	if mw.hidden != 1 {
		t.Fatalf("expected window to hide once, got %d", mw.hidden)
	}
	saved, err := vault.Read(root(a), "Welcome.md")
	if err != nil || !strings.Contains(saved, "自动保存后隐藏") {
		t.Fatalf("expected closeMain to flush unsaved edits before hiding: %q (%v)", saved, err)
	}

	// 2. Re-activating from the Dock or ⌘0 restores and focuses the main window.
	a.showMain(mw)
	if mw.shown != 1 || mw.focused != 1 {
		t.Fatalf("expected showMain to show and focus window: shown=%d focused=%d", mw.shown, mw.focused)
	}

	// 3. Full-screen close on macOS exits full-screen first before hiding.
	mw.fullScreen = true
	if a.closeMain(mw, "darwin") {
		t.Fatal("expected darwin full-screen close to prevent window destruction")
	}
	if mw.fullScreen || !a.hideAfterFullScreen || mw.hidden != 1 {
		t.Fatalf("expected full-screen exit before hide: fullScreen=%v hideAfter=%v hidden=%d", mw.fullScreen, a.hideAfterFullScreen, mw.hidden)
	}

	// 4. Explicit quit (⌘Q) on macOS or normal close on Windows/Linux allows window destruction.
	if !a.closeMain(mw, "windows") {
		t.Fatal("expected Windows closeMain to allow window close")
	}
	a.quitting = true
	if !a.closeMain(mw, "darwin") {
		t.Fatal("expected darwin closeMain during ⌘Q quit to allow window close")
	}
}
