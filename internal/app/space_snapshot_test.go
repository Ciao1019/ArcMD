package app

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestSpaceFormSnapshot renders the sidebar's new space form with one and
// two colors, while the system is light and dark, for review.
func TestSpaceFormSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the space form")
	for _, dark := range []bool{false, true} {
		for _, n := range []int{1, 2} {
			name := fmt.Sprintf("space-form-%d", n)
			if dark {
				name += "-dark"
			}
			t.Run(name, func(t *testing.T) {
				a, tt := newTestApp(t)
				tt.SetSize(1240, 700)
				tt.SetDark(dark)
				a.newSpaceForm()
				a.spaceForm.name = "旅行"
				if n == 2 {
					addKey(&a.spaceForm.theme)
				}
				for range 4 {
					tt.Frame()
				}
				file, err := os.Create(filepath.Join(dir, name+".png"))
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				if err := png.Encode(file, tt.Image()); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

// TestSpaceIconSnapshot renders the icon picker's two tabs and a space bar
// of emoji and symbol icons, for review.
func TestSpaceIconSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the icon picker")
	for _, name := range []string{"space-icons-emoji", "space-icons-symbols"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetSize(1240, 700)
			for i, icon := range [][2]string{{"📚", ""}, {"", "leaf"}, {"", "code-xml"}, {"✈️", ""}} {
				s := a.state.Add(fmt.Sprintf("Space %d", i), t.TempDir())
				s.SetIcon(icon[0], icon[1])
			}
			a.newSpaceForm()
			a.spaceForm.name = "旅行"
			a.spaceForm.cardOpen = false
			a.spaceForm.openIconPicker()
			a.spaceForm.picker.symbols = name == "space-icons-symbols"
			for range 4 {
				tt.Frame()
			}
			file, err := os.Create(filepath.Join(dir, name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := png.Encode(file, tt.Image()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestSpaceBarSnapshot renders the foot of the sidebar with a few spaces,
// light and dark, for review.
func TestSpaceBarSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the space bar")
	for _, dark := range []bool{false, true} {
		name := "space-bar"
		if dark {
			name += "-dark"
		}
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetSize(1240, 700)
			tt.SetDark(dark)
			for i, icon := range [][2]string{{"", "moon"}, {"📚", ""}} {
				s := a.state.Add(fmt.Sprintf("Space %d", i), t.TempDir())
				s.SetIcon(icon[0], icon[1])
			}
			for range 4 {
				tt.Frame()
			}
			file, err := os.Create(filepath.Join(dir, name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := png.Encode(file, tt.Image()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
