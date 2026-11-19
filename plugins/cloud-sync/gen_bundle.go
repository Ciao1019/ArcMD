//go:build ignore

package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gen_bundle: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	tmpDir, err := os.MkdirTemp("", "arcmd-cloud-sync-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	wasmPath := filepath.Join(tmpDir, "plugin.wasm")
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", wasmPath, ".")
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build wasm: %w", err)
	}

	manifestBytes, err := os.ReadFile("arcmd-plugin.json")
	if err != nil {
		return err
	}
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, data []byte) error {
		hdr := &zip.FileHeader{
			Name:     name,
			Method:   zip.Deflate,
			Modified: time.Unix(0, 0).UTC(),
		}
		hdr.SetMode(0o644)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	if err := add("arcmd-plugin.json", manifestBytes); err != nil {
		return err
	}
	if err := add("plugin.wasm", wasmBytes); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}

	outPath := filepath.Join("bundle", "cloud-sync.zip")
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
		return err
	}
	sum := sha256.Sum256(buf.Bytes())
	fmt.Printf("built %s (wasm=%d bytes, zip=%d bytes, sha256=%s)\n",
		outPath, len(wasmBytes), buf.Len(), hex.EncodeToString(sum[:]))
	return nil
}
