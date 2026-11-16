package app

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"
)

func TestBotMotionMatchesUpstream(t *testing.T) {
	data, err := os.ReadFile("testdata/qabot/motion.json")
	if err != nil {
		t.Fatal(err)
	}
	var scenarios []struct {
		Seed   float64
		Frames []struct {
			Step int
			Pose botPose
		}
	}
	if err = json.Unmarshal(data, &scenarios); err != nil {
		t.Fatal(err)
	}
	if len(scenarios) != 2 {
		t.Fatal("expected two upstream scenarios")
	}
	for _, sc := range scenarios {
		if len(sc.Frames) != 300 {
			t.Fatal("expected 300 sampled poses per scenario")
		}
		sim := newBotMotion(sc.Seed, botIdle)
		frame := 0
		for i := range 1800 {
			switch i {
			case 60:
				sim.setPointer(1, -.5, 1)
			case 120:
				sim.poke()
			case 240:
				sim.setPointer(-.8, .6, 1)
			case 360:
				sim.setPointer(0, 0, 0)
			case 480:
				sim.setState(botWorking, false)
			case 720:
				sim.setState(botSleeping, false)
			case 960:
				sim.setState(botIdle, false)
			}
			sim.update(1.0 / 60)
			if frame >= len(sc.Frames) || sc.Frames[frame].Step != i+1 {
				continue
			}
			got, want := reflect.ValueOf(sim.pose), reflect.ValueOf(sc.Frames[frame].Pose)
			for f := range got.NumField() {
				g, w := got.Field(f), want.Field(f)
				if g.Kind() == reflect.Array {
					for j := range g.Len() {
						if math.Abs(g.Index(j).Float()-w.Index(j).Float()) > 1e-6 {
							t.Fatalf("seed %v step %d state weights differ", sc.Seed, i+1)
						}
					}
				} else if math.IsNaN(g.Float()) || math.Abs(g.Float()-w.Float()) > 1e-6 {
					t.Fatalf("seed %v step %d %s: %.12g != %.12g", sc.Seed, i+1, got.Type().Field(f).Name, g.Float(), w.Float())
				}
			}
			frame++
		}
	}
}
