// Command dev launches ArcMD in MyGo's hot-reload development runner with
// isolated data storage and CGO disabled.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/Ciao1019/ArcMD/tools/internal/toolutil"
)

const mygo = "github.com/egoist/mygo"

func devEnv(root string, base []string) (string, []string) {
	var home string
	for _, item := range base {
		if k, v, ok := strings.Cut(item, "="); ok && k == "ARCMD_HOME" {
			home = v
		}
	}
	if home == "" {
		home = filepath.Join(root, ".mygo", "data")
	}
	return home, toolutil.Env(base, map[string]string{
		"ARCMD_HOME":  home,
		"CGO_ENABLED": "0",
	})
}

func mygoArgs(version string, extra []string) ([]string, error) {
	if strings.TrimSpace(version) == "" {
		return nil, fmt.Errorf("go.mod must pin a released MyGo version")
	}
	args := make([]string, 0, 3+len(extra))
	args = append(args, "run", mygo+"/cmd/mygo@"+version, "dev")
	return append(args, extra...), nil
}

func run(args []string) error {
	root, err := toolutil.Root()
	if err != nil {
		return err
	}
	version, err := toolutil.Output(root, nil, "go", "list", "-m", "-f", "{{.Version}}", mygo)
	if err != nil {
		return err
	}
	cmdArgs, err := mygoArgs(version, args)
	if err != nil {
		return err
	}
	home, env := devEnv(root, os.Environ())
	fmt.Printf("Development data: %s\n", home)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cmd := exec.CommandContext(ctx, "go", cmdArgs...)
	cmd.Dir = root
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "Dev failed:", err)
		os.Exit(1)
	}
}
