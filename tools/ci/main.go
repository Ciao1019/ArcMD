// Command ci validates versions and collects native release packages.
package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Ciao1019/ArcMD/tools/internal/toolutil"
)

const mygo = "github.com/egoist/mygo"

type config struct {
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Version string `json:"version"`
}

func metadata(root string, getenv func(string) string) (config, error) {
	var c config
	if err := toolutil.ReadJSON(filepath.Join(root, "mygo.json"), &c); err != nil {
		return c, err
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$`).MatchString(c.Version) {
		return c, fmt.Errorf("mygo.json version must be X.Y.Z or X.Y.Z-prerelease")
	}
	if c.Name == "" || c.Slug == "" || strings.ContainsAny(c.Name+c.Slug, `/\`) {
		return c, fmt.Errorf("package name and slug must be nonempty filenames")
	}
	data, err := os.ReadFile(filepath.Join(root, "internal/app/app.go"))
	if err != nil {
		return c, err
	}
	// Windows checkouts may use CRLF even though the source is stored as LF.
	match := regexp.MustCompile(`(?m)^const Version = "([^"]+)"\r?$`).FindSubmatch(data)
	if len(match) != 2 || string(match[1]) != c.Version {
		return c, fmt.Errorf("mygo.json version must match internal/app.Version")
	}
	if getenv("GITHUB_REF_TYPE") == "tag" && getenv("GITHUB_REF_NAME") != "v"+c.Version {
		return c, fmt.Errorf("release tag must match v%s", c.Version)
	}
	return c, nil
}

func checkBackend(imports []string, target string) error {
	if slices.Contains(imports, mygo+"/internal/unsupported") {
		return fmt.Errorf("%s has no MyGo native backend; refusing to package a binary that cannot open a window", target)
	}
	return nil
}

func collect(root string, c config, platform string) ([]string, error) {
	goos, goarch, ok := strings.Cut(platform, "/")
	if !ok {
		return nil, fmt.Errorf("invalid platform %q", platform)
	}
	target := goos + "-" + goarch
	build, release := filepath.Join(root, "dist", target), filepath.Join(root, "dist/release")
	if err := os.MkdirAll(release, 0o755); err != nil {
		return nil, err
	}
	var names []string
	switch goos {
	case "darwin":
		names = []string{fmt.Sprintf("%s-%s-%s.dmg", c.Slug, c.Version, goarch)}
	case "windows":
		names = []string{fmt.Sprintf("%s-setup-%s-%s.exe", c.Slug, c.Version, goarch)}
	case "linux":
		version := c.Version
		if base, pre, ok := strings.Cut(version, "-"); ok {
			version = base + "~" + strings.ReplaceAll(pre, "-", ".")
		}
		names = []string{fmt.Sprintf("%s_%s_%s.deb", c.Slug, version, goarch), fmt.Sprintf("%s-%s-%s.tar.gz", c.Slug, c.Version, target)}
	default:
		return nil, fmt.Errorf("unsupported platform %q", platform)
	}
	// A successful compiler exit is insufficient when an optional packager is absent.
	for _, name := range names {
		info, err := os.Stat(filepath.Join(build, name))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return nil, fmt.Errorf("missing or empty package: %s", filepath.Join(build, name))
		}
	}
	// A build signed for updates also writes the platform's manifest, the
	// archive it names and deltas from earlier versions, which installed
	// apps download from the release.
	updates, err := updateFiles(build, c, target)
	if err != nil {
		return nil, err
	}
	for _, name := range updates {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	portable := ""
	if goos == "windows" {
		info, err := os.Stat(filepath.Join(build, c.Name+".exe"))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return nil, fmt.Errorf("missing executable: %s.exe", c.Name)
		}
		portable = fmt.Sprintf("%s-%s-%s.zip", c.Slug, c.Version, target)
		if err := portableZIP(build, filepath.Join(release, portable), names); err != nil {
			return nil, err
		}
	}
	for _, name := range names {
		if err := toolutil.CopyFile(filepath.Join(build, name), filepath.Join(release, name)); err != nil {
			return nil, err
		}
	}
	if portable != "" {
		names = append(names, portable)
	}
	slices.Sort(names)
	var checksums strings.Builder
	for _, name := range names {
		hash, err := toolutil.HashFile(filepath.Join(release, name))
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&checksums, "%s  %s\n", hash, name)
	}
	// LF is required even on Windows: the release job verifies these on Linux.
	if err := os.WriteFile(filepath.Join(release, "SHA256SUMS-"+target+".txt"), []byte(checksums.String()), 0o644); err != nil {
		return nil, err
	}
	return names, nil
}

// updateFiles lists the update files of a build in dir: none when it was
// not signed, else the manifest, the archive and any deltas.
func updateFiles(dir string, c config, target string) ([]string, error) {
	manifest := "update-" + target + ".json"
	if _, err := os.Stat(filepath.Join(dir, manifest)); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	archive := fmt.Sprintf("%s-%s-%s.tar.gz", c.Slug, c.Version, target)
	if info, err := os.Stat(filepath.Join(dir, archive)); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return nil, fmt.Errorf("update manifest without its archive: %s", filepath.Join(dir, archive))
	}
	names := []string{manifest, archive}
	deltas, err := filepath.Glob(filepath.Join(dir, "*.delta"))
	if err != nil {
		return nil, err
	}
	for _, d := range deltas {
		names = append(names, filepath.Base(d))
	}
	return names, nil
}

func portableZIP(source, target string, excluded []string) (err error) {
	f, err := os.Create(target)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()
	archive := zip.NewWriter(f)
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || slices.Contains(excluded, entry.Name()) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("not a regular resource: %s", path)
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		header.Method = zip.Deflate
		dest, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(dest, in)
		return err
	})
	closeErr := archive.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Println("Usage: go run ./tools/ci version | build <darwin/arm64|darwin/amd64|windows/amd64|linux/amd64|linux/arm64>")
		return nil
	}
	if !(len(args) == 1 && args[0] == "version") && !(len(args) == 2 && args[0] == "build") {
		return fmt.Errorf("expected version or build <platform>")
	}
	root, err := toolutil.Root()
	if err != nil {
		return err
	}
	c, err := metadata(root, os.Getenv)
	if err != nil {
		return err
	}
	if args[0] == "version" {
		fmt.Println(c.Version)
		return nil
	}
	target := args[1]
	if !slices.Contains([]string{"darwin/amd64", "darwin/arm64", "windows/amd64", "windows/386", "linux/amd64", "linux/arm64"}, target) {
		return fmt.Errorf("unsupported platform %q", target)
	}
	goos, arch, _ := strings.Cut(target, "/")
	env := toolutil.Env(os.Environ(), map[string]string{"GOOS": goos, "GOARCH": arch, "CGO_ENABLED": "0"})
	raw, err := toolutil.Output(root, env, "go", "list", "-json", mygo)
	if err != nil {
		return err
	}
	var pkg struct{ Imports []string }
	if err := json.Unmarshal([]byte(raw), &pkg); err != nil {
		return err
	}
	if err := checkBackend(pkg.Imports, target); err != nil {
		return err
	}
	version, err := toolutil.Output(root, nil, "go", "list", "-m", "-f", "{{.Version}}", mygo)
	if err != nil {
		return err
	}
	if version == "" {
		return fmt.Errorf("go.mod must pin a released MyGo version")
	}
	env = toolutil.Env(os.Environ(), map[string]string{"GOOS": "", "GOARCH": "", "CGO_ENABLED": "0"})
	if err := toolutil.Run(context.Background(), root, env, os.Stdout, "go", "run", mygo+"/cmd/mygo@"+version, "build", "-platform", target, "-o", "dist"); err != nil {
		return err
	}
	names, err := collect(root, c, target)
	if err != nil {
		return err
	}
	for _, name := range names {
		fmt.Println(filepath.Join(root, "dist/release", name))
	}
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Packaging failed:", err)
		os.Exit(1)
	}
}
