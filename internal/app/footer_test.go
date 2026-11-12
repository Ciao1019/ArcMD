package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

// The footer of a long page rereads it once typing pauses, not at each
// keystroke; a short page's footer follows each one.
func TestFooterWaitsForTypingOnLongPage(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	long := "# Long\n\n" + strings.Repeat("Some words to make the page long enough.\n\n", 2000) + "#old"
	for name, md := range map[string]string{"Long.arcmd": long, "Short.arcmd": "# Short\n\n#old"} {
		doc := editor.ImportMarkdown(md)
		doc.ImportSource = ""
		text, _ := doc.Encode()
		vault.Write(root(a), name, text)
	}
	a.refreshTree()
	for _, c := range []struct {
		name string
		wait bool
	}{{"Long.arcmd", true}, {"Short.arcmd", false}} {
		a.open(c.name)
		focusDocument(a, tt)
		if !slices.Contains(a.doc.footer.tags, "old") {
			t.Fatal(c.name, "footer tags", a.doc.footer.tags)
		}
		a.doc.ed.SetSelection(0, 0)
		a.doc.ed.ReplaceText(nil, "#fresh ")
		tt.Frame()
		if got := slices.Contains(a.doc.footer.tags, "fresh"); got == c.wait {
			t.Fatal(c.name, "footer read the edit at once:", got)
		}
		tt.Advance(footerSettle)
		tt.Frame()
		tt.Frame()
		if !slices.Contains(a.doc.footer.tags, "fresh") {
			t.Fatal(c.name, "footer never read the edit", a.doc.footer.tags)
		}
	}
}
