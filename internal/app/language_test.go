package app

import (
	"testing"

	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/prefs"
)

func TestEnglishInterface(t *testing.T) {
	a, tt := newTestApp(t)
	t.Cleanup(func() { i18n.SetLanguage("zh-Hans") })
	tt.Frame()
	if !tt.HasText("新建文档") {
		t.Fatal("Chinese by default")
	}
	a.openSettings(tabAppearance)
	tt.Frame()
	if err := tt.Click("English"); err != nil {
		t.Fatal(err)
	}
	a.settingsOpen = false
	tt.Frame()
	if !i18n.English() || !tt.HasText("New Document") || tt.HasText("新建文档") {
		t.Fatal("the interface shows English", tt.Texts())
	}
	saved, _ := (prefs.Store{Path: a.prefStore.Path}).Load()
	if saved.Language != "en" {
		t.Fatal("kept", saved.Language)
	}
	a.openSettings(tabShortcuts)
	tt.Frame()
	if !tt.HasText("Command Bar") || !tt.HasText("Today's Note") {
		t.Fatal("labels made at start follow the language", tt.Texts())
	}
	a.settingsOpen = false
	a.notify("保存 %s 失败。%v", "x", "y")
	tt.Frame()
	if !tt.HasText("Couldn't save x. y") {
		t.Fatal("messages too", tt.Texts())
	}
	if i18n.Pick([]string{"fr-FR", "zh-Hant-TW"}) != "zh-Hans" || i18n.Pick([]string{"de"}) != "en" {
		t.Fatal("system language")
	}
}
