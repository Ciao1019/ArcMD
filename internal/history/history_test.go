package history

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestRecordCoalescesAndKeeps(t *testing.T) {
	root := t.TempDir()
	t0 := time.Date(2026, 11, 8, 9, 0, 0, 0, time.Local)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(Record(root, "Notes/Plan.arcmd", "v1", t0, true))
	must(Record(root, "Notes/Plan.arcmd", "v1", t0.Add(time.Minute), false)) // unchanged
	must(Record(root, "Notes/Plan.arcmd", "v2", t0.Add(2*time.Minute), false))
	must(Record(root, "Notes/Plan.arcmd", "v3", t0.Add(4*time.Minute), false)) // replaces v2
	must(Record(root, "Notes/Plan.arcmd", "v4", t0.Add(30*time.Minute), false))
	vs, err := List(root, "Notes/Plan.arcmd")
	must(err)
	var got []string
	for _, v := range vs {
		text, err := Read(root, "Notes/Plan.arcmd", v)
		must(err)
		got = append(got, text)
	}
	if !slices.Equal(got, []string{"v4", "v3", "v1"}) {
		t.Fatalf("versions %q", got)
	}
	if !vs[0].Time.Equal(t0.Add(30 * time.Minute)) {
		t.Errorf("time %v", vs[0].Time)
	}
	if _, err := Read(root, "Notes/Plan.arcmd", Version{File: "../x"}); err == nil {
		t.Error("read outside the history")
	}
	if _, err := List(root, "../x.arcmd"); err == nil {
		t.Error("listed outside the space")
	}
}

func TestKeepLimit(t *testing.T) {
	root := t.TempDir()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	for i := range Keep + 5 {
		if err := Record(root, "a.arcmd", fmt.Sprint(i), t0.Add(time.Duration(i)*time.Hour), false); err != nil {
			t.Fatal(err)
		}
	}
	vs, _ := List(root, "a.arcmd")
	if len(vs) != Keep {
		t.Fatalf("%d versions kept", len(vs))
	}
	if text, _ := Read(root, "a.arcmd", vs[len(vs)-1]); text != "5" {
		t.Errorf("oldest kept %q", text)
	}
}

func TestMove(t *testing.T) {
	root := t.TempDir()
	if err := Record(root, "Old/a.arcmd", "x", time.Now(), true); err != nil {
		t.Fatal(err)
	}
	if err := Move(root, "Old", "New"); err != nil {
		t.Fatal(err)
	}
	if vs, _ := List(root, "New/a.arcmd"); len(vs) != 1 {
		t.Error("versions did not follow the folder")
	}
	if _, err := os.Stat(filepath.Join(root, ".arcmd/history/Old")); !os.IsNotExist(err) {
		t.Error("old history left")
	}
	if err := Move(root, "Missing.arcmd", "Other.arcmd"); err != nil {
		t.Error(err)
	}
}

func TestDiff(t *testing.T) {
	a := []string{"title", "one", "two", "three", "end"}
	b := []string{"title", "one", "2", "three", "four", "end"}
	var got []string
	for _, l := range Diff(a, b) {
		got = append(got, [...]string{" ", "+", "-"}[l.Op]+l.Text)
	}
	want := []string{" title", " one", "+2", "-two", " three", "+four", " end"}
	if !slices.Equal(got, want) {
		t.Errorf("diff %q", got)
	}
	if d := Diff(nil, []string{"x"}); len(d) != 1 || d[0].Op != Added {
		t.Errorf("from nothing %v", d)
	}
}
