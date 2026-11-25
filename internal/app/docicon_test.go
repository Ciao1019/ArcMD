package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
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
		"roadmap":  "R",
		"  plan":   "P",
		"周报":       "周",
		"📚 读书笔记":   "读",
		"2026 规划":  "2",
		"---":      "·",
		"":         "·",
		"   🚀✨   ": "·",
	} {
		if got := docInitial(title); got != want {
			t.Errorf("docInitial(%q) = %q, want %q", title, got, want)
		}
	}
}

// Each supported file type gets its own distinct monochrome Lucide icon.
func TestFileIconGlyphsDistinct(t *testing.T) {
	samples := map[string]string{
		"markdown":   "Roadmap.md",
		"database":   "Tasks.db.md",
		"whiteboard": "Diagram.excalidraw.svg",
		"pdf":        "Paper.pdf",
		"epub":       "Novel.epub",
		"html":       "Page.html",
	}
	seen := map[*ui.SVG]string{}
	for kind, rel := range samples {
		g := fileIconGlyph(rel)
		if g == nil {
			t.Fatalf("fileIconGlyph(%q) returned nil", rel)
		}
		if prev, dup := seen[g]; dup {
			t.Errorf("%s and %s share the same icon glyph", prev, kind)
		}
		seen[g] = kind
	}
}

// TestDocIconSnapshot renders favorites and a tree of varied file types, for
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
			files := []struct {
				rel, content string
				fav          bool
			}{
				{"周报.md", "# 周报\n", true},
				{"Roadmap.md", "# Roadmap\n", true},
				{"📚 读书笔记.md", "# 读书笔记\n", true},
				{"✈️ Trip.md", "# Trip\n", true},
				{"会议记录.md", "# 会议记录\n", false},
				{"项目看板.db.md", "# 项目看板\n", false},
				{"架构草图.excalidraw.svg", "<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>", false},
				{"设计规范.pdf", "%PDF-1.4\n", false},
				{"三体.epub", "", false},
				{"产品页.html", "<html><body>Hi</body></html>", false},
			}
			for _, f := range files {
				os.WriteFile(filepath.Join(sp.Root, f.rel), []byte(f.content), 0o644)
				if f.fav {
					a.state.ToggleFavorite(sp.ID, f.rel)
				}
			}
			a.refreshTree()
			settle(tt)
			writePNG(t, filepath.Join(dir, fmt.Sprint(name, ".png")), tt.Image())
		})
	}
}
