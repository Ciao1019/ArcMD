package search

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// plain treats a file's content as its text, counting reads.
type plain struct{ reads int }

func (p *plain) extract(name, content string) (Extracted, error) {
	p.reads++
	if strings.Contains(content, "BROKEN") {
		return Extracted{}, errors.New("broken")
	}
	icon, _, _ := strings.Cut(content, "ICON:")
	if !strings.Contains(content, "ICON:") {
		icon = ""
	}
	return Extracted{Text: content, Icon: icon}, nil
}

func write(t *testing.T, root, rel, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshAndSearch(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	write(t, a, "Notes.arcmd", "Quiet writing\nA calm place to write.")
	write(t, a, "Journal/Day.md", "Nothing here")
	write(t, a, ".hidden/Secret.md", "calm")
	write(t, a, "image.png", "calm")
	write(t, a, "Bad.arcmd", "calm BROKEN")
	write(t, b, "Plan.arcmd", "Calm, calm, CALM.")
	p := &plain{}
	roots := []Root{{"a", a}, {"b", b}}
	ix := Refresh(nil, roots, p.extract)
	if ix.Len() != 3 || p.reads != 4 {
		t.Fatalf("indexed %d documents with %d reads, want 3 and 4", ix.Len(), p.reads)
	}
	hits := ix.Search("calm", nil, 10)
	if len(hits) != 2 {
		t.Fatalf("hits = %+v", hits)
	}
	// More matches first.
	if hits[0].Path != "Plan.arcmd" || hits[0].Space != "b" || hits[0].Count != 3 {
		t.Errorf("first hit = %+v", hits[0])
	}
	h := hits[1]
	if h.Path != "Notes.arcmd" || h.Snippet != "A calm place to write." || h.Snippet[h.Match[0]:h.Match[1]] != "calm" {
		t.Errorf("second hit = %+v, match %q", h, h.Snippet[h.Match[0]:h.Match[1]])
	}

	// Unchanged files are not read again; a changed one is.
	p.reads = 0
	later := time.Now().Add(time.Minute)
	write(t, a, "Journal/Day.md", "Calm now")
	os.Chtimes(filepath.Join(a, "Journal/Day.md"), later, later)
	os.Remove(filepath.Join(b, "Plan.arcmd"))
	ix = Refresh(ix, roots, p.extract)
	if p.reads != 2 { // Day.md, and Bad.arcmd, which never made it in
		t.Errorf("reads = %d, want 2", p.reads)
	}
	hits = ix.Search("calm", nil, 10)
	if len(hits) != 2 || hits[0].Path != "Notes.arcmd" || hits[1].Path != "Journal/Day.md" {
		t.Errorf("after refresh: %+v", hits)
	}
}

func TestSearchLiveAndLimits(t *testing.T) {
	a := t.TempDir()
	write(t, a, "Notes.arcmd", "saved words")
	p := &plain{}
	ix := Refresh(nil, []Root{{"a", a}}, p.extract)
	live := &Live{Space: "a", Path: "Notes.arcmd", Text: "unsaved words"}
	if hits := ix.Search("unsaved", live, 10); len(hits) != 1 {
		t.Errorf("live text not searched: %+v", hits)
	}
	if hits := ix.Search("saved words", live, 10); len(hits) != 1 || hits[0].Snippet != "unsaved words" {
		t.Errorf("indexed text searched in place of the live: %+v", hits)
	}
	// A document not yet indexed is searched too.
	fresh := &Live{Space: "a", Path: "New.arcmd", Text: "brand new"}
	if hits := ix.Search("brand", fresh, 10); len(hits) != 1 || hits[0].Path != "New.arcmd" {
		t.Errorf("new live document: %+v", hits)
	}
	if hits := ix.Search("w", nil, 10); hits != nil {
		t.Error("one ASCII letter searched")
	}
	if !Worth("字") || Worth(" a ") || !Worth("ab") {
		t.Error("Worth")
	}
	var empty *Index
	if empty.Search("words", nil, 10) != nil || empty.Len() != 0 {
		t.Error("nil index")
	}
}

func TestSnippet(t *testing.T) {
	long := strings.Repeat("前文", 30) + "\n第二行 关键词 在这里" + strings.Repeat("后", 80)
	q := []rune("关键词")
	count, first := find(long, q)
	if count != 1 {
		t.Fatal(count)
	}
	s, m := snip(long, first, len(q))
	if !strings.HasPrefix(s, "第二行 ") || !strings.HasSuffix(s, "…") || s[m[0]:m[1]] != "关键词" {
		t.Errorf("snippet %q, match %q", s, s[m[0]:m[1]])
	}
	s, m = snip(strings.Repeat("x", 40)+"hit", 40, 3)
	if !strings.HasPrefix(s, "…") || s[m[0]:m[1]] != "hit" {
		t.Errorf("cut start: %q", s)
	}
}

// Backlinks, tags and icons come from the same snapshot.
func TestLinksTagsAndIcons(t *testing.T) {
	a := t.TempDir()
	write(t, a, "Plan.arcmd", "🚀ICON:# Plan\nsee [[Notes]] #work")
	write(t, a, "Notes.arcmd", "# Notes\n[[notes]] self #work/2026 #idea")
	write(t, a, "Other.arcmd", "nothing #Idea")
	var p plain
	ix := Refresh(nil, []Root{{"s", a}}, p.extract)
	hits := ix.Backlinks("Notes", Live{Space: "s", Path: "Notes.arcmd"}, nil)
	if len(hits) != 1 || hits[0].Path != "Plan.arcmd" || hits[0].Snippet[hits[0].Match[0]:hits[0].Match[1]] != "[[Notes]]" {
		t.Fatalf("backlinks = %+v", hits)
	}
	live := &Live{Space: "s", Path: "Other.arcmd", Text: "now [[Notes|n]]"}
	if hits := ix.Backlinks("notes", Live{Space: "s", Path: "Notes.arcmd"}, live); len(hits) != 2 {
		t.Errorf("live backlink missing: %+v", hits)
	}
	if hits := ix.Tagged("#work", nil); len(hits) != 2 {
		t.Errorf("tagged work = %+v", hits)
	}
	tags := ix.Tags(nil)
	if len(tags) != 3 || tags[0].Name != "idea" || tags[0].Count != 2 {
		t.Errorf("tags = %+v", tags)
	}
	if ix.Icon("s", "Plan.arcmd") != "🚀" || ix.Icon("s", "Notes.arcmd") != "" {
		t.Error("icons")
	}
}
