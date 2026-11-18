package editor

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestSpaceInputKeepsViewport(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mods    ui.Modifiers
		compose bool
	}{
		{name: "space"},
		{name: "shift-space", mods: ui.Shift},
		{name: "composition", compose: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, tt := selectionScrollTester(false)
			tt.Scroll(350, 290, 0, 600)
			tt.ClickAt(350, 290)
			before, at, scroll := e.Text(), e.Caret, e.Scroll.Y
			text := " "
			if tc.compose {
				tt.Compose("候选", 2)
				text = "中文"
			}
			// Deliver the physical key separately: text-only input misses
			// MyGo's default Space/Shift+Space page scrolling.
			tt.Key(tc.mods, ui.KeySpace)
			if e.Scroll.Y != scroll || e.Text() != before {
				t.Fatalf("Space key changed viewport or committed text: scroll=%v, want %v", e.Scroll.Y, scroll)
			}
			tt.Type(text)
			for range 30 {
				tt.Advance(time.Second / 60)
				if e.Scroll.Y != scroll {
					t.Fatalf("text input scrolled to %v, want %v", e.Scroll.Y, scroll)
				}
			}
			byteAt := ui.UTF16ByteOffset(before, at)
			if e.Text() != before[:byteAt]+text+before[byteAt:] || e.Caret != at+ui.UTF16Len(text) {
				t.Fatal("text was not inserted exactly once at the caret")
			}
		})
	}
}

func TestReadOnlySpaceStillPages(t *testing.T) {
	e, tt := selectionScrollTester(false)
	e.ReadOnly = true
	tt.Frame()
	tt.Scroll(350, 290, 0, 600)
	tt.ClickAt(350, 290)
	before, scroll := e.Text(), e.Scroll.Y
	tt.TypeKey(0, ui.KeySpace, " ")
	tt.Frame()
	if e.Scroll.Y <= scroll || e.Text() != before || !e.refusedAt.IsZero() {
		t.Fatalf("read-only Space should page down without editing or refusing: scroll=%v (was %v), refusedAt=%v", e.Scroll.Y, scroll, e.refusedAt)
	}
	down := e.Scroll.Y
	tt.TypeKey(ui.Shift, ui.KeySpace, " ")
	tt.Frame()
	if e.Scroll.Y >= down || e.Text() != before || !e.refusedAt.IsZero() {
		t.Fatalf("read-only Shift+Space should page up without editing or refusing: scroll=%v (was %v), refusedAt=%v", e.Scroll.Y, down, e.refusedAt)
	}
}
