package wiki

import (
	"slices"
	"testing"
)

func TestLinks(t *testing.T) {
	text := "See [[Trip Plan]] and [[Ideas|my ideas]], [[Notes#Today]]; not [[]] or [[a\nb]]. [[x [[Inner]]"
	var got []string
	for _, l := range Links(text) {
		got = append(got, l.Target+"="+l.Label+"="+text[l.Start:l.End])
	}
	want := []string{
		"Trip Plan=Trip Plan=[[Trip Plan]]",
		"Ideas=my ideas=[[Ideas|my ideas]]",
		"Notes=Notes=[[Notes#Today]]",
		"Inner=Inner=[[Inner]]",
	}
	if !slices.Equal(got, want) {
		t.Errorf("links = %q, want %q", got, want)
	}
	if !LinksTo(text, "trip plan ") || LinksTo(text, "Trip") {
		t.Error("LinksTo")
	}
}

func TestRename(t *testing.T) {
	got, ok := Rename("[[Old]], [[old|alias]], [[Old#h]] and [[Older]]", "Old", "New")
	if !ok || got != "[[New]], [[New|alias]], [[New#h]] and [[Older]]" {
		t.Errorf("rename = %q %v", got, ok)
	}
	if _, ok := Rename("[[Other]]", "Old", "New"); ok {
		t.Error("renamed a link to another document")
	}
}

func TestTags(t *testing.T) {
	text := "#idea at start, (#草稿) and #work/2026 but not C# a#b #1 http://x/#frag #end-。#旅行"
	var got []string
	for _, tag := range Tags(text) {
		got = append(got, tag.Name+"="+text[tag.Start:tag.End])
	}
	want := []string{"idea=#idea", "草稿=#草稿", "work/2026=#work/2026", "end=#end", "旅行=#旅行"}
	if !slices.Equal(got, want) {
		t.Errorf("tags = %q, want %q", got, want)
	}
	if names := TagNames("#a #B #b #c"); !slices.Equal(names, []string{"a", "B", "c"}) {
		t.Errorf("names = %q", names)
	}
}

func TestOpen(t *testing.T) {
	if at, typed, ok := Open("see [[Pro"); !ok || at != 4 || typed != "Pro" {
		t.Errorf("open = %d %q %v", at, typed, ok)
	}
	if _, _, ok := Open("see [[Done]] after"); ok {
		t.Error("a finished link is open")
	}
	if _, typed, ok := Open("[["); !ok || typed != "" {
		t.Error("just the brackets")
	}
}
