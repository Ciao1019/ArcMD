// Package toolutil contains shared filesystem and process helpers for developer tools.
package toolutil

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func Root() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.Contains(string(data), "module github.com/Ciao1019/ArcMD") {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("run this command inside the ArcMD repository")
		}
		dir = parent
	}
}

// Env replaces or removes named variables without duplicating inherited entries.
func Env(base []string, changes map[string]string) []string {
	result := make([]string, 0, len(base)+len(changes))
	for _, item := range base {
		key, _, _ := strings.Cut(item, "=")
		if _, replaced := changes[key]; !replaced {
			result = append(result, item)
		}
	}
	for key, value := range changes {
		if value != "" {
			result = append(result, key+"="+value)
		}
	}
	return result
}

func Output(dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir, cmd.Env = dir, env
	var stderr strings.Builder
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %v: %w\n%s", name, args, err, stderr.String())
	}
	return strings.TrimSpace(string(data)), nil
}

func Run(ctx context.Context, dir string, env []string, output io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = dir, env, output, output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v: %w", name, args, err)
	}
	return nil
}

func ReadJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func WriteJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func Hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func CopyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// CopyTree requires a new destination and rejects links outside the source tree.
func CopyTree(source, target string) error {
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		return fmt.Errorf("destination must be new: %s", target)
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not supported: %s", path)
		}
		return CopyFile(path, dst)
	})
}
