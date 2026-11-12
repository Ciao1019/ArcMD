package app

import (
	"testing"

	"github.com/Ciao1019/ArcMD/internal/prefs"
)

func TestAppearanceFontAndPageWidth(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1500, 860)
	tt.Frame()
	standard := a.doc.ed.Padding()
	a.openSettings(tabAppearance)
	tt.Frame()
	if err := tt.Click("宽"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("衬线"); err != nil {
		t.Fatal(err)
	}
	if a.prefs.Page != "wide" || a.prefs.Font != "serif" {
		t.Fatal("choices", a.prefs.Page, a.prefs.Font)
	}
	saved, err := (prefs.Store{Path: a.prefStore.Path}).Load()
	if err != nil || saved.Page != "wide" || saved.Font != "serif" {
		t.Fatal("saved", saved, err)
	}
	a.settingsOpen = false
	tt.Frame()
	if wide := a.doc.ed.Padding(); wide >= standard {
		t.Fatal("a wide page has narrower margins", wide, standard)
	}
	a.openSettings(tabAppearance)
	tt.Frame()
	// The reset is below the sample: scroll to it.
	r, _ := tt.Find("恢复默认外观")
	tt.Scroll(r.X, 400, 0, 400)
	tt.Frame()
	if err := tt.Click("恢复默认外观"); err != nil {
		t.Fatal(err)
	}
	a.settingsOpen = false
	tt.Frame()
	if a.prefs.Page != "" || a.prefs.Font != "" || a.doc.ed.Padding() != standard {
		t.Fatal("reset")
	}
}

func TestSuggestEmoji(t *testing.T) {
	got := suggestEmoji("smil")
	if len(got) == 0 || got[0].Name != "smile" || got[0].Emoji != "😄" {
		t.Fatal(got)
	}
	if got := suggestEmoji("thumbs"); len(got) == 0 || got[0].Name != "thumbs_up" {
		t.Fatal("catalog names as shortcodes", got)
	}
	if len(suggestEmoji("a")) > 8 {
		t.Fatal("at most eight")
	}
	if shortcode("face with tears of joy") != "face_with_tears_of_joy" || shortcode("OK hand") != "ok_hand" {
		t.Fatal(shortcode("OK hand"))
	}
}
