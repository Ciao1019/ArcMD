package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/vault"
)

// TestOpenPinnedSnapshot renders the pinned section with an open document
// behind the shown one, its minus under the pointer, and a closed one
// dimmed, in light and dark.
func TestOpenPinnedSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render open pinned documents")
	for _, dark := range []bool{false, true} {
		name := "open-pinned"
		if dark {
			name += "-dark"
		}
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetDark(dark)
			a.state.Favorites = nil
			sp := a.state.ActiveSpace()
			os.WriteFile(filepath.Join(sp.Root, "Roadmap.arcmd"), nil, 0o644)
			vault.Write(sp.Root, "Roadmap.md", "# Roadmap\n")
			os.Remove(filepath.Join(sp.Root, "Roadmap.arcmd"))
			a.refreshTree()
			start := a.doc.rel
			a.togglePin(sp, ideasRel)
			a.togglePin(sp, "Roadmap.md")
			a.open(ideasRel)
			a.open(start)
			settle(tt)
			hoverRow(t, a, tt, ideasRel)
			settle(tt)
			savePNG(t, filepath.Join(dir, "feature-"+name+".png"), tt)
		})
	}
}
