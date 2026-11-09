package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Ciao1019/ArcMD/tools/internal/toolutil"
)

const fixtureName = "d0c00006-0000-4000-8000-000000000006.xml"

func readText(path string) (string, error) { data, err := os.ReadFile(path); return string(data), err }

type benchmarkOptions struct {
	QuietRepo, Library, Out, Size string
	Runs, Samples, Timeout        int
}

func benchmarkCommand(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	var o benchmarkOptions
	flags.StringVar(&o.QuietRepo, "quiet-repo", "", "Quiet.md checkout with a release binary")
	flags.StringVar(&o.Library, "library", "", "prepared synthetic library")
	flags.StringVar(&o.Out, "out", "", "new output directory")
	flags.StringVar(&o.Size, "size", "1m", "10k, 100k or 1m")
	flags.IntVar(&o.Runs, "runs", 3, "fresh-process rounds")
	flags.IntVar(&o.Samples, "samples", 100, "operations per case (1–1000)")
	flags.IntVar(&o.Timeout, "timeout", 3600, "ArcMD timeout in seconds per round")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || o.QuietRepo == "" || o.Library == "" || o.Out == "" {
		return fmt.Errorf("run requires --quiet-repo, --library and --out")
	}
	if o.Runs < 1 || o.Samples < 1 || o.Samples > 1000 || o.Timeout < 1 || !slices.Contains([]string{"10k", "100k", "1m"}, o.Size) {
		return fmt.Errorf("invalid run count, sample count, timeout or fixture size")
	}
	return benchmark(o)
}

func benchmark(o benchmarkOptions) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("the native comparison harness currently measures macOS; convert, overlay and summarize are portable")
	}
	root, err := toolutil.Root()
	if err != nil {
		return err
	}
	o.QuietRepo, err = filepath.Abs(o.QuietRepo)
	if err != nil {
		return err
	}
	o.Library, err = filepath.Abs(o.Library)
	if err != nil {
		return err
	}
	o.Out, err = filepath.Abs(o.Out)
	if err != nil {
		return err
	}
	binary := filepath.Join(o.QuietRepo, "target/release/quiet_md")
	fixture := filepath.Join(o.Library, "docs", fixtureName)
	raw, err := os.ReadFile(binary)
	if err != nil {
		return err
	}
	if !bytes.Contains(raw, []byte("devtools: cannot read")) {
		return fmt.Errorf("Quiet.md binary lacks the devtools script driver")
	}
	if _, err := os.Stat(fixture); err != nil {
		return fmt.Errorf("prepared synthetic library is required: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(o.Out), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(o.Out, 0o755); err != nil {
		return fmt.Errorf("output directory must be new: %w", err)
	}
	metadata, err := benchmarkMetadata(root, o, binary)
	if err != nil {
		return err
	}
	if _, err := convert(fixture, filepath.Join(o.Out, "fixtures")); err != nil {
		return err
	}
	// Always work on a copy; benchmark execution cannot change the source library.
	library := filepath.Join(o.Out, "library")
	if err := toolutil.CopyTree(o.Library, library); err != nil {
		return err
	}
	if err := toolutil.CopyFile(filepath.Join(o.Out, "fixtures", o.Size+".xml"), filepath.Join(library, "docs", fixtureName)); err != nil {
		return err
	}
	script, err := readText(filepath.Join(o.QuietRepo, "tools/harness/examples/readme-performance.txt"))
	if err != nil {
		return err
	}
	if strings.Count(script, "bench 100 ") != 3 {
		return fmt.Errorf("unexpected upstream benchmark script")
	}
	script = strings.ReplaceAll(script, "bench 100 ", fmt.Sprintf("bench %d ", o.Samples))
	if err := os.WriteFile(filepath.Join(o.Out, "operations.txt"), []byte(script), 0o644); err != nil {
		return err
	}
	overlay, err := prepareOverlay(root, filepath.Join(o.Out, "overlay"))
	if err != nil {
		return err
	}
	env := toolutil.Env(os.Environ(), map[string]string{"CGO_ENABLED": "0", "MYGO_FRAME_STATS": ""})
	arcBinary := filepath.Join(o.Out, "arcmd-perf.test")
	if err := toolutil.Run(context.Background(), root, env, os.Stdout, "go", "test", "-c", "-modfile", filepath.Join(overlay, "bench.mod"), "-overlay", filepath.Join(overlay, "overlay.json"), "-tags", "perfbench", "-o", arcBinary, "./internal/app"); err != nil {
		return err
	}
	hash, err := toolutil.HashFile(arcBinary)
	if err != nil {
		return err
	}
	metadata["arcmd_binary_sha256"] = hash
	if err := toolutil.WriteJSON(filepath.Join(o.Out, "metadata.json"), metadata); err != nil {
		return err
	}
	expected, err := fixtureText(filepath.Join(o.Out, "fixtures", o.Size+".xml"))
	if err != nil {
		return err
	}
	for trial := 1; trial <= o.Runs; trial++ {
		fmt.Printf("Round %d/%d: Quiet.md\n", trial, o.Runs)
		quietOut := filepath.Join(o.Out, fmt.Sprintf("quiet-%d", trial))
		if err := runQuiet(binary, library, script, quietOut); err != nil {
			return err
		}
		log, err := readText(filepath.Join(quietOut, "log"))
		if err != nil {
			return err
		}
		if _, err := parseQuietLog(log, o.Samples); err != nil {
			return err
		}
		restored, err := fixtureText(filepath.Join(quietOut, "lib/docs", fixtureName))
		if err != nil {
			return err
		}
		if restored != expected {
			return fmt.Errorf("Quiet.md did not restore fixture text after backspacing")
		}
		fmt.Print(log)
		fmt.Printf("Round %d/%d: ArcMD\n", trial, o.Runs)
		runEnv := toolutil.Env(env, map[string]string{"ARCMD_PERF_FIXTURE": filepath.Join(o.Out, "fixtures", o.Size+".arcmd"), "ARCMD_PERF_OUTPUT": filepath.Join(o.Out, fmt.Sprintf("arcmd-%d", trial)), "ARCMD_PERF_SAMPLES": strconv.Itoa(o.Samples)})
		logPath := filepath.Join(o.Out, fmt.Sprintf("arcmd-%d.log", trial))
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(o.Timeout+30)*time.Second)
		err = runLogged(ctx, root, runEnv, logPath, arcBinary, "-test.run", "^TestPerformance$", "-test.v", "-test.timeout", fmt.Sprintf("%ds", o.Timeout))
		cancel()
		if err != nil {
			return err
		}
		log, err = readText(logPath)
		if err != nil {
			return err
		}
		fmt.Print(log)
	}
	return nil
}

func runLogged(ctx context.Context, dir string, env []string, logPath, name string, args ...string) error {
	f, err := os.Create(logPath)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := toolutil.Run(ctx, dir, env, f, name, args...); err != nil {
		return fmt.Errorf("%w; see %s", err, logPath)
	}
	return nil
}
func runQuiet(binary, library, script, out string) error {
	if err := os.Mkdir(out, 0o755); err != nil {
		return err
	}
	lib := filepath.Join(out, "lib")
	if err := toolutil.CopyTree(library, lib); err != nil {
		return err
	}
	scriptPath := filepath.Join(out, "s.txt")
	if err := os.WriteFile(scriptPath, []byte(script+"\nquit\n"), 0o644); err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	env := []string{"HOME=" + home, "QUIET_MD_HOME=" + lib, "FDL_SCRIPT=" + scriptPath, "FDL_SHOTS=" + out, "PATH=/opt/homebrew/bin:/usr/bin:/bin"}
	for _, name := range []string{"FDL_TRACE", "FDL_NOPARK", "FDL_WINDOW_CAPTURE", "FDL_NOBLINK", "FDL_RECORD_FPS"} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	return runLogged(ctx, out, env, filepath.Join(out, "log"), binary)
}
func benchmarkMetadata(root string, o benchmarkOptions, binary string) (map[string]any, error) {
	hashes := map[string]string{}
	for _, folder := range []string{"cmd", "internal", "tools"} {
		if err := filepath.WalkDir(filepath.Join(root, folder), func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			hash, err := toolutil.HashFile(path)
			hashes[filepath.ToSlash(rel)] = hash
			return err
		}); err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		hash, err := toolutil.HashFile(filepath.Join(root, name))
		if err != nil {
			return nil, err
		}
		hashes[name] = hash
	}
	manifest := filepath.Join(o.Out, "source-manifest.json")
	if err := toolutil.WriteJSON(manifest, hashes); err != nil {
		return nil, err
	}
	digest, err := toolutil.HashFile(manifest)
	if err != nil {
		return nil, err
	}
	m := map[string]any{"arcmd_source_sha256": digest, "arcmd_source_files": hashes, "machine": runtime.GOARCH, "runs": o.Runs, "samples": o.Samples, "size": o.Size, "quiet_repository": o.QuietRepo, "arcmd_theme": "default light", "caveat": "CPU scene timing; native GPUI host versus MyGo test host; not a language benchmark"}
	for _, item := range []struct {
		key, dir, name string
		args           []string
	}{
		{"cpu", root, "sysctl", []string{"-n", "machdep.cpu.brand_string"}},
		{"platform", root, "sw_vers", []string{"-productVersion"}},
		{"arcmd_commit", root, "git", []string{"rev-parse", "HEAD"}},
		{"quiet_commit", o.QuietRepo, "git", []string{"rev-parse", "HEAD"}},
		{"go", root, "go", []string{"version"}},
		{"rust", root, "rustc", []string{"--version"}},
	} {
		value, err := toolutil.Output(item.dir, nil, item.name, item.args...)
		if err != nil {
			return nil, err
		}
		m[item.key] = value
	}
	m["platform"] = "macOS-" + m["platform"].(string) + "-" + runtime.GOARCH
	memory, err := toolutil.Output(root, nil, "sysctl", "-n", "hw.memsize")
	if err != nil {
		return nil, err
	}
	mem, err := strconv.ParseUint(memory, 10, 64)
	if err != nil {
		return nil, err
	}
	m["memory_bytes"] = mem
	hash, err := toolutil.HashFile(binary)
	if err != nil {
		return nil, err
	}
	m["quiet_binary_sha256"] = hash
	preferences := map[string]json.RawMessage{}
	if err := toolutil.ReadJSON(filepath.Join(o.Library, "preferences.json"), &preferences); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	m["quiet_preferences"] = preferences
	return m, nil
}
