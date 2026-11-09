package toolutil

import (
	"slices"
	"testing"
)

func TestEnvReplacesAndRemovesInheritedTargets(t *testing.T) {
	base := []string{"HOME=/users/example", "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=1", "PATH=/bin"}
	got := Env(base, map[string]string{"GOOS": "", "GOARCH": "", "CGO_ENABLED": "0"})
	slices.Sort(got)
	want := []string{"CGO_ENABLED=0", "HOME=/users/example", "PATH=/bin"}
	if !slices.Equal(got, want) || base[1] != "GOOS=windows" {
		t.Fatalf("incorrect child-process environment: %v", got)
	}
}
