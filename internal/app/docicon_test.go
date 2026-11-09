package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLeadingEmoji(t *testing.T) {
	for title, want := range map[string]string{
		"📚 读书笔记":     "📚",
		"✈️ Trip":    "✈️",
		"👩‍💻 Work":   "👩‍💻",
		"👍🏽 Yes":     "👍🏽",
		"🇨🇳 China":   "🇨🇳",
		"1️⃣ First":  "", // a digit is a letter here
		"周报":         "",
		"Roadmap":    "",
		"  🌱 garden": "🌱",
	} {
		if got := leadingEmoji(title); got != want {
			t.Errorf("leadingEmoji(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestDocName(t *testing.T) {
	for title, want := range map[string]string{"✈️ Trip": "Trip", "📚读书": "读书", "🌱": "🌱", "Plan": "Plan"} {
		if got := docName(title); got != want {
			t.Errorf("docName(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestDocInitial(t *testing.T) {
	for title, want := range map[string]string{
		"周报": "周", "roadmap": "R", "  (draft) Plan": "D", "2026 年计划": "2", "---": "·",
	} {
		if got := docInitial(title); got != want {
			t.Errorf("docInitial(%q) = %q, want %q", title, got, want)
		}
	}
}

// A title keeps its color, whatever its case, and a handful of titles use
// several colors.
func TestDocColor(t *testing.T) {
	if docColorIndex("Roadmap") != docColorIndex("roadmap ") {
		t.Error("the color depends on case or spaces")
	}
	seen := map[int]bool{}
	for _, s := range []string{"Welcome", "Ideas", "周报", "Roadmap", "Reading", "Trip", "会议记录", "Budget"} {
		seen[docColorIndex(s)] = true
	}
	if len(seen) < 5 {
		t.Errorf("8 titles share %d colors", len(seen))
	}
}

// TestDocIconSnapshot renders favorites and a tree of varied names, for
// review.
func TestDocIconSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the document icons")
	for _, dark := range []bool{false, true} {
		name := "doc-icons"
		if dark {
			name += "-dark"
		}
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetSize(1000, 640)
			tt.SetDark(dark)
			sp := a.state.ActiveSpace()
			for _, n := range []string{"周报", "Roadmap", "📚 读书笔记", "会议记录", "✈️ Trip"} {
				rel := n + ".md"
				os.WriteFile(filepath.Join(sp.Root, rel), []byte("# "+n+"\n"), 0o644)
				if n != "会议记录" {
					a.state.ToggleFavorite(sp.ID, rel)
				}
			}
			a.refreshTree()
			for range 3 {
				tt.Frame()
			}
			writePNG(t, filepath.Join(dir, fmt.Sprint(name, ".png")), tt.Image())
		})
	}
}
