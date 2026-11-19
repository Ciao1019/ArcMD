package main

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestDevEnvDefaultsAndOverrides(t *testing.T) {
	root := filepath.Join("repo", "ArcMD")
	wantDefault := filepath.Join(root, ".mygo", "data")

	home, env := devEnv(root, []string{"PATH=/usr/bin", "CGO_ENABLED=1"})
	if home != wantDefault {
		t.Fatalf("home = %q, want %q", home, wantDefault)
	}
	if !slices.Contains(env, "ARCMD_HOME="+wantDefault) {
		t.Fatalf("missing default ARCMD_HOME in %q", env)
	}
	if !slices.Contains(env, "CGO_ENABLED=0") || slices.Contains(env, "CGO_ENABLED=1") {
		t.Fatalf("CGO_ENABLED not forced to 0: %q", env)
	}

	custom := filepath.Join("tmp", "custom-home")
	home, env = devEnv(root, []string{"ARCMD_HOME=" + custom})
	if home != custom || !slices.Contains(env, "ARCMD_HOME="+custom) {
		t.Fatalf("custom ARCMD_HOME not preserved: home=%q env=%q", home, env)
	}
}

func TestMygoArgs(t *testing.T) {
	if _, err := mygoArgs("", nil); err == nil {
		t.Fatal("empty version accepted")
	}
	got, err := mygoArgs("v0.10.0", []string{"-v"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"run", "github.com/egoist/mygo/cmd/mygo@v0.10.0", "dev", "-v"}
	if !slices.Equal(got, want) {
		t.Fatalf("args = %q, want %q", got, want)
	}
}
