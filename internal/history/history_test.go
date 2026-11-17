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

	must(Record(root, "Notes/Doc.md", "# v1\n", t0, true))
	must(Record(root, "Notes/Doc.md", "# v2\n", t0.Add(2*time.Minute), false))
	mdVs, err := List(root, "Notes/Doc.md")
	must(err)
	if len(mdVs) != 2 || filepath.Ext(mdVs[0].File) != ".md" {
		t.Fatalf("markdown snapshots %+v", mdVs)
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

func joinSpans(spans []Span) string {
	var out string
	for _, sp := range spans {
		out += sp.Text
	}
	return out
}

func TestInlineGraphemeDiffCJKAndEmoji(t *testing.T) {
	a := []string{"今天在 👨‍👩‍👧‍👦 和 👍🏽 这里讨论架构", "完全不相关的一整行abcdefg"}
	b := []string{"明天在 👨‍👩‍👧‍👦 和 👍🏽 这里审核架构", "xyz123987"}
	d := Diff(a, b)

	var foundAdd, foundRem bool
	for _, l := range d {
		if l.Op == Added && l.Text == b[0] {
			foundAdd = true
			if len(l.Spans) == 0 {
				t.Fatalf("expected inline Spans on modified Added line %q", l.Text)
			}
			if got := joinSpans(l.Spans); got != l.Text {
				t.Fatalf("Added spans reconstruct %q != %q", got, l.Text)
			}
			var changed []string
			for _, sp := range l.Spans {
				if sp.Changed {
					changed = append(changed, sp.Text)
				} else if sp.Text == "👨‍👩‍👧‍👦" || sp.Text == "👍🏽" {
					t.Fatalf("emoji split unexpectedly: %+v", l.Spans)
				}
			}
			if !slices.Equal(changed, []string{"明", "审核"}) {
				t.Fatalf("unexpected Added changed spans: %+v", l.Spans)
			}
		}
		if l.Op == Removed && l.Text == a[0] {
			foundRem = true
			if len(l.Spans) == 0 {
				t.Fatalf("expected inline Spans on modified Removed line %q", l.Text)
			}
			if got := joinSpans(l.Spans); got != l.Text {
				t.Fatalf("Removed spans reconstruct %q != %q", got, l.Text)
			}
			var changed []string
			for _, sp := range l.Spans {
				if sp.Changed {
					changed = append(changed, sp.Text)
				}
			}
			if !slices.Equal(changed, []string{"今", "讨论"}) {
				t.Fatalf("unexpected Removed changed spans: %+v", l.Spans)
			}
		}
		if l.Text == a[1] || l.Text == b[1] {
			if len(l.Spans) != 0 {
				t.Fatalf("unrelated line should not get inline spans: %+v", l)
			}
		}
	}
	if !foundAdd || !foundRem {
		t.Fatalf("missing modified lines in diff: %+v", d)
	}
}

func TestMilestonesSurviveKeepAndCopyTo(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()
	t0 := time.Date(2026, 2, 1, 10, 0, 0, 0, time.Local)

	v1, err := SaveMilestone(root1, "Plan.arcmd", "v1 milestone body", "定稿 v1", t0)
	if err != nil {
		t.Fatal(err)
	}
	if !v1.Kept || v1.Label != "定稿 v1" {
		t.Fatalf("unexpected milestone: %+v", v1)
	}

	// Same content promoted in place when newest unlabeled version matches.
	if err := Record(root1, "Plan.arcmd", "v2 ordinary", t0.Add(time.Hour), false); err != nil {
		t.Fatal(err)
	}
	v2, err := SaveMilestone(root1, "Plan.arcmd", "v2 ordinary", "评审前", t0.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if v2.Label != "评审前" || !v2.Kept {
		t.Fatalf("failed in-place promotion: %+v", v2)
	}

	// Recording > Keep ordinary snapshots never evicts milestones.
	for i := range Keep + 10 {
		if err := Record(root1, "Plan.arcmd", fmt.Sprintf("auto-%d", i), t0.Add(time.Duration(i+3)*time.Hour), false); err != nil {
			t.Fatal(err)
		}
	}
	vs, err := List(root1, "Plan.arcmd")
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]bool{}
	for _, v := range vs {
		if v.Label != "" {
			labels[v.Label] = true
		}
	}
	if !labels["定稿 v1"] || !labels["评审前"] {
		t.Fatalf("milestones lost after Keep limit: %+v", labels)
	}

	// CopyTo preserves labels even across timestamp collision in root2.
	if err := Record(root2, "Copied.arcmd", "different content at same stamp", t0, false); err != nil {
		t.Fatal(err)
	}
	if err := CopyTo(root1, "Plan.arcmd", root2, "Copied.arcmd", nil); err != nil {
		t.Fatal(err)
	}
	copied, err := List(root2, "Copied.arcmd")
	if err != nil {
		t.Fatal(err)
	}
	copiedLabels := map[string]string{}
	for _, v := range copied {
		if v.Label != "" {
			copiedLabels[v.Label] = ReadOrEmpty(t, root2, "Copied.arcmd", v)
		}
	}
	if copiedLabels["定稿 v1"] != "v1 milestone body" || copiedLabels["评审前"] != "v2 ordinary" {
		t.Fatalf("CopyTo did not preserve milestone labels cleanly: %+v", copiedLabels)
	}

	// Rename label via SetLabel.
	if err := SetLabel(root1, "Plan.arcmd", v1.File, "最终归档"); err != nil {
		t.Fatal(err)
	}
	vs, _ = List(root1, "Plan.arcmd")
	renamed := false
	for _, v := range vs {
		if v.File == v1.File && v.Label == "最终归档" {
			renamed = true
		}
	}
	if !renamed {
		t.Fatal("SetLabel did not update version label")
	}
}

func ReadOrEmpty(t *testing.T, root, rel string, v Version) string {
	t.Helper()
	s, err := Read(root, rel, v)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
