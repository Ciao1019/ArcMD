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
	a.open("Journal/Ideas.arcmd")
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
	a.open("Journal/Ideas.arcmd")
	if made[0].focused != 1 || a.doc == moved || len(made) != 1 {
		t.Fatal("one file has one editor")
	}
	// Closing the window saves it.
	if !made[0].close() || len(a.windows) != 0 {
		t.Fatal("close")
	}
	text, _ := vault.Read(root(a), "Journal/Ideas.arcmd")
	if !strings.Contains(text, "窗口里") {
		t.Fatal("saved on close")
	}
	a.open("Journal/Ideas.arcmd")
	if a.doc == nil || a.doc.rel != "Journal/Ideas.arcmd" {
		t.Fatal("back in the main window")
	}
}
