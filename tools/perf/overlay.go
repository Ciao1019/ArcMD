package main

import (
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ciao1019/ArcMD/tools/internal/toolutil"
)

func overlayCommand(destination string) error {
	root, err := toolutil.Root()
	if err != nil {
		return err
	}
	dir, err := prepareOverlay(root, destination)
	if err != nil {
		return err
	}
	fmt.Println(filepath.Join(dir, "overlay.json"))
	fmt.Println(filepath.Join(dir, "bench.mod"))
	return nil
}
func prepareOverlay(root, destination string) (string, error) {
	out, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return "", err
	}
	raw, err := toolutil.Output(root, nil, "go", "list", "-m", "-json", "github.com/egoist/mygo")
	if err != nil {
		return "", err
	}
	var module struct{ Version, Dir string }
	if err := json.Unmarshal([]byte(raw), &module); err != nil {
		return "", err
	}
	if module.Version != "v0.7.4" {
		return "", fmt.Errorf("review the timing adapter for MyGo %s", module.Version)
	}
	moduleCopy := filepath.Join(out, "mygo")
	if _, err := os.Stat(moduleCopy); os.IsNotExist(err) {
		if err := toolutil.CopyTree(module.Dir, moduleCopy); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	modfile := filepath.Join(out, "bench.mod")
	if err := toolutil.CopyFile(filepath.Join(root, "go.mod"), modfile); err != nil {
		return "", err
	}
	if err := toolutil.CopyFile(filepath.Join(root, "go.sum"), filepath.Join(out, "bench.sum")); err != nil {
		return "", err
	}
	if _, err := toolutil.Output(root, nil, "go", "mod", "edit", "-modfile", modfile, "-replace", "github.com/egoist/mygo="+moduleCopy); err != nil {
		return "", err
	}
	source := filepath.Join(moduleCopy, "ui/headless.go")
	data, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	patched, err := patchHeadless(string(data))
	if err != nil {
		return "", err
	}
	target := filepath.Join(out, "headless.go")
	if err := os.WriteFile(target, patched, 0o644); err != nil {
		return "", err
	}
	err = toolutil.WriteJSON(filepath.Join(out, "overlay.json"), map[string]any{"Replace": map[string]string{source: target}})
	return out, err
}
func patchHeadless(source string) ([]byte, error) {
	for _, edit := range [][2]string{
		{"\th.img.Render(s)", "\tif !h.perfNoRaster { h.img.Render(s) }"},
		{"type headless struct {", "type headless struct {\n\tperfNoRaster bool"},
		{"h := &headless{w:", "h := &headless{perfNoRaster: true, w:"},
	} {
		if strings.Count(source, edit[0]) != 1 {
			return nil, fmt.Errorf("unexpected MyGo adapter anchor %q", edit[0])
		}
		source = strings.Replace(source, edit[0], edit[1], 1)
	}
	return format.Source([]byte(source + "\n" + timingMethods))
}

// The adapter exists only in a temporary module copy, never in production builds.
const timingMethods = `// PerfPrepare disables test-only accessibility label collection and fixes scale.
// These methods exist only in the performance overlay, never in shipped builds.
func (t *Tester) PerfPrepare() {
    t.rt.collect = false
    t.h.scale = 2
    t.rt.changed()
    t.settle()
}

// PerfOperation dispatches one event and constructs exactly one CPU scene.
// GPU submission, rasterization and display latency are outside both timers.
func (t *Tester) PerfOperation(action string) (step, frame time.Duration) {
    t.now = t.now.Add(10 * time.Millisecond)
    event := platform.SurfaceEvent{}
    switch action {
    case "type":
        event = platform.SurfaceEvent{Kind: platform.TextInput, Text: "x"}
    case "backspace":
        event = platform.SurfaceEvent{Kind: platform.KeyPressed, Key: platform.Key(KeyBackspace)}
    case "scroll":
        event = platform.SurfaceEvent{Kind: platform.PointerScroll, X: 900, Y: 500, DY: 40, Precise: true}
    default:
        panic("unknown performance operation")
    }
    start := time.Now()
    t.rt.event(event)
    step = time.Since(start)
    frame = t.PerfFrame()
    return
}

// PerfPointer measures one split-divider event and one CPU scene.
func (t *Tester) PerfPointer(action string, x,y float32) (step,frame time.Duration) {
    t.now=t.now.Add(16*time.Millisecond)
    ev:=platform.SurfaceEvent{X:float64(x),Y:float64(y)}
    switch action {
    case "press": ev.Kind=platform.PointerDown;ev.Clicks=1
    case "move": ev.Kind=platform.PointerMove
    case "release": ev.Kind=platform.PointerUp
    default: panic("unknown pointer operation")
    }
    start:=time.Now()
    t.rt.event(ev)
    step=time.Since(start)
    frame=t.PerfFrame()
    return
}

// PerfFrame constructs one frame without the Tester's multi-frame settling.
func (t *Tester) PerfFrame() time.Duration {
    start := time.Now()
    t.h.requested.Store(false)
    t.h.invalidated.Store(false)
    t.runLater()
    t.rt.runFrame()
    return time.Since(start)
}

// PerfSnapshot rasterizes the last scene outside the timing region.
func (t *Tester) PerfSnapshot() *image.RGBA {
    t.h.img.Render(t.h.last)
    return t.h.image()
}
`
