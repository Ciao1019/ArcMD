package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/tools/internal/toolutil"
)

func write(t *testing.T, root, name, text string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
func TestVersionGates(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run(map[string]string{"\n": "LF", "\r\n": "CRLF"}[newline], func(t *testing.T) {
			testVersionGates(t, newline)
		})
	}
}

func testVersionGates(t *testing.T, newline string) {
	t.Helper()
	root := t.TempDir()
	write(t, root, "mygo.json", `{"name":"ArcMD","slug":"arcmd","version":"0.0.1"}`)
	write(t, root, "internal/app/app.go", "package app"+newline+"const Version = \"0.0.1\""+newline)
	env := map[string]string{"GITHUB_REF_TYPE": "tag", "GITHUB_REF_NAME": "v0.0.1"}
	get := func(k string) string { return env[k] }
	if _, err := metadata(root, get); err != nil {
		t.Fatal(err)
	}
	env["GITHUB_REF_NAME"] = "v0.2.0"
	if _, err := metadata(root, get); err == nil {
		t.Fatal("mismatched tag accepted")
	}
	env["GITHUB_REF_NAME"] = "v0.0.1"
	write(t, root, "internal/app/app.go", "const Version = \"0.2.0\""+newline)
	if _, err := metadata(root, get); err == nil {
		t.Fatal("mismatched app version accepted")
	}
}
func TestUnsupportedBackend(t *testing.T) {
	if err := checkBackend([]string{mygo + "/internal/unsupported"}, "windows/386"); err == nil {
		t.Fatal("stub GUI backend accepted")
	}
	if err := checkBackend([]string{mygo + "/internal/windows"}, "windows/amd64"); err != nil {
		t.Fatal(err)
	}
}
func TestMissingInstaller(t *testing.T) {
	root := t.TempDir()
	write(t, root, "dist/windows-amd64/ArcMD.exe", "app")
	if _, err := collect(root, config{"ArcMD", "arcmd", "0.1.0"}, "windows/amd64"); err == nil {
		t.Fatal("missing installer accepted")
	}
	entries, err := os.ReadDir(filepath.Join(root, "dist/release"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("partial release created")
	}
}
func TestPortableResources(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{"ArcMD.exe": "app", "LICENSE": "notice", "third_party/quiet-md/LICENSE": "third-party notice", "runtime.dll": "resource", "arcmd-setup-0.1.0-amd64.exe": "installer"}
	for name, text := range files {
		write(t, root, "dist/windows-amd64/"+name, text)
	}
	names, err := collect(root, config{"ArcMD", "arcmd", "0.1.0"}, "windows/amd64")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(filepath.Join(root, "dist/release/arcmd-0.1.0-windows-amd64.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	var got []string
	for _, f := range archive.File {
		got = append(got, f.Name)
	}
	slices.Sort(got)
	want := []string{"ArcMD.exe", "LICENSE", "runtime.dll", "third_party/quiet-md/LICENSE"}
	if !slices.Equal(got, want) {
		t.Fatalf("portable files: %q", got)
	}
	verifyChecksums(t, root, "windows-amd64", names)
}
func TestLinuxPrerelease(t *testing.T) {
	root := t.TempDir()
	write(t, root, "dist/linux-arm64/arcmd_0.2.0~beta.1_arm64.deb", "deb")
	write(t, root, "dist/linux-arm64/arcmd-0.2.0-beta-1-linux-arm64.tar.gz", "archive")
	names, err := collect(root, config{"ArcMD", "arcmd", "0.2.0-beta-1"}, "linux/arm64")
	if err != nil {
		t.Fatal(err)
	}
	verifyChecksums(t, root, "linux-arm64", names)
}
func TestEmptyDMG(t *testing.T) {
	root := t.TempDir()
	write(t, root, "dist/darwin-arm64/arcmd-0.1.0-arm64.dmg", "")
	if _, err := collect(root, config{"ArcMD", "arcmd", "0.1.0"}, "darwin/arm64"); err == nil {
		t.Fatal("empty DMG accepted")
	}
}
func verifyChecksums(t *testing.T, root, target string, names []string) {
	t.Helper()
	dir := filepath.Join(root, "dist/release")
	data, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS-"+target+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "\r") {
		t.Fatal("checksum file contains CRLF")
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != len(names) {
		t.Fatal("checksum count")
	}
	for _, line := range lines {
		hash, name, ok := strings.Cut(line, "  ")
		if !ok {
			t.Fatal(line)
		}
		expected, err := toolutil.HashFile(filepath.Join(dir, name))
		if err != nil || hash != expected {
			t.Fatalf("checksum %s: %v", name, err)
		}
	}
}

func TestUpdateFiles(t *testing.T) {
	root := t.TempDir()
	c := config{Name: "ArcMD", Slug: "arcmd", Version: "0.2.0"}
	write(t, root, "dist/darwin-arm64/arcmd-0.2.0-arm64.dmg", "dmg")
	names, err := collect(root, c, "darwin/arm64")
	if err != nil || !slices.Equal(names, []string{"arcmd-0.2.0-arm64.dmg"}) {
		t.Fatalf("an unsigned build has no update files: %v %v", names, err)
	}
	write(t, root, "dist/darwin-arm64/update-darwin-arm64.json", "{}")
	if _, err := collect(root, c, "darwin/arm64"); err == nil {
		t.Fatal("a manifest without its archive accepted")
	}
	write(t, root, "dist/darwin-arm64/arcmd-0.2.0-darwin-arm64.tar.gz", "app")
	write(t, root, "dist/darwin-arm64/arcmd-0.1.0-to-0.2.0-darwin-arm64.delta", "delta")
	names, err = collect(root, c, "darwin/arm64")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"arcmd-0.1.0-to-0.2.0-darwin-arm64.delta", "arcmd-0.2.0-arm64.dmg", "arcmd-0.2.0-darwin-arm64.tar.gz", "update-darwin-arm64.json"}
	if !slices.Equal(names, want) {
		t.Fatalf("names %v, want %v", names, want)
	}
	for _, n := range want {
		if _, err := os.Stat(filepath.Join(root, "dist/release", n)); err != nil {
			t.Fatal(err)
		}
	}
}
