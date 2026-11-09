package fuzzy

import "testing"

func TestScore(t *testing.T) {
	if _, ok := Score("xyz", "Daily notes"); ok {
		t.Error("xyz matched")
	}
	if _, ok := Score("", "anything"); !ok {
		t.Error("empty query did not match")
	}
	dn, ok1 := Score("dn", "Daily notes")
	ad, ok2 := Score("dn", "Random thoughts and")
	if !ok1 || !ok2 || dn <= ad {
		t.Errorf("word starts should rank higher: %d vs %d", dn, ad)
	}
	exact, _ := Score("new", "New note")
	inner, _ := Score("new", "Renew subscription")
	if exact <= inner {
		t.Errorf("prefix should rank higher: %d vs %d", exact, inner)
	}
	if _, ok := Score("日记", "我的日记本"); !ok {
		t.Error("CJK query did not match")
	}
}
