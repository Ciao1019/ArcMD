package update

import (
	"reflect"
	"testing"
	"time"
)

func TestCurrent(t *testing.T) {
	if n := Current("", "", false); n != nil {
		t.Fatalf("nothing to say, got %v", n)
	}
	if n := Current("1.2.0", "1.3.0", false); *n != (Notice{Ready, "1.2.0"}) {
		t.Fatalf("a downloaded update comes first, got %v", n)
	}
	if n := Current("", "1.3.0", false); *n != (Notice{Available, "1.3.0"}) {
		t.Fatalf("found update, got %v", n)
	}
	if n := Current("1.2.0", "", true); n != nil {
		t.Fatalf("dismissed banner, got %v", n)
	}
}

func TestNoticeTexts(t *testing.T) {
	r := Notice{Ready, "0.2.0"}
	if r.Title() != "更新已准备就绪！" || r.Subtitle() != "点击重新启动" || r.Button() != "重新启动" {
		t.Fatalf("ready texts: %q %q %q", r.Title(), r.Subtitle(), r.Button())
	}
	a := Notice{Available, "0.2.0"}
	if a.Title() != "ArcMD 有新版本" || a.Subtitle() != "ArcMD 0.2.0" || a.Button() != "更新" {
		t.Fatalf("available texts: %q %q %q", a.Title(), a.Subtitle(), a.Button())
	}
}

func TestToast(t *testing.T) {
	n := &Notice{Ready, "0.2.0"}
	if Toast(n, nil, true) != nil {
		t.Fatal("the banner says it while the sidebar shows")
	}
	if got := Toast(n, nil, false); got == nil || *got != *n {
		t.Fatalf("hidden sidebar toasts, got %v", got)
	}
	if Toast(n, &Notice{Ready, "0.2.0"}, false) != nil {
		t.Fatal("a notice toasts once")
	}
	if Toast(&Notice{Ready, "0.3.0"}, n, false) == nil {
		t.Fatal("a newer update is news again")
	}
	if Toast(&Notice{Ready, "0.2.0"}, &Notice{Available, "0.2.0"}, false) == nil {
		t.Fatal("the found update finishing its download is news again")
	}
	if Toast(nil, nil, false) != nil {
		t.Fatal("nothing to toast")
	}
}

func TestDue(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	if Due(time.Time{}, now) {
		t.Fatal("no plan is not due")
	}
	if !Due(now, now) || Due(now.Add(time.Second), now) || !Due(now.Add(-time.Hour), now) {
		t.Fatal("due at and after the plan")
	}
}

func TestReleaseURL(t *testing.T) {
	if got := ReleaseURL("0.2.0"); got != "https://github.com/Ciao1019/ArcMD/releases/tag/v0.2.0" {
		t.Fatal(got)
	}
	if got := ReleaseURL("v0.2.0"); got != "https://github.com/Ciao1019/ArcMD/releases/tag/v0.2.0" {
		t.Fatal(got)
	}
	if got := ReleaseURL(""); got != "https://github.com/Ciao1019/ArcMD/releases/latest" {
		t.Fatal(got)
	}
}

func TestLines(t *testing.T) {
	notes := "## What's Changed\r\n\r\n\r\n* **Faster** search by @someone in https://github.com/Ciao1019/ArcMD/pull/12\n- See [the docs](https://example.com) and `code`\n1. First\n\nPlain text  \n\n"
	want := []string{
		"What's Changed",
		"",
		"• Faster search by @someone in #12",
		"• See the docs and code",
		"• First",
		"",
		"Plain text",
	}
	if got := Lines(notes); !reflect.DeepEqual(got, want) {
		t.Fatalf("Lines:\n got %q\nwant %q", got, want)
	}
	if got := Lines("\n\n"); len(got) != 0 {
		t.Fatalf("empty notes: %q", got)
	}
}
