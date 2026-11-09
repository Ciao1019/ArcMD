package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/space"
	"github.com/egoist/mygo/ui"
)

func rowRect(a *App, rel string) ui.Rect {
	for _, r := range a.rows {
		if r.rel == rel {
			return r.r
		}
	}
	return ui.Rect{}
}

// Resting on a closed folder for 0.45 s previews its documents, newest
// first; typing filters, ↩ opens the one picked, and leaving closes it.
func TestFolderPreview(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	os.WriteFile(filepath.Join(sp.Root, "Journal", "Plans.md"), []byte("# Plans\n"), 0o644)
	a.refreshTree()
	sp.SetExpanded("Journal", false)
	settle(tt)
	r := rowRect(a, "Journal")
	tt.Move(r.X+40, r.Y+r.H/2)
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if a.preview.shown != nil {
		t.Fatal("opened before 0.45 s")
	}
	tt.Advance(200 * time.Millisecond)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if s := a.preview.shown; s == nil || s.kind != previewFolder || s.rel != "Journal" {
		t.Fatalf("no folder preview: %+v", s)
	}
	if len(a.preview.items) != 2 || a.preview.items[0].rel != "Journal/Plans.md" {
		t.Fatalf("items %+v", a.preview.items)
	}
	if !tt.HasText("Plans") || !tt.Focused("搜索文件夹") {
		t.Fatalf("card: %q", tt.Texts())
	}
	tt.Type("idea")
	tt.Frame()
	if !a.preview.extended || tt.HasText("Plans") {
		t.Fatalf("filter: extended %v, %q", a.preview.extended, tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.doc == nil || a.doc.rel != "Journal/Ideas.arcmd" || !a.preview.closing {
		t.Fatalf("pick: doc %+v closing %v", a.doc, a.preview.closing)
	}
	tt.Advance(time.Second)
	tt.Frame()
	if a.preview.shown != nil {
		t.Fatal("the card did not go")
	}
}

// Leaving the folder and the card closes it after a moment.
func TestPreviewCloses(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	a.state.ActiveSpace().SetExpanded("Journal", false)
	settle(tt)
	r := rowRect(a, "Journal")
	tt.Move(r.X+40, r.Y+r.H/2)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if a.preview.shown == nil {
		t.Fatal("no preview")
	}
	tt.Move(r.X+40, r.Y+300)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if a.preview.shown != nil {
		t.Fatal("still open after leaving")
	}
}

// Resting on a document shows its thumbnail.
func TestDocPreview(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	settle(tt)
	r := rowRect(a, "Journal/Ideas.arcmd")
	tt.Move(r.X+40, r.Y+r.H/2)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if s := a.preview.shown; s == nil || s.kind != previewDoc || s.rel != "Journal/Ideas.arcmd" {
		t.Fatalf("no document preview: %+v", s)
	}
	// The editor paints its text itself: the thumbnail is a second
	// document surface beside the open one.
	surfaces := 0
	for _, s := range tt.Texts() {
		if s == "文档" {
			surfaces++
		}
	}
	if surfaces < 2 {
		t.Fatalf("no thumbnail: %q", tt.Texts())
	}
}

// TestPreviewSnapshot renders both cards in a light, a dark and a pink
// space, for review.
func TestPreviewSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the previews")
	for _, look := range []string{"light", "dark", "pink"} {
		for _, kind := range []string{"folder", "doc"} {
			t.Run(look+"-"+kind, func(t *testing.T) {
				a, tt := newTestApp(t)
				tt.SetSize(1000, 620)
				tt.SetDark(look == "dark")
				a.state.Favorites = nil
				sp := a.state.ActiveSpace()
				if look == "pink" {
					sp.SetTheme(space.NewTheme(1))
				}
				for _, n := range []string{"Journal/周报.md", "Journal/Roadmap.md", "Journal/读书笔记.md"} {
					os.WriteFile(filepath.Join(sp.Root, n), []byte("# "+filepath.Base(n)+"\n\n今天写一点东西，记录想法。\n\n- 一\n- 二\n"), 0o644)
				}
				a.refreshTree()
				rel := "Welcome.arcmd"
				if kind == "folder" {
					sp.SetExpanded("Journal", false)
					rel = "Journal"
				}
				settle(tt)
				r := rowRect(a, rel)
				tt.Move(r.X+40, r.Y+r.H/2)
				tt.Frame()
				tt.Advance(time.Second)
				tt.Frame()
				tt.Advance(time.Second)
				tt.Frame()
				writePNG(t, filepath.Join(dir, "preview-"+look+"-"+kind+".png"), tt.Image())
			})
		}
	}
}
