package app

import (
	"testing"
	"time"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

func TestClearFormattingCommandBarShortcutAndSettings(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()

	// Apply color, highlight, bold, and link to "Welcome" in the opening heading.
	ed := a.doc.ed
	end := ui.UTF16Len("Welcome")
	ed.SetSelection(0, end)
	red, yellow := "red", "light-yellow"
	ed.Engine.SetTextColor(&red, &yellow)
	ed.Wrap("**")
	ed.Engine.SetLink("https://example.com")
	tt.Frame()

	// Command bar runs "清除样式", clears all inline formatting + link, and refocuses editor.
	tt.Key(ui.Cmd, ui.KeyK)
	a.palette.query = "清除样式"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.palette.open || !tt.Focused("文档") {
		t.Fatal("command bar did not close and refocus editor")
	}
	if r := ed.Doc.Blocks[0].Runs[0]; r.Style != (model.Style{}) {
		t.Fatalf("command bar did not clear formatting: %+v", r.Style)
	}
	if ed.Doc.Blocks[0].Kind != "heading" {
		t.Fatalf("command bar altered block kind: %s", ed.Doc.Blocks[0].Kind)
	}

	// Undo restores styles, then ⇧⌘\ clears formatting without toggling the sidebar.
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	if ed.Doc.Blocks[0].Runs[0].Style.Link != "https://example.com" {
		t.Fatalf("undo did not restore link: %+v", ed.Doc.Blocks[0].Runs[0].Style)
	}
	ed.SetSelection(0, end)
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBackslash)
	tt.Frame()
	if a.sidebarHidden {
		t.Fatal("⇧⌘\\ toggled the sidebar instead of clearing formatting")
	}
	if r := ed.Doc.Blocks[0].Runs[0]; r.Style != (model.Style{}) {
		t.Fatalf("⇧⌘\\ did not clear formatting: %+v", r.Style)
	}

	// Plain ⌘\ still toggles the sidebar.
	tt.Key(ui.Cmd, ui.KeyBackslash)
	tt.Advance(time.Second)
	tt.Frame()
	if !a.sidebarHidden {
		t.Fatal("⌘\\ stopped toggling the sidebar")
	}
	tt.Key(ui.Cmd, ui.KeyBackslash)
	tt.Advance(time.Second)
	tt.Frame()

	// Settings lists "清除样式" under shortcuts and allows rebinding; English translation works.
	a.openSettings(tabShortcuts)
	tt.Frame()
	if !tt.HasText("清除样式") {
		t.Fatalf("shortcuts settings missing 清除样式: %q", tt.Texts())
	}
	custom := editor.Combo{Mods: ui.Cmd | ui.Alt, Key: ui.KeyBackslash}
	if !a.rebind("clear-format", &custom) {
		t.Fatal("rebind clear-format failed")
	}
	if got := a.editorBindings()["clear-format"]; got != custom {
		t.Fatalf("rebound editor binding = %+v; want %+v", got, custom)
	}

	t.Cleanup(func() { i18n.SetLanguage("zh-Hans") })
	i18n.SetLanguage("en")
	tt.Frame()
	if !tt.HasText("Clear Formatting") {
		t.Fatalf("English shortcuts settings missing Clear Formatting: %q", tt.Texts())
	}
}
